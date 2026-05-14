## iterate iteration 1 (2026-05-14)

Phase 16.22: AndFrom/OrFrom/NotFrom query operators implemented — v0.77.0. Added TermAndFrom/TermOrFrom/TermNotFrom (kinds 8/9/10), builder functions, snapshot expansion in validateAndSortTerms, alwaysFalse flag for empty OrFrom, DontInherit bootstrap for Disabled tag, 15 tests in query_from_test.go (95.0% coverage), and full doc updates (Queries.md, docs/README.md, README.md, CHANGELOG.md, ROADMAP.md). All tests pass with -race -count=3.

