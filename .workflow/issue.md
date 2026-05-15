## Goal

Port the upstream Flecs **Timer addon**: independent, entity-based timers and rate filters that can drive system tick rate. A `Timer` or `RateFilter` becomes a first-class entity that any number of systems can bind to via a new `(*System).SetTickSource(e ID)` hook — when the timer fires this tick, all systems pointing at it fire; otherwise they skip.

This is **distinct from** the system-level rate filters already shipped in Phase 16.6 (`(*System).SetInterval` / `(*System).SetRate` in @system.go:168-212). Those are per-system gates with no sharing; Timer addon makes the gate itself a shared entity. The two should coexist — see precedence note under tests.

The new addon also leaves the existing @timer.go (OnFixedUpdate frame-timing accumulator: `Time()`, `FrameCount()`, `SetFixedTimestep`) **completely untouched**. The new code lives in `timer_addon.go` to avoid name collision with that file. The existing test file @timer_test.go owns frame-timing tests; new addon tests live in `timer_addon_test.go`.

**Target version:** v0.91.0
**Phase number:** 16.36

### Proposed surface

**Components** (lazy-registered on first call to a Timer/RateFilter constructor; mirrors the alerts/units pattern of not consuming built-in entity indices). Both are typed components stored in archetype columns so they participate naturally in snapshots, JSON, observers, and queries.

```go
type Timer struct {
    Timeout    time.Duration
    Elapsed    time.Duration
    Active     bool
    SingleShot bool
    Fired      bool // transient: set during tick, cleared at end of tick
}

type RateFilter struct {
    Rate        int32         // fires every Nth parent tick; N<=1 ⇒ every parent tick
    TickCount   int32         // counter toward Rate
    Src         ID            // 0 ⇒ world frame; non-zero ⇒ another Timer or RateFilter entity
    TimeElapsed time.Duration // wall-clock since last fire (verify upstream semantics)
    Fired       bool          // transient: set during tick, cleared at end of tick
}
```

**Top-level API** (package `flecs`):

- `NewTimer(fw *Writer, timeout time.Duration) ID` — creates entity with `Timer{Timeout:t, SingleShot:true, Active:true}`
- `NewInterval(fw *Writer, interval time.Duration) ID` — creates entity with `Timer{Timeout:i, SingleShot:false, Active:true}`
- `SetTimeout(fw *Writer, e ID, timeout time.Duration)` — sets timeout, enables SingleShot, resets Elapsed/Fired/Active
- `SetInterval(fw *Writer, e ID, interval time.Duration)` — sets timeout, clears SingleShot, resets Elapsed/Fired/Active
- `GetTimeout(s scope, e ID) time.Duration`
- `GetInterval(s scope, e ID) time.Duration`
- `StartTimer(fw *Writer, e ID)` — `Active=true`, clears `Elapsed` and `Fired`
- `StopTimer(fw *Writer, e ID)` — `Active=false`
- `ResetTimer(fw *Writer, e ID)` — clears `Elapsed` and `Fired`; preserves `Active`
- `IsTimerFired(s scope, e ID) bool` — reads Fired flag for last tick
- `NewRateFilter(fw *Writer, rate int32, source ID) ID` — creates `RateFilter{Rate:rate, Src:source}`; source=0 ⇒ world frame
- `SetRate(fw *Writer, e ID, rate int32)` — note: package-level free function on a `Timer`/`RateFilter` entity, NOT a method on `*System`. Disambiguate from @system.go:203 `(*System).SetRate` in docs.
- `(*System).SetTickSource(e ID) *System` — bind system to fire only when the timer/ratefilter entity fires this tick. Returns `s` for fluent chaining.
- `(*System).TickSource() ID` — read accessor (returns 0 when unbound).

**Naming caution:** the package-level `SetRate(fw, e, rate)` and the existing method `(*System).SetRate(n)` will both exist. The free function takes a `*Writer` and an `ID` as first two args; the method has no `*Writer`. The names disambiguate by signature, but the `docs/Systems.md` TickSource section must call this out explicitly to prevent confusion.

### Pipeline integration

In @system.go:357 `World.Progress(dt)`, after `w.frameCount++` / `w.time += dt` (line 367-368) but BEFORE the `runPhase` loop is invoked, add two passes that walk all entities carrying the new components:

1. **`tickAllTimers(w, dt)`** — for every entity with `Timer`:
   - If `!Active`, skip (still clear `Fired` from previous tick).
   - `Elapsed += dt`
   - If `Elapsed >= Timeout`: `Fired = true`; if `SingleShot` then `Active=false, Elapsed=0`; else `Elapsed -= Timeout` with subtract-with-cap: clamp `Elapsed` to `<= Timeout` to prevent runaway when `dt >> Timeout`. Mirrors the @system.go:379-388 pattern.

