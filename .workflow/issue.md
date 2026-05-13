## Goal

Port the `Traversable` relationship trait to Go flecs. Marking a relationship `Traversable` formally permits its use as the traversal relationship in query terms with `Up`, `SelfUp`, or `Cascade` modifiers. Adding `Traversable` to a relationship also implies `Acyclic` (per upstream — see [`SanderMertens/flecs/src/bootstrap.c:1295-1296`](../SanderMertens/flecs/src/bootstrap.c)).

Today in Go flecs, any entity can be used as a traversal relationship without registration (see `docs/Queries.md:410` and the "Custom traversal relationships" recipe at `docs/Queries.md:511-532`). This phase closes that gap and brings parity with C's `flecs_query_terms_validate` enforcement, while preserving user-facing source compatibility by bootstrapping `IsA`, `ChildOf`, and `DependsOn` as Traversable so existing queries continue to work without change.

Target version: **v0.46.0** (next after v0.45.0 WriteOnce).

### What ships

1. New file `traversable.go` mirroring the shape of @writeonce.go and @singleton.go:
   - `World.Traversable() ID` — built-in entity accessor (index 27 — see ordering note below).
   - `SetTraversable(w *Writer, relID ID)` — marks a relationship as traversable. **Must also call `SetAcyclic` on the same relationship** to honor the "Traversable implies Acyclic" semantic.
   - `IsTraversable(s scope, relID ID) bool` — accepts the `scope` interface (Phase 15.8 convention; see @scope.go lines 39-44 and `IsSingleton`/`IsFinal`/`IsWriteOnce` precedent).
   - `World.traversablePolicies map[ID]bool` field in @world.go alongside `acyclicPolicies`, `singletonPolicies`, `writeOncePolicies`, etc.

2. Bootstrap in @world.go (currently only `applyAcyclicPolicy(w, w.childOfID)` at line 339):
   - Bootstrap `ChildOf`, `IsA`, and `DependsOn` as Traversable (mirroring [`bootstrap.c:1063, 1315-1316`](../SanderMertens/flecs/src/bootstrap.c)).
   - Each bootstrap call also propagates Acyclic via the trait's own implication, so `IsA` becomes Acyclic for the first time in Go flecs (currently only `ChildOf` is Acyclic — see correction note below). Self-pairs are still allowed under Acyclic, so this does not break `applyReflexivePolicy(w, w.isAID)` at @world.go:335.
   - Note: Go flecs does not have a `DependsOn` built-in. Confirm whether DependsOn needs to be added as part of this phase or deferred; the safest scope is to bootstrap only `IsA` and `ChildOf` and document the DependsOn omission. **Open question for the iterate agent** — see "Decision points" below.

3. Query-time enforcement — the novel part. Unlike prior traits which enforce at write time, Traversable is enforced at **query construction**:
   - Hook point: `validateAndSortTerms` in @query.go:822 (shared by `NewQueryFromTerms` and `NewCachedQueryFromTerms` via @cached_query.go:175). Add a pass that scans every term where `t.Traverse != TraverseSelf && t.Traverse != TraverseExplicitSelf` and panics if `t.Trav` is non-zero and not in `w.traversablePolicies`.
   - Panic message must name both the traversal flag and the relationship, e.g. `flecs: NewQueryFromTerms: cannot use non-traversable relationship %v with .Up()/.SelfUp()/.Cascade(); call SetTraversable(w, relID) first`.
   - Mirrors C's check at [`src/query/validator.c:639-647`](../SanderMertens/flecs/src/query/validator.c): `if (!ecs_has_id(world, term->trav, EcsTraversable)) { ... \"cannot traverse non-traversable relationship\" ... }`.
   - The simpler `NewQuery(w, ids...)` constructor at @query.go:211 does not let users set `Trav`, but `applyInheritablePromotion` (@query.go:171-199) does set `term.Trav = w.isAID` for inheritable components. Since `IsA` will be bootstrapped Traversable, that path remains valid without extra code. Still, the validation pass in `NewQuery` should run after promotion as a defence-in-depth check.

4. Test file `traversable_test.go` with at least 9 cases (coverage ≥ 95.0%):
   1. `SetTraversable(w, R)` then a query with `.Up(R)` succeeds.
   2. Query with `.Up(R)` on a non-Traversable `R` panics with a message naming both the traversal modifier (e.g. `.Up()`) and the relationship.
   3. Same panic-naming check for `.SelfUp(R)` and `.Cascade(R)` separately (two more cases).
   4. `IsTraversable(IsA)` and `IsTraversable(ChildOf)` return `true` at world init (bootstrap check).
   5. `IsTraversable` on a vanilla user entity returns `false`.
   6. `SetTraversable(w, R)` causes `IsAcyclic(w, R)` to return `true` (Acyclic implication).
   7. `SetTraversable` round-trip + idempotence (set twice, still true; bare-tag form `fw.AddID(R, w.Traversable())` equivalent).
   8. `SetTraversable` inside a `Write` block (deferred path via cmdAddID → bare-tag dispatch in @cmd_queue.go around line 163-165).
   9. Pair-form traversal: a query with `.Up(MakePair(R, w.Wildcard()))` — confirm whether C validates against `R` (the relationship side) or the whole pair. Cite the C behavior; document the Go decision in the test and in @docs/ComponentTraits.md.

