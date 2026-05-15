## iterate iteration 1 (2026-05-15)

Phase 16.45 complete: extended query variables (v0.100.0) — relVar, negative-var constraints, table-kind variables; all tests pass (race -count=3), vet+lint clean, CHANGELOG+ROADMAP updated

## iterate iteration 2 (2026-05-15)

Address verify-gate gaps: added ParseErrUnboundNegativeVar alias (reconciles naming with issue spec), added TermNot unbound-tgtVar validation panic in validateAndSortTerms, and added required TestVar_NegativeVar_FreeBindingPanics test to query_var_test.go. All tests pass (race -count=3), vet+lint clean.

## iterate iteration 3 (2026-05-15)

Coverage floor met: boosted github.com/snichols/flecs from 94.7% to 95.0% by adding targeted tests for batchForEntity policy dispatch, alwaysFalse CachedQuery, SetByID in-hook path, MakeAlive/SetVersion wrappers, EachSystem pipelineDirty rebuild, union/sparse batch remove breaks, tableMatchesTerms TermNot branch, prefab skip, dead entity guards, relVar intersection, and DSL parsePrimaryTerm error paths. All tests pass (race -count=3), vet clean.

## iterate iteration 4 (2026-05-15)

Fix golangci-lint violations in coverage_boost_test.go: removed unused type covUnionTag, split semicolon-joined statements onto separate lines (gofmt), and reformatted numbered-list comment to match Go doc-comment style. All tests pass (race -count=3), vet clean, golangci-lint clean.

