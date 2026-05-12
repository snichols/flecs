## Goal

Implement the **Final** trait for v0.42.0 — marks an entity as terminal so it cannot be used as the target of an `IsA` relationship. Continues the trait-system roadmap (Phases 15.0–15.9) and resolves the `⏳ planned` row in @docs/ComponentTraits.md.

**Use case:** when a concrete prefab is not meant to be a base class for other prefabs, mark it Final to enforce the boundary at add-time.

**C grounding (verified):**

- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h:1771–1781` — `EcsFinal` documented as: "Ensure that an entity or component cannot be used as a target in an `IsA` relationship... if `IsA(X, Y)` and `Final(Y)` throw error."
- `/work/agents/claude/projects/SanderMertens/flecs/src/world.c:50` — `EcsFinal = FLECS_HI_COMPONENT_ID + 21`.
- `/work/agents/claude/projects/SanderMertens/flecs/src/bootstrap.c:874, 1007, 1071` — `EcsFinal` is `flecs_bootstrap_make_alive` + `flecs_bootstrap_trait`-registered; no built-in ships Final by default.
- `/work/agents/claude/projects/SanderMertens/flecs/src/storage/component_index.c:447–453` — enforcement is exactly: when adding `(IsA, tgt)`, if `ecs_has_id(world, tgt, EcsFinal)` then `ecs_throw(ECS_CONSTRAINT_VIOLATED, ...)`. **There is no `EcsIdFinal` flag bit** — Final is stored as a plain tag on the target entity (the api_flags.h check returned no results).

**Built-in index allocation:**

@world.go currently allocates Acyclic at index 22 (Phase 15.9 shipped), Wildcard at 23, Any at 24, user entities at 25+. Final goes at **index 25**, shifting Wildcard→26, Any→27, user entities→28+.

**Likely shape:**

1. New world field `finalID ID` (built-in Final trait entity, index 25) plus accessor `World.Final() ID`.
2. New per-entity flag map `World.finalPolicies map[ID]bool` (naming parallel to `reflexivePolicies`, `acyclicPolicies`).
3. New file `final.go` with public API:
   - `flecs.SetFinal(w *World, entityID ID)` — marks an entity as Final (uses an unexported `applyFinalPolicy` helper, matching the @reflexive.go / @acyclic.go pattern).
   - `flecs.IsFinal(s scope, entityID ID) bool` — inspection; takes the `scope` interface per Phase 15.8 convention so it works inside Write blocks without `AsReader()`.
4. Enforcement hook in @id_ops.go `addIDImmediate`: when adding `id` that is a pair `(IsA, target)`, check `w.finalPolicies[target]`; if true, panic with a message modeled on the C error (`"cannot add (IsA, %v): %v has the Final trait"`). This single hook covers both code paths because the deferred queue in @cmd_queue.go flushes `cmdAddID` through `addIDImmediate` (cmd_queue.go:331) and pair-creation through the same function (cmd_queue.go:244).
5. Bootstrap registration in @world.go's `New()`: allocate the Final entity at index 25; update the index-allocation comment block; bump the documented indices for Wildcard, Any, and user entities; leave no built-in marked Final (matching C bootstrap).

**Tests** — new file `final_test.go` with 8 cases:

1. Default behavior unchanged: an entity not marked Final accepts `(IsA, target)` adds.
2. `SetFinal(w, base); AddID(child, MakePair(IsA, base))` panics with the expected message (immediate path).
3. Adding a non-`IsA` pair (e.g. `ChildOf`) to a Final-marked entity is fine — Final only gates `IsA` targets.
4. Adding `IsA` to a non-Final target is fine (negative control).
5. `IsFinal` round-trip: `SetFinal` then `IsFinal` returns true; unset entity returns false; works through both `*Reader` and `*Writer` via `scope`.
6. Final + Reflexive composition: marking a Reflexive relationship's target Final is irrelevant because reflexive self-match is implicit (no `(IsA, self)` add is performed); verify no spurious panic.
7. Self-`IsA`-add to a Final entity (`AddID(e, MakePair(IsA, e))` where `e` is Final) — edge case; match C behavior (C's check `ecs_has_id(world, tgt, EcsFinal)` fires regardless of whether `tgt == src`, so we panic).
8. Deferred path: inside `w.Write(func(fw *Writer) { fw.AddID(child, MakePair(IsA, finalBase)) })` the panic surfaces when the command flushes through `addIDImmediate`.

**Docs to update:**

- @docs/ComponentTraits.md — flip the Final row in the roadmap table from `⏳ planned` to shipped; expand the existing Final section (lines 358–371) with the Go API (`SetFinal`/`IsFinal`/`World.Final()`) and a working Go example replacing the C snippet.
- @docs/Relationships.md — note Final as the inverse boundary to `IsA` inheritance.
- @docs/PrefabsManual.md — short subsection explaining how to seal a concrete prefab with `SetFinal`.
- @docs/README.md — link to the new Final section.
- @CHANGELOG.md — v0.42.0 entry covering the API, the index reshuffle (Wildcard→26, Any→27, user→28), and the divergence note (none — matches C semantics).
- @ROADMAP.md — add Phase 15.10 Shipped bullet under \"Shipped (through v0.42)\" mirroring the 15.7/15.9 style.

## Constraints

- @cleanup.go — pattern reference: per-entity flag map + apply helper.
- @instantiate_policies.go — pattern reference: trait policy storage.
- @exclusive.go — pattern reference: trait setter + inspector + apply helper.
- @cantoggle.go — pattern reference.
- @symmetric.go — pattern reference.
- @transitive.go — pattern reference.
- @reflexive.go — closest analogue; uses `applyReflexivePolicy` and stores into `w.reflexivePolicies`. Mirror this structure for `applyFinalPolicy` / `w.finalPolicies`.
- @acyclic.go — Phase 15.9 analogue; like Final, it's a per-target policy with a write-time check (Acyclic checks reachability before adding `(R, tgt)`; Final checks the Final tag before adding `(IsA, tgt)`). Same enforcement seam in `addIDImmediate`.
- @world.go — built-in entity registration; **index 25 for Final**, shift Wildcard/Any/user accordingly; update the documentation comment block.
- @id_ops.go — `addIDImmediate` is the single enforcement seam covering both immediate and deferred adds (C precedent: component_index.c:447–453 throws on the same pair-add path).
- @cmd_queue.go — no direct edits expected; both `cmdAddID` (line 331) and pair-creation (line 244) already route through `addIDImmediate`, so Final enforcement comes for free on the deferred path.
- @docs/Relationships.md — document Final as the `IsA`-extension boundary.
- @docs/ComponentTraits.md — Final is cited as `⏳ planned` (line 678); flip to shipped and expand the existing Final section with Go API + example.
- @docs/PrefabsManual.md — add a short \"Sealing prefabs with Final\" subsection.
- @docs/README.md — link to the new Final docs.
- @CHANGELOG.md — v0.42.0 entry.
- @ROADMAP.md — Phase 15.10 shipped bullet under \"Shipped (through v0.42)\".
- All new read free-functions accept `scope` (per Phase 15.8 convention, see ROADMAP line 45). `IsFinal(s scope, entityID ID) bool` — not `IsFinal(*Reader, ID)`.
- Match C semantics: no `EcsIdFinal` flag bit (plain tag on the target); no built-in ships Final; enforcement fires regardless of whether `src == tgt` in `(IsA, tgt)`.

**Non-goals:**

- No retroactive enforcement (existing `(IsA, X)` edges where `X` is later marked Final are not removed).
- No automatic Final propagation along `IsA` chains.
- No query-side optimization using Final (C uses it in `query/validator.c:849` to suppress IsA-substitution; out of scope here — write-time enforcement only for v0.42.0).

**Mechanical acceptance:** `go vet ./...` and `golangci-lint run` clean; `go test ./... -race -count=3` passes; coverage ≥ 95%; existing tests pass after the index reshuffle; new `final_test.go` contains the 8 cases above; docs updated.
