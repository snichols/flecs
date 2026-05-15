## Goal

Add two new built-in observer events that fire on archetype table population transitions, completing two-thirds of the table-events gap in `@docs/README.md` (line 155). The third — `OnTableDelete` — remains explicitly out of scope, gated on table-reclamation infrastructure (line 154 stays as-is).

**Target version:** v0.98.0
**Phase number:** 16.43

### Events

- **`OnTableEmpty`** — fires when a table's row count transitions from 1 → 0
- **`OnTableFill`** — fires when a table's row count transitions from 0 → 1

These are observable from existing row add/remove machinery; no reclamation needed.

### Upstream reference

Before implementation, the agent must locate and capture line numbers for:

- `flecs.h` — `EcsOnTableEmpty` / `EcsOnTableFill` event entity declarations
- `flecs/src/storage/table.c` — `flecs_table_set_count` (or per-row insert/erase) transition site
- Upstream model: tables track `is_empty` or derive from `count == 0`; transitions emit on a dedicated channel separate from per-entity observers
- Verify whether upstream fires for the **root empty table** (the always-present `[]`-signature table). Likely filtered out; the Go port must document its behavior either way

### Go-port surface

**Event entities (built-in)**

The current bootstrap (see `@world.go` lines 538–589) allocates event entities sequentially. Two new event entities slot in alongside `eventOnTableCreateID` (currently index 43) and shift every entity allocated after them:

- `eventOnTableEmptyID ID` — built-in event entity (new index — agent picks the slot; recommend keeping `EventOnTableCreate` at 43, place new event entities adjacent)
- `eventOnTableFillID ID` — built-in event entity
- Accessors on `*World`: `EventOnTableEmpty() ID`, `EventOnTableFill() ID`
- New `EventKind` enum constants `EventOnTableEmpty`, `EventOnTableFill` (current enum stops at `EventMonitor = 5`, see `@observer.go` lines 9–28); update `String()` and `eventKindToEntity()` in `@observer.go`

**Registration API** (mirror `OnTableCreate` / `OnTableCreateWithOptions` at `@observer_table.go`)

- `OnTableEmpty(w *World, fn func(fw *Writer, t *Table)) *Observer`
- `OnTableEmptyWithOptions(w *World, opts ObserverOptions, fn func(fw *Writer, t *Table)) *Observer`
- `OnTableFill(w *World, fn func(fw *Writer, t *Table)) *Observer`
- `OnTableFillWithOptions(w *World, opts ObserverOptions, fn func(fw *Writer, t *Table)) *Observer`

All four register on the same observer map keyed via `tableCreateSentinelID = ID(0)` (the existing untyped-table-event sentinel — reuse it for `OnTableEmpty` / `OnTableFill`; differentiation is via the event entity in `observerKey`).

**Multi-term filter support**

Same shape as Phase 16.15 multi-term observers (`@observer_multi.go`). Observer matches only tables whose signature satisfies `With` / `Without` / OR-group terms. Filter evaluation at the table level (not per-entity) — verify whether `entityMatchesTerms` extends to a `tableMatchesTerms` helper or needs a new one.

**yield_existing**

At registration:
- `OnTableFill` + `yieldExisting` → walk all currently non-empty matching tables; fire once for each (sorted-signature order, as `OnTableCreate` does at `@observer_table.go` lines 57–70)
- `OnTableEmpty` + `yieldExisting` → symmetric: fire for every empty-but-alive matching table (typically just the root empty table)

### Fire-path integration

Row population transitions happen at table-level via `Table.Append` and `Table.RemoveSwap` (see `@internal/storage/table/table.go` lines 111–184). Detection must compare counts immediately around the call:

- World-level callers of `Append` / `RemoveSwap` (see `@world.go` lines 1572–1652 `migrate`, `@world.go` lines 1823–end `commitBatch`, `@world.go` lines 1698+ `migrateArchetypeOnly`) need to capture `prev := newTable.Count()` before `Append`, fire `OnTableFill` if `prev == 0 && newTable.Count() == 1`, and symmetric around `RemoveSwap` for `OnTableEmpty`.
- Fire **after** the migration commits (after `rec.Table`/`rec.Row` are updated and after OnAdd/OnRemove) so observer handlers see the post-migration world. Mirror the existing `notifyTableCreated` dispatch site at `@world.go` lines 1774–1776.

**Dispatch shape**

Existing dispatch key (see `@observer.go` lines 48–54): `observerKey{id, eventEntity}` with `id == tableCreateSentinelID` for untyped table events. Add new dispatches via:

