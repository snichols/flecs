package flecs_test

import (
	"context"
	"errors"
	"iter"
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
)

// ── component types ──────────────────────────────────────────────────────────

type travPos struct{ X, Y float32 }
type travVel struct{ DX float32 }
type travTag struct{}

// ── compile-time interface assertions ────────────────────────────────────────

func TestSeq_CompileTimeAssertions(t *testing.T) {
	w := flecs.New()
	ctx := context.Background()

	posID := flecs.RegisterComponent[travPos](w)
	var rel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
	})
	flecs.SetUnion(w, rel)

	w.Read(func(r *flecs.Reader) {
		var _ iter.Seq[flecs.ID] = r.Entities()
		var _ iter.Seq[flecs.ID] = r.Children(0)
		var _ iter.Seq[flecs.ID] = r.Prefabs(0)
		var _ iter.Seq[*flecs.System] = r.Systems(w.OnUpdate())
		var _ iter.Seq2[flecs.ID, flecs.ID] = flecs.Union(r, rel)
		var _ iter.Seq2[flecs.ID, *travTag] = flecs.Sparse[travTag](r)
		var _ iter.Seq2[flecs.ID, unsafe.Pointer] = flecs.ByID(r, posID)
		var _ iter.Seq2[flecs.ID, error] = r.EntitiesContext(ctx)
		var _ iter.Seq2[flecs.ID, error] = flecs.ByIDContext(ctx, r, posID)
	})
}

// ── Entities ─────────────────────────────────────────────────────────────────

func TestSeq_Entities_YieldsAll(t *testing.T) {
	w := flecs.New()
	var ids [5]flecs.ID
	w.Write(func(fw *flecs.Writer) {
		for i := range 5 {
			ids[i] = fw.NewEntity()
		}
	})

	visited := map[flecs.ID]bool{}
	w.Read(func(r *flecs.Reader) {
		for e := range r.Entities() {
			visited[e] = true
		}
	})

	for _, id := range ids {
		if !visited[id] {
			t.Errorf("entity %v not visited", id)
		}
	}
}

func TestSeq_Entities_BreakHonored(t *testing.T) {
	w := flecs.New()
	const total = 10
	w.Write(func(fw *flecs.Writer) {
		for range total {
			fw.NewEntity()
		}
	})

	count := 0
	w.Read(func(r *flecs.Reader) {
		for range r.Entities() {
			count++
			if count == 3 {
				break
			}
		}
	})

	if count != 3 {
		t.Errorf("expected exactly 3 entities visited, got %d", count)
	}
}

// TestSeq_Entities_Empty verifies that Entities() count matches AliveEntities()
// in a fresh world (including built-in entities). A truly zero-entity world is
// not achievable since the built-in entity set is always present.
func TestSeq_Entities_Empty(t *testing.T) {
	w := flecs.New()
	var alive []flecs.ID
	w.Read(func(r *flecs.Reader) {
		alive = r.AliveEntities()
	})

	count := 0
	w.Read(func(r *flecs.Reader) {
		for range r.Entities() {
			count++
		}
	})
	if count != len(alive) {
		t.Errorf("Entities() count %d != AliveEntities() count %d in fresh world", count, len(alive))
	}
}

func TestSeq_Entities_MatchesEachEntity(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		for range 8 {
			fw.NewEntity()
		}
	})

	var eachOrder []flecs.ID
	var seqOrder []flecs.ID

	w.Read(func(r *flecs.Reader) {
		r.EachEntity(func(e flecs.ID) bool {
			eachOrder = append(eachOrder, e)
			return true
		})
		for e := range r.Entities() {
			seqOrder = append(seqOrder, e)
		}
	})

	if len(eachOrder) != len(seqOrder) {
		t.Fatalf("len mismatch: EachEntity=%d Entities=%d", len(eachOrder), len(seqOrder))
	}
	for i, e := range eachOrder {
		if seqOrder[i] != e {
			t.Errorf("order mismatch at %d: EachEntity=%v Entities=%v", i, e, seqOrder[i])
		}
	}
}

func TestSeq_Entities_LargeWorld(t *testing.T) {
	const N = 10_000
	w := flecs.New()

	// Baseline: built-in entity count in a fresh world.
	var baseline int
	w.Read(func(r *flecs.Reader) { baseline = len(r.AliveEntities()) })

	w.Write(func(fw *flecs.Writer) {
		for range N {
			fw.NewEntity()
		}
	})

	count := 0
	w.Read(func(r *flecs.Reader) {
		for range r.Entities() {
			count++
		}
	})
	if count != baseline+N {
		t.Errorf("large world: expected %d (baseline %d + %d), got %d", baseline+N, baseline, N, count)
	}
}

