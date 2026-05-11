## iterate iteration 1 (2026-05-11)

Implemented Reader/Writer scoped capability API (v0.15.0 breaking change): world.Read/Write scoped methods with RWMutex, cached &w.writeCapability/readCapability fields for 0 allocs/op, hook/observer callbacks updated to func(*Writer, ID, T), removed public Defer*/Readonly* API replaced by unexported internals + ForTest export helpers, added scope.go + scope_test.go, all tests pass -race -count=3, BenchmarkDeferBatchedAdds 0 allocs/op confirmed

