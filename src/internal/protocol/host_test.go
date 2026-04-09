package protocol

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/assert"
)

func TestBackoffBaseDelay(t *testing.T) {
	min := 5 * time.Second
	max := 2 * time.Minute

	testCases := []struct {
		name    string
		attempt int
		want    time.Duration
	}{
		{name: "zero attempt defaults to min", attempt: 0, want: min},
		{name: "first attempt returns min", attempt: 1, want: min},
		{name: "second attempt doubles", attempt: 2, want: 2 * min},
		{name: "third attempt doubles again", attempt: 3, want: 4 * min},
		{name: "doubling capped at max", attempt: 6, want: max},
	}

	for _, tc := range testCases {
		got := backoffBaseDelay(tc.attempt, min, max)
		if got != tc.want {
			t.Fatalf("%s: backoffBaseDelay(%d) = %s, want %s", tc.name, tc.attempt, got, tc.want)
		}
	}
}

func TestBackoffDelay_ReturnsWithinExpectedRange(t *testing.T) {
	min := 5 * time.Second
	max := 2 * time.Minute

	for i := 0; i < 100; i++ {
		got := backoffDelay(1, min, max)
		base := backoffBaseDelay(1, min, max)
		jitterMax := base / 3

		assert.GreaterOrEqual(t, got, base, "backoffDelay should be >= base")
		assert.Less(t, got, base+jitterMax, "backoffDelay should be < base + base/3")
	}
}

func TestBackoffDelay_NeverExceedsMax(t *testing.T) {
	min := 5 * time.Second
	max := 2 * time.Minute

	for attempt := 0; attempt < 20; attempt++ {
		for i := 0; i < 50; i++ {
			got := backoffDelay(attempt, min, max)
			// The jitter adds up to base/3 on top of base, and base is capped at max.
			// So the absolute maximum is max + max/3.
			absoluteMax := max + max/3
			assert.LessOrEqual(t, got, absoluteMax,
				"backoffDelay(attempt=%d) = %s should not exceed max + jitter (%s)", attempt, got, absoluteMax)
		}
	}
}

func TestIsTransientNetworkError_NilError(t *testing.T) {
	assert.False(t, isTransientNetworkError(nil))
}

func TestIsTransientNetworkError_DeadlineExceeded(t *testing.T) {
	assert.True(t, isTransientNetworkError(context.DeadlineExceeded))
}

func TestIsTransientNetworkError_Canceled(t *testing.T) {
	assert.True(t, isTransientNetworkError(context.Canceled))
}

func TestIsTransientNetworkError_RegularError(t *testing.T) {
	err := errors.New("some error")
	assert.False(t, isTransientNetworkError(err))
}

func TestWaitFor_ReturnsTrue(t *testing.T) {
	ctx := context.Background()
	result := waitFor(ctx, 10*time.Millisecond)
	assert.True(t, result, "waitFor should return true when context is not cancelled")
}

func TestWaitFor_ReturnsFalseOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	result := waitFor(ctx, 10*time.Second)
	assert.False(t, result, "waitFor should return false when context is cancelled")
}

func TestWaitFor_ZeroDuration(t *testing.T) {
	ctx := context.Background()
	result := waitFor(ctx, 0)
	assert.True(t, result, "waitFor with zero duration should return true immediately")
}

// setNodeTableEntry injects a peer directly into the node table for testing.
func setNodeTableEntry(key string, p Peer) {
	table := getNodeTable()
	tableUpdateSem <- struct{}{}
	defer func() { <-tableUpdateSem }()
	(*table)[key] = p
}

func TestIsRecentRelayPeer_IncludesRecentRelay(t *testing.T) {
	p := Peer{
		Role:     []string{"relay"},
		LastSeen: time.Now().Unix(),
	}
	assert.True(t, isRecentRelayPeer(p), "relay seen just now should be considered recent")
}

func TestIsRecentRelayPeer_ExcludesStaleRelay(t *testing.T) {
	p := Peer{
		Role:     []string{"relay"},
		LastSeen: time.Now().Unix() - 700, // 11+ minutes ago
	}
	assert.False(t, isRecentRelayPeer(p), "relay seen 11+ minutes ago should not be considered recent")
}

func TestIsRecentRelayPeer_NonRelayIgnored(t *testing.T) {
	p := Peer{
		Role:     []string{"worker"},
		LastSeen: time.Now().Unix(),
	}
	assert.False(t, isRecentRelayPeer(p), "non-relay peer should never be considered a recent relay")
}

func TestIsRecentRelayPeer_ExactlyAtBoundary(t *testing.T) {
	// A relay seen exactly maxBootstrapAge seconds ago is NOT recent (boundary exclusive).
	p := Peer{
		Role:     []string{"relay"},
		LastSeen: time.Now().Unix() - maxBootstrapAge,
	}
	assert.False(t, isRecentRelayPeer(p), "relay seen exactly at boundary should not be recent")
}

