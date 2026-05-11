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
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	w.SetName(e, "foo")
	got, ok := w.GetName(e)
	if !ok || got != "foo" {
		t.Fatalf("GetName want (foo, true), got (%q, %v)", got, ok)
	}
}

func TestGetNameDeadEntity(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	w.Delete(e)
	got, ok := w.GetName(e)
	if ok || got != "" {
		t.Fatalf("GetName on dead entity want (\"\", false), got (%q, %v)", got, ok)
	}
}

func TestGetNameUnnamedEntity(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	got, ok := w.GetName(e)
	if ok || got != "" {
		t.Fatalf("GetName on unnamed entity want (\"\", false), got (%q, %v)", got, ok)
	}
}

func TestGetNameEmptyValueTreatedAsUnnamed(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	w.SetName(e, "")
	got, ok := w.GetName(e)
	if ok || got != "" {
		t.Fatalf("GetName with empty Value want (\"\", false), got (%q, %v)", got, ok)
	}
}

func TestReSetName(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
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
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
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
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	if w.RemoveName(e) {
		t.Fatal("RemoveName on unnamed entity should return false")
	}
}

// --- Name is a regular component ---

func TestNameIsRegularComponent(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	w.SetName(e, "thing")
	w.Read(func(r *flecs.Reader) {
		if !flecs.Has[flecs.Name](r, e) {
			t.Fatal("Has[Name] should return true after SetName")
		}
	})
}

// --- Lookup single segment ---

func TestLookupSingleSegmentRootScope(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	w.SetName(e, "scene")
	found, ok := w.Lookup("scene")
	if !ok || found != e {
		t.Fatalf("Lookup(scene) want (%v, true), got (%v, %v)", e, found, ok)
	}
}

// --- Lookup nested ---

func TestLookupNested(t *testing.T) {
	w := flecs.New()

	var root, car, wheel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		root = fw.NewEntity()
		car = fw.NewEntity()
		flecs.AddID(fw, car, flecs.MakePair(w.ChildOf(), root))
		wheel = fw.NewEntity()
		flecs.AddID(fw, wheel, flecs.MakePair(w.ChildOf(), car))
	})
	w.SetName(root, "scene")
	w.SetName(car, "car")
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

	var root flecs.ID
	w.Write(func(fw *flecs.Writer) { root = fw.NewEntity() })
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
	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
	})
	w.SetName(child, "target")

	found, ok := w.LookupChild(parent, "target")
	if !ok || found != child {
		t.Fatalf("LookupChild want (%v, true), got (%v, %v)", child, found, ok)
	}
}

func TestLookupChildMiss(t *testing.T) {
	w := flecs.New()
	var parent flecs.ID
	w.Write(func(fw *flecs.Writer) { parent = fw.NewEntity() })
	found, ok := w.LookupChild(parent, "nope")
	if ok || found != 0 {
		t.Fatalf("LookupChild miss want (0, false), got (%v, %v)", found, ok)
	}
}

func TestLookupChildRootScope(t *testing.T) {
	w := flecs.New()
	var e, parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		parent = fw.NewEntity()
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
	})
	w.SetName(e, "rooted")
	w.SetName(child, "rooted") // same name, but has a parent

	// A child entity should NOT match root scope.
	found, ok := w.LookupChild(0, "rooted")
	if !ok || found != e {
		t.Fatalf("LookupChild(0, rooted) want (%v, true), got (%v, %v)", e, found, ok)
	}
}

func TestLookupChildSiblingCollision(t *testing.T) {
	w := flecs.New()
	var parent, c1, c2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		c1 = fw.NewEntity()
		c2 = fw.NewEntity()
		flecs.AddID(fw, c1, flecs.MakePair(w.ChildOf(), parent))
		flecs.AddID(fw, c2, flecs.MakePair(w.ChildOf(), parent))
	})
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
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	w.SetName(e, "scene")
	if got := w.PathOf(e); got != "scene" {
		t.Fatalf("PathOf named root want \"scene\", got %q", got)
	}
}

func TestPathOfNestedChain(t *testing.T) {
	w := flecs.New()
	var root, car, wheel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		root = fw.NewEntity()
		car = fw.NewEntity()
		flecs.AddID(fw, car, flecs.MakePair(w.ChildOf(), root))
		wheel = fw.NewEntity()
		flecs.AddID(fw, wheel, flecs.MakePair(w.ChildOf(), car))
	})
	w.SetName(root, "scene")
	w.SetName(car, "car")
	w.SetName(wheel, "wheel")

	if got := w.PathOf(wheel); got != "scene.car.wheel" {
		t.Fatalf("PathOf nested want \"scene.car.wheel\", got %q", got)
	}
}

func TestPathOfUnnamedEntity(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	if got := w.PathOf(e); got != "" {
		t.Fatalf("PathOf unnamed entity want \"\", got %q", got)
	}
}

func TestPathOfDeadEntity(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	w.SetName(e, "gone")
	w.Delete(e)
	if got := w.PathOf(e); got != "" {
		t.Fatalf("PathOf dead entity want \"\", got %q", got)
	}
}

func TestPathOfUnnamedParentTruncation(t *testing.T) {
	w := flecs.New()
	var root, mid, leaf flecs.ID
	w.Write(func(fw *flecs.Writer) {
		root = fw.NewEntity()
		mid = fw.NewEntity() // unnamed intermediate
		flecs.AddID(fw, mid, flecs.MakePair(w.ChildOf(), root))
		leaf = fw.NewEntity()
		flecs.AddID(fw, leaf, flecs.MakePair(w.ChildOf(), mid))
	})
	w.SetName(root, "scene")
	w.SetName(leaf, "wheel")

	// mid is unnamed, so PathOf(leaf) stops at mid and returns just "wheel".
	if got := w.PathOf(leaf); got != "wheel" {
		t.Fatalf("PathOf with unnamed parent want \"wheel\", got %q", got)
	}
}

// --- round-trip ---

func TestPathRoundTrip(t *testing.T) {
	w := flecs.New()
	var root, car, wheel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		root = fw.NewEntity()
		car = fw.NewEntity()
		flecs.AddID(fw, car, flecs.MakePair(w.ChildOf(), root))
		wheel = fw.NewEntity()
		flecs.AddID(fw, wheel, flecs.MakePair(w.ChildOf(), car))
	})
	w.SetName(root, "scene")
	w.SetName(car, "car")
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
	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})
	w.SetName(prefab, "proto")

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
	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
	})
	if w.Count() != base+2 {
		t.Fatalf("Count want base+2=%d, got %d", base+2, w.Count())
	}
	w.Delete(e1)
	if w.Count() != base+1 {
		t.Fatalf("Count want base+1=%d after delete, got %d", base+1, w.Count())
	}
	_ = e2
}
