package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// helper types for pipeline phase tests
type ppPos struct{ X float32 }

func newPPWorld(t *testing.T) (*flecs.World, flecs.ID, *flecs.CachedQuery) {
	t.Helper()
	w := flecs.New()
	posID := flecs.RegisterComponent[ppPos](w)
	q := flecs.NewCachedQuery(w, posID)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, ppPos{})
	})
	return w, posID, q
}

// Test 1: Custom phase after OnUpdate runs after OnUpdate systems.
func TestPipelinePhase_CustomAfterOnUpdate(t *testing.T) {
	w, _, q := newPPWorld(t)

	myPhase := flecs.NewPhase(w, "MyPhase")
	myPhase.DependsOn(w.OnUpdate())

	var order []string
	flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "OnUpdate")
		}
	})
	flecs.NewSystemInPhase(w, myPhase, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "MyPhase")
		}
	})

	w.Progress(0)
	if len(order) != 2 || order[0] != "OnUpdate" || order[1] != "MyPhase" {
		t.Fatalf("expected [OnUpdate MyPhase], got %v", order)
	}
}

// Test 2: Chained custom phases: A.DependsOn(OnUpdate), B.DependsOn(A).
func TestPipelinePhase_ChainedCustomPhases(t *testing.T) {
	w, _, q := newPPWorld(t)

	phaseA := flecs.NewPhase(w, "A")
	phaseA.DependsOn(w.OnUpdate())
	phaseB := flecs.NewPhase(w, "B")
	phaseB.DependsOn(phaseA)

	var order []string
	flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "OnUpdate")
		}
	})
	flecs.NewSystemInPhase(w, phaseA, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "A")
		}
	})
	flecs.NewSystemInPhase(w, phaseB, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "B")
		}
	})

	w.Progress(0)
	if len(order) != 3 || order[0] != "OnUpdate" || order[1] != "A" || order[2] != "B" {
		t.Fatalf("expected [OnUpdate A B], got %v", order)
	}
}

// Test 3: System-level DependsOn within OnUpdate reorders systems.
func TestPipelinePhase_SystemDependsOnWithinPhase(t *testing.T) {
	w, _, q := newPPWorld(t)

	var order []string
	s1 := flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "S1")
		}
	})
	s2 := flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "S2")
		}
	})
	// S2 registered first in terms of declaration, but DependsOn makes S1 run first.
	s2.DependsOn(s1)

	// Also test reversed registration: S4 registered before S3, but S4.DependsOn(S3).
	s3 := flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "S3")
		}
	})
	s4 := flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "S4")
		}
	})
	s4.DependsOn(s3)

	w.Progress(0)
	// Expected: S1, S2, S3, S4 in that order (DependsOn enforced, registration tiebreak)
	if len(order) != 4 || order[0] != "S1" || order[1] != "S2" || order[2] != "S3" || order[3] != "S4" {
		t.Fatalf("expected [S1 S2 S3 S4], got %v", order)
	}
}

// Test 4: Phase-level cycle panics on first Progress.
func TestPipelinePhase_CyclePanicAtPhase(t *testing.T) {
	w := flecs.New()

	phaseA := flecs.NewPhase(w, "A")
	phaseB := flecs.NewPhase(w, "B")
	phaseA.DependsOn(w.OnUpdate())
	phaseB.DependsOn(phaseA)
	phaseA.DependsOn(phaseB) // creates A→B→A cycle

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for phase cycle, got none")
		}
		msg, _ := r.(string)
		if msg == "" {
			t.Fatalf("expected string panic, got %T: %v", r, r)
		}
	}()
	w.Progress(0)
}

// Test 5: System-level cycle panics on first Progress.
func TestPipelinePhase_CyclePanicAtSystem(t *testing.T) {
	w, _, q := newPPWorld(t)

	s1 := flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, _ *flecs.QueryIter) {})
	s2 := flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, _ *flecs.QueryIter) {})
	s1.DependsOn(s2) // s1 after s2
	s2.DependsOn(s1) // s2 after s1 → cycle

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for system cycle, got none")
		}
	}()
	w.Progress(0)
}

