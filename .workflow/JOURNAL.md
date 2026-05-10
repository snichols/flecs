## iterate iteration 1 (2026-05-10)

Implemented Phase 4.1: pair IDs as first-class signature entries. Added AddID/RemoveID/HasID raw-ID functions and SetPair[T]/GetPair[T] typed pair-data accessors in id_ops.go. Added RegisterPairData[T] and EnsureID to internal/component/registry.go. Verified storage layers are pair-ID-agnostic (no changes needed). All tests pass (go test -race -count=2), go vet clean, golangci-lint clean, flecs coverage 97.9%, internal/component coverage 100%.

