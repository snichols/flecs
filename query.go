package flecs

import (
	"fmt"
	"reflect"
	"sort"
	"unsafe"

	"github.com/snichols/flecs/internal/storage/table"
)

// TermKind enumerates how a query term participates in matching.
//
//   - TermAnd: the table's signature must include the term's ID.
//   - TermNot: the table's signature must NOT include the term's ID.
//   - TermOptional: matching is unaffected; the table may or may not include the ID.
//   - TermOr: adjacent Or terms form an OR-group; the matched table must contain
//     at least one ID from the group. Use FieldMaybe to read Or-group columns.
//
// Only TermAnd terms are eligible to seed iteration. Not, Optional, and Or
// terms cannot seed because they have no finite candidate set of their own.
type TermKind int

const (
	// TermAnd requires the term's ID to be present in the matched table.
	TermAnd TermKind = 0
	// TermNot requires the term's ID to be absent from the matched table.
	TermNot TermKind = 1
	// TermOptional does not affect table matching. Use FieldMaybe to read
	// the column when present; Field panics if the column is absent.
	TermOptional TermKind = 2
	// TermOr contributes to an OR-group. Adjacent Or terms form a single group;
	// the group is broken by any non-Or term. A table matches the OR-group if it
	// contains at least one of the group's IDs. An OR-group of size 1 is
	// degenerate but allowed (behaves identically to TermAnd for matching).
	// Use FieldMaybe — not Field — to access Or-group columns.
	TermOr TermKind = 3
)

// String returns a human-readable name for the TermKind.
func (k TermKind) String() string {
	switch k {
	case TermAnd:
		return "And"
	case TermNot:
		return "Not"
	case TermOptional:
		return "Optional"
	case TermOr:
		return "Or"
	default:
		return fmt.Sprintf("TermKind(%d)", int(k))
	}
}

// Term is a structured query term combining a component/pair/tag ID with a TermKind.
type Term struct {
	ID   ID
	Kind TermKind
}

// With returns a TermAnd term: matched tables must contain id.
func With(id ID) Term { return Term{id, TermAnd} }

// Without returns a TermNot term: matched tables must NOT contain id.
func Without(id ID) Term { return Term{id, TermNot} }

// Maybe returns a TermOptional term: matching is unaffected; id may or may not
// be present in matched tables. Use FieldMaybe to access the column when present.
func Maybe(id ID) Term { return Term{id, TermOptional} }

// Or returns a TermOr term that contributes to an OR-group. Adjacent Or terms
// in the query's term slice form a single group (broken by any non-Or term); a
// table matches the group if it contains at least one of the group's IDs.
// An OR-group of size 1 is degenerate but allowed (behaves like With).
// Use FieldMaybe — not Field — to access Or-group columns per entity.
func Or(id ID) Term { return Term{id, TermOr} }

// Query holds a structured list of query terms used to match archetype tables.
// Terms are stored sorted: TermAnd first (by ID), then TermNot (by ID), then
// TermOr-groups (preserving group adjacency, within-group sorted by ID), then
// TermOptional (by ID).
//
// Mutation warning: calling Set, Remove, or Delete on the World from inside
// Each or QueryIter.Next produces undefined behaviour; the iterator holds a
// snapshot of the seed table list taken at Iter() time and does not handle
// structural changes.
type Query struct {
	w        *World
	terms    []Term // sorted: And first (by ID), Not second, Or-groups third, Optional last
	andIDs   []ID   // pre-extracted And-term IDs; returned by Terms() for backward compat
	orGroups [][]ID // each inner slice is one OR-group; tables must match all groups
}

