## Goal

Add a public ``Stats`` API to the root ``flecs`` package that returns a snapshot of world-level counters and per-phase frame timing for tooling and observability. Most of the data is already collected internally; this phase mostly *exposes* it through a unified ``Stats`` value plus a small amount of new instrumentation in ``Progress`` for per-phase wall-clock timing.

Master HEAD ``9a13ff4`` (tagged ``v0.4.0``). JSON serialization (ChildOf, IsA, custom pairs) and introspection (``Components``, ``ComponentInfo``, ``EachEntity``, ``AliveEntities``) plus dynamic value access (``GetByID`` / ``SetByID`` / ``SetPairByID``) have shipped. What's still missing for tooling: world-level counters for tables / cached queries / per-phase frame time, and per-component table count.

### Target API

After this lands:

```go
stats := w.Stats()
fmt.Printf(\"entities: %d\n\", stats.EntityCount)
fmt.Printf(\"tables:   %d\n\", stats.TableCount)
fmt.Printf(\"queries:  %d (cached: %d)\n\", stats.QueryCount, stats.CachedQueryCount)
fmt.Printf(\"systems:  %d\n\", stats.SystemCount)
fmt.Printf(\"frame:    %d\n\", stats.FrameCount)
fmt.Printf(\"time:     %.3fs\n\", stats.Time)

// Per-phase timing of the LAST frame:
for _, phase := range stats.LastFramePhases {
    fmt.Printf(\"  %s: %v (%d systems)\n\", phase.Name, phase.Duration, phase.SystemCount)
}

// Per-component table count:
for _, c := range stats.ComponentStats {
    fmt.Printf(\"  %s: %d tables\n\", c.Name, c.TableCount)
}
```

### Deliverables

**1. New types in a new file ``stats.go`` at the root ``flecs`` package:**

```go
type Stats struct {
    EntityCount      int
    TableCount       int
    QueryCount       int    // uncached *Query active count — see note below
    CachedQueryCount int    // *CachedQuery count (excluding Closed ones)
    SystemCount      int
    FrameCount       uint64
    Time             float64 // total accumulated simulation time (seconds)
    LastFramePhases  []PhaseStats
    ComponentStats   []ComponentStat
}

type PhaseStats struct {
    Name        string         // \"PreUpdate\", \"OnFixedUpdate\", \"OnUpdate\", \"PostUpdate\"
    SystemCount int            // active systems in this phase
    Duration    time.Duration  // wall-clock for the last Progress call
}

type ComponentStat struct {
    ID          ID
    Name        string
    Size        uintptr
    TableCount  int // tables containing this component
    EntityCount int // sum of t.Count() across those tables
}
```

**2. New ``*World`` method:** ``func (w *World) Stats() Stats`` returns a fresh snapshot. Allocates one ``Stats`` value and its slices each call — designed for tooling, not hot tracing.

**3. World additions to track stats:**

