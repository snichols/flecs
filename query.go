package flecs

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unsafe"

	"github.com/snichols/flecs/internal/storage/table"
)

// TraverseFlags controls how a query term resolves the component through relationships.
//
// The default (TraverseSelf) matches only entities whose own archetype table contains
// the term's component. TraverseUp, TraverseSelfUp, and TraverseCascade add ancestor
// traversal at query-match time.
//
// Limitation (all traversal modes): if an ancestor gains or loses the component AFTER
// a CachedQuery is built, the cache is not automatically updated. Rebuild the query to
// reflect structural changes to the ancestry chain.
type TraverseFlags int

const (
	// TraverseSelf matches only entities whose own archetype table contains the
	// component. This is the default for all terms constructed with With/Without/Maybe/Or.
	TraverseSelf TraverseFlags = 0

	// TraverseUp matches entities whose nearest ancestor via Trav owns the component.
	// The entity's own table need not contain the component.
	// Use FieldShared[T] to read the inherited value; IsFieldSelf returns false.
	TraverseUp TraverseFlags = 1

	// TraverseSelfUp matches entities that own the component locally (Self path) OR
	// inherit it via Trav (Up path). Self takes precedence when both apply.
	// Use IsFieldSelf to determine which path was taken for the current table.
	TraverseSelfUp TraverseFlags = 2

	// TraverseCascade matches like TraverseSelfUp and additionally guarantees that
	// matched tables are iterated in ascending depth order in the Trav hierarchy
	// (root-first). Only valid in NewCachedQueryFromTerms; NewQueryFromTerms panics.
	TraverseCascade TraverseFlags = 3

	// TraverseExplicitSelf is an internal sentinel returned by Term.Self().
	// At query-match time it behaves identically to TraverseSelf (local-only
	// match). Its purpose is to let the auto-promotion logic in the validator
	// distinguish "user explicitly asked for Self" from "default zero value".
	// When a term carries TraverseExplicitSelf, the validator does NOT promote it
	// to SelfUp even when the underlying component is inheritable.
	TraverseExplicitSelf TraverseFlags = 4
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
	// TermScope wraps a negated sub-expression of arbitrary terms. Construct via
	// [WithoutScope]; the Sub field holds the inner terms. A TermScope term has
	// ID = 0; the sub-expression is evaluated per entity and the result is negated.
	// Within the sub-expression, a TermAnd immediately followed by one or more
	// TermOr terms forms an OR-group (C flecs convention).
	TermScope TermKind = 4
	// TermEq matches only when the iterated entity equals the target entity (ID).
	// Mirrors upstream EcsPredEq / EcsQueryPredEq. Construct via [IsEntity].
	// Cannot be combined with traversal flags or a fixed source.
	TermEq TermKind = 5
	// TermNotEq matches every entity except the target entity (ID).
	// Mirrors upstream EcsPredEq+EcsNot / EcsQueryPredNeq. Construct via [NotEntity].
	// Cannot be combined with traversal flags or a fixed source.
	TermNotEq TermKind = 6
	// TermNameMatch matches entities whose Name.Value contains the Pattern string
	// (case-insensitive substring). Unnamed entities never match. Empty Pattern
	// matches every named entity. Mirrors upstream EcsPredMatch / EcsQueryPredEqMatch.
	// Construct via [NameMatches]. Cannot be combined with traversal flags or a fixed source.
	TermNameMatch TermKind = 7
	// TermAndFrom expands the Src entity's component list into N TermAnd requirements
	// at query construction (snapshot). Entities must have ALL of Src's inheritable
	// components. Components with DontInherit policy (e.g. Prefab) are excluded.
	// Empty source type → vacuous truth, no requirements added. Source must be alive
	// at construction. Construct via [AndFrom].
	TermAndFrom TermKind = 8
	// TermOrFrom expands the Src entity's component list into one OR-group at query
	// construction (snapshot). Entities must have AT LEAST ONE of Src's inheritable
	// components. Empty source type → the query yields zero results (empty disjunction
	// = false). Source must be alive at construction. Construct via [OrFrom].
	TermOrFrom TermKind = 9
	// TermNotFrom expands the Src entity's component list into N TermNot requirements
	// at query construction (snapshot). Entities must have NONE of Src's inheritable
	// components. Empty source type → vacuous truth, no exclusions added. Source must
	// be alive at construction. Construct via [NotFrom].
	TermNotFrom TermKind = 10
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
	case TermScope:
		return "Scope"
	case TermEq:
		return "Eq"
	case TermNotEq:
		return "NotEq"
	case TermNameMatch:
		return "NameMatch"
	case TermAndFrom:
		return "AndFrom"
	case TermOrFrom:
		return "OrFrom"
	case TermNotFrom:
		return "NotFrom"
	default:
		return fmt.Sprintf("TermKind(%d)", int(k))
	}
}

// Term is a structured query term combining a component/pair/tag ID with a TermKind
// and optional traversal modifier.
//
// The zero value is a valid TermAnd/TraverseSelf term with a zero ID (not useful on
// its own; always construct via With/Without/Maybe/Or and their chained methods).
type Term struct {
	ID       ID
	Kind     TermKind
	Trav     ID            // traversal relationship (non-zero for Up/SelfUp/Cascade terms)
	Traverse TraverseFlags // traversal mode; default TraverseSelf (0) = local-only match
	// Src is the fixed source entity for this term. When non-zero, the component is
	// read from Src rather than the iterated entity ($this). The term does NOT
	// contribute to the archetype-filter set — it is resolved once at iter start.
	// Construct via WithSourceTerm or (Term).Source(e). Zero means $this (default).
	Src ID
	// Sparse is set to true by validateAndSortTerms / NewQuery when the term's
	// component stores its data in a sparse-set (IsSparse or IsDontFragment returned
	// true). Sparse is a VALUE-FETCH routing hint: Field[T] reads via sparseSetGet
	// rather than the archetype column. Not set for pair IDs.
	Sparse bool
	// DontFragment is set to true when the term's component has the DontFragment trait,
	// meaning the component does NOT appear in the entity's archetype type. This drives
	// ITERATION MODE SELECTION: DontFragment terms cannot seed archetype-table iteration
	// and are checked per-entity via sparseSetGet in matchesSparseTerms.
	// A Sparse-only term (Sparse=true, DontFragment=false) IS in the archetype and
	// can seed iteration; its value is still fetched from sparse-set via Field[T].
	DontFragment bool
	// Union is set to true when the term's ID is a pair whose relationship has the
	// Union trait. Union pairs never appear in the archetype type; the active target
	// is stored in the per-relationship union store. Union terms cannot seed archetype
	// iteration and are checked per-entity via the union store in matchesSparseTerms.
	Union bool
	// Sub holds the inner terms for a TermScope term. Non-nil only when
	// Kind == TermScope; inner terms have Sparse/DontFragment/Union routing hints
	// set by validateAndSortTerms. The sub-expression is evaluated per entity
	// and the result is negated (the scope passes when Sub evaluates to false).
	Sub []Term
	// Pattern is the match string for a TermNameMatch term. The empty string
	// matches every named entity (mirrors upstream flecs_query_match_substr_i:
	// if the pattern is empty, any name satisfies it). Ignored for other kinds.
	Pattern string
}

// With returns a TermAnd term: matched tables must contain id.
func With(id ID) Term { return Term{ID: id, Kind: TermAnd} }

// Without returns a TermNot term: matched tables must NOT contain id.
func Without(id ID) Term { return Term{ID: id, Kind: TermNot} }

// Maybe returns a TermOptional term: matching is unaffected; id may or may not
// be present in matched tables. Use FieldMaybe to access the column when present.
func Maybe(id ID) Term { return Term{ID: id, Kind: TermOptional} }

// Or returns a TermOr term that contributes to an OR-group. Adjacent Or terms
// in the query's term slice form a single group (broken by any non-Or term); a
// table matches the group if it contains at least one of the group's IDs.
// An OR-group of size 1 is degenerate but allowed (behaves like With).
// Use FieldMaybe — not Field — to access Or-group columns per entity.
func Or(id ID) Term { return Term{ID: id, Kind: TermOr} }

// IsEntity returns a TermEq term: the query matches only entities whose ID equals e.
// Panics if e is zero. Cannot be combined with traversal flags or a fixed source.
// Mirrors upstream EcsPredEq semantics ($this == e).
func IsEntity(e ID) Term {
	if e == 0 {
		panic("flecs: IsEntity: entity ID must not be zero")
	}
	return Term{ID: e, Kind: TermEq}
}

// NotEntity returns a TermNotEq term: the query matches every entity except e.
// Panics if e is zero. Cannot be combined with traversal flags or a fixed source.
// Mirrors upstream EcsPredEq+EcsNot semantics ($this != e).
func NotEntity(e ID) Term {
	if e == 0 {
		panic("flecs: NotEntity: entity ID must not be zero")
	}
	return Term{ID: e, Kind: TermNotEq}
}

// NameMatches returns a TermNameMatch term: matches entities whose Name.Value
// contains pattern (case-insensitive substring; no regex, no glob).
// Empty pattern matches every named entity. Unnamed entities never match.
// Mirrors upstream flecs_query_match_substr_i (eval_pred.c:8-41).
func NameMatches(pattern string) Term { return Term{Kind: TermNameMatch, Pattern: pattern} }

