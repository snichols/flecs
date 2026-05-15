package flecs_test

import (
	"io"
	"log/slog"
	"testing"
	"unsafe"

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

// ── observer.go ───────────────────────────────────────────────────────────────

// TestCovObserver_EventKindString covers the EventOnTableCreate case in String()
// and the EventOnTableCreate, EventMonitor, and default cases in eventKindToEntity
// (observer.go lines 39-40, 66-71). These are covered by registering an ObserveID
// with each event kind, which calls eventKindToEntity internally.
func TestCovObserver_EventKindStringAndEntity(t *testing.T) {
	// Cover EventOnTableCreate.String() → "OnTableCreate"
	if s := flecs.EventOnTableCreate.String(); s != "OnTableCreate" {
		t.Errorf("EventOnTableCreate.String() = %q, want %q", s, "OnTableCreate")
	}

	// Cover eventKindToEntity for EventOnTableCreate and EventMonitor via ObserveID.
	w := flecs.New()
	type covComp struct{ V int }
	id := flecs.RegisterComponent[covComp](w)

	// EventOnTableCreate: calls eventKindToEntity(w, EventOnTableCreate).
	obs1 := flecs.ObserveID(w, id, flecs.EventOnTableCreate, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {})
	if obs1 == nil {
		t.Fatal("ObserveID EventOnTableCreate returned nil")
	}

	// EventMonitor: calls eventKindToEntity(w, EventMonitor).
	obs2 := flecs.ObserveID(w, id, flecs.EventMonitor, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {})
	if obs2 == nil {
		t.Fatal("ObserveID EventMonitor returned nil")
	}
}

// ── hooks.go ──────────────────────────────────────────────────────────────────

// TestCovHooks_OnReplaceID_NilClear covers the fn==nil branch in OnReplaceID
// (hooks.go:128-131) which clears the OnReplace hook.
func TestCovHooks_OnReplaceID_NilClear(t *testing.T) {
	w := flecs.New()
	type hComp struct{ V int }
	id := flecs.RegisterComponent[hComp](w)
	// Set a hook, then clear it via nil fn.
	flecs.OnReplaceID(w, id, func(_ *flecs.Writer, _ flecs.ID, _, _ unsafe.Pointer) {})
	flecs.OnReplaceID(w, id, nil) // fn == nil: clears the hook (covers lines 128-131)
}

// ── value_ops.go ──────────────────────────────────────────────────────────────

// TestCovValueOps_SetByID_DeferredPanics covers the deferred panic paths in
// World.SetByID (value_ops.go:235 unregistered, :238 type mismatch).
func TestCovValueOps_SetByID_DeferredPanics(t *testing.T) {
	w := flecs.New()
	type vComp struct{ X int }
	id := flecs.RegisterComponent[vComp](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, vComp{X: 1})
	})

	var badID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		badID = fw.NewEntity() // entity, not a registered component
	})

	// Unregistered component panic (value_ops.go:235-236).
	panickedUnreg := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panickedUnreg = true
			}
		}()
		w.Write(func(_ *flecs.Writer) {
			w.SetByID(e, badID, 42) // calls World.SetByID directly; deferDepth>0 → line 235
		})
	}()
	if !panickedUnreg {
		t.Error("expected panic for unregistered component ID in deferred SetByID")
	}

	// Type mismatch panic (value_ops.go:238-240).
	panickedType := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panickedType = true
			}
		}()
		w.Write(func(_ *flecs.Writer) {
			w.SetByID(e, id, "wrong type") // deferDepth>0, type mismatch → line 238
		})
	}()
	if !panickedType {
		t.Error("expected panic for type mismatch in deferred SetByID")
	}
}

// TestCovValueOps_SetPairByID_NilPanic covers the nil-value panic in SetPairByID
// (value_ops.go:80-81).
func TestCovValueOps_SetPairByID_NilPanic(t *testing.T) {
	w := flecs.New()
	type relComp struct{ V int }
	rel := flecs.RegisterComponent[relComp](w)
	var e, tgt flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		tgt = fw.NewEntity()
	})
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		w.Write(func(_ *flecs.Writer) {
			w.SetPairByID(e, rel, tgt, nil) // v==nil → panic at line 80-81
		})
	}()
	if !panicked {
		t.Error("expected panic for nil v in SetPairByID")
	}
}

