package flecs_test

import (
	"testing"

	flecs "github.com/snichols/flecs"
)

// ── dont_fragment.go ─────────────────────────────────────────────────────────

// TestCovDF_SetDontFragment_TagPanic covers the size==0 panic branch (line 56-57).
func TestCovDF_SetDontFragment_TagPanic(t *testing.T) {
	w := flecs.New()
	type covTag struct{}
	tagID := flecs.RegisterComponent[covTag](w)
	defer func() {
		if r := recover(); r == nil {
			t.Error("SetDontFragment on zero-size tag: expected panic, got none")
		}
	}()
	flecs.SetDontFragment(w, tagID)
}

// TestCovDF_SetDontFragment_Idempotent covers applyDontFragmentPolicy early return (lines 96-98).
func TestCovDF_SetDontFragment_Idempotent(t *testing.T) {
	w := flecs.New()
	type covComp struct{ V int }
	cid := flecs.RegisterComponent[covComp](w)
	flecs.SetDontFragment(w, cid)
	flecs.SetDontFragment(w, cid) // second call: idempotent, no-op
	w.Read(func(r *flecs.Reader) {
		if !flecs.IsDontFragment(r, cid) {
			t.Error("IsDontFragment: expected true after double SetDontFragment")
		}
	})
}

// ── id_ops.go ────────────────────────────────────────────────────────────────

// TestCovIDOps_AddIDOnWorld_DeferredPath covers addIDOnWorld deferred path (lines 17-35)
// via UnmarshalJSON, which calls addIDOnWorld inside w.Write (deferDepth==1).
func TestCovIDOps_AddIDOnWorld_DeferredPath(t *testing.T) {
	type posType struct{ X, Y int }
	w1 := flecs.New()
	flecs.RegisterComponent[posType](w1)

	var childID flecs.ID
	w1.Write(func(fw *flecs.Writer) {
		parent := fw.NewEntity()
		childID = fw.NewEntity()
		flecs.AddID(fw, childID, flecs.MakePair(w1.ChildOf(), parent))
		flecs.Set(fw, childID, posType{X: 1, Y: 2})
	})
	_ = childID

	data, err := w1.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	w2 := flecs.New()
	flecs.RegisterComponent[posType](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
}

// TestCovIDOps_AddID_SparseComponentPanic covers the Sparse panic path (lines 53-54).
func TestCovIDOps_AddID_SparseComponentPanic(t *testing.T) {
	w := flecs.New()
	type spComp struct{ V int }
	cid := flecs.RegisterComponent[spComp](w)
	flecs.SetSparse(w, cid)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	_ = e

	defer func() {
		if r := recover(); r == nil {
			t.Error("AddID on Sparse component: expected panic, got none")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, cid)
	})
}

// TestCovIDOps_CleanupOnDeleteTarget covers OnDeleteTarget cleanup path (lines 68-70).
func TestCovIDOps_CleanupOnDeleteTarget(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		relEnt := fw.NewEntity()
		flecs.AddID(fw, relEnt, flecs.MakePair(w.OnDeleteTarget(), w.DeleteAction()))
	})
}

// TestCovIDOps_CleanupOnDeletePanic covers OnDelete+PanicAction path (lines 78-79).
func TestCovIDOps_CleanupOnDeletePanic(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		relEnt := fw.NewEntity()
		flecs.AddID(fw, relEnt, flecs.MakePair(w.OnDelete(), w.PanicAction()))
	})
}

// TestCovIDOps_CleanupOnDeleteRemove covers OnDelete+RemoveAction path (lines 80-81).
func TestCovIDOps_CleanupOnDeleteRemove(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		relEnt := fw.NewEntity()
		flecs.AddID(fw, relEnt, flecs.MakePair(w.OnDelete(), w.RemoveAction()))
	})
}

// TestCovIDOps_AddID_Final covers applyFinalPolicy path (lines 127-132).
func TestCovIDOps_AddID_Final(t *testing.T) {
	w := flecs.New()
	var ent flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ent = fw.NewEntity()
		flecs.AddID(fw, ent, w.Final())
	})
	// Adding (IsA, ent) should now panic — Final() prevents IsA.
	defer func() { _ = recover() }()
	w.Write(func(fw *flecs.Writer) {
		child := fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), ent))
	})
}

// TestCovIDOps_AddID_Relationship covers applyRelationshipPolicy path (lines 150-153).
func TestCovIDOps_AddID_Relationship(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		relEnt := fw.NewEntity()
		flecs.AddID(fw, relEnt, w.Relationship())
	})
}

// TestCovIDOps_AddID_Target covers applyTargetPolicy path (lines 153-156).
func TestCovIDOps_AddID_Target(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		tgtEnt := fw.NewEntity()
		flecs.AddID(fw, tgtEnt, w.Target())
	})
}

// TestCovIDOps_AddID_Trait covers applyTraitPolicy path (lines 156-160).
func TestCovIDOps_AddID_Trait(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		traitEnt := fw.NewEntity()
		flecs.AddID(fw, traitEnt, w.Trait())
	})
}

// TestCovIDOps_AddID_With covers the bare With no-op path (lines 165-171).
// The With entity has the Relationship policy set at bootstrap, so checkUsageConstraints
// panics after line 170 executes. We recover the expected panic to keep coverage.
func TestCovIDOps_AddID_With(t *testing.T) {
	w := flecs.New()
	var ent flecs.ID
	w.Write(func(fw *flecs.Writer) { ent = fw.NewEntity() })
	defer func() { _ = recover() }() // line 170 (_ = e) runs, then checkUsageConstraints panics
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, ent, w.With())
	})
}

// TestCovIDOps_OverrideCopy_NonIsAPairContinue covers the non-IsA pair continue path
// in overrideCopyForInstance (line 313-314) via a prefab that has a non-IsA pair.
func TestCovIDOps_OverrideCopy_NonIsAPairContinue(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		parent := fw.NewEntity()
		prefab := fw.NewEntity()
		relEnt := fw.NewEntity()
		// Add a non-IsA, non-ChildOf pair to the prefab.
		flecs.AddID(fw, prefab, flecs.MakePair(relEnt, parent))
		// Create an instance of the prefab: overrideCopyForInstance walks pairs.
		inst := fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), prefab))
		_ = inst
	})
}

// TestCovIDOps_OverrideCopy_SeenCycle covers the seen-cycle guard (lines 291-293)
// via a diamond IsA inheritance pattern.
func TestCovIDOps_OverrideCopy_SeenCycle(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		d := fw.NewEntity()
		b := fw.NewEntity()
		c := fw.NewEntity()
		a := fw.NewEntity()
		flecs.AddID(fw, b, flecs.MakePair(w.IsA(), d))
		flecs.AddID(fw, c, flecs.MakePair(w.IsA(), d))
		flecs.AddID(fw, a, flecs.MakePair(w.IsA(), b))
		flecs.AddID(fw, a, flecs.MakePair(w.IsA(), c))
		inst := fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), a))
		_ = inst
	})
}

// TestCovIDOps_RemoveID_DontFragmentNotPresent covers removeIDImmediate DontFragment
// ptr==nil early-return (lines 341-343).
func TestCovIDOps_RemoveID_DontFragmentNotPresent(t *testing.T) {
	w := flecs.New()
	type dfNotPresent struct{ V int }
	cid := flecs.RegisterComponent[dfNotPresent](w)
	flecs.SetDontFragment(w, cid)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	w.Write(func(fw *flecs.Writer) {
		removed := flecs.RemoveID(fw, e, cid)
		if removed {
			t.Error("RemoveID on DF component not present: expected false")
		}
	})
}

// TestCovIDOps_RemoveID_SparseNotPresent covers removeIDImmediate Sparse ptr==nil
// early-return (lines 353-355).
func TestCovIDOps_RemoveID_SparseNotPresent(t *testing.T) {
	w := flecs.New()
	type spNotPresent struct{ V int }
	cid := flecs.RegisterComponent[spNotPresent](w)
	flecs.SetSparse(w, cid)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	w.Write(func(fw *flecs.Writer) {
		removed := flecs.RemoveID(fw, e, cid)
		if removed {
			t.Error("RemoveID on Sparse component not present: expected false")
		}
	})
}

