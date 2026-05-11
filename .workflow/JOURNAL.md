## iterate iteration 1 (2026-05-10)

Phase 7.2 implemented: three built-in pipeline phases (PreUpdate/OnUpdate/PostUpdate). Added phase entity allocation in World.New() (indices 4-6), PreUpdate()/OnUpdate()/PostUpdate() accessors, phase ID field on System, NewSystemInPhase() with validation, refactored Progress() to per-phase Defer dispatch for cross-phase mutation visibility. Updated TestIsAWorldCountBaseline (3→6 built-ins). 14 new tests in pipeline_test.go. go test ./... -race -count=2 passes; golangci-lint clean; 97.1% coverage.

