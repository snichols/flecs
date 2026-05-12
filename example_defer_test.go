package flecs_test

import (
	"fmt"

	"github.com/snichols/flecs"
)

// ExampleWorld_Write shows how Write wraps a block so that structural
// mutations are queued and applied atomically after the block exits — making
// it safe to delete entities during iteration.
func ExampleWorld_Write() {
	type defPos struct{ X float32 }

	w := flecs.New()

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.Set(fw, e1, defPos{X: -1})
		flecs.Set(fw, e2, defPos{X: 5})
	})

	// Write queues mutations; reads inside the block still see current state.
	w.Write(func(fw *flecs.Writer) {
		flecs.Each1[defPos](fw, func(e flecs.ID, p *defPos) {
			if p.X < 0 {
				fw.Delete(e) // queued, not applied yet
			}
		})
		fmt.Println("during write — e1 alive:", w.IsAlive(e1))
	})

	// Deletions are applied when the Write scope exits.
	fmt.Println("e1 alive:", w.IsAlive(e1))
	fmt.Println("e2 alive:", w.IsAlive(e2))

	// Output:
	// during write — e1 alive: true
	// e1 alive: false
	// e2 alive: true
}
