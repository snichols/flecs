## iterate iteration 1 (2026-05-15)

Phase 16.45 complete: extended query variables (v0.100.0) — relVar, negative-var constraints, table-kind variables; all tests pass (race -count=3), vet+lint clean, CHANGELOG+ROADMAP updated

## iterate iteration 2 (2026-05-15)

Address verify-gate gaps: added ParseErrUnboundNegativeVar alias (reconciles naming with issue spec), added TermNot unbound-tgtVar validation panic in validateAndSortTerms, and added required TestVar_NegativeVar_FreeBindingPanics test to query_var_test.go. All tests pass (race -count=3), vet+lint clean.

