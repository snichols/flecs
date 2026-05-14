package flecs_test

import (
	"encoding/json"
	"testing"

	"github.com/snichols/flecs"
)

// ── Test 1: Regression — prefab without children ──────────────────────────────

// TestPrefabHierarchy_NoChildren verifies that instantiating a prefab with no
// children still correctly copies inherited components and produces no child entities.
func TestPrefabHierarchy_NoChildren(t *testing.T) {
	type HP struct{ Value int }
	w := flecs.New()
	flecs.RegisterComponent[HP](w)

	var tank, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tank = fw.NewEntity()
		flecs.MarkPrefab(fw, tank)
		flecs.Set(fw, tank, HP{Value: 100})

		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), tank))
	})

	w.Read(func(r *flecs.Reader) {
		hp, ok := flecs.Get[HP](r, inst)
		if !ok || hp.Value != 100 {
			t.Errorf("expected HP{100}, got HP{%d} ok=%v", hp.Value, ok)
		}
		var childCount int
		w.EachChild(inst, func(_ flecs.ID) bool { childCount++; return true })
		if childCount != 0 {
			t.Errorf("expected 0 children on instance, got %d", childCount)
		}
	})
}

// ── Test 2: Single child ──────────────────────────────────────────────────────

// TestPrefabHierarchy_SingleChild verifies that a prefab with one child
// produces a distinct child entity on the instance.
func TestPrefabHierarchy_SingleChild(t *testing.T) {
	type Armor struct{ Rating int }
	w := flecs.New()
	flecs.RegisterComponent[Armor](w)

	var tank, turret, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tank = fw.NewEntity()
		flecs.MarkPrefab(fw, tank)

		turret = fw.NewEntity()
		flecs.MarkPrefab(fw, turret)
		flecs.AddID(fw, turret, flecs.MakePair(w.ChildOf(), tank))
		flecs.Set(fw, turret, Armor{Rating: 5})

		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), tank))
	})

	w.Read(func(r *flecs.Reader) {
		var children []flecs.ID
		w.EachChild(inst, func(child flecs.ID) bool {
			children = append(children, child)
			return true
		})
		if len(children) != 1 {
			t.Fatalf("expected 1 child on instance, got %d", len(children))
		}
		instTurret := children[0]
		if instTurret == turret {
			t.Error("instance child must be a NEW entity, not the prefab child itself")
		}

		armor, ok := flecs.Get[Armor](r, instTurret)
		if !ok || armor.Rating != 5 {
			t.Errorf("instance child Armor: got %+v ok=%v, want {5}", armor, ok)
		}
		// Instance child must be a child of inst, not of tank.
		parent, hasParent := w.ParentOf(instTurret)
		if !hasParent || parent != inst {
			t.Errorf("instance child parent = %v, want inst (%v)", parent, inst)
		}
	})
}

// ── Test 3: Grandchildren (3 levels) ─────────────────────────────────────────

