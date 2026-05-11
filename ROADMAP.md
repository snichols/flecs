# Roadmap

## Shipped (through v0.14)

The following features are available in the current release:

- **Archetype-based storage** — entities sharing the same component set are grouped into structure-of-arrays tables; no pointer chasing during iteration.
- **Raw-ID API** — `AddID`, `RemoveID`, `HasID`, `OwnsID` for tag and pair manipulation without type parameters.
- **Generic-typed API** — `Set[T]`, `Get[T]`, `Has[T]`, `Owns[T]`, `Remove[T]`, `RegisterComponent[T]` with full type safety.
- **Ergonomic iteration** — `Each1`, `Each2`, `Each3`, `Each4` for the common 1-4 component case; `NewQuery`/`Iter`/`Field[T]` for programmatic access.
- **Cached queries** — `NewCachedQuery` pre-filters tables at construction and tracks new archetypes incrementally.
- **Pair IDs / relationships** — `MakePair` encodes (relationship, target) pairs; `SetPair[T]`/`GetPair[T]` store typed data on pairs.
- **ChildOf hierarchy** — cascade delete of parent removes all descendants recursively; `EachChild`, `ParentOf`.
- **IsA inheritance** — `Get`/`Has` walk the IsA chain transitively on a local miss; `Set` performs copy-on-write override; `Remove` restores inheritance; `PrefabOf`, `EachPrefab`.
- **Hierarchical entity names** — `SetName`, `GetName`, `Lookup`, `LookupChild`, `PathOf`; dot-separated path resolution.
- **Hooks** — single per-type hook for `OnAdd[T]`, `OnSet[T]`, `OnRemove[T]`.
- **Multi-subscriber observers** — `Observe[T]`, `ObserveID`, `Observe2[T]`; deferred unsubscribe via `Observer.Unsubscribe`.
- **Coalescing deferred command queue** — `DeferBegin`/`DeferEnd`/`Defer`; nested scopes; safe mutation during iteration. Tagged-union `cmd` structs replace closure captures; bump arena (`cmdArena`) eliminates per-op heap allocation; per-entity intrusive linked list folds all Add/Set/Remove ops for one entity into a single archetype migration. `BenchmarkDeferSingleSet`: 0 allocs/op. Port of C flecs `flecs_cmd_batch_for_entity`.
- **Systems + 4-phase pipeline** — `NewSystem`, `NewSystemInPhase`; built-in PreUpdate → OnFixedUpdate → OnUpdate → PostUpdate ordering; `Progress`; frame counter; elapsed time.
- **Fixed timestep** — `SetFixedTimestep`; accumulator-based `OnFixedUpdate` dispatch with spiral-of-death warning.
- **NOT, Optional, and OR query terms** — `With`/`Without`/`Maybe`/`Or` term constructors, `NewQueryFromTerms` / `NewCachedQueryFromTerms`, `FieldMaybe[T]` (also handles OR-group disambiguation).
- **Stats and observability** — `World.Stats()` snapshot with world-level counters, per-phase wall-clock timing from the last Progress, and per-component table/entity counts; `SystemCountInPhase` for tooling.
- **Change detection** — `(*CachedQuery).Changed()` returns true when any matching table was mutated since the last call. Per-table monotonic counter; over-reports never under-reports; zero-overhead when no cached queries exist.
- **REST API addon** — `NewRESTHandler(w)` returns an `http.Handler` exposing world inspection (stats, components, entities) and snapshot save/load over HTTP. Stdlib `net/http` only; users provide their own `*http.Server`.
- **Structured logging** — `World.SetLogger(*slog.Logger)` installs an optional `log/slog` lifecycle logger. Ten DEBUG-level event sites (entity created/deleted, component registered, table created, system added/closed, observer registered/unsubscribed, snapshot serialized/loaded). Nil-default; zero overhead on the hot path when no logger is set.
- **Opt-in parallel system dispatch** — `System.SetParallel(true)` + `World.SetWorkerCount(n)` runs parallel-safe systems in goroutines from a persistent worker pool. Systems with overlapping write sets are forced serial by the dispatcher (over-approximation; user can override via `System.SetWriteSet`). Defer queue is mutex-protected so deferred mutations from parallel systems are race-free. WorkerCount=0 (default) is bit-for-bit identical to v0.9.0 single-threaded behavior.
- **Within-system multi-threaded dispatch** — `System.SetMultiThreaded(true)` + `World.SetWorkerCount(n)` splits a single system's iter across N workers, each receiving a disjoint row slice of every matched table. In-place `Field[T]` updates scale linearly with core count. Deferred mutations (Set/Delete) are safe but contend on the shared queue; a future per-stage-queue phase will lift this for linear scaling of the deferred path.
- **Readonly concurrency window** — `World.ReadonlyBegin()` / `ReadonlyEnd()` / `Readonly(fn)` opens a window during which concurrent reads from any goroutine are safe. Faithful port of C flecs `ecs_readonly_begin`/`ecs_readonly_end`: no mutex on world state; an atomic flag plus the existing deferred-command queue route all writes during the window to a buffered queue that flushes on close. Readers (`Each1`/`Each2`/`Each3`/`Each4`, `Iter`, cached `Iter`) take no locks. REST handler GETs wrap their bodies in `Readonly` automatically.
- **Exclusive-access ownership assertion** — always-on goroutine-safety check. `World.ExclusiveAccessBegin(name)` claims the world for the calling goroutine; any subsequent mutation or read from another goroutine panics with `ErrExclusiveAccessViolation`. `ExclusiveAccessEnd(lockWorld bool)` releases the claim (optionally entering a write-locked state where all goroutines receive a panic on mutation but reads still pass). Goroutine ID via `github.com/petermattis/goid`; common-case overhead is one `atomic.Load` per public method. No build tag — the check is on in every build.
- **Ancestor traversal helpers** — `GetUp[T]`, `HasUp`, `TargetUp` walk any relationship (ChildOf, IsA, custom) with cycle detection and depth limit.
- **Introspection (meta) API** — `Components`, `ComponentInfo`, `EntityComponents`, `EachEntity`, `AliveEntities` for runtime inspection without exposing internal storage.
- **Dynamic value access** — `GetByID` and `SetByID` for component reads/writes when the type is only known at runtime; honors Defer + hooks; type-safe writes.
- **JSON serialization** — `World.MarshalJSON` / `World.UnmarshalJSON` round-trip entities, names, non-pair components, ChildOf hierarchies, IsA prefabs, and custom pair components (tag-only and data-bearing). Format v1 is additive and stable. `SetPairByID` auto-registers pair data types from a `reflect.Type`.

