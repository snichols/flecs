# flecs (Go port)

An idiomatic, high-performance Go port of the [flecs](https://github.com/SanderMertens/flecs)
Entity Component System library. Archetype-based storage, generic-typed API,
zero third-party dependencies.

[![Go version](https://img.shields.io/badge/go-1.26-blue)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![upstream](https://img.shields.io/badge/upstream-flecs-orange)](https://github.com/SanderMertens/flecs)

---

## Quick start

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

    // Create an entity and attach components.
    e := w.NewEntity()
    flecs.Set(w, e, Position{X: 1, Y: 2})
    flecs.Set(w, e, Velocity{DX: 0.5, DY: 0})

    // Iterate every entity that has both Position and Velocity.
    flecs.Each2[Position, Velocity](w, func(id flecs.ID, p *Position, v *Velocity) {
        p.X += v.DX
        p.Y += v.DY
    })

    // Read back.
    if pos, ok := flecs.Get[Position](w, e); ok {
        fmt.Printf("position: %.1f %.1f\n", pos.X, pos.Y) // 1.5 2.0
    }

    // Register a system and run one frame.
    posID := flecs.RegisterComponent[Position](w)
    q := flecs.NewCachedQuery(w, posID)
    flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
        for it.Next() {
            positions := flecs.Field[Position](it, posID)
            for i := range positions {
                positions[i].X += positions[i].X * dt // integrate
            }
        }
    })
    w.Progress(1.0 / 60.0)
    fmt.Printf("frame: %d\n", w.FrameCount()) // 1
}
```

---

## Core concepts

### Entities

Entities are 64-bit IDs (`flecs.ID`). The lower 32 bits are a unique index; the
upper 32 bits are a generation counter used to detect reuse of a dead slot.
Components are first-class entities — every registered component type gets its
own entity ID.

```go
e := w.NewEntity()
w.Delete(e)
fmt.Println(w.IsAlive(e)) // false
```

### Components

Any Go struct (or any fixed-size type) can be a component. `RegisterComponent[T]`
returns the component's entity ID. `Set[T]` and `Get[T]` write and read typed
values. Components are stored in structure-of-arrays columns; no heap allocation
per entity.

```go
type Health struct{ HP int }
hid := flecs.RegisterComponent[Health](w)
flecs.Set(w, e, Health{HP: 100})
h, ok := flecs.Get[Health](w, e)
```

### Archetypes

Entities sharing the same component set are grouped into an *archetype table*.
Migrating a component set (Set, Remove, AddID) moves the entity to the matching
table. Iteration is O(entity-count) with no virtual dispatch or cache misses
within a table.

### Queries

- `Each1`/`Each2`/`Each3`/`Each4` — ergonomic lambda iteration for 1–4 components.
- `NewQuery` + `Iter` + `Field[T]` — pull-style iteration for dynamic AND-only term lists.
- `NewQueryFromTerms` — structured terms with `With`, `Without`, `Maybe` (NOT / Optional support).
- `NewCachedQuery` / `NewCachedQueryFromTerms` — persistent queries that incrementally track new tables.

### Pipelines

`Progress(dt)` runs all registered systems in four built-in phases:

| Phase | ID accessor | Description |
|---|---|---|
| PreUpdate | `w.PreUpdate()` | Input, network receive |
| OnFixedUpdate | `w.OnFixedUpdate()` | Physics (fixed timestep) |
| OnUpdate | `w.OnUpdate()` | Game logic (default phase) |
| PostUpdate | `w.PostUpdate()` | Rendering, network send |

### Concurrency model

Outside `Progress`, the world is single-threaded by convention. For concurrent
read access — for example, parallelising an expensive query across workers —
wrap the read window in `w.Readonly(func() { ... })`:

```go
w.Readonly(func() {
    var wg sync.WaitGroup
    for range numWorkers {
        wg.Add(1)
        go func() {
            defer wg.Done()
            flecs.Each1[Position](w, func(e flecs.ID, p *Position) { ... })
        }()
    }
    wg.Wait()
}) // deferred writes (if any) are applied here
```

While the window is open, any goroutine that calls a mutator (`Set`, `Remove`,
`Delete`, `AddID`, `RemoveID`, `SetPair`, `SetByID`) has its operation buffered
in the deferred-command queue and applied when `ReadonlyEnd` is called.
Readers take **no locks** — the readonly flag guarantees nothing mutates world
state during the window, so all ECS tables are safe to read concurrently.

---

## Feature index

| Feature | API |
|---|---|
| Generic typed read/write | `Set[T]`, `Get[T]`, `Has[T]`, `Owns[T]`, `Remove[T]` |
| Archetype SoA storage | automatic; `RegisterComponent[T]` |
| Low-level ID API | `AddID`, `RemoveID`, `HasID`, `OwnsID` |
| Pair IDs / relationships | `MakePair`, `SetPair[T]`, `GetPair[T]` |
| ChildOf hierarchy | `w.ChildOf()`, `EachChild`, `ParentOf`, cascade delete |
| IsA inheritance | `w.IsA()`, `Get`/`Has` walk the chain, `PrefabOf` |
| Named entities | `SetName`, `GetName`, `Lookup`, `PathOf` |
| Hooks (single) | `OnAdd[T]`, `OnSet[T]`, `OnRemove[T]` |
| Observers (multi) | `Observe[T]`, `ObserveID`, `Observe2[T]`, `Unsubscribe` |
| Deferred commands | `Defer`, `DeferBegin`, `DeferEnd` |
| Readonly concurrency window | `w.Readonly(fn)`, `ReadonlyBegin`, `ReadonlyEnd` |
| Exclusive-access ownership assertion | `ExclusiveAccessBegin`, `ExclusiveAccessEnd` — always on; panics with `ErrExclusiveAccessViolation` on cross-goroutine violations; common case costs one `atomic.Load` per call |
| NOT / Optional query terms | `NewQueryFromTerms`, `With`, `Without`, `Maybe`, `FieldMaybe` |
| OR query terms | `Or`, `TermOr`, `FieldMaybe` on Or-group IDs |
| Systems + pipeline | `NewSystem`, `NewSystemInPhase`, `Progress` |
| Parallel dispatch | `sys.SetParallel(true)`, `sys.SetWriteSet(ids)`, `w.SetWorkerCount(n)` |
| Fixed timestep | `SetFixedTimestep`, `OnFixedUpdate` phase |
| JSON serialization | `w.MarshalJSON()`, `w.UnmarshalJSON()` (entities + components + names + pairs: ChildOf/IsA hierarchies + custom tag/data pairs) |
| Change detection | `q.Changed()` — opt-in per-table dirty tracking on `CachedQuery` |
| Stats / observability | `w.Stats()` — entity/table/query/system counts, per-phase frame timing, per-component table counts |
| REST API | `NewRESTHandler(w)` — read-only HTTP inspection + snapshot save/load (`GET /stats`, `/components`, `/entities`, `/snapshot`; `PUT /snapshot`) |
| Structured logging | `w.SetLogger(*slog.Logger)` — lifecycle events at DEBUG level; nil-logger fast path (single pointer compare) |

---

## Comparison to upstream flecs (C)

| Feature | Go port | Upstream C |
|---|---|---|
| Archetype-based storage | ✅ | ✅ |
| Generic typed API | ✅ (Go generics) | ✅ (macros) |
| Pair IDs | ✅ | ✅ |
| ChildOf / IsA | ✅ | ✅ |
| Hooks | ✅ | ✅ |
| Multi-subscriber observers | ✅ | ✅ |
| Deferred commands | ✅ | ✅ |
| 4-phase pipeline | ✅ | ✅ |
| Fixed timestep | ✅ | ✅ |
| NOT / Optional query terms | ✅ (`With`, `Without`, `Maybe`) | ✅ |
| OR query terms | ✅ (`Or`, `TermOr`, `FieldMaybe` on Or-group IDs) | ✅ |
| Up/Down traversal in queries | ❌ deferred | ✅ |
| Change detection | ✅ (`CachedQuery.Changed()`, per-table) | ✅ |
| Parallel system dispatch | ✅ (`SetParallel`, `SetWriteSet`, `SetWorkerCount`; per-phase disjoint write-set batching) | ✅ |
| REST API addon (minimal) | ✅ (`NewRESTHandler`, read-only inspection + snapshot) | ✅ |
| Table-graph traversal queries | ❌ deferred | ✅ |

See [ROADMAP.md](ROADMAP.md) for the full list of deferred work.

---

## Installation

```sh
go get github.com/snichols/flecs
```

Requires Go 1.26+. No third-party dependencies.

---

## Status

Pre-1.0. API may evolve between minor versions. See [ROADMAP.md](ROADMAP.md).

---

## License

MIT — see [LICENSE](LICENSE).

---

## Acknowledgments

This port is based on [flecs](https://github.com/SanderMertens/flecs) by
[Sander Mertens](https://github.com/SanderMertens). The ID encoding, archetype
model, and relationship semantics follow the upstream design closely.
