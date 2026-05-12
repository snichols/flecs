## Goal

Continue the docs port. Phases 14.0–14.3 shipped (v0.19.0–v0.22.0). This phase ports **HierarchiesManual** — the deep-dive reference on `ChildOf`, parent/child operations, cascading delete, hierarchical names, lookup, and Cascade-ordered iteration.

Upstream: `/work/agents/claude/projects/SanderMertens/flecs/docs/HierarchiesManual.md` (~3900 words, `port-adapted`, effort: medium).

**Target version: v0.23.0.** (Phase 14.3 was v0.22.0.)

**Governed by CONTRIBUTING.md "Documentation" policy.** Every code block compiles against v0.22.0. Use Go idioms throughout.

### Deliverables

1. **Full port of `docs/HierarchiesManual.md`.** Read upstream end to end. Adapt to Go:
   - C `ecs_add_pair(world, child, EcsChildOf, parent)` → `flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))` (plus the typed alternative via `flecs.MakePair`).
   - C `ecs_lookup_path(world, "Game.Level1.Enemy")` → `w.Lookup("Game.Level1.Enemy")`.
   - C cascade-delete semantics → cite our existing implementation in `childof.go`.
   - C iteration with cascade ordering → `flecs.With(...).Cascade(w.ChildOf())` term modifier + cached query.
   - Built-in traversal helpers: `EachChild`, `ParentOf`, `LookupChild`, `PathOf`, `SetName`, `GetName`.
   - `GetUp[T]`, `HasUp`, `TargetUp`, `flecs.MakePair`.
   - For features we don't have, use "Not yet ported in Go flecs" callouts: configurable cleanup policies (other than ChildOf's hardcoded cascade-delete), per-hierarchy name scoping (`ecs_set_scope`), reparenting that preserves children (need to verify our impl supports this; if so, document it).

2. **Verify code blocks.** Create `docs/hierarchies_examples_test.go` (package `docs_test`) with one `TestHierarchies_*` function per code block. Run `go test ./docs/...`.

3. **Update `docs/README.md`**: HierarchiesManual row → `✅ landed / 14.4`. Append discovered gaps. Most hierarchy concepts are already covered in our port; expect 2-4 new gaps (configurable cleanup policy, scope push/pop, hierarchy-relative iteration helpers).

4. **Update `ROADMAP.md`**: 14.4 row → `✅ shipped (v0.23.0)`. **DO NOT bump the "Shipped through vX" heading; that happens at release-tag time.**

5. **Update `CHANGELOG.md`** with an Unreleased entry. Mark it `Unreleased — Phase 14.4: HierarchiesManual doc port (upcoming v0.23.0)`.

6. **Cross-link** with Relationships (the parent doc), Quickstart (hierarchies section), and Queries (Cascade traversal).

### Non-goals

- NO source code changes. Docs-only.
- NO porting beyond HierarchiesManual.

### Mechanical acceptance

- `go test ./docs/...` passes.
- `go vet`, `golangci-lint` clean.
- `go test ./... -race -count=1` passes.
- Cross-links resolve.
- `docs/README.md` shows HierarchiesManual as landed.

### Style notes

- Native Go.
- Show parent-first, then nested children, then deep hierarchies, then iteration.
- For each operation: name → small example → contract (what happens on edge cases like reparenting, deleting parent, looking up across scopes).
- Hierarchy-aware queries (Cascade) get a dedicated section showing root-first iteration.

## Constraints

- @docs/HierarchiesManual.md — stub to expand into the full Go port; upstream source at `/work/agents/claude/projects/SanderMertens/flecs/docs/HierarchiesManual.md`.
- @docs/Quickstart.md — tone reference; cross-link the hierarchies section.
- @docs/EntitiesComponents.md — tone reference for adapted prose style.
- @docs/Queries.md — tone reference and cross-link target for Cascade traversal.
- @docs/Relationships.md — parent doc; cross-link both ways since ChildOf is the canonical hierarchy relationship.
- @docs/README.md — port-status table: flip HierarchiesManual row to `✅ landed / 14.4` and append newly discovered gaps.
- @docs/relationships_examples_test.go — most recent verification-test pattern; mirror its structure in `docs/hierarchies_examples_test.go`.
- @childof.go — cascade-delete and ChildOf relationship implementation to cite in the cascade-delete section.
- @name.go — `SetName`, `GetName`, `PathOf`, `Lookup`, `LookupChild` implementations underpinning the hierarchical-names section.
- @traversal.go — `GetUp[T]`, `HasUp`, `TargetUp`, and pair traversal helpers used throughout the manual.
- @doc.go — package-level overview; ensure terminology stays consistent.
- @README.md — top-level project README; verify any hierarchy mentions stay aligned with the new manual.
- @ROADMAP.md — flip Phase 14.4 row to `✅ shipped (v0.23.0)`; **do NOT bump the "Shipped through vX" heading (release-tag time only)**.
- @CHANGELOG.md — add `Unreleased — Phase 14.4: HierarchiesManual doc port (upcoming v0.23.0)` entry.
- CONTRIBUTING.md "Documentation" policy — every code block must compile against v0.22.0; use Go idioms throughout.
