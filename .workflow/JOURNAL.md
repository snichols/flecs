## iterate iteration 1 (2026-05-11)

Phase 9.4: OR query terms fully implemented. Added TermOr (kind=3), Or(id) constructor, orGroups [][]ID on Query/CachedQuery/QueryIter, updated validateAndSortTerms with Or-specific validation (zero-ID, within-group duplicates, cross-kind duplicates), updated matchesTable and tryMatchTable for OR-group evaluation, extended FieldMaybe to accept TermOr terms, and updated TermKind.String(). Sort order: And, Not, Or-groups, Optional. 13 new tests in query_terms_test.go. go test -race -count=2, go vet, golangci-lint all clean; coverage 96.0%. CHANGELOG, README, and doc.go updated.

