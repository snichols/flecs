package flecs

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// System is an opaque handle returned by NewSystem or NewSystemInPhase. It
// links a CachedQuery to a callback that runs once per World.Progress call
// within its assigned pipeline phase. Call Close to deregister.
//
// The callback receives the dt value passed to Progress and a fresh *QueryIter
// positioned before the first table. The callback is responsible for calling
// it.Next() in a loop; Field[T] and other iterator helpers work normally inside
// the callback.
//
// Parallel execution: call SetParallel(true) to opt in. Parallel systems in
// the same phase with disjoint write sets run concurrently in the world's
// worker pool. Each parallel system receives its own *QueryIter; callers must
// NOT call Field on another system's QueryIter from a parallel callback.
//
// Multi-threaded execution: call SetMultiThreaded(true) to split this system's
// iter across all workers. Each worker receives a disjoint row slice of every
// matched table. A multi-threaded system cannot batch with parallel siblings.
//
// *System is NOT goroutine-safe; external synchronization is required.
type System struct {
	w             *World
	query         *CachedQuery
	fn            func(dt float32, it *QueryIter)
	phase         *Phase    // which pipeline phase this system belongs to
	predecessors  []*System // DependsOn edges: this system runs after these
	removed       bool
	enabled       bool            // false = skip during pipeline dispatch; default true
	parallel      bool            // opt-in parallel dispatch; default false
	multiThreaded bool            // opt-in within-system row-range split; default false
	writeSetFixed bool            // true after SetWriteSet called
	writeSet      map[ID]struct{} // nil = derive from query terms; non-nil (incl. empty) = explicit
	interval      time.Duration   // 0 = no interval gate (every tick)
	intervalAccum time.Duration   // accumulated time since last interval fire
	rate          int32           // 0 or 1 = no rate gate (every tick)
	rateCounter   int32           // increments each pipeline visit; fires when counter%rate==0
}

// NewSystem registers a new system on w in the OnUpdate phase that runs q's
// callback fn on each World.Progress(dt) call. Systems within a phase run in
// registration order.
//
// Panics if any argument is nil, if q is already closed, or if q belongs to a
// different world than w.
//
// Closed (removed) entries in w.systems are compacted lazily before the new
// system is appended — the same amortized pattern as NewCachedQuery.
//
// The query must be *CachedQuery (not uncached *Query) because systems run every
// frame; the cached path's pre-filtered table tracking is essential for
// per-frame performance.
func NewSystem(w *World, q *CachedQuery, fn func(dt float32, it *QueryIter)) *System {
	if w == nil {
		panic("flecs: NewSystem: world must not be nil")
	}
	w.checkExclusiveAccessWrite()
	if q == nil {
		panic("flecs: NewSystem: query must not be nil")
	}
	if fn == nil {
		panic("flecs: NewSystem: fn must not be nil")
	}
	if q.IsClosed() {
		panic("flecs: NewSystem: query is already closed")
	}
	if q.w != w {
		panic("flecs: NewSystem: query belongs to a different world")
	}

	// Compact removed entries lazily before appending.
	live := w.systems[:0]
	for _, s := range w.systems {
		if !s.removed {
			live = append(live, s)
		}
	}
	w.systems = live

	sys := &System{w: w, query: q, fn: fn, phase: w.onUpdate, enabled: true}
	w.systems = append(w.systems, sys)
	w.pipelineDirty = true
	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "system added",
			slog.String("phase", sys.phase.name))
	}
	return sys
}

