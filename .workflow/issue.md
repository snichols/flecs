## Goal

Extend the Phase 16.40 Query DSL parser (`@query_dsl.go`, v1) to expose the **complete set of query engine features that already exist under the hood** but were deferred from v1. This phase is **parser surface + dispatch wiring only** — no new engine functionality. Each DSL syntax form below maps to an already-shipped engine builder; verification is documented in Constraints.

### Features (verified to exist in `@query.go`)

| DSL feature | Syntax | Engine builder | Verified at |
|---|---|---|---|
| OR operator | `Position \|\| Velocity` | `Or(id)` | `@query.go:216` |
| Scope groups (nested negation) | `!(Position, Velocity)` | `WithoutScope(buildFn)` | `@query.go:394` |
| Source binding | `Position(playerEntity)` | `WithSourceTerm(c,e)` and `Term.Source(e)` | `@query.go:498`, `@query.go:439` |
| Optional terms | `?Position` | `Maybe(id)` | `@query.go:209` |
| Traversal Up | `(ChildOf, parent).Up` | `Term.Up(rel)` | `@query.go:419` |
| Traversal SelfUp | `.SelfUp` | `Term.SelfUp(rel)` | `@query.go:425` |
| Traversal Cascade | `.Cascade` | `Term.Cascade(rel)` | `@query.go:431` |
| Query variables (target) | `(ChildOf, $parent)` | `WithPairTgtVar(rel,name)` / `Term.TgtVar(name)` | `@query.go:534`, `@query.go:478` |
| Query variables (source) | `Position($parent)` | `WithVar(c,name)` / `Term.SrcVar(name)` | `@query.go:516`, `@query.go:457` |
| Equality predicates | `$this == hero`, `$this != villain` | `IsEntity(e)`, `NotEntity(e)` | `@query.go:221`, `@query.go:231` |
| Name-match predicate | `$this ~ "pattern"` | `NameMatches(pattern)` | `@query.go:242` |
| AndFrom / OrFrom / NotFrom | `AndFrom(e)`, `OrFrom(e)`, `NotFrom(e)` | `AndFrom`, `OrFrom`, `NotFrom` | `@query.go:257`, `@query.go:272`, `@query.go:286` |

All targeted engine APIs are confirmed present at their stated line numbers in `@query.go`. **No engine functionality is missing — every v2 feature can be implemented purely as parser surface in this phase.**

Note: `Up`/`SelfUp`/`Cascade` take a relationship ID argument. The DSL parser should default the traversal relation to the pair's relation when applied as a postfix on `(R, T).Up`, so `(ChildOf, parent).Up` lowers to `pair(ChildOf, parent).Up(ChildOf)`.

### DSL grammar additions (precedence high→low)

1. `?` prefix — optional. Must lead the term; `?!X` is a parse error (`bad_combination`).
2. `!` prefix — negation.
3. Identifier or pair with optional `(entity)` source binding.
4. `.Up` / `.SelfUp` / `.Cascade` postfix on pair terms. Chains like `.Up.Cascade` error (`bad_modifier`).
5. `==` / `!=` / `~` equality predicates with `\$this` LHS and either entity-name identifier or `"string literal"` RHS.
6. `||` — OR (binds tighter than `,`).
7. `,` — AND (term separator).

### Variable syntax

- `\$identifier` after `\$` sigil; `[A-Za-z_][A-Za-z0-9_]*`.
- First use binds; subsequent uses reference.
- Cap **16 variables per query** (mirrors the engine cap documented at `@query.go:2467`).
- Parser builds a variable-dependency graph; cycles error before reaching the engine (`cycle`).

### Source-binding semantics

- Bare `Position` — source defaults to `\$this`.
- `Position(name)` — source is named entity (Lookup at parse time; unknown → `unknown_ident`).
- `Position(\$var)` — source is a query variable.
- `Position(*)` — error (`bad_combination`); wildcard source is nonsensical.

### Optional-term semantics

