package flecs

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/snichols/flecs/internal/component"
	"github.com/snichols/flecs/internal/storage/componentindex"
	"github.com/snichols/flecs/internal/storage/entityindex"
	"github.com/snichols/flecs/internal/storage/table"
)

// Table is an alias for the internal archetype table type. It is exported so
// that test code in the flecs_test package can hold typed *Table pointers
// returned by helpers such as TableOf (defined in export_test.go).
type Table = table.Table

// World is the central ECS object. It owns entities (keyed by ID), component
// metadata (in a Registry), and archetype tables (keyed by sorted component-ID
// signature). Components are first-class entities; each registered component
// type is itself allocated an entity ID.
//
// Archetype storage: entities that share the same component set are grouped into
// a Table, stored in structure-of-arrays columns. Changing the component set of
// an entity triggers an archetype migration: the entity moves to the table whose
// signature matches the new set.
//
// Signature key encoding: a sorted []ID is encoded as raw bytes
// (8 bytes per uint64 ID, host byte-order) for use as a map key. This encoding
// is stable within a single process but not across processes or machines.
//
// *World is NOT goroutine-safe; external synchronization is required.
type World struct {
	mu                  sync.RWMutex // guards Read/Write scopes
	writeCapability     Writer       // cached Writer; &writeCapability avoids per-Write allocation
	readCapability      Reader       // cached Reader; &readCapability avoids per-Read allocation
	index               *entityindex.Index
	registry            *component.Registry
	tables              map[string]*table.Table         // sigKey(sorted []ID) → table
	empty               *table.Table                    // canonical empty-signature table for new entities
	compIndex           *componentindex.Index           // reverse map: component ID → tables containing it
	observers           map[observerKey][]*observerNode // lazily allocated; keyed by (id, event)
	cachedQueries       []*CachedQuery                  // lazily allocated; notified on new table creation
	systems             []*System                       // lazily allocated; compacted in NewSystem
	childOfID           ID                              // built-in ChildOf relationship entity (index 1)
	isAID               ID                              // built-in IsA relationship entity (index 2)
	nameID              ID                              // built-in Name component entity (index 3)
	preUpdateID         ID                              // built-in PreUpdate phase entity (index 4)
	onUpdateID          ID                              // built-in OnUpdate phase entity (index 5)
	postUpdateID        ID                              // built-in PostUpdate phase entity (index 6)
	onFixedUpdateID     ID                              // built-in OnFixedUpdate phase entity (index 7)
	onInstantiateID     ID                              // built-in OnInstantiate relationship entity (index 8)
	inheritID           ID                              // built-in Inherit trait entity (index 9)
	overrideID          ID                              // built-in Override trait entity (index 10)
	dontInheritID       ID                              // built-in DontInherit trait entity (index 11)
	onDeleteID          ID                              // built-in OnDelete trait relationship entity (index 12)
	onDeleteTargetID    ID                              // built-in OnDeleteTarget trait relationship entity (index 13)
	removeActionID      ID                              // built-in Remove cleanup action entity (index 14)
	deleteActionID      ID                              // built-in Delete cleanup action entity (index 15)
	panicActionID       ID                              // built-in Panic cleanup action entity (index 16)
	exclusiveID         ID                              // built-in Exclusive trait entity (index 17)
	canToggleID         ID                              // built-in CanToggle trait entity (index 18)
	symmetricID         ID                              // built-in Symmetric trait entity (index 19)
	transitiveID        ID                              // built-in Transitive trait entity (index 20)
	wildcardID          ID                              // built-in Wildcard query-term sentinel (index 21; *)
	anyID               ID                              // built-in Any query-term sentinel (index 22; _; first user entity at index 23)
	cleanupPolicies     map[ID]cleanupPolicyFlags       // relationship entity → cleanup policy bits
	instantiatePolicies map[ID]instantiatePolicyFlags   // component entity → OnInstantiate policy bits
	exclusivePolicies   map[ID]bool                     // relationship entity → exclusive flag
	canTogglePolicies   map[ID]bool                     // component entity index → CanToggle flag
	symmetricPolicies   map[ID]bool                     // relationship entity index → symmetric flag
	transitivePolicies  map[ID]bool                     // relationship entity index → transitive flag
	exclusiveAccess     atomic.Uint64                   //nolint:unused // 0=unclaimed, goroutineID=owned, ^0=write-locked; see exclusive_access.go
	exclusiveThread     string                          //nolint:unused // human-readable label for the owner goroutine; set by ExclusiveAccessBegin
	stages              []*stage                        // stages[0] = main stage; stages[1..N] = worker stages
	workerStageWriters  []Writer                        // cached per-worker Writers; index i binds to stages[i+1]
	workerCount         int                             // number of persistent goroutines in the worker pool; 0 = serial
	workerCh            chan func()                     // job channel; nil when workerCount == 0
	inProgress          bool                            // true while Progress is executing
	time                float32                         // total accumulated simulation time
	frameCount          uint64                          // number of Progress calls
	fixedTimestep       float32                         // fixed step size; 0 means disabled
	fixedAccumulator    float32                         // internal accumulator for fixed-step dispatch
	lastFramePhases     [4]PhaseStats                   // per-phase timing from the most recent Progress call
	logger              *slog.Logger                    // optional structured logger; nil means no logging
}

