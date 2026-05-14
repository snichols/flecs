package flecs

import (
	"sort"
	"unsafe"

	"github.com/snichols/flecs/internal/storage/table"
)

// tableRelDepth returns the depth of table t in the rel relationship hierarchy.
// A table with no (rel, *) pair in its signature has depth 0 (root). Each hop
// up the chain increments the depth by one.
func tableRelDepth(w *World, t *table.Table, rel ID) int {
	relIdx := rel.Index()
	target, ok := firstPairTarget(t.Type(), relIdx)
	if !ok {
		return 0
	}
	depth := 1
	seen := make(map[ID]struct{})
	current := target
	for depth < maxTraversalDepth {
		if !w.index.IsAlive(current) {
			break
		}
		rec := w.index.Get(current)
		if rec == nil || rec.Table == nil {
			break
		}
		next, ok := firstPairTarget(rec.Table.Type(), relIdx)
		if !ok {
			break
		}
		if _, visited := seen[next]; visited {
			break
		}
		seen[next] = struct{}{}
		current = next
		depth++
	}
	return depth
}

// CachedQuery is a persistent query handle whose matching table set is built
// once at construction and maintained incrementally as new tables are created
// by entity migrations. Callers amortize the cost of repeatedly running the
// same query: Iter() is O(matching-tables) with no per-call candidate-list
// allocation.
//
// Unlike *Query, which re-derives the candidate list on every Iter() call,
// CachedQuery pre-filters at construction and keeps the match set up to date
// via the World's table-creation notification hook (notifyTableCreated).
//
// Not-term cache correctness: archetype tables are immortal — once created,
// their signatures never change. When an entity loses a component, it migrates
// to a different table; the original table's signature (and its presence in this
// cache) is unaffected. Therefore, Not-term cache entries remain valid: if a
// table was rejected by tryMatchTable because it contained a Not ID, it is never
// added; if it was accepted (Not ID absent), it stays in the cache even if some
// entity later acquires that component and migrates away (the table signature is
// still absent of the Not ID). No stale-entry problem exists.
//
// Allocation profile:
//   - NewCachedQuery / NewCachedQueryFromTerms: 1 alloc for the struct,
//     1 for terms slice, 1 for andIDs slice (when non-trivial), 1 for tables
//     slice; initial population walks all existing tables once.
//   - Iter: 1 alloc for *QueryIter (same as uncached). No new table-list alloc.
//   - Each: same as Iter + user's fn.
//   - Close: 0 allocs.
//   - notifyTableCreated: O(cachedQueries × terms) per new table.
//
// The cached tables slice is owned by CachedQuery. QueryIter takes an
// unprotected reference to it; the cache's lifetime guarantees validity for
// the iterator's lifetime.
//
// # Change detection
//
// [CachedQuery.Changed] provides opt-in change detection. It returns true the
// first time it is called (initial state is "all changed"), and thereafter
// returns true only when a matching table was mutated since the last call:
//
//	q := flecs.NewCachedQuery(w, posID, velID)
//	for {
//	    if q.Changed() {
//	        runMovement(q)
//	    }
//	    w.Progress(0.016)
//	}
//
// A change is any of: a new matching table added to the cache, a column write
// (Set[T]/SetByID or pair write), or a structural change (entity added/removed
// via migrate). Because any column write on a cached table marks it dirty for
// ALL cached queries containing it, Changed() may over-report but never
// under-reports.
//
// *CachedQuery is NOT goroutine-safe; external synchronization is required.
type CachedQuery struct {
	w        *World
	terms    []Term         // sorted: And first (by ID), Not second, Or-groups third, Optional last
	andIDs   []ID           // pre-extracted And-term IDs; returned by Terms() for backward compat
	orGroups [][]ID         // each inner slice is one OR-group; tables must match all groups
	tables   []*table.Table // pre-filtered match set; grown by tryMatchTable, never shrunk
	// tableUpSources is parallel to tables. Each entry is a map from traversal
	// term component ID to resolved source entity (0 = self, non-zero = ancestor).
	// Nil for queries with no traversal terms.
	tableUpSources  []map[ID]ID
	cascadeTermTrav ID        // traversal relationship for the Cascade term; 0 if none
	removed         bool      // set by Close; deferred-removal model (see observer.go)
	nameObserver    *Observer // non-nil when any TermNameMatch term is present; unsubscribed on Close
	// sparseAndOnly is true when all TermAnd terms are sparse. Pure-sparse cached
	// queries iterate sparse-sets directly at Iter() time and do not cache archetype
	// tables (tryMatchTable is a no-op for them).
	sparseAndOnly bool
	// unionAndOnly is true when all TermAnd terms are union pairs. Pure-union cached
	// queries iterate union stores directly at Iter() time and do not cache archetype
	// tables (tryMatchTable is a no-op for them).
	unionAndOnly bool
	// Change detection state (Phase 9.5). Not goroutine-safe.
	lastChangeCounts map[*table.Table]uint64 // last seen ChangeCount per table; nil before first Changed() call
	tablesAdded      bool                    // set by tryMatchTable when a new table is appended
	// sparseVersions tracks the last-seen version of each sparse-set referenced by
	// a sparse term, for Changed() detection. Keyed by component entity index (same
	// as sparseStorage key). Nil until first Changed() call.
	sparseVersions map[ID]uint64
	// skipDisabled and skipPrefab: same implicit-skip semantics as Query. Tables
	// containing these tags are filtered out at cache-build time in tryMatchTable,
	// so they never appear in cq.tables. This is cheaper than per-iter filtering.
	skipDisabled bool
	skipPrefab   bool
	// Sorted-iteration state — non-nil only when WithOrderBy was used.
	orderBy             ID                      // sort-by component ID
	orderByCmp          OrderByFunc             // user comparator; nil = unsorted
	sortedEntities      []ID                    // entities in sorted order; rebuilt lazily
	sortedRows          []sortedFieldRow        // parallel (table, row) for each sorted entity
	sortedLastChange    map[*table.Table]uint64 // per-table ChangeCount at last rebuild
	sortedLastSparseVer map[ID]uint64           // per-sparse-set version at last rebuild (sparseAndOnly)
	// Group-by state — non-nil only when WithGroupBy was used.
	groupByComponent ID                        // component for invalidation; 0 = any change triggers re-group
	groupByFn        GroupByFunc               // partitioning callback; nil = no grouping
	groupsByID       map[uint64][]*table.Table // tables per group ID
	groupIDs         []uint64                  // sorted populated group IDs
	groupTableStart  map[uint64]int            // start index in cq.tables per group
	groupTableEnd    map[uint64]int            // exclusive end index in cq.tables per group
	groupLastChange  map[*table.Table]uint64   // per-table ChangeCount at last group rebuild
	// Per-group sorted ranges — valid when both groupByFn and orderByCmp are set.
	groupSortedStart map[uint64]int // start index in sortedEntities per group
	groupSortedEnd   map[uint64]int // exclusive end index in sortedEntities per group
}

