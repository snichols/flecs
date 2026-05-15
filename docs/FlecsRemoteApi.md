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

```
GET /stats/world
→ 200 OK  application/json  Cache-Control: no-store
→ 503 Service Unavailable   (world panic / teardown)
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

```
GET /stats/pipeline
→ 200 OK  application/json  Cache-Control: no-store
→ 503 Service Unavailable   (world panic / teardown)
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

## GET /type_info/{path}

Returns the reflection schema for a named component as JSON. The path is a dot-separated
name resolved via `world.Lookup` (e.g. `"Position"`, `"physics.Velocity"`). Supports
typed Go components (struct fields with name, type, and byte offset), dynamic components
registered via `RegisterDynamicComponent` (size/align only; `opaque: true`), and
zero-size tag components.

> **Path separator divergence from C upstream.** C flecs uses `/` as the path separator
> (`ecs_lookup_path_w_sep(..., "/", ...)`). Go flecs uses `.` (the `world.Lookup` default).
> Request `GET /type_info/physics.Velocity`, not `GET /type_info/physics/Velocity`. Nested
> struct fields are rendered as opaque `reflect.Type.String()` strings — depth-1 only; no
> recursive field expansion in v1.

```
GET /type_info/{path}
→ 200 OK  application/json  Cache-Control: max-age=300
→ 404 Not Found             (path unknown, or entity has no TypeInfo in the registry)
```

### Curl

```
curl http://localhost:8080/type_info/Position
```

### Go client

```go
resp, err := http.Get("http://localhost:8080/type_info/Position")
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()

var info struct {
    Name   string `json:"name"`
    Size   uint64 `json:"size"`
    Align  uint64 `json:"align"`
    Fields []struct {
        Name   string `json:"name"`
        Type   string `json:"type"`
        Offset uint64 `json:"offset"`
    } `json:"fields"`
    Opaque bool   `json:"opaque,omitempty"`
    Unit   string `json:"unit,omitempty"`
}
json.NewDecoder(resp.Body).Decode(&info)
fmt.Println(info.Name, info.Size, len(info.Fields))
```

### Response shape

For a typed Go component `Position { X, Y float64 }`:

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

For a dynamic component registered via `RegisterDynamicComponent(fw, "DynComp", 8, 8)`:

```json
{
  "name": "DynComp",
  "size": 8,
  "align": 8,
  "fields": [],
  "opaque": true
}
```

### Fields

- `name` — `reflect.Type.String()` for typed Go components (e.g. `"main.Position"`), or
  the registered string name for dynamic components.
- `size` — `unsafe.Sizeof` of the component type; `0` for zero-size structs.
- `align` — `unsafe.Alignof` of the component type; `0` for zero-size structs.
- `fields` — ordered struct fields at depth 1; `[]` for zero-size, dynamic, or non-struct
  types. Each field: `name` (Go field name), `type` (`reflect.Type.String()`; pointer /
  interface / slice / nested-struct fields are rendered as opaque type strings), `offset`
  (byte offset within the struct).
- `opaque` — `true` only for dynamic components (`TypeInfo.Type == nil`); omitted
  otherwise.
- `unit` — unit entity name if `world.UnitFor(id)` returns a registered unit; omitted
  otherwise.

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

> **Partially ported in Go flecs (v0.89.0).** `PUT /component/{entity}/{component}` (set
> or add a component value) and `DELETE /component/{entity}/{component}` (remove a
> component) are now implemented — see [`## PUT /component/{entity}/{component}`](#put-componententitycomponent)
> and [`## DELETE /component/{entity}/{component}`](#delete-componententitycomponent)
> above. Go flecs deliberately diverges from C upstream: path-segment encoding (consistent
> with `DELETE /entity/{path...}`), JSON body (not `?value=` query parameter), and `~` as
> the pair separator (avoids URL-encoded parentheses). All three divergences are documented
> in the PUT section above.
>
> Not yet ported: `GET /component/<path>?component=X` (read one component value) — requires
> `ecs_ptr_to_json` / value-encoding support not yet ported to Go flecs.
>
> Go workaround for reads: use `flecs.Get[T]` directly.

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

> **Not yet ported in Go flecs.** `GET /query?expr=<expr>` evaluates a Flecs Query
> Language expression and streams matched entities with their field values. This requires
> the FlecsQueryLanguage DSL and `ecs_iter_to_json`, neither of which is ported to Go
> flecs. Go queries are constructed with type parameters (`flecs.NewQuery`,
> `flecs.NewCachedQuery`) rather than a string DSL.

### World dump endpoint

> **Not yet ported in Go flecs.** `GET /world` returns all serializable world data
> through `ecs_world_to_json`, filtered to exclude built-in modules and Flecs internal
> entities. This is conceptually similar to `GET /snapshot` but FlecsStats-aware and
> more selective. The Go `GET /snapshot` endpoint returns the full `MarshalJSON` output
> without filtering.

### Type-info and reflection endpoints

> **Partially ported in Go flecs (v0.87.0).** `GET /type_info/{path}` is now implemented
> — see [`## GET /type_info/{path}`](#get-type_infopath) above. It supports typed Go
> structs (depth-1 field walk), dynamic components, and zero-size tags. Full meta-cursor
> parity (`ecs_type_info_to_json` depth-N recursion, primitive-type annotations, enum
> members) is **not yet ported** — the Go `reflect` walk is sufficient for v1.

### Listing endpoints

> **Not yet ported in Go flecs.** `GET /queries` lists named queries; `GET /tables`
> lists archetype table information. Named queries and table introspection via the REST
> API are not yet ported.

### Aggregated stats endpoints (FlecsStats module)

> **Partially ported in Go flecs (v0.86.0).** `GET /stats/world` and `GET /stats/pipeline`
> are now implemented and return the single-frame `StatsSnapshot()` data (world counters,
> per-system metrics, per-phase timing). The upstream C equivalents return *multi-period*
> aggregated statistics collected by the `FlecsStats` module; multi-period aggregation and
> the `?period=` query parameter are **not yet ported**.

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

- **Query execution endpoint** (`GET /query?expr=`) — requires FlecsQueryLanguage DSL; not ported to Go flecs.
- **Entity mutation endpoints** (`PUT /entity`, `DELETE /entity/{path...}`) — ✅ shipped in v0.88.0. See [`## PUT /entity`](#put-entity) and [`## DELETE /entity/{path...}`](#delete-entitypath).
- **Component mutation endpoints** (`PUT /component`, `DELETE /component`) — ✅ mutation shipped in v0.89.0. See [`## PUT /component/{entity}/{component}`](#put-componententitycomponent) and [`## DELETE /component/{entity}/{component}`](#delete-componententitycomponent). `GET /component` (value read) not yet ported.
- **Toggle endpoint** (`PUT /toggle`) — ✅ shipped in v0.90.0. See [`### Toggle endpoint`](#toggle-endpoint) above.
- **Multi-period aggregated stats (FlecsStats module)** — single-frame `GET /stats/world` and `GET /stats/pipeline` shipped in v0.86.0; multi-period aggregation (`?period=`) and the FlecsStats module are still not ported.
- **Type-info / reflection endpoint** (`GET /type_info/{path}`) — depth-1 `reflect` walk shipped in v0.87.0; depth-N recursion, primitive-type annotations, and full meta-cursor parity not yet ported.
- **FlecsExplorer integration** — browser UI requires unported endpoints; not integrated.
