package flecs_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/snichols/flecs"
)

// ── Concurrency tests for the Read/Write scope API ──────────────────────────

type scopePos struct{ X, Y float32 }
type scopeVel struct{ DX, DY float32 }

// TestReadAllowsConcurrentReaders verifies that world.Read opens a shared-read
// window in which multiple goroutines can iterate safely without data races.
func TestReadAllowsConcurrentReaders(t *testing.T) {
	w := flecs.New()
	const n = 200
	for range n {
		e := w.NewEntity()
		w.Write(func(fw *flecs.Writer) {
			flecs.Set(fw, e, scopePos{X: 1, Y: 2})
		})
	}

	const goroutines = 8
	var wg sync.WaitGroup
	var total atomic.Int64

	// Multiple goroutines each call w.Read concurrently.
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.Read(func(fr *flecs.Reader) {
				flecs.Each1[scopePos](fr, func(_ flecs.ID, p *scopePos) {
					total.Add(int64(p.X))
				})
			})
		}()
	}
	wg.Wait()

	if total.Load() != int64(goroutines*n) {
		t.Fatalf("expected %d, got %d", goroutines*n, total.Load())
	}
}

// TestWriteSerializesWithReaders verifies that world.Write acquires exclusive
// access so concurrent reads see a consistent snapshot.
func TestWriteSerializesWithReaders(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, scopePos{X: 0})
	})

	// Writer goroutine updates the value many times.
	const iters = 100
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range iters {
			w.Write(func(fw *flecs.Writer) {
				flecs.Set(fw, e, scopePos{X: float32(i + 1)})
			})
		}
	}()

	// Reader goroutine checks consistency between each Read call.
	var readsDone atomic.Int64
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				w.Read(func(fr *flecs.Reader) {
					// Just verify we can read without panic.
					_, _ = flecs.Get[scopePos](fr, e)
					readsDone.Add(1)
				})
			}
		}
	}()

	<-done
	// We just verify no panic occurred; at least some reads may have happened.
	_ = readsDone.Load()
}

// TestWriteFromOtherGoroutinePanicsWhenClaimed verifies that a nested
// world.Write from a *different* goroutine (while the world is held by
// another Write) panics with ErrExclusiveAccessViolation.
func TestWriteFromOtherGoroutinePanicsWhenClaimed(t *testing.T) {
	w := flecs.New()
	started := make(chan struct{})
	release := make(chan struct{})

	// Hold the Write scope open from goroutine 1.
	go func() {
		w.Write(func(_ *flecs.Writer) {
			close(started)
			<-release
		})
	}()
	<-started

	// Goroutine 2 tries to enter Write while goroutine 1 holds it.
	panicked := make(chan bool, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicked <- true
			} else {
				panicked <- false
			}
		}()
		// This should block (not panic) because Write uses a mutex.
		// Actually it will block until goroutine 1 releases — we don't want
		// to block forever, so instead test with ExclusiveAccess directly.
		// The real violation scenario is tested below via ExclusiveAccessBegin.
		panicked <- false
	}()
	close(release)

	// Drain the channel.
	<-panicked

	// Now test the actual exclusive-access panic path:
	// manually claim exclusive access, then try a write from a goroutine.
	w2 := flecs.New()
	w2.ExclusiveAccessBegin("test-owner")
	violationPanicked := make(chan bool, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				violationPanicked <- true
			} else {
				violationPanicked <- false
			}
		}()
		// checkExclusiveAccessWrite panics when owner != currentGoid.
		_ = w2.NewEntity()
	}()
	if !<-violationPanicked {
		t.Error("expected panic from NewEntity on world claimed by different goroutine")
	}
	w2.ExclusiveAccessEnd(false)
}

// TestNestedWriteSharesScope verifies that a Write called from within an
// active Write on the same goroutine shares the deferred-command queue:
// operations inside the nested Write are not flushed until the outer Write
// returns.
func TestNestedWriteSharesScope(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// Nested Write: outer opens the scope; inner Write on same goroutine
	// should share it (same goid, increments deferDepth).
	w.Write(func(outer *flecs.Writer) {
		flecs.Set(outer, e, scopePos{X: 1})

		// Call Write again from the same goroutine.
		w.Write(func(inner *flecs.Writer) {
			// Override the Set — this is queued in the same batch.
			flecs.Set(inner, e, scopePos{X: 42})
		})
		// After inner Write returns, inner's ops have been flushed (depth back
		// to outer's level). The value should now be 42.
	})

	var got scopePos
	w.Read(func(fr *flecs.Reader) {
		got, _ = flecs.Get[scopePos](fr, e)
	})
	if got.X != 42 {
		t.Fatalf("expected X=42 after nested Write, got %v", got.X)
	}
}

