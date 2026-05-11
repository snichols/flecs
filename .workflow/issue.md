## Goal

Add an optional `log/slog`-compatible logger that flecs uses to emit lifecycle events. Users opt in with `(*World).SetLogger(*slog.Logger)`; when no logger is installed (the default), logging is a single nil-pointer compare at each event site.

After this lands:

```go
w := flecs.New()
w.SetLogger(slog.Default())  // OR slog.New(slog.NewJSONHandler(os.Stderr, nil))

posID := flecs.RegisterComponent[Position](w)
// -> DEBUG msg="component registered" name=pkg.Position id=<n> size=<n>

e := w.NewEntity()
// -> DEBUG msg="entity created" id=<n>

flecs.Set[Position](w, e, Position{1, 2})
// -> DEBUG msg="component added" entity=<n> component=pkg.Position
// (No log on the in-place Set after; the lifecycle is "added", not "set")
```

This phase implements ONLY:

- A `(*World).SetLogger(*slog.Logger)` method.
- Internal log calls at LIFECYCLE points: entity created, entity deleted, component registered, table created, system added, system closed, observer registered, observer unsubscribed, snapshot serialized, snapshot loaded.
- All logs at **DEBUG level** by default. Users filter via slog's leveler.
- No log on hot paths (no per-Set log, no per-iteration log).

This phase does NOT implement:

- Per-Set / per-Get / per-iter logs (too noisy; structured events are for lifecycle, not hot-path tracing).
- Custom log levels per event type.
- Sampling.
- Log-driven debugging features (replay, etc.).
- Wrapping `log/slog` — use it directly.

C reference (cite paths — read but do not paraphrase):

- `/work/agents/claude/projects/SanderMertens/flecs/src/addons/log.c` — C analog. Note: the C version is much more elaborate (color, levels, hooks); we're shipping the minimal slog wrapper.

### Deliverables

1. **New unexported field on `*World`:** `logger *slog.Logger`. Defaults to `nil`. When `nil`, no log calls fire (cheap nil-check at each event site).

2. **`func (w *World) SetLogger(logger *slog.Logger)`** — installs or replaces the logger. Passing `nil` disables logging. Document.

3. **`func (w *World) Logger() *slog.Logger`** — returns the current logger (or nil). Mostly for tests.

4. **Event sites and message format:**
   - **Entity created** (`NewEntity`): `Debug(\"entity created\", \"id\", uint64(e))` — log AFTER allocation.
   - **Entity deleted** (`Delete`, after cascade resolution): `Debug(\"entity deleted\", \"id\", uint64(e))` — fired once per entity in the cascade, AT the per-entity delete site (after `deleteOne` completes for each).
   - **Component registered** (`RegisterComponent[T]` first time): `Debug(\"component registered\", \"name\", info.Name, \"id\", uint64(id), \"size\", uint64(info.Size))` — only on FIRST registration; subsequent calls are no-ops.
   - **Table created** (`migrate`'s find-or-create-miss branch + `World.New`'s empty table): `Debug(\"table created\", \"signature_len\", len(t.Type()), \"signature\", formatted-id-list)` — the signature should be formatted as a space-separated list of decimal IDs or as JSON-array `[1,2,3]`. Implementer's call; document.
   - **System added** (`NewSystem` / `NewSystemInPhase`): `Debug(\"system added\", \"phase\", phaseName(w, s.phase))` — phaseName is a small helper that returns \"PreUpdate\" / \"OnUpdate\" / etc.
   - **System closed** (`(*System).Close`, only on the FIRST Close that actually marks it removed): `Debug(\"system closed\")` — no ID since systems aren't entities.
   - **Observer registered** (`Observe[T]` / `ObserveID` / `Observe2`): `Debug(\"observer registered\", \"id\", uint64(id), \"event\", eventKindString)` — `event` is `EventOnAdd`/`EventOnSet`/`EventOnRemove`.
   - **Observer unsubscribed** (`(*Observer).Unsubscribe`, only on the FIRST Unsubscribe): `Debug(\"observer unsubscribed\")`.
   - **Snapshot save/load** (`MarshalJSON`/`UnmarshalJSON`): `Debug(\"snapshot serialized\", \"entities\", n)` after Marshal; `Debug(\"snapshot loaded\", \"entities\", n)` after Unmarshal.

