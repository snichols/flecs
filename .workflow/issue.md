## Goal

Port `docs/ObserversManual.md` from upstream flecs to Go-native form, continuing the docs port stream (Phases 14.0-14.6 shipped, v0.19.0-v0.25.0). This phase covers the reference on event-driven reactions to component changes: `OnAdd`/`OnSet`/`OnRemove` hooks (single-subscriber, Phase 5.1) and multi-subscriber observers (Phase 5.2).

Upstream source: `/work/agents/claude/projects/SanderMertens/flecs/docs/ObserversManual.md` (~8600 words, `port-adapted`, effort: large).

**Target version: v0.26.0.**

### Critical context — observers infrastructure

- **Hooks** (Phase 5.1, single-subscriber per component+event): `OnAdd[T]`, `OnSet[T]`, `OnRemove[T]`. Callbacks receive `(*Writer, ID, T)` since Phase 12.0.
- **Observers** (Phase 5.2, multi-subscriber): `Observe[T]`, `ObserveID`, `Observe2[T]`, `Observer.Unsubscribe()`. Deferred unsubscribe at the node.
- **C-flecs distinction**: C has only one "observer" concept; ours splits into hooks (single-sub, faster path) and observers (multi-sub). Document this clearly.

### Likely gaps from this doc

- `OnDelete` / `OnDeleteTarget` events (deletion-time observers — C feature)
- `OnTableEmpty` / `OnTableFill` events (archetype-population events)
- `OnReplace` event (fires when Set replaces an existing value) — already on the gap list from 14.1
- Custom event types (define your own event entity)
- Observer filters (term-set filter beyond a single component)
- Yield-on-create (Observer fires retroactively for matching existing entities)
- Multi-event observers
- Observer chaining / propagation control

### Deliverables

1. **Full port of `docs/ObserversManual.md`** adapted to Go:
   - Lead with hooks: simplest event reaction, one callback per (component, event) pair.
   - Then observers: multiple subscribers, deferred unsubscribe, `Observer.Unsubscribe()`.
   - Cover `OnAdd`/`OnSet`/`OnRemove` semantics — when each fires, what value is passed (post-Set value, pre-Remove value).
   - Show observer use cases: validation, replication, indexing.
   - Cover the `*Writer` callback parameter and the "safe to mutate inside a callback" guarantee from Phase 12.0.
   - For features we don't have, use "Not yet ported in Go flecs" callouts: OnDelete, OnTableEmpty/Fill, OnReplace, custom events, term-set observer filters, yield-on-create.

2. **Verify code blocks.** Create `docs/observers_examples_test.go` with `TestObservers_*` functions. Hook/observer tests should fire and capture into a slice or counter to assert behavior.

3. **Update `docs/README.md`**: ObserversManual row → `landed / 14.7`. Append discovered gaps. Expect 4-8 new gaps (custom events, yield-on-create, term-set filters, observer propagation, etc.).

4. **Update `ROADMAP.md`**: 14.7 row → `shipped (v0.26.0)`. Do NOT bump the heading.

5. **Update `CHANGELOG.md`** with `Unreleased — Phase 14.7: ObserversManual doc port (upcoming v0.26.0)`.

6. **Cross-link** with Systems (callbacks vs. observers), Queries (term-set filtering), and EntitiesComponents (hooks).

### Non-goals

- No source changes.
- No porting beyond ObserversManual.

### Mechanical acceptance

- `go test ./docs/...` passes.
- `go vet`, `golangci-lint` clean.
- `go test ./... -race -count=1` passes.
- `docs/README.md` shows ObserversManual as landed.

### Style notes

- Native Go. Lead with the simplest hook, then escalate to observers.
- Clearly distinguish hooks (single-sub, faster) from observers (multi-sub, more flexible).
- Show the `*Writer` parameter as the safe-mutation channel.
- Cover the Unsubscribe semantics (deferred-at-node) for observers.

## Constraints

- @CONTRIBUTING.md — governs the Documentation policy; code blocks must compile against v0.25.0.
- @docs/ObserversManual.md — stub to be replaced with the full port.
- @docs/Systems.md — tone reference; cross-link callbacks vs. observers.
- @docs/EntitiesComponents.md — tone reference; cross-link hooks discussion.
- @docs/Queries.md — tone reference; cross-link term-set filtering.
- @docs/README.md — update ObserversManual row to landed/14.7 and append discovered gaps.
- @docs/systems_examples_test.go — recent test pattern to mirror for `observers_examples_test.go`.
- @hooks.go — single-sub `OnAdd`/`OnSet`/`OnRemove` API surface.
- @observer.go — multi-sub `Observe`/`ObserveID`/`Observe2` and `Observer.Unsubscribe()`.
- @scope.go — `*Writer` callback parameter signature; safe-mutation channel.
- @doc.go — package-level doc; keep cross-links coherent.
- @README.md — landing page; keep references coherent.
- @ROADMAP.md — mark 14.7 row shipped (v0.26.0); do NOT bump heading.
- @CHANGELOG.md — add Unreleased entry for Phase 14.7 (upcoming v0.26.0).
- Upstream source: `/work/agents/claude/projects/SanderMertens/flecs/docs/ObserversManual.md` (~8600 words, port-adapted, effort: large).
