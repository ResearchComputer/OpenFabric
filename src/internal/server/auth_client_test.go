package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

func resetAuthClientTestState() {
	viper.Reset()
	viper.Set("seed", "0")
	viper.Set("tcpport", "43905")
	viper.Set("udpport", "59820")
	tokenCache.mu.Lock()
	tokenCache.entries = make(map[string]authCacheEntry)
	tokenCache.mu.Unlock()
	aclDecisionCache.mu.Lock()
	aclDecisionCache.entries = make(map[string]decisionCacheEntry)
	aclDecisionCache.mu.Unlock()
}

func TestResolveClientWalletNoAuthConfigured(t *testing.T) {
	resetAuthClientTestState()
	viper.Set("security.auth_url", "")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request, _ = http.NewRequest("GET", "/", nil)
	c.Request.Header.Set("Authorization", "Bearer some_token")

	wallet := resolveClientWallet(c)
	if wallet != "" {
		t.Fatalf("expected empty wallet when auth_url is not configured, got %q", wallet)
	}
}

func TestResolveClientWalletNoHeader(t *testing.T) {
	resetAuthClientTestState()
	viper.Set("security.auth_url", "http://localhost:9999")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request, _ = http.NewRequest("GET", "/", nil)

	wallet := resolveClientWallet(c)
	if wallet != "" {
		t.Fatalf("expected empty wallet when no auth header, got %q", wallet)
	}
}

func TestVerifyBearerTokenWithMockServer(t *testing.T) {
	resetAuthClientTestState()
	// Set up a mock auth server.
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/keys/verify" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(authVerifyResponse{
			Wallet: "5TestWallet",
			KeyID:  "okey_test",
		})
	}))
	defer mock.Close()

	viper.Set("security.auth_url", mock.URL)

	wallet, err := verifyBearerToken(context.Background(), "test_token_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wallet != "5TestWallet" {
		t.Fatalf("expected 5TestWallet, got %q", wallet)
	}

	// Second call should hit the cache.
	wallet2, err := verifyBearerToken(context.Background(), "test_token_123")
	if err != nil {
		t.Fatalf("unexpected error on cached call: %v", err)
	}
	if wallet2 != "5TestWallet" {
		t.Fatalf("expected cached 5TestWallet, got %q", wallet2)
	}
}

func TestVerifyBearerTokenRejected(t *testing.T) {
	resetAuthClientTestState()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer mock.Close()

	viper.Set("security.auth_url", mock.URL)

	_, err := verifyBearerToken(context.Background(), "bad_token")
	if err == nil {
		t.Fatal("expected error for rejected token")
	}
}

func TestEvaluateBearerForPeersHashesBearerAndCaches(t *testing.T) {
	resetAuthClientTestState()

	var requests int
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.Header.Get("Authorization"); got != "Bearer internal-token" {
			t.Fatalf("expected internal token auth header, got %q", got)
		}
		if r.URL.Path != "/internal/acl/evaluate" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		var req aclEvaluateRequest
		if err := decodeStrictJSON(r.Body, &req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		wantHash := sha256.Sum256([]byte("plain-bearer-token"))
		if req.KeyHash != fmt.Sprintf("%x", wantHash[:]) {
			t.Fatalf("unexpected key hash %q", req.KeyHash)
		}
		if len(req.PeerIDs) != 2 || req.PeerIDs[0] != "peer-a" || req.PeerIDs[1] != "peer-b" {
			t.Fatalf("unexpected peer list %#v", req.PeerIDs)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(aclEvaluateResponse{
			KeyID:           "okey_test",
			AllowedPeerIDs:  []string{"peer-a"},
			Denied:          []aclDeniedPeer{{PeerID: "peer-b", Reason: "no_match"}},
			PrimaryWallet:   "WalletPrimary",
			CacheTTLSeconds: 30,
		})
	}))
	defer mock.Close()

	viper.Set("security.control_plane.url", mock.URL)
	viper.Set("security.control_plane.token", "internal-token")
	viper.Set("security.control_plane.cache_ttl", "1m")
	viper.Set("security.control_plane.stale_if_error", "1m")

	decision, cpErr := evaluateBearerForPeers(context.Background(), "plain-bearer-token", []string{"peer-a", "peer-a", "peer-b"})
	if cpErr != nil {
		t.Fatalf("unexpected control-plane error: %v", cpErr)
	}
	if decision == nil || !decision.allows("peer-a") {
		t.Fatalf("expected peer-a to be allowed: %#v", decision)
	}
	if got := decision.DeniedReasons["peer-b"]; got != "no_match" {
		t.Fatalf("unexpected denial reason %q", got)
	}
	if decision.PrimaryWallet != "WalletPrimary" {
		t.Fatalf("unexpected primary wallet %q", decision.PrimaryWallet)
	}

	decision2, cpErr := evaluateBearerForPeers(context.Background(), "plain-bearer-token", []string{"peer-b", "peer-a"})
	if cpErr != nil {
		t.Fatalf("unexpected cached control-plane error: %v", cpErr)
	}
	if decision2 == nil || !decision2.allows("peer-a") {
		t.Fatalf("expected cached peer-a allow: %#v", decision2)
	}
	if requests != 1 {
		t.Fatalf("expected exactly one upstream request, got %d", requests)
	}
}

