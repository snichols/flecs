## iterate iteration 1 (2026-05-14)

Phase 16.34 complete: PUT /component/{entity}/{component} and DELETE /component/{entity}/{component} implemented in rest.go with shared writeMu, 1 MB body cap, typed/tag/dynamic/pair support, ~ pair encoding, and idempotent DELETE. 17 tests in rest_component_test.go; restPutComponent 100%, restDeleteComponent 100%; go test -race -count=3 clean; go vet and golangci-lint clean. Package coverage 95.0%. Docs updated: FlecsRemoteApi.md (new endpoint sections, updated gap callout), docs/README.md, README.md, CHANGELOG.md v0.89.0, ROADMAP.md Phase 16.34.

