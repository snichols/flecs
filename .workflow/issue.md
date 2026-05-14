## Goal

Ship **Phase 16.14: Prefab hierarchies + slots bundle** as **v0.69.0** — the next phase after v0.68.0 (Phase 16.13 dynamic components, shipped at commit `47cac29`).

Two prefab-related gaps that share the same subtree-copy infrastructure on IsA traversal, bundled because the slot mechanism IS a visible projection of the prefab→instance mapping that subtree-copy must build anyway:

- `docs/README.md:136` (Phase 14.5 Prefabs): **Prefab hierarchies** — when a prefab has `(ChildOf, prefab)` children, instantiating the prefab replicates the entire child subtree onto the instance.
- `docs/README.md:137` (Phase 14.5 Prefabs): **Prefab slots** (`SlotOf`) — `(SlotOf, prefab)` on a prefab child causes a `(prefabChild, instanceChild)` pair to be added to the instance on instantiation, resolving to the copied child in O(1) without a name lookup.

Both lines flip to ✅ shipped (v0.69.0).

### What each does

**Prefab hierarchies.** Today, `AddID(child, MakePair(IsA, prefab))` only copies the prefab's own components to `child` (via the `overrideCopyForInstance` hook at `id_ops.go:303-310`). It does NOT replicate the prefab's child entities. If a prefab `Tank` has children `Turret`, `Tracks`, instantiating `Tank` should produce a new entity with newly-spawned `Turret` and `Tracks` children, structured identically to the prefab, recursively.

Upstream C handles this in `src/instantiate.c:452-537` (`flecs_instantiate`) by walking the prefab's `(ChildOf, prefab)` graph and recursively copying each child entity with its components. The recursion site is at `instantiate.c:378-381`.

**Prefab slots.** Slots provide a way to reference a specific copied child without a name lookup. When a prefab child has `(SlotOf, prefab)`, the instantiation pipeline adds a `(prefabChild, instanceChild)` pair on the instance. The user then does `target, _ := GetPairTarget(instance, prefabChild)` in O(1).

This is what makes prefabs composable: "the Turret slot on this Tank instance is THAT entity over there."

### C-side references (verified)

Cited from `/work/agents/claude/projects/SanderMertens/flecs`:

- `EcsSlotOf` constant: `include/flecs.h:1911` (`FLECS_API extern const ecs_entity_t EcsSlotOf;`)
- `EcsSlotOf` allocation: `src/world.c:37` (`FLECS_HI_COMPONENT_ID + 12`)
- Bootstrap tag: `src/bootstrap.c:973` (`flecs_bootstrap_tag(world, EcsSlotOf);`)
- `EcsSlotOf` traits in bootstrap (`src/bootstrap.c`):
  - line 1274: `ecs_add_id(world, EcsSlotOf, EcsPairIsTag);`
  - line 1282: `ecs_add_id(world, EcsSlotOf, EcsRelationship);`
  - line 1324: `ecs_add_id(world, EcsSlotOf, EcsExclusive);`
