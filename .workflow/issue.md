## Goal

Add `GET /component/{entity-path}/{component-path}` to the REST handler in @rest.go, completing the REST component-mutation arc. PUT (write/upsert) and DELETE (remove) shipped in v0.89.0 (Phase 16.34); this phase adds the read side so REST clients can inspect a single component's live value on a single entity without pulling a full snapshot.

The endpoint reads the live value of a component on an entity and returns it as JSON, mirroring `MarshalJSON`'s per-component serialization rules: typed components → `json.Marshal(value)`; dynamic components with a registered marshaler → marshaler output; plain dynamic → base64-encoded raw bytes; tag (zero-size) → `{}`. The handler dispatches under the existing `/component/{entity}/{component}` route (PUT/DELETE today; add GET).

Path encoding mirrors Phase 16.34 exactly so PUT/GET/DELETE share the same URL syntax: dot-delimited paths for both entity and component, `~` as the pair separator (e.g. `Likes~Bob`) — reuse `resolveComponentPaths` in @rest.go (line 582), do not re-implement.

**Response shape**
- 200 OK with `Content-Type: application/json` and `Cache-Control: no-store` (live data)
- 404 if entity path unresolved, component path unresolved, or entity does not carry the component
- 400 on malformed path / unparseable pair encoding
- 405 on other verbs
- 503 during teardown (panic recovery, mirroring PUT/DELETE)

**Implementation hooks (verified)**
- Read-only: wrap in `w.Read(func(fr *Reader) { ... })` — no `writeMu` needed
- Resolve paths via `resolveComponentPaths(fr, entityPath, componentPath)` at @rest.go:582 (returns `e, compID, relID, tgtID, isPair, entityFound, compFound`)
- Entity-carries-component check: `fr.HasID(e, compID)` at @scope.go:82 — return 404 if false
- Typed components: `fr.GetByID(e, compID)` returns `(any, bool)` — the marshal path at @marshal.go:347 and @marshal.go:402 uses this exact accessor; then `json.Marshal(v)`
- Tag (compSize == 0): write `{}` directly; do not call `GetByID`
- Dynamic components: branch on `world.dynamicMarshalers[compID]` (registered in @component_dynamic.go:52 via `RegisterDynamicComponentWithMarshaler`); if present, call the marshaler with raw bytes from `GetIDPtr(fr, e, compID)` at @component_dynamic.go:75 and emit its output; otherwise base64-encode the raw `compSize` bytes and emit as a JSON string (mirrors @marshal.go:391 and @marshal.go:453)
- Component metadata (size, reflect.Type): `w.registry.LookupByID(compID)` returns `*TypeInfo` with `Size` and `Type` — same lookup as `restPutComponent` at @rest.go:626
- New helper `restGetComponent(w *World) http.HandlerFunc` (no writeMu parameter); register via `mux.HandleFunc(\"GET /component/{entity}/{component}\", restGetComponent(w))` at @rest.go:72-73

## Constraints

