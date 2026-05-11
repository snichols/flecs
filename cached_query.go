package flecs

import (
	"sort"

	"github.com/snichols/flecs/internal/storage/table"
)

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
// Allocation profile:
//   - NewCachedQuery: 1 alloc for the struct, 1 for terms slice, 1 for tables
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
// *CachedQuery is NOT goroutine-safe; external synchronization is required.
type CachedQuery struct {
	w       *World
	terms   []ID           // sorted copy; uniqueness not enforced (duplicates are wasteful but correct)
	tables  []*table.Table // pre-filtered match set; grown by tryMatchTable, never shrunk
	removed bool           // set by Close; deferred-removal model (see observer.go)
}

// NewCachedQuery constructs a CachedQuery over w for the given component IDs.
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
	if w == nil {
		panic("flecs: NewCachedQuery: world must not be nil")
	}
	if len(ids) == 0 {
		panic("flecs: NewCachedQuery: at least one term ID is required (zero-term queries are not supported)")
	}
	cp := make([]ID, len(ids))
	copy(cp, ids)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	cq := &CachedQuery{w: w, terms: cp, tables: make([]*table.Table, 0)}

	// Initial population: check every existing table.
	for _, t := range w.tables {
		cq.tryMatchTable(t)
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

// Terms returns the query's sorted term list. Returns nil after Close.
// Callers must not mutate the returned slice.
func (cq *CachedQuery) Terms() []ID {
	if cq.removed {
		return nil
	}
	return cq.terms
}

// Iter returns a fresh iterator walking the pre-filtered cached table list.
//
// Unlike the uncached Query.Iter, no per-candidate HasComponent check is
// performed inside Next: the cache is pre-filtered by tryMatchTable.
//
// After Close, Next returns false immediately.
func (cq *CachedQuery) Iter() *QueryIter {
	if cq.removed {
		return &QueryIter{pos: -1}
	}
	return &QueryIter{
		candidates: cq.tables,
		pos:        -1,
		cached:     true,
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
	cq.removed = true
}

// IsClosed reports whether Close has been called.
func (cq *CachedQuery) IsClosed() bool {
	return cq.removed
}

// tryMatchTable checks whether t satisfies all terms. If so, and t is not
// already in cq.tables, appends t. Skips if cq is removed.
//
// Idempotent: does not add t more than once. The dedup check is defensive;
// the World guarantees that notifyTableCreated fires at most once per table.
func (cq *CachedQuery) tryMatchTable(t *table.Table) {
	if cq.removed {
		return
	}
	for _, id := range cq.terms {
		if !t.HasComponent(id) {
			return
		}
	}
	// Dedup: defensive guard against duplicate calls.
	for _, existing := range cq.tables {
		if existing == t {
			return
		}
	}
	cq.tables = append(cq.tables, t)
}