// AndFrom returns a TermAndFrom term: at query construction the source entity's
// component list is read once (snapshot) and expanded into N TermAnd requirements.
// Entities must have ALL of source's inheritable components.
//
// Components with DontInherit policy (e.g. the Prefab tag) are excluded from
// expansion, mirroring upstream EcsIdOnInstantiateDontInherit filtering.
// Empty source type → vacuous truth: no requirements added.
//
// The source must be alive at construction. Changes to source's component list
// after construction are NOT reflected — reconstruct the query to pick them up.
// This deliberately diverges from upstream C which re-reads the type at every
// iteration (eval.c:462); see CHANGELOG v0.77.0.
// Panics if source is zero.
func AndFrom(source ID) Term {
	if source == 0 {
		panic("flecs: AndFrom: source entity must not be zero")
	}
	return Term{Kind: TermAndFrom, Src: source}
}

// OrFrom returns a TermOrFrom term: at query construction the source entity's
// component list is read once (snapshot) and expanded into an OR-group.
// Entities must have AT LEAST ONE of source's inheritable components.
//
// Empty source type → the entire query yields zero results (empty disjunction
// = false; this diverges from upstream C which skips the operator — see CHANGELOG v0.77.0).
// Source must be alive at construction; changes after construction are ignored.
// Panics if source is zero.
func OrFrom(source ID) Term {
	if source == 0 {
		panic("flecs: OrFrom: source entity must not be zero")
	}
	return Term{Kind: TermOrFrom, Src: source}
}

// NotFrom returns a TermNotFrom term: at query construction the source entity's
// component list is read once (snapshot) and expanded into N TermNot requirements.
// Entities must have NONE of source's inheritable components.
//
// Empty source type → vacuous truth: no exclusions added.
// Source must be alive at construction; changes after construction are ignored.
// Panics if source is zero.
func NotFrom(source ID) Term {
	if source == 0 {
		panic("flecs: NotFrom: source entity must not be zero")
	}
	return Term{Kind: TermNotFrom, Src: source}
}

// ScopeBuilder accumulates the inner term list for a [WithoutScope] expression.
// Obtain a *ScopeBuilder via the buildFn argument of [WithoutScope]; do not
// construct directly. Methods return the receiver for chaining.
type ScopeBuilder struct {
	terms []Term
}

// With adds a TermAnd requirement to the scope sub-expression.
func (b *ScopeBuilder) With(id ID) *ScopeBuilder {
	b.terms = append(b.terms, Term{ID: id, Kind: TermAnd})
	return b
}

// Without adds a TermNot requirement to the scope sub-expression.
func (b *ScopeBuilder) Without(id ID) *ScopeBuilder {
	b.terms = append(b.terms, Term{ID: id, Kind: TermNot})
	return b
}

// Or adds a TermOr term to the scope sub-expression. When a [ScopeBuilder.With]
// call is immediately followed by one or more Or calls they form an OR-group
// (C flecs convention): the sub-expression is satisfied if any member is present.
func (b *ScopeBuilder) Or(id ID) *ScopeBuilder {
	b.terms = append(b.terms, Term{ID: id, Kind: TermOr})
	return b
}

// Maybe adds a TermOptional term to the scope sub-expression; optional terms
// do not affect whether the scope matches.
func (b *ScopeBuilder) Maybe(id ID) *ScopeBuilder {
	b.terms = append(b.terms, Term{ID: id, Kind: TermOptional})
	return b
}

// IsEntity adds a TermEq term to the scope sub-expression. Panics if e is zero.
func (b *ScopeBuilder) IsEntity(e ID) *ScopeBuilder {
	b.terms = append(b.terms, IsEntity(e))
	return b
}

// NotEntity adds a TermNotEq term to the scope sub-expression. Panics if e is zero.
func (b *ScopeBuilder) NotEntity(e ID) *ScopeBuilder {
	b.terms = append(b.terms, NotEntity(e))
	return b
}

// NameMatches adds a TermNameMatch term to the scope sub-expression.
func (b *ScopeBuilder) NameMatches(pattern string) *ScopeBuilder {
	b.terms = append(b.terms, NameMatches(pattern))
	return b
}

// Source sets a fixed source on the most recently added term. The component is
// read from src rather than the iterated entity. Panics if no term has been
// added yet or if src is zero.
func (b *ScopeBuilder) Source(src ID) *ScopeBuilder {
	if len(b.terms) == 0 {
		panic("flecs: ScopeBuilder.Source: no term to apply source to")
	}
	if src == 0 {
		panic("flecs: ScopeBuilder.Source: source entity must not be zero")
	}
	b.terms[len(b.terms)-1].Src = src
	return b
}

// WithoutScope adds a nested negated scope to the sub-expression. Panics if
// buildFn produces zero inner terms.
func (b *ScopeBuilder) WithoutScope(buildFn func(*ScopeBuilder)) *ScopeBuilder {
	b.terms = append(b.terms, WithoutScope(buildFn))
	return b
}

// WithoutScope returns a TermScope term that negates a sub-expression of
// arbitrary terms. The buildFn receives a [*ScopeBuilder]; call its
// With/Without/Or/Maybe/WithoutScope methods to populate the inner term list.
// The finished Term plugs into [NewQueryFromTerms] / [NewCachedQueryFromTerms]
// alongside With/Without/Or/Maybe.
//
// Evaluation: the sub-expression is evaluated per entity (or at table level when
// all inner terms are simple archetype TermAnd terms). The result is negated:
// an entity matches the scope if and only if the sub-expression is false.
//
// OR-groups inside the scope follow C flecs convention: a With call immediately
// followed by one or more Or calls forms a single OR-group (the With member is
// the first element; Or calls extend the group). Example:
//
//	b.With(velID).Or(speedID)   // → Velocity OR Speed
//	b.With(velID).With(speedID) // → Velocity AND Speed
//
// Panics at construction time if buildFn produces zero inner terms (mirrors
// upstream validator.c:1441 "invalid empty scope").
//
// Example — Position AND NOT (Velocity OR Speed):
//
//	q := flecs.NewQueryFromTerms(w,
//	    flecs.With(posID),
//	    flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
//	        b.With(velID).Or(speedID)
//	    }),
//	)
func WithoutScope(buildFn func(*ScopeBuilder)) Term {
	b := &ScopeBuilder{}
	buildFn(b)
	if len(b.terms) == 0 {
		panic("flecs: WithoutScope: scope must contain at least one term (empty scope is invalid)")
	}
	return Term{Kind: TermScope, Sub: b.terms}
}

// Self returns a copy of the term with explicit TraverseSelf semantics.
// This is the default traversal mode; calling Self() is provided for
// readability and symmetry with Up/SelfUp/Cascade.
//
// When a component is marked inheritable via SetInheritable[T], query
// construction normally auto-promotes terms to SelfUp(IsA). Calling Self()
// explicitly suppresses that auto-promotion: the term will match only entities
// that own the component locally, regardless of inheritable status.
func (t Term) Self() Term { t.Traverse = TraverseExplicitSelf; t.Trav = 0; return t }

// Up returns a copy of the term with TraverseUp semantics: matched tables must
// have entities whose nearest ancestor via rel owns the component (the entity's
// own table need not contain it). Use FieldShared[T] to read the inherited value.
//
// The canonical relationships are w.IsA() (prefab inheritance) and w.ChildOf()
// (parent-child hierarchy). Any custom traversable relationship is accepted.
func (t Term) Up(rel ID) Term { t.Traverse = TraverseUp; t.Trav = rel; return t }

// SelfUp returns a copy of the term with TraverseSelfUp semantics: the entity's
// own table is checked first; if the component is absent, the ancestor chain via
// rel is walked. Self takes precedence when both apply. Use IsFieldSelf to
// determine which path was taken for the current table.
func (t Term) SelfUp(rel ID) Term { t.Traverse = TraverseSelfUp; t.Trav = rel; return t }

// Cascade returns a copy of the term with TraverseCascade semantics: matches
// like SelfUp but guarantees that the matched tables are iterated in ascending
// depth order in the rel hierarchy (root-first, then children). Only valid in
// NewCachedQueryFromTerms; NewQueryFromTerms panics if a Cascade term is present.
func (t Term) Cascade(rel ID) Term { t.Traverse = TraverseCascade; t.Trav = rel; return t }

// Source binds this term to a specific entity rather than the iterated entity
// ($this). The component is read once from e at iter start and returned as a
// 1-element slice by Field[T]; it does NOT contribute to the archetype-filter set.
//
// Panics if e is zero. Cannot be combined with .Up(), .SelfUp(), or .Cascade().
// Use WithSourceTerm as the preferred one-step constructor.
func (t Term) Source(e ID) Term {
	if e == 0 {
		panic("flecs: Term.Source: source entity must not be zero")
	}
	if t.Traverse != TraverseSelf && t.Traverse != 0 {
		panic("flecs: Term.Source: cannot combine a fixed source with traversal (Up/SelfUp/Cascade)")
	}
	t.Src = e
	return t
}

