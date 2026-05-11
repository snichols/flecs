# Roadmap

## Shipped (v0.1)

The following features were implemented across Phases 1-7:

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

## Future Work

The following are deferred to later phases. No timeline is set; issues welcome.

### Query extensions
- NOT, Optional, OR query terms
- Up/Down traversal modifiers (query along ChildOf/IsA edges)
- Change-detection (delta queries)
- Query-time IsA inheritance (match entities whose prefab has a component)

### Addons
- Meta / reflection (runtime type introspection)
- REST API
- JSON serialization
- Stats / metrics
- Structured logging

### Concurrency
- Multi-threaded system dispatch
- Read-only concurrent query iteration

### Performance
- Custom allocators / `sync.Pool` for hot paths
- Benchmark suite (Phase 8.2)
- Zero-alloc `Field[T]` via direct column pointer (currently uses reflect)

## Performance

A formal benchmark suite is planned for Phase 8.2. Until then, performance
characteristics should be understood qualitatively: iteration is O(entity-count)
within each archetype table with no virtual dispatch; archetype migrations are
O(component-count) per entity; query setup (uncached) is O(smallest-set × terms).

## Contributing

Issues are welcome at <https://github.com/snichols/flecs/issues>. PRs by
arrangement — open an issue first to discuss scope and design.
