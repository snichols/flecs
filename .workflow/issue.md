## Goal

Phase 8.1 of the Go port of flecs. The ECS is feature-complete for v0 after Phase 7 (master HEAD `a7d4de9`), well-tested (97%+ coverage), and free of third-party deps — but the **public-facing documentation is minimal**. A Go developer landing on `pkg.go.dev/github.com/snichols/flecs` would struggle to get started.

This phase makes the project legible. After it lands, a Go developer reading the repo or pkg.go.dev page can:

1. Understand what flecs is and why they'd use it (README).
2. Find an executable "hello world" within 30 seconds (README quick-start).
3. Browse the API surface with consistent godocs and runnable examples (pkg.go.dev).
4. Find recipes for common patterns: hierarchies, prefabs, system pipelines, deferred mutation, observers, etc.

**Scope: comprehensive user-facing documentation + runnable examples. No new ECS features.**

### Deliverables

**1. `README.md` overhaul** (target 150-300 lines, skimmable):

- Top: one-paragraph hook. *"An idiomatic, high-performance Go port of the flecs ECS library. Archetype-based storage, generic-typed API, zero third-party dependencies."*
- Badges (Go version, license, link to upstream).
- **Quick start:** a single, copy-pasteable `package main` + `func main` block that compiles and runs. Shows: create world, register components, create entities, set/get, iterate via `Each2`, basic `Progress` loop. ~30 lines.
- **Core concepts (brief):** entities, components, archetypes, queries, pipelines. One paragraph + short snippet each.
- **Feature index** with anchor links:
  - Generic typed API (`Set[T]`, `Get[T]`, `Each2[A,B]`, `Field[T]`)
  - Archetype-based SoA storage (no pointer chasing in iteration)
  - Pair IDs / relationships (`ChildOf` cascade, `IsA` inheritance)
  - Hierarchical entity names + path lookup
  - Hooks + multi-subscriber observers
  - Deferred command queue
  - Systems + 4-phase pipeline + fixed timestep
- **Comparison to upstream flecs (C):** brief table/list — what's ported, what's deferred (table-graph traversal queries, addons like REST/JSON/meta, multi-threading).
- **Installation:** `go get github.com/snichols/flecs`.
- **Status:** "Pre-1.0; API may evolve. See ROADMAP.md for deferred work."
- **License:** MIT.
- **Acknowledgments:** link upstream flecs and credit Sander Mertens.

**2. `doc.go`** at repo root — package-level godoc:

- Package comment: 1-2 paragraphs introducing the package and major concepts (World, Entity, Component, Archetype, Query, System).
- Cross-references to subpackages where useful.
- Sections (godoc `## Heading` convention): "Creating a World", "Components", "Queries", "Pipelines and Systems", "Relationships", "Deferred Mutation". Each: 3-5 sentences + snippet.

**3. Runnable examples** as `example_*_test.go` files in the repo root (Go convention; pkg.go.dev renders inline):

- `example_basic_test.go` — `ExampleWorld_basic()`: world creation, component registration, entity creation, Set/Get.
- `example_query_test.go` — `ExampleQuery()`, `ExampleCachedQuery()`, `ExampleEach2()`: iteration patterns.
- `example_pairs_test.go` — `ExampleMakePair()`, `ExampleAddID()`: tag pairs and data pairs.
- `example_childof_test.go` — `ExampleWorld_childOf()`: 3-level hierarchy, `EachChild`/`ParentOf`/cascade delete.
- `example_isa_test.go` — `ExampleWorld_isA()`: prefab with components; child inherits; explicit override; remove restores inheritance.
- `example_name_test.go` — `ExampleWorld_name()`: SetName + Lookup + PathOf round-trip.
- `example_hooks_test.go` — `ExampleOnSet()`: register OnSet hook, demonstrate fire on Set.
- `example_observer_test.go` — `ExampleObserve()`: two Observers on EventOnSet; both fire; Unsubscribe one.
- `example_defer_test.go` — `ExampleWorld_defer()`: Defer-wrap an iteration with mutations; cross-phase visibility.
- `example_pipeline_test.go` — `ExamplePipeline()`: full pipeline (PreUpdate/OnUpdate/PostUpdate + OnFixedUpdate physics).

Each example MUST compile and produce deterministic output via `// Output:` annotations where reasonable. For non-deterministic output (entity IDs, unordered iteration), use `// Unordered output:` or omit. Keep examples 15-30 lines, demonstrating ONE concept each.

**4. `ROADMAP.md`** — short, honest:

- **Shipped:** the Phase 1-7 feature list (storage, raw-ID API, queries, ergonomic iteration, relationships, lifecycle, deferred commands, systems + pipeline).
- **Future work** (no internal phase numbering):
  - NOT/Optional/OR query terms
  - Up/Down traversal modifiers
  - Change detection
  - Query-time IsA inheritance
  - Addons: meta/reflection, REST, JSON, stats, log
  - Multi-threading
  - Custom allocators / `sync.Pool`
- **Performance work** subsection mentioning benchmark TBD.
- Brief contribution note: *"issues welcome; PRs by arrangement."*

**5. Godoc cleanup pass on all exported symbols:**

