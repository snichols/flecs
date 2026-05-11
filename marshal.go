package flecs

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// jsonWorld is the top-level JSON envelope for a serialized world.
type jsonWorld struct {
	Version  int          `json:"version"`
	Entities []jsonEntity `json:"entities"`
}

// jsonEntity is the JSON representation of a single entity.
type jsonEntity struct {
	Serial     int                        `json:"serial"`
	Name       string                     `json:"name,omitempty"`
	Parent     int                        `json:"parent,omitempty"`
	Prefabs    []int                      `json:"prefabs,omitempty"`
	Pairs      []jsonPair                 `json:"pairs,omitempty"`
	Components map[string]json.RawMessage `json:"components,omitempty"`
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
		w.ChildOf():       {},
		w.IsA():           {},
		w.Name():          {},
		w.PreUpdate():     {},
		w.OnUpdate():      {},
		w.PostUpdate():    {},
		w.OnFixedUpdate(): {},
	}
	for _, cid := range w.Components() {
		skip[cid] = struct{}{}
	}

	var userEnts []ID
	w.EachEntity(func(e ID) bool {
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
		if p, ok := w.ParentOf(e); ok {
			if _, isBuiltin := skip[p]; !isBuiltin {
				preds = append(preds, p)
			}
		}
		w.EachPrefab(e, func(prefab ID) bool {
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
			return nil, err
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

		if name, ok := w.GetName(e); ok {
			je.Name = name
		}

		if p, ok := w.ParentOf(e); ok {
			if _, isBuiltin := skip[p]; !isBuiltin {
				if serial, ok := m.idToSerial[p]; ok {
					je.Parent = serial
				}
			}
		}

		// Populate Prefabs after serials are assigned so idToSerial lookups work.
		w.EachPrefab(e, func(prefab ID) bool {
			if _, isBuiltin := skip[prefab]; isBuiltin {
				return true
			}
			if serial, ok := m.idToSerial[prefab]; ok {
				je.Prefabs = append(je.Prefabs, serial)
			}
			return true
		})

		for _, cid := range w.EntityComponents(e) {
			if cid.IsPair() {
				relIdx := uint32(cid.First())
				tgtIdx := uint32(cid.Second())
				if relIdx == childOfIdx || relIdx == isAIdx {
					continue
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
				info, hasInfo := w.ComponentInfo(cid)
				if hasInfo && info.Size > 0 && info.Type != nil {
					v, vok := w.GetByID(e, cid)
					if vok {
						raw, err := json.Marshal(v)
						if err != nil {
							return nil, err
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
			info, ok := w.ComponentInfo(cid)
			if !ok {
				continue
			}
			v, ok := w.GetByID(e, cid)
			if !ok {
				continue
			}
			raw, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			if je.Components == nil {
				je.Components = make(map[string]json.RawMessage)
			}
			je.Components[info.Name] = raw
		}

		entities = append(entities, je)
	}

	jw := jsonWorld{
		Version:  1,
		Entities: entities,
	}
	if jw.Entities == nil {
		jw.Entities = []jsonEntity{}
	}
	return json.Marshal(jw)
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

	compByName := make(map[string]ComponentInfo)
	typeStringToType := make(map[string]reflect.Type)
	for _, cid := range w.Components() {
		info, ok := w.ComponentInfo(cid)
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

	// Phase 1: allocate all entities so future phases can resolve serial→ID.
	serialToID := make(map[int]ID, len(jw.Entities))
	for _, je := range jw.Entities {
		serialToID[je.Serial] = w.NewEntity()
	}

	// Phase 2: name → ChildOf → IsA prefabs → components (JSON is in topo order).
	for _, je := range jw.Entities {
		e := serialToID[je.Serial]
		if je.Name != "" {
			w.SetName(e, je.Name)
		}
		if je.Parent > 0 {
			parentID, ok := serialToID[je.Parent]
			if !ok {
				return fmt.Errorf("flecs: unmarshal failed: entity serial %d references unknown parent serial %d", je.Serial, je.Parent)
			}
			AddID(w, e, MakePair(w.ChildOf(), parentID))
		}
		for _, prefabSerial := range je.Prefabs {
			prefabID, ok := serialToID[prefabSerial]
			if !ok {
				return fmt.Errorf("flecs: unmarshal failed: unknown prefab serial %d", prefabSerial)
			}
			AddID(w, e, MakePair(w.IsA(), prefabID))
		}
		for _, pair := range je.Pairs {
			relID, ok := serialToID[pair.Rel]
			if !ok {
				return fmt.Errorf("flecs: unmarshal failed: pair rel serial %d not found", pair.Rel)
			}
			tgtID, ok := serialToID[pair.Tgt]
			if !ok {
				return fmt.Errorf("flecs: unmarshal failed: pair tgt serial %d not found", pair.Tgt)
			}
			if pair.DataType == "" {
				AddID(w, e, MakePair(relID, tgtID))
			} else {
				foundType, ok := typeStringToType[pair.DataType]
				if !ok {
					return fmt.Errorf("flecs: unmarshal failed: pair data type %q is not registered in the world", pair.DataType)
				}
				vPtr := reflect.New(foundType)
				if err := json.Unmarshal(pair.Data, vPtr.Interface()); err != nil {
					return fmt.Errorf("flecs: unmarshal failed: pair data type %q: %w", pair.DataType, err)
				}
				w.SetPairByID(e, relID, tgtID, vPtr.Elem().Interface())
			}
		}
		for compName, raw := range je.Components {
			info, ok := compByName[compName]
			if !ok {
				return fmt.Errorf("flecs: unmarshal failed: component %q is not registered in the world", compName)
			}
			if info.Type == nil {
				continue
			}
			if info.Size == 0 {
				w.SetByID(e, info.ID, reflect.Zero(info.Type).Interface())
				continue
			}
			ptr := reflect.New(info.Type)
			if err := json.Unmarshal(raw, ptr.Interface()); err != nil {
				return fmt.Errorf("flecs: unmarshal failed: component %q: %w", compName, err)
			}
			w.SetByID(e, info.ID, ptr.Elem().Interface())
		}
	}
	return nil
}
