## Goal

Port upstream's *custom events* mechanism: let applications define arbitrary event identifiers, subscribe observers to them, and emit them as a typed event bus inside an ECS app. This makes Go-flecs's hard-coded four-event enum (`EventOnAdd`, `EventOnSet`, `EventOnRemove`, `EventOnTableCreate`) extensible while preserving every existing observer API unchanged.

In upstream C flecs, events ARE entities (`flecs.h:1437` — `ecs_event_desc_t.event` is `ecs_entity_t`; `flecs.h:1933, 1936, 1939, 1945` — `EcsOnAdd`/`EcsOnRemove`/`EcsOnSet`/`EcsOnTableCreate` are all `extern const ecs_entity_t`). The dispatch site keys on the event entity ID. Custom events compose with the entity machinery: a `PlayerDied` entity can be tagged, queried, and emitted with the same `ecs_emit` API as the built-ins (`flecs.h:5334`; defined `observable.c:1573`).

Go-flecs today (v0.62.0) keys observers on a closed `EventKind int` enum (`observer.go:10–23`). Custom events require switching the dispatch key to event-entity-ID while keeping the enum as a convenience layer (1:1 map). Concrete migration plan to **avoid breaking changes**:

1. Add **four built-in event entities** (`EventOnAdd`, `EventOnSet`, `EventOnRemove`, `EventOnTableCreate`) as entity slots in `world.go` — significant index shift. Built-in count goes from 40 to 44; user entities now start at index 44 (currently 40 per `world.go:89`). Update the index-mapping comments at `world.go:137–175` and the `// gets index N` comments around `world.go:190–419`.
2. The internal dispatch table key becomes `struct{id ID; eventEntity ID}` instead of `struct{id ID; event EventKind}`. The four `EventKind` constants keep working — they map 1:1 to the built-in event entity IDs via an internal lookup.
3. Public API additions for custom events go alongside the existing API; the existing `Observe[T]` / `ObserveID` / `Observe2[T]` / `ObserveWithOptions[T]` / `OnTableCreate` / `OnAdd[T]` / `OnSet[T]` / `OnRemove[T]` / `OnReplace[T]` functions are unchanged in signature and semantics.

### Mental model

Events are entities. Built-in events are pre-allocated entities (`w.EventOnAdd()` etc.). Custom events are user-allocated entities (`flecs.RegisterEvent(fw, \"PlayerDied\")`). Observers subscribe to an event-entity-ID. `Emit` fires the event-entity-ID. The dispatch table doesn't care whether the event entity is built-in or user-allocated.

### Phase tag note

