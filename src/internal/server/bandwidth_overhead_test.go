//go:build bandwidthbench

// Package server — opt-in bandwidth measurement.
//
// This test quantifies the *message bandwidth overhead* OpenTela adds when it
// distributes a workload over the libp2p mesh, versus sending the same payload
// to a plain HTTP backend. It stands up two real libp2p hosts in-process and
// drives traffic through the ACTUAL forwarding code paths:
//
//   - head side:   newLibp2pHTTPRoundTripper (internal/server/libp2p_http_transport.go)
//   - worker side: gostream.Listen on p2phttp.DefaultP2PProtocol (internal/server/p2p_listener.go)
//
// Bytes are counted with libp2p's own metrics.BandwidthCounter, which tags
// stream traffic by protocol ID. The data plane uses the "/libp2p-http"
// protocol, so GetBandwidthForProtocol isolates request-forwarding bytes from
// everything else on the connection.
//
// Run with:
//
//	cd src && go test -tags bandwidthbench -run TestDataPlaneBandwidthOverhead -v ./internal/server/
//
// It is build-tagged so it never runs in `make test` / CI.
package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"testing"
	"time"

	"opentela/internal/usage"

	"github.com/gin-gonic/gin"
	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/metrics"
	libp2ppeer "github.com/libp2p/go-libp2p/core/peer"

	gostream "github.com/libp2p/go-libp2p-gostream"
	p2phttp "github.com/libp2p/go-libp2p-http"
)

// representativeWallet is a real-length Solana base58 address (44 chars). In
// production X-Otela-Client-Wallet is attached by the director (proxy_handler.go:510)
// only when a verified Bearer token resolves to a wallet, i.e. auth/billing on.
const representativeWallet = "7vfCXTUXx5WJV5JADk17DUJ4ksgau7utNKj4b963voxs"

// dataPlaneProto is the protocol ID OpenTela forwards requests over.
// Value: "/libp2p-http" (see p2phttp.DefaultP2PProtocol).
var dataPlaneProto = p2phttp.DefaultP2PProtocol

// counterFlush is how long we wait for go-flow-metrics' background sweeper to
// fold pending Mark()s into the reported Total (it sweeps ~1s).
const counterFlush = 1300 * time.Millisecond

func newCountedHost(t *testing.T) (host.Host, *metrics.BandwidthCounter) {
	t.Helper()
	bwc := metrics.NewBandwidthCounter()
	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.BandwidthReporter(bwc),
	)
	if err != nil {
		t.Fatalf("libp2p.New: %v", err)
	}
	return h, bwc
}

