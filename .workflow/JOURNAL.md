## iterate iteration 1 (2026-05-13)

Implemented Phase 15.15: Relationship/Target/Trait usage constraints (v0.47.0). New file usage_constraints.go with all three trait implementations; world.go updated with 3 new built-in entities at indices 28/29/30 (shifting Wildcard→31, Any→32) and bootstrap self-classifications; id_ops.go and cmd_queue.go updated for immediate and deferred-path enforcement; 16-case test suite at 95.0% coverage; marshal.go/test baselines bumped; full docs updated (ComponentTraits.md, Relationships.md, README.md, docs/README.md, CHANGELOG.md, ROADMAP.md). All tests pass under go vet, golangci-lint, and go test -race -count=3.

