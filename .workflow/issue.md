## Goal

Phase 7.2 of the Go port of flecs. Adds three built-in pipeline phases — **PreUpdate**, **OnUpdate**, **PostUpdate** — to the existing Phase 7.1 system/Progress machinery.

**Master HEAD `2d81326`. Phase 7.1 landed:** `NewSystem(w, q, fn)`, `(*System).Close()`, `(*World).Progress(dt)`. Progress currently wraps all systems in a single outer `w.Defer`; systems run in registration order. Built-in entities (ChildOf=1, IsA=2, Name=3) are allocated at world init.

**Scope.** Systems attach to a phase; Progress runs phases in fixed order; **each phase is wrapped in its own Defer**. The semantic change vs Phase 7.1: mutations queued during PreUpdate apply BEFORE OnUpdate runs (cross-phase visibility), while within-phase behavior is unchanged.

After this lands:

```go
w := flecs.New()
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)
moveQuery := flecs.NewCachedQuery(w, posID, velID)

// Input — runs first.
flecs.NewSystemInPhase(w, w.PreUpdate(), moveQuery, func(dt float32, it *flecs.QueryIter) {
    // Read input, update Velocity.
})

// Movement — runs second (default phase OnUpdate).
flecs.NewSystem(w, moveQuery, func(dt float32, it *flecs.QueryIter) {
    // Apply Velocity to Position.
})

// Rendering — runs last.
flecs.NewSystemInPhase(w, w.PostUpdate(), moveQuery, func(dt float32, it *flecs.QueryIter) {
    // Read Position, draw.
})

w.Progress(0.016)
// PreUpdate systems run; defer flushes
// OnUpdate systems run; defer flushes
// PostUpdate systems run; defer flushes
```

This phase implements ONLY the three built-in phases. User-defined phases, DependsOn graphs, topological sort, and multi-threaded phase dispatch are deferred to later phases.

### Deliverables

1. **Allocate three built-in phase entities at `World.New()`.**
   - After `nameID` (currently last built-in at index 3), allocate three more tag entities in order: `preUpdateID` (index 4), `onUpdateID` (index 5), `postUpdateID` (index 6). First user entity now moves to index 7.
   - Update existing tests that hard-code "first user entity at N" (Phase 4.4 left them dynamic via `base := w.Count()` — spot-check).
   - These phase entities are plain tag entities: no special components, no special bit flags. Just IDs.
   - Document the new index layout in `World.New()`'s godoc.

2. **`(*World).PreUpdate() ID`, `(*World).OnUpdate() ID`, `(*World).PostUpdate() ID`** — accessors returning the cached field.

3. **`NewSystemInPhase(w *World, phase ID, q *CachedQuery, fn func(dt float32, it *QueryIter)) *System`** — same as `NewSystem` but explicit phase.
   - Validate: `phase` is one of `w.PreUpdate()`, `w.OnUpdate()`, `w.PostUpdate()`. Panic on any other value with a clear message: `\"phase ID X is not a recognized built-in phase; valid: PreUpdate, OnUpdate, PostUpdate\"`.
   - Until user-defined phases are supported, this strict check catches user errors early.
   - Other validations (nil world/query/fn, closed query, cross-world query) match `NewSystem`.

4. **`NewSystem(w, q, fn)` defaults to OnUpdate phase.** Signature unchanged; internally sets `system.phase = w.onUpdateID`. All existing Phase 7.1 tests should continue to pass (they implicitly use OnUpdate).

5. **Modify `*System` struct:** add unexported `phase ID` field.

6. **Refactor `Progress(dt)`:**
   - Iterate phases in order: `[PreUpdate, OnUpdate, PostUpdate]`.
   - For EACH phase, wrap the per-phase dispatch in its own `w.Defer(...)` block.
   - Within the per-phase Defer block: snapshot active (non-removed) systems whose `phase` field matches the current phase; iterate; call each `fn(dt, s.query.Iter())`.
   - Between phases, the previous phase's Defer flushes deferred mutations before the next phase starts.
   - **Document the new contract:** \"Mutations queue per-phase; cross-phase visibility is guaranteed.\"

7. **Phase ordering invariant:**
   - PreUpdate ALWAYS runs first.
   - OnUpdate ALWAYS runs second.
   - PostUpdate ALWAYS runs third.
   - Within a phase, systems run in **registration order** (unchanged from Phase 7.1).
   - If two systems are registered in different phases, phase order dominates regardless of registration order.

8. **`SystemCount() int`** — unchanged. Returns total non-removed system count across all phases.

