## Goal

Port upstream C flecs multi-term observers to Go-flecs. Today, observer registration is single-component: `Observe[T](w, event, fn)` fires whenever component `T` is added/set/removed on any entity. The handler cannot say "only fire when entity ALSO has `Velocity` AND NOT `Frozen`" without paying full dispatch cost on every irrelevant event and then filtering inside the callback.

**Multi-term observers** let the user attach a multi-term query at registration. The first term is the *trigger* (the dispatch key); the remaining terms are *filters* evaluated against the affected entity at fire time. Short-circuit on first-failing filter term — cheap path for the common "doesn't match" case.

After this phase: Go-flecs observer expressiveness is at parity with upstream's `ecs_observer_desc_t.query` (multi-term query) field.

**Distinction from monitor observers (Phase 16.10):**
- Monitor: fires on *transition* into/out of a multi-term match. Entry/exit semantics.
- Multi-term observer (this phase): fires on *every* subscribed event (OnAdd/OnSet/OnRemove) of the trigger term, gated by the filter terms. No transition tracking.

Both share the term-evaluation machinery in `monitor_observer.go` and `query.go`.

### Target version
v0.70.0 (next after v0.69.0 prefab hierarchies + slots — Phase 16.14, commit `07f7047`).

### Upstream verification (cited line numbers from `/work/agents/claude/projects/SanderMertens/flecs`)

1. `ecs_observer_desc_t.query` is a full multi-term query, not a single-component subscription.
   - `include/flecs.h:1374-1429` — `ecs_observer_desc_t` struct definition.
   - Line 1382: `ecs_query_desc_t query;` /* Query for observer. */
2. Multi-event subscription is *one observer, multiple event kinds* (not one observer per event).
   - `include/flecs.h:1385`: `ecs_entity_t events[FLECS_EVENT_DESC_MAX];` /* Events to observe (OnAdd, OnRemove, OnSet). */
   - `include/flecs.h:317-321`: `FLECS_EVENT_DESC_MAX 8` by default.
3. Dispatch path — `src/observer.c`:
   - `flecs_multi_observer_invoke` at line 562. Reads `it->term_index` (the pivot term) at line 579, then `flecs_observer_query_has_range` (line 365 / called at 620) evaluates the full query against the affected table range. Short-circuit semantics live inside that helper.
   - The pivot term is selected at observer construction: each non-filter term in `query->terms` becomes a *child observer* registered under that term's component ID. See loop at `observer.c:960` and the assignment `child_desc.term_index_ = query->terms[i].field_index;` at line 966; persisted via `impl->term_index = desc->term_index_;` at line 1196 and `term->field_index = flecs_ito(int8_t, desc->term_index_);` at line 1260.
   - So upstream's dispatch model is: register N child observers under N trigger keys; at fire time evaluate the full multi-term query with the trigger as pivot. This avoids quadratic blow-up: per-component-event events only walk observers registered under that component.
4. Monitor flag composes with multi-term:
   - `observer.c:1221`: `impl->flags |= EcsObserverIsMonitor;`
   - `observer.c:627`: monitor branch inside `flecs_multi_observer_invoke` — confirms multi-term and monitor share dispatch.
5. Event-kind subscription policy: ONE observer subscribes to MULTIPLE event kinds (e.g. OnAdd + OnSet in the same observer). Verified at `flecs.h:1385`.

### Go-side state (file references via `@`-syntax)