// New initializes and returns an empty World.
//
// Built-in entity allocation order:
//   - Index 0: null sentinel (never issued by Alloc)
//   - Index 1: ChildOf built-in relationship entity
//   - Index 2: IsA built-in relationship entity
//   - Index 3: Name built-in component entity
//   - Index 4: PreUpdate built-in pipeline phase entity
//   - Index 5: OnUpdate built-in pipeline phase entity
//   - Index 6: PostUpdate built-in pipeline phase entity
//   - Index 7: OnFixedUpdate built-in pipeline phase entity
//   - Index 8: OnInstantiate built-in relationship entity
//   - Index 9: Inherit built-in trait entity
//   - Index 10: Override built-in trait entity
//   - Index 11: DontInherit built-in trait entity
//   - Index 12: OnDelete built-in cleanup trait relationship entity
//   - Index 13: OnDeleteTarget built-in cleanup trait relationship entity
//   - Index 14: RemoveAction built-in cleanup action entity
//   - Index 15: DeleteAction built-in cleanup action entity
//   - Index 16: PanicAction built-in cleanup action entity
//   - Index 17: Exclusive built-in trait entity
//   - Index 18: CanToggle built-in trait entity
//   - Index 19: Symmetric built-in trait entity
//   - Index 20: Transitive built-in trait entity
//   - Index 21: Wildcard built-in query-term sentinel (*)
//   - Index 22: Any built-in query-term sentinel (_)
//   - Index 23+: user entities (NewEntity)
func New() *World {
	w := &World{
		index:     entityindex.New(),
		registry:  component.NewRegistry(),
		tables:    make(map[string]*table.Table),
		compIndex: componentindex.New(),
	}
	w.empty = table.New([]ID{}, []*component.TypeInfo{})
	w.tables[sigKey(nil)] = w.empty
	for _, id := range w.empty.Type() {
		w.compIndex.Register(id, w.empty)
	}
	w.notifyTableCreated(w.empty) // no-op: cachedQueries is nil at construction
	// Allocate the built-in ChildOf relationship entity (gets index 1).
	childOf := w.index.Alloc()
	rec := w.index.Get(childOf)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(childOf))
	w.childOfID = childOf
	// Allocate the built-in IsA relationship entity (gets index 2).
	isA := w.index.Alloc()
	rec = w.index.Get(isA)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(isA))
	w.isAID = isA
	// Register the built-in Name component (gets index 3).
	w.nameID = RegisterComponent[Name](w)
	// Allocate the built-in PreUpdate phase entity (gets index 4).
	preUpdate := w.index.Alloc()
	rec = w.index.Get(preUpdate)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(preUpdate))
	w.preUpdateID = preUpdate
	// Allocate the built-in OnUpdate phase entity (gets index 5).
	onUpdate := w.index.Alloc()
	rec = w.index.Get(onUpdate)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(onUpdate))
	w.onUpdateID = onUpdate
	// Allocate the built-in PostUpdate phase entity (gets index 6).
	postUpdate := w.index.Alloc()
	rec = w.index.Get(postUpdate)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(postUpdate))
	w.postUpdateID = postUpdate
	// Allocate the built-in OnFixedUpdate phase entity (gets index 7).
	onFixedUpdate := w.index.Alloc()
	rec = w.index.Get(onFixedUpdate)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(onFixedUpdate))
	w.onFixedUpdateID = onFixedUpdate
	// Allocate the built-in OnInstantiate relationship entity (gets index 8).
	onInstantiate := w.index.Alloc()
	rec = w.index.Get(onInstantiate)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(onInstantiate))
	w.onInstantiateID = onInstantiate
	// Allocate the built-in Inherit trait entity (gets index 9).
	inherit := w.index.Alloc()
	rec = w.index.Get(inherit)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(inherit))
	w.inheritID = inherit
	// Allocate the built-in Override trait entity (gets index 10).
	override := w.index.Alloc()
	rec = w.index.Get(override)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(override))
	w.overrideID = override
	// Allocate the built-in DontInherit trait entity (gets index 11).
	dontInherit := w.index.Alloc()
	rec = w.index.Get(dontInherit)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(dontInherit))
	w.dontInheritID = dontInherit
	// Allocate the built-in OnDelete cleanup trait relationship entity (gets index 12).
	onDelete := w.index.Alloc()
	rec = w.index.Get(onDelete)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(onDelete))
	w.onDeleteID = onDelete
	// Allocate the built-in OnDeleteTarget cleanup trait relationship entity (gets index 13).
	onDeleteTarget := w.index.Alloc()
	rec = w.index.Get(onDeleteTarget)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(onDeleteTarget))
	w.onDeleteTargetID = onDeleteTarget
	// Allocate the built-in Remove cleanup action entity (gets index 14).
	removeAction := w.index.Alloc()
	rec = w.index.Get(removeAction)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(removeAction))
	w.removeActionID = removeAction
	// Allocate the built-in Delete cleanup action entity (gets index 15).
	deleteAction := w.index.Alloc()
	rec = w.index.Get(deleteAction)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(deleteAction))
	w.deleteActionID = deleteAction
	// Allocate the built-in Panic cleanup action entity (gets index 16).
	panicAction := w.index.Alloc()
	rec = w.index.Get(panicAction)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(panicAction))
	w.panicActionID = panicAction
	// Allocate the built-in Exclusive trait entity (gets index 17).
	exclusive := w.index.Alloc()
	rec = w.index.Get(exclusive)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(exclusive))
	w.exclusiveID = exclusive
	// Allocate the built-in CanToggle trait entity (gets index 18).
	canToggle := w.index.Alloc()
	rec = w.index.Get(canToggle)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(canToggle))
	w.canToggleID = canToggle
	// Allocate the built-in Symmetric trait entity (gets index 19).
	symmetric := w.index.Alloc()
	rec = w.index.Get(symmetric)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(symmetric))
	w.symmetricID = symmetric
	// Allocate the built-in Transitive trait entity (gets index 20).
	transitive := w.index.Alloc()
	rec = w.index.Get(transitive)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(transitive))
	w.transitiveID = transitive
	// Allocate the built-in Wildcard query-term sentinel (gets index 21).
	wildcard := w.index.Alloc()
	rec = w.index.Get(wildcard)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(wildcard))
	w.wildcardID = wildcard
	// Allocate the built-in Any query-term sentinel (gets index 22).
	any_ := w.index.Alloc()
	rec = w.index.Get(any_)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(any_))
	w.anyID = any_
	// Bootstrap the ChildOf cascade-delete policy via the general cleanup mechanism.
	// (ChildOf, OnDeleteTarget) = Delete: deleting a parent cascades to all children.
	// This mirrors C src/bootstrap.c:705 where cr_childof_wildcard->flags gets
	// EcsIdOnDeleteTargetDelete. After this call deleteImmediate uses the general
	// policy loop rather than a hardcoded ChildOf branch.
	applyCleanupPolicy(w, w.childOfID, w.onDeleteTargetID, w.deleteActionID)
	// Bootstrap Exclusive on built-in relationships that must enforce single-target
	// invariants, matching C bootstrap.c:1259-1262. IsA is intentionally excluded —
	// C allows multiple prefab bases per instance.
	applyExclusivePolicy(w, w.childOfID)
	applyExclusivePolicy(w, w.onDeleteID)
	applyExclusivePolicy(w, w.onDeleteTargetID)
	applyExclusivePolicy(w, w.onInstantiateID)
	// Initialize stage 0 (main stage) and bind the cached write capability to it.
	s0 := &stage{id: 0, queue: acquireCmdQueue(), world: w}
	w.stages = []*stage{s0}
	// Initialize cached capability pointers — these avoid per-call heap allocation.
	w.writeCapability.Reader.world = w
	w.writeCapability.stage = s0
	w.readCapability.world = w
	return w
}

