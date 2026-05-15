package flecs

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"
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
//	GET /stats                               — world stats JSON (200)
//	GET /stats/world                         — WorldStats snapshot as JSON; Cache-Control: no-store (200 or 503)
//	GET /stats/pipeline                      — PipelineStats snapshot as JSON; Cache-Control: no-store (200 or 503)
//	GET /components                          — all registered component infos (200)
//	GET /components/{id}                     — single component by uint64 ID (200 or 404)
//	GET /entities                            — alive entities; optional ?limit=N (default 1000, max 10000) (200 or 400)
//	GET /entities/{id}                       — entity detail (200 or 404)
//	GET /snapshot                            — full world MarshalJSON output (200 or 500)
//	PUT /snapshot                            — replace world state; body is MarshalJSON output (204, 400)
//	GET /type_info/{path}                    — reflection schema for a named component (200, 404)
//	PUT /entity                              — create or claim entity; JSON body {id?,name?,parent?} (200, 400, 404, 409, 503)
//	DELETE /entity/{path...}                 — delete entity by dot-separated path (200, 400, 404, 503)
//	PUT /component/{entity}/{component}      — set or add a component on an entity; body is JSON (200, 400, 404, 413, 503)
//	DELETE /component/{entity}/{component}   — remove a component from an entity (200, 404, 503)
//	PUT /toggle/{entity}                     — toggle Disabled tag; ?enabled=true/false/omit-to-flip (200, 400, 404, 503)
//	PUT /toggle/{entity}/{component}         — toggle CanToggle component bit; same ?enabled semantics (200, 400, 404, 503)
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
	// writeMu serializes concurrent w.Write calls; w.Write is not goroutine-safe
	// when called from multiple goroutines simultaneously (pre-existing inMerge race).
	var writeMu sync.Mutex
	mux.HandleFunc("PUT /entity", restPutEntity(w, &writeMu))
	mux.HandleFunc("DELETE /entity/{path...}", restDeleteEntity(w, &writeMu))
	mux.HandleFunc("PUT /component/{entity}/{component}", restPutComponent(w, &writeMu))
	mux.HandleFunc("DELETE /component/{entity}/{component}", restDeleteComponent(w, &writeMu))
	mux.HandleFunc("PUT /toggle/{entity}", restPutToggle(w, &writeMu))
	mux.HandleFunc("PUT /toggle/{entity}/{component}", restPutToggleComponent(w, &writeMu))
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

// putEntityRequest is the JSON body for PUT /entity.
type putEntityRequest struct {
	ID     *uint64 `json:"id"`
	Name   string  `json:"name"`
	Parent string  `json:"parent"`
}

// putEntityResponse is the JSON body returned by PUT /entity.
type putEntityResponse struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
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

func restPutEntity(w *World, writeMu *sync.Mutex) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		var req putEntityRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(rw, http.StatusBadRequest, "malformed JSON body")
			return
		}

		var parentID ID
		if req.Parent != "" {
			var found bool
			w.Read(func(fr *Reader) {
				parentID, found = fr.Lookup(req.Parent)
			})
			if !found {
				writeError(rw, http.StatusNotFound, "parent not found")
				return
			}
		}

		var resp putEntityResponse
		var panicVal any
		writeMu.Lock()
		func() {
			defer func() { panicVal = recover() }()
			w.Write(func(fw *Writer) {
				var e ID
				if req.ID != nil {
					// MakeAlive requires deferDepth==0; w.Write sets deferDepth=1.
					// Temporarily step outside the deferred scope for this call.
					var maErr any
					func() {
						fw.stage.deferDepth--
						defer func() {
							maErr = recover()
							fw.stage.deferDepth++
						}()
						e = MakeAlive(fw, ID(*req.ID))
					}()
					if maErr != nil {
						panic(maErr)
					}
				} else {
					e = fw.NewEntity()
				}
				if req.Name != "" {
					fw.SetName(e, req.Name)
				}
				if parentID != 0 {
					fw.AddID(e, MakePair(w.ChildOf(), parentID))
				}
				resp = putEntityResponse{ID: uint64(e), Name: req.Name}
			})
		}()
		writeMu.Unlock()

		if panicVal != nil {
			if strings.HasPrefix(fmt.Sprint(panicVal), "flecs: MakeAlive:") {
				writeError(rw, http.StatusConflict, "entity alive at different generation")
				return
			}
			writeError(rw, http.StatusServiceUnavailable, "world unavailable")
			return
		}
		writeJSON(rw, http.StatusOK, resp)
	}
}

func restDeleteEntity(w *World, writeMu *sync.Mutex) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		path, _ := url.PathUnescape(r.PathValue("path"))
		if path == "" {
			writeError(rw, http.StatusBadRequest, "path is required")
			return
		}

		var e ID
		var found bool
		w.Read(func(fr *Reader) {
			e, found = fr.Lookup(path)
		})
		if !found {
			writeError(rw, http.StatusNotFound, "entity not found")
			return
		}

		var panicVal any
		writeMu.Lock()
		func() {
			defer func() { panicVal = recover() }()
			w.Write(func(fw *Writer) {
				fw.Delete(e)
			})
		}()
		writeMu.Unlock()

		if panicVal != nil {
			writeError(rw, http.StatusServiceUnavailable, "world unavailable")
			return
		}
		rw.WriteHeader(http.StatusOK)
	}
}

