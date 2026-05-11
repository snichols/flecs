## Goal

Phase 9.2.4 of the Go port of flecs. Phase 9.2.1's marshal currently skips ALL non-ChildOf, non-IsA pair components from the `components` map — they are silently lost on round-trip. This phase fixes that gap by serializing arbitrary pair components (tag-only or data-bearing) into a new `pairs` array on each entity.

After this lands:

```go
w := flecs.New()
follows := w.NewEntity()
type Edge struct { Weight float32 }

alice := w.NewEntity()
bob := w.NewEntity()
flecs.SetPair[Edge](w, alice, follows, bob, Edge{Weight: 0.8})

charlie := w.NewEntity()
flecs.AddID(w, alice, flecs.MakePair(follows, charlie))  // tag-only pair

data, _ := w.MarshalJSON()
// data contains entity for alice with:
// "pairs": [
//   {"rel": 1, "tgt": 3, "dataType": "pkg.Edge", "data": {"Weight": 0.8}},
//   {"rel": 1, "tgt": 4}  // tag-only pair, no data/dataType
// ]

w2 := flecs.New()
flecs.RegisterComponent[Edge](w2)  // Edge data type must be pre-registered
w2.UnmarshalJSON(data)
// alice's pair with bob carries Edge{Weight: 0.8}; pair with charlie is tag-only.
```

Reviewer note from Phase 9.2.3: `serialToID map[int]ID` is built up-front in Phase 1 (before any restoration walk), so forward-references work — custom pairs do NOT require topological ordering.

**This phase implements ONLY:**
- Custom pair components in the `pairs` array (rel/tgt serials + optional dataType + data).
- Tag-only pairs (no data field).
- Data-bearing pairs that auto-register pair TypeInfo on Unmarshal.
- ChildOf and IsA pairs continue to use the dedicated `parent`/`prefabs` fields and are NOT duplicated in `pairs`.

**This phase does NOT implement:**
- Pair-as-relationship-target inheritance changes.
- Cross-pair-id constraints (registry already enforces unique data type per pair-id).
- Pair-id name as `\"pair(rel, tgt)\"` strings (JSON uses numeric serials for portability).
- Wildcards.
- New format version (additive field, v1 stays).

## Deliverables

### 1. New public method: `(*World).SetPairByID(e, rel, tgt ID, v any)`

Non-generic pair-data setter that auto-registers the pair's TypeInfo on first call. Symmetric to `SetPair[T]` but for the dynamic case (used by JSON Unmarshal).

- Validates: `e` alive (panic otherwise), `v != nil` (panic with clear message).
- Compute `pairID := MakePair(rel, tgt)`.
- Look up pair's existing TypeInfo via `w.registry.LookupByID(pairID)`.
- If NOT registered yet: register a new TypeInfo for `pairID` with `v`'s `reflect.TypeOf` as the data type. Mirrors `SetPair[T]` via `component.RegisterPairData[T]` but type comes from `v` dynamically.
  - Need a helper: `component.RegisterPairDataByType(r *Registry, pairID ID, t reflect.Type) *TypeInfo` — exported within the internal package. Takes a `reflect.Type` and builds the TypeInfo accordingly. Same pointer-distinct copy + name-format as `RegisterPairData[T]`.
  - Or: add a method `(r *Registry) AssociatePairData(pairID ID, t reflect.Type) *TypeInfo` — same effect, different signature shape. Implementer's call.
- If already registered with a DIFFERENT type: panic (matches `SetPair[T]` semantics) with message `\"SetPairByID: pair (rel=%d, tgt=%d) is already registered with type %s, cannot set with type %s\"`.
- After ensuring registration, call `w.SetByID(e, pairID, v)` — which fires hooks, observers, honors Defer queue.
- Document the type-mismatch panic, the auto-register-on-first-use semantic, and that this fires hooks/observers.

### 2. Extend `jsonEntity` in `marshal.go`

```go
type jsonEntity struct {
    Serial     int                        `json:\"serial\"`
    Name       string                     `json:\"name,omitempty\"`
    Parent     int                        `json:\"parent,omitempty\"`
    Prefabs    []int                      `json:\"prefabs,omitempty\"`
    Pairs      []jsonPair                 `json:\"pairs,omitempty\"` // NEW
    Components map[string]json.RawMessage `json:\"components,omitempty\"`
}

