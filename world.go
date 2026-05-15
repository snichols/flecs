package flecs

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/snichols/flecs/internal/component"
	"github.com/snichols/flecs/internal/storage/componentindex"
	"github.com/snichols/flecs/internal/storage/entityindex"
	"github.com/snichols/flecs/internal/storage/table"
)

// Table is an alias for the internal archetype table type. It is exported so
// that test code in the flecs_test package can hold typed *Table pointers
// returned by helpers such as TableOf (defined in export_test.go).
type Table = table.Table

// World is the central ECS object. It owns entities (keyed by ID), component
// metadata (in a Registry), and archetype tables (keyed by sorted component-ID
// signature). Components are first-class entities; each registered component
// type is itself allocated an entity ID.
//
// Archetype storage: entities that share the same component set are grouped into
// a Table, stored in structure-of-arrays columns. Changing the component set of
// an entity triggers an archetype migration: the entity moves to the table whose
// signature matches the new set.
//
// Signature key encoding: a sorted []ID is encoded as raw bytes
// (8 bytes per uint64 ID, host byte-order) for use as a map key. This encoding
// is stable within a single process but not across processes or machines.
//
// *World is NOT goroutine-safe; external synchronization is required.
type World struct {
	mu                   sync.RWMutex // guards Read/Write scopes
	writeCapability      Writer       // cached Writer; &writeCapability avoids per-Write allocation
	readCapability       Reader       // cached Reader; &readCapability avoids per-Read allocation
	index                *entityindex.Index
	registry             *component.Registry
	tables               map[string]*table.Table         // sigKey(sorted []ID) → table
	empty                *table.Table                    // canonical empty-signature table for new entities
	compIndex            *componentindex.Index           // reverse map: component ID → tables containing it
	observers            map[observerKey]*observerBucket // lazily allocated; keyed by (id, event)
	cachedQueries        []*CachedQuery                  // lazily allocated; notified on new table creation
	systems              []*System                       // lazily allocated; compacted in NewSystem
	childOfID            ID                              // built-in ChildOf relationship entity (index 1)
	isAID                ID                              // built-in IsA relationship entity (index 2)
	nameID               ID                              // built-in Name component entity (index 3)
	preUpdateID          ID                              // built-in PreUpdate phase entity (index 4)
	onUpdateID           ID                              // built-in OnUpdate phase entity (index 5)
	postUpdateID         ID                              // built-in PostUpdate phase entity (index 6)
	onFixedUpdateID      ID                              // built-in OnFixedUpdate phase entity (index 7)
	onInstantiateID      ID                              // built-in OnInstantiate relationship entity (index 8)
	inheritID            ID                              // built-in Inherit trait entity (index 9)
	overrideID           ID                              // built-in Override trait entity (index 10)
	dontInheritID        ID                              // built-in DontInherit trait entity (index 11)
	onDeleteID           ID                              // built-in OnDelete trait relationship entity (index 12)
	onDeleteTargetID     ID                              // built-in OnDeleteTarget trait relationship entity (index 13)
	removeActionID       ID                              // built-in Remove cleanup action entity (index 14)
	deleteActionID       ID                              // built-in Delete cleanup action entity (index 15)
	panicActionID        ID                              // built-in Panic cleanup action entity (index 16)
	exclusiveID          ID                              // built-in Exclusive trait entity (index 17)
	canToggleID          ID                              // built-in CanToggle trait entity (index 18)
	symmetricID          ID                              // built-in Symmetric trait entity (index 19)
	transitiveID         ID                              // built-in Transitive trait entity (index 20)
	reflexiveID          ID                              // built-in Reflexive trait entity (index 21)
	acyclicID            ID                              // built-in Acyclic trait entity (index 22)
	finalID              ID                              // built-in Final trait entity (index 23)
	oneOfID              ID                              // built-in OneOf trait entity (index 24)
	singletonID          ID                              // built-in Singleton trait entity (index 25)
	writeOnceID          ID                              // built-in WriteOnce trait entity (index 26)
	traversableID        ID                              // built-in Traversable trait entity (index 27)
	relationshipID       ID                              // built-in Relationship usage-constraint trait entity (index 28)
	targetID             ID                              // built-in Target usage-constraint trait entity (index 29)
	traitID              ID                              // built-in Trait usage-constraint trait entity (index 30)
	pairIsTagID          ID                              // built-in PairIsTag trait entity (index 31)
	withID               ID                              // built-in With trait entity (index 32)
	orderedChildrenID    ID                              // built-in OrderedChildren trait entity (index 33)
	dontFragmentID       ID                              // built-in DontFragment trait entity (index 35)
	disabledID           ID                              // built-in Disabled tag entity (index 36)
	prefabID             ID                              // built-in Prefab tag entity (index 37)
	wildcardID           ID                              // built-in Wildcard query-term sentinel (index 38; *)
	anyID                ID                              // built-in Any query-term sentinel (index 39; _)
	eventOnAddID         ID                              // built-in EventOnAdd event entity (index 40)
	eventOnSetID         ID                              // built-in EventOnSet event entity (index 41)
	eventOnRemoveID      ID                              // built-in EventOnRemove event entity (index 42)
	eventOnTableCreateID ID                              // built-in EventOnTableCreate event entity (index 43)
	eventTagID           ID                              // built-in Event tag entity (index 44)
	dependsOnID          ID                              // built-in DependsOn relationship entity (index 45)
	eventMonitorID       ID                              // built-in EventMonitor event entity (index 46)
	slotOfID             ID                              // built-in SlotOf relationship entity (index 47)
	// Built-in unit entities (indices 48–62); atomic units (Phase 16.30).
	meterID       ID // built-in Meter length unit entity (index 48)
	kiloMeterID   ID // built-in KiloMeter length unit entity (index 49)
	milliMeterID  ID // built-in MilliMeter length unit entity (index 50)
	secondID      ID // built-in Second duration unit entity (index 51)
	milliSecondID ID // built-in MilliSecond duration unit entity (index 52)
	minuteID      ID // built-in Minute duration unit entity (index 53)
	hourID        ID // built-in Hour duration unit entity (index 54)
	gramID        ID // built-in Gram mass unit entity (index 55)
	kiloGramID    ID // built-in KiloGram mass unit entity (index 56)
	megaGramID    ID // built-in MegaGram mass unit entity (index 57)
	newtonID      ID // built-in Newton force unit entity (index 58, opaque root)
	jouleID       ID // built-in Joule energy unit entity (index 59, opaque root)
	hertzID       ID // built-in Hertz frequency unit entity (index 60, opaque root)
	radianID      ID // built-in Radian angle unit entity (index 61)
	degreeID      ID // built-in Degree angle unit entity (index 62)
	// Built-in compound unit entities (indices 63–72); user entities start at index 73.
	meterPerSecondID        ID // built-in MeterPerSecond velocity unit (index 63)
	kiloMeterPerHourID      ID // built-in KiloMeterPerHour velocity unit (index 64)
	meterPerSecondSquaredID ID // built-in MeterPerSecondSquared acceleration unit (index 65)
	newtonCompoundID        ID // built-in NewtonCompound force unit kg·m/s² (index 66)
	jouleCompoundID         ID // built-in JouleCompound energy unit kg·m²/s² (index 67)
	wattID                  ID // built-in Watt power unit kg·m²/s³ (index 68)
	pascalID                ID // built-in Pascal pressure unit kg/(m·s²) (index 69)
	hertzCompoundID         ID // built-in HertzCompound frequency unit 1/s (index 70)
	radianPerSecondID       ID // built-in RadianPerSecond angular velocity unit (index 71)
	inverseID               ID // built-in Inverse reciprocal helper entity (index 72)
	// Units addon state
	unitDefs             map[ID]Unit                   // unit entity ID → Unit descriptor (built-in + user)
	compoundDefs         map[ID]*compoundDef           // compound unit entity ID → factor lists
	componentUnits       map[ID]ID                     // component entity ID → unit entity ID
	monitors             []*monitorObserver            // all registered monitor observers
	preUpdate            *Phase                        // built-in PreUpdate pipeline phase
	onUpdate             *Phase                        // built-in OnUpdate pipeline phase (NewSystem default)
	postUpdate           *Phase                        // built-in PostUpdate pipeline phase
	onFixedUpdate        *Phase                        // built-in OnFixedUpdate pipeline phase (accumulator loop)
	phases               []*Phase                      // all phases: built-in then custom, in registration order
	phaseOrder           []*Phase                      // cached topological order; rebuilt when pipelineDirty
	pipelineDirty        bool                          // true when phaseOrder/orderedSystems must be rebuilt
	withExpandStack      []ID                          // call-stack tracking for With co-add cycle detection
	cleanupPolicies      map[ID]cleanupPolicyFlags     // relationship entity → cleanup policy bits
	instantiatePolicies  map[ID]instantiatePolicyFlags // component entity → OnInstantiate policy bits
	exclusivePolicies    map[ID]bool                   // relationship entity → exclusive flag
	canTogglePolicies    map[ID]bool                   // component entity index → CanToggle flag
	symmetricPolicies    map[ID]bool                   // relationship entity index → symmetric flag
	transitivePolicies   map[ID]bool                   // relationship entity index → transitive flag
	reflexivePolicies    map[ID]bool                   // relationship entity index → reflexive flag
	acyclicPolicies      map[ID]bool                   // relationship entity index → acyclic flag
	finalPolicies        map[ID]bool                   // entity index → final flag
	oneOfPolicies        map[ID]ID                     // relationship entity index → required ChildOf parent (raw index)
	singletonPolicies    map[ID]bool                   // component entity index → singleton flag
	singletonInstances   map[ID]ID                     // component entity index → entity currently holding it
	writeOncePolicies    map[ID]bool                   // component entity index → writeOnce flag
	writeOnceHasBeenSet  map[writeOnceKey]bool         // per-(entity-index, component-ID) first-write tracking
	traversablePolicies  map[ID]bool                   // relationship entity index → traversable flag
	relationshipPolicies map[ID]bool                   // entity index → Relationship usage-constraint flag
	targetPolicies       map[ID]bool                   // entity index → Target usage-constraint flag
	traitPolicies        map[ID]bool                   // entity index → Trait usage-constraint flag
	pairIsTagPolicies    map[ID]bool                   // relationship entity index → PairIsTag flag
	orderedChildren      map[ID]*orderedChildList      // keyed by parent entity index; non-nil entry means ordered
	sparseID             ID                            // built-in Sparse trait entity (index 34)
	sparsePolicies       map[ID]bool                   // component entity index → sparse flag
	sparseStorage        map[ID]*sparseSet             // per-component sparse-set; backs both Sparse and DontFragment components
	sparseHeld           map[uint32][]ID               // entity raw-index → sparse/DontFragment component IDs held (for O(k) delete cleanup)
	dontFragmentPolicies map[ID]bool                   // component entity index → dontFragment flag
	unionPolicies        map[ID]bool                   // relationship entity index → union flag
	unionStore           map[ID]*unionRelStore         // relationship index → per-relationship union store
	exclusiveAccess      atomic.Uint64                 //nolint:unused // 0=unclaimed, goroutineID=owned, ^0=write-locked; see exclusive_access.go
	exclusiveThread      string                        //nolint:unused // human-readable label for the owner goroutine; set by ExclusiveAccessBegin
	stages               []*stage                      // stages[0] = main stage; stages[1..N] = worker stages
	workerStageWriters   []Writer                      // cached per-worker Writers; index i binds to stages[i+1]
	workerCount          int                           // number of persistent goroutines in the worker pool; 0 = serial
	workerCh             chan func()                   // job channel; nil when workerCount == 0
	inProgress           bool                          // true while Progress is executing
	time                 float32                       // total accumulated simulation time
	frameCount           uint64                        // number of Progress calls
	fixedTimestep        float32                       // fixed step size; 0 means disabled
	fixedAccumulator     float32                       // internal accumulator for fixed-step dispatch
	lastFramePhases      []PhaseStats                  // per-phase timing from the most recent Progress call; one entry per phase in topo order
	logger               *slog.Logger                  // optional structured logger; nil means no logging
	dynamicMarshalers    map[ID]dynamicMarshalHooks    // optional custom JSON hooks for dynamic components
	// inheritorCache caches BFS-ordered inheritor slices keyed by prefab entity.
	// Evicted in full whenever any (IsA, *) pair is added or removed — correct for
	// transitive chains (e.g. C IsA B IsA P: adding C invalidates P's entry too).
	inheritorCache map[ID][]ID
	preMergeHooks  []func(*Writer) // nil slots are tombstones; slice index = registration ID
	postMergeHooks []func(*Writer) // nil slots are tombstones; slice index = registration ID
	alertDefs      []*alertDef
	alertInstances map[alertKey]*AlertInstance
	// Timer addon fields (Phase 16.36).
	timerComponentID      ID // lazy-cached; 0 until first NewTimer/NewInterval/etc. call
	rateFilterComponentID ID // lazy-cached; 0 until first NewRateFilter call
	// Stats addon fields — protected by statsMu except scratch fields (statsTickDidRun,
	// statsTickDuration on System) which are only accessed from the Progress goroutine.
	statsMu             sync.RWMutex
	statsLastDelta      float64
	statsEntityCount    int
	statsTableCount     int
	statsFrameCount     uint64
	statsTotalTime      float64
	statsPhaseSnapshot  []PhaseStats  // cached per-phase snapshot, rebuilt in statsCommit
	statsSystemSnapshot []SystemStats // cached per-system snapshot, rebuilt in statsCommit
	// Stats aggregator windows — protected by statsMu.
	// statsAggTickCount is the total number of ticks committed to the second window.
	// Second window records every tick; minute window reduces every 60 ticks;
	// hour window reduces every 3600 ticks.
	statsAggTickCount int
	statsAggWorldSec  worldAggWindows
	statsAggWorldMin  worldAggWindows
	statsAggWorldHour worldAggWindows
	statsAggPipeSec   pipelineAggWindows
	statsAggPipeMin   pipelineAggWindows
	statsAggPipeHour  pipelineAggWindows
	// builtinByName maps canonical built-in entity names (e.g. "ChildOf", "Disabled")
	// to their entity IDs. Checked by LookupChild before the component scan so that
	// built-in entities are DSL-resolvable without holding a Name component (which
	// would create an extra table and affect yield-existing observers).
	builtinByName map[string]ID
}

