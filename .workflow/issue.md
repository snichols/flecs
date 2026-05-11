## Goal

Add a minimal, read-only REST API addon that exposes world inspection plus snapshot save/load, so external tools (browser, IDE, CLI) can introspect a running flecs world over HTTP. This unlocks live tooling/debugging without coupling the core API to HTTP.

After this lands:

```go
w := flecs.New()
// ... populate world

server := &http.Server{Addr: ":8080", Handler: flecs.NewRESTHandler(w)}
go server.ListenAndServe()

// External tool:
//   GET  /stats              -> world stats JSON
//   GET  /components         -> list of registered components
//   GET  /components/{id}    -> ComponentInfo JSON
//   GET  /entities           -> list of alive entity IDs
//   GET  /entities/{id}      -> entity details (name, components)
//   GET  /snapshot           -> full world MarshalJSON output
//   PUT  /snapshot           -> unmarshal a JSON document into the world (use with care)
```

### Scope (this phase implements ONLY)

- A `http.Handler` factory; users wire it into their own `*http.Server`.
- Read-only inspection endpoints plus snapshot save/load.
- JSON responses, JSON request bodies.
- Path-based routing via stdlib `http.ServeMux` (Go 1.22+ path patterns). No external router dependency.

### Out of scope (this phase does NOT implement)

- Write endpoints other than `PUT /snapshot` (no per-entity Set via HTTP).
- Per-component value endpoints (e.g. `GET /entities/{id}/components/{name}/value`) — defer.
- WebSocket / SSE live updates.
- Authentication / authorization.
- Rate limiting.
- CORS configuration (users wrap).
- TLS termination.
- HTTP/2-specific features.
- gRPC.
- Query DSL endpoint (no `POST /query` that takes a term list).
- Profile / Pprof integration (stdlib provides this separately).
- Custom route prefix (users wrap with `http.StripPrefix` if needed).
- HTTP request body size limits beyond what `http.Server` enforces.

### Deliverables

1. **New file `rest.go`** at root flecs package containing the REST handler factory and route handlers.

2. **`func NewRESTHandler(w *World) http.Handler`** — returns a configured `*http.ServeMux` bound to the given world. Users wire it into their own `*http.Server`.

3. **Endpoints:**
   - `GET /stats` — returns `World.Stats()` as JSON. `Content-Type: application/json`. 200 OK.
   - `GET /components` — array of `ComponentInfo` for all registered components. Each entry includes ID, Name, Size, Align, Type (as string). 200 OK.
   - `GET /components/{id}` — single `ComponentInfo`. 404 if not registered. `{id}` is the uint64 representation of the ID.
   - `GET /entities` — array of objects: `{\"id\": \"12\", \"name\": \"foo\"}`. Optional `?limit=N` (default 1000; max 10000). 400 if `limit` is malformed or out of range.
   - `GET /entities/{id}` — `{\"id\": \"12\", \"name\": \"foo\", \"components\": [{...}], \"parent\": \"1\", \"prefabs\": [\"7\"], \"pairs\": [...]}`. 404 if entity is not alive.
   - `GET /snapshot` — returns full `World.MarshalJSON()` output. `Content-Type: application/json`. 200 OK or 500 if marshal fails.
   - `PUT /snapshot` — reads request body, calls `World.UnmarshalJSON(body)`. 204 No Content on success; 400 on parse/unmarshal error with body `{\"error\": \"...\"}`. **Loud godoc warning**: replaces world state; not transactional; partial application possible on error.
   - 404 for any unmatched route. 405 Method Not Allowed for wrong method on a known route.

4. **Routing:**
   - Use stdlib `http.ServeMux` with Go 1.22+ path patterns.
   - `r.PathValue(\"id\")` to extract path params.
   - Register exact patterns: `GET /stats`, `GET /components`, `GET /components/{id}`, `GET /entities`, `GET /entities/{id}`, `GET /snapshot`, `PUT /snapshot`.

5. **Helper types/functions (internal):**
   - `type entityListResponse struct { ID string; Name string; ... }`
   - `type entityDetailResponse struct { ... }` composed from the existing meta + marshal helpers.
   - `writeJSON(w http.ResponseWriter, status int, v any)` helper for response shaping.
   - `writeError(w http.ResponseWriter, status int, msg string)` helper.

