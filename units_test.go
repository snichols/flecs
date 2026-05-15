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
	// The user units are allocated starting at index 75; IDs differ in w2
	// since they are new entities. Count entities to verify they round-tripped.
	// Unmarshal would have returned an error on failure; reaching here is success.
	if w2.Count() < w.Count() {
		t.Errorf("entity count after unmarshal: got %d, want >= %d", w2.Count(), w.Count())
	}
}

// ── Additional coverage: builtinUnitByIndex out-of-range returns error ───────

func TestBuiltinUnitByIndex_OutOfRangeReturnsError(t *testing.T) {
	// Trigger the out-of-range path by passing an invalid built-in index
	// (47 = DependsOn, not a unit; built-in units start at 50) via a hand-crafted JSON payload.
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

	// Craft JSON with valid comp_name but invalid builtin unit index (47 < 50).
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

// ════════════════════════════════════════════════════════════════════════════
// Phase 16.42 — Compound unit tests
// ════════════════════════════════════════════════════════════════════════════

// ── TestCompound_RegisterProduct: m·s factors decompose ──────────────────────

func TestCompound_RegisterProduct(t *testing.T) {
	w := flecs.New()
	var ms flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ms = flecs.UnitProduct(fw, "MeterSecond", w.Meter(), w.Second())
	})
	nums, denoms := flecs.UnitFactors(w, ms)
	if len(nums) != 2 {
		t.Fatalf("UnitFactors num: got %d, want 2", len(nums))
	}
	if nums[0] != w.Meter() || nums[1] != w.Second() {
		t.Errorf("UnitFactors: unexpected numerators %v", nums)
	}
	if len(denoms) != 0 {
		t.Errorf("UnitFactors denom: got %d, want 0", len(denoms))
	}
	if !flecs.IsCompound(w, ms) {
		t.Error("IsCompound returned false for compound unit")
	}
}

// ── TestCompound_RegisterQuotient: m/s num/denom split ──────────────────────

func TestCompound_RegisterQuotient(t *testing.T) {
	w := flecs.New()
	var mps flecs.ID
	w.Write(func(fw *flecs.Writer) {
		mps = flecs.UnitQuotient(fw, "MyMPS", w.Meter(), w.Second())
	})
	nums, denoms := flecs.UnitFactors(w, mps)
	if len(nums) != 1 || nums[0] != w.Meter() {
		t.Errorf("UnitFactors num: got %v, want [Meter]", nums)
	}
	if len(denoms) != 1 || denoms[0] != w.Second() {
		t.Errorf("UnitFactors denom: got %v, want [Second]", denoms)
	}
	sym := w.UnitSymbol(mps)
	if sym != "m/s" {
		t.Errorf("UnitSymbol: got %q, want %q", sym, "m/s")
	}
}

// ── TestCompound_RegisterPower: m² exponent 2 ────────────────────────────────

func TestCompound_RegisterPower(t *testing.T) {
	w := flecs.New()
	var m2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		m2 = flecs.UnitPower(fw, "MeterSquared", w.Meter(), 2)
	})
	nums, denoms := flecs.UnitFactors(w, m2)
	if len(nums) != 2 {
		t.Fatalf("UnitFactors num: got %d, want 2 (m repeated)", len(nums))
	}
	if len(denoms) != 0 {
		t.Errorf("UnitFactors denom: got %d, want 0", len(denoms))
	}
	sym := w.UnitSymbol(m2)
	if sym != "m²" {
		t.Errorf("UnitSymbol: got %q, want %q", sym, "m²")
	}
}

// ── TestCompound_NegativePower: s⁻¹ reciprocal form ─────────────────────────

func TestCompound_NegativePower(t *testing.T) {
	w := flecs.New()
	var invS flecs.ID
	w.Write(func(fw *flecs.Writer) {
		invS = flecs.UnitPower(fw, "InverseSecond", w.Second(), -1)
	})
	nums, denoms := flecs.UnitFactors(w, invS)
	if len(nums) != 0 {
		t.Errorf("UnitFactors num: got %d, want 0", len(nums))
	}
	if len(denoms) != 1 || denoms[0] != w.Second() {
		t.Errorf("UnitFactors denom: got %v, want [Second]", denoms)
	}
}

