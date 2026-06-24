package analytics

import "testing"

func TestParseUsageFull(t *testing.T) {
	body := []byte(`{"id":"x","usage":{"prompt_tokens":100,"completion_tokens":40,"total_tokens":140,"prompt_tokens_details":{"cached_tokens":25}}}`)
	u, ok := ParseUsage(body)
	if !ok {
		t.Fatal("expected ok")
	}
	if u.InputTokens != 100 || u.OutputTokens != 40 || u.TotalTokens != 140 || u.CachedTokens != 25 {
		t.Fatalf("got %+v", u)
	}
}

func TestParseUsageNoCachedDetails(t *testing.T) {
	body := []byte(`{"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	u, ok := ParseUsage(body)
	if !ok || u.CachedTokens != 0 || u.InputTokens != 10 {
		t.Fatalf("got ok=%v %+v", ok, u)
	}
}

func TestParseUsageMissing(t *testing.T) {
	if _, ok := ParseUsage([]byte(`{"choices":[]}`)); ok {
		t.Fatal("expected ok=false when usage absent")
	}
}

func TestParseUsageMalformed(t *testing.T) {
	if _, ok := ParseUsage([]byte(`not json`)); ok {
		t.Fatal("expected ok=false on malformed body")
	}
}

func TestParseUsageNull(t *testing.T) {
	if _, ok := ParseUsage([]byte(`{"usage":null}`)); ok {
		t.Fatal("expected ok=false when usage is JSON null")
	}
}
