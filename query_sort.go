package flecs

import (
	"sort"
	"unsafe"

	"github.com/snichols/flecs/internal/storage/table"
)

// OrderByFunc is a comparator for sorted cached queries. Returns negative if
// entity A should come before entity B, zero if equal, positive if A should come
// after B. vA and vB point to the sort-by component value for each entity; they
// may be nil when the sort-by component is TermOptional and not present on that
// entity.
type OrderByFunc func(eA ID, vA unsafe.Pointer, eB ID, vB unsafe.Pointer) int

// OrderBy wraps a typed comparator as an OrderByFunc.
// The component type T must match the sort-by component's registered Go type.
// Nil vA/vB pointers are forwarded as nil *T (TermOptional absent case).
func OrderBy[T any](cmp func(eA ID, vA *T, eB ID, vB *T) int) OrderByFunc {
	return func(eA ID, vA unsafe.Pointer, eB ID, vB unsafe.Pointer) int {
		var pa, pb *T
		if vA != nil {
			pa = (*T)(vA)
		}
		if vB != nil {
			pb = (*T)(vB)
		}
		return cmp(eA, pa, eB, pb)
	}
}

// CachedQueryOptions carries optional configuration for
// [NewCachedQueryFromTermsWithOptions]. Construct via [WithOrderBy],
// [WithGroupBy], or combine both; use the zero value for no options.
type CachedQueryOptions struct {
	orderByComponent ID
	orderByCmp       OrderByFunc
	groupByComponent ID
	groupByFn        GroupByFunc
}

// WithOrderBy returns options that sort iteration results by componentID using
// cmp.
//
// componentID must appear as a TermAnd or TermOptional term in the query. Panics
// at query construction if the component is absent from the term set, or if it is
// a pair ID (pair-form sort is a v0.59.0 non-goal; see CHANGELOG).
//
// Sorting is cached: the sorted list is rebuilt lazily on the next Iter() call
// when any matching table's ChangeCount changes (column write, structural change)
// or when new tables are added (cq.tablesAdded). The rebuild uses a global
// sort.SliceStable rather than upstream's two-step per-table quicksort + k-way
// merge; see CHANGELOG v0.59.0 "Design decisions recorded" for rationale.
func WithOrderBy(componentID ID, cmp OrderByFunc) CachedQueryOptions {
	return CachedQueryOptions{orderByComponent: componentID, orderByCmp: cmp}
}

// sortedFieldRow records the archetype position for one entity in the sorted
// list. For pure-DontFragment sorted queries, table is nil and row is 0.
type sortedFieldRow struct {
	table *table.Table
	row   int
}

// NewCachedQueryFromTermsWithOptions constructs a sorted CachedQuery.
//
// opts carries optional configuration (e.g. [WithOrderBy]). If both
// opts.orderByComponent and opts.orderByCmp are zero/nil, the query behaves
// identically to [NewCachedQueryFromTerms].
//
// Panics with the same conditions as [NewCachedQueryFromTerms], plus:
//   - opts.orderByComponent is not TermAnd or TermOptional in the term set.
//   - opts.orderByComponent is a pair ID (pair-form sort not supported in v0.59.0).
func NewCachedQueryFromTermsWithOptions(w *World, opts CachedQueryOptions, terms ...Term) *CachedQuery {
	if w != nil {
		w.checkExclusiveAccessWrite()
	}
	if w == nil {
		panic("flecs: NewCachedQueryFromTermsWithOptions: world must not be nil")
	}
	cp, andIDs, orGroups, alwaysFalse := validateAndSortTerms(w, "flecs: NewCachedQueryFromTermsWithOptions", terms)
	if alwaysFalse {
		return &CachedQuery{w: w, alwaysFalse: true}
	}

	if opts.orderByCmp != nil {
		cid := opts.orderByComponent
		if cid.IsPair() {
			panic("flecs: NewCachedQueryFromTermsWithOptions: WithOrderBy: pair-form sort components are not supported in v0.59.0 (use a packed struct as the component type for multi-key sort)")
		}
		found := false
		for _, t := range cp {
			if t.ID == cid && (t.Kind == TermAnd || t.Kind == TermOptional) {
				found = true
				break
			}
		}
		if !found {
			panic("flecs: NewCachedQueryFromTermsWithOptions: WithOrderBy: sort-by component must appear as TermAnd or TermOptional in the query term set (Or and Not terms do not supply a readable value)")
		}
	}

	if opts.groupByFn != nil && opts.groupByComponent != 0 {
		cid := opts.groupByComponent
		if cid.IsPair() {
			panic("flecs: NewCachedQueryFromTermsWithOptions: WithGroupBy: pair-form group-by components are not supported")
		}
		found := false
		for _, t := range cp {
			if t.ID == cid && (t.Kind == TermAnd || t.Kind == TermOptional) {
				found = true
				break
			}
		}
		if !found {
			panic("flecs: NewCachedQueryFromTermsWithOptions: WithGroupBy: group-by component must appear as TermAnd or TermOptional in the query term set")
		}
	}

	cq := newCachedQueryInternal(w, cp, andIDs, orGroups)
	if opts.groupByFn != nil {
		cq.groupByComponent = opts.groupByComponent
		cq.groupByFn = opts.groupByFn
		cq.rebuildGroups()
	}
	if opts.orderByCmp != nil {
		cq.orderBy = opts.orderByComponent
		cq.orderByCmp = opts.orderByCmp
		cq.rebuildSorted()
	}
	return cq
}