func TestSeq_Entities_DuringMutation_ReadScope(t *testing.T) {
	w := flecs.New()
	var ids [5]flecs.ID
	w.Write(func(fw *flecs.Writer) {
		for i := range 5 {
			ids[i] = fw.NewEntity()
		}
	})

	// Both EachEntity and Entities() must see the same snapshot inside Read.
	var eachSet, seqSet []flecs.ID
	w.Read(func(r *flecs.Reader) {
		r.EachEntity(func(e flecs.ID) bool {
			eachSet = append(eachSet, e)
			return true
		})
		for e := range r.Entities() {
			seqSet = append(seqSet, e)
		}
	})

	if len(eachSet) != len(seqSet) {
		t.Fatalf("mutation-scope mismatch: EachEntity=%d Entities=%d", len(eachSet), len(seqSet))
	}
}

// ── EntitiesContext ──────────────────────────────────────────────────────────

// TestSeq_EntitiesContext_Canceled creates >ctxCheckInterval entities so the
// periodic ctx check fires. Cancel is called on the first yield; the ctx error
// is yielded at the 1024-entity checkpoint.
func TestSeq_EntitiesContext_Canceled(t *testing.T) {
	const N = 1025 // exceeds ctxCheckInterval=1024
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		for range N {
			fw.NewEntity()
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gotErr := false

	w.Read(func(r *flecs.Reader) {
		for id, err := range r.EntitiesContext(ctx) {
			if err != nil {
				gotErr = true
				if id != 0 {
					t.Errorf("on cancel expected id=0, got %v", id)
				}
				if !errors.Is(err, context.Canceled) {
					t.Errorf("expected context.Canceled, got %v", err)
				}
				break
			}
			cancel() // cancel on first entity; ctx check fires at 1024
		}
	})

	if !gotErr {
		t.Error("expected context-cancel error to be yielded")
	}
}

func TestSeq_EntitiesContext_PreCanceled(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) { fw.NewEntity() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	seenID := flecs.ID(1)
	var seenErr error
	w.Read(func(r *flecs.Reader) {
		for id, err := range r.EntitiesContext(ctx) {
			seenID = id
			seenErr = err
			break
		}
	})

	if seenID != 0 {
		t.Errorf("pre-canceled: expected id=0, got %v", seenID)
	}
	if seenErr == nil {
		t.Error("pre-canceled: expected non-nil error")
	}
}

// ── Children ─────────────────────────────────────────────────────────────────

func TestSeq_Children_YieldsAll(t *testing.T) {
	w := flecs.New()
	var parent flecs.ID
	var children [4]flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		for i := range 4 {
			c := fw.NewEntity()
			fw.AddID(c, flecs.MakePair(w.ChildOf(), parent))
			children[i] = c
		}
	})

	visited := map[flecs.ID]bool{}
	w.Read(func(r *flecs.Reader) {
		for c := range r.Children(parent) {
			visited[c] = true
		}
	})

	for _, c := range children {
		if !visited[c] {
			t.Errorf("child %v not visited", c)
		}
	}
}

func TestSeq_Children_BreakHonored(t *testing.T) {
	w := flecs.New()
	var parent flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		for range 6 {
			c := fw.NewEntity()
			fw.AddID(c, flecs.MakePair(w.ChildOf(), parent))
		}
	})

	count := 0
	w.Read(func(r *flecs.Reader) {
		for range r.Children(parent) {
			count++
			if count == 2 {
				break
			}
		}
	})

	if count != 2 {
		t.Errorf("expected 2 children before break, got %d", count)
	}
}

func TestSeq_Children_Empty(t *testing.T) {
	w := flecs.New()
	var parent flecs.ID
	w.Write(func(fw *flecs.Writer) { parent = fw.NewEntity() })

	called := false
	w.Read(func(r *flecs.Reader) {
		for range r.Children(parent) {
			called = true
		}
	})
	if called {
		t.Error("expected no children for childless entity")
	}
}

func TestSeq_Children_MatchesEachChild(t *testing.T) {
	w := flecs.New()
	var parent flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		for range 5 {
			c := fw.NewEntity()
			fw.AddID(c, flecs.MakePair(w.ChildOf(), parent))
		}
	})

	eachSet := map[flecs.ID]bool{}
	seqSet := map[flecs.ID]bool{}
	w.Read(func(r *flecs.Reader) {
		r.EachChild(parent, func(c flecs.ID) bool {
			eachSet[c] = true
			return true
		})
		for c := range r.Children(parent) {
			seqSet[c] = true
		}
	})

	if len(eachSet) != len(seqSet) {
		t.Fatalf("Children vs EachChild count mismatch: %d vs %d", len(seqSet), len(eachSet))
	}
	for c := range eachSet {
		if !seqSet[c] {
			t.Errorf("entity %v in EachChild but not Children", c)
		}
	}
}

