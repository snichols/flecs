package docs_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/snichols/flecs"
)

type restDocPos struct{ X, Y float32 }
type restDocVel struct{ DX, DY float32 }

// restDocSetup creates a world with two components and one named entity, then
// wraps it in an httptest.Server backed by NewRESTHandler.
func restDocSetup(t *testing.T) (*flecs.World, *httptest.Server) {
	t.Helper()
	w := flecs.New()
	flecs.RegisterComponent[restDocPos](w)
	flecs.RegisterComponent[restDocVel](w)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		w.SetName(e, "hero")
		flecs.Set(fw, e, restDocPos{X: 1, Y: 2})
		flecs.Set(fw, e, restDocVel{DX: 0.5, DY: 0})
	})
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)
	return w, srv
}

// TestRest_BasicSetup verifies that NewRESTHandler mounts cleanly and responds
// to GET /stats with 200 OK.
func TestRest_BasicSetup(t *testing.T) {
	_, srv := restDocSetup(t)
	resp, err := srv.Client().Get(srv.URL + "/stats")
	if err != nil {
		t.Fatalf("GET /stats: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /stats: want 200, got %d", resp.StatusCode)
	}
}

// TestRest_Stats verifies that GET /stats returns JSON that decodes into
// flecs.Stats with a non-zero EntityCount.
func TestRest_Stats(t *testing.T) {
	_, srv := restDocSetup(t)
	resp, err := srv.Client().Get(srv.URL + "/stats")
	if err != nil {
		t.Fatalf("GET /stats: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /stats: want 200, got %d", resp.StatusCode)
	}
	var s flecs.Stats
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("decode flecs.Stats: %v", err)
	}
	if s.EntityCount == 0 {
		t.Error("Stats.EntityCount should be > 0")
	}
}

// TestRest_Components verifies that GET /components returns an array of objects
// each with id, name, size, align, and type fields.
func TestRest_Components(t *testing.T) {
	_, srv := restDocSetup(t)
	resp, err := srv.Client().Get(srv.URL + "/components")
	if err != nil {
		t.Fatalf("GET /components: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /components: want 200, got %d", resp.StatusCode)
	}
	var items []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("decode components: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected non-empty component list")
	}
	for _, f := range []string{"id", "name", "size", "align", "type"} {
		if items[0][f] == nil {
			t.Errorf("component item missing field %q", f)
		}
	}
	// Registered type names must appear.
	names := map[string]bool{}
	for _, item := range items {
		if n, ok := item["name"].(string); ok {
			names[n] = true
		}
	}
	for _, want := range []string{"docs_test.restDocPos", "docs_test.restDocVel"} {
		if !names[want] {
			t.Errorf("GET /components: missing %q; names: %v", want, names)
		}
	}
}

// TestRest_ComponentByID verifies that GET /components/{id} returns 200 for a
// registered component ID and 404 for an unknown ID.
func TestRest_ComponentByID(t *testing.T) {
	w, srv := restDocSetup(t)
	ids := w.Components()
	if len(ids) == 0 {
		t.Fatal("no components registered")
	}
	url200 := fmt.Sprintf("%s/components/%d", srv.URL, uint64(ids[0]))
	resp, err := srv.Client().Get(url200)
	if err != nil {
		t.Fatalf("GET /components/{id}: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /components/{id}: want 200, got %d; body: %s", resp.StatusCode, body)
	}
	var comp map[string]any
	if err := json.Unmarshal(body, &comp); err != nil {
		t.Fatalf("decode component: %v", err)
	}
	if comp["id"] == nil {
		t.Error("component response missing 'id' field")
	}

	// Unknown ID → 404.
	resp404, _ := srv.Client().Get(srv.URL + "/components/9999999")
	resp404.Body.Close()
	if resp404.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown component: want 404, got %d", resp404.StatusCode)
	}
}

