## iterate iteration 1 (2026-05-12)

Phase 14.5 complete: ported docs/PrefabsManual.md with 9 code blocks covering IsA instantiation, SetInheritable query inheritance, copy-on-write override, Owns check, restore-via-Remove, prefab variants, PrefabOf/EachPrefab/GetUp traversal helpers, and OnInstantiate trait callouts (Override/DontInherit not-yet-ported). Added docs/prefabs_examples_test.go (9 TestPrefabs_* functions). Updated docs/README.md (landed / 14.5, 4 new gaps), ROADMAP.md (shipped v0.24.0), CHANGELOG.md (Unreleased entry), Quickstart.md and Relationships.md (reverse cross-links). All tests pass: go test ./... -race -count=1 clean, go vet clean, golangci-lint clean.

