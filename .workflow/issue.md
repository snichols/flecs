## Goal

v0.15.0 shipped the `Reader`/`Writer` scoped capability API and removed the `World.W/R/NewEntity` escape-hatches. Internally, however, every `Writer` still routes its mutations through a single mutex-protected `cmdQueue` on the `World`. When `SetMultiThreaded(true)` is enabled and `SetWorkerCount(N)` spawns N workers iterating disjoint row slices of one system, every `flecs.Set` / `Delete` / `AddID` call from every worker contends on the same mutex — deferred mutations from multi-threaded systems do not scale beyond one core.

Phase 12.1 ports C flecs's per-stage command-queue architecture (`~/projects/SanderMertens/flecs/src/stage.c`, `~/projects/SanderMertens/flecs/src/stage.h`, the readonly/merge flow in `~/projects/SanderMertens/flecs/src/world.c`, and the flush logic in `~/projects/SanderMertens/flecs/src/commands.c`). The key change versus the v0.15.0 internal layout: each worker goroutine writes into its own stage's queue with no synchronization on the hot path; stages merge into the world serially at the end of the scope. v0.15.0 already plumbed `*Writer` through every mutation API, so the `Writer` is the natural carrier for per-goroutine stage routing.

This phase explicitly rejects the goid-into-a-map design previously sketched for the cancelled #92. Threading wins because it is lock-free on the dispatch path, deterministic, and matches C's explicit stage-id model (`ecs_get_stage(world, i)` returns a stage by index, and the dispatcher hands stage `i` to worker `i`).

### What C actually does (research summary)

Read the files; do not paraphrase them here. The high-level shape that the Go port should mirror:

- **`ecs_stage_t`** (`stage.h`) owns its own command queue, its own bump/stack allocator, and per-stage thread context. It also owns a `cmd_stack[2]` double-buffer so that one queue can be flushed while a hook fires and enqueues into the other.
- **Stage `id`** is an `int32_t`. `id == 0` is the main stage attached to the world; `id == 1..N` are worker stages; `id == -1` is a user-owned unmanaged stage (we will NOT port this — see Notes).
- **`ecs_set_stage_count(world, n)`** (`stage.c`) lazily allocates / grows / shrinks the `world->stages` array and assigns each new stage its `id`.
- **`ecs_get_stage_count` / `ecs_get_stage`** expose the table. The dispatcher in C's pipeline addon passes `ecs_get_stage(world, worker_index)` to each worker.
- **`flecs_stage_from_world`** (`world.c`) is the polymorphic unwrap: given an `ecs_world_t*` that might actually be an `ecs_stage_t*`, return the stage. In Go we replace this with explicit threading of `*stage` on the `Writer`.
- **`ecs_readonly_begin`** (`stage.c`) calls `flecs_defer_begin` on every stage 0..N-1 and sets the world's readonly + multi-threaded flags. From this point only stages may enqueue commands.
- **`ecs_readonly_end`** clears the flags and calls `flecs_stage_merge`, which iterates every stage and calls `flecs_defer_end` on each — serial merge in stage-id order.
- **`flecs_defer_end`** (`commands.c`) atomically swaps `stage->cmd` to point at the alternate `cmd_stack` slot, then iterates the captured queue. For each entity, the FIRST command (negative `next_for_entity`) triggers `flecs_cmd_batch_for_entity` which coalesces all commands for that entity into one archetype migration. This is the per-entity coalescer we already implement in @cmd_queue.go. The swap-then-flush dance is the mechanism that lets hooks invoked during flush re-enqueue safely.

### What the Go port looks like

This is a draft; refine against the C reading. The shape is:

1. **`stage` internal type** in `stage.go` (NEW file). Owns a `*cmdQueue` (already exists in @cmd_queue.go and continues to host the per-entity coalescer verbatim). Single-goroutine ownership invariant; no mutex on the stage's hot path. Likely fields:
   - `id int` — `0` for the main stage, `1..N` for workers
   - `queue *cmdQueue` — the existing coalescing queue
   - `world *World` — back-pointer for merge calls
   - `deferDepth int` — moved off `World` (today at @world.go:62); now per-stage, mirroring C's `stage->defer`

