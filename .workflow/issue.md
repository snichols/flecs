## Goal

This is the **final trait phase that drains the trait-system roadmap vs upstream C flecs.** After this lands, the trait system is feature-complete vs upstream: `ComponentTraits.md` line 1006 — the single remaining `⏳ planned` row — will flip to `✅ shipped`, and the matching feature-gap entries in `docs/README.md` (lines 84 and 167) and `ROADMAP.md` will be resolved.

**However, a fundamental research finding changes the shape of this work and is the issue's primary open decision.** See the **Open Decision Points** section below before starting design.

### What Union semantically does

`Union` marks a relationship `R` such that:

- At most one pair `(R, T)` is active per entity at a time (semantically similar to `Exclusive`).
- The active target `T` is stored compactly without fragmenting archetype tables — changing the target value does NOT cause an archetype transition.
- Iterating "all entities with `(R, *)`" yields the entity and its current target `T`.

### CRITICAL UPSTREAM FINDING — Union no longer exists as a standalone trait

Direct research of `/work/agents/claude/projects/SanderMertens/flecs`:

1. **`EcsUnion` does not exist** in the current upstream C flecs source. `grep -rn 'EcsUnion'` against `src/`, `include/flecs.h`, and `distr/flecs.h` returns zero hits. The only `EcsExclusive`/`EcsCanToggle` constants are present (`distr/flecs.h:6846`, `distr/flecs.h:6878`).
2. **The C++ test file `test/cpp/src/Union.cpp` exists but constructs Union as a composition**, not as a standalone trait. Lines 1–110 of that file:
   ```cpp
   auto Movement = world.entity().add(flecs::DontFragment).add(flecs::Exclusive);
   auto e = world.entity().add(Movement, Standing);
   auto table = e.table();
   e.add(Movement, Walking);
   test_assert(e.table() == table);          // no archetype transition
   test_assert(e.has(Movement, Walking));
   test_assert(!e.has(Movement, Standing));  // prior target was replaced
   ```
3. **The upstream migration guide makes the replacement explicit** (`docs/MigrationGuide.md:492-502`):
   > ## Union relationships
   > Union relationships have been replaced with `DontFragment, Exclusive` relationships.
   > ```cpp
   > // Old
   > world.component<Position>().add(flecs::Union);
   > ```
   > ```cpp
   > // New
   > world.component<Position>().add(flecs::DontFragment).add(flecs::Exclusive);
   > ```
4. **Bootstrap evidence (`src/bootstrap.c:337-338`)**: upstream auto-marks `SlotOf` with `EcsDontFragment + EcsExclusive` — exactly the Union recipe — and the `(DontFragment, With, Sparse)` chain (`bootstrap.c:1302`) bootstraps Sparse storage for DontFragment but says nothing about Exclusive being implied by DontFragment.

The brief's deliverables (standalone `SetUnion`, `IsUnion`, `unionPolicies`, `unionStore[ID]map[ID]ID`, built-in `Union` entity at index 36) describe a Union trait that no longer exists upstream. The **research-driven recommendation is to evaluate whether to follow upstream and ship Union as a thin alias/sugar over `SetDontFragment + SetExclusive`** rather than introduce a separate trait that immediately diverges from upstream's just-shipped consolidation.

This is the issue's primary open decision and is captured in detail below.

### Distinction from Exclusive (for reference)

`Exclusive` enforces at-most-one but stores the active pair like any normal pair — each unique target creates a separate archetype table. Union stores the active target compactly with no table proliferation, at the cost of slightly more complex iteration.

## Constraints

### Pattern references in the local codebase

