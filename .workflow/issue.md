## Goal

Add OR query term support to the structured `Term` API. This is the last query gap from the original roadmap; after this lands, the only deferred query feature is query-term traversal modifiers (the existing `GetUp`/`HasUp`/`TargetUp` helpers cover most use cases).

Introduce a new `TermOr` kind and `Or(id)` constructor. A sequence of adjacent `Or(id)` terms in the `terms ...Term` slice forms an OR-GROUP. A table matches an OR-group if it has AT LEAST ONE of the group's ids. Multiple OR-groups in one query are each independent; non-Or terms break the group. An OR-group of size 1 is degenerate but allowed (behaves as `With`).

Example after this lands:

```go
type Sleeping struct{}
type Working struct{}
type Playing struct{}

sleepID := flecs.RegisterComponent[Sleeping](w)
workID  := flecs.RegisterComponent[Working](w)
playID  := flecs.RegisterComponent[Playing](w)
posID   := flecs.RegisterComponent[Position](w)

// Match all entities with Position AND (Sleeping OR Working OR Playing).
q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.Or(sleepID),
    flecs.Or(workID),
    flecs.Or(playID),
)

flecs.Each(q, func(it *flecs.QueryIter) {
    for it.Next() {
        positions := flecs.Field[Position](it, posID)
        // Any of Sleeping/Working/Playing may be present, in any combo.
        // Use FieldMaybe[Sleeping/Working/Playing] to disambiguate per entity.
        for i := range positions {
            // ...
        }
    }
})
```

### Semantics

- A sequence of adjacent `Or(id)` terms forms an OR-GROUP.
- A table matches an OR-group iff it has at least one of the group's ids.
- Multiple OR-groups in one query: each independent. `(With(A), Or(B), Or(C), Or(D), Or(E))` has ONE group `{B,C,D,E}`. `(Or(A), Or(B), With(X), Or(C), Or(D))` has TWO groups `{A,B}` and `{C,D}` (separated by `With(X)`).
- An OR-group of size 1 (a single `Or(x)`) is degenerate and behaves like `With(x)`. Document but don't error.
- Mixing `Or` with `With`/`Without`/`Maybe` in the same query is allowed. Adjacent Or-terms form a group; non-Or terms break the group.

### Deliverables

1. **Extend `TermKind` enum** in `query.go`:
   ```go
   const (
       TermAnd      TermKind = 0
       TermNot      TermKind = 1
       TermOptional TermKind = 2
       TermOr       TermKind = 3 // NEW
   )
   ```
   Add `(TermKind).String()` case for `\"Or\"`.

2. **New constructor:**
   ```go
   // Or returns a term that contributes to an OR-group. A table matches the
   // OR-group if it has at least one of the group's ids. Adjacent Or terms
   // form a single group; the group is broken by any non-Or term.
   func Or(id ID) Term { return Term{ID: id, Kind: TermOr} }
   ```

3. **Validation in `NewQueryFromTerms` / `NewCachedQueryFromTerms`:**
   - A query must still have at least one `TermAnd` (matches Phase 3.3 rule).
   - An OR-group of size 1 is degenerate: allowed but documented (behaves as `With`).
   - Duplicate ID across different kinds (And+Not, And+Optional, Or+Not, etc.): continue to panic per Phase 3.3.
   - Duplicate ID within an OR-group: `Or(A), Or(A)` panics with `\"duplicate id in OR-group\"`.
   - Empty / invalid OR term (`Or(0)` or any term whose `id.Index() == 0`): panic, matching Phase 3.3's hygiene checks.

4. **Internal OR-group representation:**
   - Add `orGroups [][]ID` (or equivalent `orGroup` struct) to `*Query` and `*CachedQuery`.
   - Built during construction by scanning `terms []Term` and grouping consecutive `TermOr` entries.
   - New sort order: And first, then Not, then Or-groups (preserving group adjacency), then Optional. Or-groups MUST keep their internal adjacency so the matcher recognizes them. Implementer's call whether to sort within each Or-group by id.

5. **Match algorithm extension:**
   - **Uncached `*Query`:**
     - Smallest-set seed selection uses And terms only (unchanged). Or-groups don't seed: their disjunction makes the candidate set potentially the union of multiple table sets.
     - For each candidate table from the seed:
       - And: must contain id.
       - Not: must NOT contain id.
       - Or-group: table must contain AT LEAST ONE id from the group (loop, first-match short-circuit).
       - Optional: no effect.
   - **Cached `*CachedQuery`:**
     - `tryMatchTable` applies the same predicate.
     - Cache stays correct because tables are immortal (Phase 6.1 invariant). New tables that match an Or-group are appended via `notifyTableCreated`.

6. **Per-iter Or-group presence:**
   - Like Optional presence (Phase 3.3) but for OR. The `QueryIter` caches per-table the set of present ids per Or-group so callers using `FieldMaybe[T]` on Or-group ids see which ids are present.
   - **Or-group terms are queryable via `FieldMaybe[T]` only.** `Field[T]` on an Or-group id panics if the current table lacks it (matching Optional fail-loud semantics). Users are expected to use `FieldMaybe` to disambiguate. Document.

7. **`Query.Terms()` and `TermsFull()`:**
   - `Terms()` continues to return ONLY And-term IDs for backward compat.
   - `TermsFull()` returns all terms including Or terms.