// New initializes and returns an empty World.
//
// Built-in entity allocation order:
//   - Index 0: null sentinel (never issued by Alloc)
//   - Index 1: ChildOf built-in relationship entity
//   - Index 2: IsA built-in relationship entity
//   - Index 3: Name built-in component entity
//   - Index 4: PreUpdate built-in pipeline phase entity
//   - Index 5: OnUpdate built-in pipeline phase entity
//   - Index 6: PostUpdate built-in pipeline phase entity
//   - Index 7: OnFixedUpdate built-in pipeline phase entity
//   - Index 8: OnInstantiate built-in relationship entity
//   - Index 9: Inherit built-in trait entity
//   - Index 10: Override built-in trait entity
//   - Index 11: DontInherit built-in trait entity
//   - Index 12: OnDelete built-in cleanup trait relationship entity
//   - Index 13: OnDeleteTarget built-in cleanup trait relationship entity
//   - Index 14: RemoveAction built-in cleanup action entity
//   - Index 15: DeleteAction built-in cleanup action entity
//   - Index 16: PanicAction built-in cleanup action entity
//   - Index 17: Exclusive built-in trait entity
//   - Index 18: CanToggle built-in trait entity
//   - Index 19: Symmetric built-in trait entity
//   - Index 20: Transitive built-in trait entity
//   - Index 21: Reflexive built-in trait entity
//   - Index 22: Acyclic built-in trait entity
//   - Index 23: Final built-in trait entity
//   - Index 24: OneOf built-in trait entity
//   - Index 25: Singleton built-in trait entity
//   - Index 26: WriteOnce built-in trait entity
//   - Index 27: Traversable built-in trait entity
//   - Index 28: Relationship built-in usage-constraint trait entity
//   - Index 29: Target built-in usage-constraint trait entity
//   - Index 30: Trait built-in usage-constraint trait entity
//   - Index 31: PairIsTag built-in relationship trait entity
//   - Index 32: With built-in relationship trait entity
//   - Index 33: OrderedChildren built-in trait entity
//   - Index 34: Sparse built-in trait entity
//   - Index 35: DontFragment built-in trait entity
//   - Index 36: Disabled built-in tag entity
//   - Index 37: Prefab built-in tag entity
//   - Index 38: Wildcard built-in query-term sentinel (*)
//   - Index 39: Any built-in query-term sentinel (_)
//   - Index 40: EventOnAdd built-in event entity
//   - Index 41: EventOnSet built-in event entity
//   - Index 42: EventOnRemove built-in event entity
//   - Index 43: EventOnTableCreate built-in event entity
//   - Index 44: Event built-in tag entity (marks an entity as an event identifier)
//   - Index 45: DependsOn built-in relationship entity (bootstrapped with Relationship + PairIsTag)
//   - Index 46: EventMonitor built-in event entity
//   - Index 47: SlotOf built-in relationship entity (bootstrapped with Exclusive + PairIsTag + Relationship)
//   - Index 48: Meter built-in length unit entity
//   - Index 49: KiloMeter built-in length unit entity (Factor=1000, Base=Meter)
//   - Index 50: MilliMeter built-in length unit entity (Factor=0.001, Base=Meter)
//   - Index 51: Second built-in duration unit entity
//   - Index 52: MilliSecond built-in duration unit entity (Factor=0.001, Base=Second)
//   - Index 53: Minute built-in duration unit entity (Factor=60, Base=Second)
//   - Index 54: Hour built-in duration unit entity (Factor=3600, Base=Second)
//   - Index 55: Gram built-in mass unit entity
//   - Index 56: KiloGram built-in mass unit entity (Factor=1000, Base=Gram)
//   - Index 57: MegaGram built-in mass unit entity (Factor=1_000_000, Base=Gram)
//   - Index 58: Newton built-in force unit entity (opaque root; compound alias at index 66)
//   - Index 59: Joule built-in energy unit entity (opaque root; compound alias at index 67)
//   - Index 60: Hertz built-in frequency unit entity (opaque root; compound alias at index 70)
//   - Index 61: Radian built-in angle unit entity
//   - Index 62: Degree built-in angle unit entity (Factor=math.Pi/180, Base=Radian)
//   - Index 63: MeterPerSecond built-in velocity unit entity (compound: m/s)
//   - Index 64: KiloMeterPerHour built-in velocity unit entity (compound: km/h)
//   - Index 65: MeterPerSecondSquared built-in acceleration unit entity (compound: m/s²)
//   - Index 66: NewtonCompound built-in force unit entity (compound: kg·m/s², Symbol="N")
//   - Index 67: JouleCompound built-in energy unit entity (compound: kg·m²/s², Symbol="J")
//   - Index 68: Watt built-in power unit entity (compound: kg·m²/s³, Symbol="W")
//   - Index 69: Pascal built-in pressure unit entity (compound: kg/(m·s²), Symbol="Pa")
//   - Index 70: HertzCompound built-in frequency unit entity (compound: 1/s, Symbol="Hz")
//   - Index 71: RadianPerSecond built-in angular velocity unit entity (compound: rad/s)
//   - Index 72: Inverse built-in reciprocal helper entity (opaque marker)
//   - Index 73+: user entities (NewEntity)
func New() *World {
	w := &World{
		index:     entityindex.New(),
		registry:  component.NewRegistry(),
		tables:    make(map[string]*table.Table),
		compIndex: componentindex.New(),
	}
	w.empty = table.New([]ID{}, []*component.TypeInfo{})
	w.tables[sigKey(nil)] = w.empty
	for _, id := range w.empty.Type() {
		w.compIndex.Register(id, w.empty)
	}
	w.notifyTableCreated(w.empty) // no-op: cachedQueries is nil at construction
	// Allocate the built-in ChildOf relationship entity (gets index 1).
	childOf := w.index.Alloc()
	rec := w.index.Get(childOf)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(childOf))
	w.childOfID = childOf
	// Allocate the built-in IsA relationship entity (gets index 2).
	isA := w.index.Alloc()
	rec = w.index.Get(isA)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(isA))
	w.isAID = isA
	// Register the built-in Name component (gets index 3).
	w.nameID = RegisterComponent[Name](w)
	// Allocate the built-in PreUpdate phase entity (gets index 4).
	preUpdate := w.index.Alloc()
	rec = w.index.Get(preUpdate)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(preUpdate))
	w.preUpdateID = preUpdate
	// Allocate the built-in OnUpdate phase entity (gets index 5).
	onUpdate := w.index.Alloc()
	rec = w.index.Get(onUpdate)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(onUpdate))
	w.onUpdateID = onUpdate
	// Allocate the built-in PostUpdate phase entity (gets index 6).
	postUpdate := w.index.Alloc()
	rec = w.index.Get(postUpdate)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(postUpdate))
	w.postUpdateID = postUpdate
	// Allocate the built-in OnFixedUpdate phase entity (gets index 7).
	onFixedUpdate := w.index.Alloc()
	rec = w.index.Get(onFixedUpdate)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(onFixedUpdate))
	w.onFixedUpdateID = onFixedUpdate
	// Allocate the built-in OnInstantiate relationship entity (gets index 8).
	onInstantiate := w.index.Alloc()
	rec = w.index.Get(onInstantiate)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(onInstantiate))
	w.onInstantiateID = onInstantiate
	// Allocate the built-in Inherit trait entity (gets index 9).
	inherit := w.index.Alloc()
	rec = w.index.Get(inherit)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(inherit))
	w.inheritID = inherit
	// Allocate the built-in Override trait entity (gets index 10).
	override := w.index.Alloc()
	rec = w.index.Get(override)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(override))
	w.overrideID = override
	// Allocate the built-in DontInherit trait entity (gets index 11).
	dontInherit := w.index.Alloc()
	rec = w.index.Get(dontInherit)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(dontInherit))
	w.dontInheritID = dontInherit
	// Allocate the built-in OnDelete cleanup trait relationship entity (gets index 12).
	onDelete := w.index.Alloc()
	rec = w.index.Get(onDelete)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(onDelete))
	w.onDeleteID = onDelete
	// Allocate the built-in OnDeleteTarget cleanup trait relationship entity (gets index 13).
	onDeleteTarget := w.index.Alloc()
	rec = w.index.Get(onDeleteTarget)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(onDeleteTarget))
	w.onDeleteTargetID = onDeleteTarget
	// Allocate the built-in Remove cleanup action entity (gets index 14).
	removeAction := w.index.Alloc()
	rec = w.index.Get(removeAction)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(removeAction))
	w.removeActionID = removeAction
	// Allocate the built-in Delete cleanup action entity (gets index 15).
	deleteAction := w.index.Alloc()
	rec = w.index.Get(deleteAction)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(deleteAction))
	w.deleteActionID = deleteAction
	// Allocate the built-in Panic cleanup action entity (gets index 16).
	panicAction := w.index.Alloc()
	rec = w.index.Get(panicAction)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(panicAction))
	w.panicActionID = panicAction
	// Allocate the built-in Exclusive trait entity (gets index 17).
	exclusive := w.index.Alloc()
	rec = w.index.Get(exclusive)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(exclusive))
	w.exclusiveID = exclusive
	// Allocate the built-in CanToggle trait entity (gets index 18).
	canToggle := w.index.Alloc()
	rec = w.index.Get(canToggle)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(canToggle))
	w.canToggleID = canToggle
	// Allocate the built-in Symmetric trait entity (gets index 19).
	symmetric := w.index.Alloc()
	rec = w.index.Get(symmetric)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(symmetric))
	w.symmetricID = symmetric
	// Allocate the built-in Transitive trait entity (gets index 20).
	transitive := w.index.Alloc()
	rec = w.index.Get(transitive)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(transitive))
	w.transitiveID = transitive
	// Allocate the built-in Reflexive trait entity (gets index 21).
	reflexive := w.index.Alloc()
	rec = w.index.Get(reflexive)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(reflexive))
	w.reflexiveID = reflexive
	// Allocate the built-in Acyclic trait entity (gets index 22).
	acyclic := w.index.Alloc()
	rec = w.index.Get(acyclic)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(acyclic))
	w.acyclicID = acyclic
	// Allocate the built-in Final trait entity (gets index 23).
	final_ := w.index.Alloc()
	rec = w.index.Get(final_)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(final_))
	w.finalID = final_
	// Allocate the built-in OneOf trait entity (gets index 24).
	oneOf := w.index.Alloc()
	rec = w.index.Get(oneOf)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(oneOf))
	w.oneOfID = oneOf
	// Allocate the built-in Singleton trait entity (gets index 25).
	singleton := w.index.Alloc()
	rec = w.index.Get(singleton)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(singleton))
	w.singletonID = singleton
	// Allocate the built-in WriteOnce trait entity (gets index 26).
	writeOnce := w.index.Alloc()
	rec = w.index.Get(writeOnce)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(writeOnce))
	w.writeOnceID = writeOnce
	// Allocate the built-in Traversable trait entity (gets index 27).
	traversable := w.index.Alloc()
	rec = w.index.Get(traversable)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(traversable))
	w.traversableID = traversable
	// Allocate the built-in Relationship usage-constraint trait entity (gets index 28).
	relationship := w.index.Alloc()
	rec = w.index.Get(relationship)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(relationship))
	w.relationshipID = relationship
	// Allocate the built-in Target usage-constraint trait entity (gets index 29).
	target := w.index.Alloc()
	rec = w.index.Get(target)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(target))
	w.targetID = target
	// Allocate the built-in Trait usage-constraint trait entity (gets index 30).
	trait := w.index.Alloc()
	rec = w.index.Get(trait)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(trait))
	w.traitID = trait
	// Allocate the built-in PairIsTag trait entity (gets index 31).
	pairIsTag := w.index.Alloc()
	rec = w.index.Get(pairIsTag)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(pairIsTag))
	w.pairIsTagID = pairIsTag
	// Allocate the built-in With trait entity (gets index 32).
	with := w.index.Alloc()
	rec = w.index.Get(with)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(with))
	w.withID = with
	// Allocate the built-in OrderedChildren trait entity (gets index 33).
	orderedChildren := w.index.Alloc()
	rec = w.index.Get(orderedChildren)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(orderedChildren))
	w.orderedChildrenID = orderedChildren
	// Allocate the built-in Sparse trait entity (gets index 34).
	sparse := w.index.Alloc()
	rec = w.index.Get(sparse)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(sparse))
	w.sparseID = sparse
	// Allocate the built-in DontFragment trait entity (gets index 35).
	dontFragment := w.index.Alloc()
	rec = w.index.Get(dontFragment)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(dontFragment))
	w.dontFragmentID = dontFragment
	// Allocate the built-in Disabled tag entity (gets index 36).
	disabled := w.index.Alloc()
	rec = w.index.Get(disabled)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(disabled))
	w.disabledID = disabled
	// Allocate the built-in Prefab tag entity (gets index 37).
	prefab := w.index.Alloc()
	rec = w.index.Get(prefab)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(prefab))
	w.prefabID = prefab
	// Allocate the built-in Wildcard query-term sentinel (gets index 38).
	wildcard := w.index.Alloc()
	rec = w.index.Get(wildcard)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(wildcard))
	w.wildcardID = wildcard
	// Allocate the built-in Any query-term sentinel (gets index 39).
	any_ := w.index.Alloc()
	rec = w.index.Get(any_)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(any_))
	w.anyID = any_
	// Allocate the built-in EventOnAdd event entity (gets index 40).
	eventOnAdd := w.index.Alloc()
	rec = w.index.Get(eventOnAdd)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(eventOnAdd))
	w.eventOnAddID = eventOnAdd
	// Allocate the built-in EventOnSet event entity (gets index 41).
	eventOnSet := w.index.Alloc()
	rec = w.index.Get(eventOnSet)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(eventOnSet))
	w.eventOnSetID = eventOnSet
	// Allocate the built-in EventOnRemove event entity (gets index 42).
	eventOnRemove := w.index.Alloc()
	rec = w.index.Get(eventOnRemove)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(eventOnRemove))
	w.eventOnRemoveID = eventOnRemove
	// Allocate the built-in EventOnTableCreate event entity (gets index 43).
	eventOnTableCreate := w.index.Alloc()
	rec = w.index.Get(eventOnTableCreate)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(eventOnTableCreate))
	w.eventOnTableCreateID = eventOnTableCreate
	// Allocate the built-in Event tag entity (gets index 44).
	eventTag := w.index.Alloc()
	rec = w.index.Get(eventTag)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(eventTag))
	w.eventTagID = eventTag
	// Allocate the built-in DependsOn relationship entity (gets index 45).
	// Bootstrapped with Relationship + PairIsTag (minimum for v1; Traversable and
	// OnInstantiate/Inherit deferred to a follow-up phase per issue #197 design).
	dependsOn := w.index.Alloc()
	rec = w.index.Get(dependsOn)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(dependsOn))
	w.dependsOnID = dependsOn
	// Allocate the built-in EventMonitor event entity (gets index 46).
	eventMonitor := w.index.Alloc()
	rec = w.index.Get(eventMonitor)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(eventMonitor))
	w.eventMonitorID = eventMonitor
	// Allocate the built-in SlotOf relationship entity (gets index 47).
	slotOf := w.index.Alloc()
	rec = w.index.Get(slotOf)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(slotOf))
	w.slotOfID = slotOf
	// Allocate the built-in Units addon entities (indices 48–72).
	// User entity allocation starts at index 73 after this point.
	meter := w.index.Alloc()
	rec = w.index.Get(meter)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(meter))
	w.meterID = meter
	kiloMeter := w.index.Alloc()
	rec = w.index.Get(kiloMeter)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(kiloMeter))
	w.kiloMeterID = kiloMeter
	milliMeter := w.index.Alloc()
	rec = w.index.Get(milliMeter)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(milliMeter))
	w.milliMeterID = milliMeter
	second := w.index.Alloc()
	rec = w.index.Get(second)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(second))
	w.secondID = second
	milliSecond := w.index.Alloc()
	rec = w.index.Get(milliSecond)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(milliSecond))
	w.milliSecondID = milliSecond
	minute := w.index.Alloc()
	rec = w.index.Get(minute)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(minute))
	w.minuteID = minute
	hour := w.index.Alloc()
	rec = w.index.Get(hour)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(hour))
	w.hourID = hour
	gram := w.index.Alloc()
	rec = w.index.Get(gram)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(gram))
	w.gramID = gram
	kiloGram := w.index.Alloc()
	rec = w.index.Get(kiloGram)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(kiloGram))
	w.kiloGramID = kiloGram
	megaGram := w.index.Alloc()
	rec = w.index.Get(megaGram)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(megaGram))
	w.megaGramID = megaGram
	newton := w.index.Alloc()
	rec = w.index.Get(newton)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(newton))
	w.newtonID = newton
	joule := w.index.Alloc()
	rec = w.index.Get(joule)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(joule))
	w.jouleID = joule
	hertz := w.index.Alloc()
	rec = w.index.Get(hertz)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(hertz))
	w.hertzID = hertz
	radian := w.index.Alloc()
	rec = w.index.Get(radian)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(radian))
	w.radianID = radian
	degree := w.index.Alloc()
	rec = w.index.Get(degree)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(degree))
	w.degreeID = degree
	// Allocate the built-in compound unit entities (indices 63–72).
	meterPerSecond := w.index.Alloc()
	rec = w.index.Get(meterPerSecond)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(meterPerSecond))
	w.meterPerSecondID = meterPerSecond
	kiloMeterPerHour := w.index.Alloc()
	rec = w.index.Get(kiloMeterPerHour)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(kiloMeterPerHour))
	w.kiloMeterPerHourID = kiloMeterPerHour
	meterPerSecondSquared := w.index.Alloc()
	rec = w.index.Get(meterPerSecondSquared)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(meterPerSecondSquared))
	w.meterPerSecondSquaredID = meterPerSecondSquared
	newtonCompound := w.index.Alloc()
	rec = w.index.Get(newtonCompound)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(newtonCompound))
	w.newtonCompoundID = newtonCompound
	jouleCompound := w.index.Alloc()
	rec = w.index.Get(jouleCompound)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(jouleCompound))
	w.jouleCompoundID = jouleCompound
	watt := w.index.Alloc()
	rec = w.index.Get(watt)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(watt))
	w.wattID = watt
	pascal := w.index.Alloc()
	rec = w.index.Get(pascal)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(pascal))
	w.pascalID = pascal
	hertzCompound := w.index.Alloc()
	rec = w.index.Get(hertzCompound)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(hertzCompound))
	w.hertzCompoundID = hertzCompound
	radianPerSecond := w.index.Alloc()
	rec = w.index.Get(radianPerSecond)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(radianPerSecond))
	w.radianPerSecondID = radianPerSecond
	inverse := w.index.Alloc()
	rec = w.index.Get(inverse)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(inverse))
	w.inverseID = inverse
	// Bootstrap unit addon: populate unitDefs for the 25 built-in unit entities.
	bootstrapBuiltinUnits(w)
	// Bootstrap the ChildOf cascade-delete policy via the general cleanup mechanism.
	// (ChildOf, OnDeleteTarget) = Delete: deleting a parent cascades to all children.
	// This mirrors C src/bootstrap.c:705 where cr_childof_wildcard->flags gets
	// EcsIdOnDeleteTargetDelete. After this call deleteImmediate uses the general
	// policy loop rather than a hardcoded ChildOf branch.
	applyCleanupPolicy(w, w.childOfID, w.onDeleteTargetID, w.deleteActionID)
	// Bootstrap Exclusive on built-in relationships that must enforce single-target
	// invariants, matching C bootstrap.c:1259-1262. IsA is intentionally excluded —
	// C allows multiple prefab bases per instance.
	applyExclusivePolicy(w, w.childOfID)
	applyExclusivePolicy(w, w.onDeleteID)
	applyExclusivePolicy(w, w.onDeleteTargetID)
	applyExclusivePolicy(w, w.onInstantiateID)
	applyExclusivePolicy(w, w.slotOfID) // mirrors C bootstrap.c:1324
	// Bootstrap IsA as reflexive, matching C src/bootstrap.c:1321.
	// This makes HasID(a, MakePair(IsA, a)) return true for any alive entity a
	// (deliberate divergence from C ecs_has_id; documented in CHANGELOG).
	applyReflexivePolicy(w, w.isAID)
	// Bootstrap ChildOf and IsA as Traversable, mirroring C bootstrap.c:1063,1315-1316.
	// Traversable implies Acyclic (bootstrap.c:1295-1296), so this also makes both
	// relationships acyclic. IsA becomes Acyclic for the first time in Go flecs
	// as a side effect; see CHANGELOG v0.46.0 for the behavior-change note.
	applyTraversablePolicy(w, w.childOfID)
	applyTraversablePolicy(w, w.isAID)
	// Bootstrap Relationship on built-in relationships, mirroring C bootstrap.c:1280-1288.
	// Identifier is upstream but not yet ported (skip).
	applyRelationshipPolicy(w, w.isAID)
	applyRelationshipPolicy(w, w.childOfID)
	applyRelationshipPolicy(w, w.onDeleteID)
	applyRelationshipPolicy(w, w.onDeleteTargetID)
	applyRelationshipPolicy(w, w.onInstantiateID)
	applyRelationshipPolicy(w, w.withID)
	applyRelationshipPolicy(w, w.dependsOnID)
	applyRelationshipPolicy(w, w.slotOfID) // mirrors C bootstrap.c:1282
	// Bootstrap Target on built-in target entities, mirroring C bootstrap.c:1291-1293.
	// Note: Remove, Delete, Cascade, Throw are NOT marked Target in upstream.
	applyTargetPolicy(w, w.overrideID)
	applyTargetPolicy(w, w.inheritID)
	applyTargetPolicy(w, w.dontInheritID)
	// Bootstrap Trait on ChildOf and IsA, mirroring C bootstrap.c:1060-1061.
	// This permits patterns like (SomeRel, ChildOf) where ChildOf appears in
	// the target slot despite having the Relationship trait.
	applyTraitPolicy(w, w.isAID)
	applyTraitPolicy(w, w.childOfID)
	// Bootstrap PairIsTag on IsA, ChildOf, DependsOn, and SlotOf, mirroring C bootstrap.c:1272-1273,1283.
	// Flag is upstream but not yet ported; skip.
	applyPairIsTagPolicy(w, w.isAID)
	applyPairIsTagPolicy(w, w.childOfID)
	applyPairIsTagPolicy(w, w.dependsOnID)
	applyPairIsTagPolicy(w, w.slotOfID) // mirrors C bootstrap.c:1274
	// Bootstrap DontInherit on the Prefab tag so that entities inheriting from a
	// prefab via IsA do NOT acquire the Prefab tag themselves. Mirrors C
	// ecs_add_pair(world, EcsPrefab, EcsOnInstantiate, EcsDontInherit) at
	// bootstrap.c:1308.
	applyInstantiatePolicy(w, w.prefabID, w.dontInheritID)
	// Bootstrap DontInherit on the Disabled tag so that prefabs with disabled
	// sub-entities do not force instances to also be disabled. Mirrors C
	// ecs_add_pair(world, EcsDisabled, EcsOnInstantiate, EcsDontInherit) at
	// bootstrap.c (same pattern as Prefab).
	applyInstantiatePolicy(w, w.disabledID, w.dontInheritID)
	// Create the built-in pipeline phases and establish the default ordering chain:
	// PreUpdate → OnFixedUpdate → OnUpdate → PostUpdate.
	// Built-in phases are not entity-backed in v1; they are pure Go structs.
	w.preUpdate = &Phase{name: "PreUpdate", w: w, enabled: true}
	w.onFixedUpdate = &Phase{name: "OnFixedUpdate", w: w, enabled: true}
	w.onUpdate = &Phase{name: "OnUpdate", w: w, enabled: true}
	w.postUpdate = &Phase{name: "PostUpdate", w: w, enabled: true}
	w.onFixedUpdate.predecessors = []*Phase{w.preUpdate}
	w.onUpdate.predecessors = []*Phase{w.onFixedUpdate}
	w.postUpdate.predecessors = []*Phase{w.onUpdate}
	w.phases = []*Phase{w.preUpdate, w.onFixedUpdate, w.onUpdate, w.postUpdate}
	// Set the initial topo order directly (known: linear built-in chain, no custom phases).
	w.phaseOrder = []*Phase{w.preUpdate, w.onFixedUpdate, w.onUpdate, w.postUpdate}
	w.pipelineDirty = false
	// Initialize stage 0 (main stage) and bind the cached write capability to it.
	s0 := &stage{id: 0, queue: acquireCmdQueue(), world: w}
	w.stages = []*stage{s0}
	// Initialize cached capability pointers — these avoid per-call heap allocation.
	w.writeCapability.Reader.world = w
	w.writeCapability.stage = s0
	w.readCapability.world = w
	// Register canonical names for built-in entities in a lightweight index so
	// they are resolvable via w.Lookup without adding a Name component (which
	// would create a table and affect yield-existing observers).
	w.builtinByName = map[string]ID{
		"ChildOf":  w.childOfID,
		"IsA":      w.isAID,
		"Disabled": w.disabledID,
		"Prefab":   w.prefabID,
		"Wildcard": w.wildcardID,
		"Any":      w.anyID,
	}
	return w
}