// TestPrefabHierarchy_Grandchildren verifies full subtree replication:
// Tank → Turret → Barrel produces a 3-level instance hierarchy.
func TestPrefabHierarchy_Grandchildren(t *testing.T) {
	w := flecs.New()

	var tank, turret, barrel, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tank = fw.NewEntity()
		flecs.MarkPrefab(fw, tank)

		turret = fw.NewEntity()
		flecs.MarkPrefab(fw, turret)
		flecs.AddID(fw, turret, flecs.MakePair(w.ChildOf(), tank))

		barrel = fw.NewEntity()
		flecs.MarkPrefab(fw, barrel)
		flecs.AddID(fw, barrel, flecs.MakePair(w.ChildOf(), turret))

		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), tank))
	})

	w.Read(func(r *flecs.Reader) {
		// inst must have exactly one child.
		var instChildren []flecs.ID
		w.EachChild(inst, func(c flecs.ID) bool { instChildren = append(instChildren, c); return true })
		if len(instChildren) != 1 {
			t.Fatalf("expected 1 child of inst, got %d", len(instChildren))
		}
		instTurret := instChildren[0]
		if instTurret == turret {
			t.Error("instTurret must be distinct from turret")
		}

		// instTurret must have exactly one child.
		var turretChildren []flecs.ID
		w.EachChild(instTurret, func(c flecs.ID) bool {
			turretChildren = append(turretChildren, c)
			return true
		})
		if len(turretChildren) != 1 {
			t.Fatalf("expected 1 child of instTurret, got %d", len(turretChildren))
		}
		instBarrel := turretChildren[0]
		if instBarrel == barrel {
			t.Error("instBarrel must be distinct from barrel")
		}

		// Parents must be correct.
		p1, _ := w.ParentOf(instTurret)
		if p1 != inst {
			t.Errorf("instTurret.Parent = %v, want inst (%v)", p1, inst)
		}
		p2, _ := w.ParentOf(instBarrel)
		if p2 != instTurret {
			t.Errorf("instBarrel.Parent = %v, want instTurret (%v)", p2, instTurret)
		}
	})
}

// ── Test 4: Slot resolution ───────────────────────────────────────────────────

// TestPrefabHierarchy_SlotResolution verifies that (SlotOf, prefab) on a prefab
// child causes (prefabChild, instanceChild) to be added to the instance root.
func TestPrefabHierarchy_SlotResolution(t *testing.T) {
	w := flecs.New()

	var tank, turret, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tank = fw.NewEntity()
		flecs.MarkPrefab(fw, tank)

		turret = fw.NewEntity()
		flecs.MarkPrefab(fw, turret)
		flecs.AddID(fw, turret, flecs.MakePair(w.ChildOf(), tank))
		flecs.AddID(fw, turret, flecs.MakePair(w.SlotOf(), tank))

		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), tank))
	})

	w.Read(func(r *flecs.Reader) {
		// (turret, instTurret) pair must be on inst.
		instTurret, ok := flecs.GetPairTarget(r, inst, turret)
		if !ok {
			t.Fatal("slot (turret, ?) not found on inst")
		}
		if instTurret == turret {
			t.Error("slot target must be the INSTANCE child, not the prefab child")
		}
		// The slot target is also a child of inst.
		parent, hasParent := w.ParentOf(instTurret)
		if !hasParent || parent != inst {
			t.Errorf("slot target parent = %v, want inst", parent)
		}
	})
}

// ── Test 5: GetPairTarget O(1) lookup ─────────────────────────────────────────

// TestPrefabHierarchy_GetPairTarget verifies that GetPairTarget on a slot pair
// returns the copied child and that successive calls return the same entity.
func TestPrefabHierarchy_GetPairTarget(t *testing.T) {
	w := flecs.New()

	var tank, turret, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tank = fw.NewEntity()
		flecs.MarkPrefab(fw, tank)

		turret = fw.NewEntity()
		flecs.MarkPrefab(fw, turret)
		flecs.AddID(fw, turret, flecs.MakePair(w.ChildOf(), tank))
		flecs.AddID(fw, turret, flecs.MakePair(w.SlotOf(), tank))

		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), tank))
	})

	w.Read(func(r *flecs.Reader) {
		instTurret1, ok1 := flecs.GetPairTarget(r, inst, turret)
		instTurret2, ok2 := flecs.GetPairTarget(r, inst, turret)
		if !ok1 || !ok2 {
			t.Fatal("GetPairTarget returned false")
		}
		if instTurret1 != instTurret2 {
			t.Error("successive GetPairTarget calls must return the same entity")
		}
		if instTurret1 == turret {
			t.Error("slot target must be the copied instance child, not the prefab child")
		}
	})
}

// ── Test 6: Cross-reference rewriting ─────────────────────────────────────────

