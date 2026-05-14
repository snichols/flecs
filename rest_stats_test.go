package flecs_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/snichols/flecs"
)

// statsSetup builds a world that has run one Progress tick, wrapped in an httptest.Server.
func statsSetup(t *testing.T) (*flecs.World, *httptest.Server) {
	t.Helper()
	w := flecs.New()
	w.Progress(0.016)
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)
	return w, srv
}

func TestRESTStatsWorldOK(t *testing.T) {
	_, srv := statsSetup(t)

	resp := restGet(t, srv, "/stats/world")
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /stats/world: want 200, got %d; body: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("GET /stats/world: Content-Type want application/json, got %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("GET /stats/world: Cache-Control want no-store, got %q", cc)
	}
}

func TestRESTStatsWorldShape(t *testing.T) {
	_, srv := statsSetup(t)

	resp := restGet(t, srv, "/stats/world")
	body := readBody(t, resp)

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("GET /stats/world: body is not valid JSON object: %v", err)
	}
	if _, ok := envelope["world"]; !ok {
		t.Fatalf("GET /stats/world: missing top-level 'world' key; got keys: %v", keysOf(envelope))
	}

	var ws map[string]json.RawMessage
	if err := json.Unmarshal(envelope["world"], &ws); err != nil {
		t.Fatalf("GET /stats/world: 'world' value is not a JSON object: %v", err)
	}
	for _, field := range []string{"entity_count", "table_count", "archetype_count", "frame_count", "total_time", "last_tick_delta"} {
		if _, ok := ws[field]; !ok {
			t.Errorf("GET /stats/world: missing field %q in world object; got: %v", field, keysOf(ws))
		}
	}
}

func TestRESTStatsPipelineOK(t *testing.T) {
	_, srv := statsSetup(t)

	resp := restGet(t, srv, "/stats/pipeline")
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /stats/pipeline: want 200, got %d; body: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("GET /stats/pipeline: Content-Type want application/json, got %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("GET /stats/pipeline: Cache-Control want no-store, got %q", cc)
	}
}

func TestRESTStatsPipelineShape(t *testing.T) {
	_, srv := statsSetup(t)

	resp := restGet(t, srv, "/stats/pipeline")
	body := readBody(t, resp)

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("GET /stats/pipeline: body is not valid JSON: %v", err)
	}
	for _, key := range []string{"world", "systems", "phases"} {
		if _, ok := envelope[key]; !ok {
			t.Errorf("GET /stats/pipeline: missing top-level key %q; got: %v", key, keysOf(envelope))
		}
	}

	var ws map[string]json.RawMessage
	if err := json.Unmarshal(envelope["world"], &ws); err != nil {
		t.Fatalf("GET /stats/pipeline: 'world' value is not a JSON object: %v", err)
	}
	for _, field := range []string{"entity_count", "table_count", "frame_count", "total_time", "last_tick_delta"} {
		if _, ok := ws[field]; !ok {
			t.Errorf("GET /stats/pipeline: world missing field %q; got: %v", field, keysOf(ws))
		}
	}
}

