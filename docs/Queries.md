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
- [Fixed per-term source](#fixed-per-term-source)
- [Query variables (single-variable v1)](#query-variables-single-variable-v1)
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

### Query scopes

`flecs.WithoutScope(buildFn func(*ScopeBuilder)) Term` negates an entire sub-expression of arbitrary terms as a single unit. Use it when a plain `Without` is not expressive enough — for example when the excluded condition is a disjunction (OR) or a conjunction of multiple components.

```go
type Position struct{ X, Y float32 }
type Velocity struct{ DX, DY float32 }
type Speed    struct{ Value float32 }

w := flecs.New()
posID   := flecs.RegisterComponent[Position](w)
velID   := flecs.RegisterComponent[Velocity](w)
speedID := flecs.RegisterComponent[Speed](w)

// Match entities with Position AND NOT (Velocity OR Speed).
q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
        b.With(velID).Or(speedID)
    }),
)
```

The closure receives a `*ScopeBuilder` whose methods mirror the top-level term constructors:

| ScopeBuilder method | Meaning inside scope |
|---|---|
| `b.With(id)` | Require component `id` |
| `b.Without(id)` | Require absence of `id` |
| `b.Or(id)` | OR with the preceding term |
| `b.Maybe(id)` | Optional component `id` |
| `b.Source(src)` | Fix the preceding term's source to `src` |
| `b.WithoutScope(fn)` | Nested negated sub-scope |

**De-Morgan note** — for the simple OR-of-presence case, the scope is logically equivalent to individual `Without` terms:

```
Position AND NOT (Velocity OR Speed)
    ≡ Position AND NOT Velocity AND NOT Speed   (de Morgan's law)
```

The scope form is required when the inner expression mixes AND and OR, contains nested scopes, uses fixed-source terms, or includes sparse / DontFragment components.

**Nested scopes** are supported to arbitrary depth:

```go
// Position AND NOT (Velocity AND NOT Frozen)
q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
        b.With(velID).WithoutScope(func(b2 *flecs.ScopeBuilder) {
            b2.With(frozenID)
        })
    }),
)
```

**Empty scope panics** — `WithoutScope` panics at construction when the builder function adds no terms, mirroring upstream's compile-time rejection of `!{}`.

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

### AndFrom / OrFrom / NotFrom

These operators read a *source entity's* component list **once at query construction** (snapshot) and expand it into a set of inner terms. Changes to the source entity after construction are ignored — reconstruct the query to pick them up. This deliberately diverges from upstream C flecs, which re-reads the type at every iteration (`eval.c:462`).

Components marked `DontInherit` (e.g. the built-in `Prefab` and `Disabled` tags) are excluded from the expansion, matching upstream `EcsIdOnInstantiateDontInherit` filtering.

| Operator | Expands to | Semantics |
|---|---|---|
| `AndFrom(src)` | N `With` terms | entity must have **all** of src's components |
| `OrFrom(src)` | one OR-group of N | entity must have **at least one** of src's components |
| `NotFrom(src)` | N `Without` terms | entity must have **none** of src's components |

**Canonical use case — prefab type-lists:**

```go
type Health struct{ HP  int }
type Speed  struct{ Val float32 }
type AI     struct{ Behavior int }

w := flecs.New()
healthID := flecs.RegisterComponent[Health](w)
speedID  := flecs.RegisterComponent[Speed](w)
aiID     := flecs.RegisterComponent[AI](w)

// Build a prefab template.
var enemyTemplate flecs.ID
w.Write(func(fw *flecs.Writer) {
    enemyTemplate = fw.NewEntity()
    flecs.MarkPrefab(fw, enemyTemplate)
    flecs.Set(fw, enemyTemplate, Health{})
    flecs.Set(fw, enemyTemplate, Speed{})
    flecs.Set(fw, enemyTemplate, AI{})
})

// Match everything that "looks like an enemy" (has Health+Speed+AI).
qAnd := flecs.NewQueryFromTerms(w, flecs.AndFrom(enemyTemplate))

// Match everything with at least one enemy component.
qOr := flecs.NewQueryFromTerms(w, flecs.OrFrom(enemyTemplate))

// Match everything with none of the enemy components.
qNot := flecs.NewQueryFromTerms(w, flecs.NotFrom(enemyTemplate))
_ = qAnd
_ = qOr
_ = qNot
```

**Composition with regular terms:**

```go
// Position AND all enemy components.
q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.AndFrom(enemyTemplate),
)
_ = q
```

**Snapshot semantics note:**

Unlike upstream C flecs — which re-reads `source.type` on every iteration — Go flecs snapshots the source's component list at construction. Re-construct the query after mutating the source to pick up the new expansion.

**Empty source type:**

| Operator | Behaviour |
|---|---|
| `AndFrom(empty)` | vacuous truth — no requirements added |
| `OrFrom(empty)` | zero results (empty disjunction = false) |
| `NotFrom(empty)` | vacuous truth — no exclusions added |

This diverges from upstream C, which skips the empty-type operator entirely (`compiler_term.c:1231: ctx->skipped++`). Go flecs follows set-theoretic semantics where an empty `OrFrom` is an empty disjunction and therefore false.

**Pair IDs in the source's type are included** in expansion. A source entity that is a child of a parent (`(ChildOf, parent)`) will include that pair in the expansion, requiring matched entities to also be children of that parent.

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

## Sparse-aware queries

When a component has the `Sparse` trait (`flecs.SetSparse(w, compID)`), its data lives in a per-component sparse-set rather than archetype columns. As of v0.52.0, query terms naming Sparse components are fully integrated with `NewQuery`, `NewQueryFromTerms`, `NewCachedQuery`, and `NewCachedQueryFromTerms`.

### All-sparse queries

If every required `With` term is sparse, the query uses the smallest sparse-set as the iteration driver and cross-checks each candidate against remaining sparse terms. Yields one entity at a time (per `Next()` call, `Count()` returns 1 and `Entities()` returns a single-element slice).

```go
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)
flecs.SetSparse(w, posID)
flecs.SetSparse(w, velID)

q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.With(velID))
it := q.Iter()
for it.Next() {
    e := it.Entities()[0]
    pos := flecs.Field[Position](it, 0) // 1-element slice into sparse-set
    vel := flecs.Field[Velocity](it, 1)
    pos[0].X += vel[0].DX
}
```

### Mixed queries (sparse + archetype)

When a query has both sparse and archetype terms, the iterator first narrows to matching archetype tables (via the archetype terms), then filters each entity in those tables against the sparse terms. Every `Next()` call still yields one entity.

```go
// Tag is an archetype component; Position is sparse.
q := flecs.NewQueryFromTerms(w, flecs.With(tagID), flecs.With(posID))
it := q.Iter()
for it.Next() {
    pos := flecs.Field[Position](it, 1) // 1-element slice
    _ = pos[0].X
}
```

### Not and Optional on sparse terms

`TermNot` on a sparse term filters to entities that **do not** hold that sparse component. `TermMaybe` populates the optional slot but does not filter.

```go
// Entities with Position but NOT Velocity (both sparse):
q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.Without(velID),
)

// Position required, Velocity optional (both sparse):
q2 := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.Maybe(velID),
)
it2 := q2.Iter()
for it2.Next() {
    e := it2.Entities()[0]
    vel, ok := flecs.FieldMaybe[Velocity](it2, 1)
    if ok {
        _ = vel[0].DX // entity has Velocity
    }
    _ = e
}
```

### Cached queries with sparse terms

`NewCachedQuery` and `NewCachedQueryFromTerms` work with sparse terms. For purely-sparse cached queries (`sparseAndOnly`), `Iter()` builds the driver fresh from the sparse-sets each call — no stale archetype-table cache is needed. Mixed cached queries cache the archetype table list normally and check sparse terms per-entity.

`Changed()` returns `true` when any matching sparse-set has been structurally modified (entry inserted or removed) since the last call:

```go
cq := flecs.NewCachedQueryFromTerms(w, flecs.With(posID))
// First call always true (no previous baseline).
if cq.Changed() { /* rebuild derived state */ }
// After a Set/Remove on a Sparse component:
w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, Position{X: 9}) })
if cq.Changed() { /* true — sparse-set was modified */ }
```

### Limitations

- Pair IDs are always archetype-stored in v0.52.0; pair terms are never considered sparse even if the relationship entity also holds the Sparse trait on itself as a scalar component.
- Wildcard/Any query terms cannot be mixed with sparse terms in the same query.
- `Field[T]` and `FieldMaybe[T]` for sparse terms return a 1-element slice (not a table-column-length slice). Do not range over the result expecting multiple elements per `Next()` call.

---

## Sorted queries

Sorted cached queries yield their matched entities in a user-defined order, re-sorted lazily whenever the underlying data changes. Sorting is a `CachedQuery`-only feature; non-cached queries do not support it (sorting a non-cached query would re-sort on every iteration call).

### Creating a sorted cached query

Use `NewCachedQueryFromTermsWithOptions` with the `WithOrderBy` option:

```go
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)

cq := flecs.NewCachedQueryFromTermsWithOptions(w,
    flecs.WithOrderBy(posID, flecs.OrderBy[Position](func(eA flecs.ID, vA *Position, eB flecs.ID, vB *Position) int {
        if vA.X < vB.X { return -1 }
        if vA.X > vB.X { return 1 }
        return 0
    })),
    flecs.With(posID),
    flecs.With(velID),
)
defer cq.Close()

it := cq.Iter()
for it.Next() {
    pos := flecs.Field[Position](it, posID)
    fmt.Printf("entity %v at X=%.1f\n", it.Entities()[0], pos[0].X)
}
```

`OrderBy[T]` wraps a typed comparator `func(eA ID, vA *T, eB ID, vB *T) int`. The return convention is the same as `cmp.Compare`: negative if A < B, zero if equal, positive if A > B.

### Raw OrderByFunc

For lower-level use (e.g. a component registered by ID without the Go type), use `OrderByFunc` directly:

```go
import "unsafe"

var cmpByX flecs.OrderByFunc = func(eA flecs.ID, vA unsafe.Pointer, eB flecs.ID, vB unsafe.Pointer) int {
    pa, pb := (*Position)(vA), (*Position)(vB)
    if pa.X < pb.X { return -1 }
    if pa.X > pb.X { return 1 }
    return 0
}

cq := flecs.NewCachedQueryFromTermsWithOptions(w,
    flecs.WithOrderBy(posID, cmpByX),
    flecs.With(posID),
)
```

### Lazy invalidation

The sorted order is cached: the list is rebuilt only when the data has changed, not on every `Iter()` call. A rebuild is triggered when:

- Any matching table's `ChangeCount` has increased since the last sort (a component column was written, or an entity was structurally changed within that table).
- A new table matching the query was added since the last sort (`tablesAdded` flag).

If neither condition holds, the cached sorted list is returned directly at zero cost.

### Field access in sorted iteration

Each `Next()` call yields exactly one entity. `Entities()` returns a length-1 slice. `Field[T]` and `FieldMaybe[T]` read the component value for that entity as usual. This differs from the default table-walk iteration (where `Entities()` can yield a whole table row slice), but keeps the API uniform: `Each1`/`Each2`/etc. work unchanged.

### Optional sort-by component

The sort-by component may be a `Maybe` term. Entities that do not carry the optional component receive a `nil` pointer in the comparator's `vA`/`vB` arguments; handle the nil case in your comparator:

```go
cq := flecs.NewCachedQueryFromTermsWithOptions(w,
    flecs.WithOrderBy(tagID, flecs.OrderBy[Tag](func(eA flecs.ID, vA *Tag, eB flecs.ID, vB *Tag) int {
        if vA == nil { return -1 } // entities without Tag sort first
        if vB == nil { return 1 }
        return cmp.Compare(vA.Priority, vB.Priority)
    })),
    flecs.With(baseID),
    flecs.Maybe(tagID),
)
```

### Constraints

- The sort-by component (`WithOrderBy(componentID, cmp)`) must appear as a `With` or `Maybe` term in the query term set. Using a component not in the term set or using `Without` panics at construction.
- Pair IDs as the sort-by component are not supported in v0.59.0. Use a packed struct to sort by multiple fields at once.
- Unlike upstream C flecs' two-step per-table quicksort + k-way merge, Go flecs uses a single `sort.SliceStable` over all matched entities. Observable ordering is identical; the design decision is recorded in CHANGELOG v0.59.0.

---

## Query groups

Query groups partition a cached query's matched tables into labelled buckets. A caller-supplied `GroupByFunc` assigns each table to a `uint64` group ID; default `Iter()` visits tables in ascending group-ID order; `IterGroup` jumps directly to a single group in O(1) time.

### Creating a grouped cached query

Use `NewCachedQueryFromTermsWithOptions` with `WithGroupBy`:

```go
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)

// Group tables by how many components they carry.
cq := flecs.NewCachedQueryFromTermsWithOptions(w,
    flecs.WithGroupBy(posID, func(t *table.Table) uint64 {
        return uint64(len(t.Type()))
    }),
    flecs.With(posID),
)
defer cq.Close()

// Iterate all groups in ascending group-ID order.
it := cq.Iter()
for it.Next() { /* tables visited in group order */ }

// Jump directly to group 2 (O(1) startup).
it = cq.IterGroup(2)
for it.Next() { /* only tables in group 2 */ }

// List all populated group IDs.
gids := cq.Groups() // returns []uint64, sorted ascending
```

`WithGroupBy(componentID, groupFn)` takes:

- `componentID` — the component that acts as the invalidation hint. Must appear as a `With` or `Maybe` term. Pass `0` to trigger re-grouping on any table change. Panics at construction if non-zero and not in the term set.
- `groupFn` — the partitioning callback. Called once per matched table; its return value is the group ID.

### Combining WithGroupBy and WithOrderBy

Both options can be active on the same query. Use `AndOrderBy` (or `AndGroupBy`) for chaining:

```go
cq := flecs.NewCachedQueryFromTermsWithOptions(w,
    flecs.WithGroupBy(posID, groupFn).AndOrderBy(posID, flecs.OrderBy[Position](cmpByX)),
    flecs.With(posID),
)
```

Iteration order: groups in ascending ID order; within each group, entities in sort-comparator order.

### Lazy invalidation

Groups are rebuilt lazily whenever a table's `ChangeCount` changes (any column write or structural change) or a new matching table is added. The rebuild re-runs `groupFn` for all matched tables and re-sorts the group list. Full re-group on any change — no incremental update.

### API summary

| Method | Description |
|---|---|
| `cq.Iter()` | Walk all groups in ascending ID order |
| `cq.IterGroup(id)` | Walk only tables in group `id`; O(1) startup |
| `cq.Groups()` | Return sorted slice of populated group IDs |

### Design notes (divergences from upstream C)

- **No `on_group_create` / `on_group_delete` events** — group-lifecycle callbacks (`include/flecs.h:627-638`) are not ported in v0.66.0.
- **No multi-key grouping** — single callback and single component only.
- **No persistent group state** — groups are runtime-only; not marshalled.
- **`Cascade` is not rewritten** — the existing `cascadeTermTrav` plumbing is kept as-is. `Cascade` is implementable on top of `WithGroupBy` (mirroring `flecs_query_cache_group_by_cascade` in `src/query/cache/cache.c:175-189`); refactor is deferred to a future phase.

---

## Performance Notes

- **Uncached query iteration** is O(smallest-matching-set × terms) per `Iter()` call. The seed term is the TermAnd component with the fewest matching tables. For all-sparse queries the driver is the smallest sparse-set, so iteration is proportional to that set size. For dense queries the inner-loop cost dominates.

- **Cached query iteration** is O(matching-tables) per `Iter()` after construction. No allocation on `Iter()` — the candidate list is pre-built. Construction is O(all-tables × terms) once; new-table notifications are O(terms) each.

- **`Field[T]`** is zero-allocation. It wraps an `unsafe.Slice` over the column's backing array — no boxing, no interface dispatch.

- **Up traversal** walks the ancestor chain at most once per candidate table per `Iter()` call (uncached) or once at construction (cached). Deep hierarchies with many cached traversal queries can incur rematching cost; see [BENCH.md](../BENCH.md) for numbers.

- **`Each1` / `Each2`** always use uncached queries. For systems that run every frame, prefer `NewCachedQuery` + `Iter` to amortize the candidate-list rebuild.

- For v0.16 per-stage queue and `BenchmarkMultiThreadedDeferredSet` numbers, see [BENCH.md](../BENCH.md).

---

## Equality and name-match filters

**Shipped in v0.76.0.**

Three predicate filter terms let you constrain iteration to a specific entity or to entities whose name contains a substring. These terms are **per-entity filters**, not archetype seeds — they must appear alongside at least one `With` term.

| Builder | Mirrors upstream | Semantics |
|---|---|---|
| `flecs.IsEntity(e ID) Term` | `EcsPredEq` | Match only when iterated entity == `e` |
| `flecs.NotEntity(e ID) Term` | `EcsPredEq + EcsNot` | Match every entity except `e` |
| `flecs.NameMatches(pattern string) Term` | `EcsPredMatch` | Match entities whose `Name.Value` contains `pattern` (case-insensitive substring) |

### IsEntity

```go
// Yield only the player entity, even though many entities have Position.
q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.IsEntity(playerEntity),
)
```

`IsEntity` panics at construction if `e` is zero.

### NotEntity

```go
// Yield every Position-holder except the player.
q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.NotEntity(playerEntity),
)
```

`NotEntity` panics at construction if `e` is zero.

### NameMatches

```go
// Yield entities whose name contains "Enemy" (case-insensitive).
q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.NameMatches("Enemy"),
)
```

Substring semantics (mirrors upstream `flecs_query_match_substr_i`, `eval_pred.c:8-41`):

- **Not regex, not glob** — plain substring containment only.
- **Case-insensitive** — `"ENEMY"` matches `"Enemy1"`.
- **Empty pattern** — matches every named entity (`""` is a valid pattern).
- **Unnamed entities** — never match any pattern, including `""`.

### Composing with other terms

All three predicates compose orthogonally with `With`, `Without`, `Maybe`, `Or`, and `WithoutScope`:

```go
// Position-holders named "Player" that are not the dead player.
q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.NameMatches("Player"),
    flecs.NotEntity(deadPlayerEntity),
)
```

They also work inside `WithoutScope` sub-expressions (via `ScopeBuilder.IsEntity`, `ScopeBuilder.NotEntity`, `ScopeBuilder.NameMatches`).

### Restrictions

- `TermEq`, `TermNotEq`, and `TermNameMatch` **cannot** carry `.Up()`, `.SelfUp()`, `.Cascade()`, or `.Source()` — panic at validator time.
- Equality terms are **filters, not seeds** — a query with only equality terms (no `With`) panics with the existing "at least one TermAnd required" message.

### Upstream C references

- `include/flecs.h:1979-1986` — `EcsPredEq` / `EcsPredMatch` marker constants.
- `src/query/engine/eval_pred.c:8-41` — `flecs_query_match_substr_i`: case-insensitive substring scan.
- `src/query/engine/eval_pred.c:103-209` — `flecs_query_pred_eq` / `flecs_query_pred_neq`: range-based entity equality.
- `src/query/compiler/compiler_term.c:640-685` — compile-time lowering to `EcsQueryPredEq` / `EcsQueryPredNeq` / `EcsQueryPredEqMatch`.

---

## Fixed per-term source

**Shipped in v0.73.0.**

A query term can read its component from a *specific named entity* instead of the iterated entity (`$this`). The most common use case is the **singleton-on-query** pattern: a global game state struct lives on one fixed entity, and you want every matched entity to see it without passing it in manually.

```go
// SimTime lives on the global `game` entity.
// Movement systems read SimTime once per tick alongside per-entity data.
q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.With(velID),
    flecs.WithSourceTerm(simTimeID, game), // bound to `game`, not $this
)
it := q.Iter()
for it.Next() {
    pos := flecs.Field[Position](it, posID)     // per-entity
    vel := flecs.Field[Velocity](it, velID)     // per-entity
    st  := flecs.Field[SimTime](it, simTimeID)  // 1-element; same for all rows
    dt := st[0].DT
    // ... advance each entity
}
```

### Constructing fixed-source terms

Two equivalent forms:

```go
// Top-level builder (preferred):
flecs.WithSourceTerm(simTimeID, game)

// Chained builder (useful when the base term kind is already constructed):
flecs.With(simTimeID).Source(game)
flecs.Maybe(simTimeID).Source(game) // for TermOptional (see below)
```

Panics at construction if `componentID` or `sourceEntity` is zero, or if `.Source(e)` is combined with `.Up()` / `.SelfUp()` / `.Cascade()`.

### No archetype-filter contribution

A fixed-source term does **not** add to the `$this` archetype-filter set. The `simTimeID` term in the example above does not constrain which entities are matched — only `posID` and `velID` do. This makes the singleton-on-query pattern essentially free: the extra term costs one pointer lookup at iter start, not per-entity.

This mirrors C upstream's `flecs_query_insert_fixed_src_terms` / `EcsQuerySetFixed` plan-order.

### Snapshot-at-iter-start contract

The fixed-source component pointer is resolved **once at `Iter()` time**, not per `Next()` call. Mutations to the source entity between `Next()` calls within the same iteration are not visible. This matches the C upstream `it->sources[]` behaviour (populated once by `flecs_query_setfixed`).

`Field[T]` returns a **1-element slice** backed by this snapshot pointer. The pointer is valid for the entire iteration (not invalidated by `Next()`).

For `CachedQuery`, the pointer is **re-read at each `Iter()` call**, so updates to the source between separate iterations are visible on the next execution.

### Source-missing → zero results

If the source entity does not hold the fixed-source component (for a `TermAnd` term), the entire query yields **zero results**. This mirrors the C upstream `flecs_query_with → false` propagation at `eval.c:114-117`.

```go
// game has no SimTime → 0 matches even if many entities have Position.
q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.WithSourceTerm(simTimeID, game), // game lacks SimTime
)
```

### Singleton source pattern

Use `SingletonEntity` to resolve the canonical holder of a singleton component:

```go
flecs.SetSingleton(w, simTimeID)
// ... later, when constructing the query:
holder, ok := flecs.SingletonEntity(scope, simTimeID)
// then: flecs.WithSourceTerm(simTimeID, holder)
```

### Optional fixed-source (deliberate divergence from upstream)

Use `Maybe(componentID).Source(e)` when an absent component on the source should be **acceptable** rather than zeroing results:

```go
q := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.Maybe(simTimeID).Source(game), // absent = ok; use FieldMaybe
)
it := q.Iter()
for it.Next() {
    if st, ok := flecs.FieldMaybe[SimTime](it, simTimeID); ok {
        // use st[0].DT
    }
    // entities still match even when game lacks SimTime
}
```

This is a deliberate divergence from C upstream, which treats optional fixed-source uniformly with `TermAnd`. The Go port uses the `FieldMaybe`-friendly behaviour so callers can express "match these entities, and optionally bind a config from `game`."

### Limitations (this phase)

- `TermNot` with a fixed source is not supported (panics at construction).
- `TermOr` with a fixed source is not supported (panics at construction).
- A fixed-source term cannot be combined with `.Up()` / `.SelfUp()` / `.Cascade()`.
- The source entity must be alive at query-construction time (panics if dead).

---

## Query variables (single-variable v1)

**Shipped in v0.80.0.**

Query variables let terms express **relational joins** rather than per-entity filters. Instead of filtering the iterated entity (`$this`) against a fixed component set, a variable name acts as a runtime-resolved entity slot that is constrained to satisfy other terms in the same query.

The canonical motivating example — "spaceships docked to a planet":

```go
type SpaceShip struct{}
type Planet struct{ Name string }

shipID    := flecs.RegisterComponent[SpaceShip](w)
planetID  := flecs.RegisterComponent[Planet](w)
dockedToID := /* relationship entity, created via fw.NewEntity() */

// Query: SpaceShip, DockedTo($this, $planet), Planet($planet)
q := flecs.NewQueryFromTerms(w,
    flecs.With(shipID),
    flecs.WithPairTgtVar(dockedToID, "planet"), // $this has (DockedTo, $planet)
    flecs.WithVar(planetID, "planet"),           // $planet has Planet component
)
it := q.Iter()
for it.Next() {
    ship   := it.Entities()[0]            // the matched spaceship ($this)
    planet := it.Var("planet")            // the bound planet entity
    _ = ship; _ = planet
}
```

If spaceships A, B, C and planets P1, P2 are arranged as A→P1, B→P1, B→P2, C→P2, this query yields exactly four rows: (A, P1), (B, P1), (B, P2), (C, P2).

### Constructors

```go
// WithVar — check componentID on the variable-bound entity (SrcVar term).
// Equivalent to "Planet($planet)" in Flecs Query Language.
flecs.WithVar(componentID ID, varName string) Term

// WithPairTgtVar — match (rel, $varName) on $this (TgtVar term).
// Equivalent to "DockedTo($this, $planet)" in FQL.
flecs.WithPairTgtVar(rel ID, varName string) Term

// Chained setters — symmetrical with (Term).Source(e) from Phase 16.18.
flecs.With(componentID).SrcVar("varName")
flecs.With(relID).TgtVar("varName")
```

Panics at construction if `varName` is empty or if `componentID` / `rel` is zero.

### Reading variable bindings

```go
// Var returns the current binding for the named variable.
// Panics if the name is undefined or if Next() has not been called yet.
binding := it.Var("planet") // ID of the entity bound to $planet this row
```

`Entities()` and `Count()` return the `$this` entity (one entity per step, in variable-query mode).

`Field[T]` for a `WithVar` term reads the component from the variable-bound source entity, not `$this`:

```go
planets := flecs.Field[Planet](it, planetID) // reads from $planet, not $this
_ = planets[0].Name                          // "P1" or "P2"
```

### Join semantics

- **Driver variable**: the first variable name encountered in the term list (left-to-right) is the *driver*. Its domain is enumerated at `Iter()` time by intersecting all `WithVar` constraints for that name. No auto-optimization; first-defined wins.
- **Result materialisation**: all `(driver-entity, $this-entity)` pairs are pre-computed at `Iter()` time. Each `Next()` advances to the next pre-computed row.
- **Backward compat**: queries with zero variables use the existing fast path; no overhead.

### Variable count cap

v1 caps at **8 named variables** per query. Exceeding this panics at `NewQueryFromTerms` / `NewCachedQueryFromTerms` construction time.

### Cached queries

`NewCachedQueryFromTerms` with variable terms re-executes the full variable join on each `Iter()` call. The cache memoizes archetype-table membership for the driver term but not the per-binding join itself.

```go
cq := flecs.NewCachedQueryFromTerms(w,
    flecs.With(shipID),
    flecs.WithPairTgtVar(dockedToID, "planet"),
    flecs.WithVar(planetID, "planet"),
)
// Two separate iterations produce identical row sets (bindings re-computed each time).
it1 := cq.Iter()
it2 := cq.Iter()
```

### Composing with other term types

Variable queries compose with:

- **Fixed-source terms** (`WithSourceTerm` / `(Term).Source(e)`) — resolved once at iter start, independent of variable bindings.
- **`Without` terms** — applied as archetype filters on `$this` candidate tables.
- **`Or` groups** — evaluated per candidate table in `varCheckTable`.

### v1 limitations

- **Single variable**: only one named variable plus the implicit `$this`. Multi-variable join (e.g., `$planet` and `$star`) is deferred to Phase 16.25.x.
- **Entity-kind variables only**: no table-kind variables (`EcsVarTable`).
- **Positive constraints only**: `WithVar` / `WithPairTgtVar` only. Negative-variable constraints (`!Foo($this, $planet)`) are not supported.
- **`src` and `second` positions only**: variable in relationship name position (`$Rel($this, target)`) is not supported.
- **No FQL string parsing**: programmatic API only; the `$Var` string syntax from Flecs Query Language is not parsed by the Go port.
- **No join-order optimization**: first-defined variable is always the driver. Auto-optimization (matching upstream `compiler.c:1002-1021`) is Phase 16.25.x.

### Upstream C references

- `EcsIsVariable` (`include/flecs.h:772`) — flag marking a term ref as a variable.
- `ecs_term_ref_t` (`include/flecs.h:799-811`) — term ref struct; `.src` / `.second` can be variables.
- Variable kinds (`src/query/types.h:20-37`) — `EcsVarEntity` (single-entity); v1 ports this kind only.
- Variable discovery: `flecs_query_discover_vars` (`src/query/compiler/compiler.c:128`).
- Iterator binding: `flecs_query_var_set_entity` / `flecs_query_var_reset` (`src/query/engine/eval_utils.c:58-235`).
- Join-order optimizer (`compiler.c:1002-1021`) — explicitly out of scope for v1.

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

**Fixed per-term source** — ✅ **shipped in v0.73.0.** See [§ Fixed per-term source](#fixed-per-term-source) below.

**Query variables** — ✅ **shipped in v0.80.0** (single-variable v1). See [§ Query variables (single-variable v1)](#query-variables-single-variable-v1) above. Multi-variable join optimization deferred to Phase 16.25.x.

**Sorted queries** — ✅ shipped in v0.59.0. See [§ Sorted queries](#sorted-queries) below.

**Query groups** — ✅ shipped in v0.66.0. `GroupByFunc` partitions matched tables into labelled groups; `IterGroup` provides O(1) group-iterator access; `WithGroupBy` + `WithOrderBy` compose (sorted within each group). See [§ Query groups](#query-groups) above. (`Cascade` retains its dedicated implementation; refactor onto `WithGroupBy` is deferred.)

**Equality operators** — ✅ **shipped in v0.76.0** via [`IsEntity`](../query.go) / [`NotEntity`](../query.go) / [`NameMatches`](../query.go). See [§ Equality and name-match filters](#equality-and-name-match-filters) above. (`EcsPredLookup` / `$this == "name"` is a deliberate non-goal — use `World.Lookup(path)` + `IsEntity`.)

**AndFrom / OrFrom / NotFrom** — ✅ **shipped in v0.77.0** via [`AndFrom`](../query.go) / [`OrFrom`](../query.go) / [`NotFrom`](../query.go). See [§ AndFrom / OrFrom / NotFrom](#andfrom--orfrom--notfrom) above. Snapshot-at-construction semantics; diverges deliberately from upstream's live re-read (see CHANGELOG v0.77.0).

**Query scopes** — ✅ **shipped in v0.75.0** via [`WithoutScope`](../query.go) / `*ScopeBuilder`. See [§ Query scopes](#query-scopes) above.

**Access modifiers** — `In` / `InOut` / `Out` / `None` annotations on terms (used by the C scheduler for pipeline sync-point inference). Go flecs governs mutation via `Read`/`Write` scopes at the world level; per-term access annotations are not ported.

**Member value queries** — match on the runtime value of a component field (requires reflection/meta addon). Not yet ported in Go flecs.

---

## Disabled and Prefab entities

**Shipped in v0.57.0.** Queries exclude `Disabled` and `Prefab` entities by default. Opt in by mentioning either tag in any term kind (`With`, `Without`, `Maybe`, `Or`).

```go
// Ordinary query — Disabled and Prefab entities are invisible:
q := flecs.NewQuery(w, posID)

// Opt in to disabled entities only (Prefab still excluded):
q2 := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.With(w.Disabled()))

// Opt in to prefab entities only (Disabled still excluded):
q3 := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.With(w.Prefab()))

// Without(Disabled) also opts in — but then explicitly rejects disabled tables:
q4 := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.Without(w.Disabled()))

// Both Disabled + Prefab on same entity: must opt in to both:
q5 := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.With(w.Disabled()), flecs.With(w.Prefab()))
```

The exclusion is O(1) per table: a single `HasComponent` test per table per flag, with no per-entity cost. This mirrors C `EcsTableIsDisabled` / `EcsTableIsPrefab` (eval.c:88).

**Interaction**: `Disabled` and `Prefab` are independent. An entity carrying both must be opted in to both. The opt-in checks are commutative.

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