type jsonPair struct {
    Rel      int             `json:\"rel\"`
    Tgt      int             `json:\"tgt\"`
    DataType string          `json:\"dataType,omitempty\"` // omit for tag-only pairs
    Data     json.RawMessage `json:\"data,omitempty\"`     // omit for tag-only pairs
}
```

### 3. Marshal changes

In the per-entity pair-collection loop, replace the existing \"skip all pair IDs\" with:

- For each `cid` where `cid.IsPair() == true`:
  - Extract `rel := cid.First()`, `tgt := cid.Second()` (note: `First` returns a 28-bit ID; coerce to full entity ID by promoting through the existing pair helpers).
  - **Skip ChildOf pairs** (`rel == w.ChildOf()`) — handled by `Parent` field.
  - **Skip IsA pairs** (`rel == w.IsA()`) — handled by `Prefabs` field.
  - For all other pairs: append to `entity.Pairs`:
    - Map `rel` and `tgt` to their serials via `idToSerial`. If either is a built-in entity (not in `idToSerial`), skip the pair with a debug-log-style comment (or document as known limitation: pairs referencing built-ins are not serialized).
    - Look up `info := w.ComponentInfo(pairID)`:
      - If `info.Size > 0` (data-bearing): get the value via `w.GetByID(e, pairID)`, get its data type name (from `info.Name` — recall pair TypeInfo has name like \"pair(BaseType)\"; but for round-trip purposes we need the BASE type name, which is `info.Type.String()` matching the base TypeInfo). Set `Pair.DataType = info.Type.String()` (equals the registered base type's name).
      - Marshal the value to `json.RawMessage` via `json.Marshal(v)`.
      - If `info.Size == 0` (tag pair): leave DataType and Data empty (json.RawMessage zero is `null`; let omitempty drop it).
    - Append the `jsonPair{Rel, Tgt, DataType, Data}` to the entity's Pairs slice.

Note: pair-rel ENTITY is stored by serial. If `rel` itself is a non-built-in entity (e.g., a user-created `follows` entity), its serial is in `idToSerial` because all user entities are pre-allocated. **Exception**: built-in entities don't have serials. The `rel` of a custom pair is typically a user-created tag entity (like `follows`), so this works. ChildOf and IsA are the only built-ins likely to be pair rels.

### 4. Unmarshal changes

In phase 3, AFTER Parent, AFTER Prefabs, BEFORE components:

- For each pair in `entity.Pairs`:
  - Resolve `relID := serialToID[pair.Rel]`; error if missing: `\"flecs: unmarshal failed: pair rel serial %d not found\"`.
  - Resolve `tgtID := serialToID[pair.Tgt]`; error if missing.
  - If `pair.DataType == \"\"` (tag pair): `flecs.AddID(w, e, flecs.MakePair(relID, tgtID))`.
  - Else (data pair):
    - Look up the base data type by name: scan `w.Components()` and find the one whose `info.Type.String() == pair.DataType`. (Same name-based lookup the components map uses.) If not found, return error `\"flecs: unmarshal failed: pair data type %q is not registered in the world\"`.
    - Create a fresh value: `vPtr := reflect.New(info.Type)`. Decode `pair.Data` into `vPtr.Interface()` via `json.Unmarshal`.
    - Call `w.SetPairByID(e, relID, tgtID, vPtr.Elem().Interface())` to apply.

### 5. `component.RegisterPairDataByType` (or equivalent)

Internal helper exposed for SetPairByID:

- Build a `*TypeInfo` with the given `reflect.Type`'s Size/Align (via `t.Size()` and `t.Align()`), Name = `\"pair(\" + t.String() + \")\"`, Type = t, Hooks = `Hooks{}`.
- Call `r.AssociateID(info, pairID)` (idempotent on same type, panic on different).
- Returns the TypeInfo pointer.

### 6. Tests in `marshal_test.go`

