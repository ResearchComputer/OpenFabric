package usage

import (
	"regexp"
	"testing"
)

func TestGenerateRequestID_Format(t *testing.T) {
	id := GenerateRequestID()

	// Expected format: "<digits>-<8 hex chars>"
	pattern := `^\d+-[0-9a-f]{8}$`
	matched, err := regexp.MatchString(pattern, id)
	if err != nil {
		t.Fatalf("regexp.MatchString failed: %v", err)
	}
	if !matched {
		t.Errorf("GenerateRequestID() = %q, does not match pattern %q", id, pattern)
	}
}

func TestGenerateRequestID_Unique(t *testing.T) {
	seen := make(map[string]struct{}, 100)

	for i := 0; i < 100; i++ {
		id := GenerateRequestID()
		if _, exists := seen[id]; exists {
			t.Fatalf("Duplicate request ID generated: %s (iteration %d)", id, i)
		}
		seen[id] = struct{}{}
	}
}

func TestGenerateRequestID_NotEmpty(t *testing.T) {
	id := GenerateRequestID()
	if id == "" {
		t.Fatal("GenerateRequestID() returned empty string")
	}
}
