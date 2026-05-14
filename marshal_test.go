package flecs_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/snichols/flecs"
)

// --- test component types ---

type marshalPos struct{ X, Y float32 }
type marshalVel struct{ DX, DY float32 }
type marshalTag struct{}
type marshalStr struct{ Tag string }
type marshalNested struct {
	Label string
	Inner marshalPos
}

// --- helpers ---

func newMarshalWorld() *flecs.World {
	w := flecs.New()
	flecs.RegisterComponent[marshalPos](w)
	flecs.RegisterComponent[marshalVel](w)
	return w
}

func mustMarshal(t *testing.T, w *flecs.World) []byte {
	t.Helper()
	data, err := w.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}
	return data
}

// nonDataEntities returns the set of IDs to exclude from user-entity counts:
// the 39 built-in entities plus all registered component entities.
func nonDataEntities(w *flecs.World) map[flecs.ID]struct{} {
	skip := map[flecs.ID]struct{}{
		w.ChildOf(): {}, w.IsA(): {}, w.Name(): {},
		w.PreUpdate(): {}, w.OnUpdate(): {}, w.PostUpdate(): {}, w.OnFixedUpdate(): {},
		w.OnInstantiate(): {}, w.Inherit(): {}, w.Override(): {}, w.DontInherit(): {},
		w.OnDelete(): {}, w.OnDeleteTarget(): {},
		w.RemoveAction(): {}, w.DeleteAction(): {}, w.PanicAction(): {},
		w.Exclusive(): {}, w.CanToggle(): {}, w.Symmetric(): {}, w.Transitive(): {},
		w.Reflexive(): {}, w.Acyclic(): {}, w.Final(): {}, w.OneOf(): {}, w.Singleton(): {}, w.WriteOnce(): {}, w.Traversable(): {},
		w.Relationship(): {}, w.Target(): {}, w.Trait(): {}, w.PairIsTag(): {}, w.With(): {},
		w.OrderedChildren(): {}, w.Sparse(): {}, w.DontFragment(): {},
		w.Disabled(): {}, w.Prefab(): {},
		w.Wildcard(): {}, w.Any(): {},
	}
	for _, cid := range w.Components() {
		skip[cid] = struct{}{}
	}
	return skip
}

// --- tests ---

func TestMarshalEmptyWorld(t *testing.T) {
	w := newMarshalWorld()
	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("produced invalid JSON")
	}
	var jw struct {
		Version  int               `json:"version"`
		Entities []json.RawMessage `json:"entities"`
	}
	if err := json.Unmarshal(data, &jw); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if jw.Version != 1 {
		t.Fatalf("version: got %d, want 1", jw.Version)
	}
	if len(jw.Entities) != 0 {
		t.Fatalf("entities: got %d, want 0", len(jw.Entities))
	}
}

func TestMarshalSingleEntity(t *testing.T) {
	w := newMarshalWorld()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, marshalPos{X: 1, Y: 2})
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	w2 := flecs.New()
	flecs.RegisterComponent[marshalPos](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	entities := w2.AliveEntities()
	skip := nonDataEntities(w2)
	var userEntities []flecs.ID
	for _, id := range entities {
		if _, ok := skip[id]; !ok {
			userEntities = append(userEntities, id)
		}
	}
	if len(userEntities) != 1 {
		t.Fatalf("expected 1 user entity, got %d", len(userEntities))
	}
	var p marshalPos
	var ok bool
	w2.Read(func(r *flecs.Reader) { p, ok = flecs.Get[marshalPos](r, userEntities[0]) })
	if !ok {
		t.Fatal("expected marshalPos on restored entity")
	}
	if p.X != 1 || p.Y != 2 {
		t.Fatalf("position: got %+v, want {1 2}", p)
	}
}

func TestMarshalMultipleEntities(t *testing.T) {
	w := newMarshalWorld()
	w.Write(func(fw *flecs.Writer) {
		for i := range 5 {
			e := fw.NewEntity()
			flecs.Set(fw, e, marshalPos{X: float32(i), Y: float32(i * 2)})
			if i%2 == 0 {
				flecs.Set(fw, e, marshalVel{DX: float32(i), DY: 0})
			}
		}
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	w2 := flecs.New()
	flecs.RegisterComponent[marshalPos](w2)
	flecs.RegisterComponent[marshalVel](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// Count user entities in w2
	skip := nonDataEntities(w2)
	count := 0
	w2.EachEntity(func(e flecs.ID) bool {
		if _, ok := skip[e]; !ok {
			count++
		}
		return true
	})
	if count != 5 {
		t.Fatalf("expected 5 user entities, got %d", count)
	}
}

func TestMarshalNamesRoundTrip(t *testing.T) {
	w := newMarshalWorld()
	var e1, e2, e3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		e3 = fw.NewEntity()
	})
	w.SetName(e1, "alpha")
	w.SetName(e2, "beta")
	w.SetName(e3, "gamma")

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	w2 := flecs.New()
	flecs.RegisterComponent[marshalPos](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	names := map[string]bool{"alpha": false, "beta": false, "gamma": false}
	skip := nonDataEntities(w2)
	w2.EachEntity(func(e flecs.ID) bool {
		if _, ok := skip[e]; ok {
			return true
		}
		if n, ok := w2.GetName(e); ok {
			names[n] = true
		}
		return true
	})
	for name, found := range names {
		if !found {
			t.Errorf("name %q not restored", name)
		}
	}
}

func TestMarshalUnmarshalIntoNonEmptyWorld(t *testing.T) {
	// Populate w with one entity A.
	w := newMarshalWorld()
	var a flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
		flecs.Set(fw, a, marshalPos{X: 99, Y: 99})
	})

	// Marshal a separate world with one entity.
	w_src := newMarshalWorld()
	w_src.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, marshalPos{X: 1, Y: 2})
	})
	data := mustMarshal(t, w_src)

	// Unmarshal into w — A should still exist.
	if err := w.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	var pA marshalPos
	var okA bool
	w.Read(func(r *flecs.Reader) { pA, okA = flecs.Get[marshalPos](r, a) })
	if !okA {
		t.Fatal("entity A lost after UnmarshalJSON into non-empty world")
	}
	if pA.X != 99 || pA.Y != 99 {
		t.Fatalf("entity A value corrupted: %+v", pA)
	}
}

func TestMarshalTagComponents(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[marshalTag](w)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, marshalTag{})
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	// The tag should appear as {} in JSON.
	if !strings.Contains(string(data), "{}") {
		t.Errorf("expected tag to serialize as {}: %s", data)
	}

	w2 := flecs.New()
	flecs.RegisterComponent[marshalTag](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	skip2 := nonDataEntities(w2)
	var found flecs.ID
	w2.EachEntity(func(e flecs.ID) bool {
		if _, ok := skip2[e]; !ok {
			found = e
		}
		return true
	})
	if found == 0 {
		t.Fatal("no user entity after unmarshal")
	}
	var hasTag bool
	w2.Read(func(r *flecs.Reader) { hasTag = flecs.Has[marshalTag](r, found) })
	if !hasTag {
		t.Fatal("marshalTag not restored")
	}
}

func TestMarshalUnregisteredComponentError(t *testing.T) {
	w := newMarshalWorld()
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, marshalPos{X: 1, Y: 2})
	})
	data := mustMarshal(t, w)

	// w2 does NOT register marshalPos.
	w2 := flecs.New()
	err := w2.UnmarshalJSON(data)
	if err == nil {
		t.Fatal("expected error for unregistered component")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Errorf("error should mention 'not registered', got: %v", err)
	}
}

