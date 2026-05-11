package flecs_test

import (
	"runtime"
	"testing"

	"github.com/snichols/flecs"
)

// component types shared across each_test.go

type eachPos struct{ X, Y float64 }
type eachVel struct{ X, Y float64 }
type eachMass struct{ V float64 }
type eachHealth struct{ HP int }
type eachTag struct{} // zero-size tag component

func TestEach1Basic(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[eachPos](w)

	e1 := w.NewEntity()
	e2 := w.NewEntity()
	e3 := w.NewEntity()
	flecs.Set(w.W(), e1, eachPos{X: 1})
	flecs.Set(w.W(), e2, eachPos{X: 2})
	flecs.Set(w.W(), e3, eachPos{X: 3})
	_ = posID

	var total float64
	var count int
	flecs.Each1[eachPos](w.R(), func(e flecs.ID, p *eachPos) {
		total += p.X
		count++
	})

	if count != 3 {
		t.Fatalf("want 3 entities, got %d", count)
	}
	if total != 6 {
		t.Fatalf("want total 6, got %v", total)
	}
}

func TestEach1MutationWritesBack(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w.W(), e, eachPos{X: 10})

	flecs.Each1[eachPos](w.R(), func(_ flecs.ID, p *eachPos) {
		p.X = 99
	})

	got, ok := flecs.Get[eachPos](w.R(), e)
	if !ok {
		t.Fatal("entity should have eachPos")
	}
	if got.X != 99 {
		t.Fatalf("want X=99, got %v", got.X)
	}
}

func TestEach2Basic(t *testing.T) {
	w := flecs.New()
	e1 := w.NewEntity()
	e2 := w.NewEntity()
	flecs.Set(w.W(), e1, eachPos{X: 1})
	flecs.Set(w.W(), e1, eachVel{X: 10})
	flecs.Set(w.W(), e2, eachPos{X: 2})
	flecs.Set(w.W(), e2, eachVel{X: 20})

	flecs.Each2[eachPos, eachVel](w.R(), func(_ flecs.ID, p *eachPos, v *eachVel) {
		p.X += v.X
	})

	p1, _ := flecs.Get[eachPos](w.R(), e1)
	p2, _ := flecs.Get[eachPos](w.R(), e2)
	if p1.X != 11 {
		t.Fatalf("e1: want X=11, got %v", p1.X)
	}
	if p2.X != 22 {
		t.Fatalf("e2: want X=22, got %v", p2.X)
	}
}

func TestEach3Basic(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w.W(), e, eachPos{X: 1})
	flecs.Set(w.W(), e, eachVel{X: 2})
	flecs.Set(w.W(), e, eachMass{V: 3})

	var sumX, sumV float64
	flecs.Each3[eachPos, eachVel, eachMass](w.R(), func(_ flecs.ID, p *eachPos, v *eachVel, m *eachMass) {
		sumX = p.X
		sumV = v.X + m.V
	})

	if sumX != 1 || sumV != 5 {
		t.Fatalf("want sumX=1 sumV=5, got %v %v", sumX, sumV)
	}
}

func TestEach4Basic(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w.W(), e, eachPos{X: 1})
	flecs.Set(w.W(), e, eachVel{X: 2})
	flecs.Set(w.W(), e, eachMass{V: 3})
	flecs.Set(w.W(), e, eachHealth{HP: 4})

	var total float64
	flecs.Each4[eachPos, eachVel, eachMass, eachHealth](w.R(), func(_ flecs.ID, p *eachPos, v *eachVel, m *eachMass, h *eachHealth) {
		total = p.X + v.X + m.V + float64(h.HP)
	})

	if total != 10 {
		t.Fatalf("want 10, got %v", total)
	}
}

func TestEach2MixedArchetypes(t *testing.T) {
	w := flecs.New()

	// archetype [eachPos, eachVel]
	e1 := w.NewEntity()
	flecs.Set(w.W(), e1, eachPos{X: 1})
	flecs.Set(w.W(), e1, eachVel{X: 10})

	// archetype [eachPos, eachVel, eachTag]
	e2 := w.NewEntity()
	flecs.Set(w.W(), e2, eachPos{X: 2})
	flecs.Set(w.W(), e2, eachVel{X: 20})
	flecs.Set(w.W(), e2, eachTag{})

	var count int
	flecs.Each2[eachPos, eachVel](w.R(), func(_ flecs.ID, p *eachPos, v *eachVel) {
		p.X += v.X
		count++
	})

	if count != 2 {
		t.Fatalf("want 2 entities across both archetypes, got %d", count)
	}
	p1, _ := flecs.Get[eachPos](w.R(), e1)
	p2, _ := flecs.Get[eachPos](w.R(), e2)
	if p1.X != 11 {
		t.Fatalf("e1: want X=11, got %v", p1.X)
	}
	if p2.X != 22 {
		t.Fatalf("e2: want X=22, got %v", p2.X)
	}
}

func TestEach2NoMatch(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w.W(), e, eachPos{X: 1})
	// no eachVel; Each2 should not call fn at all

	var count int
	flecs.Each2[eachPos, eachVel](w.R(), func(_ flecs.ID, _ *eachPos, _ *eachVel) {
		count++
	})

	if count != 0 {
		t.Fatalf("want 0 calls, got %d", count)
	}
}

func TestEach1AutoRegistration(t *testing.T) {
	type neverRegistered struct{ V int }
	w := flecs.New()
	before := w.Count()

	var count int
	flecs.Each1[neverRegistered](w.R(), func(_ flecs.ID, _ *neverRegistered) {
		count++
	})

	after := w.Count()
	if after <= before {
		t.Fatalf("world component count should have grown (auto-registered); before=%d after=%d", before, after)
	}
	if count != 0 {
		t.Fatalf("no entities have neverRegistered, so fn should never be called; got %d", count)
	}
}

func TestEach1TagComponent(t *testing.T) {
	w := flecs.New()
	e1 := w.NewEntity()
	e2 := w.NewEntity()
	flecs.Set(w.W(), e1, eachTag{})
	flecs.Set(w.W(), e2, eachTag{})

	var count int
	flecs.Each1[eachTag](w.R(), func(_ flecs.ID, _ *eachTag) {
		count++
	})

	if count != 2 {
		t.Fatalf("want 2 calls for tag component, got %d", count)
	}
}

func TestEach1GCPointerSurvives(t *testing.T) {
	type withString struct{ Name string }
	w := flecs.New()

	entities := make([]flecs.ID, 10)
	for i := range entities {
		e := w.NewEntity()
		flecs.Set(w.W(), e, withString{Name: "entity"})
		entities[i] = e
	}

	runtime.GC()
	runtime.GC()

	var count int
	flecs.Each1[withString](w.R(), func(_ flecs.ID, s *withString) {
		if s.Name != "entity" {
			panic("GC corrupted string in column")
		}
		count++
	})

	if count != 10 {
		t.Fatalf("want 10 entities, got %d", count)
	}
}

func TestEachMutationIsLive(t *testing.T) {
	// Explicitly verify that mutations via the pointer passed to fn write back
	// to the live column slot and are visible via Get after iteration.
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w.W(), e, eachPos{X: 0, Y: 0})

	flecs.Each1[eachPos](w.R(), func(_ flecs.ID, p *eachPos) {
		p.X = 42
		p.Y = 7
	})

	got, ok := flecs.Get[eachPos](w.R(), e)
	if !ok {
		t.Fatal("entity should still have eachPos after Each1")
	}
	if got.X != 42 || got.Y != 7 {
		t.Fatalf("mutation not live: want {42 7}, got %+v", got)
	}
}
