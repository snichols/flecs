## Goal

Ship the **observer-side mirror** of Phase 16.3 (system lifecycle) as a single v0.60.0 bundle. Two small, related features both live on the observer-registration / observer-fire plumbing in `observer.go`:

1. **`yield_existing`** — on registration, retroactively fire the observer for every entity that already matches its component term. This is the canonical "catch up to existing state" mechanism in upstream C flecs; without it, an observer registered after entities are populated misses everything. Closes `docs/README.md` line 156.
2. **Observer disabling** — pause an observer (skip its callback in the fire path) without unsubscribing. Symmetric to system disabling shipped in Phase 16.3. Closes `docs/README.md` line 159.

Both flip to ✅ shipped at v0.60.0. Same phase because they touch the same observer struct and registration entry points.

### C upstream verification

- **`yield_existing` desc field**: `include/flecs.h:1389` — `bool yield_existing;` on `ecs_observer_desc_t`. Doc reads: "When an observer is created, generate events from existing data. For example, EcsOnAdd Position would match all existing instances of Position."
- **`yield_existing` implementation**: `src/observer.c:761` `flecs_observer_yield_existing()` walks `ecs_each_id(world, register_id)` (for uni observers) and invokes the observer's callback per match. Triggered at `src/observer.c:1270-1272` after observer construction. The dispatch uses `it.callback = flecs_default_uni_observer_run_callback`, so the sweep targets only the newly-registered observer — other observers subscribed to the same event are NOT re-fired.
- **Observer fire-path Disabled gate**: `src/observer.c:342` checks `impl->flags & (EcsObserverIsDisabled|EcsObserverIsParentDisabled)` and returns early. The bit is flipped by `flecs_observer_set_disable_bit()` at `src/observer.c:1491` from the same `EcsDisabled`-trigger plumbing that systems use (`src/bootstrap.c:542`). Same `EcsDisabled` tag is reused across observers and systems upstream.
- **yield_existing event kind**: upstream emits the observer's actual subscribed event (`it.event = o->events[i]`), filtered to OnAdd/OnSet on registration and OnRemove only on deletion (`src/observer.c:779-788`). For this Go phase only the OnAdd/OnSet path applies — OnRemove yield on delete is out of scope.

### Go-side precedents

- @system.go — Phase 16.3 design verified: `enabled bool` (plain field, not `atomic.Bool`), initialised `true` in `NewSystem`/`NewSystemInPhase`, methods `SetEnabled(bool)` and `IsEnabled() bool`, no free-function aliases. The pipeline `Progress` runner checks `s.enabled` in the active-set filter loop. Mirror this exactly.
- @observer.go — current state: `Observer` struct (line 52), `Observe[T]` / `ObserveID` / `Observe2[T]` registration entry points, `addObserverNode` (line 158), `dispatchObservers` (line 183) is the single fire-path gate point and the natural place for the `enabled` check.
- @hooks.go — `fireOnAdd` / `fireOnSet` / `fireOnRemove` (lines 152-179) are the only callers of `dispatchObservers`. Hooks fire before observers; ensure the new gate sits inside `dispatchObservers`, not earlier (hooks must remain unaffected).
- @scope.go — `Reader.TablesFor(id ID) []*table.Table` (line 59) returns archetype tables matching a single component ID. This is exactly what the yield_existing sweep needs to iterate entities by component.
- @query_sort.go — option-shape precedent from Phase 16.4: `CachedQueryOptions` struct + `WithOrderBy(...)` constructor consumed by `NewCachedQueryFromTermsWithOptions`. Follow this idiom for observers: introduce `ObserverOptions` + `WithYieldExisting()` + `ObserveWithOptions[T]` (or extend existing entry points — see open decision 5).

### Scope and event kind

