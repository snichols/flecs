## Goal

Add Go-idiomatic `context.Context` cancellation to all blocking operations in flecs. The ECS port is functionally complete at v0.105.0; this is the first **post-port-completion** phase — a Go-idiomatic value-add beyond upstream C parity. Long-running operations (`Progress`, `TakeSnapshot`/`RestoreSnapshot`, `RunSystem`, query iteration, REST request handling) currently block without an interruption mechanism, forcing Go callers to abandon the goroutine or accept arbitrary latency on shutdown / client disconnect / timeout. This phase introduces a backward-compatible cancellation surface using the standard library `context.Context` idiom.

**Target version:** v0.106.0
**Phase number:** 16.51

### API additions (backward-compatible)

For each long-running blocking method, add a `...Context` variant. The existing non-Context variants remain and internally delegate to the Context variants with `context.Background()`.

- `(*World).ProgressContext(ctx context.Context, dt float32) error` — wraps `Progress(dt)`; checks ctx between systems and within long-running merges; returns `ctx.Err()` on cancel
- `(*World).TakeSnapshotContext(ctx context.Context) (*Snapshot, error)` — checks ctx between tables / per-N rows during walk
- `(*World).RestoreSnapshotContext(ctx context.Context, snap *Snapshot) error` — same checkpointing
- `RunSystemContext(ctx context.Context, sys *System, dt float32) error` — wraps `RunSystem`; check ctx between iter chunks
- `(*Query).EachContext(ctx context.Context, fn func(...)) error` — query iteration with cancellation
- `(*RESTHandler).ServeHTTPContext` — wraps existing `ServeHTTP`; uses request's context for ctx-aware operations

### Cancellation semantics

**Checkpoint frequency:**
- Every N=1024 iterations of inner loops (tune via benchmark; require ctx-check overhead < 1% of inner-loop time, else increase N)
- Between phases in `Progress`
- Between tables in iterations
- Between systems in a parallel batch (wait for current wave to complete, then check ctx)

**Cancellation policy:**
- **Mid-tick:** complete current parallel-batch wave, flush deferred mutations from that wave, then return `ctx.Err()`. Do NOT abandon mid-flush — that would leave deferred-but-not-applied mutations.
- **Mid-snapshot:** stop walking; recommended design (A) — return partial snapshot wrapped with a `Partial bool` flag so callers can detect.
- **Mid-restore:** recommended design (B) — return error; no usable partial state. Rollback to pre-restore state if feasible; otherwise document partial-restoration behavior.

**Error propagation:**
- `ctx.Err()` returned directly so callers can use `errors.Is(err, context.Canceled)` / `context.DeadlineExceeded`
- No new error types

### REST integration

Standard `http.Handler.ServeHTTP(w, r)` already passes `r.Context()`. The handler must:
- Pass `r.Context()` to the underlying world operations (`...Context` variants)
- On client disconnect (`r.Context().Done()` fires), abort the operation and write `499 Client Closed Request` (or `503` if `499` is undesirable)

Each endpoint at `@rest.go`, `@rest_query.go`, `@rest_component.go`, etc must be refactored to honor request context.

### Required tests

New file `@context_test.go` with the following coverage:

**Progress cancellation**
- `TestProgressContext_Background_NoCancellation` — `context.Background()` behaves identical to `Progress`
- `TestProgressContext_PreCanceled` — already-cancelled ctx returns `ctx.Err()` immediately, no systems run
- `TestProgressContext_CanceledMidTick` — cancel after 50ms; verify in-flight wave completes, then returns
- `TestProgressContext_TimeoutFires` — `context.WithTimeout(100ms)` on slow workload; returns deadline-exceeded
- `TestProgressContext_DeferredMutationsApplied` — cancellation does NOT lose deferred mutations from completed waves

**Snapshot cancellation**
- `TestTakeSnapshotContext_Background`
- `TestTakeSnapshotContext_PreCanceled`
- `TestTakeSnapshotContext_CanceledMidWalk` — partial state via `Partial` flag
- `TestRestoreSnapshotContext_Background`
- `TestRestoreSnapshotContext_CanceledMidRestore` — verify rollback or documented partial-state behavior

**Query iteration cancellation**
- `TestEachContext_PreCanceled`
- `TestEachContext_CanceledMidIteration` — completes current chunk, returns
- `TestEachContext_LargeWorld_Timeout` — 10k entities, 50ms timeout

