## Goal

Continue draining the trait-system roadmap from `docs/ComponentTraits.md`. **CanToggle** lets a component be temporarily disabled on an entity without removing it. Queries skip rows where the component is disabled; re-enabling restores matching behavior. Useful for systems that want to "pause" an entity without paying the table-migration cost of Remove + Add.

Cited as missing in:
- `docs/EntitiesComponents.md` (entity disabling)
- `docs/ComponentTraits.md` (CanToggle trait — explicit "Not yet ported" callout)

Phase precedent: 15.0 (cleanup policies), 15.1 (OnInstantiate Override / DontInherit), 15.2 (Exclusive) all shipped following the same small-trait pattern.

**Target version: v0.35.0.**

### C grounding (researched before drafting)

- `include/flecs.h:1859` declares `EcsCanToggle` as a built-in entity, doc-stringed "Mark a component as toggleable with ecs_enable_id()".
- `include/flecs/private/api_flags.h:76` defines `EcsIdCanToggle (1u << 13)` — the per-component flag bit cached on the component record. `:197` defines `EcsTermIsToggle (1u << 9)` — per-term marker the query compiler sets when a term names a CanToggle component. `:238` defines `EcsTableHasToggle (1u << 15u)` — table flag set when any column in the signature is a TOGGLE pseudo-id.
- `src/bootstrap.c:890,1015,1154-1156` register the entity, register the trait, and install an `EcsCanToggle` trait observer so `ecs_add_id(world, MyComp, EcsCanToggle)` mirrors into the component record's `EcsIdCanToggle` flag.
- `src/entity.c:2390-2480` is the toggle API. `flecs_can_toggle` checks `cr->flags & EcsIdCanToggle` (or falls back to `ecs_has_id(world, component, EcsCanToggle)` when no component record exists). `ecs_enable_id` asserts `flecs_can_toggle`, adds the synthetic `bs_id = component | ECS_TOGGLE` to the table (which is how the bitset column is materialized), then calls `flecs_bitset_set(bs, row, enable)`. `ecs_is_enabled_id` returns `ecs_has_id(world, entity, component)` when the table has no TOGGLE column (interpreted as "always enabled if owned"), else `flecs_bitset_get(bs, row)`.
- `src/storage/table.c:33-77,202-209,356-362` stores the bitset columns on `table->_->bs_columns[]` indexed by `i - bs_offset` for each `ECS_HAS_ID_FLAG(id, TOGGLE)` slot in the signature. `bs_count` and `bs_offset` are set at table-init time.
- `src/query/engine/eval_toggle.c:38-310` is the per-row matcher: dedicated `EcsQueryToggle` / `EcsQueryToggleOption` opcodes (eval.c:1481-1482 dispatch). `flecs_table_get_toggle(table, id)` returns the bitset; the matcher AND-folds enabled bits across all toggle-flagged terms before yielding rows.

The Go port mirrors this shape but does not need to replicate the synthetic `ECS_TOGGLE` id flag — it is an internal storage trick. A `map[ID][]uint64` on the `Table`, keyed by the bare component ID, is the equivalent.

### Likely shape (refine during implementation)

1. **New built-in entity ID** `World.CanToggle() ID` (index 18; first user entity moves to 19). Self-applied tag form: `fw.AddID(componentID, w.CanToggle())`. Mirrors the 15.2 Exclusive pattern (`exclusiveID`, index 17).

2. **Per-component flag storage.** Add `canTogglePolicies map[ID]bool` to `World`, keyed by `ID.Index()` (same generation-stripping rule as `exclusivePolicies` — see `exclusive.go:39-44`). `SetCanToggle(w, componentID)` writes the entry; `addIDImmediate` (`id_ops.go:70-75`) gains a parallel branch translating bare `w.CanToggle()` tag-adds.

3. **Per-row disabled-bit storage on Table.** New `map[ID][]uint64` field on `table.Table` keyed by component ID, each slice sized `ceil(rowCount / 64)`. Default value (no entry) = all-enabled. `Append` grows existing entries; `RemoveSwap` swaps bits and truncates. The map is allocated lazily on first `SetEnabled` write to keep zero-cost on tables that never toggle. (Alternative: `[]uint64` per-column on the Column struct — but storing on the Table by id keeps Column simple and matches the C `bs_columns` array.)

4. **Public API:**
   - `flecs.SetCanToggle(w *World, componentID ID)` — mark a component as toggleable.
   - `flecs.IsCanToggle(w *World, componentID ID) bool` — inspection.
   - `flecs.EnableID(fw *Writer, e ID, componentID ID)` / `flecs.DisableID(fw *Writer, e ID, componentID ID)` — set the bit. Panic if the entity does not have the component, or if the component is not marked CanToggle (mirrors C `ecs_check(flecs_can_toggle, ECS_INVALID_OPERATION)` at `entity.c:2416`).
   - `flecs.IsEnabled(r *Reader, e ID, componentID ID) bool` — read the bit. Returns false if entity does not have the component; returns true if the table has no bitset entry for that component (mirrors C `entity.c:2466-2470`).
   - Typed variants `flecs.Enable[T any](fw *Writer, e ID)` / `Disable[T]` / `IsEnabled[T any](r *Reader, e ID) bool` — resolve the component ID via the registry, then delegate.

