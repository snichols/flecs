package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// ── snapshot component types ──────────────────────────────────────────────────

type snapPos struct{ X, Y float32 }
type snapVel struct{ DX, DY float32 }
type snapMass struct{ V float32 }

// ── Test 1: 100-entity world round-trip ──────────────────────────────────────

func TestSnapshot_HundredEntityRoundTrip(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[snapPos](w)

	var entities []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < 100; i++ {
			e := fw.NewEntity()
			entities = append(entities, e)
			flecs.Set(fw, e, snapPos{X: float32(i), Y: float32(i * 2)})
		}
	})

	var countBefore int
	w.Read(func(r *flecs.Reader) { countBefore = r.Count() })

	s := flecs.TakeSnapshot(w)

	// Delete all user entities.
	for _, e := range entities {
		w.Delete(e)
	}

	flecs.RestoreSnapshot(w, s)

	var countAfter int
	w.Read(func(r *flecs.Reader) { countAfter = r.Count() })
	if countAfter != countBefore {
		t.Errorf("entity count after restore: want %d, got %d", countBefore, countAfter)
	}

	// Verify per-entity component state.
	w.Read(func(r *flecs.Reader) {
		for i, e := range entities {
			if !r.IsAlive(e) {
				t.Errorf("entity[%d] not alive after restore", i)
				continue
			}
			got, ok := flecs.Get[snapPos](r, e)
			if !ok {
				t.Errorf("entity[%d]: snapPos missing after restore", i)
				continue
			}
			want := snapPos{X: float32(i), Y: float32(i * 2)}
			if got != want {
				t.Errorf("entity[%d]: snapPos want %v, got %v", i, want, got)
			}
		}
	})
	_ = posID
}

// ── Test 2: Identity preservation ────────────────────────────────────────────

func TestSnapshot_IdentityPreservation(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[snapPos](w)

	// Create 50 entities; pick the 42nd (index 41).
	entities := make([]flecs.ID, 50)
	w.Write(func(fw *flecs.Writer) {
		for i := range entities {
			entities[i] = fw.NewEntity()
			flecs.Set(fw, entities[i], snapPos{X: float32(i), Y: float32(i)})
		}
	})
	e42 := entities[41]

	s := flecs.TakeSnapshot(w)

	// Mutate: delete first 25 entities.
	for i := 0; i < 25; i++ {
		w.Delete(entities[i])
	}

	flecs.RestoreSnapshot(w, s)

	if !w.IsAlive(e42) {
		t.Errorf("entity at slot 41 (ID %v) not alive after restore", e42)
	}

	w.Read(func(r *flecs.Reader) {
		got, ok := flecs.Get[snapPos](r, e42)
		if !ok {
			t.Error("entity 42: snapPos missing after restore")
			return
		}
		want := snapPos{X: 41, Y: 41}
		if got != want {
			t.Errorf("entity 42: want %v, got %v", want, got)
		}
	})
	_ = posID
}

// ── Test 3: Round-trip via Bytes / LoadSnapshot ───────────────────────────────

func TestSnapshot_BytesRoundTrip(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[snapPos](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, snapPos{X: 7, Y: 13})
	})

	s1 := flecs.TakeSnapshot(w)
	b := s1.Bytes()

	s2, err := flecs.LoadSnapshot(b)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}

	// Mutate, then restore from disk bytes.
	w.Delete(e)

	flecs.RestoreSnapshot(w, s2)

	if !w.IsAlive(e) {
		t.Error("entity not alive after bytes round-trip")
	}
	w.Read(func(r *flecs.Reader) {
		got, ok := flecs.Get[snapPos](r, e)
		if !ok {
			t.Error("snapPos missing after bytes round-trip")
			return
		}
		want := snapPos{X: 7, Y: 13}
		if got != want {
			t.Errorf("snapPos: want %v, got %v", want, got)
		}
	})
	_ = posID
}

// ── Test 4: Intervening mutations ────────────────────────────────────────────