- **Tag-only pair round-trip:** `follows := w.NewEntity()`; `AddID(w, alice, MakePair(follows, bob))`; marshal+unmarshal; `HasID(w2, restoredAlice, MakePair(restoredFollows, restoredBob))` is true.
- **Data-bearing pair round-trip:** `SetPair[Edge](w, alice, follows, bob, Edge{Weight: 0.8})`; round-trip; `GetPair[Edge](w2, restoredAlice, restoredFollows, restoredBob)` returns `(Edge{Weight: 0.8}, true)`.
- **Mixed: ChildOf + IsA + custom pair on one entity:** all three serialize via their respective fields (parent, prefabs, pairs); round-trip preserves all three.
- **Multiple custom pairs on one entity:** alice with `(follows, bob)`, `(follows, charlie)`, `(likes, dave)`; round-trip; all three present.
- **Pair where rel is a user-defined entity (most common case):** verify the rel entity is serialized as a regular entity, and its serial is correctly resolved on Unmarshal.
- **Unknown pair data type → error:** hand-craft JSON with `\"dataType\":\"pkg.NotRegistered\"`; Unmarshal returns error.
- **Unknown pair rel/tgt serial → error:** hand-craft JSON with `\"rel\":999`; Unmarshal returns error mentioning \"pair rel serial 999\".
- **ChildOf/IsA pairs NOT in `pairs` field:** verify the JSON has `parent`/`prefabs` but the `pairs` field is omitted (empty array dropped by omitempty).
- **SetPairByID auto-registers:** call SetPairByID on a fresh pair; verify it works and that `ComponentInfo(pairID)` returns the new TypeInfo.
- **SetPairByID type mismatch panics:** SetPairByID once with Edge, then SetPairByID with a different type → panic.
- **SetPairByID fires hooks/observers:** register OnSet for Edge; SetPairByID with Edge value; OnSet fires on the pair's TypeInfo (matches Phase 5.1 wiring).
- **Tag pair after data pair on same pairID:** call `AddID(w, e, pairID)` after a `SetByID(e, pairID, Edge{})` — should this work? Document. Likely: AddID is a no-op if entity already has the pair (idempotent). Test.
- **Two-step round-trip stable with pairs:** marshal+unmarshal+marshal; outputs match.
- **Phase 9.2.1/9.2.2/9.2.3 tests stay green.**

### 7. Documentation

- Update `MarshalJSON` / `UnmarshalJSON` godoc to mention `pairs` field with the data-vs-tag distinction.
- Update `doc.go` JSON section with a custom-pair example.
- Update CHANGELOG.md \"Unreleased\": \"Phase 9.2.4: Custom pair component serialization (data + tag-only)\".
- Update README feature list / ROADMAP if needed (full pair support now shipped).

### 8. Mechanical acceptance

- `go test ./... -race -count=2` passes.
- `go vet ./...` clean.
- `golangci-lint run` clean.
- Coverage on `flecs` ≥ 90% (no regression from 96.7%).
- Coverage on `internal/component` ≥ 95%.
- All exported symbols have godoc.

## Non-goals

- NO pair-introspection helpers (e.g., `EachPair(e, fn)` — could be useful but defer).
- NO change to v1 JSON format version.
- NO pair wildcards.
- NO restoration order guarantee beyond \"pairs applied before components, in array order.\"
- NO cross-document pair references (rel/tgt must be in the same JSON).
- NO multiple `SetPairByID` types on the same pair (panics).
- NO change to existing `SetPair[T]` / `GetPair[T]` / `AddID` / `MakePair` semantics.

## Constraints

- The new `SetPairByID` belongs in `value_ops.go` (sibling to `SetByID`).
- Use `info.Type.String()` (the BASE component's Go type name) as the `DataType` in JSON — NOT the pair's `info.Name` (which has the \"pair(...)\" wrapper). This makes the JSON name match the regular component name format. Document.
- Pair-id rel/tgt serial mapping: pairs whose rel or tgt is a built-in entity (e.g., a phase or ChildOf itself) won't have a serial in `idToSerial` — skip those. In practice this only happens for `(ChildOf, X)` and `(IsA, X)`, which are already excluded by the dedicated fields, so this should never trigger in practice. Defensive coding.
- The `Pairs` array's order in JSON is `EntityComponents` order (sorted by ID). This is deterministic.
- On Unmarshal, applying pairs BEFORE components is intentional: it ensures that any OnAdd/OnSet hooks observing pair-id events get the data-bearing components in the right order (pair structure before component values, matching how the user would typically build the entity).
- DO NOT modify `SetPair[T]` / `GetPair[T]` / `AddID` / `MakePair` signatures.
- DO NOT change the v1 format version.
- DO NOT collapse pairs and components into a single map (they have different shapes; the `pairs` array is the right design).
- DO NOT import third-party deps.
- The Phase 9.2.3 marshaler helper struct stays unchanged; pair collection is independent of topo-sort.

## Relevant files

C reference (filesystem paths — cite, do not @-reference):
- `/work/agents/claude/projects/SanderMertens/flecs/src/addons/json/serialize_world.c`
- `/work/agents/claude/projects/SanderMertens/flecs/src/addons/json/deserialize.c`

Go API the implementer builds on:
- @marshal.go (extend)
- @marshal_test.go (extend)
- @value_ops.go (add SetPairByID here)
- @id_ops.go (SetPair[T] reference)
- @id.go
- @meta.go
- @internal/component/registry.go (add RegisterPairDataByType or similar)
- @internal/component/typeinfo.go
- @world.go
- @doc.go
- @CHANGELOG.md
