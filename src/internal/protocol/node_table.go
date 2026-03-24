package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"opentela/internal/attestation"
	"opentela/internal/common"
	"opentela/internal/platform"
	"opentela/internal/protocol/nodetable"
	"opentela/internal/protocol/swim"
	"opentela/internal/wallet"
	"sync"
	"time"

	ds "github.com/ipfs/go-datastore"
	"github.com/spf13/viper"
)

var dntOnce sync.Once
var myself Peer
var myselfMu sync.RWMutex

var (
	scalableNodeTable *nodetable.NodeTable
	nodeTableWriter   *nodetable.Writer
	swimOnce          sync.Once
	swimInstance      *swim.SWIM
)

// InitScalableNodeTable sets up the new COW node table and SWIM.
// Called from server.go when scalability.swim_enabled=true.
func InitScalableNodeTable() {
	swimOnce.Do(func() {
		scalableNodeTable = nodetable.NewNodeTable()
		nodeTableWriter = nodetable.NewWriter(scalableNodeTable)
		nodeTableWriter.Start()
	})
}

func GetScalableSnapshot() *nodetable.NodeTableSnapshot {
	if scalableNodeTable == nil {
		return nil
	}
	return scalableNodeTable.Snapshot()
}

func GetNodeTableWriter() *nodetable.Writer {
	return nodeTableWriter
}

// StartSWIM initializes and runs the SWIM membership protocol.
// Must be called after InitScalableNodeTable() and GetP2PNode().
func StartSWIM(ctx context.Context) {
	host, _ := GetP2PNode(nil)
	eventCh := make(chan swim.MemberEvent, 1024)

	cfg := swim.Config{
		ProbeInterval:        viper.GetDuration("swim.probe_interval"),
		ProbeTimeout:         viper.GetDuration("swim.probe_timeout"),
		IndirectProbeTimeout: viper.GetDuration("swim.indirect_probe_timeout"),
		IndirectProbes:       viper.GetInt("swim.indirect_probes"),
		SuspectTimeout:       viper.GetDuration("swim.suspect_timeout"),
		RetransmitMult:       viper.GetInt("swim.retransmit_mult"),
	}

	transport := swim.NewLibP2PTransport(host, cfg.ProbeTimeout)
	swimInstance = swim.NewSWIM(host.ID(), cfg, transport, eventCh)
	swim.RegisterHandler(host, swimInstance)

	// Seed SWIM from existing libp2p connections
	for _, conn := range host.Network().Conns() {
		swimInstance.AddMember(conn.RemotePeer())
	}

	// Forward SWIM events to node table writer
	go func() {
		for ev := range eventCh {
			var eventType nodetable.EventType
			switch ev.Status {
			case swim.StatusJoin:
				eventType = nodetable.EventSWIMJoin
			case swim.StatusAlive:
				eventType = nodetable.EventSWIMAlive
			case swim.StatusSuspect:
				eventType = nodetable.EventSWIMSuspect
			case swim.StatusDead:
				eventType = nodetable.EventSWIMDead
			}

			ne := nodetable.NodeEvent{
				Type:      eventType,
				PeerID:    ev.Peer,
				Timestamp: time.Now().Unix(),
			}

			// Parse metadata if present
			if len(ev.Meta) > 0 {
				var meta swim.Metadata
				if err := meta.Unmarshal(ev.Meta); err == nil {
					pd := &nodetable.PeerData{
						IdentityGroups: meta.IdentityGroups,
						ActiveRequests: meta.ActiveRequests,
						RegionHint:     meta.RegionHint,
					}
					// Convert RoleType to []string
					switch meta.Role {
					case swim.RoleWorker:
						pd.Role = []string{"worker"}
					case swim.RoleHead:
						pd.Role = []string{"head"}
					case swim.RoleRelay:
						pd.Role = []string{"relay"}
					}
					ne.PeerData = pd
				}
			}

			nodeTableWriter.Send(ne)
		}
	}()

	// Run SWIM protocol
	go swimInstance.Run(ctx)
	common.Logger.Info("SWIM membership protocol started")
}

const (
	CONNECTED    string = "connected"
	DISCONNECTED string = "disconnected"
	LEFT         string = "left"
)