// OnDelete returns the ID of the built-in OnDelete cleanup trait relationship entity.
//
// OnDelete is a trait relationship. To apply a cleanup policy to a component or
// relationship entity, add a pair (OnDelete, action) to it:
//
//	flecs.SetCleanupPolicy(w, myRelID, w.OnDelete(), w.DeleteAction())
//
// Or via Writer.AddID:
//
//	fw.AddID(myRelID, flecs.MakePair(w.OnDelete(), w.DeleteAction()))
//
// The action is one of RemoveAction() (default), DeleteAction(), or PanicAction().
// OnDelete governs what happens to source entities when the relationship/component
// entity itself is deleted.
//
// Note: if Panic fires mid-cascade the world is in a halted state; recovery is
// not attempted. Document this contract to callers of PanicAction.
func (w *World) OnDelete() ID { return w.onDeleteID }

// OnDeleteTarget returns the ID of the built-in OnDeleteTarget cleanup trait
// relationship entity.
//
// OnDeleteTarget is a trait relationship. To apply a cleanup policy when a target
// is deleted, add a pair (OnDeleteTarget, action) to the relationship entity:
//
//	flecs.SetCleanupPolicy(w, likesID, w.OnDeleteTarget(), w.DeleteAction())
//
// Or via Writer.AddID:
//
//	fw.AddID(likesID, flecs.MakePair(w.OnDeleteTarget(), w.DeleteAction()))
//
// The action is one of RemoveAction() (default), DeleteAction(), or PanicAction().
// OnDeleteTarget governs what happens to entities that have (relationship, target)
// when the target entity is deleted. ChildOf uses DeleteAction() by default,
// which is what drives the parent-cascade-delete behavior.
func (w *World) OnDeleteTarget() ID { return w.onDeleteTargetID }

