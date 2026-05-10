package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"

	"opentela/internal/protocol"
)

func TestEnsureLibp2pKey_CreatesKeyWhenMissing(t *testing.T) {
	dir, err := os.MkdirTemp("", "otela-init-test-*")
	assert.NoError(t, err)
	defer os.RemoveAll(dir)

	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("config_dir", dir)

	err = ensureLibp2pKey()
	assert.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, "keys", "id"))

	// Idempotent: second call must not fail and must not modify the file.
	info1, _ := os.Stat(filepath.Join(dir, "keys", "id"))
	err = ensureLibp2pKey()
	assert.NoError(t, err)
	info2, _ := os.Stat(filepath.Join(dir, "keys", "id"))
	assert.Equal(t, info1.ModTime(), info2.ModTime())

	// Loadable via the same code path otela start uses.
	priv := protocol.LoadKeyFromFile()
	assert.NotNil(t, priv)
}
