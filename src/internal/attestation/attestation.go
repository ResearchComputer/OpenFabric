// Package attestation provides build attestation for verifying that nodes
// in the network are running officially signed binaries.
//
// During the release build, the CI pipeline signs "version|commitHash" with
// a maintainer Ed25519 private key and injects the signature via ldflags.
// Each node advertises its version, commit, and signature in the CRDT peer
// record.  Receiving nodes verify the signature against the embedded public
// key, or any additional key listed in security.build_pubkeys.
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
	"strings"
	"sync"

	"github.com/spf13/viper"
)

// BuildInfo holds the attestation data that is advertised in the peer record.
type BuildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Signature string `json:"build_sig"` // hex-encoded Ed25519 signature
}

// DefaultPubKeyHex is the hex-encoded Ed25519 public key of the upstream
// release maintainer: the default trust anchor when PubKeysConfigKey is unset.
//
// It is a var rather than a const so a distributor signing its own builds can
// bake in its own default at link time, the same way the signature itself is
// injected:
//
//	-ldflags "-X opentela/internal/attestation.DefaultPubKeyHex=<hex>"
//
// That keeps the trust anchor with the build it belongs to instead of
// requiring every node to be configured. PubKeysConfigKey still overrides it
// at runtime.
var DefaultPubKeyHex = "df45c7c4dd4450cd0f296ea6250c60e8a0dad2f459dbf5908e38977e45098d8b"

// PubKeysConfigKey holds the hex-encoded Ed25519 public keys whose build
// signatures this node accepts, as a list (env: OF_SECURITY_BUILD_PUBKEYS,
// comma- or space-separated). Unset means "trust DefaultPubKeyHex only".
//
// A downstream distributor signs its releases with its own key (see
// cmd/buildsign) and lists the public half here. Listing several keys lets a
// node accept both its own builds and upstream's during a migration.
const PubKeysConfigKey = "security.build_pubkeys"

var (
	pubKeyMu     sync.Mutex
	pubKeyRaw    string // config value the cache below was derived from
	pubKeyCache  []ed25519.PublicKey
	pubKeyCached bool
)

// configuredPubKeyHexes returns the trusted key list from config, falling back
// to DefaultPubKeyHex. Accepts both a real list (flag/config file) and a single
// delimited string, which is how the value arrives through the environment.
func configuredPubKeyHexes() []string {
	var out []string
	for _, v := range viper.GetStringSlice(PubKeysConfigKey) {
		for _, field := range strings.FieldsFunc(v, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
		}) {
			if field = strings.TrimSpace(field); field != "" {
				out = append(out, field)
			}
		}
	}
	if len(out) == 0 {
		return []string{DefaultPubKeyHex}
	}
	return out
}

// trustedPubKeys parses and caches the configured trust anchors. The cache is
// keyed on the raw config value rather than sync.Once: this is read first from
// a hot path, long after viper is populated, but a Once would permanently
// freeze whatever the very first caller happened to see.
func trustedPubKeys() ([]ed25519.PublicKey, error) {
	hexes := configuredPubKeyHexes()
	raw := strings.Join(hexes, ",")

	pubKeyMu.Lock()
	defer pubKeyMu.Unlock()
	if pubKeyCached && pubKeyRaw == raw {
		if len(pubKeyCache) == 0 {
			return nil, fmt.Errorf("no usable build public key configured in %s", PubKeysConfigKey)
		}
		return pubKeyCache, nil
	}

	var keys []ed25519.PublicKey
	for _, h := range hexes {
		b, err := hex.DecodeString(h)
		if err != nil {
			// Skip rather than fail closed on one bad entry: with several keys
			// configured, a typo in one must not disable the others.
			continue
		}
		if len(b) != ed25519.PublicKeySize {
			continue
		}
		keys = append(keys, ed25519.PublicKey(b))
	}

	pubKeyRaw, pubKeyCache, pubKeyCached = raw, keys, true
	if len(keys) == 0 {
		return nil, fmt.Errorf("no usable build public key configured in %s", PubKeysConfigKey)
	}
	return keys, nil
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

	keys, err := trustedPubKeys()
	if err != nil {
		return fmt.Errorf("cannot load build public keys: %w", err)
	}

	sig, err := hex.DecodeString(info.Signature)
	if err != nil {
		return fmt.Errorf("invalid signature hex: %w", err)
	}

	msg := attestationMessage(info.Version, info.Commit)
	for _, pk := range keys {
		if ed25519.Verify(pk, msg, sig) {
			return nil
		}
	}
	return fmt.Errorf("signature verification failed for version=%s commit=%s (tried %d trusted key(s))", info.Version, info.Commit, len(keys))
}

// Sign produces a hex-encoded Ed25519 signature over "version|commit".
// This is used by the release tooling (not at runtime by nodes).
func Sign(privateKey ed25519.PrivateKey, version, commit string) string {
	msg := attestationMessage(version, commit)
	sig := ed25519.Sign(privateKey, msg)
	return hex.EncodeToString(sig)
}
