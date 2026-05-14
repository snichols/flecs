## Goal

Port the upstream C flecs **fixed per-term source** mechanism to Go flecs. Today every query term implicitly binds to `$this` — the iterated entity. After this phase, a term can bind to a *specific named entity* instead. The motivating use case is the singleton-on-query pattern:

```go
// SimTime lives on the global `game` entity.
// We want movement systems that read SimTime per-tick alongside
// per-entity Position/Velocity.
q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.With(velID),
    flecs.WithSourceTerm(simTimeID, game), // bound to `game`, not $this
)
```

The query iterates entities that have Position + Velocity (driven by `$this`), and for each match the `SimTime` field is bound to the value stored on `game`. The fixed-source term does **not** add to the `$this` archetype-filter set — it is a one-time component fetch.

This is the query-side equivalent of Phase 16.12 fixed-source observers (`observer_fixed_source_test.go`). It closes the `docs/README.md:108` gap.

After this phase: `docs/README.md:108` flips to shipped (v0.73.0).

### Why this naming matters

The existing observer-side fixed-source builder is `flecs.WithSource(e ID) ObserverOptions` (single-arg, returns `ObserverOptions`). The issue's originally-proposed `flecs.WithSource(componentID, sourceEntity ID) Term` would collide on name and confuse callers. The implementation should pick a distinct identifier — recommended: `flecs.WithSourceTerm(componentID, sourceEntity ID) Term` (and matching `(Term).Source(e ID) Term` chained-builder on the existing Term). State the chosen name in the v0.73.0 CHANGELOG entry. Confirm at implementation time whether the existing observer `WithSource` should be renamed for symmetry (e.g., to `OnSource`); if so, mark the old name deprecated rather than removed.

### C upstream references (cited line numbers)

- `ecs_term_ref_t` and the `EcsIsEntity` flag — `include/flecs.h:778` (`EcsIsEntity` (1<<57)), `include/flecs.h:796` (`EcsTermRefFlags` mask), `include/flecs.h:799-811` (`ecs_term_ref_t` struct), `include/flecs.h:820` (`ecs_term_t::src`).
- `ecs_term_match_this` predicate — `include/flecs.h:4643-4659` (a term has `$this` source when `src.id is EcsThis` and `src.flags is EcsIsVariable`).
- Compiler hot spot — `src/query/compiler/compiler.c:833-882` (`flecs_query_insert_fixed_src_terms`): fixed-source terms are emitted **before** non-fixed terms; `src/query/compiler/compiler.c:934-945` inserts a single `EcsQuerySetFixed` op at the head of the plan whenever any term has a fixed source; `src/query/compiler/compiler.c:872` (`term->src.id & EcsIsEntity && ECS_TERM_REF_ID(&term->src)`) is the gate.
- The compile-per-term path — `src/query/compiler/compiler_term.c:519-522` (`EcsQueryIsEntity` flag set on the op); `src/query/compiler/compiler_term.c:1267-1272` (`!ECS_TERM_REF_ID(&term->src) && term->src.id & EcsIsEntity` → `flecs_query_compile_0_src` for 0-source terms — out of scope for this port).
- Iterator hot spot — `src/query/engine/eval.c:940-962` (`flecs_query_setfixed`): runs once per query execution; populates `it->sources[term->field_index] = ECS_TERM_REF_ID(src)` for every fixed-source term. Subsequent `flecs_query_with` invocations call `flecs_query_get_table(op, &op->src, EcsQuerySrc, ctx)` (`src/query/engine/eval.c:100`) and resolve the column via `flecs_component_get_table` (`src/query/engine/eval.c:114-117`); on miss the call returns `false` and the term yields no rows.
- Source-not-found semantic — verified at `src/query/engine/eval.c:114-117`: if the fixed source has no `ecs_component_record_t` table-record for the term's id, `flecs_query_with` returns `false`. This propagates up the plan: **the entire query yields zero results.** This is the documented upstream behaviour and the recommended Go semantic.
- Validator reset for 0-source — `src/query/validator.c:288-292` (when `ECS_TERM_REF_ID(src) == 0 && src->id & EcsIsEntity`, traversal flags are stripped). Not directly needed for the Go port (we explicitly forbid zero source).

