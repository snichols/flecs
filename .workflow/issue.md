## Goal

Bundle two strongly-related system-scheduling gaps that share a single piece of infrastructure (the DependsOn ordering graph and its topological sort):

- **Custom pipeline phases** — `docs/README.md` line 141. Today Go-flecs has exactly four hard-coded built-in phases (`PreUpdate`, `OnFixedUpdate`, `OnUpdate`, `PostUpdate`). Upstream C flecs lets any entity tagged with `EcsPhase` (`include/flecs.h:2012`) be a pipeline phase, with phase order driven by `EcsDependsOn` pairs (`include/flecs.h:1908`). Upstream ships 11 built-in phases (`EcsOnStart`, `EcsPreFrame`, `EcsOnLoad`, `EcsPostLoad`, `EcsPreUpdate`, `EcsOnUpdate`, `EcsOnValidate`, `EcsPostUpdate`, `EcsPreStore`, `EcsOnStore`, `EcsPostFrame` — `include/flecs.h:2001-2011`).
- **DependsOn ordering between systems** — `docs/README.md` line 142. Today Go-flecs orders within a phase strictly by system registration order (`system.go:333`, `system.go:465`). Upstream lets applications attach `(DependsOn, OtherSystem)` to order two systems within a phase independently of registration order.

After this phase ships both lines flip to `✅ shipped (v0.64.0)`.

### Design path: A (full divergence — recommended) over B (partial promotion)

Phase 16.3 (`*System` struct, v0.58.0), Phase 16.5/16.6 (observer disabling, rate filters), and Phase 16.8 (`RegisterEvent` entity allocation for custom events, v0.63.0 — commit 4e4bab5) all established that Go-flecs models system-side concepts as Go structs, not entities. Phase 16.8 specifically demonstrated entity allocation for non-component identifiers but kept the *observer* abstraction as `*Observer`. The same shape applies here.

**Path A — adopted**: introduce a `*Phase` Go struct alongside `*System`. Ordering is declared via methods — `phase.DependsOn(other)` and `system.DependsOn(other)`. Topo-sort runs at pipeline-build time. `*Phase` is *not* backed by an entity for v1.

**Path B — considered and rejected**: promote phases to entities tagged with a new `Phase` built-in (mirroring `EcsPhase`), encode dependencies as `(DependsOn, other)` pairs, and reuse the cascade-on-DependsOn query trick from upstream `src/addons/pipeline/pipeline.c:1016`. Rejected because Go-flecs has consistently chosen Go-idiomatic structs over entity reflection, and because Path B forces this phase to also expose a `Cascade` query traversal modifier — a much wider scope expansion.

Note on upstream's mechanism: upstream does not run an explicit Kahn or DFS pass. Its pipeline query uses `EcsCascade` traversal over `EcsDependsOn` (`src/addons/pipeline/pipeline.c:1016`) and tiebreaks systems with `flecs_entity_compare` (entity ID, i.e. registration order — `src/addons/pipeline/pipeline.c:1021`). It also inserts anonymous depth-anchoring phases between built-ins (`src/addons/pipeline/pipeline.c:983-1003`) so that disabling one built-in does not collapse the others. Go-flecs runs an explicit Kahn-pass instead because it does not have Cascade query traversal.

### Scope expansion to flag: DependsOn is not yet a built-in relationship trait in Go-flecs

The handoff brief suggested DependsOn already exists from Phase 15.14. Verification shows otherwise: `world.go:484` and `world.go:502` both contain the comment `// SlotOf, DependsOn, and Identifier are upstream but not yet ported; skip.` `pairistag.go:19` repeats this. No `DependsOn` symbol is exported from Go-flecs today. This phase therefore must also:

- Allocate a built-in `DependsOn` entity (one more built-in index — count goes 45 → 46+, and the built-in event entity indices established in v0.63.0 must shift up accordingly or `DependsOn` must land at the next free index 45). Recommend: appending at the next free index, matching the Phase 16.8 pattern.
- Decide whether to expose `DependsOn` as a query-time relationship (i.e., `Has(entity, w.DependsOn())` style) or keep it purely as an internal pipeline construct in v1. Recommend: expose `World.DependsOn() ID` for parity with `World.IsA()`, `World.ChildOf()`, etc., but do not require apps to use the pair form — the typed method API (`phase.DependsOn(other)`, `system.DependsOn(other)`) is the public surface.

