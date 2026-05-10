package flecs

import (
	"reflect"
	"unsafe"

	"github.com/snichols/flecs/internal/component"
)

// AddID adds the component or tag identified by id to entity e.
// Returns true on the first successful add, false if e already had id.
// Panics if e is not alive.
//
// id may be a component ID (obtained from RegisterComponent[T]), a raw entity ID
// used as a tag, or a pair ID (MakePair). All are treated uniformly as signature
// entries. For pair-with-data use SetPair[T] instead.
//
// When id has no TypeInfo registered yet (e.g. a raw entity ID or a pair used
// as a tag), AddID auto-registers a zero-size tag TypeInfo so the entity can be
// tracked in a table column. The column carries no data; HasID and RemoveID work
// identically whether or not data was set.
func AddID(w *World, e ID, id ID) bool {
	rec := w.index.Get(e)
	if rec == nil {
		panic("flecs: AddID called on dead entity")
	}
	if rec.Table != nil && rec.Table.HasComponent(id) {
		return false
	}
	w.registry.EnsureID(id)
	w.migrate(e, id, 0, nil)
	return true
}

// RemoveID removes the component or tag identified by id from entity e.
// Returns true if id was present and has been removed; returns false if e is not
// alive or lacked id.
func RemoveID(w *World, e ID, id ID) bool {
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	if rec.Table == nil || !rec.Table.HasComponent(id) {
		return false
	}
	w.migrate(e, 0, id, nil)
	return true
}

// HasID reports whether entity e has the component or tag identified by id —
// locally or via an IsA chain. Returns false if e is not alive or id is not
// reachable. Does NOT auto-register.
func HasID(w *World, e ID, id ID) bool {
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	if rec.Table != nil && rec.Table.HasComponent(id) {
		return true
	}
	seen := map[ID]struct{}{e: {}}
	return hasViaIsA(w, e, id, seen)
}

// OwnsID reports whether entity e locally owns the component or tag identified
// by id. Local-only: does not walk the IsA chain. Does not auto-register.
// Returns false if e is not alive.
func OwnsID(w *World, e ID, id ID) bool {
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	return rec.Table != nil && rec.Table.HasComponent(id)
}

// SetPair sets the pair (rel, tgt) on entity e with typed data value v.
//
// On first use for that pair ID, the pair is auto-registered with T's size and
// type metadata (a per-pair TypeInfo distinct from T's component TypeInfo). If
// the pair was previously registered with a different Go type, SetPair panics.
// Panics if e is not alive.
//
// For pair-as-tag (no data), prefer AddID(w, e, MakePair(rel, tgt)).
func SetPair[T any](w *World, e ID, rel ID, tgt ID, v T) {
	pairID := MakePair(rel, tgt)
	component.RegisterPairData[T](w.registry, pairID)
	rec := w.index.Get(e)
	if rec == nil {
		panic("flecs: SetPair called on dead entity")
	}
	t := rec.Table
	if t != nil && t.HasComponent(pairID) {
		t.Set(int(rec.Row), pairID, unsafe.Pointer(&v))
		return
	}
	w.migrate(e, pairID, 0, unsafe.Pointer(&v))
}

// GetPair returns the value of pair (rel, tgt) on entity e.
// Returns (zero, false) when:
//   - e is not alive
//   - the pair is not present on e
//   - the pair was registered with a different Go type than T
//
// GetPair does NOT auto-register; a missing registration returns (zero, false).
func GetPair[T any](w *World, e ID, rel ID, tgt ID) (T, bool) {
	var zero T
	pairID := MakePair(rel, tgt)
	info, ok := w.registry.LookupByID(pairID)
	if !ok {
		return zero, false
	}
	if info.Type != reflect.TypeFor[T]() {
		return zero, false
	}
	rec := w.index.Get(e)
	if rec == nil {
		return zero, false
	}
	t := rec.Table
	if t == nil || !t.HasComponent(pairID) {
		return zero, false
	}
	ptr := t.Get(int(rec.Row), pairID)
	if ptr == nil {
		// Zero-size pair data type (T = struct{}): entity has it but no data slot.
		return zero, true
	}
	return *(*T)(ptr), true
}
