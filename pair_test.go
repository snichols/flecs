package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// Component types for pair tests.
type Edge struct{ Weight float32 }
type Color struct{ R, G, B uint8 }

// ── AddID / HasID / RemoveID ──────────────────────────────────────────────────

func TestAddHasRemoveIDWithComponentID(t *testing.T) {
	// AddID/HasID/RemoveID with a regular component ID behave like
	// Set[T]/Has[T]/Remove[T]; AddID writes a zero value for the column.
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	e := w.NewEntity()

	added := flecs.AddID(w.W(), e, posID)
	if !added {
		t.Fatal("AddID returned false on first add")
	}
	if !flecs.HasID(w.R(), e, posID) {
		t.Fatal("HasID false after AddID")
	}
	// Column was zero-initialised; Get should find a zero Position.
	pos, ok := flecs.Get[Position](w.R(), e)
	if !ok {
		t.Fatal("Get[Position] not found after AddID")
	}
	if pos.X != 0 || pos.Y != 0 {
		t.Fatalf("expected zero Position after AddID, got %+v", pos)
	}
	// Has[T] agrees with HasID.
	if !flecs.Has[Position](w.R(), e) {
		t.Fatal("Has[Position] false when HasID is true")
	}
	// Idempotent: second AddID returns false.
	if flecs.AddID(w.W(), e, posID) {
		t.Fatal("AddID returned true on second call (not idempotent)")
	}

	if !flecs.RemoveID(w.W(), e, posID) {
		t.Fatal("RemoveID returned false when component was present")
	}
	if flecs.HasID(w.R(), e, posID) {
		t.Fatal("HasID true after RemoveID")
	}
	// Double remove returns false.
	if flecs.RemoveID(w.W(), e, posID) {
		t.Fatal("RemoveID returned true for absent component")
	}
}

func TestAddHasRemoveIDWithRawEntityTag(t *testing.T) {
	w := flecs.New()
	tagEnt := w.NewEntity() // raw entity used as a tag
	e := w.NewEntity()

	if !flecs.AddID(w.W(), e, tagEnt) {
		t.Fatal("AddID with raw entity tag returned false")
	}
	if !flecs.HasID(w.R(), e, tagEnt) {
		t.Fatal("HasID false after AddID with entity tag")
	}
	if !flecs.RemoveID(w.W(), e, tagEnt) {
		t.Fatal("RemoveID returned false when entity tag was present")
	}
	if flecs.HasID(w.R(), e, tagEnt) {
		t.Fatal("HasID true after RemoveID entity tag")
	}
}

func TestAddHasRemoveIDWithPairID(t *testing.T) {
	w := flecs.New()
	rel := w.NewEntity()
	tgt := w.NewEntity()
	child := w.NewEntity()

	pairID := flecs.MakePair(rel, tgt)

	if !flecs.AddID(w.W(), child, pairID) {
		t.Fatal("AddID with pair ID returned false on first add")
	}
	if !flecs.HasID(w.R(), child, pairID) {
		t.Fatal("HasID false after AddID with pair")
	}

	// Entity's table signature contains the pair id.
	tbl := flecs.TableOf(w, child)
	found := false
	for _, id := range tbl.Type() {
		if id == pairID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("entity signature does not contain pair id; sig=%v", tbl.Type())
	}

	if !flecs.RemoveID(w.W(), child, pairID) {
		t.Fatal("RemoveID returned false when pair was present")
	}
	if flecs.HasID(w.R(), child, pairID) {
		t.Fatal("HasID true after RemoveID pair")
	}
}

func TestDistinctPairTargetsDistinctArchetypes(t *testing.T) {
	w := flecs.New()
	rel := w.NewEntity()
	a := w.NewEntity()
	b := w.NewEntity()
	e1 := w.NewEntity()
	e2 := w.NewEntity()

	flecs.AddID(w.W(), e1, flecs.MakePair(rel, a))
	flecs.AddID(w.W(), e2, flecs.MakePair(rel, b))

	if flecs.TableOf(w, e1) == flecs.TableOf(w, e2) {
		t.Fatal("distinct pair targets must produce distinct archetypes")
	}
}