func TestMarshalWrongVersionError(t *testing.T) {
	data := []byte(`{"version":99,"entities":[]}`)
	w := flecs.New()
	err := w.UnmarshalJSON(data)
	if err == nil {
		t.Fatal("expected version error")
	}
	if !strings.Contains(err.Error(), "unsupported version") {
		t.Errorf("error should mention 'unsupported version', got: %v", err)
	}
}

func TestMarshalMalformedJSONError(t *testing.T) {
	data := []byte(`{"version":1,"entities":"not an array"}`)
	w := flecs.New()
	err := w.UnmarshalJSON(data)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMarshalPairComponentsSkipped(t *testing.T) {
	w := newMarshalWorld()
	w.Write(func(fw *flecs.Writer) {
		parent := fw.NewEntity()
		child := fw.NewEntity()
		flecs.Set(fw, child, marshalPos{X: 1, Y: 2})
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	// The ChildOf pair should NOT appear in any component map.
	if strings.Contains(string(data), "ChildOf") || strings.Contains(string(data), "pair(") {
		t.Errorf("pair component leaked into JSON: %s", data)
	}
}

func TestMarshalBuiltinEntitiesSkipped(t *testing.T) {
	w := flecs.New() // no user entities
	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}
	var jw struct {
		Entities []json.RawMessage `json:"entities"`
	}
	if err := json.Unmarshal(data, &jw); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(jw.Entities) != 0 {
		t.Fatalf("built-in entities leaked: got %d entities", len(jw.Entities))
	}
}

func TestMarshalFloatsAndStrings(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[marshalPos](w)
	flecs.RegisterComponent[marshalStr](w)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, marshalPos{X: 1.5, Y: 2.25})
		flecs.Set(fw, e, marshalStr{Tag: "hello"})
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	w2 := flecs.New()
	flecs.RegisterComponent[marshalPos](w2)
	flecs.RegisterComponent[marshalStr](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	skip2 := nonDataEntities(w2)
	var found flecs.ID
	w2.EachEntity(func(e flecs.ID) bool {
		if _, ok := skip2[e]; !ok {
			found = e
		}
		return true
	})
	if found == 0 {
		t.Fatal("no user entity")
	}
	var p marshalPos
	var s marshalStr
	var pOk, sOk bool
	w2.Read(func(r *flecs.Reader) {
		p, pOk = flecs.Get[marshalPos](r, found)
		s, sOk = flecs.Get[marshalStr](r, found)
	})
	if !pOk || p.X != 1.5 || p.Y != 2.25 {
		t.Fatalf("marshalPos: got %+v, want {1.5 2.25}", p)
	}
	if !sOk || s.Tag != "hello" {
		t.Fatalf("marshalStr: got %+v, want {hello}", s)
	}
}

func TestMarshalNestedStructs(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[marshalNested](w)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, marshalNested{Label: "test", Inner: marshalPos{X: 3, Y: 4}})
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	w2 := flecs.New()
	flecs.RegisterComponent[marshalNested](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	skip2 := nonDataEntities(w2)
	var found flecs.ID
	w2.EachEntity(func(e flecs.ID) bool {
		if _, ok := skip2[e]; !ok {
			found = e
		}
		return true
	})
	if found == 0 {
		t.Fatal("no user entity")
	}
	var n marshalNested
	var ok bool
	w2.Read(func(r *flecs.Reader) {
		n, ok = flecs.Get[marshalNested](r, found)
	})
	if !ok || n.Label != "test" || n.Inner.X != 3 || n.Inner.Y != 4 {
		t.Fatalf("marshalNested: got %+v", n)
	}
}

func TestMarshalJSONValid(t *testing.T) {
	w := newMarshalWorld()
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, marshalPos{X: 1, Y: 2})
		flecs.Set(fw, e, marshalVel{DX: 3, DY: 4})
		w.SetName(e, "entity1")
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatalf("json.Valid returned false: %s", data)
	}
}

func TestMarshalTwoStepRoundTrip(t *testing.T) {
	// Build world, marshal → unmarshal into w2, marshal w2 → compare.
	w := newMarshalWorld()
	w.Write(func(fw *flecs.Writer) {
		e1 := fw.NewEntity()
		flecs.Set(fw, e1, marshalPos{X: 1, Y: 2})
		w.SetName(e1, "e1")
		e2 := fw.NewEntity()
		flecs.Set(fw, e2, marshalVel{DX: 3, DY: 4})
	})

	data1 := mustMarshal(t, w)

	w2 := flecs.New()
	flecs.RegisterComponent[marshalPos](w2)
	flecs.RegisterComponent[marshalVel](w2)
	if err := w2.UnmarshalJSON(data1); err != nil {
		t.Fatalf("first unmarshal: %v", err)
	}

	data2 := mustMarshal(t, w2)

	// Both should be valid JSON and have equal entity counts.
	if !json.Valid(data1) || !json.Valid(data2) {
		t.Fatal("invalid JSON in two-step round-trip")
	}
	var j1, j2 struct {
		Entities []json.RawMessage `json:"entities"`
	}
	if err := json.Unmarshal(data1, &j1); err != nil {
		t.Fatalf("parse data1: %v", err)
	}
	if err := json.Unmarshal(data2, &j2); err != nil {
		t.Fatalf("parse data2: %v", err)
	}
	if len(j1.Entities) != len(j2.Entities) {
		t.Fatalf("entity count mismatch: %d vs %d", len(j1.Entities), len(j2.Entities))
	}
}

// ── Phase 9.2.2: ChildOf hierarchy tests ──────────────────────────────────────

func TestMarshalParentChildRoundTrip(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		root := fw.NewEntity()
		w.SetName(root, "scene")
		car := fw.NewEntity()
		w.SetName(car, "car")
		flecs.AddID(fw, car, flecs.MakePair(w.ChildOf(), root))
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	w2 := flecs.New()
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// Verify scene.car path exists in w2.
	sceneID, ok := w2.Lookup("scene")
	if !ok {
		t.Fatal("scene entity not found after unmarshal")
	}
	carID, ok := w2.LookupChild(sceneID, "car")
	if !ok {
		t.Fatal("car entity not found as child of scene")
	}
	parentID, ok := w2.ParentOf(carID)
	if !ok {
		t.Fatal("car has no parent after unmarshal")
	}
	if parentID != sceneID {
		t.Fatalf("car parent: got %v, want %v (scene)", parentID, sceneID)
	}
	if got := w2.PathOf(carID); got != "scene.car" {
		t.Fatalf("PathOf car: got %q, want \"scene.car\"", got)
	}
}

func TestMarshalMultiLevelHierarchy(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		root := fw.NewEntity()
		w.SetName(root, "root")
		parent := fw.NewEntity()
		w.SetName(parent, "parent")
		flecs.AddID(fw, parent, flecs.MakePair(w.ChildOf(), root))
		child := fw.NewEntity()
		w.SetName(child, "child")
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	w2 := flecs.New()
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	childID, ok := w2.Lookup("root.parent.child")
	if !ok {
		t.Fatal("lookup root.parent.child failed")
	}
	if got := w2.PathOf(childID); got != "root.parent.child" {
		t.Fatalf("PathOf: got %q, want \"root.parent.child\"", got)
	}
}

func TestMarshalWideHierarchy(t *testing.T) {
	const n = 5
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		root := fw.NewEntity()
		w.SetName(root, "root")
		for i := range n {
			c := fw.NewEntity()
			w.SetName(c, fmt.Sprintf("child%d", i))
			flecs.AddID(fw, c, flecs.MakePair(w.ChildOf(), root))
		}
	})

	data := mustMarshal(t, w)
	w2 := flecs.New()
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	rootID, ok := w2.Lookup("root")
	if !ok {
		t.Fatal("root not found")
	}
	count := 0
	w2.EachChild(rootID, func(_ flecs.ID) bool {
		count++
		return true
	})
	if count != n {
		t.Fatalf("EachChild: got %d children, want %d", count, n)
	}
}

