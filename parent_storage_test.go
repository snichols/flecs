package flecs_test

import (
	"encoding/json"
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
)

// helpers ─────────────────────────────────────────────────────────────────────

// newPSWorld creates a world with a custom "Located" relationship that has
// parent storage enabled. Returns the world and the relationship ID.
func newPSWorld(t *testing.T) (*flecs.World, flecs.ID) {
	t.Helper()
	w := flecs.New()
	var relID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		relID = fw.NewEntity()
	})
	flecs.SetRelationship(w, relID)
	flecs.SetExclusive(w, relID)
	flecs.SetParentStorage(w, relID)
	return w, relID
}

// ── SetParentStorage / IsParentStorage ───────────────────────────────────────

func TestSetParentStorageRoundTrip(t *testing.T) {
	w, relID := newPSWorld(t)
	w.Read(func(r *flecs.Reader) {
		if !flecs.IsParentStorage(r, relID) {
			t.Fatal("IsParentStorage should be true after SetParentStorage")
		}
	})
}

func TestIsParentStorageFalseByDefault(t *testing.T) {
	w := flecs.New()
	var relID flecs.ID
	w.Write(func(fw *flecs.Writer) { relID = fw.NewEntity() })
	flecs.SetRelationship(w, relID)
	flecs.SetExclusive(w, relID)
	w.Read(func(r *flecs.Reader) {
		if flecs.IsParentStorage(r, relID) {
			t.Fatal("IsParentStorage should be false before SetParentStorage is called")
		}
	})
}

func TestSetParentStoragePanicIfNotRelationship(t *testing.T) {
	w := flecs.New()
	var relID flecs.ID
	w.Write(func(fw *flecs.Writer) { relID = fw.NewEntity() })
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when SetParentStorage is called without SetRelationship")
		}
	}()
	flecs.SetParentStorage(w, relID)
}

func TestSetParentStoragePanicIfNotExclusive(t *testing.T) {
	w := flecs.New()
	var relID flecs.ID
	w.Write(func(fw *flecs.Writer) { relID = fw.NewEntity() })
	flecs.SetRelationship(w, relID)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when SetParentStorage is called without SetExclusive")
		}
	}()
	flecs.SetParentStorage(w, relID)
}

func TestSetParentStoragePanicIfLiveEntities(t *testing.T) {
	w := flecs.New()
	var relID, target, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		relID = fw.NewEntity()
		target = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetRelationship(w, relID)
	flecs.SetExclusive(w, relID)
	// Add pair before enabling parent storage.
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, flecs.MakePair(relID, target))
	})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when SetParentStorage is called with live entities")
		}
	}()
	flecs.SetParentStorage(w, relID)
}

// ── Basic add / has / remove ─────────────────────────────────────────────────

func TestParentStorageAddHasRemove(t *testing.T) {
	w, relID := newPSWorld(t)
	var target, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		target = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(relID, target))
	})
	pair := flecs.MakePair(relID, target)
	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, e, pair) {
			t.Fatal("HasID should be true after adding parent-storage pair")
		}
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.RemoveID(fw, e, pair)
	})
	w.Read(func(r *flecs.Reader) {
		if flecs.HasID(r, e, pair) {
			t.Fatal("HasID should be false after removing parent-storage pair")
		}
	})
}

func TestParentStorageIdempotentAdd(t *testing.T) {
	w, relID := newPSWorld(t)
	var target, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		target = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(relID, target))
		flecs.AddID(fw, e, flecs.MakePair(relID, target)) // second add is a no-op
	})
	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, e, flecs.MakePair(relID, target)) {
			t.Fatal("pair should still be present after idempotent add")
		}
	})
}

// ── O(1) reparenting ─────────────────────────────────────────────────────────

func TestParentStorageReparentOK(t *testing.T) {
	w, relID := newPSWorld(t)
	var p1, p2, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		p2 = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(relID, p1))
	})
	// Reparent: change from p1 to p2.
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, flecs.MakePair(relID, p2))
	})
	w.Read(func(r *flecs.Reader) {
		if flecs.HasID(r, e, flecs.MakePair(relID, p1)) {
			t.Fatal("old pair should be gone after reparent")
		}
		if !flecs.HasID(r, e, flecs.MakePair(relID, p2)) {
			t.Fatal("new pair should be present after reparent")
		}
	})
}

