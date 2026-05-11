package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// ── Initial state ─────────────────────────────────────────────────────────────

func TestChangedInitialStateTrue(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	e := w.NewEntity()
	flecs.Set(w, e, Position{X: 1})

	cq := flecs.NewCachedQuery(w, posID)
	defer cq.Close()

	if !cq.Changed() {
		t.Fatal("first call should return true")
	}
	if cq.Changed() {
		t.Fatal("second call with no changes should return false")
	}
}

// ── New table appears post-construction ──────────────────────────────────────

func TestChangedNewTablePostConstruction(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	cq := flecs.NewCachedQuery(w, posID)
	defer cq.Close()

	// No tables yet; first Changed() should be false (empty match set).
	if cq.Changed() {
		t.Fatal("empty match set: first call should return false")
	}

	// Creating an entity with Position creates a new matching table.
	e := w.NewEntity()
	flecs.Set(w, e, Position{X: 1})

	if !cq.Changed() {
		t.Fatal("new matching table: Changed() should return true")
	}
	if cq.Changed() {
		t.Fatal("no further changes: Changed() should return false")
	}
}

// ── Column write same archetype ───────────────────────────────────────────────

func TestChangedColumnWriteSameArchetype(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	e := w.NewEntity()
	flecs.Set(w, e, Position{X: 1})

	cq := flecs.NewCachedQuery(w, posID)
	defer cq.Close()

	// Consume initial true.
	cq.Changed()

	// Write to an entity that already has Position (no migration).
	flecs.Set(w, e, Position{X: 2})
	if !cq.Changed() {
		t.Fatal("column write: Changed() should return true")
	}
	if cq.Changed() {
		t.Fatal("no further changes: Changed() should return false")
	}
}

// ── Append into already-matching archetype ────────────────────────────────────

func TestChangedAppendNewEntity(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	e := w.NewEntity()
	flecs.Set(w, e, Position{X: 1})

	cq := flecs.NewCachedQuery(w, posID)
	defer cq.Close()

	// Consume initial true.
	cq.Changed()

	// Add a second entity to the same [Position] table.
	e2 := w.NewEntity()
	flecs.Set(w, e2, Position{X: 2})

	if !cq.Changed() {
		t.Fatal("new entity in matching table: Changed() should return true")
	}
	if cq.Changed() {
		t.Fatal("no further changes: Changed() should return false")
	}
}

// ── Migrate out of matching archetype ────────────────────────────────────────

func TestChangedMigrateOutOfMatchingArchetype(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	_ = flecs.RegisterComponent[Velocity](w)

	e := w.NewEntity()
	flecs.Set(w, e, Position{X: 1})

	cq := flecs.NewCachedQuery(w, posID)
	defer cq.Close()

	// Consume initial true.
	cq.Changed()

	// Adding Velocity migrates e from [Pos] to [Pos,Vel].
	// Source table [Pos] gets a RemoveSwap → changeCount bumped.
	flecs.Set(w, e, Velocity{DX: 1})

	if !cq.Changed() {
		t.Fatal("migration out of matching table: Changed() should return true")
	}
}

// ── Delete entity ─────────────────────────────────────────────────────────────

func TestChangedDeleteEntity(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	e := w.NewEntity()
	flecs.Set(w, e, Position{X: 1})
	e2 := w.NewEntity()
	flecs.Set(w, e2, Position{X: 2})

	cq := flecs.NewCachedQuery(w, posID)
	defer cq.Close()

	// Consume initial true.
	cq.Changed()

	w.Delete(e)

	if !cq.Changed() {
		t.Fatal("entity delete (RemoveSwap): Changed() should return true")
	}
}

// ── Multiple Sets coalesce ────────────────────────────────────────────────────

func TestChangedMultipleSetsCoalesce(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	e := w.NewEntity()
	flecs.Set(w, e, Position{X: 1})

	cq := flecs.NewCachedQuery(w, posID)
	defer cq.Close()

	// Consume initial true.
	cq.Changed()

	// Multiple writes — should all coalesce into one Changed()=true.
	flecs.Set(w, e, Position{X: 2})
	flecs.Set(w, e, Position{X: 3})
	flecs.Set(w, e, Position{X: 4})

	if !cq.Changed() {
		t.Fatal("multiple sets: Changed() should return true")
	}
	if cq.Changed() {
		t.Fatal("after coalesced changes consumed: Changed() should return false")
	}
}

