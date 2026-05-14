## Goal

Add a **binary**, **in-memory**, **fast** world-snapshot facility as an alternative to the existing JSON `Marshal` / `Unmarshal` path. Snapshots target use cases where JSON is too slow or too verbose:

- **Save games** — snapshot at checkpoint; persist bytes to disk.
- **Undo systems** — snapshot before each user action; restore on undo.
- **Network rollback** — predict server state; roll back on misprediction.
- **Editor scratchpads** — snapshot before destructive operations.

Key properties:
- **Binary format**, more compact and faster than JSON.
- **Identity-preserving**: entity IDs and generations survive snapshot/restore round-trip; external references remain valid.
- **Full-world** in v1 (selective deferred to Phase 16.25).
- **In-place restore** into the originating world; current state is fully overwritten.

Phase 14.0 gap entry at `@docs/README.md` line 86 (*"World snapshots (beyond JSON serialization) — not ported."*) flips to ✅ shipped (v0.79.0) on completion.

### Upstream C reference (historical; v4 removed the addon)

Cited from `git show ed5ec3bf6^` against `/work/agents/claude/projects/SanderMertens/flecs` (the commit immediately before `ed5ec3bf6 Remove snapshot addon`):

- `include/flecs/addons/snapshot.h` lines 30-101 — public API: `ecs_snapshot_take` (full-world), `ecs_snapshot_take_w_iter` (selective via iterator), `ecs_snapshot_restore` (in-place), `ecs_snapshot_iter` / `ecs_snapshot_next` (iterate snapshot data), `ecs_snapshot_free`. Header docstring (lines 8-11) locks in cross-world restriction: *"A snapshot is tightly coupled to a world. It is not possible to restore a snapshot from world A into world B."*
- `src/addons/snapshot.c` lines 13-18 — `ecs_snapshot_t { world, entity_index, tables, last_id }` is the entire on-the-wire shape (in-memory in C; binary serialisation is Go's addition).
- `src/addons/snapshot.c` lines 21-25 — `ecs_table_leaf_t { table, type, data }` is the per-table block.
- `src/addons/snapshot.c` lines 73 — `if (table->flags & EcsTableHasBuiltins) return;` confirms built-in tables are skipped on take (and rebuilt by world init on restore).
- `src/addons/snapshot.c` lines 144-157 — `ecs_snapshot_take`: copies the entity-index wholesale, captures `last_id`, walks every non-builtin table.
- `src/addons/snapshot.c` lines 160-172 — `ecs_snapshot_take_w_iter`: same code path, iterator-driven for selective snapshots.
- `src/addons/snapshot.c` lines 177+ `restore_unfiltered` — restore is in-place into the source world: entity index is replaced, `last_id` is reset, world tables are diffed against snapshot tables (re-created if missing, data-replaced if still present, deleted if absent from snapshot).
- `docs/MigrationGuide.md` line 504-505 — *"The snapshot feature has been removed from v4."* The Go port re-introduces it as a Go-native binary addon; no upstream feature parity to track going forward.

### Recommended Go-side approach

Define a binary format using `encoding/binary` with a deliberate 8-byte magic+version header. The format is internal; no stable-API guarantee in v1; future versions may evolve freely. Version mismatch on `LoadSnapshot` returns an error.

The JSON marshal at `@marshal.go` (764 lines) enumerates everything a full-world serialisation must capture: entity index, table data, traits, sparse + DontFragment storage, Union relationships, observer registrations (omitted — see below), policy maps, entity-ID range state. The binary snapshot captures the same logical state, just in a different encoding.

## Constraints

- @marshal.go — JSON marshal infrastructure (764 lines). `jsonWorld` (lines 14-42) enumerates everything that must round-trip. Binary snapshot captures the same logical surface (sparse-component list, DontFragment list, sparse data, union-relationship serial map, entity-range min/max/set state) in binary form. Use this as the checklist for what the snapshot block layout must include.
- @internal/storage/table/table.go — archetype-table memory layout (`Table` struct lines 30-50: `sig []ids.ID`, `columns []*Column`, `entities []ids.ID`, `bitsets map[ids.ID][]uint64`, `changeCount uint64`). Per-table snapshot block must capture sig + column bytes + entities + bitsets (CanToggle state). `changeCount` does NOT need to survive — local cached-query bookkeeping.
- @internal/storage/table/column.go — column byte layout; use the existing `reflect.SliceOf` machinery (Phase 16.13 dynamic components, `component_dynamic.go` lines 168, 187, 206) for bulk byte copy of column data.
- @internal/storage/entityindex/entityindex.go — entity index state (`Index` lines 57+: `dense []ids.ID`, `recycle []ids.ID`, paged `pages []*page` with 64-slot `Record { Row, Dense, Table }`). Must survive snapshot/restore for identity preservation. `Record.Table *table.Table` is a pointer — restore must rebuild table pointers from rebuilt archetypes.
- @internal/component/registry.go — TypeInfo registry (`Registry` lines 19-25: `m map[reflect.Type]*TypeInfo`, `order []reflect.Type`, `byID map[ids.ID]*TypeInfo`, `idOrder []ids.ID`, `byDynName map[string]*TypeInfo`). Snapshot must preserve component-ID ↔ type bindings; restore re-attaches by name to existing registry entries (registry is NOT serialised — components must be pre-registered in the restore-target world).
- @world.go lines 40-80 — `World` struct; built-in entity IDs at indices 1-30+ are recreated by `NewWorld`, never serialised. Snapshot block for built-ins is just a count + marker (per upstream `EcsTableHasBuiltins` skip at `snapshot.c:73`).
- @component_dynamic.go — dynamic component registration pattern; reflect.SliceOf machinery for bulk byte copy (lines 168, 187, 206 reference dynamic components). Re-use this for column data copy.
- @CONTRIBUTING.md — coverage ≥ 95.0%, `go vet ./... ` clean, `golangci-lint run` clean, `go test ./... -race -count=3` clean, doc updates per the standard six-target pattern (per-feature doc page + `docs/README.md` gap flip + top-level `README.md` feature list + `CHANGELOG.md` + `ROADMAP.md`).
- Cross-world restore is forbidden — locks in the upstream restriction (`snapshot.h` lines 8-11). `RestoreSnapshot` must validate the snapshot belongs to the target world (e.g. via a world-identity token captured at take time).

## Deliverables

### 1. `snapshot.go`

- `type Snapshot struct { ... }` — opaque struct holding the snapshot blob (private fields).
- `TakeSnapshot(w *World) *Snapshot` — full-world snapshot of the current state.
- `RestoreSnapshot(w *World, s *Snapshot)` — in-place restore. World's current state is fully overwritten; entity references held in user code that are NOT in the snapshot become invalid (panic on use).
- `(*Snapshot).Bytes() []byte` — serialise to bytes for disk storage.
- `LoadSnapshot(b []byte) (*Snapshot, error)` — deserialise; validate the version header.

### 2. Binary format

- 8-byte header: 4 bytes magic + 4 bytes version number (uint32 BE).
- Component-registry block: for each component ID in registry, name + ID. Restore matches by name against the target world's pre-registered components; missing components → error.
- Entity-index block: alive entities (dense vector), generations, FIFO recycle queue, paged record state minus the table pointer (rebuilt post-restore by walking restored tables).
- Per-table block: archetype signature + column data (bulk byte copy via `reflect.SliceOf`, see `component_dynamic.go` lines 168/187/206) + entity-ID column + bitsets (CanToggle).
- Sparse storage block (Sparse-trait component data).
- DontFragment storage block.
- Union state block (relationship → entity → target).
- Trait policy maps (Sparse / DontFragment / Union / Exclusive / Symmetric / Transitive / Reflexive / Acyclic / Final / OneOf / Singleton / WriteOnce / Traversable / Relationship / Target / Inherit / Override / DontInherit / CanToggle / OnDelete / OnDeleteTarget / OnInstantiate).
- Entity-range state (min/max/set).
- Built-in table marker — count only; recreated by `NewWorld`.
- Observer registrations: **NOT serialised** (function pointers can't survive). State in doc.

### 3. Restore semantics

- Stop the world: panic if a `Read` or `Write` block is in progress (`w.mu` is a `sync.RWMutex`; treat as concurrent-restore violation).
- Clear current state: drop all non-builtin entities, non-builtin tables, sparse data, traits.
- Reconstruct from snapshot data: rebuild tables in their pre-snapshot signature order, restore column bytes, restore entity-index, restore sparse / DontFragment / Union / policy state.
- Post-restore: observers registered BEFORE the snapshot do NOT fire for restored entities (would be a flood). State in doc.

### 4. Identity preservation

- Entity IDs survive: an entity `e` snapshotted is the same `e` after restore.
- Generations survive: post-restore generation matches pre-snapshot generation.
- Archetype `*Table` pointers DO NOT survive (memory is re-allocated). State in doc. Any user code that cached `*Table` pointers (rare; export_test.go territory) must reacquire after restore.

### 5. `snapshot_test.go` — at least 13 cases

1. 100-entity world: snapshot, restore — entity count + per-entity component state matches.
2. Identity preservation: entity ID `42` exists before and after restore (round-trip).
3. Round-trip via `Bytes` / `LoadSnapshot`.
4. Intervening mutations: 100 entities, snapshot, delete 50, restore → 100 entities again.
5. Sparse components survive.
6. DontFragment components survive.
7. Union state survives (relationship → entity → target).
8. Trait policies survive (e.g., a `Sparse`'d component is still `Sparse`'d after restore).
9. Observers are NOT restored — registered observer does NOT fire on restored entities.
10. Version mismatch on `LoadSnapshot` returns an error.
11. Empty-world snapshot+restore: works.
12. Concurrent take during a `Write`: panics.
13. Concurrent restore during a `Write` or `Read`: panics.
14. Memory regression: 1000 entities of a 3-component archetype produces ≤ pick-a-bound KB (e.g., 64 KB; tune during implementation).

### 6. Doc updates

- New `docs/Snapshots.md` (preferred over wedging into `EntitiesComponents.md`): API + binary-format overview + restore semantics + observer caveat + cross-world restriction + version-stability disclaimer.
- `docs/README.md` line 86 → ✅ shipped (v0.79.0).
- `README.md` feature-list bump.
- `CHANGELOG.md` v0.79.0 entry at top.
- `ROADMAP.md` heading bump to *"through v0.79.0"*.

### 7. Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run` clean.
- `go test ./... -race -count=3` passes.
- Coverage ≥ 95.0%.

## Explicit non-goals (locked in)

- **No backward compatibility** for the snapshot binary format — version mismatch returns an error. Format may evolve freely in future versions.
- **No selective snapshots** in v1 — full world only. File Phase 16.25 if/when selective is needed (upstream had `ecs_snapshot_take_w_iter`; same pattern available).
- **No streaming snapshots** — one-shot take + restore.
- **No diff snapshots** — full snapshot only, no delta encoding.
- **No observer restoration** — handlers can't be serialised; user re-registers after restore.
- **No cross-world restore** — snapshot is in-place into the originating world (matches upstream `snapshot.h` lines 8-11).
- **No `*Table` pointer preservation** — memory is re-allocated on restore; users must reacquire any cached table pointers.

## Open decisions (locked in)

1. Binary-format stability — no compatibility guarantee in v1.
2. Cross-world restore — not allowed.
3. Observers — not serialised.
4. Restore atomicity — panic on concurrent `Reader` / `Writer`.

## Process

- Feature, not bug.
- Substantial; expect more than one iterate cycle.
- All `@`-references and line numbers verified against the current tree at commit `2aca57d` (v0.78.0).
- Target version: **v0.79.0**.
