package flecs

import (
	"sort"
	"sync"
	"unsafe"
)

// cmdQueuePool recycles cmdQueue objects across DeferEnd/DeferBegin pairs so
// that the steady-state deferred path allocates 0 bytes per flush.
var cmdQueuePool = sync.Pool{
	New: func() any { return &cmdQueue{entries: make(map[ID]cmdEntry)} },
}

func acquireCmdQueue() *cmdQueue {
	return cmdQueuePool.Get().(*cmdQueue)
}

func releaseCmdQueue(q *cmdQueue) {
	q.reset()
	cmdQueuePool.Put(q)
}

// cmdQueue is the single deferred-command queue owned by a World.
// It replaces the old []func(*World) slice and adds:
//   - tagged-union cmd structs (no per-op closure allocation)
//   - a bump arena for value payloads (no per-Set heap allocation)
//   - a per-entity intrusive linked list enabling a two-pass coalescer
//
// Mirrors the ecs_commands_t aggregate in include/flecs/private/api_types.h:156–160.
type cmdQueue struct {
	cmds    []cmd
	arena   cmdArena
	entries map[ID]cmdEntry // entity → head/tail of its cmd chain; value type avoids per-entry alloc
}

// append links c into the per-entity intrusive list and appends it to cmds.
// Encoding for nextForEntity (mirrors flecs_cmd_new_batched in src/commands.c:38–85):
//
//   - Single cmd for entity: nextForEntity = 0
//   - When a second cmd is appended, the first cmd's nextForEntity becomes
//     -(index_of_second_cmd); subsequent non-head cmds use positive forward links.
//
// The flush loop uses `nextForEntity < 0` to identify the head of a multi-cmd chain.
func (q *cmdQueue) append(c cmd) {
	if q.entries == nil {
		q.entries = make(map[ID]cmdEntry)
	}
	idx := int32(len(q.cmds))
	q.cmds = append(q.cmds, c)

	entry, exists := q.entries[c.entity]
	if !exists {
		q.entries[c.entity] = cmdEntry{first: idx, last: idx}
		return
	}
	// Link previous last to the new cmd.
	lastCmd := &q.cmds[entry.last]
	lastCmd.nextForEntity = idx
	if entry.last == entry.first {
		// The previous last IS the first: flip the sign so the flush loop can
		// identify it as the head of a multi-cmd chain (C convention).
		lastCmd.nextForEntity = -lastCmd.nextForEntity
	}
	entry.last = idx
	q.entries[c.entity] = entry // write back updated value
}

// nextInChain decodes the nextForEntity field of a cmd, returning the next
// index and whether there is a next element.
func nextInChain(nfe int32) (next int32, ok bool) {
	if nfe == 0 {
		return 0, false
	}
	if nfe < 0 {
		return -nfe, true
	}
	return nfe, true
}

// flush runs the two-pass coalescer over every entity that has more than one
// cmd, then dispatches each surviving cmd in submission order.
// Mirrors flecs_defer_end / the outer flush loop in src/commands.c:1113–1361.
func (q *cmdQueue) flush(w *World) {
	for i := int32(0); i < int32(len(q.cmds)); i++ {
		c := &q.cmds[i]
		if c.kind == cmdSkip {
			continue
		}
		// Head of a multi-cmd per-entity chain: coalesce all cmds for this
		// entity into a single archetype migration.
		if c.nextForEntity < 0 {
			q.batchForEntity(w, c.entity)
			// batchForEntity may have rewritten c.kind; re-read via pointer.
		}
		q.dispatch(w, c)
	}
	q.reset()
}

