## iterate iteration 1 (2026-05-10)

Phase 3.1 query API implemented: NewQuery/Query/QueryIter/Field[T] in root flecs package; ColumnReflectSlice added to table package; 14 tests covering single-term, two-term AND, empty match, order independence, multiple tables, Field live-view mutation, type-mismatch/missing-id panics, tag components, GC safety, nil-world/empty-terms panics, Terms accessor, and smallest-set seeding correctness. All tests pass with -race -count=2; flecs coverage 97.4%, table 91.2%; golangci-lint clean.

