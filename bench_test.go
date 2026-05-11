package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// ---- component types used only in benchmarks ----

type benchPos struct{ X, Y float64 }
type benchVel struct{ DX, DY float64 }
type benchTag1 struct{}
type benchTag2 struct{}
type benchTag3 struct{}
type benchTag4 struct{}
type benchTag5 struct{}

// ---- setup helpers ----

// setupWorldMultiArchetype pre-creates n entities split evenly across `archetypes`
// distinct archetype tables. Each archetype has Position+Velocity plus one extra
// tag to force a different table. Resets b's timer and returns the world.
func setupWorldMultiArchetype(b *testing.B, n, archetypes int) *flecs.World {
	b.Helper()
	w := flecs.New()
	posID := flecs.RegisterComponent[benchPos](w)
	velID := flecs.RegisterComponent[benchVel](w)
	tagIDs := []flecs.ID{
		flecs.RegisterComponent[benchTag1](w),
		flecs.RegisterComponent[benchTag2](w),
		flecs.RegisterComponent[benchTag3](w),
		flecs.RegisterComponent[benchTag4](w),
		flecs.RegisterComponent[benchTag5](w),
	}
	if archetypes > len(tagIDs) {
		archetypes = len(tagIDs)
	}
	for i := range n {
		e := w.NewEntity()
		flecs.Set(w, e, benchPos{})
		flecs.Set(w, e, benchVel{DX: 1})
		// add an extra tag to vary the archetype
		flecs.AddID(w, e, tagIDs[i%archetypes])
	}
	_ = posID
	_ = velID
	b.ResetTimer()
	return w
}

// ---- a) Entity lifecycle ----

func BenchmarkNewEntity(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	b.ResetTimer()
	for range b.N {
		_ = w.NewEntity()
	}
}

func BenchmarkDeleteEntity(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	entities := make([]flecs.ID, b.N)
	for i := range b.N {
		entities[i] = w.NewEntity()
	}
	b.ResetTimer()
	for _, e := range entities {
		w.Delete(e)
	}
}

func BenchmarkAllocFreeAllocCycle(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	b.ResetTimer()
	for range b.N {
		e := w.NewEntity()
		w.Delete(e)
		_ = w.NewEntity()
	}
}

// ---- b) Component Set/Get/Has (single component) ----

func BenchmarkSetExistingComponent(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w, e, benchPos{X: 1, Y: 2})
	b.ResetTimer()
	for range b.N {
		flecs.Set(w, e, benchPos{X: 3, Y: 4})
	}
}

func BenchmarkSetNewComponentTriggerMigration(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	entities := make([]flecs.ID, b.N)
	for i := range b.N {
		entities[i] = w.NewEntity()
	}
	b.ResetTimer()
	for _, e := range entities {
		flecs.Set(w, e, benchPos{X: 1})
	}
}

func BenchmarkGetExistingComponent(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w, e, benchPos{X: 1, Y: 2})
	b.ResetTimer()
	for range b.N {
		_, _ = flecs.Get[benchPos](w, e)
	}
}

func BenchmarkGetMissingComponent(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	e := w.NewEntity()
	b.ResetTimer()
	for range b.N {
		_, _ = flecs.Get[benchPos](w, e)
	}
}

func BenchmarkHasExistingComponent(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w, e, benchPos{X: 1})
	b.ResetTimer()
	for range b.N {
		_ = flecs.Has[benchPos](w, e)
	}
}

func BenchmarkOwnsVsHas(b *testing.B) {
	b.ReportAllocs()

	// Build a prefab with Position, then a child entity that inherits via IsA.
	w := flecs.New()
	posID := flecs.RegisterComponent[benchPos](w)
	prefab := w.NewEntity()
	flecs.Set(w, prefab, benchPos{X: 42})

	child := w.NewEntity()
	flecs.AddID(w, child, flecs.MakePair(w.IsA(), prefab))

	b.Run("Has-via-IsA", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_ = flecs.HasID(w, child, posID)
		}
	})
	b.Run("Owns-local-only", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_ = flecs.OwnsID(w, child, posID)
		}
	})
}

// ---- c) Archetype migration ----

