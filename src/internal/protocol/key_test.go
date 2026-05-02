package protocol

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveKeyPath_DefaultsToHome(t *testing.T) {
	viper.Reset()
	home, _ := os.UserHomeDir()
	got, err := resolveKeyPath()
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".config", "opentela", "keys", "id"), got)
}

func TestResolveKeyPath_HonorsConfigDir(t *testing.T) {
	viper.Reset()
	viper.Set("config_dir", "/tmp/otela-bench-xyz")
	got, err := resolveKeyPath()
	assert.NoError(t, err)
	assert.Equal(t, "/tmp/otela-bench-xyz/keys/id", got)
}

func TestKeyRoundTrip(t *testing.T) {
	// Generate a libp2p RSA private key
	priv, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, rand.Reader)
	require.NoError(t, err, "key generation should succeed")

	// Marshal the private key to bytes
	keyData, err := crypto.MarshalPrivateKey(priv)
	require.NoError(t, err, "marshalling private key should succeed")

	// Unmarshal the bytes back to a private key
	restored, err := crypto.UnmarshalPrivateKey(keyData)
	require.NoError(t, err, "unmarshalling private key should succeed")

	// Verify the restored key matches the original by comparing their raw bytes
	origBytes, err := crypto.MarshalPrivateKey(priv)
	require.NoError(t, err)
	restoredBytes, err := crypto.MarshalPrivateKey(restored)
	require.NoError(t, err)

	assert.Equal(t, origBytes, restoredBytes, "round-tripped key bytes should match original")

	// Also verify the public keys match
	assert.True(t, priv.GetPublic().Equals(restored.GetPublic()), "public keys should be equal")
}
