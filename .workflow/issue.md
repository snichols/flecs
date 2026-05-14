## Goal

Add a single new HTTP endpoint to the read-only REST handler that returns the **reflection schema** for a named component:

```
GET /type_info/<path>
```

Response: JSON describing the component's `size`, `align`, ordered `fields` (name + type-name + offset), and registered `unit` (if any). Works for typed components (via Go `reflect.StructField` walk, depth 1), dynamic components (Phase 16.13 — size/align only, fields empty/opaque), pair components (annotated with `is_pair`/`relationship`/`target`), and zero-size tag components (size 0, no fields).

Closes part of the gap at `docs/README.md` line 90 (REST explorer outstanding work) and line 181 (Type-info / reflection endpoint gap). The C upstream relies on the meta-cursor module (`ecs_meta_cursor`), but the Go port can satisfy this gap with the much narrower `reflect` walk — full meta-cursor parity is not required for v1.

### Endpoint contract

```
GET /type_info/<path>
→ 200 OK  application/json  Cache-Control: max-age=300
→ 404 Not Found             (path resolves to nothing, or resolved entity has no TypeInfo)
```

Example body for a typed component `Position { X, Y float64 }`:

```json
{
  "name": "Position",
  "size": 16,
  "align": 8,
  "fields": [
    { "name": "X", "type": "float64", "offset": 0 },
    { "name": "Y", "type": "float64", "offset": 8 }
  ]
}
```

### Locked decisions

1. **Path style** — simple-name only in v1. Go-flecs's `Lookup` (`name.go:97`) splits on `.`, not `/`. The endpoint URL-decodes the segment and feeds it directly to `w.Lookup(decoded)`. No translation between `.` and `/`. Document this as a deliberate divergence from C upstream (`src/addons/rest.c:311–312` uses `ecs_lookup_path_w_sep(..., "/", ...)`).
2. **Pointer / interface / channel / func fields** — render as opaque `reflect.Type.String()` strings; do not recurse.
3. **Nested struct depth** — 1 level. Nested struct fields render as `{ "name": "<field>", "type": "<reflect.Type.String()>", "offset": N }` with the type rendered as an opaque string (no recursive `fields` for nested structs in v1).
4. **Cache-Control** — `max-age=300` (5 min). Component registrations are stable within a session; this matches the read-mostly, low-mutation nature of the registry.
5. **Tag-vs-component disambiguation** — return **200** with `{"size": 0, "fields": []}` only if the resolved entity has a registered `TypeInfo` in the registry (registry.LookupByID succeeds). A bare entity-tag (alive entity with a `Name` but no `TypeInfo`) returns **404**.

### Wire shape — full schema

```json
{
  "name": "<reflect.Type.String() or registered name>",
  "size": <uintptr>,
  "align": <uintptr>,
  "fields": [
    { "name": "<field>", "type": "<reflect.Type.String()>", "offset": <uintptr> }
  ],
  "unit": "<unit-name>",       // only present if World.UnitFor(id) returns ok
  "is_pair": true,             // only present for pair components
  "relationship": "<rel-name>",// only present for pair components, and only if first entity is named
  "target": "<target-name>"    // only present for pair components, and only if second entity is named
}
```

For dynamic components (Phase 16.13), `name` is the registered string (not a Go type name), `fields` is `[]`, and the response includes `\"opaque\": true` to make the omitted-fields case explicit to consumers.

### Implementation sketch

1. Add `restTypeInfo(w *World) http.HandlerFunc` to `rest.go`, mounted as `mux.HandleFunc(\"GET /type_info/{path...}\", restTypeInfo(w))` (trailing wildcard captures slashes if present; URL-decode `r.PathValue(\"path\")` once).
2. Inside `w.Read(...)`:
   a. Resolve path → entity via `w.Lookup(decoded)`. Empty result → 404.
   b. Look up `TypeInfo` via `w.registry.LookupByID(id)` (registry.go:99). Not registered → 404.
   c. Build `typeInfoResponse`:
      - For `info.Type != nil && info.Type.Kind() == reflect.Struct`: walk `reflect.StructField` (NumField/Field(i)), emit `{Name, Type.String(), Offset}` for each. Pointer/interface/etc. fields just render their reflect-type string.
      - For `info.Type == nil` (dynamic component): `fields: []`, `opaque: true`.
      - For `id.IsPair()` (id.go:38–43): set `is_pair`, look up `First()/Second()` names via `w.GetName`.
      - For Size==0 with non-nil Type (zero-size struct): `fields: []`, no `opaque` flag.
   d. If `unitID, ok := w.UnitFor(id); ok`, attach `unit` to the response using the unit entity's name via `w.GetName(unitID)`.
3. Response headers: `Content-Type: application/json`, `Cache-Control: max-age=300`. Use the existing `writeJSON` helper (rest.go:158).
4. Goroutine safety: same as existing GETs — caller serializes against world mutation; the handler uses `w.Read(...)`.

### Test plan (`rest_type_info_test.go`, ≥ 8 cases)