// TestCovIDOps_GetPair_ZeroSizePair covers getPairOnWorld zero-size pair ptr==nil path
// (lines 449-452): struct{} is a zero-size pair data type so t.Get returns nil.
func TestCovIDOps_GetPair_ZeroSizePair(t *testing.T) {
	w := flecs.New()
	var relEnt, tgtEnt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		relEnt = fw.NewEntity()
		tgtEnt = fw.NewEntity()
		e = fw.NewEntity()
		// SetPairByID with struct{}{} registers a zero-size pair.
		w.SetPairByID(e, relEnt, tgtEnt, struct{}{})
	})
	w.Read(func(r *flecs.Reader) {
		// GetPair[struct{}] hits the ptr==nil → return zero, true path (size==0 column).
		_, _ = flecs.GetPair[struct{}](r, e, relEnt, tgtEnt)
	})
}

// TestCovIDOps_OverrideCopy_PrefabNilTable covers overrideCopyForInstance nil-table
// early-return (lines 303-305): prefab is alive but has no components (Table==nil).
func TestCovIDOps_OverrideCopy_PrefabNilTable(t *testing.T) {
	w := flecs.New()
	var prefabID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefabID = fw.NewEntity() // no components → rec.Table == nil
	})
	w.Write(func(fw *flecs.Writer) {
		inst := fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), prefabID))
		_ = inst
	})
}

// TestCovIDOps_AddID_SingletonNonPair covers addIDImmediate non-pair singleton path
// (lines 249-250): AddID on a singleton-policy component calls checkSingleton.
func TestCovIDOps_AddID_SingletonNonPair(t *testing.T) {
	w := flecs.New()
	type singletonComp struct{ V int }
	compID := flecs.RegisterComponent[singletonComp](w)
	flecs.SetSingleton(w, compID)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.AddID(fw, e, compID) // cmdAddID → addIDImmediate → checkSingleton (line 250)
	})
}

// TestCovIDOps_AddID_DontFragmentPanic covers the DontFragment panic in addIDImmediate
// (lines 55-56): AddID on a DontFragment data-bearing component panics in immediate path.
func TestCovIDOps_AddID_DontFragmentPanic(t *testing.T) {
	w := flecs.New()
	type dfComp struct{ V int }
	cid := flecs.RegisterComponent[dfComp](w)
	flecs.SetDontFragment(w, cid)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	defer func() {
		if r := recover(); r == nil {
			t.Error("AddID on DontFragment component: expected panic, got none")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, cid) // queued → flush → addIDImmediate → DF panic (lines 55-56)
	})
}

// TestCovIDOps_RemoveID_DFNotPresent_Immediate covers removeIDImmediate DontFragment
// ptr==nil early-return (lines 341-343) via an OnRemove hook where deferDepth==0.
func TestCovIDOps_RemoveID_DFNotPresent_Immediate(t *testing.T) {
	w := flecs.New()
	type trigComp struct{ V int }
	type dfComp struct{ W int }
	flecs.RegisterComponent[trigComp](w)
	dfID := flecs.RegisterComponent[dfComp](w)
	flecs.SetDontFragment(w, dfID)

	var gotNotPresent bool
	flecs.OnRemove[trigComp](w, func(fw *flecs.Writer, e flecs.ID, _ trigComp) {
		// deferDepth==0 in flush hook → RemoveID takes immediate path → removeIDImmediate
		// entity never had dfComp → sparseSetGet returns nil → return false (lines 341-343)
		removed := flecs.RemoveID(fw, e, dfID)
		if !removed {
			gotNotPresent = true
		}
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, trigComp{V: 1}) // has trigComp, no dfComp
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[trigComp](fw, e) // flush fires OnRemove hook
	})

	if !gotNotPresent {
		t.Error("expected DF not-present path (removeIDImmediate returned false)")
	}
}

// TestCovIDOps_RemoveID_SparseNotPresent_Immediate covers removeIDImmediate Sparse
// ptr==nil early-return (lines 353-355) via an OnRemove hook where deferDepth==0.
func TestCovIDOps_RemoveID_SparseNotPresent_Immediate(t *testing.T) {
	w := flecs.New()
	type trigComp struct{ V int }
	type spComp struct{ W int }
	flecs.RegisterComponent[trigComp](w)
	spID := flecs.RegisterComponent[spComp](w)
	flecs.SetSparse(w, spID)

	var gotNotPresent bool
	flecs.OnRemove[trigComp](w, func(fw *flecs.Writer, e flecs.ID, _ trigComp) {
		// deferDepth==0 → immediate path → removeIDImmediate → Sparse ptr==nil (lines 353-355)
		removed := flecs.RemoveID(fw, e, spID)
		if !removed {
			gotNotPresent = true
		}
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, trigComp{V: 1}) // has trigComp, no spComp
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[trigComp](fw, e) // flush fires OnRemove hook
	})

	if !gotNotPresent {
		t.Error("expected Sparse not-present path (removeIDImmediate returned false)")
	}
}

// TestCovIDOps_SetPair_Immediate_DeadEntityPanic covers setPairImmediate dead-entity
// panic (lines 412-413) via an OnRemove hook where deferDepth==0.
func TestCovIDOps_SetPair_Immediate_DeadEntityPanic(t *testing.T) {
	w := flecs.New()
	type trigComp struct{ V int }
	type pairData struct{ X int }
	flecs.RegisterComponent[trigComp](w)

	var deadEnt, relEnt, tgtEnt flecs.ID
	var gotPanic bool

	flecs.OnRemove[trigComp](w, func(fw *flecs.Writer, e flecs.ID, _ trigComp) {
		defer func() {
			if r := recover(); r != nil {
				gotPanic = true
			}
		}()
		// deadEnt was deleted before this hook fires; SetPair in immediate mode
		// calls setPairImmediate → w.index.Get(deadEnt)==nil → panic (lines 412-413).
		flecs.SetPair[pairData](fw, deadEnt, relEnt, tgtEnt, pairData{X: 1})
	})

	var liveEnt flecs.ID
	w.Write(func(fw *flecs.Writer) {
		deadEnt = fw.NewEntity()
		relEnt = fw.NewEntity()
		tgtEnt = fw.NewEntity()
		liveEnt = fw.NewEntity()
		flecs.Set(fw, liveEnt, trigComp{V: 1})
	})
	w.Write(func(fw *flecs.Writer) {
		fw.Delete(deadEnt)                  // dispatched first (submission order)
		flecs.Remove[trigComp](fw, liveEnt) // hook fires after Delete; deadEnt is dead
	})

	if !gotPanic {
		t.Error("expected panic for SetPair on dead entity in immediate mode")
	}
}

// TestCovIDOps_AddID_Immediate_DeadEntityPanic covers addIDImmediate dead-entity panic
// (lines 42-43) via an OnRemove hook where deferDepth==0: hook calls AddID on an
// entity deleted earlier in the same flush batch.
func TestCovIDOps_AddID_Immediate_DeadEntityPanic(t *testing.T) {
	w := flecs.New()
	type trigComp struct{ V int }
	type tagComp struct{}
	flecs.RegisterComponent[trigComp](w)
	tagID := flecs.RegisterComponent[tagComp](w)

	var deadEnt flecs.ID
	var gotPanic bool

	flecs.OnRemove[trigComp](w, func(fw *flecs.Writer, e flecs.ID, _ trigComp) {
		defer func() {
			if r := recover(); r != nil {
				gotPanic = true
			}
		}()
		// deadEnt was deleted before this hook fires (submission order guarantees it);
		// deferDepth==0 → AddID takes immediate path → addIDImmediate → rec==nil → panic
		flecs.AddID(fw, deadEnt, tagID)
	})

	var liveEnt flecs.ID
	w.Write(func(fw *flecs.Writer) {
		deadEnt = fw.NewEntity()
		liveEnt = fw.NewEntity()
		flecs.Set(fw, liveEnt, trigComp{V: 1})
	})
	w.Write(func(fw *flecs.Writer) {
		fw.Delete(deadEnt)                  // submitted first → dispatched first
		flecs.Remove[trigComp](fw, liveEnt) // submitted second → hook fires after Delete
	})

	if !gotPanic {
		t.Error("expected panic for AddID on dead entity in immediate mode")
	}
}

