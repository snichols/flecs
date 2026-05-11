## Goal

Add a public introspection API to flecs that exposes registered components and alive entities as inspectable data. This is purely additive — no existing APIs change — and is the prerequisite for serialization (Phase 9.2), debugging tools, save/load, and REST APIs (Phase 9.3).

Master HEAD `a8ae532`, v0.2.0 just tagged. The ECS is feature-complete for game/simulation workloads but lacks an introspection surface. Today `internal/component.Registry` holds TypeInfo (Size, Align, Name, Type, Component ID) but is unexported, and `internal/storage/entityindex.Index.Each` can iterate alive entities but is unexported. Users can't enumerate registered components or alive entities from outside the package.

After this lands:

```go
// Enumerate registered components.
for _, id := range w.Components() {
    if info, ok := w.ComponentInfo(id); ok {
        fmt.Printf(\"%s (id=%d, size=%d)\n\", info.Name, info.ID, info.Size)
    }
}

// Iterate alive entities.
w.EachEntity(func(e flecs.ID) bool {
    if name, ok := w.GetName(e); ok {
        fmt.Println(name)
    }
    return true // continue
})

// Inspect an entity's archetype (component IDs only).
for _, id := range w.EntityComponents(e) {
    if info, ok := w.ComponentInfo(id); ok {
        fmt.Println(\" -\", info.Name)
    }
}
```

### Deliverables

**1. New exported type `ComponentInfo`** in `meta.go` (new file) at the root flecs package:

```go
type ComponentInfo struct {
    ID    ID
    Name  string       // reflect.Type.String() of the registered Go type, e.g. \"pkg.Position\"
    Size  uintptr
    Align uintptr
    Type  reflect.Type // the registered Go type
}
```

Each field documented; `Type` may be nil for tag/pair components that have no registered Go type (e.g., a raw entity-ID used as a tag, or an auto-registered pair). Document.

**2. New `*World` methods** (all in `meta.go`):

- `func (w *World) Components() []ID` — returns a slice of all currently-registered component IDs (those that have a `*TypeInfo` in the registry). Includes built-in components (`Name`). Excludes the built-in tag entities (ChildOf, IsA, phases) since those are NOT components. Order is registration order (matches internal `Registry.Each` insertion order). Returns a fresh slice copy each call; the underlying registry is not exposed.
- `func (w *World) ComponentInfo(id ID) (ComponentInfo, bool)` — returns metadata for component id. Returns `(ComponentInfo{}, false)` if id is not registered as a component. Pair-id support: if id is a pair (e.g., `(R, T)`), look up its per-pair TypeInfo and return its metadata. If neither, return false.
- `func (w *World) EntityComponents(e ID) []ID` — returns the sorted-ascending component IDs in `e`'s current archetype signature. Returns nil for dead entities or entities in the empty archetype. Includes pair IDs. Returns a fresh slice copy (do NOT expose the table's underlying signature slice).
- `func (w *World) EachEntity(fn func(e ID) bool)` — iterate all alive entities. `fn` returns false to stop iteration early. Iteration order is allocation-order (dense-set order from the entity index). Iteration does NOT include the invalid sentinel (ID 0). Document: \"Behavior is undefined if fn calls Set/Add/Remove/Delete during iteration; wrap in Defer for safe mutation.\"
- `func (w *World) AliveEntities() []ID` — convenience: collect all alive entities into a fresh slice. Allocates. For one-shot use; for hot paths, use `EachEntity`. Document.

**3. Tiny internal additions to support the public surface:**

- `internal/component.Registry` needs a public method to support `Components()`. Option A: add `(r *Registry) IDs() []ID` returning component IDs in insertion order. Option B: extend `Registry.Each` to allow extracting IDs. Implementer's call. The simplest is `IDs()` — implement it; it returns a fresh slice each call.
- `internal/storage/entityindex.Index` needs a public iteration method. The existing `Each(fn func(id flecs.ID, rec *Record))` works but exposes Record. Add a simpler `EachID(fn func(id flecs.ID) bool)` or have World wrap the existing Each by extracting just the ID and ignoring the record. **Implementer's call** — but document.

**4. Pair-ID handling in `ComponentInfo`:**

- When a user calls `ComponentInfo(MakePair(R, T))`, look up the pair-id's TypeInfo (created by `RegisterPairData[T]` if it was called). If found, return its metadata.
- For pair IDs that have no associated data (added via `AddID` only), `ComponentInfo` returns `(ComponentInfo{ID: pairID, Name: \"pair(R, T)\", Size: 0}, true)` — implementer's call on the exact `Name` format. Document.
- For raw entity IDs used as tags (no `*TypeInfo` in registry — though `EnsureID` may have auto-registered a tag info), `ComponentInfo` returns the tag info if present, else `(ComponentInfo{}, false)`.

**5. Built-in component visibility:**

- The `Name` component (registered as a built-in in `World.New`) appears in `Components()`. Document.
- Built-in TAG entities (ChildOf, IsA, PreUpdate, OnUpdate, PostUpdate, OnFixedUpdate) are NOT components — they're entities used as relationship subjects or phase markers. They should NOT appear in `Components()`.
- The `EnsureID` path (from Phase 4.1) registers tag TypeInfos for raw entity IDs used as pair components — these DO appear in `Components()` because they have TypeInfo. This is an edge case worth documenting but not changing behavior for.

**6. Tests** in `meta_test.go`:

