## Goal

Extend Phase 9.2.1 JSON serialization to round-trip `ChildOf` parent-child hierarchies. After this lands, `World.MarshalJSON` / `UnmarshalJSON` preserves single-parent `(ChildOf, parent)` relationships, including multi-level hierarchies, with parents always serialized before children so that `Unmarshal`'s `serialToID` map is valid when child entries reference their parent.

### Target behavior

```go
w := flecs.New()
root := w.NewEntity()
w.SetName(root, "scene")
car := w.NewEntity()
w.SetName(car, "car")
flecs.AddID(w, car, flecs.MakePair(w.ChildOf(), root))

data, _ := w.MarshalJSON()
// {"version":1,"entities":[
//   {"serial":1,"name":"scene"},
//   {"serial":2,"name":"car","parent":1}
// ]}

w2 := flecs.New()
w2.UnmarshalJSON(data)
// w2.PathOf(carID) == "scene.car"
```

### In scope

- Single `ChildOf` parent serialization (one parent reference per entity in the output).
- Round-trip parents-before-children ordering via topological sort by `ChildOf` depth.
- Multi-level hierarchies (grandparent → parent → child).
- Cycle detection during marshal — return an error rather than infinite-looping.

### Out of scope (explicit non-goals)

- Multiple `ChildOf` parents per entity (flecs allows it; serialize only the first by signature order and document the limitation).
- `IsA` prefab serialization (Phase 9.2.3).
- Custom pair serialization (Phase 9.2.4).
- Per-tree namespace scoping.
- Forward-references in JSON (children before parents).
- Bumping the format version — `parent` is additive and `omitempty`; v1 stays v1.
- API changes to `MarshalJSON`, `UnmarshalJSON`, or `ParentOf`.

### Deliverables

1. **Extend `jsonEntity` struct (in `marshal.go`):**

   ```go
   type jsonEntity struct {
       Serial     int                        `json:\"serial\"`
       Name       string                     `json:\"name,omitempty\"`
       Parent     int                        `json:\"parent,omitempty\"` // NEW
       Components map[string]json.RawMessage `json:\"components,omitempty\"`
   }
   ```

   `Parent == 0` means no parent (serials start at 1). `omitempty` drops the field when absent.

2. **Marshal changes:**
   - First pass: collect alive non-built-in entities.
   - Topological sort by `ChildOf` depth (roots first). Cycle detection via a `visiting` set during DFS — re-entering a visiting ID is a cycle. On cycle: `fmt.Errorf(\"flecs: marshal failed: ChildOf cycle detected involving entity serial %d\", serial)`.
   - Assign serials in topo order; build `idToSerial map[ID]int` after assignment.
   - During the per-entity emission loop, after components, call `ParentOf(e)`. If `ok && parent is not built-in && parent in idToSerial`, set `je.Parent = idToSerial[parent]`.
   - If `ParentOf` returns a built-in entity, skip (treat as no parent).
   - Verify the existing `IsPair()` check already filters the `(ChildOf, parent)` pair out of `components`.

3. **Unmarshal changes:**
   - In phase 3 (after `serialToID` is fully built), if `je.Parent > 0`:
     - `parentID, ok := serialToID[je.Parent]`; if `!ok`, return an error mentioning `\"unknown parent serial\"`.
     - `AddID(w, newEntityID, MakePair(w.ChildOf(), parentID))` — apply BEFORE the components loop so hooks fire in a clean order.
   - Skip when `je.Parent == 0` (omitempty case).

4. **Multi-`ChildOf` handling:** use `ParentOf` (which delegates to `firstPairTarget`); only the first signature-order parent is serialized. Document in `MarshalJSON` godoc and the JSON format reference.

5. **Built-in entity safety:**
   - Marshal: parent that's a built-in → skipped silently.
   - Unmarshal: `Parent` referring to a non-existent serial → error. A built-in reference is not representable in well-formed v1 output.

6. **Tests** (`marshal_test.go`):
   - Single parent-child round-trip: `ParentOf(carID) == (rootID, true)`.
   - Multi-level hierarchy round-trip: `PathOf(childID) == \"root.parent.child\"`.
   - Wide hierarchy: root with 5 children; verify all via `EachChild`.
   - Cascade after unmarshal: `Delete(parent)` deletes child (Phase 4.2 semantics).
   - **Topological order:** child allocated BEFORE parent in entity order; verify parent's serial < child's serial in output.
   - Cycle detection: A↔B mutual `ChildOf`; `MarshalJSON` errors with `\"cycle\"`.
   - Missing parent in JSON: hand-crafted `{\"version\":1,\"entities\":[{\"serial\":1,\"parent\":99}]}` errors with `\"unknown parent serial\"`.
   - Entity with parent but no name and no components serializes correctly.
   - **All Phase 9.2.1 tests stay green** — `parent` does not appear when absent.
   - Two-step round-trip stable: marshal → unmarshal → marshal yields identical JSON.
   - Sibling order is stable and documented (topo order; siblings by entity-allocation order).
   - Multiple `ChildOf` parents: only the first signature-order parent appears in JSON; godoc matches behavior.

7. **Documentation:**
   - Update `MarshalJSON` godoc (parent field, topological ordering, cycle error).
   - Update `UnmarshalJSON` godoc (parent restoration).
   - Update JSON Serialization section in `doc.go` with the new field.
   - `CHANGELOG.md` Unreleased: \"Phase 9.2.2: ChildOf hierarchy serialization\".

8. **Mechanical acceptance:**
   - `go test ./... -race -count=2` passes.
   - `go vet ./...` clean.
   - `golangci-lint run` clean.
   - Coverage on `flecs` ≥ 90% (no regression from 96.7%).
   - All exported symbols have godoc.

### Constraints / pointers

- Topological sort is the central new piece. Suggested approach: build `parentOf map[ID]ID` for user entities, DFS from each root (entity with no `ChildOf` parent), assign serials in pre-order. Track `visiting set[ID]` to detect cycles.
- `serial` assignment in 9.2.1 was \"iteration order of `EachEntity`\". This becomes \"topological order\". Marshal-internal change — the JSON spec doesn't promise a particular serial scheme, only that serials are consistent within a document.
- `idToSerial` reverse map is built after topo-sort assigns serials.
- Use existing `ParentOf` — do not re-implement the pair lookup.
- DO NOT modify `EntityComponents` / `EachEntity` / `ParentOf` / `AddID`.
- DO NOT introduce a new pair-id filter; the existing `IsPair()` check already filters `ChildOf` pairs out of `components`.
- DO NOT import third-party deps.

## Constraints

- @marshal.go — file to extend; contains `jsonEntity`, `MarshalJSON`, `UnmarshalJSON`, `serialToID` from Phase 9.2.1.
- @marshal_test.go — file to extend with hierarchy tests.
- @childof.go — `ParentOf` (uses `firstPairTarget`), `EachChild`, `ChildOf()` accessor.
- @id_ops.go — `AddID`, `MakePair`, `HasID`.
- @world.go — `World`, `NewEntity`, `Delete`, `EachEntity`.
- @meta.go — built-in entity table; needed to test \"non-built-in\" predicate during marshal.
- @id.go — `ID`, `IsPair`, pair encoding helpers.
- @doc.go — JSON Serialization reference section to update.
- @CHANGELOG.md — append under Unreleased.
- C reference (filesystem paths, do not `@`-reference): `/work/agents/claude/projects/SanderMertens/flecs/src/addons/json/serialize_world.c` and `/work/agents/claude/projects/SanderMertens/flecs/src/addons/json/deserialize.c`.
