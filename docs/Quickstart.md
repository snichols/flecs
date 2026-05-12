# Go flecs Quickstart

A tour of the core flecs concepts with runnable Go examples. Each section focuses on one idea and shows the minimal API you need.

For a deeper treatment of any topic, follow the links in [Next Steps](#next-steps).

---

## Hello World

A complete program: create a world, attach components, iterate, read back.

```go
package main

import (
    "fmt"
    "github.com/snichols/flecs"
)

type Position struct{ X, Y float32 }
type Velocity struct{ DX, DY float32 }

func main() {
    w := flecs.New()

    // Create an entity and attach two components inside a Write scope.
    var e flecs.ID
    w.Write(func(fw *flecs.Writer) {
        e = fw.NewEntity()
        flecs.Set(fw, e, Position{X: 1, Y: 2})
        flecs.Set(fw, e, Velocity{DX: 0.5, DY: 0})
    })

    // Iterate every entity with both Position and Velocity; update in place.
    w.Read(func(r *flecs.Reader) {
        flecs.Each2[Position, Velocity](r, func(_ flecs.ID, p *Position, v *Velocity) {
            p.X += v.DX
            p.Y += v.DY
        })
    })

    // Read back the result.
    w.Read(func(r *flecs.Reader) {
        if pos, ok := flecs.Get[Position](r, e); ok {
            fmt.Printf("position: %.1f %.1f\n", pos.X, pos.Y) // 1.5 2.0
        }
    })
}
```

---

## World

`flecs.New()` creates the ECS container. It owns entities, component metadata, and archetype tables. There is no practical limit on the number of worlds in one process.

```go
w := flecs.New()
```

Access the world through two scoped methods:

- **`w.Write(func(*Writer))`** — exclusive write scope. Structural mutations (`Set`, `Remove`, `Delete`, `AddID`) are enqueued and flushed when the scope exits.
- **`w.Read(func(*Reader))`** — shared-read scope. Multiple goroutines can hold concurrent Read scopes. In-place field mutations are safe; structural changes must go through Write.

---

## Entities

Entities are 64-bit IDs (`flecs.ID`). Create them inside a `Write` scope; delete and inspect them directly on the World:

```go
var e flecs.ID
w.Write(func(fw *flecs.Writer) {
    e = fw.NewEntity()
})

fmt.Println(w.IsAlive(e)) // true

w.Delete(e)
fmt.Println(w.IsAlive(e)) // false
```

The upper 32 bits of an entity ID are a generation counter. `IsAlive` uses that counter to detect reuse of deleted slots.

---

## Components

Any Go struct (or fixed-size type) is a component. Register it with `flecs.RegisterComponent[T]` to obtain its stable entity ID, then attach and read values:

```go
type Health struct{ HP int }

healthID := flecs.RegisterComponent[Health](w)

var e flecs.ID
w.Write(func(fw *flecs.Writer) {
    e = fw.NewEntity()
    flecs.Set(fw, e, Health{HP: 100})
})

w.Read(func(r *flecs.Reader) {
    if h, ok := flecs.Get[Health](r, e); ok {
        fmt.Println(h.HP) // 100
    }
    fmt.Println(flecs.Has[Health](r, e))  // true
    fmt.Println(flecs.Owns[Health](r, e)) // true (locally stored, not inherited)
    _ = healthID
})

// Remove a component (structural change → goes through Write):
w.Write(func(fw *flecs.Writer) {
    flecs.Remove[Health](fw, e)
})
```

`RegisterComponent` is idempotent — multiple calls for the same type return the same ID and are no-ops after the first call.

---

## Named Entities

Any entity can have a name. Names form dot-separated paths when combined with a ChildOf hierarchy:

```go
var scene, player flecs.ID
w.Write(func(fw *flecs.Writer) {
    scene = fw.NewEntity()
    player = fw.NewEntity()
    w.SetName(scene, "scene")
    w.SetName(player, "player")
    flecs.AddID(fw, player, flecs.MakePair(w.ChildOf(), scene))
})

fmt.Println(w.PathOf(player)) // scene.player

if found, ok := w.Lookup("scene.player"); ok {
    fmt.Println(found == player) // true
}

if name, ok := w.GetName(player); ok {
    fmt.Println(name) // player
}
```

---

## Tags

A tag is a zero-payload marker. Use an empty struct registered as a component:

```go
type Enemy struct{}

var e flecs.ID
w.Write(func(fw *flecs.Writer) {
    e = fw.NewEntity()
    flecs.Set(fw, e, Enemy{})
})

w.Read(func(r *flecs.Reader) {
    fmt.Println(flecs.Has[Enemy](r, e)) // true
})
```

For a fully runtime tag — one created dynamically without a compile-time type — use a plain entity ID with `AddID` / `HasID`:

```go
var tagEnemy, e flecs.ID
w.Write(func(fw *flecs.Writer) {
    tagEnemy = fw.NewEntity()
    e = fw.NewEntity()
    flecs.AddID(fw, e, tagEnemy)
})

w.Read(func(r *flecs.Reader) {
    fmt.Println(flecs.HasID(r, e, tagEnemy)) // true
})
```

---

## Ergonomic Iteration

`Each1`, `Each2`, `Each3`, `Each4` iterate every entity that owns all the requested components. The `*T` pointers are live views into archetype columns — no allocation per entity:

```go
type Position struct{ X, Y float32 }
type Velocity struct{ DX, DY float32 }

// Create some entities first …

w.Read(func(r *flecs.Reader) {
    flecs.Each2[Position, Velocity](r, func(_ flecs.ID, p *Position, v *Velocity) {
        p.X += v.DX // in-place update
        p.Y += v.DY
    })
})
```

Pass `e flecs.ID` as the first parameter when you need the entity ID inside the callback:

```go
w.Read(func(r *flecs.Reader) {
    flecs.Each1[Position](r, func(e flecs.ID, p *Position) {
        fmt.Printf("entity %d pos: %.1f %.1f\n", e, p.X, p.Y)
    })
})
```

---

## Queries

### Simple AND query

`flecs.NewQuery` builds a query that matches entities owning all listed components. Pull results with `Iter`, `Next`, and `Field`:

```go
type Position struct{ X, Y float32 }

posID := flecs.RegisterComponent[Position](w)

// … create entities …

q := flecs.NewQuery(w, posID)
it := q.Iter()
for it.Next() {
    positions := flecs.Field[Position](it, posID)
    for i, p := range positions {
        fmt.Printf("%d: %.1f %.1f\n", i, p.X, p.Y)
    }
}
```

### NOT and Optional terms

`flecs.NewQueryFromTerms` accepts `With`, `Without`, and `Maybe` for richer filters:

```go
type Position struct{ X, Y float32 }
type Velocity struct{ DX, DY float32 }

posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)
var deadID flecs.ID
w.Write(func(fw *flecs.Writer) { deadID = fw.NewEntity() })

// Match entities with Position but NOT dead.
qAlive := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.Without(deadID),
)

// Match entities with Position; Velocity is optional.
qMaybe := flecs.NewQueryFromTerms(w,
    flecs.With(posID),
    flecs.Maybe(velID),
)
qMaybe.Each(func(it *flecs.QueryIter) {
    for it.Next() {
        positions := flecs.Field[Position](it, posID)
        velocities, hasVel := flecs.FieldMaybe[Velocity](it, velID)
        for i := range positions {
            if hasVel {
                positions[i].X += velocities[i].DX
            }
        }
    }
})
_ = qAlive
```

### Cached queries

`flecs.NewCachedQuery` pre-scans tables at construction and tracks new archetypes automatically. Use cached queries for systems and change detection:

```go
cq := flecs.NewCachedQuery(w, posID)
fmt.Println(cq.EntityCount()) // always current
defer cq.Close()
```

---

## Relationships

A pair encodes a `(relationship, target)` edge. Any two entities can act as relationship and target. `flecs.MakePair` packs them into a single 64-bit ID:

```go
var likes, alice, pizza flecs.ID
w.Write(func(fw *flecs.Writer) {
    likes = fw.NewEntity()
    alice = fw.NewEntity()
    pizza = fw.NewEntity()
    flecs.AddID(fw, alice, flecs.MakePair(likes, pizza)) // alice likes pizza
})

w.Read(func(r *flecs.Reader) {
    fmt.Println(flecs.HasID(r, alice, flecs.MakePair(likes, pizza))) // true
})

w.Write(func(fw *flecs.Writer) {
    flecs.RemoveID(fw, alice, flecs.MakePair(likes, pizza))
})
```

Pairs can carry data. Use `flecs.SetPair` and `flecs.GetPair`:

```go
type Distance struct{ Meters float32 }

var near, bob, office flecs.ID
w.Write(func(fw *flecs.Writer) {
    near = fw.NewEntity()
    bob = fw.NewEntity()
    office = fw.NewEntity()
    flecs.SetPair(fw, bob, near, office, Distance{Meters: 500})
})

w.Read(func(r *flecs.Reader) {
    if d, ok := flecs.GetPair[Distance](r, bob, near, office); ok {
        fmt.Printf("%.0f m\n", d.Meters) // 500 m
    }
})
```

---

## Hierarchies (ChildOf)

The built-in `ChildOf` relationship links an entity to its parent. Deleting the parent cascades to all descendants:

```go
var scene, car, wheel flecs.ID
w.Write(func(fw *flecs.Writer) {
    scene = fw.NewEntity()
    car = fw.NewEntity()
    wheel = fw.NewEntity()
    w.SetName(scene, "scene")
    w.SetName(car, "car")
    w.SetName(wheel, "wheel")
    flecs.AddID(fw, car, flecs.MakePair(w.ChildOf(), scene))
    flecs.AddID(fw, wheel, flecs.MakePair(w.ChildOf(), car))
})

// Navigate upward.
if parent, ok := w.ParentOf(wheel); ok {
    name, _ := w.GetName(parent)
    fmt.Println("wheel's parent:", name) // car
}

// Iterate direct children.
w.EachChild(scene, func(child flecs.ID) bool {
    name, _ := w.GetName(child)
    fmt.Println("child of scene:", name) // car
    return true
})

// Cascade delete — car and wheel are also removed.
w.Delete(scene)
fmt.Println(w.IsAlive(wheel)) // false
```

---

## Prefabs (IsA)

The built-in `IsA` relationship provides prototype inheritance. `Get` and `Has` walk the IsA chain when a component is not locally owned:

```go
type Health struct{ HP int }

var dragon, redDragon flecs.ID
w.Write(func(fw *flecs.Writer) {
    dragon = fw.NewEntity()
    flecs.Set(fw, dragon, Health{HP: 100})

    redDragon = fw.NewEntity()
    flecs.AddID(fw, redDragon, flecs.MakePair(w.IsA(), dragon))
    // redDragon owns no local Health — it inherits from dragon.
})

w.Read(func(r *flecs.Reader) {
    if h, ok := flecs.Get[Health](r, redDragon); ok {
        fmt.Println(h.HP) // 100 — from dragon
    }
    fmt.Println(flecs.Owns[Health](r, redDragon)) // false
})

// Override locally (copy-on-write). redDragon now owns its own slot.
w.Write(func(fw *flecs.Writer) {
    flecs.Set(fw, redDragon, Health{HP: 150})
})

// Restore inheritance: remove the local override.
w.Write(func(fw *flecs.Writer) {
    flecs.Remove[Health](fw, redDragon)
})

w.Read(func(r *flecs.Reader) {
    if h, ok := flecs.Get[Health](r, redDragon); ok {
        fmt.Println(h.HP) // 100 — back to dragon's value
    }
})
```

---

## Systems

A system is a cached query combined with a callback. Systems run automatically on every `w.Progress` call:

```go
type Position struct{ X, Y float32 }
type Velocity struct{ DX, DY float32 }

posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)

w.Write(func(fw *flecs.Writer) {
    e := fw.NewEntity()
    flecs.Set(fw, e, Position{X: 0, Y: 0})
    flecs.Set(fw, e, Velocity{DX: 1, DY: 0})
})

moveQ := flecs.NewCachedQuery(w, posID, velID)
flecs.NewSystem(w, moveQ, func(dt float32, it *flecs.QueryIter) {
    for it.Next() {
        positions := flecs.Field[Position](it, posID)
        velocities := flecs.Field[Velocity](it, velID)
        for i := range positions {
            positions[i].X += velocities[i].DX * dt
            positions[i].Y += velocities[i].DY * dt
        }
    }
})

w.Progress(1.0 / 60.0) // advance one frame
fmt.Println(w.FrameCount()) // 1
```

Use `flecs.NewSystemInPhase` to place a system in a specific pipeline phase. The four built-in phases run in this order on every `Progress` call:

| Phase accessor | Purpose |
|---|---|
| `w.PreUpdate()` | Input, network receive |
| `w.OnFixedUpdate()` | Physics (fixed timestep via `w.SetFixedTimestep`) |
| `w.OnUpdate()` | Game logic (default phase for `NewSystem`) |
| `w.PostUpdate()` | Rendering, network send |

```go
inputQ := flecs.NewCachedQuery(w, posID)
flecs.NewSystemInPhase(w, w.PreUpdate(), inputQ, func(dt float32, it *flecs.QueryIter) {
    // runs before OnUpdate
})
```

---

## Observers

Observers fire a callback when an event (`EventOnSet`, `EventOnAdd`, `EventOnRemove`) matches their component filter. Unlike hooks (one per component per event), observers support multiple subscribers:

```go
type Score struct{ Points int }

obs := flecs.Observe[Score](w, flecs.EventOnSet, func(_ *flecs.Writer, _ flecs.ID, s Score) {
    fmt.Printf("score updated: %d\n", s.Points)
})

var e flecs.ID
w.Write(func(fw *flecs.Writer) {
    e = fw.NewEntity()
    flecs.Set(fw, e, Score{Points: 42}) // observer fires when Write scope closes
})

// Unsubscribe to stop receiving future events.
obs.Unsubscribe()

w.Write(func(fw *flecs.Writer) {
    flecs.Set(fw, e, Score{Points: 99}) // observer no longer fires
})
```

---

## Verification

The code patterns in this guide are verified by [`docs/quickstart_examples_test.go`](quickstart_examples_test.go).
Run from the repo root:

```sh
go test ./docs/...          # verify Quickstart examples
go test ./... -race -count=1 -timeout=180s  # full suite
```

The existing `example_*_test.go` files in the repo root are the authoritative runnable examples and serve as a secondary verification source.

---

## Next Steps

| Topic | File | Status |
|---|---|---|
| Entities & Components | [EntitiesComponents.md](EntitiesComponents.md) | pending Phase 14.1 |
| Queries | [Queries.md](Queries.md) | pending Phase 14.2 |
| Relationships | [Relationships.md](Relationships.md) | pending Phase 14.3 |
| Hierarchies | [HierarchiesManual.md](HierarchiesManual.md) | pending Phase 14.4 |
| Prefabs | [PrefabsManual.md](PrefabsManual.md) | pending Phase 14.5 |
| Systems | [Systems.md](Systems.md) | pending Phase 14.6 |
| Observers | [ObserversManual.md](ObserversManual.md) | pending Phase 14.7 |
| Component Traits | [ComponentTraits.md](ComponentTraits.md) | pending Phase 14.8 |
| REST API | [FlecsRemoteApi.md](FlecsRemoteApi.md) | pending Phase 14.9 |
| Design with flecs | [DesignWithFlecs.md](DesignWithFlecs.md) | pending Phase 14.10 |
| Reference Manual | [Manual.md](Manual.md) | pending Phase 14.11 |
| FAQ | [FAQ.md](FAQ.md) | pending Phase 14.12 |