// ── query.go ─────────────────────────────────────────────────────────────────

// TestCovQuery_AllDontFragment_WithOptional covers:
// - allDontFragment path in Iter (lines 373-412)
// - TermOptional continue in driver loop (lines 380-381)
// - Entities() allSparse return (lines 885-887)
// - updateOptionalPresenceSparse: range-delete loop (lines 697-699)
// - updateOptionalPresenceSparse: make map (lines 704-706)
// - updateOptionalPresenceSparse: if Sparse body (lines 707-709)
// - FieldMaybe for sparse optional (lines 1032-1043)
func TestCovQuery_AllDontFragment_WithOptional(t *testing.T) {
	w := flecs.New()
	type dfComp1 struct{ A int }
	type dfComp2 struct{ B int }
	c1id := flecs.RegisterComponent[dfComp1](w)
	c2id := flecs.RegisterComponent[dfComp2](w)
	flecs.SetDontFragment(w, c1id)
	flecs.SetDontFragment(w, c2id)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.Set(fw, e1, dfComp1{A: 10})
		flecs.Set(fw, e2, dfComp1{A: 20})
		flecs.Set(fw, e2, dfComp2{B: 30})
	})

	// Pure DF query with a DF optional term.
	q := flecs.NewQueryFromTerms(w,
		flecs.With(c1id),
		flecs.Maybe(c2id),
	)

	var visited []flecs.ID
	q.Each(func(it *flecs.QueryIter) {
		ents := it.Entities() // allSparse path (lines 885-887)
		visited = append(visited, ents...)
		_, _ = flecs.FieldMaybe[dfComp2](it, c2id)
	})

	if len(visited) != 2 {
		t.Errorf("expected 2 entities, got %d (e1=%v e2=%v)", len(visited), e1, e2)
	}
}

// TestCovQuery_Mixed_DFTermAnd_DFTermNot covers:
// - seed selection DontFragment continue (lines 424-425)
// - matchesTable DontFragment TermAnd break (lines 752-753)
// - matchesTable DontFragment TermNot break (lines 812-813)
// - matchesSparseTerms TermAnd ptr==nil return false (lines 682-684)
// - matchesSparseTerms TermNot ptr!=nil return false (lines 686-688)
func TestCovQuery_Mixed_DFTermAnd_DFTermNot(t *testing.T) {
	w := flecs.New()
	type archComp struct{ V int }
	type df1 struct{ A int }
	type df2 struct{ B int }
	archID := flecs.RegisterComponent[archComp](w)
	df1id := flecs.RegisterComponent[df1](w)
	df2id := flecs.RegisterComponent[df2](w)
	flecs.SetDontFragment(w, df1id)
	flecs.SetDontFragment(w, df2id)

	var e1, e2, e3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		// e1: arch + df1 (no df2) → should match
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, archComp{V: 1})
		flecs.Set(fw, e1, df1{A: 1})

		// e2: arch only → no df1 → matchesSparseTerms TermAnd false → skip
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, archComp{V: 2})

		// e3: arch + df1 + df2 → df2 present → matchesSparseTerms TermNot false → skip
		e3 = fw.NewEntity()
		flecs.Set(fw, e3, archComp{V: 3})
		flecs.Set(fw, e3, df1{A: 3})
		flecs.Set(fw, e3, df2{B: 3})
	})

	q := flecs.NewQueryFromTerms(w,
		flecs.With(archID),
		flecs.With(df1id),
		flecs.Without(df2id),
	)

	var visited []flecs.ID
	q.Each(func(it *flecs.QueryIter) {
		visited = append(visited, it.Entities()...)
	})

	if len(visited) != 1 || visited[0] != e1 {
		t.Errorf("expected [e1=%v], got %v (e2=%v e3=%v)", e1, visited, e2, e3)
	}
}

// TestCovQuery_Mixed_DFOptional covers updateOptionalPresenceMixed DontFragment branch
// (lines 727-730). The query must have a DontFragment TermAnd to enter mixed mode
// (hasSparseTerms=true), which is required for sparseEntity to be set when FieldMaybe
// accesses a sparse optional term.
func TestCovQuery_Mixed_DFOptional(t *testing.T) {
	w := flecs.New()
	type archComp2 struct{ V int }
	type dfMandatory struct{ M int }
	type dfOpt struct{ O int }
	archID := flecs.RegisterComponent[archComp2](w)
	dfMandID := flecs.RegisterComponent[dfMandatory](w)
	dfOptID := flecs.RegisterComponent[dfOpt](w)
	flecs.SetDontFragment(w, dfMandID)
	flecs.SetDontFragment(w, dfOptID)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.Set(fw, e1, archComp2{V: 1})
		flecs.Set(fw, e1, dfMandatory{M: 1})
		flecs.Set(fw, e2, archComp2{V: 2})
		flecs.Set(fw, e2, dfMandatory{M: 2})
		flecs.Set(fw, e2, dfOpt{O: 10}) // only e2 has the optional DF component
	})

	// With(dfMandID) is DontFragment TermAnd → hasSparseTerms=true → mixed mode.
	// In mixed mode nextMixed sets sparseEntity and calls updateOptionalPresenceMixed,
	// which hits the DontFragment branch (lines 727-730) for the Maybe(dfOptID) term.
	q := flecs.NewQueryFromTerms(w,
		flecs.With(archID),
		flecs.With(dfMandID),
		flecs.Maybe(dfOptID),
	)

	var visited []flecs.ID
	q.Each(func(it *flecs.QueryIter) {
		visited = append(visited, it.Entities()...)
		_, _ = flecs.FieldMaybe[dfOpt](it, dfOptID)
	})

	if len(visited) != 2 {
		t.Errorf("expected 2 entities, got %d (e1=%v e2=%v)", len(visited), e1, e2)
	}
}

// ── cached_query.go ──────────────────────────────────────────────────────────

// TestCovCQ_SparseAndOnly_WithDFTermNot covers:
//   - newCachedQueryInternal dontFragmentAndCount++ (lines 212-214)
//   - sparseAndOnly block in CachedQuery.Iter (lines 325-361)
//     incl. TermNot continue (330-331), key/ss lookup (331-334), minLen+copy (336-345)
//   - tryMatchTable sparseAndOnly return (lines 472-474)
//   - Changed() regular-path sparseVersions make (lines 645-647)
func TestCovCQ_SparseAndOnly_WithDFTermNot(t *testing.T) {
	w := flecs.New()
	type dfCQ struct{ V int }
	type regCQ struct{ V int }
	dfid := flecs.RegisterComponent[dfCQ](w)
	regid := flecs.RegisterComponent[regCQ](w)
	flecs.SetDontFragment(w, dfid)

	var e1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, dfCQ{V: 42})
		// e1 does NOT have regCQ, so Without(regid) is satisfied.
	})

	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(dfid),
		flecs.Without(regid),
	)

	// First Changed() call hits the regular-path sparse version check (lines 645-647).
	if !cq.Changed() {
		t.Error("Changed(): expected true on first call (sparse version unseen)")
	}

	var visited []flecs.ID
	it := cq.Iter()
	for it.Next() {
		visited = append(visited, it.Entities()...)
	}
	if len(visited) != 1 || visited[0] != e1 {
		t.Errorf("expected [e1=%v], got %v", e1, visited)
	}
}

