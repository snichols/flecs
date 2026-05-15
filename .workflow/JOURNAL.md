## iterate iteration 1 (2026-05-15)

Phase 16.54 implemented: iter.Seq traversal adapters for all non-query traversal primitives. New file iter_seq_traversal.go with Reader.Entities/EntitiesContext/Children/Prefabs/Systems (method-style, wrapping bool-callback Each* directly) and package-level Union/Sparse[T]/ByID/ByIDContext (driving underlying dense/compIndex machinery for break support). 54 tests in iter_seq_traversal_test.go + 7 runnable examples in iter_seq_traversal_example_test.go. Coverage maintained at 95.0%. CHANGELOG/ROADMAP/README/doc.go and 5 docs files updated. All checks clean: go vet, golangci-lint, go test -race -count=3.

