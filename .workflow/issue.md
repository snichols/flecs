## Goal

Ship two coupled cleanup-cascade features deferred on the ROADMAP `Future Work` section, plus matching documentation cleanup. Both share the same dispatch infrastructure and ship together for coherence as **Phase 16.48 / v0.103.0**.

### Feature 1 — `OnDelete` / `OnDeleteTarget` observer events

Currently the cleanup-policy side ships (Phase 15.0 / v0.32.0 — `SetCleanupPolicy` and the bit layout in `@cleanup.go`), but no observer event surface exists for users who want to react to cascading deletes. This phase introduces two new built-in event entities mirroring the existing OnAdd/OnSet/OnRemove/OnTable* set (`@world.go:90-97`):

- `EventOnDelete` — fires once per entity whose lifecycle is about to end (via `Delete(e)` or via cascade), **before** the existing `OnRemove` hook/observer dispatch in `deleteOne` (`@world.go:1077-1099`).
- `EventOnDeleteTarget` — fires once per (target, dependent, pairRel) triple during cleanup-policy cascade in `deleteImmediate` (`@world.go:1195-1316`), **before** the dependent is enqueued for delete-or-remove.

**Accessors** (mirror Phase 16.43/16.46 surface):
- `(*World).EventOnDelete() ID`
- `(*World).EventOnDeleteTarget() ID`

**Registration API** (mirror `OnTableEmpty`/`OnTableFill` shape from Phase 16.43 and `OnTableDelete` from Phase 16.46):
- `OnDelete(w *World, fn func(fr *Reader, e ID))`
- `OnDeleteWithOptions(w *World, opts ObserverOptions, fn ...) *Observer` — supports `WithQuery(terms...)` multi-term filter
- `OnDeleteTarget(w *World, fn func(fr *Reader, target ID, dependent ID, pairRelID ID))`
- `OnDeleteTargetWithOptions(w *World, opts ObserverOptions, fn ...) *Observer`

**Dispatch ordering** (the design call):

1. User calls `Delete(e)` (or cascade is triggered by parent delete).
2. **Fire `OnDelete` observers for `e`** — handler can read `e`'s state via `*Reader`.
3. Fire existing `OnRemove` hooks for each component on `e` (current `deleteOne` behavior, `@world.go:1090-1099`).
4. Walk dependents (entities with `(R, e)` where `R` has cleanup policy) — current cascade DFS in `deleteImmediate` at `@world.go:1208-1290`:
   - **Fire `OnDeleteTarget` observers for `(e, dependent, R)`**.
   - Apply cleanup policy (Delete/Remove/Panic).
   - For each dependent that gets deleted, recurse step 2.
5. Free `e`'s slot.

**`*Reader` (not `*Writer`) is deliberate** — the entity is mid-delete; mutation during the dispatch window is unsafe. Handlers that need to mutate can schedule via `World.Write(fn)` which queues commands for after the current cascade settles. Document this explicitly. This matches the Phase 16.46 `OnTableDelete` precedent (handler receives `*Reader` for the dying table).

**Filtering:**
- `WithQuery(Terms...)` filters by signature (multi-term, same engine as Phase 16.15 / 16.43).
- Pair-relationship filter for `OnDeleteTarget`: `OnDeleteTargetWithOptions(w, WithRelationship(ChildOf), fn)` — fires only for cascades where `pairRelID == ChildOf`. Re-uses or extends existing observer-options surface in `@observer_options.go`.

**`WithYieldExisting()` semantics:** no-op for both events (delete events are future-only). Mirrors the Phase 16.46 precedent.

### Feature 2 — Component-remove active cascade

**Verified current behavior** by tracing `deleteImmediate` (`@world.go:1195-1316`) and `deleteOne` (`@world.go:1077-1169`):

- `deleteImmediate` walks `w.cleanupPolicies` for **OnDeleteTarget** policies only (`@world.go:1225-1289`). It never iterates `w.compIndex.TablesFor(e)` to find entities that hold `e` as a *component*.
- The default OnDelete policy on a component entity is `RemoveAction` (per `@cleanup.go:50-51`), but `deleteImmediate` does **not** actively call Remove on sources — it falls through to `deleteOne(e)` which frees the component-entity slot. Tables retaining `e` in their type signature are left with an orphaned (now-freed) component ID.