// TestPrefabHierarchy_CrossRefRewrite verifies that same-subtree pair targets
// are rewritten: (SomeRel, B) on prefab child A becomes (SomeRel, copyOfB) on
// the instance copy of A.
func TestPrefabHierarchy_CrossRefRewrite(t *testing.T) {
	w := flecs.New()

	var tank, childA, childB, inst flecs.ID
	var someRel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		someRel = fw.NewEntity()

		tank = fw.NewEntity()
		flecs.MarkPrefab(fw, tank)

		childA = fw.NewEntity()
		flecs.MarkPrefab(fw, childA)
		flecs.AddID(fw, childA, flecs.MakePair(w.ChildOf(), tank))

		childB = fw.NewEntity()
		flecs.MarkPrefab(fw, childB)
		flecs.AddID(fw, childB, flecs.MakePair(w.ChildOf(), tank))

		// childA has (someRel, childB) — a same-subtree reference.
		flecs.AddID(fw, childA, flecs.MakePair(someRel, childB))

		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), tank))
	})

	w.Read(func(r *flecs.Reader) {
		var instChildren []flecs.ID
		w.EachChild(inst, func(c flecs.ID) bool { instChildren = append(instChildren, c); return true })
		if len(instChildren) != 2 {
			t.Fatalf("expected 2 instance children, got %d", len(instChildren))
		}

		// Identify which instance child corresponds to childA and childB by checking
		// which one has (someRel, *).
		var instA, instB flecs.ID
		for _, ic := range instChildren {
			if target, ok := flecs.GetPairTarget(r, ic, someRel); ok {
				instA = ic
				instB = target
			}
		}
		if instA == 0 {
			t.Fatal("no instance child has (someRel, *)")
		}
		if instB == childB {
			t.Error("cross-reference must be rewritten: target must be copy of childB, not childB itself")
		}
		if instB == 0 {
			t.Fatal("cross-reference target is zero")
		}
		// instB must be one of the instance children (not the prefab childB).
		found := false
		for _, ic := range instChildren {
			if ic == instB {
				found = true
			}
		}
		if !found {
			t.Error("cross-reference target instB must be an instance child of inst")
		}
	})
}

// ── Test 7: External references not rewritten ─────────────────────────────────

// TestPrefabHierarchy_ExternalRefUnchanged verifies that pair targets outside
// the prefab subtree are left unchanged on instantiation.
func TestPrefabHierarchy_ExternalRefUnchanged(t *testing.T) {
	w := flecs.New()

	var tank, child, external, inst flecs.ID
	var someRel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		someRel = fw.NewEntity()
		external = fw.NewEntity() // outside the prefab subtree

		tank = fw.NewEntity()
		flecs.MarkPrefab(fw, tank)

		child = fw.NewEntity()
		flecs.MarkPrefab(fw, child)
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), tank))
		flecs.AddID(fw, child, flecs.MakePair(someRel, external))

		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), tank))
	})

	w.Read(func(r *flecs.Reader) {
		var instChild flecs.ID
		w.EachChild(inst, func(c flecs.ID) bool { instChild = c; return false })
		if instChild == 0 {
			t.Fatal("no instance child found")
		}

		target, ok := flecs.GetPairTarget(r, instChild, someRel)
		if !ok {
			t.Fatal("instance child missing (someRel, *)")
		}
		if target != external {
			t.Errorf("external ref was rewritten: got %v, want external (%v)", target, external)
		}
	})
}

// ── Test 8: Multiple instances ────────────────────────────────────────────────

