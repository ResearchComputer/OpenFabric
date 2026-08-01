package protocol

import "time"

const (
	// peerDisconnectAfter is how long a peer may go without proof of life
	// before it is marked disconnected. Only applies to peers we cannot probe
	// (no service, so no relay reservation to ask about).
	peerDisconnectAfter = 2 * time.Minute
	// peerStaleAfter is how long a disconnected peer is retained before its
	// row is dropped entirely.
	peerStaleAfter = 10 * time.Minute
)

// peerSweepAction is what the maintenance sweep decides to do with one entry.
type peerSweepAction int

const (
	sweepKeep peerSweepAction = iota
	sweepMarkDisconnected
	sweepDelete
)

func (a peerSweepAction) String() string {
	switch a {
	case sweepMarkDisconnected:
		return "mark-disconnected"
	case sweepDelete:
		return "delete"
	default:
		return "keep"
	}
}

// decideSweepAction chooses what to do with a single node-table entry.
//
// verdict is the result of a liveness probe for peers we could not reach
// directly; it is probeUnknown for peers that were not probed.
//
// The rule this replaces skipped every peer carrying a service, on the theory
// that such a peer might be relay-reachable and unfairly evicted. That made LLM
// workers — the only peers that carry services — impossible to evict at all,
// even after their jobs had been gone for hours, while routing kept selecting
// them. The fix is to demand evidence instead of exempting: a service peer is
// disconnected only when the relay states it holds no reservation, and is never
// disconnected on an ambiguous failure.
func decideSweepAction(p Peer, verdict probeVerdict, now time.Time) peerSweepAction {
	// Never reason about a peer we have no timestamp for; there is nothing to
	// compare against and it may simply be newly discovered.
	if p.LastSeen == 0 {
		return sweepKeep
	}
	lastSeen := time.Unix(p.LastSeen, 0)

	if p.Connected {
		// An authoritative "gone" from the relay outranks any timer: act at
		// once, whether or not the peer carries a service.
		if verdict == probeDeadAuthoritative {
			return sweepMarkDisconnected
		}
		// A peer carrying a service is reachable via its relay, so silence
		// alone is not evidence — only the probe may retire it. Without this,
		// a worker mid-download or under load would be dropped for being quiet.
		if len(p.Service) > 0 {
			return sweepKeep
		}
		if lastSeen.Add(peerDisconnectAfter).Before(now) {
			return sweepMarkDisconnected
		}
		return sweepKeep
	}

	// Already disconnected. Service-carrying rows are retained rather than
	// deleted: routing only consults Connected, so the row is inert, and
	// keeping it preserves the trail for whoever asks why a worker vanished.
	if len(p.Service) > 0 {
		return sweepKeep
	}
	if lastSeen.Add(peerStaleAfter).Before(now) {
		return sweepDelete
	}
	return sweepKeep
}