- yield_existing is supported only on **OnAdd** and **OnSet** events (matches upstream's create-time sweep). On registration with `WithYieldExisting()`:
  - For OnAdd: walk tables containing the component ID; for each entity, fire the observer's callback with the current slot pointer. The synthesized event is OnAdd.
  - For OnSet: same sweep; the event kind delivered to the callback is OnSet.
  - For OnRemove: registering with `WithYieldExisting()` panics with a clear message (out of scope; upstream only yields OnRemove on observer-deletion).
- yield_existing on `Observe2[T]` registering multiple events: yield once per (event ∈ {OnAdd, OnSet}) in the event list. Skip OnRemove silently in the sweep.

### Locked-in open decisions

1. **Sweep scope** — sweep targets ONLY the newly-registered observer. Use the stored `callback` directly; do NOT route through `dispatchObservers` (which would re-fire all peer observers on that event).
2. **Disabled / Prefab entities** — sweep walks `TablesFor(id)` filtering out tables whose `HasComponent(disabledID)` or `HasComponent(prefabID)` is true. Mirrors ordinary query-exclusion semantics from Phase 16.2.
3. **Observer disabled at registration** — if the observer's `enabled` is false at the time `WithYieldExisting()` would sweep, the sweep is suppressed. (Currently the observer is created enabled by default, so this is only reachable via post-construction toggle before registration completes; doc the edge for completeness.)
4. **Sweep order** — archetype-table order (registration order of tables in `compIndex`). Document this matches normal query iteration.
5. **API shape** — Phase 16.4 precedent: introduce `ObserverOptions` + `WithYieldExisting() ObserverOptions` + `ObserveWithOptions[T](w, opts, events, fn)`. Existing `Observe[T]` / `ObserveID` / `Observe2[T]` remain unchanged. Mirrors `NewCachedQueryFromTermsWithOptions` introduction style.
6. **Free-function aliases for disable** — Phase 16.3 chose methods-only (`SetEnabled` / `IsEnabled`); no `DisableSystem` / `EnableSystem` free functions. Mirror exactly: observer gets methods only.
7. **Atomic vs plain bool** — Phase 16.3 uses plain `bool` (not `atomic.Bool`). Observer fire-path is gated by world-level exclusive-access; concurrent toggle from a goroutine outside an active Write would violate the world's exclusive-access invariant anyway. Use plain `bool`, matching system precedent. The bundle task description suggested `atomic.Bool`; supersede that with the established Phase 16.3 pattern.

## Deliverables

### Observer disabling

1. Add `enabled bool` field to `Observer` struct (`observer.go:52`); initialise true in `Observe[T]` / `ObserveID` / `Observe2[T]` / `ObserveWithOptions[T]`.
2. Methods on `*Observer`: `SetEnabled(v bool)` and `IsEnabled() bool`. Plain assignment / read, no atomic. Note in godoc that flipping is intended for serial use outside an active dispatch.
3. Fire-path gate in `dispatchObservers` (`observer.go:183`): for each node, skip if its owning `Observer.enabled` is false. Requires walking back from `*observerNode` to its `*Observer`; either add a back-pointer field on `observerNode` or thread the enabled flag onto each node at registration time. The back-pointer is cleaner and one word per node.
4. Hooks (in `hooks.go`) are unaffected — only observer dispatch is gated.

### yield_existing

1. Add `ObserverOptions` struct + `WithYieldExisting() ObserverOptions` constructor in a new file `observer_options.go` (or append to `observer.go`).
2. Add `ObserveWithOptions[T any](w *World, opts ObserverOptions, events []EventKind, fn func(fw *Writer, event EventKind, e ID, v T)) *Observer` — the generic, multi-event form (mirrors `Observe2[T]` plus options).
3. After the observer is registered and BEFORE `ObserveWithOptions` returns, if `opts.yieldExisting` is true:
   - For each subscribed event in `{OnAdd, OnSet}`:
     - Walk `w.compIndex.TablesFor(id)`.
     - Skip tables with the Disabled or Prefab tag (mirrors Phase 16.2 default exclusion).
     - For each entity in each table, dereference the component slot and invoke the observer's `callback` directly (NOT through `dispatchObservers`). Synthesize the event as the subscribed event kind.
   - Skip OnRemove entries silently. Panic at construction time if the only events are OnRemove and `yieldExisting` is true (matches upstream's invariant that yield only emits create-time events on registration).
4. Sweep runs synchronously inside the registration call. Document that registration blocks until the sweep completes.
5. Sweep respects `IsEnabled()` (set false before construction returns is reachable only via the goroutine pattern; doc the edge but no special-case beyond reading the flag).

## Tests in `observer_lifecycle_test.go` (≥ 10 cases)

Observer disabling:
1. Register OnSet[Position] observer; `Set`; handler invoked.
2. Same observer; `SetEnabled(false)`; `Set` again; handler NOT invoked.
3. `SetEnabled(true)`; `Set`; handler invoked again.
4. `IsEnabled()` round-trip across SetEnabled(false), SetEnabled(true), default-true-on-create.
5. Two observers on the same event; disable one; the other still fires.
6. Disable mid-dispatch (observer A's callback toggles observer B's enabled flag) — covered by registration-order-dispatch semantics; the same-dispatch already-iterated semantics match Unsubscribe.

yield_existing:
7. Create 100 entities with Position (some via `Set`, some via `AddID` so OnAdd-only entries exist); register OnAdd[Position] with `WithYieldExisting()`; verify handler fires exactly 100 times during registration; verify the visited entity IDs match.
8. Register OnAdd[Position] WITHOUT yield_existing on a populated world; handler does NOT fire for existing entities; subsequent `Set` on a new entity DOES fire.
9. yield_existing with OnSet event: verify the sweep delivers OnSet to the callback (not OnAdd).
10. yield_existing on a single observer does NOT cause peer observers subscribed to the same event to re-fire (validates "sweep targets only the newly-registered observer").
11. yield_existing skips entities carrying the Disabled tag (use `DisableEntity` on a subset; verify they are NOT yielded).
12. yield_existing skips entities carrying the Prefab tag (use `MarkPrefab` on a subset; verify they are NOT yielded).
13. yield_existing with `Observe2[T]`-style multi-event registration (OnAdd + OnSet via options form): verify each event kind fires once per existing entity (so 2 × N total invocations).
14. yield_existing with only OnRemove in the events list panics at construction with a clear message.
15. Sweep is synchronous: a `WithYieldExisting()` registration call returns only after all entities are visited (test by counting invocations before return).
16. Coverage ≥ 95.0%.

## Documentation updates

- `docs/ObserversManual.md` — two new sections: "Observer disabling" (mirror Systems.md disabling) and "yield_existing" (with code example showing `ObserveWithOptions` + `WithYieldExisting()`). Cross-link both ways with `docs/Systems.md#disabling-a-system`.
- `docs/README.md` — flip lines 156 and 159 to ✅ shipped (v0.60.0) with anchor links to the new sections.
- `README.md` — feature list bump if observer disabling / yield_existing appears in headline examples.
- `CHANGELOG.md` — v0.60.0 entry at top: title "Phase 16.5: Observer lifecycle bundle (yield_existing + observer disabling)"; describe API additions; cite closed gap line numbers; reference C upstream line numbers verified above.
- `ROADMAP.md` — bump "Shipped (through v0.59.0)" → "through v0.60.0"; add Phase 16.5 entry; renumber Phase 16.6+ candidates accordingly (the current "Phase 16.9 Observer disabling" and "Phase 16.6 Yield-on-create" lines become this single shipped bundle; renumber remaining candidates).

## Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run` clean.
- `go test ./... -race -count=3` passes.
- Root-package coverage ≥ 95.0%; no regression on existing observer tests.

## Explicit non-goals

- Multi-term observers (`docs/README.md` line 155) — separate phase.
- Observer propagation along IsA edges (line 157) — separate phase.
- Monitor observers (line 158) — separate phase.
- Custom events (line 154) — separate phase.
- OnDelete / OnDeleteTarget observer events (line 152) — separate phase.
- Fixed-source observer terms (line 160) — separate phase.
- OnRemove yield (sweep on observer-deletion) — out of scope; only the create-time OnAdd/OnSet sweep ships.

## Constraints

- @observer.go — current observer struct, registration entry points (`Observe[T]`, `ObserveID`, `Observe2[T]`), `addObserverNode`, and `dispatchObservers` are the modification surface. The enabled gate lives in `dispatchObservers`.
- @system.go — Phase 16.3 precedent: plain `bool enabled` field, methods `SetEnabled`/`IsEnabled` (no atomic, no free-function aliases). Mirror exactly.
- @hooks.go — hook fire-helpers (`fireOnAdd`/`fireOnSet`/`fireOnRemove`) call `dispatchObservers`; gate must not affect hooks themselves.
- @scope.go — `Reader.TablesFor(id)` returns the archetype tables that contain a component ID; this is the sweep walker for yield_existing.
- @query_sort.go — option-shape precedent: `CachedQueryOptions` + `WithOrderBy` consumed by `NewCachedQueryFromTermsWithOptions`. Mirror with `ObserverOptions` + `WithYieldExisting` consumed by `ObserveWithOptions[T]`.
- @query_filters.go — `Disabled()` / `Prefab()` table predicates govern the sweep's entity exclusion (the same default-exclusion semantics from Phase 16.2 apply).
- @CONTRIBUTING.md — coverage ≥ 90% (target ≥ 95% per this phase); `gofmt` + `golangci-lint`; docs land with code; every change updates `docs/`, `CHANGELOG.md`, and `ROADMAP.md` together.
- @docs/README.md line 156 (yield_existing gap) and line 159 (observer disabling gap) — both must flip to ✅ shipped (v0.60.0) with anchor links to the new `docs/ObserversManual.md` sections.
- C upstream verified: `include/flecs.h:1389` (yield_existing field), `src/observer.c:761` (sweep impl), `src/observer.c:1270-1272` (sweep trigger), `src/observer.c:342` (Disabled fire-path gate), `src/observer.c:1491` (disable-bit flipper), `src/bootstrap.c:542` (same `EcsDisabled` reused for observers and systems).
