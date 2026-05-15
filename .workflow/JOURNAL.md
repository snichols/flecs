## iterate iteration 1 (2026-05-15)

Phase 16.57 implemented: snapshot schema-version tagging and migration registry (v0.112.0). New migration.go with MigrationContext IR, ErrMissingMigration/ErrSnapshotNewerThanWorld errors, World.SetSchemaVersion/SchemaVersion/RegisterMigration API. Binary format bumped to v3 (4-byte schema version prefix in payload); v2 snapshots still load as schema version 0. Atomic migration path: decode+migrate fully into IR before clearUserState. 20 tests in snapshot_migration_test.go, example in snapshot_migration_example_test.go, docs/SnapshotMigration.md. All checks clean: go vet, golangci-lint, go test ./... -race -count=3.

