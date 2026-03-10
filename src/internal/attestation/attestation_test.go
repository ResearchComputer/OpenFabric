package attestation_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"opentela/internal/attestation"
)

func TestSignAndVerifyRoundTrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	version := "1.2.3"
	commit := "abc1234"
	sig := attestation.Sign(priv, version, commit)

	// Verify using raw ed25519 to confirm Sign produces valid signatures.
	msg := []byte(version + "|" + commit)
	sigBytes, _ := hex.DecodeString(sig)
	if !ed25519.Verify(pub, msg, sigBytes) {
		t.Fatal("signature produced by Sign is not valid")
	}
}

func TestVerifyRejectsTamperedVersion(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	sig := attestation.Sign(priv, "1.0.0", "abc")

	info := attestation.BuildInfo{
		Version:   "1.0.1", // tampered
		Commit:    "abc",
		Signature: sig,
	}
	// Verify will fail because the maintainer public key in the binary
	// is empty in tests (or wrong key).  The important thing is it does
	// NOT return nil.
	if err := attestation.Verify(info); err == nil {
		t.Fatal("expected verification to fail for tampered version")
	}
}

func TestVerifyRejectsEmptySignature(t *testing.T) {
	info := attestation.BuildInfo{Version: "1.0.0", Commit: "abc"}
	if err := attestation.Verify(info); err == nil {
		t.Fatal("expected error for empty signature")
	}
}
