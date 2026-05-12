package docs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// TestManual_WorldLifecycle verifies the world-creation and progress-loop snippet from Manual.md.
func TestManual_WorldLifecycle(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 0, Y: 0})
		flecs.Set(fw, e, Velocity{DX: 1, DY: 0})
	})

	// Register a system that applies velocity to position.
	q := flecs.NewCachedQuery(w, posID, velID)
	flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			positions := flecs.Field[Position](it, posID)
			velocities := flecs.Field[Velocity](it, velID)
			for i := range positions {
				positions[i].X += velocities[i].DX * dt
				positions[i].Y += velocities[i].DY * dt
			}
		}
	})

	w.Progress(1.0 / 60.0)

	// After one frame at 1/60 s, X should have advanced by DX*dt = 1/60.
	w.Read(func(r *flecs.Reader) {
		p, ok := flecs.Get[Position](r, e)
		if !ok {
			t.Fatal("Position should be present after progress")
		}
		const want = float32(1) / 60.0
		if p.X < want-1e-4 || p.X > want+1e-4 {
			t.Errorf("Position.X = %f, want ~%f", p.X, want)
		}
	})
}

// TestManual_WorldStateAccessors verifies FrameCount, Time, and IsAlive from Manual.md.
func TestManual_WorldStateAccessors(t *testing.T) {
	w := flecs.New()

	if w.FrameCount() != 0 {
		t.Errorf("FrameCount before Progress = %d, want 0", w.FrameCount())
	}
	if w.Time() != 0 {
		t.Errorf("Time before Progress = %f, want 0", w.Time())
	}

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	if !w.IsAlive(e) {
		t.Fatal("entity should be alive after creation")
	}

	w.Progress(0.016)

	if w.FrameCount() != 1 {
		t.Errorf("FrameCount after one Progress = %d, want 1", w.FrameCount())
	}
	if w.Time() < 0.015 {
		t.Errorf("Time after one Progress = %f, want ~0.016", w.Time())
	}

	w.Delete(e)
	if w.IsAlive(e) {
		t.Error("entity should not be alive after deletion")
	}
}

// TestManual_ReadScope verifies the w.Read pattern from Manual.md.
func TestManual_ReadScope(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()
	flecs.RegisterComponent[Position](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 3, Y: 7})
	})

	w.Read(func(r *flecs.Reader) {
		p, ok := flecs.Get[Position](r, e)
		if !ok {
			t.Fatal("Position should be present")
		}
		if p.X != 3 || p.Y != 7 {
			t.Errorf("Position = {%f, %f}, want {3, 7}", p.X, p.Y)
		}
	})
}

// TestManual_ExclusiveAccess verifies the ExclusiveAccessBegin/End pattern from Manual.md.
func TestManual_ExclusiveAccess(t *testing.T) {
	w := flecs.New()

	// Claim the world for the current goroutine; run a progress pass; release.
	w.ExclusiveAccessBegin("test-loop")
	w.Progress(0)
	w.ExclusiveAccessEnd(false)

	// After releasing, Write scopes work normally again.
	w.Write(func(fw *flecs.Writer) {
		_ = fw.NewEntity()
	})
}

// TestManual_WorkerCount verifies SetWorkerCount/WorkerCount from Manual.md.
func TestManual_WorkerCount(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	if w.WorkerCount() != 0 {
		t.Errorf("WorkerCount before SetWorkerCount = %d, want 0", w.WorkerCount())
	}

	w.SetWorkerCount(2)
	if w.WorkerCount() != 2 {
		t.Errorf("WorkerCount after SetWorkerCount(2) = %d, want 2", w.WorkerCount())
	}

	// Register a parallel system and run a frame.
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 0, Y: 0})
		flecs.Set(fw, e, Velocity{DX: 1, DY: 1})
		_ = e
	})

	q := flecs.NewCachedQuery(w, posID, velID)
	sys := flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			positions := flecs.Field[Position](it, posID)
			velocities := flecs.Field[Velocity](it, velID)
			for i := range positions {
				positions[i].X += velocities[i].DX * dt
				positions[i].Y += velocities[i].DY * dt
			}
		}
	})
	sys.SetParallel(true)

	w.Progress(1.0)

	// Restore to serial mode.
	w.SetWorkerCount(0)
	if w.WorkerCount() != 0 {
		t.Errorf("WorkerCount after SetWorkerCount(0) = %d, want 0", w.WorkerCount())
	}
}

// TestManual_Idempotence verifies that adding a component twice is idempotent.
func TestManual_Idempotence(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		fw.AddID(e, posID)
		fw.AddID(e, posID) // second add is a no-op
	})

	w.Read(func(r *flecs.Reader) {
		_, ok := flecs.Get[Position](r, e)
		if !ok {
			t.Fatal("Position should be present after double AddID")
		}
	})
}
