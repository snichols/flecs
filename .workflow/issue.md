## Goal

Add a user-facing **schema-version** tag to binary snapshots plus a **migration registry**, so a game/simulation can ship a new release with changed component layouts and still load snapshots persisted by older releases. This is the seventh post-port-completion Go-idiomatic value-add (after Phase 16.51–16.56) and addresses a real production-persistence concern: today a snapshot carries only the internal codec **format** version, not the shape of the user's component structs, so changing `Position{X,Y}` → `Position{X,Y,Z}` and loading an old snapshot corrupts or fails to decode.

Target version **v0.112.0**, phase number **16.57**.

### The central design constraint

Migrations must run on **decoded snapshot data**, not live world state, keyed by **component NAME string** (the Go type may no longer exist for a dropped/renamed component). The migration IR sits at the snapshot-decode seam. Verified seam in `@snapshot.go`:

- `deserializeTable` (snapshot.go:1076) reads per-component column blocks: for each `compID` in the table signature it reads `elemSize uint32` then a raw byte block of `elemSize * rowCount`, then copies into `t.ColumnBasePtr(compID)`. The IR must intercept between the raw-block read and the `ColumnBasePtr` copy.
- `deserializeComponents` (snapshot.go:854) already builds a name↔TypeInfo map and is where a snapshot component name absent from the target registry currently errors — migration rename/drop must run **before** this validation so renamed-away names don't trip it.
- Component name↔ID mapping is `@internal/component/registry.go` (`LookupByID`, `IDs`, name is `info.Name`).

### Distinction the issue must keep separate

- **Codec format version** — internal; `snapshotFormatVersion = uint32(2)` (`snapshot.go:23`), written big-endian into header bytes `[4:8]` by three sites: `(*Snapshot).WriteTo` (`snapshot_stream.go:24`), `TakeSnapshotToContext` (`snapshot_stream.go:75`), and read/validated by `LoadSnapshot` (`snapshot.go:158`) and `ReadSnapshotFrom` (`snapshot_stream.go:111`). Not user-facing.
- **User schema version** — new; declared via `World.SetSchemaVersion(uint32)`, persisted in the snapshot, drives migrations.

Verify the format-version mechanism is not conflated with the new schema-version field. The 16-byte file header today is `magic[4] + formatVersion[4 BE] + worldID[8 LE]` with no spare bytes; the schema version must be carried either by bumping `snapshotFormatVersion` to 3 and adding the field to the payload/header, or via a header flag — pick the least-invasive discriminator and ensure pre-phase snapshots (format v2, no schema field) still load and read as schema version 0.

### API surface

```go
func (w *World) SetSchemaVersion(v uint32)   // persisted into every snapshot; default 0
func (w *World) SchemaVersion() uint32

// Migrations form a chain: v3→v4, v4→v5 to load a v3 snapshot into a v5 world.
func (w *World) RegisterMigration(fromVersion, toVersion uint32, migrate MigrationFunc)

type MigrationFunc func(m *MigrationContext) error
```

`MigrationContext` is the neutral IR over decoded-but-not-yet-applied snapshot data, keyed by component name string:

```go
type MigrationContext struct { /* opaque */ }
func (m *MigrationContext) EachComponent(name string, fn func(rec *ComponentRecord) error) error
func (m *MigrationContext) RenameComponent(oldName, newName string) error
func (m *MigrationContext) DropComponent(name string) error
func (m *MigrationContext) AddComponent(name string, value []byte, where func(entityTags []string) bool) error

type ComponentRecord struct {
    Entity uint64
    Raw    []byte
}
func (r *ComponentRecord) SetRaw(b []byte)
```

The precise IR shape is a design call — keep it minimal but sufficient for rename/drop/add/byte-rewrite, sitting at the `snapshot.go` component-block decode seam described above.

### Restore flow with migration

1. Decode header → persisted `schemaVersion = S`
2. Read world's `SchemaVersion = C`
3. `S == C` → normal restore, **zero overhead** (verify fast-path; benchmark within 2% of plain restore)
4. `S < C` → build chain S→S+1→…→C from the registry; any missing link → `ErrMissingMigration` identifying the gap
5. Apply migrations in ascending order to the decoded IR
6. Materialize entities from the migrated IR into the live world
7. `S > C` → `ErrSnapshotNewerThanWorld` (forward-compat unsupported; documented)

**Headline correctness requirement:** a migration error during restore must leave the target world **exactly untouched** — decode + migrate fully into the IR, *then* materialize; never half-apply. Note `snapshotDeserializeContext` (`snapshot.go:763`) calls `clearUserState(w)` *before* deserializing tables; the migration restore path must defer `clearUserState` until after decode+migrate succeed, or otherwise guarantee no observable partial state on migration error.

### Scope recommendation: binary only for v1

`@marshal.go` carries its own independent JSON format version (`Version: 1`, marshal.go:693/750) distinct from the binary codec. Recommend binary-only schema-version + migration for v1; JSON-path migration explicitly **deferred and documented** (do not silently skip). The streaming path (`@snapshot_stream.go`, Phase 16.53/16.55 `RestoreSnapshotFrom(io.Reader)`) must support migration end-to-end since it shares the decode path.

### Back-compat

Snapshots persisted before this phase (format v2, no schema-version field) must continue to load and be treated as schema version 0. Discriminate via the format-version bump or a header flag. The wire-format change must be documented in CHANGELOG but must NOT break old-snapshot loading. Synthesize or fixture a pre-phase snapshot in the tests to prove this.

## Required tests

New file `@snapshot_migration_test.go`:

**Version tagging**
- `TestSchemaVersion_DefaultZero` — fresh world reports 0
- `TestSchemaVersion_PersistedInSnapshot` — set v5, snapshot, decode header, verify 5
- `TestSchemaVersion_OldSnapshotReadsAsZero` — pre-phase snapshot (synthesized/fixture) decodes as version 0

**No-migration fast path**
- `TestMigration_SameVersion_NoMigrationRuns` — S==C; spy migration func never invoked; state identical to plain restore
- `TestMigration_SameVersion_ZeroOverhead` — benchmark: migration-registered-but-not-triggered within 2% of plain restore

**Single migration**
- `TestMigration_RenameComponent` — v1 "Pos" → v2 "Position"
- `TestMigration_DropComponent` — v1 "Debug" dropped in v2
- `TestMigration_AddComponent` — v2 adds required "Health" with default onto old entities
- `TestMigration_RewriteBytes_StructGrew` — Position{X,Y} → {X,Y,Z}; append zeroed Z; correct 3-field values

**Migration chain**
- `TestMigration_ChainV1ToV4` — v1→v2, v2→v3, v3→v4 all run in order on a v1 snapshot into v4 world
- `TestMigration_ChainOrder_Deterministic` — strict ascending order
- `TestMigration_MissingLink_Errors` — v1→v2 and v3→v4 registered, v2→v3 missing → `ErrMissingMigration` naming the v2→v3 gap
- `TestMigration_ErrorInMigrationFunc_Aborts` — migration returns error; restore aborts; world unchanged

**Edge cases**
- `TestMigration_SnapshotNewerThanWorld_Errors` — S=5, C=3 → `ErrSnapshotNewerThanWorld`
- `TestMigration_EmptyWorld_VersionedSnapshot` — empty world, versioned snapshot, chain still validated
- `TestMigration_StreamingRestore_WithMigration` — `RestoreSnapshotFrom(io.Reader)` + chain end-to-end
- `TestMigration_JSONSnapshot_Migration` — assert JSON path does NOT carry schema version in v1 and document the deferral (scope A)

**Atomicity**
- `TestMigration_FailurePreservesWorld` — migration error during restore leaves target world exactly as pre-restore (no half-applied entities, no cleared user state)

## Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run ./...` clean
- `go test ./... -race -count=3` clean
- Coverage ≥ 95% (current baseline)
- All existing snapshot tests (Phase 16.24 `@snapshot_test.go`, streaming `@snapshot_stream_test.go`) pass unchanged
- Old (pre-phase) snapshot bytes still load (synthesized or fixtured)
- No-migration path benchmark within 2% of plain restore

## Documentation update matrix

- New `@docs/SnapshotMigration.md` — full reference: schema vs format version, registering migrations, the MigrationContext IR, chain semantics, error modes, back-compat with unversioned snapshots, JSON-path deferral note, worked example (Position gains a Z field across two releases)
- `@docs/Snapshots.md` — cross-link; note the new header/payload field and that the binary format disclaimer section already warns about versioning
- New runnable example `@snapshot_migration_example_test.go` — realistic two-release migration
- `@CHANGELOG.md` — v0.112.0 entry; explicitly note the snapshot wire-format change and that old snapshots remain loadable
- `@ROADMAP.md` — add Phase 16.57 to Shipped (note the existing "All ROADMAP items shipped" line at ROADMAP.md:116 documents the post-port value-add stream; keep that framing consistent)
- `@README.md` — persistence/migration feature row

## Non-goals

- Automatic schema-diff detection (user declares versions + writes migrations explicitly)
- Downgrade migrations (newer→older) — forward-only; newer-than-world is an error
- JSON-snapshot migration in v1 (binary only; JSON deferred and documented) unless trivially free
- Live-world migration (migrating a running world's entities in place) — restore-time only
- Schema registry / reflection-based auto-migration — explicit MigrationFunc only
- Cross-language snapshot interchange

## Constraints

- @snapshot.go — Phase 16.24 binary codec. Header is `magic[4] + formatVersion[4 BE] + worldID[8 LE]` (`snapshotFormatVersion = uint32(2)`, snapshot.go:23). The migration IR seam is `deserializeTable` (snapshot.go:1076) per-component column-block decode and `deserializeComponents` (snapshot.go:854) name↔registry validation. `snapshotDeserializeContext` (snapshot.go:763) calls `clearUserState` before table decode — the migration path must not clear user state until decode+migrate succeed (atomicity requirement).
- @snapshot_stream.go — Phase 16.53/16.55 streaming. Three header sites duplicate the format-version write/read (`WriteTo` line 24, `TakeSnapshotToContext` line 75, `ReadSnapshotFrom` line 111). Schema version must thread through all of them; migration must work via `RestoreSnapshotFrom(io.Reader)`.
- @internal/component/registry.go — component name↔ID mapping (`LookupByID`, `IDs`, `info.Name`). Migrations key on name because the Go type may be gone for dropped/renamed components.
- @marshal.go — JSON snapshot path carries its own independent format version (`Version: 1`, lines 693/750), distinct from the binary codec. Recommend binary-only v1; document the JSON deferral explicitly (do not silently skip).
- @world.go — `World` struct (line 41); add `schemaVersion` and migration registry fields here. Restore entry points and `registry`/`cachedQueries` fields are reset post-restore (snapshot.go:841-848 pattern).
- @docs/Snapshots.md — snapshot manual; existing "Stability disclaimer" section already warns the binary format is versioned and may break old snapshots — the back-compat guarantee here (old snapshots still load) is a deliberate strengthening that must be documented.
- @snapshot_test.go @snapshot_stream_test.go — existing snapshot test suites must pass unchanged.
- Label is `snichols/queued` (value-add, NOT a bug).
- VISION.md exists but is an unfilled template — no vision principles apply to this goal.
