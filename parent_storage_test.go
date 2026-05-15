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

func TestParentStorage_SetGet(t *testing.T) {
	w, relID := newPSWorld(t)
	w.Read(func(r *flecs.Reader) {
		if !flecs.IsParentStorage(r, relID) {
			t.Fatal("IsParentStorage should be true after SetParentStorage")
		}
	})
}

func TestParentStorage_DefaultDisabled(t *testing.T) {
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

func TestParentStorage_OnlyAcceptsRelationships(t *testing.T) {
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

func TestParentStorage_IsExclusiveRequired(t *testing.T) {
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

// ── Storage shape ─────────────────────────────────────────────────────────────

func TestParentStorage_SingleTable_MultipleParents(t *testing.T) {
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

func TestParentStorage_ParentColumnPopulated(t *testing.T) {
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
	// Verify parent column populated correctly via pair presence and query target binding.
	q := flecs.NewQueryFromTerms(w, flecs.WithPairTgtVar(relID, "P"))
	seen := map[flecs.ID]flecs.ID{}
	it := q.Iter()
	for it.Next() {
		pVar := it.Var("P")
		for _, e := range it.Entities() {
			seen[e] = pVar
		}
	}
	if seen[e1] != p1 {
		t.Fatalf("parent column: e1 should have p1, got %v", seen[e1])
	}
	if seen[e2] != p2 {
		t.Fatalf("parent column: e2 should have p2, got %v", seen[e2])
	}
}

func TestParentStorage_NoFragmentationOnRepartent(t *testing.T) {
	w, relID := newPSWorld(t)
	var tableCreations int
	flecs.OnTableCreate(w, func(_ *flecs.Writer, _ *flecs.Table) {
		tableCreations++
	})
	var p1, p2, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		p2 = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(relID, p1))
	})
	before := tableCreations
	// Reparent: should be an in-place column write, no new table.
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, flecs.MakePair(relID, p2))
	})
	if tableCreations > before {
		t.Fatalf("reparent created %d new table(s); parent storage should be O(1) with no migration", tableCreations-before)
	}
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

// ── Query: specific target / wildcard / variable ──────────────────────────────

func TestParentStorage_PairExactQuery(t *testing.T) {
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

func TestParentStorage_PairWildcardQuery(t *testing.T) {
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

func TestParentStorage_PairVariableQuery(t *testing.T) {
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
	seen := map[flecs.ID]flecs.ID{}
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
	})
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

// ── Traversal: GetUp / HasUp / TargetUp / Up query ────────────────────────────

func TestParentStorage_GetUp(t *testing.T) {
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

func TestParentStorage_HasUp_TargetUp_BackCompat(t *testing.T) {
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
		owner, ok := flecs.TargetUp(r, child, markerID, w.ChildOf())
		if !ok {
			t.Fatal("TargetUp should find entity with Marker")
		}
		if owner != parent {
			t.Fatalf("TargetUp: want parent %v, got %v", parent, owner)
		}
	})
}

func TestParentStorage_TraversalUp(t *testing.T) {
	// Multi-hop traversal: leaf → mid → root, root owns Score.
	// GetUp must walk parent columns across two hops.
	type Score struct{ V int }
	w := flecs.New()
	flecs.SetParentStorage(w, w.ChildOf())
	var root, mid, leaf flecs.ID
	w.Write(func(fw *flecs.Writer) {
		root = fw.NewEntity()
		mid = fw.NewEntity()
		leaf = fw.NewEntity()
		flecs.Set(fw, root, Score{V: 99})
		flecs.AddID(fw, mid, flecs.MakePair(w.ChildOf(), root))
		flecs.AddID(fw, leaf, flecs.MakePair(w.ChildOf(), mid))
	})
	w.Read(func(r *flecs.Reader) {
		val, ok := flecs.GetUp[Score](r, leaf, w.ChildOf())
		if !ok {
			t.Fatal("TraversalUp: GetUp from leaf should traverse 2 hops to find Score on root")
		}
		if val.V != 99 {
			t.Fatalf("TraversalUp: want Score.V=99, got %d", val.V)
		}
		// Mid also reaches root via 1 hop.
		val2, ok2 := flecs.GetUp[Score](r, mid, w.ChildOf())
		if !ok2 {
			t.Fatal("TraversalUp: GetUp from mid should find Score on root")
		}
		if val2.V != 99 {
			t.Fatalf("TraversalUp: mid hop: want 99, got %d", val2.V)
		}
	})
}

