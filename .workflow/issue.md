## Goal

Implement Phase 2.2 of the Go port of flecs: a **component index** — a reverse map from component ID to the tables (archetypes) containing that component. This enables fast query iteration in Phase 3 (a query like "all entities with `Position`" goes from O(tables) to O(matching tables)).

This phase implements ONLY the bookkeeping. Tables are still immortal in this phase (Phase 1 invariant), so we never *remove* from the index. Register-only.

### What's already on master (commit `6a4043a`)

Phase 2.1 landed. The world has cached add/remove edges between tables. Phase 1 + 2.1 give us:
- `flecs.ID`, `flecs.World`, `Set/Get/Has/Remove/RegisterComponent/Delete/IsAlive/Count`
- Archetype migration with edge-cached fast path
- `internal/storage/table.Table` with `NextOnAdd/NextOnRemove/CacheAddEdge/CacheRemoveEdge`
- `internal/storage/entityindex.Index` with paged records
- `internal/component.Registry` with `Component flecs.ID` field, `AssociateID`, `LookupByID`
- `internal/ids` leaf package holding the `ID` uint64 primitive
- `World.tables map[string]*table.Table` keyed by sorted-signature byte-view; two creation sites (the empty table in `New`, the find-or-create miss branch in `migrate`)

### C reference (cite, do not paraphrase)

- `/work/agents/claude/projects/SanderMertens/flecs/src/storage/component_index.h` — `ecs_component_record_t` definition. Port ONLY the table-list portion (the `cache` field — doubly-linked list of `(table, table_record)` pairs). Out of scope: flag-tracking, traversable-counting, observer-list, sparse-storage-flag, event-observation. Those land in later phases.
- `/work/agents/claude/projects/SanderMertens/flecs/src/storage/component_index.c` — function bodies. Relevant logic is `flecs_components_get` and the per-table register/unregister on creation. Ignore deferred-event paths and the linked-list table-cache machinery; a Go slice per component-id is sufficient for v0.

## Deliverables

### 1. New internal package `internal/storage/componentindex`

Files: `componentindex.go` and `componentindex_test.go`.

### 2. Public types

- `type Index struct { ... }` — the reverse index. Unexported fields.
- `func New() *Index` — constructor.

### 3. Methods on `*Index`

- `func (i *Index) Register(componentID flecs.ID, t *table.Table)` — record that `t` contains `componentID`. Idempotent: registering the same `(componentID, t)` pair twice does not produce duplicates. Document idempotence with a test.
- `func (i *Index) TablesFor(componentID flecs.ID) []*table.Table` — return a slice of tables containing `componentID`. Return a **snapshot** (a copy, not the live backing slice) — the index owns the live slice and callers must not be able to mutate the index's internal state. Empty slice (not nil) if no tables. Document both.
- `func (i *Index) Each(componentID flecs.ID, fn func(*table.Table) bool)` — iterate tables. `fn` returns `false` to stop iteration early. **NO snapshot allocation;** this is the hot path for queries (Phase 3). Document: caller must NOT mutate the index during iteration; behavior is undefined if `Register` is called from `fn`.
- `func (i *Index) Count(componentID flecs.ID) int` — number of tables containing this component. O(1).
- `func (i *Index) CountComponents() int` — total number of distinct component IDs known to the index. O(1).

### 4. Update `world.go`

- Add `compIndex *componentindex.Index` field to `World`.
- Initialize it in `New()`.
- At BOTH table-creation sites — verified call sites:
  - Empty-table creation in `New` (around line 42: `w.tables[sigKey(nil)] = w.empty`).
  - Find-or-create miss branch inside `migrate` (around line 245: `w.tables[key] = newTable`).
- Iterate the new table's `Type()` IDs and call `w.compIndex.Register(id, newTable)` for each. The empty table has an empty signature, so the loop is a no-op there — but **the call site should still exist for symmetry.** (Don't conditionalize on signature length.)
- Expose two read-only world methods that wrap the index for callers and tests:
  - `func (w *World) TablesFor(componentID flecs.ID) []*table.Table` — snapshot.
  - `func (w *World) EachTableFor(componentID flecs.ID, fn func(*table.Table) bool)` — iteration.

### 5. Tests

**On `componentindex` directly (`componentindex_test.go`):**
- `New().Count(anyID) == 0`; `TablesFor(anyID) == empty (not nil)`.
- `Register(id, t1)`; `Count(id) == 1`; `TablesFor(id)` returns a one-element slice containing `t1`.
- `Register(id, t1)` twice → still `Count(id) == 1` (idempotent).
- `Register(id, t1)` then `Register(id, t2)` → `Count(id) == 2`; order matches insertion (document the contract).
- `Register(idA, t1)` and `Register(idB, t1)` → `CountComponents() == 2`; each lookup returns `[t1]`.
- `Each` stops early when `fn` returns false.
- `TablesFor` returns a copy: mutating the returned slice does NOT mutate the index. Verify by reading `TablesFor` again.

