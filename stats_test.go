package flecs_test

import (
	"testing"
	"time"

	"github.com/snichols/flecs"
)

type statPos struct{ X, Y float32 }
type statVel struct{ DX, DY float32 }

func TestStats_EmptyWorld(t *testing.T) {
	w := flecs.New()
	s := w.Stats()

	if want := w.Count(); s.EntityCount != want {
		t.Errorf("EntityCount: got %d, want %d", s.EntityCount, want)
	}
	if s.TableCount < 1 {
		t.Errorf("TableCount: got %d, want >= 1", s.TableCount)
	}
	if s.QueryCount != 0 {
		t.Errorf("QueryCount: got %d, want 0", s.QueryCount)
	}
	if s.CachedQueryCount != 0 {
		t.Errorf("CachedQueryCount: got %d, want 0", s.CachedQueryCount)
	}
	if s.SystemCount != 0 {
		t.Errorf("SystemCount: got %d, want 0", s.SystemCount)
	}
	if s.FrameCount != 0 {
		t.Errorf("FrameCount: got %d, want 0", s.FrameCount)
	}
	if s.Time != 0 {
		t.Errorf("Time: got %f, want 0", s.Time)
	}
	if len(s.LastFramePhases) != 0 {
		t.Errorf("LastFramePhases: got len %d, want 0", len(s.LastFramePhases))
	}

	// ComponentStats should include the built-in Name component.
	found := false
	for _, cs := range s.ComponentStats {
		if cs.Name == "flecs.Name" {
			found = true
			break
		}
	}
	if !found {
		t.Error("ComponentStats: did not find built-in Name component")
	}
}

func TestStats_AfterEntitiesAndComponents(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[statPos](w)
	_ = posID

	e1 := w.NewEntity()
	flecs.Set(w.W(), e1, statPos{1, 2})
	e2 := w.NewEntity()
	flecs.Set(w.W(), e2, statPos{3, 4})

	s := w.Stats()
	// EntityCount includes builtins + registered component + 2 user entities
	if s.EntityCount != w.Count() {
		t.Errorf("EntityCount: got %d, want %d", s.EntityCount, w.Count())
	}
	// At least 2 tables: empty + {statPos}
	if s.TableCount < 2 {
		t.Errorf("TableCount: got %d, want >= 2", s.TableCount)
	}
}

func TestStats_AfterProgress(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[statPos](w)
	q := flecs.NewCachedQuery(w, posID)
	flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
		}
	})

	const dt float32 = 1.0 / 60.0
	w.Progress(dt)

	s := w.Stats()
	if s.FrameCount != 1 {
		t.Errorf("FrameCount: got %d, want 1", s.FrameCount)
	}
	if s.Time != float64(dt) {
		t.Errorf("Time: got %v, want %v", s.Time, float64(dt))
	}
	if len(s.LastFramePhases) != 4 {
		t.Fatalf("LastFramePhases: got len %d, want 4", len(s.LastFramePhases))
	}
	// System registered in OnUpdate (index 2); others have no systems.
	for i, phase := range s.LastFramePhases {
		if i == 2 {
			if phase.Duration <= 0 {
				t.Errorf("LastFramePhases[%d] (%s): Duration %v <= 0 with system", i, phase.Name, phase.Duration)
			}
			if phase.SystemCount != 1 {
				t.Errorf("LastFramePhases[%d] (%s): SystemCount %d, want 1", i, phase.Name, phase.SystemCount)
			}
		} else {
			if phase.Duration != 0 {
				t.Errorf("LastFramePhases[%d] (%s): Duration %v, want 0 (no systems)", i, phase.Name, phase.Duration)
			}
			if phase.SystemCount != 0 {
				t.Errorf("LastFramePhases[%d] (%s): SystemCount %d, want 0", i, phase.Name, phase.SystemCount)
			}
		}
	}
}

func TestStats_PhaseNames(t *testing.T) {
	w := flecs.New()
	w.Progress(0)
	s := w.Stats()
	if len(s.LastFramePhases) != 4 {
		t.Fatalf("LastFramePhases len %d, want 4", len(s.LastFramePhases))
	}
	want := []string{"PreUpdate", "OnFixedUpdate", "OnUpdate", "PostUpdate"}
	for i, name := range want {
		if s.LastFramePhases[i].Name != name {
			t.Errorf("LastFramePhases[%d].Name = %q, want %q", i, s.LastFramePhases[i].Name, name)
		}
	}
}

func TestStats_MultiFrameAccumulation(t *testing.T) {
	w := flecs.New()

	const dt float32 = 0.1
	const n = 5
	for i := 0; i < n; i++ {
		w.Progress(dt)
	}

	s := w.Stats()
	if s.FrameCount != n {
		t.Errorf("FrameCount: got %d, want %d", s.FrameCount, n)
	}
	want := float64(float32(n) * dt)
	if s.Time != want {
		t.Errorf("Time: got %v, want %v", s.Time, want)
	}
}

