package docs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// TestRelationships_BasicPair verifies the introduction snippet from Relationships.md:
// Bob Likes Alice (AddID pair) and then removes it (RemoveID).
func TestRelationships_BasicPair(t *testing.T) {
	w := flecs.New()

	var likes, bob, alice flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likes = fw.NewEntity()
		bob = fw.NewEntity()
		alice = fw.NewEntity()

		fw.AddID(bob, flecs.MakePair(likes, alice))
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, bob, flecs.MakePair(likes, alice)) {
			t.Error("bob should have (likes, alice) after AddID")
		}
	})

	w.Write(func(fw *flecs.Writer) {
		fw.RemoveID(bob, flecs.MakePair(likes, alice))
	})

	w.Read(func(r *flecs.Reader) {
		if flecs.HasID(r, bob, flecs.MakePair(likes, alice)) {
			t.Error("bob should not have (likes, alice) after RemoveID")
		}
	})
}

// TestRelationships_MultipleTargets verifies that the same relationship can be added
// multiple times with different targets (Bob Eats Apples and Bob Eats Pears).
func TestRelationships_MultipleTargets(t *testing.T) {
	w := flecs.New()

	var eats, bob, apples, pears flecs.ID
	w.Write(func(fw *flecs.Writer) {
		eats = fw.NewEntity()
		bob = fw.NewEntity()
		apples = fw.NewEntity()
		pears = fw.NewEntity()

		fw.AddID(bob, flecs.MakePair(eats, apples))
		fw.AddID(bob, flecs.MakePair(eats, pears))
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, bob, flecs.MakePair(eats, apples)) {
			t.Error("bob should have (eats, apples)")
		}
		if !flecs.HasID(r, bob, flecs.MakePair(eats, pears)) {
			t.Error("bob should have (eats, pears)")
		}
	})
}

// TestRelationships_TagPairOps verifies the Pair IDs section of Relationships.md:
// MakePair + fw.AddID / fw.RemoveID / flecs.HasID for tag pairs.
func TestRelationships_TagPairOps(t *testing.T) {
	w := flecs.New()

	var rel, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()

		pairID := flecs.MakePair(rel, tgt)
		fw.AddID(e, pairID)
	})

	w.Read(func(r *flecs.Reader) {
		has := flecs.HasID(r, e, flecs.MakePair(rel, tgt))
		if !has {
			t.Error("entity should have pair after AddID")
		}
	})

	w.Write(func(fw *flecs.Writer) {
		fw.RemoveID(e, flecs.MakePair(rel, tgt))
	})

	w.Read(func(r *flecs.Reader) {
		if flecs.HasID(r, e, flecs.MakePair(rel, tgt)) {
			t.Error("entity should not have pair after RemoveID")
		}
	})
}

// TestRelationships_HasPair verifies the HasID pair check in the relationship queries section.
func TestRelationships_HasPair(t *testing.T) {
	w := flecs.New()

	var eats, bob, apples flecs.ID
	w.Write(func(fw *flecs.Writer) {
		eats = fw.NewEntity()
		bob = fw.NewEntity()
		apples = fw.NewEntity()
		fw.AddID(bob, flecs.MakePair(eats, apples))
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, bob, flecs.MakePair(eats, apples)) {
			t.Error("HasID should return true for (eats, apples)")
		}
	})
}

// TestRelationships_QueryByPair verifies the NewQueryFromTerms snippet from Relationships.md:
// find all entities with a specific pair using With(MakePair(rel, tgt)).
func TestRelationships_QueryByPair(t *testing.T) {
	w := flecs.New()

	var eats, apples, bob, carol, dave flecs.ID
	w.Write(func(fw *flecs.Writer) {
		eats = fw.NewEntity()
		apples = fw.NewEntity()
		bob = fw.NewEntity()
		carol = fw.NewEntity()
		dave = fw.NewEntity()

		fw.AddID(bob, flecs.MakePair(eats, apples))
		fw.AddID(carol, flecs.MakePair(eats, apples))
		// dave has no pair
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(eats, apples)))
	var matched []flecs.ID
	it := q.Iter()
	for it.Next() {
		matched = append(matched, it.Entities()...)
	}

	if len(matched) != 2 {
		t.Fatalf("want 2 entities matched, got %d", len(matched))
	}
	for _, e := range matched {
		if e == dave {
			t.Error("dave (no pair) should not be matched")
		}
	}
}

