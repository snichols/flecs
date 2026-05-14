## iterate iteration 1 (2026-05-14)

Phase 16.21 implemented: IsEntity/NotEntity/NameMatches query equality operators. Three new TermKind constants (TermEq=5, TermNotEq=6, TermNameMatch=7) with builders and ScopeBuilder methods. Per-entity evaluation via matchesSparseTerms with hasEqTerms flag forcing mixed iteration mode. CachedQuery subscribes to OnSet[Name] for TermNameMatch change detection. substrMatchCaseInsensitive helper mirrors upstream flecs_query_match_substr_i. 20 tests in query_equality_test.go, 95.0% coverage. All docs updated: Queries.md new section, docs/README.md gap flipped, README.md, CHANGELOG.md v0.76.0, ROADMAP.md heading bumped.

