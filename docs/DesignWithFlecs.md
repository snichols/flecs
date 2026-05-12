# Design with flecs

This guide covers ECS design patterns: when to reach for components vs. tags vs. relationships, how to scope systems and phases, and how to structure a growing Go flecs application. This is an opinionated guide — feel free to deviate when the situation calls for it.

For hands-on first contact, start with the [Quickstart](Quickstart.md). For the full API surface of each feature, follow the cross-links throughout this guide.

---

## Entities

Entities map to in-game objects the same way you would create them in other game engines: a player, a building, a UI element, a camera, a projectile. By itself an entity is just a unique 64-bit handle (`flecs.ID`). Its character is entirely determined by which components are attached to it.

For the full entity and component API, see [Entities & Components](EntitiesComponents.md).

### Entity Initialization

When creating entities, you typically want to initialize them with a set of default components and default values. Prefabs are designed for this. A prefab is an entity template: components set on the prefab are inherited by every instance.

```go
w := flecs.New()
flecs.RegisterComponent[Position](w)
flecs.RegisterComponent[Health](w)

// Define a prefab (the template).
var heroTemplate flecs.ID
w.Write(func(fw *flecs.Writer) {
    heroTemplate = fw.NewEntity()
    flecs.Set(fw, heroTemplate, Position{X: 0, Y: 0})
    flecs.Set(fw, heroTemplate, Health{Max: 100, Current: 100})
})

// Instantiate: instance inherits Position and Health from heroTemplate.
var hero flecs.ID
w.Write(func(fw *flecs.Writer) {
    hero = fw.NewEntity()
    fw.AddID(hero, flecs.MakePair(w.IsA(), heroTemplate))
})
```

Creating entities from prefabs is not just convenient — it is faster, because all instances share the same archetype and the same cached query-match results. Prefabs also classify entity kinds, which helps editors and the [REST explorer](FlecsRemoteApi.md) display your world.

See [Prefabs](PrefabsManual.md) for the full API: value inheritance, copy-on-write override, prefab variants, and traversal helpers.

### Entity Lifecycle

Entities can be created and deleted dynamically. When an entity is deleted, any handle to that entity is no longer valid. When you store entity handles in components or other data structures, guard against stale IDs using `IsAlive`:

```go
if w.IsAlive(e) {
    // safe to use e
}
```

Entity handles are stable: flecs encodes a generation counter in the upper 32 bits of the ID. When an entity slot is recycled the generation increments, so old copies of the ID correctly report as not-alive. You can safely store entity handles in components.

### Entity Names

Entities can be given names, making them easy to identify in debuggers and inspect via the [REST API](FlecsRemoteApi.md). Names must be unique within a scope (the scope is determined by the `ChildOf` relationship). Two children of the same parent may not share a name.

```go
var e flecs.ID
w.Write(func(fw *flecs.Writer) {
    e = fw.NewEntity()
    w.SetName(e, "player")
})

// Look up by name — O(1) hashmap lookup.
found, ok := w.Lookup("player")
```

For hierarchical path lookups (`"parent.child"`) and child-scoped lookup, see [Entities & Components](EntitiesComponents.md).

---

## Components

Designing your components is the most impactful decision in an ECS application. Changing a component forces updates across every system that uses it. A few patterns keep components stable, cache-friendly, and easy to reuse.

For the full component API, see [Entities & Components](EntitiesComponents.md).

### Keep Components Small and Atomic

Prefer separate, focused components over one large combined struct. Choose `Position`, `Rotation`, and `Scale` over a single `Transform`. Choose `TurretTarget` and `TurretAngle` over a single `Turret`.

**Why this matters:**

- **Cache efficiency.** A system that only reads `Position` will not load `Rotation` and `Scale` into cache lines. Smaller components mean higher hit rates and less RAM traffic.
- **Less refactoring.** Atomic components have fewer reasons to change. A combined component invites constant reshuffling as requirements evolve.
- **More reusable systems.** Systems written for atomic components are less opinionated and compose well across projects.
- **Overhead is minimal.** Querying for multiple components adds negligible cost because most queries are cached.

