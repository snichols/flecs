# Changelog

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
