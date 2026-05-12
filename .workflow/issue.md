## Goal

Port the Flecs **Exclusive** relationship trait: a marker that constrains a relationship to allow at most one target per source entity. Adding a second target with the same relationship replaces the existing pair rather than coexisting.

This is the next pull from the trait-system roadmap in `docs/ComponentTraits.md`. Phases 15.0 (cleanup policies, v0.32.0) and 15.1 (OnInstantiate Override/DontInherit, v0.33.0) shipped the per-id-record policy storage pattern; Exclusive extends the same map machinery and lays foundational work for the future Symmetric (15.3?) and Transitive (15.4?) phases.

**Target version: v0.34.0.**

### C semantics (research summary)

C truth, ground-checked:

- `EcsExclusive` is declared in `flecs.h:1827` as a built-in entity, registered by `flecs_bootstrap_trait(world, EcsExclusive)` at `bootstrap.c:1010`.
- `EcsIdExclusive = (1u << 9)` lives in `include/flecs/private/api_flags.h:72`. The flag bit attaches to the **pair-wildcard component record** for the relationship (i.e. the cr for `(R, *)`), not to the relationship entity directly.
- Exclusive is applied as a **self-applied tag**: `ecs_add_id(world, R, EcsExclusive)` (see `bootstrap.c:1259-1263`). It is *not* a pair. A global observer at `bootstrap.c:1144-1153` watches OnAdd/OnRemove of the EcsExclusive tag on a relationship entity and toggles `EcsIdExclusive` on the corresponding `(R, *)` cr (via `flecs_register_trait` → `flecs_set_id_flag` at `bootstrap.c:138-143`).
- The **replace-existing-pair behavior** lives in `src/storage/table_graph.c:1062-1073` inside `flecs_table_traverse_add`: when adding `(R, B)` to a table that already contains `(R, A)` and `cr->flags & EcsIdExclusive`, C builds a *new destination table type* by copying the current type and overwriting the existing pair index in-place. This is a single table migration, not a Remove+Add pair of operations — but the resulting diff (source → destination) shows `(R, A)` removed and `(R, B)` added, so OnRemove and OnAdd fire naturally via `flecs_emit` during commit.
- Built-in relationships marked Exclusive in C (`bootstrap.c:1259-1262, 1324-1325`): **`ChildOf`, `OnDelete`, `OnDeleteTarget`, `OnInstantiate`, `ParentDepth`, `SlotOf`, `OneOf`**. The pre-bootstrap shortcut at `bootstrap.c:705-708` also hard-codes `cr_childof_wildcard->flags |= EcsIdExclusive` so ChildOf is exclusive from the very first allocation.
- **`IsA` is NOT exclusive in C** — multiple base prefabs per instance are allowed. The prompt's intuition was wrong here; verify against C before applying.
- Symmetric and Transitive use the same trait-tag + global-observer mechanism. Building Exclusive cleanly here clears the path.

### Current Go state (verified)

- `ChildOf` in our port currently **allows multiple parents** — `childof.go:35-36` documents the `ParentOf` accessor as picking the first `(ChildOf, *)` in the signature, calling multiple parents `"allowed but unusual"`. This phase fixes that to match C's invariant.
- `World.cleanupPolicies map[ID]cleanupPolicyFlags` (`world.go:68`) and `World.instantiatePolicies map[ID]instantiatePolicyFlags` (`world.go:69`) are the existing per-id-record stores; Exclusive needs the same shape.
- `addIDImmediate` (`id_ops.go:29-81`) is the central hook point — it already translates `(OnDelete, action)` and `(OnInstantiate, action)` pair-adds into policy-map writes (lines 43-69), and calls `w.migrate(e, addID, removeID, value)` at line 71. The Exclusive replace logic plugs in just before the migrate call.

## Constraints