// ── observer.go ───────────────────────────────────────────────────────────────

// TestCovObserver_LoggerPath covers the observer registration logger path
// (observer.go:278-282) when the world has a logger installed.
func TestCovObserver_LoggerPath(t *testing.T) {
	w := flecs.New()
	w.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	type logComp struct{ V int }
	id := flecs.RegisterComponent[logComp](w)
	obs := flecs.ObserveID(w, id, flecs.EventOnAdd, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {})
	if obs == nil {
		t.Fatal("ObserveID with logger returned nil")
	}
}

// ── Phase 16.45: coverage boost for new variable-query API ───────────────────

// covPos16b and covVel16b are local component types for coverage boost tests.
type covPos16b struct{ X, Y float32 }
type covVel16b struct{ V float32 }

// TestCovWithVarKind_EmptyNamePanic covers WithVarKind panic when name is empty.
func TestCovWithVarKind_EmptyNamePanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty name")
		}
	}()
	_ = flecs.WithVarKind("", flecs.VarEntity)
}

// TestCovWithVarKind_VarAnyPanic covers WithVarKind panic when kind is VarAny.
func TestCovWithVarKind_VarAnyPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for VarAny")
		}
	}()
	_ = flecs.WithVarKind("x", flecs.VarAny)
}

// TestCovWithVarKind_Normal covers the normal WithVarKind return path.
func TestCovWithVarKind_Normal(t *testing.T) {
	term := flecs.WithVarKind("myVar", flecs.VarEntity)
	_ = term // just verify it returns without panic
}

// TestCovWithTableVar_EmptyPanic covers WithTableVar panic when name is empty.
func TestCovWithTableVar_EmptyPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty name")
		}
	}()
	_ = flecs.WithTableVar("")
}

// TestCovWithPairRelVar_Panics covers WithPairRelVar panic paths.
func TestCovWithPairRelVar_Panics(t *testing.T) {
	t.Run("empty varName", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for empty varName")
			}
		}()
		_ = flecs.WithPairRelVar("", flecs.ID(42))
	})
	t.Run("zero target", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for zero target")
			}
		}()
		_ = flecs.WithPairRelVar("R", 0)
	})
}

// TestCovWithPairBothVar_Panics covers WithPairBothVar panic paths.
func TestCovWithPairBothVar_Panics(t *testing.T) {
	t.Run("empty relVarName", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for empty relVarName")
			}
		}()
		_ = flecs.WithPairBothVar("", "T")
	})
	t.Run("empty tgtVarName", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for empty tgtVarName")
			}
		}()
		_ = flecs.WithPairBothVar("R", "")
	})
}

// TestCovRelVar_Panics covers Term.RelVar panic paths.
func TestCovRelVar_Panics(t *testing.T) {
	w := flecs.New()
	var aID, bID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		aID = fw.NewEntity()
		bID = fw.NewEntity()
	})
	t.Run("empty name", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for empty name")
			}
		}()
		_ = flecs.With(aID).RelVar("")
	})
	t.Run("pair ID", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for pair ID")
			}
		}()
		_ = flecs.With(flecs.MakePair(aID, bID)).RelVar("R")
	})
}

// TestCovScopeBuilderSource_Panics covers ScopeBuilder.Source panic paths.
func TestCovScopeBuilderSource_Panics(t *testing.T) {
	w := flecs.New()
	var aID flecs.ID
	w.Write(func(fw *flecs.Writer) { aID = fw.NewEntity() })

	t.Run("no terms", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic when no term to apply source to")
			}
		}()
		// WithoutScope panics if buildFn panics; the defer above catches it.
		_ = flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
			b.Source(aID) // no terms yet — panic
		})
	})

	t.Run("zero src", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for zero src")
			}
		}()
		_ = flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
			b.With(aID)
			b.Source(0) // zero src — panic
		})
	})
}

