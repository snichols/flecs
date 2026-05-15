package flecs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"unsafe"
)

// jsonWorld is the top-level JSON envelope for a serialized world.
type jsonWorld struct {
	Version  int          `json:"version"`
	Entities []jsonEntity `json:"entities"`
	// SparseComponents is the list of component names that have the Sparse trait.
	// Restored BEFORE entities so that the sparse routing is live during entity replay.
	// In v0.53.0+, Sparse-only component data is in the entity body (archetype-stored);
	// DontFragment component data is separately stored in SparseData.
	SparseComponents []string `json:"sparse_components,omitempty"`
	// DontFragmentComponents is the list of component names that have the DontFragment trait.
	// Restored BEFORE entities so that DontFragment routing is live during entity replay.
	DontFragmentComponents []string `json:"dont_fragment_components,omitempty"`
	// SparseData is component name → entity serial → JSON-encoded component value.
	// In v0.53.0+, contains data for DontFragment components only (data not in entity body).
	// Restored AFTER entities so that entity IDs exist when the sparse-set is populated.
	SparseData map[string]map[int]json.RawMessage `json:"sparse_data,omitempty"`
	// ParentStorageSerials is the list of entity serials that have the ParentStorage policy.
	// Restored in Phase 1b (after entity allocation, before body replay) so that pair
	// routing is live when parent-storage pairs are applied in Phase 2.
	ParentStorageSerials []int `json:"parent_storage_serials,omitempty"`
	// UnionRelationshipSerials is the list of entity serials that have the Union policy.
	// Restored in Phase 1b (after entity allocation, before body replay) so that the
	// union routing is live when union pairs are applied in Phase 3b.
	UnionRelationshipSerials []int `json:"union_relationship_serials,omitempty"`
	// UnionRelationships holds the active union-pair assignments.
	// Format: relSerial → {entitySerial → targetSerial}.
	// Restored AFTER entities so that all entity IDs exist before union pairs are applied.
	UnionRelationships map[int]map[int]int `json:"union_relationships,omitempty"`
	// EntityRangeMin/Max/Set serialise the active ID-range constraint so that
	// RangeSet state survives marshal/unmarshal round-trips.
	EntityRangeMin uint32 `json:"entity_range_min,omitempty"`
	EntityRangeMax uint32 `json:"entity_range_max,omitempty"`
	EntityRangeSet bool   `json:"entity_range_set,omitempty"`
	// Alerts holds registered alert definitions. Instances are not stored; they
	// are recomputed from the query via WithYieldExisting during UnmarshalJSON.
	Alerts []jsonAlertDef `json:"alerts,omitempty"`
	// Units holds user-registered unit definitions. Built-in units (indices 48–62)
	// are not stored; they are re-created by New() at fixed indices.
	Units []jsonUnitDef `json:"units,omitempty"`
	// CompUnits maps component names to their unit (built-in or user).
	CompUnits []jsonCompUnit `json:"comp_units,omitempty"`
}

// jsonUnitFactor represents one factor in a compound unit's numerator or denominator list.
// Exactly one of Serial or BuiltinIdx is non-zero for each factor.
type jsonUnitFactor struct {
	Serial     int    `json:"serial,omitempty"`      // user unit entity serial
	BuiltinIdx uint32 `json:"builtin_idx,omitempty"` // built-in unit raw index (48–72)
}

// jsonUnitDef is the serialised form of one user-registered unit entity.
type jsonUnitDef struct {
	EntitySerial   int              `json:"entity_serial"`
	Symbol         string           `json:"symbol"`
	Name           string           `json:"name"`
	BaseSerial     int              `json:"base_serial,omitempty"`      // user-unit base entity serial
	BaseBuiltinIdx uint32           `json:"base_builtin_idx,omitempty"` // built-in unit base raw index (48–72)
	Factor         float64          `json:"factor"`
	OverSerial     int              `json:"over_serial,omitempty"`      // denominator unit serial (UnitQuotient)
	OverBuiltinIdx uint32           `json:"over_builtin_idx,omitempty"` // denominator built-in index
	Power          int8             `json:"power,omitempty"`            // exponent (UnitPower)
	NumFactors     []jsonUnitFactor `json:"num_factors,omitempty"`      // compound numerator factors
	DenomFactors   []jsonUnitFactor `json:"denom_factors,omitempty"`    // compound denominator factors
}

// jsonCompUnit maps one component to its unit.
type jsonCompUnit struct {
	CompName       string `json:"comp_name"`
	UnitSerial     int    `json:"unit_serial,omitempty"`      // user unit entity serial
	UnitBuiltinIdx uint32 `json:"unit_builtin_idx,omitempty"` // built-in unit raw index (48–62)
}

// jsonAlertDef is the serialised form of one registered alert.
type jsonAlertDef struct {
	EntitySerial int             `json:"entity_serial"`
	Severity     int             `json:"severity"`
	Message      string          `json:"message,omitempty"`
	Terms        []jsonAlertTerm `json:"terms"`
}

// jsonAlertTerm is the serialised form of one alert query term.
// Only terms whose ID is a registered component (non-pair) are supported in v1.
type jsonAlertTerm struct {
	Kind          int    `json:"kind"`
	ComponentName string `json:"component_name"`
}

