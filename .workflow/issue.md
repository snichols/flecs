## Goal

**Phase 16.3: System lifecycle bundle — disabling, single-Run, pipeline introspection (v0.58.0).**

Three small, independent system-side features that all touch the system registry / pipeline executor. Each is small enough to be too small for its own phase; bundling drains three gap-list entries with one shared CI cost. The bundle is enabled by the architecture shipped in v0.57.0 (Phase 16.2): per-table `Disabled` exclusion is already wired, so system-disabling can ride on the same machinery without new storage.

Gap entries from `docs/README.md` Phase 14.6 Systems section:
- Line 143: **System disabling** (`ecs_enable` / `EcsDisabled`) — pause a system without removing it; re-enable later.
- Line 145: **Single-system `Run` out-of-pipeline** — synchronous invocation of one system outside the normal pipeline (port of C `ecs_run`).
- Line 147: **Pipeline introspection** — iterate the ordered system list in a pipeline to inspect execution order at runtime.

All three flip to ✅ shipped (v0.58.0).

### Critical divergence to resolve: Go systems are not entities

In C flecs, a system **is** an entity carrying the `EcsSystem` component, so `ecs_enable(world, sys, false)` reuses `ecs_add_id(sys, EcsDisabled)`. The pipeline build query then filters them out with `{ EcsDisabled, .oper = EcsNot }` (`src/addons/pipeline/pipeline.c:724`).

In Go flecs, a system is a bare `*System` struct in `w.systems []*System` (`system.go:30-40`, `world.go:51`). It carries no `ID`. The Phase 16.2 API `DisableEntity(fw *Writer, e ID)` operates on entity IDs (`query_filters.go:54`), so it cannot be applied to a `*System` handle directly.

Two design options for the iterate agent to pick from (the design agent recommends Option A; pick one and document the choice in the v0.58.0 CHANGELOG):

- **Option A (smaller, idiomatic Go):** add `(*System).SetEnabled(bool)` / `(*System).IsEnabled() bool` as bool methods on the `*System` struct itself. The pipeline executor checks `s.enabled` (a new bool field, default true) in `runPhase` (`system.go:268`). Zero coupling to the entity-side Disabled tag.
- **Option B (closer port):** allocate an `ID` per system at `NewSystem` time, store it in `*System.entityID`, and reuse `DisableEntity` / `IsDisabled` on the system entity. The pipeline check becomes `IsDisabled(s, sys.entityID)`. This is more faithful to C but pays an entity-allocation cost per system.

Option A is recommended because (a) it keeps the bundle minimal and self-contained, (b) the pipeline doesn't need to call into the table-level `HasComponent` path for every system every frame, and (c) it preserves a single-source-of-truth (`s.enabled`) without spreading system state across both the struct and the entity index.

The rest of this issue assumes Option A. If the iterate agent picks Option B, adapt the symbol names but keep the same observable behavior.

### What each feature does

**System disabling**
- `(*System).SetEnabled(v bool)` — set the enabled flag; idempotent.
- `(*System).IsEnabled() bool` — query state.
- Pipeline executor in `runPhase` (`system.go:268`) gains one extra predicate alongside `!s.removed`: `if !s.removed && s.enabled && s.phase == p`. O(1) per system per phase, matching the per-table O(1) cost of the v0.57.0 query-side Disabled exclusion.
- No log line on skip (would be noisy at frame rate).

**Single-system Run**
- `RunSystem(s *System, dt float32)` — synchronously invoke one system once, outside the normal pipeline. Bypasses pipeline phase ordering, parallel batching, multi-threaded splitting, and the disabled check. Mirrors C `ecs_run` (`src/addons/system/system.c:180`).
- Opens its own deferred-mutation scope via `w.deferScope` (`world.go:1541`), matching C's `flecs_defer_begin` / `flecs_defer_end` wrap.
- The system's own write set / query semantics still apply (it gets a fresh `*QueryIter` exactly like a pipeline invocation).

**Pipeline introspection**
- `(*Reader).Phases() []ID` — returns `[PreUpdate, OnFixedUpdate, OnUpdate, PostUpdate]` (the four built-in phase IDs in execution order).
- `(*Reader).SystemsInPhase(phase ID) []*System` — returns the ordered system list for a phase, in registration order (matching the documented semantic on `docs/README.md:142` and the existing in-phase ordering at `system.go:268-271`). Returns a snapshot slice; concurrent mutation is not reflected.
- `(*Reader).EachSystem(phase ID, fn func(s *System) bool)` — zero-alloc callback variant for the perf-critical case.
- Includes disabled systems (introspection sees all registered systems; the executor filters at run time).
- All read-only; calls don't mutate world state.

### C research citations

