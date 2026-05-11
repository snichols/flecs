package flecs_test

import (
	"fmt"

	"github.com/snichols/flecs"
)

// plTick is a zero-size tag component used by the pipeline example to give
// systems a matching entity.
type plTick struct{}

// Example_pipeline demonstrates a four-phase pipeline. Systems are registered
// in PreUpdate, OnFixedUpdate, OnUpdate, and PostUpdate. One Progress call with
// dt equal to the fixed timestep runs each phase exactly once.
func Example_pipeline() {
	w := flecs.New()

	// Fixed timestep of 1 s; one Progress(1.0) fires OnFixedUpdate once.
	w.SetFixedTimestep(1.0)

	tickID := flecs.RegisterComponent[plTick](w)
	e := w.NewEntity()
	flecs.Set(w.W(), e, plTick{})

	preQ := flecs.NewCachedQuery(w, tickID)
	flecs.NewSystemInPhase(w, w.PreUpdate(), preQ, func(dt float32, it *flecs.QueryIter) {
		fmt.Println("pre-update")
	})

	fixQ := flecs.NewCachedQuery(w, tickID)
	flecs.NewSystemInPhase(w, w.OnFixedUpdate(), fixQ, func(dt float32, it *flecs.QueryIter) {
		fmt.Println("on-fixed-update")
	})

	onQ := flecs.NewCachedQuery(w, tickID)
	flecs.NewSystem(w, onQ, func(dt float32, it *flecs.QueryIter) {
		fmt.Println("on-update")
	})

	postQ := flecs.NewCachedQuery(w, tickID)
	flecs.NewSystemInPhase(w, w.PostUpdate(), postQ, func(dt float32, it *flecs.QueryIter) {
		fmt.Println("post-update")
	})

	w.Progress(1.0)

	// Output:
	// pre-update
	// on-fixed-update
	// on-update
	// post-update
}
