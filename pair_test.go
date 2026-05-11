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
	var e flecs.ID
	// Create entity and add component; mutations flush when scope exits.
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		added := flecs.AddID(fw, e, posID)
		if !added {
			t.Fatal("AddID returned false on first add")
		}
	})
	// AddID has been applied; verify it is visible.
	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, e, posID) {
			t.Fatal("HasID false after AddID")
		}
		// Column was zero-initialised; Get should find a zero Position.
		pos, ok := flecs.Get[Position](r, e)
		if !ok {
			t.Fatal("Get[Position] not found after AddID")
		}
		if pos.X != 0 || pos.Y != 0 {
			t.Fatalf("expected zero Position after AddID, got %+v", pos)
		}
		// Has[T] agrees with HasID.
		if !flecs.Has[Position](r, e) {
			t.Fatal("Has[Position] false when HasID is true")
		}
	})
	// Idempotent: second AddID on entity that already has the component returns false.
	w.Write(func(fw *flecs.Writer) {
		if flecs.AddID(fw, e, posID) {
			t.Fatal("AddID returned true on second call (not idempotent)")
		}
	})
	// Remove the component.
	w.Write(func(fw *flecs.Writer) {
		if !flecs.RemoveID(fw, e, posID) {
			t.Fatal("RemoveID returned false when component was present")
		}
	})
	// Verify removed.
	w.Read(func(r *flecs.Reader) {
		if flecs.HasID(r, e, posID) {
			t.Fatal("HasID true after RemoveID")
		}
	})
	// Double remove: component is absent, RemoveID should return false.
	w.Write(func(fw *flecs.Writer) {
		if flecs.RemoveID(fw, e, posID) {
			t.Fatal("RemoveID returned true for absent component")
		}
	})
}

func TestAddHasRemoveIDWithRawEntityTag(t *testing.T) {
	w := flecs.New()
	var tagEnt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tagEnt = fw.NewEntity() // raw entity used as a tag
		e = fw.NewEntity()
		if !flecs.AddID(fw, e, tagEnt) {
			t.Fatal("AddID with raw entity tag returned false")
		}
	})
	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, e, tagEnt) {
			t.Fatal("HasID false after AddID with entity tag")
		}
	})
	w.Write(func(fw *flecs.Writer) {
		if !flecs.RemoveID(fw, e, tagEnt) {
			t.Fatal("RemoveID returned false when entity tag was present")
		}
	})
	w.Read(func(r *flecs.Reader) {
		if flecs.HasID(r, e, tagEnt) {
			t.Fatal("HasID true after RemoveID entity tag")
		}
	})
}

func TestAddHasRemoveIDWithPairID(t *testing.T) {
	w := flecs.New()
	var rel, tgt, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		child = fw.NewEntity()
	})

	pairID := flecs.MakePair(rel, tgt)

	w.Write(func(fw *flecs.Writer) {
		if !flecs.AddID(fw, child, pairID) {
			t.Fatal("AddID with pair ID returned false on first add")
		}
	})
	// AddID flushed; verify pair is present.
	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, child, pairID) {
			t.Fatal("HasID false after AddID with pair")
		}
	})

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

	w.Write(func(fw *flecs.Writer) {
		if !flecs.RemoveID(fw, child, pairID) {
			t.Fatal("RemoveID returned false when pair was present")
		}
	})
	// RemoveID flushed; verify pair is gone.
	w.Read(func(r *flecs.Reader) {
		if flecs.HasID(r, child, pairID) {
			t.Fatal("HasID true after RemoveID pair")
		}
	})
}

func TestDistinctPairTargetsDistinctArchetypes(t *testing.T) {
	w := flecs.New()
	var rel, a, b, e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.AddID(fw, e1, flecs.MakePair(rel, a))
		flecs.AddID(fw, e2, flecs.MakePair(rel, b))
	})

	if flecs.TableOf(w, e1) == flecs.TableOf(w, e2) {
		t.Fatal("distinct pair targets must produce distinct archetypes")
	}
}

