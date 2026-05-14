package flecs

import "math"

// Unit describes the properties of a unit entity.
// Symbol is the short display string (e.g. "m", "km", "s").
// Name is the human-readable label (e.g. "Meter", "KiloMeter", "Second").
// Base is the entity of the unit this one is derived from; zero for a root/SI unit.
// Factor is the multiplier that converts one of this unit into one of the base unit
// (e.g. 1000 for KiloMeter → Meter; 3600 for Hour → Second).
// Root units carry Factor=1 and Base=0.
type Unit struct {
	Symbol string
	Name   string
	Base   ID
	Factor float64
}

// RegisterUnit registers a new unit entity and returns its ID.
// name is stored as the entity name and in the Unit descriptor.
// symbol is the short display string (caller-supplied; no normalization).
// base is the entity of the base unit; use 0 for a root/SI unit.
// factor is the multiplier that converts one of this unit into one of the base unit.
// Panics if factor == 0 (a zero factor would make Convert produce a division-by-zero).
func RegisterUnit(w *Writer, name, symbol string, base ID, factor float64) ID {
	if factor == 0 {
		panic("flecs: RegisterUnit: factor must be non-zero")
	}
	world := w.scopeWorld()
	e := w.NewEntity()
	w.SetName(e, name)
	world.unitDefs[e] = Unit{Symbol: symbol, Name: name, Base: base, Factor: factor}
	return e
}

// UnitFor returns the unit entity attached to componentID, if any.
func (w *World) UnitFor(componentID ID) (ID, bool) {
	unitID, ok := w.componentUnits[componentID]
	return unitID, ok
}

// SetUnit tags componentID with unitID so that UnitFor returns unitID.
// Applied directly to world state; no deferral.
func (fw *Writer) SetUnit(componentID ID, unitID ID) {
	fw.scopeWorld().componentUnits[componentID] = unitID
}

// Convert converts value from fromUnit to toUnit by walking each unit's Base chain
// to their common root. Returns ok=false when the units have different root bases
// (incompatible physical dimensions) or when either ID is not a registered unit.
//
// Special case: fromUnit == toUnit returns (value, true) with no lookup.
// Multi-hop example: Day (Base=Hour, Factor=24) → Second resolves as
// Day→Hour→Second, giving factor = 24×3600 = 86400.
func Convert(w *World, value float64, fromUnit, toUnit ID) (float64, bool) {
	if fromUnit == toUnit {
		return value, true
	}
	fromFactor, fromRoot, ok := rootFactor(w, fromUnit)
	if !ok {
		return 0, false
	}
	toFactor, toRoot, ok := rootFactor(w, toUnit)
	if !ok {
		return 0, false
	}
	if fromRoot != toRoot {
		return 0, false
	}
	return value * fromFactor / toFactor, true
}

// rootFactor walks unitID's Base chain and returns the cumulative factor to the
// root base unit (Base==0) and the root ID. Returns ok=false if unitID is not in
// unitDefs or a cycle is detected.
func rootFactor(w *World, unitID ID) (factor float64, root ID, ok bool) {
	factor = 1.0
	current := unitID
	seen := make(map[ID]bool)
	for {
		if seen[current] {
			return 0, 0, false
		}
		seen[current] = true
		u, exists := w.unitDefs[current]
		if !exists {
			return 0, 0, false
		}
		if u.Base == 0 {
			return factor, current, true
		}
		factor *= u.Factor
		current = u.Base
	}
}

// bootstrapBuiltinUnits initializes unitDefs and componentUnits in w and
// populates the 15 built-in unit definitions. Called from New() after all
// built-in unit entity IDs (indices 48–62) have been allocated.
func bootstrapBuiltinUnits(w *World) {
	w.unitDefs = make(map[ID]Unit, 20)
	w.componentUnits = make(map[ID]ID)

	// Length
	w.unitDefs[w.meterID] = Unit{Symbol: "m", Name: "Meter", Factor: 1}
	w.unitDefs[w.kiloMeterID] = Unit{Symbol: "km", Name: "KiloMeter", Base: w.meterID, Factor: 1000}
	w.unitDefs[w.milliMeterID] = Unit{Symbol: "mm", Name: "MilliMeter", Base: w.meterID, Factor: 0.001}
	// Duration
	w.unitDefs[w.secondID] = Unit{Symbol: "s", Name: "Second", Factor: 1}
	w.unitDefs[w.milliSecondID] = Unit{Symbol: "ms", Name: "MilliSecond", Base: w.secondID, Factor: 0.001}
	w.unitDefs[w.minuteID] = Unit{Symbol: "min", Name: "Minute", Base: w.secondID, Factor: 60}
	w.unitDefs[w.hourID] = Unit{Symbol: "h", Name: "Hour", Base: w.secondID, Factor: 3600}
	// Mass
	w.unitDefs[w.gramID] = Unit{Symbol: "g", Name: "Gram", Factor: 1}
	w.unitDefs[w.kiloGramID] = Unit{Symbol: "kg", Name: "KiloGram", Base: w.gramID, Factor: 1000}
	w.unitDefs[w.megaGramID] = Unit{Symbol: "Mg", Name: "MegaGram", Base: w.gramID, Factor: 1_000_000}
	// Force, Energy, Frequency — opaque root units (no compound support in v1)
	w.unitDefs[w.newtonID] = Unit{Symbol: "N", Name: "Newton", Factor: 1}
	w.unitDefs[w.jouleID] = Unit{Symbol: "J", Name: "Joule", Factor: 1}
	w.unitDefs[w.hertzID] = Unit{Symbol: "Hz", Name: "Hertz", Factor: 1}
	// Angle
	w.unitDefs[w.radianID] = Unit{Symbol: "rad", Name: "Radian", Factor: 1}
	w.unitDefs[w.degreeID] = Unit{Symbol: "°", Name: "Degree", Base: w.radianID, Factor: math.Pi / 180}
}