// OnDelete returns the ID of the built-in OnDelete cleanup trait relationship entity.
//
// OnDelete is a trait relationship. To apply a cleanup policy to a component or
// relationship entity, add a pair (OnDelete, action) to it:
//
//	flecs.SetCleanupPolicy(w, myRelID, w.OnDelete(), w.DeleteAction())
//
// Or via Writer.AddID:
//
//	fw.AddID(myRelID, flecs.MakePair(w.OnDelete(), w.DeleteAction()))
//
// The action is one of RemoveAction() (default), DeleteAction(), or PanicAction().
// OnDelete governs what happens to source entities when the relationship/component
// entity itself is deleted.
//
// Note: if Panic fires mid-cascade the world is in a halted state; recovery is
// not attempted. Document this contract to callers of PanicAction.
func (w *World) OnDelete() ID { return w.onDeleteID }

// OnDeleteTarget returns the ID of the built-in OnDeleteTarget cleanup trait
// relationship entity.
//
// OnDeleteTarget is a trait relationship. To apply a cleanup policy when a target
// is deleted, add a pair (OnDeleteTarget, action) to the relationship entity:
//
//	flecs.SetCleanupPolicy(w, likesID, w.OnDeleteTarget(), w.DeleteAction())
//
// Or via Writer.AddID:
//
//	fw.AddID(likesID, flecs.MakePair(w.OnDeleteTarget(), w.DeleteAction()))
//
// The action is one of RemoveAction() (default), DeleteAction(), or PanicAction().
// OnDeleteTarget governs what happens to entities that have (relationship, target)
// when the target entity is deleted. ChildOf uses DeleteAction() by default,
// which is what drives the parent-cascade-delete behavior.
func (w *World) OnDeleteTarget() ID { return w.onDeleteTargetID }

// RemoveAction returns the ID of the built-in Remove cleanup action entity.
// Remove is the default action for both OnDelete and OnDeleteTarget: when the
// triggering entity is deleted, the component or pair is removed from sources
// without deleting the source entities themselves.
func (w *World) RemoveAction() ID { return w.removeActionID }

// DeleteAction returns the ID of the built-in Delete cleanup action entity.
// When used with OnDeleteTarget, deleting a target entity cascades to delete all
// source entities that hold (relationship, target). This is the policy installed
// on ChildOf by default, producing the parent-cascade-delete behavior.
func (w *World) DeleteAction() ID { return w.deleteActionID }

// PanicAction returns the ID of the built-in Panic cleanup action entity.
// When used with OnDeleteTarget, attempting to delete an entity that is the
// target of the relationship panics with a descriptive message identifying the
// relationship and the target entity. The world is left in a halted state; no
// recovery is performed.
func (w *World) PanicAction() ID { return w.panicActionID }

// PreUpdate returns the ID of the built-in PreUpdate pipeline phase entity.
// Systems in this phase run first in each Progress call.
func (w *World) PreUpdate() ID { return w.preUpdateID }

// OnUpdate returns the ID of the built-in OnUpdate pipeline phase entity.
// Systems in this phase run second in each Progress call. NewSystem defaults to this phase.
func (w *World) OnUpdate() ID { return w.onUpdateID }

// PostUpdate returns the ID of the built-in PostUpdate pipeline phase entity.
// Systems in this phase run last in each Progress call (after OnFixedUpdate).
func (w *World) PostUpdate() ID { return w.postUpdateID }

// OnFixedUpdate returns the ID of the built-in OnFixedUpdate pipeline phase entity.
// Systems in this phase are dispatched via a fixed-step accumulator loop inside
// each Progress call and always receive the fixed timestep as dt.
// Use SetFixedTimestep to configure the step; a zero step disables this phase.
func (w *World) OnFixedUpdate() ID { return w.onFixedUpdateID }

