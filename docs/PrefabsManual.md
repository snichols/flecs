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

> **Go vs C flecs — no Prefab tag.** C flecs has a built-in `EcsPrefab` tag that excludes prefab entities from ordinary queries automatically. Go flecs does not have this tag yet; `spaceship` above participates in queries just like any other entity. See [Not yet ported](#not-yet-ported).

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

### Override (not yet ported)

> **Not yet ported in Go flecs.** C flecs's `(OnInstantiate, Override)` causes a component to be **automatically copied** from the prefab into each new instance at instantiation time. `w.Override()` returns the entity ID for this trait, but setting the pair has no runtime effect in Go flecs.
>
> **Workaround:** call `flecs.Set(fw, inst, value)` explicitly after adding the `(IsA, prefab)` pair.
>
> See the feature-gap list in [docs/README.md](README.md).

### DontInherit (not yet ported)

> **Not yet ported in Go flecs.** C flecs's `(OnInstantiate, DontInherit)` prevents a specific component from being inherited by instances. `w.DontInherit()` returns the entity ID, but setting the pair has no runtime effect.
>
> **Workaround:** in Go flecs inheritance is opt-in — components are not inheritable unless `SetInheritable[T]` was called for them. Simply do not call `SetInheritable[T]` for a component to achieve the same result.
>
> See the feature-gap list in [docs/README.md](README.md).

---

## Not yet ported

The following C flecs prefab features have no equivalent in the current Go port:

- **Prefab tag** — C flecs has a built-in `EcsPrefab` tag that prevents prefab entities from matching ordinary queries by default. Go flecs has no such tag; prefab entities are indistinguishable from regular entities at query time. not yet ported in Go flecs.
- **Auto-override on instantiation** — `(OnInstantiate, Override)` causes a component to be automatically deep-copied from the prefab to each new instance at `(IsA, prefab)` add time. not yet ported in Go flecs. Workaround: call `flecs.Set(fw, inst, value)` manually after instantiation.
- **DontInherit trait behavior** — `(OnInstantiate, DontInherit)` opts a component out of inheritance. not yet ported in Go flecs. Since Go flecs inheritance is opt-in (`SetInheritable[T]`), simply not calling `SetInheritable[T]` is the equivalent.
- **Prefab hierarchies** — in C flecs, instantiating a prefab that has `(ChildOf, prefab)` children copies the entire subtree to the instance. Go flecs does not replicate children on IsA instantiation. not yet ported in Go flecs.
- **Prefab slots** — `(SlotOf, prefab)` on a prefab child creates a named slot relationship on the instance that resolves to the copied child in O(1). not yet ported in Go flecs.
