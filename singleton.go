package flecs

import "fmt"

// Singleton returns the ID of the built-in Singleton trait entity.
//
// Marking a component Singleton constrains it to be held by at most one entity
// in the world at any time. Useful for global-state-as-component patterns:
// TimeOfDay, GameSettings, PlayerInput, etc.
//
// Note: this is a deliberate divergence from C EcsSingleton. C's semantic is
// "component may only be added to itself" (must-be-self). Go's semantic is
// "at most one entity may hold this component" (at-most-one-holder). The Go
// interpretation is more useful for application code; see ComponentTraits.md
// for the full divergence note.
//
// Usage:
//
//	flecs.SetSingleton(w, positionID)
//	// or bare-tag form:
//	fw.AddID(positionID, w.Singleton())
func (w *World) Singleton() ID { return w.singletonID }

// SetSingleton marks componentID as a Singleton: at most one entity may hold
// it at any time. Idempotent — marking an already-singleton component is safe.
func SetSingleton(w *World, componentID ID) {
	applySingletonPolicy(w, componentID)
}

// IsSingleton reports whether componentID has been marked Singleton. Accepts
// scope so it can be called inside both Read and Write blocks (per Phase 15.8
// convention).
func IsSingleton(s scope, componentID ID) bool {
	return s.scopeWorld().singletonPolicies[ID(componentID.Index())]
}

// SingletonEntity returns the entity currently holding componentID, plus true.
// Returns (0, false) if no entity holds it or if componentID is not a
// singleton. Accepts scope (per Phase 15.8 convention).
func SingletonEntity(s scope, componentID ID) (ID, bool) {
	holder, ok := s.scopeWorld().singletonInstances[ID(componentID.Index())]
	return holder, ok
}

// applySingletonPolicy writes the Singleton flag for componentID. Called by
// SetSingleton and by addIDImmediate when the bare Singleton tag is added.
func applySingletonPolicy(w *World, componentID ID) {
	if w.singletonPolicies == nil {
		w.singletonPolicies = make(map[ID]bool)
	}
	w.singletonPolicies[ID(componentID.Index())] = true
}

// checkSingleton panics if a different entity already holds componentID as a
// singleton. If no holder is recorded, it records holder as the new holder.
func checkSingleton(w *World, componentID ID, holder ID) {
	if w.singletonInstances == nil {
		w.singletonInstances = make(map[ID]ID)
	}
	existing, ok := w.singletonInstances[ID(componentID.Index())]
	if !ok {
		w.singletonInstances[ID(componentID.Index())] = holder
		return
	}
	if existing.Index() == holder.Index() {
		return
	}
	panic(fmt.Sprintf(
		"flecs: cannot add singleton component %v to entity %v: already held by entity %v",
		componentID, holder, existing,
	))
}
