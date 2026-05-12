## Goal

Land the **Acyclic** relationship trait — a cycle-prevention property for relationships. This is the next phase in the trait-system roadmap (15.0–15.8 shipped; v0.40.0 was the ergonomics cleanup that closed the read-helper `scope` convention). Target version: **v0.41.0**.

### Why this exists

Continuing the trait-system roadmap. Acyclic prevents cycles in a relationship: adding `(a, R, b)` is rejected if `b` transitively already has `(R, a)` via the same relationship.

**Critical correctness gap**: ChildOf currently has no cycle prevention in our port. A user can `fw.AddID(parent, MakePair(w.ChildOf(), child))` while `child` already has `(child, ChildOf, parent)`, creating an infinite cycle that breaks `EachChild` recursion. Acyclic on ChildOf prevents this.

Cited in `docs/Relationships.md` and `docs/ComponentTraits.md` (trait-system roadmap shows ⏳ planned for Acyclic). Composes with Transitive (15.5, v0.37.0) — Transitive's walker already has cycle detection at query time; Acyclic prevents cycles from being stored in the first place.

### C research findings (ground truth)

Read `flecs.h` 1829-1830 (`EcsAcyclic` extern), `bootstrap.c` 1011/1062/1296/1317, `entity.c` 76/3002, `storage/table.c` 2893, `query/validator.c` 610, `storage/component_index.c` 1188, `world.c` 60. Key discoveries:

1. **No `EcsIdAcyclic` flag bit exists in C.** Acyclic is queried via `ecs_has_id(world, rel, EcsAcyclic)` — there is no cached flag on the component record. Our port should follow the same `map[ID]bool` pattern (consistent with Phases 15.0–15.7 — `acyclicPolicies map[ID]bool`).
2. **C bootstrap applies Acyclic to: `ChildOf`, `EcsTraversable` (transitively via `(EcsTraversable, EcsWith, EcsAcyclic)`), and `EcsWith`.** Notably, **C does NOT bootstrap Acyclic on `IsA`** (bootstrap.c lines around 1315-1328 only add IsA → Traversable/Transitive/Reflexive/Inherit). The original instruction's hypothesis that IsA gets Acyclic is **wrong** per C. Only ChildOf needs the bootstrap in our port; IsA already has reflexive + recursion-depth-guarded base lookup (`entity.c` 75-76 `\"cycle detected in IsA relationship\"`). Decision for this phase: **bootstrap Acyclic on ChildOf only**, matching C exactly.
3. **C does NOT enforce Acyclic at `ecs_add_id` time.** Cycle protection in C is implicit:
   - `flecs_get_base_component` (entity.c 75) guards with `ECS_MAX_RECURSION` and errors at lookup.
   - `component_index.c` 1188 and `non_fragmenting_childof.c` 214 detect ChildOf cycles during indexing/iteration.
   - `ecs_get_depth` (entity.c 3002) and `ecs_table_get_depth` (table.c 2893) require the relationship to be Acyclic as a precondition for the operation.
   - Query validator (validator.c 610) requires Acyclic for certain traversal terms.
   
   **The Go port's proposed write-time enforcement is a deliberate divergence from C.** This goes further than C — it rejects cycles at AddID rather than letting them blow up later at query/traversal. Justification: our `EachChild` API in `childof.go` recurses unconditionally and would stack-overflow on a cycle; C's `ECS_MAX_RECURSION` guard is replaced by upfront prevention. Document as a deliberate divergence in CHANGELOG, mirroring how Phase 15.7 documented `HasID` self-pair divergence.

## Likely shape (refine during implementation)

1. **New built-in entity ID** `World.Acyclic() ID`. Per current allocation in `world.go` 254-271 (Reflexive=21, Wildcard=22, Any=23), Acyclic goes at **index 22** between Reflexive and Wildcard; Wildcard moves to 23, Any to 24, user entities start at 25. This matches the Phase 15.7 precedent (Reflexive insertion shifted Wildcard/Any). Self-applied tag form: `fw.AddID(relID, w.Acyclic())`.

