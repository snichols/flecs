package flecs

import "github.com/snichols/flecs/internal/component"

// CanToggle returns the ID of the built-in CanToggle trait entity.
//
// Marking a component as toggleable allows individual entities to have that
// component temporarily disabled without removing it. Queries (via [Each1],
// [Each2], etc.) skip rows where the component is disabled. Re-enabling
// restores normal matching behaviour without a table migration.
//
// The bare-tag form is also valid and equivalent to [SetCanToggle]:
//
//	fw.AddID(positionID, w.CanToggle())
func (w *World) CanToggle() ID { return w.canToggleID }

// SetCanToggle marks componentID as a toggleable component. After this call,
// [EnableID], [DisableID], and [IsEnabledID] may be used on entities that own
// componentID. [Each1]/[Each2] automatically skip disabled rows.
//
// Equivalent to fw.AddID(componentID, w.CanToggle()).
func SetCanToggle(w *World, componentID ID) {
	applyCanTogglePolicy(w, componentID)
}

// IsCanToggle reports whether componentID has been marked toggleable.
func IsCanToggle(w *World, componentID ID) bool {
	return w.canTogglePolicies[ID(componentID.Index())]
}

// applyCanTogglePolicy records componentID in w.canTogglePolicies keyed by the
// entity's raw index, stripping the generation field for the same reason as
// applyExclusivePolicy: component record look-ups use bare indices.
func applyCanTogglePolicy(w *World, componentID ID) {
	if w.canTogglePolicies == nil {
		w.canTogglePolicies = make(map[ID]bool)
	}
	w.canTogglePolicies[ID(componentID.Index())] = true
}

// EnableID marks componentID as enabled for entity e.
//
// Panics if e does not have componentID or if componentID is not marked
// CanToggle (via [SetCanToggle] or fw.AddID(componentID, w.CanToggle())).
func EnableID(fw *Writer, e ID, componentID ID) {
	w := fw.world
	if !IsCanToggle(w, componentID) {
		panic("flecs: EnableID: component is not marked CanToggle")
	}
	rec := w.index.Get(e)
	if rec == nil || rec.Table == nil || !rec.Table.HasComponent(componentID) {
		panic("flecs: EnableID: entity does not have the component")
	}
	rec.Table.EnableRow(componentID, int(rec.Row))
	rec.Table.BumpChange()
}

// DisableID marks componentID as disabled for entity e.
//
// Panics if e does not have componentID or if componentID is not marked
// CanToggle (via [SetCanToggle] or fw.AddID(componentID, w.CanToggle())).
func DisableID(fw *Writer, e ID, componentID ID) {
	w := fw.world
	if !IsCanToggle(w, componentID) {
		panic("flecs: DisableID: component is not marked CanToggle")
	}
	rec := w.index.Get(e)
	if rec == nil || rec.Table == nil || !rec.Table.HasComponent(componentID) {
		panic("flecs: DisableID: entity does not have the component")
	}
	rec.Table.DisableRow(componentID, int(rec.Row))
	rec.Table.BumpChange()
}

// IsEnabledID reports whether componentID is enabled for entity e.
//
// Returns false if e does not have componentID.
// Returns true if componentID is not marked CanToggle or if no row has been
// disabled in e's table (all-enabled default).
func IsEnabledID(r *Reader, e ID, componentID ID) bool {
	rec := r.world.index.Get(e)
	if rec == nil || rec.Table == nil || !rec.Table.HasComponent(componentID) {
		return false
	}
	return rec.Table.IsRowEnabled(componentID, int(rec.Row))
}

// Enable marks component T as enabled for entity e.
// Delegates to [EnableID] after resolving T's component ID from the registry.
//
// Panics if T has not been registered, if e does not have T, or if T is not
// marked CanToggle.
func Enable[T any](fw *Writer, e ID) {
	info, ok := component.LookupByType[T](fw.world.registry)
	if !ok || info.Component == 0 {
		panic("flecs: Enable: component type not registered")
	}
	EnableID(fw, e, info.Component)
}

// Disable marks component T as disabled for entity e.
// Delegates to [DisableID] after resolving T's component ID from the registry.
//
// Panics if T has not been registered, if e does not have T, or if T is not
// marked CanToggle.
func Disable[T any](fw *Writer, e ID) {
	info, ok := component.LookupByType[T](fw.world.registry)
	if !ok || info.Component == 0 {
		panic("flecs: Disable: component type not registered")
	}
	DisableID(fw, e, info.Component)
}

// IsEnabled reports whether component T is enabled for entity e.
// Delegates to [IsEnabledID] after resolving T's component ID from the registry.
//
// Returns false if T is not registered or if e does not have T.
func IsEnabled[T any](r *Reader, e ID) bool {
	info, ok := component.LookupByType[T](r.world.registry)
	if !ok || info.Component == 0 {
		return false
	}
	return IsEnabledID(r, e, info.Component)
}
