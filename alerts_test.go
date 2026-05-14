package flecs_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	flecs "github.com/snichols/flecs"
)

// ---- component types used by alert tests ----

type alertHealth struct{ HP int }
type alertDead struct{}
type alertSpeed struct{ V float32 }

// ---- helpers ----

func newAlertWorld() *flecs.World { return flecs.New() }

// ---- Test 1: register alert, create matching entity → instance present ----

func TestAlertRegisterAndMatch(t *testing.T) {
	w := newAlertWorld()
	healthID := flecs.RegisterComponent[alertHealth](w)

	var alertID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		alertID = flecs.RegisterAlert(fw, flecs.AlertDesc{
			Name:     "LowHealthAlert",
			Query:    []flecs.Term{flecs.With(healthID)},
			Severity: flecs.AlertWarning,
			Message:  "health warning",
		})
	})
	if alertID == 0 {
		t.Fatal("RegisterAlert returned zero ID")
	}

	if len(w.Alerts()) != 0 {
		t.Fatalf("before any entity: want 0 instances, got %d", len(w.Alerts()))
	}

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, alertHealth{HP: 5})
	})

	got := w.Alerts()
	if len(got) != 1 {
		t.Fatalf("after match: want 1 instance, got %d", len(got))
	}
	inst := got[0]
	if inst.Entity != e {
		t.Errorf("Entity: want %v, got %v", e, inst.Entity)
	}
	if inst.Alert != alertID {
		t.Errorf("Alert: want %v, got %v", alertID, inst.Alert)
	}
	if inst.Severity != flecs.AlertWarning {
		t.Errorf("Severity: want AlertWarning, got %d", inst.Severity)
	}
	if inst.Message != "health warning" {
		t.Errorf("Message: want %q, got %q", "health warning", inst.Message)
	}
	if inst.RaisedAt.IsZero() {
		t.Error("RaisedAt must not be zero")
	}
	if inst.RaisedAt.After(time.Now().Add(time.Second)) {
		t.Error("RaisedAt is in the future")
	}
}

// ---- Test 2: modify entity so it no longer matches → instance cleared ----

func TestAlertClearedOnNoLongerMatch(t *testing.T) {
	w := newAlertWorld()
	healthID := flecs.RegisterComponent[alertHealth](w)

	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterAlert(fw, flecs.AlertDesc{
			Query:    []flecs.Term{flecs.With(healthID)},
			Severity: flecs.AlertInfo,
			Message:  "has health",
		})
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, alertHealth{HP: 10})
	})
	if len(w.Alerts()) != 1 {
		t.Fatalf("after add: want 1 instance, got %d", len(w.Alerts()))
	}

	w.Write(func(fw *flecs.Writer) { flecs.Remove[alertHealth](fw, e) })
	if len(w.Alerts()) != 0 {
		t.Fatalf("after remove: want 0 instances, got %d", len(w.Alerts()))
	}
}

// ---- Test 3: delete entity → instance cleared ----

func TestAlertClearedOnDelete(t *testing.T) {
	w := newAlertWorld()
	healthID := flecs.RegisterComponent[alertHealth](w)

	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterAlert(fw, flecs.AlertDesc{
			Query:    []flecs.Term{flecs.With(healthID)},
			Severity: flecs.AlertError,
			Message:  "alive",
		})
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, alertHealth{HP: 3})
	})
	if len(w.Alerts()) != 1 {
		t.Fatalf("before delete: want 1 instance, got %d", len(w.Alerts()))
	}

	w.Write(func(fw *flecs.Writer) { fw.Delete(e) })
	if len(w.Alerts()) != 0 {
		t.Fatalf("after delete: want 0 instances, got %d", len(w.Alerts()))
	}
}

// ---- Test 4: multiple matching entities → one instance per entity ----

func TestAlertMultipleEntities(t *testing.T) {
	w := newAlertWorld()
	healthID := flecs.RegisterComponent[alertHealth](w)

	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterAlert(fw, flecs.AlertDesc{
			Query:    []flecs.Term{flecs.With(healthID)},
			Severity: flecs.AlertWarning,
			Message:  "multi",
		})
	})

	var e1, e2, e3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, alertHealth{HP: 1})
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, alertHealth{HP: 2})
		e3 = fw.NewEntity()
		flecs.Set(fw, e3, alertHealth{HP: 3})
	})

	got := w.Alerts()
	if len(got) != 3 {
		t.Fatalf("want 3 instances, got %d", len(got))
	}
	entities := make(map[flecs.ID]bool)
	for _, inst := range got {
		entities[inst.Entity] = true
	}
	for _, e := range []flecs.ID{e1, e2, e3} {
		if !entities[e] {
			t.Errorf("missing instance for entity %v", e)
		}
	}
}

