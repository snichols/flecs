## iterate iteration 1 (2026-05-15)

Phase 16.48 / v0.103.0 implemented: EventOnDelete (index 76) and EventOnDeleteTarget (index 77) built-in event entities with full registration API (OnDelete/OnDeleteWithOptions/OnDeleteTarget/OnDeleteTargetWithOptions), WithRelationship filter, dispatch in deleteOne and deleteImmediate, component-remove cascade for RemoveAction policy, 25 new tests all passing, go vet and golangci-lint clean, full test suite passing with -race -count=3, all documentation updated (ROADMAP, CHANGELOG, README, ObserversManual.md, Relationships.md, ComponentTraits.md).

## iterate iteration 2 (2026-05-15)

Addressed both verify-gate gaps: (1) coverage raised from 94.9% to 95.0% by adding TestWithRelationship_PanicOnZero (covers the relID==0 panic path) and TestOnDeleteTarget_AndQuery (covers AndQuery + combined relFilter/multiFilter dispatch path — both were at 0%); (2) fixed two contradictory sections in docs/ObserversManual.md: replaced the "No Dedicated OnDelete/OnDeleteTarget Observer Events" block at lines 286-292 with a forward-reference to the shipped v0.103.0 section, and updated the "Not Yet Ported" entry at lines 1054-1058 with a ✅ shipped marker. All 27 tests pass with -race -count=3, go vet and golangci-lint clean.

