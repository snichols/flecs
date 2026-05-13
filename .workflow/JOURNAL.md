## iterate iteration 1 (2026-05-13)

Phase 15.19 Sparse component storage implemented: sparse.go (sparseSet, SetSparse, IsSparse, EachSparse, write/read/remove routing), 26 tests in sparse_test.go, marshal round-trip (sparse_components + sparse_data JSON fields), builtinEntityCount 35→36 (Sparse=index 34, Wildcard=35, Any=36), docs updated (CHANGELOG v0.51.0, ROADMAP, ComponentTraits.md, EntitiesComponents.md, README.md). All tests pass with -race -count=3, golangci-lint clean, sparse.go coverage 96.4% (>95%).