type Service struct {
	Name     string              `json:"name"`
	Hardware common.HardwareSpec `json:"hardware"`
	Status   string              `json:"status"`
	Host     string              `json:"host"`
	Port     string              `json:"port"`
	// IdentityGroup is a list of identities that can access this service
	// Format: <identity_group_name>=<identity_name>
	// e.g., "model=resnet50"
	IdentityGroup []string `json:"identity_group"`
}

// Trust levels for nodes in the network.
const (
	TrustUntrusted    = 0 // no attestation or invalid
	TrustSelfAttested = 1 // wallet signature verified
	TrustUserTrusted  = 2 // explicitly trusted by the requesting user
	TrustKYCVerified  = 3 // passed KYC (future)
)

// Peer is a single node in the network, as can be seen by the current node.
type Peer struct {
	ID                string              `json:"id"`
	Latency           int                 `json:"latency"` // in ms
	Privileged        bool                `json:"privileged"`
	// Owner is the wallet public key (base58) of the node operator.
	// This field is always a raw wallet pubkey and is used for
	// trust/access-control decisions.
	Owner             string              `json:"owner"`
	// ProviderID is the deterministic human-readable identifier derived
	// from the wallet pubkey (e.g. "otela-AbCdEfGh...").  It is stored
	// separately so that Owner always carries the raw pubkey and callers
	// never confuse the two.
	ProviderID        string              `json:"provider_id,omitempty"`
	CurrentOffering   []string            `json:"current_offering"`
	Role              []string            `json:"role"`
	Status            string              `json:"status"`
	AvailableOffering []string            `json:"available_offering"`
	Service           []Service           `json:"service"`
	LastSeen          int64               `json:"last_seen"`
	Version           string              `json:"version"`
	PublicAddress     string              `json:"public_address"`
	PublicPort        string              `json:"public_port,omitempty"`
	Hardware          common.HardwareSpec `json:"hardware"`
	// RelayPeer is the peer ID of the relay this node has an active
	// reservation on. Set by workers behind firewalls so head nodes
	// know which relay to route through.
	RelayPeer         string              `json:"relay_peer,omitempty"`
	Connected         bool                `json:"connected"`
	Load              []int               `json:"load"`
	// BuildAttestation carries the version + commit + signature so peers
	// can verify that this node is running an officially signed binary.
	// Absent (nil) for nodes running older versions without attestation.
	BuildAttestation *attestation.BuildInfo `json:"build_attestation,omitempty"`
	// SignedBuild is set locally (not serialised from remote) after
	// verifying BuildAttestation.  true = signature valid.
	SignedBuild bool `json:"-"`
	// IdentityAttestation proves this node's operator controls the
	// wallet key listed in Owner.  Nil for old nodes without attestation.
	IdentityAttestation *wallet.IdentityAttestation `json:"identity_attestation,omitempty"`
	// TrustLevel is computed locally after verifying attestations.
	// 0=untrusted, 1=self-attested, 2=user-trusted, 3=KYC-verified.
	TrustLevel int `json:"-"`
}

type PeerWithStatus struct {
	ID            string `json:"id"`
	Connectedness string `json:"connectedness"` // "connected" or "disconnected"
}

// Node table tracks the nodes and their status in the network.
type NodeTable map[string]Peer

var dnt *NodeTable
var tableUpdateSem = make(chan struct{}, 1) // capacity 1 → max 1 goroutine at a time

func getNodeTable() *NodeTable {
	dntOnce.Do(func() {
		dnt = &NodeTable{}
	})
	return dnt
}

func UpdateNodeTable(peer Peer) {
	ctx := context.Background()
	host, _ := GetP2PNode(nil)
	// broadcast the peer to the network
	store, _ := GetCRDTStore()
	key := ds.NewKey(host.ID().String())
	peer.ID = host.ID().String()
	// merge services instead of overwriting
	// first find the peer in the table if it exists
	existingPeer, err := GetPeerFromTable(peer.ID)
	if err == nil {
		peer.Service = append(peer.Service, existingPeer.Service...)
		// Preserve existing wallet pubkey and provider ID if not set in the update.
		if peer.Owner == "" && existingPeer.Owner != "" {
			peer.Owner = existingPeer.Owner
		}
		if peer.ProviderID == "" && existingPeer.ProviderID != "" {
			peer.ProviderID = existingPeer.ProviderID
		}
	}
	// Track services in localServices so ReannounceLocalServices preserves them.
	for _, svc := range peer.Service {
		addLocalService(svc)
	}
	if viper.GetString("public-addr") != "" {
		peer.PublicAddress = viper.GetString("public-addr")
	}
	peer.PublicPort = viper.GetString("tcpport")
	value, err := json.Marshal(peer)
	common.ReportError(err, "Error while marshalling peer")
	if err := store.Put(ctx, key, value); err != nil {
		common.Logger.Error("Error while updating node table: ", err)
	}
}

