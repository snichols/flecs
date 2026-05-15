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

## Multi-period aggregation (v0.93.0)

Phase 16.38 adds ring-buffer aggregation that mirrors upstream's `FlecsMonitor` windowing model.

### `StatsPeriod` enum

```go
const (
    StatsSecond StatsPeriod = iota // last ≤60 ticks
    StatsMinute                    // last ≤60 second-reductions
    StatsHour                      // last ≤60 minute-reductions
)
```

> **Time-to-window note**: "1 tick ≈ 1 second" only when the pipeline runs at 1 Hz. Aggregation is tick-based, not wall-clock-based.

### `MetricGauge`

```go
type MetricGauge struct {
    Avg float64 `json:"avg"`
    Min float64 `json:"min"`
    Max float64 `json:"max"`
}
```

### `MetricCounter`

```go
type MetricCounter struct {
    Rate  float64 `json:"rate"`  // events/sec average over the window
    Value float64 `json:"value"` // cumulative count
}
```

### `StatsWindow`

Fixed 60-slot ring buffer of `MetricGauge` values.

```go
var sw flecs.StatsWindow
sw.Record(42.0)           // push instant value (Avg=Min=Max=42)
g := sw.Reduce()          // Avg/Min/Max across filled slots; zero-safe
last := sw.Last()         // most recently recorded gauge
```

`ECS_STAT_WINDOW = 60` matches upstream (`flecs/src/addons/monitor.c`).

### `WorldStatsAggregated`

Returned by `(*World).WorldStatsWindow(period)`.

```go
type WorldStatsAggregated struct {
    EntityCount    MetricGauge `json:"entity_count"`
    TableCount     MetricGauge `json:"table_count"`
    ArchetypeCount MetricGauge `json:"archetype_count"`
    FrameCount     MetricGauge `json:"frame_count"`
    TotalTime      MetricGauge `json:"total_time"`
    LastTickDelta  MetricGauge `json:"last_tick_delta"`
}
```

### `PipelineStatsAggregated`

Returned by `(*World).PipelineStatsWindow(period)`.

```go
type PipelineStatsAggregated struct {
    World   WorldStatsAggregated    `json:"world"`
    Phases  []PhaseStatsAggregated  `json:"phases,omitempty"`
    Systems []SystemStatsAggregated `json:"systems,omitempty"`
}
```

### Ring-buffer reduction cascade

- **Second window**: every `Progress` call pushes the current instant into slot `[head]`; `head = (head+1) % 60`.
- **Minute window**: every 60th `Progress`, the second ring is reduced into one slot of the minute ring.
- **Hour window**: every 60th minute-reduction, the minute ring is reduced into one slot of the hour ring.

This mirrors `flecs_stats_reduce` / `flecs_stats_reduce_last` in `flecs/src/addons/monitor.c`.

### `(*World).WorldStatsWindow(period StatsPeriod) WorldStatsAggregated`

```go
agg := w.WorldStatsWindow(flecs.StatsSecond)
fmt.Printf("entity avg over last 60 ticks: %.1f\n", agg.EntityCount.Avg)
```

### `(*World).PipelineStatsWindow(period StatsPeriod) PipelineStatsAggregated`

```go
agg := w.PipelineStatsWindow(flecs.StatsMinute)
fmt.Printf("per-minute avg frame count: %.1f\n", agg.World.FrameCount.Avg)
```

### `(*World).StatsTick()`

Manually advances the aggregator one tick using the currently committed world state. Designed for tests; the aggregator advances automatically in production via each `Progress` call.

### REST integration

See [FlecsRemoteApi.md](FlecsRemoteApi.md) for the `?period=` query parameter on `GET /stats/world` and `GET /stats/pipeline`.

## Scope note (v1)

Phase 16.38 adds second/minute/hour ring-buffer aggregation. The following remain deferred:

- **Histogram percentiles** (P50/P95/P99) — Avg/Min/Max suffices for v1.
- **Custom user metrics** — Phase 16.38 covers only the world/pipeline metrics already exposed.
- **Variable window sizes** — 60 is hardcoded (matches upstream `ECS_STAT_WINDOW`).
- **Aggregator state in snapshots** — stats are observable-only; snapshot wire format unchanged.
- **Memory allocator stats** — Go's runtime owns allocations; `runtime.MemStats` integration is out of scope.
- **Alerts-on-threshold** — users can wire `StatsSnapshot` → `RegisterAlert` manually (see [Alerts.md](Alerts.md)).
