## Goal

Split the Phase 15.19 consolidated `Sparse` trait back into two independent traits to match upstream C flecs (`EcsSparse` + `EcsIdDontFragment`) and prepare the storage substrate for Union (Phase 15.22).

Target: **v0.53.0** (next after the just-shipped v0.52.0 Sparse query integration at commit `458c9ab`).

**This is a BREAKING change.** Phase 15.19 (v0.51.0) deliberately consolidated upstream's `EcsSparse` (sparse-set storage) and `EcsIdDontFragment` (entity does not transition archetype tables on add/remove) into a single `Sparse` trait — documented at the time as temporary. This phase undoes that consolidation:

- `Sparse` alone (post-split) = data lives in a sparse-set, but the component still appears in the entity's archetype type. Entity DOES transition archetype tables on add/remove. Rare but kept for upstream fidelity.
- `DontFragment` alone (post-split) = component does NOT appear in entity's archetype type; no transitions. Storage reuses the sparse-set (see Open Decision 1 — recommended simplification over a separate per-entity map).
- `Sparse + DontFragment` together = the current v0.51.0/v0.52.0 `Sparse` behavior. **This is the canonical recommended pattern** that user code should migrate to.

User code that did `SetSparse(c)` and relied on no-archetype-transition semantics MUST now also call `SetDontFragment(c)`. Flag this prominently in the issue title, CHANGELOG v0.53.0 entry (with `BREAKING` prefix), and the new MIGRATING.md (no such file exists today — create it).

### Upstream reference points (verified)

- `EcsDontFragment` declaration: `include/flecs.h:1977` (paired with `EcsSparse` at `:1974`).
- `EcsIdDontFragment` flag bit: `include/flecs/private/api_flags.h:86` (and `EcsIdMatchDontFragment` at `:87`).
- `EcsEntityHasDontFragment`: `api_flags.h:45` — a per-entity-record row-flag bit upstream uses to mark entities that hold at least one DontFragment component.
- Bootstrap: `src/bootstrap.c:894-895` (make alive), `:1022-1023` (bootstrap trait), `:1186-1194` (DontFragment register observer), `:1302` (`ecs_add_pair(world, EcsDontFragment, EcsWith, EcsSparse)` — upstream auto-adds Sparse when DontFragment is added; Go-flecs will document this as a recommended pattern but not enforce auto-add because With + Sparse storage interaction is not yet exercised).
- **Storage routing split** in `src/entity.c:43-49` (`ecs_get_id` analogue): `cr->flags & (EcsIdSparse|EcsIdDontFragment)` both route to `flecs_component_sparse_get`. Then `:1913-1918` shows the DontFragment-only path (data via sparse, table lookup skipped), and `:1924-1925` shows the Sparse-with-table-entry path (table has the column, but data still in sparse-set).
- **Type-membership exclusion** is implicit upstream — DontFragment components are simply never added to the table's id list. Per-entity tracking lives in the record row flag `EcsEntityHasDontFragment` (set at `src/component_actions.c:176`; cleared at `:294`).
- **Per-world linked list of non-fragmenting component records**: `world->cr_non_fragmenting_head` walked at `src/entity.c:1854-1875` (clone path) and `src/component_actions.c:269-292` (entity-delete cleanup). Query path: `src/query/engine/eval_sparse.c:17, 194, 219, 451` — all sparse-query routines assert/branch on `cr->flags & EcsIdDontFragment`.

## Constraints

