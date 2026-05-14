## Goal

Port upstream C flecs's `ecs_set_scope` / `ecs_get_scope` to Go-flecs as a `WithinScope` closure-based API (primary) plus explicit `PushScope` / `PopScope` (advanced). When a scope is active, calls to `NewEntity` (and friends) automatically receive `(ChildOf, parent)` without an explicit `AddID`.

Today building hierarchies in Go-flecs requires explicit `(ChildOf, parent)` adds on every child:

```go
parent := flecs.NewEntity(fw)
child1 := flecs.NewEntity(fw)
flecs.AddID(fw, child1, flecs.Pair(fw.World().ChildOf(), parent))
child2 := flecs.NewEntity(fw)
flecs.AddID(fw, child2, flecs.Pair(fw.World().ChildOf(), parent))
```

After this phase:

```go
parent := flecs.NewEntity(fw)
flecs.WithinScope(fw, parent, func(fw *flecs.Writer) {
    child1 := flecs.NewEntity(fw)  // auto-(ChildOf, parent)
    child2 := flecs.NewEntity(fw)  // auto-(ChildOf, parent)
})
```

Target version: **v0.74.0** (next after Phase 16.18 fixed-source query terms, shipped at commit `f90a0fa`).

Gap entry being closed: `docs/README.md` line 87 ("Entity scoping (`ecs_set_scope` / push-pop) — not ported"). Doc cross-references at `docs/HierarchiesManual.md` line 228 and 334.

## Upstream C reference

Verified against `/work/agents/claude/projects/SanderMertens/flecs`:

- `include/flecs.h:4259-4286` — `ecs_set_scope(world, scope) → previous_scope` and `ecs_get_scope(world)`. Docstring: "sets the scope of the current stage to the provided entity. As a result, new entities will be created in this scope, and lookups will be relative to the provided scope. It is considered good practice to restore the scope to the old value."
- `src/entity_name.c:785-808` — implementation. **Single-level**: `stage->scope = scope` overwrites. Returns previous value so caller can save+restore (no internal stack). **Per-stage**: `flecs_stage_from_world(&world)` — the scope lives on `ecs_stage_t`, not the world. **Recycled IDs**: scope applies to any new entity allocation; recycled IDs treated as fresh allocations from the scope's perspective (no special carry-over of previous parent).
- `src/entity.c:1227` — `ecs_entity_t scope = stage->scope;` is read in the entity-init path; line 1304-1306 shows `desc->parent` overrides scope (parent field takes precedence).
- `src/entity.c:1053-1058` and `1131-1135` — the auto-injection site: `if (new_entity && scope && !name && !name_assigned) { ecs_add_id(world, entity, ecs_pair(EcsChildOf, scope)); }`. The scope add is suppressed when a name path is being processed (the name machinery handles parent injection itself). For Go-flecs, name handling is decoupled from `NewEntity`, so the suppression rule does not apply — every `NewEntity` inside a scope gets `(ChildOf, scope)`.
- `src/bootstrap.c:961` and `1336` — upstream uses `ecs_set_scope(world, EcsFlecsCore)` during bootstrap to put built-ins under a namespace, then resets to 0. Go-flecs does not need this pattern (no namespacing scheme for built-ins).
- `src/addons/units.c:25-273` and `src/addons/module.c:42-227` — upstream usage pattern: `prev := ecs_set_scope(world, X); ... ; ecs_set_scope(world, prev);` (save+restore idiom). No internal stack, but the save+restore pattern composes into a stack of arbitrary depth at the call-site level.

**Nested scopes**: upstream does NOT have a stack — each `ecs_set_scope` replaces. Stacking happens at the caller via save+restore. Go-flecs can either mirror that (return previous from Push, caller passes it to Pop) or maintain an internal stack. **Decision**: maintain an internal stack on the Writer-side (one `[]ID`), so `WithinScope` can use a clean defer-based pop without burdening callers. Explicit `PushScope` returns previous and is intended for advanced callers who need it.

## Go-side state (verified)

