## iterate iteration 1 (2026-05-15)

Phase 16.41 Query DSL v2 parser shipped as v0.96.0. Extended query_dsl.go with OR (||), negated scope groups, optional terms, traversal postfixes (.Up/.SelfUp/.Cascade), source binding, query variables, equality predicates, and type-list operators (AndFrom/OrFrom/NotFrom). Added ParseErrorCode enum (8 values). Added restBuildExecSets to rest_query.go for anchor-free query execution. 28 new parser tests + 10 REST integration tests. 95.0% coverage. go vet, golangci-lint, -race -count=3 clean. docs/QueryDSL.md replaced with full v2 reference; CHANGELOG.md, ROADMAP.md, FlecsRemoteApi.md, Queries.md, README.md updated.