// NewCachedQuery constructs a CachedQuery over w for the given component IDs
// (all TermAnd).
//
// Panics if w is nil or no IDs are provided (matching NewQuery semantics).
//
// Duplicate IDs in the term list are allowed but wasteful; uniqueness is not
// enforced. The provided ids are copied and sorted.
//
// Initial population iterates all existing tables in w.tables once
// (O(tables × terms)) and adds every match. Registration with w.cachedQueries
// enables incremental updates: when migrate() creates a new table,
// notifyTableCreated calls tryMatchTable on each active cached query.
//
// Removed (Close()d) entries in w.cachedQueries are pruned during registration
// (amortized compaction). This mirrors the observer-deferred-removal pattern.
func NewCachedQuery(w *World, ids ...ID) *CachedQuery {
	if w != nil {
		w.checkExclusiveAccessWrite()
	}
	if w == nil {
		panic("flecs: NewCachedQuery: world must not be nil")
	}
	if len(ids) == 0 {
		panic("flecs: NewCachedQuery: at least one term ID is required (zero-term queries are not supported)")
	}
	cp := make([]ID, len(ids))
	copy(cp, ids)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	terms := make([]Term, len(cp))
	for i, id := range cp {
		terms[i] = Term{ID: id, Kind: TermAnd,
			Sparse:       isSparseTermID(w, id),
			DontFragment: isDontFragmentTermID(w, id),
			Union:        isUnionTermID(w, id),
		}
		applyInheritablePromotion(w, &terms[i])
	}
	return newCachedQueryInternal(w, terms, cp, nil)
}