// TestCovVarTable_Panics covers QueryIter.VarTable panic and nil return paths.
func TestCovVarTable_Panics(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[covPos16b](w)
	var e1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, covPos16b{X: 1})
	})

	t.Run("no variables query panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic: no variables in query")
			}
		}()
		w.Read(func(_ *flecs.Reader) {
			q := flecs.NewQueryFromTerms(w, flecs.With(posID))
			it := q.Iter()
			it.Next()
			it.VarTable("T") // no variables → panic
		})
	})

	t.Run("undefined variable panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic: undefined variable")
			}
		}()
		w.Read(func(_ *flecs.Reader) {
			q := flecs.NewQueryFromTerms(w,
				flecs.WithTableVar("T"),
				flecs.With(posID),
			)
			it := q.Iter()
			it.Next()
			it.VarTable("notDefined") // variable not in query → panic
		})
	})

	t.Run("entity-kind var returns nil", func(t *testing.T) {
		w.Read(func(_ *flecs.Reader) {
			q := flecs.NewQueryFromTerms(w,
				flecs.WithPairTgtVar(posID, "planet"),
			)
			it := q.Iter()
			// VarTable on entity-kind variable should return nil
			// (We must call Next() first to position the iterator)
			for it.Next() {
				result := it.VarTable("planet")
				if result != nil {
					t.Errorf("VarTable on entity-kind var: want nil, got %v", result)
				}
			}
		})
	})

	t.Run("table-kind var before Next panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic: not positioned")
			}
		}()
		w.Read(func(_ *flecs.Reader) {
			q := flecs.NewQueryFromTerms(w,
				flecs.WithTableVar("T"),
				flecs.With(posID),
			)
			it := q.Iter()
			it.VarTable("T") // not positioned yet → panic
		})
	})
}

// TestCovOptimizer_TableKindVar exercises estimateTableKindDomain via a 2-variable
// query where one variable is VarTable. Having ≥2 variables triggers selectOptimalDriver
// which calls estimateVarDomain → estimateTableKindDomain for the table-kind var.
func TestCovOptimizer_TableKindVar(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[covPos16b](w)
	velID := flecs.RegisterComponent[covVel16b](w)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, covPos16b{X: 1})
		flecs.Set(fw, e, covVel16b{V: 2})
	})

	w.Read(func(_ *flecs.Reader) {
		// 2 variables: T (VarTable) and V (entity). selectOptimalDriver is called.
		// estimateTableKindDomain is invoked for T; estimateSrcVarDomain for V.
		// TermVarDecl for T triggers the "Kind != TermAnd → continue" in estimateVarDomain.
		q := flecs.NewQueryFromTerms(w,
			flecs.WithTableVar("T"),
			flecs.With(posID),
			flecs.WithVar(velID, "V"),
		)
		it := q.Iter()
		count := 0
		for it.Next() {
			count += len(it.Entities())
		}
		if count == 0 {
			t.Error("expected at least one entity from table-kind+entity-var query")
		}
	})
}

// TestCovOptimizer_RelVarDomain exercises estimateRelVarDomain via a 2-variable
// query with WithPairRelVar and actual pairs in the world.
func TestCovOptimizer_RelVarDomain(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[covPos16b](w)
	var heroID, likesID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		heroID = fw.NewEntity()
		likesID = fw.NewEntity()
		// entity with posID and (likesID, heroID) pair — supplies pairs for sampling
		e := fw.NewEntity()
		flecs.Set(fw, e, covPos16b{X: 1})
		flecs.AddID(fw, e, flecs.MakePair(likesID, heroID))
	})

	w.Read(func(_ *flecs.Reader) {
		// 2 variables: R (relVar with fixed target heroID) and P (entity with posID).
		// selectOptimalDriver calls estimateRelVarDomain for R.
		// The world has (likesID, heroID) pairs → sampling loop exercises the full path.
		q := flecs.NewQueryFromTerms(w,
			flecs.WithPairRelVar("R", heroID),
			flecs.WithVar(posID, "P"),
		)
		_ = q
	})

	// Exercise the "no matches" path in estimateRelVarDomain: target not in any pair.
	// A fresh world with a registered component but no pairs pointing to orphanTarget.
	w2 := flecs.New()
	pos2ID := flecs.RegisterComponent[covPos16b](w2)
	var orphanTarget flecs.ID
	w2.Write(func(fw *flecs.Writer) {
		orphanTarget = fw.NewEntity() // nothing points to this as a pair target
	})
	w2.Read(func(_ *flecs.Reader) {
		// estimateRelVarDomain iterates tables, finds nothing, returns math.MaxInt.
		q2 := flecs.NewQueryFromTerms(w2,
			flecs.WithPairRelVar("R", orphanTarget),
			flecs.WithVar(pos2ID, "P"),
		)
		_ = q2
	})
}

