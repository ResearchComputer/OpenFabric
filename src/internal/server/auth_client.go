package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"opentela/internal/common"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// authHTTPClient is a dedicated client with a short timeout so that a slow or
// unreachable auth server never hangs request handling indefinitely.
var authHTTPClient = &http.Client{Timeout: 5 * time.Second}

// authVerifyResponse is the JSON returned by POST /api/keys/verify.
type authVerifyResponse struct {
	Wallet string `json:"wallet"`
	KeyID  string `json:"key_id"`
}

// authCache caches token → wallet mappings to avoid hitting the auth server
// on every request.  Entries expire after 60 seconds.
type authCache struct {
	mu      sync.RWMutex
	entries map[string]authCacheEntry
}

type authCacheEntry struct {
	wallet  string
	expires time.Time
}

var tokenCache = &authCache{entries: make(map[string]authCacheEntry)}

func (c *authCache) get(token string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[token]
	if !ok || time.Now().After(e.expires) {
		return "", false
	}
	return e.wallet, true
}

func (c *authCache) set(token, wallet string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[token] = authCacheEntry{
		wallet:  wallet,
		expires: time.Now().Add(60 * time.Second),
	}
}

// verifyBearerToken calls the auth server to resolve a bearer token into
// a wallet public key.  Returns ("", nil) if auth is not configured.
// ctx should be the caller's request context so that the outbound request is
// canceled when the incoming request is canceled or times out.
func verifyBearerToken(ctx context.Context, token string) (string, error) {
	authURL := viper.GetString("security.auth_url")
	if authURL == "" {
		return "", nil
	}

	// Check cache first.
	if wallet, ok := tokenCache.get(token); ok {
		return wallet, nil
	}

	url := strings.TrimRight(authURL, "/") + "/api/keys/verify"
	body := fmt.Sprintf(`{"token":%q}`, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("building auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := authHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("auth server unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("invalid or revoked token")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth server returned %d", resp.StatusCode)
	}

	var result authVerifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("invalid auth response: %w", err)
	}

	tokenCache.set(token, result.Wallet)
	return result.Wallet, nil
}

// resolveClientWallet extracts the client's wallet from the Authorization
// header by verifying the bearer token against the auth server.
// If auth is not configured or no token is present, returns "".
func resolveClientWallet(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return ""
	}
	// Expect "Bearer <token>"
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	token := parts[1]

	wallet, err := verifyBearerToken(c.Request.Context(), token)
	if err != nil {
		common.Logger.Warnf("Bearer token verification failed: %v", err)
		return ""
	}
	return wallet
}
