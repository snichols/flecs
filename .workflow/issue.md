## Goal

Add **query scopes** to Go-flecs: a way to negate a sub-expression of arbitrary terms as a unit. Today, `Without` negates one term at a time, so expressions like `Position, !{ Velocity || Speed }` cannot be written directly. The user-facing equivalence `!(A∨B) = !A∧!B` works for simple presence-OR cases but breaks down once the inner expression mixes And/Or, fixed sources, or sparse terms; the DSL clarity loss is also real.

Target: **v0.75.0**, immediately following v0.74.0 entity scoping (commit `fbfb3c3`). Closes the `docs/README.md` line 114 gap:

> Query scopes — `scope_open` / `scope_close` negate a sub-expression of arbitrary terms (e.g., `Position, !{ Velocity || Speed }`). not yet ported in Go flecs.

### Surface and design lock-ins

- **Builder API shape**: **closure** (locked in).
  - `flecs.WithoutScope(buildFn func(*ScopeBuilder)) Term` — top-level constructor.
  - `(*ScopeBuilder)` exposes `With(id)`, `Without(id)`, `Or(id)`, `Maybe(id)`, plus nested `WithoutScope(buildFn)`. Mirrors top-level term constructors so users do not learn a second vocabulary.
  - Closure approach avoids forgot-to-close bugs that a `ScopeOpen` / `ScopeClose` terminator pair would invite, and produces a finished `Term` value that drops into the existing `NewQueryFromTerms(w, ...)` / `NewCachedQueryFromTerms(w, ...)` term-list. Note: today's API is positional — `flecs.NewQueryFromTerms(w, flecs.With(a), flecs.Without(b), flecs.Or(c))`. There is no chained `.And()` method; the request's example syntax was illustrative. The scope term plugs into this positional model uncha­nged.
- **Empty scope**: **panic at construction** (locked in). Mirrors upstream's `validator.c:1441` `\"invalid empty scope\"` error and matches Go-flecs's convention of failing loudly at builder time rather than producing silent match-all semantics.
- **Nested scopes**: **arbitrary depth** (locked in). Upstream supports it (`compiler.c:159–169` increments a counter); the Go closure form trivially supports it because `ScopeBuilder.WithoutScope` accepts another `buildFn`.
- **Cached query invalidation**: scope sub-query terms participate in change-tracking like normal terms. Every component referenced inside the scope is registered as a dependency on the parent `CachedQuery`; any `ChangeCount` bump on a table containing those components flips `Changed()`. Upstream marks scoped terms `EcsTermIsCacheable = false` at `validator.c:1435` — Go-flecs is more conservative because its cache is a flat table list rather than upstream's per-term instruction cache, so it can keep caching the parent without dropping correctness.

### Implementation outline

1. **`Term` extension** (`query.go`): add `TermScope` kind and `Sub []Term` for the nested term list. A `TermScope` term has `ID = 0`, `Kind = TermScope`, `Sub = <recursive terms>`, and the negation is implicit (this phase only ships negated scopes; non-goal: positive scopes).
2. **Builder API** (`query.go`):
   - `flecs.WithoutScope(buildFn func(*ScopeBuilder)) Term`.
   - `type ScopeBuilder struct { terms []Term }` with `With`, `Without`, `Or`, `Maybe`, `WithoutScope` methods that mirror the top-level constructors. The buildFn mutates the builder; `WithoutScope` collects the accumulated terms into a `TermScope` term.
   - Panic at construction if buildFn produces zero inner terms (matches upstream `validator.c:1441`).
