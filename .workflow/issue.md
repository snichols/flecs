## Goal

Implement Phase 3.1 of the Go port of flecs: the **user-visible query API** for minimum-viable queries. A query is a fixed list of component IDs that an entity must ALL have (AND terms only). The iterator yields one matching *table* at a time (not one entity), so users iterate by column for cache efficiency — the standard ECS hot loop. Typed access to component data uses a generic `Field[T]` helper that returns a `[]T` slice over the table's column.

**Scope: AND terms only, allocation-light iteration, typed field access.** No NOT, no Optional, no OR, no pairs, no wildcards, no traversal, no predicates, no cached queries — all later phases.

**Iteration matching strategy:** pick the term with the smallest `world.TablesFor(id)` set as the seed, then for each candidate table, check that it contains every other term's id. This is O(smallest-set × terms) — optimal for sparse queries, acceptable for v0 even for dense queries.

### Deliverables

**1. New file `query.go`** in the top-level `flecs` package. (No new sub-package — query is a user-facing type and we keep it in the root for now; if it grows we can split later.)

**2. `type Query struct`** with these unexported fields at minimum:
- The list of component IDs (terms).
- A reference to the `*World` it was built against.
- (Optional, for performance: a cached smallest-term index, recomputed lazily — implementer's call.)

Public methods on `*Query`:
- `func (q *Query) Terms() []flecs.ID` — read-only snapshot of the term list. Implementer's call: return a copy OR document do-not-mutate. Either is fine; pick one and stick with it.
- `func (q *Query) Iter() *QueryIter` — start a fresh iteration.
- `func (q *Query) Each(fn func(*QueryIter))` — convenience: invokes `Iter().Next()` in a loop and calls `fn` for each matching table. The `fn` parameter receives a `*QueryIter` already positioned on a table (so `Field[T](it, id)` works inside `fn`). Document that callers should NOT call `Next` themselves inside `fn`.

**3. `func NewQuery(w *World, ids ...flecs.ID) *Query`** — constructor.
- Validates: `w != nil`; `len(ids) >= 1` (zero-term queries match all entities — Phase 4 concern; for now panic on empty term list with a clear message; document).
- Stores a sorted copy of the term ids (uniqueness not strictly enforced; if the user passes the same id twice, the matching still works but it's wasteful — document but don't error).
- Does NOT precompute anything heavy. (Cached queries are Phase 6.)

**4. `type QueryIter struct`** — iterator state. Unexported fields. Exposes:
- `func (it *QueryIter) Next() bool` — advance to the next matching table. Returns false when no more matches.
- `func (it *QueryIter) Table() *table.Table` — the current matching table. Panics if called before `Next()` or after `Next()` returned false.
- `func (it *QueryIter) Count() int` — number of entities in the current table.
- `func (it *QueryIter) Entities() []flecs.ID` — read-only slice of entity IDs in the current table. Document do-not-mutate, invalid after `Next()`.
- `func (it *QueryIter) Query() *Query` — back-reference for callers that lost track.

**5. Generic typed-field accessor** (free function in the `flecs` package):
- `func Field[T any](it *QueryIter, id flecs.ID) []T`
- Returns a typed slice over the column for `id` in the current table.
- Sliced to exactly `it.Count()` (so users can range over it without bounds errors).
- Panics if: `id` is not in the current table's signature; `T` is not the registered Go type for `id`; `it` is not positioned on a table.
- Document: the returned slice is INVALIDATED by the next `it.Next()` call (because the next table has a different column). Callers must consume within the current iteration.
- Implementation: get the table's column for `id`, read its `Type reflect.Type`; assert `reflect.TypeFor[T]() == col.Type`; materialize a `[]T` via either `reflect.Value.Interface().([]T)` (clean, single interface allocation per call) OR `unsafe.Slice((*T)(col.PtrAt(0)), it.Count())` (zero-alloc, requires exposing column base pointer). **Implementer's choice — but prefer the reflect path for v0 because it's safer; document the chosen path and the perf tradeoff in a code comment so we know what to revisit.**

**6. Expose what's needed from `internal/storage/table`:**
- Either add `func (t *Table) ColumnReflectSlice(id flecs.ID) reflect.Value` (returns the typed slice value backing the column, sliced to current length) for the reflect path, OR add `func (t *Table) ColumnBase(id flecs.ID) (unsafe.Pointer, reflect.Type)` for the unsafe path.
- Pick one and add it. Either works as a public method.
- Document pointer/value stability (invalidated by `Append`/`RemoveSwap`).
- Tag (Size==0) columns: return zero `reflect.Value` / nil pointer respectively — `Field[T]` should handle that and return an empty `[]T` (length zero).

**7. Tests in `query_test.go`:**
- **Single-term query.** Register Position; create 3 entities, set Position on each with distinct values. `NewQuery(w, positionID)` matches all 3. Iterate; collect entities and positions; assert all 3 present.
- **Two-term AND.** Create entities: e1 with only Position, e2 with only Velocity, e3 with both. Query `(positionID, velocityID)` matches only e3. Verify e1 and e2 NOT visited.
- **Empty match.** Query for a registered component no entity has. `Iter().Next()` returns false immediately. `Each` invokes `fn` zero times.
- **Order independence.** `NewQuery(w, posID, velID)` and `NewQuery(w, velID, posID)` produce identical results (same set of tables, same entity ordering within each table).
- **Multiple matching tables.** Create entities in archetypes [P,V] and [P,V,Marker]. Query [P,V] matches BOTH tables. Iterate; verify entities from both tables visited; verify `Field[Position]` and `Field[Velocity]` yield correct values per table.
- **Field[T] correctness.** Set Position{1,2} on e1, Position{3,4} on e2. Query [P]; iterate; `Field[Position](it, positionID)` returns a slice; modify slice[0].X = 99; re-query; assert e1.Position.X == 99 (the slice is a live view).
- **Field[T] type-mismatch panic.** Query [Position]; iterate; call `Field[Velocity](it, positionID)` (wrong type for that id) — panics with a clear message.
- **Field[T] missing-id panic.** Query [Position]; iterate; call `Field[Velocity](it, velocityID)` (velocityID not in the table's signature) — panics.
- **Tag-component query.** Register a `type Marker struct{}` tag. Set on 2 entities. Query [markerID]; matches 2 entities. `Field[Marker](it, markerID)` returns a `[]Marker` of length 2 (containing zero-value entries — tag has Size 0, so the slice elements are degenerate; this is documented behavior).
- **GC-safe iteration.** Component `type WithStr struct { S string }`. Set on entity with a heap-allocated string. Force `runtime.GC()` twice. Query iterates; `Field[WithStr]` returns slice; assert string content survives.
- **Mutation during iteration NOT supported.** Document only — no test required, but include a comment in the QueryIter godoc explaining behavior is undefined if `Set`/`Remove`/`Delete` is called from inside `fn`.
- **Empty-terms-list panic.** `NewQuery(w)` with no ids panics with a clear message.
- **Smallest-set seeding correctness.** Set Position on 100 entities; set Velocity on 2 of them; query `(positionID, velocityID)` — the inner loop should NOT scan 100 tables (or however many tables are involved). Add a counter (test-only, via `export_test.go`) to verify the implementation actually seeds from the smaller set.

**8. Mechanical acceptance**
- `go test ./... -race -count=2` passes.
- `go vet ./...` clean.
- `golangci-lint run` clean.
- Coverage on `flecs` ≥ 90% (no regression from current 96.5%).
- Coverage on any new internal table accessor ≥ 90%.
- All exported symbols have godoc, including the perf tradeoff comment on `Field[T]`.

### Non-goals — defer to later phases

- NO Not terms, Optional terms, OR terms.
- NO pair terms, no wildcards (Phase 4).
- NO traversal modifiers (up/down/etc.; Phase 6).
- NO predicates / custom matchers.
- NO query caching / persistent queries (Phase 6).
- NO change detection / dirty tracking (Phase 6).
- NO sorting / grouping / order_by / group_by (Phase 6).
- NO observers / event firing (Phase 5).
- NO deferred operations (Phase 5).
- NO multi-threaded iteration (Phase 7).
- NO sparse-component or bitset-toggle column support.
- NO `Each2`/`Each3`/etc. helper functions for fixed-arity typed iteration (could be a nice ergonomic helper but defer; users can call `Field[T]` per term).
- NO field-index-based access (`ecs_field(it, 0)` style) — ID-based only in this phase.
- NO change to existing tests; they must stay green.

### Constraints / pointers for the implementer

- Read `src/query/engine/trivial_iter.c` for the algorithm shape: iterate over the smallest term's table set, filter the rest. The Go version doesn't need the compiler/eval-state machinery — just a direct loop.
- Use `World.EachTableFor(seedID, fn)` for the seed set (allocation-free). Use `Table.HasComponent(id)` to filter against the remaining terms.
- To pick the seed: scan the term list and select `argmin Count(id)` via `World` or `componentIndex.Count`. Expose `World.componentIndex.Count` if not already accessible — add a `World.tableCountFor(id) int` method or just `Count` if it doesn't collide.
- Iterator state machine: keep a slice of candidate tables from seed (or use the allocation-free `EachTableFor` and bridge to a `Next()` style — see implementation note below); keep an index pointing at the next-to-evaluate. For each candidate, check `HasComponent(otherTerm)` for all terms; if all pass, yield this table.
- About bridging `EachTableFor` (callback) to `Next()` (pull-style): a coroutine-based bridge is wasteful for v0. Either: (a) materialize the seed list once at `Iter()` time via `TablesFor(seedID)` (which already copies), or (b) capture the seed slice via a small wrapper, or (c) use Go 1.23+ range-over-func to avoid the materialization. Implementer's call. Go 1.23+ `iter.Seq` is fine to use internally but the public API exposed should still be the `Next()`-style `*QueryIter`.
- `Field[T]` should NOT use `reflect.Value.Interface().([]T)` slice conversion repeatedly per row — call ONCE per table (per `Next()`) at most. Users can store the result of `Field[T]` in a local `[]T` and range it.
- The table-side accessor (column-base or column-reflect-slice) must handle the tag-column nil case gracefully; document.
- Avoid creating a new type `EntityCallback`-style cycle. Query lives in the root `flecs` package; iterator can reach into `internal/storage/table` types directly since the package is internal.
- DO NOT modify the public API of `World`, `RegisterComponent`, `Set`, `Get`, `Has`, `Remove`, `Delete`. Only ADD.
- DO NOT add a query cache. Phase 6 is its own ticket.
- DO NOT import any third-party deps.

### Context — what's on master

Phase 1 + Phase 2 are complete. Master HEAD `1ba9611`. The repo has a working storage layer with:
- `flecs.World`, `flecs.RegisterComponent[T]/Set[T]/Get[T]/Has[T]/Remove[T]/Delete/IsAlive/Count/NewEntity`
- Archetype migration with edge cache (`internal/storage/table.Table` has `NextOnAdd`/`NextOnRemove`/`CacheAddEdge`/`CacheRemoveEdge`)
- Component reverse index: `internal/storage/componentindex.Index` with `Register/TablesFor/Each/Count/CountComponents`. Wired into World; exposed as `World.TablesFor(id)` (snapshot) and `World.EachTableFor(id, fn)` (allocation-free iteration).
- `internal/storage/entityindex.Index`, `internal/storage/table.Table`, `internal/component.Registry`, `internal/ids` leaf for the ID primitive.
- `.gitignore` exists; coverage artifacts are no longer committed.

## Constraints

- @world.go — public World API the query layer builds on; must not modify existing exported surface.
- @internal/storage/table/table.go — Table type the iterator returns; needs new column-access method (`ColumnReflectSlice` or `ColumnBase`).
- @internal/storage/table/column.go — Column metadata (Type, Size); the source of truth for tag-column (Size==0) handling.
- @internal/storage/componentindex/componentindex.go — reverse index used for smallest-set seed selection.
- @internal/component/typeinfo.go — registered Go type per component id, used by `Field[T]` type-mismatch check.
- @internal/component/registry.go — component registry the typed-field accessor consults.
- @id.go — the `flecs.ID` primitive used in all term lists.
- C reference (filesystem paths — read for algorithm shape, do not paraphrase):
  - `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` lines 4612-5956 (query/iterator public API shape)
  - `/work/agents/claude/projects/SanderMertens/flecs/src/query/api.c` (query construction)
  - `/work/agents/claude/projects/SanderMertens/flecs/src/query/engine/trivial_iter.c` (AND-only iteration — closest analog)
  - `/work/agents/claude/projects/SanderMertens/flecs/src/query/engine/eval_iter.c` (informational)
  - `/work/agents/claude/projects/SanderMertens/flecs/src/iter.c` (`ecs_field` implementation)