5. Doc updates (per @CONTRIBUTING.md docs convention):
   - @docs/ComponentTraits.md:729-733 — flip from "Not yet ported" to "shipped (v0.46.0)" with usage example showing `SetTraversable` and the Acyclic implication; the roadmap table row at line 812 flipped to `✅ shipped (v0.46.0)`.
   - @docs/Relationships.md:484-506 (Traversal in queries) — add a callout pointing to the new Traversable trait section.
   - @docs/Relationships.md:772-777 (Traversable section) — flip to shipped with usage example and migration note: "Existing code that traverses `IsA` or `ChildOf` continues to work without change because both are bootstrapped Traversable."
   - @docs/Queries.md:410 — update the prose "Any relationship used for traversal must be registered as traversable; the built-in `ChildOf` and `IsA` relationships are always traversable" to remove the "currently not enforced" caveat (the prose was already aspirational — match it to reality).
   - @docs/Queries.md:511-532 ("Custom traversal relationships") — update the recipe to call `SetTraversable` before using a custom relationship for traversal.
   - @docs/README.md:122 — flip the Traversable gap entry to shipped.
   - @README.md:226 — feature-index row mention (optional; consider adding a `Traversable trait` row in the same style as `WriteOnce component trait _(v0.45.0)_` at line 233).
   - @CHANGELOG.md — new `v0.46.0 — <date> — Phase 15.14: Traversable relationship trait` entry at top, following the @CHANGELOG.md:3-21 v0.45.0 template (Added / Changed sections).
   - @ROADMAP.md — heading bump `Shipped (through v0.45.0)` → `Shipped (through v0.46.0)` (line 3) and a new bullet after line 50 (the v0.45.0 WriteOnce bullet) summarizing Traversable in the standard form.

6. Marshal skip-set update in @marshal.go:101-130 — add `w.Traversable(): {}` to the skip map. Also update @marshal_test.go:51 baseline.

7. Baseline test fixups (built-in count 28 → 29):
   - @meta_test.go:18 — `const builtinEntityCount = 28` → `29`; update the comment at lines 11-17 to insert Traversable(27) and shift Wildcard(27→28), Any(28→29).
   - @isa_test.go:663-668 — `TestIsAWorldCountBaseline` (want 28 → 29; update the human-readable trait list in the panic message).
   - @world.go:78-79 — update the inline comments for `wildcardID` and `anyID` to reflect new indices 28 and 29, and add a new field `traversableID` at index 27. Also update the New() doc comment table at @world.go:111-141.
   - @docs/ComponentTraits.md:824-825 (Wildcard/Any indices in the sentinel table) — currently lists Wildcard at 21 and Any at 22; these were already stale before this phase. Either fix them as part of this phase to 28 and 29, or surface as a separate doc bug. Recommended: fix them here since we're touching adjacent rows.

### Correction to the request

