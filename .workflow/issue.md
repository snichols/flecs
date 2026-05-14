## Goal

Land Phase 16.33 — **REST entity mutation endpoints** — at v0.88.0, the next release after v0.87.0 (Phase 16.32, type-info; shipped c6b569b).

Extend `NewRESTHandler` with two routes that mutate entity state:

- **`PUT /entity`** — create or claim an entity. JSON body `{ "id"?: uint64, "name"?: string, "parent"?: string }`. Returns `{ "id": <uint64>, "name": "<name>" }`. Status: `200 OK`; `400` on malformed body; `404` if `parent` path does not resolve; `409 Conflict` if `id` is alive at a different generation; `503` if world is torn down.
- **`DELETE /entity/{path...}`** — resolve dot-separated path to an entity and delete it. Status: `200 OK` with empty body; `400` if path is empty; `404` if path does not resolve; `503` if world is torn down.

This deliberately diverges from upstream C `flecs_rest_put_entity` (which uses URL-path `PUT /entity/<slash-sep-path>` and a single `name` argument; @specs/SanderMertens/flecs/src/addons/rest.c lines 253-277). The Go form takes a JSON body and supports optional ID-claim and explicit parent path, mirroring how Phase 16.31 / 16.32 already prefer JSON shape and dot-separated paths.

**Use cases**: editor / live debugger creates and destroys entities for testing; webhook integration pushes entity lifecycle events into the world; FlecsExplorer tree-editor needs entity mutation.

**Scope boundary**: this phase covers ENTITY mutation only. Component mutation (PUT /component, DELETE /component) is Phase 16.34; toggle endpoint (PUT /toggle) is Phase 16.35.

Drains a portion of the `docs/README.md` REST-explorer gap entry (line 90) and the FlecsRemoteApi entity-mutation gap entry (line 178, formerly cited as line 173).

### Behavior — `PUT /entity`

- Empty body (`{}`): allocate a new entity via `Writer.NewEntity`. Return its ID.
- `id` set: claim that specific ID via `MakeAlive(fw, id)`. Bypasses any active range (per Phase 16.16 "MakeAlive bypass" precedent). Generation mismatch panic from `MakeAlive` (@entity_lifecycle.go:135-156) must be recovered and translated to `409 Conflict`.
- `name` set: after creation, call `Writer.SetName(e, name)` (@scope.go:419).
- `parent` set: resolve via `fr.Lookup(parent)` (dot-separated; @scope.go:206). If not found, return `404`. If found, add `MakePair(w.ChildOf(), parentID)` via `Writer.AddID`. (Equivalent to wrapping the `NewEntity` call in `WithinScope(fw, parent, ...)` — @entity_scope.go:13.)
- Response body: `{ "id": <uint64 of resulting ID>, "name": "<name>" }`. `name` is the empty string when no name was set.

### Behavior — `DELETE /entity/{path...}`

- Extract `path` via `r.PathValue("path")` and `url.PathUnescape`, matching @rest.go:386.
- Empty path → `400`.
- `fr.Lookup(path)` → `404` if not found.
- `fw.Delete(e)` (@scope.go:388).

### Concurrency & contract

- Both routes wrap mutations in `w.Write(func(fw *Writer) { ... })`. The PUT /snapshot precedent at @rest.go:373 establishes the pattern.
- Recover panics from `MakeAlive` (generation conflict) and any other write-path panic; map to `409` for generation conflict (detect via panic-message-prefix `flecs: MakeAlive:`) and `503` for unexpected panics (per Phase 16.31 pattern at @rest.go:203).
- Race-safety verified by go test -race with concurrent PUT + DELETE goroutines.

### Tests — `rest_entity_test.go` (≥ 8 cases)

