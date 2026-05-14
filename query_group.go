package flecs

import (
	"sort"

	"github.com/snichols/flecs/internal/storage/table"
)

// GroupByFunc assigns a group ID to a matched table. Called once per table at
// cache-build time and after each invalidation. Tables assigned the same group
// ID are iterated together; default Iter() walks groups in ascending ID order.
type GroupByFunc func(t *table.Table) uint64

// WithGroupBy returns options that partition a cached query's matched tables
// into groups identified by uint64 keys. Use with
// [NewCachedQueryFromTermsWithOptions].
//
// componentID, if non-zero, must appear as a TermAnd or TermOptional term in
// the query. It identifies the component whose mutations trigger re-grouping
// (detected via ChangeCount changes). Pass 0 to trigger on any table change.
//
// groupFn is called for each matched table to compute its group ID. Panics at
// query construction if componentID is non-zero and does not appear as TermAnd
// or TermOptional in the term set, or if it is a pair ID.
//
// To also sort within each group, chain [CachedQueryOptions.AndOrderBy]:
//
//	WithGroupBy(compID, fn).AndOrderBy(sortCompID, cmp)
func WithGroupBy(componentID ID, groupFn GroupByFunc) CachedQueryOptions {
	return CachedQueryOptions{groupByComponent: componentID, groupByFn: groupFn}
}

// AndGroupBy returns a copy of opts with group-by configured. It is equivalent
// to merging WithGroupBy into an existing options set (e.g. one produced by
// [WithOrderBy]):
//
//	WithOrderBy(sortComp, cmp).AndGroupBy(groupComp, fn)
func (opts CachedQueryOptions) AndGroupBy(componentID ID, groupFn GroupByFunc) CachedQueryOptions {
	opts.groupByComponent = componentID
	opts.groupByFn = groupFn
	return opts
}

// AndOrderBy returns a copy of opts with sort-by configured. It is equivalent
// to merging [WithOrderBy] into an existing options set (e.g. one produced by
// [WithGroupBy]):
//
//	WithGroupBy(groupComp, fn).AndOrderBy(sortComp, cmp)
func (opts CachedQueryOptions) AndOrderBy(componentID ID, cmp OrderByFunc) CachedQueryOptions {
	opts.orderByComponent = componentID
	opts.orderByCmp = cmp
	return opts
}

// needsGroupRebuild reports whether the group assignments are stale and need
// recomputing via rebuildGroups.
func (cq *CachedQuery) needsGroupRebuild() bool {
	if cq.tablesAdded {
		return true
	}
	for _, t := range cq.tables {
		if t.ChangeCount() != cq.groupLastChange[t] {
			return true
		}
	}
	return false
}

// rebuildGroups recomputes cq.groupsByID, cq.groupIDs, cq.groupTableStart,
// cq.groupTableEnd, and reorders cq.tables (and cq.tableUpSources) in ascending
// group-ID order.
func (cq *CachedQuery) rebuildGroups() {
	n := len(cq.tables)
	if n == 0 {
		cq.groupsByID = nil
		if cq.groupIDs != nil {
			cq.groupIDs = cq.groupIDs[:0]
		}
		cq.groupTableStart = nil
		cq.groupTableEnd = nil
		if cq.groupLastChange == nil {
			cq.groupLastChange = make(map[*table.Table]uint64)
		}
		cq.tablesAdded = false
		return
	}

	type slot struct {
		t       *table.Table
		sources map[ID]ID
		gid     uint64
	}
	slots := make([]slot, n)
	groupMap := make(map[uint64][]*table.Table, 4)
	for i, t := range cq.tables {
		var srcs map[ID]ID
		if cq.tableUpSources != nil {
			srcs = cq.tableUpSources[i]
		}
		gid := cq.groupByFn(t)
		slots[i] = slot{t, srcs, gid}
		groupMap[gid] = append(groupMap[gid], t)
	}
	cq.groupsByID = groupMap

	// Collect and sort group IDs.
	gids := make([]uint64, 0, len(groupMap))
	for gid := range groupMap {
		gids = append(gids, gid)
	}
	sort.Slice(gids, func(i, j int) bool { return gids[i] < gids[j] })
	cq.groupIDs = gids

	// Build rank map for stable-sort of slots.
	rank := make(map[uint64]int, len(gids))
	for i, g := range gids {
		rank[g] = i
	}

	// Stable-sort slots by group rank, preserving within-group table order.
	sort.SliceStable(slots, func(i, j int) bool {
		return rank[slots[i].gid] < rank[slots[j].gid]
	})

	// Apply sorted order back to cq.tables and cq.tableUpSources.
	for i, s := range slots {
		cq.tables[i] = s.t
		if cq.tableUpSources != nil {
			cq.tableUpSources[i] = s.sources
		}
	}

	// Record contiguous ranges per group in cq.tables.
	cq.groupTableStart = make(map[uint64]int, len(gids))
	cq.groupTableEnd = make(map[uint64]int, len(gids))
	pos := 0
	for _, gid := range gids {
		cnt := len(groupMap[gid])
		cq.groupTableStart[gid] = pos
		pos += cnt
		cq.groupTableEnd[gid] = pos
	}

	// Update per-table ChangeCount tracking.
	if cq.groupLastChange == nil {
		cq.groupLastChange = make(map[*table.Table]uint64, n)
	}
	for _, t := range cq.tables {
		cq.groupLastChange[t] = t.ChangeCount()
	}
	cq.tablesAdded = false
}