func TestParentStorage_Cascade(t *testing.T) {
	type Tag struct{}
	w := flecs.New()
	flecs.SetParentStorage(w, w.ChildOf())
	tagID := flecs.RegisterComponent[Tag](w)
	var root, mid, leaf flecs.ID
	w.Write(func(fw *flecs.Writer) {
		root = fw.NewEntity()
		flecs.AddID(fw, root, tagID)
		mid = fw.NewEntity()
		flecs.AddID(fw, mid, tagID)
		flecs.AddID(fw, mid, flecs.MakePair(w.ChildOf(), root))
		leaf = fw.NewEntity()
		flecs.AddID(fw, leaf, tagID)
		flecs.AddID(fw, leaf, flecs.MakePair(w.ChildOf(), mid))
	})
	cq := flecs.NewCachedQueryFromTerms(w, flecs.With(tagID).Cascade(w.ChildOf()))
	defer cq.Close()
	var order []flecs.ID
	cq.Each(func(it *flecs.QueryIter) {
		order = append(order, it.Entities()...)
	})
	// All three entities must appear.
	rootIdx, midIdx, leafIdx := -1, -1, -1
	for i, id := range order {
		switch id {
		case root:
			rootIdx = i
		case mid:
			midIdx = i
		case leaf:
			leafIdx = i
		}
	}
	if rootIdx < 0 || midIdx < 0 || leafIdx < 0 {
		t.Fatalf("cascade: not all entities returned; root=%d mid=%d leaf=%d from %v", rootIdx, midIdx, leafIdx, order)
	}
	// root (depth 0) must appear before any of its descendants.
	if rootIdx > midIdx || rootIdx > leafIdx {
		t.Fatalf("cascade: root must precede descendants; root=%d mid=%d leaf=%d", rootIdx, midIdx, leafIdx)
	}
}

// ── Back-compat: EachChild / ParentOf / PathOf / Lookup ──────────────────────

func TestParentStorage_EachChild_BackCompat(t *testing.T) {
	w := flecs.New()
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

func TestParentStorage_ParentOf_BackCompat(t *testing.T) {
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

func TestParentStorage_PathOf_BackCompat(t *testing.T) {
	w := flecs.New()
	flecs.SetParentStorage(w, w.ChildOf())
	var root, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		root = fw.NewEntity()
		child = fw.NewEntity()
	})
	w.SetName(root, "root")
	w.SetName(child, "child")
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), root))
	})
	path := w.PathOf(child)
	if path != "root.child" {
		t.Fatalf("PathOf with parent storage: want 'root.child', got %q", path)
	}
}

func TestParentStorage_Lookup_BackCompat(t *testing.T) {
	w := flecs.New()
	flecs.SetParentStorage(w, w.ChildOf())
	var root, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		root = fw.NewEntity()
		child = fw.NewEntity()
	})
	w.SetName(root, "pslookuproot")
	w.SetName(child, "pslookupchild")
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), root))
	})
	got, ok := w.Lookup("pslookuproot.pslookupchild")
	if !ok {
		t.Fatal("Lookup should find child by path with parent storage enabled")
	}
	if got != child {
		t.Fatalf("Lookup: want %v, got %v", child, got)
	}
}

// ── Cleanup: OnDeleteTarget ───────────────────────────────────────────────────

func TestParentStorage_OnDeleteTargetDelete(t *testing.T) {
	w := flecs.New()
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
		t.Fatal("all entities should be deleted when parent is deleted (OnDeleteTarget=Delete)")
	}
}

func TestParentStorage_OnDeleteTargetRemove(t *testing.T) {
	w, relID := newPSWorld(t)
	flecs.SetCleanupPolicy(w, relID, w.OnDeleteTarget(), w.RemoveAction())
	var target, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		target = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(relID, target))
	})
	w.Delete(target)
	if !w.IsAlive(e) {
		t.Fatal("source entity should still be alive after OnDeleteTarget=Remove")
	}
	w.Read(func(r *flecs.Reader) {
		if flecs.HasID(r, e, flecs.MakePair(relID, target)) {
			t.Fatal("pair should be removed after target deleted with RemoveAction")
		}
	})
}

func TestParentStorage_OnDeleteTargetPanic(t *testing.T) {
	w, relID := newPSWorld(t)
	flecs.SetCleanupPolicy(w, relID, w.OnDeleteTarget(), w.PanicAction())
	var target, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		target = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(relID, target))
	})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when deleting target with PanicAction policy")
		}
	}()
	w.Delete(target)
	_ = e
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

