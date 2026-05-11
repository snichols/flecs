# Roadmap

## Shipped (v0.6)

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
- **Deferred command queue** — `DeferBegin`/`DeferEnd`/`Defer`; nested scopes; safe mutation during iteration.
- **Systems + 4-phase pipeline** — `NewSystem`, `NewSystemInPhase`; built-in PreUpdate → OnFixedUpdate → OnUpdate → PostUpdate ordering; `Progress`; frame counter; elapsed time.
- **Fixed timestep** — `SetFixedTimestep`; accumulator-based `OnFixedUpdate` dispatch with spiral-of-death warning.
- **NOT, Optional, and OR query terms** — `With`/`Without`/`Maybe`/`Or` term constructors, `NewQueryFromTerms` / `NewCachedQueryFromTerms`, `FieldMaybe[T]` (also handles OR-group disambiguation).
- **Stats and observability** — `World.Stats()` snapshot with world-level counters, per-phase wall-clock timing from the last Progress, and per-component table/entity counts; `SystemCountInPhase` for tooling.
- **Ancestor traversal helpers** — `GetUp[T]`, `HasUp`, `TargetUp` walk any relationship (ChildOf, IsA, custom) with cycle detection and depth limit.
- **Introspection (meta) API** — `Components`, `ComponentInfo`, `EntityComponents`, `EachEntity`, `AliveEntities` for runtime inspection without exposing internal storage.
- **Dynamic value access** — `GetByID` and `SetByID` for component reads/writes when the type is only known at runtime; honors Defer + hooks; type-safe writes.
- **JSON serialization** — `World.MarshalJSON` / `World.UnmarshalJSON` round-trip entities, names, non-pair components, ChildOf hierarchies, IsA prefabs, and custom pair components (tag-only and data-bearing). Format v1 is additive and stable. `SetPairByID` auto-registers pair data types from a `reflect.Type`.

## Future Work

The following are deferred to later phases. No timeline is set; issues welcome.

### Query extensions
- Query-term traversal modifiers (`up(rel)` inline in `NewQueryFromTerms`; the explicit `GetUp`/`HasUp`/`TargetUp` helpers cover most use cases)
- Change-detection (delta queries)
- Query-time IsA inheritance (match entities whose prefab has a component)

### Addons
- REST API
- Stats / metrics
- Structured logging

### Concurrency
- Multi-threaded system dispatch
- Read-only concurrent query iteration

### Performance
- Custom allocators / `sync.Pool` for hot paths
- Defer queue refactor: closure capture → tagged-union or typed buffers (currently 1 alloc per deferred op)

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
