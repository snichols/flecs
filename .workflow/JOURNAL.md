## iterate iteration 1 (2026-05-10)

Implemented GetUp[T], HasUp, TargetUp traversal helpers in traversal.go with internal walkUp. Uses OwnsID (local-only) at each step, lazy seen map (zero alloc on self-hit), depth limit (maxTraversalDepth=64), dead-target guard. 17 tests in traversal_test.go covering single/multi-level, self-first, cycles, dead parent, depth limit, IsA and custom relationships, zero-size types, and zero-alloc self-hit. Added BenchmarkGetUp_SelfHit/Depth1/Depth5 to bench_test.go with baseline in BENCH.md. All tests pass (race -count=2), go vet clean, golangci-lint clean, coverage 97.0%.

