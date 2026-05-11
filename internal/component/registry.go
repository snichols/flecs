package component

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/snichols/flecs/internal/ids"
)

// tagType is the sentinel reflect.Type used for raw entity or pair IDs registered
// via EnsureID. Choosing struct{} keeps Size==0 and produces a nil table column.
var tagType = reflect.TypeFor[struct{}]()

// Registry maps reflect.Type values to their *TypeInfo metadata and maintains
// insertion order for deterministic iteration. It also indexes by component
// entity ID for reverse lookups.
// The registry is single-threaded by contract; concurrent access is not supported.
type Registry struct {
	m       map[reflect.Type]*TypeInfo
	order   []reflect.Type
	byID    map[ids.ID]*TypeInfo
	idOrder []ids.ID // all IDs added to byID in insertion order
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
	if _, exists := r.byID[id]; !exists {
		r.idOrder = append(r.idOrder, id)
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

// EnsureID guarantees that id has an associated TypeInfo in the registry.
// If id is already associated with a TypeInfo, that TypeInfo is returned unchanged.
// Otherwise, a zero-size tag TypeInfo is created and associated with id.
//
// This is used by AddID to support raw entity IDs and unregistered pair IDs as tags.
// A tag TypeInfo has Size==0 and no data column in the table layer, matching
// the behaviour of zero-size components.
func (r *Registry) EnsureID(id ids.ID) *TypeInfo {
	if info, ok := r.byID[id]; ok {
		return info
	}
	info := &TypeInfo{
		Size:  0,
		Align: 0,
		Name:  "tag",
		Type:  tagType,
	}
	r.AssociateID(info, id)
	return info
}

// IDs returns a fresh slice of all component IDs registered in insertion order.
// This includes IDs associated via AssociateID (RegisterComponent, RegisterPairData,
// EnsureID). The returned slice is a copy; mutating it does not affect the registry.
func (r *Registry) IDs() []ids.ID {
	if len(r.idOrder) == 0 {
		return nil
	}
	out := make([]ids.ID, len(r.idOrder))
	copy(out, r.idOrder)
	return out
}

// RegisterPairData ensures that pairID is associated with a per-pair TypeInfo
// whose metadata (Size, Align, Type, Hooks) matches T's base TypeInfo, but with
// a pointer-distinct instance and Name "pair(<base-name>)".
//
// Per-pair TypeInfo copies are required because AssociateID panics on associating
// multiple IDs with one TypeInfo, and LookupByType[T] returns one ID per Go type.
// Many pair IDs may share the same data type T by holding separate but
// value-equivalent TypeInfos. The base T TypeInfo and its component ID are unmodified.
//
// Idempotent when called with the same T and pairID: returns the existing TypeInfo.
// Panics if pairID is already associated with a different Go type than T.
func RegisterPairData[T any](r *Registry, pairID ids.ID) *TypeInfo {
	base := Register[T](r)
	if existing, ok := r.byID[pairID]; ok {
		if existing.Type != base.Type {
			panic(fmt.Sprintf("component: RegisterPairData: pair ID already associated with type %v; cannot associate with %v",
				existing.Type, base.Type))
		}
		return existing
	}
	pairInfo := &TypeInfo{
		Size:  base.Size,
		Align: base.Align,
		Name:  "pair(" + base.Name + ")",
		Hooks: base.Hooks,
		Type:  base.Type,
	}
	r.AssociateID(pairInfo, pairID)
	return pairInfo
}
