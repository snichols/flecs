## iterate iteration 1 (2026-05-12)

Implemented OneOf relationship trait (Phase 15.11, v0.43.0): SetOneOf/IsOneOf(scope)/w.OneOf() at index 24; write-time enforcement in addIDImmediate panics on (R, target) where target is not a direct ChildOf the required parent; bare-tag and pair forms; Wildcard/Any exempt; composes with Exclusive; 8 test cases pass; Wildcard→25, Any→26, user→27+; marshal skip-set, builtinEntityCount, and TestIsAWorldCountBaseline updated; docs updated across ComponentTraits.md, Relationships.md, README.md, CHANGELOG.md, ROADMAP.md; go vet + golangci-lint clean; all tests pass race -count=3; coverage 95.0%

