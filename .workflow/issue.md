## Goal

Phase 8.2 of the Go port of flecs: establish a comprehensive benchmark suite, reproducible measurement methodology, and documented baseline numbers so Phase 8.3 can target real hotspots based on data rather than speculation.

The ECS has been feature-complete since Phase 7.3 and Phase 8.1 just landed comprehensive documentation. `ROADMAP.md` flags performance work as deferred and specifically calls out `Field[T]` currently using `reflect` as a known optimization target. The accumulated follow-ups list across many phases includes:

- Recycle list as FIFO slice vs C dense-tail (entityindex)
- `Each()` in entityindex re-derives page lookup per iter
- `Get`/`Has` unconditional `seen` map alloc on local miss (isa path)
- Observer dispatch allocates snapshot slice on every fire
- Per-pair-id `TypeInfo` copies scale O(unique pairs)
- `Field[T]` reflect path vs `unsafe.Slice`
- Per-`Progress` `runPhase` allocates active-systems snapshot

None of these have known performance numbers. Phase 8.2 establishes the baselines.

After this lands, anyone can run `go test -bench=. -benchmem ./...` and get a complete picture of:

- Per-operation latency (ns/op).
- Per-operation allocations (allocs/op, B/op).
- Hot-loop throughput (entities/sec via `Each<N>` iteration over realistic working sets).
- Hook + observer dispatch overhead.
- Deferred queue overhead.
- System `Progress` throughput at varying scale.

### Deliverables

**1. New file `bench_test.go`** in root `flecs` package. Optionally split into `bench_storage_test.go`, `bench_query_test.go`, `bench_system_test.go` if size warrants — implementer's call. One file is fine for v0.

**2. Benchmark categories with specific tests:**

**a) Entity lifecycle (no components):**
- `BenchmarkNewEntity` — `b.N` calls to `w.NewEntity()`. Measure ns/op + allocs/op.
- `BenchmarkDeleteEntity` — pre-create entities; `b.ResetTimer`; delete. Reset between sub-benchmarks via `b.StopTimer/StartTimer` if needed.
- `BenchmarkAllocFreeAllocCycle` — Alloc+Free+Alloc to measure recycle path.

**b) Component Set/Get/Has (single component):**
- `BenchmarkSetExistingComponent` — entity already has Position; re-set. (Fast path, no migration.)
- `BenchmarkSetNewComponentTriggerMigration` — entity does NOT have Position; Set triggers archetype migration.
- `BenchmarkGetExistingComponent` — entity has Position; Get.
- `BenchmarkGetMissingComponent` — entity doesn't have Position; Get returns false.
- `BenchmarkHasExistingComponent`.
- `BenchmarkOwnsVsHas` — comparative bench against an entity with `(IsA, prefab)`: Has walks inheritance, Owns doesn't.

**c) Archetype migration (the hot path):**
- `BenchmarkAddOneComponent_CacheHit` — entity migrates from [] to [Position] via Set; runs many times via different entities; the destination table's edge is cached after first call.
- `BenchmarkAddOneComponent_CacheMiss` — like above but ensure cache miss each time by varying the source table (use multiple distinct entity setups). This is hard to test cleanly; document the methodology.
- `BenchmarkRemoveOneComponent`.
- `BenchmarkSwapComponent` — entity goes from [Position, Velocity] -> [Position] (remove Velocity).

**d) Query iteration (the hottest path):**
- `BenchmarkQueryEach2_1k` — 1,000 entities with [Position, Velocity]; iterate via `Each2[Position, Velocity]`, modifying both. Measure ns/op.
- `BenchmarkQueryEach2_10k` — 10,000 entities. Throughput should be linear in entity count.
- `BenchmarkQueryEach2_100k` — 100,000 entities. Tests larger working sets.
- `BenchmarkCachedQueryEach2_10k` — same but via `*CachedQuery`. Compare against uncached.
- `BenchmarkQueryIterField_10k` — manual `NewQuery -> Iter -> Next -> Field[T]` loop (matches the docs idiom). Compare with Each2 to see closure overhead.
- `BenchmarkQueryAcrossArchetypes_10k` — 10,000 entities split across 5 archetypes that ALL match `(Position, Velocity)` (i.e., they have extra tags); iterate. Tests multi-table iteration overhead.
- `BenchmarkFieldT_AllocCost` — measure `Field[T]` alone, isolating the reflect+Interface() conversion cost. Use `b.ReportAllocs()`.

**e) Hooks + Observers:**
- `BenchmarkOnSetHookFires_10k` — register OnSet hook; Set on 10,000 entities. Compare with `BenchmarkSetNoHook_10k` (no hook registered) to isolate hook dispatch cost.
- `BenchmarkObserverFires_10k` — register one Observer for EventOnSet; Set 10k times.
- `BenchmarkObserverFires_HookAndObserver_10k` — both hook AND observer registered.
- `BenchmarkObserverFires_5observers_10k` — 5 observers stacked. Tests linear dispatch overhead.

**f) Deferred queue:**
- `BenchmarkDeferOverhead_NoOps` — `Defer(func() {})` with no operations inside. Pure overhead.
- `BenchmarkDeferSet_10k` — 10k deferred Sets inside one Defer block, then flush.
- `BenchmarkDeferDelete_10k`.
- `BenchmarkDeferNested` — nested DeferBegin/End cost.

