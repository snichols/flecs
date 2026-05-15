## iterate iteration 1 (2026-05-15)

Phase 16.44 join-order optimizer implemented: query_optimizer.go with estimateVarDomain/selectOptimalDriver, DriverVariable()/VariableOrder() accessors on Query and CachedQuery, 11 optimizer unit tests, 2 benchmark variants, TestOptimizer_ResultSetIdentical in query_var_test.go, TestRest_Query_MultiVar_Optimized in rest_query_test.go, Queries.md "Join-order optimization" subsection with 256-table cap doc, ROADMAP/CHANGELOG/README updates. go test ./... -race -count=3 clean, go vet clean, golangci-lint clean.

## iterate iteration 2 (2026-05-15)

Fixed ROADMAP.md Shipped section heading from "v0.97.0" to "v0.99.0" to satisfy the verify gate requirement. All Phase 16.44 implementation was already complete from the prior iteration.

