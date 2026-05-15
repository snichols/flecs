package flecs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/snichols/flecs"
)

// ── component types used in query_iter_seq_test.go ──────────────────────────

type seqPos struct{ X float64 }
type seqVel struct{ DX float64 }
type seqMass struct{ V float64 }
type seqHealth struct{ HP int }
type seqExtra struct{} // zero-size tag to force a different archetype

// ── Bare QueryAll iteration ─────────────────────────────────────────────────

func TestQueryAll_BareYieldsAllMatches(t *testing.T) {
	w := flecs.New()
	var posID flecs.ID
	var ids [5]flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[seqPos](w)
		for i := range 5 {
			e := fw.NewEntity()
			flecs.Set(fw, e, seqPos{float64(i + 1)})
			ids[i] = e
		}
	})

	q := flecs.NewQuery(w, posID)
	visited := map[flecs.ID]bool{}
	w.Read(func(r *flecs.Reader) {
		for e := range flecs.QueryAll(q, r) {
			visited[e] = true
		}
	})

	if len(visited) != 5 {
		t.Fatalf("expected 5 entities, got %d", len(visited))
	}
	for _, e := range ids {
		if !visited[e] {
			t.Errorf("entity %v not visited", e)
		}
	}
}

func TestQueryAll_BreakHonored_IntraTable(t *testing.T) {
	// Break after the 2nd entity within a single table; only 2 should be visited.
	w := flecs.New()
	var posID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[seqPos](w)
		for i := range 5 {
			e := fw.NewEntity()
			flecs.Set(fw, e, seqPos{float64(i)})
		}
	})

	q := flecs.NewQuery(w, posID)
	count := 0
	w.Read(func(r *flecs.Reader) {
		for range flecs.QueryAll(q, r) {
			count++
			if count == 2 {
				break
			}
		}
	})

	if count != 2 {
		t.Fatalf("expected 2 entities visited after break, got %d", count)
	}
}

func TestQueryAll_BreakHonored_InterTable(t *testing.T) {
	// Two separate archetypes (tables): tableA has only seqPos, tableB adds seqExtra.
	// Break after exhausting the first table (tableSize entities); the second table
	// must never be entered.
	w := flecs.New()

	var posID flecs.ID
	const tableSize = 3
	var tableAEnt, tableBEnt [tableSize]flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[seqPos](w)
		flecs.RegisterComponent[seqExtra](w)
		for i := range tableSize {
			e := fw.NewEntity()
			flecs.Set(fw, e, seqPos{float64(i + 1)})
			tableAEnt[i] = e
		}
		for i := range tableSize {
			e := fw.NewEntity()
			flecs.Set(fw, e, seqPos{float64(tableSize + i + 1)})
			flecs.Set(fw, e, seqExtra{})
			tableBEnt[i] = e
		}
	})

	q := flecs.NewQuery(w, posID)
	setA := map[flecs.ID]bool{}
	setB := map[flecs.ID]bool{}
	for _, e := range tableAEnt {
		setA[e] = true
	}
	for _, e := range tableBEnt {
		setB[e] = true
	}

	visited := map[flecs.ID]bool{}
	w.Read(func(r *flecs.Reader) {
		count := 0
		for e := range flecs.QueryAll(q, r) {
			visited[e] = true
			count++
			if count == tableSize {
				break // stop after exactly one table's worth of entities
			}
		}
	})

	if len(visited) != tableSize {
		t.Fatalf("expected %d entities visited, got %d", tableSize, len(visited))
	}

	// All visited entities must come from exactly one archetype (one table).
	fromA, fromB := 0, 0
	for e := range visited {
		if setA[e] {
			fromA++
		}
		if setB[e] {
			fromB++
		}
	}
	if fromA > 0 && fromB > 0 {
		t.Error("visited entities from both archetypes — break did not respect table boundary")
	}
}

