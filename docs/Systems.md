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

| Phase accessor | Returns | Purpose |
|---|---|---|
| `w.PreUpdate()` | `*flecs.Phase` | Input, network receive |
| `w.OnFixedUpdate()` | `*flecs.Phase` | Physics (fixed timestep, accumulator loop) |
| `w.OnUpdate()` | `*flecs.Phase` | Game logic — **default for `NewSystem`** |
| `w.PostUpdate()` | `*flecs.Phase` | Rendering, network send |

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

### Merge hooks {#merge-hooks}

**Shipped in v0.78.0.** Persistent world-level callbacks that fire at each deferred-command merge boundary.

```go
// Register — both return an int ID for later removal.
preID  := flecs.OnPreMerge(w, func(fw *flecs.Writer) { /* fires before flush */ })
postID := flecs.OnPostMerge(w, func(fw *flecs.Writer) { /* fires after flush */ })

// Remove — idempotent; stale IDs are silent no-ops.
flecs.RemovePreMergeHook(w, preID)
flecs.RemovePostMergeHook(w, postID)
```

**Fire ordering within a merge boundary:**
1. All pre-merge hooks fire in registration order.
2. The deferred command queue is flushed (structural mutations applied).
3. All post-merge hooks fire in registration order.

**Mutation semantics:**
- *Pre-merge hook mutations* are batched with the current merge: they are queued into the same command queue that is about to be flushed, so they are visible immediately after the enclosing `Write` scope returns.
- *Post-merge hook mutations* queue for the next merge: they land in the fresh command queue installed after the flush, and are applied when the next `Write` scope exits.

**Multi-stage (multi-threaded systems) policy:** When a multi-threaded system (`SetMultiThreaded(true)`) is active, the worker-stage flush involves N worker queues + stage-0. Hooks fire **once** for this entire merge cycle — pre fires before the first worker-stage flush, post fires after the stage-0 flush. Hooks do not fire once per worker stage.

**Hook registration lifetime:** Hooks persist until removed via `RemovePreMergeHook` / `RemovePostMergeHook`. Registration must happen outside a `Write` scope. A hook registered from inside another hook fires starting on the next merge, not the current one.

**Re-entry guard:** Calling `w.Write` from inside a hook panics with `flecs.ErrMergeReentry`. Use the `*Writer` passed to the hook directly for mutations.

**Upstream C analogy:** Upstream C flecs has no persistent merge-hook API. The closest is `ecs_run_post_frame` (`include/flecs.h:2204-2216`), which is one-shot and per-frame rather than persistent and per-merge. Go flecs merge hooks are a deliberate divergence.

---

## Custom Pipeline Phases

`NewPhase` creates additional pipeline phases beyond the four built-ins. Every custom phase must declare its position in the pipeline via `DependsOn`:

```go
w := flecs.New()
posID := flecs.RegisterComponent[Position](w)
q := flecs.NewCachedQuery(w, posID)

// Custom phase that runs after OnUpdate.
postPhysics := flecs.NewPhase(w, "PostPhysics")
postPhysics.DependsOn(w.OnUpdate())

flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(dt float32, it *flecs.QueryIter) {
    for it.Next() { /* integrate velocities */ }
})
flecs.NewSystemInPhase(w, postPhysics, q, func(dt float32, it *flecs.QueryIter) {
    for it.Next() { /* resolve constraints */ }
})

w.Progress(1.0 / 60.0)
// execution order: PreUpdate → OnFixedUpdate → OnUpdate → PostPhysics → PostUpdate
```

Chains of custom phases are supported:

```go
phaseA := flecs.NewPhase(w, "A")
phaseA.DependsOn(w.OnUpdate())

phaseB := flecs.NewPhase(w, "B")
phaseB.DependsOn(phaseA) // B runs after A

phaseC := flecs.NewPhase(w, "C")
phaseC.DependsOn(phaseA) // C also runs after A (sibling of B)
```

Kahn's topological sort determines the execution order. Registration order breaks ties between sibling phases.

**Orphan custom phases** — a custom phase that has no `DependsOn` edge causes `Progress` to panic on the first tick:

```go
orphan := flecs.NewPhase(w, "Orphan") // no DependsOn
w.Progress(0)                          // panics: "phase "Orphan" has no DependsOn relation"
```

**Cycle detection** — a dependency cycle among phases causes `Progress` to panic:

```go
a := flecs.NewPhase(w, "A")
b := flecs.NewPhase(w, "B")
a.DependsOn(w.OnUpdate())
b.DependsOn(a)
a.DependsOn(b) // cycle: A→B→A
w.Progress(0)  // panics: "phase cycle detected"
```

**Enabling / disabling** a phase skips it (and all its systems) during `Progress`:

```go
postPhysics.SetEnabled(false)
w.Progress(0) // PostPhysics systems are skipped
postPhysics.SetEnabled(true)
```

