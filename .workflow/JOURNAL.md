## iterate iteration 1 (2026-05-12)

Phase 14.1 complete: full Go-idiomatic port of docs/EntitiesComponents.md. Created docs/entities_components_examples_test.go with 16 TestEC_* tests covering every code block (entity create/delete/liveness, names, hierarchical lookup, Set/Get/Has/Remove, tags, dynamic tags, OnAdd/OnSet/OnRemove hooks, hook ordering, ComponentInfo, registration, singleton workaround). Sections for 9 unported features carry explicit callouts linking to the feature-gap list. docs/README.md updated (EntitiesComponents row → landed, 9 new gaps appended). ROADMAP.md 14.1 row → shipped. CHANGELOG.md Unreleased entry added. go test ./... -race -count=1 and go vet ./... clean.

