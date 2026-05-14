package flecs

import (
	"fmt"

	"github.com/snichols/flecs/internal/storage/table"
)

// DontFragment returns the ID of the built-in DontFragment trait entity (index 35).
//
// Marking a component DontFragment causes the owning entity to NOT transition
// archetype tables when the component is added or removed. The component does NOT
// appear in the entity's archetype type. Data is stored in a per-component
// sparse-set (the same backing storage as Sparse).
//
// Consequences of DontFragment alone:
//   - Adding/removing a DontFragment component does NOT cause an archetype transition.
//   - The component is NOT present in the entity's archetype type.
//   - HasID and OwnsID consult the sparse-set index, not the entity's archetype type.
//
// Canonical pattern — use Sparse + DontFragment together:
//
//	posID := flecs.RegisterComponent[Position](w)
//	flecs.SetSparse(w, posID)
//	flecs.SetDontFragment(w, posID)
//
// This combination matches the v0.51.0–v0.52.0 Sparse behavior (data in sparse-set,
// no archetype transition). See MIGRATING.md for migration guidance.
//
// DontFragment alone (without Sparse) is valid: data is in sparse-set but the
// Sparse trait is not set. Sparse alone (without DontFragment) stores data in the
// sparse-set but the entity DOES transition archetype tables on add/remove (rare).
//
// Auto-add relationship: upstream C flecs bootstraps (DontFragment, With, Sparse)
// so that adding DontFragment automatically adds Sparse. Go-flecs does NOT auto-add
// Sparse when DontFragment is set in v0.53.0; call both explicitly.
//
// Usage:
//
//	posID := flecs.RegisterComponent[Position](w)
//	flecs.SetDontFragment(w, posID)
//	// or bare-tag form:
//	fw.AddID(posID, w.DontFragment())
func (w *World) DontFragment() ID { return w.dontFragmentID }

// SetDontFragment marks componentID as DontFragment-stored. Idempotent.
//
// Panics if:
//   - componentID is not a registered data-bearing component
//   - The component has already been added to any entity via archetype storage
//     or sparse-set — marking an in-use component as DontFragment would leave
//     existing data in an inconsistent state. Mark the component DontFragment
//     before using it.
func SetDontFragment(w *World, componentID ID) {
	info, ok := w.registry.LookupByID(componentID)
	if !ok || info.Size == 0 {
		panic(fmt.Sprintf("flecs: SetDontFragment: %v is not a registered data-bearing component", componentID))
	}
	// After-use trap: panic if the component is already held by any entity via
	// archetype storage or sparse-set.
	var inUse int
	w.compIndex.Each(componentID, func(t *table.Table) bool {
		inUse += t.Count()
		return true
	})
	key := ID(componentID.Index())
	if ss, exists := w.sparseStorage[key]; exists {
		inUse += len(ss.dense)
	}
	if inUse > 0 {
		panic(fmt.Sprintf(
			"flecs: cannot mark %v as DontFragment: component is already in use on %d entities",
			componentID, inUse,
		))
	}
	applyDontFragmentPolicy(w, componentID)
}

// IsDontFragment reports whether componentID has the DontFragment trait.
// Accepts scope so it can be called inside both Read and Write blocks.
func IsDontFragment(s scope, componentID ID) bool {
	w := s.scopeWorld()
	return w.dontFragmentPolicies[ID(componentID.Index())]
}

// applyDontFragmentPolicy writes the DontFragment flag for componentID and
// ensures the backing sparse-set storage is initialized.
// Called by SetDontFragment and by addIDImmediate when the bare DontFragment tag
// is added to a component entity (fw.AddID(posID, w.DontFragment())).
// Idempotent: a second call on an already-DontFragment component is a no-op.
func applyDontFragmentPolicy(w *World, componentID ID) {
	if w.dontFragmentPolicies == nil {
		w.dontFragmentPolicies = make(map[ID]bool)
	}
	key := ID(componentID.Index())
	if w.dontFragmentPolicies[key] {
		return
	}
	w.dontFragmentPolicies[key] = true
	// DontFragment uses sparse-set storage for data (same backing as Sparse).
	// Ensure sparseStorage entry is initialized if not already present.
	if w.sparseStorage == nil {
		w.sparseStorage = make(map[ID]*sparseSet)
	}
	if _, already := w.sparseStorage[key]; !already {
		info, _ := w.registry.LookupByID(componentID)
		w.sparseStorage[key] = &sparseSet{
			index:    make(map[uint32]int),
			typeInfo: info,
		}
	}
}
