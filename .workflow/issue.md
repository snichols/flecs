## Goal

**Phase 16.56 — expvar metrics integration** (target **v0.111.0**). The fifth post-port-completion Go-idiomatic value-add.

Publish the Stats addon's counters as stdlib `expvar.Var` values so production deployments get `/debug/vars` metrics with **zero third-party dependencies**. This matches the project's deliberate dependency discipline (stdlib `log/slog`, stdlib `net/http`, single third-party dep `github.com/petermattis/goid` — see `@/work/agents/claude/projects/flecs/go.mod`).

### Why expvar (not OpenTelemetry / Prometheus)

- `expvar` is stdlib — no dependency-graph growth.
- Auto-registers under `http.DefaultServeMux` at `/debug/vars` (the Go-standard introspection endpoint).
- Production Go services already scrape `/debug/vars` or bridge it to Prometheus via a user-controlled adapter.
- Consistent with the project's "stdlib + goid only" posture.
- Users who want OTel/Prometheus can read the same `Stats` snapshot themselves; this phase does not preclude that.

### API surface

```go
// PublishExpvar registers a set of expvar variables that lazily read the
// world's live stats on each /debug/vars scrape. prefix namespaces the
// variables (e.g. "flecs" -> flecs.entity_count, flecs.table_count, ...).
// Returns a handle for Unpublish.
func PublishExpvar(w *World, prefix string) *ExpvarHandle

// Unpublish removes the previously published variables (see deregister caveat).
func (h *ExpvarHandle) Unpublish()

// ExpvarMap returns an *expvar.Map populated with the same data, for callers
// who want to mount it under a custom name rather than the global registry.
func ExpvarMap(w *World) *expvar.Map
```

#### Published variables (under `<prefix>.`)

All lazy `expvar.Func` — re-read stats on each scrape, **no background goroutine**:

- `<prefix>.entity_count` — total alive entities
- `<prefix>.table_count` — archetype table count
- `<prefix>.component_count` — registered components
- `<prefix>.system_count` — registered systems
- `<prefix>.observer_count` — registered observers
- `<prefix>.frame_count` — Progress tick count
- `<prefix>.reclaimed_tables` — Phase 16.46 reclamation counter
- `<prefix>.last_progress_seconds` — wall-clock of last Progress
- `<prefix>.phases` — `expvar.Func` returning JSON object: phase name -> last-tick seconds
- `<prefix>.window_second` — JSON object: aggregated second-window gauges (`WorldStatsWindow(StatsSecond)`, Phase 16.38)
- `<prefix>.window_minute` — minute window
- `<prefix>.window_hour` — hour window

#### Stats-source reconciliation (design decision required of the iterate agent)

The spec's variable list does **not** map 1:1 to a single accessor. Verified current state:

- `(*World).StatsSnapshot() PipelineStats` (`@/work/agents/claude/projects/flecs/stats_addon.go:412-449`) is the **goroutine-safe** path (acquires `w.statsMu.RLock()`, returns a fully-copied snapshot with no aliased slices). The Phase 16.31 REST stats endpoint calls it directly without an outer `w.Read` scope and documents this explicitly (`@/work/agents/claude/projects/flecs/rest.go:25-31`). **Reuse this path.** Citation confirms the safety requirement in the brief.
- `PipelineStats.World` (`WorldStats`) exposes only `EntityCount, TableCount, ArchetypeCount, FrameCount, TotalTime, LastTickDelta` — it has **no** `ComponentCount`, `SystemCount`, `ObserverCount`, `ReclaimedTables`, or last-Progress wall-clock.
- `component_count` / `system_count` come from `(*World).Stats() Stats` (`@/work/agents/claude/projects/flecs/stats.go:80`) which carries `ComponentCount` and `SystemCount`. `(*World).SystemCount()` (`@/work/agents/claude/projects/flecs/system.go:710`) also exists.
- `reclaimed_tables` comes from `(*World).ReclaimedTablesCount() uint64` (`@/work/agents/claude/projects/flecs/world.go:2133`).
- `entity_count` is `(*World).Count()` (`@/work/agents/claude/projects/flecs/world.go:1374`) — note `StatsSnapshot().World.EntityCount` also reflects this as of last completed tick; prefer the accessor that is consistent with the rest of the snapshot.
- There is **no** `(*World).ObserverCount()` accessor today. The iterate agent must either add a goroutine-safe `ObserverCount()` accessor (preferred — small, mirrors `SystemCount()`) or source it from an existing internal counter under the appropriate lock. Whichever path it chooses, the accessor used for every published variable MUST be goroutine-safe (the concurrent-scrape test requires it).
- `last_progress_seconds`: if no existing wall-clock timestamp of the last Progress is captured, the iterate agent should add a minimal goroutine-safe timestamp captured at Progress completion (guarded by the existing stats mutex) rather than inventing a value.

