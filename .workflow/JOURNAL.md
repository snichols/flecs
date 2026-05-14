## iterate iteration 1 (2026-05-14)

Implemented Phase 16.5: Observer lifecycle bundle (yield_existing + observer disabling) (v0.60.0). Added enabled bool to Observer struct with SetEnabled/IsEnabled methods and dispatch gate in dispatchObservers. Created observer_options.go with ObserverOptions, WithYieldExisting, and ObserveWithOptions[T] featuring synchronous sweep of existing entities. 16 test cases in observer_lifecycle_test.go. Updated ObserversManual.md (two new sections), docs/README.md (lines 156+159 flipped to shipped), CHANGELOG.md (v0.60.0), ROADMAP.md (bumped to v0.60.0, Phase 16.5 entry). go vet/golangci-lint clean, go test -race -count=3 passing, coverage 95.0%.