- @rest.go — existing component handler structure: `restPutComponent` (line 608) and `restDeleteComponent` (line 729) define the canonical pattern for path resolution, 404 ordering (entity first, then component), panic recovery → 503, and Content-Type. New `restGetComponent` must mirror this structure but call `w.Read` instead of `w.Write` and take no `writeMu`. Method dispatch at @rest.go:70-75 — add GET to the component route.
- @rest.go:582 `resolveComponentPaths` — pair encoding helper that owns the `~` separator convention. Reuse verbatim; the issue body and any future regex helper must not duplicate the parsing logic.
- @rest.go:203 — sets `Content-Type: application/json` (existing snapshot pattern). @rest.go:228 and @rest.go:241 — set `Cache-Control: no-store` for live data endpoints. New GET reuses both headers.
- @marshal.go:347-456 — per-component JSON serialization rules. The GET response body must match this output byte-for-byte for typed, dynamic-with-marshaler, dynamic-without-marshaler, and pair components so that snapshot ↔ single-component reads stay consistent. Specifically @marshal.go:391 (base64 fallback) and @marshal.go:453-456 (`reflect.NewAt(info.Type, entry.data).Elem().Interface()`) are the precedents for the dynamic and table-direct paths.
- @component_dynamic.go:75 `GetIDPtr(s scope, e ID, componentID ID) unsafe.Pointer` — raw-bytes accessor for dynamic components. Returns nil if the entity does not carry the component; combined with `fr.HasID` this is the dynamic-path 404 signal. @component_dynamic.go:52 — `RegisterDynamicComponentWithMarshaler` stores the marshal hooks in `world.dynamicMarshalers` (private map; handler is in-package).
- @internal/component/registry.go:97 `Registry.LookupByID(id) (*TypeInfo, bool)` — already used by PUT. `TypeInfo.Size` and `TypeInfo.Type` are the fields the handler reads.
- @scope.go:82 `(*Reader).HasID(e, id) bool` — required for the entity-lacks-component 404 branch. Cheap; avoids materializing the value before deciding to 404.
- @rest_component_test.go — existing PUT/DELETE test fixtures (component registration, entity setup, request/response assertions). New GET tests live in the same file to consolidate the arc; follow the same naming style (`TestRest_GetComponent_*`).
- @docs/FlecsRemoteApi.md — extend the \"Component endpoints\" section with `GET /component/{entity}/{component}`; cross-reference PUT/DELETE entries; document pair encoding, tag behavior (`{}`), and the 4 status codes.
- @docs/README.md — flip the `GET /component` value-read line on the REST gaps row to shipped in v0.92.0 (PUT/DELETE/toggle gap line at @ROADMAP.md:92-93 tracks the partial closure).
- @CHANGELOG.md — v0.92.0 entry following the v0.91.0 / v0.90.0 / v0.89.0 layout (date 2026-05-15 baseline; bump as needed at merge).
- @ROADMAP.md:3 — bump \"Shipped (through v0.91.0)\" heading to v0.92.0; add Phase 16.37 row under the Phase 16.36 timer entry; cross-reference Phase 16.34 partial-closure note at line 92 (this phase closes the `GET /component` portion of that gap).
- @README.md — update REST feature row if it tracks individual component endpoints.

**Required tests** (in @rest_component_test.go):
- `TestRest_GetComponent_Typed` — entity has `Position{X:1,Y:2}`, GET returns `{\"X\":1,\"Y\":2}`
- `TestRest_GetComponent_PairComponent` — entity has `(Likes, Bob)` with payload, GET via `Likes~Bob` returns the payload
- `TestRest_GetComponent_Tag` — zero-size component carried by entity, GET returns `{}` with 200
- `TestRest_GetComponent_Dynamic_Marshaler` — registered marshaler output flows through
- `TestRest_GetComponent_Dynamic_NoMarshaler` — base64 of raw bytes (JSON string)
- `TestRest_GetComponent_EntityMissing` → 404
- `TestRest_GetComponent_ComponentNotRegistered` → 404
- `TestRest_GetComponent_EntityLacksComponent` → 404 (entity + component both exist, but `HasID` is false)
- `TestRest_GetComponent_MalformedPath` → 400
- `TestRest_GetComponent_BadMethod` (e.g. POST on the component route) → 405
- `TestRest_GetComponent_ConcurrentReadsDuringWrite` — race test: many concurrent GETs while a goroutine drives PUT/DELETE on the same component; assert no data races and every GET returns a clean pre- or post-state
- `TestRest_GetComponent_DuringTeardown` → 503

**Mechanical acceptance**
- `go vet ./...` clean
- `golangci-lint run ./...` clean
- `go test ./... -race -count=3` clean
- Coverage ≥ 95.0% (current baseline)

**Non-goals**
- Query-style multi-entity component reads (that is the `/query` DSL endpoint, separate phase)
- Streaming / WebSocket subscription to component changes
- PATCH semantics — PUT already covers upsert

**Target version:** v0.92.0
**Phase:** 16.37
