# Roadmap

## Shipped (through v0.17)

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
- **Coalescing deferred command queue** — `world.Write(func(*Writer))`; nested scopes; safe mutation during iteration. Tagged-union `cmd` structs replace closure captures; bump arena (`cmdArena`) eliminates per-op heap allocation; per-entity intrusive linked list folds all Add/Set/Remove ops for one entity into a single archetype migration. `BenchmarkDeferSingleSet`: 0 allocs/op. Port of C flecs `flecs_cmd_batch_for_entity`.
- **Systems + 4-phase pipeline** — `NewSystem`, `NewSystemInPhase`; built-in PreUpdate → OnFixedUpdate → OnUpdate → PostUpdate ordering; `Progress`; frame counter; elapsed time.
- **Fixed timestep** — `SetFixedTimestep`; accumulator-based `OnFixedUpdate` dispatch with spiral-of-death warning.
- **NOT, Optional, and OR query terms** — `With`/`Without`/`Maybe`/`Or` term constructors, `NewQueryFromTerms` / `NewCachedQueryFromTerms`, `FieldMaybe[T]` (also handles OR-group disambiguation).
- **Stats and observability** — `World.Stats()` snapshot with world-level counters, per-phase wall-clock timing from the last Progress, and per-component table/entity counts; `SystemCountInPhase` for tooling.
- **Change detection** — `(*CachedQuery).Changed()` returns true when any matching table was mutated since the last call. Per-table monotonic counter; over-reports never under-reports; zero-overhead when no cached queries exist.
- **REST API addon** — `NewRESTHandler(w)` returns an `http.Handler` exposing world inspection (stats, components, entities) and snapshot save/load over HTTP. Stdlib `net/http` only; users provide their own `*http.Server`.
- **Structured logging** — `World.SetLogger(*slog.Logger)` installs an optional `log/slog` lifecycle logger. Ten DEBUG-level event sites (entity created/deleted, component registered, table created, system added/closed, observer registered/unsubscribed, snapshot serialized/loaded). Nil-default; zero overhead on the hot path when no logger is set.
- **Opt-in parallel system dispatch** — `System.SetParallel(true)` + `World.SetWorkerCount(n)` runs parallel-safe systems in goroutines from a persistent worker pool. Systems with overlapping write sets are forced serial by the dispatcher (over-approximation; user can override via `System.SetWriteSet`). Defer queue is mutex-protected so deferred mutations from parallel systems are race-free. WorkerCount=0 (default) is bit-for-bit identical to v0.9.0 single-threaded behavior.
- **Within-system multi-threaded dispatch** — `System.SetMultiThreaded(true)` + `World.SetWorkerCount(n)` splits a single system's iter across N workers, each receiving a disjoint row slice of every matched table. In-place `Field[T]` updates scale linearly with core count. Deferred mutations (Set/Delete via `it.Writer()`) are lock-free on the hot path: each worker owns its own per-stage queue.
- **Per-stage command queues** _(Phase 12.1)_ — each worker goroutine writes deferred mutations into its own `stage.queue` with no synchronization. After `wg.Wait()`, the main goroutine merges stages in ascending id order (1…N then 0). Within-stage per-entity FIFO coalescing is preserved; no cross-stage coalescing. Hook callbacks during merge run on the main goroutine with the stage-0 `*Writer`. `BenchmarkMultiThreadedDeferredSet` shows ≥ 2x speedup on 4 workers vs 1 worker for the deferred-mutation path.
- **Scoped capability API — Reader / Writer** _(v0.15)_ — `world.Read(func(*Reader))` opens a concurrent-read window (RLock); `world.Write(func(*Writer))` opens an exclusive write scope that manages the deferred command queue. Hook and observer callbacks receive a `*Writer` for safe in-callback mutation. The legacy `Defer`/`DeferBegin`/`DeferEnd`/`Readonly`/`ReadonlyBegin`/`ReadonlyEnd` methods are removed. Faithful port of C flecs `ecs_readonly_begin`/`ecs_readonly_end` semantics: reads during a `Read` scope are lock-free; writes are buffered and flushed on scope close. REST handler GETs use `world.Read` automatically.
- **Exclusive-access ownership assertion** — always-on goroutine-safety check. `World.ExclusiveAccessBegin(name)` claims the world for the calling goroutine; any subsequent mutation or read from another goroutine panics with `ErrExclusiveAccessViolation`. `ExclusiveAccessEnd(lockWorld bool)` releases the claim (optionally entering a write-locked state where all goroutines receive a panic on mutation but reads still pass). Goroutine ID via `github.com/petermattis/goid`; common-case overhead is one `atomic.Load` per public method. No build tag — the check is on in every build. `world.Read` / `world.Write` integrate this check automatically.
- **Ancestor traversal helpers** — `GetUp[T]`, `HasUp`, `TargetUp` walk any relationship (ChildOf, IsA, custom) with cycle detection and depth limit.
- **Query-term traversal modifiers** — `With(id).Up(rel)`, `.SelfUp(rel)`, `.Cascade(rel)` inline traversal in `NewQueryFromTerms` / `NewCachedQueryFromTerms`; `IsFieldSelf` / `FieldShared[T]` accessors disambiguate local vs. inherited values. Cascade guarantees root-first table iteration order in cached queries.
- **Query-time IsA inheritance (inheritable components)** — `SetInheritable[T](w)` / `w.SetInheritable(cid)` marks a component as inheritable. Query terms for that component are auto-promoted to `Self|Up(IsA)` at construction: `Each1`/`Each2`/`NewQuery`/`NewQueryFromTerms`/`NewCachedQuery`/`NewCachedQueryFromTerms` all match entities that own the component locally AND entities that inherit it from a prefab via IsA. Explicit `.Self()` on a term suppresses auto-promotion. Value-inheritance (`Get`/`Has`, Phase 4.3) and match-inheritance (this phase) are orthogonal: Get/Has always walk the IsA chain regardless of this flag.
- **Introspection (meta) API** — `Components`, `ComponentInfo`, `EntityComponents`, `EachEntity`, `AliveEntities` for runtime inspection without exposing internal storage.
- **Dynamic value access** — `GetByID` and `SetByID` for component reads/writes when the type is only known at runtime; honors Defer + hooks; type-safe writes.
- **JSON serialization** — `World.MarshalJSON` / `World.UnmarshalJSON` round-trip entities, names, non-pair components, ChildOf hierarchies, IsA prefabs, and custom pair components (tag-only and data-bearing). Format v1 is additive and stable. `SetPairByID` auto-registers pair data types from a `reflect.Type`.

## Future Work

The following are deferred to later phases. No timeline is set; issues welcome.

### Query extensions
(all originally-planned query extensions shipped)

### Addons
- (all originally-planned addons shipped)

### Concurrency
- Lock-free defer queue (currently mutex-protected for parallel systems; per-stage queues shipped for multi-threaded systems in Phase 12.1)

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
