# FAQ

Frequently asked questions about Go flecs. For a hands-on introduction see [Quickstart](Quickstart.md); for the full reference see [Manual](Manual.md).

---

## General

### What is an ECS?

ECS stands for Entity Component System. Entities are IDs; components are plain data attached to those IDs; systems are functions that iterate over entities matching a set of components.

The pattern separates data from behavior and enables the runtime to group entities with the same component set into contiguous arrays — archetypes — so systems process hot, cache-friendly data.

### What is an archetype?

An archetype is a table that stores all entities with exactly the same set of component IDs. Every column in the table is one component type; every row is one entity. Iteration walks rows without pointer chasing, which maximises CPU cache utilisation.

When a component is added or removed, the entity migrates to a different archetype. Bulk creation, deferred commands, and the coalescing command queue all minimise the number of migrations per frame.

Other archetype ECS implementations include Unity DOTS, Unreal Mass, and Bevy ECS.

### How does Go flecs compare to upstream C flecs?

Go flecs is an independent Go implementation of the archetype ECS design, not a cgo binding. It targets the same conceptual model — archetypes, queries, systems, observers, relationships, prefabs, pipelines — while adapting APIs to Go idioms: generic type parameters instead of macros, `Read`/`Write` scopes instead of readonly-begin/end, `log/slog` instead of custom loggers, and Go packages in place of the C module system.

Feature coverage through v0.31.0 is substantial but not complete. See [docs/README.md](README.md) for the full feature-gap list discovered during the documentation port.

---

## Go Specifics

### Why generics instead of `interface{}`?

Generic type parameters (`RegisterComponent[Position](w)`, `flecs.Get[Position](r, e)`) let the compiler enforce type safety without boxing or type assertions at every call site. The component ID is tied to `Position` at compile time, so a mismatched `Get[Velocity]` on a `Position` component is a compile error, not a runtime panic.

`interface{}` would require casts everywhere, lose the compiler's ability to verify field access, and add allocation pressure on the hot iteration path.

### Why panic instead of returning errors?

Operations that would indicate programmer error — calling `Get` outside a `Read` scope, registering a component after the world is in use, violating exclusive-access ownership — panic. Operations that represent valid runtime states return `(value, bool)` pairs: `Get[T](r, e)` returns `(T, false)` when the component is absent; `Lookup(path)` returns `(0, false)` on a miss.

This matches Go standard-library conventions (`map` lookup, type assertion with `ok`) and keeps hot paths free of error-return boilerplate.

### How does the Reader/Writer model compare to mutex locking?

`world.Read(func(*Reader))` takes a read-lock and gives you a `*Reader` for concurrent-safe inspection. `world.Write(func(*Writer))` takes an exclusive write lock and gives you a `*Writer` that buffers all mutations in a deferred command queue, flushed atomically on scope exit.

Compared to a raw `sync.RWMutex`:
- You cannot accidentally skip the unlock — the scope callback always exits cleanly.
- Mutations inside a `Write` scope that would normally race are deferred; there is no window where another goroutine observes a half-written entity.
- The exclusive-access ownership assertion (`ExclusiveAccessBegin`) catches bugs at development time before they become production races.

See [Manual](Manual.md) for the full concurrency model.

### Can I use Go flecs with goroutines safely?

Yes, with the Reader/Writer scope model. `world.Read` is goroutine-safe and may be called concurrently from multiple goroutines; `world.Write` serialises exclusive mutation via an internal lock. For tight parallel iteration, `System.SetMultiThreaded(true)` + `world.SetWorkerCount(n)` splits a single system's iteration across N worker goroutines with per-worker deferred queues.

Do not share a `*Reader` or `*Writer` across goroutines — they are bound to the calling goroutine for the lifetime of the callback.

### Why are there no modules?

The upstream C flecs module system (`ECS_MODULE`, `world.import`) exists because C lacks a native package system. In Go, packages are the module system: put your components and system-registration logic in a Go package and import it. This keeps the API surface small and lets Go tooling (`go mod`, `go test`, `go doc`) work without a parallel flecs-specific layer.

