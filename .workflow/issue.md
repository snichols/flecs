## Goal

Ship the last outstanding REST endpoint from the original Phase 14.9 gap list: `GET /query?expr=<flecs-query-expression>`. The handler parses a small Flecs Query Language subset into structured `Term`s, runs the query inside a `World.Read` scope, and returns matched entities with optional typed-component field values as JSON. This is what unblocks the upstream FlecsExplorer for the rest of its query-editor workflow.

The scope is deliberately narrow: a single-pass recursive-descent parser over a rune scanner that covers the minimum DSL surface used by the explorer UI (bare components, AND via `,`, NOT via `!`, pairs via `(R, T)`, plus `*` / `_` wildcards). Anything richer (OR, scope groups, transitive walk, variables, source binding, equality predicates, access modifiers, optional terms, `*From` keywords) is explicitly deferred to a v2 phase and called out as such in `docs/QueryDSL.md` so future contributors don't redesign from scratch.

### v1 DSL tokens

- Bare identifier (`Position`, `game.Position`) — resolved via `World.Lookup`; dotted-path supported (mirrors existing `name.go:97`).
- `,` — AND between terms.
- `!` — prefix NOT (`!Disabled`).
- `(R, T)` — pair; `R` and `T` are entity names, `*` (Wildcard), or `_` (Any).
- `*` — `World.Wildcard()` (built-in entity index 38).
- `_` — `World.Any()` (built-in entity index 39).
- Whitespace ignored between tokens.

Unknown identifier → 400 with the offending name. Parse errors carry character offset + 5-rune context window so the explorer can pin error markers in the query input.

### Explicitly deferred to v2 (documented in QueryDSL.md)

- OR (`||`)
- Negated scope groups (`!(...)`)
- Transitive walk modifiers (`Up`, `Cascade`, `SelfUp`)
- Equality predicates (`\$this == \"name\"`)
- Source binding (`Position(e)`)
- Query variables (`\$X`)
- Term-level access modifiers (`[in]`, `[inout]`, `[out]`, `[none]`)
- Optional terms (`?Position`)
- `AndFrom` / `OrFrom` / `NotFrom` (`AND` keyword)

### API surface

Internal:
- `parseQueryExpr(expr string, w *World) ([]Term, error)` — pure parser, returns `[]Term` ready to feed into `NewQueryFromTerms`. Error type carries `pos int` (rune offset) and a `nearby string` (5-rune window).
- Recursive-descent primitives: `skipWhitespace`, `parseIdent` (`[A-Za-z_][A-Za-z0-9_.]*`), `parseTerm` (handles `!`, `(R, T)`, bare ident), `parseExpr` (comma-separated terms).
- Identifier resolution happens inside `World.Read(fn)`.

REST:
- `GET /query?expr=<urlencoded-expression>` — required; missing/empty → 400.
- `?limit=N` — default 256, max 4096; non-integer or over-max → 400.
- `?offset=N` — default 0; non-integer → 400.
- `?fields=true|false` — default `true`. `false` omits the `fields` object on each result.
- Non-GET → 405. Teardown → 503.

### Response shape (200 OK)

```json
{
  \"expr\": \"Position, !Disabled\",
  \"count\": 42,
  \"limit\": 256,
  \"offset\": 0,
  \"results\": [
    {
      \"entity\": 1234,
      \"path\": \"units.archer\",
      \"fields\": {
        \"Position\": {\"X\": 1.0, \"Y\": 2.0}
      }
    }
  ]
}
```

`fields` contains only typed components named in the query terms — skips tags, NOT-terms, and dynamic components without a marshaler. Pair components key as `\"R~T\"` to match Phase 16.34's `~` URL pair encoding (consistent with `resolveComponentPaths` in `rest.go:684`).

### Pair-as-relationship choice

The DSL accepts `(*, target)` (wildcard relationship); document this as accepted (mirrors `MakePair(w.Wildcard(), target)`).

## Constraints