// WithSourceTerm returns a TermAnd term that reads componentID from sourceEntity
// rather than the iterated entity. The term does NOT add to the archetype-filter
// set — it is resolved once at iter start. If sourceEntity does not hold
// componentID, the entire query yields zero results.
//
// Use FieldMaybe (with a Maybe(componentID).Source(sourceEntity) term) for the
// "optional config" pattern where an absent component is acceptable.
//
// Panics if componentID or sourceEntity is zero.
func WithSourceTerm(componentID, sourceEntity ID) Term {
	if componentID == 0 {
		panic("flecs: WithSourceTerm: componentID must not be zero")
	}
	if sourceEntity == 0 {
		panic("flecs: WithSourceTerm: sourceEntity must not be zero")
	}
	return Term{ID: componentID, Kind: TermAnd, Src: sourceEntity}
}

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
	// skipDisabled and skipPrefab implement the implicit-skip fast path: when no
	// term mentions Disabled or Prefab (in any kind), tables that contain either
	// tag are excluded via a single HasComponent test per table — O(1) per table.
	// This mirrors the C per-table flag mask (EcsTableIsDisabled/EcsTableIsPrefab)
	// at src/query/engine/eval.c:88. The flag approach is preferred over synthetic
	// Not terms: no impact on OR-group handling and the original term list remains
	// unmodified for Terms()/TermsFull() introspection.
	skipDisabled bool
	skipPrefab   bool
	// alwaysFalse is set when an OrFrom term was expanded at construction and the
	// source entity had no inheritable components. An empty disjunction is false
	// by set-theoretic semantics; Iter returns a zero-result iterator immediately.
	alwaysFalse bool
}

// applyInheritablePromotion auto-promotes a single term to SelfUp(IsA) when
// the term's component was marked inheritable via SetInheritable and the term
// has not already had its traversal set explicitly. Mirror of C flecs
// validator.c:766-770.
//
// Promotion rules:
//   - Only TermAnd and TermOptional are eligible (TermNot is never promoted:
//     "table does not contain this id" does not interact with Up traversal).
//   - Only terms whose Traverse is the default zero (TraverseSelf) are promoted;
//     terms where the user called .Self(), .Up(), .SelfUp(), or .Cascade() keep
//     their explicit setting.
func applyInheritablePromotion(w *World, term *Term) {
	if term.Kind != TermAnd && term.Kind != TermOptional {
		return
	}
	if term.Traverse != TraverseSelf {
		return // user already set traversal explicitly
	}
	info, ok := w.registry.LookupByID(term.ID)
	if !ok || !info.Inheritable {
		return
	}
	// DontInherit overrides Inheritable: suppress Up traversal even when Inheritable is true.
	// C flecs precedence: DontInherit > Inherit (validator.c:829, entity.c:2595).
	if w.instantiatePolicies[term.ID]&policyOnInstantiateDontInherit != 0 {
		return
	}
	term.Traverse = TraverseSelfUp
	term.Trav = w.isAID
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
		terms[i] = Term{ID: id, Kind: TermAnd,
			Sparse:       isSparseTermID(w, id),
			DontFragment: isDontFragmentTermID(w, id),
			Union:        isUnionTermID(w, id),
		}
		applyInheritablePromotion(w, &terms[i])
	}
	skipDisabled, skipPrefab := computeQuerySkipFlags(w, terms)
	return &Query{w: w, terms: terms, andIDs: cp, skipDisabled: skipDisabled, skipPrefab: skipPrefab}
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
	for _, t := range terms {
		if t.Traverse == TraverseCascade {
			panic("flecs: NewQueryFromTerms: cascade requires a cached query; use NewCachedQueryFromTerms")
		}
	}
	cp, andIDs, orGroups, alwaysFalse := validateAndSortTerms(w, "flecs: NewQueryFromTerms", terms)
	skipDisabled, skipPrefab := computeQuerySkipFlags(w, cp)
	return &Query{w: w, terms: cp, andIDs: andIDs, orGroups: orGroups, skipDisabled: skipDisabled, skipPrefab: skipPrefab, alwaysFalse: alwaysFalse}
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

// findUpSource locates the nearest ancestor of the entities in table t via
// relationship rel that locally owns component termID. It starts from the first
// (rel, target) pair in t's archetype signature — the entity itself is not
// checked (pure Up semantics). Returns (ancestorEntity, true) on success.
func findUpSource(w *World, t *table.Table, termID ID, rel ID) (ID, bool) {
	relIdx := rel.Index()
	target, ok := firstPairTarget(t.Type(), relIdx)
	if !ok {
		return 0, false // no parent in this table's archetype
	}
	return walkUp(w, target, rel, func(cur ID) bool {
		rec := w.index.Get(cur)
		return rec != nil && rec.Table != nil && rec.Table.HasComponent(termID)
	})
}

// resolveFixedSourcePtr looks up the data pointer for term.ID on term.Src.
// Returns (ptr, true) when the component is present: ptr may be nil for a tag
// (present but no data). Returns (nil, false) when the component is absent.
func resolveFixedSourcePtr(w *World, term Term) (unsafe.Pointer, bool) {
	src := term.Src
	if term.Union {
		// term.Union=true implies SetUnion was called; the store always exists.
		relKey := ID(term.ID.First().Index())
		store := w.unionStore[relKey]
		pos, has := store.index[ID(src.Index())]
		if !has {
			return nil, false
		}
		termTarget := term.ID.Second()
		if !isWildcardID(w, termTarget) && store.dense[pos].target.Index() != termTarget.Index() {
			return nil, false
		}
		// Union pairs are tag-like; return nil (present=true, no data ptr).
		return nil, true
	}
	if term.DontFragment || term.Sparse {
		p := sparseSetGet(w, src, term.ID)
		return p, p != nil
	}
	// Archetype term: look up via entity index. Src is validated alive at
	// construction time so rec is always non-nil; an alive entity always has a table.
	rec := w.index.Get(src)
	if !rec.Table.HasComponent(term.ID) {
		return nil, false
	}
	// Get returns nil for tag columns (Size==0); present=true because HasComponent passed.
	p := rec.Table.Get(int(rec.Row), term.ID)
	return p, true
}

// buildFixedSourcePtrs resolves all fixed-source terms in the term list and
// returns (ptrs, present, dead). dead=true means a required (TermAnd) fixed-source
// component is absent on its source entity; the caller should return a dead iter.
func buildFixedSourcePtrs(w *World, terms []Term) (map[ID]unsafe.Pointer, map[ID]bool, bool) {
	var ptrs map[ID]unsafe.Pointer
	var present map[ID]bool
	for _, term := range terms {
		if term.Src == 0 {
			continue
		}
		ptr, ok := resolveFixedSourcePtr(w, term)
		if !ok && term.Kind == TermAnd {
			return nil, nil, true // dead: required component absent
		}
		if ptrs == nil {
			ptrs = make(map[ID]unsafe.Pointer)
			present = make(map[ID]bool)
		}
		ptrs[term.ID] = ptr
		present[term.ID] = ok
	}
	return ptrs, present, false
}

