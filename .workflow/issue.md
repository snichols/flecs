## Goal

Add per-system rate filters that let a system run less often than every pipeline tick, without restructuring the pipeline or adding new phases. Two related controls on `*System`:

- **`SetInterval(d time.Duration)`** — system runs at most once per wall-clock duration `d` of accumulated `dt`. Useful for "save every 5 seconds" or "GC sweep every 100ms".
- **`SetRate(n int32)`** — system runs every Nth pipeline tick. Useful for "physics every 2nd frame" or "AI update every 4th frame".

**Gap closed:** `docs/README.md:144` — *"Rate filters (SetInterval / SetRate) — run a system every N frames or at a fixed wall-clock interval per system without restructuring the pipeline. not yet ported in Go flecs."* Flips to shipped (v0.61.0).

**Target version:** v0.61.0 (next after v0.60.0 observer lifecycle bundle shipped at `00fde0c`).

### Precedent (Phase 16.3, v0.58.0, `c1c3fc9`)

Go-flecs systems are `*System` structs, not entities. Per-system state is added directly to the struct (e.g., `enabled bool` at `system.go:36`, with `SetEnabled` / `IsEnabled` at `system.go:145-148`). Rate filters follow the same pattern: new fields on `*System`, new methods, executor-side check in `runPhase`.

### Upstream C semantics — verified

From `/work/agents/claude/projects/SanderMertens/flecs`:

1. **Desc fields** (`include/flecs/addons/system.h:87-91`):
   - `ecs_ftime_t interval;` — interval in seconds at which the system should run
   - `int32_t rate;` — rate at which the system should run
2. **APIs** (`include/flecs/addons/timer.h:111-115` and `:203-208`):
   - `ecs_entity_t ecs_set_interval(world, tick_source, ecs_ftime_t interval)`
   - `ecs_entity_t ecs_set_rate(world, tick_source, int32_t rate, ecs_entity_t source)`
3. **Interval accumulator** (`src/addons/timer.c:28-47`, `ProgressTimers`):
   - `time_elapsed = timer.time + delta_time_raw`
   - If `time_elapsed >= timeout`: tick fires, `timer.time = time_elapsed - timeout` (subtract-with-cap; if remainder still exceeds timeout, clamp to 0 at line 33-35)
   - Else: `timer.time = time_elapsed` (no fire, accumulate)
4. **Rate counter** (`src/addons/timer.c:75-83`, `ProgressRateFilters`):
   - `filter.tick_count++`
   - Triggered when `!(tick_count % rate)` — modulo, not equality
   - On trigger: `filter.time_elapsed = 0` (only `time_elapsed` resets; counter keeps growing)
5. **Per-system gate in pipeline** (`src/addons/system/system.c:41-58`, `flecs_run_intern`):
   - If `tick_source` is set and `!tick->tick`: `return 0` (skip the system this frame)
   - This is the precedent for the executor-side gate.
6. **Interaction — upstream forbids both** (`src/addons/system/system.c:230-235`, `flecs_system_init_timer`): if `interval != 0 && rate != 0`, upstream emits `ecs_err("system %s cannot have both interval and rate set")` and rejects the system. **Go-flecs will deliberately diverge — see open decision 1 below.**

### Go-side state to extend

- `@system.go` — `*System` struct lines 30-41 (existing `enabled bool` at line 36 is the exact precedent). `runPhase` closure lives at `system.go:279-393`; the per-system gate insertion point is the existing `for _, s := range w.systems` filter at line 282 (`if !s.removed && s.enabled && s.phase == p`).
- `@world.go` — `frameCount uint64` at line 126 and `time float32` at line 125; both are already advanced by `Progress` (`system.go:276-277`).
- `@scope.go` — `Progress`/timing helpers from Phase 16.3 research notes.

## Constraints

