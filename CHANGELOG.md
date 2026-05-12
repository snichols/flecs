# Changelog

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
