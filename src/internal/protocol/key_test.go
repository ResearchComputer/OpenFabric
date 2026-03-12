package protocol

import (
	"crypto/rand"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