func TestAddIDOnDeadEntityPanics(t *testing.T) {
	w := flecs.New()
	var e, tag flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})
	w.Delete(e)
	w.Write(func(fw *flecs.Writer) {
		tag = fw.NewEntity()
	})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("AddID on dead entity should panic, got none")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, tag)
	})
}

func TestRemoveIDOnDeadEntityReturnsFalse(t *testing.T) {
	w := flecs.New()
	var e, tag flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		tag = fw.NewEntity()
		flecs.AddID(fw, e, tag)
	})
	w.Delete(e)

	var result bool
	w.Write(func(fw *flecs.Writer) {
		result = flecs.RemoveID(fw, e, tag)
	})
	if result {
		t.Fatal("RemoveID on dead entity should return false")
	}
}

func TestHasIDOnDeadEntityReturnsFalse(t *testing.T) {
	w := flecs.New()
	var e, tag flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		tag = fw.NewEntity()
		flecs.AddID(fw, e, tag)
	})
	w.Delete(e)

	var result bool
	w.Read(func(r *flecs.Reader) {
		result = flecs.HasID(r, e, tag)
	})
	if result {
		t.Fatal("HasID on dead entity should return false")
	}
}

func TestSetPairOnDeadEntityPanics(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
	})
	w.Delete(e)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("SetPair on dead entity should panic, got none")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.SetPair[Edge](fw, e, rel, tgt, Edge{Weight: 1.0})
	})
}

func TestGetPairOnDeadEntityReturnsFalse(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
		e2 = fw.NewEntity()

		// Register the pair by setting on e2.
		flecs.SetPair[Edge](fw, e2, rel, tgt, Edge{Weight: 1.0})
	})

	// Delete e and verify GetPair returns (zero, false).
	w.Delete(e)
	var v Edge
	var ok bool
	w.Read(func(r *flecs.Reader) {
		v, ok = flecs.GetPair[Edge](r, e, rel, tgt)
	})
	if ok {
		t.Fatal("GetPair on dead entity should return false")
	}
	if v != (Edge{}) {
		t.Fatalf("GetPair on dead entity returned non-zero: %+v", v)
	}
}

func TestGetPairRegisteredButNotOnEntity(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()

		// Register the pair on e1; e2 does not have it.
		flecs.SetPair[Edge](fw, e1, rel, tgt, Edge{Weight: 1.0})
	})

	var v Edge
	var ok bool
	w.Read(func(r *flecs.Reader) {
		v, ok = flecs.GetPair[Edge](r, e2, rel, tgt)
	})
	if ok {
		t.Fatal("GetPair should return false when entity lacks the pair")
	}
	if v != (Edge{}) {
		t.Fatalf("GetPair returned non-zero: %+v", v)
	}
}

func TestNoPairLeakage(t *testing.T) {
	w := flecs.New()
	var rel, a, b, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(rel, a))
	})

	w.Read(func(r *flecs.Reader) {
		if flecs.HasID(r, e, flecs.MakePair(rel, b)) {
			t.Fatal("adding (rel,a) leaked into (rel,b)")
		}
		if flecs.HasID(r, e, rel) {
			t.Fatal("adding (rel,a) leaked into rel standalone")
		}
	})
}

// ── SetPair / GetPair ─────────────────────────────────────────────────────────

func TestSetPairGetPairRoundTrip(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
		flecs.SetPair[Edge](fw, e, rel, tgt, Edge{Weight: 1.5})
	})

	var v Edge
	var ok bool
	w.Read(func(r *flecs.Reader) {
		v, ok = flecs.GetPair[Edge](r, e, rel, tgt)
	})
	if !ok {
		t.Fatal("GetPair returned false after SetPair")
	}
	if v.Weight != 1.5 {
		t.Fatalf("GetPair weight want 1.5, got %v", v.Weight)
	}
}

