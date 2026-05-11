## Goal

Add read-only concurrent query iteration to `*World` via a `sync.RWMutex` so multiple goroutines can call `Each1`/`Each2`/`Each3`/`Each4`/`Iter` concurrently, while mutators serialize as writers. This is a re-file of #77, which self-blocked on a deadlock in `TestDeferWrappedIteration`. See that issue (especially the closing comment) for full context on why the previous attempt failed.

## Critical design constraint (root cause of #77's failure)

Every mutator currently has this shape:

```go
func (w *World) Delete(e ID) bool {
    w.deferMu.Lock()
    if w.deferDepth > 0 {
        // ...enqueue closure...
        w.deferMu.Unlock()
        return true
    }
    w.deferMu.Unlock()
    return deleteImmediate(w, e)   // <-- THIS is the immediate path
}
```

`rwmu.Lock()` / `rwmu.Unlock()` MUST be added **only on the immediate-mutation tail** (around the `deleteImmediate(w, e)` call), NOT at function entry. When `deferDepth > 0`, the function enqueues under `deferMu` and returns without ever touching `rwmu`.

This placement is what prevents the `Each` + `Defer` + `Delete` deadlock: the `Delete` enqueue path never tries to acquire `rwmu` while `Each1`'s `RLock` is held. The previous attempt (#77) acquired `rwmu.Lock()` at function entry, which deadlocked because the enqueue path tried to grab the write lock while a reader still held the read lock from the enclosing `Each1`.

The same shape applies to every mutator: see `world.go` (Delete, Set[T], Remove[T]), `id_ops.go` (AddID, RemoveID), `value_ops.go` (SetByID), plus pair-set and hierarchy mutators.

## Deliverables

1. **`*World` gains `rwmu sync.RWMutex`** (unexported field) and four public methods: `RLock()`, `RUnlock()`, `Lock()`, `Unlock()` for advanced users who need to compose multi-call read or write critical sections.

2. **Reader-side instrumentation**: `Each1`, `Each2`, `Each3`, `Each4`, `Iter` (and any other public iteration entry points in `each.go` / `query.go` / `cached_query.go`) acquire `rwmu.RLock()` at the top and `defer rwmu.RUnlock()`. **EXCEPTION**: if `w.inProgress` is true, skip rwmu acquisition entirely (`Progress` already holds the world via its own discipline and bracket-everything-in-Defer pattern).

3. **Writer-side instrumentation**: every mutator's immediate-mutation tail acquires `rwmu.Lock()` and releases before returning. The deferred-enqueue branch must NOT acquire rwmu. **EXCEPTION**: same `inProgress` skip applies. Cover at minimum: `Delete`, `Set[T]`, `Remove[T]`, `AddID`, `RemoveID`, `SetByID`, `SetPair[T]`, `SetName`, plus internal helpers `deleteImmediate`, `setImmediate*`, `removeImmediate`, `addIDImmediate`, `removeIDImmediate`, `setImmediateByPtr` — instrument at the public-method tail, not in the unexported helpers (helpers are also called from the deferred flush path which has its own locking strategy).

4. **DeferEnd flush**: the per-closure invocations inside the flush loop (`defer.go`) run mutators that will hit the immediate path (since `deferDepth==0` by then). They will each acquire/release rwmu. That's correct — flushing is a writer operation and should serialize with readers.

5. **REST handler updates** (`rest.go`): each handler that reads world state acquires `w.RLock()` / `defer w.RUnlock()` around its body. Snapshot save (Marshal) takes RLock; snapshot load (Unmarshal) takes Lock.

6. **Tests** in `concurrent_test.go`:
   - `TestConcurrentReaders`: N goroutines call `Each1` concurrently while a single goroutine periodically calls `Defer{ Set, Delete }` batches; `-race` clean over 10 iterations.
   - `TestRLockSerializesWithLock`: spawn reader, mid-iteration spawn writer goroutine, assert writer blocks until reader done (use channel ordering).
   - `TestDeferWrappedIterationStillPasses`: explicitly re-run the existing `TestDeferWrappedIteration` pattern (`Defer { Each1 { Delete } }`) to prove no deadlock with the new locking.
   - All tests run under `go test -race -count=10`.

7. **Doc.go and CHANGELOG.md (Unreleased section)** updated to describe the new concurrency model. README gets a one-paragraph "Concurrency model" section if not already present.

## Acceptance

- `go test ./... -race -count=10` passes (clean — no warnings, no skips).
- `go vet ./... && golangci-lint run` clean.
- Coverage ≥ 95%.
- All existing tests pass unchanged, especially `TestDeferWrappedIteration`.
- No public API changes besides the four new methods (`RLock`/`RUnlock`/`Lock`/`Unlock`).
- `BenchmarkSetExistingComponent` regression ≤ 5% vs v0.10.0 baseline (the extra Lock/Unlock pair per mutator on the immediate path is the only added cost; benchmarks should still be in the same ballpark since uncontended Mutex Lock/Unlock is ~12 ns).

## Non-goals

- Parallel reads inside `Progress` (deferred — Progress already serializes phases).
- Per-table parallelism within a single system (deferred to a later phase).
- Lock-free defer queue (deferred).

## Constraints

- @world.go — declares `*World`, all top-level mutators, `deferMu`, `deferDepth`, `inProgress`
- @defer.go — DeferBegin, DeferEnd, Defer
- @each.go — Each1/2/3/4 entry points
- @query.go — Iter
- @cached_query.go — cached Iter
- @id_ops.go — AddID, RemoveID
- @value_ops.go — GetByID, SetByID
- @name.go — SetName, GetName (read), Lookup (read)
- @childof.go — child-link mutations
- @isa.go — prefab-link mutations
- @hooks.go — hook-invocation surface (no locking; called from mutators which lock)
- @observer.go — observer Notify (no locking; called from mutators which lock)
- @system.go — Progress, inProgress flag, parallel dispatch
- @rest.go — HTTP handlers (need RLock around bodies)
- @marshal.go — MarshalJSON (RLock), UnmarshalJSON (Lock)
- @meta.go — Components, ComponentInfo, EntityComponents, EachEntity, AliveEntities (RLock)
- @stats.go — Stats() snapshot (RLock)
- @traversal.go — GetUp / HasUp / TargetUp (RLock; these are readers)
- @doc.go — public package documentation
- @CHANGELOG.md — append to Unreleased section
- @README.md — concurrency model paragraph
- @concurrent_test.go — NEW file for the tests above
- Re-file of #77 — read its closing comment for the deadlock post-mortem before starting.

