## Goal

Add `PUT /component/<entity-path>/<component-path>` and `DELETE /component/<entity-path>/<component-path>` to the REST handler, completing the next slice of editor / FlecsExplorer parity after the Phase 16.33 entity mutation work (v0.88.0).

**Target version:** v0.89.0.

### Why now

- The two preceding REST phases (16.31 stats, 16.32 type-info, 16.33 entity mutation) are shipped and merged. Component mutation is the next call-out in `docs/README.md` line 90 and is one of the remaining items in the line 178 catch-all (`Entity / component mutation endpoints`).
- The Go-side primitives needed for the deserialization path already exist: `(*Writer).SetByID(e, id, v any)` (`scope.go:424`), the free `SetIDPtr(fw, e, id, src)` for dynamic-bytes components (`component_dynamic.go:109`), and the registry's `TypeInfo.Type reflect.Type` (`internal/component/typeinfo.go:53–67`). No new write primitive needs to be invented.
- Phase 16.33 established the URL-shape pattern (`{path...}`), JSON-body decoder, shared write mutex, and 503 panic-recovery (`rest.go:465–567`). Phase 16.34 follows the same skeleton.

### What ships

1. **`PUT /component/<entity-path>/<component-path>`** — set or add a component on an entity. Body is JSON.
   - Tag component (`TypeInfo.Size == 0` or no `TypeInfo` registered for the id but the id is a known component-entity): body **must be empty** (`400` if non-empty). Handler calls `fw.AddID(e, id)`.
   - Data component (`TypeInfo.Type != nil`): body is JSON of the typed value. Handler does `reflect.New(info.Type)`, `json.NewDecoder(r.Body).Decode(ptr.Interface())`, then `fw.SetByID(e, id, ptr.Elem().Interface())`. `SetByID` handles the rest (defer queue, hooks, OnReplace).
   - Dynamic component (`TypeInfo.Type == nil`, registered via `RegisterDynamicComponent`): body is a JSON string containing base64 of exactly `TypeInfo.Size` bytes — matches the opaque-bytes contract used by `unmarshalDynamic` (`marshal.go:960`). Handler base64-decodes, asserts length, then `flecs.SetIDPtr(fw, e, id, &buf[0])`.
   - Pair component: see encoding below.
   - Responses: `200 OK` empty body on success; `400` malformed JSON, non-empty body on tag, size mismatch on dynamic; `404` entity path or component path unresolvable; `413` body exceeds 1 MB; `503` unexpected panic.

2. **`DELETE /component/<entity-path>/<component-path>`** — remove the component from the entity.
   - Resolves both paths. Calls `fw.RemoveID(e, id)`.
   - Responses: `200 OK` always when both paths resolve, regardless of whether the entity actually held the component (idempotent — locked-in decision below). `404` only when entity or component path is unresolvable. `503` on panic.

3. **Body decoder**: wrap `r.Body` in `http.MaxBytesReader(rw, r.Body, 1<<20)` (1 MB cap, locked-in). On overflow, the decoder returns an error containing `http: request body too large`; map to `413 Request Entity Too Large`. Tag-path: also enforce empty body via `MaxBytesReader` + `io.ReadAll` + length check (cheaper than decoder for the empty case).

4. **Pair encoding in URL**: use `~` (tilde) as the separator between relationship and target paths. So `(ChildOf, parent)` becomes `ChildOf~parent`. Tilde is URL-safe per RFC 3986 (unreserved) and is not a valid character in flecs entity names (which forbid `.`, `/`, and most punctuation by convention). Resolution algorithm:
   - Split `<component-path>` on the first `~`. If no `~`, treat as a single (non-pair) component path.
   - For pairs: `fr.Lookup(rel)` and `fr.Lookup(tgt)`; `404` if either fails. Build `id := MakePair(rel, tgt)`.
   - The pair's effective type is what `w.registry.LookupByID(MakePair(rel, tgt))` returns; if not registered, the pair is a tag (use `AddID`). If registered with a Go type, decode the JSON body into that type and `fw.SetPairByID(e, rel, tgt, v)` (`scope.go:451`).
   - Deliberate divergence from C upstream (which encodes the component as `?component=(R,T)` query parameter and parses with `flecs_id_parse`, `src/addons/rest.c:411`). Documented as a divergence callout in `FlecsRemoteApi.md` alongside the Phase 16.33 callouts.

