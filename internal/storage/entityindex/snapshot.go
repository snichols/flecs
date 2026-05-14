package entityindex

import "github.com/snichols/flecs/internal/ids"

// MaxID returns the highest raw entity index ever issued.
func (idx *Index) MaxID() uint32 { return idx.maxID }

// DenseAlive returns a copy of the alive entity IDs in dense order
// (dense[1:aliveCount], excluding the sentinel at [0]).
func (idx *Index) DenseAlive() []ids.ID {
	if idx.aliveCount <= 1 {
		return nil
	}
	out := make([]ids.ID, idx.aliveCount-1)
	copy(out, idx.dense[1:idx.aliveCount])
	return out
}

// Recycle returns a copy of the FIFO recycle queue.
func (idx *Index) Recycle() []ids.ID {
	if len(idx.recycle) == 0 {
		return nil
	}
	out := make([]ids.ID, len(idx.recycle))
	copy(out, idx.recycle)
	return out
}

// RestoreUserState removes all user entities (rawIndex >= firstUser) from the
// index and replaces them with the provided snapshot alive/recycle state.
//
// The operation is purely structural — no hooks are fired. The caller must
// rebuild Record.Table and Record.Row for each alive entity after the
// archetype tables have been reconstructed.
//
// Ordering:
//  1. All user entries are removed from the alive dense section (swap-remove,
//     correcting Dense fields for any build-in entities that get moved).
//  2. User entries are stripped from the recycle queue.
//  3. Snapshot alive user entities are re-inserted via MakeAlive (sets Dense,
//     Record.Dense; leaves Table nil for the caller to fill in).
//  4. Snapshot recycle user entries are appended to the recycle queue.
//  5. maxID is raised to at least the snapshot's maxID.
func (idx *Index) RestoreUserState(firstUser uint32, alive []ids.ID, recycled []ids.ID, maxID uint32) {
	// Pass 1: remove user entities from the alive section.
	// Iterate backwards; when we remove an entity at position i we swap it
	// with the last alive (position aliveCount-1). We then re-examine
	// position i without decrementing (the entity just moved there might
	// also need removal). When i == last we just decrement.
	i := idx.aliveCount - 1
	for i >= 1 {
		id := idx.dense[i]
		if id.Index() < firstUser {
			i--
			continue
		}
		last := idx.aliveCount - 1
		r := idx.tryGetRecord(id.Index())
		r.Dense = 0
		r.Table = nil
		if i != last {
			swapID := idx.dense[last]
			swapRec := idx.tryGetRecord(swapID.Index())
			swapRec.Dense = uint32(i)
			idx.dense[i] = swapID
			// Don't decrement i: re-examine the entity moved here.
		} else {
			i--
		}
		idx.aliveCount--
	}

	// Pass 2: strip user entries from the recycle queue.
	w := 0
	for _, rID := range idx.recycle {
		if rID.Index() < firstUser {
			idx.recycle[w] = rID
			w++
		}
	}
	idx.recycle = idx.recycle[:w]

	// Pass 3: restore alive user entities.
	// MakeAlive handles page allocation and dense-slot reuse.
	for _, id := range alive {
		idx.MakeAlive(id)
	}

	// Pass 4: restore recycle entries.
	idx.recycle = append(idx.recycle, recycled...)

	// Pass 5: ensure maxID covers the snapshot.
	if maxID > idx.maxID {
		idx.maxID = maxID
	}
}