func TestEvaluateBearerForPeersUsesStaleCacheOnError(t *testing.T) {
	resetAuthClientTestState()

	var requests int
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(aclEvaluateResponse{
				KeyID:           "okey_test",
				AllowedPeerIDs:  []string{"peer-a"},
				Denied:          []aclDeniedPeer{},
				PrimaryWallet:   "WalletPrimary",
				CacheTTLSeconds: 1,
			})
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer mock.Close()

	viper.Set("security.control_plane.url", mock.URL)
	viper.Set("security.control_plane.token", "internal-token")
	viper.Set("security.control_plane.cache_ttl", "1ms")
	viper.Set("security.control_plane.stale_if_error", "1m")

	decision, cpErr := evaluateBearerForPeers(context.Background(), "stale-token", []string{"peer-a"})
	if cpErr != nil || decision == nil || !decision.allows("peer-a") {
		t.Fatalf("expected initial allow, got decision=%#v err=%v", decision, cpErr)
	}

	time.Sleep(10 * time.Millisecond)

	decision, cpErr = evaluateBearerForPeers(context.Background(), "stale-token", []string{"peer-a"})
	if cpErr != nil || decision == nil || !decision.allows("peer-a") {
		t.Fatalf("expected stale allow, got decision=%#v err=%v", decision, cpErr)
	}
	if requests != 2 {
		t.Fatalf("expected refresh attempt after cache expiry, got %d requests", requests)
	}
}

func TestEvaluateBearerForPeersTreatsControlAuthFailureAsUnavailable(t *testing.T) {
	t.Run("cold failure returns 503", func(t *testing.T) {
		resetAuthClientTestState()
		mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(controlAuthFailureHeader, "true")
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer mock.Close()

		viper.Set("security.control_plane.url", mock.URL)
		viper.Set("security.control_plane.token", "misconfigured-token")
		decision, cpErr := evaluateBearerForPeers(context.Background(), "valid-user-token", []string{"peer-a"})
		if decision != nil {
			t.Fatalf("expected no decision, got %#v", decision)
		}
		if cpErr == nil || cpErr.Status != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 control-plane error, got %#v", cpErr)
		}
	})

	t.Run("stale decision is reused", func(t *testing.T) {
		resetAuthClientTestState()
		var requests int
		mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			if requests == 1 {
				_ = json.NewEncoder(w).Encode(aclEvaluateResponse{
					KeyID:           "key-a",
					AllowedPeerIDs:  []string{"peer-a"},
					Denied:          []aclDeniedPeer{},
					CacheTTLSeconds: 1,
				})
				return
			}
			w.Header().Set(controlAuthFailureHeader, "true")
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer mock.Close()

		viper.Set("security.control_plane.url", mock.URL)
		viper.Set("security.control_plane.token", "rotated-token")
		viper.Set("security.control_plane.cache_ttl", "1ms")
		viper.Set("security.control_plane.stale_if_error", "1m")
		if _, cpErr := evaluateBearerForPeers(context.Background(), "valid-user-token", []string{"peer-a"}); cpErr != nil {
			t.Fatalf("prime cache: %v", cpErr)
		}
		time.Sleep(10 * time.Millisecond)

		decision, cpErr := evaluateBearerForPeers(context.Background(), "valid-user-token", []string{"peer-a"})
		if cpErr != nil || decision == nil || !decision.allows("peer-a") {
			t.Fatalf("expected stale allow, decision=%#v err=%v", decision, cpErr)
		}
	})
}