func TestAddIDOnDeadEntityPanics(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	w.Delete(e)
	tag := w.NewEntity()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("AddID on dead entity should panic, got none")
		}
	}()
	flecs.AddID(w.W(), e, tag)
}

func TestRemoveIDOnDeadEntityReturnsFalse(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	tag := w.NewEntity()
	flecs.AddID(w.W(), e, tag)
	w.Delete(e)

	if flecs.RemoveID(w.W(), e, tag) {
		t.Fatal("RemoveID on dead entity should return false")
	}
}

func TestHasIDOnDeadEntityReturnsFalse(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	tag := w.NewEntity()
	flecs.AddID(w.W(), e, tag)
	w.Delete(e)

	if flecs.HasID(w.R(), e, tag) {
		t.Fatal("HasID on dead entity should return false")
	}
}

func TestSetPairOnDeadEntityPanics(t *testing.T) {
	w := flecs.New()
	rel := w.NewEntity()
	tgt := w.NewEntity()
	e := w.NewEntity()
	w.Delete(e)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("SetPair on dead entity should panic, got none")
		}
	}()
	flecs.SetPair[Edge](w.W(), e, rel, tgt, Edge{Weight: 1.0})
}

func TestGetPairOnDeadEntityReturnsFalse(t *testing.T) {
	w := flecs.New()
	rel := w.NewEntity()
	tgt := w.NewEntity()
	e := w.NewEntity()
	e2 := w.NewEntity()

	// Register the pair by setting on e2.
	flecs.SetPair[Edge](w.W(), e2, rel, tgt, Edge{Weight: 1.0})

	// Delete e and verify GetPair returns (zero, false).
	w.Delete(e)
	v, ok := flecs.GetPair[Edge](w.R(), e, rel, tgt)
	if ok {
		t.Fatal("GetPair on dead entity should return false")
	}
	if v != (Edge{}) {
		t.Fatalf("GetPair on dead entity returned non-zero: %+v", v)
	}
}

func TestGetPairRegisteredButNotOnEntity(t *testing.T) {
	w := flecs.New()
	rel := w.NewEntity()
	tgt := w.NewEntity()
	e1 := w.NewEntity()
	e2 := w.NewEntity()

	// Register the pair on e1; e2 does not have it.
	flecs.SetPair[Edge](w.W(), e1, rel, tgt, Edge{Weight: 1.0})

	v, ok := flecs.GetPair[Edge](w.R(), e2, rel, tgt)
	if ok {
		t.Fatal("GetPair should return false when entity lacks the pair")
	}
	if v != (Edge{}) {
		t.Fatalf("GetPair returned non-zero: %+v", v)
	}
}

func TestNoPairLeakage(t *testing.T) {
	w := flecs.New()
	rel := w.NewEntity()
	a := w.NewEntity()
	b := w.NewEntity()
	e := w.NewEntity()

	flecs.AddID(w.W(), e, flecs.MakePair(rel, a))

	if flecs.HasID(w.R(), e, flecs.MakePair(rel, b)) {
		t.Fatal("adding (rel,a) leaked into (rel,b)")
	}
	if flecs.HasID(w.R(), e, rel) {
		t.Fatal("adding (rel,a) leaked into rel standalone")
	}
}

// ── SetPair / GetPair ─────────────────────────────────────────────────────────

func TestSetPairGetPairRoundTrip(t *testing.T) {
	w := flecs.New()
	rel := w.NewEntity()
	tgt := w.NewEntity()
	e := w.NewEntity()

	flecs.SetPair[Edge](w.W(), e, rel, tgt, Edge{Weight: 1.5})

	v, ok := flecs.GetPair[Edge](w.R(), e, rel, tgt)
	if !ok {
		t.Fatal("GetPair returned false after SetPair")
	}
	if v.Weight != 1.5 {
		t.Fatalf("GetPair weight want 1.5, got %v", v.Weight)
	}
}

