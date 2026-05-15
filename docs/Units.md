# Units addon

The Units addon (Phase 16.30 / v0.85.0, extended in Phase 16.42 / v0.97.0) gives components typed unit semantics. A unit is a named entity carrying a `Symbol`, a `Name`, an optional `Base` (parent in the derivation chain), and a `Factor` (multiplier to convert to base). Components can be tagged with a unit so debug overlays, serializers, and conversion helpers can render values as `5.2 m/s` instead of raw numbers, and callers can convert between compatible units.

## Declaring an atomic unit

Built-in units are pre-registered at world creation. Use `RegisterUnit` to add custom atomic units:

```go
w.Write(func(fw *flecs.Writer) {
    dayID = flecs.RegisterUnit(fw, "Day", "d", w.Hour(), 24)
})
```

Arguments: `name`, `symbol`, `base` (parent unit entity; 0 for a root/SI unit), `factor` (multiplier: `Factor=24` means "1 Day = 24 Hours"). Panics if factor == 0.

## Compound units

Compound units express products, quotients, and powers of existing units. They support dimensional analysis and multi-dimensional conversion.

### Registration helpers

| Function | Effect |
|---|---|
| `RegisterCompoundUnit(fw, name, num []ID, denom []ID)` | Full product/quotient; powers by repeating IDs |
| `UnitProduct(fw, name, a, b)` | `a × b` |
| `UnitQuotient(fw, name, num, denom)` | `num / denom` |
| `UnitPower(fw, name, base, exponent)` | `base^exponent` (negative → reciprocal) |

```go
w.Write(func(fw *flecs.Writer) {
    mps := flecs.UnitQuotient(fw, "MPS", w.Meter(), w.Second())          // m/s
    ms2 := flecs.RegisterCompoundUnit(fw, "MS2",
        []flecs.ID{w.Meter()},
        []flecs.ID{w.Second(), w.Second()})                               // m/s²
    nm  := flecs.UnitProduct(fw, "NewtonMeter", w.NewtonCompound(), w.Meter()) // N·m
    invS := flecs.UnitPower(fw, "InvSecond", w.Second(), -1)             // 1/s
})
```

Panics if any factor ID is zero, a cycle is detected (depth limit 8), or depth overflows.

### Symbol composition

Auto-generated display strings use `·` (U+00B7) for products and `/` for quotients. Exponents 2 and 3 use Unicode superscripts; higher use `^n`. An explicit `Symbol` set at registration overrides the auto-generated string.

| Composition | Auto-symbol |
|---|---|
| `[Meter] / [Second]` | `m/s` |
| `[Meter] / [Second, Second]` | `m/s²` |
| `[KiloGram, Meter] / [Second, Second]` | `kg·m/s²` |
| `[] / [Second]` | `1/s` |

`w.UnitSymbol(unitID)` returns the symbol for both atomic and compound units (explicit override > auto-generated > atomic Symbol field).

### Querying compound structure

```go
if flecs.IsCompound(w, unitID) {
    nums, denoms := flecs.UnitFactors(w, unitID)
}
```

### Conversion algorithm

`Convert(w, value, fromUnit, toUnit)` supports both atomic and compound units.

For each unit, `siCanonical` builds a map from root-base entity ID to net integer exponent and a cumulative float64 scale factor:
- Atomic units: `{root: 1}`, factor = product of chain multipliers.
- Compound units: sum numerator canonical maps minus denominator canonical maps; factor = product-of-numerator-factors / product-of-denominator-factors.

Two units are compatible when their net-exponent maps are identical (same dimensions). The conversion factor is `fromFactor / toFactor`.

```go
// 100 m/s → km/h: fromFactor=1.0, toFactor=1000/3600 ≈ 0.2778 → result=360
got, ok := flecs.Convert(w, 100, w.MeterPerSecond(), w.KiloMeterPerHour())
// got == 360, ok == true

// Dimensional mismatch
_, ok = flecs.Convert(w, 1, w.Meter(), w.NewtonCompound())
// ok == false
```

## Tagging a component

