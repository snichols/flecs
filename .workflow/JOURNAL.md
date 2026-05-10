## iterate iteration 1 (2026-05-10)

Phase 4.2 complete: ChildOf built-in relationship entity (index 1) allocated in World.New(); World.ChildOf() accessor; Delete refactored into deleteOne + cascade orchestrator (iterative DFS, snapshot-before-mutate, cycle detection, post-order delete); EachChild and ParentOf methods added; childof_test.go with full test coverage for all deliverables. go test -race -count=2 passes, go vet clean, golangci-lint clean, flecs coverage 97.9%, internal/component 100%.

