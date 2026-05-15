# Testing ECS Code with flectest

`github.com/snichols/flecs/flectest` is a `testing.TB`-aware helper package for
writing ECS tests. Import it only from `_test.go` files.

## Why a Subpackage?

Following the stdlib pattern (`net/http/httptest`, `testing/fstest`):

- Keeps `testing` and `flag` out of the root `flecs` package's production import
  graph.
- Users import it only when writing tests.

```go
import "github.com/snichols/flecs/flectest"
```

## Design: `*flecs.World`, not `*Reader`/`*Writer`

The root `flecs.scope` interface (`type scope interface { scopeWorld() *World }`)
is unexported. External packages cannot name it or implement it, so `flectest`
helpers cannot accept a scope parameter.

**Every helper accepts `*flecs.World` and opens a `Read` scope internally**
via `w.Read(func(fr *flecs.Reader){ … })`. This is the most ergonomic
call-site signature for test code — no scope plumbing required:

```go
flectest.AssertAlive(t, w, hero)      // just pass the world and the entity
flectest.AssertComponentValue[HP](t, w, hero, HP{100})
```

## Lifecycle Helpers

### `NewWorld`

```go
func NewWorld(tb testing.TB) *flecs.World
```

Creates a fresh `*flecs.World` bound to `tb`. A `tb.Cleanup` closure is
registered as a forward-compatibility placeholder — `flecs.World` has no
`Close`/`Free`/`Destroy` method; the GC reclaims it. If a teardown method is
added in a future version, only `flectest` needs to change.

### `NewWorldWith`

```go
func NewWorldWith(tb testing.TB, setup func(fw *flecs.Writer)) *flecs.World
```

Creates a world and runs `setup` inside `w.Write(setup)` before returning.
Convenient for table-driven tests that share initial state:

```go
w := flectest.NewWorldWith(t, func(fw *flecs.Writer) {
    hero = fw.NewEntity()
    flecs.Set(fw, hero, HP{100})
})
```

## Assertion Helpers

Every assertion calls `tb.Helper()` so failure line numbers point at the caller,
and uses `tb.Fatalf` with explicit `got`/`want` in the message.

### Entity Lifecycle

```go
func AssertAlive(tb testing.TB, w *flecs.World, e flecs.ID)
func AssertNotAlive(tb testing.TB, w *flecs.World, e flecs.ID)
```

Internally calls `fr.IsAlive(e)`.

### Component Presence and Values

```go
func AssertHasComponent[T any](tb testing.TB, w *flecs.World, e flecs.ID)
func AssertNoComponent[T any](tb testing.TB, w *flecs.World, e flecs.ID)
func AssertComponentValue[T comparable](tb testing.TB, w *flecs.World, e flecs.ID, want T)
func AssertComponentValueFunc[T any](tb testing.TB, w *flecs.World, e flecs.ID, check func(T) bool, desc string)
```

- `AssertHasComponent`/`AssertNoComponent` call `flecs.Has[T](fr, e)` (which
  auto-registers `T`).
- `AssertComponentValue` calls `flecs.Get[T](fr, e)` and compares with `==`.
  The failure message includes both `got` and `want` values (`%+v`).
- `AssertComponentValueFunc` calls the predicate and includes `desc` and the
  actual value in the failure message.

### Entity Counts

```go
func AssertEntityCount(tb testing.TB, w *flecs.World, want int)
func AssertQueryCount(tb testing.TB, w *flecs.World, q *flecs.Query, want int)
```

- `AssertEntityCount` calls `fr.Count()` (includes built-in entities).
- `AssertQueryCount` drives `q.Iter()` and sums `it.Count()` per `it.Next()`.

### Hierarchy

```go
func AssertParent(tb testing.TB, w *flecs.World, child, wantParent flecs.ID)
func AssertNoParent(tb testing.TB, w *flecs.World, child flecs.ID)
func AssertChildren(tb testing.TB, w *flecs.World, parent flecs.ID, wantChildren ...flecs.ID)
```

- `AssertParent`/`AssertNoParent` call `fr.ParentOf(child)`.
- `AssertChildren` collects children via `fr.EachChild` and performs
  order-independent set equality. The failure message lists missing and extra
  IDs separately.

### Pairs and Tags

```go
func AssertHasPair(tb testing.TB, w *flecs.World, e, rel, target flecs.ID)
func AssertTag(tb testing.TB, w *flecs.World, e, tag flecs.ID)
```

Both call `flecs.HasID(fr, e, id)`.

### Names

```go
func AssertName(tb testing.TB, w *flecs.World, e flecs.ID, wantName string)
```

Calls `fr.GetName(e)`. The failure message shows both the got and want name.

