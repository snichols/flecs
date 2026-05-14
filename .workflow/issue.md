## Goal

Add **monitor observers**: a registration form that fires once when an entity *starts* matching a query (`entered=true`) and again when the entity *stops* matching (`entered=false`), rather than on every raw component event. This is the canonical pattern for alert systems, state transitions, debug counters, and tutorial / UX hints.

After this phase, `docs/README.md` line 160 (Monitor observers gap) flips to ✅ shipped (v0.65.0).

### API surface

```go
// Fire when entity enters/leaves the query defined by terms.
// entered=true on first match; entered=false on stop matching.
Monitor(w *World, terms []Term, fn func(fw *Writer, e ID, entered bool)) *Observer

// Options variant — supports WithYieldExisting().
MonitorWithOptions(w *World, terms []Term, opts ObserverOptions, fn func(fw *Writer, e ID, entered bool)) *Observer
```

`MonitorTyped[T]` is **not provided in v1** — a monitor is keyed on a multi-term query, not a single component, so there is no single `T` payload to surface. The handler receives only `(fw, e, entered)`.

### Built-in event entity

- `EventMonitor` constant (mirrors `EventOnTableCreate` shape from Phase 16.7).
- `World.EventMonitor() ID` accessor.
- Allocation slot: **index 46** (currently the next free built-in slot after Phase 16.9's `DependsOn` at 45 — verified in `world.go:188-195`). User entities shift from index 46 to **index 47**.
- This is a built-in-count breaking change (matches the Phase 16.7 / 16.8 / 16.9 precedent); document in CHANGELOG and MIGRATING.

### Match-state tracking — design lock-in

The user's spec proposed option A (per-monitor `map[ID]bool` matched set). Upstream C flecs (`observer.c:624-640`) uses a different mechanism: it computes match state **on each entity migration** by checking the previous-table vs new-table against the observer's query, with `EcsObserverIsMonitor` flagged on the observer. No per-entity bitset is stored — the table-pair check is sufficient because:

1. Entity match state can only change when the entity migrates archetype tables (table signatures are immutable).
2. Sparse / DontFragment / Union storage operates outside archetype, but those terms either don't participate in monitors today or carry their own change-tracking surface.

**Locked-in approach for v1: hybrid.**
- Archetype terms: re-evaluate the monitor's query against the entity's previous and new tables on each `migrate()` call (mirror C semantic exactly; O(monitors × terms) per migration; cheap, no per-entity state).
- Sparse / DontFragment / Union terms: fall back to a per-monitor `map[ID]struct{}` matched set, consulted and updated on each `OnAdd` / `OnSet` / `OnRemove` for any component the monitor's terms reference.
- Mixed archetype + sparse terms: use the matched-set fallback for the whole monitor (simpler than splitting; rare in practice).

The matched-set fallback is what option A described; the table-pair check is upstream's approach. Document both in `observer.go` comments and `ObserversManual.md`. Flag the all-archetype case as the optimized path; mixed/sparse case as the simple path. Future perf phase can hoist sparse monitors into a cached-query subscription.

### Fire path

Hook into existing fire sites:
- `migrate()` in `world.go` — for archetype monitors, evaluate query on previous and new tables; fire `entered=true` (matched new, not previous) or `entered=false` (matched previous, not new).
- `fireOnAdd` / `fireOnSet` / `fireOnRemove` in `hooks.go` — for monitors with sparse/DontFragment/Union terms or those involving the changed component, re-evaluate match state for the affected entity and fire on transitions.

Disabled monitor (Phase 16.5 `SetEnabled(false)` semantic) — silently skip. Do NOT update the matched set while disabled; on re-enable, the next migration / change for an entity computes truth fresh against the live query. This matches Phase 16.6's disabled-system 'no catch-up' semantic — document.

### yield_existing on a monitor

`MonitorWithOptions(w, terms, WithYieldExisting(), fn)` sweeps the world at registration: for every entity currently matching the terms (resolved via `NewCachedQueryFromTerms` walk, skipping `Disabled` / `Prefab` tables per Phase 16.2), invoke `fn(fw, e, true)`. The sweep fires the new monitor's callback directly (not via `dispatchObservers`), matching the existing yield_existing pattern in `observer_options.go:99-115`.

### Marshal semantics

Existing observers are **not serialized** (handlers are function pointers; verified — `marshal.go` does not reference observers). Same for monitors: the monitor registration is process-local. The sparse-fallback matched set is also NOT serialized — on unmarshal, monitors must be re-registered by application code (recommend using `WithYieldExisting()` to reconstitute matched set from world state). Document this clearly.

## C upstream research (verified)

Citations from `/work/agents/claude/projects/SanderMertens/flecs`:

- `include/flecs.h:1942` — `EcsMonitor` event entity declaration.
- `src/world.c:114` — `EcsMonitor = FLECS_HI_COMPONENT_ID + 66` (allocation).
- `src/bootstrap.c:1051` — bootstrapped name 'EcsMonitor', module EcsFlecsCore.
- `src/observer.c:1128` — when `desc->events[0] != EcsMonitor`, take the trivial-observer optimization path; monitors skip the optimization.
- `src/observer.c:1208-1238` — monitor registration unfolds `events = [EcsMonitor]` into `events = [EcsOnAdd, EcsOnRemove]` plus sets `EcsObserverIsMonitor` flag on the observer impl.
- `src/observer.c:624-640` — **the fire-site check**: for an entity matching the new (`table`) range, if `EcsObserverIsMonitor` is set, also evaluate the query against the previous (`prev_table`) range. If the previous also matches, *suppress* the fire (entity was already matching — no transition). The 'left the match set' direction is achieved symmetrically via the OnRemove leg.
- `src/observer.c:1037` — for monitors, optional terms are dropped (only `And` / `Not` terms participate in the match decision).
- Cost: per-monitor O(query) per archetype migration; no per-entity bookkeeping in archetype path.

## Go-side state (verified)

- `observer.go:9-23` — current `EventKind` enum: `EventOnAdd=1`, `EventOnSet=2`, `EventOnRemove=3`, `EventOnTableCreate=4`. Add `EventMonitor=5`.
- `observer.go:51-64` — `eventKindToEntity()` switch; extend with `EventMonitor` case.
- `observer.go:225-242` — `dispatchObservers()` is a flat map dispatch; monitors will bypass this and fire directly from `migrate()` / hook fire sites to avoid double-dispatch.
- `observer_custom.go` — Phase 16.8 added the event-as-entity infrastructure; monitors inherit the entity-allocation model.
- `observer_options.go:32-34, 99-115` — `WithYieldExisting()` sweep pattern; monitor sweep follows the same shape.
- `observer_table.go:14, 29-74` — Phase 16.7 `OnTableCreate` is the closest precedent (untyped observer, sentinel key, direct dispatch from `notifyTableCreated`).
- `query.go:101-181` — `Term` struct, `With`/`Without`/`Maybe`/`Or` constructors, `Query` type.
- `cached_query.go:96-205` — `CachedQuery` with `tryMatchTable` / table-set tracking; potential future optimization path for monitor table membership.
- `world.go:90-95, 189-195, 439-477, 636-653` — built-in event entity allocation + accessors. Monitor adds `eventMonitorID` field, allocation block, and `EventMonitor()` accessor at the next free slot (index 46).
- `world.go:1206-1357` (`migrate`) and `world.go:1358-1394` (`migrateArchetypeOnly`) — fire sites where archetype-monitor transition checks must hook in.
- `hooks.go:147-179` — `fireOnAdd` / `fireOnSet` / `fireOnRemove` — fire sites where sparse-fallback monitor re-evaluation hooks in.
- `marshal.go` — confirmed no observer state is serialized today; monitors follow that precedent.

## Deliverables

1. **New event kind** — `EventMonitor` constant in `observer.go`; built-in entity at index 46; `World.EventMonitor() ID` accessor in `world.go`.
2. **Registration**:
   - `Monitor(w, terms, fn)` — bare form.
   - `MonitorWithOptions(w, terms, opts, fn)` — with `WithYieldExisting()`.
   - No `MonitorTyped[T]` in v1 (rationale documented).
3. **Match tracking** — hybrid: archetype-only monitors use table-pair check on migrate; mixed/sparse monitors use per-monitor `matched map[ID]struct{}` updated in hooks.
4. **Fire path** — wire into `migrate` (archetype monitors) and `fireOnAdd`/`fireOnSet`/`fireOnRemove` (sparse fallback). Respect `Observer.enabled`.
5. **yield_existing** — at registration, sweep matches via the monitor's query and fire `entered=true` for each pre-existing match.
6. **Tests** in `monitor_observer_test.go` (≥ 10 cases, coverage ≥ 95.0%):
   - Single-term: add Health → entered=true; remove → entered=false; add again → entered=true.
   - Multi-term: `With Health AND Without Frozen` — add Health entered=true; add Frozen entered=false; remove Frozen entered=true.
   - 100 entities — monitor fires only for the matching subset.
   - yield_existing — fires entered=true for every pre-existing match at registration.
   - Disabled monitor — changes during disabled period do NOT fire; re-enabling does NOT retroactively fire (document semantic).
   - Overlapping monitors — multiple monitors with shared terms all fire correctly.
   - Re-entrant: mutating other entities inside a monitor handler triggers other monitors via the deferred coalescer.
   - Marshal round-trip — monitors are process-local; round-trip preserves world state but not monitor registrations or matched sets (document).
   - Sparse component term — monitor on a `DontFragment` / sparse component fires via the hook-fallback path.
   - Unsubscribe during dispatch — current-dispatch already-fired monitors unaffected; not-yet-visited skipped (mirrors Phase 14.7 behavior).
7. **Doc updates**:
   - `docs/ObserversManual.md` — new § Monitor observers with worked examples; cross-link to query construction.
   - `docs/README.md` — flip line 160 (current location of the Monitor gap entry) to ✅ shipped (v0.65.0).
   - `README.md` — feature list bump.
   - `CHANGELOG.md` — v0.65.0 entry at top.
   - `ROADMAP.md` — heading bump to 'through v0.65.0'; move Monitor observers from line 106 (Phase 16.11 candidate) into the Shipped section.
   - `MIGRATING.md` — note built-in entity count: 46 → 47; user entity index: 46 → 47.

## Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%
- No regression on existing observer, query, or cached-query tests.

## Explicit non-goals

- General multi-term observers for OnAdd/OnSet/OnRemove (`docs/README.md:157` gap) — separate phase. Monitors HAPPEN to take multi-term queries because that is their natural input, not because we are exposing multi-term observers in general.
- Observer propagation along IsA edges (`docs/README.md:159` gap) — separate phase.
- Fixed-source observer terms (`docs/README.md:162` gap) — separate phase.
- Per-component change granularity finer than OnAdd/OnRemove/OnSet.
- Cached-query-subscription optimization for sparse monitors (future perf path; v1 uses simple matched-set fallback).
- `MonitorTyped[T]` — no natural single-payload form; deferred unless a future use case demands it.

## Open decision points

All locked in by this issue body, but recorded for the record:

1. **State storage**: hybrid — archetype monitors use table-pair check (upstream pattern); sparse/mixed monitors use per-monitor matched set (option A fallback). Diverges from the user's prompt which proposed option A unconditionally; matches upstream's design where possible while preserving correctness for sparse / DontFragment / Union storage which upstream's table-pair check would not cover in Go-flecs's split-storage model.
2. **Marshal**: monitor registrations and matched sets are process-local; not serialized. Application code re-registers monitors with `WithYieldExisting()` post-unmarshal to reconstitute. Matches existing observer behavior.
3. **Disabled monitor state**: do NOT update matched set while disabled; re-enable computes truth fresh from next event. Matches Phase 16.6 'no catch-up' semantic.
4. **Initial-state observation**: `WithYieldExisting()` is the only retroactive trigger. Matches Phase 16.5 pattern.

## Notes for the implementing agent

- Phase number: **16.10** per user request, despite ROADMAP line 106 currently listing Monitor observers as a Phase 16.11 candidate. Implementer should also remove the line 106 entry when adding to the Shipped section.
- ROADMAP line 106 currently misdescribes monitors as 'fire once when a query transitions from no matches to has matches'. Upstream is **per-entity** (fire when an *entity* starts or stops matching). The implementer should correct the ROADMAP wording.
- Target version: **v0.65.0** (next after v0.64.0).
