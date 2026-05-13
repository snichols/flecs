## Goal

**Architectural significance.** This is the **largest architectural addition since the base archetype port**: a second, parallel storage backend that lives alongside the SoA archetype tables. Sparse-stored components are kept in a per-component sparse-set keyed by entity ID, so adding/removing them does not transition the owning entity's archetype, and their addresses are stable across migrations of other components on the same entity. Because the surface area is large enough to overflow a single iterate cycle, **this phase is deliberately scoped to storage + manual write/read/remove only**.

**Explicit scoping (read this first).** This phase ships:

- The `Sparse` built-in trait entity and `SetSparse` / `IsSparse` / `w.Sparse()` APIs.
- A typed sparse-set internal data structure.
- Write/read/remove routing for sparse components on the immediate AND deferred paths.
- Entity-delete cleanup of sparse entries.
- Marshal round-trip of sparse-set state.
- A small iteration convenience (`EachSparse[T]`).

**This phase does NOT ship:**

- **Query integration.** Query terms naming a Sparse component are explicitly out of scope; that is Phase 15.20.
- **`DontFragment`.** Builds on Sparse; ships in a later phase.
- **`Union`.** Separate trait; ships later.

The split is intentional — see the iterate-agent notes below — and must remain visible in `docs/ComponentTraits.md` so users do not expect query-side behavior yet.

**Target version:** v0.51.0 (next after v0.50.0 OrderedChildren shipped at `17ad14d`).

## Why now

The Sparse trait unblocks several follow-on features (`DontFragment`, `Union`, pointer-stable singletons, components touched by a small fraction of entities without archetype-table fragmentation). Splitting storage from query integration keeps each iterate cycle reviewable and lets the storage layer settle before query plumbing depends on it.

## What the trait does

`Sparse` (upstream `EcsSparse`, `include/flecs.h:1973-1974`) marks a component such that:

- Component data is stored in a per-component sparse-set keyed by entity ID, not as a column in archetype tables.
- Adding/removing a Sparse component does **not** cause an archetype transition for the owning entity — the entity stays in its current archetype table.
- Pointer stability: a Sparse component's address does not move when other components on the same entity change.
- Components added to a small fraction of entities don't fragment the archetype table space.

**Note on the C semantics.** In upstream, the Sparse flag controls only the *storage* of the data; the component still appears in the entity's archetype `type`. The "no archetype transition" property is actually contributed by the `EcsIdDontFragment` flag in upstream C (`src/storage/component_index.c:144-180` `flecs_component_init_sparse` + `flecs_component_record_init_dont_fragment`). In this port, since `DontFragment` is not yet ported, **Go-flecs Sparse implies both behaviors at once** — it is the user-visible "data lives elsewhere AND no archetype transition" trait. Document this consolidation in `ComponentTraits.md` and `CHANGELOG.md` so the divergence is auditable. When `DontFragment` lands later, the trait can be split.

## C research (upstream cite-list)

All line numbers verified against `/work/agents/claude/projects/SanderMertens/flecs`:

