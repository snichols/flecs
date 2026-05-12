## Goal

Implement the **OneOf** relationship trait — the next item on the trait-system roadmap (`docs/ComponentTraits.md:703` ⏳ planned). OneOf constrains a relationship's target to entities that are direct `ChildOf` a specified parent, enabling enum-style relationships such as `(Color, red|green|blue)` where the valid targets are children of a `Colors` namespace entity.

This continues the trait-system march: Exclusive (15.2) → CanToggle (15.3) → Symmetric (15.4) → Transitive (15.5) → Reflexive (15.7) → Acyclic (15.9) → Final (15.10) → **OneOf (15.11)**. Target version: **v0.43.0**.

### C semantics (research grounding)

`include/flecs.h:1847-1856` documents two forms of `EcsOneOf`:

- **Self-tag**: `OneOf(R)` (i.e. `R` carries the bare `EcsOneOf` tag) — target must be a child of `R` itself.
- **Pair**: `OneOf(R, O)` (i.e. `R` carries a `(EcsOneOf, O)` pair) — target must be a child of `O`.

The resolver in `src/world.c:1243-1256` (`flecs_get_oneof`) implements both forms:

```c
if (ecs_has_id(world, e, EcsOneOf)) {
    return e;                                  // self-tag form
} else {
    return ecs_get_target(world, e, EcsOneOf, 0); // pair form
}
```

The actual constraint check at write/query time is in `src/storage/component_index.c:418-441`:

```c
ecs_entity_t oneof = flecs_get_oneof(world, rel);
if (oneof) {
    if (!ecs_has_pair(world, tgt, EcsChildOf, oneof)) {
        ecs_throw(ECS_CONSTRAINT_VIOLATED, ...);
    }
}
```

Note: the check is **direct** `(ChildOf, oneof)` on `tgt`, **not** transitive ancestor traversal. The same direct semantics will be used in Go.

C bootstrap (`src/bootstrap.c:1014, 1325`) registers `EcsOneOf` as a trait and marks it `Exclusive` (a relationship can carry only one `(OneOf, P)` pair at a time). This Go phase relies on the existing Phase 15.2 Exclusive enforcement to provide that property automatically when we map `SetOneOf` onto pair storage — or it uses the simpler map form (see Design below).

### Design

**1. New built-in entity** — `World.OneOf() ID` at **index 24** (Final at 23; Wildcard shifts to 25; Any to 26; user entities now start at index 27). Update the index allocation block and the comment table in `world.go:103-130`. Increment the doc comment to `Index 24: OneOf built-in trait entity`.

**2. Per-relationship constraint storage** — `World.oneOfPolicies map[ID]ID` keyed by `relID.Index()` → parent entity `ID`. Map form (option a in the spec) chosen over pair-storage form (option b) because:
- O(1) lookup at every relationship-pair `AddID` (the hot path)
- Avoids needing to re-derive Exclusive semantics manually (the map naturally permits at-most-one parent per relationship)
- Consistent with the existing trait-policy maps (`exclusivePolicies`, `transitivePolicies`, `reflexivePolicies`, `acyclicPolicies`, `finalPolicies`)

Sentinel for the self-tag form: store `relID` itself as the parent (matches `flecs_get_oneof` returning `e` when self-tagged). Lookup at enforcement time:

```go
parent, ok := w.oneOfPolicies[ID(relID.Index())]
if !ok { /* no constraint */ }
// parent == relID means self-tag form; parent != relID means pair form
```

**3. Public API** (`oneof.go`, new file):

```go
func (w *World) OneOf() ID { return w.oneOfID }

// SetOneOf constrains relID's targets to direct children of parentID.
// Passing parentID == relID encodes the self-tag form (target must be
// a direct child of relID itself).
func SetOneOf(w *World, relID, parentID ID)

// IsOneOf reports whether relID has a OneOf constraint, and if so returns
// the required parent. Accepts scope per Phase 15.8 convention.
func IsOneOf(s scope, relID ID) (parent ID, ok bool)

// applyOneOfPolicy is the internal hook; called by SetOneOf and by
// addIDImmediate when the bare OneOf tag or (OneOf, parent) pair is added.
func applyOneOfPolicy(w *World, relID, parentID ID)

// checkOneOf panics if (relID, target) violates relID's OneOf constraint.
func checkOneOf(w *World, relID, target ID)
```

