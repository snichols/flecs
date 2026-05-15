# Observers

This manual covers the event-driven reaction system in Go flecs: hooks (single-subscriber, Phase 5.1) and observers (multi-subscriber, Phase 5.2).

See the [Quickstart](Quickstart.md) for a hands-on introduction. Cross-links: [EntitiesComponents.md](EntitiesComponents.md) (hooks), [Systems.md](Systems.md) (callbacks vs. observers), [Queries.md](Queries.md) (query terms).

---

## Introduction

Observers are a mechanism for reacting to events — structural changes in the ECS such as components being added, set, or removed. They complement systems: a system runs on every frame for all matching entities, whereas an observer fires whenever a matching event occurs.

Go flecs provides two distinct layers:

- **Hooks** — one callback per (component, event) pair. Part of the component's interface, like a constructor or destructor. Invoked synchronously before observers. Fastest path.
- **Observers** — unlimited callbacks per (component, event). Registered and cancelled at runtime by any part of the application. More flexible; slightly higher overhead.

---

## Hooks

Hooks are registered once per component type per event on the world. They fire synchronously on every matching operation, always before any observers registered for the same event.

### OnAdd Hook

`OnAdd[T]` fires the first time `T` is added to an entity. It does **not** re-fire on subsequent `Set[T]` calls that overwrite an existing value.

```go
type Position struct{ X, Y float32 }

w := flecs.New()

flecs.OnAdd[Position](w, func(fw *flecs.Writer, e flecs.ID, v Position) {
    // v is the zero value of Position — OnAdd fires before the first Set.
    fmt.Printf("Position added to entity %d\n", e)
})

var e flecs.ID
w.Write(func(fw *flecs.Writer) {
    e = fw.NewEntity()
    flecs.Set(fw, e, Position{X: 10, Y: 20}) // OnAdd fires here
    flecs.Set(fw, e, Position{X: 30, Y: 40}) // OnAdd does NOT fire again
})
```

The value `v` at OnAdd time is the zero value of `T` because `OnAdd` fires when the slot is first allocated, before `Set` writes the value. If you need the written value, use `OnSet`.

### OnSet Hook

`OnSet[T]` fires every time `T`'s value is written via `Set[T]` or `SetPair[T]`, including the initial write that follows `OnAdd`.

```go
type Position struct{ X, Y float32 }

w := flecs.New()

flecs.OnSet[Position](w, func(fw *flecs.Writer, e flecs.ID, v Position) {
    // v is the post-Set value.
    fmt.Printf("Position set to {%.1f, %.1f}\n", v.X, v.Y)
})

w.Write(func(fw *flecs.Writer) {
    e := fw.NewEntity()
    flecs.Set(fw, e, Position{X: 10, Y: 20}) // OnSet fires: {10, 20}
    flecs.Set(fw, e, Position{X: 30, Y: 40}) // OnSet fires: {30, 40}
})
```

`OnSet` does **not** fire for `fw.AddID` (which carries no value). For zero-size tags, the callback receives the zero value of `T`.

### OnReplace Hook

`OnReplace[T]` fires when `Set[T]` or `SetPair[T]` overwrites a component value that **already exists** on the entity. It does **not** fire on the first `Set` (which triggers `OnAdd` then `OnSet` instead).

The callback receives both the **previous** (`old`) and **incoming** (`new`) values, by value, before the slot is overwritten. This enables diff-style logic — delta detection, change-event publishing, undo stacks — that is awkward to express with `OnSet` alone.

```go
type Position struct{ X, Y float32 }

w := flecs.New()

flecs.OnReplace[Position](w, func(fw *flecs.Writer, e flecs.ID, old, new Position) {
    fmt.Printf("Position changed: {%.1f,%.1f} → {%.1f,%.1f}\n", old.X, old.Y, new.X, new.Y)
})

var e flecs.ID
w.Write(func(fw *flecs.Writer) {
    e = fw.NewEntity()
    flecs.Set(fw, e, Position{X: 1, Y: 2}) // first Set: OnReplace does NOT fire
})

w.Write(func(fw *flecs.Writer) {
    flecs.Set(fw, e, Position{X: 3, Y: 4}) // overwrite: OnReplace fires with old={1,2}, new={3,4}
})
```

**Dispatch order on overwrite**: `OnReplace` → column write → `OnSet`. `OnSet` still fires after `OnReplace` on every overwrite.

**First Set is not a replace**: `OnReplace` only fires when the slot already held a user-set value. Removing and re-adding a component resets this: the first `Set` after `Remove` is treated as a first add.

**Pairs**: `OnReplace` fires on `SetPair[T]` overwrites of an existing pair slot, keyed by the pair data type `T`.

**Single hook per type**: calling `OnReplace[T]` twice replaces the prior hook. Pass `nil` to clear.

```go
// Replace
flecs.OnReplace[Position](w, newCallback)

// Clear
flecs.OnReplace[Position](w, nil)
```

**Untyped variant** (`OnReplaceID`): for runtime-registered components, use the ID-keyed API. The handler receives raw `unsafe.Pointer` values; both pointers are valid only for the duration of the call.

```go
posID := flecs.RegisterComponent[Position](w)
flecs.OnReplaceID(w, posID, func(fw *flecs.Writer, e flecs.ID, oldPtr, newPtr unsafe.Pointer) {
    old := *(*Position)(oldPtr)
    new := *(*Position)(newPtr)
    fmt.Printf("changed: %v → %v\n", old, new)
})
```

**Divergence from C flecs**: C's `on_replace` hook prevents `get_mut`/`ensure`/`emplace` (mutable-pointer operations) on the same component. Go flecs has never exposed those APIs, so this restriction does not apply.

**No `EventOnReplace` observer event**: `OnReplace` is a per-component hook, not an observer event. There is no `EventOnReplace` constant; use `Observe[T](w, EventOnSet, ...)` if you want observer-style subscription.

### OnRemove Hook

`OnRemove[T]` fires before `T` is removed from an entity, including when the entity is deleted. The value `v` passed to the callback is the component's value at the time of the call — the data is still valid.

```go
type Position struct{ X, Y float32 }

w := flecs.New()

flecs.OnRemove[Position](w, func(fw *flecs.Writer, e flecs.ID, v Position) {
    // v is the pre-remove value.
    fmt.Printf("Position {%.1f, %.1f} removed from entity %d\n", v.X, v.Y, e)
})

var e flecs.ID
w.Write(func(fw *flecs.Writer) {
    e = fw.NewEntity()
    flecs.Set(fw, e, Position{X: 10, Y: 20})
})

w.Write(func(fw *flecs.Writer) {
    flecs.Remove[Position](fw, e) // OnRemove fires: {10, 20}
})
```

### Hook Ordering

Hook and observer dispatch order for a single event:

1. The hook (`OnAdd`/`OnSet`/`OnRemove`) — if registered.
2. Observers — in registration order.

For `OnRemove`, observers fire **before** the hook (the hook is last, analogous to a destructor running after other cleanup).

```go
type Tag struct{}

w := flecs.New()
var order []string

flecs.OnAdd[Tag](w, func(fw *flecs.Writer, e flecs.ID, _ Tag) {
    order = append(order, "hook")
})
flecs.Observe[Tag](w, flecs.EventOnAdd, func(fw *flecs.Writer, e flecs.ID, _ Tag) {
    order = append(order, "observer")
})

w.Write(func(fw *flecs.Writer) {
    e := fw.NewEntity()
    fw.AddID(e, flecs.RegisterComponent[Tag](w))
    // order: ["hook", "observer"]
})
```