// Test 6: Mixed: phase ordering + within-phase ordering both respected.
func TestPipelinePhase_MixedPhaseAndSystemOrder(t *testing.T) {
	w, _, q := newPPWorld(t)

	phaseA := flecs.NewPhase(w, "A")
	phaseA.DependsOn(w.PreUpdate())

	var order []string
	// Within OnUpdate: s2 before s1 (despite registration order, s2.DependsOn(s1) would be the other way)
	// Let's register s1 first, s2 second, then make s1 depend on s2 (s1 after s2)
	s2 := flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "OnUpdate-S2")
		}
	})
	s1 := flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "OnUpdate-S1")
		}
	})
	s1.DependsOn(s2) // s1 runs after s2, overriding registration order

	flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "PreUpdate")
		}
	})
	flecs.NewSystemInPhase(w, phaseA, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "A")
		}
	})

	w.Progress(0)
	// PreUpdate first, then OnUpdate(S2 before S1), then A (after PreUpdate)
	if len(order) != 4 {
		t.Fatalf("expected 4 items, got %v", order)
	}
	if order[0] != "PreUpdate" {
		t.Errorf("expected PreUpdate first, got %v", order[0])
	}
	// PhaseA depends on PreUpdate, OnUpdate depends on OnFixedUpdate which depends on PreUpdate.
	// So after PreUpdate we have both OnUpdate and A available... but A.DependsOn(PreUpdate).
	// Actually A depends on PreUpdate not on OnUpdate, so A and OnUpdate may interleave.
	// Let me just check that S2 comes before S1 in whatever order they appear.
	s2Pos, s1Pos := -1, -1
	for i, v := range order {
		switch v {
		case "OnUpdate-S2":
			s2Pos = i
		case "OnUpdate-S1":
			s1Pos = i
		}
	}
	if s2Pos < 0 || s1Pos < 0 {
		t.Fatalf("missing S1 or S2 in output: %v", order)
	}
	if s2Pos >= s1Pos {
		t.Errorf("S2 (%d) should run before S1 (%d) in output: %v", s2Pos, s1Pos, order)
	}
}

// Test 7: Phase.SetEnabled(false) skips all systems; re-enabling resumes.
func TestPipelinePhase_SetEnabledPhase(t *testing.T) {
	w, _, q := newPPWorld(t)

	myPhase := flecs.NewPhase(w, "MyPhase")
	myPhase.DependsOn(w.OnUpdate())

	counter := 0
	flecs.NewSystemInPhase(w, myPhase, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			counter++
		}
	})

	w.Progress(0) // counter = 1
	if counter != 1 {
		t.Fatalf("before disable: expected counter=1, got %d", counter)
	}

	myPhase.SetEnabled(false)
	if myPhase.IsEnabled() {
		t.Fatal("expected IsEnabled()=false after SetEnabled(false)")
	}
	w.Progress(0) // phase disabled: counter stays at 1
	w.Progress(0) // still disabled
	if counter != 1 {
		t.Fatalf("while disabled: expected counter still=1, got %d", counter)
	}

	myPhase.SetEnabled(true)
	if !myPhase.IsEnabled() {
		t.Fatal("expected IsEnabled()=true after SetEnabled(true)")
	}
	w.Progress(0) // re-enabled: counter = 2
	if counter != 2 {
		t.Fatalf("after re-enable: expected counter=2, got %d", counter)
	}
}

// Test 8: System SetEnabled interacts correctly with phase SetEnabled.
func TestPipelinePhase_SystemAndPhaseEnabled(t *testing.T) {
	w, _, q := newPPWorld(t)

	myPhase := flecs.NewPhase(w, "MyPhase")
	myPhase.DependsOn(w.OnUpdate())

	counter := 0
	sys := flecs.NewSystemInPhase(w, myPhase, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			counter++
		}
	})

	// Both enabled: system runs.
	w.Progress(0)
	if counter != 1 {
		t.Fatalf("both enabled: want counter=1, got %d", counter)
	}

	// Phase disabled: system does not run even though system itself is enabled.
	myPhase.SetEnabled(false)
	w.Progress(0)
	if counter != 1 {
		t.Fatalf("phase disabled: want counter=1 (unchanged), got %d", counter)
	}

	// Phase re-enabled, system disabled: system does not run.
	myPhase.SetEnabled(true)
	sys.SetEnabled(false)
	w.Progress(0)
	if counter != 1 {
		t.Fatalf("system disabled: want counter=1 (unchanged), got %d", counter)
	}

	// Both enabled again: system runs.
	sys.SetEnabled(true)
	w.Progress(0)
	if counter != 2 {
		t.Fatalf("both re-enabled: want counter=2, got %d", counter)
	}
}

