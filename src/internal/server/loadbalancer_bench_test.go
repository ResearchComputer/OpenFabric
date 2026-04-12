package server

import (
	"fmt"
	"math"
	"sync"
	"testing"
	"time"
)

// candidates simulates a pool of backend peers.
var benchCandidates = []string{"peer-a", "peer-b", "peer-c", "peer-d", "peer-e"}

// --- Throughput benchmarks ---

func BenchmarkRandom(b *testing.B) {
	lb := &randomLB{}
	for b.Loop() {
		lb.Pick(benchCandidates)
	}
}

func BenchmarkRoundRobin(b *testing.B) {
	lb := &roundRobinLB{}
	for b.Loop() {
		lb.Pick(benchCandidates)
	}
}

func BenchmarkShortestQueue(b *testing.B) {
	sq := newShortestQueueLB(5 * time.Minute)
	defer close(sq.stopCh)
	for b.Loop() {
		idx := sq.Pick(benchCandidates)
		sq.OnRequestStart(benchCandidates[idx])
		sq.OnRequestEnd(benchCandidates[idx])
	}
}

// --- Parallel (contended) throughput ---

func BenchmarkRandom_Parallel(b *testing.B) {
	lb := &randomLB{}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			lb.Pick(benchCandidates)
		}
	})
}

func BenchmarkRoundRobin_Parallel(b *testing.B) {
	lb := &roundRobinLB{}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			lb.Pick(benchCandidates)
		}
	})
}

func BenchmarkShortestQueue_Parallel(b *testing.B) {
	sq := newShortestQueueLB(5 * time.Minute)
	defer close(sq.stopCh)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			idx := sq.Pick(benchCandidates)
			sq.OnRequestStart(benchCandidates[idx])
			sq.OnRequestEnd(benchCandidates[idx])
		}
	})
}

// --- Distribution fairness test ---
// Not a benchmark per se, but uses the test harness to print a comparison table.

func TestDistributionFairness(t *testing.T) {
	const totalRequests = 100_000
	candidates := []string{"peer-a", "peer-b", "peer-c", "peer-d", "peer-e"}

	policies := []struct {
		name string
		lb   LoadBalancer
		stop func()
	}{
		{"random", &randomLB{}, func() {}},
		{"round-robin", &roundRobinLB{}, func() {}},
		{
			"shortest-queue",
			func() *shortestQueueLB {
				sq := newShortestQueueLB(5 * time.Minute)
				return sq
			}(),
			func() {},
		},
	}
	// set stop for shortest-queue
	policies[2].stop = func() { close(policies[2].lb.(*shortestQueueLB).stopCh) }

	for _, p := range policies {
		t.Run(p.name, func(t *testing.T) {
			defer p.stop()
			counts := make(map[string]int, len(candidates))
			for i := 0; i < totalRequests; i++ {
				idx := p.lb.Pick(candidates)
				peer := candidates[idx]
				counts[peer]++
				p.lb.OnRequestStart(peer)
				p.lb.OnRequestEnd(peer)
			}

			expected := float64(totalRequests) / float64(len(candidates))
			t.Logf("  %-15s  expected per peer: %.0f", p.name, expected)
			maxDev := 0.0
			for _, c := range candidates {
				cnt := counts[c]
				dev := math.Abs(float64(cnt)-expected) / expected * 100
				if dev > maxDev {
					maxDev = dev
				}
				t.Logf("    %-10s  %6d  (%.1f%% deviation)", c, cnt, dev)
			}
			t.Logf("  max deviation: %.2f%%", maxDev)

			// Random can have some variance; round-robin should be near-perfect.
			// Shortest-queue with synchronous pick+end always sees 0 inflight on the
			// first candidate, so it degenerates to "always pick index 0" — that's
			// correct behavior; its value shows under concurrency (see
			// TestHeterogeneousLatencySimulation).
			if p.name == "round-robin" && maxDev > 1.0 {
				t.Errorf("expected <1%% deviation for %s, got %.2f%%", p.name, maxDev)
			}
		})
	}
}

// --- Simulated heterogeneous latency ---
// Models peers with different "service times" to show how SQ adapts.

func TestHeterogeneousLatencySimulation(t *testing.T) {
	const totalRequests = 10_000
	candidates := []string{"fast", "medium", "slow"}

	// Simulated service durations per peer.
	latency := map[string]time.Duration{
		"fast":   1 * time.Millisecond,
		"medium": 3 * time.Millisecond,
		"slow":   10 * time.Millisecond,
	}

	policies := []struct {
		name string
		lb   LoadBalancer
		stop func()
	}{
		{"random", &randomLB{}, func() {}},
		{"round-robin", &roundRobinLB{}, func() {}},
		{
			"shortest-queue",
			func() *shortestQueueLB {
				sq := newShortestQueueLB(5 * time.Minute)
				return sq
			}(),
			func() {},
		},
	}
	policies[2].stop = func() { close(policies[2].lb.(*shortestQueueLB).stopCh) }

	for _, p := range policies {
		t.Run(p.name, func(t *testing.T) {
			defer p.stop()
			counts := make(map[string]int, len(candidates))

			// Use a small concurrency to simulate overlapping requests.
			const concurrency = 8
			var mu sync.Mutex
			var wg sync.WaitGroup
			sem := make(chan struct{}, concurrency)

			start := time.Now()
			for i := 0; i < totalRequests; i++ {
				sem <- struct{}{}
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer func() { <-sem }()

					idx := p.lb.Pick(candidates)
					peer := candidates[idx]
					p.lb.OnRequestStart(peer)

					mu.Lock()
					counts[peer]++
					mu.Unlock()

					time.Sleep(latency[peer])
					p.lb.OnRequestEnd(peer)
				}()
			}
			wg.Wait()
			elapsed := time.Since(start)

			t.Logf("  %-15s  total time: %s", p.name, elapsed.Round(time.Millisecond))
			for _, c := range candidates {
				t.Logf("    %-10s  %6d requests  (latency %v)", c, counts[c], latency[c])
			}

			// For shortest-queue, the fast peer should handle significantly more requests.
			if p.name == "shortest-queue" {
				if counts["fast"] <= counts["slow"] {
					t.Errorf("expected fast > slow for SQ, got fast=%d slow=%d", counts["fast"], counts["slow"])
				}
				t.Logf("  fast/slow ratio: %.1fx", float64(counts["fast"])/float64(counts["slow"]))
			}
		})
	}
}

// --- Summary printer ---

func TestPolicySummary(t *testing.T) {
	header := fmt.Sprintf("  %-18s  %-12s  %-12s  %s", "Policy", "Fairness", "Adaptive", "Overhead")
	t.Log("")
	t.Log(header)
	t.Log("  " + "--------------------------------------------------------------")
	t.Logf("  %-18s  %-12s  %-12s  %s", "random", "statistical", "no", "none")
	t.Logf("  %-18s  %-12s  %-12s  %s", "round-robin", "perfect", "no", "atomic add")
	t.Logf("  %-18s  %-12s  %-12s  %s", "shortest-queue", "adaptive", "yes", "mutex + map")
	t.Log("")
}
