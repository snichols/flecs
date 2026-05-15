# Allocation Profile — v0.104.0 (Phase 16.49)

## Methodology

```sh
go test -bench=. -benchmem -memprofile mem.out -count=1 .
go tool pprof -alloc_objects -top mem.out
```

Machine: AMD Ryzen Threadripper PRO 5995WX 64-Cores, 2026-05-15.

---

## BEFORE pool — top-20 allocation sites

Total objects allocated: **120,460,464**

```
      flat  flat%   sum%        cum   cum%
  24678232 20.49% 20.49%   40946175 33.99%  github.com/snichols/flecs.(*Query).Iter
  15715524 13.05% 33.53%   15715524 13.05%  github.com/snichols/flecs.walkUp
  14181510 11.77% 45.31%   15350431 12.74%  github.com/snichols/flecs.(*Query).buildVarRowsRec
  11264674  9.35% 54.66%   11264674  9.35%  github.com/snichols/flecs.wildcardMatchingPairs
  11105740  9.22% 63.88%   11105740  9.22%  github.com/snichols/flecs.hasViaIsA
   9328819  7.74% 71.62%    9328819  7.74%  github.com/snichols/flecs.getViaIsA[...]
   5225142  4.34% 75.96%    5982982  4.97%  github.com/snichols/flecs.(*QueryIter).matchesTable
   3733503  3.10% 79.06%    6652314  5.52%  github.com/snichols/flecs.(*World).Progress
   2704677  2.25% 81.30%    2704677  2.25%  github.com/snichols/flecs.(*CachedQuery).Iter
   2044879  1.70% 83.00%    2044879  1.70%  github.com/snichols/flecs.sigKey (inline)
   2030592  1.69% 84.69%    2030592  1.69%  github.com/snichols/flecs/internal/storage/entityindex.(*Index).Free
   1900682  1.58% 86.26%    5010483  4.16%  github.com/snichols/flecs.(*World).migrate
   1548167  1.29% 87.55%    2489370  2.07%  github.com/snichols/flecs.NewQuery
   1114121  0.92% 88.47%    1114121  0.92%  github.com/snichols/flecs/internal/storage/componentindex.(*Index).TablesFor (inline)
   1103238  0.92% 89.39%    1103238  0.92%  strings.genSplit
   1015224  0.84% 90.23%    1015224  0.84%  github.com/snichols/flecs/internal/storage/entityindex.(*Index).ensurePage (inline)
    972312  0.81% 91.04%    1168921  0.97%  github.com/snichols/flecs.(*Query).collectVarDomainFor
    967222   0.8% 91.84%    2997814  2.49%  github.com/snichols/flecs.deleteImmediate
    852063  0.71% 92.55%     852063  0.71%  github.com/snichols/flecs.validateAndSortTerms
    775522  0.64% 93.19%     808290  0.67%  github.com/snichols/flecs.bootstrapCompound
```

---

## Classification of top sites

| Site | flat% | Classification | Decision |
|---|---|---|---|
| `(*Query).Iter` | 20.49% | Allocates `*QueryIter` + `tables` slice; both **escape to caller** | ❌ Skip — escapes |
| `walkUp` | 13.05% | Allocates `seen map[ID]struct{}` per traversal; **internal, no escape** | ✅ Pool |
| `(*Query).buildVarRowsRec` | 11.77% | Allocates per-row `[]ID` binding slices; **escape into `varRows`** | ❌ Skip — escapes |
| `wildcardMatchingPairs` | 9.35% | Allocates `[]ID` result; **escapes into `QueryIter.wildcardPairs`** | ❌ Skip — escapes |
| `hasViaIsA` | 9.22% | Allocates `seen map[ID]struct{}` per traversal; **internal, no escape** | ✅ Pool |
| `getViaIsA` | 7.74% | Allocates `seen map[ID]struct{}` per traversal; **internal, no escape** | ✅ Pool |
| `(*QueryIter).matchesTable` | 4.34% | < 5% threshold | ❌ Skip — below threshold |
| `(*World).Progress` | 3.10% | < 5% threshold | ❌ Skip — below threshold |
| All others | < 3% | < 5% threshold | ❌ Skip |

### Non-poolable sites — rationale

- **`(*Query).Iter` (20.49%)** — Returns `*QueryIter` to the caller; the tables
  slice lives inside the returned struct. Pooling requires a `Close()` API on
  `QueryIter`, which is out of scope (public API surface, non-goal per issue).
