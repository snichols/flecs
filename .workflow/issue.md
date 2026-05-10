## Goal

Implement Phase 4.2 of the Go port of flecs: introduce a built-in `ChildOf` relationship pair with hierarchical cascade-on-Delete semantics. After this lands, deleting a parent entity recursively deletes all entities related to it via `(ChildOf, parent)` pairs, providing the foundation for scene-graphs, UI trees, scope-owned entities, and any hierarchical structure.

Phase 4.1 already shipped first-class pair IDs (`MakePair`, `AddID`, `HasID`, `SetPair[T]`, `GetPair[T]`) and a component reverse index that handles pair IDs uniformly. This phase layers a single hardcoded relationship (`ChildOf`) and the cascade-delete algorithm on top of that machinery.

Sample usage after this lands:

```go
parent := w.NewEntity()
child := w.NewEntity()
flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))

w.Delete(parent)             // cascade
flecs.IsAlive(w, child)      // false
```

### Deliverables

1. **Built-in `ChildOf` entity allocated at `World.New()`**
   - In `world.go` `New()`, after the empty table is created, allocate exactly one entity via the existing entity-index flow and store it on `*World` as an unexported `childOfID` field.
   - ChildOf is allocated BEFORE any user `NewEntity` calls. Document the invariant: the first user entity allocated has index 2 (1 is ChildOf, 0 is the invalid sentinel).
   - The ChildOf entity is a plain tag entity with no components or special bits. Its only specialness is being the canonical relationship ID for parent links.
   - Do NOT pre-register ChildOf as a component via `RegisterComponent`. It is just an entity.

2. **`func (w *World) ChildOf() ID`**
   - Returns the stored `childOfID` field. No allocation. Godoc.

3. **Cascade-on-Delete in `World.Delete(e ID) bool`**
   - Refactor: factor out `func (w *World) deleteOne(e ID) bool` containing the existing migration-out + free logic. `Delete` becomes a thin orchestrator: collect descendants, then `deleteOne` them in post-order.
   - Collect phase (iterative DFS):
     ```
     stack := []ID{e}
     toDelete := []ID{}
     seen := map[ID]struct{}{}
     for len(stack) > 0:
       node := pop(stack)
       if _, ok := seen[node]; ok: continue
       seen[node] = struct{}{}
       toDelete = append(toDelete, node)
       pairID := MakePair(w.childOfID, node)
       for _, t := range w.compIndex.TablesFor(pairID):
         entities := append([]ID(nil), t.Entities()...)  // SNAPSHOT
         for _, child := range entities:
           if w.index.IsAlive(child) {
             stack = append(stack, child)
           }
     ```
   - Delete phase (post-order, deepest first):
     ```
     for i := len(toDelete) - 1; i >= 0; i--:
       w.deleteOne(toDelete[i])
     ```
   - Critical invariants:
     - **Snapshot table entities before recursing.** `deleteOne` mutates the live slice via `RemoveSwap`; iterating the live view would skip entries.
     - **Cycle detection via `seen` map.** A user can add `(ChildOf, e)` on `e` itself; without a `seen` guard the collect loop infinite-loops. Add to `seen` BEFORE pushing to the stack.
     - **Dead-input behavior is preserved.** If `e` is not alive, return `false` immediately with no cascade — identical to Phase 1.5 semantics.
     - **Non-parent behavior is preserved.** If `e` is alive but has no `(ChildOf, e)` children, behavior matches Phase 1.5 exactly.
   - Document the post-order guarantee: children are deleted before their parents, leaves before any internal node. This matters for future observer hooks (Phase 5) and is the natural correctness order.

4. **`func (w *World) EachChild(parent ID, fn func(child ID) bool)`**
   - Iterates direct children of `parent` via `compIndex.EachTableFor(MakePair(w.childOfID, parent), ...)` (allocation-free), then over each table's entities.
   - `fn` returns `false` to stop early.
   - Godoc: behavior is undefined if `fn` calls `Delete` / `AddID` / `RemoveID` / `Set` during iteration (it can mutate the table being iterated).

5. **`func (w *World) ParentOf(e ID) (ID, bool)`**
   - Returns the first parent found for `e` — the second component of any pair `(ChildOf, *)` in `e`'s archetype signature.
   - Returns `(0, false)` if `e` is not alive or has no ChildOf relationship.
   - If `e` has multiple ChildOf parents (allowed but unusual), returns the first one in signature order. Document this.

