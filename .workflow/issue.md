## Goal

Ship the **Disabled** and **Prefab** built-in tags as a bundle (`v0.57.0`, Phase 16.2). Both tags share the same query-filter machinery: an entity that carries either tag is excluded from ordinary queries by default; queries opt in to seeing such entities by mentioning the tag explicitly in their term list.

Bundling these two gaps captures the shared per-table filter cost once. The implementation closes two gaps tracked in `docs/README.md`:

- Line 100 (Phase 14.1 Entities gap): **Entity disabling** (`Enable` / `Disable`).
- Line 135 (Phase 14.5 Prefabs gap): **Prefab tag** (`EcsPrefab`).

Both flip to shipped (v0.57.0) on landing.

### Surface area

**Disabled**
- `w.Disabled() ID` — built-in entity (new index 36).
- `Disable(w *Writer, e ID)` — adds the `Disabled` tag to `e`. Idempotent.
- `Enable(w *Writer, e ID)` — removes the `Disabled` tag from `e`. No-op if not disabled.
- `IsDisabled(s scope, e ID) bool` — predicate over current archetype membership.

**Prefab**
- `w.Prefab() ID` — built-in entity (new index 37).
- `MarkPrefab(w *Writer, e ID)` — adds the `Prefab` tag. Idempotent. (See open decision 1.)
- `IsPrefab(s scope, e ID) bool`.

**Query filtering** — ordinary `NewQueryFromTerms` / `NewCachedQueryFromTerms` silently exclude entities carrying `Disabled` or `Prefab` unless the query explicitly mentions the tag in any term form (`With`, `Without`, `Maybe`/`Optional`, `Or`).

### C upstream verification (`/work/agents/claude/projects/SanderMertens/flecs`)

Verified before filing:

- `EcsPrefab` declared at `include/flecs.h:1921`; `EcsDisabled` at `include/flecs.h:1925`. Sibling tags `EcsNotQueryable` (`:1930`) and `EcsModule` (`:1917`) follow the same shape.
- Bootstrap registration: `flecs_bootstrap_tag(world, EcsPrefab)` at `src/bootstrap.c:972`; `flecs_bootstrap_tag(world, EcsDisabled)` at `src/bootstrap.c:974`.
- DontInherit wire-up for Prefab: `ecs_add_pair(world, EcsPrefab, EcsOnInstantiate, EcsDontInherit)` at `src/bootstrap.c:1308` — confirms the \"prefab tag is not inherited by IsA instances\" semantics.
- **Per-table flags, not per-entity**: when an entity acquires `EcsPrefab` or `EcsDisabled`, the archetype table sets `EcsTableIsPrefab` / `EcsTableIsDisabled`. See `src/storage/table.c:257-260`. Confirmed by the brief's expectation.
- **Exclusion at SELECT**: query evaluator filters with mask `(EcsTableNotQueryable|EcsTableIsPrefab|EcsTableIsDisabled)` at `src/query/engine/eval.c:88,183,496`; same mask in `src/query/engine/trivial_iter.c:85,151`, `eval_tree.c:146,158,739`, `eval_up.c:163,222,258`, `eval_sparse.c:591,641`, `eval_trav.c:69,172`. **The check is a single bitmask test per table — O(1) per table, dramatically faster than per-entity filtering.**
- **Opt-in to include, default skip**: `src/query/validator.c:1238-1243` sets `EcsQueryMatchPrefab` / `EcsQueryMatchDisabled` on the query iff the term list mentions the tag. The compiler then clears the corresponding bit from the table-skip mask at `src/query/compiler/compiler_term.c:815-829`.
- **Traversal vs. select**: `eval_up.c` still applies the skip mask in `SelectId` calls (`:163, :222, :258`) — but the traversal walk over IsA edges itself does not consult the mask. Prefabs continue to serve as `IsA` bases; only their _direct_ matching is suppressed.

### Go-side state verified

- `world.go` built-in slot map (lines 75–88 + 133–172 doc comment): current top three are `Sparse(34)`, `DontFragment(35)`, `Wildcard(36)`, `Any(37)`. User entities begin at index 38. `meta_test.go:19` pins `builtinEntityCount = 37`.
- Allocation block at `world.go:380-403` is the insertion point: new allocations for `Disabled` and `Prefab` must slot **before** `Wildcard` so the sentinels keep their place at the tail.
- Bare-tag → policy hooks live in `id_ops.go:57-203` (`addIDImmediate`) and the matching removal path begins at `id_ops.go:366`. The bare-tag patterns at `:114-203` (Exclusive, CanToggle, Symmetric, Transitive, Reflexive, Acyclic, Final, OneOf, Singleton, WriteOnce, Traversable, Relationship, Target, Trait, PairIsTag, …) show the conventional shape.
- Marshal skip-set at `marshal.go:122-160` needs two new entries (`w.Disabled()`, `w.Prefab()`).
- Query path: `validateAndSortTerms` at `query.go:1257`; `matchesTable` at `query.go:854-943`. Cached mirror in `cached_query.go` (705 lines).
- Read-scope predicate convention: `IsFinal(s scope, …)` at `final.go:29`; `IsSparse(s scope, …)` at `sparse.go:101`. Match this shape for `IsDisabled` / `IsPrefab`.
- Most recent file pattern: `entity_lifecycle.go` (Phase 16.1, v0.56.0). New file `query_filters.go` should follow that header style and bundle both tags together (Disabled + Prefab share the filter logic).

