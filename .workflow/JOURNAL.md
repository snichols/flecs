## iterate iteration 1 (2026-05-11)

Implemented Reader/Writer scoped capability API (v0.15.0 breaking change): world.Read/Write scoped methods with RWMutex, cached &w.writeCapability/readCapability fields for 0 allocs/op, hook/observer callbacks updated to func(*Writer, ID, T), removed public Defer*/Readonly* API replaced by unexported internals + ForTest export helpers, added scope.go + scope_test.go, all tests pass -race -count=3, BenchmarkDeferBatchedAdds 0 allocs/op confirmed

## iterate iteration 2 (2026-05-11)

Deleted readonly.go entirely; removed w.readonly atomic.Bool field from world.go; removed all || w.readonly.Load() checks from world.go, id_ops.go, and value_ops.go; removed ReadonlyBeginForTest/ReadonlyEndForTest exports from export_test.go; removed five old readonly-mechanism tests from concurrent_test.go. All tests pass -race -count=3.

## iterate iteration 3 (2026-05-11)

Added QueryIter.Reader() and Writer() methods: added world *World field to QueryIter struct, populated in Query.Iter() and CachedQuery.Iter(); Reader() returns &it.world.readCapability, Writer() returns &it.world.writeCapability (zero allocs, reuses cached pointers). All tests pass -race -count=3.

## iterate iteration 4 (2026-05-11)

Added TestQueryIterReaderWriter and TestCachedQueryIterReaderWriter to scope_test.go, covering QueryIter.Reader() (query.go:372) and QueryIter.Writer() (query.go:377). Main package coverage moved from 94.9% to 95.0%, satisfying the ≥ 95% threshold. All tests pass -race -count=3.