Two AddID surfaces to recognise in `id_ops.go addIDImmediate`:
- Bare tag: `fw.AddID(relID, w.OneOf())` → calls `applyOneOfPolicy(w, relID, relID)` (self-tag form).
- Pair: `fw.AddID(relID, MakePair(w.OneOf(), parent))` → calls `applyOneOfPolicy(w, relID, parent)` (pair form).

**4. Enforcement in `addIDImmediate`** — when adding pair `(R, target)` to entity `e`:

```go
if id.IsPair() {
    if parent, ok := w.oneOfPolicies[ID(id.First().Index())]; ok {
        checkOneOf(w, id.First(), id.Second())  // verifies ParentOf(target) == parent
    }
}
```

`checkOneOf` uses the existing `World.ParentOf` (`childof.go:40`) to fetch the target's direct parent and compare. Wildcard/Any targets are exempt (consistent with C `ecs_id_is_wildcard` skip in `component_index.c:416`). Self-tag form: compare against `relID`; pair form: compare against the stored parent. Error message mirrors C's two variants (`OneOf` self-tag vs `(OneOf, P)` pair).

**5. Single code path** — same observation as Final/Acyclic precedents: the bare-tag and pair forms converge into one map entry and one enforcement site. No separate paths needed.

**6. Tests** (`oneof_test.go`, 8 cases):

1. `TestOneOf_Default_NoConstraint` — without `SetOneOf`, any `(R, x)` add succeeds.
2. `TestOneOf_PairForm_ValidTargetSucceeds` — `SetOneOf(w, R, Colors)`; `AddID(e, MakePair(R, red))` where `red` is `ChildOf Colors` succeeds.
3. `TestOneOf_PairForm_InvalidTargetPanics` — same setup; `AddID(e, MakePair(R, stranger))` where `stranger` is not a child of `Colors` panics with constraint-violated message.
4. `TestOneOf_SelfTagForm_ValidTargetSucceeds` — `SetOneOf(w, R, R)` (self-tag); `AddID(e, MakePair(R, child))` where `child` is `ChildOf R` succeeds.
5. `TestOneOf_GetOneOf_RoundTrip` — `SetOneOf(w, R, P)` then `IsOneOf(w, R)` returns `(P, true)`; unconfigured relation returns `(0, false)`.
6. `TestOneOf_BootstrappedRelationships_NoConstraint` — `IsOneOf(w, w.IsA())` and `IsOneOf(w, w.ChildOf())` return `(0, false)` (no built-in ships OneOf, matching C).
7. `TestOneOf_WithExclusive_AtomicReplacement` — relationship `R` is both `OneOf(Colors)` and `Exclusive`; replacing `(R, red)` with `(R, blue)` (both valid children of `Colors`) migrates atomically without re-violating the constraint.
8. `TestOneOf_BareTagAddID` — `fw.AddID(R, w.OneOf())` (bare tag) is equivalent to `SetOneOf(w, R, R)`; subsequent `fw.AddID(R, MakePair(w.OneOf(), parent))` updates to pair form.

**7. Documentation updates**

- `docs/Relationships.md` — new subsection with enum-style example (`Colors → red, green, blue` pattern).
- `docs/ComponentTraits.md:703` — flip OneOf row to ✅ shipped; also update the `OneOf` section detail (currently at line 403) from planned-prose to landed-prose with Go API examples.
- `docs/README.md:168` — remove the \"not yet ported in Go flecs\" suffix.
- `CHANGELOG.md` — new `## v0.43.0 — Phase 15.11: OneOf relationship trait` entry.
- `ROADMAP.md` — append to Shipped section between Phase 15.10 and the trait-system gap list.

