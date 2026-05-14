## Goal

Port the upstream C flecs **equality predicates** (`EcsPredEq` / `EcsPredMatch` / `EcsPredLookup`) to Go-flecs as three new query term kinds: per-entity identity equality, identity inequality, and case-insensitive substring name match.

Today, Go-flecs query terms match entities purely by component presence (`With` / `Without` / `Maybe` / `Or` / `WithoutScope`). They cannot express "match only this specific entity" or "match entities whose name contains a substring." This phase closes the `docs/README.md` line 112 gap.

In upstream flecs query DSL these are written `$this == Foo`, `$this != Foo`, and `$this ~= "partial"`. The Go-side surface is a builder API:

- `flecs.IsEntity(e ID) Term` — `TermEq`. Match only when the iterated entity is `e`.
- `flecs.NotEntity(e ID) Term` — `TermNotEq`. Match every entity *except* `e`.
- `flecs.NameMatches(pattern string) Term` — `TermNameMatch`. Match entities whose `Name.Value` contains `pattern` (case-insensitive substring; mirrors upstream `flecs_query_match_substr_i`).

These compose orthogonally with all existing term kinds — `flecs.With(posID), flecs.NotEntity(playerEntity)` iterates every Position-holder except the player.

### Upstream C references (verified)

- `include/flecs.h:1979-1986` — the three predicate marker constants:
  - `EcsPredEq` (`$var == ...` entity-identity).
  - `EcsPredMatch` (`$var ~= "pattern"` substring match — note: the source comment swap on lines 1983/1986 is misleading; the actual semantics are confirmed via `eval_pred.c` below).
  - `EcsPredLookup` (`$var == "name"` — name-string equality lookup, not in scope for this phase).
- `src/query/compiler/compiler_term.c:640-685` — compile-time lowering. `EcsPredEq` lowers to `EcsQueryPredEq`/`EcsQueryPredNeq` (or `…EqName`/`…NeqName` when the RHS is a name string); `EcsPredMatch` lowers to `EcsQueryPredEqMatch`/`EcsQueryPredNeqMatch`. Operator polarity is selected by `term->oper == EcsNot`.
- `src/query/compiler/compiler_term.c:652-660` — exact kind-selection logic to mirror in Go.
- `src/query/engine/eval_pred.c:8-41` — `flecs_query_match_substr_i`: **case-insensitive substring scan** (not regex, not glob). Empty match string returns `true` (matches every named entity).
- `src/query/engine/eval_pred.c:103-209` — `flecs_query_pred_eq` / `flecs_query_pred_neq`: range-based. Eq narrows the source range to a single slice of one row; Neq returns the two slices flanking the excluded row (or one slice if the excluded entity sits at an edge). Mirror this two-slice technique in `Each*` iteration so the per-entity loop stays branchless.
- `src/query/engine/eval_pred.c:212-303` — `flecs_query_pred_match`: scans names per-entity, looks up the Name column via `ecs_pair(ecs_id(EcsIdentifier), EcsName)` (line 243). When the table has no Name column → returns `is_neq` (line 246), i.e. unnamed entities never match `~=` and always match `!~`.
- `src/query/validator.c:442-474` — validator rules: both sides must not be entities; LHS must be a variable; for `EcsPredMatch` the RHS must be a name string. We adopt only the parts that map to a structured-term API (no string/variable LHS in Go-flecs — LHS is always `$this`).

**Performance note:** upstream has no special optimization for `$this != Foo` beyond the two-slice trick — the excluded entity's table is split into two contiguous spans. We mirror this; non-excluded tables fast-pass at the table level.

## Constraints

