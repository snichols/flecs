## Goal

Port the upstream **Units addon** to give components typed unit semantics. Each unit is an entity carrying `Symbol`, `Name`, an optional `Base` (the "derived from" unit, e.g. `KiloMeter.Base = Meter`), and a `Factor` describing the magnitude relation to the base. Components can be tagged with their unit so tools, debug overlays, and serializers can render values as e.g. `5.2 m/s` instead of `5.2`, and so callers can convert between compatible units (km → m, hours → seconds).

Target version: **v0.85.0** (next after v0.84.0 Stats addon, commit `3d650c8`). Closes `docs/README.md` line 78 (Phase 14.0 survey gap).

### Public API surface

New file `units.go`:
- `Unit` struct: `Symbol string`, `Name string`, `Base ID` (the base unit; zero ID for a base/SI unit itself), `Factor float64` (multiplier to convert this unit's value to its base; `1000` for `KiloMeter → Meter`).
- `RegisterUnit(w *Writer, name, symbol string, base ID, factor float64) ID` — register a new unit entity. Panics on `factor == 0` (zero factor is meaningless and would explode `Convert`).
- `(*World).UnitFor(componentID ID) (ID, bool)` — return the unit attached to a component, if any.
- `(*Writer).SetUnit(componentID ID, unitID ID)` — tag a component with its unit. Stored via a hidden built-in `(Unit, *)` exclusive relationship or a side map (implementer's choice; pick what round-trips through marshal cleanly).
- `Convert(value float64, fromUnit, toUnit ID) (float64, bool)` — convert between compatible units (same root base after walking the `Base` chain). `ok=false` for incompatible. `fromUnit == toUnit` returns `value` unchanged.

### Built-in units (bootstrapped at world creation)

Locked-in small standard set, sourced from upstream:

- **Length**: Meter (base), KiloMeter (factor 1000), MilliMeter (factor 0.001)
- **Duration**: Second (base), MilliSecond (factor 0.001), Minute (factor 60), Hour (factor 3600)
- **Mass**: Gram (base), KiloGram (factor 1000), MegaGram (factor 1_000_000)
- **Force**: Newton
- **Energy**: Joule
- **Frequency**: Hertz
- **Angle**: Radian, Degree

The next free built-in entity index after Phase 16.29 is **48** (SlotOf is 47, per `world.go:97` and `world.go:218`). Allocate the unit entities starting at 48 in a fixed order; document the allocation order in the `world.go` New() comment block and bump "user entities start at index N" accordingly.

### Compound units

Upstream C `EcsUnit` has `over` (numerator/denominator entity ref) for compounds like `MetersPerSecond` — see `meta.h:506-512`. **Deferred to Phase 16.30.1.** v1 supports atomic units only: callers wanting `m/s` register an opaque unit with `Symbol: "m/s"`, `Name: "MetersPerSecond"`, `Base: 0`, `Factor: 1`. No `over` semantics; `Convert` does not understand the compound relationship.

### Divergence from C

- Single `Factor float64` instead of C's `{factor int32, power int32}` translation — idiomatic Go, lossless for the small standard set, simpler `Convert` math.
- Symbols stored verbatim from the user (no normalization; upstream auto-populates from prefix + base when set).
- No `EcsUnitPrefix` entity tree — the small built-in set inlines its factors. Prefix entities can be added in Phase 16.30.1 alongside `over`.
- No `EcsQuantity` parent scoping (Duration, Length, etc. as parent entities). The base-unit chain implicitly groups compatible units; no separate quantity classification.

### Open decisions (locked)

1. Built-in unit list: small standard set — **locked**.
2. Compound units: deferred to Phase 16.30.1 — **locked**.
3. Unit-symbol normalization: user-supplied — **locked**.

## Constraints

- @CONTRIBUTING.md — required doc updates per the "what to update" table: `docs/Units.md`, `docs/README.md`, `README.md`, `CHANGELOG.md` v0.85.0 entry, `ROADMAP.md` (Future Work → Shipped). Coverage ≥ 95.0% on the new file (project standard is ≥ 90% but this task spec requires 95.0%).
- @docs/README.md — line 78 currently reads `- **Units addon** — not ported.` Flip to a ✅ shipped entry in the v0.83.0 / v0.84.0 / v0.85.0 style (see lines 76–77 for the recent Alerts/Stats templates).
- @ROADMAP.md — heading line 3 `## Shipped (through v0.84.0)` bumps to `## Shipped (through v0.85.0)`. Add a Phase 16.30 entry in the Shipped section in the v0.83.0/Alerts format (line 87 is the precedent).
- @CHANGELOG.md — prepend v0.85.0 entry above the v0.84.0 entry at line 3; follow the v0.84.0 Stats / v0.83.0 Alerts shape.
- @README.md — feature list bump.
- @world.go — new built-in entity slot allocation. Add unit IDs as fields (lines 60–97 region), extend the `New()` allocation-order comment block (lines 170–219), bump the "user entities start at index N" sentinel (currently `index 48` per line 97 / line 219). Allocate the built-in unit entities in `New()` itself, before the user-entity threshold. Bootstrap order must match the doc comment.
- @scope.go — `Reader`/`Writer` API patterns; `UnitFor` is a Reader method (parallels existing reader methods at lines 50–360), `SetUnit` is a Writer method (parallels Writer methods at lines 363–420). Follow the receiver style (`r *Reader`, `fw *Writer`) used throughout scope.go.
- @alerts.go — addon precedent for v1 file shape (package decl, top-of-file structs, exported constructors and registration). Phase 16.28.
- @stats_addon.go — second addon precedent. Phase 16.29.
- @marshal.go — round-trip pattern; the alerts addon serializes its `alertDefs` slice into the `jsonWorld` envelope (lines 478–509, 523). User-registered units must round-trip the same way: serialize their `(name, symbol, base-serial, factor)` plus the `(componentID → unitID)` mapping; restore in UnmarshalJSON. Built-in units do not serialize (they are re-allocated by `New()` at the same indices). Round-trip a user-registered unit + a component tagged with a built-in unit + a component tagged with a user unit.
- C upstream reference: `include/flecs/addons/units.h:43-356` (full entity list); `include/flecs/addons/meta.h:493-518` (EcsUnit / ecs_unit_translation_t struct shape); `src/addons/units.c:177-253` (Seconds / Minutes / Hours bootstrap pattern with factor+power); `src/addons/units.c:364-431` (Meters / KiloMeters / MilliMeters bootstrap); `src/addons/units.c:482-525` (MetersPerSecond / compound-unit pattern — the deferred work).
- Non-goals: no compound units (`m/s`, `kg·m²/s²`) modelled structurally in v1 — they can exist as opaque entities with literal symbols. No automatic unit conversion in formula evaluation — caller invokes `Convert` explicitly. No SI-vs-US-Customary enforcement — both coexist freely.

### Tests required in `units_test.go` (≥ 8 cases, coverage ≥ 95.0%)

1. Register a user unit; Reader returns Symbol/Name/Base/Factor matching the registration.
2. `SetUnit` on a component; `UnitFor` returns the unit.
3. Built-in units exist and resolve: Meter, Second, KiloGram, Newton, Joule, Hertz, Radian, Degree.
4. `Convert(1000, KiloMeter, Meter)` — note: 1 km = 1000 m, so the call is actually `Convert(1, KiloMeter, Meter) == 1000`, and `Convert(1000, Meter, KiloMeter) == 1`. Test both directions.
5. `Convert(1, Hour, Second) == 3600`.
6. `Convert(v, Meter, Second)` — incompatible base chain returns `ok=false`.
7. Marshal round-trip: user-registered unit survives; component tagged with built-in unit survives; component tagged with user unit survives.
8. `fromUnit == toUnit` short-circuits to the input value unchanged.
9. `RegisterUnit` with `factor == 0` panics.
10. Multi-hop conversion: register `Day` with `Base = Hour, Factor = 24`; `Convert(1, Day, Second) == 86400` (must walk `Day → Hour → Second`).

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage on `units.go` ≥ 95.0%

### Process

- Feature, not bug.
- Label: `snichols/queued`.
