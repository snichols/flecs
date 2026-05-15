# Timer addon

The Timer addon (Phase 16.36, v0.91.0) provides entity-based timers and rate filters that drive system tick rates. A `Timer` or `RateFilter` is a first-class entity; any number of systems can bind to it via `(*System).SetTickSource(e)` and fire only when that entity fires this tick.

---

## Concepts

### Timer vs OnFixedUpdate frame timing

The Timer addon is **distinct** from the OnFixedUpdate frame-timing accumulator in `timer.go` (`Time()`, `FrameCount()`, `SetFixedTimestep`). That file tracks the simulation clock and controls fixed-step dispatch. This addon creates separate, entity-based timers that are independent of the fixed-step machinery.

### Timer vs per-system SetInterval/SetRate

The Timer addon is also **distinct** from the per-system rate gates in `system.go`:
- `(*System).SetInterval(d)` — a per-system gate; not shared; driven by the same dt passed to Progress.
- `(*System).SetRate(n)` — a per-system tick-count gate; not shared.

The Timer addon creates **shared** gate entities. Multiple systems can bind to the same Timer entity and all fire (or all skip) together. This is the canonical pattern when several systems need to be synchronized to the same timing.

### Non-goal: wall-clock OS timers

`Timer.Elapsed` advances by the `dt` argument passed to `World.Progress` — it never reads `time.Now()`. If you need real-time OS timers, use `time.AfterFunc` outside the ECS.

---

## Components

### Timer

```go
type Timer struct {
    Timeout    time.Duration
    Elapsed    time.Duration
    Active     bool
    SingleShot bool
    Fired      bool // transient: true during the tick this timer fires
}
```

- `Timeout` — the period or one-shot delay.
- `Elapsed` — accumulated simulation time since last fire (or since creation/reset).
- `Active` — false = paused; tickAllTimers skips it.
- `SingleShot` — if true, deactivates after the first fire.
- `Fired` — transient flag set by `tickAllTimers` before phase dispatch; cleared at the start of the next tick's timer pass. Use `IsTimerFired` to read it from outside Progress.

### RateFilter

```go
type RateFilter struct {
    Rate        int32
    TickCount   int32
    Src         ID
    TimeElapsed time.Duration
    Fired       bool // transient
}
```

- `Rate` — fires every Nth parent tick; `Rate <= 1` means every parent tick.
- `TickCount` — counter toward `Rate`; reset to 0 on each fire.
- `Src` — `0` → world frame (every `Progress` call); non-zero → reads `Fired` from that `Timer` or `RateFilter` entity.
- `TimeElapsed` — wall-clock accumulated since last fire; reset on fire.
- `Fired` — transient, same semantics as `Timer.Fired`.

---

## API

### Creating timers

```go
// Single-shot: fires once after 500ms, then Active=false.
timerE := flecs.NewTimer(fw, 500*time.Millisecond)

// Repeating: fires every 100ms.
intervalE := flecs.NewInterval(fw, 100*time.Millisecond)
```

### Configuring existing entities

```go
// Convert entity e to a single-shot timer (resets Elapsed/Fired, sets Active=true).
flecs.SetTimeout(fw, e, 200*time.Millisecond)

// Convert entity e to an interval timer.
flecs.SetInterval(fw, e, 50*time.Millisecond)
```

### Lifecycle control

```go
flecs.StartTimer(fw, e)  // Active=true, Elapsed=0, Fired=false
flecs.StopTimer(fw, e)   // Active=false (Elapsed preserved)
flecs.ResetTimer(fw, e)  // Elapsed=0, Fired=false (Active unchanged)
```

### Reading timer state

```go
// Inside a Read or Write scope:
timeout := flecs.GetTimeout(fr, e)    // Timeout field
interval := flecs.GetInterval(fr, e)  // alias for GetTimeout
fired := flecs.IsTimerFired(fr, e)    // Fired flag (Timer or RateFilter)
```

### Rate filters

```go
// Fires every 3rd Progress call (Src=0 → world frame).
rfE := flecs.NewRateFilter(fw, 3, 0)

// Fires every 2nd time the interval timer fires.
rfE2 := flecs.NewRateFilter(fw, 2, intervalE)

// Change the rate on an existing RateFilter entity.
// Note: this free function is distinct from (*System).SetRate(n).
flecs.SetRate(fw, rfE, 5)
```

### Binding systems

```go
sys := flecs.NewSystem(w, q, fn)

// System fires only when intervalE fires this tick.
sys.SetTickSource(intervalE)

// Read back the bound entity.
fmt.Println(sys.TickSource()) // intervalE

// Clear the binding.
sys.SetTickSource(0)
```

`SetTickSource` returns `*System` for fluent chaining:

```go
flecs.NewSystem(w, q, fn).SetTickSource(rfE)
```

---

## Pipeline integration

On each `World.Progress(dt)` call, before any phase runs:
1. `tickAllTimers` — clears the previous tick's `Fired` flags and accumulates `dt`, firing each active timer that has passed its timeout.
2. `tickAllRateFilters` — clears the previous tick's `Fired` flags, reads parent `Fired`, and increments each filter's counter.

