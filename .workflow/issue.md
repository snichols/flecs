## Goal

Add the `Wildcard` (`*`) and `Any` (`_`) query-term sentinels so users can match across an entire relationship family without enumerating concrete targets. `(Likes, Wildcard)` matches every entity that has any `Likes` pair and yields one row per concrete target; `(Likes, Any)` matches the same set but yields at most one row per entity (\"exists, don't care which\"). `(Wildcard, Bob)` matches every entity that has any relationship to `Bob`. The accompanying `MatchedTarget` / `MatchedID` / `FieldByMatch` accessors let callbacks recover the concrete pair that matched on the current row.

This continues the feature-gap drain after Phase 15.5 (Transitive). Wildcard query terms are referenced as gaps in `docs/Queries.md` (lines 678 — Wildcard / Any callout), `docs/Relationships.md` (lines 164–169, 609 — \"Wildcard pair queries\" not-yet-implemented section; line 677 — Wildcard query performance forward-look), and `docs/README.md` (line 84 — \"Wildcard / Any queries — not ported\"). C semantics ground every claim:

- `EcsWildcard` and `EcsAny` declared at `flecs.h:1742` and `flecs.h:1745`.
- Registered as core entities at `bootstrap.c:888-889` (`flecs_bootstrap_make_alive`) and `bootstrap.c:996-997` (`flecs_bootstrap_entity` with names `\"*\"` and `\"_\"`).
- Wildcard / Any are marked `EcsNotQueryable` at `bootstrap.c:1267-1268` — they are query-term annotations, not components users iterate directly.
- Query compiler distinguishes them at `compiler_term.c:1141-1180`: when `ECS_PAIR_FIRST(term->id) == EcsWildcard` or `ECS_PAIR_SECOND(term->id) == EcsWildcard`, the op becomes `EcsQueryTreeWildcard` so the engine emits one row per concrete pair.
- Eval engine handles them at `eval.c:1448` (`EcsQueryAndAny` → `flecs_query_and_any` — single-match path) and `eval.c:1489` (`EcsQueryTreeWildcard` → `flecs_query_tree_and_wildcard` — per-pair iteration).
- Reverse-lookup uses dedicated component-index buckets: `component_index.c:934-937` ensures `cr_wildcard` and `cr_any`; storage code branches on \"second is Wildcard\" vs \"first is Wildcard\" at `component_index.c:78-108`.

Semantic difference (verified in C):
- **Wildcard**: emits ONE row per concrete target. Iterating `(Likes, Wildcard)` on entity `a` with `(Likes, b)` and `(Likes, c)` yields two rows.
- **Any**: matches once per entity regardless of how many concrete targets exist (the `EcsQueryAndAny` operator short-circuits after the first hit).

### Likely shape

1. **Two new built-in entity IDs** allocated in `world.go` after `transitiveID` (index 20):
   - `World.Wildcard() ID` — index 21, `*` sentinel.
   - `World.Any() ID` — index 22, `_` sentinel.
   First user entity moves to index 23.

2. **Term-builder integration**: pair construction with `flecs.MakePair(likesID, w.Wildcard())` and `flecs.MakePair(w.Wildcard(), bobID)` produces sentinel-bearing pair IDs that the matcher recognises. No new builder DSL is required — `With` already accepts arbitrary pair IDs.

3. **Matcher integration in `query.go` and `cached_query.go`**:
   - For a term `With(MakePair(R, target))` where `target == w.Wildcard()`: walk the table's type, find every `(R, *)` pair, emit one matching row per concrete target (mirrors `EcsQueryTreeWildcard`). Cost is `O(pairs_with_R_in_table)` per matched table.
   - For `target == w.Any()`: match the table if any `(R, *)` pair exists; emit exactly one row per entity (mirrors `EcsQueryAndAny`).
   - Symmetric handling when the relationship slot is `Wildcard`/`Any`.
   - `(Wildcard, Wildcard)` means \"any pair on the entity\" — verify exact semantics against C (`bootstrap.c:457` uses `ecs_pair(component, EcsWildcard)` shape; `component_index.c:1002` references the `(Wildcard, Wildcard)` bucket).

4. **New public accessors** (likely in `scope.go` for Each-callbacks):
   - `flecs.MatchedTarget(it, termIdx) ID` — concrete target for a wildcard term on the current row.
   - `flecs.MatchedID(it, termIdx) ID` — full pair ID `(rel, target)` that matched.
   - `flecs.FieldByMatch[T](it, termIdx)` — typed accessor for component data on the matched pair (when the pair carries a value, e.g. `SetPair[Distance]`).

5. **No flags or per-id storage needed.** Wildcard/Any are query-side sentinels, not component traits — a different mechanism from Phases 15.0–15.5. There is no \"apply policy\" call and nothing analogous to `applyExclusivePolicy`. Storage-side reverse lookup buckets like C's `cr_wildcard` / `cr_any` are optional optimisations; the initial implementation can scan the table's type directly.

### Tests (new `wildcard_test.go`)

1. Wildcard target: `With(MakePair(Likes, Wildcard))` — matches all entities with any `Likes` pair, one row per concrete target.
2. Wildcard relationship: `With(MakePair(Wildcard, Bob))` — matches all entities with any relationship to `Bob`.
3. Both wildcard: `With(MakePair(Wildcard, Wildcard))` — \"any pair on the entity\" semantics; verify against C.
4. Any target: `With(MakePair(Likes, Any))` — matches once per entity regardless of target count.
5. `MatchedTarget` accessor returns the concrete target during iteration.
6. `MatchedID` accessor returns the full pair ID.
7. Combined with non-wildcard terms: `With[Position] With(MakePair(Likes, Wildcard))` — multi-term mixed matching.
8. Cached query interaction: wildcard terms in cached queries — verify cache invalidation when a new concrete pair appears.
9. Field data on wildcard pair: `SetPair[Distance]` on the pair, then `FieldByMatch[Distance]` returns the per-target value.

### Benchmarks

- `BenchmarkWildcardQuery_PairsPerEntity` measuring per-pair iteration cost as the number of distinct targets per entity scales.

### Docs updates (mandatory per CONTRIBUTING.md Phase 14.0+ rule)

- `docs/Queries.md` — replace the line 678 \"Not yet ported\" callout with shipped content; document `MatchedTarget` / `MatchedID` / `FieldByMatch`.
- `docs/Relationships.md` — turn the lines 164–169 \"Wildcard pair queries\" not-yet-implemented section into shipped docs; update line 609 cross-reference; rework the line 677 \"Wildcard query performance\" section to describe actual Go-flecs behaviour.
- `docs/ComponentTraits.md` — update line 559 \"Wildcard interaction\" note; add a short \"Query-term sentinels\" subsection clarifying that Wildcard/Any are not component traits.
- `docs/README.md` — strike line 84 from the feature-gap list.
- `CHANGELOG.md` — new `v0.38.0 — Phase 15.6` entry; update built-in entity count to 22, note user entities now start at index 23.
- `ROADMAP.md` — mark Phase 15.6 shipped; refresh built-in entity count.

### Non-goals

- No Wildcard or Any in component-set position (only target/relationship slots of pair terms).
- No Wildcard/Any in single-component (non-pair) terms — \"match any component\" is not a user-requested capability and would explode the matcher cost.
- No `EcsThis` variable binding for cross-term entity-ID joins — that is a separate phase.

### Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run ./...` clean.
- `go test ./... -race -count=3` clean.
- Coverage on main package ≥ 95%.
- Existing tests pass without modification.
- `wildcard_test.go` covers all 9 cases.
- `BenchmarkWildcardQuery_PairsPerEntity` lands in the same PR.
- Docs updates land in the same PR.

### Target version

v0.38.0.

## Constraints

- @world.go — built-in entity registration (allocate Wildcard at index 21 and Any at index 22 after `transitiveID`, following the existing `w.index.Alloc()` pattern at lines 230–246).
- @query.go — Term builder + matcher; add wildcard handling in `matchesTable` plus per-pair row emission for `Wildcard`, single-emit for `Any`.
- @cached_query.go — cached matcher mirror of the live matcher; also handle cache invalidation when new concrete pairs appear on tables already matched by a wildcard term.
- @scope.go — Each-callback surface; add `MatchedTarget` / `MatchedID` / `FieldByMatch` accessors.
- @pair_internal.go — `eachPairTarget` / `firstPairTarget` helpers; likely reused or extended for the wildcard scan path.
- @id.go — `MakePair` already exists; no new constructor needed.
- @docs/Queries.md — replace line 678 \"Not yet ported\" callout with shipped content; document the new accessors.
- @docs/Relationships.md — convert lines 164–169 and 677-onwards from forward-looking notes into shipped behaviour; update line 609.
- @docs/ComponentTraits.md — update line 559 Wildcard cross-reference; add \"Query-term sentinels\" subsection.
- @docs/README.md — strike line 84 from feature-gap list.
- @CHANGELOG.md — add `v0.38.0 — Phase 15.6` entry; bump built-in entity count to 22.
- @ROADMAP.md — mark Phase 15.6 shipped.
- @CONTRIBUTING.md — Phase 14.0+ operator directive: every phase must include explicit \"update docs accordingly\" deliverable (already incorporated above).
- Phase precedent: Phases 15.0 through 15.5 (cleanup policies, OnInstantiate, Exclusive, CanToggle, Symmetric, Transitive). 15.6 differs in kind — query-term sentinels rather than per-component traits — so do NOT follow the \"apply policy at registration\" pattern from 15.2 / 15.4 / 15.5. There is no flag bit to set on the component record.
- C grounding (see Goal section for line refs): `flecs.h:1742-1745`, `bootstrap.c:888-997, 1267-1268`, `compiler_term.c:1141-1180`, `eval.c:1448, 1489`, `component_index.c:78-108, 934-937`.