// Iter starts a fresh iteration over all archetype tables matching the query.
//
// Seed strategy (all-archetype mode): pick the TermAnd term whose component-index
// entry has the fewest tables (O(And-terms) scan), then for each seed table verify
// that every other TermAnd is present and every TermNot is absent. TermOptional
// terms do not affect matching. This is O(smallest-set × terms) — optimal for
// sparse queries.
//
// Sparse modes:
//   - All-sparse: all TermAnd terms refer to sparse components. The smallest
//     sparse-set (by current dense length) is chosen as the driver; each entity
//     in it is checked against every other sparse TermAnd/TermNot term.
//   - Mixed: some TermAnd terms are archetype, some are sparse. Archetype terms
//     seed candidate tables as in the all-archetype mode; within each matched
//     table each entity is additionally checked against the sparse terms.
//
// The seed table list is materialised once via TablesFor (one allocation) when a
// TraverseSelf TermAnd term exists; otherwise all world tables are used as candidates
// so that Up/SelfUp traversal can find non-locally-owned components.
func (q *Query) Iter() *QueryIter {
	q.w.checkExclusiveAccessRead()

	// OrFrom with an empty source type sets alwaysFalse at construction.
	// No archetype scan needed — return an already-exhausted iterator.
	if q.alwaysFalse {
		return &QueryIter{
			world:           q.w,
			terms:           q.terms,
			pos:             0, // already past end
			wildcardTermIdx: -1,
			wildcardPairPos: -1,
		}
	}

	// Resolve fixed-source terms once at iter start.
	fixedPtrs, fixedPresent, dead := buildFixedSourcePtrs(q.w, q.terms)
	if dead {
		// A required fixed-source component is absent on its source entity.
		// The entire query yields zero results (mirrors upstream eval.c:114-117).
		return &QueryIter{
			world:              q.w,
			terms:              q.terms,
			fixedSourcePtrs:    fixedPtrs,
			fixedSourcePresent: fixedPresent,
			pos:                0, // already past end
			wildcardTermIdx:    -1,
			wildcardPairPos:    -1,
		}
	}

	// Classify And terms.
	// dontFragmentAndCount: terms not in archetype (drives iteration MODE).
	// sparseAndCount: terms with sparse-set data (drives Field routing + mixed mode).
	// archetypeAndCount: terms with both archetype presence AND no DontFragment.
	// unionAndCount: union-pair terms (not in archetype; checked per-entity via union store).
	// Fixed-source TermAnd terms are excluded: they don't drive $this iteration.
	dontFragmentAndCount := 0
	sparseAndCount := 0
	archetypeAndCount := 0
	unionAndCount := 0
	for _, t := range q.terms {
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
			sparseAndCount++ // DontFragment data is also in sparse-set
		} else if t.Sparse {
			sparseAndCount++ // Sparse-only: data in sparse-set, component IS in archetype
			archetypeAndCount++
		} else {
			archetypeAndCount++
		}
	}
	// allDontFragment: all And terms are DontFragment → pure sparse-set iteration.
	allDontFragment := archetypeAndCount == 0 && dontFragmentAndCount > 0 && unionAndCount == 0
	// allUnion: all And terms are union pairs → pure union-store iteration.
	allUnion := archetypeAndCount == 0 && dontFragmentAndCount == 0 && unionAndCount > 0
	// hasComplexScope: any TermScope term requires per-entity evaluation → force mixed mode.
	// hasEqTerms: any equality term (TermEq/TermNotEq/TermNameMatch) requires per-entity evaluation.
	hasComplexScope := false
	hasEqTerms := false
	for _, t := range q.terms {
		if t.Kind == TermScope && !isScopeTableSimple(t.Sub) {
			hasComplexScope = true
		}
		if t.Kind == TermEq || t.Kind == TermNotEq || t.Kind == TermNameMatch {
			hasEqTerms = true
		}
	}
	// hasSparseTerms: at least one And term has sparse-set or union data, or there
	// is a complex scope term, or there is an equality/name-match term → mixed entity-at-a-time evaluation needed.
	hasSparseTerms := sparseAndCount > 0 || (unionAndCount > 0 && !allUnion) || hasComplexScope || hasEqTerms
	_ = dontFragmentAndCount // used indirectly via allDontFragment

	wildcardIdx := findWildcardTermIdx(q.w, q.terms)

	if allDontFragment {
		// Pure-DontFragment mode: find the smallest sparse-set as driver.
		// All And terms are DontFragment (not in any archetype table).
		var driver []sparseEntry
		zeroDriver := false
		minLen := -1
		for _, term := range q.terms {
			if term.Kind != TermAnd || !term.DontFragment {
				continue
			}
			key := ID(term.ID.Index())
			ss := q.w.sparseStorage[key]
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
			q:                  q,
			world:              q.w,
			terms:              q.terms,
			orGroups:           q.orGroups,
			allSparse:          true, // kept for Table() panic message compatibility
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

	if allUnion {
		// Pure-Union mode: find the smallest union store as driver.
		// All And terms are union pairs (not in any archetype table).
		var driver []unionEntry
		zeroDriver := false
		minLen := -1
		for _, term := range q.terms {
			if term.Kind != TermAnd || !term.Union {
				continue
			}
			relKey := ID(term.ID.First().Index())
			store, ok := q.w.unionStore[relKey]
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
			q:                  q,
			world:              q.w,
			terms:              q.terms,
			orGroups:           q.orGroups,
			allUnion:           true,
			hasSparseTerms:     true, // per-entity checking
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

	// Archetype seed selection — used for both all-archetype and mixed modes.
	// Skip DontFragment/Union terms (not in archetype tables), fixed-source terms
	// (they don't constrain $this), transitive/reflexive pairs, and wildcard terms.
	// Sparse-only terms (Sparse=true, DontFragment=false) ARE in archetype tables and can seed.
	seedIdx := -1
	minCount := 0
	for i, term := range q.terms {
		if term.Kind != TermAnd || term.Traverse != TraverseSelf {
			continue
		}
		if term.Src != 0 {
			continue // fixed-source: does not constrain $this archetype set
		}
		if term.DontFragment || term.Union {
			continue // not in archetype tables; can't seed
		}
		if term.ID.IsPair() && q.w.transitivePolicies[ID(term.ID.First().Index())] {
			continue
		}
		if term.ID.IsPair() && q.w.reflexivePolicies[ID(term.ID.First().Index())] {
			continue
		}
		if isWildcardTerm(q.w, term.ID) {
			continue
		}
		c := q.w.compIndex.Count(term.ID)
		if seedIdx == -1 || c < minCount {
			minCount = c
			seedIdx = i
		}
	}
	var candidates []*table.Table
	if seedIdx >= 0 {
		candidates = q.w.TablesFor(q.terms[seedIdx].ID)
	} else {
		// All And terms use traversal or are sparse; must test every table.
		candidates = make([]*table.Table, 0, len(q.w.tables))
		for _, t := range q.w.tables {
			candidates = append(candidates, t)
		}
	}
	return &QueryIter{
		q:                  q,
		world:              q.w,
		terms:              q.terms,
		orGroups:           q.orGroups,
		candidates:         candidates,
		pos:                -1,
		hasSparseTerms:     hasSparseTerms,
		sparseTablePos:     -1,
		sparseDriverPos:    -1,
		wildcardTermIdx:    wildcardIdx,
		wildcardPairPos:    -1,
		fixedSourcePtrs:    fixedPtrs,
		fixedSourcePresent: fixedPresent,
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
//
// Sparse iteration: when any TermAnd term is sparse, the iterator operates in
// entity-at-a-time mode for those terms. Count() returns 1 per Next() step;
// Entities() returns a single-element slice; Field[T] reads through sparseSetGet.
// Wildcard pair expansion is not combined with sparse entity-at-a-time iteration
// in v0.52.0.
type QueryIter struct {
	q          *Query         // nil for CachedQuery-derived iters; use terms for term access
	world      *World         // backing world; set by Iter constructors; used by Reader()/Writer()
	terms      []Term         // full term list (And + Not + Or + Optional), set by Iter constructors
	orGroups   [][]ID         // OR-groups mirrored from Query/CachedQuery for matchesTable
	candidates []*table.Table // seed-table snapshot (uncached) or cache reference (cached)
	pos        int            // index into candidates; -1 = before first Next
	current    *table.Table   // non-nil only when positioned on a matching table
	// cached, when true, skips the per-candidate term check in Next: the
	// candidate list is pre-filtered by CachedQuery. When false (the default),
	// every candidate is evaluated against the term list.
	cached          bool
	optionalPresent map[ID]bool // Optional- and Or-term presence for the current table/entity
	// traversal source maps: component ID → resolved source entity.
	// 0 = self-matched (entity's own table owns the component).
	// non-zero = ancestor entity that owns the component (Up match).
	upSources       map[ID]ID   // per-table source map, updated by matchesTable or loaded from cache
	tableSourcesRef []map[ID]ID // for cached iters: parallel to candidates; nil for Self-only queries
	// multi-threaded dispatch clipping (zero workerTotal = no clipping, full table)
	workerIdx    int     // 0-based index of this worker
	workerTotal  int     // total worker count; 0 = no clipping
	wFirst       int     // first row for this worker in the current table
	wCount       int     // row count for this worker in the current table
	workerWriter *Writer // per-worker Writer bound to the worker's stage; set by the dispatcher
	// wildcard expansion state (zero-value = no wildcard term in this query)
	wildcardTermIdx int  // index of the first wildcard/any term in terms[]; -1 if none
	wildcardPairs   []ID // concrete matching pair IDs in current table for the wildcard term
	wildcardPairPos int  // position in wildcardPairs; -1 = not yet set for current table
	// sparse iteration state — set by Iter() when one or more And terms are sparse.
	// allSparse: ALL And terms are sparse; no archetype seed, iterate sparse-sets directly.
	// hasSparseTerms: at least one And term is sparse; entity-at-a-time within tables.
	allSparse           bool
	hasSparseTerms      bool
	sparseDriver        []sparseEntry // dense-slice snapshot of the smallest sparse-set (allSparse mode)
	sparseDriverPos     int           // current position in sparseDriver; -1 = before first
	sparseTableEntities []ID          // entities in current table that pass sparse membership (mixed mode)
	sparseTablePos      int           // current position in sparseTableEntities; -1 = needs next table
	sparseEntity        ID            // current entity in sparse/mixed mode; 0 = not positioned
	// union iteration state — set by Iter() when all And terms are union pairs.
	allUnion       bool         // all And terms are union pairs; iterate union store directly
	unionDriver    []unionEntry // snapshot of the smallest union store (allUnion mode)
	unionDriverPos int          // current position in unionDriver; -1 = before first
	// fixed-source state — set by Iter() for any term with a non-zero Src.
	// Resolved once at iter construction; Field[T] returns a 1-element slice backed
	// by the pointer. nil pointer = tag (present, no data). Absent optional terms
	// have fixedSourcePresent[id]=false.
	fixedSourcePtrs    map[ID]unsafe.Pointer // component pointer per fixed-source term ID
	fixedSourcePresent map[ID]bool           // true if the component is present on source (even for tags)
	// sorted iteration state — set by CachedQuery.Iter() when the query has an
	// order_by comparator. Entities are pre-sorted; yields one entity per Next().
	sortedMode     bool             // true = walk sortedEntities in order
	sortedPos      int              // current position; -1 = before first
	sortedEntities []ID             // sorted entity list (reference into CachedQuery)
	sortedRows     []sortedFieldRow // parallel (table, row) for each sorted entity
}

// Next advances to the next matching table (or next wildcard expansion row
// within the current table). Returns true when positioned on a valid row;
// returns false when iteration is exhausted.
//
// When the query contains a wildcard term, Next emits one row per concrete
// matching pair in each table (Wildcard) or exactly one row per table (Any).
// Use [MatchedTarget], [MatchedID], and [FieldByMatch] inside the loop body
// to inspect the concrete pair for the current row.
//
// For multi-threaded iterators (workerTotal > 0), tables where this worker's
// row count is zero are skipped transparently.
//
// Sparse modes: when the query contains sparse And terms, Next advances one
// entity at a time (not one table at a time). Count() returns 1 and Entities()
// returns a single-element slice for each step.
func (it *QueryIter) Next() bool {
	if it.sortedMode {
		return it.nextSorted()
	}
	if it.allSparse {
		return it.nextSparseOnly()
	}
	if it.allUnion {
		return it.nextUnionOnly()
	}
	if it.hasSparseTerms {
		return it.nextMixed()
	}

	// All-archetype fast path (unchanged from pre-v0.52.0).

	// Wildcard expansion: advance to the next concrete pair in the current table
	// before advancing the table pointer.
	if it.wildcardTermIdx >= 0 && it.wildcardPairPos >= 0 &&
		it.wildcardPairPos < len(it.wildcardPairs)-1 {
		it.wildcardPairPos++
		return true
	}

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
		// For cached iters, load the pre-computed per-table traversal sources.
		if it.cached && it.tableSourcesRef != nil && it.pos < len(it.tableSourcesRef) {
			it.upSources = it.tableSourcesRef[it.pos]
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
		// For wildcard terms, collect the matching concrete pairs and reset the
		// expansion position. matchesTable already guarantees at least one match.
		if it.wildcardTermIdx >= 0 {
			it.wildcardPairs = wildcardMatchingPairs(it.world, t, it.terms[it.wildcardTermIdx].ID)
			it.wildcardPairPos = 0
		}
		it.current = t
		it.updateOptionalPresence(t)
		return true
	}
}

// nextSparseOnly advances a pure-sparse iterator (all And terms are sparse).
// Iterates the driver dense-slice; for each entity checks all other sparse terms.
func (it *QueryIter) nextSparseOnly() bool {
	driver := it.sparseDriver
	if driver == nil {
		it.sparseEntity = 0
		return false
	}
	for {
		it.sparseDriverPos++
		if it.sparseDriverPos >= len(driver) {
			it.sparseEntity = 0
			return false
		}
		e := driver[it.sparseDriverPos].entity
		if it.matchesSparseTerms(e) {
			it.sparseEntity = e
			it.updateOptionalPresenceSparse(e)
			return true
		}
	}
}

// nextUnionOnly advances a pure-union iterator (all And terms are union pairs).
// Iterates the driver dense-slice; for each entity checks all other union terms.
func (it *QueryIter) nextUnionOnly() bool {
	driver := it.unionDriver
	if driver == nil {
		it.sparseEntity = 0
		return false
	}
	for {
		it.unionDriverPos++
		if it.unionDriverPos >= len(driver) {
			it.sparseEntity = 0
			return false
		}
		e := driver[it.unionDriverPos].entity
		if it.matchesSparseTerms(e) {
			it.sparseEntity = e
			it.updateOptionalPresenceSparse(e)
			return true
		}
	}
}

// nextMixed advances a mixed iterator (some And terms archetype, some sparse).
// Candidate archetype tables are iterated as in the all-archetype path; within
// each matched table entities are additionally filtered by sparse membership.
// Each Next() call yields exactly one entity (Count() == 1).
func (it *QueryIter) nextMixed() bool {
	for {
		// Try advancing within the current table's sparse-filtered entity list.
		it.sparseTablePos++
		if it.sparseTablePos < len(it.sparseTableEntities) {
			it.sparseEntity = it.sparseTableEntities[it.sparseTablePos]
			it.updateOptionalPresenceMixed(it.current, it.sparseEntity)
			return true
		}

		// Need the next matching archetype table.
		it.pos++
		if it.pos >= len(it.candidates) {
			it.current = nil
			it.sparseEntity = 0
			return false
		}
		t := it.candidates[it.pos]
		if !it.cached && !it.matchesTable(t) {
			it.sparseTableEntities = it.sparseTableEntities[:0]
			it.sparseTablePos = 0 // 0 < 0 on next iteration → advance table
			continue
		}
		if it.cached && it.tableSourcesRef != nil && it.pos < len(it.tableSourcesRef) {
			it.upSources = it.tableSourcesRef[it.pos]
		}
		it.current = t

		// Build the filtered entity list for this table.
		all := t.Entities()
		it.sparseTableEntities = it.sparseTableEntities[:0]
		for _, e := range all {
			if it.matchesSparseTerms(e) {
				it.sparseTableEntities = append(it.sparseTableEntities, e)
			}
		}
		it.sparseTablePos = -1 // incremented to 0 at top of next iteration
	}
}

// matchesSparseTerms returns true if entity e satisfies all DontFragment, Union,
// and complex-scope terms in the query. TermAnd requires presence; TermNot
// requires absence. Sparse-only terms (DontFragment=false, Union=false) are
// handled by the archetype check in matchesTable and are skipped here.
// TermOptional and TermOr do not affect matching. Simple TermScope terms
// (isScopeTableSimple) are handled at table level and skipped here; complex
// scopes are evaluated per entity.
func (it *QueryIter) matchesSparseTerms(e ID) bool {
	for _, term := range it.terms {
		if term.DontFragment {
			ptr := sparseSetGet(it.world, e, term.ID)
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
			store, ok := it.world.unionStore[relKey]
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
				if !isWildcardID(it.world, termTarget) {
					if store.dense[pos].target.Index() != termTarget.Index() {
						return false
					}
				}
			case TermNot:
				if !ok || store == nil {
					continue // entity definitely not in this union store
				}
				_, has := store.index[ID(e.Index())]
				if has {
					return false
				}
			}
		} else if term.Kind == TermScope {
			// Scope evaluation: complex scopes always require per-entity evaluation.
			// Simple scopes (all-archetype-And inner terms) are handled at the table
			// level by matchesTable, so skip them here when a current table exists.
			// In pure-sparse/union mode (it.current == nil) there is no current table,
			// so we must evaluate even simple scopes per-entity (entityHasTermInScope
			// will look up the entity's table from the world index in that case).
			if it.current != nil && isScopeTableSimple(term.Sub) {
				continue // handled by matchesTable for the current archetype table
			}
			if evalScopeSubTerms(it.world, e, it.current, term.Sub) {
				return false // NOT(sub-expression) → scope failed
			}
		} else if term.Kind == TermEq {
			if e != term.ID {
				return false
			}
		} else if term.Kind == TermNotEq {
			if e == term.ID {
				return false
			}
		} else if term.Kind == TermNameMatch {
			name, ok := it.world.GetName(e)
			if !ok {
				return false // unnamed entities never match
			}
			if !substrMatchCaseInsensitive(name, term.Pattern) {
				return false
			}
		}
	}
	return true
}

// substrMatchCaseInsensitive reports whether name contains pattern as a
// case-insensitive substring. Mirrors upstream flecs_query_match_substr_i
// (eval_pred.c:19-41). Empty pattern returns true (matches every named entity).
func substrMatchCaseInsensitive(name, pattern string) bool {
	if pattern == "" {
		return true
	}
	if len(pattern) > len(name) {
		return false
	}
	pLower := strings.ToLower(pattern)
	nLower := strings.ToLower(name)
	return strings.Contains(nLower, pLower)
}

// updateOptionalPresenceSparse updates optionalPresent for pure-DontFragment queries.
// All optional terms have sparse-set data (no current archetype table exists).
func (it *QueryIter) updateOptionalPresenceSparse(e ID) {
	for k := range it.optionalPresent {
		delete(it.optionalPresent, k)
	}
	for _, term := range it.terms {
		if term.Kind != TermOptional && term.Kind != TermOr {
			continue
		}
		if term.Src != 0 {
			continue // fixed-source optional: presence is in fixedSourcePresent, not per-table
		}
		if it.optionalPresent == nil {
			it.optionalPresent = make(map[ID]bool)
		}
		if term.Sparse { // all terms in pure-DontFragment mode have sparse-set data
			it.optionalPresent[term.ID] = sparseSetGet(it.world, e, term.ID) != nil
		}
	}
}

// updateOptionalPresenceMixed updates optionalPresent for mixed-mode queries.
// DontFragment optional terms are checked via sparse-set; Sparse-only and archetype
// optional terms are checked via the current archetype table.
func (it *QueryIter) updateOptionalPresenceMixed(t *table.Table, e ID) {
	for k := range it.optionalPresent {
		delete(it.optionalPresent, k)
	}
	for _, term := range it.terms {
		if term.Kind != TermOptional && term.Kind != TermOr {
			continue
		}
		if term.Src != 0 {
			continue // fixed-source optional: presence is in fixedSourcePresent, not per-entity
		}
		if it.optionalPresent == nil {
			it.optionalPresent = make(map[ID]bool)
		}
		if term.DontFragment {
			// DontFragment: component not in archetype; check sparse-set.
			it.optionalPresent[term.ID] = sparseSetGet(it.world, e, term.ID) != nil
		} else {
			// Archetype or Sparse-only: presence is determined by table signature.
			it.optionalPresent[term.ID] = t.HasComponent(term.ID)
		}
	}
}

// matchesTable returns true if t satisfies all TermAnd, TermNot, and OR-group
// terms. TermOptional terms are ignored during matching. For traversal And terms
// (TraverseUp, TraverseSelfUp), the ancestor chain is walked via findUpSource;
// resolved sources are stored in it.upSources for use by IsFieldSelf/FieldShared.
//
// Sparse terms (term.Sparse == true) are skipped: they do not live in archetype
// tables and are checked per-entity by matchesSparseTerms instead.
func (it *QueryIter) matchesTable(t *table.Table) bool {
	// Implicit skip: exclude tables containing Disabled or Prefab unless the
	// query explicitly mentioned either tag in any term kind.
	if it.q.skipDisabled && t.HasComponent(it.world.disabledID) {
		return false
	}
	if it.q.skipPrefab && t.HasComponent(it.world.prefabID) {
		return false
	}
	// Reset traversal sources from any previous table.
	for k := range it.upSources {
		delete(it.upSources, k)
	}
	for _, term := range it.terms {
		switch term.Kind {
		case TermAnd:
			if term.Src != 0 {
				break // fixed-source: resolved at iter start, not an archetype constraint
			}
			if term.DontFragment || term.Union {
				break // not in archetype; skip archetype check (checked per-entity)
			}
			switch term.Traverse {
			case TraverseSelf, TraverseExplicitSelf:
				if !t.HasComponent(term.ID) {
					// Wildcard/Any pair matching: the sentinel ID itself is never
					// in any table; instead check for any concrete pair that satisfies
					// the wildcard pattern (e.g. any (R, X) for term (R, Wildcard)).
					if isWildcardTerm(it.world, term.ID) {
						if !tableHasWildcardMatch(it.world, t, term.ID) {
							return false
						}
					} else if term.ID.IsPair() {
						rel := ID(term.ID.First().Index())
						isTransitive := it.world.transitivePolicies[rel]
						isReflexive := it.world.reflexivePolicies[rel]
						matched := false
						if isTransitive {
							matched = transitiveTableMatches(it.world, t, term.ID)
						}
						if !matched && isReflexive {
							// Reflexive self-match: target entity's own table qualifies.
							// Composes with Transitive: either a chain walk OR the self-match.
							matched = reflexiveTableMatches(it.world, t, term.ID)
						}
						if !matched {
							return false
						}
					} else {
						return false
					}
				}
			case TraverseUp:
				src, ok := findUpSource(it.world, t, term.ID, term.Trav)
				if !ok {
					return false
				}
				if it.upSources == nil {
					it.upSources = make(map[ID]ID)
				}
				it.upSources[term.ID] = src
			case TraverseSelfUp, TraverseCascade:
				if t.HasComponent(term.ID) {
					if it.upSources == nil {
						it.upSources = make(map[ID]ID)
					}
					it.upSources[term.ID] = 0 // self-matched
				} else {
					src, ok := findUpSource(it.world, t, term.ID, term.Trav)
					if !ok {
						return false
					}
					if it.upSources == nil {
						it.upSources = make(map[ID]ID)
					}
					it.upSources[term.ID] = src
				}
			}
		case TermNot:
			if term.DontFragment || term.Union {
				break // not in archetype; skip archetype check (checked per-entity)
			}
			if t.HasComponent(term.ID) {
				return false
			}
		case TermScope:
			if !isScopeTableSimple(term.Sub) {
				break // complex scope: per-entity evaluation in matchesSparseTerms
			}
			// Table-level fast path for all-TermAnd simple scopes:
			// NOT(A ∧ B ∧ … ∧ Z) is false for every entity in this table when all
			// inner IDs are present in the archetype (all entities own them all).
			// Reject the table; otherwise the scope is trivially satisfied for all
			// entities (at least one ID is absent from every entity in the table).
			allPresent := true
			for _, sub := range term.Sub {
				if !t.HasComponent(sub.ID) {
					allPresent = false
					break
				}
			}
			if allPresent {
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
// Optional or Or terms (common case: zero allocs). Fixed-source optional terms
// are excluded: their presence is stored in fixedSourcePresent and is constant
// across all tables.
func (it *QueryIter) updateOptionalPresence(t *table.Table) {
	for k := range it.optionalPresent {
		delete(it.optionalPresent, k)
	}
	for _, term := range it.terms {
		if (term.Kind == TermOptional || term.Kind == TermOr) && term.Src == 0 {
			if it.optionalPresent == nil {
				it.optionalPresent = make(map[ID]bool)
			}
			it.optionalPresent[term.ID] = t.HasComponent(term.ID)
		}
	}
}

// Table returns the current matching archetype table. Panics if called before
// the first Next, after Next returned false, or on a pure-sparse iterator (which
// has no archetype table; use Entities() to get the current entity instead).
func (it *QueryIter) Table() *table.Table {
	if it.current == nil {
		if it.allSparse || it.allUnion {
			panic("flecs: QueryIter.Table: not valid for pure-sparse/union queries (no archetype table); use Entities() to get the current entity")
		}
		panic("flecs: QueryIter.Table: not positioned on a valid table (call Next first)")
	}
	return it.current
}

// Count returns the number of entities visible to this iterator in the current
// step. For multi-threaded iterators this is the worker's row count (a subset of
// the full table). For sparse or mixed-sparse iterators, Count() is always 1
// (entity-at-a-time mode). Panics if not positioned.
func (it *QueryIter) Count() int {
	if it.allSparse || it.hasSparseTerms {
		return 1
	}
	if it.workerTotal > 0 {
		return it.wCount
	}
	return it.Table().Count()
}

// Entities returns the entity IDs for this iterator's rows in the current step.
// For multi-threaded iterators this is the worker's disjoint row slice; for
// sparse or mixed-sparse iterators this is a single-element slice containing the
// current entity. The slice is invalidated by the next Next call.
func (it *QueryIter) Entities() []ID {
	if it.allSparse || it.hasSparseTerms {
		if it.sparseEntity == 0 {
			return nil
		}
		return []ID{it.sparseEntity}
	}
	all := it.Table().Entities()
	if it.workerTotal > 0 {
		return all[it.wFirst : it.wFirst+it.wCount]
	}
	return all
}

// Query returns the Query that produced this iterator. Returns nil for iters
// derived from a CachedQuery.
func (it *QueryIter) Query() *Query { return it.q }

// Reader returns a *Reader capability backed by the iterator's world.
// Valid for the duration of the enclosing Read or Write scope.
func (it *QueryIter) Reader() *Reader { return &it.world.readCapability }

// Writer returns a *Writer capability backed by the iterator's world.
// In multi-threaded dispatch each worker iterator carries its own per-stage
// Writer (set by the dispatcher); for all other iterators this falls back to
// the world's main-stage Writer.
func (it *QueryIter) Writer() *Writer {
	if it.workerWriter != nil {
		return it.workerWriter
	}
	return &it.world.writeCapability
}

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
// For sparse terms: returns a single-element []T slice backed by the stable
// sparse-set pointer for the current entity. The element is valid until the next
// it.Next() call (but the allocation itself is stable for the world's lifetime).
//
// Panics if:
//   - it is not positioned on a valid entity/table (Next has not been called or
//     returned false).
//   - id is not in the current table's signature (archetype terms) or is not
//     present in the sparse-set for the current entity (sparse terms).
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
	// Fixed-source term: return 1-element slice backed by the snapshot pointer.
	// The pointer is valid for the lifetime of this iter (snapshot-at-iter-start contract).
	for _, term := range it.terms {
		if term.ID == id && term.Src != 0 {
			if !it.fixedSourcePresent[id] {
				panic(fmt.Sprintf("flecs: Field[%s]: fixed-source component %d is not present on source entity %d; use FieldMaybe for optional fixed-source terms",
					reflect.TypeFor[T](), id, term.Src))
			}
			ptr := it.fixedSourcePtrs[id]
			if ptr == nil {
				// Tag component: present on source but no data storage.
				return make([]T, 1)
			}
			return unsafe.Slice((*T)(ptr), 1)
		}
	}
	// Sparse term: read through the sparse-set for the current entity.
	for _, term := range it.terms {
		if term.ID == id && term.Sparse {
			e := it.sparseEntity
			if e == 0 {
				panic(fmt.Sprintf("flecs: Field[%s]: not positioned on a valid entity (call Next first)",
					reflect.TypeFor[T]()))
			}
			ptr := sparseSetGet(it.world, e, id)
			if ptr == nil {
				panic(fmt.Sprintf("flecs: Field[%s]: sparse component %d is not present on current entity",
					reflect.TypeFor[T](), id))
			}
			return unsafe.Slice((*T)(ptr), 1)
		}
	}

	// Archetype term: use the current table's column.
	tbl := it.Table() // panics if not positioned
	if !tbl.HasComponent(id) {
		// Check if this is a traversal term matched via an ancestor (Up path).
		for _, term := range it.terms {
			if term.ID == id && term.Traverse != TraverseSelf && term.Traverse != TraverseExplicitSelf {
				panic(fmt.Sprintf("flecs: Field[%s]: id %d was matched via Up (component lives on an ancestor); use FieldShared[T] to read the inherited value",
					reflect.TypeFor[T](), id))
			}
		}
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
// Returns (nil, false) if the current table/entity does not contain id.
// Returns (slice, true) if the current table/entity contains id.
//
// For sparse optional terms: returns a single-element slice backed by the stable
// sparse-set pointer (nil, false) when the entity does not hold the component.
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
		// Fixed-source optional: presence determined at iter start, not per table.
		if term.Src != 0 {
			if !it.fixedSourcePresent[id] {
				return nil, false
			}
			ptr := it.fixedSourcePtrs[id]
			if ptr == nil {
				return make([]T, 1), true // tag present on source
			}
			return unsafe.Slice((*T)(ptr), 1), true
		}
		if term.Sparse {
			// Sparse optional: read through sparse-set for the current entity.
			e := it.sparseEntity
			if e == 0 {
				panic(fmt.Sprintf("flecs: FieldMaybe[%s]: not positioned on a valid entity (call Next first)",
					reflect.TypeFor[T]()))
			}
			ptr := sparseSetGet(it.world, e, id)
			if ptr == nil {
				return nil, false
			}
			return unsafe.Slice((*T)(ptr), 1), true
		}
		// Archetype optional: check via optionalPresent map populated by updateOptionalPresence*.
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

// IsFieldSelf reports whether the component id in the current iterator table is
// owned locally by the matched entities (true) or was resolved from an ancestor
// via Up traversal (false). Always true for TraverseSelf terms.
//
// Panics if id is not in this query's term list.
func IsFieldSelf(it *QueryIter, id ID) bool {
	for _, term := range it.terms {
		if term.ID != id {
			continue
		}
		if term.Traverse == TraverseSelf || term.Traverse == TraverseExplicitSelf {
			return true
		}
		// upSources[id] == 0 means self-matched; non-zero means ancestor entity.
		// A nil map lookup returns the zero value (0), which also means self.
		return it.upSources[id] == 0
	}
	panic(fmt.Sprintf("flecs: IsFieldSelf: id %d is not in this query's term list", id))
}

// FieldShared returns the inherited component value for a traversal term (Up,
// SelfUp, or Cascade) that was resolved from an ancestor. Returns (zero, false)
// when the term was matched via Self for the current table — use Field[T] instead.
//
// The returned value is a copy of the ancestor's component slot and is valid only
// until the next it.Next() call (consistent with Field[T] aliasing rules).
//
// Panics if id is a TraverseSelf term (programming error; use Field[T]) or if id
// is not in this query's term list.
func FieldShared[T any](it *QueryIter, id ID) (T, bool) {
	var zero T
	for _, term := range it.terms {
		if term.ID != id {
			continue
		}
		if term.Traverse == TraverseSelf || term.Traverse == TraverseExplicitSelf {
			panic(fmt.Sprintf("flecs: FieldShared[%s]: id %d uses TraverseSelf semantics; use Field[T] for locally-owned components",
				reflect.TypeFor[T](), id))
		}
		// upSources[id] == 0 means self-matched; non-zero means ancestor entity.
		// A nil map lookup returns 0 (zero value), so a nil map also means self.
		src := it.upSources[id]
		if src == 0 {
			return zero, false // self-matched; use Field[T]
		}
		rec := it.world.index.Get(src)
		if rec == nil || rec.Table == nil {
			return zero, false
		}
		_, typ, _ := rec.Table.ColumnBasePtr(id)
		if typ == nil {
			// Tag component: no data, but component is present on the ancestor.
			return zero, true
		}
		want := reflect.TypeFor[T]()
		if typ != want {
			panic(fmt.Sprintf("flecs: FieldShared[%s]: column for id %d holds %s, not %s",
				want, id, typ, want))
		}
		ptr := rec.Table.Get(int(rec.Row), id)
		if ptr == nil {
			return zero, true
		}
		return *(*T)(ptr), true
	}
	panic(fmt.Sprintf("flecs: FieldShared[%s]: id %d is not in this query's term list",
		reflect.TypeFor[T](), id))
}

// computeQuerySkipFlags returns (skipDisabled, skipPrefab): true for each tag
// that is NOT mentioned in any term. When true, tables containing that tag are
// excluded in matchesTable / tryMatchTable without further per-entity checks.
// A tag is "mentioned" when any term's raw ID index matches the tag's index,
// regardless of TermKind (With/Without/Maybe/Or all opt in).
func computeQuerySkipFlags(w *World, terms []Term) (skipDisabled, skipPrefab bool) {
	disabledIdx := w.disabledID.Index()
	prefabIdx := w.prefabID.Index()
	skipDisabled = true
	skipPrefab = true
	for _, t := range terms {
		if t.Kind == TermScope {
			for _, sub := range t.Sub {
				idx := sub.ID.Index()
				if idx == disabledIdx {
					skipDisabled = false
				}
				if idx == prefabIdx {
					skipPrefab = false
				}
			}
		} else {
			idx := t.ID.Index()
			if idx == disabledIdx {
				skipDisabled = false
			}
			if idx == prefabIdx {
				skipPrefab = false
			}
		}
		if !skipDisabled && !skipPrefab {
			break
		}
	}
	return
}

// isScopeTableSimple reports whether all inner scope terms can be fully evaluated
// at table granularity. A scope is table-simple when every inner term is a
// standalone TermAnd (no DontFragment, Union, Sparse, fixed source, and not the
// first element of an OR-group, i.e. not followed by a TermOr term).
// When true, matchesTable / tryMatchTable can apply the fast path without any
// per-entity evaluation.
func isScopeTableSimple(sub []Term) bool {
	for i, t := range sub {
		if t.Kind != TermAnd {
			return false
		}
		if t.DontFragment || t.Union || t.Sparse || t.Src != 0 {
			return false
		}
		if i+1 < len(sub) && sub[i+1].Kind == TermOr {
			return false // starts an OR-group → per-entity evaluation needed
		}
	}
	return true
}

// entityHasTermInScope checks whether entity e satisfies a single scope inner
// term. t is the entity's current archetype table (may be nil in pure-sparse /
// union iteration; the function falls back to an index lookup in that case).
func entityHasTermInScope(w *World, e ID, t *table.Table, term Term) bool {
	if term.DontFragment {
		return sparseSetGet(w, e, term.ID) != nil
	}
	if term.Union {
		relKey := ID(term.ID.First().Index())
		store, ok := w.unionStore[relKey]
		if !ok || store == nil {
			return false
		}
		pos, has := store.index[ID(e.Index())]
		if !has {
			return false
		}
		termTarget := term.ID.Second()
		if !isWildcardID(w, termTarget) {
			return store.dense[pos].target.Index() == termTarget.Index()
		}
		return true
	}
	if term.Src != 0 {
		// Fixed-source term: evaluate against the named entity, not $this.
		rec := w.index.Get(term.Src)
		if rec == nil || rec.Table == nil {
			return false
		}
		return rec.Table.HasComponent(term.ID)
	}
	// Archetype term.
	if t != nil {
		return t.HasComponent(term.ID)
	}
	// Pure-sparse / union mode: no current archetype table; look up entity's table.
	rec := w.index.Get(e)
	if rec == nil || rec.Table == nil {
		return false
	}
	return rec.Table.HasComponent(term.ID)
}

// evalScopeSubTerms evaluates the scope sub-expression against entity e and
// returns true when the sub-expression is satisfied. The caller negates the
// result: a TermScope term passes when this function returns false.
//
// OR-group convention (C flecs style): a TermAnd immediately followed by one
// or more TermOr terms is the first member of the OR-group; the group is
// satisfied if any member is present (entityHasTermInScope returns true for it).
func evalScopeSubTerms(w *World, e ID, t *table.Table, sub []Term) bool {
	i := 0
	for i < len(sub) {
		term := sub[i]
		// C flecs OR-group: TermAnd followed by TermOr is the group's first member.
		if term.Kind == TermAnd && i+1 < len(sub) && sub[i+1].Kind == TermOr {
			groupSatisfied := entityHasTermInScope(w, e, t, term)
			i++
			for i < len(sub) && sub[i].Kind == TermOr {
				if entityHasTermInScope(w, e, t, sub[i]) {
					groupSatisfied = true
				}
				i++
			}
			if !groupSatisfied {
				return false
			}
			continue
		}
		switch term.Kind {
		case TermAnd:
			if !entityHasTermInScope(w, e, t, term) {
				return false
			}
		case TermNot:
			if entityHasTermInScope(w, e, t, term) {
				return false
			}
		case TermScope:
			// Nested scope: evaluate inner sub-expression and negate.
			if evalScopeSubTerms(w, e, t, term.Sub) {
				return false // NOT(inner)
			}
		case TermEq:
			if e != term.ID {
				return false
			}
		case TermNotEq:
			if e == term.ID {
				return false
			}
		case TermNameMatch:
			name, ok := w.GetName(e)
			if !ok {
				return false
			}
			if !substrMatchCaseInsensitive(name, term.Pattern) {
				return false
			}
		case TermOptional, TermOr:
			// Optional: no effect on matching.
			// Degenerate TermOr without a preceding TermAnd: no-op.
		}
		i++
	}
	return true
}

// validateAndSortTerms validates terms for NewQueryFromTerms/NewCachedQueryFromTerms,
// builds OR-groups by scanning consecutive TermOr entries, copies and sorts terms
// (And first by ID, Not second by ID, Or-groups third preserving adjacency, Optional
// last by ID), applies inheritable auto-promotion (see applyInheritablePromotion),
// and returns the sorted terms, pre-extracted And-term IDs, OR-groups, and an
// alwaysFalse flag (set when an OrFrom source had no inheritable components).
// Panics with messages prefixed by caller on invalid input.
func validateAndSortTerms(w *World, caller string, terms []Term) ([]Term, []ID, [][]ID, bool) {
	if len(terms) == 0 {
		panic(caller + ": at least one term is required")
	}

	// Expand *From terms (TermAndFrom, TermOrFrom, TermNotFrom) into their inner terms
	// using a snapshot of the source entity's component list at construction time.
	// This runs BEFORE all other validation so expanded terms participate in normal
	// validation and sorting. Mirrors upstream compiler_term.c:1225-1251.
	alwaysFalse := false
	allFromInput := true // true iff every original term was a *From term
	{
		r := Reader{world: w}
		expanded := make([]Term, 0, len(terms))
		for _, t := range terms {
			switch t.Kind {
			case TermAndFrom, TermOrFrom, TermNotFrom:
				if !w.index.IsAlive(t.Src) {
					panic(fmt.Sprintf("%s: %s source entity %d is dead or non-existent", caller, t.Kind, t.Src))
				}
				comps := r.EntityComponents(t.Src)
				// Filter: exclude IDs with DontInherit policy (mirrors upstream
				// flecs_query_next_inheritable_id / EcsIdOnInstantiateDontInherit).
				filtered := comps[:0:0]
				for _, cid := range comps {
					if w.instantiatePolicies[cid]&policyOnInstantiateDontInherit == 0 {
						filtered = append(filtered, cid)
					}
				}
				switch t.Kind {
				case TermAndFrom:
					for _, cid := range filtered {
						expanded = append(expanded, Term{ID: cid, Kind: TermAnd})
					}
				case TermNotFrom:
					for _, cid := range filtered {
						expanded = append(expanded, Term{ID: cid, Kind: TermNot})
					}
				case TermOrFrom:
					switch len(filtered) {
					case 0:
						// Empty disjunction = false; entire query produces zero results.
						alwaysFalse = true
					case 1:
						// Single-element OR degenerates to And (avoids a degenerate
						// one-element OR-group and keeps the hasAnd check satisfied).
						expanded = append(expanded, Term{ID: filtered[0], Kind: TermAnd})
					default:
						for _, cid := range filtered {
							expanded = append(expanded, Term{ID: cid, Kind: TermOr})
						}
					}
				}
			default:
				allFromInput = false
				expanded = append(expanded, t)
			}
		}
		terms = expanded
	}
	if alwaysFalse {
		return nil, nil, nil, true
	}

	// hasAnd check: require at least one TermAnd unless the query was built entirely
	// from *From terms (in which case expansion may have produced only Not/Or terms
	// or no terms at all — all are valid "pure-From" semantics).
	hasAnd := false
	for _, t := range terms {
		if t.Kind == TermAnd {
			hasAnd = true
			break
		}
	}
	if !hasAnd && !allFromInput {
		panic(caller + ": at least one TermAnd term is required; a query with only Not/Optional/Or terms would match an unbounded entity set")
	}

	// Validate equality terms (TermEq, TermNotEq, TermNameMatch): cannot have
	// traversal flags or a fixed source (mirrors upstream validator.c:442-474).
	for i, t := range terms {
		switch t.Kind {
		case TermEq, TermNotEq, TermNameMatch:
			if t.Src != 0 {
				panic(fmt.Sprintf("%s: equality term at index %d cannot have a fixed source (TermEq/TermNotEq/TermNameMatch are always $this-bound)", caller, i))
			}
			if t.Traverse != TraverseSelf && t.Traverse != TraverseExplicitSelf && t.Traverse != 0 {
				panic(fmt.Sprintf("%s: equality term at index %d cannot be combined with traversal flags (Up/SelfUp/Cascade)", caller, i))
			}
		}
	}

	// Validate fixed-source terms (top-level and inside scope Sub).
	for i, t := range terms {
		if t.Kind == TermScope {
			for j, sub := range t.Sub {
				if sub.Src == 0 {
					continue
				}
				if !w.index.IsAlive(sub.Src) {
					panic(fmt.Sprintf("%s: scope fixed-source sub-term at index [%d][%d] (id %d) has a dead or non-existent source entity %d", caller, i, j, sub.ID, sub.Src))
				}
			}
			continue
		}
		if t.Src == 0 {
			continue
		}
		if t.Kind == TermNot {
			panic(fmt.Sprintf("%s: fixed-source term at index %d (id %d) cannot be TermNot (not supported in this phase)", caller, i, t.ID))
		}
		if t.Kind == TermOr {
			panic(fmt.Sprintf("%s: fixed-source term at index %d (id %d) cannot be TermOr (not supported in this phase)", caller, i, t.ID))
		}
		if t.Traverse != TraverseSelf && t.Traverse != TraverseExplicitSelf && t.Traverse != 0 {
			panic(fmt.Sprintf("%s: fixed-source term at index %d (id %d) cannot be combined with traversal flags (Up/SelfUp/Cascade)", caller, i, t.ID))
		}
		if !w.index.IsAlive(t.Src) {
			panic(fmt.Sprintf("%s: fixed-source term at index %d (id %d) has a dead or non-existent source entity %d; queries iterate using the source's table, so the source must be a live entity slot", caller, i, t.ID, t.Src))
		}
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

	// Check for cross-kind duplicate IDs. TermScope and equality terms are skipped:
	// TermScope has ID=0 and sub-terms live in a separate namespace; equality terms
	// (TermEq, TermNotEq, TermNameMatch) reference entity IDs / patterns, not
	// component IDs subject to the uniqueness constraint.
	seen := make(map[ID]struct{}, len(terms))
	for _, t := range terms {
		if t.Kind == TermScope || t.Kind == TermEq || t.Kind == TermNotEq || t.Kind == TermNameMatch {
			continue
		}
		if _, dup := seen[t.ID]; dup {
			panic(fmt.Sprintf("%s: duplicate term ID %d; each ID may appear at most once across all term kinds", caller, t.ID))
		}
		seen[t.ID] = struct{}{}
	}

	// Build sorted term list: And (by ID), Not (by ID), Or-groups (group order,
	// within-group by ID), Eq/NotEq/NameMatch (in original order), Optional (by ID),
	// Scope (in original order).
	// Within the And-block, fixed-source TermAnd terms come first (parallels
	// upstream's plan-order: setfixed ops precede $this-bound TermAnd ops).
	// Scope and equality terms are appended after Or-groups in original order so that
	// per-entity evaluation is independent of archetype sorting.
	var fixedSrcAndTerms, normalAndTerms, notTerms, eqTerms, optTerms, scopeTerms []Term
	for _, t := range terms {
		switch t.Kind {
		case TermAnd:
			if t.Src != 0 {
				fixedSrcAndTerms = append(fixedSrcAndTerms, t)
			} else {
				normalAndTerms = append(normalAndTerms, t)
			}
		case TermNot:
			notTerms = append(notTerms, t)
		case TermEq, TermNotEq, TermNameMatch:
			eqTerms = append(eqTerms, t)
		case TermOptional:
			optTerms = append(optTerms, t)
		case TermScope:
			scopeTerms = append(scopeTerms, t)
		}
	}
	sort.Slice(fixedSrcAndTerms, func(i, j int) bool { return fixedSrcAndTerms[i].ID < fixedSrcAndTerms[j].ID })
	sort.Slice(normalAndTerms, func(i, j int) bool { return normalAndTerms[i].ID < normalAndTerms[j].ID })
	andTerms := append(fixedSrcAndTerms, normalAndTerms...)
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
	cp = append(cp, eqTerms...)
	cp = append(cp, optTerms...)
	cp = append(cp, scopeTerms...)

	// Apply inheritable auto-promotion: any And/Optional term whose component was
	// marked inheritable and whose traversal is still the default zero gets
	// promoted to SelfUp(IsA). Terms with explicit traversal (Self/Up/SelfUp/
	// Cascade) are unaffected. Mirror of C flecs validator.c:766-770.
	// Also recurse into scope Sub terms (sub-terms are not sorted but do get
	// inheritable promotion applied).
	for i := range cp {
		if cp[i].Kind == TermScope {
			for j := range cp[i].Sub {
				applyInheritablePromotion(w, &cp[i].Sub[j])
			}
			continue
		}
		applyInheritablePromotion(w, &cp[i])
	}

	// Mark sparse/DontFragment/Union routing hints. Must happen after sorting so
	// that cp is final. Also recurse into scope Sub terms so that
	// isScopeTableSimple and evalScopeSubTerms can use the flags.
	for i := range cp {
		if cp[i].Kind == TermScope {
			for j := range cp[i].Sub {
				cp[i].Sub[j].Sparse = isSparseTermID(w, cp[i].Sub[j].ID)
				cp[i].Sub[j].DontFragment = isDontFragmentTermID(w, cp[i].Sub[j].ID)
				cp[i].Sub[j].Union = isUnionTermID(w, cp[i].Sub[j].ID)
			}
			continue
		}
		cp[i].Sparse = isSparseTermID(w, cp[i].ID)
		cp[i].DontFragment = isDontFragmentTermID(w, cp[i].ID)
		cp[i].Union = isUnionTermID(w, cp[i].ID)
	}

	// Traversable enforcement: any term whose Trav is non-zero requires the
	// traversal relationship to be registered as Traversable. Mirrors C
	// query/validator.c:639-647. The check is unconditional on depth (if
	// term.Trav != 0, we require Traversable regardless of depth).
	for _, t := range cp {
		if t.Trav == 0 {
			continue
		}
		if t.Traverse == TraverseSelf || t.Traverse == TraverseExplicitSelf {
			continue
		}
		if !w.traversablePolicies[ID(t.Trav.Index())] {
			modifier := ".Up()"
			switch t.Traverse {
			case TraverseSelfUp:
				modifier = ".SelfUp()"
			case TraverseCascade:
				modifier = ".Cascade()"
			}
			panic(fmt.Sprintf(
				"%s: cannot use non-traversable relationship %v with %s; call SetTraversable(w, relID) first",
				caller, t.Trav, modifier,
			))
		}
	}

	// Extract And-term IDs for backward compat (Terms() / andIDs).
	// Only non-fixed-source TermAnd terms are included (fixed-source terms don't
	// participate in archetype matching and are not meaningful as plain IDs).
	var andIDs []ID
	for _, t := range andTerms {
		if t.Src == 0 {
			andIDs = append(andIDs, t.ID)
		}
	}

	return cp, andIDs, orGroups, false
}
