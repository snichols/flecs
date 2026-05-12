## Goal

Complete the partially-shipped `OnInstantiate` trait family by wiring runtime behavior for `Override` and `DontInherit`. Phase 13.1 (v0.18.0) introduced the four built-in entity IDs (`World.OnInstantiate()`, `World.Inherit()`, `World.Override()`, `World.DontInherit()`) but only `Inherit` has runtime semantics, surfaced through `SetInheritable[T]`. `Override` and `DontInherit` are exposed for API symmetry only and have no effect on instantiation — a fact `docs/PrefabsManual.md` and `docs/ComponentTraits.md` document as a known feature gap (see the `🟡 partial (v0.18.0)` rows in the ComponentTraits roadmap table and the two `Not yet ported` callouts in PrefabsManual).

Target version: **v0.33.0**. This is the natural follow-on to 15.0's `(OnDelete, X)` / `(OnDeleteTarget, X)` per-id-record flag-bit infrastructure in `cleanup.go` — the same translator pattern in `addIDImmediate` for the OnInstantiate pair family.

### Semantics (grounded in C flecs)

For a component `C` and entity `instance` where `IsA(prefab)` is added to `instance` and `prefab` owns `C`:

- **Inherit** (default; already shipped): `instance.Has(C)` walks the IsA chain and reads from the prefab; `instance.Set(C)` makes a local copy (copy-on-write override). Today implemented in Go via `SetInheritable[T]` setting `TypeInfo.Inheritable`, with the walk in `isa.go`'s `getViaIsA` / `hasViaIsA`. No change.

- **Override**: eagerly copy `C` from the prefab into the instance at the moment `(IsA, prefab)` is added. After the copy, `Get`/`Has` find `C` locally without walking the chain; mutations on the instance are isolated from the prefab and other instances.

- **DontInherit**: `C` is never visible on `instance` via IsA. `Has[C](r, instance)` returns false; `Get[C](r, instance)` returns the zero value; query auto-promotion to `Self|Up(IsA)` (Phase 13.1) is suppressed for `C` even if the component is also marked `Inheritable`. C flecs ranks `DontInherit` above `Inherit` — the same precedence applies here.

### C flecs grounding (cite-verified)

- `include/flecs.h:1791-1807` — declarations of `EcsOnInstantiate`, `EcsOverride`, `EcsInherit`, `EcsDontInherit`.
- `include/flecs/private/api_flags.h:65-70` — the flag bit triple:
  ```
  #define EcsIdOnInstantiateOverride     (1u << 6)
  #define EcsIdOnInstantiateInherit      (1u << 7)
  #define EcsIdOnInstantiateDontInherit  (1u << 8)
  #define EcsIdOnInstantiateMask (...)
  ```
- `include/flecs/private/api_flags.h:110` — `ECS_ID_ON_INSTANTIATE_FLAG(id)` macro maps `Override`/`Inherit`/`DontInherit` entity IDs to those bits by their declaration order, i.e. `(1u << (6 + ((id) - EcsOverride)))`.
- `src/bootstrap.c:311-317` — `flecs_register_on_instantiate` observer translates `(OnInstantiate, X)` pair-adds into the matching `cr->flags` bit on the component's id record. This is the pattern 15.0's cleanup-policy translator already mirrors.
- `src/storage/table.c:286-294` — table `trait_flags` accumulator: when a table type contains `(OnInstantiate, Inherit|DontInherit|Override)`, the corresponding bit is set on the table.
- `src/storage/table.c:461-525` — `flecs_table_update_overrides` builds the override-ref cache: for each column of a table whose components have `(IsA, base)` in scope, it stores an `ecs_ref_t` pointing at the base entity's slot. Used as the copy source when an Override transition fires.
- `src/instantiate.c:244-256` — when instantiating a prefab's child table, `EcsIdOnInstantiateDontInherit` components are skipped (except `EcsName` and `(ChildOf, *)`). This is the canonical \"don't copy\" path.
- `src/entity.c:2595` — `ecs_has_id`'s IsA walk: `can_inherit = cr->flags & EcsIdOnInstantiateInherit` — the chain is only consulted when the Inherit bit is set, so `DontInherit` (and `Override`, after copy) naturally short-circuit.
- `src/observable.c:1543-1557` — at OnAdd/OnRemove emit time, `EcsIdOnInstantiateDontInherit` suppresses the override-on-add/-remove OnSet emission.
- `src/query/validator.c:829, 1767` — query construction: terms only get `EcsUp(IsA)` traversal added when the component's `cr_flags & EcsIdOnInstantiateInherit` is set. `DontInherit` therefore suppresses auto-promotion at compile time.

