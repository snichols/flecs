## Goal

Eliminate the `fw.AsReader()` downgrade required to call read free-functions from inside a `Write` scope. Target **v0.40.0**.

### The smell

The Reader/Writer scoped-capability API (Phase 12.0, v0.15.0) has a longstanding ergonomic wart. From inside a `Write` scope, reading components currently requires an explicit downgrade:

```go
w.Write(func(fw *flecs.Writer) {
    flecs.Each2[Position, Velocity](fw.AsReader(), func(id flecs.ID, p *Position, v *Velocity) {
        p.X += v.DX
        p.Y += v.DY
    })
})
```

The `fw.AsReader()` call exists purely to paper over a type-system limitation: Go generics don't auto-promote `*Writer` to `*Reader` even though `Writer` embeds `Reader` by value (see @scope.go lines 36–42). The natural mental model — "Writer ⊇ Reader, so reads from a Writer just work" — is broken at the free-function boundary.

The existence of `AsReader` is itself the tell: it was added (commit `0459926` in v0.15.0) explicitly to provide the downgrade, with a docstring example showing exactly the pattern this phase fixes. Every `docs/*_examples_test.go` written during the Phases 14.0–14.12 docs port uses `fw.AsReader()` or works around it with a separate `w.Read(...)` block.

### Design: unexported `scope` interface

Define an internal interface that both `*Reader` and `*Writer` satisfy:

```go
type scope interface {
    scopeWorld() *World
}

func (r *Reader) scopeWorld() *World { return r.world }
func (w *Writer) scopeWorld() *World { return w.world }
```

Change every read free-function from `r *Reader` to `s scope`. Both `*Reader` and `*Writer` satisfy without ceremony. Function bodies change `r.world` → `s.scopeWorld()`.

**Why interface, not duplicate methods**: duplicating `Each1/Each2/etc.` as methods on Writer doubles the API surface and creates two code paths for the same operation. The interface approach is one signature change per function, no behavior duplication.

**Why unexported**: users don't write `var x flecs.Scope`; they pass `*Reader` or `*Writer`. Unexported interface = no documentation burden, no future-compatibility constraint.

### Affected functions

All in @scope.go and @query.go, all currently take `*Reader`:

- @scope.go: `Get[T]`, `GetRef[T]`, `Has[T]`, `Owns[T]`, `GetPair[T]`, `GetPairRef[T]`, `HasID`, `OwnsID`, `Each1[A]`, `Each2[A,B]`, `Each3[A,B,C]`, `Each4[A,B,C,D]`, `GetUp[T]`, `HasUp`, `TargetUp`, `PrefabOf`
- @query.go: `Field[T]`, `FieldMaybe[T]` (on QueryIter), `MatchedTarget`, `MatchedID`, `IsFieldSelf`, `FieldShared[T]`
- @wildcard.go: `FieldByMatch[T]` (added in Phase 15.6)

Plus anything missed — grep `r \*Reader` in non-test `.go` files and migrate every match.

### Deliverables

1. **Add `scope` interface** in @scope.go. Define the interface plus the two satisfying methods on `Reader` and `Writer`.
2. **Migrate all read free-functions** to take `scope` instead of `*Reader`. Update bodies: `r.world` → `s.scopeWorld()`.
3. **Decide `AsReader` fate**: per the 12.0.1 aggressive-cleanup precedent (pre-1.0, no external users), delete outright. Note in CHANGELOG.
4. **Tests**: existing tests should pass unchanged because `*Reader` still satisfies the new interface. Add `TestScopePromotion_*` (new `scope_promotion_test.go` or append to existing scope_test.go) verifying:
   - `Each2` inside a `Write` scope works without `AsReader`
   - `Get` inside a `Write` scope works without `AsReader`
   - Mixed `Each`/`Set` inside one `Write` scope works
   - `*Reader` still works outside `Write` scope
   - `QueryIter` methods accept either scope (`it.Field`, etc.)
5. **Update every `docs/*_examples_test.go`** to use `fw` directly. Run `grep -rn AsReader docs/` to find sites.
6. **Update every `docs/*.md` code block** showing the `fw.AsReader()` pattern.
7. **CHANGELOG entry** documenting the breaking change. Pre-1.0; no migration guide complexity. Note that `*Writer` is now usable directly anywhere a `*Reader` was previously required.
8. **doc.go and README.md** updates if any example uses the old pattern.

### Non-goals

- NO change to `*Reader` / `*Writer` struct shape.
- NO change to scope semantics (same Read/Write lock acquisition, same `ExclusiveAccess` integration).
- NO new public types or methods beyond the interface.
- NO change to `*QueryIter` — it's its own kind of scope; out of scope here.

### Mechanical acceptance

- `grep -rn 'r \*Reader' --include='*.go'` returns no matches in non-test code beyond the interface methods themselves (verify each remaining match is legitimate).
- `grep -rn AsReader docs/` returns zero results.
- `grep -rn AsReader --include='*.md'` returns zero results in `docs/`.
- `go vet ./...`, `golangci-lint run ./...` clean.
- `go test ./... -race -count=3` passes.
- Coverage on main package ≥ 95%.
- The original user-flagged pattern `flecs.Each2[Position, Velocity](fw, func(...) {...})` compiles inside a `w.Write(func(fw *Writer) {...})`.

### Phase ordering

Previously sketched Acyclic as 15.8. Bump Acyclic to 15.9; this Reader/Writer fix is higher user-facing priority.

## Constraints

- @scope.go — defines `Reader`, `Writer`, `AsReader`, and most affected free functions. `Writer` embeds `Reader` by value; the `world *World` field lives on `Reader`. The interface methods go here.
- @query.go — defines `Field`, `FieldMaybe`, `MatchedTarget`, `MatchedID`, `IsFieldSelf`, `FieldShared` — all currently take `*Reader`.
- @wildcard.go — defines `FieldByMatch` (Phase 15.6), currently takes `*Reader`.
- @doc.go — package-level documentation; check for any example using the old pattern.
- @README.md — surface-level examples; check for old pattern.
- @CHANGELOG.md — append v0.40.0 entry documenting the breaking change and `AsReader` removal.
- @ROADMAP.md — record Phase 15.8 as shipped; bump prior Acyclic plan to 15.9.
- CONTRIBUTING.md convention: docs and code land together. Every `docs/*.md` and `docs/*_examples_test.go` site must migrate in the same change.
- 12.0.1 precedent (aggressive pre-1.0 cleanup of `W()`/`R()` shorthand): follow the same approach for `AsReader` — delete outright, note in CHANGELOG.
- Interface naming: unexported `scope`, unexported method `scopeWorld()`. Not in public docs.
