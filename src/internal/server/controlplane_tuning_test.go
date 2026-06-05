//go:build bandwidthbench

// Verification of the scalability tuning flags' effect on idle control-plane
// bandwidth. Compares, at a fixed mesh size and sparse topology:
//
//	baseline      — production defaults (5s rebroadcast, GossipSub D=6, 20s ping)
//	crdt_tuned    — 60s rebroadcast + GossipSub D=10/Dhi=16 (scalability.crdt_tuned)
//	swim          — SWIM membership (500ms probes), no 20s pubsub ping
//	tuned+swim    — both levers together
//
// SWIM is wired exactly as protocol.StartSWIM does: NewLibP2PTransport +
// NewSWIM + RegisterHandler, seeded with the mesh peers, then Run.
//
// Run with:
//
//	cd src && go test -tags bandwidthbench -run TestControlPlaneTuning -v -timeout 1200s ./internal/server/
package server

import (
	"context"
	"testing"
	"time"

	"opentela/internal/protocol/swim"
)

// startSWIMOnNodes wires SWIM onto every node and seeds each with the others as
// members, mirroring protocol.StartSWIM (node_table.go). Must run AFTER connect.
func startSWIMOnNodes(t *testing.T, ctx context.Context, nodes []*cpNode) {
	t.Helper()
	cfg := swim.Config{ // production defaults (root.go:122-127)
		ProbeInterval:        500 * time.Millisecond,
		ProbeTimeout:         500 * time.Millisecond,
		IndirectProbeTimeout: 1 * time.Second,
		IndirectProbes:       3,
		SuspectTimeout:       5 * time.Second,
		RetransmitMult:       3,
	}
	for i, nd := range nodes {
		tr := swim.NewLibP2PTransport(nd.host, cfg.ProbeTimeout)
		ev := make(chan swim.MemberEvent, 1024)
		s := swim.NewSWIM(nd.host.ID(), cfg, tr, ev)
		swim.RegisterHandler(nd.host, s)
		for j, other := range nodes {
			if j != i {
				s.AddMember(other.host.ID())
			}
		}
		go func() {
			for range ev { // drain events
			}
		}()
		go s.Run(ctx)
	}
}

// runCPConfig builds a fresh mesh with the given config, connects it sparsely,
// optionally starts SWIM, warms up, and returns the per-node B/s.
func runCPConfig(t *testing.T, n int, opt cpOpts, label string) float64 {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	nodes := buildNodes(t, ctx, n, opt)
	defer closeNodes(nodes)
	connectSparse(t, ctx, nodes, 8, 1)
	if opt.enableSWIM {
		startSWIMOnNodes(t, ctx, nodes)
	}
	time.Sleep(25 * time.Second) // warmup
	return measureWindow(t, nodes, 40*time.Second, label)
}

func TestControlPlaneTuning(t *testing.T) {
	const n = 25

	baseline := defaultCPOpts() // 5s, D=6, ping on, no SWIM
	tuned := cpOpts{rebroadcast: 60 * time.Second, gD: 10, gDlo: 4, gDhi: 16, pubsubPing: true}
	swimOnly := cpOpts{rebroadcast: 5 * time.Second, pubsubPing: false, enableSWIM: true}
	both := cpOpts{rebroadcast: 60 * time.Second, gD: 10, gDlo: 4, gDhi: 16, pubsubPing: false, enableSWIM: true}

	base := runCPConfig(t, n, baseline, "baseline")
	rTuned := runCPConfig(t, n, tuned, "crdt_tuned")
	rSwim := runCPConfig(t, n, swimOnly, "swim")
	rBoth := runCPConfig(t, n, both, "tuned+swim")

	t.Logf("")
	t.Logf("TUNING SUMMARY (N=%d, sparse topology, idle, per-node B/s):", n)
	t.Logf("  baseline (5s, D=6, ping)        %8.0f B/s   1.00x", base)
	t.Logf("  crdt_tuned (60s, D=10)          %8.0f B/s   %.2fx", rTuned, rTuned/base)
	t.Logf("  swim (no ping, 500ms probe)     %8.0f B/s   %.2fx", rSwim, rSwim/base)
	t.Logf("  tuned + swim                    %8.0f B/s   %.2fx", rBoth, rBoth/base)
}
