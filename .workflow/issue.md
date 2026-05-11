## Goal

Phase 8.3 of the Go port of flecs. Phase 8.2 landed 40 benchmarks across 8 categories with baseline numbers in BENCH.md; the reviewer identified 5 concrete hotspots. This issue ships the top 3 quick-win micro-optimizations with before/after measurements. No behavior changes. No API changes.

Baseline: master HEAD `e867e2b`.

### Reviewer-identified hotspots

1. `Field[T]` uses `reflect.Value.Interface().([]T)` — 1 alloc / 24 B per call. Cascades into every iteration benchmark.
2. Observer dispatch allocates a snapshot slice per fire (~48 B x N fires).
3. `Get[T]`/`Has[T]` allocate a `seen` map unconditionally on local miss, even for entities with no IsA prefab.
4. Defer closure capture — ~1 alloc per deferred op (bigger refactor; NOT in scope).
5. `Swap` component 2x heavier than `Add` (493 ns / 4 allocs vs 188 ns / 2 allocs) — needs investigation (NOT in scope).

### Scope: Three targeted micro-optimizations + before/after measurements

- Optimization A: `Field[T]` -> zero-alloc unsafe.Slice path.
- Optimization B: Observer dispatch -> eliminate per-fire snapshot allocation.
- Optimization C: Lazy `seen` map allocation in IsA Get/Has fallback.

After this lands, affected benchmarks measurably improve, and the optimizations are visible in BENCH.md as before/after rows.

### Out of scope (explicit non-goals)