func TestSeq_Children_OrderedChildren(t *testing.T) {
	w := flecs.New()
	var parent flecs.ID
	var c1, c2, c3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.SetOrderedChildren(w, parent)
		c1 = fw.NewEntity()
		fw.AddID(c1, flecs.MakePair(w.ChildOf(), parent))
		c2 = fw.NewEntity()
		fw.AddID(c2, flecs.MakePair(w.ChildOf(), parent))
		c3 = fw.NewEntity()
		fw.AddID(c3, flecs.MakePair(w.ChildOf(), parent))
	})

	var order []flecs.ID
	w.Read(func(r *flecs.Reader) {
		for c := range r.Children(parent) {
			order = append(order, c)
		}
	})

	if len(order) != 3 {
		t.Fatalf("expected 3 ordered children, got %d", len(order))
	}
	want := []flecs.ID{c1, c2, c3}
	for i, got := range order {
		if got != want[i] {
			t.Errorf("ordered child[%d]: got %v, want %v", i, got, want[i])
		}
	}
}

func TestSeq_Children_Unordered(t *testing.T) {
	w := flecs.New()
	var parent flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		for range 4 {
			c := fw.NewEntity()
			fw.AddID(c, flecs.MakePair(w.ChildOf(), parent))
		}
	})

	eachSet := map[flecs.ID]bool{}
	seqSet := map[flecs.ID]bool{}
	w.Read(func(r *flecs.Reader) {
		r.EachChild(parent, func(c flecs.ID) bool {
			eachSet[c] = true
			return true
		})
		for c := range r.Children(parent) {
			seqSet[c] = true
		}
	})

	if len(eachSet) != len(seqSet) {
		t.Fatalf("unordered children count mismatch: EachChild=%d Children=%d", len(eachSet), len(seqSet))
	}
	for c := range eachSet {
		if !seqSet[c] {
			t.Errorf("entity %v in EachChild but not Children", c)
		}
	}
}

// ── Prefabs ──────────────────────────────────────────────────────────────────

func TestSeq_Prefabs_YieldsAll(t *testing.T) {
	w := flecs.New()
	var pA, pB, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		pA = fw.NewEntity()
		pB = fw.NewEntity()
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(w.IsA(), pA))
		fw.AddID(e, flecs.MakePair(w.IsA(), pB))
	})

	visited := map[flecs.ID]bool{}
	w.Read(func(r *flecs.Reader) {
		for p := range r.Prefabs(e) {
			visited[p] = true
		}
	})

	if !visited[pA] || !visited[pB] {
		t.Errorf("not all prefabs visited: %v", visited)
	}
}

func TestSeq_Prefabs_BreakHonored(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 := fw.NewEntity()
		p2 := fw.NewEntity()
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(w.IsA(), p1))
		fw.AddID(e, flecs.MakePair(w.IsA(), p2))
	})

	count := 0
	w.Read(func(r *flecs.Reader) {
		for range r.Prefabs(e) {
			count++
			break
		}
	})
	if count != 1 {
		t.Errorf("expected break after 1 prefab, got %d", count)
	}
}

func TestSeq_Prefabs_Empty(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	called := false
	w.Read(func(r *flecs.Reader) {
		for range r.Prefabs(e) {
			called = true
		}
	})
	if called {
		t.Error("expected no prefabs for entity with no IsA")
	}
}

func TestSeq_Prefabs_MatchesEachPrefab(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p := fw.NewEntity()
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(w.IsA(), p))
	})

	eachSet := map[flecs.ID]bool{}
	seqSet := map[flecs.ID]bool{}
	w.Read(func(r *flecs.Reader) {
		r.EachPrefab(e, func(p flecs.ID) bool {
			eachSet[p] = true
			return true
		})
		for p := range r.Prefabs(e) {
			seqSet[p] = true
		}
	})

	if len(eachSet) != len(seqSet) {
		t.Fatalf("Prefabs vs EachPrefab count mismatch")
	}
	for p := range eachSet {
		if !seqSet[p] {
			t.Errorf("prefab %v in EachPrefab but not Prefabs", p)
		}
	}
}