// ── TestCompound_NestedCompound: (m/s)/s flattens to m/s² ───────────────────

func TestCompound_NestedCompound(t *testing.T) {
	w := flecs.New()
	var mps, mps2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		mps = flecs.UnitQuotient(fw, "MPS", w.Meter(), w.Second())
		mps2 = flecs.UnitQuotient(fw, "MPS2", mps, w.Second())
	})
	// mps2 = (m/s) / s; siCanonical should give m/s² = {Meter:1, Second:-2}
	got, ok := flecs.Convert(w, 1.0, mps2, w.MeterPerSecondSquared())
	if !ok {
		t.Fatal("Convert(MPS2, MeterPerSecondSquared): ok=false")
	}
	if math.Abs(got-1.0) > 1e-10 {
		t.Errorf("Convert result: got %v, want 1.0", got)
	}
}

// ── TestCompound_CycleDetection: A→B, B→A → panic ───────────────────────────

func TestCompound_CycleDetection(t *testing.T) {
	w := flecs.New()
	var aID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		aID = flecs.RegisterCompoundUnit(fw, "CycleA", []flecs.ID{w.Meter()}, nil)
	})
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic for cycle detection but none occurred")
		}
	}()
	// Registering B with factor [A] creates A→B, B→A cycle: A contains Meter
	// but B's definition would walk into A which would re-enter B (being registered).
	// Actually we need A to already reference something that leads back. Register
	// A2 = [aID]; then register A3 = [A2]; then register aID-cycle by modifying...
	// Simpler: use the cycle-injection approach:
	// Register B = [aID]. Then register C = [B]. aID is compound [Meter].
	// No cycle yet. We need A = [B], B = [A]. Since A is already registered, we need
	// A's def to include B (which hasn't been registered yet).
	// Instead, prove cycle via: register A = compound[Meter], register B = compound[A],
	// then try to create a unit that references B while also including A in B's chain:
	// Actually the simplest test: B = [A], A now contains B somehow. Use the ForceInjectCompoundCycle test helper.
	// For a real API-based test:
	w.Write(func(fw *flecs.Writer) {
		// This panics: bID = [aID], then we inject cycle by trying to register aID's
		// compound as [bID], but aID is already registered. The only way to get a cycle
		// via the public API is to create B = [A] then C = [B, ???] where ??? re-enters B.
		// Let's just test: B = [A] is fine; A references B only if B references A first.
		// So: Register B first as atomic-like compound, then try A = [B].
		bID := flecs.RegisterCompoundUnit(fw, "CycleB", []flecs.ID{aID}, nil)
		// Now register C = [bID] — fine (B → A → Meter, no cycle in B's chain)
		// To create a cycle: we'd need B to reference C or C to reference something
		// that references B. Let's use a helper that modifies B's def:
		flecs.InjectCompoundCycle(w, bID, aID) // sets aID's compound def to [bID]
		// Now try to register a unit referencing aID — should panic because aID → bID → aID
		_ = flecs.RegisterCompoundUnit(fw, "CycleTrigger", []flecs.ID{aID}, nil)
	})
}

// ── TestCompound_DepthLimit: chain 9 levels → panic ──────────────────────────

func TestCompound_DepthLimit(t *testing.T) {
	w := flecs.New()
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic for depth limit exceeded but none occurred")
		}
	}()
	// Build a chain 8 deep (max allowed depth).
	var ids [9]flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ids[0] = flecs.RegisterCompoundUnit(fw, "D0", []flecs.ID{w.Meter()}, nil)
		ids[1] = flecs.RegisterCompoundUnit(fw, "D1", []flecs.ID{ids[0]}, nil)
		ids[2] = flecs.RegisterCompoundUnit(fw, "D2", []flecs.ID{ids[1]}, nil)
		ids[3] = flecs.RegisterCompoundUnit(fw, "D3", []flecs.ID{ids[2]}, nil)
		ids[4] = flecs.RegisterCompoundUnit(fw, "D4", []flecs.ID{ids[3]}, nil)
		ids[5] = flecs.RegisterCompoundUnit(fw, "D5", []flecs.ID{ids[4]}, nil)
		ids[6] = flecs.RegisterCompoundUnit(fw, "D6", []flecs.ID{ids[5]}, nil)
		ids[7] = flecs.RegisterCompoundUnit(fw, "D7", []flecs.ID{ids[6]}, nil)
		// 9th level: panics (depth 8 exceeded)
		ids[8] = flecs.RegisterCompoundUnit(fw, "D8", []flecs.ID{ids[7]}, nil)
	})
}