2. **Per-relationship flag storage**: `World.acyclicPolicies map[ID]bool` keyed by `ID(relID.Index())` — same pattern as `reflexivePolicies`, `transitivePolicies`, etc.

3. **Public API** in new `acyclic.go`:
   - `flecs.SetAcyclic(w, relID)` — mark a relationship as acyclic.
   - `flecs.IsAcyclic(w, relID) bool` — inspection (must accept `scope` per Phase 15.8 if it grows into a read helper).
   - Internal `applyAcyclicPolicy(w, relID)` invoked by `SetAcyclic` and by the `addIDImmediate` self-pair branch (when user does `fw.AddID(relID, w.Acyclic())`).

4. **`addIDImmediate` Acyclic hook** (`id_ops.go`). When adding `(R, target)` to entity `e`:
   - If `R` is Acyclic and `e != target`:
     - Walk from `target` via `(R, *)` chain. If `e` is reachable, panic with a message identifying entity, relationship, target.
     - **Reuse `walkUp` from `traversal.go`** (signature already takes `(*World, ID, ID, func(ID) bool)` and has cycle-safe semantics with `maxWalkDepth` guard — verified at traversal.go 26).
   - If `e == target` (self-pair `(a, R, a)`): match C semantics. C does not specifically reject `(a, R, a)` on Acyclic-only relationships; reflexivity is what determines whether self-pairs are meaningful. **Decision**: allow `(a, R, a)` on any relationship (Acyclic doesn't reject self-pairs). Reflexivity covers the \"this is trivially true\" case from Phase 15.7. Document this in the panic-message logic.

5. **Performance**: cycle check on AddID is O(chain length). For deep ChildOf trees this is per-add cost. Acceptable; document in CHANGELOG.

6. **Built-in bootstrap**: `applyAcyclicPolicy(w, w.childOfID)` in `world.go` next to the existing `applyReflexivePolicy(w, w.isAID)` at line 288. **Do NOT bootstrap Acyclic on IsA** — C does not, and IsA's recursion guard in our codebase is documented separately. This is the headline correctness fix for ChildOf.

7. **Tests** in new `acyclic_test.go`:
   1. Default behavior unchanged — non-Acyclic relationship allows what would otherwise be cycles.
   2. Direct cycle prevented: `(a, R, b)` then `(b, R, a)` — second add panics.
   3. Transitive cycle prevented: chain `a → b → c`, then `(c, R, a)` — rejected.
   4. Self-pair `(a, R, a)` — allowed (Acyclic doesn't reject self).
   5. ChildOf bootstrap regression: existing cascade tests still pass; cycle attempts now rejected.
   6. `IsAcyclic` round-trip.
   7. Acyclic + Transitive composition (compose at different layers — Acyclic at write, Transitive at query).
   8. Acyclic + Symmetric edge case: Symmetric tries to add the mirror; if the mirror would create a cycle, both adds must fail atomically (or with clear semantics). Verify C behavior is to fail the original add before the mirror is attempted.
   9. Bare-tag self-pair form: `fw.AddID(relID, w.Acyclic())` sets the policy (mirror Phase 15.7's `AddID(relID, Reflexive())` pattern verified in `id_ops.go` 94).

8. **Both code paths**: enforce in immediate (`addIDImmediate` in `id_ops.go`) AND deferred (`batchForEntity` in `cmd_queue.go`). **This is the gotcha that caught Phases 15.2 and 15.4** — do not skip the deferred path.

9. **Docs updates per CONTRIBUTING.md**:
   - `docs/Relationships.md` — Acyclic section: replace callout with shipped content + example showing ChildOf cycle rejection.
   - `docs/HierarchiesManual.md` — note that ChildOf cycles are now prevented at AddID time (replaces the implicit \"don't do this\" caveat).
   - `docs/ComponentTraits.md` — trait-system roadmap row → ✅ shipped.
   - `docs/README.md` — gap list pruned.
   - `CHANGELOG.md` and `ROADMAP.md` — v0.41.0 entry; explicitly note divergence from C (write-time enforcement vs. C's lookup-time guards).

### Non-goals

- NO retroactive cycle detection on existing data (only new adds are checked).
- NO automatic cycle breaking — just rejection by panic.
- NO performance optimization for deep chains (correctness first; cycle-check cost is O(chain length) per add).
- NO bootstrap of Acyclic on IsA — C does not, and we follow C here (IsA's separate recursion guard is independent).

### Mechanical acceptance

- `go vet ./...`, `golangci-lint run ./...` clean.
- `go test ./... -race -count=3` clean.
- Coverage on main package ≥ 95%.
- Existing tests pass without modification (the ChildOf bootstrap is the risk — any test that accidentally constructs a ChildOf cycle must be fixed or the test is invalid).
- New `acyclic_test.go` covers the 9 cases.
- Docs updates land in the same PR.

### Style notes

- Match the trait-pattern precedent (small file, `map[ID]bool` policies, `Set/Is` API, `apply<Trait>Policy` internal helper).
- Reuse `walkUp` from `traversal.go` for the cycle walk (its `maxWalkDepth` guard at line 6 prevents infinite walks if existing data already has a cycle from before the bootstrap landed).
- Panic message must identify the entity, relationship, and target — match the format of other trait panics (cf. `exclusive.go`).
- New helper functions must accept `scope` per Phase 15.8 convention.

## Constraints

- @specs/CONTRIBUTING.md — docs-update-with-feature requirement; CHANGELOG + ROADMAP entries; coverage ≥ 95% on main package; go vet + golangci-lint clean; race-tested test suite.
- @CLAUDE.md — project conventions for Go-port code style and divergences-from-C documentation.
- @cleanup.go — trait-pattern reference; cleanup policy `map[ID]struct{...}` is the precedent for `acyclicPolicies map[ID]bool` storage shape.
- @instantiate_policies.go — trait-pattern reference; another `apply<X>Policy` shape.
- @exclusive.go — trait-pattern reference; panic-message format for relationship-trait violations.
- @cantoggle.go — small-trait file precedent (Set/Is API surface).
- @symmetric.go — composition-with-mirror-add precedent; relevant to test case 8 (Acyclic + Symmetric atomicity).
- @transitive.go — query-time cycle handling at the walker layer; Acyclic complements this at write time.
- @reflexive.go — closest structural precedent (Phase 15.7); `applyReflexivePolicy` shape and bare-tag self-pair registration are the templates.
- @world.go — built-in entity registration block (lines 254-271 for the allocator pattern; line 288 for the `applyReflexivePolicy(w, w.isAID)` bootstrap insertion site).
- @id_ops.go — `addIDImmediate` is where the Acyclic hook lives (line 29 entry, line 94 self-pair Reflexive precedent).
- @cmd_queue.go — `batchForEntity` deferred-path enforcement; do not skip per the 15.2/15.4 gotcha.
- @childof.go — verify any existing ad-hoc cycle handling and ensure the new write-time check does not duplicate `EachChild`'s recursion guard.
- @traversal.go — reuse `walkUp` (line 26 signature) for the cycle-detection walk; already takes `*World` and respects the `maxWalkDepth` guard.
- @docs/Relationships.md — Acyclic callout to be replaced with shipped section.
- @docs/HierarchiesManual.md — ChildOf cycle-prevention note.
- @docs/ComponentTraits.md — trait-system roadmap table row.
- @docs/README.md — gap list pruning.
- @CHANGELOG.md — v0.41.0 entry, divergence-from-C note.
- @ROADMAP.md — Phase 15.9 → shipped.
- C ground truth: `EcsAcyclic` is a built-in entity (not a flag bit); bootstrap applies it to ChildOf and EcsWith/EcsTraversable only (NOT IsA); enforcement in C is at lookup/traversal time, not at AddID. Our port deliberately enforces at AddID — document as divergence.
