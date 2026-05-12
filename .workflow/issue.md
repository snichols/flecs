## Goal

Phase 14.1 (v0.20.0) landed the EntitiesComponents doc. Continuing the docs port. This phase ports the **Queries** doc — the most substantial document in upstream flecs (~19,000 words). It covers cached vs uncached queries, term builder, NOT/Optional/OR terms, traversal modifiers, change detection, iteration semantics, performance characteristics, and best practices.

Upstream source: `/work/agents/claude/projects/SanderMertens/flecs/docs/Queries.md`. Classified `port-adapted` in the 14.0 survey; effort: large.

**Governed by CONTRIBUTING.md "Documentation" policy.** Every code block must compile against current master (v0.20.0). Use Go idioms — generics, `world.Read`/`world.Write` scopes, `Each*`/`NewQuery`/`NewQueryFromTerms` patterns.

### Deliverables

1. **Full port of `docs/Queries.md`.** Read upstream end to end. Adapt every section to the Go API:
   - C macros (`ECS_TERM_COMPONENT`, etc.) → Go term builder (`flecs.With[T]()`, `flecs.Without[T]()`, `flecs.Maybe[T]()`, `flecs.Or[T1, T2]()`).
   - C iter patterns (`while (ecs_iter_next(&it))`) → Go `it := q.Iter(r); for it.Next() { ... }`.
   - C term-source / term-trav syntax → `.Up(rel)`, `.SelfUp(rel)`, `.Cascade(rel)`, `.Self()`.
   - C cached-query callbacks → `*CachedQuery` + `Iter` pattern + `Changed()`.
   - Anywhere a section describes a feature we don't have (query DSL, query groups, transitive relationships, wildcard/any, table-source iteration, etc.), insert a "Not yet ported in Go flecs" callout linking to the relevant feature-gap entry in `docs/README.md`.
   - Performance section: keep the conceptual content, reference our BENCH.md for actual numbers, mention the cost of Up traversal and the v0.16 per-stage queue optimization.

2. **Verify every code block compiles.** Add a `docs/queries_examples_test.go` file (`package docs_test`) with one `TestQueries_*` function per code block. Pattern: copy-paste the Go code into a test, add the minimal setup. Run `go test ./docs/...` to verify.

3. **Update `docs/README.md`**: change the Queries row from "pending / 14.2" to "landed / 14.2". Append any newly discovered feature gaps to the gap list. This is a large doc — expect 3-8 new gaps surfaced (query groups, group_by ordering, wildcard query terms, sort_table, table sources, custom callbacks, slot reuse, query iteration on a single table, etc.).

4. **Update `ROADMAP.md`**: change the 14.2 row to landed. Do NOT bump the "Shipped through vX" heading — that happens at release-tag time.

5. **Update `CHANGELOG.md`** with an Unreleased entry summarizing what landed.

6. **Cross-link**: in the Queries doc, link to Quickstart and EntitiesComponents where relevant. They should feel like a connected reference, not isolated pages.

### Non-goals

- NO source code changes. Docs-only. If you discover a bug, file a separate issue.
- NO porting of any docs beyond Queries.
- NO fake-it-till-you-make-it for features we don't have. Use the "Not yet ported" callout pattern.

### Mechanical acceptance

- `go test ./docs/...` passes. Every code block in `docs/Queries.md` has at least one corresponding test in `docs/queries_examples_test.go`.
- `go vet ./...` and `golangci-lint run ./...` clean.
- `go test ./... -race -count=1` passes.
- Cross-links resolve.
- `docs/README.md` shows Queries as landed.
- `CHANGELOG.md` Unreleased entry summarizes what landed plus the new gap count.

### Style notes (Quickstart and EntitiesComponents set the bar)

- Native Go phrasing. No "ECS world handle"; just "world." Replace C-isms.
- Code blocks self-contained. Each runnable inside a test function with minimal setup.
- Use `flecs.RegisterComponent[T]`, `world.Read`/`world.Write`, `Each1`/`Each2`/`NewQuery`/`Iter` consistently.
- Show the simplest API first, then the powerful one. E.g.: introduce `Each1` before `NewQueryFromTerms`.
- Performance characteristics: defer to BENCH.md for numbers; explain *why* something is fast/slow rather than quoting ns/op.
- Length: this is a large doc; structure it with clear `##` headings. Use a table of contents at the top.

**Upstream source:** `/work/agents/claude/projects/SanderMertens/flecs/docs/Queries.md`

## Constraints

- @docs/Queries.md — currently a stub; this phase fully replaces it
- @docs/Quickstart.md — reference for tone; query examples there are the gold standard
- @docs/EntitiesComponents.md — reference for tone (Phase 14.1 landed doc)
- @docs/README.md — status table + feature-gap list to update
- @docs/quickstart_examples_test.go — test pattern reference (one Test per code block)
- @docs/entities_components_examples_test.go — test pattern reference
- @doc.go — package-level overview; keep terminology consistent
- @README.md — top-level intro; keep terminology consistent
- @ROADMAP.md — 14.2 row needs landed marker (do NOT bump "Shipped through vX")
- @CHANGELOG.md — add Unreleased entry summarizing port + new gap count
- @query.go — reference for the Term builder API (With/Without/Maybe/Or, Up/SelfUp/Cascade/Self)
- @cached_query.go — reference for CachedQuery API (Iter, Changed)
- CONTRIBUTING.md "Documentation" policy — every code block must compile against current master
