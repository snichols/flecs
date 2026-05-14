## Goal

Port upstream `ecs_run_worker` to Go-flecs as `RunSystemWorker(w, sys, workerIndex, workerCount, dt)` — a synchronous, single-system "run a slice of the matched entity range" entry point that callers fan out across goroutines for explicit work partitioning.

**Why now.** Phase 16.3 shipped `RunSystem` (Phase 14.6 systems gap line 145 in `docs/README.md`). Phase 12.1 shipped the per-stage queue used by the multi-threaded pipeline dispatcher. The remaining sub-gap on line 146 of `docs/README.md` is the *out-of-pipeline* counterpart: a function callers can invoke from N goroutines, where worker `i` of `N` processes a disjoint slice of `sys`'s matched entities. Classic use case is physics or AI updates over thousands of entities where the caller wants explicit fan-out rather than the pipeline's own multi-threaded path.

**Shape.** `RunSystem` is the direct precedent (`system.go:566–588`). `RunSystemWorker` is the same shape with two extra parameters and a per-call private stage:

```go
RunSystemWorker(w *World, sys *System, workerIndex, workerCount int, dt float32)
```

Each call:
1. Validates `workerCount > 0` and `0 <= workerIndex < workerCount` (panic otherwise).
2. Opens its own deferred-mutation scope backed by a **fresh per-call stage** (cmdQueue), not `stages[0]` and not a pooled worker stage. This is what makes the call safe to invoke from multiple goroutines simultaneously: each goroutine has its own queue.
3. Builds a `QueryIter` clipped to `(workerIndex, workerCount)` via the existing `clippedCopy` mechanism (`query.go:2026–2034`). The same per-table q/r split used by the pipeline's multi-threaded dispatcher (`query.go:1508–1519`) is reused — partitioning is deterministic per table and matches upstream `ecs_worker_next` (`src/iter.c:920–1003`).
4. Calls `s.fn(dt, it)`.
5. Flushes the per-call stage's deferred commands before returning. Across concurrent worker calls the flush ordering is undefined.

**Upstream parity.** Upstream's `ecs_run_worker` (`include/flecs/addons/system.h:337–343`, `src/addons/system/system.c:161–178`) wraps `flecs_run_system` with `(stage_index, stage_count)` and `flecs_defer_begin` / `flecs_defer_end`. The clipping happens inside `ecs_worker_next` via `count / res_count` per-table — same algorithm Go already uses for `MultiThreaded` systems inside `Progress`. `RunSystemWorker` simply exposes that out-of-pipeline.

**Decision lock-ins (from the proposal).**
- Equal-slice partition (per-table q/r), deterministic, matches upstream and the existing Go pipeline path. No work-stealing.
- Each call gets a fresh per-call stage; flush happens before the call returns; ordering across concurrent calls is undefined.
- Disabled systems still run (mirrors `RunSystem`, `system.go:154`).
- No automatic worker pool — caller owns goroutine spawn/join.

**Test surface** (in `run_worker_test.go`, target ≥ 95.0% coverage):
1. 4 workers / 100 entities — total processed = 100, disjoint sets.
2. 4 workers / 7 entities (uneven) — each gets roughly 7/4; total = 7. Verifies the q/r remainder distribution.
3. 1 worker / N entities — equivalent to `RunSystem`.
4. `workerCount == 0` panics.
5. `workerIndex >= workerCount` panics; `workerIndex < 0` panics.
6. Four goroutines each calling `RunSystemWorker(w, sys, i, 4, dt)` concurrently — race-detector clean (`-race -count=3`); total work matches expected; no entity processed twice.
7. Deferred mutations inside the system — flushed per worker call; final state correct after all workers complete.
8. Empty system (no matched entities) — all workers run with zero work, no panic.
9. Disabled system + `RunSystemWorker` — still runs (parity with `RunSystem`).
10. Sparse-term query — partition still works; sparse term contributes one entity per step (see `query.go:1974–1980` `Count()` returns 1 for sparse iters), so multi-worker behaviour over sparse-only or mixed-sparse queries needs an explicit semantics decision documented in the test (suggestion: only workerIndex == 0 advances the sparse cursor; other workers see zero matches — mirrors the per-table `wCount == 0 → continue` path).

**Docs** (per CONTRIBUTING.md `New phase / feature` row, line 72):
- `docs/Systems.md` — new `## RunSystemWorker` section with the goroutine-fan-out example.
- `docs/README.md` — line 146 flips to "✅ shipped in v0.82.0".
- `README.md` — feature-list bump.
- `CHANGELOG.md` — v0.82.0 entry at top with API, semantics, parity notes, and the four lock-in decisions.
- `ROADMAP.md` — line 3 heading bumps to "Shipped (through v0.82.0)"; Phase 16.27 row added.

**Mechanical acceptance.**
- `go vet ./...` clean.
- `golangci-lint run` clean.
- `go test ./... -race -count=3` passes — concurrent paths must be race-free.
- Coverage ≥ 95.0%.

**Non-goals.** No worker pool, no work-stealing, no deadline-aware partitioning, no cross-worker batched flush.

## Constraints

