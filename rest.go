package flecs

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"time"
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
// GET /stats/world and GET /stats/pipeline are the exception: they call
// w.StatsSnapshot() directly, which acquires only a stats read-lock and is
// goroutine-safe without an outer w.Read scope.
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
//	GET /stats/world        — WorldStats snapshot as JSON; Cache-Control: no-store (200 or 503)
//	GET /stats/pipeline     — PipelineStats snapshot as JSON; Cache-Control: no-store (200 or 503)
//	GET /components         — all registered component infos (200)
//	GET /components/{id}    — single component by uint64 ID (200 or 404)
//	GET /entities           — alive entities; optional ?limit=N (default 1000, max 10000) (200 or 400)
//	GET /entities/{id}      — entity detail (200 or 404)
//	GET /snapshot           — full world MarshalJSON output (200 or 500)
//	PUT /snapshot           — replace world state; body is MarshalJSON output (204, 400)
//	GET /type_info/{path}   — reflection schema for a named component (200, 404)
func NewRESTHandler(w *World) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stats", restStats(w))
	mux.HandleFunc("GET /stats/world", restStatsWorld(w))
	mux.HandleFunc("GET /stats/pipeline", restStatsPipeline(w))
	mux.HandleFunc("GET /components", restComponents(w))
	mux.HandleFunc("GET /components/{id}", restComponentByID(w))
	mux.HandleFunc("GET /entities", restEntities(w))
	mux.HandleFunc("GET /entities/{id}", restEntityByID(w))
	mux.HandleFunc("GET /snapshot", restSnapshotGet(w))
	mux.HandleFunc("PUT /snapshot", restSnapshotPut(w))
	mux.HandleFunc("GET /type_info/{path...}", restTypeInfo(w))
	return mux
}

// worldStatsResponse is the JSON shape for the WorldStats portion of a snapshot.
// Field names are snake_case to match upstream REST conventions.
type worldStatsResponse struct {
	EntityCount    int     `json:"entity_count"`
	TableCount     int     `json:"table_count"`
	ArchetypeCount int     `json:"archetype_count"`
	FrameCount     uint64  `json:"frame_count"`
	TotalTime      float64 `json:"total_time"`
	LastTickDelta  float64 `json:"last_tick_delta"`
}

// systemStatsResponse is the JSON shape for a single SystemStats entry.
type systemStatsResponse struct {
	Name             string        `json:"name"`
	LastTickDuration time.Duration `json:"last_tick_duration"`
	Invocations      uint64        `json:"invocations"`
	AvgDuration      time.Duration `json:"avg_duration"`
	TotalSkipped     uint64        `json:"total_skipped"`
}

// phaseStatsResponse is the JSON shape for a single PhaseStats entry.
type phaseStatsResponse struct {
	Name               string        `json:"name"`
	SystemCount        int           `json:"system_count"`
	Duration           time.Duration `json:"duration"`
	CumulativeDuration time.Duration `json:"cumulative_duration"`
	Invocations        uint64        `json:"invocations"`
}

// pipelineStatsResponse is the JSON shape for the GET /stats/pipeline endpoint.
type pipelineStatsResponse struct {
	World   worldStatsResponse    `json:"world"`
	Systems []systemStatsResponse `json:"systems"`
	Phases  []phaseStatsResponse  `json:"phases"`
}

func toWorldStatsResponse(s WorldStats) worldStatsResponse {
	return worldStatsResponse(s)
}

func toPipelineStatsResponse(snap PipelineStats) pipelineStatsResponse {
	systems := make([]systemStatsResponse, len(snap.Systems))
	for i, s := range snap.Systems {
		systems[i] = systemStatsResponse(s)
	}
	phases := make([]phaseStatsResponse, len(snap.Phases))
	for i, p := range snap.Phases {
		phases[i] = phaseStatsResponse(p)
	}
	return pipelineStatsResponse{
		World:   toWorldStatsResponse(snap.World),
		Systems: systems,
		Phases:  phases,
	}
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

// typeInfoFieldResponse is the JSON shape for a single field entry in a type_info response.
type typeInfoFieldResponse struct {
	Name   string  `json:"name"`
	Type   string  `json:"type"`
	Offset uintptr `json:"offset"`
}

// typeInfoResponse is the JSON shape for GET /type_info/{path}.
type typeInfoResponse struct {
	Name         string                  `json:"name"`
	Size         uintptr                 `json:"size"`
	Align        uintptr                 `json:"align"`
	Fields       []typeInfoFieldResponse `json:"fields"`
	Opaque       bool                    `json:"opaque,omitempty"`
	Unit         string                  `json:"unit,omitempty"`
	IsPair       bool                    `json:"is_pair,omitempty"`
	Relationship string                  `json:"relationship,omitempty"`
	Target       string                  `json:"target,omitempty"`
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
		w.Read(func(fr *Reader) { stats = fr.Stats() })
		writeJSON(rw, http.StatusOK, stats)
	}
}

