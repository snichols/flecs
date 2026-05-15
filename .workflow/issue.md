## Goal

Extend Phase 12.1's per-stage queue architecture from **within-system parallelism** (multi-threaded systems) to **between-system parallelism** (parallel-batched systems via `SetParallel(true)`), eliminating the last remaining concurrency contention point on the deferred-mutation path.

This is the last item under ROADMAP `Future Work / Concurrency`. After this phase ships, every item on the ROADMAP Future Work list is closed — Phase 16.50 is the **final port-completion milestone**.

### Verified current state

A direct read of the codebase shows the situation is slightly different from the original brief's framing — capturing the verified picture here so the iterate agent does not waste cycles hunting a mutex that does not exist on this path:

- **Phase 12.1 (shipped):** multi-threaded systems get per-stage queues. The dispatcher pre-allocates `stages[1..N]` and per-worker `Writer` values in `World.SetWorkerCount` (`@world.go` lines 1037–1082). The within-system path in `runPhase` (`@system.go` lines 468–515) binds `workerIt.workerWriter = &w.workerStageWriters[wi]` before dispatch, so each worker's deferred writes hit its own `stages[wi+1].queue` — zero synchronization on the hot path. After `wg.Wait()`, stages 1..N are merged in ascending id order, then stage 0; `firePreMergeHooks` / `firePostMergeHooks` fire once per merge boundary.
- **Parallel-batched systems (`SetParallel(true)`):** the dispatcher (`@system.go` lines 526–577) collects a disjoint-write-set batch and fans the batch out across worker goroutines through `workerCh`. Critically, it **never sets `workerWriter`** on the per-system `QueryIter`. Per `@query.go` lines 2519–2528, when `workerWriter` is nil the iter falls back to `&it.world.writeCapability` — which is bound to `stages[0]` (`@world.go` lines 850–853). All batched parallel systems therefore append into the **same** `stages[0].queue` concurrently.
- **What protects that today?** Two things, neither of which is a mutex on the deferred queue itself:
  1. The dispatcher's disjoint-write-set check (`@system.go` lines 530–550) prunes the batch so two batched systems never touch the same component — so the *user-visible* invariants hold.
  2. `World.mu` (`@world.go` line 41, a `sync.RWMutex`) is held for the duration of `World.Write` / `Progress`, but that is one outer Lock; it does not serialize the inner worker goroutines against each other.
- **The actual problem:** `stages[0].queue.append` (`@cmd_queue.go` lines 48–70) mutates a shared `cmds []cmd` slice and `entries map[ID]cmdEntry` from multiple goroutines without synchronization. Even with disjoint *component* sets, the underlying slice growth and map writes race. The ROADMAP wording ("Defer queue is mutex-protected so deferred mutations from parallel systems are race-free") is aspirational rather than literal — there is no `sync.Mutex` around `cmdQueue` operations. `go test -race` may or may not surface it depending on workload coverage; the iterate agent should run `-race -count=10` and confirm.

### What to build