- **`Components()` basics:** register Position, Velocity; `Components()` includes both IDs plus Name (built-in). Verify count and that built-in tag entities are excluded.
- **`Components()` order:** registration order is preserved.
- **`ComponentInfo` for registered:** returns Name=reflect.Type.String(), Size>0, Align>=1, Type=reflect.TypeFor[T](), ID matches.
- **`ComponentInfo` for unregistered:** returns `(ComponentInfo{}, false)`.
- **`ComponentInfo` for pair-with-data:** `SetPair[Edge](w, e, R, T, ...)` then `ComponentInfo(MakePair(R, T))` returns the pair's TypeInfo.
- **`ComponentInfo` for pair-as-tag:** `AddID(w, e, MakePair(R, T))` then `ComponentInfo(MakePair(R, T))` returns the auto-registered tag info.
- **`ComponentInfo` for raw tag entity:** raw entity ID added as a tag — `ComponentInfo(tagID)` returns the tag info.
- **`EntityComponents`:** entity with `[Position, Velocity, (ChildOf, parent)]` — returns those three IDs in sorted order.
- **`EntityComponents` for dead entity:** returns nil.
- **`EntityComponents` for empty-archetype entity:** returns empty slice (or nil — implementer's call; document).
- **`EachEntity` visits every alive entity:** create N entities, collect IDs via EachEntity, assert count matches `w.Count()`.
- **`EachEntity` early-exit:** fn returns false on third call; verify only 3 invocations.
- **`EachEntity` excludes built-in tag entities:** wait — built-ins ARE alive entities (ChildOf, IsA, Name, etc.) so they ARE visited. Document this. Tests should account for the built-in count baseline.
- **`AliveEntities()` size:** matches `Count()`.
- **Mutation during EachEntity is documented unsafe but not enforced:** test that wrapping in `Defer(func() { ... EachEntity ... })` is the safe pattern.
- **Coverage of all built-in components:** after `World.New()`, `Components()` includes exactly the Name component (the only built-in COMPONENT; ChildOf/IsA/phases are not components).
- **No exposure of underlying slices:** `Components()`, `EntityComponents`, `AliveEntities` each return fresh slice copies. Mutating the returned slice must not corrupt the world. Test this.
- **Existing tests stay green.**

**7. Mechanical acceptance**

- `go test ./... -race -count=2` passes.
- `go vet ./...` clean.
- `golangci-lint run` clean.
- Coverage on `flecs` >= 90% (no regression from 97.1%).
- Coverage on `internal/component` >= 95% (currently 100%; the new `IDs()` method must be covered).
- All exported symbols have godoc.

### Non-goals

- NO JSON / serialization — Phase 9.2.
- NO REST endpoints — later phase.
- NO query listing / system listing on World.
- NO entity-component VALUE reading via meta. Use `Get[T]` for values; meta exposes only metadata.
- NO mutable inspection (e.g. \"set component value via reflect\").
- NO C-flecs `ecs_id_str` (string-format pair IDs). Users compose via `ComponentInfo(rel).Name` + `ComponentInfo(tgt).Name`.
- NO change-detection bits.
- NO ChildOf-tree inspection helpers beyond what already exists (`EachChild`, `ParentOf` are pre-existing).
- NO panic on calling `EachEntity` from within `EachEntity` — undefined behavior, not enforced.

### Implementer pointers

- `Components()` returns ONLY entities that have an associated `*TypeInfo` in the registry. Built-in TAG entities (ChildOf, IsA, phases) don't have TypeInfo and so aren't included.
- The `Name` component IS a component (it has TypeInfo since it's registered via `RegisterComponent[Name]` in `World.New`). It will appear in `Components()`.
- For pair IDs: the registry's `byID` map already supports pair lookups (Phase 4.1 added this via `RegisterPairData`). Reuse the existing lookup.
- `EntityComponents` reads from `Record.Table.Type()`. Document: returned slice is a COPY, not the table's live signature.
- `EachEntity` iteration order is allocation-order (dense vec). Document that this order may change as entities are recycled.
- DO NOT expose `*Table`, `*Column`, or any internal storage types in the public API.
- DO NOT expose `*component.TypeInfo` directly — the exported `ComponentInfo` is a snapshot copy.
- DO NOT add a method to introspect query/system/observer registrations. Those are Phase 9.3+ scope.
- DO NOT import third-party deps.

## Constraints

- @world.go — root `*World` type; new methods live in `meta.go` and operate against the same receiver.
- @id.go — exported `ID` type; new API returns `[]ID`.
- @id_ops.go — pair encoding/decoding for `MakePair`/pair-id detection used by `ComponentInfo`.
- @name.go — `Name` is the one built-in component that appears in `Components()`.
- @internal/component/registry.go — add `IDs() []ID` (or equivalent) returning component IDs in insertion order; back `Components()` and `ComponentInfo`.
- @internal/component/typeinfo.go — source of Size, Align, Name, Type, ID fields copied into the exported `ComponentInfo` snapshot.
- @internal/storage/entityindex/entityindex.go — backs `EachEntity` and `AliveEntities`; either add `EachID` or wrap existing `Each` and discard the `*Record`.
- @internal/storage/table/table.go — `Record.Table.Type()` is the source for `EntityComponents`; copy the slice, do not expose the live signature.
- C reference (read, do not paraphrase): `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` (search `ecs_get_type`, `ecs_id_str`), `/work/agents/claude/projects/SanderMertens/flecs/src/type_info.c`, `/work/agents/claude/projects/SanderMertens/flecs/src/entity.c`.
