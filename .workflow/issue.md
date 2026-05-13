## Goal

Port the upstream `EcsWith` relationship trait to Go flecs as **Phase 15.17 / v0.49.0**.

`With` ensures that whenever a tagged entity `X` is added to some target entity `E`, a second entity `Y` is **automatically also added** to `E`. The relationship is added in pair form on the source: `(With, Y)` written on `X` means \"adding X co-adds Y.\"

Two forms of auto-add must work:

1. **Bare add**: adding `X` to `E` where X has `(With, Y)` → also adds `Y` to E.
2. **Pair-with-target add**: adding `(R, T)` to `E` where R has `(With, S)` → also adds `(S, T)` to E. The co-add inherits the same target. (C source `/work/agents/claude/projects/SanderMertens/flecs/src/storage/table_graph.c:1029-1043`.)

Chained `With` cascades transitively: if A has `(With, B)` and B has `(With, C)`, adding A to E adds A, B, C. C handles this by recursing in `flecs_add_with_property` (`table_graph.c:1042`).

This replaces the current Go workaround documented at `/work/agents/claude/projects/flecs/docs/ComponentTraits.md:860-874` (an `OnAdd[Power]` hook that manually adds `Responsibility`) with a first-class declarative trait.

Target version: **v0.49.0**, next after v0.48.0 PairIsTag (shipped at commit `24eb7ad`).

## C upstream research

Cited line numbers in `/work/agents/claude/projects/SanderMertens/flecs/`:

- `EcsWith` constant declaration: `include/flecs.h:1845` (block at `:1836-1845` documents `If With(R, O) and R(X) then O(X)` and the pair-with-target rule).
- `EcsWith` constant definition: `src/world.c:62` (`FLECS_HI_COMPONENT_ID + 32`).
- Bootstrap registration: `src/bootstrap.c:1013` (`flecs_bootstrap_trait(world, EcsWith)`).
- Auto-add enforcement: `src/storage/table_graph.c:1102-1107` — when adding `with` to a node, if the source's component record has `EcsIdWith`, `flecs_add_with_property` walks the `(With, *)` pairs and adds each target.
- Pair-form co-add (preserves target): `src/storage/table_graph.c:1029-1033` — `ECS_PAIR_SECOND(id)` gives the With-target `ra`, then `if (o) { a = ecs_pair(ra, o); }` rebuilds a pair with the originating add's target.
- Recursive cascade for chained With: `src/storage/table_graph.c:1042` — `flecs_add_with_property(world, cr_with_wildcard, dst_type, ra, o);` recurses on `ra` (the co-add target) preserving the originating `o`.
- Already-added short-circuit: `src/storage/table_graph.c:1040` — `flecs_type_add` dedupes; chained With never re-adds an id already in the destination type.
- Cycle stance: With is bootstrapped Acyclic at `src/bootstrap.c:1317` (`ecs_add_id(world, EcsWith, EcsAcyclic)`), so a `With` cycle is in principle caught by the Acyclic guard at write time in C. Combined with the `flecs_type_add` dedup, a self-cycle in C resolves to a finite walk that terminates when no new ids are added.
- Table-flag propagation: `src/storage/table.c:266-267` sets `EcsIdWith` on the destination table when a `(With, *)` pair is added.
- Bootstrap entities with `(With, X)` pre-set in C: Traversable→Acyclic (`bootstrap.c:1296`), Transitive→Traversable (`bootstrap.c:1299`), DontFragment→Sparse (`bootstrap.c:1302`), Module→Singleton (`bootstrap.c:1305`). **None of these need to be ported in Phase 15.17** — they require Sparse/DontFragment/Module which are not yet shipped, and the Transitive→Traversable implication was explicitly deferred in v0.46.0 (see `/work/agents/claude/projects/flecs/CHANGELOG.md:53`). Document explicitly as a non-goal.

## Pattern reference

Read via `@`-syntax:

- @/work/agents/claude/projects/flecs/oneof.go — relationship trait with a tied parent-id map; closest existing parallel since With also stores \"source → tied id\".
- @/work/agents/claude/projects/flecs/exclusive.go — minimal relationship trait shape with `applyExclusivePolicy` hook.
- @/work/agents/claude/projects/flecs/usage_constraints.go — recent multi-trait file (Phase 15.15).
- @/work/agents/claude/projects/flecs/pairistag.go — most recently shipped (Phase 15.16, v0.48.0, merge `24eb7ad`).
- @/work/agents/claude/projects/flecs/reflexive.go — relationship trait with hook-time auto-expansion semantics.
- @/work/agents/claude/projects/flecs/id_ops.go — `addIDImmediate` lines 76-143 show the dispatch table for bare-tag traits; lines 197-201 show the symmetric-mirror recursive `addIDImmediate` call (the precedent for With's recursive auto-add with idempotence via the HasComponent early-return at line 34).
- @/work/agents/claude/projects/flecs/cmd_queue.go — `batchForEntity` lines 140-186 show the deferred-path bare-tag dispatch and acyclic check site where With's deferred coalesce must hook.
- @/work/agents/claude/projects/flecs/marshal.go — `MarshalJSON` skip-set (lines 101-135) — must add `w.With()`.

## Deliverables

### 1. New file `with.go`

- `(w *World).With() ID` — built-in entity accessor. New built-in inserted at **index 32**, shifting Wildcard→33, Any→34, user→35+. `builtinEntityCount` bumps **33 → 34**. (Verified against `/work/agents/claude/projects/flecs/world.go:120-156` ordering doc-block and `:346-363` allocation block and `/work/agents/claude/projects/flecs/meta_test.go:19`.)
- `SetWith(w *Writer, source ID, coAdd ID)` — adds the pair `(With, coAdd)` to `source` via the normal pair-add path. **Idempotent** for the same `(source, coAdd)` (HasComponent early-return covers it). A source can have multiple distinct co-adds (`SetWith(A, B); SetWith(A, C)` → adding A co-adds both B and C).
- `HasWith(s scope, source ID) []ID` — convenience inspector returning the list of co-add IDs registered on source. Use scope interface per Phase 15.8 convention.
- **No removal API in this phase** — symmetric with other trait markers (sticky, set-once).
- **Storage decision**: derive the co-add list from existing pair storage by scanning the source's table type for pairs whose `First() == w.withID`. **Recommend this approach** — single source of truth, automatic JSON round-trip via existing pair-marshalling, no separate marshal path required. State the chosen approach in code comments.

### 2. Auto-add enforcement

Insert in `addIDImmediate` (`/work/agents/claude/projects/flecs/id_ops.go`), modeled on the symmetric-mirror recursion at lines 197-201, and in `batchForEntity` (`/work/agents/claude/projects/flecs/cmd_queue.go`) after the existing trait-dispatch block:

- **Bare add** `AddID(e, X)`: after `w.migrate(e, id, 0, nil)` succeeds, look up all `(With, Y)` pairs on `X` (via X's table type, filtering for pairs with `First().Index() == w.withID.Index()`). For each Y, recursively call `addIDImmediate(w, e, Y)`. The HasComponent early-return at line 34 provides the loop-guard.
- **Pair-with-target add** `AddID(e, MakePair(R, T))`: after migrate succeeds, look up all `(With, S)` pairs on R. For each S, recursively call `addIDImmediate(w, e, MakePair(S, T))`. Same loop-guard.
- **Recursion / cycle protection**: maintain a per-add-call seen-set keyed by the originating entity + the id being added. **Cycle behavior: explicit panic** with a clear message naming the cycle path (`A → B → A`). The implementer must document this choice in `with.go` code comments. Rationale: silent short-circuit (relying on the HasComponent dedup) would mask programmer errors where two unrelated authors set up `SetWith(A, B)` and `SetWith(B, A)` independently. The C bootstrap explicitly marks With as Acyclic (`bootstrap.c:1317`), supporting a fail-fast stance.
- **Order**: the originating add's table transition lands first (`w.migrate` returns), THEN each co-add fires as its own independent `addIDImmediate` call (its own migration, its own OnAdd hook fire). Document this in code comments — this matches how the symmetric mirror works today.

### 3. Test file `with_test.go`

Minimum 12 cases; coverage on `with.go` must be ≥ 95.0%:

1. `SetWith(Power, Responsibility)` then `AddID(e, Power)` → e has both Power AND Responsibility.
2. Chained: `SetWith(A, B) + SetWith(B, C)` then add A to e → e has A, B, C.
3. Multiple co-adds on one source: `SetWith(A, B) + SetWith(A, C)` then add A → e has A, B, C.
4. Pair form: `SetWith(R, S)` then `AddID(e, MakePair(R, T))` → e has `(R, T)` AND `(S, T)`.
5. Pair form chained: `SetWith(R1, R2) + SetWith(R2, R3)` then `AddID(e, MakePair(R1, T))` → e has `(R1, T)`, `(R2, T)`, `(R3, T)`.
6. **Cycle detection**: `SetWith(A, B) + SetWith(B, A)` then `AddID(e, A)` panics with a clear message naming the cycle path.
7. Idempotent `SetWith` for same source/target pair (calling twice does not duplicate the registration).
8. **Deferred path**: `w.Write(func(fw) { fw.AddID(e, Power) })` after `SetWith(Power, Responsibility)` — Responsibility is added at coalesce time. Verify both Power and Responsibility are on e after the Write block closes.
9. `HasWith` round-trip — returns the list of co-adds (asserts length and membership; do not assume order).
10. **Compose with `IsA`**: if `e IsA template` and `template` has `Power` (which has `With(Responsibility)`), e does NOT additionally get `Responsibility` directly added. Rationale: With fires on direct add only. e inherits Power via IsA's lookup walk; that inheritance lookup does NOT re-trigger With. Document this choice with a code comment in the test and a callout in `with.go`. (This matches C: `flecs_add_with_property` runs only inside `flecs_find_table_with`, i.e. only on direct id-add, not on IsA chain walks.)
11. **Compose with `Exclusive`**: `SetExclusive(R) + SetWith(R, S)` — `AddID(e, MakePair(R, T1))` co-adds `(S, T1)`. Then `AddID(e, MakePair(R, T2))` replaces `(R, T1)` with `(R, T2)` AND co-adds `(S, T2)`. **The previously co-added `(S, T1)` is NOT removed** — With is one-way add-only. The user must manually clean up `(S, T1)` if they want that semantic. Document this in `with.go` and assert it in the test.
12. **One-way semantic on Remove**: after auto-add fires, `RemoveID(e, Power)` does NOT auto-remove `Responsibility`. With is one-way add-only (mirrors C). Assert.

Bonus (recommended, not required for ≥ 95.0%):

- Deferred path with pair form: `w.Write(func(fw) { fw.AddID(e, MakePair(R, T)) })` after `SetWith(R, S)` — verify `(S, T)` is present after the Write block closes.
- Hook ordering: register `OnAdd` hooks for both Power and Responsibility; verify Power's OnAdd fires before Responsibility's OnAdd (matches the documented \"originating add transitions first, then co-adds\" order).

### 4. Doc updates per CONTRIBUTING.md

- `/work/agents/claude/projects/flecs/docs/ComponentTraits.md:845-878` (the existing \"### With\" section) — flip from \"Not yet ported in Go flecs as a first-class trait\" to **shipped (v0.49.0)** with Go usage examples covering bare-tag, pair-form, and chained cases. Remove the OnAdd workaround block or convert it into a \"Before vs. after\" callout.
- `/work/agents/claude/projects/flecs/docs/ComponentTraits.md:32` Table-of-contents anchor already exists (`[With](#with)`); leave as-is.
- `/work/agents/claude/projects/flecs/docs/ComponentTraits.md:912` Trait-system-roadmap row for **With**: flip `⏳ planned` to `✅ shipped (v0.49.0)` with a notes column matching the format of other shipped traits (`SetWith(w, source, coAdd)` / `HasWith(scope, source)` / `w.With()` (index 32); …).
- `/work/agents/claude/projects/flecs/docs/Relationships.md` — no existing With section, but if any existing prose mentions OnAdd hooks for co-addition, add a brief pointer to the new trait.
- `/work/agents/claude/projects/flecs/docs/README.md:170` — feature-gap entry for `With` relationship: flip from \"not yet ported\" to a shipped marker matching the prose-style format used for `Final` (line 168) and `OneOf` (line 169).
- `/work/agents/claude/projects/flecs/README.md` — feature-list bump if applicable (review whether trait list in the README mentions specific traits at all; mirror the pattern used after v0.48.0 PairIsTag).
- `/work/agents/claude/projects/flecs/CHANGELOG.md` — new `## v0.49.0 — <date> — Phase 15.17: With relationship trait` entry at the top, following the prose format established by v0.48.0 (lines 3-19).
- `/work/agents/claude/projects/flecs/ROADMAP.md:3` — bump heading from \"Shipped (through v0.48.0)\" to \"Shipped (through v0.49.0)\". Append a new shipped row after line 53 matching the prose format of the v0.48.0 entry, including new builtin index 32 and the new `builtinEntityCount: 34; user entities now start at index 35`.

### 5. Marshal + baseline test fixups

- `/work/agents/claude/projects/flecs/marshal.go:101-135` — add `w.With(): {},` to the skip set (keep alphabetical/logical grouping consistent).
- `/work/agents/claude/projects/flecs/meta_test.go:11-19` — bump `builtinEntityCount` from `33` to `34`; update the comment block listing the built-in indices to include `With(32)` and shift Wildcard→33, Any→34.
- `/work/agents/claude/projects/flecs/isa_test.go:666-677` — `TestIsAWorldCountBaseline` literal `33` → `34`; update the trailing parenthesised list in the error message to include `With` in the appropriate position.
- `/work/agents/claude/projects/flecs/marshal_test.go:42-58` — `nonDataEntities` helper: add `w.With(): {}`; update the leading comment (currently says \"32 built-in entities\" — bump to 33 once user entities skip-set excludes the new built-in; recount with care).

## Decisions baked into this issue

1. **Cycle behavior**: explicit panic with cycle path naming (not silent short-circuit). Rationale: With is one-way and bootstrapped Acyclic in C; silent short-circuit can mask programmer errors. Implementer must document this in `with.go`.
2. **Storage**: derive co-add list from existing pair storage by querying the source's table type for `(With, *)` pairs. No separate side-map. Rationale: single source of truth, automatic JSON round-trip, no separate marshal path.
3. **IsA interaction**: With fires on direct add only. Inheriting a component via IsA does NOT re-trigger With for the inheritor. Test case #10 above. State and document.
4. **Exclusive interaction**: With is one-way. Replacing `(R, T1)` with `(R, T2)` co-adds `(S, T2)` but does NOT clean up the previously co-added `(S, T1)`. Test case #11 above. State and document.

## Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage on `flecs` package ≥ 95.0%; coverage on `with.go` specifically ≥ 95.0%

## Non-goals

- Do NOT bundle With with Sparse / DontFragment / Union / OrderedChildren.
- Do NOT auto-remove co-added components when the originating component is removed — With is one-way add-only (mirrors C).
- Do NOT recursively expand With through IsA inheritance — auto-add fires on direct add only.
- Do NOT add a removal API for `(With, X)` — sticky, symmetric with other trait markers.
- Do NOT port the C built-in `(With, …)` bootstrap pairs (Traversable→Acyclic, Transitive→Traversable, DontFragment→Sparse, Module→Singleton) — they require traits not yet ported, and the Transitive→Traversable implication was explicitly deferred in v0.46.0.

## Constraints

- @/work/agents/claude/projects/flecs/CONTRIBUTING.md — golangci-lint clean; coverage ≥ 90% project-wide (this phase targets ≥ 95% on `with.go`); doc updates land in the same PR; new feature → both `ROADMAP.md` and `CHANGELOG.md`.
- @/work/agents/claude/projects/flecs/pairistag.go — most recent shipped trait; the godoc shape, `Apply…Policy` naming, idempotent-Set semantics, and scope-accepting `Is…` accessor are the template to follow.
- @/work/agents/claude/projects/flecs/oneof.go — closest structural parallel: tied-id map keyed by index, dual bare-tag + pair-form entry points.
- @/work/agents/claude/projects/flecs/exclusive.go — minimal relationship-trait baseline.
- @/work/agents/claude/projects/flecs/usage_constraints.go — multi-trait single-file pattern showing how to mix Set/Is/apply helpers and per-trait check functions.
- @/work/agents/claude/projects/flecs/reflexive.go — example of a relationship trait that adds semantic-expansion behavior beyond a simple flag.
- @/work/agents/claude/projects/flecs/world.go — built-in entity ordering doc-block (lines 120-156) and allocation block (lines 346-363); bootstrap-policy block (lines 369-407) for any bootstrap policy on the new With entity.
- @/work/agents/claude/projects/flecs/id_ops.go — `addIDImmediate` dispatch table for bare-tag traits (lines 76-143); symmetric-mirror recursive call (lines 197-201) as the precedent for With's recursive auto-add.
- @/work/agents/claude/projects/flecs/cmd_queue.go — `batchForEntity` deferred-path bare-tag dispatch and trait check sites (lines 140-186).
- @/work/agents/claude/projects/flecs/marshal.go — skip-set update.
- @/work/agents/claude/projects/flecs/docs/ComponentTraits.md — `### With` section flip (lines 845-878) and roadmap-table row (line 912).
- @/work/agents/claude/projects/flecs/docs/README.md — feature-gap line 170.
- @/work/agents/claude/projects/flecs/ROADMAP.md — shipped row append and heading bump (line 3).
- @/work/agents/claude/projects/flecs/CHANGELOG.md — new v0.49.0 entry at top.
- @/work/agents/claude/projects/flecs/meta_test.go — `builtinEntityCount` bump 33→34 with comment update.
- @/work/agents/claude/projects/flecs/isa_test.go — `TestIsAWorldCountBaseline` literal bump and error-message list update.
- @/work/agents/claude/projects/flecs/marshal_test.go — `nonDataEntities` helper update.
- C upstream `/work/agents/claude/projects/SanderMertens/flecs/src/storage/table_graph.c:1005-1107` — canonical auto-add enforcement; preserve the pair-target inheritance and recursive cascade semantics.
- C upstream `/work/agents/claude/projects/SanderMertens/flecs/src/bootstrap.c:1013,1317` — With trait bootstrap and Acyclic marking; the Acyclic stance is the rationale for the explicit-panic cycle choice.
