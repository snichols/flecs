## Goal

Phase 16.44 / v0.99.0 — Make multi-variable query construction pick the **smallest-domain variable as the outermost driver loop**, instead of the current first-defined-wins rule from Phase 16.26 (v0.81.0). This is a pure performance optimization layered on top of the existing topo-sort: ordering is computed once at query construction, has no runtime cost, and must produce **bit-for-bit identical result sets** for every existing multi-variable query.

The work mirrors the upstream `compiler.c` variable-reorder loop (originally cited as lines 1002-1021 — verify in current upstream `flecs/src/query/compiler/compiler.c`; capture exact line numbers in the implementation commit). Search for `flecs_query_var_select`, `flecs_query_compile_term`, and any selectivity/smallest-domain heuristic.

### Approach

1. **Estimate initial domain size per variable** from its constraining terms:
   - Variable in source position via `WithVar($var, C)` where C is a typed component → domain ≈ entities owning C (table-density from the component→tables index, fall back to table-match count as proxy)
   - Variable in pair-target position via `WithPairTgtVar(R, $var)` → domain ≈ distinct targets across `(R, *)` pair rows (sample up to **256 tables** and extrapolate; document the cap)
   - Singleton / fixed-source-bound variable (`Source(e)` style) → domain = 1 (always wins)
   - Sparse / DontFragment / Union components → domain = per-component side-map population count
   - Free variable (no constraining terms) → domain = ∞ (deprioritized to innermost)

2. **Reuse the existing topo-sort** at `@query.go` lines 2493-2500 (`buildVarTopoOrder`). The dependency graph from Phase 16.26 must still constrain ordering — domain size is only a **tie-breaker among variables at the same topological level**.

3. **Materialise the new `varOrder` slice** at construction time. The runtime loop in `buildVarRows` / `buildVarRowsRec` (`@query.go:992-1057`) reads from `varOrder` unchanged. The `driverVar` field (`@query.go:575-579`) is updated to be the chosen driver, not the first-defined.

4. **Fall back to first-defined-wins** if domain estimation returns no information for all variables (zero info, all infinite, etc.). The optimizer must never change correctness — only performance.

### Concrete API additions

- `(*Query).DriverVariable() string` — returns the chosen driver variable name (empty if non-variable query)
- `(*Query).VariableOrder() []string` — full evaluation order (driver first, innermost last)
- Internal `estimateVarDomain(world, varName, terms) int` helper (or similar; verify name avoids collision with existing identifiers in the package)
- **No DSL changes.** The parser/builder ingests terms in author order; the optimizer rewrites the variable enumeration order transparently.

### File layout

- New file `@query_optimizer.go` — domain estimation + driver-selection algorithm
- Extend `@query.go` — wire optimizer into `NewQueryFromTerms` (currently at `@query.go:690-694`), expose `DriverVariable()` / `VariableOrder()` accessors
- Extend `@cached_query.go` — same wiring on the cached construction path
- New file `@query_optimizer_test.go` — optimizer unit tests
- New file `@query_optimizer_bench_test.go` (or extend existing benches) — performance regression benches
- Extend `@query_var_test.go` — result-set equivalence regression

### Required tests

**Optimizer correctness (`query_optimizer_test.go`):**
- `TestOptimizer_NoVars_NoOp` — non-variable query; `DriverVariable()` returns `""`
- `TestOptimizer_SingleVar` — one variable; `DriverVariable()` returns that variable name
- `TestOptimizer_TwoVars_FirstHasSmallerDomain` — driver picks the smaller-domain variable
- `TestOptimizer_TwoVars_SecondHasSmallerDomain` — verifies the optimizer **actually reorders** (driver is NOT just first-defined)
- `TestOptimizer_Dependency_ForcesOrder` — B depends on A (B in pair-target, A in source); A must be driver even if B has smaller domain
- `TestOptimizer_FixedSource_PrefersOne` — `WithSourceTerm(C, hero)`-bound variable counts as domain=1; always wins
- `TestOptimizer_TableSampling_LargeWorld` — synthetic world: variable A matches 10k tables, variable B matches 100; verify B wins as driver
- `TestOptimizer_FreeVariable_Deprioritized` — variable with no constraining terms is pushed to last position
- `TestOptimizer_DriverVariable_Accessor` — `query.DriverVariable()` reports the chosen driver
- `TestOptimizer_VariableOrder_Accessor` — `query.VariableOrder()` returns full order
- `TestOptimizer_ResultSetIdentical` — run all existing multi-variable test cases through both code paths (optimized + unoptimized via internal test hook); verify byte-identical result sets in the same order

