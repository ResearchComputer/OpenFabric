// Package analytics emits periodic per-model performance rollups from the head
// node to PostHog. It is opt-in and has no effect unless analytics.enabled.
package analytics

// Config holds the analytics module settings. It is a plain struct populated by
// the server from Viper so this package stays dependency-free and testable.
type Config struct {
	Enabled              bool
	PostHogAPIKey        string
	PostHogHost          string
	FlushIntervalSeconds int
	// ParseUsageBody controls response-body token parsing; default true is applied by Viper (not by Normalize).
	ParseUsageBody    bool
	MaxLatencySamples int
	GroupType            string
}

// Normalize fills zero-valued fields with safe defaults.
func (c Config) Normalize() Config {
	if c.FlushIntervalSeconds <= 0 {
		c.FlushIntervalSeconds = 60
	}
	if c.MaxLatencySamples <= 0 {
		c.MaxLatencySamples = 512
	}
	if c.GroupType == "" {
		c.GroupType = "model"
	}
	return c
}
