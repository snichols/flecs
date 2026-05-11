package flecs

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/snichols/flecs/internal/component"
)

// SetPairByID sets the pair (rel, tgt) on entity e with the dynamic value v.
// This is the runtime-dynamic analog of SetPair[T]: prefer SetPair[T] when the
// type is known at compile time. Use SetPairByID when only a reflect.Value or
// any is available — for example, during JSON deserialization.
//
// On first use for that pair ID, the pair is auto-registered with v's
// reflect.Type and size metadata (a per-pair TypeInfo distinct from v's base
// component TypeInfo). If the pair was previously registered with a different
// Go type, SetPairByID panics with a descriptive message.
//
// Panics if:
//   - v is nil
//   - e is not alive (delegated to SetByID)
//   - the pair is already registered with a different type than reflect.TypeOf(v)
//
// Fires OnAdd (when the pair is newly added) and OnSet on every call via
// SetByID. Honors the Defer queue.
func (w *World) SetPairByID(e, rel, tgt ID, v any) {
	if v == nil {
		panic("flecs: SetPairByID: v must not be nil")
	}
	pairID := MakePair(rel, tgt)
	vType := reflect.TypeOf(v)
	if existing, ok := w.registry.LookupByID(pairID); ok {
		if existing.Type != vType {
			panic(fmt.Sprintf("flecs: SetPairByID: pair (rel=%d, tgt=%d) is already registered with type %s, cannot set with type %s",
				uint64(rel), uint64(tgt), existing.Type, vType))
		}
	} else {
		component.RegisterPairDataByType(w.registry, pairID, vType)
	}
	w.SetByID(e, pairID, v)
}

// GetByID reads the value of the component identified by id from entity e and
// returns it boxed in an any (interface{}). This is the runtime-dynamic analog
// of Get[T]: prefer Get[T] when the component type is known at compile time.
// Use GetByID when only the component ID is available at runtime — for example,
// in a serializer iterating EntityComponents.
//
// Returns (nil, false) in any of the following cases:
//   - e is not alive
//   - id is not registered as a component (no TypeInfo)
//   - e does not have id locally, and no IsA prefab in the chain has it
//
// Inheritance: GetByID walks the IsA chain on a local miss, matching Get[T]
// semantics exactly.
//
// Tags: for zero-size components (Size == 0), returns the zero value of the
// registered type, boxed as any. For components registered via AddID (raw entity
// or pair-as-tag), the registered type is struct{}, so the returned value is
// struct{}{}.
//
// Performance: each successful call allocates one interface header to box the
// returned value. For performance-critical paths prefer Get[T].
func (w *World) GetByID(e ID, id ID) (any, bool) {
	info, ok := w.registry.LookupByID(id)
	if !ok {
		return nil, false
	}
	rec := w.index.Get(e)
	if rec == nil {
		return nil, false
	}
	t := rec.Table
	if t != nil && t.HasComponent(id) {
		ptr := t.Get(int(rec.Row), id)
		return materializeByPtr(info, ptr), true
	}
	// Local miss: walk the IsA chain.
	return getViaIsAByID(w, e, id, info, nil)
}

// materializeByPtr boxes the value at ptr using info.Type.
// When ptr is nil (zero-size / tag component), returns the zero value of the
// registered type so the caller always receives a non-nil any.
func materializeByPtr(info *component.TypeInfo, ptr unsafe.Pointer) any {
	if ptr == nil {
		return reflect.Zero(info.Type).Interface()
	}
	// reflect.NewAt creates a typed pointer over the existing column memory
	// (rule-6 unsafe.Pointer conversion: single-expression, same-layout type).
	return reflect.NewAt(info.Type, ptr).Elem().Interface()
}

// getViaIsAByID walks the IsA chain of e looking for component id.
// Non-generic analog of getViaIsA[T], returning (any, bool).
func getViaIsAByID(w *World, e ID, id ID, info *component.TypeInfo, seen map[ID]struct{}) (any, bool) {
	rec := w.index.Get(e)
	if rec == nil {
		return nil, false
	}
	isAIdx := w.isAID.Index()
	for _, pid := range rec.Table.Type() {
		if !pid.IsPair() || uint32(pid.First()) != isAIdx {
			continue
		}
		prefab := pid.Second()
		if !w.index.IsAlive(prefab) {
			continue
		}
		if seen == nil {
			seen = map[ID]struct{}{e: {}}
		}
		if _, visited := seen[prefab]; visited {
			continue
		}
		seen[prefab] = struct{}{}
		prefabRec := w.index.Get(prefab)
		if prefabRec != nil && prefabRec.Table != nil && prefabRec.Table.HasComponent(id) {
			ptr := prefabRec.Table.Get(int(prefabRec.Row), id)
			return materializeByPtr(info, ptr), true
		}
		if v, ok := getViaIsAByID(w, prefab, id, info, seen); ok {
			return v, ok
		}
	}
	return nil, false
}

