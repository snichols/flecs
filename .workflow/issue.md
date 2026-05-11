## Context

This is a faithful port of the C flecs concurrency model documented in `include/flecs.h:2312` and implemented in `src/stage.c:295` (`ecs_readonly_begin` / `ecs_readonly_end`).

Two previous attempts (#77, #78) tried a Go-idiomatic `sync.RWMutex` and ran into deadlocks. Closing #78 mid-flight after researching the C original revealed it does NOT use a mutex. See #77 and #78 for the prior history and the failure modes that motivated this redesign.

The C model has three key properties:

1. **No mutex on the world struct.** Concurrency is enforced by discipline plus an atomic flag.
2. **Readonly window.** When the flag is set, all writers from any thread route through the existing deferred-command queue. Readers run concurrently because nothing mutates world state during the window.
3. **Merge on exit.** When the window closes, all deferred ops are drained on a single thread.

## What to build

Add two new public methods on `*World` and one field:

```go
// world.go
type World struct {
    ...existing...
    readonly atomic.Bool   // when true, mutators enqueue instead of mutate
}

// readonly.go (new file)
func (w *World) ReadonlyBegin() {
    // 1. Start a Defer scope (atomically increments deferDepth)
    w.DeferBegin()
    // 2. Set the readonly flag (allows other goroutines to read safely)
    w.readonly.Store(true)
}

func (w *World) ReadonlyEnd() {
    // 1. Clear the flag first so flushing mutators run immediately
    w.readonly.Store(false)
    // 2. End the Defer scope — DeferEnd flushes the queue on the caller's goroutine
    w.DeferEnd()
}

// Convenience wrapper
func (w *World) Readonly(fn func()) {
    w.ReadonlyBegin()
    defer w.ReadonlyEnd()
    fn()
}
```

## Mutator changes

Every mutator's `deferDepth` check becomes an OR with `readonly`. Today the pattern is:

```go
w.deferMu.Lock()
if w.deferDepth > 0 {
    w.deferred = append(w.deferred, func(w *World) { deleteImmediate(w, e) })
    w.deferMu.Unlock()
    return true
}
w.deferMu.Unlock()
return deleteImmediate(w, e)
```

After this issue:

```go
w.deferMu.Lock()
if w.deferDepth > 0 || w.readonly.Load() {
    w.deferred = append(w.deferred, func(w *World) { deleteImmediate(w, e) })
    w.deferMu.Unlock()
    return true
}
w.deferMu.Unlock()
return deleteImmediate(w, e)
```

(`readonly.Load()` happens under `deferMu` so the enqueue and the flag check are consistent with a concurrent `ReadonlyBegin`/`ReadonlyEnd`.)

Apply to: `Delete`, `Set[T]`, `Remove[T]`, `AddID`, `RemoveID`, `SetByID`, `SetPair[T]`, `SetName`, plus any other public mutators in `world.go` / `id_ops.go` / `value_ops.go` / `name.go` / `childof.go` / `isa.go`.

## Reader changes

**`Each1`, `Each2`, `Each3`, `Each4`, `Iter`, and cached `Iter` take NO locks.** This is the whole point — the readonly flag guarantees no concurrent writes hit world state during the window, so readers don't need to synchronize. This avoids the `Each`+`Defer`+`Delete` deadlock that sank #77.

REST handlers (`rest.go`) wrap their bodies in `w.Readonly(func() { ... })`. Snapshot save (`Marshal`) runs inside `Readonly`; snapshot load (`Unmarshal`) does NOT — `Unmarshal` is a write and the caller is responsible for serializing it.

## `inProgress` interaction

`Progress` already wraps each phase in `Defer`, so mutators inside systems already hit the enqueue path. No change needed for the `Progress` hot path. The `inProgress` flag added in Phase 10.1 stays as-is.

## Deliverables

1. `readonly.go` (new file) — `ReadonlyBegin`/`ReadonlyEnd`/`Readonly(fn)` methods, ~30 lines.
2. `readonly atomic.Bool` field on `*World` in `world.go`.
3. Every mutator's `if w.deferDepth > 0` becomes `if w.deferDepth > 0 || w.readonly.Load()`. Audit all of `world.go`, `id_ops.go`, `value_ops.go`, `name.go`, `childof.go`, `isa.go` for the pattern.
4. REST handlers in `rest.go` wrap reads in `w.Readonly(...)`.
5. `concurrent_test.go` (new file):
   - `TestReadonlyAllowsConcurrentReaders`: N goroutines each iterate via `Each1`; main goroutine holds `Readonly` open while they iterate; race-clean.
   - `TestReadonlyEnqueuesWrites`: inside `Readonly`, a writer goroutine calls `Set`/`Delete`; verify those don't fire until `ReadonlyEnd`; verify they DO fire on `End`.
   - `TestReadonlyNestedWithDefer`: `Readonly` inside `Defer` and `Defer` inside `Readonly` — both flush correctly.
   - `TestDeferWrappedIterationStillPasses`: explicit re-run of the existing pattern to prove no regression.
   - All under `go test -race -count=10`.
6. `doc.go` and `CHANGELOG.md` Unreleased updated. README gets a "Concurrency model" paragraph explaining: "Outside `Progress`, the world is single-threaded by convention. For concurrent read access, wrap in `world.Readonly(func() {...})`; writes from other goroutines during that window are buffered and flushed on `ReadonlyEnd`."

## Non-deliverables (separately tracked)

- `exclusiveAccess` debug-mode goroutine-ID assert (FLECS_EXCLUSIVE_ACCESS analog) — separate future issue.
- Per-goroutine stages with their own queues (currently single mutex-protected queue) — separate future issue.
- Defer queue tagged-union refactor — already tracked as task #40 / future Phase 11.x.

## Acceptance

- `go test ./... -race -count=10` passes clean.
- `go vet ./... && golangci-lint run` clean.
- Coverage >= 95%.
- `TestDeferWrappedIteration` (existing) passes unchanged.
- `BenchmarkSetExistingComponent` within 2% of v0.10.0 (the new code adds one `atomic.Bool.Load()` per mutator on the immediate path; should be ~1ns).
- No public API changes besides `ReadonlyBegin`, `ReadonlyEnd`, `Readonly`.

## Relevant files (all on master, all confirmed to exist)

- @world.go
- @defer.go — `DeferBegin`/`DeferEnd` already exist; readonly piggybacks on them
- @each.go
- @query.go
- @cached_query.go
- @id_ops.go
- @value_ops.go
- @name.go
- @childof.go
- @isa.go
- @rest.go
- @marshal.go
- @system.go — `inProgress` flag (no changes needed)
- @doc.go
- @CHANGELOG.md
- @README.md
- @concurrent_test.go (NEW)
- @readonly.go (NEW)

## History

- Prior attempt: #77 (sync.RWMutex; deadlocked on Each+Defer+Delete).
- Prior attempt: #78 (closed mid-flight after researching the C original; C does not use a mutex).