### Archetype-filter interaction (verified)

A fixed-source term does **not** contribute to the `$this` archetype-filter set. The compiler emits a separate `setfixed → with(src=fixed)` op pair that runs once at iter start and does not seed table iteration. The remaining `$this`-bound `TermAnd` terms are the sole drivers of table selection. This is what makes the singleton-on-query pattern free: the `SimTime` term does not constrain the matched-table set.

## Constraints

- @docs/README.md — gap entry at line 108 to flip to shipped (v0.73.0). Same line also notes that `$this` is currently the only supported source.
- @docs/Queries.md — line 974 (Not Yet Ported § Fixed per-term source) to remove; add new § Fixed per-term source in the body documenting the API and the no-archetype-filter semantic.
- @query.go — `Term` struct (lines 101-123): add a new field for the fixed source. Recommend `Source ID` (zero = `$this`, default). Update `validateAndSortTerms` (`@query.go:1310-1455`) so that fixed-source terms (a) panic on `Source == 0` is unambiguous since zero means $this — the validation is "panic if user-supplied source ID does not refer to an alive entity"; (b) do NOT participate in archetype-seed selection (see lines 485-506 of `@query.go`, `for i, term := range q.terms { ... compIndex.Count }`); (c) do NOT contribute to `matchesTable` archetype-presence checks (lines 887-957). Sorting: keep fixed-source TermAnd at the head of the And-block so the iterator can resolve them up-front (parallels upstream's plan-order).
- @query.go — `QueryIter.Iter()` (lines 358-530): when constructing the iter, materialise the fixed-source pointer set once. Suggested data path: an iter-local `fixedSourcePtrs map[ID]unsafe.Pointer` populated at construction by looking up `world.index.Get(sourceEntity).Table.Get(row, termID)` (and the sparse-set / union-store equivalents for sparse / DontFragment / union pair terms). If any fixed-source term has no value on its source, the iter is short-circuited and `Next()` returns false immediately — matches upstream semantic.
- @query.go — `Field[T]` (lines 1094-1144): inspect terms; if the term has a non-zero Source, return a 1-element `[]T` backed by the cached pointer (parallels the sparse path at line 1097-1109). The pointer is valid for the lifetime of the iter — document the snapshot-at-iter-start contract on the new field.
- @cached_query.go — `NewCachedQuery` (lines 163-186) and `NewCachedQueryFromTerms` (line 207): mirror the validation and store the fixed-source set on the cached query. Cached queries snapshot the source pointer freshly at each `Iter()` call (not at construction), so an updated source between executions is visible on the next iteration. Document this.
- @observer_fixed_source_test.go — Phase 16.12 reference; reuse the source-validation pattern and the SimTime/Game motivating example for tests.
- @observer_options.go — existing `WithSource(e ID) ObserverOptions` (line 32). State explicitly in the v0.73.0 CHANGELOG that the new query-side helper does NOT reuse this name; recommend `WithSourceTerm` to avoid the collision.
- @singleton.go — Singleton policy already exists; the canonical fixed-source example should construct `SimTime` as a singleton (`SetSingleton(w, simTimeID)`) and bind the query term to the resolved holder entity. Reference this pattern in `docs/Queries.md`.
- @CHANGELOG.md — v0.72.0 just shipped (line 3); add a new v0.73.0 entry at the top.
- @ROADMAP.md — line 3 (`## Shipped (through v0.72.0)`) bumps to v0.73.0.

## Deliverables

1. `Term` struct change in `@query.go`:
   - Add `Source ID` field; zero means `$this` (existing behaviour, default).
   - Add `(Term).Source(e ID) Term` chained builder: panics on `e == 0`, returns a copy with `Source` set.
   - Add top-level builder `WithSourceTerm(componentID, sourceEntity ID) Term` returning a complete `TermAnd` with the source bound. Panics on either ID being zero.

2. Validation in `@query.go:validateAndSortTerms`:
   - For each term with non-zero `Source`: panic if `!w.index.IsAlive(t.Source)` (the source entity must exist at query-construction time). State the rationale: queries iterate using the source's table, so the source must be a live entity slot.
   - A fixed-source term may NOT be `TermNot` in this phase (state the limitation; upstream does support `!Component(src)` but it adds substantial validator complexity — out of scope).
   - Fixed-source `TermOr` is also disallowed in this phase (the OR-group model is `$this`-only).
   - `TermOptional` is supported: an absent component on the source yields `FieldMaybe[T] -> (nil, false)`. **This is a deliberate divergence from upstream**, which treats fixed-source-with-Optional uniformly with TermAnd. The Go port chooses the `FieldMaybe`-friendly behaviour so callers can express "match these entities, and optionally bind a config from `game`."

3. Query plan & iteration in `@query.go:Iter`:
   - Fixed-source terms do NOT participate in seed selection (line 485 loop) — they are pre-filtered out at the top of the loop.
   - Fixed-source terms do NOT contribute archetype-presence checks in `matchesTable` (line 887) — same pre-filter.
   - At iter construction, after seed selection, build `fixedSourcePtrs map[ID]unsafe.Pointer` by looking up each fixed-source term's value on its source entity:
     - Archetype term: `world.index.Get(source).Table.Get(row, termID)`.
     - Sparse term: `sparseSetGet(world, source, termID)`.
     - Union pair term: `world.unionStore[rel].dense[pos]` lookup by source.
     - DontFragment term: `sparseSetGet` (same as Sparse).
   - If any fixed-source `TermAnd` returns nil pointer → iter is dead (no candidates, `Next()` returns false). Match upstream `flecs_query_with` returning false at `src/query/engine/eval.c:114-117`.
   - For `TermOptional` fixed-source terms: cache nil as "absent"; `FieldMaybe` returns `(nil, false)`.

4. `Field[T]` / `FieldMaybe[T]` extension in `@query.go`:
   - Inspect terms; if `term.Source != 0`, return a 1-element slice over the cached pointer (no per-row lookup). Lifetime: the pointer is invalidated by the next `Iter()` call on the same query, not by `Next()`. Document the snapshot-at-iter-start contract.
   - For TraverseSelf clarity: a fixed-source term is implicitly `TraverseSelf` (no Up/SelfUp/Cascade composition). Panic if user combines `.Source(e)` with `.Up()/.SelfUp()/.Cascade()`.

5. Cached query support in `@cached_query.go`:
   - `NewCachedQuery` / `NewCachedQueryFromTerms` accept fixed-source terms unchanged.
   - Cached match-set computation: fixed-source terms do not alter the table cache (they don't change which `$this` tables match).
   - Snapshot timing: cached `Iter()` re-reads the source pointer each time (not cached on the CachedQuery), so updates to the source between iterations are visible on the next execution.

6. Tests in `query_fixed_source_test.go` (≥ 10 cases, mirror `@observer_fixed_source_test.go` style):
   - `TestFixedSourceBasic`: `With(Position).WithSourceTerm(SimTime, game)` where `game` has `SimTime`: iterates entities with Position; SimTime field is bound to game's value across iterations.
   - `TestFixedSourceNoComponentOnSourceYieldsZero`: same query but `game` lacks SimTime → 0 matches. Asserts upstream semantic.
   - `TestFixedSourceMixedThisAndFixed`: `With(Pos).With(Vel).WithSourceTerm(SimTime, game)` → iterates Pos+Vel entities; SimTime constant per iteration.
   - `TestFixedSourceSnapshotAtIterStart`: mutate source during iteration; verify the iteration sees the snapshot (not the mutated value). Document the cached-pointer contract.
   - `TestFixedSourceSparse`: source's term is a Sparse component → sparse-set lookup at iter start.
   - `TestFixedSourceDontFragmentPair`: source's term is a DontFragment-pair component → sparse-set lookup by source.
   - `TestFixedSourceZeroSourcePanics`: `WithSourceTerm(c, 0)` panics at construction.
   - `TestFixedSourceDeadEntityPanics`: `WithSourceTerm(c, deletedEntity)` panics at construction (validates via `index.IsAlive`).
   - `TestFixedSourceCachedQuerySourceUpdate`: `NewCachedQueryFromTerms` with fixed-source term; mutate source between executions; second `Iter()` sees the new value.
   - `TestFixedSourceOrderByThis`: `OrderBy[T]` on a `$this` component + fixed-source term; sort works on `$this`, fixed-source is constant.
   - `TestFixedSourceMultipleSources`: two fixed-source terms with different sources (`SimTime` on `game`, `Difficulty` on `player`) — both bind correctly.
   - `TestFixedSourcePairForm`: `WithSourceTerm(Pair(R, T), game)` where game has `(R, T)`.
   - `TestFixedSourceOptional`: `Maybe` semantic for fixed-source — absent component yields `FieldMaybe -> (nil, false)`, not zero results. Documents the divergence from upstream.
   - Coverage: ≥ 95.0% on `query.go` and `cached_query.go`.

7. Docs (per CONTRIBUTING.md):
   - `docs/Queries.md` — new § "Fixed per-term source" before § Not Yet Ported. Use the `SimTime` singleton example; explain (a) the fixed-source term does NOT add to the archetype filter, (b) the snapshot-at-iter-start contract, (c) the source-missing → zero-results semantic, (d) the Optional divergence.
   - `docs/Queries.md` — remove line 974 (Not Yet Ported entry).
   - `docs/README.md` — flip line 108 to ✅ shipped (v0.73.0) with API reference.
   - `README.md` — feature-list bump if it carries a query-features section.
   - `CHANGELOG.md` — v0.73.0 entry at top documenting `WithSourceTerm`, the `(Term).Source(e)` builder, the chosen naming rationale (no collision with observer-side `WithSource`), and the snapshot-at-iter-start contract.
   - `ROADMAP.md` — header bump to "Shipped (through v0.73.0)".

## Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%
- No regression on existing query tests (`query_test.go`, `query_terms_test.go`, `cached_query_test.go`, `query_sort_test.go`, `query_group_test.go`, `query_filters_test.go`).

## Explicit non-goals

- No query variables `$Var` (separate gap: `docs/README.md:109`).
- No `Source` as an iterator-dynamic value — `Source` is fixed at term construction.
- No multi-source terms — a term has at most one source (zero = $this).
- No `TermNot` with fixed source in this phase (defer; upstream supports it but with validator complexity).
- No `TermOr` with fixed source in this phase.
- No 0-source terms (component IDs as plain "carry-along" without entity binding) — out of scope; would map to upstream's `flecs_query_compile_0_src` at `src/query/compiler/compiler_term.c:1267-1272`.
- No renaming of the existing `flecs.WithSource(e ID) ObserverOptions` — kept as-is to avoid breakage. The new query helper is `WithSourceTerm`.

## Open decision points (state in CHANGELOG / Queries.md)

1. **Source-not-found behaviour**: query yields zero results (recommended; verified against upstream `eval.c:114-117`).
2. **Source mutation during iteration**: snapshot at iter start (recommended; matches upstream's `it->sources[]` populated once by `flecs_query_setfixed`). Live changes are out of scope.
3. **Cached pointer validity**: per iteration of `Iter()` (recommended); cached query re-reads on each `Iter()`.
4. **Term struct change**: add `Source ID` field directly (recommended; keeps API surface lean). Reject a `TermSource` subtype.
5. **Naming**: `WithSourceTerm` (new) coexists with existing `WithSource` (observers). State explicitly in CHANGELOG.
6. **Optional + fixed source**: deliberate divergence from upstream — Go's `FieldMaybe` semantic is the natural fit; document.
7. **Dead-entity source**: panic at construction (Go-side divergence from observer's "register but never fire" behaviour). Rationale: queries are read-time, panic-on-construction surfaces the bug earlier; observers are write-time, soft-fail makes more sense there.

## Process

- Feature, not bug.
- Verify all `@`-references and line numbers before filing — done.
- Label: `snichols/queued`.
