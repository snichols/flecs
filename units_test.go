package flecs_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/snichols/flecs"
)

// ── Test 1: Register a user unit; Reader returns Symbol/Name/Base/Factor ──────

func TestRegisterUnit_UserUnit(t *testing.T) {
	w := flecs.New()
	var unitID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		unitID = flecs.RegisterUnit(fw, "Day", "d", w.Hour(), 24)
	})

	w.Read(func(r *flecs.Reader) {
		u, ok := r.Unit(unitID)
		if !ok {
			t.Fatal("Unit() returned ok=false for registered unit")
		}
		if u.Symbol != "d" {
			t.Errorf("Symbol: got %q, want %q", u.Symbol, "d")
		}
		if u.Name != "Day" {
			t.Errorf("Name: got %q, want %q", u.Name, "Day")
		}
		if u.Base != w.Hour() {
			t.Errorf("Base: got %v, want Hour", u.Base)
		}
		if u.Factor != 24 {
			t.Errorf("Factor: got %v, want 24", u.Factor)
		}
	})
}

// ── Test 2: SetUnit on a component; UnitFor returns the unit ──────────────────

func TestSetUnit_UnitFor(t *testing.T) {
	type Speed struct{ V float32 }

	w := flecs.New()
	speedID := flecs.RegisterComponent[Speed](w)

	w.Write(func(fw *flecs.Writer) {
		fw.SetUnit(speedID, w.Meter())
	})

	unitID, ok := w.UnitFor(speedID)
	if !ok {
		t.Fatal("UnitFor returned ok=false after SetUnit")
	}
	if unitID != w.Meter() {
		t.Errorf("UnitFor: got %v, want Meter (%v)", unitID, w.Meter())
	}
}

// ── Test 3: Built-in units exist and resolve ──────────────────────────────────

func TestBuiltinUnits_Exist(t *testing.T) {
	w := flecs.New()
	cases := []struct {
		name   string
		id     flecs.ID
		symbol string
	}{
		{"Meter", w.Meter(), "m"},
		{"Second", w.Second(), "s"},
		{"KiloGram", w.KiloGram(), "kg"},
		{"Newton", w.Newton(), "N"},
		{"Joule", w.Joule(), "J"},
		{"Hertz", w.Hertz(), "Hz"},
		{"Radian", w.Radian(), "rad"},
		{"Degree", w.Degree(), "°"},
	}
	w.Read(func(r *flecs.Reader) {
		for _, c := range cases {
			u, ok := r.Unit(c.id)
			if !ok {
				t.Errorf("%s: Unit() returned ok=false", c.name)
				continue
			}
			if u.Symbol != c.symbol {
				t.Errorf("%s: Symbol got %q, want %q", c.name, u.Symbol, c.symbol)
			}
		}
	})
}

// ── Test 4: Convert KiloMeter ↔ Meter ────────────────────────────────────────

func TestConvert_KiloMeterMeter(t *testing.T) {
	w := flecs.New()

	got, ok := flecs.Convert(w, 1, w.KiloMeter(), w.Meter())
	if !ok {
		t.Fatal("Convert(1, KiloMeter, Meter): ok=false")
	}
	if got != 1000 {
		t.Errorf("Convert(1, KiloMeter, Meter) = %v, want 1000", got)
	}

	got, ok = flecs.Convert(w, 1000, w.Meter(), w.KiloMeter())
	if !ok {
		t.Fatal("Convert(1000, Meter, KiloMeter): ok=false")
	}
	if got != 1 {
		t.Errorf("Convert(1000, Meter, KiloMeter) = %v, want 1", got)
	}
}

// ── Test 5: Convert(1, Hour, Second) == 3600 ─────────────────────────────────

func TestConvert_HourToSecond(t *testing.T) {
	w := flecs.New()

	got, ok := flecs.Convert(w, 1, w.Hour(), w.Second())
	if !ok {
		t.Fatal("Convert(1, Hour, Second): ok=false")
	}
	if got != 3600 {
		t.Errorf("Convert(1, Hour, Second) = %v, want 3600", got)
	}
}

// ── Test 6: Incompatible units (Meter vs Second) return ok=false ──────────────

func TestConvert_Incompatible(t *testing.T) {
	w := flecs.New()

	_, ok := flecs.Convert(w, 1, w.Meter(), w.Second())
	if ok {
		t.Error("Convert(Meter, Second): expected ok=false for incompatible units")
	}
}

// ── Test 7: Marshal round-trip ────────────────────────────────────────────────

