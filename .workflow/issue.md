## Goal

Implement a **deferred command queue** so that mutations issued inside `DeferBegin`/`DeferEnd` blocks (or `Defer(fn)`) are buffered and applied in order when the outermost defer scope closes. This is the foundation for safe mutation during query iteration.

Master is at `97c2cbf` (Phase 5.2 just landed: multi-subscriber observers). The repo has a full ECS — storage, queries, pairs, ChildOf cascade, IsA inheritance, name lookup, hooks, observers — and all ECS mutations are currently **synchronous**: `Set`/`Remove`/`Delete`/`AddID` immediately apply, including hook/observer dispatch.

After this lands, users can:

```go
flecs.Each2[Position, Velocity](w, func(e flecs.ID, p *Position, v *Velocity) {
    if p.X < 0 {
        // Direct call would corrupt iteration. Defer instead.
        flecs.Delete(w, e)
    }
})

// Wrap iteration in Defer:
w.Defer(func() {
    flecs.Each1[Position](w, func(e flecs.ID, p *Position) {
        if p.X < 0 {
            flecs.Delete(w, e)  // queued
        }
    })
})  // queued deletes apply here, after iteration finishes
```

Manual control:

```go
w.DeferBegin()
flecs.Set[Position](w, e1, Position{1, 2})  // queued
flecs.Delete(w, e2)                          // queued
w.DeferEnd()                                  // applies in queue order
```

Nested:

```go
w.DeferBegin()           // depth = 1
w.DeferBegin()           // depth = 2
flecs.Set[Position](w, e, p)
w.DeferEnd()             // depth = 1, NO flush yet
w.DeferEnd()             // depth = 0, queue flushes now
```

### Scope explicitly EXCLUDES