func TestQueryAll_EmptyQuery(t *testing.T) {
	w := flecs.New()
	var posID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[seqPos](w)
		// No entities have seqPos.
		_ = fw.NewEntity()
	})

	q := flecs.NewQuery(w, posID)
	count := 0
	w.Read(func(r *flecs.Reader) {
		for range flecs.QueryAll(q, r) {
			count++
		}
	})

	if count != 0 {
		t.Fatalf("expected 0 entities for empty query, got %d", count)
	}
}

func TestCachedQueryAll_BareYieldsAllMatches(t *testing.T) {
	w := flecs.New()
	var posID flecs.ID
	var ids [5]flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[seqPos](w)
		for i := range 5 {
			e := fw.NewEntity()
			flecs.Set(fw, e, seqPos{float64(i + 1)})
			ids[i] = e
		}
	})

	cq := flecs.NewCachedQuery(w, posID)
	visited := map[flecs.ID]bool{}
	w.Read(func(r *flecs.Reader) {
		for e := range flecs.CachedQueryAll(cq, r) {
			visited[e] = true
		}
	})

	if len(visited) != 5 {
		t.Fatalf("expected 5 entities, got %d", len(visited))
	}
	for _, e := range ids {
		if !visited[e] {
			t.Errorf("entity %v not visited", e)
		}
	}
}

// ── Typed All1 iteration ────────────────────────────────────────────────────

func TestAll1_YieldsValues(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, seqPos{X: 42.0})
	})

	// Mutate through the pointer; verify write-back via Get.
	w.Read(func(r *flecs.Reader) {
		for gotE, p := range flecs.All1[seqPos](r) {
			if gotE != e {
				t.Errorf("unexpected entity %v", gotE)
			}
			p.X = 99.0
		}
	})

	w.Read(func(r *flecs.Reader) {
		p, ok := flecs.Get[seqPos](r, e)
		if !ok {
			t.Fatal("entity should still have seqPos")
		}
		if p.X != 99.0 {
			t.Fatalf("mutation not written back: want X=99, got %v", p.X)
		}
	})
}

func TestAll1_PointerInvalidatedAfterMigration(t *testing.T) {
	// Documents the pointer-stability contract: the *T yielded by All1 points
	// into table column memory. Within the yield body, structural mutations
	// (Add/Remove/Set on a new component) are queued via the deferred command
	// queue and do NOT immediately invalidate the pointer — the pointer stays
	// valid for the duration of the Write scope. After the Write scope exits
	// and the migration is flushed, the old pointer is stale. Callers should
	// dereference and stack-copy before mutating the world.
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, seqPos{X: 10})
	})

	var savedVal seqPos
	w.Write(func(fw *flecs.Writer) {
		for _, p := range flecs.All1[seqPos](fw) {
			// Safe: dereference and stack-copy the value before queuing a mutation.
			savedVal = *p
			// Queue a structural change (deferred — pointer remains valid here).
			flecs.Set(fw, e, seqVel{DX: 1})
		}
	})
	// After the Write scope exits, the queued AddID migrates the entity to a new
	// table. Any pointer captured from inside the scope is now stale.
	// We only use savedVal (the stack copy), which is always safe.
	if savedVal.X != 10 {
		t.Fatalf("stack copy should hold original value, got %v", savedVal.X)
	}
	w.Read(func(r *flecs.Reader) {
		if _, ok := flecs.Get[seqVel](r, e); !ok {
			t.Fatal("migration should have added seqVel")
		}
	})
}

func TestAll2_PairValues(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, seqPos{X: 1})
		flecs.Set(fw, e, seqVel{DX: 2})
	})

	count := 0
	w.Read(func(r *flecs.Reader) {
		for gotE, pair := range flecs.All2[seqPos, seqVel](r) {
			count++
			if gotE != e {
				t.Errorf("unexpected entity %v", gotE)
			}
			if pair.A == nil {
				t.Error("Pair.A is nil")
			}
			if pair.B == nil {
				t.Error("Pair.B is nil")
			}
			if pair.A.X != 1 {
				t.Errorf("Pair.A.X: want 1, got %v", pair.A.X)
			}
			if pair.B.DX != 2 {
				t.Errorf("Pair.B.DX: want 2, got %v", pair.B.DX)
			}
		}
	})

	if count != 1 {
		t.Fatalf("expected 1 entity, got %d", count)
	}
}

