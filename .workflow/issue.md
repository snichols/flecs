## Goal

Replace the bare-`*World` mutator API with a scoped capability pattern: `world.Read(fn func(*Reader))` and `world.Write(fn func(*Writer))`. Today, `flecs.Set[T](w, e, v)`, `w.AddID(e, id)`, `w.Defer(fn)`, and `w.Readonly(fn)` all take a `*World`, conflating reads, writes, defer-scope state, and exclusive-access claims into one type. The new design separates these concerns, gets the type system to enforce read-vs-write at compile time, makes defer scopes lexical, and composes naturally with the per-goroutine stages Phase 12.1 will port from C flecs.

This is a deliberate breaking change. There are no external users; pre-1.0; clean break with no backward-compat shim. The existing `Defer*` / `Readonly*` public API is removed entirely.

### Types

```go
// Reader is a scoped capability for reading committed world state.
// Obtained via world.Read(fn). Invalid outside the callback.
type Reader struct {
    world *World
}

// Writer is a scoped capability for reading AND writing.
// Obtained via world.Write(fn), or it.Writer() inside a system fn.
// A Writer is-a Reader.
type Writer struct {
    Reader
    // stage *Stage  // Phase 12.1 will add this; in 12.0 Writer is internally single-staged.
}
```

### World entry points

```go
// Read opens a read-only capability scope. Concurrent calls from multiple
// goroutines are allowed; writes from other goroutines block until all
// active Read callbacks return. Internally backed by an RWMutex RLock.
func (w *World) Read(fn func(*Reader))

// Write opens a read/write capability scope. Claims exclusive access for
// the duration of fn. All structural mutations are queued in a defer scope
// and flushed when fn returns. Integrates with ExclusiveAccessBegin/End:
// nested Write calls from the same goroutine nest the defer scope;
// concurrent Write from a different goroutine panics with
// ErrExclusiveAccessViolation.
func (w *World) Write(fn func(*Writer))
```

### Free-function mutators (replace today's `flecs.Set[T](w, ...)`)

```go
// Reads — operate on a Reader. *Writer satisfies *Reader (via embedding),
// so these also work inside a Write scope.
func Get[T any](r *Reader, e ID) (T, bool)        // returns COPY; false if absent
func GetRef[T any](r *Reader, e ID) *T            // pointer into storage; nil if absent
func Has[T any](r *Reader, e ID) bool
func Owns[T any](r *Reader, e ID) bool
func GetPair[T any](r *Reader, e ID, rel, tgt ID) (T, bool)
func GetPairRef[T any](r *Reader, e ID, rel, tgt ID) *T

// Writes — operate on a Writer.
func Set[T any](w *Writer, e ID, v T)
func Remove[T any](w *Writer, e ID)
func SetPair[T any](w *Writer, e ID, rel, tgt ID, v T)
```

### Method-based API partition

- **On `*World`** (lifecycle, setup-time): `RegisterComponent[T]`, `NewSystem`, `NewSystemInPhase`, `NewQuery`, `NewQueryFromTerms`, `NewCachedQuery`, `NewCachedQueryFromTerms`, `SetWorkerCount`, `SetFixedTimestep`, `SetLogger`, `MarshalJSON`, `UnmarshalJSON`, `Progress`, `Stats`, `ExclusiveAccessBegin`, `ExclusiveAccessEnd`, `Read`, `Write`.
- **On `*Reader`**: `IsAlive`, `HasID`, `OwnsID`, `ParentOf`, `EachChild`, `PrefabOf`, `EachPrefab`, `Lookup`, `LookupChild`, `PathOf`, `GetName`, `Components`, `ComponentInfo`, `EntityComponents`, `EachEntity`, `AliveEntities`, `Count`, `SystemCount`, `SystemCountInPhase`, `TablesFor`, `EachTableFor`.
- **On `*Writer`**: `NewEntity`, `AddID`, `RemoveID`, `Delete`, `SetName`, `SetByID`, `SetPairByID`.

(`NewEntity` goes on Writer since it allocates and writes entity state. Confirm during implementation.)

### Hook + Observer signature change

```go
// Before:
flecs.OnSet[Position](w, func(w *World, e ID, v Position) { ... })
flecs.Observe[Position](w, func(w *World, e ID, v Position) { ... })

// After:
flecs.OnSet[Position](w, func(fw *Writer, e ID, v Position) { ... })
flecs.Observe[Position](w, func(fw *Writer, e ID, v Position) { ... })
```