func MarkSelfAsBootstrap() {
	if viper.GetString("public-addr") != "" || viper.GetString("role") == "relay" {
		common.Logger.Debug("Registering as bootstrap node")
		ctx := context.Background()
		store, _ := GetCRDTStore()
		host, _ := GetP2PNode(nil)
		key := ds.NewKey(host.ID().String())
		// Ensure the global `myself` has at least a stable ID before marshalling.
		myselfMu.Lock()
		if myself.ID == "" {
			myself.ID = host.ID().String()
		}
		// Use the global myself which carries build attestation.
		myself.PublicAddress = viper.GetString("public-addr")
		myself.PublicPort = viper.GetString("tcpport")
		myself.Connected = true
		value, err := json.Marshal(myself)
		myselfMu.Unlock()
		UpdateNodeTableHook(key, value)
		common.ReportError(err, "Error while marshalling peer")
		if err := store.Put(ctx, key, value); err != nil {
			common.Logger.Error("Error while registering bootstrap: ", err)
		}
	}
}

func AnnounceLeave() {
	ctx := context.Background()
	host, _ := GetP2PNode(nil)
	// broadcast the peer to the network
	store, _ := GetCRDTStore()
	key := ds.NewKey(host.ID().String())
	common.Logger.Info("Leaving network")

	// Update self status to LEFT
	myselfMu.Lock()
	myself.Status = LEFT
	myself.Connected = false
	myself.LastSeen = time.Now().Unix()
	value, err := json.Marshal(myself)
	myselfMu.Unlock()
	if err != nil {
		common.Logger.Error("Error while marshalling peer for leave: ", err)
		return
	}

	if err := store.Put(ctx, key, value); err != nil {
		common.Logger.Error("Error while announcing leave: ", err)
	}
}

func UpdateNodeTableHook(key ds.Key, value []byte) {
	table := *getNodeTable()
	var peer Peer
	err := json.Unmarshal(value, &peer)
	common.ReportError(err, "Error while unmarshalling peer")

	// Verify build attestation from the remote peer.
	peer.SignedBuild = false
	if peer.BuildAttestation != nil {
		if err := attestation.Verify(*peer.BuildAttestation); err == nil {
			peer.SignedBuild = true
		} else {
			common.Logger.Debugf("Peer [%s] build attestation invalid: %v", peer.ID, err)
		}
	}

	// If enforcement is on, reject peers without a valid signed build.
	// Always trust ourselves — a node should never reject its own entry.
	if viper.GetBool("security.require_signed_binary") && !peer.SignedBuild && peer.ID != MyID {
		common.Logger.Warnf("Rejecting peer [%s]: no valid build attestation (security.require_signed_binary=true)", peer.ID)
		return
	}

	// Compute trust level from identity attestation.
	peer.TrustLevel = TrustUntrusted
	if peer.IdentityAttestation != nil {
		if err := wallet.VerifyIdentity(peer.IdentityAttestation); err == nil {
			peer.TrustLevel = TrustSelfAttested
			// Check if this peer's wallet is in our trusted wallets list.
			for _, tw := range viper.GetStringSlice("trusted_wallets") {
				if tw == peer.IdentityAttestation.WalletPubkey {
					peer.TrustLevel = TrustUserTrusted
					break
				}
			}
			// Self-trust: if our own wallet matches the peer's wallet.
			if peer.IdentityAttestation.WalletPubkey == viper.GetString("wallet.account") {
				if peer.TrustLevel < TrustUserTrusted {
					peer.TrustLevel = TrustUserTrusted
				}
			}
		} else {
			common.Logger.Warnf("Peer [%s] identity attestation invalid: %v", peer.ID, err)
		}
	}

	// Check for Left status — keep the peer in the table marked as LEFT
	// so TombstoneManager.collectCandidates can find it for deferred cleanup.
	if peer.Status == LEFT {
		common.Logger.Debugf("Peer [%s] has left, marking as LEFT in table", peer.ID)
		tableUpdateSem <- struct{}{}
		defer func() { <-tableUpdateSem }() // Release on exit
		peer.Connected = false
		if peer.LastSeen == 0 {
			peer.LastSeen = time.Now().Unix()
		}
		table[key.String()] = peer
		return
	}

	// A non-LEFT update: if this peer was previously LEFT, it has rejoined.
	tableUpdateSem <- struct{}{}
	defer func() { <-tableUpdateSem }() // Release on exit
	if existing, ok := table[key.String()]; ok {
		if existing.Status == LEFT {
			common.Logger.Debugf("Peer [%s] rejoined (was LEFT)", peer.ID)
		}
		// If LastSeen is missing in the update, keep the existing one
		if peer.LastSeen == 0 {
			peer.LastSeen = existing.LastSeen
		}
	}
	// Always update LastSeen on any CRDT update we receive for that peer
	peer.LastSeen = time.Now().Unix()
	table[key.String()] = peer
}

