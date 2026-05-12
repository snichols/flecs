## Goal

ROADMAP's "Query extensions" section has one remaining item: *"Query-time IsA inheritance (match entities whose prefab has a component)."* Phase 13.0 (v0.17.0) shipped explicit `Term.Up(w.IsA())` / `Term.SelfUp(w.IsA())` traversal modifiers. This phase closes the loop: components can be marked **inheritable**, after which any query term involving an inheritable component automatically gets `Self|Up` traversal with `IsA` — matching C flecs at `src/query/validator.c:760-826`.

### What C does (ground truth)

C flecs ships three trait entities: `EcsOnInstantiate`, `EcsInherit`, `EcsOverride`, `EcsDontInherit`, plus a `EcsInheritable` marker (see `include/flecs.h:1781-1807`). A component opts in via the pair `(OnInstantiate, Inherit)`:

```c
ecs_add_id(world, comp, ecs_pair(EcsOnInstantiate, EcsInherit));
```

The pair is interned by the per-component `ecs_component_record_t`. An observer registered in `src/bootstrap.c:1108-1116` (`flecs_register_on_instantiate`) watches `(OnInstantiate, *)` adds/removes and folds the trait into a `cr->flags` bit, e.g. `EcsIdOnInstantiateInherit` (`bootstrap.c:198-224`). At validate time the rule is:

```c
// src/query/validator.c:766-770
if (cr_flags & EcsIdOnInstantiateInherit) {
    term->src.id |= EcsSelf|EcsUp;
    if (!term->trav) {
        term->trav = EcsIsA;
    }
}
```

If the user has already set `EcsTraverseFlags` (Self/Up/SelfUp) on `term->src.id`, the auto-rule is skipped (`validator.c:765`). Symmetrically, requesting `term->trav == EcsIsA` on a *non*-inheritable component is rejected (`validator.c:829-838`). At runtime, `it->sources[fi]` is set when the term was resolved from an ancestor — our Go port already mirrors this exactly via `it.upSources` (see `IsFieldSelf` / `FieldShared[T]` in @query.go).

Built-in component records that opt **out** include `ChildOf`, `Component`, `Prefab`, `OnDelete`, `Exclusive` (`bootstrap.c:1308-1312`); built-ins that opt **in** include `IsA`, `DependsOn` (`bootstrap.c:1328-1329`). `ChildOf` itself is hard-coded as `EcsIdOnInstantiateDontInherit` in `src/storage/component_index.c:589-591`.

### Why this exists

Right now `Each1[Position]` only matches entities whose own archetype contains `Position`. If `prefab` has `Position` and `instance` has `IsA(prefab)` but no local `Position`, the instance is invisible to `Each1[Position]`. C makes the instance visible automatically, *if* `Position` was opted into inheritance. We want the same behavior.

This is a behavior change for the default `Each1`/`Each2`/`Each3`/`Each4` paths *only for components the user has explicitly marked inheritable*. Components default to non-inheritable, so existing user code is unchanged unless the user opts in. Pre-1.0, no production users; the opt-in gate makes this non-breaking in practice.

## Deliverables

### 1. Built-in trait entities

Add new built-in entity helpers on `*World`, allocated alongside `IsA()` and `ChildOf()` in @world.go (around lines 98-109):

- `World.OnInstantiate() ID` — the relationship entity, mirroring C's `EcsOnInstantiate`.
- `World.Inherit() ID` — target tag, mirroring `EcsInherit`.
- `World.Override() ID`, `World.DontInherit() ID` — declared for API symmetry; not consulted by this phase (see Non-goals).

These IDs are allocated as bare entities (parallel to how `isAID` is allocated at @world.go:104-109). Their existence makes the C-style pair `MakePair(w.OnInstantiate(), w.Inherit())` expressible for future phases that may want to honor the pair representation directly.

### 2. Component-registry inheritable flag

Add a boolean `Inheritable` field to `component.TypeInfo` in @internal/component (see `TypeInfo` at `internal/component/typeinfo.go:43-61`). Default `false`. This is the simpler-than-C representation: a flat bool on the `*TypeInfo` rather than a flag word on a per-component `ecs_component_record_t`. The Go port has no equivalent of `cr->flags` today; introducing one solely for this phase would be over-build. The C-style pair representation can be added later as an alternative opt-in path (see "C-isms not ported" below).

### 3. Public opt-in API

Add a method on `*World`:

```go
// SetInheritable marks the component associated with cid as inheritable. Any
// subsequent query term involving cid will default to Self|Up traversal via
// IsA, so the term matches both entities that own cid locally and entities
// that inherit cid from a prefab via IsA. Has no effect on terms that
// explicitly set traversal via Self()/Up()/SelfUp()/Cascade().
//
// Must be called BEFORE any query referencing cid is constructed. C flecs
// enforces this with flecs_component_is_trait_locked; the Go port should
// panic with a similar message ("cannot set Inheritable on component already
// referenced by a query"). See bootstrap.c:204-210.
func (w *World) SetInheritable(cid ID)
```

Plus a generic convenience:

```go
// SetInheritable[T] marks the component type T as inheritable. T must have
// been registered first; panics otherwise.
func SetInheritable[T any](w *World)
```

The trait-locked check can be omitted in the initial implementation if it materially complicates the change; document the requirement and add the check in a follow-up. (Tracking whether a component has been "queried for" requires extra bookkeeping the Go port doesn't currently have.)

### 4. Validator extension

In @query.go, every place that constructs a `Term` with the default `TraverseSelf` must, after construction, check the inheritable flag on the term's component and auto-promote to `Self|Up` + `Trav = w.IsA()`. The Go validator path is split across multiple call sites; the rule must apply to all of them:

- `NewQuery(w, ids...)` at @query.go:166-184 — `terms[i] = Term{ID: id, Kind: TermAnd}` is built from raw IDs.
- `NewQueryFromTerms(w, terms...)` at @query.go:212-226 → `validateAndSortTerms` at @query.go:707+.
- `NewCachedQuery` and `NewCachedQueryFromTerms` at @cached_query.go:128, 167.
- `Each1`/`Each2`/`Each3`/`Each4` in @scope.go:477-542 — these go through `NewQuery`, so fixing `NewQuery` fixes them automatically.

Mirroring `validator.c:764-770`, the rule is:

```
if term.Kind in {TermAnd, TermOptional} and term.Traverse == TraverseSelf and term.Trav == 0:
    if isInheritable(term.ID):
        term.Traverse = TraverseSelfUp
        term.Trav = w.IsA()
```

Explicit `.Self()` on a user-supplied term keeps the term Self-only (the inheritable check sees `Traverse == TraverseSelf` *but* the user invoked `.Self()` deliberately — we need a way to distinguish "default zero" from "explicit Self"). Two options:

- **(a) Add `TraverseExplicitSelf` enum value.** `.Self()` returns `Traverse = TraverseExplicitSelf`; matcher treats it identically to `TraverseSelf`; validator skips the auto-promote when it sees the explicit value.
- **(b) Document that `.Self()` on a default-zero term is a no-op and that to *force* Self on an inheritable component, the user passes a `Term{Traverse: ...}` literal with a sentinel.**

Recommend (a). It's a one-line enum addition and keeps the user-facing surface clean. The matcher in @query.go (look for `case TraverseSelf:` at ~line 402) can fall through to the same code path. Mirror C: `validator.c:765` checks `!(term->src.id & EcsTraverseFlags)` — the equivalent in Go is "user did not call `.Self()/.Up()/.SelfUp()/.Cascade()`."

`TermNot` terms ignore the auto-promote (Not means "table does not contain this id"; Up traversal doesn't change archetype matching for Not).

### 5. Field-access semantics

C flecs gives the user a single typed column pointer per field. If the term was self-matched, the pointer aliases the matched table's column (per-row). If it was up-matched, the pointer aliases the ancestor's single-element slot (shared across all rows in the iterated table). The user calls `ecs_field_is_self(it, fi)` to disambiguate (`include/flecs.h:5784` and nearby).

The Go port already implements this exactly via @query.go: `IsFieldSelf(it, id)` (line 643) and `FieldShared[T](it, id)` (line 667). What changes for `Each1`/`Each2`/`Each3`/`Each4` is that the simple "pointer per row" model breaks when the term auto-promotes to `SelfUp` and the matched table is in the Up branch.

Options:

- **(a) Pass the prefab's component pointer to the callback for every row when Up-matched.** All entities in the matched table see the *same* pointer. Mutating through that pointer silently mutates every inheritor. Document the foot-gun in `Each1`'s godoc. This matches C flecs exactly.
- **(b) Auto-snapshot the inherited value into a per-call local on the stack and pass `&local`.** No silent shared-mutation, but mutations through the pointer are lost. Performance cliff.
- **(c) Refuse to auto-promote for the `Each*` paths and only auto-promote for `NewQueryFromTerms` / `NewCachedQueryFromTerms` (where the user already opts in to the Self/Up distinction via `FieldShared`).**

**Recommend (a)** with a doc note in @scope.go on `Each1`/`Each2`/`Each3`/`Each4`. The C semantics are well-understood and the alternative (option c) is incoherent with the goal of this phase. We can revisit if usage in practice shows the foot-gun is too sharp.

Implementation: the existing iter machinery already produces an `upSources` map and `FieldShared` already does the prefab-row pointer arithmetic. In `Each1[A]`'s loop, when `it.upSources[ids[0]] != 0`, look up the prefab record and reuse its column pointer for every `i` in the row range, instead of indexing `colA[i]`.

### 6. Cached-query rematch

Cached queries pick up new inheritor tables via the existing `notifyTableCreated` hook (@world.go:676). Auto-promoting a term to `SelfUp` should not require any change to the cache notification path — the cached matcher already accepts `Up` terms after Phase 13.0. Validate this with a regression test rather than special-case code.

The Phase 13.0 limitation that the cache is *not* invalidated when a prefab gains/loses the inherited component (no down-cache observers — `cached_query.go` does not subscribe to ancestor mutations) is documented and stays in this phase. Same trade-off, same workaround (rebuild the query).

### 7. Tests

New file `inheritance_test.go` or expand `query_terms_test.go`:

- `TestInheritable_Each1MatchesInheritor` — `SetInheritable[Position]`; prefab has `Position`; child has `IsA(prefab)` and no local `Position`. `Each1[Position]` yields child.
- `TestInheritable_LocalOverridesInherited` — same setup but child also has local `Position`. `Each1[Position]` yields the local value (Self path wins per `IsFieldSelf` semantics).
- `TestInheritable_UnmarkedComponent_StaysExclusive` — `Velocity` not marked. `Each1[Velocity]` does NOT include inheritor.
- `TestInheritable_ExplicitSelfOverridesAuto` — `SetInheritable[Position]`, but `With(positionID).Self()` keeps Self-only matching (requires the `TraverseExplicitSelf` sentinel from §4).
- `TestInheritable_Each2_OneInheritedOneLocal` — Position inheritable, Velocity not. Prefab has Position; instance has IsA + Velocity. `Each2[Position, Velocity]` yields the instance; Position pointer comes from prefab, Velocity pointer from instance.
- `TestInheritable_NewQueryFromTerms_FieldShared` — for explicit `NewQueryFromTerms` callers, `IsFieldSelf` still correctly distinguishes local vs inherited and `FieldShared[T]` returns the prefab value.
- `TestInheritable_CachedQueryRematch` — new inheritor table is created *after* the cached query is built; the cached query picks it up on the next `Iter()`.
- `TestInheritable_TermNot_NotPromoted` — `With(posID).Then(Without(velID))` with Velocity inheritable; Not term must not auto-promote.
- `TestInheritable_SetAfterQueryPanics` — call `SetInheritable[Position]` *after* `NewQuery` has been built referencing Position; must panic with a clear message. *(Optional for v1 if the trait-locked check is deferred.)*
- `TestInheritable_TraversalAttribute_RejectsNonInheritable` — explicit `With(velID).Up(w.IsA())` for a non-inheritable component. Mirror C's `validator.c:829-838` reject; or accept and document as Go-permissive (decide during implementation; document either way).

### 8. Benchmarks

In `bench_test.go`:

- `BenchmarkInheritableEach1_NoInheritors` — `Position` inheritable, no IsA pairs in the world; should be within noise of `BenchmarkEach1`.
- `BenchmarkInheritableEach1_WithInheritors` — N inheritor entities; cost should approximate `BenchmarkQueryUpTraversal` from Phase 13.0.
- Confirm `BenchmarkEach1` and `BenchmarkCachedQueryEach2_10k` are unchanged for the **non-inheritable** path (the default), within noise.

### 9. Docs

- @doc.go — add a short "Inheritable components" section that introduces `SetInheritable[T]` and the auto-`Self|Up` rule, with a 6-10 line code snippet (prefab + instance + `Each1`).
- @README.md — add a corresponding bullet in the IsA-inheritance subsection.
- @ROADMAP.md — move "Query-time IsA inheritance (match entities whose prefab has a component)" from `## Future Work` → `## Shipped`. Reword the existing IsA bullet to clarify the difference between value-inheritance (Get/Has, Phase 4.3) and match-inheritance (this phase).
- `CHANGELOG.md` — phase entry under the unreleased heading.

## Constraints

- @ROADMAP.md — single remaining item under "Query extensions"; this phase closes it.
- @query.go — Term builder, validator, FieldShared, IsFieldSelf, matcher. The auto-promotion logic lives here.
- @cached_query.go — `NewCachedQuery` and `NewCachedQueryFromTerms` need the same auto-promotion as the uncached path; `notifyTableCreated` already handles new inheritor tables for explicit `SelfUp` terms.
- @scope.go — `Each1`/`Each2`/`Each3`/`Each4` go through `NewQuery`; once `NewQuery` auto-promotes, these follow. Documentation foot-gun note for shared inherited pointers lives on these functions' godoc.
- @isa.go — existing IsA helpers (`getViaIsA`, `hasViaIsA`) are unchanged; this phase changes only query *matching*, not Get/Has value lookup.
- @world.go — built-in `World.IsA()` lives at lines 105-109; add `OnInstantiate`, `Inherit`, `Override`, `DontInherit` alongside.
- @internal/component — add `Inheritable bool` to `TypeInfo` at `internal/component/typeinfo.go:43-61`; expose a setter or write directly via a registry method.
- @doc.go — package docs need the new section.
- @README.md — examples need a snippet.
- @CHANGELOG.md — phase entry.

C reference paths (cited inline above; not `@`-references because the C tree is outside this repo):

- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h:1781-1807` — trait entity declarations.
- `/work/agents/claude/projects/SanderMertens/flecs/src/query/validator.c:760-838` — the auto-`Self|Up` rule and reverse rejection.
- `/work/agents/claude/projects/SanderMertens/flecs/src/storage/component_index.c:589-591, 976-979` — built-in `ChildOf` opt-out and the `EcsIdOnInstantiateInherit` flag plumbing.
- `/work/agents/claude/projects/SanderMertens/flecs/src/bootstrap.c:198-224, 311-317, 1108-1116, 1262-1329` — observer that folds the `(OnInstantiate, Inherit)` pair into a `cr->flags` bit and the built-in opt-in/opt-out wiring.

## Non-goals

- **No change to non-IsA relationships.** Auto-Up applies only with `Trav = w.IsA()`. ChildOf, custom relationships, etc. require explicit `.Up(rel)`.
- **No automatic `Override` (copy-on-write) when a Set lands on an inheritor.** Set already does this in v0.x (Phase 4.3). This phase changes query *matching* only.
- **No `EcsDontInherit` runtime behavior.** Default is non-inheritable; opt-in is the only path. `DontInherit` only makes sense when the default is "inherit"; in the Go port it would be a no-op. We expose `World.DontInherit()` as an entity ID for API symmetry but the validator does not consult it.
- **No `EcsInheritable` marker entity behavior.** C uses `EcsInheritable` as a *separate* marker (`include/flecs.h:1788`) that enforces query-time inheritance evaluation even without an explicit `(OnInstantiate, Inherit)`. We collapse the two concepts into one boolean. Document this in the API godoc.
- **No down-cache observers.** Same limitation as Phase 13.0: when a prefab gains/loses an inheritable component *after* a cached query is built, the cache is not auto-invalidated. Workaround: rebuild the query.
- **No `Reader`/`Writer` API changes.** All work is internal to query construction and term validation.
- **No retroactive promotion for non-inheritable components.** If the user calls `SetInheritable[Position]` after a `NewQuery(w, posID)` is built, the existing query keeps its Self-only behavior. New queries promote. Optionally panic at `SetInheritable` time to enforce the ordering invariant (C flecs does this via `flecs_component_is_trait_locked` at `bootstrap.c:204-210`).

## C-isms not ported (rationale)

- **`(OnInstantiate, Inherit)` pair as the on-component representation.** C stores the trait as a pair on the component entity and runs an observer that folds it into a `cr->flags` bit. The Go port stores a bool on `TypeInfo` directly. Reason: the Go port has no `ecs_component_record_t` analog with a flag word, so introducing the pair-then-observer-then-flag-bit pipeline for one trait is over-build. The user-facing API (`SetInheritable`) is simpler than `AddPair(w.OnInstantiate(), w.Inherit())`. The built-in `World.OnInstantiate()`/`World.Inherit()` IDs are still exposed so the pair representation could be added as an additional opt-in path in a later phase without breaking the simple API.
- **`EcsInheritable` as a separate query-time-enforcement marker.** Collapsed into the single `SetInheritable` opt-in; documented above.
- **`EcsOverride` and `EcsDontInherit` runtime behavior.** Override is already handled by Set's copy-on-write semantics (Phase 4.3). DontInherit has no meaning when the default is non-inheritable.

## Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run ./...` clean.
- `go test ./... -race -count=3 -timeout=180s` passes.
- Coverage on the main `flecs` package stays at or above 95.0% (current baseline).
- New benchmarks added; existing benchmarks within noise (~5%).
- @ROADMAP.md, @README.md, @doc.go, `CHANGELOG.md` updated.
- All new public symbols documented to the same godoc standard as @isa.go and @query.go.