- Defer queue closure-to-tagged-union or sync.Pool refactor.
- Swap-component allocation pattern fix (#5). Investigation first; defer to a follow-up.
- Custom allocators / per-allocator pools globally.
- Concurrent dispatch.
- Inline-helper hand-rolling at the `migrate()` level.
- ROADMAP items that don't show in benchmarks (entityindex FIFO recycle, page-walk Each, per-Progress runPhase snapshot).
- New public API.
- New ECS features or addons.

## Deliverables

### 1. Optimization A: `Field[T]` zero-alloc path

Current `query.go` `Field[T]` body:
```go
rv := tbl.ColumnReflectSlice(id)  // returns reflect.Value of []T
s := rv.Interface().([]T)         // ALLOCATES interface header
return s[:it.Count()]
```

The `rv.Interface()` call boxes the slice into an `any`; the type assertion unboxes it. The interface header is the allocation.

Target: replace with `unsafe.Slice` over the column's base pointer. After a one-time type check, the conversion is zero-alloc.

Implementation:
- Add a new internal method to `internal/storage/table.Table`:
  ```go
  func (t *Table) ColumnBasePtr(id flecs.ID) (unsafe.Pointer, reflect.Type, int)
  ```
  Returns the base pointer of the underlying typed slice (`unsafe.Pointer(rv.Index(0).UnsafeAddr())`), the slice's element type, and the current row count. Returns `(nil, nil, 0)` for tag columns (Size==0). Document pointer stability (invalidated by Append/RemoveSwap).
- Replace `Field[T]` body to use the new method:
  ```go
  base, typ, n := tbl.ColumnBasePtr(id)
  if typ == nil {
      return make([]T, it.Count())  // tag case (unchanged semantics)
  }
  if typ != reflect.TypeFor[T]() {
      panic(\"Field[T]: type mismatch for column ID ...\")
  }
  return unsafe.Slice((*T)(base), n)[:it.Count()]
  ```
- Document GC safety: the column's reflect-backed slice keeps the backing array alive; `unsafe.Slice` over its base pointer creates a typed `[]T` view that the GC traces correctly because the element type `T` is statically known at the call site.
- Retain `ColumnReflectSlice` for any callers (check CachedQuery's iter). If unused after this change, mark with `//deprecated` comment and leave for one cycle.

### 2. Optimization B: Observer dispatch zero-alloc

Current `observer.go` `dispatchObservers`:
```go
active := make([]*observerNode, 0, len(nodes))
for _, n := range nodes { if !n.removed { active = append(active, n) } }
for _, n := range active { n.callback(w, e, ptr) }
```

The `make` allocates per fire. The snapshot exists so an observer callback can call `Unsubscribe` without corrupting iteration — but since `Unsubscribe` only flips a `removed` flag (it doesn't remove from the slice), the snapshot is unnecessary.

Implementation:
- Replace `dispatchObservers` body with a direct iteration:
  ```go
  for _, n := range nodes {
      if n.removed { continue }
      n.callback(w, e, ptr)
  }
  ```
- Critical invariant to preserve: an observer that calls `Unsubscribe()` on itself or on a peer during dispatch — the peer may still fire in the current dispatch (because we're already past it in the loop), or may be skipped (if it's later in the slice and got `removed=true` before we reached it). Document: \"Unsubscribe during dispatch takes effect immediately for not-yet-visited observers; observers already visited in the current dispatch are unaffected.\" This is a subtle semantic change from the snapshot version which guaranteed \"all observers present at dispatch-start fire.\"
- Update or add a test that confirms the chosen semantics. Update godoc on `(o *Observer).Unsubscribe` to clarify.
- Deferred-removal compaction in `addObserverNode` is unchanged.

### 3. Optimization C: Lazy `seen` map in IsA fallback

Current `isa.go` `getViaIsA`:
```go
func getViaIsA[T any](w *World, e ID, seen map[ID]struct{}) (T, bool) {
    // ... seen is allocated by the caller (Get[T]) on every miss
}
```

And in `world.go` `Get[T]`:
```go
// local miss
seen := map[ID]struct{}{e: {}}
return getViaIsA[T](w, e, seen)
```

This allocates the map even when the entity has NO IsA pairs (the common case). The map is only needed when we recurse into a prefab.

Implementation:
- Before allocating `seen`, scan the entity's signature for any `(IsA, *)` pair. If none, return `(zero, false)` directly — no allocation.
- Move the `seen` allocation INSIDE the first prefab branch:
  ```go
  var seen map[ID]struct{}
  for each id in rec.Table.Type() where id.IsPair() && id.First() == w.isAIdx {
      prefab := id.Second()
      if !w.index.IsAlive(prefab) { continue }
      if seen == nil { seen = map[ID]struct{}{e: {}} }
      // ... recursive Get on prefab
  }
  return zero, false
  ```
- Apply the same restructuring to `hasViaIsA` for `Has[T]`/`HasID`.
- Cycle detection invariant: `seen` MUST be allocated before the first recursion. Don't recurse without `seen`.

### 4. Measurements

- Run `go test -bench=. -benchmem -count=5 ./...` on a clean checkout (master AT `e867e2b`) and capture baseline. Re-run after each optimization. Use `benchstat` to compare.
- In BENCH.md, add a new section `## Phase 8.3 results` with before/after rows for ALL affected benchmarks:
  - Field[T]: `BenchmarkFieldT_AllocCost`, `BenchmarkQueryEach2_1k/10k/100k`, `BenchmarkCachedQueryEach2_10k`, `BenchmarkQueryIterField_10k`, `BenchmarkQueryAcrossArchetypes_10k`.
  - Observer: `BenchmarkObserverFires_10k`, `BenchmarkObserverFires_HookAndObserver_10k`, `BenchmarkObserverFires_5observers_10k`.
  - Lazy seen map: `BenchmarkGetExistingComponent`, `BenchmarkHasExistingComponent`, `BenchmarkOwnsVsHas` (the local-miss path).
- Format: `BenchmarkName: before X ns/op, Y allocs -> after Z ns/op, W allocs (-N% / -M allocs)`. Include both percentage and absolute delta where readable.

### 5. Tests — no new behavior tests required. Run the existing suite and ensure:
- All `Field[T]` tests pass — type-mismatch panic, missing-id panic, tag-column slice, GC-pointer survival (the `runtime.GC()` x 2 test). The unsafe.Slice path MUST not break the GC contract.
- All observer tests pass, especially `TestObserverUnsubscribeDuringDispatch` (may need updating if semantics change as documented).
- All IsA tests pass — multi-level, cycles, dead prefab.
- Existing benchmark suite still compiles and runs.

### 6. Mechanical acceptance
- `go test ./... -race -count=2` passes.
- `go vet ./...` clean.
- `golangci-lint run` clean. (`unsafe` use in `Field[T]` and `Column.ColumnBasePtr` may need a lint exemption comment; document.)
- Coverage on `flecs` >= 90% (no regression from 97.2%).
- `go test -bench=. -benchmem ./...` runs to completion.
- BENCH.md updated with before/after numbers.
- All exported symbols have godoc.

## Constraints

- @query.go — `Field[T]` body rewrite to unsafe.Slice path; do NOT change signature.
- @internal/storage/table/table.go — add new `ColumnBasePtr` method returning `(unsafe.Pointer, reflect.Type, int)`; document pointer stability (invalidated by Append/RemoveSwap).
- @internal/storage/table/column.go — potentially expose base pointer if not already.
- @observer.go — rewrite `dispatchObservers` to skip per-fire snapshot; preserve `removed`-flag semantics; document Unsubscribe-during-dispatch behavior in godoc on `(o *Observer).Unsubscribe`.
- @world.go — `Get[T]`/`Has[T]` lazy-seen path; scan for IsA before allocating map.
- @id_ops.go — `HasID` lazy-seen path if it's a separate code path.
- @isa.go — `getViaIsA`, `hasViaIsA` helpers restructured so `seen` allocates only on first recursion; cycle detection MUST remain correct.
- @BENCH.md — add `## Phase 8.3 results` section with before/after rows for all affected benchmarks (formatted as `before X ns/op, Y allocs -> after Z ns/op, W allocs`).
- GC-safety contract for Optimization A: verify with existing `TestTableGCPointerTracing` and `TestFieldGCSafe`. Run with `-race -count=10` for extra confidence.
- Do NOT change Hooks/Observer/Defer public API.
- Do NOT change Query/CachedQuery/QueryIter/Field[T] signatures.
- Do NOT change ChildOf/IsA/Name semantics.
- Do NOT modify the existing `ColumnReflectSlice` signature (may stay or be removed if unused — implementer's call; if unused, mark `//deprecated` and leave for one cycle).
- Do NOT import third-party deps.
- DO comment any new `unsafe` usage explaining why it's correct.
- Not a bug — performance work. Apply `snichols/queued` label only (no `bug`).
