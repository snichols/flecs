## iterate iteration 1 (2026-05-14)

Implemented fixed per-term source (v0.73.0): WithSourceTerm builder, (Term).Source chained builder, Term.Src field, resolveFixedSourcePtr/buildFixedSourcePtrs helpers, dead-iter on missing required source, snapshot-at-iter-start contract, optional divergence (absent optional source yields FieldMaybe nil/false, entities still match), full CachedQuery support, 19 new tests at 95.0% package coverage, docs/Queries.md new section, CHANGELOG.md v0.73.0 entry, ROADMAP.md bumped.

## iterate iteration 2 (2026-05-14)

Coverage gap closed: added 6 targeted tests in query_fixed_source_test.go to cover previously unreachable branches — TermOr+fixed-source panic, traversal+fixed-source panic via direct Term construction, updateOptionalPresenceSparse skip (pure-DontFragment query with optional fixed-source), updateOptionalPresenceMixed skip (mixed archetype+DontFragment query with optional fixed-source), Field/FieldMaybe nil-ptr paths for zero-size tag fixed-source, and Field() panic for absent optional fixed-source. Package coverage: 94.9% → 95.0%. All tests pass with -race -count=3, go vet clean, golangci-lint clean.

