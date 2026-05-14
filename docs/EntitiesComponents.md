# Entities & Components

Entities and components are the main building blocks of any ECS application. This manual covers the full entity and component API: entity lifecycle, identifiers, component registration, tags, pair IDs, hooks, and introspection.

See the [Quickstart](Quickstart.md) for a hands-on introduction.

---

## Entities

An entity is a uniquely identifiable object in your simulation. It may represent a unit, a building, a UI element, a particle effect, or the camera itself — anything with independent identity.

By itself, an entity is just a unique 64-bit integer. It carries no type information; its character is entirely determined by which components are attached to it.

In Go flecs, the entity identifier is `flecs.ID` (an alias for `uint64`). The zero value (`0`) is reserved as an invalid sentinel; functions that return an entity may return `0` to signal failure or absence.

The high 32 bits of an entity ID are used by flecs for liveliness tracking (a generation counter). This means that recycled entity IDs can appear numerically large. That is expected and normal.

### Creation

Create a new entity inside a `Write` scope:

```go
w := flecs.New()

var e flecs.ID
w.Write(func(fw *flecs.Writer) {
    e = fw.NewEntity()
})
```

The first user entity ID is not `1` — flecs reserves lower IDs for built-in entities (ChildOf, IsA, pipeline phases, the Name component, etc.).

Empty entities are not matched by any query or system. They are invisible to iteration until at least one component is added.

### Deletion

Delete an entity with `World.Delete`. Deletion cascades: all entities that have a `(ChildOf, e)` relationship are deleted first (depth-first, children before parents).

```go
w.Delete(e)
if !w.IsAlive(e) {
    // e is gone
}
```

After deletion the entity ID is freed for reuse. When flecs recycles the identifier it increments the generation counter stored in the upper 32 bits. This lets `IsAlive` distinguish the new entity (alive) from any stale copies of the old ID (not alive):

```go
var e1, e2 flecs.ID
w.Write(func(fw *flecs.Writer) { e1 = fw.NewEntity() })
w.Delete(e1) // recycles the low-32-bit index; bumps generation

w.Write(func(fw *flecs.Writer) { e2 = fw.NewEntity() }) // same index, generation+1

// e1 (old generation) is no longer alive; e2 is alive.
w.IsAlive(e1) // false
w.IsAlive(e2) // true
```

Deleting an already-deleted entity is safe — the operation is idempotent:

```go
w.Delete(e1) // OK
w.Delete(e1) // OK: post condition (e1 not alive) already satisfied
```

### Clearing

> **Not yet ported in Go flecs** — `Clear(e)` removes all components from an entity without deleting it. This is more efficient than removing components one by one. See the [feature-gap list](README.md) for details.

### Liveliness Checking

`World.IsAlive` reports whether an entity is currently alive. Use it to guard against stale IDs:

```go
var e1, e2 flecs.ID
w.Write(func(fw *flecs.Writer) {
    e1 = fw.NewEntity()
    e2 = fw.NewEntity()
})
w.Delete(e1)

w.IsAlive(e1) // false — deleted
w.IsAlive(e2) // true  — still alive
```

### Manual IDs

> **Not yet ported in Go flecs** — `MakeAlive(id)` lets applications claim a specific entity ID (useful for networked synchronisation where both sides must share the same ID). See the [feature-gap list](README.md).

### Manual Versioning

> **Not yet ported in Go flecs** — `SetVersion(versionedID)` overrides the generation counter of an entity, enabling manual version synchronisation for networked IDs. See the [feature-gap list](README.md).

### Entity Ranges

> **Not yet ported in Go flecs** — entity ranges constrain which IDs `NewEntity` issues, enabling simple ownership partitioning across clients or servers. See the [feature-gap list](README.md).

### Names

Entities can be given string names that allow them to be looked up on the world.

```go
var e flecs.ID
w.Write(func(fw *flecs.Writer) {
    e = fw.NewEntity()
    w.SetName(e, "MyEntity")
})

found, ok := w.Lookup("MyEntity")
// found == e, ok == true

name, _ := w.GetName(e)
// name == "MyEntity"
```

Names are scoped by hierarchy. An entity that is a child of another entity must be looked up via its full dot-separated path:

```go
var parent, child flecs.ID
w.Write(func(fw *flecs.Writer) {
    parent = fw.NewEntity()
    child = fw.NewEntity()
    w.SetName(parent, "Parent")
    w.SetName(child, "Child")
    flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
})

found, ok := w.Lookup("Parent.Child")
// found == child, ok == true
```

