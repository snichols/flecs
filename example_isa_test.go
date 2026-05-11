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
	var dragon, redDragon flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dragon = fw.NewEntity()
		flecs.Set(fw, dragon, isaHealth{HP: 100})

		// Create an instance that inherits from the prefab.
		redDragon = fw.NewEntity()
		flecs.AddID(fw, redDragon, flecs.MakePair(w.IsA(), dragon))
	})

	w.Read(func(r *flecs.Reader) {
		// Inherited component is visible via Get.
		if h, ok := flecs.Get[isaHealth](r, redDragon); ok {
			fmt.Println("inherited HP:", h.HP)
		}
		fmt.Println("owns HP:", flecs.Owns[isaHealth](r, redDragon))
	})

	// Override locally with Set (copy-on-write).
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, redDragon, isaHealth{HP: 150})
	})

	w.Read(func(r *flecs.Reader) {
		h, _ := flecs.Get[isaHealth](r, redDragon)
		fmt.Println("overridden HP:", h.HP)
		fmt.Println("owns HP:", flecs.Owns[isaHealth](r, redDragon))
	})

	// Remove the local override; inheritance is restored.
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[isaHealth](fw, redDragon)
	})

	w.Read(func(r *flecs.Reader) {
		if h, ok := flecs.Get[isaHealth](r, redDragon); ok {
			fmt.Println("restored HP:", h.HP)
		}
	})

	// Output:
	// inherited HP: 100
	// owns HP: false
	// overridden HP: 150
	// owns HP: true
	// restored HP: 100
}
