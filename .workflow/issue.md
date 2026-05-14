## Goal

Port the upstream \`EcsOnTableCreate\` observer event into Go-flecs as Phase 16.7, targeting v0.62.0. This is the next item after Phase 16.6 rate filters (v0.61.0, just shipped at \`30a8f7f\`).

The bundle adds a new \`EventOnTableCreate\` event kind plus a typed \`OnTableCreate\` registration helper. Observers fire once per archetype table when the table is first created (first entity migrates into a previously-unseen type signature). Mirrors Phase 16.0 (OnReplace) for shape and Phase 16.5 (observer lifecycle bundle) for the lifecycle/yield-existing/disabled-observer plumbing.

**Important scope note** — this issue intentionally drops the \`OnTableDelete\` half of the upstream pair. Research showed that Go-flecs has no table-reclamation path: \`delete(w.tables, ...)\` is never called anywhere, and tables once created persist for the lifetime of the World. Wiring \`OnTableDelete\` would require first implementing table reclamation, which is a substantial independent change. The \`OnTableDelete\` event is deferred to a follow-up phase that includes the reclamation work. This phase ships \`OnTableCreate\` only.

The neighboring gaps \`OnTableEmpty\` / \`OnTableFill\` (transition between empty and non-empty without reclamation) are also out of scope per \`docs/README.md:153\` — those are a separate listed gap.

### Background (verified)

**Upstream:**

- Event constants: \`/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h:1944–1948\` — \`EcsOnTableCreate\` / \`EcsOnTableDelete\`. Both are real observer events (live in the observer-events category, distinct from the cleanup-policy traits at lines 1950–1955).
- Fire site (create): \`/work/agents/claude/projects/SanderMertens/flecs/src/storage/table.c:847–849\` — \`flecs_table_emit(world, table, EcsOnTableCreate)\` after the table is fully initialized, gated on \`table->flags & EcsTableHasOnTableCreate\`. Note: fires AFTER \`flecs_table_init_overrides\` (line 844), so the table is observably complete when the event fires.
- Fire site (delete): \`/work/agents/claude/projects/SanderMertens/flecs/src/storage/table.c:1278–1281\` — \`flecs_table_emit(world, table, EcsOnTableDelete)\` gated on \`!is_root && !(world->flags & EcsWorldQuit)\`. Suppressed during world teardown to avoid observer thrash. Listed for future-phase reference; not part of this issue.
- Event-flag dispatch: \`src/storage/table.c:890–905\` — the lookup table mapping event entity to \`EcsTableHas*\` flag. Confirms \`EcsOnTableCreate\` lives in the same enum-style space as \`EcsOnAdd\` / \`EcsOnRemove\` / \`EcsOnSet\`.

**Go port:**

- \`observer.go:9–18\` — \`EventKind\` enum with three current entries. New value \`EventOnTableCreate\` to be added.
- \`world.go:1166\` and \`world.go:1290\` — both call sites where \`newTable = table.New(...)\` is constructed and registered into \`w.tables[key]\`. Both call \`w.notifyTableCreated(newTable)\` at lines 1173 / 1294 respectively. There is also a third call at \`world.go:1413\` and an initial call at \`world.go:189\` (the empty table at world construction — important edge case: should OnTableCreate fire for the empty table or not? Recommend NO, mirroring upstream's behavior where the root/empty table is treated specially per the \`is_root\` check at \`table.c:1278\`).
- \`world.go:1358–1375\` — existing \`notifyTableCreated\` function. This is exactly the right hook point. Extend it to dispatch \`EventOnTableCreate\` observers after the existing cached-query notification.
- \`world.go:23\` — \`type Table = table.Table\` already exposes the table type publicly. No new \`TableRef\` type is needed; the existing alias is the right shape.
- \`internal/storage/table/table.go:78\` — \`Table.Count() int\` exists. Public \`Type()\` already used at \`world.go:1370\` and elsewhere. The handler will have read access to signature via \`t.Type()\`.

### API shape (recommended; verify against existing patterns before locking)

\`\`\`go
// EventOnTableCreate fires once per archetype table when the table is first
// created. Does not fire for the world's initial empty table.
EventOnTableCreate EventKind = 4

// OnTableCreate registers an observer for table-creation events.
// Handler receives a Writer scope (for re-entry safety) and the newly
// created table. The handler may read t.Type(), t.Count() etc. but
// should not mutate the table directly; mutations happen via fw.
func OnTableCreate(w *World, fn func(fw *Writer, t *Table)) *Observer
\`\`\`

Open shape decisions to resolve during implementation:

1. **Signature: \`*Table\` vs \`Table\` value vs new opaque \`TableRef\`** — Go-flecs already exposes \`type Table = table.Table\` at \`world.go:23\`, and existing public APIs like \`TablesFor(componentID ID) []*table.Table\` (\`world.go:1331\`) pass \`*table.Table\`. Recommend \`*Table\` for symmetry. Verify no information-hiding concern (table internals are package-private already inside \`internal/storage/table\`).
2. **Typed vs untyped registration** — upstream OnTableCreate doesn't filter by component (it's table-level, not component-level). Recommend ONE untyped registration form: \`OnTableCreate(w, fn)\` with no \`[T]\` parameter. This differs from \`OnAdd[T]\` / \`OnSet[T]\` which are component-filtered. Document the distinction.
3. **Fire timing** — fire AFTER the table is fully registered into \`w.tables[key]\` and \`w.compIndex\`. This matches upstream where \`flecs_table_emit\` fires at \`table.c:848\` after \`flecs_table_init_overrides\` completes. The existing \`notifyTableCreated\` call in Go-flecs at \`world.go:1173\` is already at the correct post-registration point. No fire-site reordering needed.
4. **yield_existing** — Phase 16.5 shipped \`WithYieldExisting()\`. Extending it to OnTableCreate: at observer-registration time, iterate \`w.tables\` and fire the handler once per existing table (excluding the empty/root table). Reuses the same \`ObserveWithOptions\` plumbing.
5. **Re-entry: handler mutates the world** — if the handler creates entities that cause new tables to be allocated, those new tables also fire OnTableCreate. Confirm whether this happens via the deferred coalescer (\`cmd_queue.go\` / \`defer.go\`) or directly. Recommend: deferred — match Phase 16.0 OnReplace re-entry pattern. Verify and document.
6. **Disabled observer** — Phase 16.5's \`SetEnabled(false)\` should already gate this naturally if the new \`EventOnTableCreate\` flows through the same \`dispatchObservers\` path. Verify and add a test.

### Tests (in new \`observer_table_test.go\` or extension of \`observer_test.go\`; minimum 8 cases)

1. **Basic create**: register OnTableCreate handler; create entity with novel component combo (e.g., \`Position\` + \`Velocity\` never-before-seen); handler fires once with a table whose \`Type()\` matches the combo.
2. **De-duplication**: create a second entity in the same archetype; handler does NOT fire again.
3. **Distinct archetypes**: create entities with two distinct novel combos; handler fires twice with different tables.
4. **Migration**: add component to existing entity, causing migration to a new (novel) archetype; handler fires once for the new table.
5. **Empty/root table NOT fired**: handler registered before any entity creation; the empty table (created at \`world.go:189\`) does NOT trigger the handler. Document rationale: matches upstream's \`is_root\` suppression at \`table.c:1278\` and avoids observer noise at world construction.
6. **Disabled observer (Phase 16.5)**: register OnTableCreate, call \`SetEnabled(false)\`, create novel-combo entity; handler does NOT fire. Re-enable, create another novel combo; handler fires.
7. **yield_existing**: pre-populate world with N distinct archetypes; register OnTableCreate with \`WithYieldExisting()\`; handler fires N times at registration. Verify ordering is deterministic (insertion order is acceptable; document chosen ordering).
8. **Re-entry safety**: handler creates an entity (via \`fw\`) that triggers a NEW novel archetype; verify no panic, verify the second OnTableCreate fires (via the deferred coalescer), verify world state is consistent after.
9. **Multiple observers**: register two OnTableCreate handlers; both fire in registration order for a single table creation.
10. **Coverage ≥ 95.0%** — match repo bar; \`go test ./... -race -count=3 -coverprofile=cover.out\` then \`go tool cover -func=cover.out | tail -1\`.

### Doc updates

- **\`docs/ObserversManual.md\`** — new \`§ OnTableCreate\` section with at least one example. Note the divergence from component-filtered observers (no \`[T]\` parameter). Cross-link to \`docs/Tables.md\` if such a doc exists, otherwise just to the \`Table\` type.
- **\`docs/README.md\`** — flip line 153 partially: change wording to \"OnTableCreate ✅ shipped in v0.62.0; OnTableDelete deferred pending table-reclamation infrastructure.\" If a clean split is preferred, leave line 153 entry but reword.
- **\`README.md\`** — feature-list bump (one line).
- **\`CHANGELOG.md\`** — v0.62.0 entry at the top, calling out the create-only scope.
- **\`ROADMAP.md\`** — heading bump to \"Shipped (through v0.62.0)\".

### Mechanical acceptance

- \`go vet ./...\` clean
- \`golangci-lint run\` clean
- \`go test ./... -race -count=3\` passes
- Coverage ≥ 95.0%

### Explicit non-goals

- **No \`OnTableDelete\` / \`EventOnTableDelete\`**. Go-flecs has no table-reclamation path (\`delete(w.tables, ...)\` is unused; tables persist for World lifetime). Wiring OnTableDelete requires implementing reclamation first. Defer to a follow-up phase that bundles reclamation + OnTableDelete.
- **No \`OnTableEmpty\` / \`OnTableFill\`** (row-count transitions). These are a separate listed gap at \`docs/README.md:153\` and out of scope.
- **No \`OnDelete\` / \`OnDeleteTarget\` observer events**. Those are not real upstream observer events (see companion docs-correction issue); they are cleanup-policy relationship traits already shipped in v0.32.0.
- **No multi-term observer filters** (gap line 155 — separate large phase).
- **No table-level introspection API expansion**. Reuse existing \`Table.Type()\` / \`Table.Count()\` exposed via the public alias.

### Open decision points (resolve during implementation)

1. **Root/empty table suppression**: confirm via test 5 that the world's initial empty table does NOT trigger OnTableCreate. Implementation: either gate inside the new \`dispatchObservers\` call within \`notifyTableCreated\`, or skip the call at \`world.go:189\` specifically. Recommend the latter — clearer intent at the call site.
2. **Re-entry path**: verify the handler's writes flow through the deferred coalescer when the observer is invoked mid-fire. The existing \`OnAdd\` / \`OnSet\` path is the reference. If \`notifyTableCreated\` runs inside an active Writer scope already (likely yes — it's called from \`commitBatch\` / \`migrate\`), the handler can mutate freely.
3. **Untyped-only API decision**: confirm with one round of design review that there's no useful typed form. Upstream has only one form (\`EcsOnTableCreate\` is its own event, not parameterized).

## Constraints

- @CLAUDE.md — repo conventions for new phase bundles, naming, and doc updates.
- @observer.go — existing \`EventKind\` enum (lines 9–18); extend with \`EventOnTableCreate\`. Existing \`dispatchObservers\` / \`addObserverNode\` are the dispatch path.
- @observer_options.go — Phase 16.5 \`WithYieldExisting\` plumbing; OnTableCreate inherits it.
- @observer_lifecycle_test.go — Phase 16.5 test patterns for \`SetEnabled\` / yield_existing.
- @world.go — lines 23 (Table alias), 189 (empty-table construction), 1166/1290/1413 (table creation sites), 1331 (TablesFor public API), 1358–1375 (notifyTableCreated — the extension point).
- @internal/storage/table/table.go — line 78 \`Count()\`; the table's public surface available to handlers via the existing alias.
- @docs/README.md — line 153 is the genuine OnTableCreate/Delete gap entry being closed (partially — create only).
- @docs/ObserversManual.md — destination for the new \`§ OnTableCreate\` section.
- @CHANGELOG.md — v0.62.0 entry slot.
- @ROADMAP.md — \"Shipped (through v0.62.0)\" heading bump.
- @CONTRIBUTING.md — phase / doc-update conventions.
- Upstream reference: \`/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h:1944–1948\` — observer-event declarations.
- Upstream reference: \`/work/agents/claude/projects/SanderMertens/flecs/src/storage/table.c:847–849\` — create fire site.
- Upstream reference: \`/work/agents/claude/projects/SanderMertens/flecs/src/storage/table.c:890–905\` — event-flag mapping.
- Phase 16.0 OnReplace (v0.55.0) — most recent observer-event-shaped bundle to mirror.
- Phase 16.5 observer lifecycle (v0.60.0) — \`SetEnabled\` / \`WithYieldExisting\` plumbing to inherit.
- Companion issue: docs-correction issue (the OnDelete/OnDeleteTarget gap-entry fix) is independent and can ship in either order.