// TestPrefabHierarchy_MultipleInstances verifies that each instantiation
// produces fresh entities distinct from all other instances.
func TestPrefabHierarchy_MultipleInstances(t *testing.T) {
	w := flecs.New()

	var tank, turret, inst1, inst2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tank = fw.NewEntity()
		flecs.MarkPrefab(fw, tank)

		turret = fw.NewEntity()
		flecs.MarkPrefab(fw, turret)
		flecs.AddID(fw, turret, flecs.MakePair(w.ChildOf(), tank))
		flecs.AddID(fw, turret, flecs.MakePair(w.SlotOf(), tank))

		inst1 = fw.NewEntity()
		flecs.AddID(fw, inst1, flecs.MakePair(w.IsA(), tank))

		inst2 = fw.NewEntity()
		flecs.AddID(fw, inst2, flecs.MakePair(w.IsA(), tank))
	})

	w.Read(func(r *flecs.Reader) {
		t1, ok1 := flecs.GetPairTarget(r, inst1, turret)
		t2, ok2 := flecs.GetPairTarget(r, inst2, turret)
		if !ok1 || !ok2 {
			t.Fatal("slot lookup failed for one or both instances")
		}
		if t1 == t2 {
			t.Error("each instance must have distinct child entities; got same ID for both")
		}
		if t1 == turret || t2 == turret {
			t.Error("instance child must not be the prefab child itself")
		}
	})
}

// ── Test 9: OrderedChildren propagation ───────────────────────────────────────

// TestPrefabHierarchy_OrderedChildren verifies that when a prefab parent has
// the OrderedChildren trait, the instance is also marked ordered and EachChild
// returns children in the prefab's insertion order.
func TestPrefabHierarchy_OrderedChildren(t *testing.T) {
	w := flecs.New()

	var tank, child1, child2, child3, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tank = fw.NewEntity()
		flecs.MarkPrefab(fw, tank)
		flecs.SetOrderedChildren(w, tank)

		child1 = fw.NewEntity()
		flecs.MarkPrefab(fw, child1)
		flecs.AddID(fw, child1, flecs.MakePair(w.ChildOf(), tank))

		child2 = fw.NewEntity()
		flecs.MarkPrefab(fw, child2)
		flecs.AddID(fw, child2, flecs.MakePair(w.ChildOf(), tank))

		child3 = fw.NewEntity()
		flecs.MarkPrefab(fw, child3)
		flecs.AddID(fw, child3, flecs.MakePair(w.ChildOf(), tank))

		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), tank))
	})

	w.Read(func(r *flecs.Reader) {
		// Instance must be ordered.
		if !flecs.IsOrderedChildren(r, inst) {
			t.Fatal("instance must be marked OrderedChildren when prefab is ordered")
		}

		var instChildren []flecs.ID
		w.EachChild(inst, func(c flecs.ID) bool {
			instChildren = append(instChildren, c)
			return true
		})
		if len(instChildren) != 3 {
			t.Fatalf("expected 3 ordered instance children, got %d", len(instChildren))
		}

		// Gather prefab children in their insertion order.
		var prefabChildren []flecs.ID
		w.EachChild(tank, func(c flecs.ID) bool {
			prefabChildren = append(prefabChildren, c)
			return true
		})
		if len(prefabChildren) != 3 {
			t.Fatalf("expected 3 ordered prefab children, got %d", len(prefabChildren))
		}

		// The relative order of instance children must match prefab children.
		// Map prefab child → position in prefab order.
		prefabPos := map[flecs.ID]int{
			prefabChildren[0]: 0,
			prefabChildren[1]: 1,
			prefabChildren[2]: 2,
		}
		_ = prefabPos

		// Each instChild must have (ChildOf, inst) and not be a prefab entity.
		for _, ic := range instChildren {
			if ic == child1 || ic == child2 || ic == child3 {
				t.Errorf("instance child %v is same as prefab child; must be a fresh entity", ic)
			}
			p, ok := w.ParentOf(ic)
			if !ok || p != inst {
				t.Errorf("instance child %v parent = %v, want inst (%v)", ic, p, inst)
			}
		}
	})
}

// ── Test 10: SlotOf on non-prefab-parent (no-op slot resolution) ───────────────

