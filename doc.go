// Package flecs is an idiomatic, high-performance Go port of the [flecs] Entity
// Component System library. It provides archetype-based storage, a generic-typed
// API, and zero third-party dependencies.
//
// # Creating a World
//
// The [World] is the central ECS object. It owns entities, component metadata,
// and archetype tables. Create one with [New]:
//
//	w := flecs.New()
//
// World is NOT goroutine-safe; external synchronization is required.
//
// # Components
//
// Components are plain Go structs (or any type). Register a type with
// [RegisterComponent] to obtain its entity ID, then attach values to entities
// with [Set] and read them with [Get]:
//
//	type Position struct{ X, Y float32 }
//
//	posID := flecs.RegisterComponent[Position](w)
//	e     := w.NewEntity()
//	flecs.Set(w, e, Position{X: 1, Y: 2})
//	if p, ok := flecs.Get[Position](w, e); ok { ... }
//
// # Queries
//
// Queries select all entities that own a given set of components. The ergonomic
// helpers [Each1], [Each2], [Each3], [Each4] cover the common case. For
// programmatic or cached queries use [NewQuery], [NewCachedQuery], and [Field]:
//
//	flecs.Each2[Position, Velocity](w, func(e flecs.ID, p *Position, v *Velocity) {
//	    p.X += v.DX
//	})
//
// # Pipelines and Systems
//
// [NewSystem] registers a [System] that runs on every [World.Progress] call in
// the default OnUpdate phase. [NewSystemInPhase] places systems in PreUpdate,
// OnUpdate, PostUpdate, or OnFixedUpdate. [World.SetFixedTimestep] configures a
// fixed-rate accumulator loop for physics or other deterministic subsystems:
//
//	q := flecs.NewCachedQuery(w, posID)
//	flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) { ... })
//	w.Progress(1.0 / 60.0)
//
// # Relationships
//
// [MakePair] constructs a pair ID from a relationship entity and a target entity.
// The built-in [World.ChildOf] relationship creates parent-child hierarchies with
// cascade delete. The built-in [World.IsA] relationship provides prefab
// inheritance: [Get] and [Has] walk the IsA chain when the component is not
// locally owned.
//
//	flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))
//	flecs.AddID(w, instance, flecs.MakePair(w.IsA(), prefab))
//
// # Structured Query Terms
//
// [NewQueryFromTerms] and [NewCachedQueryFromTerms] accept a mix of [TermKind]
// values to express NOT and Optional patterns:
//
//	posID  := flecs.RegisterComponent[Position](w)
//	velID  := flecs.RegisterComponent[Velocity](w)
//	deadID := w.NewEntity() // tag
//
//	// Match entities with Position but NOT Dead.
//	q := flecs.NewQueryFromTerms(w,
//	    flecs.With(posID),
//	    flecs.Without(deadID),
//	)
//
//	// Match entities with Position; Velocity is optional.
//	q2 := flecs.NewQueryFromTerms(w,
//	    flecs.With(posID),
//	    flecs.Maybe(velID),
//	)
//	flecs.Each(q2, func(it *flecs.QueryIter) {
//	    for it.Next() {
//	        positions := flecs.Field[Position](it, posID)
//	        velocities, hasVel := flecs.FieldMaybe[Velocity](it, velID)
//	        for i := range positions {
//	            if hasVel {
//	                positions[i].X += velocities[i].X
//	            }
//	        }
//	    }
//	})
//
// At least one [TermAnd] ([With]) term is required; queries with only Not/Optional
// terms are rejected. Duplicate IDs across terms also panic at construction.
//
// # Deferred Mutation
//
// [World.Defer] wraps a block so that structural mutations (Set, Remove, Delete,
// AddID) are queued and applied atomically when the block exits. This makes it
// safe to mutate the world during iteration:
//
//	w.Defer(func() {
//	    flecs.Each1[Position](w, func(e flecs.ID, p *Position) {
//	        if p.X < 0 { w.Delete(e) } // queued, not immediate
//	    })
//	})
//
// # Dynamic Value Access
//
// [World.GetByID] and [World.SetByID] provide runtime-dynamic access when only
// the component ID is known — for example, in a serializer iterating
// EntityComponents. They are the non-generic analogs of Get[T] and Set[T]:
//
//	v, ok := w.GetByID(e, posID)     // returns any; inheritance-aware
//	w.SetByID(e, posID, Position{1}) // panics on type mismatch
//
// Use Get[T]/Set[T] when the type is known at compile time; use GetByID/SetByID
// only when the component ID is the sole available handle.
//
// See https://github.com/SanderMertens/flecs for the upstream C implementation.
package flecs
