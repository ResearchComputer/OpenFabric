package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	ds "github.com/ipfs/go-datastore"
	"github.com/spf13/viper"
)

func TestUpdateNodeTableHook_SelfTrust(t *testing.T) {
	// Save and restore state.
	oldMyID := MyID
	oldVal := viper.GetBool("security.require_signed_binary")
	defer func() {
		MyID = oldMyID
		viper.Set("security.require_signed_binary", oldVal)
	}()

	_ = GetAllPeers()
	viper.Set("security.require_signed_binary", true)

	// An unsigned remote peer should be rejected.
	remote := Peer{ID: "remote-peer", PublicAddress: "1.2.3.4"}
	b, _ := json.Marshal(remote)
	UpdateNodeTableHook(ds.NewKey("remote-peer"), b)
	_, err := GetPeerFromTable("remote-peer")
	if err == nil {
		t.Fatal("expected unsigned remote peer to be rejected")
	}

	// The local node (matching MyID) should always be accepted, even unsigned.
	MyID = "self-node"
	self := Peer{ID: "self-node", PublicAddress: "5.6.7.8"}
	b, _ = json.Marshal(self)
	UpdateNodeTableHook(ds.NewKey("self-node"), b)
	got, err := GetPeerFromTable("self-node")
	if err != nil {
		t.Fatalf("expected self to be accepted even without signed build, got: %v", err)
	}
	if got.PublicAddress != "5.6.7.8" {
		t.Fatalf("unexpected peer: %+v", got)
	}
}

func TestUpdateNodeTableHookAndGetPeer(t *testing.T) {
	_ = GetAllPeers()
	p := Peer{ID: "peer1", PublicAddress: "1.2.3.4"}
	b, _ := json.Marshal(p)
	UpdateNodeTableHook(ds.NewKey("peer1"), b)

	got, err := GetPeerFromTable("peer1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got.PublicAddress != "1.2.3.4" {
		t.Fatalf("unexpected peer: %+v", got)
	}
}

func TestDeleteNodeTableHook(t *testing.T) {
	table := GetAllPeers()
	p := Peer{ID: "peer2", PublicAddress: "5.6.7.8"}
	b, _ := json.Marshal(p)
	UpdateNodeTableHook(ds.NewKey("peer2"), b)
	DeleteNodeTableHook(ds.NewKey("peer2"))
	if _, ok := (*table)["/peer2"]; ok {
		t.Fatalf("expected peer2 deleted")
	}
}
func TestNodeLeave(t *testing.T) {
	// 1. Setup initial state
	p := Peer{ID: "peer-leaving", PublicAddress: "10.0.0.1", Status: CONNECTED, Connected: true}
	b, _ := json.Marshal(p)
	UpdateNodeTableHook(ds.NewKey("peer-leaving"), b)

	// Verify it's in the table
	got, err := GetPeerFromTable("peer-leaving")
	if err != nil {
		t.Fatalf("expected peer to be in table")
	}
	if got.Status != CONNECTED {
		t.Fatalf("expected peer status to be connected, got %s", got.Status)
	}

	// 2. Simulate Leave Update (Status = LEFT)
	p.Status = LEFT
	p.Connected = false
	bLeft, _ := json.Marshal(p)
	UpdateNodeTableHook(ds.NewKey("peer-leaving"), bLeft)

	// 3. Verify it remains in the table with Status=LEFT so TombstoneManager can
	//    find it for deferred cleanup.
	got, err = GetPeerFromTable("peer-leaving")
	if err != nil {
		t.Fatalf("expected LEFT peer to still be in table, got error: %v", err)
	}
	if got.Status != LEFT {
		t.Fatalf("expected peer status LEFT, got %s", got.Status)
	}
	if got.Connected {
		t.Fatal("expected Connected=false for LEFT peer")
	}
}

func TestPublicPortStoredInPeer(t *testing.T) {
	_ = GetAllPeers()

	p1 := Peer{ID: "head-1", PublicAddress: "1.2.3.4", PublicPort: "43905", Connected: true}
	b1, _ := json.Marshal(p1)
	UpdateNodeTableHook(ds.NewKey("head-1"), b1)

	p2 := Peer{ID: "relay-1", PublicAddress: "5.6.7.8", PublicPort: "18905", Connected: true}
	b2, _ := json.Marshal(p2)
	UpdateNodeTableHook(ds.NewKey("relay-1"), b2)

	got1, err := GetPeerFromTable("head-1")
	if err != nil {
		t.Fatalf("expected head-1 in table: %v", err)
	}
	if got1.PublicPort != "43905" {
		t.Fatalf("expected PublicPort=43905, got %s", got1.PublicPort)
	}

	got2, err := GetPeerFromTable("relay-1")
	if err != nil {
		t.Fatalf("expected relay-1 in table: %v", err)
	}
	if got2.PublicPort != "18905" {
		t.Fatalf("expected PublicPort=18905, got %s", got2.PublicPort)
	}
}

