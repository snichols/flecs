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
| [HierarchiesManual.md](HierarchiesManual.md) | pending | 14.4 |
| [PrefabsManual.md](PrefabsManual.md) | pending | 14.5 |
| [Systems.md](Systems.md) | pending | 14.6 |
| [ObserversManual.md](ObserversManual.md) | pending | 14.7 |
| [ComponentTraits.md](ComponentTraits.md) | pending | 14.8 |
| [FlecsRemoteApi.md](FlecsRemoteApi.md) | pending | 14.9 |
| [DesignWithFlecs.md](DesignWithFlecs.md) | pending | 14.10 |
| [Manual.md](Manual.md) | pending | 14.11 |
| [FAQ.md](FAQ.md) | pending | 14.12 |

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
- **Cleanup policies** (`OnDeleteTarget`, `OnDelete(component)`, `Delete` action) — partial in Go port.
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
- **Cleanup policies / component-delete cascade** — when a component entity is deleted, automatically remove that component from all entities that have it (C default behaviour via `OnDelete` cleanup policy). not yet ported in Go flecs.
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
- **Configurable cleanup policies** (`OnDelete` / `OnDeleteTarget` with `Delete` / `Remove` actions) — controls what happens when a relationship entity or target entity is deleted. `ChildOf` is hardcoded to cascade-delete children; custom cleanup policies for arbitrary relationships are not yet configurable. not yet ported in Go flecs.
- **PairIsTag trait** (`EcsPairIsTag`) — forces a relationship's pairs to behave as tags regardless of whether an element is a component type. The built-in `ChildOf` uses this internally. Custom relationships cannot yet opt into this trait. not yet ported in Go flecs.
- **Entity scoping** (`ecs_set_scope` / `ecs_get_scope`) — push/pop a parent scope so that all subsequently created entities automatically receive a `(ChildOf, scope)` pair without explicit `AddID` calls. not yet ported in Go flecs.
