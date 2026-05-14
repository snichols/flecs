package flecs

import (
	"fmt"
	"time"
)

// WorldStats is a snapshot of world-level counters from the Stats addon.
// Reflects the state at the end of the most recently completed Progress call.
type WorldStats struct {
	// EntityCount is the number of alive entities at the end of the last tick.
	EntityCount int
	// TableCount is the number of archetype tables in the world.
	TableCount int
	// ArchetypeCount mirrors TableCount; each table represents one archetype.
	ArchetypeCount int
	// FrameCount is the number of Progress calls completed since world creation.
	FrameCount uint64
	// TotalTime is the total accumulated simulation time in seconds.
	TotalTime float64
	// LastTickDelta is the dt value passed to the most recent Progress call.
	// Zero before the first Progress call.
	LastTickDelta float64
}

// SystemStats is a per-system performance snapshot from the Stats addon.
type SystemStats struct {
	// Name is the system's display name. Set via SetName; auto-generated otherwise.
	Name string
	// LastTickDuration is the wall-clock execution time in the most recent tick.
	// Zero if the system did not run (disabled, interval-gated, or rate-gated).
	LastTickDuration time.Duration
	// Invocations is the total number of times this system ran since world creation.
	Invocations uint64
	// AvgDuration is statsTotalDuration/Invocations, or zero if Invocations is zero.
	AvgDuration time.Duration
	// TotalSkipped is the number of ticks bypassed by interval or rate gating.
	// Does not count ticks where the system was disabled.
	TotalSkipped uint64
}

// PipelineStats is a full pipeline performance snapshot returned by StatsSnapshot.
type PipelineStats struct {
	// World holds world-level counters for the last completed tick.
	World WorldStats
	// Systems holds per-system stats for all non-removed systems, in pipeline order.
	// Nil before the first Progress call.
	Systems []SystemStats
	// Phases holds per-phase timing in topological order.
	// Nil before the first Progress call.
	Phases []PhaseStats
}

// StatsSnapshot returns a full pipeline performance snapshot reflecting the
// state after the most recently completed Progress call.
//
// Safe to call concurrently from any goroutine. The returned value is a fully
// copied snapshot with no aliased slices — mutations do not affect subsequent
// calls, and retained snapshots are not updated by future Progress calls.
//
// Before the first Progress call, per-tick and cumulative fields are zero and
// Systems/Phases are nil.
func (w *World) StatsSnapshot() PipelineStats {
	w.statsMu.RLock()
	defer w.statsMu.RUnlock()

	var phases []PhaseStats
	if len(w.statsPhaseSnapshot) > 0 {
		phases = make([]PhaseStats, len(w.statsPhaseSnapshot))
		copy(phases, w.statsPhaseSnapshot)
	}

	var systems []SystemStats
	if len(w.statsSystemSnapshot) > 0 {
		systems = make([]SystemStats, len(w.statsSystemSnapshot))
		copy(systems, w.statsSystemSnapshot)
	}

	return PipelineStats{
		World: WorldStats{
			EntityCount:    w.statsEntityCount,
			TableCount:     w.statsTableCount,
			ArchetypeCount: w.statsTableCount,
			FrameCount:     w.statsFrameCount,
			TotalTime:      w.statsTotalTime,
			LastTickDelta:  w.statsLastDelta,
		},
		Systems: systems,
		Phases:  phases,
	}
}

// SetName sets a display name for this system, returned in SystemStats.Name.
// Returns the receiver for fluent chaining.
func (s *System) SetName(name string) *System {
	s.statsName = name
	return s
}

// statsAutoName returns an auto-generated display name for a system at
// registration index idx (after compaction of removed entries).
func statsAutoName(idx int) string {
	return fmt.Sprintf("system-%d", idx)
}

// statsCommit publishes the current tick's accumulated per-system and per-phase
// measurements to the snapshot cache under statsMu. Called at the end of each
// Progress call from the Progress goroutine.
func (w *World) statsCommit(dt float32) {
	w.statsMu.Lock()
	defer w.statsMu.Unlock()

	w.statsLastDelta = float64(dt)
	w.statsEntityCount = w.index.Count()
	w.statsTableCount = len(w.tables)
	w.statsFrameCount = w.frameCount
	w.statsTotalTime = float64(w.time)

	// Commit per-system tick accumulators and build snapshot.
	w.statsSystemSnapshot = w.statsSystemSnapshot[:0]
	for _, s := range w.systems {
		if s.removed {
			continue
		}
		if s.statsTickDidRun {
			s.statsInvocations++
			s.statsTotalDuration += s.statsTickDuration
			s.statsLastDuration = s.statsTickDuration
			s.statsTickDidRun = false
			s.statsTickDuration = 0
		} else {
			s.statsLastDuration = 0
		}
		avg := time.Duration(0)
		if s.statsInvocations > 0 {
			avg = s.statsTotalDuration / time.Duration(s.statsInvocations)
		}
		w.statsSystemSnapshot = append(w.statsSystemSnapshot, SystemStats{
			Name:             s.statsName,
			LastTickDuration: s.statsLastDuration,
			Invocations:      s.statsInvocations,
			AvgDuration:      avg,
			TotalSkipped:     s.statsSkipped,
		})
	}

	// Commit per-phase cumulative accumulators and build snapshot.
	if len(w.statsPhaseSnapshot) != len(w.phaseOrder) {
		w.statsPhaseSnapshot = make([]PhaseStats, len(w.phaseOrder))
	}
	for i, phase := range w.phaseOrder {
		dur := w.lastFramePhases[i].Duration
		sysCount := w.lastFramePhases[i].SystemCount
		phase.statsCumDuration += dur
		phase.statsInvocations++
		w.statsPhaseSnapshot[i] = PhaseStats{
			Name:               phase.name,
			SystemCount:        sysCount,
			Duration:           dur,
			CumulativeDuration: phase.statsCumDuration,
			Invocations:        phase.statsInvocations,
		}
	}
}
