package flecs_test

import (
	"fmt"

	"github.com/snichols/flecs"
)

// ExampleQuery shows low-level query iteration using Iter, Next, and Field.
func ExampleQuery() {
	type qPos struct{ X float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[qPos](w)

	w.Write(func(fw *flecs.Writer) {
		for _, x := range []float32{10, 20, 30} {
			e := fw.NewEntity()
			flecs.Set(fw, e, qPos{X: x})
		}
	})

	// NewQuery builds an AND-term query; Iter starts iteration.
	q := flecs.NewQuery(w, posID)
	it := q.Iter()
	for it.Next() {
		// Field returns a live []T slice into the column for the current table.
		for _, p := range flecs.Field[qPos](it, posID) {
			fmt.Printf("x=%.0f\n", p.X)
		}
	}

	// Output:
	// x=10
	// x=20
	// x=30
}

// ExampleCachedQuery shows a persistent query that tracks new archetype tables
// automatically as entities acquire new component sets.
func ExampleCachedQuery() {
	type cqA struct{ V float32 }
	type cqB struct{ W float32 }

	w := flecs.New()
	aID := flecs.RegisterComponent[cqA](w)

	w.Write(func(fw *flecs.Writer) {
		e1 := fw.NewEntity()
		flecs.Set(fw, e1, cqA{V: 1})
	})

	// NewCachedQuery scans existing tables at construction.
	cq := flecs.NewCachedQuery(w, aID)
	fmt.Println("initial entities:", cq.EntityCount())

	// Adding a second entity with an extra component creates a new archetype
	// table; the cached query picks it up automatically.
	w.Write(func(fw *flecs.Writer) {
		e2 := fw.NewEntity()
		flecs.Set(fw, e2, cqA{V: 2})
		flecs.Set(fw, e2, cqB{W: 3}) // migration → new {cqA,cqB} table
	})

	fmt.Println("entities:", cq.EntityCount())
	fmt.Println("tables:", cq.Count())

	cq.Close()
	fmt.Println("closed:", cq.IsClosed())

	// Output:
	// initial entities: 1
	// entities: 2
	// tables: 2
	// closed: true
}

// ExampleEach2 shows ergonomic two-component iteration.
func ExampleEach2() {
	type e2Pos struct{ X float32 }
	type e2Vel struct{ DX float32 }

	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		for i := range 3 {
			e := fw.NewEntity()
			flecs.Set(fw, e, e2Pos{X: float32(i) * 10})
			flecs.Set(fw, e, e2Vel{DX: 5})
		}
	})

	// Each2 iterates every entity that owns both e2Pos and e2Vel.
	w.Read(func(r *flecs.Reader) {
		flecs.Each2[e2Pos, e2Vel](r, func(e flecs.ID, p *e2Pos, v *e2Vel) {
			p.X += v.DX
		})

		flecs.Each2[e2Pos, e2Vel](r, func(e flecs.ID, p *e2Pos, v *e2Vel) {
			fmt.Printf("%.0f\n", p.X)
		})
	})

	// Output:
	// 5
	// 15
	// 25
}