Upstream marks DependsOn as `Traversable`, `Relationship`, `PairIsTag`, with `(OnInstantiate, Inherit)` and explicitly NOT `Acyclic` (`src/bootstrap.c:1045, 1275, 1283, 1316, 1329`). Go-flecs only needs the Relationship + PairIsTag bootstrap for v1; Traversable/Inherit can wait unless a follow-up phase needs them. State the trait set chosen.

### What ships

1. **`pipeline_phases.go`** (new file; prefer over extending `system.go` to keep the phase abstraction clearly scoped):
   - `*Phase` struct: `name string`, `w *World`, ordered-systems slice, predecessors `[]*Phase` (DependsOn edges), `enabled bool` (plain bool, mirroring `system.go:36` and Phase 16.5 precedent). State whether predecessors is the only edge list or if successors are also cached.
   - `NewPhase(w *World, name string) *Phase` — allocate. No entity backing in v1.
   - `(*Phase).DependsOn(other *Phase) *Phase` — declare `this` runs after `other`. Returns `*Phase` for fluent chaining. Idempotent. Panics on cross-world calls.
   - `(*Phase).SetEnabled(bool)` / `(*Phase).IsEnabled() bool` / `(*Phase).Name() string`.
   - `World.PreUpdate()` / `OnFixedUpdate()` / `OnUpdate()` / `PostUpdate()` change return type from `ID` to `*Phase` — **breaking API change**.
   - Bootstrapped default chain: `PreUpdate → OnFixedUpdate → OnUpdate → PostUpdate` (each later phase has the prior as its sole predecessor).

2. **System-side changes (`system.go`)**:
   - `NewSystem(w, q, fn)` continues to default to `w.OnUpdate()` — return type of `OnUpdate()` changed, but the call shape is the same; existing call-sites compile.
   - `NewSystemInPhase(w, phase *Phase, q, fn)` accepts `*Phase` (was `ID` — `system.go:101`). Validation block at `system.go:107` deleted (the typed `*Phase` argument is self-validating).
   - `(*System).DependsOn(other *System) *System` — declare `this` runs after `other`. Both must share the same `*Phase` or the call panics with a clear message naming both systems and their phases. Returns `*System` for fluent chaining.

