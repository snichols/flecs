## Goal

Port upstream flecs' `order_by_callback` to Go flecs: a cached query can supply a comparator + sort-by component ID, and the query yields its matched entities in sorted order. The sort runs lazily on cache rebuild / invalidation, not on every iteration.

Gap entry: [`docs/README.md` line 110](docs/README.md#L110): _"Sorted queries — `order_by_callback` sorts matched entities by a component value (two-step quicksort; cached; change-detection driven). not yet ported in Go flecs."_

After this phase line 110 flips to shipped (v0.59.0).

### Upstream behaviour (verified)

`include/flecs.h`:
- `ecs_order_by_action_t` typedef — lines 602-607: `int(*)(ecs_entity_t e1, const void *ptr1, ecs_entity_t e2, const void *ptr2)`.
- `ecs_query_desc_t.order_by_callback` (line 1319), `order_by_table_callback` (line 1323), `order_by` component entity (line 1327).

`src/query/cache/order_by.c` (whole file, 316 lines) confirms the algorithm. It is a **two-step process**, but neither step is a quicksort of the global result set:
- **Step 1** — `flecs_query_cache_sort_table` (lines 11-44): for each matching table whose change-detection monitor reports it is dirty (line 257) **or** whose sort-by column was written (line 279), call a generic in-place quicksort over the table's row range. This sorts each table's rows locally.
- **Step 2** — `flecs_query_cache_build_sorted_table_range` (lines 83-214): k-way merge using a per-table `sort_helper_t` cursor. At each iteration the helper with the minimum-key head row is selected via linear scan (lines 167-196), its row is appended to `cache->table_slices` (lines 198-207), the cursor advances. Adjacent rows from the same table coalesce into a slice run. The result is `table_slices`: an ordered list of (table-slice, offset, count) entries the iterator walks in order.
- Cache invalidation (`flecs_query_cache_sort_tables`, lines 228-316): driven by change-detection monitors per table. Re-runs step 1 only on dirty tables, then unconditionally rebuilds step 2 if anything sorted or if `match_count != prev_match_count`.

### Go-side context (verified)

- [`cached_query.go`](cached_query.go) lines 96-128 — `CachedQuery` struct. Lines 204-267 — `newCachedQueryInternal` is the single construction path; lines 254-256 already host a post-population sort hook (`sortByCascadeDepth`) — the same insertion point fits an `order_by` initial sort.
- [`cached_query.go`](cached_query.go) lines 271-294 — `sortByCascadeDepth` is the direct precedent for an in-place reordering of the cached match-set; it currently sorts `cq.tables` (and parallel `cq.tableUpSources`). Sorted queries need finer granularity (entity-level, not table-level), so a parallel sorted-slice field is the right shape rather than reusing this path.
- [`query.go`](query.go) lines 263-301 — `NewQueryFromTerms` is the uncached constructor; lines 1295+ — `validateAndSortTerms` is the shared validation entry. The `WithOrderBy` option needs to plumb through `NewCachedQueryFromTerms` only (uncached queries are non-goals; see below).
- [`scope.go`](scope.go) lines 798-880 — `Each1`/`Each2` iterate via `q.Iter()` then `it.Next()` / `it.Entities()`. For sorted iteration the iterator must yield entities in a precomputed order rather than per-table.
- [`internal/storage/table/table.go`](internal/storage/table/table.go) lines 40-44, 99-109 — every table already exposes `ChangeCount()` / `BumpChange()`. Lazy re-sort can check the cached `(table, lastSeenChangeCount)` map; no `OnSet` subscription required. **Use this** instead of upstream's per-table monitor; it's simpler and equivalent for our needs.
- [`observer.go`](observer.go) lines 15-26 — `EventOnSet` exists for the OnSet path if needed as a fallback, but `ChangeCount` covers all column writes already.
- [`ordered_children.go`](ordered_children.go) — closest precedent for per-parent ordering, but per-parent slice; sorted queries are global. Not a direct match.

## Constraints

- @docs/README.md — line 110 (gap entry) flips to shipped (v0.59.0) with anchor link. Lines 100-115 are the gaps-discovered-in-Phase-14.2 section that this phase closes one bullet of.
- @CHANGELOG.md — add `v0.59.0` entry at top following the v0.58.0/v0.57.0/v0.56.0 structure (heading, summary paragraph, Added / Changed / Design decisions recorded / Changed (docs) sub-sections).
- @ROADMAP.md — bump "Shipped (through v0.58.0)" heading on line 3 to "through v0.59.0"; add a v0.59.0 bullet beneath the v0.58.0 entry on line 62.
- @CONTRIBUTING.md — doc layering convention (`docs/README.md` gap table + per-feature manual + CHANGELOG + ROADMAP). Sections § Documentation (line 58) and § Style (line 49) apply.
- @cached_query.go — sole touchpoint for cached-query construction (`newCachedQueryInternal`, lines 204-267) and the parallel-slice ownership pattern used by `tables` / `tableUpSources` / `sortByCascadeDepth` (lines 96-294).
- @query.go — `validateAndSortTerms` (line 1295) is where the "sort-by component must appear in the term set" check belongs. `Term` shape and option ergonomics defined here (lines 126-301).
- @scope.go — `Each1`/`Each2`/etc. (lines 798-880) consume the cached query's iteration order; the sorted iterator must yield entities in sort order without breaking field-access semantics.
- @internal/storage/table/table.go — `ChangeCount()` (line 99) is the invalidation signal. Use it.
- @observer.go — `EventOnSet` (line 15) is the upstream invalidation hook; we prefer ChangeCount but the file is referenced as the equivalent mechanism.
- Phase 15.20 sparse-query integration is the architectural precedent: a query-layer mode the cached-query plan recognizes. New file `query_sort.go` should slot alongside `query_filters.go` / sparse mode plumbing.
- The OrderedChildren trait (Phase 15.18) is a per-parent ordering mechanism; sorted queries are query-wide. Distinct features; mention in user docs to avoid confusion.

## Deliverables

### 1. New file `query_sort.go`

- `type OrderByFunc func(eA ID, vA unsafe.Pointer, eB ID, vB unsafe.Pointer) int` — Go-convention comparator (negative / zero / positive).
- `func OrderBy[T any](cmp func(eA ID, vA *T, eB ID, vB *T) int) OrderByFunc` — typed convenience wrapper.
- `type orderByOption struct { componentID ID; cmp OrderByFunc }` — internal carrier; exposed via `WithOrderBy(componentID ID, cmp OrderByFunc)` builder option.
- Construction validation: panic if `componentID` is not present as a TermAnd or TermOptional in the query's term set (Or/Not don't supply a value to read).