func TestMarshalCascadeDeleteAfterUnmarshal(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		root := fw.NewEntity()
		w.SetName(root, "root")
		child := fw.NewEntity()
		w.SetName(child, "child")
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), root))
	})

	data := mustMarshal(t, w)
	w2 := flecs.New()
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	rootID, _ := w2.Lookup("root")
	childID, _ := w2.Lookup("root.child")
	if !w2.IsAlive(childID) {
		t.Fatal("child should be alive before delete")
	}
	w2.Delete(rootID)
	if w2.IsAlive(childID) {
		t.Fatal("child should be dead after parent delete (cascade)")
	}
}

func TestMarshalTopologicalOrder(t *testing.T) {
	// Allocate child BEFORE parent in the world, then verify parent serial < child serial.
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		child := fw.NewEntity()
		w.SetName(child, "child")
		parent := fw.NewEntity()
		w.SetName(parent, "parent")
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	var jw struct {
		Entities []struct {
			Serial int    `json:"serial"`
			Name   string `json:"name"`
			Parent int    `json:"parent"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(data, &jw); err != nil {
		t.Fatalf("parse: %v", err)
	}

	serialByName := make(map[string]int)
	parentSerialByName := make(map[string]int)
	for _, e := range jw.Entities {
		serialByName[e.Name] = e.Serial
		parentSerialByName[e.Name] = e.Parent
	}

	if serialByName["parent"] >= serialByName["child"] {
		t.Fatalf("parent serial %d should be < child serial %d", serialByName["parent"], serialByName["child"])
	}
	if parentSerialByName["child"] != serialByName["parent"] {
		t.Fatalf("child.parent field %d should equal parent's serial %d", parentSerialByName["child"], serialByName["parent"])
	}
}

// TestMarshalCycleDetection verifies that ChildOf cycles are rejected at write
// time (ChildOf is bootstrapped as Acyclic in v0.41.0). The cycle can no longer
// be stored, so MarshalJSON's cycle-detection code is now defence-in-depth for
// corrupted data; the primary guard is at AddID.
func TestMarshalCycleDetection(t *testing.T) {
	w := flecs.New()
	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		w.Write(func(fw *flecs.Writer) {
			a := fw.NewEntity()
			b := fw.NewEntity()
			// A→B is fine; B→A must panic because ChildOf is Acyclic.
			flecs.AddID(fw, a, flecs.MakePair(w.ChildOf(), b))
			flecs.AddID(fw, b, flecs.MakePair(w.ChildOf(), a))
		})
	}()
	if !panicked {
		t.Fatal("expected panic when constructing a ChildOf cycle (ChildOf is bootstrapped as Acyclic)")
	}
}

func TestMarshalMissingParentSerialError(t *testing.T) {
	data := []byte(`{"version":1,"entities":[{"serial":1,"parent":99}]}`)
	w := flecs.New()
	err := w.UnmarshalJSON(data)
	if err == nil {
		t.Fatal("expected error for missing parent serial")
	}
	if !strings.Contains(err.Error(), "unknown parent serial") {
		t.Fatalf("error should mention 'unknown parent serial': %v", err)
	}
}

func TestMarshalEntityWithParentNoNameNoComponents(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		root := fw.NewEntity()
		w.SetName(root, "root")
		anon := fw.NewEntity()
		flecs.AddID(fw, anon, flecs.MakePair(w.ChildOf(), root))
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	w2 := flecs.New()
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	rootID, _ := w2.Lookup("root")
	count := 0
	w2.EachChild(rootID, func(_ flecs.ID) bool {
		count++
		return true
	})
	if count != 1 {
		t.Fatalf("expected 1 anonymous child, got %d", count)
	}
}

func TestMarshalHierarchyTwoStepRoundTrip(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		root := fw.NewEntity()
		w.SetName(root, "root")
		child := fw.NewEntity()
		w.SetName(child, "child")
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), root))
	})

	data1 := mustMarshal(t, w)

	w2 := flecs.New()
	if err := w2.UnmarshalJSON(data1); err != nil {
		t.Fatalf("first unmarshal: %v", err)
	}
	data2 := mustMarshal(t, w2)

	if !json.Valid(data1) || !json.Valid(data2) {
		t.Fatal("invalid JSON in two-step round-trip")
	}

	// Both outputs must have 2 entities with the same parent/child structure.
	type entryShape struct {
		Serial int `json:"serial"`
		Parent int `json:"parent"`
	}
	var jw1, jw2 struct {
		Entities []entryShape `json:"entities"`
	}
	if err := json.Unmarshal(data1, &jw1); err != nil {
		t.Fatalf("parse data1: %v", err)
	}
	if err := json.Unmarshal(data2, &jw2); err != nil {
		t.Fatalf("parse data2: %v", err)
	}
	if len(jw1.Entities) != len(jw2.Entities) {
		t.Fatalf("entity count: %d vs %d", len(jw1.Entities), len(jw2.Entities))
	}
	// Root has no parent; child's parent equals root's serial.
	for _, jw := range []struct{ Entities []entryShape }{{jw1.Entities}, {jw2.Entities}} {
		if jw.Entities[0].Parent != 0 {
			t.Errorf("first entity (root) should have no parent, got %d", jw.Entities[0].Parent)
		}
		if jw.Entities[1].Parent != jw.Entities[0].Serial {
			t.Errorf("second entity parent=%d, want %d (first serial)", jw.Entities[1].Parent, jw.Entities[0].Serial)
		}
	}
}

func TestMarshalParentNotInJSONWhenAbsent(t *testing.T) {
	// Existing entities with no parent must not have "parent" in JSON.
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		w.SetName(e, "solo")
	})

	data := mustMarshal(t, w)
	if strings.Contains(string(data), `"parent"`) {
		t.Errorf("'parent' field should be absent for entities without a parent: %s", data)
	}
}

func TestMarshalMultipleChildOfParents(t *testing.T) {
	// ChildOf is exclusive (v0.34.0): adding a second parent replaces the first.
	// Adding (ChildOf, p1) then (ChildOf, p2) in the same scope leaves only p2.
	// The JSON output must reference p2 as the sole parent.
	w := flecs.New()
	var p1, p2, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		w.SetName(p1, "parent1")
		p2 = fw.NewEntity()
		w.SetName(p2, "parent2")
		child = fw.NewEntity()
		w.SetName(child, "child")
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), p1))
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), p2))
	})

	// Verify exclusive enforcement took effect.
	w.Read(func(fr *flecs.Reader) {
		if fr.HasID(child, flecs.MakePair(w.ChildOf(), p1)) {
			t.Error("expected (ChildOf, parent1) to be replaced by exclusive enforcement")
		}
		if !fr.HasID(child, flecs.MakePair(w.ChildOf(), p2)) {
			t.Error("expected (ChildOf, parent2) to be the sole parent")
		}
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	var jw struct {
		Entities []struct {
			Serial int    `json:"serial"`
			Name   string `json:"name"`
			Parent int    `json:"parent"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(data, &jw); err != nil {
		t.Fatalf("parse: %v", err)
	}

	serialByName := make(map[string]int)
	parentSerialByName := make(map[string]int)
	for _, e := range jw.Entities {
		serialByName[e.Name] = e.Serial
		parentSerialByName[e.Name] = e.Parent
	}

	// Child must reference exactly one parent — parent2 (exclusive replace leaves p2).
	if parentSerialByName["child"] == 0 {
		t.Fatal("child should have a parent field in JSON")
	}
	if parentSerialByName["child"] != serialByName["parent2"] {
		t.Fatalf("child.parent=%d want %d (parent2); exclusive ChildOf means only the last-added parent survives",
			parentSerialByName["child"], serialByName["parent2"])
	}
}

func TestMarshalSiblingOrder(t *testing.T) {
	// Siblings allocated in order sib0..sib3 must appear in that same order in
	// the JSON output (entity-allocation order, per the topo-sort guarantee).
	const n = 4
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		root := fw.NewEntity()
		w.SetName(root, "root")
		for i := range n {
			c := fw.NewEntity()
			w.SetName(c, fmt.Sprintf("sib%d", i))
			flecs.AddID(fw, c, flecs.MakePair(w.ChildOf(), root))
		}
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	var jw struct {
		Entities []struct {
			Name   string `json:"name"`
			Parent int    `json:"parent"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(data, &jw); err != nil {
		t.Fatalf("parse: %v", err)
	}

	var siblings []string
	for _, e := range jw.Entities {
		if e.Parent != 0 {
			siblings = append(siblings, e.Name)
		}
	}
	if len(siblings) != n {
		t.Fatalf("expected %d siblings, got %d: %v", n, len(siblings), siblings)
	}
	for i, name := range siblings {
		want := fmt.Sprintf("sib%d", i)
		if name != want {
			t.Fatalf("siblings[%d]=%q want %q; siblings must appear in entity-allocation order", i, name, want)
		}
	}
}

// ── Phase 9.2.3: IsA prefab serialization tests ───────────────────────────────

func TestMarshalIsASinglePrefabRoundTrip(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[marshalPos](w)
	w.Write(func(fw *flecs.Writer) {
		prefab := fw.NewEntity()
		flecs.Set(fw, prefab, marshalPos{X: 1, Y: 1})
		child := fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	w2 := flecs.New()
	flecs.RegisterComponent[marshalPos](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// Find the child (it has IsA, no local Position).
	skip := nonDataEntities(w2)
	var prefabID2, childID2 flecs.ID
	w2.EachEntity(func(e flecs.ID) bool {
		if _, ok := skip[e]; ok {
			return true
		}
		var hasPrefab bool
		var p flecs.ID
		w2.Read(func(r *flecs.Reader) {
			p, hasPrefab = flecs.PrefabOf(r, e)
		})
		if hasPrefab {
			childID2 = e
			prefabID2 = p
		}
		return true
	})
	if childID2 == 0 {
		t.Fatal("child entity not found after unmarshal")
	}
	_ = prefabID2

	// Child inherits Position from prefab via IsA.
	var pos marshalPos
	var posOk bool
	w2.Read(func(r *flecs.Reader) {
		pos, posOk = flecs.Get[marshalPos](r, childID2)
	})
	if !posOk {
		t.Fatal("expected child to inherit marshalPos from prefab")
	}
	if pos.X != 1 || pos.Y != 1 {
		t.Fatalf("inherited position: got %+v, want {1 1}", pos)
	}
}

func TestMarshalIsAMultiPrefabRoundTrip(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[marshalPos](w)
	flecs.RegisterComponent[marshalVel](w)
	w.Write(func(fw *flecs.Writer) {
		p1 := fw.NewEntity()
		flecs.Set(fw, p1, marshalPos{X: 1, Y: 1})
		p2 := fw.NewEntity()
		flecs.Set(fw, p2, marshalVel{DX: 3, DY: 4})
		child := fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), p1))
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), p2))
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	// Verify JSON contains prefabs array with 2 entries for the child.
	var jw struct {
		Entities []struct {
			Serial  int   `json:"serial"`
			Prefabs []int `json:"prefabs"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(data, &jw); err != nil {
		t.Fatalf("parse: %v", err)
	}
	var childEntry *struct {
		Serial  int   `json:"serial"`
		Prefabs []int `json:"prefabs"`
	}
	for i := range jw.Entities {
		if len(jw.Entities[i].Prefabs) > 0 {
			childEntry = &jw.Entities[i]
		}
	}
	if childEntry == nil {
		t.Fatal("no entity with prefabs field found in JSON")
	}
	if len(childEntry.Prefabs) != 2 {
		t.Fatalf("expected 2 prefabs, got %d: %v", len(childEntry.Prefabs), childEntry.Prefabs)
	}

	// Round-trip: both prefab relationships are restored.
	w2 := flecs.New()
	flecs.RegisterComponent[marshalPos](w2)
	flecs.RegisterComponent[marshalVel](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	skip := nonDataEntities(w2)
	var childID2 flecs.ID
	w2.EachEntity(func(e flecs.ID) bool {
		if _, ok := skip[e]; ok {
			return true
		}
		var hasPrefab2 bool
		w2.Read(func(r *flecs.Reader) {
			_, hasPrefab2 = flecs.PrefabOf(r, e)
		})
		if hasPrefab2 {
			childID2 = e
		}
		return true
	})
	if childID2 == 0 {
		t.Fatal("child entity not found after unmarshal")
	}
	w2.Read(func(r *flecs.Reader) {
		if _, ok := flecs.Get[marshalPos](r, childID2); !ok {
			t.Fatal("child should inherit marshalPos from p1")
		}
		if _, ok := flecs.Get[marshalVel](r, childID2); !ok {
			t.Fatal("child should inherit marshalVel from p2")
		}
	})
}

func TestMarshalIsAFirstPrefabWins(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[marshalPos](w)
	w.Write(func(fw *flecs.Writer) {
		p1 := fw.NewEntity()
		flecs.Set(fw, p1, marshalPos{X: 10, Y: 10})
		p2 := fw.NewEntity()
		flecs.Set(fw, p2, marshalPos{X: 20, Y: 20})
		child := fw.NewEntity()
		// p1 is added first → first-prefab-wins: child inherits from p1
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), p1))
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), p2))
	})

	data := mustMarshal(t, w)
	w2 := flecs.New()
	flecs.RegisterComponent[marshalPos](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	skip := nonDataEntities(w2)
	var childID2 flecs.ID
	w2.EachEntity(func(e flecs.ID) bool {
		if _, ok := skip[e]; ok {
			return true
		}
		var hasPrefab3 bool
		w2.Read(func(r *flecs.Reader) {
			_, hasPrefab3 = flecs.PrefabOf(r, e)
		})
		if hasPrefab3 {
			childID2 = e
		}
		return true
	})
	if childID2 == 0 {
		t.Fatal("child not found after unmarshal")
	}
	var pos marshalPos
	var posOk2 bool
	w2.Read(func(r *flecs.Reader) {
		pos, posOk2 = flecs.Get[marshalPos](r, childID2)
	})
	if !posOk2 {
		t.Fatal("expected inherited marshalPos")
	}
	// First-prefab-wins: should get p1's value {10, 10}.
	if pos.X != 10 || pos.Y != 10 {
		t.Fatalf("first-prefab-wins: got %+v, want {10 10}", pos)
	}
}