// ── TestCompound_Convert_VelocityUnits: Convert(100, MPS, KMPH) == 360 ───────

func TestCompound_Convert_VelocityUnits(t *testing.T) {
	w := flecs.New()
	got, ok := flecs.Convert(w, 100, w.MeterPerSecond(), w.KiloMeterPerHour())
	if !ok {
		t.Fatal("Convert(100, MeterPerSecond, KiloMeterPerHour): ok=false")
	}
	if math.Abs(got-360) > 1e-6 {
		t.Errorf("Convert result: got %v, want 360", got)
	}
}

// ── TestCompound_Convert_ForceUnits: NewtonCompound ↔ kg·m/s² identity ───────

func TestCompound_Convert_ForceUnits(t *testing.T) {
	w := flecs.New()
	// Register a dynamic kg·m/s² compound.
	var kgMs2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		kgMs2 = flecs.RegisterCompoundUnit(fw, "kgMs2",
			[]flecs.ID{w.KiloGram(), w.Meter()},
			[]flecs.ID{w.Second(), w.Second()})
	})
	got, ok := flecs.Convert(w, 1.0, w.NewtonCompound(), kgMs2)
	if !ok {
		t.Fatal("Convert(1, NewtonCompound, kgMs2): ok=false")
	}
	if math.Abs(got-1.0) > 1e-10 {
		t.Errorf("Convert result: got %v, want 1.0 (identity)", got)
	}
}

// ── TestCompound_Convert_DimensionalMismatch: Meter vs Newton → false ────────

func TestCompound_Convert_DimensionalMismatch(t *testing.T) {
	w := flecs.New()
	_, ok := flecs.Convert(w, 1, w.Meter(), w.Newton())
	if ok {
		t.Error("Convert(Meter, Newton): expected ok=false for dimensional mismatch")
	}
}

// ── TestCompound_Convert_EnergyChain: Joule ↔ Newton·Meter identity ──────────

func TestCompound_Convert_EnergyChain(t *testing.T) {
	w := flecs.New()
	// Newton·Meter = NewtonCompound(kg·m/s²) × Meter = kg·m²/s² = JouleCompound.
	var nm flecs.ID
	w.Write(func(fw *flecs.Writer) {
		nm = flecs.UnitProduct(fw, "NewtonMeter", w.NewtonCompound(), w.Meter())
	})
	got, ok := flecs.Convert(w, 1.0, nm, w.JouleCompound())
	if !ok {
		t.Fatal("Convert(NewtonMeter, JouleCompound): ok=false")
	}
	if math.Abs(got-1.0) > 1e-10 {
		t.Errorf("Convert result: got %v, want 1.0", got)
	}
}

// ── TestCompound_Symbol_Auto: auto symbol matches expected ───────────────────

func TestCompound_Symbol_Auto(t *testing.T) {
	w := flecs.New()
	var mps, accel, energy flecs.ID
	w.Write(func(fw *flecs.Writer) {
		mps = flecs.UnitQuotient(fw, "MPS2", w.Meter(), w.Second())
		accel = flecs.RegisterCompoundUnit(fw, "Accel",
			[]flecs.ID{w.Meter()}, []flecs.ID{w.Second(), w.Second()})
		energy = flecs.RegisterCompoundUnit(fw, "Energy",
			[]flecs.ID{w.KiloGram(), w.Meter(), w.Meter()},
			[]flecs.ID{w.Second(), w.Second()})
	})
	cases := []struct {
		id   flecs.ID
		want string
	}{
		{mps, "m/s"},
		{accel, "m/s²"},
		{energy, "kg·m²/s²"},
	}
	for _, c := range cases {
		sym := w.UnitSymbol(c.id)
		if sym != c.want {
			t.Errorf("UnitSymbol(%s): got %q, want %q", c.want, sym, c.want)
		}
	}
}

// ── TestCompound_Symbol_Override: explicit override wins ─────────────────────

