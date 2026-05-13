## Goal

Wire the Sparse component storage shipped in Phase 15.19 (v0.51.0) into the query layer so user queries can name a Sparse component (or a pair whose relationship/target is Sparse) in their terms and have those terms matched correctly. Today the trait, storage, and `EachSparse[T]` work, but query terms naming a Sparse component never match because the compiler and iterator assume archetype-table residency. After this phase the same query API — `NewQueryFromTerms`, `NewCachedQueryFromTerms`, `Iter`, `Each`, `Field`, `FieldMaybe` — works uniformly across archetype and sparse storage, with the archetype-only fast path preserved.

Target release: **v0.52.0**.

### Surface area

- **Query compilation** — when `NewQueryFromTerms` / `NewCachedQueryFromTerms` validate terms, inspect each term's component ID with `IsSparse(scope, componentID)`. If sparse, mark that term as sparse in the compiled plan so the iterator can route it. Wire through `validateAndSortTerms` (`@query.go:816`-ish) so the routing decision flows into both query variants.
- **Iterator construction** — extend `Query.Iter` (`@query.go:318`). Three cases:
  - **All-sparse query**: pick the smallest sparse-set as the driver (analogous to the existing smallest-archetype-seed heuristic at `@query.go:320`-`@query.go:344`); for each entity in it, check membership in every other sparse-set term.
  - **Mixed query**: walk archetype tables for the non-sparse terms as today; for each candidate entity, check sparse-set membership for each sparse term.
  - **All-archetype query**: unchanged — fast path preserved bit-for-bit.
- **Field/value access** — `Field[T]` / `FieldMaybe[T]` (`@query.go:665`, `@query.go:712`) gain a sparse branch: a sparse term reads through `sparseSetGet` (`@sparse.go:155`). Archetype terms keep column-by-row access.
- **Wildcard/Any pair handling with sparse components** — `Pair(R, Wildcard)` where R is sparse iterates all R-sparse entries and yields each `(R, T)` pair found; `Pair(Wildcard, T)` where T is sparse follows the upstream C semantics in `eval_sparse.c:136`-`eval_sparse.c:170` (`flecs_query_sparse_select_wildcard`) and `eval_sparse.c:225`-`eval_sparse.c:265` (`flecs_query_sparse_select_all_wildcard_pairs`).
- **Traversal terms × sparse** — common cases only this phase. `Up(R)` / `SelfUp(R)` / `Cascade(R)` where R is sparse must walk sparse-set lookup at each parent step without panicking, but full traversal correctness for deeply-nested sparse relationships may defer to Phase 15.20.1 if iterator complexity grows. Document any such deferment in CHANGELOG + ROADMAP.
- **Optional and Not on sparse terms** — `Optional(C)` becomes a per-entity sparse-set check (matched ⇒ pointer, unmatched ⇒ zero/nil); `Not(C)` becomes a per-entity not-in-sparse-set check.
- **Cached query invalidation for sparse terms** — archetype caches invalidate on table transition; sparse-set mutations don't transition tables. Add a per-sparse-set version counter that the cache consults; on first `Iter` after a version bump, the cache re-evaluates the affected portion. Bumped on `sparseSetInsert` / `sparseSetRemove` (`@sparse.go:127`, `@sparse.go:170`).
- **Marshal round-trip** — queries aren't marshaled, but a cached query plan containing a sparse term must rebuild cleanly after world unmarshal. Add a round-trip test: construct cached query → marshal world → unmarshal → re-execute query → verify results.

### Open decision points (with recommendations)

1. **Cache invalidation strategy** — version counter on each sparse-set vs. opt-out from caching. **Recommend version counter**: keeps cached queries cheap, matches the existing change-detection pattern.
2. **Driver selection for pure-sparse queries** — smallest-sparse-set as driver. **Recommend smallest-driver**: parallels archetype-side smallest-seed heuristic at `@query.go:339`-`@query.go:343`; minimizes iteration cost; deterministic given current sizes.
3. **Field-access return type for sparse terms** — `*T` directly (parity with archetype) vs. `(value, ok)`. **Recommend `*T`**; `nil` signals absent. State and document.

### Test additions (extend `sparse_test.go`; do not create a new file)

At least 12 new cases:

1. Pure-sparse query, 3 sparse components, filter on all 3, verify entity yield order and count.
2. Mixed query: 2 archetype + 2 sparse terms, verify intersection.
3. All-archetype regression: archetype-only query unchanged post-rewire.
4. Wildcard pair on sparse relationship: `Pair(SparseR, Wildcard)` yields every `(SparseR, T)`.
5. Not-term on sparse: returns entities NOT in the sparse-set.
6. Optional-term on sparse: matched entities get value, unmatched get nil/zero.
7. Field-access pointer correctness: returned pointer valid until next iterator advance.
8. Mutation through field-pointer on sparse term: visible in subsequent `EachSparse` and `GetID`.
9. Cached query with one sparse term: re-execution after sparse-set mutation reflects new state (validates version counter).
10. Empty sparse-set: query yields zero results, no panic.
11. Smallest-driver heuristic: sparse term A has 5 entries, sparse term B has 5000; verify iteration touches ≈5 entities, not 5000 (deterministic test design or instrumentation counter).
12. Pure-sparse on 0 entities: zero results, no panic.

