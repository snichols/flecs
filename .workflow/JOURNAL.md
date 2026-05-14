## iterate iteration 1 (2026-05-13)

Union relationship trait fully implemented (v0.54.0, Phase 15.22): unionStore side map, no archetype transitions, pure-union query iteration, batchForEntity bypass, Reader.HasID/OwnsID union branch, marshal round-trip, hook integration, conflict detection, 22 tests all passing, docs updated

## iterate iteration 2 (2026-05-14)

coverage boosted to 95.0% — 30 new tests across union_test.go and coverage_boost_test.go covering all targeted branches; all tests pass with -race -count=3

## iterate iteration 3 (2026-05-14)

Added two missing README.md feature index rows: DontFragment component trait (v0.53.0) and Union relationship trait (v0.54.0), including the "trait system feature-complete vs upstream C flecs as of v0.54.0" note. These were the only gaps identified by the verify gate; all other checks (go vet, tests -race -count=3, coverage 95.0%, docs) had already passed.