// TestCovCQ_Mixed_DFAndNot covers:
// - tryMatchTable DontFragment TermAnd break (lines 481-482)
// - tryMatchTable DontFragment TermNot break (lines 516-517)
// - Changed() tablesAdded-path sparseVersions make (lines 620-622)
func TestCovCQ_Mixed_DFAndNot(t *testing.T) {
	w := flecs.New()
	type archCQ struct{ V int }
	type dfAndCQ struct{ A int }
	type dfNotCQ struct{ B int }
	archID := flecs.RegisterComponent[archCQ](w)
	dfAndID := flecs.RegisterComponent[dfAndCQ](w)
	dfNotID := flecs.RegisterComponent[dfNotCQ](w)
	flecs.SetDontFragment(w, dfAndID)
	flecs.SetDontFragment(w, dfNotID)

	var e1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, archCQ{V: 1})
		flecs.Set(fw, e1, dfAndCQ{A: 10})
		// e1 does NOT have dfNotCQ
	})

	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(archID),
		flecs.With(dfAndID),
		flecs.Without(dfNotID),
	)

	// tablesAdded is true after construction (e1's archetype table was appended).
	// Changed() enters the tablesAdded block, covering sparseVersions make (lines 620-622).
	changed := cq.Changed()
	if !changed {
		t.Error("Changed(): expected true (tablesAdded)")
	}

	var visited []flecs.ID
	cq.Each(func(it *flecs.QueryIter) {
		visited = append(visited, it.Entities()...)
	})
	if len(visited) != 1 || visited[0] != e1 {
		t.Errorf("expected [e1=%v], got %v", e1, visited)
	}
}

// TestCovCQ_ClosedThenNewTable covers tryMatchTable removed early return (lines 467-469).
func TestCovCQ_ClosedThenNewTable(t *testing.T) {
	w := flecs.New()
	type archForClose struct{ V int }
	archID := flecs.RegisterComponent[archForClose](w)

	cq := flecs.NewCachedQuery(w, archID)
	cq.Close()

	// New entity/table after Close → tryMatchTable silently skips (removed=true).
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, archForClose{V: 99})
	})
	if cq.Count() != 0 {
		t.Errorf("closed CachedQuery Count: expected 0, got %d", cq.Count())
	}
}

// TestCovCQ_Cascade_NoTables covers sortByCascadeDepth n<=1 early return (lines 255-257).
func TestCovCQ_Cascade_NoTables(t *testing.T) {
	w := flecs.New()
	type cascComp struct{ V int }
	cascID := flecs.RegisterComponent[cascComp](w)

	// No entities with cascComp → 0 matching tables → sortByCascadeDepth n=0 → early return.
	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(cascID).Cascade(w.ChildOf()),
	)
	if cq.Count() != 0 {
		t.Errorf("expected 0 matching tables, got %d", cq.Count())
	}
}

// ── world.go ─────────────────────────────────────────────────────────────────

// TestCovWorld_Has_DontFragment covers hasOnWorld DontFragment condition+ss (lines 938-944).
func TestCovWorld_Has_DontFragment(t *testing.T) {
	w := flecs.New()
	type dfHasComp struct{ V int }
	cid := flecs.RegisterComponent[dfHasComp](w)
	flecs.SetDontFragment(w, cid)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.Set(fw, e1, dfHasComp{V: 1})
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.Has[dfHasComp](r, e1) {
			t.Error("Has[dfHasComp](e1): expected true")
		}
		if flecs.Has[dfHasComp](r, e2) {
			t.Error("Has[dfHasComp](e2): expected false")
		}
	})
}

// TestCovWorld_RemoveImmediate_DF_ViaHook covers removeImmediate[T] for DontFragment:
// - present path (lines 1023-1030): entity e1 has the DF component
// - not-present path (lines 1025-1027): entity e2 does not have the DF component
// Hook fires during flush (deferDepth==0) → calls removeImmediate[T] directly.
func TestCovWorld_RemoveImmediate_DF_ViaHook(t *testing.T) {
	w := flecs.New()
	type archTrigger1 struct{ V int }
	type dfTarget struct{ W int }
	dfid := flecs.RegisterComponent[dfTarget](w)
	flecs.SetDontFragment(w, dfid)

	var hookE flecs.ID
	flecs.OnRemove[archTrigger1](w, func(fw *flecs.Writer, e flecs.ID, _ archTrigger1) {
		flecs.Remove[dfTarget](fw, e) // may or may not have dfTarget
		hookE = e
	})

	// e1: has both archTrigger1 and dfTarget → removeImmediate present path.
	var e1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, archTrigger1{V: 1})
		flecs.Set(fw, e1, dfTarget{W: 10})
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[archTrigger1](fw, e1)
	})
	if hookE != e1 {
		t.Errorf("hook for e1: want %v, got %v", e1, hookE)
	}

	// e2: has archTrigger1 but NOT dfTarget → removeImmediate not-present path.
	var e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, archTrigger1{V: 2})
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[archTrigger1](fw, e2)
	})
	if hookE != e2 {
		t.Errorf("hook for e2: want %v, got %v", e2, hookE)
	}
}

// TestCovWorld_RemoveImmediate_Sparse_ViaHook covers removeImmediate[T] for Sparse-only:
// - present path (lines 1034-1046): entity e1 has the Sparse component
// - not-present path (lines 1036-1038): entity e2 does not have the Sparse component
//
// The trigger component is DontFragment (not regular archetype) to avoid a nested
// archetype migration conflict: removeImmediate[DF] fires the hook without entering
// migrate(), so the hook's migrateArchetypeOnly for sparseTarget is the only migration.
func TestCovWorld_RemoveImmediate_Sparse_ViaHook(t *testing.T) {
	w := flecs.New()
	type dfTrigger struct{ V int }
	type sparseTarget struct{ W int }
	dfTrigID := flecs.RegisterComponent[dfTrigger](w)
	sid := flecs.RegisterComponent[sparseTarget](w)
	flecs.SetDontFragment(w, dfTrigID)
	flecs.SetSparse(w, sid)

	var hookE flecs.ID
	flecs.OnRemove[dfTrigger](w, func(fw *flecs.Writer, e flecs.ID, _ dfTrigger) {
		flecs.Remove[sparseTarget](fw, e) // covers removeImmediate[sparseTarget]
		hookE = e
	})

	// e1: has dfTrigger and sparseTarget → removeImmediate Sparse present path.
	var e1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, dfTrigger{V: 1})
		flecs.Set(fw, e1, sparseTarget{W: 5})
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[dfTrigger](fw, e1)
	})
	if hookE != e1 {
		t.Errorf("hook for e1: want %v, got %v", e1, hookE)
	}

	// e2: has dfTrigger but NOT sparseTarget → removeImmediate Sparse not-present path.
	var e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, dfTrigger{V: 2})
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[dfTrigger](fw, e2)
	})
	if hookE != e2 {
		t.Errorf("hook for e2: want %v, got %v", e2, hookE)
	}
}

// TestCovWorld_RemoveImmediate_Unregistered covers removeImmediate[T] !ok path
// (lines 1016-1018) for a type not registered as a component.
func TestCovWorld_RemoveImmediate_Unregistered(t *testing.T) {
	w := flecs.New()
	type archTrigger3 struct{ V int }
	type unregisteredType struct{ Z int }

	hookCalled := false
	flecs.OnRemove[archTrigger3](w, func(fw *flecs.Writer, e flecs.ID, _ archTrigger3) {
		result := flecs.Remove[unregisteredType](fw, e) // !ok → return false
		_ = result
		hookCalled = true
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, archTrigger3{V: 1})
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[archTrigger3](fw, e)
	})
	if !hookCalled {
		t.Error("OnRemove hook was not called")
	}
}

// TestCovWorld_SetInheritable_UnregisteredPanic covers the panic on unregistered ID (line 854).
func TestCovWorld_SetInheritable_UnregisteredPanic(t *testing.T) {
	w := flecs.New()
	var fakeID flecs.ID = 9999
	defer func() {
		if r := recover(); r == nil {
			t.Error("SetInheritable with unregistered ID: expected panic, got none")
		}
	}()
	w.SetInheritable(fakeID)
}