// Entities with different parents but the same other components share ONE table.
func TestParentStorageNonFragmenting(t *testing.T) {
	type Tag struct{}
	w, relID := newPSWorld(t)
	tagID := flecs.RegisterComponent[Tag](w)
	var p1, p2, e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		p2 = fw.NewEntity()
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.AddID(fw, e1, tagID)
		flecs.AddID(fw, e1, flecs.MakePair(relID, p1))
		flecs.AddID(fw, e2, tagID)
		flecs.AddID(fw, e2, flecs.MakePair(relID, p2))
	})
	// Both entities should be in the same archetype table (same signature).
	comps1 := w.EntityComponents(e1)
	comps2 := w.EntityComponents(e2)
	if len(comps1) != len(comps2) {
		t.Fatalf("non-fragmenting: different signature lengths: %d vs %d", len(comps1), len(comps2))
	}
	for i := range comps1 {
		if comps1[i] != comps2[i] {
			t.Fatalf("non-fragmenting: component[%d] differs: %v vs %v", i, comps1[i], comps2[i])
		}
	}
}

// ── EachChild / ParentOf with parent storage ─────────────────────────────────

func TestParentStorageEachChildBasic(t *testing.T) {
	w := flecs.New()
	// Use ChildOf with parent storage.
	flecs.SetParentStorage(w, w.ChildOf())
	var parent, c1, c2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		c1 = fw.NewEntity()
		c2 = fw.NewEntity()
		flecs.AddID(fw, c1, flecs.MakePair(w.ChildOf(), parent))
		flecs.AddID(fw, c2, flecs.MakePair(w.ChildOf(), parent))
	})
	var children []flecs.ID
	w.EachChild(parent, func(child flecs.ID) bool {
		children = append(children, child)
		return true
	})
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}
}

func TestParentStorageParentOf(t *testing.T) {
	w := flecs.New()
	flecs.SetParentStorage(w, w.ChildOf())
	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
	})
	got, ok := w.ParentOf(child)
	if !ok {
		t.Fatal("ParentOf should return true")
	}
	if got != parent {
		t.Fatalf("ParentOf: want %v got %v", parent, got)
	}
}

func TestParentStorageParentOfAfterReparent(t *testing.T) {
	w := flecs.New()
	flecs.SetParentStorage(w, w.ChildOf())
	var p1, p2, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		p2 = fw.NewEntity()
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), p1))
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), p2))
	})
	got, ok := w.ParentOf(child)
	if !ok {
		t.Fatal("ParentOf should return true after reparent")
	}
	if got != p2 {
		t.Fatalf("ParentOf after reparent: want %v got %v", p2, got)
	}
}

// ── Traversal: GetUp / HasUp ──────────────────────────────────────────────────

func TestParentStorageGetUp(t *testing.T) {
	type Score struct{ V int }
	w := flecs.New()
	flecs.SetParentStorage(w, w.ChildOf())
	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		child = fw.NewEntity()
		flecs.Set(fw, parent, Score{V: 42})
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
	})
	w.Read(func(r *flecs.Reader) {
		val, ok := flecs.GetUp[Score](r, child, w.ChildOf())
		if !ok {
			t.Fatal("GetUp should find Score on parent")
		}
		if val.V != 42 {
			t.Fatalf("GetUp: want 42, got %d", val.V)
		}
	})
}

func TestParentStorageHasUp(t *testing.T) {
	type Marker struct{}
	w := flecs.New()
	flecs.SetParentStorage(w, w.ChildOf())
	markerID := flecs.RegisterComponent[Marker](w)
	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		child = fw.NewEntity()
		flecs.AddID(fw, parent, markerID)
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
	})
	w.Read(func(r *flecs.Reader) {
		if !flecs.HasUp(r, child, markerID, w.ChildOf()) {
			t.Fatal("HasUp should find Marker on parent")
		}
	})
}

// ── Observer: OnAdd / OnRemove ────────────────────────────────────────────────

func TestParentStorageObserverOnAdd(t *testing.T) {
	w, relID := newPSWorld(t)
	var target, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		target = fw.NewEntity()
		e = fw.NewEntity()
	})
	var addCount int
	flecs.ObserveID(w, flecs.MakePair(relID, target), flecs.EventOnAdd,
		func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) { addCount++ })
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, flecs.MakePair(relID, target))
	})
	if addCount != 1 {
		t.Fatalf("OnAdd observer: want 1 call, got %d", addCount)
	}
}

