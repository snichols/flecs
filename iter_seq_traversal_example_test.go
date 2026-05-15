package flecs_test

import (
	"fmt"
	"sort"
	"unsafe"

	"github.com/snichols/flecs"
)

// ExampleReader_Entities demonstrates iterating all alive entities in a world.
func ExampleReader_Entities() {
	w := flecs.New()

	var a, b, c flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
		b = fw.NewEntity()
		c = fw.NewEntity()
		w.SetName(a, "alpha")
		w.SetName(b, "beta")
		w.SetName(c, "gamma")
	})

	var names []string
	w.Read(func(r *flecs.Reader) {
		for e := range r.Entities() {
			if name, ok := r.GetName(e); ok {
				names = append(names, name)
			}
		}
	})
	sort.Strings(names)
	for _, n := range names {
		fmt.Println(n)
	}

	// Output:
	// alpha
	// beta
	// gamma
}

// ExampleReader_Children demonstrates iterating direct children of a parent
// using the [OrderedChildren] trait to guarantee insertion order.
func ExampleReader_Children() {
	w := flecs.New()

	var parent flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.SetOrderedChildren(w, parent)
		for _, name := range []string{"child-1", "child-2", "child-3"} {
			c := fw.NewEntity()
			w.SetName(c, name)
			fw.AddID(c, flecs.MakePair(w.ChildOf(), parent))
		}
	})

	w.Read(func(r *flecs.Reader) {
		for child := range r.Children(parent) {
			name, _ := r.GetName(child)
			fmt.Println(name)
		}
	})

	// Output:
	// child-1
	// child-2
	// child-3
}

// ExampleReader_Prefabs demonstrates iterating the direct IsA prefabs of an entity.
func ExampleReader_Prefabs() {
	w := flecs.New()

	var prefabA, prefabB, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefabA = fw.NewEntity()
		prefabB = fw.NewEntity()
		w.SetName(prefabA, "PrefabA")
		w.SetName(prefabB, "PrefabB")
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(w.IsA(), prefabA))
		fw.AddID(e, flecs.MakePair(w.IsA(), prefabB))
	})

	var names []string
	w.Read(func(r *flecs.Reader) {
		for p := range r.Prefabs(e) {
			if name, ok := r.GetName(p); ok {
				names = append(names, name)
			}
		}
	})
	sort.Strings(names)
	for _, n := range names {
		fmt.Println(n)
	}

	// Output:
	// PrefabA
	// PrefabB
}

// ExampleReader_Systems demonstrates iterating systems registered in a phase.
func ExampleReader_Systems() {
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)
	q := flecs.NewCachedQuery(w, posID)

	flecs.NewSystem(w, q, func(_ float32, _ *flecs.QueryIter) {})
	flecs.NewSystem(w, q, func(_ float32, _ *flecs.QueryIter) {})
	flecs.NewSystem(w, q, func(_ float32, _ *flecs.QueryIter) {})

	count := 0
	w.Read(func(r *flecs.Reader) {
		for range r.Systems(w.OnUpdate()) {
			count++
		}
	})
	fmt.Println("systems in phase:", count)

	// Output:
	// systems in phase: 3
}

// ExampleUnion demonstrates iterating all active (entity, target) union pairs
// for a relationship.
func ExampleUnion() {
	w := flecs.New()

	var movement flecs.ID
	var walking, running, idle flecs.ID
	w.Write(func(fw *flecs.Writer) {
		movement = fw.NewEntity()
		walking = fw.NewEntity()
		running = fw.NewEntity()
		idle = fw.NewEntity()
		w.SetName(walking, "Walking")
		w.SetName(running, "Running")
		w.SetName(idle, "Idle")
	})
	flecs.SetUnion(w, movement)

	var soldier, scout flecs.ID
	w.Write(func(fw *flecs.Writer) {
		soldier = fw.NewEntity()
		w.SetName(soldier, "soldier")
		fw.AddID(soldier, flecs.MakePair(movement, walking))

		scout = fw.NewEntity()
		w.SetName(scout, "scout")
		fw.AddID(scout, flecs.MakePair(movement, running))
	})

	type row struct{ entity, target string }
	var rows []row
	w.Read(func(r *flecs.Reader) {
		for e, tgt := range flecs.Union(r, movement) {
			eName, _ := r.GetName(e)
			tName, _ := r.GetName(tgt)
			rows = append(rows, row{eName, tName})
		}
	})
	sort.Slice(rows, func(i, j int) bool { return rows[i].entity < rows[j].entity })
	for _, row := range rows {
		fmt.Printf("%s -> %s\n", row.entity, row.target)
	}
	_ = idle

	// Output:
	// scout -> Running
	// soldier -> Walking
}

type exTravPos struct{ X, Y float32 }

// ExampleSparse demonstrates iterating all holders of a Sparse component.
func ExampleSparse() {
	w := flecs.New()
	posID := flecs.RegisterComponent[exTravPos](w)
	flecs.SetSparse(w, posID)

	w.Write(func(fw *flecs.Writer) {
		e1 := fw.NewEntity()
		flecs.Set(fw, e1, exTravPos{X: 1, Y: 2})
		e2 := fw.NewEntity()
		flecs.Set(fw, e2, exTravPos{X: 3, Y: 4})
	})

	w.Read(func(r *flecs.Reader) {
		for _, p := range flecs.Sparse[exTravPos](r) {
			fmt.Printf("%.0f,%.0f\n", p.X, p.Y)
		}
	})

	// Output:
	// 1,2
	// 3,4
}

// ExampleByID demonstrates iterating all holders of a component by ID using a
// dynamic component (unknown Go type at compile time).
func ExampleByID() {
	w := flecs.New()
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "example/Score", 4, 4)
	})

	// Write score values as uint32 bytes.
	scores := []uint32{10, 20, 30}
	w.Write(func(fw *flecs.Writer) {
		for _, score := range scores {
			e := fw.NewEntity()
			s := score
			flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&s))
		}
	})

	var got []uint32
	w.Read(func(r *flecs.Reader) {
		for _, ptr := range flecs.ByID(r, dynID) {
			got = append(got, *(*uint32)(ptr))
		}
	})
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	for _, v := range got {
		fmt.Println(v)
	}

	// Output:
	// 10
	// 20
	// 30
}
