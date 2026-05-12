## Goal

Continue the docs port: bring `docs/ComponentTraits.md` from upstream into the Go flecs port, adapted for what's actually implemented. **This is the most gap-heavy doc remaining** — the reference on the trait system (per-component and per-relationship flags that modify ECS behavior).

Phases 14.0–14.7 shipped (v0.19.0–v0.26.0). Upstream source: `/work/agents/claude/projects/SanderMertens/flecs/docs/ComponentTraits.md` (~7400 words, classified `port-with-gaps` in the 14.0 survey, effort: medium).

**Governed by CONTRIBUTING.md "Documentation" policy.** Code blocks compile against v0.26.0.

**Target version: v0.27.0.**

### Critical context — what traits Go flecs has vs C

**Implemented in Go flecs:**
- `Inheritable` (Phase 13.1, v0.18.0) — `SetInheritable[T](w)` flag on `TypeInfo`. Auto-promotes query terms to `Self|Up(IsA)`. Wired up end to end.
- Trait entity IDs from Phase 13.1 (v0.18.0): `World.OnInstantiate()`, `World.Inherit()`, `World.Override()`, `World.DontInherit()` — the IDs exist but only `Inherit` has behavior wired up. `Override`/`DontInherit` are surfaced for API symmetry; the on-instantiate behavior they imply is not implemented.

**NOT implemented in Go flecs (most of this doc):**
- `Exclusive` — only one target allowed for a given relationship per entity (e.g. ChildOf is implicitly exclusive in our port but no general mechanism)
- `Symmetric` — auto-mirror: `Add(a, Friend, b)` also adds `Add(b, Friend, a)`
- `Transitive` — auto-chain: if `Add(a, LocatedIn, b)` and `b LocatedIn c`, then `a LocatedIn c`
- `Acyclic` — disallow cycles in a relationship
- `Traversable` — make this relationship traversable by query `Up`/`SelfUp`/`Cascade` (we currently allow any relationship for traversal)
- `Reflexive` — `Has(a, R, a)` is always true
- `PairIsTag` — treat data-bearing pair as a tag (no storage)
- `Sparse` — opt a component out of archetype-SoA into sparse map storage
- `Singleton` — only one instance allowed
- `CanToggle` — disable/enable a component without removing it
- `Constant` — read-only component (cannot Set after Add)
- `DontFragment` — co-locate components on the same table to avoid fragmentation
- `Union` — union-pair semantics
- `OrderedChildren` — ChildOf-specific: preserve child insertion order
- `OnDelete(action)` — cleanup policy for entities holding this component
- `OnDeleteTarget(action)` — cleanup policy for relationship targets

## Deliverables

1. **Full port of `docs/ComponentTraits.md`** adapted to Go:
   - Lead with what's implemented: `SetInheritable[T]` + the four built-in trait entity IDs.
   - Describe the unified trait model conceptually (traits are pairs of `(TraitMarker, Value)` added to component or relationship entities).
   - For each unimplemented trait: section header, what it would do, current workaround (if any), explicit `Not yet ported in Go flecs` callout, link to gap entry.
   - The doc should be MOSTLY unported-feature callouts — this is honest documentation of where the port is incomplete.

2. **Verify code blocks.** Create `docs/component_traits_examples_test.go` with `TestComponentTraits_*` for the implemented traits (Inheritable, the four trait IDs). Most code blocks for unimplemented traits will live in markdown but NOT in test files (no point testing nonexistent behavior).

3. **Update `docs/README.md`**: ComponentTraits row → `✅ landed / 14.8`. Most relevant trait gaps already listed across prior phases — confirm and consolidate, don't duplicate. New gaps to surface: Reflexive, Constant, DontFragment, Singleton trait, Union trait. Anything that wasn't in the gap list yet gets added.

4. **Update `ROADMAP.md`**: 14.8 row → `✅ shipped (v0.27.0)`. Do NOT bump the heading.

5. **Update `CHANGELOG.md`** with `Unreleased — Phase 14.8: ComponentTraits doc port (upcoming v0.27.0)`.

6. **Cross-link** heavily — this doc references every other doc. Quickstart, EntitiesComponents, Queries, Relationships, PrefabsManual, ObserversManual.

7. **Surface the consolidated trait-system roadmap**. At the bottom of this doc, add a section titled "Trait system roadmap" that lists every trait, current status (✅ shipped / ⏳ planned / 🟡 partial), and a brief note. This becomes the canonical reference for trait-system planning.

## Non-goals

- No source changes.
- No porting beyond ComponentTraits.

## Mechanical acceptance

- `go test ./docs/...` passes.
- `go vet`, `golangci-lint` clean.
- `go test ./... -race -count=1` passes.
- `docs/README.md` shows ComponentTraits as landed.
- Trait system roadmap section is unambiguous about what's shipped vs not.

## Style notes

- Honest about gaps — this doc's value is in being a clear map of "what works, what doesn't."
- Use a consistent callout block for unimplemented traits.
- The trait system roadmap section is the most valuable addition — make it accurate, scannable, and link-rich.

## Constraints

- @docs/ComponentTraits.md — stub to expand into full port
- @docs/Quickstart.md — tone and structure reference
- @docs/PrefabsManual.md — tone and structure reference
- @docs/Relationships.md — tone and structure reference; trait-heavy doc that this one cross-links to
- @docs/README.md — index page where ComponentTraits row flips to landed; gap list to consolidate/extend
- @docs/observers_examples_test.go — test pattern for `TestComponentTraits_*` examples
- @scope.go — implemented trait surface (Inheritable scope plumbing)
- @world.go — trait entity IDs: `OnInstantiate`, `Inherit`, `Override`, `DontInherit`
- @inheritance_test.go — real working examples of the implemented `Inheritable` trait
- @internal/component/typeinfo.go — the `Inheritable` bool on `TypeInfo`
- @doc.go — package overview; may need cross-link nudge
- @README.md — top-level README; reference for tone
- @ROADMAP.md — 14.8 row flips to shipped (v0.27.0); do not bump heading
- @CHANGELOG.md — add `Unreleased — Phase 14.8: ComponentTraits doc port (upcoming v0.27.0)`
- Upstream source: `/work/agents/claude/projects/SanderMertens/flecs/docs/ComponentTraits.md`
- CONTRIBUTING.md "Documentation" policy governs this port; code blocks must compile against v0.26.0.