```go
type Distance struct{ Meters float64 }

distID := flecs.RegisterComponent[Distance](w)

w.Write(func(fw *flecs.Writer) {
    fw.SetUnit(distID, w.Meter())
})

unitID, ok := w.UnitFor(distID)
// unitID == w.Meter(), ok == true
```

## Built-in atomic units (indices 48–62)

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
| 58 | `w.Newton()` | `N` | Force | opaque root (see NewtonCompound) |
| 59 | `w.Joule()` | `J` | Energy | opaque root (see JouleCompound) |
| 60 | `w.Hertz()` | `Hz` | Frequency | opaque root (see HertzCompound) |
| 61 | `w.Radian()` | `rad` | Angle | root |
| 62 | `w.Degree()` | `°` | Angle | π/180 × Radian |

## Built-in compound units (indices 63–72)

Shipped in Phase 16.42 / v0.97.0. User entities start at index 73.

| Index | Accessor | Symbol | Composition | Use |
|-------|----------|--------|-------------|-----|
| 63 | `w.MeterPerSecond()` | `m/s` | m/s | velocity |
| 64 | `w.KiloMeterPerHour()` | `km/h` | km/h | velocity |
| 65 | `w.MeterPerSecondSquared()` | `m/s²` | m/s² | acceleration |
| 66 | `w.NewtonCompound()` | `N` | kg·m/s² | force |
| 67 | `w.JouleCompound()` | `J` | kg·m²/s² | energy |
| 68 | `w.Watt()` | `W` | kg·m²/s³ | power |
| 69 | `w.Pascal()` | `Pa` | kg/(m·s²) | pressure |
| 70 | `w.HertzCompound()` | `Hz` | 1/s | frequency |
| 71 | `w.RadianPerSecond()` | `rad/s` | rad/s | angular velocity |
| 72 | `w.Inverse()` | `1/x` | opaque marker | reciprocal helper |

`NewtonCompound`, `JouleCompound`, `HertzCompound` are full compound units with dimensional semantics; `w.Newton()`, `w.Joule()`, `w.Hertz()` remain opaque roots for backwards compatibility.

Reciprocal units: use `UnitPower(base, -1)`:
```go
w.Write(func(fw *flecs.Writer) {
    rpm := flecs.RegisterCompoundUnit(fw, "RPM", nil, []flecs.ID{w.Minute()}) // 1/min
})
```

## Marshal round-trip

User-registered units (atomic and compound) and component→unit mappings survive `MarshalJSON` / `UnmarshalJSON`. Built-in units are not stored in JSON (re-created at fixed indices by `New()`). Compound factor lists are serialized via `num_factors` / `denom_factors` in the unit def entry.

```go
data, _ := json.Marshal(w)

w2 := flecs.New()
flecs.RegisterComponent[Distance](w2)
json.Unmarshal(data, w2)
```

## Snapshot round-trip

User-registered unit definitions (atomic and compound) also survive binary snapshots (`TakeSnapshot` / `RestoreSnapshot`). The snapshot serializes unit defs in section 10 of the binary layout.

## REST type-info integration

When a component has a unit tag, the `/type_info/{path}` endpoint emits the unit's display symbol (from `UnitSymbol`) in the `unit` field. For compound units, the auto-generated or explicit symbol is returned (e.g. `"kg·m/s²"`), not just the unit entity name.

## Divergence from upstream C

| Aspect | Go flecs | Upstream C |
|--------|----------|------------|
| Factor encoding | Single `float64` | `{factor int32, power int32}` |
| Symbol normalization | User-supplied / auto-generated | Auto-populated from prefix + base |
| Unit prefix entities | Not ported | `EcsUnitPrefix` tree |
| Compound units | ✅ Phase 16.42 | `over` numerator/denominator |
| Quantity grouping | Not ported | `EcsQuantity` parent scoping |

## Non-goals

- **Dimensional analysis at query time** — Go's type system enforces type-level distinction; no runtime dimensional checks on `Set[Position]`.
- **SI prefix as compound components** — kilo-, milli- remain atomic (`KiloMeter`, `MilliMeter`).
- **Implicit unit conversion in query results** — `Convert` is always explicit.
- **Plain-text parser** — `"m/s"` string → unit ID; registration is API-only.