// TestSeq_Prefabs_MultiLevel verifies that Prefabs(C) in an A→B→C IsA chain
// yields only C's direct prefab (B), not the transitive ancestor (A).
func TestSeq_Prefabs_MultiLevel(t *testing.T) {
	w := flecs.New()
	var a, b, c flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
		b = fw.NewEntity()
		c = fw.NewEntity()
		fw.AddID(b, flecs.MakePair(w.IsA(), a))
		fw.AddID(c, flecs.MakePair(w.IsA(), b))
	})

	var prefabs []flecs.ID
	w.Read(func(r *flecs.Reader) {
		for p := range r.Prefabs(c) {
			prefabs = append(prefabs, p)
		}
	})

	if len(prefabs) != 1 || prefabs[0] != b {
		t.Errorf("MultiLevel: expected [%v], got %v", b, prefabs)
	}
}

// ── Systems ──────────────────────────────────────────────────────────────────

func TestSeq_Systems_YieldsAll(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)
	q := flecs.NewCachedQuery(w, posID)
	s1 := flecs.NewSystem(w, q, func(_ float32, _ *flecs.QueryIter) {})
	s2 := flecs.NewSystem(w, q, func(_ float32, _ *flecs.QueryIter) {})

	visited := map[*flecs.System]bool{}
	w.Read(func(r *flecs.Reader) {
		for s := range r.Systems(w.OnUpdate()) {
			visited[s] = true
		}
	})

	if !visited[s1] || !visited[s2] {
		t.Errorf("not all systems visited: s1=%v s2=%v", visited[s1], visited[s2])
	}
}

func TestSeq_Systems_BreakHonored(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)
	q := flecs.NewCachedQuery(w, posID)
	for range 5 {
		flecs.NewSystem(w, q, func(_ float32, _ *flecs.QueryIter) {})
	}

	count := 0
	w.Read(func(r *flecs.Reader) {
		for range r.Systems(w.OnUpdate()) {
			count++
			if count == 2 {
				break
			}
		}
	})
	if count != 2 {
		t.Errorf("expected break after 2 systems, got %d", count)
	}
}

// TestSeq_Systems_Empty uses PreUpdate in a fresh world (no systems registered
// there by default) to verify the range body never executes.
func TestSeq_Systems_Empty(t *testing.T) {
	w := flecs.New()

	called := false
	w.Read(func(r *flecs.Reader) {
		for range r.Systems(w.PreUpdate()) {
			called = true
		}
	})
	if called {
		t.Error("expected no systems in empty PreUpdate phase")
	}
}

func TestSeq_Systems_MatchesEachSystem(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)
	q := flecs.NewCachedQuery(w, posID)
	for range 3 {
		flecs.NewSystem(w, q, func(_ float32, _ *flecs.QueryIter) {})
	}

	var eachSlice, seqSlice []*flecs.System
	w.Read(func(r *flecs.Reader) {
		r.EachSystem(w.OnUpdate(), func(s *flecs.System) bool {
			eachSlice = append(eachSlice, s)
			return true
		})
		for s := range r.Systems(w.OnUpdate()) {
			seqSlice = append(seqSlice, s)
		}
	})

	if len(eachSlice) != len(seqSlice) {
		t.Fatalf("Systems vs EachSystem count mismatch: %d vs %d", len(seqSlice), len(eachSlice))
	}
	for i, s := range eachSlice {
		if seqSlice[i] != s {
			t.Errorf("order mismatch at %d", i)
		}
	}
}

func TestSeq_Systems_InPhase(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)
	q := flecs.NewCachedQuery(w, posID)
	s1 := flecs.NewSystem(w, q, func(_ float32, _ *flecs.QueryIter) {})
	s2 := flecs.NewSystem(w, q, func(_ float32, _ *flecs.QueryIter) {})
	s3 := flecs.NewSystem(w, q, func(_ float32, _ *flecs.QueryIter) {})
	s3.SetEnabled(false) // disabled systems must still be yielded

	found := map[*flecs.System]bool{}
	w.Read(func(r *flecs.Reader) {
		for s := range r.Systems(w.OnUpdate()) {
			found[s] = true
		}
	})

	if !found[s1] || !found[s2] || !found[s3] {
		t.Errorf("disabled system missing from Systems(): s1=%v s2=%v s3=%v", found[s1], found[s2], found[s3])
	}
}

func TestSeq_Systems_NilPhasePanics(t *testing.T) {
	w := flecs.New()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil phase")
		}
	}()
	w.Read(func(r *flecs.Reader) {
		for range r.Systems(nil) {
		}
	})
}

// ── Union ────────────────────────────────────────────────────────────────────

func TestSeq_Union_YieldsAll(t *testing.T) {
	w := flecs.New()
	var rel, t1, t2 flecs.ID
	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		t1 = fw.NewEntity()
		t2 = fw.NewEntity()
	})
	flecs.SetUnion(w, rel)
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		fw.AddID(e1, flecs.MakePair(rel, t1))
		e2 = fw.NewEntity()
		fw.AddID(e2, flecs.MakePair(rel, t2))
	})

	type pair struct{ entity, target flecs.ID }
	visited := map[pair]bool{}
	w.Read(func(r *flecs.Reader) {
		for e, tgt := range flecs.Union(r, rel) {
			visited[pair{e, tgt}] = true
		}
	})

	if !visited[pair{e1, t1}] || !visited[pair{e2, t2}] {
		t.Errorf("not all union pairs visited: %v", visited)
	}
}

