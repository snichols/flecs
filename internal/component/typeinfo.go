// Package component implements the component metadata layer for the flecs Go port.
// It provides TypeInfo (size, alignment, name, lifecycle hooks) and a Registry
// keyed by reflect.Type.
//
// Component IDs are not issued here; that work lands in Phase 1.5 when World
// exists and integrates this registry with the entity allocator.
package component

import "unsafe"

// Hooks holds optional lifecycle callbacks for a component type.
// All hooks are optional; nil means use default semantics.
type Hooks struct {
	// Move copies the component value at src to dst during archetype migration.
	// If nil, callers perform a plain byte copy of TypeInfo.Size bytes
	// (runtime.memmove equivalent). Reserved for future use.
	Move func(dst, src unsafe.Pointer)

	// Reserved for OnAdd/OnSet/OnRemove — wired in Phase 1.5 when World exists.
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

	// TODO(phase-1.5): add Component flecs.ID once the World assigns entity IDs to components.
}