// RemoveAction returns the ID of the built-in Remove cleanup action entity.
// Remove is the default action for both OnDelete and OnDeleteTarget: when the
// triggering entity is deleted, the component or pair is removed from sources
// without deleting the source entities themselves.
func (w *World) RemoveAction() ID { return w.removeActionID }

// DeleteAction returns the ID of the built-in Delete cleanup action entity.
// When used with OnDeleteTarget, deleting a target entity cascades to delete all
// source entities that hold (relationship, target). This is the policy installed
// on ChildOf by default, producing the parent-cascade-delete behavior.
func (w *World) DeleteAction() ID { return w.deleteActionID }

// PanicAction returns the ID of the built-in Panic cleanup action entity.
// When used with OnDeleteTarget, attempting to delete an entity that is the
// target of the relationship panics with a descriptive message identifying the
// relationship and the target entity. The world is left in a halted state; no
// recovery is performed.
func (w *World) PanicAction() ID { return w.panicActionID }

// PreUpdate returns the built-in PreUpdate pipeline phase.
// Systems in this phase run first in each Progress call.
func (w *World) PreUpdate() *Phase { return w.preUpdate }

// OnUpdate returns the built-in OnUpdate pipeline phase.
// Systems in this phase run after OnFixedUpdate. [NewSystem] defaults to this phase.
func (w *World) OnUpdate() *Phase { return w.onUpdate }

