## Goal

Port upstream's observer-propagation mechanism: when a component is mutated on a prefab base, observers fire **once per inheritor** in addition to firing for the base itself. Today, observers do NOT propagate along `IsA` edges in go-flecs — if `OnSet(Position)` is registered and someone sets `Position` on a prefab, the observer fires for the prefab only, even though every instance that inherits `Position` just saw its user-visible state change.

Closes the gap entry at `docs/README.md:159`:
> *Observer propagation / forwarding — events propagate along relationship edges (e.g., `OnSet(Position)` on a parent notifies children inheriting `Position`). not yet ported in Go flecs.*

After this phase, line 159 flips to ✅ shipped (v0.72.0). Target version: **v0.72.0** (next after v0.71.0 entity ID ranges, just shipped at `000312d`).

### Behaviour summary

For each component mutation (OnAdd / OnSet / OnRemove / OnReplace / custom Emit) on a `source` entity:

1. Local fire as today.
2. If `source` is referenced as the target of any `(IsA, source)` pair, fire the **same event** for every direct inheritor — passing the **inheritor** as `e` to the callback, not `source`.
3. Recurse: an inheritor that is itself a base for further inheritors propagates onward.
4. Skip a given inheritor if either (a) `IsDontInherit(componentID)` is true, or (b) the inheritor has its own copy of the component (override — local table already contains the column).
5. Fire order: source first, then BFS over the inheritor tree.

Recommended scope decisions (locked in unless review surfaces a reason to change):

- Traversal relationship: **IsA only** for v1. Upstream supports propagating along ChildOf and any Traversable relationship; we limit scope.
- Direction: **downward only** (target → inheritors). No upward propagation.
- Observer opt-out: **none** in v1. Propagation is a property of the inheritance, not the observer.
- De-duplication for diamond inheritance (`A IsA B`, `A IsA C`, both inherit Position via different bases): **fire once per path**. Users wanting unique fires must filter in their handler.
- Pair components: `(R, T)` on prefab propagates to inheritors as `(R, T)` (no target rewriting).

## Constraints

### Upstream C reference (verified line numbers)

- `@SanderMertens/flecs/src/observable.c:504-544` — `flecs_propagate_entities`. Called from the post-fire path. Walks `(EcsWildcard, entity)` to discover entities that reference `source` as a target of a traversable relationship.
- `@SanderMertens/flecs/src/observable.c:1508-1513` — fire-site integration: after `flecs_observers_invoke` for the local table, if `has_observed` (table flag `EcsTableHasTraversable`), call `flecs_propagate_entities`.
- `@SanderMertens/flecs/src/observable.c:383-420` — `flecs_emit_propagate` walks the traversable component-record chain (`flecs_component_trav_next`); the `propagate_trav` filter at line 409-413 allows continuation iff the current relationship matches `propagate_trav` OR is `EcsIsA` (IsA always propagates).
- `@SanderMertens/flecs/src/observable.c:322-380` — `flecs_emit_propagate_id` iterates each table that has the `(IsA, base)` pair, then per-entity in each table.
- `@SanderMertens/flecs/src/observable.c:1083-1086` — **override interaction**: *"If entities already have the component, don't propagate"* — check via `flecs_component_get_table(rc_cr, it->table)`. If the inheritor's own table contains the component, skip — its local value masks the inherited one.
- `@SanderMertens/flecs/src/observer.c:1010-1012` and `@SanderMertens/flecs/src/observable.c:1543-1547` — **DontInherit interaction**: `EcsIdOnInstantiateDontInherit` flag suppresses inheritance machinery, including the override-on-add forwarding. Observer-side, components with this flag are not subscribed via the `up`/`self_up` path.
- Recursion depth: implicit in upstream via the `flecs_emit_propagate` → `flecs_emit_propagate_id` → `flecs_emit_propagate_id_for_range` → recursive walk of `traversable_count` entities (observable.c:308-318). Multi-level chains (A IsA B IsA C) propagate naturally.
- Performance: upstream uses a per-component-record `reachable` cache (observable.c:405, `cur->pair->reachable.generation`), invalidated on (IsA, *) structural changes (observable.c:423-501, `flecs_emit_propagate_invalidate_tables`). Go port can adopt a simpler per-entity inheritor set keyed on `(IsA, *)` adds/removes.

### Go-side files to extend