func TestSnapshot_InterveningMutations(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[snapPos](w)

	entities := make([]flecs.ID, 100)
	w.Write(func(fw *flecs.Writer) {
		for i := range entities {
			entities[i] = fw.NewEntity()
			flecs.Set(fw, entities[i], snapPos{X: float32(i)})
		}
	})

	s := flecs.TakeSnapshot(w)

	// Delete 50 entities after the snapshot.
	for i := 0; i < 50; i++ {
		w.Delete(entities[i])
	}

	// Confirm 50 deletions took effect.
	aliveAfterDelete := 0
	w.Read(func(r *flecs.Reader) {
		for _, e := range entities {
			if r.IsAlive(e) {
				aliveAfterDelete++
			}
		}
	})
	if aliveAfterDelete != 50 {
		t.Errorf("expected 50 alive before restore, got %d", aliveAfterDelete)
	}

	flecs.RestoreSnapshot(w, s)

	aliveAfterRestore := 0
	w.Read(func(r *flecs.Reader) {
		for _, e := range entities {
			if r.IsAlive(e) {
				aliveAfterRestore++
			}
		}
	})
	if aliveAfterRestore != 100 {
		t.Errorf("expected 100 alive after restore, got %d", aliveAfterRestore)
	}
	_ = posID
}

// ── Test 5: Sparse components survive ────────────────────────────────────────

func TestSnapshot_SparseComponents(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[snapPos](w)
	flecs.SetSparse(w, posID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, snapPos{X: 3.14, Y: 2.72})
	})

	s := flecs.TakeSnapshot(w)
	w.Delete(e)

	flecs.RestoreSnapshot(w, s)

	if !w.IsAlive(e) {
		t.Fatal("entity not alive after sparse restore")
	}
	w.Read(func(r *flecs.Reader) {
		got, ok := flecs.Get[snapPos](r, e)
		if !ok {
			t.Error("sparse snapPos missing after restore")
			return
		}
		want := snapPos{X: 3.14, Y: 2.72}
		if got != want {
			t.Errorf("sparse snapPos: want %v, got %v", want, got)
		}
	})
}

// ── Test 6: DontFragment components survive ───────────────────────────────────

func TestSnapshot_DontFragmentComponents(t *testing.T) {
	w := flecs.New()
	velID := flecs.RegisterComponent[snapVel](w)
	flecs.SetSparse(w, velID)
	flecs.SetDontFragment(w, velID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, snapVel{DX: 1.5, DY: 2.5})
	})

	s := flecs.TakeSnapshot(w)
	w.Delete(e)

	flecs.RestoreSnapshot(w, s)

	if !w.IsAlive(e) {
		t.Fatal("entity not alive after DontFragment restore")
	}
	w.Read(func(r *flecs.Reader) {
		got, ok := flecs.Get[snapVel](r, e)
		if !ok {
			t.Error("DontFragment snapVel missing after restore")
			return
		}
		want := snapVel{DX: 1.5, DY: 2.5}
		if got != want {
			t.Errorf("DontFragment snapVel: want %v, got %v", want, got)
		}
		if !flecs.IsDontFragment(r, velID) {
			t.Error("IsDontFragment: expected true after restore")
		}
	})
}

// ── Test 7: Union state survives ──────────────────────────────────────────────

func TestSnapshot_UnionState(t *testing.T) {
	w := flecs.New()

	var rel, t1, t2, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		t1 = fw.NewEntity()
		t2 = fw.NewEntity()
	})
	flecs.SetUnion(w, rel)

	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(rel, t1))
	})

	// Capture current union target before snapshot.
	var targetBefore flecs.ID
	w.Read(func(r *flecs.Reader) {
		flecs.EachUnion(r, rel, func(entity, target flecs.ID) {
			if entity.Index() == e.Index() {
				targetBefore = target
			}
		})
	})
	if targetBefore.Index() != t1.Index() {
		t.Fatalf("expected union target t1, got %v", targetBefore)
	}

	s := flecs.TakeSnapshot(w)

	// Mutate: switch to t2.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, flecs.MakePair(rel, t2))
	})

	flecs.RestoreSnapshot(w, s)

	var targetAfter flecs.ID
	w.Read(func(r *flecs.Reader) {
		flecs.EachUnion(r, rel, func(entity, target flecs.ID) {
			if entity.Index() == e.Index() {
				targetAfter = target
			}
		})
	})
	if targetAfter.Index() != t1.Index() {
		t.Errorf("union target after restore: want t1, got %v", targetAfter)
	}
}

// ── Test 8: Trait policies survive ───────────────────────────────────────────

