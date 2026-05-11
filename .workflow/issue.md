Follow-up correction to #81 / #83 before tagging v0.12.0. Not a bug — a terminology and implementation correction while the v0.12.0 section is still Unreleased.

## Motivation

Two issues with the post-#83 state:

1. **`goid.go` parses `runtime.Stack()` output.** That's slow (~µs per call) and fail-open (returns 0 on parse error). The right choice is [`github.com/petermattis/goid`](https://github.com/petermattis/goid) — a well-maintained community package using `//go:linkname` to `runtime.curg().goid`. Used by cockroachdb, etcd, and others. Drops the per-call cost to ~1-2 ns.

2. **The CHANGELOG / README / doc.go call this a "debug build" feature.** Go has no debug/release build distinction. The `flecs_exclusive_access` build tag is just an opt-in build tag with a small runtime cost. The terminology should not imply a Go-language feature that doesn't exist.

## Deliverables

### 1. Add `github.com/petermattis/goid` to `go.mod`

Recent stable version — implementer picks latest. Run `go mod tidy`.

### 2. Rewrite `goid.go`

Build-tagged `//go:build flecs_exclusive_access`:

```go
//go:build flecs_exclusive_access

package flecs

import "github.com/petermattis/goid"

// goidGet returns the current goroutine ID. Used by the exclusive-access
// assertion to detect cross-goroutine misuse. Only compiled when the
// flecs_exclusive_access build tag is set.
func goidGet() uint64 {
    return uint64(goid.Get())
}
```

(Use whatever function name the existing code uses. Replace the `runtime.Stack`-parsing body wholesale.)

### 3. Rename build-tagged files (`_debug` / `_release` → `_enabled` / `_disabled`)

- `exclusive_access_debug.go` → `exclusive_access_enabled.go`
- `exclusive_access_release.go` → `exclusive_access_disabled.go`
- `exclusive_access_release_test.go` → `exclusive_access_disabled_test.go`
- Keep `exclusive_access_on.go` / `exclusive_access_off.go` (fine as-is).

### 4. Terminology fixes

- **`CHANGELOG.md`** (still-Unreleased v0.12.0 section): replace every occurrence of "debug build" / "release build" with "build with `-tags flecs_exclusive_access`" / "build without `-tags flecs_exclusive_access`". Include explicit note: *"Go has no built-in debug/release distinction; this is an opt-in build tag that enables an additional runtime check at a small per-call cost."*
- **`README.md`**: describe the feature as "opt-in via the `flecs_exclusive_access` build tag" rather than "debug build". One paragraph in the Concurrency model section.
- **`doc.go`**: same. Replace any "Debug builds" header with "Build tags" or "Opt-in checks". Update the prose.

### 5. CI workflow (`.github/workflows/ci.yml`)

Rename the test job's display `name:` from "Test (exclusive_access debug build)" to "Test (with flecs_exclusive_access tag)". Rename the lint job similarly. YAML `name:` field only — no behavior change.

### 6. Tests

Keep all existing tests in place. Add one test (in `exclusive_access_test.go`, build-tagged enabled) that asserts `goidGet()` is non-zero in any goroutine — a basic sanity check that the petermattis/goid wiring works.

## Acceptance

- `go mod tidy` clean (no stray deps).
- `go test ./... -race -count=3` clean both with and without `-tags flecs_exclusive_access`.
- `go vet ./...` + `golangci-lint run ./...` clean both builds.
- Coverage ≥ 95% (currently 95.7%; should be unchanged).
- No file named `*_debug.go` or `*_release.go` remains in the package.
- No occurrence of the phrase "debug build" or "release build" in README.md, CHANGELOG.md, or doc.go (case-insensitive).
- No public API changes.

## Non-deliverables

- Removing the build tag entirely (keeping the opt-in shape; that's a separate design decision).
- Switching to a non-linkname approach in the petermattis/goid package itself (that's their concern).

## Relevant files

- `goid.go` — rewrite body to use petermattis/goid
- `go.mod` / `go.sum` — new dep
- `exclusive_access_debug.go` → rename → `exclusive_access_enabled.go`
- `exclusive_access_release.go` → rename → `exclusive_access_disabled.go`
- `exclusive_access_release_test.go` → rename → `exclusive_access_disabled_test.go`
- `exclusive_access_on.go` (keep)
- `exclusive_access_off.go` (keep)
- `exclusive_access_test.go` — add sanity test for `goidGet`
- `CHANGELOG.md` — terminology
- `README.md` — terminology
- `doc.go` — terminology
- `.github/workflows/ci.yml` — job display names