func TestCompound_Symbol_Override(t *testing.T) {
	w := flecs.New()
	// NewtonCompound has explicit symbol "N" despite being kg·m/s².
	sym := w.UnitSymbol(w.NewtonCompound())
	if sym != "N" {
		t.Errorf("UnitSymbol(NewtonCompound): got %q, want %q", sym, "N")
	}
	sym = w.UnitSymbol(w.HertzCompound())
	if sym != "Hz" {
		t.Errorf("UnitSymbol(HertzCompound): got %q, want %q", sym, "Hz")
	}
}

// ── TestCompound_BuiltIns_AllRegistered: all 10 built-ins exist ───────────────

func TestCompound_BuiltIns_AllRegistered(t *testing.T) {
	w := flecs.New()
	cases := []struct {
		name   string
		id     flecs.ID
		index  uint32
		symbol string
	}{
		{"MeterPerSecond", w.MeterPerSecond(), 65, "m/s"},
		{"KiloMeterPerHour", w.KiloMeterPerHour(), 66, "km/h"},
		{"MeterPerSecondSquared", w.MeterPerSecondSquared(), 67, "m/s²"},
		{"NewtonCompound", w.NewtonCompound(), 68, "N"},
		{"JouleCompound", w.JouleCompound(), 69, "J"},
		{"Watt", w.Watt(), 70, "W"},
		{"Pascal", w.Pascal(), 71, "Pa"},
		{"HertzCompound", w.HertzCompound(), 72, "Hz"},
		{"RadianPerSecond", w.RadianPerSecond(), 73, "rad/s"},
		{"Inverse", w.Inverse(), 74, "1/x"},
	}
	for _, c := range cases {
		if c.id.Index() != c.index {
			t.Errorf("%s: index got %d, want %d", c.name, c.id.Index(), c.index)
		}
		sym := w.UnitSymbol(c.id)
		if sym != c.symbol {
			t.Errorf("%s: symbol got %q, want %q", c.name, sym, c.symbol)
		}
	}
}

// ── TestCompound_JSON_RoundTrip: compound units survive Marshal/Unmarshal ─────

