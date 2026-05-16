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
  - [Exclusive](#exclusive)
  - [Union](#union)
  - [Symmetric](#symmetric)
  - [Transitive](#transitive)
  - [Reflexive](#reflexive)
  - [Acyclic](#acyclic)
  - [OneOf](#oneof)
  - [Traversable](#traversable)
  - [Cleanup policies](#cleanup-policies)
  - [PairIsTag](#pairstag)
  - [ParentStorage](#parentstorage)
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

#### Sealing an IsA target with Final

The `Final` trait is the inverse boundary to IsA inheritance: marking an entity Final prevents it from ever being used as an `IsA` target. Attempting `fw.AddID(instance, MakePair(w.IsA(), finalEntity))` panics at write time (immediate or deferred). See [ComponentTraits.md § Final](ComponentTraits.md#final) and [PrefabsManual.md § Sealing prefabs with Final](PrefabsManual.md#sealing-prefabs-with-final) for details.

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

Query terms can traverse a relationship using `.Up(rel)`, `.SelfUp(rel)`, or `.Cascade(rel)`. The traversal relationship must be registered as **Traversable** (see [ComponentTraits.md § Traversable](ComponentTraits.md#traversable)). The built-in `ChildOf` and `IsA` relationships are always Traversable; custom relationships require an explicit `SetTraversable` call:

```go
type Position struct{ X, Y float32 }

w := flecs.New()
posID := flecs.RegisterComponent[Position](w)

// Built-in relationships are always traversable — no registration needed.
qUp := flecs.NewQueryFromTerms(w, flecs.With(posID).Up(w.ChildOf()))
qSelfUp := flecs.NewQueryFromTerms(w, flecs.With(posID).SelfUp(w.ChildOf()))
cq := flecs.NewCachedQueryFromTerms(w, flecs.With(posID).Cascade(w.ChildOf()))
_, _, _ = qUp, qSelfUp, cq

// Custom traversal relationships must be registered first.
var containedBy flecs.ID
w.Write(func(fw *flecs.Writer) { containedBy = fw.NewEntity() })
flecs.SetTraversable(w, containedBy)                              // required since v0.46.0
q := flecs.NewQueryFromTerms(w, flecs.With(posID).Up(containedBy))
_ = q
```

For full traversal examples see [Queries.md — Relationship Traversal](Queries.md#relationship-traversal).

---

## Relationship traits

Relationship traits are components added to relationship *entities* to change their behaviour. Several C flecs traits are not yet ported to Go; see [ComponentTraits.md](ComponentTraits.md) for the full roadmap.

> **Usage-constraint traits (Phase 15.15 / v0.47.0):** Use `flecs.SetRelationship(w, relID)` to declare that an entity may only appear as the *relationship* (first element) of a pair. Use `flecs.SetTarget(w, tgtID)` to declare that an entity may only appear as the *target* (second element). Use `flecs.SetTrait(w, id)` to exempt an entity from `Relationship`'s no-target-slot check (this is how `ChildOf` and `IsA` can still appear as pair targets). Write-time enforcement panics with a clear message naming the entity and the violated constraint. See [ComponentTraits.md § Relationship / Target / Trait](ComponentTraits.md#relationship--target--trait) for the full API and examples.

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

### Union

**Shipped in v0.54.0.** `Union` is like `Exclusive` — at most one target per entity — but pairs are stored in a **per-relationship side map** instead of the archetype table. Adding or changing the target never triggers an archetype transition, eliminating the table fragmentation that appears when many entities cycle through many targets.

```go
w := flecs.New()
var Movement, Walking, Running flecs.ID
w.Write(func(fw *flecs.Writer) {
    Movement = fw.NewEntity()
    Walking  = fw.NewEntity()
    Running  = fw.NewEntity()
})

flecs.SetUnion(w, Movement) // store in union side map, not archetype

var e flecs.ID
w.Write(func(fw *flecs.Writer) {
    e = fw.NewEntity()
    fw.AddID(e, flecs.MakePair(Movement, Walking))
})
w.Write(func(fw *flecs.Writer) {
    fw.AddID(e, flecs.MakePair(Movement, Running)) // replaces Walking, no archetype transition
})

w.Read(func(fr *flecs.Reader) {
    flecs.HasID(fr, e, flecs.MakePair(Movement, Running))      // true
    flecs.HasID(fr, e, flecs.MakePair(Movement, Walking))      // false
    flecs.HasID(fr, e, flecs.MakePair(Movement, w.Wildcard())) // true — any target held
})
```

**Union vs Exclusive — when to use which:**

| | Exclusive | Union |
|---|---|---|
| At-most-one target | ✅ | ✅ |
| Archetype transition on change | ✅ (entity moves tables) | ❌ (no transition) |
| Data-bearing pairs | ✅ | ❌ (tag-only) |
| Table fragmentation | High when many targets exist | None |
| Query: `(R, T)` filter | Archetype scan | Side-map lookup |

Choose **Exclusive** when pairs carry typed data (`SetPair[T]`) or when target changes are rare. Choose **Union** when many entities cycle through many tag-only targets and table proliferation becomes a concern.

**Remove semantics:** `fw.RemoveID(e, MakePair(R, T))` removes the pair only when T is the currently active target; mismatched removes are a no-op. `fw.RemoveID(e, MakePair(R, w.Wildcard()))` removes the pair unconditionally.

**Conflict with Exclusive:** `SetUnion` panics if the relationship was already registered with `SetExclusive` (and vice versa). Start with `SetUnion` if you want union semantics.

**Hooks:** `OnRemove` fires with the old target before `OnAdd` fires with the new one, matching `Exclusive` behavior.

**Introspection:** `flecs.EachUnion(scope, relID, fn)` iterates all active (entity, target) pairs for a union relationship in insertion order.

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

**Transitive does not imply Reflexive:** an entity with no direct `(R, self)` pair is not auto-matched just because a transitive chain terminates at itself. Use `flecs.SetReflexive(w, relID)` (shipped in v0.39.0) to enable the implicit self-match; the two traits compose cleanly.

**Wildcard interaction:** wildcard query terms compose correctly with Transitive. A term `(R, Wildcard)` where `R` is transitive will match tables that have any `(R, X)` pair — including tables that reach the target through a chain — and will emit one row per concrete direct pair in each matched table.

### Reflexive

**Shipped in v0.39.0.** A reflexive relationship asserts `R(X, X)` — every entity implicitly holds the relationship to itself, without storing an explicit self-pair. The canonical example is `IsA`, which is bootstrapped as reflexive: `IsA(Tree, Tree)` is true even if no such pair was explicitly added.

```go
flecs.SetReflexive(w, locatedInID)
// or bare-tag form:
fw.AddID(locatedInID, w.Reflexive())

// HasID self-pair now returns true even without a stored pair.
r.HasID(city, flecs.MakePair(locatedInID, city)) // → true

// Query for (LocatedIn, city) also matches city itself.
q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(locatedInID, cityID)))
// Iteration includes city's own table in addition to entities with a direct pair.
```

Query whether a relationship is reflexive with `flecs.IsReflexive(w, relID)`.

**HasID divergence from C:** in C flecs, `ecs_has_id` does **not** consult `EcsReflexive`; the trait is query-only. In Go flecs, `HasID(e, MakePair(R, e))` returns `true` when `R` is reflexive, matching the semantic promise already documented in these docs. This is a deliberate, documented extension (see CHANGELOG v0.39.0).

**Reflexive + Transitive composition:** when a relationship has both traits, a query for `(R, target)` matches the target entity itself (Reflexive) **and** all entities that transitively chain to `target` via `R` (Transitive). The two checks are independent and additive.

**Cached query note:** `CachedQuery` evaluates the reflexive self-match at cache construction and on every new-table creation. If the target entity migrates to a different table after the cache is built the cache will not update automatically; staleness is accepted for this phase.

**IsA is reflexive after bootstrap:** any entity `a` satisfies `HasID(a, MakePair(IsA, a))` without storing a pair. Existing tests that expected this to return `false` should be updated if they relied on the pre-v0.39.0 behavior.

### Acyclic

**Shipped in v0.41.0.** Marking a relationship Acyclic prevents cycles from being stored. When `(e, R, target)` is added, the engine walks the `R` chain upward from `target`; if `e` is reachable, the add panics immediately with a clear message identifying the entity, relationship, and target. `ChildOf` is bootstrapped acyclic — mutual parent/child cycles are rejected at write time, closing a correctness gap that could cause `EachChild` to recurse infinitely.

Self-pairs `(e, R, e)` are **allowed**; Acyclic does not reject them. For a self-pair to be implicitly true without storage, combine with [Reflexive](#reflexive).

Use `flecs.SetAcyclic(w, relID)` to mark a relationship as acyclic, or the equivalent bare-tag form `fw.AddID(relID, w.Acyclic())`:

```go
w := flecs.New()
var parentOf, a, b, c flecs.ID
w.Write(func(fw *flecs.Writer) {
    parentOf = fw.NewEntity()
    a = fw.NewEntity()
    b = fw.NewEntity()
    c = fw.NewEntity()
})

flecs.SetAcyclic(w, parentOf) // or: fw.AddID(parentOf, w.Acyclic())

w.Write(func(fw *flecs.Writer) {
    fw.AddID(a, flecs.MakePair(parentOf, b)) // a → b
    fw.AddID(b, flecs.MakePair(parentOf, c)) // b → c
    // fw.AddID(c, flecs.MakePair(parentOf, a)) // would panic: c → b → a → c is a cycle
})

// Check the flag:
flecs.IsAcyclic(w, parentOf) // → true
```

The built-in `ChildOf` is bootstrapped acyclic. The following example panics at the second add:

```go
w := flecs.New()
var parent, child flecs.ID
w.Write(func(fw *flecs.Writer) {
    parent = fw.NewEntity()
    child  = fw.NewEntity()
    fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))  // ok: child → parent
    // fw.AddID(parent, flecs.MakePair(w.ChildOf(), child)) // panics: would create a cycle
})
```

**Enforcement covers both immediate and deferred writes** — the check fires whether the add is inside a `w.Write` callback or batched via a deferred command queue.

**Deliberate divergence from C flecs:** C flecs guards cycles at lookup/traversal time (via `ECS_MAX_RECURSION` and per-function depth caps). The Go port enforces at `AddID` time so that `EachChild` and similar recursors never encounter an infinite chain. Documented in CHANGELOG v0.41.0.

**Composition with Transitive:** Acyclic (write-time) and Transitive (query-time) compose cleanly — preventing storage of a cycle does not interact with transitive chain walking at query time.

Query whether a relationship is acyclic with `flecs.IsAcyclic(w, relID)`.

### OneOf

**Shipped in v0.43.0.**

`OneOf` constrains a relationship's target to entities that are **direct children** (`ChildOf`) of a specified parent. This is the idiomatic way to model enum-style relationships where valid values are a fixed set of named entities parented under a common namespace entity.

**Enum-style pattern — Colors:**

```go
w := flecs.New()

var colorRel, colors, red, green, blue, e flecs.ID
w.Write(func(fw *flecs.Writer) {
    colorRel = fw.NewEntity() // the relationship
    colors   = fw.NewEntity() // the namespace / parent
    red      = fw.NewEntity()
    green    = fw.NewEntity()
    blue     = fw.NewEntity()
    e        = fw.NewEntity()

    // Parent the color values under the colors namespace.
    fw.AddID(red,   flecs.MakePair(w.ChildOf(), colors))
    fw.AddID(green, flecs.MakePair(w.ChildOf(), colors))
    fw.AddID(blue,  flecs.MakePair(w.ChildOf(), colors))
})

// Constrain colorRel: every (colorRel, X) pair must satisfy (ChildOf, colors) on X.
flecs.SetOneOf(w, colorRel, colors)

// Valid add — red is a child of colors.
w.Write(func(fw *flecs.Writer) {
    fw.AddID(e, flecs.MakePair(colorRel, red)) // OK
})

// Invalid add — panics at AddID time.
// w.Write(func(fw *flecs.Writer) {
//     unrelated := fw.NewEntity()
//     fw.AddID(e, flecs.MakePair(colorRel, unrelated)) // panic: OneOf constraint violated
// })
```

**Self-tag form** — when `SetOneOf(w, R, R)` (or `fw.AddID(R, w.OneOf())`), targets must be direct children of `R` itself:

```go
// Self-tag: targets must be children of colorRel directly.
flecs.SetOneOf(w, colorRel, colorRel)
// Equivalent: fw.AddID(colorRel, w.OneOf())
```

**Query the constraint:**

```go
parent, ok := flecs.IsOneOf(w, colorRel)
// parent.Index() == colors.Index(), ok == true
```

`IsOneOf` accepts the `scope` interface, so it works inside both `Read` and `Write` blocks without `AsReader()`.

**Composes with Exclusive** — combining `OneOf` and `Exclusive` on a relationship enforces both constraints: at most one target per entity, and all targets must be valid children of the parent. Replacing `(R, red)` with `(R, blue)` validates the new target before the atomic migration.

**Direct check only** — the constraint checks `(ChildOf, parent)` directly on the target; it does not walk the ChildOf ancestry chain. `Wildcard` and `Any` targets are exempt.

### Traversable

**Shipped in v0.46.0.** `SetTraversable(w, relID)` / `IsTraversable(scope, relID)` / `w.Traversable()` bare-tag form.

`EcsTraversable` marks a relationship as safe to traverse in queries. Only Traversable relationships may appear in `.Up(rel)`, `.SelfUp(rel)`, or `.Cascade(rel)` terms; attempting to use a non-traversable relationship panics at query construction with a message naming both the modifier and the relationship.

Adding Traversable to a relationship also implies Acyclic (write-time cycle rejection). `ChildOf` and `IsA` are bootstrapped Traversable at world creation.

**Migration note:** Existing code that traverses `IsA` or `ChildOf` continues to work without change because both are bootstrapped Traversable. Custom traversal relationships that were previously usable without registration must now call `SetTraversable(w, relID)` before use in traversal queries.

**IsA is now Acyclic (v0.46.0 behavior change):** As a side effect of bootstrapping `IsA` as Traversable, `IsA` is also now Acyclic. Write-time cycle rejection applies to `(IsA, *)` pairs; previously, cycles were caught only at traversal time by the `walkUp` seen-map guard.

### Cleanup policies

**Shipped in v0.32.0.** Go flecs supports configurable cleanup policies via the `OnDelete` and `OnDeleteTarget` trait relationships.

- **`OnDelete`** governs what happens to source entities when the relationship or component entity itself is deleted.
- **`OnDeleteTarget`** governs what happens to source entities when a *target* entity is deleted.

Actions: `DeleteAction` (cascade-delete sources), `PanicAction` (panic if sources exist), `RemoveAction` (default — remove the pair from sources without deleting them).

`ChildOf` has `(OnDeleteTarget, DeleteAction)` registered by default in bootstrap, which is what drives the parent-cascade-delete behavior. IsA does **not** get a default `OnDeleteTarget` policy (matching C); see `docs/PrefabsManual.md` for the opt-in recipe.

#### Cleanup policy observer events {#cleanup-policy-observer-events}

**Shipped in v0.103.0.** Two built-in observer events fire alongside cleanup-policy execution:

- **`EventOnDelete`** fires once per entity about to be deleted, before `OnRemove` hooks. Register via `OnDelete(w, fn)` / `OnDeleteWithOptions(w, opts, fn)`. Handler receives `*Reader`.
- **`EventOnDeleteTarget`** fires once per `(target, dependent, pairRelID)` triple during the cleanup-policy DFS, before the dependent is enqueued. Register via `OnDeleteTarget(w, fn)` / `OnDeleteTargetWithOptions(w, opts, fn)`. Handler receives `*Reader`.

For `PanicAction` policies, both events fire **before** the panic, enabling handlers to log or capture state.

**Component-remove cascade**: when a component entity is deleted with `RemoveAction` (the default), all holder entities undergo archetype migration — `OnRemove` fires per entity and no orphaned signatures remain.

See [docs/ObserversManual.md § OnDelete and OnDeleteTarget](ObserversManual.md#on-delete-and-on-delete-target) for the full API reference, filtering options, and mutation-safety constraints.

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

### ParentStorage

**Shipped in v0.102.0 (Phase 16.47).** `SetParentStorage(w, relID)` enables *non-fragmenting hierarchy storage* for a relationship. By default, each unique `(relID, target)` pair produces a separate archetype table, so N parents × M identically-typed children fragment into N tables. Parent storage collapses all those tables into one by storing the parent ID in a per-row column rather than the archetype signature. Reparenting becomes an O(1) column write instead of an O(component-count) archetype migration.

**Prerequisites:**

- The relationship must already be declared with `SetRelationship` (or be a built-in relationship like `ChildOf`).
- The relationship must be Exclusive (`SetExclusive`) — parent storage stores exactly one parent per entity.
- No entities may already carry the relationship at the time `SetParentStorage` is called; the call panics if any exist.

```go
w := flecs.New()

// Enable parent storage for ChildOf (the most common use-case).
flecs.SetParentStorage(w, w.ChildOf())

// Check whether it is enabled.
flecs.IsParentStorage(w, w.ChildOf()) // → true

// Children of different parents now share one archetype table.
var p1, p2 flecs.ID
w.Write(func(fw *flecs.Writer) {
    p1 = fw.NewEntity()
    p2 = fw.NewEntity()
    for i := 0; i < 50; i++ {
        c := fw.NewEntity()
        fw.AddID(c, flecs.MakePair(w.ChildOf(), p1)) // no table split
    }
    for i := 0; i < 50; i++ {
        c := fw.NewEntity()
        fw.AddID(c, flecs.MakePair(w.ChildOf(), p2)) // same table as p1's children
    }
})
```

**Query behavior is unchanged.** Exact-target queries (`WithPair(ChildOf, p1)`) apply a per-row filter against the parent column; wildcard queries (`WithPair(ChildOf, *)`) scan the single shared table without filtering; variable queries (`WithPairTgtVar(ChildOf, "parent")`) bind the variable to the column value per row. `EachChild`, `ParentOf`, `GetUp`, `HasUp`, `TargetUp`, `PathOf`, `Lookup`, and all traversal queries behave identically to the fragmenting mode.

**Cleanup policies** (`OnDeleteTarget`) also work correctly — the engine scans parent columns instead of pair-target indices when applying delete/remove/panic actions.

**Observers** (`OnAdd`, `OnRemove`, `OnSet`, `OnReplace`, `OnTableCreate`, `OnTableFill`, `OnTableEmpty`) fire correctly for parent-column writes.

**Snapshot round-trip** — both binary (`snapshot.go`) and JSON (`marshal.go`) serialization preserve the parent-storage flag and per-entity parent IDs.

**When to use parent storage:**

| Concern | Fragmenting (default) | Parent storage |
|---|---|---|
| Archetype count with many parents | O(N parents) | O(1) |
| Reparenting cost | O(component-count) migration | O(1) column write |
| Carries typed pair data | ✅ | ✅ |
| At-most-one parent enforced | Requires `Exclusive` | Required (`Exclusive` implied) |
| Multi-parent per entity | ✅ | ❌ (one parent per entity) |
| Runtime mode switch after population | ❌ (fail-loud) | ❌ (fail-loud) |

Choose parent storage for `ChildOf` (or any single-parent hierarchy relationship) when the number of distinct parents is large enough to cause measurable table fragmentation. The canonical opt-in is `flecs.SetParentStorage(w, w.ChildOf())` called once at world-setup time, before any children are added.

See [docs/ParentStorage.md](ParentStorage.md) for the full motivation, API reference, performance characteristics, and limitations. See [docs/HierarchiesManual.md](HierarchiesManual.md) for hierarchy usage patterns.

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
