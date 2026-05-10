## Goal

Introduce the component metadata layer for the Go port of flecs: a `TypeInfo` describing a Go type as a component (size, alignment, name, hook placeholders) and a `Registry` keyed by `reflect.Type`. This is **Phase 1.3** and is deliberately decoupled from entity-ID issuance — Component IDs are not assigned here; that work lands in Phase 1.5 once `World` exists and integrates this registry with the entity allocator. Phase 1.4 (tables) will read `TypeInfo` to lay out SoA columns.

What this phase does *not* do is as important as what it does:
- No component-as-entity ID issuance (Phase 1.5).
- No `OnAdd`/`OnSet`/`OnRemove` hook signatures — those depend on `*World`, which doesn't exist yet. Including them now would force speculative signatures we'd rewrite.
- No `Component flecs.ID` field on `TypeInfo` (marked with TODO for 1.5).
- No serialization, no reflection-based field walking, no comparison hooks (`Cmp`, `Equals` from the C API). Deferred.
- No thread-safety. The registry is single-threaded by contract.
- No global registry. `NewRegistry()` returns a value; `World` will own one in 1.5.

### Deliverables

**New internal package `internal/component`** with files `typeinfo.go`, `registry.go`, and corresponding `_test.go` files.

#### `type Hooks struct` (in `typeinfo.go`)

Deliberately-minimal placeholder. Define exactly these fields, all optional (nil = not registered):

- `Move func(dst, src unsafe.Pointer)` — copy bytes from `src` to `dst` during archetype migration. If nil, callers do a plain byte copy of `Size` bytes (runtime.memmove equivalent). Reserved for future use; tests should confirm a nil `Move` is allowed.
- Add this comment verbatim, no fields:
  ```go
  // Reserved for OnAdd/OnSet/OnRemove — wired in Phase 1.5 when World exists.
  ```

Godoc contract: \"all hooks are optional; nil means use default semantics.\"

#### `type TypeInfo struct` (in `typeinfo.go`)

All fields exported and documented:

- `Size uintptr` — `unsafe.Sizeof` of the registered type.
- `Align uintptr` — `unsafe.Alignof` of the registered type.
- `Name string` — `reflect.Type.String()` (e.g. `\"main.Position\"`, `\"int\"`, `\"github.com/user/pkg.Foo\"`).
- `Hooks Hooks` — copy of the hooks struct registered for this type.
- Add this comment verbatim, no field:
  ```go
  // TODO(phase-1.5): add Component flecs.ID once the World assigns entity IDs to components.
  ```

#### `type Registry struct` (in `registry.go`)

Unexported fields. Maps `reflect.Type → *TypeInfo` (and only that — no ID indirection in Phase 1.3).

#### Functions / methods

- `func NewRegistry() *Registry` — constructor.
- `func Register[T any](r *Registry) *TypeInfo` — generic free function (Go does not allow generic methods). **Idempotent**: calling `Register[Foo](r)` twice returns the same `*TypeInfo` pointer (no duplicate entry). Populates Size/Align/Name on first registration. Hooks default to zero-value.
- `func RegisterWithHooks[T any](r *Registry, hooks Hooks) *TypeInfo` — same as `Register` but seeds the hooks field. If already registered, the existing TypeInfo's hooks are **replaced** with the provided ones (document: \"subsequent calls with new hooks override prior hooks for that type\"). Returns the same `*TypeInfo` pointer that any prior `Register` returned.
- `func LookupByType[T any](r *Registry) (*TypeInfo, bool)` — generic free lookup. `(nil, false)` if not registered.
- `func (r *Registry) LookupByReflectType(t reflect.Type) (*TypeInfo, bool)` — runtime-type lookup, for callers holding a `reflect.Type` rather than a static `T`.
- `func (r *Registry) Each(fn func(reflect.Type, *TypeInfo))` — iterate registered types in **insertion order**. Document that the callback must NOT register new types during iteration.
- `func (r *Registry) Count() int`.

#### Reflection details

