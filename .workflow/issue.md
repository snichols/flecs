## Goal

Three small extensions to the query variable system, each deferred from Phase 16.44, bundled together because they all touch the same variable plumbing (`@query.go`, `@query_dsl.go`, `@query_optimizer.go`). Target release: **v0.100.0** (centennial). Plus two minor `@docs/README.md` cleanups.

### Feature 1 — Variable in relationship-name position

**DSL syntax:** `($Rel, target)` or `($Rel, $tgt)` — the relationship slot of a pair is itself a variable.

**Semantics:**
- For each candidate entity, walk pairs `(*, target)` (where `target` is fixed or variable-bound).
- Bind `$Rel` to each distinct relationship found.
- Variable accessible via `(*QueryIter).Var(\"Rel\") ID` (mirrors existing entity-var accessor).

**API additions (in `@query.go`):**
- `WithPairRelVar(varName string, target ID) Term` — relationship-slot variable, target fixed.
- `WithPairBothVar(relVarName, tgtVarName string) Term` — both slots variables.
- `(*Term).RelVar(name string) *Term` — chain method (mirrors `.SrcVar` / `.TgtVar`).

### Feature 2 — Negative-variable constraints

**DSL syntax:** `!Foo($this, $planet)` or `!(Position, $planet)` — negation of a pair whose target is a previously-bound variable.

**Semantics:**
- After other terms bind `$planet`, this term filters out entities that have `(Foo, $planet)`.
- The variable MUST be bound by an earlier term (no negative-binds-fresh).
- Free negative variables → parser error at construction.

**API:** automatic via parser — uses existing `Without` term with variable target. Verify the existing `Without` accepts a variable in the target slot; if not, extend it (the variable plumbing at `@query.go:164-170` shows the `IsVariable` flag is on the Term, so extension should be local).

New parse-error code: `ParseErrUnboundNegativeVar` (added to the enum at `@query_dsl.go:5-30`).

### Feature 3 — Table-kind variables

**DSL syntax:** `$Table:Position($this)` — `$Table` binds to a table identity (kind=table); entity variable binds within that table.

**Semantics:**
- A table-kind variable iterates archetype tables matching the constraint.
- Entity variables bound to the same table can then iterate rows within it.
- Useful for batch operations: "for each table T containing Position, do X with all entities in T".

**API additions:**
- New `VarKind` enum in `@query.go`: `VarEntity` (default), `VarTable`, `VarAny` (relationship OR target OR entity — `VarAny` is reserved-but-not-implemented; see non-goals).
- `WithVarKind(name string, kind VarKind) Term` — set variable kind at registration.
- `WithTableVar(name string) Term` — convenience for table-kind variable.
- `(*QueryIter).VarTable(name string) *Table` — table-kind accessor (returns `nil` if the variable is entity-kind).
- Existing `(*QueryIter).Var(name) ID` still returns the entity ID for entity-kind variables; returns the table ID (decoded as an `ID`) for table-kind.

**Engine integration:**
- Table-kind variables iterate the table list (per-component-index entry-set).
- Entity-kind variables iterate rows within the bound table.
- The Phase 16.44 driver-variable optimizer (`@query_optimizer.go`) MUST be taught about table-kind: their domain is `count(tables matching constraint)`, typically far smaller than entity count.

### Doc cleanups

- `@docs/README.md` line 13: strip "(pending)" from `## Manuals (pending)` — all manuals are landed. Current text: `## Manuals (pending)`.
- `@docs/README.md` line 69 (issue text said 76; verify on file open): `- **Query language / DSL** (\`FlecsScript\`, \`FlecsQueryLanguage\`) — C-only scripting layer.` — the DSL itself shipped in v0.96.0 (query DSL v2, full FQL surface). Rephrase to clarify that the remaining un-ported piece is `FlecsScript` (embedded language with side-effects), and that it will not be ported.

## Constraints

