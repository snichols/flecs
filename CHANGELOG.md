# Changelog

## v0.1 (unreleased)

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
