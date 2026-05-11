## Goal

Add IsA prefab serialization to MarshalJSON / UnmarshalJSON. Each entity may have zero or more IsA targets (prefabs); these targets must round-trip through JSON and continue to provide inheritance via Phase 4.3 semantics after unmarshal. As part of the same change, generalize the Phase 9.2.2 topo-sort to handle multiple predecessor edges per entity (ChildOf parent and IsA prefabs in one DFS), so that cycles spanning both relationships are detected once.

### Target behavior

```go
w := flecs.New()
prefab := w.NewEntity()
flecs.Set[Position](w, prefab, Position{X: 1, Y: 1})

child := w.NewEntity()
flecs.AddID(w, child, flecs.MakePair(w.IsA(), prefab))

data, _ := w.MarshalJSON()
// data contains:
// {"version":1,"entities":[
//   {"serial":1,"components":{"pkg.Position":{"X":1,"Y":1}}},
//   {"serial":2,"prefabs":[1]}
// ]}

w2 := flecs.New()
flecs.RegisterComponent[Position](w2)
w2.UnmarshalJSON(data)
// child inherits Position from prefab via IsA (Phase 4.3 semantics).
// flecs.Get[Position](w2, restoredChild) returns Position{1, 1}.
```

### Scope

In:
- Multi-prefab IsA serialization (entity can have N >= 0 prefabs).
- Combined topo-sort over ChildOf + IsA edges (parents and prefabs both come before children).
- Round-trip preservation of inheritance order (first-prefab-wins semantics).
- Cycle detection that spans both ChildOf and IsA edges in a single DFS.

Out (non-goals):
- Custom pair serialization (Phase 9.2.4).
- Dynamic / cross-document prefab loading. Prefabs must live in the same JSON document.
- Extra IsA chain depth limits beyond Phase 4.3's existing 64.
- Excluding inherited-but-not-overridden components from a child's `components` map — `EntityComponents` already returns only local components.
- Bumping the format version. The `prefabs` field is additive; v1 readers without IsA support ignore it.
- Changing the MarshalJSON / UnmarshalJSON API surface.

### Deliverables

