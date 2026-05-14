## Goal

Port the three type-list expansion operators from upstream C flecs as new query term kinds in Go flecs: `AndFrom(source)` / `OrFrom(source)` / `NotFrom(source)`. Each takes a source entity and expands that entity's component list (its type vector) into an implicit set of inner terms:

- **`AndFrom(source)`** — match entities that have ALL of `source`'s components (expand to N `With` terms).
- **`OrFrom(source)`** — match entities that have AT LEAST ONE of `source`'s components (expand to one Or-group of N terms).
- **`NotFrom(source)`** — match entities that have NONE of `source`'s components (expand to N `Without` terms).

Canonical use case: prefab type-lists. A prefab `EnemyTemplate` carrying `Health + Speed + AIBehavior` lets you write `flecs.AndFrom(enemyTemplateID)` to match "everything that looks like an enemy" without naming each component manually.

This closes `docs/README.md` line 113 (the only remaining Phase 14.2 query-gap entry that is still in scope for this codebase — `$Var` query variables, line 109, are a separate future phase).

Target version: **v0.77.0** (next after `v0.76.0` query equality operators, shipped at `c3c9f6a`).

### Upstream C references

- `include/flecs.h:723-725` — `EcsAndFrom`, `EcsOrFrom`, `EcsNotFrom` constants in the `ecs_oper_kind_t` enum.
- `src/query/compiler/compiler_term.c:1225-1251` — compile-time validation: looks up the source's type with `ecs_get_type(world, term->id)`, walks the type array, and skips IDs flagged `EcsIdOnInstantiateDontInherit` (this is how Prefab tags get filtered out from the expansion automatically).
- `src/query/compiler/compiler_term.c:1090-1096` — lowering of `EcsAndFrom` / `EcsOrFrom` / `EcsNotFrom` into engine ops `EcsQueryAndFrom` / `EcsQueryOrFrom` / `EcsQueryNotFrom`.
- `src/query/engine/eval.c:427-440` — `flecs_query_next_inheritable_id` skips IDs with `EcsIdOnInstantiateDontInherit` (Prefab/Disabled when bootstrapped).
- `src/query/engine/eval.c:443-589` — `flecs_query_x_from` shared engine implementation for all three operators. Critically, this re-fetches `r->table->type` on every invocation, so **upstream is live, not snapshot** — the Go port deliberately diverges here (see Design decisions below).
- `src/query/engine/eval.c:592-616` — thin dispatchers `flecs_query_and_from`, `flecs_query_not_from`, `flecs_query_or_from`.
- `src/query/engine/eval.c:1458-1460` — op-kind dispatch.
- `test/query/src/Operators.c:9670-9801` — upstream behavioural tests (`Operators_and_from_existing_and_new_table`, `_not_from_*`, `_or_from_*`). Confirms prefab-as-source works and that Prefab tag itself is excluded from expansion.
- `test/query/src/Validator.c:1758-1805` — validator round-trip for the three opers.
- `include/flecs/addons/cpp/c_types.hpp:54-56` — C++ binding enum mirrors.

### Design decisions (locked in; document in CHANGELOG)

1. **Snapshot at construction (NOT live).** Read `Reader.EntityComponents(source)` once in `NewQueryFromTerms` / `NewCachedQueryFromTermsWithOptions` / `validateAndSortTerms`. Cache the expanded inner terms on the Term. Subsequent component changes to `source` do NOT affect the query. Rationale: matches the "queries are immutable plans" mental model already established for traversal terms (`query.go:18-21`); live expansion would invalidate cached plans on every source mutation, defeating the cache. If callers need live behaviour, they re-construct the query. **This deliberately diverges from upstream C, which re-reads the type at every iteration (`eval.c:462`).** Document the divergence in CHANGELOG and `docs/Queries.md`.
2. **Empty source type → vacuous semantics.** `AndFrom` matches everything (vacuous truth), `OrFrom` matches nothing (no disjuncts), `NotFrom` matches everything (no exclusions). Document and test.
3. **Source-as-Prefab is fine; Prefab/Disabled tags inside the source's type are EXCLUDED from expansion.** The source entity is often a prefab itself, so its own Prefab tag would otherwise leak into the expanded terms and force matched entities to also be prefabs — wrong. Filter via `w.instantiatePolicies[cid] & policyOnInstantiateDontInherit` (the Go equivalent of upstream's `EcsIdOnInstantiateDontInherit` filter at `eval.c:435` and `compiler_term.c:1241`). This implicitly excludes Prefab (bootstrapped DontInherit at `world.go:551-555`) and matches upstream behaviour observed in `test/query/src/Operators.c:9670-9706`.
4. **Pair components in source's type ARE included** in expansion. The type vector contains all IDs uniformly; pairs are just IDs. Test with a source that has `(ChildOf, parent)`.
5. **Source must be alive at construction** — panic via the existing fixed-source validation pattern at `query.go:1929-1931`. The Term carries the source entity ID for diagnostics.