Net: in C, `Override` causes auto-copy on IsA-add, and `DontInherit` causes the IsA walk and query-up to be skipped — both implemented as a per-component-record flag bit, with the bit set either by the bootstrap observer on `(OnInstantiate, X)` pair-add or via the equivalent public helpers.

## Constraints

- @cleanup.go — 15.0's reference implementation pattern. `cleanupPolicyFlags` (uint8 bitfield), `applyCleanupPolicy` writes into `World.cleanupPolicies` map keyed by relID; `SetCleanupPolicy` / `GetCleanupPolicy` are public wrappers. Phase 15.1 should mirror this: either extend `cleanupPolicyFlags` with two new bits and rename the map and helpers, or introduce a parallel `instantiatePolicyFlags` type and `World.instantiatePolicies` map. The latter keeps the OnDelete and OnInstantiate concerns cleanly separated and avoids forcing every cleanup-policy site to think about instantiation bits; recommended.
- @world.go — built-in entity bootstrap (`onInstantiateID`, `inheritID`, `overrideID`, `dontInheritID` already allocated at indexes 8-11 in `init`); add the `instantiatePolicies map[ID]instantiatePolicyFlags` field next to the existing `cleanupPolicies` field. The bootstrap code that builds child IDs should match the layout that `cleanup.go` follows.
- @id_ops.go — `addIDImmediate` already detects `(OnDelete, X)` and `(OnDeleteTarget, X)` pair-adds (lines 38-64) and calls `applyCleanupPolicy`. Extend the same dispatch to `(OnInstantiate, Override|DontInherit|Inherit)`, calling a new `applyInstantiatePolicy`. After `w.migrate` completes the IsA-pair add, if the added id is `(IsA, prefab)`, walk the prefab's components and per-policy copy for those marked `policyOnInstantiateOverride`. This is the trickiest hook — it must run inside the immediate-add path and respect the deferred-flush coalescing queue.
- @isa.go — `getViaIsA` and `hasViaIsA` are the IsA chain walkers. Both must consult the per-component instantiate-policy: if a component is `policyOnInstantiateDontInherit`, the walk for that cid must return `(zero, false)` / `false` without descending. Note that today the walk doesn't take a flag argument — the policy lookup is by component ID.
- @query.go — Phase 13.1's inheritable-component auto-promotion (`validateAndSortTerms` or wherever Self|Up(IsA) is added) must also check the DontInherit bit; if set, suppress the promotion even when `TypeInfo.Inheritable` is true. This is the C-flecs precedence rule.
- @scope.go — `Writer.AddID` is the public entry to `addIDOnWorld` which calls `addIDImmediate`. No changes here are likely needed beyond verifying that the Override copy hook fires through this path (including the deferred case).
- @doc.go — package-level doc comment may need a brief mention if the new `SetInstantiatePolicy` / `GetInstantiatePolicy` join the headline example. Otherwise leave alone.
- @README.md — feature-gap list update if Override/DontInherit are referenced as missing.
- @docs/PrefabsManual.md — replace the two `Not yet ported` callouts (lines 280-291 and the `Not yet ported` section bullets at lines 303-304) with shipped-in-v0.33.0 content and working examples.
- @docs/ComponentTraits.md — update the roadmap table rows (lines 553-556) to mark OnInstantiate/Override/DontInherit as `✅ shipped (v0.33.0)` and revise the prose at lines 43, 155-158 to reflect the new public API.
- @docs/README.md — feature-gap list (the doc-tree README, not the project README) — remove the two stale gap entries.
- @CHANGELOG.md — new `## v0.33.0` section per the existing changelog style.
- @ROADMAP.md — move `Configurable OnInstantiate policies` (or the equivalent line) from In Progress / Planned to Shipped (through v0.33).
- @CONTRIBUTING.md — documentation policy applies: any new public API surface in the Go package must land with corresponding manual-doc updates in the same PR. Inline.

Inline constraint: the `(OnInstantiate, X)` pair-add path must remain valid alongside the new `SetInstantiatePolicy` helper — the helper is sugar over the pair-add, just like `SetCleanupPolicy` is sugar in 15.0. Both should produce identical results, verified by a round-trip test.

Inline constraint: when a component is marked both `Inheritable` (via `SetInheritable[T]`) and `DontInherit` (via the new helper), DontInherit wins. C flecs treats the three OnInstantiate bits as mutually exclusive; the helper API should enforce this by clearing the other two bits on Set.

Inline constraint: Override is mutually exclusive with DontInherit and with Inherit, just like the cleanup actions in 15.0. The helper API should panic on an unknown action, matching the SetCleanupPolicy precedent.