## Future Work

The following are deferred to later phases. No timeline is set; issues welcome.

### Query extensions
- Query-term traversal modifiers (`up(rel)` inline in `NewQueryFromTerms`; the explicit `GetUp`/`HasUp`/`TargetUp` helpers cover most use cases)
- Query-time IsA inheritance (match entities whose prefab has a component)

### Addons
- (all originally-planned addons shipped)

### Concurrency
- Lock-free defer queue (currently mutex-protected)
- Per-goroutine command stages (currently a single mutex-protected queue; C flecs uses per-stage queues — porting that will unlock linear scaling for deferred mutations from multi-threaded systems)

### Performance
- Custom allocators / `sync.Pool` for additional hot paths beyond the defer queue

## Performance

A benchmark suite ships in `bench_test.go` with baseline numbers and
before/after comparisons captured in [BENCH.md](BENCH.md). Highlights of the
v0.2 hot paths:

- Iteration is O(entity-count) within each archetype table with no virtual dispatch.
- Query setup (uncached) is O(smallest-set × terms); cached queries amortize this to O(1) per Iter after construction.
- `Field[T]` is zero-allocation via `unsafe.Slice` over the column's typed backing array.
- Archetype migration on cache hit is zero-allocation (`migrate` defers signature allocation until cache miss).
- Observer dispatch is zero-allocation (deferred-removal at the node, no per-fire snapshot).
- Lifecycle hooks have zero overhead when not registered (nil-safe early return).

## Contributing

Issues are welcome at <https://github.com/snichols/flecs/issues>. PRs by
arrangement — open an issue first to discuss scope and design.
