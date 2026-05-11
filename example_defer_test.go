package flecs_test

import (
	"fmt"

	"github.com/snichols/flecs"
)

// ExampleWorld_defer shows how Defer wraps a block so that structural
// mutations are queued and applied atomically after the block exits — making
// it safe to delete entities during iteration.
func ExampleWorld_defer() {
	type defPos struct{ X float32 }

	w := flecs.New()

	e1 := w.NewEntity()
	e2 := w.NewEntity()
	flecs.Set(w, e1, defPos{X: -1})
	flecs.Set(w, e2, defPos{X: 5})

	// Defer queues mutations; reads inside the block still see current state.
	w.Defer(func() {
		flecs.Each1[defPos](w, func(e flecs.ID, p *defPos) {
			if p.X < 0 {
				w.Delete(e) // queued, not applied yet
			}
		})
		fmt.Println("during defer — e1 alive:", w.IsAlive(e1))
	})

	// Deletions are applied when DeferEnd is called (on Defer block exit).
	fmt.Println("e1 alive:", w.IsAlive(e1))
	fmt.Println("e2 alive:", w.IsAlive(e2))

	// Output:
	// during defer — e1 alive: true
	// e1 alive: false
	// e2 alive: true
}