func TestSnapshot_TraitPolicies(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[snapPos](w)
	flecs.SetSparse(w, posID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, snapPos{X: 1, Y: 2})
	})

	s := flecs.TakeSnapshot(w)
	w.Delete(e)
	flecs.RestoreSnapshot(w, s)

	// Verify the Sparse policy is still set after restore.
	w.Read(func(r *flecs.Reader) {
		if !flecs.IsSparse(r, posID) {
			t.Error("Sparse policy missing after restore")
		}
	})

	// Verify the policy is functional: Set still works.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, snapPos{X: 99, Y: 99})
	})
	w.Read(func(r *flecs.Reader) {
		got, ok := flecs.Get[snapPos](r, e)
		if !ok {
			t.Error("snapPos missing after post-restore Set")
			return
		}
		if got.X != 99 {
			t.Errorf("snapPos X after policy re-use: want 99, got %v", got.X)
		}
	})
}

// ── Test 9: Observers are NOT restored ───────────────────────────────────────

func TestSnapshot_ObserversNotRestored(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[snapPos](w)

	// Take a baseline snapshot with no user entities.
	s := flecs.TakeSnapshot(w)

	// Register observer AFTER the snapshot.
	fired := 0
	flecs.Observe[snapPos](w, flecs.EventOnAdd, func(_ *flecs.Writer, _ flecs.ID, _ snapPos) {
		fired++
	})

	// Create an entity (fires observer once for the Add event).
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, snapPos{X: 1})
		_ = e
	})
	if fired != 1 {
		t.Fatalf("expected 1 observer fire after Set, got %d", fired)
	}

	// Restore to baseline (no user entities). Observer must NOT fire.
	flecs.RestoreSnapshot(w, s)

	if fired != 1 {
		t.Errorf("observer fired during RestoreSnapshot: want 1 total, got %d", fired)
	}
	_ = posID
}

// ── Test 10: Version mismatch returns error ───────────────────────────────────

func TestSnapshot_VersionMismatch(t *testing.T) {
	w := flecs.New()
	s := flecs.TakeSnapshot(w)
	b := s.Bytes()

	// Corrupt the version field (bytes 4-7, big-endian).
	b[4] = 0xFF
	b[5] = 0xFF
	b[6] = 0xFF
	b[7] = 0xFF

	_, err := flecs.LoadSnapshot(b)
	if err == nil {
		t.Error("LoadSnapshot with corrupt version: expected error, got nil")
	}
}

// ── Test 11: Empty-world snapshot+restore ────────────────────────────────────

func TestSnapshot_EmptyWorld(t *testing.T) {
	w := flecs.New()

	var countBefore int
	w.Read(func(r *flecs.Reader) { countBefore = r.Count() })

	s := flecs.TakeSnapshot(w)
	flecs.RestoreSnapshot(w, s)

	var countAfter int
	w.Read(func(r *flecs.Reader) { countAfter = r.Count() })

	if countAfter != countBefore {
		t.Errorf("empty-world restore: count before %d, after %d", countBefore, countAfter)
	}

	// Bytes round-trip on empty world.
	b := s.Bytes()
	s2, err := flecs.LoadSnapshot(b)
	if err != nil {
		t.Fatalf("LoadSnapshot on empty-world bytes: %v", err)
	}
	flecs.RestoreSnapshot(w, s2)

	var countFinal int
	w.Read(func(r *flecs.Reader) { countFinal = r.Count() })
	if countFinal != countBefore {
		t.Errorf("empty-world double restore: count %d, want %d", countFinal, countBefore)
	}
}

// ── Test 12: Concurrent take during Write panics ─────────────────────────────

func TestSnapshot_ConcurrentTakeDuringWrite(t *testing.T) {
	w := flecs.New()
	panicSeen := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicSeen = true
			}
		}()
		w.Write(func(_ *flecs.Writer) {
			// Write lock is held; TakeSnapshot must panic.
			flecs.TakeSnapshot(w)
		})
	}()
	if !panicSeen {
		t.Error("expected panic when TakeSnapshot called during Write block")
	}
}

// ── Test 13: Concurrent restore during Write or Read panics ──────────────────

func TestSnapshot_ConcurrentRestoreDuringWrite(t *testing.T) {
	w := flecs.New()
	s := flecs.TakeSnapshot(w)

	panicSeen := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicSeen = true
			}
		}()
		w.Write(func(_ *flecs.Writer) {
			// Write lock is held; RestoreSnapshot must panic.
			flecs.RestoreSnapshot(w, s)
		})
	}()
	if !panicSeen {
		t.Error("expected panic when RestoreSnapshot called during Write block")
	}
}

