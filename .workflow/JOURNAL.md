## iterate iteration 1 (2026-05-12)

Phase 14.7 complete: ported docs/ObserversManual.md from upstream C flecs. Full Go-idiomatic manual covering hooks (OnAdd/OnSet/OnRemove, single-subscriber, hook-before-observer ordering, *Writer read-safety) and multi-subscriber observers (Observe[T], ObserveID, Observe2[T], Unsubscribe semantics). Includes use-case examples (validation, indexing, replication, logging) and 10 not-yet-ported callouts. Created docs/observers_examples_test.go with 13 TestObservers_* functions. Updated docs/README.md (landed/14.7, 10 new gaps), ROADMAP.md (14.7 shipped v0.26.0), CHANGELOG.md (Unreleased entry). All tests pass: go test ./... -race -count=1 clean, go vet clean.

