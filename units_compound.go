package flecs

import (
	"fmt"
	"strings"
)

const maxCompoundDepth = 8

// compoundDef stores the numerator and denominator unit IDs for a compound unit.
// Powers are expressed by repeating an ID (e.g., m/s² → denominators=[Second,Second]).
type compoundDef struct {
	numerators   []ID
	denominators []ID
}

// RegisterCompoundUnit registers a new compound unit entity (product/quotient form).
// Panics if any num or denom ID is zero, if a cycle is detected, or if compound
// depth exceeds maxCompoundDepth (8).
func RegisterCompoundUnit(fw *Writer, name string, num []ID, denom []ID) ID {
	w := fw.scopeWorld()
	for _, id := range num {
		if id == 0 {
			panic("flecs: RegisterCompoundUnit: zero ID in numerators")
		}
	}
	for _, id := range denom {
		if id == 0 {
			panic("flecs: RegisterCompoundUnit: zero ID in denominators")
		}
	}
	e := fw.NewEntity()
	fw.SetName(e, name)

	def := &compoundDef{
		numerators:   append([]ID(nil), num...),
		denominators: append([]ID(nil), denom...),
	}
	// Validate depth and cycles before committing.
	stack := map[ID]bool{e: true}
	for _, id := range num {
		if err := validateCompound(w, id, stack, 1); err != nil {
			panic(fmt.Sprintf("flecs: RegisterCompoundUnit %q: %v", name, err))
		}
	}
	for _, id := range denom {
		if err := validateCompound(w, id, stack, 1); err != nil {
			panic(fmt.Sprintf("flecs: RegisterCompoundUnit %q: %v", name, err))
		}
	}

	sym := autoSymbol(w, def)
	w.unitDefs[e] = Unit{Symbol: sym, Name: name, Factor: 1}
	w.compoundDefs[e] = def
	return e
}

// UnitProduct registers a compound unit that is the product a*b.
func UnitProduct(fw *Writer, name string, a, b ID) ID {
	return RegisterCompoundUnit(fw, name, []ID{a, b}, nil)
}

// UnitQuotient registers a compound unit num/denom.
func UnitQuotient(fw *Writer, name string, num, denom ID) ID {
	e := RegisterCompoundUnit(fw, name, []ID{num}, []ID{denom})
	w := fw.scopeWorld()
	u := w.unitDefs[e]
	u.Over = denom
	u.Power = 1
	w.unitDefs[e] = u
	return e
}

// UnitPower registers a compound unit base^exponent.
// Positive exponent: base repeated in numerators.
// Negative exponent: base repeated in denominators.
// Zero exponent panics.
func UnitPower(fw *Writer, name string, base ID, exponent int8) ID {
	if exponent == 0 {
		panic("flecs: UnitPower: exponent must be non-zero")
	}
	var num, denom []ID
	if exponent > 0 {
		for i := int8(0); i < exponent; i++ {
			num = append(num, base)
		}
	} else {
		for i := int8(0); i > exponent; i-- {
			denom = append(denom, base)
		}
	}
	e := RegisterCompoundUnit(fw, name, num, denom)
	w := fw.scopeWorld()
	u := w.unitDefs[e]
	u.Power = exponent
	if exponent < 0 {
		u.Over = base
	}
	w.unitDefs[e] = u
	return e
}

// IsCompound reports whether unitID is a compound unit (registered via RegisterCompoundUnit).
func IsCompound(w *World, unitID ID) bool {
	_, ok := w.compoundDefs[unitID]
	return ok
}

// UnitFactors decomposes a compound unit into its numerator and denominator unit IDs.
// Returns nil slices for atomic units.
func UnitFactors(w *World, unitID ID) (numerators []ID, denominators []ID) {
	def, ok := w.compoundDefs[unitID]
	if !ok {
		return nil, nil
	}
	return append([]ID(nil), def.numerators...), append([]ID(nil), def.denominators...)
}

// UnitSymbol returns the display symbol for unitID — the explicit override if set,
// otherwise the auto-generated compound symbol, or the atomic Symbol field.
func (w *World) UnitSymbol(unitID ID) string {
	u, ok := w.unitDefs[unitID]
	if !ok {
		return ""
	}
	def, isCompound := w.compoundDefs[unitID]
	if !isCompound {
		return u.Symbol
	}
	if u.Symbol != "" {
		return u.Symbol
	}
	return autoSymbol(w, def)
}