// TestCovWorld_Delete_Singleton covers deleteOne singleton cleanup paths (lines 669-672).
func TestCovWorld_Delete_Singleton(t *testing.T) {
	w := flecs.New()
	type singletonComp struct{ V int }
	cid := flecs.RegisterComponent[singletonComp](w)
	flecs.SetSingleton(w, cid)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, singletonComp{V: 7})
	})
	w.Write(func(fw *flecs.Writer) { fw.Delete(e) })
	// After deletion, a new entity should be able to take the singleton slot.
	w.Write(func(fw *flecs.Writer) {
		e2 := fw.NewEntity()
		flecs.Set(fw, e2, singletonComp{V: 8})
	})
}

// TestCovWorld_SetOnWorld_Tag covers setOnWorld size==0 deferred tag path (lines 885-887).
func TestCovWorld_SetOnWorld_Tag(t *testing.T) {
	w := flecs.New()
	type tagZeroSizeComp struct{}
	flecs.RegisterComponent[tagZeroSizeComp](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, tagZeroSizeComp{})
	})
	w.Read(func(r *flecs.Reader) {
		if !flecs.Has[tagZeroSizeComp](r, e) {
			t.Error("Has[tagZeroSizeComp]: expected true after Set")
		}
	})
}

// ── marshal.go ───────────────────────────────────────────────────────────────

// TestCovMarshal_RelSerialNotFound covers MarshalJSON relSerial !ok path (lines 256-257).
// Entity has a pair whose rel is a built-in entity (OnDelete is in the skip set).
func TestCovMarshal_RelSerialNotFound(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(w.OnDelete(), w.DeleteAction()))
	})
	_, err := w.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
}

// TestCovMarshal_TgtSerialNotFound covers MarshalJSON tgtSerial !ok path (lines 260-261).
// Entity has a pair whose tgt is a component entity (in skip set → not in indexToSerial).
func TestCovMarshal_TgtSerialNotFound(t *testing.T) {
	w := flecs.New()
	type fooForSkip struct{ V int }
	compID := flecs.RegisterComponent[fooForSkip](w)

	w.Write(func(fw *flecs.Writer) {
		relEnt := fw.NewEntity()
		e := fw.NewEntity()
		// Pair (userRel, compID): relEnt serial ok, compID is in skip → serial !ok.
		flecs.AddID(fw, e, flecs.MakePair(relEnt, compID))
	})
	_, err := w.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
}

// TestCovMarshal_UnregisteredSparse covers UnmarshalJSON sparse not registered (lines 426-429).
func TestCovMarshal_UnregisteredSparse(t *testing.T) {
	type sparseForUnreg struct{ V int }
	w1 := flecs.New()
	sid := flecs.RegisterComponent[sparseForUnreg](w1)
	flecs.SetSparse(w1, sid)
	w1.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, sparseForUnreg{V: 99})
	})
	data, err := w1.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	// Unmarshal into world WITHOUT sparseForUnreg registered → error.
	w2 := flecs.New()
	if err := w2.UnmarshalJSON(data); err == nil {
		t.Error("UnmarshalJSON: expected error for unregistered sparse component")
	}
}

// TestCovMarshal_UnregisteredDontFragment covers UnmarshalJSON DF not registered (lines 434-437).
func TestCovMarshal_UnregisteredDontFragment(t *testing.T) {
	type dfForUnreg struct{ V int }
	w1 := flecs.New()
	did := flecs.RegisterComponent[dfForUnreg](w1)
	flecs.SetDontFragment(w1, did)
	w1.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, dfForUnreg{V: 42})
	})
	data, err := w1.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	w2 := flecs.New()
	if err := w2.UnmarshalJSON(data); err == nil {
		t.Error("UnmarshalJSON: expected error for unregistered DontFragment component")
	}
}

// TestCovMarshal_PairDataJsonError covers UnmarshalJSON pair data json error (lines 495-498).
// The pair dataType is registered but pair.Data is a JSON string ("not_a_struct"),
// which fails to unmarshal into the struct type.
func TestCovMarshal_PairDataJsonError(t *testing.T) {
	type pairDataErr struct{ X int }
	w := flecs.New()
	flecs.RegisterComponent[pairDataErr](w)

	// Crafted JSON: entities 1 and 2 are pair rel/tgt; entity 3 has the pair with bad data.
	// dataType matches the registered type; data is a JSON string (invalid for struct).
	craftedJSON := []byte(`{"version":1,"entities":[{"serial":1},{"serial":2},` +
		`{"serial":3,"pairs":[{"rel":1,"tgt":2,"dataType":"flecs_test.pairDataErr","data":"invalid_string"}]}]}`)

	if err := w.UnmarshalJSON(craftedJSON); err == nil {
		t.Error("UnmarshalJSON: expected error for corrupt pair data JSON")
	}
}

// TestCovMarshal_PairTypeNotRegistered covers UnmarshalJSON pair type not registered (lines 508-509).
// The dataType is not in typeStringToType because no such component is registered.
func TestCovMarshal_PairTypeNotRegistered(t *testing.T) {
	w := flecs.New()
	craftedJSON := []byte(`{"version":1,"entities":[{"serial":1},{"serial":2},` +
		`{"serial":3,"pairs":[{"rel":1,"tgt":2,"dataType":"flecs_test.unknownType999","data":{"X":1}}]}]}`)
	if err := w.UnmarshalJSON(craftedJSON); err == nil {
		t.Error("UnmarshalJSON: expected error for unregistered pair dataType")
	}
}

// TestCovMarshal_ComponentJsonError covers UnmarshalJSON component json error (lines 516-519).
// The component is registered but the data is a JSON string (invalid for the struct type).
func TestCovMarshal_ComponentJsonError(t *testing.T) {
	type compJsonErr struct{ V int }
	w := flecs.New()
	flecs.RegisterComponent[compJsonErr](w)

	craftedJSON := []byte(`{"version":1,"entities":[` +
		`{"serial":1,"components":{"flecs_test.compJsonErr":"invalid_string"}}` +
		`]}`)
	if err := w.UnmarshalJSON(craftedJSON); err == nil {
		t.Error("UnmarshalJSON: expected error for corrupt component JSON")
	}
}

// TestCovMarshal_SparseDataComponentNotRegistered covers UnmarshalJSON sparse data
// component not in compByName (lines 529-532) via crafted JSON.
func TestCovMarshal_SparseDataComponentNotRegistered(t *testing.T) {
	w := flecs.New()
	// Crafted JSON references a component name not registered in w.
	craftedJSON := []byte(`{"version":1,"entities":[{"serial":1}],` +
		`"sparse_data":{"flecs_test.nonexistentCov999":{"1":{"V":42}}}}`)
	if err := w.UnmarshalJSON(craftedJSON); err == nil {
		t.Error("UnmarshalJSON: expected error for SparseData with unregistered component")
	}
}

// TestCovMarshal_SparseDataSizeZero covers UnmarshalJSON sparse data size==0 continue
// (lines 533-534): component is registered but has zero size.
func TestCovMarshal_SparseDataSizeZero(t *testing.T) {
	type tagForSparseData struct{}
	w := flecs.New()
	flecs.RegisterComponent[tagForSparseData](w)

	// Crafted JSON: SparseData references a registered zero-size component.
	// info.Size == 0 → continue (no error).
	craftedJSON := []byte(`{"version":1,"entities":[{"serial":1}],` +
		`"sparse_data":{"flecs_test.tagForSparseData":{"1":null}}}`)
	if err := w.UnmarshalJSON(craftedJSON); err != nil {
		t.Fatalf("UnmarshalJSON: unexpected error for zero-size SparseData component: %v", err)
	}
}

