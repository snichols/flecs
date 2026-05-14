## iterate iteration 1 (2026-05-14)

Phase 16.33 complete: PUT /entity and DELETE /entity/{path...} implemented in rest.go with MakeAlive deferDepth fix, writeMu serialization, and full panic recovery. 14 tests in rest_entity_test.go; restPutEntity 100%, restDeleteEntity 100%; go test -race -count=3 clean; go vet and golangci-lint clean. Docs updated: FlecsRemoteApi.md (new endpoint sections, updated unimplemented callouts), docs/README.md, README.md, CHANGELOG.md v0.88.0, ROADMAP.md Phase 16.33.

