## Goal

Implement basic JSON serialization for a flecs world — entities, non-pair components, and names — building on the Phase 9.1 introspection API and 9.1.1 GetByID/SetByID dynamic value access. After this phase, users can save a world to a JSON document and restore it into a fresh world (with components pre-registered).

**Target API:**

```go
// Save:
data, err := w.MarshalJSON()
os.WriteFile(\"save.json\", data, 0644)

// Load:
data, _ := os.ReadFile(\"save.json\")
w2 := flecs.New()
// Components must be registered BEFORE Unmarshal.
flecs.RegisterComponent[Position](w2)
flecs.RegisterComponent[Velocity](w2)
err = w2.UnmarshalJSON(data)
// w2 now has the same entities + components + names as w (modulo built-in IDs).
```

**Round-trip property:** for a world `w` containing only entities/components/names (no pairs, no hierarchies), `w2.UnmarshalJSON(w.MarshalJSON())` produces a world equivalent to `w` in entity count, names, and component values.

**JSON format (v1):**

```json
{
  \"version\": 1,
  \"entities\": [
    {
      \"serial\": 1,
      \"name\": \"foo\",
      \"components\": {
        \"pkg.Position\": {\"X\": 1.5, \"Y\": 2.0},
        \"pkg.Velocity\": {\"X\": 0, \"Y\": 0}
      }
    },
    {
      \"serial\": 2,
      \"components\": {
        \"pkg.Marker\": {}
      }
    }
  ]
}
```

- `version` is always `1` for this phase.
- `entities` is an array; each entity has a stable `serial` number (starts at 1, increments per non-built-in entity).
- `name` is optional; present only if the entity has a non-empty Name component.
- `components` is a map from component name (matching `ComponentInfo.Name`) to the JSON-encoded value.
- Built-in entities (ChildOf, IsA, Name-as-entity, PreUpdate, OnUpdate, PostUpdate, OnFixedUpdate) are SKIPPED.
- The Name COMPONENT on entities is serialized as the `name` field, NOT as a component entry.
- Pair components are SKIPPED in this phase (deferred to 9.2.4); document this.
- Tag components serialize as empty objects `{}` so they round-trip.
- Component values use Go's `encoding/json`.

**Scope: basic JSON only.** This phase does NOT implement:
- ChildOf hierarchies (Phase 9.2.2).
- IsA prefabs (Phase 9.2.3).
- Pair components or pair-data (Phase 9.2.4).
- Hook / observer / system serialization (runtime state, not data).
- Custom JSON formats / streaming / incremental save-load.
- Schema validation beyond \"all named components must be registered before Unmarshal.\"
- Backward compatibility across format versions.
- Binary or other formats.

## Deliverables

1. **New file `marshal.go`** in root `flecs` package with the JSON serialization logic.

2. **`func (w *World) MarshalJSON() ([]byte, error)`** — implements `json.Marshaler`. Serializes the world per the format above.
   - Built-in entities SKIPPED.
   - Pair components SKIPPED (warning comment in code).
   - Name component data populates the `name` field, not `components`.
   - Tag components present as `{}`.

3. **`func (w *World) UnmarshalJSON(data []byte) error`** — implements `json.Unmarshaler`. Restores entities, names, and components.
   - World need NOT be empty; new entities are ADDED to existing ones.
   - All component types in the JSON must be pre-registered.
   - Allocation strategy:
     1. Parse JSON structure; validate version is 1.
     2. Allocate fresh entity per JSON entity via `w.NewEntity()`, building a `serial -> flecs.ID` map.
     3. For each entity: `SetName` if present; for each component, look up `ComponentInfo` by name, decode JSON into `reflect.New(info.Type).Interface()` via `json.Unmarshal`, then `SetByID`.
   - Error cases:
     - JSON parse error -> wrapping error.
     - Unknown version -> `\"flecs: unmarshal failed: unsupported version N (only v1 supported)\"`.
     - Unregistered component -> `\"flecs: unmarshal failed: component %q is not registered in the world\"`.
     - Type mismatch -> propagate wrapped json error.

