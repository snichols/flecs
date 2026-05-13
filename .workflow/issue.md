## Goal

Port the upstream `EcsOrderedChildren` trait to Go flecs as **Phase 15.18 / v0.50.0**, following the recently-shipped With trait (Phase 15.17, v0.49.0, merge `a52cdd6`).

When `OrderedChildren` is added to a parent entity, iterating its direct children via `EachChild` must return them in **insertion order**, independent of any archetype-table reshuffling caused by component composition changes on the children. Without the trait, current archetype-derived iteration order is preserved unchanged.

`OrderedChildren` is opt-in per parent — set explicitly with `SetOrderedChildren(w, parent)` or the bare-tag form `fw.AddID(parent, w.OrderedChildren())`. Not bootstrapped on any built-in entity.

### Deliverables

**1. `ordered_children.go`** — new file modeled on `reflexive.go` / `oneof.go`:

- `func (w *World) OrderedChildren() ID` — built-in entity, **index 33**. New ordering after Phase 15.17: `…With=32, OrderedChildren=33, Wildcard=34, Any=35, user=36+`. `builtinEntityCount` 34 → 35.
- `func SetOrderedChildren(w *World, parent ID)` — equivalent to `fw.AddID(parent, w.OrderedChildren())`.
- `func IsOrderedChildren(s scope, parent ID) bool` — accepts the `scope` interface per Phase 15.8 convention.
- `applyOrderedChildrenPolicy(w *World, parent ID)` — internal; initializes the per-parent list and **snapshots existing `(ChildOf, parent)` children** into it (matches upstream `flecs_ordered_children_populate` in `src/storage/ordered_children.c:25-42`).

**State**: add field `orderedChildren map[ID]*orderedChildList` on `World` (keyed by parent index, mirroring `oneOfPolicies`/`reflexivePolicies` pattern). The list holds child entity IDs.

**Implementation choice — slice or linked list**: upstream C uses a contiguous vec with `ecs_vec_remove_ordered_t` (O(n) memmove-on-remove) at `src/storage/ordered_children.c:99`. Recommend matching: **plain `[]ID` slice with O(n) ordered remove (`append(s[:i], s[i+1:]...)`).** No child→index map. Rationale: simplicity, cache-friendly iteration, matches upstream, and re-parent/delete are not hot paths relative to query iteration. The user-recommended swap-with-last is wrong — it breaks the ordering guarantee that is the entire point of this trait. State this rationale in CHANGELOG.

**2. State maintenance hooks** (sites identified by upstream cite):

- `id_ops.go addIDImmediate`: after a successful `(ChildOf, parent)` add to child `e`, if `IsOrderedChildren(parent)`, append `e` to that parent's list. Mirrors upstream `flecs_ordered_children_reparent` in `src/storage/ordered_children.c:127-145` (called from `src/component_actions.c:126`).
- `id_ops.go removeIDImmediate`: before the migration completes for `(ChildOf, parent)` removal from `e`, if `IsOrderedChildren(parent)`, remove `e` from the list. Mirrors `flecs_ordered_children_unparent` in `src/storage/ordered_children.c:147-155` (called from `src/component_actions.c:141`).
- **Re-parent path**: `ChildOf` is bootstrapped Exclusive (`world.go:382`). The exclusive-replacement branch in `addIDImmediate` (`id_ops.go:178-195`) calls `w.migrate(e, id, sigID, nil)` — which fires `OnRemove` for the old pair and `OnAdd` for the new pair. Ensure both the remove hook and add hook trigger our per-parent list maintenance. **State and test re-parent A→B explicitly** — both A and B ordered, both A and B unordered, mixed.
- `cmd_queue.go batchForEntity`: deferred path coalesce. The deferred add/remove cmds collapse into the final `commitBatch` call. Add/remove hook firings need to run from the coalesce path too, after `commitBatch`. Walk `addedIDs` for `(ChildOf, parent)` where `IsOrderedChildren(parent)` and append; walk `removedIDs` for the mirror. Pattern is identical to the Symmetric mirror at `cmd_queue.go:292-301`.
- `world.go deleteOne` (around line 610): when deleting entity `e`, walk `w.orderedChildren` and remove `e` from every list it appears in. If `e` is itself an ordered parent, drop its list entirely (`delete(w.orderedChildren, e)`). Matches upstream detach in `src/on_delete.c:211-214`.
- `world.go deleteImmediate` cascade: cascade-deleted children are removed via `deleteOne`, so the above hook covers them. State this explicitly.