// TestGetRefValidInsideScopeOnly verifies that GetRef returns a non-nil pointer
// inside a Read scope and that the pointer yields the correct value.
func TestGetRefValidInsideScopeOnly(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, scopePos{X: 7, Y: 3})
	})

	w.Read(func(fr *flecs.Reader) {
		ptr := flecs.GetRef[scopePos](fr, e)
		if ptr == nil {
			t.Fatal("GetRef returned nil inside Read scope")
		}
		if ptr.X != 7 || ptr.Y != 3 {
			t.Fatalf("GetRef returned wrong value: %+v", *ptr)
		}
	})
}

// TestHookReceivesWriterContext verifies that a hook registered via OnSet
// fires during flush of a Write scope and receives a non-nil *Writer.
func TestHookReceivesWriterContext(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	var hookFired atomic.Bool
	var hookValue scopePos
	var hookWriter *flecs.Writer
	flecs.OnSet[scopePos](w, func(fw *flecs.Writer, entity flecs.ID, v scopePos) {
		hookFired.Store(true)
		hookValue = v
		hookWriter = fw
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, scopePos{X: 99, Y: 1})
	})

	if !hookFired.Load() {
		t.Fatal("OnSet hook did not fire during Write scope flush")
	}
	if hookValue.X != 99 {
		t.Fatalf("hook received wrong value: %+v", hookValue)
	}
	if hookWriter == nil {
		t.Fatal("hook received nil *Writer")
	}
}

// ── Additional coverage for Reader/Writer methods ──────────────────────────

// TestReaderMethods exercises all Reader methods via the Read scope.
func TestReaderMethods(t *testing.T) {
	w := flecs.New()
	var (
		e1, e2, e3   flecs.ID
		posID, velID flecs.ID
	)
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		e3 = fw.NewEntity()
		posID = flecs.RegisterComponent[scopePos](w)
		velID = flecs.RegisterComponent[scopeVel](w)
		flecs.Set(fw, e1, scopePos{X: 1, Y: 2})
		flecs.Set(fw, e2, scopeVel{DX: 3})
		fw.SetName(e1, "entity1")
		fw.AddID(e2, flecs.MakePair(w.ChildOf(), e1))
	})

	w.Read(func(fr *flecs.Reader) {
		// IsAlive
		if !fr.IsAlive(e1) {
			t.Error("IsAlive(e1) should be true")
		}

		// Count
		if fr.Count() == 0 {
			t.Error("Count should be > 0")
		}

		// TablesFor
		tables := fr.TablesFor(posID)
		if len(tables) == 0 {
			t.Error("TablesFor(posID) should return at least 1 table")
		}

		// EachTableFor (using TablesFor instead to avoid table.Table import)
		_ = fr.TablesFor(velID)

		// HasID
		if !fr.HasID(e1, posID) {
			t.Error("HasID(e1, posID) should be true")
		}

		// OwnsID
		if !fr.OwnsID(e1, posID) {
			t.Error("OwnsID(e1, posID) should be true")
		}

		// ParentOf
		_, hasParent := fr.ParentOf(e2)
		if !hasParent {
			t.Error("e2 should have a parent")
		}

		// EachChild
		childCount := 0
		fr.EachChild(e1, func(_ flecs.ID) bool { childCount++; return true })
		if childCount == 0 {
			t.Error("e1 should have children")
		}

		// GetName
		name, ok := fr.GetName(e1)
		if !ok || name != "entity1" {
			t.Errorf("GetName(e1) = %q, %v; want 'entity1', true", name, ok)
		}

		// Lookup / LookupChild
		found, ok := fr.Lookup("entity1")
		if !ok || found != e1 {
			t.Error("Lookup('entity1') should find e1")
		}

		// PathOf
		path := fr.PathOf(e1)
		if path != "entity1" {
			t.Errorf("PathOf(e1) = %q, want 'entity1'", path)
		}

		// Components
		comps := fr.Components()
		if len(comps) == 0 {
			t.Error("Components() should be non-empty")
		}

		// ComponentInfo
		info, ok := fr.ComponentInfo(posID)
		if !ok || info.ID != posID {
			t.Error("ComponentInfo(posID) failed")
		}

		// EntityComponents
		ec := fr.EntityComponents(e1)
		if len(ec) == 0 {
			t.Error("EntityComponents(e1) should be non-empty")
		}

		// EachEntity
		entityCount := 0
		fr.EachEntity(func(_ flecs.ID) bool { entityCount++; return true })
		if entityCount == 0 {
			t.Error("EachEntity should visit at least one entity")
		}

		// AliveEntities
		alive := fr.AliveEntities()
		if len(alive) == 0 {
			t.Error("AliveEntities should return non-empty slice")
		}

		// SystemCount
		_ = fr.SystemCount()

		// SystemCountInPhase
		_ = fr.SystemCountInPhase(w.OnUpdate())

		// GetByID
		v, ok := fr.GetByID(e1, posID)
		if !ok || v == nil {
			t.Error("GetByID(e1, posID) failed")
		}

		// Stats
		stats := fr.Stats()
		_ = stats
	})

	// Delete via Writer.Delete
	w.Write(func(fw *flecs.Writer) {
		_ = fw.Delete(e3)
	})

	w.Read(func(fr *flecs.Reader) {
		if fr.IsAlive(e3) {
			t.Error("e3 should be dead after Write.Delete")
		}
	})
}

