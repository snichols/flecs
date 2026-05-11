package flecs_test

import (
	"fmt"

	"github.com/snichols/flecs"
)

// ExampleObserve demonstrates two concurrent observers on the same event and
// selective unsubscription.
func ExampleObserve() {
	type obsScore struct{ Points int }

	w := flecs.New()

	// Register two observers on the same component event.
	flecs.Observe[obsScore](w, flecs.EventOnSet, func(_ *flecs.Writer, _ flecs.ID, s obsScore) {
		fmt.Printf("A: %d\n", s.Points)
	})
	obs2 := flecs.Observe[obsScore](w, flecs.EventOnSet, func(_ *flecs.Writer, _ flecs.ID, s obsScore) {
		fmt.Printf("B: %d\n", s.Points)
	})

	e := w.NewEntity()
	flecs.Set(w.W(), e, obsScore{Points: 5}) // both observers fire

	// Unsubscribe the second observer; only the first fires from now on.
	obs2.Unsubscribe()
	flecs.Set(w.W(), e, obsScore{Points: 9})

	// Output:
	// A: 5
	// B: 5
	// A: 9
}
