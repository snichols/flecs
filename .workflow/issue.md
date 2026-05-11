## Context

This is the most significant remaining port from C flecs. Today our defer queue is `[]func(*World)` — closures that capture mutator arguments and call the immediate-path helper at flush time. This:

1. **Allocates per op**: closure capture forces a heap allocation per enqueued mutation (1 alloc/op).
2. **Cannot coalesce**: closures are opaque to the flush loop. 100 `Add` calls on one entity become 100 archetype migrations.
3. **Doesn't scale across goroutines**: a single mutex protects the queue (Phase 10.1's contribution to safety).

Per-row-range parallelism (Phase 10.4, v0.13.0) makes the coalescing gap most visible: N workers issuing deferred mutations all serialize through one mutex and each Set/Add creates its own migration. The C original solves this in `src/commands.c` with a tagged-union `ecs_cmd_t` + bump arena `ecs_stack_t` + entity-keyed intrusive linked list + two-pass coalescer that folds all Add/Set/Remove for one entity into a single archetype migration via `ecs_table_diff_builder_t`.

This phase ports that machinery. Per-goroutine stages (the "per-stage queue" part of the C design) are deferred to a follow-up phase — this phase keeps the existing single-queue-per-world model but makes the queue tagged-union + arena + coalesced.

## Design (mirrors C flecs)

### Cmd struct (replaces `[]func(*World)`)

```go
type cmdKind uint8

const (
    cmdSkip cmdKind = iota   // sentinel, set by coalescer
    cmdAddID
    cmdRemoveID
    cmdSetByID              // payload in arena
    cmdDelete
    cmdSetPair              // payload in arena
    cmdSetName              // payload in arena (string)
    // ...as needed for current mutator API
)

// Per the C ecs_cmd_t layout in src/commands.h:49-62.
// 32 bytes vs C's 56 — Go doesn't need union-tag overhead since kind discriminates.
type cmd struct {
    kind          cmdKind  // 1 byte
    _             [3]byte  // padding
    nextForEntity int32    // intrusive list head/index; sign-flipped on head (C convention)
    id            ID       // component id (or pair id)
    entity        ID       // target entity (the key for coalescing)
    valueOff      uint32   // offset into arena for payload; 0 if none
    valueSize     uint32   // size of payload
}
```

### Bump arena (replaces closure-captured payloads)

```go
// Mirrors ecs_stack_t in include/flecs/datastructures/stack_allocator.h:10-42.
// 1 KiB pages, reset rewinds sp (keeps pages allocated for reuse next frame).
// Oversized allocations (>1 KiB) fall through to direct heap allocation tracked
// separately so they're freed on reset.
type cmdArena struct {
    pages     [][]byte
    pageIdx   int
    sp        int
    oversized [][]byte // freed on reset
}

func (a *cmdArena) alloc(size, align int) (offset uint32, buf []byte) { ... }
func (a *cmdArena) bytes(offset uint32, size uint32) []byte { ... }
func (a *cmdArena) reset() { /* sp = 0, pageIdx = 0, free oversized */ }
```

### Per-entity index (enables coalescing)

```go
// Mirrors ecs_sparse_t<ecs_cmd_entry_t> in C; in Go we use a plain map.
type cmdEntry struct {
    first, last int32 // indices into cmds slice
}

type cmdQueue struct {
    cmds    []cmd
    arena   cmdArena
    entries map[ID]*cmdEntry // entity -> head/tail of its intrusive list
}

func (q *cmdQueue) append(c cmd) int32 { ... } // links into entity's list
func (q *cmdQueue) reset() { /* clear cmds, reset arena, clear entries */ }
```

### Two-pass coalescer (`batchForEntity`)

Mirrors `flecs_cmd_batch_for_entity` in src/commands.c:836-1110.