func TestParentStorageObserverOnRemove(t *testing.T) {
	w, relID := newPSWorld(t)
	var target, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		target = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(relID, target))
	})
	var removeCount int
	flecs.ObserveID(w, flecs.MakePair(relID, target), flecs.EventOnRemove,
		func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) { removeCount++ })
	w.Write(func(fw *flecs.Writer) {
		flecs.RemoveID(fw, e, flecs.MakePair(relID, target))
	})
	if removeCount != 1 {
		t.Fatalf("OnRemove observer: want 1 call, got %d", removeCount)
	}
}

func TestParentStorageObserverOnReparent(t *testing.T) {
	w, relID := newPSWorld(t)
	var p1, p2, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		p2 = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(relID, p1))
	})
	var adds, removes int
	flecs.ObserveID(w, flecs.MakePair(relID, p1), flecs.EventOnRemove,
		func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) { removes++ })
	flecs.ObserveID(w, flecs.MakePair(relID, p2), flecs.EventOnAdd,
		func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) { adds++ })
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, flecs.MakePair(relID, p2)) // reparent
	})
	if removes != 1 {
		t.Fatalf("OnRemove for old parent: want 1, got %d", removes)
	}
	if adds != 1 {
		t.Fatalf("OnAdd for new parent: want 1, got %d", adds)
	}
}

// ── Cleanup: OnDeleteTarget ───────────────────────────────────────────────────

func TestParentStorageOnDeleteTargetDeleteCascade(t *testing.T) {
	w := flecs.New()
	// Use ChildOf with parent storage; ChildOf already has OnDeleteTarget=Delete.
	flecs.SetParentStorage(w, w.ChildOf())
	var parent, c1, c2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		c1 = fw.NewEntity()
		c2 = fw.NewEntity()
		flecs.AddID(fw, c1, flecs.MakePair(w.ChildOf(), parent))
		flecs.AddID(fw, c2, flecs.MakePair(w.ChildOf(), parent))
	})
	w.Delete(parent)
	if w.IsAlive(parent) || w.IsAlive(c1) || w.IsAlive(c2) {
		t.Fatal("all entities should be deleted when parent is deleted")
	}
}

func TestParentStorageOnDeleteTargetDeleteCustomRel(t *testing.T) {
	w, relID := newPSWorld(t)
	flecs.SetCleanupPolicy(w, relID, w.OnDeleteTarget(), w.DeleteAction())
	var target, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		target = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(relID, target))
	})
	w.Delete(target)
	if w.IsAlive(e) {
		t.Fatal("source entity should be deleted when target is deleted with OnDeleteTarget=Delete")
	}
}

func TestParentStorageOnDeleteTargetOnlyAffectsMatchingParent(t *testing.T) {
	w := flecs.New()
	flecs.SetParentStorage(w, w.ChildOf())
	var p1, p2, c1, c2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		p2 = fw.NewEntity()
		c1 = fw.NewEntity()
		c2 = fw.NewEntity()
		flecs.AddID(fw, c1, flecs.MakePair(w.ChildOf(), p1))
		flecs.AddID(fw, c2, flecs.MakePair(w.ChildOf(), p2))
	})
	w.Delete(p1)
	if w.IsAlive(c1) {
		t.Fatal("c1 should be deleted when p1 is deleted")
	}
	if !w.IsAlive(c2) {
		t.Fatal("c2 should still be alive (different parent p2)")
	}
}

// ── Query: specific target ────────────────────────────────────────────────────

func TestParentStorageQuerySpecificTarget(t *testing.T) {
	w, relID := newPSWorld(t)
	var p1, p2, e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		p2 = fw.NewEntity()
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.AddID(fw, e1, flecs.MakePair(relID, p1))
		flecs.AddID(fw, e2, flecs.MakePair(relID, p2))
	})
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(relID, p1)))
	var matched []flecs.ID
	it := q.Iter()
	for it.Next() {
		matched = append(matched, it.Entities()...)
	}
	if len(matched) != 1 || matched[0] != e1 {
		t.Fatalf("query(rel, p1): want [e1], got %v", matched)
	}
	_ = e2
}

