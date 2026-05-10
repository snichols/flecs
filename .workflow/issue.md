## Goal

Bootstrap the Go port of flecs with foundational repo scaffolding and the entity/pair ID primitive. No ECS storage, no world, no entity-as-object yet — just CI, license, lint config, and the `ID uint64` bit-packed type with its constructors and accessors.

This is the first issue in the port. Everything else (type registry, world, storage, queries, observers) builds on top of `ID`, so the bit layout must mirror upstream exactly. Get the foundation right and subsequent work is mechanical.

### Deliverables

**1. Repo scaffolding (repo root)**

- `README.md` — short. State this is a Go port of flecs, link to upstream `https://github.com/SanderMertens/flecs`, note port is in progress. No emojis.
- `.golangci.yml` — reasonable defaults for a high-performance library: enable `govet`, `staticcheck`, `errcheck`, `ineffassign`, `unused`, `gofmt`, `goimports`. Treat warnings as errors.
- `.github/workflows/ci.yml` — runs `go test ./... -race` and `golangci-lint run` on push and pull_request. Pin to Go 1.26.x (matches go.mod).
- `LICENSE` — MIT (flecs is MIT). Copy upstream's MIT license, change year to 2026 and copyright holder to `snichols`, but retain mention of the original upstream copyright so credit is preserved.

**2. Root package `flecs` — `id.go`**

- `type ID uint64` exported.
- Top-bit flag constants mirroring the C `ECS_PAIR`, `ECS_AUTO_OVERRIDE`, `ECS_TOGGLE`, `ECS_VALUE_PAIR`. Name them in Go style (not all-caps):
  - `FlagPair`
  - `FlagAutoOverride`
  - `FlagToggle`
  - `FlagValuePair`
- Methods on `ID`:
  - `Index() uint32` — lower 32 bits of an entity id.
  - `Generation() uint32` — upper 32 bits of an entity id (for non-pair ids).
  - `WithGeneration(gen uint32) ID` — return a copy with generation replaced (index preserved).
  - `IsPair() bool` — true iff `FlagPair` bit is set.
  - `First() ID` — for pair ids, the relationship part (28-bit field). Returns 0 if not a pair.
  - `Second() ID` — for pair ids, the target part (32-bit field). Returns 0 if not a pair.
  - `HasFlag(flag ID) bool` — generic top-bits flag test.
  - `String() string` — debug format: entities as `e:<idx>#<gen>`, pairs as `(<first>,<second>)`.
- Top-level constructors:
  - `MakeEntity(index, generation uint32) ID`
  - `MakePair(first, second ID) ID` — masks `first` to 28 bits, `second` to 32 bits, sets `FlagPair`.

All exported symbols documented with godoc-style comments.

**3. Tests — `id_test.go`**

Table-driven, exhaustive on the bit layout. Cover:

- Entity round-trip: `MakeEntity` → `Index` / `Generation` returns inputs.
- Generation overwrite via `WithGeneration` preserves index.
- Pair encode/decode: `MakePair` → `First` / `Second` / `IsPair`.
- Flag tests: `HasFlag` returns true only for set bits.
- Zero-value `ID(0)`: `IsPair() == false`, `Index() == 0`, `Generation() == 0`.
- `String()` produces the expected format for entity and pair inputs (a couple of golden cases).

### Non-goals (do NOT port in this issue)

- No `World`, no entity allocator, no storage, no tables, no queries, no observers.
- No type registry (next issue).
- No mirror of C macros (`ECS_COMPONENT`, etc.).
- No cgo.
- No vendoring of any flecs source.

### Mechanical acceptance

- `go test ./... -race` passes.
- `golangci-lint run` passes with the committed config.
- `go vet ./...` passes.
- `README.md`, `LICENSE`, `.golangci.yml`, `.github/workflows/ci.yml` all exist at repo root.
- `ID` type and all methods/constructors listed above are exported and documented.

## Constraints

- Read `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` lines 380-385 — defines the entity ID bit layout (lower 32 bits = unique id, upper 32 bits = generation counter). Mirror exactly.
- Read `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` lines 1670-1683 — defines the id flag constants `ECS_PAIR`, `ECS_AUTO_OVERRIDE`, `ECS_TOGGLE`, `ECS_VALUE_PAIR`. Mirror the exact bit values into the Go constants. Do not infer values — read them.
- These C header paths are read-only references on the implementer's filesystem; they are NOT part of this repo. Do not copy, vendor, or commit any C source.
- Pair encoding: `first` is 28 bits, `second` is 32 bits, with `FlagPair` set in the top bits. Match upstream's exact field positions — read the header, do not guess.
- Module path is `github.com/snichols/flecs`, Go 1.26.1 (per existing `go.mod`). CI must pin to the Go 1.26.x minor.
- License must remain MIT-compatible with upstream and credit the original flecs copyright holder.
