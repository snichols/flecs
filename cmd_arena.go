package flecs

// cmdArenaPageSize is the size of each arena page in bytes.
// 1 KiB matches the C flecs ecs_stack_page_t default.
const cmdArenaPageSize = 1024

// cmdArenaOversizedFlag marks a valueOff that indexes into cmdArena.oversized
// rather than the page array. The lower 31 bits are the slice index.
const cmdArenaOversizedFlag = uint32(1 << 31)

// cmdArena is a bump allocator used to store deferred command payloads.
// Mirrors ecs_stack_t in include/flecs/datastructures/stack_allocator.h:10–42.
//
// Pages are kept allocated across frames (reset rewinds sp, not free). Oversized
// allocations (> 1 KiB) bypass the page pool and are freed on reset.
type cmdArena struct {
	pages     [][]byte // reusable 1 KiB pages
	pageIdx   int      // index of the current page
	sp        int      // write cursor within the current page
	oversized [][]byte // allocations exceeding cmdArenaPageSize; freed on reset
}

// alloc reserves size bytes with the given alignment.
// Returns the stable offset (for later bytes() retrieval) and a slice into the
// arena that the caller may write into. The slice is valid until reset().
// size == 0 returns (0, nil).
func (a *cmdArena) alloc(size, align int) (uint32, []byte) {
	if size == 0 {
		return 0, nil
	}
	// Oversized path: skip the page pool.
	if size > cmdArenaPageSize {
		buf := make([]byte, size)
		idx := len(a.oversized)
		a.oversized = append(a.oversized, buf)
		return cmdArenaOversizedFlag | uint32(idx), buf
	}

	// Ensure at least one page exists.
	if len(a.pages) == 0 {
		a.pages = append(a.pages, make([]byte, cmdArenaPageSize))
	}

	// Align the write cursor.
	if align > 1 {
		a.sp = (a.sp + align - 1) &^ (align - 1)
	}
	// Advance to the next page if the allocation does not fit.
	if a.sp >= cmdArenaPageSize || a.sp+size > cmdArenaPageSize {
		a.pageIdx++
		a.sp = 0
		if a.pageIdx >= len(a.pages) {
			a.pages = append(a.pages, make([]byte, cmdArenaPageSize))
		}
	}

	offset := uint32(a.pageIdx)*cmdArenaPageSize + uint32(a.sp)
	buf := a.pages[a.pageIdx][a.sp : a.sp+size]
	a.sp += size
	return offset, buf
}

// bytes returns the slice previously allocated at offset with the given size.
func (a *cmdArena) bytes(offset, size uint32) []byte {
	if offset&cmdArenaOversizedFlag != 0 {
		idx := offset &^ cmdArenaOversizedFlag
		return a.oversized[idx][:size]
	}
	pageIdx := int(offset / cmdArenaPageSize)
	local := int(offset % cmdArenaPageSize)
	return a.pages[pageIdx][local : local+int(size)]
}

// reset rewinds the allocator to the beginning without freeing pages (they are
// reused next frame). Oversized allocations are freed. Mirrors flecs_stack_reset.
func (a *cmdArena) reset() {
	a.pageIdx = 0
	a.sp = 0
	for i := range a.oversized {
		a.oversized[i] = nil
	}
	a.oversized = a.oversized[:0]
}
