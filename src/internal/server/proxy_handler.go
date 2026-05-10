package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"opentela/internal/common"
	"opentela/internal/protocol"
	"opentela/internal/usage"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/axiomhq/axiom-go/axiom"
	"github.com/axiomhq/axiom-go/axiom/ingest"
	"github.com/buger/jsonparser"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/viper"
)

var (
	globalTransport *http.Transport
	transportOnce   sync.Once

	routingRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "otela_routing_requests_total",
			Help: "Total number of requests forwarded to workers",
		},
		[]string{"service", "status"},
	)
	routingRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "otela_routing_request_duration_seconds",
			Help:    "End-to-end forwarding latency",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"service"},
	)
	routingFallbackTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "otela_routing_fallback_total",
			Help: "Number of times each fallback tier was used",
		},
		[]string{"service", "level"},
	)
	routingRetriesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "otela_routing_retries_total",
			Help: "Retry outcomes during request forwarding",
		},
		[]string{"service", "outcome"},
	)
)

func init() {
	prometheus.MustRegister(routingRequestsTotal, routingRequestDuration, routingFallbackTotal, routingRetriesTotal)
}

func getGlobalTransport() *http.Transport {
	transportOnce.Do(func() {
		node, _ := protocol.GetP2PNode(nil)
		globalTransport = &http.Transport{
			ResponseHeaderTimeout: 10 * time.Minute, // Allow up to 10 minutes for response headers
			IdleConnTimeout:       60 * time.Second, // Keep connections alive for 60 seconds
			DisableKeepAlives:     false,            // Enable keep-alives for better performance
			MaxIdleConns:          512,              // Support large peer sets
			MaxIdleConnsPerHost:   4,                // Limit per-host to avoid head-of-line blocking
		}
		globalTransport.RegisterProtocol("libp2p", newLibp2pHTTPRoundTripper(node))
	})
	return globalTransport
}

func ErrorHandler(res http.ResponseWriter, req *http.Request, err error) {
	if _, werr := fmt.Fprintf(res, "ERROR: %s", err.Error()); werr != nil {
		common.Logger.Error("Error writing error response: ", werr)
	}
}

// StreamAwareResponseWriter wraps the response writer to handle streaming
type StreamAwareResponseWriter struct {
	http.ResponseWriter
	flusher http.Flusher
}

func (s *StreamAwareResponseWriter) WriteHeader(statusCode int) {
	// Enable streaming headers if this is a streaming response
	if s.ResponseWriter.Header().Get("Content-Type") == "text/event-stream" {
		s.ResponseWriter.Header().Set("Cache-Control", "no-cache")
		s.ResponseWriter.Header().Set("Connection", "keep-alive")
		s.ResponseWriter.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
	}
	s.ResponseWriter.WriteHeader(statusCode)
}

func (s *StreamAwareResponseWriter) Flush() {
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

// P2P handler for forwarding requests to other peers
func P2PForwardHandler(c *gin.Context) {
	// Set a longer timeout for AI/ML services
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Minute)
	defer cancel()
	// Pass the context to the request
	c.Request = c.Request.WithContext(ctx)

	requestPeer := c.Param("peerId")
	requestPath := c.Param("path")

	// Log event as before
	event := []axiom.Event{{ingest.TimestampField: time.Now(), "event": "P2P Forward", "from": &protocol.MyID, "to": requestPeer, "path": requestPath}}
	IngestEvents(event)

	target := url.URL{
		Scheme: "libp2p",
		Host:   requestPeer,
		Path:   requestPath,
	}
	common.Logger.Debugf("P2P forward: %s", target.String())

	director := func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Path = target.Path
		req.URL.Host = req.Host
		req.Host = target.Host
		// DO NOT read body here; httputil.ReverseProxy will stream it from c.Request.Body
	}

	proxy := httputil.NewSingleHostReverseProxy(&target)
	proxy.Director = director
	proxy.Transport = getGlobalTransport()
	proxy.ErrorHandler = ErrorHandler
	proxy.ModifyResponse = rewriteHeader()
	proxy.ServeHTTP(c.Writer, c.Request)
}

