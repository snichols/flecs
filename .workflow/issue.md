## Why this exists

Phase 14.2 (v0.21.0) landed the Queries doc. Continuing the docs port. This phase ports the **Relationships** doc — the foundational reference on pair IDs, ChildOf, IsA, custom relationships, traversal, and relationship traits.

Upstream: `/work/agents/claude/projects/SanderMertens/flecs/docs/Relationships.md` (~6400 words, `port-adapted` in the 14.0 survey, effort: large).

**Governed by CONTRIBUTING.md "Documentation" policy.** Every code block compiles against v0.21.0. Use Go idioms throughout.

**CRITICAL: When updating the ROADMAP doc table, use v0.22.0 as 14.3's shipped version.** The prior phases had an off-by-one — 14.2 is v0.21.0, so 14.3 will be v0.22.0. Don't repeat the loop's prior copy-paste mistake.

## Deliverables

1. **Full port of `docs/Relationships.md`.** Read upstream end to end. Adapt to Go:
   - C macros (`ECS_PAIR`, `ECS_RELATIONSHIP`) → Go: `flecs.MakePair(rel, tgt)`, `flecs.SetPair[T]`.
   - C relationship-instance helpers → Go: `w.ChildOf()`, `w.IsA()`, custom relationships via `flecs.NewEntity` + `flecs.MakePair`.
   - C traits (`EcsExclusive`, `EcsSymmetric`, `EcsTransitive`, `EcsAcyclic`, `EcsTraversable`, `EcsTraverse`) — these are mostly NOT ported in our Go flecs yet. Use the "Not yet ported in Go flecs" callout pattern and link to the corresponding gap entries in `docs/README.md`. Don't fake them.
   - Built-in relationships ChildOf and IsA: cite our existing implementations (`childof.go`, `isa.go`).
   - The relationship-as-component pattern (data on pairs): show `flecs.SetPair[T]`, `flecs.GetPair[T]`, `flecs.GetPairRef[T]`.
   - Wildcard targeting (`(Likes, *)` in C) is NOT ported — call out.

2. **Verify every code block.** Create `docs/relationships_examples_test.go` (`package docs_test`) with one `TestRelationships_*` function per code block. Pattern: copy from Quickstart/EntitiesComponents/Queries test files. Run `go test ./docs/...` to verify.

3. **Update `docs/README.md`**: change Relationships row to `✅ landed / 14.3`. Append any newly discovered gaps. This doc will surface many — relationship traits in particular are nearly all unported. Expect 5-10 new gaps.

4. **Update `ROADMAP.md`**: change the 14.3 row to `✅ shipped (v0.22.0)`. (Do NOT bump the "Shipped through vX" heading; that happens at tag time.)

5. **Update `CHANGELOG.md`** with an Unreleased entry — DO NOT pre-attribute it to v0.21.0 or earlier; leave it as Unreleased and note that the upcoming release will be v0.22.0.

6. **Cross-link** with Quickstart (relationships section), EntitiesComponents (pair ID encoding), Queries (relationship matching in terms).

## Non-goals

- NO source code changes. Docs-only.
- NO porting beyond Relationships.
- NO faking unported features.
- NO bumping the Shipped heading in ROADMAP — that's the release-tag commit's job.

## Mechanical acceptance

- `go test ./docs/...` passes.
- `go vet`, `golangci-lint run` clean.
- `go test ./... -race -count=1` clean.
- Cross-links resolve.
- `docs/README.md` shows Relationships as landed.

## Relevant files

- @docs/Relationships.md (stub → full port)
- @docs/Quickstart.md, @docs/EntitiesComponents.md, @docs/Queries.md (tone reference)
- @docs/README.md (status + gap list)
- @docs/queries_examples_test.go (most recent test pattern)
- @childof.go, @isa.go (the two relationships we have implemented)
- @id.go (pair encoding)
- @doc.go, @README.md, @ROADMAP.md, @CHANGELOG.md

**Upstream:** `/work/agents/claude/projects/SanderMertens/flecs/docs/Relationships.md`

## Style notes

- Native Go phrasing.
- Show the simple form first: `flecs.MakePair(rel, tgt)`, `entity.AddID(pairID)`. Then escalate to typed `SetPair[T]`, `GetPair[T]`.
- For relationship traits (most unported), use a consistent callout block: a markdown `> **Not yet ported in Go flecs.**` quote noting the C name, what it would enable, and a link to the gap entry.
- Length: structure with a clear ToC at the top.