func TestRelayRoleAndPortStoredInPeer(t *testing.T) {
	_ = GetAllPeers()

	// Simulate a relay peer arriving via CRDT replication.
	p := Peer{
		ID:            "relay-node",
		PublicAddress: "10.0.0.1",
		PublicPort:    "18905",
		Role:          []string{"relay"},
		Connected:     true,
	}
	b, _ := json.Marshal(p)
	UpdateNodeTableHook(ds.NewKey("relay-node"), b)

	got, err := GetPeerFromTable("relay-node")
	if err != nil {
		t.Fatalf("expected relay-node in table: %v", err)
	}
	if len(got.Role) == 0 || got.Role[0] != "relay" {
		t.Fatalf("expected role=[relay], got %v", got.Role)
	}
	if got.PublicAddress != "10.0.0.1" {
		t.Fatalf("expected PublicAddress=10.0.0.1, got %s", got.PublicAddress)
	}
	if got.PublicPort != "18905" {
		t.Fatalf("expected PublicPort=18905, got %s", got.PublicPort)
	}
}

func TestNodeLeaveAndRejoin(t *testing.T) {
	// 1. Peer joins
	p := Peer{ID: "peer-rejoin", PublicAddress: "10.0.0.2", Status: CONNECTED, Connected: true}
	b, _ := json.Marshal(p)
	UpdateNodeTableHook(ds.NewKey("peer-rejoin"), b)

	got, err := GetPeerFromTable("peer-rejoin")
	if err != nil {
		t.Fatalf("expected peer to be in table after join")
	}
	if got.Status != CONNECTED {
		t.Fatalf("expected CONNECTED, got %s", got.Status)
	}

	// 2. Peer leaves
	p.Status = LEFT
	p.Connected = false
	bLeft, _ := json.Marshal(p)
	UpdateNodeTableHook(ds.NewKey("peer-rejoin"), bLeft)

	got, err = GetPeerFromTable("peer-rejoin")
	if err != nil {
		t.Fatalf("expected LEFT peer to remain in table")
	}
	if got.Status != LEFT {
		t.Fatalf("expected LEFT, got %s", got.Status)
	}

	// 3. Peer rejoins — a non-LEFT update overwrites the LEFT status
	p.Status = CONNECTED
	p.Connected = true
	p.PublicAddress = "10.0.0.3" // new address after rejoin
	bRejoin, _ := json.Marshal(p)
	UpdateNodeTableHook(ds.NewKey("peer-rejoin"), bRejoin)

	got, err = GetPeerFromTable("peer-rejoin")
	if err != nil {
		t.Fatalf("expected peer to be in table after rejoin, got error: %v", err)
	}
	if got.Status != CONNECTED {
		t.Fatalf("expected CONNECTED after rejoin, got %s", got.Status)
	}
	if !got.Connected {
		t.Fatal("expected Connected=true after rejoin")
	}
	if got.PublicAddress != "10.0.0.3" {
		t.Fatalf("expected updated public address 10.0.0.3, got %s", got.PublicAddress)
	}
}

func TestGetSelf(t *testing.T) {
	myself = Peer{ID: "QmTestSelf", Role: []string{"relay"}, PublicAddress: "1.2.3.4"}
	got := GetSelf()
	assert.Equal(t, "QmTestSelf", got.ID)
	assert.Equal(t, "1.2.3.4", got.PublicAddress)
	assert.Equal(t, []string{"relay"}, got.Role)
}

func TestSetMyselfForTest(t *testing.T) {
	SetMyselfForTest(Peer{ID: "QmTestSet"})
	assert.Equal(t, "QmTestSet", GetSelf().ID)
}

func TestRegisterRemotePeer_Signature(t *testing.T) {
	// Verify the function exists with the correct signature by assigning it.
	// Full integration test requires CRDT store which is tested in Task 8.
	fn := RegisterRemotePeer
	_ = fn
}
