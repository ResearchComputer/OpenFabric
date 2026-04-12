package server

import (
	"testing"
)

func TestRandomLB(t *testing.T) {
	lb := &randomLB{}
	candidates := []string{"a", "b", "c"}
	for i := 0; i < 100; i++ {
		idx := lb.Pick(candidates)
		if idx < 0 || idx >= len(candidates) {
			t.Fatalf("index out of range: %d", idx)
		}
	}
}

func TestRoundRobinLB(t *testing.T) {
	lb := &roundRobinLB{}
	candidates := []string{"a", "b", "c"}

	// Should cycle through 1, 2, 0, 1, 2, 0, ...
	expected := []int{1, 2, 0, 1, 2, 0}
	for i, want := range expected {
		got := lb.Pick(candidates)
		if got != want {
			t.Errorf("pick %d: got %d, want %d", i, got, want)
		}
	}
}

func TestShortestQueueLB(t *testing.T) {
	sq := newShortestQueueLB(5 * 60 * 1e9) // 5 min, won't fire in test
	defer close(sq.stopCh)

	candidates := []string{"a", "b", "c"}

	// All empty — should pick first (index 0)
	if idx := sq.Pick(candidates); idx != 0 {
		t.Fatalf("expected 0, got %d", idx)
	}

	// Load up "a" with 2 inflight requests
	sq.OnRequestStart("a")
	sq.OnRequestStart("a")
	// "b" gets 1
	sq.OnRequestStart("b")

	// Should pick "c" (0 inflight)
	idx := sq.Pick(candidates)
	if candidates[idx] != "c" {
		t.Fatalf("expected c, got %s (idx %d)", candidates[idx], idx)
	}

	// Finish one from "a"
	sq.OnRequestEnd("a")
	// Now a=1, b=1, c=0 — still c
	idx = sq.Pick(candidates)
	if candidates[idx] != "c" {
		t.Fatalf("expected c, got %s", candidates[idx])
	}

	// Load c to 2
	sq.OnRequestStart("c")
	sq.OnRequestStart("c")
	// Now a=1, b=1, c=2 — should pick a or b (first found = a at index 0)
	idx = sq.Pick(candidates)
	if idx != 0 && idx != 1 {
		t.Fatalf("expected index 0 or 1, got %d", idx)
	}
}

func TestSetLoadBalancerPolicy(t *testing.T) {
	SetLoadBalancerPolicy(PolicyRandom)
	if _, ok := GetLoadBalancer().(*randomLB); !ok {
		t.Fatal("expected randomLB")
	}

	SetLoadBalancerPolicy(PolicyRoundRobin)
	if _, ok := GetLoadBalancer().(*roundRobinLB); !ok {
		t.Fatal("expected roundRobinLB")
	}

	SetLoadBalancerPolicy(PolicyShortestQueue)
	if _, ok := GetLoadBalancer().(*shortestQueueLB); !ok {
		t.Fatal("expected shortestQueueLB")
	}

	// Reset to default for other tests
	SetLoadBalancerPolicy(PolicyRandom)
}