- @cleanup.go — Phase 15.0's pattern: `cleanupPolicyFlags` typed-uint8 with bit constants, `SetCleanupPolicy`/`GetCleanupPolicy` accessors, `applyCleanupPolicy` helper called from both the public setter and the pair-add detection path in `addIDImmediate`. Mirror this shape exactly for Exclusive.
- @instantiate_policies.go — Phase 15.1's pattern: same shape as cleanup.go, with the per-component-entity map on `World`. The recommended Phase 15.2 shape is a parallel `World.exclusivePolicies map[ID]bool` (or a single-bit `exclusivePolicyFlags` type for consistency). Alternative: consolidate all three into one `World.relationshipFlags map[ID]uint32` — but the existing precedent is one map per trait family, so stay parallel.
- @world.go — Index 17 is the first user entity slot (line 105: "Index 17+: user entities"). Allocate the built-in `Exclusive` entity at index 17 with `World.exclusiveID ID` and accessor `func (w *World) Exclusive() ID`. Update the index-allocation comment block at lines 87-105 and bump "first user entity" mention at line 67. After all built-in entities are allocated, bootstrap the Exclusive flag on `ChildOf` (mandatory — fixes the existing multi-parent gap), `OnDelete`, `OnDeleteTarget`, `OnInstantiate`. Do **not** mark `IsA` Exclusive — C doesn't, and the prompt was wrong.
- @id_ops.go — Extend `addIDImmediate` (line 29) with two pieces: (1) when the added id is the bare `w.exclusiveID` self-applied to a relationship entity, write the flag into `w.exclusivePolicies` (mirrors the `applyCleanupPolicy` pair-detection block at lines 43-63 but for a non-pair id). (2) Before calling `w.migrate` at line 71, if `id.IsPair()` and `w.exclusivePolicies[id.First()] == true`, scan the entity's current archetype signature for any existing `(id.First(), *)` pair where the target differs from `id.Second()`; if found, route through the standard migrate path with both `addID` and `removeID` populated so hooks fire correctly. The single-call form `w.migrate(e, newPair, oldPair, nil)` already handles both add and remove in one transition — use it.
- @childof.go — Update the `ParentOf` doc at lines 33-38 to drop the "multiple parents allowed but unusual" caveat after Exclusive bootstrap takes effect. The function body should still return the first match (defensive), but the documented invariant becomes one-parent-at-most.
- @isa.go — No behavioral change. Verify and document that IsA permits multiple bases.
- @docs/Relationships.md — Lines 265-295 already have an Exclusive section with a "Not yet ported in Go flecs" callout at line 295. Remove the callout and replace the section body with the shipped API: `flecs.SetExclusive(w, R)`, `flecs.IsExclusive(w, R)`, and a worked example showing the replace-on-add semantics. Also update line 502 (Union comparison reference) if needed.
- @docs/ComponentTraits.md — Lines 497-503 have the Exclusive section with a gap-link callout — replace with shipped content. Line 562's trait-roadmap table row currently shows `⏳ planned` with "No automatic single-target enforcement" — flip to `✅ shipped` (or whatever shipped marker the table uses for 15.0/15.1).
- @docs/README.md — Prune the Exclusive entry from the feature-gap list.
- @CHANGELOG.md, @ROADMAP.md — Add the v0.34.0 entry per the established release-note style.

## Implementation outline

1. **Storage.** Add `exclusivePolicies map[ID]bool` to the `World` struct (`world.go:68-69` block). Lazy allocation in the setter, like `cleanupPolicies`/`instantiatePolicies`.

2. **Built-in entity.** Allocate `Exclusive` at index 17 in `New()`, store as `w.exclusiveID`, expose via `func (w *World) Exclusive() ID`. Update index-allocation docblock comments at lines 87-105.

3. **New file `exclusive.go`** (sibling to cleanup.go, instantiate_policies.go). Exports:
   - `SetExclusive(w *World, relID ID)` — writes `w.exclusivePolicies[relID] = true`.
   - `IsExclusive(w *World, relID ID) bool` — reads the map.
   - `applyExclusivePolicy(w, relID)` internal helper called from both the public setter and `addIDImmediate`'s pair-add detection.

4. **Pair-add detection in `addIDImmediate`** (`id_ops.go:43-69` block). Two sub-cases:
   - Non-pair id equals `w.exclusiveID` and target entity `e` is a relationship — call `applyExclusivePolicy(w, e)`. This is the `Writer.AddID(myRel, w.Exclusive())` form, matching C's `ecs_add_id(world, MyRel, EcsExclusive)`.
   - **Note**: revise the prompt's pair-form suggestion. C does NOT use `(Exclusive, *)`; Exclusive is a bare tag. Drop any \"`MakePair(w.Exclusive(), w.True())`\" idea.

