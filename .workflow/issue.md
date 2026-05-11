## Background

Phase 12.0 (#93, commit `2dd727d`) introduced the `Reader`/`Writer` scoped-capability API and removed the legacy `Defer` / `Readonly` public surface. The intent: make the type system enforce scope discipline so that every entity mutation goes through `world.Write(func(*Writer) { ... })` and every read through `world.Read(func(*Reader) { ... })`.

Code review of the landing identified three deviations from the #93 spec:

1. Two undocumented escape-hatches (`World.W()` / `World.R()`) that return cached capability pointers without acquiring any lock or claiming exclusive access — re-introducing bare-world mutation in disguise.
2. `World.NewEntity` was retained even though the #93 spec said `NewEntity` belongs on `*Writer` only.
3. Nearly every test and example file now uses `w.W()` / `w.R()` as the de-facto idiom — about 1,000 grep hits across 41 files. New users will copy this.

There are no external users; breaking is fine; no migration guide needed for external consumers. This issue closes the migration before tagging v0.15.0.

## Deliverables

### 1. Delete `World.W()` and `World.R()` from `@world.go`

Lines 227 and 232 (verify exact lines before editing):

```go
func (w *World) W() *Writer { return &w.writeCapability }
func (w *World) R() *Reader { return &w.readCapability }
```

These were added during 12.0 implementation but were not part of #93's spec. They hand out cached `*Writer`/`*Reader` pointers without acquiring any lock, without entering a scope, and without claiming exclusive access. They defeat the entire point of the migration.

### 2. Delete `World.NewEntity` from `@world.go`

Line 236 (verify):

```go
func (w *World) NewEntity() ID {
    w.checkExclusiveAccessWrite()
    return w.newEntityInternal()
}
```

The #93 spec said `NewEntity` belongs on `*Writer` only. The landing kept both. Drop the `*World` version. `*Writer.NewEntity` already exists in `@scope.go` (line 226). Keep `newEntityInternal` — it is used by `*Writer.NewEntity` and by setup code paths.

### 3. Audit `*World` for any remaining entity-state methods

Enumerate every public method on `*World` and classify each:

- **(a) entity-state read/write** — must move to `Reader`/`Writer` or already moved.
- **(b) lifecycle/setup/process-level** — stays on `World`. This category includes `Progress`, `SetWorkerCount`, `WorkerCount`, `SetLogger`, `Logger`, `SetFixedTimestep`, `FixedTimestep`, `Time`, `FrameCount`, `MarshalJSON`, `UnmarshalJSON`, `RegisterComponent`, `NewSystem`, `NewQuery`, `Read`, `Write`, the phase accessors (`PreUpdate`, `OnUpdate`, `PostUpdate`, `OnFixedUpdate`), etc.

Notes:
- `RegisterComponent` is process-level and stays on `World`.
- `MarshalJSON` / `UnmarshalJSON` already open scopes internally and stay on `World`.
- Currently-public `*World` methods to scrutinise (from `grep -n 'func (w \*World)' world.go`): `Delete`, `IsAlive`, `Count`, `TablesFor`, `EachTableFor`. Each must be evaluated: if it touches entity state, it belongs on `Reader` or `Writer` (and may already have an equivalent there — see `@scope.go`). Document findings in the PR description, including any that intentionally remain dual.

### 4. Migrate every call site that uses the escape-hatches

The reviewer counted ~1,000 grep hits (most are `w.W()` repeated in setup blocks; collapsing into a single `Write` scope per test reduces the count substantially). Files involved (verified to exist):

**Tests:** `@parallel_test.go`, `@hooks_test.go`, `@observer_test.go`, `@scope_test.go`, `@each_test.go`, `@value_ops_test.go`, `@cached_query_test.go`, `@change_detection_test.go`, `@childof_test.go`, `@concurrent_test.go`, `@exclusive_access_test.go`, `@isa_test.go`, `@log_test.go`, `@marshal_test.go`, `@meta_test.go`, `@name_test.go`, `@pair_test.go`, `@pipeline_test.go`, `@query_terms_test.go`, `@query_test.go`, `@rest_test.go`, `@stats_test.go`, `@system_parallel_test.go`, `@system_test.go`, `@timer_test.go`, `@traversal_test.go`, `@world_test.go`, `@defer_test.go`, `@defer_coalesce_test.go`, `@bench_test.go`.

**Examples:** `@example_basic_test.go`, `@example_childof_test.go`, `@example_defer_test.go`, `@example_hooks_test.go`, `@example_isa_test.go`, `@example_name_test.go`, `@example_observer_test.go`, `@example_pairs_test.go`, `@example_pipeline_test.go`, `@example_query_test.go`, `@example_rest_test.go`.

**Doc comment:** `@doc.go` line 133 contains `flecs.Each1[Position](w.R(), ...)` — replace with a `world.Read` example.

Search recipe:

```
grep -rEn '\bw\.W\(\)|\bw\.R\(\)|world\.W\(\)|world\.R\(\)' --include='*.go'
```

(Use word boundaries to avoid colliding with other receivers; `t.W()` / `t.R()` from `testing.TB` etc. should not exist but verify before bulk-rewriting.)

Every call site rewrites to use `world.Read(func(r *flecs.Reader) { ... })` or `world.Write(func(w *flecs.Writer) { ... })`. Setup code that is purely `RegisterComponent` and system creation (no entity mutation) does not need scoping. Calls to `w.NewEntity()` migrate to `world.Write(func(fw *flecs.Writer) { e := fw.NewEntity(); ... })`.

### 5. Fix stale test names in `@CHANGELOG.md`

Lines 112–116 of `@CHANGELOG.md` reference tests that were deleted when `readonly.go` was removed:

- `TestReadonlyAllowsConcurrentReaders`
- `TestReadonlyEnqueuesWrites`
- `TestReadonlyDeleteEnqueued`
- `TestReadonlyNestedWithDefer`
- `TestReadonlyIsDeferred`
- `TestReadonlyEndPanicsWithoutBegin`
- `TestReadonlyNestedDepthPreservation`

Replace with the actual tests now in `@scope_test.go`. Confirmed-existing names to draw from:

- `TestReadAllowsConcurrentReaders`
- `TestWriteSerializesWithReaders`
- `TestWriteFromOtherGoroutinePanicsWhenClaimed`
- `TestNestedWriteSharesScope`
- `TestGetRefValidInsideScopeOnly`
- `TestWriteNestedFromSameGoroutine`
- `TestWritePanicsWhenClaimedByOtherGoroutine`
- `TestHookReceivesWriter`
- `TestReaderMethods`
- `TestWriterMethods`

Pick the subset that best preserves the changelog's original intent (concurrent readers, write enqueue, nested scopes, end-without-begin panic, depth preservation). Also remove the bullet (around line 98–99) advertising `world.W()` / `world.R()` as convenience accessors — they are being deleted.

## Non-goals

- No new functionality.
- No performance changes.
- Do not touch `*Writer.NewEntity` or any other `@scope.go` API.
- Do not change hook/observer signatures (those are final per #93).
- Do not add new public methods to `*World`.

## Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run ./...` clean.
- `go test ./... -race -count=3 -timeout=180s` passes.
- Coverage on the main package ≥ 95% (`go test -cover`).
- `grep -rEn '\bw\.W\(\)|\bw\.R\(\)|world\.W\(\)|world\.R\(\)' --include='*.go'` returns no matches.
- No new public methods on `*World` beyond what existed before this issue.
- `@CHANGELOG.md` Unreleased / v0.15.0 section has a new entry documenting that `W()`, `R()`, and `NewEntity` were removed from `*World` as part of finishing 12.0.

## Relevant files

- `@world.go` — `W`, `R`, `NewEntity` to delete (lines 227, 232, 236).
- `@scope.go` — no changes; reference only (`*Writer.NewEntity` at line 226).
- `@CHANGELOG.md` — stale test names (lines 112–116) plus removing the W/R accessor bullet plus the new finishing-pass entry.
- `@doc.go` — doc example at line 133 references `w.R()`.
- All `*_test.go` and `example_*_test.go` files in repo root — migration.
