## Goal

Add a runtime component-registration path so callers can register a component whose Go type is unknown at compile time. The caller supplies only `size` and `alignment`; storage treats the data as opaque bytes. This is the substrate that downstream scripting / hot-reload / schema-driven serialization layers need, but the layers themselves are out of scope.

Today's API gates every component on a Go generic `T`:

```
flecs.RegisterComponent[Position](w)
```

The `T` parameter is what gives storage typed access to the value and what lets the table column know its element type via `reflect.SliceOf(reflect.TypeOf(T))`. A dynamic component cannot supply `T`. It supplies size + alignment + a debug name, and accepts the loss of generic helpers (`Get[T]`, `Each[T]`, `Field[T]`, typed `OnAdd[T]`) in exchange for that flexibility.

### Closes the v0.68.0 gap

`docs/README.md` line 102 currently reads:

> *Runtime (dynamic) component registration — register a component whose Go type is unknown at compile time (only size + alignment known; used by scripting layers). not yet ported in Go flecs.*

After this phase, line 102 flips to "shipped in v0.68.0" with a docs link to the new section in `docs/EntitiesComponents.md`.

### C upstream parity (verified against `/work/agents/claude/projects/SanderMertens/flecs`)

- `ecs_component_init` is defined at `include/flecs.h` line 4368 and implemented at `src/entity.c` line 1532. It takes a `ecs_component_desc_t` containing an `ecs_type_info_t` with explicit `size` and `alignment` fields (`include/flecs.h` lines 1023-1029 and 1122-1130). Confirmed.
- The default ctor / dtor when no hooks are provided is `flecs_default_ctor` at `src/type_info.c` line 20 — pure `memset(ptr, 0, size * count)` to zero-fill. The default dtor (`src/type_info.c` line 234) is `memcpy`-equivalent. POD fallback for no-hook components confirmed.
- Bootstrap path: `ecs_component_init` allocates a low entity ID via `ecs_new_low_id` (`src/entity.c` line 1560), then calls `ecs_ensure(world, result, EcsComponent)` (line 1566) to add the `EcsComponent` reflection tag to the new entity. The Go port's analog is `w.index.Alloc()` + `w.registry.AssociateID(info, id)` from `world.go` line 996-997 — there is no `EcsComponent` reflection entity in the Go port today; component metadata lives in the registry only. That divergence is preserved.

### Design — the typed access barrier

`Get[T]` / `Each[T]` / `Field[T]` bind to a Go type at compile time. They are unusable for dynamic components by construction. The proposal adds an untyped, pointer-based API surface that accepts both dynamic and typed components uniformly:

1. **`GetIDPtr(r *Reader, e ID, componentID ID) unsafe.Pointer`** — returns a raw pointer to the component's live storage slot, or nil if the entity does not hold the component. Caller knows size and structure from the registration site.
2. **`SetIDPtr(fw *Writer, e ID, componentID ID, src unsafe.Pointer)`** — copies `size` bytes from `src`. Triggers OnAdd / OnSet / OnReplace exactly like typed `Set`.
3. **`EachByID(s scope, componentID ID, fn func(e ID, ptr unsafe.Pointer))`** — iterates all entities holding the component, with raw pointer access per entity.
   - **Naming note**: the user-facing proposal called this `EachID`, but `*entityindex.Index.EachID` (`internal/storage/entityindex/entityindex.go` line 243) already exists, and `(*Reader).EachEntity` (`scope.go` line 259) is the public iterator for all alive entities. Recommend `EachByID` (mirrors `GetByID` / `SetByID`) to avoid confusion with the existing entity-only iterators. Open for finalization during implementation.

All three accept dynamic OR typed components, so callers do not branch on registration kind.

### API tradeoff (state in the doc)

Dynamic components sacrifice the Go-typed safety net. The compiler will not catch a size mismatch between registration site and access site; the user is responsible for the size discipline. The win is the ability to register components whose layout is determined at runtime — required for scripting reflection, hot-reload, and schema-driven serializers.

### Open decision points (locked-in defaults)

1. **Pointer lifetime contract** — pointer returned by `GetIDPtr` is valid until the next archetype migration on the entity. Matches existing archetype storage semantics (column re-allocation invalidates pointers). Document explicitly; tests cover the stale-pointer scenario.
2. **Re-registration with the same name** — panic. Surfaces programming errors at registration time rather than silently aliasing. Document.
3. **Default marshaler** — base64-encoded opaque bytes inside JSON. Custom marshaler hook available via `RegisterDynamicComponentWithMarshaler`.
4. **Alignment** — caller specifies. Explicit and flexible; no implicit promotion to 8-byte alignment.
5. **Storage routing** — dynamic components flow through the SAME archetype / sparse / DontFragment machinery as typed components. No parallel storage path. The fork-point is exclusively in `TypeInfo` construction (no `Type` field, hooks zero, `Name` user-provided), and downstream code at `internal/storage/table/table.go` lines 32-67 / `sparse.go` lines 130-157 needs the size+align fields populated correctly. Confirm via test compositions.

