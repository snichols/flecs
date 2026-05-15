## Goal

Add a new `flectest` subpackage (`github.com/snichols/flecs/flectest`) providing `testing.TB`-aware helpers that make ECS code easy to test. ECS state is graph-structured and awkward to assert on with bare `if got != want`; this package supplies idiomatic, richly-messaged assertions plus lifecycle and golden-snapshot helpers. This is the fifth post-port-completion Go-idiomatic value-add (Phase 16.54 / v0.109.0 was the last shipped phase).

**Package shape**
- New directory `@flectest/`, package `flectest`, import path `github.com/snichols/flecs/flectest`.
- Subpackage of the **same** Go module (no separate `go.mod`).
- Imports **only** the root `flecs` package + stdlib (`testing`, `flag`, `reflect`, `encoding/json`, `os`, `path/filepath`, `sort`, `bytes`). Verify with `go list -deps ./flectest`.
- Rationale: keeps `testing`/`flag` out of the root package's production import graph; mirrors the stdlib `net/http/httptest` and `testing/fstest` pattern; users import it only from `_test.go` files.

### Scope-parameter decision (resolved during exploration — must follow)

The root `scope` interface is **unexported** (`@scope.go:46` — `type scope interface { scopeWorld() *World }`, satisfied by `*Reader`/`*Writer`). External packages cannot name it or implement it, so `flectest` helpers **cannot** take a `scope` parameter and **cannot** define a generic constraint over it.

**Decision: all assertion helpers take `*flecs.World` and open a `Read` scope internally** via `w.Read(func(fr *flecs.Reader){ ... })` (verified `func (w *World) Read(fn func(*Reader))` at `@world.go:2211`). This is the most ergonomic signature for test code (no scope plumbing at call sites) and sidesteps the unexported-interface problem entirely. Document this rationale in `@docs/Testing.md`. Do NOT attempt `*Reader`/`*Writer` overloads or a re-exported constraint.

### Lifecycle helpers

```go
func NewWorld(tb testing.TB) *flecs.World
func NewWorldWith(tb testing.TB, setup func(fw *flecs.Writer)) *flecs.World
```

- `NewWorld`: calls `tb.Helper()`, `flecs.New()` (verified `@world.go:317`), registers `tb.Cleanup(...)`. **There is no `World.Close`/`Free`/`Destroy` and no finalizer** (verified — none in the codebase). The cleanup closure is therefore a documented **no-op placeholder for forward-compat**; document that GC reclaims the world. Still register the `tb.Cleanup` so the contract is stable if teardown is added later.
- `NewWorldWith`: as above, then runs `setup` inside `w.Write(setup)` before returning.

### Assertion helpers (all `tb.Helper()` + `tb.Fatalf` with got/want in the message)

```go
func AssertAlive(tb testing.TB, w *flecs.World, e flecs.ID)
func AssertNotAlive(tb testing.TB, w *flecs.World, e flecs.ID)
func AssertHasComponent[T any](tb testing.TB, w *flecs.World, e flecs.ID)
func AssertNoComponent[T any](tb testing.TB, w *flecs.World, e flecs.ID)
func AssertComponentValue[T comparable](tb testing.TB, w *flecs.World, e flecs.ID, want T)
func AssertComponentValueFunc[T any](tb testing.TB, w *flecs.World, e flecs.ID, check func(T) bool, desc string)
func AssertEntityCount(tb testing.TB, w *flecs.World, want int)        // total alive
func AssertQueryCount(tb testing.TB, w *flecs.World, q *flecs.Query, want int) // matched entities
func AssertParent(tb testing.TB, w *flecs.World, child, wantParent flecs.ID)
func AssertNoParent(tb testing.TB, w *flecs.World, child flecs.ID)
func AssertChildren(tb testing.TB, w *flecs.World, parent flecs.ID, wantChildren ...flecs.ID) // set equality, order-independent
func AssertHasPair(tb testing.TB, w *flecs.World, e, rel, target flecs.ID)
func AssertName(tb testing.TB, w *flecs.World, e flecs.ID, wantName string)
func AssertTag(tb testing.TB, w *flecs.World, e, tag flecs.ID)
```

