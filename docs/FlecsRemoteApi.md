# REST API

The Go flecs REST addon exposes world inspection and snapshot save/load over stdlib
`net/http`. It is the starting point for debugging running worlds, building tooling, and
implementing save/load workflows.

See also:
- [Quickstart](Quickstart.md) — hands-on walkthrough; the REST handler is introduced there first
- [Systems](Systems.md) — `World.Stats()` is the data source for `GET /stats`
- [ComponentTraits](ComponentTraits.md) — `GET /components` returns trait information alongside each component

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

- **GET endpoints** call `w.Read(func(*Reader) { ... })`, which acquires a read lock.
  Multiple concurrent GET requests are safe as long as no write is in progress.
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
| GET | `/stats` | World stats snapshot |
| GET | `/components` | All registered component infos |
| GET | `/components/{id}` | Single component info by numeric ID |
| GET | `/entities` | Alive entities (optional `?limit=N`) |
| GET | `/entities/{id}` | Entity detail |
| GET | `/snapshot` | Full world snapshot (JSON) |
| PUT | `/snapshot` | Load a world snapshot |

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

> **Not yet ported in Go flecs.** C flecs exposes `PUT /entity/<path>` (create by
> name/path), `DELETE /entity/<path>` (delete by path), and a richer `GET
> /entity/<path>` that returns reflection data, inherited components, doc strings, and
> alert status. These require path-based entity lookup and a reflection / meta-cursor
> API (`ecs_meta_cursor`) that is not yet ported.
>
> Go workaround: use `world.Write(func(*flecs.Writer) { ... })` for mutation, and
> `world.Lookup(name)` for path-based lookup.

### Component mutation endpoints

> **Not yet ported in Go flecs.** C flecs exposes `GET /component/<path>?component=X`
> (read one component value), `PUT /component/<path>?component=X[&value=V]` (add or set
> a component value), and `DELETE /component/<path>?component=X` (remove a component).
> Mutation requires the reflection API; value serialization requires `ecs_ptr_to_json` /
> `ecs_ptr_from_json` which depend on the meta module.
>
> Go workaround: use `flecs.Get[T]`, `flecs.Set[T]`, `flecs.Remove[T]` directly.

### Toggle endpoint

> **Not yet ported in Go flecs.** `PUT /toggle/<path>?enable=[true|false]` enables or
> disables an entity (via the `Disabled` tag) or a per-component enable bit (via
> `ecs_enable_id`). Entity disabling and the `CanToggle` component trait are not yet
> ported to Go flecs.

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

> **Not yet ported in Go flecs.** `GET /type_info/<path>` returns the reflection schema
> for a component as JSON, using `ecs_type_info_to_json`. This requires the meta /
> reflection module which is not yet ported.

### Listing endpoints

> **Not yet ported in Go flecs.** `GET /queries` lists named queries; `GET /tables`
> lists archetype table information. Named queries and table introspection via the REST
> API are not yet ported.

### Aggregated stats endpoints (FlecsStats module)

> **Not yet ported in Go flecs.** `GET /stats/world` and `GET /stats/pipeline` return
> aggregated multi-period statistics collected by the `FlecsStats` module (import
> `FlecsStats` in C). The Go `GET /stats` endpoint returns a single-frame snapshot from
> `World.Stats()`. Multi-period aggregation and the FlecsStats module are not yet ported.

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
- **Entity / component mutation endpoints** (`PUT /entity`, `DELETE /entity`, `PUT /component`, `DELETE /component`) — require reflection/meta module; not ported.
- **Toggle endpoint** (`PUT /toggle`) — requires entity disabling (`Disabled` tag) and `CanToggle` trait; not ported.
- **Aggregated stats (FlecsStats module)** — `GET /stats/world`, `GET /stats/pipeline`; FlecsStats module not ported.
- **Type-info / reflection endpoint** (`GET /type_info`) — requires meta-cursor (`ecs_meta_cursor`); not ported.
- **FlecsExplorer integration** — browser UI requires unported endpoints; not integrated.
