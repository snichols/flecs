# Benchmark Guide

## Running benchmarks

```sh
# Run all benchmarks (one pass)
go test -bench=. -benchmem ./...

# Capture a baseline for later comparison (10 runs for statistical stability)
go test -bench=. -benchmem -count=10 ./... > baseline.txt

# Compare two baselines with benchstat (install once: go install golang.org/x/perf/cmd/benchstat@latest)
benchstat before.txt after.txt
```

The `-benchmem` flag emits `B/op` and `allocs/op` per operation alongside `ns/op`.

## CI smoke test

The CI workflow runs benchmarks with `-benchtime=100ms` to verify they compile
and execute without panics. CI numbers are NOT authoritative — the runner
hardware varies. Use a dedicated machine for reproducible measurements.
See `.github/workflows/ci.yml` (`bench` job) and `CONTRIBUTING.md`.

## Benchmark index

### a) Entity lifecycle

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkNewEntity` | Single `w.NewEntity()` call; measures ns/op + allocs/op |
| `BenchmarkDeleteEntity` | Delete pre-created entities; timer reset after setup |
| `BenchmarkAllocFreeAllocCycle` | Alloc → Free → Alloc; exercises the entity recycle path |

### b) Component Set/Get/Has

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkSetExistingComponent` | Re-set Position on an entity that already has it (fast path, no migration) |
| `BenchmarkSetNewComponentTriggerMigration` | Set Position on entity without it; triggers archetype migration |
| `BenchmarkGetExistingComponent` | `Get[Position]` hit |
| `BenchmarkGetMissingComponent` | `Get[Position]` miss |
| `BenchmarkHasExistingComponent` | `Has[Position]` on entity that owns it |
| `BenchmarkOwnsVsHas/Has-via-IsA` | `HasID` on a child that inherits via `(IsA, prefab)` |
| `BenchmarkOwnsVsHas/Owns-local-only` | `OwnsID` on same child; local-only, no chain walk |

### c) Archetype migration

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkAddOneComponent_CacheHit` | All entities start from `[]`; destination edge cached after first |
| `BenchmarkAddOneComponent_CacheMiss` | Source archetype varies per entity; exercises cold-edge paths |
| `BenchmarkRemoveOneComponent` | Remove Position from entities that own it |
| `BenchmarkSwapComponent` | `[Position, Velocity]` → `[Position]` via Remove |

### d) Query iteration

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkQueryEach2_1k` | Each2 over 1,000 entities with Position+Velocity |
| `BenchmarkQueryEach2_10k` | Each2 over 10,000 entities |
| `BenchmarkQueryEach2_100k` | Each2 over 100,000 entities; tests large working sets |
| `BenchmarkCachedQueryEach2_10k` | Same as above but via `*CachedQuery` (pre-filtered table list) |
| `BenchmarkQueryIterField_10k` | Manual `NewQuery→Iter→Next→Field[T]` loop; compare vs Each2 |
| `BenchmarkQueryAcrossArchetypes_10k` | 10k entities across 5 archetypes (extra tag each); multi-table overhead |
| `BenchmarkFieldT_AllocCost` | `Field[T]` alone in a tight loop; isolates reflect cost |

### e) Hooks + Observers

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkSetNoHook_10k` | 10k Set calls, no hook; baseline for hook comparison |
| `BenchmarkOnSetHookFires_10k` | Same but OnSet hook registered |
| `BenchmarkObserverFires_10k` | One observer for EventOnSet; 10k Set calls |
| `BenchmarkObserverFires_HookAndObserver_10k` | Both hook AND observer registered |
| `BenchmarkObserverFires_5observers_10k` | 5 stacked observers; tests linear dispatch overhead |

### f) Deferred queue

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkDeferOverhead_NoOps` | `Defer(func(){})` with no operations; pure bookkeeping cost |
| `BenchmarkDeferSet_10k` | 10k deferred Sets in one Defer block, then flush |
| `BenchmarkDeferDelete_10k` | 10k deferred Deletes; timer wraps only the Defer/flush |
| `BenchmarkDeferNested` | Nested DeferBegin/End cost |

