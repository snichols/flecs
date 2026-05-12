package flecs

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/snichols/flecs/internal/storage/table"
)

// Wildcard returns the ID of the built-in Wildcard query-term sentinel (*).
//
// Use Wildcard in the target or relationship position of a pair term to match
// every concrete value that exists in the current table:
//
//	// One iterator row per concrete (Likes, X) pair per table.
//	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(likesID, w.Wildcard())))
//
//	// One iterator row per concrete (X, bob) pair per table.
//	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(w.Wildcard(), bobID)))
//
// Use [MatchedTarget] and [MatchedID] inside the iterator callback to recover
// the concrete pair that matched on the current row. Use [FieldByMatch] when
// the matched pair carries component data.
//
// Wildcard and Any are query-term sentinels only — do not add them to entity
// records or use them as component trait arguments.
func (w *World) Wildcard() ID { return w.wildcardID }

// Any returns the ID of the built-in Any query-term sentinel (_).
//
// Like Wildcard, Any matches entities that have at least one pair with the
// given relationship. Unlike Wildcard, it emits exactly one iterator row per
// entity regardless of how many concrete targets exist — it short-circuits
// after the first match (mirrors C EcsQueryAndAny semantics).
//
//	// One row per entity; no per-target expansion.
//	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(likesID, w.Any())))
func (w *World) Any() ID { return w.anyID }

// findWildcardTermIdx returns the index of the first TermAnd wildcard/any term
// in terms, or -1 if none. Used by both Query.Iter and CachedQuery.Iter to
// initialise QueryIter.wildcardTermIdx.
func findWildcardTermIdx(w *World, terms []Term) int {
	for i, term := range terms {
		if term.Kind == TermAnd && isWildcardTerm(w, term.ID) {
			return i
		}
	}
	return -1
}

// isWildcardID reports whether id (as returned by ID.First() or ID.Second())
// equals the Wildcard or Any sentinel for world w. Pair decomposition strips the
// generation field, so comparison uses the raw 28/32-bit index value.
func isWildcardID(w *World, id ID) bool {
	wi := ID(w.wildcardID.Index())
	ai := ID(w.anyID.Index())
	return id == wi || id == ai
}

// isWildcardTerm reports whether termID is a wildcard pair term — a pair whose
// first or second slot is the Wildcard or Any sentinel.
func isWildcardTerm(w *World, termID ID) bool {
	if !termID.IsPair() {
		return false
	}
	return isWildcardID(w, termID.First()) || isWildcardID(w, termID.Second())
}

// wildcardMatchingPairs returns all concrete pair IDs in t's type that satisfy
// a wildcard pair term. For Any terms the result is at most one entry.
//
// Matching rules:
//   - (R, Wildcard)  → all (R, X) pairs in the table.
//   - (Wildcard, T)  → all (X, T) pairs in the table.
//   - (Wildcard, Wildcard) → all pairs in the table.
//   - (R, Any)       → the first (R, X) pair found.
func wildcardMatchingPairs(w *World, t *table.Table, termID ID) []ID {
	fst := termID.First()  // relationship slot (or wildcard/any sentinel)
	snd := termID.Second() // target slot (or wildcard/any sentinel)
	wi := ID(w.wildcardID.Index())
	ai := ID(w.anyID.Index())
	relIsWild := fst == wi
	tgtIsWild := snd == wi
	tgtIsAny := snd == ai
	relIsAny := fst == ai
	stopAfterOne := tgtIsAny || relIsAny

	var result []ID
	for _, sigID := range t.Type() {
		if !sigID.IsPair() {
			continue
		}
		sigFst := sigID.First()
		sigSnd := sigID.Second()
		fstMatch := relIsWild || relIsAny || sigFst == fst
		sndMatch := tgtIsWild || tgtIsAny || sigSnd == snd
		if fstMatch && sndMatch {
			result = append(result, sigID)
			if stopAfterOne {
				break
			}
		}
	}
	return result
}

