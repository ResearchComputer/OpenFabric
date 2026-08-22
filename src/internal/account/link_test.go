package account

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mr-tron/base58"
)

func newKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

// linkServer builds an httptest server mimicking the two API endpoints.
// challengeStatus makes /manage/wallets/challenges reply with that status;
// linkHandler decides what /manage/wallets does.
func linkServer(t *testing.T, challengeStatus int, linkHandler http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/manage/wallets/challenges", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-jwt" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req["wallet"] == "" {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if challengeStatus != http.StatusOK {
			http.Error(w, http.StatusText(challengeStatus), challengeStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(challengeResponse{
			ID:        "chal-1",
			Message:   "api.opentela.ai wallet-link v1\nsub:u1\nwallet:" + req["wallet"],
			ExpiresAt: time.Now().Add(5 * time.Minute).UTC(),
		})
	})
	mux.HandleFunc("/manage/wallets", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-jwt" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		linkHandler(w, r)
	})
	return httptest.NewServer(mux)
}

func TestLinkWalletHappyPath(t *testing.T) {
	pub, priv := newKeypair(t)
	walletB58 := base58.Encode(pub)

	srv := linkServer(t, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ChallengeID string `json:"challenge_id"`
			Signature   string `json:"signature"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode link request: %v", err)
		}
		if req.ChallengeID != "chal-1" {
			t.Fatalf("challenge_id=%q, want chal-1", req.ChallengeID)
		}
		sigBytes, err := base58.Decode(req.Signature)
		if err != nil {
			t.Fatalf("signature is not base58: %v", err)
		}
		// The server verifies over the message it issued; the message embeds
		// the wallet so we can re-derive it here.
		msg := "api.opentela.ai wallet-link v1\nsub:u1\nwallet:" + walletB58
		if !ed25519.Verify(pub, []byte(msg), sigBytes) {
			t.Fatal("signature does not verify against the issued message")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 9, "wallet": walletB58, "primary": true, "created_at": time.Now().UTC(),
		})
	})
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Bearer: "test-jwt", HTTP: srv.Client()}
	linked, err := client.LinkWallet(context.Background(), walletB58, priv)
	if err != nil {
		t.Fatalf("LinkWallet: %v", err)
	}
	if linked.Wallet != walletB58 || !linked.Primary || linked.ID != 9 {
		t.Fatalf("linked=%+v, want wallet=%s primary=true id=9", linked, walletB58)
	}
}

func TestLinkWalletUnauthorized(t *testing.T) {
	_, priv := newKeypair(t)
	srv := linkServer(t, http.StatusOK, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	// No Bearer set → the fake control plane 401s on the challenge call.
	client := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := client.LinkWallet(context.Background(), base58.Encode(make([]byte, 32)), priv)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("err=%v, want APIError 401", err)
	}
}

func TestLinkWalletPropagatesConflictBody(t *testing.T) {
	_, priv := newKeypair(t)
	srv := linkServer(t, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w,
			"account already has a linked wallet; one account operates a single wallet for all its peers",
			http.StatusConflict)
	})
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Bearer: "test-jwt", HTTP: srv.Client()}
	_, err := client.LinkWallet(context.Background(), base58.Encode(make([]byte, 32)), priv)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err=%v, want APIError", err)
	}
	if apiErr.Status != http.StatusConflict {
		t.Fatalf("status=%d, want 409", apiErr.Status)
	}
	if apiErr.Body != "account already has a linked wallet; one account operates a single wallet for all its peers" {
		t.Fatalf("body=%q, want the server's conflict text verbatim", apiErr.Body)
	}
}

func TestSignInEmailReadsJWTHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sign-in/email" || r.Method != http.MethodPost {
			t.Fatalf("unexpected call: %s %s", r.Method, r.URL.Path)
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req["email"] != "op@example.com" || req["password"] != "hunter2" {
			t.Fatalf("sign-in body=%+v", req)
		}
		w.Header().Set("set-auth-jwt", "header.payload.signature")
		_, _ = w.Write([]byte(`{"user":{"email":"op@example.com"}}`))
	}))
	defer srv.Close()

	jwt, err := SignInEmail(context.Background(), srv.Client(), srv.URL, "op@example.com", "hunter2")
	if err != nil {
		t.Fatalf("SignInEmail: %v", err)
	}
	if jwt != "header.payload.signature" {
		t.Fatalf("jwt=%q, want the set-auth-jwt header value", jwt)
	}
}

func TestSignInEmailSurfacesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"INVALID_EMAIL_OR_PASSWORD","message":"Invalid email or password"}`))
	}))
	defer srv.Close()

	_, err := SignInEmail(context.Background(), srv.Client(), srv.URL, "op@example.com", "wrong")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("err=%v, want APIError 401", err)
	}
	if apiErr.Body == "" {
		t.Fatal("APIError lost the server's error body")
	}
}

func TestSignInEmailMissingJWTHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"user":{"email":"op@example.com"}}`))
	}))
	defer srv.Close()

	_, err := SignInEmail(context.Background(), srv.Client(), srv.URL, "op@example.com", "hunter2")
	if err == nil {
		t.Fatal("expected an error when set-auth-jwt is absent")
	}
}
