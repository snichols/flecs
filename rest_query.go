package flecs

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"unsafe"
)

const (
	queryDefaultLimit = 256
	queryMaxLimit     = 4096
)

// queryResultField holds the JSON value for one component on one entity.
// nil means the component is a tag (no data); the key will still be omitted.
type queryResultEntry struct {
	Entity int64                      `json:"entity"`
	Path   string                     `json:"path"`
	Fields map[string]json.RawMessage `json:"fields,omitempty"`
}

// queryResponse is the top-level JSON envelope for GET /query.
type queryResponse struct {
	Expr    string             `json:"expr"`
	Count   int                `json:"count"`
	Limit   int                `json:"limit"`
	Offset  int                `json:"offset"`
	Results []queryResultEntry `json:"results"`
}

func restQuery(w *World) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		expr := r.URL.Query().Get("expr")
		if strings.TrimSpace(expr) == "" {
			writeError(rw, http.StatusBadRequest, "missing required parameter: expr")
			return
		}

		limit := queryDefaultLimit
		if s := r.URL.Query().Get("limit"); s != "" {
			v, err := strconv.Atoi(s)
			if err != nil || v < 0 {
				writeError(rw, http.StatusBadRequest, "invalid limit: must be a non-negative integer")
				return
			}
			if v > queryMaxLimit {
				writeError(rw, http.StatusBadRequest, "limit exceeds maximum of 4096")
				return
			}
			limit = v
		}

		offset := 0
		if s := r.URL.Query().Get("offset"); s != "" {
			v, err := strconv.Atoi(s)
			if err != nil || v < 0 {
				writeError(rw, http.StatusBadRequest, "invalid offset: must be a non-negative integer")
				return
			}
			offset = v
		}

		includeFields := true
		if s := r.URL.Query().Get("fields"); s != "" {
			switch s {
			case "true":
				includeFields = true
			case "false":
				includeFields = false
			default:
				writeError(rw, http.StatusBadRequest, "invalid fields: must be true or false")
				return
			}
		}

		var (
			resp     queryResponse
			parseErr *ParseQueryError
			panicVal any
		)

		func() {
			defer func() { panicVal = recover() }()
			w.Read(func(fr *Reader) {
				terms, err := parseQueryExpr(expr, w)
				if err != nil {
					if pe, ok := err.(*ParseQueryError); ok {
						parseErr = pe
					} else {
						parseErr = &ParseQueryError{Msg: err.Error()}
					}
					return
				}

				q := NewQueryFromTerms(w, terms...)
				it := q.Iter()

				// Collect field-capable term IDs (TermAnd only, non-wildcard).
				type fieldSpec struct {
					id  ID
					key string
				}
				var fieldSpecs []fieldSpec
				if includeFields {
					for _, t := range terms {
						if t.Kind != TermAnd {
							continue
						}
						id := t.ID
						if id.IsPair() {
							// Skip wildcard pairs — concrete target unknown statically.
							if isWildcardID(w, id.First()) || isWildcardID(w, id.Second()) {
								continue
							}
							rel := w.PathOf(id.First())
							tgt := w.PathOf(id.Second())
							if rel == "" {
								rel = strconv.FormatUint(uint64(id.First()), 10)
							}
							if tgt == "" {
								tgt = strconv.FormatUint(uint64(id.Second()), 10)
							}
							fieldSpecs = append(fieldSpecs, fieldSpec{id: id, key: rel + "~" + tgt})
						} else {
							key := w.PathOf(id)
							if key == "" {
								key = strconv.FormatUint(uint64(id), 10)
							}
							fieldSpecs = append(fieldSpecs, fieldSpec{id: id, key: key})
						}
					}
				}

				var results []queryResultEntry
				matchIdx := 0 // 0-based index across all matched entities

				for it.Next() {
					for _, e := range it.Entities() {
						inWindow := matchIdx >= offset && (matchIdx-offset) < limit
						if inWindow {
							entry := queryResultEntry{
								Entity: int64(e),
								Path:   w.PathOf(e),
							}

							if includeFields && len(fieldSpecs) > 0 {
								fields := make(map[string]json.RawMessage)
								for _, fs := range fieldSpecs {
									raw, ok := marshalComponentForQuery(w, fr, e, fs.id)
									if !ok {
										continue
									}
									fields[fs.key] = raw
								}
								if len(fields) > 0 {
									entry.Fields = fields
								}
							}

							results = append(results, entry)
						}
						matchIdx++
					}
				}

				resp = queryResponse{
					Expr:    expr,
					Count:   matchIdx,
					Limit:   limit,
					Offset:  offset,
					Results: results,
				}
				if resp.Results == nil {
					resp.Results = []queryResultEntry{}
				}
			})
		}()

		if panicVal != nil {
			writeError(rw, http.StatusServiceUnavailable, "world unavailable")
			return
		}
		if parseErr != nil {
			writeError(rw, http.StatusBadRequest, parseErr.Error())
			return
		}

		writeJSON(rw, http.StatusOK, resp)
	}
}

// marshalComponentForQuery returns the JSON encoding of compID on entity e.
// Returns (nil, false) for tags and components not present on e.
func marshalComponentForQuery(w *World, fr *Reader, e ID, compID ID) (json.RawMessage, bool) {
	if !fr.HasID(e, compID) {
		return nil, false
	}
	ti, hasInfo := w.registry.LookupByID(compID)
	if !hasInfo || ti.Size == 0 {
		// Tag: no data, skip.
		return nil, false
	}
	if ti.Type == nil {
		// Dynamic component.
		ptr := GetIDPtr(fr, e, compID)
		if ptr == nil {
			return nil, false
		}
		if hooks, ok := w.dynamicMarshalers[compID]; ok {
			raw, err := hooks.marshal(ptr)
			if err != nil {
				return nil, false
			}
			return raw, true
		}
		// Dynamic without marshaler: base64.
		b64 := base64.StdEncoding.EncodeToString(unsafe.Slice((*byte)(ptr), ti.Size))
		raw, err := json.Marshal(b64)
		if err != nil {
			return nil, false
		}
		return raw, true
	}
	// Typed component: use reflect to build the value then marshal.
	ptr := GetIDPtr(fr, e, compID)
	if ptr == nil {
		// Try GetByID fallback.
		v, ok := fr.GetByID(e, compID)
		if !ok {
			return nil, false
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}
		return raw, true
	}
	val := reflect.NewAt(ti.Type, ptr).Elem().Interface()
	raw, err := json.Marshal(val)
	if err != nil {
		return nil, false
	}
	return raw, true
}
