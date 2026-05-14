# Stats Addon

The Stats addon ports the upstream FlecsMonitor/FlecsStats addon to Go as v0.84.0. It collects per-tick system execution times, per-phase timings, and world-level counters, and exposes them as a point-in-time `PipelineStats` snapshot.

> **Go-side rename**: The upstream C addon is named *Monitor*. To avoid a naming collision with the Phase 16.10 monitor observers in `monitor_observer.go`, the Go surface uses the name **Stats** (`StatsSnapshot`, `stats_addon.go`).

## API

### `(*World).StatsSnapshot() PipelineStats`

Returns a `PipelineStats` snapshot reflecting the state at the end of the most recently completed `Progress` call. Safe to call from any goroutine — the snapshot is a fully copied value with no aliased slices.

```go
snap := w.StatsSnapshot()
fmt.Printf("Frame: %d, Entities: %d\n", snap.World.FrameCount, snap.World.EntityCount)
```

### `(*System).SetName(name string) *System`

Sets a display name for the system, returned in `SystemStats.Name`. Returns the receiver for fluent chaining.

```go
flecs.NewSystem(w, q, fn).SetName("physics")
```

If `SetName` is not called, the system receives an auto-generated name: `"system-0"`, `"system-1"`, etc.

## Types

### `PipelineStats`

```go
type PipelineStats struct {
    World   WorldStats
    Systems []SystemStats
    Phases  []PhaseStats
}
```

`Systems` and `Phases` are nil before the first `Progress` call.

### `WorldStats`

```go
type WorldStats struct {
    EntityCount    int
    TableCount     int
    ArchetypeCount int     // same as TableCount
    FrameCount     uint64
    TotalTime      float64 // seconds
    LastTickDelta  float64 // dt of the most recent Progress call
}
```

### `SystemStats`

```go
type SystemStats struct {
    Name             string
    LastTickDuration time.Duration // zero if system did not run this tick
    Invocations      uint64        // total runs since world creation
    AvgDuration      time.Duration // TotalDuration / Invocations
    TotalSkipped     uint64        // ticks skipped by interval/rate gating
}
```

`TotalSkipped` counts interval- and rate-gated skips only; disabled-system ticks are not counted.

### `PhaseStats`

`StatsSnapshot` returns a `[]PhaseStats` in topological phase order. The struct is the same type used by `(*World).Stats()`, extended with cumulative fields:

```go
type PhaseStats struct {
    Name               string
    SystemCount        int
    Duration           time.Duration // last-tick wall-clock time
    CumulativeDuration time.Duration // total across all Progress calls
    Invocations        uint64        // Progress calls that visited this phase
}
```

## Concurrency

`StatsSnapshot` acquires a read lock (`sync.RWMutex`) and returns a fully copied value. It is safe to call from a monitoring goroutine while `Progress` runs on another goroutine. The snapshot always reflects a consistent end-of-tick state — it never captures a mid-tick view.

```go
// Monitoring goroutine pattern:
go func() {
    for range ticker.C {
        snap := w.StatsSnapshot()
        reportMetrics(snap)
    }
}()
```

## Snapshot semantics

Each call to `StatsSnapshot` returns an independent copy. Retaining the returned value is safe; subsequent `Progress` calls do not modify it.

```go
snap1 := w.StatsSnapshot()
w.Progress(0.016) // snap1 is unchanged
snap2 := w.StatsSnapshot() // snap2 reflects the new tick
```

## Examples

### Per-system profiling

```go
w.Progress(0.016)
snap := w.StatsSnapshot()
for _, ss := range snap.Systems {
    if ss.LastTickDuration > 5*time.Millisecond {
        log.Printf("slow system %q: %v (avg %v)", ss.Name, ss.LastTickDuration, ss.AvgDuration)
    }
}
```

### Rate-filtered system monitoring

```go
sys := flecs.NewSystem(w, q, fn).SetName("rare-sweep")
sys.SetRate(60) // runs every 60th tick

// After 300 ticks:
snap := w.StatsSnapshot()
// snap.Systems[0].Invocations == 5
// snap.Systems[0].TotalSkipped == 295
```

### Phase timing

```go
snap := w.StatsSnapshot()
for _, ph := range snap.Phases {
    pct := float64(ph.CumulativeDuration) / float64(ph.Invocations)
    fmt.Printf("Phase %-16s avg %v\n", ph.Name, time.Duration(pct))
}
```

## Scope note (v1)

This is a v1 port: per-tick deltas and cumulative totals only. The following upstream features are explicitly deferred:

- **Time-window aggregation** (`EcsPeriod1s` / `1m` / `1h` / `1d` / `1w`) — no ring buffers or windowed reduce/aggregate pipelines.
- **Persistence** — stats reset on world recreation.
- **REST endpoint** — deferred until the REST/Explorer port (Phase 16.30+).
- **Memory allocator stats** — Go's runtime owns allocations; `runtime.MemStats` integration is out of scope.
- **HTTP stats** — no HTTP server in scope.
- **Alerts-on-threshold** — users can wire `StatsSnapshot` → `RegisterAlert` manually (see [Alerts.md](Alerts.md)).
