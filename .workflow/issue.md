## Goal

Add frame timing state and a fourth built-in pipeline phase, `OnFixedUpdate`, that dispatches via the classic accumulator pattern at a world-level fixed step. Variable-rate phases (Pre/On/PostUpdate) continue to see the real frame `dt`; the fixed phase always sees the constant step. The world also accumulates `Time()` and `FrameCount()` from each `Progress(dt)` call.

Target API after this lands:

```go
w := flecs.New()
w.SetFixedTimestep(1.0 / 60.0)  // 60Hz physics

physicsQuery := flecs.NewCachedQuery(w, posID, velID)
flecs.NewSystemInPhase(w, w.OnFixedUpdate(), physicsQuery, func(dt float32, it *flecs.QueryIter) {
    // dt always == FixedTimestep, regardless of frame rate.
    for it.Next() {
        positions := flecs.Field[Position](it, posID)
        velocities := flecs.Field[Velocity](it, velID)
        for i := range positions {
            positions[i].X += velocities[i].X * dt
            positions[i].Y += velocities[i].Y * dt
        }
    }
})

w.Progress(0.020)
// PreUpdate(dt=0.020); accumulator 0 + 0.020 = 0.020 >= 1/60 → OnFixedUpdate(dt=1/60); acc ≈ 0.0033;
// OnUpdate(dt=0.020); PostUpdate(dt=0.020).

w.Progress(0.020)
// acc 0.0033 + 0.020 = 0.0233 >= 1/60 → OnFixedUpdate(dt=1/60); acc ≈ 0.0066.

fmt.Println(w.Time())       // ≈0.040 (total simulated time)
fmt.Println(w.FrameCount()) // 2
```

Standard accumulator pattern: `accumulator += dt; while accumulator >= step { run fixed; accumulator -= step }`.

### Deliverables

1. **New built-in phase entity.** Allocate `onFixedUpdateID` at index 7 in `World.New()`, after `postUpdateID`. First user entity moves to 8. Update the godoc in `New()` listing the index layout. Update existing tests that bake-in `Count()` baseline (the dynamic-baseline pattern used by Phase 4.4 / 7.2 already covers this — verify it still works).

2. **Phase accessor:** `func (w *World) OnFixedUpdate() ID`.

3. **`NewSystemInPhase` validation extended:** allows `OnFixedUpdate` in addition to PreUpdate/OnUpdate/PostUpdate. Panic message lists all 4 valid phases.

4. **Time-tracking fields on `*World`:**
   - `time float32` — total accumulated simulation time.
   - `frameCount uint64` — count of Progress calls.
   - `fixedTimestep float32` — fixed step size; 0 means disabled.
   - `fixedAccumulator float32` — internal accumulator.

5. **Public methods on `*World`:**
   - `Time() float32`
   - `FrameCount() uint64`
   - `SetFixedTimestep(step float32)` — panics on negative; `step == 0` disables OnFixedUpdate. Document spiral-of-death risk in godoc.
   - `FixedTimestep() float32`

6. **Refactor `Progress(dt)`:**
   - Validate `dt >= 0` (panic on negative; allow 0 — document).
   - Increment `w.frameCount`; add `dt` to `w.time`.
   - Phase order: **PreUpdate → OnFixedUpdate (looped) → OnUpdate → PostUpdate**.
   - OnFixedUpdate loop: `fixedAccumulator += dt`; while `fixedTimestep > 0 && fixedAccumulator >= fixedTimestep`: run OnFixedUpdate phase with `dt = fixedTimestep`, then `fixedAccumulator -= fixedTimestep`.
   - **Each iteration of OnFixedUpdate phase is its own `w.Defer` block** so between-tick mutations flush. Do NOT collapse multiple fixed iterations into one outer Defer.
   - Pre/On/PostUpdate still receive the real frame `dt`, not the fixed step.

7. **Spiral-of-death guard:** none for v0. Document the risk in `SetFixedTimestep`'s godoc.