func TestStats_PerPhaseTiming(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[statPos](w)
	q := flecs.NewCachedQuery(w, posID)
	flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(dt float32, it *flecs.QueryIter) {
		time.Sleep(2 * time.Millisecond)
		for it.Next() {
		}
	})

	w.Progress(0.016)
	s := w.Stats()
	if len(s.LastFramePhases) < 3 {
		t.Fatalf("LastFramePhases too short")
	}
	if s.LastFramePhases[2].Duration <= 0 {
		t.Errorf("OnUpdate Duration %v, want > 0", s.LastFramePhases[2].Duration)
	}
}

func TestStats_PerComponentTableCount(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[statPos](w)
	velID := flecs.RegisterComponent[statVel](w)

	// Position-only entity
	e1 := w.NewEntity()
	flecs.Set(w.W(), e1, statPos{})

	// Position + Velocity entity
	e2 := w.NewEntity()
	flecs.Set(w.W(), e2, statPos{})
	flecs.Set(w.W(), e2, statVel{})

	// Velocity-only entity
	e3 := w.NewEntity()
	flecs.Set(w.W(), e3, statVel{})

	_ = e1
	_ = e2
	_ = e3

	s := w.Stats()

	var posStat, velStat *flecs.ComponentStat
	for i := range s.ComponentStats {
		cs := &s.ComponentStats[i]
		if cs.ID == posID {
			posStat = cs
		}
		if cs.ID == velID {
			velStat = cs
		}
	}
	if posStat == nil {
		t.Fatal("Position not found in ComponentStats")
	}
	if velStat == nil {
		t.Fatal("Velocity not found in ComponentStats")
	}
	if posStat.TableCount != 2 {
		t.Errorf("Position.TableCount = %d, want 2", posStat.TableCount)
	}
	if velStat.TableCount != 2 {
		t.Errorf("Velocity.TableCount = %d, want 2", velStat.TableCount)
	}
}

func TestStats_CachedQueryCount(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[statPos](w)

	cq1 := flecs.NewCachedQuery(w, posID)
	cq2 := flecs.NewCachedQuery(w, posID)
	cq3 := flecs.NewCachedQuery(w, posID)
	_ = cq1
	_ = cq3

	if s := w.Stats(); s.CachedQueryCount != 3 {
		t.Errorf("CachedQueryCount: got %d, want 3", s.CachedQueryCount)
	}

	cq2.Close()
	if s := w.Stats(); s.CachedQueryCount != 2 {
		t.Errorf("CachedQueryCount after Close: got %d, want 2", s.CachedQueryCount)
	}
}

func TestStats_SystemCountAndInPhase(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[statPos](w)
	q := flecs.NewCachedQuery(w, posID)
	noop := func(dt float32, it *flecs.QueryIter) {}

	flecs.NewSystemInPhase(w, w.OnUpdate(), q, noop)
	flecs.NewSystemInPhase(w, w.OnUpdate(), q, noop)
	flecs.NewSystemInPhase(w, w.PreUpdate(), q, noop)

	s := w.Stats()
	if s.SystemCount != 3 {
		t.Errorf("SystemCount: got %d, want 3", s.SystemCount)
	}
	if n := w.SystemCountInPhase(w.OnUpdate()); n != 2 {
		t.Errorf("SystemCountInPhase(OnUpdate): got %d, want 2", n)
	}
	if n := w.SystemCountInPhase(w.PreUpdate()); n != 1 {
		t.Errorf("SystemCountInPhase(PreUpdate): got %d, want 1", n)
	}
	if n := w.SystemCountInPhase(w.PostUpdate()); n != 0 {
		t.Errorf("SystemCountInPhase(PostUpdate): got %d, want 0", n)
	}
}

func TestStats_SystemCountInPhaseInvalidPanic(t *testing.T) {
	w := flecs.New()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid phase ID")
		}
	}()
	w.SystemCountInPhase(w.ChildOf())
}

func TestStats_IsSnapshot(t *testing.T) {
	w := flecs.New()
	s1 := w.Stats()
	before := s1.EntityCount

	w.NewEntity()
	s2 := w.Stats()

	if s1.EntityCount != before {
		t.Errorf("snapshot mutated: EntityCount changed from %d to %d", before, s1.EntityCount)
	}
	if s2.EntityCount != before+1 {
		t.Errorf("second snapshot EntityCount = %d, want %d", s2.EntityCount, before+1)
	}
}

func TestStats_TableCount(t *testing.T) {
	w := flecs.New()
	s0 := w.Stats()
	initial := s0.TableCount

	posID := flecs.RegisterComponent[statPos](w)
	e := w.NewEntity()
	flecs.Set(w.W(), e, statPos{})
	_ = posID

	s1 := w.Stats()
	if s1.TableCount <= initial {
		t.Errorf("TableCount did not increase after adding component: %d -> %d", initial, s1.TableCount)
	}
}
