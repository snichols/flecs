## Goal

Port C flecs `group_by_callback` to Go flecs as **Phase 16.11 — Query groups**, target version **v0.66.0** (next after v0.65.0 monitor observers, shipped at `67886b1`).

`group_by_callback` partitions a cached query's matched tables into labelled groups. A caller-supplied callback assigns each table to a group ID; the query stores tables segregated by group; iteration runs either over a single group (O(1) startup) or over all groups in group-ID order. The canonical use case is hierarchy-depth ordering — and indeed `Cascade` is implemented in upstream C as a special built-in `group_by` callback (`src/query/cache/cache.c:175-189`). This phase generalises that mechanism to arbitrary user-defined partitioning.

After this phase ships, `docs/README.md` line 111 flips from "not yet ported in Go flecs" to "✅ shipped in v0.66.0".

### What lands

1. **New file `query_group.go`** (or extension of `query_sort.go` — they share `CachedQueryOptions`):
   - `GroupByFunc func(t *Table) uint64` — table → group ID.
   - `WithGroupBy(componentID ID, groupFn GroupByFunc) CachedQueryOptions` — `componentID` is the trigger for invalidation (mirrors upstream `ecs_query_desc_t.group_by` at `include/flecs.h:1331`); the function performs the actual partitioning. Builds on the existing `CachedQueryOptions` shape from Phase 16.4.
2. **Cached query plan extension** in `cached_query.go`:
   - When `WithGroupBy` is supplied, compute table → group assignments at cache-build time.
   - Storage: `map[uint64][]*table.Table` keyed by group ID, plus a sorted `[]uint64` of group IDs for ordered iteration. (Mirrors C `ecs_query_cache_t.groups` + `first_group` ordered list; `src/query/cache/group.c:52-89`.)
   - Re-group on cache invalidation: table-set change or component-value change on the group-by component. Full re-group on any change (mirrors sort invalidation from Phase 16.4).
3. **Iteration**:
   - Default `Iter()` walks groups in ascending group-ID order.
   - `(*CachedQuery).IterGroup(groupID uint64) *QueryIter` — start iteration at a specific group, yielding only its tables. O(1) startup cost. Mirrors `ecs_iter_set_group` (`include/flecs.h:5205`, `src/query/api.c:649-686`).
   - `(*CachedQuery).Groups() []uint64` — return the sorted list of currently-populated group IDs.
4. **Compose with `WithOrderBy`** (Phase 16.4):
   - Both options on the same query → tables grouped first, sort runs within each group.
   - Default iteration: walk groups in ID order; within each group, yield entities in sorted order.
   - Document the composition order; cover it in a test.
5. **Cascade compatibility**:
   - Existing `cascadeTermTrav` plumbing in `@cached_query.go` (lines 106, 213-218, 248, 261-262) and `sortByCascadeDepth` in `@query_sort.go` (lines 276-294) is **not** rewritten in this phase. After Phase 16.11, `docs/Queries.md` notes that `Cascade` is implementable on top of `WithGroupBy` (the C reference: `src/query/cache/cache.c:175-189` `flecs_query_cache_group_by_cascade`). Refactor deferred — that's a follow-up, not this feature.
6. **Tests in `query_group_test.go`** (≥ 10 cases, coverage ≥ 95.0%):
   - 3 archetype tables (Position; Position+Velocity; Position+Health). GroupBy → component count. Iteration yields tables in count order.
   - `IterGroup(2)` yields only the Position+Velocity table.
   - `Groups()` returns the sorted populated set.
   - GroupBy + WithOrderBy: 100 entities across 3 groups, each sorted by Position.X.
   - Empty group: terms matching 0 entities → `Groups()` returns `[]`.
   - Cache invalidation: GroupBy keys on a mutable component value; mutation triggers re-grouping on next iteration.
   - GroupBy on a non-existent component: panic at construction (mirrors sort behavior in `@query_sort.go` lines 83-96).
   - Multi-table within a single group: GroupBy returns same ID for multiple tables; iteration yields all of them.
   - Stable order across cache hits: iterate twice; same order both times.
   - Sparse component as a query term: groups still computed from archetype state.