### 2. Cached-query plan extension

- Extend `CachedQuery` with optional sort state: `orderBy ID`, `orderByCmp OrderByFunc`, `sortedEntities []ID`, `sortedFieldRows []sortedFieldRow{table *table.Table; row int}` (parallel slice for O(1) field lookup), `sortedLastChange map[*table.Table]uint64` (lazy invalidation).
- `NewCachedQueryFromTerms` accepts variadic options ending with the existing terms — pick the option-shape that minimizes API churn (likely a separate `NewCachedQueryFromTermsWithOptions` or a builder; record the decision in the CHANGELOG "Design decisions recorded" section).
- Initial build (in `newCachedQueryInternal` post-population, near line 254): walk all matching tables, collect `(entity, table, row, valuePtr)`, run a **naive `sort.Slice`** over the result with the user's comparator. (See "Open decisions" below: we deliberately don't replicate upstream's two-step algorithm in v0.59.0.)
- Lazy invalidation on `Iter()`: scan `cq.tables`, for each table compare `t.ChangeCount()` against `sortedLastChange[t]`; if any differ, rebuild the sorted slice. Also rebuild on `cq.tablesAdded`.

### 3. Iteration

- New iterator path: `sortedIter` that walks `sortedEntities` / `sortedFieldRows` in order, exposing the same `Field[T]` / `FieldMaybe[T]` semantics as the table-walk iterator. Field access reads from the entity's actual archetype using the parallel `(table, row)` lookup.
- `Each1`/`Each2`/etc. work unchanged because they go through `q.Iter()` — the iterator's `Next()` / `Entities()` shape is preserved; only the underlying order changes.
- Document that field slices returned by sorted iteration are **per-entity scalars** (not per-table slices), because a sorted run mixes rows from different tables. The cleanest shape is to yield one entity at a time from `it.Entities()` (length-1 slices) so the existing `Each*` loops work without restructuring.

