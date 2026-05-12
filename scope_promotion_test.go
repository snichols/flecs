package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// Shared test types for scope promotion tests.
type spPosition struct{ X, Y float32 }
type spVelocity struct{ DX, DY float32 }

// TestScopePromotion_Each2InsideWrite verifies that Each2 works inside a Write
// scope without AsReader().
func TestScopePromotion_Each2InsideWrite(t *testing.T) {
	w := flecs.New()

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.Set(fw, e1, spPosition{1, 2})
		flecs.Set(fw, e1, spVelocity{3, 4})
		flecs.Set(fw, e2, spPosition{5, 6})
		flecs.Set(fw, e2, spVelocity{7, 8})
	})

	var count int
	w.Write(func(fw *flecs.Writer) {
		flecs.Each2[spPosition, spVelocity](fw, func(e flecs.ID, p *spPosition, v *spVelocity) {
			p.X += v.DX
			p.Y += v.DY
			count++
		})
	})

	if count != 2 {
		t.Fatalf("expected 2 entities, got %d", count)
	}
	w.Read(func(r *flecs.Reader) {
		p1, _ := flecs.Get[spPosition](r, e1)
		if p1.X != 4 || p1.Y != 6 {
			t.Fatalf("e1 position: want {4 6}, got %v", p1)
		}
		p2, _ := flecs.Get[spPosition](r, e2)
		if p2.X != 12 || p2.Y != 14 {
			t.Fatalf("e2 position: want {12 14}, got %v", p2)
		}
	})
}

// TestScopePromotion_GetInsideWrite verifies that Get works inside a Write
// scope without AsReader().
func TestScopePromotion_GetInsideWrite(t *testing.T) {
	w := flecs.New()

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, spPosition{10, 20})
	})

	w.Write(func(fw *flecs.Writer) {
		p, ok := flecs.Get[spPosition](fw, e)
		if !ok {
			t.Fatal("Get[spPosition] inside Write scope returned false")
		}
		if p.X != 10 || p.Y != 20 {
			t.Fatalf("Get: want {10 20}, got %v", p)
		}
	})
}

// TestScopePromotion_MixedEachSetInsideWrite verifies that Each and Set can be
// mixed inside a single Write scope without AsReader().
func TestScopePromotion_MixedEachSetInsideWrite(t *testing.T) {
	w := flecs.New()

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, spPosition{1, 0})
		flecs.Set(fw, e, spVelocity{5, 0})
	})

	w.Write(func(fw *flecs.Writer) {
		// Read via Each2 (uses fw as scope, no AsReader), then mutate via Set.
		flecs.Each2[spPosition, spVelocity](fw, func(eid flecs.ID, p *spPosition, v *spVelocity) {
			// Direct mutation through p pointer is fine inside Each2.
			p.X += v.DX
		})
		// Additional Set inside the same Write scope.
		flecs.Set(fw, e, spVelocity{10, 0})
	})

	w.Read(func(r *flecs.Reader) {
		p, _ := flecs.Get[spPosition](r, e)
		if p.X != 6 {
			t.Fatalf("position.X: want 6, got %v", p.X)
		}
		v, _ := flecs.Get[spVelocity](r, e)
		if v.DX != 10 {
			t.Fatalf("velocity.DX: want 10 (deferred Set), got %v", v.DX)
		}
	})
}

// TestScopePromotion_ReaderStillWorksOutsideWrite verifies that *Reader still
// satisfies all read free-functions from inside a Read scope.
func TestScopePromotion_ReaderStillWorksOutsideWrite(t *testing.T) {
	w := flecs.New()

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, spPosition{7, 8})
	})

	var count int
	w.Read(func(r *flecs.Reader) {
		p, ok := flecs.Get[spPosition](r, e)
		if !ok || p.X != 7 {
			t.Fatalf("Get via *Reader: want {7 8}, got %v ok=%v", p, ok)
		}
		if !flecs.Has[spPosition](r, e) {
			t.Fatal("Has[spPosition] via *Reader returned false")
		}
		flecs.Each1[spPosition](r, func(_ flecs.ID, _ *spPosition) { count++ })
	})

	if count != 1 {
		t.Fatalf("Each1 via *Reader: want 1, got %d", count)
	}
}

// TestScopePromotion_HasIDFreeFuncReflexive verifies the free function HasID covers
// the reflexive self-pair branch when called via *Writer as scope.
func TestScopePromotion_HasIDFreeFuncReflexive(t *testing.T) {
	w := flecs.New()

	var rel, a flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		a = fw.NewEntity()
		flecs.SetReflexive(w, rel)
	})

	w.Write(func(fw *flecs.Writer) {
		selfPair := flecs.MakePair(rel, a)
		if !flecs.HasID(fw, a, selfPair) {
			t.Fatal("HasID free function: reflexive self-pair should return true via *Writer scope")
		}
		// Non-self pair should still return false.
		b := fw.NewEntity()
		if flecs.HasID(fw, a, flecs.MakePair(rel, b)) {
			t.Fatal("HasID free function: non-self pair should return false for reflexive R")
		}
	})
}

// TestScopePromotion_QueryIterReaderAcceptsScope verifies that QueryIter.Reader()
// returns a *Reader that satisfies read free-functions, and that the pattern the
// issue targeted compiles and runs correctly inside a system callback.
func TestScopePromotion_QueryIterReaderAcceptsScope(t *testing.T) {
	w := flecs.New()

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, spPosition{3, 4})
		flecs.Set(fw, e, spVelocity{1, 2})
	})

	var count int
	// The key pattern from the issue: fw passed directly as the scope argument.
	w.Write(func(fw *flecs.Writer) {
		flecs.Each2[spPosition, spVelocity](fw, func(eid flecs.ID, p *spPosition, v *spVelocity) {
			count++
		})
	})
	if count != 1 {
		t.Fatalf("expected 1 entity, got %d", count)
	}

	// Verify QueryIter.Reader() also satisfies read free-functions.
	posID := flecs.RegisterComponent[spPosition](w)
	it := flecs.NewQuery(w, posID).Iter()
	for it.Next() {
		rdr := it.Reader()
		p, ok := flecs.Get[spPosition](rdr, e)
		if !ok || p.X != 3 {
			t.Fatalf("Get via QueryIter.Reader(): want {3 4}, got %v ok=%v", p, ok)
		}
	}
}
