package flecs_test

import (
	"fmt"

	"github.com/snichols/flecs"
)

// ExampleMakePair shows how to create tag pairs (relationship-target pairs
// with no data payload) using MakePair, AddID, and HasID.
func ExampleMakePair() {
	w := flecs.New()

	var likes, alice, pizza flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likes = fw.NewEntity() // relationship entity
		alice = fw.NewEntity()
		pizza = fw.NewEntity()

		// Construct a pair ID and attach it as a tag.
		pairID := flecs.MakePair(likes, pizza)
		flecs.AddID(fw, alice, pairID)
	})

	w.Read(func(r *flecs.Reader) {
		pairID := flecs.MakePair(likes, pizza)
		fmt.Println("has pair:", flecs.HasID(r, alice, pairID))
	})

	w.Write(func(fw *flecs.Writer) {
		pairID := flecs.MakePair(likes, pizza)
		flecs.RemoveID(fw, alice, pairID)
	})

	w.Read(func(r *flecs.Reader) {
		pairID := flecs.MakePair(likes, pizza)
		fmt.Println("after remove:", flecs.HasID(r, alice, pairID))
	})

	// Output:
	// has pair: true
	// after remove: false
}

// ExampleAddID shows AddID for tag pairs and SetPair/GetPair for data pairs.
func ExampleAddID() {
	type pairDist struct{ Meters float32 }

	w := flecs.New()

	var near, bob, home, office flecs.ID
	w.Write(func(fw *flecs.Writer) {
		near = fw.NewEntity() // relationship entity
		bob = fw.NewEntity()
		home = fw.NewEntity()
		office = fw.NewEntity()

		// Pair-as-tag: express bob is near home (no data).
		flecs.AddID(fw, bob, flecs.MakePair(near, home))

		// Pair-with-data: store the distance to office.
		flecs.SetPair(fw, bob, near, office, pairDist{Meters: 500})
	})

	w.Read(func(r *flecs.Reader) {
		fmt.Println("near home:", flecs.HasID(r, bob, flecs.MakePair(near, home)))

		if d, ok := flecs.GetPair[pairDist](r, bob, near, office); ok {
			fmt.Printf("dist to office: %.0f m\n", d.Meters)
		}
	})

	// Output:
	// near home: true
	// dist to office: 500 m
}
