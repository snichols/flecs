## Goal

Add **basic systems and a frame loop** to the Go port of flecs. After this phase users can attach user-supplied callbacks to a cached query and drive them via `World.Progress(dt)` — the minimum viable runtime for game/simulation loops.

Current state on master (`bde9297`): Phases 1–6.1 complete. We have World, entities, components, archetype tables, edge cache, component index, uncached `Query` + cached `CachedQuery` with incremental table tracking, Each1/Each2/Each3/Each4 typed iteration helpers, pairs as first-class IDs, ChildOf cascade-delete, IsA prefab inheritance, Name component + path Lookup, Hooks (OnAdd/OnSet/OnRemove), Observers (multi-subscriber), and a deferred command queue. The remaining missing piece for a usable runtime is **systems**.

### Target API

```go
w := flecs.New()
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)

moveQuery := flecs.NewCachedQuery(w, posID, velID)

moveSystem := flecs.NewSystem(w, moveQuery, func(dt float32, it *flecs.QueryIter) {
    for it.Next() {
        positions := flecs.Field[Position](it, posID)
        velocities := flecs.Field[Velocity](it, velID)
        for i := range positions {
            positions[i].X += velocities[i].X * dt
            positions[i].Y += velocities[i].Y * dt
        }
    }
})
defer moveSystem.Close()

for {
    w.Progress(0.016) // 60 FPS
}
```

### Scope

This phase implements ONLY:
- Opaque `*System` handle.
- `NewSystem(w, q *CachedQuery, fn func(dt float32, it *QueryIter)) *System` constructor.
- `(*System).Close()` — deregister (deferred removal pattern).
- `(*System).IsClosed() bool` — for tests/inspection.
- `(*World).Progress(dt float32)` — invokes all registered systems in registration order, wrapped in a single outer `Defer` block so mutations during system execution are safely queued and applied at end-of-frame.
- `(*World).SystemCount() int` — count of currently-registered (non-closed) systems.

### Non-goals (explicit deferrals)

- NO pipeline phases / dependency ordering (Phase 7.2).
- NO timers, fixed-timestep, or interpolation (Phase 7.3).
- NO multi-threaded system execution.
- NO systems-as-entities (no entity IDs allocated for systems).
- NO system sorting beyond insertion order.
- NO per-system Defer (one outer Defer wraps the whole Progress).
- NO between-system flushes (Phase 7.2 may add).
- NO typed `NewSystem1[T]`/`NewSystem2[A,B]` helpers — defer until ergonomic need surfaces.
- NO Enable/Disable system without removing.
- NO system tags / pause groups.
- NO system tick budget / time budget.
- NO `Run` (single-shot system run outside of Progress).
- NO recover/handler for panics.

### Deliverables

1. **New file `system.go`** in root `flecs` package.

2. **`type System struct`** — opaque with unexported fields:
   - World back-pointer.
   - Query handle (`*CachedQuery`).
   - Callback `func(dt float32, it *QueryIter)`.
   - `removed bool` flag (deferred removal pattern matching observers / cached queries).

3. **Public methods on `*System`:**
   - `func (s *System) Close()` — flag removal; idempotent. After Close the system is skipped in subsequent `Progress()` calls. The next `NewSystem` invocation triggers world-side compaction (same pattern as observers/cached queries).
   - `func (s *System) IsClosed() bool` — for tests and inspection.

4. **Constructor `func NewSystem(w *World, q *CachedQuery, fn func(dt float32, it *QueryIter)) *System`:**
   - Validate `w != nil`, `q != nil`, `fn != nil` — panic with clear messages.
   - Validate `q.IsClosed() == false` — panic. (Wiring a system to a dead query is a bug.)
   - Validate `q`'s world matches `w` — panic on mismatch. (Implementer's call on whether to expose `(cq *CachedQuery).World()` or store the World back-pointer on the CachedQuery and check equality.)
   - Stores the handle; appends to `w.systems`; returns the handle.
   - Triggers lazy compaction of `w.systems` (drops `removed` entries) before appending.