// NewSystemInPhase registers a new system in the given pipeline phase.
// phase may be any built-in or custom *Phase belonging to w.
//
// All other validations match [NewSystem]: panics on nil world/phase/query/fn,
// closed query, or a query/phase belonging to a different world.
func NewSystemInPhase(w *World, phase *Phase, q *CachedQuery, fn func(dt float32, it *QueryIter)) *System {
	if w == nil {
		panic("flecs: NewSystemInPhase: world must not be nil")
	}
	w.checkExclusiveAccessWrite()
	if phase == nil {
		panic("flecs: NewSystemInPhase: phase must not be nil")
	}
	if phase.w != w {
		panic("flecs: NewSystemInPhase: phase belongs to a different world")
	}
	if q == nil {
		panic("flecs: NewSystemInPhase: query must not be nil")
	}
	if fn == nil {
		panic("flecs: NewSystemInPhase: fn must not be nil")
	}
	if q.IsClosed() {
		panic("flecs: NewSystemInPhase: query is already closed")
	}
	if q.w != w {
		panic("flecs: NewSystemInPhase: query belongs to a different world")
	}

	// Compact removed entries lazily before appending.
	live := w.systems[:0]
	for _, s := range w.systems {
		if !s.removed {
			live = append(live, s)
		}
	}
	w.systems = live

	sys := &System{w: w, query: q, fn: fn, phase: phase, enabled: true}
	w.systems = append(w.systems, sys)
	w.pipelineDirty = true
	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "system added",
			slog.String("phase", sys.phase.name))
	}
	return sys
}

// SetEnabled enables or disables this system for pipeline dispatch.
// A disabled system is excluded from Progress runs but remains registered and
// is visible to pipeline introspection (SystemsInPhase, EachSystem).
// Default is true (enabled). Idempotent.
//
// Unlike Close, SetEnabled(false) is reversible: calling SetEnabled(true)
// restores the system to full dispatch.
//
// Note: RunSystem bypasses this flag — explicit invocation always runs the system.
func (s *System) SetEnabled(v bool) { s.enabled = v }

// IsEnabled reports whether this system is enabled for pipeline dispatch.
func (s *System) IsEnabled() bool { return s.enabled }

// SetInterval configures a wall-clock interval gate. The system runs only when
// at least d of accumulated dt has elapsed since its last run. d == 0 disables
// interval gating (system runs every tick). Panics on negative d.
//
// The accumulator uses subtract-with-cap semantics (matching upstream timer.c):
// each fire subtracts d from the accumulator, preserving carry-over for
// back-to-back fast ticks; a single tick whose dt vastly exceeds d clamps the
// remainder to 0 to prevent runaway catch-up.
//
// interval and rate gates compose with AND semantics: both must pass in the same
// tick for the system to run. This diverges from upstream C flecs, which rejects
// systems with both fields set; Go-flecs allows the combination because there is
// no tick_source abstraction and the two filters compose cleanly per-system.
//
// Modify only between ticks, not from inside a system callback during dispatch.
func (s *System) SetInterval(d time.Duration) {
	if d < 0 {
		panic("flecs: SetInterval: interval must be >= 0")
	}
	s.interval = d
	s.intervalAccum = 0
}

// GetInterval returns the current interval gate value (0 = disabled).
func (s *System) GetInterval() time.Duration { return s.interval }

// SetRate configures a tick-count rate gate. The system runs every nth pipeline
// visit where n%rate == 0. n == 0 or n == 1 disables rate gating. Panics on
// negative n.
//
// The rate counter increments on every pipeline visit while the system is
// enabled; it does NOT advance during disabled periods. Re-enabling resumes
// from the pre-disable counter value — no catch-up occurs.
//
// Modify only between ticks, not from inside a system callback during dispatch.
func (s *System) SetRate(n int32) {
	if n < 0 {
		panic("flecs: SetRate: rate must be >= 0")
	}
	s.rate = n
	s.rateCounter = 0
}

// GetRate returns the current rate gate value (0 or 1 = disabled).
func (s *System) GetRate() int32 { return s.rate }

// SetParallel sets whether this system is eligible for parallel dispatch.
// Default is false (serial). When true and the world's WorkerCount is > 0,
// this system may run concurrently with other parallel systems in the same
// phase whose write sets are pairwise disjoint.
//
// Parallel systems must not call Field on each other's QueryIter; each system
// owns its own iterator. Structural mutations (Set, Remove, Delete) from
// within a parallel system are queued through the deferred mechanism and
// applied after the phase completes.
func (s *System) SetParallel(v bool) { s.parallel = v }

