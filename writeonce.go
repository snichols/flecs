package flecs

import "fmt"

// writeOnceKey uniquely identifies a single per-(entity, component) write-tracking slot.
// entity is the raw entity index; comp is the full component ID (pair or non-pair).
type writeOnceKey struct {
	entity uint32
	comp   ID
}

// WriteOnce returns the ID of the built-in WriteOnce trait entity.
//
// Marking a component WriteOnce prevents value rewrites after the first Set.
// The first write (Set) succeeds; any subsequent Set on the same (entity, component)
// pair panics with a descriptive message. Add without Set does not count as the
// first write — only Set / coalesced-deferred Set commands trigger tracking.
//
// WriteOnce on a relationship R applies to every pair (R, T): each (entity, (R, T))
// pair is tracked independently. WriteOnce on a target T does not propagate.
//
// Remove clears the per-(entity, component) tracking, so a new Add + Set cycle
// starts fresh. Entity deletion also clears all tracking slots for that entity.
//
// Note: WriteOnce guards the write API (Set), not raw pointer access via
// FieldByMatch[T] or Each[T]. Those paths are unchecked by design.
//
// Usage:
//
//	flecs.SetWriteOnce(w, positionID)
//	// or bare-tag form:
//	fw.AddID(positionID, w.WriteOnce())
func (w *World) WriteOnce() ID { return w.writeOnceID }

// SetWriteOnce marks componentID as WriteOnce: after the first value write (Set),
// subsequent writes on the same (entity, component) pair panic with a message
// naming the entity and component.
//
// Panics if componentID is not a registered component. Idempotent.
func SetWriteOnce(w *World, componentID ID) {
	applyWriteOncePolicy(w, componentID)
}

// IsWriteOnce reports whether componentID has been marked WriteOnce. Accepts
// scope so it can be called inside both Read and Write blocks (per Phase 15.8
// convention). Returns false without panic for non-component entities.
func IsWriteOnce(s scope, componentID ID) bool {
	return s.scopeWorld().writeOncePolicies[ID(componentID.Index())]
}

// applyWriteOncePolicy validates that componentID is a registered component,
// then records the WriteOnce flag. Panics if the target is not a component.
func applyWriteOncePolicy(w *World, componentID ID) {
	if _, ok := w.registry.LookupByID(componentID); !ok {
		panic(fmt.Sprintf(
			"flecs: WriteOnce requires a component target; entity %v is not a component",
			componentID,
		))
	}
	if w.writeOncePolicies == nil {
		w.writeOncePolicies = make(map[ID]bool)
	}
	w.writeOncePolicies[ID(componentID.Index())] = true
}

// checkAndSetWriteOnce enforces the WriteOnce policy for a value write to (e, id).
// Panics if the slot has already been written; otherwise marks it written.
// No-op if id is not subject to WriteOnce policy or is a tag (srcPtr path guards this).
func checkAndSetWriteOnce(w *World, e ID, id ID) {
	var policyKey ID
	if id.IsPair() {
		policyKey = ID(id.First().Index())
	} else {
		policyKey = ID(id.Index())
	}
	if !w.writeOncePolicies[policyKey] {
		return
	}
	var compKey ID
	if id.IsPair() {
		compKey = id
	} else {
		compKey = ID(id.Index())
	}
	key := writeOnceKey{entity: e.Index(), comp: compKey}
	if w.writeOnceHasBeenSet[key] {
		panic(fmt.Sprintf(
			"flecs: WriteOnce component %v on entity %v has already been written; second write not allowed",
			id, e,
		))
	}
	if w.writeOnceHasBeenSet == nil {
		w.writeOnceHasBeenSet = make(map[writeOnceKey]bool)
	}
	w.writeOnceHasBeenSet[key] = true
}

// clearWriteOnceTracking removes the hasBeenSet entry for (e, id) so that a
// Remove + re-Add + Set cycle starts from a clean slate.
func clearWriteOnceTracking(w *World, e ID, id ID) {
	if w.writeOnceHasBeenSet == nil {
		return
	}
	var compKey ID
	if id.IsPair() {
		compKey = id
	} else {
		compKey = ID(id.Index())
	}
	delete(w.writeOnceHasBeenSet, writeOnceKey{entity: e.Index(), comp: compKey})
}
