## Goal

Port world-level **pre/post merge hooks** to Go flecs as Phase 16.23, shipping in **v0.78.0**. Today the Phase 11.0 coalescer (`cmd_queue.go:flush`) applies deferred commands at the end of every `w.Write(fn)` block (and at end-of-phase in pipelined system execution), but there is no way for external code to react at those boundaries. Tools that want to time merges, log every command-merge, snapshot pre-merge state for diff, or drive editor inspectors at merge points have no hook surface.

**Pre-merge** fires immediately before deferred commands are applied (after `fn` returns, before `q.flush(w)`).
**Post-merge** fires immediately after the flush completes.

### Upstream context

Upstream C flecs does **not** ship explicit pre/post-merge user hooks. The closest surfaces are:

- `flecs_stage_merge` (`src/stage.c:21-67`) — drives `flecs_defer_end` for one or all stages, then increments `info.merge_count_total` and `info.merge_time_total` (lines 56-59) for *observability only* — no user callback fires.
- `ecs_run_post_frame(world, action, ctx)` (`include/flecs.h:2204-2216`) — registers a **one-shot** post-frame action stored on the stage's `post_frame_actions` vec; drained by `flecs_stage_merge_post_frame` (`src/stage.c:69-81`). This is per-frame, not per-merge, and one-shot rather than persistent.
- `ecs_set_hooks_id` (`include/flecs.h:4398-4401`) — per-component construct/copy/move/destruct/add/remove/set hooks; **not** merge-boundary hooks.

So Phase 16.23 is a **deliberate divergence** from upstream: a persistent, world-level registration API for callbacks at the deferred-merge boundary. The closest spiritual parent is `ecs_run_post_frame`, generalized to per-merge and made persistent.

### API shape

Mirrors the Phase 16.8 observer registration pattern (`Observe[T](w, event, fn)` returns a handle, registered handles persist until `Unsubscribe`):

```go
id := flecs.OnPreMerge(w, func(fw *flecs.Writer) { /* … */ })
id2 := flecs.OnPostMerge(w, func(fw *flecs.Writer) { /* … */ })
flecs.RemovePreMergeHook(w, id)
flecs.RemovePostMergeHook(w, id2)
```

Registration returns an `int` index (slice position). Removal is idempotent — stale IDs are silent no-ops.

### Storage

New fields on `World` (see `world.go:40` struct definition):

```go
preMergeHooks  []func(fw *Writer)
postMergeHooks []func(fw *Writer)
```

Slice index = registration ID. Removal tombstones the slot (nil entry) rather than shifting, so subsequent IDs remain stable.

### Fire wiring

Three call sites where the coalescer flushes today (verified):

1. `world.go:1700-1703` — outermost `Write` (non-nested) exit.
2. `world.go:1719-1722` — nested `Write` outermost exit on owned goroutine.
3. `world.go:1744-1747` — `deferScope` outermost exit (internal: used by `Progress`/`runPhase`).
4. `system.go:428-437` — multi-threaded dispatch merge: per-worker-stage flush in ascending id order, then stage 0.

Each site swaps `s0.queue` for a fresh queue before calling `q.flush(w)`. Pre-merge hooks fire **before** `q.flush(w)`; post-merge hooks fire **after** `releaseCmdQueue(q)`.

### Locked-in design decisions

1. **Pre-merge mutations batch with the current merge.** Hooks run inside a Writer scope; mutations queued by a hook are appended to the queue about to be flushed, so they participate in the same coalescing pass.
2. **Post-merge mutations queue for the next merge.** The current merge is complete; the fresh queue installed before flush captures any post-merge mutations, which flush when the next `Write` scope exits.
3. **Hooks registered during a merge fire on the next merge.** The fire loop iterates a captured snapshot of the hook slice; mid-loop appends affect subsequent merges only.
4. **Re-entrant `w.Write` from a hook panics.** A merge is in progress; the queue is mid-swap and the deferDepth bookkeeping would be inconsistent. Detect by checking `s0.deferDepth > 0` inside Write's same-goroutine branch and panic with a dedicated `ErrMergeReentry` (matching the `ErrExclusiveAccessViolation` pattern at `scope.go:15`).

### Multi-stage policy

Worker-stage merges (`system.go:428-437`) iterate `for i := 1; i <= n; i++` then flush stage 0. **Hooks fire ONCE per `Progress`-driven merge cycle**, not once per worker stage — pre fires before the first worker-stage flush, post fires after stage 0's flush. This matches user expectations: a hook is a "merge boundary" hook, not a "stage-flush" hook. Document explicitly.

### Tests in `merge_hooks_test.go` (≥ 8 cases, ≥ 95.0% coverage)

1. Register pre-merge hook; `w.Write(fn that adds a component)`; pre-merge fires before the component is observable via `Has[T]`.
2. Register post-merge hook; fires after the component is observable.
3. Both registered: pre fires first, then commands apply, then post fires (assert ordering via a shared counter).
4. Multiple pre-merge hooks fire in registration order (FIFO across the slice, skipping nil tombstones).
5. Pre-merge hook adds a component; that mutation batches with the current merge (visible immediately after `Write` returns).
6. Post-merge hook adds a component; queues for next merge (NOT visible until the next `Write` block flushes).
7. `RemovePreMergeHook` / `RemovePostMergeHook`: subsequent merges do not fire the removed hook; re-register after remove works.
8. Hook registered from inside another hook: fires on next merge, not the current one.
9. Nested `w.Write` from a hook: panics with `ErrMergeReentry`.
10. Empty merge (no deferred commands queued in `fn`): pre and post hooks still fire (the merge boundary exists regardless of queue depth).
11. Concurrent safety: register hooks from one goroutine while merges run on another (gated by exclusive access — registration must happen outside a Write); race-detector clean across `go test ./... -race -count=3`.
12. Multi-threaded system path: pre fires once before the first worker-stage flush, post fires once after stage-0 flush — not 1+N times.
13. Removing a hook from inside its own callback: removal applies starting on the next merge (mid-merge mutation of the slice is via the captured snapshot).

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%
- No regression on existing coalescer tests (`defer_coalesce_test.go`, `defer_test.go`).

