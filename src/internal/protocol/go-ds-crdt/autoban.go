package crdt

import (
	"context"
	"fmt"
	"sync"
	"time"

	dshelp "github.com/ipfs/boxo/datastore/dshelp"
	cid "github.com/ipfs/go-cid"
	ds "github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/query"
	logging "github.com/ipfs/go-log/v2"
)

// bannedNs is the namespace under the CRDT root where persistently
// unfetchable CIDs are recorded.
const bannedNs = "x" // banned

type failureRecord struct {
	count     int
	firstSeen time.Time
	lastSeen  time.Time
}

// autoBan tracks repeated fetch failures and persistently bans CIDs that have
// been declared permanently unreachable. Banned CIDs are skipped on incoming
// broadcasts, filtered from rebroadcasts, and excluded from DAG repair so the
// store can return to a clean state after an orphan head is recognised.
//
// The in-memory banned set is keyed by the datastore-key string derived from
// the CID's multihash, matching the format used for persistence — this keeps
// membership checks O(1) without needing to reverse multihash → CID at load
// time.
//
// A nil receiver is safe to call but does nothing — that lets callers use the
// auto-ban hooks unconditionally without nil checks scattered everywhere.
type autoBan struct {
	enabled       bool
	maxFailures   int
	minFailureAge time.Duration

	namespace ds.Key
	store     ds.Datastore
	logger    logging.StandardLogger

	mu       sync.RWMutex
	banned   map[string]struct{} // keyed by bannedKey(c).String()
	failures map[cid.Cid]*failureRecord
}

func newAutoBan(opts *Options, namespace ds.Key, store ds.Datastore) *autoBan {
	return &autoBan{
		enabled:       opts.MaxFetchFailures > 0,
		maxFailures:   opts.MaxFetchFailures,
		minFailureAge: opts.MinFetchFailureAge,
		namespace:     namespace,
		store:         store,
		logger:        opts.Logger,
		banned:        make(map[string]struct{}),
		failures:      make(map[cid.Cid]*failureRecord),
	}
}

func (a *autoBan) bannedKey(c cid.Cid) ds.Key {
	return a.namespace.ChildString(bannedNs).Child(dshelp.MultihashToDsKey(c.Hash()))
}

// loadFromStore populates the in-memory banned set from prior runs.
func (a *autoBan) loadFromStore(ctx context.Context) error {
	if a == nil || !a.enabled {
		return nil
	}
	prefix := a.namespace.ChildString(bannedNs).String()
	res, err := a.store.Query(ctx, query.Query{Prefix: prefix, KeysOnly: true})
	if err != nil {
		return fmt.Errorf("autoban: query: %w", err)
	}
	defer res.Close()

	for entry := range res.Next() {
		if entry.Error != nil {
			a.logger.Warnf("autoban: query entry error: %s", entry.Error)
			continue
		}
		a.banned[entry.Key] = struct{}{}
	}
	if n := len(a.banned); n > 0 {
		a.logger.Infof("autoban: loaded %d persisted ban records", n)
	}
	return nil
}

func (a *autoBan) isBanned(c cid.Cid) bool {
	if a == nil || !a.enabled {
		return false
	}
	key := a.bannedKey(c).String()
	a.mu.RLock()
	_, ok := a.banned[key]
	a.mu.RUnlock()
	return ok
}

// recordFailure increments the failure counter for c and returns true if the
// CID was newly promoted to banned.
func (a *autoBan) recordFailure(ctx context.Context, c cid.Cid) bool {
	if a == nil || !a.enabled {
		return false
	}
	key := a.bannedKey(c).String()
	a.mu.Lock()
	if _, already := a.banned[key]; already {
		a.mu.Unlock()
		return false
	}
	rec, ok := a.failures[c]
	now := time.Now()
	if !ok {
		rec = &failureRecord{firstSeen: now}
		a.failures[c] = rec
	}
	rec.count++
	rec.lastSeen = now
	age := now.Sub(rec.firstSeen)
	if rec.count < a.maxFailures || age < a.minFailureAge {
		a.mu.Unlock()
		return false
	}
	a.banned[key] = struct{}{}
	count := rec.count
	delete(a.failures, c)
	a.mu.Unlock()

	if err := a.store.Put(ctx, a.bannedKey(c), nil); err != nil {
		// In-memory ban is already in effect; persistence failure just
		// means we'll re-learn the ban after restart.
		a.logger.Errorf("autoban: failed to persist ban for %s: %s", c, err)
	}
	a.logger.Warnf("autoban: banning unreachable cid %s after %d failures over %s",
		c, count, age.Truncate(time.Minute))
	return true
}

// clearFailure removes failure tracking for c. Called on a successful fetch so
// that a previously-flaky CID that recovers does not accumulate stale counters
// that could trip the threshold later.
func (a *autoBan) clearFailure(c cid.Cid) {
	if a == nil || !a.enabled {
		return
	}
	a.mu.Lock()
	delete(a.failures, c)
	a.mu.Unlock()
}
