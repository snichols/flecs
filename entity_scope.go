package flecs

import "fmt"

// WithinScope pushes parent onto fw's scope stack, calls fn with the same Writer,
// then pops on return (defer-based; survives panic in fn).
// While fn executes, every NewEntity (and RangeNew) call automatically receives
// (ChildOf, parent) without an explicit AddID.
//
// Mirrors upstream ecs_set_scope / ecs_get_scope (src/entity_name.c:785-808) but
// implemented as a closure-based API for Go idiom. Nesting composes cleanly: the
// inner-most WithinScope wins for entities created during its callback.
func WithinScope(fw *Writer, parent ID, fn func(fw *Writer)) {
	prev := PushScope(fw, parent)
	defer PopScope(fw, prev)
	fn(fw)
}

// PushScope pushes parent onto fw's scope stack and returns the previous top
// (zero if the stack was empty). The returned value must be passed to PopScope.
//
// Prefer WithinScope for most use cases; PushScope/PopScope are for callers who
// need to cross function boundaries where a closure would be awkward.
func PushScope(fw *Writer, parent ID) ID {
	var prev ID
	if len(fw.scopeStack) > 0 {
		prev = fw.scopeStack[len(fw.scopeStack)-1]
	}
	fw.scopeStack = append(fw.scopeStack, parent)
	return prev
}

// PopScope pops one frame from fw's scope stack. prev must be the value returned
// by the matching PushScope call; panics on mismatch (programming error).
func PopScope(fw *Writer, prev ID) {
	n := len(fw.scopeStack)
	if n == 0 {
		panic("flecs: PopScope: scope stack is empty")
	}
	var expectedPrev ID
	if n >= 2 {
		expectedPrev = fw.scopeStack[n-2]
	}
	if prev != expectedPrev {
		panic(fmt.Sprintf("flecs: PopScope: prev mismatch: expected %d, got %d",
			uint64(expectedPrev), uint64(prev)))
	}
	fw.scopeStack = fw.scopeStack[:n-1]
}

// GetScope returns the current scope (the topmost entry on fw's scope stack),
// or 0 if no scope is active. When called on a *Reader, always returns 0 —
// read blocks have no entity-scope semantics.
func GetScope(s scope) ID {
	fw, ok := s.(*Writer)
	if !ok {
		return 0
	}
	if len(fw.scopeStack) == 0 {
		return 0
	}
	return fw.scopeStack[len(fw.scopeStack)-1]
}
