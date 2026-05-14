## Goal

Add named query variables (`$Var`) to Go flecs so terms can express **relational joins** instead of just per-entity filters. The motivating example, drawn from upstream's Flecs Query Language:

```
SpaceShip, DockedTo($this, $planet), Planet($planet)
```

Today every term binds either to `$this` (the iterated entity) or to a fixed source entity (Phase 16.18). There is no way to bind a term's source — or a pair-form term's target — to a **runtime-resolved entity slot** that is constrained to satisfy *other* terms in the same query. Variables add that slot: each binding of the variable produces a separate result row, turning the query into a join.

Today's gap entry — `/work/agents/claude/projects/flecs/docs/README.md` line 109:

> Query variables — `$Var` named variables in the Flecs Query Language constrain results across related entities (e.g., "spaceships docked to a planet"). not yet ported in Go flecs.

After this phase line 109 flips to ✅ shipped (v0.80.0) with explicit "v1 single-variable" framing.

### Scoping decision: single-variable v1

Upstream's full variable engine includes a query compiler that builds a plan tree, a query optimizer that picks join order based on table sizes (`/work/agents/claude/projects/SanderMertens/flecs/src/query/compiler/compiler.c:1002-1021` shows the reorder loop), and an executor (`flecs_query_var_set_entity` / `flecs_query_var_get_entity` in `src/query/engine/eval.c` and `eval_utils.c`) that walks the plan producing variable bindings. That is the single most complex remaining gap.

**This issue ships the minimum viable scope**: ONE user variable plus the implicit `$this`. The first-defined variable is the driver — its domain is enumerated; remaining terms re-evaluate per binding. No optimizer; no multi-variable join. Multi-variable + join-order optimization is deferred to a future Phase 16.25.x. State this prominently in the v0.80.0 CHANGELOG entry and the `docs/README.md` line 109 update.

### Upstream reference (cite in PR)

- `ecs_term_ref_t` (`include/flecs.h:799-811`) carries `id` + `name` + flag bits. `EcsIsVariable` (`include/flecs.h:772`) marks the ref as a variable; the variable's name is the `ecs_term_ref_t.name` string.
- `ecs_term_t.src` / `.first` / `.second` (`include/flecs.h:814-833`) — any of the three ref slots can be a variable. v1 ports `src` and `second` (pair target) only.
- Variable kinds (`src/query/types.h:20-37`): `EcsVarEntity` (single-entity binding) vs `EcsVarTable` (table-scope binding). v1 ports `EcsVarEntity` only.
- Variable discovery and slot assignment: `flecs_query_discover_vars` (`src/query/compiler/compiler.c:128` onwards). Variable IDs are `uint8_t` (`src/query/types.h:10-16`); upstream caps at `EcsQueryMaxVarCount = 64`.
- Iterator binding/unbinding: `flecs_query_var_set_entity` / `flecs_query_var_set_range` / `flecs_query_var_reset` (`src/query/engine/eval_utils.c:58-235`).
- Join order optimizer: `src/query/compiler/compiler.c:1002-1021` — comment block explicitly describes reordering once at least one variable is written. **Out of scope for v1; first-defined drives.**
- Variable lifetime: query-wide. The compiler builds `query->vars[]` (`src/query/types.h:439-444`) once per query; the executor maintains live bindings in `ctx->vars` for the duration of an iteration. **Locked: query-wide scope.**

### Go-side state

- `/work/agents/claude/projects/flecs/query.go` — `Term` struct at line 151. Already carries `Src ID` from Phase 16.18 (line 160). v1 extends with `SrcVar string` and `TgtVar string`.
- `/work/agents/claude/projects/flecs/query.go:447-455` — `WithSourceTerm` constructor; v1 adds `WithVar` / `WithPairTgtVar` siblings.
- `/work/agents/claude/projects/flecs/query.go:1027` — `(*QueryIter).Next`. v1 extends `QueryIter` with a variable binding table and a per-iteration driver-domain cursor.
- `/work/agents/claude/projects/flecs/cached_query.go` — cached query plan; variables fundamentally change the plan structure. Cached queries with variables re-execute bindings each `Iter()`; the cache only memoizes archetype-table membership for the driver term, not the join.
- `/work/agents/claude/projects/flecs/query_fixed_source_test.go` — most recent precedent for query-term enhancement (Phase 16.18); follow this test style.

## Constraints

