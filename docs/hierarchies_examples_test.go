package docs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// TestHierarchies_CreateParentChild verifies the introduction snippet from HierarchiesManual.md:
// create a spaceship/cockpit hierarchy with AddID and verify with HasID.
func TestHierarchies_CreateParentChild(t *testing.T) {
	w := flecs.New()

	var spaceship, cockpit flecs.ID
	w.Write(func(fw *flecs.Writer) {
		spaceship = fw.NewEntity()
		cockpit = fw.NewEntity()

		fw.AddID(cockpit, flecs.MakePair(w.ChildOf(), spaceship))
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, cockpit, flecs.MakePair(w.ChildOf(), spaceship)) {
			t.Error("cockpit should have (ChildOf, spaceship)")
		}
		// spaceship has no parent
		if flecs.HasID(r, spaceship, flecs.MakePair(w.ChildOf(), cockpit)) {
			t.Error("spaceship should not have (ChildOf, cockpit)")
		}
	})
}

// TestHierarchies_ParentOf verifies the ParentOf snippet from HierarchiesManual.md.
func TestHierarchies_ParentOf(t *testing.T) {
	w := flecs.New()

	var spaceship, cockpit flecs.ID
	w.Write(func(fw *flecs.Writer) {
		spaceship = fw.NewEntity()
		cockpit = fw.NewEntity()
		fw.AddID(cockpit, flecs.MakePair(w.ChildOf(), spaceship))
	})

	w.Read(func(r *flecs.Reader) {
		parent, ok := r.ParentOf(cockpit)
		if !ok || parent != spaceship {
			t.Errorf("ParentOf(cockpit) = (%d, %v), want (%d, true)", parent, ok, spaceship)
		}

		// entity with no parent returns (0, false)
		_, ok2 := r.ParentOf(spaceship)
		if ok2 {
			t.Error("ParentOf on root entity should return ok=false")
		}
	})
}

// TestHierarchies_EachChild verifies the EachChild snippet from HierarchiesManual.md:
// iterates direct children only; early-exit with return false works.
func TestHierarchies_EachChild(t *testing.T) {
	w := flecs.New()

	var spaceship, cockpit, engine flecs.ID
	w.Write(func(fw *flecs.Writer) {
		spaceship = fw.NewEntity()
		cockpit = fw.NewEntity()
		engine = fw.NewEntity()

		fw.AddID(cockpit, flecs.MakePair(w.ChildOf(), spaceship))
		fw.AddID(engine, flecs.MakePair(w.ChildOf(), spaceship))
	})

	w.Read(func(r *flecs.Reader) {
		var children []flecs.ID
		r.EachChild(spaceship, func(child flecs.ID) bool {
			children = append(children, child)
			return true
		})
		if len(children) != 2 {
			t.Fatalf("want 2 children, got %d", len(children))
		}

		// Early-exit: stop after finding one child.
		count := 0
		r.EachChild(spaceship, func(child flecs.ID) bool {
			count++
			return false // stop immediately
		})
		if count != 1 {
			t.Errorf("early-exit: want 1 iteration, got %d", count)
		}
	})
}

// TestHierarchies_CascadeDelete verifies the cascade delete snippet from HierarchiesManual.md:
// deleting a parent also deletes all descendants.
func TestHierarchies_CascadeDelete(t *testing.T) {
	w := flecs.New()

	var spaceship, cockpit, pilot flecs.ID
	w.Write(func(fw *flecs.Writer) {
		spaceship = fw.NewEntity()
		cockpit = fw.NewEntity()
		pilot = fw.NewEntity()

		fw.AddID(cockpit, flecs.MakePair(w.ChildOf(), spaceship))
		fw.AddID(pilot, flecs.MakePair(w.ChildOf(), cockpit))
	})

	w.Write(func(fw *flecs.Writer) {
		fw.Delete(spaceship)
	})

	w.Read(func(r *flecs.Reader) {
		if r.IsAlive(spaceship) {
			t.Error("spaceship should be dead after Delete")
		}
		if r.IsAlive(cockpit) {
			t.Error("cockpit should be dead (child of spaceship)")
		}
		if r.IsAlive(pilot) {
			t.Error("pilot should be dead (grandchild of spaceship)")
		}
	})
}

