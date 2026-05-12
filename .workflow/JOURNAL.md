## iterate iteration 1 (2026-05-12)

Phase 15.9 Acyclic relationship trait fully implemented: new built-in entity (index 22), SetAcyclic/IsAcyclic/w.Acyclic() API, write-time cycle rejection in both immediate and deferred paths, ChildOf bootstrapped acyclic, 9 tests, docs updated (ComponentTraits.md, HierarchiesManual.md, README.md), CHANGELOG v0.41.0 and ROADMAP updated. All tests pass (race -count=3), coverage 95%, go vet and golangci-lint clean.