// NewCachedQueryFromTerms constructs a CachedQuery over w for the given
// structured terms, supporting TermAnd, TermNot, TermOr, and TermOptional kinds.
//
// Panics if:
//   - w is nil.
//   - no terms are provided.
//   - no TermAnd term is present.
//   - any two terms share the same ID across any term kinds.
//   - an Or term's ID is zero/invalid.
//   - two Or terms within the same group share the same ID.
//
// Terms are sorted internally: TermAnd first (by ID), TermNot second (by ID),
// TermOr-groups third (preserving group adjacency), TermOptional last (by ID).
// The caller's slice is not retained.
//
// Not/Or-term cache behaviour: tryMatchTable evaluates Not and Or terms at the
// moment a new table is created. Because table signatures are immutable, this is
// the only time evaluation is necessary. See type comment for full correctness
// argument.
func NewCachedQueryFromTerms(w *World, terms ...Term) *CachedQuery {
	if w != nil {
		w.checkExclusiveAccessWrite()
	}
	if w == nil {
		panic("flecs: NewCachedQueryFromTerms: world must not be nil")
	}
	cp, andIDs, orGroups := validateAndSortTerms(w, "flecs: NewCachedQueryFromTerms", terms)
	return newCachedQueryInternal(w, cp, andIDs, orGroups)
}

// newCachedQueryInternal is the shared construction path for NewCachedQuery and
// NewCachedQueryFromTerms. terms must already be validated and sorted; andIDs
// must be the pre-extracted And-term IDs; orGroups must be the pre-built OR-groups
// (nil for queries with no TermOr terms).
func newCachedQueryInternal(w *World, terms []Term, andIDs []ID, orGroups [][]ID) *CachedQuery {
	// Detect the Cascade term's traversal relationship (if any).
	var cascadeTermTrav ID
	for _, t := range terms {
		if t.Traverse == TraverseCascade {
			cascadeTermTrav = t.Trav
			break
		}
	}

	// Determine if all And terms are DontFragment or Union (pure non-archetype cached queries).
	// Sparse-only terms still live in archetype tables, so they don't qualify.
	// Fixed-source terms are excluded from this classification: they don't drive $this iteration.
	dontFragmentAndCount := 0
	archetypeAndCount := 0
	unionAndCount := 0
	for _, t := range terms {
		if t.Kind != TermAnd {
			continue
		}
		if t.Src != 0 {
			continue // fixed-source: does not drive $this iteration mode
		}
		if t.Union {
			unionAndCount++
		} else if t.DontFragment {
			dontFragmentAndCount++
		} else {
			archetypeAndCount++
		}
	}
	sparseAndOnly := archetypeAndCount == 0 && dontFragmentAndCount > 0 && unionAndCount == 0
	unionAndOnly := archetypeAndCount == 0 && dontFragmentAndCount == 0 && unionAndCount > 0

	skipDisabled, skipPrefab := computeQuerySkipFlags(w, terms)
	cq := &CachedQuery{
		w:               w,
		terms:           terms,
		andIDs:          andIDs,
		orGroups:        orGroups,
		tables:          make([]*table.Table, 0),
		cascadeTermTrav: cascadeTermTrav,
		sparseAndOnly:   sparseAndOnly,
		unionAndOnly:    unionAndOnly,
		skipDisabled:    skipDisabled,
		skipPrefab:      skipPrefab,
	}

	// Initial population: check every existing table (unordered during bulk load).
	for _, t := range w.tables {
		cq.tryMatchTable(t)
	}

	// Sort once after initial population when Cascade ordering is requested.
	if cascadeTermTrav != 0 {
		cq.sortByCascadeDepth()
	}

	// Subscribe to OnSet[Name] for TermNameMatch change detection.
	// When any entity's Name component is written, mark the cached query as changed
	// so that Changed() returns true on the next call.
	for _, t := range terms {
		if t.Kind == TermNameMatch {
			cq.nameObserver = ObserveID(w, w.nameID, EventOnSet, func(_ *Writer, _ ID, _ unsafe.Pointer) {
				cq.tablesAdded = true
			})
			break
		}
	}

	// Register with the world, pruning removed entries (amortized compaction).
	live := w.cachedQueries[:0]
	for _, q := range w.cachedQueries {
		if !q.removed {
			live = append(live, q)
		}
	}
	w.cachedQueries = append(live, cq)
	return cq
}