5. **Replace-existing-pair logic** in `addIDImmediate`, immediately before `w.migrate(e, id, 0, nil)` at line 71. Pseudocode:
   ```go
   if id.IsPair() && w.exclusivePolicies[id.First()] {
       // Scan rec.Table.Type() for existing (id.First(), *) where second != id.Second().
       // If found, call w.migrate(e, id, existing, nil) — single transition, both hooks fire.
       // If existing.Second() == id.Second(): the earlier rec.Table.HasComponent(id) guard at
       //   id_ops.go:34 already returned false, so we never reach this branch.
   }
   ```
   The single `migrate(add, remove, value)` call is the equivalent of C's table-graph in-place index replacement: one source→destination migration, diff shows both adds/removes, hooks fire from the standard emit path.

6. **Bootstrap in `New()`** — after the cleanup-policy bootstrap call at `world.go:216`, call `applyExclusivePolicy(w, w.childOfID)`, plus the same for `w.onDeleteID`, `w.onDeleteTargetID`, `w.onInstantiateID`. Verify this does not regress existing tests — if any test was implicitly relying on multi-ChildOf-parent behavior, it's revealing a pre-existing bug.

7. **Tests in `exclusive_test.go`** (NEW — the filename does not collide with the existing `exclusive_access_test.go`/`exclusive_access_norace_test.go` which cover the unrelated ExclusiveAccessBegin/End API). Test cases:
   1. **Default non-Exclusive relationship allows multiple targets** — AddID(R, A), AddID(R, B) on the same entity both stick; entity has both pairs.
   2. **Mark + add second target replaces first** — `SetExclusive(w, R)` then AddID(R, A), AddID(R, B) → entity has only (R, B). Hooks fire OnRemove for A, OnAdd for B.
   3. **Re-add same target is no-op** — Exclusive R, AddID(R, A) twice → second call returns false (the `HasComponent` guard at id_ops.go:34), no duplicate OnAdd.
   4. **ChildOf is exclusive after bootstrap** — adding `MakePair(w.ChildOf(), parent2)` to a child of `parent1` replaces the parent. `ParentOf(child)` returns `parent2`. (This is a behavior change from the current "multiple parents allowed but unusual" state — document as a fix in CHANGELOG.)
   5. **IsA is NOT exclusive** — confirmed regression test: adding two prefab bases via `(IsA, p1)` then `(IsA, p2)` keeps both pairs.
   6. **IsExclusive round-trip** — SetExclusive then IsExclusive returns true; default returns false.
   7. **Exclusive + cleanup interaction** — relationship R with `SetCleanupPolicy(w, R, OnDeleteTarget, DeleteAction)` AND SetExclusive: replacing the target (AddID(R, A) then AddID(R, B)) **does not delete A** — only the pair is removed from the source. (A is still alive as an entity; cleanup only fires when A itself is deleted.)
   8. **Pair-add form sets the flag** — `fw.AddID(myRel, w.Exclusive())` produces `IsExclusive(w, myRel) == true`, parallel to how `(OnDelete, DeleteAction)` pair-add works in 15.0.

## Non-goals

- No Symmetric trait. No Transitive trait. No Reflexive trait. Those are separate phases.
- No observer-driven enforcement layer — the runtime `addIDImmediate` check is sufficient. C does both (the observer toggles the cr flag; the table_graph code reads the flag); we only need the equivalent of the table_graph code.
- No changes to non-Exclusive relationship behavior. Anything that worked before this phase continues to work.
- No public API for clearing the Exclusive flag (`UnsetExclusive`) — not needed for v0.34.0; C does support unsetting via remove of EcsExclusive but no real-world code uses it.

## Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run ./...` clean.
- `go test ./... -race -count=3` clean.
- Main-package coverage ≥ 95%.
- Existing tests pass without modification **except** for tests that explicitly exercise multi-parent ChildOf (if any) — those should be migrated to use a user-defined non-Exclusive relationship instead. Audit `childof_test.go` for this; grep for AddID with two different (ChildOf, *) targets.
- `exclusive_test.go` exercises all 8 cases above.
- Docs land in the same PR.

## Style notes

- One file per trait family (cleanup.go, instantiate_policies.go, exclusive.go). Do NOT consolidate into a shared `traits.go`.
- The replace-existing-pair logic should route through the standard `w.migrate(e, addID, removeID, value)` path so hooks/observers fire correctly via the existing emit machinery. Do not introduce a separate "exclusive migration" code path.
- The C `EcsIdExclusive` lives on the pair-wildcard cr (`(R, *)`), not on R itself. In Go the equivalent is keying `w.exclusivePolicies` by the relationship entity ID directly (same as 15.0/15.1) — the pair-wildcard distinction is C-internal and does not need to be modeled here.
