## Goal

Add an **edge cache** to `internal/storage/table.Table` so that `World.migrate` can skip the signature-compute + `tables` map lookup on repeated migrations of the same shape (e.g. `Set[Position]` on many fresh entities). This is the principal performance win that makes ECS migration fast in flecs — every archetype transition becomes a pointer follow on the second-and-subsequent call.

**This phase implements ONLY the edge cache, not the full table-graph topology.** No edge invalidation (tables are immortal in this port phase). No incoming-edge tracking. No precomputed `ecs_table_diff_t` payloads. No bidirectional add/remove pairing. Just: given table `T` and component `C`, what is the table after adding `C`? Cache the answer. Same for remove.

### Deliverables

1. **Update `internal/storage/table/table.go`:** add internal edge-cache fields on `Table`. Recommended:

   ```go
   type Table struct {
       // ...existing fields...
       addEdges    map[flecs.ID]*Table
       removeEdges map[flecs.ID]*Table
   }
   ```

   Keep the maps **lazily-allocated** (nil until first edge cached) so tables that are never migrated *through* pay no map-header overhead. Document the choice in a comment on the struct.

2. **Public methods on `*Table`:**
   - `func (t *Table) NextOnAdd(id flecs.ID) (*Table, bool)` — returns the cached destination for adding `id` to this table's signature. Second return `false` if not cached. **Pure cache lookup — does not compute or create.**
   - `func (t *Table) NextOnRemove(id flecs.ID) (*Table, bool)` — symmetric for remove.
   - `func (t *Table) CacheAddEdge(id flecs.ID, dst *Table)` — record `(t, +id) -> dst`. **Idempotent** if `dst` is the same pointer. **Panics** if a different `*Table` is already cached for the same `(t, id)` (this would indicate a correctness bug somewhere upstream).
   - `func (t *Table) CacheRemoveEdge(id flecs.ID, dst *Table)` — symmetric.
   - All four methods document in their godoc: **not goroutine-safe**; matches the Phase 1 single-threaded invariant for `World`.

3. **Update `World.migrate` in `world.go`** to consult the edge cache. The find-or-create-destination block currently sits at `world.go:215-229`. **Wrap it, don't rewrite.** Pseudocode:

   ```
   if removeID != 0 && addID == 0:
     if dst, ok := oldTable.NextOnRemove(removeID); ok:
       newTable = dst
     else:
       <existing compute newSig + tables-map find-or-create>
       oldTable.CacheRemoveEdge(removeID, newTable)
   else if addID != 0 && removeID == 0:
     if oldTable != nil:
       if dst, ok := oldTable.NextOnAdd(addID); ok:
         newTable = dst
       else:
         <existing compute newSig + tables-map find-or-create>
         oldTable.CacheAddEdge(addID, newTable)
     else:
       <existing path — no source table to cache from>
   else if addID != 0 && removeID != 0:
     <existing path — do NOT cache compound transitions>
   else:
     <both zero — existing path; current code already handles>
   ```

   Note that **both `addID` and `removeID` may be non-zero** (pair-replace patterns later). Confirm by reading current call sites in `world.go` — for Phase 2.1, `Set[T]` and `Remove[T]` only set one at a time, but the migrate function takes both, so the compound path must still work via the existing un-cached route. **Do NOT cache compound (add+remove) transitions.** Also do not cache when `oldTable` is nil (no source to cache the edge on).

   The world's existing `tables map[string]*table.Table` and `sigKey` encoding **stay unchanged**. The cache is an *acceleration* over that lookup, not a replacement. Even on cache miss, route through the existing find-or-create path so the canonical registry stays authoritative.

4. **Tests** in `world_test.go` (or a new `migration_test.go`):
   - **Cache-hit on second migration of same shape.** Create world, `Set[Position](w, e1, p1)`, then `Set[Position](w, e2, p2)`. Both migrate empty -> `[Position]`. After the first call, the empty table has `addEdges[positionID] == [Position]Table`. The second call hits the cache. Verify via a test-only inspection helper in `export_test.go` (e.g. `table.TestEdgeCount`, `table.TestNextOnAdd`).
   - **Round-trip cache.** Add Position (caches `empty -+P-> [P]`), then Remove Position (caches `[P] --P-> empty`). Both edges recorded. Subsequent identical operations hit the cache.
   - **Distinct cache entries for distinct components.** `Set[Position]` then `Set[Velocity]` on a fresh entity -> the empty table has two add-edges: one for `positionID`, one for `velocityID`, pointing at **different** destination tables.
   - **Idempotent `CacheAddEdge`.** Caching `(t, +id, dst)` twice with the same `dst` is fine; with a different `dst` panics. Test both paths (use `defer recover()` for the panic case).
   - **Tag components cache normally.** `Set` of a `struct{}` tag also populates an edge.
   - **No cache leak across migrations.** Migrate `e1` empty -> `[P]` -> `[P,V]`. The empty table's `addEdges` contains **only** `+P -> [P]` (NOT a fake `+V -> [P,V]` edge, NOT a compound edge). The `[P]` table's `addEdges` contains `+V -> [P,V]`. Verify.
   - **Existing Phase 1 tests stay green.** Run the full suite — Phase 1.5's tests must pass unchanged.