**On `World` (additions to `world_test.go`):**
- Create world, register `Position`, create entity, `Set[Position]` → `TablesFor(positionID)` contains exactly the one `[Position]` archetype table.
- `Set[Position]` then `Set[Velocity]` on one entity → `TablesFor(positionID)` contains both `[Position]` table AND `[Position, Velocity]` table; `TablesFor(velocityID)` contains only `[Position, Velocity]`.
- Create two entities both with `[Position, Velocity]` → `TablesFor(positionID)` contains the single shared table (just one — not duplicated).
- **No leak across removals:** after `Set[Position]` (creates `[Position]` table) then `Set[Velocity]` (creates `[Position, Velocity]`), the original `[Position]` table is STILL in `TablesFor(positionID)` (since tables are immortal). Document this — there will be ghost tables until table-eviction lands in a later phase.
- **Tag components indexed normally.** `Set[Tag](w, e, struct{}{})` → `TablesFor(tagID)` returns the `[tagID]` archetype table.
- **Empty table not indexed under any component.** `TablesFor(any componentID)` does not contain the empty table.

### 6. Mechanical acceptance

- `go test ./... -race` passes; existing tests stay green.
- `go vet ./...` passes.
- `golangci-lint run` passes.
- Coverage on `internal/storage/componentindex` ≥ 95%.
- No regression on `flecs` coverage (was 97.0% in Phase 2.1; stay ≥ 90%).
- All exported symbols have godoc.

## Non-goals (defer)

- NO unregistration / table eviction. Tables are immortal in this phase.
- NO component-record flags (`ecs_component_record_t` carries flags like "is observed by some observer," "sparse storage opt-in," etc.). Defer to Phase 5 (observers) and later.
- NO traversal queries (parent-of, descendants-of). Defer to Phase 4 (relationships).
- NO event observers / change tracking. Defer to Phase 5.
- NO O(1) `(table, id) → table_record_t` reverse lookup from the table side. The C version maintains a `table_cache.next/prev` linked list embedded in each table-record so you can iterate from a known table-id pair. We don't need that yet; queries in Phase 3 will iterate via `TablesFor(id)` and binary-search within each table for the column.
- NO pair-aware indexing (e.g. `TablesFor(MakePair(rel, tgt))`). Defer to Phase 4.
- NO concurrency. The index is single-threaded.

## Constraints / pointers for the implementer

- The reverse map is `map[flecs.ID][]*table.Table`. Idempotence requires scanning the slice on `Register` (O(n) per register, where n is the number of tables already registered for that id). For Phase 2.2 this is fine — table creation is rare. A future phase can switch to `map[flecs.ID]map[*Table]struct{}` if needed.
- DO NOT lock-protect the index. Document single-threaded.
- The world's existing `tables map[string]*table.Table` keyed by signature is the **authoritative** table registry. The component index is a **secondary read-optimized view.** Both are written at the same call sites.
- The two call sites in `world.go` to update (verified against current master):
  - (a) empty-table creation in `New` at line 42 (`w.tables[sigKey(nil)] = w.empty`).
  - (b) find-or-create miss branch in `migrate` at line 245 (immediately after `w.tables[key] = newTable`).
- The empty table has an empty signature. The Register loop over `newTable.Type()` becomes a no-op for it. Don't special-case.
- Read `/work/agents/claude/projects/SanderMertens/flecs/src/storage/component_index.c` to understand the C structure shape, but do not port the linked-list + table-cache machinery. A Go slice per component-id is sufficient.

## Relevant files

C reference (filesystem paths — cite, do not @-reference):
- `/work/agents/claude/projects/SanderMertens/flecs/src/storage/component_index.h`
- `/work/agents/claude/projects/SanderMertens/flecs/src/storage/component_index.c`

## Constraints

- @world.go — especially the `New` constructor (empty-table creation around line 42) and the `migrate` function (find-or-create miss branch around line 245) for the two table-creation sites where `compIndex.Register` must be called.
- @internal/storage/table/table.go — the `*table.Table` type that the index stores pointers to; `Type()` returns the signature to iterate.
- @internal/storage/entityindex/entityindex.go — stylistic reference for an internal-storage package with `New()` constructor and unexported state. Follow the same shape (single struct, unexported fields, methods on `*Index`).
- @id.go — the public `flecs.ID` type used as the map key.