// TestRelationships_SetGetPair verifies the SetPair[T] + GetPair[T] snippet from Relationships.md.
func TestRelationships_SetGetPair(t *testing.T) {
	type Distance struct{ Meters float32 }

	w := flecs.New()

	var near, bob, office flecs.ID
	w.Write(func(fw *flecs.Writer) {
		near = fw.NewEntity()
		bob = fw.NewEntity()
		office = fw.NewEntity()

		flecs.SetPair(fw, bob, near, office, Distance{Meters: 500})
	})

	w.Read(func(r *flecs.Reader) {
		d, ok := flecs.GetPair[Distance](r, bob, near, office)
		if !ok {
			t.Fatal("GetPair should return ok=true")
		}
		if d.Meters != 500 {
			t.Errorf("GetPair Meters = %.0f, want 500", d.Meters)
		}
	})
}

// TestRelationships_GetPairRef verifies GetPairRef[T] from Relationships.md.
func TestRelationships_GetPairRef(t *testing.T) {
	type Distance struct{ Meters float32 }

	w := flecs.New()

	var near, bob, office flecs.ID
	w.Write(func(fw *flecs.Writer) {
		near = fw.NewEntity()
		bob = fw.NewEntity()
		office = fw.NewEntity()

		flecs.SetPair(fw, bob, near, office, Distance{Meters: 200})
	})

	w.Read(func(r *flecs.Reader) {
		ptr := flecs.GetPairRef[Distance](r, bob, near, office)
		if ptr == nil {
			t.Fatal("GetPairRef should return non-nil pointer")
		}
		if ptr.Meters != 200 {
			t.Errorf("GetPairRef Meters = %.0f, want 200", ptr.Meters)
		}
	})
}

// TestRelationships_ComponentTwice verifies the "add component multiple times via pairs"
// snippet from Relationships.md: same component type, different pair targets.
func TestRelationships_ComponentTwice(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()

	var first, second, third, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		first = fw.NewEntity()
		second = fw.NewEntity()
		third = fw.NewEntity()
		e = fw.NewEntity()

		flecs.SetPair(fw, e, first, first, Position{X: 1, Y: 2})
		flecs.SetPair(fw, e, second, second, Position{X: 3, Y: 4})
		flecs.SetPair(fw, e, third, third, Position{X: 5, Y: 6})
	})

	w.Read(func(r *flecs.Reader) {
		p1, ok1 := flecs.GetPair[Position](r, e, first, first)
		p2, ok2 := flecs.GetPair[Position](r, e, second, second)
		p3, ok3 := flecs.GetPair[Position](r, e, third, third)

		if !ok1 || p1.X != 1 || p1.Y != 2 {
			t.Errorf("pair (first,first) = %+v ok=%v, want {1 2} true", p1, ok1)
		}
		if !ok2 || p2.X != 3 || p2.Y != 4 {
			t.Errorf("pair (second,second) = %+v ok=%v, want {3 4} true", p2, ok2)
		}
		if !ok3 || p3.X != 5 || p3.Y != 6 {
			t.Errorf("pair (third,third) = %+v ok=%v, want {5 6} true", p3, ok3)
		}
	})
}

// TestRelationships_InspectPairs verifies the EntityComponents pair inspection snippet
// from Relationships.md.
func TestRelationships_InspectPairs(t *testing.T) {
	w := flecs.New()

	var rel1, rel2, tgt, bob flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel1 = fw.NewEntity()
		rel2 = fw.NewEntity()
		tgt = fw.NewEntity()
		bob = fw.NewEntity()

		fw.AddID(bob, flecs.MakePair(rel1, tgt))
		fw.AddID(bob, flecs.MakePair(rel2, tgt))
	})

	w.Read(func(r *flecs.Reader) {
		pairCount := 0
		for _, id := range r.EntityComponents(bob) {
			if id.IsPair() {
				pairCount++
				_ = id.First()  // relationship index
				_ = id.Second() // target index
			}
		}
		if pairCount != 2 {
			t.Errorf("want 2 pair IDs on bob, got %d", pairCount)
		}
	})
}

// TestRelationships_IsA_Basic verifies the basic IsA snippet from Relationships.md:
// Apple IsA Fruit via MakePair(w.IsA(), fruit).
func TestRelationships_IsA_Basic(t *testing.T) {
	w := flecs.New()

	var apple, fruit flecs.ID
	w.Write(func(fw *flecs.Writer) {
		apple = fw.NewEntity()
		fruit = fw.NewEntity()

		fw.AddID(apple, flecs.MakePair(w.IsA(), fruit))
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, apple, flecs.MakePair(w.IsA(), fruit)) {
			t.Error("apple should have (IsA, fruit)")
		}
	})
}