5. **Query matcher integration.** When a query has a term naming a CanToggle-marked component, the iterator's `Next` (`query.go:400`) must apply a per-row filter against the table's bitset entry. C uses dedicated opcodes; our matcher is straight-line, so a single AND-fold pass at `Next` boundary (or row-yield in `Each1`/`Each2` at `scope.go:511-535`) suffices. Optimization: a query that touches no CanToggle components skips the filter entirely (gate at validate-time by walking term IDs against `w.canTogglePolicies`).

6. **Tests** (`cantoggle_test.go`, 8+ cases):
   - Non-CanToggle component: `DisableID` panics with the expected message.
   - Mark + Disable + Enable round-trip: `Has` returns true throughout; `IsEnabled` reflects current state.
   - `Each1[Position]` skips rows where Position is disabled.
   - Re-enable restores query visibility on the same row in the same table.
   - Multiple disabled components on one entity tracked independently.
   - Toggle survives table migration: disable Position, add an unrelated Tag (forces archetype move), Position is still disabled. (Verify against C; document either way.)
   - Change-detection: enabling/disabling fires `Table.BumpChange()` so cached queries re-evaluate. (Verify against C; this is consistent with how column writes behave.)
   - Multi-entity table with mixed enabled/disabled: query visits exactly the enabled ones.

7. **Docs updates per CONTRIBUTING.md:**
   - `docs/EntitiesComponents.md` — replace the entity-disabling "Not yet ported" callout with shipped content + worked example.
   - `docs/ComponentTraits.md` — trait-system roadmap row → `✅ shipped (v0.35.0)`.
   - `docs/Queries.md` — brief mention that queries skip disabled rows, with a link to the trait doc.
   - `docs/README.md` — gap list pruned.
   - `CHANGELOG.md` — new `v0.35.0 — Phase 15.3: CanToggle component trait` entry.
   - `ROADMAP.md` — mark Phase 15.3 shipped.

### Non-goals

- **Entity-level disabling** — C also has `EcsDisabled` as a tag applied to an entity to disable it entirely (separate from CanToggle). That is a future phase; this work is component-level only.
- **Bulk enable/disable across many entities** — single-entity API only.
- **Performance optimization for the per-row branch** — make it work cleanly. Benchmark later if needed.
- **No change to Add/Remove semantics** — Add still adds the column; Remove still removes it. CanToggle is orthogonal.
- **No synthetic `ECS_TOGGLE` id flag** — that is a C storage trick; the Go port stores the bitset directly on the Table keyed by component ID.

### Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run ./...` clean.
- `go test ./... -race -count=3` clean.
- Coverage on main package ≥ 95%.
- Existing tests pass unmodified.
- New `cantoggle_test.go` covers the 8 cases above.
- Docs updates land in the same PR.

## Constraints

- @CONTRIBUTING.md — every shipped feature lands with same-PR doc updates (callout removal, roadmap row flip, CHANGELOG, ROADMAP).
- @docs/ComponentTraits.md — trait-system roadmap is the source of truth for which traits are missing; the "Not yet ported" callout for CanToggle must be replaced with shipped content.
- @docs/EntitiesComponents.md — entity-disabling section currently flags this gap; rewrite it with the new API surface.
- @docs/Queries.md — queries skip disabled rows by default; add a one-paragraph cross-reference.
- @docs/README.md — gap list is pruned on every shipped trait.
- @CHANGELOG.md, @ROADMAP.md — version-cadence convention: `v0.35.0 — Phase 15.3: CanToggle component trait`.
- @exclusive.go — phase precedent for a small self-applied trait tag (index, flag map keyed by `ID.Index()`, `Set*` / `Is*` accessors, bootstrap-time `applyExclusivePolicy` calls).
- @instantiate_policies.go, @cleanup.go — adjacent precedents for per-component / per-relationship policy maps.
- @world.go — built-in entity allocation block (lines 122-219). CanToggle becomes index 18; the doc comment block at lines 89-108 must be extended, and the user-entity-start callout at line 108 updated to "Index 19+".
- @id_ops.go — `addIDImmediate` (lines 29-102) hosts the bare-tag translator block; the new branch sits alongside the existing `id.Index() == w.exclusiveID.Index()` arm at lines 70-75.
- @internal/storage/table/table.go — the bitset storage is the new mechanical piece. `Append` (108-118), `RemoveSwap` (124-144), and possibly a new `EnableRow` / `DisableRow` / `IsRowEnabled` API live here. Storage layout decision (map-on-Table vs slice-on-Column) belongs in this file.
- @query.go — `Next` at line 400 and `matchesTable` at line 439 are the integration points for the per-row filter. The validate-time gate (skip filter entirely when no term touches a CanToggle component) belongs alongside query construction.
- @scope.go — `Each1` / `Each2` at lines 511-535 yield rows; the filter must apply before invoking the user callback.
- @cantoggle_test.go — new file; pattern after `exclusive_test.go` for structure.

Use `gh issue create --label snichols/queued`.

