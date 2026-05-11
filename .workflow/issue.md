## Goal

Add NOT and Optional term kinds to the query system so users can express patterns like "entities with Position but not Dead" or "entities with Position and optionally Velocity." This is one of the most common ECS query patterns and the gap is currently user-visible. Pure additive: the existing `NewQuery(ids...)` AND-only signature stays unchanged.

After this lands:

```go
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)
deadID := w.NewEntity()  // a tag

// NOT term: entities with Position but NOT Dead.
q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.Without(deadID),
)
flecs.Each(q, func(it *flecs.QueryIter) {
    for it.Next() {
        positions := flecs.Field[Position](it, posID)
        for i := range positions {
            // process living entities with positions
        }
    }
})

// Optional term: entities with Position; Velocity is optional.
q2 := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.Maybe(velID),
)
flecs.Each(q2, func(it *flecs.QueryIter) {
    for it.Next() {
        positions := flecs.Field[Position](it, posID)
        // FieldMaybe returns ([]T, true) if column present, (nil, false) if absent.
        velocities, hasVel := flecs.FieldMaybe[Velocity](it, velID)
        for i := range positions {
            if hasVel {
                positions[i].X += velocities[i].X
            }
        }
    }
})
```

**This phase does NOT implement:**
- OR terms.
- Wildcards (`MakePair(rel, *)` patterns).
- Up/Down traversal modifiers (Phase 6.2).
- Each1/Each2/Each3/Each4 helpers for Not/Optional — users use Query directly.
- Predicate/filter terms (custom matchers).
- Term ordering hints beyond the existing sort-by-ID.
- Change detection.

## Deliverables

### 1. New types in `query.go`

```go
// TermKind enumerates how a query term participates in matching.
type TermKind int
const (
    TermAnd      TermKind = 0 // term ID must be in the table's signature.
    TermNot      TermKind = 1 // term ID must NOT be in the table's signature.
    TermOptional TermKind = 2 // term doesn't affect matching; Field returns absent flag.
)

// Term is a structured query term combining a component/pair/tag ID with a TermKind.
type Term struct {
    ID   ID
    Kind TermKind
}

// Term constructors:
func With(id ID) Term       // returns Term{id, TermAnd}
func Without(id ID) Term    // returns Term{id, TermNot}
func Maybe(id ID) Term      // returns Term{id, TermOptional}
```

### 2. New constructor

