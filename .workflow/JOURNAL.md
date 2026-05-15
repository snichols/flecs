## iterate iteration 1 (2026-05-15)

Phase 16.40 query DSL REST endpoint shipped as v0.95.0. GET /query?expr= parses FQL v1 (bare components, , AND, ! NOT, (R,T) pairs, */_ wildcards) into Terms, executes inside World.Read, returns matched entities with typed field values as JSON. New files: query_dsl.go, rest_query.go, docs/QueryDSL.md. 51 total tests (21 parser, 25 HTTP integration, 5 internal unit). 95.0% coverage. go vet, golangci-lint, -race -count=3 all clean. Docs updated: FlecsRemoteApi.md (GET /query section), Queries.md (cross-link), README.md, CHANGELOG.md, ROADMAP.md.