// NewQuery constructs a query over w for the given component IDs (all TermAnd).
//
// Panics if w is nil or no IDs are provided. Zero-term queries (match all
// entities) are not supported.
//
// Duplicate IDs in the term list are allowed but wasteful; the match still
// works correctly because HasComponent is idempotent.
//
// The provided ids are copied and sorted; the caller's slice is not retained.
func NewQuery(w *World, ids ...ID) *Query {
	if w != nil {
		w.checkExclusiveAccessWrite()
	}
	if w == nil {
		panic("flecs: NewQuery: world must not be nil")
	}
	if len(ids) == 0 {
		panic("flecs: NewQuery: at least one term ID is required (zero-term queries match all entities and are not supported)")
	}
	cp := make([]ID, len(ids))
	copy(cp, ids)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	terms := make([]Term, len(cp))
	for i, id := range cp {
		terms[i] = Term{ID: id, Kind: TermAnd}
	}
	return &Query{w: w, terms: terms, andIDs: cp}
}

// NewQueryFromTerms constructs a query over w for the given structured terms.
//
// Terms can be [With] (TermAnd), [Without] (TermNot), [Maybe] (TermOptional),
// or [Or] (TermOr). Adjacent Or terms form an OR-group; a table matches the
// group if it contains at least one of the group's IDs. Example:
//
//	// Match entities with Position AND (Sleeping OR Working OR Playing).
//	q := flecs.NewQueryFromTerms(w,
//	    flecs.With(posID),
//	    flecs.Or(sleepID),
//	    flecs.Or(workID),
//	    flecs.Or(playID),
//	)
//
// Panics if:
//   - w is nil.
//   - no terms are provided.
//   - no TermAnd term is present (queries with only Not/Optional/Or terms would
//     match an unbounded entity set and are not supported).
//   - any two terms share the same ID across any term kinds.
//   - an Or term's ID is zero/invalid.
//   - two Or terms within the same group share the same ID.
//
// Terms are copied and sorted internally: TermAnd first (by ID), TermNot
// second (by ID), TermOr-groups third (preserving group adjacency), TermOptional
// last (by ID). The caller's slice is not retained.
func NewQueryFromTerms(w *World, terms ...Term) *Query {
	if w != nil {
		w.checkExclusiveAccessWrite()
	}
	if w == nil {
		panic("flecs: NewQueryFromTerms: world must not be nil")
	}
	cp, andIDs, orGroups := validateAndSortTerms("flecs: NewQueryFromTerms", terms)
	return &Query{w: w, terms: cp, andIDs: andIDs, orGroups: orGroups}
}

// Terms returns the sorted TermAnd-only ID list for backward compatibility.
//
// For callers that predate structured terms, this returns only the TermAnd
// component IDs, matching the historic NewQuery semantics. To get the full
// term list including TermNot and TermOptional, use TermsFull.
//
// Callers must not mutate the returned slice.
func (q *Query) Terms() []ID { return q.andIDs }

// TermsFull returns a copy of the full structured term list in sorted order
// (TermAnd first, TermNot second, TermOr-groups third, TermOptional last).
func (q *Query) TermsFull() []Term {
	cp := make([]Term, len(q.terms))
	copy(cp, q.terms)
	return cp
}

