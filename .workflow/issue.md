## Goal

Extend the Units addon (Phase 16.30 / v0.85.0) with **compound units** — products, quotients, and integer powers of existing atomic units. Today `@units.go` (206 LOC) carries only single-base linear chains (`KiloMeter → Meter`, `Hour → Second`); it cannot express `m/s`, `kg·m/s²`, `m²`, or `J = kg·m²/s²`. The existing built-ins `Newton` / `Joule` / `Hertz` are placeholder *opaque* root units with `Factor=1` and no relation to their kg·m/s² / kg·m²/s² / 1/s composition. This phase makes them real.

Target version: **v0.97.0**. Phase number: **16.42**.

### Surface to add

Extend `flecs.Unit` (in `@units.go`, not `UnitDef` — the current type is `Unit{Symbol, Name, Base, Factor}`) with two optional fields:

- `Over ID` — denominator unit (zero for non-quotient units)
- `Power int8` — integer exponent for repeated multiplication; default `1`; allow negative for `1/x`; range `[-8, 8]`

Helpers (new):

- `RegisterCompoundUnit(fw *Writer, name string, num []ID, denom []ID) ID` — full product/quotient form; powers expressed by repeating an ID
- `UnitProduct(fw *Writer, name string, a, b ID) ID` — `a * b`
- `UnitQuotient(fw *Writer, name string, num, denom ID) ID` — `num / denom`
- `UnitPower(fw *Writer, name string, base ID, exponent int8) ID` — `base^exponent`
- `(s scope) IsCompound(unitID ID) bool` — predicate (free function `IsCompound(s scope, unitID ID) bool`)
- `UnitFactors(s scope, unitID ID) (numerators []ID, denominators []ID)` — decomposition for inspection

Existing `Convert(w *World, value float64, fromUnit, toUnit ID) (float64, bool)` extends to handle compound:

- Each side decomposed to SI canonical form via `rootFactor`-style recursion over `Over` / `Power`
- Require **structurally compatible** factor decomposition (same dimensions, same multiset of root bases on each side after exponent normalization). Mismatch → `(0, false)`; **no panic**.
- Cycle detection during registration with max recursion depth 8 — return error on excess; cycle detected when registering a compound whose factors are themselves compound and the chain re-enters a unit already on the stack.

Symbol composition:

- Auto-generate display string: `(Meter, Second) → \"m/s\"`; `(KiloGram, Meter, Second, Second) → \"kg·m/s²\"` (middle dot `·` U+00B7; superscript powers for exponents 2 and 3, plain `^n` for higher)
- Allow override via explicit `Symbol` field on registration
- `(*World).UnitSymbol(unitID ID) string` accessor — does not exist today (atomic units expose `Symbol` only via `r.Unit(unitID).Symbol`); add it and extend to compounds

### Built-in compound units

Bootstrap these in `World.New()` (extending atomic indices 48–62 from Phase 16.30). Assign indices 63–72:

| Index | Name | Composition | Symbol | Use |
|---|---|---|---|---|
| 63 | MeterPerSecond | m / s | m/s | velocity |
| 64 | KiloMeterPerHour | km / h | km/h | velocity |
| 65 | MeterPerSecondSquared | m / s² | m/s² | acceleration |
| 66 | NewtonCompound | kg·m / s² | N | force (see Hertz/Newton/Joule resolution below) |
| 67 | JouleCompound | kg·m² / s² | J | energy |
| 68 | Watt | kg·m² / s³ | W | power |
| 69 | Pascal | kg / (m·s²) | Pa | pressure |
| 70 | HertzCompound | 1 / s | Hz | frequency |
| 71 | RadianPerSecond | rad / s | rad/s | angular velocity |
| 72 | Inverse | 1 / x | (pattern) | reciprocal helper (`UnitPower(base, -1)`) |

User-entity-start index shifts from **63 → 73**. The `Inverse` row in the table is the documented reciprocal pattern — implement as `UnitPower` helper, not a built-in entity, unless we decide otherwise during iterate.

### Resolution: Hertz/Newton/Joule — atomic or compound?

Today these three built-ins exist as opaque atomic roots at indices 58/59/60 with `Factor=1`. Three choices, pick one in the issue body:

**Option A — Keep atomic, alias compound separately.** Add `HertzCompound`/`NewtonCompound`/`JouleCompound` at the new indices as proper compounds. The atomic `Hertz`/`Newton`/`Joule` remain reachable via `w.Hertz()` etc. and act as opaque roots that `Convert` cannot cross-relate to `1/s` / `kg·m/s²` / `kg·m²/s²`. Cleanest for back-compat (all Phase 16.30 tests untouched), worst for ergonomics — two `Hertz`es is confusing.

