## Goal

Port the heart of the ECS storage layer: archetype-based component storage with a single-archetype `Table` type. ONE table per unique component-id signature; components live in SoA columns (one column per component, indexed by row) alongside a parallel entity-ID column. Rows are added via `Append` and removed via `RemoveSwap` (last row swaps into the removed slot). Components with GC pointers (strings, slices, maps) MUST be tracked correctly by the GC.

This is the storage primitive the World will sit on top of in Phase 1.5. It does NOT include the table graph or component add/remove (those cause archetype migrations and land in Phase 2). A Table here has a fixed signature for its lifetime.

### Deliverables

#### 1. Additive update to `internal/component/typeinfo.go`

- Add field `Type reflect.Type` to `TypeInfo`.
- Populate `Type` from `Register[T]` and `RegisterWithHooks[T]`.
- Update godoc on the struct so the new field is described.
- Add a single test asserting `info.Type == reflect.TypeFor[Foo]()` for a registered struct.
- No other changes to `internal/component`.

This is needed so tables can construct typed slices via `reflect.SliceOf(info.Type)`.

#### 2. Additive update to `internal/storage/entityindex/entityindex.go`

- Add field `Table *table.Table` to `Record` (requires importing `github.com/snichols/flecs/internal/storage/table`).
- On `Free`, also clear `r.Table = nil` so dead records don't retain table pointers.
- Remove the Phase 1.2 TODO comment that anticipated this field.
- Add one test in `entityindex_test.go` that creates a fake `*table.Table` (zero-value pointer is fine), assigns it via `Get`, retrieves it, and confirms `Free` zeros it.

#### 3. New package `internal/storage/table`

Files: `table.go`, `column.go`, plus `table_test.go` and `column_test.go`.

Add a doc comment at the top of `table.go` describing the SoA layout and the swap-remove model so future readers understand the invariant.

#### 4. `type Column struct` in `column.go`

Internal mechanics — implementer's call on field shape — but it MUST satisfy:

- Stores typed elements such that the GC traces pointer-containing components correctly. Recommended: hold a `reflect.Value` of `[]T` (where T is the component type), grown via `reflect.MakeSlice` / `reflect.Copy`. DO NOT use `[]byte` for the backing store — that hides pointers from the GC and silently corrupts components like `struct { Name string }`.
- Exposes:
  - `Len() int`, `Cap() int`
  - `PtrAt(row int) unsafe.Pointer` — pointer to the element at `row`. Stable until the next operation that grows the column. Document this contract.
  - `Set(row int, src unsafe.Pointer)` — copy `Size` bytes from `src` into the column's row-th slot.
  - `Get(row int, dst unsafe.Pointer)` — copy `Size` bytes out (used by callers without the row pointer).
  - `appendZero()` (unexported) or equivalent — extend length by one with zero-value element. Grow the underlying slice via doubling when cap is exceeded.
  - `removeSwap(row int)` (unexported) — overwrite slot `row` with the last element, then truncate length by one. If `row == Len()-1`, just truncate. Document semantics.
- Zero-size components (tags): the column should still be created and respond to `Len`/`Cap` correctly; `Set`/`Get`/`PtrAt` may noop. Decide and document.

#### 5. `type Table struct` in `table.go`

Methods:

