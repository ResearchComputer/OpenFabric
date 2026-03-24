package server

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"opentela/internal/protocol"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	libp2ppeer "github.com/libp2p/go-libp2p/core/peer"

	"github.com/gin-gonic/gin"
)

func setupRegistrationRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.Default()
	r.GET("/v1/dnt/challenge", challengePeer)
	r.POST("/v1/dnt/register", registerPeer)
	return r
}

func TestChallenge_MissingPeerID(t *testing.T) {
	router := setupRegistrationRouter()
	req, _ := http.NewRequest("GET", "/v1/dnt/challenge", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestChallenge_ReturnNonce(t *testing.T) {
	router := setupRegistrationRouter()
	req, _ := http.NewRequest("GET", "/v1/dnt/challenge?peer_id=QmTestPeer", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	nonce, ok := resp["nonce"]
	if !ok {
		t.Fatal("response missing nonce field")
	}
	if len(nonce) != 64 { // 32 bytes = 64 hex chars
		t.Fatalf("expected 64-char hex nonce, got %d chars", len(nonce))
	}
}

func TestRegister_InvalidNonce(t *testing.T) {
	router := setupRegistrationRouter()

	body := RegisterRequest{
		Peer: protocol.Peer{ID: "QmFakePeer"},
		ChallengeResponse: ChallengeResponse{
			Nonce:  "deadbeef",
			PeerID: "QmFakePeer",
		},
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "/v1/dnt/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegister_ExpiredNonce(t *testing.T) {
	router := setupRegistrationRouter()

	// Manually store an already-expired nonce.
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	nonceHex := hex.EncodeToString(nonce)
	challengeStore.Store(nonceHex, pendingChallenge{
		PeerID:  "QmExpiredPeer",
		Nonce:   nonce,
		Expires: time.Now().Add(-1 * time.Second),
	})

	body := RegisterRequest{
		Peer: protocol.Peer{ID: "QmExpiredPeer"},
		ChallengeResponse: ChallengeResponse{
			Nonce:  nonceHex,
			PeerID: "QmExpiredPeer",
		},
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "/v1/dnt/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegister_PeerIDMismatch(t *testing.T) {
	router := setupRegistrationRouter()

	body := RegisterRequest{
		Peer: protocol.Peer{ID: "QmPeerA"},
		ChallengeResponse: ChallengeResponse{
			Nonce:  "aabbccdd",
			PeerID: "QmPeerB",
		},
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "/v1/dnt/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegister_WrongSignature(t *testing.T) {
	router := setupRegistrationRouter()

	// Generate RSA-2048 key pair.
	privKey, pubKey, err := libp2pcrypto.GenerateKeyPairWithReader(libp2pcrypto.RSA, 2048, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	peerID, _ := libp2ppeer.IDFromPublicKey(pubKey)

	// Get a valid challenge nonce.
	challengeReq, _ := http.NewRequest("GET", "/v1/dnt/challenge?peer_id="+peerID.String(), nil)
	cw := httptest.NewRecorder()
	router.ServeHTTP(cw, challengeReq)
	var challengeResp map[string]string
	if err := json.Unmarshal(cw.Body.Bytes(), &challengeResp); err != nil {
		t.Fatal(err)
	}
	nonceHex := challengeResp["nonce"]

	// Sign WRONG data (not the nonce).
	wrongData := []byte("this is not the nonce")
	sig, _ := privKey.Sign(wrongData)
	sigHex := hex.EncodeToString(sig)

	// Marshal public key.
	pubKeyBytes, _ := libp2pcrypto.MarshalPublicKey(pubKey)
	pubKeyB64 := base64.StdEncoding.EncodeToString(pubKeyBytes)

	body := RegisterRequest{
		Peer: protocol.Peer{ID: peerID.String()},
		ChallengeResponse: ChallengeResponse{
			Nonce:     nonceHex,
			Signature: sigHex,
			PeerID:    peerID.String(),
			PublicKey: pubKeyB64,
		},
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "/v1/dnt/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegister_NonceReuse(t *testing.T) {
	router := setupRegistrationRouter()

	privKey, pubKey, _ := libp2pcrypto.GenerateKeyPairWithReader(libp2pcrypto.RSA, 2048, rand.Reader)
	peerID, _ := libp2ppeer.IDFromPublicKey(pubKey)

	// Get a challenge nonce.
	challengeReq, _ := http.NewRequest("GET", "/v1/dnt/challenge?peer_id="+peerID.String(), nil)
	cw := httptest.NewRecorder()
	router.ServeHTTP(cw, challengeReq)
	var challengeResp map[string]string
	if err := json.Unmarshal(cw.Body.Bytes(), &challengeResp); err != nil {
		t.Fatal(err)
	}
	nonceHex := challengeResp["nonce"]

	// Sign the actual nonce.
	nonceBytes, _ := hex.DecodeString(nonceHex)
	sig, _ := privKey.Sign(nonceBytes)
	sigHex := hex.EncodeToString(sig)

	pubKeyBytes, _ := libp2pcrypto.MarshalPublicKey(pubKey)
	pubKeyB64 := base64.StdEncoding.EncodeToString(pubKeyBytes)

	body := RegisterRequest{
		Peer: protocol.Peer{
			ID:            peerID.String(),
			PublicAddress: "1.2.3.4",
			PublicPort:    "4001",
			Role:          []string{"relay"},
		},
		ChallengeResponse: ChallengeResponse{
			Nonce:     nonceHex,
			Signature: sigHex,
			PeerID:    peerID.String(),
			PublicKey: pubKeyB64,
		},
	}
	b, _ := json.Marshal(body)

	// First attempt: will fail at attestation check (no build attestation).
	// The nonce is consumed after signature verification succeeds.
	req1, _ := http.NewRequest("POST", "/v1/dnt/register", bytes.NewReader(b))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	// Should fail at build attestation step (403).
	if w1.Code != http.StatusForbidden {
		t.Fatalf("first attempt: expected 403, got %d: %s", w1.Code, w1.Body.String())
	}

	// Second attempt with same nonce: must fail with "invalid or expired nonce".
	req2, _ := http.NewRequest("POST", "/v1/dnt/register", bytes.NewReader(b))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("second attempt: expected 403, got %d: %s", w2.Code, w2.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != "invalid or expired nonce" {
		t.Fatalf("expected 'invalid or expired nonce', got '%s'", resp["error"])
	}
}

func TestRegister_MissingBuildAttestation(t *testing.T) {
	router := setupRegistrationRouter()

	privKey, pubKey, _ := libp2pcrypto.GenerateKeyPairWithReader(libp2pcrypto.RSA, 2048, rand.Reader)
	peerID, _ := libp2ppeer.IDFromPublicKey(pubKey)

	// Get challenge.
	challengeReq, _ := http.NewRequest("GET", "/v1/dnt/challenge?peer_id="+peerID.String(), nil)
	cw := httptest.NewRecorder()
	router.ServeHTTP(cw, challengeReq)
	var challengeResp map[string]string
	if err := json.Unmarshal(cw.Body.Bytes(), &challengeResp); err != nil {
		t.Fatal(err)
	}
	nonceHex := challengeResp["nonce"]

	// Sign the nonce correctly.
	nonceBytes, _ := hex.DecodeString(nonceHex)
	sig, _ := privKey.Sign(nonceBytes)
	sigHex := hex.EncodeToString(sig)

	pubKeyBytes, _ := libp2pcrypto.MarshalPublicKey(pubKey)
	pubKeyB64 := base64.StdEncoding.EncodeToString(pubKeyBytes)

	body := RegisterRequest{
		Peer: protocol.Peer{
			ID:            peerID.String(),
			PublicAddress: "1.2.3.4",
			PublicPort:    "4001",
			Role:          []string{"relay"},
		},
		ChallengeResponse: ChallengeResponse{
			Nonce:     nonceHex,
			Signature: sigHex,
			PeerID:    peerID.String(),
			PublicKey: pubKeyB64,
		},
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "/v1/dnt/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != "build attestation is required" {
		t.Fatalf("expected 'build attestation is required', got '%s'", resp["error"])
	}
}
