# Component Traits

Component traits are tags and pairs that can be added to components and relationships to modify their behavior. In the C flecs library these map to built-in entity IDs (e.g. `EcsSparse`, `EcsTransitive`). In the Go port, most traits are not yet implemented — this document is an honest map of what works today and what does not.

See the [Quickstart](Quickstart.md) for an introductory overview, [Relationships](Relationships.md) for pair encoding, [PrefabsManual](PrefabsManual.md) for IsA / instantiation context, and the [Queries manual](Queries.md) for traversal flags.

## Table of contents

- [Trait model](#trait-model)
- [Implemented traits](#implemented-traits)
  - [Inheritable trait](#inheritable-trait)
  - [OnInstantiate trait](#oninstantiate-trait)
- [Unimplemented traits](#unimplemented-traits)
  - [Acyclic](#acyclic)
  - [CanToggle](#cantoggle)
  - [Cleanup traits (OnDelete / OnDeleteTarget)](#cleanup-traits-ondelete--ondeletetarget)
  - [Constant](#constant)
  - [DontFragment](#dontfragment)
  - [Exclusive](#exclusive)
  - [Final](#final)
  - [OneOf](#oneof)
  - [OrderedChildren](#orderedchildren)
  - [PairIsTag](#pairistaag)
  - [Reflexive](#reflexive)
  - [Relationship / Target / Trait](#relationship--target--trait)
  - [Singleton](#singleton)
  - [Sparse](#sparse)
  - [Symmetric](#symmetric)
  - [Transitive](#transitive)
  - [Traversable](#traversable)
  - [Union](#union)
  - [With](#with)
- [Trait system roadmap](#trait-system-roadmap)

---

## Trait model

In C flecs, traits are special entity IDs added to component or relationship entities as tags or pairs. For example, `ecs_add_id(world, ecs_id(Position), EcsSparse)` adds the `Sparse` trait to the `Position` component entity, making that component use sparse storage for all entities.

The Go port exposes the same conceptual model: every registered component is itself an entity (its ID is the value returned by `RegisterComponent[T]`), and built-in trait entity IDs are accessible via `World` accessor methods (`w.OnInstantiate()`, `w.Inherit()`, `w.Override()`, `w.DontInherit()`). Adding a trait to a component entity would look like `fw.AddID(posID, traitID)` — the pair encoding is handled by `MakePair` when the trait is a pair.

The **Inheritable** trait and the full **OnInstantiate** family (`Inherit`, `Override`, `DontInherit`) are wired up end to end. All other traits are planned but not implemented; their sections below document what they would do and note any available workaround.

---

## Implemented traits

### Inheritable trait

The `Inheritable` trait signals that queries for a component should automatically follow the `IsA` relationship upward (prefab inheritance). When a component is marked inheritable, every query term for that component is promoted to `Self|Up(IsA)` at construction time — matching entities that own the component locally *and* entities that inherit it from a prefab.

**API:**

```go
// Mark by generic type (T must already be registered):
flecs.SetInheritable[T](w)

// Or mark by component entity ID:
w.SetInheritable(cid)
```

**Example — query matches entity inheriting from prefab:**

```go
package docs_test

import (
	"testing"
	"github.com/snichols/flecs"
)

func TestComponentTraits_InheritableQueryMatchesPrefab(t *testing.T) {
	type Mass struct{ Value float32 }

	w := flecs.New()
	flecs.RegisterComponent[Mass](w)
	flecs.SetInheritable[Mass](w)

	var base, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		base = fw.NewEntity()
		flecs.Set(fw, base, Mass{Value: 100})

		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), base))
	})

	var found []flecs.ID
	w.Read(func(r *flecs.Reader) {
		flecs.Each1[Mass](r, func(e flecs.ID, _ *Mass) {
			found = append(found, e)
		})
	})

	sawInst := false
	for _, e := range found {
		if e == inst {
			sawInst = true
		}
	}
	if !sawInst {
		t.Fatalf("Each1[Mass] did not yield inheriting instance (got %v)", found)
	}
}
```

**Behavior notes:**

- `SetInheritable` must be called **before** any query referencing that component is constructed. Calling it after a query is built leaves the query's traversal mode unchanged.
- `Get`/`Has` already walk the IsA chain on a local miss, regardless of `SetInheritable`. The flag affects only query-level matching.
- Explicit traversal modifiers (`.Self()`, `.Up()`, `.SelfUp()`, `.Cascade()`) on a query term suppress auto-promotion.

---

### OnInstantiate trait

The `OnInstantiate` trait configures how a component behaves when an entity is instantiated from a prefab (i.e., when an `(IsA, prefab)` pair is added to an entity). Three target entity IDs control the behavior:

| Target       | Accessor          | Description |
|---|---|---|
| `Inherit`    | `w.Inherit()`     | Inherit component from prefab without copying |
| `Override`   | `w.Override()`    | Copy component to instance at instantiation (C default) |
| `DontInherit`| `w.DontInherit()` | Do not inherit and do not copy |

These entity IDs are accessible in Go and can be combined with the `OnInstantiate` relationship ID (`w.OnInstantiate()`) to form pairs:

```go
package docs_test

import (
	"testing"
	"github.com/snichols/flecs"
)

func TestComponentTraits_OnInstantiateIDsNonZero(t *testing.T) {
	w := flecs.New()
	if w.OnInstantiate() == 0 {
		t.Error("OnInstantiate ID should be non-zero")
	}
	if w.Inherit() == 0 {
		t.Error("Inherit ID should be non-zero")
	}
	if w.Override() == 0 {
		t.Error("Override ID should be non-zero")
	}
	if w.DontInherit() == 0 {
		t.Error("DontInherit ID should be non-zero")
	}
}
```

**Current Go port status:**

- `w.Inherit()` — the ID exists. Go flecs implements query-time and `Get`/`Has`-time inheritance via `SetInheritable[T]` and the IsA chain walk. The `(OnInstantiate, Inherit)` pair form is also accepted by `AddID` / `SetInstantiatePolicy` and round-trips via `GetInstantiatePolicy`.
- `w.Override()` _(v0.33.0)_ — eagerly copies the component from the prefab into each new instance at `(IsA, prefab)` add time. Use `SetInstantiatePolicy(w, cid, w.Override())` or the pair form `fw.AddID(cid, MakePair(w.OnInstantiate(), w.Override()))`. See [PrefabsManual — Override](PrefabsManual.md#override-shipped-v0330).
- `w.DontInherit()` _(v0.33.0)_ — prevents a component from being visible on instances via the IsA chain (`Has`/`Get` return false/zero) and suppresses query auto-promotion even when `SetInheritable[T]` was called. Use `SetInstantiatePolicy(w, cid, w.DontInherit())`. See [PrefabsManual — DontInherit](PrefabsManual.md#dontinherit-shipped-v0330).

---

## Unimplemented traits

The following sections describe traits that exist in C flecs but are not yet ported to the Go library. Each section explains what the trait does, what the closest available workaround is (if any), and links to the relevant gap entry.

---

### Acyclic

**What it does:** Adding `Acyclic` to a relationship tells the storage that the relationship cannot contain cycles. Both `ChildOf` and `IsA` are implicitly acyclic. When `Acyclic` is set, the engine can detect and error on accidental cycles during development.

**Workaround:** No general mechanism. `ChildOf` and `IsA` are hardcoded to be traversed safely; custom relationships have no cycle detection.

> **Not yet ported in Go flecs.** See the [feature-gap list](README.md#feature-gap-list): *Transitive relationships / Traversable relationship trait*.

---

### CanToggle

**Shipped in v0.35.0.**

**What it does:** The `CanToggle` trait enables per-entity component enable/disable. Toggling is cheaper than remove/add because it flips a bit in a per-entity bitset rather than migrating the entity to a different archetype table. Disabled components are excluded from queries but their value is preserved; re-enabling restores normal query visibility instantly.

**Go API:**

```go
posID := flecs.RegisterComponent[Position](w)

// Mark the component as toggleable (call once, before any Enable/Disable).
flecs.SetCanToggle(w, posID)
// Equivalent bare-tag form:
// w.Write(func(fw *flecs.Writer) { fw.AddID(posID, w.CanToggle()) })

// Inspect the policy:
flecs.IsCanToggle(w, posID) // → true

w.Write(func(fw *flecs.Writer) {
    // Disable — the component value is preserved; Has still returns true.
    flecs.DisableID(fw, e, posID)
    // Typed variant:
    // flecs.Disable[Position](fw, e)

    // Re-enable — next Each1 iteration will visit the entity again.
    flecs.EnableID(fw, e, posID)
    // flecs.Enable[Position](fw, e)
})

w.Read(func(fr *flecs.Reader) {
    flecs.IsEnabledID(fr, e, posID)    // → true / false
    flecs.IsEnabled[Position](fr, e)   // typed variant
})

// Each1 / Each2 / Each3 / Each4 automatically skip disabled rows:
flecs.Each1[Position](r, func(e flecs.ID, p *Position) {
    // only called when Position is enabled for e
})
```

**Behaviour notes:**
- `Has` returns `true` regardless of enabled/disabled state — the component is still on the entity.
- Disabling/enabling does **not** move the entity between archetype tables.
- The disabled state survives archetype migration (e.g., adding an unrelated component).
- Disabling/enabling bumps `Table.ChangeCount()`, so cached-query change detection works correctly.
- `EnableID`/`DisableID` panic if the component is not marked `CanToggle` or if the entity does not own the component.

---

### Cleanup traits (OnDelete / OnDeleteTarget)

**Shipped in v0.32.0.**

Cleanup traits specify what happens when a component, tag, or relationship entity is deleted, or when a target used in a relationship pair is deleted.

Two **conditions**:
- `w.OnDelete()` — fires when the component/tag/relationship entity itself is deleted.
- `w.OnDeleteTarget()` — fires when a target entity used in a pair for this relationship is deleted.

Three **actions**:
- `w.RemoveAction()` (default) — remove the component/pair from source entities.
- `w.DeleteAction()` — delete all source entities that have the component/pair.
- `w.PanicAction()` — panic with a descriptive message. The world is left in a halted state; no recovery is attempted.

**Go API:**

```go
w := flecs.New()

var likesID flecs.ID
w.Write(func(fw *flecs.Writer) { likesID = fw.NewEntity() })

// Delete all "likers" when the liked target is deleted:
flecs.SetCleanupPolicy(w, likesID, w.OnDeleteTarget(), w.DeleteAction())

// Or equivalently via pair-add:
w.Write(func(fw *flecs.Writer) {
    fw.AddID(likesID, flecs.MakePair(w.OnDeleteTarget(), w.DeleteAction()))
})

// Read back the registered policy:
action, ok := flecs.GetCleanupPolicy(w, likesID, w.OnDeleteTarget())
// action == w.DeleteAction(), ok == true
```

`ChildOf` has `(OnDeleteTarget, DeleteAction)` registered at bootstrap — this drives the parent-cascade-delete behavior. `IsA` has **no** default policy (matching C); see [PrefabsManual § Protecting prefabs](PrefabsManual.md) for the opt-in recipe.

---

### Constant

**What it does:** The `Constant` trait marks a component as read-only after it has been added. Attempting to `Set` a constant component after its initial value has been written would be a fatal error.

**Workaround:** None — enforce read-only invariants manually in application code.

> **Not yet ported in Go flecs.** This trait is not listed in the upstream C `ComponentTraits.md` but was identified during Phase 14.8 as a gap. See the [feature-gap list](README.md#feature-gap-list).

---

### DontFragment

**What it does:** The `DontFragment` trait uses the same sparse storage as `Sparse` but avoids archetype table fragmentation. It is especially useful for components that are added to only a small fraction of entities — without this trait those entities would each occupy their own archetype table with a handful of rows.

Components with `DontFragment` do not appear in archetype types and do not trigger monitor observers.

**What the C API looks like:**

```c
// C — not available in Go flecs
ecs_add_id(world, ecs_id(Position), EcsDontFragment);
```

**Workaround:** None. All components in Go flecs use standard archetype-based SoA storage. Sparse or lightly-populated components will produce many small tables.

> **Not yet ported in Go flecs.** See the [feature-gap list](README.md#feature-gap-list).

---

### Exclusive

**What it does:** An exclusive relationship enforces that an entity can have at most one target for a given relationship. Adding a second target automatically replaces the first. The built-in `ChildOf`, `OnDelete`, `OnDeleteTarget`, and `OnInstantiate` relationships are exclusive by default. `IsA` is NOT exclusive — multiple prefab bases per entity are permitted.

**Go API (shipped v0.34.0):**

```go
w := flecs.New()

var marriedTo, bob, alice, carol flecs.ID
w.Write(func(fw *flecs.Writer) {
    marriedTo = fw.NewEntity()
    bob = fw.NewEntity()
    alice = fw.NewEntity()
    carol = fw.NewEntity()
})

// Mark the relationship exclusive.
flecs.SetExclusive(w, marriedTo)
// Equivalent bare-tag form:
// w.Write(func(fw *flecs.Writer) { fw.AddID(marriedTo, w.Exclusive()) })

w.Write(func(fw *flecs.Writer) {
    fw.AddID(bob, flecs.MakePair(marriedTo, alice))
})
w.Write(func(fw *flecs.Writer) {
    fw.AddID(bob, flecs.MakePair(marriedTo, carol)) // replaces (marriedTo, alice)
})

// flecs.IsExclusive(w, marriedTo) == true
```

The replace-on-add path fires `OnRemove` for the old pair and `OnAdd` for the new pair via the standard hook/observer machinery — no special handling is needed in observer callbacks.

---

### Final

**What it does:** The `Final` trait prevents an entity from being used as the target of an `IsA` relationship, similar to a `final` class in object-oriented languages. Queries may use this to optimize traversal: they do not need to explore subsets of a final entity.

**What the C API looks like:**

```c
// C — not available in Go flecs
ecs_entity_t e = ecs_new(world);
ecs_add_id(world, e, EcsFinal);

ecs_entity_t child = ecs_new(world);
ecs_add_pair(world, child, EcsIsA, e); // would panic with Final
```

**Workaround:** None — the Go port does not enforce this constraint. Any entity can be used as an `IsA` target.

> **Not yet ported in Go flecs.**

---

### OneOf

**What it does:** `OneOf` constrains the target of a relationship to be a child of a specified entity (or of the relationship itself). This is commonly used for enum-style relationships where valid values are known children of a parent entity.

**What the C API looks like:**

```c
// C — not available in Go flecs
ecs_entity_t Food = ecs_new(world);
ecs_add_id(world, Food, EcsOneOf);

ecs_entity_t Apples = ecs_new_w_pair(world, EcsChildOf, Food);
ecs_entity_t Fork   = ecs_new(world);

ecs_entity_t a = ecs_new_w_pair(world, Food, Apples); // OK
ecs_entity_t b = ecs_new_w_pair(world, Food, Fork);   // panic — Fork not child of Food
```

**Workaround:** Enforce target validity manually in application code before adding the pair.

> **Not yet ported in Go flecs.**

---

### OrderedChildren

**What it does:** The `OrderedChildren` trait guarantees that `EachChild` iterates children in insertion order, even when component mutations move children between archetype tables.

**Workaround:** In Go flecs, `EachChild` iterates children in the order they appear in their archetype tables. For most workloads this is insertion order, but it is not guaranteed when children have different component compositions and are moved between tables.

> **Not yet ported in Go flecs.** See the [feature-gap list](README.md#additional-gaps-discovered-in-phase-144-hierarchiesmanual-port): *`OrderedChildren` trait*.

---

### PairIsTag

**What it does:** When added to a relationship, `PairIsTag` forces all pairs with that relationship to behave as tags — no component data is stored, even if the pair's target is a data-bearing component. This avoids a common pitfall where `(Serializable, Position)` accidentally stores a second copy of `Position` data.

**What the C API looks like:**

```c
// C — not available in Go flecs
ECS_TAG(world, Serializable);
ecs_add_id(world, Serializable, EcsPairIsTag);

// Now (Serializable, Position) is a tag — no Position data stored
ecs_add_pair(world, e, Serializable, ecs_id(Position));
```

**Workaround:** Use a zero-size struct as the relationship first element to ensure no data storage. In Go flecs all relationships that use a zero-size struct type as the first pair element already store no data naturally.

> **Not yet ported in Go flecs.** See the [feature-gap list](README.md#additional-gaps-discovered-in-phase-143-relationships-port): *PairIsTag trait*.

---

### Reflexive

**What it does:** A reflexive relationship makes `Has(e, R, e)` evaluate to true — every entity implicitly has the relationship to itself. The built-in `IsA` is reflexive: `IsA(Tree, Tree)` is true even if no such pair was explicitly added.

**Workaround:** None in the query engine. Application code can treat `Has(e, R, target)` as true when `e == target` for relationships that should be reflexive.

> **Not yet ported in Go flecs.** See the [feature-gap list](README.md#feature-gap-list).

---

### Relationship / Target / Trait

**What they do:**
- `Relationship` enforces that an entity can only be used as the first element (relationship side) of a pair. Adding it as a plain tag or as a pair target will panic.
- `Target` enforces that an entity can only be used as the second element (target side) of a pair.
- `Trait` marks an entity as a trait so that some `Relationship` constraints are relaxed (trait entities are still allowed as pair targets even when the relationship has the `Relationship` flag).

**Workaround:** None — the Go port does not enforce these usage constraints.

> **Not yet ported in Go flecs.**

---

### Singleton

**What it does:** A singleton component can only be added to the entity that represents the component itself (i.e., the component entity is also the storage entity). Queries for a singleton component automatically use the component entity as their source rather than `$this`. This provides a convenient world-global component without requiring a dedicated entity ID.

**What the C API looks like:**

```c
// C — not available in Go flecs
ecs_add_id(world, ecs_id(TimeOfDay), EcsSingleton);
ecs_singleton_set(world, TimeOfDay, {.value = 0});
// Queries for TimeOfDay automatically match the singleton
```

**Workaround in Go flecs:** Use a dedicated entity to store the singleton component, and use a fixed-source query term via `With(id).Up(rel)` pointing at that entity. Because fixed-source query terms are also not fully implemented, the simplest workaround is to store the singleton on a well-known entity and retrieve it by entity ID:

```go
// Approximate singleton pattern in Go flecs
type TimeOfDay struct{ Value float32 }

w := flecs.New()
flecs.RegisterComponent[TimeOfDay](w)

var clockEntity flecs.ID
w.Write(func(fw *flecs.Writer) {
    clockEntity = fw.NewEntity()
    flecs.Set(fw, clockEntity, TimeOfDay{Value: 0})
})

// Retrieve by known entity ID — no query needed
w.Read(func(r *flecs.Reader) {
    tod, ok := flecs.Get[TimeOfDay](r, clockEntity)
    _ = tod
    _ = ok
})
```

> **Not yet ported in Go flecs.** See the [feature-gap list](README.md#feature-gap-list): *Singleton API shortcuts*.

---

### Sparse

**What it does:** The `Sparse` trait stores a component outside the archetype SoA tables in a per-component sparse map. Sparse components have stable pointers (not invalidated by archetype migrations) and trade query throughput for O(1) add/remove cost. In C++, non-movable types are automatically marked sparse.

**Workaround:** None — Go flecs uses archetype-based SoA storage for all components. Component pointers are not stable across operations that trigger archetype migration.

> **Not yet ported in Go flecs.** See the [feature-gap list](README.md#feature-gap-list): *Sparse storage*.

---

### Symmetric

**What it does:** When a symmetric relationship `(R, Y)` is added to entity `X`, the relationship `(R, X)` is automatically added to entity `Y`. Removal is mirrored the same way. Useful for bidirectional relationships such as `AlliesWith`, `MarriedTo`, or `TradingWith`.

**What the C API looks like:**

```c
// C — not available in Go flecs
ecs_entity_t MarriedTo = ecs_new_w_id(world, EcsSymmetric);
ecs_add_pair(world, Bob, MarriedTo, Alice); // also adds (MarriedTo, Bob) to Alice
```

**Workaround:** Add both sides of the pair manually:

```go
// Go workaround
w.Write(func(fw *flecs.Writer) {
    fw.AddID(bob, flecs.MakePair(marriedTo, alice))
    fw.AddID(alice, flecs.MakePair(marriedTo, bob))
})
```

> **Not yet ported in Go flecs.** See the [feature-gap list](README.md#additional-gaps-discovered-in-phase-143-relationships-port): *Symmetric relationship trait*.

---

### Transitive

**What it does:** A transitive relationship allows queries to follow a chain automatically. If `(R, B)` is on entity `A` and `(R, C)` is on entity `B`, then a query for `(R, C)` will also match `A`. The built-in `IsA` already behaves transitively for `Get`/`Has`; `Transitive` generalises this to arbitrary custom relationships and to the query engine.

**What the C API looks like:**

```c
// C — not available in Go flecs
ecs_add_id(world, LocatedIn, EcsTransitive);
// Now (LocatedIn, USA) matches Manhattan even though only NewYork has it directly.
```

**Workaround:** For `IsA`, transitivity already works via `Get`/`Has` chain walking. For custom relationships, there is no query-engine equivalent — application code must perform manual traversal (e.g. using `TargetUp` / `GetUp[T]` helpers with a custom relationship).

> **Not yet ported in Go flecs.** See the [feature-gap list](README.md#additional-gaps-discovered-in-phase-143-relationships-port): *Transitive relationship trait*.

---

### Traversable

**What it does:** Formally marks a relationship as safe to traverse in queries (`Up`, `SelfUp`, `Cascade` flags). In C flecs only `Traversable` relationships can be used for query traversal; adding this also implies `Acyclic`. In Go flecs, any entity can currently be used as a traversal relationship without explicit registration.

> **Not yet ported in Go flecs** (the formal constraint is not enforced). See the [feature-gap list](README.md#additional-gaps-discovered-in-phase-143-relationships-port): *Traversable relationship trait*.

---

### Union

**What it does:** The `Union` trait opts a relationship into union-pair semantics, where only one of several possible relationship values can be active for an entity at a time. It is similar to `Exclusive` but is stored differently to minimize table fragmentation.

**Workaround:** Manage mutual exclusion manually; use `RemoveID` of the old pair before adding a new one.

> **Not yet ported in Go flecs.** See the [feature-gap list](README.md#feature-gap-list): *Union relationships*.

---

### With

**What it does:** The `With` relationship added to a component or relationship entity ensures that whenever that component or relationship is added to an entity, a second component is also added automatically. When added to a relationship, the co-added id is itself a pair with the same target.

**What the C API looks like:**

```c
// C — not available in Go flecs
ecs_entity_t Responsibility = ecs_new(world);
ecs_entity_t Power = ecs_new_w_pair(world, EcsWith, Responsibility);

ecs_entity_t e = ecs_new_w_id(world, Power);
// e now has both Power and Responsibility
```

**Workaround:** Add both components explicitly in application code, or use an `OnAdd` hook to add the dependent component:

```go
// Workaround using OnAdd hook
type Power struct{}
type Responsibility struct{}

w := flecs.New()
flecs.RegisterComponent[Power](w)
flecs.RegisterComponent[Responsibility](w)

flecs.OnAdd[Power](w, func(fw *flecs.Writer, e flecs.ID, _ Power) {
    flecs.Add[Responsibility](fw, e)
})
```

> **Not yet ported in Go flecs** as a first-class trait.

---

## Trait system roadmap

The table below is the canonical reference for trait-system planning. Check the [feature-gap list in docs/README.md](README.md#feature-gap-list) for more context on each entry.

| Trait | C name | Go status | Notes |
|---|---|---|---|
| **Inheritable** | `EcsInheritable` | ✅ shipped (v0.18.0) | `SetInheritable[T](w)` / `w.SetInheritable(cid)`; auto-promotes query terms to `Self\|Up(IsA)` |
| **OnInstantiate** | `EcsOnInstantiate` | ✅ shipped (v0.33.0) | `SetInstantiatePolicy(w, cid, action)` / `GetInstantiatePolicy`; pair-add form first-class |
| **Inherit** (target) | `EcsInherit` | ✅ shipped (v0.33.0) | `w.Inherit()` action for `SetInstantiatePolicy`; query-time via `SetInheritable[T]`; pair-add equivalent |
| **Override** (target) | `EcsOverride` | ✅ shipped (v0.33.0) | Eager copy from prefab at `(IsA, prefab)` add time; multi-level chain; pre-set value wins |
| **DontInherit** (target) | `EcsDontInherit` | ✅ shipped (v0.33.0) | Suppresses `Has`/`Get` IsA walk and query auto-promotion; takes precedence over Inheritable |
| **Acyclic** | `EcsAcyclic` | ⏳ planned | No cycle detection for custom relationships |
| **CanToggle** | `EcsCanToggle` | ✅ shipped (v0.35.0) | `SetCanToggle(w, cid)` / `IsCanToggle`; `w.CanToggle()` bare-tag form; `Each1`/`Each2`/`Each3`/`Each4` skip disabled rows; `EnableID`/`DisableID`/`IsEnabledID` + typed generics |
| **OnDelete** | `EcsOnDelete` | ✅ shipped (v0.32.0) | `SetCleanupPolicy(w, id, w.OnDelete(), action)` / `GetCleanupPolicy`; actions: `RemoveAction`, `DeleteAction`, `PanicAction` |
| **OnDeleteTarget** | `EcsOnDeleteTarget` | ✅ shipped (v0.32.0) | `SetCleanupPolicy(w, id, w.OnDeleteTarget(), action)`; `ChildOf` bootstrapped with `DeleteAction`; `IsA` has no default (opt-in) |
| **Constant** | *(informal)* | ⏳ planned | No read-only enforcement after first write |
| **DontFragment** | `EcsDontFragment` | ⏳ planned | No sparse non-fragmenting storage |
| **Exclusive** | `EcsExclusive` | ✅ shipped (v0.34.0) | `SetExclusive(w, relID)` / `IsExclusive(w, relID)`; `w.Exclusive()` bare-tag form; `ChildOf`, `OnDelete`, `OnDeleteTarget`, `OnInstantiate` bootstrapped exclusive; `IsA` not exclusive |
| **Final** | `EcsFinal` | ⏳ planned | No `IsA`-extension prevention |
| **OneOf** | `EcsOneOf` | ⏳ planned | No relationship-target constraint |
| **OrderedChildren** | `EcsOrderedChildren` | ⏳ planned | No guaranteed child iteration order |
| **PairIsTag** | `EcsPairIsTag` | ⏳ planned | No forced tag semantics on data-pair relationships |
| **Reflexive** | `EcsReflexive` | ⏳ planned | No implicit self-membership |
| **Relationship** | `EcsRelationship` | ⏳ planned | No usage-as-relationship constraint |
| **Singleton** | `EcsSingleton` | ⏳ planned | No first-class singleton component; workaround via dedicated entity |
| **Sparse** | `EcsSparse` | ⏳ planned | All components use archetype SoA storage |
| **Symmetric** | `EcsSymmetric` | ⏳ planned | No automatic bidirectional pair mirroring |
| **Target** | `EcsTarget` | ⏳ planned | No usage-as-target constraint |
| **Trait** | `EcsTrait` | ⏳ planned | No first-class trait marker |
| **Transitive** | `EcsTransitive` | ⏳ planned | No query-time transitive chain traversal for custom relationships |
| **Traversable** | `EcsTraversable` | ⏳ planned | Any entity can be used for traversal; no formal enforcement |
| **Union** | `EcsUnion` | ⏳ planned | No union-pair semantics |
| **With** | `EcsWith` | ⏳ planned | No automatic co-addition; use `OnAdd` hook as workaround |

---

## See Also

- [Quickstart](Quickstart.md) — hands-on introduction to components and inheritance.
- [EntitiesComponents.md](EntitiesComponents.md) — `RegisterComponent`, hooks, and the component API.
- [Relationships.md](Relationships.md) — trait semantics interact with the pair / relationship model.
- [PrefabsManual.md](PrefabsManual.md) — `SetInheritable[T]` is the primary currently-implemented trait.
- [Manual](Manual.md) — top-level reference hub with world lifecycle, concurrency model, and concept map.