**Option B — Redefine in-place.** Reassign the existing `hertzID`/`newtonID`/`jouleID` slots in `bootstrapBuiltinUnits` from opaque atoms to compound definitions referencing `Second` / `KiloGram·Meter/Second²` / `KiloGram·Meter²/Second²`. Phase 16.30 tests that check `Symbol == \"N\"` etc. still pass (compound auto-symbol matches). Phase 16.30 tests that called `Convert(1, Newton, KiloGram)` and expected `(0, false)` continue to fail-closed because dimensions mismatch (kg vs kg·m/s²). One risk: any test that relied on `Newton`'s `Factor=1` exact representation may break; we will verify each Phase 16.30 test passes unchanged before declaring this option safe.

**Option C — Deprecate atomic, point accessors at new compound IDs.** `w.Hertz()` returns the compound ID at the new index; old indices 58–60 become unused gaps. Worst for snapshot/marshal compatibility — any persisted snapshot with the old IDs breaks.

**Recommendation (open for iterate): Option B**, if Phase 16.30 tests pass unchanged. Otherwise fall back to Option A and document the dual `Hertz`/`HertzCompound` pair.

### File layout

- Extend `@units.go` — add `Over` / `Power` fields, registration helpers, factor decomposition, extended `Convert`, `UnitSymbol`
- Extend `@units_test.go` — extend test coverage
- If `@units.go` grows past ~500 LOC, split compound logic into a new `@units_compound.go`
- `@world.go` lines 99–113 — add 10 new built-in unit ID fields (`meterPerSecondID`, etc.); extend `bootstrapBuiltinUnits`; extend `builtinUnitByIndex` and `isBuiltinUnit` to cover the new range; add 10 accessor methods

### Marshal / snapshot integration

- `@marshal.go` lines 45–67, 217, 549–591 — extend `jsonUnitDef` with `over_serial` / `over_builtin_idx` / `power` fields; preserve `Symbol` when explicitly set; round-trip compound user units. Verify built-in compound units serialize by index only (no per-world serial).
- `@snapshot.go` — **currently does not serialize unit definitions at all** (`grep` confirms no `unit` references). Decide during iterate: either snapshot user-registered units alongside the entity index, or document the same gap and require user re-registration after `RestoreSnapshot`. JSON path already does this work; snapshot should match.

### REST integration

- `@rest_type_walk.go` lines 26, 37 — `Unit string` field already emits the `Symbol`. For compound-typed component fields it should emit the **auto-generated compound symbol** (e.g. `\"kg·m/s²\"`), not the unit's name. Verify `walkTypeForJSON` calls `w.UnitFor(...)` → `UnitSymbol(...)` instead of `r.Unit(id).Symbol`, since the latter returns empty when an override is not set.

### Conversion algorithm sketch

For compound `c = (Π numᵢ^pᵢ) / (Π denomⱼ^qⱼ)`:

1. Recursively expand each factor to its atomic root via existing `rootFactor` (or a new `siCanonical` that handles compound atomic-or-recursive). Bounded depth 8.
2. Build a `map[ID]int` of net exponents per root base across both sides.
3. For two units to be Convert-compatible: their net-exponent maps must be equal as multisets.
4. Result factor = (Π fromFactorᵢ^pᵢ) / (Π toFactorⱼ^qⱼ), then `Convert` returns `value * (fromCanonical / toCanonical)`.

Worked: `Convert(100, MeterPerSecond, KiloMeterPerHour)` → fromCanonical = (1 m/s) → 1.0; toCanonical = (1000 m / 3600 s) → 0.2777…; result = 100 / 0.2777… = **360 km/h**. Matches the required test.

### Required tests

All in `@units_test.go`:

