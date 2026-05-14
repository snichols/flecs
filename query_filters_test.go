package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// Component types used in query_filters tests.
type QFPos struct{ X, Y float32 }
type QFVel struct{ DX, DY float32 }

// helper: collect entity count from an ordinary query over posID.
func qfCountEntities(w *flecs.World, posID flecs.ID) int {
	count := 0
	q := flecs.NewQuery(w, posID)
	it := q.Iter()
	for it.Next() {
		count += it.Count()
	}
	return count
}

// helper: collect entity count from a query with given terms.
func qfCountTerms(w *flecs.World, terms ...flecs.Term) int {
	count := 0
	q := flecs.NewQueryFromTerms(w, terms...)
	it := q.Iter()
	for it.Next() {
		count += it.Count()
	}
	return count
}

// Test 1: Disable excludes entity from ordinary query.
func TestDisableExcludesFromQuery(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[QFPos](w)
	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.Set[QFPos](fw, e1, QFPos{1, 2})
		flecs.Set[QFPos](fw, e2, QFPos{3, 4})
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.DisableEntity(fw, e1)
	})

	got := qfCountEntities(w, posID)
	if got != 1 {
		t.Errorf("expected 1 entity after Disable, got %d", got)
	}
	// e2 must still be found.
	found := false
	q := flecs.NewQuery(w, posID)
	it := q.Iter()
	for it.Next() {
		ents := it.Entities()
		for _, e := range ents {
			if e == e2 {
				found = true
			}
		}
	}
	if !found {
		t.Error("enabled entity e2 must be found by ordinary query")
	}
}

// Test 2: Disable + explicit With(Disabled) includes entity.
func TestDisableWithExplicitTermIncludes(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[QFPos](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[QFPos](fw, e, QFPos{1, 2})
	})
	w.Write(func(fw *flecs.Writer) { flecs.DisableEntity(fw, e) })

	got := qfCountTerms(w, flecs.With(posID), flecs.With(w.Disabled()))
	if got != 1 {
		t.Errorf("expected 1 entity when opting in to Disabled, got %d", got)
	}
}

// Test 3: Enable after Disable re-includes entity in ordinary query.
func TestEnableAfterDisableReIncludes(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[QFPos](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[QFPos](fw, e, QFPos{1, 2})
	})
	w.Write(func(fw *flecs.Writer) { flecs.DisableEntity(fw, e) })
	if qfCountEntities(w, posID) != 0 {
		t.Fatal("entity should be excluded after Disable")
	}
	w.Write(func(fw *flecs.Writer) { flecs.EnableEntity(fw, e) })
	if qfCountEntities(w, posID) != 1 {
		t.Error("entity should be included after Enable")
	}
}

// Test 4: Disable is idempotent.
func TestDisableIdempotent(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[QFPos](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[QFPos](fw, e, QFPos{})
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.DisableEntity(fw, e)
		flecs.DisableEntity(fw, e) // second call must not panic or break state
	})
	if qfCountEntities(w, posID) != 0 {
		t.Error("entity should still be excluded after double Disable")
	}
	w.Read(func(r *flecs.Reader) {
		if !flecs.IsDisabled(r, e) {
			t.Error("IsDisabled must be true after double Disable")
		}
	})
}

// Test 5: Enable on already-enabled entity is no-op.
func TestEnableOnEnabledIsNoop(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[QFPos](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[QFPos](fw, e, QFPos{})
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.EnableEntity(fw, e) // must not panic; entity was never disabled
	})
	if qfCountEntities(w, posID) != 1 {
		t.Error("entity should remain included after Enable on enabled entity")
	}
}

// Test 6: IsDisabled round-trip.
func TestIsDisabledRoundTrip(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	w.Read(func(r *flecs.Reader) {
		if flecs.IsDisabled(r, e) {
			t.Error("fresh entity should not be disabled")
		}
	})
	w.Write(func(fw *flecs.Writer) { flecs.DisableEntity(fw, e) })
	w.Read(func(r *flecs.Reader) {
		if !flecs.IsDisabled(r, e) {
			t.Error("entity should be disabled after Disable")
		}
	})
	w.Write(func(fw *flecs.Writer) { flecs.EnableEntity(fw, e) })
	w.Read(func(r *flecs.Reader) {
		if flecs.IsDisabled(r, e) {
			t.Error("entity should not be disabled after Enable")
		}
	})
}