**Goal:** when `Delete(compEntity)` is invoked and the OnDelete policy is `RemoveAction`:
- Snapshot the entity list from `w.compIndex.TablesFor(compEntity)` (avoid iterate-while-mutating).
- For each entity: actively call the equivalent of `Remove[compEntity]`, triggering archetype migration to the smaller signature.
- Fire `OnRemove` hook + `EventOnRemove` observers per entity (existing path).
- Then proceed with `deleteOne(compEntity)`.

**Defer interaction:** the active cascade must execute inside a `Write` scope (or be carefully manual about queue ordering) so that the new `EventOnDelete` for `compEntity` fires before the Remove cascade modifies storage. Avoid the iterate-while-mutating trap by snapshotting the table list and entity rows up front.

**Performance:** O(entities-with-component). Document the cost in `docs/ObserversManual.md` and `docs/Relationships.md`.

### Documentation cleanup

`@ROADMAP.md` `Future Work` section currently lists three items this phase resolves and one already-shipped item that was never moved:

- Line 141: `OnTableDelete event` — already shipped in v0.101.0 / Phase 16.46. **Move out.**
- Line 143: `OnDelete / OnDeleteTarget observer events` — **moves to Shipped** under v0.103.0.
- Line 148: `OnDelete component-remove cascade` — **moves to Shipped** under v0.103.0.
- Line 149 (in the Cleanup-policy extensions section): `Observer-driven OnDelete / OnDeleteTarget events` — **moves to Shipped** under v0.103.0.

Bump `## Shipped (through v0.102.0)` heading to `v0.103.0`.

### Required test file — new `@cleanup_observers_test.go`

**OnDelete event:**
- `TestOnDelete_FiresOnDelete` — observer with `EventOnDelete`; `Delete(e)`; verify fires.
- `TestOnDelete_FiresOncePerEntity` — cascade delete of parent with 5 ChildOf children fires `OnDelete` 6 times (parent + 5).
- `TestOnDelete_BeforeRemove` — register `OnDelete` and `OnRemove` for same component; verify `OnDelete` fires first.
- `TestOnDelete_HandlerReadsState` — handler reads `e`'s component values via `*Reader`.
- `TestOnDelete_MultiTermFilter` — observer with `WithQuery(With(Position))`; only Position-bearing entities trigger.
- `TestOnDelete_NoFireOnAlive` — non-deleted entity does not trigger.
- `TestOnDelete_ObserverDisabled` — disabled observer doesn't fire.
- `TestOnDelete_YieldExisting_IsNoOp` — `WithYieldExisting()` doesn't fire for any existing entity (delete events are future-only).

**OnDeleteTarget event:**
- `TestOnDeleteTarget_FiresPerDependent` — parent with 3 ChildOf children; delete parent; fires 3 times.
- `TestOnDeleteTarget_PassesPairRel` — verify `pairRelID` parameter correctly identifies the relationship.
- `TestOnDeleteTarget_FilterByRelationship` — observer filtered to `(IsA, *)` cascades; ChildOf cascades skip.
- `TestOnDeleteTarget_PolicyInteraction_Remove` — Remove policy: fires, then dependent's pair is removed (parent-storage `policyOnDeleteTargetRemove` path at `@world.go:1253-1256`).
- `TestOnDeleteTarget_PolicyInteraction_Delete` — Delete policy: fires, then dependent is deleted (which itself triggers `OnDelete`).
- `TestOnDeleteTarget_PolicyInteraction_Panic` — Panic policy: fires BEFORE the panic so user can log.

**Component-remove cascade:**
- `TestComponentRemove_ActivelyRemoved` — entity has component `C`; `Delete(c)`; entity loses `C` (verify via query/`HasID`).
- `TestComponentRemove_OnRemoveFires` — verify hook fires per entity.
- `TestComponentRemove_OnRemoveObserverFires` — verify `EventOnRemove` observer fires per entity.
- `TestComponentRemove_OnDeleteFires` — verify `OnDelete` fires for the component entity itself after cascade completes.
- `TestComponentRemove_ManyEntities_AllRemoved` — 1000 entities with component `C`; `Delete(c)`; all 1000 archetype-migrate.
- `TestComponentRemove_NoOrphanedSignatures` — no archetype retains the deleted component ID in its signature post-delete.

**Deferred scope integration:**
- `TestOnDelete_FiresInDeferredScope` — `Write(fn)` containing `Delete`; observer fires when `cmd_queue` flushes.
- `TestOnDeleteTarget_OrderedWithOnDelete` — cascade inside defer: `OnDeleteTarget` fires before `OnDelete` for the dependent.

