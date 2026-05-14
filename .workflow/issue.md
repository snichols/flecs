## Goal

Port upstream C flecs's entity ID range API (`ecs_entity_range_new` / `ecs_entity_range_set` / `ecs_entity_range_get`) so a `Writer` can constrain which IDs `NewEntity` issues. After this phase, `docs/README.md` line 99 flips to ✅ shipped (v0.71.0), closing the last entity-API gap from Phase 14.1.

Target version: **v0.71.0** (next after v0.70.0 multi-term observers, shipped at `29f7d3c`).

### Public API (final, recommended)

```go
// Constrain Writer's allocator to issue IDs in [min, max). Subsequent
// NewEntity calls return IDs in this range. Panics if min < 1, max <= min,
// or min/max exceed the uint32 entity-index width.
flecs.RangeSet(fw *flecs.Writer, min, max flecs.ID)

// Clear any active constraint. NewEntity reverts to monotonic-from-current
// (the world's max_id is preserved across the clear; we do not rewind).
flecs.RangeClear(fw *flecs.Writer)

// Inspect current range. set==false when no range is active.
flecs.RangeGet(s flecs.Scope) (min, max flecs.ID, set bool)

// One-shot allocation in [min, max) without modifying world state.
// Panics if range is exhausted at call time.
flecs.RangeNew(fw *flecs.Writer, min, max flecs.ID) flecs.ID
```

`Writer` thin shims mirror the precedent set by Phase 16.1: `(fw *Writer).RangeSet(min, max ID)` etc. delegate to the package-level functions. Pattern reference: @entity_lifecycle.go lines 110-199.

### Why

Networked games — server reserves IDs `1..65535`; each client gets a contiguous slice (client 1: `65536..131071`, client 2: `131072..196607`, …) and allocates entities in its slice without coordinating with the server on each ID. Also useful for editor/tooling "system" entities allocated in a reserved high range, and for snapshot replay where entities must be materialised at specific IDs.

### Upstream research (cited)

Upstream's model is **substantially more sophisticated** than the simple-allocator-bounding sketch in the original brief. The Go port should consciously **simplify** rather than full-port — the simplifications are listed below.

1. **API shape** (`/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h:2699-2747`): upstream has three calls — `ecs_entity_range_new(world, min, max)` returns a heap-allocated `ecs_entity_range_t*` that is registered with the world and **persists for the world's lifetime** (ranges cannot be deleted). `ecs_entity_range_set(world, range)` activates a previously-created range. `ecs_entity_range_get(world)` returns the active range or NULL.
2. **Range struct** (`include/flecs.h:1570-1578`): each range owns `{min, max, cur, recycled vec<uint64>}`. `max == 0` means unbounded above. Each range owns **its own recycle pool**.
3. **Allocation path** (`/work/agents/claude/projects/SanderMertens/flecs/src/storage/entity_index.c:428-459`): `flecs_entity_index_new_id` does **not** check the active range's upper bound. It just increments `index->max_id` (which was swapped to `range->cur` when the range was activated, see point 5). The "range exhausted" assertion lives **after** allocation, at `/work/agents/claude/projects/SanderMertens/flecs/src/entity.c:156-159`:
   ```c
   ecs_assert(!ecs_eis(unsafe_world)->active_range ||
       !ecs_eis(unsafe_world)->active_range->max ||
       ecs_entity_t_lo(entity) <= ecs_eis(unsafe_world)->active_range->max,
       ECS_OUT_OF_RANGE, NULL);
   ```
   Upstream sets a hard `ECS_OUT_OF_RANGE` assert (panic-equivalent).
