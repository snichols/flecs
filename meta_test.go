package flecs_test

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
)

// builtinEntityCount is the number of alive entities immediately after World.New():
// ChildOf(1), IsA(2), Name(3), PreUpdate(4), OnUpdate(5), PostUpdate(6),
// OnFixedUpdate(7), OnInstantiate(8), Inherit(9), Override(10), DontInherit(11),
// OnDelete(12), OnDeleteTarget(13), RemoveAction(14), DeleteAction(15),
// PanicAction(16), Exclusive(17), CanToggle(18).
const builtinEntityCount = 18

// ── Components() ─────────────────────────────────────────────────────────────

// TestComponentsBasic verifies that Components() returns registered component
// IDs including the built-in Name, and excludes built-in tag entities.
func TestComponentsBasic(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)
	nameID := w.Name()

	ids := w.Components()

	// Must include Name, Position, Velocity (at minimum).
	has := func(id flecs.ID) bool {
		for _, x := range ids {
			if x == id {
				return true
			}
		}
		return false
	}
	if !has(nameID) {
		t.Error("Components() missing built-in Name component")
	}
	if !has(posID) {
		t.Error("Components() missing Position")
	}
	if !has(velID) {
		t.Error("Components() missing Velocity")
	}
	// Built-in tag entities must NOT appear.
	for _, id := range ids {
		if id == w.PreUpdate() || id == w.OnUpdate() || id == w.PostUpdate() || id == w.OnFixedUpdate() {
			t.Errorf("Components() contains built-in phase entity %v", id)
		}
	}
}

// TestComponentsAfterNewContainsOnlyName verifies that immediately after
// World.New(), Components() contains exactly the Name component.
func TestComponentsAfterNewContainsOnlyName(t *testing.T) {
	w := flecs.New()
	ids := w.Components()
	if len(ids) != 1 {
		t.Fatalf("expected exactly 1 component after New(), got %d: %v", len(ids), ids)
	}
	if ids[0] != w.Name() {
		t.Fatalf("expected Name component ID %v, got %v", w.Name(), ids[0])
	}
}

// TestComponentsOrder verifies that Components() preserves registration order.
func TestComponentsOrder(t *testing.T) {
	w := flecs.New()
	// Name is already registered (index 0 in Components()).
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	ids := w.Components()
	// ids should be [Name, Position, Velocity] in that order.
	if len(ids) < 3 {
		t.Fatalf("expected at least 3 components, got %d", len(ids))
	}
	if ids[0] != w.Name() {
		t.Errorf("expected ids[0]=Name, got %v", ids[0])
	}
	if ids[1] != posID {
		t.Errorf("expected ids[1]=Position, got %v", ids[1])
	}
	if ids[2] != velID {
		t.Errorf("expected ids[2]=Velocity, got %v", ids[2])
	}
}

// TestComponentsReturnsFreshSlice verifies that mutating the returned slice
// does not corrupt the world's component list.
func TestComponentsReturnsFreshSlice(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[Position](w)
	ids1 := w.Components()
	ids1[0] = 0 // mutate the copy
	ids2 := w.Components()
	if ids2[0] == 0 {
		t.Error("mutating returned slice from Components() corrupted the world")
	}
}

// ── ComponentInfo() ───────────────────────────────────────────────────────────

// TestComponentInfoRegistered verifies metadata for a normally registered component.
func TestComponentInfoRegistered(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	info, ok := w.ComponentInfo(posID)
	if !ok {
		t.Fatal("ComponentInfo returned false for registered component")
	}
	if info.ID != posID {
		t.Errorf("info.ID=%v, want %v", info.ID, posID)
	}
	if info.Name != reflect.TypeFor[Position]().String() {
		t.Errorf("info.Name=%q, want %q", info.Name, reflect.TypeFor[Position]().String())
	}
	if info.Size != unsafe.Sizeof(Position{}) {
		t.Errorf("info.Size=%d, want %d", info.Size, unsafe.Sizeof(Position{}))
	}
	if info.Align < 1 {
		t.Errorf("info.Align=%d, want >=1", info.Align)
	}
	if info.Type != reflect.TypeFor[Position]() {
		t.Errorf("info.Type=%v, want %v", info.Type, reflect.TypeFor[Position]())
	}
}

// TestComponentInfoUnregistered verifies that ComponentInfo returns false for
// an ID not in the registry.
func TestComponentInfoUnregistered(t *testing.T) {
	w := flecs.New()
	var raw flecs.ID
	w.Write(func(fw *flecs.Writer) { raw = fw.NewEntity() }) // alive but not a component
	info, ok := w.ComponentInfo(raw)
	if ok {
		t.Errorf("ComponentInfo returned true for non-component entity; info=%+v", info)
	}
	if info != (flecs.ComponentInfo{}) {
		t.Errorf("expected zero ComponentInfo, got %+v", info)
	}
}