// Groups returns a sorted copy of the currently-populated group IDs. Triggers a
// lazy rebuild if the group state is stale. Returns nil if WithGroupBy was not
// used or after Close; returns a non-nil empty slice when no tables are matched.
func (cq *CachedQuery) Groups() []uint64 {
	if cq.removed || cq.groupByFn == nil {
		return nil
	}
	if cq.needsGroupRebuild() {
		cq.rebuildGroups()
	}
	if len(cq.groupIDs) == 0 {
		return []uint64{}
	}
	cp := make([]uint64, len(cq.groupIDs))
	copy(cp, cq.groupIDs)
	return cp
}

// IterGroup returns an iterator over the matched tables belonging to groupID,
// in their cached order. O(1) startup: the group is looked up directly from the
// internal group map built by the last rebuildGroups call.
//
// Returns an exhausted iterator (Next == false immediately) when groupID is not
// populated or when the query has no matching tables for that group.
//
// When the query was also constructed with WithOrderBy, IterGroup yields entities
// sorted within the group by the order-by component.
//
// Panics if the query was not constructed with WithGroupBy.
func (cq *CachedQuery) IterGroup(groupID uint64) *QueryIter {
	if cq.removed {
		return &QueryIter{pos: -1}
	}
	if cq.groupByFn == nil {
		panic("flecs: IterGroup: query was not constructed with WithGroupBy")
	}

	// Ensure group (and sort) state is current. Read needsSortRebuild first
	// because rebuildGroups clears tablesAdded, which needsSortRebuild also
	// checks.
	needsGroup := cq.needsGroupRebuild()
	needsSort := cq.orderByCmp != nil && cq.needsSortRebuild()
	if needsGroup || needsSort {
		cq.rebuildGroups()
		if cq.orderByCmp != nil {
			cq.rebuildSorted()
		}
	}

	if cq.orderByCmp != nil {
		// Sorted group iteration: return a sub-slice of the pre-sorted entity list.
		start, ok := cq.groupSortedStart[groupID]
		if !ok {
			return &QueryIter{pos: -1}
		}
		end := cq.groupSortedEnd[groupID]
		hasSparseTerms := false
		for _, t := range cq.terms {
			if t.Kind == TermAnd && (t.Sparse || t.Union) {
				hasSparseTerms = true
				break
			}
		}
		return &QueryIter{
			world:           cq.w,
			terms:           cq.terms,
			orGroups:        cq.orGroups,
			sortedMode:      true,
			sortedPos:       -1,
			sortedEntities:  cq.sortedEntities[start:end],
			sortedRows:      cq.sortedRows[start:end],
			allSparse:       cq.sparseAndOnly,
			hasSparseTerms:  hasSparseTerms,
			cached:          true,
			pos:             -1,
			wildcardTermIdx: -1,
			wildcardPairPos: -1,
		}
	}

	// Regular group iteration: use the contiguous sub-slice of cq.tables.
	start, ok := cq.groupTableStart[groupID]
	if !ok {
		return &QueryIter{pos: -1}
	}
	end := cq.groupTableEnd[groupID]

	var sources []map[ID]ID
	if cq.tableUpSources != nil {
		sources = cq.tableUpSources[start:end]
	}
	wildcardIdx := findWildcardTermIdx(cq.w, cq.terms)

	hasSparseTerms := false
	for _, t := range cq.terms {
		if t.Kind == TermAnd && (t.Sparse || t.Union) {
			hasSparseTerms = true
			break
		}
	}

	if hasSparseTerms {
		return &QueryIter{
			world:           cq.w,
			terms:           cq.terms,
			orGroups:        cq.orGroups,
			hasSparseTerms:  true,
			candidates:      cq.tables[start:end],
			pos:             -1,
			cached:          true,
			tableSourcesRef: sources,
			sparseTablePos:  -1,
			sparseDriverPos: -1,
			wildcardTermIdx: wildcardIdx,
			wildcardPairPos: -1,
		}
	}

	return &QueryIter{
		world:           cq.w,
		terms:           cq.terms,
		orGroups:        cq.orGroups,
		candidates:      cq.tables[start:end],
		pos:             -1,
		cached:          true,
		tableSourcesRef: sources,
		wildcardTermIdx: wildcardIdx,
		wildcardPairPos: -1,
	}
}
