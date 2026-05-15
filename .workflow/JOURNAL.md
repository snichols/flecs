## iterate iteration 1 (2026-05-15)

Phase 16.53 streaming snapshot I/O complete (v0.108.0). Refactored binWriter to write to io.Writer with sticky err/accumulated n. Added WriteTo, TakeSnapshotTo, TakeSnapshotToContext, ReadSnapshotFrom, RestoreSnapshotFrom, RestoreSnapshotFromContext. Bytes() is now a thin wrapper around WriteTo. 19 tests + benchmark + runnable example. All docs updated (Snapshots.md, CHANGELOG, ROADMAP, README). go vet, golangci-lint, go test ./... -race -count=3 all clean.

## iterate iteration 2 (2026-05-15)

Coverage pushed to 95.0% — verify gate should now pass. Added 27 targeted tests covering: 8 bw.err checks in snapshotWritePayloadContext + binWriter.u8 sticky-error guard (failOnNthCallWriter loop), CachedQuery.EachContext outer/mid-loop ctx-cancel, Query.EachContext mid-loop ctx-cancel, CachedQuery.tryMatchTable after Close, All1 shared-branch break, clearUserState singletonInstances/writeOnce/orderedChildren map bodies, serializeOrderedChildren internal/empty continues, serializeUnionState internal-relKey continue. go vet, golangci-lint, go test ./... -race -count=3 all clean.