// PostUpdate returns the built-in PostUpdate pipeline phase.
// Systems in this phase run last in each Progress call.
func (w *World) PostUpdate() *Phase { return w.postUpdate }

// OnFixedUpdate returns the built-in OnFixedUpdate pipeline phase.
// Systems in this phase are dispatched via a fixed-step accumulator loop inside
// each Progress call and always receive the fixed timestep as dt.
// Use [World.SetFixedTimestep] to configure the step; a zero step disables this phase.
func (w *World) OnFixedUpdate() *Phase { return w.onFixedUpdate }

// DependsOn returns the ID of the built-in DependsOn relationship entity.
// DependsOn is bootstrapped with [World.Relationship] and [World.PairIsTag] traits.
//
// The primary ordering API is the typed method form ([Phase.DependsOn] and
// [System.DependsOn]). This accessor exposes the entity ID for pair queries and
// HasID checks (e.g. HasID(e, MakePair(w.DependsOn(), target))).
func (w *World) DependsOn() ID { return w.dependsOnID }

// EventOnAdd returns the ID of the built-in EventOnAdd event entity.
// Observers registered via Observe[T] or ObserveID for EventOnAdd subscribe
// to the same event entity as this accessor returns.
func (w *World) EventOnAdd() ID { return w.eventOnAddID }

// EventOnSet returns the ID of the built-in EventOnSet event entity.
func (w *World) EventOnSet() ID { return w.eventOnSetID }

// EventOnRemove returns the ID of the built-in EventOnRemove event entity.
func (w *World) EventOnRemove() ID { return w.eventOnRemoveID }

// EventOnTableCreate returns the ID of the built-in EventOnTableCreate event entity.
func (w *World) EventOnTableCreate() ID { return w.eventOnTableCreateID }

// EventMonitor returns the ID of the built-in EventMonitor event entity (index 46).
// Monitor observers registered via Monitor / MonitorWithOptions subscribe to this
// event kind. The entity is exposed for introspection and symmetry with other
// built-in event entities; direct observer dispatch is not performed via this ID.
func (w *World) EventMonitor() ID { return w.eventMonitorID }

// Event returns the ID of the built-in Event tag entity.
// RegisterEvent automatically adds this tag to every custom event entity,
// enabling HasID(eventID, w.Event()) discrimination.
func (w *World) Event() ID { return w.eventTagID }

// Time returns the total simulated time accumulated across all Progress calls.
func (w *World) Time() float32 { return w.time }

// FrameCount returns the number of Progress calls made on this world.
func (w *World) FrameCount() uint64 { return w.frameCount }

// FixedTimestep returns the current fixed step size. Zero means disabled.
func (w *World) FixedTimestep() float32 { return w.fixedTimestep }

// SetFixedTimestep sets the fixed step size used by the OnFixedUpdate accumulator.
// A step of 0 disables OnFixedUpdate dispatch entirely. Panics if step < 0.
//
// Spiral-of-death risk: if the fixed step is smaller than the average frame dt,
// each Progress call must run multiple fixed iterations. If fixed systems are
// slow, the total wall time per frame grows, causing more iterations next frame,
// creating a positive-feedback loop that stalls the simulation. No guard is
// provided; callers must ensure the fixed step is reachable within one frame.
func (w *World) SetFixedTimestep(step float32) {
	if step < 0 {
		panic("flecs: SetFixedTimestep: step must be >= 0")
	}
	w.fixedTimestep = step
}

// SetWorkerCount sets the number of worker goroutines in the persistent pool
// used for parallel system dispatch. Zero (the default) disables parallel
// dispatch; all systems run serially on the calling goroutine.
//
// When n > 0, a buffered channel of size 2*n is created and n goroutines are
// started. Systems flagged with SetParallel(true) whose write sets are pairwise
// disjoint will be dispatched as concurrent jobs within each phase.
//
// Changing n between Progress calls tears down the old pool (goroutines exit
// when the old channel is drained and closed) and starts a new pool.
//
// Calling SetWorkerCount during an active Progress call is a no-op.
//
// Panics if n < 0.
func (w *World) SetWorkerCount(n int) {
	if n < 0 {
		panic("flecs: SetWorkerCount: n must be >= 0")
	}
	if w.inProgress {
		return
	}
	if w.workerCh != nil {
		close(w.workerCh)
		w.workerCh = nil
	}
	// Release any excess worker stages (indices n+1 and beyond).
	for i := n + 1; i < len(w.stages); i++ {
		releaseCmdQueue(w.stages[i].queue)
		w.stages[i].queue = nil
	}
	if n+1 < len(w.stages) {
		w.stages = w.stages[:n+1]
	}
	w.workerCount = n
	if n > 0 {
		// Grow stages table to hold n worker stages (indices 1..n).
		for len(w.stages) <= n {
			id := len(w.stages)
			s := &stage{id: id, queue: acquireCmdQueue(), world: w, deferDepth: 1}
			w.stages = append(w.stages, s)
		}
		// Build the cached per-worker Writers (index i → stages[i+1]).
		w.workerStageWriters = make([]Writer, n)
		for i := 0; i < n; i++ {
			w.workerStageWriters[i] = Writer{
				Reader: Reader{world: w},
				stage:  w.stages[i+1],
			}
		}
		ch := make(chan func(), 2*n)
		w.workerCh = ch
		for range n {
			go func() {
				for fn := range ch {
					fn()
				}
			}()
		}
	}
}

// WorkerCount returns the current number of worker goroutines. Zero means
// serial dispatch (the default).
func (w *World) WorkerCount() int { return w.workerCount }

// newEntityInternal allocates a new entity without the exclusive-access check.
// Called by Writer.NewEntity which runs inside a Write scope where access is
// already guaranteed.
func (w *World) newEntityInternal() ID {
	e := w.index.Alloc()
	rec := w.index.Get(e)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(e))
	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "entity created",
			slog.Uint64("id", uint64(e)))
	}
	return e
}

// deleteOne removes a single entity from its archetype table and frees its ID.
// It is the primitive used by both Delete (non-parent case) and the cascade
// delete orchestrator. Returns true if e was alive.
// Fires OnRemove for each component in e's current table before removal.
func (w *World) deleteOne(e ID) bool {
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	t := rec.Table
	row := int(rec.Row)
	// Fire monitor exit events BEFORE any component removal so that callbacks
	// can still read the entity's components. Mirrors the 'no-catch-up on
	// re-enable' semantic: monitors see a clean exit, not a partial state.
	if len(w.monitors) > 0 {
		w.fireMonitorsOnDelete(e, t)
	}
	if t != nil {
		for _, id := range t.Type() {
			// Sparse-only components: data is in sparse-set; sparseHeld path below will
			// fire OnRemove with the correct sparse-set pointer. Skip here to avoid double-fire.
			if !id.IsPair() && w.sparsePolicies[ID(id.Index())] && !w.dontFragmentPolicies[ID(id.Index())] {
				continue
			}
			info, _ := w.registry.LookupByID(id)
			w.fireOnRemove(info, id, e, t.Get(row, id))
		}
		moved, ok := t.RemoveSwap(row)
		if ok {
			movedRec := w.index.Get(moved)
			movedRec.Row = uint32(row)
		}
	}
	// Clear any singleton slots held by this entity.
	for compIdx, holder := range w.singletonInstances {
		if holder.Index() == e.Index() {
			delete(w.singletonInstances, compIdx)
		}
	}
	// Clear any writeOnce hasBeenSet slots for this entity.
	for key := range w.writeOnceHasBeenSet {
		if key.entity == e.Index() {
			delete(w.writeOnceHasBeenSet, key)
		}
	}
	// Clean up ordered-children state. If e was an ordered parent, drop its list.
	// Also remove e from any other parent's ordered list it appears in.
	// Cascade-deleted children reach this path via deleteOne, so this covers them too.
	// Mirrors upstream src/on_delete.c:211-214.
	if w.orderedChildren != nil {
		delete(w.orderedChildren, ID(e.Index()))
		for _, list := range w.orderedChildren {
			removeFromOrderedList(list, e)
		}
	}
	// Clean up sparse-set entries for e. Fire OnRemove for each component e holds
	// as Sparse, then remove e from the respective sparse-set.
	// sparseHeld gives O(k) cleanup where k = number of sparse components on e.
	// Mirrors upstream src/storage/component_index.c:252-265 flecs_component_fini_sparse.
	if w.sparseHeld != nil {
		eIdx := uint32(e.Index())
		if held := w.sparseHeld[eIdx]; len(held) > 0 {
			// Snapshot held slice since sparseSetRemove modifies w.sparseHeld.
			heldSnap := append([]ID(nil), held...)
			for _, cid := range heldSnap {
				key := ID(cid.Index())
				if ss, ok := w.sparseStorage[key]; ok {
					slot, hasSlot := ss.index[e.Index()]
					if hasSlot {
						info, _ := w.registry.LookupByID(cid)
						w.fireOnRemove(info, cid, e, ss.dense[slot].data)
					}
				}
				sparseSetRemove(w, e, cid)
			}
		}
	}
	// Clean up union-store entries for e. For each union relationship that has an
	// active target stored for e, fire OnRemove and remove the entry.
	// This mirrors the sparseHeld cleanup above but uses the union side store.
	unionStoreRemoveEntity(w, e)
	// If e was a custom event entity, remove its observer entry so subsequent
	// Emit calls for e become no-ops. Custom events register with {id:e, eventEntity:e}.
	if w.observers != nil {
		delete(w.observers, observerKey{id: e, eventEntity: e})
	}
	freed := w.index.Free(e)
	if freed && w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "entity deleted",
			slog.Uint64("id", uint64(e)))
	}
	return freed
}