// Time returns the total simulated time accumulated across all Progress calls.
func (w *World) Time() float32 { return w.time }

// FrameCount returns the number of Progress calls made on this world.
func (w *World) FrameCount() uint64 { return w.frameCount }

// FixedTimestep returns the current fixed step size. Zero means disabled.
func (w *World) FixedTimestep() float32 { return w.fixedTimestep }

// SetFixedTimestep sets the fixed step size used by the OnFixedUpdate accumulator.
// A step of 0 disables OnFixedUpdate dispatch entirely. Panics if step < 0.
//
// Spiral-of-death risk: if the fixed step is smaller than the average frame dt,
// each Progress call must run multiple fixed iterations. If fixed systems are
// slow, the total wall time per frame grows, causing more iterations next frame,
// creating a positive-feedback loop that stalls the simulation. No guard is
// provided; callers must ensure the fixed step is reachable within one frame.
func (w *World) SetFixedTimestep(step float32) {
	if step < 0 {
		panic("flecs: SetFixedTimestep: step must be >= 0")
	}
	w.fixedTimestep = step
}

// SetWorkerCount sets the number of worker goroutines in the persistent pool
// used for parallel system dispatch. Zero (the default) disables parallel
// dispatch; all systems run serially on the calling goroutine.
//
// When n > 0, a buffered channel of size 2*n is created and n goroutines are
// started. Systems flagged with SetParallel(true) whose write sets are pairwise
// disjoint will be dispatched as concurrent jobs within each phase.
//
// Changing n between Progress calls tears down the old pool (goroutines exit
// when the old channel is drained and closed) and starts a new pool.
//
// Calling SetWorkerCount during an active Progress call is a no-op.
//
// Panics if n < 0.
func (w *World) SetWorkerCount(n int) {
	if n < 0 {
		panic("flecs: SetWorkerCount: n must be >= 0")
	}
	if w.inProgress {
		return
	}
	if w.workerCh != nil {
		close(w.workerCh)
		w.workerCh = nil
	}
	// Release any excess worker stages (indices n+1 and beyond).
	for i := n + 1; i < len(w.stages); i++ {
		releaseCmdQueue(w.stages[i].queue)
		w.stages[i].queue = nil
	}
	if n+1 < len(w.stages) {
		w.stages = w.stages[:n+1]
	}
	w.workerCount = n
	if n > 0 {
		// Grow stages table to hold n worker stages (indices 1..n).
		for len(w.stages) <= n {
			id := len(w.stages)
			s := &stage{id: id, queue: acquireCmdQueue(), world: w, deferDepth: 1}
			w.stages = append(w.stages, s)
		}
		// Build the cached per-worker Writers (index i → stages[i+1]).
		w.workerStageWriters = make([]Writer, n)
		for i := 0; i < n; i++ {
			w.workerStageWriters[i] = Writer{
				Reader: Reader{world: w},
				stage:  w.stages[i+1],
			}
		}
		ch := make(chan func(), 2*n)
		w.workerCh = ch
		for range n {
			go func() {
				for fn := range ch {
					fn()
				}
			}()
		}
	}
}

// WorkerCount returns the current number of worker goroutines. Zero means
// serial dispatch (the default).
func (w *World) WorkerCount() int { return w.workerCount }

// newEntityInternal allocates a new entity without the exclusive-access check.
// Called by Writer.NewEntity which runs inside a Write scope where access is
// already guaranteed.
func (w *World) newEntityInternal() ID {
	e := w.index.Alloc()
	rec := w.index.Get(e)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(e))
	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "entity created",
			slog.Uint64("id", uint64(e)))
	}
	return e
}

// deleteOne removes a single entity from its archetype table and frees its ID.
// It is the primitive used by both Delete (non-parent case) and the cascade
// delete orchestrator. Returns true if e was alive.
// Fires OnRemove for each component in e's current table before removal.
func (w *World) deleteOne(e ID) bool {
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	t := rec.Table
	row := int(rec.Row)
	if t != nil {
		for _, id := range t.Type() {
			info, _ := w.registry.LookupByID(id)
			w.fireOnRemove(info, id, e, t.Get(row, id))
		}
		moved, ok := t.RemoveSwap(row)
		if ok {
			movedRec := w.index.Get(moved)
			movedRec.Row = uint32(row)
		}
	}
	freed := w.index.Free(e)
	if freed && w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "entity deleted",
			slog.Uint64("id", uint64(e)))
	}
	return freed
}

// Delete removes entity e and all entities related to it via (ChildOf, e) pairs,
// recursively. Deletion is post-order: children are deleted before their parents,
// leaves before any internal node.
//
// Returns true if e was alive. Returns false immediately with no cascade if e
// is not alive, preserving Phase 1.5 semantics.
//
// Within a deferred block, the operation is queued if e is currently alive;
// the cascade runs during flush in the order the Delete was queued.
//
// A cycle guard (seen map) prevents infinite loops for self-referential hierarchies.
func (w *World) Delete(e ID) bool {
	w.checkExclusiveAccessWrite()
	s0 := w.stages[0]
	if s0.deferDepth > 0 {
		if !w.index.IsAlive(e) {
			return false
		}
		s0.queue.append(cmd{kind: cmdDelete, entity: e})
		return true
	}
	return deleteImmediate(w, e)
}

