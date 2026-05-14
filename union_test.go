package flecs_test

import (
	"encoding/json"
	"strings"
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
)

// newUnionWorld returns a fresh world with R (union relationship), T1 and T2 (targets).
func newUnionWorld(t *testing.T) (w *flecs.World, R, T1, T2 flecs.ID) {
	t.Helper()
	w = flecs.New()
	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		T1 = fw.NewEntity()
		T2 = fw.NewEntity()
	})
	flecs.SetUnion(w, R)
	return
}

// --- Test 1: No archetype transition on union pair add (port of Union.cpp:14-17) ---

func TestUnion_NoArchetypeTransition(t *testing.T) {
	w, R, T1, T2 := newUnionWorld(t)
	var e flecs.ID

	tablesBefore := len(w.TablesFor(flecs.MakePair(R, T1)))

	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(R, T1))
	})
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, flecs.MakePair(R, T2))
	})

	// Union pairs must never appear in any archetype table.
	if len(w.TablesFor(flecs.MakePair(R, T1))) != tablesBefore {
		t.Error("union (R,T1) unexpectedly created an archetype table")
	}
	if len(w.TablesFor(flecs.MakePair(R, T2))) != tablesBefore {
		t.Error("union (R,T2) unexpectedly created an archetype table")
	}

	// Only T2 must be active after replace.
	w.Read(func(r *flecs.Reader) {
		if r.HasID(e, flecs.MakePair(R, T1)) {
			t.Error("expected (R,T1) to be gone after T2 replaced it")
		}
		if !r.HasID(e, flecs.MakePair(R, T2)) {
			t.Error("expected (R,T2) to be active after replace")
		}
	})
}

// --- Test 2: Adding second target replaces first ---

func TestUnion_ReplaceTarget(t *testing.T) {
	w, R, T1, T2 := newUnionWorld(t)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(R, T1))
		fw.AddID(e, flecs.MakePair(R, T2))
	})
	_ = e

	var seen []flecs.ID
	w.Read(func(r *flecs.Reader) {
		flecs.EachUnion(r, R, func(_ flecs.ID, target flecs.ID) {
			seen = append(seen, target)
		})
	})
	if len(seen) != 1 || seen[0].Index() != T2.Index() {
		t.Errorf("expected only T2 in union store, got %d entries", len(seen))
	}
}

// --- Test 3: HasID(T1) false after T2 replaces ---

func TestUnion_HasID_AfterReplace(t *testing.T) {
	w, R, T1, T2 := newUnionWorld(t)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(R, T1))
	})
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, flecs.MakePair(R, T2))
	})

	w.Read(func(r *flecs.Reader) {
		if r.HasID(e, flecs.MakePair(R, T1)) {
			t.Error("HasID(R,T1) should be false after T2 replaced it")
		}
		if !r.HasID(e, flecs.MakePair(R, T2)) {
			t.Error("HasID(R,T2) should be true after replace")
		}
	})
}

// --- Test 4: HasID(Wildcard) true while any target is held ---

func TestUnion_HasID_Wildcard(t *testing.T) {
	w, R, T1, _ := newUnionWorld(t)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})

	w.Read(func(r *flecs.Reader) {
		if r.HasID(e, flecs.MakePair(R, w.Wildcard())) {
			t.Error("HasID(R,Wildcard) should be false before any target is added")
		}
	})

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, flecs.MakePair(R, T1))
	})

	w.Read(func(r *flecs.Reader) {
		if !r.HasID(e, flecs.MakePair(R, w.Wildcard())) {
			t.Error("HasID(R,Wildcard) should be true while a target is held")
		}
	})
}

// --- Test 5: RemoveID(Pair(R,T)) matching current clears; non-matching is no-op ---

func TestUnion_RemoveID_SpecificMatch(t *testing.T) {
	w, R, T1, T2 := newUnionWorld(t)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(R, T1))
	})

	// Remove T2 (not current target) — should be a no-op.
	w.Write(func(fw *flecs.Writer) {
		fw.RemoveID(e, flecs.MakePair(R, T2))
	})
	w.Read(func(r *flecs.Reader) {
		if !r.HasID(e, flecs.MakePair(R, T1)) {
			t.Error("RemoveID(R,T2) should be no-op when T1 is current")
		}
	})

	// Remove T1 (current target) — clears.
	w.Write(func(fw *flecs.Writer) {
		fw.RemoveID(e, flecs.MakePair(R, T1))
	})
	w.Read(func(r *flecs.Reader) {
		if r.HasID(e, flecs.MakePair(R, T1)) {
			t.Error("HasID(R,T1) should be false after RemoveID(R,T1)")
		}
		if r.HasID(e, flecs.MakePair(R, w.Wildcard())) {
			t.Error("HasID(R,Wildcard) should be false after clearing the union target")
		}
	})
}