- @system.go — `RunSystem` (lines 566–588) is the direct precedent. `RunSystemWorker` mirrors its shape (validation panic → `deferScope` → `query.Iter()` → callback → flush), with the worker clipping and a private stage. The `SetEnabled` / `RunSystem` doc note on line 154 documents the "explicit invocation bypasses enabled flag" contract that `RunSystemWorker` must mirror.
- @system.go — multi-threaded pipeline dispatch (lines 390–448) is the reference implementation of per-table worker clipping. It uses `base.clippedCopy(wi, n)` and a per-worker `workerWriter` bound to `stages[i+1]`. `RunSystemWorker` reuses `clippedCopy` but must NOT reuse `stages[i+1]` (those belong to the pipeline worker pool); it allocates its own private stage per call.
- @query.go — `clippedCopy` (lines 2026–2034) and the per-table q/r split (lines 1508–1519) provide the partition algorithm. Reuse exactly — do not reimplement. `Count()` / `Entities()` already special-case `workerTotal > 0` (lines 1977, 2001).
- @query.go — sparse-term handling (lines 1974–1980): for sparse-only or mixed-sparse queries `Count()` returns 1 per step. The partitioning interaction is non-obvious and must be either supported and documented or explicitly rejected; the proposal recommends documenting the semantics in the test.
- @stage.go — `stage` struct (lines 16–22) is the per-goroutine command-queue context. `RunSystemWorker` constructs a transient stage on each call (not pooled) so that N concurrent calls operate on N disjoint queues without touching `stages[0]` or the pipeline worker stages.
- @world.go — `deferScope` (lines 1748–1768) is the merge-with-hooks wrapper used by `RunSystem`. `RunSystemWorker` cannot reuse it as-is because `deferScope` is bound to `stages[0]` (line 1749); a parallel helper is needed that flushes a caller-supplied stage, OR `deferScope` is refactored to accept a stage parameter. Prefer the latter — fewer code paths, easier to reason about merge-hook ordering.
- @world.go — worker stage allocation (lines 752–776) shows the pattern for building per-worker stages with `deferDepth: 1`. The fresh per-call stage in `RunSystemWorker` follows the same pattern but is short-lived (one call), not pool-resident.
- @world.go — `checkExclusiveAccessWrite` (called by `RunSystem` at line 583) gates RunSystem on the single-writer invariant. `RunSystemWorker` is explicitly designed for concurrent calls — the exclusive-access check must be replaced with a different guarantee. Options: (a) caller holds an outer `World.Write` scope and `RunSystemWorker` is called only from inside it (each call piggybacks on the outer scope's exclusivity); (b) `RunSystemWorker` is the exclusive-access claimant itself, and concurrent calls share access via the per-stage queue invariant (single-goroutine ownership of each stage, mirroring the existing pipeline worker pool). The proposal implies (b) — lock that in and document it.
- @CONTRIBUTING.md — line 72: "New phase / feature" doc-update matrix is binding. ROADMAP.md (move from Future Work → Shipped) and CHANGELOG.md entry are required.
- @CONTRIBUTING.md — line 56: file-level coverage target is ≥ 90%; the proposal raises this to ≥ 95% for this phase, matching the project's recent ratchet (last several phases shipped at 95.0%).
- @docs/README.md — line 146 is the gap entry to flip. The phrasing convention from neighbouring shipped rows (lines 141–147) is: `✅ **shipped in v0.82.0** via \`RunSystemWorker(...)\`. <one-sentence summary>. See [Systems.md § RunSystemWorker](Systems.md#runsystemworker).`
- @ROADMAP.md — line 3 heading currently reads `## Shipped (through v0.81.0)`; bump to `v0.82.0`. Add a one-line Phase 16.27 entry alongside the Phase 16.25/16.26 entries (lines 84–85 style).
- @CHANGELOG.md — top entry currently `## v0.81.0 — 2026-05-14 — Phase 16.26: Multi-variable query support`. Add `## v0.82.0 — Phase 16.27: RunSystemWorker — explicit thread dispatch` above it, following the section template (API additions, semantics, parity notes, decision lock-ins, docs touched).
- Upstream reference `include/flecs/addons/system.h:337–343` — `ecs_run_worker` signature: `world, system, stage_current, stage_count, delta_time, param`. Go signature drops `param` (unused; the Go callback closes over its environment) and uses `float32` for `dt` to match `RunSystem`.
- Upstream reference `src/addons/system/system.c:161–178` — `ecs_run_worker` body wraps `flecs_run_system` with `flecs_defer_begin` / `flecs_defer_end`. The Go equivalent is the per-call private-stage `deferScope` variant described above.
- Upstream reference `src/iter.c:920–1003` — `ecs_worker_iter` + `ecs_worker_next` define the partition algorithm: per-table `count / res_count` with `r = count % res_count` remainder distributed to the first `r` workers (`if (res_index < count) per_worker++`). Go's `query.go:1508–1519` is the line-for-line equivalent (`q, r := n/it.workerTotal, n%it.workerTotal; ... if it.workerIdx < r { it.wCount++ }`). Partition is **deterministic** per table iteration order; archetype-table order is stable for the world's lifetime modulo new table creation. Concurrency: upstream assumes the caller handles synchronization across workers — each `ecs_run_worker` call wraps its own `defer_begin/end` against a stage; Go-flecs's per-call private stage gives the same isolation.
