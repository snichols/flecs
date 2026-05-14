package flecs_test

import (
	"sync"
	"testing"
	"time"

	"github.com/snichols/flecs"
)

type addonPos struct{ X, Y float32 }

// TestStatsSnapshot_EmptyWorld verifies that StatsSnapshot on a fresh world
// returns zero for tick-derived fields and does not panic.
func TestStatsSnapshot_EmptyWorld(t *testing.T) {
	w := flecs.New()
	snap := w.StatsSnapshot()
	if snap.World.FrameCount != 0 {
		t.Errorf("FrameCount: got %d, want 0", snap.World.FrameCount)
	}
	if snap.World.TotalTime != 0 {
		t.Errorf("TotalTime: got %v, want 0", snap.World.TotalTime)
	}
	if snap.World.LastTickDelta != 0 {
		t.Errorf("LastTickDelta: got %v, want 0", snap.World.LastTickDelta)
	}
	if len(snap.Phases) != 0 {
		t.Errorf("Phases: got len %d, want 0 before first Progress", len(snap.Phases))
	}
	if len(snap.Systems) != 0 {
		t.Errorf("Systems: got len %d, want 0 before first Progress", len(snap.Systems))
	}
}

// TestStatsSnapshot_EntityCountIncrements verifies that WorldStats.EntityCount
// reflects newly created entities after a Progress call.
func TestStatsSnapshot_EntityCountIncrements(t *testing.T) {
	w := flecs.New()
	w.Progress(0)
	before := w.StatsSnapshot().World.EntityCount

	w.Write(func(fw *flecs.Writer) {
		fw.NewEntity()
		fw.NewEntity()
	})

	w.Progress(0)
	after := w.StatsSnapshot().World.EntityCount

	if after != before+2 {
		t.Errorf("EntityCount: got %d, want %d after adding 2 entities", after, before+2)
	}
}

// TestStatsSnapshot_FrameCountIncrements verifies that WorldStats.FrameCount
// increments with each Progress call.
func TestStatsSnapshot_FrameCountIncrements(t *testing.T) {
	w := flecs.New()
	const n = 5
	for i := 0; i < n; i++ {
		w.Progress(0.016)
	}
	snap := w.StatsSnapshot()
	if snap.World.FrameCount != n {
		t.Errorf("FrameCount: got %d, want %d", snap.World.FrameCount, n)
	}
}

// TestStatsSnapshot_TwoSystemsHaveInvocations verifies that two registered
// systems both have non-zero Invocations after a Progress call.
func TestStatsSnapshot_TwoSystemsHaveInvocations(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[addonPos](w)
	q := flecs.NewCachedQuery(w, posID)
	noop := func(dt float32, it *flecs.QueryIter) {}

	s1 := flecs.NewSystem(w, q, noop).SetName("alpha")
	s2 := flecs.NewSystem(w, q, noop).SetName("beta")
	_ = s1
	_ = s2

	w.Progress(0.016)
	snap := w.StatsSnapshot()

	if len(snap.Systems) != 2 {
		t.Fatalf("Systems: got %d, want 2", len(snap.Systems))
	}
	for _, ss := range snap.Systems {
		if ss.Invocations != 1 {
			t.Errorf("System %q: Invocations = %d, want 1", ss.Name, ss.Invocations)
		}
	}
}

// TestStatsSnapshot_DisabledSystemHasZeroInvocations verifies that a disabled
// system contributes zero Invocations to the snapshot.
func TestStatsSnapshot_DisabledSystemHasZeroInvocations(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[addonPos](w)
	q := flecs.NewCachedQuery(w, posID)
	noop := func(dt float32, it *flecs.QueryIter) {}

	active := flecs.NewSystem(w, q, noop).SetName("active")
	disabled := flecs.NewSystem(w, q, noop).SetName("disabled")
	disabled.SetEnabled(false)
	_ = active

	w.Progress(0.016)
	snap := w.StatsSnapshot()

	found := false
	for _, ss := range snap.Systems {
		if ss.Name == "disabled" {
			found = true
			if ss.Invocations != 0 {
				t.Errorf("disabled system Invocations = %d, want 0", ss.Invocations)
			}
		}
		if ss.Name == "active" {
			if ss.Invocations != 1 {
				t.Errorf("active system Invocations = %d, want 1", ss.Invocations)
			}
		}
	}
	if !found {
		t.Error("disabled system not found in snapshot")
	}
}

// TestStatsSnapshot_RateFilteredSystem verifies that a rate-filtered system
// fires less often and that Invocations reflects the gate, not the tick count.
func TestStatsSnapshot_RateFilteredSystem(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[addonPos](w)
	q := flecs.NewCachedQuery(w, posID)
	noop := func(dt float32, it *flecs.QueryIter) {}

	gated := flecs.NewSystem(w, q, noop).SetName("gated")
	gated.SetRate(3) // fire every 3 ticks

	const ticks = 9
	for i := 0; i < ticks; i++ {
		w.Progress(0.016)
	}
	snap := w.StatsSnapshot()

	for _, ss := range snap.Systems {
		if ss.Name != "gated" {
			continue
		}
		want := uint64(ticks / 3)
		if ss.Invocations != want {
			t.Errorf("rate-gated Invocations = %d, want %d", ss.Invocations, want)
		}
		if ss.TotalSkipped != uint64(ticks)-want {
			t.Errorf("rate-gated TotalSkipped = %d, want %d", ss.TotalSkipped, uint64(ticks)-want)
		}
		return
	}
	t.Error("gated system not found in snapshot")
}

