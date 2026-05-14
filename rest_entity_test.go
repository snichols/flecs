package flecs_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/snichols/flecs"
)

func newEntityWorld(t *testing.T) (*flecs.World, *httptest.Server) {
	t.Helper()
	w := flecs.New()
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)
	return w, srv
}

// Test 1: PUT /entity with empty body ({}) → 200, returns a fresh ID.
func TestRESTPutEntityEmpty(t *testing.T) {
	_, srv := newEntityWorld(t)

	resp := restDo(t, srv, "PUT", "/entity", `{}`)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /entity {}: want 200, got %d; body: %s", resp.StatusCode, body)
	}
	var res struct {
		ID   uint64 `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if res.ID == 0 {
		t.Error("expected non-zero id in response")
	}
	if res.Name != "" {
		t.Errorf("name want empty, got %q", res.Name)
	}
}

// Test 2: PUT /entity with name → entity has that name; response echoes the name.
func TestRESTPutEntityWithName(t *testing.T) {
	w, srv := newEntityWorld(t)

	resp := restDo(t, srv, "PUT", "/entity", `{"name":"alpha"}`)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /entity {name}: want 200, got %d; body: %s", resp.StatusCode, body)
	}
	var res struct {
		ID   uint64 `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Name != "alpha" {
		t.Errorf("response name want 'alpha', got %q", res.Name)
	}
	if name, ok := w.GetName(flecs.ID(res.ID)); !ok || name != "alpha" {
		t.Errorf("world name want 'alpha', got %q ok=%v", name, ok)
	}
}

// Test 3: PUT /entity with id → claims that specific ID; GET /entities/{id} returns it.
func TestRESTPutEntityWithID(t *testing.T) {
	w, srv := newEntityWorld(t)

	target := flecs.MakeEntity(5000, 0)
	putBody := fmt.Sprintf(`{"id":%d}`, uint64(target))
	resp := restDo(t, srv, "PUT", "/entity", putBody)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /entity {id}: want 200, got %d; body: %s", resp.StatusCode, body)
	}
	var res struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !w.IsAlive(flecs.ID(res.ID)) {
		t.Error("entity not alive after PUT /entity with id")
	}
	getPath := fmt.Sprintf("/entities/%d", res.ID)
	getResp := restGet(t, srv, getPath)
	readBody(t, getResp)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: want 200, got %d", getPath, getResp.StatusCode)
	}
}

// Test 4: PUT /entity with claim-conflict (alive at different generation) → 409.
func TestRESTPutEntityConflict(t *testing.T) {
	w, srv := newEntityWorld(t)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// Request the same slot at a different generation → conflict.
	conflictID := flecs.MakeEntity(e.Index(), e.Generation()+1)
	putBody := fmt.Sprintf(`{"id":%d}`, uint64(conflictID))
	resp := restDo(t, srv, "PUT", "/entity", putBody)
	readBody(t, resp)

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("PUT /entity conflict: want 409, got %d", resp.StatusCode)
	}
}

// Test 5: PUT /entity with resolvable parent → child has (ChildOf, parent).
func TestRESTPutEntityWithParent(t *testing.T) {
	w, srv := newEntityWorld(t)

	var parent flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		w.SetName(parent, "myparent")
	})

	resp := restDo(t, srv, "PUT", "/entity", `{"name":"mychild","parent":"myparent"}`)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /entity {parent}: want 200, got %d; body: %s", resp.StatusCode, body)
	}
	var res struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	childID := flecs.ID(res.ID)
	p, ok := w.ParentOf(childID)
	if !ok {
		t.Fatal("child has no parent after PUT /entity with parent")
	}
	if p != parent {
		t.Errorf("parent want %d, got %d", uint64(parent), uint64(p))
	}
}

// Test 6: PUT /entity with unresolvable parent → 404.
func TestRESTPutEntityUnresolvableParent(t *testing.T) {
	_, srv := newEntityWorld(t)

	resp := restDo(t, srv, "PUT", "/entity", `{"parent":"nonexistent"}`)
	readBody(t, resp)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("PUT /entity unresolvable parent: want 404, got %d", resp.StatusCode)
	}
}