4. **Implementation tactics:**
   - Internal structs: `jsonWorld { Version int; Entities []jsonEntity }`, `jsonEntity { Serial int; Name string; Components map[string]json.RawMessage }`. Use `json.RawMessage` to defer per-component decoding until type is known.
   - On Marshal: iterate `w.EachEntity` -> `w.EntityComponents(e)` -> `w.GetByID(e, id)`. Use `w.ComponentInfo(id).Name` for map keys.
   - Filter pair-component IDs (`id.IsPair() == true`) in `EntityComponents`.
   - Built-in entity skip-set populated from `w.ChildOf()`, `w.IsA()`, `w.Name()`, and the four phase accessors — no hardcoded magic numbers.
   - Skip the Name component ID when building the `components` map (it's already in the `name` field).
   - On Unmarshal: phase 1 parse + version validate, phase 2 allocate all entities (build serial map first — important for future pair refs), phase 3 set name + components.

5. **Tests** in `marshal_test.go`:
   - Empty world round-trip (zero user entities).
   - Single entity with single component (Position{1,2}).
   - Multiple entities with multiple components (5 entities mixing Position/Velocity/both).
   - Names round-trip (SetName on 3 entities; verify GetName).
   - Unmarshal into world with prior entities (A in w; unmarshal into w; A still exists).
   - Tag components round-trip (struct{}).
   - Unregistered component error (Position in w, no Position in w2 -> error mentioning \"Position is not registered\").
   - Wrong version error (`{\"version\": 99, \"entities\": []}` -> version-mismatch error).
   - Malformed JSON error (`{\"version\": 1, \"entities\": \"not an array\"}` -> parse error).
   - Pair components skipped (entity with Position + (ChildOf, parent); pair NOT in JSON).
   - Built-in entities skipped (freshly-created world has 7 built-ins; JSON has zero entities).
   - Floats / strings / nested structs round-trip (Position{X,Y float32}; struct{Tag string}).
   - `json.Valid(data) == true`.
   - Two-step round-trip: marshal -> unmarshal -> marshal again; equivalent outputs.
   - All existing tests stay green.

6. **Documentation:**
   - \"JSON Serialization\" section in `doc.go` with save+load snippet.
   - README.md feature list mentions JSON support.
   - CHANGELOG entry: \"Phase 9.2.1: basic JSON serialization (no pairs/hierarchies yet).\"

7. **Mechanical acceptance:**
   - `go test ./... -race -count=2` passes.
   - `go vet ./...` clean.
   - `golangci-lint run` clean.
   - Coverage on `flecs` >= 90% (no regression from 97.1%).
   - All exported symbols have godoc.

## Non-goals

- NO pair component serialization (9.2.4).
- NO ChildOf hierarchy serialization (9.2.2).
- NO IsA prefab serialization (9.2.3).
- NO hook/observer/system serialization.
- NO format version > 1.
- NO custom JSON tags / options.
- NO partial / streaming serialization.
- NO binary / msgpack / other formats.
- NO schema introspection endpoints.
- NO snapshot diff / delta.
- NO concurrent access during marshal/unmarshal.

## Constraints

- @world.go — provides `EachEntity`, `EntityComponents`, `NewEntity`, `AliveEntities`, `Components`, `ComponentInfo`. Marshal reads via these; Unmarshal allocates via `NewEntity`.
- @meta.go — `ComponentInfo` exposes `Name` and `Type` used to build the `components` map keys and to allocate `reflect.New(info.Type).Interface()` for decoding.
- @value_ops.go — `GetByID(e, id) (any, bool)` and `SetByID(e, id, v any)` are the dynamic value bridge between flecs storage and `encoding/json`.
- @name.go — `GetName(e)` and `SetName(e, name)` handle the `name` JSON field; the Name component itself is filtered out of the `components` map.
- @id.go — `ID.IsPair()` identifies pair IDs to skip in the Marshal path.
- @doc.go — add a \"JSON Serialization\" section with a save+load snippet.
- @README.md — feature list mentions JSON support after this lands.
- @CHANGELOG.md — add Phase 9.2.1 entry noting basic JSON serialization (no pairs/hierarchies yet).
- C reference (informational, format differs substantially): `/work/agents/claude/projects/SanderMertens/flecs/src/addons/json/serialize_world.c` and `/work/agents/claude/projects/SanderMertens/flecs/src/addons/json/deserialize_from_json.c`.
- Built-in entity skip-set is built dynamically from `w.ChildOf()`, `w.IsA()`, `w.Name()`, and the four phase accessors — no hardcoded magic numbers.
- Internal-package access NOT required — public API from Phase 9.1 + 9.1.1 is sufficient.
- Serial number assignment is internal to Marshal; NOT a public API.
- DO NOT introduce new public types beyond `MarshalJSON`/`UnmarshalJSON`.
- DO NOT modify any existing public API.
- DO NOT add `encoding/gob` or other format support.
- DO NOT import third-party deps.