### Replacing and Clearing Hooks

Calling `OnAdd[T]`, `OnSet[T]`, or `OnRemove[T]` a second time replaces the prior hook. Passing `nil` clears it:

```go
// Replace
flecs.OnSet[Position](w, newCallback)

// Clear
flecs.OnSet[Position](w, nil)
```

Only one hook per (type, event) is allowed. This mirrors the semantics of constructors and destructors in OOP.

### The *Writer Parameter

Every hook and observer callback receives a `*flecs.Writer` as its first argument. Read operations (`Get`, `Has`, `IsAlive`) are explicitly safe to call from within a callback:

```go
flecs.OnAdd[Position](w, func(fw *flecs.Writer, e flecs.ID, v Position) {
    // Safe to read from within the callback.
    if _, ok := flecs.Get[Position](fw, e); ok {
        fmt.Println("Position is present")
    }
    _ = fw.IsAlive(e) // also safe
})
```

The `*Writer` is valid only for the duration of the callback.

---

## Hooks vs. Observers

| | Hooks | Observers |
|---|---|---|
| Subscribers per (component, event) | **One** | Unlimited |
| Components matched | Single | Single (Go flecs) |
| Performance | Fastest | Slightly higher overhead |
| Dynamically add / remove | No (replace only) | Yes — `Unsubscribe()` |
| Intended use | Component interface (constructor / destructor analogue) | Cross-cutting reactions from other subsystems |
| Mutate component in callback | Yes | Prefer not to |

Use hooks when you own the component type and want lifecycle behavior baked in (e.g., initialise a render resource when `Mesh` is added, free it on remove). Use observers when another subsystem needs to react to someone else's component change (e.g., an indexing system reacting to `Position` changes).

---

## Observers

Observers provide a multi-subscriber event delivery mechanism. Any number of observers can subscribe to the same (component, event) pair, and each can be unsubscribed independently at runtime.

### Subscribing with Observe

`Observe[T]` registers a typed callback for a single component on a single event:

```go
type Position struct{ X, Y float32 }

w := flecs.New()

obs := flecs.Observe[Position](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v Position) {
    fmt.Printf("Position set: {%.1f, %.1f}\n", v.X, v.Y)
})

w.Write(func(fw *flecs.Writer) {
    e := fw.NewEntity()
    flecs.Set(fw, e, Position{X: 10, Y: 20}) // observer fires
    flecs.Set(fw, e, Position{X: 30, Y: 40}) // observer fires again
})
```

`Observe[T]` returns an `*Observer` handle. Hold onto it — you'll need it to unsubscribe.

### OnAdd Observers

```go
obs := flecs.Observe[Position](w, flecs.EventOnAdd, func(fw *flecs.Writer, e flecs.ID, v Position) {
    // Fires once when Position is first added. v is zero (slot not yet written).
    fmt.Printf("Position added to entity %d\n", e)
})

w.Write(func(fw *flecs.Writer) {
    e := fw.NewEntity()
    flecs.Set(fw, e, Position{X: 10, Y: 20}) // OnAdd fires (first add)
    flecs.Set(fw, e, Position{X: 30, Y: 40}) // OnAdd does NOT fire (already present)
})
```

### OnRemove Observers

```go
obs := flecs.Observe[Position](w, flecs.EventOnRemove, func(fw *flecs.Writer, e flecs.ID, v Position) {
    // Fires before Position is removed. v is the pre-remove value.
    fmt.Printf("Position removed: {%.1f, %.1f}\n", v.X, v.Y)
})

w.Write(func(fw *flecs.Writer) {
    e := fw.NewEntity()
    flecs.Set(fw, e, Position{X: 10, Y: 20})
    flecs.Remove[Position](fw, e) // OnRemove fires: {10, 20}
    flecs.Remove[Position](fw, e) // does NOT fire — entity no longer has Position
})
```

### OnDelete and OnDeleteTarget Observer Events

