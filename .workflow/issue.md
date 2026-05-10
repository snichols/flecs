## Goal

Land the last piece of Phase 4: a built-in `Name` component plus scan-based hierarchical path lookup. After this issue, users can name entities, look them up by dotted path, and reconstruct paths back from an entity. This closes Phase 4 of the Go port of flecs.

Master HEAD is `4198394` (Phase 4.3: IsA prefab inheritance, `Owns[T]`/`OwnsID`, inheritance-aware `Get[T]`/`Has[T]`). This issue builds directly on it. The Name component is registered exactly like a user component — it lives on entities as data, no side table.

### Target API

```go
root := w.NewEntity()
w.SetName(root, ""scene"")

car := w.NewEntity()
flecs.AddID(w, car, flecs.MakePair(w.ChildOf(), root))
w.SetName(car, ""car"")

wheel := w.NewEntity()
flecs.AddID(w, wheel, flecs.MakePair(w.ChildOf(), car))
w.SetName(wheel, ""wheel"")

found, ok := w.Lookup(""scene.car.wheel"")  // (wheel, true)
found, ok  = w.LookupChild(car, ""wheel"")    // (wheel, true)
path      := w.PathOf(wheel)                 // ""scene.car.wheel""
```

Implementation is **scan-based** for v0 — no name index. `Lookup(""a.b.c"")` does an O(N_a) scan to find ""a"" among rooted entities, then O(children-of-a) for ""b"", etc. A hash-keyed name index is deferred (probably Phase 5 with observers, or Phase 6 with cached query infrastructure).

### Deliverables

1. **`type Name struct { Value string }`** in a new file `name.go`, exported in the root `flecs` package. This IS a component, registered like any user component.

2. **Built-in registration in `World.New()`** — after IsA is allocated, register the `Name` component via the same path users would (`RegisterComponent[Name](w)`, called internally). Store the resulting ID on `*World` as unexported `nameID ID`. The first user entity moves from index 3 → index 4 (Name takes index 3). Verify Phase 4.2 / 4.3 tests use `base := w.Count()` and have no hard-coded ""first user is N"". The Name entity is alive in the entity allocator AND is registered as a component in the registry (TypeInfo with `Type = reflect.TypeFor[Name]()`).

3. **`func (w *World) Name() ID`** — returns the cached `w.nameID`.

4. **Name convenience methods on `*World`:**
   - `SetName(e ID, name string)` — equivalent to `Set[Name](w, e, Name{Value: name})`. Panics if `e` is not alive (same policy as `Set[T]`). Idempotent for re-setting.
   - `GetName(e ID) (string, bool)` — reads the Name component. Returns `("""", false)` if `e` is dead, has no Name, or Name's value is the empty string (treat empty Value as ""unnamed"" — document).
   - `RemoveName(e ID) bool` — equivalent to `Remove[Name](w, e)`. Returns true if the entity had a Name, false otherwise.

5. **Path lookup methods:**
   - `LookupChild(parent ID, name string) (ID, bool)` — finds the entity with `name` that is a direct ChildOf `parent`. `parent == 0` means ""root scope"" — scan all entities with NO ChildOf relationship and find one with the given name.
   - `Lookup(path string) (ID, bool)` — splits `path` on ""."" and walks the hierarchy, calling `LookupChild` per segment.
   - Edge cases:
     - Empty path → `(0, false)`.
     - Single segment (""foo"") → `LookupChild(0, ""foo"")` semantics (root scope).
     - Trailing dot (""foo.""), leading dot ("".foo""), double dot (""foo..bar"") → `(0, false)` (malformed). Implementer's call: validate up front or propagate empty-segment failures. Either is fine.
   - Names containing ""."" are NOT supported — document. (Future: escape characters; defer.)

6. **`PathOf(e ID) string`** — reconstructs `e`'s path from the root by walking ChildOf up.
   - Dead or unnamed → `""""`.
   - Named, no ChildOf → its name.
   - Named, named ChildOf parent → `""<parent_path>.<e_name>""`.
   - Named, but a ChildOf ancestor is unnamed: stop the walk at the first unnamed ancestor; return the path constructed so far (without the unnamed ancestor or anything above it).
   - Document these semantics with examples in godoc.

7. **Implementation guidance — scan-based lookup:**
   - `LookupChild(parent, name)`:
     - `parent != 0`: use `EachChild` to iterate; for each child, call `GetName`; compare; return on match. Do NOT collect into a slice — early-exit on first match.
     - `parent == 0`: iterate ALL live entities via `w.index.Each`; filter to those with a Name component AND no ChildOf relationship (use `Owns[Name]` for cheap presence + `ParentOf` to test for no-ChildOf); compare name; return on match. This is the O(N) path.
   - `Lookup(path)`:
     - Split on ""."" (use `strings.Split`).
     - Any empty segment (leading/trailing/double dot) → `(0, false)`.
     - Walk: `current = 0`; for each segment, `current, ok = LookupChild(current, segment)`; if !ok return `(0, false)`.
     - Return `(current, true)` after the last segment.
   - `PathOf(e)`:
     - Stack: append `e`'s name; walk up via `ParentOf`; append parent's name; stop at no-parent or unnamed.
     - Join with ""."" in reverse order.
     - Implementer's call on internal allocation; natural pattern is `[]string` + `strings.Join`.