func TestCompound_JSON_RoundTrip(t *testing.T) {
	type Force struct{ V float32 }

	w := flecs.New()
	forceID := flecs.RegisterComponent[Force](w)
	var customN, invSec, myAtomA, myAtomB, myCompound flecs.ID
	w.Write(func(fw *flecs.Writer) {
		customN = flecs.RegisterCompoundUnit(fw, "CustomNewton",
			[]flecs.ID{w.KiloGram(), w.Meter()},
			[]flecs.ID{w.Second(), w.Second()})
		fw.SetUnit(forceID, customN)

		// UnitPower(-1) sets Over=Second (builtin) → covers OverBuiltinIdx marshal path.
		// Also: nil numerators → serializeUnitFactors(nil) covers nil return path.
		invSec = flecs.UnitPower(fw, "InvSec", w.Second(), -1)

		// User atom A (root) and B with Base=A → covers BaseSerial marshal/unmarshal paths.
		myAtomA = flecs.RegisterUnit(fw, "MyAtomA", "maa", 0, 1.0)
		myAtomB = flecs.RegisterUnit(fw, "MyAtomB", "mab", myAtomA, 2.0)

		// Compound with user atom myAtomA as factor → covers user-serial in factors.
		myCompound = flecs.RegisterCompoundUnit(fw, "MyCmpd",
			[]flecs.ID{myAtomA, w.Meter()}, nil)
	})
	_ = myAtomB    // marshaled; BaseSerial path covered
	_ = invSec     // marshaled; OverBuiltinIdx + nil-num paths covered
	_ = myCompound // marshaled; user-serial factor path covered

	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	w2 := flecs.New()
	flecs.RegisterComponent[Force](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// Force component should have a unit tagged.
	forceID2 := flecs.RegisterComponent[Force](w2)
	unitID, ok := w2.UnitFor(forceID2)
	if !ok {
		t.Fatal("UnitFor(Force): ok=false after unmarshal")
	}
	// The restored unit should be a compound with same factors.
	nums, denoms := flecs.UnitFactors(w2, unitID)
	if len(nums) != 2 {
		t.Fatalf("num factors: got %d, want 2", len(nums))
	}
	if len(denoms) != 2 {
		t.Fatalf("denom factors: got %d, want 2", len(denoms))
	}
	// Verify conversion still works: 1 restored-Newton = 1 NewtonCompound.
	got, ok := flecs.Convert(w2, 1.0, unitID, w2.NewtonCompound())
	if !ok {
		t.Fatal("Convert(restored, NewtonCompound): ok=false")
	}
	if math.Abs(got-1.0) > 1e-10 {
		t.Errorf("Convert result: got %v, want 1.0", got)
	}
}

// ── TestCompound_Snapshot_RoundTrip: compound units survive snapshot ──────────

func TestCompound_Snapshot_RoundTrip(t *testing.T) {
	type Speed struct{ V float32 }

	w := flecs.New()
	speedID := flecs.RegisterComponent[Speed](w)
	var customMPS, atomicUnit flecs.ID
	w.Write(func(fw *flecs.Writer) {
		customMPS = flecs.RegisterCompoundUnit(fw, "CustomMPS",
			[]flecs.ID{w.Meter()}, []flecs.ID{w.Second()})
		fw.SetUnit(speedID, customMPS)
		// Atomic user unit: exercises the else branch in serializeUnitDefs
		// (bw.u32(0); bw.u32(0) for units with no compound factors).
		atomicUnit = flecs.RegisterUnit(fw, "AtomicUser", "au", 0, 1.0)
	})
	_ = atomicUnit

	s := flecs.TakeSnapshot(w)
	flecs.RestoreSnapshot(w, s)

	// After restore, unitDefs should still contain our compound unit.
	nums, denoms := flecs.UnitFactors(w, customMPS)
	if len(nums) != 1 || len(denoms) != 1 {
		t.Fatalf("unit factors after restore: num=%d denom=%d", len(nums), len(denoms))
	}
	// Conversion still works.
	got, ok := flecs.Convert(w, 100, customMPS, w.KiloMeterPerHour())
	if !ok {
		t.Fatal("Convert after snapshot restore: ok=false")
	}
	if math.Abs(got-360) > 1e-6 {
		t.Errorf("Convert result: got %v, want 360", got)
	}
}

// ── TestCompound_HertzAsCompound: HertzCompound = 1/Second ───────────────────

func TestCompound_HertzAsCompound(t *testing.T) {
	w := flecs.New()
	// Register RPM = 60/min = 1/min (1 cycle per minute).
	var rpm flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rpm = flecs.RegisterCompoundUnit(fw, "RPM", nil, []flecs.ID{w.Minute()})
	})
	// 60 Hz → RPM: 60 Hz = 60/s; 1 RPM = 1/60s → 60 Hz = 3600 RPM.
	got, ok := flecs.Convert(w, 60, w.HertzCompound(), rpm)
	if !ok {
		t.Fatal("Convert(60, HertzCompound, RPM): ok=false")
	}
	if math.Abs(got-3600) > 1e-6 {
		t.Errorf("Convert result: got %v, want 3600", got)
	}
}

// ── TestCompound_SetUnit_ForceOnPositionField: SetUnit works with compound ────

func TestCompound_SetUnit_ForceOnPositionField(t *testing.T) {
	type ForceComponent struct{ N float32 }
	w := flecs.New()
	forceID := flecs.RegisterComponent[ForceComponent](w)
	w.Write(func(fw *flecs.Writer) {
		fw.SetUnit(forceID, w.NewtonCompound())
	})
	unitID, ok := w.UnitFor(forceID)
	if !ok {
		t.Fatal("UnitFor(ForceComponent): ok=false")
	}
	if unitID != w.NewtonCompound() {
		t.Errorf("UnitFor: got %v, want NewtonCompound(%v)", unitID, w.NewtonCompound())
	}
	sym := w.UnitSymbol(unitID)
	if sym != "N" {
		t.Errorf("UnitSymbol: got %q, want \"N\"", sym)
	}
}

// ── TestCompound_REST_TypeInfo_ShowsCompound: UnitSymbol emits compound sym ───

