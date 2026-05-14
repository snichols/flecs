## Goal

Port the upstream **FlecsMonitor / FlecsStats addon** (`include/flecs/addons/stats.h`, `src/addons/stats/`) to Go as **v0.84.0**. The addon collects per-tick performance counters, per-system execution times, per-pipeline-phase timings, and entity/table counts, and exposes them via a structured snapshot API for instrumentation, profiling, and UI display.

This is the gap entry at `@docs/README.md` line 77 (*"Monitor addon — not ported."*).

### Naming-collision rename (lock-in)

Phase 16.10 already ships `monitor_observer.go` (entity-enter/leave-matching-a-query observers). To avoid the collision, the Go-side surface for this addon uses the name **"Stats"** rather than "Monitor":

- Top-level API: `(*World).StatsSnapshot() PipelineStats`.
- New file: `stats_addon.go` (extends `@stats.go`).
- Tests: `stats_addon_test.go`.
- Doc: `docs/Stats.md`.

`@docs/README.md` line 77 flips to ✅ shipped (v0.84.0) and notes the Go-side rename: *"Monitor (Stats) addon — ✅ shipped in v0.84.0 as Stats addon (Go-side rename to avoid collision with Phase 16.10 monitor observers in `@monitor_observer.go`)."*

### Decisions locked in

1. **Naming** — `StatsSnapshot` (NOT `MonitorSnapshot`).
2. **Timing source** — `time.Now()` (sufficient resolution; matches existing per-phase timing already in `@system.go` `runPhase`).
3. **Lock granularity** — `sync.RWMutex`: `StatsSnapshot` acquires read lock; tick-end stats update acquires write lock.
4. **Snapshot allocation** — return value (`PipelineStats`), not pointer to live state. Safe to retain across subsequent ticks.

### v1 scope (per-tick + cumulative only)

The snapshot exposes current-tick deltas plus cumulative-since-world-creation totals. Time-window aggregation (`EcsPeriod1s` / `1m` / `1h` / `1d` / `1w` from upstream `monitor.c` lines 13-17, 386-404) is **deferred** — v1 has no ring buffers or windowed reduce/aggregate pipelines.

### Deliverables

1. **`stats_addon.go`** (new file; may extend `@stats.go`):
   - `WorldStats` struct — entity count, table count, archetype count, frame count, total time, last-tick delta.
   - `SystemStats` struct — name, last-tick duration, total invocations, average duration, total skipped (disabled / rate / interval).
   - `PhaseStats` struct — extend or shadow existing `PhaseStats` in `@stats.go` to include cumulative time and invocation count.
   - `PipelineStats` struct — `World WorldStats`, `Systems []SystemStats`, `Phases []PhaseStats`.
   - `(*World).StatsSnapshot() PipelineStats`.

2. **Collection hook** at end-of-tick (after merge hooks, after pipeline run):
   - Per-system timing: instrument the system invocation in `@system.go` `runPhase` (lines 391-394, 419-421, 451-453) to record start/end nanoseconds; add to system's accumulators.
   - Per-phase timing: instrument the phase loop. Existing `w.lastFramePhases` already tracks per-phase duration for the most recent tick (see `@stats.go` lines 28-31, 41-48) — extend with cumulative totals and invocation count.
   - Use `RWMutex` write lock for the update; read lock for `StatsSnapshot`.

3. **Concurrency** — stats reads happen from any goroutine; writes happen at tick end. `sync.RWMutex` protects the stats record. `StatsSnapshot` returns a fully-copied value (no aliased slices).

4. **Tests** in `stats_addon_test.go` (≥ 8 cases, coverage ≥ 95.0%):
   - WorldStats: entity count goes up after creating entities.
   - WorldStats: frame count increments each `Progress`.
   - SystemStats: register two systems; after a `Progress`, both have non-zero `Invocations`.
   - SystemStats: disabled system has `Invocations` of 0.
   - SystemStats: rate-filtered system fires less often; `Invocations` reflects the gate.
   - PhaseStats: each phase has the correct `SystemCount`.
   - Concurrent snapshot during a `Progress`: race-clean under `-race`.
   - StatsSnapshot is a snapshot — caller's retained value does not reflect subsequent writes.
   - Empty world: zero counts, no panic.

5. **Mechanical acceptance**:
   - `go vet ./...` clean
   - `golangci-lint run` clean
   - `go test ./... -race -count=3` passes
   - Coverage ≥ 95.0%

### Doc updates per `@CONTRIBUTING.md`

- New `docs/Stats.md` — addon documentation, API reference, examples, scope note (no time-window aggregation in v1).
- `@docs/README.md` line 77 — flip to ✅ shipped (v0.84.0) with Go-side rename note.
- `@README.md` — feature-list bump.
- `@CHANGELOG.md` — v0.84.0 entry at the top.
- `@ROADMAP.md` — heading bump to "through v0.84.0".

