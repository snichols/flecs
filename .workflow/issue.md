## Goal

Port C flecs' `multi_threaded` system flag to our Go port: when a system is flagged multi-threaded and `WorkerCount > 0`, all workers run the SAME system concurrently, each on a disjoint row slice of every matched table (`count / N` rows per worker per table). Even a single big table parallelizes within one system.

This is distinct from existing `SetParallel(true)` (Phase 10.1, v0.10.0):

- `SetParallel(true)` — "this system can run in parallel with OTHER systems in the same phase, given disjoint write-sets." **Across-systems** parallelism.
- `SetMultiThreaded(true)` (new) — "split this ONE system's iter across all workers." **Within-system** parallelism.

In C the two concepts compose; in our port the simpler design is: a multi-threaded system commandeers ALL workers, so it cannot be batched with parallel siblings. This is documented and tested.

### Reference (C flecs)

- Flag declared: `include/flecs/addons/system.h:96`
- Iter splitter: `src/iter.c:956-996` (remainder handling at lines 970-993)
- Pipeline dispatcher: `src/addons/pipeline/pipeline.c:644-667`
- System entry: `src/addons/system/system.c:100-103`

### Design constraint (state prominently)

Our defer queue is currently a single mutex-protected `[]func(*World)` (Phase 10.1 added the mutex). When N workers all run the same system in parallel and the user's fn calls `w.Set` / `w.Delete` / `w.AddID` inside the iter loop, all N goroutines contend on `deferMu`. **Per-row-range parallelism therefore scales linearly ONLY for in-place updates** (the iter loop reads via `Field[T]` and writes via mutation of the slice elements, no deferred calls). For deferred mutations, expect sub-linear scaling.