1. `GET /type_info/Position` (typed two-field struct) → 200 with field names X/Y, types `float64`, offsets 0 and 8, `size:16`, `align:8`, no `unit`.
2. `GET /type_info/<dynamic-component-name>` (registered via `RegisterDynamicComponent`) → 200 with `size`/`align` set, `fields:[]`, `opaque:true`.
3. `GET /type_info/<bare entity name>` (alive entity, has `Name`, no TypeInfo) → 404.
4. `GET /type_info/Nonexistent` → 404.
5. `GET /type_info/<typed component with SetUnit applied>` → 200 with `unit` field equal to the unit entity's name.
6. `GET /type_info/<NestedHolder>` where `NestedHolder { Inner Position }` → 200 with one field `{name:\"Inner\", type:\"flecs_test.Position\", offset:0}` — proves depth-1 opaque rendering.
7. `GET /type_info/<typed component with pointer / interface / slice field>` → 200, those fields render as opaque type-name strings (no panic, no recursion).
8. Concurrent GETs under `-race` (10+ goroutines, 100 iterations each) → no race, all 200.
9. Coverage gate: rest.go per-function coverage ≥ 95.0% (matches the Phase 16.31 gate at `rest_stats_test.go:1–10`).

### Deliverables

1. `rest.go` — add route handler `restTypeInfo`, register on the mux alongside the Phase 16.31 stats endpoints, add response struct(s) with snake_case tags consistent with `worldStatsResponse` (rest.go:57–64).
2. `rest_type_info_test.go` — 8+ cases above.
3. `docs/FlecsRemoteApi.md` — new `## GET /type_info/{path}` section under the existing per-endpoint pattern (see `## GET /stats/world` block at lines 170–230 for the canonical shape: status-code block, curl, Go client, response shape, field-list paragraph). Document the `.` vs `/` divergence from C upstream and the depth-1 opaque-nested-struct decision.
4. `docs/README.md` — partially update line 90 (REST explorer entry) to indicate type_info is now shipped, and line 181 (Type-info / reflection endpoint) flipped to shipped with version pointer.
5. `README.md` — feature list bump.
6. `CHANGELOG.md` — v0.87.0 entry at the top.
7. `ROADMAP.md` — heading bump from `## Shipped (through v0.86.0)` to `## Shipped (through v0.87.0)`; add the bullet to the shipped list.

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage on `rest.go` per-function ≥ 95.0%

### Non-goals

- No mutation endpoints (`PUT`/`DELETE`).
- No query execution endpoint.
- No deep / recursive field iteration (depth=1).
- No write-side schema validation API.
- No "list all components" endpoint — clients call `/type_info/<specific>`.
- No translation between `.` and `/` path separators (divergence from C upstream is deliberate).
- No port of the C `ecs_meta_cursor` / meta module; the Go `reflect` walk is sufficient for v1.

## Constraints

- @rest.go — extend the mux with the new route alongside `GET /stats/world` / `GET /stats/pipeline`; reuse `writeJSON` (line 158) and the `Cache-Control` header pattern (lines 184, 197).
- @internal/component/registry.go — `*Registry.LookupByID` (line 99) is the source of `TypeInfo` for resolved entity IDs; `TypeInfo.Type reflect.Type` (typeinfo.go:67) drives the `reflect.StructField` walk.
- @internal/component/typeinfo.go — `TypeInfo` struct (Size, Align, Name, Type) is the v1 schema source-of-truth for typed components.
- @component_dynamic.go — `RegisterDynamicComponent` / `RegisterDynamicComponentWithMarshaler` (lines 31, 52); dynamic components have `TypeInfo.Type == nil` and must produce `fields:[]` + `opaque:true`.
- @units.go — `(*World).UnitFor(componentID ID) (ID, bool)` (line 37) provides the unit attachment; the unit entity's name is fetched via `w.GetName(unitID)`.
- @name.go — `(*World).Lookup(path string) (ID, bool)` (line 97) splits on `.`, not `/`. The endpoint accepts dot-separated paths; this is the deliberate divergence from C upstream `ecs_lookup_path_w_sep(..., "/", ...)`.
- @meta.go — `(*World).ComponentInfo(id) (ComponentInfo, bool)` (line 52) is the existing read-only metadata accessor; the new endpoint follows the same conventions (snapshot-by-value, no mutation, name fallback to `\"tag\"` for null-Type entries — but those return 404 here, not 200).
- @id.go — `MakePair(first, second ID) ID` (line 43) and the `IsPair()` / `First()` / `Second()` methods on ID drive the pair branch of the schema builder.
- @rest_stats_test.go — Phase 16.31 stats test file is the immediate precedent for test structure: cases for OK status, header, shape, after-Progress values, panic recovery via 503, concurrent access. Use the same `httptest.NewServer(NewRESTHandler(w))` setup.
- @docs/FlecsRemoteApi.md — section template (lines 170–230 for `GET /stats/world`) defines the per-endpoint docstring shape: status block, curl, Go client, response shape, field list.
- @docs/README.md — gap entries at lines 90 and 181 are what this issue partially / fully closes; update text accordingly.
- @CONTRIBUTING.md — coverage gate (≥ 95% on root flecs package), `-race`, `golangci-lint` are mandatory.
- Upstream C reference: `src/addons/rest.c:321–344` (`flecs_rest_get_type_info`), `src/addons/rest.c:2036–2038` (route dispatch), `src/addons/rest.c:305–318` (`flecs_rest_entity_from_path`; note slash separator), `src/addons/json/serialize_type_info.c:388–408` (`ecs_type_info_to_json` / `_buf`). Cited only for shape comparison — Go port does not depend on the C JSON schema, which is richer and tied to the meta-cursor.
