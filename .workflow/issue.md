## Goal

Implement the **Transitive** relationship trait — the next item on the trait-system roadmap (precedents: Phase 15.0 cleanup policies through Phase 15.4 Symmetric). Transitive auto-chains a relationship at *query time*: if `(R, B)` is on entity `a` and `b` has `(R, C)`, a query treating `R` as Transitive will also match `a` as having `(R, C)`. Formally: `aRb ∧ bRc ⇒ aRc`. The canonical motivating example is spatial/containment (`LocatedIn`).

This closes the documentation callouts in `docs/Relationships.md` (lines 333–338, 567–574) and `docs/ComponentTraits.md` (lines 514–528, roadmap row at line 614) which currently flag transitive query matching for custom relationships as **not yet ported**. C reference: `EcsTransitive` (`flecs.h:1760`), the `EcsIdIsTransitive` flag bit (`api_flags.h:77`, `1u << 14`), the term-level `EcsTermTransitive` bit (`api_flags.h:190`), bootstrap registration via `flecs_bootstrap_trait` (`src/bootstrap.c:1003`) plus the auto-implication `(EcsTransitive, EcsWith, EcsTraversable)` (`src/bootstrap.c:1299`), and the query-time walker in `src/query/engine/eval_trav.c` (header comment: *Transitive/reflexive relationship traversal*).

**Target version: v0.37.0.**

**Critical implementation distinction from Symmetric (15.4)**: Symmetric mirrors *eagerly* at write time (a `(R, B)` add on `a` triggers a `(R, A)` add on `b`). Transitive must chain *lazily* at query time. Eager expansion would produce O(n²) pair writes for a chain of length n. C confirms this: it compiles a Trav opcode (`compiler_term.c:1097–1098`) and the walker runs at iteration time using a traversal cache (`eval_trav.c`, `trav_down_cache.c`, `trav_up_cache.c`). We will mirror the lazy model. C also auto-adds `EcsTraversable` to Transitive relationships at bootstrap; the Go port has no general Traversable trait yet, so this implication is a non-goal for this phase (note it for future composition).

**Likely shape (refine during research/implementation):**

1. **New built-in entity ID** `World.Transitive() ID` at **index 20** (Symmetric is index 19; world.go:70 confirms `first user entity at index 20`, so this phase shifts user entities to index 21). Self-applied tag form: `fw.AddID(relID, w.Transitive())`. Wire allocation in `world.go` `NewWorld` alongside the existing Exclusive/CanToggle/Symmetric blocks (world.go:220–237).
2. **Per-relationship flag storage**: `World.transitivePolicies map[ID]bool` keyed by `ID(relID.Index())` — same generation-stripping convention as `symmetricPolicies` (symmetric.go:39–44).
3. **Public API** (mirror symmetric.go):
   - `flecs.SetTransitive(w *World, relID ID)`
   - `flecs.IsTransitive(w *World, relID ID) bool`
   - `(*World).Transitive() ID`
   - Bare-tag form: `fw.AddID(relID, w.Transitive())` routed through `addIDImmediate` (same path Symmetric uses).
4. **Query matcher integration.** When a term `With(MakePair(R, C))` has `R` flagged Transitive:
   - Match entities that directly hold `(R, C)`, OR
   - Match entities holding `(R, B)` where `B` (or any chain from `B`) transitively reaches `C`.
   - **Cycle detection**: visited-set per walk; never revisit the same entity.
   - **Depth limit**: hard cap (mirror C if it has one; otherwise 32) to bound runaway chains.
   - Walk implemented in the uncached path first (`query.go`); see C's `eval_trav.c` `flecs_query_trav_fixed_src_up_fixed_second` (lines 80–113) which uses `ecs_search_relation` with `EcsSelf|EcsUp` to walk upward — Go equivalent is a manual BFS/DFS over `(R, *)` pairs at each step.
5. **Cached query interaction (`cached_query.go`)**: C accepts cache staleness for some traits and re-evaluates on table create. For this phase: pre-compute on construction and re-evaluate on **table create**; **do not** invalidate the cache on pair-mutation. Document the staleness clearly. Pair-mutation cache invalidation is a future enhancement and explicit non-goal here.
6. **Self-relationship edge case**: `(R, a)` on `a`. Per C, Transitive does not imply Reflexive (`Reflexive` is a separate trait at `flecs.h:1769`). Verify that an entity with no direct `(R, self)` is **not** auto-matched by transitive chains terminating at itself.
7. **Wildcard interaction**: Wildcard query terms are not in the Go port yet — note as a future composition, do not implement.