// TestHierarchies_DepthFirstTraversal verifies the depth-first traversal snippet from
// HierarchiesManual.md: recursive EachChild visits every node in the subtree.
func TestHierarchies_DepthFirstTraversal(t *testing.T) {
	w := flecs.New()

	var root, childA, childB, grandchild flecs.ID
	w.Write(func(fw *flecs.Writer) {
		root = fw.NewEntity()
		childA = fw.NewEntity()
		childB = fw.NewEntity()
		grandchild = fw.NewEntity()

		fw.AddID(childA, flecs.MakePair(w.ChildOf(), root))
		fw.AddID(childB, flecs.MakePair(w.ChildOf(), root))
		fw.AddID(grandchild, flecs.MakePair(w.ChildOf(), childA))
	})

	var visitDepthFirst func(r *flecs.Reader, e flecs.ID, depth int)
	var visited []flecs.ID
	visitDepthFirst = func(r *flecs.Reader, e flecs.ID, depth int) {
		r.EachChild(e, func(child flecs.ID) bool {
			visited = append(visited, child)
			visitDepthFirst(r, child, depth+1)
			return true
		})
	}

	w.Read(func(r *flecs.Reader) {
		visitDepthFirst(r, root, 0)
	})

	// All 3 non-root entities should be visited.
	if len(visited) != 3 {
		t.Fatalf("depth-first traversal: want 3 visited, got %d: %v", len(visited), visited)
	}
	// grandchild must appear after childA (depth-first: childA's subtree before childB).
	foundChildA, foundGrandchild := -1, -1
	for i, e := range visited {
		if e == childA {
			foundChildA = i
		}
		if e == grandchild {
			foundGrandchild = i
		}
	}
	if foundChildA < 0 || foundGrandchild < 0 {
		t.Fatalf("childA or grandchild not in visited list")
	}
	if foundGrandchild < foundChildA {
		t.Error("grandchild should be visited after childA in depth-first order")
	}
}

// TestHierarchies_BreadthFirstCascade verifies the breadth-first traversal snippet from
// HierarchiesManual.md: CachedQuery with Cascade delivers root before child.
func TestHierarchies_BreadthFirstCascade(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var root, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		root = fw.NewEntity()
		flecs.Set(fw, root, Position{X: 0, Y: 0})

		child = fw.NewEntity()
		fw.AddID(child, flecs.MakePair(w.ChildOf(), root))
		flecs.Set(fw, child, Position{X: 1, Y: 1})
	})

	// Cascade guarantees root-first depth ordering over the ChildOf hierarchy.
	cq := flecs.NewCachedQueryFromTerms(w, flecs.With(posID).Cascade(w.ChildOf()))

	var order []flecs.ID
	cq.Each(func(it *flecs.QueryIter) {
		order = append(order, it.Entities()...)
	})

	if len(order) != 2 {
		t.Fatalf("want 2 entities, got %d", len(order))
	}
	if order[0] != root {
		t.Errorf("Cascade: want root first, got %d (root=%d)", order[0], root)
	}
	if order[1] != child {
		t.Errorf("Cascade: want child second, got %d (child=%d)", order[1], child)
	}
}

// TestHierarchies_SetGetName verifies the SetName/GetName snippet from HierarchiesManual.md.
func TestHierarchies_SetGetName(t *testing.T) {
	w := flecs.New()

	var game, level flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		level = fw.NewEntity()
		fw.AddID(level, flecs.MakePair(w.ChildOf(), game))
	})

	w.SetName(game, "Game")
	w.SetName(level, "Level1")

	w.Read(func(r *flecs.Reader) {
		name, ok := r.GetName(level)
		if !ok || name != "Level1" {
			t.Errorf("GetName(level) = (%q, %v), want (\"Level1\", true)", name, ok)
		}

		gameName, ok2 := r.GetName(game)
		if !ok2 || gameName != "Game" {
			t.Errorf("GetName(game) = (%q, %v), want (\"Game\", true)", gameName, ok2)
		}
	})
}