Hooks are inherently re-entrant — the new signature makes that explicit.

### Iter exposes Reader/Writer

```go
func (it *QueryIter) Reader() *Reader  // backed by it.world
func (it *QueryIter) Writer() *Writer  // backed by it.world for now (12.1 will bind to it.stage)
```

System fn signature unchanged: `func(dt float32, it *QueryIter)`. User code uses `it.Writer()` for mutations.

### Defer / Readonly retirement

The existing `DeferBegin`, `DeferEnd`, `Defer(fn)`, `ReadonlyBegin`, `ReadonlyEnd`, `Readonly(fn)` public API is REPLACED entirely by `Read(fn)` / `Write(fn)`. The internal `cmdQueue` machinery (Phase 11.0) stays and becomes the implementation of Write's defer scope.

### REST handlers + Marshal/Unmarshal

GET handlers wrap bodies in `w.Read(func(fr) { ... })`. POST/PUT (snapshot load) uses `w.Write(func(fw) { ... })`. `(*World).MarshalJSON()` internally opens a Read scope; `(*World).UnmarshalJSON(b)` opens a Write scope. The public `json.Marshaler` / `json.Unmarshaler` interface is preserved.

### Locking semantics

- `Read(fn)`: RLock the world's RWMutex; call fn; RUnlock.
- `Write(fn)`: Lock the world's RWMutex; call ExclusiveAccessBegin; increment deferDepth; call fn; decrement deferDepth; on 0, flush queue; ExclusiveAccessEnd; Unlock.
- Nested `Write` from the same goroutine: outer Lock is already held; inner increments deferDepth and runs fn; only outer flushes.
- `Write` while exclusive-access is held by a different goroutine: **panics** with `ErrExclusiveAccessViolation` (matches existing Phase 10.3 semantics).

## Deliverables

1. **`scope.go`** (NEW): `Reader`, `Writer` types. Doc-comments explaining the scoped-capability pattern and the C-flecs polymorphism it replaces.
2. **`world.go`**: add `Read(fn)` and `Write(fn)`; remove old `Defer*` / `Readonly*` methods; move entity-level mutators/readers to Writer/Reader; keep lifecycle/setup on `*World`; add `sync.RWMutex` field.
3. **Free functions**: rewrite signatures across `value_ops.go`, `id_ops.go`, etc. `Get[T]` now returns `(T, bool)`; new `GetRef[T]` returns `*T` (scope-bounded pointer, nil if absent).
4. **Hook signature change** (`hooks.go`): every hook signature takes `*Writer` instead of `*World`.
5. **Observer signature change** (`observer.go`): same.
6. **Iter** (`query.go`): add `Reader()` and `Writer()` methods on `*QueryIter`. System dispatch constructs these once per iter and reuses.
7. **REST handlers** (`rest.go`): rewrite using `w.Read` / `w.Write`.
8. **Marshal/Unmarshal** (`marshal.go`): internally open Read/Write scopes.
9. **Defer/Readonly API removal**: delete public API from `defer.go`; delete `readonly.go` entirely.
10. **Migration of all tests**: every `*_test.go` updated to the new shape. Particular attention to deferred-semantics tests (`TestDeferWrappedIteration`, `TestDeferCoalescesAddsToOneMigration`, v0.14.0 coalescer tests).
11. **Migration of all examples and `cmd/` programs**.
12. **Concurrency tests** in `scope_test.go`:
    - `TestReadAllowsConcurrentReaders`
    - `TestWriteSerializesWithReaders`
    - `TestWriteFromOtherGoroutinePanicsWhenClaimed`
    - `TestNestedWriteSharesScope`
    - `TestGetRefValidInsideScopeOnly`
    - `TestHookReceivesWriter`
13. **`doc.go`**: rewrite the package overview; "Concurrency model" section describes Read/Write scopes; add the explicit "modeled on C flecs's polymorphic `ecs_world_t*`" note.
14. **`README.md`**: rewrite the "first program" example to use `Write`; update the Concurrency model section.
15. **`CHANGELOG.md`** Unreleased v0.15.0: lead with the breaking-change announcement; detailed migration guide.
16. **`ROADMAP.md`**: move "Reader/Writer scoped mutation API" entry into Shipped (v0.15) on release.

## Acceptance

