## iterate iteration 1 (2026-05-14)

Phase 16.24: Binary world snapshots (v0.79.0). Implemented TakeSnapshot/RestoreSnapshot/Bytes/LoadSnapshot with full binary serialisation of entity index, archetype columns, disabled-component bitsets, sparse/DontFragment side-maps, union state, entity range, trait policies, and ordered-children lists. Same-world restriction enforced via worldID token. 28 tests; 95.0% coverage; go vet and -race -count=3 clean. All doc targets updated: docs/Snapshots.md, docs/README.md line 86 flipped to shipped, README.md feature index, CHANGELOG.md v0.79.0 entry, ROADMAP.md heading bump.