- @/work/agents/claude/projects/flecs/query.go — line 151 (`type Term`), line 202 (`With`), line 205 (`Without`), line 678 (`NewQueryFromTerms`). `Term` constructors here are the target IR for the parser. Note there is no exported `WithPair` helper — pair terms are constructed via `With(MakePair(r, t))` (see `doc.go:57` and `cleanup.go:36`). The issue text mentions `WithPair`; implementation should use `With(MakePair(...))` instead, or introduce a `WithPair(r, t)` helper as part of this phase if it improves readability.
- @/work/agents/claude/projects/flecs/rest.go — line 56 (`NewRESTHandler` route table), line 684 (`resolveComponentPaths` with `~` pair encoding). New `restQuery` handler registers as `mux.HandleFunc(\"GET /query\", restQuery(w))` alongside the existing `GET /component/...` handlers.
- @/work/agents/claude/projects/flecs/world.go — line 88-89 (`wildcardID`, `anyID` storage). `World.Wildcard()` / `World.Any()` exposed in @/work/agents/claude/projects/flecs/wildcard.go lines 28 and 39.
- @/work/agents/claude/projects/flecs/name.go — line 97 (`World.Lookup` — dotted-path resolution); line 52 (`LookupChild`). Parser must use this for every bare-identifier token.
- @/work/agents/claude/projects/flecs/scope.go — line 206 (`(*Reader).Lookup`). Parser must run inside `World.Read(fn)` so it sees a stable name index across multi-term queries.
- @/work/agents/claude/projects/flecs/marshal.go — component-value JSON marshaling rules used by the `fields` block; the response must mirror these rules exactly (typed → `json.Marshal(value)`; dynamic with marshaler → marshaler output; dynamic without marshaler → base64 string; tag → empty / omitted).
- @/work/agents/claude/projects/flecs/pair_internal.go and @/work/agents/claude/projects/flecs/with.go — pair construction via `MakePair`. Use `MakePair(rel, tgt)` to build pair IDs from parsed `(R, T)` tokens.
- @/work/agents/claude/projects/flecs/docs/README.md — line 90 (REST explorer gap entry: \"query DSL (`GET /query?expr=`) remains outstanding\") and line 177 (`Query execution endpoint` Phase 14.9 gap line). Both flip to ✅ shipped in v0.95.0.
- @/work/agents/claude/projects/flecs/docs/FlecsRemoteApi.md — add a `GET /query?expr=` section: parameter table, response shape, error semantics, examples.
- @/work/agents/claude/projects/flecs/docs/Queries.md — cross-link to new `docs/QueryDSL.md`.
- New file @/work/agents/claude/projects/flecs/docs/QueryDSL.md — full v1 reference: BNF-ish grammar, supported features, deferred features (with the same list as above), examples, error semantics (position + nearby window).
- @/work/agents/claude/projects/flecs/CHANGELOG.md — v0.95.0 entry, matching the format of the v0.94.0 entry at line 3.
- @/work/agents/claude/projects/flecs/ROADMAP.md — line 3 (\"Shipped (through v0.94.0)\") bumps to v0.95.0; add Phase 16.40 row using the v0.94.0 line 97 format.
- @/work/agents/claude/projects/flecs/README.md — line 271 REST API row updates to include `GET /query?expr=` + v0.95.0 version bump.

### Upstream references

- `flecs.h` — search for `ecs_query_desc_t` (the C struct that `Term` mirrors), `ecs_iter_to_json` (analogous to our `fields` block emission), `ecs_query_parse_vars` and `ecs_script_parse` (parser entry points; v1 we do not need these).
- `flecs/src/addons/script/query_dsl.c` — the upstream DSL parser. v1 reproduces a strict subset; capture line numbers when documenting which constructs were dropped.
- `flecs/docs/FlecsQueryLanguage.md` — the upstream DSL reference. Lift the subset table directly into `docs/QueryDSL.md` and mark v2 deferrals.

### Required tests

Parser unit tests (@/work/agents/claude/projects/flecs/query_dsl_test.go — new file):
- `TestParse_SingleComponent` — `\"Position\"` → 1 `With` term.
- `TestParse_TwoComponents_AND` — `\"Position, Velocity\"` → 2 `With` terms.
- `TestParse_NotPrefix` — `\"Position, !Disabled\"` → `With` + `Without`.
- `TestParse_Pair` — `\"(ChildOf, parent)\"` → pair term.
- `TestParse_PairWildcardTarget` — `\"(ChildOf, *)\"`.
- `TestParse_PairAnyTarget` — `\"(ChildOf, _)\"`.
- `TestParse_NestedSpaces` — whitespace before/after `,` `(` `)` `!` tolerated.
- `TestParse_EmptyExpr` → error at position 0.
- `TestParse_TrailingComma` → error.
- `TestParse_UnclosedParen` → error.
- `TestParse_UnknownIdentifier` → error with offending identifier in message.
- `TestParse_DottedPath` — `\"game.Position\"` resolved via `Lookup`.
- `TestParse_PairWithWildcardRel` — `\"(*, target)\"` accepted (Wildcard as relationship is valid; document the choice).
- `TestParse_MultipleNots` — `\"!A, !B\"` → both `Without` terms.

REST integration tests (@/work/agents/claude/projects/flecs/rest_query_test.go — new file):
- `TestRest_Query_SingleComponent` — entities with `Position`; `?expr=Position` returns them.
- `TestRest_Query_AND` — `?expr=Position,Velocity` returns only entities with both.
- `TestRest_Query_NOT` — `?expr=Position,!Disabled` skips disabled entities.
- `TestRest_Query_Pair_Exact` — entity with `(ChildOf, parent)`; exact-pair query returns it.
- `TestRest_Query_Pair_Wildcard` — `?expr=(ChildOf, *)` returns all children.
- `TestRest_Query_Limit` — 100 matching entities; `?limit=10` returns 10; `count=10`.
- `TestRest_Query_Offset` — `?limit=10&offset=20` returns entities 20–29.
- `TestRest_Query_FieldsFalse` — `?fields=false` omits `fields` per result.
- `TestRest_Query_FieldsDefault_True` — typed component values included.
- `TestRest_Query_FieldsForTag` — tag term; `fields` for that term is absent/empty.
- `TestRest_Query_FieldsForPair` — typed pair value keyed as `\"R~T\"`.
- `TestRest_Query_MissingExpr` → 400.
- `TestRest_Query_ParseError_Position` → 400, error body includes character position.
- `TestRest_Query_UnknownIdentifier` → 400 with offending name in body.
- `TestRest_Query_LimitInvalid` → 400.
- `TestRest_Query_LimitOverMax` → 400.
- `TestRest_Query_BadMethod` (POST) → 405.
- `TestRest_Query_ConcurrentReadsDuringWrite` — race test.
- `TestRest_Query_NoMatches` — empty `results`, `count=0`, status 200.

### Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run ./...` clean.
- `go test ./... -race -count=3` clean.
- Coverage ≥ 95.0% (current baseline from v0.39.0; do not regress).

### Non-goals

- OR / scope groups / transitive walk / up traversal — explicit v2 phase.
- Iterator-to-JSON streaming (chunked transfer) — single response, capped by `?limit`.
- Query result caching — the cached query API (Phase 16.4 sorted, 16.11 grouped) builds cached queries, not used here.
- Pretty-printed output / formatting toggle.

### Notes

- Target version: **v0.95.0**. Phase number: **16.40**.
- Verify no existing identifier conflicts: there is no existing `ParseQuery`, `parseQuery`, `QueryDSL`, or `QueryParse` symbol in the codebase (grep verified).
- Pair encoding in JSON output MUST match Phase 16.34's `~` convention for consistency (see `rest.go:684` `resolveComponentPaths`).
- The parser MUST report parse errors with character offset and a small context window — the explorer UI surfaces these to the user inline in the query input.
