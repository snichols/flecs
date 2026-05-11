## Goal

Add a `CachedQuery` type to the Go flecs port: a persistent query handle whose matching table set is built once at construction and maintained incrementally as new tables are created by entity migrations. After this phase, callers can amortize the cost of repeatedly running the same query and get O(matching-tables) iteration with no per-call candidate-list allocation.

This is a pure addition. The existing uncached `*Query` API stays exactly as-is: `NewQuery`, `Query.Iter`, `Query.Each`, `Query.Terms`, and the `Each1/2/3/4` helpers all keep their current behavior and tests. The new `CachedQuery` shares the same `*QueryIter` type and `Field[T]` helper as `NewQuery`; the API delta is just "build once, iterate many" with a cached table list.

### Shape

```go
cq := flecs.NewCachedQuery(w, posID, velID)
defer cq.Close()

// First iteration: cache is already populated with all existing matches.
for it := cq.Iter(); it.Next(); {
    // ...
}

// Later: an entity migrates into a NEW archetype that also matches.
flecs.Set[Acceleration](w, e, ...)  // creates a new table [P, V, A]

// CachedQuery was notified at table-creation; new table is in its cache.
for it := cq.Iter(); it.Next(); {
    // Visits the new table.
}

cq.Close()  // unregisters from world; future Iters undefined.
```

### Deliverables

1. **Table-creation notification hook in `World.migrate()`** (in `world.go`, around line 244):
   - After `w.compIndex.Register(id, newTable)` runs for each component in the new table, call `w.notifyTableCreated(newTable)` once.
   - `notifyTableCreated` walks `w.cachedQueries` and calls each query's internal `tryMatchTable(t)`.
   - Also fires for the empty table created in `World.New()` (no-op since `cachedQueries` is nil/empty at that point; defensively handle nil).
   - Hook contract documented in a comment.