**g) Systems + Progress:**
- `BenchmarkProgress_NoSystems` — empty Progress(dt). Measures the per-frame overhead.
- `BenchmarkProgress_OneSystem_10kEntities` — one OnUpdate system iterating 10k entities.
- `BenchmarkProgress_TenSystems_1kEntities` — 10 systems, each iterating 1k entities.
- `BenchmarkProgress_WithFixedTimestep` — same as above but with `SetFixedTimestep(1/60)`; Progress(1/60). Measures fixed-phase Defer overhead.
- `BenchmarkProgress_PipelineFull` — systems in all 4 phases (Pre, OnFixed, On, Post), 1k entities each.

**h) ChildOf / IsA hierarchies:**
- `BenchmarkChildOfCascadeDelete_100` — delete a parent with 100 children.
- `BenchmarkChildOfCascadeDelete_10k` — delete a parent with 10,000 children (stress test).
- `BenchmarkIsAGet_Hit` — Get on a child where the prefab has Position; one IsA hop.
- `BenchmarkIsAGet_MissedChain` — Get on a child with `(IsA, prefab)` where prefab doesn't have Position; falls through.
- `BenchmarkLookupPath_3deep` — `w.Lookup(\"scene.car.wheel\")` over a populated tree.

**3. Common benchmark setup helpers** (in `bench_test.go`):
- `setupWorldWithEntities(b, n int, components ...flecs.ID) (*flecs.World, []flecs.ID)` — pre-create n entities in a single archetype.
- `setupWorldMultiArchetype(b, n int, archetypes int) (*flecs.World, []flecs.ID)` — n entities split evenly across archetypes for query benchmarks.
- `resetMemstats(b)` (optional helper to call `runtime.GC()` + reset timer if memory churn between iterations is a concern).
- Helpers use `b.Helper()`.

**4. Allocation reporting:** every benchmark calls `b.ReportAllocs()` at the top. We care about per-op allocations.

**5. Benchmark naming convention:** `BenchmarkAreaSpecificCase_Scale` — e.g., `BenchmarkQueryEach2_10k`. `Scale` is omitted for non-scale-dependent benchmarks. Use camelCase.

**6. Documentation in `BENCH.md` (new):**
- How to run: `go test -bench=. -benchmem ./...`
- How to compare runs: `go test -bench=. -benchmem -count=10 ./... > baseline.txt`; mention `benchstat` for comparison.
- The list of benchmarks with one-line descriptions (auto-generated would be nice but a hand-written list of categories is fine).
- **Baseline numbers as of master `e2cc8ee`** — captured on the implementer's machine. Format:
  ```
  ## Baseline (Go 1.26.1, Linux amd64, [CPU model], [date])

  BenchmarkNewEntity-N                    XXX ns/op    YY B/op    Z allocs/op
  ...
  ```
- Note that absolute numbers vary by hardware; the value is in relative comparison.
- Implementer should run `go test -bench=. -benchmem -count=5 ./... > /tmp/bench.txt` and paste the trimmed output (median or best run) into BENCH.md.

**7. CI integration:** add a `bench` job to `.github/workflows/ci.yml` that runs `go test -bench=. -benchmem -benchtime=100ms ./...` (short benchtime to keep CI fast). This is a smoke test that benchmarks compile and run without panics; the numbers from CI are not authoritative (the runner varies). Document this in `CONTRIBUTING.md`.

**8. Tests / Mechanical acceptance:**
- `go test ./... -race -count=2` passes (no regression).
- `go test -bench=. -benchmem ./...` runs to completion without panic.
- `go vet ./...` clean.
- `golangci-lint run` clean.
- Coverage on `flecs` >= 90% (no regression from 97.2%; benchmarks generally don't count toward coverage but the helpers might).
- `BENCH.md` exists with at least 30 benchmark entries.

### Non-goals

- NO performance fixes. Phase 8.3 picks targets based on these numbers.
- NO comparison against flecs C numbers (different language, different methodology — unfair).
- NO third-party benchmark frameworks (use stdlib `testing.B`).
- NO continuous-benchmarking infrastructure / regression tracking (just one-shot baseline numbers in `BENCH.md`).
- NO new ECS features.
- NO changes to existing public API.

## Constraints

- Use `b.ReportAllocs()` on every benchmark.
- Use `b.ResetTimer()` after setup; use `b.StopTimer()/StartTimer()` if measurement needs to skip per-iteration setup.
- For \"cache miss\" benchmarks, document the methodology clearly — Go's benchmark framework doesn't natively support cache-state control.
- DO NOT use `time.Sleep` or any wall-clock dependent operation inside benchmarks.
- DO NOT print to stdout from benchmark bodies (it skews timing).
- DO NOT modify any non-test source. Benchmarks consume the public API only.
- Initial entity creation in setup should NOT be timed. Use `b.ResetTimer()` after creating entities.
- DO NOT import any third-party deps.
- The `Position`/`Velocity` types used in tests already exist in `*_test.go` files; reuse them in benchmarks (declare locally with `bench` prefix if naming collisions arise).
- @example_basic_test.go — Position/Velocity component types in scope; reuse where possible.
- @example_query_test.go — iteration patterns (Each2, NewQuery/Iter/Next/Field) to benchmark.
- @example_pipeline_test.go — system + Progress patterns to benchmark.
- @CONTRIBUTING.md — update with bench instructions (how to run, CI smoke-test note).
- @.github/workflows/ci.yml — add `bench` job with `-benchtime=100ms`.
- @ROADMAP.md — Phase 8.2 marked done (or leave; implementer's call).

This is feature/measurement work, not a bug. Apply only `snichols/queued`.