// ---- Test 5: multiple alerts with overlapping queries → all matching alerts fire ----

func TestAlertMultipleAlertsOverlappingQuery(t *testing.T) {
	w := newAlertWorld()
	healthID := flecs.RegisterComponent[alertHealth](w)

	var a1, a2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a1 = flecs.RegisterAlert(fw, flecs.AlertDesc{
			Query:    []flecs.Term{flecs.With(healthID)},
			Severity: flecs.AlertWarning,
			Message:  "warning",
		})
		a2 = flecs.RegisterAlert(fw, flecs.AlertDesc{
			Query:    []flecs.Term{flecs.With(healthID)},
			Severity: flecs.AlertError,
			Message:  "error",
		})
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, alertHealth{HP: 0})
	})

	got := w.Alerts()
	if len(got) != 2 {
		t.Fatalf("want 2 instances (one per alert), got %d", len(got))
	}
	alerts := make(map[flecs.ID]bool)
	for _, inst := range got {
		if inst.Entity != e {
			t.Errorf("instance entity: want %v, got %v", e, inst.Entity)
		}
		alerts[inst.Alert] = true
	}
	if !alerts[a1] {
		t.Error("missing instance for alert a1")
	}
	if !alerts[a2] {
		t.Error("missing instance for alert a2")
	}
}

// ---- Test 6: round-trip each severity constant ----

func TestAlertSeverityConstants(t *testing.T) {
	severities := []struct {
		val  int
		name string
	}{
		{flecs.AlertInfo, "AlertInfo"},
		{flecs.AlertWarning, "AlertWarning"},
		{flecs.AlertError, "AlertError"},
		{flecs.AlertCritical, "AlertCritical"},
	}
	// Verify ordering
	if flecs.AlertInfo >= flecs.AlertWarning {
		t.Error("AlertInfo must be less than AlertWarning")
	}
	if flecs.AlertWarning >= flecs.AlertError {
		t.Error("AlertWarning must be less than AlertError")
	}
	if flecs.AlertError >= flecs.AlertCritical {
		t.Error("AlertError must be less than AlertCritical")
	}

	for _, tc := range severities {
		w := newAlertWorld()
		healthID := flecs.RegisterComponent[alertHealth](w)

		var alertID flecs.ID
		w.Write(func(fw *flecs.Writer) {
			alertID = flecs.RegisterAlert(fw, flecs.AlertDesc{
				Query:    []flecs.Term{flecs.With(healthID)},
				Severity: tc.val,
				Message:  tc.name,
			})
		})
		_ = alertID

		var e flecs.ID
		w.Write(func(fw *flecs.Writer) {
			e = fw.NewEntity()
			flecs.Set(fw, e, alertHealth{HP: 1})
		})

		got := w.Alerts()
		if len(got) != 1 {
			t.Fatalf("%s: want 1 instance, got %d", tc.name, len(got))
		}
		if got[0].Severity != tc.val {
			t.Errorf("%s: severity want %d, got %d", tc.name, tc.val, got[0].Severity)
		}
		if got[0].Message != tc.name {
			t.Errorf("%s: message want %q, got %q", tc.name, tc.name, got[0].Message)
		}
	}
}

// ---- Test 7: AlertsBySeverity filters correctly ----

