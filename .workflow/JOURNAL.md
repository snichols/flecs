## iterate iteration 1 (2026-05-14)

Implemented Phase 16.8 custom events (v0.63.0). Refactored observerKey dispatch table from EventKind to event entity IDs. Added 5 built-in event entities at indices 40–44 (EventOnAdd, EventOnSet, EventOnRemove, EventOnTableCreate, Event tag); user entities now start at 45. Added RegisterEvent, Emit, EmitTyped, ObserveEvent, ObserveEventTyped in observer_custom.go. Updated all dispatch call sites in hooks.go, observer_table.go, observer_options.go, world.go. Added 12 tests in observer_custom_events_test.go (all pass). Coverage 95.0%. go vet, golangci-lint, go test -race -count=3 all clean. Updated CHANGELOG v0.63.0, ROADMAP, README feature table, docs/ObserversManual.md custom events section, docs/README.md gap flip.