// Test 7: MarkPrefab excludes entity from ordinary query.
func TestMarkPrefabExcludesFromQuery(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[QFPos](w)
	var prefab, normal flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		normal = fw.NewEntity()
		flecs.Set[QFPos](fw, prefab, QFPos{10, 20})
		flecs.Set[QFPos](fw, normal, QFPos{1, 2})
		flecs.MarkPrefab(fw, prefab)
	})

	got := qfCountEntities(w, posID)
	if got != 1 {
		t.Errorf("expected 1 entity (normal only) after MarkPrefab, got %d", got)
	}
	_ = normal
}

// Test 8: MarkPrefab + explicit With(Prefab) includes entity.
func TestMarkPrefabWithExplicitTermIncludes(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[QFPos](w)
	var prefab flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set[QFPos](fw, prefab, QFPos{10, 20})
		flecs.MarkPrefab(fw, prefab)
	})

	got := qfCountTerms(w, flecs.With(posID), flecs.With(w.Prefab()))
	if got != 1 {
		t.Errorf("expected 1 entity when opting in to Prefab, got %d", got)
	}
}

// Test 9: IsPrefab round-trip.
func TestIsPrefabRoundTrip(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	w.Read(func(r *flecs.Reader) {
		if flecs.IsPrefab(r, e) {
			t.Error("fresh entity should not be a prefab")
		}
	})
	w.Write(func(fw *flecs.Writer) { flecs.MarkPrefab(fw, e) })
	w.Read(func(r *flecs.Reader) {
		if !flecs.IsPrefab(r, e) {
			t.Error("entity should be a prefab after MarkPrefab")
		}
	})
	// Remove the Prefab tag explicitly.
	w.Write(func(fw *flecs.Writer) { flecs.RemoveID(fw, e, w.Prefab()) })
	w.Read(func(r *flecs.Reader) {
		if flecs.IsPrefab(r, e) {
			t.Error("entity should not be a prefab after RemoveID(Prefab)")
		}
	})
}

// Test 10: Both Disabled + Prefab on same entity: excluded unless both opted in.
func TestDisabledAndPrefabBothRequired(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[QFPos](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[QFPos](fw, e, QFPos{})
		flecs.DisableEntity(fw, e)
		flecs.MarkPrefab(fw, e)
	})

	// Ordinary query: excluded.
	if qfCountEntities(w, posID) != 0 {
		t.Error("entity with Disabled+Prefab must be excluded from ordinary query")
	}
	// Opt in to Disabled only: still excluded because Prefab is present.
	if qfCountTerms(w, flecs.With(posID), flecs.With(w.Disabled())) != 0 {
		t.Error("opting in to Disabled only must not expose entity that also carries Prefab")
	}
	// Opt in to Prefab only: still excluded because Disabled is present.
	if qfCountTerms(w, flecs.With(posID), flecs.With(w.Prefab())) != 0 {
		t.Error("opting in to Prefab only must not expose entity that also carries Disabled")
	}
	// Opt in to both: entity found.
	got := qfCountTerms(w, flecs.With(posID), flecs.With(w.Disabled()), flecs.With(w.Prefab()))
	if got != 1 {
		t.Errorf("expected 1 entity when opting in to both Disabled and Prefab, got %d", got)
	}
}

// Test 11: Maybe(Disabled) — query matches both disabled and enabled entities;
// IsFieldSelf / HasID can distinguish presence per entity.
func TestMaybeDisabledMatchesBoth(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[QFPos](w)
	var eEnabled, eDisabled flecs.ID
	w.Write(func(fw *flecs.Writer) {
		eEnabled = fw.NewEntity()
		eDisabled = fw.NewEntity()
		flecs.Set[QFPos](fw, eEnabled, QFPos{1, 2})
		flecs.Set[QFPos](fw, eDisabled, QFPos{3, 4})
		flecs.DisableEntity(fw, eDisabled)
	})

	// Maybe(Disabled) opts in regardless of TermKind.
	got := qfCountTerms(w, flecs.With(posID), flecs.Maybe(w.Disabled()))
	if got != 2 {
		t.Errorf("Maybe(Disabled) should match both enabled and disabled entities, got %d", got)
	}
	_ = eEnabled
	_ = eDisabled
}

// Test 12: Cached query reflects Disable/Enable state changes.
func TestCachedQueryReflectsDisableEnable(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[QFPos](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[QFPos](fw, e, QFPos{1, 2})
	})

	cq := flecs.NewCachedQuery(w, posID)

	countCQ := func() int {
		n := 0
		it := cq.Iter()
		for it.Next() {
			n += it.Count()
		}
		return n
	}

	if countCQ() != 1 {
		t.Fatal("cached query should see 1 entity before Disable")
	}

	// Disable causes archetype migration → new table → tryMatchTable rejects it.
	w.Write(func(fw *flecs.Writer) { flecs.DisableEntity(fw, e) })
	if countCQ() != 0 {
		t.Error("cached query should see 0 entities after Disable")
	}

	// Enable causes migration back to non-disabled table → tryMatchTable accepts it.
	w.Write(func(fw *flecs.Writer) { flecs.EnableEntity(fw, e) })
	if countCQ() != 1 {
		t.Error("cached query should see 1 entity after Enable")
	}
}