- @query.go — variable plumbing, `WithVar`/`WithPairTgtVar`/`SrcVar`/`TgtVar`, topo-sort over variables (2976 LOC). Extension points: `IsVariable` flag (line 164/170), `SrcVar` (line 457), `TgtVar` (line 478), `WithVar` (line 516), `WithPairTgtVar` (line 534), topo-sort comment block around line 958.
- @query_optimizer.go — Phase 16.44 driver-variable optimizer (167 LOC). Must learn to estimate domains for relationship-position and table-kind variables; otherwise it falls back to first-defined-wins for those queries (degraded performance but still correct).
- @query_dsl.go — Phase 16.40/16.41 parser (776 LOC currently; the issue brief estimated 614 — phase 16.42–16.44 grew it). Must recognize: `($Rel, target)`, `($Rel, $tgt)`, `!Foo($this, $planet)`, `$Table:Position(...)` colon-prefix table-kind annotation. New error code `ParseErrUnboundNegativeVar` joins the enum starting at `@query_dsl.go:10`.
- @cached_query.go — cached-query construction must accept the new term shapes and surface them in cached plans.
- @rest_query.go — REST endpoint must execute queries that use the three new syntactic forms and surface variable bindings (including `Rel` bindings) in the response payload.
- @internal/storage/table/table.go — Table identity (table ID) for table-kind variable accessor.
- @docs/Queries.md — extend "Query variables" with three new subsections: "Variable in relationship-name position", "Negative-variable constraints", "Table-kind variables".
- @docs/QueryDSL.md — add the three new syntactic forms to the grammar table; extend "Variables" section.
- @docs/README.md — fix line 13 heading; rephrase the DSL line in the Feature-gap list (~line 69).
- @ROADMAP.md — move the three deferrals from Deferred to Shipped under Phase 16.45; bump heading to v0.100.0.
- @CHANGELOG.md — v0.100.0 entry (centennial release — keep that in the header).
- @README.md — feature row update if variables are called out.
- @CLAUDE.md — repository conventions for testing (`go test ./... -race -count=3`), linting (`go vet`, `golangci-lint`), coverage floor (≥95%), and `@`-reference style apply.

### Variable cap

The existing variable cap of 16 (Phase 16.26) accommodates the additional variable kinds — relationship-position, table-kind, and negative-target variables all count against the same cap. Verify in tests that a query approaching the cap with mixed kinds still works.

### Topo-sort ordering

Negative-variable terms participate in the topo-sort: they depend on variables they reference but do not introduce new ones. They MUST be scheduled AFTER their referenced variables are bound. The existing topo-sort over `SrcVar`/`TgtVar` constraints (commented around `@query.go:958`) is the extension point.

### Upstream reference

The upstream `flecs/` submodule is not checked out in this working copy, so the issue cannot pin line numbers in `flecs/src/query/compiler/compiler.c`, `flecs/include/flecs.h`, or `flecs/docs/FlecsQueryLanguage.md`. The iterate agent should `git submodule update --init` (or otherwise materialize the upstream tree) and capture the exact line ranges for:
- `ecs_var_kind_t` enum (`EcsVarEntity`, `EcsVarTable`, `EcsVarAny`) in `flecs/include/flecs.h`
- `$Rel` and `EcsVarTable` patterns in `flecs/src/query/compiler/compiler.c`
- Relationship-position and negative-variable DSL grammar in `flecs/docs/FlecsQueryLanguage.md`

## Required tests

### Variable in relationship position (new file: `@query_var_test.go` if not present, else append)
- `TestVar_RelVar_Simple` — query `($Rel, hero)` against entity with `(Likes, hero)`; verify `Rel` binds to `Likes`.
- `TestVar_RelVar_MultipleBindings` — entity with `(Likes, hero)` and `(Owns, hero)`; verify iter produces both `Likes` and `Owns` bindings.
- `TestVar_RelBothVar` — `($Rel, $tgt)` query; verify both bind across all pairs.
- `TestVar_RelVar_WithFixedTarget` — `($R, parent), Position($this)` returns parent-related entities with their `Position`.
- `TestVar_RelVar_TermChainMethod` — `WithPair(_, _).RelVar(\"R\").TgtVar(\"T\")` syntax works.

