## Goal

Add two new HTTP routes to the existing read-only REST handler so external monitoring tools can poll the Phase 16.29 `StatsSnapshot()` over HTTP without embedding the Go API:

- `GET /stats/world` — returns the `WorldStats` portion of the current snapshot as JSON.
- `GET /stats/pipeline` — returns the full `PipelineStats` snapshot as JSON (world counters + `[]SystemStats` + `[]PhaseStats`).

The use case is dashboards and dev tools that render charts of frame time, entity count, and per-system execution time — they already speak HTTP+JSON but currently have no way to reach the goroutine-safe stats snapshot landed in v0.84.0.

This is a feature, not a bug. Target version: **v0.86.0** (next after v0.85.0 Units addon, shipped at `4d51a4b`).

### What lands

1. Two new route handlers on the mux returned by `NewRESTHandler`:
   - `GET /stats/world` → `200 OK`, body shape `{ "world": <WorldStats JSON> }`.
   - `GET /stats/pipeline` → `200 OK`, body shape `{ "world": ..., "systems": [...], "phases": [...] }`.
2. Snapshot taken on each request via `w.StatsSnapshot()` — fresh data per call.
3. Response headers: `Content-Type: application/json` and `Cache-Control: no-store`.
4. Error handling: panic-recovery returns `503 Service Unavailable` (for world teardown); otherwise `200`.
5. JSON field names: snake_case to match upstream REST conventions (locked in).
6. Tests in `rest_stats_test.go` with at least 6 cases, race-detector clean, coverage ≥ 95.0%.
7. Docs updated per `CONTRIBUTING.md`: `docs/FlecsRemoteApi.md`, `docs/README.md` line 90, `README.md`, `CHANGELOG.md`, `ROADMAP.md`.

### What does NOT land (explicit non-goals)

- No mutation endpoints (`PUT /entity`, `DELETE /entity`, `PUT /component`, `DELETE /component`, `PUT /toggle`).
- No query execution endpoint (`GET /query?expr=`).
- No reflection endpoint (`GET /type_info/<path>`).
- No multi-period aggregated stats — v1 Stats addon is per-tick + cumulative only; the `?period=` query parameter from upstream is out of scope.
- No authentication or CORS configuration — local development only.

### Open decisions (locked in)

1. **JSON field names**: snake_case (cross-compat with upstream).
2. **Cache-Control**: `no-store` (stats change every tick).
3. **World teardown**: return `503` rather than crashing the response.

### After this phase

`docs/README.md` line 90 — `**REST explorer** (full FlecsExplorer integration) — minimal read-only handler only.` — does NOT flip to fully shipped. Append a sub-entry noting that the stats endpoints subset shipped in v0.86.0; the remaining REST endpoints (mutation, query DSL, type_info, multi-period stats) are still outstanding.

## Constraints

- @rest.go — existing REST handler at lines 34–44 wires routes on a `http.ServeMux` via `mux.HandleFunc("GET /stats", ...)`. New routes must follow the same pattern: register `GET /stats/world` and `GET /stats/pipeline` on the existing mux. Reuse the `writeJSON` / `writeError` helpers at lines 93–101. Note: existing `restStats` at lines 103–109 returns the **legacy** `Stats` struct from `Reader.Stats()` (pre-16.29). Keep that route untouched — the new routes expose the separate `StatsSnapshot()` API.
- @stats_addon.go — `(*World).StatsSnapshot()` at line 63 returns `PipelineStats` (lines 42–52: `World WorldStats`, `Systems []SystemStats`, `Phases []PhaseStats`). `WorldStats` fields at lines 10–24: `EntityCount`, `TableCount`, `ArchetypeCount`, `FrameCount`, `TotalTime`, `LastTickDelta`. `SystemStats` fields at lines 27–40. `PhaseStats` lives in @stats.go (line 38). `StatsSnapshot` is documented as safe to call concurrently from any goroutine; uses `statsMu.RLock`, so HTTP handlers can call it directly without `w.Read(...)`.
- @stats.go — `type PhaseStats` at line 38 (defines `Name`, `SystemCount`, `Duration`, `CumulativeDuration`, `Invocations`). The two stats families (`Stats` at line 10 vs `PipelineStats` in stats_addon.go) must not be conflated: the new endpoints expose the snapshot type, not the legacy `Reader.Stats()`.
- @scope.go — World accessor pattern. The REST handler closes over `w *World` directly (rest.go line 34); no Reader/Writer scope needed for `StatsSnapshot()` calls because the method is itself goroutine-safe (stats_addon.go lines 56–60).
- @rest_test.go — existing test scaffolding pattern at lines 24–46 (`restSetup` builds a world wrapped in `httptest.NewServer`). New `rest_stats_test.go` should follow the same shape: spin up an `httptest.Server` against `NewRESTHandler(w)`, GET the new path, decode JSON, assert fields.
- @docs/FlecsRemoteApi.md — existing per-endpoint structure (heading, Path, Curl, Go client, Response shape). New `/stats/world` and `/stats/pipeline` sections must follow that structure. The table at line 83–95 lists "Available endpoints"; add two rows.
- @docs/README.md — gap entry at line 90 (`**REST explorer** (full FlecsExplorer integration) — minimal read-only handler only.`). Annotate with the v0.86.0 partial closure (stats endpoints shipped; mutation/query/type_info still outstanding).
- @README.md — REST API row at line 270 lists current endpoints; extend with `/stats/world` and `/stats/pipeline`. Feature-list row at line 294 already shows REST addon ✅ — no change there; the new endpoints append to the existing entry's description.
- @CHANGELOG.md — top of file (line 3) has the v0.85.0 entry; new v0.86.0 entry above it.
- @ROADMAP.md — heading at line 3 (`## Shipped (through v0.85.0)`) bumps to v0.86.0. Append a new `Shipped` entry following the existing one-line format (see v0.85.0 Units entry at line 88).
- @CONTRIBUTING.md — at line 6 the contract requires `go test ./... -race && golangci-lint run` clean; coverage stays ≥ 90% (target ≥ 95.0% per spec). The doc-update matrix at lines 66–72 lists what must move in the same PR (godoc, `docs/` page, `README.md` if headline-visible, `CHANGELOG.md`, `ROADMAP.md`).
- Upstream reference — `src/addons/rest.c` lines 1140–1177 (`flecs_rest_get_stats`): routes split on `category` (`world` vs `pipeline`) at the suffix of `/stats/<category>`. Lines 916–989 (`flecs_world_stats_to_json`) show the upstream snake_case JSON shape (e.g. `entities.count`, `performance.frame_time`). Go-side does not need to mirror the gauge/counter wrapping (`stats.h` `ECS_GAUGE_APPEND` wraps each metric in a per-period sample array) because v1 stats are single-snapshot; field names in lower_snake_case are the cross-compat target.

## Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0% on touched files
