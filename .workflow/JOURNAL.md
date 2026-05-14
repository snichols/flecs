## iterate iteration 1 (2026-05-14)

Implemented Units addon (Phase 16.30, v0.85.0): units.go with Unit struct, RegisterUnit, UnitFor, SetUnit, Convert (multi-hop base-chain walking), Reader.Unit; 15 built-in unit entities at indices 48–62 (Meter through Degree) bootstrapped in New(); marshal/unmarshal round-trip for user units and component→unit mappings; 12 test cases with 100% coverage on units.go; docs/Units.md; updated docs/README.md, ROADMAP.md, CHANGELOG.md, README.md; fixed existing tests that hardcoded built-in entity count (47→62) or used index ranges now occupied by built-in units. go vet, golangci-lint, go test -race -count=3 all clean.