- `?Position` lowers to `Maybe(Position)`; matches with or without the component; `Field` returns `(nil, false)` when absent.
- Must be a leading prefix only.
- Within OR: `?A || B` — the optional binds to `A` only; document this clearly.

### Equality-predicate semantics

- LHS is always `\$this` in v2; `\$var == foo` is deferred to v3.
- `==`/`!=` RHS: entity-name identifier (Lookup) or `"string literal"` (matched against Name component).
- `~` RHS: must be `"string literal"` (substring match via `NameMatches`).

### Error reporting

Preserve the v1 `pos int` + `nearby string` pattern from `@query_dsl.go:18`. Add an `ErrorCode` enum so the explorer UI can highlight specific construct types. Each parse error carries:

- `Pos int`
- `Nearby string` (5-rune window, same as v1)
- `Code` — one of: `expected_ident`, `unclosed_paren`, `unbound_var`, `cycle`, `unknown_ident`, `bad_modifier`, `bad_combination`
- `Msg string` — human-readable

### File layout

- Extend `@query_dsl.go` (v1 is 250 lines; growth is fine).
- Optionally split into `@query_dsl_v2.go` if file becomes unwieldy — agent decides.
- Extend `@query_dsl_test.go`.
- Extend `@rest_query_test.go`.

Note: the engine subsystems live in `@query.go` and `@cached_query.go`, not in separate `query_scopes.go` / `query_equality.go` / `query_var.go` files (those filenames do not exist in the repo — only their `*_test.go` counterparts do).

### Required parser tests (extend `@query_dsl_test.go`)

- `TestParse_Or_Simple` — `"A || B"` → Or(A, B)
- `TestParse_Or_TighterThanAnd` — `"A || B, C"` → AND(Or(A,B), C)
- `TestParse_Or_Chained` — `"A || B || C"` → flattened Or
- `TestParse_ScopeGroup_Not` — `"!(A, B)"` → WithoutScope([A, B])
- `TestParse_ScopeGroup_Nested` — `"!(A, !(B, C))"`
- `TestParse_ScopeGroup_RequiresNot` — `"(A, B)"` (no leading `!`) → error (must not collide with pair syntax)
- `TestParse_SourceBinding_Entity` — `"Position(hero)"` resolves `hero`
- `TestParse_SourceBinding_Unknown` — error
- `TestParse_OptionalTerm` — `"?Position"` → Maybe term
- `TestParse_OptionalNotCombination` — `"?!X"` → error
- `TestParse_TraversalUp` — `"(ChildOf, parent).Up"`
- `TestParse_TraversalSelfUp` — `".SelfUp"`
- `TestParse_TraversalCascade` — `".Cascade"`
- `TestParse_TraversalCombination` — `".Up.Cascade"` → error
- `TestParse_Variable_Simple` — `"(ChildOf, \$parent)"` → WithPairTgtVar
- `TestParse_Variable_Reused` — `"(ChildOf, \$parent), Position(\$parent)"` → shared var
- `TestParse_Variable_Cycle` — synthetic cycle → error
- `TestParse_Variable_Cap16` — 17 variables → error
- `TestParse_Equality_Entity` — `"\$this == hero"` → IsEntity
- `TestParse_Equality_String` — `"\$this == \"hero\""` → match by name
- `TestParse_Equality_NotEqual` — `"\$this != hero"` → NotEntity
- `TestParse_NameMatch` — `"\$this ~ \"prefix_*\""`
- `TestParse_AndFrom` — `"AndFrom(presetEntity)"`
- `TestParse_OrFrom` — `"OrFrom(presetEntity)"`
- `TestParse_NotFrom` — `"NotFrom(presetEntity)"`
- `TestParse_ErrorCodes` — each error code is discriminable

### Required REST integration tests (extend `@rest_query_test.go`)