**Pass 1**: walk the entity's linked list once. For each cmd, simulate its archetype effect on a running `tableDiff` accumulator (added IDs, removed IDs) and a pending `table` pointer. Add/Remove/Clear cmds get rewritten to `cmdSkip` (their effect is folded into the diff). Set/Ensure cmds stay but flag `hasSet = true`. After the walk, execute ONE archetype migration on the entity via `commit(entity, srcTable, dstTable, diff)`.

**Pass 2** (only if `hasSet`): walk the entity's linked list again. For each remaining `cmdSetByID`/`cmdSetPair`, copy the arena payload into the entity's final column storage, fire `on_replace` hook if registered, then rewrite the cmd to a `cmdModified` marker so the main flush switch fires OnSet at its original position in submission order.

### Flush

```go
func (q *cmdQueue) flush(w *World) {
    for i := int32(0); i < int32(len(q.cmds)); i++ {
        c := &q.cmds[i]
        if c.kind == cmdSkip {
            continue
        }
        // The C convention: head of a per-entity list has next_for_entity < 0.
        // First time we see an entity's head, run batchForEntity, which rewrites
        // its other cmds to cmdSkip / cmdModified.
        if c.nextForEntity < 0 && q.entries[c.entity] != nil {
            q.batchForEntity(w, c.entity)
        }
        // Then dispatch this cmd's main effect (post-rewrite).
        q.dispatch(w, c)
    }
    q.reset()
}
```

### Dispatch ordering

Preserved from C semantics: hooks fire at the submission position of each surviving cmd, in submission order. Migration happens once per entity at the position of its first cmd.

## What does NOT change in this phase