// --- Test 6: RemoveID(Pair(R,Wildcard)) clears regardless of current target ---

func TestUnion_RemoveID_Wildcard(t *testing.T) {
	w, R, T1, _ := newUnionWorld(t)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(R, T1))
	})

	w.Write(func(fw *flecs.Writer) {
		fw.RemoveID(e, flecs.MakePair(R, w.Wildcard()))
	})

	w.Read(func(r *flecs.Reader) {
		if r.HasID(e, flecs.MakePair(R, w.Wildcard())) {
			t.Error("HasID(R,Wildcard) should be false after wildcard remove")
		}
	})
}

// --- Test 7: SetPair on union relationship panics ---

func TestUnion_SetPair_Panics(t *testing.T) {
	w, R, T1, _ := newUnionWorld(t)
	type tag struct{}
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic from SetPair on a Union relationship")
		}
	}()

	w.Write(func(fw *flecs.Writer) {
		flecs.SetPair[tag](fw, e, R, T1, tag{})
	})
}

// --- Test 8: Query (R,*) yields all entities with active union targets ---

func TestUnion_Query_WildcardTarget(t *testing.T) {
	w, R, T1, T2 := newUnionWorld(t)
	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		fw.AddID(e1, flecs.MakePair(R, T1))
		fw.AddID(e2, flecs.MakePair(R, T2))
	})

	count := 0
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(R, w.Wildcard())))
	it := q.Iter()
	for it.Next() {
		count += it.Count()
	}

	if count != 2 {
		t.Errorf("expected 2 entities from wildcard union query, got %d", count)
	}
	_, _ = e1, e2
}

// --- Test 9: Query (R,T) yields only entities with exactly that target ---

func TestUnion_Query_SpecificTarget(t *testing.T) {
	w, R, T1, T2 := newUnionWorld(t)
	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		fw.AddID(e1, flecs.MakePair(R, T1))
		fw.AddID(e2, flecs.MakePair(R, T2))
	})

	var matched []flecs.ID
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(R, T1)))
	it := q.Iter()
	for it.Next() {
		matched = append(matched, it.Entities()...)
	}

	if len(matched) != 1 {
		t.Fatalf("expected 1 entity matching (R,T1), got %d", len(matched))
	}
	if matched[0].Index() != e1.Index() {
		t.Errorf("expected e1, got a different entity")
	}
	_ = e2
}

// --- Test 10: Pure-union query at scale: 100 entities, no archetype proliferation ---

func TestUnion_Query_ScaleNoFragmentation(t *testing.T) {
	w := flecs.New()
	var R flecs.ID
	w.Write(func(fw *flecs.Writer) { R = fw.NewEntity() })
	flecs.SetUnion(w, R)

	const n = 100
	targets := make([]flecs.ID, n)
	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			targets[i] = fw.NewEntity()
		}
	})
	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			e := fw.NewEntity()
			fw.AddID(e, flecs.MakePair(R, targets[i]))
		}
	})

	// No archetype table should exist for any union pair.
	for i := range n {
		if len(w.TablesFor(flecs.MakePair(R, targets[i]))) != 0 {
			t.Errorf("target %d: union pair created an archetype table", i)
		}
	}

	count := 0
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(R, w.Wildcard())))
	it := q.Iter()
	for it.Next() {
		count += it.Count()
	}
	if count != n {
		t.Errorf("expected %d entities from union query, got %d", n, count)
	}
}

// --- Test 11: Mixed query: union term + archetype term ---

func TestUnion_Query_Mixed(t *testing.T) {
	w := flecs.New()
	type Pos struct{ X, Y int }
	posID := flecs.RegisterComponent[Pos](w)
	var R, T1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		T1 = fw.NewEntity()
	})
	flecs.SetUnion(w, R)

	var withPos, withoutPos flecs.ID
	w.Write(func(fw *flecs.Writer) {
		withPos = fw.NewEntity()
		flecs.Set(fw, withPos, Pos{X: 1})
		fw.AddID(withPos, flecs.MakePair(R, T1))

		withoutPos = fw.NewEntity()
		fw.AddID(withoutPos, flecs.MakePair(R, T1))
	})

	var matched []flecs.ID
	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.With(flecs.MakePair(R, T1)),
	)
	it := q.Iter()
	for it.Next() {
		matched = append(matched, it.Entities()...)
	}

	if len(matched) != 1 {
		t.Fatalf("expected 1 entity (intersection of Pos and (R,T1)), got %d", len(matched))
	}
	if matched[0].Index() != withPos.Index() {
		t.Error("matched wrong entity in mixed union+archetype query")
	}
	_ = withoutPos
}

