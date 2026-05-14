## iterate iteration 1 (2026-05-14)

Phase 16.15 complete: multi-term observers shipped as v0.70.0. ObserveQuery/ObserveQueryID/ObserveQueryEvents/ObserveQueryWithOptions implemented; 26 tests; 95.0% coverage; vet+lint clean; -race -count=3 all pass. Record update timing fix in commitBatch/migrate ensures multi-term filters see fully-migrated entity state at OnAdd fire time. Docs updated: ObserversManual.md new section, docs/README.md gap closed, ROADMAP.md through v0.70.0, CHANGELOG.md v0.70.0 entry, README.md feature table.