- Use `reflect.TypeFor[T]()` (Go 1.22+; module is on 1.26.1). Do NOT use `reflect.TypeOf((*T)(nil)).Elem()` — `TypeFor` is the modern idiom.
- For size: `unsafe.Sizeof(*new(T))` or `reflect.TypeFor[T]().Size()` — implementer's call, but document which.
- For align: `unsafe.Alignof(*new(T))` or `reflect.TypeFor[T]().Align()`.

### Tests

`registry_test.go` and `typeinfo_test.go` as appropriate. Table-driven where it fits. Cover at minimum:

- Register a basic struct (e.g. `type Position struct { X, Y float32 }`); Size==8, Align==4, Name==`\"component_test.Position\"` (or however reflect formats it for the test package).
- Register primitives: `int`, `float64`, `bool`. Size/Align match `unsafe.Sizeof`/`unsafe.Alignof`. Name is the primitive's `reflect.Type.String()`.
- Register a zero-size struct (`struct{}`). Size==0. Registration succeeds (this is how tags will be modeled later).
- Register a 64-byte struct. Size==64.
- Register-twice idempotence: same pointer; `Count()==1`.
- `RegisterWithHooks` after `Register` updates hooks on the existing TypeInfo; pointer identity preserved.
- `LookupByType[Foo]` returns the same pointer as `Register[Foo]` did.
- `LookupByType[Bar]` (never registered) returns `(nil, false)`.
- `LookupByReflectType` matches `LookupByType` for the same type.
- `Each` visits all registered types in insertion order; visit count equals `Count()`.
- `Move` hook invocation: register a type with a `Move` hook that increments a counter; manually call `info.Hooks.Move(dst, src)` on a pair of allocated pointers; verify the hook fired and bytes copied if it does so.
- Concurrent `Register` from multiple goroutines: **not required for v0** (registry is single-threaded; document this).

### Mechanical acceptance

- `go test ./... -race` passes.
- `go vet ./...` passes.
- `golangci-lint run` passes against the existing `.golangci.yml`.
- New package `internal/component` with the deliverables above.
- Every exported symbol has a godoc comment.
- Coverage on `internal/component` is ≥ 95% statement coverage.
- No imports of `unsafe` without justification — `unsafe.Sizeof`/`Alignof`/`Pointer` are fine; the data-layout layer needs them.

### Implementer pointers

- `internal/component` may import `github.com/snichols/flecs` (for `flecs.ID` if needed) but does **not** import `internal/storage/entityindex` — no entity allocator dependency in this phase.
- Do **not** define `Hooks.OnAdd`/`OnSet`/`OnRemove`. They go in 1.5 when `World` exists.
- Idempotent registration: a `sync.Map` or `sync.RWMutex` is overkill — plain `map[reflect.Type]*TypeInfo`. Single-threaded by contract.
- `Each` ordering is **insertion order**. Implement with a parallel `[]reflect.Type` slice; iterating Go maps directly is randomized.
- Read the C `ecs_type_info_t` and `ecs_type_hooks_t` definitions in full so you understand the surface we're deferring.
- `Move`'s signature is `func(dst, src unsafe.Pointer)`. The byte count is implicit (the column knows its `Size`). Phase 1.4 will call it correctly; we don't validate that here.

## Constraints

- C reference for `ecs_type_hooks_t`: `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` lines 954-1022 — read but do not paraphrase; cite the path.
- C reference for `ecs_type_info_t`: `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` lines 1023-1060 — read but do not paraphrase; cite the path.
- C reference for hook invocation context (informational only, no port required): `/work/agents/claude/projects/SanderMertens/flecs/src/component_actions.c`.
- @id.go — `flecs.ID` (`uint64`) entity/pair encoding. May be imported if needed, but no ID issuance happens in this phase.
- @internal/storage/entityindex/entityindex.go — stylistic consistency reference for an internal package (conventions for `Index`, `Record`, paged storage). **Not a dependency** of `internal/component`; do not import.
- Not a bug — feature work. Apply `snichols/queued` only; no `bug` label.
