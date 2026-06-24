package analytics

import "testing"

func TestNormalizeFillsDefaults(t *testing.T) {
	c := Config{}.Normalize()
	if c.MaxLatencySamples != 512 {
		t.Fatalf("MaxLatencySamples = %d, want 512", c.MaxLatencySamples)
	}
	if c.GroupType != "model" {
		t.Fatalf("GroupType = %q, want model", c.GroupType)
	}
	if c.FlushIntervalSeconds != 60 {
		t.Fatalf("FlushIntervalSeconds = %d, want 60", c.FlushIntervalSeconds)
	}
}

func TestNormalizeKeepsExplicitValues(t *testing.T) {
	c := Config{MaxLatencySamples: 100, GroupType: "node", FlushIntervalSeconds: 30}.Normalize()
	if c.MaxLatencySamples != 100 || c.GroupType != "node" || c.FlushIntervalSeconds != 30 {
		t.Fatalf("Normalize overwrote explicit values: %+v", c)
	}
}