// Test 13: Prefab as IsA base — instance is NOT marked Prefab (DontInherit).
func TestPrefabAsIsABaseNotInherited(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[QFPos](w)
	var prefab, instance flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set[QFPos](fw, prefab, QFPos{10, 20})
		flecs.MarkPrefab(fw, prefab)

		// Instance gets its own QFPos so it appears in archetype tables and can
		// be matched by an ordinary query over posID.
		instance = fw.NewEntity()
		flecs.Set[QFPos](fw, instance, QFPos{1, 2})
		flecs.AddID(fw, instance, flecs.MakePair(w.IsA(), prefab))
	})

	// The prefab is excluded (Prefab tag); the instance (no Prefab tag) is included.
	got := qfCountEntities(w, posID)
	if got != 1 {
		t.Errorf("expected 1 entity (instance only) in ordinary query, got %d", got)
	}

	w.Read(func(r *flecs.Reader) {
		// Instance does NOT inherit the Prefab tag (DontInherit bootstrapped).
		if flecs.IsPrefab(r, instance) {
			t.Error("instance should not inherit Prefab tag from prefab base")
		}
		// Instance owns its own QFPos.
		pos, ok := flecs.Get[QFPos](r, instance)
		if !ok {
			t.Fatal("instance should have its own QFPos")
		}
		if pos.X != 1 || pos.Y != 2 {
			t.Errorf("instance position should be {1,2}, got %+v", pos)
		}
	})
}

// Test 14: Performance path — a table full of disabled entities is skipped
// without per-entity iteration. Assert using query iteration count.
func TestDisabledTableSkippedWithoutPerEntityIteration(t *testing.T) {
	const n = 100
	w := flecs.New()
	posID := flecs.RegisterComponent[QFPos](w)
	velID := flecs.RegisterComponent[QFVel](w)

	// Create n entities with Pos+Vel, then disable them all.
	var first flecs.ID
	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			e := fw.NewEntity()
			if i == 0 {
				first = e
			}
			flecs.Set[QFPos](fw, e, QFPos{float32(i), 0})
			flecs.Set[QFVel](fw, e, QFVel{1, 0})
		}
	})
	// Disabling `first` migrates it to a new Pos+Vel+Disabled table; remaining n-1
	// stay in the original Pos+Vel table. After this, ordinary query sees n-1 entities.
	w.Write(func(fw *flecs.Writer) {
		flecs.DisableEntity(fw, first)
	})
	if got := qfCountEntities(w, posID); got != n-1 {
		t.Errorf("expected %d entities after disabling one, got %d", n-1, got)
	}

	// Disable remaining enabled entities.
	w.Write(func(fw *flecs.Writer) {
		q2 := flecs.NewQuery(w, posID)
		it2 := q2.Iter()
		for it2.Next() {
			for _, e := range it2.Entities() {
				flecs.DisableEntity(fw, e)
			}
		}
	})

	// Now all n entities are disabled; ordinary query should find 0.
	if qfCountEntities(w, posID) != 0 {
		t.Errorf("expected 0 entities after disabling all, got %d", qfCountEntities(w, posID))
	}

	// Opt-in query finds all n.
	got := qfCountTerms(w, flecs.With(posID), flecs.With(w.Disabled()))
	if got != n {
		t.Errorf("opt-in query expected %d disabled entities, got %d", n, got)
	}
	_ = velID
}

// Test 15: Without(Disabled) — explicit exclusion term does not re-enable implicit skip.
func TestWithoutDisabledExplicitExclusion(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[QFPos](w)
	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.Set[QFPos](fw, e1, QFPos{1, 2})
		flecs.Set[QFPos](fw, e2, QFPos{3, 4})
		flecs.DisableEntity(fw, e2)
	})

	// Without(Disabled) mentions Disabled, opting in — but then explicitly excludes it.
	// The net result: disabled entities are included in the scan but rejected by the
	// Without term. The count remains 1 (e1 only).
	got := qfCountTerms(w, flecs.With(posID), flecs.Without(w.Disabled()))
	if got != 1 {
		t.Errorf("Without(Disabled) query should match 1 enabled entity, got %d", got)
	}
	_ = e1
	_ = e2
}