// --- Test 12: SetUnion after SetExclusive panics ---

func TestUnion_Conflict_ExclusiveThenUnion(t *testing.T) {
	w := flecs.New()
	var R flecs.ID
	w.Write(func(fw *flecs.Writer) { R = fw.NewEntity() })
	flecs.SetExclusive(w, R)

	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic from SetUnion on already-Exclusive relationship")
		}
		msg, ok := r.(string)
		if !ok {
			return
		}
		if !strings.Contains(msg, "Exclusive") {
			t.Errorf("panic message should mention Exclusive; got: %q", msg)
		}
	}()
	flecs.SetUnion(w, R)
}

// --- Test 13: SetExclusive after SetUnion panics ---

func TestUnion_Conflict_UnionThenExclusive(t *testing.T) {
	w, R, _, _ := newUnionWorld(t)

	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic from SetExclusive on already-Union relationship")
		}
		msg, ok := r.(string)
		if !ok {
			return
		}
		if !strings.Contains(msg, "Union") {
			t.Errorf("panic message should mention Union; got: %q", msg)
		}
	}()
	flecs.SetExclusive(w, R)
}

// --- Test 14: Marshal round-trip preserves union state ---

func TestUnion_Marshal_Roundtrip(t *testing.T) {
	w := flecs.New()
	var R, T1, T2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		T1 = fw.NewEntity()
		T2 = fw.NewEntity()
	})
	flecs.SetUnion(w, R)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		fw.AddID(e1, flecs.MakePair(R, T1))
		fw.AddID(e2, flecs.MakePair(R, T2))
	})
	_, _ = e1, e2

	data, err := w.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if !json.Valid(data) {
		t.Fatal("MarshalJSON produced invalid JSON")
	}
	if !strings.Contains(string(data), "union_relationships") {
		t.Error("marshal output should contain union_relationships")
	}
	if !strings.Contains(string(data), "union_relationship_serials") {
		t.Error("marshal output should contain union_relationship_serials")
	}

	w2 := flecs.New()
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// Find the union relationship in w2 by checking IsUnion on all alive entities.
	var R2 flecs.ID
	w2.Read(func(r *flecs.Reader) {
		for _, id := range r.AliveEntities() {
			if flecs.IsUnion(r, id) {
				R2 = id
				break
			}
		}
	})
	if R2 == 0 {
		t.Fatal("no union relationship found in restored world")
	}

	// Count entities with an active union pair for R2 in w2.
	unionCount := 0
	w2.Read(func(r *flecs.Reader) {
		flecs.EachUnion(r, R2, func(_ flecs.ID, _ flecs.ID) {
			unionCount++
		})
	})
	if unionCount != 2 {
		t.Errorf("expected 2 union entries in restored world, got %d", unionCount)
	}
}

// --- Test 15: Entity delete clears union store entries ---

func TestUnion_EntityDelete_ClearsStore(t *testing.T) {
	w, R, T1, _ := newUnionWorld(t)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(R, T1))
	})

	removeCount := 0
	flecs.ObserveID(w, flecs.MakePair(R, T1), flecs.EventOnRemove,
		func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) { removeCount++ })

	w.Write(func(fw *flecs.Writer) {
		fw.Delete(e)
	})

	if removeCount != 1 {
		t.Errorf("expected OnRemove to fire once on entity delete, got %d", removeCount)
	}

	// EachUnion should yield nothing after delete.
	count := 0
	w.Read(func(r *flecs.Reader) {
		flecs.EachUnion(r, R, func(_ flecs.ID, _ flecs.ID) { count++ })
	})
	if count != 0 {
		t.Errorf("expected empty union store after entity delete, got %d entries", count)
	}
}

// --- Test 16: Relationship entity delete drops the entire store ---

func TestUnion_RelationshipDelete_DropsStore(t *testing.T) {
	w, R, T1, _ := newUnionWorld(t)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(R, T1))
	})
	_ = e

	// Delete the relationship entity R itself.
	w.Write(func(fw *flecs.Writer) {
		fw.Delete(R)
	})

	// IsUnion should return false now that R is deleted and the policy was dropped.
	isUnion := false
	w.Read(func(r *flecs.Reader) {
		isUnion = flecs.IsUnion(r, R)
	})
	if isUnion {
		t.Error("expected IsUnion to return false after the relationship entity was deleted")
	}
}

// --- Additional test: IsUnion returns correct values ---