### g) Systems + Progress

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkProgress_NoSystems` | Empty `Progress(dt)`; per-frame overhead |
| `BenchmarkProgress_OneSystem_10kEntities` | One OnUpdate system iterating 10k entities |
| `BenchmarkProgress_TenSystems_1kEntities` | 10 systems × 1k entities |
| `BenchmarkProgress_WithFixedTimestep` | OnFixedUpdate system with `SetFixedTimestep(1/60)` |
| `BenchmarkProgress_PipelineFull` | All 4 phases (Pre, OnFixed, On, Post), 1k entities each |

### k) Parallel dispatch

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkProgress_ParallelDispatch_2systems_10k` | 2 parallel systems (disjoint write sets), `WorkerCount=2`, 10k entities each |
| `BenchmarkProgress_SerialBaseline_2systems_10k` | Same 2 systems, `WorkerCount=0` (serial baseline) |

### h) ChildOf / IsA hierarchies

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkChildOfCascadeDelete_100` | Delete parent with 100 children |
| `BenchmarkChildOfCascadeDelete_10k` | Delete parent with 10,000 children (stress test) |
| `BenchmarkIsAGet_Hit` | Get on child; prefab has Position; one IsA hop |
| `BenchmarkIsAGet_MissedChain` | Get on child; prefab lacks Position; chain falls through |
| `BenchmarkLookupPath_3deep` | `w.Lookup("scene.car.wheel")` over a populated tree |

### i) Traversal helpers (Phase 6.2)

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkGetUp_SelfHit` | `GetUp[T]` when entity locally owns component; depth-0, 0 allocs |
| `BenchmarkGetUp_Depth1` | `GetUp[T]` one hop up; parent owns component, child does not |
| `BenchmarkGetUp_Depth5` | `GetUp[T]` five hops up; only 5th ancestor owns component |

---

## v0.104.0 baseline — sync.Pool traversal pooling (Phase 16.49)

Captured on branch `snichols/issue-281` (Phase 16.49 — sync.Pool hot paths).
Machine: AMD Ryzen Threadripper PRO 5995WX 64-Cores, 2026-05-15.

### IsA traversal (getViaIsA / hasViaIsA)

| Benchmark | Before | After | Δ ns/op | Δ allocs/op |
|---|---|---|---|---|
| `BenchmarkIsAGet_Hit` | 330.2 ns, 192 B, 2 allocs | 89.33 ns, 0 B, **0 allocs** | −73% | −100% |
| `BenchmarkOwnsVsHas/Has-via-IsA` | ~330 ns, 192 B, 2 allocs | 68.35 ns, 0 B, **0 allocs** | −79% | −100% |

### Relationship traversal (walkUp)

| Benchmark | Before | After | Δ ns/op | Δ allocs/op |
|---|---|---|---|---|
| `BenchmarkGetUp_SelfHit` | 35.66 ns, 0 B, 0 allocs | 37.05 ns, 0 B, 0 allocs | +4% | 0% |
| `BenchmarkGetUp_Depth1` | 317.8 ns, 192 B, 2 allocs | 96.25 ns, 0 B, **0 allocs** | −70% | −100% |
| `BenchmarkGetUp_Depth5` | 556.1 ns, 192 B, 2 allocs | 182.6 ns, 0 B, **0 allocs** | −67% | −100% |

SelfHit depth-0 overhead is within measurement noise (the map pool Get/Put path
is not exercised for depth-0 hits, so there is no regression).

### Pooled sites

Three sites share one `idSeenPool sync.Pool` for `map[ID]struct{}`:
- `walkUp` (was 13.05% of all allocs)
- `hasViaIsA` (was 9.22%)
- `getViaIsA` / `getViaIsAByID` (was 7.74%)

Total allocation count reduced from 120.5M → 75.8M objects (37% reduction).
See `PROFILE.md` for the full before/after pprof snapshots and classification.

---

## Phase 6.2 baseline

Captured on branch `snichols/issue-51` (Phase 6.2 — traversal helpers). Same
machine as prior baselines (AMD Ryzen Threadripper PRO 5995WX 64-Cores, 2026-05-10).

