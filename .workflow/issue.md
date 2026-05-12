## Goal

Add a **Singleton** component trait (Phase 15.12, target **v0.44.0**) that constrains a marked component to be held by at most one entity in the world at any time. Useful for global-state-as-component patterns: `TimeOfDay`, `GameSettings`, `PlayerInput`, etc.

This continues the trait-system roadmap (Phases 15.0–15.11). Final (15.10) and OneOf (15.11) are the nearest precedents: both write-time-enforcement traits driven from `addIDImmediate`. Singleton differs because the policy is keyed on the **component** (not the relationship) and requires a runtime **instance map** (component ID → holding entity ID) to detect conflicts and clear on entity deletion.

### Semantic divergence from C — call out explicitly

C's `EcsSingleton` (`include/flecs.h:1969-1971`, `src/bootstrap.c:396-427`, flag `EcsIdSingleton = (1u << 25)` in `include/flecs/private/api_flags.h:89`) means "component may only be added to the entity that represents the component itself" — i.e. the component IS the storage entity, enforced via `flecs_on_singleton_add_remove` in `FLECS_DEBUG` builds with `ecs_check(component == e, ...)`. The Go-side spec here is **deliberately different**: "at most one entity may hold this component." This is a more user-friendly interpretation. The docs (`docs/ComponentTraits.md:538-574`) already note Go diverges from C on singletons. Document the divergence in CHANGELOG and ComponentTraits.

### Shape

1. **New built-in entity ID** at index **25** (Singleton). Shifts: Wildcard → 26, Any → 27, user entities → 28+. Update `world.go` field comments, allocation block, `meta_test.go` `builtinEntityCount = 27`, and the `TestIsAWorldCountBaseline` baseline.

2. **`World` fields:**
   - `singletonID ID` — built-in trait entity (index 25).
   - `singletonPolicies map[ID]bool` — component entity index → singleton flag.
   - `singletonInstances map[ID]ID` — component entity index → entity currently holding it.

3. **Public API (in new file `singleton.go`):**
   - `(w *World) Singleton() ID` — built-in trait entity accessor.
   - `SetSingleton(w *World, componentID ID)` — mark component as singleton.
   - `IsSingleton(s scope, componentID ID) bool` — inspection (accepts `scope` per 15.8 convention).
   - `SingletonEntity(s scope, componentID ID) (ID, bool)` — entity currently holding the singleton component (or `false` if none).
   - `applySingletonPolicy(w, componentID)` — internal; called by `SetSingleton` and by `addIDImmediate` when the bare `Singleton` tag is added.
   - `checkSingleton(w, componentID, holder)` — internal; panics if a different entity already holds it. Panic message identifies **both** entities (existing holder and the attempted new holder) and the component.

4. **Typed generic accessors (in `scope.go`):**
   - `Singleton[T any](s scope) (*T, bool)` — registers `T` if not registered, looks up the holder via `SingletonEntity`, calls `GetRef[T]` on the holder. Returns `(nil, false)` if no holder or not a singleton.
   - `WriteSingleton[T any](fw *Writer, v T)` — registers `T` if not registered, ensures it is marked singleton (idempotent), creates a holding entity if none exists (decision: **fail loudly** if no holder exists, do **not** auto-create — matches non-goal). Actually: per non-goal #2 below, **user must create the holding entity** — so `WriteSingleton` panics if no holder is registered. Reconsider: simpler design is `WriteSingleton[T](fw, e, v)` which sets `T` on a user-provided `e`. **Decide during iteration** — prefer the explicit-entity form to honor the non-goal.

   _(Naming note: the task spec floated `SetSingleton(w, componentID)` overlapping with a typed `SetSingleton[T]`. Resolution above: untyped marker is `SetSingleton(w, id)`; typed accessor on a held entity is `Singleton[T](s) (*T, bool)` (read) and `WriteSingleton[T](fw, e, v)` (write). No name collision.)_

5. **`addIDImmediate` Singleton hook** (`id_ops.go`):
   - On `addIDImmediate(w, e, id)` where `id == w.singletonID` (bare tag added to a component entity `e`): call `applySingletonPolicy(w, e)` — mirrors how `applyFinalPolicy`, `applyAcyclicPolicy` are gated.
   - On `addIDImmediate(w, e, componentID)` for a non-pair `componentID` where `singletonPolicies[componentID.Index()]` is true: look up `singletonInstances[componentID.Index()]`. If absent, record `e`. If present and equal to `e`, no-op. If present and **different**, panic with `cannot add singleton component '<name>' to entity '<e>': already held by entity '<existing>'`.
   - Mirror in **pair** form: pairs where `First()` is a singleton component (less common — singletons are typically data components, not relationships — but the check should be uniform).