## Constraints

- @docs/README.md — lines 100 and 135 are the gap entries this issue closes; both flip to shipped (v0.57.0) with anchor links to the docs sections added below.
- @CONTRIBUTING.md — full doc-update checklist: `docs/EntitiesComponents.md`, `docs/PrefabsManual.md`, `docs/Queries.md`, `docs/README.md`, `README.md`, `CHANGELOG.md`, `ROADMAP.md`.
- @world.go — extend the built-in allocation block at lines 380-403; update doc comment at lines 133-172 (slot reshuffle: Disabled=36, Prefab=37, Wildcard→38, Any→39). User entities now start at 40.
- @meta_test.go — line 19 baseline: `builtinEntityCount` 37 → 39. Update the descriptive comment at lines 11-18 to enumerate Disabled(36) and Prefab(37) and shift Wildcard/Any.
- @marshal.go — extend skip-set at lines 122-160 with `w.Disabled()` and `w.Prefab()`.
- @id_ops.go — extend `addIDImmediate` (lines 57-203) and `removeIDImmediate` (line 366+). **However**: Disabled and Prefab are pure archetype-stored tags, not policies. Unlike Final/Singleton/WriteOnce they do **not** need a per-entity `policies` map: archetype membership (`t.HasComponent(disabledID)`) is the source of truth. The bare-tag hooks may therefore be a no-op for these two — verify and document. The filter logic reads archetype state directly.
- @query.go — `validateAndSortTerms` at line 1257 detects whether the user mentioned Disabled/Prefab in any kind. If not mentioned, attach two query-level flags (`skipDisabled`, `skipPrefab`). `matchesTable` at line 854 short-circuits on those flags via a single `t.HasComponent` test per flag per table — the O(1) per-table fast path.
- @cached_query.go — mirror the same flags through cache assembly; ensure cache invalidation already handles tables transitioning into/out of \"contains Disabled/Prefab\" (it should, via the existing archetype-transition invalidation).
- @scope.go — no changes; `scope` interface is already the read-side shape for `IsDisabled` / `IsPrefab`.
- @entity_lifecycle.go — most recent code pattern reference (Phase 16.1, v0.56.0). Header comments and function-doc style for the new `query_filters.go` should mirror this file's tone.
- @ROADMAP.md — Future Work section at lines 90-102 currently lists Phase 16.2 as `OnTableEmpty/OnTableFill`. This bundle supersedes that slot. The bundle becomes the new Phase 16.2; rename the table-events candidate to Phase 16.3 (or later) and shift subsequent candidates by one.
- @CHANGELOG.md — new v0.57.0 entry at top; follow the format used at line 3 (v0.56.0 entry).
- @README.md — feature list bump.
- C upstream reference: `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h:1919-1925` (constants), `src/bootstrap.c:972,974,1308` (registration + DontInherit), `src/storage/table.c:257-260` (per-table flag set), `src/query/validator.c:1238-1243` (opt-in flag set), `src/query/compiler/compiler_term.c:815-829` (mask clear), `src/query/engine/eval.c:88,183,496` (mask test in eval). Per-table approach is C's choice and the bundle adopts it.

## Deliverables

1. **`query_filters.go` (new file)** bundling both built-ins:
   - `World.Disabled() ID`, `World.Prefab() ID`.
   - `Disable(w *Writer, e ID)`, `Enable(w *Writer, e ID)`.
   - `IsDisabled(s scope, e ID) bool`, `IsPrefab(s scope, e ID) bool`.
   - `MarkPrefab(w *Writer, e ID)` (see open decision 1; final name TBD).
   - All idempotent; `Enable` on enabled entity and `Disable` on disabled entity are no-ops.
2. **`world.go` allocation**:
   - New allocations for `disabledID` and `prefabID` inserted before `wildcardID`. Slot ordering: Disabled=36, Prefab=37, Wildcard=38, Any=39, user=40+.
   - Update doc comment block (lines 133-172).
3. **`query.go` + `cached_query.go` filter wiring**:
   - In `validateAndSortTerms`: detect whether any term in any kind references `Disabled` or `Prefab` (compare by raw index). Stash two booleans on `Query` (`skipDisabled`, `skipPrefab`).
   - In `matchesTable`: if `skipDisabled && t.HasComponent(disabledID)` short-circuit `false`; same for prefab. This is two pointer-cheap checks per table — O(1).
   - Mirror in `cached_query.go`'s assembly path; cached tables that contain Disabled/Prefab are filtered out at cache-build time (cheaper than per-iter).
