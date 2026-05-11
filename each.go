package flecs

// Each1 calls fn once for every entity that has component A.
//
// Auto-registration: if A has not been registered with w, it is registered
// before the query runs (matching the Set/Has convention). This diverges from
// Get, which does NOT auto-register.
//
// Pointer lifetime: the pointers passed to fn point into the live column slot
// for the current entity. They are valid only for the duration of that fn call;
// do not retain them across calls.
//
// Concurrent modification: calling Set, Remove, or Delete on w from inside fn
// produces undefined behaviour.
//
// Iteration order: dense within each archetype table; across tables, undefined.
//
// This function is now defined in scope.go with *Reader parameter.
// This file is kept for documentation; the implementation is in scope.go.