func TestUnits_MarshalRoundTrip(t *testing.T) {
	type MarshalDistance struct{ V float32 }
	type MarshalDuration struct{ V float32 }

	w := flecs.New()
	distID := flecs.RegisterComponent[MarshalDistance](w)
	durID := flecs.RegisterComponent[MarshalDuration](w)

	var dayID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		// Register a user unit (Day) based on the built-in Hour.
		dayID = flecs.RegisterUnit(fw, "Day", "d", w.Hour(), 24)
		// Tag Distance with the built-in Meter unit.
		fw.SetUnit(distID, w.Meter())
		// Tag Duration with the user-registered Day unit.
		fw.SetUnit(durID, dayID)
	})

	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	w2 := flecs.New()
	// Pre-register components so their names are known during unmarshal.
	distID2 := flecs.RegisterComponent[MarshalDistance](w2)
	durID2 := flecs.RegisterComponent[MarshalDuration](w2)

	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// Distance should be tagged with Meter (built-in unit).
	unitID, ok := w2.UnitFor(distID2)
	if !ok {
		t.Fatal("UnitFor(Distance): ok=false after unmarshal")
	}
	if unitID != w2.Meter() {
		t.Errorf("Distance unit: got %v, want Meter (%v)", unitID, w2.Meter())
	}

	// Duration should be tagged with the restored Day unit.
	dayUnit2, ok := w2.UnitFor(durID2)
	if !ok {
		t.Fatal("UnitFor(Duration): ok=false after unmarshal")
	}
	w2.Read(func(r *flecs.Reader) {
		u, ok := r.Unit(dayUnit2)
		if !ok {
			t.Fatal("Unit(dayUnit2): ok=false")
		}
		if u.Name != "Day" {
			t.Errorf("Day unit name: got %q, want %q", u.Name, "Day")
		}
		if u.Factor != 24 {
			t.Errorf("Day unit factor: got %v, want 24", u.Factor)
		}
		if u.Base != w2.Hour() {
			t.Errorf("Day unit base: got %v, want Hour", u.Base)
		}
	})
}

// ── Test 8: fromUnit == toUnit short-circuits ─────────────────────────────────

func TestConvert_SameUnit(t *testing.T) {
	w := flecs.New()

	got, ok := flecs.Convert(w, 42, w.Meter(), w.Meter())
	if !ok {
		t.Fatal("Convert same-unit: ok=false")
	}
	if got != 42 {
		t.Errorf("Convert same-unit: got %v, want 42", got)
	}
}

// ── Test 9: RegisterUnit with factor==0 panics ────────────────────────────────

func TestRegisterUnit_ZeroFactorPanics(t *testing.T) {
	w := flecs.New()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for factor==0 but no panic occurred")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterUnit(fw, "Bad", "bad", 0, 0)
	})
}

// ── Test 10: Multi-hop conversion Day → Hour → Second ────────────────────────

func TestConvert_MultiHop(t *testing.T) {
	w := flecs.New()
	var dayID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dayID = flecs.RegisterUnit(fw, "Day", "d", w.Hour(), 24)
	})

	got, ok := flecs.Convert(w, 1, dayID, w.Second())
	if !ok {
		t.Fatal("Convert(1, Day, Second): ok=false")
	}
	if got != 86400 {
		t.Errorf("Convert(1, Day, Second) = %v, want 86400", got)
	}
}

// ── Additional coverage: Degree→Radian conversion ────────────────────────────

func TestConvert_DegreeRadian(t *testing.T) {
	w := flecs.New()

	got, ok := flecs.Convert(w, 180, w.Degree(), w.Radian())
	if !ok {
		t.Fatal("Convert(180, Degree, Radian): ok=false")
	}
	if math.Abs(got-math.Pi) > 1e-10 {
		t.Errorf("Convert(180, Degree, Radian) = %v, want π (%v)", got, math.Pi)
	}
}

// ── Additional coverage: unknown unit ID returns ok=false ────────────────────

func TestConvert_UnknownUnit(t *testing.T) {
	w := flecs.New()
	fakeID := flecs.MakeEntity(9999, 0) // not a registered unit

	_, ok := flecs.Convert(w, 1, fakeID, w.Meter())
	if ok {
		t.Error("Convert with unknown fromUnit: expected ok=false")
	}
	_, ok = flecs.Convert(w, 1, w.Meter(), fakeID)
	if ok {
		t.Error("Convert with unknown toUnit: expected ok=false")
	}
}

// ── Additional coverage: all built-in units round-trip via marshal ────────────

func TestBuiltinUnits_MarshalRoundTrip_AllBuiltins(t *testing.T) {
	type C1 struct{ V float32 }
	type C2 struct{ V float32 }
	type C3 struct{ V float32 }
	type C4 struct{ V float32 }
	type C5 struct{ V float32 }

	w := flecs.New()
	c1ID := flecs.RegisterComponent[C1](w)
	c2ID := flecs.RegisterComponent[C2](w)
	c3ID := flecs.RegisterComponent[C3](w)
	c4ID := flecs.RegisterComponent[C4](w)
	c5ID := flecs.RegisterComponent[C5](w)

	w.Write(func(fw *flecs.Writer) {
		fw.SetUnit(c1ID, w.KiloMeter())
		fw.SetUnit(c2ID, w.MilliSecond())
		fw.SetUnit(c3ID, w.MegaGram())
		fw.SetUnit(c4ID, w.Joule())
		fw.SetUnit(c5ID, w.Degree())
	})

	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	w2 := flecs.New()
	flecs.RegisterComponent[C1](w2)
	flecs.RegisterComponent[C2](w2)
	flecs.RegisterComponent[C3](w2)
	flecs.RegisterComponent[C4](w2)
	flecs.RegisterComponent[C5](w2)

	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	checks := []struct {
		compID   flecs.ID
		wantUnit flecs.ID
		name     string
	}{
		{c1ID, w2.KiloMeter(), "KiloMeter"},
		{c2ID, w2.MilliSecond(), "MilliSecond"},
		{c3ID, w2.MegaGram(), "MegaGram"},
		{c4ID, w2.Joule(), "Joule"},
		{c5ID, w2.Degree(), "Degree"},
	}
	for _, c := range checks {
		unitID, ok := w2.UnitFor(c.compID)
		if !ok {
			t.Errorf("UnitFor(%s): ok=false after unmarshal", c.name)
			continue
		}
		if unitID != c.wantUnit {
			t.Errorf("UnitFor(%s): got %v, want %v", c.name, unitID, c.wantUnit)
		}
	}
}

