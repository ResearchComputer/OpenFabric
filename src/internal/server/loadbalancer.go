package server

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// Policy identifies a load-balancing strategy.
type Policy string

const (
	PolicyRandom        Policy = "random"
	PolicyRoundRobin    Policy = "round-robin"
	PolicyShortestQueue Policy = "shortest-queue"
)

// LoadBalancer picks one candidate from a list of peer IDs.
type LoadBalancer interface {
	// Pick returns the index of the chosen candidate.
	Pick(candidates []string) int
	// OnRequestStart is called when a request is dispatched to peer.
	OnRequestStart(peer string)
	// OnRequestEnd is called when a request to peer finishes.
	OnRequestEnd(peer string)
}

// --- Random ---

type randomLB struct{}

func (r *randomLB) Pick(candidates []string) int { return rand.Intn(len(candidates)) }
func (r *randomLB) OnRequestStart(string)        {}
func (r *randomLB) OnRequestEnd(string)          {}

// --- Round-Robin ---

type roundRobinLB struct {
	counter atomic.Uint64
}

func (rr *roundRobinLB) Pick(candidates []string) int {
	n := rr.counter.Add(1)
	return int(n % uint64(len(candidates)))
}
func (rr *roundRobinLB) OnRequestStart(string) {}
func (rr *roundRobinLB) OnRequestEnd(string)   {}

// --- Shortest Queue (Join-Shortest-Queue) ---

type shortestQueueLB struct {
	mu       sync.RWMutex
	inflight map[string]int64

	// Periodic refresh: reset counters to 0 to prevent drift from
	// missed OnRequestEnd calls (e.g. cancelled contexts).
	resetInterval time.Duration
	stopCh        chan struct{}
}

func newShortestQueueLB(resetInterval time.Duration) *shortestQueueLB {
	sq := &shortestQueueLB{
		inflight:      make(map[string]int64),
		resetInterval: resetInterval,
		stopCh:        make(chan struct{}),
	}
	go sq.resetLoop()
	return sq
}

func (sq *shortestQueueLB) resetLoop() {
	ticker := time.NewTicker(sq.resetInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			sq.mu.Lock()
			for k := range sq.inflight {
				sq.inflight[k] = 0
			}
			sq.mu.Unlock()
		case <-sq.stopCh:
			return
		}
	}
}

func (sq *shortestQueueLB) Pick(candidates []string) int {
	sq.mu.RLock()
	defer sq.mu.RUnlock()

	bestIdx := 0
	bestLoad := sq.inflight[candidates[0]]
	for i := 1; i < len(candidates); i++ {
		load := sq.inflight[candidates[i]]
		if load < bestLoad {
			bestLoad = load
			bestIdx = i
		}
	}
	return bestIdx
}

func (sq *shortestQueueLB) OnRequestStart(peer string) {
	sq.mu.Lock()
	sq.inflight[peer]++
	sq.mu.Unlock()
}

func (sq *shortestQueueLB) OnRequestEnd(peer string) {
	sq.mu.Lock()
	if sq.inflight[peer] > 0 {
		sq.inflight[peer]--
	}
	sq.mu.Unlock()
}

// --- Factory ---

// globalLB is the singleton load balancer used by GlobalServiceForwardHandler.
var globalLB LoadBalancer = &randomLB{} // default

// SetLoadBalancerPolicy configures the global load balancer.
// Call this once at startup before serving requests.
func SetLoadBalancerPolicy(p Policy) {
	switch p {
	case PolicyRoundRobin:
		globalLB = &roundRobinLB{}
	case PolicyShortestQueue:
		globalLB = newShortestQueueLB(5 * time.Minute)
	default:
		globalLB = &randomLB{}
	}
}

// GetLoadBalancer returns the active load balancer.
func GetLoadBalancer() LoadBalancer {
	return globalLB
}
