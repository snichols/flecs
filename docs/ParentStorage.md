# Parent Hierarchy Storage

Parent hierarchy storage is an opt-in mode that eliminates table fragmentation for exclusive parent-child relationships such as `ChildOf`. Instead of encoding the specific parent entity in the archetype signature — which forces children of distinct parents into separate tables — parent storage keeps a single shared table per component combination and stores the parent reference in a per-entity column alongside regular component data.

See [Hierarchies](HierarchiesManual.md) for general hierarchy concepts and [Relationships](Relationships.md) for the broader relationship model.

## Table of contents

- [Motivation](#motivation)
- [API](#api)
- [Storage shape](#storage-shape)
- [Performance characteristics](#performance-characteristics)
- [When to enable](#when-to-enable)
- [Query integration](#query-integration)
- [Observer and hook compatibility](#observer-and-hook-compatibility)
- [Cleanup policies](#cleanup-policies)
- [Snapshot and JSON round-trip](#snapshot-and-json-round-trip)
- [Limitations](#limitations)

---

## Motivation

In default (fragmenting) mode, a pair `(ChildOf, P)` becomes part of the archetype signature. A world with N distinct parents and M children per parent using the same components produces N tables of M entities rather than one table of N×M entities. This is the "table explosion" problem:

```
Default: N=3 parents, M children each with {Position, Velocity}
  Table 1: {Position, Velocity, (ChildOf, P1)}  — M rows
  Table 2: {Position, Velocity, (ChildOf, P2)}  — M rows
  Table 3: {Position, Velocity, (ChildOf, P3)}  — M rows

Parent storage:
  Table: {Position, Velocity, (ChildOf, *)}     — 3M rows, parentColumn = [P1, P1, …, P2, …]
```

Fragmentation degrades cache utilisation for queries and makes reparenting expensive (it requires a full archetype migration — remove the old pair, add the new one, copy all components).

Parent storage solves both problems:

- **One table** regardless of how many distinct parents entities have.
- **O(1) reparenting**: changing an entity's parent is a single column write, no component copying.

---

## API

```go
// Enable parent storage for a relationship. Must be called before any entity
// carries the relationship. Panics if:
//   - relID is not a relationship (SetRelationship was not called).
//   - relID is not exclusive (SetExclusive was not called).
//   - Any entity already carries (relID, *).
flecs.SetParentStorage(w, relID)

// Test whether parent storage is active for relID.
flecs.IsParentStorage(r, relID) bool
```

`ChildOf` is already exclusive, so enabling it requires only one call:

```go
flecs.SetParentStorage(w, w.ChildOf())
```

For a custom relationship:

```go
var rel flecs.ID
w.Write(func(fw *flecs.Writer) { rel = fw.NewEntity() })
flecs.SetRelationship(w, rel)
flecs.SetExclusive(w, rel)
flecs.SetParentStorage(w, rel)
```

---

## Storage shape

Once parent storage is active for `relID`:

- The archetype signature uses a **marker** `(relID, Any)` instead of the concrete target.
- A **parent column** — a `[]ID` slice parallel to the entity list — stores the actual parent of each row.
- Two entities `{Position, (relID, P1)}` and `{Position, (relID, P2)}` share the table `{Position, (relID, Any)}`.

All public APIs (`HasID`, `ParentOf`, `GetUp`, `HasUp`, `TargetUp`, queries, `EachChild`, `PathOf`, `Lookup`) consult the parent column transparently; callers do not need to know about the marker.

---

## Performance characteristics

| Operation | Default (fragmenting) | Parent storage |
|---|---|---|
| Reparent entity | O(components) — full migration | O(1) — column write |
| Query wildcard `(rel, *)` | O(tables) per table | O(1) — single shared table |
| Query exact `(rel, P)` | O(1) — direct table lookup | O(entities in table) — per-row scan |
| Memory | N tables × overhead | 1 table + parentColumn |
| Cache locality | Poor (N small tables) | Good (1 large table) |

The headline benchmark (`BenchmarkParentStorage_Reparent_FullArchetype`) shows ≥4× speedup for reparenting an entity with 8 components.

Exact-target queries (`WithPair(rel, P)`) require a per-row scan of the shared table, which is slower than the direct table lookup in fragmented mode. If your workload queries by exact target at high frequency, weigh this trade-off before enabling parent storage.

---

## When to enable

Enable parent storage when:

- You have many distinct parents with children that share the same component set (e.g., a scene graph with hundreds of nodes).
- Reparenting is frequent (character attachment, dynamic scene restructuring).
- Cache locality for component access across all children matters more than per-parent query speed.

Keep fragmenting mode (the default) when:

- The number of distinct parents is small and fixed.
- You query frequently by exact parent (`WithPair(ChildOf, specificParent)`) and performance of those queries matters.
- You do not reparent at runtime.

---

## Query integration

Parent-storage pairs are fully supported in queries:

```go
// Wildcard — matches all children in the shared table.
q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(rel, w.Wildcard())))

// Exact target — per-row filter on the parent column.
q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(rel, p1)))

// Variable target — binds to parentColumn[row] per entity.
q := flecs.NewQueryFromTerms(w, flecs.WithPairTgtVar(rel, "P"))

// Cascade ordering — works across parent-storage tables.
cq := flecs.NewCachedQueryFromTerms(w, flecs.With(markerID).Cascade(w.ChildOf()))
```

Ancestor traversal functions (`GetUp`, `HasUp`, `TargetUp`) and `EachChild`, `ParentOf`, `PathOf`, `Lookup` all use the parent column and behave identically whether parent storage is active.

---

## Observer and hook compatibility

All observers and hooks fire correctly with parent storage:

- `ObserveID` with `EventOnAdd` / `EventOnRemove` fires on pair add and remove (including reparenting).
- `OnReplace[T]` hooks on other components fire normally when the entity has a parent-storage pair.
- `OnTableCreate` fires once when the shared table is first allocated (not per parent).
- `OnTableFill` fires on the 0→1 row transition of the shared table.
- `OnTableEmpty` fires on the 1→0 row transition.

---

## Cleanup policies

Parent storage interacts with `OnDeleteTarget` cleanup policies:

| Policy | Behaviour |
|---|---|
| `DeleteAction` (ChildOf default) | Scans parent column; cascade-deletes all source entities whose parent is the deleted target. |
| `PanicAction` | Scans parent column; panics if any source entity has the deleted target as its parent. |
| `RemoveAction` (explicit) | Scans parent column; removes the pair from all source entities (pair cleared, sources stay alive). |

Set the policy before entities carry the relationship:

```go
flecs.SetCleanupPolicy(w, rel, w.OnDeleteTarget(), w.RemoveAction())
```

---

## Snapshot and JSON round-trip

Parent storage state is fully serialized:

- **Binary snapshot** (`TakeSnapshot` / `RestoreSnapshot`): parent columns are serialized alongside component columns; the `parentStoragePolicies` map is restored.
- **JSON** (`json.Marshal(w)` / `json.Unmarshal`): the parent-storage flag per relationship and all parent IDs are persisted.

---

## Limitations

- **Single parent**: parent storage stores exactly one parent per entity. Multi-parent stays fragmented (exclusive relationship constraint).
- **Enable before population**: `SetParentStorage` panics if any entity already carries `(relID, *)`. Plan your setup order accordingly.
- **Exact-target query cost**: `WithPair(rel, P)` scans all entities in the shared table. For high-frequency exact-target queries with many entities, fragmented mode may be faster.
- **No runtime migration**: switching an already-populated relationship between modes is not supported.
- **OrderedChildren rank**: the sibling-rank column optimization is deferred to a future phase. `OrderedChildren` uses the existing side-map.
