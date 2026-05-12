# Systems

Systems are cached queries combined with a callback that runs on every `World.Progress` call. They are the primary way to express recurring game logic — physics, AI, animation, rendering — in Go flecs.

See the [Quickstart](Quickstart.md) for a hands-on introduction. For the query model that systems build on, see [Queries.md](Queries.md). For event-driven reactions, see [ObserversManual.md](ObserversManual.md).

---

## Creating a System

Register a system with `NewSystem`. It takes a world, a `*CachedQuery`, and a callback:

```go
type Position struct{ X, Y float32 }
type Velocity struct{ DX, DY float32 }

w := flecs.New()
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)

w.Write(func(fw *flecs.Writer) {
    e := fw.NewEntity()
    flecs.Set(fw, e, Position{X: 0, Y: 0})
    flecs.Set(fw, e, Velocity{DX: 10, DY: 5})
})

moveQ := flecs.NewCachedQuery(w, posID, velID)
flecs.NewSystem(w, moveQ, func(dt float32, it *flecs.QueryIter) {
    for it.Next() {
        positions := flecs.Field[Position](it, posID)
        velocities := flecs.Field[Velocity](it, velID)
        for i := range positions {
            positions[i].X += velocities[i].DX * dt
            positions[i].Y += velocities[i].DY * dt
        }
    }
})

// Advance one frame at 1/60 s
w.Progress(1.0 / 60.0)
```

The callback receives `dt` (the delta time passed to `Progress`) and a `*QueryIter`. Call `it.Next()` in a loop to advance through matched archetype tables; `Field[T]` returns a slice of component values for the current table.

`NewSystem` registers the system in the **`OnUpdate`** phase by default. Systems within a phase run in registration order.

Because systems use cached queries, iteration cost is proportional to the number of matched entities, not the total entity count. The cached path pre-filters tables at construction and tracks new archetypes automatically — see [Queries.md](Queries.md).

---

## Pipeline Phases

Go flecs has four built-in pipeline phases that run in a fixed order on every `Progress` call:

| Phase accessor | Purpose |
|---|---|
| `w.PreUpdate()` | Input, network receive |
| `w.OnFixedUpdate()` | Physics (fixed timestep, accumulator loop) |
| `w.OnUpdate()` | Game logic — **default for `NewSystem`** |
| `w.PostUpdate()` | Rendering, network send |

Use `NewSystemInPhase` to register a system in a specific phase:

```go
w := flecs.New()
posID := flecs.RegisterComponent[Position](w)

inputQ := flecs.NewCachedQuery(w, posID)
renderQ := flecs.NewCachedQuery(w, posID)

flecs.NewSystemInPhase(w, w.PreUpdate(), inputQ, func(dt float32, it *flecs.QueryIter) {
    // runs first — read input, update position
})

flecs.NewSystemInPhase(w, w.PostUpdate(), renderQ, func(dt float32, it *flecs.QueryIter) {
    // runs last — submit draw calls
})
```

### Phase execution order

```
PreUpdate → OnFixedUpdate (accumulator) → OnUpdate → PostUpdate
```

Deferred commands from each phase are flushed before the next phase starts, so a `PostUpdate` system sees every mutation queued during `OnUpdate`.

> **Not yet ported in Go flecs:** Custom pipeline phases (beyond the four built-ins), `DependsOn` ordering between arbitrary system entities, and per-system rate filters (`SetInterval` / `SetRate`) are not available.

---

## Using delta_time

The `dt` parameter is the value passed to `Progress`. Multiply velocity by `dt` for frame-rate–independent movement:

```go
flecs.NewSystem(w, moveQ, func(dt float32, it *flecs.QueryIter) {
    for it.Next() {
        positions := flecs.Field[Position](it, posID)
        velocities := flecs.Field[Velocity](it, velID)
        for i := range positions {
            positions[i].X += velocities[i].DX * dt
            positions[i].Y += velocities[i].DY * dt
        }
    }
})

w.Progress(1.0 / 60.0) // dt = ~0.01667 s
```

Passing `0` for `dt` is allowed and runs a "null frame" that still increments `w.FrameCount()`. Applications with their own game loop typically pass measured elapsed time; applications without one can pass a constant fixed step.

---

## Fixed Timestep

`OnFixedUpdate` systems run inside an accumulator loop. Call `SetFixedTimestep` to configure the step size:

```go
w := flecs.New()
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)
w.SetFixedTimestep(1.0 / 60.0) // physics at 60 Hz

physicsQ := flecs.NewCachedQuery(w, posID, velID)
flecs.NewSystemInPhase(w, w.OnFixedUpdate(), physicsQ, func(dt float32, it *flecs.QueryIter) {
    // dt is always 1/60, regardless of frame rate
    for it.Next() {
        positions := flecs.Field[Position](it, posID)
        velocities := flecs.Field[Velocity](it, velID)
        for i := range positions {
            positions[i].X += velocities[i].DX * dt
            positions[i].Y += velocities[i].DY * dt
        }
    }
})
```

Each `Progress(variableDT)` call advances the accumulator by `variableDT`. When the accumulator reaches the fixed step, `OnFixedUpdate` systems run; the step is subtracted and the loop repeats until the accumulator falls below the step. This means `OnFixedUpdate` may run zero, one, or multiple times per `Progress` call.

```go
w.SetFixedTimestep(0.1)

w.Progress(0.1)  // accumulator 0→0.1 → 1 fixed tick
w.Progress(0.05) // accumulator 0→0.05 < 0.1 → 0 ticks
w.Progress(0.05) // accumulator 0.05→0.1 → 1 fixed tick
w.Progress(0.3)  // accumulator 0→0.3 → 3 fixed ticks (0.1 each)
```

**Spiral-of-death warning:** if each fixed-step iteration takes longer than the step size, the accumulator grows without bound and `Progress` stalls. Keep `OnFixedUpdate` work cheap, or cap the number of iterations outside the library.

A fixed step of `0` (the default) disables `OnFixedUpdate` dispatch entirely.

---

## System Lifecycle

`NewSystem` and `NewSystemInPhase` return a `*System` handle. Call `Close` to deregister:

```go
sys := flecs.NewSystem(w, moveQ, func(dt float32, it *flecs.QueryIter) { /* ... */ })

// Remove the system — it will no longer run on Progress.
sys.Close()
fmt.Println(sys.IsClosed()) // true
```

`Close` is idempotent. Closed systems are compacted lazily on the next `NewSystem` or `NewSystemInPhase` call.

**Deferred-removal semantics:** `Progress` snapshots the active system set at the start of each phase's deferred scope. A system closed by an earlier phase's callback is excluded from later phases in the same frame. A system closed mid-phase may still complete the current frame (it is already in the snapshot) but will be skipped every frame thereafter.

> **Not yet ported in Go flecs:** System disabling via `ecs_enable` / `EcsDisabled` tag — a lighter-weight way to pause a system without removing and recreating it. Go flecs has no equivalent; call `sys.Close()` to remove and call `NewSystem` again to re-add.

---

## Parallel Systems

When the world has worker goroutines, consecutive systems in the same phase that are all marked `SetParallel(true)` can run concurrently — provided their write sets are pairwise disjoint.

```go
w := flecs.New()
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)
type Health struct{ HP int }
healthID := flecs.RegisterComponent[Health](w)

// Spin up 4 worker goroutines.
w.SetWorkerCount(4)

posQ := flecs.NewCachedQuery(w, posID, velID)
sysA := flecs.NewSystem(w, posQ, func(dt float32, it *flecs.QueryIter) {
    for it.Next() {
        positions := flecs.Field[Position](it, posID)
        velocities := flecs.Field[Velocity](it, velID)
        for i := range positions {
            positions[i].X += velocities[i].DX * dt
        }
    }
})
sysA.SetParallel(true) // writes Position, Velocity (derived from query terms)

hpQ := flecs.NewCachedQuery(w, healthID)
sysB := flecs.NewSystem(w, hpQ, func(dt float32, it *flecs.QueryIter) {
    for it.Next() {
        _ = flecs.Field[Health](it, healthID)
    }
})
sysB.SetParallel(true) // writes Health — disjoint from Position/Velocity
```

The scheduler collects a "parallel batch" of consecutive parallel systems whose write sets are pairwise disjoint and dispatches them as concurrent goroutine jobs. A system with an overlapping write set starts a new batch.

### Declaring a write set explicitly

By default, the write set is derived from the system's query terms (all `And`, `Or`, and `Optional` term IDs). Override with `SetWriteSet` for finer control:

```go
// Declare an empty write set — this system is read-only; never conflicts.
sysA.SetWriteSet([]flecs.ID{})

// Declare an explicit write set — only Position, even if the query has other terms.
sysA.SetWriteSet([]flecs.ID{posID})
```

Passing an empty slice marks the system as read-only and allows it to batch with any other parallel system.

### Deferred mutations in parallel systems

While `Progress` is running the world is in a deferred state. Structural mutations — `Set`, `Remove`, `Delete`, `AddID` — are enqueued as commands and applied after the phase completes. Each parallel worker writes to its own command queue, so there is no lock contention for deferred ops.

Direct in-place mutation of component data via `Field[T]` slices is safe: workers' entity rows within a batch never overlap.