- @observer.go — current `Observe[T]` / `ObserveID` / `Observe2[T]` / `ObserveIDWithOptions`; `observerBucket` with `anyEntity` + `fixedSource` (Phase 16.12 dispatch table); `dispatchObservers` at line 392; `addObserverNode` at line 350.
- @observer_options.go — `ObserverOptions` struct; `WithSource(e)` / `WithYieldExisting()` / `AndSource(e)`; `ObserveWithOptions[T]` typed entry point at line 91.
- @observer_custom.go — `RegisterEvent` / `Emit` / `ObserveEvent` / `ObserveEventTyped` / `ObserveEventWithOptions`. Custom events route through the same dispatch table as built-ins (Phase 16.8 refactor).
- @observer_table.go — `OnTableCreate` / `OnTableCreateWithOptions` (Phase 16.7).
- @monitor_observer.go — **most-similar precedent.** Already takes a `[]Term` query and dispatches by query match. Reuses `validateAndSortTerms` (in `query.go:1310`) and `computeQuerySkipFlags` (`query.go:1284`). The per-entity term evaluator is `entityMatchesMonitorExcluding` at `monitor_observer.go:266` — multi-term observers need the same primitive but without the "excludeID" complication (events fire AFTER the storage change, like sparse monitors). The table-level primitive is `monitorMatchesTable` at line 183.
- @query.go — `Term` struct at line 101; `TermKind` at line 62 (TermAnd / TermNot / TermOptional / TermOr); `matchesTable` at line 874; `validateAndSortTerms` at line 1310; `computeQuerySkipFlags` at line 1284.
- `observer_fixed_source_test.go` — Phase 16.12 tests. (Note: there is **no** standalone `observer_fixed_source.go` file. The fixed-source dispatch lives inline in `observer.go`'s `observerBucket.fixedSource` map and the dispatch branch at `observer.go:407`. This phase should follow the same pattern: extend in-place, not a new file — unless the new surface area justifies a `observer_multi.go` companion.)

### Gap entry to close

`docs/README.md:157` (NOT line 158 as initial spec said — line 158 is yield-on-create which is already shipped):

> *Term-set observer filters (multi-term observers) — C observers can match a query with multiple terms (e.g., "fire when Position is set but only if entity also has Velocity"). Go flecs observers subscribe to a single component at a time. not yet ported in Go flecs.*

Also see `ROADMAP.md:110` — "Multi-term observers" is listed as a Phase 16.9 candidate (now reassigned to Phase 16.15).

After this phase: line 157 flips to ✅ shipped (v0.70.0). ROADMAP entry moves from Observer-system gaps section to Shipped section with header bump to "through v0.70.0".

## Constraints

- @docs/README.md — gap entry at line 157 must flip to ✅ shipped (v0.70.0); add accurate one-line entry referencing the new API surface.
- @docs/ObserversManual.md — add § Multi-term observers section with examples; trigger-vs-filter distinction documented clearly; update the single-component `Observe[T]` section to note it is a special case of multi-term (no filter terms).
- @ROADMAP.md — heading bump to "Shipped (through v0.70.0)"; new entry in Shipped section; remove from Observer-system gaps.
- @CHANGELOG.md — v0.70.0 entry at top in the existing format (date 2026-05-14 placeholder; iterate agent will set actual ship date).
- @README.md — feature list bump if multi-term observers warrant a callout in the headline list.
- @CONTRIBUTING.md — `go test ./... -race` + `golangci-lint run` required; coverage on root `flecs` package must stay ≥ 90% (current ~97%; spec requires ≥ 95.0% for this phase).
- @monitor_observer.go — primary code reuse target. The `entityMatchesMonitorExcluding` term-evaluator is bound to `*monitorObserver`. Either (a) generalize it to operate on `([]Term, [][]ID, skipDisabled, skipPrefab)` directly so multi-term observers can call it without an adapter, or (b) write a parallel `entityMatchesMultiTermObserver` and keep the monitor version. Option (a) is cleaner but touches a hot path; option (b) is safer. Document the choice in the phase work.
- @observer.go — dispatch table extension. The current `observerBucket` keys on `(componentID, eventEntity)` and stores any-entity + fixed-source nodes. Multi-term observers register child nodes under the trigger term's component, in the existing dispatch table. The per-node state must grow to include the filter-term list (plus orGroups, skipDisabled, skipPrefab) so dispatch can evaluate filters before firing.
- @query.go — `validateAndSortTerms` (line 1310) and `computeQuerySkipFlags` (line 1284) are the canonical validation entry points; reuse, do not duplicate.
- @observer_options.go — `ObserverOptions` is the canonical option struct; extend if needed (likely no new fields — yieldExisting and source already cover the documented compositions).
- @observer_custom.go — custom events compose: the trigger event may be a custom event entity, the filter terms apply at fire time exactly as for built-in events.
- C flecs upstream observer.c (lines 562-691, 950-1010, 1196, 1260) — dispatch / pivot-term semantics; align Go behavior to upstream where it does not conflict with the Go API style.

## Deliverables

1. **New registration API** — either extend `observer.go` or add a small `observer_multi.go` (pick to match existing convention; Phase 16.12 stayed inline, so default to inline unless surface area is ≥ 3 new exported functions):
   - `ObserveQuery(w *World, event EventKind, terms []Term, fn func(fw *Writer, e ID, ptr unsafe.Pointer)) *Observer`
     - The FIRST term (must be TermAnd) is the trigger; subsequent terms are filters.
     - Panics if `len(terms) == 0` or if the first term is not TermAnd.
   - `ObserveQueryEvents(w *World, events []EventKind, terms []Term, fn func(fw *Writer, event EventKind, e ID, ptr unsafe.Pointer)) *Observer`
     - Multi-event subscription: registers one child observer per event, all sharing the same filter-term snapshot.
   - `ObserveQueryWithOptions(w *World, opts ObserverOptions, events []EventKind, terms []Term, fn func(fw *Writer, event EventKind, e ID, ptr unsafe.Pointer)) *Observer`
     - Composes with `WithYieldExisting()` and `WithSource(e)` per the rules in section 5 below.
   - `ObserveQueryID` variant — explicit trigger ID for raw-ID / pair-ID triggers without a Go type.
   - Decision: NO typed `ObserveQuery[T]` generic variant in this phase. Typed convenience can be added later if there is demand; the goal is to ship the multi-term machinery first.
2. **Dispatch path extension**:
   - Reuse the existing `(componentID, eventEntity)` dispatch table. Multi-term observers register their child nodes under the trigger term's component ID + each subscribed event entity, exactly as single-component observers do.
   - Extend `observerNode` (or add a parallel `observerMultiNode`) with the filter-term snapshot: `filterTerms []Term`, `filterOrGroups [][]ID`, `skipDisabled bool`, `skipPrefab bool`. Choose the design that minimises overhead for non-multi-term observers (likely: pointer-to-filter-state on `observerNode`; nil for single-term observers; bucket scan does a nil-check before evaluating filters).
   - `dispatchObservers` at `observer.go:392` evaluates the filter snapshot per node before firing. Short-circuit on first-failing term.
3. **Filter evaluation reuse**:
   - Either generalize `entityMatchesMonitorExcluding` (monitor_observer.go:266) to operate on raw `([]Term, [][]ID)` plus skipDisabled/skipPrefab — preferred — or add a sibling `entityMatchesTerms` that mirrors it. The two implementations must not drift.
   - All term kinds supported: TermAnd / TermNot / TermOptional (no-op) / TermOr (via orGroups). Pair IDs via `MakePair(...)` and `world.Wildcard()`. Sparse / DontFragment / Union components handled uniformly (the monitor primitive already does).
4. **yield_existing compatibility**:
   - With `WithYieldExisting()`: walk archetype tables for the trigger component, filter each table via `monitorMatchesTable` (reuse), then for each entity in passing tables also run the per-entity check (covers DontFragment / Union not in the table signature). Fire the observer once per qualifying entity per requested OnAdd/OnSet event.
   - OnRemove + yieldExisting: same panic rule as `ObserveIDWithOptions` (registering with only OnRemove + yield panics).
5. **Composition with prior observer features**:
   - `WithSource(e)` (Phase 16.12): trigger event must land on entity `e` AND the entity must pass the filter. State explicitly that the fixed-source check runs FIRST (in dispatch table key), then the multi-term filter (per node). Documented.
   - `(*Observer).SetEnabled(false)` (Phase 16.5): same gate as single-component observers. The enabled-flag check happens at the observerNode level before filter evaluation (cheap path).
   - Custom events (Phase 16.8): work as trigger events. The filter terms are still evaluated against the affected entity in the world. State that custom events with no entity (`Emit(fw, eventID, 0, ...)`) skip the filter and fire unconditionally — or panic — pick one (recommend: skip filter and fire, with a documented note).
   - Monitor observers (Phase 16.10): unrelated — monitors are NOT extended in this phase. State explicitly.
6. **Tests in `observer_multi_test.go`** (≥ 12 cases):
   1. `ObserveQuery(EventOnSet, [With(Position), With(Velocity)], fn)` — fires on `Set(e, Position)` when e has Velocity; does NOT fire when Velocity absent.
   2. `Without` filter: `[With(Position), Without(Frozen)]` — fires when Position is set on un-frozen entity; does NOT fire when Frozen present.
   3. `Or` filter: `[With(Position), Or(Velocity), Or(Acceleration)]` — fires when entity has Velocity OR Acceleration.
   4. Pair filter: `[With(Position), With(MakePair(w.ChildOf(), w.Wildcard()))]` — fires for entities with any ChildOf relationship.
   5. `WithYieldExisting()` + multi-term: register on a world with pre-existing matches; fires once per match.
   6. `(*Observer).SetEnabled(false)`: no fire while disabled; re-enabling resumes.
   7. `WithSource(specificEntity)` + multi-term: fires only when trigger event is on that entity AND filter passes.
   8. `ObserveQueryEvents` with `[EventOnAdd, EventOnSet]`: both events fire for qualifying entities; non-qualifying are silent on both.
   9. Sparse component as trigger: works correctly (term evaluator handles sparse uniformly).
   10. DontFragment component as filter: works correctly (filter eval uses `entityHasComponentForMonitor`-equivalent which checks `sparseStorage`).
   11. **Performance regression**: 100 multi-term observers all keyed on `Position` (same trigger), each with a different filter. Firing `Set(e, Position)` evaluates all 100 in O(N_observers × avg_filter_len). Assert wall-clock budget that confirms no quadratic blow-up (use a benchmark or scale-up assertion).
   12. Re-entry: handler mutates world (e.g., `Add(e, Marker)`); subsequent firings deferred via existing coalescer — no infinite loop, no panic.
   13. Coverage ≥ 95.0% on the multi-term code paths (gate via `cov_pkg.out` snapshot; existing CI floor is 90%, phase floor is 95%).
7. **Doc updates** (per CONTRIBUTING.md):
   - @docs/ObserversManual.md — add `## Multi-term observers` section with full examples (trigger vs filter, all term kinds, yield_existing composition, fixed-source composition). Update the single-component `Observe[T]` section to note it is a degenerate case of multi-term.
   - @docs/README.md:157 — flip to ✅ shipped (v0.70.0); link to new manual section.
   - @README.md — feature list bump if warranted.
   - @CHANGELOG.md — v0.70.0 entry in the existing format.
   - @ROADMAP.md — header bump to "through v0.70.0"; entry in Shipped section; remove from Phase 16.x candidates list.

## Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage on root `flecs` package ≥ 95.0% (phase floor; CI floor is 90%)
- **No regression** on existing observer / monitor / fixed-source / yield_existing / custom-event / disabled-observer tests.

## Explicit non-goals

- **No observer propagation along IsA edges** — `docs/README.md:159` gap stays open for a separate phase.
- **No auto-pick of the most selective term as trigger** — first-term is the trigger, always. Predictable, explicit.
- **No retroactive conversion of `Observe[T]` to use the multi-term machinery** — `Observe[T]` stays as the typed convenience wrapper; internal implementation may share helpers but the public single-component path keeps its current signature.
- **No `ObserveQuery[T]` generic variant** — typed convenience can be added later if there is demand.
- **No monitor-observer extension** — monitors are a separate observer kind and stay as-is.

## Locked decision points

1. **Trigger term selection**: FIRST term in the list. Must be TermAnd. Panic if absent or wrong kind.
2. **Empty terms list**: PANIC at construction (mirrors `validateAndSortTerms` panic in `query.go:1311`).
3. **Filter eval cost on non-matching events**: SHORT-CIRCUIT at first-failing filter term. Documented in the manual section.
4. **Term mutation post-registration**: NOT ALLOWED. Terms are snapshotted at registration (the slice is copied into `observerNode` filter state). Documented.
5. **Custom event with entity=0 and filter terms**: SKIP FILTER, fire unconditionally. Documented.
6. **File layout**: extend `observer.go` inline OR add `observer_multi.go` — defer to implementation phase. Phase 16.12 stayed inline; default to inline unless the surface area grows past three new exported functions.
