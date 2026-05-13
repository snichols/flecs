## Goal

Port the three upstream usage-constraint trait markers — `Relationship`, `Target`, and `Trait` — as Phase 15.15, shipping in **v0.47.0** (next after Phase 15.14 Traversable at v0.46.0, merge `11eda33`).

These three traits ship together because **`Trait` only has meaning in combination with `Relationship`** (it exempts a target entity from `Relationship`'s "no-tag-as-target" check). Splitting them across phases would either leave dead code or require an awkward v0.47.0+v0.48.0 dependency dance.

**What the traits do** — documented at @docs/ComponentTraits.md (lines 553-563):

- **`Relationship`** — an entity marked with this can only appear as the **relationship** side (first element) of a pair. Adding it as a plain tag, or as a pair target without the Trait exemption, panics.
- **`Target`** — an entity marked with this can only appear as the **target** side (second element) of a pair. Adding it as a plain tag, or as a relationship of a pair, panics.
- **`Trait`** — modifier that **exempts** an entity from `Relationship`'s "no-tag-as-target" constraint when used as a pair target. (Trait entities themselves are allowed as pair targets even when the relationship has the `Relationship` flag.) Wildcard targets are also exempt — see `component_index.c:396` `!ecs_id_is_wildcard(rel)` guard.

### C reference (verified against `/work/agents/claude/projects/SanderMertens/flecs`)

- Constant definitions: `include/flecs.h:1861-1886` — `EcsTrait` (1864), `EcsRelationship` (1875), `EcsTarget` (1886). World IDs: `src/world.c:65-67` — `EcsTrait = HI+35`, `EcsRelationship = HI+36`, `EcsTarget = HI+37`.
- Bootstrap: `src/bootstrap.c:891-893` (make-alive), `1016-1018` (`flecs_bootstrap_trait` macro from `src/private_api.h:67-69` which calls `ecs_add_id(world, name, EcsTrait)`).
- Built-in `Relationship` self-classifications at `src/bootstrap.c:1280-1288`:
  - `EcsChildOf`, `EcsIsA`, `EcsSlotOf`, `EcsDependsOn`, `EcsWith`, `EcsOnDelete`, `EcsOnDeleteTarget`, `EcsOnInstantiate`, `ecs_id(EcsIdentifier)`.
- Built-in `Target` self-classifications at `src/bootstrap.c:1291-1293`:
  - `EcsOverride`, `EcsInherit`, `EcsDontInherit`. **Note: `EcsRemove`, `EcsDelete`, `EcsCascade`, `EcsThrow` are NOT marked Target in upstream** — the goal's draft listed these as candidates, but upstream behavior is the authoritative source. Do not bootstrap them.
- Built-in `Trait` self-classifications at `src/bootstrap.c:1060-1061`:
  - `EcsChildOf`, `EcsIsA` are both marked `Trait` (in addition to `Relationship`). This is what permits patterns like `(SomeRel, ChildOf)` where `ChildOf` appears in the target slot.
- Enforcement site: `src/storage/component_index.c:384-481` inside the pair-validation block. Specifically:
  - Lines 394-405: `IsRelationship(tgt)` check in pair target slot, with `!ecs_id_is_wildcard(rel) && !ecs_has_id(world, rel, EcsTrait)` exemption — note the exemption is keyed on the **relationship**, not the target. (Re-verify implication for our port; the Go bare-tag check below is for plain non-pair adds where there's no ambiguity.)
  - Lines 408-414: `IsTarget(rel)` check in pair relationship slot.
  - Lines 466-481: bare-tag (non-pair) `IsRelationship(rel) || IsTarget(rel)` rejection — \"cannot use 'X' by itself\".

### Pattern references (Go)

Read these files directly via `@`-syntax (do NOT paraphrase):

- @final.go — simplest single-flag trait, write-time panic via `checkFinal`
- @oneof.go — relationship trait with a tied second map (parent ref); useful since Trait/Relationship interact
- @traversable.go — most recently shipped (Phase 15.14, v0.46.0, merge `11eda33`), demonstrates the implication pattern (Traversable→Acyclic) and the Phase 15.8 `scope` interface
- @singleton.go — both immediate and deferred path enforcement template
- @id_ops.go (lines 85-176) — central hook site for trait policy application and write-time checks
- @cmd_queue.go (lines 140-176) — deferred path coalesce-time application/enforcement

## Constraints

- @docs/ComponentTraits.md — Phase 15.15 docs section flip; index map at line 11-18, trait body at line 554, roadmap rows at lines 833/837/838 (currently \"⏳ planned\"). Update to \"✅ shipped (v0.47.0)\" with usage example covering all three traits including the Trait-exemption pattern.
- @docs/Relationships.md — if it discusses pair construction or \"what can be a target\", add a callout pointing at these new constraint markers.
- @docs/README.md — flip the three roadmap entries to shipped.
- @README.md — feature list entry if applicable.
- @CHANGELOG.md — new top-section `## v0.47.0 — <date> — Phase 15.15: Relationship/Target/Trait usage constraints`, following the format of the v0.46.0 entry at line 3.
- @ROADMAP.md — three shipped rows; bump heading at line 3 from \"Shipped (through v0.46.0)\" to \"Shipped (through v0.47.0)\".
- @meta_test.go (line 18) — `builtinEntityCount` is currently 29; bump to **32** (three new built-ins). Update the comment block at lines 11-17 to add Relationship(28), Target(29), Trait(30), shifting Wildcard→31, Any→32.
- @world.go (lines 310-327) — current allocation order is Traversable(27) → Wildcard(28) → Any(29). Insert three new allocations after Traversable and before Wildcard so the new layout is Traversable(27) → Relationship(28) → Target(29) → Trait(30) → Wildcard(31) → Any(32). Add `relationshipID`, `targetID`, `traitID` to the World struct (near `traversableID` declaration around line 78), and `relationshipPolicies`, `targetPolicies`, `traitPolicies` maps near the existing policy maps (around line 87).
- @isa_test.go, @marshal_test.go — baseline test fixups for the new entity count and marshal skip-set.
- @CONTRIBUTING.md — documentation update process; follow the existing trait-shipping conventions (no behavior-change surprises, all docs in lockstep, no skipped hooks).

## Deliverables

### 1. New file `usage_constraints.go` (single file recommended)

Bundle the three small traits in one file with a documenting header explaining why they ship together. (Three separate files duplicates header boilerplate without clarifying anything; the three are semantically a unit.)

- Public surface:
  - `func (w *World) Relationship() ID { return w.relationshipID }`
  - `func (w *World) Target() ID { return w.targetID }`
  - `func (w *World) Trait() ID { return w.traitID }`
  - `func SetRelationship(w *World, id ID)` / `func IsRelationship(s scope, id ID) bool`
  - `func SetTarget(w *World, id ID)` / `func IsTarget(s scope, id ID) bool`
  - `func SetTrait(w *World, id ID)` / `func IsTrait(s scope, id ID) bool`
- Internals: `applyRelationshipPolicy`, `applyTargetPolicy`, `applyTraitPolicy` keyed by `ID(id.Index())` (per Phase 15.8 convention).
- Internal `checkUsageConstraints(w *World, e ID, id ID)` consolidating the bare-tag and pair-form panics; called from both the immediate and deferred paths.

### 2. Bootstrap (in `world.go`)

- Allocate `relationshipID` at index 28, `targetID` at index 29, `traitID` at index 30 (after Traversable, before Wildcard), shifting `wildcardID`→31, `anyID`→32.
- Update the index-map comment block in `world.go` (around line 141) accordingly.
- Bootstrap self-classifications (mirroring `src/bootstrap.c:1060-1061, 1280-1293`):
  - `Relationship`: `IsA`, `ChildOf`, `OnDelete`, `OnDeleteTarget`, `OnInstantiate`. (`SlotOf`, `DependsOn`, `With`, `Identifier` are upstream but not yet ported to Go; skip those — they'll be added when their built-ins land.)
  - `Target`: `Override`, `Inherit`, `DontInherit`. (Upstream does **not** mark `Remove`, `Delete`, `Cascade`, `Throw`; do not add them.)
  - `Trait`: `IsA`, `ChildOf` (both also marked `Relationship`; this is what permits `(SomeRel, ChildOf)` patterns).

### 3. Write-time enforcement (`id_ops.go` `addIDImmediate`)

- After existing bare-tag policy branches (around line 124 where `traversableID` is handled), add bare-tag branches for `relationshipID`, `targetID`, `traitID` that apply the corresponding policy when `e` receives the bare flag.
- After existing `checkFinal`/`checkOneOf` blocks (around line 140-148), add usage-constraint checks:
  - Bare-tag add (`!id.IsPair()`): if `IsRelationship(s, id)` → panic with `\"flecs: cannot add '%v' as a bare tag: has the Relationship trait and must be used in a pair as relationship\"`; same shape for `IsTarget(s, id)`.
  - Pair add (`id.IsPair()`):
    - `IsTarget(s, id.First())` → panic `\"cannot use '%v' as relationship in pair '%v': has the Target trait\"`.
    - `IsRelationship(s, id.Second()) && !IsTrait(s, id.Second())` → panic `\"cannot use '%v' as target in pair '%v': has the Relationship trait (mark target as Trait to exempt)\"`. Wildcard target exemption is **not** needed here because Wildcard is not bootstrapped Relationship — see Open decision 1.

### 4. Deferred-path enforcement (`cmd_queue.go` `batchForEntity`)

- Mirror the bare-tag policy-apply branches at line 154-168 for the three new tags.
- Mirror the pair-form usage-constraint check on `cmdAddID` so deferred ops panic at coalesce time, not before. (Note: upstream `component_index.c` enforces at the storage layer, which fires for both paths; Go's two-path symmetry is what we maintain here.)

### 5. Test file `usage_constraints_test.go` (single file, `t.Run` sub-groups)

Minimum 12 cases:
1. `SetRelationship(R)` then `Add(e, R)` (bare tag): panics with clear message naming `R` and the Relationship trait.
2. `SetRelationship(R)` then `Add(e, Pair(R, T))` where T is plain: succeeds.
3. `SetRelationship(R)` then `Add(e, Pair(T, R))` where T is plain: panics (R in target slot).
4. `SetTarget(T)` then `Add(e, T)` (bare tag): panics.
5. `SetTarget(T)` then `Add(e, Pair(R, T))` with plain R: succeeds.
6. `SetTarget(T)` then `Add(e, Pair(T, X))`: panics (T in relationship slot).
7. **Trait exemption**: `SetRelationship(R) + SetTrait(M)` then `Add(e, Pair(R, M))`: succeeds (without Trait the same `Add` would panic).
8. Built-in bootstrap: `IsRelationship(s, IsA)`, `IsRelationship(s, ChildOf)`, `IsRelationship(s, OnDelete)`, `IsRelationship(s, OnDeleteTarget)`, `IsRelationship(s, OnInstantiate)` all true at world init.
9. Built-in bootstrap: `IsTarget(s, Override)`, `IsTarget(s, Inherit)`, `IsTarget(s, DontInherit)` all true at world init. Negatively: `IsTarget(s, RemoveAction)`, `IsTarget(s, DeleteAction)`, `IsTarget(s, PanicAction)` all **false** (these are not marked Target in upstream — see C research).
10. Built-in bootstrap: `IsTrait(s, IsA)`, `IsTrait(s, ChildOf)` both true (this permits e.g. `(SomeRel, ChildOf)`).
11. Deferred path: same Add operations inside `Write(func(fw *Writer){ ... })` panic at coalesce time, not before queue submission. Use `defer recover()` inside the Write block to verify the panic site.
12. `IsRelationship` / `IsTarget` / `IsTrait` round-trip + idempotence (set twice → still true; set on already-bootstrapped entity → no-op).
13. **Composition with other traits**: `SetRelationship(R) + SetExclusive(R)`, `SetRelationship(R) + SetTraversable(R)`, `SetRelationship(R) + SetTransitive(R)` all work without conflict (these compositions exist on `IsA` and `ChildOf` already; the new check must not break them).
14. **Component-on-Target panic**: registering a component, marking it Target, then `Add(e, componentID)` panics (components are tag-added by AddID — the bare-tag rule covers it).

Coverage target: **≥ 95.0%** (current repo baseline).

### 6. Marshal / baseline fixups

- @marshal.go skip-set: add `relationshipID`, `targetID`, `traitID` to the built-in skip list so they're not serialized.
- @marshal_test.go: fixture updates if the test enumerates built-in entities.
- @isa_test.go: any test that counts entities or enumerates IDs needs the new count.
- @meta_test.go: `builtinEntityCount` 29→32; comment block update; any related entity-enumeration tests.

### 7. Doc updates (per CONTRIBUTING.md and project convention)

- @docs/ComponentTraits.md:
  - Section at line 554: flip from \"Not yet ported in Go flecs\" callout to a full section matching the Traversable section style (line 729+), with usage example for all three traits and the Trait-exemption pattern.
  - Roadmap rows at lines 833, 837, 838: flip ⏳ planned → ✅ shipped (v0.47.0) with one-line summary per row.
- @docs/Relationships.md: callout pointing at the new constraint markers in any \"pair construction\" / \"what can be a target\" section.
- @docs/README.md: flip the three roadmap entries to shipped.
- @README.md: feature list addition if appropriate.
- @CHANGELOG.md: new top section `## v0.47.0 — <date> — Phase 15.15: Relationship/Target/Trait usage constraints`, following v0.46.0 entry format (Added / Changed / Breaking changes if any).
- @ROADMAP.md: heading bump line 3 \"through v0.46.0\" → \"through v0.47.0\"; add three shipped rows.

## Open decision points for the iterate agent

1. **Wildcard / Any treatment.** Upstream `component_index.c:396` has `!ecs_id_is_wildcard(rel)` exemption inside the Relationship-as-target check, but `EcsWildcard` itself is **not** bootstrapped Relationship or Target. Recommend: do NOT bootstrap Wildcard or Any with either marker; this keeps `(R, Wildcard)` query patterns working without special-casing. Document this choice explicitly in the file header and in the docs. If the iterate agent finds a case where it matters, surface it.

2. **Self-pair `(R, R)` when R is Relationship-only.** Should this panic? Recommended: yes — R is in target slot of `(R, R)`, so the existing Relationship-as-target check fires naturally without a special case. Add an explicit test for the self-pair to lock in the behavior.

3. **Component-on-Target.** Should adding a component (which has data) to an entity marked Target panic? Components are tag-added by AddID, so yes — the existing bare-tag rule covers it. Verify in test case 14.

## Non-goals

- Do NOT bundle `DontFragment` or `Union` (sparse storage path; separate structural phase).
- Do NOT retroactively add `Relationship` / `Target` markers to user-defined entities. The marker is opt-in for backward compatibility. The bootstraps in Deliverable (2) cover built-ins where the constraint is universally correct; user-level entities stay unconstrained unless the user opts in.
- Do NOT add a way to remove a constraint marker once set — symmetry with prior traits (Final, Exclusive, etc.) where the marker is \"set once, sticky for the world\". State this explicitly in the file header.

## Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run` clean.
- `go test ./... -race -count=3` passes.
- Coverage ≥ 95.0%.
- All `@`-references in docs resolve; no `⏳ planned` rows remaining for these three traits.