8. **Tests** in `name_test.go`:
   - Built-in Name allocation: `World.Name()` returns consistent ID; the Name entity is alive; calling `RegisterComponent[Name](w)` again returns the same ID.
   - SetName / GetName round-trip.
   - GetName on dead entity → `("""", false)`.
   - GetName on unnamed entity → `("""", false)`.
   - GetName on entity with empty-string Name → `("""", false)` (empty Value treated as unnamed).
   - RemoveName: SetName, RemoveName, GetName → `("""", false)`. RemoveName on already-unnamed entity returns false.
   - Re-SetName: SetName ""foo"" then ""bar"" → GetName returns (""bar"", true).
   - Lookup single segment, root scope.
   - Lookup nested: `root → car → wheel` chain.
   - Lookup missing: `Lookup(""nonexistent"")` and `Lookup(""root.missing"")` → `(0, false)`.
   - Lookup malformed: empty string, leading dot, trailing dot, double dot → `(0, false)`.
   - LookupChild basics.
   - LookupChild miss.
   - LookupChild with `parent=0` finds rooted entities.
   - Sibling name collision: `LookupChild` returns the first found (any deterministic ordering is fine; don't promise which one in godoc). Document: ""behavior undefined when sibling names collide.""
   - PathOf for named root → its name.
   - PathOf for nested chain → ""root.car.wheel"".
   - PathOf for unnamed entity → `""""`.
   - PathOf for entity with unnamed parent — truncation at the unnamed boundary.
   - PathOf for dead entity → `""""`.
   - Path round-trip: `Lookup(PathOf(wheel)) == (wheel, true)`.
   - Name component is a regular component: verify `Has[Name](w, e)` returns true after `SetName`.
   - Name inherited via IsA: child with `(IsA, prefab)` where prefab is named — `GetName(child)` returns the prefab's name (per Phase 4.3 inheritance-aware Get semantics). Document this; might be surprising but it's the correct flecs behavior.
   - Existing tests stay green (especially Phase 4.2 / 4.3 Count baselines).

9. **Mechanical acceptance**
   - `go test ./... -race -count=2` passes.
   - `go vet ./...` clean.
   - `golangci-lint run` clean.
   - Coverage on `flecs` ≥ 90% (no regression from 97.3%).
   - All exported symbols have godoc.

### Non-goals

- NO name index / hashmap cache. Scan-based for v0.
- NO escape characters in names. Names cannot contain ""."".
- NO name uniqueness enforcement. Two entities can share a name; LookupChild returns the first match.
- NO relative paths (""../sibling"").
- NO observers for name maintenance.
- NO bulk operations (`SetNames`, etc.).
- NO Unicode path normalization.
- NO `EcsIdentifier` flecs-style alias / path-tag mechanism (flecs has name + path + alias kinds; we only do ""name"").
- NO change to the `Name` component shape — it stays `struct { Value string }` for this phase.

### Constraints / pointers for the implementer

- Use `RegisterComponent[Name](w)` inside `World.New()` to register the built-in — Name is both an entity AND associated with a TypeInfo, exactly like a user component. The first user entity index shifts by 1.
- The Name component is exported (`flecs.Name`). Users can interact with it via the generic API (`Set[Name](w, e, Name{""foo""})` works alongside `SetName`).
- Read `src/entity_name.c` for the algorithm shape; ignore the hash-index optimization and the multiple identifier-kinds machinery.
- Use `EachChild` / `ParentOf` for tree walks; use `entityindex.Index.Each` for root scope scans.
- DO NOT modify `RegisterComponent[T]`, `Set[T]`, `Get[T]`, `Has[T]`, `Owns[T]`, `Remove[T]`, `AddID`, `RemoveID`, `HasID`, `Delete`, `ChildOf`, `IsA`. They are stable.
- DO NOT add a separate ""name table"" or side-store. The name lives on the entity as a component.
- DO NOT use observers / hooks. The Hooks fields exist but should not be invoked yet (Phase 5).
- DO NOT auto-name entities. `NewEntity()` returns an unnamed entity.
- DO NOT import any third-party deps.

### C reference (read but do not paraphrase)

- `/work/agents/claude/projects/SanderMertens/flecs/src/entity_name.c` — name + path operations.
- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` — `EcsIdentifier`, `ecs_lookup`, `ecs_lookup_path`, `ecs_get_name`, `ecs_set_name`, `ecs_get_path`.
- `/work/agents/claude/projects/SanderMertens/flecs/src/bootstrap.c` — `EcsIdentifier` built-in registration.

## Constraints

- @world.go — register `Name` here in `World.New()` after IsA; cache as unexported `nameID ID`; expose `Name() ID`; add `SetName` / `GetName` / `RemoveName` / `Lookup` / `LookupChild` / `PathOf` methods. Verify the existing entity-allocation sequence still produces consistent IDs after the shift (first user entity index moves 3 → 4).
- @childof.go — use `ChildOf()`, `EachChild`, `ParentOf` for tree walks; the root-scope branch of `LookupChild(0, name)` filters on ""no ChildOf"" via `ParentOf`.
- @isa.go — Name inheritance via IsA is a direct consequence of the Phase 4.3 inheritance-aware `Get[T]` path; test it but do not re-implement the inheritance walk here.
- @pair_internal.go — `firstPairTarget` / `eachPairTarget` are the internal helpers for walking pairs by relationship; `EachChild` and `ParentOf` already wrap these for ChildOf, so the new methods should reuse the public ChildOf API rather than calling pair internals directly.
- @id_ops.go — `Set[T]` / `Get[T]` / `Has[T]` / `Remove[T]` semantics are stable; `SetName` / `GetName` / `RemoveName` are thin wrappers over these generic ops on the `Name` type.
- @id.go — `MakePair`, `ID` mechanics; no changes here, just consume.
- @internal/storage/entityindex/entityindex.go — `Index.Each` is the iteration primitive for the `parent == 0` root scope scan in `LookupChild`.
- @internal/component/registry.go — `RegisterComponent[T]` is what builds the TypeInfo entry for Name; the registration must be idempotent so a user calling `RegisterComponent[Name](w)` later returns the cached `nameID`.
