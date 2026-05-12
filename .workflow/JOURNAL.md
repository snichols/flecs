## iterate iteration 1 (2026-05-12)

Phase 14.10 complete: ported docs/DesignWithFlecs.md from upstream C flecs. Full Go-idiomatic conceptual guide covering entity lifecycle, atomic component design, uncached vs. cached query selection, single-responsibility systems, four built-in pipeline phases with conventions, Go-package module structure, relationship decision guidance, and reactive observer design with aggressive cross-links to all 10 other ported docs. Created docs/design_examples_test.go with 10 TestDesign_* functions (all passing under go test ./... -race -count=1 and go vet). Updated docs/README.md (DesignWithFlecs → landed/14.10), ROADMAP.md (14.10 → shipped v0.29.0), CHANGELOG.md (Unreleased Phase 14.10 entry).

