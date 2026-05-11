## Goal

Add a `sync.RWMutex` to `*World` so that read-only consumers (REST handler, dashboards, debuggers, monitoring) can safely access the world concurrently with the goroutine running `Progress`. Multiple concurrent readers proceed in parallel; writers are exclusive.

Phase 10.1 shipped opt-in parallel system dispatch within a phase (mutex-protected defer queue, conservative write-set conflict detection). The next concurrency gap is read-only access from goroutines other than the one running `Progress`. Today the REST handler (Phase 9.6) explicitly documents that reads are unsafe while anything is mutating the world. This phase removes that caveat by adding a world-level RWMutex.

After this lands:

```go
// Game-loop goroutine:
go func() {
    for {
        w.Progress(0.016)  // internally acquires write-lock
    }
}()

// HTTP handler (read-only):
http.HandleFunc(\"/stats\", func(rw http.ResponseWriter, r *http.Request) {
    w.RLock()
    defer w.RUnlock()
    stats := w.Stats()
    json.NewEncoder(rw).Encode(stats)
})

// Multi-reader scenario (REST + Inspector + Metrics):
// All three call RLock concurrently; all proceed; Progress blocks until they finish.
```

### Critical contract

- `Progress(dt)` internally acquires the write-lock around the entire frame.
- All public mutators (`Set`, `Remove`, `Delete`, `AddID`, etc.) acquire the write-lock briefly.
- All public readers (`Get`, `Has`, `Query.Iter`, `CachedQuery.Iter`, etc.) acquire the read-lock briefly.
- Callers can explicitly `RLock`/`RUnlock` to hold the read-lock across multiple operations (atomic snapshot read).
- Re-entrant locking is NOT supported. Calling `Set` from within an `RLock` block panics/deadlocks — cannot upgrade R to W.

### In scope

- `sync.RWMutex` value (not pointer) embedded on `*World`.
- Internal lock acquisition in public mutator/reader API.
- Public `RLock`/`RUnlock`/`Lock`/`Unlock` for caller-driven multi-call atomicity.
- Documented \"cannot upgrade R to W\" rule.
- REST handler updated to acquire RLock/Lock around handler bodies.

### Out of scope (non-goals)

- NO per-table or per-archetype locking.
- NO upgrade from RLock to Lock.
- NO lock-free fast path for reads (wait-free reading deferred).
- NO contention metrics in Stats.
- NO detection of forgotten RUnlock.
- NO change to single-goroutine behavior (existing tests still pass unmodified).
- NO change to the Phase 10.1 defer-queue mutex — it remains independent of the RWMutex.

### Deliverables

1. **New field on `*World`:** `rwmu sync.RWMutex`. NOT a pointer — embedded by value, zero-value usable.

2. **Public lock API:**
   - `func (w *World) RLock()` — wraps `w.rwmu.RLock()`.
   - `func (w *World) RUnlock()` — wraps `w.rwmu.RUnlock()`.
   - `func (w *World) Lock()` — wraps `w.rwmu.Lock()`. Document: rarely needed; only for explicit batched writes.
   - `func (w *World) Unlock()` — wraps `w.rwmu.Unlock()`.
   - Each godoc'd with the cannot-upgrade rule.

3. **Internal lock acquisition** — categorize as RLock vs Lock by whether the function mutates world state:
   - **RLock (pure reads):** `Get`, `Has`, `Owns`, `Count`, `Stats`, `Components`, `EntityComponents`, `EachEntity`, `IsAlive`, `LookupChild`, `Lookup`, `PathOf`, `EachChild`, `ParentOf`, `EachPrefab`, `PrefabOf`, `TablesFor`, `EachTableFor`, `SystemCount`, `SystemCountInPhase`, `MarshalJSON`, `GetByID`, `GetPair`, `GetName`, `ComponentInfo`, `AliveEntities`, `WorkerCount`, `Logger`, `FixedTimestep`, `Time`, `FrameCount`.
   - **Lock (mutations):** `Set[T]`, `Remove[T]`, `RegisterComponent[T]`, `AddID`, `RemoveID`, `SetPair[T]`, `SetPairByID`, `SetByID`, `NewEntity`, `Delete`, `SetName`, `RemoveName`, `UnmarshalJSON`, `Defer`/`DeferBegin`/`DeferEnd`/`IsDeferred`, `Progress(dt)`, `Observe[T]`/`ObserveID`/`Observe2`/`(*Observer).Unsubscribe`, `OnAdd[T]`/`OnSet[T]`/`OnRemove[T]`, `NewSystem`/`NewSystemInPhase`/`(*System).Close`/`SetParallel`/`SetWriteSet`/`SetWorkerCount`/`SetLogger`, `NewQuery`/`NewQueryFromTerms`, `NewCachedQuery`/`NewCachedQueryFromTerms`/`(*CachedQuery).Close`.
   - **Special: `CachedQuery.Changed`** — is logically a read, but the `lastChangeCounts` map IS mutated as a side effect. Treat as Lock for safety.