9. **Tests** in new `pipeline_test.go`:
   - **Phase accessors:** `PreUpdate`, `OnUpdate`, `PostUpdate` return consistent IDs; each is alive in `w.index`; each is distinct.
   - **Phase ordering:** systems in PreUpdate, OnUpdate, PostUpdate (in that registration order) run in [Pre, On, Post] order. Sanity.
   - **Registration order ignored across phases:** register OnUpdate system FIRST, then PreUpdate system. PreUpdate still runs first.
   - **Registration order within a phase:** two systems in PreUpdate (A then B); A runs before B in the same phase.
   - **Default phase is OnUpdate:** `NewSystem` (no phase arg) places the system in OnUpdate. Verify via order: PreUpdate-explicit + default-NewSystem + PostUpdate-explicit run in [Pre, On, Post] order.
   - **Cross-phase Defer flush (key behavioral test):** PreUpdate system queues `Set[Position]` on entity e; OnUpdate system reads Position via `Get[Position](w, e)` — expects the NEW value (set in PreUpdate, flushed before OnUpdate).
   - **Cross-phase entity creation:** PreUpdate system creates entity e with `Set[Position]`; OnUpdate system can `Has[Position](w, e) == true`. Relies on inter-phase Defer flush.
   - **Within-phase mutation NOT visible:** two PreUpdate systems A and B. A sets Position on e; B reads Position via Get — gets OLD value because A's Set is queued and flushes only at end of phase.
   - **NewSystemInPhase panics on invalid phase:** pass `w.ChildOf()` (a non-phase entity) — panic.
   - **NewSystemInPhase with explicit OnUpdate:** functionally equivalent to `NewSystem`; verify by running.
   - **Phase with no systems:** PreUpdate empty, OnUpdate has systems, PostUpdate empty — Progress runs without panic.
   - **SystemCount across phases:** register 2 in PreUpdate, 3 in OnUpdate, 1 in PostUpdate; SystemCount==6. Close one; SystemCount==5.
   - **Close-during-dispatch in cross-phase:** PreUpdate system A calls `B.Close()` where B is in OnUpdate. Since B is in a different phase and that phase's snapshot is taken later, B is excluded from this frame. Document and assert chosen semantics.
   - **Existing Phase 7.1 tests stay green:** all 16 system tests work because they used `NewSystem` (default OnUpdate) and inspected behavior at the frame boundary.
   - **Defer interaction with multiple phases:** user wraps Progress in `w.Defer(func() { w.Progress(dt) })`. Inside the outer Defer, all phase Defers nest properly (DeferDepth tracking).

10. **Mechanical acceptance**
    - `go test ./... -race -count=2` passes.
    - `go vet ./...` clean.
    - `golangci-lint run` clean.
    - Coverage on `flecs` ≥ 90% (no regression from 97.6%).
    - All exported symbols have godoc.

### Non-goals

- NO user-defined phases (only three built-ins).
- NO DependsOn relationships / topological sort.
- NO arbitrary phase ordering.
- NO multi-threaded phase dispatch.
- NO Phase introspection (e.g., \"list systems in PreUpdate\") — could be useful but defer.
- NO phase Enable/Disable.
- NO custom Progress (e.g., `RunPhase(w, phase, dt)` manual phase invocation). Phases run only through Progress.
- NO PreFrame / OnLoad / PostLoad / PreStore / OnStore / PostFrame / OnValidate — flecs has many more phases; ship only the three core ones.
- NO change to `System.Close()` semantics.
- NO change to `Query`/`CachedQuery`/`Field[T]`.
- NO removal of single-outer-Defer compat — Phase 7.1's contract for users who only use one phase is preserved (within-phase behavior unchanged from 7.1).

### Constraints / pointers for the implementer

- Read `src/addons/pipeline/pipeline.c` for the C analog. The C version uses a much more elaborate pipeline (DependsOn graph, dirty tracking, multi-threading). Port only the \"iterate three phases in fixed order with per-phase Defer flush\" core.
- Allocation order in `World.New()` is now: empty table → ChildOf → IsA → Name (component) → PreUpdate → OnUpdate → PostUpdate. Index shift: first user entity moves to 7. All built-in entities are added via the same `w.index.Alloc()` + seat-in-empty-table pattern.
- The `System.phase` field defaults to `w.onUpdateID` in `NewSystem`. Document.
- `NewSystemInPhase` should NOT auto-register the phase or do any setup beyond validation + assignment.
- Phase ordering in `Progress` is hardcoded: `[w.preUpdateID, w.onUpdateID, w.postUpdateID]`. Iterate these three; for each, snapshot+dispatch in its own Defer.
- The per-phase snapshot of active systems is the key correctness point. A system Closed in an earlier phase but still running in a later phase: depends on whether the snapshot for the later phase is taken BEFORE or AFTER the earlier phase. **Recommendation:** take each phase's snapshot at the start of that phase, INSIDE the phase's Defer block. This means a system Closed in PreUpdate is excluded from OnUpdate's snapshot. Document.
- DO NOT modify `System.Close()` — same idempotent flag-flip.
- DO NOT change the existing `NewSystem(w, q, fn)` signature.
- DO NOT introduce a phase registry / map — three hardcoded IDs are enough.
- DO NOT modify CachedQuery/Query/QueryIter/Field[T]/Defer.
- DO NOT import any third-party deps.

## Constraints

- @world.go — `World.New()` allocates built-in entities (ChildOf, IsA, Name); extend with PreUpdate/OnUpdate/PostUpdate after `nameID` using same `index.Alloc()` + empty-table seating pattern. Add `preUpdateID`, `onUpdateID`, `postUpdateID` fields and accessors.
- @system.go — Phase 7.1's `NewSystem`, `(*System).Close()`, `Progress(dt)`, `SystemCount()` live here. Add `phase ID` field on `System`; refactor `Progress` to iterate `[preUpdateID, onUpdateID, postUpdateID]` with a per-phase Defer; add `NewSystemInPhase`; default `NewSystem` to OnUpdate.
- @defer.go — `w.Defer(...)` is the per-phase flush primitive. Nesting is supported (DeferDepth); each phase's Defer block must compose correctly when the user wraps Progress in their own outer Defer.
- @cached_query.go — `CachedQuery` is what systems iterate; no API changes required.
- @query.go — `QueryIter` is what system fn receives; no API changes required.
- @id.go — `ID` is the phase entity type; phase IDs are plain tag entities, same allocation/lifetime contract.
- C reference (cite, do not @-reference): `/work/agents/claude/projects/SanderMertens/flecs/src/addons/pipeline/pipeline.c`, `/work/agents/claude/projects/SanderMertens/flecs/src/addons/pipeline/pipeline.h`, `/work/agents/claude/projects/SanderMertens/flecs/src/bootstrap.c` (search `EcsPreUpdate`/`EcsOnUpdate`/`EcsPostUpdate`), `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` (search `EcsPipeline`/`EcsPhase`).