5. **NO logs on:**
   - Get/Has/Owns/Field/Each*/EachEntity/Stats/etc. (read-only paths).
   - Set/Add/Remove (per-call lifecycle is the migrate's \"table created\", which already fires).
   - Defer Begin/End/Flush (would be too noisy on per-frame use).
   - Progress (would log per-frame, way too noisy).
   - Hook/Observer dispatch (already user-observable via Observe).

6. **Helper internal function:**
   - `(*World).log(level slog.Level, msg string, attrs ...slog.Attr)` — wraps `if w.logger != nil { w.logger.LogAttrs(ctx, level, msg, attrs...) }`. Use `context.Background()` since we don't propagate user context. Implementer's call: alternatively use the simpler `(*World).debug(msg string, args ...any)` that calls `w.logger.Debug` — cleaner but slightly more overhead per call (variadic).

7. **Tests** in `log_test.go`:
   - **Default (no logger):** create world, perform operations; no panic, no logs leaked.
   - **SetLogger then unset:** `SetLogger(slog.Default())`; `SetLogger(nil)`; no log fires.
   - **Per-event coverage:** use a custom `slog.Handler` that captures records; perform each event type listed above; verify the expected message + attributes.
     - For \"entity created\": create entity; capture; assert msg + id.
     - For \"entity deleted\": create + delete; assert two records (the delete log fires).
     - For \"component registered\": `RegisterComponent[Position]`; assert msg + name + id + size.
     - For \"table created\": create entity with [Position]; assert table-created log fires at least once.
     - For \"system added\": `NewSystem(w, q, fn)`; assert.
     - For \"system closed\": `s.Close()`; assert.
     - For \"observer registered\": `Observe[Position]`; assert.
     - For \"observer unsubscribed\": `o.Unsubscribe()`; assert.
     - For \"snapshot serialized\" / \"snapshot loaded\": Marshal/Unmarshal; assert.
   - **Logs honor slog.Level filter:** install a handler at INFO level; perform operations; no DEBUG-level logs leak.
   - **No log on read paths:** install capturing handler; perform Get/Has/Field/Each; assert zero records.
   - **No log on hot paths:** install handler; do 1000 Set calls on the same entity (no migration); assert no \"set\" record (the \"component added\" only fires once for the first migration).
   - **Existing tests stay green.**

8. **Documentation:**
   - Godoc on `SetLogger` explains the lifecycle event surface (no hot-path logs).
   - doc.go: new \"Structured Logging\" section with a code snippet.
   - CHANGELOG entry under Unreleased.
   - README feature table updated.

9. **Mechanical acceptance:**
   - `go test ./... -race -count=2` passes.
   - `go vet ./...` clean.
   - `golangci-lint run` clean.
   - Coverage on `flecs` >= 90% (no regression from 95.8%).
   - All exported symbols have godoc.
   - No new third-party deps (stdlib `log/slog` only).
   - **Performance:** nil-logger fast path must be < 1 ns per event site. The `if w.logger != nil` check is a single pointer compare; the implementer should verify this doesn't measurably regress benchmarks. Run `BenchmarkNewEntity` and `BenchmarkSetExistingComponent` before/after; report deltas in BENCH.md if any.

### Non-goals

- NO per-Set / per-Get / per-iter logs.
- NO custom level per event.
- NO sampling.
- NO log to ECS observer dispatch (logs are separate from observers).
- NO color / formatting beyond what slog provides.
- NO wrapped logger interface (use `*slog.Logger` directly).
- NO per-system or per-query logger contexts.

### Constraints / pointers for the implementer

- `log/slog` is stdlib since Go 1.21. We're on 1.26.1. No deps needed.
- The nil-logger check must be inlined; don't put it inside a helper that doesn't get inlined. Use a direct `if w.logger != nil` at each site, OR a method that the Go inliner can elide on the nil branch.
- Use `slog.LogAttrs` (not `slog.Debug`) for slightly lower allocation overhead. Use `context.Background()`.
- Attribute keys should be lowercase, no spaces — match slog convention.
- Phase names for the system-added log: use the existing phase IDs from `World.PreUpdate()`/`OnUpdate()`/etc. Map to strings via a small switch.
- DO NOT modify any existing public API surface beyond adding the two methods.
- DO NOT import third-party deps.
- DO NOT log from internal packages directly — only from root flecs package. Internal packages don't have World access; they shouldn't log.
- The \"table created\" event in `World.New` fires once for the empty table; for the seven built-in entities allocated during New, the entity-created event fires once each. This is unavoidable noise — document that built-in allocation produces these logs.

## Constraints

- @world.go — NewEntity / Delete / New / migrate event sites
- @system.go — NewSystem / Close event sites; phase name mapping
- @observer.go — Observe / ObserveID / Observe2 / Unsubscribe event sites; event kind string
- @marshal.go — MarshalJSON / UnmarshalJSON snapshot event sites
- @id.go — id formatting helpers used in signature attribute
- @internal/component/registry.go — Register hook for first-time-registration log
- @doc.go — package overview gets a new \"Structured Logging\" section
- @CHANGELOG.md — Unreleased entry
- @README.md — feature table update
- Stdlib only: use `log/slog` directly; no new third-party deps.
- Nil-logger fast path must remain < 1 ns per event site; benchmarks `BenchmarkNewEntity` and `BenchmarkSetExistingComponent` must not regress measurably.
- Only the root `flecs` package logs; internal packages do not receive a logger.
