## iterate iteration 1 (2026-05-10)

Implemented Phase 9.2.1 basic JSON serialization: marshal.go with World.MarshalJSON/UnmarshalJSON, 15 tests in marshal_test.go covering all spec scenarios (empty world, single/multi entity, names, tags, pairs skipped, built-ins skipped, error cases, two-step round-trip), doc.go JSON Serialization section, README feature entry, CHANGELOG entry. All tests pass (race -count=2), go vet clean, golangci-lint clean, coverage 96.8%.