2. **New type `CachedQuery`** (in `query.go` or new `cached_query.go` -- implementer's call). Unexported fields:
   - World back-pointer.
   - Sorted-copy of term IDs.
   - Cached `tables []*table.Table` -- the current match set.
   - `removed bool` flag for deferred removal so `Close` during iteration is safe.

3. **Construction**: `func NewCachedQuery(w *World, ids ...ID) *CachedQuery`
   - Validate: `w != nil`, `len(ids) >= 1` (panic on empty, matching `NewQuery`).
   - Sorted-copy of ids (uniqueness not enforced; document).
   - Initial population: iterate all existing tables in `w.tables` and add matches.
   - Register with `w.cachedQueries` (lazily allocated slice on `*World`).
   - Prune any `removed == true` entries during registration (amortized compaction; matches observer-deferred-removal pattern from Phase 5.2).

4. **Methods on `*CachedQuery`**:
   - `Iter() *QueryIter` -- fresh iter walking `cq.tables`. Does NOT re-filter (cache is pre-filtered). Simplest path: reuse `QueryIter` with a `cached bool` flag that skips the per-candidate `HasComponent` check; document.
   - `Each(fn func(*QueryIter))` -- convenience wrapper.
   - `Terms() []ID` -- same semantics as `Query.Terms`.
   - `Count() int` -- number of matching TABLES. O(1). Document the table-vs-entity distinction.
   - `EntityCount() int` -- sum of `t.Count()` across cached tables. O(tables).
   - `Close()` -- flips `removed` flag; idempotent. After Close, `Iter`/`Each`/`Terms`/`Count` return empty results.
   - `IsClosed() bool` -- exposed for tests.

5. **Internal `tryMatchTable(t *table.Table)`**:
   - Skip if `cq.removed`.
   - Check each term via `t.HasComponent(termID)`; if all present, append `t` to `cq.tables`.
   - Idempotent: if `t` is already in `cq.tables`, do NOT add again. (Defensive; shouldn't happen if World calls the hook once per table.)

6. **World-side cleanup**: prune `removed == true` entries during the next `NewCachedQuery` registration. Do NOT compact during `notifyTableCreated` (hot path).

7. **Allocation profile** (documented):
   - `NewCachedQuery`: 1 alloc for the struct, 1 for terms slice, 1 for tables slice; initial population walks all tables once.
   - `Iter()`: 1 alloc for `*QueryIter` (same as uncached). NO new table-list alloc.
   - `Each`: same as `Iter` + user's fn.
   - `Close`: 0 allocs.
   - `notifyTableCreated`: O(cachedQueries x terms) per new table.

8. **Tests** in `cached_query_test.go`:
   - Basic construction: empty world -> `Count() == 0`; after `Set[P]+Set[V]` -> `Count() == 1`.
   - Iter walks cached tables: 5 entities [P,V]; iter visits 5. Add 5 more in [P,V,Marker]; iter visits 10.
   - Initial population includes pre-existing matches.
   - No new table = no cache growth: repeated `Iter()` does not reallocate `cq.tables`.
   - Migration adds matching table: [P] entity, then `Set[V]` (migrates to new table) -> cache has 1.
   - Migration does NOT add non-matching table: `NewCachedQuery(w, P, V)` then create [P]-only entity -> cache empty.
   - `Close` stops matching: post-Close migrations don't grow the cache.
   - `Close` is idempotent: two Closes, no panic.
   - `Iter` after `Close`: `Next()` returns false safely.
   - Multiple cached queries: two distinct CachedQuerys on [P] and [V]; verify counts across [P], [V], [P,V] entities.
   - Same query as uncached: `NewQuery(w, posID).Each` and `NewCachedQuery(w, posID).Each` visit the SAME entities in the SAME order (or document any divergence).
   - CachedQuery with `Defer`: deferred mutations flushed, subsequent iterations see new tables.
   - Tag and pair components in cache.
   - `Field[T]` over cached iter: identical to uncached path.
   - Single-term query.
   - Existing uncached `Query` tests stay green; `Each1/2/3/4` helpers unaffected.
   - Pruning of removed queries: create 3, Close 2, create a 4th (triggers compaction); verify internal slice length 2 via test-only helper.

9. **Mechanical acceptance**:
   - `go test ./... -race -count=2` passes.
   - `go vet ./...` clean.
   - `golangci-lint run` clean.
   - Coverage on `flecs` >= 90% (no regression from 97.8%).
   - All exported symbols have godoc.

### Non-goals

- NO up/down traversal modifiers (Phase 6.2).
- NO change detection (Phase 6.3).
- NO wildcards.
- NO NOT/Optional/OR terms (Phase 3.3 deferred).
- NO cache eviction (caches grow until Close).
- NO multi-threaded cache updates (Phase 7).
- NO observer-based change tracking.
- NO per-table dirty bits.
- NO modification of existing `Query`/`QueryIter`/`Field[T]` semantics -- pure additions.
- NO change to the `*Query` (uncached) API.

### Implementer pointers

- Read `src/query/cache/match.c` for the incremental match logic; the C version handles wildcards, sparse storage, etc. -- port only the minimal AND-only path.
- The `notifyTableCreated` hook must fire ONCE per newly-created table, AFTER the table is fully registered in `w.tables` and `w.compIndex`. The earliest correct place is at the end of the find-or-create-miss branch in `migrate()`, after the loop that registers each id in compIndex.
- The empty table created in `World.New()` also triggers `notifyTableCreated` -- defensively handle a nil `cachedQueries` slice.
- The `QueryIter` cached mode can be a single boolean flag: when set, `Next()` skips the inner-term filter and treats every candidate as a match. When unset, current filtering behavior.
- The cached `tables []*table.Table` is owned by `CachedQuery`. The `QueryIter` takes an unprotected reference; the cache's lifetime guarantees validity for the iter's lifetime. Document.
- The `Close` flag (`removed bool`) is not deletion -- matches the observer-deferred-removal pattern from Phase 5.2.
- Do NOT block `notifyTableCreated` on Close -- the slice can be iterated while entries are marked removed; `tryMatchTable` skips them.
- No third-party deps.

### C reference (cite, do not paraphrase)

- `/work/agents/claude/projects/SanderMertens/flecs/src/query/cache/cache.c` -- cached query implementation.
- `/work/agents/claude/projects/SanderMertens/flecs/src/query/cache/match.c` -- incremental match-on-table-create logic.
- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` -- search `ecs_query_cache_kind_t` for the cached-vs-uncached enum.

## Constraints

- @query.go -- existing `NewQuery`, `*Query`, `*QueryIter`, `Field[T]`, and `Each1/2/3/4` semantics must stay intact. `CachedQuery` shares `*QueryIter` and `Field[T]`. Do not modify the `*Query` API.
- @world.go -- `World.migrate()` (around line 244) is where the new-table notification hook fires, after `compIndex.Register` runs for each component in the new table. The empty table created in `World.New()` also triggers the hook (defensively, since `cachedQueries` is nil/empty at that point).
- @hooks.go -- existing hook patterns inform how the table-creation notification is structured and documented.
- @observer.go -- the deferred-removal pattern (`removed bool` flag + amortized compaction on next registration) is the model for `CachedQuery.Close` and the world-side cleanup of `cachedQueries`.
- @id.go -- term IDs are stored as a sorted copy; uniqueness is not enforced (document).
- @internal/storage/table/table.go -- `table.HasComponent(id)` is the per-term match check inside `tryMatchTable`; `table.Type()` provides the component set.
- @internal/storage/componentindex/componentindex.go -- existing `compIndex.Register(id, newTable)` calls in `migrate()` are the anchor point; the new-table hook fires after these complete.