// Delete removes entity e and all entities related to it via (ChildOf, e) pairs,
// recursively. Deletion is post-order: children are deleted before their parents,
// leaves before any internal node.
//
// Returns true if e was alive. Returns false immediately with no cascade if e
// is not alive, preserving Phase 1.5 semantics.
//
// Within a deferred block, the operation is queued if e is currently alive;
// the cascade runs during flush in the order the Delete was queued.
//
// A cycle guard (seen map) prevents infinite loops for self-referential hierarchies.
func (w *World) Delete(e ID) bool {
	w.checkExclusiveAccessWrite()
	s0 := w.stages[0]
	if s0.deferDepth > 0 {
		if !w.index.IsAlive(e) {
			return false
		}
		s0.queue.append(cmd{kind: cmdDelete, entity: e})
		return true
	}
	return deleteImmediate(w, e)
}

func deleteImmediate(w *World, e ID) bool {
	if !w.index.IsAlive(e) {
		return false
	}

	// Collect e and all entities to cascade-delete via iterative DFS with cycle
	// detection. For each visited node, consult the cleanup policy registry:
	// relationships with OnDeleteTargetPanic fire immediately; relationships with
	// OnDeleteTargetDelete enqueue their sources for deletion. The default Remove
	// policy (no entry in cleanupPolicies) leaves sources alive with orphaned pairs.
	//
	// ChildOf uses OnDeleteTargetDelete (registered in New()), so the parent-cascade
	// behavior is preserved without a hardcoded branch.
	stack := []ID{e}
	var toDelete []ID
	seen := make(map[ID]struct{})
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := seen[node]; ok {
			continue
		}
		seen[node] = struct{}{}
		toDelete = append(toDelete, node)

		// Apply OnDeleteTarget policies for each relationship that has one.
		for relID, flags := range w.cleanupPolicies {
			if flags&(policyOnDeleteTargetDelete|policyOnDeleteTargetPanic) == 0 {
				continue
			}
			pairID := MakePair(relID, node)
			tables := w.compIndex.TablesFor(pairID)
			if len(tables) == 0 {
				continue
			}
			if flags&policyOnDeleteTargetPanic != 0 {
				// Identify a source entity for the panic message.
				var src ID
				for _, t := range tables {
					if es := t.Entities(); len(es) > 0 {
						src = es[0]
						break
					}
				}
				panic(fmt.Sprintf("flecs: cannot delete entity %v: it is a target of relationship %v (source entity: %v) which has OnDeleteTarget+Panic policy", node, relID, src))
			}
			// policyOnDeleteTargetDelete: cascade-delete all source entities.
			for _, t := range tables {
				// Snapshot the entity list before any deleteOne calls mutate the table.
				entities := append([]ID(nil), t.Entities()...)
				for _, src := range entities {
					if w.index.IsAlive(src) {
						stack = append(stack, src)
					}
				}
			}
		}
	}

	// If any entity being deleted is a union relationship, drop its entire union
	// store so that subsequent queries return no stale results. We don't fire
	// OnRemove for every entity that held the pair; the relationship itself is
	// going away and callers must handle the cascade.
	if w.unionStore != nil {
		for _, del := range toDelete {
			relKey := ID(del.Index())
			if w.unionPolicies[relKey] {
				delete(w.unionStore, relKey)
				delete(w.unionPolicies, relKey)
			}
		}
	}
	// Delete in post-order: deepest descendants first, root last.
	for i := len(toDelete) - 1; i >= 0; i-- {
		w.deleteOne(toDelete[i])
	}
	return true
}

// IsAlive reports whether e is currently alive.
func (w *World) IsAlive(e ID) bool {
	w.checkExclusiveAccessRead()
	return w.index.IsAlive(e)
}

// Count returns the number of currently alive entities (including component entities).
func (w *World) Count() int {
	w.checkExclusiveAccessRead()
	return w.index.Count()
}

// RegisterComponent registers T as a component-entity in w and returns its ID.
// Idempotent: if T is already registered with a component ID, returns that ID.
// The component itself is an entity, mirroring the flecs convention that
// components are first-class entities.
func RegisterComponent[T any](w *World) ID {
	w.checkExclusiveAccessWrite()
	info, ok := component.LookupByType[T](w.registry)
	if ok && info.Component != 0 {
		return info.Component
	}
	if !ok {
		info = component.Register[T](w.registry)
	}
	id := w.index.Alloc()
	w.registry.AssociateID(info, id)
	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "component registered",
			slog.String("name", info.Name),
			slog.Uint64("id", uint64(id)),
			slog.Uint64("size", uint64(info.Size)))
	}
	return id
}

// SetInheritable marks the component associated with cid as inheritable. Any
// subsequent query term involving cid will default to Self|Up traversal via
// IsA, so the term matches both entities that own cid locally and entities that
// inherit cid from a prefab via IsA. Terms that explicitly set traversal via
// Self(), Up(), SelfUp(), or Cascade() are unaffected.
//
// Must be called BEFORE any query referencing cid is constructed; calling it
// after a query is built produces undefined query-match behavior (the query
// uses whichever traversal was set at construction time).
//
// Panics if cid is not a registered component.
func (w *World) SetInheritable(cid ID) {
	info, ok := w.registry.LookupByID(cid)
	if !ok {
		panic(fmt.Sprintf("flecs: SetInheritable: component id %d is not registered", cid))
	}
	info.Inheritable = true
}

// SetInheritable marks the component type T as inheritable. T must have been
// registered with RegisterComponent first; panics otherwise.
//
// Equivalent to w.SetInheritable(RegisterComponent[T](w)) but verifies that T
// was already registered (the registration is not forced here to avoid
// accidentally creating component entities out of order).
func SetInheritable[T any](w *World) {
	info, ok := component.LookupByType[T](w.registry)
	if !ok || info.Component == 0 {
		panic(fmt.Sprintf("flecs: SetInheritable[%T]: component not registered; call RegisterComponent[T] first", *new(T)))
	}
	info.Inheritable = true
}

// setOnWorld writes value v as component T on entity e.
// Internal helper called by Writer.Set and the legacy World-based paths.
func setOnWorld[T any](w *World, e ID, v T) {
	s0 := w.stages[0]
	if s0.deferDepth > 0 {
		cid := RegisterComponent[T](w)
		info, _ := component.LookupByType[T](w.registry)
		if info.Size > 0 {
			off, buf := s0.queue.arena.alloc(int(info.Size), int(info.Align))
			copy(buf, unsafe.Slice((*byte)(unsafe.Pointer(&v)), info.Size))
			s0.queue.append(cmd{kind: cmdSetByID, entity: e, id: cid,
				valueOff: off, valueSize: uint32(info.Size)})
		} else {
			s0.queue.append(cmd{kind: cmdSetByID, entity: e, id: cid})
		}
		return
	}
	setImmediate[T](w, e, v)
}

func setImmediate[T any](w *World, e ID, v T) {
	cid := RegisterComponent[T](w)
	info, _ := component.LookupByType[T](w.registry)
	setImmediateByPtr(w, e, cid, unsafe.Pointer(&v), info)
}

// getOnWorld returns the value of component T on entity e.
// Internal helper; does not check exclusive access (called from within scopes).
func getOnWorld[T any](w *World, e ID) (T, bool) {
	var zero T
	info, ok := component.LookupByType[T](w.registry)
	if !ok || info.Component == 0 {
		return zero, false
	}
	// Sparse or DontFragment: fetch from per-component sparse-set.
	cIdx := ID(info.Component.Index())
	if w.sparsePolicies[cIdx] || w.dontFragmentPolicies[cIdx] {
		ptr := sparseSetGet(w, e, info.Component)
		if ptr == nil {
			return zero, false
		}
		return *(*T)(ptr), true
	}
	rec := w.index.Get(e)
	if rec == nil {
		return zero, false
	}
	t := rec.Table
	if t != nil && t.HasComponent(info.Component) {
		ptr := t.Get(int(rec.Row), info.Component)
		if ptr == nil {
			return zero, true
		}
		return *(*T)(ptr), true
	}
	return getViaIsA[T](w, e, info.Component, nil)
}

// hasOnWorld reports whether entity e has component T — locally or via an IsA chain.
// Internal helper; does not check exclusive access.
func hasOnWorld[T any](w *World, e ID) bool {
	cid := RegisterComponent[T](w)
	cIdx := ID(cid.Index())
	// DontFragment: component is not in the archetype type; check sparse-set index.
	// This also covers Sparse+DontFragment (the canonical combined pattern).
	if w.dontFragmentPolicies[cIdx] {
		ss, ok := w.sparseStorage[cIdx]
		if !ok {
			return false
		}
		_, has := ss.index[e.Index()]
		return has
	}
	// Sparse-only (no DontFragment): component IS in the archetype type; check table.
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	t := rec.Table
	if t != nil && t.HasComponent(cid) {
		return true
	}
	return hasViaIsA(w, e, cid, nil)
}

// ownsOnWorld reports whether entity e locally owns component T.
// Internal helper; does not check exclusive access.
func ownsOnWorld[T any](w *World, e ID) bool {
	cid := RegisterComponent[T](w)
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	return rec.Table != nil && rec.Table.HasComponent(cid)
}

// removeOnWorld removes component T from entity e.
// Internal helper; does not check exclusive access.
func removeOnWorld[T any](w *World, e ID) bool {
	s0 := w.stages[0]
	if s0.deferDepth > 0 {
		info, ok := component.LookupByType[T](w.registry)
		if !ok || info.Component == 0 {
			return false
		}
		cIdx := ID(info.Component.Index())
		// DontFragment (or Sparse+DontFragment): consult sparse-set; component not in archetype.
		if w.dontFragmentPolicies[cIdx] {
			ss, ok := w.sparseStorage[cIdx]
			if !ok {
				return false
			}
			if _, has := ss.index[e.Index()]; !has {
				return false
			}
			s0.queue.append(cmd{kind: cmdRemoveID, entity: e, id: info.Component})
			return true
		}
		// Sparse-only (no DontFragment): component IS in archetype; check via sparse-set
		// (canonical presence) and queue archetype removal.
		if w.sparsePolicies[cIdx] {
			ss, ok := w.sparseStorage[cIdx]
			if !ok {
				return false
			}
			if _, has := ss.index[e.Index()]; !has {
				return false
			}
			s0.queue.append(cmd{kind: cmdRemoveID, entity: e, id: info.Component})
			return true
		}
		rec := w.index.Get(e)
		if rec == nil || rec.Table == nil || !rec.Table.HasComponent(info.Component) {
			return false
		}
		s0.queue.append(cmd{kind: cmdRemoveID, entity: e, id: info.Component})
		return true
	}
	return removeImmediate[T](w, e)
}

