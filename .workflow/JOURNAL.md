## iterate iteration 1 (2026-05-12)

Phase 15.7 Reflexive trait fully implemented (v0.39.0). New reflexive.go with SetReflexive/IsReflexive/applyReflexivePolicy/reflexiveTableMatches. Built-in Reflexive entity at index 21 (Wildcard→22, Any→23, user entities→24). HasID extended for self-pair (gated on target==entity, zero cost for non-self). Query matchers in query.go and cached_query.go handle reflexive self-match and Reflexive+Transitive composition. IsA bootstrapped reflexive. 9 test cases in reflexive_test.go. Docs updated: Relationships.md, ComponentTraits.md, README.md, CHANGELOG.md v0.39.0, ROADMAP.md Phase 15.7 shipped. go vet/golangci-lint clean, go test ./... -race -count=3 clean, coverage 95.0%.

