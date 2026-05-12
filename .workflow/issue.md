## Goal

The C flecs library ships substantial Markdown docs at `/work/agents/claude/projects/SanderMertens/flecs/docs/`. Our Go port currently has only `README.md`, `doc.go` (package docs), `CHANGELOG.md`, and `ROADMAP.md`. To make the Go port discoverable and usable by people coming from C flecs (or new to ECS), we need to port the conceptual content: Quickstart, Queries, Relationships, Hierarchies, Prefabs, Systems, Observers, Component Traits, FAQ, Entities & Components, Design With Flecs, Manual, etc.

This phase is **survey + plan + execute Quickstart**:

1. Survey every C doc file
2. Classify each (`port-as-is`, `port-adapted`, `port-with-gaps`, `skip`)
3. Identify features the docs describe that our Go port doesn't have — those become candidate follow-up issues (listed here for the operator, not filed)
4. Produce a `docs/` directory structure with skeleton files for each planned port
5. Execute the first port: `docs/Quickstart.md`, fully written and verified to compile against v0.18.0

Subsequent phases (14.1, 14.2, …) will drain the survey queue one doc at a time.

**Process change (carry into all future phases):** every phase from this point forward must include an explicit "update docs accordingly" deliverable. The design agent must add it. The review agent must check it. This is an operator directive.

## Deliverables

### 1. Survey table

As the first work item, read every file in `/work/agents/claude/projects/SanderMertens/flecs/docs/` (excluding the `cfg/` and `img/` subdirs unless content is referenced). For each, produce a row with:

- Filename (C side)
- Word count / approximate page count
- Classification: `port-as-is` (conceptual content maps cleanly), `port-adapted` (needs Go syntax + Go ergonomics rewrite), `port-with-gaps` (describes features we don't have; gaps listed), `skip` (C-specific: build system, C DSL, C migration guide, C HTTP server, etc.)
- Mapped Go-side filename if ported (e.g. `docs/Queries.md`)
- Estimated effort: small / medium / large

The likely classifications (verify by reading):

- `Quickstart.md` — port-adapted
- `EntitiesComponents.md` — port-adapted
- `Queries.md` — port-adapted (large)
- `Relationships.md` — port-adapted (large)
- `HierarchiesManual.md` — port-adapted
- `PrefabsManual.md` — port-adapted
- `Systems.md` — port-adapted
- `ObserversManual.md` — port-adapted
- `ComponentTraits.md` — port-with-gaps (we have Inheritable in v0.18 but C has many more traits)
- `FAQ.md` — port-adapted
- `DesignWithFlecs.md` — port-as-is (conceptual)
- `Manual.md` — port-adapted (large; may split)
- `BuildingFlecs.md` — skip (replace with a "Building the Go Module" section in README)
- `MigrationGuide.md` — skip (C version migration; not relevant to Go)
- `FlecsRemoteApi.md` — port-with-gaps (we have `rest.go` but coverage may differ)
- `FlecsScript.md`, `FlecsScriptTutorial.md`, `FlecsQueryLanguage.md` — skip (the C DSL is not ported; Go is the host language)
- `Docs.md` — port-adapted as a TOC for our `docs/` directory

### 2. Feature-gap list

As you survey, accumulate a list of every flecs feature mentioned in the docs that our Go port does NOT have. Examples to look for:

- Query language (DSL)
- Entity hooks beyond OnAdd/OnSet/OnRemove (e.g. OnDelete, OnTableEmpty)
- Reflection / runtime metadata beyond what `meta.go` exposes
- Sparse storage opt-in
- World-level pre/post merge hooks
- Deferred queries
- Alerts addon
- Monitor addon
- Units addon
- System stats granularity
- Query groups
- Hierarchies-by-symbol
- World snapshots
- Transitive relationships (`Trav`)

Each gap becomes a candidate follow-up issue. **Do NOT file them in this phase** — list them in the implementation summary so the operator can prioritize.

### 3. `docs/` directory structure

Create `docs/` at the repo root with:

- `docs/README.md` — short index linking to planned files; mark which are landed vs. pending
- `docs/Quickstart.md` — fully written hello-world walkthrough covering: world creation, entities, components, Each iteration, queries, prefabs, systems. Use Go syntax. Examples must compile against current master (v0.18.0). Each code block should be runnable; consider matching them to existing `example_*_test.go` files where possible.
- Skeleton stub files for the other planned ports: title + one-line description + a placeholder comment `<!-- TODO: port from /work/agents/claude/projects/SanderMertens/flecs/docs/<FileName>.md (Phase 14.x) -->`. This makes follow-on phases trivially scoped.

### 4. Update `README.md`

Link to the new `docs/` directory and call out the Quickstart prominently.

### 5. Update `doc.go`

Update the package overview to mention `docs/` as the authoritative reference for conceptual documentation.

### 6. Update `ROADMAP.md`

Add a new section under "Documentation" (or similar) listing the docs port phases and their status. The first row: `14.0 — Survey + Quickstart — _this phase_`. Reserve rows for 14.1+ based on the survey queue.

### 7. Update `CHANGELOG.md`

Unreleased section: enumerate what landed (survey, docs scaffold, Quickstart, README/doc.go/ROADMAP updates).

## Non-goals

- NO porting of any docs beyond Quickstart. Skeleton stubs only for the rest.
- NO filing of follow-up issues for the feature gaps. List them in the implementation summary; the operator decides which to pursue.
- NO change to source code APIs. This is docs-only.
- NO porting of any C-specific docs (`BuildingFlecs`, `MigrationGuide`, `FlecsScript*`, `FlecsQueryLanguage`) — those become `skip` rows in the survey.

## Writing style

The Quickstart must feel native-Go, not "translated C." Use Go idioms (generics, `func` literals, `error` returns where appropriate, `slog` for logging). Each example small and self-contained. Reference existing `example_*_test.go` files for tone — they're already runnable, idiomatic Go.

## Mechanical acceptance

- All Quickstart code blocks compile against v0.18.0. The implementer must document the verification approach (e.g. extract blocks into a test file, or a `go test ./docs/...` style runner).
- `go vet ./...` clean.
- `golangci-lint run ./...` clean (no Go source changes, but check any new test file added for doc verification).
- `go test ./... -race -count=1 -timeout=180s` passes.
- All cross-links between Markdown files resolve (`docs/README.md` → `docs/Quickstart.md` works; relative paths to source files work).
- `doc.go` and `README.md` reference `docs/` correctly.

## Constraints

- @README.md — current top-level entry point; must be updated to link to `docs/` and call out the Quickstart.
- @doc.go — package overview shown by `go doc` and pkg.go.dev; must reference `docs/` as the authoritative conceptual reference.
- @ROADMAP.md — phase planning document; add a Documentation section listing 14.x phases and their status.
- @CHANGELOG.md — Unreleased section gets entries for the survey, scaffold, and Quickstart.
- C reference root: `/work/agents/claude/projects/SanderMertens/flecs/docs/` — read every `.md` here (skip `cfg/`, `img/` unless referenced) to build the survey.
- Reference for Go idioms: existing `example_*_test.go` files in the repo root — match their tone for the Quickstart.
- Quickstart must compile against v0.18.0 (current master tip is `44b3946 release: v0.15.0` per `git log`, but `ROADMAP.md` and `CHANGELOG.md` will indicate the actual current version; verify before writing examples).
- New `docs/` directory at repo root does not yet exist — this phase creates it.
- Process change (operator directive): every phase from 14.0 onward must include an explicit "update docs accordingly" deliverable, and the review agent must verify it. The design agent adds this to each phase brief going forward.