---

## System DependsOn Ordering

Within a single phase, systems normally run in registration order. `(*System).DependsOn` overrides that to guarantee one system runs after another, regardless of registration order:

```go
w := flecs.New()
posID := flecs.RegisterComponent[Position](w)
q := flecs.NewCachedQuery(w, posID)

// Register B before A, but want A to always run first.
sysB := flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(dt float32, it *flecs.QueryIter) {
    for it.Next() { /* B: consume position */ }
})
sysA := flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(dt float32, it *flecs.QueryIter) {
    for it.Next() { /* A: produce position */ }
})
sysB.DependsOn(sysA) // B runs after A, overriding registration order
```

`DependsOn` is idempotent and returns the receiver for chaining. It panics if the two systems are in different phases.

**Cycle detection** — a cycle among system DependsOn edges panics at the start of `Progress`:

```go
sysA.DependsOn(sysB)
sysB.DependsOn(sysA) // cycle
w.Progress(0)         // panics: "system cycle detected"
```

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

---

## Disabling a System

`SetEnabled(false)` pauses a system without removing it. The system remains registered and visible to introspection; `Progress` simply skips it during dispatch. `SetEnabled(true)` re-enables it.

```go
sys := flecs.NewSystem(w, moveQ, func(dt float32, it *flecs.QueryIter) { /* ... */ })

sys.SetEnabled(false)    // pause — Progress will skip this system
fmt.Println(sys.IsEnabled()) // false

w.Progress(dt)           // sys does not run

sys.SetEnabled(true)     // resume
w.Progress(dt)           // sys runs again
```

Unlike `Close`, `SetEnabled(false)` is **reversible**. Use it to implement pause/resume semantics without recreating the system.

**Deferred-disable semantics:** `Progress` snapshots the active set at the start of each phase. Disabling a system from within another system's callback in the same phase has no effect on the current frame (the active set was already captured). The change takes effect from the next `Progress` call.

**`RunSystem` ignores the flag.** Explicit out-of-pipeline invocation always runs the system regardless of its `enabled` state — matching `ecs_run` semantics in upstream C flecs.

---

## Rate Filters

Two independent gates let a system run less often than every `Progress` tick, without restructuring the pipeline or adding new phases.

**`SetInterval(d time.Duration)`** — run at most once per accumulated wall-clock duration `d`. The accumulator grows by `dt` on every tick; when it reaches `d` the system fires and `d` is subtracted (not reset), preserving carry-over for future ticks. If a single frame's `dt` vastly exceeds `d`, the remainder is clamped to `0` to prevent runaway catch-up — matching upstream C `timer.c:33–35`.

**`SetRate(n int32)`** — run every Nth pipeline visit. `n == 0` or `n == 1` disables rate gating (runs every tick). The counter only advances while the system is enabled.

```go
// Run the save system at most once every 5 seconds of accumulated dt.
saveSys.SetInterval(5 * time.Second)

// Run the AI update system every 4th pipeline tick.
aiSys.SetRate(4)

// Check current settings.
d := saveSys.GetInterval()  // 5s
n := aiSys.GetRate()        // 4
```

**Gates compose (AND semantics).** A system with both `interval` and `rate` set fires only on ticks where both gates pass simultaneously. This diverges from upstream C flecs, which rejects systems with both fields set; Go-flecs allows the combination because there is no `tick_source` abstraction and the two filters compose cleanly per-system.

**Interaction with `SetEnabled`.** While a system is disabled, neither the rate counter nor the interval accumulator advance. Re-enabling resumes from the pre-disable state — no catch-up storm occurs.

**`RunSystem` bypasses both gates.** Explicit out-of-pipeline invocation ignores interval and rate, matching its behaviour with the `enabled` flag.

**Thread safety.** `SetInterval` and `SetRate` use plain field writes (no atomics), matching the `SetEnabled` precedent. Modify only from the goroutine that drives `Progress`, between ticks — not from inside a system callback during dispatch.

---

## Single-system Run (out-of-pipeline)

`RunSystem(s, dt)` invokes one system synchronously, outside the normal pipeline. Phase ordering, parallel batching, multi-threaded splitting, and the `enabled` flag are all bypassed.

```go
// Invoke the system once with a fixed dt, regardless of phase or enabled state.
flecs.RunSystem(sys, 1.0/60.0)
```

Mutations performed inside the callback are deferred and flushed before `RunSystem` returns — the same `deferScope` wrap as `ecs_run` in upstream C flecs. This means structural changes (component sets, entity deletes) are visible immediately after `RunSystem` completes.

`RunSystem` panics if `s` is `nil` or if the system has been closed (`IsClosed()` is true).

---

## RunSystemWorker