`ROADMAP.md:103` currently lists Custom events as a Phase 16.9 candidate. This issue **bumps it to Phase 16.8** (it's the highest-impact unmet observer gap and unlocks a real ECS event-bus pattern). The other Phase 16.8 candidates listed at `ROADMAP.md:100–102` (OnTableDelete, OnTableEmpty/Fill, OnDelete/OnDeleteTarget observer events) shift to 16.9+; update accordingly.

## Deliverables

### 1. Event entity registration

- `RegisterEvent(fw *Writer, name string) ID` — allocate a new entity to serve as a custom event identifier. Uses the existing entity-name machinery (Name component, `name.go`) for debugging/introspection. Recommended: also add the new built-in `Event` tag (see decision 2 below) to the entity for discriminability.
- Convention: returned entity is plain (besides the optional `Event` tag); it just needs to be unique. Custom event entities can be added to other entities as tags, used in queries, and deleted like any other entity.

### 2. Emit API

- `Emit(fw *Writer, eventID ID, entity ID, payload interface{})` — fire `eventID` for `entity` with an opaque payload. Single-entity variant only (matches C's `ecs_event_desc_t.entity` single-entity alternative at `flecs.h:1460`). Payload is propagated to observer handlers via `unsafe.Pointer` internally (existing dispatch signature).
- `EmitTyped[T any](fw *Writer, eventID ID, entity ID, payload T)` — type-safe wrapper around `Emit`. Wraps `&payload` and routes through the same dispatch.
- Emit from inside a Write scope. If called while a dispatch is already running, the existing re-entrancy semantics of the observer system apply (mirrors how observers fire today — synchronous, in-order; **not** via the cmd_queue coalescer; see decision 4).
- No `Enqueue` variant in this phase (upstream `ecs_enqueue` at `flecs.h:5349` defers; we keep emit synchronous like the existing hook/observer fire path).

### 3. Observer registration for custom events

- `ObserveEvent(w *World, eventID ID, fn func(fw *Writer, e ID, payload interface{})) *Observer` — subscribe to a custom event. Payload arrives as `interface{}` (preserves whatever was passed to `Emit`).
- `ObserveEventTyped[T any](w *World, eventID ID, fn func(fw *Writer, e ID, payload T)) *Observer` — typed-payload variant; type-asserts at dispatch time. Panics with a clear message on mismatch.
- The four existing event entry points (`OnAdd[T]`, `OnSet[T]`, `OnRemove[T]`, `OnTableCreate`, plus `Observe[T]`, `ObserveID`, `Observe2[T]`, `ObserveWithOptions[T]`) remain unchanged — internally they subscribe to the corresponding built-in event entity.

### 4. Built-in event entities

Add four new built-in entity slots at the end of the current built-in range (after `anyID` at `world.go:89`). Tentative ordering (verify and adjust the comments at `world.go:137–175` to match):

- Index 40: `EventOnAdd` built-in event entity
- Index 41: `EventOnSet` built-in event entity
- Index 42: `EventOnRemove` built-in event entity
- Index 43: `EventOnTableCreate` built-in event entity
- Index 44: `Event` built-in tag entity (marks an entity as an event identifier; see decision 2)

User entities then start at index 45 (was 40).

Add accessors on `*World`:

- `(*World).EventOnAdd() ID`
- `(*World).EventOnSet() ID`
- `(*World).EventOnRemove() ID`
- `(*World).EventOnTableCreate() ID`
- `(*World).Event() ID`

The `EventKind` enum constants remain as a convenience surface (`observer.go:13–23`); add an unexported lookup `eventKindToEntity(w *World, ev EventKind) ID` used at the boundary between legacy callers and the new entity-keyed dispatch table.

### 5. Dispatch wiring

- Replace `observerKey{id ID; event EventKind}` (`observer.go:42–45`) with `observerKey{id ID; eventEntity ID}`.
- Update `addObserverNode` (`observer.go:186–204`) and `dispatchObservers` (`observer.go:211–223`) signatures: take `eventEntity ID` instead of `event EventKind`.
- Internal fire helpers (`hooks.go:156, 167, 178` and `world.go:1373`) call `w.dispatchObservers(id, w.eventOnAddID, e, ptr)` etc., consulting the four cached built-in event entity IDs.
- The dispatch table now uses the same shape for built-ins and custom events. No two code paths.

### 6. Tests in `observer_custom_events_test.go` (≥ 10 cases)

1. Register a custom event, subscribe one observer, emit it, verify the handler fires with the payload.
2. Emit a custom event with no observers: no-op, no panic.
3. Multiple observers on the same custom event fire in registration order.
4. Custom event with disabled observer (`obs.SetEnabled(false)` from Phase 16.5): handler does NOT fire; re-enable resumes.
5. Built-in events still work via legacy API: register `OnAdd[T]`, add the component, handler fires (regression guard for the dispatch-key refactor).
6. Built-in events accessible via the new entity API: `ObserveEvent(w, w.EventOnAdd(), fn)` is equivalent to `OnAddID(w, componentID, fn)` for that component (cross-check fire equivalence).
7. `EmitTyped[T]` + `ObserveEventTyped[T]` round-trip on a struct payload.
8. Payload visibility semantics — pick **read-only payload** (the `interface{}` value is shallow-copied at the API boundary; mutations inside a handler do not leak to subsequent observers in the same dispatch). Document and test this.
9. Re-entrant emit: emitting a custom event from inside a handler fires synchronously (matching the existing observer fire path). Test the ordering. (Decision 4 below.)
10. yield_existing on a custom event entity: no-op + clear error. There is no \"currently matching\" concept for an arbitrary event. Decide between (a) silent no-op or (b) panic. **Recommend silent no-op** to keep API symmetric with the built-in-events case where `WithYieldExisting()` already only applies to OnAdd/OnSet (`observer_options.go:32`); add a doc comment stating this. Test that the observer registers successfully and never fires from a sweep.
11. Custom event entity also tagged with the built-in `Event` tag (decision 2): verify `HasID(eventID, w.Event())` returns true.
12. Deleting a custom event entity unsubscribes all observers for it: any subsequent `Emit` is a no-op. (Verify cleanup story or document as deferred — pick one and test.)

Coverage on the root `flecs` package must stay ≥ 95.0% (current target per `CONTRIBUTING.md:9–10`).

### 7. Documentation updates (per CONTRIBUTING.md:69)

- **`docs/ObserversManual.md`** — add a new top-level § *Custom events* with examples: event registration via `RegisterEvent`, emit via `Emit` and `EmitTyped`, subscribe via `ObserveEvent` and `ObserveEventTyped`. Include the \"events are entities\" mental model and a callout linking the four built-in event entities (`w.EventOnAdd()` etc.). Position the section after § *OnTableCreate* (around `docs/ObserversManual.md:485`).
- **`docs/README.md` line 156** — flip the gap line to ✅ shipped (v0.63.0). Note: the line currently says \"three built-in events\" — it is stale (Phase 16.7 added a fourth). Fix the count phrasing as part of flipping it.
- **`README.md`** — feature list bump. Add a Custom Events row to the table around `README.md:220–223`.
- **`CHANGELOG.md`** — v0.63.0 entry at the top. Call out: (a) the dispatch table now keys on event entity IDs; (b) the `EventKind` enum is preserved as a convenience layer; (c) **no breaking change** to existing observer/hook APIs; (d) built-in entity count moves from 40 to 45 — call out the index renumbering of user entities (40 → 45 start).
- **`ROADMAP.md`** — heading bump to \"Shipped (through v0.63.0)\". Move the *Custom events* line from `ROADMAP.md:103` (Phase 16.9 candidate) into the Shipped section. Update the remaining Phase 16.x candidate list — promote OnTableDelete / OnTableEmpty-Fill / OnDelete-Target observer events from 16.8 to 16.9 candidates.

## Constraints

- @docs/README.md — gap inventory; line 156 is the canonical entry being closed by this phase. Verify line still reads as documented and update the phrasing about \"three built-in events\" (now four after v0.62.0) before flipping to ✅.
- @observer.go — current `EventKind` enum (`observer.go:10–23`), `observerKey` (`observer.go:42–45`), `dispatchObservers` (`observer.go:211–223`), and `Observe*` registration API. This file is the epicenter of the refactor. All four existing public registration functions must keep their signatures.
- @observer_table.go — Phase 16.7 precedent for adding a new event kind. `tableCreateSentinelID ID = 0` (line 14) is the cleanest precedent for how an untyped event maps onto the existing dispatch table. Reuse the pattern: custom-event observers use the actual event-entity ID as the key (not the component ID), with the subject entity passed via the `e ID` callback arg.
- @observer_options.go — Phase 16.5 precedent for `ObserverOptions` + `WithYieldExisting`. Custom events must interact predictably with this option: recommended **no-op for custom events**, mirroring how `WithYieldExisting()` already silently filters to OnAdd/OnSet for built-ins (`observer_options.go:55–63`). Document the semantic.
- @hooks.go — three dispatch sites at `hooks.go:156, 167, 178` fire built-in events. After the refactor they call `dispatchObservers(id, w.eventOnAddID, e, ptr)` etc. — the `EventKind` constant becomes an internal lookup to the built-in event-entity ID. **No change to public `OnAdd[T]` / `OnSet[T]` / `OnRemove[T]` / `OnReplace[T]` signatures.**
- @world.go — built-in entity allocation order (`world.go:52–89` field decls, `world.go:137–175` index map, `world.go:190–419` constructor allocation). Adding 5 new built-ins shifts the \"user entities start at\" index from 40 to 45. Update every place this index is referenced (including the comment at `world.go:89` and `world.go:176`).
- @scope.go — `*Writer` is the receiver for `Emit` (mutation API; emit fires observers which may write). `Emit` is gated by `checkExclusiveAccessWrite` like other write-side entry points.
- @cmd.go — re-entrant emit: observer fire path today is **synchronous**, NOT routed through the cmd_queue coalescer. `Emit` follows the same path. Inside a handler, calls to `fw.Set`, `fw.Delete` etc. are already deferred via the existing Writer mechanics — no special re-entry machinery needed for `Emit` itself.
- @observer.go:42–45 (`observerKey`) — must change shape. Every caller of `addObserverNode` and `dispatchObservers` must be updated (`hooks.go:156, 167, 178`; `observer.go:121, 142, 172`; `observer_table.go:48`; `observer_options.go:84`; `world.go:1373`).
- @CONTRIBUTING.md:9–10 — coverage ≥ 90% required (currently ~97%). This phase must keep coverage ≥ 95.0%.
- @CONTRIBUTING.md:69–80 — feature work updates the relevant `docs/` page. New feature section in `docs/ObserversManual.md` is mandatory.
- @ROADMAP.md:103 — Custom events currently tagged Phase 16.9. This issue bumps to 16.8; update accordingly.

### Upstream C references (verify before filing PR)

- `flecs.h:1431–1478` — `ecs_event_desc_t` (event-descriptor struct; `event` field is `ecs_entity_t`).
- `flecs.h:1374–1429` — `ecs_observer_desc_t.events[FLECS_EVENT_DESC_MAX]` array of entity IDs.
- `flecs.h:1933–1948` — `EcsOnAdd`, `EcsOnRemove`, `EcsOnSet`, `EcsMonitor`, `EcsOnTableCreate`, `EcsOnTableDelete` — all `extern const ecs_entity_t` (events ARE entities).
- `flecs.h:5334–5336` — `ecs_emit(world, desc)` API.
- `flecs.h:5349–5351` — `ecs_enqueue` (deferred variant; **not** in scope for this phase).
- `observable.c:1573–1599` — `ecs_emit` definition; the single-entity path at lines 1582–1590 maps an entity to (table, offset, count=1) — this is the model for Go-flecs's single-entity `Emit`.
- `flecs.h:1466, 1471` — payload via `void *param` / `const void *const_param`. Go port uses `interface{}` (simpler than dual mutable/const distinction; documented as read-only by convention).

## Open decisions (recommended values stated)

1. **Payload type**: untyped `interface{}` for `Emit` + typed generic wrapper `EmitTyped[T]`. **Recommend untyped + typed wrapper** — matches upstream `void *param` flexibility without forcing a payload-type registration step.
2. **`Event` tag built-in**: add a new built-in `Event` tag (index 44) that `RegisterEvent` automatically applies to event entities. **Recommend yes** — enables `HasID(eventID, w.Event())` discrimination and future query support over event entities. Cost: one extra built-in entity slot.
3. **yield_existing on custom events**: silent no-op (clearer than panic). **Recommend silent no-op + doc note** — mirrors the existing OnRemove silent-filter in `observer_options.go:88` but applies to all events outside the OnAdd/OnSet built-ins.
4. **Re-entrant emit**: synchronous fire path matching the existing hook/observer dispatch. **Recommend synchronous** — matches Phase 16.0 OnReplace's choice; mutations from within the handler still defer via the existing `Writer` cmd queue.
5. **Custom event entity deletion semantics**: deleting an event entity invalidates all observers subscribed to it (handlers silently drop). **Recommend implementing cleanup**; alternative is to document that deleting an event entity while observers are subscribed is undefined and leaves dangling observer nodes. Pick one; test the chosen path.

## Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage on the root `flecs` package ≥ 95.0%
- **All existing observer/hook tests pass unchanged** — backward compatibility is mandatory. The full `observer_test.go`, `observer_lifecycle_test.go`, `observer_table_test.go`, `hooks_test.go`, `example_observer_test.go`, `example_hooks_test.go` suites must remain green without modification (besides any internal helper renames if `observerKey` shape changes leak into tests).

## Explicit non-goals

- No removal of the `EventKind` enum (`observer.go:10`) — kept as a convenience layer mapped 1:1 to built-in event entities.
- No `Enqueue` / deferred-emit API (upstream `ecs_enqueue` at `flecs.h:5349`). Custom emit is synchronous like the existing observer fire path.
- No multi-term observers (`docs/README.md:157`) — separate large phase.
- No observer propagation along IsA edges (`docs/README.md:159`) — separate large phase.
- No event taxonomy / inheritance — custom events are flat. Event entity A does NOT inherit observers from event entity B even if A `IsA` B.
- No `Monitor` event (`flecs.h:1942`) — separate gap (`docs/README.md:160`).
- No `OnTableDelete` / `OnTableEmpty` / `OnTableFill` — separate phases (`ROADMAP.md:100–102`).
- No batched/range emit (upstream `ecs_event_desc_t.table` + `offset` + `count` at `flecs.h:1445, 1452, 1457`). Single-entity emit only in this phase.
