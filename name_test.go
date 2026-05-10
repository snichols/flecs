package flecs_test

import (
	"testing"

	flecs "github.com/snichols/flecs"
)

// --- built-in allocation ---

func TestNameBuiltinAllocation(t *testing.T) {
	w := flecs.New()
	nameID := w.Name()
	if nameID == 0 {
		t.Fatal("Name() returned zero ID")
	}
	if !w.IsAlive(nameID) {
		t.Fatal("Name entity should be alive")
	}
	// Consistency across calls.
	if w.Name() != nameID {
		t.Fatal("Name() not consistent across calls")
	}
	// Idempotent: re-registering returns the same cached ID.
	if id2 := flecs.RegisterComponent[flecs.Name](w); id2 != nameID {
		t.Fatalf("RegisterComponent[Name] want %v got %v", nameID, id2)
	}
}

// --- SetName / GetName ---

func TestSetNameGetNameRoundTrip(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	w.SetName(e, "foo")
	got, ok := w.GetName(e)
	if !ok || got != "foo" {
		t.Fatalf("GetName want (foo, true), got (%q, %v)", got, ok)
	}
}

func TestGetNameDeadEntity(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	w.Delete(e)
	got, ok := w.GetName(e)
	if ok || got != "" {
		t.Fatalf("GetName on dead entity want (\"\", false), got (%q, %v)", got, ok)
	}
}

func TestGetNameUnnamedEntity(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	got, ok := w.GetName(e)
	if ok || got != "" {
		t.Fatalf("GetName on unnamed entity want (\"\", false), got (%q, %v)", got, ok)
	}
}

func TestGetNameEmptyValueTreatedAsUnnamed(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	w.SetName(e, "")
	got, ok := w.GetName(e)
	if ok || got != "" {
		t.Fatalf("GetName with empty Value want (\"\", false), got (%q, %v)", got, ok)
	}
}

func TestReSetName(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	w.SetName(e, "foo")
	w.SetName(e, "bar")
	got, ok := w.GetName(e)
	if !ok || got != "bar" {
		t.Fatalf("GetName after re-set want (bar, true), got (%q, %v)", got, ok)
	}
}

// --- RemoveName ---

func TestRemoveName(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	w.SetName(e, "foo")
	if !w.RemoveName(e) {
		t.Fatal("RemoveName should return true when entity had a Name")
	}
	got, ok := w.GetName(e)
	if ok || got != "" {
		t.Fatalf("GetName after RemoveName want (\"\", false), got (%q, %v)", got, ok)
	}
}

func TestRemoveNameOnUnnamed(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	if w.RemoveName(e) {
		t.Fatal("RemoveName on unnamed entity should return false")
	}
}

// --- Name is a regular component ---

func TestNameIsRegularComponent(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	w.SetName(e, "thing")
	if !flecs.Has[flecs.Name](w, e) {
		t.Fatal("Has[Name] should return true after SetName")
	}
}

// --- Lookup single segment ---

func TestLookupSingleSegmentRootScope(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	w.SetName(e, "scene")
	found, ok := w.Lookup("scene")
	if !ok || found != e {
		t.Fatalf("Lookup(scene) want (%v, true), got (%v, %v)", e, found, ok)
	}
}

// --- Lookup nested ---

func TestLookupNested(t *testing.T) {
	w := flecs.New()

	root := w.NewEntity()
	w.SetName(root, "scene")

	car := w.NewEntity()
	flecs.AddID(w, car, flecs.MakePair(w.ChildOf(), root))
	w.SetName(car, "car")

	wheel := w.NewEntity()
	flecs.AddID(w, wheel, flecs.MakePair(w.ChildOf(), car))
	w.SetName(wheel, "wheel")

	found, ok := w.Lookup("scene.car.wheel")
	if !ok || found != wheel {
		t.Fatalf("Lookup(scene.car.wheel) want (%v, true), got (%v, %v)", wheel, found, ok)
	}
	// Intermediate segments work too.
	found, ok = w.Lookup("scene.car")
	if !ok || found != car {
		t.Fatalf("Lookup(scene.car) want (%v, true), got (%v, %v)", car, found, ok)
	}
}

// --- Lookup missing ---

func TestLookupMissing(t *testing.T) {
	w := flecs.New()
	found, ok := w.Lookup("nonexistent")
	if ok || found != 0 {
		t.Fatalf("Lookup(nonexistent) want (0, false), got (%v, %v)", found, ok)
	}

	root := w.NewEntity()
	w.SetName(root, "root")
	found, ok = w.Lookup("root.missing")
	if ok || found != 0 {
		t.Fatalf("Lookup(root.missing) want (0, false), got (%v, %v)", found, ok)
	}
}

// --- Lookup malformed ---

func TestLookupMalformed(t *testing.T) {
	w := flecs.New()
	cases := []string{
		"",         // empty
		".foo",     // leading dot
		"foo.",     // trailing dot
		"foo..bar", // double dot
	}
	for _, c := range cases {
		found, ok := w.Lookup(c)
		if ok || found != 0 {
			t.Fatalf("Lookup(%q) want (0, false), got (%v, %v)", c, found, ok)
		}
	}
}

// --- LookupChild ---

func TestLookupChildBasics(t *testing.T) {
	w := flecs.New()
	parent := w.NewEntity()
	child := w.NewEntity()
	flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))
	w.SetName(child, "target")

	found, ok := w.LookupChild(parent, "target")
	if !ok || found != child {
		t.Fatalf("LookupChild want (%v, true), got (%v, %v)", child, found, ok)
	}
}

