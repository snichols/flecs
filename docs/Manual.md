# Manual

Go flecs is an Entity Component System (ECS) library for Go: entities are 64-bit handles, components are plain Go structs stored in cache-friendly structure-of-arrays tables (archetypes), and systems are callbacks that iterate matching entities every frame. This document is the top-level reference hub — it summarises each major concept and points into the per-topic manuals for deeper coverage.

Start with the [Quickstart](Quickstart.md) for a hands-on walk-through. Then follow the links in the concept map below for the topic you need.

---

## Concept Map

| Concept | Where to go |
|---------|------------|
| Entities, components, tags, hooks | [EntitiesComponents.md](EntitiesComponents.md) |
| Queries — uncached, cached, terms | [Queries.md](Queries.md) |
| Relationships and pairs | [Relationships.md](Relationships.md) |
| Parent / child hierarchies | [HierarchiesManual.md](HierarchiesManual.md) |
| Prefabs and prototype inheritance | [PrefabsManual.md](PrefabsManual.md) |
| Systems and the pipeline | [Systems.md](Systems.md) |
| Observers and reactive callbacks | [ObserversManual.md](ObserversManual.md) |
| Component traits | [ComponentTraits.md](ComponentTraits.md) |
| REST / HTTP inspection | [FlecsRemoteApi.md](FlecsRemoteApi.md) |
| ECS design patterns | [DesignWithFlecs.md](DesignWithFlecs.md) |
| Benchmarks | [BENCH.md](../BENCH.md) |

---

## World Lifecycle

A `*World` is the root object. Create one with `flecs.New()` and let the GC collect it when you are done — there is no explicit destroy call. All entity, component, and system registration must go through the world.

```go
w := flecs.New()

// Register component types once at startup.
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)

// Create entities and set initial component values inside a Write scope.
w.Write(func(fw *flecs.Writer) {
    e := fw.NewEntity()
    flecs.Set(fw, e, Position{X: 0, Y: 0})
    flecs.Set(fw, e, Velocity{DX: 1, DY: 0})
    _ = e
})

// Advance the simulation by one frame (dt = 1/60 s).
w.Progress(1.0 / 60.0)
```

`w.Progress(dt)` runs all registered systems in pipeline order:
`PreUpdate` → `OnFixedUpdate` (accumulator loop) → `OnUpdate` → `PostUpdate`.
Structural mutations queued inside a phase are flushed before the next phase begins. For the full pipeline and phase reference, see [Systems.md](Systems.md).

World state can be read between frames without locking:

```go
w.Read(func(r *flecs.Reader) {
    p, ok := flecs.Get[Position](r, e)
    if ok {
        _ = p.X
    }
})
```

`w.FrameCount()` returns the number of `Progress` calls so far. `w.Time()` returns accumulated simulation time. `w.IsAlive(e)` tests whether an entity ID is still valid — always check before dereferencing a stored handle.

---

## API Design

### Naming Conventions

Go flecs follows standard Go idioms:

- **Types** use `PascalCase`: `Position`, `Velocity`, `World`, `CachedQuery`.
- **Functions and methods** use `camelCase`: `NewEntity`, `RegisterComponent`, `SetWorkerCount`.
- **Generic helpers** are package-level functions parameterized on the component type: `flecs.Get[T]`, `flecs.Set[T]`, `flecs.Field[T]`.
- **Event constants** use `PascalCase` with an `Event` prefix: `EventOnAdd`, `EventOnSet`, `EventOnRemove`.
- **Phase accessors** are world methods: `w.PreUpdate()`, `w.OnUpdate()`, `w.PostUpdate()`, `w.OnFixedUpdate()`.

C flecs uses `ECS_COMPONENT`, `ECS_SYSTEM`, and `ecs_*` function-style macros. None of these exist in Go flecs — Go generics and first-class functions replace them entirely. See [EntitiesComponents.md](EntitiesComponents.md) for the component registration and mutation APIs.

### Idempotence

Most structural operations in Go flecs are idempotent. Calling `fw.AddID(e, id)` twice has the same observable effect as calling it once: the component is present. `flecs.Set` is idempotent in terms of the final component value but fires `OnSet` observers on every call — a relevant distinction when observers have side effects.

This design enables declarative initialization: after a block of `Set` / `AddID` calls you know the entity's exact component set, regardless of prior state.

### Error Handling

Go flecs uses panics for programming errors, not returned errors. The library panics when:

- A caller violates an access contract (for example, mutating the world outside a `Write` scope when `ExclusiveAccess` is active).
- An API precondition is violated (nil world, closed query, unregistered component type).
- An OS-level resource allocation fails.

There are no sentinel error return values for normal-path operations. Application code does not need to check error returns from `Set`, `Get`, `AddID`, or similar calls. This keeps hot-path code clean and means bugs surface as panics with stack traces rather than as silently propagated errors.

`flecs.Get[T]` returns `(T, bool)` — the boolean signals whether the component was present, not an error.

### Go Modules as the Module System

C flecs has `ECS_MODULE` / `world.import` for packaging systems and components into reusable units. In Go flecs, **Go packages are the module system**. A feature package registers its components and systems in an `Init(w *flecs.World)` function called by the application at startup. Because Go's import system enforces single-import semantics, the C idempotence guarantee on module imports is provided automatically by the language.

---

## Deferred Operations

Inside a `w.Write` scope — or during `w.Progress` — structural mutations (`Set`, `Remove`, `Delete`, `AddID`, `NewEntity`) are executed immediately when the world is idle, or queued as deferred commands when the world is in a progress pass. The deferred commands are flushed at the end of each phase so that cross-phase visibility is guaranteed.