1. **`EcsSparse` constant.** Declared in `include/flecs.h:1973-1974`; defined in `src/world.c:99` as `FLECS_HI_COMPONENT_ID + 57`.
2. **Bootstrap.** `src/bootstrap.c:894` (`flecs_bootstrap_make_alive(world, EcsSparse)`), `src/bootstrap.c:1022` (`flecs_bootstrap_trait(world, EcsSparse)`), and `src/bootstrap.c:1176-1178` (observer registers `EcsIdSparse` flag on the component record when `EcsSparse` is added).
3. **Sparse-set data structure.** Declared in `include/flecs/datastructures/sparse.h:32-43` (`ecs_sparse_t`). Dense vector + sparse page index. Public API: `flecs_sparse_init/fini/add/get/insert/ensure/remove/has/count` (`sparse.h:53-329`).
4. **Trait-to-flag wiring on table.** `src/storage/table.c:301-302` (when a component entity has `EcsSparse`, its tables get the `EcsIdSparse` trait flag).
5. **Sparse-set per-component init.** `src/storage/component_index.c:144-180` `flecs_component_init_sparse` — allocates `cr->sparse` of size `cr->type_info->size` when `cr->flags & EcsIdSparse`.
6. **Set-flag-on-trait-add dispatch.** `src/bootstrap.c:125-147` — when `EcsIdSparse` is added to `cr->flags`, `flecs_component_init_sparse` runs.
7. **Storage primitives.** `src/storage/sparse_storage.c:8-57` `flecs_component_sparse_has`, `:59-118` `flecs_component_sparse_get`, `:121-161` `flecs_component_sparse_remove_intern`.
8. **Write/insert path.** `src/component_actions.c:145-182` `flecs_sparse_on_add_cr` (route inserts to sparse-set on `EcsIdSparse`); `:184-208` `flecs_sparse_on_add` (component-wide variant).
9. **Remove path.** `src/component_actions.c:210-228` `flecs_sparse_on_remove`.
10. **Read path.** `src/entity.c:43-50` (sparse data fetch), `src/entity.c:116-125` (IsA base walk), `src/entity.c:1914-1925` (`ecs_get` direct path), `src/entity.c:2062`.
11. **Has check.** `src/entity.c:2573-2580` `ecs_has_id` (consults `flecs_component_sparse_has` when `cr->flags & (EcsIdDontFragment|EcsIdMatchDontFragment)`), `src/entity.c:2649-2651` `ecs_owns_id` same pattern.
12. **Entity-delete cleanup.** `src/storage/component_index.c:252-265` `flecs_component_fini_sparse` (called when the component record is finalized), plus `src/component_actions.c:222-228` per-table sparse remove on archetype transition.
13. **Type semantics.** In pure-Sparse-without-DontFragment, the component DOES appear in `cr` and the table's `component_map` (`src/component_actions.c:548-553` and `:609-619` switch on `cr->flags & EcsIdSparse` for hook dispatch). In Sparse+DontFragment, the component does NOT appear in the table's type. Since Go-flecs is consolidating, **Go Sparse should NOT add the component to the entity's archetype** — `HasID` and `OwnsID` consult the sparse-set, not the archetype.
14. **Set-after-use trap.** Upstream allows `EcsSparse` to be set after use because the storage path checks `cr->flags & EcsIdSparse` per operation. In Go-flecs, mirror the Phase 15.16 `SetPairIsTag`-after-use trap: panic if the component has already been added to any entity via the archetype path.

## Pattern reference (Go side)

- `@oneof.go` — `applyPolicy` style + map-of-flag policy state on `World`.
- `@exclusive.go` — simplest precedent for "trait owns a `bool` flag per ID."
- `@ordered_children.go` — closest precedent for "trait owns its own auxiliary storage outside the archetype tables" (the `orderedChildren map[ID]*orderedChildList`); also includes the `applyPolicy` dispatch from `addIDImmediate` + the marshal round-trip pattern.
- `@pairistag.go` — precedent for "panic on data-write path" and for the set-after-use trap (`SetPairIsTag` panics if a `(R, *)` data-pair already exists).
- `@with.go` — recent example of immediate + deferred path enforcement (`applyWithCoAdds` and `expandWithIntoScratch`).
- `@world.go` lines 40-122 — `World` struct layout for trait policy maps; lines 619-665 — `deleteOne` cleanup pattern (singleton/writeOnce/ordered-children entries are cleared on entity delete).
- `@value_ops.go:255-289` — `setImmediateByPtr` is the central immediate write site. Sparse routing inserts here.
- `@scope.go:380-409` — `HasID` / `OwnsID` are the central Has-check sites. Sparse routing inserts here.
- `@scope.go:502-529` — `AddID` / `RemoveID` deferred-path command queueing.
- `@cmd_queue.go:421-424` — deferred `cmdSetByID` flush — sparse routing inserts here for the deferred path.
- `@id_ops.go:60-156` — `applyPolicy` dispatch table inside `addIDImmediate` — `Sparse` joins this list.

## Deliverables

### 1. `sparse.go`

