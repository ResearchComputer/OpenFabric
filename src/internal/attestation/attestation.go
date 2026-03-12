// Package attestation provides build attestation for verifying that nodes
// in the network are running officially signed binaries.
//
// During the release build, the CI pipeline signs "version|commitHash" with
// a maintainer Ed25519 private key and injects the signature via ldflags.
// Each node advertises its version, commit, and signature in the CRDT peer
// record.  Receiving nodes verify the signature against the embedded public
// key.
//
// Backward compatibility: when security.require_signed_binary is false
// (the default), nodes without a valid attestation are still accepted but
// logged as unverified.  When set to true, unverified peers are rejected
// from the node table.
package attestation

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"sync"
)

// BuildInfo holds the attestation data that is advertised in the peer record.
type BuildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Signature string `json:"build_sig"` // hex-encoded Ed25519 signature
}

// maintainerPubKeyHex is the hex-encoded Ed25519 public key of the release
// maintainer.  Replace this with the real key once generated.
// Generate a keypair with:
//
//	go run ./internal/attestation/cmd/keygen
const maintainerPubKeyHex = "df45c7c4dd4450cd0f296ea6250c60e8a0dad2f459dbf5908e38977e45098d8b"

var (
	pubKey     ed25519.PublicKey
	pubKeyOnce sync.Once
	pubKeyErr  error
)

func loadPubKey() (ed25519.PublicKey, error) {
	pubKeyOnce.Do(func() {
		if maintainerPubKeyHex == "" {
			pubKeyErr = fmt.Errorf("no maintainer public key configured")
			return
		}
		b, err := hex.DecodeString(maintainerPubKeyHex)
		if err != nil {
			pubKeyErr = fmt.Errorf("invalid maintainer public key hex: %w", err)
			return
		}
		if len(b) != ed25519.PublicKeySize {
			pubKeyErr = fmt.Errorf("maintainer public key has wrong length: got %d, want %d", len(b), ed25519.PublicKeySize)
			return
		}
		pubKey = ed25519.PublicKey(b)
	})
	return pubKey, pubKeyErr
}

// attestationMessage returns the canonical message that is signed:
// "version|commit".
func attestationMessage(version, commit string) []byte {
	return []byte(version + "|" + commit)
}

// Verify checks whether the given BuildInfo carries a valid signature from
// the maintainer key.  Returns nil on success, an error describing the
// failure otherwise.
func Verify(info BuildInfo) error {
	if info.Signature == "" {
		return fmt.Errorf("no build signature present")
	}

	pk, err := loadPubKey()
	if err != nil {
		return fmt.Errorf("cannot load maintainer public key: %w", err)
	}

	sig, err := hex.DecodeString(info.Signature)
	if err != nil {
		return fmt.Errorf("invalid signature hex: %w", err)
	}

	msg := attestationMessage(info.Version, info.Commit)
	if !ed25519.Verify(pk, msg, sig) {
		return fmt.Errorf("signature verification failed for version=%s commit=%s", info.Version, info.Commit)
	}
	return nil
}

// Sign produces a hex-encoded Ed25519 signature over "version|commit".
// This is used by the release tooling (not at runtime by nodes).
func Sign(privateKey ed25519.PrivateKey, version, commit string) string {
	msg := attestationMessage(version, commit)
	sig := ed25519.Sign(privateKey, msg)
	return hex.EncodeToString(sig)
}