You can also look up a child relative to a known parent using `LookupChild`:

```go
found, ok := w.LookupChild(parent, "Child")
// found == child, ok == true
```

`GetName` returns the entity's own name (without parent path). `PathOf` returns the full dot-separated path from the root:

```go
name, _ := w.GetName(child)  // "Child"
path := w.PathOf(child)       // "Parent.Child"
```

Names must be unique within a parent scope. To rename an entity, call `SetName` again:

```go
w.SetName(e, "NewName")
```

### Disabling

> **Not yet ported in Go flecs** — entity disabling (`Enable` / `Disable`) prevents an entity from being matched by queries without deleting it. The underlying mechanism adds a `Disabled` tag. See the [feature-gap list](README.md).

---

## Components

A component is something added to an entity. Components can tag an entity, attach data, or encode a relationship (pair) between entities. To disambiguate:

| Kind | Has Data | Is Pair |
|------|----------|---------|
| Tag | No | No |
| Component | Yes | No |
| Relationship (tag pair) | No | Yes |
| Relationship component (data pair) | Yes | Yes |

"Has data" means that in addition to asking *whether* an entity has the component, you can ask *what value* it holds.

A pair is a component composed from two entity IDs, such as `(Likes, Pizza)` or `(Near, Office)`. See the [Relationships manual](Relationships.md) for details.

### Operations

| Operation | Description |
|-----------|-------------|
| `Set[T]` | Sets the value of component T. Adds T first if the entity doesn't have it; fires OnAdd then OnSet. |
| `Get[T]` | Returns an immutable copy of T and a boolean. Returns the zero value and false if T is absent. |
| `Has[T]` | Reports whether the entity has T (own or inherited). |
| `Owns[T]` | Reports whether the entity directly owns T (not inherited). |
| `Remove[T]` | Removes T from the entity. No-op if the entity doesn't have T. |
| `AddID` | Adds a raw entity ID (tag or pair) to the entity without data. |
| `RemoveID` | Removes a raw entity ID from the entity. |
| `HasID` | Reports whether the entity has the given raw ID. |

```go
type Position struct{ X, Y float32 }

w := flecs.New()
flecs.RegisterComponent[Position](w)

var e flecs.ID
w.Write(func(fw *flecs.Writer) {
    e = fw.NewEntity()
    flecs.Set(fw, e, Position{X: 10, Y: 20})
})

w.Read(func(r *flecs.Reader) {
    p, ok := flecs.Get[Position](r, e)
    // p == Position{X: 10, Y: 20}, ok == true

    flecs.Has[Position](r, e)  // true
    flecs.Owns[Position](r, e) // true
})

w.Write(func(fw *flecs.Writer) {
    flecs.Remove[Position](fw, e)
})

w.Read(func(r *flecs.Reader) {
    flecs.Has[Position](r, e) // false
})
```

### Tags

A tag is a zero-size component (a Go struct with no fields). Tags classify entities without allocating any per-entity storage.

```go
type Enemy struct{} // zero fields → tag

w := flecs.New()

var e flecs.ID
w.Write(func(fw *flecs.Writer) {
    e = fw.NewEntity()
    flecs.Set(fw, e, Enemy{}) // adds the Enemy tag
})

w.Read(func(r *flecs.Reader) {
    flecs.Has[Enemy](r, e) // true
})
```

Tags can also be created at runtime as plain entity IDs (useful when the tag type is not known at compile time):

```go
var tagEnemy, e flecs.ID
w.Write(func(fw *flecs.Writer) {
    tagEnemy = fw.NewEntity() // dynamic tag ID
    e = fw.NewEntity()
    flecs.AddID(fw, e, tagEnemy)
})

w.Read(func(r *flecs.Reader) {
    flecs.HasID(r, e, tagEnemy) // true
})
```

### Hooks

Hooks are callbacks invoked at specific points in a component's lifecycle. Unlike observers (which can have many subscribers per component), there is exactly one hook of each kind per component type.

The Go port supports four component hooks:

| Hook | When it fires |
|------|---------------|
| `OnAdd[T]` | When T is newly added to an entity (not on subsequent Set overwrites). |
| `OnSet[T]` | Every time T's value is written via `Set[T]` (including the initial write after OnAdd). |
| `OnRemove[T]` | Before T is removed from an entity, including when the entity is deleted. |
| `OnReplace[T]` | When `Set[T]` overwrites an **existing** value; receives both `old` and `new`. Does not fire on the first Set. |

