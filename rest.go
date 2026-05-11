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
// GET endpoints (stats, components, entities, snapshot) each acquire the
// world's read lock ([World.RLock]) for the duration of the handler. Multiple
// concurrent GET requests proceed in parallel and are safe to issue while a
// goroutine runs [World.Progress].
//
// PUT /snapshot acquires the world's write lock for the duration of the
// unmarshal. It may not run concurrently with Progress or any other write;
// Progress will block until the PUT completes, and vice versa.
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
		writeJSON(rw, http.StatusOK, w.Stats())
	}
}

func restComponents(w *World) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		w.RLock()
		ids := w.registry.IDs()
		out := make([]componentInfoResponse, 0, len(ids))
		for _, id := range ids {
			info, ok := componentInfoUnlocked(w, id)
			if !ok {
				continue
			}
			out = append(out, toComponentInfoResponse(info))
		}
		w.RUnlock()
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
		w.RLock()
		info, ok := componentInfoUnlocked(w, id)
		w.RUnlock()
		if !ok {
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
		w.RLock()
		out := make([]entityListItem, 0, limit)
		w.index.EachID(func(e ID) bool {
			if len(out) >= limit {
				return false
			}
			item := entityListItem{ID: strconv.FormatUint(uint64(e), 10)}
			if name, ok := getNameUnlocked(w, e); ok {
				item.Name = name
			}
			out = append(out, item)
			return true
		})
		w.RUnlock()
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
		w.RLock()
		alive := w.index.IsAlive(id)
		if !alive {
			w.RUnlock()
			writeError(rw, http.StatusNotFound, "entity not found")
			return
		}
		resp := entityDetailResponse{
			ID:         strconv.FormatUint(uint64(id), 10),
			Components: []componentInfoResponse{},
		}
		if name, nameOK := getNameUnlocked(w, id); nameOK {
			resp.Name = name
		}
		if parent, parentOK := parentOfUnlocked(w, id); parentOK {
			resp.Parent = strconv.FormatUint(uint64(parent), 10)
		}
		rec := w.index.Get(id)
		if rec != nil && rec.Table != nil {
			isAIdx := w.isAID.Index()
			eachPairTarget(rec.Table.Type(), isAIdx, func(prefab ID) bool {
				resp.Prefabs = append(resp.Prefabs, strconv.FormatUint(uint64(prefab), 10))
				return true
			})
		}
		childOfIdx := w.childOfID.Index()
		isAIdx := w.isAID.Index()
		for _, cid := range entityComponentsUnlocked(w, id) {
			if cid.IsPair() {
				firstIdx := cid.First().Index()
				if firstIdx == childOfIdx || firstIdx == isAIdx {
					continue
				}
				resp.Pairs = append(resp.Pairs, strconv.FormatUint(uint64(cid), 10))
				continue
			}
			info, infoOK := componentInfoUnlocked(w, cid)
			if !infoOK {
				continue
			}
			resp.Components = append(resp.Components, toComponentInfoResponse(info))
		}
		w.RUnlock()
		writeJSON(rw, http.StatusOK, resp)
	}
}

func restSnapshotGet(w *World) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		data, err := w.MarshalJSON()
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
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