func TestSnapshot_ConcurrentRestoreDuringRead(t *testing.T) {
	w := flecs.New()
	s := flecs.TakeSnapshot(w)

	panicSeen := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicSeen = true
			}
		}()
		w.Read(func(_ *flecs.Reader) {
			// Read lock is held; RestoreSnapshot must panic.
			flecs.RestoreSnapshot(w, s)
		})
	}()
	if !panicSeen {
		t.Error("expected panic when RestoreSnapshot called during Read block")
	}
}

// ── Test 14: Memory regression ───────────────────────────────────────────────

func TestSnapshot_MemoryBound(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[snapPos](w)
	velID := flecs.RegisterComponent[snapVel](w)
	masID := flecs.RegisterComponent[snapMass](w)

	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < 1000; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, snapPos{X: float32(i), Y: float32(i)})
			flecs.Set(fw, e, snapVel{DX: float32(i), DY: float32(i)})
			flecs.Set(fw, e, snapMass{V: float32(i)})
		}
	})

	s := flecs.TakeSnapshot(w)
	size := len(s.Bytes())
	const maxBytes = 64 * 1024 // 64 KB
	if size > maxBytes {
		t.Errorf("snapshot too large: %d bytes, want ≤ %d", size, maxBytes)
	}
	_ = posID
	_ = velID
	_ = masID
}

// ── Test 15: Ordered children survive ────────────────────────────────────────

func TestSnapshot_OrderedChildren(t *testing.T) {
	w := flecs.New()

	var parent, c1, c2, c3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		c1 = fw.NewEntity()
		c2 = fw.NewEntity()
		c3 = fw.NewEntity()
		flecs.AddID(fw, c1, flecs.MakePair(w.ChildOf(), parent))
		flecs.AddID(fw, c2, flecs.MakePair(w.ChildOf(), parent))
		flecs.AddID(fw, c3, flecs.MakePair(w.ChildOf(), parent))
	})
	// Mark parent as ordered children after adding children.
	flecs.SetOrderedChildren(w, parent)

	s := flecs.TakeSnapshot(w)
	w.Delete(c1)
	w.Delete(c2)
	w.Delete(c3)
	w.Delete(parent)

	flecs.RestoreSnapshot(w, s)

	if !w.IsAlive(parent) {
		t.Fatal("parent not alive after restore")
	}
	var children []flecs.ID
	w.EachChild(parent, func(child flecs.ID) bool {
		children = append(children, child)
		return true
	})
	if len(children) != 3 {
		t.Errorf("expected 3 children after restore, got %d", len(children))
	}
}

// ── Test 16: Entity range survives ───────────────────────────────────────────

func TestSnapshot_EntityRange(t *testing.T) {
	w := flecs.New()

	// RangeSet panics inside a deferred scope (deferDepth > 0), so use
	// WriterForTest which provides immediate-mode writer access.
	fw := flecs.WriterForTest(w)
	flecs.RangeSet(fw, flecs.ID(1000), flecs.ID(2000))

	s := flecs.TakeSnapshot(w)

	// Clear the range.
	flecs.RangeClear(fw)

	// Verify range is gone.
	var setBeforeRestore bool
	w.Read(func(r *flecs.Reader) {
		_, _, setBeforeRestore = flecs.RangeGet(r)
	})
	if setBeforeRestore {
		t.Fatal("expected range cleared before restore")
	}

	flecs.RestoreSnapshot(w, s)

	// Verify range is restored.
	var minAfter, maxAfter flecs.ID
	var setAfter bool
	w.Read(func(r *flecs.Reader) {
		minAfter, maxAfter, setAfter = flecs.RangeGet(r)
	})
	if !setAfter {
		t.Error("entity range not set after restore")
	}
	if minAfter.Index() != 1000 || maxAfter.Index() != 2000 {
		t.Errorf("entity range: want [1000,2000), got [%d,%d)", minAfter.Index(), maxAfter.Index())
	}
}

// ── Test 17: Cleanup policy survives ─────────────────────────────────────────

func TestSnapshot_CleanupPolicy(t *testing.T) {
	w := flecs.New()

	var rel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
	})
	flecs.SetCleanupPolicy(w, rel, w.OnDelete(), w.DeleteAction())

	s := flecs.TakeSnapshot(w)

	// Clear rel and restore.
	w.Delete(rel)
	flecs.RestoreSnapshot(w, s)

	if !w.IsAlive(rel) {
		t.Fatal("rel not alive after cleanup-policy restore")
	}
	action, ok := flecs.GetCleanupPolicy(w, rel, w.OnDelete())
	if !ok {
		t.Error("cleanup policy missing after restore")
	}
	if action.Index() != w.DeleteAction().Index() {
		t.Errorf("cleanup policy action: want DeleteAction, got %v", action)
	}
}

