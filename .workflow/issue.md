## Goal

Add opt-in change detection to cached queries so systems can skip work when nothing relevant has changed since the last iteration. The structured-term query API (AND/NOT/Optional/OR) is complete as of `v0.6.0` (master HEAD `fc79192`); this phase closes the last query gap before systems work begins.

Target usage:

```go
q := flecs.NewCachedQuery(w, posID, velID)

for {
    if q.Changed() {
        // Something relevant changed since the last call. Re-run downstream work.
        runMovement(q)
    }
    w.Progress(0.016)
}
```

**Semantics.** `(*CachedQuery).Changed() bool` returns true if since the last call:
1. A new table matching the query was added to the cache, OR
2. Any cached table had a `Set[T]` / `SetByID` / `SetPair[T]` / `SetPairByID` (any column write), OR
3. Any cached table had structural changes (entity added/removed via migrate).

First call after `NewCachedQuery` returns true (initial state is "all changed"). NOT goroutine-safe.

**Scope.** Per-table only; uncached `*Query` does NOT get `Changed()` (force users to the cached path); any column write marks the table dirty for all cached queries containing it (conservative, never under-reports).

### Deliverables

1. **`internal/storage/table/table.go`** — unexported `changeCount uint64` on `Table`. Incremented in `Append`, `RemoveSwap`, and a new `BumpChange()` method called by the World on column writes. Expose `func (t *Table) ChangeCount() uint64`. Counter is monotonic uint64 (wraps eventually, practical infinity).

2. **World writes call `BumpChange()`** — in `setImmediateByPtr` (the Phase 9.1.1 shared helper) after writing column data. `migrate()` already calls `Append`/`RemoveSwap` so structural changes need no extra work. **Order matters:** hooks/observers must fire BEFORE the bump so they see the new value via `Get` while change detection still sees it as new. Both `Set[T]`/`SetByID` and pair `SetPair[T]`/`SetPairByID` flow through `setImmediateByPtr`; verify pair writes trigger the bump.

3. **`(*CachedQuery)` extensions** — unexported `lastChangeCounts map[*table.Table]uint64` and `tablesAdded bool`. Set `tablesAdded` in `tryMatchTable` on successful append. `Changed()`:
   - If `tablesAdded`: return true, reset flag, sync `lastChangeCounts` with current counts.
   - Else: scan cached tables; return true if any `ChangeCount() != lastChangeCounts[t]`, updating the map on the way out.
   - First call: `lastChangeCounts` is empty, missing keys (0) compare unequal to any actual counter, returns true, then populates.

4. **Uncached `*Query`** — DO NOT add `Changed()`. Document that change detection is cached-query-only.

5. **Tests** in `cached_query_test.go` (extend) or new `change_detection_test.go`:
   - Initial state: first call true, second call false.
   - New table appears post-construction (Set creates [Position] table after CachedQuery exists).
   - Column write same archetype.
   - Append into already-matching archetype.
   - Migrate out of matching archetype (RemoveSwap on source).
   - Delete entity.
   - Multiple Sets coalesce into one `Changed()=true`, then false.
   - Two queries q1=[Pos], q2=[Pos,Vel]: writing [Pos]-only table flips q1 but not q2.
   - Cross-query independence.
   - `Changed()` after `Close()`: safe; document chosen behavior.
   - `SetPair[Edge]` pair-id write triggers Changed.
   - Mutations inside `Defer` block trigger Changed after flush.
   - All existing CachedQuery tests stay green.

6. **Benchmarks** in `bench_test.go`:
   - `BenchmarkCachedQueryChangedHit_10kTables` — many tables, no changes between calls.
   - `BenchmarkCachedQueryChangedAfterSet` — Set + Changed.
   - Verify existing `BenchmarkSet*` show no regression (BumpChange is one uint64 increment in cache locality with the column write).

7. **Documentation** — CachedQuery godoc with `Changed()` example; doc.go "Change detection" section; CHANGELOG Unreleased entry ("Phase 9.5: Change detection via CachedQuery.Changed()"); README feature list.

8. **Mechanical acceptance** — `go test ./... -race -count=2` passes; `go vet ./...` clean; `golangci-lint run` clean; `flecs` coverage >= 90% (no regression from 96.0%); `internal/storage/table` coverage >= 90%; all exported symbols documented.

### Non-goals

- NO per-entity change tracking.
- NO per-component change tracking (any column write on a cached table marks it dirty for all queries containing it).
- NO `Changed()` on uncached `*Query`.
- NO time-windowed history.
- NO change callbacks / `OnChange(q, fn)`.
- NO selective bump per query (would be O(tables x queries) space).
- NO hooks/observers as the implementation mechanism — internal dirty bits only.
- NO breaking changes.
- NO third-party deps.
- NO modifications to `column.go` internal storage; the counter lives on `Table`.

C reference (cite, do not paraphrase):
- `/work/agents/claude/projects/SanderMertens/flecs/src/query/cache/change_detection.c`
- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` (search `ecs_query_changed`)

## Constraints

- @cached_query.go — site for `Changed()`, `lastChangeCounts`, `tablesAdded`; `tryMatchTable` sets the flag on successful append.
- @internal/storage/table/table.go — add `changeCount uint64` field, `BumpChange()`, `ChangeCount() uint64`; bump on `Append` and `RemoveSwap`.
- @internal/storage/table/column.go — DO NOT modify; counter lives on Table, not Column.
- @world.go — owner of write paths; ensure hooks fire before `BumpChange` in the write order.
- @value_ops.go — `setImmediateByPtr` is the single bump site for column writes; both `Set[T]` and `SetByID` flow through here.
- @id_ops.go — `SetPair[T]` / `SetPairByID` route through `setImmediateByPtr` (Phase 4.1 -> 9.1.1 refactor); verify pair writes bump.
- @doc.go — add "Change detection" section.
- @CHANGELOG.md — Unreleased entry for Phase 9.5.
- @README.md — update feature list.
- @bench_test.go — extend with the two new benchmarks; ensure existing `BenchmarkSet*` stay flat.
- Counter is monotonic uint64; document the wrap-is-practical-infinity property and the conservatism of "any column write marks the table dirty for all queries containing it" — never under-reports, may over-report.
- `*CachedQuery.Changed()` is NOT goroutine-safe; document.
- First-call-returns-true falls out naturally from empty `lastChangeCounts` map; do not special-case.
