package server

import (
	"opentela/internal/protocol"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func peer(id string, services ...protocol.Service) protocol.Peer {
	return protocol.Peer{ID: id, Service: services}
}

func svc(name string, identityGroups ...string) protocol.Service {
	return protocol.Service{Name: name, IdentityGroup: identityGroups}
}

// ---------------------------------------------------------------------------
// parseFallbackLevel
// ---------------------------------------------------------------------------

func TestParseFallbackLevel_EmptyHeader(t *testing.T) {
	assert.Equal(t, 0, parseFallbackLevel(""))
}

func TestParseFallbackLevel_ValidValues(t *testing.T) {
	assert.Equal(t, 0, parseFallbackLevel("0"))
	assert.Equal(t, 1, parseFallbackLevel("1"))
	assert.Equal(t, 2, parseFallbackLevel("2"))
}

func TestParseFallbackLevel_InvalidValues(t *testing.T) {
	for _, v := range []string{"3", "-1", "abc", "1.5", " 1", "2 "} {
		assert.Equal(t, 0, parseFallbackLevel(v), "expected 0 for %q", v)
	}
}

// ---------------------------------------------------------------------------
// selectCandidates – exact match
// ---------------------------------------------------------------------------

func TestSelectCandidates_ExactMatch(t *testing.T) {
	providers := []protocol.Peer{
		peer("peer-exact", svc("llm", "model=gpt4")),
		peer("peer-other", svc("llm", "model=llama")),
	}
	body := []byte(`{"model":"gpt4"}`)
	got := selectCandidates(providers, "llm", body, 0)
	assert.Equal(t, []string{"peer-exact"}, got)
}

func TestSelectCandidates_ExactMatch_MultipleMatches(t *testing.T) {
	providers := []protocol.Peer{
		peer("peer-a", svc("llm", "model=gpt4")),
		peer("peer-b", svc("llm", "model=gpt4")),
	}
	body := []byte(`{"model":"gpt4"}`)
	got := selectCandidates(providers, "llm", body, 0)
	assert.ElementsMatch(t, []string{"peer-a", "peer-b"}, got)
}

func TestSelectCandidates_ExactMatch_NoMatch_Returns_Empty(t *testing.T) {
	providers := []protocol.Peer{
		peer("peer-a", svc("llm", "model=gpt4")),
	}
	body := []byte(`{"model":"llama"}`)
	got := selectCandidates(providers, "llm", body, 0)
	assert.Empty(t, got)
}

// ---------------------------------------------------------------------------
// selectCandidates – wildcard match
// ---------------------------------------------------------------------------

func TestSelectCandidates_WildcardMatch(t *testing.T) {
	providers := []protocol.Peer{
		peer("peer-wild", svc("llm", "model=*")),
	}
	body := []byte(`{"model":"anything"}`)
	// fallback level 0: wildcard should NOT be used
	assert.Empty(t, selectCandidates(providers, "llm", body, 0))
	// fallback level 1: wildcard IS used
	got := selectCandidates(providers, "llm", body, 1)
	assert.Equal(t, []string{"peer-wild"}, got)
}

func TestSelectCandidates_WildcardMatch_KeyMissing_NotMatched(t *testing.T) {
	providers := []protocol.Peer{
		peer("peer-wild", svc("llm", "model=*")),
	}
	body := []byte(`{"temperature":0.7}`) // "model" key absent
	got := selectCandidates(providers, "llm", body, 2)
	assert.Empty(t, got)
}

// ---------------------------------------------------------------------------
// selectCandidates – catch-all match
// ---------------------------------------------------------------------------

func TestSelectCandidates_CatchAll(t *testing.T) {
	providers := []protocol.Peer{
		peer("peer-all", svc("llm", "all")),
	}
	body := []byte(`{}`)
	// fallback level 0: catch-all should NOT be used
	assert.Empty(t, selectCandidates(providers, "llm", body, 0))
	// fallback level 1: catch-all still NOT used (need level 2)
	assert.Empty(t, selectCandidates(providers, "llm", body, 1))
	// fallback level 2: catch-all IS used
	got := selectCandidates(providers, "llm", body, 2)
	assert.Equal(t, []string{"peer-all"}, got)
}

// ---------------------------------------------------------------------------
// selectCandidates – priority ordering exact > wildcard > catch-all
// ---------------------------------------------------------------------------

func TestSelectCandidates_Priority_ExactBeatsWildcard(t *testing.T) {
	providers := []protocol.Peer{
		peer("peer-exact", svc("llm", "model=gpt4")),
		peer("peer-wild", svc("llm", "model=*")),
	}
	body := []byte(`{"model":"gpt4"}`)
	// With fallback level 2 there are both exact and wildcard candidates;
	// exact tier should win and peer-wild must not appear.
	got := selectCandidates(providers, "llm", body, 2)
	assert.Equal(t, []string{"peer-exact"}, got)
}

func TestSelectCandidates_Priority_WildcardBeforeCatchAll(t *testing.T) {
	providers := []protocol.Peer{
		peer("peer-wild", svc("llm", "model=*")),
		peer("peer-all", svc("llm", "all")),
	}
	body := []byte(`{"model":"gpt4"}`)
	// fallback 2: wildcard should win over catch-all
	got := selectCandidates(providers, "llm", body, 2)
	assert.Equal(t, []string{"peer-wild"}, got)
}

func TestSelectCandidates_Priority_ExactBeatsCatchAll(t *testing.T) {
	providers := []protocol.Peer{
		peer("peer-exact", svc("llm", "model=gpt4")),
		peer("peer-all", svc("llm", "all")),
	}
	body := []byte(`{"model":"gpt4"}`)
	got := selectCandidates(providers, "llm", body, 2)
	assert.Equal(t, []string{"peer-exact"}, got)
}

// ---------------------------------------------------------------------------
// selectCandidates – fallback levels
// ---------------------------------------------------------------------------

func TestSelectCandidates_FallbackLevel0_OnlyExact(t *testing.T) {
	providers := []protocol.Peer{
		peer("peer-wild", svc("llm", "model=*")),
		peer("peer-all", svc("llm", "all")),
	}
	body := []byte(`{"model":"gpt4"}`)
	assert.Empty(t, selectCandidates(providers, "llm", body, 0))
}

func TestSelectCandidates_FallbackLevel1_AllowsWildcard(t *testing.T) {
	providers := []protocol.Peer{
		peer("peer-wild", svc("llm", "model=*")),
		peer("peer-all", svc("llm", "all")),
	}
	body := []byte(`{"model":"gpt4"}`)
	got := selectCandidates(providers, "llm", body, 1)
	assert.Equal(t, []string{"peer-wild"}, got)
}

func TestSelectCandidates_FallbackLevel2_AllowsCatchAll(t *testing.T) {
	providers := []protocol.Peer{
		peer("peer-all", svc("llm", "all")),
	}
	body := []byte(`{}`)
	got := selectCandidates(providers, "llm", body, 2)
	assert.Equal(t, []string{"peer-all"}, got)
}

// ---------------------------------------------------------------------------
// selectCandidates – edge cases
// ---------------------------------------------------------------------------

func TestSelectCandidates_EmptyProviders(t *testing.T) {
	got := selectCandidates(nil, "llm", []byte(`{"model":"gpt4"}`), 2)
	assert.Empty(t, got)
}

func TestSelectCandidates_EmptyIdentityGroup(t *testing.T) {
	providers := []protocol.Peer{
		peer("peer-a", svc("llm")), // no identity groups
	}
	got := selectCandidates(providers, "llm", []byte(`{"model":"gpt4"}`), 2)
	assert.Empty(t, got)
}

func TestSelectCandidates_MalformedIdentityGroupEntry(t *testing.T) {
	// Entries without "=" separator are skipped; only valid entries count.
	providers := []protocol.Peer{
		peer("peer-a", svc("llm", "malformed-no-equals", "model=gpt4")),
	}
	body := []byte(`{"model":"gpt4"}`)
	got := selectCandidates(providers, "llm", body, 0)
	assert.Equal(t, []string{"peer-a"}, got)
}

func TestSelectCandidates_MalformedOnly_NoMatch(t *testing.T) {
	providers := []protocol.Peer{
		peer("peer-a", svc("llm", "malformed", "also-malformed")),
	}
	got := selectCandidates(providers, "llm", []byte(`{"model":"gpt4"}`), 2)
	assert.Empty(t, got)
}

func TestSelectCandidates_MultipleIdentityGroupEntries_ExactWins(t *testing.T) {
	// Peer advertises both a catch-all and an exact entry; best match wins.
	providers := []protocol.Peer{
		peer("peer-a", svc("llm", "all", "model=gpt4")),
	}
	body := []byte(`{"model":"gpt4"}`)
	// With fallback 0, exact match is used so peer-a must appear
	got := selectCandidates(providers, "llm", body, 0)
	assert.Equal(t, []string{"peer-a"}, got)
}

func TestSelectCandidates_MultipleIdentityGroupEntries_WildcardBeforeCatchAll(t *testing.T) {
	// Peer advertises both catch-all and wildcard; wildcard should be recorded.
	providers := []protocol.Peer{
		peer("peer-a", svc("llm", "all", "model=*")),
	}
	body := []byte(`{"model":"something"}`)
	// Level 1 allows wildcard (not catch-all). Peer should match.
	got := selectCandidates(providers, "llm", body, 1)
	assert.Equal(t, []string{"peer-a"}, got)
}

func TestSelectCandidates_ServiceNameFilter(t *testing.T) {
	// Providers for a different service should not appear.
	providers := []protocol.Peer{
		peer("peer-other", svc("vision", "all")),
	}
	got := selectCandidates(providers, "llm", []byte(`{}`), 2)
	assert.Empty(t, got)
}

func TestSelectCandidates_ProviderWithMultipleServices_CorrectServiceMatched(t *testing.T) {
	// A provider offers two services; only the matching one counts.
	providers := []protocol.Peer{
		peer("peer-a", svc("vision", "all"), svc("llm", "model=gpt4")),
	}
	body := []byte(`{"model":"gpt4"}`)
	got := selectCandidates(providers, "llm", body, 0)
	assert.Equal(t, []string{"peer-a"}, got)
}

func TestSelectCandidates_EmptyBody_CatchAllStillMatches(t *testing.T) {
	providers := []protocol.Peer{
		peer("peer-all", svc("llm", "all")),
	}
	got := selectCandidates(providers, "llm", nil, 2)
	assert.Equal(t, []string{"peer-all"}, got)
}

func TestSelectCandidates_ProviderNotAddedTwice(t *testing.T) {
	// Provider has multiple services named "llm"; it should only appear once.
	providers := []protocol.Peer{
		peer("peer-a", svc("llm", "model=gpt4"), svc("llm", "all")),
	}
	body := []byte(`{"model":"gpt4"}`)
	got := selectCandidates(providers, "llm", body, 2)
	// Only one entry should be returned despite two matching services.
	assert.Len(t, got, 1)
	assert.Equal(t, "peer-a", got[0])
}

// ---------------------------------------------------------------------------
// weightedRandomSelect
// ---------------------------------------------------------------------------

func TestWeightedSelection(t *testing.T) {
	candidates := []weightedCandidate{
		{peerID: "p1", score: 0.9},
		{peerID: "p2", score: 0.1},
	}
	// Run 1000 selections — p1 should be picked much more often
	counts := map[string]int{}
	for i := 0; i < 1000; i++ {
		pick := weightedRandomSelect(candidates)
		counts[pick]++
	}
	// p1 should get ~90% of selections
	if counts["p1"] < 700 {
		t.Fatalf("p1 should be picked ~90%% of time, got %d/1000", counts["p1"])
	}
}

func TestWeightedSelectionSingleCandidate(t *testing.T) {
	candidates := []weightedCandidate{
		{peerID: "p1", score: 1.0},
	}
	pick := weightedRandomSelect(candidates)
	if pick != "p1" {
		t.Fatal("should pick the only candidate")
	}
}

// ---------------------------------------------------------------------------
// excludePeers
// ---------------------------------------------------------------------------

func TestExcludePeer(t *testing.T) {
	candidates := []string{"p1", "p2", "p3"}
	excluded := map[string]bool{"p1": true}
	filtered := excludePeers(candidates, excluded)
	if len(filtered) != 2 {
		t.Fatalf("expected 2, got %d", len(filtered))
	}
	for _, c := range filtered {
		if c == "p1" {
			t.Fatal("p1 should be excluded")
		}
	}
}

// ---------------------------------------------------------------------------
// shouldShedLoad (admission control)
// ---------------------------------------------------------------------------

func TestAdmissionControlAllowsWhenHealthy(t *testing.T) {
	if shouldShedLoad(10, 10) {
		t.Fatal("should not shed when all workers available")
	}
}

func TestAdmissionControlShedsWhenDegraded(t *testing.T) {
	shedCount := 0
	for i := 0; i < 1000; i++ {
		if shouldShedLoad(1, 100) {
			shedCount++
		}
	}
	if shedCount < 900 {
		t.Fatalf("expected ~990 shed, got %d", shedCount)
	}
}