2. **`World.stages []*stage` table** plus internal allocation hooked into `SetWorkerCount`. C's `ecs_set_stage_count` is implicit in our pipeline — when `SetWorkerCount(n)` is called (or the first multi-threaded system runs), allocate `n+1` stages: index 0 is the main stage used by the calling goroutine; indices `1..n` are owned by workers. Reuse the existing `World.stages` slot for the lifetime of the worker pool. Replace today's `World.deferred` / `deferMu` / `deferDepth` fields (@world.go:60-62) with the stage table.

3. **`*Writer` carries a `*stage` field.** Today `it.Writer()` returns `&it.world.writeCapability` — a single shared Writer cached on the world (see @query.go:377 and @world.go:41). After Phase 12.1, the worker dispatcher in @system.go constructs (or reuses) per-worker Writers, each bound to `world.stages[workerIdx+1]`. The serial path constructs a Writer bound to `world.stages[0]`. Every mutation method on `*Writer` (Set, SetByID, Delete, AddID, RemoveID, SetPairByID — see @scope.go ~lines 227-335) routes through `fw.stage.queue` instead of `fw.world.deferred`. The `*Writer` API surface stays bit-for-bit identical to v0.15.0; only the routing changes.

4. **End-of-scope merge.** When the outermost `Write` returns or the multi-threaded phase's `wg.Wait()` completes, the main goroutine walks `world.stages` in id order and merges each stage's queue into the main store. Merging reuses today's `flushDeferQueue` codepath (the two-pass coalescer in @cmd_queue.go) per-stage. **Cross-stage merge policy:** stages are merged in ascending id order; within a stage the existing per-entity FIFO coalescer applies; across stages there is no coalescing — two stages mutating the same entity produce two archetype migrations. This matches C's `flecs_stage_merge` loop. Document this in @doc.go.

5. **Multi-threaded dispatch wiring** in @system.go around the `s.multiThreaded` branch (lines ~287-306). Before pushing each worker closure to `w.workerCh`, bind a Writer to the per-worker stage. Hook callbacks fired from inside a worker receive that same Writer (the routing carries through because hooks already take the Writer that the caller passed). After `wg.Wait()`, the main goroutine merges stages `1..N` in order, then merges stage 0. This is the only place worker stages flush; workers never call merge.

6. **Single-threaded path unchanged.** `WorkerCount == 0` allocates exactly one stage (stages[0]) lazily. `world.Write(fn)` increments `stages[0].deferDepth`, runs `fn` with a Writer bound to stages[0], decrements and flushes on outermost-scope exit. Performance must be identical to v0.15.0: 0 allocs/op, the same ns/op for `BenchmarkDeferSingleSet` / `BenchmarkDeferBatchedAdds`, no new lock acquisitions, no atomic loads on the hot path.

7. **Benchmark** in @bench_test.go: new `BenchmarkMultiThreadedDeferredSet` that runs a multi-threaded system where each row's callback issues `fw.Set(e, ...)`. Sweep workers in {1, 2, 4} (mirroring the existing `BenchmarkMultiThreadedSystem` shape at line 1170). Acceptance: at least 2x speedup on 4 workers versus 1 worker for the deferred-mutation path. Modest target — a lock-free queue is a separate future phase.

8. **Tests** in `stage_test.go` (NEW) or extending @scope_test.go:
   - Per-worker stage isolation: a worker's mutations do not appear in another worker's queue before merge.
   - End-of-scope merge ordering: stages drain in id order; record observed order via a hook spy.
   - Hook firing order across stages: ensure within-stage FIFO is preserved and cross-stage ordering matches stage id.
   - Coalescing remains intact within a stage (e.g., 100 AddIDs on one entity from one worker -> one migration).

### Non-goals

- No new public mutation API. The user-visible `Writer` surface stays exactly as in v0.15.0.
- No goid lookup. Threading only. The cancelled #92 design (sync.Map keyed by goid) is explicitly rejected.
- No lock-free queue. Each stage's queue remains the existing cmdQueue with no internal mutex (single-owner invariant); contention drops to zero because no two goroutines touch the same stage on the hot path.
- No port of C's unmanaged stage (`id == -1`, `ecs_stage_new`). Out of scope.
- No port of C's `cmd_stack[2]` double-buffer. v0.15.0's single-queue-with-swap pattern at @cmd_queue.go is sufficient because Go hooks running during a stage's flush all run on the same goroutine that is doing the flush — there is no concurrent re-entry into the same stage. We may revisit if a hook needs to enqueue mid-flush and we hit a slice-resize-during-iteration hazard; if so, address with the same swap idiom C uses.
- No change to single-threaded behavior or performance.
- No change to `Reader`. Reads are already concurrent-safe via the world's RWMutex.RLock and the immutable-during-Write invariant.