- `@/work/agents/claude/projects/flecs/exclusive.go` — at-most-one-pair semantic. `applyExclusivePolicy` is the simplest possible flag-map pattern; if Union ships as a thin alias, calls to `SetUnion` should delegate to `applyExclusivePolicy` (and the DontFragment equivalent).
- `@/work/agents/claude/projects/flecs/dont_fragment.go` — just-shipped Phase 15.21 (v0.53.0, commit `5da6fe9`); closest architectural precedent for "trait controls storage routing without changing archetype membership." **Note (gap to resolve):** today `SetDontFragment` panics if its argument is not a registered data-bearing component (lines 54-57). The Union-style composition requires marking a *relationship* (not a component) as DontFragment, with data stored on the *pair* `(R, T)` rather than as a component value. Either DontFragment must be extended to accept relationship IDs, or the Union implementation has to bypass that guard.
- `@/work/agents/claude/projects/flecs/sparse.go` — the storage primitive shared with DontFragment. Inspect `sparseSet`, `sparseSetInsert`, `sparseSetRemove`, `sparseHeld` for cleanup hook precedent.
- `@/work/agents/claude/projects/flecs/with.go` — the trait-implies-trait infrastructure. If Union ships as upstream-style composition, it could be expressed as `SetWith(unionID, exclusiveID) + SetWith(unionID, dontFragmentID)` declaratively. **Investigate.**
- `@/work/agents/claude/projects/flecs/oneof.go` — pair-target constraint; should compose with Union (target must be ChildOf the OneOf parent).
- `@/work/agents/claude/projects/flecs/reflexive.go` — example of a relationship trait with policy-map storage and a query-time matcher.

### Upstream C flecs citations

- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` — `EcsUnion` absent. `EcsExclusive` defined elsewhere in distr/flecs.h:6846.
- `/work/agents/claude/projects/SanderMertens/flecs/src/bootstrap.c:337-338` — `flecs_register_slot_of` auto-adds `EcsDontFragment + EcsExclusive` to `SlotOf` registrations (the canonical Union recipe used internally by upstream).
- `/work/agents/claude/projects/SanderMertens/flecs/src/bootstrap.c:1302` — `ecs_add_pair(world, EcsDontFragment, EcsWith, EcsSparse)` — upstream bootstrap chains DontFragment → Sparse via the With infrastructure. DontFragment + Exclusive is NOT chained via With; both flags are independent.
- `/work/agents/claude/projects/SanderMertens/flecs/src/bootstrap.c:131-139` — flag-bit dispatch on `EcsIdSparse`, `EcsIdDontFragment`, `EcsIdExclusive`. No `EcsIdUnion`.
- `/work/agents/claude/projects/SanderMertens/flecs/docs/MigrationGuide.md:492-502` — official replacement guide: `flecs::Union` → `flecs::DontFragment + flecs::Exclusive`.
- `/work/agents/claude/projects/SanderMertens/flecs/test/cpp/src/Union.cpp:1-110` — acceptance behavior (no archetype transition; replacement target; Wildcard match). Use as the behavioral spec.

### Local docs and conventions

- `@/work/agents/claude/projects/flecs/docs/ComponentTraits.md` — line 906 (`### Union`) flips from "Not yet ported" to shipped section; line 1006 (the last `⏳ planned` row in the trait roadmap table) flips to `✅ shipped (v0.54.0)`. After this phase, every row in that table should read `✅ shipped`.
- `@/work/agents/claude/projects/flecs/docs/README.md` — line 84 (`### Phase 14.0 feature-gap list`) and line 167 (`Additional gaps discovered in Phase 14.8`) — both Union entries flip to shipped.
- `@/work/agents/claude/projects/flecs/docs/Relationships.md` — no current Union mention; add a section on Union vs Exclusive trade-offs.
- `@/work/agents/claude/projects/flecs/README.md` — feature list; the v0.54.0 release note can claim "trait system feature-complete vs upstream C flecs."
- `@/work/agents/claude/projects/flecs/CHANGELOG.md` — v0.54.0 entry at top. Call out the milestone.
- `@/work/agents/claude/projects/flecs/ROADMAP.md` — line 3 (`Shipped (through v0.53.0)` → bump to v0.54.0); add the Phase 15.22 entry after line 57; **the trait phase line is now complete** — note this in the entry text.
- `@/work/agents/claude/projects/flecs/CONTRIBUTING.md` — coverage requirement: stay ≥ 90% on the root `flecs` package (currently ~97%).

### Current built-in ordering (verified)

`@/work/agents/claude/projects/flecs/meta_test.go:11-19` and `@/work/agents/claude/projects/flecs/world.go:72-169`:
- DontFragment = 35 (just shipped in v0.53.0)
- Wildcard = 36
- Any = 37
- `builtinEntityCount = 37`
- User entities start at index 38.

