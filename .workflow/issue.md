Follow-up to #81. Phase 10.3 landed the exclusive-access machinery, but a code review identified instrumentation gaps that defeat the feature's purpose unless closed. Most critically, `Progress()` — the main game-loop tick — currently lacks the Write-check. Whatever else exclusive-access protects, it should protect this. Several other public entry points (mutators and readers) also take no check today.

This is a feature-completion follow-up, not a bug. The goal is to finish wiring exclusive-access into the public API surface before tagging v0.12.0.

## Goal

Close the instrumentation gaps in the exclusive-access feature so that all public mutators are Write-checked and all public readers are Read-checked, with CI lint coverage on both build tags and tests proving the new entry points respect ownership.

## Deliverables

### 1. Add `w.checkExclusiveAccessWrite()` at top of

- `(*World).Progress(dt float32)` — `system.go` around line 226 (main game-loop). This is the critical one.
- `(*World).RegisterComponent[T]` — wherever it lives (likely `world.go` or a `register.go`). Component registration mutates `w.registry` and `w.index`.
- `(*World).NewSystem` and `(*World).NewSystemInPhase` — `system.go`. Allocates system state into the world.
- `(*World).NewQuery` and `(*World).NewQueryFromTerms` — `query.go`.
- `(*World).NewCachedQuery` and `(*World).NewCachedQueryFromTerms` — `cached_query.go`.

### 2. Add `w.checkExclusiveAccessRead()` at top of

- `(*World).IsAlive(e ID)` — `world.go`.
- `(*World).Count()` — wherever the active-entity count lives.
- `(*World).SystemCount()` and `(*World).SystemCountInPhase(p ID)` — `system.go`.
- `(*World).TablesFor(id ID)` — `meta.go` or similar.
- `(*World).EachTableFor(...)` — same area.

### 3. CI

In `.github/workflows/ci.yml`, add `--build-tags flecs_exclusive_access` to the golangci-lint job (or duplicate the job). Run lint on BOTH builds, not just the default. The current ci.yml already has a `test-exclusive-access` test job — just add the matching lint coverage.

### 4. Test additions in `exclusive_access_test.go` (build-tagged)

- `TestExclusiveAccessProgressFromOtherGoroutinePanics`: Begin in goroutine A; call `Progress(0.016)` from B; expect panic with `ErrExclusiveAccessViolation` (use recover).
- `TestExclusiveAccessReadEntryPointsRespectOwnership`: Begin in A; from B call `IsAlive`, `Count`, `SystemCount`; each panics. Then `End()`; same calls from B succeed.

### 5. CHANGELOG.md

Amend the still-Unreleased v0.12.0 section to list:

- Added: `Progress` and `RegisterComponent` / `NewSystem*` / `NewQuery*` / `NewCachedQuery*` now Write-checked.
- Added: `IsAlive` / `Count` / `SystemCount*` / `TablesFor` / `EachTableFor` now Read-checked.
- Added: CI runs golangci-lint on both default and `flecs_exclusive_access` builds.
- Note: `goid()` uses `runtime.Stack` parsing rather than `//go:linkname` to `runtime.getg()`. The deviation from #81 spec is intentional — simpler, no `unsafe`, no fragile linkname. Costs ~µs per mutator in debug builds only; release builds are unaffected (the check function is a no-op).

## Non-deliverables (out of scope)

- Switching `goid()` to `//go:linkname`. Accepted as-is.
- Instrumenting `SetWorkerCount`, `SetFixedTimestep`, `SetLogger` — these are config-time, not hot path; the discipline target is the main-loop entry points.
- Instrumenting internal/private helpers — only public API.

## Acceptance

- `go test ./... -race -count=10` clean on default and debug builds.
- `go vet ./...` and `golangci-lint run` clean on BOTH builds (including the new CI job).
- Coverage ≥ 95% (already 95.7%).
- `BenchmarkProgress` (if it exists) within 1% of post-#81. Otherwise spot-check `BenchmarkSetExistingComponent`.
- No public API changes.
- Both new tests pass.

## Relevant files

- `world.go` — `IsAlive`, `Count`, `RegisterComponent[T]` (depending on placement)
- `system.go` — `Progress`, `NewSystem`, `NewSystemInPhase`, `SystemCount`, `SystemCountInPhase`
- `query.go` — `NewQuery`, `NewQueryFromTerms`
- `cached_query.go` — `NewCachedQuery`, `NewCachedQueryFromTerms`
- `meta.go` — `TablesFor`, `EachTableFor` (verify location)
- `exclusive_access_debug.go` — no changes needed; only call-sites
- `exclusive_access_test.go` — add two tests
- `.github/workflows/ci.yml` — add lint job for `flecs_exclusive_access` tag
- `CHANGELOG.md` — amend Unreleased v0.12.0 section