### Notes on what NOT to port

- C's per-stage thread-specific runtime/script state, `lookup_path`, `scope`, `with`, `base`, `system` fields are flecs-feature carry-over (scripting, scoping API) we do not have. Skip.
- C's `ecs_stage_allocators_t` (block allocator + stack allocator per stage) is an OS-level perf optimization for C's manual memory management. Go's GC + the existing per-cmdQueue bump arena (@cmd_arena.go) cover this. Do not port the block allocator.
- C's `flecs_stage_from_world` polymorphic unwrap depends on a magic-number header (`flecs_poly_is`). Go's type system makes this unnecessary — we thread `*stage` explicitly on the Writer.
- C's `ecs_stage_new` / `ecs_merge` user-owned stages are an explicit-API feature for embedding flecs in apps with custom threading. Out of scope.

### Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run ./...` clean.
- `go test ./... -race -count=3 -timeout=180s` passes.
- Coverage on main package >= 95%.
- `BenchmarkDeferSingleSet` and `BenchmarkDeferBatchedAdds` show no regression: 0 allocs/op, same ns/op within noise versus the v0.15.0 baseline.
- New `BenchmarkMultiThreadedDeferredSet` shows >= 2x speedup on 4 workers versus 1 worker.
- New tests covering (a) per-worker stage isolation, (b) end-of-scope merge ordering, (c) hook firing in correct submission order across stages.

## Constraints

- @defer.go - current single-queue lifecycle entry point (`IsDeferred` reads `World.deferDepth`); replace with stage-aware check
- @cmd_queue.go - cmdQueue + per-entity coalescer; each stage reuses this type verbatim, one instance per stage
- @cmd.go - tagged-union `cmd` type; per-stage queues store this unchanged
- @cmd_arena.go - bump arena owned by each cmdQueue; one arena per stage by virtue of one cmdQueue per stage
- @world.go - World struct (stages table replaces `deferred`/`deferMu`/`deferDepth` at lines 60-62); `SetWorkerCount` at line 200 grows the stages table; cached `writeCapability` at line 41 needs per-worker variants for dispatch
- @scope.go - Writer struct and mutation methods (Delete, SetByID, AddID, RemoveID, SetPairByID — lines ~227-335); add stage field, route through it
- @system.go - `runPhase` multi-threaded branch at lines 287-306 constructs per-worker Writers bound to the right stage; serial branch at lines 274-278 binds stage 0
- @query.go - `it.Writer()` at line 377 returns a Writer; in multi-threaded dispatch it must return the per-worker Writer, not the shared cached one
- @scope_test.go - extend with per-stage isolation / merge-ordering / hook-ordering tests, or add a new `stage_test.go` (NEW file)
- @bench_test.go - existing `BenchmarkMultiThreadedSystem` at line 1170 is the template shape for the new `BenchmarkMultiThreadedDeferredSet`; existing `BenchmarkDeferSingleSet` (line 641) and `BenchmarkDeferBatchedAdds` (line 617) are the no-regression baselines
- @doc.go - documents the cross-stage merge ordering policy
- @CHANGELOG.md - Unreleased section gets the Phase 12.1 entry
- @ROADMAP.md - move "Per-goroutine command stages" out of Future Work
- `~/projects/SanderMertens/flecs/src/stage.c` - read end to end; primary reference for stage allocation, readonly_begin/end, and merge order
- `~/projects/SanderMertens/flecs/src/stage.h` - the `ecs_stage_t` struct definition; informs the Go `stage` field list
- `~/projects/SanderMertens/flecs/src/commands.c` - `flecs_defer_end` (line ~1113) shows the swap-and-flush dance and per-entity batch coalescing; `flecs_commands_init`/`flecs_commands_fini` show queue lifecycle
- `~/projects/SanderMertens/flecs/src/world.c` - `flecs_stage_from_world` (line ~361) is the polymorphic unwrap we replace with explicit Writer threading

Unblocked by: v0.15.0 (Reader/Writer scoped API shipped, all ~1,040 call sites migrated to scoped capabilities).

Supersedes: #92 (cancelled goid-into-a-map approach).