// TestStatsSnapshot_PhaseSystemCount verifies that each phase in the snapshot
// reports the correct active SystemCount.
func TestStatsSnapshot_PhaseSystemCount(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[addonPos](w)
	q := flecs.NewCachedQuery(w, posID)
	noop := func(dt float32, it *flecs.QueryIter) {}

	flecs.NewSystemInPhase(w, w.OnUpdate(), q, noop)
	flecs.NewSystemInPhase(w, w.OnUpdate(), q, noop)
	flecs.NewSystemInPhase(w, w.PostUpdate(), q, noop)

	w.Progress(0.016)
	snap := w.StatsSnapshot()

	found := map[string]int{}
	for _, ph := range snap.Phases {
		found[ph.Name] = ph.SystemCount
	}
	if found["OnUpdate"] != 2 {
		t.Errorf("OnUpdate SystemCount = %d, want 2", found["OnUpdate"])
	}
	if found["PostUpdate"] != 1 {
		t.Errorf("PostUpdate SystemCount = %d, want 1", found["PostUpdate"])
	}
	if found["PreUpdate"] != 0 {
		t.Errorf("PreUpdate SystemCount = %d, want 0", found["PreUpdate"])
	}
}

// TestStatsSnapshot_Concurrent verifies that concurrent StatsSnapshot calls
// during Progress are race-clean (run with -race to validate).
func TestStatsSnapshot_Concurrent(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[addonPos](w)
	q := flecs.NewCachedQuery(w, posID)

	flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		time.Sleep(time.Millisecond) // slow enough for concurrent readers
	})

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Background goroutine: continuously read snapshots.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = w.StatsSnapshot()
			}
		}
	}()

	// Run several ticks from the main goroutine.
	for i := 0; i < 10; i++ {
		w.Progress(0.016)
	}

	close(stop)
	wg.Wait()
}

// TestStatsSnapshot_IsSnapshot verifies that a retained PipelineStats value
// does not reflect subsequent Progress calls.
func TestStatsSnapshot_IsSnapshot(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[addonPos](w)
	q := flecs.NewCachedQuery(w, posID)
	noop := func(dt float32, it *flecs.QueryIter) {}
	flecs.NewSystem(w, q, noop)

	w.Progress(0.016)
	snap1 := w.StatsSnapshot()
	fc1 := snap1.World.FrameCount
	inv1 := uint64(0)
	if len(snap1.Systems) > 0 {
		inv1 = snap1.Systems[0].Invocations
	}

	w.Progress(0.016)
	w.Progress(0.016)

	// snap1 must not have changed.
	if snap1.World.FrameCount != fc1 {
		t.Errorf("snapshot mutated: FrameCount changed from %d to %d", fc1, snap1.World.FrameCount)
	}
	if len(snap1.Systems) > 0 && snap1.Systems[0].Invocations != inv1 {
		t.Errorf("snapshot mutated: Invocations changed from %d to %d", inv1, snap1.Systems[0].Invocations)
	}

	// A fresh snapshot must show the updated values.
	snap2 := w.StatsSnapshot()
	if snap2.World.FrameCount != 3 {
		t.Errorf("snap2 FrameCount = %d, want 3", snap2.World.FrameCount)
	}
	if len(snap2.Systems) > 0 && snap2.Systems[0].Invocations != 3 {
		t.Errorf("snap2 Invocations = %d, want 3", snap2.Systems[0].Invocations)
	}
}

// TestStatsSnapshot_CumulativePhaseInvocations verifies that phase Invocations
// increments each Progress call.
func TestStatsSnapshot_CumulativePhaseInvocations(t *testing.T) {
	w := flecs.New()
	const n = 4
	for i := 0; i < n; i++ {
		w.Progress(0)
	}
	snap := w.StatsSnapshot()
	for _, ph := range snap.Phases {
		if ph.Invocations != n {
			t.Errorf("phase %q Invocations = %d, want %d", ph.Name, ph.Invocations, n)
		}
	}
}

// TestStatsSnapshot_SetName verifies that SetName overrides the auto-generated name.
func TestStatsSnapshot_SetName(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[addonPos](w)
	q := flecs.NewCachedQuery(w, posID)
	noop := func(dt float32, it *flecs.QueryIter) {}

	flecs.NewSystem(w, q, noop).SetName("my-physics-system")
	w.Progress(0.016)
	snap := w.StatsSnapshot()

	if len(snap.Systems) != 1 {
		t.Fatalf("Systems: got %d, want 1", len(snap.Systems))
	}
	if snap.Systems[0].Name != "my-physics-system" {
		t.Errorf("Name = %q, want %q", snap.Systems[0].Name, "my-physics-system")
	}
}
