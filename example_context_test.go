package flecs_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/snichols/flecs"
)

type ctxPosition struct{ X float32 }

// ExampleWorld_ProgressContext demonstrates cooperative context cancellation.
// A pre-cancelled context causes ProgressContext to return context.Canceled
// immediately, before executing any system.
func ExampleWorld_ProgressContext() {
	w := flecs.New()
	posID := flecs.RegisterComponent[ctxPosition](w)

	w.Write(func(fw *flecs.Writer) {
		for i := range 5 {
			e := fw.NewEntity()
			flecs.Set(fw, e, ctxPosition{X: float32(i)})
		}
	})

	// Register a system that would normally run each frame.
	q := flecs.NewCachedQuery(w, posID)
	flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			pos := flecs.Field[ctxPosition](it, posID)
			for i := range pos {
				pos[i].X += dt
			}
		}
	})

	// A pre-cancelled context: ProgressContext returns immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := w.ProgressContext(ctx, 1.0/60.0)
	if errors.Is(err, context.Canceled) {
		fmt.Println("tick skipped: context already cancelled")
	}

	// With a live context, the tick runs normally.
	err = w.ProgressContext(context.Background(), 1.0/60.0)
	fmt.Printf("normal tick err=%v frame=%d\n", err, w.FrameCount())

	// Output:
	// tick skipped: context already cancelled
	// normal tick err=<nil> frame=1
}