1. **Bind parallel-batched systems to per-worker stages.** When the parallel-batch dispatcher (`@system.go` lines 562–576) enqueues a system onto `workerCh`, capture the worker index W in the closure and set `it.workerWriter = &w.workerStageWriters[W]` before invoking `bs.fn(phaseDT, it)`. Mechanism mirrors the multi-threaded path at `@system.go` line 481. Each parallel system on a separate goroutine writes into a distinct `stages[1..N].queue` — no shared mutable state, no synchronization.
2. **Reuse the existing merge protocol.** After `wg.Wait()` for the parallel batch, merge stages 1..N in ascending id order, then stage 0. The merge code at `@system.go` lines 498–512 (currently only the multi-threaded path) is the template — factor it into a helper so both paths call it. `firePreMergeHooks` / `firePostMergeHooks` fire **once per batch merge boundary**, not once per worker.
3. **Worker index discovery.** Worker goroutines today are anonymous consumers of `workerCh` (`@world.go` lines 1072–1080). The dispatcher must communicate \"this job runs on stage W\" — either by attaching the stage to the job (preferred: closure captures `wIdx` and the dispatcher round-robins indices `wIdx := batchPos % w.workerCount`), or by making workers self-identify (each goroutine knows its index, the job uses `runtime.GOID` lookup — heavier). Recommend the former: the dispatcher hands out stage indices to the batch in order, modulo N. This deterministically pins system k of a batch to stage (k mod N)+1.
4. **Batch-larger-than-worker-count semantics.** If the parallel batch contains more systems than workers, multiple systems share a stage by serial reuse of that stage's queue — fine, because they execute serially on that worker goroutine, so no concurrent access. The merge step still flushes each stage once at the end.
5. **Multi-threaded ∩ parallel.** A system flagged both `SetMultiThreaded(true)` and `SetParallel(true)` currently follows the multi-threaded path (it short-circuits at `@system.go` line 468 before the parallel batch logic). That behavior is preserved; no nesting of stage allocations.
6. **`RunSystemWorker` mutex retained.** `@system.go` lines 723–729 hold `w.mu.Lock()` to serialize concurrent `RunSystemWorker` calls flushing per-call stages. That path is the out-of-pipeline user API and uses fresh ad-hoc stages (id = -1), not the pool. Leaving it untouched is correct; document why.
7. **No new lock-free data structures.** The fix is pure allocation routing — give each goroutine its own already-allocated stage. No Michael-Scott queue, no hazard pointers, no atomics on the hot path.

### Verification matrix

Correctness:
- `TestParallelSystems_DeferredMutationsCorrect` — N parallel systems each enqueue distinct mutations; verify all applied after `Progress`.
- `TestParallelSystems_OrderingPreserved` — within a system, FIFO submission order; across systems, stage-id order (deterministic).
- `TestParallelSystems_NoDataRace` — `go test ./... -race -count=10` clean. Iterate agent runs with `-count=10` (not the usual `-count=3`) because the bug being closed is a latent race in `stages[0].queue.append`.
- `TestParallelSystems_CoalescingPreserved` — within-stage per-entity FIFO coalescing still folds 100 AddIDs into one migration (Phase 11.0 semantics).
- `TestParallelSystems_BatchLargerThanWorkerCount` — N systems, fewer than N workers; verify correct stage reuse and final flush.

Hook integration:
- `TestParallelSystems_OnPreMergeFiresOnce` / `TestParallelSystems_OnPostMergeFiresOnce` — single fire per batch merge boundary; verify by counter in handler.

Pipeline interaction:
- `TestParallelSystems_PhaseBoundaries` — parallel systems in different phases dispatch correctly; merge happens at phase boundary.
- `TestParallelSystems_WithSerialSystem` — mix parallel and serial in one phase; serial sees merged state from prior batch.

User-facing API:
- `TestParallelSystems_UserWriteFromOutsideSystem` — `world.Write(fn)` from main goroutine works during/around Progress.
- `TestParallelSystems_NestedScopesBlocked` — existing scope guards still reject re-entry from inside a parallel system.

Integration with other subsystems:
- `TestParallelSystems_SnapshotRestore` — `TakeSnapshot` / `RestoreSnapshot` between Progress calls; clean state.
- `TestParallelSystems_DuringReclamation` — Phase 16.46 table reclamation active during parallel dispatch; no use-after-free, no missed `EventOnTableDelete`.

Performance:
- `BenchmarkParallelSystems_PerStage` — measure baseline (current shared-stage path) vs per-stage; expect substantial improvement under contention (slice growth + map writes are the hot spot today). Brief's "≥ 2x on 4-worker workload" is the target; iterate agent reports actual numbers in `BENCH.md`.
- `BenchmarkParallelSystems_Scaling` — 1, 2, 4, 8 workers; verify near-linear throughput scaling.

### Acceptance

- `go vet ./...` clean.
- `golangci-lint run ./...` clean.
- `go test ./... -race -count=10` clean.
- Coverage ≥ 95.0% (current baseline from v0.39.0; verify against current `cov.out` at merge time).
- All Phase 12.1 multi-threaded system tests pass unchanged.
- All existing parallel system tests in `@system_parallel_test.go` and `@parallel_test.go` pass unchanged.
- `RunSystemWorker` mutex retained and its rationale documented in the function's doc comment.
- Benchmarks demonstrate measurable improvement; numbers recorded in `@BENCH.md`.