Hooks are registered before any component values are written:

```go
type Position struct{ X, Y float32 }

w := flecs.New()

flecs.OnAdd[Position](w, func(fw *flecs.Writer, e flecs.ID, v Position) {
    // fires once when Position is first added to an entity
    _ = e
})

flecs.OnSet[Position](w, func(fw *flecs.Writer, e flecs.ID, v Position) {
    // fires every time Position is written
    _ = v.X
})

flecs.OnRemove[Position](w, func(fw *flecs.Writer, e flecs.ID, v Position) {
    // fires before Position is removed; v holds the final value
    _ = v
})

var e flecs.ID
w.Write(func(fw *flecs.Writer) {
    e = fw.NewEntity()
    flecs.Set(fw, e, Position{X: 1, Y: 2}) // OnAdd fires, then OnSet fires
    flecs.Set(fw, e, Position{X: 3, Y: 4}) // OnSet fires (OnAdd does not)
    flecs.Remove[Position](fw, e)           // OnRemove fires
})
```

Calling `OnAdd[T]`, `OnSet[T]`, `OnRemove[T]`, or `OnReplace[T]` a second time **replaces** the previous hook. Pass `nil` to clear a hook.

`OnReplace[T]` is useful for diff-style logic: delta detection, change-event publishing, and undo stacks. When a `Set` overwrites an existing value, it fires before `OnSet` with the previous and incoming values:

```go
flecs.OnReplace[Position](w, func(fw *flecs.Writer, e flecs.ID, old, new Position) {
    // old is the value before the Set; new is the value being written.
    // OnSet will fire immediately after with new as its value.
})
```

