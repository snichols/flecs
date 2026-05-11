//go:build !flecs_exclusive_access

package flecs

const flecsExclusiveAccess = false

// goid is a stub in release builds. Call sites are guarded by
// if !flecsExclusiveAccess { return } so this is never actually invoked,
// but it must be declared because checkExclusiveAccess* functions reference it
// in their (dead-code-eliminated) bodies.
func goid() uint64 { return 0 }