### Why does Go flecs use `log/slog` instead of a custom logger?

`log/slog` is the standard structured-logging interface since Go 1.21. Applications already control the default slog handler; wiring Go flecs into the same sink requires one call: `world.SetLogger(slog.Default())`. A custom logger interface would force every adopter to write an adapter.

---

## Performance

### Why are queries slow?

The most common cause is creating a `*CachedQuery` (or `*Query`) inside a per-frame callback or system body. Queries are fast to iterate but expensive to build: they index the world's archetype graph at construction time.

```go
// GOOD — build the query once and reuse it across every frame
q := flecs.NewCachedQuery(w, posID, velID)
flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
    for it.Next() {
        // ...
    }
})
```

```go
// BAD — allocates a fresh *Query on every Progress call
ticker := 0
qMain := flecs.NewCachedQuery(w, posID) // dummy system driver
flecs.NewSystem(w, qMain, func(dt float32, _ *flecs.QueryIter) {
    // creating a new query inside the callback is paid every frame
    scratch := flecs.NewQuery(w, posID, velID)
    it := scratch.Iter()
    for it.Next() { /* ... */ }
    ticker++
})
```

Build `*CachedQuery` values during initialisation and keep them alive for the world's lifetime. Use `*Query` (uncached) only for one-shot lookups outside the main loop.

### Why does my system run multiple times per frame?

A system body is called once per matching archetype, not once per frame. This gives the system direct access to contiguous component arrays — the inner loop is a tight slice walk with no virtual dispatch.

If you need logic that runs exactly once per frame, register a separate system with an empty query, or use the `run` pattern with a pre-cached query.

### Why does query memory grow over time?

Cached queries register themselves with the world and are not garbage-collected while the world lives. If queries are created repeatedly without being closed, memory grows. If you need a short-lived query, use `*Query` (uncached) instead of `*CachedQuery`.

---

## Components

### What is the difference between `AddID` and `Set`?

`AddID` records that an entity possesses a component without writing any data. For zero-size tag types and relationship pairs this is the correct operation. For components that have a value, `AddID` leaves the storage uninitialised; `Set` writes the value and also triggers `OnSet` observers.

Both operations ensure the entity has the component afterwards and are idempotent — calling either twice is safe.

```go
type Poisoned struct{} // tag, no data

w.Write(func(fw *flecs.Writer) {
    e := fw.NewEntity()
    fw.AddID(e, poisonedID)          // tag: AddID is correct
    flecs.Set(fw, e, Position{1, 2}) // data: Set writes the value
})
```

### Why are my entity IDs so large?

When an entity is deleted its ID slot is recycled. The upper 32 bits of the 64-bit `ID` store a generation counter that increments on each recycle, so a new entity that reuses the same slot gets a numerically large ID even though the lower 32 bits are small. `IsAlive` checks both the slot and the generation, so a stale ID from before the deletion reliably returns `false`.

```go
var e1, e2 flecs.ID
w.Write(func(fw *flecs.Writer) {
    e1 = fw.NewEntity() // small id
})
w.Delete(e1) // slot available for recycling

w.Write(func(fw *flecs.Writer) {
    e2 = fw.NewEntity() // reuses e1's slot; upper 32 bits incremented
})

w.Read(func(r *flecs.Reader) {
    _ = r.IsAlive(e1) // false
    _ = r.IsAlive(e2) // true
})
```

### Can I use arbitrary Go types as components?

Any Go value type (`struct`, `int`, `[N]byte`, etc.) registered with `RegisterComponent[T](w)` becomes a component. Pointer-typed fields are allowed but discouraged on hot paths — the component data lives in archetype column arrays and pointer fields defeat cache locality.

---

## Relationships

### Are relationships just a component with an entity handle?

