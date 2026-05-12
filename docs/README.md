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
- **Entity hooks beyond OnAdd/OnSet/OnRemove** — C has `OnDelete`, `OnTableEmpty`, `OnTableFill`.
- **Cleanup policies** (`OnDeleteTarget`, `OnDelete`) — **shipped in v0.32.0** via `SetCleanupPolicy` / `GetCleanupPolicy`.
- **Reflection / meta cursor** (`ecs_meta_cursor`, `ecs_meta_type_op`) — not ported.
- **Sparse storage** (`EcsSparse` component trait opt-in) — not ported.
- **World-level pre/post merge hooks** — not ported.
- **Alerts addon** — not ported.
- **Monitor addon** — not ported.
- **Units addon** — not ported.
- **Query groups** — not ported.
- **Transitive relationships** (`EcsTransitive`, `Trav` flag) — not ported.
- **Symmetric relationships** (`EcsSymmetric`) — not ported.
- **Exclusive relationships** (`EcsExclusive`) — not ported.
- **Union relationships** (`EcsUnion`) — not ported.
- **Wildcard / Any queries** (`EcsWildcard`, `EcsAny` as query terms) — not ported.
- **OnInstantiate / Override / DontInherit traits** (full behavior) — IDs exist; behavior not fully ported.
- **World snapshots** (beyond JSON serialization) — not ported.
- **Entity scoping** (`ecs_set_scope` / push-pop) — not ported.
- **Singleton API shortcuts** (`world.set<T>`, `world.get<T>`) — achievable via `RegisterComponent` + entity ID; no dedicated API.
- **Timer addon** (independent rate control per system) — partial (`timer.go` exists; full addon API pending).
- **REST explorer** (full `FlecsExplorer` integration) — minimal read-only handler only.

These are listed for operator prioritization; no follow-up issues were filed in Phase 14.0.

### Additional gaps discovered in Phase 14.1 (EntitiesComponents port)

- **`Clear(e)`** — remove all components from an entity without deleting it; more efficient than removing one by one. not yet ported in Go flecs.
- **`MakeAlive(id)`** — claim a specific entity ID (e.g. for networked ID synchronisation). not yet ported in Go flecs.
- **`SetVersion(versionedID)`** — override the generation counter on an entity. not yet ported in Go flecs.
- **Entity ID ranges** (`range_new` / `range_set`) — constrain which IDs `NewEntity` issues; enables per-owner ID partitioning. not yet ported in Go flecs.
- **Entity disabling** (`Enable` / `Disable`) — exclude entities from queries temporarily via a `Disabled` tag without deleting them. not yet ported in Go flecs.
- **`on_replace` hook** — receives both the previous and new component value when a component is overwritten via `Set`. not yet ported in Go flecs.
- **Runtime (dynamic) component registration** — register a component whose Go type is unknown at compile time (only size + alignment known; used by scripting layers). not yet ported in Go flecs.
- **Cleanup policies / component-delete cascade** — **shipped in v0.32.0** via `SetCleanupPolicy` / `GetCleanupPolicy`. The `OnDelete` and `OnDeleteTarget` traits are now fully configurable with `RemoveAction`, `DeleteAction`, and `PanicAction`.
- **`CanToggle` component trait** — per-entity component enable/disable via `ecs_enable_component`; cheaper than remove/add because it flips a bit rather than moving the entity to another archetype. not yet ported in Go flecs.

### Additional gaps discovered in Phase 14.2 (Queries port)

- **Fixed per-term source** — a query term can specify any entity as its source (e.g., match `SimTime` on a global `Game` entity rather than the iterated entity). Go flecs only supports the default `$this` source. not yet ported in Go flecs.
- **Query variables** — `$Var` named variables in the Flecs Query Language constrain results across related entities (e.g., "spaceships docked to a planet"). not yet ported in Go flecs.
- **Sorted queries** — `order_by_callback` sorts matched entities by a component value (two-step quicksort; cached; change-detection driven). not yet ported in Go flecs.
- **Query groups** — `group_by_callback` partitions the query cache into labelled groups with O(1) group-iterator access. not yet ported in Go flecs. (`Cascade` provides hierarchy-depth ordering as a special built-in case.)
- **Equality operators** — `$this == Foo`, `$this != Foo`, `$this ~= "partial"` name-match filter terms in the Flecs Query Language. not yet ported in Go flecs.
- **AndFrom / OrFrom / NotFrom operators** — expand the component list of a given entity into implicit AND / OR / NOT terms, useful with prefab type-lists. not yet ported in Go flecs.
- **Query scopes** — `scope_open` / `scope_close` negate a sub-expression of arbitrary terms (e.g., `Position, !{ Velocity || Speed }`). not yet ported in Go flecs.
- **Access modifiers on query terms** — `In` / `InOut` / `Out` / `None` per-term annotations used by the C scheduler for pipeline sync-point inference. Go flecs governs mutation via `Read`/`Write` world scopes; per-term annotations are not ported.