func TestParentStorageQueryWildcard(t *testing.T) {
	w, relID := newPSWorld(t)
	var p1, p2, e1, e2, e3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		p2 = fw.NewEntity()
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		e3 = fw.NewEntity()
		flecs.AddID(fw, e1, flecs.MakePair(relID, p1))
		flecs.AddID(fw, e2, flecs.MakePair(relID, p2))
		// e3 has no parent
	})
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(relID, w.Wildcard())))
	var matched []flecs.ID
	it := q.Iter()
	for it.Next() {
		matched = append(matched, it.Entities()...)
	}
	if len(matched) != 2 {
		t.Fatalf("wildcard query: want 2 results, got %d: %v", len(matched), matched)
	}
	_ = e3
}

func TestParentStorageQueryNot(t *testing.T) {
	type Tag struct{}
	w, relID := newPSWorld(t)
	tagID := flecs.RegisterComponent[Tag](w)
	var p1, e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.AddID(fw, e1, tagID)
		flecs.AddID(fw, e1, flecs.MakePair(relID, p1))
		flecs.AddID(fw, e2, tagID)
		// e2 has no parent
	})
	// Query: has Tag but NOT (rel, p1).
	q := flecs.NewQueryFromTerms(w,
		flecs.With(tagID),
		flecs.Without(flecs.MakePair(relID, p1)),
	)
	var matched []flecs.ID
	it := q.Iter()
	for it.Next() {
		for _, id := range it.Entities() {
			if id == e1 || id == e2 {
				matched = append(matched, id)
			}
		}
	}
	// e1 has (rel, p1) → must NOT appear; e2 has no pair → should appear
	for _, m := range matched {
		if m == e1 {
			t.Fatal("e1 should not match Without(rel, p1)")
		}
	}
	found := false
	for _, m := range matched {
		if m == e2 {
			found = true
		}
	}
	if !found {
		t.Fatal("e2 should match (it has Tag and no parent-storage pair)")
	}
}

// ── Query: tgtVar ─────────────────────────────────────────────────────────────

func TestParentStorageQueryTgtVar(t *testing.T) {
	w, relID := newPSWorld(t)
	var p1, p2, e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		p2 = fw.NewEntity()
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.AddID(fw, e1, flecs.MakePair(relID, p1))
		flecs.AddID(fw, e2, flecs.MakePair(relID, p2))
	})
	q := flecs.NewQueryFromTerms(w, flecs.WithPairTgtVar(relID, "P"))
	seen := map[flecs.ID]flecs.ID{} // entity → parent binding
	it := q.Iter()
	for it.Next() {
		pVar := it.Var("P")
		for _, e := range it.Entities() {
			seen[e] = pVar
		}
	}
	if seen[e1] != p1 {
		t.Fatalf("tgtVar: e1 parent want %v got %v", p1, seen[e1])
	}
	if seen[e2] != p2 {
		t.Fatalf("tgtVar: e2 parent want %v got %v", p2, seen[e2])
	}
}

// ── CachedQuery ───────────────────────────────────────────────────────────────

func TestParentStorageCachedQuerySpecificTarget(t *testing.T) {
	w, relID := newPSWorld(t)
	var p1, p2, e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		p2 = fw.NewEntity()
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.AddID(fw, e1, flecs.MakePair(relID, p1))
		flecs.AddID(fw, e2, flecs.MakePair(relID, p2))
	})
	cq := flecs.NewCachedQueryFromTerms(w, flecs.With(flecs.MakePair(relID, p1)))
	defer cq.Close()
	var matched []flecs.ID
	it := cq.Iter()
	for it.Next() {
		matched = append(matched, it.Entities()...)
	}
	if len(matched) != 1 || matched[0] != e1 {
		t.Fatalf("cached query(rel, p1): want [e1], got %v", matched)
	}
	_ = e2
}

// ── Column transfer on migrate ────────────────────────────────────────────────

func TestParentStorageColumnTransferOnMigrate(t *testing.T) {
	type Tag struct{}
	w, relID := newPSWorld(t)
	tagID := flecs.RegisterComponent[Tag](w)
	var target, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		target = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(relID, target))
	})
	// Add a new component → triggers archetype migration for other reasons.
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, tagID)
	})
	// The parent should still be correct after the migration.
	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, e, flecs.MakePair(relID, target)) {
			t.Fatal("parent-storage pair should be preserved after archetype migration")
		}
	})
}

