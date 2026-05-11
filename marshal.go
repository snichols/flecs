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
	Components map[string]json.RawMessage `json:"components"`
}

// MarshalJSON serializes the world to JSON.
//
// Format (v1): version=1, entities array with serial numbers (starting at 1),
// optional name field, and components map keyed by ComponentInfo.Name.
//
// Built-in entities (ChildOf, IsA, Name component entity, PreUpdate, OnUpdate,
// PostUpdate, OnFixedUpdate) are skipped. Pair components are skipped (deferred
// to Phase 9.2.4). The Name component value is surfaced as the "name" field,
// not as a components map entry. Tag components serialize as {}.
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

	nameID := w.Name()
	var entities []jsonEntity
	serial := 0

	w.EachEntity(func(e ID) bool {
		if _, isBuiltin := skip[e]; isBuiltin {
			return true
		}
		serial++
		je := jsonEntity{
			Serial:     serial,
			Components: make(map[string]json.RawMessage),
		}
		if name, ok := w.GetName(e); ok {
			je.Name = name
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
				return false
			}
			je.Components[info.Name] = raw
		}
		entities = append(entities, je)
		return true
	})

	jw := jsonWorld{
		Version:  1,
		Entities: entities,
	}
	if jw.Entities == nil {
		jw.Entities = []jsonEntity{}
	}
	return json.Marshal(jw)
}

// UnmarshalJSON restores entities, names, and components from JSON produced by
// MarshalJSON. The world need not be empty; new entities are added to existing
// ones.
//
// All component types present in the JSON must be pre-registered via
// RegisterComponent[T] before calling UnmarshalJSON. Pair components and
// ChildOf/IsA relationships are not restored (Phase 9.2.2–9.2.4).
//
// Error cases:
//   - JSON parse error → wrapped error.
//   - Unsupported version → descriptive error.
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

	// Phase 2: set names and components.
	for _, je := range jw.Entities {
		e := serialToID[je.Serial]
		if je.Name != "" {
			w.SetName(e, je.Name)
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
