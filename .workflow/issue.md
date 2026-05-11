## Context

Faithful port of the C flecs `FLECS_EXCLUSIVE_ACCESS` machinery (`src/world.h:175`, `src/world.c:1644-1709`, `include/flecs.h:2910`/`2937`). It's a debug-only safety net that catches "another goroutine touched the world while you owned it" violations — analog to the Go race detector but specifically for the single-mutator-thread discipline that the readonly mode (v0.11.0, #79) and Progress both rely on.

The C semantics use one `uint64_t` field with three states:
- `0` — feature inactive (no-op).
- Owning thread ID — only that thread can write; others abort with `ECS_ACCESS_VIOLATION`.
- `UINT64_MAX` — fully locked: writes abort, reads pass.

In C this is gated by `#ifdef FLECS_EXCLUSIVE_ACCESS` so it costs nothing in release builds.

## Go port

Use a Go build tag `flecs_exclusive_access` (default OFF) with two paired files exporting a `const flecsExclusiveAccess bool` value. Every check site reads `if flecsExclusiveAccess { ... }` so the compiler eliminates the call in release builds.

## Deliverables

1. **New field on `*World`**: `exclusiveAccess atomic.Uint64`. Always present (zero-valued field, no behavioral effect when tag is off).

2. **`exclusive_access_on.go` and `exclusive_access_off.go`** (paired build-tagged files):

   ```go
   // exclusive_access_on.go
   //go:build flecs_exclusive_access

   package flecs
   const flecsExclusiveAccess = true
   ```

   ```go
   // exclusive_access_off.go
   //go:build !flecs_exclusive_access

   package flecs
   const flecsExclusiveAccess = false
   ```

