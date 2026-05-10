## Goal

Implement a multi-subscriber **Observer** system layered over the Phase 5.1 lifecycle hooks (`OnAdd[T]` / `OnSet[T]` / `OnRemove[T]`). Hooks remain the single-callback-per-(type, event) path; observers are the multi-callback path. Observers are opaque handles (not entities), keyed by `(component-id, event)`, and dispatched **after** the hook at each existing fire site.

After this lands:

```go
obs1 := flecs.Observe[Position](w, flecs.EventOnSet, func(e flecs.ID, p *Position) {
    log.Printf(\"Position changed on %d to %v\", e.Index(), *p)
})

obs2 := flecs.Observe[Position](w, flecs.EventOnSet, func(e flecs.ID, p *Position) {
    dirtyTracker.Mark(e)
})

flecs.Set[Position](w, e, Position{1, 2})
// -> OnSet hook fires (if registered)
// -> obs1 fires
// -> obs2 fires

obs1.Unsubscribe()
flecs.Set[Position](w, e, Position{3, 4})
// -> OnSet hook fires (if still registered)
// -> obs2 fires
```

### Context (what's on master)

Master HEAD `e5114c7`. Phase 5.1 just landed: lifecycle hooks (`OnAdd[T]`/`OnSet[T]`/`OnRemove[T]`) are wired and fire at the correct sites. The repo has a full ECS with hooks per type (single callback per (type, event)).

Existing fire sites (Phase 5.1):
- `Set[T]` fires OnSet (and OnAdd if newly added).
- `migrate()` fires OnAdd post-write and OnRemove pre-swap.
- `deleteOne()` fires OnRemove per-component pre-swap.
- `SetPair[T]` fires OnSet on the pair-id's TypeInfo.

Centralized dispatchers `(*World).fireOnAdd/fireOnSet/fireOnRemove(info, e, ptr)` are nil-safe and the single chokepoint for hook invocation. Observers will be wired into these same chokepoints.

### Deliverables

1. **New file `observer.go`** in root `flecs` package.

2. **Event kind enum:**
   ```go
   type EventKind int
   const (
       EventOnAdd    EventKind = 1
       EventOnSet    EventKind = 2
       EventOnRemove EventKind = 3
   )
   ```
   Document each. Add `func (e EventKind) String() string` for debugging.

3. **Observer handle:**
   - `type Observer struct { ... }` exported, unexported fields. Opaque to users.
   - `func (o *Observer) Unsubscribe()` removes this observer from the world. Idempotent (calling twice is safe). After Unsubscribe, the observer never fires again.

4. **User-facing registration:**
   - `func Observe[T any](w *World, event EventKind, fn func(e ID, v *T)) *Observer` typed observer. Auto-registers `T` (matches OnSet[T] convention). Returns an `*Observer` handle. Internal callback unsafely casts `*T` from the ptr the world passes.
   - `func ObserveID(w *World, id ID, event EventKind, fn func(e ID, ptr unsafe.Pointer)) *Observer` raw-ID observer (for tags/pairs). No auto-register. The user receives an `unsafe.Pointer` and is responsible for casting.
   - `func Observe2[T any](w *World, events []EventKind, fn func(event EventKind, e ID, v *T)) *Observer` convenience for subscribing to MULTIPLE event kinds in one call. Same `fn` is called for each event; the `event` parameter distinguishes. **OPTIONAL** the implementer may skip if it complicates the design; users can call `Observe[T]` 3 times manually instead.

5. **Internal observer store on `*World`:**
   - `observers map[observerKey][]*observerNode` where:
     ```go
     type observerKey struct {
         id    ID
         event EventKind
     }
     type observerNode struct {
         id       ID
         event    EventKind
         callback func(world any, e ID, ptr unsafe.Pointer)
         handle   *Observer
         removed  bool
     }
     ```
   - Allocated lazily on first observer registration. Document.
   - Unsubscribe sets `removed = true` (deferred removal). Compaction happens lazily (e.g., on next Observe call for that key or during a periodic pass) implementer's call. Document the choice.

