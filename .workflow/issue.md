## Goal

Port the upstream **Manual** to Go flecs, adapted as a condensed cross-link hub doc that summarizes core concepts and points into the per-topic manuals. The Manual is the canonical top-level reference and the doc most likely to be linked from external pages.

Phases 14.0–14.10 shipped (v0.19.0–v0.29.0). This phase continues the docs port against v0.29.0 with **target version v0.30.0**.

Upstream source: `/work/agents/claude/projects/SanderMertens/flecs/docs/Manual.md` (~2200 words, classified `port-adapted`, effort: medium).

### Why this exists

The Manual overlaps in scope with multiple per-topic docs (EntitiesComponents, Queries, Systems, etc.). Rather than duplicate content, our port adapts it to be:

- A condensed cross-link hub pointing into per-topic manuals
- Plus original content that isn't covered elsewhere (world lifecycle, error handling philosophy, performance summary, concurrency model)
- Heavily cross-linked from every existing doc — Manual.md becomes the hub

### Deliverables

1. **Full port of `docs/Manual.md`** adapted to Go:
   - Lead with a one-paragraph summary of Go flecs.
   - Table of concepts → per-topic docs.
   - Original content for things not covered in per-topic docs (world lifecycle, error handling philosophy, performance characteristics summary).
   - Heavy cross-linking to: Quickstart, EntitiesComponents, Queries, Relationships, HierarchiesManual, PrefabsManual, Systems, ObserversManual, ComponentTraits, FlecsRemoteApi, DesignWithFlecs.
   - For C-specific Manual content that doesn't apply (e.g. CMake build, C runtime considerations, `ECS_DECLARE` macros), replace with Go equivalents or note as N/A.
   - Include a concise **Concurrency model** section summarizing what Phase 10.x and 12.x shipped (Reader/Writer, ExclusiveAccess, per-stage queues, parallel/multi-threaded systems).
   - Include a brief **Performance** section pointing to BENCH.md.

2. **Verify code blocks.** Create `docs/manual_examples_test.go` with `TestManual_*` functions per code block. Code must compile against v0.29.0.

3. **Update `docs/README.md`**: Manual row → ` landed / 14.11`. Few new gaps expected (Manual summarizes; doesn't introduce new feature dependencies).

4. **Update `ROADMAP.md`**: 14.11 row → ` shipped (v0.30.0)`. Do NOT bump the heading.

5. **Update `CHANGELOG.md`** with `Unreleased — Phase 14.11: Manual doc port (upcoming v0.30.0)`.

6. **Cross-link** from every existing doc back to Manual.md. Update existing docs' "See also" or "Where next" sections to include Manual.md.

### Non-goals

- No source changes.
- No porting beyond Manual.

### Mechanical acceptance

- `go test ./docs/...` passes.
- `go vet`, `golangci-lint` clean.
- `go test ./... -race -count=1` passes.
- `docs/README.md` shows Manual as landed.

### Style notes

- Hub doc — short, scannable, cross-link-heavy. Don't duplicate per-topic content; point at it.
- Native Go. Use the Quickstart as the most-linked starting point.
- Concurrency section is the most original new content — make it accurate, cite the relevant phases (10.x Reader/Writer, 12.x ExclusiveAccess and escape-hatch removal).

## Constraints

- @docs/Manual.md — current stub to be replaced with full port
- @docs/Quickstart.md — most-linked starting point for newcomers; Manual must lead readers here
- @docs/EntitiesComponents.md — per-topic doc Manual cross-links to; do not duplicate content
- @docs/Queries.md — per-topic doc Manual cross-links to; do not duplicate content
- @docs/Relationships.md — per-topic doc Manual cross-links to
- @docs/HierarchiesManual.md — per-topic doc Manual cross-links to
- @docs/PrefabsManual.md — per-topic doc Manual cross-links to
- @docs/Systems.md — per-topic doc Manual cross-links to
- @docs/ObserversManual.md — per-topic doc Manual cross-links to
- @docs/ComponentTraits.md — per-topic doc Manual cross-links to
- @docs/FlecsRemoteApi.md — per-topic doc Manual cross-links to
- @docs/DesignWithFlecs.md — per-topic doc Manual cross-links to; also receives reciprocal link
- @docs/README.md — index table; Manual row updated to landed / 14.11
- @docs/design_examples_test.go — recent test pattern to mirror in `manual_examples_test.go`
- @BENCH.md — Performance section in Manual points here
- @doc.go — package overview; ensure Manual reference is consistent
- @README.md — repo-level entry point; may benefit from Manual link
- @ROADMAP.md — 14.11 row updated to shipped (v0.30.0); heading not bumped
- @CHANGELOG.md — Unreleased entry for Phase 14.11
- CONTRIBUTING.md "Documentation" policy governs this phase — code blocks must compile against v0.29.0
- Upstream source: `/work/agents/claude/projects/SanderMertens/flecs/docs/Manual.md` — port-adapted classification, ~2200 words
- Target version: v0.30.0