// TestRelationships_IsA_ComponentSharing verifies the Spaceship/Frigate component
// inheritance snippet from Relationships.md.
func TestRelationships_IsA_ComponentSharing(t *testing.T) {
	type MaxSpeed struct{ Value float32 }
	type Defense struct{ Value float32 }

	w := flecs.New()
	flecs.RegisterComponent[MaxSpeed](w)
	flecs.RegisterComponent[Defense](w)
	flecs.SetInheritable[MaxSpeed](w)
	flecs.SetInheritable[Defense](w)

	var spaceship, frigate flecs.ID
	w.Write(func(fw *flecs.Writer) {
		spaceship = fw.NewEntity()
		flecs.Set(fw, spaceship, MaxSpeed{Value: 100})
		flecs.Set(fw, spaceship, Defense{Value: 50})

		frigate = fw.NewEntity()
		fw.AddID(frigate, flecs.MakePair(w.IsA(), spaceship))
		flecs.Set(fw, frigate, Defense{Value: 75}) // override
	})

	w.Read(func(r *flecs.Reader) {
		// Frigate inherits MaxSpeed from Spaceship.
		ms, ok := flecs.Get[MaxSpeed](r, frigate)
		if !ok || ms.Value != 100 {
			t.Errorf("MaxSpeed = %+v ok=%v, want {100} true", ms, ok)
		}

		// Frigate overrides Defense.
		def, ok := flecs.Get[Defense](r, frigate)
		if !ok || def.Value != 75 {
			t.Errorf("Defense = %+v ok=%v, want {75} true", def, ok)
		}
	})
}

// TestRelationships_IsA_EachPrefab verifies the EachPrefab snippet from Relationships.md.
func TestRelationships_IsA_EachPrefab(t *testing.T) {
	w := flecs.New()

	var apple, fruit flecs.ID
	w.Write(func(fw *flecs.Writer) {
		fruit = fw.NewEntity()
		apple = fw.NewEntity()
		fw.AddID(apple, flecs.MakePair(w.IsA(), fruit))
	})

	w.Read(func(r *flecs.Reader) {
		var prefabs []flecs.ID
		r.EachPrefab(apple, func(p flecs.ID) bool {
			prefabs = append(prefabs, p)
			return true
		})
		if len(prefabs) != 1 || prefabs[0] != fruit {
			t.Errorf("EachPrefab = %v, want [fruit=%d]", prefabs, fruit)
		}
	})
}

// TestRelationships_ChildOf_Basic verifies the basic ChildOf snippet from Relationships.md.
func TestRelationships_ChildOf_Basic(t *testing.T) {
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
	})
}

// TestRelationships_ChildOf_EachChildParentOf verifies EachChild and ParentOf
// from Relationships.md.
func TestRelationships_ChildOf_EachChildParentOf(t *testing.T) {
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
		// Iterate children.
		var children []flecs.ID
		r.EachChild(spaceship, func(child flecs.ID) bool {
			children = append(children, child)
			return true
		})
		if len(children) != 2 {
			t.Fatalf("want 2 children, got %d", len(children))
		}

		// ParentOf.
		parent, ok := r.ParentOf(cockpit)
		if !ok || parent != spaceship {
			t.Errorf("ParentOf(cockpit) = (%d, %v), want (%d, true)", parent, ok, spaceship)
		}
	})
}

// TestRelationships_ChildOf_Namespacing verifies the Lookup/LookupChild namespacing
// snippet from Relationships.md.
func TestRelationships_ChildOf_Namespacing(t *testing.T) {
	w := flecs.New()

	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		child = fw.NewEntity()
		w.SetName(parent, "Spaceship")
		w.SetName(child, "Cockpit")
		fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))
	})

	// Absolute path lookup.
	found, ok := w.Lookup("Spaceship.Cockpit")
	if !ok || found != child {
		t.Errorf("Lookup(\"Spaceship.Cockpit\") = (%d, %v), want (%d, true)", found, ok, child)
	}

	// Relative lookup from parent.
	found2, ok2 := w.LookupChild(parent, "Cockpit")
	if !ok2 || found2 != child {
		t.Errorf("LookupChild(parent, \"Cockpit\") = (%d, %v), want (%d, true)", found2, ok2, child)
	}
}