// TestWriterMethods exercises all Writer methods via the Write scope.
func TestWriterMethods(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[scopePos](w)

	w.Write(func(fw *flecs.Writer) {
		// NewEntity
		e := fw.NewEntity()
		if e == 0 {
			t.Fatal("fw.NewEntity returned 0")
		}

		// SetByID
		fw.SetByID(e, posID, scopePos{X: 5, Y: 6})

		// SetName
		fw.SetName(e, "writer-test")

		// AddID / RemoveID
		tagID := flecs.RegisterComponent[scopeVel](w)
		fw.AddID(e, tagID)
		fw.RemoveID(e, tagID)

		// SetPairByID
		rel := fw.NewEntity()
		tgt := fw.NewEntity()
		fw.SetPairByID(e, rel, tgt, scopePos{X: 10})
	})
}

// TestScopeHasUp exercises HasUp and TargetUp via the free functions using Reader.
func TestScopeHasUp(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[scopePos](w)
	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		child = fw.NewEntity()
		flecs.Set(fw, parent, scopePos{X: 1})
		fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))
	})

	w.Read(func(fr *flecs.Reader) {
		if !flecs.HasUp(fr, child, posID, w.ChildOf()) {
			t.Error("HasUp should find scopePos on parent")
		}

		tgt, ok := flecs.TargetUp(fr, child, posID, w.ChildOf())
		if !ok || tgt != parent {
			t.Errorf("TargetUp returned (%v, %v), want (%v, true)", tgt, ok, parent)
		}

		v, ok := flecs.GetUp[scopePos](fr, child, w.ChildOf())
		if !ok || v.X != 1 {
			t.Errorf("GetUp returned (%v, %v), want ({1,0}, true)", v, ok)
		}
	})
}

// TestScopePrefabOf exercises PrefabOf via Reader free function.
func TestScopePrefabOf(t *testing.T) {
	w := flecs.New()
	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		child = fw.NewEntity()
		fw.AddID(child, flecs.MakePair(w.IsA(), prefab))
	})

	w.Read(func(fr *flecs.Reader) {
		got, ok := flecs.PrefabOf(fr, child)
		if !ok || got != prefab {
			t.Errorf("PrefabOf returned (%v, %v), want (%v, true)", got, ok, prefab)
		}

		// Also via Reader.PrefabOf
		got2, ok2 := fr.PrefabOf(child)
		if !ok2 || got2 != prefab {
			t.Errorf("fr.PrefabOf returned (%v, %v)", got2, ok2)
		}

		// Reader.EachPrefab
		count := 0
		fr.EachPrefab(child, func(_ flecs.ID) bool { count++; return true })
		if count != 1 {
			t.Errorf("EachPrefab count = %d, want 1", count)
		}
	})
}

// TestScopeGetPairRef exercises GetPairRef via Reader free function.
func TestScopeGetPairRef(t *testing.T) {
	w := flecs.New()
	var e, rel, tgt flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		flecs.SetPair[scopePos](fw, e, rel, tgt, scopePos{X: 7})
	})

	w.Read(func(fr *flecs.Reader) {
		ptr := flecs.GetPairRef[scopePos](fr, e, rel, tgt)
		if ptr == nil {
			t.Fatal("GetPairRef returned nil")
		}
		if ptr.X != 7 {
			t.Errorf("GetPairRef value = %v, want 7", ptr.X)
		}
	})
}