6. **`removeIDImmediate` Singleton hook** (`id_ops.go`): on remove of a singleton component from `e`, if `singletonInstances[id.Index()] == e`, delete the entry.

7. **Entity deletion** (`world.go` `deleteImmediate`): when an entity is being deleted, scan `singletonInstances` for any entry where the value equals the deleted entity's index and delete those entries. (Per-entity scan of the map is O(singletons) which is small; or maintain an inverse index — start with the simple scan, optimize only if profiling demands.)

8. **Deferred path:** the cmd_queue (`cmd_queue.go`/`defer.go`) replays adds/removes through `addIDImmediate`/`removeIDImmediate`, so enforcement is automatic in deferred scopes. No new cmd kind needed. **Verify** during iteration by writing a deferred-add test case.

9. **Marshal/unmarshal:** add `w.Singleton()` to the `marshal.go` skip-set (line 101 block, before `w.Wildcard()`). Singleton policy state (`singletonPolicies`, `singletonInstances`) is **not** serialized in v1 — document this caveat (matches how exclusive/final/etc. policies are also not serialized; recovery happens via re-marking after unmarshal). _(Or: persist via the pair (component, Singleton) on the component entity, which marshal already captures as a component pair. **Decide during iteration** — likely the latter, free with no extra code.)_

10. **Bootstrap:** no built-in component ships as singleton by default. Singleton is a user-applied trait only.

### Tests — `singleton_test.go` (9 cases)

1. Default behavior unchanged — components without Singleton may be added to multiple entities.
2. `SetSingleton` then `Set` on entity A succeeds; second `Set` on entity B panics with message identifying both entities and the component.
3. `SetSingleton` + `Set` on A, then `Remove[T]` from A, then `Set` on B succeeds (slot released).
4. `IsSingleton` round-trip — false before mark, true after, false after no-such-mark (and false for non-component entities).
5. `SingletonEntity` returns `(holder, true)` after `Set`, `(_, false)` before any `Set` or after `Remove`.
6. Entity deletion via `World.Delete(holder)` clears the singleton slot — subsequent `Set` on a different entity succeeds.
7. Singleton + Exclusive composition — adding a singleton component as part of an exclusive-replaced pair behaves correctly (Singleton is component-level, Exclusive is pair-level; verify no double-fire).
8. `Singleton[T]` / `WriteSingleton[T]` typed accessors — `Singleton[Position](r)` returns `(nil, false)` before set; after `WriteSingleton[Position](fw, e, v)` returns `(&v', true)`.
9. Multiple singletons of different types coexist — `TimeOfDay` and `GameSettings` may each have one holder, possibly the same entity, possibly different, with no cross-talk.

Plus one deferred-path smoke test (folded into one of the above): enforcement fires equivalently inside a `Write` scope through cmd replay.

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- coverage ≥ 95%
- existing tests pass after Wildcard/Any index shift and `builtinEntityCount` update
- `singleton_test.go` ships with 9 cases as above
- docs updated (see below)

### Non-goals

