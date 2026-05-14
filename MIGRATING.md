# Migrating

## v0.63.0 → v0.64.0 — BREAKING: Phase accessors return `*Phase`; `NewSystemInPhase` takes `*Phase`

### What changed

The four built-in phase accessors and the `NewSystemInPhase` registration function now use `*Phase` instead of `flecs.ID`. The `*Phase` type is an opaque handle that supports `DependsOn`, `SetEnabled`, `IsEnabled`, and `Name`. The pipeline is now lazily rebuilt with Kahn's topological sort every time the topology changes.

Additionally, the built-in entity count increases from 45 to 46 (new `DependsOn` relationship entity at index 45). User entity allocation now starts at index 46.

### Required migration steps

#### 1. Update phase accessor usage

```go
// v0.63.0 — accessors returned flecs.ID
var phase flecs.ID = w.OnUpdate()
flecs.NewSystemInPhase(w, w.OnUpdate(), q, fn)

// v0.64.0 — accessors return *flecs.Phase
var phase *flecs.Phase = w.OnUpdate()
flecs.NewSystemInPhase(w, w.OnUpdate(), q, fn) // same call, *Phase accepted
```

#### 2. Update introspection call sites

```go
// v0.63.0
phases := r.Phases()        // []flecs.ID
r.SystemsInPhase(phaseID)   // flecs.ID param
r.EachSystem(phaseID, fn)   // flecs.ID param
w.SystemCountInPhase(phaseID)

// v0.64.0
phases := r.Phases()         // []*flecs.Phase
r.SystemsInPhase(phasePtr)   // *flecs.Phase param
r.EachSystem(phasePtr, fn)   // *flecs.Phase param
w.SystemCountInPhase(phasePtr)
```

#### 3. Update hardcoded built-in entity counts

Code that hardcodes `44` or `45` as the number of built-in entities must be updated to `46`:

```go
// v0.63.0
const builtinEntityCount = 45

// v0.64.0
const builtinEntityCount = 46
```

#### 4. Update `WorldStats.LastFramePhases` usage

```go
// v0.63.0 — fixed-size array
var stats flecs.WorldStats = w.Stats()
_ = stats.LastFramePhases[2] // [4]PhaseStats

// v0.64.0 — dynamic slice
_ = stats.LastFramePhases[2] // []PhaseStats — same indexing, but len() may vary
```

## v0.52.0 → v0.53.0 (Phase 15.21) — BREAKING: Sparse/DontFragment split

### What changed

In v0.52.0, the `Sparse` trait consolidated two upstream C flecs behaviors into one:
- Data stored in a per-component sparse-set (no archetype column).
- Entity does NOT transition archetype tables on add/remove.

In v0.53.0, these behaviors are split into two independent traits matching upstream C:

| Trait | Data storage | Archetype transition | HasID/OwnsID source |
|-------|-------------|----------------------|---------------------|
| `Sparse` alone | sparse-set | **YES** — entity transitions tables | archetype type |
| `DontFragment` alone | sparse-set | **NO** — entity stays in current table | sparse-set |
| `Sparse + DontFragment` | sparse-set | **NO** — entity stays in current table | sparse-set |

**The canonical pattern that matches v0.51.0–v0.52.0 behavior is `Sparse + DontFragment` together.**

### Built-in entity indices

`DontFragment` is inserted at index 35; `Wildcard` shifts from 35→36; `Any` shifts from 36→37.
User entities now start at index 38 (was 37).

### Required migration steps

#### 1. Update `SetSparse` calls to `SetSparse + SetDontFragment`

Code that relied on `SetSparse` suppressing archetype transitions must now also call `SetDontFragment`:

```go
// v0.52.0 — Sparse suppressed archetype transitions
posID := flecs.RegisterComponent[Position](w)
flecs.SetSparse(w, posID)

// v0.53.0 — Must combine Sparse + DontFragment to match old behavior
posID := flecs.RegisterComponent[Position](w)
flecs.SetSparse(w, posID)
flecs.SetDontFragment(w, posID)
```

Alternatively, use `SetDontFragment` alone (Sparse policy not required for DontFragment):

```go
posID := flecs.RegisterComponent[Position](w)
flecs.SetDontFragment(w, posID)
```

#### 2. Update hardcoded built-in entity indices

Any code that hardcodes `Wildcard` at index 35 or `Any` at index 36 must update to 36 and 37 respectively.

```go
// v0.52.0
const wildcardIdx = 35 // WRONG in v0.53.0

// v0.53.0
const wildcardIdx = 36
```

#### 3. Regenerate JSON snapshots

The `MarshalJSON` format changed in v0.53.0:

- `sparse_components`: now lists Sparse-only component names (data in entity body, NOT in `sparse_data`).
- `dont_fragment_components`: NEW field — lists DontFragment component names.
- `sparse_data`: now only contains data for DontFragment components (data NOT in entity body).

**v0.52.0 snapshots cannot be loaded by v0.53.0 `UnmarshalJSON`** because:
- Old `SparseComponents` entries were DontFragment (no archetype), but v0.53.0 will apply Sparse-only policy (which changes the storage path).
- Old `SparseData` entries will not be restored (no `DontFragmentComponents` list triggers the DontFragment policy).

Regenerate all snapshots after updating component policies.

#### 4. Update test assertions

- `builtinEntityCount` constant: 36 → 37.
- `nonDataEntities` helper: add `w.DontFragment()`.
- `Wildcard` index assertions: 35 → 36.
- `Any` index assertions: 36 → 37.
- Tests that expected `SetSparse` to suppress archetype transitions: use `Sparse + DontFragment`.
- Tests that expected `Table()` to panic for Sparse-only queries: Sparse-only now uses mixed mode (archetype-seeded). Use `DontFragment` to trigger pure-sparse iteration and `Table()` panic.

### Behavior summary

| Scenario | v0.52.0 | v0.53.0 |
|----------|---------|---------|
| `SetSparse` only | No archetype transition | **Archetype transition occurs** |
| `SetDontFragment` only | N/A (new) | No archetype transition |
| `SetSparse + SetDontFragment` | N/A (new) | No archetype transition (canonical) |
| `HasID` / `OwnsID` for Sparse-only | sparse-set | archetype type |
| `HasID` / `OwnsID` for DontFragment | N/A | sparse-set |
| `Table()` panic for Sparse-only query | Yes (pure-sparse) | No (mixed mode) |
| `Table()` panic for DontFragment query | N/A | Yes (pure-DontFragment) |
</content>
</invoke>