func TestCompound_REST_TypeInfo_ShowsCompound(t *testing.T) {
	// Verify UnitSymbol returns compound symbol for compound-typed fields
	// (tests the accessor directly since REST integration is covered in rest_test.go).
	w := flecs.New()
	// MeterPerSecond auto-symbol.
	if sym := w.UnitSymbol(w.MeterPerSecond()); sym != "m/s" {
		t.Errorf("UnitSymbol(MeterPerSecond): got %q, want \"m/s\"", sym)
	}
	// Watt compound with explicit override.
	if sym := w.UnitSymbol(w.Watt()); sym != "W" {
		t.Errorf("UnitSymbol(Watt): got %q, want \"W\"", sym)
	}
	// Atomic unit (no compound) still works.
	if sym := w.UnitSymbol(w.Meter()); sym != "m" {
		t.Errorf("UnitSymbol(Meter): got %q, want \"m\"", sym)
	}
}

// ── TestCompound_UnitSymbol_UnknownID: returns empty string for unknown ID ───

func TestCompound_UnitSymbol_UnknownID(t *testing.T) {
	w := flecs.New()
	if sym := w.UnitSymbol(flecs.ID(9999)); sym != "" {
		t.Errorf("UnitSymbol(unknown): got %q, want \"\"", sym)
	}
}

// ── TestCompound_UnitSymbol_AutoWhenNoOverride: returns auto-symbol when Symbol is empty ───

func TestCompound_UnitSymbol_AutoWhenNoOverride(t *testing.T) {
	w := flecs.New()
	var mpsID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		mpsID = flecs.UnitQuotient(fw, "MPS2", w.Meter(), w.Second())
	})
	// No explicit symbol set — should return auto-generated "m/s".
	if sym := w.UnitSymbol(mpsID); sym != "m/s" {
		t.Errorf("UnitSymbol(auto): got %q, want \"m/s\"", sym)
	}
}

// ── TestCompound_UnitFactors_Atomic: returns nil for non-compound unit ───────

func TestCompound_UnitFactors_Atomic(t *testing.T) {
	w := flecs.New()
	nums, denoms := flecs.UnitFactors(w, w.Meter())
	if nums != nil || denoms != nil {
		t.Errorf("UnitFactors(atomic): got (%v, %v), want (nil, nil)", nums, denoms)
	}
}

// ── TestCompound_ExpMapsEqual_Mismatch: returns false for differing maps ─────

func TestCompound_ExpMapsEqual_Mismatch(t *testing.T) {
	w := flecs.New()
	// MeterPerSecond has 2 dimensions (meter, second); Meter has 1.
	// expMapsEqual len(a)=2 != len(b)=1 → return false (covers len-mismatch path).
	_, ok := flecs.Convert(w, 1.0, w.MeterPerSecond(), w.Meter())
	if ok {
		t.Error("Convert(MeterPerSecond, Meter): want false, got true")
	}
}

// ── TestCompound_Symbol_CubicMeter: exponent 3 uses Unicode superscript ──────

func TestCompound_Symbol_CubicMeter(t *testing.T) {
	w := flecs.New()
	var cubicID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		cubicID = flecs.UnitPower(fw, "CubicMeter", w.Meter(), 3)
	})
	if sym := w.UnitSymbol(cubicID); sym != "m³" {
		t.Errorf("UnitSymbol(m³): got %q, want \"m³\"", sym)
	}
}

// ── TestCompound_Symbol_FourthPower: exponent 4 uses ^N notation ─────────────

func TestCompound_Symbol_FourthPower(t *testing.T) {
	w := flecs.New()
	var fourthID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		fourthID = flecs.RegisterCompoundUnit(fw, "M4",
			[]flecs.ID{w.Meter(), w.Meter(), w.Meter(), w.Meter()}, nil)
	})
	if sym := w.UnitSymbol(fourthID); sym != "m^4" {
		t.Errorf("UnitSymbol(m^4): got %q, want \"m^4\"", sym)
	}
}

// ── TestCompound_Symbol_PureReciprocal: 1/s → "1/s" ─────────────────────────

func TestCompound_Symbol_PureReciprocal(t *testing.T) {
	w := flecs.New()
	var recipID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		recipID = flecs.UnitPower(fw, "RecipSecond", w.Second(), -1)
	})
	if sym := w.UnitSymbol(recipID); sym != "1/s" {
		t.Errorf("UnitSymbol(1/s): got %q, want \"1/s\"", sym)
	}
}

