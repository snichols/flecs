//go:build flecs_exclusive_access

package flecs

import (
	"runtime"
	"strconv"
	"strings"
)

// goid returns the goroutine ID of the calling goroutine by parsing the first
// line of the stack trace returned by runtime.Stack. The format:
//
//	goroutine NNN [running]:
//
// has been stable across all released Go versions, but it is unspecified and
// could change in future versions. That is the primary reason this code is
// gated behind the flecs_exclusive_access build tag: it is intended exclusively
// for debug sessions, not production use.
//
// Performance note: runtime.Stack allocates and is slow (~microseconds per
// call). That overhead is acceptable here because every mutator call pays it —
// the intended use is running short debug sessions, not benchmarks. Do not
// enable -tags flecs_exclusive_access in production or performance testing.
//
// If the stack trace cannot be parsed (should never happen in practice), goid
// returns 0, which compares unequal to any valid goroutine ID and causes all
// ownership checks to pass (fail-open).
func goid() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	s := string(buf[:n])
	s = strings.TrimPrefix(s, "goroutine ")
	if idx := strings.IndexByte(s, ' '); idx >= 0 {
		id, _ := strconv.ParseUint(s[:idx], 10, 64)
		return id
	}
	return 0
}