**Per-system cancellation**
- `TestRunSystemContext_PreCanceled`
- `TestRunSystemContext_CanceledMidIter`

**REST integration**
- `TestRest_ClientDisconnect_AbortsProgress` — slow `/snapshot` GET; close client; verify server-side abort
- `TestRest_RequestTimeout_AbortsQuery` — slow query with client-side timeout; verify handler aborts

**Parallel system interaction**
- `TestProgressContext_ParallelSystemCancellation` — multi-system parallel batch; cancel mid-batch; current wave completes, batch aborts cleanly

**Stress**
- `TestContext_RepeatedCancellation_NoLeak` — 1000 cycles of start-cancel; verify no goroutine leak (goleak)

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run ./...` clean
- `go test ./... -race -count=3` clean
- Coverage >= 95% (current baseline)
- All existing non-Context tests pass unchanged (back-compat critical)
- `goleak` integration shows no leaks under cancellation stress

### Documentation update matrix

- New file `@docs/Cancellation.md` — full reference: supported operations, cancellation policy, partial-state handling for snapshot/restore, REST request-context wiring, examples
- `@docs/Systems.md` — note `ProgressContext` in pipeline section
- `@docs/Snapshots.md` — note `Take/RestoreSnapshotContext` and `Partial` flag
- `@docs/FlecsRemoteApi.md` — request-context behavior section
- `@docs/Queries.md` — `EachContext` for iteration cancellation
- `@CHANGELOG.md` — v0.106.0 entry; note this is the first post-port-completion phase
- `@ROADMAP.md` — add Phase 16.51 to Shipped
- `@README.md` — cancellation feature row

### Non-goals

- Removing non-Context variants — back-compat is required; they delegate to Context variants
- Cancellation of mutations inside a hook (would corrupt invariants)
- Cancellation of `OnAdd`/`OnSet`/`OnRemove` dispatch (atomic ops, not blocking)
- Forced abort with state corruption — cancellation is cooperative
- Real-time deadline guarantees — Go runtime doesn't provide them; cancellation is opportunistic

## Constraints

- @world.go — Progress, Take/RestoreSnapshot delegating entry points; add `ProgressContext`, `TakeSnapshotContext`, `RestoreSnapshotContext`. Existing non-Context methods must delegate to Context variants with `context.Background()`.
- @system.go — `RunSystem`, pipeline dispatch; add `RunSystemContext`; checkpoint between iter chunks and between parallel-batch waves. Wait for in-flight wave to complete + flush deferred mutations before returning `ctx.Err()`.
- @snapshot.go — Phase 16.24 binary snapshot; add ctx checkpoints between tables / per-N rows; surface `Partial bool` flag for mid-walk cancellation.
- @marshal.go — JSON snapshot path; honor ctx during marshal walk.
- @query.go — query iteration; add `EachContext`; check ctx every N=1024 inner iterations and between tables.
- @cached_query.go — cached query iteration; same checkpointing as `query.go`.
- @rest.go — REST handler entry point; refactor `ServeHTTP` to use `r.Context()` and call `...Context` variants; on client disconnect write `499 Client Closed Request`.
- @rest_query.go — refactor endpoint to honor request context.
- @rest_component.go — refactor endpoint to honor request context.
- @docs/Systems.md — existing pipeline doc; extend with `ProgressContext` section.
- @docs/Snapshots.md — existing snapshot doc; extend with `Take/RestoreSnapshotContext` and `Partial` flag semantics.
- @docs/Queries.md — existing query doc; extend with `EachContext`.
- @docs/FlecsRemoteApi.md — existing REST doc; extend with request-context behavior.
- @CHANGELOG.md — add v0.106.0 entry; mark as first post-port-completion phase.
- @ROADMAP.md — add Phase 16.51 to Shipped section.
- @README.md — add cancellation feature row.
- New file `@docs/Cancellation.md` — full cancellation reference (operations, policy, partial-state, REST wiring, examples).
- New file `@context_test.go` — all test cases enumerated above; uses `goleak` (or equivalent) for the leak test.
- Inner-loop checkpoint frequency: N=1024 starting point; verify via benchmark that ctx-check overhead is < 1% of inner-loop time; increase N if not.
- Cooperative cancellation only — never abandon mid-flush of deferred mutations; complete current wave first.
- `ctx.Err()` returned directly; no new error types; callers use `errors.Is` against `context.Canceled` / `context.DeadlineExceeded`.