// sortByCascadeDepth sorts cq.tables (and the parallel cq.tableUpSources) in
// ascending order of their depth in the cq.cascadeTermTrav hierarchy (root first).
func (cq *CachedQuery) sortByCascadeDepth() {
	n := len(cq.tables)
	if n <= 1 {
		return
	}
	type entry struct {
		t       *table.Table
		sources map[ID]ID
	}
	entries := make([]entry, n)
	for i := range n {
		entries[i] = entry{cq.tables[i], cq.tableUpSources[i]}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return tableRelDepth(cq.w, entries[i].t, cq.cascadeTermTrav) <
			tableRelDepth(cq.w, entries[j].t, cq.cascadeTermTrav)
	})
	for i, e := range entries {
		cq.tables[i] = e.t
		if cq.tableUpSources != nil {
			cq.tableUpSources[i] = e.sources
		}
	}
}

// Terms returns the sorted TermAnd-only ID list for backward compatibility.
//
// For callers that predate structured terms, this returns only the TermAnd
// component IDs, matching the historic NewCachedQuery semantics. To get the
// full term list including TermNot and TermOptional terms, use TermsFull.
//
// Returns nil after Close.
// Callers must not mutate the returned slice.
func (cq *CachedQuery) Terms() []ID {
	if cq.removed {
		return nil
	}
	return cq.andIDs
}

// TermsFull returns a copy of the full structured term list in sorted order
// (TermAnd first, TermNot second, TermOr-groups third, TermOptional last).
// Returns nil after Close.
func (cq *CachedQuery) TermsFull() []Term {
	if cq.removed {
		return nil
	}
	cp := make([]Term, len(cq.terms))
	copy(cp, cq.terms)
	return cp
}