Coverage gate: keep `sparse.go` ≥ 95.0% AND `query.go` ≥ 95.0% post-modification.

### Documentation updates (per CONTRIBUTING.md)

- `docs/ComponentTraits.md` — remove the \"query integration is Phase 15.20\" deferment from the Sparse section; replace with \"Queries with sparse-component terms iterate sparse-sets natively (v0.52.0).\"
- `docs/Queries.md` — new section on sparse-aware queries with worked examples.
- `docs/README.md` — feature-gap entry fully flipped.
- `README.md` — feature list bump.
- `CHANGELOG.md` — v0.52.0 entry at top.
- `ROADMAP.md` — shipped row + heading bump to \"through v0.52.0\". Current Shipped heading at `@ROADMAP.md:3` and Sparse row at `@ROADMAP.md:56` both need editing.

### Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run` clean.
- `go test ./... -race -count=3` passes.
- Coverage ≥ 95.0% on sparse.go and query.go.
- All Phase 15.19 sparse tests continue to pass.
- All existing archetype-only query tests continue to pass (no archetype-fast-path regression).

### Explicit non-goals

- **No DontFragment split** — consolidated into Sparse in 15.19; the split is deferred to Phase 15.21.
- **No Union** — Phase 15.22.
- **Deep traversal × sparse interactions** beyond the common cases listed above may defer to Phase 15.20.1 if they introduce iterator complexity; document any such deferment in CHANGELOG + ROADMAP.
- **No new built-in entity** — Sparse already shipped as built-in index 34 in v0.51.0.

## Constraints

- @sparse.go — Phase 15.19 (v0.51.0) shipped trait + storage. The query layer consults `IsSparse` (`@sparse.go:97`) to route per-term; reads go through `sparseSetGet` (`@sparse.go:155`); the iterator must integrate with the dense-vector layout described at `@sparse.go:19`-`@sparse.go:34`. Version counters for cache invalidation get hooked into `sparseSetInsert` (`@sparse.go:127`) and `sparseSetRemove` (`@sparse.go:170`).
- @query.go — primary integration site. `NewQueryFromTerms` at `@query.go:258`; `Query.Iter` table-seed selection at `@query.go:318`-`@query.go:365`; `QueryIter.Next` and `matchesTable` at `@query.go:424` and `@query.go:477`; `Field`/`FieldMaybe` at `@query.go:665` and `@query.go:712`; `validateAndSortTerms` at `@query.go:816`. The iterator extension must preserve the archetype fast path: keep the existing seed-table heuristic when no sparse terms are present.
- @cached_query.go — `NewCachedQueryFromTerms` at `@cached_query.go:168`, `newCachedQueryInternal` at `@cached_query.go:183`. Existing pre-filter loop near `@cached_query.go:385`-`@cached_query.go:401` shows the precedent for per-term routing; add a sparse-term branch here. The current code already notes that pair-mutation cache staleness is accepted (`@cached_query.go:386`-`@cached_query.go:394`); sparse mutation breaks the analogous assumption, so the version-counter approach is required to fix it.
- @ordered_children.go — Phase 15.18 precedent for iteration outside archetype tables; a small side-structure consulted during traversal. Mirrors the shape of the sparse-driver iteration path.
- @with.go — Phase 15.17 precedent for trait + side-storage with both immediate and deferred enforcement paths.
- @usage_constraints.go — Phase 15.15 precedent for `IsX(scope, id)` predicates used at compile-time; demonstrates the validation-on-construction pattern that sparse-term marking should follow.
- @ROADMAP.md — heading at line 3 (\"Shipped (through v0.51.0)\") and Sparse row at line 56 already note Phase 15.20 as the query-integration follow-up; both bump on completion.
- @CONTRIBUTING.md — docs/CHANGELOG/ROADMAP/README update matrix at lines 68-72 governs the documentation deliverables above.
- C reference: `/work/agents/claude/projects/SanderMertens/flecs/src/query/engine/eval_sparse.c` — 643 lines, the canonical reference for sparse-in-queries. Key sites: `flecs_query_sparse_init_sparse` at line 9, `flecs_query_sparse_next_entity` at line 51, `flecs_query_sparse_select_id` at line 93, `flecs_query_sparse_select_wildcard` at line 136, `flecs_query_sparse_next_wildcard_pair` at line 203, `flecs_query_sparse_select_all_wildcard_pairs` at line 225, `flecs_query_sparse_select` at line 268, `flecs_query_sparse_with_id` at line 289, `flecs_query_sparse_with_exclusive` at line 328, `flecs_query_sparse_with_wildcard` at line 364, `flecs_query_sparse_with_all_wildcard_pairs` at line 461.
- C reference: `/work/agents/claude/projects/SanderMertens/flecs/src/query/validator.c` — `is_sparse` flag determination during term validation at line 1329 (declaration), line 1341 (assignment), line 1394 (consumption).
- Process: feature (not bug). Label `snichols/queued`. The phase ships as v0.52.0 following v0.51.0 (commit `a18df9f`).
