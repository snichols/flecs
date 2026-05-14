## iterate iteration 1 (2026-05-14)

Implemented Phase 16.6: Rate filters (SetInterval / SetRate) (v0.61.0). Added interval, intervalAccum, rate, rateCounter fields to *System struct. Added SetInterval/GetInterval and SetRate/GetRate methods with negative-arg panics. Extended runPhase closure with subtract-with-cap interval gate and modulo rate gate composing with AND semantics. 12 test cases in system_rate_test.go. Updated Systems.md (new § Rate Filters), docs/README.md (gap line 144 flipped to shipped), README.md (feature row), CHANGELOG.md (v0.61.0 entry), ROADMAP.md (heading bump + Phase 16.6 bullet), docs/systems_examples_test.go (example). go vet/golangci-lint clean, go test -race -count=3 passing, coverage 95.1%.

