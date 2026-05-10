## iterate iteration 1 (2026-05-10)

Implemented all deliverables for issue #1: repo scaffolding (README.md, LICENSE with upstream credit, .golangci.yml, .github/workflows/ci.yml pinned to Go 1.26.x), and the flecs package with id.go (ID uint64 type, flag constants mirroring upstream bit values exactly, all methods and constructors) plus id_test.go with table-driven tests covering entity round-trip, generation overwrite, pair encode/decode, flag tests, zero value, and String() golden cases. All tests pass under go test ./... -race and go vet ./...

