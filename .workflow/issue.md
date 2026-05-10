## Goal

**Phase 1.5 — `World`: the public ECS facade.**

Phase 1.5 is the keystone of Phase 1. Phases 1.1–1.4 produced the building blocks (`flecs.ID`, the entity index, the component registry, archetype tables). This phase composes them into a working ECS and gives users a usable, typed Go API: create a world, register components (or auto-register on first use), create entities, set/get/has/remove components, delete entities.

The user-visible surface is small and lives in a new `world.go` at the root of the `flecs` package. The interesting machinery is the **archetype migration** — to `Set` a component an entity doesn't yet have, the World must find or create the destination table whose signature is the entity's current signature ∪ {new component}, copy existing column data over, swap-remove from the old table, and append to the new one. Phase 1.5 looks up the destination table by signature each time; the cached add/remove edges between tables (the C "table graph" in `src/storage/table_graph.c`) are deferred to Phase 2.

### Deliverables

#### 1. Tiny additive update to `internal/component/typeinfo.go`
Add field `Component flecs.ID` to `TypeInfo`. Default zero-value is `flecs.ID(0)` (invalid sentinel). Documented as "Set by `World` when the type is registered as a component-entity."

#### 2. Tiny additive update to `internal/component/registry.go`
- Add an internal `byID map[flecs.ID]*TypeInfo` index.
- `func (r *Registry) AssociateID(info *TypeInfo, id flecs.ID)` — sets `info.Component = id` and indexes by id. Panics if `id == 0` or if a different `TypeInfo` is already associated with that id (idempotent for same TypeInfo + same id).
- `func (r *Registry) LookupByID(id flecs.ID) (*TypeInfo, bool)`.
- Tests for both.

#### 3. Tiny additive update to `internal/component/typeinfo.go` — hook fields
Define a callback type. Avoid an import cycle by taking an opaque world `any`:

```go
// EntityCallback is invoked by the World on lifecycle events.
// The World is passed as `any` to avoid an import cycle; callers in the flecs
// package type-assert it back to *flecs.World.
type EntityCallback func(world any, entity flecs.ID, ptr unsafe.Pointer)
```

Add fields `OnAdd`, `OnSet`, `OnRemove EntityCallback` to `Hooks`. Default nil = no callback. **Wire-up of these callbacks (calling them from World) is deferred to Phase 5 (observers); Phase 1.5 just needs the fields to exist** so Phase 5 doesn't have to touch every call site. Add minimal tests that the fields can be set, read, and remain nil by default. Remove the placeholder comment "Reserved for OnAdd/OnSet/OnRemove — wired in Phase 1.5 when World exists."

#### 4. New file `world.go` in the top-level `flecs` package
Defines the public ECS API.

#### 5. `type World struct` with unexported fields composing
- `*entityindex.Index`
- `*component.Registry`
- A `tables map[string]*table.Table` (signature key → table). The signature key is a string-encoded sorted `[]flecs.ID` — implementer's call on the encoding (e.g. `string(unsafe.Slice(...))` over 8-byte ids, or hashed). For Phase 1.5, simplest is fine. Document the chosen encoding.
- Optionally an "empty-signature" canonical empty table (`*table.Table` with `Type()==nil`), so newly-created entities have a non-nil `Record.Table`. Implementer's call: nil-table or empty-table; just be consistent.

