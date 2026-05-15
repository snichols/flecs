## Goal

Add table reclamation infrastructure to Go-flecs so long-running worlds with archetype churn no longer leak memory monotonically. Currently archetype tables persist for the lifetime of the `World`; once entities migrate away and a table goes empty, it is never freed. This phase introduces tick-based lifetime tracking, a reference-counted reclamation policy, and a new `OnTableDelete` observer event that fires synchronously before reclamation.

Target version: **v0.101.0**. Phase number: **16.46**.

### Scope

1. **Table-lifetime tracking** — each `*Table` carries `emptyTicks uint32` (consecutive Progress ticks with `Count() == 0`), `refCount int32` (atomic; bumped on query/observer/iter registration), `reclaimEpoch uint64` (last eligible tick), and `pinned bool` (explicit user opt-out).
2. **Reclamation policy** — at start of each `Progress(dt)`, a sweep pass walks all alive tables; those with `emptyTicks >= threshold && !pinned && refCount == 0` are scheduled for reclamation. Default threshold is **60 ticks** (1s at 60fps). `SetTableReclamationThreshold(0)` disables reclamation entirely (preserves current behavior).
3. **OnTableDelete observer event** — new built-in event entity `EventOnTableDelete` at the next available index (shift built-ins; update baselines). Fires synchronously before unlinking. Iterate agent picks `*Reader` vs `*Writer` for the handler context and documents the rationale (lean toward `*Reader` since the table is mid-destruction).
4. **Configurable threshold + opt-out** — `(*World).SetTableReclamationThreshold(ticks uint32)`, `(*World).TableReclamationThreshold() uint32`, `(*World).ReclaimedTablesCount() uint64`, and optional `(*World).ReclaimNow() int` for tests / shutdown.
5. **Safety invariants** — no `*Table` pointer reachable from any observer, query, system, or open iterator may be freed. Reclamation runs only when `refCount == 0`.

### Tracking model

Each `*Table`:
- `emptyTicks uint32` — consecutive Progress ticks with `Count() == 0`
- `refCount int32` — atomic; bumped on query/observer/iter registration, decremented on Free / unsubscribe / iter close
- `reclaimEpoch uint64` — last tick at which the table was eligible
- `pinned bool` — explicit user opt-out (`(*Table).Pin()` / `Unpin()` / `IsPinned()`)

Increment / decrement points:
- On row insert: `emptyTicks = 0`
- On Progress tick: if `Count() == 0 && !pinned && refCount == 0`: `emptyTicks++`
- Sweep at start of Progress: tables with `emptyTicks >= threshold` are scheduled
- Reclamation: fire OnTableDelete → unlink from per-component-id index → free row storage → invalidate pointer

### Reclamation algorithm

`reclaimDeadTables(w *World)`:
1. Walk all alive tables (use the existing table list in `world.go`)
2. For each eligible table:
   - Fire OnTableDelete observers (matching the table signature; multi-term filter; before unlinking)
   - Remove from `worldTables` registry
   - Remove from each component-id → tables index entry
   - Clear column storage (return memory to runtime)
   - Mark `*Table.dead = true` so lingering pointer reads can detect-and-panic
3. Return count of reclaimed tables

Runs at the start of each `Progress(dt)` before phase dispatch. Mutations from OnTableDelete handlers defer through the standard write scope.

### Reference-count integration

Queries / observers / iters that hold table pointers bump refCount on registration:
- `NewQueryFromTerms` / `NewCachedQueryFromTerms` — bump per matched table at construction; decrement on `Free()`
- `Observe*` — bump on registration, decrement on Unsubscribe
- `Iter` / iter-result handles — bump on iter open, decrement on iter close
- Cached query subscriptions — bump for subscription lifetime

Under-counting causes UAF; over-counting prevents reclamation. Tests must exhaustively verify zero leaks across realistic register/unregister cycles.

### Design calls iterate agent must make explicitly

- **Dead-table pointer access** — panic in all builds? Silent zero-table? Sentinel? Pick and document.
- **OnTableDelete handler context** — `*Reader` (safer, table mid-destruction) vs `*Writer` (consistent with Phase 16.43 OnTableEmpty/Fill). Pick and document the rationale.
- **`WithYieldExisting` for OnTableDelete** — semantic is nonsensical (delete has no pre-existing events to replay). Either no-op or error; pick and document.

### Upstream reference

- `flecs/src/storage/table.c` — locate `flecs_table_free`, `flecs_table_delete`, and reclamation/sweep logic; capture exact line numbers
- `flecs/src/storage/table_cache.c` — per-component subscriber count management
- `flecs/include/flecs.h` — `EcsOnTableDelete` event entity
- Verify whether upstream reclaims synchronously on `Delete` or lazily on a sweep; record findings in the implementation commit

### Required tests (new file: @table_reclamation_test.go)

**Tracking**
- `TestReclamation_TableEmptyTicksAdvances`
- `TestReclamation_TableInsertResetsTicks`
- `TestReclamation_PinnedTableNeverReclaimed`
- `TestReclamation_RefCountedTableNeverReclaimed`
- `TestReclamation_ThresholdZeroDisables`
- `TestReclamation_DefaultThresholdIs60`