// TestCovOptimizer_TgtVarFixedSource exercises the estimateTgtVarDomain fixed-source
// path (returns 1). Needs a tgtVar term with Src != 0 and a second variable.
func TestCovOptimizer_TgtVarFixedSource(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[covPos16b](w)
	var relID, srcEnt flecs.ID
	w.Write(func(fw *flecs.Writer) {
		relID = fw.NewEntity()
		srcEnt = fw.NewEntity()
		tgtID := fw.NewEntity()
		flecs.AddID(fw, srcEnt, flecs.MakePair(relID, tgtID))
	})
	w.Read(func(_ *flecs.Reader) {
		// WithPairTgtVar(relID, "V").Source(srcEnt) creates t.tgtVar="V", t.Src=srcEnt.
		// WithVar(posID, "P") is the second variable.
		// estimateTgtVarDomain(t) with t.Src != 0 → returns 1.
		q := flecs.NewQueryFromTerms(w,
			flecs.WithPairTgtVar(relID, "V").Source(srcEnt),
			flecs.WithVar(posID, "P"),
		)
		_ = q
	})
}

// TestCovAndYieldExisting covers ObserverOptions.AndYieldExisting (0% branch).
func TestCovAndYieldExisting(t *testing.T) {
	w := flecs.New()
	type covTagYE struct{ V int }
	compID := flecs.RegisterComponent[covTagYE](w)
	var e1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, covTagYE{V: 1})
	})
	// ObserveIDWithOptions with AndYieldExisting() exercises the 0% branch.
	fired := 0
	opts := flecs.WithQuery(flecs.With(compID)).AndYieldExisting()
	flecs.ObserveIDWithOptions(w, compID, opts, []flecs.EventKind{flecs.EventOnAdd},
		func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) { fired++ },
	)
	if fired == 0 {
		t.Error("AndYieldExisting: expected at least one retroactive fire, got 0")
	}
}

// TestCovEachPrefab covers EachPrefab on an instance entity that has IsA prefab links.
func TestCovEachPrefab(t *testing.T) {
	w := flecs.New()
	type covPrefabComp struct{ V int }
	compID := flecs.RegisterComponent[covPrefabComp](w)
	var pfb, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		pfb = fw.NewEntity()
		flecs.Set(fw, pfb, covPrefabComp{V: 7})
		flecs.MarkPrefab(fw, pfb)

		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), pfb)) // inst IsA pfb
	})
	count := 0
	w.Read(func(r *flecs.Reader) {
		// EachPrefab on inst iterates its (IsA, *) pairs.
		r.EachPrefab(inst, func(prefabID flecs.ID) bool {
			if prefabID == pfb {
				count++
			}
			return true
		})
		// Also call with a non-existent entity to cover the early-return path.
		r.EachPrefab(flecs.ID(999999), func(_ flecs.ID) bool { return true })
	})
	if count == 0 {
		t.Error("EachPrefab: expected to find prefab for instance")
	}
	_ = compID
}

// TestCovTermKindString covers TermScope/TermOrFrom/TermNotFrom String() paths.
func TestCovTermKindString(t *testing.T) {
	cases := []struct {
		k    flecs.TermKind
		want string
	}{
		{flecs.TermScope, "Scope"},
		{flecs.TermOrFrom, "OrFrom"},
		{flecs.TermNotFrom, "NotFrom"},
	}
	for _, c := range cases {
		got := c.k.String()
		if got != c.want {
			t.Errorf("TermKind(%d).String() = %q, want %q", int(c.k), got, c.want)
		}
	}
}

// TestCovCachedQuery_VariableOrder_Nil covers CachedQuery.VariableOrder returning nil
// when the cached query has no variables.
func TestCovCachedQuery_VariableOrder_Nil(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[covPos16b](w)
	cq := flecs.NewCachedQueryFromTerms(w, flecs.With(posID))
	if order := cq.VariableOrder(); order != nil {
		t.Errorf("VariableOrder with no variables: want nil, got %v", order)
	}
}

// ── Coverage boost batch 2 ───────────────────────────────────────────────────

