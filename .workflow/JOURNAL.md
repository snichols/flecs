## iterate iteration 1 (2026-05-14)

Implemented Phase 16.9: custom pipeline phases and DependsOn ordering (v0.64.0). Added NewPhase, (*Phase).DependsOn, (*Phase).SetEnabled/IsEnabled/Name, (*System).DependsOn, World.DependsOn() ID, and lazy Kahn topological sort for both phases and systems. Breaking API change: built-in phase accessors now return *Phase; NewSystemInPhase takes *Phase; LastFramePhases is []PhaseStats; built-in entity count 45→46. Added pipeline_phases.go (core), pipeline_phases_test.go (16+ tests). Updated all doc files (CHANGELOG v0.64.0, MIGRATING v0.64.0, ROADMAP, README, docs/README.md, docs/Systems.md). Coverage 95.0%, go vet/golangci-lint/go test -race -count=3 all clean.

