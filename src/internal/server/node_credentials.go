package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"opentela/internal/protocol"
	"strings"
	"sync"
	"time"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/spf13/viper"
)

const nodeCredentialRenewSkew = time.Minute

type nodeCredentialChallengeRequest struct {
	PeerID string `json:"peer_id"`
}

type nodeCredentialChallengeResponse struct {
	ChallengeID string    `json:"challenge_id"`
	PeerID      string    `json:"peer_id"`
	Region      string    `json:"region"`
	Role        string    `json:"role"`
	Audience    string    `json:"audience"`
	Nonce       string    `json:"nonce"`
	IssuedAt    time.Time `json:"issued_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	Message     string    `json:"message"`
}

type nodeCredentialIssueRequest struct {
	ChallengeID string `json:"challenge_id"`
	PeerID      string `json:"peer_id"`
	Nonce       string `json:"nonce"`
	Signature   string `json:"signature"`
	PublicKey   string `json:"public_key"`
}

type nodeCredentialIssueResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type cachedNodeCredential struct {
	Token     string
	ExpiresAt time.Time
}

type nodeCredentialManager struct {
	mu         sync.Mutex
	credential cachedNodeCredential
}

var trustedNodeCredentialManager nodeCredentialManager

func resetNodeCredentialManagerForTest() {
	trustedNodeCredentialManager.mu.Lock()
	defer trustedNodeCredentialManager.mu.Unlock()
	trustedNodeCredentialManager.credential = cachedNodeCredential{}
}

func getTrustedNodeCredential(ctx context.Context) (string, error) {
	return trustedNodeCredentialManager.get(ctx)
}

func (m *nodeCredentialManager) get(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	if m.credential.Token != "" && now.Add(nodeCredentialRenewSkew).Before(m.credential.ExpiresAt) {
		return m.credential.Token, nil
	}

	token, expiresAt, err := issueTrustedNodeCredential(ctx)
	if err != nil {
		return "", err
	}
	m.credential = cachedNodeCredential{
		Token:     token,
		ExpiresAt: expiresAt,
	}
	return token, nil
}

func issueTrustedNodeCredential(ctx context.Context) (string, time.Time, error) {
	baseURL := strings.TrimRight(viper.GetString("security.control_plane.url"), "/")
	if baseURL == "" {
		return "", time.Time{}, fmt.Errorf("missing control plane url")
	}
	peerID, nonce, signature, publicKey, challengeID, err := requestAndSignNodeCredentialChallenge(ctx, baseURL)
	if err != nil {
		return "", time.Time{}, err
	}

	payload := nodeCredentialIssueRequest{
		ChallengeID: challengeID,
		PeerID:      peerID,
		Nonce:       nonce,
		Signature:   signature,
		PublicKey:   publicKey,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", time.Time{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/internal/node-credentials", bytes.NewReader(body))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if controlToken := strings.TrimSpace(viper.GetString("security.control_plane.token")); controlToken != "" {
		req.Header.Set("Authorization", "Bearer "+controlToken)
	}

	resp, err := controlPlaneHTTPClient.Do(req)
	if err != nil {
		return "", time.Time{}, errEvaluatorDown
	}
	defer resp.Body.Close()
	if !nodeCredentialResponseOK(resp.StatusCode) {
		return "", time.Time{}, fmt.Errorf("node credential issuance returned %d", resp.StatusCode)
	}

	var decoded nodeCredentialIssueResponse
	if err := decodeStrictJSON(ioLimit(resp.Body), &decoded); err != nil {
		return "", time.Time{}, err
	}
	if decoded.Token == "" || decoded.ExpiresAt.IsZero() {
		return "", time.Time{}, fmt.Errorf("node credential response incomplete")
	}
	return decoded.Token, decoded.ExpiresAt, nil
}

func requestAndSignNodeCredentialChallenge(ctx context.Context, baseURL string) (peerID, nonce, signatureB64, publicKeyB64, challengeID string, err error) {
	host, _ := protocol.GetP2PNode(nil)
	if host == nil {
		return "", "", "", "", "", fmt.Errorf("node not ready")
	}
	privKey := host.Peerstore().PrivKey(host.ID())
	if privKey == nil {
		return "", "", "", "", "", fmt.Errorf("no libp2p private key available")
	}

	payload := nodeCredentialChallengeRequest{PeerID: host.ID().String()}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", "", "", "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/internal/node-credentials/challenges", bytes.NewReader(body))
	if err != nil {
		return "", "", "", "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if controlToken := strings.TrimSpace(viper.GetString("security.control_plane.token")); controlToken != "" {
		req.Header.Set("Authorization", "Bearer "+controlToken)
	}

	resp, err := controlPlaneHTTPClient.Do(req)
	if err != nil {
		return "", "", "", "", "", errEvaluatorDown
	}
	defer resp.Body.Close()
	if !nodeCredentialResponseOK(resp.StatusCode) {
		return "", "", "", "", "", fmt.Errorf("node credential challenge returned %d", resp.StatusCode)
	}

	var challenge nodeCredentialChallengeResponse
	if err := decodeStrictJSON(ioLimit(resp.Body), &challenge); err != nil {
		return "", "", "", "", "", err
	}
	if challenge.ChallengeID == "" || challenge.Message == "" || challenge.Nonce == "" {
		return "", "", "", "", "", fmt.Errorf("node credential challenge incomplete")
	}

	signature, err := privKey.Sign([]byte(challenge.Message))
	if err != nil {
		return "", "", "", "", "", err
	}
	pubKeyBytes, err := libp2pcrypto.MarshalPublicKey(privKey.GetPublic())
	if err != nil {
		return "", "", "", "", "", err
	}

	return host.ID().String(), challenge.Nonce, base64.StdEncoding.EncodeToString(signature), base64.StdEncoding.EncodeToString(pubKeyBytes), challenge.ChallengeID, nil
}

func ioLimit(r io.Reader) io.Reader {
	return io.LimitReader(r, controlPlaneResponseMaxLen)
}

func nodeCredentialResponseOK(status int) bool {
	return status == http.StatusOK || status == http.StatusCreated
}
