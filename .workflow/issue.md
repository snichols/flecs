## Goal

Make pair IDs work end-to-end as first-class signature entries. Phases 1-3 left us with `MakePair(rel, tgt)` already producing a `flecs.ID` with `FlagPair` set and `IsPair/First/Second` decoders — but nothing in the World layer treats pair IDs as legitimate signature entries yet. This phase closes that gap by adding a raw-ID API (`AddID`/`RemoveID`/`HasID`) and typed pair-data accessors (`SetPair[T]`/`GetPair[T]`).

After this phase lands, users can:

```go
ChildOf := w.NewEntity()
Parent := w.NewEntity()

// Pair-as-tag (no data) — useful for ChildOf-style relationships:
flecs.AddID(w, child, flecs.MakePair(ChildOf, Parent))
flecs.HasID(w, child, flecs.MakePair(ChildOf, Parent)) // true
flecs.RemoveID(w, child, flecs.MakePair(ChildOf, Parent))

// Pair-with-data (e.g. relationship with payload):
type Edge struct { Weight float32 }
flecs.SetPair[Edge](w, entityA, FollowsID, entityB, Edge{Weight: 1.5})
e, ok := flecs.GetPair[Edge](w, entityA, FollowsID, entityB)
```

Pairs `(R, T)` and `(R, S)` are distinct IDs — distinct signature entries, distinct archetypes when used alone. Queries match pair IDs directly (no wildcards yet; later phase).

### Deliverables

**1. Raw-ID functions** in `world.go` (or a new `id_ops.go` / `pair.go` — implementer's call). All free functions in package `flecs`:

- `func AddID(w *World, e ID, id ID) bool` — idempotent add. Returns true on first add, false if `e` already had it. Works for component IDs, raw entity IDs used as tags, and pair IDs.
- `func RemoveID(w *World, e ID, id ID) bool` — returns true if `e` had it.
- `func HasID(w *World, e ID, id ID) bool` — true iff `e` is alive and its current table's signature contains `id`.
- All three panic if `e` is not alive. Document.
- No special-casing of pairs vs non-pairs — all just signature entries.
- DELEGATE to the existing `migrate()` plumbing; no new migration logic.

**2. Typed pair-data functions** in the same file:

- `func SetPair[T any](w *World, e ID, rel ID, tgt ID, v T)` — sets data on `MakePair(rel, tgt)`. On first use for that pair-id, auto-register by associating `reflect.TypeFor[T]()` metadata with the pair id via a per-pair-id `*component.TypeInfo`. If the pair-id was previously associated with a DIFFERENT type, panic with a clear message (e.g. `"pair (rel, tgt) was previously associated with type X; cannot SetPair[Y]"`). Panics if `e` is not alive.
- `func GetPair[T any](w *World, e ID, rel ID, tgt ID) (T, bool)` — returns zero, false if `e` is not alive, if the pair is not present, or if the pair-id has been associated with a type other than `T`. Does NOT auto-register (matches `Get[T]` policy).
- **No `HasPair`/`RemovePair` convenience wrappers** — users compose via `HasID(w, e, MakePair(rel, tgt))` and `RemoveID(w, e, MakePair(rel, tgt))`. Document on `MakePair`'s godoc.

**3. Update `internal/component/registry.go`** with a helper for pair-data registration:

- Add `func RegisterPairData[T any](r *Registry, pairID ID) *TypeInfo`:
  1. Calls `Register[T](r)` to ensure T's regular TypeInfo exists (idempotent).
  2. Builds a NEW `*TypeInfo` with the same `Size/Align/Type/Hooks` as T's TypeInfo but with a distinguishing `Name` (e.g. `"pair(" + tInfo.Name + ")"` — implementer's call).
  3. Calls `r.AssociateID(newInfo, pairID)`.
  4. Returns the new TypeInfo.
- Per-pair-id TypeInfo copies are required because `AssociateID` panics on associating multiple IDs with one TypeInfo, and `LookupByType[T]` returns one ID per Go type. We need many pair-ids to coexist with the same data type.
- Alternative considered: relax `AssociateID` to allow many IDs per TypeInfo. **NOT recommended** — breaks `LookupByType` invariants.
- Document on `RegisterPairData`: pointer-distinct, value-equivalent metadata; base T's TypeInfo and ID remain unmodified.
- Confirm and document `AssociateID`'s panic-on-duplicate scope: a different TypeInfo at the same pair-id is a conflict, but the same TypeInfo associated again is idempotent.

**4. Verify storage layers are pair-id-agnostic.** No changes should be needed to `internal/storage/table` or `internal/storage/componentindex` — pair IDs are just IDs. Read the relevant code and confirm there are no `id & ~FlagPair` masks or similar. If there are, fix in this issue.

**5. Verify queries work with pair IDs** — `NewQuery(w, ids...)/Field[T]` should already match. Add a query test:
- Create pair `(R, T)`, set it on 3 entities with `Edge{...}` data.
- `NewQuery(w, MakePair(R, T))` and iterate.
- `Field[Edge](it, MakePair(R, T))` returns correct data for all 3 entities.