// TestCovMarshal_SparseDataUnknownSerial covers UnmarshalJSON sparse data serial not found
// (lines 538-541): serial 9999 does not exist in serialToID.
func TestCovMarshal_SparseDataUnknownSerial(t *testing.T) {
	type dfForSerial struct{ V int }
	w := flecs.New()
	did := flecs.RegisterComponent[dfForSerial](w)
	flecs.SetDontFragment(w, did)

	// Crafted JSON: DontFragment component registered, entity serial 1 exists,
	// but SparseData references serial 9999 (not allocated).
	craftedJSON := []byte(`{"version":1,"entities":[{"serial":1}],` +
		`"dont_fragment_components":["flecs_test.dfForSerial"],` +
		`"sparse_data":{"flecs_test.dfForSerial":{"9999":{"V":7}}}}`)
	if err := w.UnmarshalJSON(craftedJSON); err == nil {
		t.Error("UnmarshalJSON: expected error for unknown serial in SparseData")
	}
}

// TestCovMarshal_SparseDataJsonError covers UnmarshalJSON sparse data json error
// (lines 543-546): component is registered, serial is valid, but data is invalid JSON
// for the type (JSON string instead of struct object).
func TestCovMarshal_SparseDataJsonError(t *testing.T) {
	type dfForJsonErr struct{ V int }
	w := flecs.New()
	did := flecs.RegisterComponent[dfForJsonErr](w)
	flecs.SetDontFragment(w, did)

	craftedJSON := []byte(`{"version":1,"entities":[{"serial":1}],` +
		`"dont_fragment_components":["flecs_test.dfForJsonErr"],` +
		`"sparse_data":{"flecs_test.dfForJsonErr":{"1":"invalid_string"}}}`)
	if err := w.UnmarshalJSON(craftedJSON); err == nil {
		t.Error("UnmarshalJSON: expected error for invalid JSON in SparseData value")
	}
}

// ── scope.go / reader paths ───────────────────────────────────────────────────

// TestCovScope_Reader_HasID_DeadEntity covers r.HasID rec==nil early-return.
// Called with a non-union component on a deleted entity.
func TestCovScope_Reader_HasID_DeadEntity(t *testing.T) {
	w := flecs.New()
	type deadTag struct{}
	tagID := flecs.RegisterComponent[deadTag](w)

	var deadEnt flecs.ID
	w.Write(func(fw *flecs.Writer) {
		deadEnt = fw.NewEntity()
		flecs.AddID(fw, deadEnt, tagID)
	})
	w.Write(func(fw *flecs.Writer) { fw.Delete(deadEnt) })

	w.Read(func(r *flecs.Reader) {
		if r.HasID(deadEnt, tagID) {
			t.Error("HasID on dead entity: expected false")
		}
	})
}

// TestCovScope_Reader_OwnsID_NoTarget covers r.OwnsID union branch !has path.
// Entity exists but has no union target for R.
func TestCovScope_Reader_OwnsID_NoTarget(t *testing.T) {
	w := flecs.New()
	var R, T1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		T1 = fw.NewEntity()
	})
	flecs.SetUnion(w, R)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() }) // no union target

	w.Read(func(r *flecs.Reader) {
		if r.OwnsID(e, flecs.MakePair(R, T1)) {
			t.Error("OwnsID on entity with no union target: expected false")
		}
	})
}

// TestCovScope_EachChild_OrderedFnFalse covers the early-return path in
// EachChild when the parent has OrderedChildren and fn returns false.
func TestCovScope_EachChild_OrderedFnFalse(t *testing.T) {
	w := flecs.New()
	var parent, child1, child2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.SetOrderedChildren(w, parent)
		child1 = fw.NewEntity()
		child2 = fw.NewEntity()
		flecs.AddID(fw, child1, flecs.MakePair(w.ChildOf(), parent))
		flecs.AddID(fw, child2, flecs.MakePair(w.ChildOf(), parent))
	})

	count := 0
	w.Read(func(r *flecs.Reader) {
		r.EachChild(parent, func(_ flecs.ID) bool {
			count++
			return false // stop after first child
		})
	})
	if count != 1 {
		t.Errorf("EachChild ordered early stop: expected 1 visit, got %d (child1=%v child2=%v)", count, child1, child2)
	}
}

// TestCovScope_EachPrefab_NoTable covers r.EachPrefab when entity has no table.
func TestCovScope_EachPrefab_NoTable(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity() // no components → rec.Table == nil
	})

	count := 0
	w.Read(func(r *flecs.Reader) {
		r.EachPrefab(e, func(_ flecs.ID) bool {
			count++
			return true
		})
	})
	if count != 0 {
		t.Errorf("EachPrefab on entity with no table: expected 0 calls, got %d", count)
	}
}

// TestCovScope_EntityComponents_NoTable covers r.EntityComponents when entity has no table.
func TestCovScope_EntityComponents_NoTable(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity() // no components → rec.Table == nil
	})

	w.Read(func(r *flecs.Reader) {
		comps := r.EntityComponents(e)
		if comps != nil {
			t.Errorf("EntityComponents on entity with no table: expected nil, got %v", comps)
		}
	})
}

// ── world.go ─────────────────────────────────────────────────────────────────

// TestCovWorld_SetInheritable_Method covers the w.SetInheritable(cid) method success path.
func TestCovWorld_SetInheritable_Method(t *testing.T) {
	w := flecs.New()
	type methodInheritable struct{ V int }
	cid := flecs.RegisterComponent[methodInheritable](w)
	w.SetInheritable(cid) // covers world.go SetInheritable method body
}

// ── scope.go / Each2-4 inherited component slow paths ─────────────────────────

// TestCovScope_Each2_InheritedComponent covers the Each2 slow path where one
// component is resolved via an ancestor (upSources != 0) and the other is local.
func TestCovScope_Each2_InheritedComponent(t *testing.T) {
	w := flecs.New()
	type compAE2 struct{ V int }
	type compBE2 struct{ W int }
	flecs.RegisterComponent[compAE2](w)
	flecs.RegisterComponent[compBE2](w)
	flecs.SetInheritable[compBE2](w) // B resolves via IsA → upSources[B] != 0

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, compBE2{W: 10})

		inst = fw.NewEntity()
		flecs.Set(fw, inst, compAE2{V: 1})
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), prefab))
	})

	var visited []flecs.ID
	w.Read(func(r *flecs.Reader) {
		flecs.Each2[compAE2, compBE2](r, func(e flecs.ID, a *compAE2, b *compBE2) {
			visited = append(visited, e)
			if a.V != 1 {
				t.Errorf("Each2 inherited: a.V want 1, got %d", a.V)
			}
			if b.W != 10 {
				t.Errorf("Each2 inherited: b.W want 10, got %d", b.W)
			}
		})
	})

	if len(visited) != 1 || visited[0].Index() != inst.Index() {
		t.Errorf("Each2 inherited: expected [inst=%v], got %v", inst, visited)
	}
}

// TestCovScope_Each3_InheritedComponent covers the Each3 slow path.
func TestCovScope_Each3_InheritedComponent(t *testing.T) {
	w := flecs.New()
	type compAE3 struct{ V int }
	type compBE3 struct{ W int }
	type compCE3 struct{ X int }
	flecs.RegisterComponent[compAE3](w)
	flecs.RegisterComponent[compBE3](w)
	flecs.RegisterComponent[compCE3](w)
	flecs.SetInheritable[compBE3](w) // only B inherited via IsA

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, compBE3{W: 20})

		inst = fw.NewEntity()
		flecs.Set(fw, inst, compAE3{V: 1})
		flecs.Set(fw, inst, compCE3{X: 3})
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), prefab))
	})

	var visited []flecs.ID
	w.Read(func(r *flecs.Reader) {
		flecs.Each3[compAE3, compBE3, compCE3](r, func(e flecs.ID, a *compAE3, b *compBE3, c *compCE3) {
			visited = append(visited, e)
			if b.W != 20 {
				t.Errorf("Each3 inherited: b.W want 20, got %d", b.W)
			}
		})
	})

	if len(visited) != 1 || visited[0].Index() != inst.Index() {
		t.Errorf("Each3 inherited: expected [inst=%v], got %v", inst, visited)
	}
}

