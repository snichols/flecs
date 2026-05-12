## iterate iteration 1 (2026-05-12)

Phase 14.2 complete: full Go-idiomatic port of docs/Queries.md from upstream C flecs. Created docs/queries_examples_test.go with 19 TestQueries_* tests covering every code block (archetype tables, NewQuery/NewCachedQuery/NewQueryFromTerms/NewCachedQueryFromTerms, And/Not/Optional/Or operators, Field/FieldMaybe/FieldShared/IsFieldSelf, Each1/Each2, pairs, Up/SelfUp/Cascade traversal, SetInheritable, Changed/Close). 10 not-yet-ported sections with callouts. docs/README.md Queries row → landed/14.2, 8 new gaps appended. ROADMAP.md 14.2 row → shipped. CHANGELOG.md Unreleased entry added. go test ./... -race -count=1 and go vet ./... clean.