// needsSortRebuild reports whether the sorted entity list needs to be rebuilt
// before the next Iter() returns a sorted iterator.
func (cq *CachedQuery) needsSortRebuild() bool {
	if cq.tablesAdded {
		return true
	}
	for _, t := range cq.tables {
		if t.ChangeCount() != cq.sortedLastChange[t] {
			return true
		}
	}
	// For pure-DontFragment queries, check sparse-set structural changes
	// (DontFragment components do not live in archetype tables, so ChangeCount
	// is not bumped when they are added/removed; version tracks that instead).
	if cq.sparseAndOnly {
		for _, term := range cq.terms {
			if term.Kind != TermAnd || !term.DontFragment {
				continue
			}
			key := ID(term.ID.Index())
			ss := cq.w.sparseStorage[key]
			if ss == nil {
				continue
			}
			last, seen := cq.sortedLastSparseVer[key]
			if !seen || ss.version != last {
				return true
			}
		}
	}
	return false
}

// rebuildSorted rebuilds cq.sortedEntities and cq.sortedRows from the current
// matching entity set, sorted by cq.orderByCmp on cq.orderBy.
func (cq *CachedQuery) rebuildSorted() {
	type sortEntry struct {
		entity ID
		tbl    *table.Table
		row    int
		valPtr unsafe.Pointer
	}

	orderByIsSparse := isSparseTermID(cq.w, cq.orderBy)

	getValPtr := func(e ID, t *table.Table, row int) unsafe.Pointer {
		if orderByIsSparse {
			return sparseSetGet(cq.w, e, cq.orderBy)
		}
		if t == nil || !t.HasComponent(cq.orderBy) {
			return nil
		}
		base, typ, _ := t.ColumnBasePtr(cq.orderBy)
		if base == nil || typ == nil {
			return nil // tag component
		}
		return unsafe.Add(base, uintptr(row)*typ.Size())
	}

	hasDontFragOrUnion := false
	for _, term := range cq.terms {
		if (term.Kind == TermAnd || term.Kind == TermNot) && (term.DontFragment || term.Union) {
			hasDontFragOrUnion = true
			break
		}
	}

	var entries []sortEntry

	if cq.sparseAndOnly {
		// Pure-DontFragment: walk the smallest sparse-set driver.
		var driver []sparseEntry
		zeroDriver := false
		minLen := -1
		for _, term := range cq.terms {
			if term.Kind != TermAnd || !term.DontFragment {
				continue
			}
			key := ID(term.ID.Index())
			ss := cq.w.sparseStorage[key]
			if ss == nil {
				zeroDriver = true
				break
			}
			if minLen < 0 || len(ss.dense) < minLen {
				minLen = len(ss.dense)
				snap := make([]sparseEntry, len(ss.dense))
				copy(snap, ss.dense)
				driver = snap
			}
		}
		if !zeroDriver {
			for _, se := range driver {
				e := se.entity
				if !sortMatchesSparseFilter(cq.w, cq.terms, e) {
					continue
				}
				entries = append(entries, sortEntry{e, nil, 0, getValPtr(e, nil, 0)})
			}
		}
		// Sparse-only + group-by: not supported in v0.66.0; sort flat.
		sort.SliceStable(entries, func(i, j int) bool {
			return cq.orderByCmp(entries[i].entity, entries[i].valPtr,
				entries[j].entity, entries[j].valPtr) < 0
		})
	} else if cq.groupByFn != nil {
		// Group+sort: collect entities per group, sort within each group, then
		// concatenate in group-ID order. groupsByID and groupTableStart/End must
		// already be populated by a preceding rebuildGroups call.
		cq.groupSortedStart = make(map[uint64]int, len(cq.groupIDs))
		cq.groupSortedEnd = make(map[uint64]int, len(cq.groupIDs))
		for _, gid := range cq.groupIDs {
			groupStart := len(entries)
			start := cq.groupTableStart[gid]
			end := cq.groupTableEnd[gid]
			for _, t := range cq.tables[start:end] {
				ents := t.Entities()
				for row, e := range ents {
					if hasDontFragOrUnion && !sortMatchesSparseFilter(cq.w, cq.terms, e) {
						continue
					}
					entries = append(entries, sortEntry{e, t, row, getValPtr(e, t, row)})
				}
			}
			// Sort this group's slice in place.
			groupSlice := entries[groupStart:]
			sort.SliceStable(groupSlice, func(i, j int) bool {
				return cq.orderByCmp(groupSlice[i].entity, groupSlice[i].valPtr,
					groupSlice[j].entity, groupSlice[j].valPtr) < 0
			})
			cq.groupSortedStart[gid] = groupStart
			cq.groupSortedEnd[gid] = len(entries)
		}
	} else {
		// Archetype or mixed: walk all cached tables.
		for _, t := range cq.tables {
			ents := t.Entities()
			for row, e := range ents {
				if hasDontFragOrUnion && !sortMatchesSparseFilter(cq.w, cq.terms, e) {
					continue
				}
				entries = append(entries, sortEntry{e, t, row, getValPtr(e, t, row)})
			}
		}
		sort.SliceStable(entries, func(i, j int) bool {
			return cq.orderByCmp(entries[i].entity, entries[i].valPtr,
				entries[j].entity, entries[j].valPtr) < 0
		})
	}

	// Rebuild parallel slices, reusing existing capacity when possible.
	n := len(entries)
	if cap(cq.sortedEntities) >= n {
		cq.sortedEntities = cq.sortedEntities[:n]
		cq.sortedRows = cq.sortedRows[:n]
	} else {
		cq.sortedEntities = make([]ID, n)
		cq.sortedRows = make([]sortedFieldRow, n)
	}
	for i, e := range entries {
		cq.sortedEntities[i] = e.entity
		cq.sortedRows[i] = sortedFieldRow{e.tbl, e.row}
	}

	// Update table change-count tracking.
	if cq.sortedLastChange == nil {
		cq.sortedLastChange = make(map[*table.Table]uint64, len(cq.tables))
	}
	for _, t := range cq.tables {
		cq.sortedLastChange[t] = t.ChangeCount()
	}

	// Update sparse-set version tracking (pure-DontFragment queries).
	if cq.sparseAndOnly {
		if cq.sortedLastSparseVer == nil {
			cq.sortedLastSparseVer = make(map[ID]uint64)
		}
		for _, term := range cq.terms {
			if term.Kind != TermAnd || !term.DontFragment {
				continue
			}
			key := ID(term.ID.Index())
			if ss := cq.w.sparseStorage[key]; ss != nil {
				cq.sortedLastSparseVer[key] = ss.version
			}
		}
	}

	cq.tablesAdded = false
}

