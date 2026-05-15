## Goal

Add **opt-in non-fragmenting storage** for parent relationships (typically `ChildOf`). Currently `(ChildOf, P)` joins the archetype signature, so children of different parents with otherwise identical components end up in distinct tables — a world with N parents × M children of the same type fragments into N tables of M entities, when it could be 1 table of N×M with the parent stored as a per-row column.

This is the upstream "parent hierarchy storage" feature (see `flecs/src/storage/parent.c`, `flecs/include/flecs.h` `EcsParent` / `flecs_parent_index`, and `flecs/docs/Relationships.md` non-fragmenting hierarchy section). It is the **last remaining major upstream feature** on Go-flecs's gap list — verify against @docs/README.md line 131 that the framing still matches before flipping it to shipped.

**Headline performance win:** reparenting becomes O(1) (column write + cache invalidation) instead of O(component-count) full archetype migration.

**Target version:** v0.102.0. Phase number: 16.47. Expect ~2000 LOC — the largest single phase since Phase 12.x.

### API

- `SetParentStorage(w *World, relID ID)` — declare relationship uses parent storage. Default OFF (current fragmenting behavior). Fail-loud if entities already carry the relationship.
- `IsParentStorage(s scope, relID ID) bool` — predicate.
- Preconditions: relationship must be a relationship (per `SetRelationship`); must be Exclusive (one parent per entity). Panic on violation.
- `(*World).ChildOf()` already exists; opt-in via `SetParentStorage(w, w.ChildOf())`.
- All existing APIs (`EachChild`, `ParentOf`, `GetUp`, `HasUp`, `TargetUp`, `PathOf`, `Lookup`, hierarchical queries) **MUST behave identically** whether parent-storage is enabled or disabled. This back-compat surface is the largest risk.

### Storage shape

New per-table column `parentColumn { relID ID; parents []ID }` parallel to component columns. Present iff (a) relationship is in parent-storage mode and (b) at least one entity in the table has a `(relID, *)` pair.

Archetype-signature transformation when parent-storage is active for `relID`:

- `{Position, (relID, p1)}` and `{Position, (relID, p2)}` collapse to signature `{Position, (relID, EcsAny)}` (or sentinel marker); per-row parent stored in parentColumn.
- The marker tells query construction "consult parentColumn for the target."

### Query integration

- `WithPair(rel, p1)` (specific target) → match tables with marker, then per-row filter `parentColumn[row] == p1`.
- `WithPair(rel, *)` (wildcard) → match tables with marker, no per-row filter.
- `WithPairTgtVar(rel, "v")` → bind variable to `parentColumn[row]` per row.
- `Position(ChildOf:parent)` (Phase 16.18 fixed-source + parent traversal) → look up parent via `parentColumn[row]`, read parent's Position.
- Up/Cascade traversal walks `parentColumn[row] → parent's table.parentColumn[row]` instead of pair-index lookup. Cycle-safe; bounded depth (match existing `GetUp` behavior).

### Hooks / observers

On `AddID` / `Set` of `(relID, target)`:

- Parent-storage ON: in-place column write — **no archetype migration**. Fire OnAdd (or OnReplace on overwrite), OnSet.
- Parent-storage OFF: existing migration path.

OnAdd/OnRemove/OnSet/OnReplace observers for `(relID, *)` must still fire correctly. OnTableCreate fires on first child only; OnTableFill / OnTableEmpty fire on first / last child transitions of the table.

### Cleanup policies

Iterate over parent columns (scan) instead of pair-target indices when applying `OnDeleteTarget`. See @cleanup.go. Verify behavior for `DeleteAction`, `RemoveAction`, `PanicAction`.

### OrderedChildren

Keep the existing Phase 15.x side-map for v1 (simpler, backward-compat). Note "augment parentColumn with sibling-rank uint32" as a future optimization. Do not migrate in this phase.

### Snapshot

- Binary snapshot (@snapshot.go) serializes parent columns alongside component columns.
- JSON snapshot (@marshal.go) persists the parent-storage flag (per relationship) and per-entity parent.
- Round-trip preserves everything (covered by tests below).

## Constraints