// batchForEntity is the two-pass coalescer for a single entity.
// Mirrors flecs_cmd_batch_for_entity in src/commands.c:836–1110.
//
// Pass 1: walk the entity's cmd chain, simulate the net archetype effect into
// a finalSet, rewrite Add/Remove cmds to cmdSkip. Execute ONE migration.
//
// Pass 2 (if any Set cmds exist): walk the chain again, copy each Set payload
// into the entity's final column, rewrite the cmd to cmdModified so that
// dispatch fires OnSet at its original submission position.
func (q *cmdQueue) batchForEntity(w *World, entity ID) {
	entry, ok := q.entries[entity]
	if !ok {
		return
	}
	rec := w.index.Get(entity)
	if rec == nil {
		return // entity no longer alive
	}

	// --- Pass 1: compute net component set ---

	oldSig := rec.Table.Type()
	// finalSet starts as the entity's current component set.
	finalSet := make(map[ID]struct{}, len(oldSig)+8)
	for _, id := range oldSig {
		finalSet[id] = struct{}{}
	}

	deleted := false
	hasSet := false

	idx := entry.first
	for {
		c := &q.cmds[idx]
		switch c.kind {
		case cmdDelete:
			deleted = true
		case cmdAddID:
			finalSet[c.id] = struct{}{}
			c.kind = cmdSkip
		case cmdRemoveID:
			delete(finalSet, c.id)
			c.kind = cmdSkip
		case cmdSetByID, cmdSetPair:
			finalSet[c.id] = struct{}{}
			hasSet = true
			// keep kind; handled in pass 2
		}
		if deleted {
			break
		}
		ni, hasNi := nextInChain(c.nextForEntity)
		if !hasNi {
			break
		}
		idx = ni
	}

	if deleted {
		deleteImmediate(w, entity)
		// Rewrite the whole chain to cmdSkip.
		idx2 := entry.first
		for {
			c2 := &q.cmds[idx2]
			c2.kind = cmdSkip
			ni, hasNi := nextInChain(c2.nextForEntity)
			if !hasNi {
				break
			}
			idx2 = ni
		}
		return
	}

	// Build the new sorted signature.
	newSig := make([]ID, 0, len(finalSet))
	for id := range finalSet {
		newSig = append(newSig, id)
	}
	sort.Slice(newSig, func(i, j int) bool { return newSig[i] < newSig[j] })

	// Compute actually-added and actually-removed IDs for hook firing.
	oldSet := make(map[ID]struct{}, len(oldSig))
	for _, id := range oldSig {
		oldSet[id] = struct{}{}
	}
	var addedIDs, removedIDs []ID
	for _, id := range newSig {
		if _, inOld := oldSet[id]; !inOld {
			addedIDs = append(addedIDs, id)
		}
	}
	for _, id := range oldSig {
		if _, inNew := finalSet[id]; !inNew {
			removedIDs = append(removedIDs, id)
		}
	}

	// Ensure TypeInfo exists for every newly-added ID (tags registered via
	// AddID may not have TypeInfo yet).
	for _, id := range addedIDs {
		w.registry.EnsureID(id)
	}

	// Execute ONE archetype migration only when the component set actually changes.
	if len(addedIDs) > 0 || len(removedIDs) > 0 {
		w.commitBatch(entity, newSig, addedIDs, removedIDs)
	}

	if !hasSet {
		return
	}

	// Re-fetch record: migration updated it.
	rec = w.index.Get(entity)
	if rec == nil {
		return
	}

	// --- Pass 2: rewrite Set cmds to cmdModified ---
	// Values are NOT written here; dispatch writes each value at its original
	// submission position so that OnSet fires with the correct per-call value.
	idx = entry.first
	for {
		c := &q.cmds[idx]
		if c.kind == cmdSetByID || c.kind == cmdSetPair {
			c.kind = cmdModified
		}
		ni, hasNi := nextInChain(c.nextForEntity)
		if !hasNi {
			break
		}
		idx = ni
	}
}

// dispatch executes a single post-coalescing cmd.
// For single-cmd entities (no coalescing) it performs the full operation.
// For multi-cmd entities it handles the rewritten cmdSkip / cmdModified kinds.
func (q *cmdQueue) dispatch(w *World, c *cmd) {
	switch c.kind {
	case cmdSkip:
		// no-op

	case cmdAddID:
		addIDImmediate(w, c.entity, c.id)

	case cmdRemoveID:
		removeIDImmediate(w, c.entity, c.id)

	case cmdSetByID, cmdSetPair:
		info, ok := w.registry.LookupByID(c.id)
		if !ok {
			return
		}
		if c.valueSize > 0 {
			payload := q.arena.bytes(c.valueOff, c.valueSize)
			setImmediateByPtr(w, c.entity, c.id, unsafe.Pointer(&payload[0]), info)
		} else {
			// Zero-size / tag: no payload; treat like AddID + OnSet.
			setImmediateByPtr(w, c.entity, c.id, nil, info)
		}

	case cmdDelete:
		deleteImmediate(w, c.entity)

	case cmdModified:
		// Write this cmd's specific value into the column (last write wins when
		// the same component is set multiple times), then fire OnSet so that the
		// hook sees the value submitted at THIS call site — preserving FIFO
		// submission order for hook invocations.
		rec := w.index.Get(c.entity)
		if rec == nil || rec.Table == nil {
			return
		}
		if !rec.Table.HasComponent(c.id) {
			return
		}
		info, ok := w.registry.LookupByID(c.id)
		if !ok {
			return
		}
		var ptr unsafe.Pointer
		if c.valueSize > 0 {
			payload := q.arena.bytes(c.valueOff, c.valueSize)
			ptr = unsafe.Pointer(&payload[0])
			rec.Table.Set(int(rec.Row), c.id, ptr)
			ptr = rec.Table.Get(int(rec.Row), c.id) // stable column pointer
		} else {
			ptr = rec.Table.Get(int(rec.Row), c.id)
		}
		w.fireOnSet(info, c.id, c.entity, ptr)
		if ptr != nil { // non-tag: bump change detection counter
			rec.Table.BumpChange()
		}
	}
}

// reset clears the queue and rewinds the arena for the next frame.
func (q *cmdQueue) reset() {
	q.cmds = q.cmds[:0]
	q.arena.reset()
	for k := range q.entries {
		delete(q.entries, k)
	}
}