6. **Tests** (in `childof_test.go` or extending `pair_test.go`)
   - `World.ChildOf()` returns the same ID across multiple calls.
   - `World.ChildOf()` is alive.
   - Two distinct `World.New()` instances have distinct ChildOf IDs.
   - `AddID(w, child, MakePair(w.ChildOf(), parent))` then `HasID(w, child, MakePair(...))` is true and `ParentOf(w, child)` returns `(parent, true)`.
   - `ParentOf` on an entity with no ChildOf returns `(0, false)`.
   - `ParentOf` on a dead entity returns `(0, false)`.
   - `EachChild`: parent with 3 children — fn called 3 times with each child id; not called on non-children.
   - `EachChild` early-exit: returning false stops iteration.
   - `EachChild` on a parent with no children: fn not called.
   - Cascade single-level: parent with 2 children; `Delete(parent)` -> both children dead, parent dead.
   - Cascade multi-level: grandparent -> parent -> child; `Delete(grandparent)` -> all three dead.
   - Cascade isolation: two parents each with one child; `Delete(parent1)` -> child1 dead, parent2 + child2 alive.
   - Cascade scrubs row data: child has Position; after `Delete(parent)` and entity index recycles the index, the new entity has no Position (confirms `deleteOne` ran a proper swap-remove).
   - Cascade post-order proxy: after `Delete(parent)`, both `IsAlive(parent)` and `IsAlive(child)` are false.
   - Wide cascade: parent with 100 children; `Delete(parent)` -> all 100 dead; `world.Count()` reflects.
   - Delete on a non-parent entity behaves like Phase 1.5 (alive -> true, dead -> false).
   - Self-cycle: `AddID(w, p, MakePair(w.ChildOf(), p))`; `Delete(p)` terminates and `p` is deleted.
   - All existing Phase 1.5 and Phase 4.1 tests stay green.

7. **Mechanical acceptance**
   - `go test ./... -race -count=2` passes.
   - `go vet ./...` clean.
   - `golangci-lint run` clean.
   - Coverage on `flecs` >= 90% (do not regress from 97.9%).
   - All exported symbols have godoc.

### Non-goals (explicitly out of scope)

- NO IsA / prefab inheritance (Phase 4.3 — different semantics).
- NO path / name lookup (Phase 4.4).
- NO configurable cleanup policies (`Remove` / `Delete` / `Throw` — flecs has these; v0 hardcodes cascade-delete on ChildOf only).
- NO preventing multiple ChildOf parents on one entity (`ParentOf` simply returns the first).
- NO wildcard query matching `(ChildOf, *)`.
- NO `Roots()` / `Descendants()` / `Ancestors()` helpers — users compose via `EachChild` + recursion.
- NO observer firing (Phase 5).
- NO cascade for pair IDs with rel != ChildOf. Only `(ChildOf, e)` triggers cascade.
- NO new package. Add to `world.go` and optionally a sibling `childof.go` in the repo root.
- NO third-party deps.

### Implementer pointers

- C reference (filesystem paths — read, do not paraphrase):
  - `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` (search `EcsChildOf`)
  - `/work/agents/claude/projects/SanderMertens/flecs/src/bootstrap.c` (built-in entity allocation at world init; around `flecs_bootstrap`)
  - `/work/agents/claude/projects/SanderMertens/flecs/src/on_delete.c` (cascade structure — ignore the multi-policy machinery and observer firing; we only do post-order recursive delete of `(ChildOf, target)` children)
- Component index API: `world.compIndex.TablesFor(pairID)` returns a snapshot safe to read; `world.compIndex.EachTableFor(pairID, fn)` is allocation-free but caller must not mutate the table during iteration.
- Table API: `table.Entities()` returns a live view — copy before iterating + deleting.
- Cycle-detection map: `map[ID]struct{}` for O(1) membership. Insert BEFORE pushing to the stack.
- Initialization order in `New()`: empty table -> allocate ChildOf via entity index -> store on world. The empty table must exist before allocation so the new entity has a home archetype.

## Constraints

- @world.go — `World.New()` (allocate `childOfID` after empty table), `World.Delete` (refactor into `deleteOne` + cascade orchestrator), add `ChildOf()`, `EachChild`, `ParentOf` methods.
- @id_ops.go — existing `AddID` / `RemoveID` / `HasID` / `MakePair` / `IsPair` / `First` / `Second` are the surface this builds on; do not modify their semantics.
- @id.go — pair ID encoding used by `MakePair(w.childOfID, target)`.
- @query.go — existing query path must remain unaffected; cascade reads tables directly via the component index, not via queries.
- @internal/storage/entityindex/entityindex.go — entity allocation + `IsAlive` used by both ChildOf allocation and cascade guards.
- @internal/storage/componentindex/componentindex.go — `TablesFor` (snapshot) and `EachTableFor` (allocation-free) are the cascade + `EachChild` entry points.
- @internal/storage/table/table.go — `Entities()` returns a live view; snapshot before mutating-while-iterating.
- Do NOT modify `RegisterComponent`, `Set[T]`, `Get[T]`, `Has[T]`, `Remove[T]`, `AddID`, `RemoveID`, `HasID`, `SetPair[T]`, `GetPair[T]`, `NewQuery`, `Field[T]`, `Each<N>` — they should continue to work unchanged.
- Do NOT pre-register ChildOf as a component; it is a plain tag entity.
- Do NOT introduce a new package or any third-party dependency.
- Preserve Phase 1.5 `Delete` semantics for entities with no ChildOf descendants (alive -> true + free; dead -> false + no-op).
