package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"opentela/internal/protocol"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestGetSelf_LocalhostAllowed(t *testing.T) {
	protocol.SetMyselfForTest(protocol.Peer{ID: "QmTestNode", Role: []string{"relay"}})

	gin.SetMode(gin.TestMode)
	r := gin.Default()
	r.GET("/v1/self", getSelf)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/self", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "QmTestNode")
}

func TestGetSelf_RemoteDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.Default()
	r.GET("/v1/self", getSelf)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/self", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	r.ServeHTTP(w, req)
	assert.Equal(t, 403, w.Code)
}

func TestGetSelf_SpoofedXForwardedFor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.Default()
	r.GET("/v1/self", getSelf)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/self", nil)
	req.RemoteAddr = "10.0.0.1:12345"            // actual remote address
	req.Header.Set("X-Forwarded-For", "127.0.0.1") // spoofed
	r.ServeHTTP(w, req)
	assert.Equal(t, 403, w.Code) // must still be denied
}

func TestSignData_RemoteDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.Default()
	r.POST("/v1/sign", signData)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/sign", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	r.ServeHTTP(w, req)
	assert.Equal(t, 403, w.Code)
}