- `World.Sparse() ID` — built-in entity. **Index 34.** Current state (verified against `world.go:84-86` and `meta_test.go:11-19`): OrderedChildren=33, Wildcard=34, Any=35, `builtinEntityCount = 35`, user entities start at 36. After this phase: OrderedChildren=33, **Sparse=34**, Wildcard=35, Any=36, `builtinEntityCount = 36`, user entities start at 37.
- `SetSparse(w *World, componentID ID)` — marks the component sparse-stored; panics if the component has already been added to any entity via the archetype path.
- `IsSparse(s scope, componentID ID) bool` — uses `scope` interface (Phase 15.8 convention).
- `applySparsePolicy(w *World, componentID ID)` — writes the policy flag; dispatched from `addIDImmediate` for the bare-tag form (`fw.AddID(componentID, w.Sparse())`).
- `World.sparsePolicies map[ID]bool` flag map (keyed by raw index, matching prior trait pattern).

### 2. Sparse-set storage

- Recommended location: `internal/sparse/set.go` (new package) or extend `internal/component/`. Pick whichever fits the existing layout — `internal/sparse/` keeps the storage independent of `component` metadata.
- **Shape:** `World.sparseStorage map[ID]*sparseSet` where `*sparseSet` holds the data for one component across all entities. Each set has:
  - `dense []sparseEntry` (entries) where `sparseEntry` is `{entity ID, data *T-box}` or `{entity ID, data []byte}` plus a type descriptor.
  - `index map[uint32]int` mapping entity raw-index → dense slot.
  - `typeInfo *component.TypeInfo` for size/align/reflect.Type.
- **Pointer stability** (open decision 1): use the **`*T` boxed approach** — each dense entry holds a pointer to a heap-allocated copy of the value. Pointer returned by `Get` is the boxed pointer itself, stable across slice growth. Justification: simpler to implement, naturally pointer-stable, no fixed-capacity backing strategy needed. Modest GC cost is acceptable for the v1 storage path. State and justify in the package doc.
- **Per-entity holding tracking** (open decision 2): add `World.sparseHeld map[uint32][]ID` (entity raw-index → list of component IDs this entity holds as Sparse). Recommend **list approach**. Justification: O(k) entity-delete cleanup where k = sparse components held by that entity, vs. O(n) iterating all sparse-capable components in the world. State and justify in `world.go`.

### 3. Write/read/remove path wiring

- **Write path:**
  - `setImmediateByPtr` (`value_ops.go:259`): if `w.sparsePolicies[ID(id.Index())]`, route to `sparseSetInsert(w, e, id, srcPtr)` and **return early** — do NOT call `w.migrate`. Fire OnAdd/OnSet hooks against the sparse-set entry.
  - Deferred `cmdSetByID` flush (`cmd_queue.go:421-424`): branch on `IsSparse(componentID)` before calling `setImmediateByPtr`.
  - `Set[T]` deferred path (`scope.go:445-461`): no change needed — it queues `cmdSetByID` which the flusher routes correctly.
- **Read path:**
  - `GetID` / `getOnWorld[T]` (`world.go:854-873`): if `IsSparse`, fetch from the sparse-set; **do NOT walk the archetype table** for the sparse component. Still walk IsA for inheritance (Sparse + IsA interaction is allowed; see test 9 below).
  - `GetByID` (`value_ops.go`): same branch.
  - `GetRef[T]`: same branch — returns the boxed pointer.
- **Remove path:**
  - `removeIDImmediate` / `removeImmediate[T]` (`world.go:920-936`, `id_ops.go`): if `IsSparse(id)`, call `sparseSetRemove(w, e, id)` and return; do NOT call `w.migrate`. Fire OnRemove against the sparse-set entry first.
  - Deferred `cmdRemoveID`: branch in the flusher.
- **`AddID` (bare-tag add)** (open decision: tag vs. data-bearing):
  - **Recommendation:** Sparse components are data-bearing. Panic on `AddID(e, sparseComponentID)` with a clear message: `"flecs: AddID: cannot add Sparse component %v as a tag; use SetID with a value"`. Same path for deferred form. Justify in `sparse.go` doc comment.
