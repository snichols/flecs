## build iteration 1 (2026-05-13)

Phase 15.21 complete: Sparse/DontFragment split shipped as v0.53.0. DontFragment at index 35, Wildcard→36, Any→37. Three-way dispatch across all storage paths. 10 new tests, MIGRATING.md, CHANGELOG.md, ROADMAP.md, docs updated. All tests pass with -race -count=3, go vet, golangci-lint clean.

## iterate iteration 2 (2026-05-13)

Phase 15.21 complete: all tests pass (go test ./... -race -count=1), implementation verified. DontFragment trait at index 35, Wildcard→36, Any→37, builtinEntityCount=37. dont_fragment.go, dont_fragment_test.go (10 cases), MIGRATING.md, CHANGELOG v0.53.0 with BREAKING prefix, ROADMAP.md, docs all in place.

