## iterate iteration 1 (2026-05-13)

Phase 15.20 (v0.52.0): Sparse query integration complete. Three-mode iterator (all-sparse/mixed/all-archetype), Field[T]/FieldMaybe[T] sparse branches, Not/Optional on sparse terms, CachedQuery.Changed() via sparseSet version counter, pure-sparse cached query shortcut. 13 new tests in sparse_test.go, all tests pass -race -count=1. Docs: CHANGELOG v0.52.0, ROADMAP, ComponentTraits.md, Queries.md new § Sparse-aware queries, docs/README.md, README.md.

## iterate iteration 2 (2026-05-13)

Coverage gap resolved: added 4 new test functions and sparseArch2 helper type to sparse_test.go. query.go coverage raised from 92.7% to 95.8% (461/481 statements), clearing the ≥95.0% gate. sparse.go remains at 96.6%. Tests cover: Count()/Table()/Entities() API on sparse iterators, nextMixed non-matching-table branch (line 624), matchesTable sparse-Not skip (line 785), updateOptionalPresenceMixed (lines 692-706, all branches including cleanup loop), and IsFieldSelf continue/panic paths. All tests pass -race -count=1, go vet clean.

