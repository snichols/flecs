## iterate iteration 1 (2026-05-11)

Implemented Reader/Writer scoped capability API (v0.15.0 breaking change): world.Read/Write scoped methods with RWMutex, cached &w.writeCapability/readCapability fields for 0 allocs/op, hook/observer callbacks updated to func(*Writer, ID, T), removed public Defer*/Readonly* API replaced by unexported internals + ForTest export helpers, added scope.go + scope_test.go, all tests pass -race -count=3, BenchmarkDeferBatchedAdds 0 allocs/op confirmed

## iterate iteration 2 (2026-05-11)

Deleted readonly.go entirely; removed w.readonly atomic.Bool field from world.go; removed all || w.readonly.Load() checks from world.go, id_ops.go, and value_ops.go; removed ReadonlyBeginForTest/ReadonlyEndForTest exports from export_test.go; removed five old readonly-mechanism tests from concurrent_test.go. All tests pass -race -count=3.

