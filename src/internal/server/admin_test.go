package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"opentela/internal/protocol"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupAdminRouter(t *testing.T) *gin.Engine {
	t.Helper()
	// Reset registrar in-memory state so each test starts clean.
	protocol.ResetLocalServicesForTest()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/_admin/register", adminRegisterHandler)
	return r
}

func TestAdminRegister_ValidName_PassesValidation(t *testing.T) {
	err := validateAdminRegisterReq(adminRegisterReq{
		Name: "convbench-w-run1-s0-0", Port: 65000,
	})
	assert.NoError(t, err)
}

func TestAdminRegister_RejectsInvalidName(t *testing.T) {
	r := setupAdminRouter(t)
	cases := []string{
		"convbench-",         // empty suffix
		"convbench-bad name", // space
		"convbench-bad/name", // slash
		"otherprefix-foo",    // wrong prefix
		"",
	}
	for _, name := range cases {
		body, _ := json.Marshal(map[string]any{"name": name, "port": 1})
		req, _ := http.NewRequest("POST", "/v1/_admin/register",
			bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code, "name=%q", name)
	}
}

func TestAdminRegister_RejectsBadJSON(t *testing.T) {
	r := setupAdminRouter(t)
	req, _ := http.NewRequest("POST", "/v1/_admin/register",
		bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
