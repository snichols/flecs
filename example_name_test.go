package flecs_test

import (
	"fmt"

	"github.com/snichols/flecs"
)

// ExampleWorld_name demonstrates SetName, Lookup, and PathOf for hierarchical
// entity naming with dot-separated path strings.
func ExampleWorld_name() {
	w := flecs.New()

	// Build a two-level hierarchy: scene.player.
	scene := w.NewEntity()
	player := w.NewEntity()

	w.SetName(scene, "scene")
	w.SetName(player, "player")
	flecs.AddID(w.W(), player, flecs.MakePair(w.ChildOf(), scene))

	// PathOf reconstructs the full dot-separated path from the root.
	fmt.Println(w.PathOf(player))

	// Lookup resolves a dot-separated path to an entity.
	found, ok := w.Lookup("scene.player")
	fmt.Println("found:", ok, "same entity:", found == player)

	// GetName retrieves the leaf name.
	name, _ := w.GetName(player)
	fmt.Println("name:", name)

	// Output:
	// scene.player
	// found: true same entity: true
	// name: player
}