func TestUnion_IsUnion(t *testing.T) {
	w := flecs.New()
	var R flecs.ID
	w.Write(func(fw *flecs.Writer) { R = fw.NewEntity() })

	w.Read(func(r *flecs.Reader) {
		if flecs.IsUnion(r, R) {
			t.Error("IsUnion should be false before SetUnion")
		}
	})
	flecs.SetUnion(w, R)
	w.Read(func(r *flecs.Reader) {
		if !flecs.IsUnion(r, R) {
			t.Error("IsUnion should be true after SetUnion")
		}
	})
}

// --- Additional test: SetUnion is idempotent ---

func TestUnion_SetUnion_Idempotent(t *testing.T) {
	w, R, _, _ := newUnionWorld(t)
	// Calling SetUnion a second time must not panic.
	flecs.SetUnion(w, R)
	flecs.SetUnion(w, R)
}

// --- Additional test: EachUnion preserves insertion order ---

func TestUnion_EachUnion_InsertionOrder(t *testing.T) {
	w, R, T1, T2 := newUnionWorld(t)
	var T3 flecs.ID
	w.Write(func(fw *flecs.Writer) { T3 = fw.NewEntity() })

	var e1, e2, e3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		e3 = fw.NewEntity()
		fw.AddID(e1, flecs.MakePair(R, T1))
		fw.AddID(e2, flecs.MakePair(R, T2))
		fw.AddID(e3, flecs.MakePair(R, T3))
	})

	var order []flecs.ID
	w.Read(func(r *flecs.Reader) {
		flecs.EachUnion(r, R, func(e flecs.ID, _ flecs.ID) {
			order = append(order, e)
		})
	})

	if len(order) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(order))
	}
	if order[0].Index() != e1.Index() || order[1].Index() != e2.Index() || order[2].Index() != e3.Index() {
		t.Error("EachUnion: entries not in insertion order")
	}
}

// --- Additional test: OnAdd and OnRemove hooks fire on union pair replace ---

func TestUnion_Hooks_FireOnReplace(t *testing.T) {
	w, R, T1, T2 := newUnionWorld(t)

	addCount := map[flecs.ID]int{}
	removeCount := map[flecs.ID]int{}

	pairA := flecs.MakePair(R, T1)
	pairB := flecs.MakePair(R, T2)

	flecs.ObserveID(w, pairA, flecs.EventOnAdd,
		func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) { addCount[T1]++ })
	flecs.ObserveID(w, pairA, flecs.EventOnRemove,
		func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) { removeCount[T1]++ })
	flecs.ObserveID(w, pairB, flecs.EventOnAdd,
		func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) { addCount[T2]++ })
	flecs.ObserveID(w, pairB, flecs.EventOnRemove,
		func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) { removeCount[T2]++ })

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		fw.AddID(e, pairA)
	})
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, pairB) // replaces T1 with T2
	})

	if addCount[T1] != 1 {
		t.Errorf("OnAdd(R,T1): expected 1, got %d", addCount[T1])
	}
	if removeCount[T1] != 1 {
		t.Errorf("OnRemove(R,T1): expected 1 (fired when T2 replaced T1), got %d", removeCount[T1])
	}
	if addCount[T2] != 1 {
		t.Errorf("OnAdd(R,T2): expected 1, got %d", addCount[T2])
	}
	if removeCount[T2] != 0 {
		t.Errorf("OnRemove(R,T2): expected 0, got %d", removeCount[T2])
	}
	_ = e
}

// --- Additional test: CachedQuery with union terms ---

func TestUnion_CachedQuery(t *testing.T) {
	w, R, T1, T2 := newUnionWorld(t)
	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		fw.AddID(e1, flecs.MakePair(R, T1))
		fw.AddID(e2, flecs.MakePair(R, T2))
	})
	_, _ = e1, e2

	cq := flecs.NewCachedQueryFromTerms(w, flecs.With(flecs.MakePair(R, w.Wildcard())))
	defer cq.Close()

	count := 0
	cq.Each(func(it *flecs.QueryIter) {
		count += it.Count()
	})

	if count != 2 {
		t.Errorf("expected 2 entities from cached union query, got %d", count)
	}
}

// --- Additional test: OwnsID matches HasID for union pairs ---

func TestUnion_OwnsID(t *testing.T) {
	w, R, T1, _ := newUnionWorld(t)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(R, T1))
	})

	w.Read(func(r *flecs.Reader) {
		if !r.OwnsID(e, flecs.MakePair(R, T1)) {
			t.Error("OwnsID(R,T1) should be true for union pair")
		}
		if !r.OwnsID(e, flecs.MakePair(R, w.Wildcard())) {
			t.Error("OwnsID(R,Wildcard) should be true when any target is held")
		}
	})
}