6. **Wire dispatch into the fire helpers:**
   - After Phase 5.1's `fireOnAdd/Set/Remove` invokes the single hook, also invoke the observer list for `(id, event)`.
   - **Order:** hook fires first, then observers in registration order. Document.
   - **Defense / signature refactor:** when `info == nil`, observers must still be looked up by raw id (because `ObserveID` doesn't go through TypeInfo). **Pick Option A:** change the fire helper signature so it takes both `info *TypeInfo` AND `id ID` explicitly. Callers (migrate, Set, deleteOne, SetPair) already have both available. Update Phase 5.1's call sites accordingly.
   - For `SetPair[T]`: observers registered for the pair-id (`ObserveID(w, MakePair(R, T), EventOnSet, fn)`) should fire. Observers registered for T directly do NOT fire on pair events they're keyed by different IDs.

7. **Tests** in `observer_test.go`:
   - **Single observer fires:** Observe[Position] on EventOnSet; Set[Position]; callback fires once with correct entity and value.
   - **Multiple observers fire in registration order:** register A then B; both fire on Set; A's callback runs before B's.
   - **Observer on different event doesn't fire:** Observe on EventOnAdd; Set[Position] (which fires OnAdd + OnSet); the OnAdd observer fires once (only on the add path).
   - **Unsubscribe stops firing:** Observe; Unsubscribe; Set; callback NOT called.
   - **Unsubscribe is idempotent:** call Unsubscribe twice; no panic; subsequent Sets don't fire.
   - **Unsubscribe during dispatch:** observer A's callback calls `obs2.Unsubscribe()`. Contract: **deferred removal** A fires for the current event; B still fires for the current event; B does not fire on subsequent events. Document and test this exact semantics.
   - **Observer + hook coexist:** register OnSet[Position] AND Observe[Position] EventOnSet; hook fires first, then observer.
   - **Observer on raw entity (tag):** `tagID := w.NewEntity()`; `ObserveID(w, tagID, EventOnAdd, fn)`; `AddID(w, e, tagID)`; observer fires.
   - **Observer on pair-id:** `pairID := MakePair(R, T)`; `ObserveID(w, pairID, EventOnSet, fn)`; `SetPair[T](w, e, R, T, ...)`; observer fires with the data pointer.
   - **No firing for inherited components:** child with `(IsA, prefab)`, prefab has Position with observer; Get[Position] on child does NOT fire (only WRITES fire).
   - **Cascade delete fires observers:** parent + child via ChildOf, both have Position with observer; Delete(parent); observer fires twice (once per removal). Post-order: child first.
   - **Multiple events, multiple observers:** register 3 observers for 3 different event kinds on the same type; Set fires the correct subset; Remove fires the OnRemove observer only.
   - **Observe[T] auto-registers:** Observe[NewType] before any other use of NewType World's component count increments; subsequent Set[NewType] fires the observer.
   - **ObserveID does NOT auto-register:** ObserveID with a raw entity id that's not a registered component observer is registered but never fires unless `AddID` etc. is called with that exact id.
   - **Existing tests stay green:** Phase 5.1's 21 hook tests should still pass (hook dispatch order is preserved).

8. **Mechanical acceptance**
   - `go test ./... -race -count=2` passes.
   - `go vet ./...` clean.
   - `golangci-lint run` clean.
   - Coverage on `flecs` >= 90% (no regression from 97.7%).
   - All exported symbols have godoc.

### Non-goals

- NO observer entities (Observers stay opaque handles).
- NO query-based observers.
- NO change-detection (always fire on event).
- NO priority levels / ordered observation beyond registration order.
- NO async / goroutine-safe registration or dispatch.
- NO per-pair-aware typed `Observe[T](w, rel, tgt, event, fn)` use `ObserveID`.
- NO multi-component observers (multi-term).
- NO event propagation up/down hierarchies (e.g., \"observer on parent fires for child events\").
- NO panic recovery during dispatch.

### Constraints / pointers for the implementer

- Read `src/observer.c` for the C analog. The C model is much more elaborate (query-based, multi-term, with event propagation). We are NOT porting all that only the flat-dispatch, single-component path.
- The fire helpers (`fireOnAdd/fireOnSet/fireOnRemove` in `world.go` per Phase 5.1) are the single dispatch point. Extend them to invoke observers AFTER the hook. Recommended factoring: change the fire helper signature to take both `info *TypeInfo` AND `id ID` so the observer lookup happens unconditionally. The callers (in migrate/Set/deleteOne/SetPair) have both available update Phase 5.1's call sites.
- Storage: `map[observerKey][]*observerNode` is fine for v0. Two-level (`map[ID]map[EventKind][]node`) is unnecessary at this scale implementer's call.
- **Re-entrancy / Unsubscribe during dispatch:** use **deferred removal**. Set `removed = true` on the node when Unsubscribe is called; skip nodes with `removed == true` during dispatch; compact the slice lazily. This avoids the \"modify slice during iteration\" trap.
- Observer node ownership: the `*Observer` handle holds a back-pointer to its node. Unsubscribe sets `removed = true` and clears the back-pointer to allow GC.
- DO NOT modify `EntityCallback` signature.
- DO NOT modify `Hooks` struct.
- DO NOT add observer-as-entity machinery (no special entity allocation for observers).
- DO NOT import any third-party deps.

### C reference (cite, do not paraphrase)

- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` (search `ecs_observer_t` / `ecs_observer_init`)
- `/work/agents/claude/projects/SanderMertens/flecs/src/observer.c` registration/dispatch
- `/work/agents/claude/projects/SanderMertens/flecs/src/observable.c` observable target side (much more elaborate; we only port the single-component flat-dispatch path)

## Constraints

- @world.go fire helpers (`fireOnAdd/fireOnSet/fireOnRemove`) are the single dispatch chokepoint; extend them to invoke observers after the hook, and refactor the signature to take `(info, id, e, ptr)` so `ObserveID` works when `info == nil`. Update Phase 5.1 call sites.
- @hooks.go hook contract is frozen for this phase; do not modify `EntityCallback` signature or the `Hooks` struct. Observers layer on top, they do not replace hooks.
- @id_ops.go `AddID`, `SetPair`, and related write paths must funnel through the fire helpers so observers see the same events as hooks. Pair-id observers are keyed by `MakePair(R, T)`, distinct from observers on `T`.
- @id.go `ID` and `MakePair` define the observer key space. Observer storage uses `observerKey{id ID; event EventKind}`; pair observers are registered against the full pair-id.
- @internal/component/typeinfo.go `Observe[T]` auto-registers `T` via TypeInfo (matching the `OnSet[T]` convention). `ObserveID` bypasses TypeInfo entirely the raw id may have no TypeInfo and that is OK.
- @internal/component/registry.go observer registration must not perturb the component registry beyond the auto-registration that `Observe[T]` already triggers via the existing typed-registration path.
