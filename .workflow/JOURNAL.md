## iterate iteration 1 (2026-05-10)

Implemented Phase 9.1 public introspection API. Added ComponentInfo snapshot type and five *World methods in meta.go: Components(), ComponentInfo(), EntityComponents(), EachEntity(), AliveEntities(). Added Registry.IDs() (with idOrder tracking in AssociateID) and entityindex.EachID(fn func(ID) bool). Comprehensive meta_test.go covers all spec cases. All tests pass (race -count=2), go vet clean, golangci-lint clean, flecs coverage 97.1%, internal/component 100%.

