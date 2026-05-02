package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func newTestEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	probeGroup := r.Group("/v1/probe")
	probeGroup.GET("/echo", echoHandler)
	return r
}

func TestEcho_ReturnsRequestedByteCount(t *testing.T) {
	r := newTestEngine()
	for _, n := range []int64{0, 1, 1024, 1 << 20} {
		req := httptest.NewRequest(http.MethodGet, "/v1/probe/echo?bytes="+strconv.FormatInt(n, 10), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "n=%d", n)
		body, _ := io.ReadAll(w.Body)
		assert.Equal(t, int(n), len(body), "n=%d", n)
		assert.Equal(t, strconv.FormatInt(n, 10), w.Header().Get("Content-Length"), "n=%d", n)
		if n > 0 && n <= 1024 {
			expected := make([]byte, n)
			assert.Equal(t, expected, body, "body should be all-zero, n=%d", n)
		}
	}
}

func TestEcho_RejectsMissingBytes(t *testing.T) {
	r := newTestEngine()
	req := httptest.NewRequest(http.MethodGet, "/v1/probe/echo", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestEcho_RejectsInvalidBytes(t *testing.T) {
	r := newTestEngine()
	req := httptest.NewRequest(http.MethodGet, "/v1/probe/echo?bytes=notanumber", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestEcho_RejectsExcessiveBytes(t *testing.T) {
	r := newTestEngine()
	req := httptest.NewRequest(http.MethodGet, "/v1/probe/echo?bytes=1073741825", nil) // 1 GiB + 1
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestEcho_RejectsNegativeBytes(t *testing.T) {
	r := newTestEngine()
	req := httptest.NewRequest(http.MethodGet, "/v1/probe/echo?bytes=-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