- Prefab-hierarchy instantiation: `src/instantiate.c:207-386` (`flecs_instantiate_children`)
- Recursion site for grandchildren: `src/instantiate.c:378-381`
- Slot resolution: `src/instantiate.c:9-92` (`flecs_instantiate_slot`); detection at line 262 (`if (ECS_IS_PAIR(id) && ECS_PAIR_FIRST(id) == EcsSlotOf)`); pair-add at line 27 (`flecs_sparse_on_add_cr(world, r->table, …, cr, true, NULL)` where `cr = flecs_components_ensure(world, ecs_pair(slot, child))` — the **`slot`** parameter at this call site IS the prefab-child entity, and **`child`** is the instance-child copy. So the pair added on the instance is `(prefabChild, instanceChild)`.
- Cross-reference handling (the ChildOf rewrite): `src/instantiate.c:272-275` records the prefab→instance pair, then `instantiate.c:318` replaces it: `diff.added.array[childof_base_index] = ecs_pair(EcsChildOf, instance);`

### Go-side state (verified)

- `isa.go` — current IsA inheritance walk; `prefabOfInternal`, `EachPrefab`, `getViaIsA`, `hasViaIsA`. The override-copy hook is at `id_ops.go:303-310`, calling `overrideCopyForInstance` (defined at `id_ops.go:319-364`). Subtree-copy plugs in alongside this hook.
- `childof.go:25` — `World.EachChild(parent ID, fn func(child ID) bool)` already iterates direct children (respecting OrderedChildren). Subtree-copy uses this to walk prefab children.
- `ordered_children.go` — `World.OrderedChildren()`, `applyOrderedChildrenPolicy`. If a prefab parent has the trait, the iteration order is deterministic; the instance must also be marked ordered if we want the instance subtree to preserve order. Decision needed.
- `exclusive.go` — `applyExclusivePolicy(w, relID)` is the SlotOf-trait registration helper.
- `oneof.go` — pattern to follow for an exclusive single-target trait.
- `pairistag.go` — SlotOf is a tag relationship (only the structure of the pair matters, no payload).
- `query_filters.go:79-93` — `MarkPrefab` / `IsPrefab`; prefab children inside a prefab are themselves prefabs (the `Prefab` tag is inherited via `EcsTableIsPrefab` flag check in `instantiate.c:304-309`).

### Built-in entity ordering

Current state (post-v0.65.0 Phase 16.10 + v0.68.0 dynamic components):
- Index 45: DependsOn (added in v0.64.0)
- Index 46: EventMonitor (added in v0.65.0)
- Indices 47+: user entities
- `builtinEntityCount = 46` (see `meta_test.go:11-22`)

For Phase 16.14, **SlotOf is the next built-in at index 47**, with `EventMonitor` staying at 46 and user entities shifting to start at 48. This avoids renumbering existing indices.

(Alternative: insert SlotOf at 46 and bump EventMonitor to 47 — but this would renumber an existing v0.65.0 built-in and is not preferable. Lock in **append at 47**.)

### Pattern

The instantiation flow is a depth-first walk of the prefab's child tree. Visit each child of the prefab, allocate a new entity for the instance, copy components, recurse on grandchildren. Track a `map[prefabChild]instanceChild` to rewrite same-subtree cross-references. Slots ARE this mapping made visible as `(prefabChild, instanceChild)` pairs on the instance root.

## Constraints

- @docs/README.md — line 136 (Prefab hierarchies) and line 137 (Prefab slots) flip from "not yet ported" to "✅ shipped in v0.69.0". Format must match the per-line shipped style established by surrounding entries.
- @isa.go — current IsA semantics: reads consult IsA chain transitively on local miss; writes always land locally on child (copy-on-write override). Subtree-copy must NOT change this. Existing `getViaIsA` / `hasViaIsA` recursion stays untouched. The override-copy hook at `id_ops.go:303-310` is the existing site to extend with a sibling subtree-copy call.
- @id_ops.go — `addIDImmediate` is the choke point. The new subtree-copy fires from the same `if id.First().Index() == w.isAID.Index()` branch that currently calls `overrideCopyForInstance`. ChildOf-cycle / Exclusive / OneOf / Final guards at lines 222-285 must run BEFORE subtree-copy, on the root `(IsA, prefab)` add only. Subtree-copy itself uses `AddID(fw, ...)` recursively; each individual add re-enters `addIDImmediate` and gets its own guards via the standard path.
- @childof.go — `EachChild` is the iteration primitive. It already respects OrderedChildren ordering. No new iteration API needed.
- @ordered_children.go — if the prefab parent has OrderedChildren, the new instance subtree's parent should also be marked ordered, so EachChild on the instance returns children in the prefab's insertion order. State this explicitly in the test and behaviour spec.
- @exclusive.go — `applyExclusivePolicy(w, w.slotOfID)` bootstraps SlotOf as exclusive (only one slot per child), mirroring `src/bootstrap.c:1324`.
- @pairistag.go — `applyPairIsTagPolicy(w, w.slotOfID)` mirrors `src/bootstrap.c:1274`. SlotOf pairs are tag-shaped.
- @world.go — `slotOfID` field, `World.SlotOf() ID` accessor at index 47, allocated after EventMonitor in `New()`. Update the `Built-in entity allocation order` doc block at lines 151-199. Update `meta_test.go:22` `builtinEntityCount = 47`.
- @CONTRIBUTING.md — doc updates required: `docs/PrefabsManual.md` (new sections), `docs/README.md` (line flips), `README.md` (feature list), `CHANGELOG.md` v0.69.0 entry, `ROADMAP.md` heading bump to "through v0.69.0" (currently "through v0.68.0" at line 3).
- @CHANGELOG.md — v0.69.0 entry must appear at the top, following the established format of the v0.68.0 entry.
- @ROADMAP.md — line 3 reads `## Shipped (through v0.68.0)`; bump to `through v0.69.0`. Add a "Prefab hierarchies + slots bundle" bullet to the Shipped list.
- @meta_test.go — bump `builtinEntityCount` from 46 to 47 and update the comment at lines 11-22 to include SlotOf(47).

## Deliverables

1. **`SlotOf` built-in entity** (`world.go`):
   - `slotOfID` field; `World.SlotOf() ID` accessor.
   - Allocated after EventMonitor in `New()`, taking index 47. User entities start at index 48.
   - Bootstrap traits applied: `applyExclusivePolicy(w, w.slotOfID)`, `applyPairIsTagPolicy(w, w.slotOfID)`, `applyRelationshipPolicy(w, w.slotOfID)`.
   - `meta_test.go` `builtinEntityCount` bumped to 47 with comment line updated.

2. **Subtree-copy hook** (extending `id_ops.go:303-310`):
   - When adding `(IsA, prefab)` to instance `e`:
     - Existing behavior: call `overrideCopyForInstance(w, e, prefab, nil)`. Keep unchanged.
     - **New**: call a sibling `instantiateChildrenForInstance(w, e, prefab, nil)` that walks `EachChild(prefab)` and recursively spawns instance children.
   - For each prefab child `c`:
     - Allocate new entity `ic` via `NewEntity(fw)`.
     - Copy `c`'s components onto `ic` (mirroring the `flecs_instantiate_children` component-loop logic at `instantiate.c:241-298`; skip `DontInherit` components, skip the `(ChildOf, prefab)` pair, skip `(SlotOf, prefab)` pair — those get rewritten / projected to slots).
     - Add `(ChildOf, e)` to `ic`.
     - Apply pair rewriting (see deliverable 4).
     - If `c` has children of its own, recurse on grandchildren with the same instance subtree mapping.
   - Build a `map[ID]ID` from `prefabEntity → instanceEntity`, scoped to one root instantiation.

3. **Slot resolution** during child-copy:
   - If prefab child `c` has `(SlotOf, prefab)` (where `prefab` is the IsA target being instantiated):
     - Do NOT add `(SlotOf, prefab)` to `ic`.
     - Add the pair `(c, ic)` to the instance root `e`. C reference: `instantiate.c:9-27` (the slot-of-base branch).
   - The C nested-slot branch (lines 28-87, "slot is registered for other prefab") handles slots on grand-prefabs; defer to a follow-up unless it falls out trivially. State explicitly as a deferred enhancement if not implemented.
   - User-facing API:
     ```go
     tank := NewEntity(fw); MarkPrefab(fw, tank)
     turret := NewEntity(fw); MarkPrefab(fw, turret)
     AddID(fw, turret, MakePair(w.ChildOf(), tank))
     AddID(fw, turret, MakePair(w.SlotOf(), tank))

     inst := NewEntity(fw)
     AddID(fw, inst, MakePair(w.IsA(), tank))

     instTurret, ok := GetPairTarget(r, inst, turret) // → the copied turret child
     ```

4. **Cross-reference rewriting** (the tricky piece):
   - Build the `map[prefabChild]instanceChild` during the depth-first walk, populating each entry before recursing into the child's component-copy phase. (This ordering ensures that when a child's `(R, otherPrefabChild)` pair is processed, if `otherPrefabChild` was already walked, its mapping exists. If it hasn't been walked yet — sibling forward-reference — we need a two-pass approach: first pass allocates all instance IDs and populates the map; second pass copies components with rewrite. State this clearly in the implementation.)
   - When copying a pair `(R, target)` onto `ic`:
     - If `target` is in the prefab→instance map (same-subtree), rewrite to `(R, mappedTarget)`.
     - Otherwise leave unchanged (external reference).
   - **Rule (locked in)**: rewrite ONLY when `target` is part of the same root-instantiation subtree. References to entities outside the prefab subtree (e.g., a global Player entity, or a prefab sibling not under the same root) remain unchanged.
   - This generalizes the `instantiate.c:272-275 + :318` ChildOf-rewrite pattern from one specific pair (ChildOf) to all pairs whose target is in the prefab subtree.

5. **Deferred-path support**: instantiation inside `World.Write` must work via the existing coalescer. The subtree-copy is logically one operation but mechanically a sequence of individual `AddID` / `Set` calls. Each individual call coalesces normally through the existing cmd queue (`cmd_queue.go:510` `addIDImmediate` dispatch). Document this; no new coalescer logic needed.

6. **Tests** in `prefab_hierarchies_test.go` (at least 12 cases):
   1. Prefab Tank with components A, B; instantiate; instance has A, B but no children. Regression test for existing behavior.
   2. Prefab Tank with one child Turret; instantiate; instance has a new entity as its child with Turret's components. The new entity is distinct from the original Turret (different ID).
   3. Prefab with grandchildren (3 levels: Tank → Turret → Barrel); instantiate; full subtree replicated; deep entities have correct parents.
   4. Prefab child with `(SlotOf, prefab)`: instantiation creates `(prefabChild, instanceChild)` pair on the instance root.
   5. `GetPairTarget(instance, slotID)` returns the copied child; the slot resolves in O(1) without name lookup.
   6. Cross-reference rewriting: prefab has children A and B with `(SomeRel, B)` on A; instance's copy of A has `(SomeRel, copyOfB)` (NOT `B`).
   7. External references not rewritten: prefab child has `(SomeRel, externalEntity)` where `externalEntity` is outside the prefab subtree; instance keeps `(SomeRel, externalEntity)` unchanged.
   8. Multiple instances: each `AddID(child, MakePair(IsA, prefab))` produces a fresh subtree with distinct entity IDs; no sharing across instances.
   9. Prefab parent with `OrderedChildren`: instance is also marked ordered; `EachChild(instance)` returns children in the prefab's insertion order.
   10. `(SlotOf, X)` where `X` is NOT a prefab parent of the child: panic at trait validation time (SlotOf only meaningful within a prefab subtree). OR: silently no-op for slot resolution if not inside an IsA copy. State the chosen rule and test it.
   11. Marshal round-trip: instantiate, marshal, unmarshal, verify subtree structure and slot pairs survive.
   12. Deep prefab hierarchy (4+ levels): correct subtree replication; spot-check that all grandchildren are fresh entities, not aliases of prefab grandchildren.
   - Coverage ≥ 95.0%.

7. **Doc updates** (per `CONTRIBUTING.md`):
   - `docs/PrefabsManual.md` — add `## Prefab hierarchies` and `## Prefab slots` sections (TOC entries too at line 7). Both with code examples. Update line 413 "Not yet ported" section to remove these two entries.
   - `docs/README.md` — flip lines 136 and 137 to ✅ shipped in v0.69.0.
   - `README.md` — feature list bump.
   - `CHANGELOG.md` — v0.69.0 entry at top.
   - `ROADMAP.md` line 3 — heading bump to "through v0.69.0"; add bullet to Shipped list.

## Constraints (additional design rules)

- **Cross-reference rewriting scope (locked)**: same-subtree only — rewrite only when target is part of the same root instantiation. External references unchanged.
- **SlotOf semantic (locked)**: slot ID = the prefab child entity itself. Matches C `instantiate.c:9-27` (`flecs_components_ensure(world, ecs_pair(slot, child))` where `slot` is the prefab child entity). API: `GetPairTarget(instance, prefabChild) → instanceChild`.
- **External reference treatment (locked)**: leave unchanged. Do NOT panic on detection.
- **Prefab-of-prefab (deferred)**: a prefab `B` with `(IsA, prefabA)` is NOT in scope. Document as future enhancement. If it falls out naturally from the recursion, allow it but do not write tests beyond level-1 IsA from instance to prefab.
- **Nested-slot branch (state explicitly)**: the C `instantiate.c:28-87` path (slot registered on a grand-prefab, instance hierarchy traversal upward) — implement if straightforward; otherwise defer with a clearly-documented gap.
- **No prefab variants / overrides beyond what IsA already supports** — `Phase 4.3` copy-on-write override semantics remain unchanged.
- **No automatic re-instantiation** when the prefab is mutated post-instantiation. Instantiation is a one-time copy.
- **No provenance tracking** ("this instance came from this prefab"). Instances are independent after creation.

## Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%
- No regression on existing IsA / prefab / OnInstantiate / Final tests
- `meta_test.go` `builtinEntityCount` test updated and passing

## Process notes

- Feature, not bug.
- Cited @-references and line numbers in this issue are verified against `flecs` repo HEAD `5836c1e` (post-v0.68.0 workflow housekeeping; the v0.68.0 ship commit itself is `47cac29`) and upstream `/work/agents/claude/projects/SanderMertens/flecs` (current HEAD).
- Phase 14.5 (PrefabsManual port) is the original lineage gap source.
- Target version: **v0.69.0** (next after v0.68.0).