// TestComponentInfoPairWithData verifies ComponentInfo for a pair registered
// via SetPair[T].
func TestComponentInfoPairWithData(t *testing.T) {
	w := flecs.New()
	var relID, tgtID, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		relID = fw.NewEntity()
		tgtID = fw.NewEntity()
		e = fw.NewEntity()
		flecs.SetPair[Edge](fw, e, relID, tgtID, Edge{Weight: 1.5})
	})

	pairID := flecs.MakePair(relID, tgtID)
	info, ok := w.ComponentInfo(pairID)
	if !ok {
		t.Fatal("ComponentInfo returned false for pair-with-data")
	}
	if info.ID != pairID {
		t.Errorf("info.ID=%v, want %v", info.ID, pairID)
	}
	if info.Size != unsafe.Sizeof(Edge{}) {
		t.Errorf("info.Size=%d, want %d", info.Size, unsafe.Sizeof(Edge{}))
	}
	if info.Type != reflect.TypeFor[Edge]() {
		t.Errorf("info.Type=%v, want %v", info.Type, reflect.TypeFor[Edge]())
	}
}

// TestComponentInfoPairAsTag verifies ComponentInfo for a pair added as a tag
// via AddID, which auto-registers a zero-size tag TypeInfo.
func TestComponentInfoPairAsTag(t *testing.T) {
	w := flecs.New()
	var relID, tgtID, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		relID = fw.NewEntity()
		tgtID = fw.NewEntity()
		e = fw.NewEntity()
	})
	pairID := flecs.MakePair(relID, tgtID)
	w.Write(func(fw *flecs.Writer) { flecs.AddID(fw, e, pairID) })

	info, ok := w.ComponentInfo(pairID)
	if !ok {
		t.Fatal("ComponentInfo returned false for pair-as-tag")
	}
	if info.ID != pairID {
		t.Errorf("info.ID=%v, want %v", info.ID, pairID)
	}
	if info.Size != 0 {
		t.Errorf("info.Size=%d, want 0 for tag", info.Size)
	}
}

// TestComponentInfoRawTagEntity verifies ComponentInfo for a raw entity ID
// used as a tag (auto-registered via EnsureID by AddID).
func TestComponentInfoRawTagEntity(t *testing.T) {
	w := flecs.New()
	var tagID, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tagID = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, tagID)
	})

	info, ok := w.ComponentInfo(tagID)
	if !ok {
		t.Fatal("ComponentInfo returned false for raw tag entity")
	}
	if info.ID != tagID {
		t.Errorf("info.ID=%v, want %v", info.ID, tagID)
	}
	if info.Size != 0 {
		t.Errorf("info.Size=%d, want 0 for raw tag", info.Size)
	}
}

// ── EntityComponents() ────────────────────────────────────────────────────────

// TestEntityComponentsBasic verifies that EntityComponents returns the correct
// sorted component IDs for an entity with multiple components including a pair.
func TestEntityComponentsBasic(t *testing.T) {
	w := flecs.New()
	var parent, e flecs.ID

	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		e = fw.NewEntity()
	})

	childPairID := flecs.MakePair(w.ChildOf(), parent)

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, e, Position{1, 2})
		flecs.Set[Velocity](fw, e, Velocity{3, 4})
		flecs.AddID(fw, e, childPairID)
	})

	comps := w.EntityComponents(e)
	if len(comps) != 3 {
		t.Fatalf("expected 3 components, got %d: %v", len(comps), comps)
	}

	has := func(id flecs.ID) bool {
		for _, x := range comps {
			if x == id {
				return true
			}
		}
		return false
	}
	if !has(posID) {
		t.Error("EntityComponents missing Position")
	}
	if !has(velID) {
		t.Error("EntityComponents missing Velocity")
	}
	if !has(childPairID) {
		t.Errorf("EntityComponents missing (ChildOf, parent) pair")
	}

	// Verify sorted ascending order.
	for i := 1; i < len(comps); i++ {
		if comps[i] < comps[i-1] {
			t.Errorf("EntityComponents not sorted: %v", comps)
			break
		}
	}
}

// TestEntityComponentsDeadEntity verifies that EntityComponents returns nil
// for a dead entity.
func TestEntityComponentsDeadEntity(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	w.Delete(e)

	comps := w.EntityComponents(e)
	if comps != nil {
		t.Errorf("expected nil for dead entity, got %v", comps)
	}
}

// TestEntityComponentsEmptyArchetype verifies that EntityComponents returns nil
// for an entity in the empty archetype (no components).
func TestEntityComponentsEmptyArchetype(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	comps := w.EntityComponents(e)
	if comps != nil {
		t.Errorf("expected nil for empty-archetype entity, got %v", comps)
	}
}

// TestEntityComponentsReturnsFreshSlice verifies that mutating the returned
// slice does not corrupt the entity's archetype.
func TestEntityComponentsReturnsFreshSlice(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[Position](fw, e, Position{1, 2})
		flecs.Set[Velocity](fw, e, Velocity{3, 4})
	})

	comps := w.EntityComponents(e)
	if len(comps) < 1 {
		t.Fatal("expected at least one component")
	}
	comps[0] = 0 // mutate the copy

	comps2 := w.EntityComponents(e)
	if comps2[0] == 0 {
		t.Error("mutating returned slice from EntityComponents() corrupted the world")
	}
}