### Non-goals

- **No automatic parent-entity creation**: the user supplies the parent entity (e.g. they must create `Colors` and parent `red`/`green`/`blue` to it themselves). Matches C.
- **No transitive ancestor check**: the constraint is a direct `(ChildOf, parent)` check, not a walk up the ChildOf chain. Matches C `ecs_has_pair` semantics in `component_index.c:420`.
- **No removal API in this phase**: `SetOneOf` is one-shot. (Removal could be added later; consistent with how other trait setters work today.)

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0% (current baseline from Phase 15.7)
- All existing tests still pass
- New file `oneof_test.go` with exactly 8 `Test*` cases
- Docs updated as listed above

## Constraints

- @cleanup.go — pattern: per-entity policy map keyed by `ID.Index()`, applied via `applyXxxPolicy(w, e, ...)` helper, enforced inline in `addIDImmediate`
- @instantiate_policies.go — pattern: pair-form policy registration (`(OnInstantiate, action)`) translated into a flag map at AddID time; OneOf's pair-form follows the same shape
- @exclusive.go — pattern: bare-tag AddID maps to `applyExclusivePolicy(w, e)`; OneOf bare-tag form follows the same shape, and OneOf-constrained Exclusive relations must compose correctly (test case 7)
- @cantoggle.go — pattern: simplest bool-map trait; OneOf's storage is similar but holds an `ID` value rather than a bool
- @symmetric.go — pattern: bool-map trait with mirror-write side-effect; OneOf has no mirror side-effect but shares the AddID-hook shape
- @transitive.go — pattern: bool-map trait with query-time consumer (matchers); OneOf is write-time only, so simpler
- @reflexive.go — pattern: bool-map trait with HasID-time side-effect; OneOf is write-time only
- @acyclic.go — pattern: write-time constraint check (`checkAcyclic` called inline from `addIDImmediate` for relationship pairs); closest structural analogue for the enforcement site
- @final.go — closest precedent (v0.42.0, just shipped): write-time check, scope-accepting `IsFinal`, bare-tag AddID maps to policy-apply, single code path. OneOf differs only in storing an `ID` value instead of a bool and in the enforcement predicate (ParentOf check vs Final-flag check)
- @world.go — built-in entity registration: add `oneOfID` field, allocate at index 24, shift `wildcardID` to 25 and `anyID` to 26, update doc-comment table at lines 103-130, no built-in relationship ships OneOf by default (matches C)
- @id_ops.go — `addIDImmediate` enforcement site: add a bare-tag branch (`id.Index() == w.oneOfID.Index()` → `applyOneOfPolicy(w, e, e)`), a pair-form branch (`id.IsPair() && id.First().Index() == w.oneOfID.Index()` → `applyOneOfPolicy(w, e, id.Second())`), and the relationship-pair enforcement (`if parent, ok := w.oneOfPolicies[ID(id.First().Index())]; ok { checkOneOf(w, id.First(), id.Second()) }`). Place the enforcement after the Acyclic/Final blocks and before the Exclusive migration block so replacement targets are validated before migration
- @childof.go — provides `World.ParentOf(e) (ID, bool)`; `checkOneOf` uses this to look up the target's direct parent. Direct check only — no transitive walk
- @docs/Relationships.md — add a OneOf subsection with enum-style worked example
- @docs/ComponentTraits.md — flip line 703 row to ✅ shipped; rewrite the prose section near line 403 with the Go API
- @docs/README.md — remove the \"not yet ported\" note at line 168
- @CHANGELOG.md — new v0.43.0 entry
- @ROADMAP.md — Shipped section entry between 15.10 and the gap list
- `IsOneOf` accepts `scope`, not `*Reader` or `*Writer` (Phase 15.8 convention, see @final.go for the closest example)
- `oneof_test.go` is a NEW file with exactly 8 test cases enumerated above