- `@isa.go` — IsA inheritance machinery, `getViaIsA`/`hasViaIsA`. Provides the precedent for walking IsA chains downward. Note the `DontInherit` early-out at `isa.go:73-76` and `isa.go:118-121` — mirror the same gate in propagation.
- `@hooks.go:152-179` — fire sites `fireOnAdd` / `fireOnSet` / `fireOnRemove`. These each call `w.dispatchObservers(...)` after the local hook. Propagation hook attaches **after** the local `dispatchObservers` call. `fireOnReplace` (line 141) currently has no observer dispatch (no `OnReplace` event in C) — but propagation still matters for any future replace-event observer; design must not preclude wiring it later.
- `@observer.go:400-435` — `dispatchObservers`. This is the natural place to extend if propagation lives below the fire-site call; alternatively the propagation engine sits between `fireOn*` and `dispatchObservers`. Decide during implementation.
- `@observer_multi.go` — multi-term observer dispatch. Filter terms are evaluated per-fire via `entityMatchesTerms` at `observer.go:420,430`. Propagation must invoke `entityMatchesTerms` with the **inheritor** entity, not the source, so filter terms can correctly exclude inheritors that don't match.
- `@observer_custom.go:50-59` — `Emit` dispatches custom events. Propagation should compose with custom events: an `Emit` aimed at a prefab should optionally propagate to inheritors. Mirror upstream's behaviour (custom events also flow through `flecs_emit` which contains the propagate-entities call).
- `@instantiate_policies.go:17-20,50` — `policyOnInstantiateDontInherit` flag and the `IsDontInherit`-equivalent check. There is no `dont_inherit.go` or `override.go` file in the Go port — the DontInherit and Override flags both live here as `policyOnInstantiateDontInherit` / `policyOnInstantiateOverride`. Use the `policyOnInstantiateDontInherit` bit-test (the same pattern as `isa.go:74,119`) as the propagation gate.
- `@traversable.go` — `IsTraversable` / `applyTraversablePolicy`. IsA is bootstrapped Traversable at `@world.go:520`. Phase locks propagation to IsA only, so we do not need to consult the traversable map at fire time — but document that future phases may generalise via this map.
- `@observer.go:9-46` — `EventKind` enum and `eventKindToEntity`. Propagation reuses the same event entities; no new event kinds.

### Project conventions (the patterns to follow)

- `@CONTRIBUTING.md` — doc-update checklist; coverage ≥ 95.0%; the agreed phase-completion checklist.
- `@CHANGELOG.md:1-30` — version bumps land at the top with a structured Added / Changed / Implementation notes layout. Follow the v0.71.0 entry as the template.
- `@docs/ObserversManual.md` — existing observer documentation. Add a new top-level `## Propagation along IsA` section after `## Custom Events` and before `## Deferred Execution`; cross-link to `docs/PrefabsManual.md`.
- Phase 16.15 multi-term observers (`@observer_multi.go`) — dispatch path now evaluates a query per fire. Propagation is orthogonal: happens at the fire site, regardless of the observer's term count. The multi-term filter must run with the **inheritor** as the candidate.
- Phase 16.10 monitor observers (`@monitor_observer.go`) — NOT a precedent. Monitors track query-match entry/exit and the inheritor's match state is determined by its own table signature plus inherited components — but the **transition event** is owned by the monitor's normal entry/exit machinery, not propagation. Document explicitly that monitors do not receive propagated fires.

## Deliverables (mechanical)

1. **`observer_propagation.go`** (new file):
   - `propagateEvent(w *World, eventEntity, componentID, sourceEntity ID, ptr unsafe.Pointer)` — at the fire site, walk inheritors of `sourceEntity` and emit the event for each one, subject to the DontInherit + Override gates.
   - Cache the inheritor set per prefab (`map[ID][]ID` keyed by prefab entity), invalidated whenever an `(IsA, prefab)` pair is added or removed anywhere. Lazy build on first read.
   - Recursion: flatten the multi-level inheritor tree on first cache miss; cache the BFS-ordered slice for fast subsequent fires.

2. **Fire-site integration**: in `fireOnAdd`, `fireOnSet`, `fireOnRemove`, `fireOnReplace`, and the custom-event `Emit` path — after the local `dispatchObservers` call, invoke `propagateEvent` if:
   - The component is not `DontInherit` (`w.instantiatePolicies[componentID] & policyOnInstantiateDontInherit == 0`); AND
   - The source entity has at least one inheritor (cache lookup returns non-empty).

3. **Per-inheritor dispatch**: each propagated event calls `dispatchObservers` with the inheritor as `e`. Multi-term observer filter terms (`@observer.go:420,430`) evaluate against the inheritor — an inheritor that doesn't match the filter does not fire.

4. **Override gate**: for each inheritor, check if its archetype has the component locally (`rec.Table.HasComponent(componentID)`). If yes, **skip** — its local value masks the inherited one (mirrors upstream observable.c:1083-1086).

5. **DontInherit gate**: short-circuit at the propagation entry. Same pattern as `isa.go:73-76`.