func TestLookupChildMiss(t *testing.T) {
	w := flecs.New()
	parent := w.NewEntity()
	found, ok := w.LookupChild(parent, "nope")
	if ok || found != 0 {
		t.Fatalf("LookupChild miss want (0, false), got (%v, %v)", found, ok)
	}
}

func TestLookupChildRootScope(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	w.SetName(e, "rooted")

	// A child entity should NOT match root scope.
	parent := w.NewEntity()
	child := w.NewEntity()
	flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))
	w.SetName(child, "rooted") // same name, but has a parent

	found, ok := w.LookupChild(0, "rooted")
	if !ok || found != e {
		t.Fatalf("LookupChild(0, rooted) want (%v, true), got (%v, %v)", e, found, ok)
	}
}

func TestLookupChildSiblingCollision(t *testing.T) {
	w := flecs.New()
	parent := w.NewEntity()
	c1 := w.NewEntity()
	c2 := w.NewEntity()
	flecs.AddID(w, c1, flecs.MakePair(w.ChildOf(), parent))
	flecs.AddID(w, c2, flecs.MakePair(w.ChildOf(), parent))
	w.SetName(c1, "twin")
	w.SetName(c2, "twin")

	// Both exist; LookupChild returns one of them (first in iteration order).
	found, ok := w.LookupChild(parent, "twin")
	if !ok {
		t.Fatal("LookupChild should find a match when sibling names collide")
	}
	if found != c1 && found != c2 {
		t.Fatalf("LookupChild returned unexpected entity %v", found)
	}
}

// --- PathOf ---

func TestPathOfNamedRoot(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	w.SetName(e, "scene")
	if got := w.PathOf(e); got != "scene" {
		t.Fatalf("PathOf named root want \"scene\", got %q", got)
	}
}

func TestPathOfNestedChain(t *testing.T) {
	w := flecs.New()
	root := w.NewEntity()
	w.SetName(root, "scene")
	car := w.NewEntity()
	flecs.AddID(w, car, flecs.MakePair(w.ChildOf(), root))
	w.SetName(car, "car")
	wheel := w.NewEntity()
	flecs.AddID(w, wheel, flecs.MakePair(w.ChildOf(), car))
	w.SetName(wheel, "wheel")

	if got := w.PathOf(wheel); got != "scene.car.wheel" {
		t.Fatalf("PathOf nested want \"scene.car.wheel\", got %q", got)
	}
}

func TestPathOfUnnamedEntity(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	if got := w.PathOf(e); got != "" {
		t.Fatalf("PathOf unnamed entity want \"\", got %q", got)
	}
}

func TestPathOfDeadEntity(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	w.SetName(e, "gone")
	w.Delete(e)
	if got := w.PathOf(e); got != "" {
		t.Fatalf("PathOf dead entity want \"\", got %q", got)
	}
}

func TestPathOfUnnamedParentTruncation(t *testing.T) {
	w := flecs.New()
	root := w.NewEntity()
	w.SetName(root, "scene")
	mid := w.NewEntity() // unnamed intermediate
	flecs.AddID(w, mid, flecs.MakePair(w.ChildOf(), root))
	leaf := w.NewEntity()
	flecs.AddID(w, leaf, flecs.MakePair(w.ChildOf(), mid))
	w.SetName(leaf, "wheel")

	// mid is unnamed, so PathOf(leaf) stops at mid and returns just "wheel".
	if got := w.PathOf(leaf); got != "wheel" {
		t.Fatalf("PathOf with unnamed parent want \"wheel\", got %q", got)
	}
}

// --- round-trip ---

func TestPathRoundTrip(t *testing.T) {
	w := flecs.New()
	root := w.NewEntity()
	w.SetName(root, "scene")
	car := w.NewEntity()
	flecs.AddID(w, car, flecs.MakePair(w.ChildOf(), root))
	w.SetName(car, "car")
	wheel := w.NewEntity()
	flecs.AddID(w, wheel, flecs.MakePair(w.ChildOf(), car))
	w.SetName(wheel, "wheel")

	path := w.PathOf(wheel)
	found, ok := w.Lookup(path)
	if !ok || found != wheel {
		t.Fatalf("round-trip Lookup(PathOf(wheel)) want (%v, true), got (%v, %v)", wheel, found, ok)
	}
}

// --- Name inheritance via IsA ---

func TestNameInheritedViaIsA(t *testing.T) {
	w := flecs.New()
	prefab := w.NewEntity()
	w.SetName(prefab, "proto")

	child := w.NewEntity()
	flecs.AddID(w, child, flecs.MakePair(w.IsA(), prefab))

	// child has no own Name; Get[Name] walks IsA and finds prefab's name.
	got, ok := w.GetName(child)
	if !ok || got != "proto" {
		t.Fatalf("GetName via IsA want (proto, true), got (%q, %v)", got, ok)
	}
}

// --- existing baselines stay green ---

func TestNameDoesNotBreakCountBaseline(t *testing.T) {
	w := flecs.New()
	base := w.Count()
	e1 := w.NewEntity()
	e2 := w.NewEntity()
	if w.Count() != base+2 {
		t.Fatalf("Count want base+2=%d, got %d", base+2, w.Count())
	}
	w.Delete(e1)
	if w.Count() != base+1 {
		t.Fatalf("Count want base+1=%d after delete, got %d", base+1, w.Count())
	}
	_ = e2
}