No. Relationships are first-class identifiers encoded directly in the archetype signature. A pair `(ChildOf, parent)` is part of the entity's archetype key: queries and observers match on the relationship without scanning every entity. Storage layout, cascade delete, and traversal modifiers (`.Up`, `.Cascade`) all operate on the archetype graph directly.

A component that stores an entity ID by value is a user-defined pointer — it carries no structural meaning to the query engine. For more detail see [Relationships](Relationships.md).

---

## Hierarchies and Names

### Why does the lookup function not find my entity?

`world.Lookup` resolves a dot-separated path from the root. If the entity has a parent you must provide the full path:

```go
var parent, child flecs.ID
w.Write(func(fw *flecs.Writer) {
    parent = fw.NewEntity()
    fw.SetName(parent, "Galaxy")
    child = fw.NewEntity()
    fw.SetName(child, "Sol")
    fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))
})

w.Read(func(r *flecs.Reader) {
    // bare name lookup fails for non-root entities
    _, ok := r.Lookup("Sol")       // ok == false
    id, ok2 := r.Lookup("Galaxy.Sol") // ok2 == true
    _ = id
    _ = ok2
})
```

To find a child without knowing the full path, use `r.LookupChild(parentID, "Sol")`.

---

## Systems and Observers

### Can I create systems outside of `main`?

Yes. `flecs.NewSystem` and `flecs.NewSystemInPhase` can be called from any function — an init helper, a package-level `Register` function, or a constructor — as long as the `*World` is available and no `Progress` has yet started.

### Can I use my own scheduler?

Yes. The four-phase pipeline (`PreUpdate → OnFixedUpdate → OnUpdate → PostUpdate`) is optional. You can drive systems manually by holding the `*CachedQuery` and calling your own iteration logic outside `Progress`. The deferred command queue still works; call `world.Write` as the mutation scope.

### Can I use Go flecs without systems?

Yes. `flecs.NewSystem` is optional. Many applications use only the entity/component storage and cached queries, polling them from a hand-written game loop.

### Can I add or remove components from within a system?

Yes. All mutations inside a system body (via `it.Writer()`) are deferred: they are added to a per-frame command queue and applied after the current system finishes. You do not need a separate command API.

```go
flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
    fw := it.Writer()
    for it.Next() {
        entities := it.Entities()
        for _, e := range entities {
            flecs.Set(fw, e, Velocity{DX: 1}) // deferred, applied after this system
        }
    }
})
```

### How do I detect which entities have changed?

Register an `OnSet` observer for the component you care about:

```go
flecs.Observe[Position](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, p Position) {
    // called whenever Position is written on any entity
})
```

For query-level change detection, `(*CachedQuery).Changed()` returns `true` if any matching table was written since the last call. It over-reports and never under-reports.

---

## REST Handler

### Why does the REST handler not return data?

Make sure:
1. The handler is registered with an `*http.ServeMux` and the server is listening.
2. You are calling `world.Progress` or otherwise advancing the world so systems and observers fire.
3. The world has at least one entity/component registered.

See [FlecsRemoteApi](FlecsRemoteApi.md) for the full setup guide.

### Does the REST handler send my data externally?

No. `flecs.NewRESTHandler` returns a standard `http.Handler`; it only serves requests to whatever address your `*http.Server` binds to. No data is transmitted to any third party.

---

## Errors and Panics

### What does a concurrent-mutation panic mean?

If you mutate the world from two goroutines without using `world.Write` (or inside a system without `it.Writer()`), the exclusive-access check fires a panic:

```
flecs: exclusive access violation: goroutine N attempted mutation while goroutine M holds the world
```

Wrap all mutations in `world.Write(func(*flecs.Writer) { ... })`. For mutations inside a system, use `it.Writer()` — the deferred queue is safe from any goroutine within the same `Progress` call.

See [Manual](Manual.md) for the concurrency model.

### What does "component registered after first use" mean?

`RegisterComponent[T](w)` must be called before any entity is created with component `T`. Calling it after the first entity mutation panics with a message that names the type. Register all components during world initialisation, before the first `Progress` or `Write` that uses them.