### Storage compatibility — the one place this is structural

`internal/storage/table/column.go` line 36-42 (`newColumn`) calls `reflect.MakeSlice(reflect.SliceOf(elemType), …)` — it requires a non-nil `reflect.Type`. Likewise `sparse.go` line 144 calls `reflect.New(info.Type)` for per-entry allocation.

Dynamic components have no `reflect.Type`. The implementation must:

- For the table column: choose a sentinel "raw bytes" backing — likely a `[N]byte` array type synthesized via `reflect.ArrayOf(int(info.Size), reflect.TypeOf(byte(0)))` so the slice is still a typed Go slice and the GC sees no pointers (POD-only constraint). Alternatively, special-case columns with `info.Type == nil` and back them with a manually-grown `unsafe.Pointer`-aligned byte buffer; trade-offs to evaluate.
- For sparse storage: heap-allocate a `[]byte` of `info.Size` bytes (or a `reflect.New` on the array-of-bytes synthesized type) and take its data pointer. Aligned via `runtime.SetFinalizer`-free allocation; alignment guaranteed by the caller's `info.Align` request when using `unsafe.UnsafePointer` allocations from `unsafe`.

Locking in: synthesize a `reflect.ArrayOf(int(info.Size), byte)`-style backing when `info.Type == nil`. This keeps every downstream path (`reflect.SliceOf`, `reflect.NewAt`, GC tracing) working unchanged. Document the POD restriction.

### Deliverables

1. **New entry point** in a new file `component_dynamic.go` (top-level) plus supporting work in `internal/component/registry.go`:
   - `RegisterDynamicComponent(w *Writer, name string, size, alignment uintptr) ID` — allocates a new entity ID, constructs a `TypeInfo` with `Size`/`Align` set, `Hooks` zero, `Type` either nil or a synthesized POD array-of-bytes (decision per the storage section above), `Name` = the user-supplied path-style string (`"scripting/PlayerStats"`).
   - Panics on name collision with a previously-registered dynamic component.
   - Optional: `RegisterDynamicComponentWithMarshaler(w, name, size, align, marshaler, unmarshaler) ID` for custom (de)serializers.

2. **Untyped access API**:
   - `GetIDPtr(r *Reader, e ID, componentID ID) unsafe.Pointer` — nil on miss; pointer valid until next archetype migration on `e`.
   - `SetIDPtr(fw *Writer, e ID, componentID ID, src unsafe.Pointer)` — copies `info.Size` bytes; fires OnAdd / OnSet / OnReplace.
   - `EachByID(s scope, componentID ID, fn func(e ID, ptr unsafe.Pointer))` — iterates all holders.
   - Each function accepts dynamic OR typed component IDs; routes via the existing `TypeInfo` lookup.

3. **Storage compatibility**:
   - Archetype storage: dynamic components live in archetype columns the same as typed ones; column backing synthesized when `Type == nil`.
   - Sparse storage: works for dynamic components; same allocation strategy.
   - Confirm DontFragment, Union, OrderedChildren, Singleton compose orthogonally. Test the most common compositions.

4. **Marshal / unmarshal** (`marshal.go`):
   - Dynamic component values serialize as base64-encoded raw bytes (`json.RawMessage` containing a base64 string) under `info.Name` in the entity's `Components` map.
   - Custom marshaler/unmarshaler hooks override the base64 default when registered.
   - Round-trip test: register a dynamic component in `worldA`, set values on entities, marshal, unmarshal in a fresh `worldB` after re-registering the same name+size+align, verify byte-exact equality.

5. **Hooks** (`hooks.go` / `observer.go`):
   - OnAdd / OnSet / OnRemove / OnReplace observers can subscribe to dynamic components by ID. Handler signature: `func(fw *Writer, e ID, ptr unsafe.Pointer)`.
   - Typed-hook generics (`OnAdd[T]`, etc.) remain typed-only and are unusable for dynamic components. Document this restriction explicitly.

6. **Tests** in `component_dynamic_test.go` — minimum 12 cases:
   - Register size-16 component; set value; round-trip get matches input bytes.
   - Multiple dynamic components with distinct sizes (e.g. 8, 16, 32) on the same world; verify no cross-contamination.
   - Dynamic + typed component coexist on the same entity (each carries its own column).
   - Dynamic component on sparse storage (`SetSparse` works).
   - Dynamic component on DontFragment storage.
   - Marshal round-trip with default base64 serializer.
   - Marshal round-trip with custom serializer (e.g. JSON-encoded `int32`).
   - OnSet observer on a dynamic component fires with the correct pointer; value bytes match what was set.
   - `EachByID` iterates every holder; verify count and per-entity pointer.
   - Pointer lifetime: get pointer, add another component to the same entity (triggers archetype migration), re-get pointer; document and assert the first pointer is stale.
   - Alignment correctness: register with alignment 8; set value; assert returned pointer satisfies `uintptr(ptr) % 8 == 0`.
   - Re-registration with the same name panics with a clear message.
   - Coverage ≥ 95.0%.