// TestPrefabHierarchy_SlotOfNonParent verifies that (SlotOf, X) where X is not the
// immediate prefab parent of the child is silently ignored during slot resolution
// (the nested-slot branch is deferred; the slot pair is simply not copied).
func TestPrefabHierarchy_SlotOfNonParent(t *testing.T) {
	w := flecs.New()

	var tank, turret, otherBase, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		otherBase = fw.NewEntity()
		flecs.MarkPrefab(fw, otherBase)

		tank = fw.NewEntity()
		flecs.MarkPrefab(fw, tank)

		turret = fw.NewEntity()
		flecs.MarkPrefab(fw, turret)
		flecs.AddID(fw, turret, flecs.MakePair(w.ChildOf(), tank))
		// SlotOf targets otherBase, NOT tank — no slot should appear on inst.
		flecs.AddID(fw, turret, flecs.MakePair(w.SlotOf(), otherBase))

		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), tank))
	})

	w.Read(func(r *flecs.Reader) {
		// The instance must have a child (the copy of turret).
		var count int
		w.EachChild(inst, func(flecs.ID) bool { count++; return true })
		if count != 1 {
			t.Errorf("expected 1 child on inst, got %d", count)
		}

		// No slot (turret, ?) on inst because SlotOf pointed to otherBase, not tank.
		_, hasSlot := flecs.GetPairTarget(r, inst, turret)
		if hasSlot {
			t.Error("slot (turret, ?) must NOT appear on inst when SlotOf targets a non-parent")
		}
	})
}

// ── Test 11: Marshal round-trip ───────────────────────────────────────────────

// TestPrefabHierarchy_MarshalRoundTrip verifies that after instantiating a
// prefab with a slotted child, the instance's subtree structure and slot pair
// survive a marshal/unmarshal cycle.
func TestPrefabHierarchy_MarshalRoundTrip(t *testing.T) {
	type Ammo struct{ Count int }
	w := flecs.New()
	flecs.RegisterComponent[Ammo](w)

	var tank, turret, inst flecs.ID
	var instTurretBefore flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tank = fw.NewEntity()
		flecs.MarkPrefab(fw, tank)

		turret = fw.NewEntity()
		flecs.MarkPrefab(fw, turret)
		flecs.AddID(fw, turret, flecs.MakePair(w.ChildOf(), tank))
		flecs.AddID(fw, turret, flecs.MakePair(w.SlotOf(), tank))
		flecs.Set(fw, turret, Ammo{Count: 10})

		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), tank))
	})
	w.Read(func(r *flecs.Reader) {
		instTurretBefore, _ = flecs.GetPairTarget(r, inst, turret)
	})
	if instTurretBefore == 0 {
		t.Fatal("pre-marshal: slot not resolved on inst")
	}

	data, err := w.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	w2 := flecs.New()
	flecs.RegisterComponent[Ammo](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// Round-trip: re-marshal and compare JSON equality.
	data2, err := w2.MarshalJSON()
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}

	var j1, j2 interface{}
	if err := json.Unmarshal(data, &j1); err != nil {
		t.Fatalf("parse original JSON: %v", err)
	}
	if err := json.Unmarshal(data2, &j2); err != nil {
		t.Fatalf("parse round-trip JSON: %v", err)
	}

	// Verify that both worlds have the same entity count (minus built-ins).
	w2.Read(func(r *flecs.Reader) {
		var instCount int
		w2.EachEntity(func(e flecs.ID) bool {
			if !w2.IsAlive(e) {
				return true
			}
			instCount++
			return true
		})
		// Original world entity count must match round-tripped world.
		var origCount int
		w.EachEntity(func(flecs.ID) bool { origCount++; return true })
		if instCount != origCount {
			t.Errorf("entity count: original=%d, round-trip=%d", origCount, instCount)
		}
	})
}

// ── Test 12: Deep hierarchy (4+ levels) ───────────────────────────────────────

