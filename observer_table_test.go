package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// Component types used in table-observer tests.
type tcPos struct{ X, Y float32 }
type tcVel struct{ X, Y float32 }
type tcMass struct{ V float32 }
type tcTag struct{} // zero-size tag component

// ── Test 1: Basic create ──────────────────────────────────────────────────────

// TestOnTableCreate_BasicCreate: registering OnTableCreate before entity
// creation; first entity with a novel (Position+Velocity) combo fires handler
// once with a table whose Type() contains both component IDs.
func TestOnTableCreate_BasicCreate(t *testing.T) {
	w := flecs.New()
	var posID, velID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[tcPos](w)
		velID = flecs.RegisterComponent[tcVel](w)
	})

	var seen []*flecs.Table
	w.Write(func(fw *flecs.Writer) {
		flecs.OnTableCreate(w, func(fw *flecs.Writer, t *flecs.Table) {
			seen = append(seen, t)
		})
	})

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, tcPos{X: 1, Y: 2})
		flecs.Set(fw, e, tcVel{X: 3, Y: 4})
	})

	if len(seen) != 1 {
		t.Fatalf("expected 1 table-create event, got %d", len(seen))
	}
	typ := seen[0].Type()
	hasPos, hasVel := false, false
	for _, id := range typ {
		if id == posID {
			hasPos = true
		}
		if id == velID {
			hasVel = true
		}
	}
	if !hasPos || !hasVel {
		t.Fatalf("table type %v does not contain both posID and velID", typ)
	}
}

// ── Test 2: De-duplication ────────────────────────────────────────────────────

// TestOnTableCreate_Deduplication: a second entity placed in the same archetype
// does NOT fire the handler again.
func TestOnTableCreate_Deduplication(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterComponent[tcPos](w)
	})

	var count int
	w.Write(func(fw *flecs.Writer) {
		flecs.OnTableCreate(w, func(fw *flecs.Writer, t *flecs.Table) {
			count++
		})
	})

	w.Write(func(fw *flecs.Writer) {
		e1 := fw.NewEntity()
		flecs.Set(fw, e1, tcPos{X: 1, Y: 2})
	})
	w.Write(func(fw *flecs.Writer) {
		e2 := fw.NewEntity()
		flecs.Set(fw, e2, tcPos{X: 3, Y: 4}) // same archetype
	})

	if count != 1 {
		t.Fatalf("expected 1 table-create event (de-duplication), got %d", count)
	}
}

// ── Test 3: Distinct archetypes ───────────────────────────────────────────────

// TestOnTableCreate_DistinctArchetypes: two distinct novel component combos each
// fire the handler once, for a total of 2.
func TestOnTableCreate_DistinctArchetypes(t *testing.T) {
	w := flecs.New()

	var count int
	var tables []*flecs.Table
	w.Write(func(fw *flecs.Writer) {
		flecs.OnTableCreate(w, func(fw *flecs.Writer, t *flecs.Table) {
			count++
			tables = append(tables, t)
		})
	})

	w.Write(func(fw *flecs.Writer) {
		e1 := fw.NewEntity()
		flecs.Set(fw, e1, tcPos{X: 1, Y: 2})
	})
	w.Write(func(fw *flecs.Writer) {
		e2 := fw.NewEntity()
		flecs.Set(fw, e2, tcVel{X: 3, Y: 4}) // different signature
	})

	if count != 2 {
		t.Fatalf("expected 2 table-create events, got %d", count)
	}
	if tables[0] == tables[1] {
		t.Fatal("both events reported the same table pointer; expected distinct tables")
	}
}

// ── Test 4: Migration ─────────────────────────────────────────────────────────

