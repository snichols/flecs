## iterate iteration 1 (2026-05-14)

Implemented Clear, MakeAlive, SetVersion entity lifecycle ops (v0.56.0). All 23 entity_lifecycle tests + 12 entityindex tests pass. go vet clean, golangci-lint clean, go test -race -count=3 clean. Coverage 95.0% main / 97.3% entityindex. Docs updated (EntitiesComponents.md, docs/README.md, README.md, CHANGELOG.md, ROADMAP.md).

