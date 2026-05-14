# Go flecs Documentation

Conceptual documentation for the Go flecs ECS library. Start with the [Quickstart](Quickstart.md).

---

## Getting Started

- **[Quickstart](Quickstart.md)** ✅ — hello-world walkthrough: world, entities, components, queries, hierarchies, prefabs, systems, observers.

---

## Manuals (pending)

| File | Status | Phase |
|---|---|---|
| [EntitiesComponents.md](EntitiesComponents.md) | ✅ landed | 14.1 |
| [Queries.md](Queries.md) | ✅ landed | 14.2 |
| [Relationships.md](Relationships.md) | ✅ landed | 14.3 |
| [HierarchiesManual.md](HierarchiesManual.md) | ✅ landed | 14.4 |
| [PrefabsManual.md](PrefabsManual.md) | ✅ landed | 14.5 |
| [Systems.md](Systems.md) | ✅ landed / 14.6 | 14.6 |
| [ObserversManual.md](ObserversManual.md) | ✅ landed / 14.7 | 14.7 |
| [ComponentTraits.md](ComponentTraits.md) | ✅ landed / 14.8 | 14.8 |
| [FlecsRemoteApi.md](FlecsRemoteApi.md) | ✅ landed / 14.9 | 14.9 |
| [DesignWithFlecs.md](DesignWithFlecs.md) | ✅ landed / 14.10 | 14.10 |
| [Manual.md](Manual.md) | ✅ landed / 14.11 | 14.11 |
| [FAQ.md](FAQ.md) | ✅ landed / 14.12 | 14.12 |

---

## Survey Table (Phase 14.0)

Each C doc was read and classified for porting effort.

| C filename | ~words | Classification | Go filename | Effort |
|---|---|---|---|---|
| `Quickstart.md` | 6 500 | port-adapted | `docs/Quickstart.md` | medium |
| `EntitiesComponents.md` | 7 000 | port-adapted | `docs/EntitiesComponents.md` | large |
| `Queries.md` | 19 000 | port-adapted | `docs/Queries.md` | large |
| `Relationships.md` | 6 400 | port-adapted | `docs/Relationships.md` | large |
| `HierarchiesManual.md` | 3 900 | port-adapted | `docs/HierarchiesManual.md` | medium |
| `PrefabsManual.md` | 3 100 | port-adapted | `docs/PrefabsManual.md` | medium |
| `Systems.md` | 6 600 | port-adapted | `docs/Systems.md` | large |
| `ObserversManual.md` | 8 600 | port-adapted | `docs/ObserversManual.md` | large |
| `ComponentTraits.md` | 7 400 | port-with-gaps | `docs/ComponentTraits.md` | medium |
| `FAQ.md` | 1 500 | port-adapted | `docs/FAQ.md` | small |
| `DesignWithFlecs.md` | 3 200 | port-as-is | `docs/DesignWithFlecs.md` | small |
| `Manual.md` | 2 200 | port-adapted | `docs/Manual.md` | medium |
| `FlecsRemoteApi.md` | 4 900 | port-with-gaps | `docs/FlecsRemoteApi.md` | medium |
| `BuildingFlecs.md` | 1 800 | skip — replace with Go module section in README | — | — |
| `MigrationGuide.md` | 2 200 | skip — C version migration, irrelevant to Go | — | — |
| `FlecsScript.md` | 7 200 | skip — C DSL not ported to Go | — | — |
| `FlecsScriptTutorial.md` | 4 200 | skip — C DSL not ported to Go | — | — |
| `FlecsQueryLanguage.md` | 2 700 | skip — C DSL not ported to Go | — | — |
| `Docs.md` | 400 | skip — becomes this file | — | — |

### Classification notes

- **port-as-is** — conceptual content maps cleanly; only code examples need Go syntax.
- **port-adapted** — needs Go syntax, Go idioms, and ergonomics rewrite throughout.
- **port-with-gaps** — describes features the Go port does not have; gaps annotated in the stub.
- **skip** — C-specific tooling, build system, DSL, or migration content that has no Go equivalent.

### Feature-gap list (candidate follow-up issues)

Features described in the C docs that the Go port does not currently implement:

- **Query language / DSL** (`FlecsScript`, `FlecsQueryLanguage`) — C-only scripting layer.
- **Module system** (`ECS_MODULE` / `world.import`) — Go packages serve this role natively.
- **Entity hooks beyond OnAdd/OnSet/OnRemove** — C has `OnDelete`, `OnTableEmpty`, `OnTableFill`. `OnTableCreate` observer event **shipped in v0.62.0**; `OnTableDelete` deferred pending table-reclamation.
- **Cleanup policies** (`OnDeleteTarget`, `OnDelete`) — **shipped in v0.32.0** via `SetCleanupPolicy` / `GetCleanupPolicy`.
- **Reflection / meta cursor** (`ecs_meta_cursor`, `ecs_meta_type_op`) — not ported.
- **Sparse storage** (`EcsSparse` component trait opt-in) — **fully shipped in v0.52.0** — storage, write/read/remove API, and query integration (all-sparse, mixed, and all-archetype iterator modes; `Field[T]`/`FieldMaybe[T]` sparse branches; `Not`/`Optional` on sparse terms; `CachedQuery.Changed()` via version counter). See [ComponentTraits.md § Sparse](ComponentTraits.md#sparse) and [Queries.md § Sparse-aware queries](Queries.md#sparse-aware-queries).
- **World-level pre/post merge hooks** — not ported.
- **Alerts addon** — not ported.
- **Monitor addon** — not ported.
- **Units addon** — not ported.
- **Query groups** — not ported.
- **Acyclic relationship trait** (`EcsAcyclic`) — **shipped in v0.41.0** via `SetAcyclic` / `IsAcyclic` / `w.Acyclic()`. Write-time cycle rejection at `AddID`; `ChildOf` bootstrapped acyclic (deliberate divergence from C's lookup-time guards). See [ComponentTraits.md § Acyclic](ComponentTraits.md#acyclic).
- **Transitive relationships** (`EcsTransitive`, `Trav` flag) — **shipped in v0.37.0** via `SetTransitive` / `IsTransitive` / `w.Transitive()`. Query terms for `(R, C)` where R is transitive walk the chain automatically at query time. See the [Transitive section in ComponentTraits.md](ComponentTraits.md#transitive).
- **Symmetric relationships** (`EcsSymmetric`) — **shipped in v0.36.0** via `SetSymmetric` / `IsSymmetric` / `w.Symmetric()`. Adding `(R, B)` to `A` automatically mirrors `(R, A)` to `B`; removal is mirrored too. See the [Symmetric section in ComponentTraits.md](ComponentTraits.md#symmetric).
- **Exclusive relationships** (`EcsExclusive`) — **shipped in v0.34.0** via `SetExclusive` / `IsExclusive` / `w.Exclusive()`. `ChildOf`, `OnDelete`, `OnDeleteTarget`, `OnInstantiate` bootstrapped exclusive; `IsA` not exclusive.
- **Union relationships** (`EcsUnion`) — **shipped in v0.54.0** via `SetUnion(w, relID)` / `IsUnion(scope, relID)` / `EachUnion(scope, relID, fn)`. At-most-one-target per relationship per entity; stored in a per-relationship side map — no archetype fragmentation. See [ComponentTraits.md § Union](ComponentTraits.md#union).
- **Wildcard / Any queries** (`EcsWildcard`, `EcsAny` as query terms) — **shipped in v0.38.0** via `w.Wildcard()`, `w.Any()`, `MatchedTarget`, `MatchedID`, `FieldByMatch`. See [`docs/Queries.md`](Queries.md#wildcard-and-any-query-terms-phase-156-v0380).
- **World snapshots** (beyond JSON serialization) — not ported.
- **Entity scoping** (`ecs_set_scope` / push-pop) — not ported.
- **Singleton API shortcuts** (`world.set<T>`, `world.get<T>`) — **shipped in v0.44.0** via `SetSingleton` / `IsSingleton` / `SingletonEntity` / `Singleton[T]` / `WriteSingleton[T]`. Go semantic: at most one holder (vs. C must-be-self). See [ComponentTraits.md#singleton](ComponentTraits.md#singleton).
- **Timer addon** (independent rate control per system) — partial (`timer.go` exists; full addon API pending).
- **REST explorer** (full `FlecsExplorer` integration) — minimal read-only handler only.

These are listed for operator prioritization; no follow-up issues were filed in Phase 14.0.

### Additional gaps discovered in Phase 14.1 (EntitiesComponents port)

- **`Clear(e)`** — ✅ **shipped in v0.56.0**. Removes all components from an entity without deleting it; fires `OnRemove` per component; deferred coalescer support.
- **`MakeAlive(id)`** — ✅ **shipped in v0.56.0**. Claims a specific entity ID (e.g. for networked ID synchronisation); panics on generation conflict or inside deferred scope.
- **`SetVersion(versionedID)`** — ✅ **shipped in v0.56.0**. Overrides the generation counter on an entity (monotonic; panics on decrease or inside deferred scope).
- **Entity ID ranges** (`range_new` / `range_set`) — constrain which IDs `NewEntity` issues; enables per-owner ID partitioning. not yet ported in Go flecs.
- **Entity disabling** (`DisableEntity` / `EnableEntity`) — ✅ **shipped in v0.57.0** via [`DisableEntity`](../query_filters.go) / `EnableEntity` / `IsDisabled` and `w.Disabled()`. Ordinary queries silently exclude entities carrying the `Disabled` tag; opt in by adding `With(w.Disabled())` (or any other term kind mentioning the tag). See [Queries.md § Disabled and Prefab entities](Queries.md#disabled-and-prefab-entities).
- **`on_replace` hook** — ✅ **shipped in v0.55.0** as `OnReplace[T]` / `OnReplaceID`. Receives both the previous and new component value when a component is overwritten via `Set`. Fires only on overwrites (not on first Set). See [ObserversManual.md § OnReplace Hook](ObserversManual.md#onreplace-hook).
- **Runtime (dynamic) component registration** — register a component whose Go type is unknown at compile time (only size + alignment known; used by scripting layers). not yet ported in Go flecs.
- **Cleanup policies / component-delete cascade** — **shipped in v0.32.0** via `SetCleanupPolicy` / `GetCleanupPolicy`. The `OnDelete` and `OnDeleteTarget` traits are now fully configurable with `RemoveAction`, `DeleteAction`, and `PanicAction`.
- **`CanToggle` component trait** — **shipped in v0.35.0** via `SetCanToggle` / `EnableID` / `DisableID` / `IsEnabledID` and typed generics. `Each1`/`Each2`/`Each3`/`Each4` automatically skip disabled rows. See the [ComponentTraits manual](ComponentTraits.md#cantoggle).

### Additional gaps discovered in Phase 14.2 (Queries port)

- **Fixed per-term source** — a query term can specify any entity as its source (e.g., match `SimTime` on a global `Game` entity rather than the iterated entity). Go flecs only supports the default `$this` source. not yet ported in Go flecs.
- **Query variables** — `$Var` named variables in the Flecs Query Language constrain results across related entities (e.g., "spaceships docked to a planet"). not yet ported in Go flecs.
- **Sorted queries** — ✅ **shipped in v0.59.0** via `NewCachedQueryFromTermsWithOptions` + `WithOrderBy`. `OrderBy[T]` for typed comparators; `OrderByFunc` for raw pointer form. Cached; lazily re-sorted on table `ChangeCount` changes or when new matching tables are added. Cached queries only (sorting a non-cached query would re-sort on every iteration). See [Queries.md § Sorted queries](Queries.md#sorted-queries).
- **Query groups** — ✅ **shipped in v0.66.0** via `NewCachedQueryFromTermsWithOptions` + `WithGroupBy`. `GroupByFunc` partitions matched tables; `IterGroup` for O(1) group access; `WithGroupBy` + `AndOrderBy` compose (sort within each group). `Cascade` retains its dedicated implementation; refactor deferred. See [Queries.md § Query groups](Queries.md#query-groups).
- **Equality operators** — `$this == Foo`, `$this != Foo`, `$this ~= "partial"` name-match filter terms in the Flecs Query Language. not yet ported in Go flecs.
- **AndFrom / OrFrom / NotFrom operators** — expand the component list of a given entity into implicit AND / OR / NOT terms, useful with prefab type-lists. not yet ported in Go flecs.
- **Query scopes** — `scope_open` / `scope_close` negate a sub-expression of arbitrary terms (e.g., `Position, !{ Velocity || Speed }`). not yet ported in Go flecs.
- **Access modifiers on query terms** — `In` / `InOut` / `Out` / `None` per-term annotations used by the C scheduler for pipeline sync-point inference. Go flecs governs mutation via `Read`/`Write` world scopes; per-term annotations are not ported.

### Additional gaps discovered in Phase 14.3 (Relationships port)

- **Exclusive relationship trait** (`EcsExclusive`) — **shipped in v0.34.0**. `SetExclusive(w, relID)` / `IsExclusive(w, relID)` / `w.Exclusive()`. Built-in relationships `ChildOf`, `OnDelete`, `OnDeleteTarget`, `OnInstantiate` are exclusive by default. `IsA` is not exclusive.
- **Symmetric relationship trait** (`EcsSymmetric`) — **shipped in v0.36.0**. `SetSymmetric(w, relID)` / `IsSymmetric(w, relID)` / `w.Symmetric()`. Adding `(R, B)` to `A` automatically mirrors `(R, A)` to `B`; removal is mirrored too. See [ComponentTraits.md § Symmetric](ComponentTraits.md#symmetric).
- **Transitive relationship trait** (`EcsTransitive`) — **shipped in v0.37.0**. `SetTransitive(w, relID)` / `IsTransitive(w, relID)` / `w.Transitive()`. Queries for `(R, C)` walk the `(R, *)` chain lazily at query time; cycle-safe and bounded. See [ComponentTraits.md § Transitive](ComponentTraits.md#transitive).
- **Traversable relationship trait** (`EcsTraversable`) — **shipped in v0.46.0**. `SetTraversable(w, relID)` / `IsTraversable(scope, relID)` / `w.Traversable()`. Query-time enforcement: non-traversable relationships panic when used with `.Up()`/`.SelfUp()`/`.Cascade()`. Traversable implies Acyclic. `ChildOf` and `IsA` bootstrapped Traversable. See [ComponentTraits.md § Traversable](ComponentTraits.md#traversable).
- **Relationship / Target / Trait usage constraints** (`EcsRelationship`, `EcsTarget`, `EcsTrait`) — **shipped in v0.47.0**. `SetRelationship(w, id)` / `SetTarget(w, id)` / `SetTrait(w, id)` and corresponding `Is*` query functions plus `w.Relationship()` / `w.Target()` / `w.Trait()` bare-tag accessors. Write-time enforcement panics on constraint violations. `Trait` exempts an entity from `Relationship`'s no-target-slot check. Built-ins bootstrapped: `IsA`, `ChildOf`, `OnDelete`, `OnDeleteTarget`, `OnInstantiate` → Relationship; `Override`, `Inherit`, `DontInherit` → Target; `IsA`, `ChildOf` → Trait. See [ComponentTraits.md § Relationship / Target / Trait](ComponentTraits.md#relationship--target--trait).
- **Configurable cleanup policies** (`OnDelete` / `OnDeleteTarget`) — **shipped in v0.32.0**. `SetCleanupPolicy` / `GetCleanupPolicy` with `DeleteAction`, `RemoveAction`, and `PanicAction`. `ChildOf` is bootstrapped with `(OnDeleteTarget, DeleteAction)`. See [Relationships.md § Cleanup policies](Relationships.md).
- **PairIsTag trait** (`EcsPairIsTag`) — **shipped in v0.48.0**. `SetPairIsTag(w, relID)` / `IsPairIsTag(scope, relID)` / `w.PairIsTag()`. Forces a relationship's pairs to behave as tags; `SetPair[T]`/`SetPairByID` panic on a marked relationship. `IsA` and `ChildOf` bootstrapped. See [ComponentTraits.md § PairIsTag](ComponentTraits.md#pairistag).
- **Entity scoping** (`ecs_set_scope` / `ecs_get_scope`) — push/pop a parent scope so that all subsequently created entities automatically receive a `(ChildOf, scope)` pair without explicit `AddID` calls. not yet ported in Go flecs.

### Additional gaps discovered in Phase 14.4 (HierarchiesManual port)

- **`OrderedChildren` trait** — **shipped in v0.50.0**. `SetOrderedChildren(w, parentID)` / `IsOrderedChildren(scope, parentID)` / `w.OrderedChildren()`. Opt-in per parent; `EachChild` and `Reader.EachChild` iterate in insertion order. JSON round-trip via `ordered_children` field. See [HierarchiesManual.md § OrderedChildren](HierarchiesManual.md#orderedchildren).
- **`Parent` hierarchy storage** — a second, non-fragmenting storage for small structured hierarchies where children of multiple parents share the same archetype table. Reduces table fragmentation and memory footprint for prefab-heavy workloads. not yet ported in Go flecs.

### Additional gaps discovered in Phase 14.5 (PrefabsManual port)

- **Prefab tag** (`EcsPrefab`) — ✅ **shipped in v0.57.0** via `MarkPrefab` / `IsPrefab` / `w.Prefab()`. Ordinary queries silently exclude entities carrying the `Prefab` tag; the tag is bootstrapped with `DontInherit` so IsA instances do not acquire it. See [PrefabsManual.md § Prefab tag](PrefabsManual.md#prefab-tag) and [Queries.md § Disabled and Prefab entities](Queries.md#disabled-and-prefab-entities).
- **Prefab hierarchies** — when a prefab has `(ChildOf, prefab)` children, instantiating the prefab replicates the entire child subtree onto the instance. not yet ported in Go flecs.
- **Prefab slots** (`SlotOf`) — `(SlotOf, prefab)` on a prefab child creates a named slot relationship on the instance that resolves to the copied child in O(1) without a name lookup. not yet ported in Go flecs.

### Additional gaps discovered in Phase 14.6 (Systems port)

- **Custom pipeline phases** — ✅ **shipped in v0.64.0** via `NewPhase(w, name)` + `(*Phase).DependsOn(other)`. Any `*Phase` can be created and ordered relative to built-in phases or other custom phases. Kahn's topological sort runs lazily at `Progress` time. Orphan custom phases (no `DependsOn` edge) panic on first tick. See [Systems.md § Custom Pipeline Phases](Systems.md#custom-pipeline-phases).
- **DependsOn ordering between systems** — ✅ **shipped in v0.64.0** via `(*System).DependsOn(other)`. Overrides registration order within a phase; panics if systems are in different phases. Kahn sort with registration-order tiebreaking. See [Systems.md § System DependsOn Ordering](Systems.md#system-dependson-ordering).
- **System disabling** (`ecs_enable` / `EcsDisabled`) — ✅ **shipped in v0.58.0** via `(*System).SetEnabled(bool)` / `(*System).IsEnabled() bool`. `Progress` skips disabled systems; `RunSystem` ignores the flag. See [Systems.md § Disabling a System](Systems.md#disabling-a-system).
- **Rate filters** (`SetInterval` / `SetRate`) — ✅ **shipped in v0.61.0** via `(*System).SetInterval(d time.Duration)` / `(*System).SetRate(n int32)`. Interval uses subtract-with-cap accumulator; rate uses modulo counter; both gates compose with AND semantics (diverges from upstream which forbids the combination). See [Systems.md § Rate Filters](Systems.md#rate-filters).
- **Single-system `Run` out-of-pipeline** — ✅ **shipped in v0.58.0** via `RunSystem(s *System, dt float32)`. Bypasses pipeline phase ordering; mutations deferred and flushed before return. See [Systems.md § Single-system Run](Systems.md#single-system-run-out-of-pipeline).
- **`RunWorker` / explicit thread dispatch** — `ecs_run_worker` for manual entity-range partitioning outside the pipeline. not yet ported in Go flecs.
- **Pipeline introspection** — ✅ **shipped in v0.58.0** via `(*Reader).Phases() []ID`, `(*Reader).SystemsInPhase(phase ID) []*System`, and `(*Reader).EachSystem(phase ID, fn func(*System) bool)`. See [Systems.md § Pipeline Introspection](Systems.md#pipeline-introspection).

### Additional gaps discovered in Phase 14.7 (ObserversManual port)

- **`OnReplace` hook** — ✅ **shipped in v0.55.0** as `OnReplace[T]` / `OnReplaceID`. Fires when `Set` overwrites an existing component value; receives both the old and new value. See [ObserversManual.md § OnReplace Hook](ObserversManual.md#onreplace-hook).
- **`OnDelete` / `OnDeleteTarget` cleanup-policy traits** — ✅ **shipped in v0.32.0** (Phase 15.0). These are **not** observer events — upstream (`flecs.h:1950–1955`) documents them strictly as *cleanup-policy relationship traits* used as the first element of a pair (e.g. `(OnDelete, Delete)`). No C code path emits observer callbacks for them. The Go port exposes them as built-in trait entities (`world.go:63–64`, `OnDelete()` / `OnDeleteTarget()`) and implements the full policy machinery in `cleanup.go`. For the observer-side of deletion — reacting when an entity or component is removed — use `EventOnRemove`, which fires before each component is removed during entity deletion (per upstream `ObserversManual.md:2273`; see `observer.go:17`). See [ComponentTraits.md § Cleanup traits (OnDelete / OnDeleteTarget)](ComponentTraits.md#cleanup-traits-ondelete--ondeletetarget) for the cleanup-policy API surface.
- **`OnTableCreate` event** — ✅ **shipped in v0.62.0** via `OnTableCreate(w, fn)` / `OnTableCreateWithOptions(w, opts, fn)`. Fires once per archetype table when first created; does not fire for the empty root table. See [ObserversManual.md § OnTableCreate](ObserversManual.md#on-table-create).
- **`OnTableDelete` event** — deferred pending table-reclamation infrastructure. Go-flecs tables persist for the World lifetime; reclamation is a substantial independent change required before `OnTableDelete` can be wired.
- **`OnTableEmpty` / `OnTableFill` events** — fire when an archetype table transitions between empty and non-empty. not yet ported in Go flecs.
- **Custom events** — ✅ **shipped in v0.63.0** via `RegisterEvent(fw, name)`, `Emit(fw, eventID, entity, payload)`, `EmitTyped[T]`, `ObserveEvent(w, eventID, fn)`, `ObserveEventTyped[T]`. Events are entities; four built-in event entities exposed as `w.EventOnAdd()` / `w.EventOnSet()` / `w.EventOnRemove()` / `w.EventOnTableCreate()`; fifth built-in `w.Event()` tags event entities for discriminability. Dispatch table now keys on event entity IDs (built-in and custom share the same path). `EventKind` enum preserved as a convenience layer. See [ObserversManual.md § Custom Events](ObserversManual.md#custom-events).
- **Term-set observer filters (multi-term observers)** — C observers can match a query with multiple terms (e.g., "fire when Position is set but only if entity also has Velocity"). Go flecs observers subscribe to a single component at a time. not yet ported in Go flecs.
- **Yield-on-create** — `yield_existing` flag retroactively fires an observer for entities already matching the query at registration time. ✅ **shipped in v0.60.0** via `ObserveWithOptions[T]` + `WithYieldExisting()`. See [ObserversManual.md § yield_existing](ObserversManual.md#yield-existing).
- **Observer propagation / forwarding** — events propagate along relationship edges (e.g., `OnSet(Position)` on a parent notifies children inheriting `Position`). not yet ported in Go flecs.
- **Monitor observers** — ✅ **shipped in v0.65.0** via `Monitor(w, terms, fn)` / `MonitorWithOptions(w, terms, opts, fn)`. Fires `fn(fw, e, entered bool)` on query-match entry/exit; multi-term; yield_existing; supports DontFragment and Union terms. See [ObserversManual.md § Monitor Observers](ObserversManual.md#monitor-observers).
- **Observer disabling** — pause an observer without removing it (analogous to system disabling). ✅ **shipped in v0.60.0** via `(*Observer).SetEnabled(bool)` / `(*Observer).IsEnabled() bool`. See [ObserversManual.md § Disabling an Observer](ObserversManual.md#disabling-an-observer).
- **Fixed-source observer terms** — observer terms that match a component on a specific entity (not `$this`). not yet ported in Go flecs.

### Additional gaps discovered in Phase 14.8 (ComponentTraits port)

- **`Constant` component trait** — marks a component read-only after its initial write; subsequent `Set` calls would be a fatal error. not yet ported in Go flecs.
- **`DontFragment` component trait** — opt a component into non-fragmenting sparse storage; entity does not transition archetype tables on add/remove. **Shipped in v0.53.0** via `SetDontFragment(w, cid)` / `IsDontFragment(scope, cid)` / `w.DontFragment()`. See [ComponentTraits.md § DontFragment](ComponentTraits.md#dontfragment).
- **`Singleton` component trait** (`EcsSingleton`) — **shipped in v0.44.0** via `SetSingleton` / `IsSingleton` / `SingletonEntity` / `Singleton[T]` / `WriteSingleton[T]`. At-most-one-holder semantic (deliberately different from C must-be-self). See [ComponentTraits.md § Singleton](ComponentTraits.md#singleton).
- **`Union` relationship trait** — union-pair semantics: only one of several targets may be active for a given relationship on an entity; stored to minimise table fragmentation. **Shipped in v0.54.0** via `SetUnion(w, relID)` / `IsUnion(scope, relID)` / `EachUnion(scope, relID, fn)`. See [ComponentTraits.md § Union](ComponentTraits.md#union).
- **`Final` entity trait** — **shipped in v0.42.0** via `SetFinal(w, entityID)` / `IsFinal(scope, entityID)` / `w.Final()`. Write-time enforcement: adding `(IsA, target)` panics if target is Final. See [ComponentTraits.md § Final](ComponentTraits.md#final) and [PrefabsManual.md § Sealing prefabs with Final](PrefabsManual.md#sealing-prefabs-with-final).
- **`OneOf` relationship trait** — constrains relationship targets to be children of a specified entity (useful for enum-style relationships). **Shipped in v0.43.0** via `SetOneOf(w, relID, parentID)` / `IsOneOf(scope, relID)` / `w.OneOf()`. Write-time enforcement: adding `(R, target)` panics if target is not a direct child of the required parent. See [ComponentTraits.md § OneOf](ComponentTraits.md#oneof).
- **`With` relationship** — automatically co-adds a second component when a component is added; when added to a relationship, the co-added id mirrors the pair target. **Shipped in v0.49.0** via `SetWith(w, source, coAdd)` / `HasWith(scope, source) []ID` / `w.With()`. See [ComponentTraits.md § With](ComponentTraits.md#with).
- **`Relationship` / `Target` / `Trait` enforcement traits** — ✅ **shipped in v0.47.0** via `SetRelationship(w, id)` / `SetTarget(w, id)` / `SetTrait(w, id)`. Write-time enforcement panics on constraint violations. See [ComponentTraits.md § Relationship / Target / Trait](ComponentTraits.md#relationship--target--trait).

### Additional gaps discovered in Phase 14.9 (FlecsRemoteApi port)

- **Query execution endpoint** (`GET /query?expr=`) — evaluates a Flecs Query Language expression over HTTP and returns matched entities with field values. Requires the FlecsQueryLanguage DSL and `ecs_iter_to_json`; not yet ported in Go flecs.
- **Entity / component mutation endpoints** (`PUT /entity`, `DELETE /entity`, `PUT /component`, `DELETE /component`) — create, delete, and mutate entities and components via REST. Require the reflection / meta-cursor API (`ecs_meta_cursor`); not yet ported in Go flecs.
- **Toggle endpoint** (`PUT /toggle`) — enable/disable an entity or a per-component enable bit via REST. Requires entity disabling (`Disabled` tag) and the `CanToggle` component trait; not yet ported in Go flecs.
- **Aggregated stats (FlecsStats module)** — `GET /stats/world` and `GET /stats/pipeline` return multi-period aggregated statistics collected by the `FlecsStats` addon. FlecsStats module not yet ported in Go flecs.
- **Type-info / reflection endpoint** (`GET /type_info/<path>`) — returns the reflection schema for a component type. Requires the meta-cursor module (`ecs_meta_cursor`); not yet ported in Go flecs.
- **FlecsExplorer integration** — the browser-based world inspector at `https://flecs.dev/explorer` connects via the C REST API and requires entity mutation, query execution, and type-info endpoints. Not integrated with the Go REST handler.