func TestConnectedBootstraps_IncludesRecentRelay(t *testing.T) {
	// A real peer ID from a test key (Ed25519, deterministic).
	// Using a well-known test peer ID string for the node-table lookup path.
	// We test only the node-table filter; we don't need a live P2P host.
	// Instead we verify via setNodeTableEntry + GetAllPeers that the relay
	// would pass the isRecentRelayPeer filter.
	peerID := "12D3KooWGDMwwqrpcYUs7FEF2WsMj7nG5dkTkrqMNguLGNipFKE3"
	p := Peer{
		ID:            peerID,
		PublicAddress: "1.2.3.4",
		PublicPort:    "9000",
		Role:          []string{"relay"},
		LastSeen:      time.Now().Unix(),
		Connected:     false,
	}
	setNodeTableEntry(peerID, p)
	defer func() {
		table := getNodeTable()
		tableUpdateSem <- struct{}{}
		delete(*table, peerID)
		<-tableUpdateSem
	}()

	// Confirm the relay peer is present and passes the recent-relay check.
	peers := GetAllPeers()
	found := false
	for _, peer := range *peers {
		if peer.ID == peerID {
			assert.True(t, isRecentRelayPeer(peer), "injected relay should be considered recent")
			found = true
			break
		}
	}
	assert.True(t, found, "injected relay peer should be present in node table")
}

func TestConnectedBootstraps_ExcludesStaleRelay(t *testing.T) {
	peerID := "12D3KooWHHzSeKaY8xuZVzkLbKFfCddgzQVjAd8jyS37UDiHiKLV"
	p := Peer{
		ID:            peerID,
		PublicAddress: "5.6.7.8",
		PublicPort:    "9001",
		Role:          []string{"relay"},
		LastSeen:      time.Now().Unix() - 700, // 11+ minutes ago
		Connected:     false,
	}
	setNodeTableEntry(peerID, p)
	defer func() {
		table := getNodeTable()
		tableUpdateSem <- struct{}{}
		delete(*table, peerID)
		<-tableUpdateSem
	}()

	peers := GetAllPeers()
	for _, peer := range *peers {
		if peer.ID == peerID {
			assert.False(t, isRecentRelayPeer(peer), "stale relay should not be considered recent")
			return
		}
	}
	// If not found in table, the test still passes (peer was cleaned up).
}

func TestBuildBootstrapAddr(t *testing.T) {
	tests := []struct {
		name         string
		publicAddr   string
		publicPort   string
		fallbackPort string
		peerID       string
		want         string
	}{
		{
			name:         "uses peer port",
			publicAddr:   "1.2.3.4",
			publicPort:   "18905",
			fallbackPort: "43905",
			peerID:       "QmTest123",
			want:         "/ip4/1.2.3.4/tcp/18905/p2p/QmTest123",
		},
		{
			name:         "falls back when port empty",
			publicAddr:   "5.6.7.8",
			publicPort:   "",
			fallbackPort: "43905",
			peerID:       "QmTest456",
			want:         "/ip4/5.6.7.8/tcp/43905/p2p/QmTest456",
		},
		{
			name:         "supports dns hostnames",
			publicAddr:   "bootstrap.opentela.io",
			publicPort:   "43905",
			fallbackPort: "9999",
			peerID:       "QmDnsTest",
			want:         "/dns/bootstrap.opentela.io/tcp/43905/p2p/QmDnsTest",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildBootstrapAddr(tt.publicAddr, tt.publicPort, tt.fallbackPort, tt.peerID)
			if got != tt.want {
				t.Errorf("BuildBootstrapAddr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildPublicTCPMultiaddr(t *testing.T) {
	tests := []struct {
		name       string
		publicAddr string
		port       string
		want       string
	}{
		{
			name:       "ipv4 address",
			publicAddr: "148.187.108.172",
			port:       "43905",
			want:       "/ip4/148.187.108.172/tcp/43905",
		},
		{
			name:       "hostname address",
			publicAddr: "head-eu-01.opentela.io",
			port:       "43905",
			want:       "/dns/head-eu-01.opentela.io/tcp/43905",
		},
		{
			name:       "strips accidental host port",
			publicAddr: "148.187.108.172:8092",
			port:       "43905",
			want:       "/ip4/148.187.108.172/tcp/43905",
		},
		{
			name:       "ipv6 address",
			publicAddr: "2001:db8::1",
			port:       "43905",
			want:       "/ip6/2001:db8::1/tcp/43905",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildPublicTCPMultiaddr(tt.publicAddr, tt.port)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got.String())
		})
	}
}

func TestAppendUniqueMultiaddrs(t *testing.T) {
	base := []multiaddr.Multiaddr{
		multiaddr.StringCast("/ip4/10.0.0.1/tcp/43905"),
	}
	extra := multiaddr.StringCast("/ip4/148.187.108.172/tcp/43905")

	got := appendUniqueMultiaddrs(base, extra, extra)

	if assert.Len(t, got, 2) {
		assert.Equal(t, "/ip4/10.0.0.1/tcp/43905", got[0].String())
		assert.Equal(t, "/ip4/148.187.108.172/tcp/43905", got[1].String())
	}
}