- @docs/README.md — gap entry to close at line 109; flip to ✅ shipped (v0.80.0) with explicit "v1 supports single-variable join; multi-variable join optimization is future work" framing.
- @docs/Queries.md — add a § Query variables section with the spaceships-and-planets example and the join-semantics explanation. Follow the structure of the existing § Fixed per-term source (line 1152 onwards).
- @query.go — `Term` struct (line 151) extends with `SrcVar` and `TgtVar` string fields, mutually exclusive with `Src` and the implicit `$this`. New constructors `WithVar(componentID ID, varName string) Term` and `WithPairTgtVar(rel ID, varName string) Term`. New `(Term).SrcVar(name string) Term` / `(Term).TgtVar(name string) Term` chained setters following the Phase 16.18 `(Term).Source(e)` pattern (line 427).
- @cached_query.go — cached query plan re-executes variable bindings per `Iter()`; cache only memoizes driver-term archetype-table membership.
- @query_fixed_source_test.go — test-style precedent. Mirror its setup-then-iterate-then-assert structure in `query_var_test.go`.
- @ROADMAP.md — heading "Shipped (through v0.79.0)" (line 3) bumps to "through v0.80.0". Append a Phase 16.25 entry under it with the "single-variable v1" framing. Add a Future Work note for Phase 16.25.x multi-variable optimization.
- @CHANGELOG.md — new `v0.80.0 — <date> — Phase 16.25: Query variables (\$Var, single-variable v1)` entry at top, following the format established by v0.79.0 (line 3) and earlier.
- @README.md — feature list bump for query variables (single-variable v1).
- @CONTRIBUTING.md — follow its required doc-update / coverage / lint / race-test discipline.
- Upstream parity references (cite in implementation comments where load-bearing): `include/flecs.h:772` (`EcsIsVariable`), `include/flecs.h:799-811` (`ecs_term_ref_t`), `src/query/types.h:20-37` (variable kinds), `src/query/compiler/compiler.c:128` onwards (variable discovery), `src/query/engine/eval_utils.c:58-235` (set/reset), `src/query/compiler/compiler.c:1002-1021` (optimizer — explicitly out of scope for v1).

## Deliverables (minimal viable scope)

1. **`Term` shape** (`query.go`):
   - `Term.SrcVar string` — when non-empty, the term's source is a named variable. Mutually exclusive with `Term.Src` (panic at construction if both set) and with the implicit `$this`.
   - `Term.TgtVar string` — for pair-form terms (`MakePair(rel, tgt)`), the second slot is a variable. Mutually exclusive with a non-zero pair target in the ID.

2. **Builders**:
   - `flecs.WithVar(componentID ID, varName string) Term` — match `componentID` on the entity bound to `\$varName`. Equivalent to `Component(\$var)` in FQL.
   - `flecs.WithPairTgtVar(rel ID, varName string) Term` — match `(rel, \$varName)`; pair-target variable form.
   - Chained setters `(Term).SrcVar(name string) Term` and `(Term).TgtVar(name string) Term` for symmetry with `(Term).Source(e)`.
   - Panic on empty `varName`. Panic on combining `SrcVar` with traversal (`Up`/`SelfUp`/`Cascade`) — same restriction as fixed-source.