func deleteImmediate(w *World, e ID) bool {
	if !w.index.IsAlive(e) {
		return false
	}

	// Collect e and all entities to cascade-delete via iterative DFS with cycle
	// detection. For each visited node, consult the cleanup policy registry:
	// relationships with OnDeleteTargetPanic fire immediately; relationships with
	// OnDeleteTargetDelete enqueue their sources for deletion. The default Remove
	// policy (no entry in cleanupPolicies) leaves sources alive with orphaned pairs.
	//
	// ChildOf uses OnDeleteTargetDelete (registered in New()), so the parent-cascade
	// behavior is preserved without a hardcoded branch.
	stack := []ID{e}
	var toDelete []ID
	seen := make(map[ID]struct{})
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := seen[node]; ok {
			continue
		}
		seen[node] = struct{}{}
		toDelete = append(toDelete, node)

		// Apply OnDeleteTarget policies for each relationship that has one.
		for relID, flags := range w.cleanupPolicies {
			if flags&(policyOnDeleteTargetDelete|policyOnDeleteTargetPanic) == 0 {
				continue
			}
			pairID := MakePair(relID, node)
			tables := w.compIndex.TablesFor(pairID)
			if len(tables) == 0 {
				continue
			}
			if flags&policyOnDeleteTargetPanic != 0 {
				// Identify a source entity for the panic message.
				var src ID
				for _, t := range tables {
					if es := t.Entities(); len(es) > 0 {
						src = es[0]
						break
					}
				}
				panic(fmt.Sprintf("flecs: cannot delete entity %v: it is a target of relationship %v (source entity: %v) which has OnDeleteTarget+Panic policy", node, relID, src))
			}
			// policyOnDeleteTargetDelete: cascade-delete all source entities.
			for _, t := range tables {
				// Snapshot the entity list before any deleteOne calls mutate the table.
				entities := append([]ID(nil), t.Entities()...)
				for _, src := range entities {
					if w.index.IsAlive(src) {
						stack = append(stack, src)
					}
				}
			}
		}
	}

	// Delete in post-order: deepest descendants first, root last.
	for i := len(toDelete) - 1; i >= 0; i-- {
		w.deleteOne(toDelete[i])
	}
	return true
}

// IsAlive reports whether e is currently alive.
func (w *World) IsAlive(e ID) bool {
	w.checkExclusiveAccessRead()
	return w.index.IsAlive(e)
}

// Count returns the number of currently alive entities (including component entities).
func (w *World) Count() int {
	w.checkExclusiveAccessRead()
	return w.index.Count()
}

// RegisterComponent registers T as a component-entity in w and returns its ID.
// Idempotent: if T is already registered with a component ID, returns that ID.
// The component itself is an entity, mirroring the flecs convention that
// components are first-class entities.
func RegisterComponent[T any](w *World) ID {
	w.checkExclusiveAccessWrite()
	info, ok := component.LookupByType[T](w.registry)
	if ok && info.Component != 0 {
		return info.Component
	}
	if !ok {
		info = component.Register[T](w.registry)
	}
	id := w.index.Alloc()
	w.registry.AssociateID(info, id)
	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "component registered",
			slog.String("name", info.Name),
			slog.Uint64("id", uint64(id)),
			slog.Uint64("size", uint64(info.Size)))
	}
	return id
}

// SetInheritable marks the component associated with cid as inheritable. Any
// subsequent query term involving cid will default to Self|Up traversal via
// IsA, so the term matches both entities that own cid locally and entities that
// inherit cid from a prefab via IsA. Terms that explicitly set traversal via
// Self(), Up(), SelfUp(), or Cascade() are unaffected.
//
// Must be called BEFORE any query referencing cid is constructed; calling it
// after a query is built produces undefined query-match behavior (the query
// uses whichever traversal was set at construction time).
//
// Panics if cid is not a registered component.
func (w *World) SetInheritable(cid ID) {
	info, ok := w.registry.LookupByID(cid)
	if !ok {
		panic(fmt.Sprintf("flecs: SetInheritable: component id %d is not registered", cid))
	}
	info.Inheritable = true
}

// SetInheritable marks the component type T as inheritable. T must have been
// registered with RegisterComponent first; panics otherwise.
//
// Equivalent to w.SetInheritable(RegisterComponent[T](w)) but verifies that T
// was already registered (the registration is not forced here to avoid
// accidentally creating component entities out of order).
func SetInheritable[T any](w *World) {
	info, ok := component.LookupByType[T](w.registry)
	if !ok || info.Component == 0 {
		panic(fmt.Sprintf("flecs: SetInheritable[%T]: component not registered; call RegisterComponent[T] first", *new(T)))
	}
	info.Inheritable = true
}

// setOnWorld writes value v as component T on entity e.
// Internal helper called by Writer.Set and the legacy World-based paths.
func setOnWorld[T any](w *World, e ID, v T) {
	s0 := w.stages[0]
	if s0.deferDepth > 0 {
		cid := RegisterComponent[T](w)
		info, _ := component.LookupByType[T](w.registry)
		if info.Size > 0 {
			off, buf := s0.queue.arena.alloc(int(info.Size), int(info.Align))
			copy(buf, unsafe.Slice((*byte)(unsafe.Pointer(&v)), info.Size))
			s0.queue.append(cmd{kind: cmdSetByID, entity: e, id: cid,
				valueOff: off, valueSize: uint32(info.Size)})
		} else {
			s0.queue.append(cmd{kind: cmdSetByID, entity: e, id: cid})
		}
		return
	}
	setImmediate[T](w, e, v)
}