4. **Tests in `query_filters_test.go` (≥10 cases)**:
   - Disable excludes entity from ordinary query.
   - Disable + explicit `With(Disabled)` includes entity.
   - Enable after Disable re-includes.
   - Disable is idempotent.
   - Enable on already-enabled is no-op.
   - `IsDisabled` round-trip.
   - MarkPrefab excludes from ordinary query.
   - MarkPrefab + explicit `With(Prefab)` includes.
   - `IsPrefab` round-trip.
   - Both Disabled + Prefab on same entity: excluded unless query opts in to both.
   - `Maybe(Disabled)`: matches both kinds; field bound when present.
   - Cached query: after Disable/Enable, subsequent execution reflects state.
   - Prefab as IsA base: `e IsA prefab` succeeds; `IsPrefab(e)` is false (Prefab tag is DontInherit; confirmed via C `bootstrap.c:1308`).
   - Performance: a table full of disabled entities is skipped without per-entity iteration — assert via a counter on table-visits or per-entity-checks.
5. **Doc updates**:
   - `docs/EntitiesComponents.md` — Disable/Enable section with \"pause without delete\" use case.
   - `docs/PrefabsManual.md` — Prefab-tag section with the build-template / mark-as-prefab / use-as-IsA-base flow.
   - `docs/Queries.md` — callout: \"Queries exclude Disabled and Prefab entities by default; opt in by mentioning the tag in any term.\"
   - `docs/README.md` — flip lines 100 and 135 to shipped (v0.57.0) with anchor links.
   - `README.md` — feature list bump.
   - `CHANGELOG.md` — v0.57.0 entry at top, following the v0.56.0 shape.
   - `ROADMAP.md` — Shipped heading bump to \"through v0.57.0\"; insert the Phase 16.2 entry; renumber the displaced `OnTableEmpty/OnTableFill` candidate (currently \"Phase 16.2 candidate\" at line 95) to a later slot.
6. **Marshal**: skip-set additions for both new built-ins; `builtinEntityCount` 37 → 39; baseline test fixups (`isa_test.go`, `marshal_test.go`, `meta_test.go`).

## Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%
- No regression in existing query tests — the implicit-skip is the touchy piece; any test that relied on a prefab entity matching an ordinary query needs to opt in explicitly. Audit `prefab*_test.go`, `isa_test.go`, and `query_test.go`.

## Explicit non-goals

- No prefab hierarchies (`docs/README.md:136`).
- No prefab slots (`docs/README.md:137`).
- No observer disabling (`docs/README.md:159`).
- No system disabling (`docs/README.md:143`).
- No entity ID ranges (`docs/README.md:99`).
- No REST toggle endpoint (`docs/README.md:177`) — unblocked by Disabled landing but not implemented in this phase.

## Open decision points

1. **`MarkPrefab` vs `SetPrefab` vs `MakePrefab`**: recommend `MarkPrefab`. \"Set\" is reserved for value-bearing operations in this codebase (`SetSparse`, `SetSingleton`, `SetWriteOnce` all manage policies that gate later behavior; `Set[T]` writes values). \"Make\" implies allocation. \"Mark\" reads as \"tag this entity\" without overloading semantics. The naming asymmetry with `Disable`/`Enable` is intentional: disabling is a state toggle, marking-as-prefab is one-way (followed by `Remove` to undo, not `Unmark`). **Decide: `MarkPrefab` (recommended).**
2. **Implicit-skip implementation site**: synthesize implicit `Not(Disabled)`/`Not(Prefab)` terms at validate time **versus** stash booleans on the Query and apply a table-skip check at iterate time. Recommend the **table-skip flag approach**: cheaper (no extra term machinery, no impact on OR-group handling, original term list preserved for `Terms()` / `TermsFull()` introspection), and matches the C upstream approach (compile-time mask manipulation rather than synthetic terms). State the choice in the implementation comment.
3. **Prefab + Disabled on the same entity**: order of skip-checks is independent and commutative; both must be opted in if both tags are present. Document this in `docs/Queries.md` to forestall future debate.
4. **`Disable(disabled_entity_id)`**: idempotent (recommended) — matches the `applyXxxPolicy` patterns and `OrderedChildren` insertion-style idempotency. State explicitly in `Disable`'s doc comment.

## Process

- Feature, not bug.
- Verified all `@`-references and line numbers against the current tree at commit `fb44642`.
- Phase numbering: this bundle becomes Phase 16.2, displacing the ROADMAP's previously-listed `OnTableEmpty/OnTableFill` candidate (renumber to 16.3+).