2. **`tickAllRateFilters(w, dt)`** — for every entity with `RateFilter`:
   - `TimeElapsed += dt`
   - Determine `parentFired`: if `Src == 0`, parentFired=true (world frame ticks every Progress call); else read Timer.Fired or RateFilter.Fired on `Src` — must run AFTER tickAllTimers so the parent's flag is up-to-date for this tick.
   - If parentFired: `TickCount++`; if `TickCount >= Rate` (treating Rate<=1 as 1): `Fired=true, TickCount=0, TimeElapsed=0`.

3. **System gate** at @system.go:376-396: after the existing `interval` and `rate` checks (Phase 16.6), if `s.tickSource != 0`, look up that entity's `Timer.Fired` or `RateFilter.Fired`; if not fired this tick, skip the system (increment `statsSkipped`). **Precedence:** existing per-system `interval`/`rate` gates are evaluated FIRST; if they skip the system, the tick-source check is not reached. Document this precedence in `docs/Systems.md`. Optionally panic on `SetTickSource` if `interval > 0 || rate > 1` to force users to pick one model — agent decides after reading upstream behaviour.

4. **End-of-tick fired-flag sweep**: after all phases complete in `Progress`, walk both component populations again and clear `Fired = false`. Transient one-tick signal. Implement as a third helper `clearAllTimerFiredFlags(w)` called from a `defer` at the top of `Progress` (after the existing `defer func() { w.inProgress = false }()`).