// ── Snapshot round-trip ───────────────────────────────────────────────────────

func TestParentStorage_Snapshot_RoundTrip(t *testing.T) {
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

func TestParentStorage_JSON_RoundTrip(t *testing.T) {
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

// ── Observers ────────────────────────────────────────────────────────────────

func TestParentStorage_OnAddFires(t *testing.T) {
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

func TestParentStorage_OnRemoveFires(t *testing.T) {
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

func TestParentStorage_OnReplaceFires(t *testing.T) {
	type Score struct{ V int }
	w, relID := newPSWorld(t)
	var fireCount, capturedOld, capturedNew int
	flecs.OnReplace[Score](w, func(_ *flecs.Writer, _ flecs.ID, old, newVal Score) {
		fireCount++
		capturedOld = old.V
		capturedNew = newVal.V
	})
	var target, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		target = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(relID, target))
		flecs.Set(fw, e, Score{V: 10}) // first set — no OnReplace
	})
	if fireCount != 0 {
		t.Fatalf("OnReplace must not fire on first Set; got %d", fireCount)
	}
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, Score{V: 20}) // overwrite — fires OnReplace
	})
	if fireCount != 1 {
		t.Fatalf("OnReplace should fire once on overwrite with parent storage; got %d", fireCount)
	}
	if capturedOld != 10 {
		t.Fatalf("OnReplace old value: want 10, got %d", capturedOld)
	}
	if capturedNew != 20 {
		t.Fatalf("OnReplace new value: want 20, got %d", capturedNew)
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
		flecs.AddID(fw, e, flecs.MakePair(relID, p2))
	})
	if removes != 1 {
		t.Fatalf("OnRemove for old parent: want 1, got %d", removes)
	}
	if adds != 1 {
		t.Fatalf("OnAdd for new parent: want 1, got %d", adds)
	}
}

func TestParentStorage_OnTableCreate_Once(t *testing.T) {
	w, relID := newPSWorld(t)
	var tableCreations int
	flecs.OnTableCreate(w, func(_ *flecs.Writer, _ *flecs.Table) {
		tableCreations++
	})
	before := tableCreations
	var p1, p2, e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		p2 = fw.NewEntity()
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.AddID(fw, e1, flecs.MakePair(relID, p1))
		flecs.AddID(fw, e2, flecs.MakePair(relID, p2))
	})
	// With parent storage, both children share ONE table regardless of parent.
	if got := tableCreations - before; got != 1 {
		t.Fatalf("OnTableCreate_Once: want 1 new table for two children with different parents, got %d", got)
	}
}

func TestParentStorage_OnTableFill_OnFirstChild(t *testing.T) {
	w, relID := newPSWorld(t)
	var p1, e1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		e1 = fw.NewEntity()
	})
	var fillCount int
	flecs.OnTableFill(w, func(_ *flecs.Writer, _ *flecs.Table) {
		fillCount++
	})
	before := fillCount
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e1, flecs.MakePair(relID, p1))
	})
	afterFirst := fillCount
	if afterFirst-before != 1 {
		t.Fatalf("OnTableFill should fire once on first child added; got %d fills", afterFirst-before)
	}
	var e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e2 = fw.NewEntity()
		flecs.AddID(fw, e2, flecs.MakePair(relID, p1))
	})
	if fillCount != afterFirst {
		t.Fatalf("OnTableFill must not fire again on second child; got %d extra fills", fillCount-afterFirst)
	}
}

func TestParentStorage_OnTableEmpty_OnLastChild(t *testing.T) {
	w, relID := newPSWorld(t)
	var p1, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(relID, p1))
	})
	var emptyCount int
	flecs.OnTableEmpty(w, func(_ *flecs.Writer, _ *flecs.Table) {
		emptyCount++
	})
	before := emptyCount
	w.Write(func(fw *flecs.Writer) {
		flecs.RemoveID(fw, e, flecs.MakePair(relID, p1))
	})
	if got := emptyCount - before; got != 1 {
		t.Fatalf("OnTableEmpty should fire once when last child removed; got %d", got)
	}
}

// ── Reclamation ───────────────────────────────────────────────────────────────

