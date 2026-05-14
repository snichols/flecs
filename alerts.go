package flecs

import (
	"fmt"
	"strings"
	"time"
)

// Alert severity constants. Higher value = higher severity.
const (
	AlertInfo     = 0
	AlertWarning  = 1
	AlertError    = 2
	AlertCritical = 3
)

// AlertDesc describes an alert to register with RegisterAlert.
type AlertDesc struct {
	Name     string // optional name for the alert entity
	Query    []Term // query terms; must include at least one TermAnd term
	Severity int    // AlertInfo, AlertWarning, AlertError, or AlertCritical
	// Message is the alert text. A single %d verb is replaced with the entity ID;
	// all other text is used as-is. Full component-value interpolation is out of scope for v1.
	Message string
}

// AlertInstance is a snapshot of one active alert raised for one entity.
type AlertInstance struct {
	Entity   ID
	Alert    ID
	Severity int
	Message  string
	RaisedAt time.Time
}

// alertKey uniquely identifies a (alert entity, subject entity) pair in the
// world's active instance map.
type alertKey struct {
	alertID  ID
	entityID ID
}

// alertDef is the internal bookkeeping for one registered alert.
type alertDef struct {
	desc    AlertDesc
	alertID ID
}

// RegisterAlert registers a new alert defined by desc and returns the alert entity ID.
//
// Internally installs a hidden monitor observer keyed off desc.Query:
//   - Entity entering the query → AlertInstance raised (RaisedAt = time.Now()).
//   - Entity leaving the query or being deleted → instance cleared.
//
// If entities already match the query at registration time they are swept
// synchronously and instances are created before RegisterAlert returns.
func RegisterAlert(w *Writer, desc AlertDesc) ID {
	alertID := w.NewEntity()
	if desc.Name != "" {
		w.SetName(alertID, desc.Name)
	}
	registerAlertInternal(w, alertID, desc)
	return alertID
}

// registerAlertInternal wires up the monitor and bookkeeping for alertID without
// allocating a new entity. Called by RegisterAlert and by UnmarshalJSON restoration.
func registerAlertInternal(w *Writer, alertID ID, desc AlertDesc) {
	world := w.scopeWorld()
	if world.alertInstances == nil {
		world.alertInstances = make(map[alertKey]*AlertInstance)
	}
	world.alertDefs = append(world.alertDefs, &alertDef{desc: desc, alertID: alertID})

	localAlertID := alertID
	localDesc := desc
	MonitorWithOptions(world, desc.Query, WithYieldExisting(), func(fw *Writer, e ID, entered bool) {
		key := alertKey{alertID: localAlertID, entityID: e}
		if entered {
			fw.scopeWorld().alertInstances[key] = &AlertInstance{
				Entity:   e,
				Alert:    localAlertID,
				Severity: localDesc.Severity,
				Message:  renderAlertMessage(localDesc.Message, e),
				RaisedAt: time.Now(),
			}
		} else {
			delete(fw.scopeWorld().alertInstances, key)
		}
	})
}

// renderAlertMessage replaces the first %d verb in msg with the uint64 entity ID.
// If no %d is present the message is returned unchanged.
func renderAlertMessage(msg string, e ID) string {
	if strings.Contains(msg, "%d") {
		return fmt.Sprintf(msg, uint64(e))
	}
	return msg
}

// Alerts returns a snapshot of all currently-active alert instances.
// Order is undefined.
func (w *World) Alerts() []AlertInstance {
	result := make([]AlertInstance, 0, len(w.alertInstances))
	for _, inst := range w.alertInstances {
		result = append(result, *inst)
	}
	return result
}

// AlertsBySeverity returns all active alert instances whose severity equals sev.
func (w *World) AlertsBySeverity(sev int) []AlertInstance {
	var result []AlertInstance
	for _, inst := range w.alertInstances {
		if inst.Severity == sev {
			result = append(result, *inst)
		}
	}
	return result
}

// AlertsForEntity returns all active alert instances for the given entity.
func (w *World) AlertsForEntity(e ID) []AlertInstance {
	var result []AlertInstance
	for _, inst := range w.alertInstances {
		if inst.Entity == e {
			result = append(result, *inst)
		}
	}
	return result
}
