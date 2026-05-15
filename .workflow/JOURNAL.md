## iterate iteration 1 (2026-05-15)

Phase 16.51 complete: Go-idiomatic context.Context cancellation (v0.106.0). Added ProgressContext, RunSystemContext, TakeSnapshotContext, RestoreSnapshotContext, Query.EachContext, CachedQuery.EachContext, MarshalJSONContext, Snapshot.Partial flag, REST 499 on client disconnect. 21 new tests with goleak coverage. Full docs: new Cancellation.md plus updates to Systems.md, Snapshots.md, Queries.md, FlecsRemoteApi.md, CHANGELOG.md, ROADMAP.md, README.md. go test ./... -race -count=1 clean.

## iterate iteration 2 (2026-05-15)

Fixed delegation requirement: RunSystem, Query.Each, and CachedQuery.Each now delegate to their Context variants with context.Background() instead of having independent implementations. All tests pass with -race -count=1.

