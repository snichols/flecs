## Goal

Extend the Stats addon (Phase 16.29 / v0.84.0) to expose **multi-period aggregated windows** — per-second, per-minute, per-hour — for all currently exposed world and pipeline metrics. The addon today reports single-instant gauges via `(*World).StatsSnapshot()`; this phase adds ring-buffer aggregation that mirrors upstream's `FlecsMonitor` windowing model, plus a `?period=` query parameter on the REST stats endpoints.

Target version: **v0.93.0**. Phase: **16.38**.

### Upstream reference

The reduction algorithm matches upstream's `flecs/src/addons/monitor.c` and `flecs/include/flecs/addons/monitor.h`:

- `EcsStatsHeader` carries `reduce_count`, `t`, the per-window `gauge`, and the `counter`/`rate` accumulators.
- `ECS_STAT_WINDOW = 60` is the fixed slot count for every period.
- `flecs_stats_reduce`, `flecs_stats_reduce_last`, `flecs_stats_repeat_last` drive the cascade:
  - Every tick records into the **second** ring.
  - Every 60 ticks the second ring reduces into one slot of the **minute** ring.
  - Every 60 minute-ticks the minute ring reduces into one slot of the **hour** ring.
- `ecs_world_stats_t` and `ecs_pipeline_stats_t` are the per-metric containers; each field is an `ecs_metric_t` exposing `gauge.avg/min/max` plus `counter.rate/value`.

Exact line numbers should be quoted from `monitor.c` in the implementation commit message and CHANGELOG entry (the iterate agent has network access to fetch the upstream tree at implementation time).

### Go-port surface

Build on @stats_addon.go (164 LOC) — **extend, do not replace**. Preserve the existing instant-gauge API (`StatsSnapshot`, `WorldStats`, `PipelineStats`, `SystemStats`, `PhaseStats`).

New types:

- `MetricGauge` struct with `Avg`, `Min`, `Max float64`. Computed per-window.
- `MetricCounter` struct with `Rate float64` (events/sec average over the window) and `Value float64` (cumulative count).
- `StatsWindow` struct: fixed 60-slot `MetricGauge` ring + head index. Methods:
  - `Record(value float64)`
  - `Reduce() MetricGauge` — Avg/Min/Max across the 60 slots; zero-safe on empty window (no divide-by-zero).
  - `Last() MetricGauge`
- `WorldStatsAggregated` struct holding `Second`, `Minute`, `Hour` `MetricGauge` fields per world metric (EntityCount, TableCount, ArchetypeCount, FrameCount, TotalTime, LastTickDelta).
- `PipelineStatsAggregated` struct with the analogous shape for system count, time spent per phase, and per-system invocation/duration stats.

New `StatsPeriod` enum: `StatsSecond`, `StatsMinute`, `StatsHour` (and implicit `StatsInstant` for back-compat, or a sentinel value).

New API:

- `WorldStatsWindow(s scope, period StatsPeriod) WorldStatsAggregated` — returns the reduced gauge across the named window.
- `PipelineStatsWindow(s scope, period StatsPeriod) PipelineStatsAggregated`
- `(*World).StatsTick()` — manually advance the aggregator one tick (for tests).
- In production, the Stats addon registers a recurring system on `OnUpdate` (or hooks into `Progress`'s `statsCommit`) to tick once per Progress call.

Reduction algorithm (mirrors upstream):

- **Second window**: on every Progress, push current instant into `slot[head]`; `head = (head + 1) % 60`. After 60 ticks the window represents the last 60 ticks.
- **Minute window**: every 60th Progress, call `Reduce()` on the second ring and push the result into one slot of the minute ring.
- **Hour window**: every 60th minute-tick, reduce the minute ring and push into the hour ring.
- Time-to-window mapping: "1 tick ≈ 1 second" only when called from a 1Hz pipeline. Document the simplification explicitly — do not pretend to be wall-clock accurate.

### REST integration

@rest.go currently has `GET /stats/world` and `GET /stats/pipeline` returning instant gauges (Phase 16.31, lines 222–246). Add a `?period=` query parameter:

- `?period=instant` (default — preserves current behavior) → returns single-instant gauge (`Stats` struct shape unchanged)
- `?period=second` → returns reduced second window (`WorldStatsAggregated` / `PipelineStatsAggregated` shape)
- `?period=minute` → reduced minute window
- `?period=hour` → reduced hour window
- Unknown `period` value → `400 Bad Request`
- Each metric in the aggregated response is `{avg, min, max}` JSON.

Back-compat: omitting `?period=` MUST produce byte-identical JSON to the pre-16.38 endpoint.

### Required tests

Extend @stats_addon_test.go (currently 301 LOC):

- `TestStats_RingBuffer_Records60Ticks` — record 60 distinct values; verify `Reduce()` returns correct Avg/Min/Max.
- `TestStats_RingBuffer_Wraps` — record 70 values; verify only the last 60 are reflected.
- `TestStats_WindowReduction_MinuteFromSecond` — drive 60 ticks; verify minute window contains exactly one reduced slot.
- `TestStats_WindowReduction_HourFromMinute` — drive 3600 ticks; verify hour window contains exactly one reduced slot.
- `TestStats_WindowReduction_EmptyWindow` — read window before any ticks; zero-value safe (no divide-by-zero).
- `TestStats_WorldStatsWindow_Second` — drive Progress 60× with growing entity count; verify Second window Avg lands mid-range.
- `TestStats_PipelineStatsWindow_Second` — same, for pipeline metrics (system count, per-phase time, etc.).
- `TestRest_StatsWorld_PeriodSecond` — `GET /stats/world?period=second` returns 200 + JSON shaped as `WorldStatsAggregated`.
- `TestRest_StatsWorld_PeriodInvalid` → 400.
- `TestRest_StatsWorld_PeriodDefault` — omitting `?period=` returns the same JSON as before (back-compat).
- `TestRest_StatsPipeline_PeriodMinute` — pipeline endpoint with minute period.
- `TestStats_Snapshot_PreservesWindows` — `TakeSnapshot` then `RestoreSnapshot`: aggregator state survives. If genuinely too expensive, document the deferral and skip the test.
- `TestStats_JSON_Aggregated_RoundTrip` — `MarshalJSON` / `UnmarshalJSON` for `WorldStatsAggregated`.

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run ./...` clean
- `go test ./... -race -count=3` clean
- Coverage ≥ 95.0% (current baseline)

### Non-goals

- Histogram percentiles (P50/P95/P99) — Avg/Min/Max suffices for v1.
- Wall-clock-driven aggregation — tick-based; document the simplification.
- Custom user metrics — Phase 16.38 covers only the world/pipeline metrics already exposed.
- Variable window sizes — 60 is hardcoded (matches upstream).

### Documentation update matrix

- @docs/Stats.md — new section "Multi-period aggregation" with the `StatsPeriod` enum, `MetricGauge`/`MetricCounter` shapes, ring-buffer behavior, time-to-window mapping.
- @docs/FlecsRemoteApi.md — extend the stats section with the `?period=` query parameter and per-period JSON shape.
- @CHANGELOG.md — v0.93.0 entry (quote `monitor.c` line numbers for the reduction algorithm).
- @ROADMAP.md — bump "Shipped (through v0.92.0)" heading to v0.93.0; add a Phase 16.38 entry.
- @README.md — update the Stats feature row.

**Required doc-stale fixes in @docs/README.md (gate-failing if skipped):**

1. Line 126 — §14.3 Relationships: "Entity scoping (`ecs_set_scope` / `ecs_get_scope`) — not yet ported" is wrong. Actually shipped in v0.74.0 (Phase 16.19) via `WithinScope` / `PushScope` / `PopScope` / `GetScope`. Flip to ✅ with the same reference style as line 87.
2. Line 166 — §14.8 ComponentTraits: "`Constant` component trait — not yet ported" is wrong. Actually shipped as **`WriteOnce`** in v0.45.0 (Phase 15.13, with a deliberate Go-side rename from upstream's `EcsConstant`). Flip to ✅. Also note that upstream's `EcsConstant` has a **second, distinct use** in meta reflection for enum constants — that secondary use is a separate feature (meta system) and is NOT covered by `WriteOnce`. Document that distinction.
3. Line 178 — §14.9 FlecsRemoteApi: "Entity / component mutation endpoints — not yet ported" is wrong. Entity mutation shipped in v0.88.0 (Phase 16.33), component mutation in v0.89.0 (Phase 16.34), `GET /component` in v0.92.0 (Phase 16.37). Flip to ✅ with references to those phases.

## Constraints

- @stats_addon.go — Phase 16.29 entity-bearing addon (164 LOC). This is the file to extend. Preserve the existing `StatsSnapshot` / `WorldStats` / `PipelineStats` / `SystemStats` / `PhaseStats` types unchanged.
- @stats.go — Phase 16.31 backing `Stats` struct (156 LOC). Aggregated variants are additive; do not break the existing single-instant `(*World).Stats()` API.
- @stats_addon_test.go — existing 301 LOC test file. Extend, do not replace.
- @rest.go — `restStats`, `restStatsWorld`, and `restStatsPipeline` live around lines 214–246. Wire `?period=` parsing into the two `/stats/world` and `/stats/pipeline` handlers; preserve byte-identical JSON when the query parameter is absent.
- @snapshot.go — snapshot integration if you choose to preserve aggregator state across `TakeSnapshot` / `RestoreSnapshot`. Stats fields are not currently in the snapshot wire format; if including them, follow the existing migration pattern (version byte / additive field).
- @docs/Stats.md — primary documentation surface for the addon; new "Multi-period aggregation" section lives here.
- @docs/FlecsRemoteApi.md — REST endpoint reference; document `?period=` parameter and per-period response shape.
- @docs/README.md — survey-table doc; lines 126, 166, 178 must be flipped (see "Required doc-stale fixes" above).
- @CHANGELOG.md — v0.93.0 entry; quote `monitor.c` line numbers for the reduce algorithm.
- @ROADMAP.md — bump "Shipped (through vX)" heading and add Phase 16.38 row.
- @README.md — Stats feature row.
- Upstream reference: `flecs/src/addons/monitor.c`, `flecs/include/flecs/addons/monitor.h` — `EcsStatsHeader`, `ecs_metric_t`, `ecs_world_stats_t`, `ecs_pipeline_stats_t`, `ECS_STAT_WINDOW` (60), `flecs_stats_reduce`, `flecs_stats_reduce_last`, `flecs_stats_repeat_last`. Quote exact line numbers in the commit message and CHANGELOG.
- Label: `snichols/queued` (this is a feature, not a bug).