// ── TestCompound_ZeroNumeratorPanics: RegisterCompoundUnit with zero ID panics ───

func TestCompound_ZeroNumeratorPanics(t *testing.T) {
	w := flecs.New()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for zero numerator ID, got none")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterCompoundUnit(fw, "Bad", []flecs.ID{0}, nil)
	})
}

// ── TestCompound_ZeroDenominatorPanics: RegisterCompoundUnit with zero denom panics ───

func TestCompound_ZeroDenominatorPanics(t *testing.T) {
	w := flecs.New()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for zero denominator ID, got none")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterCompoundUnit(fw, "Bad", nil, []flecs.ID{0})
	})
}

// ── TestCompound_ZeroExponentPanics: UnitPower with exponent=0 panics ────────

func TestCompound_ZeroExponentPanics(t *testing.T) {
	w := flecs.New()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for zero exponent, got none")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.UnitPower(fw, "Bad", w.Meter(), 0)
	})
}

// ── TestCompound_SiCanonical_SelfRefCycle: self-referential compound → false ──

func TestCompound_SiCanonical_SelfRefCycle(t *testing.T) {
	// InjectCompoundCycle(w, aID, aID) sets aID's compound def to contain aID
	// as a numerator, forming a direct self-reference. siCanonical detects the
	// already-visited entry and returns ok=false, so Convert must return false.
	w := flecs.New()
	var aID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		aID = flecs.RegisterUnit(fw, "SelfRef", "sr", 0, 1)
	})
	flecs.InjectCompoundCycle(w, aID, aID) // aID's compound def now contains aID
	_, ok := flecs.Convert(w, 1.0, aID, w.Meter())
	if ok {
		t.Error("Convert with self-referential compound: want false, got true")
	}
}

// ── TestCompound_ZeroExpCancellation: opposite exponents cancel out ───────────

func TestCompound_ZeroExpCancellation(t *testing.T) {
	// m/m and km/km are both dimensionless: siCanonical cancels the exponent to
	// zero and deletes it (covers the delete(exponents,k) path). Both are
	// dimensionless with scale factor 1, so Convert returns 1:1.
	w := flecs.New()
	var mDivM, kmDivKm flecs.ID
	w.Write(func(fw *flecs.Writer) {
		mDivM = flecs.UnitQuotient(fw, "MDivM", w.Meter(), w.Meter())
		kmDivKm = flecs.UnitQuotient(fw, "KmDivKm", w.KiloMeter(), w.KiloMeter())
	})
	got, ok := flecs.Convert(w, 5.0, mDivM, kmDivKm)
	if !ok {
		t.Error("Convert(mDivM, kmDivKm): want ok=true, got false")
	}
	if math.Abs(got-5.0) > 1e-9 {
		t.Errorf("Convert(mDivM, kmDivKm): got %v, want 5.0", got)
	}
}

// ── TestCompound_Empty_Compound: nil/nil compound → autoSymbol returns "1" ────

func TestCompound_Empty_Compound(t *testing.T) {
	w := flecs.New()
	var emptyID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		emptyID = flecs.RegisterCompoundUnit(fw, "EmptyUnit", nil, nil)
	})
	sym := w.UnitSymbol(emptyID)
	if sym != "1" {
		t.Errorf("UnitSymbol(empty compound): got %q, want \"1\"", sym)
	}
}

// ── TestCompound_Denom_Cycle: validateCompound denom cycle panics ─────────────

func TestCompound_Denom_Cycle(t *testing.T) {
	// Inject A→B and B→A in denom, then RegisterCompoundUnit with A as denom.
	// validateCompound detects the cycle in the denom loop and panics.
	w := flecs.New()
	var aID, bID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		aID = flecs.RegisterUnit(fw, "CycleA", "ca", 0, 1)
		bID = flecs.RegisterUnit(fw, "CycleB", "cb", 0, 1)
	})
	flecs.InjectCompoundDefFull(w, aID, nil, []flecs.ID{bID})
	flecs.InjectCompoundDefFull(w, bID, nil, []flecs.ID{aID})
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for denom cycle, got none")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterCompoundUnit(fw, "CyclicQuotient", []flecs.ID{w.Meter()}, []flecs.ID{aID})
	})
}

// ── TestCompound_SiCanonical_DepthOverflow: chain deeper than maxCompoundDepth ─