- `w.dispatchObservers(tableCreateSentinelID, w.eventOnTableFillID, 0, unsafe.Pointer(t))`
- `w.dispatchObservers(tableCreateSentinelID, w.eventOnTableEmptyID, 0, unsafe.Pointer(t))`

The `e` parameter (third arg) is 0 for table-level events. `dispatchObservers` at `@observer.go` lines 405–436 already handles the `e == 0` case by skipping multi-term filter evaluation — that path is wrong for table events with terms. The agent must extend the dispatch (or add a parallel `dispatchTableObservers`) so multi-term filtering evaluates against the **table signature**, not the entity.

**Deferred-path safety**

Inside `Write(fn)` defer scope (see `@cmd_queue.go`), row add/remove happens during cmd_queue flush. Transitions must fire when the queue flushes (NOT mid-coalescer):

- The current `migrate` / `commitBatch` paths already run inside the flush, so the natural dispatch site after `notifyTableCreated`-style notification works correctly.
- A table can flicker 0→1→0 inside a single defer batch (entity created and deleted before flush). **Decision: fire all transitions in order of occurrence**, mirroring how OnAdd/OnRemove fire per entity rather than coalesce. Document this explicitly.

**Root empty table**

The root table (signature `[]`) is always alive. Decision: fire as normal events — `OnTableFill` on the first `NewEntity` (0→1) and `OnTableEmpty` on the last `Delete` (1→0). `yieldExisting` observers see the root table reflected in their initial sweep when its current state matches. Document explicitly in `@docs/ObserversManual.md`.

### Required tests

In `@observer_table_test.go` (or a new `observer_table_pop_test.go`):