## Golden Snapshot Helpers

### `AssertSnapshotGolden`

```go
func AssertSnapshotGolden(tb testing.TB, w *flecs.World, goldenPath string)
```

Compares `w.MarshalJSON()` against the JSON file at `goldenPath`. The JSON is
normalized (compact re-marshal) before writing or comparing, so whitespace
differences in manually edited golden files are ignored.

**Updating golden files:** pass `-update` on the test command line:

```
go test ./... -update
```

When `-update` is set, `AssertSnapshotGolden` writes the current world JSON to
`goldenPath` (creating parent directories if needed) instead of comparing.

**Workflow:**

1. Write your test with `AssertSnapshotGolden(t, w, "testdata/world.golden.json")`.
2. Run `go test ./... -update` once to create the golden file.
3. Commit the golden file alongside your test.
4. Future test runs compare against the committed file; run `-update` again after
   intentional world changes.

### `RequireRoundTrip`

```go
func RequireRoundTrip(tb testing.TB, w *flecs.World)
```

Verifies that `w` survives a JSON round-trip:

1. `b1 := w.MarshalJSON()`
2. Create `w2 := flecs.New()` and copy component type registrations from `w`.
3. `w2.UnmarshalJSON(b1)`
4. `b2 := w2.MarshalJSON()`
5. Assert `normalize(b1) == normalize(b2)`.

**Why JSON, not binary snapshots?**

Binary snapshots (`TakeSnapshot`/`RestoreSnapshot`) are bound to their origin
world via an `unsafe.Pointer` identity token. `RestoreSnapshot` panics when the
world argument differs from the snapshot's origin world — making cross-world
round-trip testing impossible by design. The JSON path has no such restriction:
`UnmarshalJSON` into a fresh `flecs.New()` works correctly.

**Component pre-registration:** `UnmarshalJSON` requires component types to be
registered in the destination world. `RequireRoundTrip` automatically copies
all typed component registrations from `w` to `w2` using
`flecs.RegisterComponentByType`. Components registered via
`RegisterDynamicComponent` (dynamic, no Go type) are not copied; if your world
uses only dynamic components with data, `RequireRoundTrip` will fail. Use
`AssertSnapshotGolden` in that case.

## Helper Builders

### `MustEntity`

```go
func MustEntity(tb testing.TB, w *flecs.World, name string, comps ...any) flecs.ID
```

Creates a new entity with the given name and components. Component types are
auto-registered via `flecs.RegisterComponentByType` — callers do NOT need to
call `flecs.RegisterComponent[T](w)` before using `MustEntity`:

```go
// Position is NOT pre-registered — MustEntity auto-registers it.
hero := flectest.MustEntity(t, w, "hero", Position{X: 1, Y: 2}, HP{100})
```

### `MustChild`

```go
func MustChild(tb testing.TB, w *flecs.World, parent flecs.ID, name string, comps ...any) flecs.ID
```

Like `MustEntity`, plus adds `(ChildOf, parent)` via `flecs.AddID`.

## Mock-TB Pattern for Testing Test Helpers

To test flectest helpers themselves (verifying that `Fatalf` is called on
failure), use the mock-TB pattern:

```go
// recordingTB is a mock testing.TB.
// Embed testing.TB as a nil interface field to satisfy the unexported
// private() method at compile time. Override only the methods actually called.
type recordingTB struct {
    testing.TB // nil — satisfies unexported private()

    mu      sync.Mutex
    fatalf  []string
    helpers int
}

func (r *recordingTB) Helper() {
    r.mu.Lock()
    r.helpers++
    r.mu.Unlock()
}

func (r *recordingTB) Fatalf(format string, args ...any) {
    r.mu.Lock()
    r.fatalf = append(r.fatalf, fmt.Sprintf(format, args...))
    r.mu.Unlock()
}

func (r *recordingTB) Cleanup(fn func()) {} // no-op or run fn immediately
```

**Key insight:** the embedded nil `testing.TB` satisfies the `private()` method
(which Go's `testing` package defines on the interface to prevent external
implementations). The only methods actually called by flectest helpers are
`Helper()`, `Fatalf()`, and `Cleanup()`, so the nil embed never panics.

Usage in tests:

```go
mock := &recordingTB{}
flectest.AssertAlive(mock, w, deadEntity)
if len(mock.fatalf) == 0 {
    t.Error("expected AssertAlive to fail")
}
if mock.helpers == 0 {
    t.Error("AssertAlive did not call tb.Helper()")
}
```

This pattern is safe as long as no unoverridden method is ever called. If a
helper adds a new `tb.X()` call in the future, the nil embed will panic — a
compile-time-obvious gap in the mock.
