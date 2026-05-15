# Context Cancellation

Go flecs (v0.106.0) extends every long-running operation with a `context.Context`-aware
variant. These variants return early when the context is cancelled or its deadline
expires, making it safe to impose per-request timeouts in game-server handlers, REST
middlewares, and background workers.

---

## Design principles

- **Cooperative cancellation.** Cancellation is checked at fixed points between
  units of work (every phase boundary, every 1024 entities). It is never forced
  mid-mutation — ECS invariants are always maintained.
- **Zero cost for uncancelled callers.** `context.Background()` returns a channel
  that is never closed; the `select` always takes the `default` branch. There is no
  measurable overhead on the hot path when cancellation is not requested.
- **Wrapper pattern.** Each existing API has a new `…Context` sibling. The original
  function delegates to the context variant with `context.Background()`, so all
  existing code compiles unchanged.

---

## API surface

### ProgressContext

```go
err := w.ProgressContext(ctx, dt)
```

Runs one ECS frame with the context applied. Returns `ctx.Err()` if cancelled.

- Cancellation is checked before each pipeline phase, after each serial system, and
  after each parallel batch wave (with a mandatory `mergeWorkerStages` flush to
  preserve mutations from already-completed waves).
- If the context fires mid-frame, mutations from completed systems are still flushed
  before the error is returned. Mutations from the in-progress system are discarded
  only if the system itself did not finish.

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
defer cancel()
if err := w.ProgressContext(ctx, dt); errors.Is(err, context.DeadlineExceeded) {
    log.Println("frame overran budget")
}
```

### RunSystemContext

```go
err := RunSystemContext(ctx, sys, dt)
```

Out-of-pipeline single-system invocation with a context check before execution.
Returns `ctx.Err()` if already cancelled; otherwise runs the system synchronously
and flushes deferred mutations.

### TakeSnapshotContext / RestoreSnapshotContext

```go
snap, err := w.TakeSnapshotContext(ctx)
err = w.RestoreSnapshotContext(ctx, snap)
```

`TakeSnapshotContext` serialises the world state into a `*Snapshot`. If the context
fires mid-walk, the snapshot is returned with `snap.Partial == true` along with the
error. A partial snapshot should not be used for restore — discard it.

`RestoreSnapshotContext` deserialises the snapshot back into the world. If cancelled
mid-way, the world is left in a partially-restored state. There is no rollback —
callers that need atomicity should take a second snapshot before restoring.

```go
// Check for a partial snapshot before using it.
snap, err := w.TakeSnapshotContext(ctx)
if snap.Partial {
    log.Println("snapshot incomplete — discarding")
    return err
}
```

### Query.EachContext / CachedQuery.EachContext

```go
err := q.EachContext(ctx, func(it *flecs.QueryIter) { ... })
err := cq.EachContext(ctx, func(it *flecs.QueryIter) { ... })
```

Iterates query results, checking the context every 1024 `it.Next()` advances.
Returns `ctx.Err()` if cancelled, `nil` otherwise.

```go
ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
defer cancel()
err := q.EachContext(ctx, func(it *flecs.QueryIter) {
    for it.Next() {
        // process entities
    }
})
if err != nil {
    log.Println("query iteration cancelled:", err)
}
```

### MarshalJSONContext

```go
data, err := w.MarshalJSONContext(ctx)
```

JSON-serialises the world, checking the context every 1024 entities. Returns
`ctx.Err()` if cancelled. On cancellation the returned byte slice is nil.

---

## REST endpoint cancellation

All REST handlers honour the `*http.Request.Context()`. When the client closes the
connection before the response is written, the server writes HTTP status **499
Client Closed Request** and aborts processing.

- `GET /query` — cancellation aborts the entity-iteration loop.
- `GET /snapshot` (`MarshalJSON`) — cancellation aborts the entity-serialization walk.
- All other endpoints — cancellation is checked once at handler entry.

```go
mux := http.NewServeMux()
mux.Handle("/", flecs.NewRESTHandler(w))
srv := &http.Server{
    Addr:        ":8080",
    Handler:     mux,
    ReadTimeout: 5 * time.Second,
}
```

The Go standard library propagates `r.Context()` cancellation automatically when
`ReadTimeout` fires or the client disconnects. No extra configuration is needed in
the REST handler.

---

## ctxCheckInterval

The constant `ctxCheckInterval = 1024` controls how often the inner loops poll the
context. It is intentionally not exported — the value is a performance tuning knob,
not part of the public API. If you need tighter cancellation granularity, use shorter
timeouts rather than shorter check intervals.

---

## Goroutine safety

All `…Context` variants follow the same goroutine-safety rules as the non-context
variants:

- `ProgressContext` / `RunSystemContext` require exclusive write access (panics with
  `ErrExclusiveAccessViolation` if called concurrently).
- `TakeSnapshotContext` / `RestoreSnapshotContext` acquire `w.mu` internally; they
  must be called **outside** any `w.Write` or `w.Read` block.
- `Query.EachContext` / `CachedQuery.EachContext` must be called from within a
  `w.Read` or `w.Write` block.
- `MarshalJSONContext` must be called **outside** any `w.Write` or `w.Read` block
  (it opens its own `w.Read` internally).

---

## Goroutine leak safety

The context variants do not start goroutines. Cancellation is cooperative — the
calling goroutine returns early from the check point, and no background goroutine is
left running. Using `go.uber.org/goleak` in tests is safe without any special
ignore rules (beyond the usual `goleak.IgnoreCurrent()` for pre-existing goroutines
from the worker pool and `net/http` server infrastructure).

---

## See Also

- [Systems.md](Systems.md) — `ProgressContext` and `RunSystemContext` details.
- [Snapshots.md](Snapshots.md) — `TakeSnapshotContext`, `RestoreSnapshotContext`, and the `Partial` flag.
- [Queries.md](Queries.md) — `EachContext` on `Query` and `CachedQuery`.
- [FlecsRemoteApi.md](FlecsRemoteApi.md) — REST endpoint context cancellation and 499 responses.