- @system.go — *Phase 16.3 precedent.* `*System` struct (lines 30-41) already carries `enabled bool` (line 36) with `SetEnabled` / `IsEnabled` (lines 145-148). Rate fields go alongside `enabled`; methods follow the same single-line style. The `runPhase` closure at lines 279-393 is the gate insertion point — the existing pre-loop filter at line 282-286 (`!s.removed && s.enabled && s.phase == p`) is where the new interval/rate gate composes.
- @world.go — *Tick / dt source.* `frameCount uint64` at line 126 and `time float32` at line 125 are already incremented in `Progress` (`system.go:276-277`). The new fields read `dt` (the `phaseDT float32` parameter to `runPhase`) — already available; no new world-side state needed.
- @scope.go — *Progress / fixed-step machinery.* Documents how `dt` is sourced and forwarded. Rate filters operate on the same `dt` channel.
- @docs/Systems.md — *Doc convention.* Existing § Disabling a System (line 174) and § Single-system Run (line 198) are the exact precedent for the new § Rate filters section. Match style: short intro, single code block, callout about composition with other gates.
- @docs/README.md — *Gap registry.* Line 144 flips from "not yet ported" to shipped marker (matching the format used at lines 143, 145, 147).
- @CHANGELOG.md — *Top entry.* v0.61.0 entry at line 3 (above the current v0.60.0). Convention matches existing entries.
- @ROADMAP.md — *Heading bump.* Line 3 "Shipped (through v0.60.0)" becomes "through v0.61.0". Add bullet under v0.61.0 with the same Phase entry style as line 64 (Phase 16.5).
- @CONTRIBUTING.md — *Mechanical gates.* `go test ./... -race && golangci-lint run` (line 6) must pass; coverage `≥ 95.0%` per recent commits' standard (CONTRIBUTING.md line 9 specifies the root flecs package floor; recent phases have held 95.0%).
- Upstream C divergence — `/work/agents/claude/projects/SanderMertens/flecs/src/addons/system/system.c:230-235` explicitly rejects systems with both `interval` and `rate` set. Go-flecs deliberately allows both gates simultaneously (AND composition) because there is no `tick_source` abstraction in Go-flecs and the two filters can compose cleanly per-system. **Document this divergence in the Systems.md § Rate filters callout.**

## Deliverables

### 1. New fields on `*System` (system.go:30-41)

```go
interval      time.Duration // 0 = no interval gate (every tick)
intervalAccum time.Duration // accumulated time since last run; subtracted (not reset) on fire
rate          int32         // 0 or 1 = no rate gate (every tick)
rateCounter   int32         // increments each pipeline visit; runs when counter % rate == 0
```

### 2. New methods on `*System`

- `SetInterval(d time.Duration)` — store the interval; reset `intervalAccum` to 0. `d == 0` disables interval gating. Panic on `d < 0`.
- `GetInterval() time.Duration`
- `SetRate(n int32)` — store the rate; reset `rateCounter` to 0. `n == 0` or `n == 1` disables rate gating. Panic on `n < 0`.
- `GetRate() int32`

**Concurrency**: plain field access, no atomics (matches Phase 16.3 `SetEnabled`). Document that interval/rate should only be modified between ticks, not from inside a system's callback during dispatch.

### 3. Pipeline executor gate (system.go runPhase, line 279)

Refactor the per-system enabled filter at lines 282-286 into a composable gate. The new active-set construction:

```go
for _, s := range w.systems {
    if s.removed || !s.enabled || s.phase != p { continue }
    // Interval gate
    if s.interval > 0 {
        s.intervalAccum += time.Duration(float64(phaseDT) * float64(time.Second))
        if s.intervalAccum < s.interval { continue }
        s.intervalAccum -= s.interval
        if s.intervalAccum > s.interval { s.intervalAccum = 0 } // cap matches upstream timer.c:33-35
    }
    // Rate gate
    if s.rate > 1 {
        s.rateCounter++
        if s.rateCounter % s.rate != 0 { continue }
    }
    active = append(active, s)
}
```

