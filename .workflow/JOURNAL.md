## iterate iteration 1 (2026-05-15)

Phase 16.52 complete: added iter.Seq/iter.Seq2 range-over-func adapters for queries. New API: All1-4 (typed, iter.Seq2), QueryAll/CachedQueryAll (bare, iter.Seq), QueryAllContext/CachedQueryAllContext (context-aware, iter.Seq2[ID,error]), plus Pair/Triple/Quad generic structs. All adapters drive Query.Iter()/Next()/Field[T] directly, preserving Inheritable/CanToggle semantics and supporting intra/inter-table break. 19 tests, godoc examples, benchmark (BenchmarkEach1_vs_All1), and docs (CHANGELOG v0.107.0, ROADMAP, README, docs/Queries.md, doc.go) all updated. go test ./... -race -count=3 clean; go vet and golangci-lint clean.

## iterate iteration 2 (2026-05-15)

Added missing TestCachedQueryAllContext_TimeoutFires test — mirrors TestQueryAllContext_TimeoutFires using CachedQueryAllContext, pre-fires a 1ms timeout, and verifies DeadlineExceeded is yielded. All three CachedQueryAllContext_* tests now present. go test ./... -race -count=3 clean.