// jsonEntity is the JSON representation of a single entity.
type jsonEntity struct {
	Serial          int                        `json:"serial"`
	Name            string                     `json:"name,omitempty"`
	Parent          int                        `json:"parent,omitempty"`
	Prefabs         []int                      `json:"prefabs,omitempty"`
	Pairs           []jsonPair                 `json:"pairs,omitempty"`
	Components      map[string]json.RawMessage `json:"components,omitempty"`
	OrderedChildren bool                       `json:"ordered_children,omitempty"`
}

// jsonPair is the JSON representation of a single custom pair on an entity.
// ChildOf and IsA pairs are serialized via the parent/prefabs fields instead.
// Tag-only pairs omit DataType and Data; data-bearing pairs include both.
type jsonPair struct {
	Rel      int             `json:"rel"`
	Tgt      int             `json:"tgt"`
	DataType string          `json:"dataType,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}

// marshaler holds state for a single MarshalJSON run: the combined ChildOf+IsA
// predecessor graph plus DFS bookkeeping.
type marshaler struct {
	predecessorsOf map[ID][]ID
	visited        map[ID]struct{}
	visiting       map[ID]struct{}
	order          []ID
	idToSerial     map[ID]int
	indexToSerial  map[uint32]int // entity index → serial; used for pair rel/tgt lookup
}

func (m *marshaler) visit(e ID) error {
	if _, done := m.visited[e]; done {
		return nil
	}
	if _, active := m.visiting[e]; active {
		if serial, ok := m.idToSerial[e]; ok {
			return fmt.Errorf("flecs: marshal failed: cycle detected in ChildOf+IsA graph involving entity serial %d", serial)
		}
		return fmt.Errorf("flecs: marshal failed: cycle detected in ChildOf+IsA graph involving entity at allocation index %d", e.Index())
	}
	m.visiting[e] = struct{}{}
	for _, pred := range m.predecessorsOf[e] {
		if err := m.visit(pred); err != nil {
			return err
		}
	}
	delete(m.visiting, e)
	m.visited[e] = struct{}{}
	m.order = append(m.order, e)
	return nil
}

// MarshalJSON serializes the world to JSON.
//
// Format (v1): version=1, entities array with serial numbers (starting at 1),
// optional name field, optional parent field (serial of the ChildOf parent),
// optional prefabs field (serials of IsA targets in EachPrefab order),
// optional pairs array (custom pair components not handled by parent/prefabs),
// and components map keyed by ComponentInfo.Name.
//
// The pairs array contains entries of the form
// {"rel":<serial>,"tgt":<serial>} for tag-only pairs and
// {"rel":<serial>,"tgt":<serial>,"dataType":"pkg.T","data":{...}} for
// data-bearing pairs. DataType is info.Type.String() of the base Go type
// (not the "pair(T)" wrapper name). ChildOf and IsA pairs are NOT duplicated
// in the pairs array; they are handled exclusively via parent and prefabs.
//
// Built-in entities (ChildOf, IsA, Name component entity, PreUpdate, OnUpdate,
// PostUpdate, OnFixedUpdate) are skipped. The Name component value is surfaced
// as the "name" field, not as a components map entry.
//
// Entities are emitted in topological order over the combined ChildOf+IsA
// predecessor graph so that UnmarshalJSON can restore all relationships with a
// single sequential pass. The DFS orders predecessors as: ChildOf parent first,
// then IsA prefabs in EachPrefab order. Siblings retain entity allocation order.
//
// The "parent" field holds the serial of the ChildOf parent (omitted when
// absent). Only the first ChildOf parent is serialized for entities that have
// multiple (a rare configuration). The "prefabs" field holds the serials of all
// IsA targets in EachPrefab order (omitted when empty).
//
// Returns an error if a cycle is detected in the combined ChildOf+IsA graph.
func (w *World) MarshalJSON() ([]byte, error) {
	skip := map[ID]struct{}{
		w.ChildOf():             {},
		w.IsA():                 {},
		w.Name():                {},
		w.preUpdateID:           {}, // built-in phase entities (no longer in public API)
		w.onUpdateID:            {},
		w.postUpdateID:          {},
		w.onFixedUpdateID:       {},
		w.OnInstantiate():       {},
		w.Inherit():             {},
		w.Override():            {},
		w.DontInherit():         {},
		w.OnDelete():            {},
		w.OnDeleteTarget():      {},
		w.RemoveAction():        {},
		w.DeleteAction():        {},
		w.PanicAction():         {},
		w.Exclusive():           {},
		w.CanToggle():           {},
		w.Symmetric():           {},
		w.Transitive():          {},
		w.Reflexive():           {},
		w.Acyclic():             {},
		w.Final():               {},
		w.OneOf():               {},
		w.Singleton():           {},
		w.WriteOnce():           {},
		w.Traversable():         {},
		w.Relationship():        {},
		w.Target():              {},
		w.Trait():               {},
		w.PairIsTag():           {},
		w.With():                {},
		w.OrderedChildren():     {},
		w.Sparse():              {},
		w.DontFragment():        {},
		w.Disabled():            {},
		w.Prefab():              {},
		w.Wildcard():            {},
		w.Any():                 {},
		w.EventOnAdd():          {},
		w.EventOnSet():          {},
		w.EventOnRemove():       {},
		w.EventOnTableCreate():  {},
		w.EventOnTableEmpty():   {},
		w.EventOnTableFill():    {},
		w.EventOnTableDelete():  {},
		w.EventOnDelete():       {},
		w.EventOnDeleteTarget(): {},
		w.Event():               {},
		w.DependsOn():           {},
		w.EventMonitor():        {},
		w.SlotOf():              {},
		// Built-in unit entities (indices 50–74).
		w.Meter():                 {},
		w.KiloMeter():             {},
		w.MilliMeter():            {},
		w.Second():                {},
		w.MilliSecond():           {},
		w.Minute():                {},
		w.Hour():                  {},
		w.Gram():                  {},
		w.KiloGram():              {},
		w.MegaGram():              {},
		w.Newton():                {},
		w.Joule():                 {},
		w.Hertz():                 {},
		w.Radian():                {},
		w.Degree():                {},
		w.MeterPerSecond():        {},
		w.KiloMeterPerHour():      {},
		w.MeterPerSecondSquared(): {},
		w.NewtonCompound():        {},
		w.JouleCompound():         {},
		w.Watt():                  {},
		w.Pascal():                {},
		w.HertzCompound():         {},
		w.RadianPerSecond():       {},
		w.Inverse():               {},
	}

	var result []byte
	var resultErr error

	w.Read(func(fr *Reader) {
		for _, cid := range fr.Components() {
			skip[cid] = struct{}{}
		}

		var userEnts []ID
		fr.EachEntity(func(e ID) bool {
			if _, isBuiltin := skip[e]; !isBuiltin {
				userEnts = append(userEnts, e)
			}
			return true
		})

		// Build predecessorsOf: ChildOf parent first, then IsA prefabs in EachPrefab
		// order. Built-in entities are filtered at insertion time.
		m := &marshaler{
			predecessorsOf: make(map[ID][]ID, len(userEnts)),
			visited:        make(map[ID]struct{}, len(userEnts)),
			visiting:       make(map[ID]struct{}),
			order:          make([]ID, 0, len(userEnts)),
			idToSerial:     make(map[ID]int, len(userEnts)),
			indexToSerial:  make(map[uint32]int, len(userEnts)),
		}
		for _, e := range userEnts {
			var preds []ID
			if p, ok := fr.ParentOf(e); ok {
				if _, isBuiltin := skip[p]; !isBuiltin {
					preds = append(preds, p)
				}
			}
			fr.EachPrefab(e, func(prefab ID) bool {
				if _, isBuiltin := skip[prefab]; !isBuiltin {
					preds = append(preds, prefab)
				}
				return true
			})
			if len(preds) > 0 {
				m.predecessorsOf[e] = preds
			}
		}

		for _, e := range userEnts {
			if err := m.visit(e); err != nil {
				resultErr = err
				return
			}
		}

		// Assign serials in topo order; build reverse maps for Prefabs and pair lookups.
		for i, e := range m.order {
			m.idToSerial[e] = i + 1
			m.indexToSerial[e.Index()] = i + 1
		}

		nameID := w.Name()
		childOfIdx := w.ChildOf().Index()
		isAIdx := w.IsA().Index()
		var entities []jsonEntity

		for _, e := range m.order {
			je := jsonEntity{Serial: m.idToSerial[e]}

			if name, ok := fr.GetName(e); ok {
				je.Name = name
			}

			if w.orderedChildren != nil {
				if _, ok := w.orderedChildren[ID(e.Index())]; ok {
					je.OrderedChildren = true
				}
			}

			if p, ok := fr.ParentOf(e); ok {
				if _, isBuiltin := skip[p]; !isBuiltin {
					if serial, ok := m.idToSerial[p]; ok {
						je.Parent = serial
					}
				}
			}

			// Populate Prefabs after serials are assigned so idToSerial lookups work.
			fr.EachPrefab(e, func(prefab ID) bool {
				if _, isBuiltin := skip[prefab]; isBuiltin {
					return true
				}
				if serial, ok := m.idToSerial[prefab]; ok {
					je.Prefabs = append(je.Prefabs, serial)
				}
				return true
			})

			for _, cid := range fr.EntityComponents(e) {
				if cid.IsPair() {
					relIdx := uint32(cid.First())
					tgtIdx := uint32(cid.Second())
					if relIdx == childOfIdx || relIdx == isAIdx {
						continue
					}
					// Parent-storage: signature carries (rel, Any) marker; resolve
					// the actual parent from the per-row parent column.
					relKey := ID(relIdx)
					if w.parentStoragePolicies[relKey] && uint32(cid.Second()) == w.anyID.Index() {
						rec := w.index.Get(e)
						if rec == nil || rec.Table == nil {
							continue
						}
						parent, ok := rec.Table.GetParentEntry(int(rec.Row), relKey)
						if !ok {
							continue
						}
						tgtIdx = parent.Index()
					}
					relSerial, ok := m.indexToSerial[relIdx]
					if !ok {
						continue
					}
					tgtSerial, ok := m.indexToSerial[tgtIdx]
					if !ok {
						continue
					}
					jp := jsonPair{Rel: relSerial, Tgt: tgtSerial}
					info, hasInfo := fr.ComponentInfo(cid)
					if hasInfo && info.Size > 0 && info.Type != nil {
						v, vok := fr.GetByID(e, cid)
						if vok {
							raw, err := json.Marshal(v)
							if err != nil {
								resultErr = err
								return
							}
							jp.DataType = info.Type.String()
							jp.Data = raw
						}
					}
					je.Pairs = append(je.Pairs, jp)
					continue
				}
				if cid == nameID {
					continue
				}
				info, ok := fr.ComponentInfo(cid)
				if !ok {
					continue
				}
				// Skip raw entity IDs used as tags (EnsureID sentinel TypeInfos have
				// Name=="tag"). These include built-in entities like Prefab and Disabled
				// applied to user entities. They cannot be round-tripped by name; user
				// zero-size components registered via RegisterComponent[T] have proper
				// type names (e.g. "flecs_test.myTag"), never just "tag".
				if info.Name == "tag" {
					continue
				}
				if info.Type == nil && info.Size > 0 {
					// Dynamic component: serialize as base64-encoded raw bytes (default)
					// or via a registered custom marshaler.
					ptr := getIDPtrRaw(w, e, cid)
					if ptr == nil {
						continue
					}
					var raw json.RawMessage
					if hooks, hasHooks := w.dynamicMarshalers[cid]; hasHooks {
						raw, resultErr = hooks.marshal(ptr)
						if resultErr != nil {
							return
						}
					} else {
						b64 := base64.StdEncoding.EncodeToString(unsafe.Slice((*byte)(ptr), info.Size))
						raw, resultErr = json.Marshal(b64)
						if resultErr != nil {
							return
						}
					}
					if je.Components == nil {
						je.Components = make(map[string]json.RawMessage)
					}
					je.Components[info.Name] = raw
					continue
				}
				v, ok := fr.GetByID(e, cid)
				if !ok {
					continue
				}
				raw, err := json.Marshal(v)
				if err != nil {
					resultErr = err
					return
				}
				if je.Components == nil {
					je.Components = make(map[string]json.RawMessage)
				}
				je.Components[info.Name] = raw
			}

			entities = append(entities, je)
		}

		// Serialize Sparse trait component names (policy must be restored before entity replay).
		var sparseComponents []string
		if w.sparsePolicies != nil {
			for key := range w.sparsePolicies {
				if ss, ok := w.sparseStorage[key]; ok {
					if ss.typeInfo != nil && ss.typeInfo.Name != "" {
						sparseComponents = append(sparseComponents, ss.typeInfo.Name)
					}
				}
			}
		}

		// Serialize DontFragment trait component names AND their data (data not in entity body).
		// For Sparse+DontFragment components, the name appears in BOTH lists.
		var dontFragmentComponents []string
		var sparseData map[string]map[int]json.RawMessage
		if w.dontFragmentPolicies != nil {
			for key := range w.dontFragmentPolicies {
				if ss, ok := w.sparseStorage[key]; ok {
					info := ss.typeInfo
					if info != nil && info.Name != "" {
						dontFragmentComponents = append(dontFragmentComponents, info.Name)
						// Serialize each entity's data (DontFragment data is NOT in entity body).
						for _, entry := range ss.dense {
							if serial, ok := m.idToSerial[entry.entity]; ok {
								var raw json.RawMessage
								var err error
								if info.Type == nil {
									// Dynamic component: base64 or custom marshaler.
									if hooks, hasHooks := w.dynamicMarshalers[info.Component]; hasHooks {
										raw, err = hooks.marshal(entry.data)
									} else {
										b64 := base64.StdEncoding.EncodeToString(unsafe.Slice((*byte)(entry.data), info.Size))
										raw, err = json.Marshal(b64)
									}
								} else {
									raw, err = json.Marshal(reflect.NewAt(info.Type, entry.data).Elem().Interface())
								}
								if err != nil {
									resultErr = err
									return
								}
								if sparseData == nil {
									sparseData = make(map[string]map[int]json.RawMessage)
								}
								if sparseData[info.Name] == nil {
									sparseData[info.Name] = make(map[int]json.RawMessage)
								}
								sparseData[info.Name][serial] = raw
							}
						}
					}
				}
			}
		}

		// Serialize which entity serials have the ParentStorage policy.
		var parentStorageSerials []int
		if w.parentStoragePolicies != nil {
			for relKey := range w.parentStoragePolicies {
				if relSerial, ok := m.indexToSerial[uint32(relKey)]; ok {
					parentStorageSerials = append(parentStorageSerials, relSerial)
				}
			}
		}

		// Serialize which entity serials have the Union policy (so UnmarshalJSON can
		// restore the policy before replaying union pairs in Phase 3b).
		var unionRelSerials []int
		if w.unionPolicies != nil {
			for relKey := range w.unionPolicies {
				if relSerial, ok := m.indexToSerial[uint32(relKey)]; ok {
					unionRelSerials = append(unionRelSerials, relSerial)
				}
			}
		}

		// Serialize union relationship policies and their active targets.
		// relSerial → {entitySerial → targetSerial}.
		var unionRelationships map[int]map[int]int
		if w.unionStore != nil {
			for relKey, store := range w.unionStore {
				if len(store.dense) == 0 {
					continue
				}
				relSerial, ok := m.indexToSerial[uint32(relKey)]
				if !ok {
					continue
				}
				for _, entry := range store.dense {
					entitySerial, eOK := m.idToSerial[entry.entity]
					targetSerial, tOK := m.indexToSerial[entry.target.Index()]
					if !eOK || !tOK {
						continue
					}
					if unionRelationships == nil {
						unionRelationships = make(map[int]map[int]int)
					}
					if unionRelationships[relSerial] == nil {
						unionRelationships[relSerial] = make(map[int]int)
					}
					unionRelationships[relSerial][entitySerial] = targetSerial
				}
			}
		}

		// Serialize alert definitions. Only terms with registered component IDs
		// (non-pair) are supported; alerts with unsupported terms are silently skipped.
		var alertDefs []jsonAlertDef
		for _, def := range w.alertDefs {
			serial, ok := m.idToSerial[def.alertID]
			if !ok {
				continue
			}
			terms := make([]jsonAlertTerm, 0, len(def.desc.Query))
			valid := true
			for _, term := range def.desc.Query {
				if term.ID.IsPair() {
					valid = false
					break
				}
				info, hasInfo := fr.ComponentInfo(term.ID)
				if !hasInfo {
					valid = false
					break
				}
				terms = append(terms, jsonAlertTerm{Kind: int(term.Kind), ComponentName: info.Name})
			}
			if !valid {
				continue
			}
			alertDefs = append(alertDefs, jsonAlertDef{
				EntitySerial: serial,
				Severity:     def.desc.Severity,
				Message:      def.desc.Message,
				Terms:        terms,
			})
		}

		// Serialize user-registered unit definitions. Built-in units (indices 48–72)
		// are excluded; they are re-created by New() at fixed indices on unmarshal.
		var unitDefs []jsonUnitDef
		for unitID, u := range w.unitDefs {
			if isBuiltinUnit(unitID) {
				continue
			}
			serial, ok := m.idToSerial[unitID]
			if !ok {
				continue
			}
			def := jsonUnitDef{
				EntitySerial: serial,
				Symbol:       u.Symbol,
				Name:         u.Name,
				Factor:       u.Factor,
				Power:        u.Power,
			}
			if u.Base != 0 {
				if isBuiltinUnit(u.Base) {
					def.BaseBuiltinIdx = u.Base.Index()
				} else if baseSerial, ok := m.idToSerial[u.Base]; ok {
					def.BaseSerial = baseSerial
				}
			}
			if u.Over != 0 {
				if isBuiltinUnit(u.Over) {
					def.OverBuiltinIdx = u.Over.Index()
				} else if overSerial, ok := m.idToSerial[u.Over]; ok {
					def.OverSerial = overSerial
				}
			}
			// Serialize compound factor lists if present.
			if cd, isCompound := w.compoundDefs[unitID]; isCompound {
				def.NumFactors = serializeUnitFactors(cd.numerators, m.idToSerial, w)
				def.DenomFactors = serializeUnitFactors(cd.denominators, m.idToSerial, w)
			}
			unitDefs = append(unitDefs, def)
		}

		// Serialize component→unit mappings.
		var compUnits []jsonCompUnit
		for compID, unitID := range w.componentUnits {
			info, ok := fr.ComponentInfo(compID)
			if !ok {
				continue
			}
			cu := jsonCompUnit{CompName: info.Name}
			if isBuiltinUnit(unitID) {
				cu.UnitBuiltinIdx = unitID.Index()
			} else if unitSerial, ok := m.idToSerial[unitID]; ok {
				cu.UnitSerial = unitSerial
			} else {
				continue
			}
			compUnits = append(compUnits, cu)
		}

		rangeMin, rangeMax, rangeIsSet := w.index.GetRange()
		jw := jsonWorld{
			Version:                  1,
			Entities:                 entities,
			SparseComponents:         sparseComponents,
			DontFragmentComponents:   dontFragmentComponents,
			SparseData:               sparseData,
			ParentStorageSerials:     parentStorageSerials,
			UnionRelationshipSerials: unionRelSerials,
			UnionRelationships:       unionRelationships,
			EntityRangeMin:           rangeMin,
			EntityRangeMax:           rangeMax,
			EntityRangeSet:           rangeIsSet,
			Alerts:                   alertDefs,
			Units:                    unitDefs,
			CompUnits:                compUnits,
		}
		if jw.Entities == nil {
			jw.Entities = []jsonEntity{}
		}
		result, resultErr = json.Marshal(jw)
		if resultErr == nil && w.logger != nil {
			w.logger.LogAttrs(context.Background(), slog.LevelDebug, "snapshot serialized",
				slog.Int("entities", len(entities)))
		}
	})

	return result, resultErr
}

// UnmarshalJSON restores entities, names, ChildOf parent-child relationships,
// IsA prefab relationships, custom pair components, and regular components from
// JSON produced by MarshalJSON. The world need not be empty; new entities are
// added to existing ones.
//
// All component types present in the JSON must be pre-registered via
// RegisterComponent[T] before calling UnmarshalJSON. This applies to both
// regular components and the base data types of custom pairs. Tag-only pairs
// (no dataType field) are restored via AddID without pre-registration.
//
// Restoration order per entity: name → ChildOf parent → IsA prefabs (in
// "prefabs" array order) → pairs (in "pairs" array order) → components. The
// prefabs array preserves the first-prefab-wins inheritance semantics of the
// original world.
//
// Error cases:
//   - JSON parse error → wrapped error.
//   - Unsupported version → descriptive error.
//   - Parent serial not found in document → descriptive error.
//   - Unknown prefab serial → "unknown prefab serial N".
//   - Unknown pair rel/tgt serial → "pair rel serial N not found".
//   - Unregistered pair data type → descriptive error.
//   - Unregistered component → descriptive error.
//   - Type mismatch → wrapped json error.
func (w *World) UnmarshalJSON(data []byte) error {
	var jw jsonWorld
	if err := json.Unmarshal(data, &jw); err != nil {
		return fmt.Errorf("flecs: unmarshal failed: %w", err)
	}
	if jw.Version != 1 {
		return fmt.Errorf("flecs: unmarshal failed: unsupported version %d (only v1 supported)", jw.Version)
	}

	var unmarshalErr error

	w.Write(func(fw *Writer) {
		compByName := make(map[string]ComponentInfo)
		typeStringToType := make(map[string]reflect.Type)
		for _, cid := range fw.Components() {
			info, ok := fw.ComponentInfo(cid)
			if !ok {
				continue
			}
			compByName[info.Name] = info
			if info.Type != nil {
				ts := info.Type.String()
				if _, exists := typeStringToType[ts]; !exists {
					typeStringToType[ts] = info.Type
				}
			}
		}

		// Phase 0: restore Sparse and DontFragment policies BEFORE entity allocation so
		// that the storage routing is live during entity replay.
		// Components must be pre-registered before UnmarshalJSON.
		for _, compName := range jw.SparseComponents {
			info, ok := compByName[compName]
			if !ok {
				unmarshalErr = fmt.Errorf("flecs: unmarshal failed: sparse component %q is not registered in the world", compName)
				return
			}
			applySparsePolicy(w, info.ID)
		}
		for _, compName := range jw.DontFragmentComponents {
			info, ok := compByName[compName]
			if !ok {
				unmarshalErr = fmt.Errorf("flecs: unmarshal failed: dont_fragment component %q is not registered in the world", compName)
				return
			}
			applyDontFragmentPolicy(w, info.ID)
		}

		// Phase 1: allocate all entities so future phases can resolve serial→ID.
		serialToID := make(map[int]ID, len(jw.Entities))
		for _, je := range jw.Entities {
			serialToID[je.Serial] = fw.NewEntity()
		}

		// Phase 1b: restore ParentStorage and Union policies now that entity IDs are known.
		// Must happen before Phase 2 (pair replay) so that routing is live.
		for _, relSerial := range jw.ParentStorageSerials {
			relID, ok := serialToID[relSerial]
			if !ok {
				unmarshalErr = fmt.Errorf("flecs: unmarshal failed: parent_storage_serials: serial %d not found", relSerial)
				return
			}
			if w.parentStoragePolicies == nil {
				w.parentStoragePolicies = make(map[ID]bool)
			}
			w.parentStoragePolicies[ID(relID.Index())] = true
		}
		for _, relSerial := range jw.UnionRelationshipSerials {
			relID, ok := serialToID[relSerial]
			if !ok {
				unmarshalErr = fmt.Errorf("flecs: unmarshal failed: union_relationship_serials: serial %d not found", relSerial)
				return
			}
			applyUnionPolicy(w, relID)
		}

		// Phase 2: name → ChildOf → IsA prefabs → components (JSON is in topo order).
		for _, je := range jw.Entities {
			e := serialToID[je.Serial]
			if je.Name != "" {
				fw.SetName(e, je.Name)
			}
			// OrderedChildren trait replay: initialize the ordered list before children
			// are added so the child-add hook (addIDImmediate) can append them in order.
			// JSON is in topo order so parents are processed before their children.
			if je.OrderedChildren {
				applyOrderedChildrenPolicy(w, e)
			}
			if je.Parent > 0 {
				parentID, ok := serialToID[je.Parent]
				if !ok {
					unmarshalErr = fmt.Errorf("flecs: unmarshal failed: entity serial %d references unknown parent serial %d", je.Serial, je.Parent)
					return
				}
				addIDOnWorld(w, e, MakePair(w.ChildOf(), parentID))
			}
			for _, prefabSerial := range je.Prefabs {
				prefabID, ok := serialToID[prefabSerial]
				if !ok {
					unmarshalErr = fmt.Errorf("flecs: unmarshal failed: unknown prefab serial %d", prefabSerial)
					return
				}
				addIDOnWorld(w, e, MakePair(w.IsA(), prefabID))
			}
			for _, pair := range je.Pairs {
				relID, ok := serialToID[pair.Rel]
				if !ok {
					unmarshalErr = fmt.Errorf("flecs: unmarshal failed: pair rel serial %d not found", pair.Rel)
					return
				}
				tgtID, ok := serialToID[pair.Tgt]
				if !ok {
					unmarshalErr = fmt.Errorf("flecs: unmarshal failed: pair tgt serial %d not found", pair.Tgt)
					return
				}
				if pair.DataType == "" {
					addIDOnWorld(w, e, MakePair(relID, tgtID))
				} else {
					foundType, ok := typeStringToType[pair.DataType]
					if !ok {
						unmarshalErr = fmt.Errorf("flecs: unmarshal failed: pair data type %q is not registered in the world", pair.DataType)
						return
					}
					vPtr := reflect.New(foundType)
					if err := json.Unmarshal(pair.Data, vPtr.Interface()); err != nil {
						unmarshalErr = fmt.Errorf("flecs: unmarshal failed: pair data type %q: %w", pair.DataType, err)
						return
					}
					fw.SetPairByID(e, relID, tgtID, vPtr.Elem().Interface())
				}
			}
			for compName, raw := range je.Components {
				info, ok := compByName[compName]
				if !ok {
					unmarshalErr = fmt.Errorf("flecs: unmarshal failed: component %q is not registered in the world", compName)
					return
				}
				if info.Type == nil && info.Size > 0 {
					// Dynamic component: decode base64 bytes or use custom unmarshaler.
					if unmarshalErr = unmarshalDynamic(w, fw, e, info.ID, info.Size, raw); unmarshalErr != nil {
						return
					}
					continue
				}
				if info.Type == nil {
					continue
				}
				if info.Size == 0 {
					fw.SetByID(e, info.ID, reflect.Zero(info.Type).Interface())
					continue
				}
				ptr := reflect.New(info.Type)
				if err := json.Unmarshal(raw, ptr.Interface()); err != nil {
					unmarshalErr = fmt.Errorf("flecs: unmarshal failed: component %q: %w", compName, err)
					return
				}
				fw.SetByID(e, info.ID, ptr.Elem().Interface())
			}
		}

		// Phase 3: restore sparse data AFTER entities are created so that entity
		// IDs exist when the sparse-set is populated. Ordering: policies first
		// (phase 0), entities second (phases 1-2), sparse values here (phase 3).

		// Phase 3b: restore union relationships. Each entry is relSerial →
		// {entitySerial → targetSerial}. The relationship must already have the
		// Union trait (set during entity replay when the relationship entity's
		// metadata pairs were restored).
		for relSerial, byEntity := range jw.UnionRelationships {
			relID, ok := serialToID[relSerial]
			if !ok {
				unmarshalErr = fmt.Errorf("flecs: unmarshal failed: union_relationships: rel serial %d not found", relSerial)
				return
			}
			if !w.unionPolicies[ID(relID.Index())] {
				unmarshalErr = fmt.Errorf("flecs: unmarshal failed: union_relationships: rel serial %d is not a Union relationship in the restored world", relSerial)
				return
			}
			for entitySerial, targetSerial := range byEntity {
				e, ok := serialToID[entitySerial]
				if !ok {
					unmarshalErr = fmt.Errorf("flecs: unmarshal failed: union_relationships: entity serial %d not found", entitySerial)
					return
				}
				tgt, ok := serialToID[targetSerial]
				if !ok {
					unmarshalErr = fmt.Errorf("flecs: unmarshal failed: union_relationships: target serial %d not found", targetSerial)
					return
				}
				addIDImmediate(w, e, MakePair(relID, tgt))
			}
		}

		for compName, bySerial := range jw.SparseData {
			info, ok := compByName[compName]
			if !ok {
				unmarshalErr = fmt.Errorf("flecs: unmarshal failed: sparse data component %q is not registered in the world", compName)
				return
			}
			if info.Size == 0 {
				continue
			}
			for serial, raw := range bySerial {
				e, ok := serialToID[serial]
				if !ok {
					unmarshalErr = fmt.Errorf("flecs: unmarshal failed: sparse data for %q references unknown serial %d", compName, serial)
					return
				}
				if info.Type == nil {
					// Dynamic component.
					if unmarshalErr = unmarshalDynamic(w, fw, e, info.ID, info.Size, raw); unmarshalErr != nil {
						return
					}
					continue
				}
				ptr := reflect.New(info.Type)
				if err := json.Unmarshal(raw, ptr.Interface()); err != nil {
					unmarshalErr = fmt.Errorf("flecs: unmarshal failed: sparse data for %q: %w", compName, err)
					return
				}
				fw.SetByID(e, info.ID, ptr.Elem().Interface())
			}
		}

		// Restore range constraint after all entities are allocated so entity
		// allocation during the phases above is unconstrained.
		if jw.EntityRangeSet {
			w.index.SetRange(jw.EntityRangeMin, jw.EntityRangeMax)
		}

		// Phase 4: restore alert definitions. Instances are recomputed via
		// WithYieldExisting inside registerAlertInternal.
		for _, ja := range jw.Alerts {
			alertID, ok := serialToID[ja.EntitySerial]
			if !ok {
				unmarshalErr = fmt.Errorf("flecs: unmarshal failed: alert entity serial %d not found", ja.EntitySerial)
				return
			}
			terms := make([]Term, 0, len(ja.Terms))
			for _, jt := range ja.Terms {
				info, ok := compByName[jt.ComponentName]
				if !ok {
					unmarshalErr = fmt.Errorf("flecs: unmarshal failed: alert term component %q is not registered", jt.ComponentName)
					return
				}
				terms = append(terms, Term{ID: info.ID, Kind: TermKind(jt.Kind)})
			}
			desc := AlertDesc{
				Query:    terms,
				Severity: ja.Severity,
				Message:  ja.Message,
			}
			if name, ok := fw.GetName(alertID); ok {
				desc.Name = name
			}
			registerAlertInternal(fw, alertID, desc)
		}

		// Phase 5: restore user-registered unit definitions. Built-in units are
		// already live (created by New()); only user units need restoration here.
		for _, ju := range jw.Units {
			unitID, ok := serialToID[ju.EntitySerial]
			if !ok {
				unmarshalErr = fmt.Errorf("flecs: unmarshal failed: unit entity serial %d not found", ju.EntitySerial)
				return
			}
			var base ID
			switch {
			case ju.BaseSerial != 0:
				base, ok = serialToID[ju.BaseSerial]
				if !ok {
					unmarshalErr = fmt.Errorf("flecs: unmarshal failed: unit base serial %d not found", ju.BaseSerial)
					return
				}
			case ju.BaseBuiltinIdx != 0:
				base = w.builtinUnitByIndex(ju.BaseBuiltinIdx)
				if base == 0 {
					unmarshalErr = fmt.Errorf("flecs: unmarshal failed: unit base builtin index %d is not a built-in unit", ju.BaseBuiltinIdx)
					return
				}
			}
			var over ID
			switch {
			case ju.OverSerial != 0:
				over, ok = serialToID[ju.OverSerial]
				if !ok {
					unmarshalErr = fmt.Errorf("flecs: unmarshal failed: unit over serial %d not found", ju.OverSerial)
					return
				}
			case ju.OverBuiltinIdx != 0:
				over = w.builtinUnitByIndex(ju.OverBuiltinIdx)
				if over == 0 {
					unmarshalErr = fmt.Errorf("flecs: unmarshal failed: unit over builtin index %d is not a built-in unit", ju.OverBuiltinIdx)
					return
				}
			}
			w.unitDefs[unitID] = Unit{Symbol: ju.Symbol, Name: ju.Name, Base: base, Factor: ju.Factor, Over: over, Power: ju.Power}

			// Restore compound def if present.
			if len(ju.NumFactors) > 0 || len(ju.DenomFactors) > 0 {
				numIDs, err := resolveUnitFactors(ju.NumFactors, serialToID, w)
				if err != nil {
					unmarshalErr = fmt.Errorf("flecs: unmarshal failed: unit %d num_factors: %w", ju.EntitySerial, err)
					return
				}
				denomIDs, err := resolveUnitFactors(ju.DenomFactors, serialToID, w)
				if err != nil {
					unmarshalErr = fmt.Errorf("flecs: unmarshal failed: unit %d denom_factors: %w", ju.EntitySerial, err)
					return
				}
				if w.compoundDefs == nil {
					w.compoundDefs = make(map[ID]*compoundDef)
				}
				w.compoundDefs[unitID] = &compoundDef{numerators: numIDs, denominators: denomIDs}
			}
		}

		// Phase 6: restore component→unit mappings.
		for _, cu := range jw.CompUnits {
			info, ok := compByName[cu.CompName]
			if !ok {
				unmarshalErr = fmt.Errorf("flecs: unmarshal failed: comp_unit component %q is not registered", cu.CompName)
				return
			}
			var unitID ID
			switch {
			case cu.UnitSerial != 0:
				unitID, ok = serialToID[cu.UnitSerial]
				if !ok {
					unmarshalErr = fmt.Errorf("flecs: unmarshal failed: comp_unit unit serial %d not found", cu.UnitSerial)
					return
				}
			case cu.UnitBuiltinIdx != 0:
				unitID = w.builtinUnitByIndex(cu.UnitBuiltinIdx)
				if unitID == 0 {
					unmarshalErr = fmt.Errorf("flecs: unmarshal failed: comp_unit builtin index %d is not a built-in unit", cu.UnitBuiltinIdx)
					return
				}
			default:
				continue
			}
			w.componentUnits[info.ID] = unitID
		}

		if w.logger != nil {
			w.logger.LogAttrs(context.Background(), slog.LevelDebug, "snapshot loaded",
				slog.Int("entities", len(jw.Entities)))
		}
	})

	return unmarshalErr
}

// serializeUnitFactors converts a list of compound unit factor IDs to jsonUnitFactor.
func serializeUnitFactors(ids []ID, idToSerial map[ID]int, w *World) []jsonUnitFactor {
	if len(ids) == 0 {
		return nil
	}
	result := make([]jsonUnitFactor, 0, len(ids))
	for _, id := range ids {
		if isBuiltinUnit(id) {
			result = append(result, jsonUnitFactor{BuiltinIdx: id.Index()})
		} else if serial, ok := idToSerial[id]; ok {
			result = append(result, jsonUnitFactor{Serial: serial})
		}
	}
	return result
}

// resolveUnitFactors resolves a list of jsonUnitFactor to entity IDs during unmarshal.
func resolveUnitFactors(factors []jsonUnitFactor, serialToID map[int]ID, w *World) ([]ID, error) {
	if len(factors) == 0 {
		return nil, nil
	}
	ids := make([]ID, 0, len(factors))
	for _, f := range factors {
		switch {
		case f.Serial != 0:
			id, ok := serialToID[f.Serial]
			if !ok {
				return nil, fmt.Errorf("factor serial %d not found", f.Serial)
			}
			ids = append(ids, id)
		case f.BuiltinIdx != 0:
			id := w.builtinUnitByIndex(f.BuiltinIdx)
			if id == 0 {
				return nil, fmt.Errorf("factor builtin index %d is not a built-in unit", f.BuiltinIdx)
			}
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// unmarshalDynamic decodes a dynamic component value from raw JSON and calls
// SetIDPtr to write it to the entity's slot. Uses the registered custom unmarshaler
// if available; otherwise decodes as a base64-encoded raw byte string.
func unmarshalDynamic(w *World, fw *Writer, e ID, id ID, size uintptr, raw json.RawMessage) error {
	if hooks, ok := w.dynamicMarshalers[id]; ok {
		buf := make([]byte, size)
		if err := hooks.unmarshal(raw, unsafe.Pointer(&buf[0])); err != nil {
			return fmt.Errorf("flecs: unmarshal failed: dynamic component %d: %w", uint64(id), err)
		}
		SetIDPtr(fw, e, id, unsafe.Pointer(&buf[0]))
		return nil
	}
	var b64 string
	if err := json.Unmarshal(raw, &b64); err != nil {
		return fmt.Errorf("flecs: unmarshal failed: dynamic component %d (expected base64 string): %w", uint64(id), err)
	}
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("flecs: unmarshal failed: dynamic component %d (base64 decode): %w", uint64(id), err)
	}
	if uintptr(len(decoded)) != size {
		return fmt.Errorf("flecs: unmarshal failed: dynamic component %d size mismatch: got %d bytes, expected %d", uint64(id), len(decoded), size)
	}
	SetIDPtr(fw, e, id, unsafe.Pointer(&decoded[0]))
	return nil
}
