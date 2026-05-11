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
// values to express NOT, Optional, and OR patterns:
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
//	// Match entities with Position AND (Sleeping OR Working OR Playing).
//	// Adjacent Or terms form one OR-group; multiple Or sequences (separated by
//	// non-Or terms) each form independent groups. A table matches an OR-group
//	// when it contains at least one of the group's IDs.
//	type Sleeping struct{}
//	type Working struct{}
//	sleepID := flecs.RegisterComponent[Sleeping](w)
//	workID  := flecs.RegisterComponent[Working](w)
//	q3 := flecs.NewQueryFromTerms(w,
//	    flecs.With(posID),
//	    flecs.Or(sleepID),
//	    flecs.Or(workID),
//	)
//	flecs.Each(q3, func(it *flecs.QueryIter) {
//	    for it.Next() {
//	        // Use FieldMaybe to disambiguate which Or-group member is present.
//	        _, isSleeping := flecs.FieldMaybe[Sleeping](it, sleepID)
//	        _, isWorking  := flecs.FieldMaybe[Working](it, workID)
//	        _ = isSleeping
//	        _ = isWorking
//	    }
//	})
//
// At least one [TermAnd] ([With]) term is required; queries with only Not/Optional/Or
// terms are rejected. Duplicate IDs across terms also panic at construction.
// An Or term with a zero/invalid ID panics. Duplicate IDs within one OR-group panic.
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
// # JSON Serialization
//
// [World.MarshalJSON] and [World.UnmarshalJSON] implement [json.Marshaler] and
// [json.Unmarshaler] for world persistence. Entities, names, ChildOf
// parent-child hierarchies, IsA prefab relationships, custom pair components
// (data-bearing and tag-only), and regular components are saved and restored.
// Built-in entities are skipped.
//
// The v1 JSON format uses a "parent" field (serial of the ChildOf parent,
// omitted when absent), an optional "prefabs" field (array of IsA target
// serials in EachPrefab order, omitted when empty), an optional "pairs" array
// for custom pair components (omitted when empty), and a "components" map.
// Entities are emitted in topological order over the combined ChildOf+IsA
// predecessor graph (parents and prefabs before their dependents), so a single
// sequential pass restores the full hierarchy.
//
// Each entry in the "pairs" array has the form:
//
//	{"rel":<serial>,"tgt":<serial>}                              // tag-only pair
//	{"rel":<serial>,"tgt":<serial>,"dataType":"pkg.T","data":{}} // data-bearing pair
//
// DataType is the base Go type's reflect.Type.String() (not the "pair(T)"
// wrapper name). Pre-register the base type via RegisterComponent[T] before
// calling UnmarshalJSON for data-bearing pairs; tag-only pairs need no
// pre-registration.
//
// All regular component types must be pre-registered before calling UnmarshalJSON:
//
//	// Save:
//	data, err := w.MarshalJSON()
//	os.WriteFile("save.json", data, 0644)
//
//	// Load:
//	data, _ := os.ReadFile("save.json")
//	w2 := flecs.New()
//	flecs.RegisterComponent[Position](w2)
//	flecs.RegisterComponent[Velocity](w2)
//	err = w2.UnmarshalJSON(data)
//
// Custom pair example — tag pair and data-bearing pair round-trip:
//
//	type Edge struct{ Weight float32 }
//
//	follows := w.NewEntity()
//	alice, bob, charlie := w.NewEntity(), w.NewEntity(), w.NewEntity()
//	flecs.SetPair[Edge](w, alice, follows, bob, Edge{Weight: 0.8})
//	flecs.AddID(w, alice, flecs.MakePair(follows, charlie)) // tag-only
//
//	data, _ := w.MarshalJSON()
//	// JSON for alice: {"serial":2,"pairs":[
//	//   {"rel":1,"tgt":3,"dataType":"pkg.Edge","data":{"Weight":0.8}},
//	//   {"rel":1,"tgt":4}
//	// ]}
//
//	w2 := flecs.New()
//	flecs.RegisterComponent[Edge](w2)
//	w2.UnmarshalJSON(data)
//	// flecs.GetPair[Edge](w2, alice2, follows2, bob2) returns Edge{0.8}.
//
// IsA prefab example — a prefab entity is serialized before its instances, and
// inheritance is restored transparently after UnmarshalJSON:
//
//	prefab := w.NewEntity()
//	flecs.Set(w, prefab, Position{X: 1, Y: 1})
//
//	inst := w.NewEntity()
//	flecs.AddID(w, inst, flecs.MakePair(w.IsA(), prefab))
//
//	data, _ := w.MarshalJSON()
//	// JSON: {"version":1,"entities":[
//	//   {"serial":1,"components":{"pkg.Position":{"X":1,"Y":1}}},
//	//   {"serial":2,"prefabs":[1]}
//	// ]}
//
//	w2 := flecs.New()
//	flecs.RegisterComponent[Position](w2)
//	w2.UnmarshalJSON(data)
//	// flecs.Get[Position](w2, restoredInst) returns Position{1, 1}.
//
// # Stats and Observability
//
// [World.Stats] returns a snapshot of world-level counters and per-phase frame
// timing. The snapshot allocates once per call and is designed for tooling, not
// hot-path use. [World.SystemCountInPhase] returns the active system count for
// a specific pipeline phase.
//
//	stats := w.Stats()
//	fmt.Printf("entities: %d\n", stats.EntityCount)
//	fmt.Printf("tables:   %d\n", stats.TableCount)
//	fmt.Printf("queries:  %d (cached: %d)\n", stats.QueryCount, stats.CachedQueryCount)
//	fmt.Printf("systems:  %d\n", stats.SystemCount)
//	fmt.Printf("frame:    %d\n", stats.FrameCount)
//	fmt.Printf("time:     %.3fs\n", stats.Time)
//
//	// Per-phase timing of the LAST frame (nil until Progress is called):
//	for _, phase := range stats.LastFramePhases {
//	    fmt.Printf("  %s: %v (%d systems)\n", phase.Name, phase.Duration, phase.SystemCount)
//	}
//
//	// Per-component table and entity counts:
//	for _, c := range stats.ComponentStats {
//	    fmt.Printf("  %s: %d tables, %d entities\n", c.Name, c.TableCount, c.EntityCount)
//	}
//
// See https://github.com/SanderMertens/flecs for the upstream C implementation.
package flecs
