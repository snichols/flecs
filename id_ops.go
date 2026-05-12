package flecs

import (
	"reflect"
	"unsafe"

	"github.com/snichols/flecs/internal/component"
)

// addIDOnWorld adds the component or tag identified by id to entity e.
// Internal helper used by Writer.AddID and scope.AddID.
func addIDOnWorld(w *World, e ID, e2 ID) bool {
	s0 := w.stages[0]
	if s0.deferDepth > 0 {
		rec := w.index.Get(e)
		if rec == nil {
			panic("flecs: AddID called on dead entity")
		}
		if rec.Table != nil && rec.Table.HasComponent(e2) {
			return false
		}
		w.registry.EnsureID(e2)
		s0.queue.append(cmd{kind: cmdAddID, entity: e, id: e2})
		return true
	}
	return addIDImmediate(w, e, e2)
}

func addIDImmediate(w *World, e ID, id ID) bool {
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

func removeIDImmediate(w *World, e ID, id ID) bool {
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

func setPairImmediate[T any](w *World, e ID, rel ID, tgt ID, v T) {
	pairID := MakePair(rel, tgt)
	pairInfo := component.RegisterPairData[T](w.registry, pairID)
	rec := w.index.Get(e)
	if rec == nil {
		panic("flecs: SetPair called on dead entity")
	}
	t := rec.Table
	if t != nil && t.HasComponent(pairID) {
		t.Set(int(rec.Row), pairID, unsafe.Pointer(&v))
		w.fireOnSet(pairInfo, pairID, e, t.Get(int(rec.Row), pairID))
		t.BumpChange() // pair data column write
		return
	}
	w.migrate(e, pairID, 0, unsafe.Pointer(&v))
	// OnAdd fired inside migrate; fire OnSet now.
	rec = w.index.Get(e)
	w.fireOnSet(pairInfo, pairID, e, rec.Table.Get(int(rec.Row), pairID))
}

// getPairOnWorld returns the value of pair (rel, tgt) on entity e.
// Internal helper.
func getPairOnWorld[T any](w *World, e ID, rel ID, tgt ID) (T, bool) {
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
