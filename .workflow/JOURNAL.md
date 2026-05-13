## iterate iteration 1 (2026-05-13)

Phase 15.18 OrderedChildren trait complete: 18 tests pass at 100% coverage, all existing tests pass with -race -count=3. Root cause of test failures was Reader.EachChild not checking orderedChildren map (fixed in scope.go). Marshal round-trip fixed by adding ordered_children field to jsonEntity. Docs updated: CHANGELOG v0.50.0, ROADMAP, ComponentTraits.md, HierarchiesManual.md, docs/README.md, README.md.

## iterate iteration 2 (2026-05-13)

Fixed all three verify-gate gaps: (1) S1011 gosimple in ordered_children.go:applyOrderedChildrenPolicy replaced loop with append(…, t.Entities()...); (2) gofmt alignment in marshal.go and world.go fixed via gofmt -w; (3) coverage pushed from 94.7% to 95.1% by adding 4 targeted tests — TestOrderedChildren_WorldEachChildOrdered and TestOrderedChildren_WorldEachChildEarlyStop cover the ordered path in World.EachChild (previously only Reader.EachChild was exercised), and TestOrderedChildren_BatchedDeferredAdd and TestOrderedChildren_BatchedDeferredRemove cover the batchForEntity ordered-children hook (lines 312-329) which only fires when an entity has 2+ commands in a single Write block. golangci-lint and go vet clean; go test -race -count=3 ./... passes.

