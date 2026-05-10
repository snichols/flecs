## iterate iteration 1 (2026-05-10)

Implemented Phase 4.3: built-in IsA relationship with transitive component inheritance. Allocates IsA entity at index 2 in World.New(). Added IsA() accessor, PrefabOf(), EachPrefab(), Owns[T]/OwnsID (local-only checks), and made Get[T]/Has[T]/HasID inheritance-aware with cycle detection and dead-prefab guards. Extracted shared firstPairTarget/eachPairTarget helpers into pair_internal.go and refactored ParentOf to use them. Full isa_test.go coverage including multi-level chains, cycles, override, remove-restores-inheritance, tag inheritance, EachPrefab direct-only, and delete-prefab footgun. go test -race -count=2 passes, go vet clean, golangci-lint clean, coverage 97.3%.