// ── Two queries — write hits only the matching one ───────────────────────────

func TestChangedTwoQueriesCrossIndependence(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	// Create a [Pos]-only entity and a [Pos,Vel] entity.
	ePos := w.NewEntity()
	flecs.Set(w, ePos, Position{X: 1})

	ePosVel := w.NewEntity()
	flecs.Set(w, ePosVel, Position{X: 2})
	flecs.Set(w, ePosVel, Velocity{DX: 1})

	q1 := flecs.NewCachedQuery(w, posID)        // matches [Pos] and [Pos,Vel]
	q2 := flecs.NewCachedQuery(w, posID, velID) // matches only [Pos,Vel]
	defer q1.Close()
	defer q2.Close()

	// Consume initial true for both.
	q1.Changed()
	q2.Changed()

	// Write to ePos — only the [Pos] table is dirty.
	flecs.Set(w, ePos, Position{X: 99})

	if !q1.Changed() {
		t.Fatal("q1 (matches [Pos]): should detect the column write on [Pos] table")
	}
	if q2.Changed() {
		t.Fatal("q2 (only [Pos,Vel]): should NOT detect write on [Pos]-only table")
	}
}

// ── Cross-query independence ──────────────────────────────────────────────────

func TestChangedCrossQueryIndependence(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	e := w.NewEntity()
	flecs.Set(w, e, Position{X: 1})

	q1 := flecs.NewCachedQuery(w, posID)
	q2 := flecs.NewCachedQuery(w, posID)
	defer q1.Close()
	defer q2.Close()

	// Consume initial true for both.
	q1.Changed()
	q2.Changed()

	// Mutate — both should see it, independently.
	flecs.Set(w, e, Position{X: 2})
	if !q1.Changed() {
		t.Fatal("q1: should see the change")
	}
	if !q2.Changed() {
		t.Fatal("q2: should see the change independently")
	}

	// Both consumed — neither reports another change.
	if q1.Changed() {
		t.Fatal("q1: no further changes")
	}
	if q2.Changed() {
		t.Fatal("q2: no further changes")
	}
}

// ── Changed() after Close() ───────────────────────────────────────────────────

func TestChangedAfterClose(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	e := w.NewEntity()
	flecs.Set(w, e, Position{X: 1})

	cq := flecs.NewCachedQuery(w, posID)
	cq.Close()

	// Changed() on a closed query should be safe and return false.
	if cq.Changed() {
		t.Fatal("Changed() after Close() should return false")
	}
}

// ── SetPair triggers Changed ──────────────────────────────────────────────────

func TestChangedSetPairTriggersChanged(t *testing.T) {
	w := flecs.New()
	rel := w.NewEntity()

	e := w.NewEntity()
	flecs.SetPair[Edge](w, e, rel, e, Edge{Weight: 1.0})

	pairID := flecs.MakePair(rel, e)
	cq := flecs.NewCachedQuery(w, pairID)
	defer cq.Close()

	// Consume initial true.
	cq.Changed()

	// In-place pair write.
	flecs.SetPair[Edge](w, e, rel, e, Edge{Weight: 2.0})
	if !cq.Changed() {
		t.Fatal("SetPair column write: Changed() should return true")
	}
	if cq.Changed() {
		t.Fatal("no further changes: Changed() should return false")
	}
}

// ── Defer block triggers Changed after flush ──────────────────────────────────

func TestChangedDeferBlockTriggersAfterFlush(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	e := w.NewEntity()
	flecs.Set(w, e, Position{X: 1})

	cq := flecs.NewCachedQuery(w, posID)
	defer cq.Close()

	// Consume initial true.
	cq.Changed()

	// Queue a write inside Defer — it should not trigger Changed until flushed.
	w.DeferBegin()
	flecs.Set(w, e, Position{X: 2})
	// Still inside defer: not flushed yet. Changed() should be false.
	if cq.Changed() {
		t.Fatal("inside Defer: write not yet flushed, Changed() should be false")
	}
	w.DeferEnd() // flush

	if !cq.Changed() {
		t.Fatal("after DeferEnd flush: Changed() should return true")
	}
}