5. **Tests in `rest_component_test.go` (≥ 10 functions)**:
   - PUT data component with valid JSON → 200, component is set (verify via `flecs.Get`).
   - PUT data component with malformed JSON → 400.
   - PUT data component with wrong-shape JSON (e.g. string where struct expected) → 400.
   - PUT tag with empty body → 200, tag is added.
   - PUT tag with non-empty body → 400.
   - PUT dynamic component with valid base64 → 200, bytes written (verify via `GetIDPtr`).
   - PUT dynamic component with wrong-size base64 → 400.
   - PUT pair `R~T` (tag pair) with empty body → 200; PUT typed pair with JSON body → 200.
   - PUT with unresolvable entity path → 404.
   - PUT with unresolvable component path → 404.
   - PUT with body > 1 MB → 413.
   - DELETE existing component → 200, removed (`fr.HasID` false).
   - DELETE component the entity never had → 200 (locked-in idempotent semantic).
   - DELETE with unresolvable entity path → 404.
   - Concurrent PUT/DELETE under `-race` (mirrors `TestRESTEntityMutationConcurrent` from Phase 16.33).
   - 503 panic-recovery path for both handlers.
   - Per-function coverage: 100% on the new handlers; overall package coverage ≥ 95.0%.

6. **Docs and version bumps** (per `CONTRIBUTING.md` line 68–72):
   - `docs/FlecsRemoteApi.md` — add `## PUT /component/{entity}/{component}` and `## DELETE /component/{entity}/{component}` sections in the style of the v0.88.0 entity sections (lines 721–829); update the existing `### Component mutation endpoints` gap callout (line 865–874) to say "partially ported in v0.89.0" and link forward; update the "Shipped" status line at the bottom (line 952–953).
   - `docs/README.md` line 90 — append "component mutation endpoints (`PUT /component`, `DELETE /component`) shipped in v0.89.0".
   - `README.md` line 270 — extend the REST API row to mention the new endpoints; bump version annotation to `v0.89.0`.
   - `CHANGELOG.md` — new `## v0.89.0 — <date> — Phase 16.34: REST component mutation endpoints` entry at the top, mirroring the v0.88.0 structure (API additions, semantics, test coverage sections).
   - `ROADMAP.md` line 3 — change `## Shipped (through v0.88.0)` to `## Shipped (through v0.89.0)`; add a new entry at the end of the Phase 16 list under line 91 following the same format.

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Package coverage ≥ 95.0%

### Non-goals

- No `GET /component` value-read endpoint — depends on the meta-cursor / value-encoding path that Phase 16.32 deliberately did not pull in (it would essentially require porting `ecs_ptr_to_json`). Separate phase.
- No `PATCH /component` — overwrite via `PUT` only.
- No nested field updates (`PUT /component/Foo/Position/X` to set just one field) — full component replacement only.
- No authentication / authorization.

### Locked-in decisions

1. Pair URL encoding: `<rel-path>~<tgt-path>` (tilde separator).
2. Body size cap: 1 MB via `http.MaxBytesReader`; over-size → `413`.
3. `DELETE` on a component the entity does not hold: `200 OK` (idempotent), not `404`. Matches the upstream C behaviour of `ecs_remove_id` (silent no-op on absent component).
4. `PUT` body for a tag component: must be empty; non-empty body → `400`. Avoids the silent-discard ambiguity of "ignore body".

### Open question for the iterating agent (not blocking)

