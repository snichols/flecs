package flecs

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
)

// Phase is an opaque handle for a pipeline phase. Systems registered in a
// phase run together in each Progress call, after all phases they depend on.
//
// The four built-in phases are accessed via [World.PreUpdate],
// [World.OnFixedUpdate], [World.OnUpdate], and [World.PostUpdate], in that
// execution order. Custom phases are created with [NewPhase]; each custom phase
// must anchor itself to the built-in chain via [Phase.DependsOn].
//
// *Phase is NOT goroutine-safe; external synchronization is required.
type Phase struct {
	name           string
	w              *World
	predecessors   []*Phase // DependsOn edges: this phase runs after these
	enabled        bool
	orderedSystems []*System // cached topo order, set by rebuildPipeline
}

// NewPhase creates a new custom pipeline phase in world w with the given name.
// By default the phase has no ordering relationship to any other phase; call
// [Phase.DependsOn] to anchor it to the built-in chain.
//
// A custom phase that is not anchored via DependsOn causes [World.Progress] to
// panic on the first tick. Panics if w is nil or name is empty.
func NewPhase(w *World, name string) *Phase {
	if w == nil {
		panic("flecs: NewPhase: world must not be nil")
	}
	if name == "" {
		panic("flecs: NewPhase: name must not be empty")
	}
	w.checkExclusiveAccessWrite()
	p := &Phase{name: name, w: w, enabled: true}
	w.phases = append(w.phases, p)
	w.pipelineDirty = true
	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "phase added",
			slog.String("phase", name))
	}
	return p
}

// DependsOn declares that this phase runs after other in the pipeline.
// Idempotent: calling with the same other multiple times is a no-op.
// Returns this for fluent chaining.
//
// Panics if other is nil or belongs to a different world.
func (p *Phase) DependsOn(other *Phase) *Phase {
	if other == nil {
		panic("flecs: Phase.DependsOn: other must not be nil")
	}
	if other.w != p.w {
		panic("flecs: Phase.DependsOn: other belongs to a different world")
	}
	for _, pred := range p.predecessors {
		if pred == other {
			return p // idempotent
		}
	}
	p.predecessors = append(p.predecessors, other)
	p.w.pipelineDirty = true
	return p
}

// SetEnabled enables or disables this phase for pipeline dispatch.
// A disabled phase and all systems within it are skipped during Progress.
// Default is true (enabled). Idempotent.
func (p *Phase) SetEnabled(v bool) { p.enabled = v }

// IsEnabled reports whether this phase is enabled for pipeline dispatch.
func (p *Phase) IsEnabled() bool { return p.enabled }

// Name returns the display name of this phase.
func (p *Phase) Name() string { return p.name }

// ── Pipeline rebuild ──────────────────────────────────────────────────────────

// rebuildPipeline computes a fresh topological ordering of all phases and, for
// each phase, a topological ordering of its registered systems. Called lazily
// at the start of Progress when pipelineDirty is true.
func (w *World) rebuildPipeline() {
	builtinPhases := map[*Phase]bool{
		w.preUpdate:     true,
		w.onUpdate:      true,
		w.postUpdate:    true,
		w.onFixedUpdate: true,
	}
	w.phaseOrder = kahnSortPhases(w.phases, builtinPhases)
	for _, phase := range w.phases {
		var phaseSystems []*System
		for _, s := range w.systems {
			if !s.removed && s.phase == phase {
				phaseSystems = append(phaseSystems, s)
			}
		}
		phase.orderedSystems = kahnSortSystems(phaseSystems)
	}
	w.pipelineDirty = false
}

// kahnSortPhases returns the phases in topological (DependsOn) order using
// Kahn's algorithm. Insertion order breaks ties so the built-in chain is
// always stable.
//
// Panics:
//   - if any custom (non-built-in) phase has no predecessors (orphan)
//   - if a cycle is detected
func kahnSortPhases(phases []*Phase, builtinPhases map[*Phase]bool) []*Phase {
	if len(phases) == 0 {
		return nil
	}

	// Orphan check: every custom phase must have at least one DependsOn edge.
	for _, p := range phases {
		if !builtinPhases[p] && len(p.predecessors) == 0 {
			panic(fmt.Sprintf(
				"flecs: pipeline build: phase %q has no DependsOn relation; "+
					"custom phases must be ordered via DependsOn",
				p.name,
			))
		}
	}

	// Build in-degree map and successor adjacency lists.
	inDegree := make(map[*Phase]int, len(phases))
	successors := make(map[*Phase][]*Phase, len(phases))
	for _, p := range phases {
		inDegree[p] = 0
	}
	for _, p := range phases {
		for _, pred := range p.predecessors {
			inDegree[p]++
			successors[pred] = append(successors[pred], p)
		}
	}

	// Seed the queue with zero-in-degree phases in insertion order.
	queue := make([]*Phase, 0, len(phases))
	for _, p := range phases {
		if inDegree[p] == 0 {
			queue = append(queue, p)
		}
	}

	result := make([]*Phase, 0, len(phases))
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		result = append(result, cur)
		for _, succ := range successors[cur] {
			inDegree[succ]--
			if inDegree[succ] == 0 {
				queue = append(queue, succ)
			}
		}
	}

	if len(result) != len(phases) {
		var names []string
		for _, p := range phases {
			if inDegree[p] > 0 {
				names = append(names, fmt.Sprintf("%q", p.name))
			}
		}
		panic(fmt.Sprintf(
			"flecs: pipeline build: phase cycle detected among: %v", names,
		))
	}
	return result
}

// kahnSortSystems returns the systems in topological (DependsOn) order within
// a phase. Systems with no DependsOn edges keep their registration order as
// the tiebreaker, matching upstream's entity-ID tiebreak.
//
// Panics if a cycle is detected among the systems.
func kahnSortSystems(systems []*System) []*System {
	if len(systems) == 0 {
		return nil
	}

	// Record registration position for tiebreaking.
	posOf := make(map[*System]int, len(systems))
	for i, s := range systems {
		posOf[s] = i
	}
	sysSet := make(map[*System]bool, len(systems))
	for _, s := range systems {
		sysSet[s] = true
	}

	inDegree := make(map[*System]int, len(systems))
	successors := make(map[*System][]*System, len(systems))
	for _, s := range systems {
		inDegree[s] = 0
	}
	for _, s := range systems {
		for _, pred := range s.predecessors {
			if !sysSet[pred] {
				continue // predecessor closed or in a different phase; skip
			}
			inDegree[s]++
			successors[pred] = append(successors[pred], s)
		}
	}

	// Seed queue with zero-in-degree systems sorted by registration position.
	queue := make([]*System, 0, len(systems))
	for _, s := range systems {
		if inDegree[s] == 0 {
			queue = append(queue, s)
		}
	}
	sort.Slice(queue, func(i, j int) bool {
		return posOf[queue[i]] < posOf[queue[j]]
	})

	result := make([]*System, 0, len(systems))
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		result = append(result, cur)
		for _, succ := range successors[cur] {
			inDegree[succ]--
			if inDegree[succ] == 0 {
				queue = append(queue, succ)
			}
		}
		sort.Slice(queue, func(i, j int) bool {
			return posOf[queue[i]] < posOf[queue[j]]
		})
	}

	if len(result) != len(systems) {
		var addrs []string
		for _, s := range systems {
			if inDegree[s] > 0 {
				addrs = append(addrs, fmt.Sprintf("%p", s))
			}
		}
		panic(fmt.Sprintf(
			"flecs: pipeline build: system cycle detected among: %v", addrs,
		))
	}
	return result
}