// startWorker runs the real worker-side listener: a gostream listener on
// /libp2p-http serving an HTTP handler that drains the request body and
// returns a body of the size requested via the X-Resp-Size header.
func startWorker(t *testing.T, h host.Host) {
	t.Helper()
	ln, err := gostream.Listen(h, p2phttp.DefaultP2PProtocol)
	if err != nil {
		t.Fatalf("gostream.Listen: %v", err)
	}
	mux := http.NewServeMux()
	// Catch-all so both /echo (bare-transport test) and the rewritten
	// /v1/_service/... path (end-to-end test) are served.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		respSize := 0
		if v := r.Header.Get("X-Resp-Size"); v != "" {
			respSize, _ = strconv.Atoi(v)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		if respSize > 0 {
			_, _ = w.Write(make([]byte, respSize))
		}
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
}

func doRequest(t *testing.T, client *http.Client, workerID libp2ppeer.ID, reqBody []byte, respSize int) {
	t.Helper()
	url := fmt.Sprintf("libp2p://%s/echo", workerID)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Resp-Size", strconv.Itoa(respSize))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}
}

func TestDataPlaneBandwidthOverhead(t *testing.T) {
	ctx := context.Background()

	worker, _ := newCountedHost(t)
	head, headBW := newCountedHost(t)
	t.Cleanup(func() { _ = worker.Close(); _ = head.Close() })

	startWorker(t, worker)

	if err := head.Connect(ctx, libp2ppeer.AddrInfo{ID: worker.ID(), Addrs: worker.Addrs()}); err != nil {
		t.Fatalf("connect head->worker: %v", err)
	}
	client := &http.Client{Transport: newLibp2pHTTPRoundTripper(head)}

	// Warm up so the keep-alive stream is established; steady-state numbers
	// below then exclude the one-time connection handshake.
	doRequest(t, client, worker.ID(), nil, 0)
	time.Sleep(counterFlush)

	const N = 20

	// ---- Sweep request-body size, tiny (2 B) response -------------------
	// Isolates the request-direction overhead: bytes the head writes onto the
	// stream minus the raw payload it was asked to forward.
	t.Log("")
	t.Log("REQUEST direction  (vary request body, 2 B response)")
	t.Log("  payload      wireOut/req   overhead/req   ratio(wire/payload)")
	reqSizes := []int{0, 1024, 16 * 1024, 256 * 1024, 1024 * 1024}
	for _, sz := range reqSizes {
		headBW.Reset()
		body := make([]byte, sz)
		for i := 0; i < N; i++ {
			doRequest(t, client, worker.ID(), body, 2)
		}
		time.Sleep(counterFlush)
		p := headBW.GetBandwidthForProtocol(dataPlaneProto)
		perOut := float64(p.TotalOut) / N
		t.Logf("  %8d B   %9.1f B   %8.1f B     %.4f",
			sz, perOut, perOut-float64(sz), perOut/maxf(1, float64(sz)))
	}

	// ---- Sweep response-body size, tiny request -------------------------
	// Isolates response-direction overhead: bytes the head reads from the
	// stream minus the raw response payload.
	t.Log("")
	t.Log("RESPONSE direction  (vary response body, empty request)")
	t.Log("  payload       wireIn/req   overhead/req   ratio(wire/payload)")
	respSizes := []int{0, 1024, 16 * 1024, 256 * 1024, 1024 * 1024}
	for _, sz := range respSizes {
		headBW.Reset()
		for i := 0; i < N; i++ {
			doRequest(t, client, worker.ID(), nil, sz)
		}
		time.Sleep(counterFlush)
		p := headBW.GetBandwidthForProtocol(dataPlaneProto)
		perIn := float64(p.TotalIn) / N
		t.Logf("  %8d B   %9.1f B   %8.1f B     %.4f",
			sz, perIn, perIn-float64(sz), perIn/maxf(1, float64(sz)))
	}

	// ---- Compression check ----------------------------------------------
	// If OpenTela compressed bodies, a 1 MiB run of zeros would put far fewer
	// bytes on the wire than 1 MiB of incompressible data. Equal wire bytes
	// confirms the body is sent raw.
	t.Log("")
	t.Log("COMPRESSION check  (1 MiB request body, 2 B response)")
	const mib = 1024 * 1024
	zeros := make([]byte, mib)
	incompressible := make([]byte, mib)
	for i := range incompressible {
		incompressible[i] = byte(i*2654435761 + 1) // cheap non-repeating fill
	}
	for _, c := range []struct {
		name string
		body []byte
	}{{"zeros       ", zeros}, {"incompressible", incompressible}} {
		headBW.Reset()
		for i := 0; i < N; i++ {
			doRequest(t, client, worker.ID(), c.body, 2)
		}
		time.Sleep(counterFlush)
		p := headBW.GetBandwidthForProtocol(dataPlaneProto)
		t.Logf("  %s  wireOut/req=%9.1f B  (payload=%d B)", c.name, float64(p.TotalOut)/N, mib)
	}
}

// TestDataPlaneConnectionSetupCost measures the one-time libp2p connection
// overhead (security + muxer handshake + identify) by counting TOTAL bytes
// for a freshly-dialed connection that carries exactly one tiny request.
func TestDataPlaneConnectionSetupCost(t *testing.T) {
	ctx := context.Background()

	worker, _ := newCountedHost(t)
	head, headBW := newCountedHost(t)
	t.Cleanup(func() { _ = worker.Close(); _ = head.Close() })

	startWorker(t, worker)

	headBW.Reset()
	if err := head.Connect(ctx, libp2ppeer.AddrInfo{ID: worker.ID(), Addrs: worker.Addrs()}); err != nil {
		t.Fatalf("connect head->worker: %v", err)
	}
	client := &http.Client{Transport: newLibp2pHTTPRoundTripper(head)}
	doRequest(t, client, worker.ID(), nil, 2) // one tiny request on a cold connection
	time.Sleep(counterFlush)

	total := headBW.GetBandwidthTotals()
	stream := headBW.GetBandwidthForProtocol(dataPlaneProto)
	t.Log("")
	t.Logf("COLD connection (connect + 1 tiny request):")
	t.Logf("  total bytes out=%d in=%d", total.TotalOut, total.TotalIn)
	t.Logf("  of which /libp2p-http stream out=%d in=%d", stream.TotalOut, stream.TotalIn)
	t.Logf("  handshake/framing (total - stream) out=%d in=%d",
		total.TotalOut-stream.TotalOut, total.TotalIn-stream.TotalIn)
}

// mirrorForwardHandler reproduces the request-forwarding block of the real
// GlobalServiceForwardHandler (proxy_handler.go:457-547) for a fixed worker
// target, so the bytes put on the libp2p wire carry the exact production
// header set. It reuses the real rewriteHeader() and usage.GenerateRequestID(),
// and a real *httputil.ReverseProxy (which adds X-Forwarded-For just like prod).
//
// withWallet toggles whether X-Otela-Client-Wallet is attached (production does
// so only when auth/billing resolves a wallet — proxy_handler.go:510-512).
func mirrorForwardHandler(headHost host.Host, workerID libp2ppeer.ID, service string, withWallet bool) gin.HandlerFunc {
	rt := newLibp2pHTTPRoundTripper(headHost) // the real transport
	return func(c *gin.Context) {
		requestID := usage.GenerateRequestID() // real: "<unixnano>-<8hex>"
		bodyBytes, _ := io.ReadAll(c.Request.Body)

		// proxy_handler.go:457 — path is rewritten to the internal service path.
		requestPath := "/v1/_service/" + service + c.Param("path")
		target := url.URL{Scheme: "libp2p", Host: workerID.String(), Path: requestPath}

		attemptReq := c.Request.Clone(c.Request.Context())
		attemptReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		attemptReq.Header.Set("X-Otela-Request-Id", requestID) // proxy_handler.go:479

		director := func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host // peer id; the transport reads URL.Host
			req.URL.Path = target.Path
			req.Host = target.Host // Host header on the wire = peer id (as in prod)
			if withWallet {        // proxy_handler.go:510-512
				req.Header.Set("X-Otela-Client-Wallet", representativeWallet)
			}
		}

		proxy := httputil.NewSingleHostReverseProxy(&target)
		proxy.Director = director
		proxy.Transport = rt
		proxy.ModifyResponse = func(r *http.Response) error {
			if err := rewriteHeader()(r); err != nil { // proxy_handler.go:531 (real)
				return err
			}
			r.Header.Set("X-Computing-Node", workerID.String()) // proxy_handler.go:534
			return nil
		}
		proxy.ServeHTTP(c.Writer, attemptReq)
	}
}