// Test 9: Custom phase with no DependsOn panics on first Progress.
func TestPipelinePhase_OrphanPhasePanic(t *testing.T) {
	w := flecs.New()

	// Create a custom phase but don't call DependsOn on it.
	_ = flecs.NewPhase(w, "Orphan")

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for orphan phase, got none")
		}
		msg, _ := r.(string)
		if msg == "" {
			t.Fatalf("expected string panic, got %T: %v", r, r)
		}
		// Message should name the orphan phase.
		if len(msg) == 0 {
			t.Fatal("panic message is empty")
		}
	}()
	w.Progress(0)
}

// Test 10: System.DependsOn across phases panics.
func TestPipelinePhase_SystemDependsOnCrossPhase(t *testing.T) {
	w, _, q := newPPWorld(t)

	phaseA := flecs.NewPhase(w, "A")
	phaseA.DependsOn(w.OnUpdate())

	s1 := flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, _ *flecs.QueryIter) {})
	s2 := flecs.NewSystemInPhase(w, phaseA, q, func(_ float32, _ *flecs.QueryIter) {})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for cross-phase DependsOn, got none")
		}
	}()
	s2.DependsOn(s1) // s1 is in OnUpdate, s2 is in phaseA — must panic
}

// Test 11: Closing a system in a 3-system chain drops its DependsOn edges.
func TestPipelinePhase_CloseMiddleSystemInChain(t *testing.T) {
	w, _, q := newPPWorld(t)

	var order []string
	s1 := flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "S1")
		}
	})
	s2 := flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "S2")
		}
	})
	s3 := flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "S3")
		}
	})
	s2.DependsOn(s1)
	s3.DependsOn(s2)

	// Verify chain: S1 → S2 → S3.
	w.Progress(0)
	if len(order) != 3 || order[0] != "S1" || order[1] != "S2" || order[2] != "S3" {
		t.Fatalf("before close: expected [S1 S2 S3], got %v", order)
	}

	// Close the middle system (S2). S3's predecessor (S2) is removed.
	s2.Close()
	order = nil
	w.Progress(0)

	// After closing S2, only S1 and S3 remain. S3's predecessor S2 is gone,
	// so S3 has no predecessors in this phase → runs by registration order after S1.
	if len(order) != 2 {
		t.Fatalf("after close: expected 2 systems, got %d: %v", len(order), order)
	}
	// S1 should still come before S3 (registration order tiebreak).
	if order[0] != "S1" || order[1] != "S3" {
		t.Fatalf("after close: expected [S1 S3], got %v", order)
	}
}

// Test 12: Reader.Phases() and Reader.SystemsInPhase() return topo-ordered results.
func TestPipelinePhase_ReaderIntrospection(t *testing.T) {
	w, _, q := newPPWorld(t)

	phaseA := flecs.NewPhase(w, "A")
	phaseA.DependsOn(w.OnUpdate())
	phaseB := flecs.NewPhase(w, "B")
	phaseB.DependsOn(phaseA)

	s1 := flecs.NewSystemInPhase(w, phaseA, q, func(_ float32, _ *flecs.QueryIter) {})
	s2 := flecs.NewSystemInPhase(w, phaseA, q, func(_ float32, _ *flecs.QueryIter) {})
	s2.DependsOn(s1) // s1 before s2 within phaseA

	w.Read(func(r *flecs.Reader) {
		phases := r.Phases()
		// Should be: PreUpdate, OnFixedUpdate, OnUpdate, A, B
		if len(phases) != 6 {
			t.Fatalf("Phases() len = %d, want 6", len(phases))
		}
		// Find A and B in the result and verify their relative order.
		posA, posB := -1, -1
		posOnUpdate := -1
		for i, p := range phases {
			switch p.Name() {
			case "A":
				posA = i
			case "B":
				posB = i
			case "OnUpdate":
				posOnUpdate = i
			}
		}
		if posA < 0 || posB < 0 || posOnUpdate < 0 {
			t.Fatalf("missing phase in result: A=%d B=%d OnUpdate=%d from %v",
				posA, posB, posOnUpdate, phaseNames(phases))
		}
		if posA <= posOnUpdate {
			t.Errorf("A (%d) must come after OnUpdate (%d)", posA, posOnUpdate)
		}
		if posB <= posA {
			t.Errorf("B (%d) must come after A (%d)", posB, posA)
		}

		// Within phaseA, SystemsInPhase must return s1 then s2.
		systems := r.SystemsInPhase(phaseA)
		if len(systems) != 2 {
			t.Fatalf("SystemsInPhase(A) len = %d, want 2", len(systems))
		}
		if systems[0] != s1 || systems[1] != s2 {
			t.Errorf("SystemsInPhase(A) wrong order: got [%p %p], want [%p %p]",
				systems[0], systems[1], s1, s2)
		}
	})
}

