package flecs

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
)

// NewRESTHandler returns an http.Handler that exposes read-only inspection
// endpoints and snapshot save/load for w.
//
// # Concurrency
//
// *World is NOT goroutine-safe. The REST handler treats the world as read-only
// for all GET endpoints; concurrent GET requests must be externally serialized,
// or the world must not be mutated while they run.
//
// PUT /snapshot replaces world state and MUST NOT run concurrently with any
// other world operation — whether another GET request, a direct Set/Delete
// call, or a second PUT /snapshot. Callers that need concurrent access must
// add their own mutex or run all HTTP requests on the same goroutine as the
// world.
//
// # Routes
//
//	GET /stats              — world stats JSON (200)
//	GET /components         — all registered component infos (200)
//	GET /components/{id}    — single component by uint64 ID (200 or 404)
//	GET /entities           — alive entities; optional ?limit=N (default 1000, max 10000) (200 or 400)
//	GET /entities/{id}      — entity detail (200 or 404)
//	GET /snapshot           — full world MarshalJSON output (200 or 500)
//	PUT /snapshot           — replace world state; body is MarshalJSON output (204, 400)
func NewRESTHandler(w *World) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stats", restStats(w))
	mux.HandleFunc("GET /components", restComponents(w))
	mux.HandleFunc("GET /components/{id}", restComponentByID(w))
	mux.HandleFunc("GET /entities", restEntities(w))
	mux.HandleFunc("GET /entities/{id}", restEntityByID(w))
	mux.HandleFunc("GET /snapshot", restSnapshotGet(w))
	mux.HandleFunc("PUT /snapshot", restSnapshotPut(w))
	return mux
}

// componentInfoResponse is the JSON shape for a registered component.
type componentInfoResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Size  uint64 `json:"size"`
	Align uint64 `json:"align"`
	Type  string `json:"type"`
}

// entityListItem is the JSON shape for a single entry in the entity list.
type entityListItem struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// entityDetailResponse is the JSON shape returned by GET /entities/{id}.
type entityDetailResponse struct {
	ID         string                  `json:"id"`
	Name       string                  `json:"name,omitempty"`
	Parent     string                  `json:"parent,omitempty"`
	Prefabs    []string                `json:"prefabs,omitempty"`
	Components []componentInfoResponse `json:"components"`
	Pairs      []string                `json:"pairs,omitempty"`
}

func toComponentInfoResponse(info ComponentInfo) componentInfoResponse {
	typeName := ""
	if info.Type != nil {
		typeName = info.Type.String()
	}
	return componentInfoResponse{
		ID:    strconv.FormatUint(uint64(info.ID), 10),
		Name:  info.Name,
		Size:  uint64(info.Size),
		Align: uint64(info.Align),
		Type:  typeName,
	}
}

func parseID(s string) (ID, bool) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return ID(n), true
}

func writeJSON(rw http.ResponseWriter, status int, v any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(v)
}

func writeError(rw http.ResponseWriter, status int, msg string) {
	writeJSON(rw, status, map[string]string{"error": msg})
}

func restStats(w *World) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		var stats Stats
		w.Readonly(func() { stats = w.Stats() })
		writeJSON(rw, http.StatusOK, stats)
	}
}

func restComponents(w *World) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		var out []componentInfoResponse
		w.Readonly(func() {
			ids := w.Components()
			out = make([]componentInfoResponse, 0, len(ids))
			for _, id := range ids {
				info, ok := w.ComponentInfo(id)
				if !ok {
					continue
				}
				out = append(out, toComponentInfoResponse(info))
			}
		})
		writeJSON(rw, http.StatusOK, out)
	}
}

func restComponentByID(w *World) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		id, ok := parseID(r.PathValue("id"))
		if !ok {
			writeError(rw, http.StatusBadRequest, "invalid id")
			return
		}
		var info ComponentInfo
		var found bool
		w.Readonly(func() { info, found = w.ComponentInfo(id) })
		if !found {
			writeError(rw, http.StatusNotFound, "component not found")
			return
		}
		writeJSON(rw, http.StatusOK, toComponentInfoResponse(info))
	}
}

func restEntities(w *World) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		limit := 1000
		if s := r.URL.Query().Get("limit"); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 || n > 10000 {
				writeError(rw, http.StatusBadRequest, "limit must be an integer between 1 and 10000")
				return
			}
			limit = n
		}
		var out []entityListItem
		w.Readonly(func() {
			out = make([]entityListItem, 0, limit)
			w.EachEntity(func(e ID) bool {
				if len(out) >= limit {
					return false
				}
				item := entityListItem{ID: strconv.FormatUint(uint64(e), 10)}
				if name, ok := w.GetName(e); ok {
					item.Name = name
				}
				out = append(out, item)
				return true
			})
		})
		writeJSON(rw, http.StatusOK, out)
	}
}

func restEntityByID(w *World) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		id, ok := parseID(r.PathValue("id"))
		if !ok {
			writeError(rw, http.StatusBadRequest, "invalid id")
			return
		}
		var resp entityDetailResponse
		var alive bool
		w.Readonly(func() {
			if !w.IsAlive(id) {
				return
			}
			alive = true
			resp = entityDetailResponse{
				ID:         strconv.FormatUint(uint64(id), 10),
				Components: []componentInfoResponse{},
			}
			if name, nameOK := w.GetName(id); nameOK {
				resp.Name = name
			}
			if parent, parentOK := w.ParentOf(id); parentOK {
				resp.Parent = strconv.FormatUint(uint64(parent), 10)
			}
			w.EachPrefab(id, func(prefab ID) bool {
				resp.Prefabs = append(resp.Prefabs, strconv.FormatUint(uint64(prefab), 10))
				return true
			})
			childOfIdx := w.ChildOf().Index()
			isAIdx := w.IsA().Index()
			for _, cid := range w.EntityComponents(id) {
				if cid.IsPair() {
					firstIdx := cid.First().Index()
					if firstIdx == childOfIdx || firstIdx == isAIdx {
						continue
					}
					resp.Pairs = append(resp.Pairs, strconv.FormatUint(uint64(cid), 10))
					continue
				}
				info, infoOK := w.ComponentInfo(cid)
				if !infoOK {
					continue
				}
				resp.Components = append(resp.Components, toComponentInfoResponse(info))
			}
		})
		if !alive {
			writeError(rw, http.StatusNotFound, "entity not found")
			return
		}
		writeJSON(rw, http.StatusOK, resp)
	}
}

func restSnapshotGet(w *World) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		var data []byte
		var marshalErr error
		w.Readonly(func() { data, marshalErr = w.MarshalJSON() })
		if marshalErr != nil {
			writeError(rw, http.StatusInternalServerError, marshalErr.Error())
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write(data)
	}
}

func restSnapshotPut(w *World) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(rw, http.StatusBadRequest, "failed to read request body")
			return
		}
		if !json.Valid(body) {
			writeError(rw, http.StatusBadRequest, "request body is not valid JSON")
			return
		}
		if err := w.UnmarshalJSON(body); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		rw.WriteHeader(http.StatusNoContent)
	}
}
