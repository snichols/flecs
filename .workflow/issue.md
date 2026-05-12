## Goal

Continue the docs port begun in Phase 14.0 by landing **DesignWithFlecs** — the high-level conceptual guide on modeling game/sim state with ECS, when to use tags vs components vs relationships, and common architectural patterns. Phases 14.0–14.9 have shipped (v0.19.0–v0.28.0); this is the next sequential phase.

**Target version: v0.29.0.**

Upstream source: `/work/agents/claude/projects/SanderMertens/flecs/docs/DesignWithFlecs.md` (~3200 words). Classified `port-as-is` in the 14.0 survey — content maps cleanly, only code examples need Go syntax.

This is the most conceptual doc in the port set. It's about *design patterns* and *modeling choices*, not API surface. The port should:

- Lead with the conceptual frame (entity = unique handle, component = data, system = behavior).
- Keep the upstream's conceptual frame intact while translating code examples to Go idioms.
- Cover when to use tags vs components vs relationships; composition over inheritance; ECS for state machines; entity lifecycle patterns.
- Adapt section-specific advice where Go ergonomics differ (e.g., where C might recommend a `void*` and `ECS_TAG`, Go would use a typed component).
- For any C-specific design advice that doesn't translate (arena allocators, raw `void*`, etc.), either skip or replace with the Go-equivalent advice.
- Cross-link aggressively — this is the "where do I start" hub doc and should naturally point to specific subsystem docs.
- Reference existing examples (`example_*_test.go`) where appropriate, but use docs as the primary tone.

### Deliverables

1. **Full port of `docs/DesignWithFlecs.md`** adapted to Go, replacing the current stub.
2. **`docs/design_examples_test.go`** with `TestDesign_*` functions verifying each code block compiles and runs against v0.28.0. Since the doc is design-focused, most blocks will be small illustrative snippets rather than full programs.
3. **Update `docs/README.md`**: DesignWithFlecs row → `✅ landed / 14.10`. Likely 0–3 new gaps (this is conceptual; few implementation gaps surface). Don't force gaps; only add if genuinely surfaced.
4. **Update `ROADMAP.md`**: 14.10 row → `✅ shipped (v0.29.0)`. Do NOT bump the heading.
5. **Update `CHANGELOG.md`** with `Unreleased — Phase 14.10: DesignWithFlecs doc port (upcoming v0.29.0)`.
6. **Cross-link** with every other ported doc.

### Style notes

- Native Go throughout.
- Conceptual tone — focus on *why*, *when*, and *how to think about it*. Less API reference, more architectural advice.
- Heavy cross-linking is fine — this doc is the navigator.

### Non-goals

- No source changes outside `docs/`, `ROADMAP.md`, `CHANGELOG.md`, `doc.go` cross-references.
- No porting beyond DesignWithFlecs.

### Mechanical acceptance

- `go test ./docs/...` passes.
- `go vet`, `golangci-lint` clean.
- `go test ./... -race -count=1` passes.
- `docs/README.md` shows DesignWithFlecs as landed.

## Constraints

- @docs/DesignWithFlecs.md — current stub to be replaced with the full port.
- @docs/Quickstart.md — cross-link source; the entry-point doc DesignWithFlecs should point readers toward for hands-on first contact.
- @docs/EntitiesComponents.md — cross-link source for the entity/component frame; underpins the conceptual lead of DesignWithFlecs.
- @docs/Queries.md — cross-link source when discussing how systems read state.
- @docs/Relationships.md — cross-link source for tag-vs-component-vs-relationship guidance.
- @docs/HierarchiesManual.md — cross-link source for composition-over-inheritance and hierarchy modeling.
- @docs/PrefabsManual.md — cross-link source for entity templates and lifecycle patterns.
- @docs/Systems.md — cross-link source for behavior/system design.
- @docs/ObserversManual.md — cross-link source for reactive patterns.
- @docs/ComponentTraits.md — cross-link source for trait-based modeling choices.
- @docs/FlecsRemoteApi.md — cross-link source for tooling/inspection advice.
- @docs/README.md — index that must reflect 14.10 status.
- @docs/rest_examples_test.go — recent test pattern; `design_examples_test.go` should follow the same shape (build-tag-free, `TestDesign_*` per code block, idiomatic teardown).
- @example_basic_test.go — concrete Go example to reference and link from the doc.
- @example_pipeline_test.go — concrete Go example to reference and link from the doc.
- @doc.go — package-level docs; cross-references may need updating to point at DesignWithFlecs once landed.
- @README.md — repo README; check whether the docs table needs a status nudge.
- @ROADMAP.md — Phase 14.10 row must flip to `✅ shipped (v0.29.0)`; heading stays put.
- @CHANGELOG.md — add `Unreleased — Phase 14.10` entry for upcoming v0.29.0.
- Governed by CONTRIBUTING.md "Documentation" policy: code blocks must compile against v0.28.0 and be exercised by `docs/*_test.go`.
- Upstream reference (not in-repo): `/work/agents/claude/projects/SanderMertens/flecs/docs/DesignWithFlecs.md` — source of truth for content structure and conceptual frame.