func TestSeq_Union_BreakHonored(t *testing.T) {
	w := flecs.New()
	var rel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
	})
	flecs.SetUnion(w, rel)
	w.Write(func(fw *flecs.Writer) {
		tgt := fw.NewEntity()
		for range 5 {
			e := fw.NewEntity()
			fw.AddID(e, flecs.MakePair(rel, tgt))
		}
	})

	count := 0
	w.Read(func(r *flecs.Reader) {
		for range flecs.Union(r, rel) {
			count++
			if count == 2 {
				break
			}
		}
	})
	if count != 2 {
		t.Errorf("expected break after 2 union entries, got %d", count)
	}
}

func TestSeq_Union_Empty(t *testing.T) {
	w := flecs.New()
	var rel flecs.ID
	w.Write(func(fw *flecs.Writer) { rel = fw.NewEntity() })
	flecs.SetUnion(w, rel)

	called := false
	w.Read(func(r *flecs.Reader) {
		for range flecs.Union(r, rel) {
			called = true
		}
	})
	if called {
		t.Error("expected no union entries for empty relationship")
	}
}

func TestSeq_Union_MatchesEachUnion(t *testing.T) {
	w := flecs.New()
	var rel, tgt flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
	})
	flecs.SetUnion(w, rel)
	w.Write(func(fw *flecs.Writer) {
		for range 4 {
			e := fw.NewEntity()
			fw.AddID(e, flecs.MakePair(rel, tgt))
		}
	})

	type pair struct{ e, t flecs.ID }
	var eachPairs, seqPairs []pair
	w.Read(func(r *flecs.Reader) {
		flecs.EachUnion(r, rel, func(e, target flecs.ID) {
			eachPairs = append(eachPairs, pair{e, target})
		})
		for e, target := range flecs.Union(r, rel) {
			seqPairs = append(seqPairs, pair{e, target})
		}
	})

	if len(eachPairs) != len(seqPairs) {
		t.Fatalf("Union vs EachUnion count mismatch: %d vs %d", len(seqPairs), len(eachPairs))
	}
	for i, p := range eachPairs {
		if seqPairs[i] != p {
			t.Errorf("order mismatch at %d: EachUnion=%v Union=%v", i, p, seqPairs[i])
		}
	}
}

func TestSeq_Union_Members(t *testing.T) {
	w := flecs.New()
	var rel, tgt1, tgt2 flecs.ID
	var e1, e2, e3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
	})
	flecs.SetUnion(w, rel)
	w.Write(func(fw *flecs.Writer) {
		tgt1 = fw.NewEntity()
		tgt2 = fw.NewEntity()
		e1 = fw.NewEntity()
		fw.AddID(e1, flecs.MakePair(rel, tgt1))
		e2 = fw.NewEntity()
		fw.AddID(e2, flecs.MakePair(rel, tgt2))
		e3 = fw.NewEntity()
		fw.AddID(e3, flecs.MakePair(rel, tgt1))
	})

	type pair struct{ e, t flecs.ID }
	var got []pair
	w.Read(func(r *flecs.Reader) {
		for e, tgt := range flecs.Union(r, rel) {
			got = append(got, pair{e, tgt})
		}
	})

	// Insertion order: e1→tgt1, e2→tgt2, e3→tgt1.
	want := []pair{{e1, tgt1}, {e2, tgt2}, {e3, tgt1}}
	if len(got) != len(want) {
		t.Fatalf("union members count: got %d, want %d", len(got), len(want))
	}
	for i, p := range want {
		if got[i] != p {
			t.Errorf("union members[%d]: got %v, want %v", i, got[i], p)
		}
	}
}

// ── Sparse ───────────────────────────────────────────────────────────────────

func TestSeq_Sparse_YieldsAll(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)
	flecs.SetSparse(w, posID)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, travPos{X: 1})
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, travPos{X: 2})
	})

	visited := map[flecs.ID]bool{}
	w.Read(func(r *flecs.Reader) {
		for e := range flecs.Sparse[travPos](r) {
			visited[e] = true
		}
	})

	if !visited[e1] || !visited[e2] {
		t.Errorf("not all sparse holders visited: %v", visited)
	}
}

