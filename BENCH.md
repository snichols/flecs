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

### h) ChildOf / IsA hierarchies

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkChildOfCascadeDelete_100` | Delete parent with 100 children |
| `BenchmarkChildOfCascadeDelete_10k` | Delete parent with 10,000 children (stress test) |
| `BenchmarkIsAGet_Hit` | Get on child; prefab has Position; one IsA hop |
| `BenchmarkIsAGet_MissedChain` | Get on child; prefab lacks Position; chain falls through |
| `BenchmarkLookupPath_3deep` | `w.Lookup("scene.car.wheel")` over a populated tree |

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