- @sparse.go — the existing consolidated implementation; the file to refactor most extensively. Keep `SetSparse`, `IsSparse`, `EachSparse`, `World.Sparse()`, `sparsePolicies`, sparse-set storage. **Remove** from Sparse's effect: the "do not transition archetype tables" logic — that moves to DontFragment. Update godoc to make explicit that Sparse alone does NOT bypass archetype transitions and to point at `Sparse + DontFragment` as the canonical pattern.
- @oneof.go, @final.go, @exclusive.go — simple-trait templates for the new `dont_fragment.go` (godoc shape, `Set…`/`Is…`/`World.…` triad, `apply…Policy` internal).
- @with.go, @ordered_children.go, @usage_constraints.go — recent patterns with both immediate and deferred paths; the new `SetDontFragment` and the bare-tag `fw.AddID(componentID, w.DontFragment())` form must follow these.
- @id_ops.go — `addIDImmediate` already houses bare-tag dispatch (lines 85-169) and the Sparse-tag rejection (lines 21-22, 44-45). The new branch for the `w.DontFragment()` bare tag goes alongside the Sparse one at line 165-168. The "no archetype transition" hook currently keyed off `sparsePolicies` in `removeIDImmediate` (line 319) must move to `dontFragmentPolicies`. The same applies to the `setImmediateByPtr` sparse-routing branch.
- @query.go — `Term.Sparse` (line 106-110) currently means "term is a sparse-stored component" and drives the three-mode iterator (line 318+). After the split, the mode-selection criterion is `IsDontFragment(c)` (term is NOT in any archetype type) rather than `IsSparse(c)`. A Sparse-but-not-DontFragment term IS in the archetype type, so archetype-driven iteration applies for table selection; the value is fetched from sparse-set via the existing `Field[T]` sparse branch. Rename `Term.Sparse` field carefully or add `Term.DontFragment` and keep `Term.Sparse` as a storage-routing hint for `Field[T]`. **Recommend**: add `Term.DontFragment bool` for iterator-mode selection; keep `Term.Sparse bool` to flag value-fetch routing.
- @cached_query.go — `sparseAndOnly`, `sparseVersions`, the pure-sparse cached-query shortcut all currently key off `Term.Sparse`. After the split they key off `Term.DontFragment` for the mode shortcut; sparse version-counter tracking still keys off `Term.Sparse`. Walk every caller of `term.Sparse` in `query.go` and `cached_query.go` and classify it as mode-selection vs. value-fetch.
- @world.go — built-in entity allocation list (lines 129-167); ordering after Phase 15.20 has Sparse=34, Wildcard=35, Any=36, user=37+. Insert DontFragment at index 35; Wildcard shifts 35→36; Any shifts 36→37; user entities now start at index 38. Update the `builtinEntityCount` constant in `meta_test.go:19` (36→37). Update `nonDataEntities` in `marshal_test.go`. Update the comment block.
- @marshal.go — `sparse_components` + `sparse_data` field names (line 16-20) stay; data storage is unchanged regardless of which flag drove it. The trait flag itself round-trips via the standard policy-tag replay if a `dont_fragment_components` field is added analogously to `sparse_components`. Add a new built-in skip-set entry for DontFragment.
- @docs/ComponentTraits.md — Sparse section (currently at line 332 references `DontFragment` as "planned"; line 721 documents the consolidation; line 962 has DontFragment as `⏳ planned`; line 971 has the consolidation note). Flip the table row to shipped; add a full DontFragment section with the canonical-pattern guidance; update the Sparse section to reflect that it is data-storage-only post-v0.53.0.
- @docs/README.md:165 — DontFragment entry currently says "not yet ported in Go flecs"; flip to shipped with a one-liner pointing at ComponentTraits.md.
- @CHANGELOG.md — add a v0.53.0 entry at the top with a `BREAKING:` prefix, the migration sketch, and a pointer to MIGRATING.md.
- @ROADMAP.md — "Shipped (through v0.52.0)" heading at line 3 bumps to "through v0.53.0"; add a Phase 15.21 row in the bulleted list at line 56.
- @CONTRIBUTING.md — `Documentation` table at lines 64-72: this is a breaking change with a new feature, so the deliverables list must include both the CHANGELOG Migration Guide section and the touched `docs/` pages. (No CONTRIBUTING.md edits; this is a constraint on the workflow.)

## Design notes (for the iterate agent)

### Storage routing (Open Decision 1)

**Recommend**: DontFragment-only reuses the existing `sparseSet` storage. The flag named "sparseStorage" is then a misnomer post-split — rename to `nonFragmentingStorage` or `boxedStorage` for clarity, OR keep the name and document that sparse-set storage backs both `Sparse`-flagged and `DontFragment`-flagged components. Recommend: keep the name to minimize churn; document the dual usage.

This diverges from upstream C, where DontFragment-only components also use sparse-set storage but the data structures are slightly different. The simplification matches the Go-flecs design choice from Phase 15.19 (boxed-pointer storage via `reflect.New`).

