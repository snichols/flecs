package flecs

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/snichols/flecs/internal/storage/table"
)

// Query holds a fixed AND-term list of component IDs that an entity must ALL
// have. Phase 3.1 scope: AND terms only, allocation-light iteration, typed
// field access. NOT/Optional/OR/pairs/wildcards are later phases.
//
// Mutation warning: calling Set, Remove, or Delete on the World from inside
// Each or QueryIter.Next produces undefined behaviour; the iterator holds a
// snapshot of the seed table list taken at Iter() time and does not handle
// structural changes.
type Query struct {
	w     *World
	terms []ID // sorted copy; do not mutate
}

// NewQuery constructs a query over w for the given component IDs.
//
// Panics if w is nil or no IDs are provided. Zero-term queries (match all
// entities) are a Phase 4 concern and are not supported here.
//
// Duplicate IDs in the term list are allowed but wasteful; the match still
// works correctly because HasComponent is idempotent.
//
// The provided ids are copied and sorted; the caller's slice is not retained.
func NewQuery(w *World, ids ...ID) *Query {
	if w == nil {
		panic("flecs: NewQuery: world must not be nil")
	}
	if len(ids) == 0 {
		panic("flecs: NewQuery: at least one term ID is required (zero-term queries match all entities and are a Phase 4 feature)")
	}
	cp := make([]ID, len(ids))
	copy(cp, ids)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	return &Query{w: w, terms: cp}
}

// Terms returns the query's sorted term list. Callers must not mutate the
// returned slice.
func (q *Query) Terms() []ID { return q.terms }

// Iter starts a fresh iteration over all archetype tables matching the query.
//
// Matching strategy: pick the term whose component-index entry has the fewest
// tables as the seed (O(terms) scan via compIndex.Count), then for each seed
// table check that every other term's ID is present via Table.HasComponent.
// This is O(smallest-set × terms) — optimal for sparse queries.
//
// The seed table list is materialised once via TablesFor (one allocation).
func (q *Query) Iter() *QueryIter {
	// Select seed: term with the fewest tables in the component index.
	seedIdx := 0
	minCount := q.w.compIndex.Count(q.terms[0])
	for i := 1; i < len(q.terms); i++ {
		if c := q.w.compIndex.Count(q.terms[i]); c < minCount {
			minCount = c
			seedIdx = i
		}
	}
	seedID := q.terms[seedIdx]
	candidates := q.w.TablesFor(seedID) // snapshot; one allocation
	return &QueryIter{
		q:          q,
		candidates: candidates,
		pos:        -1,
	}
}

// Each iterates all matching tables and calls fn once per table. The
// *QueryIter passed to fn is already positioned on a matching table; callers
// must NOT call Next inside fn.
func (q *Query) Each(fn func(*QueryIter)) {
	it := q.Iter()
	for it.Next() {
		fn(it)
	}
}

// QueryIter is a pull-style iterator over the tables matching a Query or
// CachedQuery. Obtain via Query.Iter or CachedQuery.Iter; the zero value is
// unusable.
//
// Mutation warning: calling Set, Remove, or Delete on the World while this
// iterator is active produces undefined behaviour.
type QueryIter struct {
	q          *Query
	candidates []*table.Table // seed-table snapshot (uncached) or cache reference (cached)
	pos        int            // index into candidates; -1 = before first Next
	current    *table.Table   // non-nil only when positioned on a matching table
	// cached, when true, skips the per-candidate HasComponent check in Next:
	// the candidate list is pre-filtered by CachedQuery. When false (the
	// default), every candidate is checked against q.terms.
	cached bool
}

// Next advances to the next matching table. Returns true when positioned on a
// valid table; returns false when iteration is exhausted.
func (it *QueryIter) Next() bool {
	for {
		it.pos++
		if it.pos >= len(it.candidates) {
			it.current = nil
			return false
		}
		t := it.candidates[it.pos]
		if it.cached {
			// Cache is pre-filtered by CachedQuery.tryMatchTable; every
			// candidate is already a match.
			it.current = t
			return true
		}
		match := true
		for _, id := range it.q.terms {
			if !t.HasComponent(id) {
				match = false
				break
			}
		}
		if match {
			it.current = t
			return true
		}
	}
}

// Table returns the current matching table. Panics if called before the first
// Next or after Next returned false.
func (it *QueryIter) Table() *table.Table {
	if it.current == nil {
		panic("flecs: QueryIter.Table: not positioned on a valid table (call Next first)")
	}
	return it.current
}

// Count returns the number of entities in the current table.
// Panics if not positioned on a valid table.
func (it *QueryIter) Count() int { return it.Table().Count() }

// Entities returns a read-only slice of entity IDs in the current table.
// The slice is invalidated by the next Next call; callers must consume or
// copy within the current iteration step.
func (it *QueryIter) Entities() []ID { return it.Table().Entities() }

// Query returns the Query that produced this iterator.
func (it *QueryIter) Query() *Query { return it.q }

// Field returns a typed []T slice over the column for id in the current table.
//
// The slice is a live view into the column backing store; mutations are
// immediately visible to subsequent Get calls on the same entity. The slice is
// invalidated by the next it.Next() call — each table has its own column. Do
// not retain the slice across Next calls.
//
// For tag components (Size==0), returns a []T of length it.Count() containing
// zero-value entries. Tag columns carry no data, so the slice elements are
// degenerate; ranging over it is valid but the elements are always zero.
//
// Panics if:
//   - it is not positioned on a valid table (Next has not been called or
//     returned false).
//   - id is not in the current table's signature.
//   - T does not match the Go type registered for id.
//
// Implementation: uses reflect.Value.Interface().([]T) — one interface
// allocation per Field call. The zero-alloc alternative (unsafe.Slice over
// the column base pointer) would require exposing column memory addresses;
// deferred to a later optimisation phase once profiling confirms the cost.
func Field[T any](it *QueryIter, id ID) []T {
	tbl := it.Table() // panics if not positioned
	if !tbl.HasComponent(id) {
		panic(fmt.Sprintf("flecs: Field[%s]: component id %d is not in the current table's signature",
			reflect.TypeFor[T](), id))
	}
	rv := tbl.ColumnReflectSlice(id)
	if !rv.IsValid() {
		// Tag component: return a zero-value slice of the right length so
		// callers can range over it safely.
		return make([]T, it.Count())
	}
	want := reflect.TypeFor[T]()
	got := rv.Type().Elem()
	if got != want {
		panic(fmt.Sprintf("flecs: Field[%s]: column for id %d holds %s, not %s",
			want, id, got, want))
	}
	s := rv.Interface().([]T)
	return s[:it.Count()]
}