**Constraint:** every variable's read path must be goroutine-safe (RLock-guarded snapshot/accessors). Do not read raw `World` fields directly from the `expvar.Func` closures.

#### Scrape-consistency model

A single `/debug/vars` scrape reads many vars; they should reflect a consistent snapshot. Recommended (and to be implemented unless the iterate agent has a strong reason otherwise):

- One `expvar.Func` published as `<prefix>` returning the **whole tree** as a single JSON object — atomic and internally consistent (one `StatsSnapshot()` + supporting accessors per scrape of that var).
- PLUS individual scalar vars for the common counters for convenience — these may exhibit minor inter-variable skew across a scrape (each scalar re-reads independently). **This skew must be documented honestly** in `docs/Observability.md`.

The `TestPublishExpvar_ScrapeConsistency` test asserts the whole-tree JSON var is internally consistent.

#### Registration semantics

- `PublishExpvar` calls `expvar.Publish(name, ...)` per var. `expvar.Publish` **panics on duplicate name**. Required behaviour: **idempotent** — if already published under this prefix, return the existing handle and log a warning via the project's `log/slog` logger. Do not panic on re-publish.
- `Unpublish`: `expvar` has **no public deregister API**. Workaround: published `expvar.Func` closures read through an atomic pointer; `Unpublish` swaps the pointer to a nil-returning stub so the var emits `null`. The variable **name stays registered for process lifetime**; only the value goes null. This limitation is real and **must be documented honestly** in `docs/Observability.md` — not hidden.
- Multiple worlds: each gets its own prefix. The caller is responsible for unique prefixes across worlds; document this.

### Required tests (new file `@/work/agents/claude/projects/flecs/expvar_test.go`)

- `TestPublishExpvar_VariablesAppear`
- `TestPublishExpvar_ReflectsLiveState` — create 10 entities -> scrape -> ==10; delete 5 -> scrape -> ==5
- `TestPublishExpvar_PhasesJSON` — valid JSON, expected keys
- `TestPublishExpvar_WindowAggregates` — drive Progress 60x; window_second reflects aggregation
- `TestPublishExpvar_IdempotentDoublePublish` — same prefix twice; no panic; documented idempotent behaviour
- `TestPublishExpvar_Unpublish_EmitsNull` — publish, unpublish, scrape -> `null`; assert name-stays-registered caveat
- `TestExpvarMap_StandaloneMap` — populated map without touching the global registry
- `TestPublishExpvar_TwoWorlds_DistinctPrefixes` — no collision
- `TestPublishExpvar_ScrapeConsistency` — whole-tree JSON var internally consistent
- `TestPublishExpvar_HTTPEndpoint` — mount `expvar.Handler()` on `httptest`; GET `/debug/vars`; flecs vars present and valid JSON
- `TestPublishExpvar_ConcurrentScrapeAndMutation` — race test: repeated scrape while another goroutine mutates the world (under `World.Write` scope); no data races, valid JSON every scrape
- `TestPublishExpvar_NoBackgroundGoroutine` — verify no goroutine spawned (use `go.uber.org/goleak`, already an indirect dep in `@/work/agents/claude/projects/flecs/go.mod`, or before/after goroutine count)
- `TestPublishExpvar_ZeroStatsBeforeProgress` — fresh world, no Progress; vars valid (zeros, not panics)