The `OnFixedUpdate` phase may run multiple sub-steps per `Progress` call, but **timers are ticked exactly once per outer Progress call** — not per fixed sub-step. A timer with `Timeout < fixedTimestep` will still only see one accumulation per Progress call.

### System gate precedence

Inside each phase, the dispatch gate order is:

1. `enabled` flag (SetEnabled) — skip if false.
2. `interval` gate (SetInterval on *System) — skip if not enough time has elapsed.
3. `rate` gate ((*System).SetRate) — skip if the tick-count modulus does not match.
4. `tickSource` gate (SetTickSource) — skip if the bound entity's `Fired` is false.

All four gates compose with AND semantics. If any gate skips the system, all subsequent gates are not evaluated. The skipped tick increments `statsSkipped` for Stats addon visibility.

To avoid ambiguity, prefer using `SetTickSource` **alone** without `(*System).SetInterval` or `(*System).SetRate` on the same system. Both combinations are legal; they AND-compose.

---

## Chaining

RateFilters can chain: `Src` may point to another RateFilter.

```go
// RF1: fires every 2nd Progress call.
rf1 := flecs.NewRateFilter(fw, 2, 0)
// RF2: fires every 3rd time RF1 fires → every 6th Progress call.
rf2 := flecs.NewRateFilter(fw, 3, rf1)
```

For correct chaining, create parent RateFilters before child RateFilters. `tickAllRateFilters` processes entities in table row order (creation order within the same archetype), so the parent's `Fired` flag is up-to-date when the child is evaluated.

---

## Naming disambiguation

| Name | Signature | What it configures |
|---|---|---|
| `flecs.SetRate(fw, e, n)` | `*Writer, ID, int32` | `RateFilter.Rate` on an entity |
| `(*System).SetRate(n)` | `int32` | per-system tick-count gate (no entity) |
| `flecs.SetInterval(fw, e, d)` | `*Writer, ID, time.Duration` | `Timer.Timeout` + `SingleShot=false` on an entity |
| `(*System).SetInterval(d)` | `time.Duration` | per-system interval gate (no entity) |

---

## Deleted tick-source entities

If a system's `TickSource` entity is deleted, `getRefOnWorld` returns nil and `Fired` defaults to false — the system simply never fires again. No cleanup hooks fire; no panic occurs. Document this expected behavior to callers.

---

## JSON and snapshot round-trips

`Timer` and `RateFilter` are plain typed components stored in archetype columns. They round-trip automatically through `MarshalJSON`/`UnmarshalJSON` and `TakeSnapshot`/`RestoreSnapshot` without addon-specific serialization code.

**Pre-registration requirement for JSON:** the target world must have `Timer` and `RateFilter` registered (via `RegisterComponent[Timer]` / `RegisterComponent[RateFilter]`, or by calling any Timer addon constructor) before `UnmarshalJSON` is called. The component type names must match exactly.

For deterministic component entity IDs across worlds, register the component types in the same order before creating any user entities:

```go
w1 := flecs.New()
flecs.RegisterComponent[flecs.Timer](w1)
flecs.RegisterComponent[flecs.RateFilter](w1)
// ... create timer entities ...
data, _ := w1.MarshalJSON()

w2 := flecs.New()
flecs.RegisterComponent[flecs.Timer](w2)
flecs.RegisterComponent[flecs.RateFilter](w2)
_ = w2.UnmarshalJSON(data)
```

Snapshots (`TakeSnapshot`/`RestoreSnapshot`) are same-world and do not require pre-registration.

---

## Divergences from upstream C flecs

Upstream C flecs (`src/addons/timer.c`, `include/flecs.h`):

- `EcsTimer` / `EcsRateFilter` struct layouts match closely. Go uses `time.Duration` for `Timeout`, `Elapsed`, and `TimeElapsed` instead of `ecs_ftime_t` (float32).
- Upstream `ecs_set_tick_source` / `ecs_get_tick_source` use a C component `EcsPipelineTick` to propagate the fired flag. Go stores `Fired` directly on `Timer` and `RateFilter`, avoiding the extra component.
- The per-tick loop in `tickAllTimers` subtracts `Timeout` repeatedly until `Elapsed < Timeout`, carrying the remainder into the next tick. This matches the upstream accumulator (equivalent to `timer.c:33–35` subtract-and-carry semantics).
- Combining `SetTickSource` with `(*System).SetInterval` or `(*System).SetRate` is allowed (AND-composed). Upstream C rejects the combination; Go allows it because the existing per-system gates and the tick-source gate compose cleanly. See the [Rate filters section in Systems.md](Systems.md#rate-filters) and the [TickSource section](Systems.md#ticksource).

---

## See Also

- [Systems.md § TickSource](Systems.md#ticksource) — precedence rules, examples.
- [Systems.md § Rate filters](Systems.md#rate-filters) — per-system `SetInterval`/`SetRate` gates.
- [Stats.md](Stats.md) — `statsSkipped` counts tick-source-gated skips.
- [Snapshots.md](Snapshots.md) — binary snapshot round-trip.
