## iterate iteration 1 (2026-05-14)

Phase 16.34 complete: PUT /component/{entity}/{component} and DELETE /component/{entity}/{component} implemented in rest.go with shared writeMu, 1 MB body cap, typed/tag/dynamic/pair support, ~ pair encoding, and idempotent DELETE. 17 tests in rest_component_test.go; restPutComponent 100%, restDeleteComponent 100%; go test -race -count=3 clean; go vet and golangci-lint clean. Package coverage 95.0%. Docs updated: FlecsRemoteApi.md (new endpoint sections, updated gap callout), docs/README.md, README.md, CHANGELOG.md v0.89.0, ROADMAP.md Phase 16.34.

## iterate iteration 2 (2026-05-14)

Added 6 tests to cover all previously uncovered branches in restPutComponent: tag-branch MaxBytesReader overflow → 413, tag-branch non-overflow read error → 400, typed-branch MaxBytesReader overflow → 413, dynamic-branch non-JSON-string body → 400, dynamic-branch invalid base64 → 400, and dynamic-branch non-overflow read error → 400. restPutComponent now 100%; restDeleteComponent 100%; package coverage 95.1%. go test -race -count=3 clean; go vet and golangci-lint clean.

