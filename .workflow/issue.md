## Goal

Phase 14.0 (v0.19.0) landed the docs scaffold with a full Quickstart and skeleton stubs for 12 follow-on doc phases. This phase ports the **EntitiesComponents** doc — the foundational reference on entity lifecycle, IDs, component registration, tags, and pair IDs. Required reading after Quickstart.

Upstream source: `/work/agents/claude/projects/SanderMertens/flecs/docs/EntitiesComponents.md` (~7000 words, classified `port-adapted` in the 14.0 survey).

**This phase is governed by the new CONTRIBUTING.md "Documentation" policy.** Every code example must compile against current master (v0.19.0). Use Go idioms throughout — generics, callbacks, error returns where appropriate. Don't translate C-isms (manual hooks, C-specific lifecycle quirks); replace them with the Go equivalent.

### Deliverables

1. **Full port of `docs/EntitiesComponents.md`.** Read the upstream file end to end. Adapt every section to the Go API:
   - Replace C function calls with their Go equivalents (`ecs_new` → `fw.NewEntity()`, `ecs_get_id(...)` → `flecs.Get[T](r, e)`, etc.).
   - Replace C type declarations with `flecs.RegisterComponent[T](w)`.
   - Replace C macro patterns (`ECS_COMPONENT`, `ECS_TAG`) with the Go API.
   - Keep the *content* — the explanations of entity ID structure, component identity, sparse vs dense, tags as components, pair IDs — but adapt the surrounding code.
   - If a section describes a feature we don't have (e.g. C's `ecs_set_scope`, sparse storage trait, full cleanup policies), do NOT fake it. Mark the section with a "Not yet ported in Go flecs" callout and link to the relevant feature-gap entry in `docs/README.md` so the operator knows.

2. **Verify code blocks compile.** Add code paths to `docs/entities_components_examples_test.go` (new file, package `docs_test`) that exercise every code block. Run `go test ./docs/...` to verify. Pattern: copy the code-blocks from the Markdown, wrap each in a `TestExample_*` function. Use stable function naming (TestExample_NewEntity, TestExample_RegisterComponent, etc.) so the markdown line can reference the test if helpful.

3. **Update `docs/README.md`**: change the EntitiesComponents row from "pending / 14.1" to "✅ landed / 14.1". The other rows stay pending.

4. **Update `ROADMAP.md`**: change the 14.1 row in the Documentation table to landed/shipped status. Bump the "Shipped (through vX)" heading on tagging (do not bump in the issue work itself; the release-tag commit handles that).

5. **Update `CHANGELOG.md`** with an Unreleased entry summarizing what landed.

6. **Surface any feature gaps you discover** while porting. The 14.0 survey listed 17 candidate gaps; reading EntitiesComponents end to end will likely surface 1-5 more (especially around component traits and sparse storage). Add discovered gaps to the feature-gap list in `docs/README.md` — append, don't replace. Format: bullet item with the C feature name, a one-line description, and a `not yet ported in Go flecs` tag. Do NOT file new follow-up issues; the operator decides which to pursue.

### Non-goals

- NO source code changes. This is docs-only. If you discover a bug while porting, file a separate issue rather than fixing inline.
- NO porting of any docs beyond EntitiesComponents. The other stubs stay stubs.
- NO removal of the upstream C reference — `docs/README.md` continues to cite the upstream path.
- NO change to the public API surface.

### Mechanical acceptance

- `go test ./docs/...` passes. Every code block in `docs/EntitiesComponents.md` has at least one corresponding test in `docs/entities_components_examples_test.go`.
- `go vet ./...` and `golangci-lint run ./...` clean.
- `go test ./... -race -count=1` passes (the new test file shouldn't break anything).
- All cross-links between Markdown files resolve.
- `docs/README.md` reflects EntitiesComponents as landed.
- `CHANGELOG.md` Unreleased entry summarizes what landed.

### Style notes (critical — Quickstart is the bar)

- Native-Go phrasing, not translated-C. Use "register a component" not "create a component declaration."
- Code examples must be self-contained — each fenced code block should be runnable inside a test function with minimal setup.
- Use generics over `interface{}` or `unsafe`.
- Use `world.Read`/`world.Write` scopes for all entity mutation/read examples (the v0.15.0+ API).
- Use `Each1`/`Each2`/`NewQuery`/`Iter` patterns from Quickstart, not lower-level table iteration.
- When mentioning a feature that's a follow-on (e.g. observers, hooks), link to the relevant docs/ file (even if a stub).

### Upstream source

- `/work/agents/claude/projects/SanderMertens/flecs/docs/EntitiesComponents.md`

## Constraints

- @docs/EntitiesComponents.md — currently a stub; this phase replaces with the full port
- @docs/Quickstart.md — reference for tone/style; Quickstart is the gold standard
- @docs/README.md — status table + feature-gap list (update EntitiesComponents row to landed; append discovered gaps)
- @docs/quickstart_examples_test.go — reference for test pattern
- @doc.go — cross-link to EntitiesComponents may want updating
- @README.md — cross-link to EntitiesComponents may want updating
- @ROADMAP.md — status row update (14.1 → landed)
- @CHANGELOG.md — Unreleased entry summarizes what landed
- CONTRIBUTING.md Documentation policy: every code example must compile against current master (v0.19.0); use Go idioms (generics, callbacks, error returns), not translated C-isms
