package protocol

import (
	"testing"

	"github.com/multiformats/go-multiaddr"
)

func TestIsTrustedDirectConn(t *testing.T) {
	direct, err := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/4001")
	if err != nil {
		t.Fatalf("direct multiaddr: %v", err)
	}
	relay, err := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/4001/p2p-circuit")
	if err != nil {
		t.Fatalf("relay multiaddr: %v", err)
	}

	if !IsTrustedDirectConn(false, direct) {
		t.Fatal("expected direct non-limited conn to be trusted")
	}
	if IsTrustedDirectConn(true, direct) {
		t.Fatal("expected limited conn to be rejected")
	}
	if IsTrustedDirectConn(false, relay) {
		t.Fatal("expected circuit relay conn to be rejected")
	}
}
