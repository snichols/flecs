## iterate iteration 1 (2026-05-10)

Implemented Phase 5.1 lifecycle hooks. New hooks.go: OnAdd[T]/OnSet[T]/OnRemove[T] typed registration API + fireOnAdd/fireOnSet/fireOnRemove nil-safe helpers on *World. Wired fire-sites: migrate() fires OnAdd (post-write) and OnRemove (pre-swap); Set[T] fires OnSet after every write; deleteOne() fires OnRemove per-component before RemoveSwap, giving post-order cascade semantics. SetPair[T] fires OnSet on pair TypeInfo. 15 tests in hooks_test.go covering all required scenarios. go test -race -count=2 passes; coverage 97.7%; go vet + golangci-lint clean.

