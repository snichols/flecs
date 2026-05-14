package flecs_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/snichols/flecs"
)

// restPosition and restVelocity are local test types to avoid name collisions.
type restPosition struct{ X, Y float32 }
type restVelocity struct{ DX, DY float32 }

// restSetup builds a small world and wraps it in an httptest.Server.
// The world has:
//   - Two components: restPosition, restVelocity
//   - A named parent entity "parent"
//   - A named child entity "child" with ChildOf(parent), Position{1,2}, Velocity{3,4}
func restSetup(t *testing.T) (*flecs.World, *httptest.Server) {
	t.Helper()
	w := flecs.New()
	flecs.RegisterComponent[restPosition](w)
	flecs.RegisterComponent[restVelocity](w)

	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		w.SetName(parent, "parent")

		child = fw.NewEntity()
		w.SetName(child, "child")
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
		flecs.Set(fw, child, restPosition{X: 1, Y: 2})
		flecs.Set(fw, child, restVelocity{DX: 3, DY: 4})
	})
	_, _ = parent, child

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)
	return w, srv
}

func restGet(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func restDo(t *testing.T, srv *httptest.Server, method, path, body string) *http.Response {
	t.Helper()
	var br io.Reader
	if body != "" {
		br = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, br)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return b
}

func TestRESTStats(t *testing.T) {
	w, srv := restSetup(t)
	resp := restGet(t, srv, "/stats")
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /stats: want 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("GET /stats: Content-Type want application/json, got %q", ct)
	}
	var s flecs.Stats
	if err := json.Unmarshal(body, &s); err != nil {
		t.Fatalf("GET /stats: body is not valid Stats JSON: %v", err)
	}
	if s.EntityCount != w.Stats().EntityCount {
		t.Fatalf("GET /stats: EntityCount want %d, got %d", w.Stats().EntityCount, s.EntityCount)
	}
}

func TestRESTComponents(t *testing.T) {
	_, srv := restSetup(t)
	resp := restGet(t, srv, "/components")
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /components: want 200, got %d", resp.StatusCode)
	}
	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("GET /components: invalid JSON: %v", err)
	}
	names := map[string]bool{}
	for _, item := range items {
		if n, ok := item["name"].(string); ok {
			names[n] = true
		}
	}
	for _, want := range []string{"flecs_test.restPosition", "flecs_test.restVelocity"} {
		if !names[want] {
			t.Errorf("GET /components: missing %q; got names %v", want, names)
		}
	}
}

func TestRESTComponentByID(t *testing.T) {
	w, srv := restSetup(t)

	// 200 for registered ID.
	ids := w.Components()
	if len(ids) == 0 {
		t.Fatal("no components registered")
	}
	path := fmt.Sprintf("/components/%d", uint64(ids[0]))
	resp := restGet(t, srv, path)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: want 200, got %d; body: %s", path, resp.StatusCode, body)
	}
	var item map[string]any
	if err := json.Unmarshal(body, &item); err != nil {
		t.Fatalf("GET %s: invalid JSON: %v", path, err)
	}
	if item["id"] == nil {
		t.Errorf("GET %s: missing 'id' field", path)
	}

	// 404 for unregistered ID (use a large number unlikely to be allocated).
	resp404 := restGet(t, srv, "/components/99999999")
	readBody(t, resp404)
	if resp404.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /components/99999999: want 404, got %d", resp404.StatusCode)
	}
}

func TestRESTEntities(t *testing.T) {
	_, srv := restSetup(t)

	// Default limit: 200, array returned.
	resp := restGet(t, srv, "/entities")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /entities: want 200, got %d", resp.StatusCode)
	}
	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("GET /entities: invalid JSON: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("GET /entities: expected non-empty array")
	}

	// ?limit=2: returns exactly 2 entries.
	resp2 := restGet(t, srv, "/entities?limit=2")
	body2 := readBody(t, resp2)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("GET /entities?limit=2: want 200, got %d", resp2.StatusCode)
	}
	var items2 []map[string]any
	if err := json.Unmarshal(body2, &items2); err != nil {
		t.Fatalf("GET /entities?limit=2: invalid JSON: %v", err)
	}
	if len(items2) != 2 {
		t.Fatalf("GET /entities?limit=2: want 2, got %d", len(items2))
	}

	// Malformed limit → 400.
	respBad := restGet(t, srv, "/entities?limit=notanumber")
	readBody(t, respBad)
	if respBad.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET /entities?limit=notanumber: want 400, got %d", respBad.StatusCode)
	}

	// Out-of-range limit → 400.
	respOOB := restGet(t, srv, "/entities?limit=99999")
	readBody(t, respOOB)
	if respOOB.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET /entities?limit=99999: want 400, got %d", respOOB.StatusCode)
	}
}