```
BenchmarkGetUp_SelfHit-128    118704559   30.49 ns/op    0 B/op   0 allocs/op
BenchmarkGetUp_Depth1-128      11875677  318.20 ns/op  192 B/op   2 allocs/op
BenchmarkGetUp_Depth5-128       6931424  525.40 ns/op  192 B/op   2 allocs/op
```

Key observations:
- **SelfHit (depth 0)**: 0 allocs — lazy `seen` map is never allocated when the
  component is found on the starting entity itself.
- **Depth 1 → 5**: both allocate 192 B / 2 allocs (one seen map, one map entry) —
  the map is allocated exactly once on the first step past e and reused for all
  subsequent steps.
- Depth-5 is ~1.6× slower than Depth-1, consistent with ~4 additional
  `index.Get` + `firstPairTarget` calls.

---

## Baseline

Captured on `e2cc8ee` (Phase 8.1 documentation). Absolute numbers vary by
hardware; use these for relative comparisons only.

```
## Baseline (Go 1.21, Linux amd64, AMD Ryzen Threadripper PRO 5995WX 64-Cores, 2026-05-10)

BenchmarkNewEntity-128                             	 1762947	        75.57 ns/op	     111 B/op	       0 allocs/op
BenchmarkDeleteEntity-128                          	 1554351	        68.92 ns/op	      42 B/op	       0 allocs/op
BenchmarkAllocFreeAllocCycle-128                   	  879300	       191.1 ns/op	     121 B/op	       1 allocs/op
BenchmarkSetExistingComponent-128                  	 2372041	        61.18 ns/op	       0 B/op	       0 allocs/op
BenchmarkSetNewComponentTriggerMigration-128       	  327762	       339.2 ns/op	     123 B/op	       2 allocs/op
BenchmarkGetExistingComponent-128                  	 3537337	        30.76 ns/op	       0 B/op	       0 allocs/op
BenchmarkGetMissingComponent-128                   	 9538428	        10.58 ns/op	       0 B/op	       0 allocs/op
BenchmarkHasExistingComponent-128                  	 4879249	        23.04 ns/op	       0 B/op	       0 allocs/op
BenchmarkOwnsVsHas/Has-via-IsA-128                 	 2905844	        38.32 ns/op	       0 B/op	       0 allocs/op
BenchmarkOwnsVsHas/Owns-local-only-128             	18154311	         5.540 ns/op	       0 B/op	       0 allocs/op
BenchmarkAddOneComponent_CacheHit-128              	  492201	       266.0 ns/op	     108 B/op	       2 allocs/op
BenchmarkAddOneComponent_CacheMiss-128             	  426528	       265.2 ns/op	     121 B/op	       2 allocs/op
BenchmarkRemoveOneComponent-128                    	  674796	       188.5 ns/op	      79 B/op	       2 allocs/op
BenchmarkSwapComponent-128                         	  208042	       493.6 ns/op	     176 B/op	       4 allocs/op
BenchmarkQueryEach2_1k-128                         	   29575	      4070 ns/op	     224 B/op	       8 allocs/op
BenchmarkQueryEach2_10k-128                        	    4788	     24178 ns/op	     224 B/op	       8 allocs/op
BenchmarkQueryEach2_100k-128                       	     534	    188841 ns/op	     224 B/op	       8 allocs/op
BenchmarkCachedQueryEach2_10k-128                  	   13644	      7812 ns/op	      48 B/op	       2 allocs/op
BenchmarkQueryIterField_10k-128                    	   11880	      8992 ns/op	     120 B/op	       4 allocs/op
BenchmarkQueryAcrossArchetypes_10k-128             	    4267	     28865 ns/op	     504 B/op	      18 allocs/op
BenchmarkFieldT_AllocCost-128                      	 1397854	        93.68 ns/op	      24 B/op	       1 allocs/op
BenchmarkSetNoHook_10k-128                         	     165	    630373 ns/op	       0 B/op	       0 allocs/op
BenchmarkOnSetHookFires_10k-128                    	     159	    667878 ns/op	       0 B/op	       0 allocs/op
BenchmarkObserverFires_10k-128                     	     111	    922046 ns/op	       0 B/op	       0 allocs/op
BenchmarkObserverFires_HookAndObserver_10k-128     	     116	    901452 ns/op	       0 B/op	       0 allocs/op
BenchmarkObserverFires_5observers_10k-128          	      38	   2980658 ns/op	  480000 B/op	   10000 allocs/op
BenchmarkDeferOverhead_NoOps-128                   	14735953	         7.087 ns/op	       0 B/op	       0 allocs/op
BenchmarkDeferSet_10k-128                          	      55	   2411259 ns/op	  790392 B/op	   10018 allocs/op
BenchmarkDeferDelete_10k-128                       	      79	   1833463 ns/op	  828024 B/op	   10037 allocs/op
BenchmarkDeferNested-128                           	  456914	       248.5 ns/op	      56 B/op	       2 allocs/op
BenchmarkProgress_NoSystems-128                    	 3809521	        29.45 ns/op	       0 B/op	       0 allocs/op
BenchmarkProgress_OneSystem_10kEntities-128        	   16984	      6124 ns/op	     112 B/op	       3 allocs/op
BenchmarkProgress_TenSystems_1kEntities-128        	    8041	     14490 ns/op	    1360 B/op	      33 allocs/op
BenchmarkProgress_WithFixedTimestep-128            	   81717	      1324 ns/op	     112 B/op	       3 allocs/op
BenchmarkProgress_PipelineFull-128                 	   20436	      5116 ns/op	     448 B/op	      12 allocs/op
BenchmarkChildOfCascadeDelete_100-128              	    4203	     26106 ns/op	   11416 B/op	      30 allocs/op
BenchmarkChildOfCascadeDelete_10k-128              	      46	   2397978 ns/op	 1746223 B/op	     133 allocs/op
BenchmarkIsAGet_Hit-128                            	 1602481	        67.72 ns/op	       0 B/op	       0 allocs/op
BenchmarkIsAGet_MissedChain-128                    	 8853752	        11.58 ns/op	       0 B/op	       0 allocs/op
BenchmarkLookupPath_3deep-128                      	  186806	       576.6 ns/op	      48 B/op	       1 allocs/op
```

