## iterate iteration 1 (2026-05-10)

Implemented Phase 2.1 edge cache for archetype migration. Added addEdges/removeEdges lazy maps to Table with NextOnAdd, NextOnRemove, CacheAddEdge (panic on conflicting dst), CacheRemoveEdge. Updated World.migrate to check cache before the find-or-create block and write cache after for single-component transitions; compound transitions use the existing tables-map path unchanged. Added export_test.go (table) with EdgeCount/AddEdgeFor/RemoveEdgeFor helpers. Full test suite: flecs 97.0%, table 97.2% coverage; go test ./... -race passes; go vet passes.