func BenchmarkAddOneComponent_CacheHit(b *testing.B) {
	// All entities start with no components. After the first entity migrates
	// from [] → [benchPos], the destination table edge is cached; subsequent
	// entities reuse the same edge.
	b.ReportAllocs()
	w := flecs.New()
	entities := make([]flecs.ID, b.N)
	for i := range b.N {
		entities[i] = w.NewEntity()
	}
	b.ResetTimer()
	for _, e := range entities {
		flecs.Set(w, e, benchPos{})
	}
}

func BenchmarkAddOneComponent_CacheMiss(b *testing.B) {
	// Methodology: each entity starts from a distinct source archetype by
	// pre-loading it with a different number of tags (0..4 tags cycling).
	// This forces the migration code to look up different source-table edges
	// each iteration, exercising the cold-edge path more often.
	// Note: Go's testing framework provides no mechanism to flush CPU caches,
	// so "miss" here means "edge-map miss due to varied source archetypes."
	b.ReportAllocs()
	w := flecs.New()
	tagIDs := [4]flecs.ID{
		flecs.RegisterComponent[benchTag1](w),
		flecs.RegisterComponent[benchTag2](w),
		flecs.RegisterComponent[benchTag3](w),
		flecs.RegisterComponent[benchTag4](w),
	}
	entities := make([]flecs.ID, b.N)
	for i := range b.N {
		e := w.NewEntity()
		// give each entity a varying set of tags so source archetypes differ
		for j := range i % len(tagIDs) {
			flecs.AddID(w, e, tagIDs[j])
		}
		entities[i] = e
	}
	b.ResetTimer()
	for _, e := range entities {
		flecs.Set(w, e, benchPos{})
	}
}

func BenchmarkRemoveOneComponent(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	entities := make([]flecs.ID, b.N)
	for i := range b.N {
		e := w.NewEntity()
		flecs.Set(w, e, benchPos{})
		entities[i] = e
	}
	b.ResetTimer()
	for _, e := range entities {
		flecs.Remove[benchPos](w, e)
	}
}

func BenchmarkSwapComponent(b *testing.B) {
	// Entity goes [benchPos, benchVel] → [benchPos] (remove benchVel).
	b.ReportAllocs()
	w := flecs.New()
	entities := make([]flecs.ID, b.N)
	for i := range b.N {
		e := w.NewEntity()
		flecs.Set(w, e, benchPos{})
		flecs.Set(w, e, benchVel{})
		entities[i] = e
	}
	b.ResetTimer()
	for _, e := range entities {
		flecs.Remove[benchVel](w, e)
	}
}

// ---- d) Query iteration ----

func BenchmarkQueryEach2_1k(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	for range 1_000 {
		e := w.NewEntity()
		flecs.Set(w, e, benchPos{X: 1})
		flecs.Set(w, e, benchVel{DX: 1})
	}
	b.ResetTimer()
	for range b.N {
		flecs.Each2[benchPos, benchVel](w, func(_ flecs.ID, p *benchPos, v *benchVel) {
			p.X += v.DX
			p.Y += v.DY
		})
	}
}

func BenchmarkQueryEach2_10k(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	for range 10_000 {
		e := w.NewEntity()
		flecs.Set(w, e, benchPos{X: 1})
		flecs.Set(w, e, benchVel{DX: 1})
	}
	b.ResetTimer()
	for range b.N {
		flecs.Each2[benchPos, benchVel](w, func(_ flecs.ID, p *benchPos, v *benchVel) {
			p.X += v.DX
			p.Y += v.DY
		})
	}
}

func BenchmarkQueryEach2_100k(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	for range 100_000 {
		e := w.NewEntity()
		flecs.Set(w, e, benchPos{X: 1})
		flecs.Set(w, e, benchVel{DX: 1})
	}
	b.ResetTimer()
	for range b.N {
		flecs.Each2[benchPos, benchVel](w, func(_ flecs.ID, p *benchPos, v *benchVel) {
			p.X += v.DX
			p.Y += v.DY
		})
	}
}

func BenchmarkCachedQueryEach2_10k(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	posID := flecs.RegisterComponent[benchPos](w)
	velID := flecs.RegisterComponent[benchVel](w)
	for range 10_000 {
		e := w.NewEntity()
		flecs.Set(w, e, benchPos{X: 1})
		flecs.Set(w, e, benchVel{DX: 1})
	}
	cq := flecs.NewCachedQuery(w, posID, velID)
	b.ResetTimer()
	for range b.N {
		it := cq.Iter()
		for it.Next() {
			col := flecs.Field[benchPos](it, posID)
			colV := flecs.Field[benchVel](it, velID)
			for i := range col {
				col[i].X += colV[i].DX
				col[i].Y += colV[i].DY
			}
		}
	}
}

