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
//	var e flecs.ID
//	w.Write(func(fw *flecs.Writer) {
//	    e = fw.NewEntity()
//	    flecs.Set(fw, e, Position{X: 1, Y: 2})
//	})
//	w.Read(func(fr *flecs.Reader) {
//	    if p, ok := flecs.Get[Position](fr, e); ok { ... }
//	})
//
// # Queries
//
// Queries select all entities that own a given set of components. The ergonomic
// helpers [Each1], [Each2], [Each3], [Each4] cover the common case. For
// programmatic or cached queries use [NewQuery], [NewCachedQuery], and [Field]:
//
//	w.Read(func(fr *flecs.Reader) {
//	    flecs.Each2[Position, Velocity](fr, func(e flecs.ID, p *Position, v *Velocity) {
//	        p.X += v.DX
//	    })
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
//	w.Write(func(fw *flecs.Writer) {
//	    flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
//	    flecs.AddID(fw, instance, flecs.MakePair(w.IsA(), prefab))
//	})
//
// # Structured Query Terms
//
// [NewQueryFromTerms] and [NewCachedQueryFromTerms] accept a mix of [TermKind]
// values to express NOT, Optional, and OR patterns:
//
//	posID  := flecs.RegisterComponent[Position](w)
//	velID  := flecs.RegisterComponent[Velocity](w)
//	var deadID flecs.ID
//	w.Write(func(fw *flecs.Writer) { deadID = fw.NewEntity() }) // tag
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
// # Query Traversal Modifiers
//
// Terms can traverse relationships at query-match time via chained builder methods
// on [Term]: [Term.Up], [Term.SelfUp], and [Term.Cascade]. This allows queries to
// find entities that inherit a component through an IsA prefab or ChildOf hierarchy
// without calling [GetUp] or [HasUp] in an iteration loop.
//
// Example — match instances that inherit Position from a prefab:
//
//	type Position struct{ X, Y float32 }
//
//	posID := flecs.RegisterComponent[Position](w)
//
//	var prefab, child flecs.ID
//	w.Write(func(fw *flecs.Writer) {
//	    prefab = fw.NewEntity()
//	    flecs.Set(fw, prefab, Position{X: 10, Y: 20})
//
//	    child = fw.NewEntity()
//	    flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
//	    // child has no local Position; it inherits from prefab.
//	})
//
//	// Up(IsA): match entities whose ancestor via IsA owns Position.
//	q := flecs.NewQueryFromTerms(w, flecs.With(posID).Up(w.IsA()))
//	q.Each(func(it *flecs.QueryIter) {
//	    p, ok := flecs.FieldShared[Position](it, posID) // returns prefab's value
//	    _ = flecs.IsFieldSelf(it, posID)                // false: value is inherited
//	    _, _ = p, ok
//	})
//
// [Term.SelfUp] checks the entity first, then walks up; [IsFieldSelf] distinguishes
// which path was taken. [Term.Cascade] (cached queries only) orders matched tables
// from root to leaf, enabling parent-before-child system execution.
//
// Note: if a prefab or parent gains or loses the traversal component AFTER a
// [CachedQuery] is built, the cache is NOT automatically updated. Rebuild the
// query to reflect structural ancestry changes.
//
// # Inheritable Components
//
// Marking a component inheritable causes queries to automatically match entities
// that inherit the component from a prefab via IsA — without requiring explicit
// traversal modifiers on every query term.
//
//	type Position struct{ X, Y float32 }
//
//	posID := flecs.RegisterComponent[Position](w)
//	flecs.SetInheritable[Position](w) // opt-in; default is non-inheritable
//
//	var prefab, instance flecs.ID
//	w.Write(func(fw *flecs.Writer) {
//	    prefab = fw.NewEntity()
//	    flecs.Set(fw, prefab, Position{X: 10, Y: 20})
//
//	    instance = fw.NewEntity()
//	    flecs.AddID(fw, instance, flecs.MakePair(w.IsA(), prefab))
//	    // instance has no local Position; it inherits from prefab
//	})
//
//	// Each1 now matches both prefab (owns Position) and instance (inherits it).
//	w.Read(func(r *flecs.Reader) {
//	    flecs.Each1[Position](r, func(e flecs.ID, p *Position) {
//	        // e may be prefab (p points to its local slot) or instance
//	        // (p points to prefab's slot — shared C-flecs semantics).
//	        _ = p
//	    })
//	})
//
// The auto-promotion applies to [Each1], [Each2], [Each3], [Each4], [NewQuery],
// [NewQueryFromTerms], [NewCachedQuery], and [NewCachedQueryFromTerms].
// Calling [Term.Self] on a term suppresses auto-promotion for that term.
//
// When an entity inherits a component (Up path), [Each1]-style callbacks receive
// the prefab's component pointer — the same pointer for every entity in the
// matched table. Mutating through the pointer alters the prefab's value and
// therefore all inheritors. Use [IsFieldSelf] to detect the Up path and
// [FieldShared] to read a safe copy.
//
// [SetInheritable] must be called BEFORE any query referencing that component
// is constructed. Components default to non-inheritable; only opt-in types
// change query semantics, so existing code is unaffected.
//
// # Deferred Mutation
//
// [World.Write] opens an exclusive read/write scope. All structural mutations
// (Set, Remove, Delete, AddID) inside the scope are queued and applied atomically
// when the scope exits. This makes it safe to mutate the world during iteration:
//
//	w.Write(func(fw *flecs.Writer) {
//	    flecs.Each1[Position](fw, func(e flecs.ID, p *Position) {
//	        if p.X < 0 { fw.Delete(e) } // queued, not immediate
//	    })
//	})
//
// # Concurrency model
//
// [World.Read] opens a shared-read scope backed by sync.RWMutex. Multiple
// goroutines may hold concurrent Read scopes; a Write scope waits for all
// active Read scopes to finish before acquiring exclusive access:
//
//	var wg sync.WaitGroup
//	for range numWorkers {
//	    wg.Add(1)
//	    go func() {
//	        defer wg.Done()
//	        w.Read(func(fr *flecs.Reader) {
//	            flecs.Each1[Position](fr, func(e flecs.ID, p *Position) { ... })
//	        })
//	    }()
//	}
//	wg.Wait()
//
// [World.Write] acquires an exclusive write lock. Structural mutations inside
// the scope (Set, Remove, Delete, AddID, RemoveID, SetPair, SetByID) are
// transparently enqueued in the deferred-command queue and flushed when the
// scope exits. Nested Write calls from the same goroutine share the queue and
// flush only when the outermost Write returns.
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
//	var follows, alice, bob, charlie flecs.ID
//	w.Write(func(fw *flecs.Writer) {
//	    follows = fw.NewEntity()
//	    alice, bob, charlie = fw.NewEntity(), fw.NewEntity(), fw.NewEntity()
//	    flecs.SetPair[Edge](fw, alice, follows, bob, Edge{Weight: 0.8})
//	    flecs.AddID(fw, alice, flecs.MakePair(follows, charlie)) // tag-only
//	})
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
//	var prefab, inst flecs.ID
//	w.Write(func(fw *flecs.Writer) {
//	    prefab = fw.NewEntity()
//	    inst = fw.NewEntity()
//	    flecs.Set(fw, prefab, Position{X: 1, Y: 1})
//	    flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), prefab))
//	})
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
// # Change Detection
//
// [CachedQuery.Changed] provides opt-in, per-table change detection so that
// systems can skip work when nothing relevant has changed since the last call.
// The first call after construction always returns true ("initial state is all
// changed"); subsequent calls return true only when the cached match set was
// mutated:
//
//	q := flecs.NewCachedQuery(w, posID, velID)
//
//	for {
//	    if q.Changed() {
//	        // Something relevant changed since the last call. Re-run downstream work.
//	        runMovement(q)
//	    }
//	    w.Progress(0.016)
//	}
//
// A change is any of: a new matching table added to the cache, a column write
// (Set[T]/SetByID or pair write), or a structural change (entity added/removed
// via migrate). The change counter is monotonic uint64; any column write on a
// cached table marks it dirty for ALL cached queries containing it (never
// under-reports, may over-report). Changed() is NOT goroutine-safe.
//
// Change detection is only available on [CachedQuery]; uncached [*Query] does
// not provide Changed(). Use [NewCachedQuery] or [NewCachedQueryFromTerms] when
// change detection is needed.
//
// # Parallel Execution
//
// Systems can be dispatched concurrently within a phase by opting in to
// parallel execution and configuring a worker pool:
//
//	w := flecs.New()
//	w.SetWorkerCount(4) // 0 (default) = serial; positive = parallel pool
//
//	posID := flecs.RegisterComponent[Position](w)
//	velID := flecs.RegisterComponent[Velocity](w)
//
//	moveQ := flecs.NewCachedQuery(w, posID, velID)
//	moveSys := flecs.NewSystem(w, moveQ, func(dt float32, it *flecs.QueryIter) {
//	    for it.Next() {
//	        pos := flecs.Field[Position](it, posID)
//	        vel := flecs.Field[Velocity](it, velID)
//	        for i := range pos {
//	            pos[i].X += vel[i].DX * dt
//	        }
//	    }
//	})
//	moveSys.SetParallel(true)
//	moveSys.SetWriteSet([]flecs.ID{posID}) // declares written components
//
//	w.Progress(dt)
//
// Conflict detection: two parallel systems in the same phase are placed in the
// same batch only when their write sets are pairwise disjoint. The world
// over-approximates: unless [System.SetWriteSet] is called explicitly, the
// default write set contains all And, Or, and Optional query term IDs. Even
// read-only access to a component that another system writes is treated as a
// conflict unless SetWriteSet([]flecs.ID{}) is used to declare a read-only
// system.
//
// Storage is NOT goroutine-safe; the world prevents parallel writes by
// enforcing disjoint write sets. The ECS tables are NOT goroutine-safe — only
// systems in the same batch with disjoint write sets are allowed to run
// concurrently. Reading a component that another parallel system writes
// produces undefined behaviour.
//
// Parallel systems must NOT call [Field] on each other's [QueryIter]; each
// system owns its iterator for the duration of its callback.
//
// Structural mutations ([Set], [Remove], [Delete], [AddID]) from within a
// parallel system are safe: they are queued through the deferred mechanism
// (mutex-protected) and applied after the entire phase completes.
//
// # Multi-Threaded Systems
//
// [System.SetMultiThreaded] provides within-system parallelism: a single
// system's iter is split across all workers, each receiving a disjoint row
// slice of every matched table. Unlike [System.SetParallel] (which runs
// different systems concurrently), SetMultiThreaded splits ONE system across N
// goroutines. The two flags are orthogonal; a multi-threaded system cannot
// batch with parallel siblings.
//
//	cq := flecs.NewCachedQuery(w, vecID)
//	sys := flecs.NewSystem(w, cq, func(dt float32, it *flecs.QueryIter) {
//	    for it.Next() {
//	        vecs := flecs.Field[Vec3](it, vecID)
//	        for i := range vecs { // only THIS worker's rows
//	            vecs[i].X += dt
//	        }
//	    }
//	})
//	sys.SetMultiThreaded(true)
//
// Partition formula (matches C flecs src/iter.c:970-993): for worker i of N
// on a table with count rows, first = (count/N)*i + min(i, count%N) and
// worker_count = count/N + (i < count%N ? 1 : 0). Workers whose count is zero
// skip the table silently. [Field], [FieldMaybe], and [QueryIter.Entities] all
// return the worker's slice; the caller sees no difference from a full iter.
//
// In-place updates (mutating Field[T] slice elements directly) scale linearly
// because workers never touch the same memory. Deferred structural mutations
// (Set, Delete, AddID from inside the loop via [QueryIter.Writer]) are also
// lock-free on the hot path: each worker goroutine writes into its own
// per-stage command queue with no synchronization. After wg.Wait(), the main
// goroutine merges stages in ascending id order (stage 1, 2, …, N, then
// stage 0). Within each stage, per-entity FIFO coalescing is preserved; there
// is no cross-stage coalescing. Hook callbacks fired during the merge always
// execute on the main goroutine and receive the stage-0 Writer.
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
// # Structured Logging
//
// [World.SetLogger] installs a [log/slog]-compatible logger that receives
// DEBUG-level records for each lifecycle event. When no logger is installed
// (the default), logging is a single nil-pointer comparison at each event site
// — zero overhead on hot paths.
//
//	w := flecs.New()
//	w.SetLogger(slog.Default())
//	// or with JSON output:
//	w.SetLogger(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
//
//	posID := flecs.RegisterComponent[Position](w)
//	// -> DEBUG msg="component registered" name=pkg.Position id=<n> size=<n>
//
//	var e flecs.ID
//	w.Write(func(fw *flecs.Writer) {
//	    e = fw.NewEntity()
//	    // -> DEBUG msg="entity created" id=<n>
//	    flecs.Set(fw, e, Position{1, 2})
//	})
//	// -> DEBUG msg="table created" signature_len=1 signature="<id>"
//
// Lifecycle events that produce log records:
//
//   - Entity created / deleted
//   - Component registered (first call only; subsequent RegisterComponent are no-ops)
//   - Table created (new archetype; signature is a space-separated list of decimal IDs)
//   - System added / closed
//   - Observer registered / unsubscribed
//   - Snapshot serialized / loaded (entities count included)
//
// No records fire on hot paths: Set, Get, Has, Each, Progress, and similar
// read or per-frame operations are intentionally excluded. Use [World.Logger]
// to retrieve the currently installed logger. Pass nil to [World.SetLogger] to
// disable logging.
//
// Note: lifecycle events that occur inside [New] (empty table creation and
// built-in entity allocation) are not logged because SetLogger cannot be called
// until after New() returns.
//
// # REST API
//
// [NewRESTHandler] returns an [http.Handler] that exposes world inspection and
// snapshot endpoints over HTTP. Wire it into your own [http.Server]:
//
//	w := flecs.New()
//	// ... populate world
//
//	server := &http.Server{Addr: ":8080", Handler: flecs.NewRESTHandler(w)}
//	go server.ListenAndServe()
//
//	// GET  /stats              → world stats JSON
//	// GET  /components         → all registered component infos
//	// GET  /components/{id}    → single component info by uint64 ID
//	// GET  /entities           → alive entities; optional ?limit=N (default 1000, max 10000)
//	// GET  /entities/{id}      → entity detail (name, components, parent, prefabs, pairs)
//	// GET  /snapshot           → full world MarshalJSON output
//	// PUT  /snapshot           → load a snapshot into the world (replaces state; not transactional)
//
// Concurrency: all GET endpoints treat the world as read-only; they must not
// run while the world is being mutated. PUT /snapshot mutates world state and
// must not run concurrently with any other world operation. Add your own mutex
// if multiple goroutines share the world.
//
// # Concurrency safety — exclusive access ownership assertion
//
// Every public World method asserts that either no goroutine has claimed the
// world via [World.ExclusiveAccessBegin], OR the calling goroutine is the
// claimant. Cross-goroutine misuse panics with [ErrExclusiveAccessViolation].
// The assertion is always on — no build tag is required.
//
// Usage:
//
//		w := flecs.New()
//		w.ExclusiveAccessBegin("main-thread")
//		// ... only this goroutine may call World methods ...
//		w.ExclusiveAccessEnd(false) // false = release; true = lock all writes
//
//	  - [World.ExclusiveAccessBegin] records the calling goroutine's ID and a
//	    human-readable label. Any subsequent call from a different goroutine
//	    panics with [ErrExclusiveAccessViolation].
//	  - [World.ExclusiveAccessEnd] releases the claim. Passing lockWorld=true
//	    transitions to a write-locked state where ALL goroutines receive a
//	    violation panic on any mutation; reads still pass. Passing lockWorld=false
//	    returns the world to the unclaimed state where any goroutine may call it.
//
// Common case overhead: one atomic.Load per public call. [goid.Get] is only
// invoked when an owner is set, so the un-claimed path costs ~1 ns total.
// Goroutine IDs are obtained via github.com/petermattis/goid.
//
// # Conceptual Documentation
//
// For topic-level guides with worked Go examples, see the docs/ directory:
//
//   - [docs/Quickstart.md] — start here; covers world, entities, components, queries,
//     hierarchies, prefabs, systems, and observers.
//   - [docs/README.md] — full docs index, survey table, and feature-gap list vs. upstream C.
//
// See https://github.com/SanderMertens/flecs for the upstream C implementation.
package flecs