Implementation grounding (verified APIs):
- `AssertAlive`/`AssertNotAlive` → `fr.IsAlive(e)` (`@scope.go:56`).
- `AssertHasComponent[T]`/`AssertNoComponent[T]` → `flecs.Has[T](fr, e)` (`@scope.go:516`, auto-registers T — fine for tests).
- `AssertComponentValue[T]` → `flecs.Get[T](fr, e)` (`@scope.go:503`); on mismatch `Fatalf` must include both `got` and `want` (use `%+v`).
- `AssertComponentValueFunc[T]` → `Get[T]`, then `check`; message includes `desc` and the actual value.
- `AssertEntityCount` → `fr.Count()` (`@world.go:1373`); message: got N alive, want M.
- `AssertQueryCount` → drive `q.Iter()`; sum `it.Count()` per `it.Next()` (verified `@query.go:1544/1936/2504`); message includes got/want.
- `AssertParent`/`AssertNoParent` → `fr.ParentOf(child)` (`@scope.go:189` / `@childof.go:76`); message names actual vs expected parent.
- `AssertChildren` → collect via `fr.EachChild(parent, ...)` (`@childof.go:25` / `@scope.go:195`) into a set; compare set-equal to `wantChildren` ignoring order; message lists missing + extra IDs.
- `AssertHasPair` → `flecs.HasID(fr, e, flecs.MakePair(rel, target))` (`@scope.go:558`).
- `AssertName` → `fr.GetName(e)` (`@scope.go:249` / `@name.go:30`); message shows got/want name.
- `AssertTag` → `flecs.HasID(fr, e, tag)`.

### Golden snapshot helpers

```go
func AssertSnapshotGolden(tb testing.TB, w *flecs.World, goldenPath string)
func RequireRoundTrip(tb testing.TB, w *flecs.World)
```

- Package-level `var update = flag.Bool("update", false, "rewrite flectest golden files")`. When set, `AssertSnapshotGolden` writes the marshaled JSON to `goldenPath` (creating parent dirs) instead of comparing. Standard Go golden-test idiom. Use `w.MarshalJSON()` (verified `@marshal.go:189`); normalize for stable diffs if needs be (document any normalization).
- **`RequireRoundTrip` must use the JSON path, NOT the binary snapshot path.** Exploration found that binary `RestoreSnapshot` **panics on cross-world restore**: a `Snapshot` is bound to its origin world via an `unsafe.Pointer` identity token and `RestoreSnapshotContext` panics if `s.worldID` differs (`@snapshot.go`). Restoring "into a fresh world" is therefore impossible by design. Implement `RequireRoundTrip` as: `b1 := w.MarshalJSON()` → `w2 := flecs.New(); w2.UnmarshalJSON(b1)` (verified `@marshal.go:745`, no world-identity binding on the JSON path) → `b2 := w2.MarshalJSON()` → assert `b1 == b2` (structural round-trip). Document this constraint and why the binary path was rejected.

### Helper builders

```go
func MustEntity(tb testing.TB, w *flecs.World, name string, comps ...any) flecs.ID
func MustChild(tb testing.TB, w *flecs.World, parent flecs.ID, name string, comps ...any) flecs.ID
```

- Create the entity in a `w.Write` scope, set its name via `fw.SetName` (`@scope.go:447`), and for each `comps` element reflect its concrete type and write it. Use the reflection-based dynamic path: `flecs.RegisterComponentByType`-equivalent + `fw.SetByID(e, id, v)` (verified `(*Writer).SetByID` at `@scope.go:452`, `(*World).SetByID` at `@value_ops.go:242`). Auto-register the component type by reflection so callers don't have to pre-register — friendlier for tests; document that requirement is removed.
- `MustChild`: same, plus add `(ChildOf, parent)` via `fw.AddID(e, flecs.MakePair(w.ChildOf(), parent))` (verified `@childof.go:10`).

## Required tests (`@flectest/flectest_test.go`)

