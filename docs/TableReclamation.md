# Table Reclamation

**Shipped in v0.101.0 (Phase 16.46)**

Go-flecs uses an archetype ECS model: every entity is stored in an archetype table whose component signature exactly matches the entity's current component set. Archetypes are created lazily — the first entity with a new signature creates a new table — but prior to v0.101.0 tables were never freed. In workloads with high archetype churn (AI state machines, procedural content, networked replicas) this caused unbounded memory growth.

Table reclamation frees empty archetype tables that have been idle for a configurable number of ticks, bounding memory in long-running worlds.

---

## How reclamation works

### Tracking model

Each `*Table` carries three reclamation fields:

| Field | Type | Meaning |
|---|---|---|
| `emptyTicks` | `uint32` | Consecutive `Progress()` ticks during which `Count() == 0`. Reset to 0 by any `Append`. |
| `pinned` | `bool` | Set by `Pin()` / cleared by `Unpin()`. A pinned table is never reclaimed. |
| `refCount` | `int32` (atomic) | Bumped by open iterators, live query objects, and cached-query subscriptions. A table with `refCount > 0` is never reclaimed. |

### Sweep pass

At the start of every `Progress(dt)` call — before any system phase runs — a sweep pass walks all alive tables (skipping the root table, which has no component signature). For each non-root table:

1. If `Count() != 0`, reset `emptyTicks` (a new entity filled the table since last tick).
2. If `Count() == 0 && !pinned && refCount == 0`, increment `emptyTicks`.
3. If `emptyTicks >= threshold`, add the table to the reclamation set.

Tables in the reclamation set are freed in a second pass (after the full walk, to avoid iterator invalidation):

1. Fire `OnTableDelete` observers synchronously.
2. Remove the table from the world's table registry (`w.tables`).
3. Unlink the table from the component index (`compIndex.RemoveTable`).
4. Remove the table from all cached-query subscription lists.
5. Free column storage (`FreeColumns`).
6. Mark the table dead (`MarkDead`).
7. Increment `ReclaimedTablesCount`.

---

## Configuration

### Default threshold

The default threshold is **60 ticks**. At 60 fps this is 1 second of idle time before a table is reclaimed.

### API

```go
// Set the number of empty ticks before a table is reclaimed.
// Pass 0 to disable reclamation entirely (tables persist for World lifetime).
w.SetTableReclamationThreshold(ticks uint32)

// Read the current threshold.
w.TableReclamationThreshold() uint32

// Total number of tables freed since world creation.
w.ReclaimedTablesCount() uint64

// Force-reclaim all currently eligible tables immediately, regardless of threshold.
// Returns the number of tables freed. Useful in tests and at shutdown.
w.ReclaimNow() int
```

### Disabling reclamation

```go
w.SetTableReclamationThreshold(0) // tables persist for World lifetime (pre-v0.101.0 behavior)
```

---

## Pinning

Pin a table to prevent it from ever being reclaimed:

```go
t.Pin()   // permanent — survives empty-ticks accumulation
t.Unpin() // re-enables reclamation eligibility
t.IsPinned() bool
```

Pinned tables still accumulate `emptyTicks` but are skipped by the reclamation check. `IsPinned()` is evaluated atomically with the tick-advance so there is no window where a pinned table can be freed.

Use pinning for tables you cache a pointer to outside of an iterator (e.g. a hot-path lookup table you resolve once at startup).

---

## Reference counting

The reclamation sweep never frees a table with `refCount > 0`. The refcount is bumped automatically in three situations:

| Situation | Bump | Release |
|---|---|---|
| `NewQuery` / `NewQueryFromTerms` | +1 per matched table at construction | `q.Free()` |
| `CachedQuery` subscription | +1 when a table is added to the subscription list | `cq.Close()` or table removal |
| Open iterator (`Iter`) | +1 at `Iter.Open()` | `Iter.Close()` |

**Always call `q.Free()` and `cq.Close()` when done.** Leaked query objects hold refcounts and prevent reclamation indefinitely.

### Manual bump (advanced)

```go
t.IncrRef() // bump — table will not be reclaimed while held
t.DecrRef() // release — allow reclamation once count reaches 0
t.RefCount() int32
```

---

## Dead-table safety

Once a table is reclaimed it is marked dead (`t.IsDead() == true`) and its columns are freed. Any pointer to the table that was cached outside the refcount mechanism (e.g. in a stale edge-cache entry) will be detected: the `migrate()` path checks `IsDead()` before following a cached edge, and discards the stale entry if the destination is dead. The edge is then re-resolved from scratch and cached fresh.

---

## OnTableDelete event

`OnTableDelete` fires synchronously inside the reclamation loop, just before column storage is freed. See [ObserversManual.md § OnTableDelete](ObserversManual.md#on-table-delete) for the full API.

The handler receives a `*Reader` (not `*Writer`) because the table is mid-destruction. `t.Type()` is still valid; `t.Count()` is 0; columns are not yet freed but must not be accessed directly.

---

## Snapshot integration

`RestoreSnapshot` resets `emptyTicks` to 0 for all tables in the restored world. This prevents tables that were idle in the snapshot from being immediately reclaimed on the first `Progress()` call after restore.

---

## Performance notes

- The sweep pass is O(T) in the number of alive tables — typically small (hundreds, not millions).
- The reclamation pass is O(R×Q) where R is the number of reclaimed tables and Q is the number of cached queries. In normal workloads R is 0 most ticks.
- `ReclaimedTablesCount()` is a simple counter read — suitable for metrics/monitoring in the game loop.
- At shutdown, call `w.ReclaimNow()` to eagerly free all idle tables before the world is discarded.

---

## Debugging tips

- `w.ReclaimedTablesCount()` — check if reclamation is running at all (should grow in workloads with archetype churn).
- `w.TableReclamationThreshold()` — verify the configured threshold.
- `w.ReclaimNow()` — force a full sweep in a test to observe immediate reclamation without waiting for 60 ticks.
- If a test sees `Count() == 0` but the table is still alive, check: is `threshold == 0`? Is `emptyTicks < threshold`? Is the table pinned or ref-counted?
- Add an `OnTableDelete` observer to log which tables are being freed and when.