3. **Pipeline build (`system.go` Progress dispatch loop)**:
   - Compute a global phase order via topological sort on `*Phase.predecessors` lazily on first tick after any registration; cache; invalidate on `NewPhase`, `(*Phase).DependsOn`, `NewSystem`, `(*System).DependsOn`, `(*System).Close`.
   - Within each phase, compute system order via topological sort on `*System.predecessors`. Systems with no DependsOn edges keep registration order as the tiebreaker (matches upstream's entity-ID tiebreak at `src/addons/pipeline/pipeline.c:1021`).
   - **Algorithm: Kahn's** — linear time, produces an explicit cycle path on failure (good error messages), and matches the existing `acyclic.go` precedent in the repo. State the cycle-path format: e.g., `flecs: pipeline build: phase cycle detected: A → B → C → A`.
   - **Cycle policy**: panic (strict, matching every prior phase-16 lifecycle decision). No upstream-style cycle tolerance.
   - **Default phase ordering for unordered custom phases**: panic at first Progress tick with a clear message listing the dangling phase names. Recommendation: explicit ordering required. Lock this in.

4. **Introspection (`scope.go`)**:
   - `(*Reader).Phases() []*Phase` (return type changed from `[]ID`) — returns all phases in computed topo order.
   - `(*Reader).SystemsInPhase(phase *Phase) []*System` (argument type changed from `ID`) — returns systems in computed topo order within `phase`.
   - `(*Reader).EachSystem(phase *Phase, fn func(*System) bool)` — same shape, typed argument.

5. **Tests (`pipeline_phases_test.go`, ≥12 cases, coverage ≥ 95.0%)**:
   1. Create custom phase `MyPhase`; `MyPhase.DependsOn(w.OnUpdate())`; register one system in `MyPhase`, one in `w.OnUpdate()`; verify OnUpdate system runs before MyPhase system.
   2. Two custom phases A and B; A.DependsOn(OnUpdate), B.DependsOn(A); verify A → B execution order.
   3. System-level DependsOn within OnUpdate: register S1 *then* S2, then call `S2.DependsOn(S1)`; verify S1 runs before S2. Then register S3, S4 in reverse order with S4.DependsOn(S3); verify S3 before S4.
   4. Cycle detection at phase level: A.DependsOn(B), B.DependsOn(A); first `Progress` panics naming both phases.
   5. Cycle detection at system level: S1.DependsOn(S2), S2.DependsOn(S1); first `Progress` panics naming both systems.
   6. Mixed: phase ordering plus within-phase ordering both respected in a single pipeline.
   7. `(*Phase).SetEnabled(false)`: all systems in that phase are skipped; their counters do NOT advance during the disabled period (mirror Phase 16.6 rate filter behavior); re-enabling resumes.
   8. `(*System).SetEnabled(false)` interacts correctly with phase disabling (intersection: both must be enabled).
   9. Custom phase with no DependsOn relation to anything: first `Progress` panics with a clear message naming the orphan phase.
   10. `(*System).DependsOn` across phases: panic with a message naming both systems and both phases.
   11. Removing a system via `(*System).Close`: its DependsOn edges are dropped; other systems' ordering recomputed on the next tick; verify with a 3-system chain where the middle one is removed.
   12. `Reader.Phases()` returns phases in topo order; `Reader.SystemsInPhase(p)` returns systems in topo order within `p`.
   13. **Marshal round-trip** (open decision below): if phases are world state, serializing a world with custom phases and DependsOn edges and unmarshalling must reproduce the same topo order. If phases are *not* serialized in v1, document the decision and add a test asserting that custom phases are dropped (and only built-ins survive) across MarshalJSON/UnmarshalJSON.

6. **Docs**:
   - `docs/Systems.md` — major rewrite of the pipeline section. New § Custom phases. New § DependsOn ordering. Migrate every existing example from `w.OnUpdate()` (returning ID) to its new form. Document the cycle-detection panic format.
   - `docs/README.md` — flip lines 141 and 142 to ✅ shipped (v0.64.0).
   - `README.md` — feature list bump.
   - `CHANGELOG.md` — v0.64.0 entry at top. Lead with **Breaking change**: `World.PreUpdate()` / `OnFixedUpdate()` / `OnUpdate()` / `PostUpdate()` return type changed from `ID` to `*Phase`. Document new built-in entity (`DependsOn` at index 45 — adjust as built-in count requires).
   - `MIGRATING.md` — new v0.64.0 section with a before/after code-snippet diff for the four built-in phase accessors and for `NewSystemInPhase`.
   - `ROADMAP.md` — heading bumps to `Shipped (through v0.64.0)`.

### Explicit non-goals

- No `RunWorker` / explicit thread dispatch (`docs/README.md` line 146 — separate phase).
- No per-phase worker count or parallel-within-phase execution. Sequential within phase.
- No phase priority levels beyond DependsOn. DependsOn is the only ordering mechanism.
- No system-IsA inheritance of phases. A system's phase is direct, not inherited.
- No DependsOn cycle resolution attempts. Strict panic.
- No port of upstream's additional 7 phases (OnStart, PreFrame, OnLoad, PostLoad, OnValidate, PreStore, OnStore, PostFrame). Apps can add them as custom phases. State this.
- No Cascade query traversal modifier. Pipeline build uses a dedicated Kahn pass, not query traversal.

### Open decision points (resolve in design before implementation)

1. **`DependsOn` built-in entity placement**: append at index 45 (next free after Phase 16.8 events) vs interleave. Recommend: append. State.
2. **`DependsOn` trait set bootstrap**: minimum is `Relationship` + `PairIsTag`. Should `Traversable` and `(OnInstantiate, Inherit)` ship in this phase too, or wait? Recommend: minimum only (Relationship + PairIsTag); `Traversable` waits for a phase that needs it. State.
3. **Expose `World.DependsOn() ID`?** Recommend: yes, for parity with `World.IsA()` etc., even though the public API for ordering is method-based. State.
4. **Phase serialization in MarshalJSON**: serialize the phase graph + DependsOn edges (round-trip test required) vs drop custom phases on marshal (only built-ins survive). Recommend: drop in v1, defer round-trip serialization to a follow-up. State.
5. **`*Phase` predecessor edge representation**: slice vs map. Recommend: slice (tiny N; cache-friendly; matches the pattern in `acyclic.go`). State.
6. **Default phase landing for unordered custom phases**: panic (recommended) vs land after PostUpdate. Lock in panic.
7. **Cycle detection algorithm**: Kahn's (recommended; linear; explicit cycle path) vs DFS. Lock in Kahn's.
8. **`(*Phase).DependsOn` and `(*System).DependsOn` return type**: `*Phase` / `*System` for fluent chaining (recommended) vs `void`. State.

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%
- All existing system tests continue to pass after migration to `*Phase` — convert as needed.
- New tests cover all new behavior, especially cycle detection and disabled-phase counter freezing.

### Process

- Feature, not bug.
- **Breaking API change**: prominent in CHANGELOG, MIGRATING, and the issue body above.
- Target: v0.64.0 (next after v0.63.0 custom events, commit 4e4bab5).

## Constraints

- @CONTRIBUTING.md — release process, doc-update obligations (CHANGELOG, MIGRATING, ROADMAP, README, docs/README.md, docs/Systems.md), and ≥95.0% coverage gate.
- @docs/README.md — gap-tracker; lines 141 (custom phases) and 142 (DependsOn ordering) are the two gaps closed by this bundle. Both flip to ✅ v0.64.0 on ship.
- @docs/Systems.md — current pipeline section, which gets the major rewrite. Migration examples must update every `w.OnUpdate()`-style snippet.
- @system.go — current 4-phase pipeline. Key reference points: `NewSystem` at line 87 (defaults phase to `w.onUpdateID`), `NewSystemInPhase` at line 101 (validates ID against the four built-ins, line 107), the `enabled bool` field at line 36 (precedent: plain bool, not atomic), `runPhase` closure at line 329, phase-skip check at lines 333 and 465 (`s.phase != p`), pipeline executor at lines 486-514 (PreUpdate → OnFixedUpdate accumulator → OnUpdate → PostUpdate), phase-name lookup at lines 557-563, and `RunSystem` semantics at lines 532-533 (bypasses both phase ordering and disabled flag — preserve).
- @world.go — current built-in phase IDs: `preUpdateID` (line 55, allocated line 214), `onUpdateID` (line 56, line 220), `postUpdateID` (line 57, line 226), `onFixedUpdateID` (line 58, line 232). Existing accessors `PreUpdate()/OnUpdate()/PostUpdate()/OnFixedUpdate()` return `ID` (lines 578, 582, 585, 592) — these are the four signatures that change. The "DependsOn not yet ported" comments at lines 484 and 502 (and `pairistag.go:19`) confirm DependsOn is a new built-in this phase must introduce. `SetFixedTimestep` semantics at line 622 must keep working.
- @scope.go — `Reader.Phases()` at line 291, `Reader.SystemsInPhase()` at line 300, `Reader.EachSystem()` at line 318. All three change from `ID` to `*Phase` typed argument/return. The validation panics at lines 303 and 321 are deleted (typed args are self-validating).
- @observer_custom.go — Phase 16.8 precedent for the entity-allocation pattern (`RegisterEvent` at line 19, allocates a new entity, applies a built-in tag). The `*Phase` API does NOT follow this pattern in v1 (no entity backing), but the documentation diff in CHANGELOG should explicitly contrast the two choices.
- @CHANGELOG.md — v0.63.0 entry at top shows the breaking-change call-out style (built-in entity count change in v0.63.0). v0.64.0 entry follows the same shape: lead with the breaking change, then Added, Changed, etc.
- @MIGRATING.md — append a v0.64.0 section with the before/after diff for the four built-in phase accessors and `NewSystemInPhase`.
- @ROADMAP.md — heading at line 3 (`## Shipped (through v0.63.0)`) bumps to v0.64.0.
- @acyclic.go — existing Kahn-style precedent in the repo to reuse for the topological-sort implementation; match the cycle-path error message format if one exists there.
- @pairistag.go — line 19 confirms DependsOn is not yet ported; the bootstrap addition for the new `DependsOn` built-in entity must also update this file's documentation and any pair-is-tag flags table.
- Upstream reference (do not include in repo, but cite in PR description): `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h:1908` (EcsDependsOn), `2001-2012` (phase entities + EcsPhase tag); `/work/agents/claude/projects/SanderMertens/flecs/src/addons/pipeline/pipeline.c:953-1027` (FlecsPipelineImport, phase bootstrap, anonymous depth-anchoring phases, Cascade-traversal pipeline query, entity-ID tiebreak); `/work/agents/claude/projects/SanderMertens/flecs/src/bootstrap.c:1045, 1275, 1283, 1316, 1329` (DependsOn trait set).