// Iter starts a fresh iteration over all archetype tables matching the query.
//
// Seed strategy: pick the TermAnd term whose component-index entry has the
// fewest tables (O(And-terms) scan), then for each seed table verify that
// every other TermAnd is present and every TermNot is absent. TermOptional
// terms do not affect matching. This is O(smallest-set × terms) — optimal for
// sparse queries.
//
// The seed table list is materialised once via TablesFor (one allocation).
func (q *Query) Iter() *QueryIter {
	q.w.checkExclusiveAccessRead()
	// Select seed: TermAnd term with the fewest tables in the component index.
	seedIdx := -1
	minCount := 0
	for i, term := range q.terms {
		if term.Kind != TermAnd {
			continue
		}
		c := q.w.compIndex.Count(term.ID)
		if seedIdx == -1 || c < minCount {
			minCount = c
			seedIdx = i
		}
	}
	// seedIdx is always valid: NewQuery/NewQueryFromTerms guarantee >= 1 TermAnd.
	seedID := q.terms[seedIdx].ID
	candidates := q.w.TablesFor(seedID)
	return &QueryIter{
		q:          q,
		terms:      q.terms,
		orGroups:   q.orGroups,
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
	q          *Query         // nil for CachedQuery-derived iters; use terms for term access
	terms      []Term         // full term list (And + Not + Or + Optional), set by Iter constructors
	orGroups   [][]ID         // OR-groups mirrored from Query/CachedQuery for matchesTable
	candidates []*table.Table // seed-table snapshot (uncached) or cache reference (cached)
	pos        int            // index into candidates; -1 = before first Next
	current    *table.Table   // non-nil only when positioned on a matching table
	// cached, when true, skips the per-candidate term check in Next: the
	// candidate list is pre-filtered by CachedQuery. When false (the default),
	// every candidate is evaluated against the term list.
	cached          bool
	optionalPresent map[ID]bool // Optional- and Or-term presence for the current table
	// multi-threaded dispatch clipping (zero workerTotal = no clipping, full table)
	workerIdx   int // 0-based index of this worker
	workerTotal int // total worker count; 0 = no clipping
	wFirst      int // first row for this worker in the current table
	wCount      int // row count for this worker in the current table
}

// Next advances to the next matching table. Returns true when positioned on a
// valid table; returns false when iteration is exhausted.
//
// For multi-threaded iterators (workerTotal > 0), tables where this worker's
// row count is zero are skipped transparently.
func (it *QueryIter) Next() bool {
	for {
		it.pos++
		if it.pos >= len(it.candidates) {
			it.current = nil
			return false
		}
		t := it.candidates[it.pos]
		// Cached iters skip the per-table term check; uncached iters filter.
		if !it.cached && !it.matchesTable(t) {
			continue
		}
		// Compute this worker's row slice when clipping is active.
		if it.workerTotal > 0 {
			n := t.Count()
			q, r := n/it.workerTotal, n%it.workerTotal
			it.wFirst = q*it.workerIdx + min(it.workerIdx, r)
			it.wCount = q
			if it.workerIdx < r {
				it.wCount++
			}
			if it.wCount == 0 {
				continue // this worker has no rows in this table
			}
		}
		it.current = t
		it.updateOptionalPresence(t)
		return true
	}
}

// matchesTable returns true if t satisfies all TermAnd, TermNot, and OR-group
// terms. TermOptional terms are ignored during matching.
func (it *QueryIter) matchesTable(t *table.Table) bool {
	for _, term := range it.terms {
		switch term.Kind {
		case TermAnd:
			if !t.HasComponent(term.ID) {
				return false
			}
		case TermNot:
			if t.HasComponent(term.ID) {
				return false
			}
		}
	}
	for _, group := range it.orGroups {
		matched := false
		for _, id := range group {
			if t.HasComponent(id) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// updateOptionalPresence records which TermOptional and TermOr IDs are present
// in t. Called once per table transition inside Next. Skipped when there are no
// Optional or Or terms (common case: zero allocs).
func (it *QueryIter) updateOptionalPresence(t *table.Table) {
	for k := range it.optionalPresent {
		delete(it.optionalPresent, k)
	}
	for _, term := range it.terms {
		if term.Kind == TermOptional || term.Kind == TermOr {
			if it.optionalPresent == nil {
				it.optionalPresent = make(map[ID]bool)
			}
			it.optionalPresent[term.ID] = t.HasComponent(term.ID)
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

// Count returns the number of entities visible to this iterator in the current
// table. For multi-threaded iterators this is the worker's row count (a
// subset of the full table); for ordinary iterators it is the full table count.
// Panics if not positioned on a valid table.
func (it *QueryIter) Count() int {
	if it.workerTotal > 0 {
		return it.wCount
	}
	return it.Table().Count()
}

// Entities returns the entity IDs for this iterator's rows in the current table.
// For multi-threaded iterators this is the worker's disjoint row slice; for
// ordinary iterators it is the full table entity list. The slice is invalidated
// by the next Next call; callers must consume or copy within the current step.
func (it *QueryIter) Entities() []ID {
	all := it.Table().Entities()
	if it.workerTotal > 0 {
		return all[it.wFirst : it.wFirst+it.wCount]
	}
	return all
}

// Query returns the Query that produced this iterator. Returns nil for iters
// derived from a CachedQuery.
func (it *QueryIter) Query() *Query { return it.q }

// clippedCopy returns a shallow copy of it restricted to worker workerIdx of
// workerTotal. Each copy independently iterates the same table list but sees
// only its disjoint row slice per table. Used by the multi-threaded dispatcher.
func (it *QueryIter) clippedCopy(workerIdx, workerTotal int) *QueryIter {
	cp := *it
	cp.workerIdx = workerIdx
	cp.workerTotal = workerTotal
	return &cp
}

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
// For TermOptional terms, use FieldMaybe instead: Field panics if the column
// is absent in the current table (even when the term is Optional).
//
// Implementation: uses unsafe.Slice over the column's base pointer — zero
// allocs per call. GC safety: the column's reflect-backed slice (reachable
// through it.current → Table → Column) keeps the backing array alive; the
// returned []T header's data pointer is an unsafe.Pointer to T, which the GC
// traces correctly for pointer-containing element types.
func Field[T any](it *QueryIter, id ID) []T {
	tbl := it.Table() // panics if not positioned
	if !tbl.HasComponent(id) {
		panic(fmt.Sprintf("flecs: Field[%s]: component id %d is not in the current table's signature",
			reflect.TypeFor[T](), id))
	}
	base, typ, n := tbl.ColumnBasePtr(id)
	if typ == nil {
		// Tag column: return zero-value slice of the right length so callers
		// can range over it safely.
		return make([]T, it.Count())
	}
	want := reflect.TypeFor[T]()
	if typ != want {
		panic(fmt.Sprintf("flecs: Field[%s]: column for id %d holds %s, not %s",
			want, id, typ, want))
	}
	if n == 0 {
		return nil
	}
	full := unsafe.Slice((*T)(base), n)
	if it.workerTotal > 0 {
		return full[it.wFirst : it.wFirst+it.wCount]
	}
	return full[:it.Count()]
}

// FieldMaybe returns a typed []T slice and a presence flag for a TermOptional
// or TermOr query term id.
//
// Panics if id is not a TermOptional or TermOr term in this iter's query.
// For TermAnd terms, use Field instead. For TermOr terms, Field also panics if
// the current table does not contain id — always prefer FieldMaybe for Or-group
// IDs to safely disambiguate which members are present in the current table.
//
// Returns (nil, false) if the current table does not contain id.
// Returns (slice, true) if the current table contains id — identical semantics
// to Field for the present case.
//
// FieldMaybe must be called after a successful Next call.
func FieldMaybe[T any](it *QueryIter, id ID) ([]T, bool) {
	for _, term := range it.terms {
		if term.ID != id {
			continue
		}
		if term.Kind != TermOptional && term.Kind != TermOr {
			panic(fmt.Sprintf("flecs: FieldMaybe[%s]: id %d is not a TermOptional or TermOr term; use Field for TermAnd terms",
				reflect.TypeFor[T](), id))
		}
		if !it.optionalPresent[id] {
			return nil, false
		}
		tbl := it.Table()
		base, typ, n := tbl.ColumnBasePtr(id)
		if typ == nil {
			return make([]T, it.Count()), true
		}
		want := reflect.TypeFor[T]()
		if typ != want {
			panic(fmt.Sprintf("flecs: FieldMaybe[%s]: column for id %d holds %s, not %s",
				want, id, typ, want))
		}
		if n == 0 {
			return nil, true
		}
		full := unsafe.Slice((*T)(base), n)
		if it.workerTotal > 0 {
			return full[it.wFirst : it.wFirst+it.wCount], true
		}
		return full[:it.Count()], true
	}
	panic(fmt.Sprintf("flecs: FieldMaybe[%s]: id %d is not in this query's term list",
		reflect.TypeFor[T](), id))
}

// validateAndSortTerms validates terms for NewQueryFromTerms/NewCachedQueryFromTerms,
// builds OR-groups by scanning consecutive TermOr entries, copies and sorts terms
// (And first by ID, Not second by ID, Or-groups third preserving adjacency, Optional
// last by ID), and returns the sorted terms, pre-extracted And-term IDs, and OR-groups.
// Panics with messages prefixed by caller on invalid input.
func validateAndSortTerms(caller string, terms []Term) ([]Term, []ID, [][]ID) {
	if len(terms) == 0 {
		panic(caller + ": at least one term is required")
	}
	hasAnd := false
	for _, t := range terms {
		if t.Kind == TermAnd {
			hasAnd = true
			break
		}
	}
	if !hasAnd {
		panic(caller + ": at least one TermAnd term is required; a query with only Not/Optional/Or terms would match an unbounded entity set")
	}

	// Build OR-groups by scanning for consecutive TermOr sequences.
	// Simultaneously validate zero IDs on Or terms.
	var orGroups [][]ID
	termGroup := make([]int, len(terms)) // group index for TermOr terms; -1 for non-Or
	for i := range termGroup {
		termGroup[i] = -1
	}
	inGroup := false
	for i, t := range terms {
		if t.Kind == TermOr {
			if t.ID.Index() == 0 {
				panic(fmt.Sprintf("%s: Or term at index %d has zero/invalid ID", caller, i))
			}
			if !inGroup {
				orGroups = append(orGroups, nil)
				inGroup = true
			}
			g := len(orGroups) - 1
			termGroup[i] = g
			orGroups[g] = append(orGroups[g], t.ID)
		} else {
			inGroup = false
		}
	}

	// Check for duplicate IDs within each OR-group.
	for _, g := range orGroups {
		seen := make(map[ID]struct{}, len(g))
		for _, id := range g {
			if _, dup := seen[id]; dup {
				panic(fmt.Sprintf("%s: duplicate id in OR-group: id %d appears more than once", caller, id))
			}
			seen[id] = struct{}{}
		}
	}

	// Check for cross-kind duplicate IDs.
	seen := make(map[ID]struct{}, len(terms))
	for _, t := range terms {
		if _, dup := seen[t.ID]; dup {
			panic(fmt.Sprintf("%s: duplicate term ID %d; each ID may appear at most once across all term kinds", caller, t.ID))
		}
		seen[t.ID] = struct{}{}
	}

	// Build sorted term list: And (by ID), Not (by ID), Or-groups (group order,
	// within-group by ID), Optional (by ID).
	var andTerms, notTerms, optTerms []Term
	for _, t := range terms {
		switch t.Kind {
		case TermAnd:
			andTerms = append(andTerms, t)
		case TermNot:
			notTerms = append(notTerms, t)
		case TermOptional:
			optTerms = append(optTerms, t)
		}
	}
	sort.Slice(andTerms, func(i, j int) bool { return andTerms[i].ID < andTerms[j].ID })
	sort.Slice(notTerms, func(i, j int) bool { return notTerms[i].ID < notTerms[j].ID })
	sort.Slice(optTerms, func(i, j int) bool { return optTerms[i].ID < optTerms[j].ID })

	// Build Or section: groups in original order; within each group sorted by ID.
	var orTerms []Term
	for _, g := range orGroups {
		ids := make([]ID, len(g))
		copy(ids, g)
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			orTerms = append(orTerms, Term{ID: id, Kind: TermOr})
		}
	}

	cp := make([]Term, 0, len(terms))
	cp = append(cp, andTerms...)
	cp = append(cp, notTerms...)
	cp = append(cp, orTerms...)
	cp = append(cp, optTerms...)

	// Extract And-term IDs (already sorted).
	andIDs := make([]ID, len(andTerms))
	for i, t := range andTerms {
		andIDs[i] = t.ID
	}
	if len(andIDs) == 0 {
		andIDs = nil
	}

	return cp, andIDs, orGroups
}