A new built-in `Union` slotted **before** the query-term sentinels would shift Wildcard → 37, Any → 38, user entities → 39, and `builtinEntityCount` → 38. If Union ships as pure alias-with-no-built-in-entity (recommended under Option B below), there is no shift and `builtinEntityCount` stays at 37.

## Open Decision Points (must resolve before implementation)

### 1. Standalone trait vs composition alias — fundamental shape of the phase

The brief assumes a standalone `Union` trait with its own built-in entity, policy map, side storage, write/read paths, and marshalling. Upstream has eliminated this and replaced it with `DontFragment + Exclusive`. Two viable shapes:

**Option A — Standalone trait (matches the brief, diverges from upstream):**
- New built-in entity at index 36 (shifts Wildcard/Any, `builtinEntityCount` → 38).
- New `unionPolicies map[ID]bool` and `unionStore map[ID]map[ID]ID` (or sparse-set reuse).
- Independent write/read paths, marshal section, query mode.
- Conflict rules: panic on `SetUnion` of an already-Exclusive relationship and vice versa (Union is the superset).

Pros: matches the brief verbatim; one-call API for users. Cons: diverges from upstream's just-shipped consolidation; doubles the maintenance surface alongside DontFragment + Exclusive; the underlying flag-bit machinery would have to dispatch to a Union-specific path that semantically duplicates what DontFragment + Exclusive already do.

**Option B — Composition alias (matches upstream, recommended):**
- No new built-in entity. `w.Union()` either does not exist or returns an alias resolving at call time.
- `SetUnion(w, relID)` is defined as `func SetUnion(w *World, relID ID) { SetDontFragment(w, relID); SetExclusive(w, relID) }`.
- `IsUnion(scope, relID)` is `IsDontFragment(scope, relID) && IsExclusive(scope, relID)` — purely derived.
- All Union semantics fall out of the DontFragment + Exclusive composition. Acceptance tests can be the upstream Union.cpp test cases ported to Go.

Pros: matches upstream's chosen direction; tiny implementation surface; immediately benefits from future DontFragment / Exclusive improvements. Cons: requires `SetDontFragment` to accept *relationship* IDs (not just data-bearing components — see the dont_fragment.go gap noted above) AND requires the relationship-pair write path to consult DontFragment to suppress archetype transitions for pair adds (which it does not do today). This is the real implementation work hidden inside Option B.

**Hybrid — alias surface + internal generalization:**
- Public API is Option B (`SetUnion = SetDontFragment + SetExclusive`).
- Phase 15.22's actual work is generalizing `DontFragment` to:
  - Accept relationship IDs in addition to component IDs.
  - On pair add `(R, T)` where `R` is DontFragment + Exclusive, store the active target in a per-relationship side store (sparse-set keyed by entity, value = target ID) and skip archetype transition.
- This generalizes the trait correctly per upstream and leaves no Union-specific code path. The "Union" name is documentation, not implementation.

**Recommendation: Hybrid.** It honors the milestone framing ("final trait phase, trait system feature-complete vs upstream") while doing the smallest amount of work that actually closes the gap. Under Hybrid, the Option A storage and built-in proposal in the brief is replaced by *making DontFragment work correctly on relationships*.

This decision must be resolved before implementation begins. If the decision lands on Option A (deliberate divergence), the rest of the brief applies as written. If Hybrid or B, most of the brief is restructured.

### 2. Storage representation (only if Option A or Hybrid is chosen)

- **Per-relationship `map[ID]ID`** (entity → current target) on `World`: smallest possible footprint; O(1) lookup. Recommend this for simplicity unless upstream's storage is worth porting (it isn't, since upstream uses generic sparse storage).
- **Sparse-set reuse with value = target ID**: more uniform with Sparse, but ID boxing overhead.

Recommend per-relationship `map[ID]ID`, mirroring the simplicity of `exclusivePolicies` / `oneOfPolicies` rather than the full sparse-set machinery.

### 3. PairIsTag interaction