- **Disabled systems**: neither counter advances while `!s.enabled`. Re-enabling does not cause backlog catch-up.
- **Both gates set**: BOTH must pass (AND semantics). Divergence from upstream which forbids this combination — document in Systems.md.
- **Interval accumulator**: subtract-with-cap (matches upstream `timer.c:33-35`). Subtraction preserves catch-up accuracy across normal-length ticks; the cap prevents runaway accumulation if a single tick's dt vastly exceeds the interval.

### 4. Tests in `system_rate_test.go` (≥ 10 cases, ≥ 95.0% coverage)

1. **Rate=2 exact firing pattern**: 10 `Progress` calls → system runs on ticks 2, 4, 6, 8, 10 (5 runs).
2. **Rate=1 disables gating**: equivalent to no rate filter.
3. **Rate=0 disables gating** (sentinel).
4. **Interval=100ms with dt=30ms**: fires on tick 4 (accum 120ms ≥ 100ms; remainder 20ms), then tick 8 (accum 20+30+30+30=110ms ≥ 100ms; remainder 10ms). Lock in the exact firing pattern.
5. **Interval with variable dt**: dts of 50ms, 200ms (single tick exceeds interval), 30ms; verify subtract-with-cap.
6. **SetInterval(0)** disables interval gating after it was set.
7. **SetRate(0)** disables rate gating; **SetRate(1)** also disables.
8. **Combined interval AND rate**: both must pass. System fires only on ticks where both gates open.
9. **Disabled system**: rate counter and interval accumulator do not advance during disabled period.
10. **Re-enable after disable**: counters resume from pre-disable state; no catch-up storm.
11. **Concurrent SetRate / SetInterval from outside the pipeline (between ticks)**: race detector clean.
12. **Negative argument panic**: `SetInterval(-1)` and `SetRate(-1)` panic.

### 5. Doc updates (per CONTRIBUTING.md)

- `docs/Systems.md` — new § Rate filters section (after § Disabling a System at line 174). Show both methods, code example, callout: "gates compose (enabled + interval + rate); upstream C flecs forbids combining interval + rate but Go-flecs allows AND composition because there is no tick_source abstraction." Add corresponding example in `docs/systems_examples_test.go`.
- `docs/README.md:144` — flip to shipped marker matching format used at line 143, 145, 147.
- `README.md` — feature list bump.
- `CHANGELOG.md` — v0.61.0 entry at line 3 (above v0.60.0).
- `ROADMAP.md` — line 3 heading bump to "through v0.61.0"; add Phase 16.6 bullet alongside the v0.61.0 entry, matching the Phase 16.5 style at line 64.

### Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%

## Explicit non-goals

- No RunWorker (gap `docs/README.md:146` — separate phase).
- No DependsOn ordering between systems (gap `docs/README.md:142` — separate phase; needs entity-side machinery).
- No custom pipeline phases (gap `docs/README.md:141` — separate phase).
- No `tick_source` abstraction (upstream's chained tick-source model is out of scope; Go-flecs uses per-system filters only).
- No pipeline pause/resume (not a gap).

## Open decision points

1. **Combination semantics: AND vs reject.** Upstream C flecs (`src/addons/system/system.c:230-235`) rejects systems with both interval and rate set. **Recommend AND**: gates compose; no `tick_source` chaining in Go-flecs means the upstream reasoning ("ambiguous which timer ticks first") does not apply. Document divergence in Systems.md callout.
2. **Disabled-system counter advance: do NOT advance.** Locks in "re-enable doesn't catch up" semantic. Recommended.
3. **Interval accumulator: subtract-with-cap.** Matches upstream `timer.c:33-35` exactly. Preserves catch-up accuracy across normal ticks; caps runaway accumulation if a single dt vastly exceeds the interval. Recommended over plain subtract or reset-to-zero.
4. **`time.Duration` vs `float64` seconds for interval.** Upstream uses `ecs_ftime_t` (double seconds). **Recommend `time.Duration`** — Go-idiomatic; conversion to/from `dt float32` is explicit at the gate boundary. Document the conversion.
5. **`int32` vs `int` for rate.** Recommend `int32` to match upstream's type. Avoids surprises on 32-bit targets.

## Process

- Feature, not bug.
- Label: `snichols/queued`.
