## Goal

Implement the **Symmetric** relationship trait: marking a relationship `R` symmetric causes any `(R, B)` added to entity `a` to auto-mirror as `(R, A)` on entity `b`. Same for removal: removing `(R, B)` from `a` removes `(R, A)` from `b`. Symmetric in the mathematical sense: `aRb ⇒ bRa`. Useful for inherently undirected relations like `Friend`, `MarriedTo`, `AlliesWith`, `Coplanar`.

Target version: **v0.36.0**. Continues the small-trait-per-phase pattern from 15.0 (cleanup policies), 15.1 (OnInstantiate Override/DontInherit), 15.2 (Exclusive), 15.3 (CanToggle).

### C grounding

Verified against upstream C at `/work/agents/claude/projects/SanderMertens/flecs`:

- **`include/flecs.h:1816`** — `FLECS_API extern const ecs_entity_t EcsSymmetric;` with docstring \"if R(X, Y) then R(Y, X).\"
- **`src/world.c:48-50`** — `EcsReflexive = FLECS_HI_COMPONENT_ID + 19; EcsSymmetric = FLECS_HI_COMPONENT_ID + 20; EcsFinal = FLECS_HI_COMPONENT_ID + 21;`
- **`include/flecs/private/api_flags.h`** — **no `EcsIdSymmetric` flag bit exists**. Confirmed by exhaustive grep across the C tree: `Symmetric` appears only as the entity ID and the observer functions. Symmetric is implemented purely observer-driven, not via a per-id table flag. (`EcsIdExclusive = 1u<<9`, `EcsIdCanToggle = 1u<<13`, etc., are present; no Symmetric counterpart.)
- **`src/bootstrap.c:343-372`** — `flecs_on_symmetric_add_remove` observer body. Reads the pair, extracts `rel` and `tgt`, then for each subject entity:
  - On `EcsOnAdd`: `if (!ecs_has_id(real_world, tgt, ecs_pair(rel, subj))) { ecs_add_pair(world, tgt, rel, subj); }`
  - On `EcsOnRemove`: `if (ecs_has_id(real_world, tgt, ecs_pair(rel, subj))) { ecs_remove_pair(world, tgt, rel, subj); }`
- **`src/bootstrap.c:375-392`** — `flecs_register_symmetric`: when `EcsSymmetric` is added to a relationship `r`, install an observer on `(r, *)` that fires the mirror function for `EcsOnAdd`/`EcsOnRemove` events.
- **`src/bootstrap.c:1005, 1119-1122`** — `flecs_bootstrap_trait(world, EcsSymmetric)` and the meta-observer registration.

**Loop guard mechanism (the critical correctness piece):** C uses **approach (a) — idempotence via has-check**. When the mirror observer adds `(R, subj)` to `tgt`, it fires the observer recursively on `tgt`. The recursive invocation reads `subj` as the new \"target\" and `e` (the original subject) as the would-be mirror target. It then checks `ecs_has_id(e, pair(R, subj))` — which is true (we just added it) — and short-circuits. Recursion terminates in one extra hop.

For removal, the symmetric guard: after removing `(R, B)` from `a`, the mirror removes `(R, A)` from `b`, which fires `OnRemove` on `b`'s side, which would try to remove `(R, B)` from `a` — but `a` no longer has it (`ecs_has_id` returns false), so the recursive call short-circuits.

### Likely Go shape (subject to refinement during implementation)

1. **New built-in entity** at index 19: `World.Symmetric() ID`. Push first user-entity index from 19 to 20. Update `world.go` registration block, doc comment, and bootstrap.
2. **Policy storage**: `World.symmetricPolicies map[ID]bool` keyed by `ID(relID.Index())` — same pattern as `exclusivePolicies` (`exclusive.go:39-44`) and `canTogglePolicies` (`cantoggle.go:34-39`).
3. **Public API** in new `symmetric.go`:
   - `SetSymmetric(w *World, relID ID)` — marks a relationship symmetric.
   - `IsSymmetric(w *World, relID ID) bool` — inspection.
   - `(w *World) Symmetric() ID` — returns the built-in entity.
