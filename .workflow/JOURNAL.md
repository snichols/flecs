## iterate iteration 1 (2026-05-15)

Phase 16.43 complete: OnTableEmpty and OnTableFill observer events implemented. Two new built-in event entities at indices 44–45 shift all subsequent indices by +2 (user entities now start at index 75). New API: OnTableFill/OnTableEmpty/OnTableFillWithOptions/OnTableEmptyWithOptions with multi-term filter support (WithQuery) and yield_existing. Fire-path wired in migrate, migrateArchetypeOnly, and commitBatch via dispatchTableObservers. 17 tests in observer_table_pop_test.go. All docs updated: ObserversManual.md, docs/README.md, CHANGELOG.md, ROADMAP.md, README.md. go test ./... -race -count=3 clean, go vet clean, golangci-lint clean.