See [ObserversManual.md § OnReplace Hook](ObserversManual.md#onreplace-hook) for the full contract.

### Components are Entities

Every registered component is itself an entity in the world. This is fundamental to how flecs works: component metadata is stored as regular entity data, and all operations that apply to entities also apply to components.

`RegisterComponent[T]` returns the component's entity ID. `ComponentInfo` retrieves its size and alignment:

```go
type Position struct{ X, Y float32 }

w := flecs.New()
posID := flecs.RegisterComponent[Position](w)

w.Read(func(r *flecs.Reader) {
    info, ok := r.ComponentInfo(posID)
    // ok == true
    // info.Size  == unsafe.Sizeof(Position{})
    // info.Align == unsafe.Alignof(Position{})
    // info.Name  == "docs_test.Position" (reflect.Type.String())
    _ = info
    _ = ok
})
```

Because component entities are regular entities, you can name them, add tags to them, or delete them — all via the same API you use for any other entity.

### Component Traits

Tags added to component entities customize their storage and query behaviour. These are called *traits*.

The `CanToggle` trait (shipped v0.35.0) allows individual entities to have a component temporarily disabled without removing it. See the [ComponentTraits manual](ComponentTraits.md#cantoggle) for the full API.

The `Sparse` trait (shipped v0.51.0, storage path) stores a component in a per-component sparse-set rather than the archetype tables, giving pointer-stable addresses and no archetype transition on add/remove. Use `SetSparse(w, compID)` before first use. See the [ComponentTraits manual § Sparse](ComponentTraits.md#sparse) for the full API. Note: query integration is deferred to Phase 15.20 — use `EachSparse[T]` for bulk iteration in the meantime.

### Registration

Register a component before using it. `RegisterComponent[T]` is idempotent — calling it multiple times for the same type returns the same ID:

```go
type Velocity struct{ DX, DY float32 }

w := flecs.New()
velID := flecs.RegisterComponent[Velocity](w)
_ = velID
```

Registration also happens implicitly the first time `Set[T]` is called for a type that has not been explicitly registered. Explicit registration is recommended for clarity, and required when you need to set up hooks or inspect the component ID before any entity is created.

A natural place to register related components together is an initialisation function or a constructor:

```go
func registerMovement(w *flecs.World) {
    flecs.RegisterComponent[Position](w)
    flecs.RegisterComponent[Velocity](w)
}
```

### Unregistration

In Go flecs v0.32.0, configurable cleanup policies are shipped. The **default** behavior when a component entity is deleted is `OnDelete + Remove`: the component is removed from all entities that hold it. You can change the policy using `SetCleanupPolicy`:

```go
posID := flecs.RegisterComponent[Position](w)

// Panic if any entity still holds Position when posID is deleted.
flecs.SetCleanupPolicy(w, posID, w.OnDelete(), w.PanicAction())

// Or via pair-add (equivalent):
w.Write(func(fw *flecs.Writer) {
    fw.AddID(posID, flecs.MakePair(w.OnDelete(), w.PanicAction()))
})
```

**Note:** if `PanicAction` fires mid-cascade the world is in a halted state; no recovery is attempted. See `World.PanicAction()` godoc for details.

### Singletons

**Shipped in v0.44.0** — the Singleton trait constrains a component to at most one holder entity in the world at any time.

> **Semantic note:** Go's Singleton semantic ("at most one holder") differs from C's `EcsSingleton` ("component may only be added to itself"). The Go interpretation is more useful for application code. See [ComponentTraits.md](ComponentTraits.md#singleton) for the full divergence note.

```go
type TimeOfDay struct{ Hour float32 }

w := flecs.New()
todID := flecs.RegisterComponent[TimeOfDay](w)
flecs.SetSingleton(w, todID)

var clock flecs.ID
w.Write(func(fw *flecs.Writer) {
    clock = fw.NewEntity()
    flecs.WriteSingleton(fw, clock, TimeOfDay{Hour: 6})
})

// Read via typed accessor
w.Read(func(fr *flecs.Reader) {
    ptr, ok := flecs.Singleton[TimeOfDay](fr) // *TimeOfDay, bool
    _ = ptr
    _ = ok
})

// Adding to a second entity panics with a message naming both entities
// w.Write(func(fw *flecs.Writer) {
//     e2 := fw.NewEntity()
//     flecs.Set(fw, e2, TimeOfDay{Hour: 12}) // panic!
// })
```

The singleton slot is released when the component is removed (via `Remove[T]` or `RemoveID`) or when the holding entity is deleted.

### WriteOnce

**Shipped in v0.45.0** — the `WriteOnce` trait prevents value rewrites after the first `Set` on a given (entity, component) pair.

> **Renamed from `Constant`**: Previously called `Constant` in the Phase 14.8 gap analysis; renamed to avoid a future collision with upstream `EcsConstant` (an enum-value tag in the meta addon).

```go
type Config struct{ MaxPlayers int }

w := flecs.New()
cfgID := flecs.RegisterComponent[Config](w)
flecs.SetWriteOnce(w, cfgID)

var e flecs.ID
w.Write(func(fw *flecs.Writer) {
    e = fw.NewEntity()
    flecs.Set(fw, e, Config{MaxPlayers: 4}) // first write — OK
})

// Second write panics: "WriteOnce component ... already written"
// w.Write(func(fw *flecs.Writer) {
//     flecs.Set(fw, e, Config{MaxPlayers: 8}) // panic!
// })
```

`Remove` clears the per-(entity, component) tracking so a fresh `Add + Set` cycle starts over. `WriteOnce` does not block `Remove`. Raw-pointer access via `FieldByMatch[T]` or `Each[T]` is unchecked by design.

### Component Disabling

**Shipped in v0.35.0** — components can be individually disabled on an entity using the `CanToggle` trait. Disabling prevents the entity from matching queries on that component without removing it; the component value is preserved and the entity stays in its archetype table.

```go
posID := flecs.RegisterComponent[Position](w)
flecs.SetCanToggle(w, posID) // mark once

w.Write(func(fw *flecs.Writer) {
    flecs.DisableID(fw, e, posID)          // bit-flip, no table migration
    // flecs.Disable[Position](fw, e)      // typed variant

    flecs.IsEnabledID(fw, e, posID) // → false
    fw.HasID(e, posID)                     // → true (still on entity)

    flecs.EnableID(fw, e, posID)           // restore
})

// Queries skip disabled rows automatically:
flecs.Each1[Position](r, func(e flecs.ID, p *Position) {
    // not called while Position is disabled for e
})
```

See the [ComponentTraits manual](ComponentTraits.md#cantoggle) for the complete API reference.

---

## See Also

- [Quickstart](Quickstart.md) — hands-on introduction to entities, components, queries, and systems.
- [Relationships](Relationships.md) — pairs, ChildOf hierarchies, IsA inheritance.
- [Queries](Queries.md) — query terms, AND/NOT/Optional, cached queries.
- [ComponentTraits](ComponentTraits.md) — sparse storage, CanToggle, and other component-level customisation.
- [ObserversManual](ObserversManual.md) — multi-subscriber reactive callbacks (complement to hooks).
- [Manual](Manual.md) — top-level reference hub with world lifecycle, concurrency model, and concept map.
