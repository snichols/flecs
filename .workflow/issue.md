## Goal

Port the upstream `EcsPairIsTag` trait into Go flecs as Phase 15.16, shipping v0.48.0. `PairIsTag` marks a relationship `R` so that every pair `(R, T)` is forced to be tag-only — no per-pair component data is allowed, regardless of whether `R` itself is a data-bearing component type.

The canonical upstream use case: a `Serializable` component that the user wants to *also* be usable as a pair relationship without `(Serializable, Position)` accidentally allocating a second Position-shaped slot per entity. Today in Go flecs the situation is the inverse — pair data only materializes when a caller explicitly invokes `SetPair[T]` / `SetPairByID` / `RegisterPairData[T]`. So PairIsTag in Go is primarily a **defensive declaration**: it makes those value-bearing paths panic on a marked relationship, preventing accidental promotion of an intended tag-pair into a data-pair.

The work is roughly symmetric to the other single-flag relationship traits (Final, Exclusive, Traversable). The interesting design surface is (a) the set-after-use trap, since pair TypeInfo is resolved lazily and cached on first use, and (b) the small bootstrap of marking `IsA` and `ChildOf` as PairIsTag, mirroring `bootstrap.c:1272-1273`.

### Reference upstream sites (cite in implementation)

- `include/flecs.h:1888-1890` — `EcsPairIsTag` constant declaration with the upstream docstring.
- `src/world.c:58` — `EcsPairIsTag = FLECS_HI_COMPONENT_ID + 28`.
- `src/bootstrap.c:270-290` — `flecs_register_tag` observer: ensures all id records under a newly-PairIsTag relationship have `type_info = NULL`, and asserts the relationship has not yet been used (`flecs_assert_relation_unused`).
- `src/bootstrap.c:897` — `flecs_bootstrap_make_alive` of EcsPairIsTag.
- `src/bootstrap.c:951-958` — observer registration (yield-existing pattern).
- `src/bootstrap.c:1009` — `flecs_bootstrap_trait(world, EcsPairIsTag)`.
- `src/bootstrap.c:1271-1277` — bootstrap PairIsTag onto `IsA`, `ChildOf`, `SlotOf`, `DependsOn`, `Flag`, `With`. **Go has IsA and ChildOf only; SlotOf/DependsOn/Flag/With are not yet ported.** Bootstrap only the two Go has.
- `src/id.c:300-334` — `ecs_id_is_tag` consults `EcsPairIsTag` when classifying a non-wildcard pair.
- `src/type_info.c:740-779` — when a relationship's component type is initialized, the per-relationship type info is **not** propagated to `(R, *)` id records when `is_tag` is true; existing pair records have their `type_info` set to NULL.
- `src/type_info.c:823-849` — `flecs_determine_type_info_for_component`: returns NULL (no data) when `ecs_owns_id(world, rel, EcsPairIsTag)` for a pair id.
- `src/storage/table.c:309-310` — table builds `EcsIdPairIsTag` flag when the trait id appears in the type.

## Constraints

