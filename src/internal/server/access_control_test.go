package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestAccessControlPolicyAny(t *testing.T) {
	viper.Set("security.access_control.policy", "any")
	defer viper.Set("security.access_control.policy", "")

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
	viper.Set("security.access_control.policy", "")
	defer viper.Set("security.access_control.policy", "")

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
	viper.Set("security.access_control.policy", "self")
	viper.Set("wallet.account", "MyWalletPubkey123")
	defer func() {
		viper.Set("security.access_control.policy", "")
		viper.Set("wallet.account", "")
	}()

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
	viper.Set("security.access_control.policy", "blacklist")
	viper.Set("security.access_control.blacklist", []string{"BadWallet"})
	defer func() {
		viper.Set("security.access_control.policy", "")
		viper.Set("security.access_control.blacklist", []string{})
	}()

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
	viper.Set("security.access_control.policy", "whitelist")
	viper.Set("security.access_control.whitelist", []string{"AllowedWallet"})
	defer func() {
		viper.Set("security.access_control.policy", "")
		viper.Set("security.access_control.whitelist", []string{})
	}()

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
