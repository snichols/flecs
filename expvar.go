package flecs

import (
	"expvar"
	"log/slog"
	"sync"
	"sync/atomic"
)

// expvarRegistry tracks live handles by prefix to enforce idempotent publish semantics.
var (
	expvarRegistryMu sync.Mutex
	expvarRegistry   = map[string]*ExpvarHandle{}
)

// expvarTree is the whole-tree JSON structure for the scrape-consistent expvar var.
// All fields are captured under a single statsMu.RLock for internal consistency.
type expvarTree struct {
	EntityCount      int                  `json:"entity_count"`
	TableCount       int                  `json:"table_count"`
	ComponentCount   int                  `json:"component_count"`
	SystemCount      int                  `json:"system_count"`
	ObserverCount    int                  `json:"observer_count"`
	FrameCount       uint64               `json:"frame_count"`
	ReclaimedTables  uint64               `json:"reclaimed_tables"`
	LastProgressSecs *float64             `json:"last_progress_seconds,omitempty"`
	Phases           map[string]float64   `json:"phases"`
	WindowSecond     WorldStatsAggregated `json:"window_second"`
	WindowMinute     WorldStatsAggregated `json:"window_minute"`
	WindowHour       WorldStatsAggregated `json:"window_hour"`
}

// expvarFullSnapshot captures all published-var data under a single statsMu.RLock
// for scrape consistency. Use this for the whole-tree var.
func (w *World) expvarFullSnapshot() expvarTree {
	w.statsMu.RLock()
	defer w.statsMu.RUnlock()

	phases := make(map[string]float64, len(w.statsPhaseSnapshot))
	for _, ph := range w.statsPhaseSnapshot {
		phases[ph.Name] = ph.Duration.Seconds()
	}

	var lastProg *float64
	if w.statsLastProgressWall != 0 {
		v := w.statsLastProgressWall
		lastProg = &v
	}

	return expvarTree{
		EntityCount:      w.statsEntityCount,
		TableCount:       w.statsTableCount,
		ComponentCount:   w.statsComponentCount,
		SystemCount:      w.statsSystemCount,
		ObserverCount:    w.statsObserverCount,
		FrameCount:       w.statsFrameCount,
		ReclaimedTables:  w.statsReclaimedTables,
		LastProgressSecs: lastProg,
		Phases:           phases,
		WindowSecond:     w.statsAggWorldSec.toAggregated(),
		WindowMinute:     w.statsAggWorldMin.toAggregated(),
		WindowHour:       w.statsAggWorldHour.toAggregated(),
	}
}

// ExpvarHandle is returned by PublishExpvar. Call Unpublish to null out the
// previously published variables.
type ExpvarHandle struct {
	prefix string
	active atomic.Bool
}

// Unpublish nulls out the expvar variables published under this handle's prefix
// by making each closure return nil (JSON null) on subsequent scrapes.
//
// The variable names remain registered in the global expvar registry for the
// lifetime of the process; expvar provides no deregister API. See
// docs/Observability.md for the full caveat.
func (h *ExpvarHandle) Unpublish() {
	h.active.Store(false)
}

