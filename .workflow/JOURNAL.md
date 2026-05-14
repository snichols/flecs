## iterate iteration 1 (2026-05-14)

Phase 16.12 fixed-source observer terms fully implemented and shipped as v0.67.0. Storage upgraded from flat slice to observerBucket with lazy fixedSource map. New API: WithSource(e ID), AndSource(e ID), ObserveIDWithOptions, ObserveEventWithOptions. 19 tests in observer_fixed_source_test.go, coverage 95.0%, go vet/golangci-lint/go test -race -count=3 all clean. Docs: ObserversManual.md gap replaced with full section, docs/README.md flipped to shipped, README.md feature row added, CHANGELOG.md v0.67.0 entry, ROADMAP.md heading bumped to v0.67.0.

