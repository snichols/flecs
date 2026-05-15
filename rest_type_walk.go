package flecs

import (
	"net/url"
	"reflect"
	"strconv"
)

const (
	defaultWalkDepth = 8
	maxWalkDepth     = 16
)

// typeNodeJSON is a recursive JSON node produced by the depth-N type walk.
type typeNodeJSON struct {
	Name       string         `json:"name,omitempty"`
	Kind       string         `json:"kind"`
	Type       string         `json:"type,omitempty"`
	Underlying string         `json:"underlying,omitempty"`
	Recursive  bool           `json:"recursive,omitempty"`
	Length     int            `json:"length,omitempty"`
	Fields     []typeNodeJSON `json:"fields,omitempty"`
	Element    *typeNodeJSON  `json:"element,omitempty"`
	Key        *typeNodeJSON  `json:"key,omitempty"`
	Value      *typeNodeJSON  `json:"value,omitempty"`
	Unit       string         `json:"unit,omitempty"`
}

// typeInfoResponseV2 is the JSON envelope for GET /type_info/{path} when depth != 1.
type typeInfoResponseV2 struct {
	Name         string         `json:"name"`
	ID           string         `json:"id"`
	Size         uintptr        `json:"size"`
	Align        uintptr        `json:"align"`
	Kind         string         `json:"kind"`
	Fields       []typeNodeJSON `json:"fields,omitempty"`
	Unit         string         `json:"unit,omitempty"`
	IsPair       bool           `json:"is_pair,omitempty"`
	Relationship string         `json:"relationship,omitempty"`
	Target       string         `json:"target,omitempty"`
}

// parseWalkDepth parses the ?depth= query parameter from q.
// Returns (defaultWalkDepth, false, true) when absent.
// Returns (parsed, true, true) for a valid explicit value.
// Returns (0, true, false) for an invalid value.
func parseWalkDepth(q url.Values) (depth int, explicit bool, ok bool) {
	if !q.Has("depth") {
		return defaultWalkDepth, false, true
	}
	s := q.Get("depth")
	d, err := strconv.Atoi(s)
	if err != nil || d < 0 || d > maxWalkDepth {
		return 0, true, false
	}
	return d, true, true
}

// walkTypeForJSON recursively walks t and returns a typeNodeJSON.
// depth is the number of additional struct nesting levels to expand;
// 0 means show the node type but do not expand its fields.
// seen tracks struct types along the current path for cycle detection —
// siblings each receive the same parent seen set, not a post-sibling copy,
// so two siblings with the same type both expand independently.
func walkTypeForJSON(t reflect.Type, depth int, seen map[reflect.Type]bool) typeNodeJSON {
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128,
		reflect.String:
		return primitiveTypeNode(t)

	case reflect.Struct:
		if seen[t] {
			return typeNodeJSON{Kind: "struct", Type: t.String(), Recursive: true}
		}
		if depth == 0 {
			return typeNodeJSON{Kind: "struct", Type: t.String()}
		}
		// Clone seen for this path before adding the current struct, so that
		// all sibling fields of this struct receive the same parent path state.
		childSeen := make(map[reflect.Type]bool, len(seen)+1)
		for k := range seen {
			childSeen[k] = true
		}
		childSeen[t] = true
		fields := make([]typeNodeJSON, 0, t.NumField())
		for i := range t.NumField() {
			sf := t.Field(i)
			if !sf.IsExported() {
				continue
			}
			node := walkTypeForJSON(sf.Type, depth-1, childSeen)
			node.Name = sf.Name
			fields = append(fields, node)
		}
		return typeNodeJSON{Kind: "struct", Type: t.String(), Fields: fields}

	case reflect.Ptr:
		elem := walkTypeForJSON(t.Elem(), depth, seen)
		return typeNodeJSON{Kind: "pointer", Element: &elem}

	case reflect.Slice:
		elem := walkTypeForJSON(t.Elem(), depth, seen)
		return typeNodeJSON{Kind: "slice", Element: &elem}

	case reflect.Array:
		elem := walkTypeForJSON(t.Elem(), depth, seen)
		return typeNodeJSON{Kind: "array", Length: t.Len(), Element: &elem}

	case reflect.Map:
		k := walkTypeForJSON(t.Key(), depth, seen)
		v := walkTypeForJSON(t.Elem(), depth, seen)
		return typeNodeJSON{Kind: "map", Key: &k, Value: &v}

	case reflect.Interface:
		return typeNodeJSON{Kind: "interface"}

	default:
		// Chan, Func, UnsafePointer — opaque, no recursion.
		return typeNodeJSON{Kind: t.Kind().String()}
	}
}

// primitiveTypeNode returns a typeNodeJSON for a primitive kind.
// User-defined named types (e.g. type Score int32) emit the full qualified name
// as "type" and the underlying kind as "underlying".
// Built-in types (bool, int8, float64, string, …) emit the kind string as "type".
func primitiveTypeNode(t reflect.Type) typeNodeJSON {
	underlying := t.Kind().String()
	name := t.Name()
	pkgPath := t.PkgPath()

	// Named user-defined primitive (non-builtin package path, or name differs from kind).
	if pkgPath != "" || (name != "" && name != underlying) {
		return typeNodeJSON{Kind: "primitive", Type: t.String(), Underlying: underlying}
	}

	// Built-in primitive: emit the kind string (e.g. "float64", "uint8", "string").
	return typeNodeJSON{Kind: "primitive", Type: underlying}
}
