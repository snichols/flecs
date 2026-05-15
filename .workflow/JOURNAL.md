## iterate iteration 1 (2026-05-15)

Phase 16.53 streaming snapshot I/O complete (v0.108.0). Refactored binWriter to write to io.Writer with sticky err/accumulated n. Added WriteTo, TakeSnapshotTo, TakeSnapshotToContext, ReadSnapshotFrom, RestoreSnapshotFrom, RestoreSnapshotFromContext. Bytes() is now a thin wrapper around WriteTo. 19 tests + benchmark + runnable example. All docs updated (Snapshots.md, CHANGELOG, ROADMAP, README). go vet, golangci-lint, go test ./... -race -count=3 all clean.