The alternative (separate per-entity map for DontFragment-only) is rejected: adds an entire storage backend, doubles the read-path branching, and gains no functionality the unified sparse-set lacks.

### Has-check mechanism for DontFragment-only (Open Decision 2)

**Recommend**: per-component `dontFragmentEntities map[ID]bool` (analogous to `sparsePolicies` but indexed by component ID and storing the set of entity IDs that hold it). For DontFragment-only components, archetype lookup returns false (component excluded from type), so `Has` must consult this map.

Actually — since DontFragment-only components store data in `sparseStorage`, presence is already queryable via `ss.index[e.Index()]`. **Revised recommendation**: reuse the existing `sparseStorage[cid].index` for presence checks. No new map needed for Has; the storage IS the presence index.

For the recommended canonical pattern `Sparse + DontFragment`, this is the existing behavior. For DontFragment-alone, the same sparse-set indexing applies. For Sparse-alone, presence is in the archetype type (no change).

Update `Has`/`Owns`/`HasID`/`OwnsID` to branch on `IsDontFragment` before `IsSparse`, because DontFragment changes the type-membership picture; Sparse alone does not.

### Type-membership wiring

Find the site where the archetype-type vector is constructed. In Go-flecs that lives inside `World.migrate` (`world.go:1046-1063`) — `newSig` is built by walking `oldSig`, dropping `removeID`, and inserting `addID`. After the split:

- If `IsDontFragment(addID)`: do NOT insert into `newSig`. The entity does not transition (skip `migrate` entirely from the caller — already done if we keep the existing sparse-skip pattern but rebind it from `sparsePolicies` to `dontFragmentPolicies`).
- If `IsDontFragment(removeID)`: do NOT search `oldSig` for it (it was never there). Again, skip `migrate` from the caller.
- If `IsSparse(addID)` but NOT `IsDontFragment(addID)`: DO insert into `newSig`; the component appears in the type. Data still goes into sparse-set (the existing sparse insert hook).
- If `IsSparse(removeID)` but NOT `IsDontFragment(removeID)`: DO remove from `newSig`; archetype transition happens. The sparse-set entry is cleared via the existing sparse-set remove hook.

The hooks at `id_ops.go:319, 44-45` etc. that currently key on `sparsePolicies` for the "skip migrate" decision must rebind to `dontFragmentPolicies`. The hooks at `world.go:893-928` (`getOnWorld`, `hasOnWorld`, `removeOnWorld`) that route reads to sparse-set must split: route to sparse-set whenever `IsSparse(c) || IsDontFragment(c)` (both store data there); but the archetype-type check applies only when NOT `IsDontFragment(c)`.

### Query iterator three-mode selection

Currently the modes are all-sparse / mixed / all-archetype, keyed off `Term.Sparse`. After the split, mode is keyed off `Term.DontFragment` because that's what determines whether the term has a corresponding archetype-table column to drive iteration:

- All-DontFragment: pure sparse-set-driven iteration (today's all-sparse path).
- Mixed (archetype + DontFragment terms): archetype-driven, per-entity sparse-set check (today's mixed path).
- All-archetype (no DontFragment terms): archetype-driven, with possible Sparse-but-not-DontFragment value fetches via the existing `Field[T]` sparse branch (today's all-archetype path, unchanged in structure, but `Field[T]` may need to dispatch on `Term.Sparse` for read routing).

Rename/refactor `sparseAndCount` → `dontFragmentAndCount` (or add a new counter). Keep the version-counter logic on sparse-set; it tracks both Sparse-driven and DontFragment-driven storage changes.

### Bootstrap

The upstream pair `ecs_add_pair(EcsDontFragment, EcsWith, EcsSparse)` at `bootstrap.c:1302` means DontFragment auto-adds Sparse via the With trait. Go-flecs has `With` (Phase 15.17) — investigate whether to bootstrap `(DontFragment, With, Sparse)`. **Recommend NOT auto-adding** in v0.53.0:

- The canonical pattern is for users to call BOTH `SetSparse` and `SetDontFragment` explicitly. Bootstrapping the With auto-add hides the relationship and re-merges the traits at the API surface, partially undoing the split we just did.
- Defer this to a follow-up phase if user feedback wants the upstream ergonomics.
- Document the choice in the CHANGELOG.

### Tests

`dont_fragment_test.go` with at least 10 cases:

1. `SetDontFragment(c)` only (no Sparse): entity does NOT transition tables (assert table identity unchanged across Set); `Has` returns true via sparse-set index; data is in sparse-set storage.
2. `SetSparse(c)` only (no DontFragment): entity DOES transition tables (assert table identity changes); `Has` returns true via archetype lookup; data is in sparse-set.
3. `SetSparse(c) + SetDontFragment(c)`: no transition (assert table identity stable); `Has` returns true via sparse-set; data is in sparse-set. Must behave identically to v0.52.0 Sparse.
4. Migration test: demonstrate old-style `SetSparse(c)` migration to `SetSparse(c) + SetDontFragment(c)` produces equivalent observable behavior (no-transition, sparse-set storage, etc.) — anchors the breaking-change documentation.
5. `IsDontFragment` round-trip + idempotence (second `SetDontFragment` on same component is a no-op, mirroring `applySparsePolicy`).
6. Composability with Final / Exclusive / OneOf (no surprising interactions).
7. Marshal round-trip with DontFragment-only components.
8. Marshal round-trip with Sparse+DontFragment components.
9. `SetDontFragment` after first use of the component: panic with clear message (mirrors `SetSparse`'s after-use trap).
10. Query integration: pure-DontFragment query, mixed DontFragment + archetype, all-archetype with a Sparse-only (no DontFragment) component (the rare case — verify table-driven iteration with sparse value fetch).

`sparse_test.go` updates:
- Tests that asserted "Sparse alone = no archetype transition" must either flip to `Sparse + DontFragment` or change their assertions to the new transition-happens behavior. Document each change in the commit.
- All Phase 15.20 query tests re-checked under the new mode-selection criterion.

Coverage target: ≥ 95.0% on `dont_fragment.go` and on the new branches in `sparse.go`, `id_ops.go`, `query.go`, `cached_query.go`, `world.go`, `marshal.go`.

### MIGRATING.md (new file)

```
# Migration Guide

## v0.53.0 — Sparse / DontFragment split

Phase 15.21 splits the consolidated v0.51.0 `Sparse` trait into two independent
traits matching upstream C flecs.

### What changed

Before (v0.51.0 — v0.52.0):
- `SetSparse(w, cid)` → data in sparse-set AND no archetype transition.

After (v0.53.0):
- `SetSparse(w, cid)` → data in sparse-set; archetype transition still happens.
- `SetDontFragment(w, cid)` → no archetype transition; data still in sparse-set.
- `SetSparse(w, cid)` + `SetDontFragment(w, cid)` → both, canonical pattern.

### Migration

```go
// Old (v0.52.0):
posID := flecs.RegisterComponent[Position](w)
flecs.SetSparse(w, posID)

// New (v0.53.0):
posID := flecs.RegisterComponent[Position](w)
flecs.SetSparse(w, posID)
flecs.SetDontFragment(w, posID)
```

The canonical recommended pattern is `Sparse + DontFragment` together. Sparse
alone is rarely what users want; it's kept for upstream fidelity.
```

## Acceptance criteria

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0% on touched files
- Migration test demonstrates equivalence of old-Sparse to new-Sparse+DontFragment
- All Phase 15.20 query tests pass under the new mode-selection criterion
- Built-in entity count: 37; user entities start at index 38
- CHANGELOG v0.53.0 entry has `BREAKING:` prefix and links to MIGRATING.md
- MIGRATING.md created at repository root

## Explicit non-goals

- **No Union** — Phase 15.22.
- **No new storage backends** — DontFragment-only reuses sparse-set storage (Open Decision 1, recommended simplification).
- **No removal of the breaking change via shim/compatibility layer** — pre-1.0; clean breaks are fine, documented in MIGRATING.md.
- **No auto-add of Sparse when DontFragment is set** (no bootstrapping of `(DontFragment, With, Sparse)`) — users must call both. Defer the ergonomic auto-add to a follow-up phase.
- **No rename of `Term.Sparse`** — add `Term.DontFragment` alongside, classify each existing caller as mode-selection or value-fetch.
