## Goal

Consolidate three corrections to #81/#83 into a single phase before tagging v0.12.0. This is a feature-completion correction, not a bug fix. References #81, #83, and the closed #85 (an earlier, narrower take on the same correction; closed when the user clarified the goal is always-on, not just-tag-renamed).

User feedback on the post-#83 state:

> "There's no such thing as a 'debug' golang build. Lean on github.com/petermattis/goid to get a goroutine id number."
> "flecs_exclusive_access should always be enabled. Unlike C, Go makes threading a first-class feature, so the defense being on by default is preferred."

Three corrections:

1. **Remove the `flecs_exclusive_access` build tag entirely.** Make the check always-on.
2. **Replace `runtime.Stack` parsing with `github.com/petermattis/goid`.** Drops the per-call cost from ~µs to ~ns and uses a well-maintained linkname implementation (used by cockroachdb, etcd, etc.).
3. **Purge "debug build" / "release build" terminology** from CHANGELOG, README, doc.go. Go has no debug/release distinction; calling it that misleads readers.

### Rationale for always-on

In C, threading is opt-in (pthreads, etc.) and most flecs users are single-threaded; that's why the upstream uses `#ifdef FLECS_EXCLUSIVE_ACCESS`. In Go, goroutines are the default mode of writing concurrent code — every net/http handler, every channel-based pipeline, every benchmark `b.RunParallel` runs in multiple goroutines. The check should be on by default to catch misuse out of the box.

### Performance

The hot path is one `atomic.Uint64.Load()` and one compare:

```go
func (w *World) checkExclusiveAccessWrite() {
    owner := w.exclusiveAccess.Load()
    if owner == 0 { return }  // common case: nobody claimed; ~1 ns total
    if owner == ^uint64(0) { panic(...) }
    if owner != uint64(goid.Get()) { panic(...) }
}
```

When `exclusiveAccess == 0` (default, no `ExclusiveAccessBegin` called), cost is one atomic load. `goid.Get()` only runs when an owner is set. Expect <1% regression on `BenchmarkSetExistingComponent` for the un-claimed case; ~5% regression when a window is open. Both acceptable.

### Deliverables

1. **Add `github.com/petermattis/goid` to go.mod** (latest stable). Run `go mod tidy`.

2. **Rewrite `goid.go`** (no build tag now):

   ```go
   package flecs

   import "github.com/petermattis/goid"

   // currentGoid returns the calling goroutine ID. Always compiled in.
   func currentGoid() uint64 {
       return uint64(goid.Get())
   }
   ```

3. **Remove the build tag entirely**:
   - Delete `exclusive_access_on.go`, `exclusive_access_off.go` — the `flecsExclusiveAccess` const goes away.
   - Delete `exclusive_access_release.go` (the no-op stubs file).
   - Merge `exclusive_access_debug.go` into a single `exclusive_access.go` containing the real implementation, no `if !flecsExclusiveAccess { return }` guards. Functions are always live.
   - Delete `exclusive_access_release_test.go` (the "release build no-op" tests — no longer meaningful).
   - Keep `exclusive_access_test.go` but remove its `//go:build flecs_exclusive_access` constraint so it runs every test cycle.

4. **CI workflow (`.github/workflows/ci.yml`)**: remove the duplicate "Test (exclusive_access debug build)" and "Lint (exclusive_access debug build)" jobs — there's only one build now. Verify only one test job and one lint job remain, both run the unified suite.

5. **Terminology fixes**: replace every occurrence of "debug build" / "release build" / "debug mode" with accurate phrasing ("always-on", "exclusive-access check", "ownership assertion") in:
   - `CHANGELOG.md` (still-Unreleased v0.12.0 section) — rewrite the relevant entries.
   - `README.md` (Concurrency model section) — describe the feature as: "Every call to a public World method asserts that either no goroutine has claimed the world via `ExclusiveAccessBegin`, OR the calling goroutine is the claimant. Use `ExclusiveAccessBegin/End` to declare ownership; cross-goroutine misuse panics with `ErrExclusiveAccessViolation`."
   - `doc.go` — remove any "Debug builds" or "Build tags" section; replace with a "Concurrency safety" or similar section describing the always-on check.

6. **Tests**: keep all existing tests (the build-tagged ones lose their tag and run unconditionally). Add:
   - `TestExclusiveAccessZeroOverheadCommonPath`: benchmark or test asserting that with no `ExclusiveAccessBegin` ever called, a tight Set loop allocates 0 bytes and runs within 2% of the no-check baseline (you can compare to a pre-Phase-10.3 commit using `git stash`-equivalent or just check absolute numbers against the v0.11.0 BENCH.md baseline if recorded).
   - `TestGoidGetIsNonZero`: trivial sanity check on the petermattis/goid wiring.

7. **CHANGELOG**: rewrite the Unreleased v0.12.0 section as:
   - "Exclusive-access ownership assertion. Public World methods panic with `ErrExclusiveAccessViolation` if called from a goroutine other than the one that called `ExclusiveAccessBegin`. Always on; no build tag. Common case (no owner claimed) costs one atomic.Load per public call."
   - Drop the previous "debug build" wording entirely.

### Acceptance

- `go mod tidy` clean (`go.mod` lists petermattis/goid).
- `go test ./... -race -count=10` clean (single build, no `-tags`).
- `go vet ./... && golangci-lint run ./...` clean (single build).
- Coverage ≥ 95% (currently 95.7%; expect to hold).
- No file named `*_on.go`, `*_off.go`, `*_debug.go`, `*_release.go`, `*_enabled.go`, `*_disabled.go` related to exclusive access.
- No occurrence of "debug build", "release build", or "debug mode" (case-insensitive) in README.md, CHANGELOG.md, or doc.go.
- `BenchmarkSetExistingComponent` regression ≤ 2% vs v0.11.0 in the common case (no Begin called).
- No public API changes — `ExclusiveAccessBegin`, `ExclusiveAccessEnd`, `ErrExclusiveAccessViolation` all keep their existing signatures.

### Non-deliverables

- Behavioral change to `ExclusiveAccessBegin/End` semantics (still 0 / goid / ^uint64(0) states).
- Removing the feature (the check stays — only the build-tag gating goes away).
- Performance optimization beyond what petermattis/goid provides.

## Constraints

- @go.mod, @go.sum — add petermattis/goid
- @goid.go — rewrite without build tag
- @exclusive_access.go — single unified file (created by merging the _debug variant); remove others
- DELETE @exclusive_access_on.go
- DELETE @exclusive_access_off.go
- DELETE @exclusive_access_debug.go (merge into exclusive_access.go)
- DELETE @exclusive_access_release.go
- DELETE @exclusive_access_release_test.go
- @exclusive_access_test.go — drop build tag; add the two new tests
- @CHANGELOG.md — rewrite Unreleased section
- @README.md — terminology
- @doc.go — terminology
- @.github/workflows/ci.yml — collapse duplicate jobs