4. **Recycle handling on delete** (`src/storage/entity_index.c:253-286`): when an entity is deleted, if its ID is **outside the currently-active range**, upstream looks up the range that "owns" that ID via `flecs_entity_index_find_range`, removes the recycled ID from the dense tail, and appends it to that range's `recycled` vec. If no range owns the ID, the recycled ID is **dropped**. Recycled IDs stay siloed per range.
5. **Range activation** (`src/storage/entity_index.c:288-374`): swapping the active range is heavy-weight — it spills the not-alive section of the dense vec into the previous range's `recycled` vec, saves `prev->cur = max_id`, loads the new range's `recycled` into not-alive, and sets `max_id = range->cur`. This makes set/clear/swap a relatively expensive operation but keeps the hot allocation path identical to the no-range path.
6. **Overlap detection** (`src/world.c:1472-1497`): `ecs_entity_range_new` rejects ranges that overlap any existing range — `ECS_INVALID_PARAMETER`.
7. **Recycled vs in-range interaction**: because upstream **swaps the entire recycle pool on activation**, the active recycle pool contains only IDs that were freed while that same range was active. Out-of-range recycled IDs are physically not in the pool during fresh allocs.
8. **MakeAlive interaction**: `flecs_entity_index_make_alive` (`src/storage/entity_index.c:376-384`) does not consult any range. It claims the specific ID requested. If the claim creates a new max_id (which it can via `set_id` upstream / `MakeAlive` in our Go port), the claim succeeds and `max_id` may move past `range->max`. The next `NewEntity` would then fail the post-alloc assertion — but `MakeAlive` itself bypasses range checks. Document this asymmetry.

### Go-side state (cited)

- @internal/storage/entityindex/entityindex.go — `Index` struct (lines 55-61): `dense`, `recycle`, `pages`, `aliveCount`, `maxID`. `Alloc` (lines 107-135) is the only fresh-allocation path. `MakeAlive` (lines 274-316) sets `maxID = rawIdx` when claiming a higher slot.
- @entity_lifecycle.go — the closest precedent (Phase 16.1 bundle); style for package-level functions + Writer thin shims; defer-scope panic checks.
- @world.go — `World.index *entityindex.Index` (line 44); `newEntityInternal` (lines 775-785) is the single callsite that calls `index.Alloc()` for user entities.
- @scope.go — `Writer.NewEntity` (lines 353-357) calls `newEntityInternal`; `Writer.MakeAlive` thin shim (line 386).

### Recommended simplifications vs upstream

The brief suggests a Go-side simplification: ranges are **transient parameters of RangeSet/RangeClear**, not persistent objects. Lock these in:

1. **No `entity_range_new` step.** `RangeSet(min, max)` is one call. There is no separate range-object that the caller must hold. State explicitly.
2. **No multi-range registry, no per-range recycle pools.** A single `(rangeMin, rangeMax, rangeSet bool)` triple on `*entityindex.Index`. Out-of-range recycled IDs handling (see #4) is implemented without a separate per-range pool.
3. **No overlap detection.** Without multi-range registry, this is unreachable.
4. **Out-of-range recycled IDs**: when `rangeSet` is true and a recycled ID at the head of `idx.recycle` falls outside `[rangeMin, rangeMax)`, **skip it** (do not reissue), and continue scanning the queue for the first in-range candidate. If none is in range, fall through to fresh allocation in `[rangeMin, rangeMax)`. Skipped IDs **stay in the queue** for later reuse when the range is cleared or changed. This diverges from upstream (upstream physically silos pools by range), but preserves the observable behaviour the brief asks for and avoids losing recycled IDs.
   - Note: this turns Alloc's amortised O(1) into a worst-case O(k) where k is the number of out-of-range entries at the queue head. For typical use (single static range set once at world creation), k is bounded by IDs allocated before the range was set. Document and accept.
5. **Range-exhausted check**: panic **before** allocation, not after. In `Alloc`, when `rangeSet && len(recycle in-range) == 0 && maxID+1 >= rangeMax`, panic with a clear message: `flecs: NewEntity: range [%d, %d) exhausted; last issued = %d`. Matches the brief.
6. **`RangeClear` semantics**: continue from the current `maxID` (which may be inside the previous range). The brief recommends this; lock in. Subsequent `NewEntity` allocates `maxID+1`, even if that ID was in the range. This is fine — the slot was never issued, so it's free.
7. **`RangeNew(min, max)`**: implemented as a standalone path that does NOT touch `idx.rangeMin` / `idx.rangeMax`. Walks `idx.recycle` for the first in-range entry; if none, allocates `idx.maxID+1` and verifies it falls in `[min, max)` — if not, panic. State: this means `RangeNew` is most useful when the world's monotonic counter is already inside the requested range, OR when there is a recycled ID available in-range. **Edge case**: if `min > idx.maxID+1`, `RangeNew` jumps `maxID` to `min-1` and allocates `min`. This is a deliberate choice — `RangeNew` is a hammer; it advances the counter as needed. Document.
8. **MakeAlive bypass**: `MakeAlive` does not consult `rangeSet` (matches upstream). Document the implication: if `MakeAlive` advances `maxID` past `rangeMax`, the next `NewEntity` immediately panics range-exhausted. State explicitly.
9. **Marshal**: serialise `rangeMin`, `rangeMax`, `rangeSet` as additional fields on the world snapshot. Round-trip must reproduce them. The recycle queue is already serialised (verify).

### Open decision points — locked in

1. **RangeClear semantics** → continue from current `maxID`. Locked.
2. **NewEntity while range exhausted** → panic. Locked.
3. **MakeAlive bypass** → explicit doc note; bypass is fine, no special handling. Locked.
4. **Recycle pool in-range only** → skip out-of-range entries at queue head; do not lose them. Locked.

### Deliverables

1. **New file `entity_range.go`** (parallel to `entity_lifecycle.go`):
   - `RangeSet(fw *Writer, min, max ID)` — constrain `fw.world.index`'s allocator. Panic on `min < 1`, `max <= min`, or either exceeding `uint32`.
   - `RangeClear(fw *Writer)` — clear the constraint.
   - `RangeGet(s Scope) (min, max ID, set bool)` — query.
   - `RangeNew(fw *Writer, min, max ID) ID` — one-shot.
   - Panic if called in deferred scope (matches @entity_lifecycle.go:137 pattern).
2. **Allocator extension in @internal/storage/entityindex/entityindex.go**:
   - Add fields `rangeMin, rangeMax uint32; rangeSet bool` on `Index`.
   - `SetRange(min, max uint32)` / `ClearRange()` / `GetRange() (uint32, uint32, bool)`.
   - Extend `Alloc()` to: (a) walk `idx.recycle` skipping out-of-range entries when `rangeSet`; (b) when no in-range recycled ID, allocate fresh in `[rangeMin, rangeMax)`; (c) panic on exhaustion with clear message.
   - New `AllocInRange(min, max uint32) ID` for `RangeNew`.
   - Keep the package-private `maxID` field unchanged; tests should not depend on field names.
3. **Writer thin shims in @scope.go**:
   - `func (fw *Writer) RangeSet(min, max ID) { RangeSet(fw, min, max) }`
   - `func (fw *Writer) RangeClear() { RangeClear(fw) }`
   - `func (fw *Writer) RangeNew(min, max ID) ID { return RangeNew(fw, min, max) }`
4. **Tests in `entity_range_test.go`** — at least 12 cases (the brief said 10; add two for `RangeNew` edge cases):
   1. `RangeSet(100, 200)` then 10 `NewEntity` calls — all IDs in `[100, 200)`.
   2. `RangeSet(100, 105)` then 5 `NewEntity` calls succeed; 6th panics with "range [100, 105) exhausted".
   3. `RangeClear` after RangeSet — subsequent `NewEntity` continues from current `maxID` (verify the recovered IDs are NOT in the previous range unless the counter was already there).
   4. `RangeSet(min, max)` where `max <= min` — panic at call site.
   5. `RangeSet(0, 100)` — panic ("min must be >= 1").
   6. `RangeNew(50, 60)` — allocates one ID in `[50, 60)` without changing world's `rangeSet` state. Verify `RangeGet` still returns `set==false`.
   7. **Corrected recycled-ID test**: set range to `100..200`, allocate entity `100`, delete it; then `RangeSet(300, 400)`; then `NewEntity` returns `300`, not the recycled `100` (out-of-range recycled is skipped). After `RangeClear`, the recycled `100` becomes eligible again on the next `NewEntity` if the counter has moved past it. (The brief's original wording was contradictory because it claimed an entity `50` was allocated while the range was `100..200`; corrected here.)
   8. MakeAlive bypasses range: `RangeSet(100, 200)`; `MakeAlive(w, MakeEntity(500, 0))` succeeds; subsequent `NewEntity` panics range-exhausted *only* if `maxID` was advanced past `rangeMax` by the claim — verify the panic message names the actual range.
   9. Marshal round-trip with active range: `RangeSet`, marshal, unmarshal — range survives.
   10. Re-range mid-game: `RangeSet(100, 200)`, allocate 3 entities, `RangeSet(500, 600)`, allocate 3 more — second batch is in `[500, 600)`.
   11. `RangeGet` round-trip: `RangeSet(7, 42)` → `RangeGet` returns `(7, 42, true)`; after `RangeClear` returns `(0, 0, false)`.
   12. `RangeNew` advances counter when needed: starting `maxID = 5`, `RangeNew(100, 110)` returns `100` and advances `maxID` to `100`. Subsequent `NewEntity` (no range set) returns `101`. State that this jump is intentional.
   - Coverage ≥ 95.0% on `entity_range.go` and on the new `entityindex` lines.
5. **Docs (per @CONTRIBUTING.md:55-82)**:
   - `docs/EntitiesComponents.md` § Entity Ranges (currently a placeholder at line 146-148) — replace with a code-example section that demonstrates the networked-app use case (server reserves `1..65535`, client gets `65536..131071`).
   - `docs/README.md` line 99 — flip to ✅ shipped (v0.71.0).
   - `README.md` — feature-list bump.
   - `CHANGELOG.md` — new v0.71.0 entry at the top, following the v0.70.0 format (Added / Changed / Implementation notes sections).
   - `ROADMAP.md` line 3 — heading bumps to "Shipped (through v0.71.0)"; add a bullet for Phase 16.16 mirroring lines 60 / 74's style.

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0% on touched files
- No regression on existing entity-allocator tests in @internal/storage/entityindex/entityindex_test.go

### Explicit non-goals

- **Multi-range registry** (upstream's `index->ranges` vec). Single active range only.
- **Per-range recycle pools** (upstream's `range->recycled`). Single global recycle pool; out-of-range entries are skipped, not siloed.
- **Range overlap detection** — unreachable without a multi-range registry.
- **Automatic per-stage / per-thread partitioning** — explicit `RangeSet` only.
- **Cross-world range coordination** — single-world only.
- **`max == 0 == unbounded` upstream convention** — we require `max > min`; explicit upper bound only. State.

## Constraints

- @internal/storage/entityindex/entityindex.go — sole entity allocator; all range plumbing lives here. Keep `maxID` package-private; expose via methods only.
- @entity_lifecycle.go — Phase 16.1 precedent: package-level functions + `Writer` thin shims; defer-scope panic checks; godoc references upstream C with file:line.
- @scope.go — `Writer` shim methods sit alongside `MakeAlive` / `SetVersion` (lines 386, 389).
- @world.go — `newEntityInternal` (lines 775-785) is the sole user-entity allocation path; built-in bootstrap allocs (lines 216-380) run before any range can be set, so they are unaffected. Verify with a test that `RangeSet` does not perturb built-in entity indices.
- @marshal.go — serialise `rangeMin`/`rangeMax`/`rangeSet` on the world snapshot; round-trip test #9.
- @CONTRIBUTING.md — godoc on exported symbols beginning with the symbol name; `doc.go` updates if conceptually new; `docs/EntitiesComponents.md` for narrative; `CHANGELOG.md` + `ROADMAP.md` for shipping.
- @docs/README.md — line 99 is the gap entry to flip to ✅ shipped (v0.71.0).
- @docs/EntitiesComponents.md — line 146-148 is the placeholder § Entity Ranges to replace with real prose + example.
- Upstream cite: `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h:2699-2747` (API), `src/storage/entity_index.c:288-374` (set_range), `src/storage/entity_index.c:253-286` (range-aware delete), `src/storage/entity_index.c:428-459` (new_id), `src/entity.c:156-159` (post-alloc range assert), `src/world.c:1462-1552` (range_new / range_set / range_get).