1. Extend `jsonEntity` in `marshal.go` with `Prefabs []int \`json:\"prefabs,omitempty\"\`` (serials of IsA targets).
2. Refactor: replace `parentOf map[ID]ID` with `predecessorsOf map[ID][]ID` containing the union of ChildOf parent + IsA prefabs. ChildOf parent first, then IsA prefabs in `EachPrefab` order. Built-in IDs are filtered at insertion time.
3. Extract topo-sort into a small helper struct (e.g. `marshaler`) with `predecessorsOf`, `visited`, `visiting`, `order` fields; DFS iterates ALL predecessors. Cycle error wording: `\"flecs: marshal failed: cycle detected in ChildOf+IsA graph involving entity serial %d\"` using SERIAL, not raw `uint64(e)`. Fallback `\"entity at allocation index N\"` when the serial isn't assigned yet.
4. Marshal: for each entity in topo order, gather IsA targets via `EachPrefab`, map each to its serial via `idToSerial`, store in `jsonEntity.Prefabs`. Skip built-in targets defensively. Preserve `EachPrefab` iteration order.
5. Unmarshal: in phase 3, AFTER applying Parent (ChildOf) and BEFORE applying components, walk `entity.Prefabs` and call `AddID(w, newEntityID, MakePair(w.IsA(), prefabID))` per serial. Error with `\"unknown prefab serial N\"` on missing lookup. Order in the array is preserved (first-prefab-wins).
6. Apply the Phase 9.2.2 reviewer nit at the same time: use SERIAL in the existing cycle error, not raw `uint64`.
7. Tests in `marshal_test.go`:
   - Single-prefab round-trip (inheritance works after restore).
   - Multi-prefab round-trip (both prefabs in `Prefabs` in signature order).
   - First-prefab-wins after round-trip (p1.Position vs p2.Position; child gets p1's).
   - Topological order with IsA (prefab allocated AFTER child; prefab serial < child serial in JSON).
   - Combined ChildOf + IsA topo order (entity has both; both predecessors serialize first).
   - Mixed cycle ChildOf -> IsA -> ChildOf returns error mentioning \"cycle\".
   - Multiple IsA but no ChildOf — `parent` omitted, `prefabs` present.
   - No-IsA round-trip stable (`prefabs` absent in JSON).
   - Hand-crafted JSON with unknown prefab serial — error mentions \"unknown prefab serial 999\".
   - ChildOf+IsA combination cascades — parent has IsA(prefab); child has ChildOf(parent); cascade Delete(parent) deletes child on the restored world.
   - Override after IsA — local `Set` on child wins; marshal carries both local component and prefab serial; unmarshal preserves both.
   - Phase 9.2.1 and 9.2.2 tests stay green.
   - Two-step round-trip stable with IsA (marshal+unmarshal+marshal byte-equal).
8. Documentation: MarshalJSON / UnmarshalJSON godoc mentions prefabs and combined topo-sort; `doc.go` JSON section gains the prefabs example; CHANGELOG \"Unreleased\" gains \"Phase 9.2.3: IsA prefab serialization\".
9. Mechanical acceptance: `go test ./... -race -count=2`, `go vet ./...`, `golangci-lint run` all clean. Coverage on `flecs` >= 90% (no regression from 96.8%). All exported symbols documented.

### Implementer notes

- Reuse `EachPrefab` (built atop `eachPairTarget`) for IsA enumeration; reuse `ParentOf` for ChildOf. Do not re-implement either pair walk.
- Populate per-entity `Prefabs` AFTER topo-sort assigns serials so `idToSerial` lookups succeed.
- Do NOT modify `EachPrefab` / `EachChild` / `ParentOf` / `PrefabOf` / `AddID`.
- Do NOT change the `version` field (stays at 1) or the `Parent` field's semantics.
- Do NOT add third-party deps.
- A single DFS handles cycle detection across both edge kinds — no separate per-relationship cycle pass.

## Constraints

- @marshal.go — extend `jsonEntity` with `Prefabs []int`; refactor `parentOf` -> `predecessorsOf map[ID][]ID`; extract topo-sort into a `marshaler` helper; update cycle error wording to use SERIAL; populate Prefabs after serials assigned; preserve `EachPrefab` order; filter built-ins; unmarshal phase 3 adds IsA pairs after Parent and before components.
- @marshal_test.go — add the test cases enumerated above; keep all Phase 9.2.1 and 9.2.2 tests green.
- @isa.go — source of `EachPrefab`, `PrefabOf`, and the `IsA()` accessor. Marshal reads via `EachPrefab`; unmarshal writes via `AddID(w, e, MakePair(w.IsA(), prefab))`. Do not modify.
- @childof.go — source of `ParentOf` already used by Phase 9.2.2. Continue to use for the ChildOf predecessor edge. Do not modify.
- @pair_internal.go — `eachPairTarget` underlies `EachPrefab`; do not re-implement the pair walk.
- @id_ops.go — `AddID` and `MakePair` are the unmarshal-side restore primitives. Do not modify.
- @world.go — entity allocation and built-in ID filtering live here; the marshaler's \"user entity\" predicate must continue to exclude built-ins (ChildOf, IsA, phases) as values in `predecessorsOf`.
- @meta.go — component name registry used by serialization; unchanged.
- @id.go — ID type and helpers; unchanged.
- @doc.go — extend the JSON Serialization section with a prefabs example showing multi-prefab usage and round-trip inheritance.
- @CHANGELOG.md — append \"Phase 9.2.3: IsA prefab serialization\" under Unreleased.
- C reference (cite, do not @-reference): `/work/agents/claude/projects/SanderMertens/flecs/src/addons/json/serialize_world.c` and `/work/agents/claude/projects/SanderMertens/flecs/src/addons/json/deserialize.c` — model for serialization/deserialization shape; this phase intentionally diverges by representing IsA targets as a `prefabs` integer array rather than custom pair encoding (Phase 9.2.4 will handle generic pairs).
- Format version stays at v1; `prefabs` is an additive optional field with `omitempty`. No breaking changes to MarshalJSON / UnmarshalJSON signatures.
- Combined ChildOf+IsA cycle detection: one DFS; no per-relationship passes. Built-in entities are filtered both from `predecessorsOf` keys and values.
- Mechanical gates: `go test ./... -race -count=2`, `go vet ./...`, `golangci-lint run` clean; flecs coverage >= 90% (no regression from 96.8%); all exported symbols carry godoc.