The task description says "the entities currently bootstrapped as Acyclic (`IsA`, `ChildOf`)" — verified only `ChildOf` is bootstrapped Acyclic today (see @world.go:339 `applyAcyclicPolicy(w, w.childOfID)`; no `applyAcyclicPolicy(w, w.isAID)` exists). After this phase, `IsA` will become Acyclic for the first time (as a side effect of becoming Traversable). This is consistent with upstream: C bootstraps `IsA` as Traversable at [`bootstrap.c:1315`](../SanderMertens/flecs/src/bootstrap.c), and Traversable implies Acyclic via the `With` chain at [`bootstrap.c:1296`](../SanderMertens/flecs/src/bootstrap.c). Document this as a behavior change in the v0.46.0 CHANGELOG entry (write-time cycle rejection now also applies to user-built `(IsA, *)` cycles — previously these were caught only at traversal time by `walkUp`'s seen-map guard in @traversal.go:42-54).

### Decision points for the iterate agent

1. **SelfUp depth=0** — investigate whether C requires Traversable when a `SelfUp` term's effective depth is zero (i.e. self matches). Search [`src/query/validator.c`](../SanderMertens/flecs/src/query/validator.c) for the `term->trav` guard at line 639; the check appears unconditional (gated only by `if (term->trav)` non-zero). Decision: Go flecs should match — if `term.Trav != 0` we require Traversable, regardless of depth. State this in the test for case 9 and in the CHANGELOG.
2. **Pair-form `.Up(MakePair(R, target))`** — confirm whether the Traversable check applies to `R` (the relationship side) when `t.Trav` is a pair. Likely yes (C's `term->trav` field holds a single entity, not a pair). Verify by reading the `Trav` field semantics in @query.go:104. Document the decision.
3. **DependsOn** — Go flecs lacks a `DependsOn` built-in. Either add it as part of this phase (matches C's [`bootstrap.c:1045, 1316`](../SanderMertens/flecs/src/bootstrap.c) bootstrap of `DependsOn` as Traversable + Transitive + Acyclic) or document the gap and bootstrap only `IsA` + `ChildOf` as Traversable. Recommend deferring DependsOn to its own phase — it's a separate primitive with its own semantics (scheduler-ordering trait).
4. **`Transitive` → `Traversable` implication** — C also has `(EcsTransitive, EcsWith, EcsTraversable)` at [`bootstrap.c:1299`](../SanderMertens/flecs/src/bootstrap.c). Go flecs has Transitive (v0.37.0) but does not currently mark transitive relationships as traversable. Should `SetTransitive` also call `SetTraversable`? Recommend yes for parity, but flag this as a behavior change in CHANGELOG and add a test that confirms `SetTransitive(w, R)` makes `R` queryable with `.Up(R)`.

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%

### Non-goals

- Do NOT implement DontFragment or Union — those are larger structural changes (sparse storage path).
- Do NOT implement the Relationship / Target / Trait usage-constraints bundle in this phase.
- Do NOT migrate existing query-traversal callsites to require explicit Traversable registration in user code — the bootstrap of `IsA` + `ChildOf` is what keeps user-facing code source-compatible. Document this as the migration story in @docs/Relationships.md.

## Constraints

- @CONTRIBUTING.md — doc-update conventions for new trait phases (CHANGELOG at top, ROADMAP shipped row, docs/ComponentTraits.md trait section + roadmap table, docs/README.md feature-gap list flip, README.md feature-index row).
- @writeonce.go — most recent shipped trait (Phase 15.13, v0.45.0); structure to mirror: per-id flag map, scope-accepting reader, bare-tag dispatch in id_ops.go and cmd_queue.go.
- @singleton.go — Phase 15.12 precedent for `scope`-accepting readers and deferred-path enforcement.
- @acyclic.go — relationship-trait precedent (write-time enforcement); note its `IsAcyclic` predates the Phase 15.8 `scope` convention and uses `*World` — new code should use `scope`.
- @final.go — bare-tag dispatch and policy-map precedent.
- @world.go — built-in allocation, ordering at lines 250-318; bootstrap calls at 320-339; built-in entity doc table at 111-141; field declarations at 52-93.
- @query.go — `TraverseFlags` definition (lines 12-49), `Term.Up`/`SelfUp`/`Cascade` (lines 141-153), `validateAndSortTerms` (lines 822-915), `applyInheritablePromotion` (lines 171-199), `NewQuery` (line 211), `NewQueryFromTerms` (line 258).
- @cached_query.go — `NewCachedQueryFromTerms` (line 168) and `newCachedQueryInternal` (line 183); Cascade detection at lines 184-191.
- @scope.go — `scope` interface (lines 39-44); precedent for `scope`-accepting public API.
- @id_ops.go — bare-tag dispatch chain at lines 90-124; new branch for `w.traversableID` slots in here.
- @cmd_queue.go — deferred bare-tag dispatch at lines 151-166; new branch for `w.traversableID` slots in here.
- @marshal.go — built-in skip set at lines 101-130 (must add `w.Traversable()`).
- @meta_test.go — `builtinEntityCount` (line 18) baseline; doc comment at lines 11-17.
- @isa_test.go — `TestIsAWorldCountBaseline` at line 663 (want 28 → 29; update trait-list message).
- @marshal_test.go — baseline trait list at line 51 (add `w.Traversable()`).
- @docs/ComponentTraits.md — trait page section at lines 729-733; roadmap table at line 812; stale sentinel-index table at lines 824-825 (fix Wildcard 21→28 and Any 22→29 in same PR).
- @docs/Relationships.md — Traversal-in-queries section at lines 484-506; Traversable shipped-flip at lines 772-777.
- @docs/Queries.md — Traversal prose at line 410; Custom-traversal-relationships recipe at lines 511-532.
- @docs/README.md — feature-gap list entry for Traversable at line 122.
- @README.md — feature index at lines 210-238 (line 226 is the Traversal row; consider adding a new row for the Traversable trait near the v0.45.0 WriteOnce row at line 233).
- @CHANGELOG.md — v0.45.0 entry template at lines 3-21 (new v0.46.0 entry follows same shape).
- @ROADMAP.md — heading bump at line 3 (`through v0.45.0` → `through v0.46.0`); new bullet after the v0.45.0 WriteOnce bullet at line 50.
- @traversal.go — `walkUp` at lines 26-70 is the runtime traversal primitive; Traversable does not change `walkUp` behavior, only gates which relationships are allowed to reach it via query terms.
- Upstream C reference: [`SanderMertens/flecs/include/flecs.h:1832-1834`](../SanderMertens/flecs/include/flecs.h) for `EcsTraversable` declaration; [`SanderMertens/flecs/src/bootstrap.c:1012, 1063, 1295-1296, 1299, 1315-1316`](../SanderMertens/flecs/src/bootstrap.c) for bootstrap + `With Acyclic` implication; [`SanderMertens/flecs/src/query/validator.c:639-647`](../SanderMertens/flecs/src/query/validator.c) for the query-time enforcement site.