// ── Test 18: OneOf policy survives ────────────────────────────────────────────

func TestSnapshot_OneOfPolicy(t *testing.T) {
	w := flecs.New()

	var rel, parent flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		parent = fw.NewEntity()
	})
	flecs.SetOneOf(w, rel, parent)

	s := flecs.TakeSnapshot(w)
	w.Delete(rel)
	w.Delete(parent)
	flecs.RestoreSnapshot(w, s)

	if !w.IsAlive(rel) {
		t.Fatal("rel not alive after oneOf restore")
	}
	w.Read(func(r *flecs.Reader) {
		gotParent, ok := flecs.IsOneOf(r, rel)
		if !ok {
			t.Error("OneOf policy missing after restore")
			return
		}
		if gotParent.Index() != parent.Index() {
			t.Errorf("OneOf parent: want %v, got %v", parent.Index(), gotParent.Index())
		}
	})
}

// ── Test 19: CanToggle bitsets survive ───────────────────────────────────────

func TestSnapshot_CanToggleBitsets(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[snapPos](w)
	flecs.SetCanToggle(w, posID)

	// Mutations are deferred inside w.Write, so Set and DisableID must be in
	// separate blocks (entity is not in the component table until the first
	// Write block flushes).
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, snapPos{X: 5, Y: 6})
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.DisableID(fw, e, posID)
	})

	var enabledBefore bool
	w.Read(func(r *flecs.Reader) {
		enabledBefore = flecs.IsEnabled[snapPos](r, e)
	})
	if enabledBefore {
		t.Fatal("expected component to be disabled before snapshot")
	}

	s := flecs.TakeSnapshot(w)
	w.Delete(e)
	flecs.RestoreSnapshot(w, s)

	if !w.IsAlive(e) {
		t.Fatal("entity not alive after CanToggle restore")
	}
	w.Read(func(r *flecs.Reader) {
		if flecs.IsEnabled[snapPos](r, e) {
			t.Error("expected component still disabled after restore")
		}
		// Component value survives even when disabled.
		got, ok := flecs.Get[snapPos](r, e)
		if !ok {
			t.Error("snapPos missing after CanToggle restore")
			return
		}
		want := snapPos{X: 5, Y: 6}
		if got != want {
			t.Errorf("snapPos: want %v, got %v", want, got)
		}
	})
}

// ── Test 20: Recycle queue survives ──────────────────────────────────────────

func TestSnapshot_RecycleQueueSurvives(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[snapPos](w)

	// Create two entities then delete one to put it in the recycle queue.
	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.Set(fw, e1, snapPos{X: 1})
		flecs.Set(fw, e2, snapPos{X: 2})
	})
	w.Delete(e1) // e1's ID goes to recycle queue.

	s := flecs.TakeSnapshot(w)

	// Consume recycled ID (creates e3 which should reuse e1's index).
	var e3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e3 = fw.NewEntity()
	})

	flecs.RestoreSnapshot(w, s)

	// After restore, e1's index should be back in the recycle queue (dead).
	if w.IsAlive(e1) {
		t.Error("e1 should be dead (in recycle queue) after restore")
	}
	if !w.IsAlive(e2) {
		t.Error("e2 should be alive after restore")
	}
	// e3 was created after the snapshot, so it should be gone after restore.
	if w.IsAlive(e3) {
		t.Error("e3 (created after snapshot) should not be alive after restore")
	}
	_ = posID
}

// ── Test 22: Singleton policy survives (covers singletonInstances cleanup) ───

func TestSnapshot_SingletonSurvives(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[snapPos](w)
	flecs.SetSingleton(w, posID)

	var holder flecs.ID
	w.Write(func(fw *flecs.Writer) {
		holder = fw.NewEntity()
		flecs.Set(fw, holder, snapPos{X: 11, Y: 22})
	})

	s := flecs.TakeSnapshot(w)
	w.Delete(holder)
	flecs.RestoreSnapshot(w, s)

	if !w.IsAlive(holder) {
		t.Fatal("singleton holder not alive after restore")
	}
	w.Read(func(r *flecs.Reader) {
		got, ok := flecs.Get[snapPos](r, holder)
		if !ok {
			t.Error("singleton component missing after restore")
			return
		}
		want := snapPos{X: 11, Y: 22}
		if got != want {
			t.Errorf("singleton: want %v, got %v", want, got)
		}
	})
}