// Iter returns a fresh iterator walking the pre-filtered cached table list.
//
// Unlike the uncached Query.Iter, no per-candidate term check is performed
// inside Next: the cache is pre-filtered by tryMatchTable. For queries with
// TermOptional terms, Next still computes per-table optional presence so that
// FieldMaybe works correctly.
//
// For pure-sparse cached queries, Iter builds the sparse driver fresh on each
// call (sparse-sets may have changed since construction). For mixed queries, the
// cached archetype tables are used; per-entity sparse filtering is applied at
// iteration time.
//
// After Close, Next returns false immediately.
func (cq *CachedQuery) Iter() *QueryIter {
	if cq.removed {
		return &QueryIter{pos: -1}
	}

	// Resolve fixed-source terms fresh on each Iter() call so that mutations to
	// the source entity between iterations are visible on the next execution.
	fixedPtrs, fixedPresent, dead := buildFixedSourcePtrs(cq.w, cq.terms)
	if dead {
		// A required fixed-source component is absent → zero results.
		return &QueryIter{
			world:              cq.w,
			terms:              cq.terms,
			fixedSourcePtrs:    fixedPtrs,
			fixedSourcePresent: fixedPresent,
			pos:                0, // already past end
			wildcardTermIdx:    -1,
			wildcardPairPos:    -1,
		}
	}

	// Sorted iteration: lazily rebuild the sorted entity list and return a
	// sorted iterator. Wildcard expansion is incompatible with sorted mode and
	// is disabled (wildcardTermIdx = -1).
	if cq.orderByCmp != nil {
		if cq.groupByFn != nil {
			// Group+sort: check both rebuild conditions before any rebuild (tablesAdded
			// is cleared by rebuildGroups, so needsSortRebuild must be read first).
			needsSort := cq.needsSortRebuild()
			if needsSort || cq.needsGroupRebuild() {
				cq.rebuildGroups()
				cq.rebuildSorted()
			}
		} else if cq.needsSortRebuild() {
			cq.rebuildSorted()
		}
		hasSparseTerms := false
		for _, t := range cq.terms {
			if t.Kind == TermAnd && (t.Sparse || t.Union) {
				hasSparseTerms = true
				break
			}
		}
		return &QueryIter{
			world:              cq.w,
			terms:              cq.terms,
			orGroups:           cq.orGroups,
			sortedMode:         true,
			sortedPos:          -1,
			sortedEntities:     cq.sortedEntities,
			sortedRows:         cq.sortedRows,
			allSparse:          cq.sparseAndOnly,
			hasSparseTerms:     hasSparseTerms,
			cached:             true,
			pos:                -1,
			wildcardTermIdx:    -1,
			wildcardPairPos:    -1,
			fixedSourcePtrs:    fixedPtrs,
			fixedSourcePresent: fixedPresent,
		}
	}

	// Group-only (no sort): reorder cq.tables in group-ID order when stale.
	if cq.groupByFn != nil && cq.needsGroupRebuild() {
		cq.rebuildGroups()
	}

	wildcardIdx := findWildcardTermIdx(cq.w, cq.terms)

	if cq.sparseAndOnly {
		// Pure-DontFragment: build a fresh sparse driver (sparse-sets may have mutated).
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
		if zeroDriver {
			driver = nil
		}
		return &QueryIter{
			world:              cq.w,
			terms:              cq.terms,
			orGroups:           cq.orGroups,
			allSparse:          true,
			hasSparseTerms:     true,
			sparseDriver:       driver,
			sparseDriverPos:    -1,
			pos:                -1,
			wildcardTermIdx:    wildcardIdx,
			wildcardPairPos:    -1,
			fixedSourcePtrs:    fixedPtrs,
			fixedSourcePresent: fixedPresent,
		}
	}

	if cq.unionAndOnly {
		// Pure-Union: build a fresh union driver (union stores may have mutated).
		var driver []unionEntry
		zeroDriver := false
		minLen := -1
		for _, term := range cq.terms {
			if term.Kind != TermAnd || !term.Union {
				continue
			}
			relKey := ID(term.ID.First().Index())
			store, ok := cq.w.unionStore[relKey]
			if !ok || store == nil {
				zeroDriver = true
				break
			}
			if minLen < 0 || len(store.dense) < minLen {
				minLen = len(store.dense)
				snap := make([]unionEntry, len(store.dense))
				copy(snap, store.dense)
				driver = snap
			}
		}
		if zeroDriver {
			driver = nil
		}
		return &QueryIter{
			world:              cq.w,
			terms:              cq.terms,
			orGroups:           cq.orGroups,
			allUnion:           true,
			hasSparseTerms:     true,
			unionDriver:        driver,
			unionDriverPos:     -1,
			sparseDriverPos:    -1,
			pos:                -1,
			wildcardTermIdx:    wildcardIdx,
			wildcardPairPos:    -1,
			fixedSourcePtrs:    fixedPtrs,
			fixedSourcePresent: fixedPresent,
		}
	}

	hasSparseTerms := false
	for _, t := range cq.terms {
		if t.Kind == TermAnd && (t.Sparse || t.Union) {
			hasSparseTerms = true
			break
		}
		if t.Kind == TermScope && !isScopeTableSimple(t.Sub) {
			hasSparseTerms = true
			break
		}
		if t.Kind == TermEq || t.Kind == TermNotEq || t.Kind == TermNameMatch {
			hasSparseTerms = true
			break
		}
	}

	if hasSparseTerms {
		// Mixed: use cached archetype tables; per-entity sparse/DontFragment/scope filtering in nextMixed.
		return &QueryIter{
			world:              cq.w,
			terms:              cq.terms,
			orGroups:           cq.orGroups,
			hasSparseTerms:     true,
			candidates:         cq.tables,
			pos:                -1,
			cached:             true,
			tableSourcesRef:    cq.tableUpSources,
			sparseTablePos:     -1,
			sparseDriverPos:    -1,
			wildcardTermIdx:    wildcardIdx,
			wildcardPairPos:    -1,
			fixedSourcePtrs:    fixedPtrs,
			fixedSourcePresent: fixedPresent,
		}
	}

	// All-archetype: existing behavior.
	return &QueryIter{
		world:              cq.w,
		terms:              cq.terms,
		orGroups:           cq.orGroups,
		candidates:         cq.tables,
		pos:                -1,
		cached:             true,
		tableSourcesRef:    cq.tableUpSources,
		wildcardTermIdx:    wildcardIdx,
		wildcardPairPos:    -1,
		fixedSourcePtrs:    fixedPtrs,
		fixedSourcePresent: fixedPresent,
	}
}

