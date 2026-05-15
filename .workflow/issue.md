## Goal

Add a single new REST mutation route to `NewRESTHandler` — **`PUT /toggle/<entity-path>`** — that toggles the built-in `Disabled` tag on an entity (and, as a v1 sub-route, a `CanToggle`-marked component bit on a specific component). This is the next step in the FlecsRemoteApi port after Phase 16.34 (REST component mutation, v0.89.0).

Use cases:
- **Editor**: enable/disable game objects from a UI panel.
- **Live debugger**: temporarily disable an entity to test behavior.
- **FlecsExplorer**: inspector toggle for entity visibility.

This is target **v0.90.0** (next after v0.89.0 just shipped at commit `eb17e38`).

### Endpoints

- **`PUT /toggle/<entity-path>?enabled=<bool>`** — toggles the entity's `Disabled` tag.
  - `?enabled=true` → remove `Disabled` (enable).
  - `?enabled=false` → add `Disabled` (disable).
  - omitted `enabled` → flip current state.
  - `200 OK` with body `{ "enabled": <bool> }` indicating the new state.
  - `404` if the entity is not found.
  - `400` if `enabled` is present but unparseable as a bool.
- **`PUT /toggle/<entity-path>/<component-path>?enabled=<bool>`** — toggles a single component's enable bit via Phase 15.3 `CanToggle` (locked in for v1; small additional surface, completes the toggle story).
  - Same `enabled` semantics (true/false/omitted-flip).
  - `400` if the component isn't `CanToggle`-registered.
  - `404` if entity or component isn't found.
  - `200 OK` with `{ "enabled": <bool> }`.

### Locked decisions (from the design spec)

1. **`enabled` omitted → flip current state** (intuitive; matches the toggle-button mental model).
2. **Per-component toggle included in v1** (small surface; completes the story).
3. **Response body is `{ "enabled": <bool> }`** (the new state; lets clients avoid a follow-up read).

### Deliberate C divergences

- **Query parameter name `enabled`** (Go spec) vs upstream C's `enable` (rest.c:509). Pick `enabled` per the goal spec — it reads more naturally in URL form and matches the response body field. Document the divergence in `docs/FlecsRemoteApi.md` alongside the existing `.`-vs-`/` path and `~`-vs-`()` pair divergences.
- **Path style**: dot-separated entity path resolved via `Reader.Lookup` (consistent with Phase 16.33 / 16.34); component path also dot-separated with `~` for pair encoding (consistent with Phase 16.34).
- **Response body**: Go returns `{"enabled": <bool>}`; C upstream returns an empty 200 body. Document as a deliberate Go addition that saves a round-trip.

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage >= 95.0%

### Explicit non-goals

- No `GET /toggle` to query state. Clients read state from the `PUT` response or via existing inspection endpoints.
- No `DELETE` method — `PUT` is idempotent and covers all states.
- No authentication.

## Constraints