7. **Doc updates** per `CONTRIBUTING.md`:
   - `docs/EntitiesComponents.md` — new "Dynamic component registration" section with code example, the API-tradeoff explanation, and the pointer-lifetime contract.
   - `docs/README.md` line 102 — flip to "shipped in v0.68.0".
   - `README.md` — feature-list bump.
   - `CHANGELOG.md` — v0.68.0 entry at top (above v0.67.0).
   - `ROADMAP.md` — heading bump to "Shipped (through v0.68.0)" and a Phase 16.13 line in the shipped list.

### Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run` clean.
- `go test ./... -race -count=3` passes.
- Coverage ≥ 95.0%.
- No regression on the existing typed-component test suite.
- Race-detector clean — dynamic-component pointer access honors existing `Read`/`Write` scope checks via `w.checkExclusiveAccessRead()` / `w.checkExclusiveAccessWrite()`.

### Explicit non-goals

- No scripting-language integration (Lua / Python / Wasm bindings). This phase delivers the substrate; binding layers are downstream.
- No automatic alignment inference. Caller supplies `alignment` explicitly.
- No automatic dtor or non-POD storage for dynamic components. POD-only. Callers who need destructors register typed components.
- No name-resolution lookup API like `GetComponentByName("Position")`. Naming layer is a separate concern.

## Constraints

- @CONTRIBUTING.md — every phase ships docs (`docs/`, `README.md`, `CHANGELOG.md`, `ROADMAP.md`) in the same commit as the code; release version bump in `version.go` and CHANGELOG entry; coverage gate ≥ 95.0%.
- @docs/README.md — line 102 is the gap entry this phase closes; flip status and add doc link in the same commit.
- @internal/component/typeinfo.go — `TypeInfo` is the data structure dynamic components produce; new path constructs one with `Hooks` zero, `Type` synthesized POD or nil, `Size`/`Align` explicit. No new fields on `TypeInfo` needed.
- @internal/component/registry.go — `Register[T]` (line 41) and `RegisterPairDataByType` (line 158) are the existing patterns; new `RegisterDynamicByName(r, name, size, align)` follows the same shape but does not key on `reflect.Type`. `EnsureID` (line 123) demonstrates the precedent for "TypeInfo without a Go type" via the `tagType` sentinel.
- @internal/storage/table/column.go — `newColumn` (line 36) calls `reflect.SliceOf(elemType)`; dynamic components synthesize the element type as `reflect.ArrayOf(int(size), byte)` or equivalent so this path is unchanged.
- @sparse.go — `sparseSetInsert` (line 131) uses `reflect.New(info.Type)` for per-entry allocation; dynamic components need an analogous byte-array allocation path.
- @marshal.go — `MarshalJSON` / `UnmarshalJSON` currently gate on `info.Type != nil` (line 282, 472, 579, 634); add the base64-bytes path for `info.Type == nil` (or for dynamic components specifically).
- @value_ops.go — `setImmediateByPtr` (line 280) is already non-generic and pointer-based; `SetIDPtr` is essentially a public wrapper that does not need a `reflect.TypeOf` check (since the dynamic component has no Go type). Existing typed-`SetByID` (line 249) keeps its `reflect.TypeOf` guard.
- @world.go — `RegisterComponent[T]` (line 987) is the model: allocate entity ID via `w.index.Alloc()`, associate via `w.registry.AssociateID(info, id)`, log via `w.logger.LogAttrs`. Dynamic registration follows the same shape.
- @ROADMAP.md — Shipped header bumps to "through v0.68.0"; Phase 16.13 line lands in the shipped list with feature summary mirroring the Phase 16.12 entry style on line 71.
- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h:1023-1029, 1118-1130, 4368-4370` — C upstream `ecs_type_info_t` / `ecs_component_desc_t` / `ecs_component_init` define the API parity reference.
- `/work/agents/claude/projects/SanderMertens/flecs/src/entity.c:1532-1612` — `ecs_component_init` implementation. The Go port deliberately omits the `EcsComponent` reflection-tag step since component metadata lives in the registry only.
- `/work/agents/claude/projects/SanderMertens/flecs/src/type_info.c:20-26, 234-243` — `flecs_default_ctor` (memset zero) and the no-hook dtor fallback (memcpy) confirm the POD semantics for dynamic components.