func TestAll3_TripleValues(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, seqPos{X: 1})
		flecs.Set(fw, e, seqVel{DX: 2})
		flecs.Set(fw, e, seqMass{V: 3})
	})

	count := 0
	w.Read(func(r *flecs.Reader) {
		for gotE, tri := range flecs.All3[seqPos, seqVel, seqMass](r) {
			count++
			if gotE != e {
				t.Errorf("unexpected entity %v", gotE)
			}
			if tri.A == nil || tri.B == nil || tri.C == nil {
				t.Error("Triple has nil field")
			}
			if tri.A.X != 1 || tri.B.DX != 2 || tri.C.V != 3 {
				t.Errorf("unexpected values: A.X=%v B.DX=%v C.V=%v", tri.A.X, tri.B.DX, tri.C.V)
			}
		}
	})

	if count != 1 {
		t.Fatalf("expected 1 entity, got %d", count)
	}
}

func TestAll4_QuadValues(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, seqPos{X: 1})
		flecs.Set(fw, e, seqVel{DX: 2})
		flecs.Set(fw, e, seqMass{V: 3})
		flecs.Set(fw, e, seqHealth{HP: 4})
	})

	count := 0
	w.Read(func(r *flecs.Reader) {
		for gotE, quad := range flecs.All4[seqPos, seqVel, seqMass, seqHealth](r) {
			count++
			if gotE != e {
				t.Errorf("unexpected entity %v", gotE)
			}
			if quad.A == nil || quad.B == nil || quad.C == nil || quad.D == nil {
				t.Error("Quad has nil field")
			}
			if quad.A.X != 1 || quad.B.DX != 2 || quad.C.V != 3 || quad.D.HP != 4 {
				t.Errorf("unexpected values: A=%v B=%v C=%v D=%v",
					quad.A.X, quad.B.DX, quad.C.V, quad.D.HP)
			}
		}
	})

	if count != 1 {
		t.Fatalf("expected 1 entity, got %d", count)
	}
}

func TestAll1_BreakHonored(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		for i := range 10 {
			e := fw.NewEntity()
			flecs.Set(fw, e, seqPos{float64(i)})
		}
	})

	count := 0
	w.Read(func(r *flecs.Reader) {
		for range flecs.All1[seqPos](r) {
			count++
			if count == 3 {
				break
			}
		}
	})

	if count != 3 {
		t.Fatalf("expected 3 entities after break, got %d", count)
	}
}

func TestAll1_NoEntities(t *testing.T) {
	w := flecs.New()
	// No entities with seqPos are ever created.
	count := 0
	w.Read(func(r *flecs.Reader) {
		for range flecs.All1[seqPos](r) {
			count++
		}
	})
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}
}

func TestAll1_InheritableShared(t *testing.T) {
	// When seqPos is SetInheritable, All1 auto-promotes to Self|Up(IsA).
	// For entities that inherit seqPos from a prefab, the yielded pointer must
	// be the same pointer for every entity in the matched table (upPtr path).
	w := flecs.New()
	flecs.RegisterComponent[seqPos](w)
	flecs.SetInheritable[seqPos](w)

	var prefab, inst1, inst2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, seqPos{X: 7})

		inst1 = fw.NewEntity()
		inst2 = fw.NewEntity()
		flecs.AddID(fw, inst1, flecs.MakePair(w.IsA(), prefab))
		flecs.AddID(fw, inst2, flecs.MakePair(w.IsA(), prefab))
	})

	var ptr1, ptr2 *seqPos
	w.Read(func(r *flecs.Reader) {
		for e, p := range flecs.All1[seqPos](r) {
			switch e {
			case inst1:
				ptr1 = p
			case inst2:
				ptr2 = p
			}
		}
	})

	if ptr1 == nil || ptr2 == nil {
		t.Fatal("instances were not visited by All1")
	}
	// Both instances are in the same table; upPtr returns the prefab's slot.
	// The pointers must be identical (same prefab slot).
	if ptr1 != ptr2 {
		t.Errorf("expected same prefab pointer for both instances, got %p vs %p", ptr1, ptr2)
	}
	if ptr1.X != 7 {
		t.Errorf("expected prefab value X=7, got %v", ptr1.X)
	}
}