### Explicit non-goals (v1)

- No time-window aggregation (`Period1s`/`1m`/`1h`/`1d`/`1w`). The upstream `MonitorStats` / `ReduceStats` / `AggregateStats` machinery in `monitor.c` is deferred — v1 is per-tick + cumulative only.
- No persistence — stats reset on world recreation.
- No alerts-on-threshold integration (Alerts addon is Phase 16.28; users can wire `StatsSnapshot` → `RegisterAlert` manually).
- No REST endpoint exposure — deferred until the REST/Explorer port lands (Phase 16.30+).
- No memory-allocator stats (`EcsWorldMemory`, `ecs_allocator_memory_t` from upstream `stats.h` lines 595-622). Go's runtime owns allocations; surfacing them would require `runtime.MemStats` integration, which is out of scope here.
- No HTTP stats (upstream `stats.h` lines 152-163) — no HTTP server in scope.

## Constraints

- @docs/README.md — line 77 gap entry; flip to ✅ shipped (v0.84.0) with the Go-side `Stats` rename noted (preserve the "Monitor (Stats)" framing so the upstream addon name remains findable).
- @stats.go — existing `Stats` / `PhaseStats` / `ComponentStat` types and `(*World).Stats()`. The new `StatsSnapshot` lives alongside these; reuse `PhaseStats` if it can be extended without breaking callers, otherwise introduce a distinct namespace (`stats_addon.go` types).
- @system.go — `Progress` (line 349) increments `w.frameCount` and `w.time`; `runPhase` (line 364) is where per-system invocation happens. The new per-system timing instrumentation hooks into the three call sites (serial line 393, multi-threaded worker line 420, parallel batch line 452) and the early-continue paths (disabled line 368, interval-gated lines 371-380, rate-gated lines 381-386) which must increment `Skipped` counters.
- @pipeline_phases.go — `Phase.orderedSystems` (line 24) is the per-phase system list used for `SystemCount`. Built-in phases (PreUpdate, OnFixedUpdate, OnUpdate, PostUpdate) plus custom user phases must all surface in `PipelineStats.Phases`.
- @world.go — `frameCount` (line 142), `lastFramePhases` (line 145), `Time()` (line 697), `FrameCount()` (line 700) are the existing stats foundation from Phase 16.6 rate filters. The new addon extends rather than duplicates these.
- @monitor_observer.go — naming collision source. Confirm the existing `Monitor*` symbols stay unchanged; the new addon must NOT introduce a top-level `Monitor` identifier.
- @CONTRIBUTING.md — doc-update checklist (README, CHANGELOG, ROADMAP, docs/<Feature>.md).
- @CHANGELOG.md — current header is `v0.83.0 — 2026-05-14 — Phase 16.28: Alerts addon`; new v0.84.0 entry goes at the top.
- @ROADMAP.md — current header is `Shipped (through v0.83.0)`; bump to `(through v0.84.0)`.
- Upstream C reference — `/work/agents/claude/projects/SanderMertens/flecs/include/flecs/addons/stats.h`:
  - `ecs_world_stats_t` (lines 63-169) — entity/table/query/system/frame/timing counters.
  - `ecs_system_stats_t` (lines 184-192) — `time_spent` metric + query stats.
  - `ecs_pipeline_stats_t` (lines 207-223) — vector of system IDs plus sync-point stats.
  - `EcsWorldSummary` (lines 466-507) — `entity_count`, `table_count`, `frame_count`, `systems_ran_total`, `merge_count`, etc.; **this is the closest analog to v1 scope** (per-tick + cumulative, no ring buffer).
- Upstream C reference — `/work/agents/claude/projects/SanderMertens/flecs/src/addons/stats/`:
  - `world_monitor.c` (full file, 117 lines) — world-stats ctor/copy/move/dtor and `flecs_stats_api_t` registration.
  - `system_monitor.c` (full file, 122 lines) — per-system stats via `flecs_stats_api_t` with `query_component_id = EcsSystem`.
  - `pipeline_monitor.c` (full file, 132 lines) — per-pipeline stats with `query_component_id = ecs_id(EcsPipeline)`.
  - `monitor.c` lines 13-17, 386-404 — `EcsPeriod1s/1m/1h/1d/1w` time-window tags **explicitly out of scope for v1**.
  - `monitor.c` lines 271-378 — `MonitorStats` / `ReduceStats` / `AggregateStats` ring-buffer machinery, **out of scope for v1**.
  - `world_summary.c` (174 lines) — closest structural analog to v1 scope; per-tick summary without windowed aggregation.
- Coverage target ≥ 95.0% per repo convention.
- All four mechanical checks (`go vet`, `golangci-lint`, `go test -race -count=3`, coverage) must pass.
