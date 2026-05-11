# Changelog

## v0.12.0 — 2026-05-11 — Exclusive Access Ownership Assertion

Always-on ownership assertion: every public `World` method panics with
`ErrExclusiveAccessViolation` if called from a goroutine other than the one that
called `ExclusiveAccessBegin`. No build tag required; the check is always live.

### Added

- **`(*World).ExclusiveAccessBegin(threadName string)`** — claims the world for
  the calling goroutine. Any subsequent mutation or read from a different goroutine
  panics with `ErrExclusiveAccessViolation`.
- **`(*World).ExclusiveAccessEnd(lockWorld bool)`** — releases the claim. When
  `lockWorld=true` the world enters a write-locked state where all goroutines
  receive a violation panic on mutation; reads still pass. Passing `false` returns
  the world to the unclaimed state.
- **`exclusive_access atomic.Uint64` field on `*World`** — three states:
  0 = unclaimed, goroutine ID = owned by that goroutine, ^uint64(0) = write-locked.
- **`checkExclusiveAccessWrite` / `checkExclusiveAccessRead`** — internal
  functions inserted at every public entry point. Common case (no owner claimed)
  costs one `atomic.Load` per call; `goid.Get()` only runs when an owner is set.
- **`Progress` and `RegisterComponent` / `NewSystem*` / `NewQuery*` / `NewCachedQuery*`**
  are Write-checked: any of these called from a non-owner goroutine panics with
  `ErrExclusiveAccessViolation`.
- **`IsAlive` / `Count` / `SystemCount*` / `TablesFor` / `EachTableFor`**
  are Read-checked: panics when called from a non-owner goroutine while the world
  is exclusively owned.

### Changed

- **Goroutine ID** is now obtained via `github.com/petermattis/goid` (used by
  cockroachdb, etcd, and others) instead of `runtime.Stack` parsing. Cost drops
  from ~µs to ~ns per check. No `unsafe` or fragile stack-format dependency.
- **No build tag** — the exclusive-access check is always compiled in. Go makes
  goroutines a first-class feature; the ownership assertion is on by default to
  catch misuse in any build.
- **CI** — collapsed to a single test job and a single lint job; the separate
  `-tags flecs_exclusive_access` jobs are removed (there is only one build now).

## v0.11.0 — 2026-05-11 — Readonly Concurrency Window

Faithful Go port of the C flecs readonly concurrency model (`ecs_readonly_begin` /
`ecs_readonly_end`). No mutex on world state; concurrency is enforced by an
atomic flag plus deferred-command discipline. No breaking changes.

### Added

- **`(*World).ReadonlyBegin()`** — opens a readonly window. Atomically routes all
  subsequent structural mutations (Set, Remove, Delete, AddID, RemoveID, SetPair,
  SetByID) through the deferred-command queue so that concurrent readers see a
  stable snapshot of world state.
- **`(*World).ReadonlyEnd()`** — closes the window and flushes all deferred
  mutations on the calling goroutine.
- **`(*World).Readonly(fn func())`** — convenience wrapper around
  `ReadonlyBegin`/`ReadonlyEnd` with a deferred `ReadonlyEnd` for panic-safety.
- **`readonly atomic.Bool` field on `*World`** — the flag checked by every
  mutator. One extra `atomic.Bool.Load()` per mutator on the non-deferred path
  (≈1 ns; within 2% of v0.10.0 on `BenchmarkSetExistingComponent`).

### Changed

- **All mutators** (`Delete`, `Set`, `Remove`, `AddID`, `RemoveID`, `SetPair`,
  `SetByID`) — the defer-check condition `w.deferDepth > 0` is extended to
  `w.deferDepth > 0 || w.readonly.Load()`, evaluated under `deferMu`.
- **REST GET handlers** (`/stats`, `/components`, `/components/{id}`,
  `/entities`, `/entities/{id}`, `/snapshot GET`) — bodies wrapped in
  `w.Readonly(...)` so concurrent read requests get a consistent snapshot.