Union (by composition or standalone) implies pairs carry no data — the only state is the active target. Should `SetUnion(R)` after `SetPairIsTag(R)` (or vice versa) panic? Recommend **silent accept**: both flags coexist without harm because the underlying constraint is the same. State the chosen behavior in CHANGELOG.

### 4. SetID on a Union pair

`SetID(e, Pair(R, T), &v)` on a Union relationship has no value to write. Mirroring PairIsTag, the recommendation is **panic with a clear message**. Document.

### 5. Iteration order stability

`EachUnion[…]` (parallel to `EachSparse`) and Union-driven query iteration: recommend **stable order**. Either store entries in a slice alongside the map (insertion order) or document explicitly that order is undefined. The DontFragment/Sparse precedent is dense-slice insertion order.

### 6. Wildcard read semantics

`HasID(e, Pair(R, Wildcard))` where R is Union: true iff entity has any active target. `RemoveID(e, Pair(R, Wildcard))`: clear regardless of current target. These match the upstream Union.cpp test cases (lines 75, 90-91).

## Deliverables (assuming Option A — brief verbatim; revise after decision-point resolution)

> The list below transcribes the brief's deliverables for traceability. If Hybrid/Option B is chosen, the actual implementation work is **generalizing DontFragment to work on relationship pairs** and most of these become aliases or no-ops.

1. **New file `union.go`** with:
   - `World.Union() ID` — built-in entity at index 36 (shifts Wildcard → 37, Any → 38, user → 39; `builtinEntityCount` → 38).
   - `SetUnion(w *World, relID ID)` — marks the relationship.
   - `IsUnion(s scope, relID ID) bool` — accepts `scope` (Phase 15.8 convention).
   - `World.unionPolicies map[ID]bool` flag map.

2. **Storage**: per-relationship `map[ID]ID` (entity → current target), keyed on relationship ID's raw index. Justification: smallest footprint, mirrors `exclusivePolicies` and `oneOfPolicies`.

3. **Write path enforcement**:
   - `AddID(e, Pair(R, T))` where `IsUnion(R)`:
     - If entity has prior `(R, Tprev)`: silently replace (no panic; at-most-one semantic). Fire `OnRemove` for old target, `OnAdd` for new target.
     - Update the union store: `unionStore[R][e] = T`.
     - Do NOT add `(R, T)` to the archetype type — no table transition. (This is the defining Union property; verify by table-pointer equality in tests, per upstream `Union.cpp:17`.)
   - `SetID(e, Pair(R, T), &v)` where `IsUnion(R)`: **panic** with clear message — Union pairs don't carry data.
   - `RemoveID(e, Pair(R, T))` where `IsUnion(R)`: if current target == T, clear; else no-op (matches Exclusive semantics).
   - `RemoveID(e, Pair(R, Wildcard))` where `IsUnion(R)`: clear regardless of current target.

4. **Read path**:
   - `HasID(e, Pair(R, T))` where `IsUnion(R)`: true iff `unionStore[R][e] == T`.
   - `HasID(e, Pair(R, Wildcard))`: true iff entity has any entry in `unionStore[R]`.
   - `EachUnion[…](r, rel, func(e ID, target ID))` convenience iterator (parallel to `EachSparse`). Stable iteration order.

5. **Query integration**:
   - Term `Pair(R, *)` or `Pair(R, T)` where R is Union: iterate `unionStore[R]` as the driver.
   - Compose with archetype-stored terms via the existing three-mode iterator (extend the mode-selection criterion in `query.go` and `cached_query.go` to recognize Union in addition to DontFragment).
   - Wildcard target: yield every entity with its current target. Specific target: filter by `target == T`.

6. **Trait composability**:
   - `SetUnion` on an already-Exclusive relationship: **panic** (redundant; Union subsumes the semantic).
   - `SetExclusive` on an already-Union relationship: **panic**.
   - Union + OneOf: compatible. Targets must be ChildOf the OneOf parent. Test required.
   - Union + Transitive / Reflexive / Acyclic: should compose. Tests required.
   - Union + PairIsTag: silently accept (recommended).

7. **Marshal**:
   - Add `union_state` field to marshal output: `{relID: {entityID: targetID}}` per Union relationship.
   - Round-trip test (entity delete after unmarshal should still clean up the union store).
   - New built-in skip-set entry for the Union built-in entity.
   - `builtinEntityCount` bump 37 → 38 in `meta_test.go` (only if Option A; not under Hybrid).