func setImmediate[T any](w *World, e ID, v T) {
	cid := RegisterComponent[T](w)
	info, _ := component.LookupByType[T](w.registry)
	setImmediateByPtr(w, e, cid, unsafe.Pointer(&v), info)
}

// getOnWorld returns the value of component T on entity e.
// Internal helper; does not check exclusive access (called from within scopes).
func getOnWorld[T any](w *World, e ID) (T, bool) {
	var zero T
	info, ok := component.LookupByType[T](w.registry)
	if !ok || info.Component == 0 {
		return zero, false
	}
	rec := w.index.Get(e)
	if rec == nil {
		return zero, false
	}
	t := rec.Table
	if t != nil && t.HasComponent(info.Component) {
		ptr := t.Get(int(rec.Row), info.Component)
		if ptr == nil {
			return zero, true
		}
		return *(*T)(ptr), true
	}
	return getViaIsA[T](w, e, info.Component, nil)
}

// hasOnWorld reports whether entity e has component T — locally or via an IsA chain.
// Internal helper; does not check exclusive access.
func hasOnWorld[T any](w *World, e ID) bool {
	cid := RegisterComponent[T](w)
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	t := rec.Table
	if t != nil && t.HasComponent(cid) {
		return true
	}
	return hasViaIsA(w, e, cid, nil)
}

// ownsOnWorld reports whether entity e locally owns component T.
// Internal helper; does not check exclusive access.
func ownsOnWorld[T any](w *World, e ID) bool {
	cid := RegisterComponent[T](w)
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	return rec.Table != nil && rec.Table.HasComponent(cid)
}

// removeOnWorld removes component T from entity e.
// Internal helper; does not check exclusive access.
func removeOnWorld[T any](w *World, e ID) bool {
	s0 := w.stages[0]
	if s0.deferDepth > 0 {
		info, ok := component.LookupByType[T](w.registry)
		if !ok || info.Component == 0 {
			return false
		}
		rec := w.index.Get(e)
		if rec == nil || rec.Table == nil || !rec.Table.HasComponent(info.Component) {
			return false
		}
		s0.queue.append(cmd{kind: cmdRemoveID, entity: e, id: info.Component})
		return true
	}
	return removeImmediate[T](w, e)
}

func removeImmediate[T any](w *World, e ID) bool {
	info, ok := component.LookupByType[T](w.registry)
	if !ok || info.Component == 0 {
		return false
	}
	cid := info.Component
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	t := rec.Table
	if t == nil || !t.HasComponent(cid) {
		return false
	}
	w.migrate(e, 0, cid, nil)
	return true
}

// migrate moves entity e to the archetype table for the new component set
// computed as: currentSet ∪ {addID} \ {removeID}.
//
// Either addID or removeID may be 0 (no-op for that side). copyValue, if non-nil,
// is the value to write for addID in the destination table; the old table's value
// for addID (if any) is NOT carried over — copyValue always wins.
//
// Row-tracking invariant: after RemoveSwap on the source table, the entity that
// was in the last row is now at the vacated row; its Record.Row is updated to
// reflect the new position. Failure to maintain this invariant causes subsequent
// Get/Set/Has/Remove operations to read/write the wrong row.
func (w *World) migrate(e ID, addID, removeID ID, copyValue unsafe.Pointer) {
	rec := w.index.Get(e)
	oldTable := rec.Table
	oldRow := int(rec.Row)

	var oldSig []ID
	if oldTable != nil {
		oldSig = oldTable.Type()
	}

	// Consult the edge cache BEFORE computing newSig so that cache hits pay
	// zero allocation cost (no make([]ID, ...) on the fast path).
	var newTable *table.Table
	switch {
	case removeID != 0 && addID == 0 && oldTable != nil:
		if dst, ok := oldTable.NextOnRemove(removeID); ok {
			newTable = dst
		}
	case addID != 0 && removeID == 0 && oldTable != nil:
		if dst, ok := oldTable.NextOnAdd(addID); ok {
			newTable = dst
		}
	}

	if newTable == nil {
		// Cache miss: compute the new sorted signature and look up (or create)
		// the destination table. Never mutate the slice returned by Type().
		newSig := make([]ID, 0, len(oldSig)+1)
		for _, id := range oldSig {
			if id == removeID {
				continue
			}
			newSig = append(newSig, id)
		}
		if addID != 0 {
			// Insert addID in sorted position to maintain the sorted-ascending invariant.
			pos := sort.Search(len(newSig), func(i int) bool { return newSig[i] >= addID })
			newSig = append(newSig, 0)
			copy(newSig[pos+1:], newSig[pos:])
			newSig[pos] = addID
		}

		key := sigKey(newSig)
		var exists bool
		newTable, exists = w.tables[key]
		if !exists {
			types := make([]*component.TypeInfo, len(newSig))
			for i, id := range newSig {
				info, ok := w.registry.LookupByID(id)
				if !ok {
					panic("flecs: migrate: component ID not registered")
				}
				types[i] = info
			}
			newTable = table.New(newSig, types)
			w.tables[key] = newTable
			for _, id := range newTable.Type() {
				w.compIndex.Register(id, newTable)
			}
			// Notify cached queries that a new table is available. Fires once
			// per newly-created table, after full registration. Hot path: do
			// NOT compact w.cachedQueries here.
			w.notifyTableCreated(newTable)
		}
		// Cache the edge for future single-component transitions from oldTable.
		if oldTable != nil {
			switch {
			case removeID != 0 && addID == 0:
				oldTable.CacheRemoveEdge(removeID, newTable)
			case addID != 0 && removeID == 0:
				oldTable.CacheAddEdge(addID, newTable)
			}
		}
	}

	// Append a new zero-initialized row for e in the destination table.
	newRow := newTable.Append(e)

	// Carry component data from the old table to the new one.
	// Skip the removed component (not present in new table) and the added
	// component (its value comes from copyValue, not from the source).
	if oldTable != nil {
		for _, id := range oldSig {
			if id == removeID || id == addID {
				continue
			}
			if !newTable.HasComponent(id) {
				continue
			}
			ptr := oldTable.Get(oldRow, id)
			if ptr != nil {
				newTable.Set(newRow, id, ptr)
			}
		}
		// Transfer CanToggle disabled bits for components shared by both tables.
		for _, id := range oldSig {
			if id == removeID {
				continue
			}
			if !newTable.HasComponent(id) {
				continue
			}
			if !oldTable.IsRowEnabled(id, oldRow) {
				newTable.DisableRow(id, newRow)
			}
		}
	}

	// Write the new component value, if any.
	if addID != 0 && copyValue != nil {
		newTable.Set(newRow, addID, copyValue)
	}

	// Fire OnAdd for the newly-added component — destination slot is fully written.
	if addID != 0 {
		addInfo, _ := w.registry.LookupByID(addID)
		w.fireOnAdd(addInfo, addID, e, newTable.Get(newRow, addID))
	}

	// Fire OnRemove for the removed component — source slot still intact.
	if removeID != 0 && oldTable != nil {
		remInfo, _ := w.registry.LookupByID(removeID)
		w.fireOnRemove(remInfo, removeID, e, oldTable.Get(oldRow, removeID))
	}

	// Remove e from the old table using swap-remove.
	// If another entity was in the last row it has been moved to oldRow;
	// update its record so future operations find it at the correct position.
	if oldTable != nil {
		moved, ok := oldTable.RemoveSwap(oldRow)
		if ok {
			movedRec := w.index.Get(moved)
			movedRec.Row = uint32(oldRow)
		}
	}

	// Update the migrating entity's record to point at the new location.
	rec.Table = newTable
	rec.Row = uint32(newRow)
}