func TestParentStorage_TableReclaimedAfterAllChildrenGone(t *testing.T) {
	w, relID := newPSWorld(t)
	w.SetTableReclamationThreshold(5)
	var p1, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(relID, p1))
	})
	// Remove the child, making the parent-storage table empty.
	w.Write(func(fw *flecs.Writer) {
		flecs.RemoveID(fw, e, flecs.MakePair(relID, p1))
	})
	before := w.ReclaimedTablesCount()
	// Drive past the threshold.
	for range 10 {
		w.Progress(0)
	}
	if w.ReclaimedTablesCount() <= before {
		t.Fatal("parent-storage table should be reclaimed after all children are gone and threshold ticks exceeded")
	}
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
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, tagID)
	})
	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, e, flecs.MakePair(relID, target)) {
			t.Fatal("parent-storage pair should be preserved after archetype migration")
		}
	})
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
	p, ok := w.ParentOf(leaf)
	if !ok || p != mid {
		t.Fatalf("ParentOf(leaf) want mid, got %v ok=%v", p, ok)
	}
	p, ok = w.ParentOf(mid)
	if !ok || p != root {
		t.Fatalf("ParentOf(mid) want root, got %v ok=%v", p, ok)
	}
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

// ── Benchmark: reparent with 8 components — ON vs OFF ────────────────────────

func BenchmarkParentStorage_Reparent_FullArchetype(b *testing.B) {
	type C1 struct{ V [8]int64 }
	type C2 struct{ V [8]int64 }
	type C3 struct{ V [8]int64 }
	type C4 struct{ V [8]int64 }
	type C5 struct{ V [8]int64 }
	type C6 struct{ V [8]int64 }
	type C7 struct{ V [8]int64 }
	type C8 struct{ V [8]int64 }

	b.Run("ON", func(b *testing.B) {
		w := flecs.New()
		flecs.SetParentStorage(w, w.ChildOf())
		c1 := flecs.RegisterComponent[C1](w)
		c2 := flecs.RegisterComponent[C2](w)
		c3 := flecs.RegisterComponent[C3](w)
		c4 := flecs.RegisterComponent[C4](w)
		c5 := flecs.RegisterComponent[C5](w)
		c6 := flecs.RegisterComponent[C6](w)
		c7 := flecs.RegisterComponent[C7](w)
		c8 := flecs.RegisterComponent[C8](w)
		var p1, p2, e flecs.ID
		w.Write(func(fw *flecs.Writer) {
			p1 = fw.NewEntity()
			p2 = fw.NewEntity()
			e = fw.NewEntity()
			flecs.AddID(fw, e, c1)
			flecs.AddID(fw, e, c2)
			flecs.AddID(fw, e, c3)
			flecs.AddID(fw, e, c4)
			flecs.AddID(fw, e, c5)
			flecs.AddID(fw, e, c6)
			flecs.AddID(fw, e, c7)
			flecs.AddID(fw, e, c8)
			flecs.AddID(fw, e, flecs.MakePair(w.ChildOf(), p1))
		})
		parents := [2]flecs.ID{p1, p2}
		b.ResetTimer()
		for i := range b.N {
			target := parents[i&1]
			w.Write(func(fw *flecs.Writer) {
				flecs.AddID(fw, e, flecs.MakePair(w.ChildOf(), target))
			})
		}
	})

	b.Run("OFF", func(b *testing.B) {
		w := flecs.New()
		c1 := flecs.RegisterComponent[C1](w)
		c2 := flecs.RegisterComponent[C2](w)
		c3 := flecs.RegisterComponent[C3](w)
		c4 := flecs.RegisterComponent[C4](w)
		c5 := flecs.RegisterComponent[C5](w)
		c6 := flecs.RegisterComponent[C6](w)
		c7 := flecs.RegisterComponent[C7](w)
		c8 := flecs.RegisterComponent[C8](w)
		var p1, p2, e flecs.ID
		w.Write(func(fw *flecs.Writer) {
			p1 = fw.NewEntity()
			p2 = fw.NewEntity()
			e = fw.NewEntity()
			flecs.AddID(fw, e, c1)
			flecs.AddID(fw, e, c2)
			flecs.AddID(fw, e, c3)
			flecs.AddID(fw, e, c4)
			flecs.AddID(fw, e, c5)
			flecs.AddID(fw, e, c6)
			flecs.AddID(fw, e, c7)
			flecs.AddID(fw, e, c8)
			flecs.AddID(fw, e, flecs.MakePair(w.ChildOf(), p1))
		})
		parents := [2]flecs.ID{p1, p2}
		b.ResetTimer()
		for i := range b.N {
			target := parents[i&1]
			w.Write(func(fw *flecs.Writer) {
				flecs.AddID(fw, e, flecs.MakePair(w.ChildOf(), target))
			})
		}
	})
}