- `TestOnTableFill_FiresWhenEntityCreated` — entity with Position; verify `OnTableFill` fires once with the Position table
- `TestOnTableEmpty_FiresWhenLastRowRemoved` — entity with Position; delete it; verify `OnTableEmpty` fires once
- `TestOnTableFill_DoesNotFireOnSecondInsert` — entity1 with Position fires Fill; entity2 with Position does not (already non-empty)
- `TestOnTableEmpty_DoesNotFireUntilLast` — three entities; delete two; no Empty event; delete third; Empty fires
- `TestOnTableTransition_RoundTrip` — fill, empty, fill again; verify both events fire on each transition
- `TestOnTableFill_MultiTermFilter` — observer registered with `With(Position)`; only Position tables trigger the observer
- `TestOnTableEmpty_MultiTermFilter` — same
- `TestOnTableFill_YieldExisting` — pre-populate 5 tables; register with yield_existing; verify Fill fires 5 times
- `TestOnTableEmpty_YieldExisting` — register with yield_existing; verify fires for currently-empty matching tables (typically root)
- `TestOnTableFill_RootTable` — verify root empty table transition fires when first entity created
- `TestOnTableTransition_DeferredScope` — inside `Write(fn)`: add+remove+add; verify transitions fire in order after flush
- `TestOnTableTransition_FlickerInsideDefer` — inside `Write(fn)`: add+remove (net empty); verify exact ordering documented
- `TestOnTableFill_HandlerInsideDefer` — Fill handler that itself adds/removes components; verify no infinite loop or panic
- `TestOnTableFill_ObserverDisabled` — disabled observer doesn't fire
- `TestOnTableTransition_JSON_RoundTrip` — registrations don't survive JSON; sanity-check that MarshalJSON / UnmarshalJSON doesn't break tables; transitions fire correctly post-restore. If observers don't currently serialize, skip with a `t.Skip` and a documented reason
- `TestOnTableFill_DontFragment_Component` — entity with a DontFragment component; verify behavior (DontFragment skips archetype transitions per `@dont_fragment.go` line 17, so Fill should fire for the underlying-archetype table, not a side-map slot — agent confirms during implementation)
- `TestOnTableFill_Snapshot_RoundTrip` — observers don't snapshot; sanity-check that snapshot doesn't break tables; transitions fire correctly on the restored world

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run ./...` clean
- `go test ./... -race -count=3` clean
- Coverage ≥ 95% (current baseline)

### Documentation update matrix

- `@docs/ObserversManual.md` — new sections "OnTableEmpty event" and "OnTableFill event" with semantics, multi-term filtering, yield_existing behavior, deferred-scope behavior, root-table semantics
- `@docs/README.md` — flip line 155 to ✅ **shipped in v0.98.0**; leave line 154 (`OnTableDelete`) as-is (still blocked on reclamation)
- `@CHANGELOG.md` — v0.98.0 entry following the v0.97.0 / v0.39.0 templates
- `@ROADMAP.md` — bump "Shipped (through vX)" heading; add Phase 16.43 row; remove the OnTableEmpty / OnTableFill candidate entry at line 135
- `@README.md` — update the Observer feature row

## Constraints

- @docs/README.md — line 154 (`OnTableDelete`) explicitly stays unshipped: reclamation gating must be preserved as a non-goal. Line 155 flips to shipped. Line 71 (the broader hooks gap entry) needs its prose updated.
- @world.go — built-in event entities allocated sequentially at lines 538–561 alongside other built-ins (DontFragment, Wildcard, Any, EventOn*, Event tag, DependsOn, EventMonitor, SlotOf, Units 48–62, Compound units 63–72). Adding two new event entities shifts every subsequent index. The Phase 16.42 entry in `@ROADMAP.md` (line 100) declares "user entities now start at index 73"; this becomes index 75 after the shift.
- @isa_test.go — `TestIsAWorldCountBaseline` at line 669 asserts a fresh-world count of 72 and must be updated to 74. The agent must grep for every baseline-count assertion (`grep "Count(): want\|index 7[0-9]\|index 4[0-9]"`) and update each callsite; `@marshal_test.go` line 42 and `@snapshot.go` line 24 are known additional touchpoints.
- @observer.go — `EventKind` enum at lines 9–28 stops at `EventMonitor = 5`. Add `EventOnTableEmpty = 6` and `EventOnTableFill = 7` (or whichever ordering matches the existing convention). Update `String()` (lines 31–46) and `eventKindToEntity()` (lines 73–88).
- @observer.go — `dispatchObservers` at lines 405–436 currently skips multi-term filter evaluation when `e == 0`; this path is the custom-event-with-no-entity case. Table-level events also pass `e == 0` but DO need filter evaluation against the table signature. Either extend dispatch to differentiate via a flag or introduce a parallel `dispatchTableObservers` keyed off the table.
- @observer_table.go — existing `OnTableCreate` / `OnTableCreateWithOptions` is the canonical shape to mirror. `tableCreateSentinelID = ID(0)` is reused as the map key for all untyped table events; the event entity in `observerKey.eventEntity` discriminates among them.
- @observer_multi.go — multi-term observers (Phase 16.15) operate via `multiTermFilter` on `observerNode`. For table-level filter evaluation, the helper must match the table's signature against `With` / `Without` / OR-group terms rather than calling `entityMatchesTerms`.
- @internal/storage/table/table.go — `Table.Count()` (line 78), `Table.Append` (line 114), `Table.RemoveSwap` (line 140) are the transition primitives. The package is internal and exposes only the existing API; transitions must be observed by the World-level callers, not added to the table package.
- @world.go — three call sites need the transition check: `commitBatch` (around line 1825 where `newTable.Append` runs), `migrate` (around lines 1572–1635 with `Append` and `RemoveSwap`), `migrateArchetypeOnly` (around lines 1698–1730). The dispatch fires after observer-relevant state is settled (after OnAdd/OnRemove, after `rec.Table`/`rec.Row` updates).
- @cmd_queue.go — deferred-mode flush runs the migration paths above; transitions naturally surface during the flush. The flicker-inside-defer model is fire-in-order-of-occurrence (matches OnAdd/OnRemove per-entity semantics).
- @dont_fragment.go — DontFragment components don't cause archetype transitions (lines 17–21). Fill should still fire when the entity's underlying archetype table transitions; the side-map storage doesn't have an archetype-table identity. Agent confirms behavior during implementation and tests.
- @ROADMAP.md — line 135 lists `OnTableEmpty` / `OnTableFill` as a Phase 16.9 candidate. The agent moves this to the shipped section and aligns the phase number to 16.43 (16.9 was the original numbering before later phases interleaved).
- The label `snichols/queued` (not a bug) is correct for this issue.
- Non-goals (preserve as boundaries):
  - `OnTableDelete` is explicitly out of scope; reclamation infrastructure stays untouched
  - No pre-emptive table sweep (firing Empty for tables never used) — fire only on transition
  - No post-hoc custom event entity registration for Empty/Fill — these are built-ins
  - No hierarchical / cascade firing (Empty/Fill on parent tables of subset signatures) — observer evaluates per-table-signature only
