# Relationships

Entity relationships make it possible to describe entity graphs natively in ECS. A relationship adds a *pair* of two entities to a source entity — the first element is the **relationship** (e.g. "Eats") and the second is the **target** (e.g. "Apples"). This lets you express hierarchies, inventory systems, trade links, or any other directed association between entities.

See the [Quickstart](Quickstart.md) for a hands-on introduction, [EntitiesComponents](EntitiesComponents.md) for pair-ID encoding details, and [Queries](Queries.md) for relationship matching in query terms.

## Table of contents

- [Definitions](#definitions)
- [Introduction](#introduction)
- [Pair IDs](#pair-ids)
- [Relationship queries](#relationship-queries)
- [Relationship components](#relationship-components)
- [Inspecting relationships](#inspecting-relationships)
- [Built-in relationships](#built-in-relationships)
  - [IsA](#the-isa-relationship)
  - [ChildOf](#the-childof-relationship)
- [Relationship traversal](#relationship-traversal)
- [Relationship traits](#relationship-traits)
- [Relationship performance](#relationship-performance)

---

## Definitions

| Name         | Description |
|--------------|-------------|
| ID           | A 64-bit value that can be added to and removed from an entity |
| Component    | An ID whose value maps to a registered Go type |
| Pair         | An ID encoding two entity IDs: a relationship and a target |
| Tag          | A component or pair not associated with data |
| Relationship | The first element of a pair |
| Target       | The second element of a pair |
| Source       | The entity to which an ID is added |

---

## Introduction

The following example creates a simple relationship between two entities:

```go
w := flecs.New()

var likes, bob, alice flecs.ID
w.Write(func(fw *flecs.Writer) {
    likes = fw.NewEntity() // relationship
    bob   = fw.NewEntity()
    alice = fw.NewEntity()

    // Bob likes Alice — construct the pair ID and add it as a tag.
    fw.AddID(bob, flecs.MakePair(likes, alice))
})

w.Write(func(fw *flecs.Writer) {
    // Bob likes Alice no more.
    fw.RemoveID(bob, flecs.MakePair(likes, alice))
})
```

In this example `bob` is the *source*, `likes` is the *relationship*, and `alice` is the *target*. A relationship combined with a target is called a **relationship pair**.

The same relationship can be added multiple times as long as its target differs:

```go
w := flecs.New()

var eats, bob, apples, pears flecs.ID
w.Write(func(fw *flecs.Writer) {
    eats   = fw.NewEntity()
    bob    = fw.NewEntity()
    apples = fw.NewEntity()
    pears  = fw.NewEntity()

    fw.AddID(bob, flecs.MakePair(eats, apples))
    fw.AddID(bob, flecs.MakePair(eats, pears))
})

w.Read(func(r *flecs.Reader) {
    fmt.Println(flecs.HasID(r, bob, flecs.MakePair(eats, apples))) // true
    fmt.Println(flecs.HasID(r, bob, flecs.MakePair(eats, pears)))  // true
})
```

---

## Pair IDs

A pair ID is a regular 64-bit `flecs.ID` value with `flecs.FlagPair` set in the top nibble. Construct one with `flecs.MakePair`:

```go
pairID := flecs.MakePair(rel, tgt)
```

Because a pair ID is just an ID, the same operations that work on regular component/tag IDs work on pairs:

```go
w.Write(func(fw *flecs.Writer) {
    // Add a tag pair (no data payload).
    fw.AddID(e, flecs.MakePair(rel, tgt))

    // Remove it.
    fw.RemoveID(e, flecs.MakePair(rel, tgt))
})

w.Read(func(r *flecs.Reader) {
    // Test for presence.
    has := flecs.HasID(r, e, flecs.MakePair(rel, tgt))
    _ = has
})
```

See [Relationship components](#relationship-components) for the typed `SetPair[T]` / `GetPair[T]` API when the pair carries data.

---

## Relationship queries

### Test if an entity has a pair

```go
w.Read(func(r *flecs.Reader) {
    has := flecs.HasID(r, bob, flecs.MakePair(eats, apples))
    _ = has
})
```

### Get the parent of an entity

`ParentOf` returns the target of the first `(ChildOf, *)` pair on the entity:

```go
w.Read(func(r *flecs.Reader) {
    parent, ok := r.ParentOf(child)
    _, _ = parent, ok
})
```

### Iterate all children of a parent

```go
w.Read(func(r *flecs.Reader) {
    r.EachChild(parent, func(child flecs.ID) bool {
        // process child
        return true // return false to stop early
    })
})
```

### Find all entities with a specific pair

Use `NewQueryFromTerms` with a `With(MakePair(rel, tgt))` term to find every entity that has a given pair:

```go
q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(eats, apples)))
it := q.Iter()
for it.Next() {
    for _, e := range it.Entities() {
        _ = e // entity has (Eats, Apples)
    }
}
```

### Wildcard pair queries

Use `w.Wildcard()` (`*`) or `w.Any()` (`_`) in the target or relationship slot of a pair term to match an entire relationship family without enumerating concrete targets.

```go
// One iterator row per concrete (Likes, X) pair per matched table.
q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(likesID, w.Wildcard())))
it := q.Iter()
for it.Next() {
    concrete := flecs.MatchedTarget(it, 0) // Bob, then Alice, etc.
    for _, e := range it.Entities() {
        _ = e
        _ = concrete
    }
}

// Any: exactly one row per entity that has any (Likes, X) pair.
q2 := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(likesID, w.Any())))

// Wildcard in relationship position: any (X, Bob) pair.
q3 := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(w.Wildcard(), bobID)))
```

See [`docs/Queries.md`](Queries.md) § *Wildcard and Any query terms* for the full accessor reference.

---

## Relationship components

Pair IDs are not limited to tags. When at least one element of a pair is a registered component type, the pair carries typed data. Use `SetPair[T]`, `GetPair[T]`, and `GetPairRef[T]` for typed access:

```go
type Distance struct{ Meters float32 }

w := flecs.New()

var near, bob, office flecs.ID
w.Write(func(fw *flecs.Writer) {
    near   = fw.NewEntity()
    bob    = fw.NewEntity()
    office = fw.NewEntity()

    // Store data on the (Near, Office) pair.
    flecs.SetPair(fw, bob, near, office, Distance{Meters: 500})
})

w.Read(func(r *flecs.Reader) {
    d, ok := flecs.GetPair[Distance](r, bob, near, office)
    if ok {
        fmt.Printf("distance: %.0f m\n", d.Meters) // distance: 500 m
    }
})
```

`GetPairRef[T]` returns a direct pointer into the storage slot, useful for in-place mutation inside a Read scope (the pointer is valid until the next structural change):

```go
w.Read(func(r *flecs.Reader) {
    ptr := flecs.GetPairRef[Distance](r, bob, near, office)
    if ptr != nil {
        fmt.Printf("ref value: %.0f m\n", ptr.Meters)
    }
})
```

### Data-pair type rules

The storage determines which Go type is associated with a pair by inspecting both elements in order:

1. If neither element is a registered component, the pair is a tag.
2. If the first element is a registered component, the pair's type is that component type.
3. If only the second element is a registered component, the pair's type is that component type.

### Adding a component multiple times via pairs

A limitation of plain components is that each type can only appear once per entity. Pairs remove this limitation: the same component type can appear multiple times as long as the pair's target differs:

```go
type Position struct{ X, Y float32 }

w := flecs.New()

var first, second, third, e flecs.ID
w.Write(func(fw *flecs.Writer) {
    first  = fw.NewEntity()
    second = fw.NewEntity()
    third  = fw.NewEntity()
    e      = fw.NewEntity()

    // Attach Position data to e three times, once per target.
    flecs.SetPair(fw, e, first,  first,  Position{X: 1, Y: 2})
    flecs.SetPair(fw, e, second, second, Position{X: 3, Y: 4})
    flecs.SetPair(fw, e, third,  third,  Position{X: 5, Y: 6})
})
```

---

## Inspecting relationships

To enumerate all IDs (including pairs) currently on an entity, call `r.EntityComponents`:

```go
w.Read(func(r *flecs.Reader) {
    for _, id := range r.EntityComponents(bob) {
        if id.IsPair() {
            rel := id.First()  // 28-bit relationship index
            tgt := id.Second() // 32-bit target index
            _, _ = rel, tgt
        }
    }
})
```

Note that `id.First()` and `id.Second()` return the raw index portions of the ID. Compare against `w.ChildOf().Index()`, `w.IsA().Index()`, or your relationship entity's index to identify known relationships.

---

## Built-in relationships

Flecs ships two built-in relationships: `IsA` and `ChildOf`. Both are implemented as regular entities whose IDs are available on the `*World`.

### The IsA relationship

`w.IsA()` is the built-in relationship that expresses that one entity is a *specialisation* of another. This is Go flecs's prefab / prototype mechanism: components added to the prefab are *inherited* by its instances. See also [isa.go](../isa.go) for the implementation.

```go
apple := fw.NewEntity()
fruit := fw.NewEntity()

// Apple is a kind of Fruit.
fw.AddID(apple, flecs.MakePair(w.IsA(), fruit))
```

#### Component sharing (inheritance)

An entity with `(IsA, prefab)` inherits the prefab's components. The Go port implements copy-on-write semantics: `Get` walks the IsA chain on a local miss; `Set` always writes locally (override):

```go
type MaxSpeed struct{ Value float32 }
type Defense  struct{ Value float32 }

w := flecs.New()
flecs.RegisterComponent[MaxSpeed](w)
flecs.RegisterComponent[Defense](w)

var spaceship, frigate flecs.ID
w.Write(func(fw *flecs.Writer) {
    spaceship = fw.NewEntity()
    flecs.Set(fw, spaceship, MaxSpeed{Value: 100})
    flecs.Set(fw, spaceship, Defense{Value: 50})

    frigate = fw.NewEntity()
    fw.AddID(frigate, flecs.MakePair(w.IsA(), spaceship))
    flecs.Set(fw, frigate, Defense{Value: 75}) // override Defense
})

w.Read(func(r *flecs.Reader) {
    // MaxSpeed inherited from Spaceship.
    ms, _ := flecs.Get[MaxSpeed](r, frigate)
    fmt.Println(ms.Value) // 100

    // Defense overridden on Frigate.
    def, _ := flecs.Get[Defense](r, frigate)
    fmt.Println(def.Value) // 75
})
```

For a component to be inherited through an IsA link, call `flecs.SetInheritable[T](w)` after registering it. See [Queries.md — Inheritable components](Queries.md#inheritable-components) for details and [PrefabsManual.md](PrefabsManual.md) for the full prefab reference.

#### Iterating prefabs

Use `r.EachPrefab` to enumerate the direct IsA targets of an entity:

```go
w.Read(func(r *flecs.Reader) {
    r.EachPrefab(apple, func(prefab flecs.ID) bool {
        _ = prefab // fruit, ...
        return true
    })
})
```

#### Transitivity of IsA

IsA is transitive: if `GrannySmith IsA Apple` and `Apple IsA Fruit`, then `GrannySmith` is considered a `Fruit` as well. `Get` and `Has` walk the full chain automatically.

Custom relationships can also be made transitive using `flecs.SetTransitive` (see [Transitive trait in ComponentTraits.md](ComponentTraits.md#transitive)). For IsA specifically, `Get`, `Has`, `GetUp`, and `HasUp` already walk the chain regardless of the Transitive flag.

---

### The ChildOf relationship

`w.ChildOf()` is the built-in relationship for entity hierarchies. Adding `(ChildOf, parent)` to an entity makes it a child of `parent`. When a parent is deleted, all its `ChildOf` children are deleted recursively. See also [childof.go](../childof.go) and [HierarchiesManual.md](HierarchiesManual.md) for the full hierarchy reference.

```go
var spaceship, cockpit flecs.ID
w.Write(func(fw *flecs.Writer) {
    spaceship = fw.NewEntity()
    cockpit   = fw.NewEntity()

    fw.AddID(cockpit, flecs.MakePair(w.ChildOf(), spaceship))
})
```

#### Iterating children and getting a parent

```go
// Iterate all direct children.
w.Read(func(r *flecs.Reader) {
    r.EachChild(spaceship, func(child flecs.ID) bool {
        _ = child
        return true
    })
})

// Get the parent of an entity.
w.Read(func(r *flecs.Reader) {
    parent, ok := r.ParentOf(cockpit)
    _, _ = parent, ok
})
```

#### Namespacing

Named entities support hierarchical path lookup. The separator is `"."`:

```go
var parent, child flecs.ID
w.Write(func(fw *flecs.Writer) {
    parent = fw.NewEntity()
    child  = fw.NewEntity()
    w.SetName(parent, "Spaceship")
    w.SetName(child, "Cockpit")
    fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))
})

w.Read(func(r *flecs.Reader) {
    // Absolute path lookup.
    found, ok := r.Lookup("Spaceship.Cockpit")
    _ = found // == child
    _ = ok    // true

    // Relative lookup from parent.
    found2, ok2 := r.LookupChild(parent, "Cockpit")
    _ = found2 // == child
    _ = ok2    // true
})
```

#### Entity scoping

> **Not yet ported in Go flecs.**
> C flecs provides `ecs_set_scope` / `ecs_get_scope` so that all entities created
> within a scope automatically receive a `(ChildOf, scope)` pair without each
> `ecs_new` call having to add it explicitly. This convenience API is not yet
> available in the Go port.
> See the [Entity scoping gap](README.md#feature-gap-list-candidate-follow-up-issues).

---

## Relationship traversal

Relationships create graphs. Go flecs provides `GetUp`, `HasUp`, and `TargetUp` to walk a relationship chain upward from an entity.

### GetUp — inherit a component from an ancestor

`GetUp[T](r, e, rel)` walks the `rel` chain from `e` upward and returns the first `T` component found on any ancestor:

```go
type Tag struct{}

w := flecs.New()
flecs.RegisterComponent[Tag](w)
flecs.SetInheritable[Tag](w)
tagID := flecs.RegisterComponent[Tag](w)

var parent, child flecs.ID
w.Write(func(fw *flecs.Writer) {
    parent = fw.NewEntity()
    flecs.Set(fw, parent, Tag{})

    child = fw.NewEntity()
    fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))
})

w.Read(func(r *flecs.Reader) {
    v, ok := flecs.GetUp[Tag](r, child, w.ChildOf())
    _ = v
    fmt.Println(ok) // true — inherited from parent
})
```

Any entity can be used as the traversal relationship — not only `ChildOf`.

### HasUp — check whether a component exists in the chain

```go
w.Read(func(r *flecs.Reader) {
    has := flecs.HasUp(r, child, tagID, w.ChildOf())
    fmt.Println(has) // true
})
```

### TargetUp — find the ancestor that owns a component

`TargetUp` returns the entity in the chain that *directly owns* the component:

```go
w.Read(func(r *flecs.Reader) {
    owner, ok := flecs.TargetUp(r, child, tagID, w.ChildOf())
    fmt.Println(ok)          // true
    fmt.Println(owner == parent) // true
})
```

### Traversal in queries

Query terms can traverse a relationship using `.Up(rel)`, `.SelfUp(rel)`, or `.Cascade(rel)`:

```go
type Position struct{ X, Y float32 }

w := flecs.New()
posID := flecs.RegisterComponent[Position](w)

// Up: match entities whose nearest ancestor (via ChildOf) has Position.
qUp := flecs.NewQueryFromTerms(w, flecs.With(posID).Up(w.ChildOf()))

// SelfUp: match entities that own Position locally OR inherit it.
qSelfUp := flecs.NewQueryFromTerms(w, flecs.With(posID).SelfUp(w.ChildOf()))

// Cascade: iterate parent-before-child in depth order (CachedQuery only).
cq := flecs.NewCachedQueryFromTerms(w, flecs.With(posID).Cascade(w.ChildOf()))

_, _, _ = qUp, qSelfUp, cq
```

For full traversal examples see [Queries.md — Traversal](Queries.md#traversal).

---

## Relationship traits

Relationship traits are components added to relationship *entities* to change their behaviour. Most traits from the C flecs implementation are not yet ported to Go.

### Exclusive

An exclusive relationship enforces that an entity can have **at most one target** for that relationship. Adding a second target automatically replaces the first. Useful for state machines and other single-valued relationships.

Use `flecs.SetExclusive(w, relID)` to mark a custom relationship as exclusive, or the equivalent bare-tag form `fw.AddID(relID, w.Exclusive())`:

```go
w := flecs.New()
var marriedTo, bob, alice, carol flecs.ID
w.Write(func(fw *flecs.Writer) {
    marriedTo = fw.NewEntity()
    bob = fw.NewEntity()
    alice = fw.NewEntity()
    carol = fw.NewEntity()
})

flecs.SetExclusive(w, marriedTo) // or: fw.AddID(marriedTo, w.Exclusive())

w.Write(func(fw *flecs.Writer) {
    fw.AddID(bob, flecs.MakePair(marriedTo, alice))
    // (marriedTo, alice) is now on bob.
})
w.Write(func(fw *flecs.Writer) {
    fw.AddID(bob, flecs.MakePair(marriedTo, carol))
    // (marriedTo, carol) replaces (marriedTo, alice); bob now married to carol only.
})
```

Query `flecs.IsExclusive(w, relID)` to check whether a relationship is exclusive.

The following built-in relationships are exclusive by default: **`ChildOf`**, **`OnDelete`**, **`OnDeleteTarget`**, **`OnInstantiate`**. `IsA` is intentionally NOT exclusive — multiple prefab bases per entity are allowed.

### Symmetric

**Shipped in v0.36.0.** Marking a relationship symmetric causes any `(R, B)` added to entity `A` to be automatically mirrored as `(R, A)` on entity `B`. Removal is mirrored the same way. Useful for inherently undirected relations such as `Friend`, `MarriedTo`, `AlliesWith`, or `Coplanar`.

```go
w := flecs.New()
marriedTo := w.Write(func(fw *flecs.Writer) flecs.ID { return fw.NewEntity() })
// or alternatively: flecs.SetSymmetric(w, marriedToID)
flecs.SetSymmetric(w, marriedTo)

var bob, alice flecs.ID
w.Write(func(fw *flecs.Writer) {
    bob = fw.NewEntity()
    alice = fw.NewEntity()
    fw.AddID(bob, flecs.MakePair(marriedTo, alice))
    // alice now automatically has (marriedTo, bob)
})

w.Read(func(fr *flecs.Reader) {
    _ = fr.HasID(bob, flecs.MakePair(marriedTo, alice))   // true
    _ = fr.HasID(alice, flecs.MakePair(marriedTo, bob))   // true — mirrored
})
```

The bare-tag form is equivalent:

```go
fw.AddID(marriedTo, w.Symmetric())
```

Query whether a relationship is symmetric with `flecs.IsSymmetric(w, relID)`.

**Loop guard:** the mirror is idempotent — adding `(R, B)` to `A` mirrors `(R, A)` to `B`, which would try to mirror `(R, B)` back to `A`, but `A` already has it, so the recursion short-circuits in one extra hop.

**Self-pairs:** `AddID(a, MakePair(R, a))` adds a single pair; no duplication occurs.

**Interaction with Exclusive:** when `R` is both symmetric and exclusive, replacing `(R, X)` with `(R, B)` on `A` also mirrors `(R, A)` to `B`; if `B` held a conflicting `(R, Y)`, the exclusive constraint replaces it with `(R, A)` on `B` as well.

### Transitive

**Shipped in v0.37.0.** Use `flecs.SetTransitive(w, relID)` to enable transitive query matching on a custom relationship. If `(R, B)` is on entity `A` and `(R, C)` is on entity `B`, a query for `(R, C)` also matches `A`. Formally: `aRb ∧ bRc ⇒ aRc`.

```go
// LocatedIn example: Manhattan LocatedIn NewYork, NewYork LocatedIn USA.
// Query for (LocatedIn, USA) matches both Manhattan and NewYork.
w := flecs.New()
var locatedIn, manhattan, newYork, usa flecs.ID
w.Write(func(fw *flecs.Writer) {
    locatedIn = fw.NewEntity()
    manhattan  = fw.NewEntity()
    newYork    = fw.NewEntity()
    usa        = fw.NewEntity()
})
flecs.SetTransitive(w, locatedIn)
w.Write(func(fw *flecs.Writer) {
    fw.AddID(manhattan, flecs.MakePair(locatedIn, newYork))
    fw.AddID(newYork, flecs.MakePair(locatedIn, usa))
})

// Query for all things "in USA" — transitively includes manhattan and newYork.
q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(locatedIn, usa)))
q.Each(func(it *flecs.QueryIter) {
    for _, e := range it.Entities() {
        _ = e // manhattan and newYork
    }
})
```

The bare-tag form is equivalent:

```go
fw.AddID(locatedIn, w.Transitive())
```

Query whether a relationship is transitive with `flecs.IsTransitive(w, relID)`.

**Cycle detection:** the walk uses a visited set to prevent infinite loops on cyclic chains.

**Depth limit:** chains are bounded by an internal depth cap (64 hops); chains exceeding the limit are silently truncated — no panic occurs.

**Cached query staleness:** `CachedQuery` evaluates transitive chains at construction and on every new table creation. It does **not** re-evaluate when an intermediate entity's pairs mutate after the cache is built. This is documented and accepted; pair-mutation invalidation is a future enhancement.

**Transitive does not imply Reflexive:** an entity with no direct `(R, self)` pair is not auto-matched just because a chain terminates at itself. Reflexive is a separate unported trait.

**Wildcard interaction:** wildcard query terms compose correctly with Transitive. A term `(R, Wildcard)` where `R` is transitive will match tables that have any `(R, X)` pair — including tables that reach the target through a chain — and will emit one row per concrete direct pair in each matched table.

### Traversable

> **Not yet ported in Go flecs.**
> `EcsTraversable` marks a relationship as safe to traverse in queries. In the Go port
> any entity can be used as a traversal relationship with `Up`/`SelfUp`/`Cascade`
> without explicit registration; the formal trait and its safety checks are not ported.

### Cleanup policies

**Shipped in v0.32.0.** Go flecs supports configurable cleanup policies via the `OnDelete` and `OnDeleteTarget` trait relationships.

- **`OnDelete`** governs what happens to source entities when the relationship or component entity itself is deleted.
- **`OnDeleteTarget`** governs what happens to source entities when a *target* entity is deleted.

Actions: `DeleteAction` (cascade-delete sources), `PanicAction` (panic if sources exist), `RemoveAction` (default — remove the pair from sources without deleting them).

`ChildOf` has `(OnDeleteTarget, DeleteAction)` registered by default in bootstrap, which is what drives the parent-cascade-delete behavior. IsA does **not** get a default `OnDeleteTarget` policy (matching C); see `docs/PrefabsManual.md` for the opt-in recipe.

```go
w := flecs.New()

var likesID flecs.ID
w.Write(func(fw *flecs.Writer) { likesID = fw.NewEntity() })

// When a liked target is deleted, delete all entities that liked it.
flecs.SetCleanupPolicy(w, likesID, w.OnDeleteTarget(), w.DeleteAction())

// Or via pair-add (equivalent):
w.Write(func(fw *flecs.Writer) {
    fw.AddID(likesID, flecs.MakePair(w.OnDeleteTarget(), w.DeleteAction()))
})

// Read back the registered policy.
action, ok := flecs.GetCleanupPolicy(w, likesID, w.OnDeleteTarget())
// action == w.DeleteAction(), ok == true
```

### PairIsTag

> **Not yet ported in Go flecs.**
> `EcsPairIsTag` forces a relationship's pairs to behave as tags regardless of whether
> an element is a component. The built-in `ChildOf` uses this internally. Custom
> relationships cannot yet opt into this trait.
> See the [PairIsTag trait gap](README.md#feature-gap-list-candidate-follow-up-issues).

---

## Relationship performance

### How pairs are stored

A pair ID is two entity indices encoded into a single 64-bit `flecs.ID` with `FlagPair` set. At the storage level pairs are treated identically to regular component IDs: adding or removing a pair has the same O(1) cost as adding or removing a plain component.

The type of data associated with a pair is determined by the rules in [Relationship components](#relationship-components).

### Fragmentation

Archetype-based ECS stores entities that share the same set of IDs together in a table. Adding different pair targets to different entities creates more table combinations (one archetype per unique type set), which can increase fragmentation:

- More tables means more work at table creation time and more tables for queries to match.
- Flecs is optimised for large numbers of tables (hundreds of thousands), but fragmentation remains a factor for applications that add many distinct pair combinations to individual entities.

### ID ranges

Pair IDs are never in the low-ID range that Flecs reserves for built-in components. Adding or removing a pair always looks up the next archetype via a hashmap (rather than a direct array index), which introduces a small overhead — typically 5–10 % of the total cost of an add/remove operation.

### Wildcard query performance

Wildcard matching is O(pairs\_with\_R\_in\_table) per matched table: at table-admission time `tableHasWildcardMatch` scans the table's type for any `(R, X)` pair (early exit on first hit), and at expansion time `wildcardMatchingPairs` collects all concrete matches.

- `(Rel, Wildcard)` — linear scan of the table type for all `(Rel, X)` pairs. Cost proportional to the number of distinct targets for that relationship in the table.
- `(Wildcard, Target)` — linear scan for all `(X, Target)` pairs. Same O(pairs) cost.
- `(Wildcard, Wildcard)` — all pair IDs in the table; emits one row per pair.
- `(Rel, Any)` — early exit after the first `(Rel, X)` hit; O(1) in the best case.

All tables are scanned at query construction when a wildcard term is present (no component-index shortcut, since the sentinel ID is never registered as a concrete component).

---

## See Also

- [Quickstart](Quickstart.md) — hands-on introduction covering `ChildOf` and `IsA` relationships.
- [HierarchiesManual.md](HierarchiesManual.md) — parent/child tree traversal and named paths.
- [PrefabsManual.md](PrefabsManual.md) — `IsA` prototype inheritance and copy-on-write override.
- [Queries.md](Queries.md) — traversal terms (`Up`, `SelfUp`, `Cascade`) in query expressions.
- [Manual](Manual.md) — top-level reference hub with world lifecycle, concurrency model, and concept map.