// TestCovDeferredPolicyTags covers the deferred AddID dispatch paths in
// cmd_queue.go (batchForEntity pass-1) for 6 policy trait tags: Exclusive,
// Symmetric, Acyclic, Traversable, OrderedChildren, Sparse.
// Each entity gets 2 commands so batchForEntity is triggered (c.nextForEntity < 0).
// Note: WriteOnce is excluded because RegisterComponent entities lack rec.Table.
func TestCovDeferredPolicyTags(t *testing.T) {
	w := flecs.New()
	var neutralTag, relExcl, relSym, relAcyc, relTrav, relOC, compSparse flecs.ID
	w.Write(func(fw *flecs.Writer) {
		neutralTag = fw.NewEntity()
		relExcl = fw.NewEntity()
		relSym = fw.NewEntity()
		relAcyc = fw.NewEntity()
		relTrav = fw.NewEntity()
		relOC = fw.NewEntity()
		compSparse = fw.NewEntity()
	})
	// Two commands per entity triggers batchForEntity, which executes the
	// policy dispatch branches in cmd_queue.go's pass-1 loop.
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, relExcl, w.Exclusive()); flecs.AddID(fw, relExcl, neutralTag)
		flecs.AddID(fw, relSym, w.Symmetric()); flecs.AddID(fw, relSym, neutralTag)
		flecs.AddID(fw, relAcyc, w.Acyclic()); flecs.AddID(fw, relAcyc, neutralTag)
		flecs.AddID(fw, relTrav, w.Traversable()); flecs.AddID(fw, relTrav, neutralTag)
		flecs.AddID(fw, relOC, w.OrderedChildren()); flecs.AddID(fw, relOC, neutralTag)
		flecs.AddID(fw, compSparse, w.Sparse()); flecs.AddID(fw, compSparse, neutralTag)
	})
	// Verify at least one policy was applied.
	w.Read(func(r *flecs.Reader) {
		if !r.HasID(relExcl, w.Exclusive()) {
			t.Error("Exclusive policy not applied")
		}
	})
}

// TestCovDeferredPolicyWith covers the bare With no-op branch in cmd_queue.go
// batchForEntity pass-1 (the _ = entity stmt). w.With() has the Relationship
// trait, so checkUsageConstraints panics immediately after; we recover it.
func TestCovDeferredPolicyWith(t *testing.T) {
	w := flecs.New()
	var relWith, neutralTag flecs.ID
	w.Write(func(fw *flecs.Writer) {
		neutralTag = fw.NewEntity()
		relWith = fw.NewEntity()
	})
	defer func() { _ = recover() }()
	// Two commands for relWith triggers batchForEntity; the With no-op branch
	// (_ = entity) executes, then checkUsageConstraints panics.
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, relWith, w.With())
		flecs.AddID(fw, relWith, neutralTag)
	})
}

// TestCovAlwaysFalseCachedQuery covers NewCachedQueryFromTerms returning
// alwaysFalse=true (via OrFrom with empty source) and CachedQuery.Iter()
// returning the zero iterator for that case.
func TestCovAlwaysFalseCachedQuery(t *testing.T) {
	w := flecs.New()
	var emptyEnt flecs.ID
	w.Write(func(fw *flecs.Writer) { emptyEnt = fw.NewEntity() })
	cq := flecs.NewCachedQueryFromTerms(w, flecs.OrFrom(emptyEnt))
	it := cq.Iter()
	if it.Next() {
		t.Error("expected zero results from alwaysFalse query")
	}
}

// TestCovSetByIDInHook covers the SetByID deferDepth==0 immediate path,
// which is reached when SetByID is called from within an OnAdd hook
// (the hook fires during queue dispatch where deferDepth==0).
func TestCovSetByIDInHook(t *testing.T) {
	w := flecs.New()
	type covHookA struct{ V int }
	type covHookB struct{ V int }
	bID := flecs.RegisterComponent[covHookB](w)
	hookFired := false
	flecs.OnAdd[covHookA](w, func(fw *flecs.Writer, e flecs.ID, v covHookA) {
		hookFired = true
		fw.SetByID(e, bID, covHookB{V: 42})
	})
	var e1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, covHookA{V: 1})
	})
	if !hookFired {
		t.Error("OnAdd hook did not fire")
	}
	w.Read(func(r *flecs.Reader) {
		if v, ok := flecs.Get[covHookB](r, e1); !ok || v.V != 42 {
			t.Errorf("SetByID in hook: got %v %v", v, ok)
		}
	})
}

