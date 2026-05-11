## iterate iteration 1 (2026-05-10)

Implemented NOT and Optional query term kinds. Added TermKind/Term types, With/Without/Maybe constructors, NewQueryFromTerms, NewCachedQueryFromTerms, TermsFull(), FieldMaybe[T], and query_terms_test.go covering all required test cases. All tests pass (race -count=2), go vet clean, golangci-lint clean, coverage 97.1%.

