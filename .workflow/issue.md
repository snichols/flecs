## Goal

Port the upstream FAQ to Go, completing the docs-port project (Phases 14.0–14.12).

**Why this exists:**

Phases 14.0–14.11 shipped (v0.19.0–v0.30.0). This phase ports the **FAQ** — short Q&A on common questions about flecs's design, performance, threading, and idioms.

Upstream: `/work/agents/claude/projects/SanderMertens/flecs/docs/FAQ.md` (~1500 words, classified `port-adapted`, effort: small). This is the last docs phase.

Governed by CONTRIBUTING.md "Documentation" policy. Code blocks compile against v0.30.0.

**Target version: v0.31.0.**

### Deliverables

1. **Full port of `docs/FAQ.md`** adapted to Go:
   - Keep the Q&A format.
   - Replace each C-specific answer with the Go equivalent.
   - Add Go-specific Q&A entries that aren't in the upstream (e.g. "Why generics? Why not interface{}?", "How does the Reader/Writer model compare to mutex locking?", "Can I use this with goroutines safely?").
   - Reference the relevant `docs/` pages and CHANGELOG entries for technical answers.

2. **Verify code blocks.** Create `docs/faq_examples_test.go` with `TestFAQ_*` functions for any code-bearing answers. Many FAQ answers will be prose-only with no code.

3. **Update `docs/README.md`**: FAQ row → `✅ landed / 14.12`. Mark the docs-port phase as complete in the survey table. Likely 0–3 new gaps surfaced.

4. **Update `ROADMAP.md`**: 14.12 row → `✅ shipped (v0.31.0)`. Add a final "Documentation port complete (v0.31.0)" note above the phase table. Do NOT bump the heading.

5. **Update `CHANGELOG.md`** with `Unreleased — Phase 14.12: FAQ doc port (upcoming v0.31.0)`. Include a celebratory note that this completes the docs-port project (14.0–14.12, 13 phases spanning v0.19.0–v0.31.0).

6. **Cross-link** with Manual (the hub), Quickstart (most common starting point), and any specific subsystem docs the FAQ touches.

### Non-goals

- No source changes. This is the final docs phase.

### Mechanical acceptance

- `go test ./docs/...` passes.
- `go vet`, `golangci-lint` clean.
- `go test ./... -race -count=1` passes.
- `docs/README.md` shows FAQ as landed and the survey table shows all 12 doc phases complete.

### Style notes

- Q&A format. Each Q gets a `###` heading, each A is a short paragraph.
- Reference per-topic docs liberally — "For details see [Queries.md]".
- Add Go-specific Q&A: generics, goroutine safety, slog logger, error vs panic, Reader/Writer scopes, why no module system.

### Upstream

`/work/agents/claude/projects/SanderMertens/flecs/docs/FAQ.md`

## Constraints

- @docs/FAQ.md — stub to be replaced with the full port; preserve Q&A heading structure
- @docs/Manual.md — hub doc; the FAQ must cross-link back here and adopt its tone
- @docs/Quickstart.md — most-linked starting point; reference it from beginner-oriented answers
- @docs/README.md — FAQ row must be flipped to `✅ landed / 14.12`; survey table must show all 12 doc phases complete
- @docs/manual_examples_test.go — recent test pattern to mirror in `docs/faq_examples_test.go` (`TestFAQ_*` functions, compile-against-v0.30.0 code blocks)
- @doc.go — package-level overview; FAQ answers should be consistent with its phrasing
- @README.md — project-level entry point; FAQ should be discoverable from here
- @ROADMAP.md — 14.12 row must flip to `✅ shipped (v0.31.0)`; add "Documentation port complete (v0.31.0)" note above the phase table; do NOT bump the heading
- @CHANGELOG.md — add `Unreleased — Phase 14.12: FAQ doc port (upcoming v0.31.0)` with a celebratory note completing the 14.0–14.12 docs-port project
- CONTRIBUTING.md "Documentation" policy governs this phase — code blocks must compile against v0.30.0