- Audit each `.go` file in the root flecs package; ensure every exported function, method, type, field, and constant has a Go-idiomatic godoc comment beginning with the symbol's name.
- Bring inconsistent comments into a consistent voice (terse, declarative, "returns X when Y" style).
- Particularly tighten: `Progress`, `Defer`, `NewSystem`, `Observe`, `Each<N>`, `MakePair`, `ChildOf`/`IsA`/`Name` accessors.
- Internal packages (`internal/...`) get a light pass — not user-facing but should still be self-documenting.

**6. `CONTRIBUTING.md`** (~30-50 lines):

- Build: `go test ./... -race && golangci-lint run`
- Architecture overview: paragraph describing package layout (root + `internal/ids`, `internal/component`, `internal/storage/*`).
- Style: gofmt + golangci-lint clean; coverage ≥ 90% on touched files; reference the autonomous loop briefly (*"automated PRs land via the snichols/queued workflow"*).

**7. `CHANGELOG.md`** — initial entry:

- `## v0.1 (unreleased)`
- Phase 1-7 feature list condensed.
- *"No breaking changes from prior versions."*

**8. No new tests for ECS behavior.** This is a docs phase; behavior is unchanged. Existing tests must still pass.

### Mechanical acceptance

- `go test ./... -race -count=2` passes (all existing tests + new `Example*` functions which `go test` automatically runs).
- `go vet ./...` clean.
- `golangci-lint run` clean.
- Coverage on `flecs` ≥ 90% (no regression from 97.2%). Examples MAY raise coverage; great if so.
- All `Example*` functions compile and produce expected output.
- All exported symbols in root `flecs` package have godoc beginning with the symbol's name.
- README.md, doc.go, ROADMAP.md, CHANGELOG.md, CONTRIBUTING.md all exist.

### Non-goals

- NO new ECS features.
- NO changes to existing public API signatures.
- NO performance optimizations (Phase 8.3 territory).
- NO benchmark code (Phase 8.2).
- NO new addons (REST, JSON, meta, stats, log all deferred).
- NO scripting language support.
- NO interop with upstream C flecs.
- NO migration tools.
- NO video tutorials or hugo-style docs site (just README + godoc).

### Implementer pointers

- Examples MUST be runnable. `go test ./...` runs Example functions; ensure they don't panic or hang.
- Use `// Output:` (or `// Unordered output:`) annotations so `go test -run Example` verifies them. Skip only if output is genuinely non-deterministic (entity IDs in iteration order are NOT deterministic without sorting).
- For examples involving iteration order, either sort output before printing or use `// Unordered output:`.
- The README's quick-start code block MUST compile if copy-pasted into a fresh `main.go`. Literally test this by creating a tempfile and running it.
- DO NOT introduce a `go run ./examples/...` directory; use idiomatic `Example*` test functions. pkg.go.dev renders them.
- DO NOT change any existing source code semantics. Godoc edits to existing files are fine; behavior changes are not.
- DO NOT add third-party dependencies (still pure stdlib).
- DO NOT remove existing comments — augment them.
- DO NOT lengthen the API surface. If you find a missing accessor that an example needs, note it as a follow-up; this is a docs phase.

## Constraints

- @world.go — godoc cleanup target; entry-point types and constructors users land on first.
- @id.go — godoc cleanup target; `Entity`/`ID` semantics surfaced in nearly every example.
- @id_ops.go — godoc cleanup target; `AddID`/`RemoveID`/`HasID`/`OwnsID` and pair-data accessors.
- @query.go — godoc cleanup target; `NewQuery`, `Query.Iter/Each/Terms`, `QueryIter`, `Field[T]`.
- @cached_query.go — godoc cleanup target; cached query lifecycle + `Close()`.
- @each.go — godoc cleanup target; ergonomic `Each1`/`Each2`/`Each3`/`Each4` — must be especially clear.
- @hooks.go — godoc cleanup target; `OnAdd[T]`/`OnSet[T]`/`OnRemove[T]` single-hook API.
- @observer.go — godoc cleanup target; multi-subscriber `Observe`/`ObserveID`/`Observe2` + `Unsubscribe`.
- @defer.go — godoc cleanup target; `DeferBegin`/`DeferEnd`/`Defer`/`IsDeferred` semantics.
- @system.go — godoc cleanup target; `NewSystem`/`NewSystemInPhase`/`Progress`/timestep accessors.
- @childof.go — godoc cleanup target; `ChildOf` cascade and `EachChild`/`ParentOf`.
- @isa.go — godoc cleanup target; `IsA` inheritance via `Get`/`Has`.
- @name.go — godoc cleanup target; `SetName`/`GetName`/`Lookup`/`LookupChild`/`PathOf`/`EachChild`/`ParentOf`/`EachPrefab`/`PrefabOf`.
- @pair_internal.go — godoc cleanup target; `MakePair` + `SetPair[T]`/`GetPair[T]`.
- @README.md — currently sparse; full replacement per Deliverable 1.
- @LICENSE — MIT; keep as-is, referenced from README.
- @.golangci.yml — keep as-is; the acceptance criterion `golangci-lint run` clean must hold.
- @.github/workflows/ci.yml — keep as-is; CI must continue to pass.
- Upstream flecs README for tone/concept-ordering reference: `/work/agents/claude/projects/SanderMertens/flecs/README.md`. Don't copy text — read for tone and concept ordering.
- Pure stdlib — no new third-party dependencies.
- Not a bug; docs/feature work. Label `snichols/queued` only.
