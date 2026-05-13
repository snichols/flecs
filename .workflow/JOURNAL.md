## iterate iteration 1 (2026-05-13)

Phase 15.18 OrderedChildren trait complete: 18 tests pass at 100% coverage, all existing tests pass with -race -count=3. Root cause of test failures was Reader.EachChild not checking orderedChildren map (fixed in scope.go). Marshal round-trip fixed by adding ordered_children field to jsonEntity. Docs updated: CHANGELOG v0.50.0, ROADMAP, ComponentTraits.md, HierarchiesManual.md, docs/README.md, README.md.

