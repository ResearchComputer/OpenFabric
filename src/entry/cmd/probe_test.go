package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestProbeCommand_PostsAndPrintsResponse(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/probe/run", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"kind":"latency","metrics":{"avg_ns":1234567}}`))
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("port", strings.TrimPrefix(u.Host, "127.0.0.1:"))

	var stdout strings.Builder
	probeCmd.SetOut(&stdout)
	_ = probeCmd.Flags().Set("target", "12D3KooWtarget")
	_ = probeCmd.Flags().Set("kind", "latency")
	_ = probeCmd.Flags().Set("count", "5")
	err := probeCmd.RunE(probeCmd, nil)
	assert.NoError(t, err)
	assert.Contains(t, stdout.String(), `"avg_ns":1234567`)
	assert.Equal(t, "12D3KooWtarget", gotBody["target"])
	assert.Equal(t, "latency", gotBody["kind"])
	assert.Equal(t, float64(5), gotBody["count"])
}

func TestProbeCommand_ReturnsErrorOnNotOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"error":"unreachable"}`))
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("port", strings.TrimPrefix(u.Host, "127.0.0.1:"))

	var stdout strings.Builder
	probeCmd.SetOut(&stdout)
	_ = probeCmd.Flags().Set("target", "12D3KooWtarget")
	err := probeCmd.RunE(probeCmd, nil)
	assert.Error(t, err)
}