3. **Validation / sort** (`validateAndSortTerms` in `query.go:1483`): recurse into `Sub` for sparse/DontFragment/Union routing-hint propagation, fixed-source resolution, and inheritable-promotion. Scope terms themselves do not participate in the And/Not/Or/Optional sort; they are appended after Optional terms (or interleaved adjacent to their semantic siblings — implementer's call, but document the choice).
4. **Match evaluation**:
   - **Table-level fast path**: a scope containing only `With` terms (no Or, no nested scope, no traversal) has \"present in this table → fail\". If NONE of the contained component IDs are in the table, the negated scope is satisfied. Skip whole tables when the parent's other filters allow.
   - **Per-entity slow path**: otherwise, evaluate the sub-query against the entity using existing `entityMatchesTerms`-style logic (already used by Phase 16.15 multi-term observers — reuse pattern). Flip the result for the negation.
   - **Sparse / DontFragment / Union inside scope**: term evaluator already handles these uniformly per-entity; no special-casing needed.
   - **Fixed-source inside scope** (Phase 16.18): nested fixed-source terms evaluate at the named entity (not `$this`). The resolution path is the same as the top-level — read the component snapshot at iter start and treat as a constant during evaluation.
5. **Cached query** (`cached_query.go`): `newCachedQueryInternal` already accepts arbitrary term lists. The validator hook extends to register scope-internal IDs as table-match dependencies for `tryMatchTable`. `Changed()` consults the union of parent-term IDs and scope-internal-term IDs.

### Tests (new `query_scope_test.go`, ≥ 10 cases)

1. `With(posID), WithoutScope(b => b.With(velID).Or(speedID))` — Position AND NOT (Velocity OR Speed).
2. Verify equivalence with `With(posID), Without(velID), Without(speedID)` on the simple Or-of-presence case (de-Morgan sanity).
3. `With(posID), WithoutScope(b => b.With(velID).With(speedID))` — Position AND NOT (Velocity AND Speed). Verify result set differs from case 2.
4. Nested: `With(posID), WithoutScope(b => b.With(velID).WithoutScope(b2 => b2.With(frozenID)))` — Position AND NOT (Velocity AND NOT Frozen).
5. Scope with multi-Or: `With(posID), WithoutScope(b => b.With(aID).Or(bID).Or(cID))`.
6. Empty-scope panic: `WithoutScope(func(b *ScopeBuilder) {})` panics at construction with a clear message.
7. Scope with sparse component: at least one term inside scope refers to a `SetSparse` component; verify per-entity evaluation works.
8. Scope with DontFragment component inside: verify routing.
9. Scope with fixed-source term: `WithoutScope(b => b.With(configID).Source(globalEntity))` — nested fixed source resolves at `globalEntity`.
10. Cached query with scope: build a `CachedQuery` containing a scope, iterate, then mutate inner-scope components; verify `Changed()` flips and subsequent iter reflects the change.
11. (Bonus) Coverage ≥ 95.0% on the new file plus the modified hunks in `query.go` / `cached_query.go`.

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0% on touched files (above the 90% baseline in CONTRIBUTING.md)
- No regression on existing query tests (`query_test.go`, `query_terms_test.go`, `cached_query_test.go`, `query_filters_test.go`, `query_fixed_source_test.go`, `query_sort_test.go`, `query_group_test.go`)

### Doc updates (per CONTRIBUTING.md § Documentation)

- `docs/Queries.md` — add `## Query scopes` section with code example, de-Morgan note, and pointer to `WithoutScope` / `ScopeBuilder`.
- `docs/README.md` line 114 — flip to ✅ shipped in v0.75.0.
- `README.md` — feature list bump.
- `CHANGELOG.md` — v0.75.0 entry at top, following the established Phase 16.x format.
- `ROADMAP.md` — heading `## Shipped (through v0.74.0)` → `## Shipped (through v0.75.0)`; add Phase 16.20 bullet.

### Explicit non-goals

- No positive scope (only negated). Rare and adds complexity without much win.
- No equality operators (`$this == Foo`, etc.) — separate phase, `docs/README.md` line 112.
- No `AndFrom` / `OrFrom` / `NotFrom` — separate phase, `docs/README.md` line 113.
- No query variables (`$Var`) — separate phase, `docs/README.md` line 109.

### Upstream C references (cited)

- `include/flecs.h:1989` `EcsScopeOpen` / `flecs.h:1992` `EcsScopeClose` — scope marker entities.
- `src/query/compiler/compiler.c:151–171` — scope counter increment/decrement at compile time; anonymous variable handling.
- `src/query/compiler/compiler_term.c:785–803` — `flecs_query_compile_0_src` opens a `EcsQueryNot` block when `term->oper == EcsNot`, pushes/pops compile-scope, ends the block on close. This is the core \"scope is a Not-wrapped sub-instruction\" pattern.
- `src/query/compiler/compiler_term.c:720–752` — `flecs_query_ensure_scope_vars` walks until `EcsScopeClose`, resolving variables used in the sub-expression as entities so they are available on scope entry.
- `src/query/validator.c:1427–1452` — `EcsQueryHasScopes` flag, nesting counter, empty-scope rejection, unbalanced `{`/`}` rejection.
- `src/query/validator.c:1432–1436` — scoped terms marked `EcsTermIsScope` and stripped of `EcsTermIsCacheable` / `EcsTermIsTrivial` flags. **Caveat for Go port**: upstream caches at per-term instruction granularity; Go-flecs caches at table-list granularity, so we keep the parent cached and treat scope-internal IDs as additional dependencies for `Changed()` and `tryMatchTable`. State this divergence in the CHANGELOG.
- `src/query/util.c:572–578` — `{` / `}` rendering in `ecs_query_str`; arbitrary depth supported (no upper bound).
- `src/query/util.c:710–715` — separator handling: no `, ` printed adjacent to `{` open or before `}` close.

### Go-side references (cited)

- `@query.go:62–94` — `TermKind` enum: extend with `TermScope`.
- `@query.go:101–128` — `Term` struct: extend with `Sub []Term` field.
- `@query.go:131–145` — top-level term constructors `With` / `Without` / `Maybe` / `Or`: add `WithoutScope` peer.
- `@query.go:222–236` — `Query` struct: scope terms participate in the existing `terms` slice; no struct change needed beyond what already supports nested fields.
- `@query.go:1015–1138` — `matchesTable`: extend the term-kind switch to handle `TermScope` (table-level fast path when scope has only `With` terms, else fall through to per-entity).
- `@query.go:913–1014` — `matchesSparseTerms` / per-entity check path: recurse for scope terms.
- `@query.go:1483` — `validateAndSortTerms`: recurse into `Sub` for routing-hint propagation.
- `@cached_query.go:96–146` — `CachedQuery` struct: scope-internal IDs registered as dependencies for `tryMatchTable` and `Changed()`. No new field necessary if we walk `terms` recursively.
- `@cached_query.go:616–760` — `tryMatchTable` / `Changed()`: extend recursion.

## Constraints

- @CONTRIBUTING.md — § Style: coverage ≥ 90% on touched files (this goal targets 95% on new code, per the request). § Documentation: every shipped feature updates `docs/<Topic>.md`, `docs/README.md` gap list, `README.md` feature list, `CHANGELOG.md`, `ROADMAP.md` heading bump.
- @docs/README.md — line 114 is the gap entry to close. Line 108 (Phase 14.2 Queries) is the parent gap section. Phase 16.18 fixed-source (line 108) and Phase 16.15 multi-term observers established the per-entity term-evaluation pattern that scope evaluation reuses.
- @query.go — `Term` / `TermKind` shape; `With` / `Without` / `Or` / `Maybe` constructor convention; `validateAndSortTerms` recursion site; `matchesTable` switch site; positional `NewQueryFromTerms(w, terms...)` call convention.
- @cached_query.go — table-list cache model; `tryMatchTable` per-new-table call; `Changed()` per-table `ChangeCount` model; `lastChangeCounts` invalidation map.
- @CHANGELOG.md — v0.74.0 Phase 16.19 (commit `fbfb3c3`, just shipped) sets the format for the new v0.75.0 entry. Use the same Added / Behaviour / Tests / Documentation section structure.
- @ROADMAP.md — line 3 heading `## Shipped (through v0.74.0)` bumps to `v0.75.0`; line 78 is the most recent Phase 16.x bullet to follow as a template.
- @docs/Queries.md — § Operators (line 154) and § Not (Without) (line 177) are the structural neighbours for the new § Query scopes section.