```go
// Preferred: separate, atomic components.
type Position struct{ X, Y float32 }
type Velocity struct{ DX, DY float32 }
type Health   struct{ Current, Max int }

// Avoid: one large combined struct.
// type ActorState struct{ X, Y, DX, DY float32; HP, MaxHP int }
```

A downside of many components is navigation overhead in large projects. Tools like the [REST explorer](FlecsRemoteApi.md) help you browse and inspect all registered components at runtime.

### Complex Component Data

There is a misconception that ECS components must be plain value types. You can store slices, maps, and other dynamic structures in components — Go manages the allocation transparently.

```go
type Inventory struct {
    Items []ItemID
}
```

The main consideration: components with pointer-like fields (slices, maps, pointers) require careful access discipline. Use `world.Read` for concurrent reads and `world.Write` for mutation so that flecs can manage thread safety and deferred command merging correctly.

---

## Queries

Queries are how you find entities matching a given component expression. Go flecs has two query kinds that trade construction cost against iteration cost. For the full query API, see [Queries](Queries.md).

### Use the Right Query

| | Uncached (`NewQuery`) | Cached (`NewCachedQuery`) |
|---|---|---|
| Construction | Fast — scans live tables once | Slow — pre-filters, tracks new archetypes |
| Iteration | Slower — rescans each call | Fast — amortized O(1) per `Iter` |
| Best for | Ad-hoc or one-shot lookups | Repeated iteration (e.g. per-frame systems) |
| Inside a system body | Yes | No — create outside, pass in |

```go
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)

// Uncached: fast to construct, fine for ad-hoc lookups.
q := flecs.NewQuery(w, posID, velID)
it := q.Iter()
for it.Next() {
    pos := flecs.Field[Position](it, posID)
    vel := flecs.Field[Velocity](it, velID)
    for i := range it.Entities() {
        pos[i].X += vel[i].DX
        pos[i].Y += vel[i].DY
    }
}
```

```go
// Cached: create once at startup, iterate every frame.
cq := flecs.NewCachedQuery(w, posID, velID)
defer cq.Close()
// ... call cq.Iter() each frame inside a system
```

> **Important:** Do not create and destroy cached queries in a tight loop. Their construction cost is meant to be paid once and amortized over many iterations.

### Mutation Access

In Go flecs, mutation access is governed by `world.Read` / `world.Write` scopes rather than per-term annotations. Use `world.Read` when you only need to read world state; use `world.Write` when you need to add, remove, or set components. Deferred commands accumulated inside a `Write` scope are flushed when the scope closes.

See [Systems](Systems.md) for how deferred mutations interact with the pipeline and how to structure system callbacks that need both reads and writes.

---

## Systems

Systems operate over collections of entities that match a query. This is the main conceptual shift from OOP, where logic lives inside objects. For the full systems API, see [Systems](Systems.md).

### System Scope

Design systems with a single responsibility. Starting with a larger system while a feature is taking shape is fine — split it once responsibilities become clear.

Advantages of focused systems:

- **Isolation.** Removing a system from the pipeline removes exactly that behavior — useful for testing and debugging.
- **Compiler opportunities.** Simpler hot loops with fewer side effects give the compiler more room to auto-vectorize.
- **Reusability.** Narrow systems travel well across projects.

```go
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)
moveQ := flecs.NewCachedQuery(w, posID, velID)

// Preferred: two small systems, each with one job.
flecs.NewSystem(w, moveQ, func(dt float32, it *flecs.QueryIter) {
    for it.Next() {
        pos := flecs.Field[Position](it, posID)
        vel := flecs.Field[Velocity](it, velID)
        for i := range it.Entities() {
            pos[i].X += vel[i].DX * dt
            pos[i].Y += vel[i].DY * dt
        }
    }
})
```

### System Scheduling

Go flecs schedules systems automatically via `World.Progress`. The advantage over calling queries manually is that you can compose systems from separate packages without manual ordering work — the pipeline sorts them by phase and registration order.

A system is three things: a query, a function, and ordering information. The query finds the right entities; the function is invoked with those entities; and the ordering (phase + declaration order) controls when in the frame it runs.

