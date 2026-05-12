package flecs

import "fmt"

// cleanupPolicyFlags stores OnDelete and OnDeleteTarget action bits for a
// relationship or component entity. The bit layout mirrors C flecs
// include/flecs/private/api_flags.h:52–63.
type cleanupPolicyFlags uint8

const (
	// policyOnDeleteDelete: when the rel/component entity is deleted, delete all
	// entities that have it as a component. (EcsIdOnDeleteDelete in C)
	policyOnDeleteDelete cleanupPolicyFlags = 1 << 1
	// policyOnDeletePanic: when the rel/component entity is deleted and has
	// live sources, panic. (EcsIdOnDeletePanic in C)
	policyOnDeletePanic cleanupPolicyFlags = 1 << 2
	// policyOnDeleteTargetDelete: when an entity used as a target of this
	// relationship is deleted, delete all source entities. (EcsIdOnDeleteTargetDelete)
	policyOnDeleteTargetDelete cleanupPolicyFlags = 1 << 4
	// policyOnDeleteTargetPanic: when an entity used as a target of this
	// relationship is deleted and there are live sources, panic.
	// (EcsIdOnDeleteTargetPanic)
	policyOnDeleteTargetPanic cleanupPolicyFlags = 1 << 5
)

// SetCleanupPolicy registers the cleanup action for a relationship or component
// entity under the specified trait relationship.
//
// trait must be w.OnDelete() or w.OnDeleteTarget().
// action must be w.RemoveAction(), w.DeleteAction(), or w.PanicAction().
//
// This is a convenience wrapper around Writer.AddID with a cleanup-trait pair.
// The pair-add form is also valid and produces identical results:
//
//	w.Write(func(fw *flecs.Writer) {
//	    fw.AddID(likesID, flecs.MakePair(w.OnDeleteTarget(), w.DeleteAction()))
//	})
func SetCleanupPolicy(w *World, relID ID, trait ID, action ID) {
	applyCleanupPolicy(w, relID, trait, action)
}

// GetCleanupPolicy returns the action registered for relID under trait.
// Returns (actionID, true) if a non-Remove policy has been set; (0, false) otherwise.
// The default Remove policy is implicit and does not appear in the registry.
func GetCleanupPolicy(w *World, relID ID, trait ID) (ID, bool) {
	flags, ok := w.cleanupPolicies[relID]
	if !ok {
		return 0, false
	}
	traitIdx := trait.Index()
	switch {
	case traitIdx == w.onDeleteID.Index():
		if flags&policyOnDeleteDelete != 0 {
			return w.deleteActionID, true
		}
		if flags&policyOnDeletePanic != 0 {
			return w.panicActionID, true
		}
	case traitIdx == w.onDeleteTargetID.Index():
		if flags&policyOnDeleteTargetDelete != 0 {
			return w.deleteActionID, true
		}
		if flags&policyOnDeleteTargetPanic != 0 {
			return w.panicActionID, true
		}
	}
	return 0, false
}

// applyCleanupPolicy writes policy bits into w.cleanupPolicies[relID].
// Called by SetCleanupPolicy and by addIDImmediate when it detects a
// (OnDelete|OnDeleteTarget, action) pair being added to a relationship entity.
func applyCleanupPolicy(w *World, relID ID, trait ID, action ID) {
	if w.cleanupPolicies == nil {
		w.cleanupPolicies = make(map[ID]cleanupPolicyFlags)
	}
	flags := w.cleanupPolicies[relID]
	traitIdx := trait.Index()
	actionIdx := action.Index()
	switch {
	case traitIdx == w.onDeleteID.Index():
		flags &^= policyOnDeleteDelete | policyOnDeletePanic
		switch actionIdx {
		case w.deleteActionID.Index():
			flags |= policyOnDeleteDelete
		case w.panicActionID.Index():
			flags |= policyOnDeletePanic
		case w.removeActionID.Index():
			// Remove is the default; clearing the bits above is sufficient.
		default:
			panic(fmt.Sprintf("flecs: SetCleanupPolicy: unknown action entity %v for OnDelete", action))
		}
	case traitIdx == w.onDeleteTargetID.Index():
		flags &^= policyOnDeleteTargetDelete | policyOnDeleteTargetPanic
		switch actionIdx {
		case w.deleteActionID.Index():
			flags |= policyOnDeleteTargetDelete
		case w.panicActionID.Index():
			flags |= policyOnDeleteTargetPanic
		case w.removeActionID.Index():
			// Remove is the default; clearing the bits above is sufficient.
		default:
			panic(fmt.Sprintf("flecs: SetCleanupPolicy: unknown action entity %v for OnDeleteTarget", action))
		}
	default:
		panic(fmt.Sprintf("flecs: SetCleanupPolicy: trait must be OnDelete() or OnDeleteTarget(), got %v", trait))
	}
	w.cleanupPolicies[relID] = flags
}