- **`HasID` / `OwnsID`** (the trickiest piece — `scope.go:382-409`):
  - If `id` names a Sparse component, look in `sparseStorage[id]`'s index map instead of the entity's archetype type. The entity's archetype type does NOT contain a Sparse component (consequence of "no archetype transition").
  - Pair-form Sparse: out of scope for this phase — `SetSparse` rejects non-component entities (see non-goal below).
  - State this design choice in the `IsSparse` doc comment and exercise it in tests.

### 4. Entity-delete cleanup

In `deleteOne` (`world.go:619-665`), after the existing per-table OnRemove loop and before `w.index.Free(e)`: for each component ID in `w.sparseHeld[uint32(e.Index())]`, fire OnRemove against the sparse-set entry and call `sparseSetRemove(w, e, cid)`. Then `delete(w.sparseHeld, uint32(e.Index()))`. Mirrors the existing singleton/writeOnce/ordered-children cleanup blocks already in this function.

### 5. `SetSparse` after first use

If `SetSparse(w, cid)` is called and any entity already has `cid` in its archetype (`w.compIndex.TablesFor(cid)` returns a non-empty set with at least one entity), panic with: `"flecs: cannot mark %v as Sparse: component is already in use on %d entities via archetype storage"`. Mirrors the Phase 15.16 `SetPairIsTag`-after-use trap.

### 6. Marshal

Sparse-set state IS world state and MUST round-trip. Update `marshal.go`:

- Add a top-level field `Sparse map[ID]map[ID]json.RawMessage` (component ID → entity ID → JSON-encoded value). Use `info.Type` from the registry for typed marshal/unmarshal via `reflect`.
- Also marshal `sparsePolicies` so `IsSparse` survives round-trip even for components with no held entries.
- **Unmarshal order:** `sparsePolicies` BEFORE entities (so the "is this component sparse" lookup is live during entity replay). Sparse-set entries populate AFTER entities are created (so the entity IDs exist). Document the ordering with a comment.
- Add test cases in `marshal_test.go` for round-trip with and without sparse data.

### 7. `sparse_test.go` (at least 12 cases)

1. Basic write/read/remove of a sparse component on one entity.
2. Multiple entities each with the same sparse component, different values.
3. Adding/removing a sparse component does NOT change the entity's archetype (assert `entityindex.Get(e).Table` identity is unchanged before and after `Set` and `Remove`).
4. Pointer stability: get pointer, mutate other (non-sparse) components on the entity to force archetype migration, re-get pointer — same address, same value.
5. `HasID(e, sparseComponentID)` returns true after Set, false after Remove.
6. `AddID` (no value) on a sparse component panics with the documented message.
7. `SetSparse(R)` after an entity already has R via archetype: panics with the documented message.
8. Entity delete removes the entity's sparse entries (verify via `HasID` check on the freed ID slot before re-allocation, or via inspecting `len(sparseStorage[cid].dense)` decreasing).
9. Re-allocation of an entity ID (`Delete` + `NewEntity`): the new entity does NOT inherit the deleted entity's sparse data.
10. Deferred path: `w.Write(func(fw) { Set[T](fw, e, v) })` flushes correctly when T is Sparse, including when batched with non-sparse Set calls on the same entity.
11. Marshal round-trip preserves sparse-set state and `IsSparse` policy.
12. `IsSparse` round-trip + idempotence (`SetSparse` called twice is a no-op).
13. **Composition (open decision 4):** `SetSparse(C) + SetFinal(C)` (or similar prior trait) — both apply without conflict; assert behavior is independent.
14. **Explicit non-coverage:** any test exercising query terms with sparse components must `t.Skip("Sparse query integration is Phase 15.20")`.

Coverage ≥ 95.0% on `sparse.go` and the new sparse-set internal package.

### 8. `EachSparse[T]` iteration convenience (open decision 3)

**Recommended:** ship a small, separate iteration entry point so users can use the storage without query integration:

```go
func EachSparse[T any](s scope, fn func(e ID, v *T))
```

