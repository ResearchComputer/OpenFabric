package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"opentela/internal/protocol"
	"testing"

	"github.com/gin-gonic/gin"
	ds "github.com/ipfs/go-datastore"
	"github.com/spf13/viper"
)

func resetNodeTableForTest() {
	peers := protocol.GetAllPeers()
	for key := range *peers {
		protocol.DeleteNodeTableHook(ds.NewKey(key))
	}
}

func addProviderPeer(t *testing.T, peerID string, service protocol.Service) {
	t.Helper()
	peer := protocol.Peer{
		ID:        peerID,
		Connected: true,
		Service:   []protocol.Service{service},
	}
	body, err := json.Marshal(peer)
	if err != nil {
		t.Fatalf("marshal peer: %v", err)
	}
	protocol.UpdateNodeTableHook(ds.NewKey(peerID), body)
}

func TestGlobalServiceForwardHandlerReturnsForbiddenWhenAllCandidatesDenied(t *testing.T) {
	resetAuthClientTestState()
	resetNodeTableForTest()
	viper.Set("security.require_signed_binary", false)
	viper.Set("security.control_plane.cache_ttl", "1m")
	viper.Set("security.control_plane.stale_if_error", "1m")

	addProviderPeer(t, "peer-a", protocol.Service{Name: "llm", IdentityGroup: []string{"model=gpt4"}})
	addProviderPeer(t, "peer-b", protocol.Service{Name: "llm", IdentityGroup: []string{"model=gpt4"}})

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(aclEvaluateResponse{
			KeyID:          "okey_test",
			AllowedPeerIDs: []string{},
			Denied: []aclDeniedPeer{
				{PeerID: "peer-a", Reason: "no_match"},
				{PeerID: "peer-b", Reason: "no_match"},
			},
			PrimaryWallet:   "",
			CacheTTLSeconds: 30,
		})
	}))
	defer mock.Close()

	viper.Set("security.control_plane.url", mock.URL)
	viper.Set("security.control_plane.token", "internal-token")

	r := gin.New()
	r.POST("/v1/service/:service/*path", GlobalServiceForwardHandler)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/v1/service/llm/infer", bytes.NewBufferString(`{"model":"gpt4"}`))
	req.Header.Set("Authorization", "Bearer user-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestP2PForwardHandlerReturnsForbiddenWhenDenied(t *testing.T) {
	resetAuthClientTestState()

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(aclEvaluateResponse{
			KeyID:           "okey_test",
			AllowedPeerIDs:  []string{},
			Denied:          []aclDeniedPeer{{PeerID: "peer-target", Reason: "no_match"}},
			PrimaryWallet:   "",
			CacheTTLSeconds: 30,
		})
	}))
	defer mock.Close()

	viper.Set("security.control_plane.url", mock.URL)
	viper.Set("security.control_plane.token", "internal-token")

	r := gin.New()
	r.GET("/v1/p2p/:peerId/*path", P2PForwardHandler)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v1/p2p/peer-target/health", nil)
	req.Header.Set("Authorization", "Bearer user-token")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}