// Each iterates all matching tables and calls fn once per table. The
// *QueryIter passed to fn is already positioned on a matching table; callers
// must NOT call Next inside fn.
func (cq *CachedQuery) Each(fn func(*QueryIter)) {
	it := cq.Iter()
	for it.Next() {
		fn(it)
	}
}

// Count returns the number of matching tables. O(1).
//
// This is the TABLE count, not the entity count. A table may contain many
// entities; use EntityCount for the total entity count.
// Returns 0 after Close.
func (cq *CachedQuery) Count() int {
	if cq.removed {
		return 0
	}
	return len(cq.tables)
}

// EntityCount returns the sum of entity counts across all cached tables.
// O(matching-tables). Returns 0 after Close.
func (cq *CachedQuery) EntityCount() int {
	if cq.removed {
		return 0
	}
	n := 0
	for _, t := range cq.tables {
		n += t.Count()
	}
	return n
}

// Close marks the query as removed. Idempotent: safe to call multiple times.
// After Close, Iter/Each/Terms/Count/EntityCount return empty results, and
// future table-creation notifications are ignored. O(1), 0 allocs.
func (cq *CachedQuery) Close() {
	if cq.nameObserver != nil {
		cq.nameObserver.Unsubscribe()
		cq.nameObserver = nil
	}
	cq.removed = true
}

// IsClosed reports whether Close has been called.
func (cq *CachedQuery) IsClosed() bool {
	return cq.removed
}