// TestOnTableCreate_Migration: adding a component to an entity triggers
// migration to a new table; the handler fires once for the new table.
func TestOnTableCreate_Migration(t *testing.T) {
	w := flecs.New()

	// Pre-create the [Position] table so only the [Position+Velocity] migration
	// table is novel when the observer is registered.
	var posID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		posID = flecs.RegisterComponent[tcPos](w)
		_ = posID
		e := fw.NewEntity()
		flecs.Set(fw, e, tcPos{X: 0, Y: 0})
	})

	var count int
	var seen []*flecs.Table
	w.Write(func(fw *flecs.Writer) {
		flecs.OnTableCreate(w, func(fw *flecs.Writer, t *flecs.Table) {
			count++
			seen = append(seen, t)
		})
	})

	// Now add Velocity to a fresh entity that starts with only Position.
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, tcPos{X: 1, Y: 2})
		flecs.Set(fw, e, tcVel{X: 3, Y: 4}) // migration: [Pos] → [Pos,Vel]
	})

	if count != 1 {
		t.Fatalf("expected 1 table-create event for migration table, got %d", count)
	}
	// Verify the table contains both Position and Velocity.
	velID := flecs.RegisterComponent[tcVel](w)
	hasVel := false
	for _, id := range seen[0].Type() {
		if id == velID {
			hasVel = true
		}
	}
	if !hasVel {
		t.Fatalf("migration table type %v does not include Velocity", seen[0].Type())
	}
}

// ── Test 5: Empty/root table NOT fired ───────────────────────────────────────

// TestOnTableCreate_EmptyTableNotFired: the world's initial empty table (created
// at world construction) does not trigger OnTableCreate, matching upstream's
// is_root suppression at table.c:1278.
func TestOnTableCreate_EmptyTableNotFired(t *testing.T) {
	w := flecs.New()

	var count int
	// Register the observer right after world construction, before any user
	// entities or components are created.
	w.Write(func(fw *flecs.Writer) {
		flecs.OnTableCreate(w, func(fw *flecs.Writer, t *flecs.Table) {
			count++
		})
	})

	// No entity creation here; the handler should not have fired from the world's
	// internal empty table construction.
	if count != 0 {
		t.Fatalf("handler fired %d time(s) for the empty root table; expected 0", count)
	}
}

// ── Test 6: Disabled observer ─────────────────────────────────────────────────

// TestOnTableCreate_DisabledObserver: SetEnabled(false) suppresses the handler;
// re-enabling resumes it.
func TestOnTableCreate_DisabledObserver(t *testing.T) {
	w := flecs.New()

	var count int
	var obs *flecs.Observer
	w.Write(func(fw *flecs.Writer) {
		obs = flecs.OnTableCreate(w, func(fw *flecs.Writer, t *flecs.Table) {
			count++
		})
	})

	// Disable the observer then create a novel archetype — handler must NOT fire.
	obs.SetEnabled(false)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, tcPos{X: 1, Y: 2})
	})
	if count != 0 {
		t.Fatalf("handler fired %d time(s) while disabled; expected 0", count)
	}

	// Re-enable and create a different novel archetype — handler MUST fire.
	obs.SetEnabled(true)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, tcVel{X: 3, Y: 4}) // novel signature
	})
	if count != 1 {
		t.Fatalf("expected 1 invocation after re-enable, got %d", count)
	}
}

// ── Test 7: yield_existing ────────────────────────────────────────────────────

// TestOnTableCreate_YieldExisting: pre-populate with N distinct archetypes;
// OnTableCreateWithOptions + WithYieldExisting() fires N times synchronously at
// registration, once per existing non-empty table. Ordering is sorted-signature
// order (deterministic within a run).
func TestOnTableCreate_YieldExisting(t *testing.T) {
	w := flecs.New()
	const N = 3

	// Pre-populate N distinct archetypes.
	w.Write(func(fw *flecs.Writer) {
		e1 := fw.NewEntity()
		flecs.Set(fw, e1, tcPos{})
		e2 := fw.NewEntity()
		flecs.Set(fw, e2, tcVel{})
		e3 := fw.NewEntity()
		flecs.Set(fw, e3, tcMass{})
	})

	var yielded []*flecs.Table
	w.Write(func(fw *flecs.Writer) {
		flecs.OnTableCreateWithOptions(w, flecs.WithYieldExisting(), func(fw *flecs.Writer, t *flecs.Table) {
			yielded = append(yielded, t)
		})
	})

	if len(yielded) != N {
		t.Fatalf("expected %d yield invocations, got %d", N, len(yielded))
	}
	// Verify all yielded tables are distinct.
	seen := make(map[*flecs.Table]bool)
	for _, tbl := range yielded {
		if seen[tbl] {
			t.Fatal("yield delivered the same table pointer twice")
		}
		seen[tbl] = true
	}
}

// ── Test 8: Re-entry safety ───────────────────────────────────────────────────

