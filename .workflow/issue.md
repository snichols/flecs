## Goal

Profile-driven pass through the codebase to identify allocation hot paths beyond the already-pooled defer queue (Phase 11.0 bump arena) and apply `sync.Pool` where the savings are measurable. This is the last optimization deferral on ROADMAP.md (Performance section).

Target version: **v0.104.0** / Phase number: **16.49**.

### Methodology — profile first, optimize second

Before any code change, run the existing benchmark suite with memory profiling to **identify** the hot paths:

1. `go test -bench=. -benchmem -memprofile mem.out ./...` — capture allocation profile
2. `go tool pprof -alloc_objects mem.out` — list top 20 allocation sites
3. For each top site, classify:
   - Already pooled (defer queue, etc.) — skip
   - Per-call short-lived slice/map (good pool candidate)
   - Per-call long-lived or reference-escaping (NOT a pool candidate — would defeat GC)
   - Already amortized (e.g. cached query results) — skip

The PR description must include the profile output (top 20 sites) as a code block so we can verify alignment.

### Candidate sites to investigate

Verify each candidate via profile **before** pooling. Likely hot paths based on architecture:

- **`query.go` query construction** — `[]Term` slices, scratch buffers for variable ordering, term-decomposition scratch
- **`query.go` query iteration** — per-iter scratch (matched-table list, variable bindings)
- **`cached_query.go` re-sort / re-group** — temporary sort buffers
- **`observer.go` dispatch** — per-fire snapshot list (verify if zero-alloc already — Phase 16.x notes claim it is)
- **`observer_multi.go` multi-term evaluation** — per-fire scratch
- **`marshal.go` JSON serialization** — per-component byte buffers
- **`snapshot.go` binary serialization** — encoder scratch
- **`rest_query.go` query results** — response building scratch
- **`internal/storage/componentindex/componentindex.go` table-lookup** — slice scratch
- **`cleanup.go` cascade walker** — entity-list scratch (Phase 16.48 new path)
- **`cmd_queue.go` flush** — coalescer scratch (verify if bump arena already covers)
- **`world.go` migrate / commitBatch** — column-copy scratch
- **`parent_storage.go` parent column** — Phase 16.47 column scratch

### Pooling strategy

For each chosen site:

1. Declare a package-level `var fooPool = sync.Pool{New: func() any { return ... }}`
2. At callsite entry: `x := fooPool.Get().(*FooType); x.Reset()`
3. At callsite exit (defer): `fooPool.Put(x)`
4. Verify `Reset` zero's all referenced slices/maps (to avoid keeping garbage alive)
5. Add a benchmark: `BenchmarkFoo_Pooled` vs `BenchmarkFoo_NoPool` (with build tag or function dispatch)

**Critical safety:** if a pooled object outlives the callsite (escapes via channel, closure, return value), it CANNOT be pooled — that would create use-after-free. Audit each candidate carefully.

### What NOT to pool

- Objects that escape to heap and are held by user code
- Objects with short-lived references that the compiler can stack-allocate (escape analysis already handles them)
- Tiny objects (< 64 bytes) — sync.Pool overhead exceeds savings
- Objects with non-trivial Reset cost (e.g. clearing 10MB maps takes longer than allocating fresh)

### Required tests

#### Profile capture
- Add a script or test helper that captures the memory profile and documents top 20 allocation sites. Place in `@PROFILE.md` (new file). Capture both BEFORE-pool and AFTER-pool snapshots.

#### Per-site regression tests
For each pooled site, add a `BenchmarkXxx_AllocCount` test that exercises the path 1000 times and asserts:
- Allocations per op are within ±5% of the expected value
- No correctness regression (existing tests still pass)

#### Concurrency safety tests
- `TestSyncPool_ConcurrentGet` — N goroutines acquiring/releasing pooled objects; no data races, no stale state
- `TestSyncPool_ResetClearsState` — verify Reset on each pooled type fully clears state (especially slice contents, map entries)
- `TestSyncPool_NoEscape` — verify pooled objects don't escape (use `go test -gcflags='-m'` or manual audit)

#### Allocation regression guard
- `TestAllocations_Regression` — a single test that runs a representative workload (create 1000 entities, query, mutate, query again, delete) and asserts total allocations < threshold. Document the threshold; refresh when intentional changes alter it.