**Tests** — new `transitive_test.go`, 9+ cases:
1. **Default behavior unchanged** — a non-Transitive relationship does not chain.
2. **Simple chain** — `a (R) b (R) c`, mark R Transitive; query `With(MakePair(R, c))` matches `a` AND `b`.
3. **Longer chain** — `a (R) b (R) c (R) d` matches `a`, `b`, `c`.
4. **Branching chain** — `a (R) b`, `a (R) c`, `b (R) d`; query for d matches `a` and `b`; query for c matches only `a`.
5. **Cycle safety** — `a (R) b (R) a`; no infinite loop; both match each other.
6. **Cache interaction** — cached query with transitive term documents pre-compute / table-create re-eval; explicit table mutation after cache build covered.
7. **Depth limit** — chain longer than the bound terminates gracefully without panic.
8. **`IsTransitive` round-trip** — both `SetTransitive` and bare-tag form set the flag; tag removal unsets.
9. **Transitive + Symmetric composition** — both traits on the same `R`; Symmetric mirrors at write time, Transitive walks at query time; documented compositional semantics hold.

Plus benchmark `BenchmarkTransitiveQuery_ChainLen10` measuring the walk cost.

**Docs updates (same PR, per CONTRIBUTING.md):**
- `docs/Relationships.md` — replace the *not yet ported* callouts at 333–338 and 567–574 with shipped content + a `LocatedIn` worked example.
- `docs/ComponentTraits.md` — rewrite the Transitive section (514–528) with the shipped Go API; flip roadmap row 614 to `✅ shipped (v0.37.0)`.
- `docs/Queries.md` — brief mention of transitive-aware matching with a forward-link to Relationships.
- `docs/README.md` — prune the two gap-list entries (lines 80 and 120).
- `CHANGELOG.md` — new `## v0.37.0 — <date> — Phase 15.5: Transitive relationship trait` entry following the v0.36.0 template.
- `ROADMAP.md` — mark Phase 15.5 complete.

## Non-goals

- **NO Reflexive trait** — separate phase (`EcsReflexive` at `flecs.h:1769`).
- **NO eager transitive expansion at write time** — intentionally lazy.
- **NO automatic re-evaluation of cached queries on pair-mutation** — accept staleness; document. Pair-mutation invalidation is a future enhancement.
- **NO Wildcard query terms** — separate; will compose with Transitive when Wildcard lands.
- **NO general Traversable trait** — C auto-adds `(EcsTransitive, EcsWith, EcsTraversable)` at bootstrap; Go has no Traversable yet. Note this implication for a future phase; do not implement Traversable here.

## Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run ./...` clean.
- `go test ./... -race -count=3` clean.
- Coverage on main package ≥ 95%.
- Existing tests pass unmodified.
- New `transitive_test.go` covers the 9 cases above.
- New `BenchmarkTransitiveQuery_ChainLen10` documents the walk cost.
- Docs updates land in the same PR.

## Constraints

- @symmetric.go — direct pattern precedent for the trait API (Set/Is/`World.X()` + bare-tag form via `addIDImmediate`); mirror exactly for the Transitive flag/Set/Is API.
- @exclusive.go — earlier pattern precedent (Phase 15.2); confirms the per-relationship flag-map convention.
- @cantoggle.go — Phase 15.3 precedent; confirms how new built-in IDs are slotted into the index-allocation block.
- @instantiate_policies.go — Phase 15.0 precedent for trait-flag storage and pair-form add routing.
- @cleanup.go — the foundational trait-pattern reference (Phase 15.0).
- @world.go — built-in entity registration at index 20; extend `NewWorld` (line 220 onward) and the field block at line 70 with `transitiveID` + `transitivePolicies`. Shift the *first user entity at index 20* comment to index 21.
- @query.go — uncached matcher; add the transitive walk for pair-term matching with cycle-detection and depth limit. Ground the walk on C `eval_trav.c` semantics (lazy, EcsSelf|EcsUp traversal).
- @cached_query.go — cached matcher; pre-compute on construction, re-evaluate on table create, accept pair-mutation staleness; document the staleness model in package doc.
- @transitive_test.go — NEW; covers the 9 test cases above plus the benchmark.
- @docs/Relationships.md — Transitive section (lines 333–338, 567–574): replace *not yet ported* callouts with shipped content and a `LocatedIn` worked example.
- @docs/ComponentTraits.md — Transitive section (lines 514–528) rewritten with shipped Go API; roadmap row (line 614) flipped to `✅ shipped (v0.37.0)`.
- @docs/Queries.md — brief mention of transitive-aware matching.
- @docs/README.md — prune gap-list entries at lines 80 and 120.
- @CHANGELOG.md — new `## v0.37.0` Phase 15.5 entry following the v0.36.0 template (line 3).
- @ROADMAP.md — mark Phase 15.5 complete.
- C reference: `EcsTransitive` (`flecs.h:1760`), `EcsIdIsTransitive` flag bit `1u << 14` (`api_flags.h:77`), `EcsTermTransitive` `1u << 2` (`api_flags.h:190`), bootstrap (`src/bootstrap.c:1003`, `:1299`), compiler emission of the Trav opcode (`src/query/compiler/compiler_term.c:1097–1098`, `:1392–1398`), and the walker at `src/query/engine/eval_trav.c` — ground every claim in C.
- Style: match the trait-pattern precedent exactly. The lazy-walk-at-query-time logic with cycle detection and depth limit is the substantive new code; test coverage of the walk (chains, branches, cycles, depth) is the most important deliverable.
