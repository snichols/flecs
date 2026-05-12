## iterate iteration 1 (2026-05-12)

Implemented Singleton component trait (Phase 15.12, v0.44.0): w.Singleton() at index 25 (Wildcard→26, Any→27, user→28+); SetSingleton/IsSingleton/SingletonEntity/Singleton[T]/WriteSingleton[T]; write-time at-most-one-holder enforcement in addIDImmediate, setImmediateByPtr, removeIDImmediate, batchForEntity (coalesced deferred path), and deleteOne; singleton_test.go with 9+ cases including deferred/coalesced/pair-form paths; go vet + golangci-lint clean; all tests pass race -count=3; coverage 95.0%; docs updated across ComponentTraits.md, EntitiesComponents.md, Quickstart.md, README.md, CHANGELOG.md, ROADMAP.md.