3. **`goid.go`** (build-tagged `//go:build flecs_exclusive_access`) — uses `//go:linkname` to access `runtime.getg().goid`. This is the standard Go community workaround (used by cockroachdb's util/goid, petermattis/goid). Stub a `goid()` function returning a `uint64` goroutine ID. Comment heavily: "fragile against Go runtime changes; that's why this is debug-only."

4. **Public API in `exclusive_access.go`** (always compiled):

   ```go
   // ExclusiveAccessBegin claims the world for the calling goroutine. In debug
   // builds (-tags flecs_exclusive_access), any mutation from another goroutine
   // panics with ErrExclusiveAccessViolation. In release builds this is a no-op.
   //
   // threadName is recorded for diagnostics in the panic message.
   func (w *World) ExclusiveAccessBegin(threadName string)

   // ExclusiveAccessEnd releases the world. If lockWorld is true, writes from
   // ALL goroutines panic in debug builds until another ExclusiveAccessBegin
   // call (reads still pass). If false, the world goes back to "unclaimed".
   func (w *World) ExclusiveAccessEnd(lockWorld bool)
   ```

   When `flecsExclusiveAccess == true`:
   - `Begin`: asserts current value is 0, then stores `goid()` and `threadName`.
   - `End(true)`: stores `^uint64(0)`.
   - `End(false)`: stores `0`.

   When `flecsExclusiveAccess == false`: both bodies are bare; the compiler dead-codes them.

5. **Check functions** (`exclusive_access.go`):

   ```go
   // checkExclusiveAccessWrite panics in debug builds if another goroutine
   // claimed the world via ExclusiveAccessBegin, or if the world is locked.
   //go:inline
   func (w *World) checkExclusiveAccessWrite() {
       if !flecsExclusiveAccess { return }
       owner := w.exclusiveAccess.Load()
       if owner == 0 { return }
       if owner == ^uint64(0) {
           panic(fmt.Errorf("flecs: exclusive_access violation: world is locked for writes"))
       }
       if owner != goid() {
           panic(fmt.Errorf("flecs: exclusive_access violation: this world is owned by another goroutine"))
       }
   }

   func (w *World) checkExclusiveAccessRead() {
       if !flecsExclusiveAccess { return }
       owner := w.exclusiveAccess.Load()
       if owner == 0 || owner == ^uint64(0) { return }
       if owner != goid() {
           panic(fmt.Errorf("flecs: exclusive_access violation: this world is owned by another goroutine (read)"))
       }
   }
   ```

   In release builds the function body collapses to `return` and the call site `w.checkExclusiveAccessWrite()` inlines to nothing.

6. **Call-site instrumentation**: insert `w.checkExclusiveAccessWrite()` at the top of every PUBLIC mutator (Delete, Set[T], Remove[T], AddID, RemoveID, SetByID, SetPair[T], SetName, plus pair/childof/isa/hook-register/observer-register). Insert `w.checkExclusiveAccessRead()` at the top of every PUBLIC reader (Get[T], GetByID, Has, Owns, HasID, GetName, Lookup, ParentOf, EachChild, Each1-4, Iter, NewQuery, etc.). The Readonly entry points themselves do NOT take the check — they're how callers transition into/out of locked mode.

7. **Tests** in `exclusive_access_test.go` — build-tagged `//go:build flecs_exclusive_access`:
   - `TestExclusiveAccessOwnerCanWrite`: Begin in goroutine A, write succeeds. End. No panics.
   - `TestExclusiveAccessOtherGoroutineWritePanics`: Begin in A, goroutine B's Set call panics with ErrExclusiveAccessViolation. (Use recover() in the test goroutine.)
   - `TestExclusiveAccessLockedWorldRejectsWrites`: Begin, End(lockWorld=true). Any goroutine's Set panics; Get succeeds.
   - `TestExclusiveAccessUnsetIsNoop`: Without Begin, any goroutine can read/write freely.
   - `TestExclusiveAccessNestedBeginPanics`: Begin twice from same goroutine asserts.
   - All run under `go test -tags flecs_exclusive_access -race -count=10`.

8. **Tests in regular test suite** (no tag) — `exclusive_access_release_test.go`:
   - `TestExclusiveAccessReleaseBuildNoop`: in release builds, Begin/End succeed silently; cross-goroutine writes do NOT panic. Proves the no-op path.

9. **CI matrix** (`.github/workflows/ci.yml` if exists, otherwise note in CHANGELOG): add a job that runs `go test -tags flecs_exclusive_access -race ./...` so the debug build doesn't bitrot.

10. **doc.go**: new "Debug builds" section explaining the `-tags flecs_exclusive_access` build, what it catches, and that it is debug-only with non-zero overhead per mutator call (one atomic.Load).

11. **CHANGELOG.md** Unreleased section + README brief mention.

## Acceptance

- `go test ./...` (default tag-off build) passes clean under `-race -count=10`. Coverage ≥ 95%.
- `go test -tags flecs_exclusive_access ./...` passes clean under `-race -count=10`. Coverage measurement may differ; not enforced.
- `go vet ./...` and `golangci-lint run` clean on BOTH builds.
- `BenchmarkSetExistingComponent` in the default build: within 1% of v0.11.0 (the `if flecsExclusiveAccess` check is a compile-time constant false in release, eliminated).
- No public API changes besides `ExclusiveAccessBegin` / `ExclusiveAccessEnd`.

## Non-goals

- Cross-process / shared-memory protection.
- Production-grade race detection (use `-race`).
- Mandatory adoption: existing callers can ignore this entirely.

## Relevant files

- @world.go — add `exclusiveAccess atomic.Uint64` field
- @exclusive_access.go (NEW) — public API + check functions
- @exclusive_access_on.go (NEW, build-tagged) — `const flecsExclusiveAccess = true`
- @exclusive_access_off.go (NEW, build-tagged) — `const flecsExclusiveAccess = false`
- @goid.go (NEW, build-tagged) — `runtime.getg().goid` via `//go:linkname`
- @exclusive_access_test.go (NEW, build-tagged) — debug-build tests
- @exclusive_access_release_test.go (NEW) — release-build no-op tests
- @id_ops.go — call-site instrumentation
- @value_ops.go — call-site instrumentation
- @name.go — call-site instrumentation
- @childof.go — call-site instrumentation
- @isa.go — call-site instrumentation
- @hooks.go — call-site instrumentation (Hook registration is a write)
- @observer.go — call-site instrumentation (Observer registration is a write)
- @each.go — call-site instrumentation (Each is a read)
- @query.go — call-site instrumentation (NewQuery, Iter are reads)
- @cached_query.go — call-site instrumentation
- @meta.go — call-site instrumentation (introspection is a read)
- @stats.go — call-site instrumentation (Stats is a read)
- @traversal.go — call-site instrumentation (GetUp etc. are reads)
- @readonly.go — does NOT take the check; it's the transition mechanism
- @doc.go — Debug builds section
- @CHANGELOG.md — Unreleased
- @README.md — brief mention

