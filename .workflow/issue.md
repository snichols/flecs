## Goal

Implement the `WriteOnce` component trait for Go flecs as **Phase 15.13**, targeting release **v0.45.0** (next after v0.44.0). A component marked `WriteOnce` becomes read-only after its first **value write** (`Set` / coalesced deferred set) — any subsequent value-write attempt panics with a clear message.

### Naming: renamed from `Constant`

This trait was originally tracked as `Constant` in the Phase 14.8 gap analysis (see @docs/ComponentTraits.md lines 293-301). It is being **renamed to `WriteOnce`** before implementation to preempt a name collision: upstream C flecs' `EcsConstant` is the meta-addon tag applied to enum/bitmask value entities (`include/flecs/flecs.h:2014` — *\"Tag added to enum or bitmask constants\"*; bootstrap at `src/bootstrap.c:1055-1056`; world.c:170; used throughout `src/addons/meta/type_support/enum_ts.c`). When the meta addon is eventually ported, it will need the name `Constant` for its true upstream semantic. `WriteOnce` is also more precise than `ReadOnly` (overloaded with thread-safety read-view semantics) or `Immutable` (implies removes are blocked too).

Public surface:
- `World.WriteOnce() ID` — built-in marker entity at index 26 (Wildcard shifts 26→27, Any shifts 27→28, user entities start at 29).
- `SetWriteOnce(w *Writer, componentID ID)` — idempotent marker application.
- `IsWriteOnce(s scope, componentID ID) bool` — accepts the `scope` interface per Phase 15.8 conventions; never panics.
- `World.writeOncePolicies map[ID]bool` — storage map declared in @world.go alongside the other policy maps (followed by a per-(entity, component) `hasBeenSet` tracking map; see Open semantic decisions below).

### No upstream counterpart

This is a Go-flecs-only ergonomic trait. Upstream C flecs has no `EcsReadonly` / `EcsImmutable` / write-once trait — verified by exhaustive search of `/work/agents/claude/projects/SanderMertens/flecs/src/` and `include/`. The implementer should design freely within the bounds of this issue; **no upstream citations are expected for the trait's own behavior**. Bootstrap pattern and write-path hook points may still cite the in-repo references to `final.go`, `oneof.go`, `singleton.go`.

### Pattern to follow

Three local references share the uniform shape this trait should adopt:
- @final.go — bare-tag built-in entity, per-id flag map, write-time check that panics. The simplest reference.
- @oneof.go — same pattern with a second map for the parent relationship.
- @singleton.go — most recently shipped (Phase 15.12, v0.44.0, commit b486760). Most similar in spirit because it also enforces a write-time invariant and wires **both** the immediate path (`setImmediateByPtr`) and the coalesced deferred path (`batchForEntity`).

### Open semantic decisions (pre-resolved)

These are pure design choices since there is no C precedent. Resolved here so the implementer has clear marching orders:

1. **Add-then-set behavior.** The \"first write\" is the **Set**, not the Add. `addIDImmediate` without a value slots the component with its zero value — that is not a meaningful write. Therefore `Add` → `Set` is **allowed** (the Set IS the first write); a second `Set` after that **panics**. Track via a per-(entity, component) `hasBeenSet` bit, not via component presence.

2. **Remove clears tracking.** Yes. `Remove` genuinely deletes the component, and a fresh `Add` + `Set` cycle starts over from a clean slate. The trait is per-component-instance, not per-entity-forever. Rationale: matches how users intuitively think about lifecycle — *\"I removed it, so it's gone, so re-adding is a new component.\"*

3. **Non-component target.** `SetWriteOnce` on an entity that is not a registered component (no `EcsComponent`/`World.componentInfo` entry) **panics at trait-application time** with a clear message: `\"WriteOnce requires a component target; entity X is not a component\"`. `IsWriteOnce` on the same entity returns `false` without panic (queries don't panic).

4. **Pair-form `(R, T)` components.** `WriteOnce` on the relationship `R` applies to **every** pair with `R` as the relationship. `WriteOnce` on the target `T` does **not** propagate. State this explicitly in the docs. Mirrors how Exclusive/OneOf attach to the relationship side.

### Hook points to wire

The `hasBeenSet` tracking must be **set** on first value-write, **enforced** on subsequent value-writes, and **cleared** on remove / entity delete. Investigate and modify:

- @value_ops.go — `setImmediateByPtr` (line 254-258, immediate-set path). Check before write; set tracking bit after first successful write.
- @cmd_queue.go — `batchForEntity` (line 103-112, coalesced deferred path). Enforce on the coalesced final set; track per the same map. See line 366/369 for the `setImmediateByPtr` call sites within the batch path.
- @id_ops.go — `removeIDImmediate` (line 233). Clear the per-(entity, component) tracking bit on remove. Also covers pair-remove via line 257.
- @world.go — `deleteOne` (line 522). On entity delete, clear tracking for all of the deleted entity's WriteOnce components (mirrors how Singleton's deleteOne clears its slot per commit b486760).

