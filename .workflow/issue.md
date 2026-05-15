## Goal

Extend the REST `GET /type_info/{path}` endpoint (shipped in Phase 16.32 / v0.87.0) from a **depth-1 reflect walk** to **depth-N recursion with cycle detection and precise primitive-type annotations**.

Currently the endpoint walks a struct one level deep and returns each field's name + Go kind. This phase makes it:

1. **Recurse into nested structs** to arbitrary depth (configurable max, default 8) — capturing the full type tree.
2. **Detect cycles** (e.g. `type Node struct { Next *Node }`) — emit a `"recursive"` marker instead of looping.
3. **Annotate primitive types precisely** — distinguish `int8`/`int16`/`int32`/`int64`, `uint8`/.../`uint64`, `float32`/`float64`, `bool`, `string`, `byte` (alias for uint8 — reported as `"byte"`), `rune` (alias for int32 — reported as `"rune"`).
4. **Handle slices/arrays/maps/pointers** — emit the kind plus the element type (recursively).
5. **Handle unit annotations** (already present for depth-1) — preserve at every nesting level.

### Response shape (v1 — propose, iterate agent may refine)

```json
{
  "name": "game.Player",
  "id": 42,
  "size": 32,
  "align": 8,
  "kind": "struct",
  "fields": [
    {"name": "Pos", "kind": "struct", "type": "game.Vec3", "fields": [
      {"name": "X", "kind": "primitive", "type": "float32"},
      {"name": "Y", "kind": "primitive", "type": "float32"},
      {"name": "Z", "kind": "primitive", "type": "float32"}
    ]},
    {"name": "HP",  "kind": "primitive", "type": "int32", "unit": "Percentage"},
    {"name": "Tags", "kind": "slice", "element": {"kind": "primitive", "type": "string"}},
    {"name": "Stats", "kind": "map", "key": {"kind": "primitive", "type": "string"}, "value": {"kind": "primitive", "type": "int32"}},
    {"name": "Owner", "kind": "pointer", "element": {"kind": "struct", "type": "game.Player", "recursive": true}}
  ]
}
```

Iterate agent should verify this shape against the depth-1 output already produced by `restTypeInfo` and propose adjustments to **preserve back-compat** for clients that only consume the top-level depth-1 fields.

### Configurable depth

- Add `?depth=N` query parameter (default 8; max enforced at 16 to bound CPU)
- `?depth=0` returns just the top-level struct header (name/id/size/align/kind) with no field expansion
- `?depth=1` matches current behavior (back-compat — must be byte-identical to v0.87.0)
- Unknown / negative / > 16 → `400 Bad Request`

### Primitive type detection

Map Go `reflect.Kind` to the response's `type` field:
- `Bool` → `"bool"`
- `Int8`/`Int16`/`Int32`/`Int64`/`Int` → `"int8"` / `"int16"` / `"int32"` / `"int64"` / `"int"` (use `Kind().String()`)
- Same for unsigned variants
- `Float32`/`Float64` → `"float32"` / `"float64"`
- `Complex64`/`Complex128` → `"complex64"` / `"complex128"`
- `String` → `"string"`

For named aliases via `reflect.Type.Name()`:
- If the **named** type is `byte` (which is `uint8`) — emit `"byte"`
- If the **named** type is `rune` (which is `int32`) — emit `"rune"`
- For any other named primitive (e.g. `type Score int32`) — emit `"Score"` as `type`, with `"underlying": "int32"` to disambiguate

For complex / non-primitive kinds:
- `Struct` → `"kind": "struct"`, recurse into fields
- `Slice` → `"kind": "slice"`, recurse into `Elem()`
- `Array` → `"kind": "array"`, `"length": N`, recurse into `Elem()`
- `Map` → `"kind": "map"`, recurse into `Key()` and `Elem()`
- `Ptr` → `"kind": "pointer"`, recurse into `Elem()` (with cycle detection)
- `Interface` → `"kind": "interface"` (no recursion — runtime-only)
- `Chan`, `Func`, `UnsafePointer` → `"kind": "<kind>"` (no recursion; document as opaque)

### Cycle detection

Use a `seen map[reflect.Type]bool` along the recursion path. If a type is encountered twice on the same path, emit `{"kind": "struct", "type": "<name>", "recursive": true}` instead of recursing. Reset `seen` between siblings (not just along the path); two unrelated siblings should both be able to reference the same type at depth >= 2 independently.

### File layout

- Extend `restTypeInfo` handler in @rest.go (currently 1046 LOC at line 468)
- New helper `walkTypeForJSON(t reflect.Type, depth int, maxDepth int, seen map[reflect.Type]bool) any` — extract into a new file @rest_type_walk.go if `rest.go` is getting large (iterate agent to decide)

### Required tests (extend or create alongside existing @rest_type_info_test.go)