## Phase 8.3 results

Three micro-optimizations landed: (A) `Field[T]` zero-alloc `unsafe.Slice` path,
(B) observer dispatch without per-fire snapshot slice, (C) lazy `seen` map in
IsA Get/Has fallback. Measured on the same machine as the Phase 8.2 baseline.

### A: Field[T] zero-alloc

Before: `rv.Interface().([]T)` — boxes a `[]T` slice into an `any`, then type-asserts it back; 1 alloc per `Field` call.
After: `unsafe.Slice((*T)(base), n)[:it.Count()]` — zero-alloc typed view over the column backing array.

```
BenchmarkFieldT_AllocCost:          before 93.68 ns/op, 24 B/op, 1 alloc  -> after 16.9 ns/op,  0 B/op, 0 allocs  (-82% / -1 alloc)
BenchmarkQueryIterField_10k:        before  8992 ns/op, 120 B/op, 4 allocs -> after 6700 ns/op, 72 B/op, 2 allocs  (-25% / -2 allocs)
BenchmarkQueryEach2_1k:             before  4070 ns/op, 224 B/op, 8 allocs -> after 3450 ns/op, 176 B/op, 6 allocs (-15% / -2 allocs)
BenchmarkQueryEach2_10k:            before 24178 ns/op, 224 B/op, 8 allocs -> after 21000 ns/op, 176 B/op, 6 allocs (-13% / -2 allocs)
BenchmarkQueryEach2_100k:           before 188841 ns/op, 224 B/op, 8 allocs -> after 189900 ns/op, 176 B/op, 6 allocs (flat ns / -2 allocs)
BenchmarkCachedQueryEach2_10k:      before  7812 ns/op,  48 B/op, 2 allocs -> after  7380 ns/op,  0 B/op, 0 allocs  (-6% / -2 allocs)
BenchmarkQueryAcrossArchetypes_10k: before 28865 ns/op, 504 B/op, 18 allocs -> after 21100 ns/op, 216 B/op, 6 allocs (-27% / -12 allocs)
```

