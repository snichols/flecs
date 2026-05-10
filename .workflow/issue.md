## Goal

Port the flecs entity ID allocator and per-entity record map to Go as the next foundational piece after `flecs.ID` (issue #1 — already on `master`).

This package hands out entity IDs, tracks aliveness, recycles freed IDs with bumped generations, and stores per-entity location records. It does **not** wire up tables yet — that is Phase 1.4. The `Record` struct in this issue carries only `Row` and `Dense`; the `Table` field is deliberately deferred.

The C reference is `flecs/src/storage/entity_index.{h,c}`. Read both files in full before writing Go. The struct layout lives at `entity_index.h:14-26` and the page size constant `FLECS_ENTITY_PAGE_SIZE = 64` is at `entity_index.h:11`. Function bodies are in `entity_index.c`.

### Deliverables

1. **New internal package**: `internal/storage/entityindex` with implementation in `internal/storage/entityindex/entityindex.go` and tests in `internal/storage/entityindex/entityindex_test.go`.

2. **Exported types** in that package:
   - `Record struct { Row uint32; Dense uint32 }` — per-entity location record. Add a TODO godoc comment noting that a `Table *table.Table` field will be added in Phase 1.4. **Do NOT add the field now** (the `table` package does not exist yet).
   - `Index struct { ... }` — the entity index. Internal fields unexported.

3. **Methods on `*Index`**:
   - `New() *Index` — constructor.
   - `Alloc() flecs.ID` — issue a fresh entity ID. Recycles freed IDs, bumping generation. Returned ID has its index in lower 32 bits and generation in upper 32 bits, per `flecs.ID` semantics.
     - NOTE: this Go port uses a **32-bit generation**, not the 16-bit C upstream — see `@id.go` for the layout.
   - `Free(id flecs.ID) bool` — release an entity; returns true if the ID was alive (and is now freed and gen-bumped), false if already dead or unknown. Move the freed slot into the recycle list (the dense-vec tail past `aliveCount`) so the next `Alloc` reuses it with `gen+1`.
   - `IsAlive(id flecs.ID) bool` — true iff `id.Index()` has a record AND that record's stored generation equals `id.Generation()`. A stale handle (correct index, old generation) returns false.
   - `Get(id flecs.ID) *Record` — return the record for an alive `id` (read/write access; pointer must remain stable until the next `Alloc`/`Free` that triggers a page-grow). Returns nil if not alive.
   - `Count() int` — number of currently alive entities.
   - `Each(fn func(id flecs.ID, rec *Record))` — iterate alive entities in dense order. Implementer's choice whether `fn` returns `bool` for early-exit; if so, document it.

4. **Internal storage**:
   - **Paged record array**: records stored in pages of 64 entries (`entityPageSize = 64`, mirroring `FLECS_ENTITY_PAGE_SIZE`). Pages allocated lazily as indices grow. Use `[]*[entityPageSize]Record` or `[][]Record` — implementer's call, but record pointers returned from `Get` MUST remain stable as long as no operation grows the page array. **Document the stability contract** in the `Get` godoc.
   - **Dense vec**: a `[]flecs.ID` storing all-ever-allocated IDs in their current generations. Indices `[0:aliveCount)` are alive; indices `[aliveCount:len(dense))` are recycled-and-available, in insertion order. `Alloc` first checks the recycle range; only if empty does it grow `dense` and allocate a new index.
   - **Generation tracking**: the `Record` struct stores `Dense` (the current dense-vec index for this entity). Generation is encoded in the `dense[i]` ID itself, not separately on the record — this matches C, where `record->dense` is a sparse-set position and `dense[record->dense]` carries the live ID-with-generation.

5. **Tests** in `internal/storage/entityindex/entityindex_test.go`. Table-driven where it fits. Cover at minimum:
   - Fresh `Alloc`: index starts at 1 (**NOT 0** — index 0 is reserved as the invalid/null ID, matching ECS convention; document this), generation 0. Repeated allocs return distinct indices.
   - `IsAlive` true after `Alloc`, false after `Free`, false for any never-allocated ID, false for stale handles (same index, lower generation).
   - `Free` returns false for unknown IDs, false for already-freed IDs.
   - **Generation recycling**: alloc → free → alloc returns same index with `gen+1`. Repeat 5+ times; gen monotonically increases.
   - **Recycle ordering**: if you free A then free B, the next two allocs reuse A then B (FIFO from the recycle list). Verify this exactly.
   - **Page growth**: alloc 200+ entities (crosses multiple 64-entry pages), confirm all are alive, all `Get` return non-nil pointers. Free half (every other one), realloc 100, verify recycled IDs come from the freed pool first before allocating new indices.
   - **Record pointer stability**: alloc one, take pointer from `Get`, alloc 1000 more without freeing, confirm the original pointer still references the same record (because we never moved the page).
   - **`Count` correctness** through a sequence of mixed alloc/free.
   - **`Each` iterates exactly the alive set**, in dense order, no dead entries.
   - **Generation overflow not handled silently**: at gen `0xFFFFFFFF`, the next free should... behavior the implementer must **DECIDE and document**. Recommendation: **panic on generation overflow** (these are 4 billion frees of one slot — practically never; panic is safer than silent wrap). Test with a constructor option or unexported helper that fast-forwards generation to near-max.

### Non-goals

- No `Table` integration. The `Record` struct does not carry a table pointer in this issue; Phase 1.4 adds it.
- No `ecs_id_record_t *idr` field (C optimization for component-id records). Defer to Phase 2 when component index is ported.
- No deferred operations / staging. Single-threaded only.
- No `sync.Pool` or custom allocator. Use plain Go allocation. (Performance optimization later.)
- No iteration-during-mutation safety. Document that `Each` callbacks must not call `Free`/`Alloc` (or, if they can, define semantics — implementer's call, but write it down).
- No cgo, no `reflect`, no `unsafe` (none should be needed).

### Mechanical acceptance

- `go test ./... -race` passes.
- `go vet ./...` passes.
- `golangci-lint run` passes against the existing `.golangci.yml`.
- The `internal/storage/entityindex` package exists with the deliverables above.
- Every exported symbol has a godoc comment.
- Coverage on `entityindex.go` is **≥ 95% statement coverage**.

### Constraints / pointers for the implementer

- **Read the C source in full** before writing Go:
  - `/work/agents/claude/projects/SanderMertens/flecs/src/storage/entity_index.h`
  - `/work/agents/claude/projects/SanderMertens/flecs/src/storage/entity_index.c`
  
  These are filesystem paths (not files in this repo). The Go port follows the same algorithmic shape (paged records + dense vec + recycle-via-tail) but uses Go-native idioms. Drop the `ecs_allocator_t *` parameter (we use Go GC). Drop the `ecs_block_allocator_t page_allocator` (Go's allocator is fine).
- Do **NOT** include a `Table` field in `Record` even though the C source has `ecs_table_t *table`. Phase 1.4 adds it.
- The C entity index reserves index 0 as the null/invalid ID. **Mirror this**: `Alloc()` never returns an ID with index 0; `IsAlive(flecs.ID(0))` is false; `Get(flecs.ID(0))` returns nil.
- Use `flecs.MakeEntity(idx, gen)` from `@id.go` to construct IDs, and `id.Index()` / `id.Generation()` to read them. **Do not bit-twiddle directly.**
- Do **NOT** depend on any package outside the module (no third-party deps).

## Constraints

- `@id.go` — provides `flecs.ID`, `MakeEntity`, `Index`, `Generation`, `WithGeneration`, etc. Build on this; do not duplicate or bypass it.
- `/work/agents/claude/projects/SanderMertens/flecs/src/storage/entity_index.h` — C struct layout and API surface. Mirror the algorithmic shape (paged records, dense vec, recycle tail) in Go idioms.
- `/work/agents/claude/projects/SanderMertens/flecs/src/storage/entity_index.c` — function bodies for the algorithms above.
- `@.golangci.yml` — enabled linters (govet, staticcheck, errcheck, ineffassign, unused, gofmt, goimports). `goimports` `local-prefixes` is `github.com/snichols/flecs` — group imports accordingly.
- `@go.mod` — module path `github.com/snichols/flecs`, Go 1.26.1. No third-party deps.
- Not a bug — foundation work. Issue is labeled `snichols/queued` only.