// TestCovUnionPairBatchRemove covers the union-pair remove in batchForEntity
// (cmd_queue.go:222) by queuing two commands for the same entity, one of
// which is a union-pair RemoveID.
func TestCovUnionPairBatchRemove(t *testing.T) {
	w := flecs.New()
	type covUnionTag struct{}
	var rel, tgt, e1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e1 = fw.NewEntity()
	})
	flecs.SetUnion(w, rel)
	// Add union pair immediately so the entity has it.
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e1, flecs.MakePair(rel, tgt))
	})
	type covExtraTag struct{}
	extraID := flecs.RegisterComponent[covExtraTag](w)
	// Two commands for e1: AddID(extraTag) + RemoveID(unionPair).
	// Having two commands triggers batchForEntity, which hits the union-pair break.
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e1, extraID)
		flecs.RemoveID(fw, e1, flecs.MakePair(rel, tgt))
	})
	w.Read(func(r *flecs.Reader) {
		if r.HasID(e1, flecs.MakePair(rel, tgt)) {
			t.Error("union pair should be removed")
		}
	})
}

// TestCovTableObserverTermNot covers tableMatchesTerms TermNot branch
// (observer_table.go:245) by registering a WithYieldExisting OnTableFill
// observer with a TermNot filter for a component that exists in a table.
func TestCovTableObserverTermNot(t *testing.T) {
	w := flecs.New()
	type covObsA struct{ V int }
	type covObsB struct{ V int }
	aID := flecs.RegisterComponent[covObsA](w)
	bID := flecs.RegisterComponent[covObsB](w)
	// Create entity with A first (so the A table exists).
	var e1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, covObsA{V: 1})
		flecs.Set(fw, e1, covObsB{V: 2})
	})
	// Register observer with TermNot(A) and yieldExisting.
	// Tables that have A will fail the TermNot check (covering the return path).
	callCount := 0
	flecs.OnTableFillWithOptions(w,
		flecs.WithQuery(flecs.Without(aID)).AndYieldExisting(),
		func(fw *flecs.Writer, tbl *flecs.Table) { callCount++ },
	)
	_ = bID
	_ = e1
	// callCount may be 0 or >0 depending on what tables exist; we just need the code to run.
}

// TestCovCachedQueryPrefabSkip covers tryMatchTable's skipPrefab early-return
// (cached_query.go:737) by creating a CachedQuery and then adding a Prefab entity.
func TestCovCachedQueryPrefabSkip(t *testing.T) {
	w := flecs.New()
	type covPrefSkip struct{ V int }
	posID := flecs.RegisterComponent[covPrefSkip](w)
	// CachedQuery with skipPrefab=true (default).
	cq := flecs.NewCachedQueryFromTerms(w, flecs.With(posID))
	// Create a Prefab entity — this triggers tryMatchTable on cq with a Prefab table.
	var pfb flecs.ID
	w.Write(func(fw *flecs.Writer) {
		pfb = fw.NewEntity()
		flecs.MarkPrefab(fw, pfb)
		flecs.Set(fw, pfb, covPrefSkip{V: 1})
	})
	count := 0
	it := cq.Iter()
	for it.Next() {
		count++
	}
	// Prefab should not appear in results.
	if count != 0 {
		t.Errorf("expected 0 non-prefab results, got %d", count)
	}
	_ = pfb
}

// TestCovSparseBatchRemove covers two coverage gaps:
// 1. cmd_queue.go:226 (sparse/DontFragment remove break in batchForEntity)
//    - triggered when two commands for the same entity include Remove[Sparse]
// 2. scope.go:692 (sparse remove returns false when entity lacks component)
//    - triggered by calling Remove[Sparse] on an entity without the component
func TestCovSparseBatchRemove(t *testing.T) {
	w := flecs.New()
	type covSparseRm struct{ V int }
	cID := flecs.RegisterComponent[covSparseRm](w)
	flecs.SetSparse(w, cID)
	type covExtraRm struct{}
	extraID := flecs.RegisterComponent[covExtraRm](w)
	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.Set(fw, e1, covSparseRm{V: 5})
	})
	// Two commands for e1: AddID(extra) + Remove[Sparse].
	// batchForEntity sees the sparse Remove and hits cmd_queue.go:226.
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e1, extraID)
		flecs.Remove[covSparseRm](fw, e1)
	})
	// Remove sparse from e2 which doesn't have it → scope.go:692 return false.
	removed := false
	w.Write(func(fw *flecs.Writer) {
		removed = flecs.Remove[covSparseRm](fw, e2)
	})
	if removed {
		t.Error("expected Remove to return false for entity without sparse component")
	}
}

