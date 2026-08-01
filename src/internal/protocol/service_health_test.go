package protocol

import "testing"

// A service is health-checked once at startup and then advertised forever. If
// the backing engine dies while otela survives — the normal shape of a SLURM
// job ending, since otela and sglang are separate processes — the node stays
// genuinely alive and pingable while advertising a model it can no longer
// serve. No head-side liveness probe can detect that by construction, so the
// producer has to notice it itself.
func TestServiceHealthState_Observe(t *testing.T) {
	const threshold = 3

	t.Run("healthy service is left advertised", func(t *testing.T) {
		s := &serviceHealthState{}
		if got := s.observe(true, threshold); got != serviceHealthNoop {
			t.Fatalf("observe(healthy) = %v, want %v", got, serviceHealthNoop)
		}
	})

	t.Run("failures below the threshold do not withdraw", func(t *testing.T) {
		s := &serviceHealthState{}
		for i := 1; i < threshold; i++ {
			if got := s.observe(false, threshold); got != serviceHealthNoop {
				t.Fatalf("failure %d: observe(unhealthy) = %v, want %v — a single blip must not drop a busy service", i, got, serviceHealthNoop)
			}
		}
	})

	t.Run("reaching the threshold withdraws once", func(t *testing.T) {
		s := &serviceHealthState{}
		for i := 1; i < threshold; i++ {
			s.observe(false, threshold)
		}
		if got := s.observe(false, threshold); got != serviceHealthWithdraw {
			t.Fatalf("observe at threshold = %v, want %v", got, serviceHealthWithdraw)
		}
		// Already withdrawn: further failures must not re-publish the same fact.
		if got := s.observe(false, threshold); got != serviceHealthNoop {
			t.Fatalf("observe past threshold = %v, want %v", got, serviceHealthNoop)
		}
	})

	t.Run("recovery restores the service", func(t *testing.T) {
		s := &serviceHealthState{}
		for i := 0; i < threshold; i++ {
			s.observe(false, threshold)
		}
		if got := s.observe(true, threshold); got != serviceHealthRestore {
			t.Fatalf("observe(healthy) after withdrawal = %v, want %v", got, serviceHealthRestore)
		}
		// And the failure count must have reset, or the next single failure
		// would immediately re-withdraw a service that just proved healthy.
		if got := s.observe(false, threshold); got != serviceHealthNoop {
			t.Fatalf("first failure after recovery = %v, want %v — failure count did not reset", got, serviceHealthNoop)
		}
	})
}

// ReannounceLocalServices merges services found in the node-table entry back
// into the advertised set, so that services registered through other paths are
// not lost. Left alone, that merge would immediately resurrect a service we
// just withdrew for being unhealthy, and the withdrawal would silently no-op.
func TestMergeAdvertisedServices(t *testing.T) {
	local := []Service{{Name: "llm", Port: "30000"}}
	existing := []Service{
		{Name: "llm", Port: "30000"},
		{Name: "flash-sandbox", Port: "8080"},
	}

	t.Run("services from the table are merged in", func(t *testing.T) {
		got := mergeAdvertisedServices(local, existing, nil)
		if len(got) != 2 {
			t.Fatalf("merged = %+v, want both llm and flash-sandbox", got)
		}
	})

	t.Run("a withdrawn service is not resurrected by the merge", func(t *testing.T) {
		withdrawn := map[string]bool{serviceKey(Service{Name: "flash-sandbox", Port: "8080"}): true}
		got := mergeAdvertisedServices(local, existing, withdrawn)
		for _, s := range got {
			if s.Name == "flash-sandbox" {
				t.Fatalf("merged = %+v, want flash-sandbox to stay withdrawn", got)
			}
		}
		if len(got) != 1 {
			t.Fatalf("merged = %+v, want only llm", got)
		}
	})
}