// resolveComponentID parses the component URL segment and returns the component ID.
// The segment may be a plain entity name or a tilde-separated pair "rel~tgt".
// Returns compID, relID, tgtID, isPair, and whether both entity and component resolved.
func resolveComponentPaths(fr *Reader, entityPath, componentPath string) (e ID, compID ID, relID ID, tgtID ID, isPair bool, entityFound bool, compFound bool) {
	e, entityFound = fr.Lookup(entityPath)
	if !entityFound {
		return
	}
	if idx := strings.IndexByte(componentPath, '~'); idx >= 0 {
		rel, relOK := fr.Lookup(componentPath[:idx])
		tgt, tgtOK := fr.Lookup(componentPath[idx+1:])
		if !relOK || !tgtOK {
			return
		}
		isPair = true
		relID, tgtID = rel, tgt
		compID = MakePair(rel, tgt)
		compFound = true
	} else {
		id, ok := fr.Lookup(componentPath)
		if !ok {
			return
		}
		compID = id
		compFound = true
	}
	return
}

func restPutComponent(w *World, writeMu *sync.Mutex) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		entityPath, _ := url.PathUnescape(r.PathValue("entity"))
		componentPath, _ := url.PathUnescape(r.PathValue("component"))

		var e ID
		var entityFound bool
		var compID ID
		var compFound bool
		var isPair bool
		var relID, tgtID ID
		var hasInfo bool
		var compSize uintptr
		var compType reflect.Type

		w.Read(func(fr *Reader) {
			e, compID, relID, tgtID, isPair, entityFound, compFound = resolveComponentPaths(fr, entityPath, componentPath)
			if compFound {
				if ti, ok := w.registry.LookupByID(compID); ok {
					hasInfo = true
					compSize = ti.Size
					compType = ti.Type
				}
			}
		})

		if !entityFound {
			writeError(rw, http.StatusNotFound, "entity not found")
			return
		}
		if !compFound {
			writeError(rw, http.StatusNotFound, "component not found")
			return
		}

		r.Body = http.MaxBytesReader(rw, r.Body, 1<<20)

		var writeBody func(fw *Writer)

		switch {
		case !hasInfo || compSize == 0:
			// Tag: body must be empty.
			body, err := io.ReadAll(r.Body)
			if err != nil {
				if strings.Contains(err.Error(), "http: request body too large") {
					writeError(rw, http.StatusRequestEntityTooLarge, "request body too large")
				} else {
					writeError(rw, http.StatusBadRequest, "failed to read body")
				}
				return
			}
			if len(body) > 0 {
				writeError(rw, http.StatusBadRequest, "tag component must have empty body")
				return
			}
			writeBody = func(fw *Writer) { fw.AddID(e, compID) }

		case compType != nil:
			// Typed data component or pair: decode JSON body.
			ptr := reflect.New(compType)
			if err := json.NewDecoder(r.Body).Decode(ptr.Interface()); err != nil {
				if strings.Contains(err.Error(), "http: request body too large") {
					writeError(rw, http.StatusRequestEntityTooLarge, "request body too large")
				} else {
					writeError(rw, http.StatusBadRequest, "malformed JSON body")
				}
				return
			}
			v := ptr.Elem().Interface()
			if isPair {
				writeBody = func(fw *Writer) { fw.SetPairByID(e, relID, tgtID, v) }
			} else {
				writeBody = func(fw *Writer) { fw.SetByID(e, compID, v) }
			}

		default:
			// Dynamic component: JSON string of base64-encoded bytes.
			body, err := io.ReadAll(r.Body)
			if err != nil {
				if strings.Contains(err.Error(), "http: request body too large") {
					writeError(rw, http.StatusRequestEntityTooLarge, "request body too large")
				} else {
					writeError(rw, http.StatusBadRequest, "failed to read body")
				}
				return
			}
			var b64 string
			if err := json.Unmarshal(body, &b64); err != nil {
				writeError(rw, http.StatusBadRequest, "dynamic component body must be a JSON string (base64)")
				return
			}
			decoded, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				writeError(rw, http.StatusBadRequest, "invalid base64 in body")
				return
			}
			if uintptr(len(decoded)) != compSize {
				writeError(rw, http.StatusBadRequest, fmt.Sprintf("dynamic component size mismatch: got %d bytes, expected %d", len(decoded), compSize))
				return
			}
			writeBody = func(fw *Writer) {
				SetIDPtr(fw, e, compID, unsafe.Pointer(&decoded[0]))
			}
		}

		var panicVal any
		writeMu.Lock()
		func() {
			defer func() { panicVal = recover() }()
			w.Write(writeBody)
		}()
		writeMu.Unlock()

		if panicVal != nil {
			writeError(rw, http.StatusServiceUnavailable, "world unavailable")
			return
		}
		rw.WriteHeader(http.StatusOK)
	}
}