func BenchmarkQueryIterField_10k(b *testing.B) {
	// Manual NewQuery→Iter→Next→Field[T] loop; compare with Each2 to see closure overhead.
	b.ReportAllocs()
	w := flecs.New()
	posID := flecs.RegisterComponent[benchPos](w)
	velID := flecs.RegisterComponent[benchVel](w)
	for range 10_000 {
		e := w.NewEntity()
		flecs.Set(w, e, benchPos{X: 1})
		flecs.Set(w, e, benchVel{DX: 1})
	}
	q := flecs.NewQuery(w, posID, velID)
	b.ResetTimer()
	for range b.N {
		it := q.Iter()
		for it.Next() {
			col := flecs.Field[benchPos](it, posID)
			colV := flecs.Field[benchVel](it, velID)
			for i := range col {
				col[i].X += colV[i].DX
				col[i].Y += colV[i].DY
			}
		}
	}
}

func BenchmarkQueryAcrossArchetypes_10k(b *testing.B) {
	// 10,000 entities split across 5 archetypes, each having benchPos+benchVel
	// plus a distinct extra tag. Tests multi-table iteration overhead.
	b.ReportAllocs()
	w := setupWorldMultiArchetype(b, 10_000, 5)
	b.ResetTimer()
	for range b.N {
		flecs.Each2[benchPos, benchVel](w, func(_ flecs.ID, p *benchPos, v *benchVel) {
			p.X += v.DX
			p.Y += v.DY
		})
	}
}

func BenchmarkFieldT_AllocCost(b *testing.B) {
	// Isolate the reflect+Interface() cost of Field[T] by calling it in a tight
	// loop. Each call touches one table column.
	b.ReportAllocs()
	w := flecs.New()
	posID := flecs.RegisterComponent[benchPos](w)
	for range 100 {
		e := w.NewEntity()
		flecs.Set(w, e, benchPos{X: 1})
	}
	q := flecs.NewQuery(w, posID)
	it := q.Iter()
	it.Next() // position on the single table
	b.ResetTimer()
	for range b.N {
		col := flecs.Field[benchPos](it, posID)
		_ = col
	}
}

// ---- e) Hooks + Observers ----

func BenchmarkSetNoHook_10k(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	entities := make([]flecs.ID, 10_000)
	for i := range entities {
		entities[i] = w.NewEntity()
		flecs.Set(w, entities[i], benchPos{})
	}
	b.ResetTimer()
	for range b.N {
		for _, e := range entities {
			flecs.Set(w, e, benchPos{X: 1})
		}
	}
}

func BenchmarkOnSetHookFires_10k(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	var sink float64
	flecs.OnSet[benchPos](w, func(_ flecs.ID, p *benchPos) {
		sink += p.X
	})
	entities := make([]flecs.ID, 10_000)
	for i := range entities {
		entities[i] = w.NewEntity()
		flecs.Set(w, entities[i], benchPos{})
	}
	b.ResetTimer()
	for range b.N {
		for _, e := range entities {
			flecs.Set(w, e, benchPos{X: 1})
		}
	}
	_ = sink
}

func BenchmarkObserverFires_10k(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	var sink float64
	flecs.Observe[benchPos](w, flecs.EventOnSet, func(_ flecs.ID, p *benchPos) {
		sink += p.X
	})
	entities := make([]flecs.ID, 10_000)
	for i := range entities {
		entities[i] = w.NewEntity()
		flecs.Set(w, entities[i], benchPos{})
	}
	b.ResetTimer()
	for range b.N {
		for _, e := range entities {
			flecs.Set(w, e, benchPos{X: 1})
		}
	}
	_ = sink
}

func BenchmarkObserverFires_HookAndObserver_10k(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	var sink float64
	flecs.OnSet[benchPos](w, func(_ flecs.ID, p *benchPos) {
		sink += p.X
	})
	flecs.Observe[benchPos](w, flecs.EventOnSet, func(_ flecs.ID, p *benchPos) {
		sink += p.X
	})
	entities := make([]flecs.ID, 10_000)
	for i := range entities {
		entities[i] = w.NewEntity()
		flecs.Set(w, entities[i], benchPos{})
	}
	b.ResetTimer()
	for range b.N {
		for _, e := range entities {
			flecs.Set(w, e, benchPos{X: 1})
		}
	}
	_ = sink
}