// ── Test 23: WriteOnce policy survives (covers writeOnceHasBeenSet cleanup) ──

func TestSnapshot_WriteOnceSurvives(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[snapPos](w)
	flecs.SetWriteOnce(w, posID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, snapPos{X: 7, Y: 8})
	})

	s := flecs.TakeSnapshot(w)
	w.Delete(e)
	flecs.RestoreSnapshot(w, s)

	if !w.IsAlive(e) {
		t.Fatal("writeOnce entity not alive after restore")
	}
	w.Read(func(r *flecs.Reader) {
		if !flecs.IsWriteOnce(r, posID) {
			t.Error("WriteOnce policy missing after restore")
		}
	})
}

// ── Test 24: Instantiate policy survives ──────────────────────────────────────

func TestSnapshot_InstantiatePolicy(t *testing.T) {
	w := flecs.New()

	var rel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
	})
	flecs.SetInstantiatePolicy(w, rel, w.Override())

	s := flecs.TakeSnapshot(w)
	w.Delete(rel)
	flecs.RestoreSnapshot(w, s)

	if !w.IsAlive(rel) {
		t.Fatal("rel not alive after instantiate-policy restore")
	}
	action, ok := flecs.GetInstantiatePolicy(w, rel)
	if !ok {
		t.Error("instantiate policy missing after restore")
	}
	if action.Index() != w.Override().Index() {
		t.Errorf("instantiate policy action: want Override, got %v", action)
	}
}

// ── Test 25: Truncated blob panics covering error-return paths ────────────────

func TestSnapshot_TruncatedBlobPanics(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[snapPos](w)
	flecs.SetSparse(w, posID)
	flecs.SetCanToggle(w, posID)

	var rel, tgt1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt1 = fw.NewEntity()
	})
	flecs.SetUnion(w, rel)
	flecs.SetCleanupPolicy(w, rel, w.OnDelete(), w.DeleteAction())
	flecs.SetInstantiatePolicy(w, rel, w.Override())

	entities := make([]flecs.ID, 5)
	w.Write(func(fw *flecs.Writer) {
		for i := range entities {
			entities[i] = fw.NewEntity()
			flecs.Set(fw, entities[i], snapPos{X: float32(i)})
			fw.AddID(entities[i], flecs.MakePair(rel, tgt1))
		}
	})

	s := flecs.TakeSnapshot(w)
	worldID := flecs.SnapshotWorldID(s)
	blob := flecs.SnapshotBlob(s)

	// Sweep every 3-byte increment of the blob. Each truncation makes
	// snapshotDeserialize error out at a different decode point, covering
	// the error-return paths across all deserialize functions.
	for cut := 0; cut < len(blob); cut += 3 {
		truncated := flecs.NewSnapshotRaw(worldID, blob[:cut])
		func() {
			defer func() { _ = recover() }()
			flecs.RestoreSnapshot(w, truncated)
		}()
	}
	// Restore world to a clean state for subsequent tests.
	flecs.RestoreSnapshot(w, s)
	_ = posID
}

// ── Test 26: Cross-world restore panics ──────────────────────────────────────

func TestSnapshot_CrossWorldRestore(t *testing.T) {
	w1 := flecs.New()
	w2 := flecs.New()
	flecs.RegisterComponent[snapPos](w2)

	s := flecs.TakeSnapshot(w1)

	panicSeen := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicSeen = true
			}
		}()
		flecs.RestoreSnapshot(w2, s)
	}()
	if !panicSeen {
		t.Error("expected panic when restoring snapshot from a different world")
	}
}

// ── Test 16: LoadSnapshot rejects invalid magic ───────────────────────────────

func TestSnapshot_InvalidMagic(t *testing.T) {
	_, err := flecs.LoadSnapshot([]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	if err == nil {
		t.Error("LoadSnapshot with invalid magic: expected error, got nil")
	}
}

// ── Test 17: LoadSnapshot rejects short buffer ────────────────────────────────

func TestSnapshot_ShortBuffer(t *testing.T) {
	_, err := flecs.LoadSnapshot([]byte{0xF1, 0xEC})
	if err == nil {
		t.Error("LoadSnapshot with short buffer: expected error, got nil")
	}
}
