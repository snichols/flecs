## Goal

Complete the `iter.Seq` adoption story started in Phase 16.52 (v0.107.0, queries). Phase 16.52 added range-over-func adapters for `Query`/`CachedQuery` (`QueryAll`, `CachedQueryAll`, `All1`–`All4`, ctx variants) in `@/work/agents/claude/projects/flecs/query_iter_seq.go`. This phase covers the **remaining non-query traversal primitives**: the standalone `Each*` iteration methods on `Reader`/`World` and the package-level `Each*` traversal functions.

Target version: **v0.109.0**. Phase number: **16.54**. This is a pure-additive enhancement (no deprecations) that completes the idiomatic-iteration narrative: queries in 16.52, traversal here.

### Verified inventory (signatures + file:line confirmed against codebase)

The codebase was grepped; the actual set differs from initial assumptions in important ways. Adapters must mirror the **verified** shapes below, not the assumed ones.

**Early-exit supported (callback returns `bool`) — wrap directly, returning `!yield(...)` continuation:**

| Method | Location | Verified signature |
|---|---|---|
| `(*Reader).EachEntity` | `@/work/agents/claude/projects/flecs/scope.go:300` | `func (r *Reader) EachEntity(fn func(e ID) bool)` |
| `(*Reader).EachChild` | `@/work/agents/claude/projects/flecs/scope.go:195` | `func (r *Reader) EachChild(parent ID, fn func(ID) bool)` |
| `(*Reader).EachPrefab` | `@/work/agents/claude/projects/flecs/scope.go:225` | `func (r *Reader) EachPrefab(e ID, fn func(prefab ID) bool)` |
| `(*Reader).EachSystem` | `@/work/agents/claude/projects/flecs/scope.go:367` | `func (r *Reader) EachSystem(phase *Phase, fn func(*System) bool)` — **takes `*Phase`, NOT `phase ID`** |
| `(*World).EachEntity` | `@/work/agents/claude/projects/flecs/meta.go:102` | `func (w *World) EachEntity(fn func(e ID) bool)` |
| `(*World).EachChild` | `@/work/agents/claude/projects/flecs/childof.go:25` | `func (w *World) EachChild(parent ID, fn func(child ID) bool)` |
| `(*World).EachPrefab` | `@/work/agents/claude/projects/flecs/isa.go:56` | `func (w *World) EachPrefab(e ID, fn func(prefab ID) bool)` |

**No early-exit (callback has NO `bool` return) AND yields TWO values (not bare IDs):**

| Function | Location | Verified signature |
|---|---|---|
| `EachUnion` (package-level) | `@/work/agents/claude/projects/flecs/union.go:93` | `func EachUnion(s scope, relID ID, fn func(e ID, target ID))` |
| `EachSparse` (package-level, **generic**) | `@/work/agents/claude/projects/flecs/sparse.go:248` | `func EachSparse[T any](s scope, fn func(e ID, v *T))` |
| `EachByID` (package-level) | `@/work/agents/claude/projects/flecs/component_dynamic.go:135` | `func EachByID(s scope, componentID ID, fn func(e ID, ptr unsafe.Pointer))` |

**Corrections vs. task assumptions (load-bearing):**
- `EachSystem` takes `*Phase`, not `phase ID`. The adapter signature is `Systems(phase *Phase)`.
- `EachUnion`/`EachSparse`/`EachByID` yield **(entity, payload)** pairs, not bare IDs, and their callbacks have **no `bool` return** (no early-exit hook). Adapters for these MUST be `iter.Seq2` and MUST drive the underlying snapshot/iteration machinery directly to support `break` (mirrors the Phase 16.52 finding that `Query.Each` could not be reused for break support).
- `EachSparse` is generic over `T`; its adapter is generic: `Sparse[T any](s scope) iter.Seq2[ID, *T]`.
- `(*Reader).Components()` / `(*Reader).AliveEntities()` / `(*Reader).Phases()` / `(*Reader).SystemsInPhase()` already return fresh slices — **no adapter needed (skip).**
- `EachTableFor` (`scope.go:71`, `world.go:1943`) iterates `*table.Table` (internal storage type) — **out of scope; not a public traversal primitive over IDs/entities.**
- No naming collisions: `Entities`, `Children`, `Prefabs`, `Systems`, `Union`, `Sparse`, `ByID` are all free as method/function names (verified via grep).