// Test 7: PUT /entity with malformed JSON body → 400.
func TestRESTPutEntityMalformedJSON(t *testing.T) {
	_, srv := newEntityWorld(t)

	resp := restDo(t, srv, "PUT", "/entity", `{not valid json`)
	readBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT /entity malformed: want 400, got %d", resp.StatusCode)
	}
}

// Test 8: DELETE /entity/{name} → entity removed; GET /entities/{id} → 404.
func TestRESTDeleteEntityByName(t *testing.T) {
	w, srv := newEntityWorld(t)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		w.SetName(e, "todelete")
	})

	resp := restDo(t, srv, "DELETE", "/entity/todelete", "")
	readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /entity/todelete: want 200, got %d", resp.StatusCode)
	}
	if w.IsAlive(e) {
		t.Error("entity still alive after DELETE")
	}
	getPath := fmt.Sprintf("/entities/%d", uint64(e))
	getResp := restGet(t, srv, getPath)
	readBody(t, getResp)
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET %s after delete: want 404, got %d", getPath, getResp.StatusCode)
	}
}

// Test 9: DELETE /entity/nonexistent → 404.
func TestRESTDeleteEntityNonexistent(t *testing.T) {
	_, srv := newEntityWorld(t)

	resp := restDo(t, srv, "DELETE", "/entity/nonexistent", "")
	readBody(t, resp)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("DELETE /entity/nonexistent: want 404, got %d", resp.StatusCode)
	}
}

// Test 10: DELETE /entity/ (trailing slash → empty path) → 400.
func TestRESTDeleteEntityEmptyPath(t *testing.T) {
	_, srv := newEntityWorld(t)

	resp := restDo(t, srv, "DELETE", "/entity/", "")
	readBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("DELETE /entity/: want 400, got %d", resp.StatusCode)
	}
}

// Test 11: Concurrent PUT + DELETE goroutines — race-detector clean.
func TestRESTPutDeleteEntityConcurrent(t *testing.T) {
	_, srv := newEntityWorld(t)

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("cent%d", i)
			putResp := restDo(t, srv, "PUT", "/entity", fmt.Sprintf(`{"name":%q}`, name))
			readBody(t, putResp)
			if putResp.StatusCode != http.StatusOK {
				t.Errorf("concurrent PUT /entity: want 200, got %d", putResp.StatusCode)
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("cent%d", i)
			delResp := restDo(t, srv, "DELETE", "/entity/"+name, "")
			readBody(t, delResp)
			sc := delResp.StatusCode
			if sc != http.StatusOK && sc != http.StatusNotFound {
				t.Errorf("concurrent DELETE /entity/%s: want 200 or 404, got %d", name, sc)
			}
		}(i)
	}
	wg.Wait()
}

// Test 12: PUT response Content-Type is application/json.
func TestRESTPutEntityResponseContentType(t *testing.T) {
	_, srv := newEntityWorld(t)

	resp := restDo(t, srv, "PUT", "/entity", `{}`)
	readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /entity: want 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type want application/json, got %q", ct)
	}
}

// Test 13: PUT /entity when a post-merge hook panics → 503.
func TestRESTPutEntityWorldUnavailable(t *testing.T) {
	w := flecs.New()
	flecs.OnPostMerge(w, func(_ *flecs.Writer) {
		panic("simulated world unavailable")
	})
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	// no t.Cleanup(srv.Close) — world is broken after the panic; just let it GC

	resp := restDo(t, srv, "PUT", "/entity", `{}`)
	readBody(t, resp)

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("PUT /entity world unavailable: want 503, got %d", resp.StatusCode)
	}
}

// Test 14: DELETE /entity/{name} when a post-merge hook panics → 503.
func TestRESTDeleteEntityWorldUnavailable(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	w.SetName(e, "doomed")

	flecs.OnPostMerge(w, func(_ *flecs.Writer) {
		panic("simulated world unavailable")
	})
	srv := httptest.NewServer(flecs.NewRESTHandler(w))

	resp := restDo(t, srv, "DELETE", "/entity/doomed", "")
	readBody(t, resp)

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("DELETE /entity/doomed world unavailable: want 503, got %d", resp.StatusCode)
	}
}
