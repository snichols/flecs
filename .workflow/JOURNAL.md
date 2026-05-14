## build iteration 1 (2026-05-13)

Phase 15.21 complete: Sparse/DontFragment split shipped as v0.53.0. DontFragment at index 35, Wildcard→36, Any→37. Three-way dispatch across all storage paths. 10 new tests, MIGRATING.md, CHANGELOG.md, ROADMAP.md, docs updated. All tests pass with -race -count=3, go vet, golangci-lint clean.

## iterate iteration 2 (2026-05-13)

Phase 15.21 complete: all tests pass (go test ./... -race -count=1), implementation verified. DontFragment trait at index 35, Wildcard→36, Any→37, builtinEntityCount=37. dont_fragment.go, dont_fragment_test.go (10 cases), MIGRATING.md, CHANGELOG v0.53.0 with BREAKING prefix, ROADMAP.md, docs all in place.

## iterate iteration 3 (2026-05-13)

Coverage requirements met. Fixed DontFragment coalescer bug in cmd_queue.go (batchForEntity missing dontFragmentPolicies checks). Added 7 tests to coverage_boost_test.go covering id_ops.go paths: singleton non-pair (line 249), DF AddID panic (line 55), DF/Sparse not-present removal in immediate hook context (lines 341-343, 353-355), dead-entity panics for AddID and SetPair in immediate mode (lines 42-43, 412-413). All touched files ≥ 95.0%: id_ops.go 95.2%, dont_fragment.go 100%, query.go 95.9%, cached_query.go 95.8%, world.go 95.0%, marshal.go 95.1%, sparse.go 96.7%. All tests pass with -race -count=1.

## iterate iteration 4 (2026-05-13)

Fixed 3 golangci-lint violations in coverage_boost_test.go: replaced bare recover() with _ = recover() on lines 124 and 165 (errcheck), and ran gofmt to fix formatting on line 605. All other requirements already satisfied from prior iterations.

## iterate iteration 5 (2026-05-13)

Fixed the only remaining verify gate gap: updated docs/README.md:165 to flip the DontFragment entry from "not yet ported in Go flecs" to shipped in v0.53.0, with API triad (SetDontFragment/IsDontFragment/w.DontFragment) and a link to ComponentTraits.md. All other acceptance criteria were already satisfied from prior iterations.