Inline constraint: when an instance with an Override'd component has the component removed locally, the resulting behavior depends on whether DontInherit is also set. C: removal restores the IsA-walk path (Inherit is implicit unless DontInherit is set). Verify and test rather than assume.

Inline non-goal: NO change to the existing `SetInheritable[T]` / `TypeInfo.Inheritable` mechanism. The two systems must coexist — `Inheritable` controls query auto-promotion, `DontInherit` overrides it.

Inline non-goal: NO recursive prefab-children copying (the C `flecs_instantiate_children` flow). Phase 15.1 is component-level only. Multi-level IsA chains (instance IsA prefab1, prefab1 IsA prefab2) must work transitively for Override and DontInherit, but child-entity replication remains out of scope (it's a separate documented gap in PrefabsManual lines 305-306).

Inline non-goal: NO partial-flush handling. If the Override copy hook panics mid-transition, the world halts per the existing exclusive-access discipline. No rollback machinery.

## Deliverables

1. **Per-component instantiate policy storage**. New `instantiatePolicyFlags uint8` type in a new file `instantiate_policies.go` (or extend `cleanup.go` if a single `idTraitFlags` umbrella is cleaner — decide during implementation based on what the diff looks like). Three bits: `policyOnInstantiateOverride` (1<<0), `policyOnInstantiateInherit` (1<<1), `policyOnInstantiateDontInherit` (1<<2). Mutually exclusive — setting one clears the others. `World.instantiatePolicies map[ID]instantiatePolicyFlags` added to `world.go`.

2. **Public API** mirroring the 15.0 cleanup helpers:
   - `flecs.SetInstantiatePolicy(w *World, componentID ID, action ID)` where `action` is `w.Override()`, `w.DontInherit()`, or `w.Inherit()` (clears non-default bits).
   - `flecs.GetInstantiatePolicy(w *World, componentID ID) (ID, bool)` — returns `(actionID, true)` if non-default policy set; `(0, false)` for the implicit Inherit default.
   - The pair-add form `fw.AddID(componentID, flecs.MakePair(w.OnInstantiate(), w.Override()))` continues to work and produces identical state — implemented by extending `addIDImmediate`'s pair detector.

3. **`addIDImmediate` extension**. Detect `(OnInstantiate, Override|Inherit|DontInherit)` pair-adds and translate to `applyInstantiatePolicy(w, e, action)`. Pattern identical to the existing `(OnDelete, X)` / `(OnDeleteTarget, X)` translator (id_ops.go:38-64). The `applyInstantiatePolicy` helper sets the per-component flag in `world.instantiatePolicies[componentID]`. Panic on unknown action, matching the SetCleanupPolicy precedent.

4. **Override behavior — eager copy hook on IsA-add**. In `addIDImmediate`, after `w.migrate(e, id, 0, nil)` completes for an `(IsA, prefab)` pair, walk the prefab's components. For each component cid where `world.instantiatePolicies[cid] & policyOnInstantiateOverride != 0`:
   - If the prefab has cid set, copy the prefab's value into `e`'s local slot via the existing Set path (which itself triggers OnSet observers).
   - Skip if `e` already has cid locally (the user may have pre-set a value before adding IsA).
   - Multi-level chains: walk transitively through any `(IsA, *)` pairs on the prefab itself.
   - The hook must work under the deferred-flush mechanism (cmd_queue) — the simplest implementation is to emit additional `cmdSet` entries into the queue when in deferred mode, mirroring how the IsA-pair add itself flushes through the cmdAddID path.

5. **DontInherit behavior — IsA-walk and query suppression**.
   - In `isa.go`, both `getViaIsA` and `hasViaIsA`: if `world.instantiatePolicies[cid] & policyOnInstantiateDontInherit != 0`, return zero/false immediately without entering the walk.
   - In `query.go`, the inheritable-component auto-promotion path (the spot that adds Self|Up(IsA) when `TypeInfo.Inheritable` is true — find this with a `grep -n 'Inheritable' query.go` during implementation, likely in `validateAndSortTerms`): also check `world.instantiatePolicies[cid] & policyOnInstantiateDontInherit`. If set, do NOT add the Up(IsA) traversal — even if Inheritable is true.

6. **Inherit remains the implicit default**. No code change. The Phase 13.0/13.1 behavior — `SetInheritable[T]` plus query auto-promotion plus `isa.go`'s chain walk — continues to be the canonical path for the default case.

7. **Tests in `instantiate_policies_test.go`** (new file at repo root):
   - **t.Run(\"Override copies prefab value to instance at IsA-add time\")** — Position{10,20} on prefab, SetInstantiatePolicy(w, posID, w.Override()), AddID(instance, MakePair(w.IsA(), prefab)) — instance has local Position{10,20}, mutating it does not affect prefab or sibling instances.
   - **t.Run(\"DontInherit suppresses Get and Has via IsA\")** — Secret{...} on prefab, SetInstantiatePolicy(w, secretID, w.DontInherit()), AddID(instance, MakePair(w.IsA(), prefab)) — Has[Secret](r, instance) == false, Get[Secret](r, instance) returns zero.
   - **t.Run(\"DontInherit overrides Inheritable in query auto-promotion\")** — call SetInheritable[T](w) AND SetInstantiatePolicy(w, cid, w.DontInherit()); a query for T does NOT match the instance.
   - **t.Run(\"Multi-component prefab with mixed policies\")** — prefab has A (Inherit), B (Override), C (DontInherit); after IsA-add, instance has B locally, sees A via IsA, does not see C anywhere.
   - **t.Run(\"Override removal restores IsA path (when not DontInherit)\")** — after Override copy, RemoveID(instance, posID) — Get[Position] walks IsA and returns the prefab's value (default Inherit behavior).
   - **t.Run(\"SetInstantiatePolicy / GetInstantiatePolicy round-trip\")** — for each of the three actions: Set, then Get returns the same action ID.
   - **t.Run(\"Pair-add form equivalent to SetInstantiatePolicy\")** — `fw.AddID(cid, flecs.MakePair(w.OnInstantiate(), w.Override()))` produces the same GetInstantiatePolicy result as the helper.
   - **t.Run(\"Multi-level IsA chain — Override propagates through chain\")** — instance IsA prefab1 IsA prefab2; Override on prefab2's Position; instance gets a copy.
   - **t.Run(\"Default behavior unchanged\")** — sanity: a component with no policy set and no SetInheritable behaves identically to the existing tests in `inheritance_test.go` / `isa_test.go`.

8. **Docs updates** (per CONTRIBUTING.md \"Documentation\" policy, same PR):
   - `docs/PrefabsManual.md`: replace the two `Not yet ported` callouts in the OnInstantiate traits section (lines 280-291) with shipped-in-v0.33.0 content + working examples that the corresponding `docs/prefabs_examples_test.go` can execute. Remove the two stale entries from the `Not yet ported` section bullets (lines 303-304).
   - `docs/ComponentTraits.md`: revise the roadmap table (lines 553-556) — OnInstantiate row to `✅ shipped (v0.33.0)`, Inherit row clarified (already query-time inheritance via SetInheritable, plus pair-add form now equivalent), Override and DontInherit rows to `✅ shipped (v0.33.0)`. Revise the prose at lines 43, 155-158.
   - `docs/README.md`: remove the corresponding feature-gap-list entries.
   - `CHANGELOG.md`: new `## v0.33.0` section describing the public API additions.
   - `ROADMAP.md`: move the OnInstantiate-full-behavior line from In Progress / Planned to Shipped through v0.33.

## Acceptance

- `go vet ./...` clean.
- `golangci-lint run ./...` clean.
- `go test ./... -race -count=3` clean.
- Coverage on main package ≥ 95% (matches the v0.32.0 baseline).
- Existing `inheritance_test.go` and `isa_test.go` pass unmodified.
- New `instantiate_policies_test.go` covers all nine test cases above.
- Docs land in the same commit/PR as the code per CONTRIBUTING.md.

## Implementation notes

- Follow the `cleanup.go` pattern from 15.0 verbatim: a small file with a flag-bits type, an apply helper, public Set/Get wrappers, and a translator hook in `addIDImmediate`.
- The Override-on-IsA-add eager-copy hook is the trickiest piece. Recommended implementation: in `addIDImmediate`, after `w.migrate` returns for an `(IsA, prefab)` add, immediately iterate `prefabRec.Table.Type()` and for each component cid with the Override flag, call the same code path that `flecs.Set` uses (so OnSet observers fire correctly). Validate under the deferred-flush coalescing queue from Phase 11.0 — if the IsA-add itself is deferred, the copy must be deferred too, or batched into the same flush.
- DontInherit's query-suppression hook needs to land in the same Phase 13.1 code path that consults `TypeInfo.Inheritable`. Grep `query.go` for `Inheritable` and add the policy check at the same predicate.
- Naming: prefer `instantiate_policies.go` and `instantiate_policies_test.go` for clean parallel with `cleanup.go` / `cleanup_policies_test.go`. The user-facing helper names mirror SetCleanupPolicy/GetCleanupPolicy.