func TestRESTEntityByID(t *testing.T) {
	w, srv := restSetup(t)

	// Find the "child" entity by iterating.
	var childID flecs.ID
	w.EachEntity(func(e flecs.ID) bool {
		if name, ok := w.GetName(e); ok && name == "child" {
			childID = e
			return false
		}
		return true
	})
	if childID == 0 {
		t.Fatal("child entity not found")
	}

	// 200 for alive entity; verify name and parent fields.
	path := fmt.Sprintf("/entities/%d", uint64(childID))
	resp := restGet(t, srv, path)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: want 200, got %d; body: %s", path, resp.StatusCode, body)
	}
	var detail map[string]any
	if err := json.Unmarshal(body, &detail); err != nil {
		t.Fatalf("GET %s: invalid JSON: %v", path, err)
	}
	if detail["name"] != "child" {
		t.Errorf("GET %s: name want 'child', got %v", path, detail["name"])
	}
	if detail["parent"] == nil || detail["parent"] == "" {
		t.Errorf("GET %s: expected non-empty 'parent' field, got %v", path, detail["parent"])
	}
	comps, ok := detail["components"].([]any)
	if !ok {
		t.Fatalf("GET %s: 'components' field missing or wrong type: %T", path, detail["components"])
	}
	if len(comps) == 0 {
		t.Errorf("GET %s: expected non-empty 'components'", path)
	}

	// 404 for dead entity: delete the child then query it.
	w.Delete(childID)
	resp404 := restGet(t, srv, path)
	readBody(t, resp404)
	if resp404.StatusCode != http.StatusNotFound {
		t.Fatalf("GET %s (dead): want 404, got %d", path, resp404.StatusCode)
	}
}

func TestRESTSnapshot(t *testing.T) {
	w, srv := restSetup(t)

	// GET /snapshot: 200, valid JSON.
	resp := restGet(t, srv, "/snapshot")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /snapshot: want 200, got %d", resp.StatusCode)
	}
	if !json.Valid(body) {
		t.Fatalf("GET /snapshot: body is not valid JSON")
	}

	// Round-trip: unmarshal into a fresh world.
	w2 := flecs.New()
	flecs.RegisterComponent[restPosition](w2)
	flecs.RegisterComponent[restVelocity](w2)
	if err := w2.UnmarshalJSON(body); err != nil {
		t.Fatalf("round-trip UnmarshalJSON: %v", err)
	}
	// The fresh world should have at least as many user entities as the original.
	if w2.Count() < w.Count() {
		t.Errorf("round-trip: fresh world entity count %d < original %d", w2.Count(), w.Count())
	}
}

func TestRESTPutSnapshot(t *testing.T) {
	// Build a source world to get a snapshot from.
	wsrc := flecs.New()
	flecs.RegisterComponent[restPosition](wsrc)
	flecs.RegisterComponent[restVelocity](wsrc)
	wsrc.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		wsrc.SetName(e, "from-snapshot")
		flecs.Set(fw, e, restPosition{X: 9, Y: 8})
	})
	snapshot, err := wsrc.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	// Create the target world and server.
	wdst := flecs.New()
	flecs.RegisterComponent[restPosition](wdst)
	flecs.RegisterComponent[restVelocity](wdst)
	srv := httptest.NewServer(flecs.NewRESTHandler(wdst))
	t.Cleanup(srv.Close)

	// PUT /snapshot with valid snapshot → 204.
	resp := restDo(t, srv, http.MethodPut, "/snapshot", string(snapshot))
	readBody(t, resp)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT /snapshot: want 204, got %d", resp.StatusCode)
	}
	// Verify world received the data.
	if wdst.Count() == 0 {
		t.Error("PUT /snapshot: destination world still empty after PUT")
	}

	// PUT /snapshot with malformed JSON → 400.
	resp400 := restDo(t, srv, http.MethodPut, "/snapshot", `{not valid json`)
	readBody(t, resp400)
	if resp400.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT /snapshot malformed: want 400, got %d", resp400.StatusCode)
	}

	// PUT /snapshot with valid JSON but unknown version → 400 (UnmarshalJSON error).
	resp400v := restDo(t, srv, http.MethodPut, "/snapshot", `{"version":99,"entities":[]}`)
	readBody(t, resp400v)
	if resp400v.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT /snapshot bad version: want 400, got %d", resp400v.StatusCode)
	}
}