### Documentation updates

- `@docs/Systems.md` — \"Parallel system dispatch\" section: replace the mutex narrative with per-stage routing; document scaling characteristics; cross-reference the multi-threaded path.
- `@ROADMAP.md`:
  - Remove \"Lock-free defer queue\" from `Future Work / Concurrency`.
  - Add a `v0.105.0 (Phase 16.50)` Shipped entry describing the per-stage extension.
  - Update the closing line of the Shipped section to assert **every ROADMAP item shipped**; Phase 16.50 is the final port-completion milestone.
- `@CHANGELOG.md` — `v0.105.0 — 2026-05-15 — Phase 16.50: Per-stage queues for parallel system dispatch` entry.
- `@README.md` — concurrency feature row update.
- `@BENCH.md` — before/after benchmark numbers; brief commentary on the contention removed.

### Non-goals

- Lock-free / wait-free `cmdQueue` data structure (Michael-Scott, hazard pointers, epoch-based reclamation) — per-stage allocation makes them unnecessary.
- Removing `world.mu` from the user-facing `world.Write(fn)` outside-of-system path — that path is single-goroutine; `RWMutex` is fine.
- Removing the `w.mu.Lock()` in `RunSystemWorker` (`@system.go` line 725) — that serializes ad-hoc concurrent flushes from user code outside the pipeline; out of scope.
- Distributed / cross-process synchronization.
- Changes to the disjoint-write-set batching logic at `@system.go` lines 526–550 — the routing fix is orthogonal to which systems batch together.

## Constraints

- @cmd_queue.go — defines `cmdQueue` and its `append` / `flush` operations; the file is **not** mutex-protected today, contrary to the original brief's framing. Per-stage routing removes the need for protection here.
- @system.go — parallel-batch dispatcher (lines 526–577) is the primary change site; multi-threaded path (lines 468–515) is the template to mirror; `RunSystemWorker` (lines 692–730) retains its `w.mu.Lock()`.
- @world.go — `SetWorkerCount` (lines 1037–1082) already allocates `stages[1..N]` and `workerStageWriters[0..N-1]`; reuse without modification. `writeCapability` (lines 42, 852–853) is the stage-0 binding that parallel systems currently fall through to.
- @stage.go — `stage` struct contract: \"single-goroutine ownership is the invariant\" comment (lines 4–8) already covers parallel systems once routing is fixed; consider extending the doc comment to call out the parallel-batch case explicitly.
- @query.go — `QueryIter.Writer()` (lines 2519–2528) is the routing decision point; the dispatcher must set `workerWriter` before calling the system function so this returns the per-stage writer.
- @merge_hooks.go — `firePreMergeHooks` / `firePostMergeHooks` semantics; the parallel batch merge must fire these once per batch boundary (matches Phase 16.23 contract for the multi-threaded path).
- @parallel_test.go and @system_parallel_test.go — existing parallel-system tests; all must pass unchanged. New `TestParallelSystems_*` cases above go in `@system_parallel_test.go`.
- @bench_test.go — append `BenchmarkParallelSystems_PerStage` / `BenchmarkParallelSystems_Scaling`; baseline numbers must be recorded in `@BENCH.md`.
- @ROADMAP.md — `Lock-free defer queue` line at 157 is the entry that gets removed; Shipped list (lines 26–107) is the destination for the new v0.105.0 entry; line 109 (\"All major upstream gaps closed\") gets upgraded to assert ROADMAP-complete.
- @CHANGELOG.md — version sequence is strictly monotonic (v0.104.0 is current head); new entry must be v0.105.0 and dated 2026-05-15.
- @docs/Systems.md — \"Parallel system dispatch\" section is the documentation surface that needs the per-stage narrative.
- Target version: **v0.105.0**. Phase number: **16.50**. Label: `snichols/queued`.