func TestRESTStatsAfterProgress(t *testing.T) {
	w := flecs.New()
	w.Progress(0.016)
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	// /stats/world should show frame_count == 1 after one Progress.
	resp := restGet(t, srv, "/stats/world")
	body := readBody(t, resp)

	var envelope struct {
		World struct {
			FrameCount    uint64  `json:"frame_count"`
			LastTickDelta float64 `json:"last_tick_delta"`
		} `json:"world"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("GET /stats/world: unmarshal error: %v", err)
	}
	if envelope.World.FrameCount != 1 {
		t.Errorf("GET /stats/world: frame_count want 1, got %d", envelope.World.FrameCount)
	}
	if envelope.World.LastTickDelta == 0 {
		t.Errorf("GET /stats/world: last_tick_delta want non-zero after Progress(0.016)")
	}
}

func TestRESTStatsPipelineAfterProgress(t *testing.T) {
	w := flecs.New()
	w.Progress(0.016)
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := restGet(t, srv, "/stats/pipeline")
	body := readBody(t, resp)

	var pipeline struct {
		World struct {
			FrameCount uint64 `json:"frame_count"`
		} `json:"world"`
		Phases []struct {
			Name        string `json:"name"`
			Invocations uint64 `json:"invocations"`
		} `json:"phases"`
	}
	if err := json.Unmarshal(body, &pipeline); err != nil {
		t.Fatalf("GET /stats/pipeline: unmarshal error: %v", err)
	}
	if pipeline.World.FrameCount != 1 {
		t.Errorf("GET /stats/pipeline: world.frame_count want 1, got %d", pipeline.World.FrameCount)
	}
	if len(pipeline.Phases) == 0 {
		t.Errorf("GET /stats/pipeline: phases should be non-empty after Progress")
	}
}

func TestRESTStatsWorldPanic503(t *testing.T) {
	// A nil *World causes StatsSnapshot to panic; the handler must recover and return 503.
	srv := httptest.NewServer(flecs.NewRESTHandler(nil))
	t.Cleanup(srv.Close)

	resp := restGet(t, srv, "/stats/world")
	readBody(t, resp)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("GET /stats/world nil world: want 503, got %d", resp.StatusCode)
	}
}

func TestRESTStatsPipelinePanic503(t *testing.T) {
	// A nil *World causes StatsSnapshot to panic; the handler must recover and return 503.
	srv := httptest.NewServer(flecs.NewRESTHandler(nil))
	t.Cleanup(srv.Close)

	resp := restGet(t, srv, "/stats/pipeline")
	readBody(t, resp)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("GET /stats/pipeline nil world: want 503, got %d", resp.StatusCode)
	}
}

type statsPos struct{ X, Y float32 }

func TestRESTStatsPipelineWithSystem(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[statsPos](w)
	q := flecs.NewCachedQuery(w, posID)
	sys := flecs.NewSystem(w, q, func(_ float32, _ *flecs.QueryIter) {})
	sys.SetName("test-system")
	w.Progress(0.016)

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := restGet(t, srv, "/stats/pipeline")
	body := readBody(t, resp)

	var pipeline struct {
		Systems []struct {
			Name        string `json:"name"`
			Invocations uint64 `json:"invocations"`
		} `json:"systems"`
	}
	if err := json.Unmarshal(body, &pipeline); err != nil {
		t.Fatalf("GET /stats/pipeline: unmarshal error: %v", err)
	}
	if len(pipeline.Systems) == 0 {
		t.Fatalf("GET /stats/pipeline: expected non-empty systems after registering a system")
	}
	found := false
	for _, s := range pipeline.Systems {
		if s.Name == "test-system" {
			found = true
			if s.Invocations == 0 {
				t.Errorf("GET /stats/pipeline: test-system invocations want >0 after Progress")
			}
		}
	}
	if !found {
		t.Errorf("GET /stats/pipeline: 'test-system' not found in systems list")
	}
}

func TestRESTStatsConcurrent(t *testing.T) {
	_, srv := statsSetup(t)

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)
	for range goroutines {
		go func() {
			defer wg.Done()
			resp := restGet(t, srv, "/stats/world")
			readBody(t, resp)
			if resp.StatusCode != http.StatusOK {
				t.Errorf("concurrent GET /stats/world: want 200, got %d", resp.StatusCode)
			}
		}()
		go func() {
			defer wg.Done()
			resp := restGet(t, srv, "/stats/pipeline")
			readBody(t, resp)
			if resp.StatusCode != http.StatusOK {
				t.Errorf("concurrent GET /stats/pipeline: want 200, got %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()
}

// keysOf returns the keys of a map[string]json.RawMessage as a slice for error messages.
func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