6. **Tests in `rest_test.go`:**
   - Setup: build a small world (Position+Velocity, ChildOf hierarchy, named entities), wrap in `httptest.NewServer`.
   - `GET /stats`: 200, JSON parses as `Stats`, has expected EntityCount.
   - `GET /components`: 200, contains Position and Velocity by name.
   - `GET /components/{id}`: 200 for registered ID; 404 for unregistered.
   - `GET /entities`: 200, returns array, default limit honored.
   - `GET /entities` with `?limit=N`: 200, length matches limit.
   - `GET /entities` with malformed limit: 400.
   - `GET /entities/{id}`: 200 for alive entity with components/name/parent; 404 for dead.
   - `GET /snapshot`: 200, `json.Valid`, round-trips via Unmarshal into a fresh world.
   - `PUT /snapshot`: 204 on success; verify world received the data; 400 on malformed body; 400 on Unmarshal error.
   - Method not allowed: `POST /stats` -> 405.
   - Unknown route: `GET /unknown` -> 404.
   - Concurrent reads: N goroutines hitting `GET /stats` in parallel — must not race under `go test -race`. World is NOT goroutine-safe in general; the REST handler exposes the world as if read-only. Document that `PUT /snapshot` must NOT run concurrently with other readers or another world mutator. Tests verify READ-side parallel safety only.

7. **Documentation:**
   - Godoc on `NewRESTHandler` clearly states concurrency limitations (no mutex; users must not call `PUT /snapshot` concurrently with other world mutation or read).
   - `doc.go`: new \"REST API\" section with a code snippet.
   - CHANGELOG entry under Unreleased.
   - README: add row to feature table.

8. **Examples:**
   - `example_rest_test.go` with `ExampleNewRESTHandler` using `httptest.NewServer`. Show `GET /stats` and verify response.

9. **Mechanical acceptance:**
   - `go test ./... -race -count=2` passes.
   - `go vet ./...` clean.
   - `golangci-lint run` clean.
   - Coverage on `flecs` >= 90% (no regression from 96.0%).
   - All exported symbols have godoc.
   - No third-party deps (`net/http` and `encoding/json` only).

### Implementer pointers

- Use only stdlib `net/http` and `encoding/json`. No router framework.
- Use `http.ServeMux` with Go 1.22+ patterns (`GET /entities/{id}`).
- `r.PathValue(\"id\")` to extract path params.
- Parse `{id}` as `strconv.ParseUint(s, 10, 64)` then cast to `flecs.ID`.
- For entity detail: reuse `World.EntityComponents`, `World.GetName`, `World.ParentOf`, `World.EachPrefab`. Compose into a response shape — don't expose internal types directly.
- For snapshot endpoints: directly call `World.MarshalJSON()` / `World.UnmarshalJSON(body)`.
- DO NOT introduce a separate router type; just a configured `*http.ServeMux`.
- DO NOT add `(*World).ServeHTTP` (would conflate the World API with HTTP). Factory `NewRESTHandler(w)` keeps concerns separate.

### C reference (cite, do not paraphrase)

- `/work/agents/claude/projects/SanderMertens/flecs/src/addons/rest.c` — analog. Note: the C version is much more elaborate (WebSocket, per-entity edit endpoints, query execution); we're shipping a minimal read-only surface.

## Constraints

- @meta.go — `ComponentInfo` shape and component metadata accessors used by `/components` endpoints.
- @stats.go — `World.Stats()` is the payload for `GET /stats`.
- @marshal.go — `World.MarshalJSON()` / `World.UnmarshalJSON()` back the snapshot endpoints.
- @world.go — `World` lifecycle, `IsAlive`, `EntityComponents`, and other accessors used to compose entity details.
- @childof.go — `ParentOf` accessor for entity-detail `parent` field.
- @isa.go — `EachPrefab` traversal for entity-detail `prefabs` field.
- @name.go — `GetName` for entity-detail `name` field.
- @id.go — `ID` type; path `{id}` is `strconv.ParseUint(s, 10, 64)` cast to `flecs.ID`.
- @doc.go — package overview; add a \"REST API\" section with a code snippet.
- @CHANGELOG.md — add an Unreleased entry for the REST addon.
- @README.md — add a row to the feature table for the REST addon.
- Stdlib-only: `net/http` and `encoding/json`. No third-party deps. Routing must use `http.ServeMux` with Go 1.22+ path patterns and `r.PathValue`.
- World is not goroutine-safe in general; the REST handler treats the world as read-only. `PUT /snapshot` must not run concurrently with other readers or mutators — document this loudly in godoc.
- No `(*World).ServeHTTP` method; only the `NewRESTHandler(w)` factory.
- Coverage on `flecs` must stay >= 90% (currently 96.0%); `go test ./... -race -count=2`, `go vet ./...`, and `golangci-lint run` must all be clean.