Within a phase, systems execute in registration order. This is deliberate: explicit per-system dependency declarations couple systems to each other's names, making reuse across projects difficult.

```go
type Tag struct{}
tagID := flecs.RegisterComponent[Tag](w)
q := flecs.NewCachedQuery(w, tagID)

flecs.NewSystemInPhase(w, w.PreUpdate(),  q, func(dt float32, it *flecs.QueryIter) { /* load input */ })
flecs.NewSystemInPhase(w, w.OnUpdate(),   q, func(dt float32, it *flecs.QueryIter) { /* game logic */ })
flecs.NewSystemInPhase(w, w.PostUpdate(), q, func(dt float32, it *flecs.QueryIter) { /* render prep */ })
```

---

## Phases and Pipelines

Phases order the systems inside a pipeline. Each call to `World.Progress` executes all phases in sequence, running every system in a phase before advancing to the next.

### Built-in Phases

Go flecs ships with four built-in phases executed in this order:

**`w.PreUpdate()`** — runs before gameplay logic. Load input events here and translate them into higher-level actions before gameplay systems need them.

**`w.OnFixedUpdate()`** — fixed-timestep dispatch. Driven by an accumulator: `SetFixedTimestep` controls how often this phase fires, independent of wall-clock frame rate. Use it for deterministic physics or network simulation. See [Systems](Systems.md) for accumulator details and the spiral-of-death warning.

**`w.OnUpdate()`** — the main gameplay phase and the default for `NewSystem`. Most game logic belongs here.

**`w.PostUpdate()`** — runs after gameplay logic. Good for applying corrections (collision resolution), computing world-space transforms from a [hierarchy](HierarchiesManual.md), or preparing data for rendering.

### Phase Conventions

Systems assigned to a phase should do work that fits that phase's role. When third-party packages follow this convention, their systems slot naturally into the right phase without manual ordering work.

```go
flecs.NewSystemInPhase(w, w.PreUpdate(),  inputQ,  loadInput)
flecs.NewSystemInPhase(w, w.OnUpdate(),   moveQ,   applyVelocity)
flecs.NewSystemInPhase(w, w.OnUpdate(),   aiQ,     runAI)
flecs.NewSystemInPhase(w, w.PostUpdate(), renderQ, prepareRender)
```

> **Custom phases not yet ported.** The upstream C flecs supports arbitrary phase entities ordered via `(DependsOn, phase)` pairs and fully custom pipelines. Go flecs has exactly the four hard-coded built-in phases above. See the [feature-gap list](README.md).

---

## Modules

Large applications accumulate many components and systems. Go's package system fills the role that the C flecs module system provides in C applications — Go packages are the natural unit of feature isolation and reuse.

### Structuring Features as Packages

Define a feature as a Go package that exports its component types and a registration function:

```go
// Package physics — component types and system registration.
package physics

import "github.com/snichols/flecs"

type Position struct{ X, Y float32 }
type Velocity struct{ DX, DY float32 }

func Register(w *flecs.World) {
    flecs.RegisterComponent[Position](w)
    flecs.RegisterComponent[Velocity](w)
    // register physics systems...
}
```

Other packages import `physics` for the types and call `physics.Register(w)` once at startup. Calling `RegisterComponent` more than once for the same type is idempotent — importing the same package from multiple places is safe.

### Feature Swapping

A powerful pattern: split a feature into a *data package* (component types) and a *behavior package* (systems). Application code imports only the data package. The behavior package can be swapped for another implementation without touching the rest of the application.

This is the Go equivalent of the C flecs `components.*` / `systems.*` split:

```
myapp/
  physics/          ← component types (Position, Velocity, Mass)
  physics/rigid/    ← rigid-body system implementation
  physics/verlet/   ← alternative verlet integration
```

The application references `physics` for component types. At startup it chooses which implementation to register. Swapping one implementation for another requires changing a single `import` line.

### Dormant Systems

Systems that never match any entities remain dormant. Importing a package that registers systems for components you do not use adds no per-frame cost — an unmatched system has nothing to iterate.

---

## Relationships

Relationships extend the component model from plain data attachment to first-class associations between entities. The two most common built-in relationships are `ChildOf` (scene hierarchies) and `IsA` (prefab inheritance).