func TestEvaluateBearerForPeersCachedAllowExpiresIntoOwnershipMismatch(t *testing.T) {
	resetAuthClientTestState()
	var requests int
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		response := aclEvaluateResponse{
			KeyID:           "key-a",
			AllowedPeerIDs:  []string{"peer-a"},
			Denied:          []aclDeniedPeer{},
			CacheTTLSeconds: 1,
		}
		if requests > 1 {
			response.AllowedPeerIDs = nil
			response.Denied = []aclDeniedPeer{{PeerID: "peer-a", Reason: "ownership_mismatch"}}
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer mock.Close()

	viper.Set("security.control_plane.url", mock.URL)
	viper.Set("security.control_plane.token", "internal-token")
	viper.Set("security.control_plane.cache_ttl", "1ms")
	viper.Set("security.control_plane.stale_if_error", "1m")

	decision, cpErr := evaluateBearerForPeers(context.Background(), "user-token", []string{"peer-a"})
	if cpErr != nil || decision == nil || !decision.allows("peer-a") {
		t.Fatalf("expected initial allow, decision=%#v err=%v", decision, cpErr)
	}
	time.Sleep(10 * time.Millisecond)

	decision, cpErr = evaluateBearerForPeers(context.Background(), "user-token", []string{"peer-a"})
	if cpErr != nil || decision == nil || decision.allows("peer-a") {
		t.Fatalf("expected refreshed ownership denial, decision=%#v err=%v", decision, cpErr)
	}
	if got := decision.DeniedReasons["peer-a"]; got != "ownership_mismatch" {
		t.Fatalf("denial reason=%q, want ownership_mismatch", got)
	}
}

func TestEvaluateBearerForPeersCachedAllowFailsAfterStaleWindow(t *testing.T) {
	resetAuthClientTestState()
	var requests int
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			_ = json.NewEncoder(w).Encode(aclEvaluateResponse{
				KeyID:           "key-a",
				AllowedPeerIDs:  []string{"peer-a"},
				Denied:          []aclDeniedPeer{},
				CacheTTLSeconds: 1,
			})
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer mock.Close()

	viper.Set("security.control_plane.url", mock.URL)
	viper.Set("security.control_plane.token", "internal-token")
	viper.Set("security.control_plane.cache_ttl", "1ms")
	viper.Set("security.control_plane.stale_if_error", "1ms")

	if _, cpErr := evaluateBearerForPeers(context.Background(), "user-token", []string{"peer-a"}); cpErr != nil {
		t.Fatalf("prime cache: %v", cpErr)
	}
	time.Sleep(10 * time.Millisecond)

	decision, cpErr := evaluateBearerForPeers(context.Background(), "user-token", []string{"peer-a"})
	if decision != nil {
		t.Fatalf("expected expired cache to return no decision, got %#v", decision)
	}
	if cpErr == nil || cpErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 after stale window, got %#v", cpErr)
	}
}

