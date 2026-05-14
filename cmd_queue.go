package flecs

import (
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
	cmds     []cmd
	arena    cmdArena
	entries  map[ID]cmdEntry // entity → head/tail of its cmd chain; value type avoids per-entry alloc
	scratch1 []ID            // reused per batchForEntity call: running sorted finalSet
	scratch2 []ID            // reused per batchForEntity call: addedIDs
	scratch3 []ID            // reused per batchForEntity call: removedIDs
	scratch4 []ID            // reused per batchForEntity Pass 2: tracks which newly-added IDs have been firstAdd-marked
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
// a running sorted ID slice (scratch1), rewrite Add/Remove cmds to cmdSkip.
// Execute ONE migration using sort-merge diff against oldSig — no map allocs.
//
// Pass 2 (if any Set cmds exist): rewrite Set cmds to cmdModified so that
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

	// oldSig is already sorted ascending (table invariant: table.New panics if not).
	oldSig := rec.Table.Type()

	// scratch1 is the running sorted ID set; starts as a copy of oldSig.
	// Reusing the backing array avoids allocation after the first warm-up iteration.
	q.scratch1 = append(q.scratch1[:0], oldSig...)

	deleted := false
	hasSet := false
	cleared := false
	var clearCmdIdx int32 = -1

	idx := entry.first
	for {
		c := &q.cmds[idx]
		switch c.kind {
		case cmdDelete:
			deleted = true
		case cmdClear:
			// Reset net archetype to empty. Prior cmds are superseded.
			cleared = true
			clearCmdIdx = idx
			q.scratch1 = q.scratch1[:0]
			c.kind = cmdSkip
		case cmdAddID:
			// Union pair: do not modify archetype; dispatch will call addIDImmediate
			// which routes to the union store. Union pairs never appear in archetypes.
			if c.id.IsPair() && w.unionPolicies[ID(c.id.First().Index())] {
				break // keep c.kind = cmdAddID; skip scratch1 modification and cmdSkip
			}
			if c.id.IsPair() && w.exclusivePolicies[c.id.First()] {
				// Exclusive pair: remove any existing (R, A) before inserting (R, B)
				// so the net signature has exactly one target for this relationship.
				relIdx := uint32(c.id.First())
				for k, sigID := range q.scratch1 {
					if sigID.IsPair() && uint32(sigID.First()) == relIdx && sigID.Second() != c.id.Second() {
						q.scratch1 = append(q.scratch1[:k], q.scratch1[k+1:]...)
						break
					}
				}
			} else if !c.id.IsPair() && c.id.Index() == w.exclusiveID.Index() {
				// Bare Exclusive tag: mark entity as an exclusive relationship.
				applyExclusivePolicy(w, entity)
			} else if !c.id.IsPair() && c.id.Index() == w.symmetricID.Index() {
				// Bare Symmetric tag: mark entity as a symmetric relationship.
				applySymmetricPolicy(w, entity)
			} else if !c.id.IsPair() && c.id.Index() == w.acyclicID.Index() {
				// Bare Acyclic tag: mark entity as an acyclic relationship.
				applyAcyclicPolicy(w, entity)
			} else if !c.id.IsPair() && c.id.Index() == w.singletonID.Index() {
				// Bare Singleton tag: mark entity as a singleton component.
				applySingletonPolicy(w, entity)
			} else if !c.id.IsPair() && c.id.Index() == w.writeOnceID.Index() {
				// Bare WriteOnce tag: mark entity as a write-once component.
				applyWriteOncePolicy(w, entity)
			} else if !c.id.IsPair() && c.id.Index() == w.traversableID.Index() {
				// Bare Traversable tag: mark entity as a traversable relationship.
				applyTraversablePolicy(w, entity)
			} else if !c.id.IsPair() && c.id.Index() == w.relationshipID.Index() {
				// Bare Relationship tag: mark entity as a Relationship-constrained entity.
				applyRelationshipPolicy(w, entity)
			} else if !c.id.IsPair() && c.id.Index() == w.targetID.Index() {
				// Bare Target tag: mark entity as a Target-constrained entity.
				applyTargetPolicy(w, entity)
			} else if !c.id.IsPair() && c.id.Index() == w.traitID.Index() {
				// Bare Trait tag: mark entity as a Trait entity.
				applyTraitPolicy(w, entity)
			} else if !c.id.IsPair() && w.withID != 0 && c.id.Index() == w.withID.Index() {
				// Bare With tag: no side-map to update — With co-add metadata lives
				// in (With, Y) pair storage on the entity itself.
				_ = entity
			} else if !c.id.IsPair() && w.orderedChildrenID != 0 && c.id.Index() == w.orderedChildrenID.Index() {
				// Bare OrderedChildren tag: apply ordered-children policy to entity,
				// snapshotting any children already committed to their tables at this
				// point in the flush.
				applyOrderedChildrenPolicy(w, entity)
			} else if !c.id.IsPair() && w.sparseID != 0 && c.id.Index() == w.sparseID.Index() {
				// Bare Sparse tag: mark entity as a Sparse-stored component.
				applySparsePolicy(w, entity)
			}
			// Usage-constraint enforcement on deferred path (mirrors immediate path
			// in id_ops.go). Panics at coalesce time, not at queue submission.
			checkUsageConstraints(w, c.id)
			// Acyclic cycle check on pair adds (deferred path).
			if c.id.IsPair() && w.acyclicPolicies[ID(c.id.First().Index())] {
				checkAcyclic(w, entity, c.id.First(), c.id.Second())
			}
			q.scratch1 = sortedIDInsert(q.scratch1, c.id)
			// With co-add expansion for deferred path: insert co-add IDs for c.id
			// into the pending signature so they land in the same archetype migration.
			// expandWithIntoScratch recurses for chained With (A→B→C); sortedIDInsert
			// deduplicates so diamond patterns (A→C and B→C) are handled without duplication.
			expandWithIntoScratch(w, c.id, &q.scratch1)
			c.kind = cmdSkip
		case cmdRemoveID:
			// Union pair: do not modify archetype; dispatch will call removeIDImmediate
			// which routes to the union store. Union pairs never appear in archetypes.
			if c.id.IsPair() && w.unionPolicies[ID(c.id.First().Index())] {
				break // keep c.kind = cmdRemoveID; skip scratch1 modification and cmdSkip
			}
			// Sparse or DontFragment: do not modify archetype signature; removal is handled via dispatch.
			if !c.id.IsPair() && (w.sparsePolicies[ID(c.id.Index())] || w.dontFragmentPolicies[ID(c.id.Index())]) {
				// leave kind; dispatch will call removeIDImmediate which handles sparse/DF
				break
			}
			q.scratch1 = sortedIDDelete(q.scratch1, c.id)
			c.kind = cmdSkip
		case cmdSetByID, cmdSetPair:
			// Sparse or DontFragment: do not modify archetype signature; value is routed via sparse-set.
			if !c.id.IsPair() && (w.sparsePolicies[ID(c.id.Index())] || w.dontFragmentPolicies[ID(c.id.Index())]) {
				hasSet = true
				// keep kind; dispatch handles Set via setImmediateByPtr
				break
			}
			q.scratch1 = sortedIDInsert(q.scratch1, c.id)
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

	if cleared {
		// Mark all cmds submitted before the (last) cmdClear as cmdSkip so that
		// non-archetype cmds such as union AddID or sparse RemoveID that were not
		// yet rewritten in pass 1 are not dispatched after the clear.
		idx2 := entry.first
		for idx2 != clearCmdIdx {
			c2 := &q.cmds[idx2]
			c2.kind = cmdSkip
			ni, hasNi := nextInChain(c2.nextForEntity)
			if !hasNi {
				break
			}
			idx2 = ni
		}
		// Perform the full immediate clear (fires OnRemove, cleans sparse/union/etc.).
		clearImmediate(w, entity)
		// Entity is now in the empty table; subsequent adds in scratch1 apply to [].
		oldSig = w.empty.Type()
	}

	// newSig is scratch1 — already sorted, no extra allocation.
	newSig := q.scratch1

	// Compute addedIDs / removedIDs via a single sort-merge pass over the two
	// sorted slices. Both newSig and oldSig are sorted ascending.
	// scratch2 = addedIDs (in newSig but not in oldSig)
	// scratch3 = removedIDs (in oldSig but not in newSig)
	q.scratch2 = q.scratch2[:0]
	q.scratch3 = q.scratch3[:0]
	i, j := 0, 0
	for i < len(newSig) && j < len(oldSig) {
		switch {
		case newSig[i] < oldSig[j]:
			q.scratch2 = append(q.scratch2, newSig[i])
			i++
		case newSig[i] > oldSig[j]:
			q.scratch3 = append(q.scratch3, oldSig[j])
			j++
		default:
			i++
			j++
		}
	}
	for ; i < len(newSig); i++ {
		q.scratch2 = append(q.scratch2, newSig[i])
	}
	for ; j < len(oldSig); j++ {
		q.scratch3 = append(q.scratch3, oldSig[j])
	}
	addedIDs := q.scratch2
	removedIDs := q.scratch3

	// Ensure TypeInfo exists for every newly-added ID (tags registered via
	// AddID may not have TypeInfo yet).
	for _, id := range addedIDs {
		w.registry.EnsureID(id)
	}

	// Singleton enforcement for removed IDs: release slots before migration.
	for _, id := range removedIDs {
		if !id.IsPair() && w.singletonPolicies[ID(id.Index())] {
			if existing, ok := w.singletonInstances[ID(id.Index())]; ok && existing.Index() == entity.Index() {
				delete(w.singletonInstances, ID(id.Index()))
			}
		} else if id.IsPair() && w.singletonPolicies[ID(id.First().Index())] {
			if existing, ok := w.singletonInstances[ID(id.First().Index())]; ok && existing.Index() == entity.Index() {
				delete(w.singletonInstances, ID(id.First().Index()))
			}
		}
	}
	// Singleton enforcement for added IDs: check or record holder.
	for _, id := range addedIDs {
		if !id.IsPair() && w.singletonPolicies[ID(id.Index())] {
			checkSingleton(w, id, entity)
		} else if id.IsPair() && w.singletonPolicies[ID(id.First().Index())] {
			checkSingleton(w, id.First(), entity)
		}
	}
	// Execute ONE archetype migration only when the component set actually changes.
	if len(addedIDs) > 0 || len(removedIDs) > 0 {
		w.commitBatch(entity, newSig, addedIDs, removedIDs)
		// Symmetric mirror: fire add/remove mirrors for symmetric pair changes.
		for _, id := range addedIDs {
			if id.IsPair() && w.symmetricPolicies[id.First()] {
				addIDImmediate(w, id.Second(), MakePair(id.First(), entity))
			}
		}
		for _, id := range removedIDs {
			if id.IsPair() && w.symmetricPolicies[id.First()] {
				removeIDImmediate(w, id.Second(), MakePair(id.First(), entity))
			}
		}
		// OrderedChildren deferred hooks: after the single archetype migration,
		// update ordered-children lists for any (ChildOf, parent) add/remove changes.
		// Mirrors the symmetric pattern above. Handles new-child, re-parent, and
		// orphan cases in a single pass. Pattern mirrors the Symmetric mirror at
		// cmd_queue.go:292-301.
		if w.orderedChildren != nil {
			for _, id := range removedIDs {
				if id.IsPair() && id.First().Index() == w.childOfID.Index() {
					parent := id.Second()
					if list, ok := w.orderedChildren[ID(parent.Index())]; ok {
						removeFromOrderedList(list, entity)
					}
				}
			}
			for _, id := range addedIDs {
				if id.IsPair() && id.First().Index() == w.childOfID.Index() {
					parent := id.Second()
					if list, ok := w.orderedChildren[ID(parent.Index())]; ok {
						list.entries = append(list.entries, entity)
					}
				}
			}
		}
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
	// Sparse Set cmds are NOT rewritten to cmdModified — they dispatch via
	// setImmediateByPtr which handles OnAdd+OnSet correctly without a table slot.
	//
	// For components newly added in this batch (in addedIDs), the first cmdModified
	// is marked firstAdd=true so dispatch knows to skip OnReplace on that write.
	// Subsequent cmds for the same newly-added component are NOT firstAdd, so
	// OnReplace fires between them (matching the chained old→new semantics).
	addedIDsP2 := q.scratch2 // sorted ascending; computed above
	q.scratch4 = q.scratch4[:0]
	idx = entry.first
	for {
		c := &q.cmds[idx]
		if c.kind == cmdSetByID || c.kind == cmdSetPair {
			if !c.id.IsPair() && (w.sparsePolicies[ID(c.id.Index())] || w.dontFragmentPolicies[ID(c.id.Index())]) {
				// Leave as cmdSetByID: dispatch will call setImmediateByPtr which
				// fires OnAdd (first Set) and OnSet correctly for sparse/DF storage.
			} else {
				c.kind = cmdModified
				// Mark the first cmdModified for each newly-added component so
				// dispatch can skip OnReplace on the initial slot write.
				if sortedIDContains(addedIDsP2, c.id) && !sliceIDContains(q.scratch4, c.id) {
					c.firstAdd = true
					q.scratch4 = append(q.scratch4, c.id)
				}
			}
		}
		ni, hasNi := nextInChain(c.nextForEntity)
		if !hasNi {
			break
		}
		idx = ni
	}
}

// sortedIDContains reports whether id is present in sorted slice s.
// Uses binary search; O(log n).
func sortedIDContains(s []ID, id ID) bool {
	lo, hi := 0, len(s)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if s[mid] < id {
			lo = mid + 1
		} else if s[mid] > id {
			hi = mid
		} else {
			return true
		}
	}
	return false
}

// sliceIDContains reports whether id is present in an unsorted slice s.
// Linear scan; intended for small slices (per-entity firstAdd tracking).
func sliceIDContains(s []ID, id ID) bool {
	for _, v := range s {
		if v == id {
			return true
		}
	}
	return false
}

// sortedIDInsert inserts id into the sorted slice s, keeping it sorted.
// Operates in-place on s's backing array when cap permits (no alloc after warmup).
func sortedIDInsert(s []ID, id ID) []ID {
	// Inline binary search avoids the closure allocation of sort.Search.
	lo, hi := 0, len(s)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if s[mid] < id {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(s) && s[lo] == id {
		return s // already present
	}
	// Grow by one slot, then shift right to open position lo.
	s = append(s, 0)
	copy(s[lo+1:], s[lo:]) // memmove semantics; safe for overlapping slices
	s[lo] = id
	return s
}

// sortedIDDelete removes id from the sorted slice s.
// Operates in-place (shifts elements left); no allocation.
func sortedIDDelete(s []ID, id ID) []ID {
	lo, hi := 0, len(s)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if s[mid] < id {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo >= len(s) || s[lo] != id {
		return s // not present
	}
	return append(s[:lo], s[lo+1:]...)
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

	case cmdClear:
		clearImmediate(w, c.entity)

	case cmdModified:
		// Write this cmd's specific value into the column (last write wins when
		// the same component is set multiple times), then fire OnSet so that the
		// hook sees the value submitted at THIS call site — preserving FIFO
		// submission order for hook invocations.
		info, ok := w.registry.LookupByID(c.id)
		if !ok {
			return
		}
		// Sparse: write directly into the sparse-set slot.
		if !c.id.IsPair() && w.sparsePolicies[ID(c.id.Index())] {
			if c.valueSize > 0 {
				payload := q.arena.bytes(c.valueOff, c.valueSize)
				ptr := unsafe.Pointer(&payload[0])
				checkAndSetWriteOnce(w, c.entity, c.id)
				// Fire OnReplace if the entity already had the component at this
				// point in dispatch (runtime check; O(1) sparse index lookup).
				oldPtr := sparseSetGet(w, c.entity, c.id)
				if oldPtr != nil {
					w.fireOnReplace(info, c.id, c.entity, oldPtr, ptr)
				}
				sparseSetInsert(w, c.entity, c.id, ptr)
				w.fireOnSet(info, c.id, c.entity, sparseSetGet(w, c.entity, c.id))
			} else {
				w.fireOnSet(info, c.id, c.entity, sparseSetGet(w, c.entity, c.id))
			}
			return
		}
		rec := w.index.Get(c.entity)
		if rec == nil || rec.Table == nil {
			return
		}
		if !rec.Table.HasComponent(c.id) {
			return
		}
		var ptr unsafe.Pointer
		if c.valueSize > 0 {
			payload := q.arena.bytes(c.valueOff, c.valueSize)
			ptr = unsafe.Pointer(&payload[0])
			// WriteOnce enforcement for coalesced deferred Set commands.
			checkAndSetWriteOnce(w, c.entity, c.id)
			// Skip OnReplace for the first write to a just-migrated slot (first add
			// in this batch): the slot was zero-initialized by migration, not set by
			// user code, so this is not a "replace" in the C flecs sense.
			if !c.firstAdd {
				oldPtr := rec.Table.Get(int(rec.Row), c.id) // capture before overwrite
				w.fireOnReplace(info, c.id, c.entity, oldPtr, ptr)
			}
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
