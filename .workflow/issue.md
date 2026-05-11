## Goal

Add non-generic `GetByID` and `SetByID` methods on `*World` so callers can read and write component values when only the component `ID` is known at runtime — not the Go type parameter. This is the missing primitive for Phase 9.2 (JSON serialization), where the serializer walks `EntityComponents(e)` and needs the boxed value per component without compile-time `T`.

Phase 9.1 exposed introspection (`Components()`, `ComponentInfo()`, `EntityComponents()`, `EachEntity()`, `AliveEntities()`), but values remain locked behind generics. This phase opens the value plane.

After this lands:

```go
posID := flecs.RegisterComponent[Position](w)
flecs.Set[Position](w, e, Position{X: 1, Y: 2})

// Read by ID, get back an interface{}.
v, ok := w.GetByID(e, posID)
if ok {
    pos := v.(Position)  // type assertion
    fmt.Println(pos.X, pos.Y)
}

// Write by ID, accept any compatible value.
w.SetByID(e, posID, Position{X: 99, Y: 99})  // OK
w.SetByID(e, posID, "not a Position")        // panic: type mismatch
```

Serialization use (Phase 9.2 preview — not implemented here):

```go
for _, id := range w.EntityComponents(e) {
    info, ok := w.ComponentInfo(id)
    if !ok || info.Size == 0 { continue }  // skip tags

    v, ok := w.GetByID(e, id)
    if !ok { continue }

    bytes, err := json.Marshal(v)
    // ... emit as JSON
}
```

### Deliverables