// TestRelationships_GetUp verifies the GetUp traversal snippet from Relationships.md.
// A child entity inherits a component from its parent via the ChildOf relationship.
func TestRelationships_GetUp(t *testing.T) {
	type Tag struct{ Value int }

	w := flecs.New()
	flecs.RegisterComponent[Tag](w)
	flecs.SetInheritable[Tag](w)

	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.Set(fw, parent, Tag{Value: 42})

		child = fw.NewEntity()
		fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))
	})

	w.Read(func(r *flecs.Reader) {
		v, ok := flecs.GetUp[Tag](r, child, w.ChildOf())
		if !ok {
			t.Fatal("GetUp should find Tag on parent")
		}
		if v.Value != 42 {
			t.Errorf("GetUp Tag.Value = %d, want 42", v.Value)
		}
	})
}

// TestRelationships_HasUp verifies the HasUp traversal snippet from Relationships.md.
func TestRelationships_HasUp(t *testing.T) {
	type Tag struct{}

	w := flecs.New()
	tagID := flecs.RegisterComponent[Tag](w)
	flecs.SetInheritable[Tag](w)

	var parent, child, lone flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.Set(fw, parent, Tag{})

		child = fw.NewEntity()
		fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))

		lone = fw.NewEntity() // no parent, no Tag
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.HasUp(r, child, tagID, w.ChildOf()) {
			t.Error("HasUp should be true for child (Tag on parent)")
		}
		if flecs.HasUp(r, lone, tagID, w.ChildOf()) {
			t.Error("HasUp should be false for lone entity")
		}
	})
}

// TestRelationships_TargetUp verifies TargetUp from Relationships.md:
// finds the ancestor that directly owns the component.
func TestRelationships_TargetUp(t *testing.T) {
	type Tag struct{}

	w := flecs.New()
	tagID := flecs.RegisterComponent[Tag](w)
	flecs.SetInheritable[Tag](w)

	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.Set(fw, parent, Tag{})

		child = fw.NewEntity()
		fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))
	})

	w.Read(func(r *flecs.Reader) {
		owner, ok := flecs.TargetUp(r, child, tagID, w.ChildOf())
		if !ok {
			t.Fatal("TargetUp should find owner")
		}
		if owner != parent {
			t.Errorf("TargetUp owner = %d, want parent (%d)", owner, parent)
		}
	})
}

// TestRelationships_QueryTraversal verifies the query traversal snippets from Relationships.md:
// Up, SelfUp, and Cascade traversal terms.
func TestRelationships_QueryTraversal(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var root, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		root = fw.NewEntity()
		flecs.Set(fw, root, Position{X: 0, Y: 0})

		child = fw.NewEntity()
		fw.AddID(child, flecs.MakePair(w.ChildOf(), root))
		// child has no Position — must inherit via Up
	})

	// Up: matches child (inherits Position from root via ChildOf).
	qUp := flecs.NewQueryFromTerms(w, flecs.With(posID).Up(w.ChildOf()))
	var upMatched []flecs.ID
	it := qUp.Iter()
	for it.Next() {
		upMatched = append(upMatched, it.Entities()...)
	}
	if len(upMatched) != 1 || upMatched[0] != child {
		t.Errorf("Up query: want [child], got %v", upMatched)
	}

	// SelfUp: matches both root (owns Position) and child (inherits via ChildOf).
	qSelfUp := flecs.NewQueryFromTerms(w, flecs.With(posID).SelfUp(w.ChildOf()))
	var selfUpMatched []flecs.ID
	it2 := qSelfUp.Iter()
	for it2.Next() {
		selfUpMatched = append(selfUpMatched, it2.Entities()...)
	}
	if len(selfUpMatched) != 2 {
		t.Errorf("SelfUp query: want 2 entities, got %d", len(selfUpMatched))
	}

	// Cascade: root-before-child depth ordering (CachedQuery only).
	cq := flecs.NewCachedQueryFromTerms(w, flecs.With(posID).Cascade(w.ChildOf()))
	var cascadeOrder []flecs.ID
	cq.Each(func(it *flecs.QueryIter) {
		cascadeOrder = append(cascadeOrder, it.Entities()...)
	})
	if len(cascadeOrder) != 2 || cascadeOrder[0] != root {
		t.Errorf("Cascade query: want [root, child], got %v", cascadeOrder)
	}
}
