# Queries

Queries find entities that match a list of conditions. They are the core iteration mechanism in Go flecs, powering systems, observers, and direct application logic alike.

See the [Quickstart](Quickstart.md) for a hands-on introduction. See [EntitiesComponents](EntitiesComponents.md) for entity and component setup. For actual benchmark numbers, see [BENCH.md](../BENCH.md). Queries power systems — see [Systems.md](Systems.md) for how `NewCachedQuery` connects to `NewSystem` and `Progress`.

---

## Table of Contents

- [Definitions](#definitions)
- [Performance and Caching](#performance-and-caching)
- [Creating Queries](#creating-queries)
- [Operators](#operators)
- [Iteration](#iteration)
- [Typed Iteration](#typed-iteration)
- [Pairs in Queries](#pairs-in-queries)
- [Relationship Traversal](#relationship-traversal)
- [Inheritable Components](#inheritable-components)
- [Change Detection](#change-detection)
- [Performance Notes](#performance-notes)
- [Not Yet Ported](#not-yet-ported)

---

## Definitions

| Term | Meaning |
|------|---------|
| Table | A group of entities sharing exactly the same set of component IDs (archetype) |
| Component | A single-ID slot that stores typed data for each entity in the table |
| Tag | A component whose data size is zero — present or absent, no value |
| Pair | A two-element ID built from a relationship and a target; stored like a component |
| Term | One condition in a query (e.g., "must have Position", "must not have Velocity") |
| Seed term | The TermAnd term with the fewest matching tables, used as iteration starting point |
| Iterator | A cursor over the sequence of matching tables (`*QueryIter`) |
| Field | The typed column slice for one term in the current table |

---

## Performance and Caching

### Archetype tables

Go flecs is an archetype ECS. Entities that share exactly the same component set live together in a table. When an entity's component set changes, the entity migrates to a different table. Iteration means walking a packed array — no pointer chasing.

A query matches tables, not individual entities. A query for `[Position, Velocity]` visits only the tables whose signatures include both component IDs. It never examines entities that lack either.

```go
type Position struct{ X, Y float32 }
type Velocity struct{ DX, DY float32 }
type Mass     struct{ Value float32 }

w := flecs.New()
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)

var e1, e2, e3 flecs.ID
w.Write(func(fw *flecs.Writer) {
    // e1 lands in table [Position]
    e1 = fw.NewEntity()
    flecs.Set(fw, e1, Position{X: 1})

    // e2, e3 land in table [Position, Velocity]
    e2 = fw.NewEntity()
    flecs.Set(fw, e2, Position{X: 2})
    flecs.Set(fw, e2, Velocity{DX: 1})

    e3 = fw.NewEntity()
    flecs.Set(fw, e3, Position{X: 3})
    flecs.Set(fw, e3, Velocity{DX: 2})
})

// Only e2 and e3 match — e1 is in a different table.
q := flecs.NewQuery(w, posID, velID)
```

### Uncached vs cached

**Uncached queries** (`NewQuery` / `NewQueryFromTerms`) derive their candidate table list on every `Iter()` call. Construction is O(1); each iteration scans from the seed term's component index. Use these for one-shot or ad-hoc queries.

**Cached queries** (`NewCachedQuery` / `NewCachedQueryFromTerms`) pre-filter all matching tables at construction time and keep that list current as new tables are created. `Iter()` is O(matching-tables) with no per-call allocation. Use these for queries that run every frame (systems) or for change detection.

When in doubt, start with uncached. Promote to cached only if profiling shows the per-frame candidate-list rebuild is measurable.

---

## Creating Queries

### Simple AND queries

`NewQuery` takes a world and one or more component IDs. Every ID is a required (AND) term:

```go
type Position struct{ X, Y float32 }
type Velocity struct{ DX, DY float32 }

w := flecs.New()
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)

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

`NewCachedQuery` has the same signature; it pre-filters tables at construction:

```go
cq := flecs.NewCachedQuery(w, posID, velID)
it := cq.Iter()
for it.Next() {
    pos := flecs.Field[Position](it, posID)
    vel := flecs.Field[Velocity](it, velID)
    for i := range it.Entities() {
        pos[i].X += vel[i].DX
        pos[i].Y += vel[i].DY
    }
}
```

### Structured terms

`NewQueryFromTerms` and `NewCachedQueryFromTerms` accept a slice of `flecs.Term`, enabling NOT, Optional, and OR operators alongside AND:

```go
type Position struct{ X, Y float32 }
type Velocity struct{ DX, DY float32 }
type Mass     struct{ Value float32 }

w := flecs.New()
posID  := flecs.RegisterComponent[Position](w)
velID  := flecs.RegisterComponent[Velocity](w)
massID := flecs.RegisterComponent[Mass](w)

// Entities with Position and without Mass, optionally with Velocity.
q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.Without(massID),
    flecs.Maybe(velID),
)
```

---

## Operators

### And (With)

`flecs.With(id)` builds a TermAnd term: matched tables must contain `id`. This is the default operator; `NewQuery` uses it for every ID.

```go
type Position struct{ X, Y float32 }
type Velocity struct{ DX, DY float32 }

w := flecs.New()
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)

// Equivalent to NewQuery(w, posID, velID).
q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.With(velID),
)
```

At least one AND term is required. A query with only NOT or Optional terms would match an unbounded entity set and is not supported.

### Not (Without)

`flecs.Without(id)` builds a TermNot term: matched tables must not contain `id`. The excluded component is never present in the column — do not call `Field` for a NOT term.

```go
type Position struct{ X, Y float32 }
type Velocity struct{ DX, DY float32 }

w := flecs.New()
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)

// Match entities with Position that do NOT have Velocity.
q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.Without(velID),
)
```

### Optional (Maybe)

`flecs.Maybe(id)` builds a TermOptional term: matched tables may or may not include `id`. Optional terms do not affect which tables are matched — they just make the component column available when present.

Use `flecs.FieldMaybe[T]` to access an Optional column. `flecs.Field[T]` panics when the column is absent.

```go
type Position struct{ X, Y float32 }
type Velocity struct{ DX, DY float32 }

w := flecs.New()
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)

q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.Maybe(velID),
)
it := q.Iter()
for it.Next() {
    pos        := flecs.Field[Position](it, posID)
    vel, hasVel := flecs.FieldMaybe[Velocity](it, velID)
    for i := range it.Entities() {
        if hasVel {
            pos[i].X += vel[i].DX
            pos[i].Y += vel[i].DY
        }
    }
}
```

### Or

Adjacent `flecs.Or(id)` terms form an OR-group. A table matches the group when it contains at least one of the group's component IDs. Non-Or terms break the group.

Use `flecs.FieldMaybe` to read Or-group columns — `flecs.Field` panics if the column is absent from the current table.

```go
type Position struct{ X, Y float32 }
type Speed    struct{ Value float32 }
type Velocity struct{ DX, DY float32 }

w := flecs.New()
posID   := flecs.RegisterComponent[Position](w)
speedID := flecs.RegisterComponent[Speed](w)
velID   := flecs.RegisterComponent[Velocity](w)

// Match entities with Position AND (Speed OR Velocity).
q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.Or(speedID),
    flecs.Or(velID),
)
it := q.Iter()
for it.Next() {
    speedCol, hasSpeed := flecs.FieldMaybe[Speed](it, speedID)
    velCol,   hasVel   := flecs.FieldMaybe[Velocity](it, velID)
    for i := range it.Entities() {
        if hasSpeed {
            _ = speedCol[i].Value
        } else if hasVel {
            _ = velCol[i].DX
        }
    }
}
```

---

## Iteration

### Pull-style iteration (Iter / Next)

`q.Iter()` returns a `*QueryIter`. Call `it.Next()` to advance to the next matching table. It returns `true` when positioned and `false` when exhausted.

```go
it := q.Iter()
for it.Next() {
    // it.Count()    — entities visible to this iterator in this table
    // it.Entities() — their IDs
    for _, e := range it.Entities() {
        _ = e
    }
}
```

`q.Each(fn)` is a convenience wrapper that calls `fn` once per matching table with the iterator already positioned:

```go
q.Each(func(it *flecs.QueryIter) {
    for _, e := range it.Entities() {
        _ = e
    }
})
```

### Field access

`flecs.Field[T](it, id)` returns a live `[]T` slice over the column for `id` in the current table. The slice is zero-allocation (an `unsafe.Slice` over the column's backing array). Do not retain it across `Next()` calls.

```go
type Position struct{ X, Y float32 }
type Velocity struct{ DX, DY float32 }

w := flecs.New()
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)

q  := flecs.NewQuery(w, posID, velID)
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

For TermOptional or TermOr columns, use `flecs.FieldMaybe[T]` instead (shown in the [Optional](#optional-maybe) section).

### IsFieldSelf and FieldShared

For traversal terms (Up / SelfUp / Cascade), a component may come from an ancestor rather than the entity's own table. `flecs.IsFieldSelf(it, id)` reports whether the component is owned locally (`true`) or inherited from an ancestor (`false`).

When the component is on an ancestor, use `flecs.FieldShared[T]` to read the single inherited value. When it is local, use `flecs.Field[T]`.

```go
type Mass struct{ Value float32 }

w := flecs.New()
massID := flecs.RegisterComponent[Mass](w)

// SelfUp: match entities that own Mass OR whose ancestor owns Mass via ChildOf.
q := flecs.NewQueryFromTerms(w,
    flecs.With(massID).SelfUp(w.ChildOf()),
)
it := q.Iter()
for it.Next() {
    if flecs.IsFieldSelf(it, massID) {
        col := flecs.Field[Mass](it, massID)
        for i := range it.Entities() {
            _ = col[i].Value
        }
    } else {
        inherited, ok := flecs.FieldShared[Mass](it, massID)
        if ok {
            for range it.Entities() {
                _ = inherited.Value // same value for every entity in this table
            }
        }
    }
}
```

---

## Typed Iteration

For the common 1–4 component pattern, `Each1`–`Each4` provide idiomatic Go closures over a `*Reader` scope. They handle component registration, query construction, and field dispatch internally.

```go
type Position struct{ X, Y float32 }
type Velocity struct{ DX, DY float32 }

w := flecs.New()
w.Write(func(fw *flecs.Writer) {
    e := fw.NewEntity()
    flecs.Set(fw, e, Position{X: 1, Y: 2})
    flecs.Set(fw, e, Velocity{DX: 0.5, DY: 0.5})
})

w.Read(func(fr *flecs.Reader) {
    flecs.Each2(fr, func(e flecs.ID, p *Position, v *Velocity) {
        p.X += v.DX
        p.Y += v.DY
        _ = e
    })
})
```

`Each1`–`Each4` always use uncached queries. For cached queries, change detection, or traversal terms, use `NewCachedQueryFromTerms` with `Iter` directly.

---

## Pairs in Queries

A pair ID is formed with `flecs.MakePair(rel, tgt)`. Use `flecs.With(flecs.MakePair(rel, tgt))` to require the pair as an AND term:

```go
w := flecs.New()

var rel, alice, bob flecs.ID
w.Write(func(fw *flecs.Writer) {
    rel   = fw.NewEntity()
    alice = fw.NewEntity()
    bob   = fw.NewEntity()
    fw.AddID(alice, flecs.MakePair(rel, bob)) // alice Likes bob
})

pairID := flecs.MakePair(rel, bob)
q  := flecs.NewQueryFromTerms(w, flecs.With(pairID))
it := q.Iter()
for it.Next() {
    for _, e := range it.Entities() {
        _ = e // alice
    }
}
```

---

## Relationship Traversal

Traversal modifiers let a query term follow a relationship chain to find a component on an ancestor rather than — or in addition to — the matched entity itself. Any relationship used for traversal must be registered as traversable; the built-in `ChildOf` and `IsA` relationships are always traversable.

### Up — ancestor-only

`.Up(rel)` matches entities whose nearest ancestor via `rel` owns the component. The entity's own table need not contain the component. Use `flecs.FieldShared[T]` to read the inherited value.

```go
type Mass struct{ Value float32 }

w := flecs.New()
massID := flecs.RegisterComponent[Mass](w)

var parent, child flecs.ID
w.Write(func(fw *flecs.Writer) {
    parent = fw.NewEntity()
    flecs.Set(fw, parent, Mass{Value: 100})

    child = fw.NewEntity()
    fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))
})

// Match entities whose parent owns Mass via ChildOf.
q  := flecs.NewQueryFromTerms(w, flecs.With(massID).Up(w.ChildOf()))
it := q.Iter()
for it.Next() {
    inherited, ok := flecs.FieldShared[Mass](it, massID)
    if ok {
        for _, e := range it.Entities() {
            _ = e
            _ = inherited.Value // 100, from parent
        }
    }
}
```

### SelfUp — self-or-ancestor

`.SelfUp(rel)` checks the entity's own table first; if the component is absent, it walks `rel` upwards. Use `flecs.IsFieldSelf(it, id)` to distinguish the two cases per matched table:

```go
type Mass struct{ Value float32 }

w := flecs.New()
massID := flecs.RegisterComponent[Mass](w)

// SelfUp: match entities that own Mass OR inherit it from a ChildOf ancestor.
q  := flecs.NewQueryFromTerms(w, flecs.With(massID).SelfUp(w.ChildOf()))
it := q.Iter()
for it.Next() {
    if flecs.IsFieldSelf(it, massID) {
        col := flecs.Field[Mass](it, massID)
        for i := range it.Entities() {
            _ = col[i].Value
        }
    } else {
        inherited, ok := flecs.FieldShared[Mass](it, massID)
        if ok {
            for range it.Entities() {
                _ = inherited.Value
            }
        }
    }
}
```

### Cascade — root-first order

`.Cascade(rel)` behaves like `SelfUp` but guarantees that matched tables are visited in ascending depth order (root first, then children, then grandchildren). This is useful for top-down hierarchy passes such as transform propagation.

`Cascade` requires a cached query — using it with `NewQueryFromTerms` panics.

```go
type Position struct{ X, Y float32 }

w := flecs.New()
posID := flecs.RegisterComponent[Position](w)

var root, child flecs.ID
w.Write(func(fw *flecs.Writer) {
    root = fw.NewEntity()
    flecs.Set(fw, root, Position{X: 0, Y: 0})

    child = fw.NewEntity()
    fw.AddID(child, flecs.MakePair(w.ChildOf(), root))
    flecs.Set(fw, child, Position{X: 1, Y: 1})
})

// Iterate Position in root-first (depth-ascending) order.
cq := flecs.NewCachedQueryFromTerms(w,
    flecs.With(posID).Cascade(w.ChildOf()),
)
var order []flecs.ID
cq.Each(func(it *flecs.QueryIter) {
    for _, e := range it.Entities() {
        order = append(order, e)
    }
})
// order[0] == root (depth 0), order[1] == child (depth 1)
_ = order
```

### Custom traversal relationships

Custom traversal relationships must be registered as Traversable with `SetTraversable` before use. The built-in `ChildOf` and `IsA` are always Traversable:

```go
type Mass struct{ Value float32 }

w := flecs.New()

// Custom traversal relationships must be marked Traversable first.
var containedBy flecs.ID
w.Write(func(fw *flecs.Writer) {
    containedBy = fw.NewEntity()
})
flecs.SetTraversable(w, containedBy) // required since v0.46.0

massID := flecs.RegisterComponent[Mass](w)

q := flecs.NewQueryFromTerms(w,
    flecs.With(massID).Up(containedBy),
)
_ = q
```

---

## Inheritable Components

`flecs.SetInheritable[T](w)` marks a component type as inheritable. Any query term for that component is then automatically promoted to `Self|Up(IsA)` at construction: the query matches both entities that own the component locally and entities that inherit it from a prefab via `IsA`.

Call `SetInheritable` after `RegisterComponent` and before building any query that references the component.

```go
type Mass struct{ Value float32 }

w := flecs.New()
flecs.RegisterComponent[Mass](w)
flecs.SetInheritable[Mass](w) // promote all terms for Mass to Self|Up(IsA)

massID := flecs.RegisterComponent[Mass](w) // idempotent; returns same ID

var prefab, inst flecs.ID
w.Write(func(fw *flecs.Writer) {
    prefab = fw.NewEntity()
    flecs.Set(fw, prefab, Mass{Value: 50})

    inst = fw.NewEntity()
    fw.AddID(inst, flecs.MakePair(w.IsA(), prefab)) // inst inherits Mass
})

// This query also matches inst even though inst doesn't own Mass directly.
q  := flecs.NewQuery(w, massID)
it := q.Iter()
var matched []flecs.ID
for it.Next() {
    for _, e := range it.Entities() {
        matched = append(matched, e)
    }
}
// matched includes prefab AND inst.
_ = matched
```

To suppress auto-promotion on a specific term, call `.Self()` explicitly:

```go
// Only match entities that locally own Mass; do not walk IsA.
q := flecs.NewQueryFromTerms(w,
    flecs.With(massID).Self(),
)
```

---

## Transitive Pair Matching

When a relationship is marked **Transitive** (via `flecs.SetTransitive(w, relID)`), a pair term `With(MakePair(R, C))` also matches entities that hold `(R, B)` where `B` (or any entity reachable from `B` via further `R` pairs) holds `(R, C)`. This is the query-engine generalisation of the `IsA` chain-walking already done by `Get`/`Has`.

```go
// LocatedIn: Manhattan LocatedIn NewYork, NewYork LocatedIn USA.
// Query for (LocatedIn, USA) matches both.
flecs.SetTransitive(w, locatedIn)
q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(locatedIn, usa)))
```

The walk is lazy (query time only), cycle-safe, and bounded at 64 hops. For full documentation and a worked example see [ComponentTraits.md § Transitive](ComponentTraits.md#transitive) and [Relationships.md § Transitive](Relationships.md#transitive).

---

## Change Detection

`(*CachedQuery).Changed()` reports whether any matching table was mutated since the last call. It returns `true` on the first call (initial state is "all changed"), and thereafter returns `true` only when:

- A component value in a matching table was written (via `Set` or `SetByID`).
- An entity was added to or removed from a matching table.
- A new matching table was added to the cache.

```go
type Position struct{ X, Y float32 }

w := flecs.New()
posID := flecs.RegisterComponent[Position](w)

var e flecs.ID
w.Write(func(fw *flecs.Writer) {
    e = fw.NewEntity()
    flecs.Set(fw, e, Position{X: 1})
})

cq := flecs.NewCachedQuery(w, posID)

// First call always returns true.
changed := cq.Changed() // true

// No mutations since last call — Changed() returns false.
changed = cq.Changed() // false

// Write a new value.
w.Write(func(fw *flecs.Writer) {
    flecs.Set(fw, e, Position{X: 2})
})

changed = cq.Changed() // true
_ = changed
```

Change detection uses per-table monotonic counters. Any write to a column marks it dirty for all cached queries covering it. `Changed()` may over-report (false positives are safe) but never under-reports.

### Closing a cached query

Release a cached query with `cq.Close()` when it is no longer needed. After `Close()`, `Iter().Next()` returns `false` and `Changed()` returns `false`.

```go
type Position struct{ X, Y float32 }

w  := flecs.New()
posID := flecs.RegisterComponent[Position](w)
cq := flecs.NewCachedQuery(w, posID)

cq.Close()

if cq.IsClosed() {
    // Query is closed; Iter().Next() returns false immediately.
}
```

---

## Performance Notes

- **Uncached query iteration** is O(smallest-matching-set × terms) per `Iter()` call. The seed term is the TermAnd component with the fewest matching tables. For sparse queries this is very fast; for dense queries the inner-loop cost dominates.

- **Cached query iteration** is O(matching-tables) per `Iter()` after construction. No allocation on `Iter()` — the candidate list is pre-built. Construction is O(all-tables × terms) once; new-table notifications are O(terms) each.

- **`Field[T]`** is zero-allocation. It wraps an `unsafe.Slice` over the column's backing array — no boxing, no interface dispatch.

- **Up traversal** walks the ancestor chain at most once per candidate table per `Iter()` call (uncached) or once at construction (cached). Deep hierarchies with many cached traversal queries can incur rematching cost; see [BENCH.md](../BENCH.md) for numbers.

- **`Each1` / `Each2`** always use uncached queries. For systems that run every frame, prefer `NewCachedQuery` + `Iter` to amortize the candidate-list rebuild.

- For v0.16 per-stage queue and `BenchmarkMultiThreadedDeferredSet` numbers, see [BENCH.md](../BENCH.md).

---

## Not Yet Ported

The following features from the upstream C flecs `Queries.md` are not yet available in the Go port. See `docs/README.md` for the full feature-gap list.

### Wildcard and Any query terms (Phase 15.6, v0.38.0)

Use `w.Wildcard()` or `w.Any()` in the target or relationship slot of a pair term to match across an entire relationship family.

**Wildcard** emits one iterator row per concrete target that exists in the matched table:

```go
// One row per (Likes, X) pair — entity "a" with (Likes, Bob) and (Likes, Alice) yields two rows.
q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(likesID, w.Wildcard())))
it := q.Iter()
for it.Next() {
    target := flecs.MatchedTarget(it, 0)  // Bob on the first row, Alice on the second
    mid    := flecs.MatchedID(it, 0)      // full pair ID: (Likes, Bob) then (Likes, Alice)
    for _, e := range it.Entities() {
        _ = e
        _ = target
    }
}
```

**Any** matches once per entity regardless of how many concrete targets exist (short-circuit semantics):

```go
// Exactly one row per entity that has any (Likes, X) pair.
q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(likesID, w.Any())))
```

**Wildcard in relationship position** matches every relationship to a fixed target:

```go
// One row per (X, Bob) pair in each table.
q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(w.Wildcard(), bobID)))
```

**Accessors for wildcard rows:**

| Function | Returns |
|---|---|
| `flecs.MatchedTarget(it, termIdx)` | Concrete target entity for the current wildcard row |
| `flecs.MatchedID(it, termIdx)` | Full pair ID `(rel, target)` that matched |
| `flecs.FieldByMatch[T](it, termIdx)` | Typed `[]T` column for the matched pair (when it carries data) |

`termIdx` is the 0-based index of the wildcard term in the sorted term list (same order as `it.TermsFull()`).

**Wildcard and Any in CachedQuery**: both work in `NewCachedQueryFromTerms`. The cache updates automatically when new tables with matching concrete pairs are created.

**Fixed per-term source** — In C flecs a term can match a component on a *specific* named entity rather than the iterated entity (e.g., match `SimTime` on a global `Game` entity). Go flecs only supports the default `$this` source. Not yet ported in Go flecs.

**Query variables** — `$Var` named variables in the Flecs Query Language constrain results across related entities (e.g., "spaceships docked to a planet"). Not yet ported in Go flecs.

**Sorted queries** — `order_by_callback` sorts matched entities by a component value. Not yet ported in Go flecs.

**Query groups** — `group_by_callback` partitions the query cache by a computed group ID, enabling O(1) group-iterator lookups. Not yet ported in Go flecs. (`Cascade` provides hierarchy-depth ordering as a special built-in case.)

**Equality operators** — `$this == Foo`, `$this ~= "partial"` name-match filters in the Flecs Query Language. Not yet ported in Go flecs.

**AndFrom / OrFrom / NotFrom** — operators that expand a component-list entity into implicit terms. Not yet ported in Go flecs.

**Query scopes** — `scope_open` / `scope_close` to negate a sub-expression. Not yet ported in Go flecs.

**Access modifiers** — `In` / `InOut` / `Out` / `None` annotations on terms (used by the C scheduler for pipeline sync-point inference). Go flecs governs mutation via `Read`/`Write` scopes at the world level; per-term access annotations are not ported.

**Member value queries** — match on the runtime value of a component field (requires reflection/meta addon). Not yet ported in Go flecs.

---

## Disabled rows (CanToggle)

When a component is marked with the `CanToggle` trait (`flecs.SetCanToggle(w, compID)`), individual entities can have that component temporarily disabled. `Each1`, `Each2`, `Each3`, and `Each4` **automatically skip rows where the component is disabled**; no extra filtering is needed in the callback.

```go
flecs.SetCanToggle(w, posID)

// Disable Position for one entity — it won't appear in the loop below.
w.Write(func(fw *flecs.Writer) { flecs.DisableID(fw, e, posID) })

flecs.Each1[Position](r, func(e flecs.ID, p *Position) {
    // called only for entities where Position is enabled
})
```

See the [ComponentTraits manual](ComponentTraits.md#cantoggle) for full details.

---

## See Also

- [Quickstart](Quickstart.md) — hands-on introduction to entities, components, queries, and systems.
- [EntitiesComponents.md](EntitiesComponents.md) — entity and component API in full detail.
- [Systems.md](Systems.md) — systems use cached queries; parallel and multi-threaded dispatch.
- [Relationships.md](Relationships.md) — pair traversal terms and relationship queries.
- [ComponentTraits](ComponentTraits.md) — CanToggle, Exclusive, and other trait customisation.
- [Manual](Manual.md) — top-level reference hub with world lifecycle, concurrency model, and concept map.
