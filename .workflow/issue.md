## Goal

Add C-style term traversal modifiers (`Self` / `Up(rel)` / `SelfUp(rel)` / `Cascade(rel)`) to `NewQueryFromTerms` and `NewCachedQueryFromTerms`. Today's `GetUp[T]` / `HasUp` / `TargetUp` helpers (see @scope.go and @traversal.go) walk a relationship at iteration time, but there is no way to express the traversal in the query itself — `With[Position](w).Up(IsA)` cannot be written. This phase ports C flecs's `EcsSelf` / `EcsUp` / `EcsCascade` term flags so a query can match entities that inherit a component through any traversable relationship.

ROADMAP's "Query extensions" section (`Query-term traversal modifiers (up(rel) inline in NewQueryFromTerms; the explicit GetUp/HasUp/TargetUp helpers cover most use cases)`) explicitly defers this work — see @ROADMAP.md line 41.

### What C does (research summary)

Traversal flags are bits packed into `ecs_term_ref_t::id` (the term's `src` ref). From `include/flecs.h` lines 736-796:

| Flag         | Bit       | Meaning                                                |
|--------------|-----------|--------------------------------------------------------|
| `EcsSelf`    | 1<<63     | Match on entity itself.                                |
| `EcsUp`      | 1<<62     | Match by walking the traversal relationship upwards.   |
| `EcsTrav`    | 1<<61     | Transitive traversal (not in scope — see non-goals).   |
| `EcsCascade` | 1<<60     | Sort matched tables breadth-first by depth.            |
| `EcsDesc`    | 1<<59     | Descending sort (not in scope).                        |

The relationship to traverse is stored separately in `ecs_term_t::trav` (default `IsA` per the term struct comment in flecs.h line 824-826; but the validator overrides this — see below).

**Validator defaults** (`src/query/validator.c` lines 760-826):
- If no traversal flags set + component has `OnInstantiate, Inherit` trait → auto-set `Self|Up` with `trav=IsA`.
- If no traversal flags set + component is not inheritable → set `Self` only.
- If `EcsUp` set without explicit `trav` → default `trav=ChildOf` (NOT IsA — important).
- `EcsCascade` implies `EcsUp`.

**Compiler dispatch** (`src/query/compiler/compiler_term.c` lines 1080-1187):
- `(src & traverse_flags) == EcsSelf` → `EcsQueryAnd` / `EcsQueryWith` (normal table match).
- `(src & traverse_flags) == EcsUp` → `EcsQueryUp` (must traverse up to find component).
- `(src & traverse_flags) == (EcsSelf|EcsUp)` → `EcsQuerySelfUp` (try self first, then up).
- `EcsCascade` is consumed by group_by, not the compiler dispatch (note the mask `traverse_flags = EcsTraverseFlags & ~(EcsCascade|EcsDesc)`).

**Up evaluator** (`src/query/engine/eval_up.c` lines 122-470):
- `flecs_query_up_select` iterates tables that have the component, then walks downwards through cached "down" entries to find descendants that reach the component via `trav`.
- `flecs_query_self_up_with` first checks if the entity's own table has the component (Self path); if not, falls through to `flecs_query_up_with` (Up path).
- Up matches set `it->sources[field_index] = <ancestor entity>` so the field accessor knows the data lives on someone else.

**Cascade** (`src/query/cache/cache.c` lines 175-189, `src/query/api.c` lines 75-94, 240-262):
- Cached queries with a Cascade term install a `group_by` callback `flecs_query_cache_group_by_cascade` that groups tables by `flecs_relation_depth(world, term->trav, table)`. Iteration order is by depth.
- **Cascade is rejected for uncached queries** — `src/query/api.c` line 246-261 errors with `"cascade is unsupported for uncached query"`. Our port should mirror this.
- `EcsCascade` causes `require_caching = true` in `ecs_query_init` (`api.c` line 79-88).

**Field source disambiguation** (`include/flecs.h` lines 5780-5952):
- `ecs_field_is_self(it, idx)` returns true when the field's data is owned by the iterated entity; false when matched via Up (data lives on an ancestor).
- `ecs_field_src(it, idx)` returns the source entity (0 for Self).
- When `ecs_field_is_self` is false, the field returns a single shared value rather than a per-row array — the documentation in flecs.h line 5780-5790 spells this out.

### How this maps to our port

Our `Term` struct (see @query.go lines 56-77) is a flat struct with package-level constructors `With(id)` / `Without(id)` / `Maybe(id)` / `Or(id)`. **Note:** the spec referenced `query_terms.go` but that file does not exist — the term builder API lives in @query.go. Tests are in @query_terms_test.go.

Our `*World.IsA()` and `*World.ChildOf()` are method calls (see @isa.go line 9, @childof.go line 10) — there is no package-level `flecs.ChildOf` constant. Builder APIs that take a `rel ID` parameter must be invoked with `w.ChildOf()` / `w.IsA()` at construction.

## Constraints

- @query.go — where `Term`, `TermKind`, `With`/`Without`/`Maybe`/`Or` constructors, `NewQueryFromTerms`, `Field[T]`, `FieldMaybe[T]`, and the seed-and-verify match loop live. The traversal additions go here. `Term` is a flat struct (lines 56-60) — extending it with traversal fields (e.g. `Trav ID`, `Flags TraverseFlags`) is the simplest mechanical change; alternative is a builder pattern returning `Term`. Decide during design but keep the `Term` value type cheap to copy (the Iter() hot loop uses `range q.terms`).
- @cached_query.go — `*CachedQuery` mirrors `*Query` matching and adds the `tables []*table.Table` pre-filter, change-detection, and `notifyTableCreated` hook (lines 62-72). Cascade ordering must live here: cached queries pay the depth-sort cost once at construction (and on `notifyTableCreated`). Per C's `api.c` line 246-261, **Cascade is rejected for uncached `NewQueryFromTerms`** — our port should panic with a parallel message if a `Cascade(...)` term is passed to `NewQueryFromTerms`.
- @query_terms_test.go — existing test style (Position/Velocity components, `w.Write(...)` for setup, `flecs.NewQueryFromTerms(w, flecs.With(posID))`, `q.Each(...)`). New tests follow this pattern.
- @traversal.go — `walkUp(w, e, rel, fn)` already implements self-first traversal with cycle detection, dead-target guard, and depth limit `maxTraversalDepth=64` (lines 26-70). The new query matcher should reuse `walkUp` for the per-table Up resolution — do not re-implement traversal walking.
- @scope.go — `GetUp[T](r *Reader, e, rel)` (line 453), `HasUp(r, e, id, rel)` (line 459), `TargetUp(r, e, id, rel)` (line 465) all sit behind the post-v0.15 scoped API. New traversal-aware field accessors must follow the same pattern (no `World.X` shortcuts).
- @isa.go and @childof.go — `*World.IsA()` (isa.go line 9) and `*World.ChildOf()` (childof.go line 10) are the entry points. The builder API should accept any `ID` (allowing custom traversable relationships in the future) but document IsA and ChildOf as the canonical choices.
- @doc.go — package overview. Add a "Traversal modifiers" section with a short prefab-inheritance example.
- @README.md — usage docs. Add a Cascade example for the OnUpdate-with-parent-first pattern (drives the public motivation).
- @ROADMAP.md — line 41 lists this as deferred. Move it to "Recent" / strike it from "Query extensions" when shipping.
- Non-goal: NO change to the default behaviour of `Each1`/`Each2`/`Each3`/`Each4` or the existing `NewQueryFromTerms` semantics. Traversal stays opt-in. C auto-enables `Self|Up` when a component has `OnInstantiate, Inherit` (validator.c line 766-770); we explicitly do NOT replicate that auto-enable in this phase — keep matching local-only by default.
- Non-goal: NO `EcsTrav` (transitive traversal — a different feature, not in our roadmap).
- Non-goal: NO `EcsDesc` (descending depth sort). C requires `EcsDesc` to be combined with `EcsCascade` (validator.c line 811-814). Skip both for now; only forward (root-first) cascade is in scope.
- Non-goal: NO change-detection invalidation when a prefab gains/loses a component AFTER a cached query is built. C handles this via the down-cache and component-record observers (see eval_up.c line 114-115 reference to `flecs_query_get_down_cache`); our port intentionally defers the observer wiring. Document the limitation in the GoDoc for `Up`/`SelfUp`/`Cascade`.
- Non-goal: NO new public surface on `*Reader` / `*Writer`. The new term builders and `FieldShared[T]` / `IsFieldSelf` accessors are top-level functions parameterized on `*QueryIter`, matching the existing `Field[T]` / `FieldMaybe[T]` pattern (@query.go lines 421, 461).

## Deliverables

1. **Extend the term builder API** in @query.go:
   - Decide on representation. Two viable shapes:
     - **Struct-fields:** add `Trav ID` and `Traverse TraverseFlags` fields to `Term`. Constructors stay package-level: `WithUp(id, rel ID) Term`, `WithSelfUp(id, rel ID) Term`, `WithCascade(id, rel ID) Term`.
     - **Builder methods on `Term`:** `With(id).Up(w.IsA())`, `With(id).SelfUp(w.ChildOf())`, `With(id).Cascade(w.ChildOf())`. Cleaner call site but requires `Term` methods to allocate or return value copies.
   - Recommendation: builder methods returning `Term` by value (no allocation, chainable). Document the API in this issue once a decision is made.
   - Define `TraverseFlags` (or equivalent enum) covering `TraverseSelf`, `TraverseUp`, `TraverseSelfUp`, `TraverseCascade`. Match the C semantic that Cascade implies Up.
   - `Self()` is a no-op alias for the current default; expose for symmetry and readability.
   - Default trav when omitted: follow C — `ChildOf` for Up (validator.c line 822-826), `IsA` only when explicitly requested or when the component opts in via a future inheritance trait (out of scope).

2. **Matcher extension** in @query.go and @cached_query.go:
   - Add a per-term "resolved source" cache: for each (query, table) pair, record whether the match was via Self or via Up, and if Up, which ancestor table provides the column data. For `*Query` this is computed per Iter() call; for `*CachedQuery` it is recorded alongside each entry in `tables` (extend the cache entry struct or add a parallel `sources []sourceInfo` slice).
   - During table matching for a `Up(rel)` term: if `tbl.HasComponent(termID)` is false, walk `rel` upwards from any entity in the table (use existing `walkUp` from @traversal.go) until an ancestor whose table contains `termID` is found. If none, the table does NOT match.
   - `SelfUp(rel)` matches if Self OR Up succeeds; Self takes precedence (matches C `flecs_query_self_up_with` in eval_up.c lines 395-442).
   - All entities in a single matched table share the same parent/prefab chain in the common case (because archetype identity captures `(rel, target)` pairs); this lets us cache the resolved source per-table rather than per-entity. Document this assumption — it holds for `IsA` always and for `ChildOf` when the parent is part of the table signature.

3. **Field accessors** for shared (Up-matched) columns:
   - Port `ecs_field_is_self` as `IsFieldSelf(it *QueryIter, id ID) bool` (true when the current table owns the column; false when matched via Up).
   - Add `FieldShared[T](it *QueryIter, id ID) (T, bool)` returning a single value (not a slice) when the term was matched via Up. The boolean is false when the term was matched via Self (caller should use `Field[T]` instead) or when the column is absent.
   - Decide: keep `Field[T]` strict (panic on non-Self) and force callers to disambiguate via `IsFieldSelf`, OR extend `Field[T]` to return a length-1 broadcast slice for shared columns. Recommendation: keep `Field[T]` strict — the C semantic is that the array-vs-pointer distinction is explicit (flecs.h line 5780-5790), and silent broadcasting hides allocation/aliasing surprises.
   - Document the shared-value lifetime: the returned `T` is a copy of the ancestor's column entry, valid only until the next `it.Next()` (consistent with `Field[T]` aliasing rules at @query.go line 398-401).

4. **Cascade ordering** (cached only):
   - At `NewCachedQueryFromTerms` time, after the initial table population, sort `tables` by depth using the `Cascade` term's `trav`. Compute depth via a BFS from any root entity (entity with no `(trav, *)` pair), or by walking up from each table's representative entity until no parent exists.
   - On `notifyTableCreated` (see @cached_query.go line 67), insert the new table at its correct depth position (binary-search by depth, O(log N + N) for the shift).
   - **Reject Cascade in `NewQueryFromTerms`** with a clear panic message matching C's error string (api.c line 246-261). The panic message should say "cascade requires a cached query; use NewCachedQueryFromTerms".

5. **Tests** in @query_terms_test.go (existing test style with `flecs.New()`, `w.Write(...)`, `q.Each(...)`):
   - `TestQueryUp_MatchesViaPrefab` — entity has `(IsA, prefab)`, prefab has Position. `With(posID).Up(w.IsA())` matches the entity; `FieldShared[Position]` returns prefab's value; `IsFieldSelf` is false.
   - `TestQueryUp_MatchesViaChildOf` — child→mid→grandparent ChildOf chain; grandparent has `Marker`. `With(markerID).Up(w.ChildOf())` matches the child; resolved source is grandparent.
   - `TestQuerySelfUp_PrefersSelf` — entity has Position locally AND inherits via IsA. `SelfUp(w.IsA())` matches via Self; `Field[Position]` returns the local slice; `IsFieldSelf` is true.
   - `TestQuerySelfUp_FallsBackToUp` — entity has no Position locally but inherits; SelfUp matches via Up.
   - `TestQueryUp_NoMatch` — entity with no ancestor having the component → no match (the table is filtered out).
   - `TestQueryUp_DeadAncestor` — ancestor entity is deleted between query construction and iteration; matcher behaviour matches existing `walkUp` dead-target semantics (the chain terminates cleanly without panic).
   - `TestQueryUp_CycleSafety` — pathological cycle via IsA; matcher terminates within `maxTraversalDepth`.
   - `TestQueryCascade_OrdersByDepth` — three-level ChildOf chain (root, mid, leaf), each with `Marker`. `NewCachedQueryFromTerms(w, With(markerID).Cascade(w.ChildOf()))` iterates in depth order: root, then mid, then leaf.
   - `TestQueryCascade_RejectedForUncached` — `NewQueryFromTerms(w, With(markerID).Cascade(w.ChildOf()))` panics with the C-parallel error message.
   - `TestCachedQueryUp_MatchesViaPrefab` — same as the first test but with `NewCachedQueryFromTerms`.
   - `TestCachedQueryUp_NewTableTriggersRematch` — create a query, then create a new entity-via-prefab that lands in a brand-new table; verify the cached query picks it up via `notifyTableCreated` (existing hook in cached_query.go line 67).
   - `TestFieldShared_PanicWhenSelf` — calling `FieldShared` on a Self-matched term returns (zero, false) (or panics — pick one and stick to it).
   - `TestField_PanicWhenShared` — calling `Field[T]` on an Up-matched term panics with a clear message directing callers to `FieldShared`.

6. **Benchmarks** in @bench_test.go:
   - `BenchmarkQueryUpTraversal` — N entities each inheriting Position from a single prefab; iterate a `Up(IsA)` query and read `FieldShared[Position]`. Document the per-table cost of the up-walk.
   - Re-run `BenchmarkEach1`, `BenchmarkQueryIter`, `BenchmarkCachedQuery` to confirm no regression for queries that don't use Up/Cascade.

7. **Doc updates** in @doc.go and @README.md:
   - New section "Query traversal modifiers" in @doc.go with a 10-line example: prefab + child entity + `With(...).Up(w.IsA())` query.
   - README adds a "Cascade for parent-before-child systems" example showing the OnUpdate use case.

8. **ROADMAP** in @ROADMAP.md:
   - Move the "Query-term traversal modifiers" bullet from "Query extensions" (line 41) into a new "Recent" entry under the appropriate version.

## C-isms we deliberately skip

- **`EcsTrav` (transitive relationships).** Not on our roadmap; would require pair-target chasing distinct from up-traversal.
- **`EcsDesc` (descending depth sort).** Combined with Cascade; rarely used; out of scope.
- **Auto-enable `Self|Up` for inheritable components** (validator.c line 766-770). This is C's "if a component has OnInstantiate=Inherit, queries match inherited copies by default" behaviour. Our port keeps inheritance opt-in via the explicit builder API to preserve the current `Each1`/`Each2` semantics. A separate future phase can revisit if user feedback demands it.
- **Down-cache observers for prefab/parent mutations.** C maintains a "down" cache invalidated by component-record observers (eval_up.c line 114-115); we defer this and document that prefabs/parents gaining/losing the component AFTER cache construction may not be reflected until the cache is rebuilt.
- **`EcsQueryTreeUpPost` / `EcsQueryTreeUpPre` / sparse-up variants.** C has specialized dispatchers for ChildOf-with-cache and sparse components (compiler_term.c lines 1122-1137); our matcher uses a single up-walk code path that handles both via `walkUp`. Performance follow-up if profiling demands it.
- **`flecs_relation_depth` cache.** C precomputes relationship depths for cascade group_by (cache.c line 187). For our first cut, compute depth on-demand at cached-query construction. If iteration cost dominates, add a per-world depth cache later.

## Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run ./...` clean.
- `go test ./... -race -count=3 -timeout=180s` passes.
- Coverage on `github.com/snichols/flecs` >= 95%.
- Existing query benchmarks (`BenchmarkEach1`, `BenchmarkQueryIter`, `BenchmarkCachedQuery`) show no regression (within 5%) for queries that don't use Up/Cascade.
- New benchmark `BenchmarkQueryUpTraversal` exists and documents the cost of the up-walk relative to a flat-table query.
- @README.md and @doc.go gain examples for both Up and Cascade.
- @ROADMAP.md is updated to reflect the shipped capability.