4. **`Query.Iter` / `CachedQuery.Iter`:** Iteration itself doesn't mutate world state, but `Field[T]` reads table columns. These reads are inherently safe under RLock — but the caller must hold the lock across the entire iteration.
   - **Document explicitly:** \"Holding a `*QueryIter` requires `RLock`/`RUnlock` to be held by the caller across the iteration. The Iter methods do NOT acquire the lock internally; the caller is responsible.\"
   - This is intentional: locking internally would deadlock on `Field[T]` which the user calls directly.

5. **Re-entrancy via `inProgress` flag (Phase 10.1):**
   - If a system in `Progress` (which holds Lock) calls `Set`, that Set would acquire Lock again to deadlock.
   - **Solution:** public mutators check the existing `inProgress` flag. If true, skip lock acquisition (we already hold it). If false, acquire Lock.
   - Non-recursive solution that avoids deadlock.
   - **Caveat to document:** user goroutines calling Set during Progress (without going through a system) would NOT acquire the lock and could race. Document: do NOT call public mutators from goroutines that don't hold the lock; use Defer or system-based mutation.

6. **Lock-from-parallel-systems (Phase 10.1 interaction):**
   - Parallel systems run in worker goroutines while Progress holds Lock.
   - Worker goroutines call Set/etc. — these go through the Phase 10.1 Defer mutex, which is independent of the RWMutex.
   - The Defer queue does NOT need to acquire the RWMutex — Progress holds Lock for the entire frame; parallel-system writes via Defer happen while the world is exclusively owned.
   - `DeferEnd` flush runs under existing Lock (still inside Progress). Good.

7. **REST handler update (`rest.go`):**
   - Each of the 7 handlers acquires `RLock` around its read body.
   - For PUT `/snapshot`, acquire `Lock` around the Unmarshal.
   - Small additive change; verify all 7 handlers updated.

8. **Tests in `concurrent_test.go`:**
   - **Single-goroutine baseline:** existing tests pass unmodified.
   - **Concurrent readers:** 100 goroutines each calling `w.Stats()`; verify no race, no panic.
   - **Reader during Progress:** start Progress (slow sleeping system) in one goroutine; in another, call `w.RLock()` — verify it blocks until Progress finishes.
   - **Writer during reader:** hold `w.RLock` in one goroutine; in another, call `w.Set[T]` — verify it blocks until RUnlock.
   - **Two writers serialize:** two goroutines each calling Set; both succeed eventually; no race.
   - **R to W upgrade deadlocks:** `w.RLock(); w.Set(...)` — verify clear failure mode. Document and recommend \"don't do this.\"
   - **Progress + parallel system + concurrent reader:** Progress with parallel systems writing; concurrent `RLock` from another goroutine — verify blocks until Progress finishes, no race.
   - **REST handler concurrent reads:** Progress loop in one goroutine; concurrent N goroutines hitting `/stats` via `httptest.NewServer` wrapping `NewRESTHandler` — no races, no panics, valid responses.
   - `go test ./... -race -count=10` passes consistently.

9. **Each helpers (`Each1`–`Each4`):**
   - Acquire RLock at the start of the Each function, release at the end.
   - User's `fn` callback runs WITH the lock held — document. User must NOT call mutators from inside the callback (use Defer).

10. **Observe[T] / hook firing:**
    - Observer hooks invoked from inside Progress (which already holds Lock) — do NOT re-acquire.

11. **Documentation:**
    - Godoc on `RLock`/`RUnlock`/`Lock`/`Unlock` covers semantics and upgrade-deadlock pitfall.
    - `doc.go`: new \"Concurrent Access\" section with example.
    - `CHANGELOG.md` entry under Unreleased.
    - `README.md` feature list updated.