- Single queue per world (no per-stage queues yet — that's the next phase). `deferMu` still protects against concurrent enqueue.
- Public API: `DeferBegin`, `DeferEnd`, `Defer(fn)`, all mutators called inside defer scope — signatures identical.
- Readonly mode behavior — uses the same queue.
- `inProgress` semantics in Progress.

## Deliverables

1. **`cmd.go`** (NEW): `cmd` struct + `cmdKind` constants. Doc-comment the layout vs C `src/commands.h`.

2. **`cmd_arena.go`** (NEW): `cmdArena` with `alloc(size, align)`, `bytes(offset, size)`, `reset()`. 1 KiB pages reusable across frames. Doc-comment vs C `flecs_stack_alloc`.

3. **`cmd_queue.go`** (NEW): `cmdQueue` with `append`, `flush`, `reset`, `batchForEntity`, `dispatch`. The two-pass coalescer lives here. Replaces the old `[]func(*World)` field on `World`.

4. **`world.go`**: replace `deferred []func(*World)` with `deferred *cmdQueue`. `deferMu` still guards enqueue+flush.

5. **`defer.go`**: `DeferEnd` calls `w.deferred.flush(w)` instead of iterating closures.

6. **Mutator paths** (existing locations in world.go, id_ops.go, value_ops.go, name.go, etc.): replace the closure-append pattern with a `cmd`-struct append. Example today:
   ```go
   w.deferred = append(w.deferred, func(w *World) { deleteImmediate(w, e) })
   ```
   becomes:
   ```go
   w.deferred.append(cmd{kind: cmdDelete, entity: e})
   ```
   For Set with payload:
   ```go
   off, buf := w.deferred.arena.alloc(int(typeSize), int(typeAlign))
   copy(buf, valueBytes)
   w.deferred.append(cmd{kind: cmdSetByID, entity: e, id: cid, valueOff: off, valueSize: typeSize})
   ```

7. **`tableDiff` builder** in `internal/storage/table`: mirrors `ecs_table_diff_builder_t`. Accumulates added/removed IDs during pass 1 so pass 1's final `commit` can do one migration. Most of the migration code already exists — this is a per-walk accumulator wrapper.

8. **Tests** in `defer_coalesce_test.go` (NEW):
   - `TestDeferCoalescesAddsToOneMigration`: 100 `AddID` calls on one entity inside a Defer scope, verify exactly 1 archetype migration via a hook counter or table-version counter.
   - `TestDeferCoalescesRemoveAfterAdd`: `AddID(A); RemoveID(A); AddID(B)` → final table has B, not A; one migration.
   - `TestDeferSetValuePreservedAfterCoalesce`: `AddID(C); SetByID(C, value)` → final value in column matches.
   - `TestDeferHooksFireAtSubmissionPosition`: `OnSet[C]` registered; `SetByID(C, v1); SetByID(C, v2);` → hook fires twice in order.
   - `TestDeferOriginalTestsStillPass`: rerun existing TestDeferWrappedIteration / TestDeferNesting / etc.

9. **Benchmarks** in `bench_test.go`:
   - `BenchmarkDeferBatchedAdds`: 100 Adds on one entity inside one Defer scope. Compare new vs current (current expected ~100 migrations; new expected 1).
   - `BenchmarkDeferSingleSet`: zero-alloc on the hot path (no closure capture).
   - Verify `BenchmarkSetExistingComponent` (deferred-not-active path) is unchanged.

10. **doc.go + CHANGELOG.md + README**: document the design and the coalescing behavior. Cross-reference C `src/commands.c`.

11. **ROADMAP.md**: when promoting to Shipped (v0.14), move "Defer queue refactor: closure capture → tagged-union or typed buffers" out of Future Work / Performance.

## Acceptance

- `go test ./... -race -count=10` clean.
- `go vet` + `golangci-lint run` clean.
- Coverage ≥ 95%.
- `BenchmarkDeferBatchedAdds`: ≥10× speedup vs current (the closure-based code).
- `BenchmarkDeferSingleSet`: 0 allocs/op on the deferred path.
- `BenchmarkSetExistingComponent` (the immediate path): regression ≤ 1% vs v0.13.0.
- All existing defer tests pass unchanged.
- Hook firing order preserved (FIFO submission order). Document this invariant explicitly.
- No public API changes.

## Non-deliverables (explicitly out of scope)

- Per-goroutine / per-stage queues. The current single mutex-protected queue stays. Phase 11.1 will lift this.
- Double-buffered queue for re-entrant flush (`cmd_stack[2]` in C). Hooks fired during flush still enqueue back to the SAME queue (which is OK since the flush loop appends and re-reads `len(cmds)` on each iteration — see C's commands.c:1169-1337 outer-loop semantics).
- BulkNew opcode (port later when needed).
- Lock-free flush (mutex-protected fine for now).

## Relevant C source (cross-reference these while implementing)

- /work/agents/claude/projects/SanderMertens/flecs/src/commands.h:49-62 — `ecs_cmd_t` struct layout
- /work/agents/claude/projects/SanderMertens/flecs/src/commands.c:38-85 — `flecs_cmd_new_batched` (enqueue + intrusive-list linking)
- /work/agents/claude/projects/SanderMertens/flecs/src/commands.c:836-1110 — `flecs_cmd_batch_for_entity` (the coalescer)
- /work/agents/claude/projects/SanderMertens/flecs/src/commands.c:1113-1361 — `flecs_defer_end` (flush)
- /work/agents/claude/projects/SanderMertens/flecs/src/datastructures/stack_allocator.c:30-183 — bump arena
- /work/agents/claude/projects/SanderMertens/flecs/include/flecs/private/api_types.h:156-160 — `ecs_commands_t` aggregate

## Relevant Go files

- @world.go — replace deferred []func with *cmdQueue field; mutator dispatch
- @defer.go — DeferEnd → cmdQueue.flush
- @id_ops.go, @value_ops.go, @name.go, @childof.go, @isa.go — call-site rewrites
- @cmd.go (NEW), @cmd_arena.go (NEW), @cmd_queue.go (NEW)
- @internal/storage/table/table.go — may need tableDiff builder helpers (or build inside cmd_queue.go using existing primitives)
- @defer_coalesce_test.go (NEW), @bench_test.go — additions
- @doc.go, @CHANGELOG.md, @README.md, @ROADMAP.md

