package protocol

import (
	"opentela/internal/common"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/spf13/viper"
)

func resolveKeyPath() (string, error) {
	if cd := viper.GetString("config_dir"); cd != "" {
		return filepath.Join(cd, "keys", "id"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opentela", "keys", "id"), nil
}

func writeKeyToFile(priv crypto.PrivKey) {
	keyData, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		common.Logger.Error("Error while marshalling private key: ", err)
	}
	keyPath, err := resolveKeyPath()
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

func loadKeyFromFile() crypto.PrivKey {
	keyPath, err := resolveKeyPath()
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