1. PUT /entity with empty body → 200, returns a fresh ID.
2. PUT /entity with `name` → entity has that name; response echoes the name.
3. PUT /entity with `id` → claims that specific ID; subsequent GET /entities/{id} returns it.
4. PUT /entity with claim-conflict (alive at different generation) → 409.
5. PUT /entity with `parent` (resolvable) → child has (ChildOf, parent).
6. PUT /entity with `parent` (unresolvable) → 404.
7. PUT /entity with malformed JSON body → 400.
8. DELETE /entity/{name} → entity removed; subsequent GET /entities/{id} → 404.
9. DELETE /entity/nonexistent → 404.
10. DELETE /entity (bare empty path) → 400.
11. Race-detector clean: concurrent PUT + DELETE from multiple goroutines.
12. Coverage ≥ 95.0% on the new handlers.

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%

### Locked decisions

1. **Path style**: dot-separated, matching Phase 16.32 type_info (deliberate C divergence; C uses `/`).
2. **Claim-conflict status**: 409 Conflict.
3. **Parent must exist**: yes; 404 if `parent` does not resolve.
4. **DELETE success status**: 200 OK with empty body (not 204).

### Explicit non-goals

- No component mutation (Phase 16.34).
- No toggle endpoint (Phase 16.35).
- No PATCH /entity (no rename-or-reparent in one call; use DELETE + PUT or future PUT /component).
- No authentication.
- No CORS.

## Constraints

- @CONTRIBUTING.md — doc-update matrix: new phase requires `ROADMAP.md` (Future Work → Shipped) and `CHANGELOG.md` entry; new public API requires godoc and `README.md` headline-example refresh.
- @rest.go — existing handler. Phase 16.31 stats and 16.32 type_info precedents for routing (`mux.HandleFunc("VERB /path", handler(w))`), JSON encoding (`writeJSON`), error responses (`writeError`), 503 panic-recovery (lines 203-204), and dot-separated path resolution via `r.PathValue("path")` + `url.PathUnescape`.
- @scope.go — `Writer.NewEntity` (line 369), `Writer.Delete` (line 388), `Writer.AddID` (line 378), `Writer.SetName` (line 419), `Reader.Lookup` (line 206) for path resolution.
- @entity_lifecycle.go — `MakeAlive(fw, id)` (line 135). Panics on generation conflict; the handler must recover and translate to 409. Panics in deferred scope; since `w.Write` enters at depth 0, this is safe.
- @name.go — `World.SetName` (line 20); path-separator contract: "." separator, names may not contain ".".
- @entity_scope.go — `WithinScope(fw, parent, fn)` (line 13) optional convenience; spec uses explicit `AddID(e, MakePair(ChildOf, parent))` after `NewEntity` for clarity, but `WithinScope` is functionally equivalent.
- @childof.go — `World.ChildOf()` (line 10) and `MakePair(w.ChildOf(), parent)` (referenced at line 38) for the parent linkage.
- @docs/FlecsRemoteApi.md — add a "## Entity mutation endpoints" section with example `curl` invocations for both routes; document the deliberate JSON-body divergence from C.
- @docs/README.md — partial update at the REST-explorer gap entry (line 90) noting that entity-mutation shipped in v0.88.0; full closure still gated on component-mutation + toggle + query DSL.
- @README.md — feature-list bump for v0.88.0.
- @CHANGELOG.md — new v0.88.0 entry at the top following the v0.87.0 template.
- @ROADMAP.md — add Phase 16.33 to the Shipped list (after Phase 16.32 at line 90); bump the "Shipped (through vX.Y.Z)" heading to v0.88.0.
- @specs/SanderMertens/flecs/src/addons/rest.c — C reference: `flecs_rest_put_entity` (lines 253-277), `flecs_rest_delete_entity` (lines 481-494), `flecs_rest_entity_from_path` (lines 305-318, uses `ecs_lookup_path_w_sep` with "/" separator), routing dispatch (lines 2073-2103). Document the JSON-body and dot-separator divergence in `CHANGELOG.md`.
- Label: `snichols/queued`.