`RunSystemWorker(w, sys, workerIndex, workerCount, dt)` is the out-of-pipeline counterpart to within-system multi-threaded dispatch. Callers fan out N goroutines, each calling `RunSystemWorker` with a distinct `workerIndex` in `[0, workerCount)`, and each goroutine processes a **disjoint slice** of `sys`'s matched entities.

### Goroutine fan-out example

```go
const workers = 4
var wg sync.WaitGroup
for i := range workers {
    wg.Add(1)
    go func(idx int) {
        defer wg.Done()
        flecs.RunSystemWorker(w, sys, idx, workers, 1.0/60.0)
    }(i)
}
wg.Wait()
// All workers have completed and their deferred mutations are flushed.
```

### Partition algorithm

Per archetype table, the row range is split as `count / workerCount` with the first `count % workerCount` workers each receiving one extra row — the same algorithm as the pipeline's multi-threaded dispatcher and upstream `ecs_worker_next`. The partition is **deterministic** for a given world state.

### Per-call stage

Each `RunSystemWorker` call allocates a fresh per-call command queue owned exclusively by the calling goroutine. Deferred mutations (via `it.Writer()`) go into this private queue and are flushed before the call returns. Concurrent callers flush in undefined order; each flush is serialized by the world write mutex so concurrent flushes do not race each other or a simultaneous `World.Write` scope.

### Semantics decisions

- **Disabled flag bypassed** — like `RunSystem`, `RunSystemWorker` runs the system regardless of `SetEnabled(false)`.
- **No worker pool** — the caller owns goroutine spawn/join; `RunSystemWorker` is a synchronous single-call primitive.
- **Sparse-only queries not partitioned** — for pure-DontFragment (sparse-only) queries, the sparse driver is iterated in full by every worker. Callers who need sparse partitioning must coordinate assignment externally.
- **Flush ordering undefined across concurrent callers** — each worker's deferred mutations are applied atomically (under the world write mutex), but the order in which concurrent workers flush is unspecified.

### Panics

- `w` or `sys` is nil
- `sys` is closed (`IsClosed() == true`)
- `workerCount ≤ 0`
- `workerIndex < 0` or `workerIndex ≥ workerCount`

---

## Pipeline Introspection

`Reader` exposes three methods for inspecting the registered system list at runtime without mutating world state.

### `Phases() []*Phase`

Returns all phases (built-in and custom) in topological execution order:

```go
w.Read(func(r *flecs.Reader) {
    for _, phase := range r.Phases() {
        fmt.Println(phase.Name()) // e.g. "PreUpdate", "OnFixedUpdate", ...
    }
})
```

### `SystemsInPhase(phase *Phase) []*System`

Returns a snapshot of all registered (including disabled) non-closed systems in the given phase, in topological execution order:

```go
w.Read(func(r *flecs.Reader) {
    systems := r.SystemsInPhase(w.OnUpdate())
    for _, s := range systems {
        fmt.Println("enabled:", s.IsEnabled())
    }
})
```

Returns an empty (non-nil) slice when no systems are registered for the phase. Panics if `phase` is nil.

### `EachSystem(phase *Phase, fn func(*System) bool)`

Zero-alloc callback variant — no slice is allocated. `fn` returning `false` halts iteration early:

```go
w.Read(func(r *flecs.Reader) {
    r.EachSystem(w.OnUpdate(), func(s *flecs.System) bool {
        fmt.Println("system enabled:", s.IsEnabled())
        return true // continue
    })
})
```

Both `SystemsInPhase` and `EachSystem` include disabled systems. The pipeline executor applies the `enabled` filter at dispatch time; introspection sees the complete registered set.

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

While `Progress` is running the world is in a deferred state. Structural mutations — `Set`, `Remove`, `Delete`, `AddID` — are enqueued as commands and applied after the phase completes.

**Per-stage routing (Phase 16.50):** within a parallel batch, each system is assigned an exclusive stage slot (a pre-allocated command queue). The dispatcher groups systems into sequential waves of at most `WorkerCount`; within each wave, system position `wavePos` owns stage `(wavePos+1)`. Because wave positions are disjoint within a wave and waves execute sequentially (via `wg.Wait()`), deferred mutations from concurrent goroutines never share a queue — zero synchronization on the hot path.

After all waves complete, stages are merged into the world in ascending ID order (stage 0 last). Pre/post merge hooks fire once per batch merge boundary.

Direct in-place mutation of component data via `Field[T]` slices is safe: within a batch, write-set checks guarantee disjoint component ownership across systems.

> **Cross-link:** Per-stage routing for multi-threaded systems was introduced in Phase 12.1; the parallel-batch extension shipped in Phase 16.50. See [CHANGELOG.md](../CHANGELOG.md) for implementation details.

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

`LastFramePhases` is a `[]PhaseStats` slice indexed by topological phase order. For a world with only the four built-in phases:

