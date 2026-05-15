package flecs_test

import (
	"compress/gzip"
	"fmt"
	"os"

	"github.com/snichols/flecs"
)

type examplePos struct{ X, Y float32 }
type exampleVel struct{ VX, VY float32 }

// ExampleWorld_TakeSnapshotTo demonstrates compressed snapshot persistence
// using TakeSnapshotTo with gzip.NewWriter and an os.File, then restoring
// via ReadSnapshotFrom.
func ExampleWorld_TakeSnapshotTo() {
	w := flecs.New()
	flecs.RegisterComponent[examplePos](w)
	flecs.RegisterComponent[exampleVel](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, examplePos{X: 1.0, Y: 2.0})
		flecs.Set(fw, e, exampleVel{VX: 0.5, VY: 0.1})
	})

	// ── Write: world → gzip → file ────────────────────────────────────────────
	f, err := os.CreateTemp("", "world-*.snap.gz")
	if err != nil {
		panic(err)
	}
	name := f.Name()
	defer os.Remove(name)

	gz := gzip.NewWriter(f)
	if _, err := w.TakeSnapshotTo(gz); err != nil {
		gz.Close()
		f.Close()
		panic(err)
	}
	gz.Close()
	f.Close()

	// ── Read: file → gzip → snapshot → restore ────────────────────────────────
	f2, err := os.Open(name)
	if err != nil {
		panic(err)
	}
	defer f2.Close()

	gr, err := gzip.NewReader(f2)
	if err != nil {
		panic(err)
	}
	defer gr.Close()

	snap, err := flecs.ReadSnapshotFrom(gr)
	if err != nil {
		panic(err)
	}

	// Delete entity, then restore to verify.
	w.Delete(e)
	flecs.RestoreSnapshot(w, snap)

	w.Read(func(r *flecs.Reader) {
		pos, ok := flecs.Get[examplePos](r, e)
		if !ok {
			fmt.Println("entity missing")
			return
		}
		fmt.Printf("X=%.1f Y=%.1f\n", pos.X, pos.Y)
	})

	// Output:
	// X=1.0 Y=2.0
}