**Multi-event observers:**
- `TestOnDelete_AndOnRemove_SameComponent` — separate observers for `OnDelete` and `OnRemove` fire in correct order.
- `TestOnDelete_PanicInHandler_DoesNotCorruptState` — handler panics; world remains usable for queries afterward.

**Reclamation interaction:**
- `TestOnDelete_BeforeTableReclamation` — entity delete triggers `OnDelete`; later in tick the now-empty table triggers `OnTableDelete` (Phase 16.46); both events fire correctly in order.

## Constraints

- @cleanup.go — Phase 15.0 cleanup-policy bit layout (`cleanupPolicyFlags`, `applyCleanupPolicy`, `policyOnDelete*`). The new observer events sit alongside this policy machinery, not inside it; cascade hooks live in `deleteImmediate`/`deleteOne`.
- @world.go — `deleteImmediate` at lines 1195–1316 is the cascade walker (DFS, post-order, cycle-guarded). `OnDeleteTarget` events fire inside this loop. `deleteOne` at lines 1077–1169 is where `OnDelete` fires (before the existing `OnRemove` hook loop at line 1090). New event entity allocation joins the block at lines 562–752 (current latest built-in `eventOnTableDeleteID` at index 75); the two new entities take indices 76 and 77, shifting user entities to start at index 78.
- @observer.go — observer registration and dispatch machinery; new event entities reuse the existing dispatch path.
- @observer_table.go — pattern reference for built-in event entities and table-event dispatch; the new `OnDelete`/`OnDeleteTarget` registration helpers follow this shape.
- @observer_multi.go — Phase 16.15 multi-term filter; `WithQuery(terms...)` reuses this engine.
- @observer_options.go — `ObserverOptions` surface; this phase may add `WithRelationship(relID)` for the `OnDeleteTarget` filter. Verify whether an existing option already covers the use case before adding new surface.
- @cmd_queue.go — defer queue integration; `Delete` already queues correctly (`@world.go:1182-1193`); the new event dispatch must respect the existing queued-cascade ordering.
- @docs/ObserversManual.md — new section "OnDelete and OnDeleteTarget events" with semantics, `*Reader`-only handler-context restriction, fire ordering, multi-term filter examples.
- @docs/Relationships.md — note the observer-event surface for cleanup policies; cross-link to `ObserversManual.md`.
- @docs/ComponentTraits.md — under "Cleanup traits (OnDelete / OnDeleteTarget)" cross-link to the observer events.
- @ROADMAP.md — Future Work cleanup as described above; bump "Shipped (through vX)" heading to v0.103.0.
- @CHANGELOG.md — new v0.103.0 entry following the v0.101.0 / v0.102.0 format. Note the built-in entity reindex (user entities 76 → 78).
- @README.md — observer feature row mention.

## Non-goals

- New cleanup policies beyond Delete/Remove/Panic — three policies cover all cases.
- Async cleanup — synchronous, in-tick.
- Observer event for `Clear(e)` (which doesn't delete) — separate from delete events; no new event entity for this.
- Component-remove cascade for partial signatures (e.g. "remove this component only from entities matching some filter") — full cascade only.
- Handler-side mutation during the dispatch window — `*Reader` is the contract; users defer via `World.Write(fn)`.

## Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run ./...` clean.
- `go test ./... -race -count=3` clean.
- Coverage ≥ 95% (current baseline).
- All existing cleanup-policy tests (Phase 15.0, `@cleanup_policies_test.go`) pass unchanged.
- All observer tests (Phases 16.0–16.47) pass unchanged.

## Notes

- Target version: **v0.103.0**.
- Phase number: **16.48**.
- Both features share the same dispatch infrastructure — they ship together for coherence.
- Built-in entity reindex: two new event entities at indices 76–77; user entities shift 76 → 78. This is the second shift in three phases (Phase 16.46 already shifted 75 → 76); call this out in CHANGELOG migration notes.
- The `*Reader` vs `*Writer` decision for the handler context is settled: `*Reader`, documented as a hard constraint with a `World.Write(fn)` escape hatch for mutation.
- Feature 2 verified against current source: `deleteImmediate` does not iterate `w.compIndex.TablesFor(e)`, so component-entity delete today leaves orphaned signatures on tables. Active cascade is genuinely new work.
