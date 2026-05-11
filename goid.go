package flecs

import "github.com/petermattis/goid"

// currentGoid returns the calling goroutine ID. Always compiled in.
func currentGoid() uint64 {
	return uint64(goid.Get())
}