### B: Observer dispatch without per-fire snapshot

Before: `active := make([]*observerNode, 0, len(nodes))` allocated on every dispatch.
After: direct range over `nodes`, skipping `removed` entries in-place.

Semantic change: `Unsubscribe` called from a callback now takes effect immediately
for not-yet-visited observers in the same dispatch (was: all nodes active at dispatch-start
always fired). See `(*Observer).Unsubscribe` godoc.

```
BenchmarkObserverFires_10k:              before 922046 ns/op,      0 B/op,     0 allocs -> after 745000 ns/op, 0 B/op, 0 allocs (-19%)
BenchmarkObserverFires_HookAndObserver_10k: before 901452 ns/op,  0 B/op,     0 allocs -> after 763000 ns/op, 0 B/op, 0 allocs (-15%)
BenchmarkObserverFires_5observers_10k:   before 2980658 ns/op, 480000 B/op, 10000 allocs -> after 1013000 ns/op, 0 B/op, 0 allocs (-66% / -10000 allocs/10k)
```

### C: Lazy `seen` map in IsA Get/Has fallback

Before: `Get[T]`, `Has[T]`, and `HasID` unconditionally allocated `map[ID]struct{}{e: {}}` on every local miss before walking the IsA chain — even when the entity has no IsA pairs at all.
After: `getViaIsA`/`hasViaIsA` allocate `seen` lazily only when the first IsA pair is encountered. Entities without IsA pairs incur zero map allocation on a local miss.

The existing benchmarks (`BenchmarkGetMissingComponent`, `BenchmarkGetExistingComponent`, `BenchmarkHasExistingComponent`) return before the IsA path (component unregistered or direct hit), so they don't directly exercise this hotspot. The `BenchmarkOwnsVsHas/Has-via-IsA` and `BenchmarkIsAGet_Hit` benchmarks measure the IsA-hit path which does allocate the map — 192 B / 2 allocs in both old and new code (unchanged; only the common no-IsA case is improved).

### Notable findings from baseline

- **`OwnsID` vs `HasID`**: Owns is 7× faster (5.5 ns vs 38 ns) — HasID walks the
  IsA chain even on a local hit because it may inherit.
- **`CachedQuery` vs uncached `Each2`**: CachedQuery iteration at 7.8 µs vs
  24 µs for 10k entities — 3× faster due to pre-filtered table list.
- **Observer dispatch allocates**: 5 stacked observers allocate 48 B per entity
  fired (480 kB / 10k ops) — the snapshot-slice allocation flagged in Phase 8.1
  follow-ups is confirmed here.
- **Deferred Set**: 10k deferred Sets allocate ~79 B each (closure capture) —
  expected; Phase 8.3 can evaluate arena/pool strategies.
- **`Field[T]`**: 24 B / 1 alloc per call — the reflect path allocates one
  `[]T` slice header per column access. An `unsafe.Slice` path would eliminate this.

---

## Phase 8.4 results

**Root cause of Add/Remove asymmetry in `migrate()`:**

Three allocation sites on the Remove path that were absent (or fewer) on the Add path:

1. `world.go:438` — `newSig := make([]ID, 0, len(oldSig)+1)` always ran, even on edge-cache hits. Add and Remove both paid this; it was 1 of the 2 allocs in Add and 1 of the 4 in SwapComponent.

2. `column.go:93` — `c.slice = c.slice.Slice(0, n+1)` in `appendZero()`. Each `reflect.Value.Slice()` call allocates a heap-resident slice header. Both paths pay this once (the destination column grows by one row). 1 alloc.

3. `column.go:131` — `c.slice = c.slice.Slice(0, last)` in `removeSwap()`. Same issue, one per source column being removed. SwapComponent removes from a 2-column source table → 2 allocs here. Add removes from the empty table (0 columns) → 0 allocs.

Summary: Add ([] → [benchPos]) had 2 allocs; SwapComponent ([benchPos, benchVel] → [benchPos]) had 4 allocs = 1 (newSig) + 1 (appendZero Slice) + 2 (removeSwap Slice for each source column). The extra 2 allocs were structural: SwapComponent's source table has 2 data columns vs Add's empty-table source.