- Should the dynamic-component PUT accept a raw bytes body (with `Content-Type: application/octet-stream`) in addition to the JSON-string-of-base64 form? The base64 form is required for parity with `marshal.go:960` (snapshot round-trip), but raw bytes is friendlier for tooling. Recommended: ship JSON-string-of-base64 only in Phase 16.34; revisit if a user requests it.

## Constraints

- @rest.go — extend `NewRESTHandler` (lines 49–67), add `restPutComponent` and `restDeleteComponent` (mirror `restPutEntity` at lines 465–531 and `restDeleteEntity` at 533–567). Reuse the package-level `writeMu sync.Mutex` (line 63). JSON helpers `writeJSON` / `writeError` at lines 192–200. The handler's package-level docstring (lines 16–48) is the route catalogue — extend it with the two new routes.
- @value_ops.go — `SetByID(e, id, v any)` at line 230 is the typed write path used for data components and typed pairs; it handles defer-queue routing, type-mismatch panics, and bounce-buffer allocation. `setImmediateByPtr` at line 288 is the underlying primitive but is not called directly from REST.
- @scope.go — `Writer.AddID` (line 378) for tag PUTs; `Writer.RemoveID` (line 383) for DELETE; `Writer.SetByID` (line 424) for data PUTs; `Writer.SetPairByID` (line 451) for typed pairs; `Reader.HasID` (line 82) if a status check is needed before DELETE.
- @marshal.go — `unmarshalDynamic` at line 960 is the reference implementation for the base64-string contract used by dynamic components. Follow the same pattern: `json.Unmarshal` into a string, `base64.StdEncoding.DecodeString`, length check against `info.Size`, then `SetIDPtr`.
- @internal/component/registry.go — `Registry.LookupByID(id)` returns `(*TypeInfo, bool)`. `TypeInfo` definition at @internal/component/typeinfo.go lines 50–75: `Size`, `Align`, `Type reflect.Type` (nil for dynamic), `Component ids.ID`.
- @component_dynamic.go — `SetIDPtr(fw, e, componentID, src)` at line 109 is the raw-pointer write used for dynamic components.
- @rest_entity_test.go — Phase 16.33 test file. Mirror its structure for `rest_component_test.go`: `newComponentWorld` helper, per-test `httptest.Server`, `restDo` / `readBody` from `rest_test.go:58,75`, concurrent-mutation test pattern.
- @CONTRIBUTING.md — doc-update matrix at lines 67–72: changelog + ROADMAP entry + README row + `docs/` reference page (FlecsRemoteApi.md). Coverage target ≥ 90% on touched files (line 56); package convention is ≥ 95%.
- @docs/FlecsRemoteApi.md — the Phase 16.33 entity-mutation sections (lines 721–829) are the model for the new component-mutation sections; the existing gap callout at lines 865–874 must be updated from "Not yet ported" to "Partially ported in v0.89.0" with a forward link, matching the v0.88.0 update at lines 854–863. Path-separator divergence callout (lines 729–732, 801–803) is the model for the pair-encoding-divergence callout.
- @docs/README.md — line 90 is the canonical gap statement to update.
- @CHANGELOG.md — the v0.88.0 entry at lines 3–45 is the format model: API additions, semantics, test coverage sections.
- @ROADMAP.md — line 3 (`## Shipped (through v0.88.0)`) bumps to v0.89.0; new entry follows the Phase 16.33 line 91 format.
- C upstream parity reference (read-only, for divergence justification): `/work/agents/claude/projects/SanderMertens/flecs/src/addons/rest.c` lines 392–447 (`flecs_rest_put_component`) and 450–478 (`flecs_rest_delete_component`). Upstream encodes the component as a `?component=` query parameter and a `?value=` query parameter for the body, parsed via `flecs_id_parse` (`src/addons/query_dsl/parser.c:671`) and `ecs_ptr_from_json`. Go-flecs deliberately diverges: path-segment encoding (consistent with `DELETE /entity/{path...}` from Phase 16.33), JSON body (not query param), `~` for pairs (avoids URL-encoded parentheses). All three divergences must be documented in `FlecsRemoteApi.md`.