func BenchmarkObserverFires_5observers_10k(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	var sink float64
	for range 5 {
		flecs.Observe[benchPos](w, flecs.EventOnSet, func(_ flecs.ID, p *benchPos) {
			sink += p.X
		})
	}
	entities := make([]flecs.ID, 10_000)
	for i := range entities {
		entities[i] = w.NewEntity()
		flecs.Set(w, entities[i], benchPos{})
	}
	b.ResetTimer()
	for range b.N {
		for _, e := range entities {
			flecs.Set(w, e, benchPos{X: 1})
		}
	}
	_ = sink
}

// ---- f) Deferred queue ----

func BenchmarkDeferOverhead_NoOps(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	b.ResetTimer()
	for range b.N {
		w.Defer(func() {})
	}
}

func BenchmarkDeferSet_10k(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	entities := make([]flecs.ID, 10_000)
	for i := range entities {
		entities[i] = w.NewEntity()
		flecs.Set(w, entities[i], benchPos{})
	}
	b.ResetTimer()
	for range b.N {
		w.Defer(func() {
			for _, e := range entities {
				flecs.Set(w, e, benchPos{X: 2})
			}
		})
	}
}

func BenchmarkDeferDelete_10k(b *testing.B) {
	b.ReportAllocs()
	b.StopTimer()
	for range b.N {
		w := flecs.New()
		entities := make([]flecs.ID, 10_000)
		for i := range entities {
			entities[i] = w.NewEntity()
		}
		b.StartTimer()
		w.Defer(func() {
			for _, e := range entities {
				w.Delete(e)
			}
		})
		b.StopTimer()
	}
}

func BenchmarkDeferNested(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w, e, benchPos{})
	b.ResetTimer()
	for range b.N {
		w.DeferBegin()
		w.DeferBegin()
		flecs.Set(w, e, benchPos{X: 1})
		w.DeferEnd()
		w.DeferEnd()
	}
}

// ---- g) Systems + Progress ----

func BenchmarkProgress_NoSystems(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	b.ResetTimer()
	for range b.N {
		w.Progress(1.0 / 60.0)
	}
}

func BenchmarkProgress_OneSystem_10kEntities(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	posID := flecs.RegisterComponent[benchPos](w)
	velID := flecs.RegisterComponent[benchVel](w)
	for range 10_000 {
		e := w.NewEntity()
		flecs.Set(w, e, benchPos{X: 1})
		flecs.Set(w, e, benchVel{DX: 1})
	}
	cq := flecs.NewCachedQuery(w, posID, velID)
	flecs.NewSystem(w, cq, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			col := flecs.Field[benchPos](it, posID)
			colV := flecs.Field[benchVel](it, velID)
			for i := range col {
				col[i].X += colV[i].DX
			}
		}
	})
	b.ResetTimer()
	for range b.N {
		w.Progress(1.0 / 60.0)
	}
}

func BenchmarkProgress_TenSystems_1kEntities(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	posID := flecs.RegisterComponent[benchPos](w)
	velID := flecs.RegisterComponent[benchVel](w)
	for range 1_000 {
		e := w.NewEntity()
		flecs.Set(w, e, benchPos{X: 1})
		flecs.Set(w, e, benchVel{DX: 1})
	}
	for range 10 {
		cq := flecs.NewCachedQuery(w, posID, velID)
		flecs.NewSystem(w, cq, func(_ float32, it *flecs.QueryIter) {
			for it.Next() {
				col := flecs.Field[benchPos](it, posID)
				colV := flecs.Field[benchVel](it, velID)
				for i := range col {
					col[i].X += colV[i].DX
				}
			}
		})
	}
	b.ResetTimer()
	for range b.N {
		w.Progress(1.0 / 60.0)
	}
}

