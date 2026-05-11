# Changelog

## Unreleased

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
