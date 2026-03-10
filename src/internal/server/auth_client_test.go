package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

func TestResolveClientWalletNoAuthConfigured(t *testing.T) {
	viper.Set("security.auth_url", "")
	defer viper.Set("security.auth_url", "")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request, _ = http.NewRequest("GET", "/", nil)
	c.Request.Header.Set("Authorization", "Bearer some_token")

	wallet := resolveClientWallet(c)
	if wallet != "" {
		t.Fatalf("expected empty wallet when auth_url is not configured, got %q", wallet)
	}
}

func TestResolveClientWalletNoHeader(t *testing.T) {
	viper.Set("security.auth_url", "http://localhost:9999")
	defer viper.Set("security.auth_url", "")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request, _ = http.NewRequest("GET", "/", nil)

	wallet := resolveClientWallet(c)
	if wallet != "" {
		t.Fatalf("expected empty wallet when no auth header, got %q", wallet)
	}
}

func TestVerifyBearerTokenWithMockServer(t *testing.T) {
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
	defer viper.Set("security.auth_url", "")

	// Clear cache to avoid stale entries from other tests.
	tokenCache.mu.Lock()
	tokenCache.entries = make(map[string]authCacheEntry)
	tokenCache.mu.Unlock()

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
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer mock.Close()

	viper.Set("security.auth_url", mock.URL)
	defer viper.Set("security.auth_url", "")

	tokenCache.mu.Lock()
	tokenCache.entries = make(map[string]authCacheEntry)
	tokenCache.mu.Unlock()

	_, err := verifyBearerToken(context.Background(), "bad_token")
	if err == nil {
		t.Fatal("expected error for rejected token")
	}
}
