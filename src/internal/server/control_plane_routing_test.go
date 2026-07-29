package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"opentela/internal/protocol"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	ds "github.com/ipfs/go-datastore"
	"github.com/spf13/viper"
)

func writeMockNodeCredential(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	switch r.URL.Path {
	case "/internal/node-credentials/challenges":
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(nodeCredentialChallengeResponse{
			ChallengeID: "challenge-1",
			PeerID:      protocol.MyID,
			Region:      "research-eu",
			Role:        "head",
			Audience:    "api.opentela.ai/internal/acl",
			Nonce:       "nonce-1",
			Message:     "sign-this-exact-message",
		})
	case "/internal/node-credentials":
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(nodeCredentialIssueResponse{
			Token:     "trusted-node-jwt",
			ExpiresAt: time.Now().Add(10 * time.Minute),
		})
	default:
		t.Fatalf("unexpected path %q", r.URL.Path)
	}
}

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
		if r.URL.Path != "/internal/acl/evaluate-v2" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(aclEvaluateV2Response{
			KeyID:         "okey_test",
			PrimaryWallet: "",
			DecisionScope: aclDecisionScope{
				Partition: partitionPermissionless,
				Region:    "",
				RouteKind: routeKindServiceIngress,
				Service:   "llm",
			},
			Decisions: []aclDecisionV2{
				{PeerID: "peer-a", Allowed: false, Reason: "service_disabled", PolicyScope: "service", ServiceExposure: "disabled"},
				{PeerID: "peer-b", Allowed: false, Reason: "service_disabled", PolicyScope: "service", ServiceExposure: "disabled"},
			},
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
			Denied:          []aclDeniedPeer{{PeerID: "peer-target", Reason: "service_context_required"}},
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

func TestTrustedRegionP2PServiceForwardHandlerRequiresDirectPath(t *testing.T) {
	resetAuthClientTestState()

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/acl/evaluate-v2":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(aclEvaluateV2Response{
				KeyID:         "okey_test",
				PrimaryWallet: "",
				DecisionScope: aclDecisionScope{
					Partition: partitionTrustedRegion,
					Region:    "research-eu",
					RouteKind: routeKindP2PIngress,
					Service:   "llm-private",
				},
				Decisions: []aclDecisionV2{
					{
						PeerID:          "12D3KooWNoDirect",
						Allowed:         true,
						Reason:          "instance_acl_allow",
						PolicyScope:     "service",
						ServiceExposure: "trusted_region",
					},
				},
			})
		case "/internal/node-credentials/challenges", "/internal/node-credentials":
			writeMockNodeCredential(t, w, r)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer mock.Close()

	viper.Set("security.control_plane.url", mock.URL)
	viper.Set("security.control_plane.token", "internal-token")

	r := gin.New()
	r.GET("/v1/regions/:region/p2p-service/:peerId/:service/*path", TrustedRegionP2PServiceForwardHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/regions/research-eu/p2p-service/12D3KooWNoDirect/llm-private/infer", nil)
	req.Header.Set("Authorization", "Bearer user-token")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}