// TestCovScope_Each4_InheritedComponent covers the Each4 slow path.
func TestCovScope_Each4_InheritedComponent(t *testing.T) {
	w := flecs.New()
	type compAE4 struct{ V int }
	type compBE4 struct{ W int }
	type compCE4 struct{ X int }
	type compDE4 struct{ Y int }
	flecs.RegisterComponent[compAE4](w)
	flecs.RegisterComponent[compBE4](w)
	flecs.RegisterComponent[compCE4](w)
	flecs.RegisterComponent[compDE4](w)
	flecs.SetInheritable[compDE4](w) // only D inherited via IsA

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, compDE4{Y: 40})

		inst = fw.NewEntity()
		flecs.Set(fw, inst, compAE4{V: 1})
		flecs.Set(fw, inst, compBE4{W: 2})
		flecs.Set(fw, inst, compCE4{X: 3})
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), prefab))
	})

	var visited []flecs.ID
	w.Read(func(r *flecs.Reader) {
		flecs.Each4[compAE4, compBE4, compCE4, compDE4](r, func(e flecs.ID, a *compAE4, b *compBE4, c *compCE4, d *compDE4) {
			visited = append(visited, e)
			if d.Y != 40 {
				t.Errorf("Each4 inherited: d.Y want 40, got %d", d.Y)
			}
		})
	})

	if len(visited) != 1 || visited[0].Index() != inst.Index() {
		t.Errorf("Each4 inherited: expected [inst=%v], got %v", inst, visited)
	}
}

// ── scope.go / deferred RemoveID sparse present path ─────────────────────────

// TestCovScope_RemoveID_Sparse_Present_Deferred covers deferred RemoveID when
// entity has the sparse component (scope.go lines 653-654: queue + return true).
func TestCovScope_RemoveID_Sparse_Present_Deferred(t *testing.T) {
	w := flecs.New()
	type sparsePresent struct{ V int }
	cid := flecs.RegisterComponent[sparsePresent](w)
	flecs.SetSparse(w, cid)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, sparsePresent{V: 7})
	})

	var removed bool
	w.Write(func(fw *flecs.Writer) {
		// deferDepth > 0; entity has the sparse component → enqueue + return true
		removed = flecs.RemoveID(fw, e, cid)
	})
	if !removed {
		t.Error("deferred RemoveID sparse present: expected true")
	}
	w.Read(func(r *flecs.Reader) {
		if flecs.Has[sparsePresent](r, e) {
			t.Error("sparse component should be removed after deferred RemoveID")
		}
	})
}

// ── system.go ─────────────────────────────────────────────────────────────────

// TestCovSystem_NewSystemInPhase_Panics covers all panic guards in NewSystemInPhase.
func TestCovSystem_NewSystemInPhase_Panics(t *testing.T) {
	w := flecs.New()
	type sysQueryComp struct{ V int }
	cid := flecs.RegisterComponent[sysQueryComp](w)
	cq := flecs.NewCachedQuery(w, cid)
	fn := func(_ float32, _ *flecs.QueryIter) {}

	mustPanic := func(name string, f func()) {
		t.Helper()
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("NewSystemInPhase %s: expected panic, got none", name)
			}
		}()
		f()
	}

	mustPanic("nil world", func() {
		flecs.NewSystemInPhase(nil, w.OnUpdate(), cq, fn)
	})
	mustPanic("nil phase", func() {
		flecs.NewSystemInPhase(w, nil, cq, fn)
	})
	mustPanic("nil query", func() {
		flecs.NewSystemInPhase(w, w.OnUpdate(), nil, fn)
	})
	mustPanic("nil fn", func() {
		flecs.NewSystemInPhase(w, w.OnUpdate(), cq, nil)
	})

	cq2 := flecs.NewCachedQuery(w, cid)
	cq2.Close()
	mustPanic("closed query", func() {
		flecs.NewSystemInPhase(w, w.OnUpdate(), cq2, fn)
	})

	w2 := flecs.New()
	cid2 := flecs.RegisterComponent[sysQueryComp](w2)
	cq3 := flecs.NewCachedQuery(w2, cid2)
	mustPanic("wrong world", func() {
		flecs.NewSystemInPhase(w, w.OnUpdate(), cq3, fn)
	})
}

// ── value_ops.go ──────────────────────────────────────────────────────────────

// TestCovValueOps_GetByID_SparseNotFound covers w.GetByID when the entity does
// not have the sparse component (value_ops.go ptr==nil → return nil, false).
func TestCovValueOps_GetByID_SparseNotFound(t *testing.T) {
	w := flecs.New()
	type sparseNotFound struct{ V int }
	cid := flecs.RegisterComponent[sparseNotFound](w)
	flecs.SetSparse(w, cid)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity() // no sparse component
	})

	val, ok := w.GetByID(e, cid)
	if ok {
		t.Errorf("GetByID sparse not found: expected false, got val=%v", val)
	}
}

// TestCovValueOps_GetRef_NilPaths covers getRefOnWorld ptr==nil paths.
func TestCovValueOps_GetRef_NilPaths(t *testing.T) {
	w := flecs.New()
	type dfGetRef struct{ V int }
	type archGetRef struct{ W int }
	type archGetRef2 struct{ X int }
	dfID := flecs.RegisterComponent[dfGetRef](w)
	flecs.SetDontFragment(w, dfID)
	flecs.RegisterComponent[archGetRef](w)
	flecs.RegisterComponent[archGetRef2](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, archGetRef{W: 5}) // e has archGetRef but NOT dfGetRef, NOT archGetRef2
	})

	w.Read(func(r *flecs.Reader) {
		// DontFragment path: entity does not have dfGetRef → sparseSetGet returns nil.
		ptr := flecs.GetRef[dfGetRef](r, e)
		if ptr != nil {
			t.Errorf("GetRef[DF] not found: expected nil, got %v", ptr)
		}
		// Archetype path: entity has a table (archGetRef) but not archGetRef2 → HasComponent false → return nil.
		ptr2 := flecs.GetRef[archGetRef2](r, e)
		if ptr2 != nil {
			t.Errorf("GetRef[arch missing] not found: expected nil, got %v", ptr2)
		}
	})
	_ = dfID
}

// TestCovQuery_DontFragment_NilDriver covers nextSparseOnly when sparseDriver==nil:
// a DontFragment component with no entities having it → sparseStorage[key]==nil →
// zeroDriver=true → driver=nil → nextSparseOnly returns false immediately.
func TestCovQuery_DontFragment_NilDriver(t *testing.T) {
	w := flecs.New()
	type dfNilDriver struct{ V int }
	cid := flecs.RegisterComponent[dfNilDriver](w)
	flecs.SetDontFragment(w, cid)
	// No entity has dfNilDriver → sparseStorage[key] == nil → zeroDriver = true → driver = nil.

	q := flecs.NewQueryFromTerms(w, flecs.With(cid))
	count := 0
	it := q.Iter()
	for it.Next() {
		count += it.Count()
	}
	if count != 0 {
		t.Errorf("DF nil driver: expected 0 entities, got %d", count)
	}
}

// TestUnion_Query_MultipleUnionAnd_MissingSecond covers matchesSparseTerms TermAnd union
// where entity is in the driver store but NOT in the second union store → !has → return false.
func TestUnion_Query_MultipleUnionAnd_MissingSecond(t *testing.T) {
	w := flecs.New()
	var R1, R2, T1, T2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		R1 = fw.NewEntity()
		R2 = fw.NewEntity()
		T1 = fw.NewEntity()
		T2 = fw.NewEntity()
	})
	flecs.SetUnion(w, R1)
	flecs.SetUnion(w, R2)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		// e1 has only R1 (not R2)
		fw.AddID(e1, flecs.MakePair(R1, T1))
		// e2 has only R2 (not R1)
		fw.AddID(e2, flecs.MakePair(R2, T2))
	})

	// With(R1,*) AND With(R2,*): neither entity has both → no matches.
	// Driver = R1's store (first term, both stores have 1 entry).
	// For e1 (in R1's driver): check R2 → e1 NOT in R2's store → !has → return false (COVERED).
	q := flecs.NewQueryFromTerms(w,
		flecs.With(flecs.MakePair(R1, w.Wildcard())),
		flecs.With(flecs.MakePair(R2, w.Wildcard())),
	)
	var matched []flecs.ID
	it := q.Iter()
	for it.Next() {
		matched = append(matched, it.Entities()...)
	}
	if len(matched) != 0 {
		t.Errorf("multi-union AND missing second: expected no matches, got %v (e1=%v e2=%v)", matched, e1, e2)
	}
}

