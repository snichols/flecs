# flecs (Go port)

An idiomatic, high-performance Go port of the [flecs](https://github.com/SanderMertens/flecs)
Entity Component System library. Archetype-based storage, generic-typed API,
zero third-party dependencies.

[![Go version](https://img.shields.io/badge/go-1.26-blue)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![upstream](https://img.shields.io/badge/upstream-flecs-orange)](https://github.com/SanderMertens/flecs)

---

## Documentation

**[docs/Quickstart.md](docs/Quickstart.md)** — start here. Covers world creation, entities, components, queries, hierarchies, prefabs, systems, and observers with runnable Go examples.

The full docs index (survey table, porting status, and feature-gap list vs. upstream C) is at **[docs/README.md](docs/README.md)**.

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

    // Create an entity and attach components inside a Write scope.
    var e flecs.ID
    w.Write(func(fw *flecs.Writer) {
        e = fw.NewEntity()
        flecs.Set(fw, e, Position{X: 1, Y: 2})
        flecs.Set(fw, e, Velocity{DX: 0.5, DY: 0})
    })

    // Iterate every entity that has both Position and Velocity.
    w.Write(func(fw *flecs.Writer) {
        flecs.Each2[Position, Velocity](fw, func(id flecs.ID, p *Position, v *Velocity) {
            p.X += v.DX
            p.Y += v.DY
        })
    })

    // Read back.
    w.Read(func(fr *flecs.Reader) {
        if pos, ok := flecs.Get[Position](fr, e); ok {
            fmt.Printf("position: %.1f %.1f\n", pos.X, pos.Y) // 1.5 2.0
        }
    })

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
- `NewQueryFromTerms` — structured terms with `With`, `Without`, `Maybe`, `Or`, `WithoutScope`, `IsEntity`, `NotEntity`, `NameMatches`, `AndFrom`, `OrFrom`, `NotFrom` (NOT / Optional / OR / scope / equality / name-match / type-list expansion support).
- **`GET /query?expr=`** — REST endpoint accepts Flecs Query Language v2 strings (v0.96.0): AND/OR/NOT, scope groups, optional terms, traversal (`.Up`/`.SelfUp`/`.Cascade`), source binding, query variables, equality predicates, `AndFrom`/`OrFrom`/`NotFrom`. See [docs/QueryDSL.md](docs/QueryDSL.md).
- `NewCachedQuery` / `NewCachedQueryFromTerms` — persistent queries that incrementally track new tables.

#### Cascade for parent-before-child systems

`Term.Cascade(rel)` orders matched tables in a cached query from shallowest to
deepest in the `rel` hierarchy — so a parent's row is always processed before its
children. This is the canonical pattern for "propagate from parent to child" systems
(e.g. accumulate a world-space transform from a local-space transform):

```go
type Transform struct{ X, Y float32 }

transformID := flecs.RegisterComponent[Transform](w)

var root, mid, leaf flecs.ID
w.Write(func(fw *flecs.Writer) {
    root = fw.NewEntity()
    flecs.Set(fw, root, Transform{X: 0, Y: 0})

    mid = fw.NewEntity()
    flecs.Set(fw, mid, Transform{X: 1, Y: 0})
    flecs.AddID(fw, mid, flecs.MakePair(w.ChildOf(), root))

    leaf = fw.NewEntity()
    flecs.Set(fw, leaf, Transform{X: 0, Y: 1})
    flecs.AddID(fw, leaf, flecs.MakePair(w.ChildOf(), mid))
})

// Iterate root → mid → leaf (ascending ChildOf depth).
cq := flecs.NewCachedQueryFromTerms(w,
    flecs.With(transformID).Cascade(w.ChildOf()),
)
cq.Each(func(it *flecs.QueryIter) {
    transforms := flecs.Field[Transform](it, transformID)
    for i := range transforms {
        // process in parent-before-child order
        _ = transforms[i]
    }
})
```

`Term.Up(rel)` and `Term.SelfUp(rel)` express per-term inheritance in both
uncached and cached queries. `IsFieldSelf` and `FieldShared[T]` disambiguate
local vs. inherited values at iteration time.

Marking a component **inheritable** via `SetInheritable[T](w)` causes all
`Each1`/`Each2`/`NewQuery` terms for that component to auto-promote to
`Self|Up(IsA)` — inheritor entities are visible without explicit traversal
modifiers. Non-inheritable components (the default) are unaffected.

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
| Non-fragmenting parent storage _(v0.102.0)_ | `SetParentStorage(w, rel)`, `IsParentStorage(s, rel)` — opt-in O(1) reparenting; all child children of different parents share one archetype table |
| IsA inheritance | `w.IsA()`, `Get`/`Has` walk the chain, `PrefabOf` |
| Named entities | `SetName`, `GetName`, `Lookup`, `PathOf` |
| Hooks (single) | `OnAdd[T]`, `OnSet[T]`, `OnRemove[T]`, `OnReplace[T]` / `OnReplaceID` |
| Observers (multi) | `Observe[T]`, `ObserveID`, `Observe2[T]`, `Unsubscribe` |
| Observer disabling _(v0.60.0)_ | `obs.SetEnabled(false)` / `obs.IsEnabled()` — pause/resume without removing; mirrors system disabling |
| yield_existing _(v0.60.0)_ | `ObserveWithOptions[T](w, WithYieldExisting(), events, fn)` — retroactively fire for all existing matching entities at registration; OnAdd/OnSet only; synchronous; skips Disabled/Prefab |
| OnTableCreate observer _(v0.62.0)_ | `OnTableCreate(w, fn)` / `OnTableCreateWithOptions(w, WithYieldExisting(), fn)` — fires once per new archetype table; untyped (no `[T]`); handler reads `t.Type()` / `t.Count()`; does not fire for the empty root table |
| OnTableFill / OnTableEmpty _(v0.98.0)_ | `OnTableFill(w, fn)` / `OnTableEmpty(w, fn)` — fires on 0→1 / 1→0 row-count transitions for archetype tables; `WithOptions` variants support multi-term filters (`WithQuery(terms...)`) and `WithYieldExisting()`; root empty table fires normally |
| Table reclamation _(v0.101.0)_ | Empty archetype tables are freed after 60 idle `Progress()` ticks (configurable via `SetTableReclamationThreshold`); `ReclaimNow()` forces immediate sweep; `OnTableDelete(w, fn)` fires before reclamation; `Pin()` / `Unpin()` opt a table out; reference-counted safety (queries, cached queries, open iters hold a ref). See [docs/TableReclamation.md](docs/TableReclamation.md) |
| Custom events _(v0.63.0)_ | `RegisterEvent(fw, name)`, `Emit(fw, eventID, entity, payload)`, `EmitTyped[T]`, `ObserveEvent(w, eventID, fn)`, `ObserveEventTyped[T]` — arbitrary user-defined event entities; events ARE entities; synchronous dispatch; payload is `interface{}` with typed wrapper; built-in event entity accessors: `w.EventOnAdd()` etc. |
| Query groups _(v0.66.0)_ | `WithGroupBy(compID, groupFn)` partitions a cached query's tables by `uint64` group ID; `IterGroup(id)` for O(1) group access; `Groups()` lists populated IDs; composes with `WithOrderBy` (sort within each group) |
| Monitor observers _(v0.65.0)_ | `Monitor(w, terms, fn)` / `MonitorWithOptions(w, terms, opts, fn)` — fires `fn(fw, e, entered)` on query-match entry/exit; multi-term; yield_existing; DontFragment/Union terms supported; `Unsubscribe` to cancel |
| Fixed-source observers _(v0.67.0)_ | `ObserveWithOptions[T](w, WithSource(playerID), events, fn)` — restricts dispatch to a single named entity; composes with `WithYieldExisting()` via `WithYieldExisting().AndSource(e)`; `ObserveIDWithOptions` for raw-ID / pair IDs; `ObserveEventWithOptions` for custom events |
| Dynamic component registration _(v0.68.0)_ | `RegisterDynamicComponent(fw, name, size, align)` / `RegisterDynamicComponentWithMarshaler` — runtime component registration with no Go type at compile time; `GetIDPtr` / `SetIDPtr` / `EachByID` for raw-pointer access; `OnAddByID` / `OnSetByID` / `OnRemoveByID` lifecycle hooks; routes through archetype / sparse / DontFragment storage; base64 JSON serialization by default with optional custom marshal/unmarshal hooks |
| Prefab hierarchies + slots _(v0.69.0)_ | `AddID(e, MakePair(w.IsA(), prefab))` replicates prefab's full child subtree (two-pass, cross-reference rewriting); `SlotOf` relationship (`w.SlotOf()`) — prefab child with `(SlotOf, prefab)` adds `(prefabChild, instanceChild)` on the instance root; `GetPairTarget(scope, inst, prefabChild)` resolves slot O(1) |
| Multi-term observers _(v0.70.0)_ | `ObserveQuery(w, event, terms, fn)` / `ObserveQueryID` / `ObserveQueryEvents` / `ObserveQueryWithOptions` — fires on trigger component event only if entity passes all filter terms; supports TermAnd / TermNot / TermOr / wildcard pairs / DontFragment/Sparse triggers; `WithYieldExisting()` and `WithSource(e)` options |
| Entity ID ranges _(v0.71.0)_ | `RangeSet(fw, min, max)` / `RangeClear(fw)` / `RangeGet(scope)` / `RangeNew(fw, min, max)` — constrain `NewEntity` to a `[min, max)` slice for per-owner ID partitioning; one-shot allocation via `RangeNew`; JSON round-trip preserves range state |
| Entity scoping _(v0.74.0)_ | `WithinScope(fw, parent, fn)` — push a parent so every `NewEntity` / `RangeNew` inside fn auto-receives `(ChildOf, parent)`; defer-based pop survives panics. `PushScope(fw, parent)` / `PopScope(fw, prev)` for cross-function-boundary callers. `GetScope(scope)` returns current top (0 on Reader). `MakeAlive` bypasses scope; `RangeNew` respects it. Stack resets at each top-level `w.Write` entry. |
| Observer propagation along IsA _(v0.72.0)_ | `OnAdd`/`OnSet`/`OnRemove` and custom `Emit` propagate downward along IsA edges after the source-entity dispatch — once per transitive inheritor; DontInherit gate suppresses entirely; override gate (inheritor owns local copy) skips that inheritor; multi-term observers re-evaluate filter per inheritor; BFS cache with O(1) invalidation |
| Deferred commands | `Defer`, `DeferBegin`, `DeferEnd` |
| Readonly concurrency window | `w.Readonly(fn)`, `ReadonlyBegin`, `ReadonlyEnd` |
| Exclusive-access ownership assertion | `ExclusiveAccessBegin`, `ExclusiveAccessEnd` — always on; panics with `ErrExclusiveAccessViolation` on cross-goroutine violations; common case costs one `atomic.Load` per call |
| NOT / Optional query terms | `NewQueryFromTerms`, `With`, `Without`, `Maybe`, `FieldMaybe` |
| OR query terms | `Or`, `TermOr`, `FieldMaybe` on Or-group IDs |
| Traversal query terms | `With(id).Up(rel)`, `.SelfUp(rel)`, `.Cascade(rel)`; `IsFieldSelf`, `FieldShared[T]` |
| Inheritable components | `SetInheritable[T](w)` / `w.SetInheritable(cid)` — auto-promotes query terms to `Self\|Up(IsA)` so inheritor entities are matched without explicit traversal |
| Systems + pipeline | `NewSystem`, `NewSystemInPhase`, `Progress`; custom phases via `NewPhase` + `(*Phase).DependsOn`; within-phase `(*System).DependsOn` ordering _(v0.64.0)_ |
| System disabling _(v0.58.0)_ | `sys.SetEnabled(false)` / `sys.IsEnabled()` — pause/resume without removing; `RunSystem` ignores the flag |
| Rate filters _(v0.61.0)_ | `sys.SetInterval(d)` / `sys.SetRate(n)` — run a system every N ticks or at most once per wall-clock duration; gates compose with AND semantics; counters freeze while disabled |
| Timer addon _(v0.91.0)_ | `NewTimer` / `NewInterval` / `NewRateFilter` — entity-based shared timers; `(*System).SetTickSource(e)` binds a system to fire only when its Timer/RateFilter fires; `StartTimer`/`StopTimer`/`ResetTimer`/`IsTimerFired`; `GetTimeout`/`GetInterval`; chained RateFilters; JSON + snapshot round-trip. See [docs/Timer.md](docs/Timer.md). |
| Single-system Run _(v0.58.0)_ | `RunSystem(s, dt)` — invoke one system synchronously, outside the pipeline; mutations flushed before return |
| Explicit thread dispatch _(v0.82.0)_ | `RunSystemWorker(w, sys, workerIndex, workerCount, dt)` — out-of-pipeline fan-out: each goroutine gets a disjoint row slice; fresh per-call command queue flushed before return; disabled flag bypassed. See [Systems.md § RunSystemWorker](docs/Systems.md#runsystemworker). |
| Alerts addon _(v0.83.0)_ | `RegisterAlert(fw, AlertDesc{Query, Severity, Message})` — query-driven raise/clear lifecycle via monitor observer; `w.Alerts()` / `w.AlertsBySeverity(sev)` / `w.AlertsForEntity(e)` snapshot helpers; `AlertInfo`/`AlertWarning`/`AlertError`/`AlertCritical` constants; `%d` entity-ID interpolation; definitions survive JSON round-trip. See [docs/Alerts.md](docs/Alerts.md). |
| Pipeline introspection _(v0.58.0)_ | `r.Phases() []*Phase`, `r.SystemsInPhase(phase *Phase) []*System`, `r.EachSystem(phase *Phase, fn)` — inspect registered systems in execution order |
| Parallel dispatch | `sys.SetParallel(true)`, `sys.SetWriteSet(ids)`, `w.SetWorkerCount(n)` — across-system concurrency with disjoint write sets |
| Multi-threaded dispatch | `sys.SetMultiThreaded(true)` — splits ONE system's iter across all workers (disjoint row slices); in-place `Field[T]` updates scale linearly; deferred mutations (Set/Delete) are safe but contend on the shared defer queue until Phase 11.0 |
| World-level merge hooks _(v0.78.0)_ | `OnPreMerge(w, fn)` / `OnPostMerge(w, fn)` — persistent callbacks at every deferred-command merge boundary; `RemovePreMergeHook` / `RemovePostMergeHook` for teardown; pre-hook mutations batch with current merge, post-hook mutations queue for next; one fire per merge boundary (not per worker stage); `ErrMergeReentry` guard prevents re-entrant `w.Write` from inside hooks. See [Systems.md § Merge hooks](docs/Systems.md#merge-hooks). |
| Fixed timestep | `SetFixedTimestep`, `OnFixedUpdate` phase |
| Singleton component trait _(v0.44.0)_ | `SetSingleton(w, compID)` / `IsSingleton(scope, compID)` / `SingletonEntity` / `Singleton[T]` / `WriteSingleton[T]` — at-most-one-holder enforcement (Go semantic; differs from C must-be-self) |
| WriteOnce component trait _(v0.45.0)_ | `SetWriteOnce(w, compID)` / `IsWriteOnce(scope, compID)` — panics on second Set; Remove clears tracking; formerly `Constant` (renamed to avoid collision with upstream `EcsConstant`) |
| Traversable relationship trait _(v0.46.0)_ | `SetTraversable(w, relID)` / `IsTraversable(scope, relID)` — query-time enforcement: non-traversable `.Up()`/`.SelfUp()`/`.Cascade()` panics at construction; implies Acyclic; `ChildOf` + `IsA` bootstrapped Traversable |
| Relationship / Target / Trait usage constraints _(v0.47.0)_ | `SetRelationship(w, id)` / `SetTarget(w, id)` / `SetTrait(w, id)` — write-time enforcement: bare-tag add or wrong-slot pair add panics; `Trait` exempts entity from `Relationship`'s no-target-slot check; built-ins bootstrapped |
| OrderedChildren trait _(v0.50.0)_ | `SetOrderedChildren(w, parentID)` / `IsOrderedChildren(scope, parentID)` — opt-in per parent; `EachChild` returns children in insertion order; in-callback snapshot; JSON round-trip |
| Sparse component storage _(v0.52.0)_ | `SetSparse(w, compID)` / `IsSparse(scope, compID)` / `EachSparse[T](scope, fn)` — per-component sparse-set; no archetype transition; pointer-stable; entity-delete cleanup; JSON round-trip; query integration: three-mode iterator (all-sparse / mixed / all-archetype), `Field[T]`/`FieldMaybe[T]` sparse branches, `Not`/`Optional` on sparse terms, `CachedQuery.Changed()` via version counter |
| DontFragment component trait _(v0.53.0)_ | `SetDontFragment(w, compID)` / `IsDontFragment(scope, compID)` — data in sparse-set, archetype transitions suppressed; use with `SetSparse` for the canonical no-transition + pointer-stable combination; entity-delete cleanup; JSON round-trip; query integration mirrors Sparse |
| Union relationship trait _(v0.54.0)_ | `SetUnion(w, relID)` / `IsUnion(scope, relID)` / `EachUnion(scope, relID, fn)` — at-most-one target per entity, stored in a per-relationship side map; changing the target does NOT trigger an archetype transition (no table proliferation); implies Exclusive; conflict-detection on mixed `SetExclusive`/`SetUnion`; entity-delete and relationship-delete cleanup; JSON round-trip; query integration: wildcard and specific-target iteration over the union store. **As of v0.54.0, the trait system is feature-complete vs upstream C flecs.** |
| Sorted cached queries _(v0.59.0)_ | `NewCachedQueryFromTermsWithOptions(w, WithOrderBy(compID, cmp), terms...)` / `OrderBy[T]` typed comparator wrapper / `OrderByFunc` raw unsafe.Pointer form — cached, lazily rebuilt on table `ChangeCount` changes; each `Next()` yields one entity in sort order; optional sort-by component supported (nil pointer for absent) |
| Entity lifecycle ops _(v0.56.0)_ | `Clear(fw, e)` — removes all components, fires `OnRemove`, leaves entity alive; deferred-coalescer support. `MakeAlive(fw, id)` — claims a specific entity ID for networked ID sync; panics on conflict or in deferred scope. `SetVersion(fw, newID)` — overrides the generation counter (monotonic; panics on decrease or in deferred scope). |
| Disabled + Prefab built-in tags _(v0.57.0)_ | `DisableEntity(fw, e)` / `EnableEntity(fw, e)` / `IsDisabled(scope, e)` — adds/removes the `Disabled` tag; excluded from ordinary queries (O(1) per-table skip). `MarkPrefab(fw, e)` / `IsPrefab(scope, e)` / `w.Prefab()` — marks an entity as a build template; excluded from ordinary queries; `Prefab` tag is `DontInherit` so IsA instances are not tagged. Both: opt in by mentioning the tag in any query term kind. |
| JSON serialization | `w.MarshalJSON()`, `w.UnmarshalJSON()` (entities + components + names + pairs: ChildOf/IsA hierarchies + custom tag/data pairs) |
| Binary snapshots _(v0.79.0)_ | `TakeSnapshot(w)` / `RestoreSnapshot(w, s)` — in-memory point-in-time world snapshot; `Bytes(s)` / `LoadSnapshot(data)` for disk/network. Captures entities, components, sparse/union state, policies, ordered children, recycle queue. Observers and systems are not captured. Same-world restriction. See [docs/Snapshots.md](docs/Snapshots.md). |
| Query variables ($Var) _(v0.81.0)_ | `WithVar(componentID, varName)` / `WithPairTgtVar(rel, varName)` / `(Term).SrcVar(name)` / `(Term).TgtVar(name)` / `(*QueryIter).Var(name) ID` — named runtime entity slots for multi-hop relational joins. Example: `With(shipID), WithPairTgtVar(dockedToID, "planet"), WithVar(planetID, "planet"), With(orbitsID).SrcVar("planet").TgtVar("star"), WithVar(starID, "star")` yields one row per (ship, planet, star) triple. Topo-sorted dependency order; cycle detection panics at construction; 16-variable cap. See [docs/Queries.md § Query variables](docs/Queries.md#query-variables). |
| Change detection | `q.Changed()` — opt-in per-table dirty tracking on `CachedQuery` |
| Stats / observability | `w.Stats()` — entity/table/query/system counts, per-phase frame timing, per-component table counts |
| Stats addon _(v0.93.0)_ | `w.StatsSnapshot()` — goroutine-safe `PipelineStats` snapshot: `WorldStats` (entity/table/frame counts, dt), `[]SystemStats` (per-system invocations, last-tick/avg/cumulative duration, gated-skip count), `[]PhaseStats` (per-phase last-tick + cumulative duration, invocation count); `(*System).SetName` for display names. Multi-period ring-buffer aggregation: `w.WorldStatsWindow(StatsSecond|StatsMinute|StatsHour)` → `WorldStatsAggregated` with per-metric `{Avg,Min,Max}`; `w.PipelineStatsWindow` for full pipeline; `w.StatsTick()` for manual advancement. See [docs/Stats.md](docs/Stats.md). |
| Units addon _(v0.85.0)_ | `RegisterUnit(fw, name, symbol, base, factor)` — register typed unit entities; `(*Writer).SetUnit(compID, unitID)` — tag a component; `(*World).UnitFor(compID)` — retrieve the unit; `Convert(w, value, from, to)` — walk the Base chain to convert compatible units (multi-hop; ok=false for incompatible). 15 built-in units: Meter/KiloMeter/MilliMeter, Second/MilliSecond/Minute/Hour, Gram/KiloGram/MegaGram, Newton, Joule, Hertz, Radian/Degree. User units and component→unit mappings survive JSON round-trip. See [docs/Units.md](docs/Units.md). |
| REST API _(v0.95.0)_ | `NewRESTHandler(w)` — HTTP inspection + entity/component mutation + query DSL (`GET /stats`, `/stats/world`, `/stats/pipeline`, `/components`, `/entities`, `/snapshot`, `/type_info/{path}`, `/query`; `PUT /snapshot`; `PUT /entity`, `DELETE /entity/{path...}`; `GET /component/{entity}/{component}`, `PUT /component/{entity}/{component}`, `DELETE /component/{entity}/{component}`; `PUT /toggle/{entity}`, `PUT /toggle/{entity}/{component}`). `GET /query?expr=<expression>` evaluates a Flecs Query Language v1 expression and returns matched entities with typed field values (`?limit`, `?offset`, `?fields=true/false`). `GET /component` reads a live component value (typed → JSON; tag → `{}`; dynamic → base64 string or custom marshaler); `PUT /component` sets or adds a component (typed, tag, dynamic, or pair); `DELETE /component` removes a component (idempotent). `PUT /toggle` toggles the `Disabled` tag (entity) or a `CanToggle` component bit; `?enabled=true/false` or omit to flip. Pair encoding: `~` separator in the component segment. `GET /type_info/{path}?depth=N` returns recursive depth-N struct schema with cycle detection, primitive annotations, and slice/array/map/pointer support (default depth 8, max 16; `?depth=1` for v0.87.0 back-compat). |
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
| Up/SelfUp/Cascade query terms | ✅ (`With(id).Up(rel)`, `.SelfUp(rel)`, `.Cascade(rel)`; `IsFieldSelf`, `FieldShared[T]`) | ✅ |
| Change detection | ✅ (`CachedQuery.Changed()`, per-table) | ✅ |
| Parallel system dispatch | ✅ (`SetParallel`, `SetWriteSet`, `SetWorkerCount`; per-phase disjoint write-set batching) | ✅ |
| Multi-threaded system dispatch | ✅ (`SetMultiThreaded`; within-system row-range split across all workers; in-place updates scale linearly; deferred mutations serialize on shared queue until Phase 11.0) | ✅ |
| REST API addon (minimal) | ✅ (`NewRESTHandler`, read-only inspection + snapshot) | ✅ |
| Query variables ($Var) | ✅ multi-variable (`WithVar`, `WithPairTgtVar`, `SrcVar`, `TgtVar`, `Var`; 16-variable cap; join-order optimizer v0.99.0 / Phase 16.44) | ✅ |
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