**Fixes applied:**

A. `world.go`: moved the edge-cache check before `newSig` computation. On a cache hit the `make([]ID, ...)` is never reached, saving 1 alloc per repeated single-component migration.

B. `column.go`: added `n int` to `Column` to track logical row count separately from `c.slice`. `c.slice` now always has `Len() == Cap()` (the full backing array). `appendZero` and `removeSwap` update `c.n` directly instead of calling `reflect.Value.Slice()`, eliminating the heap allocation per call.

**Before / After (benchstat, 10 runs, same machine):**

```
                             │   before    │              after               │
                             │   sec/op    │   sec/op     vs base             │
AddOneComponent_CacheHit-128   214.8n ± 3%  143.7n ± 4%  -33.14% (p=0.000)
RemoveOneComponent-128         161.5n ± 2%   87.2n ± 7%  -45.97% (p=0.000)
SwapComponent-128              287.6n ± 3%  168.2n ± 2%  -41.54% (p=0.000)

                             │  allocs/op  │  allocs/op  vs base              │
AddOneComponent_CacheHit-128    2 ± 0%       0 ± 0%      -100.00% (p=0.000)
RemoveOneComponent-128          2 ± 0%       0 ± 0%      -100.00% (p=0.000)
SwapComponent-128               4 ± 0%       0 ± 0%      -100.00% (p=0.000)
```

All three benchmarks reach **0 allocs/op** on the cache-hit steady-state path. The remaining `B/op` (amortized memory from table growth) is from Go's `append` doubling on the plain `[]ID` entity column and from periodic `reflect.MakeSlice` growth in the component columns — both amortized to sub-1 alloc/op over many iterations.

---

## Nil-logger fast path (issue #73 — structured logging)

Added `if w.logger != nil` checks at each lifecycle event site. Measured before/after to confirm no measurable regression on the hot paths (no logger installed):

**Before (5 runs, no logger field):**
```
BenchmarkNewEntity-128              ~43 ns/op    0 allocs/op
BenchmarkSetExistingComponent-128   ~51 ns/op    0 allocs/op
```

**After (5 runs, logger field present, nil — fast path):**
```
BenchmarkNewEntity-128              ~47 ns/op    0 allocs/op
BenchmarkSetExistingComponent-128   ~52 ns/op    0 allocs/op
```

`BenchmarkSetExistingComponent` (the true no-migration hot path) shows no change. `BenchmarkNewEntity` variance is within normal measurement noise (~10 ns swing between runs). The nil-logger check is a single pointer compare that the compiler treats as a branch-predicted fast path; `BenchmarkSetExistingComponent` confirms **0 allocs/op** is maintained.

---

## Phase 10.1 — Parallel system dispatch

Two systems each iterating 10k entities (pos += 1, vel.DX += 0.5). Captured on
AMD Ryzen Threadripper PRO 5995WX 64-Cores, 2026-05-11.

```
BenchmarkProgress_ParallelDispatch_2systems_10k-128     103977    22591 ns/op   496 B/op   7 allocs/op
BenchmarkProgress_SerialBaseline_2systems_10k-128       241250     9558 ns/op   224 B/op   2 allocs/op
```

**Speedup ratio: 0.42× (serial is 2.4× faster for this workload).**

For this workload (tight CPU-bound loops on 10k entities with no I/O), the
goroutine dispatch and `sync.WaitGroup` overhead (~12 µs) outweighs the
parallelism benefit. The per-system iteration takes ~5 µs each; the 2-worker
pool adds ~13 µs of coordination overhead, yielding a net slowdown.

Parallel dispatch becomes beneficial when individual system work takes longer
than the goroutine dispatch overhead (≈10–50 µs depending on pool size and
GOMAXPROCS). Typical candidates: systems with heavy computation, systems that
sleep, or systems with large entity counts (> 100k) on multi-core hardware.

The serial baseline (`WorkerCount=0`) is bit-for-bit identical to the behavior
before Phase 10.1; no regression was observed on existing benchmarks.