func removeImmediate[T any](w *World, e ID) bool {
	info, ok := component.LookupByType[T](w.registry)
	if !ok || info.Component == 0 {
		return false
	}
	cid := info.Component
	cIdx := ID(cid.Index())
	// DontFragment (alone or with Sparse): remove from sparse-set; do NOT cause an
	// archetype transition (component was never in the archetype type).
	if w.dontFragmentPolicies[cIdx] {
		ptr := sparseSetGet(w, e, cid)
		if ptr == nil {
			return false
		}
		w.fireOnRemove(info, cid, e, ptr)
		sparseSetRemove(w, e, cid)
		// Fire sparse monitors AFTER removal so entityMatchesMonitorExcluding
		// sees the updated sparse-set state (no excludeID needed).
		if len(w.monitors) > 0 {
			w.fireSparseMonitors(e, cid, 0)
		}
		return true
	}
	// Sparse-only (no DontFragment): remove from sparse-set AND cause an archetype
	// transition (component IS in the archetype type, so removing it changes the type).
	if w.sparsePolicies[cIdx] {
		ptr := sparseSetGet(w, e, cid)
		if ptr == nil {
			return false
		}
		w.fireOnRemove(info, cid, e, ptr)
		sparseSetRemove(w, e, cid)
		// Remove from archetype type without re-firing OnRemove.
		rec := w.index.Get(e)
		if rec != nil && rec.Table != nil && rec.Table.HasComponent(cid) {
			w.migrateArchetypeOnly(e, 0, cid)
		}
		// Fire sparse monitors AFTER both sparse-set and archetype are updated.
		if len(w.monitors) > 0 {
			w.fireSparseMonitors(e, cid, 0)
		}
		return true
	}
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	t := rec.Table
	if t == nil || !t.HasComponent(cid) {
		return false
	}
	w.migrate(e, 0, cid, nil)
	return true
}

// migrate moves entity e to the archetype table for the new component set
// computed as: currentSet ∪ {addID} \ {removeID}.
//
// Either addID or removeID may be 0 (no-op for that side). copyValue, if non-nil,
// is the value to write for addID in the destination table; the old table's value
// for addID (if any) is NOT carried over — copyValue always wins.
//
// Row-tracking invariant: after RemoveSwap on the source table, the entity that
// was in the last row is now at the vacated row; its Record.Row is updated to
// reflect the new position. Failure to maintain this invariant causes subsequent
// Get/Set/Has/Remove operations to read/write the wrong row.
func (w *World) migrate(e ID, addID, removeID ID, copyValue unsafe.Pointer) {
	rec := w.index.Get(e)
	oldTable := rec.Table
	oldRow := int(rec.Row)

	var oldSig []ID
	if oldTable != nil {
		oldSig = oldTable.Type()
	}

	// Consult the edge cache BEFORE computing newSig so that cache hits pay
	// zero allocation cost (no make([]ID, ...) on the fast path).
	var newTable *table.Table
	switch {
	case removeID != 0 && addID == 0 && oldTable != nil:
		if dst, ok := oldTable.NextOnRemove(removeID); ok {
			newTable = dst
		}
	case addID != 0 && removeID == 0 && oldTable != nil:
		if dst, ok := oldTable.NextOnAdd(addID); ok {
			newTable = dst
		}
	}

	if newTable == nil {
		// Cache miss: compute the new sorted signature and look up (or create)
		// the destination table. Never mutate the slice returned by Type().
		newSig := make([]ID, 0, len(oldSig)+1)
		for _, id := range oldSig {
			if id == removeID {
				continue
			}
			newSig = append(newSig, id)
		}
		if addID != 0 {
			// Insert addID in sorted position to maintain the sorted-ascending invariant.
			pos := sort.Search(len(newSig), func(i int) bool { return newSig[i] >= addID })
			newSig = append(newSig, 0)
			copy(newSig[pos+1:], newSig[pos:])
			newSig[pos] = addID
		}

		key := sigKey(newSig)
		var exists bool
		newTable, exists = w.tables[key]
		if !exists {
			types := make([]*component.TypeInfo, len(newSig))
			for i, id := range newSig {
				info, ok := w.registry.LookupByID(id)
				if !ok {
					panic("flecs: migrate: component ID not registered")
				}
				types[i] = info
			}
			newTable = table.New(newSig, types)
			w.tables[key] = newTable
			for _, id := range newTable.Type() {
				w.compIndex.Register(id, newTable)
			}
			// Notify cached queries that a new table is available. Fires once
			// per newly-created table, after full registration. Hot path: do
			// NOT compact w.cachedQueries here.
			w.notifyTableCreated(newTable)
		}
		// Cache the edge for future single-component transitions from oldTable.
		if oldTable != nil {
			switch {
			case removeID != 0 && addID == 0:
				oldTable.CacheRemoveEdge(removeID, newTable)
			case addID != 0 && removeID == 0:
				oldTable.CacheAddEdge(addID, newTable)
			}
		}
	}

	// Append a new zero-initialized row for e in the destination table.
	newRow := newTable.Append(e)

	// Carry component data from the old table to the new one.
	// Skip the removed component (not present in new table) and the added
	// component (its value comes from copyValue, not from the source).
	if oldTable != nil {
		for _, id := range oldSig {
			if id == removeID || id == addID {
				continue
			}
			if !newTable.HasComponent(id) {
				continue
			}
			ptr := oldTable.Get(oldRow, id)
			if ptr != nil {
				newTable.Set(newRow, id, ptr)
			}
		}
		// Transfer CanToggle disabled bits for components shared by both tables.
		for _, id := range oldSig {
			if id == removeID {
				continue
			}
			if !newTable.HasComponent(id) {
				continue
			}
			if !oldTable.IsRowEnabled(id, oldRow) {
				newTable.DisableRow(id, newRow)
			}
		}
	}

	// Write the new component value, if any.
	if addID != 0 && copyValue != nil {
		newTable.Set(newRow, addID, copyValue)
	}

	// Update the migrating entity's record before firing observers so that
	// multi-term observer filter evaluation sees the fully-migrated entity state.
	// The old table slot remains valid until RemoveSwap below.
	rec.Table = newTable
	rec.Row = uint32(newRow)

	// Fire OnAdd for the newly-added component — destination slot is fully written.
	if addID != 0 {
		addInfo, _ := w.registry.LookupByID(addID)
		w.fireOnAdd(addInfo, addID, e, newTable.Get(newRow, addID))
	}

	// Fire OnRemove for the removed component — source slot still intact.
	if removeID != 0 && oldTable != nil {
		remInfo, _ := w.registry.LookupByID(removeID)
		w.fireOnRemove(remInfo, removeID, e, oldTable.Get(oldRow, removeID))
	}

	// Remove e from the old table using swap-remove.
	// If another entity was in the last row it has been moved to oldRow;
	// update its record so future operations find it at the correct position.
	if oldTable != nil {
		moved, ok := oldTable.RemoveSwap(oldRow)
		if ok {
			movedRec := w.index.Get(moved)
			movedRec.Row = uint32(oldRow)
		}
	}

	// Fire archetype monitors using the table-pair check now that the record
	// reflects the new state. Fire sparse monitors for each component that
	// changed — entity state is fully updated so entityMatchesMonitorExcluding
	// evaluates correctly without an excludeID hint.
	if len(w.monitors) > 0 {
		w.fireArchetypeMonitors(e, oldTable, newTable)
		if addID != 0 {
			w.fireSparseMonitors(e, addID, 0)
		}
		if removeID != 0 {
			w.fireSparseMonitors(e, removeID, 0)
		}
	}
}

// migrateArchetypeOnly moves entity e's archetype record by adding or removing
// one component from the type vector WITHOUT firing any OnAdd/OnRemove hooks.
// Used for Sparse-only components where hooks are fired externally (with the
// sparse-set pointer) before this function is called.
// Exactly one of addID/removeID must be non-zero.
func (w *World) migrateArchetypeOnly(e ID, addID, removeID ID) {
	rec := w.index.Get(e)
	oldTable := rec.Table
	oldRow := int(rec.Row)
	var oldSig []ID
	if oldTable != nil {
		oldSig = oldTable.Type()
	}
	newSig := make([]ID, 0, len(oldSig)+1)
	for _, id := range oldSig {
		if id == removeID {
			continue
		}
		newSig = append(newSig, id)
	}
	if addID != 0 {
		pos := sort.Search(len(newSig), func(i int) bool { return newSig[i] >= addID })
		newSig = append(newSig, 0)
		copy(newSig[pos+1:], newSig[pos:])
		newSig[pos] = addID
	}
	key := sigKey(newSig)
	newTable, exists := w.tables[key]
	if !exists {
		types := make([]*component.TypeInfo, len(newSig))
		for i, id := range newSig {
			info, ok := w.registry.LookupByID(id)
			if !ok {
				panic("flecs: migrateArchetypeOnly: component ID not registered")
			}
			types[i] = info
		}
		newTable = table.New(newSig, types)
		w.tables[key] = newTable
		for _, id := range newTable.Type() {
			w.compIndex.Register(id, newTable)
		}
		w.notifyTableCreated(newTable)
	}
	newRow := newTable.Append(e)
	if oldTable != nil {
		for _, id := range oldSig {
			if id == removeID || id == addID {
				continue
			}
			if !newTable.HasComponent(id) {
				continue
			}
			ptr := oldTable.Get(oldRow, id)
			if ptr != nil {
				newTable.Set(newRow, id, ptr)
			}
		}
		for _, id := range oldSig {
			if id == removeID || !newTable.HasComponent(id) {
				continue
			}
			if !oldTable.IsRowEnabled(id, oldRow) {
				newTable.DisableRow(id, newRow)
			}
		}
		moved, ok := oldTable.RemoveSwap(oldRow)
		if ok {
			movedRec := w.index.Get(moved)
			movedRec.Row = uint32(oldRow)
		}
	}
	rec.Table = newTable
	rec.Row = uint32(newRow)
}