// TestEndToEndProxyBandwidthOverhead measures the bytes the head puts on the
// libp2p wire (both directions) for a full client -> head -> worker -> head ->
// client round trip, WITH the production X-Otela-* headers attached. It drives
// a real http.Client against a real gin server, so the request carries the
// headers a real reverse proxy adds (X-Forwarded-For, Host rewrite, etc.).
func TestEndToEndProxyBandwidthOverhead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	worker, _ := newCountedHost(t)
	head, headBW := newCountedHost(t)
	t.Cleanup(func() { _ = worker.Close(); _ = head.Close() })

	startWorker(t, worker)
	if err := head.Connect(ctx, libp2ppeer.AddrInfo{ID: worker.ID(), Addrs: worker.Addrs()}); err != nil {
		t.Fatalf("connect head->worker: %v", err)
	}

	// Real gin head node exposing the production-shaped route.
	r := gin.New()
	r.POST("/v1/service/:service/*path", mirrorForwardHandler(head, worker.ID(), "llm", true))
	headSrv := httptest.NewServer(r)
	t.Cleanup(headSrv.Close)

	client := headSrv.Client()
	endpoint := headSrv.URL + "/v1/service/llm/v1/chat/completions"

	// A realistic client request body (small chat completion) and headers.
	doE2E := func(reqBody []byte, respSize int) {
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(reqBody))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer test-token") // exercises resolveClientWallet path
		req.Header.Set("X-Resp-Size", strconv.Itoa(respSize))
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("client.Do: %v", err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d", resp.StatusCode)
		}
	}

	// Warm up the kept-alive stream so steady-state excludes the handshake.
	doE2E([]byte(`{"model":"x"}`), 0)
	time.Sleep(counterFlush)

	const N = 20

	t.Log("")
	t.Log("END-TO-END, production headers attached (X-Otela-Request-Id + Client-Wallet + Computing-Node)")
	t.Log("Request leg = client->worker on libp2p ;  Response leg = worker->client on libp2p")
	t.Log("")
	t.Log("  reqBody  respBody    wireOut/req   wireIn/req   out-reqBody   in-respBody")

	type caseT struct{ reqSize, respSize int }
	cases := []caseT{
		{13, 2},                // tiny chat request, tiny response
		{1024, 1024},           // 1 KiB each way
		{16 * 1024, 64 * 1024}, // 16 KiB prompt, 64 KiB completion
		{1024 * 1024, 2},       // 1 MiB request
		{2, 1024 * 1024},       // 1 MiB response
	}
	for _, cs := range cases {
		headBW.Reset()
		body := make([]byte, cs.reqSize)
		// make the first bytes valid-ish JSON so nothing rejects it
		copy(body, []byte(`{"model":"llm","x":"`))
		for i := 0; i < N; i++ {
			doE2E(body, cs.respSize)
		}
		time.Sleep(counterFlush)
		p := headBW.GetBandwidthForProtocol(dataPlaneProto)
		outPer := float64(p.TotalOut) / N
		inPer := float64(p.TotalIn) / N
		t.Logf("  %7d  %8d   %10.1f B  %9.1f B   %8.1f B   %8.1f B",
			cs.reqSize, cs.respSize, outPer, inPer,
			outPer-float64(cs.reqSize), inPer-float64(cs.respSize))
	}
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