// tryMatchTable checks whether t satisfies all terms and OR-groups. If so, and
// t is not already in cq.tables, appends t. Skips if cq is removed.
//
// Matching predicate:
//   - TermAnd: t must contain the ID.
//   - TermNot: t must NOT contain the ID.
//   - TermOr (via orGroups): t must contain at least one ID from each OR-group.
//   - TermOptional: no effect on matching.
//
// Idempotent: does not add t more than once. The dedup check is defensive;
// the World guarantees that notifyTableCreated fires at most once per table.
//
// Not/Or-term correctness: table signatures are immutable. A table rejected here
// will never change its signature — no stale cache entries can result from
// entities migrating through the table.
func (cq *CachedQuery) tryMatchTable(t *table.Table) {
	if cq.removed {
		return
	}
	// Pure-sparse and pure-union queries have no archetype requirements; their tables
	// list stays empty. Iter() builds the driver fresh from stores each time.
	if cq.sparseAndOnly || cq.unionAndOnly {
		return
	}
	// Implicit skip: exclude tables containing Disabled or Prefab unless the
	// query explicitly mentioned the tag in any term kind. Filtered at cache-build
	// time so disabled/prefab tables never enter cq.tables.
	if cq.skipDisabled && t.HasComponent(cq.w.disabledID) {
		return
	}
	if cq.skipPrefab && t.HasComponent(cq.w.prefabID) {
		return
	}
	// Phase 1: Check TraverseSelf And terms and Not terms (fast path, no allocation).
	// DontFragment, Union, and fixed-source terms are skipped: they do not live in
	// archetype tables (or are pre-resolved at iter start for fixed-source).
	// Sparse-only terms DO live in archetype tables, so they are checked here.
	for _, term := range cq.terms {
		switch term.Kind {
		case TermAnd:
			if term.Src != 0 {
				break // fixed-source: not an archetype constraint; skip table matching
			}
			if term.DontFragment || term.Union {
				break // not in archetype; skip archetype check (verified per-entity at iteration time)
			}
			if term.Traverse == TraverseSelf && !t.HasComponent(term.ID) {
				// Wildcard/Any pair matching: sentinel IDs never appear in table
				// signatures; check for any concrete pair that satisfies the pattern.
				if isWildcardTerm(cq.w, term.ID) {
					if !tableHasWildcardMatch(cq.w, t, term.ID) {
						return
					}
				} else if term.ID.IsPair() {
					rel := ID(term.ID.First().Index())
					isTransitive := cq.w.transitivePolicies[rel]
					isReflexive := cq.w.reflexivePolicies[rel]
					matched := false
					if isTransitive {
						// Transitive pair matching: walk (R, *) chains.
						// Cached queries re-evaluate on table creation; pair-mutation
						// staleness is accepted and documented (see type comment).
						matched = transitiveTableMatches(cq.w, t, term.ID)
					}
					if !matched && isReflexive {
						// Reflexive self-match: target entity's own table qualifies.
						// Composes with Transitive. Staleness on entity migration is
						// accepted; cache invalidation on migration is a future enhancement.
						matched = reflexiveTableMatches(cq.w, t, term.ID)
					}
					if !matched {
						return
					}
				} else {
					return
				}
			}
		case TermNot:
			if term.DontFragment || term.Union {
				break // not in archetype; skip archetype check (checked per-entity at iteration time)
			}
			if t.HasComponent(term.ID) {
				return
			}
		}
	}
	// Phase 2: Check OR groups.
	for _, group := range cq.orGroups {
		matched := false
		for _, id := range group {
			if t.HasComponent(id) {
				matched = true
				break
			}
		}
		if !matched {
			return
		}
	}
	// Phase 2b: Check TermScope terms (table-level fast path for simple scopes).
	// NOT(A ∧ B ∧ … ∧ Z) is false for every entity in this table when all inner
	// archetype IDs are present in the table signature → reject the table.
	// Complex scopes (with Or-groups, DontFragment, Union, fixed-source, or nested
	// scopes) are deferred to per-entity evaluation during iteration.
	for _, term := range cq.terms {
		if term.Kind != TermScope {
			continue
		}
		if !isScopeTableSimple(term.Sub) {
			continue // complex scope: evaluated per entity during iteration
		}
		allPresent := true
		for _, sub := range term.Sub {
			if !t.HasComponent(sub.ID) {
				allPresent = false
				break
			}
		}
		if allPresent {
			return // scope never satisfied for any entity in this table
		}
	}
	// Phase 2c: TermEq/TermNotEq/TermNameMatch are per-entity predicates evaluated
	// in matchesSparseTerms during iteration. No table-level fast-skip is applied
	// here: a table-level skip based on TermEq's target entity position would
	// become stale when the entity migrates into an existing table (notifyTableCreated
	// only fires for newly created tables, so migration into an already-cached table
	// would not re-run tryMatchTable). Correctness takes priority over the optimisation.
	//
	// Future: a TermEq table-level skip is possible if paired with an EventOnAdd
	// observer on the target entity to trigger cache rebuild on migration.
	// Similarly, TermNameMatch could skip tables without a Name column.

	// Phase 3: Check traversal And terms; compute per-term resolved sources.
	var sources map[ID]ID
	for _, term := range cq.terms {
		if term.Kind != TermAnd {
			continue
		}
		switch term.Traverse {
		case TraverseUp:
			src, ok := findUpSource(cq.w, t, term.ID, term.Trav)
			if !ok {
				return
			}
			if sources == nil {
				sources = make(map[ID]ID)
			}
			sources[term.ID] = src
		case TraverseSelfUp, TraverseCascade:
			if t.HasComponent(term.ID) {
				if sources == nil {
					sources = make(map[ID]ID)
				}
				sources[term.ID] = 0 // self-matched
			} else {
				src, ok := findUpSource(cq.w, t, term.ID, term.Trav)
				if !ok {
					return
				}
				if sources == nil {
					sources = make(map[ID]ID)
				}
				sources[term.ID] = src
			}
		}
	}
	// Dedup: defensive guard against duplicate calls.
	for _, existing := range cq.tables {
		if existing == t {
			return
		}
	}
	cq.tables = append(cq.tables, t)
	cq.tableUpSources = append(cq.tableUpSources, sources)
	cq.tablesAdded = true
}

