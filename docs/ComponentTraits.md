# Component Traits

<!-- TODO: port from /work/agents/claude/projects/SanderMertens/flecs/docs/ComponentTraits.md (Phase 14.8) -->

This manual covers component traits: special pair relationships that alter how a component behaves. The Go port currently implements:

- **Inheritable** — `SetInheritable[T](w)` / `w.SetInheritable(cid)`: auto-promotes query terms to `Self|Up(IsA)`.
- **OnInstantiate / Inherit / Override / DontInherit** trait IDs — available via `w.OnInstantiate()`, `w.Inherit()`, `w.Override()`, `w.DontInherit()`; full behavior pending.

### Feature gaps vs. upstream C

The following C traits are **not yet ported** to Go (see [README.md](README.md) feature-gap list):

- `EcsSparse` — opt-in sparse storage.
- `EcsSymmetric`, `EcsTransitive`, `EcsExclusive`, `EcsUnion` — relationship traits.
- `EcsOnDelete` / `EcsOnDeleteTarget` cleanup actions.
- `EcsAlerts`, `EcsMonitor`, `EcsUnits` addon traits.

See the [Quickstart](Quickstart.md) for a hands-on introduction to the traits that are currently available.
