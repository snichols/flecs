## iterate iteration 1 (2026-05-15)

Phase 16.57 implemented: snapshot schema-version tagging and migration registry (v0.112.0). New migration.go with MigrationContext IR, ErrMissingMigration/ErrSnapshotNewerThanWorld errors, World.SetSchemaVersion/SchemaVersion/RegisterMigration API. Binary format bumped to v3 (4-byte schema version prefix in payload); v2 snapshots still load as schema version 0. Atomic migration path: decode+migrate fully into IR before clearUserState. 20 tests in snapshot_migration_test.go, example in snapshot_migration_example_test.go, docs/SnapshotMigration.md. All checks clean: go vet, golangci-lint, go test ./... -race -count=3.

## iterate iteration 2 (2026-05-15)

Closed coverage gap: added 40 targeted tests to snapshot_migration_test.go covering previously uncovered branches in migration.go (MigrationContext API edge cases, parse-error paths for all binary sections 1-7, context cancellation, size-mismatch, tag component). Coverage for github.com/snichols/flecs rose from 94.5% to 95.0%. All checks clean: go vet, golangci-lint, go test -race -count=3.

