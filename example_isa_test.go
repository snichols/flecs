package flecs_test

import (
	"fmt"

	"github.com/snichols/flecs"
)

// ExampleWorld_isA demonstrates IsA prefab inheritance: reading inherited
// components, overriding them locally, and restoring inheritance by removing
// the override.
func ExampleWorld_isA() {
	type isaHealth struct{ HP int }

	w := flecs.New()

	// Create a prefab with base stats.
	dragon := w.NewEntity()
	flecs.Set(w, dragon, isaHealth{HP: 100})

	// Create an instance that inherits from the prefab.
	redDragon := w.NewEntity()
	flecs.AddID(w, redDragon, flecs.MakePair(w.IsA(), dragon))

	// Inherited component is visible via Get.
	if h, ok := flecs.Get[isaHealth](w, redDragon); ok {
		fmt.Println("inherited HP:", h.HP)
	}
	fmt.Println("owns HP:", flecs.Owns[isaHealth](w, redDragon))

	// Override locally with Set (copy-on-write).
	flecs.Set(w, redDragon, isaHealth{HP: 150})
	h, _ := flecs.Get[isaHealth](w, redDragon)
	fmt.Println("overridden HP:", h.HP)
	fmt.Println("owns HP:", flecs.Owns[isaHealth](w, redDragon))

	// Remove the local override; inheritance is restored.
	flecs.Remove[isaHealth](w, redDragon)
	if h, ok := flecs.Get[isaHealth](w, redDragon); ok {
		fmt.Println("restored HP:", h.HP)
	}

	// Output:
	// inherited HP: 100
	// owns HP: false
	// overridden HP: 150
	// owns HP: true
	// restored HP: 100
}
