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

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, obsScore{Points: 5}) // both observers fire when flushed
	})
	// Unsubscribe the second observer before the next Set fires.
	obs2.Unsubscribe()
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, obsScore{Points: 9}) // only observer A fires
	})

	// Output:
	// A: 5
	// B: 5
	// A: 9
}
