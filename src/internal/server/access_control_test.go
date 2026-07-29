package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"opentela/internal/common"
	"opentela/internal/protocol"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestAccessControlPolicyAny(t *testing.T) {
	resetAuthClientTestState()
	viper.Set("security.access_control.policy", "any")

	r := gin.New()
	r.Use(accessControlMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestAccessControlPolicyEmptyDefaultsToAny(t *testing.T) {
	resetAuthClientTestState()
	viper.Set("security.access_control.policy", "")

	r := gin.New()
	r.Use(accessControlMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestAccessControlSelfDeniesUnknownCaller(t *testing.T) {
	resetAuthClientTestState()
	viper.Set("security.access_control.policy", "self")
	viper.Set("wallet.account", "MyWalletPubkey123")

	r := gin.New()
	r.Use(accessControlMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	// RemoteAddr is a regular IP, not a libp2p peer ID → wallet resolves to ""
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unknown caller with policy=self, got %d", w.Code)
	}
}

func TestAccessControlBlacklistAllowsUnlisted(t *testing.T) {
	resetAuthClientTestState()
	viper.Set("security.access_control.policy", "blacklist")
	viper.Set("security.access_control.blacklist", []string{"BadWallet"})

	r := gin.New()
	r.Use(accessControlMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	// Non-libp2p caller → wallet="" → not in blacklist → allowed
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for unlisted caller with policy=blacklist, got %d", w.Code)
	}
}

func TestAccessControlWhitelistDeniesUnlisted(t *testing.T) {
	resetAuthClientTestState()
	viper.Set("security.access_control.policy", "whitelist")
	viper.Set("security.access_control.whitelist", []string{"AllowedWallet"})

	r := gin.New()
	r.Use(accessControlMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	// Non-libp2p caller → wallet="" → not in whitelist → denied
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unlisted caller with policy=whitelist, got %d", w.Code)
	}
}

func TestContainsWallet(t *testing.T) {
	list := []string{"A", "B", "C"}
	if !containsWallet(list, "B") {
		t.Fatal("expected B to be in list")
	}
	if containsWallet(list, "D") {
		t.Fatal("expected D to NOT be in list")
	}
	if containsWallet(nil, "A") {
		t.Fatal("expected nil list to not contain anything")
	}
}

func TestAccessControlDenialLogsDoNotExposeCallerWallet(t *testing.T) {
	resetAuthClientTestState()
	viper.Set("security.access_control.policy", "whitelist")
	viper.Set("security.access_control.whitelist", []string{"AllowedWallet"})

	core, observed := observer.New(zap.WarnLevel)
	originalLogger := common.Logger
	common.Logger = zap.New(core).Sugar()
	defer func() { common.Logger = originalLogger }()

	r := gin.New()
	r.Use(accessControlMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	const callerWallet = "SensitiveCallerWallet"
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "12D3KooWTrustedPeer"
	req.Header.Set("X-Otela-Client-Wallet", callerWallet)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("code=%d, want 403", w.Code)
	}
	for _, entry := range observed.All() {
		if strings.Contains(entry.Message, callerWallet) {
			t.Fatalf("denial log exposed caller wallet: %q", entry.Message)
		}
	}
}

func TestAccessControlCentralAndLocalPolicyComposition(t *testing.T) {
	resetAuthClientTestState()

	oldMyID := protocol.MyID
	protocol.MyID = "worker-self"
	defer func() { protocol.MyID = oldMyID }()

	type testCase struct {
		name          string
		policy        string
		whitelist     []string
		controlStatus int
		controlBody   aclEvaluateResponse
		wantStatus    int
	}

	tests := []testCase{
		{
			name:          "central allow and local allow over libp2p",
			policy:        "whitelist",
			whitelist:     []string{"AllowedWallet"},
			controlStatus: http.StatusOK,
			controlBody: aclEvaluateResponse{
				KeyID:           "okey_test",
				AllowedPeerIDs:  []string{"worker-self"},
				Denied:          []aclDeniedPeer{},
				PrimaryWallet:   "AllowedWallet",
				CacheTTLSeconds: 30,
			},
			wantStatus: http.StatusOK,
		},
		{
			name:          "central deny wins over local allow",
			policy:        "whitelist",
			whitelist:     []string{"AllowedWallet"},
			controlStatus: http.StatusOK,
			controlBody: aclEvaluateResponse{
				KeyID:           "okey_test",
				AllowedPeerIDs:  []string{},
				Denied:          []aclDeniedPeer{{PeerID: "worker-self", Reason: "no_match"}},
				PrimaryWallet:   "AllowedWallet",
				CacheTTLSeconds: 30,
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name:          "local deny wins over central allow",
			policy:        "whitelist",
			whitelist:     []string{"DifferentWallet"},
			controlStatus: http.StatusOK,
			controlBody: aclEvaluateResponse{
				KeyID:           "okey_test",
				AllowedPeerIDs:  []string{"worker-self"},
				Denied:          []aclDeniedPeer{},
				PrimaryWallet:   "AllowedWallet",
				CacheTTLSeconds: 30,
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name:          "invalid key returns unauthorized",
			policy:        "any",
			controlStatus: http.StatusUnauthorized,
			wantStatus:    http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetAuthClientTestState()
			viper.Set("security.access_control.policy", tt.policy)
			viper.Set("security.access_control.whitelist", tt.whitelist)

			mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.controlStatus != http.StatusOK {
					w.WriteHeader(tt.controlStatus)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tt.controlBody)
			}))
			defer mock.Close()

			viper.Set("security.control_plane.url", mock.URL)
			viper.Set("security.control_plane.token", "internal-token")

			r := gin.New()
			r.Use(accessControlMiddleware())
			r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			req.RemoteAddr = "12D3KooWTestPeer"
			req.Header.Set("Authorization", "Bearer user-token")
			req.Header.Set("X-Otela-Client-Wallet", "AllowedWallet")
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("expected %d, got %d", tt.wantStatus, w.Code)
			}
		})
	}
}

func TestAccessControlCentralPrimaryWalletAppliedToDirectHTTPWhitelist(t *testing.T) {
	resetAuthClientTestState()

	oldMyID := protocol.MyID
	protocol.MyID = "worker-self"
	defer func() { protocol.MyID = oldMyID }()

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(aclEvaluateResponse{
			KeyID:           "okey_test",
			AllowedPeerIDs:  []string{"worker-self"},
			Denied:          []aclDeniedPeer{},
			PrimaryWallet:   "AllowedWallet",
			CacheTTLSeconds: 30,
		})
	}))
	defer mock.Close()

	viper.Set("security.control_plane.url", mock.URL)
	viper.Set("security.control_plane.token", "internal-token")
	viper.Set("security.access_control.policy", "whitelist")
	viper.Set("security.access_control.whitelist", []string{"AllowedWallet"})

	r := gin.New()
	r.Use(accessControlMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	req.Header.Set("Authorization", "Bearer user-token")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestAccessControlCentralPrimaryWalletAppliedToDirectHTTPSelfPolicy(t *testing.T) {
	resetAuthClientTestState()

	oldMyID := protocol.MyID
	protocol.MyID = "worker-self"
	defer func() { protocol.MyID = oldMyID }()

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(aclEvaluateResponse{
			KeyID:           "okey_test",
			AllowedPeerIDs:  []string{"worker-self"},
			Denied:          []aclDeniedPeer{},
			PrimaryWallet:   "MyWalletPubkey123",
			CacheTTLSeconds: 30,
		})
	}))
	defer mock.Close()

	viper.Set("security.control_plane.url", mock.URL)
	viper.Set("security.control_plane.token", "internal-token")
	viper.Set("security.access_control.policy", "self")
	viper.Set("wallet.account", "MyWalletPubkey123")

	r := gin.New()
	r.Use(accessControlMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	req.Header.Set("Authorization", "Bearer user-token")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestAccessControlCentralPrimaryWalletOverridesForgedLibp2PHeader(t *testing.T) {
	resetAuthClientTestState()

	oldMyID := protocol.MyID
	protocol.MyID = "worker-self"
	defer func() { protocol.MyID = oldMyID }()

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(aclEvaluateResponse{
			KeyID:           "okey_test",
			AllowedPeerIDs:  []string{"worker-self"},
			Denied:          []aclDeniedPeer{},
			PrimaryWallet:   "EmailAllowedWallet",
			CacheTTLSeconds: 30,
		})
	}))
	defer mock.Close()

	viper.Set("security.control_plane.url", mock.URL)
	viper.Set("security.control_plane.token", "internal-token")
	viper.Set("security.access_control.policy", "whitelist")
	viper.Set("security.access_control.whitelist", []string{"ForgedWalletOnly"})

	r := gin.New()
	r.Use(accessControlMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "12D3KooWForgingPeer"
	req.Header.Set("Authorization", "Bearer user-token")
	req.Header.Set("X-Otela-Client-Wallet", "ForgedWalletOnly")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestAccessControlCentralMissingWalletCannotBeFilledByForgedLibp2PHeader(t *testing.T) {
	resetAuthClientTestState()

	oldMyID := protocol.MyID
	protocol.MyID = "worker-self"
	defer func() { protocol.MyID = oldMyID }()

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(aclEvaluateResponse{
			KeyID:           "okey_test",
			AllowedPeerIDs:  []string{"worker-self"},
			Denied:          []aclDeniedPeer{},
			PrimaryWallet:   "",
			CacheTTLSeconds: 30,
		})
	}))
	defer mock.Close()

	viper.Set("security.control_plane.url", mock.URL)
	viper.Set("security.control_plane.token", "internal-token")
	viper.Set("security.access_control.policy", "whitelist")
	viper.Set("security.access_control.whitelist", []string{"ForgedWalletOnly"})

	r := gin.New()
	r.Use(accessControlMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "12D3KooWForgingPeer"
	req.Header.Set("Authorization", "Bearer user-token")
	req.Header.Set("X-Otela-Client-Wallet", "ForgedWalletOnly")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}