// TestHierarchies_PathOf verifies the PathOf snippet from HierarchiesManual.md.
func TestHierarchies_PathOf(t *testing.T) {
	w := flecs.New()

	var game, level flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		level = fw.NewEntity()
		fw.AddID(level, flecs.MakePair(w.ChildOf(), game))
	})

	w.SetName(game, "Game")
	w.SetName(level, "Level1")

	w.Read(func(r *flecs.Reader) {
		path := r.PathOf(level)
		if path != "Game.Level1" {
			t.Errorf("PathOf(level) = %q, want \"Game.Level1\"", path)
		}

		// Root entity: just its own name.
		rootPath := r.PathOf(game)
		if rootPath != "Game" {
			t.Errorf("PathOf(game) = %q, want \"Game\"", rootPath)
		}
	})
}

// TestHierarchies_Lookup verifies the Lookup snippet from HierarchiesManual.md.
func TestHierarchies_Lookup(t *testing.T) {
	w := flecs.New()

	var game, level flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		level = fw.NewEntity()
		fw.AddID(level, flecs.MakePair(w.ChildOf(), game))
	})

	w.SetName(game, "Game")
	w.SetName(level, "Level1")

	w.Read(func(r *flecs.Reader) {
		e, ok := r.Lookup("Game.Level1")
		if !ok || e != level {
			t.Errorf("Lookup(\"Game.Level1\") = (%d, %v), want (%d, true)", e, ok, level)
		}

		// Missing segment returns (0, false).
		_, ok2 := r.Lookup("Game.Missing")
		if ok2 {
			t.Error("Lookup on missing segment should return ok=false")
		}
	})
}

// TestHierarchies_LookupChild verifies the LookupChild snippet from HierarchiesManual.md.
func TestHierarchies_LookupChild(t *testing.T) {
	w := flecs.New()

	var game, level flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		level = fw.NewEntity()
		fw.AddID(level, flecs.MakePair(w.ChildOf(), game))
	})

	w.SetName(game, "Game")
	w.SetName(level, "Level1")

	w.Read(func(r *flecs.Reader) {
		e, ok := r.LookupChild(game, "Level1")
		if !ok || e != level {
			t.Errorf("LookupChild(game, \"Level1\") = (%d, %v), want (%d, true)", e, ok, level)
		}

		// parent=0 searches the root scope.
		root, ok2 := r.LookupChild(0, "Game")
		if !ok2 || root != game {
			t.Errorf("LookupChild(0, \"Game\") = (%d, %v), want (%d, true)", root, ok2, game)
		}
	})
}

// TestHierarchies_Reparent verifies the reparenting snippet from HierarchiesManual.md:
// remove old ChildOf pair, add new one; children follow automatically.
func TestHierarchies_Reparent(t *testing.T) {
	w := flecs.New()

	var spaceship, station, cockpit, seat flecs.ID
	w.Write(func(fw *flecs.Writer) {
		spaceship = fw.NewEntity()
		station = fw.NewEntity()
		cockpit = fw.NewEntity()
		seat = fw.NewEntity()

		fw.AddID(cockpit, flecs.MakePair(w.ChildOf(), spaceship))
		fw.AddID(seat, flecs.MakePair(w.ChildOf(), cockpit))
	})

	// Reparent cockpit from spaceship to station.
	w.Write(func(fw *flecs.Writer) {
		fw.RemoveID(cockpit, flecs.MakePair(w.ChildOf(), spaceship))
		fw.AddID(cockpit, flecs.MakePair(w.ChildOf(), station))
	})

	w.Read(func(r *flecs.Reader) {
		// cockpit now belongs to station
		parent, ok := r.ParentOf(cockpit)
		if !ok || parent != station {
			t.Errorf("after reparent: ParentOf(cockpit) = (%d, %v), want (%d, true)", parent, ok, station)
		}

		// cockpit is no longer a child of spaceship
		if flecs.HasID(r, cockpit, flecs.MakePair(w.ChildOf(), spaceship)) {
			t.Error("cockpit should no longer have (ChildOf, spaceship)")
		}

		// seat still belongs to cockpit (children follow their parent automatically)
		seatParent, ok2 := r.ParentOf(seat)
		if !ok2 || seatParent != cockpit {
			t.Errorf("ParentOf(seat) = (%d, %v), want (%d, true)", seatParent, ok2, cockpit)
		}
	})
}