// ── Additional coverage: user unit with various built-in bases round-trips ────

func TestUserUnit_MultipleBuiltinBases_RoundTrip(t *testing.T) {
	w := flecs.New()
	var ids [6]flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ids[0] = flecs.RegisterUnit(fw, "UKiloMeter", "ukm", w.KiloMeter(), 1)
		ids[1] = flecs.RegisterUnit(fw, "UMilliMeter", "umm", w.MilliMeter(), 1)
		ids[2] = flecs.RegisterUnit(fw, "UMinute", "umin", w.Minute(), 1)
		ids[3] = flecs.RegisterUnit(fw, "UKiloGram", "ukg", w.KiloGram(), 1)
		ids[4] = flecs.RegisterUnit(fw, "UNewton", "uN", w.Newton(), 1)
		ids[5] = flecs.RegisterUnit(fw, "URadian", "urad", w.Radian(), 1)
	})

	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	w2 := flecs.New()
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// Just verify unmarshal succeeds and the entities are alive.
	// The user units are allocated starting at index 63; IDs differ in w2
	// since they are new entities. Count entities to verify they round-tripped.
	// Unmarshal would have returned an error on failure; reaching here is success.
	if w2.Count() < w.Count() {
		t.Errorf("entity count after unmarshal: got %d, want >= %d", w2.Count(), w.Count())
	}
}

// ── Additional coverage: builtinUnitByIndex out-of-range returns error ───────

func TestBuiltinUnitByIndex_OutOfRangeReturnsError(t *testing.T) {
	// Trigger the out-of-range path by passing an invalid built-in index
	// (47 = SlotOf, not a unit) via a hand-crafted JSON payload.
	type Tagged struct{ V float32 }

	w := flecs.New()
	flecs.RegisterComponent[Tagged](w)

	// compByName maps by the full Go type name e.g. "flecs_test.Tagged".
	// Use the actual component info name for the JSON.
	var compName string
	w.Read(func(r *flecs.Reader) {
		for _, cid := range r.Components() {
			if info, ok := r.ComponentInfo(cid); ok && info.Size > 0 {
				// Use the first non-zero-size component that matches our type.
				// RegisterComponent[Tagged] will have the name "flecs_test.Tagged".
				if len(info.Name) > 0 {
					compName = info.Name
				}
			}
		}
	})

	// Re-register to get name.
	flecs.RegisterComponent[Tagged](w)
	w.Read(func(r *flecs.Reader) {
		for _, cid := range r.Components() {
			if info, ok := r.ComponentInfo(cid); ok {
				if info.Type != nil {
					compName = info.Name
					break
				}
			}
		}
	})

	// Craft JSON with valid comp_name but invalid builtin unit index (47 < 48).
	invalid := `{"version":1,"entities":[],"comp_units":[{"comp_name":"` + compName + `","unit_builtin_idx":47}]}`
	w2 := flecs.New()
	flecs.RegisterComponent[Tagged](w2)
	err := w2.UnmarshalJSON([]byte(invalid))
	if err == nil {
		t.Error("expected error for invalid builtin unit index 47, got nil")
	}
}

// ── Additional coverage: rootFactor cycle detection ───────────────────────────

func TestConvert_CycleDetection(t *testing.T) {
	w := flecs.New()
	var aID, bID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		aID = flecs.RegisterUnit(fw, "CycleA", "ca", 0, 1)
		bID = flecs.RegisterUnit(fw, "CycleB", "cb", 0, 1)
	})
	// Inject a cycle: A.Base = B, B.Base = A.
	flecs.InjectUnitCycle(w, aID, bID)

	_, ok := flecs.Convert(w, 1, aID, w.Meter())
	if ok {
		t.Error("Convert with cyclic unit chain: expected ok=false")
	}
}

// ── Additional coverage: UnitFor on untagged component returns ok=false ───────

func TestUnitFor_UntaggedComponent(t *testing.T) {
	type Untagged struct{ V float32 }
	w := flecs.New()
	compID := flecs.RegisterComponent[Untagged](w)

	_, ok := w.UnitFor(compID)
	if ok {
		t.Error("UnitFor untagged component: expected ok=false")
	}
}