// TablesFor returns a snapshot of all archetype tables that contain
// componentID, in registration order. Returns an empty (non-nil) slice when no
// tables are registered for componentID.
func (w *World) TablesFor(componentID ID) []*table.Table {
	w.checkExclusiveAccessRead()
	return w.compIndex.TablesFor(componentID)
}

// EachTableFor calls fn for every archetype table containing componentID, in
// registration order. fn returns false to stop iteration early. No allocation
// is performed; this is the hot path for Phase 3 query iteration.
func (w *World) EachTableFor(componentID ID, fn func(*table.Table) bool) {
	w.checkExclusiveAccessRead()
	w.compIndex.Each(componentID, fn)
}

// eachAlive calls fn for every currently alive entity, in dense order.
// Callbacks must not call Alloc or Free (i.e. NewEntity, Delete) during iteration.
func (w *World) eachAlive(fn func(ID)) {
	w.index.Each(func(id ID, _ *entityindex.Record) {
		fn(id)
	})
}

// notifyTableCreated walks w.cachedQueries and calls tryMatchTable on each
// active (non-removed) entry. Called once per newly-created table, after the
// table is fully registered in w.tables and w.compIndex.
//
// Compaction of removed entries is intentionally skipped here (hot path);
// it happens lazily in NewCachedQuery. Defensively handles nil cachedQueries.
func (w *World) notifyTableCreated(t *table.Table) {
	for _, cq := range w.cachedQueries {
		if !cq.removed {
			prevLen := len(cq.tables)
			cq.tryMatchTable(t)
			// Re-sort when Cascade is active and a new table was accepted.
			if cq.cascadeTermTrav != 0 && len(cq.tables) > prevLen {
				cq.sortByCascadeDepth()
			}
		}
	}
	// Dispatch EventOnTableCreate observers for non-empty tables.
	// The empty root table (len == 0) is excluded intentionally, matching
	// upstream's is_root suppression at table.c:1278.
	if len(t.Type()) > 0 {
		w.dispatchObservers(tableCreateSentinelID, w.eventOnTableCreateID, 0, unsafe.Pointer(t))
	}
	if w.logger != nil {
		sig := t.Type()
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "table created",
			slog.Int("signature_len", len(sig)),
			slog.String("signature", formatSig(sig)))
	}
}

// commitBatch migrates entity e to the table described by newSig, carrying
// component data from its current table. It fires OnAdd for each id in addedIDs
// and OnRemove for each id in removedIDs, then removes e from the old table.
//
// Unlike migrate (which handles a single add+remove), commitBatch handles an
// arbitrary number of added and removed IDs in one pass — the coalescer's key
// primitive. Values for Set cmds are NOT written here; they are written in the
// coalescer's pass 2 after this function returns.
func (w *World) commitBatch(e ID, newSig []ID, addedIDs, removedIDs []ID) {
	rec := w.index.Get(e)
	if rec == nil {
		return
	}
	oldTable := rec.Table
	oldRow := int(rec.Row)

	// Find or create the destination table.
	// sigKeyLookup is allocation-free for the common "table already exists" path.
	newTable, exists := w.tables[sigKeyLookup(newSig)]
	if !exists {
		// New archetype: copy newSig before storing — newSig may alias a scratch
		// buffer in the caller (batchForEntity reuses cmdQueue.scratch1).
		sigCopy := append([]ID(nil), newSig...)
		types := make([]*component.TypeInfo, len(sigCopy))
		for i, id := range sigCopy {
			info, ok := w.registry.LookupByID(id)
			if !ok {
				panic("flecs: commitBatch: component ID not registered")
			}
			types[i] = info
		}
		newTable = table.New(sigCopy, types)
		w.tables[sigKey(sigCopy)] = newTable
		for _, id := range newTable.Type() {
			w.compIndex.Register(id, newTable)
		}
		w.notifyTableCreated(newTable)
	}

	// Append a new zero-initialised row for e in the destination table.
	newRow := newTable.Append(e)

	// Carry existing component data from old table to new table.
	if oldTable != nil {
		for _, id := range oldTable.Type() {
			if !newTable.HasComponent(id) {
				continue
			}
			ptr := oldTable.Get(oldRow, id)
			if ptr != nil {
				newTable.Set(newRow, id, ptr)
			}
		}
	}

	// Point e's record at its new location before firing OnAdd so that observers
	// (including multi-term observers) see the fully-migrated entity state.
	// The old table slot remains valid until RemoveSwap below.
	rec.Table = newTable
	rec.Row = uint32(newRow)

	// Fire OnAdd for newly-added components (slot is zero-initialised here;
	// Set payloads are written by the coalescer's pass 2 after this returns).
	for _, id := range addedIDs {
		info, _ := w.registry.LookupByID(id)
		w.fireOnAdd(info, id, e, newTable.Get(newRow, id))
	}

	// Fire OnRemove for removed components while the old slot is still live.
	for _, id := range removedIDs {
		if oldTable == nil {
			continue
		}
		info, _ := w.registry.LookupByID(id)
		w.fireOnRemove(info, id, e, oldTable.Get(oldRow, id))
	}

	// Swap-remove e from the old table and fix up the moved entity's record.
	if oldTable != nil {
		moved, ok := oldTable.RemoveSwap(oldRow)
		if ok {
			movedRec := w.index.Get(moved)
			movedRec.Row = uint32(oldRow)
		}
	}

	// Fire monitors after the record is updated so entity state is consistent.
	if len(w.monitors) > 0 {
		w.fireArchetypeMonitors(e, oldTable, newTable)
		for _, id := range addedIDs {
			w.fireSparseMonitors(e, id, 0)
		}
		for _, id := range removedIDs {
			w.fireSparseMonitors(e, id, 0)
		}
	}
}

// sigKey encodes a sorted []ID as a string map key (allocating copy).
// Use for map store. Each ID is stored as 8 raw bytes (host byte-order).
func sigKey(sig []ID) string {
	if len(sig) == 0 {
		return ""
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(&sig[0])), len(sig)*8))
}

// sigKeyLookup returns a map-lookup string that points directly into sig's
// backing array — no allocation. The returned string must NOT be stored in a
// map or outlive sig. Safe to use only as a transient lookup key.
func sigKeyLookup(sig []ID) string {
	if len(sig) == 0 {
		return ""
	}
	return unsafe.String((*byte)(unsafe.Pointer(&sig[0])), len(sig)*8)
}

// Read opens a read-only capability scope. Concurrent calls from multiple
// goroutines are allowed; writes from other goroutines block until all active
// Read callbacks return. Internally backed by sync.RWMutex.RLock.
func (w *World) Read(fn func(*Reader)) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	fn(&w.readCapability)
}

// Write opens a read/write capability scope. Claims exclusive access for
// the duration of fn. All structural mutations are queued in a defer scope
// and flushed when fn returns.
//
// Nested Write calls from the same goroutine increment deferDepth and run fn;
// only the outermost Write flushes. Write from a different goroutine while
// exclusive access is held panics with ErrExclusiveAccessViolation.
func (w *World) Write(fn func(*Writer)) {
	s0 := w.stages[0]
	if s0.inMerge {
		panic(ErrMergeReentry)
	}
	id := currentGoid()
	owner := w.exclusiveAccess.Load()
	if owner == id {
		// Nested Write from same goroutine — increment depth and run without re-locking.
		s0.deferDepth++
		defer func() {
			s0.deferDepth--
			if s0.deferDepth == 0 {
				s0.inMerge = true
				w.firePreMergeHooks()
				q := s0.queue
				s0.queue = acquireCmdQueue()
				q.flush(w)
				releaseCmdQueue(q)
				w.firePostMergeHooks()
				s0.inMerge = false
			}
		}()
		fn(&w.writeCapability)
		return
	}
	if owner != 0 {
		panic(ErrExclusiveAccessViolation)
	}
	w.mu.Lock()
	w.ExclusiveAccessBegin("Write")
	w.writeCapability.scopeStack = w.writeCapability.scopeStack[:0]
	s0.deferDepth++
	defer func() {
		s0.deferDepth--
		if s0.deferDepth == 0 {
			s0.inMerge = true
			w.firePreMergeHooks()
			q := s0.queue
			s0.queue = acquireCmdQueue()
			q.flush(w)
			releaseCmdQueue(q)
			w.firePostMergeHooks()
			s0.inMerge = false
		}
		w.ExclusiveAccessEnd(false)
		w.mu.Unlock()
	}()
	fn(&w.writeCapability)
}

// deferScope runs fn in a deferred-mutation scope without claiming exclusive
// access. It is the internal equivalent of the former public Defer method,
// used by Progress/runPhase which already holds or does not require exclusive
// access. Worker goroutines that call mutators inside a system callback rely
// on deferScope not checking goroutine ownership.
func (w *World) deferScope(fn func()) {
	s0 := w.stages[0]
	s0.deferDepth++
	defer func() {
		if s0.deferDepth <= 0 {
			panic("flecs: deferScope: depth underflow")
		}
		s0.deferDepth--
		if s0.deferDepth == 0 {
			s0.inMerge = true
			w.firePreMergeHooks()
			q := s0.queue
			s0.queue = acquireCmdQueue()
			q.flush(w)
			releaseCmdQueue(q)
			w.firePostMergeHooks()
			s0.inMerge = false
		}
	}()
	fn()
}

// SetLogger installs l as the structured logger for lifecycle events.
// Passing nil disables logging. When set, the logger receives DEBUG-level
// records for the following lifecycle events: entity created, entity deleted,
// component registered, table created, system added, system closed, observer
// registered, observer unsubscribed, snapshot serialized, snapshot loaded.
//
// No logs fire on hot paths: Set/Get/Has/Each/Progress and similar read or
// per-frame operations are intentionally excluded for performance.
//
// Note: lifecycle events that occur inside World.New (empty table creation,
// built-in entity allocation) are not logged because SetLogger cannot be
// called until after New() returns.
func (w *World) SetLogger(l *slog.Logger) { w.logger = l }

// Logger returns the current logger, or nil if none is installed.
func (w *World) Logger() *slog.Logger { return w.logger }

// formatSig returns a space-separated string of decimal component IDs for use
// as the "signature" attribute on "table created" log records.
func formatSig(sig []ID) string {
	if len(sig) == 0 {
		return ""
	}
	var b strings.Builder
	for i, id := range sig {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(strconv.FormatUint(uint64(id), 10))
	}
	return b.String()
}