// ServiceHandler
func ServiceForwardHandler(c *gin.Context) {
	st := newStageTimer()
	serviceName := c.Param("service")
	requestPath := c.Param("path")
	service, err := protocol.GetService(serviceName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	target := url.URL{
		Scheme: "http",
		Host:   service.Host + ":" + service.Port,
		Path:   requestPath,
	}
	director := func(req *http.Request) {
		req.Host = target.Host
		req.URL.Host = req.Host
		req.URL.Scheme = target.Scheme
		req.URL.Path = target.Path
	}
	st.Mark("local_proxy")

	proxy := httputil.NewSingleHostReverseProxy(&target)
	proxy.Director = director
	// Use global transport here too if we want pooling to external HTTP services,
	// though standard http.DefaultTransport also pools.
	// However, if we want shared settings (timeouts), we can use ours.
	// NOTE: standard http transport doesn't support libp2p.
	// Ideally we separate p2p transport from standard http transport, OR register protocols on one.
	// Our getGlobalTransport() has registered libp2p, so it works for both (http falls back to standard).
	proxy.Transport = getGlobalTransport()

	proxy.ModifyResponse = func(r *http.Response) error {
		st.Mark("sglang_ttft")
		setWorkerTimingHeader(st, r)
		return nil
	}

	proxy.ServeHTTP(c.Writer, c.Request)
}

// parseFallbackLevel parses the value of the X-Otela-Fallback header.
// Valid values are 0, 1, or 2; anything else (including an empty string)
// returns 0 (the default, exact-match-only behaviour).
func parseFallbackLevel(header string) int {
	if header == "" {
		return 0
	}
	if lvl, err := strconv.Atoi(header); err == nil && lvl >= 0 && lvl <= 2 {
		return lvl
	}
	return 0
}

// selectCandidates iterates over the provided peers and returns the peer IDs
// that are eligible to serve the named service for the given request body.
//
// Match priority:  exact (3) > wildcard "*" (2) > catch-all "all" (1).
// fallbackLevel controls which tiers are considered when no exact match exists:
//
//	0 – exact matches only
//	1 – exact, then wildcard
//	2 – exact, then wildcard, then catch-all
func selectCandidates(providers []protocol.Peer, serviceName string, body []byte, fallbackLevel int) []string {
	var exactCandidates, wildcardCandidates, catchAllCandidates []string
	for _, provider := range providers {
		for _, service := range provider.Service {
			if service.Name == serviceName {
				// Track the best (highest-priority) match for this provider.
				// 0 = no match, 1 = catch-all, 2 = wildcard, 3 = exact
				bestMatch := 0
				if len(service.IdentityGroup) > 0 {
					for _, ig := range service.IdentityGroup {
						// "all" is a shortcut that matches every request
						if ig == "all" {
							if bestMatch < 1 {
								bestMatch = 1
							}
							continue
						}
						igGroup := strings.SplitN(ig, "=", 2)
						if len(igGroup) != 2 {
							continue
						}
						igKey := igGroup[0]
						igValue := igGroup[1]
						// "*" wildcard: match if the key exists in the request body (any value)
						if igValue == "*" {
							if _, _, _, err := jsonparser.Get(body, igKey); err == nil {
								if bestMatch < 2 {
									bestMatch = 2
								}
							}
							continue
						}
						// exact match
						requestGroup, err := jsonparser.GetString(body, igKey)
						if err == nil && requestGroup == igValue {
							bestMatch = 3
							break // can't do better than exact
						}
					}
				}
				switch bestMatch {
				case 3:
					exactCandidates = append(exactCandidates, provider.ID)
				case 2:
					wildcardCandidates = append(wildcardCandidates, provider.ID)
				case 1:
					catchAllCandidates = append(catchAllCandidates, provider.ID)
				}
				// Once we've recorded a match for this provider, avoid adding it again
				if bestMatch > 0 {
					break
				}
			}
		}
	}

	// Pick from the highest-priority non-empty tier, respecting fallback level
	candidates := exactCandidates
	if len(candidates) == 0 && fallbackLevel >= 1 {
		candidates = wildcardCandidates
	}
	if len(candidates) == 0 && fallbackLevel >= 2 {
		candidates = catchAllCandidates
	}
	return candidates
}

// weightedCandidate pairs a peer ID with a routing score.
type weightedCandidate struct {
	peerID string
	score  float64
}

// weightedRandomSelect picks a peer using weighted-random selection proportional
// to each candidate's score. Falls back to uniform random if all scores are zero.
func weightedRandomSelect(candidates []weightedCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	if len(candidates) == 1 {
		return candidates[0].peerID
	}

	totalWeight := 0.0
	for _, c := range candidates {
		totalWeight += c.score
	}
	if totalWeight <= 0 {
		return candidates[rand.Intn(len(candidates))].peerID
	}

	r := rand.Float64() * totalWeight
	cumulative := 0.0
	for _, c := range candidates {
		cumulative += c.score
		if r <= cumulative {
			return c.peerID
		}
	}
	return candidates[len(candidates)-1].peerID
}

// scoreCandidates assigns scores to candidate peer IDs. Currently all peers
// receive an equal default score of 1.0; this function is the extension point
// for richer scoring (latency, load, trust, etc.) in the future.
func scoreCandidates(candidateIDs []string) []weightedCandidate {
	result := make([]weightedCandidate, 0, len(candidateIDs))
	for _, id := range candidateIDs {
		score := 1.0 // Default score for non-scalable mode
		result = append(result, weightedCandidate{peerID: id, score: score})
	}
	return result
}

// excludePeers returns the candidates slice with any peer ID present in the
// excluded map removed. It is used to implement retry-with-peer-exclusion so
// that a failed request is not retried against the same worker.
func excludePeers(candidates []string, excluded map[string]bool) []string {
	var result []string
	for _, c := range candidates {
		if !excluded[c] {
			result = append(result, c)
		}
	}
	return result
}

// shouldShedLoad returns true when the head node should reject a request due to
// insufficient worker availability. It uses probabilistic load shedding: the
// acceptance rate is proportional to available/expected workers, so requests
// are rejected with probability (1 - available/expected).
func shouldShedLoad(available, expected int) bool {
	if expected <= 0 || available >= expected {
		return false
	}
	acceptRate := float64(available) / float64(expected)
	return rand.Float64() > acceptRate
}

// filterByTrust removes candidate peer IDs whose TrustLevel is below minTrust.
func filterByTrust(candidates []string, minTrust int) []string {
	var filtered []string
	for _, id := range candidates {
		peer, err := protocol.GetPeerFromTable(id)
		if err != nil {
			continue
		}
		if peer.TrustLevel >= minTrust {
			filtered = append(filtered, id)
		}
	}
	return filtered
}

// in case of global service, we need to forward the request to the service, identified by the service name and identity group
func GlobalServiceForwardHandler(c *gin.Context) {
	// Generate request ID for usage tracking
	requestID := usage.GenerateRequestID()
	c.Set("requestId", requestID)

	// Set a longer timeout for AI/ML services
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Minute)
	defer cancel()

	// Always buffer the request body so transport-level failures can retry
	// against a different worker. The retry loop replays bodyBytes on each
	// attempt.
	st := newStageTimer()
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	st.Mark("head_recv")
	c.Request = c.Request.WithContext(ctx)

	// matchBody is used *only* by selectCandidates to pick the identity group.
	// If the caller provides X-Otela-Identity-Group we synthesize a tiny JSON
	// object ({"key":"value"}) so the matcher works unchanged, without needing
	// to parse the real body for routing. The actual body forwarded to the
	// worker is always bodyBytes.
	matchBody := bodyBytes
	if identityGroupHeader := c.GetHeader("X-Otela-Identity-Group"); identityGroupHeader != "" {
		parts := strings.SplitN(identityGroupHeader, "=", 2)
		if len(parts) == 2 {
			obj := map[string]string{parts[0]: parts[1]}
			if b, err := json.Marshal(obj); err == nil {
				matchBody = b
			}
		}
	}

	serviceName := c.Param("service")
	routingStart := time.Now()
	requestPath := c.Param("path")
	providers, err := protocol.GetAllProviders(serviceName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Determine fallback level from the X-Otela-Fallback request header.
	// 0 (default): exact match only
	// 1: allow wildcard fallback when no exact match exists
	// 2: allow wildcard + catch-all fallback
	fallbackLevel := parseFallbackLevel(c.GetHeader("X-Otela-Fallback"))

	candidates := selectCandidates(providers, serviceName, matchBody, fallbackLevel)
	st.Mark("head_dnt")
	routingFallbackTotal.WithLabelValues(serviceName, strconv.Itoa(fallbackLevel)).Inc()
	if len(candidates) == 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "No provider found for the requested service."})
		return
	}

	// Trust-aware filtering: if the client specifies a minimum trust level
	// via X-Otela-Trust, remove candidates that don't meet the threshold.
	if trustHeader := c.GetHeader("X-Otela-Trust"); trustHeader != "" {
		if minTrust, err := strconv.Atoi(trustHeader); err == nil && minTrust > 0 {
			candidates = filterByTrust(candidates, minTrust)
			if len(candidates) == 0 {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"error": fmt.Sprintf("No provider meets the requested trust level (%d).", minTrust),
				})
				return
			}
		}
	}

	// Admission control: probabilistically reject requests when the number of
	// available workers is below the configured expected count.
	if viper.GetBool("scalability.admission_control") {
		expected := viper.GetInt("scalability.expected_workers")
		if expected > 0 && shouldShedLoad(len(candidates), expected) {
			c.Header("Retry-After", "5")
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Service degraded, try again later"})
			return
		}
	}

	// --- Retry loop: peer selection + proxy forwarding ---

	maxRetries := 0
	if viper.GetBool("routing.retry_enabled") {
		maxRetries = viper.GetInt("routing.max_retries")
	}
	maxBuffer := viper.GetInt("routing.max_response_buffer_bytes")
	excluded := make(map[string]bool)

	// Streaming detection — layer 1: request body "stream":true (OpenAI-compatible)
	// Layer 2: Accept header. Layer 3 is in retryableResponseWriter.WriteHeader.
	streamVal, streamType, _, _ := jsonparser.Get(bodyBytes, "stream")
	isStreaming := (streamType == jsonparser.Boolean && string(streamVal) == "true") ||
		strings.Contains(c.GetHeader("Accept"), "text/event-stream")

	requestPath = "/v1/_service/" + serviceName + requestPath
	clientWallet := resolveClientWallet(c)

	for attempt := 0; attempt <= maxRetries; attempt++ {
		remaining := excludePeers(candidates, excluded)
		if len(remaining) == 0 {
			break
		}

		// Select peer
		var targetPeer string
		if viper.GetBool("scalability.weighted_routing") {
			weighted := scoreCandidates(remaining)
			targetPeer = weightedRandomSelect(weighted)
		} else {
			targetPeer = remaining[rand.Intn(len(remaining))]
		}
		excluded[targetPeer] = true
		if attempt == 0 {
			st.Mark("head_peer_select")
		}

		// Clone request — ReverseProxy mutates URL, Host, X-Forwarded-*
		attemptReq := c.Request.Clone(ctx)
		attemptReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		attemptReq.Header.Set("X-Otela-Request-Id", requestID)

		event := []axiom.Event{{ingest.TimestampField: time.Now(), "event": "Service Forward", "from": protocol.MyID, "to": targetPeer, "path": requestPath, "service": serviceName}}
		IngestEvents(event)
		common.Logger.Debugf("Service forward: attempt=%d peer=%s path=%s", attempt+1, targetPeer, requestPath)

		// Resolve target URL (direct or via relay)
		var target url.URL
		if protocol.IsDirectlyConnected(targetPeer) {
			target = url.URL{Scheme: "libp2p", Host: targetPeer, Path: requestPath}
		} else {
			relayPeer := protocol.FindRelayFor(targetPeer)
			if relayPeer == "" {
				common.Logger.Warnf("No relay for peer %s, skipping", targetPeer)
				if attempt <= maxRetries {
					routingRetriesTotal.WithLabelValues(serviceName, strconv.Itoa(attempt+1)).Inc()
				}
				continue
			}
			relayPath := "/v1/p2p/" + targetPeer + requestPath
			common.Logger.Debugf("Relay hop: relay=%s path=%s", relayPeer[:12], relayPath)
			target = url.URL{Scheme: "libp2p", Host: relayPeer, Path: relayPath}
		}

		rw := newRetryableResponseWriter(c.Writer, isStreaming, maxBuffer)

		director := func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Path = target.Path
			req.URL.Host = req.Host
			req.Host = target.Host
			if clientWallet != "" {
				req.Header.Set("X-Otela-Client-Wallet", clientWallet)
			}
		}

		proxy := httputil.NewSingleHostReverseProxy(&target)
		proxy.Director = director
		proxy.Transport = getGlobalTransport()
		proxy.ErrorHandler = func(res http.ResponseWriter, req *http.Request, err error) {
			if rrw, ok := res.(*retryableResponseWriter); ok {
				common.Logger.Warnf("Transport error for peer %s: %v", targetPeer, err)
				rrw.markFailed()
				return
			}
			if _, werr := fmt.Fprintf(res, "ERROR: %s", err.Error()); werr != nil {
				common.Logger.Error("Error writing error response: ", werr)
			}
		}

		capturedPeer := targetPeer
		proxy.ModifyResponse = func(r *http.Response) error {
			st.Mark("head_p2p_to_worker_first_byte")
			mergeWorkerTiming(st, r)
			if hv := st.Header(); hv != "" {
				r.Header.Set("Server-Timing", hv)
			}
			if err := rewriteHeader()(r); err != nil {
				return err
			}
			r.Header.Set("X-Computing-Node", capturedPeer)
			if viper.GetBool("billing.enabled") {
				if metrics, err := usage.ExtractUsageMetrics(r); err == nil && len(metrics) > 0 {
					for metricName, value := range metrics {
						if err := usage.Track(requestID, serviceName, protocol.MyID, capturedPeer, metricName, value); err != nil {
							common.Logger.Errorf("Tracking usage: %v", err)
						}
					}
				}
			}
			return nil
		}

		proxy.ServeHTTP(rw, attemptReq)

		if rw.isRetryable() {
			routingRetriesTotal.WithLabelValues(serviceName, strconv.Itoa(attempt+1)).Inc()
			common.Logger.Warnf("Attempt %d/%d failed for peer %s, retrying",
				attempt+1, maxRetries+1, targetPeer)
			continue
		}

		// Success (or streaming already committed to client)
		if !rw.headersSent {
			rw.flushToClient()
		}
		if attempt > 0 {
			routingRetriesTotal.WithLabelValues(serviceName, "succeeded_after_retry").Inc()
		}
		status := strconv.Itoa(c.Writer.Status())
		routingRequestsTotal.WithLabelValues(serviceName, status).Inc()
		routingRequestDuration.WithLabelValues(serviceName).Observe(time.Since(routingStart).Seconds())
		return
	}

	// All attempts exhausted (or no candidates left)
	routingRetriesTotal.WithLabelValues(serviceName, "exhausted").Inc()
	routingRequestsTotal.WithLabelValues(serviceName, "502").Inc()
	routingRequestDuration.WithLabelValues(serviceName).Observe(time.Since(routingStart).Seconds())
	c.JSON(http.StatusBadGateway, gin.H{"error": "all workers unreachable for service " + serviceName})
}
