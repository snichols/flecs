## Goal

Continue the docs port begun in Phases 14.0–14.8 (v0.19.0–v0.27.0). This phase ports **FlecsRemoteApi** — the reference on the HTTP-based REST API for world inspection and remote tooling.

Upstream: `/work/agents/claude/projects/SanderMertens/flecs/docs/FlecsRemoteApi.md` (~4900 words, classified `port-with-gaps`, effort: medium).

**Target version:** v0.28.0.

### Critical context — what we have vs C

Implemented in Go flecs (Phase 9.6, v0.x):
- `flecs.NewRESTHandler(w *World) http.Handler` — stdlib `net/http` only; users mount on their own server.
- Endpoints exposed: world stats, component listing, entity listing, snapshot save/load.
- Read endpoints wrap in `world.Read`; write endpoints (snapshot load) wrap in `world.Write` (Phase 12.0+).

NOT implemented in Go flecs:
- Full **FlecsExplorer** (web UI) integration — we have the raw API but no browser frontend.
- Query-execution endpoints (run an arbitrary query via HTTP and get results).
- Entity-creation / mutation endpoints beyond snapshot load.
- WebSocket / streaming endpoints.
- Authentication / authorization.
- Custom HTTP routing beyond the stdlib mount.

### Deliverables

1. **Full port of `docs/FlecsRemoteApi.md`** adapted to Go:
   - Lead with the simplest usage: `http.Handle(\"/flecs/\", flecs.NewRESTHandler(w))`, point at `http://localhost:8080/flecs/`, list available endpoints.
   - Document each endpoint accurately. Read @rest.go to verify the actual paths and response shapes; don't paraphrase C behavior.
   - Document the world.Read/world.Write integration (Phase 12.0): the handler is goroutine-safe and respects ExclusiveAccess.
   - For unimplemented endpoints (query exec, mutation, websocket, auth, explorer UI): use \"Not yet ported in Go flecs\" callouts.
   - Show a small Go example combining `NewRESTHandler` with `http.ServeMux` and a custom `http.Server`.

2. **Verify code blocks.** Create `docs/rest_examples_test.go` with `TestRest_*` functions using `httptest` package to spin up the handler and verify responses. Standard Go pattern.

3. **Update `docs/README.md`**: FlecsRemoteApi row → `✅ landed / 14.9`. Append discovered gaps. Likely 4-6 new gaps (query-exec endpoint, mutation endpoints, websocket, explorer UI, auth, custom routing).

4. **Update `ROADMAP.md`**: 14.9 row → `✅ shipped (v0.28.0)`. Do NOT bump the heading.

5. **Update `CHANGELOG.md`** with `Unreleased — Phase 14.9: FlecsRemoteApi doc port (upcoming v0.28.0)`.

6. **Cross-link** with Quickstart (REST handler usage), Systems (Stats() is exposed via REST), and ComponentTraits (component listing returns trait info).

### Non-goals

- No source changes.
- No porting beyond FlecsRemoteApi.
- If you find inaccuracies in `rest.go`'s actual behavior vs what we document, file a separate bug issue rather than fix inline.

### Mechanical acceptance

- `go test ./docs/...` passes (including the new httptest-based tests).
- `go vet`, `golangci-lint` clean.
- `go test ./... -race -count=1` passes.
- `docs/README.md` shows FlecsRemoteApi as landed.
- All documented endpoints + response shapes match `rest.go` reality (verify by code-reading + the new tests).

### Style notes

- Native Go. Use stdlib `net/http` patterns throughout.
- Show curl examples *and* Go client examples (`http.Get` etc).
- Be explicit about request/response JSON shapes — show actual bytes.
- For unimplemented endpoints, the callout pattern should explain *what* the C endpoint does and *why* the Go port doesn't (yet) have it.

## Constraints

- @CONTRIBUTING.md — governs the Documentation policy this port follows; code blocks must compile against v0.27.0.
- @docs/FlecsRemoteApi.md — current stub to replace with the full port.
- @docs/Quickstart.md — REST section is the cross-link target and tone reference.
- @docs/Systems.md — `Stats()` is exposed via REST; cross-link target and tone reference.
- @docs/ComponentTraits.md — component listing returns trait info; cross-link target and tone reference.
- @docs/README.md — landed/queued status table; update FlecsRemoteApi row and append discovered gaps.
- @docs/observers_examples_test.go — recent test pattern for `docs/*_examples_test.go` files.
- @rest.go — authoritative source for endpoint paths and response shapes; read end to end.
- @rest_test.go — existing endpoint tests; reference for httptest pattern in the new examples test.
- @stats.go — Stats response shape exposed via REST; verify documented JSON matches.
- @doc.go — package-level doc; update if REST surface description needs a touch-up.
- @README.md — top-level README; update if it mentions the REST handler.
- @ROADMAP.md — mark 14.9 row `✅ shipped (v0.28.0)`; do NOT bump the heading.
- @CHANGELOG.md — add `Unreleased — Phase 14.9: FlecsRemoteApi doc port (upcoming v0.28.0)` entry.
- Upstream reference: `/work/agents/claude/projects/SanderMertens/flecs/docs/FlecsRemoteApi.md`.
