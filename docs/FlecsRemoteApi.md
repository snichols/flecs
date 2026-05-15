# REST API

The Go flecs REST addon exposes world inspection and snapshot save/load over stdlib
`net/http`. It is the starting point for debugging running worlds, building tooling, and
implementing save/load workflows.

See also:
- [Quickstart](Quickstart.md) — hands-on walkthrough; the REST handler is introduced there first
- [Systems](Systems.md) — `World.Stats()` is the data source for `GET /stats`
- [ComponentTraits](ComponentTraits.md) — `GET /components` returns trait information alongside each component
- [Manual](Manual.md) — top-level reference hub with world lifecycle, concurrency model, and concept map

---

## Quickstart

`NewRESTHandler` returns a stdlib `http.Handler`. The simplest usage is:

```go
package main

import (
    "log"
    "net/http"

    "github.com/snichols/flecs"
)

func main() {
    w := flecs.New()
    // ... register components, populate world ...

    log.Fatal(http.ListenAndServe(":8080", flecs.NewRESTHandler(w)))
}
```

Navigate to `http://localhost:8080/stats` to verify the handler is live.

To mount the handler under a path prefix, use `http.StripPrefix`:

```go
package main

import (
    "log"
    "net/http"

    "github.com/snichols/flecs"
)

func main() {
    w := flecs.New()

    mux := http.NewServeMux()
    mux.Handle("/flecs/", http.StripPrefix("/flecs", flecs.NewRESTHandler(w)))

    srv := &http.Server{Addr: ":8080", Handler: mux}
    log.Fatal(srv.ListenAndServe())
}
```

Navigate to `http://localhost:8080/flecs/stats`.

---

## Goroutine safety

`*World` is not goroutine-safe on its own. `NewRESTHandler` wraps every call in the
appropriate world scope:

- **GET endpoints** (except the stats-snapshot endpoints below) call
  `w.Read(func(*Reader) { ... })`, which acquires a read lock. Multiple concurrent GET
  requests are safe as long as no write is in progress.
- **GET /stats/world and GET /stats/pipeline** call `w.StatsSnapshot()` directly.
  `StatsSnapshot` acquires only a lightweight stats read-lock (`statsMu.RLock`) and is
  goroutine-safe from any goroutine — no outer `w.Read` scope is needed or taken.
- **PUT /snapshot** calls `w.Write(func(*Writer) { ... })`, which acquires an exclusive
  write lock. This must not run concurrently with any other world operation.

If you run a simulation loop alongside the HTTP server, coordinate with `world.Read` /
`world.Write` in your loop as well. The REST handler participates in the same exclusive-
access ownership assertion (`ExclusiveAccessBegin` / `ExclusiveAccessEnd`) as the rest of
the world API.

---

## Available endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/stats` | World stats snapshot (legacy `Stats` struct) |
| GET | `/stats/world` | `WorldStats` snapshot; `Cache-Control: no-store` |
| GET | `/stats/pipeline` | `PipelineStats` snapshot (world + systems + phases); `Cache-Control: no-store` |
| GET | `/components` | All registered component infos |
| GET | `/components/{id}` | Single component info by numeric ID |
| GET | `/entities` | Alive entities (optional `?limit=N`) |
| GET | `/entities/{id}` | Entity detail |
| GET | `/snapshot` | Full world snapshot (JSON) |
| PUT | `/snapshot` | Load a world snapshot |
| GET | `/type_info/{path}` | Reflection schema for a named component; `Cache-Control: max-age=300` |
| GET | `/query` | Query DSL: evaluate FQL v2 expression; returns matched entities + fields |
| PUT | `/entity` | Create or claim an entity (JSON body); returns `{ id, name }` |
| DELETE | `/entity/{path...}` | Delete an entity by dot-separated path |

