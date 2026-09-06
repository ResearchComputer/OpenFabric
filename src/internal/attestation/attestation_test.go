package attestation_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/spf13/viper"

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

// newKeyPair returns a fresh Ed25519 pair with the public half hex-encoded,
// matching the form `buildsign keygen` prints and security.build_pubkeys takes.
func newKeyPair(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv, hex.EncodeToString(pub)
}

func TestVerifyAcceptsConfiguredPubKey(t *testing.T) {
	viper.Reset()
	priv, pubHex := newKeyPair(t)
	viper.Set(attestation.PubKeysConfigKey, []string{pubHex})

	info := attestation.BuildInfo{Version: "sai-v0.0.7", Commit: "abc1234"}
	info.Signature = attestation.Sign(priv, info.Version, info.Commit)

	if err := attestation.Verify(info); err != nil {
		t.Fatalf("expected a build signed by a configured key to verify, got %v", err)
	}
}

func TestVerifyRejectsKeyNotConfigured(t *testing.T) {
	viper.Reset()
	priv, _ := newKeyPair(t)
	_, otherHex := newKeyPair(t)
	viper.Set(attestation.PubKeysConfigKey, []string{otherHex})

	info := attestation.BuildInfo{Version: "sai-v0.0.7", Commit: "abc1234"}
	info.Signature = attestation.Sign(priv, info.Version, info.Commit)

	if err := attestation.Verify(info); err == nil {
		t.Fatal("expected verification to fail for a key that is not trusted")
	}
}

func TestVerifyAcceptsAnyOfSeveralPubKeys(t *testing.T) {
	viper.Reset()
	priv, pubHex := newKeyPair(t)
	viper.Set(attestation.PubKeysConfigKey, []string{attestation.DefaultPubKeyHex, pubHex})

	info := attestation.BuildInfo{Version: "sai-v0.0.7", Commit: "abc1234"}
	info.Signature = attestation.Sign(priv, info.Version, info.Commit)

	if err := attestation.Verify(info); err != nil {
		t.Fatalf("expected the second trusted key to be tried, got %v", err)
	}
}

// The value arrives as one delimited string when set through the environment
// (OF_SECURITY_BUILD_PUBKEYS), not as a real list.
func TestVerifyAcceptsDelimitedPubKeyString(t *testing.T) {
	viper.Reset()
	priv, pubHex := newKeyPair(t)
	viper.Set(attestation.PubKeysConfigKey, attestation.DefaultPubKeyHex+","+pubHex)

	info := attestation.BuildInfo{Version: "sai-v0.0.7", Commit: "abc1234"}
	info.Signature = attestation.Sign(priv, info.Version, info.Commit)

	if err := attestation.Verify(info); err != nil {
		t.Fatalf("expected comma-separated keys to be parsed, got %v", err)
	}
}

// One malformed entry must not disable the remaining trusted keys.
func TestVerifyToleratesMalformedPubKeyEntry(t *testing.T) {
	viper.Reset()
	priv, pubHex := newKeyPair(t)
	viper.Set(attestation.PubKeysConfigKey, []string{"not-hex", "aabb", pubHex})

	info := attestation.BuildInfo{Version: "sai-v0.0.7", Commit: "abc1234"}
	info.Signature = attestation.Sign(priv, info.Version, info.Commit)

	if err := attestation.Verify(info); err != nil {
		t.Fatalf("expected a valid key alongside malformed entries to work, got %v", err)
	}
}

func TestVerifyErrorsWhenNoUsablePubKey(t *testing.T) {
	viper.Reset()
	priv, _ := newKeyPair(t)
	viper.Set(attestation.PubKeysConfigKey, []string{"not-hex"})

	info := attestation.BuildInfo{Version: "sai-v0.0.7", Commit: "abc1234"}
	info.Signature = attestation.Sign(priv, info.Version, info.Commit)

	if err := attestation.Verify(info); err == nil {
		t.Fatal("expected an error when no configured key is usable")
	}
}
