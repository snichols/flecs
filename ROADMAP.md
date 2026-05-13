# Roadmap

## Shipped (through v0.47.0)

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
- **Configurable cleanup policies** _(v0.32.0)_ — `OnDelete` and `OnDeleteTarget` trait relationships with `RemoveAction`, `DeleteAction`, and `PanicAction`. `SetCleanupPolicy` / `GetCleanupPolicy` public API. Pair-add path (`AddID(relID, MakePair(w.OnDeleteTarget(), w.DeleteAction()))`) is first-class. `ChildOf` cascade-delete is now driven by the general mechanism (`(ChildOf, OnDeleteTarget, DeleteAction)` bootstrap). `IsA` has no default policy — opt-in recipe documented in `docs/PrefabsManual.md`.
- **Configurable OnInstantiate policies** _(v0.33.0)_ — `Override` (eager copy at IsA-add time), `DontInherit` (suppresses IsA chain walk and query auto-promotion), and `Inherit` (explicit default). `SetInstantiatePolicy` / `GetInstantiatePolicy` public API. Pair-add form (`AddID(cid, MakePair(w.OnInstantiate(), w.Override()))`) is first-class. Multi-level IsA chains handled transitively. DontInherit takes precedence over `SetInheritable[T]`.
- **Exclusive relationship trait** _(v0.34.0)_ — `SetExclusive(w, relID)` / `IsExclusive(w, relID)` / `w.Exclusive()`. Adding a second target for an exclusive relationship replaces the first (OnRemove + OnAdd fire correctly). Built-in relationships `ChildOf`, `OnDelete`, `OnDeleteTarget`, `OnInstantiate` are bootstrapped exclusive. `IsA` is not exclusive. Built-in entity count: 17.
- **CanToggle component trait** _(v0.35.0)_ — `SetCanToggle(w, componentID)` / `IsCanToggle` / `w.CanToggle()`. `EnableID` / `DisableID` / `IsEnabledID` + typed `Enable[T]` / `Disable[T]` / `IsEnabled[T]` generics. Per-row bitset storage on `table.Table`; lazy allocation on first `DisableRow`. `Each1`/`Each2`/`Each3`/`Each4` skip disabled rows automatically. Disabled state survives archetype migration. Built-in entity count: 18.
- **Symmetric relationship trait** _(v0.36.0)_ — `SetSymmetric(w, relID)` / `IsSymmetric(w, relID)` / `w.Symmetric()`. Adding `(R, B)` to `A` automatically mirrors `(R, A)` to `B`; removal is mirrored too. Loop guard via `HasComponent` idempotence; self-pairs handled naturally. Composes with Exclusive: each side's exclusivity is enforced independently when both traits are active. Built-in entity count: 19.
- **Transitive relationship trait** _(v0.37.0)_ — `SetTransitive(w, relID)` / `IsTransitive(w, relID)` / `w.Transitive()`. Query terms for `(R, C)` walk the `(R, *)` chain lazily at query time; cycle-safe, depth-bounded (64 hops). Cached queries pre-evaluate at construction and on table-create; pair-mutation staleness accepted. Composes with Symmetric: both traits can be active on the same relationship. Built-in entity count: 20.
- **Wildcard and Any query-term sentinels** _(v0.38.0)_ — `w.Wildcard()` (`*`, index 22) and `w.Any()` (`_`, index 23). `MakePair(R, w.Wildcard())` emits one iterator row per concrete `(R, X)` pair in the table; `MakePair(R, w.Any())` matches once per entity. `MatchedTarget(it, termIdx)` / `MatchedID(it, termIdx)` / `FieldByMatch[T](it, termIdx)` accessors. Works in both live queries and `CachedQuery`.
- **Reflexive relationship trait** _(v0.39.0)_ — `SetReflexive(w, relID)` / `IsReflexive(w, relID)` / `w.Reflexive()` (index 21). `R(X, X)` is implicitly true for all X without storing an explicit self-pair. `HasID(e, MakePair(R, e))` returns `true` (deliberate extension of C semantics; documented in CHANGELOG). Query self-match includes the target entity's own table. Composes with Transitive: a Reflexive+Transitive query yields the starting entity and all ancestors. `IsA` bootstrapped as reflexive. Built-in entity count: 23; user entities now start at index 24.
- **Writer ⊇ Reader at free-function boundaries** _(v0.40.0, Phase 15.8)_ — unexported `scope` interface with `scopeWorld() *World`. All read free-functions (`Get`, `Has`, `Each1`–`Each4`, `HasID`, `OwnsID`, `GetUp`, `HasUp`, `TargetUp`, `PrefabOf`, `IsEnabledID`, `IsEnabled`, etc.) now accept `scope` instead of `*Reader`. `*Writer` satisfies `scope`, so `flecs.Each2[A,B](fw, ...)` compiles inside a `Write` block without `fw.AsReader()`. `AsReader()` removed (pre-1.0 breaking change).
- **Acyclic relationship trait** _(v0.41.0, Phase 15.9)_ — `SetAcyclic(w, relID)` / `IsAcyclic(w, relID)` / `w.Acyclic()` (index 22). Write-time cycle rejection: adding `(e, R, target)` panics if `target` can already reach `e` via `R`. Self-pairs allowed. `ChildOf` bootstrapped acyclic (prevents `EachChild` infinite recursion). Deliberate divergence from C's lookup-time guards; documented in CHANGELOG v0.41.0. Composes with Transitive (Acyclic at write time, Transitive at query time). Built-in entity count: 24; user entities now start at index 25.
- **Final entity trait** _(v0.42.0, Phase 15.10)_ — `SetFinal(w, entityID)` / `IsFinal(scope, entityID)` / `w.Final()` (index 23). Write-time enforcement: adding `(IsA, target)` panics if target is Final; self-pairs also rejected (matching C semantics). `IsFinal` accepts `scope` interface (Phase 15.8 convention). No built-in ships Final. Built-in entity count: 25; user entities now start at index 26.
- **OneOf relationship trait** _(v0.43.0, Phase 15.11)_ — `SetOneOf(w, relID, parentID)` / `IsOneOf(scope, relID)` / `w.OneOf()` (index 24). Write-time enforcement: adding `(R, target)` panics if target is not a direct child of the required parent. Self-tag form (`SetOneOf(w, R, R)`) and pair form (`SetOneOf(w, R, P)`) both supported. Wildcard/Any targets exempt. Composes with Exclusive: replacement target validated before atomic migration. No built-in ships OneOf. Built-in entity count: 26; user entities now start at index 27.
- **Singleton component trait** _(v0.44.0, Phase 15.12)_ — `SetSingleton(w, componentID)` / `IsSingleton(scope, componentID)` / `SingletonEntity(scope, componentID)` / `Singleton[T](scope)` / `WriteSingleton[T](fw, e, v)` / `w.Singleton()` (index 25). At-most-one-holder enforcement (deliberately different from C must-be-self; always-on vs. C's debug-only guard). Write-time panic names both the current holder and the attempted new holder. Slot released on Remove or entity delete. No built-in ships Singleton. Built-in entity count: 27; user entities now start at index 28.
- **WriteOnce component trait** _(v0.45.0, Phase 15.13)_ — `SetWriteOnce(w, componentID)` / `IsWriteOnce(scope, componentID)` / `w.WriteOnce()` (index 26). Per-(entity, component) first-write tracking: first Set records the slot; second Set panics naming the entity and component. `Add` is not a trigger — only value-writing Set calls count. Remove clears tracking (fresh Add + Set cycle starts over). Pair-form: WriteOnce on relationship R governs each (R, T) slot independently. Non-component target panics at trait-application time. No upstream C counterpart. Previously called `Constant` in the Phase 14.8 gap analysis; renamed to avoid collision with upstream `EcsConstant` (enum-value tag in meta addon). Built-in entity count: 28; user entities now start at index 29.
- **Traversable relationship trait** _(v0.46.0, Phase 15.14)_ — `SetTraversable(w, relID)` / `IsTraversable(scope, relID)` / `w.Traversable()` (index 27). Query-time enforcement: `NewQueryFromTerms` / `NewCachedQueryFromTerms` panic if a term's `Trav` is non-zero and the relationship is not registered as Traversable; message names both modifier and relationship. Traversable implies Acyclic (write-time cycle rejection). `ChildOf` and `IsA` bootstrapped Traversable (mirroring C `bootstrap.c:1063,1315-1316`). Behavior change: `IsA` is now Acyclic as a side effect; `(IsA, *)` cycles are rejected at write time instead of being caught at traversal time. Transitive→Traversable implication deferred to a follow-up phase. Built-in entity count: 29 before this release.
- **Relationship / Target / Trait usage constraints** _(v0.47.0, Phase 15.15)_ — `SetRelationship(w, id)` / `SetTarget(w, id)` / `SetTrait(w, id)` / `IsRelationship(scope, id)` / `IsTarget(scope, id)` / `IsTrait(scope, id)` / `w.Relationship()` (index 28) / `w.Target()` (index 29) / `w.Trait()` (index 30). Write-time enforcement on both immediate and deferred paths: bare-tag add panics for Relationship/Target entities; pair slot violations panic with clear messages. `Trait` exempts an entity from `Relationship`'s no-target-slot check. Bootstrap: `IsA`, `ChildOf`, `OnDelete`, `OnDeleteTarget`, `OnInstantiate` → Relationship; `Override`, `Inherit`, `DontInherit` → Target; `IsA`, `ChildOf` → Trait. Wildcard/Any not constrained (mirrors C `!ecs_id_is_wildcard` guard). Built-in entity count: 32; user entities now start at index 33.

## Documentation

Conceptual docs are ported from the upstream C flecs docs one phase at a time. Each phase ports one doc to Go idioms and verifies all code blocks compile.

**Documentation port complete (v0.31.0)** — all 13 phases (14.0–14.12) shipped across v0.19.0–v0.31.0.

| Phase | Doc | Status |
|---|---|---|
| 14.0 | Survey + `docs/` scaffold + `Quickstart.md` | ✅ shipped (v0.19.0) |
| 14.1 | `EntitiesComponents.md` | ✅ shipped (v0.20.0) |
| 14.2 | `Queries.md` | ✅ shipped (v0.21.0) |
| 14.3 | `Relationships.md` | ✅ shipped (v0.22.0) |
| 14.4 | `HierarchiesManual.md` | ✅ shipped (v0.23.0) |
| 14.5 | `PrefabsManual.md` | ✅ shipped (v0.24.0) |
| 14.6 | `Systems.md` | ✅ shipped (v0.25.0) |
| 14.7 | `ObserversManual.md` | ✅ shipped (v0.26.0) |
| 14.8 | `ComponentTraits.md` | ✅ shipped (v0.27.0) |
| 14.9 | `FlecsRemoteApi.md` | ✅ shipped (v0.28.0) |
| 14.10 | `DesignWithFlecs.md` | ✅ shipped (v0.29.0) |
| 14.11 | `Manual.md` | ✅ shipped (v0.30.0) |
| 14.12 | `FAQ.md` | ✅ shipped (v0.31.0) |

**Process rule (operator directive, Phase 14.0+):** every phase from 14.0 onward must include an explicit "update docs accordingly" deliverable. The design agent must add it to each phase brief; the review agent must verify it.

## Future Work

The following are deferred to later phases. No timeline is set; issues welcome.

### Cleanup policy extensions (Phase 15.1 candidates)
- **OnDelete component-remove cascade** — when a component entity is deleted, actively remove that component from all entities that hold it (currently orphans the pair; Remove is the default but not actively applied on the component-remove path).
- **Observer-driven OnDelete / OnDeleteTarget events** — fire observer callbacks when cleanup policies trigger, matching C's `flecs_invoke_hook` path.

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