// TestCovDeadEntityCalls covers three early-return paths for dead/nil entities:
// scope.go:149 (OwnsID), scope.go:511 (GetPairTarget), scope.go:258 (EntityComponents).
func TestCovDeadEntityCalls(t *testing.T) {
	w := flecs.New()
	type covDeadComp struct{ V int }
	relID := flecs.RegisterComponent[covDeadComp](w)
	var deadID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		deadID = fw.NewEntity()
		fw.Delete(deadID)
	})
	w.Read(func(r *flecs.Reader) {
		// OwnsID on dead entity → scope.go:149 return false.
		if r.OwnsID(deadID, relID) {
			t.Error("OwnsID should return false for dead entity")
		}
		// GetPairTarget on dead entity → scope.go:511 return 0,false.
		if tgt, ok := flecs.GetPairTarget(r, deadID, relID); ok || tgt != 0 {
			t.Errorf("GetPairTarget should return 0,false for dead entity, got %v %v", tgt, ok)
		}
		// EntityComponents on dead entity → scope.go:258 return nil.
		if comps := r.EntityComponents(deadID); comps != nil {
			t.Errorf("EntityComponents should return nil for dead entity, got %v", comps)
		}
	})
}

// TestCovWriterMethodWrappers covers the fw.MakeAlive and fw.SetVersion
// method wrappers (scope.go:404 and :407). Both panic in deferred scope,
// so each is called in a separate immediately-invoked closure with recover.
func TestCovWriterMethodWrappers(t *testing.T) {
	w := flecs.New()
	// Cover scope.go:404 — MakeAlive wrapper; panics in deferred scope.
	func() {
		defer func() { _ = recover() }()
		w.Write(func(fw *flecs.Writer) { fw.MakeAlive(flecs.ID(9999)) })
	}()
	// Cover scope.go:407 — SetVersion wrapper; panics in deferred scope.
	func() {
		defer func() { _ = recover() }()
		w.Write(func(fw *flecs.Writer) { fw.SetVersion(flecs.ID(1)) })
	}()
}

// TestCovEachSystemPipelineDirty covers the pipelineDirty rebuild path
// in Reader.EachSystem (scope.go:340).
func TestCovEachSystemPipelineDirty(t *testing.T) {
	w := flecs.New()
	// NewPhase + DependsOn sets pipelineDirty=true; anchoring avoids rebuild panic.
	phase := flecs.NewPhase(w, "covTestPhase")
	phase.DependsOn(w.OnUpdate())
	// EachSystem with a dirty pipeline triggers rebuildPipeline() (scope.go:341).
	w.Read(func(r *flecs.Reader) {
		count := 0
		r.EachSystem(phase, func(s *flecs.System) bool {
			count++
			return true
		})
		_ = count
	})
}

// TestCovRelVarIntersection covers the intersection branch in collectVarDomainFor
// (query.go:1101) by creating a query with two terms sharing the same relVar.
func TestCovRelVarIntersection(t *testing.T) {
	w := flecs.New()
	type covRVI struct{ V int }
	posID := flecs.RegisterComponent[covRVI](w)
	var tgt1, tgt2, e1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tgt1 = fw.NewEntity()
		tgt2 = fw.NewEntity()
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, covRVI{V: 1})
	})
	// Create a relationship rel that has pairs with both tgt1 and tgt2.
	var rel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		flecs.AddID(fw, e1, flecs.MakePair(rel, tgt1))
		flecs.AddID(fw, e1, flecs.MakePair(rel, tgt2))
	})
	_ = rel
	// Two terms with the same relVar "R" — first builds the domain set,
	// second hits the else/intersection branch (query.go:1101).
	q := flecs.NewQueryFromTerms(w,
		flecs.WithPairRelVar("R", tgt1),
		flecs.WithPairRelVar("R", tgt2),
		flecs.WithVar(posID, "E"),
	)
	w.Read(func(_ *flecs.Reader) {
		it := q.Iter()
		for it.Next() {
			_ = it
		}
	})
}
