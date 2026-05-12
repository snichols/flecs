## iterate iteration 1 (2026-05-12)

Implemented Phase 15.3 CanToggle component trait (v0.35.0): cantoggle.go with SetCanToggle/IsCanToggle/EnableID/DisableID/IsEnabledID + typed generics, w.CanToggle() built-in entity at index 18, per-row bitset storage on table.Table (lazy allocation, Append/RemoveSwap maintenance, migration transfer), Each1/Each2/Each3/Each4 per-row filter, cantoggle_test.go with 13 test cases, docs updates across ComponentTraits.md/EntitiesComponents.md/Queries.md/README.md/CHANGELOG.md/ROADMAP.md. All tests pass (go test ./... -race -count=3), go vet clean, golangci-lint clean, coverage 95.0%.