- @scope.go:31 — `type Writer struct { Reader; stage *stage }`. Scope stack will live here as a new field `scopeStack []ID`. (The file is named `scope.go` but it's the Reader/Writer scope-capability file — unrelated to entity scoping. The new file will be `entity_scope.go` to avoid name collision.)
- @scope.go:355 — `func (fw *Writer) NewEntity() ID { return fw.world.newEntityInternal() }`. The Writer-side hook will live here: after `newEntityInternal`, if `fw.scopeStack` is non-empty, call `addIDImmediate` (or the deferred-coalescer path) with `MakePair(fw.world.childOfID, top)`. Routing through `AddID(fw, e, pairID)` is preferable because it already handles the immediate vs deferred fork and respects ChildOf's Exclusive / OrderedChildren / cycle-detect / etc.
- @world.go:776-789 — `newEntityInternal`. Note: world-level, no scope awareness; this is correct — scope is a Writer concern. The hook stays at the Writer.NewEntity level.
- @id_ops.go:57 — `addIDImmediate`. Already used by `AddID` for the deferDepth==0 path; will be reached transparently via the Writer.AddID call from the scope hook.
- @entity_lifecycle.go:135 — `func MakeAlive(fw *Writer, id ID) ID`. Per the Phase 16.16 precedent ("explicit ID claim bypasses range"), `MakeAlive` will NOT apply auto-scope.
- @entity_range.go:72 — `func RangeNew(fw *Writer, min, max ID) ID`. Will apply auto-scope after the ranged allocation (it's still a fresh-entity allocation, just constrained).
- @stage.go:16 — `type stage struct { ... deferDepth int }`. **Important**: scope lives on `Writer`, not `stage`. Rationale below in Open Decisions.
- @world.go:42 — `writeCapability Writer` is a single cached Writer on the World (re-used across calls to `w.Write`). This means the scope stack must be reset to empty on entry to a fresh `Write(...)` call (otherwise a previous Write's leftover stack would leak). On entry to nested Write (same goroutine, `deferDepth > 0`), the stack is preserved (mirrors the existing deferDepth semantics).

## Pattern reference

The `Reader.Read(fn)` / `Writer.Write(fn)` closure pattern is the Go-flecs idiom for scoped state. `WithinScope(fw, parent, fn)` follows the same shape and is the recommended primary API. `PushScope` / `PopScope` mirror upstream's save+restore idiom for callers who need it (e.g. crossing function boundaries where a closure would be awkward).

## Constraints

- @docs/README.md — line 87 is the gap entry to flip ("Entity scoping (`ecs_set_scope` / push-pop) — not ported" → "✅ shipped in v0.74.0"). Also line 88-91 for the surrounding context.
- @docs/HierarchiesManual.md — line 228 ("Not yet ported: Per-hierarchy name scoping…") and line 334 (full "Not yet ported" entry) must be updated; new § "Entity scoping" will be added with the `WithinScope` example.
- @scope.go — Reader/Writer machinery; the Writer struct gets a new `scopeStack []ID` field. Writer.NewEntity gets the auto-injection hook.
- @world.go — `newEntityInternal` stays scope-unaware; `w.Write` resets the Writer's scope stack on top-level entry. `childOfID` is the relationship ID for the auto-pair.
- @id_ops.go — `addIDImmediate` is the immediate-path target; reached via `AddID(fw, e, pairID)` from the hook.
- @entity_lifecycle.go — `MakeAlive` explicitly does NOT auto-apply scope (mirrors Phase 16.16 "explicit ID claim bypasses range" precedent).
- @entity_range.go — `RangeNew` DOES auto-apply scope (fresh allocation, just range-constrained).
- @stage.go — stage struct stays unchanged; scope is a Writer-level concern.
- @CONTRIBUTING.md — doc update checklist: docs/HierarchiesManual.md, docs/README.md, README.md, CHANGELOG.md (top entry), ROADMAP.md (heading bump).
- @ROADMAP.md — heading "Shipped (through v0.73.0)" → "through v0.74.0"; add Phase 16.19 entry under shipped list.
- @CHANGELOG.md — new top entry: `## v0.74.0 — <date> — Phase 16.19: Entity scoping`.

## Deliverables

1. New file `entity_scope.go`:
   - `WithinScope(fw *Writer, parent ID, fn func(fw *Writer))` — primary API. Pushes parent onto Writer's scope stack, calls fn with the same Writer, pops on return (defer-based; survives panic in fn).
   - `PushScope(fw *Writer, parent ID) ID` — pushes scope, returns previous top (zero if stack was empty). Advanced.
   - `PopScope(fw *Writer, prev ID)` — pops one frame; panics if `prev` does not match the value `PushScope` returned (mismatched push/pop).
   - `GetScope(s scope) ID` — returns current scope (top of stack) or 0 if none. Accessible from both Reader and Writer (via the `scope` interface in @scope.go:39). On Reader, returns 0 (read scopes have no entity scope semantics).

2. Storage:
   - Add `scopeStack []ID` to `*Writer` in @scope.go:31.
   - On entry to a top-level `w.Write(fn)` (i.e. `deferDepth` transitions from 0 → 1), reset `scopeStack = scopeStack[:0]`. Nested Write (deferDepth > 1) preserves the stack.
   - **Per-Writer, not per-stage**: scope is a caller-facing API tied to the Writer's lexical scope. Worker stages do not need a scope (systems do not create scope-bound hierarchies in the parallel path). If a future use case demands per-stage scope, the field can be moved to `stage` then.

3. NewEntity hook (in `Writer.NewEntity`, @scope.go:355):
   - After `fw.world.newEntityInternal()`, if `len(fw.scopeStack) > 0`, call `AddID(fw, e, MakePair(fw.world.childOfID, fw.scopeStack[len-1]))`.
   - Route through `AddID` (not direct `addIDImmediate`) so the immediate vs deferred fork is handled, plus all existing ChildOf trait checks (Exclusive, OrderedChildren, cycle detect, OnDeleteTarget cleanup wiring, etc.) run unchanged.

4. RangeNew hook (in `RangeNew`, @entity_range.go:72): same as NewEntity — apply auto-scope after the ranged allocation.

5. MakeAlive (in @entity_lifecycle.go:135): does NOT apply auto-scope. Explicit ID claim bypasses scope, mirroring the Phase 16.16 "explicit ID claim bypasses range" decision.

6. Tests in `entity_scope_test.go` (≥ 10 cases):
   1. Basic: `WithinScope(fw, parent, ...)` + NewEntity inside → child has `(ChildOf, parent)`.
   2. Nested: outer = parent1, inner = parent2; NewEntity inside inner has `(ChildOf, parent2)`; after inner returns, NewEntity has `(ChildOf, parent1)`.
   3. Pop restores: NewEntity outside any scope has no auto-ChildOf.
   4. Explicit Push/Pop: `prev := PushScope(fw, parent); ... ; PopScope(fw, prev)` — same observable behavior as `WithinScope`.
   5. Mismatched pop: `PopScope(fw, wrongValue)` panics with a clear message.
   6. Panic inside fn: scope still popped via defer (followed by a subsequent NewEntity having no auto-ChildOf).
   7. GetScope: returns current top inside, 0 outside; returns 0 on a Reader.
   8. MakeAlive ignores scope: `WithinScope(fw, parent, func(fw) { id := MakeAlive(fw, x); ... })` — id does NOT have `(ChildOf, parent)`.
   9. RangeNew respects scope: `WithinScope(fw, parent, func(fw) { id := RangeNew(fw, 1000, 2000); ... })` — id HAS `(ChildOf, parent)`.
   10. Composition with OrderedChildren: if parent has OrderedChildren, scope-created children appear in the ordered list in insertion order.
   11. Composition with deferred path: inside `w.Write(fn)` (which is already deferred), scope-created entities still get auto-ChildOf via the coalescer.
   12. Stack reset across Write boundaries: leftover scope from one `w.Write(...)` does not leak into the next; verify by directly inspecting `GetScope` at top of a second Write.
   13. Recursive WithinScope with same parent: allowed; stack grows by one, pops correctly.
   - Package coverage ≥ 95.0%.

7. Doc updates per CONTRIBUTING.md:
   - `docs/HierarchiesManual.md`:
     - Add a new section "Entity scoping" with the `WithinScope` example and the nesting story. Insert before the "Not yet ported" section (currently around line 330).
     - Line 228: rewrite the "Not yet ported" callout to link to the new "Entity scoping" section.
     - Line 334: remove the "Entity scoping" bullet from the "Not yet ported" list.
   - `docs/README.md` line 87: flip to "✅ Entity scoping — shipped in v0.74.0 via `WithinScope` / `PushScope` / `PopScope` / `GetScope`. See [docs/HierarchiesManual.md § Entity scoping](HierarchiesManual.md#entity-scoping)."
   - `README.md` feature list: add an Entity scoping bullet.
   - `CHANGELOG.md`: new top entry `## v0.74.0 — <date> — Phase 16.19: Entity scoping (push/pop)` documenting WithinScope/PushScope/PopScope/GetScope, the per-Writer stack design, RangeNew opt-in / MakeAlive opt-out, and the closed gap entries.
   - `ROADMAP.md`: heading line 3 `Shipped (through v0.73.0)` → `through v0.74.0`; add a Phase 16.19 bullet to the Shipped list.

## Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Package coverage ≥ 95.0%
- No regression on existing entity / hierarchy / OrderedChildren / RangeNew / MakeAlive tests

## Explicit non-goals

- No automatic scope-tagging via a special marker entity (upstream's bootstrap uses `EcsFlecsCore` for namespacing; Go-flecs uses the explicit parent entity ID directly).
- No per-Reader scope (Read blocks don't create entities; no scope needed).
- No scope inheritance from outer Writer to inner Writer (Go-flecs doesn't have nested Writers in practice; nested `w.Write` from the same goroutine shares the cached Writer and therefore its scope stack — which is the correct behavior).
- No scope persistence across top-level `w.Write(...)` boundaries — each top-level Write call starts with an empty scope stack.
- No name-based scope semantics (upstream's `scope` also affects name lookups; this issue only covers the auto-ChildOf-on-new behavior. Name-relative lookup can be a follow-up phase if demand arises).
- No `desc.parent` precedence rule (no equivalent of `ecs_entity_desc_t::parent` in Go-flecs since there's no descriptor; explicit `AddID(fw, e, Pair(ChildOf, x))` after `NewEntity` always wins by virtue of being a separate call — Exclusive ChildOf semantics will swap the parent).

## Open decision points (with recommendations)

1. **Primary API**: `WithinScope` closure (recommended; safer; defer-based pop survives panics) vs `PushScope`/`PopScope` (more flexible across function boundaries). **Ship both**, with `WithinScope` documented as the preferred path.
2. **GetScope on Reader**: returns 0 (recommended; Read blocks don't create entities so scope is N/A) vs panic. **Returns 0.**
3. **Auto-ChildOf timing**: inside `Writer.NewEntity` after `newEntityInternal` (recommended; atomic from caller's perspective) vs as a separate post-call. **Inside NewEntity.**
4. **Stack location**: on `*Writer` (recommended; user-facing API surface lives there; matches the cached `writeCapability` lifecycle) vs on `*stage` (mirrors C's per-stage placement). **On `*Writer`** — worker stages don't need scope; per-Writer keeps the API surface coherent. Move to stage later if a use case emerges.
5. **Recursive WithinScope with same parent**: allowed (recommended; lets nesting compose freely) vs panic. **Allowed.**
6. **Mismatched PopScope**: panic with clear message (recommended; programming error) vs silent best-effort. **Panic.**
7. **RangeNew + scope**: applies scope (recommended; fresh allocation) vs bypasses (mirrors MakeAlive). **Applies scope** — RangeNew is fresh allocation, not explicit ID claim.

## Process

- Feature, not bug.
- Verify all `@`-references and line numbers before filing — done (see verification embedded above).
- Label: `snichols/queued`.