// Changed reports whether any matching table or sparse-set was mutated since the
// last call. Returns true on the first call (initial state is "all changed"). NOT
// goroutine-safe.
//
// A change is detected when:
//   - A new matching archetype table was added to the cache since the last call, OR
//   - Any cached table had a column write (Set[T]/SetByID or pair write), OR
//   - Any cached table had a structural change (entity added/removed via migrate), OR
//   - Any sparse-set referenced by a sparse term had a structural change (new entity
//     added or entity removed).
//
// Because any column write on a cached table marks it dirty for ALL cached
// queries containing it, Changed() may over-report but never under-reports.
// The change counter is monotonic uint64; counter wrap is treated as a change.
//
// Returns false after Close.
func (cq *CachedQuery) Changed() bool {
	if cq.removed {
		return false
	}
	if cq.tablesAdded {
		cq.tablesAdded = false
		if cq.lastChangeCounts == nil {
			cq.lastChangeCounts = make(map[*table.Table]uint64, len(cq.tables))
		}
		for _, t := range cq.tables {
			cq.lastChangeCounts[t] = t.ChangeCount()
		}
		// Sync sparse versions so the next call doesn't double-report the same change.
		for _, term := range cq.terms {
			if !term.Sparse {
				continue
			}
			key := ID(term.ID.Index())
			ss := cq.w.sparseStorage[key]
			if ss == nil {
				continue
			}
			if cq.sparseVersions == nil {
				cq.sparseVersions = make(map[ID]uint64)
			}
			cq.sparseVersions[key] = ss.version
		}
		// Sync sparse versions for scope-internal sparse terms.
		for _, term := range cq.terms {
			if term.Kind != TermScope {
				continue
			}
			for _, sub := range term.Sub {
				if !sub.Sparse {
					continue
				}
				key := ID(sub.ID.Index())
				ss := cq.w.sparseStorage[key]
				if ss == nil {
					continue
				}
				if cq.sparseVersions == nil {
					cq.sparseVersions = make(map[ID]uint64)
				}
				cq.sparseVersions[key] = ss.version
			}
		}
		return true
	}
	changed := false
	for _, t := range cq.tables {
		cc := t.ChangeCount()
		if cc != cq.lastChangeCounts[t] {
			cq.lastChangeCounts[t] = cc
			changed = true
		}
	}
	// Check sparse-set versions for any sparse term.
	for _, term := range cq.terms {
		if !term.Sparse {
			continue
		}
		key := ID(term.ID.Index())
		ss := cq.w.sparseStorage[key]
		if ss == nil {
			continue
		}
		if cq.sparseVersions == nil {
			cq.sparseVersions = make(map[ID]uint64)
		}
		last, seen := cq.sparseVersions[key]
		if !seen || ss.version != last {
			cq.sparseVersions[key] = ss.version
			changed = true
		}
	}
	// Check sparse-set versions for scope-internal sparse sub-terms.
	for _, term := range cq.terms {
		if term.Kind != TermScope {
			continue
		}
		for _, sub := range term.Sub {
			if !sub.Sparse {
				continue
			}
			key := ID(sub.ID.Index())
			ss := cq.w.sparseStorage[key]
			if ss == nil {
				continue
			}
			if cq.sparseVersions == nil {
				cq.sparseVersions = make(map[ID]uint64)
			}
			last, seen := cq.sparseVersions[key]
			if !seen || ss.version != last {
				cq.sparseVersions[key] = ss.version
				changed = true
			}
		}
	}
	return changed
}