- `go test ./... -race -count=10` clean.
- `go vet ./...` + `golangci-lint run` clean.
- Coverage ≥ 95%.
- All v0.14.0 coalescer tests pass under the new API (mechanically migrated).
- `BenchmarkSetExistingComponent` (wrapped in `w.Write`): regression ≤ 5% vs v0.14.0.
- `BenchmarkDeferBatchedAdds` (100 AddIDs inside one Write block): same 16× speedup as v0.14.0; same 0 allocs.
- No public references to `Defer`, `DeferBegin`, `DeferEnd`, `Readonly`, `ReadonlyBegin`, `ReadonlyEnd` anywhere in the package.
- `flecs.Set` / `flecs.Get` etc. require `*Writer` / `*Reader` parameters; passing `*World` directly is a compile error.
- All examples, tests, REST handlers, marshal code use the new API.

## Non-deliverables (Phase 12.1)

- Per-goroutine stages. Writer is internally bound to the world's single defer queue in this phase. Phase 12.1 will add a `stage *Stage` field on Writer and bind iter Writers to per-worker stages.

## Constraints

- @world.go — major edits: add Read/Write, RWMutex; move entity mutators to Writer / readers to Reader; keep lifecycle on World
- @scope.go — NEW: Reader, Writer types; doc-comments explaining the scoped-capability pattern
- @defer.go — DELETE public API (DeferBegin/DeferEnd/Defer); keep internal cmdQueue machinery used by Write
- @readonly.go — DELETE entirely; replaced by Read(fn)
- @id_ops.go — mutator/reader signature rewrites (AddID/RemoveID move to Writer; HasID/OwnsID move to Reader)
- @value_ops.go — `flecs.Set[T]` takes `*Writer`; `flecs.Get[T]` takes `*Reader` and returns `(T, bool)`; new `GetRef[T]` returns `*T`; same for pair variants
- @name.go — SetName moves to Writer; GetName/Lookup/PathOf move to Reader
- @childof.go — ParentOf/EachChild move to Reader
- @isa.go — PrefabOf/EachPrefab move to Reader
- @hooks.go — every hook signature takes `*Writer` instead of `*World`
- @observer.go — observer callback signature takes `*Writer`
- @query.go — add `Reader()` and `Writer()` methods on `*QueryIter`
- @cached_query.go — same iter-accessor exposure
- @rest.go — GET handlers wrap body in `w.Read`; POST/PUT in `w.Write`
- @marshal.go — MarshalJSON opens Read scope internally; UnmarshalJSON opens Write
- @meta.go — Components/ComponentInfo/EntityComponents move to Reader
- @stats.go — readers move to Reader (where they read live state); Stats stays on World as a snapshot accessor
- @traversal.go — traversal readers move to Reader
- @system.go — system fn signature unchanged externally; internal dispatch exposes Reader/Writer on iter
- @doc.go — rewrite Concurrency model section; remove old deferred-queue narrative or rewrite as Write's internal mechanism; add the C-flecs polymorphism note
- @README.md — rewrite first-program example to use Write; update Concurrency model section
- @CHANGELOG.md — Unreleased v0.15.0 entry leads with breaking-change banner + migration guide
- @ROADMAP.md — move entry to Shipped (v0.15) on release
- @scope_test.go — NEW: concurrency tests (concurrent readers; reader-vs-writer serialization; cross-goroutine Write panic; nested Write scope; GetRef-inside-scope; hook receives Writer)
- /work/agents/claude/projects/SanderMertens/flecs/src/world.c lines 361-377 — `flecs_stage_from_world` is the C polymorphism this redesign replaces with explicit scoping
- /work/agents/claude/projects/SanderMertens/flecs/include/flecs.h lines 1170-1173 — `ecs_iter_t.world` carries a world-shaped pointer that's actually the stage; in Go we make that capability explicit via Reader/Writer
- /work/agents/claude/projects/SanderMertens/flecs/src/stage.c lines 295-339 — `ecs_readonly_begin`/`end` is the existing C readonly window — replaced by Read(fn)
- Pre-1.0, no external users: breaking change is intentional; no backward-compat shim
- Locking semantics: Read uses RWMutex.RLock; Write uses RWMutex.Lock + ExclusiveAccessBegin/End + nested deferDepth; concurrent Write from different goroutine PANICS (not blocks) to match Phase 10.3 ErrExclusiveAccessViolation

