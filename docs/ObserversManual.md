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

## Not Yet Ported in Go flecs

The following features from C flecs are not yet available in the Go port. They are documented here so you know where the boundaries are.

### OnDelete / OnDeleteTarget Events

C flecs fires `OnDelete` when a component entity itself is deleted, and `OnDeleteTarget` when a pair target is deleted. Neither event exists in Go flecs.

**Workaround**: Manually call cleanup code before deleting component entities.

### OnTableEmpty / OnTableFill Events

C flecs can fire events when an archetype table transitions between empty and non-empty. Not ported to Go flecs.

### Custom Events

C flecs lets you create arbitrary event entities and emit them with `ecs_emit`. Go flecs has no `emit` API. Only the three built-in events (`EventOnAdd`, `EventOnSet`, `EventOnRemove`) are supported.

### Term-Set Observer Filters (Multi-Term Observers)

C flecs observers can match a query with multiple terms (e.g., "fire when Position is set, but only if the entity also has Velocity"). Go flecs `Observe[T]` and `Observe2[T]` subscribe to a single component at a time. `ObserveID` also covers one component. There is no multi-term observer API.

### Yield-on-Create

✅ **Shipped in v0.60.0.** See [yield_existing](#yield-existing) above for the `ObserveWithOptions[T]` + `WithYieldExisting()` API.

### Observer Propagation / Forwarding

C flecs propagates events along relationship edges (e.g., an `OnSet(Position)` on a parent notifies children that inherit `Position` via `ChildOf`). Go flecs does not propagate events.

### Monitor Observers

C flecs has a `Monitor` event that fires when an entity starts or stops matching a query (once on enter, once on exit). Not ported to Go flecs.

### Observer Disabling

✅ **Shipped in v0.60.0.** See [Disabling an Observer](#disabling-an-observer) above for `(*Observer).SetEnabled(bool)` / `(*Observer).IsEnabled() bool`. Cross-link: [Systems.md § Disabling a System](Systems.md#disabling-a-system).

### Fixed-Source Observer Terms

C flecs observers can match a component on a specific entity (not `$this`). Not ported to Go flecs — all `Observe[T]` subscriptions match on any entity.

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
| `ObserveWithOptions[T](w, opts, events, fn)` | Subscribe with options (e.g. `WithYieldExisting()`); returns `*Observer` |
| `WithYieldExisting()` | Option: retroactively fire for all existing matching entities at registration |
| `(*Observer).Unsubscribe()` | Cancel subscription (idempotent; safe from callback) |
| `(*Observer).SetEnabled(bool)` | Enable or disable observer dispatch; default true |
| `(*Observer).IsEnabled() bool` | Report whether observer is currently enabled |
| `EventOnAdd` | Event fired on first component add |
| `EventOnSet` | Event fired on every component set |
| `EventOnRemove` | Event fired before component remove |

---

## See Also

- [Quickstart](Quickstart.md) — hands-on introduction to observers and hooks.
- [EntitiesComponents.md](EntitiesComponents.md) — `OnAdd` / `OnSet` / `OnRemove` hooks on component registration.
- [Systems.md](Systems.md) — per-frame systems as a complement to reactive observers.
- [Manual](Manual.md) — top-level reference hub with world lifecycle, concurrency model, and concept map.