**6. Tests** in `pair_test.go` (or new file):
- AddID/HasID/RemoveID with a regular component ID — equivalent to `Set[T]`/`Has[T]`/`Remove[T]` (modulo: AddID adds the column with zero value).
- AddID with a raw entity ID as a tag.
- AddID with a pair ID; HasID true; entity's `Type()` contains pair id; RemoveID removes.
- Distinct pair targets, distinct archetypes — add `(R, A)` to e1, `(R, B)` to e2; assert their tables differ.
- Pair re-registration with same T is idempotent.
- Pair re-registration with different T panics.
- `GetPair` returns zero,false when pair not present.
- `GetPair` returns zero,false when type mismatched.
- Pair data round-trip: `SetPair[Edge](w, e, R, T, Edge{Weight: 1.5})` then `GetPair[Edge]` returns `(Edge{Weight: 1.5}, true)`.
- Pair-with-data archetype migration: add `Position` then pair `(R, T)` with data; both columns coexist.
- Query for pair-id (verification test from item 5).
- AddID on non-alive entity panics.
- No leakage: adding pair `(R, A)` doesn't add `(R, B)` or `R` standalone.

**7. Mechanical acceptance**
- `go test ./... -race -count=2` passes.
- `go vet ./...` clean.
- `golangci-lint run` clean.
- Coverage on `flecs` ≥ 90% (no regression from current 97.9%).
- Coverage on `internal/component` ≥ 95% (currently 100%; new `RegisterPairData` branch must be covered).
- All exported symbols have godoc.

### Non-goals

- NO ChildOf cleanup semantics (Phase 4.2).
- NO IsA inheritance (Phase 4.3).
- NO wildcards `(R, *)`, `(*, T)`, or `(*, *)` matching.
- NO `GetTarget(w, e, rel) → tgt` introspection.
- NO `EachPair`/`EachWithPair` type-parametric helpers; users use `NewQuery(w, MakePair(R, T))/Iter/Field[T]`.
- NO change to existing `Set[T]/Get[T]/Has[T]/Remove[T]` semantics.
- NO observer firing for pair lifecycle events (Phase 5).
- NO special signature ordering for pair IDs — sort by underlying `uint64` alongside non-pair IDs.
- NO pair-ID validity checks (e.g. \"rel must be alive\"). Trust the caller.

### Implementer pointers

- `AddID` shares almost all logic with `Set[T]`'s add path. Refactor cleanly: extract the common migration call.
- `HasID` is trivial: lookup record, check `record.Table.HasComponent(id)`.
- `RemoveID` triggers a migration with `removeID = id`.
- `SetPair[T]` reuses the migration path; new piece is pair-id TypeInfo registration on first use.
- For pair-id TypeInfo, do NOT call `Register[T]` (key-maps T to a single ID). Build a TypeInfo directly and pass to `AssociateID(info, pairID)`. Use the new `RegisterPairData[T]` helper.
- Pair-id TypeInfo can share `Hooks` and `Type reflect.Type` with T's regular TypeInfo — pointer-distinct, value-equivalent. Document.
- DO NOT mutate the existing T-keyed TypeInfo.
- DO NOT introduce a new package.
- DO NOT import any third-party deps.

## Constraints

- @world.go — existing `Set[T]/Get[T]/Has[T]/Remove[T]` migration path is the model for the new raw-ID functions; extract the shared migration call cleanly.
- @query.go — existing `NewQuery/Iter/Field[T]` should already match pair IDs without modification; verification only.
- @each.go — no changes; out of scope for this phase.
- @id.go — `MakePair/IsPair/First/Second/FlagPair` already exist; godoc on `MakePair` should note pair-as-tag composes via `AddID(w, e, MakePair(rel, tgt))` / `HasID` / `RemoveID`.
- @internal/component/registry.go — add `RegisterPairData[T]`. Verify `AssociateID`'s panic-on-duplicate scope (same TypeInfo re-associated is idempotent; different TypeInfo at same id is a conflict). Document.
- @internal/component/typeinfo.go — TypeInfo struct must support being constructed as a per-pair-id copy with shared `Size/Align/Type/Hooks` and a distinct `Name`.
- @internal/storage/table/table.go — verify no `id & ~FlagPair` masks; pair IDs should be opaque signature entries. Read and confirm.
- @internal/storage/componentindex/componentindex.go — same verification; the reverse index keys on `ID` and must treat pair IDs identically.
- C reference (read but do not paraphrase):
  - `/work/agents/claude/projects/SanderMertens/flecs/src/id.c` — pair encoding/validation; mostly mirrored in `id.go`.
  - `/work/agents/claude/projects/SanderMertens/flecs/src/entity.c` — `ecs_add_id`/`ecs_remove_id`/`ecs_has_id` algorithm shape for raw-ID variants.
  - `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` lines 1670-1716 — id flag definitions and pair encoding rules.
  - `/work/agents/claude/projects/SanderMertens/flecs/src/storage/component_index.c` (around `flecs_components_get`) — pair IDs index the same as regular component IDs.