- **`(*Query).buildVarRowsRec` (11.77%)** — Per-row `[]ID` binding slices are
  stored in `varRow.bindings` which flows into `QueryIter.varRows`. The binding
  slices escape when the iterator is returned to the caller.
- **`wildcardMatchingPairs` (9.35%)** — The `[]ID` result is stored in
  `QueryIter.wildcardPairs` and referenced by the caller during iteration.
  Cannot be reclaimed until the iterator is done — no `Close()` hook.

### Pooled sites — Reset semantics

| Pool | Type | Reset | Safety |
|---|---|---|---|
| `idSeenPool` | `map[ID]struct{}` | `clear(m)` — all entries deleted, backing array retained | Acquired and released within the same call frame; never escapes |

All three pooled call sites share one pool (`idSeenPool`) because they use
identical map types and are never concurrent within a single `*World` operation
(the world holds an exclusive-access lock during writes).

---

## AFTER pool — top-20 allocation sites

Total objects allocated: **75,791,670** (37% reduction from 120,460,464)

```
      flat  flat%   sum%        cum   cum%
  23425390 30.91% 30.91%   37260901 49.16%  github.com/snichols/flecs.(*Query).Iter
  12406845 16.37% 47.28%   13376754 17.65%  github.com/snichols/flecs.(*Query).buildVarRowsRec
   8511914 11.23% 58.51%    8511914 11.23%  github.com/snichols/flecs.wildcardMatchingPairs
   4922932  6.50% 65.00%    5522610  7.29%  github.com/snichols/flecs.(*QueryIter).matchesTable
   4268811  5.63% 70.64%    6838493  9.02%  github.com/snichols/flecs.(*World).Progress
   2814299  3.71% 74.35%    2814299  3.71%  github.com/snichols/flecs.(*CachedQuery).Iter
   2135087  2.82% 77.17%    2135087  2.82%  github.com/snichols/flecs/internal/storage/entityindex.(*Index).Free
   1469868  1.94% 79.11%    2359191  3.11%  github.com/snichols/flecs.NewQuery
   1238724  1.63% 80.74%    1238724  1.63%  github.com/snichols/flecs.sigKey (inline)
   1205954  1.59% 82.33%    3715791  4.90%  github.com/snichols/flecs.(*World).migrate
   1013464  1.34% 83.67%    1013464  1.34%  github.com/snichols/flecs/internal/storage/entityindex.(*Index).ensurePage (inline)
    969909  1.28% 84.95%     969909  1.28%  github.com/snichols/flecs.(*Query).collectVarDomainFor
    872560  1.15% 86.10%    3007647  3.97%  github.com/snichols/flecs.deleteImmediate
    742775  0.98% 87.08%     742775  0.98%  strings.genSplit
    690541  0.91% 87.99%     690541  0.91%  github.com/snichols/flecs/internal/storage/table.(*Table).Append
    659807  0.87% 88.86%     659807  0.87%  github.com/snichols/flecs.validateAndSortTerms
    633768  0.84% 89.70%     633768  0.84%  github.com/snichols/flecs.(*World).inheritorsBFS
    633528  0.84% 90.53%     633528  0.84%  github.com/snichols/flecs.bootstrapCompound
    599678  0.79% 91.32%     599678  0.79%  github.com/snichols/flecs.transitiveWalk
    544793  0.72% 92.04%    1208178  1.59%  github.com/snichols/flecs.bumpMatchingTableRefs
```

`walkUp`, `hasViaIsA`, and `getViaIsA` no longer appear — fully eliminated
from the top-20 allocation sites by sync.Pool pooling.

---

## Benchmark comparison: affected hot paths

| Benchmark | Before | After | Δ ns/op | Δ allocs/op |
|---|---|---|---|---|
| `BenchmarkIsAGet_Hit` | 330.2 ns, 192 B, 2 allocs | 89.33 ns, 0 B, 0 allocs | −73% | −100% |
| `BenchmarkGetUp_Depth1` | 317.8 ns, 192 B, 2 allocs | 96.25 ns, 0 B, 0 allocs | −70% | −100% |
| `BenchmarkGetUp_Depth5` | 556.1 ns, 192 B, 2 allocs | 182.6 ns, 0 B, 0 allocs | −67% | −100% |
| `BenchmarkOwnsVsHas/Has-via-IsA` | ~330 ns, 192 B, 2 allocs | 68.35 ns, 0 B, 0 allocs | −79% | −100% |

All three benchmarks meet the ≥ 20% improvement gate. All now show 0 allocs/op.