- `TestCompound_RegisterProduct` — `m·s`; factors decompose
- `TestCompound_RegisterQuotient` — `m/s`; num/denom split
- `TestCompound_RegisterPower` — `m²`; exponent 2
- `TestCompound_NegativePower` — `s⁻¹`; reciprocal form
- `TestCompound_NestedCompound` — `(m/s) / s` flattens or composes to m/s²
- `TestCompound_CycleDetection` — A in terms of B, B in terms of A → error
- `TestCompound_DepthLimit` — chain 9 levels → error at depth 8
- `TestCompound_Convert_VelocityUnits` — `Convert(100, MeterPerSecond, KiloMeterPerHour) == 360`
- `TestCompound_Convert_ForceUnits` — Newton ↔ kg·m/s² identity (factor 1)
- `TestCompound_Convert_DimensionalMismatch` — `Convert(1, Meter, Newton) == (0, false)`
- `TestCompound_Convert_EnergyChain` — Joule ↔ Newton·Meter identity
- `TestCompound_Symbol_Auto` — auto symbol matches expected
- `TestCompound_Symbol_Override` — explicit override wins
- `TestCompound_BuiltIns_AllRegistered` — all 10 new built-ins exist with expected IDs and symbols
- `TestCompound_JSON_RoundTrip` — compound units survive `MarshalJSON` / `UnmarshalJSON` with full factor preservation
- `TestCompound_Snapshot_RoundTrip` — compound units survive `TakeSnapshot` / `RestoreSnapshot`
- `TestCompound_HertzAsCompound` — Hertz = 1/Second; 60 Hz → RPM via compound
- `TestCompound_SetUnit_ForceOnPositionField` — set Newton on a force-typed component field; REST type-info reflects compound
- `TestCompound_REST_TypeInfo_ShowsCompound` — `/type_info/{path}` returns compound symbol for compound-typed fields

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run ./...` clean
- `go test ./... -race -count=3` clean
- Coverage ≥ 95% (current baseline)
- **All existing Phase 16.30 atomic-unit tests must pass without modification**

### Baseline test shifts

User-entity-start moves from 63 to 73. Update:

- `@isa_test.go` lines 666–681 — `TestIsAWorldCountBaseline` currently asserts `base == 62` (47 original + 15 unit); after this phase: `62 + 10 = 72`. Update comment and constant.

Audit any other place that hard-codes 62 or relies on the first user entity index. The grep above found only `isa_test.go`; iterate should re-grep before finalizing.

### Documentation matrix

- `@docs/Units.md` — major extension; new \"Compound units\" section with built-in table, helpers, conversion algorithm, symbol composition rules, dimensional-mismatch behavior
- `@docs/README.md` line 78 — flip \"Compound units (`m/s`, `kg·m²/s²`) deferred to Phase 16.30.1\" → ✅ **shipped in v0.97.0**
- `@CHANGELOG.md` — v0.97.0 entry
- `@ROADMAP.md` line 88 — strip the \"deferred to Phase 16.30.1\" sentence; add a new Phase 16.42 entry under Shipped; bump header from \"through v0.96.0\" to \"through v0.97.0\"
- `@README.md` — Units feature row, if compound is called out separately

## Constraints

- @units.go — current Units addon (206 LOC, Phase 16.30); `Unit` type is `{Symbol, Name, Base, Factor}`; the field names `Over`, `Power`, and `Symbol` must not collide with existing fields (verified: no collision today). `RegisterUnit` panics on `factor == 0` — extend the same panic discipline to compound registration (zero numerator/denominator, depth overflow).
- @units_test.go — extend, do not replace. Phase 16.30 tests assert `Newton.Symbol == \"N\"`, `Hertz.Symbol == \"Hz\"`, etc.; the compound auto-symbol must produce the same strings for those three.
- @world.go — 10 new ID fields added to the `World` struct; preserve declaration order under the existing comment block (lines 98–113). `New()` (lines 206+) extends to allocate the new built-in entities. `builtinUnitByIndex` and `isBuiltinUnit` bound checks change from `48..62` to `48..72`.
- @marshal.go — `jsonUnitDef` is the persistence struct; extend additively so existing v1-format snapshots still load. `BaseSerial` / `BaseBuiltinIdx` pattern repeats for `Over`.
- @snapshot.go — currently has no unit serialization (zero matches for \"unit\"). Add unit serialization OR document the same gap; do not silently lose compound user units across snapshot round-trip.
- @rest_type_walk.go — `Unit` field (lines 26, 37) must carry the compound symbol, not just an atomic-unit symbol; verify the call site uses `UnitSymbol(unitID)` so compound formatting flows through.
- @isa_test.go — `TestIsAWorldCountBaseline` at line 675 asserts `base == 62`; update to `72` and the comment to reflect 47 originals + 25 unit entities (15 atomic + 10 compound).
- @docs/README.md line 78 — flip the explicit \"deferred to Phase 16.30.1\" gap note; the gap closes with this phase.
- @ROADMAP.md line 88 — strip the deferred clause from the Phase 16.30 entry; add a new Phase 16.42 entry; bump the \"Shipped through\" header.
- @docs/Units.md — extend with a Compound units section before declaring this phase complete.
- @CHANGELOG.md — add v0.97.0 entry under the v0.96.0 entry (current top).
- **Non-goals (do not implement):**
  - Dimensional analysis at query time — Go's type system enforces type-level distinction; no runtime dimensional checks on `Set[Position]`.
  - Treating SI prefixes (kilo-, milli-) as compound-unit components — they remain atomic (`KiloMeter`, `MilliMeter`); compound units sit on top.
  - Implicit unit conversion in query results — `Convert` is always explicit.
  - Plain-text parser for compound unit symbols (e.g. parsing `\"m/s\"` to a unit ID) — registration is via API only.