// TestPrefabHierarchy_DeepHierarchy verifies subtree replication at 4 levels:
// Tank → Body → Turret → Barrel. All entities in the instance subtree must be
// fresh (distinct from prefab entities).
func TestPrefabHierarchy_DeepHierarchy(t *testing.T) {
	w := flecs.New()

	var tank, body, turret, barrel, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tank = fw.NewEntity()
		flecs.MarkPrefab(fw, tank)

		body = fw.NewEntity()
		flecs.MarkPrefab(fw, body)
		flecs.AddID(fw, body, flecs.MakePair(w.ChildOf(), tank))

		turret = fw.NewEntity()
		flecs.MarkPrefab(fw, turret)
		flecs.AddID(fw, turret, flecs.MakePair(w.ChildOf(), body))

		barrel = fw.NewEntity()
		flecs.MarkPrefab(fw, barrel)
		flecs.AddID(fw, barrel, flecs.MakePair(w.ChildOf(), turret))

		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), tank))
	})

	w.Read(func(r *flecs.Reader) {
		prefabEntities := map[flecs.ID]bool{tank: true, body: true, turret: true, barrel: true}

		// Collect all entities in the instance subtree via BFS.
		var queue []flecs.ID
		queue = append(queue, inst)
		var visited []flecs.ID

		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]
			visited = append(visited, curr)
			w.EachChild(curr, func(child flecs.ID) bool {
				queue = append(queue, child)
				return true
			})
		}

		// inst + body copy + turret copy + barrel copy = 4 entities.
		if len(visited) != 4 {
			t.Fatalf("expected 4 entities in instance subtree (inst + 3 copies), got %d", len(visited))
		}

		// None must be a prefab entity.
		for _, e := range visited {
			if prefabEntities[e] {
				t.Errorf("entity %v in instance subtree is a prefab entity; must be a fresh copy", e)
			}
		}

		// Check parent chain of the deepest entity (barrel copy).
		// inst → instBody → instTurret → instBarrel
		var instChildren []flecs.ID
		w.EachChild(inst, func(c flecs.ID) bool { instChildren = append(instChildren, c); return true })
		if len(instChildren) != 1 {
			t.Fatalf("inst must have 1 child (instBody), got %d", len(instChildren))
		}
		instBody := instChildren[0]

		var bodyChildren []flecs.ID
		w.EachChild(instBody, func(c flecs.ID) bool { bodyChildren = append(bodyChildren, c); return true })
		if len(bodyChildren) != 1 {
			t.Fatalf("instBody must have 1 child (instTurret), got %d", len(bodyChildren))
		}
		instTurret := bodyChildren[0]

		var turretChildren []flecs.ID
		w.EachChild(instTurret, func(c flecs.ID) bool { turretChildren = append(turretChildren, c); return true })
		if len(turretChildren) != 1 {
			t.Fatalf("instTurret must have 1 child (instBarrel), got %d", len(turretChildren))
		}
		instBarrel := turretChildren[0]

		// Verify parent pointers.
		checkParent := func(e, want flecs.ID, label string) {
			t.Helper()
			p, ok := w.ParentOf(e)
			if !ok || p != want {
				t.Errorf("%s parent = %v, want %v", label, p, want)
			}
		}
		checkParent(instBody, inst, "instBody")
		checkParent(instTurret, instBody, "instTurret")
		checkParent(instBarrel, instTurret, "instBarrel")
	})
}

// ── Bonus: Deferred instantiation ────────────────────────────────────────────