// TablesFor returns a snapshot of all archetype tables that contain
// componentID, in registration order. Returns an empty (non-nil) slice when no
// tables are registered for componentID.
func (w *World) TablesFor(componentID ID) []*table.Table {
	w.checkExclusiveAccessRead()
	return w.compIndex.TablesFor(componentID)
}

// EachTableFor calls fn for every archetype table containing componentID, in
// registration order. fn returns false to stop iteration early. No allocation
// is performed; this is the hot path for Phase 3 query iteration.
func (w *World) EachTableFor(componentID ID, fn func(*table.Table) bool) {
	w.checkExclusiveAccessRead()
	w.compIndex.Each(componentID, fn)
}

// eachAlive calls fn for every currently alive entity, in dense order.
// Callbacks must not call Alloc or Free (i.e. NewEntity, Delete) during iteration.
func (w *World) eachAlive(fn func(ID)) {
	w.index.Each(func(id ID, _ *entityindex.Record) {
		fn(id)
	})
}

// notifyTableCreated walks w.cachedQueries and calls tryMatchTable on each
// active (non-removed) entry. Called once per newly-created table, after the
// table is fully registered in w.tables and w.compIndex.
//
// Compaction of removed entries is intentionally skipped here (hot path);
// it happens lazily in NewCachedQuery. Defensively handles nil cachedQueries.
func (w *World) notifyTableCreated(t *table.Table) {
	for _, cq := range w.cachedQueries {
		if !cq.removed {
			prevLen := len(cq.tables)
			cq.tryMatchTable(t)
			// Re-sort when Cascade is active and a new table was accepted.
			if cq.cascadeTermTrav != 0 && len(cq.tables) > prevLen {
				cq.sortByCascadeDepth()
			}
		}
	}
	if w.logger != nil {
		sig := t.Type()
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "table created",
			slog.Int("signature_len", len(sig)),
			slog.String("signature", formatSig(sig)))
	}
}

// commitBatch migrates entity e to the table described by newSig, carrying
// component data from its current table. It fires OnAdd for each id in addedIDs
// and OnRemove for each id in removedIDs, then removes e from the old table.
//
// Unlike migrate (which handles a single add+remove), commitBatch handles an
// arbitrary number of added and removed IDs in one pass — the coalescer's key
// primitive. Values for Set cmds are NOT written here; they are written in the
// coalescer's pass 2 after this function returns.
func (w *World) commitBatch(e ID, newSig []ID, addedIDs, removedIDs []ID) {
	rec := w.index.Get(e)
	if rec == nil {
		return
	}
	oldTable := rec.Table
	oldRow := int(rec.Row)

	// Find or create the destination table.
	// sigKeyLookup is allocation-free for the common "table already exists" path.
	newTable, exists := w.tables[sigKeyLookup(newSig)]
	if !exists {
		// New archetype: copy newSig before storing — newSig may alias a scratch
		// buffer in the caller (batchForEntity reuses cmdQueue.scratch1).
		sigCopy := append([]ID(nil), newSig...)
		types := make([]*component.TypeInfo, len(sigCopy))
		for i, id := range sigCopy {
			info, ok := w.registry.LookupByID(id)
			if !ok {
				panic("flecs: commitBatch: component ID not registered")
			}
			types[i] = info
		}
		newTable = table.New(sigCopy, types)
		w.tables[sigKey(sigCopy)] = newTable
		for _, id := range newTable.Type() {
			w.compIndex.Register(id, newTable)
		}
		w.notifyTableCreated(newTable)
	}

	// Append a new zero-initialised row for e in the destination table.
	newRow := newTable.Append(e)

	// Carry existing component data from old table to new table.
	if oldTable != nil {
		for _, id := range oldTable.Type() {
			if !newTable.HasComponent(id) {
				continue
			}
			ptr := oldTable.Get(oldRow, id)
			if ptr != nil {
				newTable.Set(newRow, id, ptr)
			}
		}
	}

	// Fire OnAdd for newly-added components (slot is zero-initialised here;
	// Set payloads are written by the coalescer's pass 2 after this returns).
	for _, id := range addedIDs {
		info, _ := w.registry.LookupByID(id)
		w.fireOnAdd(info, id, e, newTable.Get(newRow, id))
	}

	// Fire OnRemove for removed components while the old slot is still live.
	for _, id := range removedIDs {
		if oldTable == nil {
			continue
		}
		info, _ := w.registry.LookupByID(id)
		w.fireOnRemove(info, id, e, oldTable.Get(oldRow, id))
	}

	// Swap-remove e from the old table and fix up the moved entity's record.
	if oldTable != nil {
		moved, ok := oldTable.RemoveSwap(oldRow)
		if ok {
			movedRec := w.index.Get(moved)
			movedRec.Row = uint32(oldRow)
		}
	}

	// Point e's record at its new location.
	rec.Table = newTable
	rec.Row = uint32(newRow)
}