**1. Two new methods on `*World`** — landing in a new `value_ops.go` (or appended to `id_ops.go` — implementer's call):

- `func (w *World) GetByID(e ID, id ID) (any, bool)`
  - If `e` is not alive → `(nil, false)`.
  - If `id` is not a registered component (no `*TypeInfo`) → `(nil, false)`.
  - If `e`'s table doesn't have `id` → check IsA fallback (matches `Get[T]`'s inheritance-aware semantics). If still no match → `(nil, false)`.
  - If found, materialize the value: `reflect.NewAt(info.Type, ptr).Elem().Interface()` to box the column slot into an `any`.
  - For tag components (`info.Size == 0`), return `(struct{}{}, true)` — the zero-value of the registered type. Document.
  - Returns ONE allocation per call (the interface header for boxing). Document the perf characteristic.

- `func (w *World) SetByID(e ID, id ID, v any)`
  - Panics if `e` is not alive (matches `Set[T]` panic policy).
  - Panics if `id` is not a registered component.
  - Panics if `reflect.TypeOf(v)` does not match the component's `info.Type` — critical type-safety check; without it, the unsafe write would corrupt memory. Document.
  - For tag components, accept any `v` with the right type and write nothing (no-op on the column). `info.Size == 0`.
  - For data components, perform archetype migration if needed (via existing `migrate()` plumbing), then copy `v`'s bytes into the column slot via `reflect.ValueOf(v).UnsafeAddr()` → `memmove` of `info.Size` bytes.
  - Fires OnAdd (if newly added) and OnSet hooks/observers, mirroring the typed `Set[T]` path.
  - Honors the Defer queue: if `w.IsDeferred()`, queues the operation and applies on flush. The queued closure captures `v` and uses `SetByID` semantics at flush time.

**2. Internal helper extraction:** factor the migration + write core out of `setImmediate[T]` (added in Phase 5.3) into a non-generic `setImmediateByPtr(w, e, id, srcPtr, info)`:

- `setImmediate[T]` builds `unsafe.Pointer(&v)` and delegates to `setImmediateByPtr`.
- `SetByID` checks the type assertion, then obtains an addressable pointer to `v`. Since `reflect.ValueOf(v)` over an `any` argument is not addressable, allocate a one-element bounce buffer via `pv := reflect.New(info.Type); pv.Elem().Set(reflect.ValueOf(v))`, then use `unsafe.Pointer(pv.Pointer())` (or `pv.UnsafeAddr()` after `Elem()`).
- This refactor isolates the migration + write logic from the type-parameter unwinding. Do NOT duplicate migration logic.

**3. Pair ID support:**

- `GetByID(e, MakePair(R, T))`: returns the pair's data (if the pair was set via `SetPair[T]`) or `(struct{}{}, true)` for tag pairs.
- `SetByID(e, MakePair(R, T), v)`: panics if the pair's TypeInfo is incompatible with `v`'s `reflect.Type`. If the pair is unregistered, panic — we do NOT auto-register pair TypeInfos for the dynamic path. Caller must `SetPair[T]` explicitly first to register, OR use `AddID` for tag pairs.

**4. Tests** in `value_ops_test.go`:

- GetByID basics: register Position, Set, GetByID returns Position value with correct X/Y.
- GetByID on tag component: returns `struct{}{}`; ok=true.
- GetByID on dead entity: returns nil, false.
- GetByID on unregistered ID: returns nil, false.
- GetByID on missing component: entity exists but doesn't have the component → nil, false.
- GetByID with IsA inheritance: child with (IsA, prefab) where prefab has Position → returns prefab's Position via GetByID.
- SetByID basics: SetByID(e, posID, Position{1,2}); GetByID returns Position{1,2}.
- SetByID with type mismatch panics: SetByID(e, posID, "wrong"); panic.
- SetByID on dead entity panics: matches Set[T] policy.
- SetByID auto-migrates archetype: entity without Position; SetByID(e, posID, Position{...}); entity now has Position.
- SetByID fires OnAdd + OnSet: register hooks; SetByID; both fire.
- SetByID fires observer: register observer; SetByID; observer fires.
- GetByID via SetPair data: SetPair[Edge](w, e, R, T, Edge{}); pairID := MakePair(R, T); GetByID(e, pairID) returns Edge value.
- SetByID on pair-id: SetByID(e, MakePair(R, T), Edge{...}); GetByID returns the value.
- GetByID on tag pair: AddID(e, MakePair(R, T)) (no data); GetByID returns `struct{}{}`, true.
- SetByID respects Defer: wrap in Defer(func() { SetByID(...) }); read shows old value inside, new value after.
- Existing tests stay green: Set[T]/Get[T] behavior unchanged.

**5. Mechanical acceptance:**

- `go test ./... -race -count=2` passes.
- `go vet ./...` clean.
- `golangci-lint run` clean (any new `unsafe` usage commented).
- Coverage on `flecs` ≥ 90% (no regression from 97.1%).
- All exported symbols have godoc.

**6. Documentation:**

- Godoc on `GetByID` documents: returns interface header (1 alloc/call); inheritance-aware (IsA fallback); never panics.
- Godoc on `SetByID` documents: panics on dead entity / unregistered id / type mismatch; fires hooks/observers; honors Defer queue.
- Add a note to `doc.go` explaining when to use `GetByID`/`SetByID` vs `Get[T]`/`Set[T]` — TL;DR: typed when possible, GetByID/SetByID when the ID is only known at runtime.

### Non-goals

- NO JSON / serialization (Phase 9.2).
- NO bulk Set/Get APIs.
- NO component data conversion / migration utilities.
- NO reflection-based field walking.
- NO `GetID[T any](w, e, id ID)` generic variant — use `Get[T]` for typed access.
- NO automatic pair-data registration via SetByID — caller must `SetPair[T]` first.
- NO change to `Get[T]` / `Set[T]` signatures or behavior.
- NO change to `migrate()` public contract.
- NO bypass of hook/observer dispatch.
- NO non-defer-aware path. SetByID honors the defer queue.

### Constraints / pointers for the implementer

- Use `reflect.NewAt(info.Type, unsafe.Pointer)` to materialize a typed Value over the column's memory; `.Elem().Interface()` boxes it into `any`.
- For SetByID's address-of-v: `reflect.ValueOf(v)` returns a Value over `v`'s memory; if it's not addressable (it usually isn't for an `any` parameter), allocate via `reflect.New(info.Type)`, `Elem().Set(reflect.ValueOf(v))`, then take `Pointer()`/`UnsafeAddr()`. This is a one-alloc bounce buffer per SetByID call. Document.
- The type assertion check: `reflect.TypeOf(v) != info.Type` → panic with a message like `"SetByID: type mismatch for component %s (id=%d); expected %s, got %s"`.
- The shared `setImmediateByPtr` helper extracted from `setImmediate[T]` is the cleanest factoring. Don't duplicate the migration logic.
- Hook/observer firing must happen at the same sites as `setImmediate[T]` — after the value is written.
- Defer queue: SetByID's deferred-path closure captures `v` (the `any` value). The flush invokes `SetByID` again (or the immediate helper) — same effect.
- DO NOT import third-party deps.
- The unsafe.Pointer arithmetic must be commented with the rule-6 conversion pattern (single expression).

C reference (filesystem paths — cite, do not paraphrase):

- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` — search `ecs_get_id` / `ecs_set_id` for the non-generic C analogs.
- `/work/agents/claude/projects/SanderMertens/flecs/src/entity.c` — function bodies for `ecs_get_id` / `ecs_set_id` (informational; the Go port already mirrors the algorithm in `Get[T]` / `Set[T]`).

## Constraints

- @world.go — owner of `*World`; new methods live here or in `id_ops.go` / new `value_ops.go`. Mirror existing method placement and panic conventions.
- @id_ops.go — existing `*World` ID-keyed operations (`AddID`, `RemoveID`, `HasID`). New `GetByID` / `SetByID` follow the same pattern; appending here is a valid alternative to a new file.
- @meta.go — Phase 9.1 introspection (`ComponentInfo`, `EntityComponents`). `GetByID` consumes the same `*TypeInfo` lookup these expose. Reuse the lookup path.
- @id.go — `ID` type and `MakePair` builder. Pair IDs must work as inputs to GetByID/SetByID; route through the same TypeInfo lookup.
- @defer.go — defer-queue plumbing. SetByID must enqueue when `w.IsDeferred()` and apply on flush, matching `Set[T]`'s deferred path.
- @hooks.go — OnAdd / OnSet hook dispatch. SetByID must fire these at the same sites as `setImmediate[T]`.
- @observer.go — observer dispatch. SetByID must fire observers identically to `setImmediate[T]`.
- @internal/component/typeinfo.go — `TypeInfo` carries `Type reflect.Type` and `Size`. Source of truth for the type-mismatch check and tag-component (`Size == 0`) detection.
- @internal/component/registry.go — registry lookup by ID. GetByID/SetByID both go through this to find the `*TypeInfo`; an unregistered ID is the fast-fail / panic gate.
- @internal/storage/table/table.go — column-slot pointer arithmetic. `reflect.NewAt(info.Type, ptr).Elem().Interface()` reads from this; the byte-copy write targets it. Preserve the rule-6 unsafe.Pointer comment pattern already used in `setImmediate[T]`.
- @CLAUDE.md — repo conventions: no third-party deps, godoc on all exported symbols, `unsafe` usage commented, coverage and lint gates.
- @VISION.md — Go port stays faithful to the C semantics of `ecs_get_id` / `ecs_set_id` while presenting idiomatic Go ergonomics (interface boxing, panic on type mismatch). This phase is the bridge between the typed generic API and the dynamic reflect-based path Phase 9.2 needs.

Not a bug — feature work. No `--label bug`. `snichols/queued` applied.