func TestCompound_SiCanonical_DepthOverflow(t *testing.T) {
	// Chain: atoms[0]→atoms[1]→...→atoms[8] (injected compound defs), atoms[9] atomic.
	// siCanonical recurses 9 levels deep, exceeding maxCompoundDepth=8.
	w := flecs.New()
	atoms := make([]flecs.ID, 10)
	w.Write(func(fw *flecs.Writer) {
		for i := range atoms {
			atoms[i] = flecs.RegisterUnit(fw, "DA", "da", 0, 1)
		}
	})
	for i := 0; i < 9; i++ {
		flecs.InjectCompoundDefFull(w, atoms[i], []flecs.ID{atoms[i+1]}, nil)
	}
	_, ok := flecs.Convert(w, 1.0, atoms[0], w.Meter())
	if ok {
		t.Error("Convert with depth-overflow chain: want false, got true")
	}
}

// ── TestCompound_SiCanonical_DenomNotOk: denom with bad siCanonical ───────────

func TestCompound_SiCanonical_DenomNotOk(t *testing.T) {
	// bID self-referential denom, cID has bID as denom.
	// siCanonical(cID) → processes bID's denom → visited cycle → !ok → line 203.
	w := flecs.New()
	var bID, cID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		bID = flecs.RegisterUnit(fw, "BadDenomB", "bdb", 0, 1)
		cID = flecs.RegisterUnit(fw, "BadDenomC", "bdc", 0, 1)
	})
	flecs.InjectCompoundDefFull(w, bID, nil, []flecs.ID{bID})
	flecs.InjectCompoundDefFull(w, cID, []flecs.ID{w.Meter()}, []flecs.ID{bID})
	_, ok := flecs.Convert(w, 1.0, cID, w.Meter())
	if ok {
		t.Error("Convert with bad denom chain: want false, got true")
	}
}

// ── TestCompound_UnitSymbol_AutoFallback: Symbol=="" triggers autoSymbol ──────

func TestCompound_UnitSymbol_AutoFallback(t *testing.T) {
	// Cover UnitSymbol line 136 (return autoSymbol) and buildFactorString line 277
	// (fmt.Sprintf fallback for unknown factor ID).
	w := flecs.New()
	var cID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		cID = flecs.UnitProduct(fw, "MeterSec", w.Meter(), w.Second())
	})
	// Replace compound def to include an unknown factor ID, then clear symbol.
	flecs.InjectCompoundDefFull(w, cID, []flecs.ID{w.Meter(), flecs.ID(9999)}, nil)
	flecs.ZeroUnitSymbolForTest(w, cID)
	sym := w.UnitSymbol(cID)
	if sym == "" {
		t.Error("UnitSymbol with unknown factor: got empty string, want non-empty")
	}
}

// ── TestCompound_Snapshot_Truncated: exercises deserializeUnitDefs error paths ─

func TestCompound_Snapshot_Truncated(t *testing.T) {
	// Build a source world with a compound unit so the snapshot has a non-empty
	// unit defs section (the last serialized section).
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		flecs.RegisterCompoundUnit(fw, "CU", []flecs.ID{w.Meter()}, []flecs.ID{w.Second()})
	})
	s := flecs.TakeSnapshot(w)
	blob := flecs.SnapshotBlob(s)
	blobLen := len(blob)

	// Sweep truncations of the last 70 bytes. For each, create a fresh world,
	// build a snapshot with that world's identity but a truncated blob, and
	// attempt RestoreSnapshot — it panics on deserialize error, caught here.
	// Sweeping 70 different truncation lengths covers every br.xxx() return err
	// path in deserializeUnitDefs.
	errCount := 0
	for n := 1; n <= 70 && n <= blobLen; n++ {
		w2 := flecs.New()
		s2ID := flecs.SnapshotWorldID(flecs.TakeSnapshot(w2))
		truncSnap := flecs.NewSnapshotRaw(s2ID, blob[:blobLen-n])
		func() {
			defer func() {
				if r := recover(); r != nil {
					errCount++
				}
			}()
			flecs.RestoreSnapshot(w2, truncSnap)
		}()
	}
	if errCount == 0 {
		t.Error("expected truncated snapshot restores to fail")
	}
}
