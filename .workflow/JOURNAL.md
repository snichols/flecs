## iterate iteration 1 (2026-05-12)

Implemented Phase 15.5 Transitive relationship trait (v0.37.0): transitive.go with SetTransitive/IsTransitive/applyTransitivePolicy/w.Transitive()/transitiveWalk/transitiveTableMatches, built-in entity at index 20 (user entities now start at 21), bare-tag handler in addIDImmediate, transitive pair matching in matchesTable (uncached) and tryMatchTable (cached) with seed-skip for transitive terms, 10 test cases + BenchmarkTransitiveQuery_ChainLen10 in transitive_test.go, docs updates across ComponentTraits/Relationships/Queries/README, CHANGELOG v0.37.0 entry, ROADMAP Phase 15.5 marked shipped. All tests pass (go test ./... -race -count=3), go vet clean, golangci-lint clean, coverage 95.0%.