5. **World API additions:**
   - `func (w *World) Progress(dt float32)` — iterates `w.systems` in registration order, skipping `removed`, calling `s.fn(dt, s.query.Iter())` for each. The entire `Progress` call is wrapped in `w.Defer(func() { ... })` so mutations from any system are queued and applied at end-of-frame (NOT between systems — between-system flushes are Phase 7.2's job).
   - If a system's `fn` panics, the panic propagates through Progress. The outer Defer's `DeferEnd` still runs (Go `defer` semantics in `World.Defer`), so queued operations from previous systems are flushed before the panic surfaces. Document.
   - `func (w *World) SystemCount() int` — number of currently-registered (non-closed) systems.

6. **Internal `*World.systems []*System` field:**
   - Unexported. Lazily allocated.
   - Compaction (removing closed systems) happens during `NewSystem`, NOT during `Progress` (hot path stays simple — Progress just skips `removed` entries).

7. **Per-Progress iteration mechanics:**
   - Each system gets its OWN fresh iterator (`s.query.Iter()`) — multiple systems on the same query are independent.
   - The iterator is NOT shared across systems.
   - The user's `fn` is responsible for advancing the iterator (calling `it.Next()` in a loop). Document.
   - If `fn` does NOT call Next, no entities are touched. That's the user's choice.
   - Document: "`Field[T]` etc. work normally inside `fn` against the iterator passed in."

### Tests (in `system_test.go`)

- **Basic system runs on Progress:** create a system that increments a counter; Progress; counter is 1. Progress again; counter is 2.
- **System sees matching entities:** system that sums `Position.X` across entities; create 3 entities with Position{X:1}, {X:2}, {X:3}; Progress; sum is 6.
- **`dt` is passed through correctly:** system that captures the dt value; Progress(0.016); captured == 0.016. Progress(0.033); captured == 0.033.
- **Velocity-on-Position integration:** the classic example. After N frames, Position equals starting Position + Velocity*dt*N. Verify exactly.
- **Multiple systems run in order:** systems A, B, C. Each records the order. After Progress, order recorded is [A, B, C].
- **`System.Close()` stops execution:** system records calls. Close it. Progress. No call recorded.
- **Close is idempotent.**
- **Close during dispatch:** system A calls B.Close(); on the SAME Progress, B may still run (deferred-removal pattern). Document and verify the chosen semantics. (Recommend: deferred removal — B still runs in current frame, removed for subsequent.)
- **Compaction after Close:** open 3 systems, Close 2 of them, open a new one (which triggers compaction); verify `w.SystemCount() == 2`. Use test helper if needed (e.g., `SystemSliceLen` in `export_test.go`).
- **System on empty match:** query with no matching tables. Progress invokes fn; `it.Next()` immediately returns false. fn completes without writing anything. No panic.
- **Defer behavior:** system that calls `flecs.Delete(w, someEntity)` mid-iteration. The Delete is queued (because Progress wraps in Defer). Verify that AFTER Progress returns, the entity is dead. Verify that DURING the same iteration, the entity is still alive (deferred-not-applied).
- **Mutation during iteration (without user Defer wrapping):** since Progress already wraps in Defer, the user's mutations are automatically queued. This is the safety guarantee. Test it.
- **Multiple Progress calls:** sustained over 100 frames; system runs 100 times; integration math works.
- **System with no Next call:** fn doesn't iterate; Progress completes; no entities touched.
- **System panic propagates:** fn that panics; Progress propagates the panic. Verify the outer Defer's flush still runs (queued mutations from any prior systems in the same frame are still applied).
- **Constructor panics:** `NewSystem(nil, q, fn)`, `NewSystem(w, nil, fn)`, `NewSystem(w, q, nil)`, `NewSystem(w, closedQuery, fn)`.
- **Existing tests stay green:** Phase 5 deferred command tests, Phase 6.1 cached query tests, all earlier — must not regress.

### Mechanical acceptance

- `go test ./... -race -count=2` passes.
- `go vet ./...` clean.
- `golangci-lint run` clean.
- Coverage on `flecs` >= 90% (no regression from 97.5%).
- All exported symbols have godoc.

### Implementation notes / pointers

- Read `src/addons/system/system.c` for the C analog. Ignore the multi-threaded pipeline machinery; we're only doing single-threaded sequential execution.
- The deferred-removal pattern matches Observers (`observer.go`) and CachedQueries (`cached_query.go`) exactly. Use the same pattern: a `removed bool` flag, skip-during-iter, lazy compaction at next registration.
- Progress wraps systems in Defer: `w.Defer(func() { for each system: ... })`. This uses the existing Phase 5.3 `(*World).Defer(fn func())`. If Defer panics during flush, the panic propagates through Progress — that's already the documented behavior.
- The query passed to NewSystem MUST be `*CachedQuery`, not the uncached `*Query`. Document why: systems run every frame, so the cached path's per-table tracking is essential for performance. (Could accept either via an interface in a future phase.)
- DO NOT allocate per-Progress: the `systems` slice is mutated only on Close (lazy compaction); the iter from `s.query.Iter()` is a normal QueryIter allocation (one per system per Progress).
- The `World.Defer(fn)` outer wrapping is the safety guarantee. Without it, a system mutating entities while iterating would corrupt the iterator. WITH it, mutations queue and apply at end-of-frame. Document this contract.
- DO NOT modify CachedQuery, Query, QueryIter, Field[T].
- DO NOT modify the existing public API of any other function.
- DO NOT import any third-party deps.

### Constraints

- @world.go — World struct gets a new unexported `systems []*System` field and the new `Progress` / `SystemCount` methods; lazy allocation pattern matches existing world-side slices.
- @defer.go — Progress wraps the per-system dispatch in `w.Defer(func() { ... })` using the existing Phase 5.3 deferred command queue; this is the safety contract that lets systems mutate entities mid-iteration without corrupting the iterator.
- @cached_query.go — NewSystem accepts `*CachedQuery` only (not uncached `*Query`); validate `q.IsClosed() == false` and that `q`'s world matches `w` (implementer chooses whether to add `(cq *CachedQuery).World()` accessor or store world back-pointer + equality check).
- @query.go — System callback receives `*QueryIter`; user is responsible for calling `it.Next()` in a loop; `Field[T]` works normally inside the callback against the iterator passed in.
- @observer.go — Mirror the deferred-removal pattern exactly: `removed bool` flag on the handle, `Close()` flips the flag (idempotent), `Progress` skips removed entries, lazy compaction at next `NewSystem`.
- @hooks.go — Reference for the same deferred-removal / handle-style API (`OnAdd[T]`, etc.).
- @id.go — Component IDs and pair encoding are unchanged; systems are not entities in this phase.
- C reference (cite paths, do not paraphrase): `/work/agents/claude/projects/SanderMertens/flecs/src/addons/system/system.c` (`ecs_system_init`, system data); `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` (search `ecs_system_desc_t` / `ecs_run` / `ecs_progress` for the C API contract — we mirror only `ecs_progress` and a simplified `ecs_system_init`); `/work/agents/claude/projects/SanderMertens/flecs/src/addons/pipeline/pipeline.c` (informational only — Phase 7.2 will port the pipeline structure; this issue does NOT).