func TestEvaluateBearerForPeersRejectsMalformedResponse(t *testing.T) {
	resetAuthClientTestState()

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key_id":"okey_test","allowed_peer_ids":["peer-a"],"denied":[],"primary_wallet":"WalletPrimary","cache_ttl_seconds":30}`))
	}))
	defer mock.Close()

	viper.Set("security.control_plane.url", mock.URL)
	viper.Set("security.control_plane.token", "internal-token")

	decision, cpErr := evaluateBearerForPeers(context.Background(), "bad-response-token", []string{"peer-a", "peer-b"})
	if decision != nil {
		t.Fatalf("expected malformed response to fail, got decision %#v", decision)
	}
	if cpErr == nil || cpErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 control-plane error, got %#v", cpErr)
	}
}

func TestEvaluateBearerForPeersMissingControlTokenFailsClosed(t *testing.T) {
	resetAuthClientTestState()

	var requests int
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer mock.Close()

	viper.Set("security.control_plane.url", mock.URL)
	viper.Set("security.control_plane.token", "")

	decision, cpErr := evaluateBearerForPeers(context.Background(), "user-token", []string{"peer-a"})
	if decision != nil {
		t.Fatalf("expected no decision, got %#v", decision)
	}
	if cpErr == nil || cpErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 control-plane error, got %#v", cpErr)
	}
	if requests != 0 {
		t.Fatalf("expected no network calls when token is missing, got %d", requests)
	}
}

func TestEvaluateBearerForPeersChunksLargeCandidateSets(t *testing.T) {
	resetAuthClientTestState()

	var requests int
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var req aclEvaluateRequest
		if err := decodeStrictJSON(r.Body, &req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.PeerIDs) == 0 || len(req.PeerIDs) > controlPlaneBatchLimit {
			t.Fatalf("batch size=%d, want 1..%d", len(req.PeerIDs), controlPlaneBatchLimit)
		}
		_ = json.NewEncoder(w).Encode(aclEvaluateResponse{
			KeyID:           "key-large",
			AllowedPeerIDs:  req.PeerIDs,
			Denied:          []aclDeniedPeer{},
			PrimaryWallet:   "WalletPrimary",
			CacheTTLSeconds: 30,
		})
	}))
	defer mock.Close()

	viper.Set("security.control_plane.url", mock.URL)
	viper.Set("security.control_plane.token", "internal-token")
	peers := make([]string, controlPlaneBatchLimit+1)
	for i := range peers {
		peers[i] = fmt.Sprintf("peer-%03d", i)
	}

	decision, cpErr := evaluateBearerForPeers(context.Background(), "large-token", peers)
	if cpErr != nil {
		t.Fatalf("evaluate large set: %v", cpErr)
	}
	if requests != 2 || len(decision.AllowedPeerIDs) != len(peers) {
		t.Fatalf("requests=%d allowed=%d, want 2/%d", requests, len(decision.AllowedPeerIDs), len(peers))
	}
}

func TestEvaluateBearerForPeersRejectsInconsistentPrimaryWalletAcrossBatches(t *testing.T) {
	resetAuthClientTestState()

	var requests int
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var req aclEvaluateRequest
		if err := decodeStrictJSON(r.Body, &req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		primaryWallet := ""
		if requests == 2 {
			primaryWallet = "WalletPrimary"
		}

		_ = json.NewEncoder(w).Encode(aclEvaluateResponse{
			KeyID:           "key-large",
			AllowedPeerIDs:  req.PeerIDs,
			Denied:          []aclDeniedPeer{},
			PrimaryWallet:   primaryWallet,
			CacheTTLSeconds: 30,
		})
	}))
	defer mock.Close()

	viper.Set("security.control_plane.url", mock.URL)
	viper.Set("security.control_plane.token", "internal-token")
	peers := make([]string, controlPlaneBatchLimit+1)
	for i := range peers {
		peers[i] = fmt.Sprintf("peer-%03d", i)
	}

	decision, cpErr := evaluateBearerForPeers(context.Background(), "large-token", peers)
	if decision != nil {
		t.Fatalf("expected nil decision, got %#v", decision)
	}
	if cpErr == nil || cpErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 malformed-response error, got %#v", cpErr)
	}
	if requests != 2 {
		t.Fatalf("requests=%d, want 2", requests)
	}
}