12. **Performance:**
    - RWMutex acquisition adds ~10-20 ns per call.
    - Run `BenchmarkSetExistingComponent` and `BenchmarkGetExistingComponent` before/after; document deltas in `BENCH.md`.

13. **Mechanical acceptance:**
    - `go test ./... -race -count=2` passes.
    - `go vet ./...` clean.
    - `golangci-lint run` clean.
    - Coverage on `flecs` >= 90% (no regression from 95.5%).
    - All exported symbols have godoc.

### Implementer pointers

- Use `sync.RWMutex` value, not pointer. Embedded directly on `*World`.
- `inProgress` flag (Phase 10.1, see `world.go:54` and `system.go:230`) is the discriminator for in-Progress mutators to skip locking.
- Hold the lock for the WHOLE duration of public-call critical section, then release. Do NOT hold across user fn callbacks (Each/Observe) — wait, correction: Each/Observe DO run with the lock held; document and forbid mutating from callbacks.
- DO NOT modify any existing function signatures.
- DO NOT import third-party deps (`sync` is stdlib).
- DO NOT add RWMutex to internal/storage types — only `World`.
- `RLock`/`Lock` MUST NOT be called from a system fn (would deadlock since Progress holds Lock). Document.

### C reference

- `/work/agents/claude/projects/SanderMertens/flecs/src/stage.c` — flecs's threading model is more elaborate (per-stage queues); we ship a simpler model.

## Constraints

- @world.go — `*World` struct; add `rwmu sync.RWMutex` field; `inProgress` flag lives here (line 54); existing exported methods need lock acquisition wrapping.
- @system.go — `Progress(dt)` acquires write-lock around the frame; `inProgress` set/cleared at lines 230–231 remains the discriminator; `NewSystem`/`SetParallel`/`SetWriteSet`/`SetWorkerCount` acquire Lock.
- @id_ops.go — `AddID`/`RemoveID`/`HasID`/`OwnsID`/`SetByID`/`SetPairByID`/`GetByID` need lock wrapping per the read/write categorization.
- @value_ops.go — generic `Set[T]`/`Get[T]`/`Has[T]`/`Owns[T]`/`Remove[T]`/`SetPair[T]`/`GetPair[T]`/`RegisterComponent[T]` need lock wrapping.
- @defer.go — `Defer`/`DeferBegin`/`DeferEnd`/`IsDeferred` acquire Lock; Phase 10.1 defer-queue mutex remains independent.
- @query.go — `NewQuery`/`NewQueryFromTerms` acquire Lock for registration; `Query.Iter` does NOT acquire — caller responsibility; document.
- @cached_query.go — `NewCachedQuery`/`Close` acquire Lock; `Changed` treated as Lock due to `lastChangeCounts` mutation; `Iter` is caller-managed.
- @hooks.go — `OnAdd[T]`/`OnSet[T]`/`OnRemove[T]` acquire Lock; hook firing inside Progress does NOT re-acquire.
- @observer.go — `Observe[T]`/`ObserveID`/`Observe2`/`(*Observer).Unsubscribe` acquire Lock.
- @stats.go — `Stats`/`SystemCount`/`SystemCountInPhase`/`WorkerCount` acquire RLock.
- @meta.go — `Components`/`ComponentInfo`/`EntityComponents`/`EachEntity`/`AliveEntities` acquire RLock.
- @rest.go — 7 handlers updated: 6 readers acquire RLock around handler body; PUT `/snapshot` acquires Lock around Unmarshal.
- @name.go — `SetName` Lock; `GetName`/`Lookup`/`LookupChild`/`PathOf`/`RemoveName` RLock for reads, Lock for mutators.
- @childof.go — `EachChild`/`ParentOf` acquire RLock.
- @isa.go — `EachPrefab`/`PrefabOf` acquire RLock.
- @marshal.go — `MarshalJSON` RLock; `UnmarshalJSON` Lock.
- @traversal.go — `TablesFor`/`EachTableFor` acquire RLock.
- @each.go — `Each1`–`Each4` acquire RLock for full duration; user callback runs under lock; document forbidding mutation from callback (use Defer).
- @doc.go — add \"Concurrent Access\" section with example showing RLock/RUnlock around Stats and the upgrade-deadlock pitfall.
- @CHANGELOG.md — Unreleased entry for Phase 10.2 RWMutex.
- @README.md — feature list updated with concurrent-read capability.