func TestSeq_Sparse_BreakHonored(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)
	flecs.SetSparse(w, posID)

	w.Write(func(fw *flecs.Writer) {
		for i := range 5 {
			e := fw.NewEntity()
			flecs.Set(fw, e, travPos{X: float32(i)})
		}
	})

	count := 0
	w.Read(func(r *flecs.Reader) {
		for range flecs.Sparse[travPos](r) {
			count++
			if count == 2 {
				break
			}
		}
	})
	if count != 2 {
		t.Errorf("expected break after 2 sparse entries, got %d", count)
	}
}

func TestSeq_Sparse_Empty(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)
	flecs.SetSparse(w, posID)

	called := false
	w.Read(func(r *flecs.Reader) {
		for range flecs.Sparse[travPos](r) {
			called = true
		}
	})
	if called {
		t.Error("expected no sparse entries before any Set")
	}
}

func TestSeq_Sparse_MatchesEachSparse(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)
	flecs.SetSparse(w, posID)

	w.Write(func(fw *flecs.Writer) {
		for i := range 4 {
			e := fw.NewEntity()
			flecs.Set(fw, e, travPos{X: float32(i)})
		}
	})

	type ep struct {
		e flecs.ID
		x float32
	}
	var eachPairs, seqPairs []ep
	w.Read(func(r *flecs.Reader) {
		flecs.EachSparse[travPos](r, func(e flecs.ID, v *travPos) {
			eachPairs = append(eachPairs, ep{e, v.X})
		})
		for e, v := range flecs.Sparse[travPos](r) {
			seqPairs = append(seqPairs, ep{e, v.X})
		}
	})

	if len(eachPairs) != len(seqPairs) {
		t.Fatalf("Sparse vs EachSparse count mismatch: %d vs %d", len(seqPairs), len(eachPairs))
	}
	for i, p := range eachPairs {
		if seqPairs[i] != p {
			t.Errorf("order mismatch at %d: EachSparse=%v Sparse=%v", i, p, seqPairs[i])
		}
	}
}

func TestSeq_Sparse_Holders(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)
	flecs.SetSparse(w, posID)

	var e1, e2, e3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, travPos{X: 10, Y: 1})
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, travPos{X: 20, Y: 2})
		e3 = fw.NewEntity()
		flecs.Set(fw, e3, travPos{X: 30, Y: 3})
	})

	type pair struct {
		e flecs.ID
		x float32
	}
	var got []pair
	w.Read(func(r *flecs.Reader) {
		for e, p := range flecs.Sparse[travPos](r) {
			got = append(got, pair{e, p.X})
		}
	})

	want := []pair{{e1, 10}, {e2, 20}, {e3, 30}}
	if len(got) != len(want) {
		t.Fatalf("sparse holders count: got %d, want %d", len(got), len(want))
	}
	for i, p := range want {
		if got[i] != p {
			t.Errorf("sparse holders[%d]: got %v, want %v", i, got[i], p)
		}
	}
}

// ── ByID ─────────────────────────────────────────────────────────────────────

func TestSeq_ByID_YieldsAll(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, travPos{X: 1})
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, travPos{X: 2})
		// unrelated entity: must not be yielded by ByID(posID)
		e3 := fw.NewEntity()
		flecs.Set(fw, e3, travVel{DX: 9})
	})

	visited := map[flecs.ID]bool{}
	w.Read(func(r *flecs.Reader) {
		for e := range flecs.ByID(r, posID) {
			visited[e] = true
		}
	})

	if !visited[e1] || !visited[e2] {
		t.Errorf("not all ByID holders visited: %v", visited)
	}
}

func TestSeq_ByID_BreakHonored(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)

	w.Write(func(fw *flecs.Writer) {
		for i := range 6 {
			e := fw.NewEntity()
			flecs.Set(fw, e, travPos{X: float32(i)})
		}
	})

	count := 0
	w.Read(func(r *flecs.Reader) {
		for range flecs.ByID(r, posID) {
			count++
			if count == 3 {
				break
			}
		}
	})
	if count != 3 {
		t.Errorf("expected break after 3 ByID entries, got %d", count)
	}
}

func TestSeq_ByID_Empty(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)

	called := false
	w.Read(func(r *flecs.Reader) {
		for range flecs.ByID(r, posID) {
			called = true
		}
	})
	if called {
		t.Error("expected no ByID entries for unset component")
	}
}

