package flecs

import (
	"time"
)

// Stats is a snapshot of world-level counters and per-phase frame timing.
// Designed for tooling and observability; Stats allocates once per call and is
// not intended for hot-path use.
type Stats struct {
	// EntityCount is the number of currently alive entities, including
	// component entities and built-in entities.
	EntityCount int
	// TableCount is the number of archetype tables in the world.
	TableCount int
	// QueryCount is reserved for future uncached *Query tracking. Uncached
	// queries are one-shot value types; the world does not track them.
	// Always 0 in this release.
	QueryCount int
	// CachedQueryCount is the number of active (non-closed) *CachedQuery values.
	CachedQueryCount int
	// SystemCount is the number of active (non-closed) systems.
	SystemCount int
	// FrameCount is the number of Progress calls made on this world.
	FrameCount uint64
	// Time is the total accumulated simulation time in seconds.
	Time float64
	// LastFramePhases holds per-phase timing for the most recent Progress call,
	// indexed by phase order: PreUpdate[0], OnFixedUpdate[1], OnUpdate[2],
	// PostUpdate[3]. Nil if Progress has never been called.
	LastFramePhases []PhaseStats
	// ComponentStats holds per-component table and entity counts.
	// Includes all registered components: data types, tag types, and pair types.
	ComponentStats []ComponentStat
}

// PhaseStats holds per-phase timing for a single Progress call and cumulative totals.
type PhaseStats struct {
	// Name is the phase name: "PreUpdate", "OnFixedUpdate", "OnUpdate", or "PostUpdate".
	Name string
	// SystemCount is the number of active systems registered for this phase.
	// Zero if the phase had no active systems.
	SystemCount int
	// Duration is the wall-clock time for the phase in the last Progress call.
	// Zero if the phase had no active systems.
	// For OnFixedUpdate, this is the sum across all fixed-step iterations.
	Duration time.Duration
	// CumulativeDuration is the total wall-clock time spent in this phase across
	// all Progress calls since world creation.
	CumulativeDuration time.Duration
	// Invocations is the number of Progress calls in which this phase was visited.
	Invocations uint64
}

// ComponentStat holds per-component table and entity counts.
// Both data components (Size > 0) and tag components (Size == 0) are included.
type ComponentStat struct {
	// ID is the component entity ID.
	ID ID
	// Name is the component's registered name.
	Name string
	// Size is unsafe.Sizeof of the component type. Zero for tags.
	Size uintptr
	// TableCount is the number of archetype tables containing this component.
	TableCount int
	// EntityCount is the sum of entity counts across those tables.
	EntityCount int
}

// Stats returns a snapshot of world-level counters and per-phase frame timing.
// Allocates once per call; designed for tooling, not hot-path tracing.
//
// QueryCount is always 0: uncached queries are one-shot value types that the
// world does not track.
//
// LastFramePhases is nil if Progress has never been called on this world.
//
// ComponentStats includes all registered components: data types (Size > 0),
// tag types (Size == 0), and pair types (both data-bearing and tag-only).
func (w *World) Stats() Stats {
	w.checkExclusiveAccessRead()
	s := Stats{
		EntityCount:      w.Count(),
		TableCount:       len(w.tables),
		QueryCount:       0,
		CachedQueryCount: w.cachedQueryCount(),
		SystemCount:      w.SystemCount(),
		FrameCount:       w.frameCount,
		Time:             float64(w.time),
	}
	if w.frameCount > 0 {
		phases := make([]PhaseStats, len(w.lastFramePhases))
		copy(phases, w.lastFramePhases)
		for i, phase := range w.phaseOrder {
			if i < len(phases) {
				phases[i].CumulativeDuration = phase.statsCumDuration
				phases[i].Invocations = phase.statsInvocations
			}
		}
		s.LastFramePhases = phases
	}
	s.ComponentStats = w.buildComponentStats()
	return s
}

// SystemCountInPhase returns the number of registered non-closed systems in the
// given pipeline phase. Disabled systems are included. Panics if phase is nil.
func (w *World) SystemCountInPhase(phase *Phase) int {
	w.checkExclusiveAccessRead()
	if phase == nil {
		panic("flecs: SystemCountInPhase: phase must not be nil")
	}
	if w.pipelineDirty {
		w.rebuildPipeline()
	}
	return len(phase.orderedSystems)
}

// cachedQueryCount returns the number of active (non-closed) cached queries.
func (w *World) cachedQueryCount() int {
	n := 0
	for _, cq := range w.cachedQueries {
		if !cq.removed {
			n++
		}
	}
	return n
}

// buildComponentStats builds per-component table and entity counts for Stats().
func (w *World) buildComponentStats() []ComponentStat {
	ids := w.Components()
	if len(ids) == 0 {
		return nil
	}
	stats := make([]ComponentStat, 0, len(ids))
	for _, id := range ids {
		info, ok := w.registry.LookupByID(id)
		if !ok {
			continue
		}
		tables := w.compIndex.TablesFor(id)
		entityCount := 0
		for _, t := range tables {
			entityCount += t.Count()
		}
		stats = append(stats, ComponentStat{
			ID:          id,
			Name:        info.Name,
			Size:        info.Size,
			TableCount:  len(tables),
			EntityCount: entityCount,
		})
	}
	return stats
}