- @final.go — simplest single-flag trait template; `IsFinal(scope, ...)` scope-interface signature is the convention to copy.
- @exclusive.go — relationship trait with write-time enforcement; bare-tag `applyExclusivePolicy` dispatch pattern in `addIDImmediate`.
- @usage_constraints.go — most recently shipped (Phase 15.15, v0.47.0, commit `e47729a`); idiom for multi-built-in policy maps and `applyXPolicy` helpers.
- @traversable.go — Phase 15.14 query-time enforcement example; demonstrates `IsX(s scope, ...)` plus `applyTraversablePolicy` plus bare-tag dispatch.
- @reflexive.go — most recently shipped relationship trait that bootstraps onto a built-in (`IsA`); mirrors how this issue will bootstrap onto `IsA` + `ChildOf`.
- @id_ops.go — `addIDImmediate` is the bare-tag dispatch site; new branch needed at line ~129 area: `else if id.Index() == w.pairIsTagID.Index() { applyPairIsTagPolicy(w, e) }`.
- @value_ops.go — `SetByID` / `setByIDImmediate` and `setImmediateByPtr` are where value-bearing writes flow. The enforcement hook lives in `setPairImmediate[T]` (id_ops.go:288) and `SetPairByID` (value_ops.go:69) and the deferred `SetPair[T]` path (scope.go:468) — all three call `RegisterPairData*` and must check `IsPairIsTag(rel)` first.
- @scope.go — confirms the `scope` interface (`scopeWorld() *World`) used since Phase 15.8; `Is*` getters take `scope`.
- @world.go — built-in entity allocation block (lines 320-354); add Allocator block for `pairIsTagID` BEFORE Wildcard. Policy map declarations live around lines 84-101.
- @marshal.go — built-in skip set at lines 101-134; add `w.PairIsTag(): {}`.
- @meta_test.go — `builtinEntityCount = 32` constant at line 18; comment-of-record at lines 11-17 enumerates each index. Update both the constant and the inline list.
- @docs/ComponentTraits.md — Section at lines 505-523 needs to flip from \"Not yet ported\" to \"Shipped in v0.48.0\" with Go usage example; roadmap row at line 889 flips to ✅ shipped. The workaround note at line 520 about zero-size struct types remains valid for the simpler case.
- @docs/README.md — Feature-gap list entry at line 125 needs to be flipped/removed.
- @ROADMAP.md — \"Shipped (through v0.47.0)\" heading at line 3 bumps to v0.48.0; add a new bullet after line 52 for Phase 15.16.
- @CHANGELOG.md — Add a v0.48.0 entry at the top.
- @CONTRIBUTING.md — Section \"Documentation\" (lines 58-81) lists the mandatory doc surfaces for every shipped phase: godoc, doc.go, README.md, docs/ page, CHANGELOG.md, ROADMAP.md.

## Deliverables

### 1. `pairistag.go` (new file)

Single-flag trait module following the `final.go` shape:

- `World.PairIsTag() ID` — built-in entity getter.
- `SetPairIsTag(w *World, relID ID)` — marks the relationship. Idempotent. Panics with a clear message if `relID` has already been used as the relationship of any pair on any entity (the \"set after use\" trap — see Deliverable 2).
- `IsPairIsTag(s scope, relID ID) bool` — uses `scope` per Phase 15.8 convention. **Open decision (3) recommendation: accept `Pair(R, T)` too and answer based on the relationship side, for ergonomic symmetry with `IsTrait`/`IsRelationship`.**
- `applyPairIsTagPolicy(w *World, relID ID)` — internal helper that writes `w.pairIsTagPolicies[ID(relID.Index())] = true`. Called by `SetPairIsTag` and by the bare-tag dispatch in `addIDImmediate`.

### 2. Built-in entity wiring and storage decision

- `World.pairIsTagPolicies map[ID]bool` declared alongside the other policy maps in `world.go` (~line 84-101).
- `World.pairIsTagID ID` declared in world.go's built-in-id block.
- Allocation in `world.go` `New()`: **insert the new built-in allocation BEFORE Wildcard**. Current state per `meta_test.go:11-18`:
  - 27=Traversable, 28=Relationship, 29=Target, 30=Trait, 31=Wildcard, 32=Any, user starts at 33.
  - After this phase: 27=Traversable, 28=Relationship, 29=Target, 30=Trait, **31=PairIsTag**, 32=Wildcard, 33=Any, user starts at 34.
  - `builtinEntityCount` 32 → 33.
- Bootstrap PairIsTag onto `IsA` and `ChildOf` (mirroring upstream bootstrap.c:1272-1273; SlotOf/DependsOn/Flag/With are skipped because they are not ported).

**Set-after-use trap.** Pair TypeInfo registration is lazy: a pair has tag-form storage until someone calls `RegisterPairData[T]` / `RegisterPairDataByType` / `SetPair[T]` / `SetPairByID`. Once that registration happens for `(R, T)`, the per-pair `TypeInfo` is cached in the component registry with non-zero size, and existing entity records that hold the pair would still have their old (tag) storage. Mirroring upstream `flecs_assert_relation_unused`, `SetPairIsTag(R)` must reject the case where the relationship has already been promoted to data form:

- On `SetPairIsTag(R)`: iterate `w.registry.IDs()` and panic if any registered ID is a pair `(R, *)` with non-zero size TypeInfo. Message: `\"flecs: cannot mark <R> as PairIsTag: pair (<R>, <T>) already has data registered\"`.
- Bare-tag adds of `(R, *)` are unaffected — they go through `EnsureID` which creates a zero-size tag TypeInfo. The trap only fires when data registration has occurred.

### 3. Write-time enforcement

The enforcement hook lives at three call sites that all funnel into `RegisterPairData*`:

- **`setPairImmediate[T]` (id_ops.go:288)** — called by `SetPair[T]` on the immediate path. Insert `checkPairIsTag(w, rel)` before `RegisterPairData[T]`.
- **`SetPairByID` (value_ops.go:69)** — dynamic dispatch. Insert `checkPairIsTag(w, rel)` before the `RegisterPairDataByType` call.
- **`SetPair[T]` deferred path (scope.go:475)** — the deferred path also calls `RegisterPairData[T]` to size the value payload before queuing. The check must fire here too, at coalesce or at write-time. Recommendation: panic at the deferred *enqueue* site so it surfaces at the same call site the user typed, matching how other traits surface in the deferred path.

`checkPairIsTag(w, rel)` panics with: `\"flecs: cannot set data on pair (<rel>, <tgt>): <rel> has the PairIsTag trait\"`.

**The bare-tag `AddID(e, MakePair(R, T))` path is unchanged** — that path calls `EnsureID(pairID)` which creates a tag TypeInfo. PairIsTag only rejects value-bearing operations.

### 4. Bare-tag dispatch

Add a branch in `addIDImmediate` (id_ops.go ~line 129, alongside the existing Relationship/Target/Trait branches):

```go
} else if id.Index() == w.pairIsTagID.Index() {
    applyPairIsTagPolicy(w, e)
}
```

This handles `fw.AddID(myRelID, w.PairIsTag())` as equivalent to `flecs.SetPairIsTag(w, myRelID)`, matching upstream `ecs_add_id(world, R, EcsPairIsTag)`.

### 5. `pairistag_test.go` (new file)

At least 8 test cases; aim for coverage ≥ 95.0% on `pairistag.go`:

1. **Tag-form add is unaffected.** `SetPairIsTag(R)` then `fw.AddID(e, MakePair(R, T))` succeeds; verify `r.HasID(e, MakePair(R, T))`. Verify no per-pair TypeInfo with non-zero size exists in the registry.
2. **Value-bearing SetPair panics.** `SetPairIsTag(R)` then `flecs.SetPair[Position](fw, e, R, T, Position{X:1})` panics with the expected message.
3. **Value-bearing SetPairByID panics.** Same as (2) but via `w.SetPairByID(e, R, T, Position{X:1})`.
4. **Pre-existing pair data blocks SetPairIsTag.** `flecs.SetPair[Position](fw, e, R, T, Position{X:1})` then `SetPairIsTag(R)` panics with `\"already has data registered\"` message naming both the pair and the data type. Verify the original pair value is still readable (no state damage).
5. **Bare-tag dispatch via AddID.** `fw.AddID(R, w.PairIsTag())` then `IsPairIsTag(fr, R) == true`.
6. **Idempotent + round-trip.** Two consecutive `SetPairIsTag(R)` calls are no-ops; `IsPairIsTag` returns true.
7. **Bootstrap built-ins.** `IsPairIsTag(fr, w.IsA()) == true` and `IsPairIsTag(fr, w.ChildOf()) == true` immediately after `New()`.
8. **Composition with Exclusive.** `SetExclusive(R)` + `SetPairIsTag(R)`. `AddID(e, Pair(R, T1))` then `AddID(e, Pair(R, T2))` — T1 pair is replaced by T2 (Exclusive), no data column allocated either time. Verify `HasID(e, Pair(R, T1)) == false` and `HasID(e, Pair(R, T2)) == true`.
9. **Deferred SetPair coalesce-time panic.** `w.Write(func(fw) { flecs.SetPair[Position](fw, e, R, T, v) })` after `SetPairIsTag(R)` panics; verify the panic surfaces at the SetPair call site (immediate-style) per Deliverable 3 recommendation.
10. **Remove still works.** `SetPairIsTag(R)` + `AddID(e, Pair(R, T))` + `RemoveID(e, Pair(R, T))` clears the pair.
11. **`IsPairIsTag` on a pair id.** Per Open Decision (3), `IsPairIsTag(fr, MakePair(R, T))` returns the same answer as `IsPairIsTag(fr, R)`. Document the choice.

### 6. Documentation surfaces

Per CONTRIBUTING.md:58-81 (mandatory doc surfaces for every shipped phase):

- **`docs/ComponentTraits.md:505-523`** — flip the PairIsTag section to \"Shipped in v0.48.0\". Add a Go usage example matching the section style of Reflexive (lines 526-549). Keep the workaround note at line 520 — it remains valid as guidance for users who can use a zero-size struct relationship.
- **`docs/ComponentTraits.md:889`** — flip the roadmap-table row to ✅ shipped (v0.48.0) with a one-line summary matching the Final/OneOf row style.
- **`docs/README.md:125`** — feature-gap entry for PairIsTag: flip to shipped or remove.
- **`README.md`** — feature list bump if PairIsTag appears in the headline coverage table (verify before filing).
- **`CHANGELOG.md`** — Add v0.48.0 entry at top with the same level of detail as v0.47.0 (Phase 15.15) entry.
- **`ROADMAP.md`** — Line 3 heading bumps to \"through v0.48.0\". Add a new bullet after line 52 mirroring the Phase 15.15 entry's structure (API signatures, built-in index, what's bootstrapped, divergences from C, built-in entity count bump).

### 7. Marshal + baseline test fixups

- `marshal.go:101-134` — add `w.PairIsTag(): {}` to the skip map.
- `meta_test.go:11-18` — bump `builtinEntityCount` 32→33; update the inline enumeration comment to list PairIsTag at index 31 and shift Wildcard→32, Any→33.
- `marshal_test.go`, `isa_test.go` — search for hardcoded `32` / `Wildcard` / `Any` index references and fix.

## Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage on `pairistag.go` ≥ 95.0%

## Non-goals

- Do NOT bundle DontFragment, Sparse, Union, OrderedChildren, or With.
- Do NOT retroactively rewrite existing pair storage when `SetPairIsTag(R)` is called — panic on the set-after-use case per Deliverable 2.
- Do NOT change the behavior of zero-size struct relationships — they continue to work without PairIsTag.

## Open decision points for the iterate agent

1. **Should `SetPairIsTag` on a non-component entity (an entity with no associated data type) be a no-op or panic?** Recommendation: **no-op (idempotent).** Marking a tag-only relationship as PairIsTag does no harm — the relationship was already tag-form — and a defensive caller may want to declare the intent regardless. Document the choice in the godoc.
2. **Should `(R, Wildcard)` queries iterate tag-form pairs differently?** Recommendation: **no.** Wildcard pair iteration already handles tag pairs uniformly. Add one assertion in a test if convenient, otherwise out of scope.
3. **`IsPairIsTag(Pair(R, T))` accepting a pair id, not a bare relationship.** Recommendation: **yes** — look up the relationship side and answer, matching the ergonomic shape of `IsTrait`/`IsRelationship`. Document in godoc.

## Process

- Feature, not bug.
- Verify all `@`-references resolve to real files/line ranges before starting work.
- Phase: 15.16. Target version: v0.48.0.