// TestOnTableCreate_ReEntrySafety: when the handler creates an entity (via fw)
// that causes a NEW novel archetype, the second OnTableCreate fires (via the
// deferred coalescer after the outer flush), there is no panic, and world state
// is consistent.
func TestOnTableCreate_ReEntrySafety(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[tcPos](w)
	flecs.RegisterComponent[tcTag](w)

	var tableTypes [][]flecs.ID
	w.Write(func(fw *flecs.Writer) {
		flecs.OnTableCreate(w, func(fw *flecs.Writer, t *flecs.Table) {
			tableTypes = append(tableTypes, t.Type())
			// When the [tcPos] table is created, create a new entity with tcTag
			// to trigger a second novel table. This command is deferred.
			if len(t.Type()) == 1 {
				e2 := fw.NewEntity()
				flecs.Set(fw, e2, tcTag{})
			}
		})
	})

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, tcPos{X: 1, Y: 2}) // creates [tcPos] table
	})

	// After the outer Write scope flushes, the deferred entity with tcTag is
	// committed, which creates the [tcTag] table and fires the handler again.
	if len(tableTypes) != 2 {
		t.Fatalf("expected 2 table-create events (re-entry path), got %d", len(tableTypes))
	}
}

// ── Test 9: Multiple observers ────────────────────────────────────────────────

// TestOnTableCreate_MultipleObservers: two OnTableCreate handlers both fire in
// registration order for a single table creation.
func TestOnTableCreate_MultipleObservers(t *testing.T) {
	w := flecs.New()

	var order []int
	w.Write(func(fw *flecs.Writer) {
		flecs.OnTableCreate(w, func(fw *flecs.Writer, t *flecs.Table) {
			order = append(order, 1)
		})
		flecs.OnTableCreate(w, func(fw *flecs.Writer, t *flecs.Table) {
			order = append(order, 2)
		})
	})

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, tcPos{X: 1, Y: 2})
	})

	if len(order) != 2 {
		t.Fatalf("expected 2 invocations (one per observer), got %d", len(order))
	}
	if order[0] != 1 || order[1] != 2 {
		t.Fatalf("expected fire order [1,2], got %v", order)
	}
}

// ── Test 10: Unsubscribe stops delivery ───────────────────────────────────────

// TestOnTableCreate_Unsubscribe: after Unsubscribe(), the handler no longer fires.
func TestOnTableCreate_Unsubscribe(t *testing.T) {
	w := flecs.New()

	var count int
	var obs *flecs.Observer
	w.Write(func(fw *flecs.Writer) {
		obs = flecs.OnTableCreate(w, func(fw *flecs.Writer, t *flecs.Table) {
			count++
		})
	})

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, tcPos{X: 1, Y: 2})
	})
	if count != 1 {
		t.Fatalf("expected 1 invocation before Unsubscribe, got %d", count)
	}

	obs.Unsubscribe()
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, tcVel{X: 3, Y: 4}) // novel table
	})
	if count != 1 {
		t.Fatalf("expected no invocation after Unsubscribe, count is %d", count)
	}
}

// ── Test 11: yield_existing does not fire for future tables ───────────────────

// TestOnTableCreate_YieldExistingFutureStillFires: after the yield sweep, the
// same observer continues to receive events for tables created after registration.
func TestOnTableCreate_YieldExistingFutureStillFires(t *testing.T) {
	w := flecs.New()

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, tcPos{}) // existing table
	})

	var yieldCount, futureCount int
	posID := flecs.RegisterComponent[tcPos](w)
	_ = posID

	w.Write(func(fw *flecs.Writer) {
		flecs.OnTableCreateWithOptions(w, flecs.WithYieldExisting(), func(fw *flecs.Writer, t *flecs.Table) {
			if len(t.Type()) == 1 {
				yieldCount++ // the pre-existing [tcPos] table
			} else {
				futureCount++
			}
		})
	})

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, tcPos{})
		flecs.Set(fw, e, tcVel{}) // novel table
	})

	if yieldCount != 1 {
		t.Fatalf("expected 1 yield invocation for existing table, got %d", yieldCount)
	}
	if futureCount != 1 {
		t.Fatalf("expected 1 future invocation for post-registration table, got %d", futureCount)
	}
}