For the full relationship API, see [Relationships](Relationships.md) and [Hierarchies](HierarchiesManual.md).

### Signs You Need a Relationship

Consider relationships when you notice any of the following:

- You have a component that stores an entity handle, and you need to find all entities that point to a specific entity. A relationship lets flecs index that association for you.
- You have many components with similar members, and systems that duplicate code for each. A typed relationship lets one system cover all instances.
- You need multiple instances of the same logical association on one entity (e.g., membership in multiple groups). Relationships naturally support multiple targets.
- You are designing a container structure — an inventory, a socket, a slot. Relationships make containment explicit and queryable.
- You want to group entities by a label (world cell, layer, team) and look up all members of a group efficiently.
- You are modeling an enum-like state where you want to query for individual values as separate terms.

```go
// Tag relationship — no data, just membership.
type Likes struct{}

w.Write(func(fw *flecs.Writer) {
    likesID := flecs.RegisterComponent[Likes](w)
    _ = likesID // tag used as relationship by entity ID

    alice := fw.NewEntity()
    bob   := fw.NewEntity()
    carol := fw.NewEntity()

    // bob likes both alice and carol — multiple targets.
    fw.AddID(alice, flecs.MakePair(bob, carol)) // using bob as relationship
})
```

For a simpler relationship example — and querying by target — see the `TestRelationships_BasicPair` test in [relationships_examples_test.go](relationships_examples_test.go).

### Tags vs. Components vs. Relationships

| Use | When |
|---|---|
| **Tag** (zero-size type) | Marking category membership: `type Enemy struct{}`, `type Selected struct{}` |
| **Component** (data-bearing type) | Per-entity data: `Position`, `Health`, `Velocity` |
| **Relationship** | Associating two entities; querying by target; multiple instances per entity |

[ComponentTraits](ComponentTraits.md) covers `SetInheritable[T]`, which makes a component automatically inherited by instances via the `IsA` relationship — the bridge between the prefab and query systems.

---

## Observers and Reactive Design

Sometimes you need code to run in response to structural changes — an entity gaining or losing a component — rather than on every frame. [Observers](ObserversManual.md) and [hooks](ObserversManual.md) handle this.

Use observers when:

- You need to maintain a secondary data structure in sync with world state (an index, a cache, a renderer upload queue).
- You want to log or validate state at the moment a component is added or removed.
- You are building a replication layer that needs to know which components changed.

Hooks run synchronously on Add/Set/Remove; observers run per-subscriber. Both receive a `*flecs.Writer` for safe in-callback mutation. See [Observers](ObserversManual.md) for the full API.

---

## Design Tips Summary

- **Start with entities and components.** Reach for relationships when you notice the signs above.
- **Keep components small.** Composite structs hurt cache performance and cause more refactoring than they prevent.
- **Use prefabs for entity templates.** They are faster than manual initialization and make entity classification explicit. See [Prefabs](PrefabsManual.md).
- **Single-responsibility systems.** A system that does one thing is easy to remove, test, and reuse. See [Systems](Systems.md).
- **Use phases to separate concerns.** Input in `PreUpdate`, simulation in `OnUpdate`, corrections and rendering prep in `PostUpdate`.
- **Package features as Go packages.** Go's import system provides the module isolation that C flecs achieves with `ECS_MODULE`.
- **React to changes with observers.** Use per-frame queries for work that happens every frame; use observers for structural events. See [Observers](ObserversManual.md).
- **Inspect at runtime.** The [REST API](FlecsRemoteApi.md) exposes world state over HTTP for debugging and tooling.
- **Read the other manuals.** This guide points the way; the details are in [Quickstart](Quickstart.md), [Entities & Components](EntitiesComponents.md), [Queries](Queries.md), [Relationships](Relationships.md), [Hierarchies](HierarchiesManual.md), [Prefabs](PrefabsManual.md), [Systems](Systems.md), [Observers](ObserversManual.md), [ComponentTraits](ComponentTraits.md), [FlecsRemoteApi](FlecsRemoteApi.md), and the [Reference Manual](Manual.md).