// TestHierarchies_GetUp verifies the GetUp snippet from HierarchiesManual.md:
// GetUp[T] retrieves a component from the closest ancestor that owns it.
func TestHierarchies_GetUp(t *testing.T) {
	type Zone struct{ Name string }

	w := flecs.New()
	flecs.RegisterComponent[Zone](w)

	var region, city flecs.ID
	w.Write(func(fw *flecs.Writer) {
		region = fw.NewEntity()
		flecs.Set(fw, region, Zone{Name: "NorthEast"})

		city = fw.NewEntity()
		fw.AddID(city, flecs.MakePair(w.ChildOf(), region))
	})

	w.Read(func(r *flecs.Reader) {
		z, ok := flecs.GetUp[Zone](r, city, w.ChildOf())
		if !ok {
			t.Fatal("GetUp[Zone] should find Zone on ancestor")
		}
		if z.Name != "NorthEast" {
			t.Errorf("GetUp Zone.Name = %q, want \"NorthEast\"", z.Name)
		}
	})
}

// TestHierarchies_HasUp verifies the HasUp snippet from HierarchiesManual.md:
// HasUp reports whether entity or any ancestor owns a component ID.
func TestHierarchies_HasUp(t *testing.T) {
	type Zone struct{ Name string }

	w := flecs.New()
	zoneID := flecs.RegisterComponent[Zone](w)

	var region, city, lone flecs.ID
	w.Write(func(fw *flecs.Writer) {
		region = fw.NewEntity()
		flecs.Set(fw, region, Zone{Name: "NorthEast"})

		city = fw.NewEntity()
		fw.AddID(city, flecs.MakePair(w.ChildOf(), region))

		lone = fw.NewEntity() // no parent, no Zone
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.HasUp(r, city, zoneID, w.ChildOf()) {
			t.Error("HasUp should be true for city (Zone on parent region)")
		}
		if flecs.HasUp(r, lone, zoneID, w.ChildOf()) {
			t.Error("HasUp should be false for entity with no parent")
		}
	})
}

// TestHierarchies_TargetUp verifies the TargetUp snippet from HierarchiesManual.md:
// TargetUp returns the entity that directly owns the component.
func TestHierarchies_TargetUp(t *testing.T) {
	type Zone struct{ Name string }

	w := flecs.New()
	zoneID := flecs.RegisterComponent[Zone](w)

	var region, city flecs.ID
	w.Write(func(fw *flecs.Writer) {
		region = fw.NewEntity()
		flecs.Set(fw, region, Zone{Name: "NorthEast"})

		city = fw.NewEntity()
		fw.AddID(city, flecs.MakePair(w.ChildOf(), region))
	})

	w.Read(func(r *flecs.Reader) {
		owner, ok := flecs.TargetUp(r, city, zoneID, w.ChildOf())
		if !ok {
			t.Fatal("TargetUp should find the owner of Zone")
		}
		if owner != region {
			t.Errorf("TargetUp owner = %d, want region (%d)", owner, region)
		}
	})
}
