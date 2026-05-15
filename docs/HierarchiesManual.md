# Hierarchies

ChildOf hierarchies let you organize entities into trees — scenes, prefab structures, UI widgets, or file-system-like paths. Any entity can be the parent of any other entity. Deleting a parent recursively deletes all descendants.

See the [Quickstart](Quickstart.md) for a hands-on introduction, [Relationships](Relationships.md) for the ChildOf pair and relationship concepts, and [Queries](Queries.md) for Cascade traversal details.

## Table of contents

- [Creating hierarchies](#creating-hierarchies)
- [Get parent and children](#get-parent-and-children)
- [Cascade delete](#cascade-delete)
- [Depth-first traversal](#depth-first-traversal)
- [Breadth-first traversal](#breadth-first-traversal)
- [Hierarchical names](#hierarchical-names)
- [OrderedChildren](#orderedchildren)
- [Reparenting](#reparenting)
- [Ancestor traversal](#ancestor-traversal)
- [Not yet ported](#not-yet-ported)

---

## Creating hierarchies

A parent-child link is an ordinary ChildOf relationship pair. Use `MakePair(w.ChildOf(), parent)` as the pair ID and add it to the child:

```go
w := flecs.New()

var spaceship, cockpit flecs.ID
w.Write(func(fw *flecs.Writer) {
    spaceship = fw.NewEntity()
    cockpit   = fw.NewEntity()

    // cockpit is a child of spaceship
    fw.AddID(cockpit, flecs.MakePair(w.ChildOf(), spaceship))
})

w.Read(func(r *flecs.Reader) {
    if flecs.HasID(r, cockpit, flecs.MakePair(w.ChildOf(), spaceship)) {
        // cockpit is a child of spaceship
    }
})
```

An entity can have **at most one ChildOf parent**. Adding a second `(ChildOf, newParent)` pair replaces the first. A parent can have any number of children.

**Cycle prevention (v0.41.0):** `ChildOf` is bootstrapped as [Acyclic](ComponentTraits.md#acyclic). Adding a pair that would create a cycle — for example, making a parent a child of one of its own descendants — panics immediately at `AddID` time with a message identifying the entities involved. This prevents infinite recursion in `EachChild` and related hierarchy walks. (C flecs detects this at traversal time via depth caps; Go flecs rejects it earlier, at write time.)

---

## Get parent and children

### ParentOf

`Reader.ParentOf` returns the direct parent of an entity:

```go
w.Read(func(r *flecs.Reader) {
    parent, ok := r.ParentOf(cockpit)
    // parent == spaceship, ok == true
})
```

Returns `(0, false)` if the entity has no ChildOf relationship.

### EachChild

`Reader.EachChild` iterates over all **direct** children of a parent:

```go
w.Read(func(r *flecs.Reader) {
    r.EachChild(spaceship, func(child flecs.ID) bool {
        // called once per direct child
        return true // return false to stop early
    })
})
```

**Range-over-func (v0.109.0):** `(*Reader).Children` returns an `iter.Seq[ID]` adapter
with identical semantics and full `break` support:

```go
w.Read(func(r *flecs.Reader) {
    for child := range r.Children(spaceship) {
        // use child
    }
})
```

Only direct children are visited. To walk the entire subtree, recurse manually (see [Depth-first traversal](#depth-first-traversal)).

For the `Prefabs` range-over-func adapter (`r.Prefabs(e) iter.Seq[ID]`) see
[PrefabsManual.md § EachPrefab](PrefabsManual.md#eachprefab).

---

## Cascade delete

When a parent is deleted, all of its children are deleted in **depth-last order** (deepest descendants first). This is driven by the general cleanup-policy mechanism (shipped in v0.32.0): `ChildOf` has `(OnDeleteTarget, DeleteAction)` registered at bootstrap, so deleting any entity cascades to all `(ChildOf, entity)` sources:

```go
w := flecs.New()

var spaceship, cockpit, pilot flecs.ID
w.Write(func(fw *flecs.Writer) {
    spaceship = fw.NewEntity()
    cockpit   = fw.NewEntity()
    pilot     = fw.NewEntity()

    fw.AddID(cockpit, flecs.MakePair(w.ChildOf(), spaceship))
    fw.AddID(pilot,   flecs.MakePair(w.ChildOf(), cockpit))
})

w.Write(func(fw *flecs.Writer) {
    fw.Delete(spaceship)
})

w.Read(func(r *flecs.Reader) {
    _ = r.IsAlive(spaceship) // false
    _ = r.IsAlive(cockpit)   // false
    _ = r.IsAlive(pilot)     // false
})
```

The same mechanism applies to any custom relationship — see [Relationships.md § Cleanup policies](Relationships.md) for the full API.

---

## Depth-first traversal

Traverse a subtree depth-first by recursing inside `EachChild`:

```go
var visitDepthFirst func(r *flecs.Reader, e flecs.ID, depth int)
visitDepthFirst = func(r *flecs.Reader, e flecs.ID, depth int) {
    r.EachChild(e, func(child flecs.ID) bool {
        visitDepthFirst(r, child, depth+1)
        return true
    })
}

w.Read(func(r *flecs.Reader) {
    visitDepthFirst(r, spaceship, 0)
})
```

---

## Breadth-first traversal

Use a cached query with `Cascade(w.ChildOf())` to iterate entities in root-first order. This is the standard pattern for propagating parent transforms to children — a parent's result is always processed before its children's:

```go
type Position struct{ X, Y float32 }

w := flecs.New()
posID := flecs.RegisterComponent[Position](w)

// Cascade guarantees root-first depth ordering over the ChildOf hierarchy.
cq := flecs.NewCachedQueryFromTerms(w, flecs.With(posID).Cascade(w.ChildOf()))

cq.Each(func(it *flecs.QueryIter) {
    for _, e := range it.Entities() {
        // process e; its parent has already been processed
        _ = e
    }
})
```

See [Queries — Relationship traversal](Queries.md#relationship-traversal) for `Up`, `SelfUp`, and `Cascade` term modifiers.

---

## Hierarchical names

Entities can be given a name with `World.SetName`. Named entities form a dot-separated path that can be resolved with `Lookup` or `LookupChild`.

Names may not contain `.`; that character is reserved as the path separator. Name uniqueness among siblings is not enforced — `LookupChild` returns the first match when siblings share a name.

### SetName / GetName

Set a name at any point; the name is stored as the built-in `Name` component:

```go
w := flecs.New()

var game, level flecs.ID
w.Write(func(fw *flecs.Writer) {
    game  = fw.NewEntity()
    level = fw.NewEntity()
    fw.AddID(level, flecs.MakePair(w.ChildOf(), game))
})

w.SetName(game,  "Game")
w.SetName(level, "Level1")

w.Read(func(r *flecs.Reader) {
    name, ok := r.GetName(level)
    // name == "Level1", ok == true
})
```

### PathOf

`Reader.PathOf` reconstructs the full dot-separated path from the root:

```go
w.Read(func(r *flecs.Reader) {
    path := r.PathOf(level)
    // path == "Game.Level1"
})
```

If an ancestor is unnamed, the walk stops at the first unnamed node and returns only the path segment below it.

### Lookup

`Reader.Lookup` resolves an absolute dot-separated path:

```go
w.Read(func(r *flecs.Reader) {
    e, ok := r.Lookup("Game.Level1")
    // e == level, ok == true
})
```

Returns `(0, false)` if any path segment is absent. An empty string or a path with empty segments (leading dot, trailing dot, consecutive dots) also returns `(0, false)`.

### LookupChild

`Reader.LookupChild` resolves a single name relative to a parent:

```go
w.Read(func(r *flecs.Reader) {
    e, ok := r.LookupChild(game, "Level1")
    // e == level, ok == true
})
```

Pass `0` as the parent to search the root scope — alive entities with no ChildOf relationship.

> **Entity scoping shipped:** Use `WithinScope` to push a parent so that newly created entities automatically receive `(ChildOf, scope)` without explicit `AddID` calls. See [Entity scoping](#entity-scoping).

---

## OrderedChildren

By default, `EachChild` iterates children in archetype-derived order, which can change when children gain or lose components (moving them between tables). Use the `OrderedChildren` trait to lock a parent into insertion-order iteration.

```go
w.Write(func(fw *flecs.Writer) {
    parent := fw.NewEntity()
    flecs.SetOrderedChildren(w, parent) // or: flecs.AddID(fw, parent, w.OrderedChildren())
    c1 := fw.NewEntity()
    flecs.AddID(fw, c1, flecs.MakePair(w.ChildOf(), parent))
    c2 := fw.NewEntity()
    flecs.AddID(fw, c2, flecs.MakePair(w.ChildOf(), parent))
})
// EachChild always yields c1, c2 — regardless of future component changes on c1/c2.
```

`SetOrderedChildren` is opt-in per parent. After it is applied, any child added, removed, or re-parented updates the ordered list immediately. The iteration list is snapshotted at the start of each `EachChild` call so mutations inside the callback are safe.

If a parent already has children when `SetOrderedChildren` is called, those children are captured in their current archetype order.

Check whether a parent is ordered with `flecs.IsOrderedChildren(scope, parentID)`. JSON marshal/unmarshal preserves the trait transparently.

---

## Reparenting

To move an entity from one parent to another, remove the old ChildOf pair and add the new one:

```go
w.Write(func(fw *flecs.Writer) {
    // move cockpit from spaceship to station
    fw.RemoveID(cockpit, flecs.MakePair(w.ChildOf(), spaceship))
    fw.AddID(cockpit,    flecs.MakePair(w.ChildOf(), station))
})
```

Children of `cockpit` follow automatically — their ChildOf pairs still point to `cockpit`; only `cockpit`'s own pair changes.

---

## Ancestor traversal

`GetUp`, `HasUp`, and `TargetUp` walk the ChildOf chain upward from an entity, searching for a component on an ancestor. All three accept any traversal relationship, not just ChildOf — see [Relationships — Relationship traversal](Relationships.md#relationship-traversal).

The component must be registered with `flecs.RegisterComponent[T](w)` before calling `GetUp[T]`.

### GetUp

`flecs.GetUp[T]` returns the component value from the **closest** entity in the chain (self or ancestor) that locally owns it:

```go
type Zone struct{ Name string }

w := flecs.New()
flecs.RegisterComponent[Zone](w)

var region, city flecs.ID
w.Write(func(fw *flecs.Writer) {
    region = fw.NewEntity()
    flecs.Set(fw, region, Zone{Name: "NorthEast"})

    city = fw.NewEntity()
    fw.AddID(city, flecs.MakePair(w.ChildOf(), region))
})

w.Read(func(r *flecs.Reader) {
    z, ok := flecs.GetUp[Zone](r, city, w.ChildOf())
    // z.Name == "NorthEast", ok == true (inherited from region)
})
```

### HasUp

`flecs.HasUp` reports whether the entity or any ancestor owns the component ID:

```go
zoneID := flecs.RegisterComponent[Zone](w)

w.Read(func(r *flecs.Reader) {
    if flecs.HasUp(r, city, zoneID, w.ChildOf()) {
        // city or one of its ancestors has Zone
    }
})
```

### TargetUp

`flecs.TargetUp` returns the entity (self or ancestor) that directly owns the component:

```go
w.Read(func(r *flecs.Reader) {
    owner, ok := flecs.TargetUp(r, city, zoneID, w.ChildOf())
    // owner == region, ok == true
})
```

---

## Entity scoping

`WithinScope` pushes a parent onto the Writer's scope stack. Every `NewEntity` (and `RangeNew`) call inside the callback automatically receives `(ChildOf, parent)` without an explicit `AddID`:

```go
parent := flecs.NewEntity(fw)
flecs.WithinScope(fw, parent, func(fw *flecs.Writer) {
    child1 := fw.NewEntity() // auto-(ChildOf, parent)
    child2 := fw.NewEntity() // auto-(ChildOf, parent)
})
```

Scopes nest: the inner-most scope wins for entities created during its callback. When the inner `WithinScope` returns, the outer scope is restored:

```go
flecs.WithinScope(fw, parent1, func(fw *flecs.Writer) {
    flecs.WithinScope(fw, parent2, func(fw *flecs.Writer) {
        inner := fw.NewEntity() // (ChildOf, parent2)
    })
    outer := fw.NewEntity() // (ChildOf, parent1) — restored
})
```

For advanced callers who need to cross function boundaries where a closure is awkward, `PushScope` / `PopScope` provide the same semantics:

```go
prev := flecs.PushScope(fw, parent)
// ... create entities ...
flecs.PopScope(fw, prev)
```

`GetScope` returns the current top of the stack (zero if no scope is active):

```go
flecs.WithinScope(fw, parent, func(fw *flecs.Writer) {
    current := flecs.GetScope(fw) // == parent
})
```

**Opt-out:** `MakeAlive` ignores the scope (explicit ID claim bypasses auto-ChildOf, mirroring the Phase 16.16 range-bypass precedent). `RangeNew` respects the scope (fresh allocation, range-constrained).

**Stack lifetime:** the scope stack is per-Writer and is reset to empty at the start of each top-level `w.Write(...)` call. Nested `w.Write` calls from the same goroutine share the Writer and therefore its stack.

---

## Parent storage

By default, every distinct `(ChildOf, target)` pair produces its own archetype table
(fragmenting mode). For hierarchies with many parents and children that share the same
other components, this can create thousands of tables and hurt cache efficiency.

**Parent storage** is an opt-in mode that collapses all children of any parent — as
long as their other components are identical — into **one** archetype table. The
concrete parent is stored in a per-row column rather than the pair signature.

### Enabling parent storage

Parent storage is supported on any relationship that is declared both `Exclusive` and
`Relationship`. The built-in `ChildOf` satisfies both:

```go
// Enable for a custom relationship:
flecs.SetRelationship(w, MyRel)
flecs.SetExclusive(w, MyRel)
flecs.SetParentStorage(w, MyRel)

// Enable for the built-in ChildOf:
flecs.SetParentStorage(w, w.ChildOf())
```

`SetParentStorage` panics if the relationship is not Exclusive, not a Relationship, or
if any entity currently carries a `(relID, *)` pair (must be called before entities are
added).

### Reparenting is O(1)

Changing the parent of an entity is a single column write — no archetype migration:

```go
// With parent storage active, this is O(1):
flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), newParent))
```

### Querying parent-storage hierarchies

All query APIs work transparently. For specific-target terms the query engine switches
to per-entity mixed mode, checking the parent column per row:

```go
// Specific parent — mixed-mode per-entity filter:
q := flecs.NewQueryFromTerms(w,
    flecs.With[Position](),
    flecs.WithPair(w.ChildOf(), parentA),
)

// Wildcard — matches all children regardless of parent:
q2 := flecs.NewQueryFromTerms(w,
    flecs.With[Position](),
    flecs.WithPair(w.ChildOf(), w.Any()),
)

// Target variable — binds $Parent per entity:
q3 := flecs.NewQueryFromTerms(w,
    flecs.With[Position](),
    flecs.WithPairTgtVar(w.ChildOf(), "$Parent"),
)
```

`EachChild`, `ParentOf`, `GetUp`, `HasUp`, `HasID`, `RemoveID`, observers, monitor
observers, cleanup policies (`OnDeleteTarget`), snapshot, and JSON round-trips all
behave identically to the default fragmenting mode.

### Checking whether a relationship uses parent storage

```go
if flecs.IsParentStorage(r, w.ChildOf()) {
    fmt.Println("ChildOf uses parent storage")
}
```

---

## See Also

- [Quickstart](Quickstart.md) — hands-on introduction to ChildOf hierarchies.
- [Relationships.md](Relationships.md) — the `ChildOf` and `IsA` built-in relationships in full detail.
- [PrefabsManual.md](PrefabsManual.md) — `IsA` prototype inheritance; prefabs and hierarchies compose.
- [Queries.md](Queries.md) — `Cascade` and `Up` traversal terms for querying into hierarchies.
- [Manual](Manual.md) — top-level reference hub with world lifecycle, concurrency model, and concept map.