- `TestRest_TypeInfo_DepthDefault8` — nested struct 8 levels deep; default request returns all levels
- `TestRest_TypeInfo_DepthExplicit3` — same nested struct; `?depth=3` returns levels 1-3, level 4 elided
- `TestRest_TypeInfo_Depth0` — `?depth=0` returns only top-level header
- `TestRest_TypeInfo_Depth1_BackCompat` — `?depth=1` returns byte-identical response to v0.87.0 behavior
- `TestRest_TypeInfo_DepthInvalid` — `?depth=-1` → 400; `?depth=99` → 400; `?depth=abc` → 400
- `TestRest_TypeInfo_RecursiveType` — `type Node struct { Next *Node; Value int }` — cycle detected; `"recursive": true` marker
- `TestRest_TypeInfo_MutualRecursion` — `type A struct { B *B }`; `type B struct { A *A }` — both cycles detected
- `TestRest_TypeInfo_SiblingTypeReuse` — `type T struct { L *Sub; R *Sub }`; both L and R expand fully (NOT marked recursive — they're siblings, not a cycle)
- `TestRest_TypeInfo_PrimitiveAnnotations` — struct with one field per primitive Go kind; each field's `type` reports the exact kind name
- `TestRest_TypeInfo_ByteAndRune` — fields of type `byte` and `rune` (aliases) report `"byte"` and `"rune"`, not `"uint8"` / `"int32"`
- `TestRest_TypeInfo_NamedPrimitive` — `type Score int32` reports `"type": "<pkg>.Score"`, `"underlying": "int32"`
- `TestRest_TypeInfo_SliceArrayMap` — struct with `[]string`, `[5]int`, `map[string]int` — each field reports kind + nested element details
- `TestRest_TypeInfo_Pointer` — `*Vec3` field expands its target with cycle detection
- `TestRest_TypeInfo_Interface` — `interface{}` field reports `"kind": "interface"` with no nested expansion
- `TestRest_TypeInfo_DynamicComponent` — dynamic component (no Go type) still returns header (name/id/size/align) but with `"kind": "dynamic"` and no fields
- `TestRest_TypeInfo_UnitAnnotation_Nested` — nested struct field carrying a Unit annotation preserves it
- `TestRest_TypeInfo_DepthLimit16Enforced` — request `?depth=17` → 400

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run ./...` clean
- `go test ./... -race -count=3` clean
- Coverage ≥ 95% (current baseline)
- Back-compat: `?depth=1` MUST produce byte-identical JSON to the v0.87.0 endpoint (test asserts this)

### Documentation update matrix

- @docs/FlecsRemoteApi.md — extend `/type_info/{path}` section with depth parameter, response shape examples for nested structs, slice/map/pointer handling, cycle detection, primitive annotations
- @docs/README.md:
  - **Line 181** — flip "Full meta-cursor parity (depth-N recursion, primitive-type annotations) not yet ported" to shipped in v0.94.0
  - **Line 90** — fix stale "multi-period stats aggregation remain outstanding" (shipped in v0.93.0 — only query DSL remains outstanding)
- @CHANGELOG.md — v0.94.0 entry
- @ROADMAP.md — bump heading; add Phase 16.39 row
- @README.md — update REST feature row if individual endpoints tracked

### Non-goals

- Generating OpenAPI schema or other meta-format export from type-info — this phase emits JSON only
- Field tag parsing (e.g. `json:"foo,omitempty"`) — leave for a future phase
- Runtime value inspection — type-info is structural, not value-based; that's the `/component` endpoint's job

### Notes

- Target version: **v0.94.0**
- Phase number: **16.39**
- Verify the existing `restTypeInfo` shape and field names — preserve them at the top level
- Document the cycle-detection semantics carefully — siblings reuse vs path repeat

## Constraints

- @rest.go — extend `restTypeInfo` handler at line 468 (currently 1046 LOC); existing `typeInfoFieldResponse` (line 176) and `typeInfoResponse` (line 183) JSON shapes define the v0.87.0 contract that `?depth=1` must remain byte-identical to
- @rest_type_info_test.go — existing Phase 16.32 tests live here; extend in place rather than creating a parallel file
- @internal/component/registry.go — `TypeInfo` registry exposes `*TypeInfo.Type reflect.Type`, the entry point for the reflect walk
- @units.go — unit annotations on component fields (already integrated at depth-1) must be preserved at every nesting level
- @component_dynamic.go — dynamic components have no Go type; emit `"kind": "dynamic"` with header-only response (no fields)
- @docs/FlecsRemoteApi.md — REST API reference; extend `/type_info/{path}` section with depth parameter and recursion semantics
- @docs/README.md — docs index; lines 90 and 181 carry stale status text that this phase flips
- @CHANGELOG.md — version log; add v0.94.0 entry
- @ROADMAP.md — phase tracker; bump heading and add Phase 16.39 row
- @README.md — top-level project readme; update REST feature row if individual endpoints are tracked
- Mechanical gates: `go vet ./...`, `golangci-lint run ./...`, `go test ./... -race -count=3` all clean; coverage ≥ 95%
- Back-compat is load-bearing: `?depth=1` MUST produce byte-identical JSON to v0.87.0 (an explicit test asserts this)
- Depth bounded at 16 to cap CPU; out-of-range / non-numeric → 400 Bad Request
- Cycle-detection semantics: `seen` tracks the recursion path only; siblings independently re-expand the same type (the path-vs-tree distinction is the load-bearing contract)
- Non-goals: no OpenAPI/meta-format export, no struct tag parsing, no runtime value inspection (that belongs to `/component`)