8. **Tests** in new `timer_test.go`:
   - Time accumulates across multiple Progress calls.
   - FrameCount increments (0 → 5 after 5 Progress calls).
   - SetFixedTimestep stores and round-trips through FixedTimestep().
   - SetFixedTimestep(0) disables: fixed system never runs even with large dt.
   - SetFixedTimestep(-1) panics.
   - OnFixedUpdate runs exactly once per accumulated step (step=0.1, Progress(0.1) → 1 run; Progress(0.05) → 0; Progress(0.05) → 1).
   - OnFixedUpdate runs multiple times in one Progress (Progress(0.3) with step=0.1 → 3 runs).
   - OnFixedUpdate dt is always step (captured dt == 1/60 every call).
   - Variable-rate systems see real dt (OnUpdate captured dt == Progress dt).
   - Phase order: PreUpdate → OnFixedUpdate → OnUpdate → PostUpdate (verify via capture sequence).
   - Each OnFixedUpdate iteration is its own Defer (mutations from tick N visible to tick N+1's reads in the same Progress).
   - Accumulator fractional carry: 60 frames @ ~50fps (Progress(0.025) ×60, step=1/60) → ≈72 fixed ticks ±1 for float drift.
   - Disabled fixed timestep: system registered to OnFixedUpdate but never runs.
   - Mid-game SetFixedTimestep change: 10 frames at 60Hz, switch to 1/30, 10 more frames — frequency changes mid-run.
   - NewSystemInPhase with OnFixedUpdate works.
   - NewSystemInPhase still rejects invalid phases (ChildOf, IsA, Name, raw entity all panic).
   - Existing Phase 7.1 / 7.2 system + pipeline tests stay green.

9. **Mechanical acceptance.**
   - `go test ./... -race -count=2` passes.
   - `go vet ./...` clean.
   - `golangci-lint run` clean.
   - Coverage on `flecs` ≥ 90% (no regression from 97.1%).
   - All exported symbols have godoc.

### Non-goals

- No per-system timer rates (rate is per-WORLD, not per-system).
- No wall-clock vs sim-time decoupling (we only track simulated time).
- No time scaling / pause / bullet-time.
- No interpolation factor accessor (`alpha = accumulator/step`).
- No multiple fixed phases (only one `OnFixedUpdate`).
- No spiral-of-death guard beyond documented risk.
- No target-FPS auto-throttle.
- No frame-skip / step-skip strategies beyond the basic accumulator.
- No DependsOn / phase ordering changes; no user-defined phases.
- No async / multi-threaded fixed loop.
- No refactor of per-field phase ID storage to a slice (Phase 7.2 reviewer note — deferred).

## Constraints

- @world.go — `World.New()` currently allocates ChildOf/IsA/Name/PreUpdate/OnUpdate/PostUpdate at indices 1–6 with first user entity at 7. Add `onFixedUpdateID` at index 7 (first user entity moves to 8). Add the four new fields (`time`, `frameCount`, `fixedTimestep`, `fixedAccumulator`) and update the index-layout godoc. Do not change the per-field style for phase IDs.
- @system.go — `Progress(dt)` currently iterates `[preUpdateID, onUpdateID, postUpdateID]` with per-phase `w.Defer`. Insert `OnFixedUpdate` between PreUpdate and OnUpdate as an accumulator loop where **each fixed iteration is its own `w.Defer`** (critical for between-tick mutation visibility). `NewSystemInPhase`'s whitelist clause must accept `onFixedUpdateID` and the panic message must list all 4 phases.
- @defer.go — `w.Defer(fn)` is the per-phase wrapper to reuse. Do not modify defer semantics.
- @cached_query.go — Do not modify `Query` / `CachedQuery` / `Field[T]` semantics.
- @id.go — Phase entities are plain `ID`s; no new ID semantics needed.
- Existing tests using `w.Count()` already follow a dynamic-baseline pattern (see `world_test.go:53`, `name_test.go:353`, `isa_test.go:559`, `each_test.go:174`, `childof_test.go:296`, `observer_test.go:292`/`315`) — verify the new index shift doesn't break any baked-in baseline elsewhere.
- C reference (read, don't paraphrase): `/work/agents/claude/projects/SanderMertens/flecs/src/addons/timer.c` (timer / rate-filter), `/work/agents/claude/projects/SanderMertens/flecs/src/addons/pipeline/pipeline.c` (search `fixed_delta_time`), `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` (search `ecs_set_target_fps`, `EcsTickSource`). The Go port only takes the world-level fixed accumulator — not per-system tick sources or rate filters.
- No third-party deps.