### Documentation

- `doc.go`: new "Concurrency model" section explaining the readonly window
  pattern and when to use it.
- `README.md`: "Concurrency model" paragraph in the core-concepts section.

---

## v0.10.0 — 2026-05-11 — Parallel System Dispatch

Opt-in parallel system dispatch within a phase. Systems flagged as
parallel-safe run in goroutines from a persistent worker pool; systems with
overlapping write sets are forced serial. ECS storage remains non-goroutine-safe;
safety is enforced conservatively via per-system write-set conflict detection.
No breaking changes.

### Added

- **`(*System).SetParallel(bool)`** — opts a system in to parallel dispatch.
  Default: `false` (serial). Takes effect only when `WorkerCount > 0`.
- **`(*System).Parallel() bool`** — returns the current parallel flag.
- **`(*System).SetWriteSet(ids []flecs.ID)`** — declares the component IDs this
  system writes. Overrides the default (all And/Or/Optional query term IDs).
  Empty slice declares a read-only system that never conflicts.
- **`(*World).SetWorkerCount(n int)`** — sets the worker pool size. `0`
  (default) = serial dispatch; `n > 0` = persistent goroutine pool with a
  buffered channel of size `2n`. Negative panics. Changing `n` between
  `Progress` calls tears down the old pool. Calling during `Progress` is a
  no-op.
- **`(*World).WorkerCount() int`** — returns the current pool size.
- **Parallel phase dispatch** — within each phase, systems are partitioned into
  maximal contiguous batches of parallel-safe systems with pairwise-disjoint
  write sets. Each batch is dispatched via `sync.WaitGroup` before the next
  batch starts. Serial systems form single-system batches.
- **Deferred-safe parallel mutations** — `Set`, `Remove`, `Delete`, `AddID`,
  `RemoveID`, `SetPair`, `SetByID` are mutex-protected on the defer queue;
  parallel systems can safely call these without data races.

### Documentation

- `doc.go`: new "Parallel Execution" section with code snippet, conflict-
  detection explanation, and storage-not-goroutine-safe rule.
- `BENCH.md`: parallel vs serial speedup measurements for 10k-entity dispatch.

---

## v0.9.0 — 2026-05-11

Structured lifecycle logging via `log/slog`. No breaking changes.

### Added

- **`(*World).SetLogger(*slog.Logger)`** — installs or replaces the structured
  logger. Passing `nil` disables logging (the default). Documented lifecycle
  event surface: no hot-path logs.
- **`(*World).Logger() *slog.Logger`** — returns the currently installed logger,
  or `nil` if none.
- **Lifecycle log records** at DEBUG level for:
  - `entity created` / `entity deleted` (one per entity, including cascade deletes)
  - `component registered` (first `RegisterComponent[T]` call only)
  - `table created` (new archetype; `signature_len` + `signature` attrs)
  - `system added` (with `phase` attr) / `system closed`
  - `observer registered` (with `id` + `event` attrs) / `observer unsubscribed`
  - `snapshot serialized` / `snapshot loaded` (with `entities` count attr)
- Nil-logger fast path: single pointer compare at each event site; verified
  no measurable regression on `BenchmarkNewEntity` or `BenchmarkSetExistingComponent`.

---

## v0.8.0 — 2026-05-11

Minimal read-only REST API addon — exposes world inspection and snapshot
save/load over HTTP so external tools can introspect a running flecs world.
No breaking changes.

### Added

- **`NewRESTHandler(w *World) http.Handler`** — returns a configured
  `*http.ServeMux` wired to the given world. Users provide their own
  `*http.Server`. Routes:
  - `GET /stats` — world stats JSON (`Stats`)
  - `GET /components` — all registered component infos
  - `GET /components/{id}` — single component by uint64 ID (404 if not registered)
  - `GET /entities` — alive entity list; optional `?limit=N` (default 1000, max 10000; 400 if out of range)
  - `GET /entities/{id}` — entity detail: name, components, parent, prefabs, pairs (404 if dead)
  - `GET /snapshot` — full `World.MarshalJSON()` output
  - `PUT /snapshot` — load a snapshot into the world; 204 on success, 400 on parse error. **Warning**: replaces world state; not transactional.