- @docs/README.md — line 131 frames parent hierarchy storage as "not yet ported in Go flecs" — verified still accurate; flip to shipped in v0.102.0. Line 109 stale reference to "Join-order optimization deferred to Phase 16.27" should be updated to "shipped in v0.99.0 (Phase 16.44)" while in the area.
- @docs/Relationships.md — relationship traits documentation; add a parent-storage note as a relationship-level option.
- @docs/HierarchiesManual.md — primary hierarchy reference; add a major section cross-linking to a new `ParentStorage.md`, with before/after table-fragmentation comparison.
- @docs/ParentStorage.md — new file: motivation, API, performance characteristics, when to enable, limitations.
- @CHANGELOG.md — add v0.102.0 entry.
- @ROADMAP.md — add Phase 16.47 row; bump heading; mark "all major gaps closed" if appropriate (this is the last item).
- @README.md — update Hierarchies feature row.
- @internal/storage/table/table.go — Table struct gets `parentColumn` field; verify columnar layout assumptions hold.
- @internal/storage/componentindex/componentindex.go — component-to-tables index; parent-storage tables may need a parallel index path.
- @world.go — archetype management and pair handling are the primary integration site for signature transformation.
- @query.go — pair-matching logic; teach query construction about the parent-storage marker (exact target → per-row filter; wildcard → no filter; variable → bind per-row).
- @cached_query.go — same marker awareness; ensure cache invalidation on parent-column writes.
- @childof.go — where ChildOf is wired; SetParentStorage hooks in here.
- @traversal.go — `GetUp` / `HasUp` / `TargetUp` walk parent column when storage active.
- @cleanup.go — cleanup-policy scan iterates parent columns.
- @snapshot.go — binary serialization of parent columns.
- @marshal.go — JSON serialization of parent-storage flag and parent IDs.
- @observer.go and @observer_table.go — observer dispatch on parent column writes (OnAdd / OnRemove / OnSet / OnReplace / OnTableCreate / OnTableFill / OnTableEmpty).

### Required tests in @parent_storage_test.go (new file)

**API**
- `TestParentStorage_SetGet` — `SetParentStorage(w, ChildOf)` then `IsParentStorage` → true.
- `TestParentStorage_DefaultDisabled` — fresh world: `IsParentStorage(ChildOf)` → false.
- `TestParentStorage_OnlyAcceptsRelationships` — non-relationship → panic.
- `TestParentStorage_IsExclusiveRequired` — non-exclusive relationship → panic.

**Storage shape**
- `TestParentStorage_SingleTable_MultipleParents` — 100 entities `{Position, (ChildOf, pᵢ)}` for distinct pᵢ → single archetype table holding all 100.
- `TestParentStorage_ParentColumnPopulated` — verify the parent column contains the right parent IDs.
- `TestParentStorage_NoFragmentationOnRepartent` — change parent; assert no table migration (counter/hook).

**Query integration**
- `TestParentStorage_PairExactQuery` — `WithPair(ChildOf, p1)` returns only p1's children.
- `TestParentStorage_PairWildcardQuery` — `WithPair(ChildOf, *)` returns all children.
- `TestParentStorage_PairVariableQuery` — `WithPairTgtVar(ChildOf, "parent")` binds var per row.
- `TestParentStorage_TraversalUp` — `(ChildOf, root).Up` traversal works.
- `TestParentStorage_Cascade` — Cascade orders by depth.
- `TestParentStorage_GetUp` — `GetUp[Position]` walks parent columns to ancestor.

**Reparenting performance**
- `BenchmarkParentStorage_Reparent_FullArchetype` — reparent entity with 8 components. ON vs OFF; expect ≥4× speedup.

**Back-compat**
- `TestParentStorage_EachChild_BackCompat`
- `TestParentStorage_ParentOf_BackCompat`
- `TestParentStorage_HasUp_TargetUp_BackCompat`
- `TestParentStorage_PathOf_BackCompat`
- `TestParentStorage_Lookup_BackCompat`

**Cleanup policies**
- `TestParentStorage_OnDeleteTargetDelete`
- `TestParentStorage_OnDeleteTargetRemove`
- `TestParentStorage_OnDeleteTargetPanic`

**Snapshot round-trip**
- `TestParentStorage_JSON_RoundTrip`
- `TestParentStorage_Snapshot_RoundTrip`

**Observers**
- `TestParentStorage_OnAddFires`
- `TestParentStorage_OnRemoveFires`
- `TestParentStorage_OnReplaceFires` — old/new payload.
- `TestParentStorage_OnTableCreate_Once` — first child only.
- `TestParentStorage_OnTableFill_OnFirstChild`
- `TestParentStorage_OnTableEmpty_OnLastChild`

**Reclamation**
- `TestParentStorage_TableReclaimedAfterAllChildrenGone` — Phase 16.46 reclamation interop.

### Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run ./...` clean.
- `go test ./... -race -count=3` clean.
- Coverage ≥ 95% (current baseline).
- ALL existing tests pass unchanged (large back-compat surface — flag regressions early).

### Non-goals

- Multi-parent via parent-storage (column holds one ID; multi-parent stays fragmented).
- Runtime migration of an already-populated relationship between fragmented and parent-storage modes — `SetParentStorage` is fail-loud if entities already carry the relationship.
- OrderedChildren-as-rank optimization — future phase.
- Compaction of parent-column gaps after deletions — use the existing swap-remove pattern.
- Cross-table parent-walk optimizations — single-step walk only.

### Notes for the iterate agent

- The Exclusive precondition is critical — parent storage stores ONE parent per entity.
- The "no migration on reparent" benchmark is the headline; ensure it clearly demonstrates ≥4× speedup.
- After this lands, the Go-flecs roadmap is effectively complete — `ROADMAP.md` should note the transition to "all major upstream gaps closed."
- Expect ~2000 LOC of diff; pace accordingly.