### Deliverables

1. **New term kinds in `query.go`** — `TermAndFrom`, `TermOrFrom`, `TermNotFrom` (values 8, 9, 10). Update `TermKind.String()`.
2. **Builder functions**:
   - `flecs.AndFrom(source ID) Term` → `Term{Kind: TermAndFrom, Src: source}` (Src holds the *source whose type to expand*, not a fixed-source semantic — clarify in doc comment).
   - `flecs.OrFrom(source ID) Term`.
   - `flecs.NotFrom(source ID) Term`.
   - Panic if `source == 0`.
3. **Expansion strategy (snapshot)** — in `validateAndSortTerms` (`query.go:1875`), before the OR-group scan, walk the input terms and replace each `TermAndFrom` / `TermOrFrom` / `TermNotFrom` with the expanded inner terms. The snapshot reads `Reader.EntityComponents(t.Src)` (`scope.go:244-256`), filters out IDs whose component has `policyOnInstantiateDontInherit` (`isa.go:74`), then:
   - `TermAndFrom` → N `TermAnd` terms.
   - `TermOrFrom` → one OR-group of N terms (N consecutive `TermOr` terms, then a non-Or terminator to close the group; if N == 1, emit `TermAnd` to keep `validateAndSortTerms`' "at least one TermAnd" precondition happy without a degenerate single-element OR group).
   - `TermNotFrom` → N `TermNot` terms.
   - Empty filtered type: `TermAndFrom` and `TermNotFrom` expand to nothing (vacuous); `TermOrFrom` expands to nothing AND must produce a zero-match query — represent this by appending a sentinel `TermNot` on the wildcard or any always-present ID? Cleaner: when an `OrFrom` source has an empty inheritable type, panic-on-construction is wrong (it's a legal runtime state); instead, the expansion adds a single `Term{Kind: TermAnd, ID: w.alwaysFalseID()}` — but Go flecs has no such sentinel. **Decision**: implement empty-`OrFrom` by inserting a `TermNot` on an ID that is guaranteed present in every table's lookup (e.g., `w.anyID` doesn't fit). Cleanest: track an `alwaysFalse` flag on the query plan that short-circuits `Iter` to return zero results. Confirm this approach in implementation review.
   - Source-doesn't-exist: panic with `"<caller>: AndFrom/OrFrom/NotFrom source entity <e> is dead or non-existent"` (mirror message at `query.go:1929`).
4. **Validator placement** — `validateAndSortTerms` must run expansion BEFORE the existing "at least one TermAnd is required" check; otherwise a query containing only `AndFrom` would falsely fail validation. After expansion, run normal validation on the expanded terms.
5. **Sort order** — expanded terms participate in the normal And/Not/Or sort just like any other term. The original `*From` term is consumed and does not appear in the final term slice.
6. **No CachedQuery special handling** — once expanded at construction, the resulting term slice is indistinguishable from a hand-written query. Existing cache invalidation (table ChangeCount) handles re-execution correctly. Snapshot semantics mean type changes to `source` post-construction are ignored, which IS the intended behaviour.
7. **Tests in `query_from_test.go`** — at least 10 cases (see Test plan below). Coverage ≥ 95.0%.
8. **Doc updates** per CONTRIBUTING.md:
   - `docs/Queries.md` — add § AndFrom / OrFrom / NotFrom under ## Operators, with examples and the snapshot-at-construction note (call out the deliberate divergence from upstream's live behaviour).
   - `docs/README.md` — flip line 113 from "not yet ported" to ✅ shipped (v0.77.0) with a link to the new Queries.md section.
   - `README.md` — feature-list bump.
   - `CHANGELOG.md` — v0.77.0 entry at top, following the v0.76.0 format (Added / Behaviour / Upstream C references / Deliberate non-goals).
   - `ROADMAP.md` — heading bump to "Shipped (through v0.77.0)" and append the v0.77.0 line item in the same style as `v0.76.0, Phase 16.21` at line 80.

### Test plan (≥ 10 cases in `query_from_test.go`)

- [ ] Prefab `EnemyTemplate` carrying `Health + Speed + AIBehavior` — `AndFrom(enemyTemplate)` matches entities with all three.
- [ ] Same query: entity with only `Health + Speed` (missing `AIBehavior`) does NOT match.
- [ ] `OrFrom(enemyTemplate)` matches entities with at least one of `Health`, `Speed`, `AIBehavior`.
- [ ] `NotFrom(enemyTemplate)` matches entities with none of `Health`, `Speed`, `AIBehavior`.
- [ ] Composition with normal terms: `flecs.With(posID), flecs.AndFrom(enemyTemplate)` — entities with `Position` AND all enemy components.
- [ ] Snapshot semantics: add a new component to `source` AFTER query construction; query results unchanged. Reconstruct → new component is now in the expansion.
- [ ] Empty source type: `AndFrom(empty)` → matches all entities (vacuous), `OrFrom(empty)` → matches none, `NotFrom(empty)` → matches all.
- [ ] Source itself is a prefab: its `Prefab` tag is excluded from the expansion (otherwise the AndFrom query would only match other prefabs — wrong). Verify with a non-prefab entity matching `AndFrom(prefab)`.
- [ ] Source has a Disabled tag in its type: also excluded.
- [ ] Source doesn't exist: panic at construction with the documented message.
- [ ] `CachedQuery` with `AndFrom`: re-execution uses the snapshot; standard cache invalidation on regular component changes still triggers correctly via `Changed()`.
- [ ] Pair components in source's type ARE included in expansion: source has `(ChildOf, parent)`, `AndFrom(source)` matches entities that are also children of `parent` (plus all other expanded ids).
- [ ] `go vet ./...` clean.
- [ ] `golangci-lint run` clean.
- [ ] `go test ./... -race -count=3` passes.
- [ ] Coverage ≥ 95.0% on `query.go`.

### Explicit non-goals

- **Live expansion on source type changes** — explicit re-construction required (deliberate divergence from upstream).
- **`AndFromQuery` / `OrFromQuery` operators** that expand from another query's matched components — too meta, out of scope.
- **`$Var` query variables** — separate phase (`docs/README.md` line 109).
- **Empty-OrFrom as a noop instead of zero-match** — upstream treats empty-type *From as a noop (`compiler_term.c:1231: ctx->skipped++`); Go flecs follows set-theoretic semantics where empty `OrFrom` = empty disjunction = false. Discrepancy is documented.

## Constraints

- @docs/README.md — gap entry at line 113 flips to ✅ shipped (v0.77.0) on completion; preserve the format of adjacent shipped entries (lines 108, 110, 112, 114).
- @query.go — `Term`, `TermKind`, builder functions, and `validateAndSortTerms` live here; expansion is plugged into `validateAndSortTerms` before the OR-group scan (line 1934) so expanded terms participate in normal validation and sorting. New term kinds must update `TermKind.String()` (line 101).
- @scope.go — `Reader.EntityComponents(e ID) []ID` (lines 244-256) is the type-inspection primitive that returns the source's component list. Use this; do NOT reach into `rec.Table.Type()` directly from query.go.
- @isa.go — `policyOnInstantiateDontInherit` and `w.instantiatePolicies[cid]` (lines 73-74, 117-119) are the Go equivalent of upstream's `EcsIdOnInstantiateDontInherit` filter. Use the same gating to exclude Prefab/Disabled tags from expansion.
- @cached_query.go — no special handling needed once expansion happens at construction; existing change detection (line 121: sparse versions, line 423: `Changed()`) covers post-construction component mutations on matched entities.
- @docs/Queries.md — add a new § AndFrom / OrFrom / NotFrom subsection under ## Operators (after § Or at line 286), and update the gap-table entry at line 1231 to point to the new section. Follow the doc style of § Query scopes (line 196) and § Equality and name-match filters (line 984).
- @CHANGELOG.md — v0.77.0 entry at top, mirroring the v0.76.0 entry structure (lines 3-37): Added / Behaviour / Upstream C references / Deliberate non-goals.
- @ROADMAP.md — bump heading at line 3 to "through v0.77.0"; append a line item in the format of the v0.76.0 entry at line 80.
- @CONTRIBUTING.md — follow the version-bump checklist (CHANGELOG + ROADMAP + README + docs/README.md + docs/Queries.md, all in one commit).
- `go vet ./...`, `golangci-lint run`, and `go test ./... -race -count=3` must all be clean before commit. Coverage ≥ 95.0%.