#### 6. World methods (non-generic)
- `func New() *World` — constructor. Initializes index, registry, tables map. Allocates the empty-signature table (if that pattern is chosen).
- `func (w *World) NewEntity() flecs.ID` — allocates an entity ID; sets its record table to the empty table (or leaves nil per pattern). Returns the entity.
- `func (w *World) Delete(e flecs.ID) bool` — if alive: RemoveSwap from current table (and update the moved entity's `Record.Row` accordingly), then `Free` the entity. Returns true if it was alive.
- `func (w *World) IsAlive(e flecs.ID) bool` — wraps `index.IsAlive`.
- `func (w *World) Count() int` — count of alive entities.

#### 7. Generic free functions in the same package
(Go does not allow generic methods, so these are package-level functions.)

- `func RegisterComponent[T any](w *World) flecs.ID` — idempotent: if `T` already registered, returns its existing component ID. Otherwise allocates a new entity ID via `w.Alloc()`, calls `component.Register[T](w.Registry)` to get the `*TypeInfo`, calls `w.Registry.AssociateID(info, id)`, and returns the id. Document that the component itself is an entity, mirroring flecs convention.
- `func Set[T any](w *World, e flecs.ID, v T)` — auto-registers `T` if not yet registered. Looks up `e`'s record (panics if not alive). If `e` already has component `T`: writes via `table.Set`, done. Otherwise performs an **archetype migration** as described in §8.
- `func Get[T any](w *World, e flecs.ID) (T, bool)` — returns `zero,false` if `T` not registered, if `e` not alive, or if `e` doesn't have `T`. Otherwise copies the column slot into a stack `T` and returns it. **Does NOT auto-register** (a Get on an unregistered type is a not-found, not a side effect).
- `func Has[T any](w *World, e flecs.ID) bool` — auto-registers `T` (so the answer is meaningful); returns true iff `e` is alive and its current table contains `T`'s id.
- `func Remove[T any](w *World, e flecs.ID) bool` — if `e` has `T`: performs an archetype migration to the table with signature minus `T`. Returns true on a successful removal, false if `e` didn't have `T` or wasn't alive.

#### 8. Archetype migration helper (unexported, in `world.go`)
The workhorse. Suggested signature:

```go
func (w *World) migrate(e flecs.ID, addID, removeID flecs.ID, copyValue unsafe.Pointer, copyValueSize uintptr)
```

Either or both of `addID` / `removeID` may be 0 (no-op for that side). The function:
1. Reads current record + current table.
2. Computes new signature: `current.Type() ∪ {addID} \ {removeID}`, sorted. **Do NOT mutate `Table.Type()`'s returned slice — compute new signatures in a fresh slice.**
3. Looks up or creates the destination table (`tables[sigKey]`). Creating a new table requires `[]*component.TypeInfo` — the world resolves these via `Registry.LookupByID`.
4. Appends a new row in the destination table with the entity ID; copies each column that exists in BOTH tables from old → new (use `table.Get(oldRow, id)` → `table.Set(newRow, id, ptr)`). When `addID != 0` and `copyValue != nil`, `table.Set(newRow, addID, copyValue)` — and SKIP `addID` in the carry loop (the source doesn't have it).
5. RemoveSwap the entity from the old table; if `moved == true`, the entity that was at the LAST row is now at the row vacated by the migrating entity — look up that entity's record and set `Row = oldRow`. **Document this carefully** — World is the caller of RemoveSwap and must maintain this invariant.
6. Update the migrating entity's record: `r.Table = newTable; r.Row = newRow`.

This routine is the main correctness-critical piece of 1.5. **Write it once, write the unit tests against it, then have `Set[T]`/`Remove[T]` call it.** Don't open-code migration in Set and Remove separately. Cover: add to empty entity, add to entity with prior components, remove from entity, remove leaving an empty signature, simultaneous add+remove (no-op overlap), tag-component add/remove (Size==0), GC-pointer component carry-over.

#### 9. Tests in `world_test.go` at the package root
Cover at minimum:
- `New().NewEntity()` returns a non-zero ID; `IsAlive`==true.
- Auto-registration: `Set[Position](w, e, p)` works without explicit `RegisterComponent`.
- Round-trip: `Set[Position](w, e, Position{1,2})` then `Get[Position](w, e)` returns `(Position{1,2}, true)`.
- Two-component flow: `Set[Position]` then `Set[Velocity]` triggers two archetype migrations; `Get` for both still works.
- `Has[Position]` correctness before/after `Set` and `Remove`.
- `Remove[Position]` actually removes; subsequent `Get` returns `zero,false`.
- `Delete(e)` removes the entity; `IsAlive`==false; component data not retrievable.
- **Multi-entity per archetype:** create 3 entities with `Position`; verify they share a table (`reflect.ValueOf(record.Table).Pointer()` equality — or expose a test-only helper).
- **Migration of co-located entity:** create e1, e2, e3 all with `Position`. `Set[Velocity](w, e2, v)` migrates e2. e1 and e3 stay together; e2 moves. After migration: `Get[Position](w, e1) == p1`, `Get[Position](w, e3) == p3` still — i.e., the swap-remove on the source table updates e3's `Record.Row` correctly (e3 was at row 2, e2 was at row 1; RemoveSwap moves e3 into row 1; e3's Record.Row must now be 1).
- **Delete in middle of table:** create e1, e2, e3 with `Position{1}`, `Position{2}`, `Position{3}`. Delete e2. `Get[Position](w, e3)` still returns `Position{3}`.
- **GC-pointer component:** type with a `string` field. Set, force GC, Get back. String survives.
- **Tag component (Size==0):** `Set[Tag](w, e, struct{}{})` adds the tag; `Has[Tag]` true; `Remove[Tag]` works.
- **Component re-registration (idempotent):** `RegisterComponent[Position]` twice returns the same ID.
- **Recycled entity ID:** create entity, set component, delete, create another (which recycles the index with bumped generation); old `Get` with the dead handle returns false; new entity is fresh, no leftover component data.

#### 10. Mechanical acceptance
- `go test ./... -race` passes.
- `go vet ./...` passes.
- `golangci-lint run` passes against the repo's `.golangci.yml`.
- Coverage on `flecs` (the root package additions) ≥ 90%.
- All exported symbols have godoc.
- The package-level docstring on `flecs` (in `world.go` or a new `doc.go` — implementer's call) describes the high-level model: world owns entities + components + tables; components are entities; archetype-based storage.
- **Existing tests stay green.** Don't regress `internal/component/`, `internal/storage/entityindex/`, `internal/storage/table/`, or `id_test.go`.

### Non-goals — defer to later phases
- NO queries, NO iteration over entities by component (Phase 3).
- NO observers — `Hooks.OnAdd`/`OnSet`/`OnRemove` fields exist but World does NOT invoke them yet (Phase 5).
- NO deferred commands (Phase 5).
- NO relationships / pairs / hierarchies (Phase 4) — `flecs.MakePair` exists but Set/Get/Has do not handle pair component IDs specially.
- NO table graph cache — every migration does signature lookup (Phase 2).
- NO concurrency / threading. Document that `*World` is NOT goroutine-safe.
- NO bulk operations (`SetMany`, etc.).
- NO lookups by name. Entity name is part of the `EcsIdentifier` component — that's Phase 4.
- NO addons.
- NO `unsafe.Pointer` exposure in the public API. Users see only typed `T` values.

### Implementer pointers
- Read `src/world.c` for `ecs_init` and `src/entity.c` for `ecs_set_id`/`ecs_get_id`/`ecs_new`/`ecs_delete`. Mirror the algorithm shape; drop deferred-command, hook-firing, and observer paths.
- The World owns ONE `component.Registry` — pass it to `component.Register[T]` calls inside `RegisterComponent[T]`.
- The signature key for the `tables` map: encode the sorted `[]flecs.ID` as a string. Simplest: `unsafe.Slice` to `[]byte` then string-convert (8 bytes per id). Faster: hash. For Phase 1.5, simplest is fine.
- The migration helper is the most error-prone piece. Write it once, then have `Set`/`Remove` call it.
- When migrating, the old row's component data must be carried over to the new table EXCEPT for the removed component (if any) and EXCEPT for the new component (which the caller's `copyValue` provides). When `addID != 0`, the destination has `addID` but the source doesn't — skip it in the carry loop.
- After RemoveSwap on the source table: if `moved == true`, the entity that was previously at the LAST row is now at the row vacated by the migrating entity. Look up that entity's record and set `Row = oldRow`.
- After Append on the destination table: the migrating entity's record is `Table = newTable; Row = newRow`.
- DO NOT mutate `Table.Type()`'s returned slice. Compute new signatures in a fresh slice.
- DO NOT use cgo. DO NOT add third-party deps.
- Auto-registration in `Set[T]`/`Has[T]` calls `RegisterComponent[T]` if `LookupByType[T]` returns false. `Get[T]` should NOT auto-register.

## Constraints

- @id.go — `flecs.ID` (`uint64`) entity/pair encoding; `MakeEntity`, `Index`, `Generation`, etc. The World allocates IDs via the entity index and constructs `flecs.ID` values through this API.
- @internal/component/typeinfo.go — `TypeInfo { Size, Align uintptr; Name string; Type reflect.Type; Hooks Hooks }`. This issue adds `Component flecs.ID` and `OnAdd/OnSet/OnRemove EntityCallback` fields. Pointer-stable `*TypeInfo` across registrations is a load-bearing invariant — `AssociateID` indexes by pointer-stable `*TypeInfo`.
- @internal/component/registry.go — `Registry` with `Register[T]`, `RegisterWithHooks[T]`, `LookupByType[T]`, `LookupByReflectType`, `Each`, `Count`. This issue adds `AssociateID` and `LookupByID`. The World owns one Registry.
- @internal/storage/entityindex/entityindex.go — `Index.Alloc/Free/IsAlive/Get/Count/Each`. `Record { Row uint32; Dense uint32; Table *table.Table }`. The World composes `*Index` and updates `Record.Table` and `Record.Row` during migration.
- @internal/storage/table/table.go — `Table` with `New(ids, types) → *Table`, `Append/RemoveSwap/Set/Get/Type/Count/Entities/HasComponent/ColumnIndex`. SoA reflect-backed columns; GC-safe; tags supported via nil column slots; **signatures must be sorted**. The migration helper appends/removes rows and copies column data through this API.
- @internal/storage/table/column.go — column implementation (reflect-backed SoA, GC-safe). Tag components use nil column slots — migration must handle `Size==0` correctly.
- C reference (filesystem paths — cite, do not @-reference): `/work/agents/claude/projects/SanderMertens/flecs/src/world.c` (`ecs_init`, top-level lifecycle), `/work/agents/claude/projects/SanderMertens/flecs/src/entity.c` (`ecs_new`, `ecs_set_id`, `ecs_get_id`, `ecs_delete`, `ecs_has_id` — read function bodies for migration shape; ignore deferred-command paths and observer firing for this phase), `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` lines 2946–3976 (public entity API surface, for context on what Phase 1+ is building toward).