**Reclamation path**
- `TestReclamation_BasicSweep` — create entity, delete, drive 61 Progress ticks; verify `ReclaimedTablesCount() == 1`
- `TestReclamation_ReclaimNow_Force`
- `TestReclamation_MultipleTablesInOneSweep`
- `TestReclamation_NewTableAfterReclamation` — verify fresh table, not zombie reuse

**OnTableDelete event**
- `TestOnTableDelete_FiresBeforeReclamation`
- `TestOnTableDelete_TableSignaturePassedCorrectly`
- `TestOnTableDelete_MultiTermFilter`
- `TestOnTableDelete_YieldExistingIsNoOp` (or error — per chosen design)
- `TestOnTableDelete_HandlerCannotMutate` (or `_CanMutate` — per chosen design)
- `TestOnTableDelete_ObserverDisabled`

**Reference counting**
- `TestReclamation_QueryConstructionBumpsRef`
- `TestReclamation_QueryFreeDecrementsRef`
- `TestReclamation_ObserverRegistrationBumpsRef`
- `TestReclamation_IterOpenBumpsRef`
- `TestReclamation_NoRefLeakAfterRoundTrip`

**Stress / property**
- `TestReclamation_LongRunWorkload` — 10k entities × 100 archetypes × 1000 ticks; memory bounded / non-monotonic table growth
- `TestReclamation_RaceCondition_NoCrash` — concurrent queries during reclamation; clean under `-race`; no nil derefs

**Snapshot integration**
- `TestReclamation_SnapshotRestore_StartsFresh` — TakeSnapshot/RestoreSnapshot resets all empty counters; restored world reclaims correctly on subsequent Progress

**Threshold tuning**
- `TestReclamation_HighThresholdDelaysReclamation`
- `TestReclamation_VeryLowThreshold`

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run ./...` clean
- `go test ./... -race -count=3` clean
- Coverage ≥ 95% (current baseline)
- All Phase 16.43 OnTableEmpty/Fill tests pass unchanged
- All snapshot tests (Phase 16.24) pass unchanged
- All query tests (Phase 16.0–16.45) pass unchanged

### Documentation update matrix

- New section in @docs/ObserversManual.md for OnTableDelete
- @docs/README.md:
  - Line 154 — flip "OnTableDelete event — deferred pending table-reclamation infrastructure" to **shipped in v0.101.0**
  - Line 71 — update "Entity hooks beyond OnAdd/OnSet/OnRemove" entry: all four table events now shipped
- New file @docs/TableReclamation.md — tracking model, threshold tuning, refcount semantics, pinning API, performance notes, debugging tips (`ReclaimedTablesCount` for monitoring)
- @docs/Manual.md — cross-link to TableReclamation.md
- @CHANGELOG.md — v0.101.0 entry
- @ROADMAP.md — bump heading; add Phase 16.46 row
- @README.md — update feature row

### Operational notes

- This is the most architecturally invasive phase since Phase 12.x. Iterate agent should profile a long-running test with and without reclamation to demonstrate the memory bound.
- Reference-count hook sites are critical and require careful research into the existing query / observer / iter registration paths before edits.

## Constraints

- @internal/storage/table/table.go — Table struct; add `emptyTicks`, `refCount` (atomic), `reclaimEpoch`, `pinned`, `dead` fields and the Pin/Unpin/IsPinned API; this is the central data structure for the phase
- @world.go — table registry, Progress loop, built-in entity allocation; sweep pass hooks into Progress start, `EventOnTableDelete` allocates at next built-in index (currently Reflexive=21, Wildcard=22, Any=23, user=24 per the v0.39.0 release), config API (`SetTableReclamationThreshold`, `TableReclamationThreshold`, `ReclaimedTablesCount`, optional `ReclaimNow`) lands here
- @query.go — query construction and table matching; refCount bump per matched table at construction, decrement on `Free()`
- @cached_query.go — cached query subscriptions; refCount bump for subscription lifetime
- @observer.go — observer registration; refCount bump on register, decrement on Unsubscribe
- @observer_table.go — Phase 16.7 / 16.43 patterns for OnTableCreate / OnTableEmpty / OnTableFill; OnTableDelete follows the same multi-term `WithQuery` filter shape and `WithYieldExisting` mirror conventions
- @snapshot.go — TakeSnapshot/RestoreSnapshot must reset `emptyTicks` to zero on restore; restored worlds must reclaim correctly on subsequent Progress
- @docs/ObserversManual.md — observer event documentation conventions; new OnTableDelete section follows existing OnTableEmpty/Fill structure
- @docs/README.md — feature ledger lines 71 and 154 flip to shipped
- @CHANGELOG.md — v0.101.0 entry follows existing release-note format
- @ROADMAP.md — phase row append; bump heading version
- @README.md — feature row update follows existing matrix
- Label `snichols/queued` (not a bug; this is a queued feature phase)
- Non-goals: compaction / table merging; async / background reclamation; per-table custom thresholds; memory-pressure-based reclamation; forced reclamation of pinned tables under memory pressure — these are explicitly out of scope for v1