Calling `Get` or `Has` while commands are queued reflects the pre-mutation state. If you need a value that depends on a pending mutation, flush by ending the current phase or by structuring the mutation into a separate `Write` call outside `Progress`.

For more on the deferred model, including the parallel per-stage command queues that eliminate lock contention under multi-threaded dispatch, see [Systems.md — Deferred mutations in parallel systems](Systems.md).

---

## Concurrency Model

Go flecs is **not goroutine-safe by default**. All world access must be externally synchronized. The library provides two mechanisms:

### Reader / Writer Scopes (Phase 10.x)

`w.Read(fn)` and `w.Write(fn)` are the normal access gates. They are backed by a `sync.RWMutex`:

- Multiple goroutines can call `w.Read` concurrently — reads do not block each other.
- `w.Write` acquires an exclusive lock; concurrent reads and writes are serialized.

```go
// Two goroutines reading the world concurrently — safe.
go w.Read(func(r *flecs.Reader) { _, _ = flecs.Get[Position](r, e) })
go w.Read(func(r *flecs.Reader) { _, _ = flecs.Get[Velocity](r, e) })
```

During `w.Progress` the pipeline acquires a write lock internally, so no external `w.Write` can overlap with a progress call.

### ExclusiveAccess (Phase 12.x)

`ExclusiveAccess` is an escape hatch for game loops that own the world on a single goroutine and want to avoid mutex overhead on every `Read`/`Write` call:

```go
w.ExclusiveAccessBegin("main-loop")
// Between Begin and End, direct field access and world calls are allowed
// without the RWMutex — but only from the owning goroutine.
// Calling any world method from a different goroutine panics immediately.
w.Progress(dt)
w.ExclusiveAccessEnd(false) // false = release ownership; true = lock world for writes
```

When `ExclusiveAccessBegin` is active, any attempt to access the world from a different goroutine triggers an immediate panic, surfacing concurrency bugs at the exact call site rather than as silent data races. This is enforced via goroutine-ID tracking — a lightweight alternative to a full mutex on every operation.

`ExclusiveAccessEnd(true)` leaves the world in a write-locked state (no goroutine may mutate it). Call `ExclusiveAccessEnd(false)` to release ownership entirely.

### Parallel and Multi-Threaded Systems

Worker goroutines are enabled with `w.SetWorkerCount(n)`:

```go
w.SetWorkerCount(4) // spawn 4 worker goroutines

sys.SetParallel(true)      // system may run concurrently with disjoint siblings
sys.SetMultiThreaded(true) // system's rows are split across all workers
```

Parallel systems within a phase whose write sets are pairwise disjoint run concurrently. Multi-threaded systems split their query iterator across all workers, each receiving a disjoint row slice. Structural mutations from within parallel or multi-threaded callbacks are enqueued on per-stage command queues (one per worker) and applied after the phase, so there is no contention on the mutation path.

For the full parallel dispatch model, disjoint write-set inference, and `SetWriteSet`, see [Systems.md](Systems.md).

---

## Performance

Go flecs is designed for cache-friendly iteration. Key characteristics:

- **Archetype storage** — entities with identical component sets share one table (SoA columns). Iterating a query touches only the tables that match, not the full entity index.
- **Cached queries** — `NewCachedQuery` pre-filters tables at construction and tracks new archetypes automatically. Per-frame cost is proportional to matched entities, not total entity count.
- **Parallel dispatch** — `SetParallel` and `SetMultiThreaded` scale iteration across worker goroutines with no synchronization on the read path.
- **In-place mutation** — mutating `Field[T]` slices directly (rather than calling `Set`) avoids the deferred-command allocation and is the fastest mutation path.

For benchmark numbers, methodology, and a comparison baseline workflow, see [BENCH.md](../BENCH.md).

---

## C flecs Features Not Applicable in Go

The upstream C manual covers topics that do not apply to the Go port:

| C feature | Go equivalent / status |
|-----------|----------------------|
| CMake / meson build integration | `go get github.com/snichols/flecs` |
| `ECS_COMPONENT`, `ECS_SYSTEM` macros | `flecs.RegisterComponent[T]`, `flecs.NewSystem` |
| `ECS_MODULE` / `world.import` | Go packages with an `Init(w)` function |
| `ecs_os_free` memory ownership rules | GC-managed; no manual free needed |
| C99 naming (`ecs_*`, `FLECS__E`) | Go generics and method syntax |
| Portability (C89/C99 ABI) | Go module system; cross-platform via `GOOS`/`GOARCH` |

---

## See Also

- [Quickstart](Quickstart.md) — hands-on introduction; the best starting point for newcomers.
- [EntitiesComponents.md](EntitiesComponents.md) — entity and component API in full detail.
- [Queries.md](Queries.md) — query terms, cached vs uncached, change detection.
- [Relationships.md](Relationships.md) — pairs, ChildOf, IsA.
- [HierarchiesManual.md](HierarchiesManual.md) — parent/child hierarchies, tree traversal.
- [PrefabsManual.md](PrefabsManual.md) — prefab templates, inheritance, copy-on-write.
- [Systems.md](Systems.md) — pipeline, phases, parallel dispatch, fixed timestep.
- [ObserversManual.md](ObserversManual.md) — hooks, observers, reactive patterns.
- [ComponentTraits.md](ComponentTraits.md) — inheritable, sparse, and other per-component customisation.
- [FlecsRemoteApi.md](FlecsRemoteApi.md) — REST handler and HTTP inspection endpoints.
- [DesignWithFlecs.md](DesignWithFlecs.md) — opinionated ECS design guide.
- [BENCH.md](../BENCH.md) — benchmark index and measurement methodology.
