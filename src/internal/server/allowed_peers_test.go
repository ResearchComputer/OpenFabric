package server

import (
	"testing"
)

func TestIntersectAllowedPeersPreservesOrder(t *testing.T) {
	candidates := []string{"peer-A", "peer-B", "peer-C", "peer-D"}
	allowed := "peer-C,peer-A"
	result := intersectAllowedPeers(candidates, allowed)
	// Order follows candidates (priority-sorted), not the header.
	want := []string{"peer-A", "peer-C"}
	if len(result) != len(want) {
		t.Fatalf("got %v, want %v", result, want)
	}
	for i, p := range result {
		if p != want[i] {
			t.Fatalf("result[%d] = %q, want %q", i, p, want[i])
		}
	}
}

func TestIntersectAllowedPeersEmptyHeader(t *testing.T) {
	candidates := []string{"peer-A", "peer-B"}
	if got := intersectAllowedPeers(candidates, ""); len(got) != len(candidates) {
		t.Fatalf("empty header should be a no-op, got %v", got)
	}
}

func TestIntersectAllowedPeersNoMatch(t *testing.T) {
	candidates := []string{"peer-A", "peer-B"}
	allowed := "peer-X,peer-Y"
	if got := intersectAllowedPeers(candidates, allowed); len(got) != 0 {
		t.Fatalf("no match should return empty, got %v", got)
	}
}

func TestIntersectAllowedPeersWhitespace(t *testing.T) {
	candidates := []string{"peer-A", "peer-B"}
	allowed := " peer-A , peer-B "
	result := intersectAllowedPeers(candidates, allowed)
	if len(result) != 2 {
		t.Fatalf("whitespace should be trimmed, got %v", result)
	}
}

func TestIntersectAllowedPeersEmptyCandidates(t *testing.T) {
	if got := intersectAllowedPeers(nil, "peer-A"); len(got) != 0 {
		t.Fatalf("nil candidates should return empty, got %v", got)
	}
}
