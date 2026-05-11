package flecs

import (
	"reflect"
	"unsafe"

	"github.com/snichols/flecs/internal/component"
)

// addIDOnWorld adds the component or tag identified by id to entity e.
// Internal helper used by Writer.AddID and scope.AddID.
func addIDOnWorld(w *World, e ID, e2 ID) bool {
	w.deferMu.Lock()
	if w.deferDepth > 0 || w.readonly.Load() {
		rec := w.index.Get(e)
		if rec == nil {
			w.deferMu.Unlock()
			panic("flecs: AddID called on dead entity")
		}
		if rec.Table != nil && rec.Table.HasComponent(e2) {
			w.deferMu.Unlock()
			return false
		}
		w.registry.EnsureID(e2)
		w.deferred.append(cmd{kind: cmdAddID, entity: e, id: e2})
		w.deferMu.Unlock()
		return true
	}
	w.deferMu.Unlock()
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

// removeIDOnWorld removes the component or tag identified by id from entity e.
// Internal helper used by Writer.RemoveID and scope.RemoveID.
func removeIDOnWorld(w *World, e ID, id ID) bool {
	w.deferMu.Lock()
	if w.deferDepth > 0 || w.readonly.Load() {
		rec := w.index.Get(e)
		if rec == nil || rec.Table == nil || !rec.Table.HasComponent(id) {
			w.deferMu.Unlock()
			return false
		}
		w.deferred.append(cmd{kind: cmdRemoveID, entity: e, id: id})
		w.deferMu.Unlock()
		return true
	}
	w.deferMu.Unlock()
	return removeIDImmediate(w, e, id)
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

// setPairOnWorld sets the pair (rel, tgt) on entity e with typed data value v.
// Internal helper used by Writer.SetPair and scope.SetPair.
func setPairOnWorld[T any](w *World, e ID, rel ID, tgt ID, v T) {
	w.deferMu.Lock()
	if w.deferDepth > 0 || w.readonly.Load() {
		pairID := MakePair(rel, tgt)
		pairInfo := component.RegisterPairData[T](w.registry, pairID)
		if pairInfo.Size > 0 {
			off, buf := w.deferred.arena.alloc(int(pairInfo.Size), int(pairInfo.Align))
			copy(buf, unsafe.Slice((*byte)(unsafe.Pointer(&v)), pairInfo.Size))
			w.deferred.append(cmd{kind: cmdSetPair, entity: e, id: pairID,
				valueOff: off, valueSize: uint32(pairInfo.Size)})
		} else {
			w.deferred.append(cmd{kind: cmdSetPair, entity: e, id: pairID})
		}
		w.deferMu.Unlock()
		return
	}
	w.deferMu.Unlock()
	setPairImmediate[T](w, e, rel, tgt, v)
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
