# Prefabs

The built-in `IsA` relationship provides prototype inheritance. A **prefab** is any entity whose components other entities inherit. Instances are entities that carry an `(IsA, prefab)` pair; `Get` and `Has` walk the IsA chain on a local miss so the instance appears to own the prefab's components without storing a copy. `Set` on an instance creates a local copy (copy-on-write override) that shadows the prefab's value for that instance alone.

See the [Quickstart](Quickstart.md#prefabs-isa) for a hands-on introduction, [Relationships](Relationships.md#the-isa-relationship) for IsA pair encoding and traversal details, and the [Phase 13.1 CHANGELOG entry](../CHANGELOG.md) for the `SetInheritable` specification.

## Table of contents

- [Instantiating a prefab](#instantiating-a-prefab)
- [Component inheritance](#component-inheritance)
- [Owns check](#owns-check)
- [Copy-on-write override](#copy-on-write-override)
- [Restoring inheritance](#restoring-inheritance)
- [Prefab variants](#prefab-variants)
- [Prefab traversal](#prefab-traversal)
- [OnInstantiate traits](#oninstantiate-traits)
- [Prefab hierarchies](#prefab-hierarchies)
- [Prefab slots](#prefab-slots)
- [Not yet ported](#not-yet-ported)

---

## Instantiating a prefab

A prefab is a regular entity. Create it with `fw.NewEntity()`, set components on it, and then add an `(IsA, prefab)` pair to any entity that should inherit from it. Multiple instances can share the same prefab:

```go
type Defense struct{ Value int }

w := flecs.New()
flecs.RegisterComponent[Defense](w)

var spaceship, inst1, inst2 flecs.ID
w.Write(func(fw *flecs.Writer) {
    spaceship = fw.NewEntity()
    flecs.Set(fw, spaceship, Defense{Value: 50})

    inst1 = fw.NewEntity()
    fw.AddID(inst1, flecs.MakePair(w.IsA(), spaceship))

    inst2 = fw.NewEntity()
    fw.AddID(inst2, flecs.MakePair(w.IsA(), spaceship))
})

w.Read(func(r *flecs.Reader) {
    d, ok := flecs.Get[Defense](r, inst1)
    // ok == true; d.Value == 50 — inherited from spaceship
    _ = d
    _ = ok
})
```

Both instances share the value stored in `spaceship`'s archetype table. No copy is made until an instance calls `Set` (see [copy-on-write override](#copy-on-write-override)).

> **Go flecs v0.57.0: Prefab tag shipped.** Call `flecs.MarkPrefab(fw, spaceship)` to add the built-in `Prefab` tag. Ordinary queries then exclude the prefab entity automatically; use `With(w.Prefab())` in any term to opt back in. See [Prefab tag](#prefab-tag) below.

---

## Component inheritance

By default `Get` and `Has` walk the IsA chain on a local miss. To also match inheriting instances in queries (e.g. `Each1`, `NewCachedQuery`), call `SetInheritable[T]` after registering the component. The query engine then auto-promotes terms for that component to `Self|Up(IsA)`:

```go
type Defense struct{ Value int }

w := flecs.New()
flecs.RegisterComponent[Defense](w)
flecs.SetInheritable[Defense](w) // queries match inheritors too

var spaceship, inst flecs.ID
w.Write(func(fw *flecs.Writer) {
    spaceship = fw.NewEntity()
    flecs.Set(fw, spaceship, Defense{Value: 50})

    inst = fw.NewEntity()
    fw.AddID(inst, flecs.MakePair(w.IsA(), spaceship))
    // inst owns no local Defense but is now matched by queries
})

w.Read(func(r *flecs.Reader) {
    var found []flecs.ID
    flecs.Each1[Defense](r, func(e flecs.ID, _ *Defense) {
        found = append(found, e)
    })
    // found contains spaceship (local owner) and inst (inheritor)
    _ = found
})
```

`SetInheritable` must be called after `RegisterComponent[T]` and before any query referencing `T` is built. See the [Phase 13.1 CHANGELOG entry](../CHANGELOG.md) for full details.

---

## Owns check

`Owns[T]` reports whether an entity locally owns a component — it does not walk the IsA chain. Use it to distinguish locally-owned components from inherited ones:

```go
w.Read(func(r *flecs.Reader) {
    ownsLocal := flecs.Owns[Defense](r, inst)
    // ownsLocal == false — Defense is inherited, not locally owned
    _ = ownsLocal
})
```

`OwnsID` is the raw-ID equivalent for use with dynamically-known component IDs.

---

## Copy-on-write override

Calling `Set[T]` on an instance that inherits `T` creates a **local copy** in the instance's archetype. The prefab's value is unaffected; other instances that have not overridden `T` still see the prefab's value:

```go
type Defense struct{ Value int }

w := flecs.New()
flecs.RegisterComponent[Defense](w)
flecs.SetInheritable[Defense](w)

var spaceship, instA, instB flecs.ID
w.Write(func(fw *flecs.Writer) {
    spaceship = fw.NewEntity()
    flecs.Set(fw, spaceship, Defense{Value: 50})

    instA = fw.NewEntity()
    fw.AddID(instA, flecs.MakePair(w.IsA(), spaceship))

    instB = fw.NewEntity()
    fw.AddID(instB, flecs.MakePair(w.IsA(), spaceship))
})

// Override Defense only on instA.
w.Write(func(fw *flecs.Writer) {
    flecs.Set(fw, instA, Defense{Value: 75})
})

w.Read(func(r *flecs.Reader) {
    dA, _ := flecs.Get[Defense](r, instA)
    dB, _ := flecs.Get[Defense](r, instB)
    // dA.Value == 75 (local override)
    // dB.Value == 50 (still inherited from spaceship)
    _ = dA
    _ = dB
})
```

---

## Restoring inheritance

Removing the locally-owned component from an instance restores inheritance: the next `Get` will find the value in the prefab again:

```go
w.Write(func(fw *flecs.Writer) {
    flecs.Remove[Defense](fw, instA)
})

w.Read(func(r *flecs.Reader) {
    d, ok := flecs.Get[Defense](r, instA)
    // ok == true; d.Value == 50 — inherited from spaceship again
    _ = d
    _ = ok
})
```

---

## Prefab variants

A prefab can itself inherit from another prefab. This creates a **variant** that shares the base prefab's data and can selectively override components without duplicating data. `Get` walks the full chain depth-first:

```go
type Health struct{ HP int }
type Defense struct{ Value int }

w := flecs.New()
flecs.RegisterComponent[Health](w)
flecs.RegisterComponent[Defense](w)

var spaceship, freighter, inst flecs.ID
w.Write(func(fw *flecs.Writer) {
    // Base prefab
    spaceship = fw.NewEntity()
    flecs.Set(fw, spaceship, Defense{Value: 50})
    flecs.Set(fw, spaceship, Health{HP: 100})

    // Variant prefab: inherits from spaceship, overrides Health
    freighter = fw.NewEntity()
    fw.AddID(freighter, flecs.MakePair(w.IsA(), spaceship))
    flecs.Set(fw, freighter, Health{HP: 150})

    // Instance of the variant
    inst = fw.NewEntity()
    fw.AddID(inst, flecs.MakePair(w.IsA(), freighter))
})

w.Read(func(r *flecs.Reader) {
    h, _ := flecs.Get[Health](r, inst)
    d, _ := flecs.Get[Defense](r, inst)
    // h.HP   == 150 — from freighter (overrides spaceship)
    // d.Value == 50 — from spaceship (via freighter chain)
    _ = h
    _ = d
})
```

The chain is walked depth-first starting from the innermost `IsA` target. `freighter`'s local `Health` is found before `spaceship`'s, so the variant's value wins.

---

## Prefab traversal

### PrefabOf

`Reader.PrefabOf` returns the first `(IsA, *)` prefab of an entity. The free function `flecs.PrefabOf` is equivalent:

```go
w.Read(func(r *flecs.Reader) {
    prefab, ok := flecs.PrefabOf(r, inst)
    // ok == true; prefab == freighter (the direct IsA target)
    _ = prefab
    _ = ok
})
```

If the entity has multiple IsA pairs the first one in archetype order is returned. Use `EachPrefab` to iterate all of them.

### EachPrefab

`Reader.EachPrefab` iterates every **direct** `(IsA, *)` pair of an entity in archetype order. Return `false` to stop early:

```go
w.Read(func(r *flecs.Reader) {
    r.EachPrefab(inst, func(p flecs.ID) bool {
        _ = p // direct prefab (freighter in the example above)
        return true
    })
})
```

`EachPrefab` is **direct only** — it does not recurse into the prefab's own IsA chain. To walk the full chain, call `EachPrefab` recursively on each yielded prefab.

### GetUp with IsA

`GetUp[T]` explicitly retrieves the component value from the first entity in a traversal chain (self or ancestor) that locally owns it. Passing `w.IsA()` as the traversal relationship is the IsA-aware equivalent of the implicit `Get` chain walk, and makes the traversal relationship explicit:

```go
type Defense struct{ Value int }

w := flecs.New()
flecs.RegisterComponent[Defense](w)

var spaceship, inst flecs.ID
w.Write(func(fw *flecs.Writer) {
    spaceship = fw.NewEntity()
    flecs.Set(fw, spaceship, Defense{Value: 50})
    inst = fw.NewEntity()
    fw.AddID(inst, flecs.MakePair(w.IsA(), spaceship))
})

w.Read(func(r *flecs.Reader) {
    d, ok := flecs.GetUp[Defense](r, inst, w.IsA())
    // ok == true; walks IsA chain even if inst has no local Defense
    _ = d
    _ = ok
})
```

`GetUp[T]` is useful when you want to be explicit about traversal, or when combining it with custom relationships that mirror IsA-like inheritance semantics.

---

## OnInstantiate traits

C flecs exposes three traits for controlling how individual components behave during prefab instantiation, expressed as `(OnInstantiate, Trait)` pairs. The four related entity IDs are accessible via `w.OnInstantiate()`, `w.Inherit()`, `w.Override()`, and `w.DontInherit()`.

### Inherit (supported)

`(OnInstantiate, Inherit)` marks a component as inheritable — instances can read the prefab's value through the IsA chain. The Go port implements this through `SetInheritable[T](w)`, which sets a flag on the component's type metadata. `w.Inherit()` exposes the C flecs entity ID for API symmetry, but `SetInheritable[T]` is the idiomatic Go API.

### Override (shipped v0.33.0)

`(OnInstantiate, Override)` causes a component to be **automatically copied** from the prefab into each new instance at `(IsA, prefab)` add time. The instance gets its own local slot so that mutations to the instance are isolated from the prefab and from sibling instances.

**API:**

```go
type Position struct{ X, Y float32 }

w := flecs.New()
posID := flecs.RegisterComponent[Position](w)
flecs.SetInstantiatePolicy(w, posID, w.Override())

var prefab, inst flecs.ID
w.Write(func(fw *flecs.Writer) {
    prefab = fw.NewEntity()
    flecs.Set(fw, prefab, Position{X: 10, Y: 20})

    inst = fw.NewEntity()
    fw.AddID(inst, flecs.MakePair(w.IsA(), prefab))
    // inst now owns a local copy of Position{X:10, Y:20}
})
```

The pair-add form produces identical results:

```go
w.Write(func(fw *flecs.Writer) {
    fw.AddID(posID, flecs.MakePair(w.OnInstantiate(), w.Override()))
})
```

Multi-level chains are handled transitively: if prefab2 IsA prefab1 and prefab1 has an Override component, instantiating prefab2 copies it too. If instance already owns the component locally before the `(IsA, prefab)` add, the Override copy is skipped (user value wins).

### DontInherit (shipped v0.33.0)

`(OnInstantiate, DontInherit)` prevents a specific component from being visible on instances via the IsA chain. `Has[C](r, inst)` returns false and `Get[C](r, inst)` returns the zero value, even if the prefab owns C. Query auto-promotion is also suppressed: even if `SetInheritable[T]` was called for C, instances will not match a query for C through Up(IsA) traversal.

**API:**

```go
type Secret struct{ Code int }

w := flecs.New()
secretID := flecs.RegisterComponent[Secret](w)
flecs.SetInstantiatePolicy(w, secretID, w.DontInherit())

var prefab, inst flecs.ID
w.Write(func(fw *flecs.Writer) {
    prefab = fw.NewEntity()
    flecs.Set(fw, prefab, Secret{Code: 42})

    inst = fw.NewEntity()
    fw.AddID(inst, flecs.MakePair(w.IsA(), prefab))
})

w.Read(func(r *flecs.Reader) {
    // inst cannot see Secret from prefab
    has := flecs.Has[Secret](r, inst) // false
    _ = has
})
```

DontInherit takes precedence over Inheritable: calling both `SetInheritable[T](w)` and `SetInstantiatePolicy(w, cid, w.DontInherit())` on the same component means DontInherit wins.

---

## Sealing prefabs with Final

When a concrete prefab is not meant to be a base class for other prefabs, mark it with `flecs.SetFinal` to enforce the boundary at `AddID` time:

```go
w := flecs.New()
flecs.RegisterComponent[Health](w)

var concreteFrigate flecs.ID
w.Write(func(fw *flecs.Writer) {
    concreteFrigate = fw.NewEntity()
    flecs.Set(fw, concreteFrigate, Health{Max: 500})
})

// Seal concreteFrigate — no further IsA subtyping allowed.
flecs.SetFinal(w, concreteFrigate)

// Verify at query time.
w.Read(func(fr *flecs.Reader) {
    fmt.Println(flecs.IsFinal(fr, concreteFrigate)) // true
})

// Attempting to subtype will panic:
// w.Write(func(fw *flecs.Writer) {
//     fw.AddID(someOtherPrefab, flecs.MakePair(w.IsA(), concreteFrigate))
//     // panics: "cannot add (IsA, <id>): <id> has the Final trait"
// })
```

`IsFinal` accepts the `scope` interface, so it works in both `Read` and `Write` blocks without `AsReader()`. The bare-tag form `fw.AddID(entityID, w.Final())` is equivalent to `SetFinal(w, entityID)`.

See [ComponentTraits.md § Final](ComponentTraits.md#final) for the full API reference.

---

## Prefab tag

**Shipped in v0.57.0.** The built-in `Prefab` tag marks an entity as a template, excluding it from ordinary queries. This matches C `EcsPrefab` / `EcsTableIsPrefab` semantics.

```go
w.Write(func(fw *flecs.Writer) {
    spaceship = fw.NewEntity()
    flecs.Set[Speed](fw, spaceship, Speed{100})
    flecs.MarkPrefab(fw, spaceship)  // add Prefab tag; entity leaves ordinary iteration
})

// Ordinary queries skip the prefab:
q := flecs.NewQuery(w, speedID) // does NOT match spaceship

// Opt in to see prefabs:
q2 := flecs.NewQueryFromTerms(w, flecs.With(speedID), flecs.With(w.Prefab()))

// Instances inherit normally — instance is NOT tagged Prefab (DontInherit):
w.Write(func(fw *flecs.Writer) {
    instance = fw.NewEntity()
    flecs.AddID(fw, instance, flecs.MakePair(w.IsA(), spaceship))
})
flecs.IsPrefab(r, instance) // → false (Prefab tag is not inherited via IsA)
```

**`MarkPrefab` is one-way**: use `flecs.RemoveID(fw, e, w.Prefab())` to un-mark. The naming asymmetry with `DisableEntity`/`EnableEntity` is intentional: disabling is a togglable state, marking-as-prefab is a one-time labelling.

**DontInherit semantics**: the `Prefab` tag is bootstrapped with `DontInherit` (mirroring C `bootstrap.c:1308`). Entities that inherit from a prefab via `IsA` do _not_ acquire the `Prefab` tag, so instances remain visible to ordinary queries.

---

## Prefab hierarchies

When a prefab has children (entities with a `(ChildOf, prefab)` pair), instantiating the prefab via `AddID(e, MakePair(w.IsA(), prefab))` replicates the entire child subtree onto the instance. Each prefab child gets a fresh entity on the instance; the new entities have `(ChildOf, instance)` (or `(ChildOf, instanceParent)` for grandchildren) added automatically.

```go
type HP struct{ Value int }
w := flecs.New()
flecs.RegisterComponent[HP](w)

var tank, turret, tracks, inst flecs.ID
w.Write(func(fw *flecs.Writer) {
    // Build the prefab hierarchy.
    tank = fw.NewEntity()
    flecs.MarkPrefab(fw, tank)

    turret = fw.NewEntity()
    flecs.MarkPrefab(fw, turret)
    flecs.AddID(fw, turret, flecs.MakePair(w.ChildOf(), tank))
    flecs.Set(fw, turret, HP{Value: 10})

    tracks = fw.NewEntity()
    flecs.MarkPrefab(fw, tracks)
    flecs.AddID(fw, tracks, flecs.MakePair(w.ChildOf(), tank))
    flecs.Set(fw, tracks, HP{Value: 20})

    // Instantiate: spawns instTurret and instTracks as children of inst.
    inst = fw.NewEntity()
    flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), tank))
})

w.Read(func(r *flecs.Reader) {
    // inst now has two children — copies of turret and tracks.
    var children []flecs.ID
    w.EachChild(inst, func(c flecs.ID) bool {
        children = append(children, c)
        return true
    })
    // len(children) == 2
    // children[0] != turret && children[1] != tracks (fresh entities)
})
```

The replication is recursive: grandchildren, great-grandchildren, and so on are all copied. Each level respects the same rules — `DontInherit` components are skipped, `(ChildOf, *)` and `(IsA, *)` pairs are not blindly forwarded to the copy.

**Cross-reference rewriting.** If a prefab child carries a pair whose target is another entity in the same prefab subtree — e.g. child A has `(Targets, B)` and B is also a child of the prefab — the copy of A will have `(Targets, copyOfB)`, not `(Targets, B)`. References to entities outside the subtree (global entities, unrelated prefabs) are left unchanged.

**OrderedChildren propagation.** If the prefab has the `OrderedChildren` trait (see [HierarchiesManual.md § OrderedChildren](HierarchiesManual.md#orderedchildren)), the instance is marked ordered before children are added, preserving insertion order.

---

## Prefab slots

Slots provide O(1) named access to specific children of an instance without a name lookup. A slot is declared by adding `(SlotOf, prefab)` to a prefab child; the instantiation pipeline then adds `(prefabChild, instanceChild)` to the instance root.

```go
var tank, turret, inst flecs.ID
w.Write(func(fw *flecs.Writer) {
    tank = fw.NewEntity()
    flecs.MarkPrefab(fw, tank)

    turret = fw.NewEntity()
    flecs.MarkPrefab(fw, turret)
    flecs.AddID(fw, turret, flecs.MakePair(w.ChildOf(), tank))
    flecs.AddID(fw, turret, flecs.MakePair(w.SlotOf(), tank)) // declare slot

    inst = fw.NewEntity()
    flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), tank))
})

w.Read(func(r *flecs.Reader) {
    // Resolve the slot: inst has pair (turret, instTurret).
    instTurret, ok := flecs.GetPairTarget(r, inst, turret)
    // ok == true; instTurret is the copied child, not turret itself
    _ = instTurret
})
```

The slot relationship is exclusive: only one slot per prefab child is permitted on any instance (enforced by the `Exclusive` trait on `SlotOf`).

`w.SlotOf()` returns the built-in `SlotOf` relationship entity (index 47). It is bootstrapped with the `Exclusive`, `Relationship`, and `PairIsTag` traits, mirroring C `bootstrap.c:1274,1282,1324`.

> **Note:** The nested-slot variant — where `(SlotOf, grandparent)` names a prefab higher up the hierarchy — is not yet ported. Only `(SlotOf, directParent)` is handled.

---

## Not yet ported

The following C flecs prefab features have no equivalent in the current Go port:

- **Prefab-of-prefab instantiation** — a prefab `B` that inherits from another prefab `A` via `(IsA, A)` does not trigger recursive subtree copying from `A` when `B` is instantiated. Deferred to a future phase.
- **Nested slots** — `(SlotOf, grandparentPrefab)` where the slot targets a prefab higher than the immediate parent. Only `(SlotOf, directParent)` is currently resolved. Deferred to a future phase.

---

## Protecting prefabs from accidental deletion

Go flecs does **not** auto-install `(OnDeleteTarget, Panic)` on `IsA` — this matches C flecs, where `EcsIsA` has no default cleanup policy either. Deleting a prefab that instances depend on simply orphans those instances (the `(IsA, prefab)` pair remains but the prefab entity is dead).

If you want deleting a prefab to panic (e.g., to catch lifecycle bugs during development), install the policy explicitly:

```go
w := flecs.New()

// Create a prefab.
var vehiclePrefab flecs.ID
w.Write(func(fw *flecs.Writer) { vehiclePrefab = fw.NewEntity() })

// Guard: panics if any entity still inherits vehiclePrefab via IsA when it is deleted.
flecs.SetCleanupPolicy(w, w.IsA(), w.OnDeleteTarget(), w.PanicAction())
```

Alternatively, install it only on specific prefab entities rather than on `IsA` globally:

```go
// Panic when vehiclePrefab itself is used as a target and someone tries to delete it.
// Not standard flecs — use carefully; this is an opt-in safety net.
flecs.SetCleanupPolicy(w, vehiclePrefab, w.OnDelete(), w.PanicAction())
```

---

## See Also

- [Quickstart](Quickstart.md) — hands-on introduction to `IsA` prefab inheritance.
- [Relationships.md](Relationships.md) — `IsA` as a relationship; pair encoding; traversal.
- [HierarchiesManual.md](HierarchiesManual.md) — `ChildOf` hierarchies; prefabs and child trees compose.
- [ComponentTraits.md](ComponentTraits.md) — `SetInheritable[T]`; which components are inherited by instances.
- [Manual](Manual.md) — top-level reference hub with world lifecycle, concurrency model, and concept map.