All other C flecs REST endpoints are **not yet ported** — see the [unimplemented endpoints](#unimplemented-c-flecs-rest-endpoints) table below.

---

## GET /stats

Returns a JSON snapshot of world-level counters and per-phase timing from the last
`Progress` call. Equivalent to calling `world.Stats()` directly.

```
GET /stats
→ 200 OK  application/json
```

### Curl

```
curl http://localhost:8080/stats
```

### Go client

```go
resp, err := http.Get("http://localhost:8080/stats")
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()

var s flecs.Stats
json.NewDecoder(resp.Body).Decode(&s)
fmt.Println(s.EntityCount, s.FrameCount)
```

### Response shape

```json
{
  "EntityCount": 4,
  "TableCount": 2,
  "QueryCount": 0,
  "CachedQueryCount": 1,
  "SystemCount": 2,
  "FrameCount": 100,
  "Time": 1.666,
  "LastFramePhases": [
    {"Name": "PreUpdate",     "SystemCount": 0, "Duration": 0},
    {"Name": "OnFixedUpdate", "SystemCount": 0, "Duration": 0},
    {"Name": "OnUpdate",      "SystemCount": 2, "Duration": 16666666},
    {"Name": "PostUpdate",    "SystemCount": 0, "Duration": 0}
  ],
  "ComponentStats": [
    {
      "ID": 20,
      "Name": "main.Position",
      "Size": 8,
      "TableCount": 1,
      "EntityCount": 3
    }
  ]
}
```

The JSON field names are the exported Go field names (no struct tags on `flecs.Stats`).
`Duration` is a `time.Duration` — encoded as an `int64` in nanoseconds.
`LastFramePhases` is `null` if `Progress` has never been called.

---

## GET /stats/world

Returns the `WorldStats` portion of the current `StatsSnapshot()` as JSON. Calls
`world.StatsSnapshot()` directly — no outer `Read` scope needed; the method is
goroutine-safe. Returns `503 Service Unavailable` if the world panics (e.g. during
teardown).

### `?period=` query parameter

```
GET /stats/world?period=<period>
```

| `period` value | Response shape | Description |
|---|---|---|
| (absent) or `instant` | `{"world": {...}}` instant gauge | Back-compat default; byte-identical to pre-v0.93.0 |
| `second` | `WorldStatsAggregated` | Reduced across last ≤60 ticks |
| `minute` | `WorldStatsAggregated` | Reduced across last ≤60 second-reductions |
| `hour` | `WorldStatsAggregated` | Reduced across last ≤60 minute-reductions |
| (unknown) | `400 Bad Request` | |

When `period` is `second`, `minute`, or `hour`, each metric is returned as an object with `avg`, `min`, and `max` sub-fields:

```json
{
  "entity_count": {"avg": 42.3, "min": 40.0, "max": 45.0},
  "table_count":  {"avg": 7.0,  "min": 7.0,  "max": 7.0},
  "archetype_count": {"avg": 7.0, "min": 7.0, "max": 7.0},
  "frame_count":  {"avg": 50.5, "min": 1.0,  "max": 100.0},
  "total_time":   {"avg": 0.83, "min": 0.016, "max": 1.666},
  "last_tick_delta": {"avg": 0.016, "min": 0.016, "max": 0.016}
}
```

**Time-to-window note**: "1 tick ≈ 1 second" only when the pipeline runs at 1 Hz.
Aggregation is tick-based, not wall-clock-based.

```
GET /stats/world
GET /stats/world?period=instant    (same as no parameter)
GET /stats/world?period=second     → WorldStatsAggregated
GET /stats/world?period=minute     → WorldStatsAggregated
GET /stats/world?period=hour       → WorldStatsAggregated
→ 200 OK  application/json  Cache-Control: no-store
→ 400 Bad Request                  (unknown period value)
→ 503 Service Unavailable          (world panic / teardown)
```

### Curl

```
curl http://localhost:8080/stats/world
```

### Go client

```go
resp, err := http.Get("http://localhost:8080/stats/world")
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()

var result struct {
    World struct {
        EntityCount   int     `json:"entity_count"`
        FrameCount    uint64  `json:"frame_count"`
        LastTickDelta float64 `json:"last_tick_delta"`
    } `json:"world"`
}
json.NewDecoder(resp.Body).Decode(&result)
fmt.Println(result.World.FrameCount, result.World.LastTickDelta)
```

### Response shape

```json
{
  "world": {
    "entity_count": 42,
    "table_count": 7,
    "archetype_count": 7,
    "frame_count": 100,
    "total_time": 1.666,
    "last_tick_delta": 0.016
  }
}
```

Fields (all snake_case):
- `entity_count` — alive entities at the end of the last tick.
- `table_count` — number of archetype tables.
- `archetype_count` — mirrors `table_count`; each table is one archetype.
- `frame_count` — number of `Progress` calls since world creation.
- `total_time` — total accumulated simulation time in seconds.
- `last_tick_delta` — `dt` passed to the most recent `Progress` call; `0` before first call.

---

## GET /stats/pipeline

Returns the full `PipelineStats` snapshot as JSON: world counters, per-system
performance metrics, and per-phase timing. Calls `world.StatsSnapshot()` directly
(goroutine-safe). Returns `503 Service Unavailable` if the world panics.

Accepts the same `?period=` query parameter as `GET /stats/world`. When a non-instant
period is specified, the response is a `PipelineStatsAggregated` object with the same
`world` sub-object as `WorldStatsAggregated` plus `phases` and `systems` arrays where
each metric is `{avg, min, max}`.

```
GET /stats/pipeline
GET /stats/pipeline?period=instant  (same as no parameter)
GET /stats/pipeline?period=second   → PipelineStatsAggregated
GET /stats/pipeline?period=minute   → PipelineStatsAggregated
GET /stats/pipeline?period=hour     → PipelineStatsAggregated
→ 200 OK  application/json  Cache-Control: no-store
→ 400 Bad Request                   (unknown period value)
→ 503 Service Unavailable           (world panic / teardown)
```

### Curl

```
curl http://localhost:8080/stats/pipeline
```

### Go client

```go
resp, err := http.Get("http://localhost:8080/stats/pipeline")
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()

var result struct {
    World   map[string]any   `json:"world"`
    Systems []map[string]any `json:"systems"`
    Phases  []map[string]any `json:"phases"`
}
json.NewDecoder(resp.Body).Decode(&result)
fmt.Println("systems:", len(result.Systems), "phases:", len(result.Phases))
```

### Response shape

```json
{
  "world": {
    "entity_count": 42,
    "table_count": 7,
    "archetype_count": 7,
    "frame_count": 100,
    "total_time": 1.666,
    "last_tick_delta": 0.016
  },
  "systems": [
    {
      "name": "movement",
      "last_tick_duration": 250000,
      "invocations": 100,
      "avg_duration": 240000,
      "total_skipped": 0
    }
  ],
  "phases": [
    {
      "name": "PreUpdate",
      "system_count": 0,
      "duration": 0,
      "cumulative_duration": 0,
      "invocations": 100
    },
    {
      "name": "OnUpdate",
      "system_count": 1,
      "duration": 250000,
      "cumulative_duration": 24000000,
      "invocations": 100
    }
  ]
}
```

Fields:
- `world` — same shape as `GET /stats/world`.
- `systems` — per-system entries in pipeline order; `null` before first `Progress` call.
  - `name` — display name set via `(*System).SetName`; auto-generated otherwise.
  - `last_tick_duration` — wall-clock nanoseconds in the most recent tick; `0` if skipped.
  - `invocations` — total number of times this system ran since world creation.
  - `avg_duration` — `invocations > 0 ? total / invocations : 0`.
  - `total_skipped` — ticks skipped by interval or rate gating.
- `phases` — per-phase entries in topological order; `null` before first `Progress` call.
  - `name` — phase display name (e.g. `"PreUpdate"`, `"OnUpdate"`).
  - `system_count` — active systems in this phase during the last tick.
  - `duration` — phase wall-clock nanoseconds in the last tick.
  - `cumulative_duration` — total across all ticks since world creation.
  - `invocations` — number of `Progress` calls in which this phase ran.

Duration values are `time.Duration` encoded as `int64` nanoseconds.

---

## GET /components

Returns all registered component infos as a JSON array.

```
GET /components
→ 200 OK  application/json
```

### Curl

```
curl http://localhost:8080/components
```

### Go client

```go
resp, _ := http.Get("http://localhost:8080/components")
defer resp.Body.Close()

var comps []map[string]any
json.NewDecoder(resp.Body).Decode(&comps)
for _, c := range comps {
    fmt.Println(c["name"], c["size"])
}
```

### Response shape

```json
[
  {
    "id": "20",
    "name": "main.Position",
    "size": 8,
    "align": 4,
    "type": "main.Position"
  },
  {
    "id": "21",
    "name": "main.Velocity",
    "size": 8,
    "align": 4,
    "type": "main.Velocity"
  }
]
```

Fields:
- `id` — numeric entity ID encoded as a decimal string.
- `name` — fully-qualified Go type name (`package/path.TypeName`).
- `size` — `unsafe.Sizeof` of the component type in bytes. `0` for tag types.
- `align` — `unsafe.Alignof` of the component type in bytes.
- `type` — Go `reflect.Type.String()` result; equals `name` for named types.

The response includes all registered components, including trait-related component
entities. See [ComponentTraits](ComponentTraits.md) for details on trait metadata.

---

## GET /components/{id}

Returns a single component info by its numeric entity ID.

```
GET /components/{id}
→ 200 OK  application/json   (component found)
→ 400 Bad Request            (id is not a valid decimal integer)
→ 404 Not Found              (no component registered with that ID)
```

### Curl

```
curl http://localhost:8080/components/20
```

### Go client

```go
resp, _ := http.Get("http://localhost:8080/components/20")
defer resp.Body.Close()
if resp.StatusCode == http.StatusNotFound {
    fmt.Println("not found")
    return
}
var comp map[string]any
json.NewDecoder(resp.Body).Decode(&comp)
fmt.Println(comp["name"])
```

### Response shape

Same as a single element of the `GET /components` array.

---

## GET /entities

Returns a page of alive entities in unspecified order.

```
GET /entities[?limit=N]
→ 200 OK  application/json
→ 400 Bad Request  (limit not in [1, 10000])
```

### Query parameter

`limit` — maximum number of entities to return. Integer in `[1, 10000]`. Default: `1000`.

### Curl

```
curl "http://localhost:8080/entities?limit=50"
```

### Go client

```go
resp, _ := http.Get("http://localhost:8080/entities?limit=50")
defer resp.Body.Close()

var entities []map[string]any
json.NewDecoder(resp.Body).Decode(&entities)
for _, e := range entities {
    fmt.Println(e["id"], e["name"])
}
```

### Response shape

```json
[
  {"id": "1"},
  {"id": "2", "name": "player"},
  {"id": "3", "name": "enemy"}
]
```

Fields:
- `id` — numeric entity ID as a decimal string.
- `name` — entity name; omitted if the entity has none.

---

## GET /entities/{id}

Returns detailed information about a single entity.

```
GET /entities/{id}
→ 200 OK  application/json   (entity found and alive)
→ 400 Bad Request            (id is not a valid decimal integer)
→ 404 Not Found              (no alive entity with that ID)
```

### Curl

```
curl http://localhost:8080/entities/42
```

### Go client

```go
resp, _ := http.Get("http://localhost:8080/entities/42")
defer resp.Body.Close()
if resp.StatusCode == http.StatusNotFound {
    fmt.Println("not found")
    return
}
var entity map[string]any
json.NewDecoder(resp.Body).Decode(&entity)
fmt.Println(entity["name"], entity["parent"])
```

### Response shape

```json
{
  "id": "42",
  "name": "player",
  "parent": "10",
  "prefabs": ["5"],
  "components": [
    {
      "id": "20",
      "name": "main.Position",
      "size": 8,
      "align": 4,
      "type": "main.Position"
    },
    {
      "id": "21",
      "name": "main.Velocity",
      "size": 8,
      "align": 4,
      "type": "main.Velocity"
    }
  ],
  "pairs": ["167772160"]
}
```

Fields:
- `id` — entity ID as decimal string.
- `name` — entity name; omitted if unset.
- `parent` — parent entity ID if the entity has a `ChildOf` pair; omitted otherwise.
- `prefabs` — list of prefab entity IDs from `IsA` pairs; omitted if none.
- `components` — registered data/tag components on this entity. `ChildOf` and `IsA` pairs
  are excluded (they appear in `parent` and `prefabs` instead).
- `pairs` — remaining pair IDs (encoded as `uint64` decimal strings), excluding `ChildOf`
  and `IsA`. Omitted if none.

---

## GET /snapshot

Returns the full world state as a JSON snapshot, equivalent to `world.MarshalJSON()`.

```
GET /snapshot
→ 200 OK  application/json
→ 500 Internal Server Error  (marshaling failed)
```

### Curl

```
curl http://localhost:8080/snapshot > world.json
```

### Go client

```go
resp, _ := http.Get("http://localhost:8080/snapshot")
defer resp.Body.Close()
snapshot, _ := io.ReadAll(resp.Body)
// snapshot is []byte suitable for world.UnmarshalJSON
```

The snapshot includes entities, component values, names, `ChildOf` hierarchies, `IsA`
prefabs, and custom pair data. The format matches `world.MarshalJSON()` /
`world.UnmarshalJSON()`.

---

## PUT /snapshot

Replaces the world state from a JSON snapshot. Equivalent to calling
`world.UnmarshalJSON(body)`.

```
PUT /snapshot
Content-Type: application/json
Body: <snapshot JSON>

→ 204 No Content   (snapshot loaded)
→ 400 Bad Request  (body is not valid JSON, or UnmarshalJSON returned an error)
```

> **Concurrency:** `PUT /snapshot` acquires an exclusive write lock. Do not call it
> concurrently with other world operations or while `Progress` is running.

### Curl

```
curl -X PUT http://localhost:8080/snapshot \
  -H "Content-Type: application/json" \
  -d @world.json
```

### Go client

```go
f, _ := os.Open("world.json")
defer f.Close()

req, _ := http.NewRequest(http.MethodPut, "http://localhost:8080/snapshot", f)
resp, err := http.DefaultClient.Do(req)
if err != nil {
    log.Fatal(err)
}
resp.Body.Close()
if resp.StatusCode != http.StatusNoContent {
    log.Fatalf("PUT /snapshot: %d", resp.StatusCode)
}
```

---

## GET /query

Evaluates a Flecs Query Language v2 expression and returns matched entities with optional
typed-component field values as JSON (v0.95.0 shipped v1; v0.96.0 extended to v2). See
[QueryDSL.md](QueryDSL.md) for the full v2 grammar reference.

```
GET /query?expr=<urlencoded-expression>[&limit=N][&offset=N][&fields=true|false]
→ 200 OK           application/json
→ 400 Bad Request  (missing/blank expr, parse error, invalid limit/offset/fields)
→ 503 Service Unavailable  (world exclusively locked by another goroutine)
```

### Query parameters

| Parameter | Default | Constraints |
|-----------|---------|-------------|
| `expr` | — | **Required.** URL-encoded FQL v2 expression. |
| `limit` | `256` | Max `4096`. Non-integer or negative → 400. |
| `offset` | `0` | Non-integer or negative → 400. |
| `fields` | `true` | `true` or `false`. Other values → 400. |

### Response shape

```json
{
  "expr": "Position, !Disabled",
  "count": 42,
  "limit": 256,
  "offset": 0,
  "results": [
    {
      "entity": 1234,
      "path": "units.archer",
      "fields": {
        "Position": {"X": 1.0, "Y": 2.0},
        "Velocity": {"DX": 3.0, "DY": 0.0}
      }
    }
  ]
}
```

`count` is the total number of matched entities (always reflects the full result set,
not the windowed `results` array). `path` is the dot-separated entity path or an empty
string for unnamed entities.

**`fields` map rules** — mirrors the marshaling rules from `GET /component`:

| Component kind | Field value |
|----------------|-------------|
| Typed (Go struct) | `json.Marshal(value)` |
| Dynamic + custom marshaler | Marshaler output |
| Dynamic without marshaler | Omitted (not included) |
| Tag (zero-size) | Omitted (not included) |
| NOT-term | Omitted (excluded by query) |
| Wildcard pair | Omitted (concrete target unknown statically) |

Pair component keys use `~` as the separator: `"ChildOf~parent"` (consistent with the
`PUT /component` and `GET /component` pair encoding from Phase 16.34).

### Curl

```bash
# Simple component query
curl "http://localhost:8080/query?expr=Position"

# AND query with NOT
curl "http://localhost:8080/query?expr=Position%2C%20!Disabled"

# OR query
curl "http://localhost:8080/query?expr=Position%20%7C%7C%20Velocity"

# Pair query with pagination
curl "http://localhost:8080/query?expr=(ChildOf%2C%20scene)&limit=20&offset=40"

# Optional term (fields only when present)
curl "http://localhost:8080/query?expr=Position%2C%20%3FVelocity"

# Equality predicate (exact entity)
curl "http://localhost:8080/query?expr=%24this%20%3D%3D%20hero"

# Name-match predicate
curl 'http://localhost:8080/query?expr=$this+~+"unit"'

# Traversal Up (entity or ancestor carries Position)
curl "http://localhost:8080/query?expr=Position.Up"

# Source binding (reads Position from fixed entity "hero")
curl "http://localhost:8080/query?expr=Position(hero)"

# Type-list expansion
curl "http://localhost:8080/query?expr=AndFrom(prefab)"

# Field values suppressed
curl "http://localhost:8080/query?expr=Position&fields=false"
```

### Go client

```go
import (
    "encoding/json"
    "fmt"
    "net/http"
    "net/url"
)

type QueryResult struct {
    Expr    string `json:"expr"`
    Count   int    `json:"count"`
    Limit   int    `json:"limit"`
    Offset  int    `json:"offset"`
    Results []struct {
        Entity int64                      `json:"entity"`
        Path   string                     `json:"path"`
        Fields map[string]json.RawMessage `json:"fields,omitempty"`
    } `json:"results"`
}

func queryEntities(base, expr string, limit, offset int) (*QueryResult, error) {
    u := fmt.Sprintf("%s/query?expr=%s&limit=%d&offset=%d",
        base, url.QueryEscape(expr), limit, offset)
    resp, err := http.Get(u)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    var result QueryResult
    return &result, json.NewDecoder(resp.Body).Decode(&result)
}
```

### Error response for parse failures

```json
{
  "error": "query parse error at position 9 (near \"XYZ\"): unknown identifier \"XYZ\""
}
```

The `position` field in the error message is the 0-based rune offset where the parser
detected the error. The FlecsExplorer UI uses this to place an inline error marker in
the query input box.

---

## GET /type_info/{path}

Returns the reflection schema for a named component as JSON (v0.94.0). The path is a
dot-separated name resolved via `world.Lookup` (e.g. `"Position"`, `"physics.Velocity"`).
Supports typed Go components with recursive depth-N field expansion, primitive-type
annotations, cycle detection, and slice/array/map/pointer handling. Dynamic components
(`RegisterDynamicComponent`) and zero-size tag components are also supported.

> **Path separator divergence from C upstream.** C flecs uses `/` as the path separator.
> Go flecs uses `.` (the `world.Lookup` default). Request
> `GET /type_info/physics.Velocity`, not `GET /type_info/physics/Velocity`.

```
GET /type_info/{path}[?depth=N]
→ 200 OK  application/json  Cache-Control: max-age=300
→ 400 Bad Request           (?depth= is non-numeric, negative, or > 16)
→ 404 Not Found             (path unknown, or entity has no TypeInfo in the registry)
```

### Query parameter: `?depth=N`

| Value | Behaviour |
|-------|-----------|
| absent | default depth 8 — new schema (see below) |
| `0` | header-only response (name/id/size/align/kind), no field expansion |
| `1` | **back-compat** — byte-identical to the v0.87.0 depth-1 schema |
| `2`–`16` | new schema with N struct-nesting levels expanded |
| `< 0`, `> 16`, non-numeric | `400 Bad Request` |

### Curl

```
curl http://localhost:8080/type_info/Position
curl "http://localhost:8080/type_info/Position?depth=3"
curl "http://localhost:8080/type_info/Position?depth=0"   # header only
curl "http://localhost:8080/type_info/Position?depth=1"   # v0.87.0 back-compat
```

### Response shape (new schema — depth ≠ 1)

For a typed Go component `game.Player`:

```json
{
  "name": "game.Player",
  "id": "42",
  "size": 32,
  "align": 8,
  "kind": "struct",
  "unit": "Percentage",
  "fields": [
    {"name": "Pos",   "kind": "struct",    "type": "game.Vec3",
     "fields": [
       {"name": "X", "kind": "primitive", "type": "float32"},
       {"name": "Y", "kind": "primitive", "type": "float32"},
       {"name": "Z", "kind": "primitive", "type": "float32"}
     ]},
    {"name": "HP",    "kind": "primitive", "type": "int32"},
    {"name": "Tags",  "kind": "slice",     "element": {"kind": "primitive", "type": "string"}},
    {"name": "Stats", "kind": "map",
     "key":   {"kind": "primitive", "type": "string"},
     "value": {"kind": "primitive", "type": "int32"}},
    {"name": "Owner", "kind": "pointer",
     "element": {"kind": "struct", "type": "game.Player", "recursive": true}}
  ]
}
```

For a dynamic component:

```json
{"name": "DynComp", "id": "7", "size": 8, "align": 8, "kind": "dynamic"}
```

### Response shape (v0.87.0 back-compat — explicit `?depth=1`)

```json
{
  "name": "main.Position",
  "size": 16,
  "align": 8,
  "fields": [
    { "name": "X", "type": "float64", "offset": 0 },
    { "name": "Y", "type": "float64", "offset": 8 }
  ]
}
```

### Type node fields (new schema)

Each node in the type tree (`fields[]`, `element`, `key`, `value`) carries:

| Field | When present | Meaning |
|-------|-------------|---------|
| `name` | field nodes | Go struct field name |
| `kind` | always | `"struct"`, `"primitive"`, `"slice"`, `"array"`, `"map"`, `"pointer"`, `"interface"`, `"chan"`, `"func"`, `"dynamic"` |
| `type` | struct, primitive, recursive marker | qualified Go type name or primitive kind string |
| `underlying` | named primitive only | underlying kind string (e.g. `"int32"` for `type Score int32`) |
| `recursive` | struct | `true` when this type is already on the current path (cycle detected) |
| `length` | array | element count |
| `fields` | struct | expanded sub-fields (absent at depth limit or when recursive) |
| `element` | slice, array, pointer | element type node |
| `key` | map | key type node |
| `value` | map | value type node |
| `unit` | top-level response | unit entity name if attached via `world.SetUnit` |

### Primitive type mapping

| Go type | `"type"` value |
|---------|---------------|
| `bool` | `"bool"` |
| `int`, `int8`, …, `int64` | `"int"`, `"int8"`, … |
| `uint`, `uint8`, …, `uint64` | `"uint"`, `"uint8"`, … |
| `float32`, `float64` | `"float32"`, `"float64"` |
| `complex64`, `complex128` | `"complex64"`, `"complex128"` |
| `string` | `"string"` |
| `type Score int32` (named) | `"pkg.Score"` + `"underlying": "int32"` |
| `byte` (alias for `uint8`) | `"uint8"` (aliases are indistinguishable via reflection) |
| `rune` (alias for `int32`) | `"int32"` |

### Cycle detection semantics

The walk uses a `seen` set tracking struct types along the **current path** from root to
the current node. If a struct type appears a second time on the same path it is emitted as
`{"kind": "struct", "type": "...", "recursive": true}` instead of being expanded.

Two sibling fields that reference the same type are **not** a cycle — each sibling
receives the parent path's `seen` set independently, so both expand fully.

```
type T struct { L *Sub; R *Sub }   // L and R both expand Sub — not recursive
type Node struct { Next *Node }    // Next.element is {"recursive": true}
```

---

## PUT /entity

Creates or claims an entity. The request body is JSON. All fields are optional.

```json
{ "id": 12345678, "name": "myEntity", "parent": "parentName" }
```

> **JSON-body divergence from C upstream.** C flecs uses `PUT /entity/<path>` with a
> name embedded in the URL. Go flecs uses a JSON body so that ID-claim and parent can be
> expressed in one call without URL encoding. Path separator for `parent` is `.`
> (Go flecs default), not `/` as in C upstream.

```
PUT /entity
→ 200 OK              application/json  { "id": <uint64>, "name": "<string>" }
→ 400 Bad Request     malformed JSON body
→ 404 Not Found       parent path does not resolve to a live entity
→ 409 Conflict        id field is alive at a different generation
→ 503 Service Unavailable  world unavailable (unexpected internal panic)
```

### Fields

- `id` _(optional uint64)_ — claim a specific entity ID via `MakeAlive`. The ID encodes
  both the slot index (lower 32 bits) and generation (upper 32 bits); construct one with
  `flecs.MakeEntity(index, generation)`. Bypasses any active entity-ID range. Returns
  `409 Conflict` if the slot is already alive at a different generation.
- `name` _(optional string)_ — name the entity via `Writer.SetName`. Names must not
  contain `.` (the path separator).
- `parent` _(optional string)_ — dot-separated path resolved via `Reader.Lookup`. If
  found, adds `(ChildOf, parent)` to the new entity. Returns `404 Not Found` if the
  path does not resolve.

### Curl

```
# Create an anonymous entity
curl -X PUT http://localhost:8080/entity -d '{}'

# Create a named entity
curl -X PUT http://localhost:8080/entity -d '{"name":"hero"}'

# Create a named child of an existing entity
curl -X PUT http://localhost:8080/entity -d '{"name":"sword","parent":"hero"}'

# Claim a specific ID (MakeAlive)
curl -X PUT http://localhost:8080/entity -d '{"id":5000}'
```

### Go client

```go
resp, err := http.NewRequest(http.MethodPut, "http://localhost:8080/entity",
    strings.NewReader(`{"name":"hero"}`))
if err != nil {
    log.Fatal(err)
}
resp.Header.Set("Content-Type", "application/json")
res, err := http.DefaultClient.Do(resp)
if err != nil {
    log.Fatal(err)
}
defer res.Body.Close()

var result struct {
    ID   uint64 `json:"id"`
    Name string `json:"name"`
}
json.NewDecoder(res.Body).Decode(&result)
fmt.Println(result.ID, result.Name)
```

---

## DELETE /entity/{path...}

Deletes an entity identified by a dot-separated path. The path is resolved via
`Reader.Lookup`; if it resolves, `Writer.Delete` is called.

> **Path separator divergence from C upstream.** C flecs uses `/` as the path separator
> in `DELETE /entity/<path>`. Go flecs uses `.` (the `world.Lookup` default). Send
> `DELETE /entity/parent.child`, not `DELETE /entity/parent/child`.

```
DELETE /entity/{path...}
→ 200 OK              empty body
→ 400 Bad Request     empty path
→ 404 Not Found       path does not resolve to a live entity
→ 503 Service Unavailable  world unavailable (unexpected internal panic)
```

### Curl

```
curl -X DELETE http://localhost:8080/entity/hero
curl -X DELETE http://localhost:8080/entity/parent.child
```

### Go client

```go
req, _ := http.NewRequest(http.MethodDelete, "http://localhost:8080/entity/hero", nil)
res, err := http.DefaultClient.Do(req)
if err != nil {
    log.Fatal(err)
}
fmt.Println(res.StatusCode) // 200
```

---

## PUT /component/{entity}/{component}

Sets or adds a component on an entity. The `{entity}` segment is a dot-separated path
resolved via `Reader.Lookup`; the `{component}` segment is either a plain entity name or a
tilde-separated pair `<rel>~<tgt>` (see [Pair encoding](#pair-encoding) below).

> **Path-separator and encoding divergence from C upstream.** C flecs encodes the
> component as a `?component=` query parameter (parsed by `flecs_id_parse`) and the value
> as `?value=`. Go flecs uses URL path segments (consistent with
> `DELETE /entity/{path...}` from Phase 16.33), a JSON request body, and `~` for pairs.
> All three divergences are deliberate and documented here.

```
PUT /component/{entity}/{component}
→ 200 OK              empty body on success
→ 400 Bad Request     malformed JSON body; non-empty body on tag component; wrong size on dynamic
→ 404 Not Found       entity path or component path does not resolve
→ 413 Request Entity Too Large   body exceeds 1 MB
→ 503 Service Unavailable        unexpected internal panic
```

### Component kinds

| Kind | Detection | Body | Write call |
|---|---|---|---|
| Tag | no `TypeInfo` OR `TypeInfo.Size == 0` | **must be empty** (`400` if non-empty) | `fw.AddID(e, id)` |
| Typed data | `TypeInfo.Type != nil` | JSON of the registered Go type | `fw.SetByID(e, id, v)` |
| Typed pair | `TypeInfo.Type != nil` on pair ID | JSON of the registered Go type | `fw.SetPairByID(e, rel, tgt, v)` |
| Dynamic | `TypeInfo.Type == nil`, `TypeInfo.Size > 0` | JSON string of base64 of exactly `TypeInfo.Size` bytes | `SetIDPtr(fw, e, id, ptr)` |

### Pair encoding

Use `~` (tilde) as the separator between the relationship and target paths:

```
PUT /component/myentity/ChildOf~parent     # tag pair
PUT /component/myentity/HasData~slot1      # typed pair (if registered)
```

Tilde is unreserved in RFC 3986 (no percent-encoding needed) and is not valid in Flecs
entity names. Resolution splits on the first `~`; each side is resolved separately via
`Reader.Lookup`. `404` if either side fails to resolve.

### Curl

```
# Add a typed component
curl -X PUT http://localhost:8080/component/hero/Position \
     -d '{"X":10.0,"Y":20.0}'

# Add a tag
curl -X PUT http://localhost:8080/component/hero/Warrior

# Add a dynamic component (base64 of 4 bytes)
curl -X PUT http://localhost:8080/component/hero/Dyn4 \
     -d '"AAAAAA=="'

# Add a tag pair
curl -X PUT http://localhost:8080/component/hero/ChildOf~village
```

---

## GET /component/{entity}/{component}

Reads the live value of a component on an entity and returns it as JSON. Mirrors
`MarshalJSON`'s per-component serialization rules so that snapshot reads and single-component
reads stay byte-for-byte consistent.

> **Pair encoding.** Same `~` separator as `PUT /component` — see above.

```
GET /component/{entity}/{component}
→ 200 OK  application/json  Cache-Control: no-store
→ 400 Bad Request          malformed path (invalid percent-encoding in path segment)
→ 404 Not Found            entity path unresolved, component path unresolved,
                           or entity does not carry the component
→ 405 Method Not Allowed   verb other than GET, PUT, or DELETE on this route
→ 503 Service Unavailable  unexpected internal panic
```

### Response shape per component kind

| Kind | Detection | Response body |
|---|---|---|
| Tag | no `TypeInfo` OR `TypeInfo.Size == 0` | `{}` |
| Typed data / typed pair | `TypeInfo.Type != nil` | `json.Marshal(value)` of the registered Go type |
| Dynamic with marshaler | `TypeInfo.Type == nil`, marshaler registered | marshaler output (arbitrary JSON) |
| Dynamic (no marshaler) | `TypeInfo.Type == nil`, no marshaler | JSON string of base64-encoded raw bytes |

### Curl

```
# Read a typed component
curl http://localhost:8080/component/hero/Position
# → {"X":10.0,"Y":20.0}

# Read a tag (zero-size component)
curl http://localhost:8080/component/hero/Warrior
# → {}

# Read a dynamic component (returns base64 JSON string)
curl http://localhost:8080/component/hero/Dyn4
# → "AAAAAA=="

# Read a typed pair
curl http://localhost:8080/component/hero/Likes~Village
# → {"N":42}
```

---

## DELETE /component/{entity}/{component}

Removes a component from an entity. Resolves both paths and calls `fw.RemoveID(e, id)`.

> **Idempotent.** Removing a component the entity does not hold returns `200 OK` (matching
> the upstream C `ecs_remove_id` silent-no-op behaviour). This is a locked-in decision.

> **Pair encoding.** Same `~` separator as `PUT /component` — see above.

```
DELETE /component/{entity}/{component}
→ 200 OK              always when both paths resolve (idempotent)
→ 404 Not Found       entity path or component path does not resolve
→ 503 Service Unavailable   unexpected internal panic
```

### Curl

```
curl -X DELETE http://localhost:8080/component/hero/Position
curl -X DELETE http://localhost:8080/component/hero/ChildOf~village
```

---

## Error responses

All error responses use JSON with an `"error"` field:

```json
{"error": "entity not found"}
```

HTTP status codes:
- `400 Bad Request` — malformed path parameter, query parameter, or request body.
- `404 Not Found` — entity or component does not exist.
- `405 Method Not Allowed` — wrong HTTP method for a known path.
- `500 Internal Server Error` — world marshaling failed (rare).

---

## Unimplemented C flecs REST endpoints

The upstream C flecs REST API provides many additional endpoints not yet ported to Go
flecs. Each callout below explains what the C endpoint does and why it is absent.

### Entity mutation endpoints

> **Partially ported in Go flecs (v0.88.0).** `PUT /entity` (create or claim) and
> `DELETE /entity/{path...}` (delete by dot-separated path) are now implemented — see
> [`## PUT /entity`](#put-entity) and [`## DELETE /entity/{path...}`](#delete-entitypath)
> above. The Go form uses a JSON request body instead of a URL-embedded path, and `.` as
> the path separator instead of `/` (deliberate C divergence).
>
> Not yet ported: the richer C `GET /entity/<path>` that returns reflection data,
> inherited components, doc strings, and alert status (requires `ecs_meta_cursor`).

### Component mutation endpoints

> **Fully ported in Go flecs (v0.92.0).** The complete component-mutation arc is now
> implemented:
> - `PUT /component/{entity}/{component}` (set or add) — v0.89.0
> - `DELETE /component/{entity}/{component}` (remove) — v0.89.0
> - `GET /component/{entity}/{component}` (read live value) — v0.92.0 (this phase)
>
> See [`## GET /component/{entity}/{component}`](#get-componententitycomponent),
> [`## PUT /component/{entity}/{component}`](#put-componententitycomponent),
> and [`## DELETE /component/{entity}/{component}`](#delete-componententitycomponent)
> above. Go flecs deliberately diverges from C upstream: path-segment encoding, JSON body
> (not `?value=` query parameter), and `~` as the pair separator. All divergences are
> documented in the PUT section above.

### Toggle endpoint

**Shipped in Go flecs v0.90.0** as two routes registered in `NewRESTHandler`:

#### `PUT /toggle/{entity}`

Toggles the `Disabled` tag on an entity.

| `?enabled=` | Effect |
|---|---|
| `true` | Removes `Disabled` (enables the entity). |
| `false` | Adds `Disabled` (disables the entity). |
| omitted | Flips the current state. |

- **`200 OK`** — body `{"enabled": <bool>}` reflecting the new state.
- **`400`** — `?enabled=` value is not a valid boolean.
- **`404`** — entity not found at the dot-separated path.
- **`503`** — unexpected world panic.

Entity paths use the same dot-separated format as all other Go REST endpoints (e.g. `parent.child`).

#### `PUT /toggle/{entity}/{component}`

Toggles a per-component enable bit via the `CanToggle` trait (Phase 15.3). The component
segment uses the same encoding as `PUT /component`: plain name for a regular component,
`rel~tgt` for a pair (tilde separator, no URL-encoding needed).

- **`200 OK`** — body `{"enabled": <bool>}` reflecting the new state.
- **`400`** — component is not registered as `CanToggle`, or entity does not have the component.
- **`404`** — entity or component path does not resolve.
- **`503`** — unexpected world panic.

#### Deliberate divergences from C upstream

| Aspect | Go (v0.90.0) | C upstream (rest.c:509) |
|---|---|---|
| Query param name | `?enabled=` | `?enable=` (singular) |
| Component selection | URL path segment `/{component}` | `?component=` query param |
| Response body | `{"enabled": <bool>}` | Empty 200 body |

The `enabled` spelling matches the response body field. The path-segment approach is
consistent with `PUT /component/{entity}/{component}` (Phase 16.34). The non-empty
response body saves a follow-up read and tells the client the new state directly.

### Query execution endpoint

> **Ported in Go flecs (v0.96.0, Phase 16.41).** `GET /query?expr=<expr>` is fully
> implemented — see [`## GET /query`](#get-query) above. The Go implementation parses
> Flecs Query Language v2 (AND/OR/NOT, scope groups, optional terms, traversal postfixes,
> source binding, query variables, equality predicates, `AndFrom`/`OrFrom`/`NotFrom`) and
> returns matched entities with typed field values. Streaming (`chunked transfer`) is
> deferred; see [QueryDSL.md](QueryDSL.md) for the full v2 language reference.

### World dump endpoint

> **Not yet ported in Go flecs.** `GET /world` returns all serializable world data
> through `ecs_world_to_json`, filtered to exclude built-in modules and Flecs internal
> entities. This is conceptually similar to `GET /snapshot` but FlecsStats-aware and
> more selective. The Go `GET /snapshot` endpoint returns the full `MarshalJSON` output
> without filtering.

### Type-info and reflection endpoints

> **Ported in Go flecs (v0.87.0 + v0.94.0).** `GET /type_info/{path}` is fully
> implemented — see [`## GET /type_info/{path}`](#get-type_infopath) above. v0.87.0
> shipped depth-1 field walk; v0.94.0 adds depth-N recursion (default 8, max 16 via
> `?depth=N`), precise primitive-type annotations, cycle detection, and
> slice/array/map/pointer handling. Dynamic components and zero-size tags are also
> supported. Enum member annotation is not yet ported.

### Listing endpoints

> **Not yet ported in Go flecs.** `GET /queries` lists named queries; `GET /tables`
> lists archetype table information. Named queries and table introspection via the REST
> API are not yet ported.

### Aggregated stats endpoints (FlecsStats module)

> **Ported in Go flecs (v0.86.0 + v0.93.0).** `GET /stats/world` and `GET /stats/pipeline`
> are implemented and support the `?period=` query parameter (v0.93.0). Omitting `?period=`
> returns the single-frame `StatsSnapshot()` data (back-compat). Adding `?period=second`,
> `?period=minute`, or `?period=hour` returns multi-period aggregated `WorldStatsAggregated`
> / `PipelineStatsAggregated` with per-metric `{avg, min, max}` objects. See
> [`## GET /stats/world`](#get-statsworld) and [`## GET /stats/pipeline`](#get-statspipeline)
> for full details. The full FlecsStats C module (histogram percentiles, custom user
> metrics, variable window sizes) is not ported.

### Command capture endpoints

> **Not yet ported in Go flecs.** `GET /commands/capture` starts recording deferred
> commands; `GET /commands/frame/<n>` retrieves the recorded commands for a given frame.
> Deferred command capture for the REST API is not yet ported.

### Action and script endpoints

> **Not yet ported in Go flecs.** `PUT /action/<action>` triggers server-side actions
> (e.g. `shrink_memory`). `PUT /script/<path>` hot-reloads a FlecsScript entity.
> FlecsScript is a C-only DSL that is not ported to Go flecs.

### FlecsExplorer

> **Not yet ported in Go flecs.** [Flecs Explorer](https://www.flecs.dev/explorer/) is
> a browser-based world inspector that connects via the C REST API. It requires the
> entity mutation, query execution, stats, and type-info endpoints listed above. The Go
> REST handler provides raw JSON inspection only; the Explorer browser UI is not
> integrated.

### WebSocket / streaming, authentication, and JavaScript client

> **Not yet ported in Go flecs.** The C REST server supports WebSocket-based event
> streaming, connection-level authentication, and ships a JavaScript client library
> (`flecs.js`). None of these are ported. The Go handler is a plain `http.Handler`; any
> authentication or TLS should be handled by the caller's `*http.Server`.

---

## Feature gaps discovered in Phase 14.9 (FlecsRemoteApi port)

- **Query execution endpoint** (`GET /query?expr=`) — ✅ shipped in v0.95.0 (Phase 16.40). See [`## GET /query`](#get-query) and [QueryDSL.md](QueryDSL.md).
- **Entity mutation endpoints** (`PUT /entity`, `DELETE /entity/{path...}`) — ✅ shipped in v0.88.0. See [`## PUT /entity`](#put-entity) and [`## DELETE /entity/{path...}`](#delete-entitypath).
- **Component mutation endpoints** (`PUT /component`, `DELETE /component`) — ✅ mutation shipped in v0.89.0. See [`## PUT /component/{entity}/{component}`](#put-componententitycomponent) and [`## DELETE /component/{entity}/{component}`](#delete-componententitycomponent). `GET /component` (value read) not yet ported.
- **Toggle endpoint** (`PUT /toggle`) — ✅ shipped in v0.90.0. See [`### Toggle endpoint`](#toggle-endpoint) above.
- **Multi-period aggregated stats (FlecsStats module)** — single-frame `GET /stats/world` and `GET /stats/pipeline` shipped in v0.86.0; multi-period aggregation (`?period=`) and the FlecsStats module are still not ported.
- **Type-info / reflection endpoint** (`GET /type_info/{path}`) — ✅ depth-1 `reflect` walk shipped in v0.87.0; depth-N recursion, primitive-type annotations, cycle detection, and slice/array/map/pointer handling shipped in v0.94.0. Enum member annotation not yet ported.
- **FlecsExplorer integration** — browser UI requires unported endpoints; not integrated.
