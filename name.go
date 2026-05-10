package flecs

import "strings"

// Name is the built-in name component. Entities with a Name can be addressed
// by dot-separated path strings and located via Lookup / LookupChild. Names
// may not contain "."; "." is the path separator. Name uniqueness is not
// enforced: two siblings may share a name; LookupChild returns the first match
// in iteration order.
type Name struct {
	Value string
}

// Name returns the built-in Name component entity ID.
func (w *World) Name() ID { return w.nameID }

// SetName sets the Name component on entity e to name.
// Equivalent to Set[Name](w, e, Name{Value: name}). Panics if e is not alive.
func (w *World) SetName(e ID, name string) {
	Set[Name](w, e, Name{Value: name})
}

// GetName returns the name of entity e.
//
// Returns ("", false) if e is dead, has no Name component, or has a Name whose
// Value is the empty string. An empty Value is treated as "unnamed" for path
// purposes. Inherited names via IsA are visible (same as Get[Name] semantics).
func (w *World) GetName(e ID) (string, bool) {
	n, ok := Get[Name](w, e)
	if !ok || n.Value == "" {
		return "", false
	}
	return n.Value, true
}

// RemoveName removes the Name component from entity e.
// Returns true if e had a Name, false if e was dead or had no Name.
func (w *World) RemoveName(e ID) bool {
	return Remove[Name](w, e)
}

// LookupChild finds the direct child of parent with the given name. If parent
// is 0, searches among root-scope entities — alive entities with no ChildOf
// relationship. Returns (0, false) if no match is found.
//
// When sibling names collide, the first match in iteration order is returned.
// Behavior is undefined when sibling names collide; do not rely on ordering.
func (w *World) LookupChild(parent ID, name string) (ID, bool) {
	if parent != 0 {
		var found ID
		w.EachChild(parent, func(child ID) bool {
			if n, ok := w.GetName(child); ok && n == name {
				found = child
				return false
			}
			return true
		})
		if found != 0 {
			return found, true
		}
		return 0, false
	}
	// Root scope: scan all alive entities for those locally named with no parent.
	var found ID
	w.eachAlive(func(id ID) {
		if found != 0 {
			return
		}
		if !Owns[Name](w, id) {
			return
		}
		if _, hasParent := w.ParentOf(id); hasParent {
			return
		}
		if n, ok := w.GetName(id); ok && n == name {
			found = id
		}
	})
	if found != 0 {
		return found, true
	}
	return 0, false
}

// Lookup resolves a dot-separated path and returns the entity at the leaf.
//
// Rules:
//   - Empty path → (0, false).
//   - Single segment ("foo") → LookupChild(0, "foo") semantics (root scope).
//   - Any empty segment (leading dot, trailing dot, consecutive dots) → (0, false).
//   - Names containing "." are not supported.
func (w *World) Lookup(path string) (ID, bool) {
	if path == "" {
		return 0, false
	}
	segments := strings.Split(path, ".")
	for _, seg := range segments {
		if seg == "" {
			return 0, false
		}
	}
	var current ID // 0 = root scope
	for _, seg := range segments {
		var ok bool
		current, ok = w.LookupChild(current, seg)
		if !ok {
			return 0, false
		}
	}
	return current, true
}

// PathOf reconstructs e's dot-separated path from the root.
//
// Semantics:
//   - Dead or unnamed entity → "".
//   - Named entity with no ChildOf → its name.
//   - Named entity with a named ChildOf parent → "<parent_path>.<e_name>".
//   - If a ChildOf ancestor is unnamed, the walk stops at the first unnamed
//     ancestor and returns the path built from e up to (but not including) that
//     ancestor. For example, if "wheel" → "car" (unnamed) → "scene", PathOf
//     returns "wheel", not "car.wheel" or "scene.car.wheel".
func (w *World) PathOf(e ID) string {
	name, ok := w.GetName(e)
	if !ok {
		return ""
	}
	segments := []string{name}
	cur := e
	seen := map[ID]struct{}{e: {}}
	for {
		parent, hasParent := w.ParentOf(cur)
		if !hasParent {
			break
		}
		if _, visited := seen[parent]; visited {
			break // cycle guard
		}
		seen[parent] = struct{}{}
		pname, ok := w.GetName(parent)
		if !ok {
			break // unnamed ancestor: stop here
		}
		segments = append(segments, pname)
		cur = parent
	}
	// segments is in reverse order: [e_name, parent_name, grandparent_name, ...]
	for i, j := 0, len(segments)-1; i < j; i, j = i+1, j-1 {
		segments[i], segments[j] = segments[j], segments[i]
	}
	return strings.Join(segments, ".")
}
