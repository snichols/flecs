package flecs_test

import (
	"fmt"

	"github.com/snichols/flecs"
)

// ExampleMakePair shows how to create tag pairs (relationship-target pairs
// with no data payload) using MakePair, AddID, and HasID.
func ExampleMakePair() {
	w := flecs.New()

	likes := w.NewEntity() // relationship entity
	alice := w.NewEntity()
	pizza := w.NewEntity()

	// Construct a pair ID and attach it as a tag.
	pairID := flecs.MakePair(likes, pizza)
	flecs.AddID(w.W(), alice, pairID)

	fmt.Println("has pair:", flecs.HasID(w.R(), alice, pairID))

	flecs.RemoveID(w.W(), alice, pairID)
	fmt.Println("after remove:", flecs.HasID(w.R(), alice, pairID))

	// Output:
	// has pair: true
	// after remove: false
}

// ExampleAddID shows AddID for tag pairs and SetPair/GetPair for data pairs.
func ExampleAddID() {
	type pairDist struct{ Meters float32 }

	w := flecs.New()

	near := w.NewEntity() // relationship entity
	bob := w.NewEntity()
	home := w.NewEntity()
	office := w.NewEntity()

	// Pair-as-tag: express bob is near home (no data).
	flecs.AddID(w.W(), bob, flecs.MakePair(near, home))
	fmt.Println("near home:", flecs.HasID(w.R(), bob, flecs.MakePair(near, home)))

	// Pair-with-data: store the distance to office.
	flecs.SetPair(w.W(), bob, near, office, pairDist{Meters: 500})
	if d, ok := flecs.GetPair[pairDist](w.R(), bob, near, office); ok {
		fmt.Printf("dist to office: %.0f m\n", d.Meters)
	}

	// Output:
	// near home: true
	// dist to office: 500 m
}