- Routing via stdlib `http.ServeMux` with Go 1.22+ path patterns (`r.PathValue`). No external router dependency.

### Fixed

- `getViaIsA`, `hasViaIsA`, `PrefabOf`, `EachPrefab`, and `ParentOf` now return
  the zero value / false instead of panicking when called on entities whose
  archetype record has a `nil` table. Component entities allocated via
  `RegisterComponent` are not seated in the empty archetype, so their record's
  `Table` is `nil`; existing `EntityComponents` and `Get[T]` paths already
  guarded against this, but the five listed functions did not. The new REST
  endpoint `GET /entities/{component_id}` exposed this latent panic, which is
  now defensively avoided.

---

## v0.7.0 — 2026-05-11

Change detection on cached queries for delta-style systems. No breaking
changes.

### Added

- **Change detection via `CachedQuery.Changed()`** — `(*CachedQuery).Changed() bool`
  returns true when any matching table was mutated since the last call. The first call
  after construction always returns true (initial state is "all changed"). Changes detected:
  new matching table added to the cache; column write (`Set[T]`/`SetByID`/`SetPair[T]`/`SetPairByID`);
  structural change (entity added/removed via migrate). The change counter is a monotonic
  `uint64` on each `Table`; any column write marks the table dirty for all cached queries
  containing it (never under-reports, may over-report). The counter is incremented in
  `Table.Append`, `Table.RemoveSwap`, and a new `Table.BumpChange()` method called by the
  World after in-place column writes. NOT goroutine-safe. Change detection is
  cached-query-only; uncached `*Query` does not get `Changed()`.

---

## v0.6.0 — 2026-05-11

Completes the structured-term query API with OR support. No breaking
changes.

### Added

- **OR query terms** — `TermOr` (value 3) and the `Or(id)` constructor complete
  the structured-term API. Adjacent `Or` terms in a `NewQueryFromTerms` /
  `NewCachedQueryFromTerms` call form an OR-group; a table matches the group when
  it contains at least one of the group's IDs. Multiple OR-groups in one query are
  each independent. `FieldMaybe[T]` is extended to accept `TermOr` terms in
  addition to `TermOptional` — use it to disambiguate which members of an OR-group
  are present in the current table; `Field[T]` on an Or-group ID panics if the
  current table lacks it. Validation: `Or(0)` panics; duplicate IDs within an
  OR-group panic; cross-kind duplicate IDs panic (matching Phase 3.3 rules). The
  smallest-seed strategy and `CachedQuery` incremental cache maintenance are both
  Or-aware. `TermKind.String()` now returns `"Or"` for `TermOr`. Sort order for
  `TermsFull()` is: And, Not, Or-groups, Optional. No breaking changes.

---

## v0.5.0 — 2026-05-11

Stats and per-phase frame timing for tooling and observability. No breaking
changes; all existing public signatures are unchanged.

### Added

- **Stats and observability API** — `World.Stats()` returns a `Stats` snapshot
  with world-level counters (`EntityCount`, `TableCount`, `QueryCount`,
  `CachedQueryCount`, `SystemCount`, `FrameCount`, `Time`), per-phase wall-clock
  timing from the most recent `Progress` call (`LastFramePhases []PhaseStats`),
  and per-component table/entity counts (`ComponentStats []ComponentStat`).
  `PhaseStats` holds `Name`, `SystemCount`, and `Duration` for each of the four
  pipeline phases (PreUpdate[0], OnFixedUpdate[1], OnUpdate[2], PostUpdate[3]).
  `OnFixedUpdate` sums durations across all fixed-step iterations. Phases with no
  active systems report `Duration == 0`. `LastFramePhases` is nil until `Progress`
  is called at least once.
  `World.SystemCountInPhase(phase ID) int` is a convenience method for tooling;
  panics on non-built-in phase IDs (mirrors `NewSystemInPhase` validation).
  `QueryCount` is always 0 in this release (uncached queries are one-shot values
  the world does not track). No new third-party dependencies; stdlib `time` only.