### Negative-variable constraints
- `TestVar_NegativeVar_Filters` — `Position($this), Velocity($x), !Brake($this, $x)` returns entities with `Position` and any `Velocity`-bound `x` that don't have `Brake-x`.
- `TestVar_NegativeVar_FreeBindingPanics` — `!Foo($this, $freeVar)` with no earlier binding → parse error (code `ParseErrUnboundNegativeVar`).
- `TestVar_NegativeVar_OptimizerInteraction` — verify driver selection ignores negative terms for domain estimation (negative terms filter, not constrain).

### Table-kind variables
- `TestVar_TableKind_Basic` — `$T:Position($this)` returns all tables containing `Position`; per table iterate entities.
- `TestVar_TableKind_Accessor` — `iter.VarTable(\"T\")` returns the bound table; `iter.Var(\"T\")` returns the table-decoded ID.
- `TestVar_TableKind_OptimizerWins` — query with both entity-kind and table-kind variables; verify optimizer picks table-kind as driver when its domain is smaller.
- `TestVar_TableKind_WithPair` — table-kind variable bound to tables having `(ChildOf, *)` pairs.

### DSL integration (`@query_dsl_test.go` + `@rest_query_test.go`)
- `TestParse_RelVarSyntax` — parser builds correct `Term` for `($Rel, target)`.
- `TestParse_RelBothVar` — parser handles `($R, $T)`.
- `TestParse_NegativeVar` — parser handles `!Foo($this, $planet)`.
- `TestParse_NegativeVar_UnboundError` — error code = `ParseErrUnboundNegativeVar`.
- `TestParse_TableVarSyntax` — parser handles `$Table:Position(...)`.
- `TestRest_Query_RelVar` — REST endpoint executes a relationship-variable query and returns matches with the `Rel` binding in the response.
- `TestRest_Query_NegativeVar` — REST executes a negative-variable query.
- `TestRest_Query_TableVar` — REST executes a table-variable query.

### Doc cleanups
- Verify `@docs/README.md` line 13 reads `## Manuals` (no "(pending)").
- Verify the DSL line (~line 69) reflects shipped DSL v2 state and notes `FlecsScript` (embedded scripting w/ side effects) is out of scope.

## Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run ./...` clean.
- `go test ./... -race -count=3` clean.
- Coverage ≥ 95% (current baseline).
- All Phase 16.25 / 16.26 / 16.40 / 16.41 / 16.44 tests must pass unchanged.

## Non-goals

- Variable in component-position-only (e.g. `$C($this)`) — effectively a bare variable; no syntactic value.
- Cross-query variable references (variable bound in query A used in query B) — query-local scope only.
- Streaming join over table-kind variables (always materialized today).
- Negative-variable binding (negation that introduces a new variable) — strictly filter-only.
- Multi-kind variable (`VarAny` — relationship OR target OR entity slot) — reserve the enum value but defer the implementation to a follow-up phase.

## Notes for the iterate agent

- Target version: **v0.100.0** (centennial release — keep that in the CHANGELOG entry header).
- Phase number: **16.45**.
- If any of the three features turns out to require deep iterator surgery (table-kind is the highest-risk candidate — it touches the per-component-index entry-set traversal and may require a parallel iterator state machine), flag in the PR body and split that feature into a follow-up phase; ship the other two and document the deferral.
- The optimizer update is required-for-perf, not required-for-correctness. If `@query_optimizer.go` integration grows beyond ~80 LOC of changes, ship correctness first and follow up with the optimizer in a 16.45.1.