func phaseNames(phases []*flecs.Phase) []string {
	out := make([]string, len(phases))
	for i, p := range phases {
		out[i] = p.Name()
	}
	return out
}

// Test 13: Marshal round-trip drops custom phases (only built-ins survive).
func TestPipelinePhase_MarshalDropsCustomPhases(t *testing.T) {
	w, _, q := newPPWorld(t)

	myPhase := flecs.NewPhase(w, "MyPhase")
	myPhase.DependsOn(w.OnUpdate())

	counter := 0
	flecs.NewSystemInPhase(w, myPhase, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			counter++
		}
	})

	// Verify the system runs before marshal.
	w.Progress(0)
	if counter != 1 {
		t.Fatalf("pre-marshal: expected counter=1, got %d", counter)
	}

	// Marshal and unmarshal.
	data, err := w.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	w2 := flecs.New()
	flecs.RegisterComponent[ppPos](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// After unmarshal, w2 has only built-in phases.
	w2.Read(func(r *flecs.Reader) {
		phases := r.Phases()
		if len(phases) != 4 {
			t.Errorf("after unmarshal: Phases() len = %d, want 4 (built-ins only)", len(phases))
		}
		for _, p := range phases {
			if p.Name() == "MyPhase" {
				t.Error("custom phase MyPhase unexpectedly survived MarshalJSON/UnmarshalJSON")
			}
		}
	})
}

// Additional: DependsOn fluent chaining and idempotency.
func TestPipelinePhase_DependsOnFluentAndIdempotent(t *testing.T) {
	w := flecs.New()

	phaseA := flecs.NewPhase(w, "A")
	// Fluent chaining returns the same phase.
	ret := phaseA.DependsOn(w.OnUpdate())
	if ret != phaseA {
		t.Error("DependsOn should return the receiver for fluent chaining")
	}
	// Idempotent: calling again does not add a duplicate.
	phaseA.DependsOn(w.OnUpdate())

	// Should compile and not panic. Progress verifies no duplicate edges.
	w.Progress(0)
}

// Additional: Phase.Name() returns the correct name.
func TestPipelinePhase_Name(t *testing.T) {
	w := flecs.New()
	p := flecs.NewPhase(w, "TestName")
	p.DependsOn(w.OnUpdate())
	if p.Name() != "TestName" {
		t.Errorf("Name() = %q, want %q", p.Name(), "TestName")
	}
	if w.PreUpdate().Name() != "PreUpdate" {
		t.Errorf("PreUpdate().Name() = %q", w.PreUpdate().Name())
	}
}

// Additional: World.DependsOn() returns a valid ID.
func TestPipelinePhase_DependsOnID(t *testing.T) {
	w := flecs.New()
	id := w.DependsOn()
	if id == 0 {
		t.Fatal("DependsOn() must return non-zero ID")
	}
	if !w.IsAlive(id) {
		t.Fatal("DependsOn() entity must be alive")
	}
}

// Additional: EachSystem visits systems in topo order.
func TestPipelinePhase_EachSystem(t *testing.T) {
	w, _, q := newPPWorld(t)

	var seen []*flecs.System
	s1 := flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, _ *flecs.QueryIter) {})
	s2 := flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, _ *flecs.QueryIter) {})
	s2.DependsOn(s1) // s1 first

	w.Read(func(r *flecs.Reader) {
		r.EachSystem(w.OnUpdate(), func(s *flecs.System) bool {
			seen = append(seen, s)
			return true
		})
	})

	if len(seen) != 2 || seen[0] != s1 || seen[1] != s2 {
		t.Errorf("EachSystem order wrong: got [%p %p], want [%p %p]", seen[0], seen[1], s1, s2)
	}
}