The full fix is Phase 11.0 (per-stage command queues, tracked as task #40). This issue intentionally lands the parallelism first without waiting for the queue redesign, because in-place updates are the common ECS pattern.

## Deliverables

1. **`System.SetMultiThreaded(bool)`** and **`System.MultiThreaded() bool`** in `system.go`. Default `false`. When `true` AND `WorkerCount > 0`, the system is dispatched as a multi-threaded batch.

2. **Dispatcher in `runPhase`** (`system.go` around line 270-307 today). New logic:
   - When iterating systems in a phase, check `s.MultiThreaded()` first.
   - If multi-threaded: cannot batch with siblings. Close any in-flight `parallel` batch by waiting on `wg`. Then dispatch this system as N worker jobs, each with a clipped iter; wait for all N. Then continue.
   - If not multi-threaded but parallel: existing logic (batch with siblings whose write sets are disjoint).
   - Otherwise: existing serial path.

3. **Iter clipping.** The current `*QueryIter` does not expose row-range slicing. Add an internal method or constructor on `QueryIter` so the dispatcher can produce N independent iters, each clipped:
   - For worker `i` of `N` against a table of `count` rows:
     - `first = (count / N) * i + min(i, count % N)`
     - `worker_count = count / N + (1 if i < count % N else 0)`
     - Matches C splitter at `src/iter.c:970-993` (first `count % N` workers get one extra row).
   - Worker skips a table if its `worker_count == 0`.
   - Inside an iter step, `Field[T]`'s base pointer is offset by `first` and length is `worker_count`; user code sees a contiguous slice of "my rows."
   - Adjust the `unsafe.Slice` construction in `query.go`'s `Field[T]` — currently the slice spans `[0:tableCount)`; for a clipped iter it must span `[first:first+workerCount)`.

4. **`it.Entities()` and field accessors must also be clipped** — returns entity IDs for THIS worker's rows, not the whole table.

5. **User-facing semantics** — clear doc comment:

   > A multi-threaded system runs across all workers configured by `World.SetWorkerCount`. Each worker receives a disjoint slice of every matched table's row range. The user's fn may read and write component slices in place without synchronization (workers' slices never overlap). Calls to `World.Set`, `Delete`, `AddID` etc. from inside the iter loop are safe but contend on the world's defer queue — for in-place updates, prefer mutating `Field[T]` slices directly to maximize scaling.

6. **Tests** in `system_parallel_test.go` (extend existing; create if absent):
   - `TestMultiThreadedSystemProcessesEachEntityOnce` — 100k entities with a `Counter int` component; multi-threaded system increments each; verify sum == 100k. Run with `WorkerCount` in `{1, 2, 4, 8}`.
   - `TestMultiThreadedSystemCannotBatchWithSiblings` — a multi-threaded system and a regular parallel sibling — verify they run serially (timing or a sync flag).
   - `TestMultiThreadedSystemUnevenSplit` — 1000 rows, `WorkerCount=3` — verify worker 0 gets 334, workers 1-2 get 333 each (total 1000).
   - `TestMultiThreadedSystemEmptyWorkers` — 2 rows, `WorkerCount=4` — verify workers 0-1 each get 1 row, workers 2-3 skip.
   - `TestMultiThreadedSystemWithDeferredMutations` — workers call `w.Delete` from inside iter; verify all deletes apply correctly post-phase (uses the existing single mutex-protected queue — slow but correct).
   - All under `-race -count=10`.

7. **Benchmark** in `bench_test.go`: `BenchmarkMultiThreadedSystem` — 100k entities, in-place Vec3 update (Add). Compare `WorkerCount=1` vs `2` vs `4`. Expect near-linear speedup up to physical core count for the in-place case.

8. **Docs**: `doc.go`, `CHANGELOG.md` (Unreleased v0.13.0), `README.md` — document `SetMultiThreaded`, the in-place-vs-deferred caveat, and the relationship to Phase 11.0 (defer queue redesign as the path to deferred-mutation scaling).

9. **`ROADMAP.md`** — when promoting to Shipped (v0.13), add the entry under the v0.13 shipped list.

## Acceptance

- `go test ./... -race -count=10` clean.
- `go vet ./...` and `golangci-lint run` clean.
- Coverage ≥ 95%.
- `BenchmarkMultiThreadedSystem` shows ≥ 1.7× speedup at `WorkerCount=2` vs `WorkerCount=1` on a 100k-entity in-place update workload (so per-worker overhead is bounded).
- No public API regression. New surface: `SetMultiThreaded`, `MultiThreaded`.
- `BenchmarkSetExistingComponent` (single-threaded baseline) regression ≤ 1% — the new code path only kicks in when `SetMultiThreaded` is true.

## Non-deliverables

- **Per-stage command queues.** Defer queue stays single-mutex; deferred mutations from multi-threaded systems serialize. Phase 11.0 (task #40) lifts this.
- **C's `immediate` flag** (`include/flecs/addons/system.h:99`) — combine with `multi_threaded` only if the system needs direct world access; not in scope.
- **Granularity threshold** — C has none either; empty workers just skip.

## Constraints

- @system.go — System struct, `runPhase`, `SetWorkerCount`; add `SetMultiThreaded`/`MultiThreaded` methods and the new dispatcher branch around lines 270-307.
- @query.go — `Iter`, `Field[T]`, `QueryIter`; add clipped-iter mechanism; adjust `unsafe.Slice` bounds in `Field[T]` so a clipped iter spans `[first:first+workerCount)`.
- @cached_query.go — same iter clipping mechanism applies.
- @each.go — `Each1`-`Each4` internally use `Iter`; not directly affected (Each is not the system path), but verify no regression.
- @world.go — `workerCh`, `WorkerCount`; no field changes needed.
- @system_parallel_test.go — extend with multi-threaded tests; create if absent.
- @bench_test.go — add `BenchmarkMultiThreadedSystem`.
- @doc.go — add multi-threaded section.
- @CHANGELOG.md — Unreleased v0.13.0 entry.
- @README.md — brief mention of `SetMultiThreaded` and the in-place-vs-deferred caveat.
- @ROADMAP.md — on release of v0.13, move "per-table parallelism within a single system" from Future Work to Shipped.
- Phase 10.1 (`SetParallel`, v0.10.0) establishes the across-systems parallelism baseline and the defer-queue mutex; this issue layers within-system parallelism on top without changing that queue.
- Phase 11.0 / task #40 (per-stage command queues) is the follow-up that unlocks linear scaling for deferred mutations; this issue calls that out but does not depend on it.

