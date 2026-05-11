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
// the 7 built-in entities plus all registered component entities.
func nonDataEntities(w *flecs.World) map[flecs.ID]struct{} {
	skip := map[flecs.ID]struct{}{
		w.ChildOf(): {}, w.IsA(): {}, w.Name(): {},
		w.PreUpdate(): {}, w.OnUpdate(): {}, w.PostUpdate(): {}, w.OnFixedUpdate(): {},
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
	e := w.NewEntity()
	flecs.Set(w, e, marshalPos{X: 1, Y: 2})

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
	p, ok := flecs.Get[marshalPos](w2, userEntities[0])
	if !ok {
		t.Fatal("expected marshalPos on restored entity")
	}
	if p.X != 1 || p.Y != 2 {
		t.Fatalf("position: got %+v, want {1 2}", p)
	}
}

func TestMarshalMultipleEntities(t *testing.T) {
	w := newMarshalWorld()
	for i := range 5 {
		e := w.NewEntity()
		flecs.Set(w, e, marshalPos{X: float32(i), Y: float32(i * 2)})
		if i%2 == 0 {
			flecs.Set(w, e, marshalVel{DX: float32(i), DY: 0})
		}
	}

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
	e1 := w.NewEntity()
	w.SetName(e1, "alpha")
	e2 := w.NewEntity()
	w.SetName(e2, "beta")
	e3 := w.NewEntity()
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
	a := w.NewEntity()
	flecs.Set(w, a, marshalPos{X: 99, Y: 99})

	// Marshal a separate world with one entity.
	w_src := newMarshalWorld()
	e := w_src.NewEntity()
	flecs.Set(w_src, e, marshalPos{X: 1, Y: 2})
	data := mustMarshal(t, w_src)

	// Unmarshal into w — A should still exist.
	if err := w.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	p, ok := flecs.Get[marshalPos](w, a)
	if !ok {
		t.Fatal("entity A lost after UnmarshalJSON into non-empty world")
	}
	if p.X != 99 || p.Y != 99 {
		t.Fatalf("entity A value corrupted: %+v", p)
	}
}

func TestMarshalTagComponents(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[marshalTag](w)
	e := w.NewEntity()
	flecs.Set(w, e, marshalTag{})

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
	if !flecs.Has[marshalTag](w2, found) {
		t.Fatal("marshalTag not restored")
	}
}

func TestMarshalUnregisteredComponentError(t *testing.T) {
	w := newMarshalWorld()
	e := w.NewEntity()
	flecs.Set(w, e, marshalPos{X: 1, Y: 2})
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
	parent := w.NewEntity()
	child := w.NewEntity()
	flecs.Set(w, child, marshalPos{X: 1, Y: 2})
	flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))

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
	e := w.NewEntity()
	flecs.Set(w, e, marshalPos{X: 1.5, Y: 2.25})
	flecs.Set(w, e, marshalStr{Tag: "hello"})

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
	p, ok := flecs.Get[marshalPos](w2, found)
	if !ok || p.X != 1.5 || p.Y != 2.25 {
		t.Fatalf("marshalPos: got %+v, want {1.5 2.25}", p)
	}
	s, ok := flecs.Get[marshalStr](w2, found)
	if !ok || s.Tag != "hello" {
		t.Fatalf("marshalStr: got %+v, want {hello}", s)
	}
}

func TestMarshalNestedStructs(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[marshalNested](w)
	e := w.NewEntity()
	flecs.Set(w, e, marshalNested{Label: "test", Inner: marshalPos{X: 3, Y: 4}})

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
	n, ok := flecs.Get[marshalNested](w2, found)
	if !ok || n.Label != "test" || n.Inner.X != 3 || n.Inner.Y != 4 {
		t.Fatalf("marshalNested: got %+v", n)
	}
}

func TestMarshalJSONValid(t *testing.T) {
	w := newMarshalWorld()
	e := w.NewEntity()
	flecs.Set(w, e, marshalPos{X: 1, Y: 2})
	flecs.Set(w, e, marshalVel{DX: 3, DY: 4})
	w.SetName(e, "entity1")

	data := mustMarshal(t, w)
	if !json.Valid(data) {
		t.Fatalf("json.Valid returned false: %s", data)
	}
}

func TestMarshalTwoStepRoundTrip(t *testing.T) {
	// Build world, marshal → unmarshal into w2, marshal w2 → compare.
	w := newMarshalWorld()
	e1 := w.NewEntity()
	flecs.Set(w, e1, marshalPos{X: 1, Y: 2})
	w.SetName(e1, "e1")
	e2 := w.NewEntity()
	flecs.Set(w, e2, marshalVel{DX: 3, DY: 4})

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
	root := w.NewEntity()
	w.SetName(root, "scene")
	car := w.NewEntity()
	w.SetName(car, "car")
	flecs.AddID(w, car, flecs.MakePair(w.ChildOf(), root))

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
	root := w.NewEntity()
	w.SetName(root, "root")
	parent := w.NewEntity()
	w.SetName(parent, "parent")
	flecs.AddID(w, parent, flecs.MakePair(w.ChildOf(), root))
	child := w.NewEntity()
	w.SetName(child, "child")
	flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))

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
	w := flecs.New()
	root := w.NewEntity()
	w.SetName(root, "root")
	const n = 5
	for i := range n {
		c := w.NewEntity()
		w.SetName(c, fmt.Sprintf("child%d", i))
		flecs.AddID(w, c, flecs.MakePair(w.ChildOf(), root))
	}

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
	root := w.NewEntity()
	w.SetName(root, "root")
	child := w.NewEntity()
	w.SetName(child, "child")
	flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), root))

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
	child := w.NewEntity()
	w.SetName(child, "child")
	parent := w.NewEntity()
	w.SetName(parent, "parent")
	flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))

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

func TestMarshalCycleDetection(t *testing.T) {
	w := flecs.New()
	a := w.NewEntity()
	b := w.NewEntity()
	// A and B are each other's ChildOf parent — a mutual cycle.
	flecs.AddID(w, a, flecs.MakePair(w.ChildOf(), b))
	flecs.AddID(w, b, flecs.MakePair(w.ChildOf(), a))

	_, err := w.MarshalJSON()
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error should mention 'cycle': %v", err)
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
	root := w.NewEntity()
	w.SetName(root, "root")
	anon := w.NewEntity()
	flecs.AddID(w, anon, flecs.MakePair(w.ChildOf(), root))

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
	root := w.NewEntity()
	w.SetName(root, "root")
	child := w.NewEntity()
	w.SetName(child, "child")
	flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), root))

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
	e := w.NewEntity()
	w.SetName(e, "solo")

	data := mustMarshal(t, w)
	if strings.Contains(string(data), `"parent"`) {
		t.Errorf("'parent' field should be absent for entities without a parent: %s", data)
	}
}