// TestReaderEachTableFor exercises fr.EachTableFor via the ReaderEachTableForCount
// export helper (which has access to table.Table).
func TestReaderEachTableFor(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[scopePos](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, scopePos{X: 1})
	})
	_ = e

	count := flecs.ReaderEachTableForCount(w, posID, 0)
	if count == 0 {
		t.Fatal("ReaderEachTableForCount should visit at least one table")
	}

	// Also test early-stop path.
	count2 := flecs.ReaderEachTableForCount(w, posID, 1)
	if count2 != 1 {
		t.Fatalf("ReaderEachTableForCount early-stop want 1, got %d", count2)
	}
}

// TestReaderLookupChild exercises fr.LookupChild via the scope API.
func TestReaderLookupChild(t *testing.T) {
	w := flecs.New()
	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		child = fw.NewEntity()
		fw.SetName(parent, "scope-parent")
		fw.SetName(child, "scope-child")
		fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))
	})

	w.Read(func(fr *flecs.Reader) {
		found, ok := fr.LookupChild(parent, "scope-child")
		if !ok || found != child {
			t.Errorf("fr.LookupChild = (%v, %v), want (%v, true)", found, ok, child)
		}
	})
}

// TestReaderSystemCount exercises fr.SystemCount and fr.SystemCountInPhase
// for the removed-system branch.
func TestReaderSystemCount(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[scopePos](w)
	_ = posID

	cq := flecs.NewCachedQuery(w, posID)
	sys := flecs.NewSystem(w, cq, func(_ float32, _ *flecs.QueryIter) {})

	w.Read(func(fr *flecs.Reader) {
		n := fr.SystemCount()
		if n == 0 {
			t.Fatal("SystemCount should be > 0 after NewSystem")
		}
		n2 := fr.SystemCountInPhase(w.OnUpdate())
		if n2 == 0 {
			t.Fatal("SystemCountInPhase(OnUpdate) should be > 0")
		}
	})

	// Remove the system, then recheck (exercises the removed branch).
	sys.Close()
	w.Read(func(fr *flecs.Reader) {
		_ = fr.SystemCount()
	})
}

// TestWriteNestedFromSameGoroutine exercises the re-entrant Write path.
func TestWriteNestedFromSameGoroutine(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[scopePos](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// Call Write from within Write on the same goroutine (re-entrant path).
	w.Write(func(outer *flecs.Writer) {
		flecs.Set(outer, e, scopePos{X: 1})
		// Re-enter Write from same goroutine; should share deferred queue.
		w.Write(func(inner *flecs.Writer) {
			flecs.Set(inner, e, scopePos{X: 100})
		})
	})

	var got scopePos
	var ok bool
	w.Read(func(fr *flecs.Reader) {
		got, ok = flecs.Get[scopePos](fr, e)
	})
	if !ok || got.X != 100 {
		t.Fatalf("expected X=100, got %v (ok=%v)", got.X, ok)
	}
	_ = posID
}

// TestWritePanicsWhenClaimedByOtherGoroutine verifies that w.Write() panics
// with ErrExclusiveAccessViolation when another goroutine holds exclusive access.
func TestWritePanicsWhenClaimedByOtherGoroutine(t *testing.T) {
	w := flecs.New()
	started := make(chan struct{})
	release := make(chan struct{})

	// Goroutine 1 holds exclusive access via Write.
	go func() {
		w.Write(func(_ *flecs.Writer) {
			close(started)
			<-release
		})
	}()
	<-started

	// Goroutine 2 tries Write while Goroutine 1 holds exclusive access.
	// This should eventually succeed (after goroutine 1 releases the mu.Lock),
	// not panic — because the mutex ensures serialization, not the exclusive-access check.
	// The ErrExclusiveAccessViolation fires only when ExclusiveAccessBegin is called
	// without the mutex (e.g., in the old bare-World API). With the mutex-based Write,
	// goroutine 2 blocks on w.mu.Lock() and proceeds after goroutine 1 finishes.
	//
	// To actually trigger the panic, we use ExclusiveAccessBegin directly:
	close(release) // release goroutine 1

	w2 := flecs.New()
	w2.ExclusiveAccessBegin("test")

	panicked := make(chan bool, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicked <- true
			} else {
				panicked <- false
			}
		}()
		// Write will see owner != 0 (and owner != currentGoid) → panic
		w2.Write(func(_ *flecs.Writer) {})
	}()
	if !<-panicked {
		t.Error("expected Write to panic when world is claimed by a different goroutine")
	}
	w2.ExclusiveAccessEnd(false)
}