Use a **mock `testing.TB`** (`recordingTB`) that records `Fatalf`/`Errorf`/`Helper` calls instead of aborting, so helper failure paths can be asserted. **Mock-TB pattern (spell out so the iterate agent does not fight `testing.TB`'s unexported `private()` method): embed `testing.TB` as an interface field left nil, and override only `Helper()`, `Fatalf(...)`, `Errorf(...)` (and `Cleanup` for the lifecycle tests). The embedded nil interface satisfies the unexported method requirement at compile time; the overridden methods are the only ones ever called.** Document this pattern in `@docs/Testing.md`.

Test cases:
- `TestNewWorld_CleanupRegistered` — `tb.Cleanup` is invoked.
- `TestNewWorldWith_SetupRuns` — setup closure runs; entities exist afterward.
- `TestAssertAlive_PassAndFail`
- `TestAssertHasComponent_PassAndFail`
- `TestAssertNoComponent_PassAndFail`
- `TestAssertComponentValue_Equal_Mismatch` — mismatch message contains got+want.
- `TestAssertComponentValueFunc_PredicateFail`
- `TestAssertEntityCount_Mismatch` — message includes got+want.
- `TestAssertQueryCount_Mismatch`
- `TestAssertParent_WrongParent` — message identifies actual vs expected parent.
- `TestAssertChildren_SetEquality_OrderIndependent` — passes regardless of order; fails on missing/extra.
- `TestAssertHasPair_PassAndFail`
- `TestAssertName_Mismatch`
- `TestAssertTag_PassAndFail`
- `TestAssertSnapshotGolden_MatchAndMismatch` — fixture golden under `@flectest/testdata/`.
- `TestAssertSnapshotGolden_UpdateFlag` — with the flag forced true, rewrites a golden at a `t.TempDir()` path.
- `TestRequireRoundTrip_HappyPath` — a populated world round-trips JSON cleanly; also assert a contrived mismatch is detected (e.g. compare two deliberately-different JSON blobs through the same comparison helper).
- `TestMustEntity_AutoRegisters` — component type auto-registered if not pre-registered.
- `TestMustChild_ParentLinkCorrect`
- `TestHelpers_CallTBHelper` — every assertion calls `tb.Helper()` (recordingTB records Helper() invocations; table-driven over all helpers).

## Mechanical acceptance

- `go vet ./...` clean (incl. `./flectest/...`).
- `golangci-lint run ./...` clean.
- `go test ./... -race -count=3` clean (incl. `flectest`).
- Coverage ≥ 95% for the `flectest` package itself.
- Root `flecs` package coverage unchanged (flectest is a separate package).
- `go list -deps ./flectest` shows only `flecs` + stdlib (no third-party).
- Every assertion calls `tb.Helper()` so failure line numbers point at the caller.

## Documentation update matrix

- New `@docs/Testing.md` — full flectest reference: the `*flecs.World`-parameter rationale, lifecycle (no-op cleanup explanation), every assertion with a short example, golden-snapshot workflow + `-update` flag, the JSON-based `RequireRoundTrip` and why binary snapshot can't cross worlds, and the mock-TB pattern for testing test-helpers.
- New `@flectest/example_test.go` — runnable `Example` functions: `NewWorld` + a few assertions in a realistic test.
- New `@flectest/testdata/` — fixture golden file(s) for the snapshot tests.
- `@docs/FAQ.md` — add "How do I test ECS code?" pointing at flectest.
- `@CHANGELOG.md` — `v0.110.0` entry (Phase 16.55).
- `@ROADMAP.md` — add Phase 16.55 to Shipped.
- `@README.md` — testing feature row + flectest mention.
- `@doc.go` — mention the `flectest` subpackage in the package overview.

## Constraints

- @scope.go — `scope` interface is unexported (`scope { scopeWorld() *World }`, line 46); helpers MUST take `*flecs.World` and open `Read` internally, not `scope`/`*Reader`/`*Writer`. Verified read/write free functions `Get`/`Has`/`HasID`, and `fr.IsAlive`/`Count`/`ParentOf`/`EachChild`/`GetName`.
- @snapshot.go — binary `Snapshot` is world-identity-bound; `RestoreSnapshot` panics on cross-world restore. `RequireRoundTrip` MUST use the JSON marshal/unmarshal path instead.
- @marshal.go — `(*World).MarshalJSON` (line 189) and `(*World).UnmarshalJSON` (line 745); JSON path has no world-identity binding, so it round-trips into a fresh `flecs.New()`.
- @world.go — `flecs.New()` (317), `(*World).Read` (2211), `(*World).Write` (2224), `(*World).Count` (1373), `RegisterComponent[T]` (1382).
- @value_ops.go — `(*World).SetByID` (242) / `GetByID` (119) for reflection-based `MustEntity`/`MustChild` component writes.
- @query.go — `Query.Iter()`, `it.Next()`, `it.Count()`, `it.Entities()` for `AssertQueryCount`.
- @name.go — `SetName`/`GetName` for name helpers and `AssertName`.
- @childof.go — `(*World).ChildOf()`, `EachChild`, `ParentOf` for parent/children/MustChild.
- @CLAUDE.md — follow repo conventions: zero third-party deps; subpackage of the same module; stdlib `testing`/`flag` only; match existing docs/test/changelog/roadmap style.
- Non-goals (do NOT build): property-based/fuzz harness; benchmark helpers; mocking the World itself; factories beyond MustEntity/MustChild; third-party assertion-lib integration (testify etc.); a separate Go module. Keep the package small and focused — resist scope creep into a full test framework.
- Target version **v0.110.0**, phase **16.55**, label `snichols/queued` (not a bug).