6. **Cache invalidation hooks**: wherever `(IsA, *)` is added/removed on any entity, invalidate the corresponding prefab's cached inheritor slice. Two hook points:
   - `cmd.go` / commit path when an entity transitions to/from a table containing an `(IsA, prefab)` pair.
   - Entity deletion path: when an inheritor or a prefab is deleted, evict from caches.

7. **Tests in `observer_propagation_test.go`** (at least 12 cases):
   - Single inheritor: register `OnSet(Position)`; create prefab P with Position; create instance I with `(IsA, P)`; `Set(P, Position)` fires for both P and I in that order.
   - Multiple inheritors: P has 5 instances; `Set(P, Position)` fires the observer 6 times (P + 5 instances).
   - Recursive chain: A IsA B IsA P. `Set(P, Position)` fires for P, then B, then A (BFS).
   - Diamond: `A IsA B`, `A IsA C`, both B and C have Position. `Set(B, Position)` fires for B and A; `Set(C, Position)` fires for C and A. (No de-dup across separate sets — but a single set on B fires A only once.)
   - DontInherit blocks: mark Position as DontInherit; `Set(P, Position)` fires only for P, not inheritors.
   - Override blocks: instance I has its own Position (local table column); `Set(P, Position)` fires for P and other inheritors but NOT I.
   - OnAdd propagation: `AddID(P, somePair)` — observers see the add on each inheritor (treat as a newly-reachable component).
   - OnRemove propagation: `Remove(P, Position)` — observers see the remove on each inheritor.
   - OnReplace propagation: `Set(P, Position{v1})`, then `Set(P, Position{v2})` — `OnReplace` observers see old+new on P and each inheritor.
   - Pair components: `SetPair((R, T))` on P propagates to inheritors with the same `(R, T)` pair.
   - Multi-term observer (Phase 16.15) with propagation: filter terms evaluate per-inheritor; an inheritor that doesn't match the filter doesn't fire.
   - Disabled observer: no fire on source, no propagation overhead (early-out in `dispatchObservers`).
   - Performance: 1000 inheritors of one prefab; `Set(P, Position)` completes in linear time, not quadratic. Assert wall-clock or syscall-count bound.
   - Marshal round-trip with active observers — observers don't serialize, but post-restore propagation behaviour is consistent.
   - Coverage ≥ 95.0%.

8. **Doc updates** per CONTRIBUTING.md:
   - `docs/ObserversManual.md` — new section `## Propagation along IsA` covering the recursive BFS model, override + DontInherit interactions, dispatch ordering, and the explicit non-goal of propagation-along-ChildOf.
   - `docs/PrefabsManual.md` — cross-link to the observer-propagation section near the existing IsA / override discussion.
   - `docs/README.md` line 159 — flip from gap entry to ✅ shipped (v0.72.0).
   - `README.md` feature table around line 220-230 — add propagation row.
   - `CHANGELOG.md` — v0.72.0 entry at top.
   - `ROADMAP.md:3` — heading bump to "through v0.72.0".

## Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%
- No regression on existing observer tests (`observer_test.go`, `observer_multi_test.go`, `observer_custom_events_test.go`, `monitor_observer_test.go`, `observer_lifecycle_test.go`, `observer_fixed_source_test.go`, `observer_table_test.go`).

## Explicit non-goals

- No propagation along non-IsA relationships in this phase (no ChildOf, no arbitrary Traversable).
- No upward propagation (child → parent).
- No observer-by-observer opt-out of propagation. Future phase may add a `WithoutPropagation()` option.
- No event de-duplication when an inheritor is reached via multiple bases in a single fire. Recommend: fire once per path; users wanting unique fires filter in handler. Documented.
- No new event kinds — propagation reuses existing `OnAdd` / `OnSet` / `OnRemove` / `OnReplace` / custom event entities.

## Open decision points (recommended defaults, lock in during route)

1. **Traversal relationship**: hard-coded IsA only. Lock in IsA-only for v1.
2. **Inheritor cache invalidation strategy**: lazy invalidate on `(IsA, *)` add/remove anywhere. Recommend lazy for perf.
3. **Fire order**: source first, then inheritors in BFS order. Recommended for predictability.
4. **Pair components**: propagate `(R, T)` unchanged on the inheritor; do not rewrite the target.
5. **Diamond inheritance**: fire once per inheritor-path. Do not de-dup at the engine level.

## Notes on the prompt's `@`-references

The user prompt referenced `@dont_inherit.go` (Phase 15.1) and `@override.go` as separate files. These files do **not exist** in the Go port: both DontInherit and Override are policies in `instantiate_policies.go` (the `policyOnInstantiateDontInherit` and `policyOnInstantiateOverride` flags). The bit-test pattern at `isa.go:73-76,118-121` is the canonical gate to mirror. Adjusted in the deliverables section accordingly.

The prompt also said the gap entry is at `docs/README.md:160` — the actual line is **159** as of `000312d`.