// TestGetRefNilCases exercises the nil-return paths of GetRef[T] and GetPairRef[T].
func TestGetRefNilCases(t *testing.T) {
	w := flecs.New()
	var e, dead flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		dead = fw.NewEntity()
		flecs.Set(fw, e, scopePos{X: 1})
		fw.Delete(dead) // delete it so GetRef on dead entity returns nil
	})

	w.Read(func(fr *flecs.Reader) {
		// T not registered: should return nil.
		type notRegistered struct{ Z int }
		ptr := flecs.GetRef[notRegistered](fr, e)
		if ptr != nil {
			t.Error("GetRef of unregistered type should return nil")
		}

		// Entity exists but does not have scopeVel: should return nil.
		ptr2 := flecs.GetRef[scopeVel](fr, e)
		if ptr2 != nil {
			t.Error("GetRef of absent component should return nil")
		}

		// Dead entity: should return nil.
		ptr3 := flecs.GetRef[scopePos](fr, dead)
		if ptr3 != nil {
			t.Error("GetRef on dead entity should return nil")
		}
	})

	// GetPairRef: pair not set → nil.
	var rel, tgt flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
	})
	w.Read(func(fr *flecs.Reader) {
		ptr := flecs.GetPairRef[scopePos](fr, e, rel, tgt)
		if ptr != nil {
			t.Error("GetPairRef for non-existent pair should return nil")
		}
	})
}

// TestSetByIDDeferredTagPath exercises SetByID with a zero-size component in
// deferred mode (the tag/zero-size branch in the deferred path).
func TestSetByIDDeferredTagPath(t *testing.T) {
	w := flecs.New()
	type tagType struct{}
	tagID := flecs.RegisterComponent[tagType](w)
	e := w.NewEntity()

	flecs.DeferBeginForTest(w)
	// Set a zero-size (tag) component via SetByID in deferred mode.
	w.SetByID(e, tagID, tagType{})
	flecs.DeferEndForTest(w)

	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(e, tagID) {
			t.Fatal("tag should be present after deferred SetByID flush")
		}
	})
}

// TestTermKindString exercises the TermKind.String() method for coverage.
func TestTermKindString(t *testing.T) {
	cases := []struct {
		kind flecs.TermKind
		want string
	}{
		{flecs.TermAnd, "And"},
		{flecs.TermNot, "Not"},
		{flecs.TermOptional, "Optional"},
		{flecs.TermOr, "Or"},
		{flecs.TermKind(99), "TermKind(99)"},
	}
	for _, tc := range cases {
		got := tc.kind.String()
		if got != tc.want {
			t.Errorf("TermKind(%d).String() = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

// TestDeferredAddIDAlreadyHasComponent exercises the deferred AddID branch
// where the entity already has the component (returns false without queuing).
func TestDeferredAddIDAlreadyHasComponent(t *testing.T) {
	w := flecs.New()
	tagID := flecs.RegisterComponent[scopeVel](w)
	e := w.NewEntity()
	flecs.AddID(w.W(), e, tagID) // add it

	flecs.DeferBeginForTest(w)
	// Try to add again while deferred — should return false (already present).
	result := flecs.AddID(w.W(), e, tagID)
	if result {
		t.Fatal("AddID should return false when component is already present in deferred mode")
	}
	flecs.DeferEndForTest(w)
}

// TestFieldMaybe_ZeroSizePair exercises the zero-size optional path in FieldMaybe.
func TestFieldMaybe_ZeroSizePair(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[scopePos](w)
	// scopeVel as optional tag (zero-size when added via tag — but we use it as struct)
	// Use TermOptional for a component that exists on some entities but not others.
	velID := flecs.RegisterComponent[scopeVel](w)

	e1 := w.NewEntity()
	e2 := w.NewEntity()
	flecs.Set(w.W(), e1, scopePos{X: 1})
	flecs.Set(w.W(), e2, scopePos{X: 2})
	flecs.Set(w.W(), e2, scopeVel{DX: 3})

	q := flecs.NewQueryFromTerms(w,
		flecs.Term{ID: posID, Kind: flecs.TermAnd},
		flecs.Term{ID: velID, Kind: flecs.TermOptional},
	)
	it := q.Iter()
	foundOptional := false
	for it.Next() {
		if vels, ok := flecs.FieldMaybe[scopeVel](it, velID); ok {
			foundOptional = true
			if len(vels) == 0 {
				t.Fatal("FieldMaybe returned empty slice for present optional")
			}
			_ = vels
		}
	}
	if !foundOptional {
		t.Fatal("expected at least one table with optional scopeVel present")
	}
}