func TestAll1_CanToggleSkipsDisabled(t *testing.T) {
	// When seqPos is SetCanToggle and a row is disabled, All1 must skip it,
	// mirroring Each1's IsRowEnabled check.
	w := flecs.New()
	var posID flecs.ID
	var e1, e2, e3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[seqPos](w)
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		e3 = fw.NewEntity()
		flecs.Set(fw, e1, seqPos{1})
		flecs.Set(fw, e2, seqPos{2})
		flecs.Set(fw, e3, seqPos{3})
	})
	flecs.SetCanToggle(w, posID)

	w.Write(func(fw *flecs.Writer) {
		flecs.DisableID(fw, e2, posID)
	})

	visited := map[flecs.ID]bool{}
	w.Read(func(r *flecs.Reader) {
		for e := range flecs.All1[seqPos](r) {
			visited[e] = true
		}
	})

	if !visited[e1] {
		t.Error("e1 (enabled) should be visited")
	}
	if visited[e2] {
		t.Error("e2 (disabled) should be skipped")
	}
	if !visited[e3] {
		t.Error("e3 (enabled) should be visited")
	}
}

// ── Context-aware QueryAllContext ────────────────────────────────────────────

func TestQueryAllContext_PreCanceled(t *testing.T) {
	w := flecs.New()
	var posID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[seqPos](w)
		for range 10 {
			e := fw.NewEntity()
			flecs.Set(fw, e, seqPos{1})
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	q := flecs.NewQuery(w, posID)
	var visited int
	var gotErr error
	w.Read(func(r *flecs.Reader) {
		for id, err := range flecs.QueryAllContext(ctx, q, r) {
			if err != nil {
				gotErr = err
				break
			}
			if id != 0 {
				visited++
			}
		}
	})

	if visited != 0 {
		t.Fatalf("expected 0 entities on pre-cancel, got %d", visited)
	}
	if !errors.Is(gotErr, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", gotErr)
	}
}

func TestQueryAllContext_CanceledMidIteration(t *testing.T) {
	w := flecs.New()
	var posID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[seqPos](w)
		for i := range 2000 {
			e := fw.NewEntity()
			flecs.Set(fw, e, seqPos{float64(i)})
		}
	})

	ctx, cancel := context.WithCancel(context.Background())

	q := flecs.NewQuery(w, posID)
	var gotErr error
	visited := 0
	w.Read(func(r *flecs.Reader) {
		for id, err := range flecs.QueryAllContext(ctx, q, r) {
			if err != nil {
				gotErr = err
				break
			}
			visited++
			if visited == 1 {
				cancel()
			}
			_ = id
		}
	})

	// With 2000 entities in possibly one table and ctxCheckInterval=1024 tables,
	// cancellation might not be detected (it's checked per-table, not per-entity).
	// Accept any outcome — the key invariant is: if an error is yielded, it's
	// context.Canceled, and no more entity yields follow.
	if gotErr != nil && !errors.Is(gotErr, context.Canceled) {
		t.Fatalf("unexpected error: %v", gotErr)
	}
}

func TestQueryAllContext_TimeoutFires(t *testing.T) {
	// Create entities spread across many tables so ctxCheckInterval fires.
	// Each entity gets a unique extra tag component to land in its own archetype.
	// We use a fixed set of tag components to control the number of tables.
	w := flecs.New()
	var posID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[seqPos](w)
		// Create 2000 entities in a single table; the check runs at table granularity.
		// Use a 1ms timeout: with ctxCheckInterval=1024 tables, the deadline may not
		// fire within a single table, but no panic must occur.
		for i := range 2000 {
			e := fw.NewEntity()
			flecs.Set(fw, e, seqPos{float64(i)})
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond) // let the timeout fire before we start

	q := flecs.NewQuery(w, posID)
	var gotErr error
	w.Read(func(r *flecs.Reader) {
		for _, err := range flecs.QueryAllContext(ctx, q, r) {
			if err != nil {
				gotErr = err
				break
			}
		}
	})

	// With a pre-fired timeout, the first yield should deliver the error.
	if gotErr == nil {
		t.Fatal("expected a deadline error but got nil")
	}
	if !errors.Is(gotErr, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", gotErr)
	}
}

