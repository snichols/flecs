# Snapshots

A *snapshot* is a point-in-time, in-memory copy of all user-created world state.
It can be restored to the same world at any later point, or serialised to bytes and
written to disk / sent over the wire.

## API

```go
// Capture the world.
s := flecs.TakeSnapshot(w)

// Restore from a snapshot (same world only).
flecs.RestoreSnapshot(w, s)

// Serialise to a byte slice (for disk / network).
data, err := flecs.Bytes(s)

// Deserialise a byte slice back to a snapshot.
s2, err := flecs.LoadSnapshot(data)
```

All four functions are safe to call from any goroutine, but they require exclusive
world access — they must be called **outside** any `w.Write` or `w.Read` block.
Calling `TakeSnapshot` or `RestoreSnapshot` while a write block is active panics
with `ErrExclusiveAccessViolation`.

## What is captured

`TakeSnapshot` copies:

- Every user-created entity, its generation counter, and its full component set.
- Component column data (structure-of-arrays).
- Disabled-component bitsets (from `SetCanToggle` / `DisableID`).
- Sparse-component and DontFragment side-maps.
- Union relationship state.
- Active entity ID range (`RangeSet`).
- Cleanup, instantiate, and OneOf trait policies.
- Ordered-children insertion-order lists.
- The entity-index recycle queue.

Built-in entities (component-registration entities, built-in relationship targets,
built-in phase entities, etc.) are **not** captured — they are re-created at
`flecs.New()` time and are always present.

## What is NOT captured

- **Observers and observer subscriptions.** They must be re-registered manually
  after a restore. This is a deliberate design choice: observers carry Go closures
  that cannot be serialised, and re-firing `OnAdd` for every entity on restore
  would trigger side effects that contradict the snapshot's point-in-time semantics.
- **Systems and pipeline.** Systems are code, not data.
- **Hooks** (`OnAdd[T]`, `OnSet[T]`, `OnRemove[T]`, `OnReplace[T]`). Same reason
  as observers.
- **World-level merge hooks** (`OnPreMerge` / `OnPostMerge`).

## Restore semantics

`RestoreSnapshot` performs an in-place overwrite of the world's user state:

1. All user entities are removed from the entity index and archetype tables.
2. User entries are stripped from the recycle queue.
3. The snapshot's entities are re-inserted exactly — same raw index, same
   generation, same component data. `TakeSnapshot` → `RestoreSnapshot` produces
   a world that is bit-for-bit equivalent to the state at snapshot time, with the
   exception of observers, systems, and hooks (see above).

The operation is atomic from the caller's perspective: the world is in a consistent
state before the call and after it. No partial state is observable.

## Serialisation format

`flecs.Bytes` serialises a snapshot to a compact binary format:

```
Header:
  magic:    [0xF1, 0xEC, 0x53, 0x00]  (4 bytes)
  version:  uint32 big-endian         (format version, currently 1)
  worldID:  uint64 little-endian      (world identity token)

Body (little-endian throughout):
  Components     — registered user component descriptors
  EntityIndex    — dense alive + recycle queue + maxID
  Tables         — per-archetype column data
  EmptyTableEnts — entities in empty (no-component) tables
  SparseData     — per-component sparse-set entries
  UnionState     — per-relationship union side-map
  EntityRange    — min/max from RangeSet (0/0 if cleared)
  Policies       — cleanup, instantiate, OneOf policy maps
  OrderedChildren — per-parent ordered child lists
```

`flecs.LoadSnapshot` reads the byte slice and validates the magic bytes and format
version. A version mismatch returns an error; it does not panic. A worldID mismatch
between the snapshot and the target world also returns an error.

**Stability disclaimer**: The binary format is versioned but is not yet considered
stable across library versions. Version 1 may gain additive fields without bumping
the version number. Breaking changes will increment the version number, causing
`LoadSnapshot` to return an error on old snapshots.

## Cross-world restriction

A snapshot is tied to the world that created it. Calling `RestoreSnapshot` with a
snapshot from a *different* world panics:

```go
w1 := flecs.New()
w2 := flecs.New()
s := flecs.TakeSnapshot(w1)
flecs.RestoreSnapshot(w2, s) // panics: world identity mismatch
```

The `worldID` field in the serialised format enforces the same check for
`LoadSnapshot` / `RestoreSnapshot` pairs across process boundaries: load the bytes
on the same world that produced them (same `flecs.New` instance or a world
initialised with the same built-in entity layout).

## Memory bound

A snapshot allocates approximately `entity_count × avg_bytes_per_entity` for
component data plus a small fixed overhead per archetype table. There is no
reference counting — the snapshot owns its byte blob independently of the world.
Multiple snapshots from the same world coexist without interference.

## Example: undo / redo

```go
w := flecs.New()

// Populate the world...
var stack []*flecs.Snapshot

w.Write(func(fw *flecs.Writer) {
    e := fw.NewEntity()
    flecs.Set(fw, e, Position{X: 1, Y: 2})
})
stack = append(stack, flecs.TakeSnapshot(w))

w.Write(func(fw *flecs.Writer) {
    flecs.Each1[Position](fw, func(id flecs.ID, p *Position) {
        p.X += 10
    })
})
stack = append(stack, flecs.TakeSnapshot(w))

// Undo: restore the previous snapshot.
flecs.RestoreSnapshot(w, stack[len(stack)-2])
```

## Example: save to disk

```go
s := flecs.TakeSnapshot(w)
data, err := flecs.Bytes(s)
if err != nil {
    log.Fatal(err)
}
os.WriteFile("world.snap", data, 0o644)

// Later, in another process:
raw, _ := os.ReadFile("world.snap")
s2, err := flecs.LoadSnapshot(raw)
if err != nil {
    log.Fatal(err)
}
flecs.RestoreSnapshot(w, s2)
```