func BenchmarkProgress_WithFixedTimestep(b *testing.B) {
	b.ReportAllocs()
	w := flecs.New()
	w.SetFixedTimestep(1.0 / 60.0)
	posID := flecs.RegisterComponent[benchPos](w)
	velID := flecs.RegisterComponent[benchVel](w)
	for range 1_000 {
		e := w.NewEntity()
		flecs.Set(w, e, benchPos{X: 1})
		flecs.Set(w, e, benchVel{DX: 1})
	}
	cq := flecs.NewCachedQuery(w, posID, velID)
	flecs.NewSystemInPhase(w, w.OnFixedUpdate(), cq, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			col := flecs.Field[benchPos](it, posID)
			colV := flecs.Field[benchVel](it, velID)
			for i := range col {
				col[i].X += colV[i].DX
			}
		}
	})
	b.ResetTimer()
	for range b.N {
		w.Progress(1.0 / 60.0)
	}
}

func BenchmarkProgress_PipelineFull(b *testing.B) {
	// Systems in all 4 phases (Pre, OnFixed, On, Post), 1k entities each.
	b.ReportAllocs()
	w := flecs.New()
	w.SetFixedTimestep(1.0 / 60.0)
	posID := flecs.RegisterComponent[benchPos](w)
	velID := flecs.RegisterComponent[benchVel](w)
	for range 1_000 {
		e := w.NewEntity()
		flecs.Set(w, e, benchPos{X: 1})
		flecs.Set(w, e, benchVel{DX: 1})
	}
	sysFn := func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			col := flecs.Field[benchPos](it, posID)
			colV := flecs.Field[benchVel](it, velID)
			for i := range col {
				col[i].X += colV[i].DX
			}
		}
	}
	for _, phase := range []flecs.ID{w.PreUpdate(), w.OnFixedUpdate(), w.OnUpdate(), w.PostUpdate()} {
		cq := flecs.NewCachedQuery(w, posID, velID)
		flecs.NewSystemInPhase(w, phase, cq, sysFn)
	}
	b.ResetTimer()
	for range b.N {
		w.Progress(1.0 / 60.0)
	}
}

// ---- h) ChildOf / IsA hierarchies ----

func BenchmarkChildOfCascadeDelete_100(b *testing.B) {
	b.ReportAllocs()
	b.StopTimer()
	for range b.N {
		w := flecs.New()
		parent := w.NewEntity()
		for range 100 {
			child := w.NewEntity()
			flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))
		}
		b.StartTimer()
		w.Delete(parent)
		b.StopTimer()
	}
}

func BenchmarkChildOfCascadeDelete_10k(b *testing.B) {
	b.ReportAllocs()
	b.StopTimer()
	for range b.N {
		w := flecs.New()
		parent := w.NewEntity()
		for range 10_000 {
			child := w.NewEntity()
			flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))
		}
		b.StartTimer()
		w.Delete(parent)
		b.StopTimer()
	}
}

func BenchmarkIsAGet_Hit(b *testing.B) {
	// Get on a child where the prefab has benchPos; one IsA hop.
	b.ReportAllocs()
	w := flecs.New()
	prefab := w.NewEntity()
	flecs.Set(w, prefab, benchPos{X: 99})
	child := w.NewEntity()
	flecs.AddID(w, child, flecs.MakePair(w.IsA(), prefab))
	b.ResetTimer()
	for range b.N {
		_, _ = flecs.Get[benchPos](w, child)
	}
}

func BenchmarkIsAGet_MissedChain(b *testing.B) {
	// Get on a child with (IsA, prefab) where prefab doesn't have benchPos;
	// the chain falls through without finding the component.
	b.ReportAllocs()
	w := flecs.New()
	prefab := w.NewEntity() // no benchPos
	child := w.NewEntity()
	flecs.AddID(w, child, flecs.MakePair(w.IsA(), prefab))
	b.ResetTimer()
	for range b.N {
		_, _ = flecs.Get[benchPos](w, child)
	}
}

func BenchmarkLookupPath_3deep(b *testing.B) {
	// w.Lookup("scene.car.wheel") over a populated tree.
	b.ReportAllocs()
	w := flecs.New()
	scene := w.NewEntity()
	w.SetName(scene, "scene")
	car := w.NewEntity()
	flecs.AddID(w, car, flecs.MakePair(w.ChildOf(), scene))
	w.SetName(car, "car")
	wheel := w.NewEntity()
	flecs.AddID(w, wheel, flecs.MakePair(w.ChildOf(), car))
	w.SetName(wheel, "wheel")
	b.ResetTimer()
	for range b.N {
		_, _ = w.Lookup("scene.car.wheel")
	}
}
