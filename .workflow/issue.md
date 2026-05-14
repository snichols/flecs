## Goal

Port C flecs's `on_replace` component lifecycle hook to Go flecs as `OnReplace[T]`. The hook receives both the **previous** and **new** component value when `Set` overwrites an existing slot — enabling diff-style logic (delta detection, change-event publishing, undo stacks) that's awkward to express with `OnSet` alone.

**Framing — this kicks off the post-trait-roadmap phase (16.x).** The trait system roadmap closed with v0.54.0 (Union). The 16.x line resumes draining the broader feature-gap list at @docs/README.md, starting with the observer/hook gaps in the Phase 14.1 (line 101) and Phase 14.7 (line 152) sections.

### Naming clarification (important)

The brief described an "OnReplace observer hook." In upstream C flecs, `on_replace` is **not** an event — it has no `EcsOnReplace` constant and is not registerable via `ecs_observer_init`. It is a **per-component lifecycle hook** living in `ecs_type_hooks_t` alongside `on_add`, `on_set`, `on_remove`. This phase ports it as a per-component hook (`OnReplace[T]`), which is the natural mirror of Go flecs's existing @hooks.go pattern (`OnAdd[T]`/`OnSet[T]`/`OnRemove[T]`).

A future "EventOnReplace" observer would be a separate, opinionated extension if ever wanted — out of scope here.

## Constraints

### Upstream C reference (`/work/agents/claude/projects/SanderMertens/flecs`)

