## Goal

Port the C flecs **Alerts addon** to the Go port as **Phase 16.28**, shipping in **v0.83.0** (next after v0.82.0 RunSystemWorker, commit `90736e5`).

An alert is a registered query + severity + message template. Each tick the alert's query is evaluated; entities entering the query raise an alert instance; entities leaving the query (or being deleted) clear theirs. Apps iterate the current alert set to render warnings (UI overlay, log line, telemetry).

Canonical use: `Query: With(Health), HealthBelowZero($this)`, `Severity: AlertWarning`, `Message: "Entity %d has low health"` → matching entities have alert instances raised; UI loops over `w.Alerts()` and renders them.

Closes the gap at `docs/README.md` line 76: *"Alerts addon — not ported."*

### Deliverables

1. **New file `alerts.go`:**
   - Severity constants: `AlertInfo`, `AlertWarning`, `AlertError`, `AlertCritical` (int 0..3).
   - `AlertDesc` struct: `Name string`, `Query []Term`, `Severity int`, `Message string`.
   - `RegisterAlert(w *Writer, desc AlertDesc) ID` — register an alert; returns alert entity ID. Internally installs a hidden monitor observer keyed off `desc.Query`.
   - `(*World).Alerts() []AlertInstance` — snapshot of currently-active alert instances.
   - `AlertInstance` struct: `Entity ID`, `Alert ID`, `Severity int`, `Message string`, `RaisedAt time.Time`.
   - Filter helpers: `AlertsBySeverity(sev int) []AlertInstance`, `AlertsForEntity(e ID) []AlertInstance`.

2. **Lifecycle (monitor-based):**
   - On `RegisterAlert`, register an internal monitor observer over `desc.Query` (reusing Phase 16.10 infrastructure at `monitor_observer.go:47`).
   - On entity entering the query: create an internal `AlertInstance` record, populate fields, set `RaisedAt = time.Now()`.
   - On entity leaving the query: remove the corresponding instance.
   - On entity deletion: remove all instances for that entity (monitor observer's existing on-delete path handles this).

3. **Message templates (v1 scope):**
   - Static string used as-is when no `%d` format verb is present.
   - Single `%d` interpolation substitutes the offending entity ID.
   - Out of scope for v1: arbitrary component-value interpolation (C uses `ecs_script_string_interpolate`).

4. **Tests in `alerts_test.go` (≥10 cases, ≥95.0% coverage):**
   - Register alert; create matching entity → instance present in `Alerts()`.
   - Modify entity so it no longer matches → instance cleared.
   - Delete entity → instance cleared.
   - Multiple matching entities → one instance per (entity, alert).
   - Multiple alerts with overlapping queries → all matching alerts fire.
   - Round-trip each of `AlertInfo` / `AlertWarning` / `AlertError` / `AlertCritical`.
   - `AlertsBySeverity` filters correctly.
   - `AlertsForEntity` filters correctly.
   - `%d` interpolation produces the correct rendered message.
   - Marshal round-trip: registered alerts survive snapshot/restore; active instances are recomputed on the next tick post-restore (query-driven, not data-driven).

5. **Doc updates per CONTRIBUTING.md:**
   - New `docs/Alerts.md` — addon documentation.
   - `docs/README.md` — flip line 76 to ✅ shipped (v0.83.0).
   - `README.md` — feature list bump.
   - `CHANGELOG.md` — v0.83.0 entry at top.
   - `ROADMAP.md` — heading bump to "through v0.83.0"; Phase 16.28 entry.

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%

### Explicit non-goals (v1)

- No alert delivery API (HTTP / file / callback) — caller iterates `Alerts()` and decides what to do.
- No first-class alert-instance entities — instances are an internal map; C's child-entity model (`flecs/src/addons/alerts.c:318` creating `ecs_new_w_pair(world, EcsChildOf, a)`) is deliberately not replicated.
- No member-field reference (C's `desc.member` / `MemberRanges` path in `flecs/src/addons/alerts.c:556-585`) — v1 alerts are at-entity granularity.
- No severity filters (C's `ecs_alert_severity_filter_t` at `flecs/include/flecs/addons/alerts.h:69-75` and `flecs/src/addons/alerts.c:533-553`) — single fixed severity per alert.
- No retain period / alert timeout (C's `EcsAlertTimeout` at `flecs/include/flecs/addons/alerts.h:42`, `flecs/src/addons/alerts.c:28-31`) — instantaneous clear.
- No alert ordering — `Alerts()` returns in undefined order.
- No full message-template interpolation — `%d` (entity ID) only.

### Locked design decisions

1. **Instance representation**: internal struct map keyed on (alert ID, entity ID), not first-class entities. Diverges from C (`alerts.c:318`); justification is simpler API + no entity-index churn for transient warnings.
2. **Message format**: static string with optional single `%d` (entity ID). Full interpolation deferred.
3. **Severity values**: int constants 0..3 (`AlertInfo`=0, `AlertWarning`=1, `AlertError`=2, `AlertCritical`=3). C uses tag entities (`flecs/include/flecs/addons/alerts.h:45-48`); Go uses ints for ergonomic comparison and filter helpers.
4. **Lifecycle integration**: hidden monitor observer reusing Phase 16.10 (`monitor_observer.go:47`), not a separate query-walking system. C runs `MonitorAlerts` in `EcsPreStore` (`flecs/src/addons/alerts.c:241-340, 768`); Go gets entry/exit events for free from the monitor observer.

## Constraints

- @CONTRIBUTING.md — required doc updates (`docs/Alerts.md`, `docs/README.md` line 76, `README.md`, `CHANGELOG.md`, `ROADMAP.md`), version bump rules, mechanical acceptance gates (vet/lint/test/coverage ≥ 95.0%).
- @docs/README.md — line 76 is the canonical gap entry to flip; surrounding section (lines 65-89) defines the gap-list format and severity conventions.
- @monitor_observer.go — Phase 16.10 precedent for entity-enters / entity-exits-matches semantics. `Monitor(w, terms, fn)` at line 47, `MonitorWithOptions` at line 58, sweep-existing at line 143, table-match logic at line 183, on-delete path at line 487. Alert instances are this plus (severity, message, raised-at) metadata.
- @observer.go — observer infrastructure on which the monitor observer is layered. The hidden alert observer registers through the same path.
- @query.go — `Term` construction. `AlertDesc.Query` is `[]Term`, identical to existing query / monitor APIs.
- @scope.go — `Writer` scope; `RegisterAlert(w *Writer, desc AlertDesc) ID` follows the same Writer-scoped registration shape as other addons.
- @marshal.go — snapshot/restore plumbing. Alert *definitions* must survive marshal (registered queries are persistent state); *instances* must NOT be marshaled (they are recomputed from the query on the next tick post-restore).
- C upstream `include/flecs/addons/alerts.h` (lines 1-224) — descriptor shape (`ecs_alert_desc_t` at line 78), severity tags (lines 45-48), `EcsAlertInstance` (line 51) with a single `char *message` field. v1 Go API mirrors descriptor at a high level but omits `severity_filters`, `retain_period`, `member`/`id`/`var`, and `doc_name`/`brief`.
- C upstream `src/addons/alerts.c` (lines 1-788) — lifecycle reference. `MonitorAlerts` system (lines 241-340) is the canonical raise/clear loop; `ecs_alert_init` (lines 496-585) is the registration entry point. Go reuses monitor observer in place of this dedicated system.