> **Cross-link:** Per-stage worker command queues were introduced in Phase 12.1. See [CHANGELOG.md](../CHANGELOG.md) for the v0.16 implementation details.

---

## Multi-threaded Systems

`SetMultiThreaded(true)` enables within-system parallelism: the system's query iterator is split into N disjoint row-range slices (N = `WorkerCount`), one per worker goroutine. All workers run the same callback concurrently on different entity rows.

```go
w := flecs.New()
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)
w.SetWorkerCount(4)

heavyQ := flecs.NewCachedQuery(w, posID, velID)
sys := flecs.NewSystem(w, heavyQ, func(dt float32, it *flecs.QueryIter) {
    for it.Next() {
        positions := flecs.Field[Position](it, posID)
        velocities := flecs.Field[Velocity](it, velID)
        for i := range positions {
            positions[i].X += velocities[i].DX * dt
            positions[i].Y += velocities[i].DY * dt
        }
    }
})
sys.SetMultiThreaded(true)
```

For a table with 1 000 entities and 4 workers, worker 0 processes rows 0–249, worker 1 rows 250–499, and so on. The callback need not take any special precautions — slices never overlap.

A multi-threaded system always runs alone (it cannot batch with parallel siblings). The system completes fully before the next system in the phase begins.

**Performance note:** for in-place updates (mutating `Field[T]` slices directly), `SetMultiThreaded` scales linearly with worker count. For deferred mutations (`Set`, `Delete`, etc.), all workers share one command queue per stage; prefer in-place mutation inside multi-threaded callbacks.

> **Cross-link:** Multi-threaded dispatch was introduced in Phase 10.4. Per-worker stage queues (Phase 12.1) reduce contention for deferred mutations.

> **Not yet ported in Go flecs:** `RunWorker` / explicit thread dispatch — C provides `ecs_run_worker` for manual entity-range partitioning outside the pipeline. Not available in Go flecs.

---

## Stats and Observability

`World.Stats()` returns a `Stats` snapshot that includes per-phase wall-clock timing from the most recent `Progress` call:

```go
w.Progress(1.0 / 60.0)

w.Read(func(r *flecs.Reader) {
    s := w.Stats()
    fmt.Printf("frame %d  systems %d\n", s.FrameCount, s.SystemCount)
    for _, ph := range s.LastFramePhases {
        if ph.SystemCount > 0 {
            fmt.Printf("  %-16s systems=%d  %v\n", ph.Name, ph.SystemCount, ph.Duration)
        }
    }
})
```

`LastFramePhases` is indexed by phase order:

| Index | Phase |
|---|---|
| 0 | PreUpdate |
| 1 | OnFixedUpdate |
| 2 | OnUpdate |
| 3 | PostUpdate |

Each `PhaseStats` entry has `Name`, `SystemCount`, and `Duration`. For `OnFixedUpdate`, `Duration` is the sum across all fixed-step iterations in the frame.

`SystemCountInPhase` returns the live count for a single phase:

```go
w.Read(func(r *flecs.Reader) {
    n := w.SystemCountInPhase(w.OnUpdate())
    fmt.Println("OnUpdate systems:", n)
})
```

---

## Features Not Yet Ported

The following features from the upstream C flecs systems API are not yet available in Go flecs. They are listed so you can plan accordingly.

**Custom pipeline phases** — In C, any entity tagged with `EcsPhase` can be a pipeline phase and phases are ordered via `DependsOn` pairs. Go flecs has exactly four hard-coded built-in phases (`PreUpdate`, `OnFixedUpdate`, `OnUpdate`, `PostUpdate`). Not yet ported in Go flecs.

**DependsOn ordering between systems** — C lets applications add `(DependsOn, OtherSystem)` to order two systems within a phase independently of registration order. Go flecs orders within a phase strictly by registration order. Not yet ported in Go flecs.

**System tags / disabling** — C provides `ecs_enable(world, sys, false)` and the `EcsDisabled` tag to pause a system without removing it. Go flecs has no equivalent; use `sys.Close()` to remove and `NewSystem` again to re-add. Not yet ported in Go flecs.

**Rate filters** (`SetInterval` / `SetRate`) — run a system every N frames or at a fixed wall-clock interval without restructuring the pipeline. Not yet ported in Go flecs.

**Single-system `Run` out-of-pipeline** — C provides `ecs_run` to invoke one system synchronously outside the pipeline. Not yet ported in Go flecs.

**`RunWorker` / explicit thread dispatch** — C provides `ecs_run_worker` for manual entity-range partitioning. Not yet ported in Go flecs.

**Pipeline introspection** — C lets applications iterate over the ordered system list in a pipeline. Not yet ported in Go flecs.
