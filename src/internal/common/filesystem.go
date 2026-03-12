package common

import (
	"os"
)

func RemoveDir(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		Logger.Debug("path does not exist: " + path + ", skipping")
		return nil
	}
	return os.RemoveAll(path)
}