// sigKey encodes a sorted []ID as a string map key (allocating copy).
// Use for map store. Each ID is stored as 8 raw bytes (host byte-order).
func sigKey(sig []ID) string {
	if len(sig) == 0 {
		return ""
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(&sig[0])), len(sig)*8))
}

// sigKeyLookup returns a map-lookup string that points directly into sig's
// backing array — no allocation. The returned string must NOT be stored in a
// map or outlive sig. Safe to use only as a transient lookup key.
func sigKeyLookup(sig []ID) string {
	if len(sig) == 0 {
		return ""
	}
	return unsafe.String((*byte)(unsafe.Pointer(&sig[0])), len(sig)*8)
}

// Read opens a read-only capability scope. Concurrent calls from multiple
// goroutines are allowed; writes from other goroutines block until all active
// Read callbacks return. Internally backed by sync.RWMutex.RLock.
func (w *World) Read(fn func(*Reader)) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	fn(&w.readCapability)
}

// Write opens a read/write capability scope. Claims exclusive access for
// the duration of fn. All structural mutations are queued in a defer scope
// and flushed when fn returns.
//
// Nested Write calls from the same goroutine increment deferDepth and run fn;
// only the outermost Write flushes. Write from a different goroutine while
// exclusive access is held panics with ErrExclusiveAccessViolation.
func (w *World) Write(fn func(*Writer)) {
	s0 := w.stages[0]
	id := currentGoid()
	owner := w.exclusiveAccess.Load()
	if owner == id {
		// Nested Write from same goroutine — increment depth and run without re-locking.
		s0.deferDepth++
		defer func() {
			s0.deferDepth--
			if s0.deferDepth == 0 {
				q := s0.queue
				s0.queue = acquireCmdQueue()
				q.flush(w)
				releaseCmdQueue(q)
			}
		}()
		fn(&w.writeCapability)
		return
	}
	if owner != 0 {
		panic(ErrExclusiveAccessViolation)
	}
	w.mu.Lock()
	w.ExclusiveAccessBegin("Write")
	s0.deferDepth++
	defer func() {
		s0.deferDepth--
		if s0.deferDepth == 0 {
			q := s0.queue
			s0.queue = acquireCmdQueue()
			q.flush(w)
			releaseCmdQueue(q)
		}
		w.ExclusiveAccessEnd(false)
		w.mu.Unlock()
	}()
	fn(&w.writeCapability)
}

// deferScope runs fn in a deferred-mutation scope without claiming exclusive
// access. It is the internal equivalent of the former public Defer method,
// used by Progress/runPhase which already holds or does not require exclusive
// access. Worker goroutines that call mutators inside a system callback rely
// on deferScope not checking goroutine ownership.
func (w *World) deferScope(fn func()) {
	s0 := w.stages[0]
	s0.deferDepth++
	defer func() {
		if s0.deferDepth <= 0 {
			panic("flecs: deferScope: depth underflow")
		}
		s0.deferDepth--
		if s0.deferDepth == 0 {
			q := s0.queue
			s0.queue = acquireCmdQueue()
			q.flush(w)
			releaseCmdQueue(q)
		}
	}()
	fn()
}

// SetLogger installs l as the structured logger for lifecycle events.
// Passing nil disables logging. When set, the logger receives DEBUG-level
// records for the following lifecycle events: entity created, entity deleted,
// component registered, table created, system added, system closed, observer
// registered, observer unsubscribed, snapshot serialized, snapshot loaded.
//
// No logs fire on hot paths: Set/Get/Has/Each/Progress and similar read or
// per-frame operations are intentionally excluded for performance.
//
// Note: lifecycle events that occur inside World.New (empty table creation,
// built-in entity allocation) are not logged because SetLogger cannot be
// called until after New() returns.
func (w *World) SetLogger(l *slog.Logger) { w.logger = l }

// Logger returns the current logger, or nil if none is installed.
func (w *World) Logger() *slog.Logger { return w.logger }

// formatSig returns a space-separated string of decimal component IDs for use
// as the "signature" attribute on "table created" log records.
func formatSig(sig []ID) string {
	if len(sig) == 0 {
		return ""
	}
	var b strings.Builder
	for i, id := range sig {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(strconv.FormatUint(uint64(id), 10))
	}
	return b.String()
}