The same two write paths Singleton hooked must be hooked here; both must enforce.

### Out of scope (non-goals)

- Do **not** implement read-only enforcement for raw pointer access (component slice mutation through `FieldByMatch[T]` or `Each[T]`). Those are unchecked by design — WriteOnce guards the write **API**, not the access **API**. State this explicitly in the doc.
- Do **not** bundle `DontFragment`, `Union`, or `Traversable`. Separate phases.

## Constraints

- @docs/ComponentTraits.md — section at lines 293-301 currently named \"Constant\" and marked \"not yet ported / workaround: none\". Rename to \"WriteOnce\", flip to \"shipped (v0.45.0)\", add usage example, and include the historical breadcrumb: *\"Previously called 'Constant' in the Phase 14.8 gap analysis; renamed to avoid collision with upstream `EcsConstant` (an enum-value tag in the meta addon).\"* Update the TOC anchor at line 17. Update the roadmap-table row at line 768 to \"✅ shipped (v0.45.0)\". Document the pair-form rule (relationship-side only) and the non-component-target rule.
- @docs/EntitiesComponents.md — add a `WriteOnce` example alongside other trait examples if a natural slot exists.
- @README.md — feature-list entry mirroring the v0.44.0 Singleton entry at line 49. Include the rename note.
- @CHANGELOG.md — new v0.45.0 entry at the top titled *\"Phase 15.13: WriteOnce component trait\"*. Include: *\"Renamed from 'Constant' to avoid future collision with `EcsConstant` enum-value tag (meta addon).\"*
- @ROADMAP.md — flip the shipped-table row for WriteOnce/Constant; bump the heading at line 3 from \"Shipped (through v0.43)\" to \"Shipped (through v0.45.0)\" (note: heading was not bumped when v0.44 / Singleton shipped — this lands both bumps).
- @world.go — built-in entity allocation at lines 291-303 currently slots Singleton=25, Wildcard=26, Any=27. Insert WriteOnce at 26; shift Wildcard→27, Any→28. Update the field comments at lines 76-78 and the doc-comment block at lines 134-136.
- @marshal.go — extend the marshal skip-set for the new built-in entity.
- @meta_test.go line 17 — bump `const builtinEntityCount = 27` to `28`. Baseline test fixups likely also needed in `isa_test.go` and `marshal_test.go` (anywhere a built-in entity index or count is hard-coded).
- @CONTRIBUTING.md — doc-update and coverage expectations apply.

### Required test cases (`writeonce_test.go`, at least 8)

1. `SetWriteOnce` + first `Set`: succeeds, value readable.
2. `SetWriteOnce` + first `Set` + second `Set`: second panics with a message naming the entity and component.
3. `Add` then `Set` then `Set` again: first Set succeeds, second panics. (Confirms `Add` is not the trigger — the per-(entity, component) `hasBeenSet` bit, not component presence, gates the panic.)
4. `Set` then `Remove` then `Add` then `Set`: succeeds. (Remove clears tracking.)
5. Deferred-coalesced path: `w.Write(func(fw *Writer) { SetID(fw, e, c, v1); SetID(fw, e, c, v2) })` — second `SetID` inside the same `Write` panics during coalesce (not on the first call).
6. Pair-form: `SetWriteOnce` on relationship `R`, then `SetID(e, Pair(R, T1), v1)` succeeds, second `SetID(e, Pair(R, T1), v2)` panics. Different target `T2` is a separate pair instance: `SetID(e, Pair(R, T2), v3)` succeeds.
7. `SetWriteOnce` on a non-component entity panics at trait-application time; `IsWriteOnce` on a non-component entity returns `false` without panic.
8. `IsWriteOnce` round-trip across `SetWriteOnce`; idempotent `SetWriteOnce` (calling it twice is a no-op, not a panic).
9. Bonus: `World.WriteOnce()` is itself a tag, not a component — applying the WriteOnce trait to the marker entity itself doesn't recurse or blow up.

### Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run` clean.
- `go test ./... -race -count=3` passes.
- Coverage ≥ 95.0% (current baseline).

### Process notes

- This is a **feature**, not a bug.
- All `@`-references in this issue resolve to real files and line ranges as of `master` at issue creation.