| Index | Phase |
|---|---|
| 0 | PreUpdate |
| 1 | OnFixedUpdate |
| 2 | OnUpdate |
| 3 | PostUpdate |

Custom phases appear after the built-ins in their DependsOn-sorted order. Each `PhaseStats` entry has `Name`, `SystemCount`, and `Duration`. For `OnFixedUpdate`, `Duration` is the sum across all fixed-step iterations in the frame.

`SystemCountInPhase` returns the live count for a single phase:

```go
w.Read(func(r *flecs.Reader) {
    n := w.SystemCountInPhase(w.OnUpdate())
    fmt.Println("OnUpdate systems:", n)
})
```

---

## TickSource

`(*System).SetTickSource(e ID)` binds a system to a `Timer` or `RateFilter` entity (created by the Timer addon). The system fires only when that entity's `Fired` flag is true this tick — i.e., only on the ticks where the timer or rate filter fires.

```go
// Create an interval timer that fires every 100ms.
intervalE := flecs.NewInterval(fw, 100*time.Millisecond)

// Bind a system to it — the system runs only when the timer fires.
sys := flecs.NewSystem(w, q, fn)
sys.SetTickSource(intervalE)

// Read back the binding.
fmt.Println(sys.TickSource()) // intervalE

// Clear the binding (system runs every tick again).
sys.SetTickSource(0)
```

`SetTickSource` returns `*System` for fluent chaining:

```go
flecs.NewSystem(w, q, fn).SetTickSource(rfE)
```

### Precedence vs SetInterval and SetRate

The gate evaluation order within each phase is:

1. `enabled` flag — skip if false.
2. `interval` gate (`(*System).SetInterval`) — skip if not enough time has elapsed.
3. `rate` gate (`(*System).SetRate`) — skip if the tick-count modulus does not match.
4. `tickSource` gate (`SetTickSource`) — skip if the bound entity's `Fired` is false.

If any gate skips the system, subsequent gates are not evaluated. All four compose with AND semantics.

**Recommendation:** prefer `SetTickSource` alone without `(*System).SetInterval` or `(*System).SetRate` on the same system. Combining them is legal (AND-compose) but reduces readability. Use the Timer addon's rate filters to express timing relationships between systems instead.

Note: `flecs.SetRate(fw, e, n)` (free function) configures a `RateFilter` entity's `Rate` field. `(*System).SetRate(n)` configures a per-system tick-count gate. These are distinct — see [Timer.md § Naming disambiguation](Timer.md#naming-disambiguation).

### Deleted tick-source entities

If the bound entity is deleted, `getRefOnWorld` returns nil and `Fired` defaults to false. The system simply never fires — no crash, no panic.

### Timer addon reference

For the full `Timer` / `RateFilter` component API, constructor functions, and chaining examples, see [Timer.md](Timer.md).

---

## Context cancellation

`ProgressContext` is the context-aware sibling of `Progress`. It honours a
`context.Context` and returns early with `ctx.Err()` if the context is cancelled
or its deadline expires before the frame completes.

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
defer cancel()
if err := w.ProgressContext(ctx, 1.0/60.0); err != nil {
    log.Println("frame cancelled:", err) // context.DeadlineExceeded
}
```

Cancellation is checked:
- Before each pipeline phase.
- After each serial system completes.
- After each parallel batch wave (with a mandatory `mergeWorkerStages` flush, so
  deferred mutations from completed waves are preserved).

`Progress` continues to exist and delegates to `ProgressContext(context.Background(), dt)` — all existing code compiles unchanged.

### RunSystemContext

`RunSystemContext` is the context-aware version of `RunSystem` for out-of-pipeline
single-system invocation:

```go
err := flecs.RunSystemContext(ctx, sys, dt)
```

Returns `ctx.Err()` if the context is already cancelled before execution starts.

For the full cancellation API including `TakeSnapshotContext`, `EachContext`, and
REST endpoint behaviour, see [docs/Cancellation.md](Cancellation.md).

---

## Features Not Yet Ported

The following features from the upstream C flecs systems API are not yet available in Go flecs. They are listed so you can plan accordingly.

**`RunWorker` / explicit thread dispatch** — C provides `ecs_run_worker` for manual entity-range partitioning. Not yet ported in Go flecs.

**Pipeline introspection** — C lets applications iterate over the ordered system list in a pipeline. Not yet ported in Go flecs.

---

## See Also

- [Quickstart](Quickstart.md) — hands-on introduction to systems and `Progress`.
- [Queries.md](Queries.md) — cached queries that power systems; iteration and change detection.
- [ObserversManual.md](ObserversManual.md) — event-driven reactions as a complement to per-frame systems.
- [Manual](Manual.md) — top-level reference hub covering the concurrency model, ExclusiveAccess, and parallel dispatch in depth.