### Additional gaps discovered in Phase 14.3 (Relationships port)

- **Exclusive relationship trait** (`EcsExclusive`) — ensures an entity can have at most one target for a given relationship; adding a second target automatically removes the first. Useful for state machines. not yet ported in Go flecs.
- **Symmetric relationship trait** (`EcsSymmetric`) — makes a relationship bidirectional: adding `(R, Y)` to entity `X` automatically adds `(R, X)` to entity `Y`. not yet ported in Go flecs.
- **Transitive relationship trait** (`EcsTransitive`) — enables transitive query matching for custom relationships (if `A R B` and `B R C`, queries for `(R, C)` also match `A`). The built-in `IsA` already walks its chain in `Get`/`Has`; general transitivity for custom relationships requires this unported trait. not yet ported in Go flecs.
- **Traversable relationship trait** (`EcsTraversable`) — formally marks a relationship as safe to traverse in queries; controls which relationships the query engine may follow when evaluating `Up`/`SelfUp`/`Cascade` terms. In Go flecs any entity can be used as a traversal relationship without explicit registration; the formal trait and its safety enforcement are not ported. not yet ported in Go flecs.
- **Configurable cleanup policies** (`OnDelete` / `OnDeleteTarget`) — **shipped in v0.32.0**. `SetCleanupPolicy` / `GetCleanupPolicy` with `DeleteAction`, `RemoveAction`, and `PanicAction`. `ChildOf` is bootstrapped with `(OnDeleteTarget, DeleteAction)`. See [Relationships.md § Cleanup policies](Relationships.md).
- **PairIsTag trait** (`EcsPairIsTag`) — forces a relationship's pairs to behave as tags regardless of whether an element is a component type. The built-in `ChildOf` uses this internally. Custom relationships cannot yet opt into this trait. not yet ported in Go flecs.
- **Entity scoping** (`ecs_set_scope` / `ecs_get_scope`) — push/pop a parent scope so that all subsequently created entities automatically receive a `(ChildOf, scope)` pair without explicit `AddID` calls. not yet ported in Go flecs.

### Additional gaps discovered in Phase 14.4 (HierarchiesManual port)

- **`OrderedChildren` trait** — add this trait to a parent entity to guarantee that `EachChild` iterates children in creation order regardless of component mutations that would otherwise move children between archetype tables. not yet ported in Go flecs.
- **`Parent` hierarchy storage** — a second, non-fragmenting storage for small structured hierarchies where children of multiple parents share the same archetype table. Reduces table fragmentation and memory footprint for prefab-heavy workloads. not yet ported in Go flecs.

### Additional gaps discovered in Phase 14.5 (PrefabsManual port)

- **Prefab tag** (`EcsPrefab`) — a built-in tag that excludes prefab entities from ordinary queries by default. In Go flecs, prefab entities participate in queries like any other entity. not yet ported in Go flecs.
- **Auto-override on instantiation** (`OnInstantiate, Override`) — automatically copies a component from the prefab into each new instance at `(IsA, prefab)` add time. `w.Override()` exposes the entity ID but the behavior is not implemented. not yet ported in Go flecs.
- **Prefab hierarchies** — when a prefab has `(ChildOf, prefab)` children, instantiating the prefab replicates the entire child subtree onto the instance. not yet ported in Go flecs.
- **Prefab slots** (`SlotOf`) — `(SlotOf, prefab)` on a prefab child creates a named slot relationship on the instance that resolves to the copied child in O(1) without a name lookup. not yet ported in Go flecs.

### Additional gaps discovered in Phase 14.6 (Systems port)

- **Custom pipeline phases** — In C flecs any entity tagged with `EcsPhase` can be a pipeline phase; phases are ordered via `DependsOn` pairs. Go flecs has exactly four hard-coded built-in phases (`PreUpdate`, `OnFixedUpdate`, `OnUpdate`, `PostUpdate`). not yet ported in Go flecs.
- **DependsOn ordering between systems** — C lets applications add `(DependsOn, OtherSystem)` to order two systems within a phase independently of registration order. Go flecs orders within a phase strictly by registration order. not yet ported in Go flecs.
- **System disabling** (`ecs_enable` / `EcsDisabled`) — pause a system without removing it; re-enable later. Go flecs requires `Close()` + `NewSystem` to approximate this. not yet ported in Go flecs.
- **Rate filters** (`SetInterval` / `SetRate`) — run a system every N frames or at a fixed wall-clock interval per system without restructuring the pipeline. not yet ported in Go flecs.
- **Single-system `Run` out-of-pipeline** — `ecs_run` invokes one system synchronously outside the normal pipeline. not yet ported in Go flecs.
- **`RunWorker` / explicit thread dispatch** — `ecs_run_worker` for manual entity-range partitioning outside the pipeline. not yet ported in Go flecs.
- **Pipeline introspection** — iterate the ordered system list in a pipeline to inspect execution order at runtime. not yet ported in Go flecs.