✅ **Shipped in v0.103.0.** Go flecs provides `EventOnDelete` and `EventOnDeleteTarget` as first-class observer event entities. See [OnDelete and OnDeleteTarget Events](#on-delete-and-on-delete-target) for the full API, dispatch ordering, and examples.

Cleanup policies (what to do when a component entity or relationship target is deleted) are a separate concern; see [ComponentTraits.md § Cleanup traits (OnDelete / OnDeleteTarget)](ComponentTraits.md#cleanup-traits-ondelete--ondeletetarget).

### Multiple Subscribers

Multiple observers can subscribe to the same event. They fire in registration order, after the hook:

```go
obs1 := flecs.Observe[Position](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v Position) {
    fmt.Println("observer 1")
})
obs2 := flecs.Observe[Position](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v Position) {
    fmt.Println("observer 2")
})

// Both fire when Position is set: "observer 1", then "observer 2".
```

### Unsubscribing

Call `Unsubscribe()` on the `*Observer` handle to stop receiving events. The call is idempotent.

```go
obs := flecs.Observe[Position](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v Position) {
    fmt.Println("observer fired")
})

w.Write(func(fw *flecs.Writer) {
    e := fw.NewEntity()
    flecs.Set(fw, e, Position{X: 10, Y: 20}) // fires
})

obs.Unsubscribe() // stop receiving

w.Write(func(fw *flecs.Writer) {
    e := fw.NewEntity()
    flecs.Set(fw, e, Position{X: 10, Y: 20}) // does NOT fire
})
```

Unsubscribing during a callback takes effect immediately: observers not yet visited in the current dispatch are skipped; observers that already fired in this event are unaffected.

```go
var obs *flecs.Observer
obs = flecs.Observe[Position](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v Position) {
    obs.Unsubscribe() // safe to call from within callback; fires once only
})
```

### Raw-ID Observer (ObserveID)

`ObserveID` subscribes to any entity ID — including tag IDs and pair IDs — without type parameters:

```go
tagID := flecs.RegisterComponent[MyTag](w)

obs := flecs.ObserveID(w, tagID, flecs.EventOnAdd, func(fw *flecs.Writer, e flecs.ID, ptr unsafe.Pointer) {
    fmt.Printf("tag added to entity %d\n", e)
})
```

Use `ObserveID` when you only know the component ID at runtime, or when subscribing to tags that have no associated data.

### Multi-Event Observer (Observe2)

`Observe2[T]` registers a single callback for multiple events. The callback receives the `EventKind` that triggered it:

```go
obs := flecs.Observe2[Position](w,
    []flecs.EventKind{flecs.EventOnAdd, flecs.EventOnRemove},
    func(fw *flecs.Writer, event flecs.EventKind, e flecs.ID, v Position) {
        switch event {
        case flecs.EventOnAdd:
            fmt.Printf("Position added to entity %d\n", e)
        case flecs.EventOnRemove:
            fmt.Printf("Position removed from entity %d: {%.1f, %.1f}\n", e, v.X, v.Y)
        }
    },
)
```

`Observe2` returns a single `*Observer` handle. `Unsubscribe()` cancels all subscriptions held by that handle.

---

## Disabling an Observer {#disabling-an-observer}

An observer can be paused without removing it using `SetEnabled(false)`. A disabled observer is silently skipped in the dispatch path but remains registered; it can be re-enabled at any time with `SetEnabled(true)`. This mirrors system disabling (see [Systems.md § Disabling a System](Systems.md#disabling-a-system)).

```go
type Position struct{ X, Y float32 }

w := flecs.New()

var obs *flecs.Observer
w.Write(func(fw *flecs.Writer) {
    obs = flecs.Observe[Position](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v Position) {
        fmt.Printf("Position set: {%.1f, %.1f}\n", v.X, v.Y)
    })
    e := fw.NewEntity()
    flecs.Set(fw, e, Position{X: 1, Y: 2}) // fires
})

obs.SetEnabled(false)
w.Write(func(fw *flecs.Writer) {
    e := fw.NewEntity()
    flecs.Set(fw, e, Position{X: 3, Y: 4}) // does NOT fire — observer is disabled
})

obs.SetEnabled(true)
w.Write(func(fw *flecs.Writer) {
    e := fw.NewEntity()
    flecs.Set(fw, e, Position{X: 5, Y: 6}) // fires again
})
```

`IsEnabled()` returns the current state:

```go
obs.SetEnabled(false)
fmt.Println(obs.IsEnabled()) // false
obs.SetEnabled(true)
fmt.Println(obs.IsEnabled()) // true
```

**Key properties:**

- Default is enabled (`true`). All constructors (`Observe[T]`, `ObserveID`, `Observe2[T]`, `ObserveWithOptions[T]`) create enabled observers.
- Multiple observers on the same event are independent; disabling one does not affect others.
- Toggling mid-dispatch is safe: a callback that disables a later observer in the same dispatch suppresses it for that dispatch. Observers already fired in the current event are unaffected.
- `SetEnabled` / `IsEnabled` are plain field access, not atomic. Intended for serial use outside an active dispatch; the world's exclusive-access model ensures no concurrent observer dispatch.
- Unlike `Unsubscribe`, `SetEnabled(false)` is fully reversible.

---

## yield_existing {#yield-existing}

Registering a `WithYieldExisting()` observer retroactively fires the observer's callback for every entity that already matches the component at registration time. This is the canonical "catch up to existing state" mechanism from upstream C flecs (`yield_existing` field on `ecs_observer_desc_t`).

Use `ObserveWithOptions[T]` with `WithYieldExisting()`:

```go
type Position struct{ X, Y float32 }

w := flecs.New()

// Populate the world before the observer is registered.
w.Write(func(fw *flecs.Writer) {
    for i := 0; i < 100; i++ {
        e := fw.NewEntity()
        flecs.Set(fw, e, Position{X: float32(i), Y: 0})
    }
})

// Register an OnAdd observer with yield_existing.
// The sweep fires immediately for all 100 existing entities.
var obs *flecs.Observer
w.Write(func(fw *flecs.Writer) {
    obs = flecs.ObserveWithOptions[Position](w,
        flecs.WithYieldExisting(),
        []flecs.EventKind{flecs.EventOnAdd},
        func(fw *flecs.Writer, ev flecs.EventKind, e flecs.ID, v Position) {
            fmt.Printf("entity %d has Position {%.1f, %.1f}\n", e, v.X, v.Y)
        },
    )
    // All 100 invocations complete before ObserveWithOptions returns (synchronous).
})
```

**Supported events:** OnAdd and OnSet. The sweep fires one invocation per entity per event kind; a registration with `[]EventKind{EventOnAdd, EventOnSet}` produces 2×N total invocations for N existing entities. OnRemove events are skipped silently in the sweep; if the events list contains only OnRemove, `ObserveWithOptions` panics with a clear message.

**Exclusion semantics:** The sweep skips tables carrying the `Disabled` or `Prefab` tag, matching ordinary query-exclusion semantics (Phase 16.2). The sweep targets **only** the newly-registered observer; peer observers already subscribed to the same event are not re-fired.

**Sweep is synchronous.** `ObserveWithOptions` returns only after all matching entities are visited.

**Iteration order:** archetype-table order (the order tables were registered in the component index). This matches normal query iteration order.

**Multi-event registration:**

```go
obs = flecs.ObserveWithOptions[Position](w,
    flecs.WithYieldExisting(),
    []flecs.EventKind{flecs.EventOnAdd, flecs.EventOnSet},
    func(fw *flecs.Writer, ev flecs.EventKind, e flecs.ID, v Position) {
        // Called twice per existing entity: once with EventOnAdd, once with EventOnSet.
    },
)
```

**Notes:**

- If the observer is disabled (`SetEnabled(false)`) at the time `ObserveWithOptions` would run the sweep, the sweep is suppressed. This is reachable only via concurrent goroutine patterns that violate the world's exclusive-access model; in normal use the observer is always enabled at construction time.
- For OnRemove-only registrations with `WithYieldExisting()`, the function panics immediately (upstream C only yields OnRemove on observer-deletion, which is out of scope).

---

## OnTableCreate {#on-table-create}

`OnTableCreate` fires once per archetype table when that table is **first created** — that is, the first time any entity migrates into a previously-unseen component signature. It is a table-level event, not a component-level event.

Unlike `Observe[T]` / `OnAdd[T]` / `OnSet[T]`, `OnTableCreate` has **no type parameter**: it fires for every new archetype regardless of which components it contains. The handler reads the table's full signature via `t.Type()` and the current row count via `t.Count()`. Mutations to the world must go through `fw` (they are deferred).

```go
type Position struct{ X, Y float32 }
type Velocity struct{ X, Y float32 }

w := flecs.New()

w.Write(func(fw *flecs.Writer) {
    obs := flecs.OnTableCreate(w, func(fw *flecs.Writer, t *flecs.Table) {
        fmt.Printf("new archetype: %v\n", t.Type())
    })
    _ = obs
})

w.Write(func(fw *flecs.Writer) {
    e1 := fw.NewEntity()
    flecs.Set(fw, e1, Position{1, 2})          // new archetype [Position] → fires once
    e2 := fw.NewEntity()
    flecs.Set(fw, e2, Position{3, 4})          // same archetype → does NOT fire again
    e3 := fw.NewEntity()
    flecs.Set(fw, e3, Position{})
    flecs.Set(fw, e3, Velocity{})              // new archetype [Position, Velocity] → fires once
})
```

### OnTableCreate vs. component observers

| | `Observe[T]` / `OnAdd[T]` | `OnTableCreate` |
|---|---|---|
| Scope | one component type `T` | all archetypes |
| Type parameter | yes (`[T]`) | no |
| Fires per | entity | archetype table |
| Re-fires | every matching entity | never (once per table) |
| `t.Type()` | N/A | full component-ID list |

### Empty root table

`OnTableCreate` does **not** fire for the world's initial empty table (the table that newly created entities occupy before any component is added). This matches upstream's `is_root` suppression in `table.c:1278` and avoids spurious callbacks during world construction.

### yield_existing with OnTableCreate

Use `OnTableCreateWithOptions` with `WithYieldExisting()` to retroactively receive one callback per table that already existed at registration time:

```go
w.Write(func(fw *flecs.Writer) {
    e := fw.NewEntity()
    flecs.Set(fw, e, Position{1, 2}) // [Position] table already exists
})

w.Write(func(fw *flecs.Writer) {
    // Fires once immediately for the existing [Position] table.
    flecs.OnTableCreateWithOptions(w, flecs.WithYieldExisting(),
        func(fw *flecs.Writer, t *flecs.Table) {
            fmt.Printf("existing table: %v\n", t.Type())
        })
})
```

The yield sweep fires synchronously before `OnTableCreateWithOptions` returns. Iteration order is sorted-signature order (deterministic within a single run). The empty root table is excluded from the sweep.

### Re-entry

If the handler creates entities (via `fw`) whose component signatures form a new archetype, those commands are deferred and flushed after the outer `Write` scope closes. The resulting new table fires `OnTableCreate` as part of that next flush. This is the same deferred-coalescer path used by `OnAdd`/`OnSet` handlers.

For world-level hooks that fire at every merge boundary regardless of component, see [Systems.md § Merge hooks](Systems.md#merge-hooks).

### Table type

The `*flecs.Table` pointer passed to the handler is the same type exposed by `w.TablesFor(componentID)` — it is the `table.Table` type aliased at `world.go:23`. Fields are accessible via the public methods `t.Type() []flecs.ID` and `t.Count() int`.

`OnTableDelete` fires when a table is about to be reclaimed by the table-reclamation sweep. See [OnTableDelete](#on-table-delete) below.

---

## Custom Events {#custom-events}

**Events are entities.** In upstream C flecs — and in Go flecs since v0.63.0 — an event is just an entity used as a dispatch key. The four built-in events (`OnAdd`, `OnSet`, `OnRemove`, `OnTableCreate`) are pre-allocated built-in entities accessible via `w.EventOnAdd()`, `w.EventOnSet()`, `w.EventOnRemove()`, `w.EventOnTableCreate()`. A custom event is a user-allocated entity that plays the same role.

### Registering a custom event

```go
w := flecs.New()

var playerDied flecs.ID
w.Write(func(fw *flecs.Writer) {
    playerDied = flecs.RegisterEvent(fw, "PlayerDied")
})
// playerDied is a regular entity tagged with the built-in Event tag.
// HasID(playerDied, w.Event()) == true
```

`RegisterEvent` allocates a new entity, optionally names it, and applies the built-in `Event` tag so you can distinguish event entities from ordinary entities:

```go
w.Read(func(r *flecs.Reader) {
    fmt.Println(flecs.HasID(r, playerDied, w.Event())) // true
})
```

### Subscribing to a custom event

```go
type DeathPayload struct {
    Killer flecs.ID
    Weapon string
}

flecs.ObserveEventTyped[DeathPayload](w, playerDied, func(fw *flecs.Writer, e flecs.ID, d DeathPayload) {
    fmt.Printf("entity %v died — killed by %v with %q\n", e, d.Killer, d.Weapon)
})
```

For dynamically typed payloads use the untyped form:

```go
flecs.ObserveEvent(w, playerDied, func(fw *flecs.Writer, e flecs.ID, payload interface{}) {
    if d, ok := payload.(DeathPayload); ok {
        fmt.Println("weapon:", d.Weapon)
    }
})
```

Both return an `*Observer` handle — call `Unsubscribe()` to cancel. `SetEnabled(false)` pauses without removing (same as component observers).

### Emitting a custom event

```go
w.Write(func(fw *flecs.Writer) {
    target := fw.NewEntity()
    flecs.EmitTyped(fw, playerDied, target, DeathPayload{
        Killer: someEntity,
        Weapon: "sword",
    })
})
```

`Emit` (and `EmitTyped`) dispatch is **synchronous**: all subscribed observers fire in registration order before `Emit` returns. Re-entrant emit — calling `Emit` from within an observer callback — is safe and also fires synchronously.

For the untyped form:

```go
flecs.Emit(fw, playerDied, target, DeathPayload{...})
```

### Payload semantics

The payload is **shallow-copied** at the `Emit` call site: each observer receives its own copy of the `interface{}` value. Mutations to the interface value itself do not leak to subsequent observers. Mutations to a pointed-to struct inside the payload are visible to all observers (it is a shallow, not deep, copy).

### Built-in event entities

The four built-in events are accessible as regular entity IDs:

| Accessor | Fires when |
|---|---|
| `w.EventOnAdd()` | A component/tag is added to an entity |
| `w.EventOnSet()` | A component value is written |
| `w.EventOnRemove()` | A component/tag is removed from an entity |
| `w.EventOnTableCreate()` | A new archetype table is created |

These entity IDs can be used with `Emit` and `ObserveEvent` like any custom event entity. Note: component-based observers registered via `Observe[T]` use a dispatch key of `{componentID, eventEntityID}`, so `ObserveEvent(w, w.EventOnAdd(), fn)` subscribes to explicit `Emit(fw, w.EventOnAdd(), ...)` calls, **not** to all component OnAdd events.

### Deleting a custom event entity

Deleting a custom event entity automatically removes its observer registrations. Any subsequent `Emit` for the deleted event entity is a no-op.

```go
w.Write(func(fw *flecs.Writer) {
    fw.Delete(playerDied)
    // Emit(fw, playerDied, e, payload) is now a no-op.
})
```

### yield_existing on custom events

`ObserveEvent` does not accept `ObserverOptions` directly. Custom events have no "currently matching" concept, so `WithYieldExisting()` is intentionally not wired to the custom-event path — there is nothing to sweep at registration time. Observers registered for a custom event will only fire for future `Emit` calls.

---

## Propagation along IsA {#propagation-along-isa}

When a component is mutated on a prefab entity, Go flecs automatically fires the same observer event for every transitive inheritor — entities that inherit the prefab via `(IsA, prefab)`, directly or transitively. This mirrors the upstream C flecs `observable.c:1083` behaviour.

### How it works

After the local observer dispatch for the source entity, `propagateEvent` performs a **breadth-first search** (BFS) over the `(IsA, *)` relationship graph starting from the source:

```
P (prefab, Position set here)
├── A  (IsA, P)     → also receives OnSet(Position)
│   └── B  (IsA, A) → also receives OnSet(Position)
└── C  (IsA, P)     → also receives OnSet(Position)
```

Two gates suppress propagation for individual inheritors:

| Gate | Behaviour |
|------|-----------|
| **DontInherit** | If the component is marked `DontInherit`, propagation is skipped entirely — inheritors do not receive the event for that component. |
| **Override** | If an inheritor owns its own copy of the component locally (i.e., the component appears in its own archetype table), propagation is skipped for that specific inheritor only. All other inheritors still receive the event. |

### Supported event kinds

Propagation is wired to all four built-in mutation paths:

- **`OnAdd`** — when a component is added to the prefab
- **`OnSet`** — when a component is set/updated on the prefab
- **`OnRemove`** — when a component is removed from the prefab
- **`OnReplace`** hook — called on the inheritor with the same old/new pointers as the prefab

Custom events fired via `Emit` also propagate when a non-zero entity is passed:

```go
// Custom event fires for P and all transitive inheritors of P.
w.Write(func(fw *flecs.Writer) {
    flecs.Emit(fw, myEvent, P, payload)
})
```

### Multi-term observers

Multi-term observers (registered via `ObserveQuery` / `ObserveQueryID` / `ObserveQueryWithOptions`) re-evaluate their filter per inheritor at dispatch time. The trigger component term is treated as automatically satisfied (the inheritor "has" it via IsA even though it does not own a local copy), while all other filter terms are evaluated against the inheritor's own archetype:

```go
// Observer fires for inheritors that own Velocity locally.
flecs.ObserveQuery(w, flecs.EventOnSet,
    []flecs.Term{
        flecs.With(posID),
        flecs.With(velID),
    },
    func(_ *flecs.Writer, e flecs.ID, _ unsafe.Pointer) { /* … */ },
)
```

### BFS cache

The transitive inheritor list is computed once per prefab and cached. The cache is **invalidated entirely** whenever any `(IsA, *)` pair is added or removed from any entity — clearing all entries is necessary because adding `(IsA, B)` to `C` also stales the cache for any ancestor of `B`. Invalidation is O(1) (sets the cache map to nil).

### Example

```go
w := flecs.New()

type Hp struct{ Value int }
hpID := flecs.RegisterComponent[Hp](w)

var p, inst flecs.ID
w.Write(func(fw *flecs.Writer) {
    p = fw.NewEntity()
    inst = fw.NewEntity()
    flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), p))
})

// Observer fires for p (direct) and inst (propagated).
flecs.Observe[Hp](w, flecs.EventOnSet, func(_ *flecs.Writer, e flecs.ID, v Hp) {
    fmt.Printf("hp set on %v: %v\n", e, v)
})

w.Write(func(fw *flecs.Writer) {
    flecs.Set[Hp](fw, p, Hp{100}) // prints twice: for p and for inst
})
```

---

## Deferred Execution

Observers fire synchronously when the triggering operation is executed. When operations are inside a `Write` scope (which is always the case in Go flecs), the mutations are queued in the deferred command queue and the observers fire when the queue is flushed:

```go
flecs.Observe[Position](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v Position) {
    fmt.Println("observer fired for:", v)
})

w.Write(func(fw *flecs.Writer) {
    e := fw.NewEntity()
    flecs.Set(fw, e, Position{X: 10, Y: 20})
    // The deferred queue is flushed at end of Write scope.
    // The observer fires as part of the flush.
})
```

This is consistent with how hooks work: both hooks and observers are invoked during the flush of the deferred command queue at the close of the `Write` scope.

For world-level callbacks that fire at every merge boundary (not tied to a specific component or event), see [Systems.md § Merge hooks](Systems.md#merge-hooks). Merge hooks (`OnPreMerge` / `OnPostMerge`) are the world-level analog to per-id observers: observers react to specific component add/set/remove events; merge hooks react to the merge boundary itself.

---

## Use Cases

### Validation

Fire a callback whenever a component is set to enforce invariants:

```go
type Health struct{ HP int }

flecs.OnSet[Health](w, func(fw *flecs.Writer, e flecs.ID, v Health) {
    if v.HP < 0 {
        panic(fmt.Sprintf("entity %d: Health.HP must not be negative, got %d", e, v.HP))
    }
})
```

### Indexing / Secondary Data Structures

Maintain an external index in sync with component changes:

```go
type Name struct{ Value string }

nameIndex := map[string]flecs.ID{}

flecs.Observe[Name](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v Name) {
    nameIndex[v.Value] = e
})
flecs.Observe[Name](w, flecs.EventOnRemove, func(fw *flecs.Writer, e flecs.ID, v Name) {
    delete(nameIndex, v.Value)
})
```

### Replication

Detect mutations and send them over the network:

```go
type Transform struct{ X, Y, Z float32 }

flecs.Observe[Transform](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v Transform) {
    sendToNetwork(e, v)
})
```

### Logging / Auditing

Attach multiple subscribers for different concerns without modifying the component owner:

```go
// Subsystem A
flecs.Observe[Position](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v Position) {
    metrics.Record("position_set", e)
})

// Subsystem B
flecs.Observe[Position](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v Position) {
    logger.Debug("position updated", "entity", e, "pos", v)
})
```

---

## Observer Ordering

### Hook vs. Observer Order

For the same (component, event) pair, the hook always fires before observers. For `OnRemove`, the ordering is: observers fire first, then the `OnRemove` hook (hook is last, like a destructor).

### Observer Order is Registration Order

When two observers subscribe to the same (component, event), they fire in registration order. While the current implementation is deterministic, you should not rely on the relative order of independent observers — treat it as unspecified.

---

## Monitor Observers {#monitor-observers}

A **monitor observer** fires at most once when an entity *enters* a query match (all terms satisfied) and once when it *exits* (any term no longer satisfied). Unlike a regular `OnSet` observer — which fires on every write — a monitor fires only on the boolean transition.

Canonical uses: state machines, tutorial triggers, alert systems, debug counters.

### Basic Usage

```go
w := flecs.New()
healthID := flecs.RegisterComponent[Health](w)
frozenID := flecs.RegisterComponent[Frozen](w)

// Fire when entity first has BOTH Health AND NOT Frozen.
obs := flecs.Monitor(w, []flecs.Term{
    flecs.With(healthID),
    flecs.Without(frozenID),
}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
    if entered {
        fmt.Println("entity entered combat-ready state:", e)
    } else {
        fmt.Println("entity left combat-ready state:", e)
    }
})

// Cancel subscription when no longer needed.
obs.Unsubscribe()
```

### Yield-Existing

`MonitorWithOptions` with `WithYieldExisting()` sweeps all entities that already satisfy the query at registration time, firing `fn(fw, e, true)` for each. The sweep is synchronous — `MonitorWithOptions` returns only after all existing matches are visited.

```go
flecs.MonitorWithOptions(w, []flecs.Term{flecs.With(healthID)},
    flecs.WithYieldExisting(),
    func(fw *flecs.Writer, e flecs.ID, entered bool) {
        // fires true for all current Health-holders at registration time,
        // and true/false for all subsequent transitions
    },
)
```

### Supported Term Types

Monitors support the full set of query terms:

| Term kind | Constructor | Notes |
|-----------|-------------|-------|
| Require component | `With(id)` | Entity must have this component |
| Exclude component | `Without(id)` | Entity must NOT have this component |
| OR group | `Or(a), Or(b)` | Entity must have at least one in the group |
| Optional | `Maybe(id)` | No effect on matching |
| DontFragment | `With(dfID)` | Tracked via per-entity matched set |
| Union pair | `With(MakePair(R, T))` | Tracked via union store |

### Disabled Monitor Semantics

A disabled monitor (via `obs.SetEnabled(false)`) receives no events and accumulates no catch-up state. Re-enabling it does not sweep existing matches — it resumes from whatever state was current when it was disabled.

### Entity Deletion and Clear

When an entity that matches a monitor is deleted (`fw.Delete(e)`) or cleared (`fw.Clear(e)`), the monitor fires `fn(fw, e, false)` **before** any component removal, so the callback can still read the entity's components.

### Implementation Notes

Monitors use a hybrid match-state tracking strategy:

- **Archetype-only monitors** (no DontFragment/Union/Sparse terms): A table-pair check is performed on every `migrate()` call — O(monitors × terms) per archetype transition, no per-entity state.
- **Sparse-mode monitors** (any And/Not term with DontFragment, Union, or Sparse flag): A per-monitor `matched` set tracks which entities currently satisfy the query. The set is updated at each relevant component-change site.

---

## Multi-Term Observers {#multi-term-observers}

**Shipped in v0.70.0.**

A multi-term observer fires when a trigger component event fires **and** the entity passes all additional filter terms. This is the observer equivalent of a query filter: you subscribe to a component event but only react when the entity matches a broader set of conditions.

### Basic Usage

```go
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)

// Fires when Position is set, but only if the entity also has Velocity.
obs := flecs.ObserveQuery(w, flecs.EventOnSet,
    []flecs.Term{
        flecs.With(posID),   // trigger term — must be first, must be TermAnd
        flecs.With(velID),   // filter term — entity must also have Velocity
    },
    func(fw *flecs.Writer, e flecs.ID, ptr unsafe.Pointer) {
        pos := *(*Position)(ptr)
        fmt.Printf("entity %v moved to %+v\n", e, pos)
    },
)
defer obs.Unsubscribe()
```

The **first term** is always the trigger: it determines which `(component, event)` pair dispatches the observer. The remaining terms are filters evaluated per-entity at dispatch time. Only if all filters pass does the callback fire.

### Term Kinds

All term kinds supported by queries work as filter terms:

| Term | Meaning |
|------|---------|
| `flecs.With(id)` | Entity must have the component / tag |
| `flecs.Without(id)` | Entity must **not** have the component / tag |
| `flecs.Or(id1), flecs.Or(id2)` | Entity must have at least one of the consecutive `Or` terms |
| `flecs.With(flecs.MakePair(R, Wildcard))` | Entity must have **any** pair with relation R |

```go
frozenID := flecs.RegisterComponent[Frozen](w)
childOfAny := flecs.MakePair(w.ChildOf(), w.Wildcard())

// Fires on Position set, entity not Frozen, and entity has some parent.
obs := flecs.ObserveQuery(w, flecs.EventOnSet,
    []flecs.Term{
        flecs.With(posID),
        flecs.Without(frozenID),
        flecs.With(childOfAny),
    },
    func(fw *flecs.Writer, e flecs.ID, ptr unsafe.Pointer) { ... },
)
```

### Multi-Event Variant

`ObserveQueryEvents` subscribes to multiple events at once. The callback receives the `EventKind` that fired:

```go
obs := flecs.ObserveQueryEvents(w,
    []flecs.EventKind{flecs.EventOnAdd, flecs.EventOnSet, flecs.EventOnRemove},
    []flecs.Term{flecs.With(posID), flecs.With(velID)},
    func(fw *flecs.Writer, ev flecs.EventKind, e flecs.ID, ptr unsafe.Pointer) {
        switch ev {
        case flecs.EventOnAdd:    fmt.Println("added")
        case flecs.EventOnSet:    fmt.Println("set")
        case flecs.EventOnRemove: fmt.Println("removed")
        }
    },
)
```

### Options Variant

`ObserveQueryWithOptions` accepts `ObserverOptions` for additional control:

```go
// WithYieldExisting: fire for entities that already match at registration time.
obs := flecs.ObserveQueryWithOptions(w,
    flecs.WithYieldExisting(),
    []flecs.EventKind{flecs.EventOnSet},
    []flecs.Term{flecs.With(posID), flecs.With(velID)},
    func(fw *flecs.Writer, _ flecs.EventKind, e flecs.ID, ptr unsafe.Pointer) { ... },
)

// WithSource: fire only for a specific entity, with additional filter terms.
obs = flecs.ObserveQueryWithOptions(w,
    flecs.WithSource(playerID),
    []flecs.EventKind{flecs.EventOnSet},
    []flecs.Term{flecs.With(posID), flecs.Without(frozenID)},
    func(fw *flecs.Writer, _ flecs.EventKind, e flecs.ID, ptr unsafe.Pointer) { ... },
)
```

### Raw-ID Trigger Variant

`ObserveQueryID` separates the trigger ID from the filter terms. Use this when the trigger is a pair or raw ID that is inconvenient to express as `terms[0]`:

```go
pairID := flecs.MakePair(velocityID, someRelID)

obs := flecs.ObserveQueryID(w, pairID, flecs.EventOnSet,
    []flecs.Term{flecs.With(posID)}, // filter only; trigger is pairID
    func(fw *flecs.Writer, e flecs.ID, ptr unsafe.Pointer) { ... },
)
```

### DontFragment / Sparse Terms

Filter terms referencing DontFragment or Sparse (Union) components are evaluated per-entity at dispatch time — the same path as DontFragment-mode queries. The trigger can also be a sparse/DontFragment component.

### Semantics

- **Trigger must be TermAnd**: `terms[0].Kind` must be `TermAnd`. Any other kind panics at registration time.
- **At least one term required**: `len(terms) == 0` panics.
- **Short-circuit evaluation**: the first failing filter term skips the callback; remaining terms are not evaluated.
- **Dispatch order**: Multi-term observers fire in registration order, after any single-term observers for the same trigger, as part of the same `dispatchObservers` loop.
- **Re-entry safety**: multi-term observer callbacks run in deferred mode (like all observers); structural mutations inside the callback are queued and applied after dispatch.
- **yield_existing + OnRemove-only**: panics at registration time — there are no "existing removed" entities.
- **yield_existing skips Disabled/Prefab**: entities in a table tagged `Disabled` or `Prefab` are not yielded.

---

## Not Yet Ported in Go flecs

The following features from C flecs are not yet available in the Go port. They are documented here so you know where the boundaries are.

### OnDelete / OnDeleteTarget Events

✅ **Shipped in v0.103.0.** Go flecs now fires `EventOnDelete` when an entity is deleted and `EventOnDeleteTarget` when a pair target is deleted. See [OnDelete and OnDeleteTarget Events](#on-delete-and-on-delete-target) for the full API.

### OnTableDelete {#on-table-delete}

✅ **Shipped in v0.101.0.**

`OnTableDelete` fires synchronously just before a table is freed by the table-reclamation sweep. It is the counterpart to `OnTableCreate` and the last event an archetype table can produce.

```go
flecs.OnTableDelete(w, func(fr *flecs.Reader, t *flecs.Table) {
    fmt.Printf("reclaiming table: %v (had %d rows)\n", t.Type(), t.Count())
})
```

The handler receives a `*flecs.Reader` instead of a `*flecs.Writer`. The table is mid-destruction: its row count is already 0, but `t.Type()` is still valid and the columns have not yet been freed. Structural mutations (adding/removing components) must not be issued from this handler; use the reader to inspect world state only.

#### WithYieldExisting is a no-op

`WithYieldExisting()` is silently ignored for `OnTableDelete`. A table cannot retroactively be "already deleted" — deletion is a one-time event; there is nothing to replay at observer registration time.

#### Multi-term filter

Use `OnTableDeleteWithOptions` with `WithQuery(terms...)` to restrict the observer to tables whose component signature satisfies the filter:

```go
posID := flecs.RegisterComponent[Position](w)

flecs.OnTableDeleteWithOptions(w,
    flecs.WithQuery(flecs.Term{ID: posID, Kind: flecs.TermAnd}),
    func(fr *flecs.Reader, t *flecs.Table) {
        fmt.Printf("Position table reclaimed: %v\n", t.Type())
    },
)
```

The filter is evaluated against `t.Type()` at dispatch time. Only tables whose signature satisfies all filter terms will invoke the handler.

#### Handler context rationale

The handler uses `*Reader` (not `*Writer`) because the table is mid-destruction: its columns are about to be freed and the table will be removed from all world indexes. Issuing a `Write` from inside the handler could recreate the same archetype (allocating a fresh table), which is safe — but issuing mutations that reference the dying table itself would produce undefined behavior. The `*Reader` constraint makes the distinction explicit.

See [TableReclamation.md](TableReclamation.md) for the full reclamation model, threshold tuning, and reference-counting semantics.

### OnTableEmpty / OnTableFill Events {#on-table-empty-and-on-table-fill}

✅ **Shipped in v0.98.0.**

`OnTableFill` fires the first time any entity is placed in a previously-empty archetype table (row count 0→1). `OnTableEmpty` fires when the last entity leaves a table (row count 1→0). These are table-level events, not per-entity events.

```go
// Fire once when the first entity enters any Position table.
flecs.OnTableFill(w, func(fw *flecs.Writer, t *flecs.Table) {
    fmt.Printf("table filled: %v\n", t.Type())
})

// Fire once when the last entity leaves any Position table.
flecs.OnTableEmpty(w, func(fw *flecs.Writer, t *flecs.Table) {
    fmt.Printf("table emptied: %v\n", t.Type())
})
```

#### Multi-term filter

Use `OnTableFillWithOptions` / `OnTableEmptyWithOptions` with `WithQuery(terms...)` to restrict the observer to tables whose component signature satisfies the filter:

```go
posID := flecs.RegisterComponent[Position](w)

// Only fires for tables that include Position.
flecs.OnTableFillWithOptions(w,
    flecs.WithQuery(flecs.Term{ID: posID, Kind: flecs.TermAnd}),
    func(fw *flecs.Writer, t *flecs.Table) {
        fmt.Printf("Position table filled: %v\n", t.Type())
    })
```

Filter terms are evaluated against the table's archetype signature (the sorted component ID list). `TermAnd` requires the ID to be present; `TermNot` requires the ID to be absent; OR-groups require at least one ID to be present. DontFragment and Sparse components are stored outside the archetype signature and always fail `TermAnd` on table-level filters.

#### yield_existing

`WithYieldExisting()` fires the callback synchronously at registration time for all matching tables in their current state:

- `OnTableFill + WithYieldExisting()` — sweeps all currently **non-empty** tables (tables with at least one row), in sorted-signature order. The empty root table (`[]`) is excluded.
- `OnTableEmpty + WithYieldExisting()` — sweeps all currently **empty** tables (tables with zero rows). The root empty table is included.

```go
// Receive Fill for every non-empty table that already exists.
flecs.OnTableFillWithOptions(w, flecs.WithYieldExisting(),
    func(fw *flecs.Writer, t *flecs.Table) { ... })
```

#### Root empty table semantics

The root table (signature `[]`) is the table all entities occupy before any component is added. It is always alive. `OnTableFill` fires on the first `NewEntity` call (0→1 transition) and `OnTableEmpty` fires when the last bare entity is deleted (1→0 transition). `yield_existing` for `OnTableEmpty` includes the root table when it is currently empty.

#### Deferred-scope semantics

Transitions fire in order of occurrence during `Write(fn)` flush, matching the per-entity OnAdd/OnRemove semantics. A table can flicker 0→1→0 within a single batch; all transitions are emitted in sequence. The handler receives a `*Writer` for deferred mutations; any components added or deleted inside the handler are processed after the enclosing flush completes.

#### Comparison with OnTableCreate

| | `OnTableCreate` | `OnTableFill` / `OnTableEmpty` |
|---|---|---|
| Fires | Once per new archetype (first time any entity uses it) | Every 0→1 / 1→0 row-count transition |
| Fires for root table? | No (`[]` excluded) | Yes (as a normal transition) |
| yield_existing | Non-empty tables | Fill: non-empty; Empty: empty tables |
| Multi-term filter | No | Yes, against table signature |

### Custom Events

✅ **Shipped in v0.63.0.** See [Custom Events](#custom-events) above. `RegisterEvent`, `Emit`, `EmitTyped[T]`, `ObserveEvent`, `ObserveEventTyped[T]`, and built-in event entity accessors (`w.EventOnAdd()` etc.) are all available.

### Term-Set Observer Filters (Multi-Term Observers)

✅ **Shipped in v0.70.0.** See [Multi-Term Observers](#multi-term-observers) above for the `ObserveQuery` / `ObserveQueryID` / `ObserveQueryEvents` / `ObserveQueryWithOptions` API.

### Yield-on-Create

✅ **Shipped in v0.60.0.** See [yield_existing](#yield-existing) above for the `ObserveWithOptions[T]` + `WithYieldExisting()` API.

### Observer Propagation / Forwarding

✅ **Shipped in v0.72.0.** See [Propagation along IsA](#propagation-along-isa) above. `OnAdd`, `OnSet`, `OnRemove`, `OnReplace` hook, and custom `Emit` events all propagate downward along IsA edges with DontInherit and override gates. BFS cache with O(1) invalidation on structural change.

### Monitor Observers

✅ **Shipped in v0.65.0.** See [Monitor Observers](#monitor-observers) above for the `Monitor` / `MonitorWithOptions` API.

### Observer Disabling

✅ **Shipped in v0.60.0.** See [Disabling an Observer](#disabling-an-observer) above for `(*Observer).SetEnabled(bool)` / `(*Observer).IsEnabled() bool`. Cross-link: [Systems.md § Disabling a System](Systems.md#disabling-a-system).

### Fixed-Source Observer Terms

✅ **Shipped in v0.67.0** via `WithSource(e ID)` option on `ObserveWithOptions[T]` / `ObserveIDWithOptions` / `ObserveEventWithOptions`.

A fixed-source observer fires only when the event lands on a specific named entity rather than any entity. Common use cases: watching the `Player` singleton, tracking a global `GameTime` entity, or reacting when a specific UI widget's state changes.

```go
player := ...  // entity ID obtained earlier

// Register: fires only when Position is set on player, not on any other entity.
obs := flecs.ObserveWithOptions[Position](w,
    flecs.WithSource(player),
    []flecs.EventKind{flecs.EventOnSet},
    func(fw *flecs.Writer, _ flecs.EventKind, e flecs.ID, pos Position) {
        fmt.Printf("player position changed to %+v\n", pos)
    },
)

// Combine with yield_existing to also fire immediately if player already has Position:
obs = flecs.ObserveWithOptions[Position](w,
    flecs.WithYieldExisting().AndSource(player),
    []flecs.EventKind{flecs.EventOnSet},
    func(fw *flecs.Writer, _ flecs.EventKind, e flecs.ID, pos Position) { ... },
)
```

#### Semantics

- **Registration-time panics**: `WithSource(0)` panics (zero ID is never valid). `WithSource` combined with `EventOnTableCreate` panics (tables are not entities).
- **Stale entity IDs**: Registering with a deleted entity ID succeeds silently and the observer simply never fires — consistent with other stale-ID semantics throughout the API.
- **Dispatch order**: Any-entity observers fire **before** fixed-source observers for the same `(component, event)` key. Within each group, registration order is preserved.
- **Disabled / Prefab interaction**: The check is against the named source entity's archetype table. If the source entity carries the `Disabled` or `Prefab` tag, it is skipped in `yield_existing` (mirroring upstream C behavior where the affected entity's table flags gate dispatch). Runtime dispatch is not affected — use `(*Observer).SetEnabled` to pause a fixed-source observer.
- **yield_existing + WithSource**: O(1) — only the named entity is checked; no table walk. Fires once iff the source holds the component (and is not Disabled/Prefab) at registration time.
- **Custom events**: `ObserveEventWithOptions(w, eventID, WithSource(player), fn)` restricts the observer to `Emit` calls that name `player` as the entity.

#### Non-goals (v0.67.0)

- Multi-term observers with mixed `$this` / fixed-source terms are out of scope.
- Fixed-source for monitor observers is out of scope.
- Automatic cleanup when the named source is deleted is out of scope.
- Traversal modifiers on the source (e.g. `Up(ChildOf)`) are out of scope.

---

## OnDelete and OnDeleteTarget Events {#on-delete-and-on-delete-target}

✅ **Shipped in v0.103.0.**

`OnDelete` fires once per entity whose lifecycle is about to end (via `Delete` or via cleanup-policy cascade), **before** the existing `OnRemove` hooks run. `OnDeleteTarget` fires once per `(target, dependent, pairRelID)` triple during the cleanup-policy DFS, **before** the dependent entity is enqueued for delete-or-remove.

### Dispatch ordering

```
1. Delete(e) called (or cascade triggered by parent delete)
2. EventOnDelete fires for e — handler reads e's state via *Reader
3. Component-remove cascade: if e is used as a component (RemoveAction policy),
   remove it from all holder entities (fires OnRemove per holder)
4. OnRemove hooks fire for each component on e
5. Walk dependents: for each (R, e) where R has an OnDeleteTarget policy —
   a. EventOnDeleteTarget fires for (target=e, dependent, pairRelID=R)
   b. Apply policy: Delete → recurse step 2; Remove → remove pair; Panic → panic after fire
6. Free e's slot
```

### Handler context: `*Reader`, not `*Writer`

Both handlers receive `*Reader` because the entity is mid-delete: its component storage is still intact and readable, but issuing structural mutations (Add, Remove, Set) during the dispatch window is unsafe. To mutate the world from an `OnDelete` or `OnDeleteTarget` handler, defer via `World.Write(fn)`:

```go
flecs.OnDelete(w, func(fr *flecs.Reader, e flecs.ID) {
    v, _ := flecs.Get[Health](fr, e)
    // Schedule a write after the cascade settles:
    w.Write(func(fw *flecs.Writer) {
        flecs.Set(fw, logEntity, DeathLog{Victim: e, HP: v.HP})
    })
})
```

### Basic registration

```go
// Fire for any entity being deleted.
flecs.OnDelete(w, func(fr *flecs.Reader, e flecs.ID) {
    fmt.Printf("entity %v is being deleted\n", e)
})

// Fire for each (target, dependent, relationship) triple during cascade.
flecs.OnDeleteTarget(w, func(fr *flecs.Reader, target, dependent, pairRelID flecs.ID) {
    fmt.Printf("deleting %v cascades to %v via relationship %v\n",
        target, dependent, pairRelID)
})
```

### WithQuery filter

Use `OnDeleteWithOptions` with `WithQuery(terms...)` to fire only for entities whose archetype table matches at the moment of deletion:

```go
posID := flecs.RegisterComponent[Position](w)

flecs.OnDeleteWithOptions(w,
    flecs.WithQuery(flecs.With(posID)),
    func(fr *flecs.Reader, e flecs.ID) {
        fmt.Printf("Position-bearing entity %v deleted\n", e)
    },
)
```

### WithRelationship filter

Use `OnDeleteTargetWithOptions` with `WithRelationship(relID)` to restrict the observer to cascades driven by a specific relationship:

```go
// Only fire when ChildOf cascade is the cause — ignore other relationships.
flecs.OnDeleteTargetWithOptions(w,
    flecs.WithRelationship(w.ChildOf()),
    func(fr *flecs.Reader, target, dependent, pairRelID flecs.ID) {
        fmt.Printf("child %v orphaned by deletion of parent %v\n", dependent, target)
    },
)
```

Combine `WithRelationship` and `WithQuery` by chaining:

```go
flecs.OnDeleteTargetWithOptions(w,
    flecs.WithRelationship(w.ChildOf()).AndQuery(flecs.With(posID)),
    func(fr *flecs.Reader, target, dep, rel flecs.ID) { ... },
)
```

### WithYieldExisting — no-op

`WithYieldExisting()` is silently ignored for both events. Delete events are future-only; there is no meaningful state to replay at registration time.

### Component-remove cascade (Feature 2)

When a component entity is deleted with the default `RemoveAction` policy, all entities that currently hold it as a component undergo archetype migration: the component is removed from their signature and `OnRemove` fires per entity. This ensures no table retains a deleted component ID in its live entity set.

**Performance note**: the cascade is O(entities-with-component). For large component sets this can be significant; consider using `OnDelete` to observe and `World.Write(fn)` to schedule deferred cleanup if needed.

```go
type Position struct{ X, Y float32 }
posID := flecs.RegisterComponent[Position](w)

e := flecs.NewEntityWith[Position](w, Position{X: 1})

// After this: e is still alive but no longer has Position.
w.Delete(posID)
```

See [docs/Relationships.md](Relationships.md#cleanup-policy-observer-events) for the relationship between cleanup policies and observer events.

---

## Summary

| API | What it does |
|-----|-------------|
| `OnAdd[T](w, fn)` | Register OnAdd hook for component T (one per type) |
| `OnSet[T](w, fn)` | Register OnSet hook for component T (one per type) |
| `OnRemove[T](w, fn)` | Register OnRemove hook for component T (one per type) |
| `Observe[T](w, event, fn)` | Subscribe typed observer; returns `*Observer` |
| `ObserveID(w, id, event, fn)` | Subscribe raw-ID observer; returns `*Observer` |
| `Observe2[T](w, events, fn)` | Subscribe to multiple events; single `*Observer` handle |
| `ObserveWithOptions[T](w, opts, events, fn)` | Subscribe with options (e.g. `WithYieldExisting()`, `WithSource(e)`); returns `*Observer` |
| `ObserveIDWithOptions(w, id, opts, events, fn)` | Raw-ID options variant; accepts `WithSource(e)` |
| `ObserveEventWithOptions(w, eventID, opts, fn)` | Custom-event options variant; accepts `WithSource(e)` |
| `WithYieldExisting()` | Option: retroactively fire for all existing matching entities at registration |
| `WithSource(e ID)` | Option: restrict observer to fire only for the named entity |
| `(ObserverOptions).AndSource(e ID)` | Chain source onto an existing options value (e.g. `WithYieldExisting().AndSource(player)`) |
| `(*Observer).Unsubscribe()` | Cancel subscription (idempotent; safe from callback) |
| `(*Observer).SetEnabled(bool)` | Enable or disable observer dispatch; default true |
| `(*Observer).IsEnabled() bool` | Report whether observer is currently enabled |
| `EventOnAdd` | Event fired on first component add |
| `EventOnSet` | Event fired on every component set |
| `EventOnRemove` | Event fired before component remove |
| `Monitor(w, terms, fn)` | Register monitor observer; fires on query-match entry/exit |
| `MonitorWithOptions(w, terms, opts, fn)` | Monitor with options (e.g. `WithYieldExisting()`) |
| `(*World).EventMonitor()` | Built-in EventMonitor entity (index 46) |
| `ObserveQuery(w, event, terms, fn)` | Multi-term observer: trigger from `terms[0]`, filter from `terms[1:]` |
| `ObserveQueryID(w, triggerID, event, filterTerms, fn)` | Multi-term observer with explicit trigger ID |
| `ObserveQueryEvents(w, events, terms, fn)` | Multi-term, multi-event observer |
| `ObserveQueryWithOptions(w, opts, events, terms, fn)` | Multi-term observer with options (yield_existing, source) |
| `OnDelete(w, fn)` | Register observer that fires before entity deletion (before `OnRemove`) |
| `OnDeleteWithOptions(w, opts, fn)` | OnDelete with `WithQuery` filter; `WithYieldExisting()` is a no-op |
| `OnDeleteTarget(w, fn)` | Register observer for cleanup-policy cascade triples |
| `OnDeleteTargetWithOptions(w, opts, fn)` | OnDeleteTarget with `WithRelationship` / `WithQuery` filters |
| `WithRelationship(relID ID)` | Option: restrict `OnDeleteTarget` to a specific relationship |
| `(ObserverOptions).AndQuery(terms ...Term)` | Chain `WithRelationship` with multi-term filter |
| `(*World).EventOnDelete() ID` | Built-in EventOnDelete entity (index 76) |
| `(*World).EventOnDeleteTarget() ID` | Built-in EventOnDeleteTarget entity (index 77) |

---

## See Also

- [Quickstart](Quickstart.md) — hands-on introduction to observers and hooks.
- [EntitiesComponents.md](EntitiesComponents.md) — `OnAdd` / `OnSet` / `OnRemove` hooks on component registration.
- [Systems.md](Systems.md) — per-frame systems as a complement to reactive observers.
- [Manual](Manual.md) — top-level reference hub with world lifecycle, concurrency model, and concept map.
