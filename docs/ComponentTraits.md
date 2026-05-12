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

**Shipped in v0.41.0.**

**What it does:** Marking a relationship `Acyclic` prevents cycles from being stored. When a pair `(e, R, target)` is added, the engine walks the `R` chain upward from `target`; if `e` is reachable, the add panics with a clear message. `ChildOf` is bootstrapped acyclic, so mutual parent/child cycles are rejected at write time — fixing a correctness gap that could cause `EachChild` to recurse infinitely.

Self-pairs `(e, R, e)` are allowed; Acyclic does not reject them. For self-pairs to be implicitly true without storage, combine with [Reflexive](#reflexive).

**Deliberate divergence from C flecs:** C guards cycles at lookup/traversal time (via `ECS_MAX_RECURSION` and per-function depth caps). The Go port enforces at `AddID` time so that `EachChild` and similar recursors never encounter an infinite chain. This is documented in CHANGELOG v0.41.0.

**Go API:**

```go
myRelID := fw.NewEntity()

// Register the relationship as acyclic (either form is equivalent):
flecs.SetAcyclic(w, myRelID)
// or:
fw.AddID(myRelID, w.Acyclic())

// Safe add — no cycle:
fw.AddID(a, flecs.MakePair(myRelID, b))
fw.AddID(b, flecs.MakePair(myRelID, c))

// This would panic: c → b → a, adding (c, R, a) completes the cycle.
// fw.AddID(c, flecs.MakePair(myRelID, a)) // panics

// Check the flag:
flecs.IsAcyclic(w, myRelID) // → true

// ChildOf is bootstrapped acyclic — this panics:
// fw.AddID(parent, flecs.MakePair(w.ChildOf(), child)) // when child is already ChildOf parent
```

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

**Shipped in v0.42.0.** The `Final` trait prevents an entity from being used as the target of an `IsA` relationship, similar to a `final` class in object-oriented languages. Use it to seal a concrete prefab so no further specialization is possible.

**Go API:**

- `flecs.SetFinal(w, entityID)` — marks `entityID` as Final.
- `flecs.IsFinal(s scope, entityID ID) bool` — reports whether `entityID` is marked Final; accepts `scope` so it works inside both `Read` and `Write` blocks.
- `w.Final() ID` — returns the built-in Final trait entity (index 23). The bare-tag form `fw.AddID(entityID, w.Final())` is equivalent to `SetFinal(w, entityID)`.

**Enforcement:** Adding `(IsA, target)` panics immediately if `target` has the Final trait. The check fires on both the immediate path and the deferred path (when the `Write` scope flushes). Self-pairs — `(IsA, e)` where `e` is Final — are also rejected, matching C semantics.

```go
w := flecs.New()

var concretePrefab, instance flecs.ID
w.Write(func(fw *flecs.Writer) {
    concretePrefab = fw.NewEntity()
    instance = fw.NewEntity()
})

// Seal the prefab — no further IsA subtyping allowed.
flecs.SetFinal(w, concretePrefab)

// This would panic: "cannot add (IsA, <id>): <id> has the Final trait"
// w.Write(func(fw *flecs.Writer) {
//     fw.AddID(instance, flecs.MakePair(w.IsA(), concretePrefab))
// })

// Non-IsA pairs to a Final entity are unaffected.
w.Write(func(fw *flecs.Writer) {
    fw.AddID(instance, flecs.MakePair(w.ChildOf(), concretePrefab)) // OK
})

w.Read(func(fr *flecs.Reader) {
    fmt.Println(flecs.IsFinal(fr, concretePrefab)) // true
})
```

**No built-in ships Final** — matching C bootstrap behavior. Final is a plain tag stored on the target entity (no `EcsIdFinal` flag bit in Go, matching C's implementation in `component_index.c:447-453`).

**Divergence from C:** C's query engine uses `EcsFinal` to suppress IsA-substitution in the validator (`query/validator.c:849`). The Go port enforces Final only at write time for v0.42.0; query-side optimization is out of scope.

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

**Shipped in v0.39.0.** A reflexive relationship asserts `R(X, X)` — every entity implicitly holds the relationship to itself, without storing an explicit self-pair. The built-in `IsA` is reflexive: `IsA(Tree, Tree)` is true even without an explicit pair.

```go
flecs.SetReflexive(w, myRelID)
// or bare-tag form:
fw.AddID(myRelID, w.Reflexive())

// HasID self-pair returns true without a stored pair:
r.HasID(a, flecs.MakePair(myRelID, a)) // → true

// Query for (R, a) also matches a itself:
q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(myRelID, a)))
```

**API:** `flecs.SetReflexive(w, relID)`, `flecs.IsReflexive(w, relID) bool`, `w.Reflexive() ID` (built-in entity at index 21).

**HasID divergence from C:** in C flecs `ecs_has_id` does **not** consult `EcsReflexive`; it is query-only. In Go flecs `HasID` has been extended to return `true` for self-pairs of reflexive relationships, matching the semantic promise in the documentation. This divergence is explicitly documented in CHANGELOG v0.39.0.

**Composition with Transitive:** a relationship that is both Reflexive and Transitive matches the target entity itself **and** all entities that chain to the target via transitive walk. `IsA` uses both traits.

**Cached query note:** `CachedQuery` evaluates the reflexive self-match at cache construction and on every new-table creation; it does not re-evaluate when the target entity migrates. Staleness is accepted for this phase.

**Implementation note:** index 21 in the built-in entity allocation order; mirrors C `EcsReflexive` (a tag entity, not a flag bit). `IsA` is bootstrapped as reflexive, matching `src/bootstrap.c:1321`.

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

**Shipped in v0.36.0.** When a symmetric relationship `(R, B)` is added to entity `A`, the relationship `(R, A)` is automatically added to entity `B`. Removal is mirrored the same way. Useful for bidirectional relationships such as `AlliesWith`, `MarriedTo`, or `TradingWith`.

```go
w := flecs.New()
marriedTo := flecs.RegisterComponent[struct{}](w) // or use fw.NewEntity()
flecs.SetSymmetric(w, marriedTo)

var bob, alice flecs.ID
w.Write(func(fw *flecs.Writer) {
    bob = fw.NewEntity()
    alice = fw.NewEntity()
    fw.AddID(bob, flecs.MakePair(marriedTo, alice))
    // alice now automatically has (marriedTo, bob)
})

w.Read(func(fr *flecs.Reader) {
    _ = fr.HasID(alice, flecs.MakePair(marriedTo, bob)) // true — mirrored automatically
})
```

**API:**
- `flecs.SetSymmetric(w, relID)` — marks a relationship as symmetric.
- `flecs.IsSymmetric(w, relID) bool` — reports whether a relationship is symmetric.
- `w.Symmetric() ID` — the built-in Symmetric trait entity (index 19). The bare-tag form `fw.AddID(relID, w.Symmetric())` is equivalent to `SetSymmetric(w, relID)`.

**Loop guard:** the mirror is idempotent — adding `(R, B)` to `A` mirrors `(R, A)` to `B`, which would try to mirror back, but `A` already has `(R, B)`, so recursion terminates in one extra hop with no observable side effects.

**Interaction with Exclusive:** when `R` is both symmetric and exclusive, replacing `(R, X)` with `(R, B)` on `A` also mirrors `(R, A)` to `B`; if `B` held a conflicting exclusive target, the exclusive constraint replaces it as well.

---

### Transitive

**Shipped in v0.37.0.**

**What it does:** A transitive relationship allows queries to follow a chain automatically at query time. If `(R, B)` is on entity `A` and `(R, C)` is on entity `B`, then a query for `(R, C)` also matches `A`. Formally: `aRb ∧ bRc ⇒ aRc`. The built-in `IsA` already behaves transitively for `Get`/`Has`; `Transitive` generalises this to arbitrary custom relationships and to the full query engine.

**Go API:**

- `flecs.SetTransitive(w, relID)` — marks a relationship as transitive; equivalent to the bare-tag form.
- `flecs.IsTransitive(w, relID) bool` — reports whether a relationship is transitive.
- `w.Transitive() ID` — the built-in Transitive trait entity (index 20). The bare-tag form `fw.AddID(relID, w.Transitive())` is equivalent to `SetTransitive(w, relID)`.

**LocatedIn example:**

```go
w := flecs.New()
var locatedIn, manhattan, newYork, usa flecs.ID
w.Write(func(fw *flecs.Writer) {
    locatedIn = fw.NewEntity()
    manhattan  = fw.NewEntity()
    newYork    = fw.NewEntity()
    usa        = fw.NewEntity()
})
flecs.SetTransitive(w, locatedIn)
w.Write(func(fw *flecs.Writer) {
    fw.AddID(manhattan, flecs.MakePair(locatedIn, newYork))
    fw.AddID(newYork,   flecs.MakePair(locatedIn, usa))
})

// Query for (LocatedIn, USA) matches both manhattan (transitively) and newYork (directly).
q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(locatedIn, usa)))
q.Each(func(it *flecs.QueryIter) {
    for _, e := range it.Entities() {
        _ = e // manhattan and newYork
    }
})
```

**Implementation notes:**

- **Lazy evaluation:** chaining happens at query time only; no pairs are written eagerly. This avoids O(n²) writes for long chains. Compare to [Symmetric](#symmetric) which mirrors eagerly at write time.
- **Cycle detection:** the walk uses a visited set; cyclic chains terminate cleanly.
- **Depth limit:** bounded at 64 hops; chains deeper than the limit are silently truncated without panicking.
- **Cached query staleness:** `CachedQuery` pre-evaluates transitive chains at construction and on new-table creation. It does NOT re-evaluate on pair mutation; staleness is accepted and documented.
- **Transitive does not imply Reflexive:** `(R, self)` is not auto-matched by transitive chains alone. Use `flecs.SetReflexive` (shipped in v0.39.0) to enable the self-match; the two traits compose cleanly.
- **Wildcard interaction:** wildcard query terms compose correctly with Transitive (shipped in v0.38.0). A `(R, Wildcard)` term on a transitive relationship will match tables that have any direct `(R, X)` pair and emit one expansion row per concrete pair found. See [Query-term sentinels](#query-term-sentinels-wildcard-and-any) below.

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
| **Acyclic** | `EcsAcyclic` | ✅ shipped (v0.41.0) | `SetAcyclic(w, relID)` / `IsAcyclic(w, relID)`; `w.Acyclic()` bare-tag form; write-time cycle rejection on AddID; `ChildOf` bootstrapped acyclic; deliberate divergence from C (write-time vs. lookup-time guards) |
| **CanToggle** | `EcsCanToggle` | ✅ shipped (v0.35.0) | `SetCanToggle(w, cid)` / `IsCanToggle`; `w.CanToggle()` bare-tag form; `Each1`/`Each2`/`Each3`/`Each4` skip disabled rows; `EnableID`/`DisableID`/`IsEnabledID` + typed generics |
| **OnDelete** | `EcsOnDelete` | ✅ shipped (v0.32.0) | `SetCleanupPolicy(w, id, w.OnDelete(), action)` / `GetCleanupPolicy`; actions: `RemoveAction`, `DeleteAction`, `PanicAction` |
| **OnDeleteTarget** | `EcsOnDeleteTarget` | ✅ shipped (v0.32.0) | `SetCleanupPolicy(w, id, w.OnDeleteTarget(), action)`; `ChildOf` bootstrapped with `DeleteAction`; `IsA` has no default (opt-in) |
| **Constant** | *(informal)* | ⏳ planned | No read-only enforcement after first write |
| **DontFragment** | `EcsDontFragment` | ⏳ planned | No sparse non-fragmenting storage |
| **Exclusive** | `EcsExclusive` | ✅ shipped (v0.34.0) | `SetExclusive(w, relID)` / `IsExclusive(w, relID)`; `w.Exclusive()` bare-tag form; `ChildOf`, `OnDelete`, `OnDeleteTarget`, `OnInstantiate` bootstrapped exclusive; `IsA` not exclusive |
| **Final** | `EcsFinal` | ✅ shipped (v0.42.0) | `SetFinal(w, entityID)` / `IsFinal(scope, entityID)`; `w.Final()` bare-tag form; write-time enforcement: adding `(IsA, target)` panics if target is Final; no built-in ships Final; self-pair `(IsA, e)` also rejected when e is Final |
| **OneOf** | `EcsOneOf` | ⏳ planned | No relationship-target constraint |
| **OrderedChildren** | `EcsOrderedChildren` | ⏳ planned | No guaranteed child iteration order |
| **PairIsTag** | `EcsPairIsTag` | ⏳ planned | No forced tag semantics on data-pair relationships |
| **Reflexive** | `EcsReflexive` | ✅ shipped (v0.39.0) | `SetReflexive(w, relID)` / `IsReflexive(w, relID)`; `w.Reflexive()` bare-tag form; `HasID(e, (R,e))` returns true; query self-match includes target's own table; composes with Transitive; `IsA` bootstrapped reflexive |
| **Relationship** | `EcsRelationship` | ⏳ planned | No usage-as-relationship constraint |
| **Singleton** | `EcsSingleton` | ⏳ planned | No first-class singleton component; workaround via dedicated entity |
| **Sparse** | `EcsSparse` | ⏳ planned | All components use archetype SoA storage |
| **Symmetric** | `EcsSymmetric` | ✅ shipped (v0.36.0) | `SetSymmetric(w, relID)` / `IsSymmetric(w, relID)`; `w.Symmetric()` bare-tag form; mirror fires on add and remove; loop-guard via `HasComponent` idempotence; composes with `Exclusive` |
| **Target** | `EcsTarget` | ⏳ planned | No usage-as-target constraint |
| **Trait** | `EcsTrait` | ⏳ planned | No first-class trait marker |
| **Transitive** | `EcsTransitive` | ✅ shipped (v0.37.0) | `SetTransitive(w, relID)` / `IsTransitive(w, relID)`; `w.Transitive()` bare-tag form; lazy walk at query time with cycle detection and depth limit; cached query re-evaluates on table-create |
| **Traversable** | `EcsTraversable` | ⏳ planned | Any entity can be used for traversal; no formal enforcement |
| **Union** | `EcsUnion` | ⏳ planned | No union-pair semantics |
| **With** | `EcsWith` | ⏳ planned | No automatic co-addition; use `OnAdd` hook as workaround |

---

## Query-term sentinels: Wildcard and Any

`w.Wildcard()` (`*`) and `w.Any()` (`_`) are built-in entity IDs used exclusively as query-term annotations. They are **not** component traits — do not add them to entity or component records.

| Sentinel | Index | Semantics |
|---|---|---|
| `w.Wildcard()` | 21 | Emits one iterator row per concrete target. `(R, Wildcard)` yields one row per `(R, X)` pair in the table. |
| `w.Any()` | 22 | Short-circuit match: at most one row per entity. `(R, Any)` yields one row if any `(R, X)` pair exists. |

Both sentinels work in target and relationship positions. See [`docs/Queries.md`](Queries.md) § *Wildcard and Any query terms* for the full API.

---

## See Also

- [Quickstart](Quickstart.md) — hands-on introduction to components and inheritance.
- [EntitiesComponents.md](EntitiesComponents.md) — `RegisterComponent`, hooks, and the component API.
- [Relationships.md](Relationships.md) — trait semantics interact with the pair / relationship model.
- [PrefabsManual.md](PrefabsManual.md) — `SetInheritable[T]` is the primary currently-implemented trait.
- [Manual](Manual.md) — top-level reference hub with world lifecycle, concurrency model, and concept map.
