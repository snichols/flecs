## Goal

Port the `Reflexive` relationship trait from C flecs, continuing to drain the trait-system roadmap in `docs/ComponentTraits.md`. **Reflexive** asserts that a relationship is reflexive on its source: for any entity `a`, the pair `(R, a)` is implicitly considered to apply to `a` itself — without storing an actual self-pair. This trait composes with **Transitive** (Phase 15.5) so that traversal queries naturally include the starting node.

**Target version: `v0.39.0`.** Phase precedents: 15.0–15.6.

### Key C semantics (researched, ground every claim against these)

1. `EcsReflexive` is a tag entity (no `EcsIdReflexive` flag bit exists — the user's draft was wrong about that). It's declared in `include/flecs.h:1769`. The doc above the declaration is the canonical semantic statement:

   > `R(X, X) == true`

2. **`ecs_has_id` does NOT consult Reflexive in C.** See `src/entity.c:2540` — the function walks the entity's table records and IsA chain, but never checks for the Reflexive trait. So in C, `ecs_has_id(world, a, ecs_pair(R, a))` returns false unless a self-pair is actually stored. Reflexive is purely a **query-time matching** property.

3. **The Go flecs docs (`docs/Relationships.md`, `docs/ComponentTraits.md`) already promise that `Has(e, R, e) == true` for reflexive relationships.** This is a deliberate Go-flecs choice that goes *beyond* C — and it is what users will expect from the docs. **Decision required during iterate:** match the docs (extend `HasID` to consult Reflexive), or match C (Reflexive is query-only and `HasID` is unchanged). The user's draft of this phase leans toward extending `HasID`, which is consistent with the docs but a deliberate divergence from C semantics. Document the choice in CHANGELOG.

4. **Query-engine handling in C** lives in `src/query/engine/eval_trav.c`:
   - `flecs_query_trav_fixed_src_reflexive` (line 9): when matching a fixed source, if the target entity is in the source's table, it's a match — even without a stored pair.
   - `flecs_query_trav_unknown_src_reflexive` (line 54): when the source variable is unbound, bind it to the target entity (the self-match yields one row).
   - These run inside `EcsQueryTrav` ops (`src/query/types.h:56`) — the same op family that handles transitive walks. Reflexive composes with Transitive: a Reflexive+Transitive R yields the starting entity *and* its ancestors in a `With(MakePair(R, target))` query.

5. **The validator sets the term flag**: `src/query/validator.c:884-886` reads `ecs_table_has_id(world, first_table, EcsReflexive)` on the relationship entity and ORs `EcsTermReflexive` into `term->flags_`. So Reflexive is *stored as a tag on the relationship entity* (the standard trait pattern, identical to Transitive in 15.5), not as a per-id cache flag.

6. **Bootstrap.c registers IsA as both Transitive AND Reflexive** (`src/bootstrap.c:1320-1321`):
   ```c
   ecs_add_id(world, EcsIsA, EcsTransitive);
   ecs_add_id(world, EcsIsA, EcsReflexive);
   ```
   And `EcsReflexive` itself is bootstrapped as a trait at line 1004.

### Likely shape (refine after re-reading C during iterate)

1. **New built-in entity ID** `World.Reflexive() ID`. **Index allocation: not 23 as the draft suggested.** Phase 15.6 already placed `Wildcard` at 21 and `Any` at 22, so user entities currently start at 23. Insert Reflexive at index 21 (between Transitive and the sentinels), bumping Wildcard to 22, Any to 23, user entities to 24 — OR append Reflexive at index 23 and bump sentinels to 24/25, users to 26. **The first option is cleaner because it keeps trait entities contiguous before the sentinels.** Iterate agent decides; document the choice in CHANGELOG.

2. **Per-relationship flag storage.** Same pattern as 15.4–15.6: `World.reflexivePolicies map[ID]bool`. Mirror `transitivePolicies` from `transitive.go`.

3. **Public API** (mirror `transitive.go`):
   - `flecs.SetReflexive(w, relID)` — mark a relationship as reflexive.
   - `flecs.IsReflexive(w, relID) bool` — inspection.
   - `w.Reflexive() ID` accessor.
   - `addIDImmediate` bare-tag handler in `id_ops.go` so `fw.AddID(relID, w.Reflexive())` also populates the policy map (matches the Transitive precedent).

4. **`HasID` extension in `scope.go`.** When asked `HasID(a, MakePair(R, a))`:
   - If `R` is Reflexive AND first == second (the self-pair case), return true.
   - Otherwise fall through to existing logic.
   - **Cheap gate:** check `first == second` first; only consult the policy map when the source equals the target. This avoids a map lookup on every `HasID` call.
   - **Note: this diverges from C** (see point 2 above). It matches the existing flecs Go documentation promise.

5. **Query matcher extension** in `query.go` / `cached_query.go`. When matching `With(MakePair(R, target))` where R is Reflexive:
   - The target entity itself becomes a match row, in addition to entities holding a stored `(R, target)` pair.
   - For the uncached path: term-match check at iteration time.
   - For the cached path: this is the tricky piece. Cached queries discover matching tables at cache-build time; if the target is parameterized at query time, the cached path may not pick up the reflexive self-match cleanly. **Document any limitation explicitly** rather than silently failing.
   - **Reflexive + Transitive composition** (Phase 15.5): a Reflexive+Transitive R lets `With(MakePair(R, target))` match both `target` itself and all entities chained to `target` via R. This is the IsA case post-bootstrap.

6. **Built-in: IsA gains Reflexive in bootstrap.** Match C `src/bootstrap.c:1321`. After this change, `HasID(a, MakePair(IsA, a))` newly returns true. Verify no existing IsA test asserts this returns false; the Phase 15.5 Transitive change to IsA is the immediate precedent.

7. **Tests** in `reflexive_test.go` (NEW):
   - Default behavior unchanged: a non-Reflexive relationship — `HasID(a, MakePair(R, a))` returns false unless a pair is stored.
   - Mark + HasID self-pair: returns true without a stored pair.
   - Query self-match: `With(MakePair(R, target))` includes target itself as a row when R is Reflexive.
   - Reflexive + Transitive (15.5) composition: well-defined semantics. The chain yields ancestors AND the starting entity.
   - IsA is reflexive after bootstrap: regression covering the new built-in.
   - `IsReflexive` round-trip.
   - Reflexive on a non-relationship entity (component or tag without target): no-op or panic. **Check C behavior** — `ecs_add_id(world, MyTag, EcsReflexive)` doesn't error in C; the tag just has no effect during query evaluation since the entity is never used as a pair-first. Match that lenient behavior.
   - Race coverage equivalent to `exclusive_access_norace_test.go` if Reflexive introduces any shared mutable state outside `World`'s normal locking.

8. **Docs updates per CONTRIBUTING.md**:
   - `docs/Relationships.md:624` — promote the existing \"Reflexive is a separate unported trait\" callout into a shipped section with example.
   - `docs/ComponentTraits.md:408-412` (the existing Reflexive subsection that currently says \"Workaround: None in the query engine\") → replace with shipped content.
   - `docs/ComponentTraits.md:638` — table row Reflexive → ✅ shipped.
   - `docs/README.md:162` — remove from gap list.
   - `CHANGELOG.md` — new `v0.39.0` entry; **explicitly note the deliberate divergence from C** if `HasID` is extended (point 2 above).
   - `ROADMAP.md` — Phase 15.7 marked shipped; built-in entity count updated; user-entity start index updated.

### Non-goals

- NO change to non-Reflexive relationships.
- NO automatic \"Reflexive implies X\" propagation in either direction.
- NOT porting `EcsTermReflexive` as a public flag — it's an internal C compilation detail.
- NOT changing existing IsA test expectations except the one new positive `HasID(a, MakePair(IsA, a))` case.

### Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run ./...` clean.
- `go test ./... -race -count=3` clean.
- Coverage on main package ≥ 95%.
- Existing tests pass without modification (Reflexive-on-IsA is a one-way add — `Has(a, MakePair(IsA, a))` newly returns true, but no existing test relies on this returning false).
- New `reflexive_test.go` covers the 7 cases listed above.
- Docs updates land in the same PR.

### Style notes

- Match the trait-pattern precedent (Transitive in 15.5 is the closest analogue).
- `HasID` self-pair check must be cheap: gate on `first == second` *before* the map lookup so non-self queries pay zero cost.
- Cached-query interaction with Reflexive is the trickiest piece — document any limitations rather than silently failing.
- Reflexive composes with Transitive; ensure the composed case has explicit test coverage.

## Constraints

- @cleanup.go — Phase 15.0 trait-pattern precedent (policy-map storage).
- @instantiate_policies.go — Phase 15.1 precedent for inheritance-policy-style traits.
- @exclusive.go — Phase 15.2 precedent (bare-tag-add policy plumbing).
- @cantoggle.go — Phase 15.3 precedent (component-trait flag).
- @symmetric.go — Phase 15.4 precedent (relationship-trait flag, bidirectional pair semantics).
- @transitive.go — Phase 15.5 precedent (closest analogue: relationship trait + traversal semantics, `SetTransitive` / `IsTransitive` / `Transitive()` API shape, `transitivePolicies` map, `applyTransitivePolicy`).
- @world.go — built-in entity registration; IsA is at index 2 and currently has Transitive (15.5) added in bootstrap; Reflexive should slot into the trait region (likely index 21) before the Wildcard/Any sentinels.
- @id_ops.go — `addIDImmediate` bare-tag handler must recognize the Reflexive tag so `AddID(relID, w.Reflexive())` populates the policy map (matches Transitive precedent).
- @scope.go — `HasID` extension for the Reflexive self-pair case; gate cheaply on `first == second`.
- @query.go — uncached matcher extension for Reflexive self-match.
- @cached_query.go — cached-path interaction; document any limitations explicitly.
- @docs/Relationships.md — section currently flags Reflexive as unported (line 624); rewrite to shipped with example.
- @docs/ComponentTraits.md — subsection at line 408-412 (\"Workaround: None\") and trait table row at line 638 both need updating to ✅ shipped.
- @docs/README.md — gap list at line 162 needs Reflexive removed.
- @CHANGELOG.md — add `v0.39.0` entry; explicitly call out the divergence from C if `HasID` is extended to honor Reflexive.
- @ROADMAP.md — mark Phase 15.7 shipped; update built-in entity count and user-entity start index.
- C reference: `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h:1769` — `EcsReflexive` declaration and `R(X, X) == true` doc.
- C reference: `/work/agents/claude/projects/SanderMertens/flecs/include/flecs/private/api_flags.h:191` — `EcsTermReflexive` term flag (internal compiler detail; NOT for porting).
- C reference: `/work/agents/claude/projects/SanderMertens/flecs/src/bootstrap.c:1004,1320-1321` — Reflexive trait bootstrap and IsA registered as both Transitive and Reflexive.
- C reference: `/work/agents/claude/projects/SanderMertens/flecs/src/query/engine/eval_trav.c:9-77` — `flecs_query_trav_fixed_src_reflexive` and `flecs_query_trav_unknown_src_reflexive`, the matcher routines.
- C reference: `/work/agents/claude/projects/SanderMertens/flecs/src/query/validator.c:884-886` — how validator promotes the relationship trait into the per-term `EcsTermReflexive` flag.
- C reference: `/work/agents/claude/projects/SanderMertens/flecs/src/entity.c:2540` — `ecs_has_id` does NOT consult Reflexive; documenting the divergence if Go flecs chooses to honor Reflexive in `HasID`.
