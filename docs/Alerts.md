# Alerts Addon

The Alerts addon lets you register **query-driven alert rules** that automatically track which entities are in a "problem" state. When an entity enters a monitored query, an `AlertInstance` is raised; when it leaves (or is deleted), the instance is cleared. Callers poll `w.Alerts()` to render warnings, write log lines, or push telemetry.

Ported in Phase 16.28 (v0.83.0). Closes the gap at `docs/README.md` line 76.

---

## Core types

```go
const (
    AlertInfo     = 0
    AlertWarning  = 1
    AlertError    = 2
    AlertCritical = 3
)

type AlertDesc struct {
    Name     string   // optional entity name for the alert
    Query    []Term   // must include at least one With (TermAnd) term
    Severity int      // AlertInfo … AlertCritical
    Message  string   // static text; single %d is replaced with the entity ID
}

type AlertInstance struct {
    Entity   ID
    Alert    ID
    Severity int
    Message  string
    RaisedAt time.Time
}
```

---

## Registering an alert

```go
w.Write(func(fw *flecs.Writer) {
    flecs.RegisterAlert(fw, flecs.AlertDesc{
        Name:     "LowHealthAlert",
        Query:    []flecs.Term{flecs.With(healthID), flecs.Without(deadID)},
        Severity: flecs.AlertWarning,
        Message:  "entity %d has low health",
    })
})
```

`RegisterAlert` allocates an entity for the alert, optionally names it, and installs a hidden monitor observer over the query. Entities that already match the query at registration time are swept synchronously; their instances appear before `RegisterAlert` returns.

---

## Reading active alerts

```go
// All active instances (undefined order)
all := w.Alerts()

// Filtered by severity
warnings := w.AlertsBySeverity(flecs.AlertWarning)

// Filtered by subject entity
forUnit := w.AlertsForEntity(unitID)
```

---

## Lifecycle

| Event | Effect |
|---|---|
| Entity enters the query | `AlertInstance` created; `RaisedAt = time.Now()` |
| Entity leaves the query | Instance removed |
| Entity deleted | Instance removed (monitor observer on-delete path) |

Instances are **not** serialised in `MarshalJSON`. After `UnmarshalJSON` the alert definitions are restored and instances are recomputed from the current entity state via `WithYieldExisting`.

---

## Message templates

`AlertDesc.Message` supports a single `%d` format verb which is replaced with the offending entity's raw ID:

```go
flecs.AlertDesc{
    Message: "entity %d overheated",
    // → "entity 12345 overheated"
}
```

All other text is used as-is. Full component-value interpolation (C's `ecs_script_string_interpolate`) is out of scope for v1.

---

## Snapshot / restore

Alert **definitions** survive `MarshalJSON` / `UnmarshalJSON`:

```go
data, _ := json.Marshal(w)

w2 := flecs.New()
flecs.RegisterComponent[Health](w2)
flecs.RegisterComponent[Dead](w2)
json.Unmarshal(data, w2)
// Alert definitions restored; instances recomputed from current entity state.
instances := w2.Alerts()
```

Requirements:
- All component types referenced by alert queries must be pre-registered in the target world before `UnmarshalJSON` is called.
- Alert instances are **not** stored in the JSON; they are recreated by sweeping the world post-restore.

---

## Design notes

- **Instance representation** — internal `map[alertKey]*AlertInstance` keyed on `(alertID, entityID)`. This diverges from C flecs which creates first-class child entities (`alerts.c:318`); the map approach avoids entity-index churn for transient warnings.
- **Lifecycle engine** — reuses the Phase 16.10 monitor observer (`monitor_observer.go`) rather than a dedicated sweep system. The C `MonitorAlerts` system (`alerts.c:241-340`) is replaced by entry/exit events from the hidden monitor.
- **Severity values** — int constants 0–3 rather than tag entities (C uses tag entities; Go ints are ergonomic for comparison and filter helpers).

---

## v1 non-goals

The following C flecs features are deliberately out of scope for the Go v1 port:

- Alert delivery callbacks (HTTP, file, Slack) — caller iterates `Alerts()` and decides.
- First-class alert-instance entities — instances are a plain Go map.
- Member-field value ranges (`desc.member` / `MemberRanges` in `alerts.c:556-585`).
- Per-alert severity filters (`ecs_alert_severity_filter_t`).
- Retain period / alert timeout (`EcsAlertTimeout`).
- Alert ordering — `Alerts()` returns in undefined order.
- Full message-template interpolation — `%d` (entity ID) only.