func TestAlertsBySeverity(t *testing.T) {
	w := newAlertWorld()
	healthID := flecs.RegisterComponent[alertHealth](w)
	speedID := flecs.RegisterComponent[alertSpeed](w)

	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterAlert(fw, flecs.AlertDesc{
			Query:    []flecs.Term{flecs.With(healthID)},
			Severity: flecs.AlertWarning,
			Message:  "warn",
		})
		flecs.RegisterAlert(fw, flecs.AlertDesc{
			Query:    []flecs.Term{flecs.With(speedID)},
			Severity: flecs.AlertError,
			Message:  "err",
		})
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, alertHealth{HP: 1})
		flecs.Set(fw, e, alertSpeed{V: 2.0})
	})

	warnings := w.AlertsBySeverity(flecs.AlertWarning)
	if len(warnings) != 1 || warnings[0].Severity != flecs.AlertWarning {
		t.Errorf("AlertsBySeverity(Warning): want 1, got %v", warnings)
	}

	errors := w.AlertsBySeverity(flecs.AlertError)
	if len(errors) != 1 || errors[0].Severity != flecs.AlertError {
		t.Errorf("AlertsBySeverity(Error): want 1, got %v", errors)
	}

	infos := w.AlertsBySeverity(flecs.AlertInfo)
	if len(infos) != 0 {
		t.Errorf("AlertsBySeverity(Info): want 0, got %d", len(infos))
	}
}

// ---- Test 8: AlertsForEntity filters correctly ----

func TestAlertsForEntity(t *testing.T) {
	w := newAlertWorld()
	healthID := flecs.RegisterComponent[alertHealth](w)

	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterAlert(fw, flecs.AlertDesc{
			Query:    []flecs.Term{flecs.With(healthID)},
			Severity: flecs.AlertWarning,
			Message:  "has health",
		})
	})

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, alertHealth{HP: 1})
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, alertHealth{HP: 2})
	})

	forE1 := w.AlertsForEntity(e1)
	if len(forE1) != 1 || forE1[0].Entity != e1 {
		t.Errorf("AlertsForEntity(e1): want 1 with Entity=e1, got %v", forE1)
	}

	forE2 := w.AlertsForEntity(e2)
	if len(forE2) != 1 || forE2[0].Entity != e2 {
		t.Errorf("AlertsForEntity(e2): want 1 with Entity=e2, got %v", forE2)
	}

	var eOther flecs.ID
	w.Write(func(fw *flecs.Writer) { eOther = fw.NewEntity() })
	if len(w.AlertsForEntity(eOther)) != 0 {
		t.Error("AlertsForEntity(non-matching): want 0 instances")
	}
}

// ---- Test 9: %d interpolation substitutes entity ID ----

func TestAlertMessageInterpolation(t *testing.T) {
	w := newAlertWorld()
	healthID := flecs.RegisterComponent[alertHealth](w)

	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterAlert(fw, flecs.AlertDesc{
			Query:    []flecs.Term{flecs.With(healthID)},
			Severity: flecs.AlertWarning,
			Message:  "entity %d is low",
		})
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, alertHealth{HP: 1})
	})

	got := w.Alerts()
	if len(got) != 1 {
		t.Fatalf("want 1 instance, got %d", len(got))
	}
	want := fmt.Sprintf("entity %d is low", uint64(e))
	if got[0].Message != want {
		t.Errorf("interpolated message: want %q, got %q", want, got[0].Message)
	}
}

// ---- Test 10: marshal round-trip — definitions survive, instances recomputed ----

func TestAlertMarshalRoundTrip(t *testing.T) {
	w := newAlertWorld()
	healthID := flecs.RegisterComponent[alertHealth](w)
	deadID := flecs.RegisterComponent[alertDead](w)

	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterAlert(fw, flecs.AlertDesc{
			Name:     "HealthAlert",
			Query:    []flecs.Term{flecs.With(healthID), flecs.Without(deadID)},
			Severity: flecs.AlertWarning,
			Message:  "health alert for %d",
		})
	})

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, alertHealth{HP: 5})
		// e1 matches (has Health, no Dead)

		e2 = fw.NewEntity()
		flecs.Set(fw, e2, alertHealth{HP: 0})
		flecs.Set(fw, e2, alertDead{})
		// e2 does not match (has Dead)
	})

	beforeInstances := w.Alerts()
	if len(beforeInstances) != 1 || beforeInstances[0].Entity != e1 {
		t.Fatalf("before marshal: want 1 instance for e1, got %v", beforeInstances)
	}

	// Marshal
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	// Verify alert definition is in JSON
	if !strings.Contains(string(data), "HealthAlert") {
		t.Error("marshaled JSON should contain alert name")
	}

	// Unmarshal into a fresh world with the same component types registered
	w2 := flecs.New()
	_ = flecs.RegisterComponent[alertHealth](w2)
	_ = flecs.RegisterComponent[alertDead](w2)
	if err := json.Unmarshal(data, w2); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// Alert instances are recomputed from entity state via WithYieldExisting
	afterInstances := w2.Alerts()
	if len(afterInstances) != 1 {
		t.Fatalf("after unmarshal: want 1 instance (e1 only), got %d", len(afterInstances))
	}

	inst := afterInstances[0]
	if inst.Alert == 0 {
		t.Error("Alert ID must be non-zero after restore")
	}
	if inst.Severity != flecs.AlertWarning {
		t.Errorf("severity after restore: want AlertWarning, got %d", inst.Severity)
	}
	if !strings.Contains(inst.Message, "health alert for") {
		t.Errorf("message after restore: unexpected %q", inst.Message)
	}
}

