# Observability — expvar Metrics Integration

Phase 16.56 (v0.111.0) publishes the Stats addon's counters as stdlib `expvar.Var` values, giving production deployments a `/debug/vars` metrics endpoint with **zero third-party dependencies**.

> **See also**: [Stats.md](Stats.md) for the underlying stats model and window-aggregation API.

## Why expvar

- `expvar` is stdlib — no dependency-graph growth.
- Auto-registers under `http.DefaultServeMux` at `/debug/vars` (the Go-standard introspection endpoint).
- Production Go services can scrape `/debug/vars` directly or bridge it to Prometheus via a user-controlled adapter.
- Consistent with the project's "stdlib + goid only" dependency posture.
- Users who want OTel or Prometheus can read the same `Stats` snapshot themselves; this integration does not preclude that.

## API

### `PublishExpvar(w *World, prefix string) *ExpvarHandle`

Registers expvar variables that lazily read live world stats on each `/debug/vars` scrape. `prefix` namespaces all variables (e.g., `"myapp"` produces `myapp`, `myapp.entity_count`, etc.). Returns a handle for `Unpublish`.

**Idempotent**: if `prefix` is already registered, returns the existing handle and logs a warning via `log/slog`. Does not panic on re-publish.

**No background goroutines**: all stats are read lazily inside `expvar.Func` closures on each scrape.

```go
h := flecs.PublishExpvar(w, "myapp")
defer h.Unpublish()

// Mount expvar.Handler on your server:
http.Handle("/debug/vars", expvar.Handler())
log.Fatal(http.ListenAndServe(":8080", nil))
```

### `(*ExpvarHandle).Unpublish()`

Nulls out the published variables: each closure returns `nil` (JSON `null`) on subsequent scrapes.

**Caveat — no deregister**: `expvar` has no public deregister API. The variable *names* remain in the global registry for the lifetime of the process; only the values go null. This is a stdlib limitation and cannot be worked around. Document it in your shutdown procedures.

### `ExpvarMap(w *World) *expvar.Map`

Returns an `*expvar.Map` populated with the same live Funcs, for callers who want to mount stats under a custom name without touching the global registry. The map is not published globally; callers may pass it to `expvar.Publish` under any name, or serve it directly.

```go
m := flecs.ExpvarMap(w)
expvar.Publish("myapp_stats", m)
```

## Published Variables

All variables are lazy `expvar.Func` — they re-read stats on each scrape with no background goroutine.

| Variable | Type | Description |
|---|---|---|
| `<prefix>` | JSON object | **Whole-tree var** — all fields below in one internally consistent snapshot (single `statsMu.RLock`). Use this for scrape-consistent reads. |
| `<prefix>.entity_count` | int | Total alive entities |
| `<prefix>.table_count` | int | Archetype table count |
| `<prefix>.component_count` | int | Registered components |
| `<prefix>.system_count` | int | Registered systems |
| `<prefix>.observer_count` | int | Active (non-unsubscribed) observers |
| `<prefix>.frame_count` | uint64 | Progress tick count |
| `<prefix>.reclaimed_tables` | uint64 | Cumulative reclaimed tables (Phase 16.46) |
| `<prefix>.last_progress_seconds` | float64\|null | Unix timestamp (seconds) of last Progress call; `null` before first Progress |
| `<prefix>.phases` | JSON object | Phase name → last-tick duration in seconds |
| `<prefix>.window_second` | JSON object | `WorldStatsAggregated` for the last ≤60 ticks |
| `<prefix>.window_minute` | JSON object | Minute-window aggregation |
| `<prefix>.window_hour` | JSON object | Hour-window aggregation |

## Wiring `/debug/vars`

`expvar.Handler()` is auto-registered at `/debug/vars` on `http.DefaultServeMux`. If you use `http.DefaultServeMux` (the default for `http.ListenAndServe`), no extra wiring is needed:

```go
flecs.PublishExpvar(w, "myapp")
log.Fatal(http.ListenAndServe(":8080", nil)) // expvar.Handler already on DefaultServeMux
```

For a custom mux:

```go
mux := http.NewServeMux()
mux.Handle("/debug/vars", expvar.Handler())
flecs.PublishExpvar(w, "myapp")
log.Fatal(http.ListenAndServe(":8080", mux))
```

## Prometheus Bridge

`expvar` does not include a Prometheus exposition format. Users who need Prometheus metrics can:

1. Scrape `/debug/vars` JSON from the Prometheus [`expvar_exporter`](https://github.com/prometheus-community/prom-label-proxy) or a custom bridge.
2. Read the `Stats` snapshot directly and publish via the [prometheus/client_golang](https://github.com/prometheus/client_golang) library.

Both approaches are user-controlled and introduce no new dependencies in the `flecs` module.

## Scrape-Consistency Model

### Whole-tree var (consistent)

The `<prefix>` var calls `w.expvarFullSnapshot()`, which takes **one `statsMu.RLock`** and reads all fields atomically. Use this var when you need all metrics from the same tick.

### Individual scalar vars (may skew)

Each scalar var (e.g., `<prefix>.entity_count`) takes its own `statsMu.RLock` independently. If a `Progress` call commits new stats between two scalar reads within the same `/debug/vars` scrape, the two scalars may reflect different ticks.

**This skew is intentional and documented** — individual vars are provided for Prometheus-style label cardinality and dashboarding convenience; the whole-tree var is provided for consistency.

## Multiple Worlds

Each world must use a unique prefix. Colliding prefixes return the first handle without registering new variables:

```go
h1 := flecs.PublishExpvar(world1, "game.physics")
h2 := flecs.PublishExpvar(world2, "game.ai")
```

The caller is responsible for prefix uniqueness. A collision logs a `log/slog` warning and returns the existing handle.

## Goroutine Safety

All published var closures are goroutine-safe:

- `entity_count`, `table_count`, `frame_count` read via `StatsSnapshot()` (`statsMu.RLock`).
- `component_count`, `system_count`, `observer_count`, `reclaimed_tables`, `last_progress_seconds` read directly from snapshot fields under `statsMu.RLock`.
- Window vars call `WorldStatsWindow(...)` which also holds `statsMu.RLock`.
- The whole-tree var takes a single `statsMu.RLock` for internal consistency.

`statsCommit` (called at the end of each `Progress` call) updates all snapshot fields under `statsMu.Lock`, so concurrent scrapes and Progress calls are race-free.

## Limitations

- **No deregister**: `expvar` names registered via `PublishExpvar` cannot be removed for the lifetime of the process. `Unpublish` nulls out values but leaves the names registered. Plan for this in long-lived or multi-tenant processes.
- **Scalar inter-variable skew**: individual scalar vars may reflect different ticks within one scrape. Use the whole-tree var for consistency.
- **No per-system or per-component vars**: only bounded world-level counters are published. Cardinality explosion (one var per entity/component) is a non-goal.
- **No histogram/percentile metrics**: only the gauges and counters already computed by the Stats addon are exposed.