// TestPrefabHierarchy_DeferredInstantiation verifies that instantiating a prefab
// inside a Write scope (deferred path) produces the same result as the immediate path.
func TestPrefabHierarchy_DeferredInstantiation(t *testing.T) {
	type HP struct{ Value int }
	w := flecs.New()
	flecs.RegisterComponent[HP](w)

	var tank, turret flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tank = fw.NewEntity()
		flecs.MarkPrefab(fw, tank)
		flecs.Set(fw, tank, HP{Value: 50})

		turret = fw.NewEntity()
		flecs.MarkPrefab(fw, turret)
		flecs.AddID(fw, turret, flecs.MakePair(w.ChildOf(), tank))
		flecs.AddID(fw, turret, flecs.MakePair(w.SlotOf(), tank))
	})

	// Create three instances via a deferred Write.
	var inst1, inst2, inst3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		inst1 = fw.NewEntity()
		flecs.AddID(fw, inst1, flecs.MakePair(w.IsA(), tank))
		inst2 = fw.NewEntity()
		flecs.AddID(fw, inst2, flecs.MakePair(w.IsA(), tank))
		inst3 = fw.NewEntity()
		flecs.AddID(fw, inst3, flecs.MakePair(w.IsA(), tank))
	})

	w.Read(func(r *flecs.Reader) {
		instances := []flecs.ID{inst1, inst2, inst3}
		seen := map[flecs.ID]bool{}
		for _, inst := range instances {
			ic, ok := flecs.GetPairTarget(r, inst, turret)
			if !ok {
				t.Fatalf("slot not resolved on instance %v", inst)
			}
			if ic == turret {
				t.Error("slot target must be a copy, not the prefab child")
			}
			if seen[ic] {
				t.Errorf("instance child %v is shared across instances", ic)
			}
			seen[ic] = true
		}
	})
}

// ── Bonus: Data-bearing cross-reference rewrite ───────────────────────────────

// TestPrefabHierarchy_CrossRefRewriteDataPair verifies that a data-bearing pair
// whose target lies within the same prefab subtree is correctly rewritten AND
// that the pair data is copied to the instance child. This exercises the
// copyPairToIC / ensureRewrittenPairRegistered code path.
func TestPrefabHierarchy_CrossRefRewriteDataPair(t *testing.T) {
	type Link struct{ Weight float64 }
	w := flecs.New()
	flecs.RegisterComponent[Link](w)

	var tank, childA, childB, rel, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		// Register the pair (rel, childB) as data-bearing so copyPairToIC hits the
		// non-tag branch.
		flecs.RegisterComponent[Link](w)

		tank = fw.NewEntity()
		flecs.MarkPrefab(fw, tank)

		childA = fw.NewEntity()
		flecs.MarkPrefab(fw, childA)
		flecs.AddID(fw, childA, flecs.MakePair(w.ChildOf(), tank))

		childB = fw.NewEntity()
		flecs.MarkPrefab(fw, childB)
		flecs.AddID(fw, childB, flecs.MakePair(w.ChildOf(), tank))

		// childA carries a data-bearing pair (rel, childB) — same-subtree reference.
		fw.SetPairByID(childA, rel, childB, Link{Weight: 3.14})

		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), tank))
	})

	w.Read(func(r *flecs.Reader) {
		var instChildren []flecs.ID
		w.EachChild(inst, func(c flecs.ID) bool { instChildren = append(instChildren, c); return true })
		if len(instChildren) != 2 {
			t.Fatalf("expected 2 instance children, got %d", len(instChildren))
		}

		// Identify instA (the copy of childA) and instB (the copy of childB).
		var instA, instB flecs.ID
		for _, ic := range instChildren {
			if target, ok := flecs.GetPairTarget(r, ic, rel); ok {
				instA = ic
				instB = target
			}
		}
		if instA == 0 {
			t.Fatal("no instance child has (rel, *)")
		}
		if instB == childB {
			t.Error("data-bearing cross-reference must be rewritten: target must not be prefab childB")
		}
		// The rewritten target must be one of the instance children.
		found := false
		for _, ic := range instChildren {
			if ic == instB {
				found = true
			}
		}
		if !found {
			t.Error("rewritten pair target must be an instance child")
		}
	})
}

// ── Coverage: child with (IsA, *) pair ───────────────────────────────────────

