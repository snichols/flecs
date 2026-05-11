package flecs_test

import (
	"fmt"

	"github.com/snichols/flecs"
)

// ExampleOnSet shows how to register an OnSet hook that fires every time a
// component value is written to an entity.
func ExampleOnSet() {
	type hookScore struct{ Points int }

	w := flecs.New()

	// Register a hook that fires on every Set[hookScore].
	flecs.OnSet[hookScore](w, func(_ *flecs.Writer, _ flecs.ID, s hookScore) {
		fmt.Printf("set: %d\n", s.Points)
	})

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, hookScore{Points: 10}) // hook fires
		flecs.Set(fw, e, hookScore{Points: 20}) // hook fires again on overwrite
	})

	// Output:
	// set: 10
	// set: 20
}
