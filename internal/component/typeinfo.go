// Package component implements the component metadata layer for the flecs Go port.
// It provides TypeInfo (size, alignment, name, lifecycle hooks) and a Registry
// keyed by reflect.Type.
package component

import (
	"reflect"
	"unsafe"

	"github.com/snichols/flecs/internal/ids"
)

// EntityCallback is invoked by the World on lifecycle events.
// The World is passed as any to avoid an import cycle; callers in the flecs
// package type-assert it back to *flecs.World.
type EntityCallback func(world any, entity ids.ID, ptr unsafe.Pointer)

// Hooks holds optional lifecycle callbacks for a component type.
// All hooks are optional; nil means use default semantics.
type Hooks struct {
	// Move copies the component value at src to dst during archetype migration.
	// If nil, callers perform a plain byte copy of TypeInfo.Size bytes
	// (runtime.memmove equivalent). Reserved for future use.
	Move func(dst, src unsafe.Pointer)

	// OnAdd is called when the component is added to an entity.
	// Wired by the World in Phase 5 (observers); field exists here so Phase 5
	// does not need to touch every call site.
	OnAdd EntityCallback

	// OnSet is called when the component value is set on an entity.
	// Wired by the World in Phase 5 (observers).
	OnSet EntityCallback

	// OnRemove is called when the component is removed from an entity.
	// Wired by the World in Phase 5 (observers).
	OnRemove EntityCallback
}

// TypeInfo describes a Go type registered as a component.
// All fields are populated by Register or RegisterWithHooks; callers should
// treat TypeInfo values as read-only after registration.
type TypeInfo struct {
	// Size is unsafe.Sizeof of the registered type.
	Size uintptr
	// Align is unsafe.Alignof of the registered type.
	Align uintptr
	// Name is reflect.Type.String() for the registered type
	// (e.g. "main.Position", "int", "github.com/user/pkg.Foo").
	Name string
	// Hooks is a copy of the hooks struct registered for this type.
	Hooks Hooks
	// Type is the reflect.Type of the registered component.
	// Used by the table layer to construct typed slice columns via
	// reflect.SliceOf(info.Type), ensuring the GC traces pointer-containing
	// components correctly.
	Type reflect.Type
	// Component is the entity ID assigned to this type when it is registered
	// as a component-entity by the World. Zero means not yet associated.
	Component ids.ID
	// Inheritable marks the component as eligible for automatic Self|Up(IsA)
	// promotion in queries. Set via World.SetInheritable or SetInheritable[T].
	// Default false: components are matched locally only unless the user opts in.
	Inheritable bool
}