8. **Tests** in `query_terms_test.go`:
   - Basic OR matching: entities with `Sleeping` only, `Working` only, `Playing` only, none. Query `[With(Pos), Or(Sleep), Or(Work), Or(Play)]`. Matches the first three but not the fourth.
   - Multiple OR-groups: `[Or(A), Or(B), With(X), Or(C), Or(D)]`. Two independent groups: entity must have (A or B) AND X AND (C or D).
   - Single-term OR-group: `[With(Pos), Or(A)]` behaves the same as `[With(Pos), With(A)]`.
   - Or + Not: `[With(Pos), Or(A), Or(B), Without(Dead)]` matches `Pos AND (A or B) AND NOT Dead`.
   - Or + Optional: `[With(Pos), Or(A), Or(B), Maybe(C)]` matches all `(A or B)` entities; `FieldMaybe[C]` reports presence.
   - No-And-with-only-Or panics: `[Or(A), Or(B)]`.
   - Duplicate-id-in-Or-group panics: `[With(Pos), Or(A), Or(A)]`.
   - Cross-kind duplicate panics: `[With(A), Or(A)]`.
   - Empty / invalid OR term `Or(0)` panics.
   - CachedQuery with Or: create entities with A or B; `NewCachedQueryFromTerms(With(Pos), Or(A), Or(B))`; verify cache contains matching tables; create a new entity with neither A nor B; verify cache does NOT grow.
   - `FieldMaybe` on Or-group id: entity with A but not B; `FieldMaybe[A]` returns `(slice, true)`; `FieldMaybe[B]` returns `(nil, false)`.
   - `Field` on Or-group id panics when absent: `Field[B]` on an entity that has A but not B (force `FieldMaybe` usage).
   - `TermsFull` includes Or terms in correct order.
   - (Optional) Two-step round-trip via JSON: marshal a world, unmarshal, and verify the Or query results match before-and-after. Sanity-check that JSON addons don't regress with the new kind.
   - Phase 3.3 tests stay green.

9. **Documentation:**
   - Update `NewQueryFromTerms` godoc with an OR example.
   - Update `doc.go`'s structured-terms section.
   - Update CHANGELOG (Unreleased section: `Phase 9.4: OR query terms`).
   - Update README feature list.

10. **Mechanical acceptance:**
    - `go test ./... -race -count=2` passes.
    - `go vet ./...` clean.
    - `golangci-lint run` clean.
    - Coverage on `flecs` >= 90% (no regression from 96.3%).
    - All exported symbols have godoc.

### Non-goals

- NO query-term traversal modifiers (`up(rel)`, Cascade/Down).
- NO nested groups via `Group` field on `Term`.
- NO negative OR groups (`NOT (A OR B)` / NotGroup).
- NO wildcards.
- NO predicate / custom matcher terms.
- NO change to existing `TermAnd` / `TermNot` / `TermOptional` semantics.
- NO change to `Each1` / `Each2` / `Each3` / `Each4` (they stay AND-only).
- NO change to `Field[T]` semantics for And/Not/Optional terms.

### Implementation pointers

- Adjacent-Or-grouping during validation: a single forward pass over `terms []Term` builds `[]orGroup` as a side effect. Break the current group when a non-Or term is encountered; start a new group on the next Or term.
- Or-groups can be stored on `*Query` and `*CachedQuery` as `[][]ID` (group -> ids). The per-iter presence cache can be `[]bool` per group (one boolean per id) or `[][]bool` (group-indexed).
- The cache-invalidation invariant from 6.1 still holds: tables are immortal, so a cached Or-query's matchset only grows over time.
- The smallest-seed strategy operates on And terms only; Or-groups never seed.
- DO NOT modify the public API of Phase 3.3 / 9.2 / 9.3 features.
- DO NOT introduce a new public type beyond the `Or` constructor and `TermOr` constant.
- DO NOT change the JSON format (queries are not serialized).
- DO NOT import third-party deps.

### Master starting point

- Branch: `master` HEAD `33608ee` (tagged `v0.5.0`).
- The structured `Term` API (Phase 3.3) already supports `With` (AND), `Without` (NOT), and `Maybe` (Optional). OR is the last query gap from the original roadmap.

## Constraints

- @query.go - extend `TermKind` enum with `TermOr`, add `Or(id)` constructor, extend validation and matching in `NewQueryFromTerms`, add `orGroups` storage on `*Query`, extend per-iter presence cache, and update `TermKind.String()` plus `TermsFull()`.
- @cached_query.go - extend `NewCachedQueryFromTerms` validation, `orGroups` storage, and `tryMatchTable` predicate; rely on Phase 6.1 immortal-table invariant for `notifyTableCreated`.
- @query_terms_test.go - add all new test cases (basic OR, multi-group, single-term group, Or+Not, Or+Optional, panics, CachedQuery, FieldMaybe vs Field, TermsFull ordering); existing Phase 3.3 tests must stay green.
- @world.go - reference for table layout and table-set lookup used by the matcher.
- @meta.go - reference for component metadata used by Field/FieldMaybe.
- @id.go - reference for `ID.Index()` sentinel check used to reject `Or(0)`.
- @doc.go - extend the structured-terms section with OR documentation and example.
- @CHANGELOG.md - add `Phase 9.4: OR query terms` under Unreleased.
- @README.md - add OR to the feature list.
- C reference (cite, do not paraphrase): `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` (search `EcsOr`), `/work/agents/claude/projects/SanderMertens/flecs/src/query/util.c` (term validation for OR groups), `/work/agents/claude/projects/SanderMertens/flecs/src/query/engine/eval_iter.c` (OR-group iteration logic).
