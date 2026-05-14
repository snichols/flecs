# Units addon

The Units addon (Phase 16.30, v0.85.0) gives components typed unit semantics. A unit is a named entity carrying a `Symbol`, a `Name`, an optional `Base` (the parent unit in the derivation chain), and a `Factor` (the multiplier to convert one of this unit to one of its base). Components can be tagged with a unit so debug overlays, serializers, and conversion helpers can render values as `5.2 m/s` instead of raw numbers, and callers can convert between compatible units.

## Declaring a unit

Built-in units are pre-registered at world creation. Call `w.Meter()`, `w.Second()`, etc. to obtain their IDs. Use `RegisterUnit` to add custom units:

```go
w.Write(func(fw *flecs.Writer) {
    // Register a Day unit derived from the built-in Hour.
    dayID = flecs.RegisterUnit(fw, "Day", "d", w.Hour(), 24)
})
```

Arguments: `name` (entity name + Unit.Name), `symbol` (short display string), `base` (parent unit entity; 0 for a root/SI unit), `factor` (multiplier to convert one of this unit into one of the base: `Factor=24` means "1 Day = 24 Hours"). Panics if factor == 0.

## Tagging a component

```go
type Distance struct{ Meters float64 }

distID := flecs.RegisterComponent[Distance](w)

w.Write(func(fw *flecs.Writer) {
    fw.SetUnit(distID, w.Meter())
})
```

Retrieve the unit later:

```go
unitID, ok := w.UnitFor(distID)
// unitID == w.Meter(), ok == true
```

## Converting between units

`Convert(w, value, fromUnit, toUnit)` walks each unit's `Base` chain to find a common root. Returns `ok=false` for incompatible units (different root bases) or unknown unit IDs. Same-unit conversion short-circuits with no lookup.

```go
// 1 km = 1000 m
v, ok := flecs.Convert(w, 1, w.KiloMeter(), w.Meter())
// v == 1000, ok == true

// 1 hour = 3600 seconds
v, ok = flecs.Convert(w, 1, w.Hour(), w.Second())
// v == 3600, ok == true

// Multi-hop: Day → Hour → Second
var dayID flecs.ID
w.Write(func(fw *flecs.Writer) {
    dayID = flecs.RegisterUnit(fw, "Day", "d", w.Hour(), 24)
})
v, ok = flecs.Convert(w, 1, dayID, w.Second())
// v == 86400, ok == true

// Incompatible units
_, ok = flecs.Convert(w, 1, w.Meter(), w.Second())
// ok == false
```

## Built-in unit set

All 15 built-in units are allocated at world creation at fixed entity indices (48–62) and are never serialized (they are re-created by `New()`).

| Index | Accessor | Symbol | Quantity | Factor |
|-------|----------|--------|----------|--------|
| 48 | `w.Meter()` | `m` | Length | root |
| 49 | `w.KiloMeter()` | `km` | Length | 1000 × Meter |
| 50 | `w.MilliMeter()` | `mm` | Length | 0.001 × Meter |
| 51 | `w.Second()` | `s` | Duration | root |
| 52 | `w.MilliSecond()` | `ms` | Duration | 0.001 × Second |
| 53 | `w.Minute()` | `min` | Duration | 60 × Second |
| 54 | `w.Hour()` | `h` | Duration | 3600 × Second |
| 55 | `w.Gram()` | `g` | Mass | root |
| 56 | `w.KiloGram()` | `kg` | Mass | 1000 × Gram |
| 57 | `w.MegaGram()` | `Mg` | Mass | 1 000 000 × Gram |
| 58 | `w.Newton()` | `N` | Force | root (opaque in v1) |
| 59 | `w.Joule()` | `J` | Energy | root (opaque in v1) |
| 60 | `w.Hertz()` | `Hz` | Frequency | root (opaque in v1) |
| 61 | `w.Radian()` | `rad` | Angle | root |
| 62 | `w.Degree()` | `°` | Angle | π/180 × Radian |

Newton, Joule, and Hertz are opaque root units in v1; compound-unit semantics (`kg·m/s²`) are deferred to Phase 16.30.1.

## Marshal round-trip

User-registered units and component→unit mappings survive `MarshalJSON` / `UnmarshalJSON`. Built-in units are not stored in the JSON (they are re-created at fixed indices by `New()`).

```go
// Serialize
data, _ := json.Marshal(w)

// Restore in a new world
w2 := flecs.New()
flecs.RegisterComponent[Distance](w2) // must pre-register all component types
json.Unmarshal(data, w2)

unitID, ok := w2.UnitFor(distID2)
// unitID == w2.Meter(), ok == true
```

## Divergence from upstream C

| Aspect | Go flecs | Upstream C |
|--------|----------|------------|
| Factor encoding | Single `float64` | `{factor int32, power int32}` |
| Symbol normalization | User-supplied verbatim | Auto-populated from prefix + base |
| Unit prefix entities | Not ported (Phase 16.30.1) | `EcsUnitPrefix` tree |
| Compound units (`m/s`) | Not ported (Phase 16.30.1) | `over` numerator/denominator |
| Quantity grouping | Not ported | `EcsQuantity` parent scoping |

## Non-goals (v1)

- **Compound units** — `MetersPerSecond` (`m/s`), `Joule` as `kg·m²/s²`, etc. are deferred to Phase 16.30.1. Callers wanting `m/s` register an opaque unit with `Symbol: "m/s"`, `Base: 0`, `Factor: 1`.
- **Automatic conversion in formulas** — callers invoke `Convert` explicitly.
- **SI vs. US-Customary enforcement** — both coexist freely; no conflict detection.
