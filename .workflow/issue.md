## Goal

Implement opt-in parallel system dispatch within a phase. Systems flagged as parallel-safe run in goroutines from a persistent worker pool; systems within the same phase that share component-write access are forced serial. ECS storage remains non-goroutine-safe — the world enforces safety conservatively via per-system write-set conflict detection.

### API after this phase lands

```go
w := flecs.New()
w.SetWorkerCount(4) // 0 (default) = serial dispatch (existing behavior)

moveQuery := flecs.NewCachedQuery(w, posID, velID)

// Mark this system as parallel-safe. Defaults to serial (false).
sys := flecs.NewSystem(w, moveQuery, func(dt float32, it *flecs.QueryIter) {
    // ... iteration code ...
})
sys.SetParallel(true)

// Optional: override the inferred write set (default = all query term ids).
// Empty slice = read-only system, never conflicts.
sys.SetWriteSet([]flecs.ID{posID})

// Multiple parallel-safe systems in the same phase whose write sets are
// pairwise disjoint run concurrently. Systems whose write sets intersect
// run serially in registration order.
w.Progress(dt)
```

### Deliverables

1. `(*System).SetParallel(bool)` — flag, default false.
2. `(*System).Parallel() bool` — accessor.
3. `(*System).SetWriteSet(ids []ID)` — declare written components. Default: derived from the system's query terms (all And/Or/Optional ids). Empty slice = read-only (no conflicts).
4. `(*World).SetWorkerCount(n int)` — pool size. 0 (default) = serial; n > 0 = persistent goroutine pool; negative panics. Calling during Progress: panic OR no-op (implementer's call, must be documented). Changing N between Progress calls allowed: tear down old pool, start new pool.
5. `(*World).WorkerCount() int` — accessor.
6. Internal worker pool:
   - Persistent goroutines created in SetWorkerCount(n).
   - Buffered `chan func()`, size `2*workerCount`.
   - Workers exit via channel close on pool shutdown.
   - No public goroutine pool API beyond SetWorkerCount.
7. Phase dispatch refactor in Progress:
   - For each phase, partition the system list into BATCHES.
   - A batch is a maximal contiguous run of parallel-safe systems whose write sets are pairwise disjoint.
   - Within a batch: dispatch each system as a job; await all via sync.WaitGroup before proceeding.
   - Serial systems form single-system batches.
   - Conflict detection: precompute write-set as `map[ID]struct{}` for O(1) overlap check; track the running batch's union write-set.
   - Document the over-approximation: even read-only access by one system on a component another writes counts as conflict because reads are not tracked.
8. Defer queue + parallel systems:
   - Add a `sync.Mutex` on `*World` protecting deferDepth and the deferred slice.
   - Acquire/release around DeferBegin/End/Defer (public) AND internal queue-append paths.
   - The mutex must NOT be held during system fn invocation.
   - Set from a parallel system goes through Defer (mutex-protected) — must not panic.
   - Per-goroutine defer queues with merge: documented as future work, NOT implemented.
9. Tests in `parallel_test.go` (new file):
   - SetParallel default false.
   - SetWorkerCount default 0 (existing tests stay green).
   - Negative SetWorkerCount panics.
   - Parallel systems run on pool: 2 systems, disjoint write sets, parallel=true; verify both ran and overlapped via timing (total < sum).
   - Conflicting parallel systems serialize: 2 systems with overlapping write sets; total time ≈ sum.
   - Mixed batch [serial, parallel-A, parallel-B-disjoint]: serial first, then A and B concurrent.
   - WriteSet override: same query, disjoint explicit write sets → run in parallel.
   - Read-only WriteSet: WriteSet([]) on both → run in parallel.
   - Defer-during-parallel: parallel system calls flecs.Set; passes -race; mutations apply after phase.
   - SetWorkerCount mid-frame: matches documented behavior (panic or no-op).
   - Worker pool shutdown: SetWorkerCount(4) then SetWorkerCount(0); goroutines exit (verify with runtime.NumGoroutine() plus small sleep).
   - `-race -count=10` on parallel scenario passes consistently.
10. Benchmarks in `bench_test.go`:
    - `BenchmarkProgress_ParallelDispatch_2systems_10k` (WorkerCount=2).
    - `BenchmarkProgress_SerialBaseline_2systems_10k` (WorkerCount=0).
    - Speedup ratio recorded in BENCH.md.
11. Documentation:
    - Godoc on SetParallel, Parallel, SetWriteSet, SetWorkerCount, WorkerCount covering semantics and risks.
    - doc.go: new \"Parallel Execution\" section with snippet, conflict-detection note, and the rule: \"Storage is NOT goroutine-safe; the world prevents parallel writes by enforcing disjoint write sets.\"
    - CHANGELOG entry under Unreleased.
    - README feature table updated.
    - Document: parallel systems must not call Field on each other's queries (each system owns its QueryIter).
12. Mechanical acceptance:
    - `go test ./... -race -count=2` passes.
    - `go vet ./...` clean.
    - `golangci-lint run` clean.
    - Coverage on flecs ≥ 90% (no regression from 95.6%).
    - All exported symbols have godoc.

### Non-goals (do NOT implement)

- Per-archetype-table parallelism (one system across multiple tables).
- Lock-free defer queue (single mutex is the v0 trade-off).
- Cross-frame async scheduling.
- Job dependency graph.
- Read-only system optimization beyond explicit WriteSet([]).
- Goroutine pool tuning beyond size.
- context.Context propagation to system fn.
- Custom worker placement / pinning.
- Third-party deps.
- Any change to the WorkerCount=0 single-threaded baseline behavior.
- Changes to Set/Get/Has/Field goroutine-safety semantics.

### C reference (cite, do not paraphrase)

- `/work/agents/claude/projects/SanderMertens/flecs/src/addons/pipeline/pipeline.c` — search for thread/worker setup.
- `/work/agents/claude/projects/SanderMertens/flecs/src/stage.c` — per-thread stages (we do NOT port stages; goroutines + serial flush is our model).

## Constraints

- @world.go — World holds the new worker pool, mutex, and SetWorkerCount/WorkerCount API; Progress refactor for batch dispatch lives here.
- @system.go — System gains SetParallel/Parallel/SetWriteSet and stores its precomputed write set.
- @defer.go — Add sync.Mutex protecting deferDepth and deferred slice around DeferBegin/End/Defer and internal queue-append paths; mutex must not be held during system fn invocation.
- @cached_query.go — Source of default write-set ids (And/Or/Optional terms) when SetWriteSet is not called.
- @query.go — Query term inspection for write-set inference; document Field/QueryIter ownership rule for parallel systems.
- @id.go — ID type used for write-set maps (`map[ID]struct{}` for O(1) overlap check).
- @doc.go — New \"Parallel Execution\" section: snippet, conflict-detection note, storage-not-goroutine-safe rule.
- @CHANGELOG.md — Unreleased entry for Phase 10.1.
- @README.md — Feature table updated to reflect parallel dispatch.
- @BENCH.md — Record parallel vs serial speedup ratio from new benchmarks.
- @bench_test.go — Extend with BenchmarkProgress_ParallelDispatch_2systems_10k and BenchmarkProgress_SerialBaseline_2systems_10k.
- No third-party deps; stdlib only (sync.WaitGroup, sync.Mutex, chan func()).
- Buffered channel size: 2*workerCount.
- Conflict detection: precompute per-system write-set as map[ID]struct{} for O(1) overlap.
- Defer mutex acquired only around queue mutation, never around system fn.
- WorkerCount=0 default keeps existing single-threaded behavior bit-for-bit.