4. **Bare-tag handler** in `addIDImmediate` (`id_ops.go`): add an `else if id.Index() == w.symmetricID.Index()` branch alongside the existing Exclusive (`id_ops.go:70-74`) and CanToggle (`id_ops.go:75-79`) branches, calling `applySymmetricPolicy(w, e)`.
5. **`addIDImmediate` mirror hook**: after the migration completes, if `id.IsPair() && w.symmetricPolicies[id.First()]`, also call `addIDImmediate(w, id.Second(), MakePair(id.First(), e))` — guarded by the standard early-return at line 34-36 (`HasComponent` check) which provides the idempotence loop-guard automatically. Verify by walking the call graph: the recursive call will re-enter the symmetric branch, but `HasComponent` returns true on the second hop and the function returns false.
6. **`removeIDImmediate` mirror hook**: after the migration, if `id.IsPair() && w.symmetricPolicies[id.First()]`, also call `removeIDImmediate(w, id.Second(), MakePair(id.First(), e))` — guarded by the early-return at line 161-163 (`!HasComponent` returns false), giving the same idempotence loop-guard for the remove path.
7. **Self-pair handling**: `AddID(a, MakePair(R, a))` — when `e == id.Second()`, the mirror would add `(R, a)` to `a`, which is the same pair already added. The `HasComponent` early-return handles this naturally.
8. **Interaction with Exclusive (15.2)**: when both flags are set on `R`, adding `(R, B)` to `a` (already holds `(R, X)`) replaces with `(R, B)` via the existing exclusive enforcement at `id_ops.go:85-96`. The symmetric mirror then adds `(R, A)` to `b`, which if `b` also held `(R, Y)` exclusively, replaces that too. The test must demonstrate this composes sanely (C's behavior: each side's exclusivity is enforced independently; mirror fires after the exclusive replace finishes).
9. **Hook ordering**: OnAdd / OnRemove should fire for both sides via the standard `migrate` emit path (no extra emission needed) — the mirror call goes through `addIDImmediate` / `removeIDImmediate` which already drives the emit chain.

### Tests in new `symmetric_test.go` (9 cases)

1. **Default behavior unchanged** — non-Symmetric relationship does not auto-mirror.
2. **Mark + Add mirrors** — `AddID(a, MakePair(R, b))` causes `HasID(b, MakePair(R, a)) == true`.
3. **Idempotent** — adding the same pair twice is a no-op (no duplicate hook fires).
4. **Remove mirrors** — `RemoveID(a, MakePair(R, b))` causes `HasID(b, MakePair(R, a)) == false`.
5. **Self-relationship** — `AddID(a, MakePair(R, a))` results in a single pair, not two; `RemoveID` cleans up.
6. **Symmetric + Exclusive (15.2 interaction)** — verify pair replacement propagates correctly through the mirror.
7. **IsSymmetric round-trip** — `SetSymmetric` then `IsSymmetric` returns true; never-set returns false.
8. **Loop guard correctness** — Add and Remove mirror without infinite recursion (correctness test; would hang on broken implementation). Combined Add then Remove returns world to baseline state.
9. **OnAdd/OnRemove hooks fire on both sides** — observers on `b` see the mirrored pair add/remove; no double-fire from the mirror (hook count == 1 per side per operation).

### Docs updates (same PR, per `CONTRIBUTING.md`)

- `docs/Relationships.md:529-534` — Symmetric section: replace \"Not yet ported\" callout (currently points readers to the gap list in `docs/README.md`) with shipped API documentation and a worked example using `MarriedTo`.
- `docs/ComponentTraits.md:481-503` — replace the \"Not yet ported in Go flecs\" callout at line 503 with shipped Go-idiomatic content.
- `docs/ComponentTraits.md:604` — roadmap table row: change \"⏳ planned / No automatic bidirectional pair mirroring\" → \"✅ shipped (v0.36.0)\".
- `docs/README.md:81` — \"Symmetric relationships (`EcsSymmetric`) — not ported.\" → shipped.
- `docs/README.md:119` — full feature-gap row → shipped.
- `CHANGELOG.md` — new entry `## v0.36.0 — Phase 15.4: Symmetric relationship trait` (follow the v0.35.0 entry structure at `CHANGELOG.md:3-35`).
- `ROADMAP.md` — move Symmetric from candidate list into Shipped; update built-in entity count note from 18 → 19; note that user entities now start at index 20.

### Non-goals