// builtinUnitByIndex returns the world's built-in unit entity ID for the given
// raw entity index (48–62). Returns 0 if the index does not correspond to a
// built-in unit.
func (w *World) builtinUnitByIndex(idx uint32) ID {
	if idx < 48 || idx > 62 {
		return 0
	}
	units := [15]ID{
		w.meterID, w.kiloMeterID, w.milliMeterID,
		w.secondID, w.milliSecondID, w.minuteID, w.hourID,
		w.gramID, w.kiloGramID, w.megaGramID,
		w.newtonID, w.jouleID, w.hertzID,
		w.radianID, w.degreeID,
	}
	return units[idx-48]
}

// isBuiltinUnit reports whether unitID is one of the 15 built-in unit entities
// (raw entity index 48–62).
func isBuiltinUnit(unitID ID) bool {
	idx := unitID.Index()
	return idx >= 48 && idx <= 62
}

// ── Built-in unit accessors ────────────────────────────────────────────────────

// Meter returns the ID of the built-in Meter length unit entity (index 48).
func (w *World) Meter() ID { return w.meterID }

// KiloMeter returns the ID of the built-in KiloMeter length unit entity (index 49).
// Factor=1000 relative to Meter.
func (w *World) KiloMeter() ID { return w.kiloMeterID }

// MilliMeter returns the ID of the built-in MilliMeter length unit entity (index 50).
// Factor=0.001 relative to Meter.
func (w *World) MilliMeter() ID { return w.milliMeterID }

// Second returns the ID of the built-in Second duration unit entity (index 51).
func (w *World) Second() ID { return w.secondID }

// MilliSecond returns the ID of the built-in MilliSecond duration unit entity (index 52).
// Factor=0.001 relative to Second.
func (w *World) MilliSecond() ID { return w.milliSecondID }

// Minute returns the ID of the built-in Minute duration unit entity (index 53).
// Factor=60 relative to Second.
func (w *World) Minute() ID { return w.minuteID }

// Hour returns the ID of the built-in Hour duration unit entity (index 54).
// Factor=3600 relative to Second.
func (w *World) Hour() ID { return w.hourID }

// Gram returns the ID of the built-in Gram mass unit entity (index 55).
func (w *World) Gram() ID { return w.gramID }

// KiloGram returns the ID of the built-in KiloGram mass unit entity (index 56).
// Factor=1000 relative to Gram.
func (w *World) KiloGram() ID { return w.kiloGramID }

// MegaGram returns the ID of the built-in MegaGram mass unit entity (index 57).
// Factor=1_000_000 relative to Gram.
func (w *World) MegaGram() ID { return w.megaGramID }

// Newton returns the ID of the built-in Newton force unit entity (index 58).
// Opaque root unit in v1 (compound unit support deferred to Phase 16.30.1).
func (w *World) Newton() ID { return w.newtonID }

// Joule returns the ID of the built-in Joule energy unit entity (index 59).
// Opaque root unit in v1 (compound unit support deferred to Phase 16.30.1).
func (w *World) Joule() ID { return w.jouleID }

// Hertz returns the ID of the built-in Hertz frequency unit entity (index 60).
// Opaque root unit in v1 (compound unit support deferred to Phase 16.30.1).
func (w *World) Hertz() ID { return w.hertzID }

// Radian returns the ID of the built-in Radian angle unit entity (index 61).
func (w *World) Radian() ID { return w.radianID }

// Degree returns the ID of the built-in Degree angle unit entity (index 62).
// Factor=math.Pi/180 relative to Radian (1° = π/180 rad).
func (w *World) Degree() ID { return w.degreeID }
