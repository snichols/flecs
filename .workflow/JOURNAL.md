## iterate iteration 1 (2026-05-14)

Phase 16.20 query scopes implemented and shipped as v0.75.0. WithoutScope(buildFn func(*ScopeBuilder)) Term + ScopeBuilder with With/Without/Or/Maybe/Source/WithoutScope methods. Table-level fast path for simple scopes; per-entity slow path for OR-groups/nested/sparse/DontFragment/Union/fixed-source. CachedQuery participates in change tracking for scope-internal sparse versions. 22 tests in query_scope_test.go, package coverage 95.0%. go test -race -count=3 clean, go vet clean, golangci-lint clean. Docs updated: docs/Queries.md §Query scopes, docs/README.md gap entry flipped, README.md, CHANGELOG.md v0.75.0, ROADMAP.md. Closes #221.