Iterates the dense vector of `sparseStorage[cid]` for component T. Two tests:

- Visits all entities holding T as Sparse, exactly once.
- Iteration order is dense (insertion order in the sparse-set).

Justification: ~30 lines of code, gives users something concrete to do with sparse storage before query integration lands, and the iteration order semantics are predictable. Without this, Sparse is write-only-by-entity in v1 — usable but unobservable in bulk.

### 9. Composability with prior traits (open decision 4)

State explicitly in the issue body (and in `sparse.go` doc) that **Sparse composes orthogonally with prior traits**:

- Sparse + `Final`: a Sparse component can be Final; both apply. Test it.
- Sparse + `WriteOnce`: a Sparse component can be WriteOnce; the first-write trap applies on the sparse-set write path. Test it.
- Sparse + `Singleton`: a Sparse component can be Singleton; the at-most-one-holder check applies. Test it (or `t.Skip` with rationale if it surfaces design questions worth deferring).
- Sparse + `CanToggle`: deferred — toggling a Sparse component's bitset would require a parallel structure. Out of scope; document the gap.
- Sparse + relationships (`Exclusive`, `OneOf`, etc.): out of scope; Sparse only applies to non-pair components in this phase.

### 10. Doc updates (per CONTRIBUTING.md Phase 14.0+ rule)

- `docs/ComponentTraits.md` § Sparse: flip from "not yet ported" to **"shipped in v0.51.0 (storage path)"**. State explicitly: "Query integration is deferred to Phase 15.20 — terms naming a Sparse component will not match in queries yet." Document the Go-Sparse-implies-DontFragment consolidation.
- `docs/EntitiesComponents.md` line 330: flip the not-yet-ported callout to reference v0.51.0 storage-only.
- `docs/README.md` line 74: flip the feature-gap entry to "**partially shipped in v0.51.0** — storage and manual write/read/remove only; query side pending in Phase 15.20."
- `README.md` line 165 region: feature list bump to add Sparse storage.
- `CHANGELOG.md` v0.51.0 entry. **Flag the "part 1 — storage only" framing prominently** at the top of the Added section.
- `ROADMAP.md`: shipped row under "Shipped (through v0.51.0)" heading (bump heading). Phrase: `**Sparse component storage (storage path)** *(v0.51.0, Phase 15.19)* — ... Query integration deferred to Phase 15.20.`

## Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%
- **All existing tests must continue to pass.** This phase MUST NOT regress archetype-stored components. Specifically: `Set`/`Get`/`Has`/`Remove`/`AddID`/`RemoveID` on non-Sparse components must take the existing archetype path bit-for-bit. The Sparse branch is gated entirely on `w.sparsePolicies[ID(id.Index())]`.

## Explicit non-goals for this phase

- **No query integration.** Queries with terms naming a Sparse component are explicitly out of scope. `NewQuery` / `NewCachedQuery` / `Each1`-`Each4` with a sparse-component term: not handled. Phase 15.20 handles this.
- **No `DontFragment`.** Separate trait; ships later. Go-Sparse currently consolidates both behaviors.
- **No `Union`.** Separate trait; ships later.
- **No automatic Sparse on any built-ins.** Fully opt-in via `SetSparse(componentID)`.
- **No `Sparse` for non-component entities** (tags, relationships, pairs). Reject in `SetSparse` if `info, ok := w.registry.LookupByID(componentID)` returns `!ok || info.Size == 0`. Panic with: `"flecs: SetSparse: %v is not a registered data-bearing component"`. Sparse implies data.

## Open decision points — recommended resolutions

1. **Pointer stability:** `*T` boxing. State + justify.
2. **Per-entity sparse-held tracking:** explicit list (`sparseHeld map[uint32][]ID`). State + justify.
3. **`EachSparse[T]` convenience:** ship it.
4. **Composability:** explicit orthogonal composition with prior traits; one composition test (Sparse + Final) at minimum.

## Process

- Feature, not bug.
- Verify all `@`-references and line numbers before filing (done above).
- Label: `snichols/queued`.