- @rest.go — handler infrastructure; `NewRESTHandler` registers routes via `http.ServeMux.HandleFunc`. Phase 16.33 lines 471 / 539 (`restPutEntity` / `restDeleteEntity`) and Phase 16.34 lines 604 / 725 (`restPutComponent` / `restDeleteComponent`) are the template for the new handler: `w.Read` to resolve paths, then `writeMu.Lock` + `w.Write` (with `recover()`) to mutate; `503` on unexpected panic. The shared `writeMu sync.Mutex` declared at line 67 must be reused for the new toggle handler to maintain the same concurrent-write serialisation. The doc-comment routes block at lines 39-52 (and the typed-handler call list at lines 55-71) needs new lines for `PUT /toggle/<path...>` and `PUT /toggle/<entity>/<component>`. `resolveComponentPaths` (line 578) already handles the entity-plus-component (with `~`-pair) resolution that the sub-route needs — reuse it.
- @query_filters.go — `DisableEntity(fw, e)` adds the `Disabled` tag (line 54), `EnableEntity(fw, e)` removes it (line 64), `IsDisabled(s, e)` predicate (line 70). These are the primitive operations the entity-level toggle handler calls. Idempotent on both sides (per the godoc).
- @cantoggle.go — `IsCanToggle(w, componentID)` (line 27) is the predicate the per-component toggle handler must check before calling `EnableID` (line 45) / `DisableID` (line 62). Both panic if the component isn't `CanToggle`-marked — the handler must check `IsCanToggle` up-front and return `400` rather than relying on `recover()`. Both also panic if the entity does not have the component; that case is also `400` (caller bug) rather than `503`.
- @scope.go — `Reader.Lookup(path)` (line 206) returns `(ID, bool)`. `Writer` embeds `Reader` and adds `AddID` / `RemoveID` (lines 378 / 383). These are the only scope primitives needed for the route.
- @rest_entity_test.go — Phase 16.33 test patterns (test naming `TestRESTEntity_<Case>`, `httptest.NewServer(NewRESTHandler(w))` setup, table-driven case style). The new test file `rest_toggle_test.go` should follow the same naming and setup conventions.
- @rest_component_test.go — Phase 16.34 test patterns; the per-component toggle handler shares the same component-path resolution (`~` pairs, dot entities) so the test setup mirrors this file's helper structure.
- @docs/FlecsRemoteApi.md — line 969 has the "Toggle endpoint" gap section currently marked **"Not yet ported in Go flecs."**. Replace with a real "### Toggle endpoint" section documenting both routes, the `enabled=` parameter, the response body, and the C divergences. Line 1048 has the "Toggle endpoint (PUT /toggle)" bullet in the trailing gap list — remove it (port is no longer outstanding).
- @docs/README.md — line 90 is the rolling FlecsRemoteApi gap line ("REST explorer — partial handler"). Update to add v0.90.0 toggle shipped (so the remaining gaps are `GET /component`, query DSL `GET /query?expr=`, multi-period stats, and FlecsExplorer integration). Line 179 has the "Toggle endpoint (PUT /toggle)" gap bullet — remove it.
- @README.md — line 270 has the REST API row in the headline feature table; bump from `_(v0.89.0)_` to `_(v0.90.0)_` and add `PUT /toggle/{path}` (plus the per-component sub-route) to the route list.
- @CHANGELOG.md — add a new top entry: `## v0.90.0 — <date> — Phase 16.35: REST toggle endpoint` documenting the API addition, the locked decisions (flip-on-omitted, per-component included, response body shape), and the deliberate C divergences (`enabled` vs `enable`, response body addition).
- @ROADMAP.md — line 3 heading `## Shipped (through v0.89.0)` bumps to `## Shipped (through v0.90.0)`. Add a v0.90.0 bullet under Shipped describing the toggle endpoint and noting partial closure of `docs/README.md` line 90 (toggle subset).
- @CONTRIBUTING.md — lines 68-72 list the doc-update matrix: new behavior → `docs/` page, new phase → `ROADMAP.md` move + `CHANGELOG.md` entry, headline-API additions → `README.md`. This Phase ships all three categories.
- Upstream reference: `/work/agents/claude/projects/SanderMertens/flecs/src/addons/rest.c:497-540` — `flecs_rest_toggle` handler. Notable upstream details: line 509 reads `?enable=` (singular; Go spec uses `enabled=`); line 511 reads `?component=` query param (Go uses URL path segment instead, consistent with Phase 16.34); line 513 calls `ecs_enable(world, e, enable)` (entity-level toggle — equivalent to Go's `DisableEntity` / `EnableEntity`); line 531 checks `EcsCanToggle` then calls `ecs_enable_id` at 537 (per-component toggle — equivalent to Go's `IsCanToggle` + `EnableID` / `DisableID`). The C handler's route prefix dispatch is at rest.c:2083 (`!ecs_os_strncmp(req->path, "toggle/", 7)`).
- Upstream API reference: `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h:3338` — `ecs_enable` (entity-level, toggles `EcsDisabled`); line 3358 — `ecs_enable_id` (per-component, requires `EcsCanToggle`). Note line 1858 confirms `EcsCanToggle` is the trait gate.
- **Test deliverable** — new file `rest_toggle_test.go` with at least these cases (extend as needed for coverage >= 95.0%): (1) `PUT /toggle/Foo?enabled=false` then verify `IsDisabled` is true; (2) `PUT /toggle/Foo?enabled=true` re-enables; (3) `PUT /toggle/Foo` (no param) flips current state; (4) `PUT /toggle/NonexistentEntity` → 404; (5) `PUT /toggle/Foo?enabled=bogus` → 400; (6) `PUT /toggle/Foo/Position?enabled=false` where Position is `CanToggle`; (7) `PUT /toggle/Foo/Position?enabled=false` where Position is NOT `CanToggle` → 400; (8) concurrent PUTs race-clean (mirrors `rest_entity_test.go:241` concurrent-PUT pattern); (9) response body shape `{"enabled": <bool>}` matches new state on both true/false/flip paths; (10) idempotency — second `PUT ?enabled=false` on an already-disabled entity is still 200 with body `{"enabled": false}`.
