## iterate iteration 1 (2026-05-12)

Implemented Final entity trait (Phase 15.10, v0.42.0): SetFinal/IsFinal(scope)/w.Final() at index 23; write-time enforcement in addIDImmediate panics on (IsA, Final-entity) including self-pairs; bare-tag handler; marshal skip-set updated; 8 test cases pass; Wildcard→24, Any→25, user→26+; docs updated across ComponentTraits.md, Relationships.md, PrefabsManual.md, README.md, CHANGELOG.md, ROADMAP.md; all tests pass race -count=3

