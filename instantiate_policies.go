package flecs

import "fmt"

// instantiatePolicyFlags stores OnInstantiate action bits for a component entity.
// Mirrors C flecs include/flecs/private/api_flags.h:65–70.
// The three bits are mutually exclusive; setting one clears the others.
type instantiatePolicyFlags uint8

const (
	// policyOnInstantiateOverride: eagerly copy the component from the prefab into
	// each new instance at (IsA, prefab) add time. (EcsIdOnInstantiateOverride in C)
	policyOnInstantiateOverride instantiatePolicyFlags = 1 << 0
	// policyOnInstantiateInherit: inherit the component via the IsA chain (the default
	// when SetInheritable is also called). (EcsIdOnInstantiateInherit in C)
	policyOnInstantiateInherit instantiatePolicyFlags = 1 << 1
	// policyOnInstantiateDontInherit: the component is never visible on instances via
	// IsA; Get/Has return zero/false and query auto-promotion is suppressed.
	// (EcsIdOnInstantiateDontInherit in C)
	policyOnInstantiateDontInherit instantiatePolicyFlags = 1 << 2
)

// SetInstantiatePolicy registers the OnInstantiate behavior for a component entity.
//
// action must be w.Override(), w.Inherit(), or w.DontInherit().
// The three actions are mutually exclusive; calling Set replaces any prior policy.
//
// This is a convenience wrapper around Writer.AddID with an OnInstantiate-trait pair.
// The pair-add form is also valid and produces identical results:
//
//	w.Write(func(fw *flecs.Writer) {
//	    fw.AddID(posID, flecs.MakePair(w.OnInstantiate(), w.Override()))
//	})
func SetInstantiatePolicy(w *World, componentID ID, action ID) {
	applyInstantiatePolicy(w, componentID, action)
}

// GetInstantiatePolicy returns the action registered for componentID.
// Returns (actionID, true) if an explicit policy has been set; (0, false) for
// the implicit default (no policy set, which is treated as Inherit when
// SetInheritable is also active).
func GetInstantiatePolicy(w *World, componentID ID) (ID, bool) {
	flags, ok := w.instantiatePolicies[componentID]
	if !ok {
		return 0, false
	}
	switch {
	case flags&policyOnInstantiateOverride != 0:
		return w.overrideID, true
	case flags&policyOnInstantiateDontInherit != 0:
		return w.dontInheritID, true
	case flags&policyOnInstantiateInherit != 0:
		return w.inheritID, true
	}
	return 0, false
}

// applyInstantiatePolicy writes the policy bits into w.instantiatePolicies[componentID].
// Called by SetInstantiatePolicy and by addIDImmediate when it detects a
// (OnInstantiate, action) pair being added to a component entity.
func applyInstantiatePolicy(w *World, componentID ID, action ID) {
	if w.instantiatePolicies == nil {
		w.instantiatePolicies = make(map[ID]instantiatePolicyFlags)
	}
	actionIdx := action.Index()
	switch {
	case actionIdx == w.overrideID.Index():
		w.instantiatePolicies[componentID] = policyOnInstantiateOverride
	case actionIdx == w.inheritID.Index():
		w.instantiatePolicies[componentID] = policyOnInstantiateInherit
	case actionIdx == w.dontInheritID.Index():
		w.instantiatePolicies[componentID] = policyOnInstantiateDontInherit
	default:
		panic(fmt.Sprintf("flecs: SetInstantiatePolicy: unknown action entity %v; expected Override(), Inherit(), or DontInherit()", action))
	}
}
