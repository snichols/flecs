## iterate iteration 1 (2026-05-14)

Implemented Phase 16.35 REST toggle endpoint (v0.90.0): PUT /toggle/{entity} toggles Disabled tag; PUT /toggle/{entity}/{component} toggles CanToggle component bits. Both support ?enabled=true/false/omit-to-flip and return {"enabled": <bool>}. 14 tests in rest_toggle_test.go cover all required cases. Coverage 95.1%, go vet and golangci-lint clean, -race -count=3 passes. Docs updated: FlecsRemoteApi.md, docs/README.md, README.md, CHANGELOG.md, ROADMAP.md.