**Performance regression (`query_optimizer_bench_test.go`):**
- `BenchmarkOptimizer_SkewedDomain` — A: 10k tables; B: 10 tables. Unoptimized (force driver=A via internal test API) vs optimized (driver=B). Target: ≥10x speedup
- `BenchmarkOptimizer_BalancedDomain` — equal-domain variables; optimizer must not regress (within 10% of unoptimized)

**DSL integration (extend `@rest_query_test.go`):**
- `TestRest_Query_MultiVar_Optimized` — REST `GET /query?expr=...` with a multi-variable expression; verify the optimizer picks the smaller-domain variable; response payload identical to pre-optimization

### Domain estimation caveats

For v1 keep estimation cheap:
- **Typed component term** — read table-density from the component→tables index; otherwise use number of tables matching the component as a proxy. A relative comparison is what matters.
- **Pair term `(R, $var)`** — for each table with `(R, *)`, count distinct targets across rows. Cap at sampling **256 tables** and extrapolate. Document the cap in `Queries.md`.
- **Sparse / DontFragment / Union** — use the per-component side-map population count.
- **Singleton / fixed-source** — domain = 1.

**Deferred to v2** (document in `Queries.md` as known limitations):
- Multi-term intersection refinement (estimating `|A ∩ B|` from `|A| × selectivity(B)`)
- Dynamic re-ordering mid-iteration based on observed cardinality
- Cost model accounting for materialization vs streaming
- Histograms / HyperLogLog / learned costs

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run ./...` clean
- `go test ./... -race -count=3` clean
- Coverage ≥ 95% (current baseline)
- All Phase 16.25 and 16.26 multi-variable tests pass unchanged
- All Phase 16.40 / 16.41 DSL tests pass unchanged

### Documentation update matrix

- `@docs/Queries.md` — extend "Query variables" section with a new "Join-order optimization" subsection: how the optimizer picks the driver, the domain-estimation heuristic, the 256-table sampling cap, deferred-to-v2 features, and performance notes
- `@ROADMAP.md` — move the "Join-order optimization" entry from the "Deferred" section (currently at line 147) into the "Shipped" section as Phase 16.44 / v0.99.0; update the heading version
- `@CHANGELOG.md` — v0.99.0 entry summarising the optimizer
- `@README.md` — add the feature row mention if optimization is called out elsewhere
- **No `docs/README.md` flip** — this was a deferral inside Phase 16.26, not a separate gap line in the docs index

### Non-goals

- Cost-model evaluation across multiple plan candidates (no Volcano-style optimizer)
- Statistics gathering across runs (no learned costs)
- Plan caching across query constructions (each query is built fresh)
- Cardinality estimation via histograms or HyperLogLog
- Variable in relationship-name position (`$Rel($this, target)`) — separate follow-up phase
- Negative-variable constraints (`!Foo($this, $planet)`) — separate follow-up phase
- Table-kind variables (`EcsVarTable`) — separate follow-up phase
- Streaming joins (always materialized today)

## Constraints

- @query.go — `Query` struct fields `driverVar` (line 575-579) and `varOrder` (line 580-583); construction at `NewQueryFromTerms` (lines 690-694); existing helpers `buildVarSlotsFromTerms` (line 2463) and `buildVarTopoOrder` (line 2493); runtime enumeration at `buildVarRows` / `buildVarRowsRec` (lines 992-1057). The optimizer must hook in between `buildVarTopoOrder` and the field assignment, and must preserve the existing topo-sort dependency invariant.
- @cached_query.go — cached query construction path; must be wired identically so cached and uncached queries pick the same driver for the same term list.
- @query_var_test.go — existing multi-variable test cases; all must continue to pass and produce identical result sets in identical order. Add `TestOptimizer_ResultSetIdentical` here.
- @rest_query.go — REST `/query` endpoint executes queries through `World.Read`; the optimizer is engaged transparently. Add a REST-level integration test in `@rest_query_test.go`.
- @docs/Queries.md — primary user-facing reference for query variables; the optimizer's behaviour and the 256-table sampling cap must be documented here under a new "Join-order optimization" subsection.
- @ROADMAP.md — "Deferred" entry at line 147 (`Phase 16.27 candidate` — renumber to Phase 16.44 on ship) must move to "Shipped through v0.99.0".
- @CHANGELOG.md — version entries are appended at the top; follow the existing format for v0.97.0 / v0.98.0.
- The optimizer must **never change correctness** — only performance. If domain estimation fails for all variables, fall back to first-defined-wins (current Phase 16.26 behaviour).
- Result-set ordering is part of the public contract: the optimizer changes the LOOP order, but user-visible row order must be identical to the pre-optimization output for every existing query.
- Document the 256-table sampling cap and the "proxy-when-cheap" rule for typed-component domain estimation in `Queries.md`.
- Upstream parity reference: verify current line numbers in `flecs/src/query/compiler/compiler.c` near the variable reorder loop (originally cited as 1002-1021) and capture them in the implementation commit message.
