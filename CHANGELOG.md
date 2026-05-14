# Changelog

## v0.77.0 — 2026-05-14 — Phase 16.22: AndFrom / OrFrom / NotFrom query operators

Ports the three type-list expansion operators from upstream C flecs (`include/flecs.h:723-725`, `src/query/compiler/compiler_term.c:1225-1251`, `src/query/engine/eval.c:427-616`) as new query term kinds. Closes `docs/README.md` gap entry for `AndFrom / OrFrom / NotFrom`.

### Added

- **`AndFrom(source ID) Term`** — `TermAndFrom TermKind = 8`. Expands `source`'s component list into N `TermAnd` requirements at construction (snapshot). Entities must have **all** of source's inheritable components. Empty source type → vacuous truth, no requirements added.
- **`OrFrom(source ID) Term`** — `TermOrFrom TermKind = 9`. Expands `source`'s component list into one OR-group at construction. Entities must have **at least one** of source's inheritable components. Empty source type → zero results (empty disjunction = false).
- **`NotFrom(source ID) Term`** — `TermNotFrom TermKind = 10`. Expands `source`'s component list into N `TermNot` requirements at construction. Entities must have **none** of source's inheritable components. Empty source type → vacuous truth, no exclusions added.
- **`alwaysFalse` query flag** — `OrFrom` with an empty source sets this flag; `Iter()` returns an exhausted iterator immediately without scanning any tables.
- **DontInherit bootstrap for `Disabled`** — `world.go` now bootstraps `DontInherit` on the `Disabled` tag (mirrors upstream C `bootstrap.c`), so `Disabled` is excluded from *From expansion just as `Prefab` is.

### Behaviour

- **Snapshot at construction, not live.** The source's component list is read once via `Reader.EntityComponents` in `validateAndSortTerms` and cached as expanded inner terms. Subsequent mutations to the source entity do not affect the query — reconstruct to pick up changes. This deliberately diverges from upstream C, which re-reads `r->table->type` on every iteration (`eval.c:462`).
- **DontInherit filter.** Components with `DontInherit` policy (e.g. `Prefab`, `Disabled`) are excluded from expansion, matching upstream `EcsIdOnInstantiateDontInherit` filtering (`eval.c:435`, `compiler_term.c:1241`).
- **Pair IDs are included.** Pair components (e.g. `(ChildOf, parent)`) in the source's type are expanded verbatim into pair terms.
- **Single-component `OrFrom` degenerates to `TermAnd`** to avoid a one-element OR-group.
- **Pure-*From queries bypass the hasAnd check.** A query whose entire term list came from *From terms (e.g. standalone `NotFrom(template)` or `OrFrom(template)`) is valid even without a `TermAnd` seed — iteration falls back to all tables, and `matchesTable` applies the Not/Or filters.
- **Source must be alive at construction.** Panics with `"<caller>: AndFrom/OrFrom/NotFrom source entity <e> is dead or non-existent"`.
- **Empty source type diverges from upstream C.** Upstream `compiler_term.c:1231` skips (`ctx->skipped++`) empty-type operators; Go flecs follows set-theoretic semantics: `OrFrom(empty)` = empty disjunction = false.

### Upstream C references

- `include/flecs.h:723-725` — `EcsAndFrom`, `EcsOrFrom`, `EcsNotFrom` in `ecs_oper_kind_t`.
- `src/query/compiler/compiler_term.c:1225-1251` — compile-time type lookup and `EcsIdOnInstantiateDontInherit` filtering.
- `src/query/compiler/compiler_term.c:1090-1096` — lowering to `EcsQueryAndFrom` / `EcsQueryOrFrom` / `EcsQueryNotFrom` ops.
- `src/query/engine/eval.c:427-440` — `flecs_query_next_inheritable_id`: per-ID DontInherit skip.
- `src/query/engine/eval.c:443-616` — `flecs_query_x_from` shared engine and dispatchers.
- `test/query/src/Operators.c:9670-9801` — upstream behavioural tests confirming prefab-source exclusion.

### Deliberate non-goals

- **Live expansion** — upstream C re-reads `source.type` at every iteration; Go flecs snapshots at construction. Callers requiring live behaviour must re-construct the query.
- **`AndFromQuery` / `OrFromQuery`** — expand from another query's matched components; too meta, out of scope.
- **`$Var` query variables** — separate future phase.
- **Empty-OrFrom as a noop** — upstream treats empty-type *From as a noop; Go flecs follows set-theoretic semantics where empty `OrFrom` = false.

---

## v0.76.0 — 2026-05-14 — Phase 16.21: Query equality operators

Ports the upstream C flecs equality predicates (`include/flecs.h:1979-1986`, `src/query/engine/eval_pred.c:8-303`, `src/query/compiler/compiler_term.c:640-685`) as three new per-entity filter terms. These compose orthogonally with all existing term kinds.

### Added

- **`IsEntity(e ID) Term`** — `TermEq TermKind = 5`. Matches only when the iterated entity equals `e`. Mirrors upstream `EcsPredEq` / `EcsQueryPredEq`. Panics at construction if `e` is zero.
- **`NotEntity(e ID) Term`** — `TermNotEq TermKind = 6`. Matches every entity except `e`. Mirrors upstream `EcsPredEq+EcsNot` / `EcsQueryPredNeq`. Panics at construction if `e` is zero.
- **`NameMatches(pattern string) Term`** — `TermNameMatch TermKind = 7`. Matches entities whose `Name.Value` contains `pattern` (case-insensitive substring; no regex, no glob). Empty pattern matches every named entity. Unnamed entities never match. Mirrors upstream `EcsPredMatch` / `EcsQueryPredEqMatch` and `flecs_query_match_substr_i` (`eval_pred.c:8-41`).
- **`ScopeBuilder.IsEntity`, `.NotEntity`, `.NameMatches`** — equality terms compose inside `WithoutScope` sub-expressions.
- **`Term.Pattern string`** — new field on `Term`; populated only for `TermNameMatch`; empty string is a valid pattern (matches every named entity).
- **`CachedQuery` `OnSet[Name]` observer** — cached queries with a `TermNameMatch` term subscribe to `OnSet[Name]` at construction and unsubscribe on `Close()`; `Changed()` returns true after any name write, enabling pattern-sensitive invalidation.

### Behaviour

- Equality terms are **filters, not seeds** — they evaluate per entity after archetype matching. A query with only equality terms (no `With`) panics with the existing "at least one TermAnd required" message.
- `TermEq` / `TermNotEq` / `TermNameMatch` **cannot** carry `.Up()`, `.SelfUp()`, `.Cascade()`, or `.Source()` — panic at validator time.
- Sort order: equality terms sort after `TermOr` groups and before `TermOptional`, mirroring the per-entity evaluation ordering.
- `substrMatchCaseInsensitive` helper mirrors `flecs_query_match_substr_i` exactly: empty pattern → true; otherwise case-fold both sides with `strings.ToLower` and use `strings.Contains`.

### Upstream C references

- `include/flecs.h:1979-1986` — `EcsPredEq`, `EcsPredMatch`, `EcsPredLookup` marker constants.
- `src/query/engine/eval_pred.c:8-41` — `flecs_query_match_substr_i`: case-insensitive substring scan.
- `src/query/engine/eval_pred.c:103-209` — `flecs_query_pred_eq` / `flecs_query_pred_neq`: entity range narrowing.
- `src/query/engine/eval_pred.c:212-303` — `flecs_query_pred_match`: per-entity name scan.
- `src/query/compiler/compiler_term.c:640-685` — compile-time lowering to `EcsQueryPredEq` / `EcsQueryPredNeq` / `EcsQueryPredEqMatch`.
- `src/query/validator.c:442-474` — validator rules for predicate terms.

### Deliberate non-goals

- **`EcsPredLookup`** (`$this == "name"`) — overlaps with `World.Lookup(path)` + `IsEntity`; not ported.
- **Regex / glob matching** — `NameMatches` is substring-only, faithful to upstream.
- **Component-value comparison** (`Position.X > 100`) — separate concern, out of scope.
- **`$Var` query variables** — separate future phase.

---

## v0.75.0 — 2026-05-14 — Phase 16.20: Query scopes (WithoutScope)

Ports the upstream C flecs `EcsScopeOpen` / `EcsScopeClose` scope mechanism (`include/flecs.h:1989–1992`, `src/query/validator.c:1427–1452`, `src/query/compiler/compiler_term.c:785–803`) as a Go-idiomatic closure-based API. A query scope negates a sub-expression of arbitrary terms as a single unit, enabling expressions such as `Position AND NOT (Velocity OR Speed)` that cannot be written as a flat list of `Without` terms.

### Added

- **`WithoutScope(buildFn func(*ScopeBuilder)) Term`** — top-level term constructor. Calls `buildFn` with a fresh `*ScopeBuilder`, collects the accumulated inner terms into a `TermScope` term, and returns it. Panics if `buildFn` produces zero inner terms (mirrors upstream `validator.c:1441` empty-scope rejection). The returned term drops into any `NewQueryFromTerms` / `NewCachedQueryFromTerms` positional term list unchanged.
- **`type ScopeBuilder struct`** — closure receiver exposing:
  - `b.With(id) *ScopeBuilder` — require `id` inside the scope.
  - `b.Without(id) *ScopeBuilder` — require absence of `id` inside the scope.
  - `b.Or(id) *ScopeBuilder` — OR with the preceding term (C flecs OR-group convention: `With` is first member, adjacent `Or` calls extend the group).
  - `b.Maybe(id) *ScopeBuilder` — optional `id` inside the scope.
  - `b.Source(src ID) *ScopeBuilder` — fix the preceding term's source entity (parallel to `(Term).Source`).
  - `b.WithoutScope(fn func(*ScopeBuilder)) *ScopeBuilder` — nested negated sub-scope; arbitrary depth.
- **`TermScope TermKind = 4`** — new term kind. `TermScope` terms carry `Sub []Term` (the inner term list); `ID = 0`; negation is implicit. Exposed via `String() → "Scope"`.
- **`Sub []Term` field on `Term`** — populated only for `TermScope` terms.

### Behaviour

- **Table-level fast path**: when all inner terms are plain `TermAnd` (no DontFragment / Union / Sparse / fixed source, not followed by an Or), `matchesTable` / `tryMatchTable` evaluates the scope at table granularity: if every inner component ID is present in the table's archetype signature the scope rejects the table (all entities in that table have the excluded component set). Otherwise the scope passes for the whole table and no per-entity work is needed.
- **Per-entity slow path**: complex scopes (containing Or-groups, TermNot, nested scopes, DontFragment, Union, Sparse, or fixed-source terms) are evaluated entity-by-entity via `evalScopeSubTerms`. Queries containing at least one complex scope set `hasSparseTerms = true` to route through the `nextMixed()` path regardless of whether any actual sparse components are present.
- **OR-group semantics inside scope**: a `TermAnd` immediately followed by one or more `TermOr` terms in `Sub` forms an OR-group matching C flecs convention — the first member uses the And slot, subsequent members use Or slots. The group is satisfied when at least one member is present on the entity.
- **Nested scope semantics**: a `TermScope` inside `Sub` is evaluated recursively by `evalScopeSubTerms`; its result is flipped (NOT of inner) before contributing to the parent scope's conjunction.
- **Fixed-source terms inside scope**: `entityHasTermInScope` resolves fixed-source terms by looking up the named entity's table at evaluation time (not at iter start), consistent with per-entity evaluation semantics.
- **allSparse mode (`it.current == nil`)**: even simple scopes are evaluated per-entity via the index-lookup fallback in `entityHasTermInScope`; the table-level fast path is gated on `it.current != nil`.
- **Cached query participation**: scope-internal component IDs are included in `tryMatchTable`'s archetype match for simple scopes. `Changed()` consults scope-internal sparse-set `ChangeCount` values alongside regular term IDs, so a mutation to any scope-internal component flips `Changed()` correctly.
- **Go-flecs vs upstream cache granularity**: upstream C flecs marks scoped terms `EcsTermIsScope` and strips `EcsTermIsCacheable` / `EcsTermIsTrivial` flags because its cache operates at per-term instruction granularity. Go-flecs caches at table-list granularity, so the parent `CachedQuery` remains fully cached; scope-internal IDs are treated as additional table-match dependencies instead.

### Tests

- 18 new tests in `query_scope_test.go`:
  1. `TestWithoutScope_PositionNotVelocityOrSpeed` — Position AND NOT (Velocity OR Speed).
  2. `TestWithoutScope_DeMorganEquivalence` — de-Morgan sanity: scope ≡ flat `Without` on simple presence-OR.
  3. `TestWithoutScope_PositionNotVelocityAndSpeed` — Position AND NOT (Velocity AND Speed); verifies result differs from case 1.
  4. `TestWithoutScope_Nested` — Position AND NOT (Velocity AND NOT Frozen); nested scope.
  5. `TestWithoutScope_MultiOr` — Position AND NOT (A OR B OR C); three-member OR-group.
  6. `TestWithoutScope_EmptyPanic` — `WithoutScope(func(*ScopeBuilder) {})` panics with clear message.
  7. `TestWithoutScope_SparseComponent` — sparse component inside scope; per-entity path.
  8. `TestWithoutScope_DontFragmentComponent` — DontFragment component inside scope.
  9. `TestWithoutScope_FixedSource` — fixed-source term inside scope resolves at named entity.
  10. `TestWithoutScope_CachedQuery` — cached query with scope; `Changed()` flips after inner-scope mutation.
  11. `TestWithoutScope_AllMatch` — all entities match when none have the scoped components.
  12. `TestWithoutScope_NoneMatch` — no entities match when all have the scoped components.
  13. `TestWithoutScope_ComplexOr` — `With(A).Or(B).With(C).Or(D)` — two independent OR-groups inside one scope.
  14. `TestWithoutScope_ScopeWithWithout` — `Without` term inside scope (TermNot inside TermScope).
  15. `TestWithoutScope_CachedQueryChanged` — `Changed()` re-arms correctly across multiple mutations.
  16. `TestWithoutScope_NestedScopeDeep` — three-level nested scope.
  17. `TestWithoutScope_MatchesTableIncrementalTable` — table added after cached query construction is picked up.
  18. `TestWithoutScope_SparseOuterSimpleScope` — outer-simple scope in allSparse (`it.current == nil`) mode exercises index-lookup fallback.

### Documentation

- `docs/Queries.md` — new `### Query scopes` section (after `### Not (Without)`) with code examples, OR-group table, de-Morgan note, nested-scope example, and empty-scope behaviour.
- `docs/README.md` line 114 — query-scopes gap entry flipped to ✅ shipped in v0.75.0.
- `README.md` — `NewQueryFromTerms` feature-list entry updated to include `WithoutScope`.
- `ROADMAP.md` — heading bumped to `Shipped (through v0.75.0)`; Phase 16.20 bullet added.

## v0.74.0 — 2026-05-14 — Phase 16.19: Entity scoping (push/pop)

Ports upstream C flecs `ecs_set_scope` / `ecs_get_scope` (`src/entity_name.c:785-808`) as a Go-idiomatic closure-based API. When a scope is active, `NewEntity` and `RangeNew` automatically add `(ChildOf, scope)` to each new entity without an explicit `AddID` call.

### Added

- **`WithinScope(fw *Writer, parent ID, fn func(*Writer))`** — primary API. Pushes `parent` onto the Writer's scope stack, calls `fn` with the same Writer, pops on return (defer-based; survives panics in `fn`). Scopes nest: the inner-most scope wins.
- **`PushScope(fw *Writer, parent ID) ID`** — pushes `parent`, returns the previous top (zero if stack was empty). For callers who need to cross function boundaries where a closure is awkward. The returned value must be passed to `PopScope`.
- **`PopScope(fw *Writer, prev ID)`** — pops one frame. Panics with a clear message if `prev` does not match the value `PushScope` returned (programming error).
- **`GetScope(s scope) ID`** — returns the current scope (topmost stack entry) or 0 if none. Returns 0 on a `*Reader` (read blocks have no entity-scope semantics).
- **`(*Writer).NewEntity`** — now checks `scopeStack` after `newEntityInternal`; if non-empty, routes through `AddID` so all ChildOf trait machinery (Exclusive swap, OrderedChildren insertion, cycle detect, cleanup-policy wiring) runs unchanged.
- **`RangeNew`** — applies the active scope (fresh allocation, range-constrained); mirrors `NewEntity` hook. Scope add is routed through `AddID` for the same trait guarantees.
- **`MakeAlive`** — explicitly does NOT apply the scope (explicit ID claim bypasses scope, mirroring the Phase 16.16 "MakeAlive bypasses range" precedent).

### Design

- **Per-Writer, not per-stage**: scope lives on `*Writer` (the user-facing capability), not on the internal `*stage`. Worker stages in parallel-dispatch never create scope-bound hierarchies; moving to `*stage` can be done in a follow-up if a use case arises.
- **Stack reset on top-level Write entry**: `w.Write(fn)` resets `scopeStack` to empty before calling `fn` when `deferDepth` transitions from 0→1. Nested `w.Write` calls (same goroutine, `deferDepth > 1`) preserve the stack, matching the existing deferred-command semantics.
- **No internal stack in upstream C**: upstream uses a save+restore idiom (`prev := ecs_set_scope(w, X); ...; ecs_set_scope(w, prev)`). Go-flecs maintains an explicit `[]ID` stack on the Writer so `WithinScope` can use a clean defer-based pop without burdening callers.

### Closed gap entries

- `docs/README.md` line 87: "Entity scoping (`ecs_set_scope` / push-pop) — not ported" → shipped.
- `docs/HierarchiesManual.md` line 228: "Not yet ported" callout updated to link to new § Entity scoping.
- `docs/HierarchiesManual.md` line 334: Entity scoping removed from "Not yet ported" list.

## v0.73.0 — 2026-05-14 — Phase 16.18: Fixed per-term source

Ports the upstream C flecs fixed-source query mechanism (`compiler.c:833-882`,
`eval.c:940-962`). A query term can now bind its component to a specific named
entity rather than the iterated entity (`$this`).

### Added

- **`WithSourceTerm(componentID, sourceEntity ID) Term`** — top-level builder that returns a `TermAnd` with a fixed source entity. Panics if either ID is zero.
- **`(Term).Source(e ID) Term`** — chained builder; panics if `e` is zero or if the term already has traversal flags (`.Up()` / `.SelfUp()` / `.Cascade()`).
- **Fixed-source iteration machinery** in `query.go` and `cached_query.go`:
  - Fixed-source terms do **not** contribute to the `$this` archetype-filter set.
  - Component pointer resolved once at `Iter()` start (`buildFixedSourcePtrs`); `Field[T]` returns a 1-element slice backed by the snapshot pointer.
  - Absent required source → entire query yields zero results (matches upstream `eval.c:114-117`).
  - `CachedQuery.Iter()` re-reads the source pointer on each call so updates between iterations are visible.
- **`(Term).TermOptional` + fixed source** (`Maybe(id).Source(e)`): absent component on source yields `FieldMaybe → (nil, false)` — entities still match. Deliberate divergence from upstream's uniform treatment of optional fixed-source; the Go `FieldMaybe`-friendly behaviour is the natural fit.

### Naming rationale

The new query-side helper is `WithSourceTerm` to avoid collision with the existing observer-side `WithSource(e ID) ObserverOptions` (which accepts a single entity and returns `ObserverOptions`). The two APIs serve different scopes (query term vs observer options) and intentionally keep distinct names. `WithSource` is **not** renamed; no deprecation is introduced.

### Behaviour