// TestRest_Entities verifies GET /entities default response and ?limit=N.
func TestRest_Entities(t *testing.T) {
	_, srv := restDocSetup(t)

	// Default response: non-empty array; each item has "id".
	resp, err := srv.Client().Get(srv.URL + "/entities")
	if err != nil {
		t.Fatalf("GET /entities: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /entities: want 200, got %d", resp.StatusCode)
	}
	var entities []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&entities); err != nil {
		t.Fatalf("decode entities: %v", err)
	}
	if len(entities) == 0 {
		t.Fatal("expected non-empty entity list")
	}
	for i, e := range entities {
		if e["id"] == nil {
			t.Errorf("entities[%d]: missing 'id' field", i)
		}
	}

	// ?limit=1 returns exactly one entity.
	resp1, err := srv.Client().Get(srv.URL + "/entities?limit=1")
	if err != nil {
		t.Fatalf("GET /entities?limit=1: %v", err)
	}
	defer resp1.Body.Close()
	var one []map[string]any
	if err := json.NewDecoder(resp1.Body).Decode(&one); err != nil {
		t.Fatalf("decode /entities?limit=1: %v", err)
	}
	if len(one) != 1 {
		t.Fatalf("?limit=1: want 1 entity, got %d", len(one))
	}
}

// TestRest_EntityByID verifies that GET /entities/{id} returns entity detail
// for a live entity and 404 after the entity is deleted.
func TestRest_EntityByID(t *testing.T) {
	w, srv := restDocSetup(t)

	// Find the "hero" entity.
	var heroID flecs.ID
	w.EachEntity(func(e flecs.ID) bool {
		if n, ok := w.GetName(e); ok && n == "hero" {
			heroID = e
			return false
		}
		return true
	})
	if heroID == 0 {
		t.Fatal("hero entity not found")
	}

	url := fmt.Sprintf("%s/entities/%d", srv.URL, uint64(heroID))
	resp, err := srv.Client().Get(url)
	if err != nil {
		t.Fatalf("GET /entities/{id}: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /entities/{id}: want 200, got %d; body: %s", resp.StatusCode, body)
	}
	var detail map[string]any
	if err := json.Unmarshal(body, &detail); err != nil {
		t.Fatalf("decode entity detail: %v", err)
	}
	if detail["name"] != "hero" {
		t.Errorf("entity name: want 'hero', got %v", detail["name"])
	}
	if detail["components"] == nil {
		t.Error("entity detail missing 'components' field")
	}

	// Delete entity → 404.
	w.Delete(heroID)
	resp404, err := srv.Client().Get(url)
	if err != nil {
		t.Fatalf("GET /entities/{id} after delete: %v", err)
	}
	resp404.Body.Close()
	if resp404.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted entity: want 404, got %d", resp404.StatusCode)
	}
}

// TestRest_Snapshot verifies the GET /snapshot + PUT /snapshot round-trip.
// GET returns valid JSON; PUT with that JSON returns 204.
func TestRest_Snapshot(t *testing.T) {
	_, srv := restDocSetup(t)

	// GET /snapshot → 200 + valid JSON body.
	resp, err := srv.Client().Get(srv.URL + "/snapshot")
	if err != nil {
		t.Fatalf("GET /snapshot: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /snapshot: want 200, got %d", resp.StatusCode)
	}
	if !json.Valid(body) {
		t.Fatal("GET /snapshot: body is not valid JSON")
	}

	// PUT /snapshot with the snapshot → 204.
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/snapshot", strings.NewReader(string(body)))
	putResp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("PUT /snapshot: %v", err)
	}
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT /snapshot: want 204, got %d", putResp.StatusCode)
	}
}

// TestRest_CustomServeMux demonstrates mounting NewRESTHandler under a path
// prefix using http.StripPrefix with a custom ServeMux.
func TestRest_CustomServeMux(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[restDocPos](w)

	mux := http.NewServeMux()
	mux.Handle("/flecs/", http.StripPrefix("/flecs", flecs.NewRESTHandler(w)))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/flecs/stats")
	if err != nil {
		t.Fatalf("GET /flecs/stats: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /flecs/stats: want 200, got %d", resp.StatusCode)
	}
}