**Test isolation (mandatory):** `expvar` uses a process-global registry and names **cannot be unregistered**. Every test MUST use a unique prefix that includes the test name (e.g. `"flecs_TestPublishExpvar_VariablesAppear"`) to avoid cross-test pollution. The iterate agent must design all tests with per-test prefixes from the start.

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run ./...` clean
- `go test ./... -race -count=3` clean
- Coverage >= 95% (current baseline 95.0%)
- All existing tests pass unchanged
- `go list -deps` shows **no new third-party dependency** (stdlib `expvar` only; `goleak` already present for tests)
- No background goroutine spawned by `PublishExpvar` (lazy `Func` evaluation only)

### Documentation update matrix

- **New** `@/work/agents/claude/projects/flecs/docs/Observability.md` — expvar reference: published-vars table, `/debug/vars` wiring, Prometheus-bridge note (point at user-side adapters), Unpublish/no-deregister limitation, multi-world prefixes, scrape-consistency model + documented scalar skew
- `@/work/agents/claude/projects/flecs/docs/Stats.md` — cross-link to Observability.md
- **New** runnable example `@/work/agents/claude/projects/flecs/expvar_example_test.go` — publish + mount `expvar.Handler()` on a server
- `@/work/agents/claude/projects/flecs/CHANGELOG.md` — v0.111.0 entry
- `@/work/agents/claude/projects/flecs/ROADMAP.md` — add Phase 16.56 to Shipped
- `@/work/agents/claude/projects/flecs/README.md` — observability feature row (follow the existing addon-row format around line 275-282)
- `@/work/agents/claude/projects/flecs/doc.go` — mention expvar integration

### Non-goals

- OpenTelemetry / Prometheus client libraries (third-party deps — out of scope by design)
- Background metric push (statsd, etc.) — pull-only via expvar
- Histogram/percentile metrics beyond what the Stats addon already computes
- Custom user-metric registration through expvar — only built-in world/pipeline stats
- A real deregister for expvar (stdlib does not support it) — null-emission workaround only
- Per-system / per-component cardinality explosion — keep the published var set small and bounded (no per-entity / per-component vars)

## Constraints

- @/work/agents/claude/projects/flecs/stats_addon.go — `StatsSnapshot()` (lines 412-449) is the goroutine-safe snapshot path (`w.statsMu.RLock()`, fully-copied result). Reuse it; mirror `WorldStatsWindow(StatsSecond|StatsMinute|StatsHour)` (Phase 16.38) for window vars.
- @/work/agents/claude/projects/flecs/rest.go — lines 25-31 document that `StatsSnapshot()` is safe to call concurrently without an outer `w.Read` scope (Phase 16.31 stats endpoint). Cite this as the precedent for concurrent scrape safety.
- @/work/agents/claude/projects/flecs/stats.go — `(*World).Stats() Stats` (line 80) provides `ComponentCount` / `SystemCount`; `PipelineStats.World` does not.
- @/work/agents/claude/projects/flecs/world.go — `(*World).ReclaimedTablesCount()` (line 2133, Phase 16.46), `(*World).Count()` (line 1374). No `ObserverCount()` exists — add a goroutine-safe one mirroring `SystemCount()` if needed.
- @/work/agents/claude/projects/flecs/system.go — `(*World).SystemCount()` (line 710) pattern to mirror for any new goroutine-safe count accessor.
- @/work/agents/claude/projects/flecs/go.mod — stdlib + `github.com/petermattis/goid` only; `go.uber.org/goleak` already an indirect test dep. No new third-party dependency permitted.
- @/work/agents/claude/projects/flecs/docs/Stats.md — existing stats manual; new Observability.md must cross-link and stay consistent with its terminology.
- @/work/agents/claude/projects/flecs/README.md — observability row must follow the existing addon-row format (see Stats-addon / Testing-helpers rows ~line 275-282).
- @/work/agents/claude/projects/flecs/ROADMAP.md — "All ROADMAP items shipped" post-port-completion section; add Phase 16.56 (v0.111.0) consistent with the Phase 16.51-16.55 value-add entries.
- The expvar no-deregister limitation and the scalar-skew tradeoff are real and MUST be documented honestly in docs/Observability.md, not hidden.
- Tests MUST use per-test unique prefixes (process-global expvar registry, no unregister).
- Every published-var read path MUST be goroutine-safe (RLock-guarded snapshot/accessors); the concurrent-scrape race test depends on it.

