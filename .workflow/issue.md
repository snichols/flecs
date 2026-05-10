## Goal

Add ergonomic typed-iteration helpers `Each1[A]`, `Each2[A,B]`, `Each3[A,B,C]`, `Each4[A,B,C,D]` as a thin layer over the Phase 3.1 query API. After this lands, the common case of "iterate entities matching these components and mutate them" collapses from ~10 lines of `Iter/Field/Entities` boilerplate to a single closure call.

### Before (Phase 3.1, current master)

```go
posID := flecs.RegisterComponent[Position](w)
velID := flecs.RegisterComponent[Velocity](w)
q := flecs.NewQuery(w, posID, velID)
it := q.Iter()
for it.Next() {
    positions := flecs.Field[Position](it, posID)
    velocities := flecs.Field[Velocity](it, velID)
    entities := it.Entities()
    for i := range entities {
        positions[i].X += velocities[i].X
    }
}
```

### After (this issue)

```go
flecs.Each2[Position, Velocity](w, func(e flecs.ID, p *Position, v *Velocity) {
    p.X += v.X
})
```

### What the helper does

1. Looks up the registered component ID for each type parameter via `internal/component.LookupByType[T]` (or equivalent path through `RegisterComponent[T]`, which is idempotent).
2. **Auto-registers types that aren't yet registered** — matches the `Set[T]`/`Has[T]` convention. Explicitly diverges from `Get[T]`, which does NOT auto-register. Godoc on each `Each<N>` must call out this asymmetry.
3. Constructs a `Query` with those term ids via `NewQuery(w, ids...)`. No query caching — Phase 6's job.
4. Iterates and invokes `fn` once per matching entity, passing the entity ID and **pointers into the live column slot** for each component.
5. Returns when iteration completes.

### Pointer semantics (the load-bearing contract)

The pointers passed to `fn` ARE the live column slots — derived from `Field[T](it, id)` which returns `[]T` backed by `table.Column`. Taking `&slice[i]` yields a stable pointer for the duration of the `fn` call only. Mutations through the pointer write back to storage. This must be:

- Documented on each `Each<N>` godoc.
- Tested explicitly (`TestEachMutationIsLive`) in addition to being incidentally exercised by the basic tests.

### Deliverables

1. **New file `each.go`** in the top-level `flecs` package (implementer may fold into `query.go` instead — judgment call). Pure additions; no changes to existing Phase 3.1 surface.

2. **Four generic free functions:**
   - `func Each1[A any](w *World, fn func(e ID, a *A))`
   - `func Each2[A, B any](w *World, fn func(e ID, a *A, b *B))`
   - `func Each3[A, B, C any](w *World, fn func(e ID, a *A, b *B, c *C))`
   - `func Each4[A, B, C, D any](w *World, fn func(e ID, a *A, b *B, c *C, d *D))`

3. **Implementation pattern:**
   - Resolve component IDs once per call. Store in a fixed-size local `[N]ID` to avoid heap-allocating the term list.
   - `NewQuery(w, ids[:]...)` — the slice for the variadic does require one heap alloc; accept that.
   - Iterate via `Query.Iter()`; for each table fetch each column via `Field[T_i](it, id_i)`; range over entities calling `fn(entities[i], &cols0[i], &cols1[i], ...)`.

4. **Tests in `each_test.go`:**
   - **Each1 basic** — register Position, 3 entities, visit all 3, totals match, mutation writes back.
   - **Each2 basic** — Position + Velocity, apply `p.X += v.X`, verify after.
   - **Each3 basic** — three components.
   - **Each4 basic** — four components.
   - **Mixed archetypes** — entities in `[P,V]` and `[P,V,Tag]` archetypes; `Each2[Position, Velocity]` visits both. The extra Tag doesn't disqualify.
   - **No-match** — `Each2` on a world with no matching entity; `fn` never called.
   - **Auto-registration** — `Each1[NewType]` on a never-registered type; world component count increments; helper runs and visits zero entities.
   - **Tag component** — `Each1[Tag]` where `Tag = struct{}`; pointer-to-zero-size-struct doesn't panic; count is correct.
   - **GC pointer survives** — component with string field on 10 entities; `runtime.GC()` twice BEFORE Each1; strings still intact when read inside the closure.
   - **`TestEachMutationIsLive`** — explicit test for the live-pointer contract.
   - **Order semantics** — document but do NOT test specific ordering. Iteration is "within a table, dense order; across tables, undefined."
   - **Concurrent modification** — document on godoc as undefined behavior; no test required. Godoc warning: "Behavior is undefined if fn calls Set/Remove/Delete on entities being iterated."

5. **Mechanical acceptance:**
   - `go test ./... -race -count=2` passes.
   - `go vet ./...` clean.
   - `golangci-lint run` clean.
   - Coverage on `flecs` package >= 90% (no regression from current 97.4%).
   - All exported symbols have godoc.

### Non-goals

- NO `Each5`/`Each6`/etc. Four is enough; users with more components fall back to `Iter`/`Field`.
- NO `Each2OnQuery(q, fn)` variant taking an existing `*Query`.
- NO `Each2With(w, idA, idB, fn)` variant taking runtime IDs.
- NO filtering of "extra" components in matched archetypes — `Each2[P,V]` correctly visits `[P,V,Marker]` entities; the helper ignores the extra column.
- NO change to `Query`, `QueryIter`, `Field[T]`, or any `internal/storage` package. Helpers are purely additive in the root `flecs` package.
- NO caching of `Query` across `Each` calls (Phase 6).
- NO concurrent iteration support.
- NO observers / hooks (Phase 5).

## Constraints

- @query.go — Phase 3.1 query API the helpers wrap. `NewQuery(w, ids...) *Query`, `(*Query).Iter() *QueryIter`, `(*QueryIter).Next/Table/Count/Entities`, and `Field[T any](it *QueryIter, id ID) []T` are the only entry points the helper should use. Do not modify.
- @world.go — `RegisterComponent[T](w) ID` is idempotent and is the auto-registration path. No new `World` API is needed; verify the existing path covers the case where T was never registered.
- @id.go — `ID` is the entity/component identifier type. Since `each.go` lives in package `flecs`, just `ID` (not `flecs.ID`).
- @internal/component/registry.go — type-to-ID lookup lives here. `RegisterComponent[T]` routes through this; if a direct lookup-without-register helper is more efficient, the implementer may use it, but DO NOT add new exported API to the internal package solely for `Each`.
- @internal/storage/table/table.go — `ColumnReflectSlice(id) reflect.Value` is what `Field[T]` uses under the hood. Helpers go through `Field[T]`, not directly to this. Do not modify.
- Auto-registration policy: matches `Set[T]`/`Has[T]` (auto-registers), diverges from `Get[T]` (does not auto-register). Document on each `Each<N>` godoc.
- Pointer lifetime: pointers passed to `fn` are valid for the duration of that `fn` call only. Mutations write back to the live column. Document and explicitly test.
- Performance: term-id array must be a fixed-size local `[N]ID`. The variadic call to `NewQuery` is allowed to allocate; do not micro-optimize past that.
- Dependencies: no third-party imports. Stdlib only (and `reflect` only transitively via existing `Field[T]`).
