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
	Components map[string]json.RawMessage `json:"components,omitempty"`
}

// MarshalJSON serializes the world to JSON.
//
// Format (v1): version=1, entities array with serial numbers (starting at 1),
// optional name field, optional parent field (serial of the ChildOf parent),
// and components map keyed by ComponentInfo.Name.
//
// Built-in entities (ChildOf, IsA, Name component entity, PreUpdate, OnUpdate,
// PostUpdate, OnFixedUpdate) are skipped. Pair components are skipped (deferred
// to Phase 9.2.4). The Name component value is surfaced as the "name" field,
// not as a components map entry.
//
// Entities are emitted in topological order (parents before children) so that
// UnmarshalJSON can restore relationships with a single sequential pass.
// Siblings are ordered by entity allocation order. The "parent" field holds the
// serial number of the ChildOf parent; it is omitted when zero.
//
// Only the first ChildOf parent (in signature order) is serialized when an
// entity has multiple ChildOf relationships (a legal but unusual configuration).
//
// Returns an error if a ChildOf cycle is detected during serialization.
func (w *World) MarshalJSON() ([]byte, error) {
	// Build the skip-set from built-in IDs — no hardcoded magic numbers.
	// Also skip all registered component entities (they are alive but not user data).
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

	// Collect user entities in allocation order.
	var userEnts []ID
	w.EachEntity(func(e ID) bool {
		if _, isBuiltin := skip[e]; !isBuiltin {
			userEnts = append(userEnts, e)
		}
		return true
	})

	// Build parentOf map: child → user-entity parent only.
	// Built-in parents are treated as "no parent" for serialization purposes.
	parentOf := make(map[ID]ID, len(userEnts))
	for _, e := range userEnts {
		if p, ok := w.ParentOf(e); ok {
			if _, isBuiltin := skip[p]; !isBuiltin {
				parentOf[e] = p
			}
		}
	}

	// Topological sort (parent-before-child) via iterative DFS.
	// Iterating userEnts in allocation order keeps sibling order stable.
	visited := make(map[ID]struct{}, len(userEnts))
	visiting := make(map[ID]struct{})
	order := make([]ID, 0, len(userEnts))

	var visit func(e ID) error
	visit = func(e ID) error {
		if _, done := visited[e]; done {
			return nil
		}
		if _, active := visiting[e]; active {
			return fmt.Errorf("flecs: marshal failed: ChildOf cycle detected involving entity %d", uint64(e))
		}
		visiting[e] = struct{}{}
		if p, ok := parentOf[e]; ok {
			if err := visit(p); err != nil {
				return err
			}
		}
		delete(visiting, e)
		visited[e] = struct{}{}
		order = append(order, e)
		return nil
	}

	for _, e := range userEnts {
		if err := visit(e); err != nil {
			return nil, err
		}
	}

	// Assign serials in topo order; build reverse map.
	idToSerial := make(map[ID]int, len(order))
	for i, e := range order {
		idToSerial[e] = i + 1
	}

	nameID := w.Name()
	var entities []jsonEntity

	for _, e := range order {
		je := jsonEntity{Serial: idToSerial[e]}

		if name, ok := w.GetName(e); ok {
			je.Name = name
		}

		// Emit parent serial if the parent is a user entity.
		if p, ok := w.ParentOf(e); ok {
			if _, isBuiltin := skip[p]; !isBuiltin {
				if serial, ok := idToSerial[p]; ok {
					je.Parent = serial
				}
			}
		}

		for _, cid := range w.EntityComponents(e) {
			// Skip pair components (Phase 9.2.4).
			if cid.IsPair() {
				continue
			}
			// Skip the Name component — it's already in the "name" field.
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
// and components from JSON produced by MarshalJSON. The world need not be
// empty; new entities are added to existing ones.
//
// All component types present in the JSON must be pre-registered via
// RegisterComponent[T] before calling UnmarshalJSON. IsA prefab relationships
// are not restored (Phase 9.2.3). Custom pair components are not restored
// (Phase 9.2.4).
//
// Error cases:
//   - JSON parse error → wrapped error.
//   - Unsupported version → descriptive error.
//   - Parent serial not found in document → descriptive error.
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

	// Build a name→ComponentInfo lookup from all registered components.
	compByName := make(map[string]ComponentInfo)
	for _, cid := range w.Components() {
		info, ok := w.ComponentInfo(cid)
		if !ok {
			continue
		}
		compByName[info.Name] = info
	}

	// Phase 1: allocate all entities so future phases can resolve serial→ID.
	serialToID := make(map[int]ID, len(jw.Entities))
	for _, je := range jw.Entities {
		serialToID[je.Serial] = w.NewEntity()
	}

	// Phase 2: set names, restore ChildOf relationships, then set components.
	// JSON guarantees topological order (parents before children), so parent
	// entities are already allocated and their ChildOf is set when children
	// are processed. ChildOf is applied before components so hooks fire in
	// a clean structural order.
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
		for compName, raw := range je.Components {
			info, ok := compByName[compName]
			if !ok {
				return fmt.Errorf("flecs: unmarshal failed: component %q is not registered in the world", compName)
			}
			// Raw tag with no Go type — skip (cannot decode from JSON without a type).
			if info.Type == nil {
				continue
			}
			// Zero-size tag (e.g. struct{}) — SetByID with zero value; no decode needed.
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
