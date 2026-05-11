package flecs_test

import (
	"fmt"

	"github.com/snichols/flecs"
)

// Component types used in the basic example.
type basicPos struct{ X, Y float32 }
type basicVel struct{ DX, DY float32 }

// ExampleWorld_basic demonstrates the core workflow: create a world, attach
// components to entities, read them back, and iterate with Each2.
func ExampleWorld_basic() {
	w := flecs.New()

	// Create two entities with Position and Velocity.
	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.Set(fw, e1, basicPos{X: 1, Y: 2})
		flecs.Set(fw, e1, basicVel{DX: 0.5})
		flecs.Set(fw, e2, basicPos{X: 4, Y: 0})
		flecs.Set(fw, e2, basicVel{DX: -1})
	})

	// Read a single component.
	w.Read(func(r *flecs.Reader) {
		if p, ok := flecs.Get[basicPos](r, e1); ok {
			fmt.Printf("e1 pos: %.0f %.0f\n", p.X, p.Y)
		}

		// Iterate all entities that have both Position and Velocity.
		flecs.Each2[basicPos, basicVel](r, func(e flecs.ID, p *basicPos, v *basicVel) {
			p.X += v.DX // update in place
		})

		// Positions are updated.
		p1, _ := flecs.Get[basicPos](r, e1)
		p2, _ := flecs.Get[basicPos](r, e2)
		fmt.Printf("after step: e1=%.1f e2=%.0f\n", p1.X, p2.X)
	})

	// Output:
	// e1 pos: 1 2
	// after step: e1=1.5 e2=3
}