// tableHasWildcardMatch reports whether t's type contains at least one pair
// that satisfies the wildcard term termID. Used by matchesTable and tryMatchTable
// to gate table admission before collecting the full expansion list.
func tableHasWildcardMatch(w *World, t *table.Table, termID ID) bool {
	fst := termID.First()
	snd := termID.Second()
	wi := ID(w.wildcardID.Index())
	ai := ID(w.anyID.Index())
	relIsWild := fst == wi || fst == ai
	tgtIsWild := snd == wi || snd == ai

	for _, sigID := range t.Type() {
		if !sigID.IsPair() {
			continue
		}
		fstMatch := relIsWild || sigID.First() == fst
		sndMatch := tgtIsWild || sigID.Second() == snd
		if fstMatch && sndMatch {
			return true
		}
	}
	return false
}

// MatchedTarget returns the concrete target entity for the wildcard term at
// index termIdx on the current iterator expansion row.
//
// The termIdx must identify the first wildcard term in the query (the index into
// the term list as passed to [NewQueryFromTerms] or [NewCachedQueryFromTerms]).
//
// Panics if termIdx does not match the active wildcard term index, or if the
// iterator is not positioned on a valid expansion row.
func MatchedTarget(it *QueryIter, termIdx int) ID {
	if it.wildcardTermIdx != termIdx {
		panic(fmt.Sprintf("flecs: MatchedTarget: term %d is not the active wildcard term (active index: %d)",
			termIdx, it.wildcardTermIdx))
	}
	if it.wildcardPairPos < 0 || it.wildcardPairPos >= len(it.wildcardPairs) {
		panic("flecs: MatchedTarget: iterator not positioned on a valid wildcard expansion row")
	}
	return it.wildcardPairs[it.wildcardPairPos].Second()
}

// MatchedID returns the full concrete pair ID (rel, target) matched by the
// wildcard term at index termIdx on the current iterator expansion row.
//
// Panics if termIdx does not match the active wildcard term index, or if the
// iterator is not positioned on a valid expansion row.
func MatchedID(it *QueryIter, termIdx int) ID {
	if it.wildcardTermIdx != termIdx {
		panic(fmt.Sprintf("flecs: MatchedID: term %d is not the active wildcard term (active index: %d)",
			termIdx, it.wildcardTermIdx))
	}
	if it.wildcardPairPos < 0 || it.wildcardPairPos >= len(it.wildcardPairs) {
		panic("flecs: MatchedID: iterator not positioned on a valid wildcard expansion row")
	}
	return it.wildcardPairs[it.wildcardPairPos]
}

// FieldByMatch returns a typed []T slice for the component data stored under the
// concrete pair matched by the wildcard term at index termIdx on the current row.
//
// Use when the matched pair carries typed data (stored via [SetPair][T]). For
// tag pairs (no data), returns a zero-value slice of length [QueryIter.Count].
//
// The returned slice is a live view into the column backing store and is
// invalidated by the next [QueryIter.Next] call — same aliasing rules as [Field].
//
// Panics if termIdx does not match the active wildcard term, if T does not
// match the registered type for the pair, or if the iterator is not positioned.
func FieldByMatch[T any](it *QueryIter, termIdx int) []T {
	if it.wildcardTermIdx != termIdx {
		panic(fmt.Sprintf("flecs: FieldByMatch: term %d is not the active wildcard term (active index: %d)",
			termIdx, it.wildcardTermIdx))
	}
	if it.wildcardPairPos < 0 || it.wildcardPairPos >= len(it.wildcardPairs) {
		panic("flecs: FieldByMatch: iterator not positioned on a valid wildcard expansion row")
	}
	matchedPair := it.wildcardPairs[it.wildcardPairPos]
	tbl := it.Table()
	base, typ, n := tbl.ColumnBasePtr(matchedPair)
	if typ == nil {
		return make([]T, it.Count())
	}
	want := reflect.TypeFor[T]()
	if typ != want {
		panic(fmt.Sprintf("flecs: FieldByMatch[%s]: column for pair %v holds %s, not %s",
			want, matchedPair, typ, want))
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