- **NO Transitive trait** (`EcsTransitive`) — separate future phase.
- **NO Reflexive trait** (`EcsReflexive`) — separate future phase, even though it is the immediately adjacent C entity ID (FLECS_HI_COMPONENT_ID + 19, same numeric slot we're using in Go for Symmetric; the Go index 19 / C entity ordering need not match).
- **NO `UnsetSymmetric`** / unmarking — same precedent as Exclusive/CanToggle (one-way trait marking).
- **NO wildcard symmetric** — adding `(R, *)` does not mirror; only concrete pair targets.

### Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run ./...` clean.
- `go test ./... -race -count=3` clean.
- Coverage on main package ≥ 95%.
- All existing tests pass without modification.
- New `symmetric_test.go` covers the 9 cases above.
- Docs updates land in the same PR.

## Constraints

- @CONTRIBUTING.md — docs-with-code rule: trait phases ship docs updates in the same PR (precedent: 15.0–15.3).
- @exclusive.go — canonical pattern for trait policy (file structure, `SetXxx` / `IsXxx` / `(w *World) Xxx() ID`, `applyXxxPolicy` helper, index-keyed policy map). Mirror its file layout in the new `symmetric.go`.
- @cantoggle.go — second precedent for the same pattern; confirms the trait module shape.
- @cleanup.go — earlier trait-storage pattern (cleanup policies); shows how bare-tag pair-form handling integrates in `id_ops.go`.
- @instantiate_policies.go — Phase 15.1 pattern for component-vs-relationship trait storage; confirms the per-relationship map keyed by index.
- @world.go — built-in entity registration block (lines 217-228 show Exclusive at 17 and CanToggle at 18); Symmetric goes at 19, doc comment at lines 100-111 updated, bootstrap call inserted, `symmetricID ID` field added to the struct (line ~69), `symmetricPolicies map[ID]bool` field added. Push \"first user entity\" from 19 → 20.
- @id_ops.go — `addIDImmediate` (line 29) and `removeIDImmediate` (line 156): add the bare-tag handler branch (alongside existing Exclusive/CanToggle branches at lines 70-79) and the mirror hook. The existing early-return at lines 34-36 (`HasComponent` check) and 161-163 (`!HasComponent` check) provides the loop-guard idempotence — verify by reading and confirm no additional flag-threading is needed.
- @docs/Relationships.md — has an explicit \"Not yet ported\" callout at lines 529-534 pointing readers to the gap list. Replace with shipped API + worked example.
- @docs/ComponentTraits.md — has the most extensive existing documentation for Symmetric (lines 481-503 + roadmap row at 604). Rewrite section 481-503 with Go-idiomatic content; update row 604.
- @docs/README.md — feature-gap entries at lines 81 and 119 must be moved out of \"not ported\" and into shipped.
- @CHANGELOG.md — append `## v0.36.0` entry following the v0.35.0 entry shape (lines 3-35).
- @ROADMAP.md — move Symmetric from candidate list (currently grouped with Transitive/Reflexive at the implied line 70 area for Phase 15.x candidates) into the Shipped section; bump built-in entity count to 19.

C-grounding (read but not edited; cited inline above):
- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h:1816` — entity declaration with docstring.
- `/work/agents/claude/projects/SanderMertens/flecs/src/world.c:49` — `EcsSymmetric = FLECS_HI_COMPONENT_ID + 20`.
- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs/private/api_flags.h` — confirmed **no** `EcsIdSymmetric` flag bit (exhaustive grep). Symmetric is observer-only in C. Go-flecs deviates by storing the policy in a map (consistent with the existing `exclusivePolicies` / `canTogglePolicies` precedent) — this is a faithful adaptation, not a divergence, because Go-flecs lacks the table-flag fast-path C uses for other traits.
- `/work/agents/claude/projects/SanderMertens/flecs/src/bootstrap.c:343-392` — observer body (`flecs_on_symmetric_add_remove`) and registration (`flecs_register_symmetric`). Confirms loop-guard mechanism: `ecs_has_id`-based idempotence check before mirror add (line 362) and mirror remove (line 366).
- `/work/agents/claude/projects/SanderMertens/flecs/src/entity.c` — checked for any Symmetric handling in the Remove path; none found. All symmetric logic lives in the bootstrap observer.
