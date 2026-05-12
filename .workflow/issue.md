## Goal

Port `docs/Systems.md` from the upstream flecs documentation, adapted to Go. This is the deep-dive on creating systems, pipeline phases, ordering, parallel and multi-threaded dispatch, fixed timestep, write sets, and system lifecycle.

Phases 14.0–14.5 shipped (v0.19.0–v0.24.0). This phase continues the docs port and targets **v0.25.0**. (Phase 14.5 was v0.24.0; do not bump the ROADMAP heading.)

**Upstream:** `/work/agents/claude/projects/SanderMertens/flecs/docs/Systems.md` (~6600 words, `port-adapted`, effort: large).

### Critical context — Go flecs has a lot of systems infrastructure

- Basic: `NewSystem`, `NewSystemInPhase`, `Progress`
- Phases: `PreUpdate`, `OnUpdate`, `PostUpdate`, `OnFixedUpdate` (4-phase pipeline)
- Fixed timestep: `SetFixedTimestep`, accumulator + spiral-of-death warning
- Parallel: `System.SetParallel(true)` + `World.SetWorkerCount(n)` from Phase 10.1
- Multi-threaded within-system: `System.SetMultiThreaded(true)` from Phase 10.4 (per-row-range splitting)
- Per-stage queues: Phase 12.1, lock-free worker deferred mutations
- Write-set tracking: `System.SetWriteSet`

### Likely gaps from this doc (capture as "Not yet ported in Go flecs" callouts)

- Custom phase entities (C lets you create your own phases beyond the four built-in)
- DependsOn between systems (C uses DependsOn pair to order systems)
- System tags / disabling
- Rate filters (run every N frames)
- Single-system `Run` (run one system out-of-pipeline)
- `RunWorker` / explicit thread dispatch
- `System.SetInterval` / `SetRate`
- Pipeline introspection (list systems in execution order)

### Deliverables

1. **Full port of `docs/Systems.md`** adapted to Go:
   - Show `NewSystem` + Each-callback first (the simple form).
   - Then `NewSystemInPhase` with the four built-in phases.
   - Then `SetFixedTimestep` for fixed-frequency systems.
   - Then concurrency: `SetParallel`, `SetMultiThreaded`, `SetWorkerCount`, `SetWriteSet`.
   - Cover the deferred-mutation semantics in parallel systems and within-stage queue merging (cross-link Phase 10.1, 10.4, 12.1).
   - For custom phases, DependsOn-style ordering, system tags, rate filters, etc.: use "Not yet ported in Go flecs" callouts.
   - Cover the `World.Stats()` per-phase timing visibility.

2. **Verify code blocks.** Create `docs/systems_examples_test.go` with `TestSystems_*` functions. Note: testing systems is more involved than testing data ops — you need a world with at least one system registered + a `Progress` call. Use small frame counts and `world.Read` inside the system callback to verify expected entity counts after the system runs.

3. **Update `docs/README.md`**: Systems row → `✅ landed / 14.6`. Append discovered gaps. Expect 4–7 (custom phases, DependsOn ordering, rate filters, system tags, system `Run`/`RunWorker`, system `SetInterval`/`SetRate`, pipeline introspection).

4. **Update `ROADMAP.md`**: 14.6 row → `✅ shipped (v0.25.0)`. **Do NOT bump the heading.**

5. **Update `CHANGELOG.md`** with `Unreleased — Phase 14.6: Systems doc port (upcoming v0.25.0)`.

6. **Cross-link** with Queries (systems run queries), Quickstart (systems section), and Observers (the next phase).

### Style notes

- Native Go.
- Lead with the simplest example — a one-system world that runs a query each `Progress`.
- Build up incrementally: phase ordering → fixed timestep → parallel → multi-threaded.
- For concurrency, lean on Phase 10.x and 12.1 CHANGELOG entries for accurate framing.

### Non-goals

- No source changes.
- No porting beyond Systems.

### Mechanical acceptance

- `go test ./docs/...` passes.
- `go vet`, `golangci-lint` clean.
- `go test ./... -race -count=1` clean.
- `docs/README.md` shows Systems as landed.

## Constraints

- @docs/Systems.md — current stub to be replaced with the full port
- @docs/Quickstart.md — tone reference; cross-link the systems section
- @docs/Queries.md — tone reference; cross-link (systems run queries)
- @docs/README.md — flip Systems row to landed and append discovered gaps
- @docs/prefabs_examples_test.go — recent test pattern to mirror in `systems_examples_test.go`
- @system.go — `NewSystem`, `NewSystemInPhase`, `SetParallel`, `SetMultiThreaded`, `SetWriteSet` (also hosts pipeline logic; no separate `pipeline.go`)
- @world.go — built-in phase IDs (`PreUpdate`/`OnUpdate`/`PostUpdate`/`OnFixedUpdate`), `Progress`, `SetFixedTimestep`, `SetWorkerCount` (no separate `timer.go`)
- @stats.go — per-phase timing surface for `World.Stats()`
- @doc.go — package-level overview; check for systems narrative to cross-link
- @README.md — top-level project doc; ensure consistency
- @ROADMAP.md — flip 14.6 row to shipped (v0.25.0); do not bump the heading
- @CHANGELOG.md — add `Unreleased — Phase 14.6: Systems doc port (upcoming v0.25.0)`
- Governed by the CONTRIBUTING.md "Documentation" policy: code blocks compile against v0.24.0; use Go idioms.
- Upstream source: `/work/agents/claude/projects/SanderMertens/flecs/docs/Systems.md`.