// ── Snapshot round-trip ───────────────────────────────────────────────────────

func TestParentStorageSnapshotRoundTrip(t *testing.T) {
	w, relID := newPSWorld(t)
	var target, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		target = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(relID, target))
	})
	snap := flecs.TakeSnapshot(w)
	flecs.RestoreSnapshot(w, snap)
	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, e, flecs.MakePair(relID, target)) {
			t.Fatal("pair should be present after snapshot round-trip")
		}
		if !flecs.IsParentStorage(r, relID) {
			t.Fatal("parentStoragePolicies should be restored by snapshot")
		}
	})
}

func TestParentStorageSnapshotRoundTripBytes(t *testing.T) {
	w, relID := newPSWorld(t)
	var target, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		target = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(relID, target))
	})
	snap := flecs.TakeSnapshot(w)
	b := snap.Bytes()
	snap2, err := flecs.LoadSnapshot(b)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	flecs.RestoreSnapshot(w, snap2)
	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, e, flecs.MakePair(relID, target)) {
			t.Fatal("pair should be present after bytes round-trip")
		}
	})
}

// ── JSON marshal/unmarshal round-trip ─────────────────────────────────────────

func TestParentStorageMarshalJSONRoundTrip(t *testing.T) {
	w, relID := newPSWorld(t)
	var target, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		target = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(relID, target))
	})
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	w2 := flecs.New()
	if err := json.Unmarshal(data, w2); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	// After unmarshal the serials are re-mapped; verify the world is non-empty.
	count := 0
	w2.Read(func(r *flecs.Reader) {
		r.EachEntity(func(id flecs.ID) bool {
			count++
			return true
		})
	})
	if count == 0 {
		t.Fatal("unmarshal produced no entities")
	}
	_ = relID
	_ = e
	_ = target
}

// ── ChildOf parent storage: end-to-end ───────────────────────────────────────

func TestParentStorageChildOfEndToEnd(t *testing.T) {
	w := flecs.New()
	flecs.SetParentStorage(w, w.ChildOf())
	var root, mid, leaf flecs.ID
	w.Write(func(fw *flecs.Writer) {
		root = fw.NewEntity()
		mid = fw.NewEntity()
		leaf = fw.NewEntity()
		flecs.AddID(fw, mid, flecs.MakePair(w.ChildOf(), root))
		flecs.AddID(fw, leaf, flecs.MakePair(w.ChildOf(), mid))
	})
	// ParentOf chain.
	p, ok := w.ParentOf(leaf)
	if !ok || p != mid {
		t.Fatalf("ParentOf(leaf) want mid, got %v ok=%v", p, ok)
	}
	p, ok = w.ParentOf(mid)
	if !ok || p != root {
		t.Fatalf("ParentOf(mid) want root, got %v ok=%v", p, ok)
	}
	// Delete root: cascade should delete mid and leaf.
	w.Delete(root)
	if w.IsAlive(root) || w.IsAlive(mid) || w.IsAlive(leaf) {
		t.Fatal("cascade delete failed for parent-storage ChildOf chain")
	}
}

func TestParentStorageChildOfReparentPreservesOtherChildren(t *testing.T) {
	w := flecs.New()
	flecs.SetParentStorage(w, w.ChildOf())
	var p1, p2, c1, c2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		p2 = fw.NewEntity()
		c1 = fw.NewEntity()
		c2 = fw.NewEntity()
		flecs.AddID(fw, c1, flecs.MakePair(w.ChildOf(), p1))
		flecs.AddID(fw, c2, flecs.MakePair(w.ChildOf(), p1))
	})
	// Reparent c1 to p2; c2 stays under p1.
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, c1, flecs.MakePair(w.ChildOf(), p2))
	})
	var p1Children, p2Children []flecs.ID
	w.EachChild(p1, func(c flecs.ID) bool { p1Children = append(p1Children, c); return true })
	w.EachChild(p2, func(c flecs.ID) bool { p2Children = append(p2Children, c); return true })
	if len(p1Children) != 1 || p1Children[0] != c2 {
		t.Fatalf("p1 children after reparent: want [c2], got %v", p1Children)
	}
	if len(p2Children) != 1 || p2Children[0] != c1 {
		t.Fatalf("p2 children after reparent: want [c1], got %v", p2Children)
	}
}
