package protocol

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multiformats/go-multiaddr"
	"github.com/spf13/viper"
)

const testPeerID = "12D3KooWJ7BrgG4dF1u9wB3XAGKTd7Lw1R6CQqp38zdc6PGBHFcM"

func TestGetDefaultBootstrapPeersStandalone(t *testing.T) {
	viper.Reset()
	res := getDefaultBootstrapPeers(nil, "standalone")
	if len(res) != 0 {
		t.Fatalf("expected 0 peers, got %d", len(res))
	}
}

func TestGetDefaultBootstrapPeersLocal(t *testing.T) {
	viper.Reset()
	res := getDefaultBootstrapPeers(nil, "local")
	if len(res) != 1 {
		t.Fatalf("expected 1 local peer, got %d", len(res))
	}
	if _, err := multiaddr.NewMultiaddr(res[0].String()); err != nil {
		t.Fatalf("invalid multiaddr: %v", err)
	}
}

func TestGetDefaultBootstrapPeersExplicit(t *testing.T) {
	viper.Reset()
	res := getDefaultBootstrapPeers([]string{"/ip4/127.0.0.1/tcp/1234/p2p/" + testPeerID}, "any")
	if len(res) != 1 {
		t.Fatalf("expected 1 explicit peer, got %d", len(res))
	}
}

func TestBootstrapSourcesHTTP(t *testing.T) {
	viper.Reset()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := fmt.Sprintf(`{"bootstraps":["/ip4/127.0.0.1/tcp/4001/p2p/%s"]}`, testPeerID)
		_, _ = w.Write([]byte(payload))
	}))
	defer ts.Close()

	viper.Set("bootstrap.sources", []string{ts.URL})
	res := getDefaultBootstrapPeers(nil, "node")
	if len(res) != 1 {
		t.Fatalf("expected 1 peer from HTTP source, got %d", len(res))
	}
}

func TestBootstrapSourcesFallback(t *testing.T) {
	viper.Reset()
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"bootstraps":`))
	}))
	defer failing.Close()

	viper.Set("bootstrap.sources", []string{failing.URL, "/ip4/10.0.0.1/tcp/4001/p2p/" + testPeerID})
	res := getDefaultBootstrapPeers(nil, "node")
	if len(res) != 1 {
		t.Fatalf("expected 1 fallback peer, got %d", len(res))
	}
}

func TestBootstrapSourcesDNSAddr(t *testing.T) {
	viper.Reset()
	originalLookup := lookupTXT
	lookupTXT = func(name string) ([]string, error) {
		if name == "_dnsaddr.bootstrap.example.com" {
			return []string{"dnsaddr=/ip4/10.0.0.2/tcp/4001/p2p/" + testPeerID}, nil
		}
		return nil, nil
	}
	defer func() { lookupTXT = originalLookup }()

	viper.Set("bootstrap.sources", []string{"dnsaddr://bootstrap.example.com"})
	res := getDefaultBootstrapPeers(nil, "node")
	if len(res) != 1 {
		t.Fatalf("expected 1 peer from dnsaddr, got %d", len(res))
	}
}

func TestBootstrapExplicitAddrOverridesStatic(t *testing.T) {
	viper.Reset()
	staticAddr := "/ip4/10.9.9.9/tcp/4001/p2p/" + testPeerID
	explicitAddr := "/ip4/10.1.1.1/tcp/4001/p2p/" + testPeerID
	viper.Set("bootstrap.static", []string{staticAddr})
	viper.Set("bootstrap.addr", explicitAddr)

	res := getDefaultBootstrapPeers(nil, "node")
	if len(res) != 1 {
		t.Fatalf("expected only the explicit peer, got %d: %v", len(res), res)
	}
	if res[0].String() != explicitAddr {
		t.Fatalf("expected %s, got %s", explicitAddr, res[0].String())
	}
}

func TestBootstrapExplicitSourceOverridesStatic(t *testing.T) {
	viper.Reset()
	staticAddr := "/ip4/10.9.9.9/tcp/4001/p2p/" + testPeerID
	explicitAddr := "/ip4/10.2.2.2/tcp/4001/p2p/" + testPeerID
	viper.Set("bootstrap.static", []string{staticAddr})
	viper.Set("bootstrap.source", []string{explicitAddr})

	res := getDefaultBootstrapPeers(nil, "node")
	if len(res) != 1 {
		t.Fatalf("expected only the explicit source, got %d: %v", len(res), res)
	}
	if res[0].String() != explicitAddr {
		t.Fatalf("expected %s, got %s", explicitAddr, res[0].String())
	}
}

func TestBootstrapStaticUsedWhenNoExplicitSources(t *testing.T) {
	viper.Reset()
	staticAddr := "/ip4/10.9.9.9/tcp/4001/p2p/" + testPeerID
	viper.Set("bootstrap.static", []string{staticAddr})

	res := getDefaultBootstrapPeers(nil, "node")
	if len(res) != 1 {
		t.Fatalf("expected the static peer, got %d: %v", len(res), res)
	}
	if res[0].String() != staticAddr {
		t.Fatalf("expected %s, got %s", staticAddr, res[0].String())
	}
}