func TestSeq_ByID_MatchesEachByID(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)

	w.Write(func(fw *flecs.Writer) {
		for i := range 5 {
			e := fw.NewEntity()
			flecs.Set(fw, e, travPos{X: float32(i)})
		}
	})

	eachSet := map[flecs.ID]bool{}
	seqSet := map[flecs.ID]bool{}
	w.Read(func(r *flecs.Reader) {
		flecs.EachByID(r, posID, func(e flecs.ID, _ unsafe.Pointer) {
			eachSet[e] = true
		})
		for e := range flecs.ByID(r, posID) {
			seqSet[e] = true
		}
	})

	if len(eachSet) != len(seqSet) {
		t.Fatalf("ByID vs EachByID count mismatch: %d vs %d", len(seqSet), len(eachSet))
	}
	for e := range eachSet {
		if !seqSet[e] {
			t.Errorf("entity %v in EachByID but not ByID", e)
		}
	}
}

func TestSeq_ByID_DynamicComponent(t *testing.T) {
	w := flecs.New()
	var dynID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "test/TravDyn", 4, 4)
	})

	src := [4]byte{1, 2, 3, 4}
	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.SetIDPtr(fw, e1, dynID, unsafe.Pointer(&src[0]))
		e2 = fw.NewEntity()
		flecs.SetIDPtr(fw, e2, dynID, unsafe.Pointer(&src[0]))
		// create-and-delete to exercise the IsAlive filter
		dead := fw.NewEntity()
		flecs.SetIDPtr(fw, dead, dynID, unsafe.Pointer(&src[0]))
		fw.Delete(dead)
	})

	visited := map[flecs.ID]bool{}
	w.Read(func(r *flecs.Reader) {
		for e, ptr := range flecs.ByID(r, dynID) {
			if ptr == nil {
				t.Error("expected non-nil pointer for dynamic component")
			}
			visited[e] = true
		}
	})

	if !visited[e1] || !visited[e2] {
		t.Errorf("not all dynamic component holders visited: %v", visited)
	}
}

func TestSeq_ByID_SparseComponent(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)
	flecs.SetSparse(w, posID)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, travPos{X: 5})
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, travPos{X: 6})
	})

	visited := map[flecs.ID]bool{}
	var xs []float32
	w.Read(func(r *flecs.Reader) {
		for e, ptr := range flecs.ByID(r, posID) {
			visited[e] = true
			p := (*travPos)(ptr)
			xs = append(xs, p.X)
		}
	})

	if !visited[e1] || !visited[e2] {
		t.Errorf("sparse-policy ByID missing holders: %v", visited)
	}
	if len(xs) != 2 {
		t.Errorf("expected 2 sparse ByID entries, got %d", len(xs))
	}
}

// ── ByIDContext ──────────────────────────────────────────────────────────────

// TestSeq_ByIDContext_Canceled creates entities across >ctxCheckInterval tables
// so the periodic ctx check fires. Each entity gets a unique pair tag to force a
// unique archetype.
func TestSeq_ByIDContext_Canceled(t *testing.T) {
	const numTables = 1025 // exceeds ctxCheckInterval=1024
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)
	var relID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		relID = fw.NewEntity()
		for i := range numTables {
			tgt := fw.NewEntity()
			e := fw.NewEntity()
			flecs.Set(fw, e, travPos{X: float32(i)})
			flecs.AddID(fw, e, flecs.MakePair(relID, tgt))
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gotErr := false

	w.Read(func(r *flecs.Reader) {
		for id, err := range flecs.ByIDContext(ctx, r, posID) {
			if err != nil {
				gotErr = true
				if id != 0 {
					t.Errorf("on cancel expected id=0, got %v", id)
				}
				if !errors.Is(err, context.Canceled) {
					t.Errorf("expected context.Canceled, got %v", err)
				}
				break
			}
			cancel() // cancel on first entity; ctx check fires at table 1024
		}
	})

	if !gotErr {
		t.Error("expected context-cancel error to be yielded by ByIDContext")
	}
}

// ── Benchmark ────────────────────────────────────────────────────────────────

func BenchmarkEachEntity_vs_Entities(b *testing.B) {
	const N = 1000
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		for range N {
			fw.NewEntity()
		}
	})

	b.Run("EachEntity", func(b *testing.B) {
		for range b.N {
			w.Read(func(r *flecs.Reader) {
				r.EachEntity(func(_ flecs.ID) bool { return true })
			})
		}
	})

	b.Run("Entities", func(b *testing.B) {
		for range b.N {
			w.Read(func(r *flecs.Reader) {
				for range r.Entities() {
				}
			})
		}
	})
}

// ── coverage helpers: edge-case paths ────────────────────────────────────────

// TestSeq_Union_NoStore verifies that Union returns immediately (no panic) when
// the relationship has no union store (SetUnion was never called).
func TestSeq_Union_NoStore(t *testing.T) {
	w := flecs.New()
	var rel flecs.ID
	w.Write(func(fw *flecs.Writer) { rel = fw.NewEntity() })
	// No SetUnion — the union store map entry doesn't exist.
	called := false
	w.Read(func(r *flecs.Reader) {
		for range flecs.Union(r, rel) {
			called = true
		}
	})
	if called {
		t.Error("expected no union entries for non-union relationship")
	}
}

