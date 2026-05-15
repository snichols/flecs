# Snapshot Migration

Snapshot migration lets you load binary snapshots that were saved with an older
component layout and automatically transform the data to match the current world.
This is essential for games and simulations that persist world state to disk and
need to load saves from previous versions of the application.

## Overview

Every world has a **schema version** — a `uint32` that you increment whenever you
change component layouts in a way that is incompatible with existing snapshots.
When a snapshot is loaded, if its schema version is lower than the world's, the
registered **migration chain** is applied to transform the data before it is
materialized into the live world.

```
Snapshot (schema v1) ──► migrate v1→v2 ──► migrate v2→v3 ──► live world (schema v3)
```

## API

```go
// Set and query the world's current schema version.
w.SetSchemaVersion(v uint32)
w.SchemaVersion() uint32

// Register a migration step from one schema version to the next.
w.RegisterMigration(fromVersion, toVersion uint32, fn flecs.MigrationFunc)

// Query a snapshot's embedded schema version.
snap.SchemaVersion() uint32
```

## MigrationContext

The `MigrationFunc` receives a `*MigrationContext` that exposes the decoded
snapshot data as component-name-keyed records. Mutations to the context are
applied before the data is materialized into the live world.

```go
type MigrationFunc func(m *MigrationContext) error
```

Available operations:

| Method | Description |
|---|---|
| `m.RenameComponent(old, new string)` | Rename all records for a component |
| `m.DropComponent(name string)` | Remove all records for a component |
| `m.AddComponent(name string, value []byte, where func(tags []string) bool)` | Add a component with default value to matching entities |
| `m.EachComponent(name string, fn func(*ComponentRecord) error)` | Iterate and mutate per-entity records |

`ComponentRecord.SetRaw(b []byte)` replaces raw component bytes for one entity.

## Naming convention

Component names in `MigrationContext` are the fully-qualified Go type names as
stored in the snapshot's component registry. For a type `Pos` defined in
`package flecs_test`, the name is `"flecs_test.Pos"`.

Use `ComponentInfo(w, id).Name` to discover the name of a registered component at
runtime if you are unsure.

## Example: two-release migration

### Release 1

```go
type Player struct {
    HP int32
}

w := flecs.New()
w.SetSchemaVersion(1)
flecs.RegisterComponent[Player](w)
```

### Release 2 — HP renamed to Health, Level field added

```go
type Player struct {
    Health int32
    Level  int32
}

w := flecs.New()
w.SetSchemaVersion(2)
flecs.RegisterComponent[Player](w)

w.RegisterMigration(1, 2, func(m *flecs.MigrationContext) error {
    // The package path changes because the type is the same name but
    // may be in a different package, or you can use the old name if
    // the package hasn't changed.
    //
    // In practice both are "mypackage.Player" — just rename in-place.
    // (No rename needed here since the type name is the same.)

    // Expand raw bytes: old HP (4 bytes) → new Health+Level (8 bytes).
    return m.EachComponent("mypackage.Player", func(rec *flecs.ComponentRecord) error {
        newRaw := make([]byte, 8)
        copy(newRaw[:4], rec.Raw) // Health = old HP
        // Level defaults to 1 (LE int32 = [1,0,0,0]).
        newRaw[4] = 1
        rec.SetRaw(newRaw)
        return nil
    })
})
```

### Loading the old snapshot

```go
if err := w.RestoreSnapshotFrom(reader); err != nil {
    log.Fatalf("migration failed: %v", err)
}
```

## Chain migrations (v1 → v2 → v3 → …)

To migrate a v1 snapshot into a v3 world, register two consecutive steps:

```go
w.RegisterMigration(1, 2, migrateV1toV2)
w.RegisterMigration(2, 3, migrateV2toV3)
```

If any step is missing (e.g. v2→v3 is absent), the restore returns
`flecs.ErrMissingMigration`.

## Error handling

| Error | Meaning |
|---|---|
| `flecs.ErrMissingMigration` | A required migration step is not registered |
| `flecs.ErrSnapshotNewerThanWorld` | Snapshot schema > world schema (downgrade not supported) |
| Any error returned by `MigrationFunc` | Migration aborted; world state preserved |

### Atomicity guarantee

Migration is **all-or-nothing**. The snapshot blob is fully decoded and the entire
migration chain is applied to an in-memory intermediate representation before any
change is made to the live world. If any step fails — including individual
`MigrationFunc` errors and registry-validation failures — the world is left exactly
as it was before the restore call.

## Format versioning

Binary snapshots written by v0.112.0+ are **format v3**. The 16-byte header is
followed by a 4-byte little-endian schema version, then the payload sections.

Format v2 snapshots (written by earlier releases) are still accepted; their schema
version defaults to 0.

```
Header (16 bytes):
  magic[4]         = {0xF1, 0xEC, 0x53, 0x00}
  formatVersion[4] = 3  (big-endian)
  worldID[8]       = world identity token (little-endian)

Payload:
  schemaVersion[4] = user schema version (little-endian)  ← new in v3
  ... sections 1–10 (same as v2) ...
```

## Streaming API compatibility

Migration works transparently with the streaming API:

```go
// Write (schema embedded automatically).
w.TakeSnapshotTo(out io.Writer)

// Read + migrate.
w2.RestoreSnapshotFrom(in io.Reader)
```

## Limitations

- **Binary snapshots only.** JSON (`MarshalJSON` / `UnmarshalJSON`) does not carry
  a schema version and does not trigger migration.
- **Bitsets and parent-column data are not preserved across migrations.** If your
  entities use `CanToggle` or parent-storage policies, those are reconstructed as
  zero/default after migration.
- **No cross-version registry validation during migrate.** Unknown component names
  in the snapshot are silently ignored (they map to empty string in the ID→name
  lookup). If a migration renames them to a registered name, they appear; otherwise
  they disappear.

## See also

- [docs/Snapshots.md](Snapshots.md) — binary snapshot format, streaming API, and
  serialization guarantees.