#### Reset correctness
For each pooled type, add a unit test that:
- Get from pool
- Mutate (populate slices, set fields)
- Reset
- Verify state is fully zeroed
- Put back to pool
- Get again (potentially same instance)
- Verify the second Get sees a fully-reset state

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run ./...` clean
- `go test ./... -race -count=3` clean
- Coverage ≥ 95% (current baseline)
- `go test -bench=. -benchmem ./...` shows measurable allocation reduction (≥ 20% on at least 3 hot benchmarks)
- All existing tests pass unchanged
- No correctness regressions (existing tests, including stress tests, pass)

### Documentation update matrix

- New file `@PROFILE.md` — before/after profile snapshots, list of pooled sites with rationale
- `@BENCH.md` — append v0.104.0 section with before/after numbers for the affected benchmarks
- `@ROADMAP.md`:
  - Move "Custom allocators / `sync.Pool` for additional hot paths" from Future Work / Performance to Shipped
  - Add a "Shipped" entry describing the pooled sites
  - Bump heading to v0.104.0
- `@CHANGELOG.md` — v0.104.0 entry listing each pooled site
- `@README.md` — update Performance feature row if pool coverage is called out

### Non-goals

- New benchmarks beyond the regression guards (existing `bench_test.go` suite is the baseline)
- Replacing sync.Pool with a custom allocator (sync.Pool is the right tool for this granularity)
- Object pinning / lifetime extension across goroutines (sync.Pool is goroutine-local-friendly; cross-goroutine pooling is a separate problem)
- Eliminating allocations from public API surface (only internal hot paths)
- Migrating away from `unsafe.Slice` / `reflect.Slice` patterns (those are already zero-alloc)
- Switching to a different allocator (e.g. tcmalloc) — Go's runtime allocator + sync.Pool is the design choice
- Further optimizing the defer queue (already optimal via Phase 11.0 bump arena)
- Lock-free defer queue (Concurrency section deferral) — separate phase

### Notes

- This is a profile-driven phase — the iterate agent MUST run the profile and quote results in the PR description, not invent target sites
- If a candidate site shows < 5% allocation contribution in the profile, skip it (overhead would not pay)
- For each pooled site, document the Reset semantics — particularly which slices/maps need clearing

## Constraints

- @bench_test.go — existing benchmark suite; baseline for allocation measurements and the location where new `BenchmarkXxx_AllocCount` regression guards land
- @BENCH.md — benchmark history; append v0.104.0 section with before/after numbers for affected benchmarks
- @cmd_queue.go — existing bump arena from Phase 11.0; the reference pattern for hot-path allocator coverage and explicitly out-of-scope for further optimization
- @query.go — query construction hot path; `[]Term` slices, variable ordering scratch, term-decomposition scratch are candidate pool sites pending profile verification
- @cached_query.go — cached query re-sort / re-group temporary sort buffers are candidate pool sites
- @observer.go — observer dispatch per-fire snapshot list; verify zero-alloc claim from Phase 16.x before pooling
- @observer_multi.go — multi-term evaluation per-fire scratch
- @marshal.go — JSON serialization per-component byte buffer scratch
- @snapshot.go — binary serialization encoder scratch
- @rest_query.go — REST query response building scratch
- @internal/storage/componentindex/componentindex.go — table-lookup slice scratch
- @cleanup.go — Phase 16.48 cascade walker entity-list scratch (newly added path)
- @parent_storage.go — Phase 16.47 parent column scratch
- @world.go — migrate / commitBatch column-copy scratch
- @ROADMAP.md — Performance section currently lists "Custom allocators / `sync.Pool` for additional hot paths" as Future Work; this phase ships it. Bump heading to v0.104.0 and add Shipped entry
- @CHANGELOG.md — append v0.104.0 entry enumerating each pooled site
- @README.md — update Performance feature row if pool coverage is surfaced there
- @PROFILE.md — new file capturing BEFORE/AFTER `go tool pprof -alloc_objects` top-20 snapshots with rationale per pooled site
- Coverage gate ≥ 95% (current baseline); `go vet ./...`, `golangci-lint run ./...`, and `go test ./... -race -count=3` must remain clean
- sync.Pool safety rule: pooled objects MUST NOT escape the callsite (no return, no channel send, no closure capture beyond defer-scope); audit each candidate
- Profile-gated inclusion rule: any candidate showing < 5% allocation contribution is skipped — overhead would exceed savings