- `ecs_enable` definition — `src/entity.c:3286-3316`. Just calls `ecs_add_id(world, entity, EcsDisabled)` / `ecs_remove_id`.
- Pipeline skip site — `src/addons/pipeline/pipeline.c:724-725` and `:1018-1019`. The pipeline build query has `{ EcsDisabled, .oper = EcsNot }` terms with `EcsUp` traversal on both `EcsDependsOn` and `EcsChildOf`. Disabled systems are filtered at pipeline build time, not at dispatch — but Go's flat `w.systems` slice has no equivalent "pipeline rebuild" step, so checking in `runPhase` is the natural port.
- `ecs_run` definition — `src/addons/system/system.c:180-194`. Wraps `flecs_run_system` in `flecs_defer_begin` / `flecs_defer_end`. **Does not check `EcsDisabled`** — explicit invocation overrides. This confirms the recommendation that `RunSystem` on a "disabled" system runs anyway.
- `flecs_run_system` — `src/addons/system/system.c:22-156`. Builds the iter, calls the system's action with `delta_time`, then returns.
- Pipeline introspection — upstream exposes the ordered system list via `ecs_pipeline_stats_t.systems` (`include/flecs/addons/stats.h:207-223`, `:210-212`: "Vector with system IDs of all systems in the pipeline. The systems are stored in the order they are executed."). The Go port discovers the order natively from `w.systems` since there's no separate pipeline cache.

### Open decisions (recommendations to record in CHANGELOG)

1. **`RunSystem` on a disabled system**: runs anyway. Matches C `ecs_run`; explicit invocation overrides the pipeline-disabled state.
2. **`RunSystem` scope**: opens its own implicit `w.deferScope`. Matches C `ecs_run`'s `flecs_defer_begin`/`flecs_defer_end` wrap. Callable from outside any other scope.
3. **Introspection signature**: ships both `SystemsInPhase` (returns `[]*System`, copy-safe) and `EachSystem` (callback, zero-alloc). The common case gets the slice; the hot path gets the callback.
4. **Snapshot semantics**: `SystemsInPhase` / `EachSystem` return a snapshot taken at call time. Systems added during iteration are not reflected. Documented behavior.
5. **Naming**: `SetEnabled` / `IsEnabled` on `*System`, not `Enable` / `Disable` (avoids collision with the CanToggle generic `Enable[T]` / `Disable[T]` already shipped in v0.35.0).

### Deliverables

1. **System disabling integration**
   - Add `enabled bool` field to `*System` (default `true` at construction in `NewSystem` `system.go:82` and `NewSystemInPhase` `system.go:126`).
   - `(*System).SetEnabled(v bool)` / `(*System).IsEnabled() bool`.
   - In `runPhase` at `system.go:268`, extend the active filter from `!s.removed && s.phase == p` to `!s.removed && s.enabled && s.phase == p`. Same change needed in `countPhase` at `system.go:382`. Also update `SystemCount` / `SystemCountInPhase` (`system.go:436`, `stats.go:99`) — leave them counting all non-closed systems regardless of enabled state, OR add an `ActiveSystemCount` variant; iterate-agent's call (recommend keeping existing functions counting all non-closed, as the test expects `SystemsInPhase` to list disabled systems too).
2. **`RunSystem(s *System, dt float32)`** — extend `system.go` or new `run_system.go`
   - Open `s.w.deferScope(func() { s.fn(dt, s.query.Iter()) })`.
   - No active-set check; no disabled check; no parallel/multi-threaded path. Single synchronous call.
   - Panics if `s == nil` or `s.removed`.
3. **Pipeline introspection** in `scope.go` (next to `SystemCount` / `SystemCountInPhase` at lines 273-287)
   - `(*Reader).Phases() []ID` returning `[w.preUpdateID, w.onFixedUpdateID, w.onUpdateID, w.postUpdateID]`.
   - `(*Reader).SystemsInPhase(phase ID) []*System` — iterate `w.systems`, append non-closed systems whose `s.phase == phase` in slice order. Panic if `phase` isn't one of the four built-ins (matching `SystemCountInPhase`).
   - `(*Reader).EachSystem(phase ID, fn func(*System) bool)` — same iteration, callback returning `false` halts.
