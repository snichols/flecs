## iterate iteration 1 (2026-05-13)

Phase 15.20 (v0.52.0): Sparse query integration complete. Three-mode iterator (all-sparse/mixed/all-archetype), Field[T]/FieldMaybe[T] sparse branches, Not/Optional on sparse terms, CachedQuery.Changed() via sparseSet version counter, pure-sparse cached query shortcut. 13 new tests in sparse_test.go, all tests pass -race -count=1. Docs: CHANGELOG v0.52.0, ROADMAP, ComponentTraits.md, Queries.md new § Sparse-aware queries, docs/README.md, README.md.

