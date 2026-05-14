## iterate iteration 1 (2026-05-14)

Phase 16.17: Observer propagation along IsA edges shipped as v0.72.0. observer_propagation.go implements BFS traversal with DontInherit/override gates, cycle-safe visited set, cached inheritorsBFS with O(1) invalidation, propagation-aware multi-term filter, and OnReplace hook propagation. All four fireOn* hooks wired; Emit propagates custom events. 24 tests, 95.0% package coverage, vet+lint clean, -race -count=3 passes. Full doc suite updated: ObserversManual.md new section, PrefabsManual.md cross-link, docs/README.md gap entry flipped, README.md table row, CHANGELOG.md v0.72.0 entry, ROADMAP.md heading bumped.

