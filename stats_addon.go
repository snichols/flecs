package flecs

import (
	"fmt"
	"math"
	"time"
)

// statsWindowSize is the fixed slot count for every aggregation period,
// matching upstream ECS_STAT_WINDOW = 60 (flecs/src/addons/monitor.c).
const statsWindowSize = 60

// StatsPeriod selects which aggregation window to query.
type StatsPeriod int

const (
	// StatsSecond returns the reduced gauge across the last ≤60 ticks.
	// One tick ≈ one second only when the pipeline runs at 1 Hz.
	StatsSecond StatsPeriod = iota
	// StatsMinute returns the reduced gauge across the last ≤60 second-reductions.
	StatsMinute
	// StatsHour returns the reduced gauge across the last ≤60 minute-reductions.
	StatsHour
)

// MetricGauge holds Avg/Min/Max statistics for a metric over a window.
// The JSON tags use lowercase names to match upstream REST conventions.
type MetricGauge struct {
	Avg float64 `json:"avg"`
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// MetricCounter holds rate and cumulative count for a counter metric.
type MetricCounter struct {
	Rate  float64 `json:"rate"`
	Value float64 `json:"value"`
}

// StatsWindow is a fixed 60-slot ring buffer of MetricGauge values.
// Record pushes an instant float64 value; Reduce computes Avg/Min/Max across
// all filled slots; Last returns the most recently recorded gauge.
//
// Time-to-window mapping: "1 tick ≈ 1 second" only when the pipeline runs at
// 1 Hz. StatsWindow is tick-based, not wall-clock-based.
type StatsWindow struct {
	slots [statsWindowSize]MetricGauge
	head  int
	count int
}

// Record pushes value into the ring buffer as an instant gauge (Avg=Min=Max=value).
func (sw *StatsWindow) Record(value float64) {
	sw.slots[sw.head] = MetricGauge{Avg: value, Min: value, Max: value}
	sw.head = (sw.head + 1) % statsWindowSize
	if sw.count < statsWindowSize {
		sw.count++
	}
}

// recordGauge pushes a pre-computed MetricGauge into the ring (used for minute/hour reduction).
func (sw *StatsWindow) recordGauge(g MetricGauge) {
	sw.slots[sw.head] = g
	sw.head = (sw.head + 1) % statsWindowSize
	if sw.count < statsWindowSize {
		sw.count++
	}
}

// Reduce returns Avg/Min/Max across all filled slots. Returns zero MetricGauge
// if no values have been recorded (divide-by-zero safe).
func (sw *StatsWindow) Reduce() MetricGauge {
	if sw.count == 0 {
		return MetricGauge{}
	}
	sum := 0.0
	min := math.MaxFloat64
	max := -math.MaxFloat64
	for i := 0; i < sw.count; i++ {
		// Oldest slot is at (head - count + i + statsWindowSize) % statsWindowSize.
		// Adding statsWindowSize guarantees non-negative before mod when count ≤ 60.
		idx := (sw.head - sw.count + i + statsWindowSize) % statsWindowSize
		g := sw.slots[idx]
		sum += g.Avg
		if g.Min < min {
			min = g.Min
		}
		if g.Max > max {
			max = g.Max
		}
	}
	return MetricGauge{Avg: sum / float64(sw.count), Min: min, Max: max}
}

// Last returns the most recently recorded MetricGauge, or zero if empty.
func (sw *StatsWindow) Last() MetricGauge {
	if sw.count == 0 {
		return MetricGauge{}
	}
	return sw.slots[(sw.head-1+statsWindowSize)%statsWindowSize]
}

// WorldStatsAggregated holds reduced MetricGauge values for one aggregation
// period per world metric. Returned by (*World).WorldStatsWindow.
type WorldStatsAggregated struct {
	EntityCount    MetricGauge `json:"entity_count"`
	TableCount     MetricGauge `json:"table_count"`
	ArchetypeCount MetricGauge `json:"archetype_count"`
	FrameCount     MetricGauge `json:"frame_count"`
	TotalTime      MetricGauge `json:"total_time"`
	LastTickDelta  MetricGauge `json:"last_tick_delta"`
}

// PhaseStatsAggregated holds reduced MetricGauge values for one pipeline phase.
type PhaseStatsAggregated struct {
	Name        string      `json:"name"`
	Duration    MetricGauge `json:"duration"`
	SystemCount MetricGauge `json:"system_count"`
}

// SystemStatsAggregated holds reduced MetricGauge values for one system.
type SystemStatsAggregated struct {
	Name             string      `json:"name"`
	LastTickDuration MetricGauge `json:"last_tick_duration"`
	Invocations      MetricGauge `json:"invocations"`
	AvgDuration      MetricGauge `json:"avg_duration"`
}

// PipelineStatsAggregated holds reduced MetricGauge values for one aggregation
// period across world metrics plus per-phase and per-system breakdown.
type PipelineStatsAggregated struct {
	World   WorldStatsAggregated    `json:"world"`
	Phases  []PhaseStatsAggregated  `json:"phases,omitempty"`
	Systems []SystemStatsAggregated `json:"systems,omitempty"`
}

// worldAggWindows holds one StatsWindow per world metric (for one period tier).
type worldAggWindows struct {
	EntityCount    StatsWindow
	TableCount     StatsWindow
	ArchetypeCount StatsWindow
	FrameCount     StatsWindow
	TotalTime      StatsWindow
	LastTickDelta  StatsWindow
}

func (w *worldAggWindows) record(s WorldStats) {
	w.EntityCount.Record(float64(s.EntityCount))
	w.TableCount.Record(float64(s.TableCount))
	w.ArchetypeCount.Record(float64(s.ArchetypeCount))
	w.FrameCount.Record(float64(s.FrameCount))
	w.TotalTime.Record(s.TotalTime)
	w.LastTickDelta.Record(s.LastTickDelta)
}

func (w *worldAggWindows) reduceInto(dst *worldAggWindows) {
	dst.EntityCount.recordGauge(w.EntityCount.Reduce())
	dst.TableCount.recordGauge(w.TableCount.Reduce())
	dst.ArchetypeCount.recordGauge(w.ArchetypeCount.Reduce())
	dst.FrameCount.recordGauge(w.FrameCount.Reduce())
	dst.TotalTime.recordGauge(w.TotalTime.Reduce())
	dst.LastTickDelta.recordGauge(w.LastTickDelta.Reduce())
}

func (w *worldAggWindows) toAggregated() WorldStatsAggregated {
	return WorldStatsAggregated{
		EntityCount:    w.EntityCount.Reduce(),
		TableCount:     w.TableCount.Reduce(),
		ArchetypeCount: w.ArchetypeCount.Reduce(),
		FrameCount:     w.FrameCount.Reduce(),
		TotalTime:      w.TotalTime.Reduce(),
		LastTickDelta:  w.LastTickDelta.Reduce(),
	}
}

// phaseAggWindows holds per-phase duration and system-count StatsWindows.
type phaseAggWindows struct {
	Duration    StatsWindow
	SystemCount StatsWindow
}

// sysAggWindows holds per-system metric StatsWindows.
type sysAggWindows struct {
	LastTickDuration StatsWindow
	Invocations      StatsWindow
	AvgDuration      StatsWindow
}

// pipelineAggWindows holds aggregation state for a full pipeline snapshot.
type pipelineAggWindows struct {
	SystemCount StatsWindow
	phases      map[string]*phaseAggWindows
	systems     map[string]*sysAggWindows
}

func (p *pipelineAggWindows) phaseFor(name string) *phaseAggWindows {
	if p.phases == nil {
		p.phases = make(map[string]*phaseAggWindows)
	}
	pw, ok := p.phases[name]
	if !ok {
		pw = &phaseAggWindows{}
		p.phases[name] = pw
	}
	return pw
}

func (p *pipelineAggWindows) sysFor(name string) *sysAggWindows {
	if p.systems == nil {
		p.systems = make(map[string]*sysAggWindows)
	}
	sw, ok := p.systems[name]
	if !ok {
		sw = &sysAggWindows{}
		p.systems[name] = sw
	}
	return sw
}

func (p *pipelineAggWindows) recordSnapshot(snap PipelineStats) {
	p.SystemCount.Record(float64(len(snap.Systems)))
	for _, ph := range snap.Phases {
		pw := p.phaseFor(ph.Name)
		pw.Duration.Record(float64(ph.Duration))
		pw.SystemCount.Record(float64(ph.SystemCount))
	}
	for _, sys := range snap.Systems {
		sw := p.sysFor(sys.Name)
		sw.LastTickDuration.Record(float64(sys.LastTickDuration))
		sw.Invocations.Record(float64(sys.Invocations))
		sw.AvgDuration.Record(float64(sys.AvgDuration))
	}
}

func (p *pipelineAggWindows) reduceInto(dst *pipelineAggWindows) {
	dst.SystemCount.recordGauge(p.SystemCount.Reduce())
	for name, pw := range p.phases {
		dp := dst.phaseFor(name)
		dp.Duration.recordGauge(pw.Duration.Reduce())
		dp.SystemCount.recordGauge(pw.SystemCount.Reduce())
	}
	for name, sw := range p.systems {
		ds := dst.sysFor(name)
		ds.LastTickDuration.recordGauge(sw.LastTickDuration.Reduce())
		ds.Invocations.recordGauge(sw.Invocations.Reduce())
		ds.AvgDuration.recordGauge(sw.AvgDuration.Reduce())
	}
}

func (p *pipelineAggWindows) toAggregated() PipelineStatsAggregated {
	var phases []PhaseStatsAggregated
	for name, pw := range p.phases {
		phases = append(phases, PhaseStatsAggregated{
			Name:        name,
			Duration:    pw.Duration.Reduce(),
			SystemCount: pw.SystemCount.Reduce(),
		})
	}
	var systems []SystemStatsAggregated
	for name, sw := range p.systems {
		systems = append(systems, SystemStatsAggregated{
			Name:             name,
			LastTickDuration: sw.LastTickDuration.Reduce(),
			Invocations:      sw.Invocations.Reduce(),
			AvgDuration:      sw.AvgDuration.Reduce(),
		})
	}
	return PipelineStatsAggregated{
		Phases:  phases,
		Systems: systems,
	}
}

// statsAggTick advances the aggregator one tick using the currently committed
// snapshot values. Must be called while statsMu is held for writing.
// Mirrors upstream flecs_stats_reduce / flecs_stats_reduce_last cascade
// (flecs/src/addons/monitor.c).
func (w *World) statsAggTick() {
	snap := PipelineStats{
		World: WorldStats{
			EntityCount:    w.statsEntityCount,
			TableCount:     w.statsTableCount,
			ArchetypeCount: w.statsTableCount,
			FrameCount:     w.statsFrameCount,
			TotalTime:      w.statsTotalTime,
			LastTickDelta:  w.statsLastDelta,
		},
		Phases:  w.statsPhaseSnapshot,
		Systems: w.statsSystemSnapshot,
	}

	w.statsAggWorldSec.record(snap.World)
	w.statsAggPipeSec.recordSnapshot(snap)

	w.statsAggTickCount++

	if w.statsAggTickCount%statsWindowSize == 0 {
		w.statsAggWorldSec.reduceInto(&w.statsAggWorldMin)
		w.statsAggPipeSec.reduceInto(&w.statsAggPipeMin)

		minuteTick := w.statsAggTickCount / statsWindowSize
		if minuteTick%statsWindowSize == 0 {
			w.statsAggWorldMin.reduceInto(&w.statsAggWorldHour)
			w.statsAggPipeMin.reduceInto(&w.statsAggPipeHour)
		}
	}
}

// StatsTick manually advances the aggregator one tick using the current
// committed world state. Designed for use in tests; in production the
// aggregator advances automatically during each Progress call.
func (w *World) StatsTick() {
	w.statsMu.Lock()
	defer w.statsMu.Unlock()
	w.statsAggTick()
}

// WorldStatsWindow returns the reduced WorldStatsAggregated for the given period.
// The second window reflects up to the last 60 ticks; the minute window reflects
// up to the last 60 second-reductions (≈60 minutes at 1 Hz); the hour window
// reflects up to the last 60 minute-reductions (≈60 hours at 1 Hz).
//
// Safe to call concurrently. Returns zero gauges if no ticks have been committed
// for the requested period.
func (w *World) WorldStatsWindow(period StatsPeriod) WorldStatsAggregated {
	w.statsMu.RLock()
	defer w.statsMu.RUnlock()
	switch period {
	case StatsMinute:
		return w.statsAggWorldMin.toAggregated()
	case StatsHour:
		return w.statsAggWorldHour.toAggregated()
	default:
		return w.statsAggWorldSec.toAggregated()
	}
}

// PipelineStatsWindow returns the reduced PipelineStatsAggregated for the given period.
// Safe to call concurrently.
func (w *World) PipelineStatsWindow(period StatsPeriod) PipelineStatsAggregated {
	w.statsMu.RLock()
	defer w.statsMu.RUnlock()
	var pw *pipelineAggWindows
	switch period {
	case StatsMinute:
		pw = &w.statsAggPipeMin
	case StatsHour:
		pw = &w.statsAggPipeHour
	default:
		pw = &w.statsAggPipeSec
	}
	agg := pw.toAggregated()
	agg.World = func() WorldStatsAggregated {
		switch period {
		case StatsMinute:
			return w.statsAggWorldMin.toAggregated()
		case StatsHour:
			return w.statsAggWorldHour.toAggregated()
		default:
			return w.statsAggWorldSec.toAggregated()
		}
	}()
	return agg
}

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
	w.statsAggTick()
}
