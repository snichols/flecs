package flecs

import (
	"math"

	"github.com/snichols/flecs/internal/storage/table"
)

const pairTargetSampleCap = 256

// estimateVarDomain returns an approximate domain size for the named variable
// given the query terms. Smaller values indicate more selective variables that
// make better outer-loop drivers. math.MaxInt signals a free variable (no
// constraining terms) — always deprioritized to the innermost position.
//
// Domain rules (v1 heuristics; v2 multi-term intersection deferred):
//   - Table-kind variable: count of tables matching the minimum TermAnd constraint.
//   - srcVar == varName (WithVar style): tables containing that component;
//     sparse/DontFragment components use the sparse-set dense-slice length.
//   - tgtVar == varName, Src != 0 (fixed-source pair target): 1, the most
//     selective possible case (the source entity is fixed; targets are few).
//   - tgtVar == varName, srcVar == "" (pair target on $this): count of distinct
//     targets across (R, *) pair entries, sampling up to pairTargetSampleCap
//     tables (see docs/Queries.md "Join-order optimization").
//   - tgtVar == varName, srcVar != "" (pair target on another variable): source
//     is not yet bound statically; domain is unknown — treated as math.MaxInt.
//   - relVar == varName, relVarTarget != 0 (WithPairRelVar): count of distinct
//     relationships paired with the fixed target across sampled tables.
//   - relVar == varName, relVarTarget == 0 (WithPairBothVar): target unknown
//     statically; domain is unknown — treated as math.MaxInt.
//   - No constraining terms: math.MaxInt (free variable).
//
// The function returns the minimum estimate across all constraining terms for
// the variable, favouring the most selective constraint.
func estimateVarDomain(w *World, varName string, terms []Term, varKinds map[string]VarKind) int {
	// Table-kind variable: domain is the count of matching archetype tables.
	if varKinds != nil && varKinds[varName] == VarTable {
		return estimateTableKindDomain(w, terms)
	}

	best := math.MaxInt
	for _, t := range terms {
		if t.Kind != TermAnd {
			continue
		}
		if t.srcVar == varName && t.tgtVar == "" && t.relVar == "" {
			d := estimateSrcVarDomain(w, t)
			if d < best {
				best = d
			}
		}
		if t.tgtVar == varName {
			d := estimateTgtVarDomain(w, t)
			if d < best {
				best = d
			}
		}
		if t.relVar == varName {
			d := estimateRelVarDomain(w, t)
			if d < best {
				best = d
			}
		}
	}
	return best
}

// estimateTableKindDomain estimates the domain for a table-kind variable:
// the minimum table count across all regular (non-variable) TermAnd terms.
// This mirrors the seed-table selection heuristic in Query.Iter().
func estimateTableKindDomain(w *World, terms []Term) int {
	minTables := math.MaxInt
	for _, t := range terms {
		if t.Kind != TermAnd || t.ID == 0 || t.srcVar != "" || t.tgtVar != "" || t.relVar != "" {
			continue
		}
		cnt := w.compIndex.Count(t.ID)
		if cnt < minTables {
			minTables = cnt
		}
	}
	return minTables
}

// estimateSrcVarDomain estimates domain size for a WithVar-style srcVar term.
// For sparse/DontFragment components it reads the sparse-set population count
// (exact). For archetype components it sums entity row counts across all tables
// that contain the component (mirrors the "entities owning C" guidance in the
// issue spec). Falls back to compIndex.Count (table count) when the sum is zero
// (component is registered but no tables exist yet).
func estimateSrcVarDomain(w *World, t Term) int {
	if t.ID == 0 || t.ID.IsPair() {
		return math.MaxInt
	}
	key := ID(t.ID.Index())
	if (w.sparsePolicies[key] || w.dontFragmentPolicies[key]) && w.sparseStorage != nil {
		if ss, ok := w.sparseStorage[key]; ok {
			return len(ss.dense)
		}
	}
	total := 0
	w.compIndex.Each(t.ID, func(tbl *table.Table) bool {
		total += tbl.Count()
		return true
	})
	if total > 0 {
		return total
	}
	return w.compIndex.Count(t.ID) // fallback: table count when no entities yet
}

