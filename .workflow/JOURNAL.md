## iterate iteration 1 (2026-05-11)

Deleted World.W/R/NewEntity escape-hatches; migrated all ~1,040 call sites across 41 test/example files to world.Write/world.Read scopes; fixed doc.go and CHANGELOG.md; added exclusive_access_norace_test.go; restored coverage to 95.0%. All tests pass with -race -count=3; grep for w.W()/w.R() returns 0 hits.