- @docs/README.md — line 112 is the gap entry to flip: `**Equality operators** — \`\$this == Foo\`, \`\$this != Foo\`, \`\$this ~= "partial"\` name-match filter terms in the Flecs Query Language. not yet ported in Go flecs.` After this phase: `✅ shipped in v0.76.0`.
- @ROADMAP.md — heading `Shipped (through v0.75.0)` bumps to `Shipped (through v0.76.0)`; new Phase 16.21 bullet appended at line 80.
- @CHANGELOG.md — new `## v0.76.0 — <date> — Phase 16.21: Query equality operators` entry at the top, before the v0.75.0 entry.
- @query.go — defines `TermKind` (lines 62-84), `Term` struct (lines 109-141), and the existing `With`/`Without`/`Maybe`/`Or`/`WithoutScope` builders. Three new `TermKind` constants (`TermEq`, `TermNotEq`, `TermNameMatch`) extend the enum after `TermScope = 4`. Three new builders mirror the shape of `With` (lines 143-158).
- @query.go — `validateAndSortTerms` (line 1756) enforces "at least one TermAnd is required." Equality terms are filters, not seeds — they MUST NOT count toward the TermAnd seed requirement. Mirror the existing fixed-source / TermNot rules around line 1762-1799.
- @query.go — `matchesSparseTerms` (line 1029) is the per-entity evaluation hook. `TermNameMatch` slots in here as a new branch; `TermEq` / `TermNotEq` are evaluated alongside in the same loop. Mirror the `TermScope` branch (lines 1070-1083).
- @query.go — `Term.Pattern string` is a new field for `TermNameMatch` (or store the pattern in the `Sub []Term` slot is wrong — use a dedicated field). Choose a field name compatible with the existing JSON-comment style on lines 110-141.
- @cached_query.go — `tryMatchTable` (line 620) handles archetype-level pre-filtering. `TermEq` table fast-skip: if the target entity's table doesn't match this table, drop the whole table. Mirror the implicit-skip pattern at lines 632-637 (Disabled/Prefab) but per-cached-query, not per-world.
- @cached_query.go — `Changed()` (line 788) ties cached queries to change detection. `TermNameMatch` needs an `OnSet[Name]` subscription (the Name component is at `w.nameID`; observer wiring uses `ObserveID` per @observer.go:160). `TermEq` cache invalidates when the named entity migrates tables (use the existing per-table `ChangeCount` already wired in at lines 838-844 — no new observer needed if the named entity's *table* changes is detected by the table-creation hook; but a migration that keeps the same table needs explicit handling — call out as an open question in implementation).
- @query_filters.go — Phase 16.2 reference for special-purpose query filters with per-table fast-skip. Equality terms follow the same shape but operate per-entity (for `TermEq` after table match, and always for `TermNameMatch`).
- @name.go — `World.Name() ID` at line 16 returns the Name component entity. `World.GetName(e ID) (string, bool)` at line 30 returns `("", false)` for unnamed entities. The `Name` struct (line 10) has `Value string`. `TermNameMatch` evaluation calls `w.GetName(e)` per entity in the matched table.
- @observer.go — `EventOnSet` (line 16) and `ObserveID` (line 160) provide the OnSet hook needed for `TermNameMatch` cache invalidation. Cached queries with `TermNameMatch` register an `OnSet[Name]` observer at construction; the observer marks the cached query as changed.
- @CONTRIBUTING.md — doc-update protocol: every shipped phase updates `docs/<Manual>.md`, `docs/README.md`, top-level `README.md`, `CHANGELOG.md`, and `ROADMAP.md`.

### Vision-derived constraints

- Faithful port of upstream C flecs semantics, with Go-idiomatic shape where ergonomics improve. Substring (not regex, not glob) matches upstream `flecs_query_match_substr_i` exactly — do not invent richer semantics.
- Always-on safety: panic at construction time on invalid inputs (zero ID, etc.) rather than runtime no-yield.

## Deliverables

### Term kinds (query.go)

Three new `TermKind` constants:

- `TermEq TermKind = 5` — matches when iterated entity == target entity.
- `TermNotEq TermKind = 6` — matches when iterated entity != target entity.
- `TermNameMatch TermKind = 7` — matches when iterated entity's `Name.Value` contains the pattern (case-insensitive substring).

Extend `TermKind.String()` (line 87) with cases for the three new kinds.

Extend `Term` struct (line 109) with a `Pattern string` field used only when `Kind == TermNameMatch`. Document zero-value: `""` is a valid pattern (matches every named entity, mirroring upstream).

### Builder functions (query.go)

```go
// IsEntity returns a TermEq term: matches only the named entity. Panics if e is 0.
func IsEntity(e ID) Term { /* … */ }

// NotEntity returns a TermNotEq term: matches every entity except the named one. Panics if e is 0.
func NotEntity(e ID) Term { /* … */ }

// NameMatches returns a TermNameMatch term: matches entities whose Name.Value
// contains pattern (case-insensitive substring). Empty pattern matches every
// named entity. Unnamed entities never match.
func NameMatches(pattern string) Term { /* … */ }
```

Mirror behavior in `ScopeBuilder` (lines 163-213): add `.IsEntity(e)`, `.NotEntity(e)`, `.NameMatches(pattern)` methods so equality terms compose inside `WithoutScope`.

### Validator updates (query.go validateAndSortTerms)

- The "at least one TermAnd required" check (line 1762-1768) is unchanged — equality terms are filters, not seeds, and cannot satisfy it.
- A query of only equality terms (no `TermAnd`) panics with the existing message.
- `TermEq` / `TermNotEq` with `ID == 0` panic at builder time (not at validator time — fail fast).
- Add an entry to the sort order: equality terms sort after `TermOptional`, before / after `TermScope` per consistency rule (place them between `TermOr` and `TermOptional` so they evaluate before optional column fetch but after archetype intersection).
- `TermEq` / `TermNotEq` MAY NOT carry `.Up()` / `.SelfUp()` / `.Cascade()` / `.Source()` — panic at validator time if any traversal or fixed source is set.
- `TermNameMatch` same restrictions.

### Table-level evaluation (cached_query.go tryMatchTable)

- `TermEq`: read the target entity's record (`w.index.Get(targetID)`); if `rec == nil` or `rec.Table != t`, drop this table. If it matches, keep the table — per-entity filter narrows down to the single row.
- `TermNotEq`: no table-level optimization needed — the excluded entity contributes at most one row to be skipped.
- `TermNameMatch`: no table-level optimization. A future enhancement could fast-skip tables with no Name column (`!t.HasComponent(w.nameID)`); include this as a doc'd optimization.

### Per-entity evaluation (query.go matchesSparseTerms)

Extend the per-entity loop (line 1029) with branches for `TermEq`, `TermNotEq`, `TermNameMatch`:

```go
case TermEq:
    if e != term.ID {
        return false
    }
case TermNotEq:
    if e == term.ID {
        return false
    }
case TermNameMatch:
    name, ok := it.world.GetName(e)
    if !ok {
        return false // unnamed entities never match
    }
    if !substrMatchCaseInsensitive(name, term.Pattern) {
        return false
    }
```

The helper `substrMatchCaseInsensitive(name, pattern string) bool` mirrors `flecs_query_match_substr_i` (`eval_pred.c:19-41`):
- Empty pattern → return true.
- Otherwise scan `name` for any starting position from which `pattern` matches case-insensitively.

### Cached-query change detection (cached_query.go Changed)

- `TermEq`: invalidate when the target entity migrates. The existing `lastChangeCounts[t]` mechanism (lines 838-844) catches changes within the matched table. For migration *out of* the matched table, the cache rebuild on `tryMatchTable` (called from `notifyTableCreated`) handles new-table emergence, but migration to a different existing table needs explicit handling. Approach: subscribe to `EventOnAdd` for the target entity (fixed-source observer per Phase 16.12; see @query.go) and mark the cached query as changed when fired. Acceptable alternative: document staleness like Phase 15.7 (Reflexive) did.
- `TermNotEq`: per-iteration filter only; no cache observation needed.
- `TermNameMatch`: register `OnSet[Name]` observer at cached-query construction; observer signals `cq.tablesAdded = true` (re-use existing mechanism). Unsubscribe on `Close`.

### Tests (query_equality_test.go) — minimum 12 cases

1. `IsEntity(foo)` + `With(posID)` where `foo` has Position → yields only `foo`.
2. `IsEntity(foo)` + `With(posID)` where `foo` has no Position → yields nothing.
3. `IsEntity(foo)` + `With(posID)` where `foo` is dead → yields nothing (no panic).
4. `NotEntity(foo)` + `With(posID)` → yields all Position-holders except `foo`.
5. `NotEntity(foo)` + `With(posID)` where `foo` doesn't have Position → yields all Position-holders (no excluded row).
6. `NameMatches("Player")` against `{Player1, Player2, Enemy, unnamed}` → yields `Player1, Player2`.
7. `NameMatches("PLAYER")` against same set → yields `Player1, Player2` (case-insensitive).
8. `NameMatches("nothing")` → yields nothing.
9. `NameMatches("")` → yields every entity with a non-empty name.
10. Composition: `With(velID), NameMatches("Player"), NotEntity(deadPlayer)` — three-way intersection.
11. Cached query with `IsEntity`: re-execute after target gains the component; cache reflects.
12. Cached query with `NameMatches`: rename an entity to match the pattern; cache reflects on next execution.
13. Edge: `IsEntity(0)` → panic at construction.
14. Edge: `NotEntity(0)` → panic at construction.
15. Edge: `IsEntity(e).Up(rel)` → panic at validator time.
16. Edge: `IsEntity(e).Source(other)` → panic at validator time.
17. Composition with `WithoutScope`: `WithoutScope(b => b.NotEntity(foo))` — sub-expression `NotEntity(foo)` is true for every entity except `foo`; negating it means the scope passes only for `foo`. Verify three-row scenario.
18. Coverage ≥ 95.0%.

### Documentation updates

- `docs/Queries.md` — new section `§ Equality and name-match filters` with three examples (one per builder) and the substring semantics callout.
- `docs/README.md` — flip line 112 to `✅ shipped in v0.76.0` with link to `IsEntity` / `NotEntity` / `NameMatches`.
- `README.md` — feature list bump in the Queries section.
- `CHANGELOG.md` — `## v0.76.0 — <date> — Phase 16.21: Query equality operators` entry with C-reference citations (`include/flecs.h:1979-1986`, `src/query/engine/eval_pred.c:8-303`).
- `ROADMAP.md` — heading bumps to `Shipped (through v0.76.0)`; Phase 16.21 bullet inserted after Phase 16.20.

## Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0% (verify via `go test -cover ./...`)
- No regression on existing query tests (especially `query_scope_test.go`, `query_fixed_source_test.go`, `query_terms_test.go`)

## Explicit non-goals

- **No regex, no glob.** Substring only, mirroring `flecs_query_match_substr_i`. State this in the `NameMatches` doc comment.
- **No `<` / `>` / `<=` / `>=` ordering predicates** — not in upstream either; out of scope.
- **No component-value comparison** (e.g., `Position.X > 100`) — that is a separate concern (predicates on components, not entities).
- **No `EcsPredLookup` port** (`$this == "name"`) — overlaps with the existing `World.Lookup(path)` API; adding it as a term kind brings little value over a single `Lookup` + `IsEntity` chain. State in CHANGELOG as a deliberate omission.
- **No `$Var` query variables** (gap line 109) — separate future phase.

## Locked decisions

1. **NameMatches semantic**: case-insensitive substring (mirrors `flecs_query_match_substr_i` in `eval_pred.c:19-41`). NOT glob, NOT regex.
2. **NameMatches on unnamed entity**: no match. Mirrors upstream `eval_pred.c:246` which returns `is_neq` when the table has no Name column.
3. **NameMatches empty pattern**: matches every named entity. Mirrors `flecs_query_match_substr_i` line 23-25: `if (!match[0]) return true`.
4. **IsEntity(0) / NotEntity(0)**: panic at construction in the builder function (not at validator time — fail fast at the call site).
5. **TermEq / TermNotEq with traversal or fixed source**: panic at validator time.
6. **TermEq / TermNotEq does NOT satisfy the "at least one TermAnd required" check.** Equality terms are filters, not seeds.

## Process

- Feature, not bug.
- Label: `snichols/queued`.
- Return the issue number.