- **No archetype-filter contribution**: the fixed-source term is resolved once at iter start via `buildFixedSourcePtrs`; the `$this` seed and `matchesTable` checks ignore it entirely.
- **Snapshot-at-iter-start**: the data pointer is captured at `Iter()` time; mid-iteration mutations to the source entity are not reflected until the next `Iter()` call. `CachedQuery` re-reads at each `Iter()`.
- **Dead-source → dead iter**: if any `TermAnd` fixed-source component is absent on its source entity, `Iter()` returns a dead iterator immediately (zero `Next()` calls).
- **Optional divergence**: `Maybe(id).Source(e)` caches `nil` for the absent case; `FieldMaybe` returns `(nil, false)` and entities continue to match.
- **Validation**: panics at query-construction time if (a) source entity is dead, (b) `TermNot` or `TermOr` used with fixed source (out of scope for this phase), or (c) traversal flags combined with `.Source()`.
- **Sorting**: fixed-source `TermAnd` terms sort to the head of the And-block (parallels upstream's `setfixed` plan order).

### Tests

`query_fixed_source_test.go` — 19 new test functions covering basic, no-component-on-source, mixed `$this`+fixed, snapshot, sparse, DontFragment, zero-source panic, dead-entity panic, cached-query source update, OrderBy composition, multiple sources, pair form, optional divergence, TermNot panic, union-term (present + absent + no-store), cached dead iter, method zero panic, and chained builder.

---

## v0.72.0 — 2026-05-14 — Phase 16.17: Observer propagation along IsA

Ports the upstream C flecs observer-propagation mechanism (`observable.c:1083`).
When a component is mutated on a prefab entity, the same observer event now fires
once on the source entity and once per transitive inheritor via BFS over IsA edges.

### Added

- **`observer_propagation.go`** — new file containing the propagation engine:
  - `(*World).propagateEvent(componentID, eventEntity, sourceEntity ID, ptr)` — BFS over IsA inheritors; fires `dispatchObserversForPropagation` for each live, non-override inheritor.
  - `(*World).dispatchObserversForPropagation(componentID, eventEntity, inh ID, ptr)` — propagation-aware variant of `dispatchObservers`; multi-term observers skip the trigger-component term in their filter (it is auto-satisfied since the inheritor has the component via IsA).
  - `entityMatchesTermsForPropagation(w, terms, orGroups, e, inheritedID)` — like `entityMatchesTerms` but treats `inheritedID` (the propagated trigger) as automatically satisfied for `TermAnd`.
  - `(*World).propagateReplaceHook(componentID, sourceEntity ID, fn)` — propagates `OnReplace` hook calls to inheritors.
  - `(*World).inheritorsBFS(prefab ID) []ID` — cache-backed BFS result; entire cache cleared on any `(IsA, *)` structural change.
  - `buildInheritorsBFS(w, prefab)` — BFS with visited-set for cycle safety (handles `(IsA, self)` loops).
  - `(*World).invalidateInheritorCache()` — clears the entire inheritor BFS cache; O(1).
- **`world.go`** — added `inheritorCache map[ID][]ID` field to `World` struct.
- **`hooks.go`** — `fireOnAdd`, `fireOnSet`, `fireOnRemove`, `fireOnReplace` each call the corresponding propagation function after local dispatch.
- **`id_ops.go`** — `addIDImmediate` and `removeIDImmediate` call `invalidateInheritorCache` when an `(IsA, prefab)` pair is added or removed.
- **`observer_custom.go`** — `Emit` calls `propagateEvent` when `entity != 0`, so custom events also propagate along IsA.

### Behaviour

- **DontInherit gate**: if the component is marked `DontInherit`, `propagateEvent` returns immediately — no inheritors receive the event.
- **Override gate**: inheritors that own their own local copy of the component are skipped; other inheritors still receive the event.
- **Multi-term filter**: the trigger component term is treated as auto-satisfied for propagated dispatch; remaining terms are evaluated against the inheritor's own archetype.
- **BFS cache**: computed once per prefab, shared across calls within a Write scope. Invalidated entirely (set to nil) on any `(IsA, *)` structural change to guarantee correctness for multi-level chains.
- **Cycle safety**: the BFS visited set prevents infinite loops when entities form `(IsA, self)` cycles.

### Tests

- **`observer_propagation_test.go`** — 24 tests covering: single/multiple/recursive/diamond inheritors, DontInherit gate, override gate, OnAdd/OnSet/OnRemove/OnReplace propagation, pair components, multi-term per-inheritor filter, disabled observer, 1 000-inheritor performance, marshal round-trip, cache invalidation, custom event propagation, TermNot filter, OnReplace DontInherit/override blocks, fixed-source observer in propagation, OR-group table match/fail, wildcard TermAnd/TermNot in propagated dispatch, disabled fixed-source observer skipped, fixed-source multiFilter mismatch skipped. Package coverage: 95.0%.

### Documentation

- **`docs/ObserversManual.md`** — new `## Propagation along IsA` section (gates, supported events, multi-term filter, BFS cache, example); status table entry flipped to shipped (v0.72.0).
- **`docs/PrefabsManual.md`** — cross-link to `ObserversManual.md § Propagation along IsA` added.
- **`docs/README.md`** — propagation gap entry flipped to shipped (v0.72.0).
- **`README.md`** — observer propagation row added to the feature table.
- **`ROADMAP.md`** — heading bumped to "through v0.72.0"; propagation entry marked shipped.

## v0.71.0 — 2026-05-14 — Phase 16.16: Entity ID ranges

Ports the entity ID range API from upstream C flecs. A `Writer` can now be
constrained to issue entity IDs in a specific `[min, max)` range, enabling
per-owner ID partitioning for networked games and tooling. Closes the entity
ID ranges gap entry in `docs/README.md` line 99.

### Added

- **`RangeSet(fw, min, max)`** — constrain `fw`'s allocator to issue IDs in `[min, max)`. Subsequent `NewEntity` calls return IDs within the range. Panics if `min < 1`, `max <= min`, or in a deferred scope.
- **`RangeClear(fw)`** — remove the active constraint. `NewEntity` resumes from the current `maxID`; no rewind. Recycled IDs skipped during the previous range become eligible again.
- **`RangeGet(scope)`** — inspect the active range; returns `(min, max ID, set bool)`. Works from both `*Reader` and `*Writer`.
- **`RangeNew(fw, min, max)`** — one-shot allocation in `[min, max)` without modifying the world's active range constraint. Jumps `maxID` to `min` if the current counter is below the range; panics if the range is exhausted.
- **`(*Writer).RangeSet` / `(*Writer).RangeClear` / `(*Writer).RangeNew`** — thin shims delegating to the package-level functions; follow the Phase 16.1 `MakeAlive`/`SetVersion` pattern.
- **`entityindex.SetRange` / `ClearRange` / `GetRange` / `AllocInRange`** — allocator-level plumbing in `internal/storage/entityindex`.
- **`entity_range_min` / `entity_range_max` / `entity_range_set`** fields in the JSON world snapshot — range state survives marshal/unmarshal round-trips.

### Changed

- **`entityindex.Alloc`** extended: when `rangeSet` is true, scans the recycle queue for the first in-range entry (O(k) where k = out-of-range entries before the first in-range entry); falls through to fresh allocation in `[rangeMin, rangeMax)` if none found; panics with a clear message when the range is exhausted.
- **`jsonWorld`** extended with `entity_range_min`, `entity_range_max`, `entity_range_set` fields. `UnmarshalJSON` restores the range constraint after entity allocation so restoration is unconstrained.

### Implementation notes

1. **Simplifications vs upstream**: no persistent range objects (`ecs_entity_range_new` step removed), no multi-range registry, no per-range recycle pools. A single `(rangeMin, rangeMax, rangeSet bool)` triple on `*entityindex.Index`. See `docs/EntitiesComponents.md § Entity Ranges`.
2. **Out-of-range recycled IDs**: skipped (not removed) when scanning the recycle queue. Preserved for reuse after `RangeClear` or range change. Worst-case O(k) alloc where k = out-of-range queue head entries; amortised O(1) for the typical single-static-range-set-at-boot use case.
3. **`MakeAlive` bypass**: matches upstream — `MakeAlive` does not consult the active range. If it advances `maxID` past `rangeMax`, the next `NewEntity` panics range-exhausted. Documented in `entity_range.go` and `docs/EntitiesComponents.md`.
4. **Dense-slice invariant**: range-constrained fresh allocation may consume a stale slot (from a prior Free) when `aliveCount < len(dense)`. The no-range recycled path is guarded with the same bounds check to prevent index-out-of-bounds when a stale slot was consumed by an earlier range-constrained alloc.
5. **Marshal**: range state serialised as three JSON fields; applied after entity restoration so the allocation phase during `UnmarshalJSON` is unconstrained.
6. **Built-in entity count**: unchanged at 48; user entities still start at index 48.

## v0.70.0 — 2026-05-14 — Phase 16.15: Multi-term observers

Ports multi-term observer support from upstream C flecs. An `ObserveQuery` observer
fires on a trigger component event only when the entity also passes all filter terms.
Closes the term-set observer filters gap entry in `docs/README.md` line 157.

### Added

- **`ObserveQuery(w, event, terms, fn)`** — registers a multi-term observer for a single event. `terms[0]` is the trigger (TermAnd, determines the dispatch key); `terms[1:]` are filter terms evaluated per-entity at fire time. Returns `*Observer`; call `Unsubscribe` to cancel.
- **`ObserveQueryID(w, triggerID, event, filterTerms, fn)`** — variant with an explicit trigger ID. Useful for pair-ID or raw-ID triggers that are inconvenient to express as `terms[0]`. Filter terms need not include a TermAnd for the trigger.
- **`ObserveQueryEvents(w, events, terms, fn)`** — multi-event variant; callback receives the `EventKind` that fired.
- **`ObserveQueryWithOptions(w, opts, events, terms, fn)`** — options-bearing variant; supports `WithYieldExisting()` (sweep at registration) and `WithSource(e)` (fixed-source restriction).
- **`termsMatchTable`** (internal) — table-level filter check for yield_existing sweeps; handles wildcard pairs and DontFragment terms.
- **`entityMatchesTerms`** (internal) — per-entity filter evaluation at dispatch time; handles TermAnd / TermNot / TermOr / wildcard pairs / DontFragment / Sparse / Union.

### Changed

- **`observerNode`** extended with `multiFilter *multiTermFilter` (nil for all pre-existing single-term observers; zero-overhead on the existing dispatch path).
- **`dispatchObservers`** evaluates `n.multiFilter` before invoking the callback; skips filter when `e == 0` (custom-event-with-no-entity path).
- **`commitBatch` and `migrate`** in `world.go` now update `rec.Table` / `rec.Row` **before** firing `fireOnAdd`, so multi-term filters see the fully-migrated entity state at dispatch time. (This also fixes a latent consistency issue for any future caller that reads `rec.Table` inside an OnAdd observer.)

### Implementation notes

1. **Dispatch key unchanged**: multi-term observers register with the same `(triggerID, eventEntity)` key as single-term observers. The filter is stored in the node and evaluated at dispatch time.
2. **Short-circuit evaluation**: the first failing filter term causes the callback to be skipped; remaining terms are not evaluated.
3. **yield_existing sparseMode**: when any TermAnd/TermNot term has DontFragment, Union, or Sparse flag, per-entity `entityMatchesTerms` is called even after the table-level check passes (DontFragment storage is not visible in the archetype).
4. **Record update timing**: moving `rec.Table = newTable; rec.Row = uint32(newRow)` before `fireOnAdd` is safe because `newTable` and `newRow` are fully committed before any observer fires. The old table reference is preserved locally for `fireOnRemove` / `RemoveSwap` calls that follow.
5. **Built-in entity count**: unchanged at 48; user entities still start at index 48.

## v0.69.0 — 2026-05-14 — Phase 16.14: Prefab hierarchies + slots

Ports prefab hierarchy replication and the `SlotOf` relationship from upstream C flecs.
Instantiating a prefab now replicates its entire child subtree onto the instance.
Slot pairs provide O(1) named access to copied children. Closes the prefab hierarchies
and prefab slots gap entries in `docs/README.md` lines 136–137.

### Added

- **Prefab hierarchy replication** — `AddID(e, MakePair(w.IsA(), prefab))` now traverses `prefab`'s children recursively and spawns a mirrored subtree on `e`. Two-pass algorithm: pass 1 pre-allocates all instance entity IDs (enabling sibling forward-reference rewriting); pass 2 copies components with cross-reference rewriting.
- **Same-subtree cross-reference rewriting** — pair targets that belong to the instantiated prefab subtree are rewritten from the prefab child entity to the corresponding instance child entity. External references are left unchanged.
- **`SlotOf` built-in relationship** (`w.SlotOf()`, index 47) — bootstrapped with `Exclusive`, `Relationship`, and `PairIsTag` traits. A prefab child carrying `(SlotOf, prefabParent)` causes `(prefabChild, instanceChild)` to be added to the instance root during instantiation. `OrderedChildren` propagation: when the prefab is ordered, the instance is marked ordered before children are added.
- **`GetPairTarget(scope, e, rel) (ID, bool)`** — O(1) lookup of the first pair target for relationship `rel` on entity `e`. Primary use case: `GetPairTarget(r, inst, turretPrefab)` resolves the slot to the copied turret instance child.
- **`(*World).SlotOf() ID`** — returns the built-in `SlotOf` relationship entity (index 47; user entities now start at index 48).

### Implementation notes

1. **Two-pass subtree copy**: `allocSubtreeEntities` pre-allocates all instance entity IDs before `copyPrefabChildComponents` begins, ensuring sibling forward-references (`(R, sibling)` where sibling hasn't been copied yet) can be rewritten correctly.
2. **Slot resolution**: only the direct `(SlotOf, directParent)` case is handled. Nested-slot (`(SlotOf, grandparentPrefab)`) and prefab-of-prefab instantiation are deferred to a future phase.
3. **Marshal fix**: built-in tag entities (e.g., `Prefab`, `Disabled`) applied to user entities produce a `TypeInfo` with `Name == "tag"` sentinel via `EnsureID`. These are now skipped during `MarshalJSON` component serialization so they don't appear as unresolvable `"tag": {}` entries in the JSON output.
4. **Built-in entity count**: 47; `SlotOf` occupies index 47; user entities start at index 48.

## v0.68.0 — 2026-05-14 — Phase 16.13: Runtime dynamic component registration

Ports runtime (dynamic) component registration from upstream C flecs. A dynamic
component is registered by name, size, and alignment at runtime with no Go type at
compile time. All data is treated as opaque bytes and routed through the same
archetype / sparse / DontFragment machinery as typed components. Closes the dynamic
component gap in `docs/README.md` line 102.

### Added

- **`RegisterDynamicComponent(fw, name, size, align) ID`** — allocates a new component entity whose layout is determined at runtime by `size` and `alignment`. Panics on name collision.
- **`RegisterDynamicComponentWithMarshaler(fw, name, size, align, marshal, unmarshal) ID`** — like `RegisterDynamicComponent` but registers custom JSON marshal/unmarshal hooks, overriding the default base64-encoded bytes representation.
- **`GetIDPtr(s scope, e ID, componentID ID) unsafe.Pointer`** — returns a raw pointer to the component slot for entity `e`, or nil if the entity does not hold the component. Pointer is valid until the next structural change on `e`.
- **`SetIDPtr(fw, e ID, componentID ID, src unsafe.Pointer)`** — copies `size` bytes from `src` into the component slot for `e`. Fires `OnAdd` / `OnSet` / `OnReplace` exactly like typed `Set`. Honors the defer queue.
- **`EachByID(s scope, componentID ID, fn func(e ID, ptr unsafe.Pointer))`** — iterates all live entities holding `componentID`, calling `fn` with the entity ID and a raw pointer.
- **`OnAddByID(w, componentID ID, fn func(fw *Writer, e ID, ptr unsafe.Pointer))`** — registers an `OnAdd` hook for a dynamic component. Pass nil to clear.
- **`OnSetByID(w, componentID ID, fn func(fw *Writer, e ID, ptr unsafe.Pointer))`** — registers an `OnSet` hook for a dynamic component. Pass nil to clear.
- **`OnRemoveByID(w, componentID ID, fn func(fw *Writer, e ID, ptr unsafe.Pointer))`** — registers an `OnRemove` hook for a dynamic component. Pass nil to clear.
- **`dynamicMarshalers map[ID]dynamicMarshalHooks`** field on `World` — stores custom marshal/unmarshal hooks per dynamic component ID.
- **`unmarshalDynamic`** internal helper — decodes dynamic component JSON (base64 or custom hook) and writes the value via `SetIDPtr`.

### Implementation notes

1. **Nil Type marker**: `TypeInfo.Type == nil` is the sentinel for a dynamic component. All downstream code (`column.go`, `sparse.go`, `materializeByPtr`, `GetByID`) handles nil Type explicitly.
2. **Storage**: `column.go:newColumn` synthesizes a `reflect.ArrayOf(int(size), byte)` backing type when `elemType == nil`, enabling reuse of the existing reflect-based column machinery.
3. **Sparse storage**: `sparseSetInsert` synthesizes the same `[size]byte` array type for the boxed pointer when `info.Type == nil`.
4. **Deferred SetIDPtr**: When the world is deferred (`deferDepth > 0`), `SetIDPtr` copies `src` bytes into the command arena and enqueues a `cmdSetByID` command; matched to `Set[T]` and `SetByID` flush semantics.
5. **JSON**: Dynamic components in archetype tables are serialized in the entity body with base64 (or custom hook). DontFragment dynamic components appear in `SparseData` with the same encoding.
6. **No new file needed**: Public API lives in `component_dynamic.go`; internal helpers added to `marshal.go`, `sparse.go`, `internal/component/registry.go`, and `internal/storage/table/column.go`.

### Explicit non-goals (v0.68.0)

- `Get[T]`, `Each[T]`, `Field[T]`, `OnAdd[T]` typed generics are not supported for dynamic components.
- No automatic size/alignment validation against Go struct layout.
- No reflection-based introspection of the byte layout.

---

## v0.67.0 — 2026-05-14 — Phase 16.12: Fixed-source observer terms

Ports fixed-source observer terms from upstream C flecs. An observer registered with `WithSource(e)` fires only when the event lands on the named entity, moving the per-entity filter from application code into the dispatch layer. Closes the fixed-source observer gap in `docs/README.md`.

### Added

- **`WithSource(e ID) ObserverOptions`** — option that constrains `ObserveWithOptions[T]`, `ObserveIDWithOptions`, and `ObserveEventWithOptions` to fire only when the event entity matches `e`. Panics if `e == 0`. Stale (deleted) entity IDs register successfully and silently never fire.
- **`(ObserverOptions).AndSource(e ID) ObserverOptions`** — chaining helper for combining `WithSource` with `WithYieldExisting`: `WithYieldExisting().AndSource(playerID)`. Panics if `e == 0`.
- **`ObserveIDWithOptions(w, id, opts, events, fn)`** — options-bearing variant of `ObserveID`; the raw-ID entry point for fixed-source observers (e.g. pair IDs). Supports `WithSource` and `WithYieldExisting`.
- **`ObserveEventWithOptions(w, eventID, opts, fn)`** — options-bearing variant of `ObserveEvent`; restricts custom-event observers to a named source entity via `WithSource`.
- **`observerBucket`** internal storage type (`anyEntity []*observerNode`, `fixedSource map[ID][]*observerNode`) — replaces the plain `[]*observerNode` slice in the observer dispatch table. `fixedSource` is lazily allocated on first fixed-source registration; any-entity observer dispatch pays zero additional overhead.

### Implementation notes

1. **Storage**: `w.observers` changed from `map[observerKey][]*observerNode` to `map[observerKey]*observerBucket`. The `fixedSource` map within a bucket is nil until the first fixed-source registration for that `(component, event)` key.
2. **Dispatch order**: Any-entity observers in `bucket.anyEntity` fire before fixed-source observers in `bucket.fixedSource[e]`. Registration order is preserved within each list.
3. **yield_existing + WithSource**: O(1) — `entityRawPtrForYield` checks only the named source entity (table or sparse-set); no table walk. Fires once iff the source holds the component and its archetype table does not carry `Disabled` or `Prefab`.
4. **Disabled / Prefab**: Mirrors upstream C behavior — the check is on the **event entity's table** (which, for fixed-source observers, is the source entity's table). Disabled or Prefab sources are skipped in `yield_existing`.
5. **`OnTableCreate + WithSource` panics**: Tables are not entities; this combination is rejected at registration time with a clear message.
6. **No new file**: All new types land in `observer.go`, `observer_options.go`, and `observer_custom.go` per the placement recommendation in the issue.

### Explicit non-goals (v0.67.0)

- No multi-term observers with mixed `$this` / fixed-source terms.
- No fixed-source for monitor observers.
- No automatic cleanup when the named source is deleted.
- No traversal modifiers (e.g. `Up(ChildOf)`) on the fixed source.
- No observer propagation along IsA edges.

---

## v0.66.0 — 2026-05-14 — Phase 16.11: Query groups (group_by_callback port)

Ports `group_by_callback` from upstream C flecs. Closes the query-groups gap in `docs/README.md`.

### Added

- **`GroupByFunc func(t *table.Table) uint64`** — callback type that assigns a group ID to each matched archetype table.
- **`WithGroupBy(componentID ID, groupFn GroupByFunc) CachedQueryOptions`** — partitions a cached query's matched tables into labelled groups. `componentID` (if non-zero) must appear as a `With` or `Maybe` term and serves as the invalidation hint; pass 0 to trigger on any table change.
- **`(*CachedQuery).IterGroup(groupID uint64) *QueryIter`** — O(1) group-iterator lookup; yields only tables in the requested group. Returns an exhausted iterator for non-existent groups.
- **`(*CachedQuery).Groups() []uint64`** — returns the sorted slice of currently-populated group IDs; non-nil empty slice when no tables match; nil when `WithGroupBy` was not used.
- **`(CachedQueryOptions).AndGroupBy(componentID ID, groupFn GroupByFunc) CachedQueryOptions`** — chains group-by onto an existing options set (e.g. one produced by `WithOrderBy`).
- **`(CachedQueryOptions).AndOrderBy(componentID ID, cmp OrderByFunc) CachedQueryOptions`** — chains sort onto an existing options set (e.g. one produced by `WithGroupBy`).

### Compose with WithOrderBy

Both `WithGroupBy` and `WithOrderBy` active on the same query: groups outer, sort inner. Default `Iter()` walks groups in ascending ID order; within each group, entities are yielded in sort-comparator order. `IterGroup` also yields sorted entities for the requested group.

### Implementation notes

1. **Cache invalidation**: full re-group on any table `ChangeCount` change or new-table addition — mirrors sort invalidation from Phase 16.4. Simpler than incremental; adequate for canonical workloads.
2. **Storage**: `map[uint64][]*table.Table` + sorted `[]uint64` group IDs + contiguous range offsets `groupTableStart/End` in `cq.tables` for O(1) `IterGroup` slicing.
3. **Table reordering**: `rebuildGroups` stable-sorts `cq.tables` (and the parallel `cq.tableUpSources`) in group-ID order; default `Iter()` inherits group order from the cached table slice without extra indirection.
4. **Cascade compatibility**: `cascadeTermTrav` plumbing is untouched. `Cascade` retains its dedicated `sortByCascadeDepth` implementation; refactor onto `WithGroupBy` is a future-phase concern.
5. **Marshal**: group state is runtime-only; not serialised. Recomputed lazily on next `Iter()` or `IterGroup()` after `UnmarshalJSON`.

### Explicit non-goals (v0.66.0)

- No `on_group_create` / `on_group_delete` lifecycle events.
- No multi-key grouping (single callback, single component).
- No persistent group identifiers across world reloads.
- No automatic refactor of `Cascade` to use `WithGroupBy`.

---

## v0.65.0 — 2026-05-14 — Phase 16.10: Monitor observers

Implements monitor observers: callbacks that fire once when an entity enters or exits a multi-term query match. Closes the monitor-observers gap in `docs/README.md`.

**Breaking changes**: Built-in entity count increases from 46 to 47 (new `EventMonitor` entity at index 46); user entity allocation now starts at index 47. Code that hardcodes `46` as the built-in entity count must update to `47`. See MIGRATING.md § v0.65.0.

### Added

- **`Monitor(w *World, terms []Term, fn func(fw *Writer, e ID, entered bool)) *Observer`** — registers a monitor observer that fires `fn` with `entered=true` when `e` first matches all `terms`, and `entered=false` when it stops matching. At most one fire per entry/exit transition.
- **`MonitorWithOptions(w *World, terms []Term, opts ObserverOptions, fn ...) *Observer`** — options-bearing variant; `WithYieldExisting()` sweeps matching entities at registration time and fires `fn(fw, e, true)` for each.
- **`(*World).EventMonitor() ID`** — returns the built-in `EventMonitor` event entity (index 46).
- **`EventMonitor EventKind = 5`** — new `EventKind` constant; `EventMonitor.String()` returns `"Monitor"`.

### Implementation notes

- **Hybrid match-state tracking**: archetype-only monitors (no DontFragment/Union/Sparse terms) use a table-pair check per `migrate()` call — O(monitors×terms), no per-entity state. Sparse-mode monitors track a per-monitor `matched` set, updated at each relevant component-change site.
- Monitor callbacks can safely mutate the world via the provided `*Writer` (re-entrancy deferred via the command queue).
- Disabled monitors receive no events and do not accumulate catch-up state on re-enable.
- Entity deletion and `Clear` fire exit events before component removal so callbacks can read component state.

## v0.64.0 — 2026-05-14 — Phase 16.9: Custom pipeline phases + DependsOn ordering

Ports the two remaining Phase 14.6 gaps: user-defined pipeline phases ordered via `DependsOn` edges, and `(*System).DependsOn` for ordering systems within a phase.

**Breaking changes**: The four built-in phase accessors (`World.PreUpdate`, `World.OnFixedUpdate`, `World.OnUpdate`, `World.PostUpdate`) now return `*Phase` instead of `ID`. Code that stored the return value as `flecs.ID` or passed it directly to `NewSystemInPhase` must be updated (see MIGRATING.md § v0.64.0). Built-in entity count increases from 45 to 46 (new `DependsOn` entity at index 45); user entity allocation now starts at index 46.

### Added

- **`NewPhase(w *World, name string) *Phase`** — creates a custom pipeline phase. The returned `*Phase` must be anchored to the built-in chain via `DependsOn`; `Progress` panics on the first tick for any orphan custom phase.
- **`(*Phase).DependsOn(other *Phase) *Phase`** — declares execution-order dependency (runs after `other`). Idempotent; returns `this` for fluent chaining. Panics if `other` is nil or from a different world.
- **`(*Phase).SetEnabled(bool)` / `(*Phase).IsEnabled() bool`** — enable/disable a phase; disabled phases and all their systems are skipped during `Progress`.
- **`(*Phase).Name() string`** — returns the display name of the phase.
- **`(*System).DependsOn(other *System) *System`** — orders `this` system after `other` within the same phase, overriding registration order. Panics if systems are in different phases.
- **`World.DependsOn() ID`** — returns the built-in `DependsOn` relationship entity (index 45).

### Changed

- **`World.PreUpdate() *Phase`**, **`World.OnFixedUpdate() *Phase`**, **`World.OnUpdate() *Phase`**, **`World.PostUpdate() *Phase`** — return type changed from `ID` to `*Phase`. **Breaking**.
- **`NewSystemInPhase(w, phase *Phase, q, fn)`** — `phase` parameter changed from `ID` to `*Phase`. Panics on nil or cross-world phase. **Breaking**.
- **`(*Reader).Phases() []*Phase`** — return type changed from `[]ID` to `[]*Phase`.
- **`(*Reader).SystemsInPhase(phase *Phase) []*System`** — `phase` parameter changed from `ID` to `*Phase`.
- **`(*Reader).EachSystem(phase *Phase, fn func(*System) bool)`** — `phase` parameter changed from `ID` to `*Phase`.
- **`World.SystemCountInPhase(phase *Phase) int`** — `phase` parameter changed from `ID` to `*Phase`.
- **`WorldStats.LastFramePhases`** — type changed from `[4]PhaseStats` to `[]PhaseStats` to accommodate dynamic phase counts.
- **Pipeline rebuild is now lazy**: any mutation that changes phase or system topology sets a `pipelineDirty` flag; the actual topological sort runs at the start of `Progress` or on-demand in introspection calls.
- **Built-in entity count**: 45 → 46. User entities now start at index 46. `MarshalJSON` skip set updated; custom phases are NOT serialized (built-ins only survive round-trip).

### Design decisions recorded

1. **Orphan panic policy**: custom phases with no `DependsOn` edge panic at `Progress` time (not at `NewPhase` time). Matches upstream "fail fast at use" rather than "fail fast at construction".
2. **Cycle panic**: strict panic for both phase cycles and system-within-phase cycles. Error message includes phase/system names.
3. **Kahn sort tiebreaker**: registration order (insertion index) breaks ties in topological order, matching upstream's entity-ID tiebreak. The full queue is re-sorted by position after each step so that freed nodes always follow all previously-freed nodes with smaller registration indices.
4. **Custom phases in marshal**: deliberately not serialized. Phase topology is structural metadata that must be re-declared in code; serializing it would create a fragile coupling between snapshot format and pipeline registration order.
5. **`DependsOn` built-in entity**: bootstrapped with `Relationship` and `PairIsTag` traits (mirrors `ChildOf`/`IsA`). Stored as `w.dependsOnID` (unexported); exposed via `World.DependsOn() ID` for tests and introspection.

## v0.63.0 — 2026-05-14 — Phase 16.8: Custom events

Ports upstream C flecs's custom event mechanism. Applications can now define arbitrary event entities, subscribe observers to them, and emit them as a typed event bus inside an ECS app. The dispatch table now keys on event entity IDs — a structural change that keeps the `EventKind` convenience enum as a 1:1 mapping to built-in event entities while making the dispatch path uniform across built-in and custom events.

**Breaking change**: built-in entity count increases from 40 to 45 (five new built-in event entities at indices 40–44). User entity allocation now starts at index 45 (previously 40). Serialized worlds (`MarshalJSON`) use serial numbers, not raw indices, so existing snapshots round-trip correctly. Code that hardcodes world entity counts must be updated.

**No breaking change** to any existing observer or hook API signatures: `Observe[T]`, `ObserveID`, `Observe2[T]`, `ObserveWithOptions[T]`, `OnTableCreate`, `OnAdd[T]`, `OnSet[T]`, `OnRemove[T]`, `OnReplace[T]`, `EventOnAdd`, `EventOnSet`, `EventOnRemove`, `EventOnTableCreate` all keep their existing signatures and semantics.

### Added

- **`RegisterEvent(fw *Writer, name string) ID`** — allocates a new entity as a custom event identifier. Applies the built-in `Event` tag so `HasID(eventID, w.Event())` is true. The entity can be named, queried, and deleted like any other.
- **`Emit(fw *Writer, eventID ID, entity ID, payload interface{})`** — fires `eventID` for `entity` with an opaque payload. Synchronous; all subscribed observers fire in registration order before `Emit` returns. Payload is shallow-copied at the API boundary.
- **`EmitTyped[T any](fw *Writer, eventID ID, entity ID, payload T)`** — typed wrapper around `Emit`.
- **`ObserveEvent(w *World, eventID ID, fn func(fw *Writer, e ID, payload interface{})) *Observer`** — subscribes to a custom event. Payload arrives as `interface{}`. Returns `*Observer`; call `Unsubscribe` to cancel.
- **`ObserveEventTyped[T any](w *World, eventID ID, fn func(fw *Writer, e ID, payload T)) *Observer`** — typed-payload variant; panics with a clear message on type mismatch at dispatch time.
- **Built-in event entity accessors** on `*World`: `EventOnAdd() ID`, `EventOnSet() ID`, `EventOnRemove() ID`, `EventOnTableCreate() ID`, `Event() ID`. These map 1:1 to the existing `EventKind` constants via `eventKindToEntity`.
- **`eventKindToEntity(w *World, ev EventKind) ID`** (unexported) — maps `EventKind` enum to built-in event entity IDs; used internally at the boundary between legacy callers and the entity-keyed dispatch table.

### Changed

- **`observerKey`** (`observer.go`) — field `event EventKind` replaced by `eventEntity ID`. The dispatch table now keys on event entity IDs for both built-in and custom events.
- **`addObserverNode` / `dispatchObservers`** (`observer.go`) — signatures updated from `event EventKind` to `eventEntity ID`.
- **`fireOnAdd` / `fireOnSet` / `fireOnRemove`** (`hooks.go`) — updated to pass `w.eventOnAddID` / `w.eventOnSetID` / `w.eventOnRemoveID` to `dispatchObservers` instead of `EventOnAdd` / `EventOnSet` / `EventOnRemove`.
- **`notifyTableCreated`** (`world.go`) — updated to pass `w.eventOnTableCreateID` instead of `EventOnTableCreate`.
- **`OnTableCreateWithOptions`** (`observer_table.go`) — updated to pass `w.eventOnTableCreateID` to `addObserverNode`.
- **`deleteOne`** (`world.go`) — when an entity is deleted, its custom event observer entry `{id:e, eventEntity:e}` is removed from the observer map, making subsequent `Emit` calls for that event a no-op.
- **Built-in entity count**: 40 → 45. User entities now start at index 45. `MarshalJSON` skip set updated to exclude all five new built-in event entities.

### Design decisions recorded

1. **Payload type**: untyped `interface{}` for `Emit` + typed generic wrapper `EmitTyped[T]`. Matches upstream `void *param` flexibility without forcing a payload-type registration step.
2. **`Event` tag built-in**: index 44; `RegisterEvent` always applies it. Enables `HasID(eventID, w.Event())` discrimination.
3. **Dispatch key for custom events**: `{id: eventID, eventEntity: eventID}` — both fields equal the event entity ID. Distinct from component observers (`{componentID, eventOnAddID}`) and table-create observers (`{0, eventOnTableCreateID}`).
4. **yield_existing on custom events**: `ObserveEvent` is intentionally not wired to `ObserverOptions`; passing `WithYieldExisting()` is a silent no-op (there is no "currently matching" concept for an arbitrary event).
5. **Re-entrant emit**: synchronous fire path matching the existing hook/observer dispatch. Mutations from within the handler still defer via the existing Writer cmd queue.
6. **Event entity deletion**: O(1) cleanup in `deleteOne` via `delete(w.observers, observerKey{id:e, eventEntity:e})`. Subsequent `Emit` is a no-op.

## v0.62.0 — 2026-05-14 — Phase 16.7: OnTableCreate observer event

Ports `EcsOnTableCreate` from upstream C flecs as a new observer event kind. Observers register via `OnTableCreate(w, fn)` and fire once per archetype table when the table is first created (first entity migrates into a previously-unseen component signature). Closes the OnTableCreate half of `docs/README.md` gap entry; `OnTableDelete` is deferred pending table-reclamation infrastructure.

**Scope note**: ships `OnTableCreate` only. `OnTableDelete` requires Go-flecs to implement table reclamation (`delete(w.tables, ...)` is currently never called; tables persist for World lifetime). `OnTableEmpty` / `OnTableFill` (row-count transitions) remain a separate listed gap.

**C upstream references verified:**
- Event constants: `include/flecs.h:1944–1948` — `EcsOnTableCreate` / `EcsOnTableDelete` (observer-events category, distinct from cleanup-policy traits at lines 1950–1955).
- Fire site: `src/storage/table.c:847–849` — `flecs_table_emit(world, table, EcsOnTableCreate)` after `flecs_table_init_overrides`, gated on `EcsTableHasOnTableCreate`.
- Root suppression: `src/storage/table.c:1278–1281` — `is_root` check suppresses the delete side; Go port mirrors this by excluding the empty root table from dispatch.

### Added

- **`EventOnTableCreate EventKind = 4`** — new event kind constant in `observer.go`.
- **`OnTableCreate(w *World, fn func(fw *Writer, t *Table)) *Observer`** — registers an observer that fires once per newly-created archetype table. Untyped (no `[T]` parameter); the handler receives the new `*Table` directly. Does not fire for the world's initial empty table.
- **`OnTableCreateWithOptions(w *World, opts ObserverOptions, fn func(fw *Writer, t *Table)) *Observer`** — variant that accepts `WithYieldExisting()`: sweeps existing tables synchronously at registration time (excluding the empty root table) in sorted-signature order, then continues to fire for future table creations.
- `tableCreateSentinelID ID = 0` in `observer_table.go` — internal sentinel used as the observer-map key for table-create subscriptions (ID 0 is never a valid entity or component).

### Changed

- **`notifyTableCreated`** (`world.go`) — extended to call `dispatchObservers(tableCreateSentinelID, EventOnTableCreate, 0, unsafe.Pointer(t))` for every non-empty table created, after the existing cached-query notification. Empty root table excluded by `len(t.Type()) > 0` guard.
- **`EventKind.String()`** (`observer.go`) — added `"OnTableCreate"` case.

### Design decisions recorded

1. **Untyped-only API** — upstream `EcsOnTableCreate` is not parameterized by component; Go port follows. No `[T]` variant makes sense since the event is table-level, not component-level.
2. **Sentinel ID 0** — valid entity/component IDs start at 1 in Go-flecs; ID 0 is safe as the observer-map key for the untyped table-create slot.
3. **Empty root table excluded** — matches upstream's `is_root` guard; avoids observer noise during world construction (`observers` is nil at that point anyway).
4. **Re-entry is deferred** — handler writes go through the deferred coalescer; a new archetype created by the handler fires `OnTableCreate` in the next flush iteration, not recursively.
5. **yield_existing ordering** — sorted by signature string for deterministic output within a single run.

## v0.61.0 — 2026-05-14 — Phase 16.6: Rate filters (SetInterval / SetRate)

Ports per-system rate-filter controls that let a system run less often than every pipeline tick. Closes `docs/README.md` gap line 144.

**C upstream references verified:**
- `ecs_ftime_t interval` / `int32_t rate` desc fields: `include/flecs/addons/system.h:87–91`.
- `ecs_set_interval` / `ecs_set_rate` APIs: `include/flecs/addons/timer.h:111–115` and `:203–208`.
- Interval accumulator (subtract-with-cap): `src/addons/timer.c:28–47` (`ProgressTimers`).
- Rate counter (modulo): `src/addons/timer.c:75–83` (`ProgressRateFilters`).
- Per-system gate: `src/addons/system/system.c:41–58` (`flecs_run_intern`).

### Added

- **`(*System).SetInterval(d time.Duration)`** — install a wall-clock interval gate; the system fires when accumulated `dt` reaches `d`. `d == 0` disables interval gating. Resets the accumulator to 0 on call. Panics on negative `d`. Uses subtract-with-cap accumulator: each fire subtracts `d` preserving carry; a single tick whose `dt` vastly exceeds `d` clamps the remainder to `0` (mirrors upstream `timer.c:33–35`).
- **`(*System).GetInterval() time.Duration`** — returns the current interval gate value (`0` = disabled).
- **`(*System).SetRate(n int32)`** — install a tick-count rate gate; the system fires when `rateCounter % n == 0`. `n == 0` or `n == 1` disables rate gating. Resets the counter to 0 on call. Panics on negative `n`.
- **`(*System).GetRate() int32`** — returns the current rate gate value (`0` or `1` = disabled).

### Changed

- **`System` struct** — extended with `interval time.Duration`, `intervalAccum time.Duration`, `rate int32`, `rateCounter int32` fields.
- **`runPhase` closure** — per-system gate in `Progress` extended: interval check (accumulate + subtract-with-cap) and rate check (modulo) compose with the existing `enabled` check. Neither counter advances while a system is disabled.

### Design decisions recorded

1. **AND composition for combined interval + rate** — upstream C flecs rejects systems with both `interval` and `rate` set (`system.c:230–235`). Go-flecs allows both simultaneously because there is no `tick_source` chaining abstraction; the two filters compose cleanly per-system. Documented in `docs/Systems.md § Rate Filters`.
2. **Disabled-system counters do not advance** — re-enabling resumes from pre-disable state; no catch-up storm. Matches the "re-enable doesn't back-fill" design of `SetEnabled`.
3. **`time.Duration` for interval** — Go-idiomatic vs. upstream `ecs_ftime_t` (double seconds). Conversion `time.Duration(float64(phaseDT) * float64(time.Second))` is explicit at the gate boundary.
4. **`int32` for rate** — matches upstream `int32_t`; avoids surprises on 32-bit targets.
5. **Plain field access, no atomics** — matches Phase 16.3 `(*System).SetEnabled` precedent; callers must modify between ticks only.

## v0.60.0 — 2026-05-14 — Phase 16.5: Observer lifecycle bundle (yield_existing + observer disabling)

Ports two upstream C flecs observer features that both live on the observer-registration and observer-fire plumbing in `observer.go`. Closes `docs/README.md` gap lines 156 (yield_existing) and 159 (observer disabling).

**C upstream references verified:**
- `yield_existing` field: `include/flecs.h:1389` on `ecs_observer_desc_t`.
- `yield_existing` implementation: `src/observer.c:761` (`flecs_observer_yield_existing`), triggered at `src/observer.c:1270-1272` after observer construction.
- Observer fire-path Disabled gate: `src/observer.c:342`; bit flipper: `src/observer.c:1491`; same `EcsDisabled` tag reused for observers and systems: `src/bootstrap.c:542`.

### Added

- **`ObserverOptions`** — zero-value struct for `ObserveWithOptions[T]`. Construct via `WithYieldExisting()` or use the zero value for no options.
- **`WithYieldExisting() ObserverOptions`** — returns options that retroactively fire the observer for every entity that already carries the component at registration time. The sweep targets only the newly-registered observer (peer observers subscribed to the same event are not re-fired). Supported events: OnAdd and OnSet; OnRemove-only panics at construction. Skips tables tagged Disabled or Prefab. Synchronous: `ObserveWithOptions` returns only after all entities are visited.
- **`ObserveWithOptions[T any](w *World, opts ObserverOptions, events []EventKind, fn func(fw *Writer, event EventKind, e ID, v T)) *Observer`** — multi-event observer registration with optional configuration. When `opts` carries no options (zero value), behaves identically to `Observe2[T]`. The generic, multi-event form mirrors `Observe2[T]` plus options, following the `NewCachedQueryFromTermsWithOptions` introduction pattern.
- **`(*Observer).SetEnabled(v bool)`** — enable or disable this observer for event dispatch. A disabled observer is silently skipped in `dispatchObservers` but remains registered and can be re-enabled at any time. Default is true (enabled). Idempotent. Mirrors `(*System).SetEnabled` from Phase 16.3.
- **`(*Observer).IsEnabled() bool`** — reports whether this observer is currently enabled for dispatch.

### Changed

- **`Observer` struct** — extended with `enabled bool` field. Initialised `true` in all constructors (`Observe[T]`, `ObserveID`, `Observe2[T]`, `ObserveWithOptions[T]`).
- **`observerNode` struct** — extended with `observer *Observer` back-pointer. Set at registration; used by `dispatchObservers` to read the enabled flag with one pointer deref per node.
- **`dispatchObservers`** — now skips nodes where `n.observer != nil && !n.observer.enabled` in addition to `n.removed`. Hook callers (`fireOnAdd`, `fireOnSet`, `fireOnRemove`) are unaffected.

### Design decisions recorded

1. **Plain `bool` (not `atomic.Bool`)** — matches Phase 16.3 (`*System.enabled`). Observer fire-path is gated by the world's exclusive-access invariant; concurrent toggle from outside an active Write violates that invariant regardless.
2. **Back-pointer on `observerNode`** — one word per node; cleaner than threading the flag separately and avoids a map lookup on the hot dispatch path.
3. **Direct callback invocation in yield sweep** — sweep does NOT route through `dispatchObservers`. Only the newly-registered observer's callback fires; peer observers on the same event are not re-fired. Mirrors upstream `it.callback = flecs_default_uni_observer_run_callback`.
4. **`ObserveWithOptions` as a new entry point** — existing `Observe[T]` / `ObserveID` / `Observe2[T]` remain unchanged. Mirrors `NewCachedQueryFromTermsWithOptions` introduction style from Phase 16.4.
5. **No free-function aliases for enable/disable** — Phase 16.3 chose methods-only; mirrored exactly. No `DisableObserver` / `EnableObserver` free functions.

---

## v0.59.0 — 2026-05-14 — Phase 16.4: Sorted cached queries (order_by_callback port)

Ports upstream flecs' `order_by_callback` to Go flecs. A cached query can supply a
comparator function and a sort-by component ID; the query yields its matched entities
in sorted order on each `Iter()` call. The sort is lazy: it runs only when the underlying
data has changed (table `ChangeCount` increased or a new table was added), not on every
iteration. Closes the gap entry at `docs/README.md` line 110.

### Added

- **`OrderByFunc`** — comparator type `func(eA ID, vA unsafe.Pointer, eB ID, vB unsafe.Pointer) int`.
  Negative means A < B, zero means equal, positive means A > B. `vA`/`vB` point to the
  sort-by component value for each entity; they are `nil` when the sort-by component is
  `TermOptional` (i.e., `Maybe`) and not present on that entity.
- **`OrderBy[T](cmp func(eA ID, vA *T, eB ID, vB *T) int) OrderByFunc`** — typed convenience
  wrapper that casts the raw `unsafe.Pointer` arguments to `*T`. Nil pointers are forwarded as
  nil `*T` (the optional-absent case).
- **`CachedQueryOptions`** — zero-value struct for `NewCachedQueryFromTermsWithOptions`. Construct
  via `WithOrderBy` or use the zero value for no sort.
- **`WithOrderBy(componentID ID, cmp OrderByFunc) CachedQueryOptions`** — returns options that
  sort the query by `componentID` using `cmp`. `componentID` must appear as a `With` or `Maybe`
  term; pair IDs are not supported in v0.59.0 (panics at construction with a clear message).
- **`NewCachedQueryFromTermsWithOptions(w *World, opts CachedQueryOptions, terms ...Term) *CachedQuery`** —
  new constructor that accepts optional configuration before the term list. When `opts` carries
  no sort (zero value), behaves identically to `NewCachedQueryFromTerms`.

### Changed

- **`CachedQuery`** — extended with `orderBy ID`, `orderByCmp OrderByFunc`, `sortedEntities []ID`,
  `sortedRows []sortedFieldRow`, `sortedLastChange map[*table.Table]uint64`,
  `sortedLastSparseVer map[ID]uint64`. Fields are zero / nil when no sort is configured; no
  overhead for unsorted queries.
- **`QueryIter`** — extended with `sortedMode bool`, `sortedPos int`, `sortedEntities []ID`,
  `sortedRows []sortedFieldRow`. When `sortedMode` is true, `Next()` dispatches to `nextSorted`
  which yields one entity at a time in sort order, wiring the worker-clip trick
  (`wFirst=row, wCount=1, workerTotal=1`) so that `Field[T]` returns a length-1 slice over the
  entity's archetype column row as usual.
- **`CachedQuery.Iter()`** — sorted path runs before all other paths. Calls `needsSortRebuild()`
  and `rebuildSorted()` lazily, then returns an iterator in `sortedMode`.

### Design decisions recorded

1. **Global `sort.SliceStable` instead of upstream's two-step algorithm.** Upstream uses per-table
   in-place quicksort + k-way merge. Go flecs uses a single `sort.SliceStable` over all matched
   entities. Observable ordering is identical and the implementation is far simpler. The
   performance difference is a future optimization target if benchmarks show it matters.
2. **`NewCachedQueryFromTermsWithOptions` as a new constructor** rather than variadic options
   appended to `NewCachedQueryFromTerms`. Keeps the existing call sites unmodified and avoids
   ambiguity between option structs and term values at the call site.
3. **Panic at construction if sort-by component is absent.** Matches Go flecs' strict-validation
   precedent in `validateAndSortTerms`. Clear error message at construction beats a silent
   wrong-result or nil-dereference at iteration time.
4. **Pair IDs as sort-by component deferred to v0.60.0+.** Pair storage is in archetype columns
   or union store; the extra dispatch is straightforward but not needed for the primary use case.
   Users can work around with a packed struct component for multi-key sort.
5. **`ChangeCount`-based invalidation** rather than upstream's `OnSet` monitor subscription.
   `ChangeCount` covers all column writes and structural changes (entity add/remove) in one
   monotonic counter per table; no observer subscription overhead per sorted query.

### Changed (docs)

- `docs/Queries.md` — new § **Sorted queries** section added (above Performance Notes) with
  `OrderBy[T]` and raw `OrderByFunc` examples, lazy-invalidation explanation, optional sort-by
  component usage, and constraints. The "Not Yet Ported" stub for sorted queries replaced with a
  cross-reference to the new section.
- `docs/README.md` — line 110 flipped to ✅ shipped (v0.59.0) with anchor link to the new
  Queries.md section.
- `README.md` — new feature-list row for sorted cached queries.
- `ROADMAP.md` — "Shipped (through v0.58.0)" heading bumped to "through v0.59.0"; v0.59.0
  entry added.

## v0.58.0 — 2026-05-14 — Phase 16.3: System lifecycle bundle (disabling, single-Run, pipeline introspection)

Ships three independent system-side features as a bundle, closing three entries
from the docs/README.md feature-gap list (Phase 14.6 Systems section, lines 143, 145, 147).
Implemented via **Option A** (bool field on `*System`) rather than Option B (entity-per-system):
Option A is minimal, self-contained, and avoids an entity-allocation cost per system. The
pipeline executor checks `s.enabled` at O(1) per system per phase — the same cost as the
per-table `Disabled` exclusion shipped in v0.57.0. The two mechanisms are intentionally
independent: `SetEnabled` does not reuse `DisableEntity`/`IsDisabled`.

### Added

- **`(*System).SetEnabled(v bool)`** — pauses (`false`) or resumes (`true`) a system for
  pipeline dispatch. Default is `true` at construction. Idempotent. Unlike `Close`, reversible.
- **`(*System).IsEnabled() bool`** — queries the current enabled state.
- **`RunSystem(s *System, dt float32)`** — synchronously invokes one system once, outside the
  normal pipeline. Bypasses phase ordering, parallel batching, multi-threaded splitting, and the
  `enabled` flag (explicit invocation always runs). Opens its own implicit `deferScope`; deferred
  mutations are flushed before `RunSystem` returns, matching `ecs_run`'s `flecs_defer_begin` /
  `flecs_defer_end` wrap. Panics if `s` is `nil` or `s.IsClosed()`.
- **`(*Reader).Phases() []ID`** — returns `[PreUpdate, OnFixedUpdate, OnUpdate, PostUpdate]` in
  execution order.
- **`(*Reader).SystemsInPhase(phase ID) []*System`** — snapshot of all non-closed (including
  disabled) systems in the given phase, in registration order. Returns empty non-nil slice for
  phases with no systems. Panics on non-built-in phase ID, matching `SystemCountInPhase`.
- **`(*Reader).EachSystem(phase ID, fn func(*System) bool)`** — zero-alloc callback variant;
  `fn` returning `false` halts iteration. Same panic contract as `SystemsInPhase`.

### Changed

- **`runPhase`** (pipeline executor in `system.go`) — active-set filter extended from
  `!s.removed && s.phase == p` to `!s.removed && s.enabled && s.phase == p`.
- **`countPhase`** (internal per-phase count used for frame timing) — same filter extension.
  `SystemCount` and `SystemCountInPhase` intentionally unchanged: they count all non-closed
  systems regardless of enabled state.

### Design decisions recorded

1. **`RunSystem` on a disabled system runs anyway.** Matches C `ecs_run`; explicit invocation
   overrides the pipeline-disabled state.
2. **`RunSystem` scope.** Opens its own implicit `deferScope`, matching C's
   `flecs_defer_begin` / `flecs_defer_end` wrap. Callable from outside any other scope.
3. **Introspection signature.** Ships both `SystemsInPhase` (returns `[]*System`, copy-safe)
   and `EachSystem` (callback, zero-alloc). Common case gets the slice; hot path gets the callback.
4. **Snapshot semantics.** `SystemsInPhase` / `EachSystem` return a snapshot at call time.
   Systems added during iteration are not reflected.
5. **Naming.** `SetEnabled` / `IsEnabled` on `*System`, not `Enable` / `Disable` — avoids
   collision with the CanToggle generic `Enable[T]` / `Disable[T]` shipped in v0.35.0.

### Changed (docs)

- `docs/Systems.md` — "System Lifecycle" stub `> Not yet ported` removed; three new sections
  added after it: **Disabling a System**, **Single-system Run (out-of-pipeline)**, **Pipeline
  Introspection**. Each with a compilable code example verified in `docs/systems_examples_test.go`.
- `docs/README.md` — lines 143, 145, 147 flipped to ✅ shipped (v0.58.0) with anchor links.
- `README.md` — three new feature-list rows for system disabling, `RunSystem`, and pipeline
  introspection.
- `ROADMAP.md` — Phase 16.3 shipped entry added; OnTableEmpty/OnTableFill and downstream
  candidates renumbered (16.3→16.4, 16.4→16.5, ..., 16.9→16.10).

## v0.57.0 — 2026-05-14 — Phase 16.2: Disabled and Prefab built-in tags

Ships the `Disabled` and `Prefab` built-in tags as a bundle, closing two entries
from the docs/README.md feature-gap list (Phase 14.1 entity-disabling gap and
Phase 14.5 Prefab-tag gap).

### Added

- **`World.Disabled() ID`** — built-in `Disabled` tag entity (index 36). Entities
  carrying this tag are excluded from ordinary queries. Built-in entity count raised
  to 39; user entities now start at index 40.
- **`DisableEntity(fw *Writer, e ID)`** — adds the `Disabled` tag; idempotent.
  Causes an archetype migration (same as any tag add); the resulting table is excluded
  at the per-table level — O(1), no per-entity cost. Named `DisableEntity` to avoid
  shadowing the CanToggle generic `Disable[T]`.
- **`EnableEntity(fw *Writer, e ID)`** — removes the `Disabled` tag; no-op when not
  disabled. Named `EnableEntity` symmetrically with `DisableEntity`.
- **`IsDisabled(s scope, e ID) bool`** — predicate; accepts `scope` interface (works
  in both `Read` and `Write` blocks per Phase 15.8 convention).
- **`World.Prefab() ID`** — built-in `Prefab` tag entity (index 37). Entities carrying
  this tag are excluded from ordinary queries. The tag is bootstrapped with `DontInherit`
  (mirrors C `bootstrap.c:1308`) so IsA instances do not acquire it.
- **`MarkPrefab(fw *Writer, e ID)`** — adds the `Prefab` tag; idempotent.
- **`IsPrefab(s scope, e ID) bool`** — predicate; accepts `scope`.
- **Query implicit skip** — `NewQuery`, `NewQueryFromTerms`, `NewCachedQuery`,
  `NewCachedQueryFromTerms` all detect whether `Disabled` / `Prefab` are mentioned in
  any term kind (`With`, `Without`, `Maybe`, `Or`). When not mentioned, `matchesTable`
  / `tryMatchTable` short-circuit with a single `HasComponent` test per flag per table.
  No synthetic Not terms are injected; the original term list is preserved for
  `Terms()` / `TermsFull()` introspection.

### Changed

- **`meta_test.go`** `builtinEntityCount` updated 37 → 39.
- **`ordered_children_test.go`** / **`with_test.go`** — Wildcard/Any index assertions
  updated (36→38, 37→39).
- **`isa_test.go`** — `TestIsAWorldCountBaseline` updated 37 → 39.
- **`marshal.go`** / **`marshal_test.go`** — `Disabled` and `Prefab` added to skip-sets.

### Changed (docs)

- `docs/EntitiesComponents.md` § Disabling: replaced "Not yet ported" stub with
  `DisableEntity` / `EnableEntity` / `IsDisabled` API and use-case description.
- `docs/PrefabsManual.md` § Prefab tag: new section with `MarkPrefab` / `IsPrefab`
  pattern and DontInherit semantics. § Not yet ported: removed Prefab-tag entry.
- `docs/Queries.md`: new "Disabled and Prefab entities" section with opt-in examples
  and O(1) complexity note.
- `docs/README.md` lines 100 and 135: flipped to shipped (v0.57.0).
- `README.md`: feature table row added.
- `ROADMAP.md`: Shipped heading bumped to "through v0.57.0"; Phase 16.2 entry added;
  OnTableEmpty/OnTableFill renumbered to Phase 16.3.

## v0.56.0 — 2026-05-14 — Phase 16.1: Clear, MakeAlive, SetVersion

Drains three more entries from the docs/README.md feature-gap list identified
in Phase 14.1 (EntitiesComponents port).

### Added

- **`Clear(fw *Writer, e ID) bool`** — removes all components, tags, and pairs from entity `e`,
  leaving it alive in the empty archetype. `OnRemove` fires for each component; `OnDelete` does not
  fire. Deferred when called inside `w.Write`; the coalescing queue skips prior `AddID`/`Set` for
  the same entity and re-applies only commands queued *after* the `Clear`. Mirrors C `ecs_clear`.
- **`MakeAlive(fw *Writer, id ID) ID`** — claims a specific entity ID (index + generation) for use
  in this world. Useful for networked scenarios where both peers must share the same entity IDs.
  Slot-free: advances the registry to the requested generation and places the entity in the empty
  archetype. Slot alive at same generation: no-op. Slot alive at different generation: panics with a
  descriptive message. Panics when called in a deferred scope. Removes the raw index from the FIFO
  recycle queue if present (so `NewEntity` will not re-issue the claimed slot). Mirrors C
  `ecs_make_alive`.
- **`SetVersion(fw *Writer, versionedID ID)`** — overrides the generation counter on an alive
  entity. After the call `IsAlive(oldID)` is false and `IsAlive(versionedID)` is true. Panics on
  downgrade (new generation < current generation — use `Delete` + `MakeAlive` to reset deliberately;
  this is a deliberate divergence from C `ecs_set_version` which accepts any value), on dead entity,
  and in deferred scope. Mirrors C `ecs_set_version`.
- **`entityindex.Index.GetCurrentByIndex(rawIndex uint32) (ids.ID, bool)`** — returns the currently
  alive entity at `rawIndex`, or `(0, false)` if dead or never allocated.
- **`entityindex.Index.MakeAlive(id ids.ID) (ids.ID, bool)`** — lower-level primitive backing
  `flecs.MakeAlive`; handles recycle-queue removal and dense-vector placement.
- **`entityindex.Index.SetVersion(rawIndex uint32, newGen uint32)`** — lower-level primitive backing
  `flecs.SetVersion`; updates the dense vector entry directly.
- **`cmdClear`** — new command kind in the deferred queue; dispatched by `batchForEntity` (clears
  prior commands for the same entity, resets migration baseline to the empty archetype) and by
  `dispatch` (direct `clearImmediate` call for non-coalesced paths).
- **Writer methods** — `(*Writer).Clear`, `(*Writer).MakeAlive`, `(*Writer).SetVersion` thin
  wrappers for call-site ergonomics.

### Changed (docs)

- `docs/EntitiesComponents.md` §§ Clearing, Manual IDs, Manual Versioning: replaced "Not yet ported"
  stubs with working Go code examples.
- `docs/README.md` feature-gap list: flipped three entries from "not yet ported" to "shipped v0.56.0".

## v0.55.0 — 2026-05-14 — Phase 16.0: OnReplace hook

First phase beyond the trait-system roadmap; resumes draining the docs/README.md feature-gap list.
The Phase 14.8 ComponentTraits gap entries are now exhausted — 16.x continues with observer/hook
and entity gaps.

### Added

- **`OnReplace[T]`** — registers a per-component lifecycle hook that fires when `Set[T]` overwrites
  an existing component value. Receives both the previous (`old`) and incoming (`new`) value, by
  value, before the slot is overwritten. Does **not** fire on the first `Set` (which uses `OnAdd` +
  `OnSet`). Dispatch order on overwrite: `OnReplace` → column write → `OnSet`. `OnSet` still fires
  after `OnReplace`.
- **`OnReplaceID`** — untyped (ID-keyed) variant. Handler receives `(fw *Writer, e ID, oldPtr,
  newPtr unsafe.Pointer)`; both pointers are valid only for the duration of the call.
- **`fireOnReplace`** — internal dispatcher wired into all fire sites:
  - `setImmediateByPtr` — archetype, sparse-only, and DontFragment branches.
  - `setPairImmediate[T]` — pair overwrite path.
  - `dispatch` `cmdModified` — deferred archetype and sparse legs.
- **`cmd.firstAdd`** — per-cmd flag (uses existing padding byte) marking the first `cmdModified`
  write to a just-migrated slot; dispatch skips `OnReplace` for that write to preserve the
  "first add is not a replace" semantic.
- **`sortedIDContains` / `sliceIDContains`** — small helpers used by batchForEntity Pass 2 to
  track which newly-added component IDs have been first-add-marked.
- **`component.ReplaceCallback`** — new callback type `func(world any, entity ids.ID, oldPtr, newPtr unsafe.Pointer)`.
- **`Hooks.OnReplace`** — new field on `component.Hooks`.

### Changed (docs)

- `docs/ObserversManual.md`: added **OnReplace Hook** section under `## Hooks`; removed the
  "Not Yet Ported — OnReplace Event" stub.
- `docs/EntitiesComponents.md`: updated hooks table to include `OnReplace[T]`; replaced the
  "not yet ported" note with a callout and link to ObserversManual.
- `docs/README.md`: flipped Phase 14.1 line 101 and Phase 14.7 line 152 from "not yet ported"
  to ✅ shipped v0.55.0; fixed Phase 14.8 line 171 (`Relationship`/`Target`/`Trait`) from
  "not yet ported" to ✅ shipped v0.47.0.
- `ROADMAP.md`: updated heading to "Shipped (through v0.55.0)"; added v0.55.0 entry; added
  "Observer-system gaps (Phase 16.x candidates)" future-work section.
- `README.md`: added `OnReplace[T]` / `OnReplaceID` to Hooks feature row.

## v0.54.0 — 2026-05-13 — Phase 15.22: Union relationship trait

### Added

- **`Union` relationship trait** — `SetUnion(w *World, relID ID)`, `IsUnion(s scope, relID ID) bool`,
  `EachUnion(s scope, relID ID, fn func(e ID, target ID))`.
  Marks a relationship as union: at most one target per entity, stored in a per-relationship side
  map (`unionStore`) rather than the archetype table. Unlike `Exclusive`, union pairs never trigger
  an archetype transition — the entity's archetype is unchanged when the target is added, replaced,
  or removed. Union implies Exclusive (both traits are active; the exclusive path is also enforced).
- **`unionStore` / `unionRelStore`** — per-relationship dense slice + index map for O(1) lookup and
  stable iteration order. Keyed by relationship index (not full entity ID) for generation safety.
- **`unionStoreSet` / `unionStoreRemove` / `unionStoreRemoveEntity`** — internal helpers wiring the
  union store into `addIDImmediate` / `removeIDImmediate` / `deleteOne`.
- **`isUnionTermID(w, id) bool`** — returns true when a query term refers to a union pair. Drives
  `Term.Union` routing hint.
- **`Term.Union bool`** — new field on `Term`, stamped by `NewQuery` / `NewCachedQuery`. Signals
  the iteration engine to consult the union store rather than archetype columns.
- **Pure-union query iteration** — when all `TermAnd` terms are union pairs, `QueryIter` iterates
  the smallest matching union store directly (`nextUnionOnly`), without visiting any archetype table.
- **`CachedQuery.unionAndOnly bool`** — pure-union cached queries skip `tryMatchTable` (union pairs
  have no archetype columns to track); `Iter()` drives from the union store directly.
- **`Reader.HasID` / `Reader.OwnsID` union branch** — pair IDs for union relationships are now
  resolved through the union store before the archetype table lookup.
- **`batchForEntity` union bypass** — deferred `cmdAddID` / `cmdRemoveID` commands for union pairs
  skip `scratch1` modification and `cmdSkip` rewriting, so `dispatch` routes them to
  `addIDImmediate` / `removeIDImmediate` which write to the union store.
- **Marshal round-trip** — `MarshalJSON` emits `union_relationship_serials` (which entity serials
  are union relationships) and `union_relationships` (active targets per entity). `UnmarshalJSON`
  restores policies in Phase 1b (after entity allocation, before body replay) and targets in Phase 3b.
- **Conflict detection** — `SetUnion` panics if the relationship already has `SetExclusive`;
  `SetExclusive` panics if the relationship already has Union. Data-bearing `SetPair[T]` /
  `SetPairByID` on a union relationship panics with a clear message.
- **Hook integration** — `OnRemove` fires with the old target; `OnAdd` fires with the new target
  on replace. `OnRemove` fires for every active union pair when an entity or the relationship
  entity is deleted.
- **22 tests in `union_test.go`** — no-archetype-transition, replace target, HasID wildcard and
  specific, RemoveID specific/wildcard, SetPair panic, query wildcard/specific/scale/mixed,
  conflict detection, marshal round-trip, entity delete, relationship delete, IsUnion, idempotent
  SetUnion, EachUnion insertion order, hooks on replace, CachedQuery, OwnsID.

### Changed

- **`matchesSparseTerms`** — extended to handle `Term.Union` (TermAnd + TermNot) in addition to
  existing DontFragment terms.
- **`matchesTable`** — union terms are skipped in archetype-column verification (like DontFragment):
  `if term.DontFragment || term.Union { continue }`.

## v0.53.0 — 2026-05-13 — Phase 15.21: Sparse/DontFragment split (BREAKING)

### Breaking changes

- **`Sparse` alone no longer suppresses archetype transitions.** In v0.52.0, `SetSparse` consolidated
  upstream `EcsSparse + EcsIdDontFragment` into a single trait. In v0.53.0, these are split:
  - `Sparse` alone: data in sparse-set, entity DOES transition archetype tables on add/remove.
  - `DontFragment` alone: data in sparse-set, entity does NOT transition archetype tables.
  - `Sparse + DontFragment` together: the canonical combination matching v0.51.0–v0.52.0 `Sparse` behavior.
  See `MIGRATING.md` for the full migration guide.
- **Built-in entity index shift:** `DontFragment` inserted at index 35; `Wildcard` shifts 35→36;
  `Any` shifts 36→37; user entities now start at index 38. The built-in entity count is now 37.
- **`MarshalJSON` format changed:** `sparse_data` now only covers DontFragment components.
  New `dont_fragment_components` field. Sparse-only data is in the entity body. v0.52.0 snapshots
  are not forward-compatible; regenerate snapshots after migrating.

### Added

- **`DontFragment` trait** — `SetDontFragment(w *World, componentID ID)`, `IsDontFragment(s scope, componentID ID) bool`,
  `w.DontFragment() ID` (index 35), `fw.AddID(compID, w.DontFragment())` bare-tag form.
  `SetDontFragment` panics if the component is already in use. `applyDontFragmentPolicy` initializes
  the backing sparse-set storage (shared with `Sparse`).
- **`isDontFragmentTermID(w *World, id ID) bool`** — returns true when a component has the DontFragment
  trait. Used by query terms (`Term.DontFragment`) to select iteration mode (pure sparse-set vs mixed).
- **`Term.DontFragment bool`** — new field on `Term`, stamped by `validateAndSortTerms` and `NewQuery`.
  Drives iteration mode selection: DontFragment terms trigger pure sparse-set iteration.
- **Three-way dispatch in `setImmediateByPtr`** — `DontFragment` path (no archetype migrate),
  `Sparse-only` path (`migrateArchetypeOnly` + sparse-set write), archetype path.
- **`migrateArchetypeOnly(e, addID, removeID)`** — helper that performs archetype table migration
  without firing `OnAdd`/`OnRemove` hooks. Used by Sparse-only add/remove paths where hooks are
  fired externally with the sparse-set pointer.
- **10 new tests** in `dont_fragment_test.go` — built-in index, no-archetype-transition, sparse-only
  does-transition, Sparse+DontFragment=old-behavior, IsDontFragment roundtrip, HasID/OwnsID,
  Remove, after-use panic, query integration, marshal roundtrip, bare-tag AddID form.
- **`MIGRATING.md`** — full migration guide from v0.52.0 to v0.53.0.

### Changed

- **`isSparseTermID`** — now returns true for both `Sparse` and `DontFragment` components (data in sparse-set).
- **`sparseAndOnly` in `CachedQuery`** — now driven by `dontFragmentAndCount` (not `sparseAndCount`).
  Only pure-DontFragment queries use the sparse-set driver; Sparse-only queries use mixed archetype-seeded iteration.
- **`tryMatchTable` in `CachedQuery`** — now skips `term.DontFragment` (not `term.Sparse`) for archetype checks.
- **`HasID` / `OwnsID`** — now check `sparsePolicies OR dontFragmentPolicies` before falling through to archetype.
- **`Remove[T]` deferred path** — now checks `dontFragmentPolicies` in addition to `sparsePolicies`.
- **`deleteOne`** — skips OnRemove for Sparse-only components in the archetype loop; `sparseHeld` cleanup fires OnRemove with the correct sparse-set pointer.
- **`MarshalJSON`** — emits `dont_fragment_components` list; `sparse_data` now covers DontFragment data only.
- **`UnmarshalJSON` Phase 0** — restores both `sparse_components` and `dont_fragment_components` policies.
- **`CachedQuery.Changed()` tablesAdded path** — now also syncs `sparseVersions` to prevent double-reporting.
- **`TestSparse_NoArchetypeTransition` → `TestSparse_ArchetypeTransition`** — updated to reflect that Sparse-only DOES cause an archetype transition in v0.53.0.
- **`TestSparse_QueryIterSparseCountTableEntities`** — updated: Sparse-only uses mixed mode (Table() does not panic); DontFragment-only added to verify Table() panic.
- **`TestSparse_MarshalRoundTrip`** — updated: sparse_data absent for Sparse-only; data in entity body.
- **`builtinEntityCount`** updated from 36 → 37 in `meta_test.go`.
- **Wildcard/Any index assertions** updated in `ordered_children_test.go`, `with_test.go`, `isa_test.go`.

## v0.52.0 — 2026-05-13 — Phase 15.20: Sparse query integration

### Added

- **Sparse-aware query terms** — `NewQuery`, `NewQueryFromTerms`, `NewCachedQuery`, and `NewCachedQueryFromTerms` now compile each term with `Sparse: true` when the term's component has the Sparse trait. Sparse pair terms are not marked sparse (pairs remain archetype-stored in this release).
- **Three-mode query iterator** — `QueryIter` dispatches to one of three paths based on term composition:
  - **All-sparse** (every required TermAnd term is sparse): the smallest sparse-set is chosen as the iteration driver; each candidate entity is cross-checked against the remaining sparse required/not terms. Yields one entity at a time (`Count()=1`, `Entities()` returns a single-element slice).
  - **Mixed** (at least one sparse term alongside archetype terms): iterates matching archetype tables; for each table entity, sparse terms are checked via the sparse-set before yielding. `Not`/`Optional` sparse terms handled per-entity.
  - **All-archetype** (no sparse terms): the existing fast table-based path, unchanged.
- **`Field[T]`/`FieldMaybe[T]` sparse branches** — when a term is sparse, `Field[T]` returns a 1-element `unsafe.Slice` pointing to the entity's stable boxed value. `FieldMaybe[T]` returns the slice and `true` when present, `nil` and `false` when absent. Neither allocates.
- **`Not`/`Optional` on sparse terms** — `TermNot` sparse terms require the entity to be absent from the sparse-set; `TermMaybe` sparse terms populate the optional slot but do not filter.
- **Sparse version counter for `CachedQuery.Changed()`** — `sparseSet.version` (uint64) is bumped on each structural change (new entry insert or removal, not in-place updates). `CachedQuery.Changed()` consults `cq.sparseVersions map[ID]uint64` after its archetype checks; returns `true` on first call (unseen version) and whenever a sparse-set version advances.
- **Pure-sparse cached query shortcut** — `CachedQuery` sets `sparseAndOnly = true` when all required terms are sparse and there are no archetype terms. `Iter()` on such queries builds a fresh driver directly from the sparse-sets each call (no stale table caching required). Mixed queries still cache the archetype table list.
- **`isSparseTermID(w *World, id ID) bool`** helper in `sparse.go` — returns `false` for pair IDs (which remain archetype-stored), checks `w.sparsePolicies` for scalar IDs.
- **13 new tests** in `sparse_test.go` — `TestSparse_QueryPureSparse`, `TestSparse_QueryMixed`, `TestSparse_QueryAllArchetypeRegression`, `TestSparse_QueryWildcardPairOnSparseRelationship`, `TestSparse_QueryNotSparse`, `TestSparse_QueryOptionalSparse`, `TestSparse_QueryFieldPtrCorrectness`, `TestSparse_QueryFieldPtrMutation`, `TestSparse_CachedQueryVersionCounter`, `TestSparse_QueryEmptySparseset`, `TestSparse_QuerySmallestDriverHeuristic`, `TestSparse_QueryPureSparseZeroEntities`, `TestSparse_QueryMarshalRoundTrip`.

### Changed

- **`Term.Sparse bool`** (new field) — `validateAndSortTerms` stamps this field after promotion resolution so downstream code can branch on it without re-checking `sparsePolicies`.
- **`matchesTable`** — TermAnd and TermNot branches now skip the archetype column check when `term.Sparse` is true; sparse presence is validated per-entity in `matchesSparseTerms`.
- **`sparseSet.version`** — new uint64 field on `sparseSet`; `sparseSetInsert` bumps it only on new-entry (not in-place update); `sparseSetRemove` bumps it on deletion.
- **`w.Sparse()` doc comment** — removed "deferred to Phase 15.20" note.

## v0.51.0 — 2026-05-13 — Phase 15.19: Sparse component storage (storage path only)

> **Part 1 of 2 — storage only.** This release shipped the Sparse storage backend and the manual write/read/remove API. Query integration (query terms naming a Sparse component) is available in v0.52.0.

### Added

- **`SetSparse(w *World, componentID ID)`** — marks `componentID` as Sparse-stored. Idempotent. Panics if the component is a tag (zero-size) or not registered, or if it has already been added to any entity via archetype storage (set-before-first-use enforcement mirrors the Phase 15.16 `SetPairIsTag` after-use trap).
- **`IsSparse(s scope, componentID ID) bool`** — reports whether `componentID` has the Sparse trait. Accepts `scope` so it works inside both `Read` and `Write` blocks (per Phase 15.8 convention).
- **`w.Sparse() ID`** — returns the built-in Sparse trait entity (index 34). Bare-tag form: `fw.AddID(posID, w.Sparse())` is equivalent to `SetSparse(w, posID)`.
- **Per-component sparse-set storage** — `World.sparseStorage map[ID]*sparseSet` holds one `sparseSet` per Sparse component. Each set uses a dense vector + sparse page index for O(1) insert/remove/lookup. Pointer stability is achieved by boxing each value on the heap via `reflect.New` so that dense slice growth never moves existing component addresses.
- **No archetype transition** — Set/Remove on a Sparse component does NOT call `w.migrate`; the entity stays in its current archetype table. `HasID`, `OwnsID`, `GetRef`, `GetByID`, and `Get[T]` all consult the sparse-set rather than the archetype type.
- **Write path** — `setImmediateByPtr` detects sparse via `w.sparsePolicies` and routes to `sparseSetInsert`. Fires `OnAdd` (first write) and `OnSet`. Honors `WriteOnce` and `Singleton` composition.
- **Read path** — `getOnWorld[T]`, `getRefOnWorld[T]`, `GetByID`, `HasID`, `OwnsID`, `hasOnWorld[T]` all branch on `sparsePolicies` before consulting the archetype table.
- **Remove path** — `removeImmediate[T]`, `removeIDImmediate`, and the deferred `Remove[T]`/`RemoveID` paths branch on `sparsePolicies`. Fires `OnRemove` before deletion.
- **Deferred path** — `Set[T]` and `Remove[T]` inside a `Write` block queue `cmdSetByID`/`cmdRemoveID`; the flush dispatcher routes through `setImmediateByPtr`/`removeIDImmediate` which handle sparse correctly.
- **`AddID` rejection** — `AddID(e, sparseComponentID)` panics with `"flecs: AddID: cannot add Sparse component %v as a tag; use Set with a value"` on both immediate and deferred paths. Sparse components are data-bearing.
- **Entity-delete cleanup** — `deleteOne` uses `w.sparseHeld map[uint32][]ID` (entity raw-index → sparse component IDs held) for O(k) cleanup where k = sparse components on the entity. Fires `OnRemove` before removal.
- **`EachSparse[T](s scope, fn func(e ID, v *T))`** — iterates all entities holding T as Sparse in dense (insertion) order. The pointer `v` is the stable boxed pointer to the entity's data. Snapshot-on-entry makes in-callback mutation safe. Phase 15.20 will integrate Sparse into the query system; until then `EachSparse` is the bulk-iteration entry point.
- **Marshal/unmarshal round-trip** — `MarshalJSON` adds a `sparse_policies` field (list of component names with the Sparse trait) and `sparse_data` (component name → entity serial → JSON-encoded value). Unmarshal order: `sparse_policies` restored before entities so the sparse routing is live during entity replay; `sparse_data` restored after entities are created so entity IDs exist. Documented with comments in `marshal.go`.
- **`sparse_test.go`** — 24 test cases covering: basic Set/Get/Remove, multiple entities, no archetype transition, pointer stability across migration, HasID/OwnsID, AddID panic, SetSparse after-use panic, SetSparse on tag panic, entity delete cleanup, ID reuse, deferred path, EachSparse visits all/insertion order, EachSparse on unregistered/non-sparse types, remove from middle (swap-with-last), marshal round-trip, idempotence, bare-tag form, hooks (OnAdd/OnSet/OnRemove), composition with Final.

### Changed

- **Built-in entity reindex** — Sparse inserted at index 34; Wildcard shifts 34→35; Any shifts 35→36; user entities now start at index 37.
- **`HasID` / `OwnsID`** — for non-pair IDs with the Sparse trait, these functions now consult the sparse-set index rather than the entity's archetype type. The entity's archetype does NOT contain Sparse components (consequence of no-archetype-transition semantics).

### Go-flecs divergence from upstream C

In upstream flecs, `EcsSparse` controls storage only; the "no archetype transition" property is separately contributed by `EcsIdDontFragment` (`src/storage/component_index.c:144-180`). Go flecs consolidates both behaviors into a single `Sparse` trait for v0.51.0. When `DontFragment` is ported in a later phase, the behaviors can be split. This consolidation is documented in `docs/ComponentTraits.md § Sparse`.

## v0.50.0 — 2026-05-13 — Phase 15.18: OrderedChildren trait

### Added

- **`SetOrderedChildren(w *World, parent ID)`** — marks `parent` as an ordered-children parent so that `EachChild` (and `Reader.EachChild`) iterates direct children in insertion order regardless of archetype-table reshuffling. Opt-in per parent; calling twice is a no-op. If the parent already has children when the trait is applied, those children are snapshotted in their current archetype order.
- **`IsOrderedChildren(s scope, parent ID) bool`** — reports whether `parent` has been marked with `OrderedChildren`. Accepts `scope` so it works inside both `Read` and `Write` blocks (per Phase 15.8 convention).
- **`w.OrderedChildren() ID`** — returns the built-in `OrderedChildren` trait entity (index 33). Bare-tag form: `fw.AddID(parentID, w.OrderedChildren())` is equivalent to `SetOrderedChildren(w, parentID)`.
- **Insertion-order iteration** — `EachChild` and `Reader.EachChild` snapshot the ordered list at iteration start, so mutations inside the callback (add/remove children) do not affect the current iteration but are visible on the next call. Unordered parents continue to use the existing archetype-derived path.
- **Hook sites** — ordered list is maintained on: child add (`addIDImmediate` and `batchForEntity` deferred path), child remove (`removeIDImmediate`), re-parent (exclusive pair replacement in `addIDImmediate`), and entity delete (`deleteOne` cleans up both the parent map entry and any child appearances).
- **`ordered_children_test.go`** — 18 test cases covering: basic insertion order, trait-absent fallthrough, remove-middle, re-parent both ordered, re-parent src-ordered/dest-unordered, re-parent src-unordered/dest-ordered, delete child, delete parent, deferred add, idempotence/round-trip, `Reader.EachChild` from Read block, stress (100 children with random interleaving), set-after-children-exist, iteration-during-mutation, marshal round-trip, bare-tag form, built-in index, deferred bare-tag.
- **Marshal/unmarshal support** — `MarshalJSON` adds `"ordered_children": true` to any entity in `w.orderedChildren`. `UnmarshalJSON` calls `applyOrderedChildrenPolicy` for such entities before adding children, so the child-add hook restores the list in the correct order.

### Changed

- **Built-in entity reindex** — `OrderedChildren` inserted at index 33; `Wildcard` shifts 33→34; `Any` shifts 34→35; user entities now start at index 36.
- **`Reader.EachChild`** — updated to check the `orderedChildren` map before falling through to the archetype-derived path, matching the `World.EachChild` behavior.

## v0.49.0 — 2026-05-13 — Phase 15.17: With relationship trait

### Added

- **`SetWith(w *World, source ID, coAdd ID)`** — registers `coAdd` as a co-add for `source`. Idempotent. Stored as a `(With, coAdd)` pair on `source`'s archetype; automatic JSON round-trip via existing pair marshalling. No removal API — With is sticky.
- **`HasWith(s scope, source ID) []ID`** — returns all co-add IDs registered on `source` via `SetWith`. Accepts `scope` so it works inside `Read` and `Write` blocks. Returns nil if none registered.
- **`w.With() ID`** — returns the built-in With trait entity (index 32). Bare-tag form: `fw.AddID(source, w.With())` has no meaning on its own; use `SetWith` to register co-adds.
- **Auto-add enforcement — immediate path** — `applyWithCoAdds` fires after every `addIDImmediate` call: scans the source entity's archetype for `(With, *)` pairs and calls `addIDImmediate` for each co-add. Pair form: adding `(R, T)` where R has `(With, S)` co-adds `(S, T)`.
- **Auto-add enforcement — deferred path** — `expandWithIntoScratch` fires in `batchForEntity` during the two-pass coalescer: inserts With co-add IDs into the running sorted signature before the single archetype migration. Diamond dedup: if a co-add is already in the target signature, recursion is skipped.
- **Transitive chaining** — `SetWith(A, B)` + `SetWith(B, C)`: adding A also adds B then C. Both immediate and deferred paths recurse transitively.
- **Cycle detection** — mutual cycles (`SetWith(A,B)` + `SetWith(B,A)`) panic with a message naming the cycle path (e.g. `flecs: With cycle detected: A → B → A`) on both the immediate and deferred paths.
- **`with_test.go`** — 22 test cases at ≥95% coverage on `with.go`: bare add, chained, multiple co-adds, pair form, pair form chained, cycle detection, idempotent, deferred bare add, HasWith round-trip, IsA no-retrigger, Exclusive interaction, one-way remove, deferred pair form, hook ordering, batched deferred (exercises `batchForEntity`/`expandWithIntoScratch`), diamond dedup, deferred cycle (helper path), source with extra tag (covers scanning continue branch), HasWith null entity, dead source immediate/batched.

### Changed

- **Built-in entity reindex** — With inserted at index 32; Wildcard shifts 32→33; Any shifts 33→34; user entities now start at index 35.
- **Bootstrap** — With is bootstrapped at world creation with `applyRelationshipPolicy`. PairIsTag bootstrap note updated (SlotOf/DependsOn/Flag still not ported).

## v0.48.0 — 2026-05-13 — Phase 15.16: PairIsTag relationship trait

### Added

- **`SetPairIsTag(w, relID)`** — marks `relID` as a PairIsTag relationship: all pairs `(relID, T)` are forced to behave as tags; `SetPair[T]` and `SetPairByID` (both immediate and deferred paths) panic with a message naming the relationship and the PairIsTag trait. Idempotent.
- **`IsPairIsTag(s scope, relID ID) bool`** — reports whether `relID` has been marked PairIsTag. Accepts `scope` (per Phase 15.8 convention) so it works inside `Read` and `Write` blocks. Also accepts a pair ID — if `relID.IsPair()`, the relationship side is extracted before the lookup, matching the ergonomic shape of `IsTrait`/`IsRelationship`.
- **`w.PairIsTag() ID`** — returns the built-in PairIsTag trait entity (index 31). Bare-tag form: `fw.AddID(relID, w.PairIsTag())` is equivalent to `SetPairIsTag(w, relID)`.
- **Set-after-use trap** — `SetPairIsTag(R)` checks the component registry for any pair `(R, *)` with non-zero-size TypeInfo. If found, it panics with a message naming both the pair and the data type. This mirrors C `flecs_assert_relation_unused` in `bootstrap.c:270-290`. Bare-tag adds (zero-size TypeInfo) are unaffected.
- **Write-time enforcement** — `checkPairIsTag` fires at three call sites: `setPairImmediate[T]` (id_ops.go), `w.SetPairByID` (value_ops.go), `SetPair[T]` deferred path (scope.go), and `(*Writer).SetPairByID` (scope.go). The deferred path panics at enqueue time so the error surfaces at the user's call site, not at flush.
- **`pairistag_test.go`** — 12 test cases: tag-form add unaffected; `SetPair[T]` panics; `SetPairByID` panics; pre-existing data blocks `SetPairIsTag`; bare-tag dispatch via `AddID`; idempotent round-trip; bootstrap of `IsA`/`ChildOf`; composition with Exclusive; deferred enqueue panic; `RemoveID` still works; `IsPairIsTag` on a pair ID; skip-different-relationship coverage.

### Changed

- **Built-in entity reindex** — PairIsTag inserted at index 31; Wildcard shifts 31→32; Any shifts 32→33; user entities now start at index 34.
- **Built-in bootstrap** — `IsA` and `ChildOf` are bootstrapped PairIsTag at world creation, mirroring C `bootstrap.c:1272-1273`. SlotOf/DependsOn/Flag/With are not yet ported.
- **Divergence from C** — In C `EcsPairIsTag` retroactively sets `type_info = NULL` on all existing `(R, *)` id records when the trait is applied. In Go flecs the set-after-use trap panics instead, keeping storage consistent without a retroactive rewrite.

## v0.47.0 — 2026-05-13 — Phase 15.15: Relationship/Target/Trait usage constraints

### Added

- **`SetRelationship(w, id)`** — marks `id` as a Relationship-constrained entity: it may only appear as the relationship (first element) of a pair. Attempting to add it as a plain tag or as a pair target panics with a message naming the entity and the violated constraint.
- **`SetTarget(w, id)`** — marks `id` as a Target-constrained entity: it may only appear as the target (second element) of a pair.
- **`SetTrait(w, id)`** — marks `id` as a Trait entity, exempting it from `Relationship`'s no-target-slot check. This allows patterns like `(SomeRel, ChildOf)` where a `Relationship`-marked entity appears in the target slot.
- **`IsRelationship(s scope, id ID) bool`** / **`IsTarget(s scope, id ID) bool`** / **`IsTrait(s scope, id ID) bool`** — query functions for each constraint marker; accept `scope` (per Phase 15.8 convention) so they work inside `Read` and `Write` blocks.
- **`w.Relationship() ID`** / **`w.Target() ID`** / **`w.Trait() ID`** — bare-tag accessors for the three built-in constraint entities (indices 28, 29, 30 respectively).
- **Write-time enforcement** — `checkUsageConstraints` fires on both the immediate path (`addIDImmediate`) and the deferred-coalesce path (`batchForEntity`), ensuring panics are consistent regardless of how the add was submitted.
- **`usage_constraints_test.go`** — 15 test cases: Relationship/Target/Trait bare-tag panics; pair slot panics; Trait-exemption success; built-in bootstrap checks (IsA/ChildOf/OnDelete/OnDeleteTarget/OnInstantiate → Relationship; Override/Inherit/DontInherit → Target; IsA/ChildOf → Trait); deferred-path panics at coalesce time; idempotence; composition with Exclusive/Traversable/Transitive; component-on-Target panic; self-pair panic.

### Changed

- **Built-in entity reindex** — Relationship/Target/Trait inserted at indices 28/29/30; Wildcard shifts 28→31; Any shifts 29→32; user entities now start at index 33.
- **Built-in bootstrap** — `IsA`, `ChildOf`, `OnDelete`, `OnDeleteTarget`, `OnInstantiate` are bootstrapped `Relationship`; `Override`, `Inherit`, `DontInherit` are bootstrapped `Target`; `IsA` and `ChildOf` are bootstrapped `Trait`. Note: `RemoveAction`, `DeleteAction`, `PanicAction` are **not** marked Target (matches upstream).
- **Wildcard and Any not constrained** — consistent with the upstream `!ecs_id_is_wildcard(rel)` guard in `component_index.c:396`, Wildcard and Any are not bootstrapped with either marker. Query patterns like `(R, *)` continue to work without special-casing.

## v0.46.0 — 2026-05-13 — Phase 15.14: Traversable relationship trait

### Added

- **`SetTraversable(w, relID)`** — marks `relID` as a traversable relationship, permitting its use in query terms with `.Up(rel)`, `.SelfUp(rel)`, or `.Cascade(rel)` modifiers. Also marks `relID` as Acyclic (Traversable implies Acyclic, mirroring C `bootstrap.c:1295-1296`).
- **`IsTraversable(s scope, relID ID) bool`** — reports whether `relID` has been marked Traversable. Accepts `scope` (per Phase 15.8 convention) so it works inside `Read` and `Write` blocks.
- **`w.Traversable() ID`** — returns the built-in Traversable trait entity (index 27). Bare-tag form: `fw.AddID(relID, w.Traversable())` is equivalent to `SetTraversable(w, relID)`.
- **Query-time enforcement** — `validateAndSortTerms` (shared by `NewQueryFromTerms` and `NewCachedQueryFromTerms`) now validates that any term with a non-zero `Trav` uses a relationship registered as Traversable. Non-compliant terms panic with a message naming both the traversal modifier (`.Up()`, `.SelfUp()`, or `.Cascade()`) and the relationship. Mirrors C `query/validator.c:639-647`.
- **`traversable_test.go`** — 12 test cases: SetTraversable+Up succeeds; non-traversable Up/SelfUp/Cascade panics each naming the correct modifier; IsTraversable bootstrap for IsA+ChildOf; vanilla entity returns false; Traversable implies IsAcyclic; idempotence and bare-tag equivalence; deferred path via Write block; pair-form Trav panics (pairs not valid traversal relationships); SetTransitive alone does not imply Traversable in this phase; TraverseSelf guard skips check; scope in Write/Read blocks.

### Changed

- **`ChildOf` and `IsA` bootstrapped Traversable** — both relationships now register as Traversable at world creation, mirroring C `bootstrap.c:1063,1315-1316`. Existing queries that traverse `ChildOf` or `IsA` continue to work without change.
- **`IsA` is now Acyclic (behavior change)** — as a side effect of being bootstrapped Traversable, `IsA` is also now Acyclic for the first time in Go flecs. Write-time cycle rejection (`checkAcyclic`) now also applies to `(IsA, *)` pairs. Previously, IsA cycles were caught only at traversal time by `walkUp`'s seen-map guard. Code that deliberately created IsA cycles (e.g. `TestIsATwoEntityCycleTerminates`) now panics at the cycle-creating `AddID` call.
- **Built-in entity reindex** — Traversable inserted at index 27; Wildcard shifts 27→28; Any shifts 28→29; user entities now start at index 30.
- **`Transitive → Traversable` implication deferred** — C `bootstrap.c:1299` has `(EcsTransitive, EcsWith, EcsTraversable)`, making all transitive relationships also traversable (and therefore acyclic). Go flecs defers this implication to a follow-up phase to preserve the existing cycle-safety behavior in `transitiveWalk`. Users who need to traverse a transitive relationship with `.Up(R)` must call `SetTraversable(w, R)` explicitly.
- **Pair-form traversal** — `term.Trav` must be a single entity ID. Passing a pair ID (e.g. `MakePair(R, w.Wildcard())`) to `.Up()` will always fail the Traversable check because the check operates on `t.Trav.Index()`, which for a pair encodes the target's index, not the relationship's. This matches C's convention that `term->trav` is always a single entity.

## v0.45.0 — 2026-05-13 — Phase 15.13: WriteOnce component trait

### Added

- **`SetWriteOnce(w, componentID)`** — marks `componentID` as WriteOnce: after the first value write (`Set`), any subsequent `Set` on the same `(entity, component)` pair panics with a message naming both the entity and the component. No upstream C counterpart — this is a Go-flecs-only ergonomic trait.
- **`IsWriteOnce(s scope, componentID ID) bool`** — reports whether `componentID` has been marked WriteOnce. Accepts `scope` (per Phase 15.8 convention) so it works inside `Read` and `Write` blocks. Returns `false` without panic for non-component entities.
- **`w.WriteOnce() ID`** — returns the built-in WriteOnce trait entity (index 26). Bare-tag form: `fw.AddID(componentID, w.WriteOnce())` is equivalent to `SetWriteOnce(w, componentID)`.
- **Write-time enforcement** — `setImmediateByPtr` (immediate path) and the `cmdModified` dispatch case (coalesced-deferred path) both enforce the WriteOnce constraint. The first Set records `hasBeenSet`; subsequent Sets panic.
- **`Add` is not a write** — `addIDImmediate` slots the component with a zero value; this does not count as the first write. `Add → Set` is allowed; a second `Set` after that panics.
- **Pair-form rule** — `WriteOnce` on relationship `R` governs every pair `(R, T)`. Each `(entity, (R, T))` slot is tracked independently. `WriteOnce` on a target `T` does not propagate.
- **Non-component-target panic** — `SetWriteOnce(w, e)` panics at application time if `e` is not a registered component entity.
- **Slot lifecycle** — `Remove` clears the per-(entity, component) `hasBeenSet` tracking so a fresh `Add + Set` cycle starts over. Entity deletion (`deleteOne`) clears all WriteOnce slots for the deleted entity.
- **`writeonce_test.go`** — 11 test cases: first Set succeeds; second Set panics naming entity and component; Add→Set→Set panics (Add is not a trigger); Remove clears tracking; deferred-coalesced path panics on second queued Set; pair-form with independent (R, T1) and (R, T2) slots; non-component target panics at trait-application time; `IsWriteOnce` round-trip and idempotent `SetWriteOnce`; marker entity is not a component; remove without prior Set (nil-map early return); bare-tag form equivalent.

### Changed

- **Built-in entity reindex** — WriteOnce inserted at index 26; Wildcard shifts 26→27; Any shifts 27→28; user entities now start at index 29.
- **Renamed from `Constant`** — previously tracked as `Constant` in the Phase 14.8 gap analysis. Renamed to `WriteOnce` to avoid a future collision with upstream `EcsConstant`, which is an enum-value tag applied to enum/bitmask constant entities in the meta addon (`include/flecs/flecs.h:2014`). `WriteOnce` is also more precise: `ReadOnly` is overloaded with thread-safety read-view semantics; `Immutable` would imply removes are blocked too.

## v0.44.0 — 2026-05-12 — Phase 15.12: Singleton component trait

### Added

- **`SetSingleton(w, componentID)`** — marks `componentID` as a singleton: at most one entity may hold it at any time. Mirrors C `EcsSingleton` (a trait entity, index 25) but with deliberately different semantics (see below).
- **`IsSingleton(s scope, componentID ID) bool`** — reports whether `componentID` has been marked Singleton. Accepts `scope` (per Phase 15.8 convention) so it works inside `Read` and `Write` blocks.
- **`SingletonEntity(s scope, componentID ID) (ID, bool)`** — returns the entity currently holding `componentID` as a singleton, plus true. Returns `(0, false)` if no entity holds it.
- **`w.Singleton() ID`** — returns the built-in Singleton trait entity (index 25). Bare-tag form: `fw.AddID(componentID, w.Singleton())` is equivalent to `SetSingleton(w, componentID)`.
- **`Singleton[T any](s scope) (*T, bool)`** — typed read accessor: registers `T` if needed, looks up the singleton holder, returns a pointer into the holding entity's component column. Returns `(nil, false)` if not a singleton or no holder.
- **`WriteSingleton[T any](fw *Writer, e ID, v T)`** — typed write accessor: ensures `T` is marked singleton (idempotent), then calls `Set[T](fw, e, v)`. Panics if a different entity already holds `T`.
- **Write-time enforcement** — `addIDImmediate` and the coalesced deferred path both check the singleton instance map before migrating. Panics with `"cannot add singleton component <name> to entity <e>: already held by entity <existing>"`, naming both entities.
- **Slot lifecycle** — the singleton slot is released when the component is removed via `Remove[T]` or `RemoveID` (both immediate and coalesced deferred paths), or when the holding entity is deleted (`deleteOne` scans `singletonInstances`).
- **`singleton_test.go`** — 9+ test cases: default no-constraint, second-holder panic (immediate), slot released on remove, `IsSingleton` round-trip, `SingletonEntity` lifecycle, entity delete clears slot, Singleton+Exclusive composition, typed accessors + deferred smoke test, multiple types coexist, pair-form singleton, coalesced deferred paths.

### Changed

- Built-in entity count increases from 26 to 27. User entities now start at index 28.
- Singleton is at index 25; Wildcard moves to index 26; Any moves to index 27.
- `marshal.go` skip-set updated to exclude Singleton (25) from JSON serialization.
- `TestIsAWorldCountBaseline`, `nonDataEntities` in `marshal_test.go`, and `builtinEntityCount` in `meta_test.go` updated to reflect the new count.

### Breaking changes

- Built-in entity count increases from 26 to 27. If your code hardcodes the built-in entity count (e.g., in marshal skip-sets or test baselines), update to 27. User entities now start at index 28.

### Deliberate divergence from C flecs

**Go semantic differs from C `EcsSingleton`:**

| Dimension | C `EcsSingleton` | Go `Singleton` (v0.44.0) |
|---|---|---|
| Enforcement predicate | `component == e` (must-be-self: only the component entity itself may hold it) | `at most one entity` (any entity, first wins) |
| Enforcement scope | Debug builds only (`#ifdef FLECS_DEBUG` in `bootstrap.c:396-427`) | Always on |
| Instance tracking | None needed (component IS the entity) | `singletonInstances map[ID]ID` per world |
| Query integration | Queries auto-target the component entity | No query integration in v0.44.0 |

**Migration from the v0.43.0 workaround:** If you were using `RegisterComponent[T] + entity ID` as a manual singleton, no migration is required — it continues to work. To adopt the first-class API: call `SetSingleton(w, compID)` to enforce the at-most-one constraint going forward.

### Non-goals (explicitly out of scope for v0.44.0)

- No singleton-as-tag (only data-bearing components are meaningful as singletons).
- No automatic creation of the holding entity — the caller creates it explicitly.
- No serialization of `singletonInstances` runtime state in v1 marshal. The singleton policy on the component entity round-trips automatically (it's stored as a pair in the entity graph); the holding entity's component data round-trips as normal entity data.
- No query integration (fixed-source query terms for singletons).

---

## v0.43.0 — 2026-05-12 — Phase 15.11: OneOf relationship trait

### Added

- **`SetOneOf(w, relID, parentID)`** — constrains `relID`'s targets to direct children of `parentID`. Passing `parentID == relID` encodes the self-tag form (target must be a direct child of the relationship entity itself). Mirrors C `EcsOneOf` (a trait entity, index 24).
- **`IsOneOf(s scope, relID ID) (parent ID, ok bool)`** — reports whether `relID` has a OneOf constraint and returns the required parent. Accepts the `scope` interface (per Phase 15.8 convention) so it works inside both `Read` and `Write` blocks without `AsReader()`.
- **`w.OneOf() ID`** — returns the built-in OneOf trait entity (index 24). Two bare forms: `fw.AddID(relID, w.OneOf())` (self-tag — equivalent to `SetOneOf(w, relID, relID)`) and `fw.AddID(relID, MakePair(w.OneOf(), parentID))` (pair form — equivalent to `SetOneOf(w, relID, parentID)`).
- **Write-time enforcement** — `addIDImmediate` checks for a OneOf constraint before storing any `(R, target)` pair. Fires on both the immediate path and the deferred path (when the `Write` scope flushes). The check is a direct `(ChildOf, parent)` lookup on `target` — no transitive ancestor traversal. Wildcard and Any targets are exempt.
- **Composes with Exclusive** — when a relationship has both `OneOf` and `Exclusive` traits, the OneOf check fires before the Exclusive atomic migration so that replacement targets are validated before the swap.
- **`oneof_test.go`** — 8 test cases: default no-constraint, pair-form valid target succeeds, pair-form invalid target panics, self-tag form valid target succeeds, `IsOneOf` round-trip, bootstrapped relationships have no OneOf constraint, Exclusive+OneOf atomic replacement, bare-tag AddID equivalence.

### Changed

- Built-in entity count increases from 25 to 26. User entities now start at index 27.
- OneOf is at index 24; Wildcard moves to index 25; Any moves to index 26.
- `marshal.go` skip-set updated to exclude OneOf (24) from JSON serialization.
- `TestIsAWorldCountBaseline`, `nonDataEntities` in `marshal_test.go`, and `builtinEntityCount` in `meta_test.go` updated to reflect the new count.

### Breaking changes

- Built-in entity count increases from 25 to 26. If your code hardcodes the built-in entity count (e.g., in marshal skip-sets or test baselines), update to 26. User entities now start at index 27.

### Deliberate divergence from C flecs

The check fires only at write time (`AddID`); C also enforces OneOf at query plan construction time. Write-time enforcement gives clear early-error semantics consistent with Acyclic (Phase 15.9) and Final (Phase 15.10).

### Non-goals (explicitly out of scope for v0.43.0)

- No removal API for OneOf constraints (`SetOneOf` is one-shot; consistent with other trait setters).
- No automatic parent-entity creation (the user must create the parent and its children).
- No transitive ancestor check (direct `(ChildOf, parent)` only, matching C `ecs_has_pair` semantics).

---

## v0.42.0 — 2026-05-12 — Phase 15.10: Final entity trait

### Added

- **`SetFinal(w, entityID)`** — marks an entity as Final: any subsequent `AddID(src, MakePair(IsA, entityID))` panics with `"cannot add (IsA, <id>): <id> has the Final trait"`. Mirrors C `EcsFinal` (a tag entity, index 23).
- **`IsFinal(s scope, entityID ID) bool`** — reports whether an entity is marked Final. Accepts the `scope` interface (per Phase 15.8 convention) so it works inside both `Read` and `Write` blocks without `AsReader()`.
- **`w.Final() ID`** — returns the built-in Final trait entity (index 23). The bare-tag form `fw.AddID(entityID, w.Final())` is equivalent to `SetFinal(w, entityID)`.
- **Write-time enforcement** — `addIDImmediate` checks for Final before storing an `(IsA, target)` pair. The check fires on both the immediate path and the deferred path (when the `Write` scope flushes). Self-pairs (`AddID(e, MakePair(IsA, e))` where `e` is Final) are also rejected, matching C's unconditional `ecs_has_id` check in `component_index.c:447-453`.
- **`final_test.go`** — 8 test cases: default allows IsA, immediate path panics, non-IsA pairs to Final entity allowed, non-Final target allowed, IsFinal round-trip via *Reader and *Writer, Final+Reflexive composition (no spurious panic), self-IsA-add to Final entity panics, deferred path panics on flush.

### Changed

- Built-in entity count increases from 24 to 25. User entities now start at index 26.
- Final is at index 23; Wildcard moves to index 24; Any moves to index 25.
- `marshal.go` skip-set updated to exclude Final (23) from JSON serialization.
- `TestIsAWorldCountBaseline`, `nonDataEntities` in `marshal_test.go`, and `builtinEntityCount` in `meta_test.go` updated to reflect the new count.

### Breaking changes

- Built-in entity count increases from 24 to 25. If your code hardcodes the built-in entity count (e.g., in marshal skip-sets or test baselines), update to 25. User entities now start at index 26.

### Deliberate divergence from C flecs

C's query engine uses `EcsFinal` to suppress IsA-substitution in the query validator (`query/validator.c:849`). The Go port enforces Final only at write time for v0.42.0; query-side optimization is out of scope. Write-time enforcement gives clear early-error semantics and matches the pattern established by Acyclic (Phase 15.9).

### Non-goals (explicitly out of scope for v0.42.0)

- No retroactive enforcement (existing `(IsA, X)` edges where `X` is later marked Final are not removed).
- No automatic Final propagation along IsA chains.
- No query-side optimization using Final (C uses it in `query/validator.c:849` to suppress IsA-substitution).

---

## v0.41.0 — 2026-05-12 — Phase 15.9: Acyclic relationship trait

### Added

- **`SetAcyclic(w, relID)`** — marks a relationship as acyclic: adding a pair `(e, R, target)` panics if `target` can transitively reach `e` via `R`. Mirrors C `EcsAcyclic` (a tag entity, index 22).
- **`IsAcyclic(w, relID) bool`** — reports whether a relationship is marked acyclic.
- **`w.Acyclic() ID`** — returns the built-in Acyclic trait entity (index 22). The bare-tag form `fw.AddID(relID, w.Acyclic())` is equivalent to `SetAcyclic(w, relID)`.
- **Write-time cycle rejection** — `addIDImmediate` and the deferred `batchForEntity` path both check for cycles before storing a pair when the relationship is acyclic. The check uses `walkUp` from `traversal.go`, which has its own depth limit and seen-map so that pre-existing malformed data cannot cause an infinite walk.
- **Self-pair allowed** — `(e, R, e)` is explicitly permitted; Acyclic does not reject self-pairs. Combine with Reflexive if implicit self-pair truth is desired.
- **ChildOf bootstrapped as acyclic** — matching C `src/bootstrap.c:1011`, `ChildOf` gains the Acyclic policy at world construction. Mutual parent/child cycles now panic at `AddID` time, preventing infinite recursion in `EachChild` and related hierarchy traversals.
- **`acyclic_test.go`** — 9 test cases: non-acyclic allows cycles, direct cycle rejected, transitive cycle rejected, self-pair allowed, ChildOf bootstrap regression, IsAcyclic round-trip, Acyclic+Transitive composition, Acyclic+Symmetric edge case, bare-tag form.

### Changed

- Built-in entity count increases from 23 to 24. User entities now start at index 25.
- Acyclic is at index 22; Wildcard moves to index 23; Any moves to index 24.
- `marshal.go` skip-set updated to exclude Acyclic (22) from JSON serialization.
- `TestMarshalCycleDetection` updated: the test now verifies the write-time panic rather than a MarshalJSON error, since ChildOf cycles can no longer be stored.

### Breaking changes

- Built-in entity count increases from 23 to 24. If your code hardcodes the built-in entity count (e.g., in marshal skip-sets or test baselines), update to 24. User entities now start at index 25.
- **ChildOf is now acyclic.** Code that deliberately constructs circular parent hierarchies (e.g., `a ChildOf b` and `b ChildOf a`) will now panic at the second `AddID`. This was always undefined behavior; the new behavior makes it explicit.

### Deliberate divergence from C flecs

C flecs guards Acyclic cycles at lookup/traversal time via `ECS_MAX_RECURSION` depth caps in `flecs_get_base_component` (entity.c:75) and related functions. The Go port enforces at `AddID` time so that `EachChild` and similar recursors never encounter an infinite chain. The tradeoff is a per-add O(chain length) check on acyclic relationships; for typical ChildOf trees this is negligible. This divergence is analogous to Phase 15.7's `HasID` self-pair extension — both go further than C to provide clearer early-error semantics.

### Non-goals (explicitly out of scope for v0.41.0)

- No retroactive cycle detection on existing data (only new adds are checked).
- No automatic cycle breaking — just rejection by panic.
- No Acyclic bootstrap on IsA — C does not bootstrap it, and IsA's separate recursion guard is independent.
- No performance optimization for deep chains (correctness first).

---

## v0.40.0 — 2026-05-12 — Phase 15.8: scope interface — Writer ⊇ Reader at free-function boundaries

### Added

- **Unexported `scope` interface** — defined in `scope.go`. Both `*Reader` and `*Writer` satisfy `scope` via a single unexported `scopeWorld() *World` method. Users never name the interface; they pass `*Reader` or `*Writer` as before.

### Changed

- **All read free-functions now accept `scope` instead of `*Reader`** — `Get[T]`, `GetRef[T]`, `Has[T]`, `Owns[T]`, `GetPair[T]`, `GetPairRef[T]`, `HasID`, `OwnsID`, `Each1`, `Each2`, `Each3`, `Each4`, `GetUp[T]`, `HasUp`, `TargetUp`, `PrefabOf` (all in `scope.go`), plus `IsEnabledID` and `IsEnabled[T]` in `cantoggle.go`. Callers that passed `*Reader` continue to compile unchanged; callers inside a `Write` scope may now pass `fw` directly.

### Breaking changes

- **`(*Writer).AsReader()` removed** — previously provided a `*Reader` downgrade for passing to read free-functions from inside a `Write` scope. No longer needed: pass the `*Writer` directly. Follow the mechanical recipe: `fw.AsReader()` → `fw`. Pre-1.0; no migration-guide complexity.

### Non-goals (explicitly out of scope for v0.40.0)

- No change to `*Reader` / `*Writer` struct shape or scope semantics.
- No new public types or methods beyond the interface (which is unexported).
- No change to `*QueryIter` — it is its own kind of scope accessed via `it.Reader()` / `it.Writer()`.

---

## v0.39.0 — 2026-05-12 — Phase 15.7: Reflexive relationship trait

### Added

- **`SetReflexive(w, relID)`** — marks a relationship as reflexive: `R(X, X)` is implicitly true for all entities `X`, without storing an explicit self-pair. Mirrors C `EcsReflexive` (a tag entity, index 21).
- **`IsReflexive(w, relID) bool`** — reports whether a relationship is marked reflexive.
- **`w.Reflexive() ID`** — returns the built-in Reflexive trait entity (index 21). The bare-tag form `fw.AddID(relID, w.Reflexive())` is equivalent to `SetReflexive(w, relID)`.
- **`HasID` self-pair extension** — `HasID(e, MakePair(R, e))` now returns `true` when `R` is reflexive, even if no self-pair is stored. The check is gated on `target == entity` before the policy-map lookup, so non-self queries pay zero extra cost. **Deliberate divergence from C:** in C flecs `ecs_has_id` does not consult `EcsReflexive`; it is purely a query-time trait. Go flecs extends `HasID` to match the semantic promise already documented in `docs/Relationships.md` and `docs/ComponentTraits.md`.
- **Query self-match** — in both `NewQueryFromTerms` and `NewCachedQueryFromTerms`, a term `With(MakePair(R, target))` where `R` is reflexive additionally matches the table that contains `target` itself, in addition to tables that directly hold `(R, target)`.
- **Reflexive + Transitive composition** — when a relationship has both traits, the query match is: direct `(R, target)` pair **or** transitive chain to `target` **or** self-match (target's own table). `IsA` composes both traits.
- **IsA bootstrapped as reflexive** — matching C `src/bootstrap.c:1321`, `IsA` gains the Reflexive policy at world construction. `HasID(a, MakePair(IsA, a))` now returns `true` for any alive entity `a` without storing a self-pair.
- **`reflexive_test.go`** — 9 test cases: default no-self-pair, SetReflexive + HasID, non-self HasID unchanged, query self-match, Reflexive+Transitive composition, IsA bootstrap, IsReflexive round-trip, non-relationship entity lenient, cached query self-match.

### Changed

- Built-in entity count increases from 22 to 23. User entities now start at index 24.
- Wildcard is now at index 22; Any is at index 23 (each bumped by one to make room for Reflexive at index 21).
- `marshal.go` skip-set updated to exclude Reflexive (21) from JSON serialization.

### Breaking changes

- Built-in entity count increases from 22 to 23. If your code hardcodes the built-in entity count (e.g., in marshal skip-sets or test baselines), update to 23. User entities now start at index 24.
- `IsA` is now reflexive: `HasID(a, MakePair(IsA, a))` returns `true` for any alive `a`. If existing code expected this to return `false`, update it.

### Non-goals (explicitly out of scope for v0.39.0)

- No `EcsTermReflexive` internal flag porting (a C query-compiler detail).
- No "Reflexive implies Transitive" or other cross-trait propagation.
- No automatic pair storage for self-pairs (Reflexive is a lazy, check-at-query-time trait).
- No entity-migration cache invalidation for `CachedQuery` (staleness is accepted and documented).

---

## v0.38.0 — 2026-05-12 — Phase 15.6: Wildcard and Any query-term sentinels

### Added

- **`w.Wildcard() ID`** — returns the built-in Wildcard sentinel (index 21, `*`). Use in a pair term's target or relationship slot to match every concrete value: `With(MakePair(likesID, w.Wildcard()))` yields one iterator row per concrete `(Likes, X)` pair in each matched table.
- **`w.Any() ID`** — returns the built-in Any sentinel (index 22, `_`). Like Wildcard but short-circuits after the first match: exactly one row per entity regardless of how many concrete targets exist. Mirrors C `EcsQueryAndAny` semantics.
- **`flecs.MatchedTarget(it, termIdx) ID`** — concrete target entity for the wildcard term's current expansion row.
- **`flecs.MatchedID(it, termIdx) ID`** — full pair ID `(rel, target)` matched on the current row.
- **`flecs.FieldByMatch[T](it, termIdx) []T`** — typed column slice for the concrete pair matched by the wildcard term; handles both value pairs and tag pairs.
- Wildcard and Any work in both `NewQueryFromTerms` and `NewCachedQueryFromTerms`. Cache invalidation is automatic: when a new table with matching concrete pairs is created, the cached query picks it up via `notifyTableCreated`.
- **`wildcard_test.go`** — 9 test cases: wildcard target, wildcard relationship, both-wildcard, Any target, `MatchedTarget`, `MatchedID`, mixed terms, cached query interaction, `FieldByMatch`. Plus `BenchmarkWildcardQuery_PairsPerEntity`.

### Changed

- Built-in entity count increases from 20 to 22. User entities now start at index 23.
- `marshal.go` skip-set updated to exclude Wildcard (21) and Any (22) from JSON serialization.

### Breaking changes

- Built-in entity count increases from 20 to 22. If your code hardcodes the built-in entity count (e.g., in marshal skip-sets or test baselines), update to 22. User entities now start at index 23.
- `World.Wildcard()` and `World.Any()` are now valid built-in entity accessors. If you had user entities starting at index 21 or 22, they now start at index 23.

### Non-goals (explicitly out of scope for v0.38.0)

- No Wildcard/Any in single-component (non-pair) terms.
- No Wildcard/Any in component-set position (only target/relationship slots of pair terms).
- No `EcsThis` variable binding or cross-term entity-ID joins.
- No C-style component-index `cr_wildcard`/`cr_any` buckets (initial implementation scans the table type directly).

---

## v0.37.0 — 2026-05-12 — Phase 15.5: Transitive relationship trait

### Added

- **`SetTransitive(w, relID)`** — marks a relationship as transitive: when evaluating a query term `(R, C)`, entities that hold `(R, B)` are also matched if `B` (or any entity reachable from `B` via further `R` chains) holds `(R, C)`. Formally: `aRb ∧ bRc ⇒ aRc`.
- **`IsTransitive(w, relID) bool`** — reports whether a relationship is marked transitive.
- **`w.Transitive() ID`** — returns the built-in Transitive trait entity (index 20). The bare-tag form `fw.AddID(relID, w.Transitive())` is equivalent to `SetTransitive(w, relID)`.
- **Lazy walk at query time:** chaining walks the `(R, *)` graph only when a query term is evaluated. No pairs are written eagerly; avoids O(n²) writes for long chains. Compare to Symmetric (v0.36.0) which mirrors eagerly at write time.
- **Cycle detection:** a visited set prevents infinite loops on cyclic pair graphs; both `NewQueryFromTerms` and `NewCachedQueryFromTerms` are safe.
- **Depth limit:** bounded at 64 hops (`maxTraversalDepth`); chains deeper than the limit are silently truncated — no panic occurs.
- **Cached query staleness model:** `CachedQuery` pre-evaluates transitive chains at construction and on new-table creation. It does NOT re-evaluate on pair mutation. Staleness is documented; pair-mutation invalidation is a future enhancement.
- **`transitive.go`** — new file parallel to `symmetric.go`, `exclusive.go`, and `cantoggle.go`.
- **`transitive_test.go`** — 10 test cases: default no-chain, simple chain, longer chain, branching chain, cycle safety, cache interaction, depth limit, `IsTransitive` round-trip, Transitive+Symmetric composition, cycle-dead guard. Plus `BenchmarkTransitiveQuery_ChainLen10`.

### Documentation

- `docs/ComponentTraits.md` — Transitive section rewritten with shipped Go API and worked `LocatedIn` example; roadmap row updated to `✅ shipped (v0.37.0)`.
- `docs/Relationships.md` — "Not yet ported" callouts at IsA transitivity section and Transitive section replaced with shipped API documentation and a `LocatedIn` worked example.
- `docs/Queries.md` — new *Transitive Pair Matching* section with brief usage example and forward-links.
- `docs/README.md` — Transitive entries in the feature-gap list updated to shipped status.
- `ROADMAP.md` — Phase 15.5 marked shipped; built-in entity count note updated to 20; user entities now start at index 21.

### Migration Guide

- Built-in entity count increases from 19 to 20. If your code hardcodes the built-in entity count (e.g., in marshal skip-sets or test baselines), update to include `Transitive` (20).
- `World.Transitive()` is now a valid built-in entity accessor. If you had user entities starting at index 20, they now start at index 21.

### Non-goals (explicitly out of scope for v0.37.0)

- No Reflexive trait (`EcsReflexive`) — separate future phase.
- No eager transitive expansion at write time — intentionally lazy.
- No automatic re-evaluation of cached queries on pair-mutation — accept staleness.
- No Wildcard query terms — will compose with Transitive when Wildcard lands.
- No general Traversable trait — C auto-adds `(EcsTransitive, EcsWith, EcsTraversable)` at bootstrap; Go has no Traversable yet.

## v0.36.0 — 2026-05-12 — Phase 15.4: Symmetric relationship trait

### Added

- **`SetSymmetric(w, relID)`** — marks a relationship as symmetric: adding `(R, B)` to entity `A` automatically adds `(R, A)` to entity `B`; removing `(R, B)` from `A` removes `(R, A)` from `B`.
- **`IsSymmetric(w, relID) bool`** — reports whether a relationship is marked symmetric.
- **`w.Symmetric() ID`** — returns the built-in Symmetric trait entity (index 19). The bare-tag form `fw.AddID(relID, w.Symmetric())` is equivalent to `SetSymmetric(w, relID)`.
- **Loop guard:** implemented via the existing `HasComponent` early-return in `addIDImmediate` / `removeIDImmediate`. Adding `(R, B)` to `A` mirrors `(R, A)` to `B`; the mirror tries to add `(R, B)` back to `A`, but `HasComponent` returns true, so the recursion terminates in one extra hop. Identical logic for removal.
- **Self-pair handling:** `AddID(a, MakePair(R, a))` results in a single pair; no duplication. The `HasComponent` guard handles this naturally.
- **Interaction with Exclusive (v0.34.0):** when both traits are active on `R`, each side's exclusivity is enforced independently. Replacing `(R, X)` with `(R, B)` on `A` mirrors `(R, A)` to `B`; if `B` held a conflicting `(R, Y)`, the exclusive constraint replaces it with `(R, A)` on `B` as well.
- **`symmetric.go`** — new file parallel to `exclusive.go` and `cantoggle.go`.
- **`symmetric_test.go`** — 12 test cases covering: non-Symmetric no-mirror, mark+add mirrors, idempotent add, remove mirrors, self-relationship, exclusive interaction, IsSymmetric round-trip, loop guard correctness, batched-add via batchForEntity, batched-remove via batchForEntity, exclusive-mirror-replaces-target (covers addIDImmediate exclusive+symmetric branch), OnAdd/OnRemove hooks fire on both sides.

### Documentation

- `docs/ComponentTraits.md` — Symmetric section rewritten with shipped Go API and worked example; roadmap row updated to `✅ shipped (v0.36.0)`.
- `docs/Relationships.md` — Symmetric section replaced "Not yet ported" callout with shipped API documentation and worked example.
- `docs/README.md` — Symmetric entries in the feature-gap list updated to shipped status.
- `ROADMAP.md` — Phase 15.4 marked shipped; built-in entity count note updated to 19; user entities now start at index 20.

### Migration Guide

- Built-in entity count increases from 18 to 19. If your code hardcodes the built-in entity count (e.g., in marshal skip-sets or test baselines), update to include `Symmetric` (19).
- `World.Symmetric()` is now a valid built-in entity accessor. If you had user entities starting at index 19, they now start at index 20.

### Non-goals (explicitly out of scope for v0.36.0)

- No Transitive trait (`EcsTransitive`) — separate future phase.
- No Reflexive trait (`EcsReflexive`) — separate future phase.
- No `UnsetSymmetric` / unmarking — same precedent as Exclusive/CanToggle (one-way trait marking).
- No wildcard symmetric — adding `(R, *)` does not mirror; only concrete pair targets.

## v0.35.0 — 2026-05-12 — Phase 15.3: CanToggle component trait

### Added

- **`SetCanToggle(w, componentID)`** — marks a component as toggleable: individual entities can have the component temporarily disabled without removing it or migrating to a different archetype table.
- **`IsCanToggle(w, componentID) bool`** — reports whether a component is marked CanToggle.
- **`w.CanToggle() ID`** — returns the built-in CanToggle trait entity (index 18). The bare-tag form `fw.AddID(componentID, w.CanToggle())` is equivalent to `SetCanToggle(w, componentID)`.
- **`EnableID(fw, e, componentID)`** / **`DisableID(fw, e, componentID)`** — set the enabled bit for a specific entity+component pair. Panics if the component is not marked CanToggle or the entity does not own the component.
- **`IsEnabledID(r, e, componentID) bool`** — reads the enabled bit; returns `true` when no bitset entry exists (all-enabled default) and `false` when the entity does not own the component.
- **Typed generics**: `Enable[T](fw, e)` / `Disable[T](fw, e)` / `IsEnabled[T](r, e) bool` — resolve the component ID via the registry and delegate to the ID-based variants.
- **Per-row bitset storage** on `table.Table` — lazy `map[ID][]uint64` allocated on the first `DisableRow` call; default (no entry) means all rows enabled. `Append` grows existing bitsets (new row = enabled); `RemoveSwap` swaps bits and shrinks. The disabled state survives archetype migration (`migrate` transfers toggle bits for shared components).
- **Query filter integration**: `Each1`, `Each2`, `Each3`, `Each4` check the bitset for CanToggle components and skip rows where any queried component is disabled. Components without the CanToggle policy bypass the check entirely (zero overhead for non-toggle queries).
- **`cantoggle.go`** — new file parallel to `exclusive.go` and `cleanup.go`.
- **`cantoggle_test.go`** — 13 test cases covering: non-CanToggle panic (EnableID and DisableID), mark+disable+enable round-trip, `Each1` skips disabled rows, re-enable restores visibility, independent per-component tracking, table migration preserves disable state, change-count bump on toggle, multi-entity table with mixed enabled/disabled, `Each2` filter, typed generic API, error paths.

### Documentation

- `docs/ComponentTraits.md` — CanToggle section rewritten with shipped Go API and worked example; roadmap row updated to `✅ shipped (v0.35.0)`.
- `docs/EntitiesComponents.md` — Component Disabling section replaced "Not yet ported" callout with shipped API and worked example; Component Traits note updated.
- `docs/Queries.md` — new "Disabled rows (CanToggle)" section explaining automatic row-skip in `Each1`/`Each2`/etc.
- `docs/README.md` — CanToggle entry in the feature-gap list updated to shipped status.
- `ROADMAP.md` — Phase 15.3 marked shipped; built-in entity count note updated to 18.

### Migration Guide

- Built-in entity count increases from 17 to 18. If your code hardcodes the built-in entity count (e.g., in marshal skip-sets or test baselines), update to include `CanToggle` (18).
- `World.CanToggle()` is now a valid built-in entity accessor. If you had user entities starting at index 18, they now start at index 19.

### Non-goals (explicitly out of scope for v0.35.0)

- No entity-level disabling (`EcsDisabled` tag); this is component-level only.
- No bulk enable/disable across many entities.
- No deferred toggle commands; `EnableID`/`DisableID` operate immediately even inside a `Write` scope.

## v0.34.0 — 2026-05-12 — Phase 15.2: Exclusive relationship trait

### Added

- **`SetExclusive(w, relID)`** — marks a relationship as exclusive: at most one target per source entity is allowed. Adding a second target automatically replaces the first via a single archetype migration, firing `OnRemove` for the old pair and `OnAdd` for the new pair through the standard hook/observer machinery.
- **`IsExclusive(w, relID) bool`** — reports whether a relationship is marked exclusive.
- **`w.Exclusive() ID`** — returns the built-in Exclusive trait entity (index 17). The bare-tag form `fw.AddID(relID, w.Exclusive())` is equivalent to `SetExclusive(w, relID)`.
- **Exclusive bootstrap**: the built-in relationships `ChildOf`, `OnDelete`, `OnDeleteTarget`, and `OnInstantiate` are marked exclusive in `World.New()`. `IsA` is intentionally NOT exclusive — multiple prefab bases per entity are permitted.
- **ChildOf single-parent fix**: `ChildOf` is now enforced exclusive. `ParentOf` always returns the sole parent; the prior "multiple parents allowed but unusual" caveat is removed.
- **`exclusive.go`** — new file parallel to `cleanup.go` and `instantiate_policies.go`.
- **`exclusive_test.go`** — 9 test cases: non-exclusive allows multiple targets, replace-on-add with hook verification, replace-on-add in deferred batch (net result only), re-add same target is no-op, ChildOf exclusive after bootstrap, IsA NOT exclusive, IsExclusive round-trip, exclusive+cleanup interaction (no cascade delete on pair replace), bare-tag form sets flag.

### Documentation

- `docs/Relationships.md` — Exclusive section replaced "Not yet ported" callout with shipped API and worked example.
- `docs/ComponentTraits.md` — Exclusive section rewritten with Go API; roadmap table row updated to `✅ shipped (v0.34.0)`.
- `docs/README.md` — Exclusive entries in the feature-gap lists updated to shipped status.
- `ROADMAP.md` — Exclusive added to the Shipped section; built-in entity count note updated to 17.

### Migration Guide

- `ChildOf` is now exclusive by default. If any existing code added two `(ChildOf, *)` pairs to the same entity (the prior "allowed but unusual" path), the second add now silently replaces the first. Audit `childof_test.go` or similar if you relied on multiple parents.
- Built-in entity count increases from 16 to 17. If your code hardcodes the built-in entity count (e.g., in marshal skip-sets or test baselines), update to include `Exclusive` (17).

### Non-goals (explicitly out of scope for v0.34.0)

- No Symmetric trait. No Transitive trait. No Reflexive trait.
- No observer-driven enforcement layer (runtime addIDImmediate check is sufficient).
- No `UnsetExclusive` / removing the Exclusive flag.

---

## v0.33.0 — 2026-05-12 — Phase 15.1: OnInstantiate Override / DontInherit policies

### Added

- **`SetInstantiatePolicy(w, componentID, action)`** — register the OnInstantiate behavior for a component entity. `action` is one of `w.Override()`, `w.Inherit()`, or `w.DontInherit()`. The three actions are mutually exclusive; calling Set replaces any prior policy.
- **`GetInstantiatePolicy(w, componentID) (ID, bool)`** — read back a registered policy. Returns `(actionID, true)` if an explicit policy has been set; `(0, false)` for the implicit default.
- **Override behavior**: when `(IsA, prefab)` is added to an entity, every component on the prefab (or its IsA chain) that has `policyOnInstantiateOverride` is eagerly copied into the instance's own local slot. Mutations to the instance copy are isolated from the prefab and sibling instances. If the instance already owns the component before the IsA-add, the copy is skipped (user value wins).
- **DontInherit behavior**: `Get`/`Has` no longer walk the IsA chain for a component with `policyOnInstantiateDontInherit`. Query auto-promotion to `Self|Up(IsA)` is suppressed even when `SetInheritable[T]` was called (DontInherit takes precedence).
- **Pair-add form first-class**: `fw.AddID(cid, MakePair(w.OnInstantiate(), w.Override()))` produces identical state to `SetInstantiatePolicy(w, cid, w.Override())`. Both are tested via `GetInstantiatePolicy` round-trip.
- **`instantiate_policies_test.go`** — 13 test cases covering: Override eager copy, DontInherit suppresses Get/Has, DontInherit overrides Inheritable in query, mixed policies (Inherit/Override/DontInherit on same prefab), Override removal restores IsA path, Set/Get round-trip for all three actions, pair-add equivalence, multi-level IsA chain Override propagation, default behavior unchanged, GetInstantiatePolicy no-policy case, unknown-action panic, pre-set value wins over Override copy, and Inherit pair-add form.

### Documentation

- `docs/PrefabsManual.md` — replaced the two "Not yet ported" callouts for Override and DontInherit (lines 280–294) with shipped-in-v0.33.0 content and working code examples. Removed the corresponding stale bullets from the "Not yet ported" section.
- `docs/ComponentTraits.md` — updated the roadmap table (OnInstantiate/Inherit/Override/DontInherit rows to `✅ shipped (v0.33.0)`); revised the prose at lines 43 and 155–158.
- `docs/README.md` — removed the stale "OnInstantiate / Override / DontInherit traits" and "Auto-override on instantiation" feature-gap entries.
- `ROADMAP.md` — moved the OnInstantiate full-behavior item to the Shipped section.

### Non-goals (explicitly out of scope for v0.33.0)

- No change to `SetInheritable[T]` / `TypeInfo.Inheritable`; the two systems coexist.
- No recursive prefab-children copying (`flecs_instantiate_children`); child-entity replication remains a documented gap.
- No partial-flush rollback on panic mid-Override-copy.

---

## v0.32.0 — 2026-05-12 — Phase 15.0: Configurable cleanup policies

### Added

- **Configurable cleanup policies** — `OnDelete` and `OnDeleteTarget` trait relationships with `RemoveAction`, `DeleteAction`, and `PanicAction` action entities. Mirrors C flecs `src/on_delete.c` and `src/bootstrap.c:294–309`.
  - `World.OnDelete() ID`, `World.OnDeleteTarget() ID` — trait relationship accessors.
  - `World.RemoveAction() ID`, `World.DeleteAction() ID`, `World.PanicAction() ID` — action tag accessors.
  - `SetCleanupPolicy(w, relID, trait, action)` — register a cleanup policy for a relationship or component entity.
  - `GetCleanupPolicy(w, relID, trait) (ID, bool)` — read back a registered policy.
  - Pair-add path: `fw.AddID(relID, MakePair(w.OnDeleteTarget(), w.DeleteAction()))` is equivalent to `SetCleanupPolicy` and is first-class.
- **`ChildOf` cascade-delete is now policy-driven.** Bootstrap installs `(ChildOf, OnDeleteTarget, DeleteAction)` via the general mechanism. Existing `childof_test.go` behavior is preserved bit-for-bit; there is no hardcoded ChildOf branch in `deleteImmediate`.
- **`cleanup_policies_test.go`** — 8 test cases covering: default Remove unchanged, OnDeleteTarget+Delete, OnDeleteTarget+Panic, ChildOf cascade regression (verifies general mechanism drives existing cascade), OnDelete+Delete entity-delete no-op, Delete-beats-Remove precedence, Panic propagation from World.Delete and deferred Writer.Delete, and wildcard target cascade.

### Migration Guide

Existing code using `ChildOf` cascade-delete is unaffected — the observable behavior is identical. The new `OnDelete` / `OnDeleteTarget` API is purely additive.

Built-in entity count increases from 11 to 16. If your code hardcodes the built-in entity count (e.g., in marshal skip-sets or test baselines), update to include `OnDelete` (12), `OnDeleteTarget` (13), `RemoveAction` (14), `DeleteAction` (15), `PanicAction` (16).

### Non-goals (explicitly out of scope for v0.32.0)

- No observer-driven `OnDelete` / `OnDeleteTarget` event callbacks (policies only).
- No `OnDelete` component-remove cascade on the component-remove path (only the entity-delete path is covered).
- No auto-`(IsA, OnDeleteTarget, Panic)` bootstrap — matches C. See `docs/PrefabsManual.md` for opt-in recipe.

---

## v0.31.0 — 2026-05-12 — Phase 14.12: FAQ doc port (docs port complete)

This phase completes the docs-port project. Phases 14.0–14.12 spanned 13 releases (v0.19.0–v0.31.0) and ported every relevant upstream C flecs document to Go idioms, verified with compile-tested code blocks throughout.

### Added

- **`docs/FAQ.md`** — full Go-idiomatic port of the upstream C flecs FAQ. Keeps the Q&A format; every C-specific answer is replaced with its Go equivalent. Adds Go-specific entries: why generics over `interface{}`, why panic over error returns, how the Reader/Writer model compares to `sync.RWMutex`, goroutine safety, why there is no module system, and why `log/slog` is the logger. Covers performance pitfalls (query creation in loops), the entity ID recycling large-number behaviour, `AddID` vs `Set` semantics, hierarchy path lookup, deferred mutations inside systems, and change detection via observers. Cross-links [Manual](docs/Manual.md), [Quickstart](docs/Quickstart.md), [Relationships](docs/Relationships.md), [FlecsRemoteApi](docs/FlecsRemoteApi.md), and [docs/README.md](docs/README.md).
- **`docs/faq_examples_test.go`** — 6 test functions (`TestFAQ_*`) verifying every code-bearing answer in the FAQ: cached-query build-once pattern, entity ID recycling and generation bits, AddID vs Set tag/value semantics, full-path vs LookupChild hierarchy lookup, deferred mutations from inside a system via `it.Writer()`, and OnSet observer change detection. Run with `go test ./docs/...`.
- **`docs/README.md`** — FAQ row updated to `✅ landed / 14.12`.

### Changed

- **`ROADMAP.md`** — Phase 14.12 row updated to `✅ shipped (v0.31.0)`; "Documentation port complete (v0.31.0)" note added above the phase table.

## v0.30.0 — 2026-05-12 — Phase 14.11: Manual doc port

### Added

- **`docs/Manual.md`** — full Go-idiomatic port of the upstream C flecs Manual, adapted as a condensed cross-link hub. Leads with a one-paragraph summary of Go flecs and a concept-map table pointing into all per-topic manuals. Original content covers: world lifecycle (`New`, `Read`/`Write`, `Progress`, `FrameCount`, `Time`, `IsAlive`), Go API design conventions (naming, idempotence, panic-based error handling, Go packages as module system), deferred-operation semantics, concurrency model (Reader/Writer scopes from Phase 10.x, ExclusiveAccess goroutine-pinning from Phase 12.x, parallel and multi-threaded dispatch), performance characteristics summary, and a C-to-Go feature mapping table. Aggressively cross-links all ten per-topic manuals and BENCH.md.
- **`docs/manual_examples_test.go`** — 6 test functions (`TestManual_*`) verifying every code block in the manual: world lifecycle with system progress, world-state accessors (FrameCount / Time / IsAlive), read-scope inspection, ExclusiveAccess begin/end, worker-count configuration with parallel system, and idempotent component add. Run with `go test ./docs/...`.
- **`docs/README.md`** — Manual row updated to `✅ landed / 14.11`.

### Changed

- **`ROADMAP.md`** — Phase 14.11 row updated to `✅ shipped (v0.30.0)`.
- All 11 per-topic docs updated to cross-link back to `Manual.md` in their See Also / Where Next sections.

## v0.29.0 — 2026-05-12 — Phase 14.10: DesignWithFlecs doc port

### Added

- **`docs/DesignWithFlecs.md`** — full Go-idiomatic port of the upstream C flecs DesignWithFlecs guide. Covers ECS design patterns: entity lifecycle and naming, small atomic components vs. complex component data, uncached vs. cached query selection, single-responsibility system design, four built-in pipeline phases (`PreUpdate`, `OnFixedUpdate`, `OnUpdate`, `PostUpdate`) with conventions for each, Go-package-based module structure as the Go equivalent of C `ECS_MODULE`, relationship signs and the tags-vs-components-vs-relationships decision table, and reactive observer design. Includes a "Custom phases not yet ported" callout and a design-tips summary section. Aggressively cross-links all other ported docs: [Quickstart](docs/Quickstart.md), [Entities & Components](docs/EntitiesComponents.md), [Queries](docs/Queries.md), [Relationships](docs/Relationships.md), [Hierarchies](docs/HierarchiesManual.md), [Prefabs](docs/PrefabsManual.md), [Systems](docs/Systems.md), [Observers](docs/ObserversManual.md), [ComponentTraits](docs/ComponentTraits.md), [FlecsRemoteApi](docs/FlecsRemoteApi.md).
- **`docs/design_examples_test.go`** — 10 test functions (`TestDesign_*`) verifying every code block in the manual: entity creation via prefab, lifecycle/IsAlive guard, entity names, atomic component queries, uncached and cached query patterns, single-responsibility system, phase ordering (PreUpdate → OnUpdate → PostUpdate), tag relationship pairs, and prefab variant inheritance. Run with `go test ./docs/...`.
- **`docs/README.md`** — DesignWithFlecs row updated to `✅ landed / 14.10`.

### Changed

- **`ROADMAP.md`** — Phase 14.10 row updated to `✅ shipped (v0.29.0)`.

## v0.28.0 — 2026-05-12 — Phase 14.9: FlecsRemoteApi doc port

### Added

- **`docs/FlecsRemoteApi.md`** — full Go-idiomatic port of the upstream C flecs FlecsRemoteApi manual. Leads with the simplest mount pattern (`http.ListenAndServe(":8080", flecs.NewRESTHandler(w))`), shows a custom `ServeMux` + `StripPrefix` example, and covers goroutine safety via `world.Read` / `world.Write` integration. Documents all 7 implemented endpoints (`GET /stats`, `GET /components`, `GET /components/{id}`, `GET /entities`, `GET /entities/{id}`, `GET /snapshot`, `PUT /snapshot`) with accurate request/response JSON shapes (verified against `rest.go`), curl examples, and Go client examples. Covers all unimplemented C flecs REST endpoints (entity mutation, component mutation, toggle, query execution, world dump, type-info, FlecsStats stats, command capture, FlecsExplorer, WebSocket, auth, JavaScript client) as explicit "Not yet ported in Go flecs" callouts with explanations of what the C endpoint does and why it is absent. Cross-links to [Quickstart](docs/Quickstart.md), [Systems](docs/Systems.md), and [ComponentTraits](docs/ComponentTraits.md).
- **`docs/rest_examples_test.go`** — 8 test functions (`TestRest_*`) using `httptest` to spin up `NewRESTHandler` and verify the code patterns shown in the manual: basic setup, Stats decoding, component listing and lookup, entity listing and detail, snapshot round-trip, and custom-ServeMux mounting. Run with `go test ./docs/...`.
- **`docs/README.md`** — FlecsRemoteApi row updated to `✅ landed / 14.9`; 6 newly discovered feature gaps appended: query execution endpoint, entity/component mutation endpoints, toggle endpoint, FlecsStats aggregated-stats module, type-info/reflection endpoint, and FlecsExplorer integration.

### Changed

- **`ROADMAP.md`** — Phase 14.9 row updated to `✅ shipped (v0.28.0)`.

## v0.27.0 — 2026-05-12 — Phase 14.8: ComponentTraits doc port

### Added

- **`docs/ComponentTraits.md`** — full Go-idiomatic port of the upstream C flecs ComponentTraits manual. Leads with the two implemented traits: `SetInheritable[T]` / `w.SetInheritable(cid)` (auto-promotes query terms to `Self|Up(IsA)`) and the `OnInstantiate` / `Inherit` / `Override` / `DontInherit` entity ID accessors (IDs exist; full runtime behavior is partial). Covers all 20+ remaining traits from the upstream doc as explicit `Not yet ported in Go flecs` callouts with C-API sketches and Go workarounds where available. Closes with a scannable "Trait system roadmap" table listing every trait, its current status (✅ shipped / 🟡 partial / ⏳ planned), and a brief note. Cross-links to [Quickstart](Quickstart.md), [Relationships](Relationships.md), [PrefabsManual](PrefabsManual.md), [Queries](Queries.md), and the [feature-gap list](docs/README.md).
- **`docs/component_traits_examples_test.go`** — 8 test functions (`TestComponentTraits_*`) exercising all Go code blocks in the manual: inheritable query match, inherited value from base, non-match without the flag, `w.SetInheritable(cid)` by ID, all four `OnInstantiate`/`Inherit`/`Override`/`DontInherit` IDs non-zero and distinct, `Get[T]` IsA chain walk, copy-on-write override. Run with `go test ./docs/...`.
- **`docs/README.md`** — ComponentTraits row updated to `✅ landed / 14.8`; 9 newly discovered feature gaps appended: `Reflexive`, `Constant`, `DontFragment`, `Singleton` trait, `Union` trait, `Final`, `OneOf`, `With`, and `Relationship`/`Target`/`Trait` enforcement traits.

### Changed

- **`ROADMAP.md`** — Phase 14.8 row updated to `✅ shipped (v0.27.0)`.

## v0.26.0 — 2026-05-12 — Phase 14.7: ObserversManual doc port

### Added

- **`docs/ObserversManual.md`** — full Go-idiomatic port of the upstream C flecs ObserversManual. Leads with hooks (`OnAdd[T]`, `OnSet[T]`, `OnRemove[T]`): single-subscriber per (component, event), hook ordering relative to observers, replacing and clearing hooks, and the `*Writer` parameter for safe reads from within callbacks. Then covers multi-subscriber observers: `Observe[T]`, `ObserveID`, `Observe2[T]`, `Observer.Unsubscribe()`, deferred-unsubscribe semantics during active dispatch, and registration-order guarantees. Includes observer use cases (validation, indexing, replication, logging). Documents 10 not-yet-ported features: `OnReplace`, `OnDelete`/`OnDeleteTarget`, `OnTableEmpty`/`OnTableFill`, custom events, term-set observer filters, yield-on-create, observer propagation/forwarding, monitor observers, observer disabling, and fixed-source observer terms.
- **`docs/observers_examples_test.go`** — 13 test functions (`TestObservers_*`) exercising every Go code block in the manual. Run with `go test ./docs/...`.
- **`docs/README.md`** — ObserversManual row updated to `✅ landed / 14.7`; 10 newly discovered feature gaps appended.

### Changed

- **`ROADMAP.md`** — Phase 14.7 row updated to `✅ shipped (v0.26.0)`.

## v0.25.0 — 2026-05-12 — Phase 14.6: Systems doc port

### Added

- **`docs/Systems.md`** — full Go-idiomatic port of the upstream C flecs Systems doc. Covers `NewSystem` with the default OnUpdate phase, `NewSystemInPhase` with all four built-in phases (`PreUpdate`, `OnFixedUpdate`, `OnUpdate`, `PostUpdate`), pipeline phase execution order, `delta_time` semantics, `SetFixedTimestep` accumulator loop with spiral-of-death warning, system lifecycle (`Close` / `IsClosed`), parallel dispatch (`SetParallel`, `SetWriteSet`, `SetWorkerCount`), deferred-mutation semantics in parallel systems, multi-threaded within-system row-range splitting (`SetMultiThreaded`), and `World.Stats()` per-phase timing observability. Seven not-yet-ported features documented: custom phases, DependsOn ordering, system disabling, rate filters, single-system `Run`, `RunWorker`, and pipeline introspection.
- **`docs/systems_examples_test.go`** — 10 test functions (`TestSystems_*`) exercising every Go code block in the manual. Run with `go test ./docs/...`.
- **`docs/README.md`** — Systems row updated to `✅ landed / 14.6`; 7 newly discovered feature gaps appended.

### Changed

- **`ROADMAP.md`** — Phase 14.6 row updated to `✅ shipped (v0.25.0)`.
- **`docs/Quickstart.md`** — updated Systems Next Steps row from "pending Phase 14.6" to link to the landed manual.

## v0.24.0 — 2026-05-12 — Phase 14.5: PrefabsManual doc port

### Added

- **`docs/PrefabsManual.md`** — full Go-idiomatic port of the upstream C flecs PrefabsManual. Covers declaring and instantiating prefabs (`fw.NewEntity()` + `fw.AddID` with `MakePair(w.IsA(), prefab)`), value inheritance via `Get`/`Has`, query-time inheritance via `SetInheritable[T]` (cross-link to [Phase 13.1](#v0180--2026-05-12--phase-131-inheritable-components)), copy-on-write override (`Set` on instance), restoring inheritance (`Remove`), `Owns[T]` to distinguish local from inherited components, prefab variants (IsA chain between prefabs), and traversal helpers (`PrefabOf`, `EachPrefab`, `GetUp[T]` with `w.IsA()`). The `(OnInstantiate, Override)` and `(OnInstantiate, DontInherit)` trait sections carry explicit `Not yet ported in Go flecs` callouts with workarounds. Prefab tag, prefab hierarchies, and prefab slots are documented as not-yet-ported in the final section.
- **`docs/prefabs_examples_test.go`** — 9 test functions (`TestPrefabs_*`) exercising every Go code block in the manual. Run with `go test ./docs/...`.
- **`docs/README.md`** — PrefabsManual row updated to `✅ landed / 14.5`; 4 newly discovered feature gaps appended: prefab tag (`EcsPrefab`), auto-override on instantiation, prefab hierarchies, and prefab slots (`SlotOf`).

### Changed

- **`ROADMAP.md`** — Phase 14.5 row updated to `✅ shipped (v0.24.0)`.
- **`docs/Quickstart.md`** — added cross-link from the Prefabs section to `PrefabsManual.md`.
- **`docs/Relationships.md`** — added cross-link from the IsA section to `PrefabsManual.md`.

## v0.23.0 — 2026-05-12 — Phase 14.4: HierarchiesManual doc port

### Added

- **`docs/HierarchiesManual.md`** — full Go-idiomatic port of the upstream C flecs HierarchiesManual. Covers creating ChildOf hierarchies (`AddID` + `MakePair(w.ChildOf(), parent)`), getting parents and children (`Reader.ParentOf`, `Reader.EachChild`), cascade delete semantics (hardcoded for ChildOf, implemented in `childof.go`), depth-first traversal via recursive `EachChild`, breadth-first (Cascade) traversal with `NewCachedQueryFromTerms` + `Cascade(w.ChildOf())`, hierarchical names (`SetName`, `GetName`, `PathOf`, `Lookup`, `LookupChild`), reparenting (remove old pair, add new pair), and ancestor traversal helpers (`GetUp[T]`, `HasUp`, `TargetUp`). Unported features carry explicit `Not yet ported in Go flecs` callouts: configurable cleanup policies, `OrderedChildren` trait, entity scoping (`ecs_set_scope`), and `Parent` hierarchy storage.
- **`docs/hierarchies_examples_test.go`** — 14 test functions (`TestHierarchies_*`) exercising every Go code block in the manual. Run with `go test ./docs/...`.
- **`docs/README.md`** — HierarchiesManual row updated to `✅ landed / 14.4`; 2 newly discovered feature gaps appended: `OrderedChildren` trait and `Parent` hierarchy storage.

### Changed

- **`ROADMAP.md`** — Phase 14.4 row updated to `✅ shipped (v0.23.0)`.

## v0.22.0 — 2026-05-12 — Phase 14.3: Relationships doc port

### Added

- **`docs/Relationships.md`** — full Go-idiomatic port of the upstream C flecs Relationships manual. Covers pair-ID encoding (`MakePair`), tag pairs (`AddID`/`RemoveID`/`HasID`), data pairs (`SetPair[T]`/`GetPair[T]`/`GetPairRef[T]`), relationship queries (`NewQueryFromTerms` with pair terms), adding a component multiple times via different pair targets, inspecting entity pairs (`EntityComponents`), the built-in `IsA` relationship (component sharing, copy-on-write override, `EachPrefab`), the built-in `ChildOf` relationship (`EachChild`, `ParentOf`, namespacing via `Lookup`/`LookupChild`), relationship traversal (`GetUp`/`HasUp`/`TargetUp`), and query traversal terms (`Up`/`SelfUp`/`Cascade`). Unported features carry explicit `Not yet ported in Go flecs` callouts: wildcard queries, exclusive/symmetric/transitive/traversable relationship traits, configurable cleanup policies, `PairIsTag` trait, and entity scoping.
- **`docs/relationships_examples_test.go`** — 19 test functions (`TestRelationships_*`) exercising every Go code block in the manual. Run with `go test ./docs/...`.
- **`docs/README.md`** — Relationships row updated to `✅ landed / 14.3`; 7 newly discovered feature gaps appended: exclusive relationship trait, symmetric relationship trait, transitive relationship trait, traversable relationship trait, configurable cleanup policies, PairIsTag trait, entity scoping.

### Changed

- **`ROADMAP.md`** — Phase 14.3 row updated to `✅ shipped (v0.22.0)`.

## v0.21.0 — 2026-05-12 — Phase 14.2: Queries doc port

### Added

- **`docs/Queries.md`** — full Go-idiomatic port of the upstream C flecs Queries manual. Covers archetype tables and caching, creating queries (`NewQuery` / `NewQueryFromTerms` / `NewCachedQuery` / `NewCachedQueryFromTerms`), operators (And / Not / Optional / Or), pull-style iteration (`Iter` / `Next` / `Field[T]` / `FieldMaybe[T]` / `FieldShared[T]` / `IsFieldSelf`), typed iteration (`Each1` / `Each2`), pairs in queries, relationship traversal (`Up` / `SelfUp` / `Cascade`), inheritable components (`SetInheritable`), and change detection (`Changed` / `Close`). Sections for features not yet ported carry explicit `Not yet ported in Go flecs` callouts: wildcards, fixed per-term sources, query variables, sorted queries, query groups, equality operators, AndFrom/OrFrom/NotFrom operators, query scopes, access modifiers, and member value queries.
- **`docs/queries_examples_test.go`** — 19 test functions (`TestQueries_*`) exercising every Go code block in the manual. Run with `go test ./docs/...`.
- **`docs/README.md`** — Queries row updated to `✅ landed / 14.2`; 8 newly discovered feature gaps appended to the feature-gap list (fixed per-term source, query variables, sorted queries, query groups, equality operators, AndFrom/OrFrom/NotFrom, query scopes, access modifiers).

### Changed

- **`ROADMAP.md`** — Phase 14.2 row updated to `✅ shipped (v0.21.0)`; corrected off-by-one version attributions for 14.0 (v0.19.0) and 14.1 (v0.20.0).

## v0.20.0 — 2026-05-12 — Phase 14.1: EntitiesComponents doc port

### Added

- **`docs/EntitiesComponents.md`** — full Go-idiomatic port of the upstream C flecs EntitiesComponents manual. Covers entity lifecycle (create, delete, liveliness, naming, hierarchical lookup), component operations (`Set`/`Get`/`Has`/`Owns`/`Remove`/`AddID`), tags (static and dynamic), component hooks (`OnAdd`/`OnSet`/`OnRemove`), components as entities (`RegisterComponent`, `ComponentInfo`), and the singleton workaround. Sections for features not yet ported carry explicit `Not yet ported in Go flecs` callouts with links to the feature-gap list.
- **`docs/entities_components_examples_test.go`** — 16 test functions (`TestEC_*`) exercising every Go code block in the manual. Run with `go test ./docs/...`.
- **`docs/README.md`** — EntitiesComponents row updated to `✅ landed / 14.1`; 9 newly discovered feature gaps appended to the feature-gap list (Clear, MakeAlive, SetVersion, entity ranges, entity disabling, on_replace hook, runtime component registration, cleanup policy cascade, CanToggle trait).

### Changed

- **`ROADMAP.md`** — Phase 14.1 row updated to `✅ shipped (v0.20.0)`.

## v0.19.0 — 2026-05-12 — Phase 14.0: Documentation survey + Quickstart

### Added

- **`docs/` directory** — new top-level directory containing the Go flecs conceptual documentation.
- **`docs/Quickstart.md`** — fully written Go-idiomatic walkthrough covering world creation, entities, components, named entities, tags, ergonomic iteration (`Each1`/`Each2`), queries (AND / NOT / Optional), relationships, ChildOf hierarchies, IsA prefabs, systems, and observers. All code blocks verified against v0.18.0.
- **`docs/quickstart_examples_test.go`** — Go test file (`package docs_test`) exercising every Quickstart code pattern; run with `go test ./docs/...`.
- **`docs/README.md`** — docs index with landing status (✅ Quickstart, pending 14.1–14.12), full survey table (19 C docs classified as port-adapted / port-with-gaps / skip), and feature-gap list vs. upstream C (17 candidate follow-up issues listed for operator prioritization; none filed in this phase).
- **Skeleton stub files** for the remaining planned ports: `EntitiesComponents.md`, `Queries.md`, `Relationships.md`, `HierarchiesManual.md`, `PrefabsManual.md`, `Systems.md`, `ObserversManual.md`, `ComponentTraits.md`, `FlecsRemoteApi.md`, `DesignWithFlecs.md`, `Manual.md`, `FAQ.md`. Each stub has a title, one-line description, and a `<!-- TODO: port from ... (Phase 14.x) -->` marker.

### Changed

- **`README.md`** — added a "Documentation" section prominently linking to `docs/Quickstart.md` and `docs/README.md`.
- **`doc.go`** — added a `# Conceptual Documentation` section pointing to `docs/` as the authoritative reference for topic-level guides.
- **`ROADMAP.md`** — added a "Documentation" section with the 14.0–14.12 phase table and the operator-directive process rule (every phase from 14.0 onward must include an "update docs accordingly" deliverable).

## v0.18.0 — 2026-05-12 — Phase 13.1: Inheritable components

Auto-`Self|Up(IsA)` promotion for components marked with `SetInheritable`.
`Each1`/`Each2`/`NewQuery` and friends now match entities that *inherit* a
component from a prefab via IsA — without requiring explicit traversal modifiers
on every query term. Port of C flecs `validator.c:766-770`.

### Added

- **`SetInheritable[T any](w *World)`** — marks component type T as inheritable.
  Must be called after `RegisterComponent[T]` and before any query referencing T
  is built.
- **`(*World).SetInheritable(cid ID)`** — non-generic variant; panics if cid is
  not a registered component.
- **`World.OnInstantiate() ID`**, **`World.Inherit() ID`**,
  **`World.Override() ID`**, **`World.DontInherit() ID`** — four new built-in
  trait entities (indices 8-11). These expose the C flecs `(OnInstantiate,
  Inherit)` pair IDs for API symmetry. The Go port uses a direct bool on
  `TypeInfo` rather than a pair observer; the IDs are provided for future-proofing.
- **`TraverseExplicitSelf`** (`= 4`) internal sentinel returned by `Term.Self()`.
  The validator skips auto-promotion when it sees this value, so explicit
  `.Self()` on an inheritable-component term keeps the term local-only.
- **Auto-promotion in `NewQuery`, `NewQueryFromTerms`, `NewCachedQuery`,
  `NewCachedQueryFromTerms`** — any `TermAnd`/`TermOptional` term whose
  `Traverse` is the default zero and whose component is inheritable is promoted
  to `TraverseSelfUp` with `Trav = w.IsA()`. TermNot is never promoted.
- **Shared-pointer semantics in `Each1`/`Each2`/`Each3`/`Each4`** — when a term
  was resolved via an ancestor (Up path), the same prefab component pointer is
  passed for every entity in the matched table (C flecs option (a); documented
  as a foot-gun in each function's godoc).
- **20 new tests** in `inheritance_test.go` covering: Each1 match, value from
  prefab, local override, unmarked stays exclusive, explicit Self override, Each2
  mixed, Each3/Each4 all-inherited and mixed-inherited-local variants, inherited
  tag component, first-local-rest-inherited, NewQueryFromTerms FieldShared,
  cached query rematch, TermNot not promoted, SetInheritable panic for
  unregistered ID/type, explicit Up, built-in trait entity distinctness.
- **2 new benchmarks** in `bench_test.go`:
  - `BenchmarkInheritableEach1_NoInheritors` — inheritable component, no IsA
    pairs (baseline; should be within noise of `BenchmarkEach1`).
  - `BenchmarkInheritableEach1_WithInheritors` — N inheritors from one prefab.

### Changed

- `Term.Self()` now returns `TraverseExplicitSelf` (4) instead of `TraverseSelf`
  (0). At runtime both behave identically (local-only match). The change is
  source-compatible: callers don't inspect the numeric value.
- `validateAndSortTerms` signature now takes `*World` as the first argument to
  enable auto-promotion. Package-internal only; no public API change.
- Marshal (`MarshalJSON`) now skips the four new built-in trait entities, keeping
  serialized output user-entity-only as before.
- `World.Count()` on a fresh world now returns 11 instead of 7.

### Not ported (deliberate)

- **`(OnInstantiate, Inherit)` pair as the component representation.** C flecs
  stores the trait as a pair and folds it into `cr->flags` via an observer. The
  Go port stores a bool on `TypeInfo` directly (no `ecs_component_record_t`
  analog). The pair IDs are exposed but not consumed by the validator.
- **Trait-locked check.** C flecs panics if `EcsInheritable` is set after a
  query has been built (`flecs_component_is_trait_locked`). The Go port omits
  this for now; calling `SetInheritable` after queries produces undefined
  match-behavior on existing queries but no panic.
- **Down-cache observers.** Same limitation as Phase 13.0: cached queries are
  NOT automatically invalidated when a prefab gains/loses an inheritable
  component after construction. Rebuild the query in that case.

## v0.17.0 — 2026-05-12 — Phase 13.0: Query-term traversal modifiers

Inline traversal in `NewQueryFromTerms` / `NewCachedQueryFromTerms`. Terms can
now express "match this entity OR any ancestor through relationship `rel`."
Faithful port of C flecs's `EcsSelf` / `EcsUp` / `EcsCascade` term traversal
flags (`/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h`
lines 736-833).

### Added

- **`Term` traversal modifiers** (chained on the term builder):
  - `.Self()` — match only the entity itself (the existing default; named for symmetry).
  - `.Up(rel ID)` — match if any ancestor via `rel` has the component; the entity itself need not have it.
  - `.SelfUp(rel ID)` — match if the entity has the component locally OR any ancestor via `rel` does. Local takes precedence.
  - `.Cascade(rel ID)` — same as `Up` but the cached query iterates tables in breadth-first depth order (roots first). Cached queries only.
- **`TraverseFlags` type** — internal bitfield carried on `Term`. Values: `TraverseSelf`, `TraverseUp`, `TraverseCascade` (combinable). Exposed for advanced custom term construction.
- **`IsFieldSelf(it, termIdx) bool`** — true if the term was matched via Self (local), false if matched via Up/SelfUp from an ancestor. Mirror of C's `ecs_field_is_self`.
- **`FieldShared[T](it, termIdx) (T, bool)`** — returns the single shared ancestor value for an Up-matched term. Returns `(zero, false)` if the term was matched via Self. Mirror of C's `ecs_field_src` + `ecs_get`.
- **Cascade ordering for cached queries** — `tableRelDepth(table, rel) int` computes depth from a root; `sortByCascadeDepth` orders the cache's table list at construction. New matching tables (from `notifyTableCreated`) re-sort on insertion.
- **`BenchmarkQueryUpTraversal`** — establishes the cost of the up-walk relative to flat queries.
- **15 new tests** in `query_terms_test.go` covering matches-via-prefab, matches-via-ChildOf, SelfUp-prefers-self, dead-ancestor safety, cycle safety, cascade depth ordering, cascade-rejected-for-uncached, cached-query up-match, new-table-triggers-rematch, FieldShared/Field panic boundaries.

### Changed

- **`Field[T]` panics when called on an Up-matched term.** The panic message redirects to `FieldShared[T]`. This is a runtime check to catch the common mistake of treating a shared inherited value as a per-row column. Self-matched terms behave exactly as before.
- **`NewQueryFromTerms` panics on `Cascade`.** Cascade is cached-only (matches C's behavior in `src/query/api.c:246`). The panic message points to `NewCachedQueryFromTerms`.
- **`QueryIter` carries traversal state.** Per-table `upSources map[ID]ID` records which ancestor provides each Up-matched component. Adds ~128 B per iter and ~10% on `BenchmarkQueryIterField_10k`; flat queries with no traversal terms unaffected on the matcher hot path.

### Not ported (deliberate)

- **`EcsTrav`** (transitive query) — advanced; not in our roadmap.
- **`EcsDesc`** (descending sort) — niche.
- **Down-cache observers** — runtime mutation of prefab components doesn't invalidate cached queries that matched via Up. Document the limitation; refile if a real use case appears.
- **Auto-`Self|Up` for inheritable components** (C `validator.c:766-770`) — would change the default semantics of `Each1`/`Each2`/`NewQuery[T]`. We keep inheritance strictly opt-in via the term builder to preserve current behavior.

### Performance

- Existing benches (`BenchmarkEach1`, `BenchmarkCachedQueryEach2_10k`, `BenchmarkSetExistingComponent`) within noise of v0.16.0.
- `BenchmarkQueryIterField_10k`: ~+10% time, +128 B/op due to per-iter `upSources` bookkeeping. Allocations unchanged (still 2 allocs/op). Acceptable; this is the cost of carrying traversal state on every iter.
- Coverage: 95.1% on main package.

## v0.16.0 — 2026-05-12 — Phase 12.1: Per-stage command queues

Lock-free deferred mutations for multi-threaded systems. Each worker goroutine
now writes into its own per-stage command queue with no synchronization on the
hot path. After `wg.Wait()`, the main goroutine merges stages in ascending id
order (worker stages 1…N, then stage 0). Within each stage, per-entity FIFO
coalescing is preserved; there is no cross-stage coalescing. Hook callbacks
fired during the merge always run on the main goroutine and receive the stage-0
`*Writer`.

### Changes

- **`World.stages`** — replaces `deferMu`/`deferDepth`/`deferred`; a slice of
  `*stage` structs (one per goroutine context). `stages[0]` is the main stage;
  `stages[1..N]` are worker stages with `deferDepth` permanently 1.
- **`Writer.stage`** — each `Writer` now carries a pointer to its owning stage.
  `Set`, `Remove`, `Delete`, `AddID`, `RemoveID`, `SetPair`, `SetByID`,
  `SetPairByID` all route through `stage.queue` when `deferDepth > 0`.
- **`QueryIter.Writer()`** — returns the per-worker `*Writer` inside a
  multi-threaded system dispatch; returns the shared stage-0 `*Writer` otherwise.
- **`BenchmarkMultiThreadedDeferredSet`** — new benchmark sweeping workers in
  {1, 2, 4}; demonstrates ≥ 2x speedup on 4 workers vs 1 worker for the
  deferred-mutation path.

## v0.15.0 — 2026-05-11 — Scoped Capability API (Reader / Writer) — BREAKING CHANGE

> **Breaking change.** The legacy `Defer`/`DeferBegin`/`DeferEnd`/`Readonly`/`ReadonlyBegin`/`ReadonlyEnd`
> methods have been removed from `*World`. Hook and observer callback signatures have changed.
> See the migration guide below.

Completes the Reader/Writer scoped-capability migration begun in v0.14.0.
All mutation entrypoints now require an explicit `*Writer` capability obtained from
`world.Write(func(*Writer))`. All read entrypoints require an explicit `*Reader` from
`world.Read(func(*Reader))`. The old bare-`*World` mutation methods are gone.

### Breaking Changes

#### API removals

| Removed | Replacement |
|---------|-------------|
| `world.Defer(fn func())` | `world.Write(func(fw *flecs.Writer) { fn() })` |
| `world.DeferBegin()` | `world.Write(...)` or internal `deferScope` (unexported) |
| `world.DeferEnd()` | (same — managed by `Write` scope) |
| `world.Readonly(fn func())` | `world.Read(func(fr *flecs.Reader) { fn() })` |
| `world.ReadonlyBegin()` | (internal only; use `world.Read`) |
| `world.ReadonlyEnd()` | (internal only; use `world.Read`) |

#### Hook callback signature

```go
// v0.14 and earlier:
flecs.OnSet[T](w, func(e flecs.ID, v *T) { ... })

// v0.15:
flecs.OnSet[T](w, func(fw *flecs.Writer, e flecs.ID, v T) { ... })
```

Same change applies to `OnAdd[T]` and `OnRemove[T]`. The value is now passed by
value (not pointer). The `*Writer` parameter provides safe mutation access from
within a hook without re-entering the world mutex.

#### Observer callback signature

```go
// v0.14 and earlier:
flecs.Observe[T](w, func(e flecs.ID, v *T) { ... })
flecs.ObserveID(w, id, event, func(e flecs.ID, ptr unsafe.Pointer) { ... })
flecs.Observe2[T](w, func(e flecs.ID, v *T) { ... })

// v0.15:
flecs.Observe[T](w, func(fw *flecs.Writer, e flecs.ID, v T) { ... })
flecs.ObserveID(w, id, event, func(fw *flecs.Writer, e flecs.ID, ptr unsafe.Pointer) { ... })
flecs.Observe2[T](w, func(fw *flecs.Writer, e flecs.ID, v T) { ... })
```

#### Migration guide

```go
// --- Mutation (Set/Add/Remove/Delete) ---
// Before:
w.Defer(func() {
    flecs.Set(w, e, MyComp{X: 1})
    w.Delete(e2)
})
// After:
w.Write(func(fw *flecs.Writer) {
    flecs.Set(fw, e, MyComp{X: 1})
    fw.Delete(e2)
})

// --- Read-only iteration ---
// Before:
w.Readonly(func() {
    flecs.Each1[MyComp](w, func(e flecs.ID, p *MyComp) { ... })
})
// After:
w.Read(func(fr *flecs.Reader) {
    flecs.Each1[MyComp](fr, func(e flecs.ID, p *MyComp) { ... })
})

// --- Hooks ---
// Before:
flecs.OnSet[Score](w, func(e flecs.ID, v *Score) { fmt.Println(v.Value) })
// After:
flecs.OnSet[Score](w, func(_ *flecs.Writer, e flecs.ID, v Score) { fmt.Println(v.Value) })
```

### Added

- **`Reader` / `Writer` types** — `Reader` holds read-only methods; `Writer` embeds
  `Reader` and adds mutating methods. Both are obtained via `world.Read` / `world.Write`.
- **`world.Read(fn func(*Reader))`** — opens a shared-read scope (RLock). Multiple
  goroutines may hold concurrent Read scopes.
- **`world.Write(fn func(*Writer))`** — opens an exclusive read/write scope. Nested
  calls from the same goroutine share the defer queue; calls from other goroutines
  block until the scope is released. Panics with `ErrExclusiveAccessViolation` if the
  world is held by a different goroutine via `ExclusiveAccessBegin`.
- **`ErrExclusiveAccessViolation`** — sentinel error value for the above panic.
- **Free functions on `*Reader`**: `Get[T]`, `GetRef[T]`, `Has[T]`, `Owns[T]`,
  `GetPair[T]`, `GetPairRef[T]`, `HasID`, `OwnsID`, `GetUp[T]`, `HasUp`, `TargetUp`,
  `PrefabOf`, `Each1–Each4`.
- **Free functions on `*Writer`**: `Set[T]`, `Remove[T]`, `AddID`, `RemoveID`,
  `SetPair[T]`.
- **`*Writer` passed to hooks and observers** — hook and observer callbacks receive a
  `*Writer` as their first argument, enabling safe mutation inside a callback without
  re-acquiring any lock.
- **`TestHookReceivesWriter`** — confirms that the `*Writer` passed to `OnSet` hooks is
  non-nil and functional.
- **`TestObserverReceivesWriter`** — confirms that the `*Writer` passed to `Observe`
  observers is non-nil and functional.
- **Concurrent-reader tests** — `TestReadAllowsConcurrentReaders`,
  `TestWriteSerializesWithReaders`, `TestWriteFromOtherGoroutinePanicsWhenClaimed`,
  `TestNestedWriteSharesScope`, `TestWriteNestedFromSameGoroutine`,
  `TestWritePanicsWhenClaimedByOtherGoroutine`,
  `TestGetRefValidInsideScopeOnly`.

### Changed

- All free functions that previously accepted `*World` as their first argument
  (`Set`, `Get`, `Has`, `Remove`, `AddID`, `RemoveID`, `HasID`, `OwnsID`, `SetPair`,
  `GetPair`, `Each1–Each4`, `GetUp`, `HasUp`, `TargetUp`, `PrefabOf`, etc.) now
  accept `*Writer` or `*Reader` as appropriate.
- Hook callbacks changed from `func(e ID, v *T)` to `func(fw *Writer, e ID, v T)`.
- Observer callbacks changed from `func(e ID, v *T)` to `func(fw *Writer, e ID, v T)`.
- `system.go`'s `runPhase` now uses the internal `deferScope` instead of
  `world.Write`, avoiding a spurious exclusive-access claim that conflicted with the
  worker goroutines in multi-threaded dispatch.
- `rest.go` handlers use `world.Read(func(*Reader))` for all read-only responses.

### Removed

- `world.Defer(fn func())` — use `world.Write(func(fw *Writer))`.
- `world.DeferBegin()` / `world.DeferEnd()` — internal lifecycle now managed by `Write`.
- `world.Readonly(fn func())` — use `world.Read(func(fr *Reader))`.
- `world.ReadonlyBegin()` / `world.ReadonlyEnd()` — internalized; use `world.Read`.
- **`world.W()` / `world.R()`** — unsynchronized escape-hatches that bypassed lock
  acquisition; removed to close the 12.0 finishing pass. Use `world.Write` / `world.Read`.
- **`world.NewEntity()`** — moved to `*Writer` only; use `world.Write(func(fw *Writer) { e = fw.NewEntity() })`.

### Performance

- `BenchmarkSetExistingComponent`: 0 allocs/op (unchanged).
- `BenchmarkDeferBatchedAdds`: 0 allocs/op, ~7 200 ns/op (unchanged from v0.14.0).
- `BenchmarkDeferSingleSet`: 0 allocs/op (unchanged).
- Test coverage: 95.1% of statements.

## v0.14.0 — 2026-05-11 — Coalescing Deferred Command Queue

Port of C flecs' tagged-union command queue and two-pass entity coalescer.
Replaces the old `[]func(*World)` closure slice with typed `cmd` structs and a
bump arena (`cmdArena`), eliminating all per-op heap allocations on the deferred
path. A per-entity intrusive linked list lets a single `batchForEntity` pass fold
every Add/Set/Remove for one entity into ONE archetype migration, matching C
flecs `flecs_cmd_batch_for_entity` semantics.

### Changed

- **`cmd` tagged-union struct** — `cmdKind` discriminant (`cmdAddID`, `cmdRemoveID`,
  `cmdSetByID`, `cmdSetPair`, `cmdDelete`, `cmdModified`, `cmdSkip`) replaces opaque
  `func(*World)` closures. 32-byte struct vs C's 56-byte `ecs_cmd_t` (Go omits
  union-tag overhead and the stage pointer).
- **`cmdArena` bump allocator** — 1 KiB reusable pages with oversized-payload
  fallback (bit 31 flag). Mirrors `ecs_stack_t`. Pages are reused across
  DeferBegin/DeferEnd pairs via `sync.Pool`; zero heap allocation in steady state.
- **Per-entity intrusive list + sign-flipped head encoding** — mirrors
  `flecs_cmd_new_batched` in `src/commands.c`. `nextForEntity < 0` identifies the
  head of a multi-cmd chain; the coalescer iterates the chain without a separate
  index structure.
- **`cmdQueue.batchForEntity`** — two-pass coalescer:
  - Pass 1: walks the chain, simulates the net component set (Add/Remove),
    rewrites processed cmds to `cmdSkip`, and calls `commitBatch` for ONE migration.
  - Pass 2: rewrites remaining `cmdSetByID`/`cmdSetPair` to `cmdModified` so that
    `dispatch` fires `OnSet` at the original submission position (FIFO hook order).
- **`sync.Pool` queue recycling** — `acquireCmdQueue`/`releaseCmdQueue` return
  `cmdQueue` objects to a pool after flush; zero allocation per flush in steady state.
- **Queue swap under mutex** — `DeferEnd` atomically swaps in a fresh `cmdQueue`
  before releasing the lock, so goroutines that start new Defer scopes during flush
  write into an independent queue.
- **`World.commitBatch`** — new internal method performing a multi-component
  add+remove migration that fires `OnAdd`/`OnRemove` only for genuinely changed IDs.

### Performance

- `BenchmarkDeferSingleSet`: **0 allocs/op**, ~112 ns/op (was 7 allocs/op).
- `BenchmarkSetExistingComponent`: 0 allocs/op, ~57 ns/op — no regression.
- `BenchmarkDeferBatchedAdds`: **~15× speedup** vs v0.13.0 closure baseline
  (7,200 ns/op vs 111,897 ns/op; 0 allocs/op vs 108 allocs/op). 100 deferred
  AddID calls on one entity produce ONE archetype migration after coalescing.
  Achieved by replacing per-call map/sort allocations in `batchForEntity` with
  reusable sorted-slice scratch buffers (`cmdQueue.scratch1/2/3`) and a
  sort-merge diff algorithm. `sigKeyLookup` uses `unsafe.String` for a
  zero-allocation table lookup in `commitBatch`'s common path.

### Tests

- `TestDeferCoalescesAddsToOneMigration` — 3 Add cmds → 1 migration, 3 OnAdd events.
- `TestDeferCoalescesRemoveAfterAdd` — Add+Remove net-zero produces no migration.
- `TestDeferSetValuePreservedAfterCoalesce` — Set value survives coalesce.
- `TestDeferHooksFireAtSubmissionPosition` — OnSet fires with per-call value in FIFO order.
- `TestDeferDeleteCoalescedWithAdd` — Delete wins over preceding Add; entity is gone.
- `TestDeferSetPairCoalesced` — pair data coalesced and written correctly.
- `TestDeferArenaMultiPage` — oversized payloads, multi-page allocation.
- `TestDeferSetZeroSizeTag` / `TestDeferSetZeroSizeTagCoalesced` — zero-size tags.
- `TestDeferArenaOversized` — payload > 1 KiB page uses oversized fallback.
- `TestDeferOriginalTestsStillPass` — regression guard for pre-existing defer tests.
- `TestDeferRemoveNonExistent` — deferred RemoveID for absent component is a no-op.
- `TestDeferCoalesceToEmpty` — entity losing all components coalesces to empty sig.
- All pass under `-race -count=5`; coverage ≥ 95.1%.

## v0.13.0 — 2026-05-11 — Within-System Multi-Threaded Dispatch

Port of C flecs' `multi_threaded` system flag. When a system calls
`SetMultiThreaded(true)` and `World.SetWorkerCount(n) > 0`, the dispatcher
fans out N concurrent worker jobs, each iterating a disjoint row slice of every
matched table. Workers never share memory; in-place `Field[T]` updates scale
linearly with core count. Deferred structural mutations (Set, Delete, AddID)
remain safe via the existing mutex-protected queue but serialize under
contention — a future per-stage-queue phase will fix that.

### Added

- **`(*System).SetMultiThreaded(bool)`** / **`(*System).MultiThreaded() bool`** — flag a system for within-system parallel dispatch. Default `false`.
- **Iter clipping in `QueryIter`** — internal `clippedCopy(workerIdx, workerTotal)` method produces N independent iters, each seeing `[first, first+count)` rows per table. `Field[T]`, `FieldMaybe[T]`, `Entities()`, and `Count()` all respect the clipped range transparently.
- **Multi-threaded dispatcher branch in `runPhase`** — multi-threaded systems are dispatched first (before parallel-batch logic), fan out N worker goroutines, and `sync.WaitGroup`-wait before continuing. Cannot batch with parallel siblings.
- **Partition formula** — matches C `src/iter.c:970-993`: `first = (count/N)*i + min(i, count%N)`, `worker_count = count/N + (i < count%N ? 1 : 0)`. Workers with `count == 0` skip the table.

### Tests

- `TestMultiThreadedSystemProcessesEachEntityOnce` — 100k entities, WorkerCount ∈ {1,2,4,8}, in-place increment, sum verified.
- `TestMultiThreadedSystemCannotBatchWithSiblings` — timing test verifying the parallel sibling waits for all MT workers.
- `TestMultiThreadedSystemUnevenSplit` — 1000 rows / 3 workers → {334, 333, 333}.
- `TestMultiThreadedSystemEmptyWorkers` — 2 rows / 4 workers → 2 active, 2 skip.
- `TestMultiThreadedSystemWithDeferredMutations` — workers calling `w.Delete`; all deletes applied correctly.
- All pass under `-race -count=10`.

### Benchmarks

- `BenchmarkMultiThreadedSystem` — 100k Vec3 entities, workers ∈ {1,2,4}; in-place Add; near-linear speedup expected.

## v0.12.0 — 2026-05-11 — Exclusive Access Ownership Assertion

Always-on ownership assertion: every public `World` method panics with
`ErrExclusiveAccessViolation` if called from a goroutine other than the one that
called `ExclusiveAccessBegin`. No build tag required; the check is always live.

### Added

- **`(*World).ExclusiveAccessBegin(threadName string)`** — claims the world for
  the calling goroutine. Any subsequent mutation or read from a different goroutine
  panics with `ErrExclusiveAccessViolation`.
- **`(*World).ExclusiveAccessEnd(lockWorld bool)`** — releases the claim. When
  `lockWorld=true` the world enters a write-locked state where all goroutines
  receive a violation panic on mutation; reads still pass. Passing `false` returns
  the world to the unclaimed state.
- **`exclusive_access atomic.Uint64` field on `*World`** — three states:
  0 = unclaimed, goroutine ID = owned by that goroutine, ^uint64(0) = write-locked.
- **`checkExclusiveAccessWrite` / `checkExclusiveAccessRead`** — internal
  functions inserted at every public entry point. Common case (no owner claimed)
  costs one `atomic.Load` per call; `goid.Get()` only runs when an owner is set.
- **`Progress` and `RegisterComponent` / `NewSystem*` / `NewQuery*` / `NewCachedQuery*`**
  are Write-checked: any of these called from a non-owner goroutine panics with
  `ErrExclusiveAccessViolation`.
- **`IsAlive` / `Count` / `SystemCount*` / `TablesFor` / `EachTableFor`**
  are Read-checked: panics when called from a non-owner goroutine while the world
  is exclusively owned.

### Changed

- **Goroutine ID** is now obtained via `github.com/petermattis/goid` (used by
  cockroachdb, etcd, and others) instead of `runtime.Stack` parsing. Cost drops
  from ~µs to ~ns per check. No `unsafe` or fragile stack-format dependency.
- **No build tag** — the exclusive-access check is always compiled in. Go makes
  goroutines a first-class feature; the ownership assertion is on by default to
  catch misuse in any build.
- **CI** — collapsed to a single test job and a single lint job; the separate
  `-tags flecs_exclusive_access` jobs are removed (there is only one build now).

## v0.11.0 — 2026-05-11 — Readonly Concurrency Window

Faithful Go port of the C flecs readonly concurrency model (`ecs_readonly_begin` /
`ecs_readonly_end`). No mutex on world state; concurrency is enforced by an
atomic flag plus deferred-command discipline. No breaking changes.

### Added

- **`(*World).ReadonlyBegin()`** — opens a readonly window. Atomically routes all
  subsequent structural mutations (Set, Remove, Delete, AddID, RemoveID, SetPair,
  SetByID) through the deferred-command queue so that concurrent readers see a
  stable snapshot of world state.
- **`(*World).ReadonlyEnd()`** — closes the window and flushes all deferred
  mutations on the calling goroutine.
- **`(*World).Readonly(fn func())`** — convenience wrapper around
  `ReadonlyBegin`/`ReadonlyEnd` with a deferred `ReadonlyEnd` for panic-safety.
- **`readonly atomic.Bool` field on `*World`** — the flag checked by every
  mutator. One extra `atomic.Bool.Load()` per mutator on the non-deferred path
  (≈1 ns; within 2% of v0.10.0 on `BenchmarkSetExistingComponent`).

### Changed

- **All mutators** (`Delete`, `Set`, `Remove`, `AddID`, `RemoveID`, `SetPair`,
  `SetByID`) — the defer-check condition `w.deferDepth > 0` is extended to
  `w.deferDepth > 0 || w.readonly.Load()`, evaluated under `deferMu`.
- **REST GET handlers** (`/stats`, `/components`, `/components/{id}`,
  `/entities`, `/entities/{id}`, `/snapshot GET`) — bodies wrapped in
  `w.Readonly(...)` so concurrent read requests get a consistent snapshot.

### Documentation

- `doc.go`: new "Concurrency model" section explaining the readonly window
  pattern and when to use it.
- `README.md`: "Concurrency model" paragraph in the core-concepts section.

---

## v0.10.0 — 2026-05-11 — Parallel System Dispatch

Opt-in parallel system dispatch within a phase. Systems flagged as
parallel-safe run in goroutines from a persistent worker pool; systems with
overlapping write sets are forced serial. ECS storage remains non-goroutine-safe;
safety is enforced conservatively via per-system write-set conflict detection.
No breaking changes.

### Added

- **`(*System).SetParallel(bool)`** — opts a system in to parallel dispatch.
  Default: `false` (serial). Takes effect only when `WorkerCount > 0`.
- **`(*System).Parallel() bool`** — returns the current parallel flag.
- **`(*System).SetWriteSet(ids []flecs.ID)`** — declares the component IDs this
  system writes. Overrides the default (all And/Or/Optional query term IDs).
  Empty slice declares a read-only system that never conflicts.
- **`(*World).SetWorkerCount(n int)`** — sets the worker pool size. `0`
  (default) = serial dispatch; `n > 0` = persistent goroutine pool with a
  buffered channel of size `2n`. Negative panics. Changing `n` between
  `Progress` calls tears down the old pool. Calling during `Progress` is a
  no-op.
- **`(*World).WorkerCount() int`** — returns the current pool size.
- **Parallel phase dispatch** — within each phase, systems are partitioned into
  maximal contiguous batches of parallel-safe systems with pairwise-disjoint
  write sets. Each batch is dispatched via `sync.WaitGroup` before the next
  batch starts. Serial systems form single-system batches.
- **Deferred-safe parallel mutations** — `Set`, `Remove`, `Delete`, `AddID`,
  `RemoveID`, `SetPair`, `SetByID` are mutex-protected on the defer queue;
  parallel systems can safely call these without data races.

### Documentation

- `doc.go`: new "Parallel Execution" section with code snippet, conflict-
  detection explanation, and storage-not-goroutine-safe rule.
- `BENCH.md`: parallel vs serial speedup measurements for 10k-entity dispatch.

---

## v0.9.0 — 2026-05-11

Structured lifecycle logging via `log/slog`. No breaking changes.

### Added

- **`(*World).SetLogger(*slog.Logger)`** — installs or replaces the structured
  logger. Passing `nil` disables logging (the default). Documented lifecycle
  event surface: no hot-path logs.
- **`(*World).Logger() *slog.Logger`** — returns the currently installed logger,
  or `nil` if none.
- **Lifecycle log records** at DEBUG level for:
  - `entity created` / `entity deleted` (one per entity, including cascade deletes)
  - `component registered` (first `RegisterComponent[T]` call only)
  - `table created` (new archetype; `signature_len` + `signature` attrs)
  - `system added` (with `phase` attr) / `system closed`
  - `observer registered` (with `id` + `event` attrs) / `observer unsubscribed`
  - `snapshot serialized` / `snapshot loaded` (with `entities` count attr)
- Nil-logger fast path: single pointer compare at each event site; verified
  no measurable regression on `BenchmarkNewEntity` or `BenchmarkSetExistingComponent`.

---

## v0.8.0 — 2026-05-11

Minimal read-only REST API addon — exposes world inspection and snapshot
save/load over HTTP so external tools can introspect a running flecs world.
No breaking changes.

### Added

- **`NewRESTHandler(w *World) http.Handler`** — returns a configured
  `*http.ServeMux` wired to the given world. Users provide their own
  `*http.Server`. Routes:
  - `GET /stats` — world stats JSON (`Stats`)
  - `GET /components` — all registered component infos
  - `GET /components/{id}` — single component by uint64 ID (404 if not registered)
  - `GET /entities` — alive entity list; optional `?limit=N` (default 1000, max 10000; 400 if out of range)
  - `GET /entities/{id}` — entity detail: name, components, parent, prefabs, pairs (404 if dead)
  - `GET /snapshot` — full `World.MarshalJSON()` output
  - `PUT /snapshot` — load a snapshot into the world; 204 on success, 400 on parse error. **Warning**: replaces world state; not transactional.
- Routing via stdlib `http.ServeMux` with Go 1.22+ path patterns (`r.PathValue`). No external router dependency.

### Fixed

- `getViaIsA`, `hasViaIsA`, `PrefabOf`, `EachPrefab`, and `ParentOf` now return
  the zero value / false instead of panicking when called on entities whose
  archetype record has a `nil` table. Component entities allocated via
  `RegisterComponent` are not seated in the empty archetype, so their record's
  `Table` is `nil`; existing `EntityComponents` and `Get[T]` paths already
  guarded against this, but the five listed functions did not. The new REST
  endpoint `GET /entities/{component_id}` exposed this latent panic, which is
  now defensively avoided.

---

## v0.7.0 — 2026-05-11

Change detection on cached queries for delta-style systems. No breaking
changes.

### Added

- **Change detection via `CachedQuery.Changed()`** — `(*CachedQuery).Changed() bool`
  returns true when any matching table was mutated since the last call. The first call
  after construction always returns true (initial state is "all changed"). Changes detected:
  new matching table added to the cache; column write (`Set[T]`/`SetByID`/`SetPair[T]`/`SetPairByID`);
  structural change (entity added/removed via migrate). The change counter is a monotonic
  `uint64` on each `Table`; any column write marks the table dirty for all cached queries
  containing it (never under-reports, may over-report). The counter is incremented in
  `Table.Append`, `Table.RemoveSwap`, and a new `Table.BumpChange()` method called by the
  World after in-place column writes. NOT goroutine-safe. Change detection is
  cached-query-only; uncached `*Query` does not get `Changed()`.

---

## v0.6.0 — 2026-05-11

Completes the structured-term query API with OR support. No breaking
changes.

### Added

- **OR query terms** — `TermOr` (value 3) and the `Or(id)` constructor complete
  the structured-term API. Adjacent `Or` terms in a `NewQueryFromTerms` /
  `NewCachedQueryFromTerms` call form an OR-group; a table matches the group when
  it contains at least one of the group's IDs. Multiple OR-groups in one query are
  each independent. `FieldMaybe[T]` is extended to accept `TermOr` terms in
  addition to `TermOptional` — use it to disambiguate which members of an OR-group
  are present in the current table; `Field[T]` on an Or-group ID panics if the
  current table lacks it. Validation: `Or(0)` panics; duplicate IDs within an
  OR-group panic; cross-kind duplicate IDs panic (matching Phase 3.3 rules). The
  smallest-seed strategy and `CachedQuery` incremental cache maintenance are both
  Or-aware. `TermKind.String()` now returns `"Or"` for `TermOr`. Sort order for
  `TermsFull()` is: And, Not, Or-groups, Optional. No breaking changes.

---

## v0.5.0 — 2026-05-11

Stats and per-phase frame timing for tooling and observability. No breaking
changes; all existing public signatures are unchanged.

### Added

- **Stats and observability API** — `World.Stats()` returns a `Stats` snapshot
  with world-level counters (`EntityCount`, `TableCount`, `QueryCount`,
  `CachedQueryCount`, `SystemCount`, `FrameCount`, `Time`), per-phase wall-clock
  timing from the most recent `Progress` call (`LastFramePhases []PhaseStats`),
  and per-component table/entity counts (`ComponentStats []ComponentStat`).
  `PhaseStats` holds `Name`, `SystemCount`, and `Duration` for each of the four
  pipeline phases (PreUpdate[0], OnFixedUpdate[1], OnUpdate[2], PostUpdate[3]).
  `OnFixedUpdate` sums durations across all fixed-step iterations. Phases with no
  active systems report `Duration == 0`. `LastFramePhases` is nil until `Progress`
  is called at least once.
  `World.SystemCountInPhase(phase ID) int` is a convenience method for tooling;
  panics on non-built-in phase IDs (mirrors `NewSystemInPhase` validation).
  `QueryCount` is always 0 in this release (uncached queries are one-shot values
  the world does not track). No new third-party dependencies; stdlib `time` only.

---

## v0.4.0 — 2026-05-10

Complete JSON serialization: ChildOf hierarchies, IsA prefabs, and custom
pair components (data + tag-only) all round-trip. The v1 format is preserved
— all new fields are additive `omitempty`. No breaking changes.

### Added

- **Custom pair component serialization** — `World.MarshalJSON`
  now serializes custom pair components (non-ChildOf, non-IsA) into a `"pairs"`
  array on each entity. Tag-only pairs emit `{"rel":<serial>,"tgt":<serial>}`;
  data-bearing pairs add `"dataType"` (the base Go type's `reflect.Type.String()`)
  and `"data"`. `World.UnmarshalJSON` restores pairs after prefabs and before
  components: tag pairs via `AddID`, data pairs via the new `SetPairByID`.
  A new `(*World).SetPairByID(e, rel, tgt ID, v any)` method auto-registers
  the pair TypeInfo on first use and delegates to `SetByID`, firing
  hooks/observers and honoring the Defer queue. `component.RegisterPairDataByType`
  is the corresponding internal helper. ChildOf and IsA pairs continue to use
  the dedicated `parent`/`prefabs` fields and are not duplicated in `pairs`.
  v1 format unchanged (additive field). Coverage ≥ 96.4% (flecs), 100% (component).
- **IsA prefab serialization** — `World.MarshalJSON` now
  serializes IsA relationships as a `"prefabs"` array of serials (omitted when
  empty; v1 format unchanged — the field is additive). Topo-sort is generalized
  to a combined ChildOf+IsA predecessor graph so prefabs always appear before
  their instances. `World.UnmarshalJSON` restores IsA relationships after ChildOf
  and before components, preserving first-prefab-wins inheritance semantics.
  Cycle detection spans both edge kinds in a single DFS.
- **ChildOf hierarchy serialization** — `World.MarshalJSON` now
  serializes single-parent `(ChildOf, parent)` relationships as a `"parent"`
  serial field (omitted when absent; v1 format unchanged). Entities are emitted
  in topological order (parents before children) via DFS, with sibling order
  matching entity allocation order. `World.UnmarshalJSON` restores ChildOf
  relationships in a single sequential pass. Cycle detection returns a
  descriptive error rather than looping indefinitely.

---

## v0.3.0 — 2026-05-10

Introspection API, dynamic value access, and basic JSON serialization. No
breaking changes.

### Added

- **Introspection (meta) API** — `World.Components()`, `World.ComponentInfo(id)`,
  `World.EntityComponents(e)`, `World.EachEntity(fn)`, `World.AliveEntities()`.
  Public access to registered components and alive entities; no exposure of
  internal storage types.
- **Dynamic value access** — `World.GetByID(e, id) (any, bool)` and
  `World.SetByID(e, id, v any)` for component reads/writes when the type is
  only known at runtime. IsA inheritance-aware on Get; type-safety panic on
  Set with a mismatched value. Honors the Defer queue; fires hooks and
  observers like the typed paths.
- **JSON serialization** — `World.MarshalJSON()` and `World.UnmarshalJSON()`
  implement `json.Marshaler` / `json.Unmarshaler`. Saves and restores entities,
  non-pair components, and entity names. Built-in entities and pair components
  are skipped. Pair components, ChildOf hierarchies, and IsA prefabs will be
  added in subsequent 0.3.x or 0.4.x releases.

---

## v0.2.0 — 2026-05-10

Query extensions and traversal helpers. No breaking changes.

### Added

- **NOT and Optional query terms** — new structured `Term` API with
  `With(id)`, `Without(id)`, `Maybe(id)` constructors and
  `NewQueryFromTerms` / `NewCachedQueryFromTerms`. Use `FieldMaybe[T]` to
  access Optional-term columns with a presence flag. Legacy
  `NewQuery(w, ids...)` continues to produce AND-only queries with
  unchanged behavior.
- **Ancestor traversal helpers** — `GetUp[T]`, `HasUp`, `TargetUp` walk a
  relationship up from an entity and return the first match. Works for
  `ChildOf`, `IsA`, or any user-defined relationship. Cycle detection and
  64-level depth limit included. Zero allocation when the component is on
  the entity itself.

### Performance

- `BenchmarkGetUp_SelfHit`: 30 ns/op, 0 allocs/op.
- `BenchmarkGetUp_Depth1`/`Depth5`: 318/525 ns/op, 2 allocs/op (the seen-map for cycle detection).
- Optional-term presence cache is lazy-allocated; AND-only queries pay no overhead.

### Documentation

- `doc.go` extended with structured-term and traversal-helper examples.
- README feature index updated.

## v0.1.0 — 2026-05-10

Initial Go port of [flecs](https://github.com/SanderMertens/flecs). No breaking
changes from prior versions (this is the first public release).

### Added

- **Archetype-based storage** — structure-of-arrays tables keyed by sorted
  component-ID signatures; O(entity-count) iteration with no virtual dispatch.
- **Generic-typed API** — `Set[T]`, `Get[T]`, `Has[T]`, `Owns[T]`, `Remove[T]`,
  `RegisterComponent[T]`; full compile-time type safety, zero reflect at call sites.
- **Raw-ID API** — `AddID`, `RemoveID`, `HasID`, `OwnsID`, `SetPair[T]`,
  `GetPair[T]`, `MakePair`; tag and data-pair support.
- **Query API** — `NewQuery`, `NewCachedQuery`, `Field[T]`; ergonomic helpers
  `Each1` through `Each4`.
- **ChildOf hierarchy** — cascade delete; `EachChild`, `ParentOf`.
- **IsA inheritance** — transitive `Get`/`Has` on miss; copy-on-write `Set`;
  `PrefabOf`, `EachPrefab`.
- **Named entities** — `SetName`, `GetName`, `Lookup`, `LookupChild`, `PathOf`.
- **Lifecycle hooks** — `OnAdd[T]`, `OnSet[T]`, `OnRemove[T]` (one per type per event).
- **Observers** — `Observe[T]`, `ObserveID`, `Observe2[T]`; multiple subscribers
  per (component, event); deferred `Unsubscribe`.
- **Deferred commands** — `DeferBegin`/`DeferEnd`/`Defer`; nested scopes; safe
  structural mutation during iteration.
- **Systems + pipeline** — `NewSystem`, `NewSystemInPhase`; four built-in phases
  (PreUpdate, OnFixedUpdate, OnUpdate, PostUpdate); `Progress`; fixed-timestep
  accumulator; `Time`, `FrameCount`.
- Zero third-party dependencies (pure stdlib).
- >97% test coverage on the root package.

### Performance

- `Field[T]` zero-alloc fast path via `unsafe.Slice` over typed column memory.
- `unsafe.Slice` typed-slice view in queries; no `reflect.Value.Interface()` boxing.
- Observer dispatch with no per-fire snapshot allocation (deferred-removal at the node).
- Lazy `seen` map allocation in `Get[T]`/`Has[T]` IsA fallback; zero alloc on the
  common no-IsA path.
- Archetype migration zero-alloc on edge-cache hits (`migrate()` defers signature
  allocation until cache miss).
- Column logical-length tracking via internal counter; no `reflect.Value.Slice`
  allocation on `Append`/`RemoveSwap` hot paths.
- Benchmark baseline + before/after measurements captured in [BENCH.md](BENCH.md).