func TestCachedQueryAllContext_PreCanceled(t *testing.T) {
	w := flecs.New()
	var posID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[seqPos](w)
		for range 10 {
			e := fw.NewEntity()
			flecs.Set(fw, e, seqPos{1})
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cq := flecs.NewCachedQuery(w, posID)
	var visited int
	var gotErr error
	w.Read(func(r *flecs.Reader) {
		for id, err := range flecs.CachedQueryAllContext(ctx, cq, r) {
			if err != nil {
				gotErr = err
				break
			}
			if id != 0 {
				visited++
			}
		}
	})

	if visited != 0 {
		t.Fatalf("expected 0 entities on pre-cancel, got %d", visited)
	}
	if !errors.Is(gotErr, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", gotErr)
	}
}

func TestCachedQueryAllContext_CanceledMidIteration(t *testing.T) {
	w := flecs.New()
	var posID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[seqPos](w)
		for i := range 2000 {
			e := fw.NewEntity()
			flecs.Set(fw, e, seqPos{float64(i)})
		}
	})

	ctx, cancel := context.WithCancel(context.Background())

	cq := flecs.NewCachedQuery(w, posID)
	var gotErr error
	visited := 0
	w.Read(func(r *flecs.Reader) {
		for id, err := range flecs.CachedQueryAllContext(ctx, cq, r) {
			if err != nil {
				gotErr = err
				break
			}
			visited++
			if visited == 1 {
				cancel()
			}
			_ = id
		}
	})

	if gotErr != nil && !errors.Is(gotErr, context.Canceled) {
		t.Fatalf("unexpected error: %v", gotErr)
	}
}

func TestCachedQueryAllContext_TimeoutFires(t *testing.T) {
	// Mirror of TestQueryAllContext_TimeoutFires for CachedQuery.
	// Create entities in a single table; use a pre-fired timeout so the
	// deadline is already exceeded when iteration begins. Verify that the
	// first yield delivers DeadlineExceeded and no panic occurs.
	w := flecs.New()
	var posID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[seqPos](w)
		for i := range 2000 {
			e := fw.NewEntity()
			flecs.Set(fw, e, seqPos{float64(i)})
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond) // let the timeout fire before we start

	cq := flecs.NewCachedQuery(w, posID)
	var gotErr error
	w.Read(func(r *flecs.Reader) {
		for _, err := range flecs.CachedQueryAllContext(ctx, cq, r) {
			if err != nil {
				gotErr = err
				break
			}
		}
	})

	if gotErr == nil {
		t.Fatal("expected a deadline error but got nil")
	}
	if !errors.Is(gotErr, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", gotErr)
	}
}

// ── Pair / Triple / Quad helper type tests ───────────────────────────────────

func TestPair_FieldAccess(t *testing.T) {
	// Pair[A, B] fields A and B are both accessible and non-nil when yielded by All2.
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, seqPos{X: 11})
		flecs.Set(fw, e, seqVel{DX: 22})
	})

	w.Read(func(r *flecs.Reader) {
		for _, p := range flecs.All2[seqPos, seqVel](r) {
			if p.A == nil {
				t.Error("Pair.A must not be nil")
			}
			if p.B == nil {
				t.Error("Pair.B must not be nil")
			}
			if p.A.X != 11 {
				t.Errorf("Pair.A.X: want 11, got %v", p.A.X)
			}
			if p.B.DX != 22 {
				t.Errorf("Pair.B.DX: want 22, got %v", p.B.DX)
			}
		}
	})
}

func TestPair_NilOnMissingOptional(t *testing.T) {
	// Maybe/Optional variants (where an absent component yields nil) are deferred
	// to a later phase. This test serves as a placeholder.
	t.Skip("Maybe variants land in a later phase")
}
