package component

import (
	"reflect"
	"unsafe"

	"github.com/snichols/flecs/internal/ids"
)

// Registry maps reflect.Type values to their *TypeInfo metadata and maintains
// insertion order for deterministic iteration. It also indexes by component
// entity ID for reverse lookups.
// The registry is single-threaded by contract; concurrent access is not supported.
type Registry struct {
	m     map[reflect.Type]*TypeInfo
	order []reflect.Type
	byID  map[ids.ID]*TypeInfo
}

// NewRegistry returns a new, empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		m:    make(map[reflect.Type]*TypeInfo),
		byID: make(map[ids.ID]*TypeInfo),
	}
}

// Register records T in r and returns a pointer to its TypeInfo.
// Idempotent: calling Register[T] more than once returns the same *TypeInfo
// pointer without overwriting existing fields. Hooks default to zero-value on
// first registration.
//
// Size is determined by unsafe.Sizeof; Align by unsafe.Alignof.
// Name is reflect.TypeFor[T]().String().
func Register[T any](r *Registry) *TypeInfo {
	t := reflect.TypeFor[T]()
	if info, ok := r.m[t]; ok {
		return info
	}
	var zero T
	info := &TypeInfo{
		Size:  unsafe.Sizeof(zero),
		Align: unsafe.Alignof(zero),
		Name:  t.String(),
		Type:  t,
	}
	r.m[t] = info
	r.order = append(r.order, t)
	return info
}

// RegisterWithHooks records T in r with the provided hooks and returns its *TypeInfo.
// If T was already registered, the existing TypeInfo's hooks are replaced with
// the provided ones; subsequent calls with new hooks override prior hooks for
// that type. Returns the same *TypeInfo pointer that any prior Register returned.
func RegisterWithHooks[T any](r *Registry, hooks Hooks) *TypeInfo {
	info := Register[T](r)
	info.Hooks = hooks
	return info
}

// LookupByType returns the *TypeInfo for T, or (nil, false) if T is not registered.
func LookupByType[T any](r *Registry) (*TypeInfo, bool) {
	return r.LookupByReflectType(reflect.TypeFor[T]())
}

// LookupByReflectType returns the *TypeInfo for t, or (nil, false) if t is not registered.
func (r *Registry) LookupByReflectType(t reflect.Type) (*TypeInfo, bool) {
	info, ok := r.m[t]
	return info, ok
}

// AssociateID sets info.Component to id and indexes info by id in the byID map.
// Panics if id == 0 or if a different *TypeInfo is already associated with that id.
// Idempotent when called with the same info and same id.
func (r *Registry) AssociateID(info *TypeInfo, id ids.ID) {
	if id == 0 {
		panic("component: AssociateID called with zero ID")
	}
	if existing, ok := r.byID[id]; ok && existing != info {
		panic("component: AssociateID: id already associated with a different TypeInfo")
	}
	info.Component = id
	r.byID[id] = info
}

// LookupByID returns the *TypeInfo associated with the given component entity ID,
// or (nil, false) if no type has been associated with that ID.
func (r *Registry) LookupByID(id ids.ID) (*TypeInfo, bool) {
	info, ok := r.byID[id]
	return info, ok
}

// Each calls fn for every registered type in insertion order.
// The callback must not register new types during iteration.
func (r *Registry) Each(fn func(reflect.Type, *TypeInfo)) {
	for _, t := range r.order {
		fn(t, r.m[t])
	}
}

// Count returns the number of registered types.
func (r *Registry) Count() int {
	return len(r.order)
}