// ---- Test 10b: marshal with unregistered alert component → unmarshal error ----

func TestAlertMarshalUnregisteredComponentError(t *testing.T) {
	w := newAlertWorld()
	healthID := flecs.RegisterComponent[alertHealth](w)

	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterAlert(fw, flecs.AlertDesc{
			Query:    []flecs.Term{flecs.With(healthID)},
			Severity: flecs.AlertInfo,
			Message:  "health",
		})
	})

	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	// Unmarshal into a world that does NOT have alertHealth registered → error
	w2 := flecs.New()
	// deliberately do NOT register alertHealth
	err = json.Unmarshal(data, w2)
	if err == nil {
		t.Fatal("UnmarshalJSON: expected error for unregistered alert component, got nil")
	}
	if !strings.Contains(err.Error(), "alert term component") {
		t.Errorf("error message should mention alert term component, got: %v", err)
	}
}

// ---- Test 10c: unmarshal JSON with invalid alert entity serial → error ----

func TestAlertMarshalInvalidEntitySerialError(t *testing.T) {
	// Craft JSON that references a non-existent entity serial for an alert.
	badJSON := `{"version":1,"entities":[],` +
		`"alerts":[{"entity_serial":999,"severity":1,"message":"x",` +
		`"terms":[{"kind":0,"component_name":"flecs_test.alertHealth"}]}]}`

	w := flecs.New()
	flecs.RegisterComponent[alertHealth](w)
	err := json.Unmarshal([]byte(badJSON), w)
	if err == nil {
		t.Fatal("expected error for unknown alert entity serial, got nil")
	}
	if !strings.Contains(err.Error(), "alert entity serial") {
		t.Errorf("error should mention alert entity serial, got: %v", err)
	}
}

// ---- Test 11: alert with multi-term query (AND + NOT) ----

func TestAlertMultiTermQuery(t *testing.T) {
	w := newAlertWorld()
	healthID := flecs.RegisterComponent[alertHealth](w)
	deadID := flecs.RegisterComponent[alertDead](w)

	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterAlert(fw, flecs.AlertDesc{
			Query:    []flecs.Term{flecs.With(healthID), flecs.Without(deadID)},
			Severity: flecs.AlertWarning,
			Message:  "alive with health",
		})
	})

	var alive, dead flecs.ID
	w.Write(func(fw *flecs.Writer) {
		alive = fw.NewEntity()
		flecs.Set(fw, alive, alertHealth{HP: 10})

		dead = fw.NewEntity()
		flecs.Set(fw, dead, alertHealth{HP: 10})
		flecs.Set(fw, dead, alertDead{})
	})

	got := w.Alerts()
	if len(got) != 1 || got[0].Entity != alive {
		t.Fatalf("want 1 instance for alive entity, got %v", got)
	}

	// Add Dead to alive → instance should clear
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, alive, alertDead{})
	})
	if len(w.Alerts()) != 0 {
		t.Fatal("after adding Dead: want 0 instances")
	}
}

// ---- Test 12: static message (no %d) ----

func TestAlertStaticMessage(t *testing.T) {
	w := newAlertWorld()
	healthID := flecs.RegisterComponent[alertHealth](w)

	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterAlert(fw, flecs.AlertDesc{
			Query:    []flecs.Term{flecs.With(healthID)},
			Severity: flecs.AlertInfo,
			Message:  "static alert message",
		})
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, alertHealth{HP: 1})
	})
	_ = e

	got := w.Alerts()
	if len(got) != 1 {
		t.Fatalf("want 1 instance, got %d", len(got))
	}
	if got[0].Message != "static alert message" {
		t.Errorf("static message: want %q, got %q", "static alert message", got[0].Message)
	}
}
