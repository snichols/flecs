## Goal

Port `docs/PrefabsManual.md` from upstream flecs to Go flecs, completing the next chapter in the documentation port series (Phases 14.0–14.4 shipped in v0.19.0–v0.23.0). This phase covers prefab declaration, instantiation, component sharing via `IsA`, copy-on-write override, and the `OnInstantiate` trait semantics.

**Target version: v0.24.0** (Phase 14.4 was v0.23.0).

**Upstream:** `/work/agents/claude/projects/SanderMertens/flecs/docs/PrefabsManual.md` (~3100 words, `port-adapted`, effort: medium).

### Why this exists

Continuing the docs port. PrefabsManual is the reference on prefab declaration, instantiation, component sharing via `IsA`, copy-on-write override, and `OnInstantiate` trait semantics. Governed by CONTRIBUTING.md "Documentation" policy — code blocks must compile against v0.23.0 and use Go idioms.

### Critical context

- We have `MakePrefab(fw, e)`, `IsA` relationship, copy-on-write `Set` override (Phase 4.3), `SetInheritable[T]` (Phase 13.1), and the four trait entities `OnInstantiate`/`Inherit`/`Override`/`DontInherit` (also Phase 13.1).
- **HOWEVER**: only `Inherit` is fully wired up via `SetInheritable`. `Override` (copy on instantiate) and `DontInherit` (never inherit) are surfaced as entity IDs but have no behavior. This is a known gap from the survey.
- The doc should accurately describe what works, and mark `Override` and `DontInherit` as "Not yet ported in Go flecs" with a callout linking to the feature-gap entry.

### Deliverables

1. **Full port of `docs/PrefabsManual.md`** adapted to Go:
   - `MakePrefab(fw, e)` to mark an entity as a prefab.
   - `flecs.MakePair(w.IsA(), prefab)` to instantiate.
   - `SetInheritable[T](w)` to enable query-time inheritance (cross-link Phase 13.1).
   - Copy-on-write override: `flecs.Set(fw, instance, MyComp{...})` on an instance with an inherited component creates a local copy. Already implemented per `isa.go`.
   - `PrefabOf`, `EachPrefab` traversal helpers.
   - `flecs.GetUp[T]` for explicit prefab-value access.
   - **`Override` and `DontInherit` traits**: explicit "Not yet ported in Go flecs" callouts.
   - Variant/inheritance chain: prefab `IsA`-ing another prefab.

2. **Verify code blocks.** Create `docs/prefabs_examples_test.go` with one `TestPrefabs_*` per code block.

3. **Update `docs/README.md`**: PrefabsManual row → `✅ landed / 14.5`. Append any newly discovered gaps. Expect 2–4 (auto-override on add, slot trait, sub-prefab inheritance variants).

4. **Update `ROADMAP.md`**: 14.5 row → `✅ shipped (v0.24.0)`. Do NOT bump the heading.

5. **Update `CHANGELOG.md`** with `Unreleased — Phase 14.5: PrefabsManual doc port (upcoming v0.24.0)`.

6. **Cross-link** with Quickstart (prefab section), Relationships (`IsA`), Inheritance/`SetInheritable` (Phase 13.1 v0.18.0 entry in CHANGELOG).

### Non-goals

- No source changes.
- No porting beyond PrefabsManual.
- No faking unported `Override`/`DontInherit` behavior.

### Mechanical acceptance

- `go test ./docs/...` passes.
- `go vet`, `golangci-lint` clean.
- `go test ./... -race -count=1` passes.
- `docs/README.md` shows PrefabsManual as landed.

### Style notes

- Lead with the minimal example: declare prefab, instantiate, see inherited value.
- Then escalate to `SetInheritable`-and-query and copy-on-write override.
- Then variant chains (`IsA`-chain).
- For `Override`/`DontInherit`, structure the section like: "C flecs supports these; Go flecs has the trait IDs but not the behavior" → callout → workaround (explicit local `Set` or explicit `Remove` on the instance).

## Constraints

- @docs/PrefabsManual.md — current stub to be replaced with the full port
- @docs/Quickstart.md — tone reference and cross-link target (prefab section)
- @docs/Relationships.md — tone reference and cross-link target (`IsA`)
- @docs/HierarchiesManual.md — tone reference for recently-landed manual ports
- @docs/README.md — landing table row to update to `✅ landed / 14.5`; gap list to append
- @docs/hierarchies_examples_test.go — recent test pattern to mirror for `prefabs_examples_test.go`
- @isa.go — `IsA` + copy-on-write override + `PrefabOf` + `EachPrefab` definitions
- @scope.go — `Writer.MakePrefab`, `SetInheritable` on `Writer`, `Reader.GetUp`/`HasUp`/`TargetUp`
- @world.go — `OnInstantiate`/`Inherit`/`Override`/`DontInherit` entity IDs
- @inheritance_test.go — real-world inheritance examples to draw from
- @doc.go — top-level package doc; ensure any prefab snippet stays accurate
- @README.md — project README; verify prefab references remain consistent
- @ROADMAP.md — 14.5 row to mark `✅ shipped (v0.24.0)`; do NOT bump heading
- @CHANGELOG.md — add `Unreleased — Phase 14.5: PrefabsManual doc port (upcoming v0.24.0)`; cross-link Phase 13.1 v0.18.0 entry
- Upstream source: `/work/agents/claude/projects/SanderMertens/flecs/docs/PrefabsManual.md` — port-adapted, effort medium
- Governed by CONTRIBUTING.md "Documentation" policy — code blocks compile against v0.23.0; use Go idioms
- Only `Inherit` is wired up via `SetInheritable`; `Override` and `DontInherit` exist as IDs only — document accurately, no faked behavior