- `TestRest_Query_Or` — `"Position || Velocity"` returns union
- `TestRest_Query_ScopeGroup` — `"Position, !(Velocity, Mass)"` correct subset
- `TestRest_Query_SourceBinding` — `"Position(playerEntity)"` snapshot-at-iter-start semantics
- `TestRest_Query_OptionalTerm` — `"Position, ?Velocity"`; results have `fields.Velocity` present or null
- `TestRest_Query_TraversalUp` — hierarchical setup; `(ChildOf, root).Up` returns descendants
- `TestRest_Query_Variable` — multi-hop join (spaceship → planet → star)
- `TestRest_Query_Equality_Entity` — `\$this == hero` returns just hero
- `TestRest_Query_NameMatch` — wildcard name pattern returns matching set
- `TestRest_Query_AndFrom` — preset trait expansion
- `TestRest_Query_ComplexCompound` — OR + scope + variable + traversal in one expression

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run ./...` clean
- `go test ./... -race -count=3` clean
- Coverage ≥ 95% (current baseline)
- All Phase 16.40 v1 parser tests continue to pass byte-identically

### Documentation update matrix

- `@docs/QueryDSL.md` — major extension: full BNF grammar, each new feature with examples, precedence table, error-codes table
- `@docs/FlecsRemoteApi.md` — expand `GET /query?expr=` examples with v2 syntax
- `@docs/Queries.md` — cross-link to QueryDSL.md v2 features
- `@CHANGELOG.md` — v0.96.0 entry
- `@ROADMAP.md` — bump heading; add Phase 16.41 row
- `@README.md` — feature row mention

### Target

- Version: **v0.96.0**
- Phase: **16.41**

### Non-goals

- New engine features — parser surface only
- Generator-based parser (yacc/peg) — keep hand-rolled recursive descent
- Streaming results
- Pretty-printing DSL expressions back to text
- `\$var == foo` (variable LHS in equality) — deferred to v3

## Constraints

- `@query_dsl.go` — Phase 16.40 v1 parser; extend in place. Error pattern at line 18: `query parse error at position N (near %q): msg`. Tokenizer state at lines 52-100. Preserve byte-identical v1 behavior; new error code field must be additive (existing call sites continue to work).
- `@query.go` — all targeted engine builders live here. Verified line numbers:
  - `Maybe`: 209, `Or`: 216, `IsEntity`: 221, `NotEntity`: 231, `NameMatches`: 242
  - `AndFrom`: 257, `OrFrom`: 272, `NotFrom`: 286
  - `WithoutScope`: 394 (and `ScopeBuilder` methods 328-380)
  - `Term.Up`: 419, `Term.SelfUp`: 425, `Term.Cascade`: 431
  - `Term.Source`: 439, `Term.SrcVar`: 457, `Term.TgtVar`: 478
  - `WithSourceTerm`: 498, `WithVar`: 516, `WithPairTgtVar`: 534
  - Variable cap at 2467-2481: panics if count exceeds 16. Parser must mirror this with a clean `cycle`/cap-exceeded parse error before reaching engine code.
- `@cached_query.go` — query execution path; v2 parser output must be compatible with `World.Read` iteration used by the REST handler.
- `@rest_query.go` — Phase 16.40 REST handler; `GET /query?expr=...` dispatch. Extend with v2 syntax test fixtures.
- `@rest_query_test.go` — 628 lines; v2 integration tests must keep this organized (consider helper for setup).
- `@query_dsl_test.go` — 343 lines; v1 tests must remain byte-identical.
- `@docs/QueryDSL.md` — existing v1 docs; v2 expansion will be the biggest doc change in the phase.
- `@CHANGELOG.md` — v0.95.0 entry at line 3 (current); add v0.96.0 entry above it.
- `@ROADMAP.md` — "Shipped (through v0.95.0)" at line 3 — bump to v0.96.0 and append Phase 16.41 row.
- Each DSL feature must be **verified** against the engine builder it dispatches to. If during implementation an engine signature differs from this issue's claim, fix the DSL dispatch to match the engine (not the other way around). All builders were verified present at issue-creation time.
- Hand-rolled recursive descent parser — do not introduce a parser generator.
