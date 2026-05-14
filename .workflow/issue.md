## Goal

Port **fixed-source observer terms** from upstream C flecs. Today, an observer registered via `Observe[T](w, EventOnSet, fn)` (or `ObserveID`, or `ObserveWithOptions[T]`) fires whenever component `T` is set on **any** entity. There is no way to subscribe an observer to "set of `T` on a **specific** named entity" without writing a generic observer and filtering inside the callback (`if e == playerID { ... }`). The filter approach pays full dispatch cost for every unrelated set and is awkward at the call site.

Fixed-source observer terms move the filtering into the dispatch layer. Canonical use cases: "watch the Player entity," "watch the global GameTime singleton," "fire when this UI widget's `Selected` flag flips." The user names the source entity at registration time; the observer is only invoked when the event lands on that entity.

This phase introduces a new observer-registration option `WithSource(e ID)`, plumbed through `ObserveWithOptions[T]` and a new `ObserveIDWithOptions`. Lookup extends from `{componentID, eventEntityID}` to also consider `{componentID, eventEntityID, sourceID}` for fixed-source observers, with the per-source map laid out so any-entity observers (the common case) pay zero additional cost.

Target release: **v0.67.0**, following v0.66.0 query groups at commit `4694ff2`.

### C upstream references

Cited line numbers in `/work/agents/claude/projects/SanderMertens/flecs`:

1. `ecs_term_t.src` (type `ecs_term_ref_t`) — `include/flecs.h:820`. The `src.id` field carries either a `$this` variable marker (`EcsThis | EcsIsVariable | EcsSelf`) or an explicit entity ID (with `EcsIsEntity` flag). `ecs_term_ref_t` is defined at `include/flecs.h:798-811`; `EcsIsEntity` flag at `include/flecs.h:796`.
2. `ecs_observer_desc_t.query.terms[].src.id` — `include/flecs.h:1374-1429` (struct) → `include/flecs.h:1382` (`query` field) → `include/flecs.h:841` (`ecs_query_t.terms`). The full path from observer desc to per-term source.
3. **Dispatch site** — `src/observer.c:418-535` (`flecs_uni_observer_invoke`). The key block is `src/observer.c:479-516`: when `match_this` is false (i.e. the term has a fixed source), the C dispatch loop walks the event's entity list and compares `observer_src == e` (line 499). On match, it fires the callback with `it->sources[0] = e`; on miss it skips. **No separate per-source map** — fixed-source observers live in the same per-component observer list and are filtered inline. The Go phase can either mirror this (single list with a `source` field on each node) or use a per-source side map for O(1) hit; the issue recommends the side-map approach for the common case where most observers are any-entity.
4. **Registration validation** — `src/observer.c:1170-1183`. The "observer must have at least one term where `src` matches `$this`" check; pure-non-`$this` observers panic with `ECS_UNSUPPORTED` ("observers with only non-$this variable sources are not yet supported"). The Go phase's single-term-only design effectively limits non-`$this` use to "fixed entity source" — variable sources (e.g. cascading observers) are out of scope.
5. **yield_existing for fixed source** — `src/observer.c:761-816` (`flecs_observer_yield_existing`). The sweep uses `ecs_query_iter(world, o->query)` (line 792); the query iterator naturally yields the single named source if it carries the component, or zero entities if it does not. Therefore upstream behavior: yield_existing + fixed source = fires once iff the source has the component at registration.
6. **Disabled / Prefab interaction** — `src/observer.c:340-354` (`flecs_ignore_observer`). The check is against the **affected entity's table** (`table->flags & EcsTableIsPrefab/IsDisabled`) — i.e. the event's entity table, not the source entity. For fixed-source observers, the event's entity IS the source, so a `Disabled` or `Prefab` source would skip the observer (the source table carries the tag). This is upstream behavior; the Go phase should mirror it.

### Go-side state

- `@observer.go` — `EventKind` enum, `observerKey{id, eventEntity}` storage key, `addObserverNode`, `dispatchObservers`. Dispatch table is `w.observers map[observerKey][]*observerNode`. Post-Phase 16.8, the key includes the event entity. Fixed-source adds a third dimension: source entity.
- `@observer_options.go` — `ObserverOptions{yieldExisting bool}`, `WithYieldExisting()`, `ObserveWithOptions[T]`. The option-pattern shape is established; `WithSource(e)` plugs in here.
- `@observer_custom.go` — Phase 16.8 custom-event observer; uses the same `addObserverNode` / `dispatchObservers` path. Fixed-source must compose naturally with custom events.
- `@monitor_observer.go` — Phase 16.10. Demonstrates multi-term observer semantics in a separate dispatch path. Fixed-source for monitors is **out of scope** for this phase (the explicit non-goals below) — monitors already express "watch entity X" via single-term query semantics if needed.

### Pattern reference