- `func New(ids []flecs.ID, types []*component.TypeInfo) *Table` — construct. `len(ids) == len(types)`; tag-style components have `info.Size==0`. The `ids` slice is the canonical component-id signature; the caller must pass it sorted-ascending (document this; do NOT sort inside `New`). Validate via `sort.SliceIsSorted` and panic if not sorted. Allocate one `Column` per id where `info.Size > 0`; for tags (Size==0), the column slot in the internal `columns` slice may be nil — design the lookup to handle this.
- `func (t *Table) Type() []flecs.ID` — return the signature. May return the underlying slice; document that it is read-only.
- `func (t *Table) Count() int` — current row count.
- `func (t *Table) Entities() []flecs.ID` — read-only view of the entity column. Document do-not-mutate.
- `func (t *Table) HasComponent(id flecs.ID) bool`.
- `func (t *Table) ColumnIndex(id flecs.ID) int` — index of the column for `id`, or -1 if not present. O(log n) via binary search OR O(1) via a precomputed `map[flecs.ID]int` (implementer's call; for archetypes >8 components a map wins). Document the choice.
- `func (t *Table) Append(entity flecs.ID) int` — append a new row for `entity`, all component columns zero-initialized. Returns the row index (== `Count()-1` after the call).
- `func (t *Table) RemoveSwap(row int) (movedEntity flecs.ID, moved bool)` — swap-remove `row`. If `row` was not the last row, the previously-last entity is moved into `row`; `movedEntity` is that entity's ID and `moved` is true. If `row` was the last row, returns `(0, false)`. The caller (World, Phase 1.5) is responsible for updating the moved entity's `Record.Row` to `row`. Out-of-range `row` panics.
- `func (t *Table) Set(row int, id flecs.ID, src unsafe.Pointer)` — copy bytes into the column for `id` at `row`. Panics if `id` is not in the signature or if `row` is out of range. For tag components (Size==0): `Set` is a noop — document this. Source pointer must point to at least `Size` bytes of the matching type's representation.
- `func (t *Table) Get(row int, id flecs.ID) unsafe.Pointer` — return a pointer to the column slot. Panics on missing id / out of range. For tags, returns nil (or a documented dummy pointer). The returned pointer is stable until the next `Append` (which may grow columns) or `RemoveSwap` (which may move data via swap).

#### 6. Tests

Cover at minimum:

- Construct table with two non-tag components (`Position{X,Y float32}`, `Velocity{X,Y float32}`); `Type()` returns the signature; `HasComponent` true for each; `Count()==0`.
- Append three entities; `Count()==3`; `Entities()` returns those three in order.
- Set/Get round-trip on each component column for each row. Use `unsafe.Pointer(&pos)` from a stack-allocated `Position` value.
- **GC pointer tracing.** Define `type WithStr struct { S string }`. Append entity, set component with `S = strings.Repeat(\"a\", 1<<10)` (real heap string), `runtime.GC()` twice, read back, assert string content. This proves the column doesn't hide GC pointers.
- **RemoveSwap (middle).** Append e1, e2, e3 with distinct Position values. RemoveSwap row 0. Returns `(e3, true)`. `Count()==2`. `Get(row=0, Position) == e3's position`. `Entities() == [e3, e2]`.
- **RemoveSwap (last).** Append e1, e2. RemoveSwap row 1. Returns `(0, false)`. `Count()==1`. `Entities() == [e1]`.
- **RemoveSwap (only).** Append e1. RemoveSwap row 0. Returns `(0, false)`. `Count()==0`.
- **Growth.** Append 1024 entities (forces several internal grows). All round-trip Set/Get correctly. Pointer-stability after a grow is NOT guaranteed (document this in `PtrAt`/`Get`); the test should NOT cache pointers across appends.
- **Tag component (Size==0).** Construct table with `Position` and `type Marker struct{}`. Append entity, Set/Get on Position works; Set/Get on Marker is a noop (matches documented behavior).
- **Empty signature.** Construct with no components. Append entity, RemoveSwap. Works as a pure entity column.
- **`ColumnIndex` correctness** for present and absent ids.
- **Unsorted-signature panic.** Construct with reversed-order ids — panics.
- **Out-of-range panics** on `Get`, `Set`, `RemoveSwap`.

#### 7. Mechanical acceptance

- `go test ./... -race` passes (including the GC test — verify it passes consistently).
- `go vet ./...` passes.
- `golangci-lint run` passes.
- Coverage on `internal/storage/table` >= 90% (aim for 95% if achievable; some `unsafe`/`reflect` paths are awkward to cover).
- All exported symbols have godoc.

### Implementer notes

- Read `/work/agents/claude/projects/SanderMertens/flecs/src/storage/table.h` lines 98-168 for `ecs_table__t`, `ecs_table_t`, `ecs_column_t`, plus the embedded `data` struct. Read `/work/agents/claude/projects/SanderMertens/flecs/src/storage/table.c` for `flecs_table_grow_data`, swap-remove, and column setup. Mirror the algorithm using Go-native primitives.
- Use `reflect.MakeSlice(reflect.SliceOf(typeInfo.Type), len, cap)` to materialize columns. Use `reflect.Value.Index(i).UnsafeAddr()` for `PtrAt`. Use `runtime.KeepAlive(slice)` after `UnsafeAddr` if the compiler is suspected of dropping it (test will fail if so).
- For `Set`/`Get` byte-copy: use `copy` on byte slices via `unsafe.Slice((*byte)(ptr), size)`. Document that this works because `unsafe.Slice` produces a `[]byte` whose backing array is the same memory.
- Do NOT use `unsafe.Slice` to materialize a `[]byte` view of pointer-containing component memory and pass it across hot loops — only use it locally inside `Set`/`Get` for the explicit copy.
- Growth policy: double the capacity when full. Initial cap of 8 is fine.
- The `entities` column is NOT a `Column` (no TypeInfo); it's a plain `[]flecs.ID` slice managed directly by the table.

### Non-goals (deferred to later phases)

- NO table graph (`add`/`remove` component / archetype migrations) — Phase 2.
- NO component-id flag-bit handling beyond storage (no toggle bitset, no sparse-column path) — Phase 2/later.
- NO queries, no iter API — Phase 3.
- NO observers, no commands — Phase 5.
- NO `World` integration — Phase 1.5 wires Table into World.
- NO reuse-after-free of column memory; let GC handle dropped tables.
- NO custom allocators / `sync.Pool` — plain Go allocation.
- NO concurrency — Tables are NOT goroutine-safe; document this.

## Constraints

- @id.go — `flecs.ID` (`uint64`) is the entity/pair encoding used as the column key and entity-column element type.
- @internal/component/typeinfo.go — `TypeInfo { Size, Align uintptr; Name string; Hooks { Move ... } }`. Phase 1.4 adds `Type reflect.Type` here; tables consume `Size` for byte-copy and `Type` for `reflect.SliceOf`. Pointer-stable TypeInfo is part of the contract.
- @internal/component/registry.go — `Register[T]`, `RegisterWithHooks[T]`, `LookupByType[T]`, `LookupByReflectType`, `Each`, `Count`. Registration is idempotent; both `Register*` functions must populate the new `Type` field.
- @internal/storage/entityindex/entityindex.go — `Record { Row uint32; Dense uint32 }` plus the existing TODO for the `Table *table.Table` field that THIS issue adds. `Free` must clear the new field.
- C reference (cite, do not paraphrase): `/work/agents/claude/projects/SanderMertens/flecs/src/storage/table.h` (lines 98-168 for `ecs_table__t`, `ecs_table_t`, `ecs_column_t`, embedded `data` struct).
- C reference: `/work/agents/claude/projects/SanderMertens/flecs/src/storage/table.c` for `flecs_table_grow_data`, swap-remove, column setup. Mirror algorithm; ignore table-graph machinery, table-cache hookups, sparse columns, and bitset toggle columns (those are later phases).
