## Goal

Phase 8.3 closed most of the reviewer's top-5 hotspots (Field[T] zero-alloc, observer snapshot elimination, lazy IsA seen map). One open item remains on the deferred list:

> `BenchmarkSwapComponent` 493 ns / 4 allocs vs `BenchmarkAddOneComponent_CacheHit` 188 ns / 2 allocs — Remove path is 2x heavier than Add despite both going through `migrate()`.

The other deferred hotspot (Defer closure capture, item #4) requires a closure → tagged-union refactor that is intentionally out of scope until either multi-threading concerns arrive or a real workload surfaces. This issue is solely about the Add/Remove asymmetry in `migrate()`.

**Scope: Investigate why Remove allocates 2x Add through `migrate()`, then fix the root cause if it's localized. If it requires a larger refactor, document the investigation and file a follow-up issue instead of half-fixing.**

After this issue:
- The root cause is documented (in BENCH.md or a brief code comment).
- If the fix is localized (e.g., redundant slice allocation, missing cache-edge use, sort/filter inefficiency), it's applied.
- If the fix needs a larger refactor (e.g., command-queue restructure), investigation is captured and a follow-up issue is filed.
- `BenchmarkSwapComponent` improves measurably OR we have a clear explanation of why it can't.

### Deliverables

1. **Investigation (do this FIRST before any code changes):**
   - Look at `migrate()` in `world.go`. Trace BOTH the `addID != 0` and `removeID != 0` branches step-by-step.
   - Compare the signature-computation path: what does `addID != 0` do vs `removeID != 0`?
   - Compare the edge-cache lookup: `NextOnAdd(addID)` vs `NextOnRemove(removeID)`. Are both used symmetrically?
   - Compare the new-signature creation: how is the new sorted `[]ID` built? Is one path doing more work (e.g., allocating a fresh slice, sorting, scanning) than the other?
   - Compare the data-copy loop: when migrating, which components' bytes are copied? Are extra columns processed in one path that aren't in the other?
   - Compare any panic-guards, defensive copies, or extra ID lookups (`registry.LookupByID`) on one path that aren't on the other.

2. **Capture findings:**
   - Either in a comment block in the issue's PR commit message, OR in a "Phase 8.4 investigation" section in `BENCH.md`.
   - Format: "Root cause: …" followed by specific file:line references.
   - If the cause is multiple things, list them by frequency/impact.

3. **Apply the fix (if localized):**
   - Examples of localized fixes that ARE in scope:
     - Reusing the same scratch `[]ID` instead of allocating a fresh signature slice.
     - Using the edge cache symmetrically (both `removeEdges` and `addEdges`).
     - Avoiding a redundant `registry.LookupByID` call.
     - Using `copy` over the source signature's slice instead of `append`-and-grow.
     - Avoiding `sort` when the input is already sorted (removing an ID from a sorted slice doesn't require a re-sort).
   - Examples of fixes that are NOT in scope (file a follow-up issue instead):
     - Tagged-union command queue refactor.
     - Migrating to a per-archetype edge table with prefix sharing.
     - Adding a typed cmd buffer.
     - Changing the `Table` storage layout.

4. **Verify the fix:**
   - Re-run `go test ./... -race -count=2` — must pass.
   - Re-run `go test -bench=BenchmarkSwapComponent -benchmem -count=10 ./...` and capture before/after numbers via `benchstat`.
   - Verify `BenchmarkAddOneComponent_CacheHit` is unaffected (no regression).
   - Verify `BenchmarkRemoveOneComponent` is also unaffected or improved.

5. **Update BENCH.md:**
   - Add a "## Phase 8.4 results" section.
   - Show before/after for `BenchmarkSwapComponent`, `BenchmarkAddOneComponent_CacheHit`, `BenchmarkRemoveOneComponent`.
   - Include the root cause finding.
   - If the fix wasn't possible without a larger refactor: document the investigation finding and explicitly note "no code change in this phase; follow-up issue #N filed for the refactor."

6. **Test acceptance:**
   - All existing tests pass.
   - No new tests required unless the fix introduces a new code path; in that case, add a targeted test for the path.

7. **Mechanical acceptance:**
   - `go test ./... -race -count=2` passes.
   - `go vet ./...` clean.
   - `golangci-lint run` clean.
   - Coverage on `flecs` ≥ 90% (no regression from 97.0%).
   - `go test -bench=. -benchmem ./...` runs to completion.

### Non-goals

- NO defer-queue closure refactor.
- NO changes to the `Table` storage layout.
- NO changes to the public API.
- NO new ECS features.
- NO tagged-union cmd struct.

## Constraints

- @world.go — `migrate()` lives here; trace both the `addID != 0` and `removeID != 0` branches step-by-step before changing anything. Do NOT change the migrate() contract (internal method but called from many sites).
- @internal/storage/table/table.go — Table operations that migrate calls; check whether asymmetric column work happens here vs in migrate().
- @id_ops.go — AddID/RemoveID flow into migrate; verify both go through the same entry point with symmetric arguments.
- @bench_test.go — `BenchmarkSwapComponent` setup lives here; confirm what it actually measures (likely `Add Position; Set Velocity; Remove Position` or similar) BEFORE optimizing the wrong path.
- @BENCH.md — Phase 8.2 baseline and Phase 8.3 results; add a "## Phase 8.4 results" section showing before/after for SwapComponent, AddOneComponent_CacheHit, RemoveOneComponent, plus the root cause finding.
- Investigation comes BEFORE code changes. Read the migrate() body twice; trace mentally; only THEN modify.
- If the fix is a 2-line change, great. If it requires touching 5+ files, file a follow-up issue instead.
- DO NOT optimize speculatively. Measure first.
- DO NOT touch the Defer queue, the closure capture pattern, or Field[T].
- DO NOT add `sync.Pool` for migration helpers — measure first whether pooling actually helps.
- DO NOT import third-party deps (still pure stdlib).
- If the investigation reveals NO root cause / the asymmetry is fundamental (e.g., Remove requires a re-sort that Add doesn't), document and close — don't force a synthetic optimization.