- Phases 16.5 / 16.8 / 16.10 establish observer registration + options shape.
- Phase 16.7 (OnTableCreate) — precedent for an observer that has no meaningful "source" concept.
- Phase 16.11 (query groups, just shipped) — most recent phase; uses the chained-options pattern (`AndOrderBy`, `AndGroupBy`). `WithSource` is a simple bool-bearing option, not chainable; no new chaining method needed.

## Deliverables

### 1. New observer-registration option

Add to `observer_options.go` (or a new `observer_fixed_source.go` if cleaner):

```go
// WithSource returns an option that constrains an observer to fire only when
// the event lands on the named entity. Compose with the events list passed to
// ObserveWithOptions / ObserveIDWithOptions. The zero ID and unknown IDs are
// rejected at registration: WithSource(0) panics; WithSource(staleID) registers
// successfully but never fires.
func WithSource(e ID) ObserverOptions { ... }
```

Internally extend `ObserverOptions`:

```go
type ObserverOptions struct {
    yieldExisting bool
    source        ID // 0 = any-entity (default); non-zero = fixed source
}
```

### 2. Storage extension

The current shape is `w.observers map[observerKey][]*observerNode`. Extend to:

```go
type observerBucket struct {
    anyEntity     []*observerNode      // current behavior
    fixedSource   map[ID][]*observerNode // nil unless any fixed-source observer is registered for this key
}
w.observers map[observerKey]*observerBucket
```

**Performance invariant**: any-entity observer dispatch must traverse exactly one slice (the `anyEntity` slice), identical to today. The `fixedSource` map is allocated lazily on first fixed-source registration and consulted only when non-nil.

Dispatch (in `dispatchObservers(id, eventEntity, e, ptr)`):

