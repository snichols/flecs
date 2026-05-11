## iterate iteration 1 (2026-05-11)

Implemented coalescing deferred command queue: cmd.go (tagged-union cmd struct + cmdKind), cmd_arena.go (bump arena with 1KiB pages + sync.Pool recycling), cmd_queue.go (per-entity intrusive list + two-pass batchForEntity coalescer + dispatch). Wired into world.go (commitBatch, deferred paths for Set/Remove/Delete/AddID/RemoveID/SetPair/SetByID), defer.go (queue swap under mutex), id_ops.go, value_ops.go. Added defer_coalesce_test.go (11 tests) and bench entries. BenchmarkDeferSingleSet: 0 allocs/op. All tests pass under -race -count=3; coverage 95.2%; go vet + golangci-lint clean. CHANGELOG.md and ROADMAP.md updated for v0.14.