### API additions (finalized names — no collisions)

Mirror the Phase 16.52 file structure: new file `@/work/agents/claude/projects/flecs/iter_seq_traversal.go` (sibling to `query_iter_seq.go`).

| Callback method | New iter adapter | Shape |
|---|---|---|
| `(*Reader).EachEntity` | `(*Reader).Entities() iter.Seq[ID]` | `iter.Seq[ID]` |
| `(*Reader).EachChild` | `(*Reader).Children(parent ID) iter.Seq[ID]` | `iter.Seq[ID]` |
| `(*Reader).EachPrefab` | `(*Reader).Prefabs(e ID) iter.Seq[ID]` | `iter.Seq[ID]` |
| `(*Reader).EachSystem` | `(*Reader).Systems(phase *Phase) iter.Seq[*System]` | `iter.Seq[*System]` |
| `EachUnion` | `Union(s scope, relID ID) iter.Seq2[ID, ID]` | `iter.Seq2[ID, ID]` (entity, target) |
| `EachSparse[T]` | `Sparse[T any](s scope) iter.Seq2[ID, *T]` | `iter.Seq2[ID, *T]` (generic) |
| `EachByID` | `ByID(s scope, componentID ID) iter.Seq2[ID, unsafe.Pointer]` | `iter.Seq2[ID, unsafe.Pointer]` |

Notes on shape decisions:
- `Reader`-only for the method-style adapters (consistent with `query_iter_seq.go` which is scope/Reader oriented). `World.Each*` variants are covered transitively because `Reader` wraps the world; no separate `World.Entities()` adapter (avoid surface bloat — document that `w.Read(func(r){ for e := range r.Entities() {...} })` is the idiom).
- `Union`/`Sparse`/`ByID` keep their package-level + `scope` shape to exactly mirror `EachUnion`/`EachSparse`/`EachByID` (consistent with `All1`–`All4` which are also package-level `scope`-taking).
- Bare-ID traversals are `iter.Seq[ID]`; payload-bearing traversals are `iter.Seq2` yielding the same second value as their `Each*` source — this honors the non-goal "iter.Seq2 with component values is the query API's job" only for *query* traversal; `EachSparse`/`EachByID`/`EachUnion` are payload-bearing primitives whose existing callbacks already pass a second value, so the faithful adapter must preserve it.

### Context-aware variants