// TestSeq_Sparse_UnregisteredType verifies that Sparse returns immediately when
// the component type has never been registered.
func TestSeq_Sparse_UnregisteredType(t *testing.T) {
	type ghostType struct{ V int }
	w := flecs.New()
	called := false
	w.Read(func(r *flecs.Reader) {
		for range flecs.Sparse[ghostType](r) {
			called = true
		}
	})
	if called {
		t.Error("expected no entries for unregistered sparse type")
	}
}

// TestSeq_Sparse_NotSparse verifies that Sparse returns immediately when the
// component type is registered but not as Sparse (no sparse storage allocated).
func TestSeq_Sparse_NotSparse(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[travPos](w) // registered, but NOT SetSparse
	called := false
	w.Read(func(r *flecs.Reader) {
		for range flecs.Sparse[travPos](r) {
			called = true
		}
	})
	if called {
		t.Error("expected no entries when component is not sparse")
	}
}

// TestSeq_ByID_UnknownComponent verifies ByID returns immediately for
// a component ID that was never registered.
func TestSeq_ByID_UnknownComponent(t *testing.T) {
	w := flecs.New()
	// Use an arbitrary ID that hasn't been registered.
	fakeID := flecs.ID(99999)
	called := false
	w.Read(func(r *flecs.Reader) {
		for range flecs.ByID(r, fakeID) {
			called = true
		}
	})
	if called {
		t.Error("expected no entries for unknown component ID")
	}
}

// TestSeq_ByID_BreakHonored_SparseComponent verifies ByID break support on the
// sparse/DontFragment dense-store path (distinct from the archetype path).
func TestSeq_ByID_BreakHonored_SparseComponent(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)
	flecs.SetSparse(w, posID)

	w.Write(func(fw *flecs.Writer) {
		for i := range 5 {
			e := fw.NewEntity()
			flecs.Set(fw, e, travPos{X: float32(i)})
		}
	})

	count := 0
	w.Read(func(r *flecs.Reader) {
		for range flecs.ByID(r, posID) {
			count++
			if count == 2 {
				break
			}
		}
	})
	if count != 2 {
		t.Errorf("sparse ByID break: expected 2, got %d", count)
	}
}

// TestSeq_ByIDContext_PreCanceled verifies that ByIDContext with a pre-canceled
// context immediately yields the ctx error without visiting any entities.
func TestSeq_ByIDContext_PreCanceled(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, travPos{X: 1})
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var seenID flecs.ID
	var seenErr error
	w.Read(func(r *flecs.Reader) {
		for id, err := range flecs.ByIDContext(ctx, r, posID) {
			seenID = id
			seenErr = err
			break
		}
	})

	if seenID != 0 {
		t.Errorf("pre-canceled ByIDContext: expected id=0, got %v", seenID)
	}
	if !errors.Is(seenErr, context.Canceled) {
		t.Errorf("pre-canceled ByIDContext: expected context.Canceled, got %v", seenErr)
	}
}

// TestSeq_ByIDContext_SparseComponent verifies ByIDContext works with a sparse
// component, exercising the dense-store path in the ctx variant.
func TestSeq_ByIDContext_SparseComponent(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)
	flecs.SetSparse(w, posID)

	const N = 3
	w.Write(func(fw *flecs.Writer) {
		for i := range N {
			e := fw.NewEntity()
			flecs.Set(fw, e, travPos{X: float32(i)})
		}
	})

	count := 0
	w.Read(func(r *flecs.Reader) {
		for _, err := range flecs.ByIDContext(context.Background(), r, posID) {
			if err != nil {
				break
			}
			count++
		}
	})
	if count != N {
		t.Errorf("ByIDContext sparse: expected %d entities, got %d", N, count)
	}
}

// TestSeq_ByIDContext_BreakHonored verifies that breaking from ByIDContext stops
// iteration on the archetype path.
func TestSeq_ByIDContext_BreakHonored(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[travPos](w)

	w.Write(func(fw *flecs.Writer) {
		for i := range 6 {
			e := fw.NewEntity()
			flecs.Set(fw, e, travPos{X: float32(i)})
		}
	})

	count := 0
	w.Read(func(r *flecs.Reader) {
		for _, err := range flecs.ByIDContext(context.Background(), r, posID) {
			if err != nil {
				break
			}
			count++
			if count == 3 {
				break
			}
		}
	})
	if count != 3 {
		t.Errorf("ByIDContext break: expected 3, got %d", count)
	}
}