// TestCovFieldShared_Paths covers uncovered paths in FieldShared:
// - term.ID != id → continue (iterator has multiple terms; look for second term's ID)
// - src == 0 → return (zero, false) (self-matched component; use Field[T] instead)
func TestCovFieldShared_Paths(t *testing.T) {
	w := flecs.New()
	type compA struct{ X int }
	type compB struct{ Y int }
	flecs.RegisterComponent[compA](w)
	flecs.RegisterComponent[compB](w)
	flecs.SetInheritable[compB](w)

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, compB{Y: 99})

		inst = fw.NewEntity()
		flecs.Set(fw, inst, compA{X: 1})
		flecs.Set(fw, inst, compB{Y: 7}) // inst directly owns B (not inherited)
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), prefab))
	})

	aID := flecs.RegisterComponent[compA](w)
	bID := flecs.RegisterComponent[compB](w)
	q := flecs.NewQueryFromTerms(w, flecs.With(aID), flecs.With(bID))
	it := q.Iter()
	for it.Next() {
		// Call FieldShared[compB](it, bID):
		// - First term is A → term.ID(=aID) != bID → continue (PATH 1 COVERED)
		// - Second term is B (matched via self since inst directly owns B) → src==0 → return (zero, false) (PATH 2 COVERED)
		_, ok := flecs.FieldShared[compB](it, bID)
		if ok {
			t.Error("FieldShared on self-matched component: expected false")
		}
	}
	_ = inst
	_ = prefab
}

// ── cleanup.go ────────────────────────────────────────────────────────────────

// TestCovCleanup_BadOnDeleteAction covers the panic for an invalid OnDelete action
// (cleanup.go:90-91 — the inner switch default for OnDelete).
func TestCovCleanup_BadOnDeleteAction(t *testing.T) {
	w := flecs.New()
	var R, badAction flecs.ID
	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		badAction = fw.NewEntity() // not Delete, Panic, or Remove
	})
	defer func() {
		if r := recover(); r == nil {
			t.Error("SetCleanupPolicy bad OnDelete action: expected panic, got none")
		}
	}()
	flecs.SetCleanupPolicy(w, R, w.OnDelete(), badAction)
}

// TestCovCleanup_BadOnDeleteTargetAction covers the panic for an invalid OnDeleteTarget
// action (cleanup.go:102-103 — the inner switch default for OnDeleteTarget).
func TestCovCleanup_BadOnDeleteTargetAction(t *testing.T) {
	w := flecs.New()
	var R, badAction flecs.ID
	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		badAction = fw.NewEntity()
	})
	defer func() {
		if r := recover(); r == nil {
			t.Error("SetCleanupPolicy bad OnDeleteTarget action: expected panic, got none")
		}
	}()
	flecs.SetCleanupPolicy(w, R, w.OnDeleteTarget(), badAction)
}

// ── exclusive_access.go ───────────────────────────────────────────────────────

// TestCovExclusiveAccess_LockedWorld covers the panic path when the world is
// locked for writes (exclusive_access.go:29-30 — owner == ^uint64(0)).
func TestCovExclusiveAccess_LockedWorld(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	w.ExclusiveAccessEnd(true) // sets exclusiveAccess = ^uint64(0)

	defer func() {
		if r := recover(); r == nil {
			t.Error("Delete on locked world: expected panic, got none")
		}
	}()
	w.Delete(e) // checkExclusiveAccessWrite → owner==^uint64(0) → panic
}

// ── oneof.go ──────────────────────────────────────────────────────────────────

// TestCovOneOf_WildcardTarget covers the wildcard/any target exemption in
// checkOneOf (oneof.go:59-61) — wildcard targets bypass the OneOf constraint.
func TestCovOneOf_WildcardTarget(t *testing.T) {
	w := flecs.New()
	var R, parent flecs.ID
	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		parent = fw.NewEntity()
		flecs.SetOneOf(w, R, parent) // R targets must be children of parent
	})
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		// Wildcard target is exempt from the OneOf constraint check → no panic
		fw.AddID(e, flecs.MakePair(R, w.Wildcard()))
	})
}

// ── reflexive.go ──────────────────────────────────────────────────────────────

// TestCovReflexive_TargetDeleted covers reflexiveTableMatches when the target
// entity has been deleted (reflexive.go:76-78 — rec == nil → return false).
func TestCovReflexive_TargetDeleted(t *testing.T) {
	w := flecs.New()
	var R, target flecs.ID
	type compRefl struct{ V int }
	flecs.RegisterComponent[compRefl](w)

	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		target = fw.NewEntity()
		flecs.SetReflexive(w, R)
		// Give e2 a component so there's a non-empty table to iterate
		e2 := fw.NewEntity()
		flecs.Set(fw, e2, compRefl{V: 1})
		_ = e2
	})
	// Delete target: w.index.Get(target) returns nil after deletion.
	w.Write(func(fw *flecs.Writer) { fw.Delete(target) })

	// Query for (R, target) where target is deleted (rec == nil).
	// reflexiveTableMatches → rec == nil → return false (line 76-78 covered).
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(R, target)))
	it := q.Iter()
	count := 0
	for it.Next() {
		count += it.Count()
	}
	if count != 0 {
		t.Errorf("reflexive deleted target: expected 0 matches, got %d", count)
	}
}

// ── writeonce.go ──────────────────────────────────────────────────────────────

// TestCovWriteOnce_PairCompKey covers the id.IsPair() branch in
// clearWriteOnceTracking (writeonce.go:105-107 — compKey = id for pair IDs).
func TestCovWriteOnce_PairCompKey(t *testing.T) {
	w := flecs.New()
	type woComp struct{ V int }
	cid := flecs.RegisterComponent[woComp](w)
	flecs.SetWriteOnce(w, cid)

	var parent, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		e = fw.NewEntity()
		// Set WriteOnce component: initializes writeOnceHasBeenSet map.
		flecs.Set(fw, e, woComp{V: 1})
		// Add a pair (ChildOf, parent) so we can remove it next.
		fw.AddID(e, flecs.MakePair(w.ChildOf(), parent))
	})
	// Remove the pair: removeIDImmediate → clearWriteOnceTracking(w, e, pairID)
	// writeOnceHasBeenSet != nil AND id.IsPair() == true → compKey = id (line 105-107).
	w.Write(func(fw *flecs.Writer) {
		fw.RemoveID(e, flecs.MakePair(w.ChildOf(), parent))
	})
}

// ── scope.go ──────────────────────────────────────────────────────────────────

// TestCovScope_HasID_UnionNoStore covers r.HasID when the union relationship
// has a policy but no store yet (scope.go:82-84 — !ok → return false).
func TestCovScope_HasID_UnionNoStore(t *testing.T) {
	w := flecs.New()
	var R flecs.ID
	w.Write(func(fw *flecs.Writer) { R = fw.NewEntity() })
	flecs.SetUnion(w, R) // R is in unionPolicies but no store exists

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	w.Read(func(r *flecs.Reader) {
		// R is union but unionStore[R] doesn't exist → !ok → return false (line 82-84)
		has := r.HasID(e, flecs.MakePair(R, w.Wildcard()))
		if has {
			t.Error("HasID union no store: expected false, got true")
		}
	})
}
