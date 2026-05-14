## Goal

**Phase 16.26: Multi-variable query support — v0.81.0**

Phase 16.25 (v0.80.0, commit `f2988d6`) shipped single-variable query support: the driver-variable approach handles one named variable (plus implicit `$this`) across multiple terms. This phase extends to **N named variables in a single query**, unlocking multi-hop relational queries like:

> "Find spaceships docked to a planet that orbits a star."

In term form: `SpaceShip($this), DockedTo($this, $planet), Planet($planet), Orbits($planet, $star), Star($star)` — two variables (`$planet`, `$star`) and three relational joins.

Single-variable v1 deferred this because it requires a Cartesian-product enumeration across multiple variable domains. After this phase, the `Multi-variable join optimization` deferral (currently labelled Phase 16.25.x in `ROADMAP.md:130`, `docs/Queries.md:1348`, `docs/Queries.md:1353`, `docs/README.md:109`, `CHANGELOG.md v0.80.0`, and `README.md:263`/`291`) is **closed**.

### Approach (locked decisions)

1. **Variable cap: 16** (current cap is 8 at `query.go:2387`; upstream `EcsQueryMaxVarCount = 64` at `SanderMertens/flecs/src/query/types.h:15`). Leaves headroom for future increases.
2. **Join order: first-variable-as-driver, deterministic.** Outermost loop is the first variable's domain; each additional variable is enumerated nested inside it, with outer bindings substituted. **Smarter join-order optimization is deferred to Phase 16.27** (a separate future-work entry).
3. **Cycle detection: panic at construction.** Topo-sort over the variable-dependency graph (variable A depends on variable B if A's domain-constraining term references B). Cycles fail the topo-sort with a clear cycle-path message.
4. **Performance: prominently documented.** Multi-variable runtime is O(d1 × d2 × … × dN) where dN is each variable's domain size. Users should structure their queries so inner variables are heavily constrained.

### Non-goals (still deferred)

- Join-order optimization (driver-by-cost, table-size heuristics) — **Phase 16.27 candidate**.
- Variable as relationship-name slot (`$Rel($this, target)`) — relationship/component slot stays compile-time fixed.
- Negative-variable constraints (`!Foo($this, $planet)`).
- Variable cap above 16.
- Incremental binding cache between executions of the same `CachedQuery`.

### Deliverables

1. **Variable table extension in `query.go`**:
   - Bump the per-query variable cap from 8 to 16 at `query.go:2387` and update the panic message (drop the `Phase 16.25.x` reference; mention upstream cap 64 still allows further increases).
   - `buildVarSlotsFromTerms` at `query.go:2375` already handles N — only the cap check and doc-comment at `query.go:2370-2374` need updating.

2. **Variable discovery & dependency analysis**:
   - Extend `buildVarSlotsFromTerms` (or a sibling pass) to build a variable-dependency graph. Variable A depends on variable B if any term constraining A's domain also references B.
   - Run a topo-sort; cycles panic at construction with the cycle path.
   - Each variable's "constraining terms" (terms where this variable appears in `srcVar` or `tgtVar`) determine its domain at runtime.

3. **Nested-join iteration**:
   - Outer loop: first variable's domain (currently `collectVarDomain` at `query.go:805` — generalize to "collect domain for variable name X given a partial binding").
   - Inner loops: for each additional variable in topo order, re-materialize its domain with outer bindings substituted, and iterate.
   - For each leaf binding (all variables bound), evaluate remaining `$this`-only terms via `varCheckTable` at `query.go:868` and yield rows. The existing `buildVarRows` at `query.go:921` becomes the inner-most yield step.
   - Pre-materialize all rows at `Iter()` time (preserves the current contract; matches `cached_query.go:432` which re-executes on each iter).

4. **Domain materialization**:
   - For the driver variable: same as today — intersect `SrcVar` constraints, or scan pair targets for `TgtVar`-only constraints. See `collectVarDomain` at `query.go:805-863`.
   - For inner variables: same logic, but with outer-variable bindings substituted into any pair targets / sources before resolution. Re-materialize per outer-binding combination.

5. **API**:
   - `Iter.Var(name)` at `query.go:1553` already works for N variables (it indexes into `varBindings` by slot). No API change needed.
   - `WithVar`, `WithPairTgtVar`, `(Term).SrcVar`, `(Term).TgtVar` already accept any name; no change.

6. **Tests in `query_var_test.go`** (extend; **do NOT** replace the 27 existing 16.25 cases; current size 837 lines). At least 8 new cases:
   - Two-variable chain: `SpaceShip → DockedTo($planet) → Planet → Orbits($star) → Star`. Yields `(spaceship, planet, star)` triples.
   - Three-variable chain: extend further (e.g., star-in-galaxy).
   - Empty join: no spaceship is docked → zero results despite the rest of the world existing.
   - Many-to-many: 3 spaceships × 2 planets × 1 star = 6 result rows.
   - Multi-variable with `With`: `With(Health), DockedTo($this, $planet), With(Planet, $planet)` — entities with Health AND docked to a planet.
   - `Iter.Var` on each variable returns the correct binding for each row.
   - Cycle: variable A's domain depends on B and B's depends on A — panic at construction with cycle path.
   - Variable count at the limit (16): works. 17 panics at construction.
   - Cached query with multi-variable: works after re-execution (state is reset cleanly).
   - Composition with `WithSourceTerm` (Phase 16.18, `query.go:498`): a fixed-source term can reference variables in the `TgtVar` slot. Verify.

   Coverage ≥ 95.0%.

7. **Doc updates** (per `CONTRIBUTING.md:69-80`):
   - `docs/Queries.md`: extend § Query variables to cover multi-variable with the multi-hop spaceship-planet-star example. Add the O(prod of domain sizes) performance caveat. Update the heading and TOC entry at `docs/Queries.md:23`/`1246` (drop "single-variable v1" qualifier). Remove the multi-variable carve-out at `docs/Queries.md:1348` and the join-order carve-out at `docs/Queries.md:1353` (replace with a forward reference to Phase 16.27 join-order optimization). Update the future-work entry at `docs/Queries.md:1418`.
   - `docs/README.md:109`: rewrite to drop "single-variable v1" and the "Phase 16.25.x" carve-out; note multi-variable support shipped in v0.81.0.
   - `README.md:263` and `README.md:291`: update the feature index entry to reflect multi-variable support and the new 16-variable cap.
   - `CHANGELOG.md`: new v0.81.0 entry at top.
   - `ROADMAP.md`: heading bump to "through v0.81.0"; remove the `Multi-variable join optimization` future-work entry at `ROADMAP.md:130`; add a new "Join-order optimization" future-work entry for Phase 16.27 candidate (smarter driver selection by cost / domain size, matching upstream `compiler.c:1002-1021` reorder loop).

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%
- **All 27 existing Phase 16.25 single-variable tests must continue to pass** — multi-variable is an extension, not a replacement.

## Constraints

- @query.go — existing variable infrastructure to extend. Key sites: `Term.srcVar`/`tgtVar` fields (lines 164-170); `(Term).SrcVar`/`TgtVar` chained setters (lines 457-487); `WithVar`/`WithPairTgtVar` constructors (lines ~520, 536); `Query.varSlots`/`driverVar` (lines 573-576); `buildVarSlotsFromTerms` with the 8-cap to bump (lines 2370-2398); `collectVarDomain` to generalize for inner-variable domain resolution with substituted outer bindings (lines 805-863); `varCheckTable` for archetype + bound-variable check (lines 868-913); `buildVarRows` — the current outer driver loop becomes the outermost level of an N-level nested loop (lines 921-1002); `QueryIter.Var` already slot-indexed and works unchanged (lines 1553-1565).
- @cached_query.go — `CachedQuery.varSlots` and the per-Iter re-execution path at lines 134-136, 224-231, 421-440. Extension must preserve the re-execution contract for multi-variable.
- @query_var_test.go — 27 Phase 16.25 test functions; new multi-variable cases extend this file. Existing tests must continue to pass.
- @docs/Queries.md — § Query variables at line 1246, TOC at line 23, v1 limitations at 1348/1353, future-work entry at 1418. All multi-variable carve-outs are removed/rewritten in this phase.
- @docs/README.md — gap entry at line 109 ("Multi-variable join optimization deferred to Phase 16.25.x") — remove the carve-out.
- @README.md — feature index at line 263 and matrix at line 291 — bump to multi-variable.
- @ROADMAP.md — v0.80.0 entry at line 84 and future-work entry at line 130 (multi-variable join optimization → close; add Phase 16.27 join-order optimization candidate).
- @CHANGELOG.md — new v0.81.0 entry at top; existing v0.80.0 carve-outs at lines 26-27 are historical and stay.
- @CONTRIBUTING.md — doc-update requirements at lines 69-80 (Queries.md for query work; CHANGELOG.md and migration notes if breaking).
- Upstream cap: `SanderMertens/flecs/src/query/types.h:15` defines `EcsQueryMaxVarCount = 64`. Go cap of 16 stays well below; usage sites at `src/query/util.c:190` and `src/query/util.c:198`.
- Upstream variable evaluator: `SanderMertens/flecs/src/query/engine/eval_utils.c:57-148` (`flecs_query_var_get_range`, `flecs_query_var_get_table`, `flecs_query_var_get_entity`); set-side at `eval_utils.c:151-235` (`flecs_query_var_set_range`, `flecs_query_var_set_entity`); `flecs_query_set_vars` at `eval_utils.c:237+`. Upstream walks per-variable via the compiled op program; our pre-materialized rows approach (set at `Iter()` time) is a deliberate divergence and stays for multi-variable.
- Upstream acyclic check at `SanderMertens/flecs/src/query/validator.c:610-613` is for acyclic-relationship traversal, not variable cycles — variable cycle detection (this phase's new requirement) has no direct upstream analogue; topo-sort over the variable-dependency graph is the recommended Go-side approach.
- Composition target: `WithSourceTerm` (Phase 16.18, `query.go:489-510`) with `TgtVar` on a fixed-source term must still resolve correctly under multi-variable bindings — covered by a dedicated test case.

## Process

- Feature, not bug.
- Target version v0.81.0 (next after v0.80.0).
- Label: `snichols/queued`.
