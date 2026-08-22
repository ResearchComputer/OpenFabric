// Package account talks to the OpenTela account control plane
// (api.opentela.ai) on behalf of the node operator. Its only job today is
// linking the node's wallet to an OpenTela Cloud account.
//
// One OpenTela account operates a single wallet, and every peer the account
// claims is owned by that wallet. Linking is a two-step challenge handshake:
//
//	POST {api}/manage/wallets/challenges  {"wallet": "<base58 pubkey>"}
//	  -> {id, message, expires_at}
//	signature = base58( ed25519.Sign(walletKey, []byte(message)) )
//	POST {api}/manage/wallets             {"challenge_id": id, "signature": sig}
package account

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mr-tron/base58"
)

// DefaultAPIBaseURL is the production control plane.
const DefaultAPIBaseURL = "https://api.opentela.ai"

// DefaultNeonAuthURL is the production Neon Auth (Better Auth) server whose
// JWTs the control plane accepts. Operators of a staging/dev console point
// the CLI at that console's sign-in URL instead.
const DefaultNeonAuthURL = "https://ep-empty-water-b13qhokv.neonauth.c-5.eu-central-1.aws.neon.tech/neondb/auth"

// LinkedWallet is the server-confirmed result of a successful link.
type LinkedWallet struct {
	ID        int64     `json:"id"`
	Wallet    string    `json:"wallet"`
	Primary   bool      `json:"primary"`
	CreatedAt time.Time `json:"created_at"`
}

// challengeResponse mirrors the API's POST /manage/wallets/challenges reply.
type challengeResponse struct {
	ID        string    `json:"id"`
	Message   string    `json:"message"`
	ExpiresAt time.Time `json:"expires_at"`
}

// APIError is returned for any non-2xx control-plane response. It keeps the
// server's plain-text body so callers can show the API's own explanation
// (e.g. the one-wallet-per-account conflict message) verbatim.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("server returned %d", e.Status)
	}
	return fmt.Sprintf("server returned %d: %s", e.Status, e.Body)
}

// Client is a thin, stateless client for the wallet-link handshake.
type Client struct {
	// BaseURL is the control-plane origin, e.g. https://api.opentela.ai.
	BaseURL string
	// Bearer is the Neon Auth JWT identifying the operator's cloud account.
	Bearer string
	// HTTP is optional; nil uses http.DefaultClient.
	HTTP *http.Client
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) doJSON(ctx context.Context, path string, req, out any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.Bearer != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.Bearer)
	}

	resp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return fmt.Errorf("call %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read %s response: %w", path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &APIError{Status: resp.StatusCode, Body: string(bytes.TrimSpace(raw))}
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode %s response: %w", path, err)
		}
	}
	return nil
}

// LinkWallet runs the full challenges→sign→link handshake for walletPubkey
// using privKey (the matching 64-byte Ed25519 key). ctx should carry a
// timeout: the challenge is single-use and expires five minutes after issue.
//
// Errors:
//   - 401: the JWT is missing/expired — get a fresh sign-in token.
//   - 404: challenge not found (already consumed or unknown ID).
//   - 409: challenge expired/consumed, wallet already linked, or the account
//     already operates a different wallet (one wallet per account).
//   - 422: signature failed verification (wrong key for walletPubkey).
func (c *Client) LinkWallet(
	ctx context.Context, walletPubkey string, privKey ed25519.PrivateKey,
) (*LinkedWallet, error) {
	var challenge challengeResponse
	if err := c.doJSON(ctx, "/manage/wallets/challenges",
		map[string]string{"wallet": walletPubkey}, &challenge); err != nil {
		return nil, err
	}
	if challenge.ID == "" || challenge.Message == "" {
		return nil, fmt.Errorf("challenge response is missing id/message")
	}

	// The API verifies ed25519 over the exact UTF-8 message bytes and expects
	// the 64-byte signature base58-encoded.
	sig := base58.Encode(ed25519.Sign(privKey, []byte(challenge.Message)))

	var linked LinkedWallet
	if err := c.doJSON(ctx, "/manage/wallets",
		map[string]string{"challenge_id": challenge.ID, "signature": sig},
		&linked); err != nil {
		return nil, err
	}
	return &linked, nil
}

// signInRequest/response mirror Better Auth's email-password sign-in. The
// usable JWT travels in the set-auth-jwt response header (the browser client
// copies it into its session); the body offers no guaranteed-JWT field.
func SignInEmail(
	ctx context.Context, httpClient *http.Client, neonAuthURL, email, password string,
) (string, error) {
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		return "", fmt.Errorf("encode sign-in request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, neonAuthURL+"/sign-in/email", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build sign-in request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("call sign-in/email: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read sign-in response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", &APIError{Status: resp.StatusCode, Body: string(bytes.TrimSpace(raw))}
	}

	jwt := resp.Header.Get("set-auth-jwt")
	if jwt == "" {
		return "", fmt.Errorf(
			"sign-in succeeded but the auth server did not issue a JWT " +
				"(no set-auth-jwt header); sign in at the console instead")
	}
	return jwt, nil
}
