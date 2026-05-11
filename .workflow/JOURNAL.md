## iterate iteration 1 (2026-05-10)

Phase 9.2.3 IsA prefab serialization implemented. Extended jsonEntity with Prefabs []int, refactored parentOf→predecessorsOf map[ID][]ID, extracted marshaler helper struct with combined ChildOf+IsA DFS topo-sort, updated cycle error wording to use serial/alloc-index. Marshal populates Prefabs after serial assignment preserving EachPrefab order; Unmarshal restores IsA pairs after ChildOf and before components. 13 new tests added. doc.go JSON section updated with prefab example. CHANGELOG entry added. All gates pass: go test ./... -race -count=2, go vet, golangci-lint, coverage 96.7%.

