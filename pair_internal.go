package flecs

// firstPairTarget returns the target of the first pair in sig whose
// relationship index (28-bit raw entity index of the relationship entity)
// equals relIdx. Returns (0, false) if no such pair exists.
func firstPairTarget(sig []ID, relIdx uint32) (ID, bool) {
	for _, id := range sig {
		if id.IsPair() && uint32(id.First()) == relIdx {
			return id.Second(), true
		}
	}
	return 0, false
}

// eachPairTarget calls fn for every pair in sig whose relationship index equals
// relIdx. Stops early if fn returns false.
func eachPairTarget(sig []ID, relIdx uint32, fn func(ID) bool) {
	for _, id := range sig {
		if id.IsPair() && uint32(id.First()) == relIdx {
			if !fn(id.Second()) {
				return
			}
		}
	}
}