// estimateTgtVarDomain estimates domain size for a tgtVar pair-target term.
// Fixed-source terms (Src != 0) are cheap and return 1 (the target population
// on that specific entity is nearly always 1 for exclusive relationships).
// For $this-source terms, we scan up to pairTargetSampleCap tables and count
// distinct targets; the result is a lower-bound estimate (see Queries.md).
// Variable-source terms (srcVar != "") cannot be estimated statically without
// knowing the outer binding, so math.MaxInt is returned.
func estimateTgtVarDomain(w *World, t Term) int {
	if t.srcVar != "" {
		// Source bound by another variable — cannot estimate without its binding.
		return math.MaxInt
	}
	if t.Src != 0 {
		// Fixed source: target population is typically 1 (exclusive) or very small.
		return 1
	}
	// Source is $this: count distinct targets in (R, *) pairs across all tables,
	// sampling up to pairTargetSampleCap tables.
	relIdx := t.ID.Index()
	seen := make(map[ID]struct{})
	sampled := 0
	for _, tbl := range w.tables {
		if sampled >= pairTargetSampleCap {
			break
		}
		for _, cid := range tbl.Type() {
			if !cid.IsPair() {
				continue
			}
			if cid.First().Index() == relIdx {
				seen[cid.Second()] = struct{}{}
			}
		}
		sampled++
	}
	if len(seen) == 0 {
		return math.MaxInt // relationship not present in any table
	}
	return len(seen)
}

// estimateRelVarDomain estimates domain size for a relVar pair-relationship term.
// For WithPairRelVar (fixed target): count distinct relationship IDs paired with
// that target across sampled tables. For WithPairBothVar (target is also a
// variable, relVarTarget == 0): target unknown statically → math.MaxInt.
func estimateRelVarDomain(w *World, t Term) int {
	if t.relVarTarget == 0 {
		// Both slots are variables — target unknown statically.
		return math.MaxInt
	}
	target := t.relVarTarget
	seen := make(map[ID]struct{})
	sampled := 0
	for _, tbl := range w.tables {
		if sampled >= pairTargetSampleCap {
			break
		}
		for _, cid := range tbl.Type() {
			if !cid.IsPair() {
				continue
			}
			if cid.Second() == target {
				seen[cid.First()] = struct{}{}
			}
		}
		sampled++
	}
	if len(seen) == 0 {
		return math.MaxInt
	}
	return len(seen)
}

// selectOptimalDriver returns the best driver variable for the outermost join loop.
// It preserves the topo-sort invariant: only root variables (in-degree 0 in the
// dependency graph) are candidates. Among roots, the variable with the smallest
// estimated domain wins. Ties are broken by slot order (first-defined). Falls
// back to defaultDriver when no useful domain estimate exists.
//
// Upstream reference: flecs/src/query/compiler/compiler.c lines 1002-1021
// (variable reorder loop selecting the smallest-domain variable).
func selectOptimalDriver(w *World, varSlots map[string]int, terms []Term, defaultDriver string, varKinds map[string]VarKind) string {
	if len(varSlots) <= 1 {
		return defaultDriver
	}

	// Build in-degree map to find root variables (no dependencies).
	inDeg := make(map[string]int, len(varSlots))
	for name := range varSlots {
		inDeg[name] = 0
	}
	for _, t := range terms {
		if t.srcVar != "" && t.tgtVar != "" {
			inDeg[t.tgtVar]++ // tgtVar depends on srcVar
		}
	}

	bestDriver := defaultDriver
	bestDomain := math.MaxInt
	bestSlot := varSlots[defaultDriver]

	for varName := range varSlots {
		if inDeg[varName] > 0 {
			continue // dependent variable — cannot be driver
		}
		d := estimateVarDomain(w, varName, terms, varKinds)
		slot := varSlots[varName]
		// Prefer smaller domain; break ties by slot order (first-defined).
		if d < bestDomain || (d == bestDomain && slot < bestSlot) {
			bestDomain = d
			bestDriver = varName
			bestSlot = slot
		}
	}

	// Fall back to first-defined driver when no useful estimate found.
	if bestDomain == math.MaxInt {
		return defaultDriver
	}
	return bestDriver
}