8. **Cleanup hooks**:
   - In `deleteOne`: remove the deleted entity from every union store it appears in. Iterate `unionPolicies` to find all Union relationships; for each, `delete(unionStore[rel], e)`.
   - Deleting the relationship entity itself: drop the entire union store for that relationship.

9. **Tests in `union_test.go`** (≥ 12 cases, coverage ≥ 95.0%):
   1. `SetUnion(R)` + add `(R, T1)` + add `(R, T2)`: only `(R, T2)` is held; entity's table pointer is unchanged between adds (port `Union.cpp:14-17`).
   2. `HasID(e, Pair(R, T1))` → false after T2 replaces.
   3. `HasID(e, Pair(R, Wildcard))` → true while any target is held.
   4. `RemoveID(e, Pair(R, T))` (matching current) clears; `RemoveID(e, Pair(R, T_other))` is no-op.
   5. `RemoveID(e, Pair(R, Wildcard))` clears regardless.
   6. `SetID(e, Pair(R, T), &val)` panics with clear message.
   7. Query `(R, *)` yields entity-target tuples; iteration count matches entity count.
   8. Query `(R, T)` yields only entities currently holding that target.
   9. Pure-union query: 100 entities each with a different target; verify O(n) iteration and no archetype proliferation.
   10. Mixed query: Union term + archetype term; intersection works.
   11. Composition: `SetUnion(R) + SetOneOf(R, parent)` — Union targets must be ChildOf the OneOf parent.
   12. Conflict: `SetUnion(R)` after `SetExclusive(R)` panics with clear message (and vice versa).
   13. Marshal round-trip with Union state.
   14. Entity delete clears Union store entries.
   15. Relationship-entity delete drops the entire store.
   16. (Hybrid only) Port `test/cpp/src/Union.cpp:Union_add_case`, `Union_get_case`, `Union_add_remove_switch_w_type`, `Union_switch_enum_type` to Go as behavioral parity tests.

10. **Doc updates** (per CONTRIBUTING.md "update docs accordingly" rule):
    - `docs/ComponentTraits.md` line 906 (`### Union`): flip from "Not yet ported" to "shipped (v0.54.0)" with a Go usage example showing the at-most-one-target-without-fragmentation pattern. **If Hybrid/Option B is chosen, also document explicitly that `SetUnion` is sugar for `SetDontFragment + SetExclusive` and link to the upstream MigrationGuide rationale.**
    - `docs/ComponentTraits.md` line 1006: flip the `⏳ planned` row to `✅ shipped (v0.54.0)` with API triad. Confirm no other `⏳`/`planned` rows remain in the file (grep verifies this is the last one).
    - `docs/Relationships.md`: new section on Union vs Exclusive trade-offs.
    - `docs/README.md` line 84: flip the Union feature-gap entry to shipped.
    - `docs/README.md` line 167: flip the second Union entry (Phase 14.8 list) to shipped.
    - `README.md`: feature list entry; note that **the trait system is feature-complete vs upstream C flecs as of v0.54.0**.
    - `CHANGELOG.md` v0.54.0 entry at top — call out the milestone and the upstream-divergence-or-conformance decision (Option A vs Hybrid).
    - `ROADMAP.md`: shipped row at line 57+ (after the v0.53.0 DontFragment entry); heading bump line 3 to "through v0.54.0". The "Trait system" phase line is now complete.

## Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage on the root `flecs` package ≥ 95.0% (CONTRIBUTING.md requires ≥ 90%; user brief tightens to 95%).

## Explicit non-goals

- No further trait additions in this phase beyond Union.
- No retroactive migration of existing Exclusive relationships to Union — opt-in only.
- No changes to `SetDontFragment`'s public API contract for data-bearing components. **(If Hybrid is chosen, the DontFragment changes are internal — same surface, extended to accept relationship IDs.)**

## Process

- Feature, not bug.
- Resolve the standalone-vs-composition decision (#1 under Open Decision Points) before implementation begins.
- Verify all `@`-references and line numbers before opening a PR.