// validateCompound walks the compound def for id and verifies no cycle or depth overflow.
func validateCompound(w *World, id ID, stack map[ID]bool, depth int) error {
	if depth > maxCompoundDepth {
		return fmt.Errorf("compound depth exceeds maximum (%d)", maxCompoundDepth)
	}
	if stack[id] {
		return fmt.Errorf("cycle detected involving unit %v", id)
	}
	def, isCompound := w.compoundDefs[id]
	if !isCompound {
		return nil
	}
	stack[id] = true
	defer func() { delete(stack, id) }()
	for _, f := range def.numerators {
		if err := validateCompound(w, f, stack, depth+1); err != nil {
			return err
		}
	}
	for _, f := range def.denominators {
		if err := validateCompound(w, f, stack, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// siCanonical returns the SI-canonical exponent map and scale factor for unitID.
// Atomic units contribute {root: 1} with their cumulative chain factor.
// Compound units contribute the sum/difference of their factor canonicals.
// Returns ok=false on unknown unit, cycle, or depth overflow.
func siCanonical(w *World, unitID ID, depth int, visited map[ID]bool) (exponents map[ID]int, factor float64, ok bool) {
	if depth > maxCompoundDepth {
		return nil, 0, false
	}
	if visited[unitID] {
		return nil, 0, false
	}
	def, isCompound := w.compoundDefs[unitID]
	if !isCompound {
		// Atomic unit: walk base chain.
		f, root, ok2 := rootFactor(w, unitID)
		if !ok2 {
			return nil, 0, false
		}
		return map[ID]int{root: 1}, f, true
	}
	visited[unitID] = true
	defer func() { delete(visited, unitID) }()

	exponents = make(map[ID]int)
	factor = 1.0
	for _, numID := range def.numerators {
		exp, f, ok2 := siCanonical(w, numID, depth+1, visited)
		if !ok2 {
			return nil, 0, false
		}
		for k, v := range exp {
			exponents[k] += v
		}
		factor *= f
	}
	for _, denomID := range def.denominators {
		exp, f, ok2 := siCanonical(w, denomID, depth+1, visited)
		if !ok2 {
			return nil, 0, false
		}
		for k, v := range exp {
			exponents[k] -= v
		}
		factor /= f
	}
	// Remove zero exponents.
	for k, v := range exponents {
		if v == 0 {
			delete(exponents, k)
		}
	}
	return exponents, factor, true
}

// expMapsEqual returns true when two exponent maps have identical key-value pairs.
func expMapsEqual(a, b map[ID]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// autoSymbol generates a display string for a compound unit def.
// Numerator symbols are joined with "·"; denominator symbols are joined similarly
// with exponent superscripts (² for 2, ³ for 3, ^n for higher).
func autoSymbol(w *World, def *compoundDef) string {
	numStr := buildFactorString(w, def.numerators)
	denomStr := buildFactorString(w, def.denominators)
	if denomStr == "" {
		if numStr == "" {
			return "1"
		}
		return numStr
	}
	if numStr == "" {
		numStr = "1"
	}
	return numStr + "/" + denomStr
}

// buildFactorString collapses an ID list into a symbol string with exponent notation.
func buildFactorString(w *World, ids []ID) string {
	if len(ids) == 0 {
		return ""
	}
	// Count occurrences while preserving first-seen order.
	type entry struct {
		id    ID
		count int
	}
	var order []entry
	seen := make(map[ID]int)
	for _, id := range ids {
		if idx, ok := seen[id]; ok {
			order[idx].count++
		} else {
			seen[id] = len(order)
			order = append(order, entry{id, 1})
		}
	}
	parts := make([]string, 0, len(order))
	for _, e := range order {
		sym := ""
		if u, ok := w.unitDefs[e.id]; ok {
			sym = u.Symbol
		}
		if sym == "" {
			sym = fmt.Sprintf("%d", uint64(e.id))
		}
		switch e.count {
		case 1:
			parts = append(parts, sym)
		case 2:
			parts = append(parts, sym+"²")
		case 3:
			parts = append(parts, sym+"³")
		default:
			parts = append(parts, fmt.Sprintf("%s^%d", sym, e.count))
		}
	}
	return strings.Join(parts, "·")
}

// bootstrapCompound creates a compound unit at a pre-allocated entity ID.
// Used by bootstrapBuiltinUnits for the 10 new built-in compounds.
func bootstrapCompound(w *World, id ID, name, symbol string, num, denom []ID) {
	sym := symbol
	if sym == "" {
		def := &compoundDef{numerators: num, denominators: denom}
		sym = autoSymbol(w, def)
	}
	w.unitDefs[id] = Unit{Symbol: sym, Name: name, Factor: 1}
	w.compoundDefs[id] = &compoundDef{numerators: append([]ID(nil), num...), denominators: append([]ID(nil), denom...)}
}