- Add ``(*World).TableCount() int`` returning ``len(w.tables)`` (or fold into ``Stats`` only — implementer's call).
- ``w.cachedQueries`` already exists from Phase 6.1; expose count of non-closed entries (either a ``CachedQueryCount()`` method or via ``Stats`` only).
- **Skip uncached ``*Query`` counting.** Uncached queries are one-shot, value-style types; the world doesn't track them. Document this. (``QueryCount`` stays in the struct for future use; populate as 0 for now.)
- ``w.systems`` + Phase 7.1 compaction already give us ``SystemCount`` — fold into ``Stats``.

**4. Per-phase frame timing in ``(*World).Progress(dt float32)``:**

- For each phase, record ``time.Now()`` before and after the phase's dispatch, store the duration on the World.
- Maintain a fixed-size ``lastFramePhases [4]PhaseStats`` on the World, indexed by phase position. Names are constants (``\"PreUpdate\"``, ``\"OnFixedUpdate\"``, ``\"OnUpdate\"``, ``\"PostUpdate\"``).
- ``Stats()`` returns a copy of this slice.
- For phases with no systems: ``Duration == 0`` and ``SystemCount == 0``.
- Use ``time.Since`` for wall-clock; do NOT use ``dt`` (simulation time).
- ``OnFixedUpdate`` may run zero or more times in a single ``Progress``; SUM per-iteration durations into one entry.
- Document: timing requires ``Progress`` to have been called at least once; on a fresh world ``LastFramePhases`` is empty or all zeros.

**5. Per-component statistics in ``Stats()``:**

- Iterate ``w.Components()`` (or the registry's IDs); for each component ID, count via ``w.compIndex.TablesFor(id)`` / ``Count(id)`` for ``TableCount``. For ``EntityCount``, sum ``t.Count()`` for each matching table.
- Pair components with ``info.Size > 0`` (data pairs) AND with ``info.Size == 0`` (tag pairs auto-registered via ``EnsureID``) both appear in ``w.Components()``. Include both. Document.

**6. ``(*World).SystemCountInPhase(phase ID) int``** — convenience for tooling. Returns count of active systems in a specific phase. Panics if ``phase`` is not a built-in phase (mirrors ``NewSystemInPhase`` validation).

**7. Tests in ``stats_test.go``:**

- **Empty world:** ``Stats()`` returns ``EntityCount == (builtin count)``, ``TableCount >= 1``, ``QueryCount == 0``, ``CachedQueryCount == 0``, ``SystemCount == 0``, ``FrameCount == 0``, ``Time == 0``, ``LastFramePhases`` empty, ``ComponentStats`` includes the built-in ``Name`` component.
- **After adding entities + components:** ``Stats`` reflects updated counters.
- **After Progress:** ``FrameCount``, ``Time``, and ``LastFramePhases`` populated. ``Time`` matches the ``dt`` passed. Each phase has ``Duration > 0`` if it had systems; ``0`` otherwise.
- **Multi-frame timing accumulation:** ``Progress`` 5 times; ``FrameCount == 5``; ``Time`` accumulates ``dt``.
- **Per-phase timing:** register a slow system in ``OnUpdate`` that sleeps briefly; verify ``LastFramePhases`` for ``OnUpdate`` has ``Duration > 0``.
- **Per-component table count:** create entities with ``Position`` only; with ``Position+Velocity``; with ``Velocity`` only. ``ComponentStats[Position].TableCount == 2``, ``ComponentStats[Velocity].TableCount == 2``.
- **CachedQueryCount:** ``NewCachedQuery`` 3 times; ``CachedQueryCount == 3``. ``Close`` one; ``CachedQueryCount == 2``.
- **SystemCount and SystemCountInPhase:** add 2 systems to ``OnUpdate``, 1 to ``PreUpdate``; ``SystemCount == 3``; ``SystemCountInPhase(OnUpdate) == 2``; ``SystemCountInPhase(PreUpdate) == 1``; ``SystemCountInPhase(PostUpdate) == 0``.
- **SystemCountInPhase invalid panics:** pass ``ChildOf`` -> panic.
- **Stats is a snapshot:** call ``Stats()``, mutate world (add entity), call ``Stats()`` again — first snapshot is unchanged.
- **Existing tests stay green.**

**8. Documentation:**

- ``doc.go``: new \"Stats and Observability\" section with a code snippet.
- ``CHANGELOG.md``: \"Phase 9.3: Stats and per-phase timing\".
- ``README.md``: add a row in the feature table.

**9. Mechanical acceptance:**

- ``go test ./... -race -count=2`` passes.
- ``go vet ./...`` clean.
- ``golangci-lint run`` clean.
- Coverage on ``flecs`` >= 90% (no regression from 96.4%).
- All exported symbols have godoc.
- No new third-party deps (stdlib ``time`` only).
- Stats collection on ``Progress`` adds < 1us overhead per phase; a single ``time.Now()`` is ~30ns and ``Progress`` is already I/O-bound.

### Non-Goals

- NO time-series history (per-frame log of the last N frames).
- NO per-system timing — per-phase only. Per-system can come later.
- NO Prometheus / OpenMetrics export.
- NO memory profiling.
- NO allocation counters.
- NO query / observer / hook hit rates.
- NO per-query iteration counters.
- NO live-streaming or subscription model.
- NO concurrent-safe stats (single-threaded; matches existing World).
- NO benchmark of ``Stats()`` itself.

### C Reference

Read but do not paraphrase:

- ``/work/agents/claude/projects/SanderMertens/flecs/src/addons/stats/stats.c`` — analog. The C version is far more elaborate (histograms, per-system gauges); this Go phase ships a minimal subset for v0.

### Implementer Notes

- Two ``time.Now()`` calls per phase (4 phases + N fixed iterations) plus a sum. Negligible.
- ``lastFramePhases`` is a fixed-size ``[4]PhaseStats`` on the World struct, indexed by phase position. Phase names are constants.
- ``ComponentStats`` is freshly built per ``Stats()`` call; not retained on World.
- Implementation is mostly EXPOSURE of already-collected data through a unified snapshot. The only new instrumentation is per-phase wall-clock timing in ``Progress``.

## Constraints

- @world.go -- ``Progress`` lives here and is where per-phase ``time.Now()`` instrumentation goes; ``Count``, ``FrameCount``, ``Time``, ``SystemCount`` are already exposed and must NOT change signature. Add ``Stats()``, ``SystemCountInPhase()``, optional ``TableCount()`` / ``CachedQueryCount()`` here (or in ``stats.go``).
- @system.go -- per-phase ``Progress`` dispatch logic; phase iteration (including the ``OnFixedUpdate`` inner loop) is the seam where per-phase timing must wrap. Don't change ``Progress`` semantics beyond timing instrumentation.
- @cached_query.go -- ``w.cachedQueries`` slice and Close semantics; ``CachedQueryCount`` reads non-closed entries here.
- @meta.go -- ``Components`` and ``ComponentInfo`` are the source for ``ComponentStats``. Both data pairs (``Size > 0``) and tag pairs (``Size == 0``) appear here; include both.
- @internal/storage/componentindex/componentindex.go -- ``TablesFor`` / ``Count`` is how per-component ``TableCount`` is computed. Sum ``t.Count()`` over the returned tables for ``EntityCount``.
- @internal/component/registry.go -- registry holds component IDs / names / sizes; ``Components`` iterates this.
- @id.go -- ``ID`` type used in ``ComponentStat.ID`` and ``SystemCountInPhase(phase ID)``.
- @doc.go -- add a new \"Stats and Observability\" section with a runnable snippet.
- @CHANGELOG.md -- add a \"Phase 9.3: Stats and per-phase timing\" entry.
- @README.md -- add a row in the feature table for Stats / observability.
- DO NOT change the public signatures of ``World.Count``, ``World.Time``, ``World.FrameCount``, or ``World.SystemCount`` — fold their values into ``Stats``, don't replace them.
- DO NOT introduce a metrics-export goroutine or background thread.
- DO NOT import third-party dependencies — stdlib ``time`` only.
- DO NOT track per-system timing in this phase.
