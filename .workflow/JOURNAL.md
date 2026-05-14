## iterate iteration 1 (2026-05-14)

Implemented Phase 16.2: Disabled and Prefab built-in tags (v0.57.0). New query_filters.go with World.Disabled()/World.Prefab() (indices 36/37), DisableEntity/EnableEntity/IsDisabled, MarkPrefab/IsPrefab. Query implicit-skip wiring in query.go (matchesTable) and cached_query.go (tryMatchTable) via skipDisabled/skipPrefab flags — O(1) per-table exclusion. Prefab bootstrapped DontInherit. 15 new tests in query_filters_test.go. All baseline tests updated (builtinEntityCount 37→39, Wildcard/Any 36→38, 37→39). go vet + golangci-lint clean, go test -race -count=3 passing, coverage 95.0%.