func TestRESTMethodNotAllowed(t *testing.T) {
	_, srv := restSetup(t)

	resp := restDo(t, srv, http.MethodPost, "/stats", "")
	readBody(t, resp)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /stats: want 405, got %d", resp.StatusCode)
	}
}

func TestRESTUnknownRoute(t *testing.T) {
	_, srv := restSetup(t)

	resp := restGet(t, srv, "/unknown")
	readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /unknown: want 404, got %d", resp.StatusCode)
	}
}

func TestRESTInvalidIDs(t *testing.T) {
	_, srv := restSetup(t)

	// /components/{id} with non-numeric id → 400.
	respC := restGet(t, srv, "/components/notanumber")
	readBody(t, respC)
	if respC.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET /components/notanumber: want 400, got %d", respC.StatusCode)
	}

	// /entities/{id} with non-numeric id → 400.
	respE := restGet(t, srv, "/entities/notanumber")
	readBody(t, respE)
	if respE.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET /entities/notanumber: want 400, got %d", respE.StatusCode)
	}
}

func TestRESTEntityWithCustomPair(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[restPosition](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel := fw.NewEntity()
		tgt := fw.NewEntity()
		e = fw.NewEntity()
		w.SetName(e, "paired")
		flecs.AddID(fw, e, flecs.MakePair(rel, tgt))
		flecs.Set(fw, e, restPosition{X: 5, Y: 6})
	})

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	path := fmt.Sprintf("/entities/%d", uint64(e))
	resp := restGet(t, srv, path)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: want 200, got %d; body: %s", path, resp.StatusCode, body)
	}
	var detail map[string]any
	if err := json.Unmarshal(body, &detail); err != nil {
		t.Fatalf("GET %s: invalid JSON: %v", path, err)
	}
	pairs, _ := detail["pairs"].([]any)
	if len(pairs) == 0 {
		t.Errorf("GET %s: expected non-empty 'pairs' field for custom pair, got %v", path, detail)
	}
}

func TestRESTEntityWithPrefab(t *testing.T) {
	w := flecs.New()
	var prefab, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		e = fw.NewEntity()
		w.SetName(e, "instance")
		flecs.AddID(fw, e, flecs.MakePair(w.IsA(), prefab))
	})

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	path := fmt.Sprintf("/entities/%d", uint64(e))
	resp := restGet(t, srv, path)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: want 200, got %d; body: %s", path, resp.StatusCode, body)
	}
	var detail map[string]any
	if err := json.Unmarshal(body, &detail); err != nil {
		t.Fatalf("GET %s: invalid JSON: %v", path, err)
	}
	prefabs, _ := detail["prefabs"].([]any)
	if len(prefabs) == 0 {
		t.Errorf("GET %s: expected non-empty 'prefabs' for IsA entity; detail: %v", path, detail)
	}
}

func TestRESTEntityWithEntityTag(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tag := fw.NewEntity()
		e = fw.NewEntity()
		w.SetName(e, "tagged")
		flecs.AddID(fw, e, tag)
	})

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	path := fmt.Sprintf("/entities/%d", uint64(e))
	resp := restGet(t, srv, path)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: want 200, got %d; body: %s", path, resp.StatusCode, body)
	}
	var detail map[string]any
	if err := json.Unmarshal(body, &detail); err != nil {
		t.Fatalf("GET %s: invalid JSON: %v", path, err)
	}
	// bare entity tag has no ComponentInfo and is not a pair — not in 'components' or 'pairs'
	if _, ok := detail["components"]; !ok {
		t.Errorf("GET %s: 'components' field missing from response", path)
	}
}

func TestRESTConcurrentReads(t *testing.T) {
	_, srv := restSetup(t)

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			resp := restGet(t, srv, "/stats")
			readBody(t, resp)
			if resp.StatusCode != http.StatusOK {
				t.Errorf("concurrent GET /stats: want 200, got %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()
}