func TestPairReregistrationSameTypeIdempotent(t *testing.T) {
	w := flecs.New()
	rel := w.NewEntity()
	tgt := w.NewEntity()
	e := w.NewEntity()

	flecs.SetPair[Edge](w.W(), e, rel, tgt, Edge{Weight: 1.0})
	flecs.SetPair[Edge](w.W(), e, rel, tgt, Edge{Weight: 2.0}) // must not panic

	v, ok := flecs.GetPair[Edge](w.R(), e, rel, tgt)
	if !ok {
		t.Fatal("GetPair returned false after second SetPair")
	}
	if v.Weight != 2.0 {
		t.Fatalf("GetPair weight want 2.0, got %v", v.Weight)
	}
}

func TestPairReregistrationDifferentTypePanics(t *testing.T) {
	w := flecs.New()
	rel := w.NewEntity()
	tgt := w.NewEntity()
	e := w.NewEntity()

	flecs.SetPair[Edge](w.W(), e, rel, tgt, Edge{Weight: 1.0})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("SetPair with different type should panic, got none")
		}
	}()
	flecs.SetPair[Color](w.W(), e, rel, tgt, Color{R: 1})
}

func TestGetPairNotPresent(t *testing.T) {
	w := flecs.New()
	rel := w.NewEntity()
	tgt := w.NewEntity()
	e := w.NewEntity()

	v, ok := flecs.GetPair[Edge](w.R(), e, rel, tgt)
	if ok {
		t.Fatal("GetPair returned true when pair not present")
	}
	if v != (Edge{}) {
		t.Fatalf("GetPair returned non-zero: %+v", v)
	}
}

func TestGetPairTypeMismatch(t *testing.T) {
	w := flecs.New()
	rel := w.NewEntity()
	tgt := w.NewEntity()
	e := w.NewEntity()

	flecs.SetPair[Edge](w.W(), e, rel, tgt, Edge{Weight: 1.5})

	// GetPair with mismatched type returns (zero, false), does not panic.
	v, ok := flecs.GetPair[Color](w.R(), e, rel, tgt)
	if ok {
		t.Fatal("GetPair[Color] returned true for an Edge pair")
	}
	if v != (Color{}) {
		t.Fatalf("GetPair[Color] returned non-zero: %+v", v)
	}
}

func TestPairWithDataAndRegularComponentCoexist(t *testing.T) {
	w := flecs.New()
	rel := w.NewEntity()
	tgt := w.NewEntity()
	e := w.NewEntity()

	flecs.Set[Position](w.W(), e, Position{X: 1, Y: 2})
	flecs.SetPair[Edge](w.W(), e, rel, tgt, Edge{Weight: 3.0})

	pos, ok := flecs.Get[Position](w.R(), e)
	if !ok || pos.X != 1 || pos.Y != 2 {
		t.Fatalf("Position corrupted after SetPair: ok=%v, pos=%+v", ok, pos)
	}
	edge, ok := flecs.GetPair[Edge](w.R(), e, rel, tgt)
	if !ok || edge.Weight != 3.0 {
		t.Fatalf("Edge wrong after Set[Position]: ok=%v, edge=%+v", ok, edge)
	}
}

// ── Query with pair IDs ───────────────────────────────────────────────────────

func TestQueryForPairID(t *testing.T) {
	w := flecs.New()
	follows := w.NewEntity()
	target := w.NewEntity()
	pairID := flecs.MakePair(follows, target)

	e1 := w.NewEntity()
	e2 := w.NewEntity()
	e3 := w.NewEntity()

	flecs.SetPair[Edge](w.W(), e1, follows, target, Edge{Weight: 1.0})
	flecs.SetPair[Edge](w.W(), e2, follows, target, Edge{Weight: 2.0})
	flecs.SetPair[Edge](w.W(), e3, follows, target, Edge{Weight: 3.0})

	q := flecs.NewQuery(w, pairID)
	var weights []float32
	q.Each(func(it *flecs.QueryIter) {
		for _, edge := range flecs.Field[Edge](it, pairID) {
			weights = append(weights, edge.Weight)
		}
	})

	if len(weights) != 3 {
		t.Fatalf("expected 3 results, got %d: %v", len(weights), weights)
	}
	seen := map[float32]bool{}
	for _, w := range weights {
		seen[w] = true
	}
	for _, want := range []float32{1.0, 2.0, 3.0} {
		if !seen[want] {
			t.Errorf("weight %v missing from query results", want)
		}
	}
}