// Parallel reports whether this system is flagged for parallel dispatch.
func (s *System) Parallel() bool { return s.parallel }

// SetMultiThreaded sets whether this system uses within-system parallelism.
// Default is false. When true and the world's WorkerCount is > 0, the system
// is dispatched as N concurrent worker jobs, each receiving a disjoint slice
// of every matched table's row range (N = WorkerCount). A multi-threaded
// system cannot batch with parallel siblings; it always runs alone.
//
// A multi-threaded system runs across all workers configured by
// World.SetWorkerCount. Each worker receives a disjoint slice of every matched
// table's row range. The user's fn may read and write component slices in place
// without synchronization (workers' slices never overlap). Calls to World.Set,
// Delete, AddID etc. from inside the iter loop are safe but contend on the
// world's defer queue — for in-place updates, prefer mutating Field[T] slices
// directly to maximize scaling.
//
// Per-stage command queues (Phase 11.0, task #40) are the follow-up that
// unlocks linear scaling for deferred mutations.
func (s *System) SetMultiThreaded(v bool) { s.multiThreaded = v }

// MultiThreaded reports whether this system is flagged for multi-threaded dispatch.
func (s *System) MultiThreaded() bool { return s.multiThreaded }

// SetWriteSet declares the set of component IDs this system writes. This
// overrides the default, which is derived from the system's query terms
// (all And, Or, and Optional IDs).
//
// Pass an empty slice to declare a read-only system that never conflicts with
// any other parallel system.
//
// Conflict detection uses the write set for O(1) overlap checks. The world
// over-approximates: even read-only access to a component that another system
// writes is treated as a conflict unless SetWriteSet([]) is explicitly used.
func (s *System) SetWriteSet(ids []ID) {
	m := make(map[ID]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	s.writeSet = m
	s.writeSetFixed = true
}

// effectiveWriteSet returns the write set used for conflict detection.
// Returns the explicitly set map when SetWriteSet was called, otherwise
// derives it from the system's query terms (And, Or, Optional IDs).
func (s *System) effectiveWriteSet() map[ID]struct{} {
	if s.writeSetFixed {
		return s.writeSet
	}
	terms := s.query.TermsFull()
	m := make(map[ID]struct{}, len(terms))
	for _, t := range terms {
		if t.Kind == TermAnd || t.Kind == TermOr || t.Kind == TermOptional {
			m[t.ID] = struct{}{}
		}
	}
	return m
}

// Close marks this system as removed. Idempotent: safe to call multiple times.
// After Close returns, the system will be skipped in subsequent Progress calls.
//
// Deferred-removal semantics: if Close is called by another system during an
// active Progress dispatch, whether this system still runs in the current frame
// depends on whether Progress already captured the active set before Close was
// called. Progress snapshots the active systems at the start of dispatch; any
// system in that snapshot still runs even if Close is called mid-frame. On
// the next Progress call the system is definitively skipped.
// DependsOn declares that this system runs after other within the same phase.
// Both systems must belong to the same *Phase or the call panics with a
// message naming both systems and their phases. Idempotent. Returns this for
// fluent chaining.
func (s *System) DependsOn(other *System) *System {
	if other == nil {
		panic("flecs: System.DependsOn: other must not be nil")
	}
	if s.phase != other.phase {
		panic(fmt.Sprintf(
			"flecs: System.DependsOn: systems must be in the same phase "+
				"(this system is in %q, other is in %q)",
			s.phase.name, other.phase.name,
		))
	}
	for _, pred := range s.predecessors {
		if pred == other {
			return s // idempotent
		}
	}
	s.predecessors = append(s.predecessors, other)
	s.w.pipelineDirty = true
	return s
}

func (s *System) Close() {
	if s.removed {
		return
	}
	s.removed = true
	s.w.pipelineDirty = true
	if s.w.logger != nil {
		s.w.logger.LogAttrs(context.Background(), slog.LevelDebug, "system closed")
	}
}

// IsClosed reports whether Close has been called.
func (s *System) IsClosed() bool {
	return s.removed
}

// Progress runs all registered (non-closed) systems in pipeline phase order.
// The default order is PreUpdate → OnFixedUpdate → OnUpdate → PostUpdate.
// Custom phases created with [NewPhase] are inserted at the position defined
// by their [Phase.DependsOn] declarations. Within each phase, systems run in
// the order defined by their [System.DependsOn] declarations, with registration
// order as the tiebreaker.
//
// dt must be >= 0; Progress panics on negative dt. A zero dt is allowed and
// counts as a "null frame" that still increments FrameCount.
//
// Each phase is wrapped in its own deferred-command scope. The OnFixedUpdate
// phase runs inside a per-iteration deferred scope so that mutations from one
// fixed tick are visible to the next fixed tick within the same Progress call.
//
// Mutations queued by systems in an earlier phase are flushed before the next
// phase starts, so cross-phase visibility is guaranteed.
//
// If pipelineDirty is true (any phase or system was added/removed/reordered
// since the last Progress call), the topo sort is rebuilt before dispatch.
//
// Compaction of closed systems is NOT done here; it happens lazily in NewSystem
// and NewSystemInPhase.
func (w *World) Progress(dt float32) {
	if dt < 0 {
		panic("flecs: Progress: dt must be >= 0")
	}
	w.checkExclusiveAccessWrite()
	if w.pipelineDirty {
		w.rebuildPipeline()
	}
	w.inProgress = true
	defer func() { w.inProgress = false }()
	w.frameCount++
	w.time += dt

	w.lastFramePhases = make([]PhaseStats, len(w.phaseOrder))

	runPhase := func(p *Phase, phaseDT float32) {
		w.deferScope(func() {
			active := make([]*System, 0, len(p.orderedSystems))
			for _, s := range p.orderedSystems {
				if s.removed || !s.enabled {
					continue
				}
				if s.interval > 0 {
					s.intervalAccum += time.Duration(float64(phaseDT) * float64(time.Second))
					if s.intervalAccum < s.interval {
						continue
					}
					s.intervalAccum -= s.interval
					if s.intervalAccum > s.interval {
						s.intervalAccum = 0
					}
				}
				if s.rate > 1 {
					s.rateCounter++
					if s.rateCounter%s.rate != 0 {
						continue
					}
				}
				active = append(active, s)
			}
			if w.workerCount == 0 {
				// Serial dispatch: single-threaded behavior unchanged.
				for _, s := range active {
					it := s.query.Iter()
					s.fn(phaseDT, it)
				}
				return
			}
			// Worker-pool dispatch. Multi-threaded systems commandeer all
			// workers for within-system row-range parallelism; parallel
			// systems batch across disjoint write sets; serial systems run
			// on the calling goroutine.
			i := 0
			for i < len(active) {
				s := active[i]
				if s.multiThreaded {
					// Within-system parallelism: split the iter across all N
					// workers, each receiving a disjoint row slice per table.
					// Each worker writes into its own stage queue (no contention).
					// Cannot batch with siblings — waits for all workers before
					// the next system starts.
					n := w.workerCount
					base := s.query.Iter()
					var wg sync.WaitGroup
					for wi := 0; wi < n; wi++ {
						wi := wi
						workerIt := base.clippedCopy(wi, n)
						workerIt.workerWriter = &w.workerStageWriters[wi]
						wg.Add(1)
						w.workerCh <- func() {
							defer wg.Done()
							s.fn(phaseDT, workerIt)
						}
					}
					wg.Wait()
					// Merge worker stages in ascending id order, then stage 0.
					// Within each stage the per-entity coalescer applies; across
					// stages there is no coalescing (two stages mutating the same
					// entity produce two migrations in id order).
					for i := 1; i <= n; i++ {
						q := w.stages[i].queue
						w.stages[i].queue = acquireCmdQueue()
						q.flush(w)
						releaseCmdQueue(q)
					}
					q0 := w.stages[0].queue
					w.stages[0].queue = acquireCmdQueue()
					q0.flush(w)
					releaseCmdQueue(q0)
					// stages[0].deferDepth is unchanged (managed by deferScope).
					i++
					continue
				}
				if !s.parallel {
					it := s.query.Iter()
					s.fn(phaseDT, it)
					i++
					continue
				}
				// Collect a parallel batch: advance i while systems are
				// parallel, not multi-threaded, and their write sets are
				// disjoint with the running union.
				batchStart := i
				var batchUnion map[ID]struct{}
				for i < len(active) && active[i].parallel && !active[i].multiThreaded {
					ws := active[i].effectiveWriteSet()
					conflict := false
					for id := range ws {
						if _, ok := batchUnion[id]; ok {
							conflict = true
							break
						}
					}
					if conflict {
						break
					}
					if batchUnion == nil {
						batchUnion = make(map[ID]struct{}, len(ws))
					}
					for id := range ws {
						batchUnion[id] = struct{}{}
					}
					i++
				}
				batch := active[batchStart:i]
				if len(batch) <= 1 {
					if len(batch) == 1 {
						it := batch[0].query.Iter()
						batch[0].fn(phaseDT, it)
					}
					continue
				}
				// Dispatch all systems in the batch as concurrent jobs.
				var wg sync.WaitGroup
				for _, bs := range batch {
					bs := bs
					wg.Add(1)
					w.workerCh <- func() {
						defer wg.Done()
						it := bs.query.Iter()
						bs.fn(phaseDT, it)
					}
				}
				wg.Wait()
			}
		})
	}

	countPhase := func(p *Phase) int {
		n := 0
		for _, s := range p.orderedSystems {
			if !s.removed && s.enabled {
				n++
			}
		}
		return n
	}

	for i, phase := range w.phaseOrder {
		w.lastFramePhases[i].Name = phase.name
		if !phase.enabled {
			continue
		}
		if phase == w.onFixedUpdate {
			// Special: accumulator loop; sum durations across all fixed iterations.
			count := countPhase(phase)
			w.lastFramePhases[i].SystemCount = count
			var total time.Duration
			if w.fixedTimestep > 0 {
				w.fixedAccumulator += dt
				for w.fixedAccumulator >= w.fixedTimestep {
					step := w.fixedTimestep
					if count > 0 {
						start := time.Now()
						runPhase(phase, step)
						total += time.Since(start)
					} else {
						runPhase(phase, step)
					}
					w.fixedAccumulator -= step
				}
			}
			w.lastFramePhases[i].Duration = total
			continue
		}
		count := countPhase(phase)
		w.lastFramePhases[i].SystemCount = count
		if count > 0 {
			start := time.Now()
			runPhase(phase, dt)
			w.lastFramePhases[i].Duration = time.Since(start)
		} else {
			runPhase(phase, dt)
		}
	}
}

// SystemCount returns the number of currently registered (non-closed) systems.
func (w *World) SystemCount() int {
	w.checkExclusiveAccessRead()
	n := 0
	for _, s := range w.systems {
		if !s.removed {
			n++
		}
	}
	return n
}

// RunSystem invokes s once synchronously with the given dt, outside the normal
// pipeline. The system's query and callback run exactly as in a Progress call,
// but phase ordering, parallel batching, and multi-threaded splitting are all
// bypassed. The disabled flag (SetEnabled) is also bypassed — explicit
// invocation always runs the system regardless of its enabled state.
//
// Mutations are deferred and flushed before RunSystem returns, matching the
// flecs_defer_begin / flecs_defer_end wrap in C ecs_run.
//
// Panics if s is nil or s is closed (IsClosed() == true).
func RunSystem(s *System, dt float32) {
	if s == nil {
		panic("flecs: RunSystem: system must not be nil")
	}
	if s.removed {
		panic("flecs: RunSystem: system is closed")
	}
	s.w.checkExclusiveAccessWrite()
	s.w.deferScope(func() {
		it := s.query.Iter()
		s.fn(dt, it)
	})
}