func DeleteNodeTableHook(key ds.Key) {
	table := *getNodeTable()
	tableUpdateSem <- struct{}{}
	defer func() { <-tableUpdateSem }() // Release on exit
	delete(table, key.String())
}

func GetPeerFromTable(peerId string) (Peer, error) {
	table := *getNodeTable()
	tableUpdateSem <- struct{}{}
	defer func() { <-tableUpdateSem }() // Release on exit
	peer, ok := table["/"+peerId]
	if !ok {
		return Peer{}, errors.New("peer not found")
	}
	return peer, nil
}

func GetConnectedPeers() *NodeTable {
	var connected = NodeTable{}
	tableUpdateSem <- struct{}{}
	defer func() { <-tableUpdateSem }() // Release on exit
	for id, p := range *getNodeTable() {
		if p.Connected {
			connected[id] = p
		}
	}
	return &connected
}

func GetAllPeers() *NodeTable {
	var peers = NodeTable{}
	tableUpdateSem <- struct{}{}
	defer func() { <-tableUpdateSem }() // Release on exit
	for id, p := range *getNodeTable() {
		peers[id] = p
	}
	return &peers
}

func GetService(name string) (Service, error) {
	host, _ := GetP2PNode(nil)
	store, _ := GetCRDTStore()
	key := ds.NewKey(host.ID().String())
	value, err := store.Get(context.Background(), key)
	common.ReportError(err, "Error while getting peer")
	var peer Peer
	err = json.Unmarshal(value, &peer)
	common.ReportError(err, "Error while unmarshalling peer")
	for _, service := range peer.Service {
		if service.Name == name {
			return service, nil
		}
	}
	return Service{}, errors.New("Service not found")
}

func GetAllProviders(serviceName string) ([]Peer, error) {
	var providers []Peer
	table := *getNodeTable()
	tableUpdateSem <- struct{}{}
	defer func() { <-tableUpdateSem }() // Release on exit
	for _, peer := range table {
		// Include all peers with matching services, not just directly
		// connected ones. Workers behind relays appear as disconnected
		// but are reachable via relay-hop routing.
		for _, service := range peer.Service {
			if service.Name == serviceName {
				providers = append(providers, peer)
				break
			}
		}
	}
	if len(providers) == 0 {
		return providers, errors.New("no providers found")
	}
	return providers, nil
}

