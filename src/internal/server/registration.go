package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"opentela/internal/attestation"
	"opentela/internal/common"
	"opentela/internal/protocol"
	"opentela/internal/wallet"

	"github.com/gin-gonic/gin"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	libp2ppeer "github.com/libp2p/go-libp2p/core/peer"
)

const challengeTTL = 60 * time.Second

// ChallengeResponse contains the fields a registering peer must submit to
// prove ownership of its libp2p private key.
type ChallengeResponse struct {
	Nonce     string `json:"nonce"`      // hex-encoded
	Signature string `json:"signature"`  // hex-encoded libp2p key signature
	PeerID    string `json:"peer_id"`
	PublicKey string `json:"public_key"` // base64 marshalled libp2p public key
}

// RegisterRequest is the payload for POST /v1/dnt/register.
type RegisterRequest struct {
	protocol.Peer
	ChallengeResponse ChallengeResponse `json:"challenge_response"`
}

type pendingChallenge struct {
	PeerID  string
	Nonce   []byte
	Expires time.Time
}

// challengeStore maps hex(nonce) → pendingChallenge.
var challengeStore sync.Map

// challengePeer handles GET /v1/dnt/challenge?peer_id=QmXYZ.
// It issues a random 32-byte nonce the caller must sign to register.
func challengePeer(c *gin.Context) {
	peerID := c.Query("peer_id")
	if peerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "peer_id is required"})
		return
	}

	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate nonce"})
		return
	}

	nonceHex := hex.EncodeToString(nonce)
	challengeStore.Store(nonceHex, pendingChallenge{
		PeerID:  peerID,
		Nonce:   nonce,
		Expires: time.Now().Add(challengeTTL),
	})

	c.JSON(http.StatusOK, gin.H{"nonce": nonceHex})
}

// registerPeer handles POST /v1/dnt/register.
// It validates the challenge-response, attestation, and writes the peer to CRDT.
func registerPeer(c *gin.Context) {
	var req RegisterRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 1. ID fields must be non-empty and match.
	if req.ID == "" || req.ChallengeResponse.PeerID == "" || req.ID != req.ChallengeResponse.PeerID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "peer id mismatch or missing"})
		return
	}

	// 2. Look up nonce (do NOT consume yet — consume after signature verification
	// so a failed request from a different party doesn't burn the legitimate nonce).
	val, ok := challengeStore.Load(req.ChallengeResponse.Nonce)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid or expired nonce"})
		return
	}
	challenge := val.(pendingChallenge)
	if time.Now().After(challenge.Expires) {
		challengeStore.Delete(req.ChallengeResponse.Nonce)
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid or expired nonce"})
		return
	}
	if challenge.PeerID != req.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "nonce peer id mismatch"})
		return
	}

	// 3. Public key must be present.
	if req.ChallengeResponse.PublicKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "public_key is required"})
		return
	}

	// 4. Unmarshal public key and verify peer ID derivation.
	pubKeyBytes, err := base64.StdEncoding.DecodeString(req.ChallengeResponse.PublicKey)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid public key encoding"})
		return
	}
	pubKey, err := libp2pcrypto.UnmarshalPublicKey(pubKeyBytes)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid public key"})
		return
	}
	derivedID, err := libp2ppeer.IDFromPublicKey(pubKey)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot derive peer id from public key"})
		return
	}
	if derivedID.String() != req.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "public key does not match peer id"})
		return
	}

	// 5. Verify nonce signature.
	sigBytes, err := hex.DecodeString(req.ChallengeResponse.Signature)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid signature encoding"})
		return
	}
	verified, err := pubKey.Verify(challenge.Nonce, sigBytes)
	if err != nil || !verified {
		c.JSON(http.StatusForbidden, gin.H{"error": "signature verification failed"})
		return
	}

	// Nonce signature verified — consume the nonce now (single-use).
	challengeStore.Delete(req.ChallengeResponse.Nonce)

	// 6. Build attestation must be present and valid.
	if req.BuildAttestation == nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "build attestation is required"})
		return
	}
	if err := attestation.Verify(*req.BuildAttestation); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "build attestation verification failed: " + err.Error()})
		return
	}

	// 7. If identity attestation is present, verify it and enforce owner == wallet_pubkey.
	if req.IdentityAttestation != nil {
		if err := wallet.VerifyIdentity(req.IdentityAttestation); err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "identity attestation verification failed: " + err.Error()})
			return
		}
		if req.Owner != req.IdentityAttestation.WalletPubkey {
			c.JSON(http.StatusForbidden, gin.H{"error": "owner does not match wallet pubkey in identity attestation"})
			return
		}
	}

	// 8. Public address must be a valid IP.
	if net.ParseIP(req.PublicAddress) == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid public_address"})
		return
	}

	// 9. Public port must be a valid TCP port number.
	port, err := strconv.Atoi(req.PublicPort)
	if err != nil || port < 1 || port > 65535 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "public_port must be a valid TCP port (1-65535)"})
		return
	}

	// 10. Role must include "relay".
	hasRelay := false
	for _, r := range req.Role {
		if strings.EqualFold(r, "relay") {
			hasRelay = true
			break
		}
	}
	if !hasRelay {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role must include relay"})
		return
	}

	// 11. Sanitize: clear fields that are unverified or must not come from the caller.
	if req.IdentityAttestation == nil {
		req.Owner = ""
		req.ProviderID = ""
	}
	// Restrict role to exactly ["relay"] regardless of what the caller sent.
	req.Role = []string{"relay"}
	// Clear fields that only the node itself should set.
	req.Service = nil
	req.Load = nil
	req.Hardware = common.HardwareSpec{}

	// 12. Write peer to CRDT.
	if err := protocol.RegisterRemotePeer(req.Peer); err != nil {
		common.Logger.Errorf("Failed to register remote peer %s: %v", req.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register peer"})
		return
	}

	// 13. Success.
	multiaddr := protocol.BuildBootstrapAddr(req.PublicAddress, req.PublicPort, "", req.ID)
	c.JSON(http.StatusOK, gin.H{
		"status":    "registered",
		"multiaddr": multiaddr,
	})
}

// StartChallengeCleanup starts a background goroutine that periodically
// removes expired challenge nonces from the store.
func StartChallengeCleanup() {
	go func() {
		for {
			time.Sleep(challengeTTL)
			cleanExpiredChallenges()
		}
	}()
}

func cleanExpiredChallenges() {
	now := time.Now()
	challengeStore.Range(func(key, value any) bool {
		ch := value.(pendingChallenge)
		if now.After(ch.Expires) {
			challengeStore.Delete(key)
		}
		return true
	})
}