- Per-goroutine staging (flecs C has multi-stage cmd queues for threading; Phase 7 territory).
- Suspend/Resume mid-defer.
- Transactional rollback.
- Deferred `RegisterComponent[T]` / `NewEntity` (these are always synchronous).
- Deferred reads (`Get[T]`/`Has[T]`/`Owns[T]` always see CURRENT state, not post-flush).
- Re-deferring while flushing (DeferBegin during flush is allowed — produces a fresh nested scope but won't re-defer the in-progress flush; document).
- Per-operation cancellation handles.
- Bounded queue sizes (queue grows as needed).
- Panic recovery during flush.

### Deliverables

1. **New file `defer.go`** in root `flecs` package.

2. **World fields (unexported, added to `world.go`):**
   - `deferDepth int` — nesting counter. Zero means "apply immediately."
   - `deferred []func(w *World)` — queue of buffered operations. Each closure re-invokes the immediate-apply path with captured arguments.

3. **Public methods on `*World`:**
   - `func (w *World) DeferBegin()` — increment `deferDepth`. Document: "Within a deferred block, Set/Remove/Delete/AddID/RemoveID/SetPair/SetName/RemoveName operations are queued and applied on DeferEnd. Reads (Get/Has/Owns/IsAlive) see CURRENT state, not the deferred future state."
   - `func (w *World) DeferEnd()` — decrement `deferDepth`. If now zero, FLUSH the queue (replay each closure in order) and clear it. If still non-zero (nested), no flush yet. **Panics** if `deferDepth` would become negative (mismatched calls).
   - `func (w *World) Defer(fn func())` — convenience: `w.DeferBegin(); fn(); w.DeferEnd()`. Even if `fn` panics, ensure DeferEnd runs (use `defer` keyword internally). Document: "Wraps fn in DeferBegin/DeferEnd. Recommended over manual Begin/End for matched-pair guarantee."
   - `func (w *World) IsDeferred() bool` — true iff `deferDepth > 0`.

4. **Refactor mutating operations to be defer-aware:**
   For each of `Set[T]`, `AddID`, `RemoveID`, `Remove[T]`, `Delete`, `SetPair[T]`, `SetName`, `RemoveName`:
   - Split into a **public wrapper** that checks `w.IsDeferred()` and either queues a closure or calls the immediate path, AND a **private immediate function** (e.g., `setImmediate[T]`, `addIDImmediate`, etc.) that does the actual work.
   - The closure captures the necessary arguments by value (entity ID, value of type T, raw IDs, etc.). For value types T, the closure captures T by value (a copy on the heap — accept this allocation cost).
   - When flushing, each closure is invoked with `w` (the world). The closure re-enters the immediate function. Hook/observer dispatch happens during flush, in queue order.

   **Operations to NOT defer (read-only or returns-a-value):**
   - `Get[T]`, `Has[T]`, `Owns[T]`, `HasID`, `OwnsID`, `IsAlive`, `Count` — read-only.
   - `RegisterComponent[T]` — synchronous (no point deferring registration).
   - `NewEntity` — synchronous (caller needs the ID NOW).
   - `Observe[T]` / `ObserveID` / `Unsubscribe` — observer manipulation synchronous.
   - `OnAdd[T]` / `OnSet[T]` / `OnRemove[T]` — hook registration synchronous.
   - `ChildOf` / `IsA` / `Name` accessors — read-only.
   - `LookupChild` / `Lookup` / `PathOf` / `GetName` — read-only.
   - `ParentOf` / `PrefabOf` / `EachChild` / `EachPrefab` — read-only.
   - `EachTableFor` / `TablesFor` — read-only.
   - All query-related: `NewQuery`, `Query.Iter`, `QueryIter.Next/Table/Count/Entities`, `Field[T]` — read-only.
   - `Each1/2/3/4` — iteration is read-only; the user's fn may queue mutations if Deferred.

5. **Flush semantics:**
   - The flush iterates the `deferred` slice in order, calling each closure.
   - **Critical**: during flush, `deferDepth` is 0 so closures don't re-defer themselves into the same queue. Closures call the immediate paths, which fire hooks and observers.
   - If a closure panics, the flush aborts. Remaining queued operations are lost. Document. (Phase 7 may add rollback; defer for now.)
   - **Re-deferring during flush:** if a closure calls `DeferBegin` (manually or via a hook/observer), a fresh nested defer scope starts. Queue allocates fresh entries. The new scope must DeferEnd before the outer flush continues. The simplest implementation: closures see `deferDepth == 0` at entry, so they apply immediately by default; if they explicitly call DeferBegin within their body, they enter a new defer scope which they must close themselves. Document the contract precisely.

6. **Hook/Observer interaction:**
   - When a deferred Set is flushed, OnSet fires (and OnAdd if applicable). Hooks and observers fire DURING flush, in flush order.
   - If a hook/observer calls a mutating operation while flushing (deferDepth == 0 at flush time), that operation applies IMMEDIATELY. This can cause subtle bugs if the user expected those nested mutations to also be deferred. The standard mitigation is "wrap your whole event-driven flow in a Defer block."
   - Document this behavior clearly with examples.

7. **Cascade Delete + Defer:**
   - When deferred Delete is flushed, the cascade still happens (ChildOf children are deleted as part of the single Delete operation). The cascade is internal to `Delete`, so it just runs at flush time.
   - If a CHILD of the deleted parent is also separately queued for Delete, the second Delete might find the entity already dead (cascaded). The current `Delete` returns false for dead entities — that path is already exercised. No special handling needed.

8. **Tests** in `defer_test.go`:
   - **Basic queue + flush:** `DeferBegin`; `Set[Position]`; verify `Has[Position] == false` (deferred, not yet applied); `DeferEnd`; verify `Has[Position] == true`.
   - **Get during defer sees old state:** entity has Position{1,2}; `DeferBegin`; `Set[Position](w, e, Position{99,99})`; `Get[Position](w, e)` returns `(Position{1,2}, true)`; `DeferEnd`; `Get` returns `(Position{99,99}, true)`.
   - **Order preserved:** queue 3 Sets with different values for the same entity; after flush, Get returns the LAST value.
   - **Multi-operation:** queue Set + Delete on different entities; both apply on flush.
   - **Nested DeferBegin:** `Begin; Begin; Set; End; verify still deferred; End; flushes.`
   - **Convenience `Defer(fn)`:** calls fn, flushes on return. Even if fn panics, flush attempts to run (verify via captured state).
   - **Mismatched DeferEnd panics:** `DeferEnd` without prior `DeferBegin` → panic.
   - **IsDeferred reports correctly:** false before, true inside, false after.
   - **Observer fires during flush:** register observer; defer Set; flush; observer fires once after flush completes the Set.
   - **Observer queuing in observer:** an observer (registered for Set) ITSELF queues a Set; this happens at flush time when deferDepth==0; the new Set applies immediately. Document and verify.
   - **Defer-wrapped iteration:** `Defer(func() { Each1[Position] {... Delete(e) ...} })` — iteration completes without modification; deletes apply after.
   - **Cascade through defer:** `DeferBegin; Delete(parent_with_children); DeferEnd` — flush triggers the cascade; all children deleted.
   - **AddID deferred:** `DeferBegin; AddID(w, e, tagID); DeferEnd` — tag is added.
   - **RemoveID deferred:** symmetric.
   - **SetPair deferred:** `DeferBegin; SetPair[Edge](w, e, R, T, ...); DeferEnd` — pair data set.
   - **SetName deferred:** `DeferBegin; SetName(w, e, \"foo\"); DeferEnd` — name set; Lookup works after.
   - **RegisterComponent NOT deferred:** `DeferBegin; RegisterComponent[NewType](w)` returns an ID immediately, even though Set on that type would be deferred.
   - **NewEntity NOT deferred:** `DeferBegin; e := w.NewEntity()` returns a valid ID immediately; `IsAlive(e)` is true even within the defer block.
   - **Get/Has/Has2 NOT deferred:** all read APIs work inside defer.
   - **Existing tests stay green** — non-deferred behavior is unchanged for all callers.
   - **Race detection:** `go test -race` must pass; even though World is single-threaded, the queue + flush should not introduce visible data races.

9. **Mechanical acceptance**
   - `go test ./... -race -count=2` passes.
   - `go vet ./...` clean.
   - `golangci-lint run` clean.
   - Coverage on `flecs` ≥ 90% (no regression from 97.9%).
   - All exported symbols have godoc.

### Implementation pointers

- Read `src/commands.c` and `src/stage.c` for the C analog. Note: the C model uses a tagged-union command struct with a stack allocator; we use Go closures. This is simpler but allocates per command. Performance optimization (closure-pool, tagged struct) is a future concern.
- Closures capture by VALUE for entity ID and component data. For value types T, that means the closure carries a `T` field — Go handles this correctly.
- For `Set[T]`, the closure body MUST be the same code path as immediate Set (calls hooks, fires observers, etc.). That's why we extract `setImmediate[T]` — both the immediate path and the deferred-flush path call the same function.
- Refactoring is mechanical:
  ```go
  func Set[T any](w *World, e ID, v T) {
      if w.deferDepth > 0 {
          captured := v
          w.deferred = append(w.deferred, func(w *World) {
              setImmediate[T](w, e, captured)
          })
          return
      }
      setImmediate[T](w, e, v)
  }

  func setImmediate[T any](w *World, e ID, v T) {
      // existing Set body
  }
  ```
- `setImmediate[T]` is a generic function. Defining it should work fine in Go 1.21+.
- The `deferred` queue is `[]func(w *World)`. Don't index it by op-kind; just call in order.
- DO NOT skip flushing if the queue is empty — DeferEnd should always decrement.
- DO NOT introduce a per-operation "kind" enum. The closure encapsulates everything.
- DO NOT modify hook/observer dispatch.
- DO NOT change the public API surface of any existing function — only add new behavior (deferred queueing).
- DO NOT import any third-party deps.

### C reference (cite paths — read, do not paraphrase)

- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` — search `ecs_defer_begin` / `ecs_defer_end` for the public API contract.
- `/work/agents/claude/projects/SanderMertens/flecs/src/commands.c` — flush logic, command discrimination.
- `/work/agents/claude/projects/SanderMertens/flecs/src/commands.h` — `ecs_cmd_t` shape (tagged union; we use Go closures).
- `/work/agents/claude/projects/SanderMertens/flecs/src/stage.c` — `ecs_defer_*` body. Read `flecs_defer_begin` and `flecs_defer_end` specifically.

### Non-goals

- NO per-goroutine staging (Phase 7).
- NO Suspend/Resume mid-defer.
- NO transactional rollback.
- NO deferred reads.
- NO deferred component or observer registration.
- NO bounded queue size / overflow handling.
- NO cancellation handles.
- NO panic recovery during flush.

## Constraints

- @world.go — owner of `*World`; add unexported `deferDepth int` and `deferred []func(w *World)` fields here. Existing fields and public surface unchanged.
- @hooks.go — centralized `fireOnAdd/Set/Remove(info, id, e, ptr)` dispatch is invoked from the immediate paths and therefore fires during flush in queue order. Do not modify hook dispatch.
- @observer.go — `Observe[T]`/`ObserveID`/`Observe2[T]`/`Unsubscribe` stay synchronous; observers fire after hooks at the same dispatch points during flush.
- @id_ops.go — home of `Set[T]`, `AddID`, `RemoveID`, `Remove[T]`, `Delete`, `SetPair[T]`. Split each into a public defer-aware wrapper + `*Immediate` helper. Public signatures unchanged.
- @name.go — `SetName` and `RemoveName` get the same wrapper/immediate split. `Lookup`/`PathOf`/`GetName` stay read-only.
- @childof.go — cascade Delete is internal to `Delete`; when a deferred Delete is flushed, the cascade just runs. No special handling.
- @id.go — `ID` is the value carried by closures; capture-by-value is fine.
- @internal/component/registry.go — `RegisterComponent[T]` stays synchronous (returns the ID); not part of the defer path.