**3. `EachChild` modification** (`childof.go:19-30`):

```go
func (w *World) EachChild(parent ID, fn func(child ID) bool) {
    w.checkExclusiveAccessRead()
    if list, ok := w.orderedChildren[ID(parent.Index())]; ok {
        // Snapshot the list at iteration start to mirror the safe-iteration
        // pattern used elsewhere in flecs (matches non-mutating semantics
        // of upstream ecs_children).
        snapshot := append([]ID(nil), list.entries...)
        for _, child := range snapshot {
            if !fn(child) {
                return
            }
        }
        return
    }
    // Existing archetype-based path unchanged.
    pairID := MakePair(w.childOfID, parent)
    w.compIndex.Each(pairID, func(t *table.Table) bool {
        for _, child := range t.Entities() {
            if !fn(child) {
                return false
            }
        }
        return true
    })
}
```

Callback signature preserved; existing users unaffected.

**4. Test file `ordered_children_test.go`** — at least 10 cases:

1. `SetOrderedChildren(parent)` → add C1, C2, C3 → `EachChild` yields C1, C2, C3 in order.
2. Without the trait: existing archetype-derived behavior unchanged (regression assertion against current order).
3. Five children, remove the middle one: order preserves the remaining four with the middle gone (covers ordered-remove correctness, not swap-with-last).
4. Re-parent — both ordered: child C moves from ordered A to ordered B; C is gone from A's list; C appended to B's list.
5. Re-parent — source ordered, dest unordered: C gone from A's list; B unaffected (no list).
6. Re-parent — source unordered, dest ordered: C appended to B's list; A unaffected.
7. Delete a child entity: removed from the parent's list (verify via `EachChild` enumeration; no leaked tombstones).
8. Delete the parent entity: ordered list dropped; cascade-deleted children also dropped from any other ordered-parent lists they appear in (compose with multi-parent edge case if applicable — though ChildOf is exclusive so usually N/A).
9. Deferred path: `w.Write(func(fw) { fw.AddID(child, MakePair(w.ChildOf(), parent)) })` after parent is ordered — coalesced add appends to list at flush time.
10. `IsOrderedChildren` round-trip and idempotence (`SetOrderedChildren` called twice is a no-op).
11. `EachChild` invoked from inside a `Read` block via `fr` honors ordering (scope-interface composition).
12. Stress: 1000 children added, verify iteration order; remove every other child, verify the surviving 500 are in original order.
13. **Set-after-children-exist (snapshot semantics)**: create parent + 3 children first; then `SetOrderedChildren(parent)`; verify `EachChild` returns the 3 existing children in their current archetype order. Document that the snapshot captures whatever order they happen to have at trait-set time.
14. **Iteration-during-mutation**: callback inside `EachChild` calls `AddID` to create a new child of the same parent. With snapshot-at-iteration-start semantics, the new child is NOT seen during this iteration but IS seen on the next one. State and test both directions (add and remove during iteration).

Coverage ≥ 95.0%.

**5. Doc updates per CONTRIBUTING.md:68-72**:

- `docs/ComponentTraits.md:495-503` — flip section from "Not yet ported" to "Shipped in v0.50.0" with full description, Go usage example showing `SetOrderedChildren(parent)` + `EachChild` ordering guarantee, snapshot-on-set semantics, snapshot-on-iteration semantics, and storage/perf note (slice, O(n) ordered remove).
- `docs/ComponentTraits.md:924` — roadmap row `OrderedChildren` → `✅ shipped (v0.50.0)`.
- `docs/ComponentTraits.md:23` — TOC entry already exists; verify link still valid.
- `docs/HierarchiesManual.md:65-78` — add a callout to the `EachChild` section pointing at OrderedChildren for the ordering guarantee.
- `docs/HierarchiesManual.md:309` — remove `OrderedChildren` from the "Not yet ported" list.
- `docs/README.md:130` — remove the OrderedChildren feature-gap bullet (or flip to "shipped" if other gaps remain in the same section — verify the section's surrounding bullets).
- `README.md` — add OrderedChildren to the trait list / feature bump.
- `CHANGELOG.md` — new v0.50.0 entry at top, following the format of v0.49.0 entry (Phase 15.17 With) on line ~54.
- `ROADMAP.md:3` — heading bump from `Shipped (through v0.49.0)` to `Shipped (through v0.50.0)`; append a Phase 15.18 line in the shipped list around line 54.

**6. Marshal** (`marshal.go:100-298`, `:324-435`):

The existing marshal model **re-applies policy state via replay**: on unmarshal, the `(parent, OrderedChildren)` tag-add re-fires `addIDImmediate` → `applyOrderedChildrenPolicy`, which snapshots the children that already exist at that point. **Because Phase 2 of unmarshal processes entities in topo order with parents allocated first, then their ChildOf links established, then other pairs** — the snapshot taken when `(parent, OrderedChildren)` is replayed will capture all already-linked children in the same order they were created in the original world.

**However** there is a subtle correctness risk: the topo order serializes child-of-P after P, but a *grandchild* of P (whose parent is one of P's children) is serialized in topo order **after** its own immediate parent — and the immediate parent's `(immediate_parent, OrderedChildren)` tag might be processed *before* the grandchild's `(ChildOf, immediate_parent)` link is added. In that case, the snapshot of immediate_parent's list will be empty at the moment the OrderedChildren tag is replayed, and the grandchild will be appended via the normal `addIDImmediate` ChildOf hook on its own pair-add. **This still produces the correct order** because both paths preserve insertion order. State and test this round-trip explicitly:

- **Marshal round-trip test**: parent P with OrderedChildren + children C1, C2, C3 + grandchild G1 (parent C2) + G1 also has OrderedChildren on itself. Serialize, deserialize, verify `EachChild(P)` returns C1, C2, C3 in order and `EachChild(G1)` is correctly initialized.

Also update the skip-set in `marshal.go:101-136` to include `w.OrderedChildren()`. Update `builtinEntityCount` 34 → 35 in `meta_test.go:19` and the comment block. Update the comment block in `world.go:121-160` to insert OrderedChildren at index 33, shift Wildcard→34, Any→35, user→36+. Update the field-doc comments in `world.go:52-85` similarly. Audit all test files for hardcoded built-in indices (grep `34\|35` in test files alongside `Wildcard\|Any\|builtinEntityCount`).

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%

### Non-goals

- Do NOT bundle Sparse, DontFragment, or Union (separate phases).
- Do NOT make `ecs_set_child_order` reorder API a first-class export in this phase. (It's a follow-up; the trait alone is the deliverable.) State this as a deferred follow-up in CHANGELOG.
- Do NOT add per-relationship `OrderedChildren` variants (i.e., ordered for `IsA` or arbitrary traversal rels). C upstream supports only `ChildOf` ordered children; match that.
- Do NOT scan the entire entity index on every child add/remove to detect "is this parent ordered" — keep the check O(1) via the `orderedChildren` map lookup.

### Open decision points (recommendations stated above; iterate agent should confirm at write time)

1. **Storage**: `[]ID` slice with O(n) ordered remove. Matches upstream; ordered removal is the whole point of the trait.
2. **Set-after-children-exist**: snapshot existing children at trait-set time. User-friendly; matches upstream `flecs_ordered_children_populate`.
3. **Marshal**: rely on replay (no explicit serialization of the list). Existing topo order + replay-applies-policy semantics already used by Reflexive, OneOf, Exclusive, etc.
4. **Iteration during mutation**: snapshot the list at `EachChild` entry. Callback's adds/removes do not affect the in-progress iteration.

## Constraints

- @ROADMAP.md — phase numbering: this is Phase 15.18 in the trait-port series (15.7 Reflexive, 15.8 scope, 15.9 Acyclic, 15.10 Final, 15.11 OneOf, 15.12 Singleton, 15.13 WriteOnce, 15.14 Traversable, 15.15 Relationship/Target/Trait, 15.16 PairIsTag, 15.17 With). Shipped header must bump to `through v0.50.0`.
- @CONTRIBUTING.md — doc-update rules: any new public API gets godoc; new behavior gets a `docs/` page section; new phase gets ROADMAP move + CHANGELOG entry.
- @reflexive.go — pattern reference for bare-tag-add policy registration via `addIDImmediate` (line 96-100). Use the same idiom: bare tag → `applyOrderedChildrenPolicy(w, e)` in the `else if id.Index() == w.orderedChildrenID.Index()` branch in `id_ops.go addIDImmediate`. **However**, unlike Reflexive which only flips a flag, OrderedChildren's apply must additionally snapshot existing `(ChildOf, parent)` children — handled by walking `w.compIndex.Each(MakePair(w.childOfID, parent), …)`.
- @oneof.go — pattern reference for per-parent map state (line 32: `s.scopeWorld().oneOfPolicies[ID(relID.Index())]`). Use identical keying: `w.orderedChildren[ID(parent.Index())]` so generation-stripped pair extraction works correctly.
- @with.go — most recent shipped trait, pattern reference for both immediate and deferred hook expansion. Note the deferred-path expansion at `cmd_queue.go:191-195`; OrderedChildren needs an analogous hook for post-commit list maintenance (but its trigger is `(ChildOf, parent)` add/remove, not a per-source co-add expansion — so the shape is simpler than With).
- @final.go — bare-tag built-in template for `World.OrderedChildren() ID` accessor and field name.
- @childof.go — direct edit site for `EachChild` modification (lines 19-30). The existing function signature and table.Table iteration must be preserved on the non-ordered path.
- @id_ops.go — `addIDImmediate` (line 29) and `removeIDImmediate` (line 274) are the hook sites. The new branch must run AFTER `w.migrate` so the table state is consistent when the hook fires. State the ordering explicitly. Also: when the added id is `(ChildOf, parent)`, the hook must check `IsOrderedChildren(parent)` AFTER the migrate so a freshly created child's record is in the new table.
- @cmd_queue.go — `batchForEntity` (line 112) is the deferred coalesce site. Mirror the Symmetric pattern (line 292-301) for the post-`commitBatch` hook calls.
- @world.go — `deleteOne` (line 610) is the entity-deletion hook site; mirror the `singletonInstances` cleanup at line 629-633 to clean up `orderedChildren`. Also: built-in entity allocation block (line 121-160 + field declarations 52-85), add a new field `orderedChildrenID ID` and the index-33 allocation slot.
- @world.go:382 — `applyExclusivePolicy(w, w.childOfID)` already established. This means re-parent is `remove (ChildOf, A)` + `add (ChildOf, B)` via the exclusive replacement branch in `addIDImmediate:178-195` — state and test that both hooks fire in the correct order during that single migration.
- @marshal.go:101-136 — the skip-set for built-in entities. Add `w.OrderedChildren(): {}`.
- @marshal.go:324 — `UnmarshalJSON` replay: topo-ordered Phase 1 entity allocation + Phase 2 pair replay. The OrderedChildren policy is re-applied via the (parent, OrderedChildren) pair replay; existing children at that moment are snapshotted into the list. No explicit serialization of `orderedChildren` is needed.
- @meta_test.go:11-19 — `builtinEntityCount` constant: bump from 34 to 35, update the comment listing built-ins. Audit `meta_test.go` and other test files for `34` and `35` literals tied to entity numbering.
- @docs/ComponentTraits.md:494-503 — current placeholder section. Flip to shipped with full prose, Go usage example, and the storage/perf note.
- @docs/ComponentTraits.md:924 — roadmap table row.
- @docs/HierarchiesManual.md:65-78 — `EachChild` section needs an OrderedChildren cross-reference.
- @docs/HierarchiesManual.md:309 — remove the "Not yet ported" bullet.
- @docs/README.md:130 — remove the OrderedChildren feature-gap bullet.
- @CHANGELOG.md — v0.50.0 entry at top following v0.49.0 / Phase 15.17 With format.
- C upstream `src/storage/ordered_children.c` (entire file) — reference implementation; the Go port mirrors structure but uses Go-idiomatic slice operations and the `orderedChildren` field on `World` instead of per-`ecs_component_record_t` storage.
- C upstream `src/bootstrap.c:604-630` — `flecs_register_ordered_children` observer pattern. In Go flecs the equivalent runs synchronously in `addIDImmediate` (no observer infrastructure for built-in trait registration; pattern matches Reflexive/Final/etc.).
- C upstream `src/each.c:110-141` — the iteration site that consults the ordered list. The Go port's site is `childof.go:19 EachChild`.
- C upstream `src/component_actions.c:126,141` — `flecs_ordered_children_reparent` and `flecs_ordered_children_unparent` are called from the table-migration path. The Go port hooks at `addIDImmediate`/`removeIDImmediate` immediately after `w.migrate`.