### Explicit non-goals

- No fine-grained per-command hooks (those exist via observers on individual components).
- No conditional pre-merge that can CANCEL the merge — pre-merge is observe-only (plus mutate-to-batch).
- No batched stats / timing API as part of this phase — that's a separate observability layer atop these hooks.
- No per-stage-merge hooks distinct from world-level — world-level only (all stages share the registered hooks; one fire pair per `Progress`-merge cycle).
- No analog to `ecs_run_post_frame`'s one-shot semantics — hooks here are persistent until removed.

### Doc updates (per CONTRIBUTING.md § Documentation)

- **`docs/Systems.md`** near line 86 (phase-merge paragraph) — add a § Merge hooks subsection covering registration API, fire ordering, and the multi-stage one-fire-per-merge policy.
- **`docs/ObserversManual.md`** — cross-reference from the deferred-coalescer section (~ line 554, 766) noting merge hooks as the world-level analog to per-id observers.
- **`docs/README.md` line 75** — flip `World-level pre/post merge hooks — not ported.` to `**World-level pre/post merge hooks** — **shipped in v0.78.0** via `OnPreMerge` / `OnPostMerge` / `RemovePreMergeHook` / `RemovePostMergeHook`. See [Systems.md § Merge hooks](Systems.md#merge-hooks).`
- **`README.md`** — feature-list bump near line 234 (existing Defer entry) or in the v0.78.0 row.
- **`CHANGELOG.md`** — new v0.78.0 entry at top, dated 2026-05-14.
- **`ROADMAP.md`** line 3 — heading bump to `## Shipped (through v0.78.0)`; add bullet under Shipped.

## Constraints

- @cmd_queue.go — the coalescer entry point: `flush(w)` at line 87 runs the two-pass batcher per entity and dispatches. Pre-merge hooks fire **before** the outer `q.flush(w)` call at `world.go:1702`/`1721`/`1746`; post-merge fires after `releaseCmdQueue(q)`. The hooks do NOT fire per-entity inside the dispatch loop — that is a per-command observer concern.
- @scope.go — `Writer` struct at line 31; `Writer.Write` is on `*World` not `*Writer`, but every mutator method (`AddID` at line 366, `SetByID` at line 412, `Set[T]` at line 645) routes via `fw.stage.queue.append`. Hook callbacks receive a `*Writer` so they can read AND queue mutations using the existing API. Pre-merge hook mutations land in the same queue about to be flushed; post-merge hook mutations land in the fresh queue installed at `world.go:1701`/`1720`/`1745`.
- @world.go — `World` struct at line 40; merge fire sites at lines 1700-1703 (outermost Write same-goroutine), 1719-1722 (Write fresh-claim), 1744-1747 (deferScope). The `s0.deferDepth == 0` guard at each site is the exact point pre-fire happens after, post-fire before the closure ends. `ErrExclusiveAccessViolation` at `scope.go:15` is the precedent for `ErrMergeReentry`.
- @stage.go — `stage` struct at line 16; `deferDepth` field at line 20 tracks Write/deferScope depth. Worker stages (`system.go:428-437`) flush in ascending id order then stage 0; the multi-stage merge counts as ONE merge boundary for hook firing purposes.
- @system.go — multi-threaded dispatch merge at lines 428-437: per-worker flush loop then stage 0 flush. Pre fires before line 428's loop entry; post fires after the `releaseCmdQueue(q0)` at line 437.
- @observer.go — Phase 16.8 reference pattern: `Observe[T]` (line 166) takes a typed callback, registers via `addObserverNode`, returns an `*Observer` handle with `Unsubscribe`. Merge hooks follow the same shape but world-level (no entity/event keying, no observerNode/observerBucket dispatch table — direct slice append).
- @CHANGELOG.md — v0.77.0 entry style at top (header `## v0.77.0 — 2026-05-14 — Phase 16.22: …` followed by paragraph + `### Added` / `### Behaviour` / `### Upstream C references` sections). Match this structure for v0.78.0.
- @CONTRIBUTING.md § Documentation — every shipped feature updates `docs/`, `README.md`, `CHANGELOG.md`, `ROADMAP.md`.
- @docs/README.md line 75 — the gap-list entry that flips to ✅ on ship.
- @docs/Systems.md line 86 — natural home for the new § Merge hooks subsection, adjacent to the existing "Deferred commands from each phase are flushed before the next phase starts" sentence.
- Upstream C flecs has **no equivalent persistent merge-hook API** — the closest is `ecs_run_post_frame` (`include/flecs.h:2204-2216`, one-shot, per-frame). Phase 16.23 is a deliberate divergence. Cite this in the CHANGELOG `### Upstream C references` section.
- Upstream merge mechanics: `src/stage.c:21-67` (`flecs_stage_merge`) is the C analog of Go's `flush(w)`; it increments `info.merge_count_total` / `info.merge_time_total` (lines 56-59) for observability without a user callback. The Go hooks complete that gap.