func restDeleteComponent(w *World, writeMu *sync.Mutex) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		entityPath, _ := url.PathUnescape(r.PathValue("entity"))
		componentPath, _ := url.PathUnescape(r.PathValue("component"))

		var e ID
		var entityFound bool
		var compID ID
		var compFound bool

		w.Read(func(fr *Reader) {
			e, compID, _, _, _, entityFound, compFound = resolveComponentPaths(fr, entityPath, componentPath)
		})

		if !entityFound {
			writeError(rw, http.StatusNotFound, "entity not found")
			return
		}
		if !compFound {
			writeError(rw, http.StatusNotFound, "component not found")
			return
		}

		var panicVal any
		writeMu.Lock()
		func() {
			defer func() { panicVal = recover() }()
			w.Write(func(fw *Writer) {
				fw.RemoveID(e, compID)
			})
		}()
		writeMu.Unlock()

		if panicVal != nil {
			writeError(rw, http.StatusServiceUnavailable, "world unavailable")
			return
		}
		rw.WriteHeader(http.StatusOK)
	}
}

// parseEnabledParam parses the ?enabled= query parameter.
// Returns (targetEnabled, flip, ok). If ok is false, the caller should return 400.
// flip is true when the parameter is absent (flip current state).
func parseEnabledParam(r *http.Request) (targetEnabled bool, flip bool, ok bool) {
	raw := r.URL.Query().Get("enabled")
	if raw == "" {
		return false, true, true
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, false, false
	}
	return v, false, true
}

func restPutToggle(w *World, writeMu *sync.Mutex) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		entityPath, _ := url.PathUnescape(r.PathValue("entity"))

		targetEnabled, flip, ok := parseEnabledParam(r)
		if !ok {
			writeError(rw, http.StatusBadRequest, "enabled must be a boolean")
			return
		}

		var e ID
		var entityFound bool
		var currentlyDisabled bool
		w.Read(func(fr *Reader) {
			e, entityFound = fr.Lookup(entityPath)
			if entityFound {
				currentlyDisabled = IsDisabled(fr, e)
			}
		})
		if !entityFound {
			writeError(rw, http.StatusNotFound, "entity not found")
			return
		}

		if flip {
			targetEnabled = currentlyDisabled // disabled → enable; enabled → disable
		}

		var panicVal any
		writeMu.Lock()
		func() {
			defer func() { panicVal = recover() }()
			w.Write(func(fw *Writer) {
				if targetEnabled {
					EnableEntity(fw, e)
				} else {
					DisableEntity(fw, e)
				}
			})
		}()
		writeMu.Unlock()

		if panicVal != nil {
			writeError(rw, http.StatusServiceUnavailable, "world unavailable")
			return
		}
		writeJSON(rw, http.StatusOK, map[string]bool{"enabled": targetEnabled})
	}
}

func restPutToggleComponent(w *World, writeMu *sync.Mutex) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		entityPath, _ := url.PathUnescape(r.PathValue("entity"))
		componentPath, _ := url.PathUnescape(r.PathValue("component"))

		targetEnabled, flip, ok := parseEnabledParam(r)
		if !ok {
			writeError(rw, http.StatusBadRequest, "enabled must be a boolean")
			return
		}

		var e ID
		var compID ID
		var entityFound bool
		var compFound bool
		var canToggle bool
		var currentlyEnabled bool
		w.Read(func(fr *Reader) {
			e, compID, _, _, _, entityFound, compFound = resolveComponentPaths(fr, entityPath, componentPath)
			if compFound {
				canToggle = IsCanToggle(w, compID)
				if canToggle && flip {
					currentlyEnabled = IsEnabledID(fr, e, compID)
				}
			}
		})

		if !entityFound {
			writeError(rw, http.StatusNotFound, "entity not found")
			return
		}
		if !compFound {
			writeError(rw, http.StatusNotFound, "component not found")
			return
		}
		if !canToggle {
			writeError(rw, http.StatusBadRequest, "component is not CanToggle")
			return
		}

		if flip {
			targetEnabled = !currentlyEnabled
		}

		var panicVal any
		writeMu.Lock()
		func() {
			defer func() { panicVal = recover() }()
			w.Write(func(fw *Writer) {
				if targetEnabled {
					EnableID(fw, e, compID)
				} else {
					DisableID(fw, e, compID)
				}
			})
		}()
		writeMu.Unlock()

		if panicVal != nil {
			msg := fmt.Sprint(panicVal)
			if strings.Contains(msg, "entity does not have the component") {
				writeError(rw, http.StatusBadRequest, "entity does not have the component")
			} else {
				writeError(rw, http.StatusServiceUnavailable, "world unavailable")
			}
			return
		}
		writeJSON(rw, http.StatusOK, map[string]bool{"enabled": targetEnabled})
	}
}
