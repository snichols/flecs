## iterate iteration 1 (2026-05-11)

Implemented coalescing deferred command queue: cmd.go (tagged-union cmd struct + cmdKind), cmd_arena.go (bump arena with 1KiB pages + sync.Pool recycling), cmd_queue.go (per-entity intrusive list + two-pass batchForEntity coalescer + dispatch). Wired into world.go (commitBatch, deferred paths for Set/Remove/Delete/AddID/RemoveID/SetPair/SetByID), defer.go (queue swap under mutex), id_ops.go, value_ops.go. Added defer_coalesce_test.go (11 tests) and bench entries. BenchmarkDeferSingleSet: 0 allocs/op. All tests pass under -race -count=3; coverage 95.2%; go vet + golangci-lint clean. CHANGELOG.md and ROADMAP.md updated for v0.14.

## iterate iteration 2 (2026-05-11)

Fixed BenchmarkDeferBatchedAdds ≥10× speedup requirement. Replaced per-call map/sort allocations in batchForEntity with reusable sorted-slice scratch buffers (cmdQueue.scratch1/2/3) and a sort-merge diff algorithm. Added sigKeyLookup using unsafe.String for zero-allocation table lookup in commitBatch hot path. Also fixed scratch1 alias bug (commitBatch now copies newSig before table.New). Result: 7,200 ns/op, 0 allocs/op (~15× speedup vs v0.13.0 baseline of 111,897 ns/op). BenchmarkDeferSingleSet 0 allocs/op, BenchmarkSetExistingComponent no regression. All tests pass under -race -count=5; coverage 95.1%; go vet + golangci-lint clean. Added TestDeferRemoveNonExistent and TestDeferCoalesceToEmpty for coverage of new code paths. CHANGELOG.md updated.

