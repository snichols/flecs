package flecs_test

import (
	"fmt"

	"github.com/snichols/flecs"
)

// Component types used in the iter.Seq example functions.

type exPos struct{ X, Y float32 }
type exVel struct{ DX, DY float32 }
type exMass struct{ V float32 }
type exHealth struct{ HP int }

func ExampleAll1() {
	w := flecs.New()

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.Set(fw, e1, exPos{X: 1, Y: 2})
		flecs.Set(fw, e2, exPos{X: 3, Y: 4})
	})

	w.Read(func(r *flecs.Reader) {
		for e, pos := range flecs.All1[exPos](r) {
			fmt.Printf("entity %v pos=(%.0f,%.0f)\n", e, pos.X, pos.Y)
		}
	})

	// (Output ordering depends on iteration order; omit Output directive for
	// non-deterministic cases in production tests.)
	_ = e1
	_ = e2
}

func ExampleAll2() {
	w := flecs.New()

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, exPos{X: 0, Y: 0})
		flecs.Set(fw, e, exVel{DX: 1, DY: 2})
	})

	w.Read(func(r *flecs.Reader) {
		for _, pv := range flecs.All2[exPos, exVel](r) {
			pos, vel := pv.A, pv.B
			fmt.Printf("pos=(%.0f,%.0f) vel=(%.0f,%.0f)\n", pos.X, pos.Y, vel.DX, vel.DY)
		}
	})

	// Output:
	// pos=(0,0) vel=(1,2)
}

func ExampleAll3() {
	w := flecs.New()

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, exPos{X: 1})
		flecs.Set(fw, e, exVel{DX: 2})
		flecs.Set(fw, e, exMass{V: 3})
	})

	w.Read(func(r *flecs.Reader) {
		for _, t := range flecs.All3[exPos, exVel, exMass](r) {
			fmt.Printf("X=%.0f DX=%.0f V=%.0f\n", t.A.X, t.B.DX, t.C.V)
		}
	})

	// Output:
	// X=1 DX=2 V=3
}

func ExampleAll4() {
	w := flecs.New()

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, exPos{X: 1})
		flecs.Set(fw, e, exVel{DX: 2})
		flecs.Set(fw, e, exMass{V: 3})
		flecs.Set(fw, e, exHealth{HP: 100})
	})

	w.Read(func(r *flecs.Reader) {
		for _, q := range flecs.All4[exPos, exVel, exMass, exHealth](r) {
			fmt.Printf("X=%.0f DX=%.0f V=%.0f HP=%d\n",
				q.A.X, q.B.DX, q.C.V, q.D.HP)
		}
	})

	// Output:
	// X=1 DX=2 V=3 HP=100
}

func ExampleQueryAll() {
	w := flecs.New()
	var posID flecs.ID

	var entities []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[exPos](w)
		for i := range 3 {
			e := fw.NewEntity()
			flecs.Set(fw, e, exPos{X: float32(i)})
			entities = append(entities, e)
		}
	})

	q := flecs.NewQuery(w, posID)
	count := 0
	w.Read(func(r *flecs.Reader) {
		for range flecs.QueryAll(q, r) {
			count++
		}
	})

	fmt.Printf("matched %d entities\n", count)

	// Output:
	// matched 3 entities
}