### Additional gaps discovered in Phase 14.7 (ObserversManual port)

- **`OnReplace` hook** — fires when `Set` overwrites an existing component value; receives both the old and new value. not yet ported in Go flecs.
- **`OnDelete` / `OnDeleteTarget` observer events** — C flecs fires observer callbacks for these events when a component entity or pair target is deleted. The Go port implements cleanup *policies* (v0.32.0) but not observer-driven cleanup hooks. not yet ported in Go flecs.
- **`OnTableEmpty` / `OnTableFill` events** — fire when an archetype table transitions between empty and non-empty. not yet ported in Go flecs.
- **Custom events** — create arbitrary event entities and emit them with `ecs_emit`; Go flecs supports only the three built-in events (`EventOnAdd`, `EventOnSet`, `EventOnRemove`). not yet ported in Go flecs.
- **Term-set observer filters (multi-term observers)** — C observers can match a query with multiple terms (e.g., "fire when Position is set but only if entity also has Velocity"). Go flecs observers subscribe to a single component at a time. not yet ported in Go flecs.
- **Yield-on-create** — `yield_existing` flag retroactively fires an observer for entities already matching the query at registration time. not yet ported in Go flecs.
- **Observer propagation / forwarding** — events propagate along relationship edges (e.g., `OnSet(Position)` on a parent notifies children inheriting `Position`). not yet ported in Go flecs.
- **Monitor observers** — `EcsMonitor` event fires when an entity starts or stops matching a query. not yet ported in Go flecs.
- **Observer disabling** — pause an observer without removing it (analogous to system disabling). not yet ported in Go flecs.
- **Fixed-source observer terms** — observer terms that match a component on a specific entity (not `$this`). not yet ported in Go flecs.

### Additional gaps discovered in Phase 14.8 (ComponentTraits port)

- **`Reflexive` relationship trait** — `Has(e, R, e)` is always true for a reflexive relationship (e.g. `IsA` is reflexive; `LocatedIn` is not). not yet ported in Go flecs.
- **`Constant` component trait** — marks a component read-only after its initial write; subsequent `Set` calls would be a fatal error. not yet ported in Go flecs.
- **`DontFragment` component trait** — opt a component into non-fragmenting sparse storage; sparse but does not create new archetype tables. not yet ported in Go flecs.
- **`Singleton` component trait** (`EcsSingleton`) — constrains a component to a single world-global instance stored on the component entity itself; queries auto-match the component entity as source. not yet ported in Go flecs.
- **`Union` relationship trait** — union-pair semantics: only one of several targets may be active for a given relationship on an entity; stored to minimise table fragmentation. not yet ported in Go flecs.
- **`Final` entity trait** — prevents an entity from being used as an `IsA` target; enables query optimisation. not yet ported in Go flecs.
- **`OneOf` relationship trait** — constrains relationship targets to be children of a specified entity (useful for enum-style relationships). not yet ported in Go flecs.
- **`With` relationship** — automatically co-adds a second component when a component is added; when added to a relationship, the co-added id mirrors the pair target. not yet ported in Go flecs.
- **`Relationship` / `Target` / `Trait` enforcement traits** — restrict how an entity may be used in pairs (as relationship, as target, or as a trait); relax some constraints when the `Trait` marker is present. not yet ported in Go flecs.

### Additional gaps discovered in Phase 14.9 (FlecsRemoteApi port)

- **Query execution endpoint** (`GET /query?expr=`) — evaluates a Flecs Query Language expression over HTTP and returns matched entities with field values. Requires the FlecsQueryLanguage DSL and `ecs_iter_to_json`; not yet ported in Go flecs.
- **Entity / component mutation endpoints** (`PUT /entity`, `DELETE /entity`, `PUT /component`, `DELETE /component`) — create, delete, and mutate entities and components via REST. Require the reflection / meta-cursor API (`ecs_meta_cursor`); not yet ported in Go flecs.
- **Toggle endpoint** (`PUT /toggle`) — enable/disable an entity or a per-component enable bit via REST. Requires entity disabling (`Disabled` tag) and the `CanToggle` component trait; not yet ported in Go flecs.
- **Aggregated stats (FlecsStats module)** — `GET /stats/world` and `GET /stats/pipeline` return multi-period aggregated statistics collected by the `FlecsStats` addon. FlecsStats module not yet ported in Go flecs.
- **Type-info / reflection endpoint** (`GET /type_info/<path>`) — returns the reflection schema for a component type. Requires the meta-cursor module (`ecs_meta_cursor`); not yet ported in Go flecs.
- **FlecsExplorer integration** — the browser-based world inspector at `https://flecs.dev/explorer` connects via the C REST API and requires entity mutation, query execution, and type-info endpoints. Not integrated with the Go REST handler.
