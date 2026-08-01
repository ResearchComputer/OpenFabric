package protocol

import (
	"context"
	"errors"
	"fmt"
	"testing"

	pbv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/pb"
)

// classifyProbeError matches a string because circuitv2/client.relayError is
// unexported — there is no type to assert on and no sentinel for errors.Is.
// This test is what makes that safe: it rebuilds the error from go-libp2p's own
// Status_name table using the same format string as client.dial, so a rename or
// reformat upstream fails here instead of silently degrading every probe to
// probeUnknown (at which point ghosts would never be evicted again).
func TestClassifyProbeError_MatchesRealLibp2pStatusName(t *testing.T) {
	status := pbv2.Status_NO_RESERVATION
	// Mirrors client/dial.go:177:
	//   newRelayError("error opening relay circuit: %s (%d)", pbv2.Status_name[int32(status)], status)
	realErr := fmt.Errorf("error opening relay circuit: %s (%d)",
		pbv2.Status_name[int32(status)], status)

	if got := classifyProbeError(realErr); got != probeDeadAuthoritative {
		t.Fatalf("classifyProbeError(%q) = %v, want %v — relayNoReservation (%q) no longer matches what go-libp2p emits",
			realErr, got, probeDeadAuthoritative, relayNoReservation)
	}
}

// The whole point of the liveness probe is that a failed dial is not by itself
// evidence that the *worker* is gone — it is usually evidence about the relay.
// Only the relay's NO_RESERVATION answer is a statement about the worker, so
// only that may evict. Everything else must fail safe (leave the peer alone),
// because evicting on an ambiguous failure would wipe every worker behind a
// relay that happens to be restarting.
func TestClassifyProbeError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want probeVerdict
	}{
		{
			name: "no error means the peer answered a ping",
			err:  nil,
			want: probeAlive,
		},
		{
			name: "relay reports no reservation: the worker is definitively detached",
			err:  errors.New("error opening relay circuit: NO_RESERVATION (204)"),
			want: probeDeadAuthoritative,
		},
		{
			name: "wrapped no-reservation is still authoritative",
			err:  fmt.Errorf("dial %s: %w", "QmAbc", errors.New("error opening relay circuit: NO_RESERVATION (204)")),
			want: probeDeadAuthoritative,
		},
		{
			name: "deadline exceeded says nothing about the worker",
			err:  context.DeadlineExceeded,
			want: probeUnknown,
		},
		{
			name: "relay unreachable says nothing about the worker",
			err:  errors.New("dial tcp 10.128.1.1:43917: connect: connection refused"),
			want: probeUnknown,
		},
		{
			name: "relay refusing us says nothing about the worker",
			err:  errors.New("error opening relay circuit: PERMISSION_DENIED (202)"),
			want: probeUnknown,
		},
		{
			name: "relay resource limits say nothing about the worker",
			err:  errors.New("error opening relay circuit: RESOURCE_LIMIT_EXCEEDED (201)"),
			want: probeUnknown,
		},
		{
			// The relay holds a reservation but could not open the stream. That
			// is a claim about reachability, not attachment, and it can be
			// transient at the relay — so it must not evict.
			name: "connection failed at the relay is not authoritative",
			err:  errors.New("error opening relay circuit: CONNECTION_FAILED (203)"),
			want: probeUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyProbeError(tc.err); got != tc.want {
				t.Fatalf("classifyProbeError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
