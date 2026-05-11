package flecs_test

import (
	"fmt"

	"github.com/snichols/flecs"
)

// ExampleWorld_childOf demonstrates a three-level hierarchy using the built-in
// ChildOf relationship: navigating parents, iterating children, and cascade delete.
func ExampleWorld_childOf() {
	w := flecs.New()

	// Build: scene → car → wheel.
	scene := w.NewEntity()
	car := w.NewEntity()
	wheel := w.NewEntity()

	w.SetName(scene, "scene")
	w.SetName(car, "car")
	w.SetName(wheel, "wheel")

	// ChildOf pair attaches a child to its parent.
	flecs.AddID(w.W(), car, flecs.MakePair(w.ChildOf(), scene))
	flecs.AddID(w.W(), wheel, flecs.MakePair(w.ChildOf(), car))

	// Navigate upward.
	if parent, ok := w.ParentOf(wheel); ok {
		name, _ := w.GetName(parent)
		fmt.Println("wheel's parent:", name)
	}

	// Iterate direct children of scene.
	w.EachChild(scene, func(child flecs.ID) bool {
		name, _ := w.GetName(child)
		fmt.Println("child of scene:", name)
		return true
	})

	// Deleting scene cascades: car and wheel are also deleted.
	w.Delete(scene)
	fmt.Println("wheel alive:", w.IsAlive(wheel))

	// Output:
	// wheel's parent: car
	// child of scene: car
	// wheel alive: false
}
