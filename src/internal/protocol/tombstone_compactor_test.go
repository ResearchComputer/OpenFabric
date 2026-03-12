package protocol

import (
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestReadDurationSetting_ReturnsFallbackWhenNotSet(t *testing.T) {
	// Use a key that is definitely not set
	fallback := 24 * time.Hour
	got := readDurationSetting("test.nonexistent.duration.key", fallback)
	assert.Equal(t, fallback, got)
}

func TestReadDurationSetting_ReturnsFallbackWhenZero(t *testing.T) {
	key := "test.zero.duration"
	viper.Set(key, "0s")
	defer viper.Set(key, nil)

	fallback := 30 * time.Minute
	got := readDurationSetting(key, fallback)
	assert.Equal(t, fallback, got)
}

func TestReadDurationSetting_ReturnsConfiguredValue(t *testing.T) {
	key := "test.configured.duration"
	viper.Set(key, "30m")
	defer viper.Set(key, nil)

	fallback := 24 * time.Hour
	got := readDurationSetting(key, fallback)
	assert.Equal(t, 30*time.Minute, got)
}