7. **Doc updates** per CONTRIBUTING.md:
   - `docs/Queries.md` — extend the Sorted queries section (currently line 747) to add Query groups; show the compose-with-sort pattern.
   - `docs/README.md` — flip line 111 to ✅ shipped (v0.66.0).
   - `README.md` — feature-list bump.
   - `CHANGELOG.md` — v0.66.0 entry at top.
   - `ROADMAP.md` — heading bump to "through v0.66.0".

### Decisions (stated up-front, validate during impl)

1. **Group key type**: `uint64` — matches upstream `ecs_group_by_action_t` return at `include/flecs.h:621-625`, matches the typical hash-id use case, and avoids interface boxing in the hot path.
2. **Cache invalidation granularity**: full re-group on any change (table-set change or change-count bump). Mirrors sort invalidation from Phase 16.4 — simpler than incremental and adequate for the canonical workloads.
3. **Compose-with-sort order**: groups outer, sort inner. Not configurable — the natural interpretation of "grouped, sorted within each group" is the only one upstream C supports either, and adding a knob is gratuitous.
4. **Marshal**: do NOT serialize group state. Recomputed lazily on next Iter after Unmarshal. (Mirrors sort and change-detection state which are also runtime-only.)

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%
- No regression on existing query / sort / cascade tests.

### Explicit non-goals

- No automatic refactor of Cascade to use group_by — Cascade keeps its current dedicated implementation. Doc page notes the generalisation for future work.
- No multi-key grouping — single component / single callback only.
- No group-level events ("entity joined group 3", `on_group_create`/`on_group_delete` from `include/flecs.h:627-638`) — out of scope.
- No persistent group identifiers across world reloads — groups are runtime-only state, not marshalled.

## Constraints

- @docs/README.md — line 111 is the gap-list entry being closed by this phase; flip to ✅ shipped in v0.66.0.
- @docs/Queries.md — line 747 § Sorted queries is the section to extend with Query groups; line 906 has the index entry.
- @query_sort.go — `CachedQueryOptions` shape (lines 36-39), `WithOrderBy` constructor (lines 53-55), `NewCachedQueryFromTermsWithOptions` validation pattern (lines 73-106), `needsSortRebuild`/`rebuildSorted` invalidation pattern (lines 110-265). `WithGroupBy` extends this same options struct and follows the same construction-time validation idiom.
- @cached_query.go — `CachedQuery` struct (line 96); existing `cascadeTermTrav` field (line 106); sorted-iteration state fields (lines 128-134); `Iter()` sorted branch (lines 348-377). Group state lives in this struct alongside the sort state.
- @query.go — `TraverseCascade` and the cascade detection paths (lines 38-41, 166-170, 297, 932) — group_by generalises this mechanism but does not replace it in v0.66.0.
- @scope.go — iterator construction APIs; `IterGroup` returns `*QueryIter` and must compose with the existing `scope` interface.
- @CONTRIBUTING.md — doc-update checklist (README.md / docs/README.md / CHANGELOG.md / ROADMAP.md / Queries.md), version-bump cadence, and test-coverage threshold.
- @VISION.md — guides phase scoping and the "faithful port with clear divergences documented" principle. Group state being runtime-only (no marshal) and the deliberate scope-narrowing (no `on_group_create`/`on_group_delete`, no multi-key grouping) are documented divergences.
- C reference: `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h:621-625` (`ecs_group_by_action_t` signature), `:1329-1352` (`ecs_query_desc_t.group_by`, `group_by_callback`, `group_by_ctx`), `:5183-5237` (`ecs_iter_set_group` + `ecs_query_get_groups` API).
- C reference: `/work/agents/claude/projects/SanderMertens/flecs/src/query/cache/group.c:25-146` (per-group storage; ordered-list insertion at `:52-89`; ensure-group + on-group-create dispatch at `:91-146`).
- C reference: `/work/agents/claude/projects/SanderMertens/flecs/src/query/cache/cache.c:175-189` (`flecs_query_cache_group_by_cascade` — Cascade is upstream's canonical group_by use; informs the Go cascade-on-top-of-group_by future refactor).
- C reference: `/work/agents/claude/projects/SanderMertens/flecs/src/query/api.c:649-686` (`ecs_iter_set_group` — informs `IterGroup` semantics including O(1) startup cost).