func TestPairReregistrationSameTypeIdempotent(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
		flecs.SetPair[Edge](fw, e, rel, tgt, Edge{Weight: 1.0})
		flecs.SetPair[Edge](fw, e, rel, tgt, Edge{Weight: 2.0}) // must not panic
	})

	var v Edge
	var ok bool
	w.Read(func(r *flecs.Reader) {
		v, ok = flecs.GetPair[Edge](r, e, rel, tgt)
	})
	if !ok {
		t.Fatal("GetPair returned false after second SetPair")
	}
	if v.Weight != 2.0 {
		t.Fatalf("GetPair weight want 2.0, got %v", v.Weight)
	}
}

func TestPairReregistrationDifferentTypePanics(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
		flecs.SetPair[Edge](fw, e, rel, tgt, Edge{Weight: 1.0})
	})

	panicked := false
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		w.Write(func(fw *flecs.Writer) {
			flecs.SetPair[Color](fw, e, rel, tgt, Color{R: 1})
		})
	}()
	if !panicked {
		t.Fatal("SetPair with different type should panic, got none")
	}
}

func TestGetPairNotPresent(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
	})

	var v Edge
	var ok bool
	w.Read(func(r *flecs.Reader) {
		v, ok = flecs.GetPair[Edge](r, e, rel, tgt)
	})
	if ok {
		t.Fatal("GetPair returned true when pair not present")
	}
	if v != (Edge{}) {
		t.Fatalf("GetPair returned non-zero: %+v", v)
	}
}

func TestGetPairTypeMismatch(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
		flecs.SetPair[Edge](fw, e, rel, tgt, Edge{Weight: 1.5})
	})

	// GetPair with mismatched type returns (zero, false), does not panic.
	var v Color
	var ok bool
	w.Read(func(r *flecs.Reader) {
		v, ok = flecs.GetPair[Color](r, e, rel, tgt)
	})
	if ok {
		t.Fatal("GetPair[Color] returned true for an Edge pair")
	}
	if v != (Color{}) {
		t.Fatalf("GetPair[Color] returned non-zero: %+v", v)
	}
}

func TestPairWithDataAndRegularComponentCoexist(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
		flecs.Set[Position](fw, e, Position{X: 1, Y: 2})
		flecs.SetPair[Edge](fw, e, rel, tgt, Edge{Weight: 3.0})
	})

	var pos Position
	var posOk bool
	var edge Edge
	var edgeOk bool
	w.Read(func(r *flecs.Reader) {
		pos, posOk = flecs.Get[Position](r, e)
		edge, edgeOk = flecs.GetPair[Edge](r, e, rel, tgt)
	})
	if !posOk || pos.X != 1 || pos.Y != 2 {
		t.Fatalf("Position corrupted after SetPair: ok=%v, pos=%+v", posOk, pos)
	}
	if !edgeOk || edge.Weight != 3.0 {
		t.Fatalf("Edge wrong after Set[Position]: ok=%v, edge=%+v", edgeOk, edge)
	}
}

// ── Query with pair IDs ───────────────────────────────────────────────────────

func TestQueryForPairID(t *testing.T) {
	w := flecs.New()
	var follows, target flecs.ID
	w.Write(func(fw *flecs.Writer) {
		follows = fw.NewEntity()
		target = fw.NewEntity()
	})
	pairID := flecs.MakePair(follows, target)

	w.Write(func(fw *flecs.Writer) {
		e1 := fw.NewEntity()
		e2 := fw.NewEntity()
		e3 := fw.NewEntity()
		flecs.SetPair[Edge](fw, e1, follows, target, Edge{Weight: 1.0})
		flecs.SetPair[Edge](fw, e2, follows, target, Edge{Weight: 2.0})
		flecs.SetPair[Edge](fw, e3, follows, target, Edge{Weight: 3.0})
	})

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