- `func NewQueryFromTerms(w *World, terms ...Term) *Query`
- Validates: `w != nil`, `len(terms) >= 1`. Panic on empty.
- **Validates that at least one term is `TermAnd`** — a query with only Not/Optional terms is degenerate (matches everything not excluded, infinitely large in principle). Panic on no-And-terms with a clear message. Document.
- **Validates no duplicate IDs across terms.** A term like `(With(P), Without(P))` is contradictory; panic. Document.
- Stores a copy of the terms; sorts AND terms first (by ID), then Not terms, then Optional terms (implementer's call on internal layout — document).

### 3. Existing `NewQuery(ids ...ID)` unchanged signature

- Internally now builds `[]Term` with all `TermAnd` and calls the same internal logic. No behavior change for existing callers.
- `Query.Terms() []ID` continues to return ONLY the AND-term IDs for backward compatibility. Document. Add a new method `(*Query).TermsFull() []Term` that returns the full term list (copy or read-only).

### 4. Match algorithm in `*QueryIter`

- **For uncached `*Query`:** the existing smallest-set seed strategy uses the smallest AND term's table set. The inner filter must:
  - For each candidate table from the seed:
    - For every And term (other than the seed): `table.HasComponent(term.ID)` must be true.
    - For every Not term: `table.HasComponent(term.ID)` must be false.
    - Optional terms do NOT affect matching.
- **For cached `*CachedQuery`:** `tryMatchTable(t)` must implement the same predicate. The `notifyTableCreated` path uses this; verify the cache stays correct.

### 5. Per-iter presence tracking for Optional terms

- When `Next()` advances to a new table, compute and cache which Optional-term IDs are present in that table.
- `QueryIter` gains an internal `optionalPresent map[ID]bool` (or a small array if the term count is bounded) — implementer's call on data structure.
- The presence-check happens once per table transition, not per `Field[T]` call.

### 6. New typed field accessor for Optional terms

- `func FieldMaybe[T any](it *QueryIter, id ID) ([]T, bool)`:
  - Looks up `id` in the iter's term list. If `id` is not an Optional term, panic with a clear message ("FieldMaybe requires an Optional term; use Field for And terms").
  - If the current table has `id`, returns `(slice, true)` — same path as `Field[T]`.
  - If the table doesn't have `id`, returns `(nil, false)`.
  - DO NOT panic on missing column for Optional terms (that's the WHOLE point of Optional).
- Keep `Field[T]` unchanged: it panics on missing ID. Users must use `FieldMaybe` for Optional terms.

### 7. Documentation

- Update godocs on `NewQuery`, `NewQueryFromTerms`, `Field`, `FieldMaybe`, `TermKind`, `Term`, `With`, `Without`, `Maybe`.
- Add a new section to `doc.go` describing structured terms with a code snippet.
- Update README.md feature index to mention NOT/Optional support.

### 8. Tests in `query_terms_test.go` (new file)

- **`With` term kind:** `NewQueryFromTerms(w, With(posID))` matches all entities with Position.
- **`Without` term:** `NewQueryFromTerms(w, With(posID), Without(deadID))` matches entities with Position but NOT Dead.
- **`Maybe` term:** `NewQueryFromTerms(w, With(posID), Maybe(velID))` matches all entities with Position, regardless of Velocity. `FieldMaybe[Velocity](it, velID)` returns `(slice, true)` for tables with Velocity, `(nil, false)` for tables without.
- **Mixed Not + Optional + And:** all three kinds in one query, verify matching.
- **Multiple Not terms:** `Without(A), Without(B), With(C)` — only entities with C, neither A nor B.
- **Multiple Optional terms:** `With(P), Maybe(V), Maybe(M)` — all P entities visited; FieldMaybe per optional works.
- **NewQuery (legacy) unchanged:** existing AND-only tests pass.
- **No-And-terms panics:** `NewQueryFromTerms(w, Without(P))` panics.
- **Duplicate-ID panics:** `NewQueryFromTerms(w, With(P), Without(P))` panics.
- **FieldMaybe on And term panics:** `FieldMaybe[T](it, andTermID)` panics with clear message.
- **Field on Optional term panics if missing:** `Field[T](it, optionalTermID)` on a table without T panics (use FieldMaybe instead). Or alternatively: Field works for Optional terms that ARE present in the table. Choose and document.
- **CachedQuery with Not/Optional:** `NewCachedQueryFromTerms`? Or just `NewQueryFromTerms` + `Cache(q)` adapter? **Decide:** add `func NewCachedQueryFromTerms(w *World, terms ...Term) *CachedQuery` for consistency. Verify it integrates with the existing tryMatchTable + notifyTableCreated paths.
- **Migration adds/removes from a cached Not query:** create cached query [With(P), Without(D)]; create entity with P+D; verify not matched. Remove D; verify migration causes cache to add the entity's table. Add D back; verify cache removes it. (This is the trickiest: Not-term cache invalidation now responds to migrations that REMOVE a component, not just ADD.)

### 9. Cache invalidation for Not terms

- The current `notifyTableCreated` only fires on NEW table creation. For Not terms, a cached query could become stale when an EXISTING table loses a component — but that's not actually how archetype storage works. When you remove a component from an entity, the entity MIGRATES to a different table (which may already exist or be newly created). The original table just loses an entity but its signature is unchanged.
- So Not-term cache invalidation is actually simpler than it seems: cached match sets are keyed by TABLE, and tables are immortal. A Not-query's cache is correct as long as the predicate `table.HasComponent(notID) == false` was evaluated correctly at the time of cache addition.
- **Verify:** when a new table is created via migration (e.g., `[P]` migrating to `[P,D]` creates the `[P,D]` table), and a cached query has `With(P), Without(D)`, `tryMatchTable([P,D])` correctly returns false. The `[P]` table remains in the cache. No stale entries.
- Document this in code comments.

### 10. Mechanical acceptance

- `go test ./... -race -count=2` passes.
- `go vet ./...` clean.
- `golangci-lint run` clean.
- Coverage on `flecs` >= 90% (no regression from 97.0%).
- All exported symbols have godoc.

## Non-goals

- NO OR terms.
- NO wildcards.
- NO Up/Down traversal modifiers.
- NO Each1/Each2/Each3/Each4 helpers for Not/Optional. Users use NewQueryFromTerms.
- NO change-detection.
- NO predicate/custom-filter terms.
- NO removal of existing `NewQuery(ids...)` or `Query.Terms()` — pure additions.

## Implementation pointers

- Read `src/query/engine/eval_iter.c` for how the C version handles term kinds during iteration. We mirror only the And/Not/Optional logic; ignore the multi-term-relationship machinery.
- The internal term storage in `Query` and `CachedQuery` widens from `[]ID` to `[]Term`. Existing `Terms() []ID` continues to filter to And-only for backward compat. New `TermsFull() []Term` exposes the structured list.
- The `seed` selection for iteration should consider ONLY And terms — Not and Optional terms can't seed the iteration (Not terms have no candidate set; Optional terms include all tables).
- For `Field[T]` on an Optional term: implementer's call. Options: (a) panic if missing on this table (force FieldMaybe usage); (b) return empty slice if missing. Recommend (a) — fail-loud for correctness; users must use FieldMaybe for Optional terms.
- The per-table Optional-presence cache: simplest is a `map[ID]bool` recomputed on each `Next()`. For performance, a small fixed array works if Optional term count is bounded.
- DO NOT modify Each1/Each2/Each3/Each4 — they stay AND-only.
- DO NOT modify the existing public Query/QueryIter/Field[T] signatures.
- DO NOT import third-party deps.

## Context

Master HEAD `7fc1d6c` (tagged `v0.1.0`). The ECS is feature-complete and benchmarked. Current query surface:
- `NewQuery(w *World, ids ...ID) *Query` — AND terms only.
- `*Query.Terms() []ID`, `*Query.Iter()`, `*Query.Each(fn)`.
- `*QueryIter.Next/Table/Count/Entities/Query`.
- `Field[T any](it *QueryIter, id ID) []T`.
- `NewCachedQuery(w, ids ...ID) *CachedQuery` with same iteration shape.
- `Each1`/`Each2`/`Each3`/`Each4` typed convenience.

## Constraints

C reference (filesystem paths — cite, do not @-reference):
- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` — search `ecs_oper_kind_t` for the term-kind enum (EcsAnd / EcsOr / EcsNot / EcsOptional).
- `/work/agents/claude/projects/SanderMertens/flecs/src/query/util.c` — term validation.
- `/work/agents/claude/projects/SanderMertens/flecs/src/query/engine/eval_iter.c` — how the C version filters by term kind during iteration.

Go API the implementer builds on:
- @query.go
- @cached_query.go
- @each.go
- @world.go
- @id.go
- @internal/storage/table/table.go
- @internal/storage/componentindex/componentindex.go