// sortMatchesSparseFilter reports whether entity e satisfies all DontFragment
// and Union TermAnd/TermNot requirements. Used during sorted rebuild to filter
// entities from archetype tables in mixed-mode queries.
func sortMatchesSparseFilter(w *World, terms []Term, e ID) bool {
	for _, term := range terms {
		if term.DontFragment {
			ptr := sparseSetGet(w, e, term.ID)
			switch term.Kind {
			case TermAnd:
				if ptr == nil {
					return false
				}
			case TermNot:
				if ptr != nil {
					return false
				}
			}
		} else if term.Union {
			relKey := ID(term.ID.First().Index())
			store, ok := w.unionStore[relKey]
			switch term.Kind {
			case TermAnd:
				if !ok || store == nil {
					return false
				}
				pos, has := store.index[ID(e.Index())]
				if !has {
					return false
				}
				termTarget := term.ID.Second()
				if !isWildcardID(w, termTarget) {
					if store.dense[pos].target.Index() != termTarget.Index() {
						return false
					}
				}
			case TermNot:
				if !ok || store == nil {
					continue
				}
				_, has := store.index[ID(e.Index())]
				if has {
					return false
				}
			}
		}
	}
	return true
}

// nextSorted advances the sorted iterator by one entity. Called by
// [QueryIter.Next] when sortedMode is true.
func (it *QueryIter) nextSorted() bool {
	it.sortedPos++
	if it.sortedPos >= len(it.sortedEntities) {
		it.current = nil
		it.sparseEntity = 0
		return false
	}
	pos := it.sortedPos
	e := it.sortedEntities[pos]
	row := it.sortedRows[pos]

	it.sparseEntity = e
	it.current = row.table
	// Enable the worker-clip path in Field[T]/FieldMaybe[T] for archetype terms:
	// wFirst=row index, wCount=1, workerTotal=1 → Field returns full[row:row+1].
	it.wFirst = row.row
	it.wCount = 1
	it.workerTotal = 1

	// Update optional-term presence for this entity.
	if row.table != nil {
		it.updateOptionalPresenceMixed(row.table, e)
	} else {
		it.updateOptionalPresenceSparse(e)
	}
	return true
}