func TestMarshalIsATopoOrder(t *testing.T) {
	// Allocate child BEFORE prefab; after topo-sort, prefab serial < child serial.
	w := flecs.New()
	flecs.RegisterComponent[marshalPos](w)
	w.Write(func(fw *flecs.Writer) {
		child := fw.NewEntity()
		w.SetName(child, "child")
		prefab := fw.NewEntity()
		w.SetName(prefab, "prefab")
		flecs.Set(fw, prefab, marshalPos{X: 5, Y: 5})
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	var jw struct {
		Entities []struct {
			Serial  int    `json:"serial"`
			Name    string `json:"name"`
			Prefabs []int  `json:"prefabs"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(data, &jw); err != nil {
		t.Fatalf("parse: %v", err)
	}
	serialByName := make(map[string]int)
	for _, e := range jw.Entities {
		serialByName[e.Name] = e.Serial
	}
	if serialByName["prefab"] >= serialByName["child"] {
		t.Fatalf("prefab serial %d should be < child serial %d (prefab must precede its dependent)",
			serialByName["prefab"], serialByName["child"])
	}
}

func TestMarshalIsACombinedChildOfIsATopoOrder(t *testing.T) {
	// Entity has both ChildOf(parent) and IsA(prefab); both must serialize before it.
	w := flecs.New()
	flecs.RegisterComponent[marshalPos](w)
	// Allocate child first, then parent, then prefab — all reversed from topo order.
	w.Write(func(fw *flecs.Writer) {
		child := fw.NewEntity()
		w.SetName(child, "child")
		parent := fw.NewEntity()
		w.SetName(parent, "parent")
		prefab := fw.NewEntity()
		w.SetName(prefab, "prefab")
		flecs.Set(fw, prefab, marshalPos{X: 7, Y: 7})
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	var jw struct {
		Entities []struct {
			Serial  int    `json:"serial"`
			Name    string `json:"name"`
			Parent  int    `json:"parent"`
			Prefabs []int  `json:"prefabs"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(data, &jw); err != nil {
		t.Fatalf("parse: %v", err)
	}
	serialByName := make(map[string]int)
	for _, e := range jw.Entities {
		serialByName[e.Name] = e.Serial
	}
	if serialByName["parent"] >= serialByName["child"] {
		t.Fatalf("parent serial %d must be < child serial %d", serialByName["parent"], serialByName["child"])
	}
	if serialByName["prefab"] >= serialByName["child"] {
		t.Fatalf("prefab serial %d must be < child serial %d", serialByName["prefab"], serialByName["child"])
	}
}

func TestMarshalIsAMixedCycleDetection(t *testing.T) {
	// Create a mixed ChildOf→IsA cycle: a has ChildOf(b), b has IsA(a).
	// predecessors(a) = {b}, predecessors(b) = {a} → cycle.
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		a := fw.NewEntity()
		b := fw.NewEntity()
		flecs.AddID(fw, a, flecs.MakePair(w.ChildOf(), b))
		flecs.AddID(fw, b, flecs.MakePair(w.IsA(), a))
	})

	_, err := w.MarshalJSON()
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error should mention 'cycle': %v", err)
	}
}

func TestMarshalIsAMultipleIsANoChildOf(t *testing.T) {
	// Entity has IsA relationships but no ChildOf; "parent" must be absent, "prefabs" present.
	w := flecs.New()
	flecs.RegisterComponent[marshalPos](w)
	w.Write(func(fw *flecs.Writer) {
		p1 := fw.NewEntity()
		flecs.Set(fw, p1, marshalPos{X: 1, Y: 2})
		p2 := fw.NewEntity()
		flecs.Set(fw, p2, marshalPos{X: 3, Y: 4})
		child := fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), p1))
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), p2))
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	var jw struct {
		Entities []struct {
			Parent  int   `json:"parent"`
			Prefabs []int `json:"prefabs"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(data, &jw); err != nil {
		t.Fatalf("parse: %v", err)
	}
	var found bool
	for _, e := range jw.Entities {
		if len(e.Prefabs) > 0 {
			found = true
			if e.Parent != 0 {
				t.Errorf("entity with only IsA must have parent=0, got %d", e.Parent)
			}
			if len(e.Prefabs) != 2 {
				t.Errorf("expected 2 prefabs, got %d", len(e.Prefabs))
			}
		}
	}
	if !found {
		t.Fatal("no entity with prefabs field found in JSON")
	}
}

func TestMarshalNoIsARoundTripStable(t *testing.T) {
	// Entities with no IsA relationships must not emit a "prefabs" field.
	w := flecs.New()
	flecs.RegisterComponent[marshalPos](w)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, marshalPos{X: 1, Y: 2})
	})

	data := mustMarshal(t, w)
	if strings.Contains(string(data), `"prefabs"`) {
		t.Errorf(`"prefabs" field must be absent for entities without IsA: %s`, data)
	}
}

func TestMarshalIsAUnknownPrefabSerialError(t *testing.T) {
	data := []byte(`{"version":1,"entities":[{"serial":1,"prefabs":[999]}]}`)
	w := flecs.New()
	err := w.UnmarshalJSON(data)
	if err == nil {
		t.Fatal("expected error for unknown prefab serial")
	}
	if !strings.Contains(err.Error(), "unknown prefab serial 999") {
		t.Fatalf("error should mention 'unknown prefab serial 999': %v", err)
	}
}

func TestMarshalIsAChildOfCascadeAfterUnmarshal(t *testing.T) {
	// parent has IsA(prefab); child has ChildOf(parent).
	// After unmarshal, deleting parent must cascade-delete child.
	w := flecs.New()
	flecs.RegisterComponent[marshalPos](w)
	w.Write(func(fw *flecs.Writer) {
		prefab := fw.NewEntity()
		w.SetName(prefab, "prefab")
		flecs.Set(fw, prefab, marshalPos{X: 1, Y: 1})
		parent := fw.NewEntity()
		w.SetName(parent, "parent")
		flecs.AddID(fw, parent, flecs.MakePair(w.IsA(), prefab))
		child := fw.NewEntity()
		w.SetName(child, "child")
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
	})

	data := mustMarshal(t, w)
	w2 := flecs.New()
	flecs.RegisterComponent[marshalPos](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	parentID2, ok := w2.Lookup("parent")
	if !ok {
		t.Fatal("parent not found after unmarshal")
	}
	childID2, ok := w2.LookupChild(parentID2, "child")
	if !ok {
		t.Fatal("child not found as child of parent after unmarshal")
	}
	if !w2.IsAlive(childID2) {
		t.Fatal("child should be alive before delete")
	}
	w2.Delete(parentID2)
	if w2.IsAlive(childID2) {
		t.Fatal("child should be dead after cascade delete of parent")
	}
}

func TestMarshalIsAOverrideAfterIsA(t *testing.T) {
	// Child has IsA(prefab) and a local override of Position.
	// Marshal must include both prefabs and the local component.
	// After unmarshal, Get[Position](child) returns the local value (override wins).
	w := flecs.New()
	flecs.RegisterComponent[marshalPos](w)
	w.Write(func(fw *flecs.Writer) {
		prefab := fw.NewEntity()
		flecs.Set(fw, prefab, marshalPos{X: 1, Y: 1})
		child := fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
		flecs.Set(fw, child, marshalPos{X: 99, Y: 99}) // local override
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	// JSON must carry both prefabs and the local component.
	var jw struct {
		Entities []struct {
			Prefabs    []int                      `json:"prefabs"`
			Components map[string]json.RawMessage `json:"components"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(data, &jw); err != nil {
		t.Fatalf("parse: %v", err)
	}
	var childEntry *struct {
		Prefabs    []int                      `json:"prefabs"`
		Components map[string]json.RawMessage `json:"components"`
	}
	for i := range jw.Entities {
		if len(jw.Entities[i].Prefabs) > 0 {
			childEntry = &jw.Entities[i]
		}
	}
	if childEntry == nil {
		t.Fatal("no entity with prefabs found in JSON")
	}
	if len(childEntry.Prefabs) != 1 {
		t.Fatalf("expected 1 prefab serial, got %d", len(childEntry.Prefabs))
	}
	if _, hasComp := childEntry.Components["flecs_test.marshalPos"]; !hasComp {
		// Try without package prefix in case the name differs.
		found := false
		for k := range childEntry.Components {
			if strings.Contains(k, "marshalPos") {
				found = true
			}
		}
		if !found {
			t.Fatalf("child entity should have local marshalPos in components map; got: %v", childEntry.Components)
		}
	}

	// Unmarshal and verify override wins.
	w2 := flecs.New()
	flecs.RegisterComponent[marshalPos](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	skip := nonDataEntities(w2)
	var childID2 flecs.ID
	w2.EachEntity(func(e flecs.ID) bool {
		if _, ok := skip[e]; ok {
			return true
		}
		var hasPrefab4 bool
		w2.Read(func(r *flecs.Reader) {
			_, hasPrefab4 = flecs.PrefabOf(r, e)
		})
		if hasPrefab4 {
			childID2 = e
		}
		return true
	})
	if childID2 == 0 {
		t.Fatal("child not found after unmarshal")
	}
	var pos2 marshalPos
	var posOk3 bool
	w2.Read(func(r *flecs.Reader) {
		pos2, posOk3 = flecs.Get[marshalPos](r, childID2)
	})
	if !posOk3 {
		t.Fatal("expected marshalPos on child")
	}
	if pos2.X != 99 || pos2.Y != 99 {
		t.Fatalf("override should win: got %+v, want {99 99}", pos2)
	}
}

func TestMarshalIsATwoStepRoundTrip(t *testing.T) {
	// marshal → unmarshal → marshal should produce structurally identical JSON.
	w := flecs.New()
	flecs.RegisterComponent[marshalPos](w)
	w.Write(func(fw *flecs.Writer) {
		prefab := fw.NewEntity()
		w.SetName(prefab, "prefab")
		flecs.Set(fw, prefab, marshalPos{X: 3, Y: 4})
		child := fw.NewEntity()
		w.SetName(child, "child")
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	data1 := mustMarshal(t, w)

	w2 := flecs.New()
	flecs.RegisterComponent[marshalPos](w2)
	if err := w2.UnmarshalJSON(data1); err != nil {
		t.Fatalf("first unmarshal: %v", err)
	}
	data2 := mustMarshal(t, w2)

	if !json.Valid(data1) || !json.Valid(data2) {
		t.Fatal("invalid JSON in two-step round-trip")
	}

	type entShape struct {
		Name    string `json:"name"`
		Prefabs []int  `json:"prefabs"`
	}
	var jw1, jw2 struct {
		Entities []entShape `json:"entities"`
	}
	if err := json.Unmarshal(data1, &jw1); err != nil {
		t.Fatalf("parse data1: %v", err)
	}
	if err := json.Unmarshal(data2, &jw2); err != nil {
		t.Fatalf("parse data2: %v", err)
	}
	if len(jw1.Entities) != len(jw2.Entities) {
		t.Fatalf("entity count mismatch: %d vs %d", len(jw1.Entities), len(jw2.Entities))
	}
	// Both should have one entity with prefabs and one without.
	prefabCount1, prefabCount2 := 0, 0
	for _, e := range jw1.Entities {
		if len(e.Prefabs) > 0 {
			prefabCount1++
		}
	}
	for _, e := range jw2.Entities {
		if len(e.Prefabs) > 0 {
			prefabCount2++
		}
	}
	if prefabCount1 != 1 || prefabCount2 != 1 {
		t.Fatalf("expected exactly 1 entity with prefabs in each marshal: got %d, %d", prefabCount1, prefabCount2)
	}
}

// ── Phase 9.2.4: Custom pair component serialization tests ────────────────────

type marshalEdge struct{ Weight float32 }
type marshalEdge2 struct{ Label string }

func TestMarshalTagOnlyPairRoundTrip(t *testing.T) {
	w := flecs.New()
	var follows, alice, bob flecs.ID
	w.Write(func(fw *flecs.Writer) {
		follows = fw.NewEntity()
		alice = fw.NewEntity()
		bob = fw.NewEntity()
		flecs.AddID(fw, alice, flecs.MakePair(follows, bob))
	})
	_, _, _ = follows, alice, bob

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	w2 := flecs.New()
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// Find the three user entities: follows, alice, bob in topo order.
	skip := nonDataEntities(w2)
	var ents []flecs.ID
	w2.EachEntity(func(e flecs.ID) bool {
		if _, ok := skip[e]; !ok {
			ents = append(ents, e)
		}
		return true
	})
	if len(ents) != 3 {
		t.Fatalf("expected 3 user entities, got %d", len(ents))
	}
	// The pair (follows, bob) should exist on alice. We verify by checking any
	// entity has a pair. Since the entities are anonymous we check via JSON structure.
	var jw struct {
		Entities []struct {
			Pairs []struct {
				Rel int `json:"rel"`
				Tgt int `json:"tgt"`
			} `json:"pairs"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(data, &jw); err != nil {
		t.Fatalf("parse: %v", err)
	}
	hasPair := false
	for _, e := range jw.Entities {
		if len(e.Pairs) > 0 {
			hasPair = true
			if e.Pairs[0].Rel == 0 || e.Pairs[0].Tgt == 0 {
				t.Errorf("pair has zero rel or tgt: %+v", e.Pairs[0])
			}
		}
	}
	if !hasPair {
		t.Fatal("no entity has pairs in JSON")
	}

	// Verify pair is present in w2: one entity should have a pair component.
	found := false
	for _, e := range ents {
		comps := w2.EntityComponents(e)
		for _, cid := range comps {
			if cid.IsPair() {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no entity has a pair component after unmarshal")
	}
}

func TestMarshalDataBearingPairRoundTrip(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[marshalEdge](w)
	var follows, alice, bob flecs.ID
	w.Write(func(fw *flecs.Writer) {
		follows = fw.NewEntity()
		alice = fw.NewEntity()
		bob = fw.NewEntity()
		flecs.SetPair[marshalEdge](fw, alice, follows, bob, marshalEdge{Weight: 0.8})
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	// Verify pairs array in JSON has dataType set.
	var jw struct {
		Entities []struct {
			Pairs []struct {
				Rel      int    `json:"rel"`
				Tgt      int    `json:"tgt"`
				DataType string `json:"dataType"`
			} `json:"pairs"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(data, &jw); err != nil {
		t.Fatalf("parse: %v", err)
	}
	var pairEntry *struct {
		Rel      int    `json:"rel"`
		Tgt      int    `json:"tgt"`
		DataType string `json:"dataType"`
	}
	for i := range jw.Entities {
		for j := range jw.Entities[i].Pairs {
			pairEntry = &jw.Entities[i].Pairs[j]
		}
	}
	if pairEntry == nil {
		t.Fatal("no pair found in JSON")
	}
	if !strings.Contains(pairEntry.DataType, "marshalEdge") {
		t.Errorf("expected dataType to contain 'marshalEdge', got %q", pairEntry.DataType)
	}

	w2 := flecs.New()
	flecs.RegisterComponent[marshalEdge](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// Find alice (the entity with a pair).
	skip := nonDataEntities(w2)
	var aliceID2 flecs.ID
	w2.EachEntity(func(e flecs.ID) bool {
		if _, ok := skip[e]; ok {
			return true
		}
		for _, cid := range w2.EntityComponents(e) {
			if cid.IsPair() {
				aliceID2 = e
			}
		}
		return true
	})
	if aliceID2 == 0 {
		t.Fatal("no entity with pair found after unmarshal")
	}

	// Extract follows2 and bob2 from the pair.
	var follows2, bob2 flecs.ID
	for _, cid := range w2.EntityComponents(aliceID2) {
		if cid.IsPair() {
			follows2 = cid.First()
			bob2 = cid.Second()
		}
	}
	var edge marshalEdge
	var edgeOk bool
	w2.Read(func(r *flecs.Reader) { edge, edgeOk = flecs.GetPair[marshalEdge](r, aliceID2, follows2, bob2) })
	if !edgeOk {
		t.Fatal("expected marshalEdge pair on restored alice")
	}
	if edge.Weight != 0.8 {
		t.Fatalf("edge weight: got %v, want 0.8", edge.Weight)
	}
}

func TestMarshalMixedChildOfIsAPairRoundTrip(t *testing.T) {
	// One entity has ChildOf(parent) + IsA(prefab) + custom pair(follows, bob).
	// All three must serialize via their respective fields and round-trip correctly.
	w := flecs.New()
	flecs.RegisterComponent[marshalEdge](w)
	var prefab, parent, follows, bob, alice flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		w.SetName(prefab, "prefab")
		parent = fw.NewEntity()
		w.SetName(parent, "parent")
		follows = fw.NewEntity()
		w.SetName(follows, "follows")
		bob = fw.NewEntity()
		w.SetName(bob, "bob")
		alice = fw.NewEntity()
		w.SetName(alice, "alice")
		flecs.AddID(fw, alice, flecs.MakePair(w.ChildOf(), parent))
		flecs.AddID(fw, alice, flecs.MakePair(w.IsA(), prefab))
		flecs.SetPair[marshalEdge](fw, alice, follows, bob, marshalEdge{Weight: 1.5})
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	var jw struct {
		Entities []struct {
			Name    string `json:"name"`
			Parent  int    `json:"parent"`
			Prefabs []int  `json:"prefabs"`
			Pairs   []struct {
				DataType string `json:"dataType"`
			} `json:"pairs"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(data, &jw); err != nil {
		t.Fatalf("parse: %v", err)
	}
	var aliceEntry *struct {
		Name    string `json:"name"`
		Parent  int    `json:"parent"`
		Prefabs []int  `json:"prefabs"`
		Pairs   []struct {
			DataType string `json:"dataType"`
		} `json:"pairs"`
	}
	for i := range jw.Entities {
		if jw.Entities[i].Name == "alice" {
			aliceEntry = &jw.Entities[i]
		}
	}
	if aliceEntry == nil {
		t.Fatal("alice not found in JSON")
	}
	if aliceEntry.Parent == 0 {
		t.Error("alice must have parent field")
	}
	if len(aliceEntry.Prefabs) != 1 {
		t.Errorf("alice must have 1 prefab, got %d", len(aliceEntry.Prefabs))
	}
	if len(aliceEntry.Pairs) != 1 {
		t.Errorf("alice must have 1 custom pair, got %d", len(aliceEntry.Pairs))
	}

	// Round-trip.
	w2 := flecs.New()
	flecs.RegisterComponent[marshalEdge](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	aliceID2, ok := w2.Lookup("parent.alice")
	if !ok {
		t.Fatal("alice not found in w2")
	}
	// ChildOf restored.
	parentID2, ok := w2.Lookup("parent")
	if !ok {
		t.Fatal("parent not found in w2")
	}
	if p, ok2 := w2.ParentOf(aliceID2); !ok2 || p != parentID2 {
		t.Error("ChildOf not restored correctly")
	}
	// IsA restored.
	prefabID2, ok := w2.Lookup("prefab")
	if !ok {
		t.Fatal("prefab not found in w2")
	}
	var hasIsA bool
	w2.Read(func(r *flecs.Reader) { hasIsA = flecs.HasID(r, aliceID2, flecs.MakePair(w2.IsA(), prefabID2)) })
	if !hasIsA {
		t.Error("IsA not restored correctly")
	}
	// Custom pair restored.
	followsID2, ok := w2.Lookup("follows")
	if !ok {
		t.Fatal("follows not found in w2")
	}
	bobID2, ok := w2.Lookup("bob")
	if !ok {
		t.Fatal("bob not found in w2")
	}
	var edge marshalEdge
	var edgeOk bool
	w2.Read(func(r *flecs.Reader) { edge, edgeOk = flecs.GetPair[marshalEdge](r, aliceID2, followsID2, bobID2) })
	if !edgeOk {
		t.Fatal("custom pair not restored")
	}
	if edge.Weight != 1.5 {
		t.Fatalf("edge weight: got %v, want 1.5", edge.Weight)
	}
}

func TestMarshalMultipleCustomPairsRoundTrip(t *testing.T) {
	// alice has (follows, bob), (follows, charlie), (likes, dave).
	w := flecs.New()
	flecs.RegisterComponent[marshalEdge](w)
	var follows, likes, alice, bob, charlie, dave flecs.ID
	w.Write(func(fw *flecs.Writer) {
		follows = fw.NewEntity()
		w.SetName(follows, "follows")
		likes = fw.NewEntity()
		w.SetName(likes, "likes")
		alice = fw.NewEntity()
		w.SetName(alice, "alice")
		bob = fw.NewEntity()
		w.SetName(bob, "bob")
		charlie = fw.NewEntity()
		w.SetName(charlie, "charlie")
		dave = fw.NewEntity()
		w.SetName(dave, "dave")
		flecs.SetPair[marshalEdge](fw, alice, follows, bob, marshalEdge{Weight: 1.0})
		flecs.SetPair[marshalEdge](fw, alice, follows, charlie, marshalEdge{Weight: 2.0})
		flecs.SetPair[marshalEdge](fw, alice, likes, dave, marshalEdge{Weight: 3.0})
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	w2 := flecs.New()
	flecs.RegisterComponent[marshalEdge](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	aliceID2, ok := w2.Lookup("alice")
	if !ok {
		t.Fatal("alice not found")
	}
	followsID2, _ := w2.Lookup("follows")
	likesID2, _ := w2.Lookup("likes")
	bobID2, _ := w2.Lookup("bob")
	charlieID2, _ := w2.Lookup("charlie")
	daveID2, _ := w2.Lookup("dave")

	w2.Read(func(r *flecs.Reader) {
		e1, ok := flecs.GetPair[marshalEdge](r, aliceID2, followsID2, bobID2)
		if !ok || e1.Weight != 1.0 {
			t.Errorf("(follows, bob) pair: got %v ok=%v, want Weight=1.0", e1, ok)
		}
		e2, ok := flecs.GetPair[marshalEdge](r, aliceID2, followsID2, charlieID2)
		if !ok || e2.Weight != 2.0 {
			t.Errorf("(follows, charlie) pair: got %v ok=%v, want Weight=2.0", e2, ok)
		}
		e3, ok := flecs.GetPair[marshalEdge](r, aliceID2, likesID2, daveID2)
		if !ok || e3.Weight != 3.0 {
			t.Errorf("(likes, dave) pair: got %v ok=%v, want Weight=3.0", e3, ok)
		}
	})
}

func TestMarshalChildOfIsANotInPairsField(t *testing.T) {
	// ChildOf and IsA pairs must NOT appear in the "pairs" field.
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		parent := fw.NewEntity()
		w.SetName(parent, "parent")
		prefab := fw.NewEntity()
		w.SetName(prefab, "prefab")
		child := fw.NewEntity()
		w.SetName(child, "child")
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	var jw struct {
		Entities []struct {
			Name    string        `json:"name"`
			Parent  int           `json:"parent"`
			Prefabs []int         `json:"prefabs"`
			Pairs   []interface{} `json:"pairs"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(data, &jw); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, e := range jw.Entities {
		if e.Name == "child" {
			if e.Parent == 0 {
				t.Error("child must have parent field")
			}
			if len(e.Prefabs) != 1 {
				t.Errorf("child must have 1 prefab, got %d", len(e.Prefabs))
			}
			if len(e.Pairs) != 0 {
				t.Errorf("child must have empty pairs field (ChildOf/IsA excluded), got %d pairs", len(e.Pairs))
			}
		}
	}
}

func TestMarshalUnknownPairDataTypeError(t *testing.T) {
	// Hand-crafted JSON with an unknown pair dataType should return an error.
	data := []byte(`{"version":1,"entities":[
		{"serial":1},
		{"serial":2},
		{"serial":3,"pairs":[{"rel":1,"tgt":2,"dataType":"pkg.NotRegistered","data":{}}]}
	]}`)
	w := flecs.New()
	err := w.UnmarshalJSON(data)
	if err == nil {
		t.Fatal("expected error for unregistered pair data type")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Errorf("error should mention 'not registered': %v", err)
	}
}

func TestMarshalUnknownPairRelSerialError(t *testing.T) {
	data := []byte(`{"version":1,"entities":[
		{"serial":1,"pairs":[{"rel":999,"tgt":1}]}
	]}`)
	w := flecs.New()
	err := w.UnmarshalJSON(data)
	if err == nil {
		t.Fatal("expected error for unknown pair rel serial")
	}
	if !strings.Contains(err.Error(), "pair rel serial 999") {
		t.Errorf("error should mention 'pair rel serial 999': %v", err)
	}
}

func TestMarshalUnknownPairTgtSerialError(t *testing.T) {
	data := []byte(`{"version":1,"entities":[
		{"serial":1},
		{"serial":2,"pairs":[{"rel":1,"tgt":999}]}
	]}`)
	w := flecs.New()
	err := w.UnmarshalJSON(data)
	if err == nil {
		t.Fatal("expected error for unknown pair tgt serial")
	}
	if !strings.Contains(err.Error(), "pair tgt serial 999") {
		t.Errorf("error should mention 'pair tgt serial 999': %v", err)
	}
}

func TestSetPairByIDAutoRegisters(t *testing.T) {
	w := flecs.New()
	var follows, alice, bob flecs.ID
	w.Write(func(fw *flecs.Writer) {
		follows = fw.NewEntity()
		alice = fw.NewEntity()
		bob = fw.NewEntity()
	})
	w.SetPairByID(alice, follows, bob, marshalEdge{Weight: 0.5})

	// Verify the pair is set.
	pairID := flecs.MakePair(follows, bob)
	info, ok := w.ComponentInfo(pairID)
	if !ok {
		t.Fatal("pair not registered after SetPairByID")
	}
	if info.Size == 0 {
		t.Fatal("pair should have non-zero size")
	}
	if !strings.Contains(info.Name, "marshalEdge") {
		t.Errorf("pair info name should mention 'marshalEdge', got %q", info.Name)
	}

	// GetByID should return the value.
	v, ok := w.GetByID(alice, pairID)
	if !ok {
		t.Fatal("GetByID returned false for pair")
	}
	edge, ok := v.(marshalEdge)
	if !ok {
		t.Fatalf("expected marshalEdge, got %T", v)
	}
	if edge.Weight != 0.5 {
		t.Fatalf("weight: got %v, want 0.5", edge.Weight)
	}
}

func TestSetPairByIDTypeMismatchPanics(t *testing.T) {
	w := flecs.New()
	var follows, alice, bob flecs.ID
	w.Write(func(fw *flecs.Writer) {
		follows = fw.NewEntity()
		alice = fw.NewEntity()
		bob = fw.NewEntity()
	})
	w.SetPairByID(alice, follows, bob, marshalEdge{Weight: 1.0})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on type mismatch")
		}
		msg := fmt.Sprint(r)
		if !strings.Contains(msg, "already registered") {
			t.Errorf("panic message should mention 'already registered': %v", msg)
		}
	}()
	w.SetPairByID(alice, follows, bob, marshalEdge2{Label: "x"})
}

func TestSetPairByIDFiresHooks(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[marshalEdge](w)
	var follows, alice, bob flecs.ID
	w.Write(func(fw *flecs.Writer) {
		follows = fw.NewEntity()
		alice = fw.NewEntity()
		bob = fw.NewEntity()
	})

	var onSetCount int
	flecs.OnSet[marshalEdge](w, func(_ *flecs.Writer, _ flecs.ID, _ marshalEdge) {
		onSetCount++
	})

	w.SetPairByID(alice, follows, bob, marshalEdge{Weight: 2.0})
	if onSetCount == 0 {
		t.Fatal("OnSet hook did not fire for SetPairByID")
	}
}

func TestAddIDAfterSetPairIsNoOp(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[marshalEdge](w)
	var follows, alice, bob flecs.ID
	w.Write(func(fw *flecs.Writer) {
		follows = fw.NewEntity()
		alice = fw.NewEntity()
		bob = fw.NewEntity()
		flecs.SetPair[marshalEdge](fw, alice, follows, bob, marshalEdge{Weight: 1.5})
	})
	pairID := flecs.MakePair(follows, bob)

	// AddID after SetPair on the same pairID must be idempotent: entity already has
	// the pair component, so AddID is a no-op and must not clear the data.
	w.Write(func(fw *flecs.Writer) { flecs.AddID(fw, alice, pairID) })

	var hasID bool
	w.Read(func(r *flecs.Reader) { hasID = flecs.HasID(r, alice, pairID) })
	if !hasID {
		t.Fatal("entity lost pair after AddID no-op")
	}
	v, ok := w.GetByID(alice, pairID)
	if !ok {
		t.Fatal("GetByID returned false after AddID no-op")
	}
	edge, ok := v.(marshalEdge)
	if !ok {
		t.Fatalf("unexpected type %T after AddID no-op", v)
	}
	if edge.Weight != 1.5 {
		t.Fatalf("pair data corrupted: got Weight=%v, want 1.5", edge.Weight)
	}
}

func TestMarshalPairsTwoStepRoundTripStable(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[marshalEdge](w)
	var follows, alice, bob flecs.ID
	w.Write(func(fw *flecs.Writer) {
		follows = fw.NewEntity()
		w.SetName(follows, "follows")
		alice = fw.NewEntity()
		w.SetName(alice, "alice")
		bob = fw.NewEntity()
		w.SetName(bob, "bob")
		flecs.SetPair[marshalEdge](fw, alice, follows, bob, marshalEdge{Weight: 0.7})
	})

	data1 := mustMarshal(t, w)

	w2 := flecs.New()
	flecs.RegisterComponent[marshalEdge](w2)
	if err := w2.UnmarshalJSON(data1); err != nil {
		t.Fatalf("first unmarshal: %v", err)
	}
	data2 := mustMarshal(t, w2)

	if !json.Valid(data1) || !json.Valid(data2) {
		t.Fatal("invalid JSON in two-step round-trip")
	}

	type pairShape struct {
		DataType string `json:"dataType"`
	}
	type entShape struct {
		Pairs []pairShape `json:"pairs"`
	}
	var jw1, jw2 struct {
		Entities []entShape `json:"entities"`
	}
	if err := json.Unmarshal(data1, &jw1); err != nil {
		t.Fatalf("parse data1: %v", err)
	}
	if err := json.Unmarshal(data2, &jw2); err != nil {
		t.Fatalf("parse data2: %v", err)
	}
	if len(jw1.Entities) != len(jw2.Entities) {
		t.Fatalf("entity count: %d vs %d", len(jw1.Entities), len(jw2.Entities))
	}
	count1, count2 := 0, 0
	for _, e := range jw1.Entities {
		count1 += len(e.Pairs)
	}
	for _, e := range jw2.Entities {
		count2 += len(e.Pairs)
	}
	if count1 != 1 || count2 != 1 {
		t.Fatalf("expected 1 pair in each marshal, got %d and %d", count1, count2)
	}
}