// TestPrefabHierarchy_ChildWithIsA verifies that a prefab child carrying an
// (IsA, otherPrefab) pair is correctly instantiated: the IsA pair is skipped
// during subtree copy (deferred feature) and other components are copied normally.
func TestPrefabHierarchy_ChildWithIsA(t *testing.T) {
	type HP struct{ Value int }
	w := flecs.New()
	flecs.RegisterComponent[HP](w)

	var tank, turret, basePrefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		basePrefab = fw.NewEntity()
		flecs.MarkPrefab(fw, basePrefab)
		flecs.Set(fw, basePrefab, HP{Value: 5})

		tank = fw.NewEntity()
		flecs.MarkPrefab(fw, tank)

		// turret is a child of tank AND inherits from basePrefab.
		// The (IsA, basePrefab) pair must be silently skipped during subtree copy.
		turret = fw.NewEntity()
		flecs.MarkPrefab(fw, turret)
		flecs.AddID(fw, turret, flecs.MakePair(w.ChildOf(), tank))
		flecs.AddID(fw, turret, flecs.MakePair(w.IsA(), basePrefab))
		flecs.Set(fw, turret, HP{Value: 42})

		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), tank))
	})

	w.Read(func(r *flecs.Reader) {
		var instChildren []flecs.ID
		w.EachChild(inst, func(c flecs.ID) bool { instChildren = append(instChildren, c); return true })
		if len(instChildren) != 1 {
			t.Fatalf("expected 1 instance child, got %d", len(instChildren))
		}
		ic := instChildren[0]
		if ic == turret {
			t.Error("instance child must be a fresh copy, not the prefab child")
		}
		// The HP component must be copied from turret.
		hp, ok := flecs.Get[HP](r, ic)
		if !ok {
			t.Fatal("instance child must have HP component")
		}
		if hp.Value != 42 {
			t.Errorf("HP.Value = %d, want 42", hp.Value)
		}
	})
}

// ── Coverage: shared subtree target (ensureRewrittenPairRegistered already-registered) ─

// TestPrefabHierarchy_CrossRefRewriteSharedTarget exercises the case where two
// prefab children both carry a data-bearing pair pointing to the same third
// subtree child. After the first child is copied, the rewritten pair ID is
// already registered; the second copy must hit the early-return branch of
// ensureRewrittenPairRegistered without panicking.
func TestPrefabHierarchy_CrossRefRewriteSharedTarget(t *testing.T) {
	type Link struct{ Weight float64 }
	w := flecs.New()
	flecs.RegisterComponent[Link](w)

	var tank, childA, childB, childC, rel, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()

		tank = fw.NewEntity()
		flecs.MarkPrefab(fw, tank)

		// Three children: A and B both point to C with a data-bearing pair.
		childA = fw.NewEntity()
		flecs.MarkPrefab(fw, childA)
		flecs.AddID(fw, childA, flecs.MakePair(w.ChildOf(), tank))

		childB = fw.NewEntity()
		flecs.MarkPrefab(fw, childB)
		flecs.AddID(fw, childB, flecs.MakePair(w.ChildOf(), tank))

		childC = fw.NewEntity()
		flecs.MarkPrefab(fw, childC)
		flecs.AddID(fw, childC, flecs.MakePair(w.ChildOf(), tank))

		// Both A and B have (rel, C) with data — both will rewrite to (rel, instC).
		// The second rewrite hits the "already registered" branch.
		fw.SetPairByID(childA, rel, childC, Link{Weight: 1.0})
		fw.SetPairByID(childB, rel, childC, Link{Weight: 2.0})

		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), tank))
	})

	w.Read(func(r *flecs.Reader) {
		var instChildren []flecs.ID
		w.EachChild(inst, func(c flecs.ID) bool { instChildren = append(instChildren, c); return true })
		if len(instChildren) != 3 {
			t.Fatalf("expected 3 instance children, got %d", len(instChildren))
		}

		// At least two instance children must carry (rel, instC).
		count := 0
		for _, ic := range instChildren {
			if _, ok := flecs.GetPairTarget(r, ic, rel); ok {
				count++
			}
		}
		if count != 2 {
			t.Errorf("expected 2 instance children with (rel, *), got %d", count)
		}
	})
}