// ── EachEntity() ─────────────────────────────────────────────────────────────

// TestEachEntityVisitsAll verifies that EachEntity visits all alive entities
// including built-ins, and the count matches w.Count().
func TestEachEntityVisitsAll(t *testing.T) {
	w := flecs.New()
	const n = 5
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < n; i++ {
			fw.NewEntity()
		}
	})

	var visited []flecs.ID
	w.EachEntity(func(e flecs.ID) bool {
		visited = append(visited, e)
		return true
	})

	want := w.Count()
	if len(visited) != want {
		t.Errorf("EachEntity visited %d entities, Count()=%d", len(visited), want)
	}
}

// TestEachEntityEarlyExit verifies that returning false from fn stops iteration.
func TestEachEntityEarlyExit(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < 10; i++ {
			fw.NewEntity()
		}
	})

	count := 0
	w.EachEntity(func(e flecs.ID) bool {
		count++
		return count < 3
	})
	if count != 3 {
		t.Errorf("expected 3 invocations before early exit, got %d", count)
	}
}

// TestEachEntityIncludesBuiltins verifies that built-in entities (ChildOf, IsA,
// phase entities, Name, and instantiation-trait entities) are included in EachEntity.
func TestEachEntityIncludesBuiltins(t *testing.T) {
	w := flecs.New()

	// After New() with no user entities, Count() == builtinEntityCount.
	if w.Count() != builtinEntityCount {
		t.Errorf("expected %d built-in entities, got %d", builtinEntityCount, w.Count())
	}

	var visited []flecs.ID
	w.EachEntity(func(e flecs.ID) bool {
		visited = append(visited, e)
		return true
	})
	if len(visited) != builtinEntityCount {
		t.Errorf("expected %d visited, got %d", builtinEntityCount, len(visited))
	}
}

// TestEachEntityDeferSafeMutation verifies the documented pattern: wrapping
// mutations in Defer(func(){ ... }) is safe during (a post-each-entity) call.
// This does not test mutation DURING iteration (undefined), but that Defer
// + EachEntity compose correctly.
func TestEachEntityDeferSafeMutation(t *testing.T) {
	w := flecs.New()
	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
	})

	// Collect entities first, then apply mutations deferred.
	var ids []flecs.ID
	w.EachEntity(func(e flecs.ID) bool {
		ids = append(ids, e)
		return true
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, e1, Position{1, 2})
		flecs.Set[Velocity](fw, e2, Velocity{3, 4})
	})

	// Verify mutations applied.
	w.Read(func(r *flecs.Reader) {
		pos, ok := flecs.Get[Position](r, e1)
		if !ok || pos.X != 1 {
			t.Errorf("expected Position{1,2}, got %+v ok=%v", pos, ok)
		}
		vel, ok := flecs.Get[Velocity](r, e2)
		if !ok || vel.DX != 3 {
			t.Errorf("expected Velocity{3,4}, got %+v ok=%v", vel, ok)
		}
	})
}

// ── AliveEntities() ───────────────────────────────────────────────────────────

// TestAliveEntitiesMatchesCount verifies that AliveEntities() returns a slice
// whose length matches w.Count().
func TestAliveEntitiesMatchesCount(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < 8; i++ {
			fw.NewEntity()
		}
	})
	entities := w.AliveEntities()
	if len(entities) != w.Count() {
		t.Errorf("AliveEntities() len=%d, Count()=%d", len(entities), w.Count())
	}
}

// TestAliveEntitiesReturnsFreshSlice verifies that mutating the returned slice
// does not affect the world state.
func TestAliveEntitiesReturnsFreshSlice(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) { fw.NewEntity() })
	ents1 := w.AliveEntities()
	ents1[0] = 0
	ents2 := w.AliveEntities()
	if ents2[0] == 0 {
		t.Error("mutating AliveEntities() slice corrupted the world")
	}
}

// TestAliveEntitiesAfterDelete verifies that AliveEntities() reflects deletions.
func TestAliveEntitiesAfterDelete(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	before := w.Count()
	w.Delete(e)
	entities := w.AliveEntities()
	if len(entities) != before-1 {
		t.Errorf("expected %d after delete, got %d", before-1, len(entities))
	}
}

// ── World.ChildOf() ───────────────────────────────────────────────────────────

// TestChildOfAccessor verifies that the ChildOf() accessor returns the
// built-in ChildOf relationship entity.
func TestChildOfAccessor(t *testing.T) {
	w := flecs.New()
	childOfID := w.ChildOf()
	if !w.IsAlive(childOfID) {
		t.Error("ChildOf() returned a dead entity")
	}
	// ChildOf is not a component: ComponentInfo returns false.
	if _, ok := w.ComponentInfo(childOfID); ok {
		t.Error("ChildOf entity should not be a component")
	}
}