---

## v0.4.0 — 2026-05-10

Complete JSON serialization: ChildOf hierarchies, IsA prefabs, and custom
pair components (data + tag-only) all round-trip. The v1 format is preserved
— all new fields are additive `omitempty`. No breaking changes.

### Added

- **Custom pair component serialization** — `World.MarshalJSON`
  now serializes custom pair components (non-ChildOf, non-IsA) into a `"pairs"`
  array on each entity. Tag-only pairs emit `{"rel":<serial>,"tgt":<serial>}`;
  data-bearing pairs add `"dataType"` (the base Go type's `reflect.Type.String()`)
  and `"data"`. `World.UnmarshalJSON` restores pairs after prefabs and before
  components: tag pairs via `AddID`, data pairs via the new `SetPairByID`.
  A new `(*World).SetPairByID(e, rel, tgt ID, v any)` method auto-registers
  the pair TypeInfo on first use and delegates to `SetByID`, firing
  hooks/observers and honoring the Defer queue. `component.RegisterPairDataByType`
  is the corresponding internal helper. ChildOf and IsA pairs continue to use
  the dedicated `parent`/`prefabs` fields and are not duplicated in `pairs`.
  v1 format unchanged (additive field). Coverage ≥ 96.4% (flecs), 100% (component).
- **IsA prefab serialization** — `World.MarshalJSON` now
  serializes IsA relationships as a `"prefabs"` array of serials (omitted when
  empty; v1 format unchanged — the field is additive). Topo-sort is generalized
  to a combined ChildOf+IsA predecessor graph so prefabs always appear before
  their instances. `World.UnmarshalJSON` restores IsA relationships after ChildOf
  and before components, preserving first-prefab-wins inheritance semantics.
  Cycle detection spans both edge kinds in a single DFS.
- **ChildOf hierarchy serialization** — `World.MarshalJSON` now
  serializes single-parent `(ChildOf, parent)` relationships as a `"parent"`
  serial field (omitted when absent; v1 format unchanged). Entities are emitted
  in topological order (parents before children) via DFS, with sibling order
  matching entity allocation order. `World.UnmarshalJSON` restores ChildOf
  relationships in a single sequential pass. Cycle detection returns a
  descriptive error rather than looping indefinitely.

---

## v0.3.0 — 2026-05-10

Introspection API, dynamic value access, and basic JSON serialization. No
breaking changes.

### Added

- **Introspection (meta) API** — `World.Components()`, `World.ComponentInfo(id)`,
  `World.EntityComponents(e)`, `World.EachEntity(fn)`, `World.AliveEntities()`.
  Public access to registered components and alive entities; no exposure of
  internal storage types.
- **Dynamic value access** — `World.GetByID(e, id) (any, bool)` and
  `World.SetByID(e, id, v any)` for component reads/writes when the type is
  only known at runtime. IsA inheritance-aware on Get; type-safety panic on
  Set with a mismatched value. Honors the Defer queue; fires hooks and
  observers like the typed paths.
- **JSON serialization** — `World.MarshalJSON()` and `World.UnmarshalJSON()`
  implement `json.Marshaler` / `json.Unmarshaler`. Saves and restores entities,
  non-pair components, and entity names. Built-in entities and pair components
  are skipped. Pair components, ChildOf hierarchies, and IsA prefabs will be
  added in subsequent 0.3.x or 0.4.x releases.

---

## v0.2.0 — 2026-05-10

Query extensions and traversal helpers. No breaking changes.

### Added

- **NOT and Optional query terms** — new structured `Term` API with
  `With(id)`, `Without(id)`, `Maybe(id)` constructors and
  `NewQueryFromTerms` / `NewCachedQueryFromTerms`. Use `FieldMaybe[T]` to
  access Optional-term columns with a presence flag. Legacy
  `NewQuery(w, ids...)` continues to produce AND-only queries with
  unchanged behavior.
- **Ancestor traversal helpers** — `GetUp[T]`, `HasUp`, `TargetUp` walk a
  relationship up from an entity and return the first match. Works for
  `ChildOf`, `IsA`, or any user-defined relationship. Cycle detection and
  64-level depth limit included. Zero allocation when the component is on
  the entity itself.

### Performance

- `BenchmarkGetUp_SelfHit`: 30 ns/op, 0 allocs/op.
- `BenchmarkGetUp_Depth1`/`Depth5`: 318/525 ns/op, 2 allocs/op (the seen-map for cycle detection).
- Optional-term presence cache is lazy-allocated; AND-only queries pay no overhead.

### Documentation

- `doc.go` extended with structured-term and traversal-helper examples.
- README feature index updated.

## v0.1.0 — 2026-05-10

Initial Go port of [flecs](https://github.com/SanderMertens/flecs). No breaking
changes from prior versions (this is the first public release).

### Added

- **Archetype-based storage** — structure-of-arrays tables keyed by sorted
  component-ID signatures; O(entity-count) iteration with no virtual dispatch.
- **Generic-typed API** — `Set[T]`, `Get[T]`, `Has[T]`, `Owns[T]`, `Remove[T]`,
  `RegisterComponent[T]`; full compile-time type safety, zero reflect at call sites.
- **Raw-ID API** — `AddID`, `RemoveID`, `HasID`, `OwnsID`, `SetPair[T]`,
  `GetPair[T]`, `MakePair`; tag and data-pair support.
- **Query API** — `NewQuery`, `NewCachedQuery`, `Field[T]`; ergonomic helpers
  `Each1` through `Each4`.
- **ChildOf hierarchy** — cascade delete; `EachChild`, `ParentOf`.
- **IsA inheritance** — transitive `Get`/`Has` on miss; copy-on-write `Set`;
  `PrefabOf`, `EachPrefab`.
- **Named entities** — `SetName`, `GetName`, `Lookup`, `LookupChild`, `PathOf`.
- **Lifecycle hooks** — `OnAdd[T]`, `OnSet[T]`, `OnRemove[T]` (one per type per event).
- **Observers** — `Observe[T]`, `ObserveID`, `Observe2[T]`; multiple subscribers
  per (component, event); deferred `Unsubscribe`.
- **Deferred commands** — `DeferBegin`/`DeferEnd`/`Defer`; nested scopes; safe
  structural mutation during iteration.
- **Systems + pipeline** — `NewSystem`, `NewSystemInPhase`; four built-in phases
  (PreUpdate, OnFixedUpdate, OnUpdate, PostUpdate); `Progress`; fixed-timestep
  accumulator; `Time`, `FrameCount`.
- Zero third-party dependencies (pure stdlib).
- >97% test coverage on the root package.

### Performance

- `Field[T]` zero-alloc fast path via `unsafe.Slice` over typed column memory.
- `unsafe.Slice` typed-slice view in queries; no `reflect.Value.Interface()` boxing.
- Observer dispatch with no per-fire snapshot allocation (deferred-removal at the node).
- Lazy `seen` map allocation in `Get[T]`/`Has[T]` IsA fallback; zero alloc on the
  common no-IsA path.
- Archetype migration zero-alloc on edge-cache hits (`migrate()` defers signature
  allocation until cache miss).
- Column logical-length tracking via internal counter; no `reflect.Value.Slice`
  allocation on `Append`/`RemoveSwap` hot paths.
- Benchmark baseline + before/after measurements captured in [BENCH.md](BENCH.md).
