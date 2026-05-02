package protocol

import (
	"crypto/rand"
	"opentela/internal/common"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/spf13/viper"
)

// ResolveKeyPath returns the on-disk location of the libp2p identity key.
// It honors viper's "config_dir" override (set by the --config-dir flag) and
// falls back to ~/.config/opentela/keys/id.
func ResolveKeyPath() (string, error) {
	if cd := viper.GetString("config_dir"); cd != "" {
		return filepath.Join(cd, "keys", "id"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opentela", "keys", "id"), nil
}

// WriteKeyToFile marshals priv and writes it to the resolved key path,
// creating parent directories as needed. Exits the process on failure to
// preserve the historical behavior of newHost.
func WriteKeyToFile(priv crypto.PrivKey) {
	keyData, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		common.Logger.Error("Error while marshalling private key: ", err)
	}
	keyPath, err := ResolveKeyPath()
	if err != nil {
		common.Logger.Error("Could not determine home directory: ", err)
		os.Exit(1)
	}
	err = os.MkdirAll(filepath.Dir(keyPath), os.ModePerm)
	if err != nil {
		common.Logger.Error("Could not create keys directory", "error", err)
		os.Exit(1)
	}
	err = os.WriteFile(keyPath, keyData, 0600)
	if err != nil {
		common.Logger.Error("Could not write key to file", err)
		os.Exit(1)
	}
}

// LoadKeyFromFile reads the libp2p identity key from disk and returns it.
// Returns nil if the file does not exist or cannot be parsed; errors are
// logged but not propagated, matching the historical behavior used by
// newHost when probing for an existing identity.
func LoadKeyFromFile() crypto.PrivKey {
	keyPath, err := ResolveKeyPath()
	if err != nil {
		return nil
	}
	common.Logger.Debug("Looking for keys under: ", keyPath)
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		common.Logger.Debug("No key file found: ", err)
		return nil
	}
	priv, err := crypto.UnmarshalPrivateKey(keyData)
	if err != nil {
		common.Logger.Error("Error while unmarshalling private key: ", err)
		return nil
	}
	return priv
}

// GenerateAndWriteKey creates a fresh RSA-2048 libp2p identity key and
// persists it via WriteKeyToFile. It is the same keygen flow used lazily
// by newHost when seed=0 and no key exists, exposed so that `otela init`
// can pre-generate the key without starting a libp2p host.
func GenerateAndWriteKey() error {
	priv, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, rand.Reader)
	if err != nil {
		return err
	}
	WriteKeyToFile(priv)
	return nil
}