// InitializeMyself registers this node in the CRDT.
// walletPubkeyOverride is the raw wallet public key (base58) for this node
// (may be empty).  It is stored in Owner so that all trust/access-control
// comparisons work with like-for-like values.
// wm is the wallet manager for signing identity attestations and deriving the
// ProviderID (may be nil if no wallet is configured).
func InitializeMyself(walletPubkeyOverride string, wm *wallet.WalletManager) {
	host, _ := GetP2PNode(nil)
	ctx := context.Background()
	store, _ := GetCRDTStore()
	key := ds.NewKey(host.ID().String())
	myselfMu.Lock()
	myself = Peer{
		ID:            host.ID().String(),
		PublicAddress: viper.GetString("public-addr"),
		LastSeen:      time.Now().Unix(),
		Connected:     true,
	}

	// Attach build attestation so peers can verify we run a signed binary.
	if common.JSONVersion.Version != "" && common.JSONVersion.Commit != "" {
		myself.BuildAttestation = &attestation.BuildInfo{
			Version:   common.JSONVersion.Version,
			Commit:    common.JSONVersion.Commit,
			Signature: common.JSONVersion.BuildSig,
		}
		if err := attestation.Verify(*myself.BuildAttestation); err == nil {
			myself.SignedBuild = true
			common.Logger.Info("Build attestation verified")
		} else {
			common.Logger.Debugf("Build attestation not verified: %v", err)
		}
	}

	// Owner always holds the raw wallet public key so access-control
	// comparisons are like-for-like (wallet pubkey vs wallet pubkey).
	if walletPubkeyOverride != "" {
		myself.Owner = walletPubkeyOverride
		common.Logger.Debugf("Wallet provider: %s (verified)", myself.Owner)
	} else if account := viper.GetString("wallet.account"); account != "" {
		myself.Owner = account
		common.Logger.Debugf("Wallet provider: %s (configured)", myself.Owner)
	} else if wm != nil && wm.WalletExists() {
		myself.Owner = wm.GetPublicKey()
		if myself.Owner != "" {
			common.Logger.Debugf("Wallet provider: %s (local)", myself.Owner)
		}
	}

	// Store the human-readable provider ID separately so it never
	// overwrites the wallet pubkey in Owner.
	if wm != nil && wm.WalletExists() {
		if pid := wm.GetProviderID(); pid != "" {
			myself.ProviderID = pid
			common.Logger.Debugf("Provider ID: %s", myself.ProviderID)
		}
	}

	// Sign identity attestation binding our peer ID to our wallet key.
	if wm != nil && wm.WalletExists() {
		att, err := wallet.SignIdentity(myself.ID, time.Now().Unix(), wm)
		if err != nil {
			common.Logger.Warnf("Could not sign identity attestation: %v", err)
		} else {
			myself.IdentityAttestation = att
			myself.TrustLevel = TrustSelfAttested
			common.Logger.Info("Identity attestation signed (self-attested)")
		}
	}

	myself.Hardware.GPUs = platform.GetGPUInfo()

	// Set role from config.
	if role := viper.GetString("role"); role != "" {
		myself.Role = []string{role}
	}

	// All nodes carry their own port so ConnectedBootstraps builds correct multiaddrs.
	myself.PublicPort = viper.GetString("tcpport")

	// Warn if relay has no public address — it won't be discoverable as a bootstrap.
	if viper.GetString("role") == "relay" && myself.PublicAddress == "" {
		common.Logger.Warn("Relay node has no public-addr set; it won't be discoverable as a bootstrap")
	}

	value, err := json.Marshal(myself)
	myselfMu.Unlock()
	common.ReportError(err, "Error while marshalling peer")
	err = store.Put(ctx, key, value)
	if err != nil {
		common.Logger.Error("Error while initializing myself in the node table: ", err)
	}
}

// GetSelf returns a copy of this node's own Peer record.
func GetSelf() Peer {
	myselfMu.RLock()
	defer myselfMu.RUnlock()
	return myself
}

// SetMyselfRelayPeer atomically updates the RelayPeer field on myself.
func SetMyselfRelayPeer(relayPeer string) {
	myselfMu.Lock()
	defer myselfMu.Unlock()
	myself.RelayPeer = relayPeer
}

// SetMyselfForTest sets the myself var for testing. Test-only.
func SetMyselfForTest(p Peer) {
	myselfMu.Lock()
	defer myselfMu.Unlock()
	myself = p
}

// RegisterRemotePeer writes a remote peer's entry to the CRDT store.
// Used by the HTTP registration endpoint to insert relay addresses.
func RegisterRemotePeer(p Peer) error {
	ctx := context.Background()
	store, _ := GetCRDTStore()
	key := ds.NewKey(p.ID)
	p.Connected = false
	p.LastSeen = time.Now().Unix()
	value, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal remote peer: %w", err)
	}
	UpdateNodeTableHook(key, value)
	return store.Put(ctx, key, value)
}