- **`include/flecs.h:1004–1008`** — the `on_replace` hook field on `ecs_type_hooks_t`. Comment defines the semantic exactly: *"Callback that is invoked with the existing and new value before the value is assigned. Invoked after on_add and before on_set. Registering an on_replace hook prevents using operations that return a mutable pointer to the component, like get_mut(), ensure(), and emplace()."* Note: no `EcsOnReplace` event constant exists anywhere in `include/`.
- **`src/component_actions.c:71–115`** — `flecs_invoke_replace_hook`: builds an `ecs_iter_t` with `field_count = 2` and `ptrs = {old_ptr, new_ptr}`. This is the canonical handler-payload shape.
- **`src/entity.c:875–878`** — fire site in `flecs_copy_id` (immediate Set into an existing slot): `if (ti->hooks.on_replace) { flecs_invoke_replace_hook(world, r->table, entity, component, dst_ptr, src_ptr, ti); }`. The hook fires **before** `flecs_type_info_copy` writes the new value, and **before** `flecs_notify_on_set`.
- **`src/entity.c:2315–2318`** — fire site in `ecs_set_id` for the slot-already-existed path (this is the analog of Go's immediate-path "table has component" branch in @value_ops.go).
- **`src/entity.c:1940–1949, 1963–2039`** — `flecs_component_has_on_replace`; asserts that block `get_mut`/`ensure`/`emplace` when a component has on_replace. We don't ship those APIs in Go flecs (no `Mutable` pointer API), so the C divergence is automatic — document it.
- **`src/commands.c:540–547, 616–621, 649–654, 1015–1018`** — four deferred-path fire sites: `flecs_defer_set` (when value already present → `EcsCmdAddModified`), `flecs_defer_cpp_set`, `flecs_defer_cpp_assign`, and the `EcsCmdEnsure` execution branch. Each calls `flecs_invoke_replace_hook` **before** copying the new value.
- **`include/flecs/addons/cpp/component.hpp:519–534`** and **`include/flecs/addons/cpp/delegate.hpp:298–305`** — the C++ binding for `on_replace`. Confirms the handler signature shape (old then new) and the rule "only one on_replace per component."
- **`test/cpp/src/main.cpp:361–367`** — upstream test cases: `on_replace_w_get_mut`, `on_replace_w_ensure`, `on_replace_w_emplace`, `on_replace_w_set`, `on_replace_w_set_existing`, `on_replace_w_assign`, `on_replace_w_assign_existing`. The "existing" variants are the ones we actually fire on; the others verify the get-mut family panics.

### Open decision points — answered from C

1. **OnSet + OnReplace interleaving.** Per `include/flecs.h:1004–1008` the order is `on_add` → `on_replace` → `on_set`. C's `flecs_copy_id` (`src/entity.c:875–888`) calls the replace hook **and then** `flecs_notify_on_set`. So **OnSet still fires on the replace**. Mirror this in Go: on the overwrite path, fire OnReplace, then write the value, then fire OnSet. State this in the doc — it differs from a naive "OnReplace replaces OnSet" intuition.
2. **Buffer lifetime.** In C, both pointers are slot pointers (or stack pointers for the deferred path) that are valid only inside the handler. Handler must copy if it needs to retain. Document this contract in Go too. The typed `OnReplace[T]` will dereference into `T` (value copy) before user code sees it, which sidesteps the issue for typed callers; the untyped `OnReplaceID` keeps the C contract.
3. **Pair-form OnReplace.** C fires on `ecs_set_id(e, pair, &v)` overwrites via the same path (no special pair branch in `src/entity.c:875` — it's keyed by `id`). Mirror: OnReplace fires on `SetPair[T]` and `SetPairByID` overwrites of an existing pair slot.

### Vision principles (@VISION.md)

VISION.md is currently a stub (no principles or non-goals filled in). No vision-derived constraints apply.

### Go-side state

- @hooks.go — `OnAdd[T]`/`OnSet[T]`/`OnRemove[T]` registration and the `fireOnAdd`/`fireOnSet`/`fireOnRemove` dispatchers. **OnReplace registration belongs here.** Note the existing two-stage pattern: hook fires first via `info.Hooks.OnReplace`, then `dispatchObservers(id, EventOn?, e, ptr)` — but OnReplace has no observer event in C, so the dispatch step is **omitted** for OnReplace (skip the `dispatchObservers` call entirely).
- @internal/component/typeinfo.go (lines 18–38) — the `Hooks` struct. Add a new field. The existing `EntityCallback` signature `func(world any, entity ids.ID, ptr unsafe.Pointer)` does **not** carry two pointers, so a sibling type is needed:
  ```go
  type ReplaceCallback func(world any, entity ids.ID, oldPtr, newPtr unsafe.Pointer)
  ```
  This is the minimal-blast-radius shape and parallels C's two-pointer iter layout.
- @value_ops.go:276–379 — `setImmediateByPtr`, the immediate-path migration+write core. Three fire-site branches need OnReplace insertion:
  - **lines 288–314 (DontFragment)**: sparse-set path. Already discriminates `isExisting`. On `isExisting == true`, call OnReplace with `old = sparseSetGet` and `new = srcPtr` **before** `sparseSetInsert`. Then proceed to fire OnSet.
  - **lines 316–348 (Sparse-only)**: similar — `isExisting && t.HasComponent(id)` is the "replace" condition. Fire OnReplace before `sparseSetInsert`.
  - **lines 350–365 (archetype-stored)**: `t.HasComponent(id)` branch. Capture `oldPtr := t.Get(int(rec.Row), id)`, fire OnReplace, then call `t.Set` (which overwrites), then fire OnSet. This is the most common path.
  - Note line 375 (`w.migrate(...)`) is the first-add path; OnReplace does **not** fire here.
- @id_ops.go:468–488 — `setPairImmediate[T]`. Same shape as the archetype-stored branch in `setImmediateByPtr`: the `t.HasComponent(pairID)` branch (lines 478–483) is where OnReplace fires for pair overwrites. The first-set branch (lines 484–487) does not fire OnReplace.
- @cmd_queue.go:445–505 — deferred-path dispatch. Two sub-paths:
  - **`cmdSetByID`/`cmdSetPair`** (line 445, for sparse/DontFragment Set cmds that bypass cmdModified rewrite): these route to `setImmediateByPtr`, so OnReplace is picked up automatically from the immediate-path wiring above.
  - **`cmdModified`** (line 461, archetype-stored Set after Pass-1 coalescing): the value has not yet been written. Lines 487–497 do `rec.Table.Set(int(rec.Row), c.id, ptr)`. **Insert OnReplace just before that `Set`**, capturing the column pointer first: `oldPtr := rec.Table.Get(int(rec.Row), c.id); w.fireOnReplace(info, c.id, c.entity, oldPtr, ptr)`. Note: the coalescer rewrites multi-Set sequences into a single `cmdModified` per submission position (see lines 366–387), so OnReplace fires **once per surviving submission** of an existing slot — matching `OnSet`'s coalesced firing semantics.
- @cmd_queue.go:103–388 — `batchForEntity`. Critical detail: Pass 1 computes the net signature into `scratch1` and detects via `oldSig := rec.Table.Type()` whether each component was present **at batch start**. Pass 2 rewrites Sets to `cmdModified`. The "was the component present at batch start" answer is exactly the OnReplace discrimination. For the coalesced case (multiple `SetID(e, c, ...)` in one Write block on a component that was already on the entity), OnReplace fires on the **first surviving cmdModified**, with `old = pre-batch column value` and `new = that submission's value`. Subsequent `cmdModified` for the same component still fire OnSet at their own positions and may or may not fire OnReplace — **propose: fire OnReplace on every surviving cmdModified for an existing component**, matching the natural per-call semantic. Tests must lock this in.
- @observer.go — irrelevant to OnReplace (no observer event), but referenced for the parallel split: `Observe[T]`/`ObserveID`/`Observe2[T]` use `EventKind` constants. Do **not** add `EventOnReplace` to `EventKind` — keep OnReplace strictly in the hooks layer.

### Docs gap-list state (@docs/README.md)

- **Line 101** (Phase 14.1 EntitiesComponents): *"on_replace hook — receives both the previous and new component value when a component is overwritten via Set. not yet ported in Go flecs."* → flip to ✅ shipped in v0.55.0 with anchor link to ObserversManual.md.
- **Line 152** (Phase 14.7 ObserversManual): *"OnReplace hook — fires when Set overwrites an existing component value; receives both the old and new value. not yet ported in Go flecs."* → flip to ✅ shipped in v0.55.0.
- **In-passing fix at line 171** (Phase 14.8 ComponentTraits): currently reads *"`Relationship` / `Target` / `Trait` enforcement traits — restrict how an entity may be used in pairs … not yet ported."* This is **wrong** — Phase 15.15 / v0.47.0 shipped exactly this (see line 123 of the same file, and CHANGELOG.md line 210). Fix to "✅ shipped in v0.47.0" with anchor link to ComponentTraits.md § Relationship / Target / Trait. Note: the brief said "line ~167" but the actual line is 171.

### Documentation policy (operator directive)

Per @ROADMAP.md line 82, every phase from 14.0 onward must include an explicit "update docs accordingly" deliverable.

### Contributing process (@CONTRIBUTING.md)

Standard phase-completion checklist applies: `go vet`, `golangci-lint`, `go test ./... -race -count=3`, coverage ≥ 95.0% on modified files, CHANGELOG and ROADMAP updates, doc updates for any user-visible API surface.

## Deliverables

### 1. Hooks-layer API (in @hooks.go)

```go
// OnReplace registers fn as the OnReplace hook for component T in w.
// fn is called when a Set call overwrites an existing component value;
// it does NOT fire on the first Set (which calls OnAdd then OnSet).
// fn receives both the previous and the incoming value, by value, before
// the slot is overwritten.
//
// Mirrors C flecs `ti->hooks.on_replace`. Dispatch order on overwrite:
// OnReplace -> column write -> OnSet. OnSet still fires after OnReplace.
//
// Calling OnReplace[T] twice replaces the prior hook. Passing fn=nil clears.
func OnReplace[T any](w *World, fn func(fw *Writer, e ID, old, new T))

// OnReplaceID is the untyped variant for dynamic/unboxed cases. The handler
// receives raw pointers; both are valid only for the duration of the call.
// Mirrors the shape of ObserveID's untyped-pointer payload.
func OnReplaceID(w *World, componentID ID, fn func(fw *Writer, e ID, oldPtr, newPtr unsafe.Pointer))
```

### 2. TypeInfo plumbing (in @internal/component/typeinfo.go)

Add to the `Hooks` struct (after line 37):
```go
// OnReplace is called when an existing component value is overwritten.
// Receives both the prior slot pointer and the incoming value pointer,
// before the slot is written. Wired by the World in Phase 16.0.
OnReplace ReplaceCallback
```
And the new callback type:
```go
type ReplaceCallback func(world any, entity ids.ID, oldPtr, newPtr unsafe.Pointer)
```

### 3. Dispatcher (in @hooks.go)

```go
// fireOnReplace invokes the OnReplace hook (if set) for id on entity e.
// oldPtr points to the current slot value; newPtr to the incoming value.
// Both are valid only for the duration of the call. No observer dispatch:
// OnReplace has no observer event in upstream C flecs.
func (w *World) fireOnReplace(info *component.TypeInfo, id ID, e ID, oldPtr, newPtr unsafe.Pointer) {
    if info != nil && info.Hooks.OnReplace != nil {
        info.Hooks.OnReplace(&w.writeCapability, e, oldPtr, newPtr)
    }
}
```

### 4. Fire-site wiring

- @value_ops.go `setImmediateByPtr`:
  - DontFragment branch (lines 301–312): on `isExisting`, fire OnReplace with `old = sparseSetGet(w, e, id)`, `new = srcPtr`, **before** `sparseSetInsert`.
  - Sparse-only branch (lines 318–347): on the `isExisting` overwrite leg, same treatment.
  - Archetype branch (lines 355–364): on `t.HasComponent(id)`, capture `oldPtr := t.Get(int(rec.Row), id)` **into a stack-local copy** of size `info.Size` (or pass it through directly — the slot is read before `t.Set` overwrites), fire OnReplace, then `t.Set`, then OnSet.
- @id_ops.go `setPairImmediate[T]` (lines 478–483): mirror the archetype branch — capture old, fire OnReplace, `t.Set`, fire OnSet.
- @cmd_queue.go `dispatch` case `cmdModified` (lines 461–504):
  - Sparse leg (lines 471–482): on `c.valueSize > 0`, before `sparseSetInsert`, fire OnReplace if the entity was in the sparse set at batch start. **Plumb this** — the cleanest path is: in `batchForEntity` Pass 2, when rewriting `cmdSetByID/cmdSetPair` for an already-present component, stash a flag on the cmd (or rely on a runtime `sparseSet.contains(e)` check, which is O(1) anyway). Prefer the runtime check.
  - Archetype leg (lines 487–501): on `c.valueSize > 0`, capture `oldPtr := rec.Table.Get(int(rec.Row), c.id)` before `rec.Table.Set`, fire OnReplace with `old = oldPtr`, `new = ptr`. Then proceed with the existing OnSet fire.
- @cmd_queue.go `dispatch` case `cmdSetByID/cmdSetPair` (line 445): routes to `setImmediateByPtr` which is already wired in #4 above — no extra change needed.

### 5. Old-value capture mechanism

- For archetype-stored and sparse-stored components: the existing slot pointer is the "old" pointer. **No copy is required at this layer** — the column/sparse slot is still live until `t.Set`/`sparseSetInsert` overwrites it. The typed `OnReplace[T]` wrapper dereferences `*(*T)(oldPtr)` and `*(*T)(newPtr)` into `T` values before user code sees them, which is a single load and avoids escape entirely. The untyped `OnReplaceID` exposes the raw pointer with a "valid only during the call" contract — same as C, same as Go's existing untyped observer.
- **For the deferred path**: the "new" pointer points into the arena (`q.arena.bytes(c.valueOff, c.valueSize)`). The "old" pointer points into the column/sparse slot. Both remain valid through the `fireOnReplace` call; the column write happens after. Lifetime contract identical to immediate path.

### 6. Coalescing semantics (deferred path)

- Multiple `SetID(e, c, v1); SetID(e, c, v2); SetID(e, c, v3)` in one Write block, on a component **already present** on `e`:
  - **OnReplace fires three times** — once per surviving `cmdModified`, in submission order.
  - Each fire sees the column pointer **at that point in dispatch** as `old`, and the submission's payload as `new`.
  - First fire: `old = pre-batch value`, `new = v1`. Second: `old = v1`, `new = v2`. Third: `old = v2`, `new = v3`.
  - Rationale: this mirrors today's OnSet semantics (fires once per submission position, see lines 461–465 comment in cmd_queue.go). Symmetry > batch-collapsing.
- If a component was **not present at batch start** but is `SetID`'d twice in the same Write: the first Set is a "first add" (OnAdd + OnSet, no OnReplace), the second is a replace (OnReplace + OnSet). This requires Pass 2 to know per-cmd whether the slot existed at the moment of dispatch — the natural answer is the runtime `rec.Table.HasComponent(c.id)` (or sparse `contains`) check at dispatch time, which is already cheap.
- Test the above cases explicitly.

### 7. Tests (extend @observer_test.go and/or @hooks_test.go — prefer @hooks_test.go since OnReplace is a hook)

At least 10 cases:

1. **Basic**: register OnReplace for `Position`. `Set(e, Position{1, 2})` does not fire (first set). `Set(e, Position{3, 4})` fires once with `old = {1,2}, new = {3,4}`.
2. **Old-value capture**: handler captures `old.X`; asserts it matches the prior set's value across N sequential Sets.
3. **New-value visibility**: handler reads `new`; asserts it matches the current Set's value.
4. **No observer event leak**: verify that `Observe[Position](w, EventOnSet, ...)` still fires alongside, **and** that there is no `EventOnReplace` constant or observer behavior change. (Negative test: ensure `EventKind` was not extended.)
5. **OnReplace + OnSet interleaving**: register both. On first Set: OnSet fires, OnReplace does not. On second Set: OnReplace fires, then OnSet fires (in that order). Lock in the dispatch order.
6. **Coalesced deferred (existing component)**: pre-Set `Position` outside the Write. Inside one `Write`, call `SetID(e, posID, v1); SetID(e, posID, v2); SetID(e, posID, v3)`. Verify OnReplace fires 3 times in order with the chained old→new values described in deliverable 6.
7. **Coalesced deferred (first-add then replace)**: inside one `Write`, `SetID(e, posID, v1); SetID(e, posID, v2)` on a component the entity did not have. Verify: OnAdd fires once, OnSet fires twice (existing behavior), OnReplace fires **once** (on the second submission), with `old = v1, new = v2`.
8. **Cross-Write**: pre-Set in `Write1`. `Write2` calls `SetID(e, posID, v2)`. OnReplace fires once at Write2 flush with `old = pre-Write2 value, new = v2`.
9. **Sparse component**: `SetSparse(w, posID)`. Repeat case 1 — verify OnReplace fires for sparse-stored components.
10. **DontFragment component**: `SetSparse + SetDontFragment` on `posID`. Repeat case 1 — verify OnReplace fires through the DontFragment branch.
11. **Pair-form**: register OnReplace for pair data type `(Likes, Bob)`. `SetPair[Friendship](e, likes, bob, v1)`; `SetPair[Friendship](e, likes, bob, v2)`. Verify OnReplace fires on the second call.
12. **Remove + re-Set**: `Set; Remove; Set` — the second Set is a "first add" again, OnReplace does not fire.
13. **Multiple OnReplace hooks**: registering twice replaces the prior hook (single-hook-per-type semantic, matches OnSet). Verify only the most-recently-registered fires.
14. **Nil registration clears**: `OnReplace[T](w, nil)` clears the hook. Subsequent overwrites do not fire any handler.
15. **Untyped `OnReplaceID`**: register via the ID-keyed API on a runtime-registered component. Verify pointer-shape contract: handler can read both pointers; the pointers are invalid after the call returns (document, do not enforce — same as observer-untyped contract).

Coverage: ≥ 95.0% on modified files.

### 8. Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%

### 9. Doc updates

- @docs/ObserversManual.md:
  - Replace the existing "OnReplace Event" subsection under "Not Yet Ported in Go flecs" (line 411) with a new top-level **"OnReplace Hook"** section under "## Hooks" (after the OnSet subsection at line 48). Show the typed handler signature, the old/new semantics, the "fires only on overwrite, not on first set" rule, and the "OnReplace -> column write -> OnSet" interleaving.
  - Note the buffer-lifetime contract for `OnReplaceID`.
- @docs/EntitiesComponents.md: add a brief callout where Set semantics are covered, pointing readers at OnReplace for diff use cases.
- @docs/README.md: flip line 101 and line 152 to ✅ shipped v0.55.0; fix line 171 (Relationship/Target/Trait) to ✅ shipped v0.47.0 with anchor.
- @README.md: feature list bump under "Hooks" / "Observers."
- @CHANGELOG.md: add v0.55.0 entry at top. Body should explicitly state: *"First phase beyond the trait-system roadmap; resumes draining the docs/README.md feature-gap list. The Phase 14.8 ComponentTraits gap entries are now exhausted — 16.x continues with observer/hook and entity gaps."*
- @ROADMAP.md:
  - Update line 3 heading to "Shipped (through v0.55.0)".
  - Add a new "Observer-system gaps (Phase 16.x candidates)" section under "Future Work" listing the remaining observer/hook gaps from docs/README.md lines 152–161 (OnDelete events, OnTableEmpty/OnTableFill, Custom events, multi-term observers, yield-on-create, propagation, monitor observers, observer disabling, fixed-source terms).
  - Add the v0.55.0 entry to the Shipped section.
- @MIGRATING.md: no breaking changes — no entry needed unless the brief diverges.

## Non-Goals

- No `OnDelete` / `OnDeleteTarget` observer events (Phase 16.1 candidate per docs/README.md line 152).
- No `OnTableEmpty` / `OnTableFill` events (Phase 16.2 candidate).
- No custom events.
- No multi-term observers.
- No yield-on-create flag.
- No observer propagation along relationship edges.
- No `EventOnReplace` event constant — OnReplace is a hook, not an event (mirrors C).
- No `Mutable`/`get_mut`/`ensure`/`emplace` API surface — Go flecs has never exposed these, so the C-side restriction (registering on_replace disables them) doesn't apply.
