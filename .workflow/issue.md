## Goal

Add Go 1.23+ `iter.Seq` / `iter.Seq2` adapters to query iteration so users can write range-over-func loops over queries and component values. This is the second post-port-completion Go-idiomatic value-add (following Phase 16.51's `context.Context` cancellation). All existing APIs (`Each` / `EachContext` taking `*QueryIter`, package-level `Each1`-`Each4`, `Iter` / `Field[T]`) remain unchanged — these are pure additions.

Target shape:

```go
for e := range flecs.QueryAll(q, r) {
    // ...
}

for e, pos := range flecs.All1[Position](r) {
    // use e and pos
}

for e, pair := range flecs.All2[Position, Velocity](r) {
    pos, vel := pair.A, pair.B
    // ...
}
```

### Existing API surface (verified)

- `(*Query).Each(fn func(*QueryIter))` and `(*Query).EachContext(ctx, fn) error` in `@query.go` lines 1808 / 1821 — callback receives a positioned `*QueryIter`; callers must NOT call `Next` inside `fn`.
- `(*CachedQuery).Each` / `EachContext` in `@cached_query.go` lines 677 / 685 with the same shape.
- Package-level typed helpers `Each1[A]` / `Each2[A,B]` / `Each3[A,B,C]` / `Each4[A,B,C,D]` in `@scope.go` lines 933 / 962 / 1017 / 1086 — they take `(s scope, fn func(e ID, a *A, ...))` and internally use `Query.Iter` + `Field[T]`.
- `(*Query).Iter() *QueryIter` and `Field[T](it, id) []T` in `@query.go` lines 1544 / 2600 — the low-level driver.
- `scope` interface in `@scope.go` line 46 — satisfied by `*Reader` and `*Writer`.

### API additions

**Bare iteration (entity IDs only):**

```go
func QueryAll(q *Query, s scope) iter.Seq[ID]
func CachedQueryAll(q *CachedQuery, s scope) iter.Seq[ID]
```

(Method form `(*Query).All` is also acceptable — agent should choose based on parameter symmetry with `Each1`-style package-level helpers. Recommendation: keep package-level to mirror `Each1`-`Each4`.)

**Typed iteration with component values:**

```go
type Pair[A, B any]       struct { A *A; B *B }
type Triple[A, B, C any]  struct { A *A; B *B; C *C }
type Quad[A, B, C, D any] struct { A *A; B *B; C *C; D *D }

func All1[A any](s scope)                   iter.Seq2[ID, *A]
func All2[A, B any](s scope)                iter.Seq2[ID, Pair[A, B]]
func All3[A, B, C any](s scope)             iter.Seq2[ID, Triple[A, B, C]]
func All4[A, B, C, D any](s scope)          iter.Seq2[ID, Quad[A, B, C, D]]
```

Package-level `Pair`/`Triple`/`Quad` rather than nesting under `Query` (Triple already exists as a name? agent should grep and rename if collision).

**Context-aware variants** (parallel to existing `EachContext`):

```go
func QueryAllContext(ctx context.Context, q *Query, s scope) iter.Seq2[ID, error]
func CachedQueryAllContext(ctx context.Context, q *CachedQuery, s scope) iter.Seq2[ID, error]
```

Yield `(id, nil)` for each match; on ctx cancellation yield `(0, ctx.Err())` once and stop.

### Implementation strategy

The existing `Each(fn func(*QueryIter))` callback does NOT return `bool`, so it cannot honor `break`. The iter.Seq adapter MUST drive iteration through the low-level `Query.Iter()` / `QueryIter.Next()` / `Entities()` / `Field[T]` machinery directly. Do not refactor `Each` to return `bool` — that would be a breaking change to a stable API.

Reference pattern (mirrors `Each1`'s internal loop in `@scope.go` line 933):

```go
func All1[A any](s scope) iter.Seq2[ID, *A] {
    return func(yield func(ID, *A) bool) {
        w := s.scopeWorld()
        var ids [1]ID
        ids[0] = RegisterComponent[A](w)
        toggleA := w.canTogglePolicies[ID(ids[0].Index())]
        q := NewQuery(w, ids[:]...)
        it := q.Iter()
        for it.Next() {
            if aShared := upPtr[A](w, it, ids[0]); aShared != nil {
                for _, e := range it.Entities() {
                    if !yield(e, aShared) { return }
                }
                continue
            }
            colA := Field[A](it, ids[0])
            for i, e := range it.Entities() {
                if toggleA && !it.current.IsRowEnabled(ids[0], i) { continue }
                if !yield(e, &colA[i]) { return }
            }
        }
    }
}
```

The same Inheritable / CanToggle / sparse handling currently inside `Each1` must be preserved. Factor shared bookkeeping into a helper if it removes duplication; do NOT regress the toggle/inheritable paths.

### Go version

`@go.mod` is already on `go 1.26.1` — well past 1.23. No bump needed; CHANGELOG should NOT call out a version-requirement change.

### Early-exit semantics

When the user breaks out of a `range` over an `iter.Seq`/`iter.Seq2`, the runtime calls `yield` with the sentinel and expects subsequent calls to be skipped. Every loop level in the adapter (the outer `it.Next()` table loop AND the inner per-entity loop) must check `yield`'s return value and `return` immediately on `false`. Tests must cover both intra-table break (break in the middle of one table) and inter-table break (break after exhausting one table, before the next).

### Pointer-stability semantics

The `*T` yielded by `All1`/`All2`/etc. points into table column memory. The pointer is valid only within the body of the yield call: any mutation that triggers a table migration (Add/Remove/Set on a new component) MAY invalidate the pointer. Document this in `@docs/Queries.md` and in godoc comments. The recommendation is the same as for `Each1`-`Each4`: dereference and stack-copy the value before mutating the world.

## Constraints

- @go.mod — already on `go 1.26.1`; no version bump required, do not mention as a change in CHANGELOG.
- @query.go — `(*Query).Each` (line 1808) takes `func(*QueryIter)` not `func(ID) bool`; cannot be refactored without breaking. Drive iteration via `Query.Iter()` + `QueryIter.Next()` + `Field[T]` directly. `EachContext` (line 1821) checks `ctx.Done()` every `ctxCheckInterval = 1024` tables — the new `*AllContext` variants should use the same cadence for consistency.
- @cached_query.go — `(*CachedQuery).Each` (line 677) and `EachContext` (line 685) follow the same `func(*QueryIter)` shape; `CachedQuery.Iter()` (line 420) is the pull-style entry point to drive directly.
- @scope.go — `Each1`-`Each4` (lines 933 / 962 / 1017 / 1086) are package-level free functions accepting `scope` and a typed callback. The new `All1`-`All4` should mirror this shape (package-level, take `scope`). The Inheritable / `upPtr` / `canTogglePolicies` / sparse-row handling inside each `EachN` MUST be preserved verbatim in the adapter; consider extracting a shared inner helper if duplication grows.
- @scope.go line 46 — `scope` interface (`scopeWorld() *World`) is satisfied by both `*Reader` and `*Writer`; new APIs accept `scope` so they compile inside both read and write blocks.
- @CHANGELOG.md — current top entry is `v0.106.0 — 2026-05-15 — Phase 16.51` (context.Context cancellation). New entry must be `v0.107.0 — Phase 16.52: iter.Seq / range-over-func for queries`. Follow the same section structure (`What changed`, `New API`, `Design notes`, `New tests`, `Documentation`, `Breaking changes` — none).
- @ROADMAP.md — top of Shipped section currently reads `## Shipped (through v0.104.0)`; iterate agent will bump and add a Phase 16.52 bullet.
- @docs/Queries.md — add a major section "Range-over-func iteration" covering all signatures, examples, performance equivalence with `Each*`, early-exit semantics, pointer-stability caveat.
- @doc.go — package overview gets one runnable example showing `for e, pos := range flecs.All1[Position](r)`.
- @README.md — feature row mention.
- New file `@queries_iter_examples_test.go` — runnable `Example*` functions for godoc rendering.
- New file `@query_iter_seq_test.go` — the test suite below.
- Naming check: grep the codebase for existing `Pair` / `Triple` / `Quad` symbols before adding the helper types. `MakePair` and pair-related entities exist; the new `Pair[A,B]` generic struct must not collide. If `Pair` is already taken by a non-generic type, prefer `Tuple2[A,B]` / `Tuple3` / `Tuple4`.

### Tests (in new `@query_iter_seq_test.go`)

**Bare iteration:**
- `TestQueryAll_BareYieldsAllMatches` — 5 entities matching; range yields all 5.
- `TestQueryAll_BreakHonored_IntraTable` — break after 2nd entity within a single table; verify only 2 yielded, iterator stops cleanly, no panics on close.
- `TestQueryAll_BreakHonored_InterTable` — multiple tables; break after exhausting first table; verify subsequent tables never visited.
- `TestQueryAll_EmptyQuery` — no matches → range body never enters.
- `TestCachedQueryAll_BareYieldsAllMatches` — same for cached.

**Typed iteration:**
- `TestAll1_YieldsValues` — `All1[Position]` yields `*Position` pointing to live column data; mutating through the pointer is reflected in `Get[Position]` after the loop (table-backed memory).
- `TestAll1_PointerInvalidatedAfterMigration` — within the yield body, do an `AddID` that triggers migration; document that the post-migration pointer is invalid (this is the same semantics as `Each1`).
- `TestAll2_PairValues` — `All2[Position, Velocity]` yields `Pair` with both pointers.
- `TestAll3_TripleValues` — `All3` yields `Triple` with three pointers.
- `TestAll4_QuadValues` — `All4` yields `Quad` with four pointers.
- `TestAll1_BreakHonored` — break out mid-iteration.
- `TestAll1_NoEntities` — empty query.
- `TestAll1_InheritableShared` — when component A is `SetInheritable`, range-over-func yields the same prefab pointer for every instance in a matched table (mirrors `Each1`'s `upPtr` path).
- `TestAll1_CanToggleSkipsDisabled` — when component A is `SetCanToggle` and a row is disabled, that row is skipped (mirrors `Each1`'s `IsRowEnabled` check).

**Context-aware:**
- `TestQueryAllContext_PreCanceled` — already-cancelled ctx → first yield is `(0, ctx.Err())` and iterator stops.
- `TestQueryAllContext_CanceledMidIteration` — cancel after N yields → next yield is error, then stops.
- `TestQueryAllContext_TimeoutFires` — `context.WithTimeout` (1ms over a large table set); verify deadline observed within `ctxCheckInterval` tables.
- `TestCachedQueryAllContext_*` — equivalents for cached.

**Pair / Triple / Quad helpers:**
- `TestPair_FieldAccess` — `Pair[A,B]` fields `A` and `B` both accessible and non-nil.
- `TestPair_NilOnMissingOptional` — if a future Optional/Maybe variant is added, the absent slot is nil. (Phase 16.52 does NOT add Maybe variants; this test is a placeholder skipping with `t.Skip("Maybe variants land in a later phase")`.)

**Back-compat:**
- All existing `Each*` tests pass unchanged.
- All existing `Iter` / `Field[T]` tests pass unchanged.

**Benchmark:**
- `BenchmarkEach1_vs_All1` — same workload (1000 matching entities, single `Position`); All1 within 5% of Each1 wall-clock per iteration. Place in `@bench_test.go` or a new file consistent with project layout.

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run ./...` clean
- `go test ./... -race -count=3` clean
- Coverage ≥ 95% (current baseline)
- All existing tests pass unchanged
- `@go.mod` Go version unchanged (already 1.26.1)

### Documentation update matrix

- @docs/Queries.md — new major section "Range-over-func iteration": signatures, examples, performance equivalence with `Each*`, early-exit semantics, pointer-stability caveat. Cross-link from the existing `Each*` section.
- @queries_iter_examples_test.go (new) — `Example*` functions for godoc.
- @CHANGELOG.md — v0.107.0 entry following the v0.106.0 / Phase 16.51 template; no Go-version note.
- @ROADMAP.md — add Phase 16.52 to Shipped; bump top-of-section "through vX.Y.Z" tag.
- @README.md — feature row mention.
- @doc.go — one range-over-func example in the package overview.

### Non-goals

- Replacing existing `Each*` / `Iter` APIs — pure additions.
- iter.Seq for non-query iteration (`EachEntity`, `EachChild`, `EachUnion`, etc.) — separate phase.
- Specialized `iter.SeqN` for >2 values — using `Pair`/`Triple`/`Quad` structs is sufficient.
- Performance optimization beyond matching `Each*` — sub-5% delta is sufficient; do not micro-optimize.
- `iter.Pull` / `iter.Pull2` manual-advance API — yield-based is sufficient for v1; revisit if a user requests pull-style.
- Adding Maybe / Optional component variants to `All1`-`All4` — defer to a later phase tracking Optional-aware typed helpers.