// SetByID writes value v as the component identified by id on entity e.
// This is the runtime-dynamic analog of Set[T]: prefer Set[T] when the
// component type is known at compile time. Use SetByID when only the component
// ID is available at runtime — for example, when replaying a serialized stream.
//
// Panics if:
//   - e is not alive (matches Set[T] panic policy)
//   - id is not registered as a component
//   - reflect.TypeOf(v) does not match the component's registered reflect.Type
//     (critical type-safety guard; a mismatch would corrupt column memory)
//
// Tags: for zero-size components (Size == 0), v must have the correct type but
// no bytes are written to the column (there is none). OnAdd and OnSet still fire.
//
// Archetype migration: if e does not currently own id, SetByID migrates e to the
// appropriate archetype, exactly as Set[T] does.
//
// Hooks and observers: fires OnAdd (when id is newly added) and OnSet on every
// call, in the same order as Set[T].
//
// Defer queue: when the world is deferred (IsDeferred), the operation is queued
// and applied on DeferEnd. v is captured by value. Panics on type mismatch or
// unregistered id are deferred to flush time, matching Set[T] behavior.
//
// Performance: one extra allocation per call for a bounce buffer that makes v
// addressable for the unsafe column write.
func (w *World) SetByID(e ID, id ID, v any) {
	if w.deferDepth > 0 {
		captured := v
		w.deferred = append(w.deferred, func(w *World) {
			setByIDImmediate(w, e, id, captured)
		})
		return
	}
	setByIDImmediate(w, e, id, v)
}

func setByIDImmediate(w *World, e ID, id ID, v any) {
	info, ok := w.registry.LookupByID(id)
	if !ok {
		panic(fmt.Sprintf("flecs: SetByID: component id %d is not registered", uint64(id)))
	}
	if reflect.TypeOf(v) != info.Type {
		panic(fmt.Sprintf("flecs: SetByID: type mismatch for component %s (id=%d); expected %s, got %s",
			info.Name, uint64(id), info.Type, reflect.TypeOf(v)))
	}
	rec := w.index.Get(e)
	if rec == nil {
		panic("flecs: SetByID called on dead entity")
	}
	if info.Size == 0 {
		// Tag: no data column — pass nil srcPtr; setImmediateByPtr skips the write.
		setImmediateByPtr(w, e, id, nil, info)
		return
	}
	// Bounce buffer: reflect.ValueOf(v) over an any argument is not addressable.
	// Allocate a typed pointer, copy the value in, then pass its address.
	// This is one allocation per SetByID call on a non-tag component.
	pv := reflect.New(info.Type)
	pv.Elem().Set(reflect.ValueOf(v))
	// rule-6 unsafe.Pointer conversion: single-expression, pointer to same-layout type
	setImmediateByPtr(w, e, id, pv.UnsafePointer(), info)
}

// setImmediateByPtr is the non-generic migration+write core shared by
// setImmediate[T] and setByIDImmediate. id must already be registered in
// w.registry; info is its TypeInfo. srcPtr points to the value to write
// (nil for tag components, where the column write is a no-op).
func setImmediateByPtr(w *World, e ID, id ID, srcPtr unsafe.Pointer, info *component.TypeInfo) {
	rec := w.index.Get(e)
	if rec == nil {
		panic("flecs: Set called on dead entity")
	}
	t := rec.Table
	if t != nil && t.HasComponent(id) {
		t.Set(int(rec.Row), id, srcPtr)
		w.fireOnSet(info, id, e, t.Get(int(rec.Row), id))
		return
	}
	w.migrate(e, id, 0, srcPtr)
	// OnAdd fired inside migrate; fire OnSet now that the slot is written.
	rec = w.index.Get(e)
	w.fireOnSet(info, id, e, rec.Table.Get(int(rec.Row), id))
}
