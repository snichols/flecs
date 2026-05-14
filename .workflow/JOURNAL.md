## iterate iteration 1 (2026-05-14)

Reclassified access modifiers from gap to N/A by design. docs/README.md line 115 rewritten with N/A-by-design prefix and cross-link to Queries.md. docs/Queries.md: promoted inline callout to anchored section "## Access modifiers — N/A by design" explaining upstream feature, Go-flecs scoping model, and finality of the decision. go vet and go test -race -count=1 pass; no code changes.