3. **Query compilation** (validation + plan):
   - At construction, walk all terms, collect variable names, build a `map[string]int` slot table on `*Query`.
   - **Driver selection**: the FIRST variable encountered (registration / term-list order) is the driver. Document the deterministic choice; do not auto-optimize.
   - Classify terms into: driver-constraining (SrcVar or TgtVar mentions the driver — these define the driver's domain via archetype membership) vs filter (other terms re-evaluated per binding).
   - Cap at 8 named variables for v1 (parity-safe headroom; upstream caps at 64). Exceeding panics at construction.

4. **Iterator** (`QueryIter`):
   - At `Iter()`, enumerate the driver variable's possible values as the union of entities in tables matching the driver-constraining terms. Implementation: standard archetype-filter walk, then per-entity emission into the driver domain.
   - For each candidate binding, substitute and evaluate all other terms with the variable resolved to that entity. Pair-target variables substitute into a per-iteration pair ID; src-variables substitute into a per-iteration fixed-source.
   - Yield once per `(driver-entity, \$this-entity)` pair that satisfies every term. The `$this` entity remains the per-row iterated entity (as today). Variable bindings are per-result-row.
   - Backward-compat: queries with zero variables take the existing fast path; no overhead.

5. **Result API** (`QueryIter`):
   - `(*QueryIter).Var(name string) ID` — current binding of the named variable for the current iteration position. Panics on undefined name.
   - Existing `Field[T]` / `FieldMaybe[T]` / `Entities()` continue to work unchanged. `Field` access for a variable-source term returns the 1-element slice as fixed-source already does.

6. **Tests** (`query_var_test.go`, ≥ 10 cases, coverage ≥ 95.0%):
   1. Single-variable join: 3 spaceships (A, B, C), 2 planets (P1, P2), 4 docking pairs (A→P1, B→P1, B→P2, C→P2). Query `With(SpaceShip), WithPairTgtVar(DockedTo, "planet"), WithVar(Planet, "planet")` yields exactly 4 rows; verify each row's `$this` and `Var("planet")` binding.
   2. Variable target-bound only: same scene, query via pair-target variable without an explicit `WithVar(Planet, ...)` constraint — every dock target is allowed; rows = all docking pairs.
   3. Multi-hop relational ("spaceships docked to a planet that has a star"): if this requires a second variable, **defer to Phase 16.25.x** and document the deferral in the test as a TODO `t.Skip` with a link to this issue. State in the issue why: it needs multi-variable join.
   4. No matches: `WithPairTgtVar(DockedTo, "planet")` but no entity holds a `DockedTo` pair. Zero results.
   5. Empty variable domain: `WithVar(Planet, "planet")` constrains the domain; no entity has Planet. Zero results.
   6. Combined: `With(SpaceShip), WithPairTgtVar(DockedTo, "planet"), WithVar(Planet, "planet")` — all three terms apply (the canonical motivating example).
   7. `Var(name)` returns the correct binding for each iteration.
   8. `Var("undefined")` panics with a clear error message.
   9. Cached query with a variable: re-running `Iter()` recomputes the bindings; verify two consecutive iterations produce identical row sets.
   10. Mixed with fixed-source (Phase 16.18): a query with both `WithSourceTerm(simTimeID, game)` and `WithPairTgtVar(DockedTo, "planet")` correctly resolves both — fixed at iter-start, variable per-row.
   11. Mixed with `Without` on a variable-bound term: `WithVar(Planet, "planet"), Without(Star)` — driver domain excludes planets with Star.

7. **Documentation** (per CONTRIBUTING.md):
   - `docs/Queries.md` — § Query variables (single-variable v1) with the spaceships-and-planets worked example, join-semantics explanation, and the v1 limitations callout.
   - `docs/README.md` line 109 → ✅ shipped (v0.80.0); single-variable framing noted.
   - `README.md` — feature list bump.
   - `CHANGELOG.md` v0.80.0 entry at top with explicit "single-variable v1" framing and the future Phase 16.25.x deferral.
   - `ROADMAP.md` — heading bump to "through v0.80.0"; add Phase 16.25 to Shipped; add Phase 16.25.x to Future Work.

## Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%
- No regression on existing query tests (Phases 14.2 through 16.24 must remain green)

## Explicit non-goals (v1)

- **Multi-variable join optimization** — first-defined variable is the driver; no auto-reorder. Future Phase 16.25.x.
- **Variable as relationship name** (`\$Rel(\$this, target)`) — only `Src` and pair-`Tgt` positions.
- **Negative-variable constraints** (`!Foo(\$this, \$planet)`) — positive only.
- **Table-kind variables** (`EcsVarTable`) — entity-kind variables only.
- **`$this.field` lookup variables** (`src/query/compiler/compiler.c:90-94`) — out of scope.
- **String-interning** for variable names — Go strings, no pool. Performance optimization is future.
- **FQL string parsing** — programmatic API only; no string-syntax for `\$Var` in v1.

## Open decision points (locked)

1. **Variable scope**: query-wide (matches upstream `query->vars[]` ownership). **Locked: query-wide.**
2. **Driver variable selection**: first-defined (deterministic; matches the order variables appear in the user's `Term` slice). **Locked: first-defined for v1.** Auto-optimize is Phase 16.25.x.
3. **Variable name format**: plain Go string in code; doc-render as `\$name` for FQL clarity. **Locked.**
4. **Variable binding visibility**: `(*QueryIter).Var(name) ID`. **Locked.**
5. **Variable count cap**: 8 named variables for v1 (parity headroom; upstream caps at 64 via `EcsQueryMaxVarCount`). **Locked: 8.**

## Process notes

- Feature, not bug.
- Substantial v1; expect more than one iterate cycle.
- All `@`-references and line numbers verified against the current tree (Go flecs commit on `master` and upstream `/work/agents/claude/projects/SanderMertens/flecs`).