func restStatsWorld(w *World) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				writeError(rw, http.StatusServiceUnavailable, "world unavailable")
			}
		}()
		snap := w.StatsSnapshot()
		rw.Header().Set("Cache-Control", "no-store")
		writeJSON(rw, http.StatusOK, map[string]worldStatsResponse{"world": toWorldStatsResponse(snap.World)})
	}
}

func restStatsPipeline(w *World) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				writeError(rw, http.StatusServiceUnavailable, "world unavailable")
			}
		}()
		snap := w.StatsSnapshot()
		rw.Header().Set("Cache-Control", "no-store")
		writeJSON(rw, http.StatusOK, toPipelineStatsResponse(snap))
	}
}

func restComponents(w *World) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		var out []componentInfoResponse
		w.Read(func(fr *Reader) {
			ids := fr.Components()
			out = make([]componentInfoResponse, 0, len(ids))
			for _, id := range ids {
				info, ok := fr.ComponentInfo(id)
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
		w.Read(func(fr *Reader) { info, found = fr.ComponentInfo(id) })
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
		w.Read(func(fr *Reader) {
			out = make([]entityListItem, 0, limit)
			fr.EachEntity(func(e ID) bool {
				if len(out) >= limit {
					return false
				}
				item := entityListItem{ID: strconv.FormatUint(uint64(e), 10)}
				if name, ok := fr.GetName(e); ok {
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
		w.Read(func(fr *Reader) {
			if !fr.IsAlive(id) {
				return
			}
			alive = true
			resp = entityDetailResponse{
				ID:         strconv.FormatUint(uint64(id), 10),
				Components: []componentInfoResponse{},
			}
			if name, nameOK := fr.GetName(id); nameOK {
				resp.Name = name
			}
			if parent, parentOK := fr.ParentOf(id); parentOK {
				resp.Parent = strconv.FormatUint(uint64(parent), 10)
			}
			fr.EachPrefab(id, func(prefab ID) bool {
				resp.Prefabs = append(resp.Prefabs, strconv.FormatUint(uint64(prefab), 10))
				return true
			})
			childOfIdx := w.ChildOf().Index()
			isAIdx := w.IsA().Index()
			for _, cid := range fr.EntityComponents(id) {
				if cid.IsPair() {
					firstIdx := cid.First().Index()
					if firstIdx == childOfIdx || firstIdx == isAIdx {
						continue
					}
					resp.Pairs = append(resp.Pairs, strconv.FormatUint(uint64(cid), 10))
					continue
				}
				info, infoOK := fr.ComponentInfo(cid)
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
		w.Read(func(fr *Reader) { data, marshalErr = w.MarshalJSON() })
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
		var unmarshalErr error
		w.Write(func(fw *Writer) {
			unmarshalErr = w.UnmarshalJSON(body)
		})
		if unmarshalErr != nil {
			writeError(rw, http.StatusBadRequest, unmarshalErr.Error())
			return
		}
		rw.WriteHeader(http.StatusNoContent)
	}
}

func restTypeInfo(w *World) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		path, _ := url.PathUnescape(r.PathValue("path"))

		var resp typeInfoResponse
		var found bool
		w.Read(func(fr *Reader) {
			id, ok := fr.Lookup(path)
			if !ok {
				return
			}
			info, ok := w.registry.LookupByID(id)
			if !ok {
				return
			}
			found = true

			resp = typeInfoResponse{
				Name:   info.Name,
				Size:   info.Size,
				Align:  info.Align,
				Fields: []typeInfoFieldResponse{},
			}

			if info.Type != nil {
				if info.Type.Kind() == reflect.Struct {
					t := info.Type
					n := t.NumField()
					resp.Fields = make([]typeInfoFieldResponse, n)
					for i := range n {
						f := t.Field(i)
						resp.Fields[i] = typeInfoFieldResponse{
							Name:   f.Name,
							Type:   f.Type.String(),
							Offset: f.Offset,
						}
					}
				}
			} else {
				resp.Opaque = true
			}

			if unitID, ok := w.UnitFor(id); ok {
				if name, nameOK := fr.GetName(unitID); nameOK {
					resp.Unit = name
				}
			}
		})

		if !found {
			writeError(rw, http.StatusNotFound, "type info not found")
			return
		}
		rw.Header().Set("Cache-Control", "max-age=300")
		writeJSON(rw, http.StatusOK, resp)
	}
}