Add ctx variants only where iteration is inherently unbounded:
- `(*Reader).EntitiesContext(ctx context.Context) iter.Seq2[ID, error]` — world entity count can be huge (10k+). Yields `(0, ctx.Err())` once on cancellation and stops; checks ctx every `ctxCheckInterval` (=1024, defined `@/work/agents/claude/projects/flecs/query.go:1815`) entities, matching the cadence of `(*Query).EachContext`.
- `ByIDContext(ctx context.Context, s scope, componentID ID) iter.Seq2[ID, error]` — holder set can be large; same cadence. (Note: collapses the payload — ctx variant yields `(id, err)` only, consistent with `QueryAllContext`'s `iter.Seq2[ID, error]` precedent. Document this.)
- **Skip ctx variants** for `Children` (children of one parent — bounded), `Prefabs` (IsA targets of one entity — tiny), `Systems` (systems in one phase — small, bounded by registered system count), `Union` (membership of one relationship — typically small), `Sparse` (generic; if needed callers can wrap — keep surface minimal). Document the rationale inline in the issue/code: "ctx variant omitted — population bounded by a single entity/phase/relationship; cancellation latency is negligible."

### Early-exit / break semantics

Same contract as Phase 16.52: when the user `break`s, `yield` returns false, the adapter MUST stop immediately and MUST NOT visit further entities/tables.

- For `bool`-returning callbacks (`EachEntity`, `EachChild`, `EachPrefab`, `EachSystem`): the existing callback already threads a `bool`; wrap directly — pass `func(x) bool { return yield(x) }` to the existing `Each*` method. Break support is inherited for free. (Confirm `EachChild`'s OrderedChildren snapshot path and archetype path both honor the `false` return — verified at `scope.go:200-216`: both do.)
- For no-`bool` callbacks (`EachUnion`, `EachSparse`, `EachByID`): the existing callback CANNOT signal stop. The adapter MUST drive the underlying machinery directly (snapshot the dense store / iterate `compIndex.Each` tables) and check `yield`'s return at every element, mirroring the Phase 16.52 precedent (`query_iter_seq.go` drives `q.Iter()`/`it.Next()` directly rather than reusing `Query.Each`). Re-implementing the *snapshot* logic is acceptable here precisely because the existing callbacks expose no stop hook — this is the documented Phase 16.52 pattern, NOT a non-goal violation (the non-goal forbids re-implementing *iteration logic* gratuitously; here it is required for break support).

### Snapshot / mutation semantics

`EachEntity` iterates `index.EachID` over `dense[1:aliveCount]` with behavior undefined if fn mutates during iteration (verified `entityindex.go:304-313`). `EachChild`/`EachUnion`/`EachSparse` snapshot before iterating (mutation-safe). Adapters must preserve identical semantics — the `Entities()` adapter inherits the "undefined under mutation" contract of `EachEntity`; the snapshotting adapters inherit snapshot-safety. The `TestSeq_Entities_DuringMutation_ReadScope` test asserts the adapter's behavior is *consistent with* `EachEntity` (snapshot-equivalence under a Read scope), not stronger.

## Constraints

- @/work/agents/claude/projects/flecs/query_iter_seq.go — canonical Phase 16.52 pattern to mirror exactly: per-element `if !yield(x) { return }`, ctx variants yield `(0, ctx.Err())` once then stop, ctx checked every `ctxCheckInterval`, package-level `scope`-taking signatures for payload adapters. New file `iter_seq_traversal.go` mirrors this structure.
- @/work/agents/claude/projects/flecs/scope.go — `(*Reader).EachEntity`/`EachChild`/`EachPrefab`/`EachSystem` source methods; new method adapters live as `Reader` methods here or in the new file (place adapters in `iter_seq_traversal.go` for structural parity with 16.52).
- @/work/agents/claude/projects/flecs/union.go — `EachUnion` at :93; adapter must snapshot `w.unionStore[...].dense` directly for break support (no bool hook in callback).
- @/work/agents/claude/projects/flecs/sparse.go — `EachSparse[T]` at :248; generic adapter `Sparse[T]`; snapshot `w.sparseStorage[...].dense`.
- @/work/agents/claude/projects/flecs/component_dynamic.go — `EachByID` at :135; adapter must handle BOTH the sparse/dont-fragment dense path AND the `compIndex.Each` archetype path, checking `yield` at every element and respecting `w.index.IsAlive` filtering exactly as the source does; preserve `w.checkExclusiveAccessRead()`.
- @/work/agents/claude/projects/flecs/query.go — `ctxCheckInterval = 1024` at :1815; reuse this constant for ctx-variant cadence (do not redefine).
- @/work/agents/claude/projects/flecs/internal/storage/entityindex/entityindex.go — `EachID` at :307 is the entity iteration machinery; the `Entities()` adapter wraps `(*Reader).EachEntity` (which wraps `index.EachID`); `EntitiesContext` drives `EachID` with a periodic ctx check (cannot reuse plain `EachID` for ctx — drive it via the `bool` return to break on ctx cancel).
- @/work/agents/claude/projects/flecs/CHANGELOG.md — add a `## v0.109.0 — Phase 16.54` entry at the top, matching the structure of the v0.107.0/v0.108.0 entries (What changed / New API / Design notes / New tests / New runnable example / Documentation / Breaking changes: None).
- @/work/agents/claude/projects/flecs/ROADMAP.md — `## Shipped` header is `## Shipped (through v0.108.0)` at :3; bump to `(through v0.109.0)` and add the Phase 16.54 line in the same bulleted style as the v0.107.0 range-over-func entry at :8. Update the "All ROADMAP items shipped" closing note at :113 if it references the final phase.
- @/work/agents/claude/projects/flecs/README.md — feature table has a "Range-over-func iteration _(v0.107.0)_" row at :280; add a sibling row (or extend) for traversal adapters _(v0.109.0)_ linking to the relevant docs.
- @/work/agents/claude/projects/flecs/doc.go — package-doc range-over-func section at :46-56 mentions `All1`–`All4`/`QueryAll`/`CachedQueryAll`; extend it to mention the traversal adapters (`Entities`/`Children`/`Prefabs`/`Systems`/`Union`/`Sparse`/`ByID`) with a short example.
- @/work/agents/claude/projects/flecs/docs/HierarchiesManual.md — add `Children`/`Prefabs` range-over-func examples.
- @/work/agents/claude/projects/flecs/docs/EntitiesComponents.md — add `Entities`/`Sparse`/`ByID` range-over-func examples.
- @/work/agents/claude/projects/flecs/docs/Systems.md — add `Systems(phase)` range-over-func example.
- @/work/agents/claude/projects/flecs/docs/ComponentTraits.md — add `Union` range-over-func example.
- Mechanical acceptance (project baseline, non-negotiable): `go vet ./...` clean; `golangci-lint run ./...` clean; `go test ./... -race -count=3` clean; coverage ≥ 95% (current baseline); all existing `Each*` tests pass unchanged (pure addition, no signature changes).
- Compile-time interface assertions required, e.g. `var _ iter.Seq[ID] = (&Reader{}).Entities` is not directly expressible (method value needs a receiver) — use the established pattern: assert the return type at a call site in a test, e.g. `var _ iter.Seq[ID] = r.Entities()` and `var _ iter.Seq2[ID, error] = r.EntitiesContext(ctx)`, mirroring how 16.52 validates `QueryAll`.

## Required tests

New file `@/work/agents/claude/projects/flecs/iter_seq_traversal_test.go`. New runnable examples in `@/work/agents/claude/projects/flecs/iter_seq_traversal_example_test.go`.

For each adapter (`Entities`, `Children`, `Prefabs`, `Systems`, `Union`, `Sparse`, `ByID`):
- `TestSeq_<Name>_YieldsAll` — populated case yields exactly the expected set.
- `TestSeq_<Name>_BreakHonored` — `break` after N; assert exactly N yielded and no further underlying work (for the no-bool-callback adapters, this is the critical regression: prove the direct-machinery drive actually stops).
- `TestSeq_<Name>_Empty` — empty case; assert the range body never executes.
- `TestSeq_<Name>_MatchesEachX` — golden equivalence: collect via the adapter and via the existing `EachX` callback; assert identical set AND identical order.

Specific scenarios:
- `TestSeq_Entities_LargeWorld` — 10k entities; assert count.
- `TestSeq_Children_OrderedChildren` — parent with `OrderedChildren` trait; assert insertion order preserved (covers the `scope.go:197` snapshot path).
- `TestSeq_Children_Unordered` — parent without OrderedChildren; assert archetype-path equivalence with `EachChild`.
- `TestSeq_Prefabs_MultiLevel` — IsA chain A→B→C; assert `Prefabs(C)` yields only the direct prefab (matches `EachPrefab` DIRECT-only semantics; not transitive).
- `TestSeq_Systems_InPhase` — multiple systems in a phase; assert topological/registration order matches `EachSystem`; include a disabled system (must still be yielded).
- `TestSeq_Systems_NilPhasePanics` — `Systems(nil)` panics consistent with `EachSystem(nil, ...)`.
- `TestSeq_Union_Members` — union relationship; assert `(entity, target)` pairs yielded in insertion order matching `EachUnion`.
- `TestSeq_Sparse_Holders` — sparse component holders; assert `(entity, *T)` pairs, dense insertion order.
- `TestSeq_ByID_DynamicComponent` — dynamic (non-typed) component holders; assert `(entity, unsafe.Pointer)` and that dead entities are filtered (mirrors `component_dynamic.go:158` `IsAlive` guard).
- `TestSeq_ByID_SparseComponent` — `ByID` over a Sparse-policy component (covers the dense-store branch at `component_dynamic.go:144-153`).
- `TestSeq_EntitiesContext_Canceled` — cancel ctx mid-iteration; assert final yield is `(0, ctx.Err())` and iteration stops.
- `TestSeq_EntitiesContext_PreCanceled` — ctx already done before first yield; assert `(0, ctx.Err())` yielded once, body sees error immediately.
- `TestSeq_ByIDContext_Canceled` — same for `ByIDContext`.
- `TestSeq_Entities_DuringMutation_ReadScope` — iterate `Entities()` inside `w.Read(...)`; assert behavior is consistent with `EachEntity` under the same scope (snapshot-equivalence; do NOT assert a stronger guarantee than `EachEntity` provides).
- `BenchmarkEachEntity_vs_Entities` — adapter overhead within 5% of the raw `EachEntity` callback.
- Compile-time assertions in-test: `var _ iter.Seq[ID] = r.Entities()`, `var _ iter.Seq[ID] = r.Children(p)`, `var _ iter.Seq[ID] = r.Prefabs(e)`, `var _ iter.Seq[*System] = r.Systems(ph)`, `var _ iter.Seq2[ID, ID] = Union(r, rel)`, `var _ iter.Seq2[ID, *C] = Sparse[C](r)`, `var _ iter.Seq2[ID, unsafe.Pointer] = ByID(r, cid)`, `var _ iter.Seq2[ID, error] = r.EntitiesContext(ctx)`, `var _ iter.Seq2[ID, error] = ByIDContext(ctx, r, cid)`.

Runnable examples (godoc):
- `ExampleReader_Entities`, `ExampleReader_Children`, `ExampleReader_Prefabs`, `ExampleReader_Systems`, `ExampleUnion`, `ExampleSparse`, `ExampleByID` — each a minimal `w.Read(func(r){ for ... := range ... {} })` with deterministic `// Output:`.

### Back-compat
All existing `Each*` tests pass unchanged — these are pure additions; no existing signature is touched.

## Non-goals

- Re-implementing iteration logic gratuitously — adapters for `bool`-callback methods wrap the existing `Each*` directly. Direct machinery drive is used ONLY for the three no-bool-callback functions where break support is otherwise impossible (this is the documented Phase 16.52 pattern, not a violation).
- ctx variants for inherently-small populations (`Children`/`Prefabs`/`Systems`/`Union`/`Sparse`) — only `Entities`/`ByID` get ctx variants.
- iter.Pull / manual-advance API.
- Deprecating or modifying any `Each*` method — pure additions only.
- Separate `World.Entities()`/`World.Children()` method adapters — the `Reader`-scoped adapters plus `w.Read(...)` are the documented idiom; avoid surface duplication.
- `EachTableFor` adapter — iterates internal `*table.Table`, not a public ID/entity traversal primitive.
- Bare-ID-only stripping of payload from `Union`/`Sparse`/`ByID` — these primitives are payload-bearing; the faithful adapter yields the same second value the existing callback passes (their non-ctx forms are `iter.Seq2`).