5. **Mechanical acceptance**
   - `go test ./... -race` passes.
   - `go vet ./...` passes.
   - `golangci-lint run` passes.
   - Coverage on `flecs` >= 90% (don't regress from Phase 1.5's 97.6%).
   - Coverage on `internal/storage/table` >= 90% (Phase 1.4 was 96.6%).
   - All exported symbols have godoc.

### Non-goals

- NO incoming-edge tracking (deferred — needed later for table eviction).
- NO precomputed `ecs_table_diff_t` (component-set differences cached on edges — defer; the current copy loop in `migrate` recomputes overlap each call, which is acceptable for v0).
- NO compound (add+remove) edges. Only single-id edges.
- NO `lo_edges` small-array optimization. Go maps are fine.
- NO graph traversal beyond a single edge step. The only callers of `NextOnAdd`/`NextOnRemove` in this issue are inside `World.migrate`.
- NO concurrency. Edge caches are NOT goroutine-safe.
- NO edge invalidation. Tables live forever in this phase.
- NO observer firing on archetype transitions (deferred to Phase 5).

### Constraints / pointers for the implementer

- Edge maps are stored *on* the `Table`; when a new table is created via `table.New(...)` the maps default to nil. **Lazy-allocate** on first `Cache*Edge` call.
- Cache lookup must be **O(1)**. Don't substitute a slice scan.
- Cache key is the raw `flecs.ID`. The pair/flag bits in an ID do not need special handling here — an ID is just a `uint64` map key.
- Don't bypass the `tables map[string]*table.Table` registry. Even on cache miss, the world finds-or-creates through the existing path; the cache only short-circuits *successful* lookups.
- Test-only inspection helpers go in `export_test.go` (already exists in the repo per Phase 1.5). Pattern: `TestEdgeCount(t *Table) int`, `TestNextOnAdd(t *Table, id flecs.ID) (*Table, bool)`. Add a comment noting they're for tests only.
- `migrate` currently has its 13-line find-or-create-destination block at `world.go:215-229`. **Wrap it (don't rewrite).** The cache check goes before; the cache write goes after the destination is found/created.

### C reference (read but do not paraphrase)

- `/work/agents/claude/projects/SanderMertens/flecs/src/storage/table_graph.h` lines 1-100 — struct definitions for edges and graph nodes (`ecs_graph_edge_t`, `ecs_graph_node_t`).
- `/work/agents/claude/projects/SanderMertens/flecs/src/storage/table_graph.c` — function bodies; the relevant logic is `flecs_table_traverse_add` / `flecs_table_traverse_remove`. **Ignore** the `lo_edges` (small fixed-size array for low ids) vs `hi_edges` (map for high ids) split — Go maps are good enough; use one map.
- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs/private/api_internals.h` line 43 — `ecs_table_diff_t` struct (informational only; we are **not** porting precomputed diffs in this phase).

## Constraints

- @world.go — Phase 1.5 `migrate()` function. The cache check + cache write wrap the existing find-or-create block at lines 215-229. The cache is an acceleration, not a replacement — even on cache miss, route through the existing `tables map[string]*table.Table` path so the canonical registry stays authoritative.
- @internal/storage/table/table.go — `Table` struct receives the new `addEdges` / `removeEdges` map fields and four new public methods (`NextOnAdd`, `NextOnRemove`, `CacheAddEdge`, `CacheRemoveEdge`). Maps are lazily allocated; methods are not goroutine-safe, matching the Phase 1 single-threaded `World` invariant.
- @internal/storage/table/column.go — unchanged; provided for context on the SoA storage Table wraps.
- @id.go — `flecs.ID` is a type alias for `ids.ID` (uint64). Used directly as the edge-map key; no flag-bit special handling.
- @export_test.go — Phase 1.5 already established the `export_test.go` convention for test-only inspection. Add helpers here (e.g. `TestEdgeCount`, `TestNextOnAdd`) for the cache assertions, with a comment noting they are for tests only.