- No singleton-as-tag (only data-bearing components are meaningful as singletons).
- No automatic creation of the holding entity — caller creates it explicitly.
- No serialization of `singletonInstances` runtime state in v1 marshal (the (component, Singleton) pair on the component entity already round-trips the policy; the holder's component data round-trips as normal entity data).

### Docs to update

- `docs/ComponentTraits.md` — Singleton section currently labeled planned (line 538-574, table line 747). Promote to shipped with API, examples, and the C-divergence note.
- `docs/EntitiesComponents.md` — replace the "Singletons" workaround (line 373-396) with the new first-class API.
- `docs/Quickstart.md` — small mention with example.
- `docs/README.md` — feature-gap list entry: move Singleton from gaps to shipped.
- `CHANGELOG.md` — v0.44.0 entry citing C-divergence and giving migration guidance from the workaround pattern.
- `ROADMAP.md` — Phase 15.12 entry promoted to shipped with index 25, count 27, user start 28.

## Constraints

- @cleanup.go — Phase 15.0 cleanup machinery; entity-delete hook integrates with `deleteImmediate` in world.go; the singleton-instance slot clear lives here-adjacent.
- @instantiate_policies.go — pattern reference for component-keyed (not relationship-keyed) policy maps.
- @exclusive.go — pattern reference; pair-level write-time enforcement precedent.
- @cantoggle.go — pattern reference; component-keyed flag precedent.
- @symmetric.go — pattern reference; bare-tag bootstrap precedent.
- @transitive.go — pattern reference; tag composition precedent.
- @reflexive.go — pattern reference; built-in entity allocation order and `scope`-accepting reads (Phase 15.7).
- @acyclic.go — pattern reference; both immediate and deferred enforcement paths (Phase 15.9).
- @final.go — pattern reference; write-time panic with both entity identities in the message (Phase 15.10).
- @oneof.go — pattern reference; `applyXPolicy` + `checkX` split, `IsX(scope, ...)` shape (Phase 15.11).
- @world.go — built-in entity registration block (lines 197-298) for Singleton at index 25; Wildcard → 26 / Any → 27 shift; `singletonPolicies` and `singletonInstances` field additions (lines 78-87 area); `deleteImmediate` (line 562) is where singleton-slot cleanup hooks in.
- @id_ops.go — `addIDImmediate` (line 29) and `removeIDImmediate` (line 221); existing 15.10/15.11 hooks at lines 100-134 are the precedent.
- @childof.go — reference only (not directly relevant).
- @scope.go — `Singleton[T]` / `WriteSingleton[T]` typed generics live here alongside `Get[T]`/`Set[T]` (lines 332-438).
- @docs/EntitiesComponents.md — singleton-workaround section (line 373-396) is replaced by the first-class API.
- @docs/ComponentTraits.md — Singleton section (line 538-574) and trait table (line 747) promoted from planned to shipped; document the C-divergence (C: must-be-self; Go: at-most-one-holder).
- @docs/README.md — feature-gap list updated.
- @CHANGELOG.md — v0.44.0 entry with API, index shift (Wildcard 25→26, Any 26→27, user 27→28), and C-divergence note.
- @ROADMAP.md — Phase 15.12 promoted to shipped, citing index 25, builtinEntityCount 27, user start 28.
- `singleton_test.go` (NEW) — 9 cases per the test list above.

### C research grounding

- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h:1969-1971` — `EcsSingleton` declared with comment "Singleton components may only be added to themselves." Confirms C's must-be-self semantic.
- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs/private/api_flags.h:89` — `EcsIdSingleton (1u << 25)` flag bit.
- `/work/agents/claude/projects/SanderMertens/flecs/src/bootstrap.c:396-427` — `flecs_on_singleton_add_remove` observer body. **Mode: panic** via `ecs_check(component == e, ECS_CONSTRAINT_VIOLATED, ...)`. Note: C wraps this observer in `#ifdef FLECS_DEBUG` — enforcement is **debug-build-only** in C. Go ships always-on enforcement.
- `/work/agents/claude/projects/SanderMertens/flecs/src/bootstrap.c:431-464` — `flecs_register_singleton` installs two observers (single-component term and pair-with-wildcard-target term) on `OnAdd`. Bootstrap registers `EcsSingleton` as a trait at `bootstrap.c:1006`.
- `/work/agents/claude/projects/SanderMertens/flecs/src/bootstrap.c:1304-1305` — `EcsModule` is bootstrapped as a singleton in C (`ecs_add_pair(world, EcsModule, EcsWith, EcsSingleton)`). Go has no module system, so this bootstrap line has no analog — skip.
- `/work/agents/claude/projects/SanderMertens/flecs/src/entity.c` — no direct singleton enforcement in entity.c; all C enforcement is via the observer registered in bootstrap.
- `/work/agents/claude/projects/SanderMertens/flecs/src/storage/component_index.c` — no singleton-specific tracking in the component index (no `singletonInstances` analog); C does not need it because the must-be-self rule means the holding entity is always the component entity itself, recoverable from the component ID.

The Go semantic ("at most one holder") is genuinely new — the `singletonInstances` map has no C analog. This is fine: the Go semantic is more useful for application code; document it as a deliberate divergence.
