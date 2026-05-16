package flecs_test

import (
	"fmt"

	"github.com/snichols/flecs"
)

type snapPosition struct{ X, Y float32 }

// ExampleTakeSnapshot demonstrates the basic in-memory binary snapshot API:
// capturing world state with TakeSnapshot, serialising to bytes with Bytes,
// deserialising with LoadSnapshot, and restoring with RestoreSnapshot.
func ExampleTakeSnapshot() {
	w := flecs.New()
	flecs.RegisterComponent[snapPosition](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, snapPosition{X: 3.0, Y: 4.0})
	})

	// Capture the world state as a binary snapshot.
	snap := flecs.TakeSnapshot(w)

	// Serialise to []byte (e.g. for disk or network).
	data := snap.Bytes()

	// Mutate the world so we have something to roll back.
	w.Delete(e)

	// Deserialise from bytes and restore.
	snap2, err := flecs.LoadSnapshot(data)
	if err != nil {
		panic(err)
	}
	flecs.RestoreSnapshot(w, snap2)

	w.Read(func(r *flecs.Reader) {
		pos, ok := flecs.Get[snapPosition](r, e)
		if !ok {
			fmt.Println("entity missing after restore")
			return
		}
		fmt.Printf("X=%.1f Y=%.1f\n", pos.X, pos.Y)
	})

	// Output:
	// X=3.0 Y=4.0
}
