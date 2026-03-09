package solana

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindATA(t *testing.T) {
	owner := make([]byte, 32)
	mint := make([]byte, 32)
	owner[0] = 1
	mint[0] = 2

	ata, err := FindATA(owner, mint)
	require.NoError(t, err)
	assert.Len(t, ata, 32)

	// Same inputs should produce same ATA.
	ata2, err := FindATA(owner, mint)
	require.NoError(t, err)
	assert.Equal(t, ata, ata2)

	// Different owner should produce different ATA.
	owner2 := make([]byte, 32)
	owner2[0] = 3
	ata3, err := FindATA(owner2, mint)
	require.NoError(t, err)
	assert.NotEqual(t, ata, ata3)
}

func TestFindATA_InvalidInput(t *testing.T) {
	_, err := FindATA([]byte{1, 2, 3}, make([]byte, 32))
	assert.Error(t, err)

	_, err = FindATA(make([]byte, 32), []byte{1, 2, 3})
	assert.Error(t, err)
}

func TestBuildSPLTransferChecked(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	fromATA := make([]byte, 32)
	toATA := make([]byte, 32)
	mint := make([]byte, 32)
	blockhash := make([]byte, 32)
	fromATA[0] = 1
	toATA[0] = 2
	mint[0] = 3

	msg := BuildSPLTransferChecked(pub, fromATA, toATA, mint, 1000, 9, blockhash)
	assert.NotEmpty(t, msg)

	// Verify header.
	assert.Equal(t, byte(1), msg[0]) // num_required_signatures
	assert.Equal(t, byte(0), msg[1]) // num_readonly_signed
	assert.Equal(t, byte(2), msg[2]) // num_readonly_unsigned
}

func TestBuildCreateATAInstruction(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	owner := make([]byte, 32)
	mint := make([]byte, 32)
	ata := make([]byte, 32)
	blockhash := make([]byte, 32)

	msg := BuildCreateATAInstruction(pub, owner, mint, ata, blockhash)
	assert.NotEmpty(t, msg)

	// Verify header.
	assert.Equal(t, byte(1), msg[0]) // num_required_signatures
	assert.Equal(t, byte(0), msg[1]) // num_readonly_signed
	assert.Equal(t, byte(5), msg[2]) // num_readonly_unsigned
}