// PublishExpvar registers expvar.Func variables that lazily read live world stats
// on each /debug/vars scrape. prefix namespaces all variables (e.g., "flecs"
// produces "flecs.entity_count", "flecs.table_count", etc.) plus a "flecs"
// whole-tree JSON var for scrape-consistent reads.
//
// Idempotent: if prefix is already registered, returns the existing handle and
// logs a warning. Does not panic on re-publish.
//
// Multiple worlds: each world must use a distinct prefix; the caller is
// responsible for uniqueness. Colliding prefixes return the first handle without
// registering new variables.
//
// No background goroutines are spawned. All stats are read lazily on each scrape.
func PublishExpvar(w *World, prefix string) *ExpvarHandle {
	expvarRegistryMu.Lock()
	defer expvarRegistryMu.Unlock()

	if existing, ok := expvarRegistry[prefix]; ok {
		slog.Warn("flecs: PublishExpvar: prefix already registered; returning existing handle",
			"prefix", prefix)
		return existing
	}

	h := &ExpvarHandle{prefix: prefix}
	h.active.Store(true)
	expvarRegistry[prefix] = h

	pub := func(name string, fn func() any) {
		expvar.Publish(name, expvar.Func(fn))
	}

	// Whole-tree var: one statsMu.RLock per scrape; internally consistent snapshot.
	pub(prefix, func() any {
		if !h.active.Load() {
			return nil
		}
		return w.expvarFullSnapshot()
	})

	// Individual scalar vars: each re-reads independently via separate statsMu.RLock
	// calls; minor inter-variable skew is possible across a /debug/vars scrape.
	// See docs/Observability.md for the documented trade-off.
	pub(prefix+".entity_count", func() any {
		if !h.active.Load() {
			return nil
		}
		return w.StatsSnapshot().World.EntityCount
	})
	pub(prefix+".table_count", func() any {
		if !h.active.Load() {
			return nil
		}
		return w.StatsSnapshot().World.TableCount
	})
	pub(prefix+".component_count", func() any {
		if !h.active.Load() {
			return nil
		}
		w.statsMu.RLock()
		v := w.statsComponentCount
		w.statsMu.RUnlock()
		return v
	})
	pub(prefix+".system_count", func() any {
		if !h.active.Load() {
			return nil
		}
		w.statsMu.RLock()
		v := w.statsSystemCount
		w.statsMu.RUnlock()
		return v
	})
	pub(prefix+".observer_count", func() any {
		if !h.active.Load() {
			return nil
		}
		w.statsMu.RLock()
		v := w.statsObserverCount
		w.statsMu.RUnlock()
		return v
	})
	pub(prefix+".frame_count", func() any {
		if !h.active.Load() {
			return nil
		}
		return w.StatsSnapshot().World.FrameCount
	})
	pub(prefix+".reclaimed_tables", func() any {
		if !h.active.Load() {
			return nil
		}
		w.statsMu.RLock()
		v := w.statsReclaimedTables
		w.statsMu.RUnlock()
		return v
	})
	pub(prefix+".last_progress_seconds", func() any {
		if !h.active.Load() {
			return nil
		}
		w.statsMu.RLock()
		v := w.statsLastProgressWall
		w.statsMu.RUnlock()
		if v == 0 {
			return nil
		}
		return v
	})
	pub(prefix+".phases", func() any {
		if !h.active.Load() {
			return nil
		}
		snap := w.StatsSnapshot()
		phases := make(map[string]float64, len(snap.Phases))
		for _, ph := range snap.Phases {
			phases[ph.Name] = ph.Duration.Seconds()
		}
		return phases
	})
	pub(prefix+".window_second", func() any {
		if !h.active.Load() {
			return nil
		}
		return w.WorldStatsWindow(StatsSecond)
	})
	pub(prefix+".window_minute", func() any {
		if !h.active.Load() {
			return nil
		}
		return w.WorldStatsWindow(StatsMinute)
	})
	pub(prefix+".window_hour", func() any {
		if !h.active.Load() {
			return nil
		}
		return w.WorldStatsWindow(StatsHour)
	})

	return h
}

// ExpvarMap returns an *expvar.Map populated with live world-stats Funcs, for
// callers who want to mount stats under a custom name without touching the
// global expvar registry.
//
// Each value in the map is a live expvar.Func that re-reads stats on each
// access. The map itself is not registered globally; callers may publish it
// via expvar.Publish under any name, or serve it directly.
func ExpvarMap(w *World) *expvar.Map {
	m := new(expvar.Map)
	m.Init()
	m.Set("entity_count", expvar.Func(func() any {
		return w.StatsSnapshot().World.EntityCount
	}))
	m.Set("table_count", expvar.Func(func() any {
		return w.StatsSnapshot().World.TableCount
	}))
	m.Set("component_count", expvar.Func(func() any {
		w.statsMu.RLock()
		v := w.statsComponentCount
		w.statsMu.RUnlock()
		return v
	}))
	m.Set("system_count", expvar.Func(func() any {
		w.statsMu.RLock()
		v := w.statsSystemCount
		w.statsMu.RUnlock()
		return v
	}))
	m.Set("observer_count", expvar.Func(func() any {
		w.statsMu.RLock()
		v := w.statsObserverCount
		w.statsMu.RUnlock()
		return v
	}))
	m.Set("frame_count", expvar.Func(func() any {
		return w.StatsSnapshot().World.FrameCount
	}))
	m.Set("reclaimed_tables", expvar.Func(func() any {
		w.statsMu.RLock()
		v := w.statsReclaimedTables
		w.statsMu.RUnlock()
		return v
	}))
	m.Set("last_progress_seconds", expvar.Func(func() any {
		w.statsMu.RLock()
		v := w.statsLastProgressWall
		w.statsMu.RUnlock()
		if v == 0 {
			return nil
		}
		return v
	}))
	m.Set("phases", expvar.Func(func() any {
		snap := w.StatsSnapshot()
		phases := make(map[string]float64, len(snap.Phases))
		for _, ph := range snap.Phases {
			phases[ph.Name] = ph.Duration.Seconds()
		}
		return phases
	}))
	m.Set("window_second", expvar.Func(func() any {
		return w.WorldStatsWindow(StatsSecond)
	}))
	m.Set("window_minute", expvar.Func(func() any {
		return w.WorldStatsWindow(StatsMinute)
	}))
	m.Set("window_hour", expvar.Func(func() any {
		return w.WorldStatsWindow(StatsHour)
	}))
	return m
}