### 4. Tests in `query_sort_test.go` (>= 10 cases)

1. Basic ascending sort by integer component — 100 entities random values, iteration yields ascending order.
2. Descending sort via comparator return-sign flip.
3. Stable across re-iteration — same query iterated twice, identical order.
4. Cache invalidation on value mutation — iterate, `Set` a new value on one entity, re-iterate, observe new order.
5. Table-set change — `Add` a new entity matching the query, re-iterate, new entity in correct position.
6. String comparator via `OrderBy[string]`.
7. Multi-term sorted query — 3 TermAnd terms; sort by term 1; all 3 fields accessible during iteration.
8. Empty result — 0 matches; iteration yields nothing without panic.
9. Construction panic — `WithOrderBy(C, cmp)` where `C` is not in the term set.
10. Mixed sparse + archetype + sort-by — verify sort applies across the union of sparse-driven and archetype-driven matches.
11. (bonus) Sort by a sparse component — recommended in open decision 3.

Coverage target: ≥ 95.0%.

### 5. Doc updates per CONTRIBUTING.md

- `docs/Queries.md` — new § Sorted queries with two examples (sort by `Position.X` with `OrderBy[Position]`; sort with raw `OrderByFunc`) and the lazy-invalidation note. Document the cached-only restriction (no `NewQueryFromTerms` support).
- `docs/README.md` — line 110 flips to shipped (v0.59.0) with anchor link to the new § Sorted queries section.
- `README.md` — feature list bump.
- `CHANGELOG.md` — v0.59.0 entry at top.
- `ROADMAP.md` — heading on line 3 bumps to "through v0.59.0"; v0.59.0 entry added beneath line 62.

## Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%
- No regression on existing query tests (Phase 9.5 change detection, Phase 14.2 cached queries, Phase 15.20 sparse iteration).

## Explicit non-goals

- **No non-cached sorted queries.** Sorting a non-cached query would re-sort on every iteration; we do not expose `WithOrderBy` on `NewQueryFromTerms`. Document.
- **No query groups (`group_by`).** Separate phase (gap line 111).
- **No fixed per-term source.** Separate phase (gap line 108).
- **No multi-key sort.** Single component only. Users wanting multi-key compose in their comparator using a packed struct.
- **No upstream's "only invalidate if reordering changed" optimization.** Lazy ChangeCount-based rebuild is accepted as is.
- **No `order_by_table_callback` (upstream's bulk-table sort hook).** Single comparator API only.

## Open decision points

1. **Sort algorithm.** Recommend **naive global `sort.Slice` over the result list** (simpler, identical observable result). Upstream's two-step (per-table quicksort + k-way merge) is a future performance optimization, document the choice in CHANGELOG "Design decisions recorded".
2. **Sort component must be in term set.** Recommend **panic at construction** for clarity (matches Go flecs' strict-validation precedent in `validateAndSortTerms`). Document.
3. **Sparse component sort.** Recommend **yes** — read the value via the existing sparse-set lookup. Include a test case.
4. **DontFragment / Union pair sort.** Recommend **defer** — document "supported only for plain components in v0.59.0; pair-form support is a future enhancement". Test that construction panics with a clear message if the user attempts it.
5. **Invalidation source.** Recommend **per-table `ChangeCount`** rather than upstream's `OnSet`-driven monitor. ChangeCount already covers all column writes; this avoids subscribing an observer for every sorted query and naturally extends to structural changes (new entity added, entity removed) that OnSet alone would miss.
6. **Option API shape.** Decision: introduce variadic `NewCachedQueryFromTermsWithOptions(w, opts, terms...)` or add an option-builder pattern. Pick whichever minimizes churn against existing call sites. Record decision in CHANGELOG.

## Process

- Feature, not bug.
- Label: `snichols/queued`.
- Target version: v0.59.0 (next after v0.58.0 system lifecycle bundle, shipped at commit c1c3fc9).