1. Look up bucket for `observerKey{id, eventEntity}`.
2. Iterate `bucket.anyEntity` — fire each enabled node (today's behavior).
3. If `bucket.fixedSource != nil`, look up `bucket.fixedSource[e]` and iterate that slice.

Registration order is preserved within each list. Across lists, fixed-source observers for entity `e` fire AFTER any-entity observers for the same component, mirroring C's single-list semantics where insertion order is the only ordering guarantee. (Document this in the godoc.)

### 3. Registration API

| Function | Existing? | After this phase |
|---|---|---|
| `Observe[T](w, event, fn)` | yes | unchanged |
| `ObserveID(w, id, event, fn)` | yes | unchanged |
| `Observe2[T](w, events, fn)` | yes | unchanged |
| `ObserveWithOptions[T](w, opts, events, fn)` | yes (Phase 16.5) | accepts `WithSource(e)`; if `opts.source != 0`, registers a fixed-source node instead of an any-entity node |
| `ObserveIDWithOptions(w, id, opts, events, fn)` | **new** | options variant of `ObserveID`; accepts `WithSource(e)` |

`ObserveIDWithOptions` is necessary because `ObserveID` is the raw-ID entry point and currently has no options variant; fixed-source must be expressible there too (e.g., for pair IDs).

### 4. yield_existing + WithSource

Composes: at registration, if the source entity has the component, fire `entered=true` once (for OnAdd / OnSet); otherwise no initial fire. Implementation: instead of walking `compIndex.TablesFor(id)`, just check `HasID(source, id)` and call the callback with `(source, ptr)` if true. Single check; O(1).

`WithYieldExisting() + WithSource(e) + OnRemove`-only events: same panic as today's any-entity case (yieldExisting requires at least one OnAdd or OnSet event).

### 5. Built-in event entity compatibility

Fixed-source must work for OnAdd, OnSet, OnRemove. **OnTableCreate + WithSource is nonsensical** (tables are not entities and have no source semantics) — panic at construction with a clear message naming the violation.

### 6. Custom event + fixed source

Composes naturally because the dispatch extension is at the storage layer; `Emit(fw, eventID, entity, payload)` already routes through `dispatchObservers(eventID, eventID, entity, ptr)`. Fixed-source observers registered via `ObserveEvent` (a future `ObserveEventWithOptions` if needed) get the same per-source filtering.

For this phase, add the option support to `ObserveEvent` via a new `ObserveEventWithOptions(w, eventID, opts, fn)`. If scope is a concern, defer custom-event fixed-source to a follow-up — but the dispatch-layer change automatically enables it, so adding the registration entry point is cheap.

### 7. Tests in `observer_fixed_source_test.go`

At least 10 cases, covering:

1. `Observe + WithSource(playerID), OnSet[Position]`. Set Position on otherEntity → handler does NOT fire. Set on playerID → handler fires exactly once.
2. `OnAdd + WithSource`: AddID on other does not fire; AddID on source fires.
3. `OnRemove + WithSource`: same pattern.
4. `yield_existing + WithSource(e)` when `e` already has the component → fires once with the existing value.
5. `yield_existing + WithSource(e)` when `e` does NOT have the component → no fire.
6. Multiple fixed-source observers on the same source — all fire in registration order.
7. Mixed: one any-entity observer + one fixed-source observer on the same component. Set on the fixed source → both fire (any-entity first, then fixed-source). Set on a different entity → only the any-entity observer fires.
8. Disabled fixed-source observer does not fire (`SetEnabled(false)` works identically).
9. `WithSource(0)` panics at construction with a clear message.
10. `WithSource(stale ID)` registers successfully; subsequent emits on the dead ID never match (semantic: the observer remains live but is unreachable).
11. `OnTableCreate + WithSource` panics at construction.
12. Custom event + WithSource fires correctly via `Emit`.

Coverage ≥ 95.0%.

### 8. Documentation

- `docs/ObserversManual.md` — replace the gap stub at line 881-883 (currently "Not ported") with a full section titled "Fixed-source observers" with example, semantics (including the Disabled/Prefab interaction), and the OnTableCreate ban.
- `docs/README.md` line 162 — flip from "not yet ported" to "✅ **shipped in v0.67.0** via `WithSource(e ID)` option on `ObserveWithOptions[T]` / `ObserveIDWithOptions` / `ObserveEventWithOptions`."
- `README.md` — add a feature-table row for fixed-source observers under the Observers section (around line 226).
- `CHANGELOG.md` — v0.67.0 entry at top, following the v0.66.0 template.
- `ROADMAP.md` — heading bump to "Shipped (through v0.67.0)"; add the new shipped row; remove or update line 109 (the Phase 16.12 candidate stub).
- `ROADMAP.md` line 108 stays unchanged (observer propagation along IsA edges is still future work — see non-goals).

### 9. Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%
- No regression on existing observer tests (especially `observer_test.go`, `observer_lifecycle_test.go`, `observer_custom_events_test.go`).
- The `observerKey` → `*observerBucket` transition must not regress benchmark numbers for any-entity observer dispatch. Re-run `BenchmarkObserverDispatch` (or add one if absent) and report.

## Open decision points (with recommendations)

1. **`WithSource(0)`**: **panic at construction**. The zero ID is never a valid entity; silent acceptance hides bugs.
2. **`WithSource(stale entity ID)`**: **silent no-fire**, matching existing stale-ID semantics elsewhere in the API (a deleted entity in a query argument is silently skipped). No registration-time validity check; the entity may be deleted later anyway, so a check is racy.
3. **`OnTableCreate + WithSource`**: **panic at construction** with message naming the violation. Tables are not entities; the option is nonsense for this event.
4. **Dispatch order**: any-entity observers fire BEFORE fixed-source observers for the same `(component, event)` key. Within each list, registration order. Document this.
5. **Storage layout**: lazy `fixedSource map[ID][]*observerNode`, allocated on first fixed-source registration for the bucket. Zero overhead for the common case (any-entity-only buckets).
6. **API placement**: add `ObserveIDWithOptions` alongside `ObserveID` in `observer.go`. Add `ObserveEventWithOptions` alongside `ObserveEvent` in `observer_custom.go`. Add `WithSource` to `observer_options.go`. No new file needed.

## Explicit non-goals

- **No multi-term observers** (gap line 157). Fixed-source is single-term only in this phase. A multi-term observer with a mix of `$this` and fixed-source terms (e.g. "fire when Position changes on $this AND that entity also has Velocity") is a separate, larger phase.
- **No observer propagation along IsA edges** (gap line 159). Subclass-propagating observers are a separate phase.
- **No fixed-source for monitor observers** (Phase 16.10). Monitors express "watch entity X" via a single-term query if needed; the fixed-source mechanism is for component observers.
- **No automatic cleanup when the named source is deleted**. The observer continues to exist; subsequent emits for the dead ID just don't match. Matches existing stale-ID semantics.
- **No traversal modifiers on the fixed source** (e.g. `WithSource(parent).Up(ChildOf)`). The source is the literal entity, not a query expression.

## Constraints

- @observer.go — `EventKind`, `observerKey`, `addObserverNode`, `dispatchObservers`; the storage layer that must be extended (lazy per-source map, no any-entity regression).
- @observer_options.go — `ObserverOptions`, `WithYieldExisting`, `ObserveWithOptions[T]`; the option-pattern shape that `WithSource(e)` plugs into.
- @observer_custom.go — custom-event registration and dispatch; the path that must compose with fixed source.
- @monitor_observer.go — Phase 16.10 precedent for multi-term observer design; informs the boundary of this phase (monitors are NOT fixed-source candidates).
- @docs/README.md — line 162 carries the "not yet ported" stub for fixed-source observer terms; flip to shipped on completion.
- @docs/ObserversManual.md — lines 881-883 contain the gap section to be replaced with a real "Fixed-source observers" section.
- @ROADMAP.md — line 109 lists the Phase 16.12 candidate ("Fixed-source query terms"); update on completion, bump heading to "through v0.67.0".
- @CHANGELOG.md — add v0.67.0 entry at top following the v0.66.0 template (Added / Implementation notes / Explicit non-goals subsections).
- @README.md — observer feature table around line 226; add a row for fixed-source observers.
- @CONTRIBUTING.md — documentation policy: every behavior change updates the relevant docs page in the same PR.