**Walk implementation:** use a cached query over each component type (lazy-registered on the world the first time it's needed). Populations are expected to be small (tens, not thousands); archetype iteration is fine.

**OnFixedUpdate interaction:** the OnFixedUpdate phase calls Progress-phase logic multiple times per outer Progress. Tick the Timer/RateFilter components ONCE per outer Progress (in the pre-phase pass), not per fixed-step iteration. Document this — a Timer with timeout < fixedTimestep will still only see one accumulation per Progress call, not per fixed sub-step.

### File layout

- **NEW** `timer_addon.go` — `Timer` / `RateFilter` types, lazy component registration, all top-level API functions, `tickAllTimers` / `tickAllRateFilters` / `clearAllTimerFiredFlags`.
- **NEW** `timer_addon_test.go` — comprehensive tests (see acceptance list below).
- **MODIFY** @world.go (`Progress` body, ~line 357-368): hook `tickAllTimers` then `tickAllRateFilters` before phase dispatch begins; arrange clear-fired-flags via deferred end-of-tick. Confirm exact insertion site (the function actually lives in `system.go` at line 357 per current code — verify and place the call there if `system.go` still owns Progress).
- **MODIFY** @system.go: add `tickSource ID` field to `*System`; add `SetTickSource(e ID) *System` and `TickSource() ID` methods. Modify the pipeline-dispatch gate (@system.go:376-396) to consult `tickSource` after existing interval/rate checks.
- **MODIFY** @snapshot.go: ensure `Timer` and `RateFilter` round-trip via the generic archetype serializer (they should "just work" because they're plain typed components; add explicit tests anyway).

The existing @timer.go (OnFixedUpdate frame timing) is **not modified**.

## Constraints

- @CLAUDE.md — repository conventions (commit/PR/test standards).
- @ROADMAP.md — version + phase numbering. Current heading is "Shipped (through v0.90.0)" (line 3); bump to v0.91.0 on completion. Add Phase 16.36 row in the chronological list immediately after the v0.90.0 entry (line 93).
- @docs/README.md — line 89 currently reads: \`- **Timer addon** (independent rate control per system) — partial (\`timer.go\` exists; full addon API pending).\` Flip to: \`- **Timer addon** — ✅ **shipped in v0.91.0** ...\` with link to docs/Timer.md. This is the explicit gap entry the phase closes.
- @CHANGELOG.md — top-of-file v0.91.0 entry following the format at line 3 (e.g. \`## v0.91.0 — <date> — Phase 16.36: Timer addon (entity-based Timer + RateFilter)\`).
- @README.md — feature table addition for Timer addon.
- @system.go:42-45 — Phase 16.6 per-system rate gate fields (`interval`, `intervalAccum`, `rate`, `rateCounter`). The new `tickSource` field belongs adjacent to these.
- @system.go:168-212 — Phase 16.6 `SetInterval` / `SetRate` methods. The new `SetTickSource` belongs adjacent to these and must document the interaction with them in its godoc.
- @system.go:357-396 — `Progress` body and per-system gate logic. The hook sites for both the pre-phase Timer/RateFilter walks and the tick-source gate live here.
- @timer.go — existing OnFixedUpdate frame-timing accumulator. DO NOT MODIFY. New addon lives in new file `timer_addon.go` to avoid filename collision.
- @timer_test.go — existing OnFixedUpdate frame-timing tests. New addon tests live in new file `timer_addon_test.go`.
- @alerts.go — pattern template for an entity-bearing addon: `Register*(fw *Writer, ...) ID` shape, lazy `world.alertInstances` map init, snapshot-friendly storage, package-level reader functions on `*World`.
- @units.go — pattern template for typed entity components, lazy map init in `world.go`, `scopeWorld()` Writer access pattern, JSON round-trip via `unitDefs` map.
- @scope.go — `scope` interface for read accessors like `GetTimeout`, `GetInterval`, `IsTimerFired`. Use this interface (not `*Reader` or `*World` directly) so accessors compose with both `Read` and `Write` blocks.
- @snapshot.go — typed components round-trip through the generic archetype serializer at lines 36-71; verify Timer / RateFilter survive without addon-specific snapshot code, and add explicit tests.
- @docs/Systems.md — append a `## TickSource` subsection cross-linking to `Timer.md`. Document precedence vs `SetInterval`/`SetRate`.
- @docs/Stats.md — Stats addon tracks `statsSkipped` per system (@system.go:51). Tick-source-gated skips must increment this counter the same way interval/rate skips do, so existing Stats output continues to reflect "ticks where system did not run". No new field needed.
- @doc.go:454 — package overview lists current addons; append a "Timer addon" section mirroring the Stats one once Phase 16.36 ships.
- **Non-goal: async / wall-clock OS timers.** Timer.Elapsed advances by `dt` passed to Progress; never by `time.Now()`. Document this prominently in `docs/Timer.md`.
- **Non-goal: TickSource liveness tracking.** If a system's `TickSource` entity is deleted, the system simply never fires again (Timer/RateFilter component lookup returns zero-value, `Fired` is false). Document this; do not add cleanup hooks.
- **Non-goal: histograms / windowed timing.** That belongs to the Stats addon, not Timer.

## Upstream research (do before writing code)

Fetch the upstream Flecs sources and quote line numbers for these. The iterate agent should use WebFetch on `https://github.com/SanderMertens/flecs/blob/master/include/flecs.h` and `https://github.com/SanderMertens/flecs/blob/master/src/addons/timer.c` (or local clone) and confirm:

1. **`EcsTimer` / `EcsRateFilter` struct layouts** in `flecs.h`. Quote line numbers and the exact field names. Verify whether upstream has `time_elapsed` on RateFilter and what semantics — accumulator since last fire, or wall-clock total?
2. **`ecs_set_timeout` / `ecs_set_interval` / `ecs_set_rate` / `ecs_start_timer` / `ecs_stop_timer` / `ecs_reset_timer`** function signatures in `flecs.h`. Quote line numbers. Confirm the Go port matches semantically.
3. **`ecs_set_tick_source` / `ecs_get_tick_source`** signatures and behavior. Critically: does upstream allow chaining (RateFilter.src → Timer; system.tick_source → RateFilter)? If so the Go port must too — Test `TestRateFilter_ChainedRateFilters` and `TestRateFilter_ParentTimer` cover this.
4. **`src/addons/timer.c`** — actual subtract-with-cap accumulator logic. Quote the lines so the Go implementation matches numerically. The Phase 16.6 commentary at @system.go:172-176 already cites `timer.c:33–35` for the existing rate gate; the new addon must use the identical math.
5. **System-vs-tick-source precedence:** does upstream forbid combining `tick_source` with `rate_filter`/`interval` on the same system, or does it allow the combination? Phase 16.6 already documents (@system.go:177-180) that Go-flecs allows the existing interval+rate AND-combination as a deliberate divergence; document the analogous decision for tick_source.
6. **End-of-tick clearing of Fired:** confirm upstream timer.c semantics — is `Fired` cleared at end of tick or sticky until next fire? The proposal above assumes one-tick-window clearing; revise if upstream differs and document the chosen behavior.

Pull request must quote line numbers from these files in commit message / docs.

## Acceptance — required tests (in `timer_addon_test.go`)

- `TestTimer_SingleShotFires` — Timer{Timeout:100ms}: Progress(50ms) → not fired; Progress(60ms) → Fired==true exactly once, Active==false.
- `TestTimer_IntervalFires` — Interval{100ms}: Progress(250ms) once → Fired==true; assert Elapsed≈50ms (subtract-with-cap). Then Progress(60ms) → Fired==true again; Elapsed≈10ms.
- `TestTimer_StartStopReset` — Stop halts accumulation across Progress calls; Start re-arms (Elapsed=0, Active=true); Reset clears Elapsed without changing Active.
- `TestTimer_NotActive_DoesNotAccumulate` — paused timer: Active=false; multiple Progress calls; Elapsed remains 0.
- `TestTimer_FiredFlag_OneTickWindow` — immediately after Progress where timer fires: IsTimerFired==true; after next Progress (where it doesn't fire): IsTimerFired==false.
- `TestRateFilter_WorldFrame` — `NewRateFilter(rate=3, source=0)`: fires on Progress calls 3, 6, 9, …
- `TestRateFilter_ParentTimer` — RateFilter{Rate=2} bound to Interval{100ms}: fires every 200ms of accumulated dt (every 2nd interval-timer fire).
- `TestRateFilter_ChainedRateFilters` — two chained RateFilters Rate=2 and Rate=3 with world-frame root: second fires every 6th Progress call.
- `TestSystem_SetTickSource_Timer` — system bound to SingleShot Timer{50ms}: Progress(60ms) once runs system; subsequent Progress calls do not.
- `TestSystem_SetTickSource_Interval` — system bound to Interval{100ms}: Progress(50ms)×4 → system runs twice (at ticks 2 and 4).
- `TestSystem_SetTickSource_RateFilter` — system bound to RateFilter{Rate=3, src=0}: system runs on Progress calls 3, 6, 9.
- `TestSystem_TickSource_ReplacesInternalRate` — research upstream precedence first. If upstream forbids combining: panic on SetTickSource when interval/rate set. If upstream composes: AND-compose, document. Test asserts whichever decision is made.
- `TestSystem_TickSource_DeletedEntity` — system bound to a Timer; delete the Timer entity; subsequent Progress calls do not run the system (no crash, no panic).
- `TestTimer_DeferredOps` — StartTimer/StopTimer/ResetTimer called inside `w.Write(fn)` (deferred scope) take effect after flush, not synchronously.
- `TestTimer_JSON_RoundTrip` — populate Timer + RateFilter entities + system tick sources; `MarshalJSON` then fresh world + `UnmarshalJSON`; equality on all fields including Elapsed and TickCount.
- `TestTimer_Snapshot_RoundTrip` — same as above but via `TakeSnapshot` / `RestoreSnapshot`.
- `TestTimer_GetTimeoutInterval_Accessors` — round-trip GetTimeout / GetInterval for both SingleShot and Interval timers; assert correct values via the `scope` interface inside both `Read` and `Write` blocks.

## Acceptance — mechanical

- `go vet ./...` clean
- `golangci-lint run ./...` clean
- `go test ./... -race -count=3` clean
- Coverage ≥ 95% (current baseline at HEAD per commit 66efcdf is 95.0%)
- New files present: `timer_addon.go`, `timer_addon_test.go`, `docs/Timer.md`
- Existing files unchanged in name: @timer.go, @timer_test.go

## Documentation update matrix

- **NEW** `docs/Timer.md` — top-level user guide: Timer vs RateFilter; the `SetTickSource` binding; relationship to (but distinct from) @system.go:168-212 per-system `SetInterval`/`SetRate`; relationship to (but distinct from) @timer.go OnFixedUpdate frame-timing; chaining (RateFilter → Timer → system); JSON & snapshot round-trip notes; divergences from upstream with `timer.c` / `flecs.h` line citations.
- @docs/Systems.md — append `## TickSource` subsection cross-linking to `Timer.md`. Document precedence over `SetInterval`/`SetRate`.
- @docs/README.md — flip line 89 from "partial" to "✅ shipped in v0.91.0" with link to `Timer.md`.
- @README.md — add Timer addon row to feature table.
- @CHANGELOG.md — new top entry for v0.91.0 mirroring v0.90.0's format (CHANGELOG.md:3).
- @ROADMAP.md — bump "Shipped (through v0.90.0)" → v0.91.0 (line 3); add a Phase 16.36 entry in the shipped list (insert after the v0.90.0 entry at line 93). Cross-reference Phase 16.6 (line 65) which shipped the per-system gates.
- @doc.go — once Stats section is in place (line 454), append a `# Timer addon` section.

## Notes

- Bundle Timer + RateFilter + SetTickSource in a single phase: they share the pre-phase walk hook in `Progress`, share end-of-tick fired-flag clearing, and have minimal value individually.
- Label: `snichols/queued` (NOT a bug — this is greenfield phase work).
- The `(*System).SetTickSource` method must return `*System` for fluent chaining; the existing @system.go:298 `DependsOn` is a precedent.
- The package-level `SetRate(fw, e, rate)` function name conflicts visually with the existing `(*System).SetRate(n)` method. Disambiguation is by receiver/argument shape, but the godoc on the new free function and the existing method must each cross-link the other to prevent caller confusion.
- Use `scopeWorld()` from `*Writer` for write-side world access (see @units.go:29, @units.go:45).
- Use the `scope` interface from @scope.go for read accessors so they compose with both `Read` and `Write` blocks.
- Verify the exact insertion site for `tickAllTimers` / `tickAllRateFilters` in @world.go vs @system.go — the prompt's source assumes `Progress` lives in `world.go`, but a grep places it at @system.go:357. The iterate agent should follow the actual code, not the prompt's path hint.