4. **Tests in `system_lifecycle_test.go`** — at least 10 cases (the brief asked for 8; add two more for completeness):
   - Register two systems in OnUpdate. Disable one via `SetEnabled(false)`. Step `Progress`. Only the enabled system runs.
   - Re-enable the disabled system. Step again. Both run.
   - `IsEnabled()` returns the correct state across `SetEnabled(false)` / `SetEnabled(true)` cycles.
   - `SetEnabled(false)` mid-Progress (called from one system's callback affecting another) — document the deferred-snapshot semantics that already apply to `Close` (`system.go:206-223`): the disabled flag flips immediately but the active set was already captured.
   - `RunSystem` invokes the system once with the given dt; internal state changes (e.g., counter incremented) are visible after the call.
   - `RunSystem` on a disabled system runs it anyway.
   - `RunSystem` mutations are flushed (because of the wrapping `deferScope`) before the call returns.
   - `Phases()` returns `[PreUpdate, OnFixedUpdate, OnUpdate, PostUpdate]` in that order.
   - `SystemsInPhase(OnUpdate)` returns systems in registration order.
   - `SystemsInPhase` for a phase with no systems returns an empty slice (not nil).
   - After registering N systems and disabling M, `SystemsInPhase` still lists all N.
   - `EachSystem` halts when the callback returns `false`.
   - `SystemsInPhase` panics on a non-built-in phase ID (matches `SystemCountInPhase`).
   - Coverage ≥ 95.0%.
5. **Doc updates per CONTRIBUTING.md**
   - `docs/Systems.md` — add three sections after the existing "System Lifecycle" section at line 156: **Disabling a system**, **Single-system Run (out-of-pipeline)**, **Pipeline introspection**. Each with a small compilable code example tied into `docs/systems_examples_test.go`.
   - `docs/README.md` — flip lines 143, 145, 147 to ✅ shipped (v0.58.0) with anchor links into Systems.md.
   - `README.md` — feature-list bump.
   - `CHANGELOG.md` — v0.58.0 entry at top with the recommendation-decisions documented and the Option-A-vs-B rationale captured.
   - `ROADMAP.md` — heading bump to "through v0.58.0"; add Phase 16.3 shipped entry between lines 61 and the Future Work section. Bump the OnTableEmpty/OnTableFill candidate at line 96 from "Phase 16.3 candidate" to "Phase 16.4 candidate" (and renumber the chain at lines 97-103 accordingly: Custom events 16.5, Multi-term observers 16.5, Yield-on-create 16.6, Observer propagation 16.7, Monitor observers 16.8, Observer disabling 16.9, Fixed-source query terms 16.10). Or leave them un-renumbered if the iterate agent prefers — but the line-96 "Phase 16.3 candidate" cannot stand as written once this bundle ships as 16.3.

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%
- No regression in `system_test.go`, `system_parallel_test.go`, `pipeline_test.go`, or `parallel_test.go`.

### Non-goals (explicit, separate phases)

- No custom pipeline phases (gap line 141; separate phase).
- No `DependsOn` ordering between systems (gap line 142; separate phase).
- No rate filters / `SetInterval` / `SetRate` (gap line 144; separate phase).
- No `RunWorker` / explicit thread dispatch (gap line 146; separate phase).
- No observer disabling (gap line 159; relies on observer-side machinery, separate phase).
- No promotion of `*System` to a first-class entity (Option B above is explicitly out-of-scope for v0.58.0; revisit if Custom Pipeline Phases is taken on later, since that phase will need system entities anyway).

## Constraints

- @docs/README.md — gap-list source of truth; lines 143, 145, 147 flip to ✅ shipped (v0.58.0). Line 142 documents registration-order semantics that `SystemsInPhase` must match.
- @CONTRIBUTING.md — every user-visible change must update docs; line 60 directive.
- @ROADMAP.md — line 96 (OnTableEmpty/OnTableFill at "Phase 16.3 candidate") collides with this bundle's intended phase number; must be bumped.
- @CHANGELOG.md — v0.57.0 entry at line 3 sets the format precedent (date, phase, bundle summary, then per-API bullets).
- @system.go — `*System` struct (line 30), `NewSystem` (line 55), `NewSystemInPhase` (line 96), `runPhase` (line 264-378), `countPhase` (line 380-388), `Progress` (line 254-433). All disabling and RunSystem changes touch this file.
- @world.go — `w.systems` slice (line 51), built-in phase IDs (lines 55-58), `deferScope` (line 1541), `inProgress` (line 124). `RunSystem` uses `deferScope`.
- @query_filters.go — Phase 16.2 reference for the entity-side `Disabled` tag (lines 54-77). System-side disabling does NOT reuse this directly under Option A; the issue must state that clearly.
- @scope.go — `(*Reader).SystemCount` (line 273) and `(*Reader).SystemCountInPhase` (line 284) are the existing precedent for `Phases` / `SystemsInPhase` / `EachSystem`.
- @stats.go — `SystemCountInPhase` (line 96) panics on non-built-in phase IDs; new introspection functions match this contract.
- @docs/Systems.md — existing "System Lifecycle" section at line 156 is where the three new sections land.
- @/work/agents/claude/projects/SanderMertens/flecs/src/entity.c — `ecs_enable` (line 3286-3316) confirms upstream just toggles `EcsDisabled`.
- @/work/agents/claude/projects/SanderMertens/flecs/src/addons/system/system.c — `flecs_run_system` (line 22-156) and `ecs_run` (line 180-194) confirm upstream wraps in `flecs_defer_begin` / `flecs_defer_end`, does not check `EcsDisabled`.
- @/work/agents/claude/projects/SanderMertens/flecs/src/addons/pipeline/pipeline.c — disabled-system filter at lines 724-725 and 1018-1019 (build-time `Not EcsDisabled` term in the pipeline query).
- @/work/agents/claude/projects/SanderMertens/flecs/include/flecs/addons/stats.h — `ecs_pipeline_stats_t.systems` (lines 207-223) documents upstream's "stored in the order they are executed" guarantee that `SystemsInPhase` matches.

