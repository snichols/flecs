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

// rtPos is the typed component used in toggle endpoint tests.
type rtPos struct{ X, Y float32 }

// newToggleWorld creates a world with:
//   - a named entity "Foo"
//   - a "Pos" component registered with CanToggle
//   - a "Vel" component NOT registered with CanToggle
func newToggleWorld(t *testing.T) (*flecs.World, flecs.ID, *httptest.Server) {
	t.Helper()
	w := flecs.New()

	posID := flecs.RegisterComponent[rtPos](w)
	w.SetName(posID, "Pos")
	flecs.SetCanToggle(w, posID)

	type rtVel struct{ DX, DY float32 }
	velID := flecs.RegisterComponent[rtVel](w)
	w.SetName(velID, "Vel")

	var foo flecs.ID
	w.Write(func(fw *flecs.Writer) {
		foo = fw.NewEntity()
		w.SetName(foo, "Foo")
		flecs.Set(fw, foo, rtPos{X: 1, Y: 2})
	})
	_ = velID

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)
	return w, foo, srv
}

// decodeToggleBody decodes the {"enabled": <bool>} response body.
func decodeToggleBody(t *testing.T, body []byte) bool {
	t.Helper()
	var resp struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decodeToggleBody: %v; raw: %s", err, body)
	}
	return resp.Enabled
}

// Test 1: PUT /toggle/Foo?enabled=false → 200, entity is disabled.
func TestRESTToggle_DisableEntity(t *testing.T) {
	w, foo, srv := newToggleWorld(t)

	resp := restDo(t, srv, "PUT", "/toggle/Foo?enabled=false", "")
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", resp.StatusCode, body)
	}
	if got := decodeToggleBody(t, body); got != false {
		t.Errorf("response enabled want false, got %v", got)
	}

	var disabled bool
	w.Read(func(fr *flecs.Reader) { disabled = flecs.IsDisabled(fr, foo) })
	if !disabled {
		t.Error("entity should be disabled after PUT ?enabled=false")
	}
}

// Test 2: PUT /toggle/Foo?enabled=true re-enables a disabled entity.
func TestRESTToggle_EnableEntity(t *testing.T) {
	w, foo, srv := newToggleWorld(t)

	// First disable the entity.
	w.Write(func(fw *flecs.Writer) { flecs.DisableEntity(fw, foo) })

	resp := restDo(t, srv, "PUT", "/toggle/Foo?enabled=true", "")
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", resp.StatusCode, body)
	}
	if got := decodeToggleBody(t, body); got != true {
		t.Errorf("response enabled want true, got %v", got)
	}

	var disabled bool
	w.Read(func(fr *flecs.Reader) { disabled = flecs.IsDisabled(fr, foo) })
	if disabled {
		t.Error("entity should be enabled after PUT ?enabled=true")
	}
}

// Test 3: PUT /toggle/Foo (no param) flips current state.
func TestRESTToggle_FlipState(t *testing.T) {
	w, foo, srv := newToggleWorld(t)

	// Entity starts enabled; flip should disable it.
	resp := restDo(t, srv, "PUT", "/toggle/Foo", "")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("flip to disabled: want 200, got %d; body: %s", resp.StatusCode, body)
	}
	if got := decodeToggleBody(t, body); got != false {
		t.Errorf("flip to disabled: response enabled want false, got %v", got)
	}
	var disabled bool
	w.Read(func(fr *flecs.Reader) { disabled = flecs.IsDisabled(fr, foo) })
	if !disabled {
		t.Error("entity should be disabled after first flip")
	}

	// Flip again; should re-enable.
	resp2 := restDo(t, srv, "PUT", "/toggle/Foo", "")
	body2 := readBody(t, resp2)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("flip to enabled: want 200, got %d; body: %s", resp2.StatusCode, body2)
	}
	if got := decodeToggleBody(t, body2); got != true {
		t.Errorf("flip to enabled: response enabled want true, got %v", got)
	}
	w.Read(func(fr *flecs.Reader) { disabled = flecs.IsDisabled(fr, foo) })
	if disabled {
		t.Error("entity should be enabled after second flip")
	}
}

// Test 4: PUT /toggle/NoSuchEntity → 404.
func TestRESTToggle_EntityNotFound(t *testing.T) {
	_, _, srv := newToggleWorld(t)

	resp := restDo(t, srv, "PUT", "/toggle/NoSuchEntity", "")
	readBody(t, resp)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

// Test 5: PUT /toggle/Foo?enabled=bogus → 400.
func TestRESTToggle_BadEnabledParam(t *testing.T) {
	_, _, srv := newToggleWorld(t)

	resp := restDo(t, srv, "PUT", "/toggle/Foo?enabled=bogus", "")
	readBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

// Test 6: PUT /toggle/Foo/Pos?enabled=false where Pos is CanToggle → 200.
func TestRESTToggle_ComponentCanToggle(t *testing.T) {
	w, foo, srv := newToggleWorld(t)

	resp := restDo(t, srv, "PUT", "/toggle/Foo/Pos?enabled=false", "")
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", resp.StatusCode, body)
	}
	if got := decodeToggleBody(t, body); got != false {
		t.Errorf("response enabled want false, got %v", got)
	}

	posID, _ := w.Lookup("Pos")
	var enabled bool
	w.Read(func(fr *flecs.Reader) { enabled = flecs.IsEnabledID(fr, foo, posID) })
	if enabled {
		t.Error("Pos should be disabled on Foo after PUT ?enabled=false")
	}
}

// Test 7: PUT /toggle/Foo/Vel?enabled=false where Vel is NOT CanToggle → 400.
func TestRESTToggle_ComponentNotCanToggle(t *testing.T) {
	_, _, srv := newToggleWorld(t)

	resp := restDo(t, srv, "PUT", "/toggle/Foo/Vel?enabled=false", "")
	readBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

// Test 8: concurrent PUTs are race-clean.
func TestRESTToggle_Concurrent(t *testing.T) {
	_, _, srv := newToggleWorld(t)

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			r := restDo(t, srv, "PUT", fmt.Sprintf("/toggle/Foo?enabled=%v", i%2 == 0), "")
			readBody(t, r)
			sc := r.StatusCode
			if sc != http.StatusOK {
				t.Errorf("concurrent toggle: want 200, got %d", sc)
			}
		}(i)
		go func() {
			defer wg.Done()
			r := restDo(t, srv, "PUT", "/toggle/Foo", "")
			readBody(t, r)
			sc := r.StatusCode
			if sc != http.StatusOK {
				t.Errorf("concurrent flip: want 200, got %d", sc)
			}
		}()
	}
	wg.Wait()
}

// Test 9: response body shape {"enabled": <bool>} on all paths.
func TestRESTToggle_ResponseBodyShape(t *testing.T) {
	cases := []struct {
		query   string
		wantVal bool
	}{
		{"?enabled=false", false},
		{"?enabled=true", true},
	}
	for _, tc := range cases {
		_, _, srv := newToggleWorld(t)
		resp := restDo(t, srv, "PUT", "/toggle/Foo"+tc.query, "")
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("query %q: want 200, got %d", tc.query, resp.StatusCode)
		}
		// Decode as generic map to verify exact key/type.
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Fatalf("query %q: unmarshal: %v; body: %s", tc.query, err, body)
		}
		v, hasKey := raw["enabled"]
		if !hasKey {
			t.Errorf("query %q: response missing 'enabled' key; body: %s", tc.query, body)
			continue
		}
		vBool, isBool := v.(bool)
		if !isBool {
			t.Errorf("query %q: 'enabled' is not bool, got %T", tc.query, v)
			continue
		}
		if vBool != tc.wantVal {
			t.Errorf("query %q: enabled want %v, got %v", tc.query, tc.wantVal, vBool)
		}
	}

	// Also verify flip path.
	_, _, srv := newToggleWorld(t)
	resp := restDo(t, srv, "PUT", "/toggle/Foo", "") // flip from enabled → disabled
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("flip: want 200, got %d", resp.StatusCode)
	}
	got := decodeToggleBody(t, body)
	if got != false {
		t.Errorf("flip from enabled: response enabled want false, got %v", got)
	}
}

// Test 10: idempotency — second PUT ?enabled=false on already-disabled entity → 200.
func TestRESTToggle_Idempotent(t *testing.T) {
	w, foo, srv := newToggleWorld(t)

	// Disable once.
	resp1 := restDo(t, srv, "PUT", "/toggle/Foo?enabled=false", "")
	readBody(t, resp1)
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first disable: want 200, got %d", resp1.StatusCode)
	}

	// Disable again — should still be 200 and body {"enabled": false}.
	resp2 := restDo(t, srv, "PUT", "/toggle/Foo?enabled=false", "")
	body2 := readBody(t, resp2)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second disable: want 200, got %d; body: %s", resp2.StatusCode, body2)
	}
	if got := decodeToggleBody(t, body2); got != false {
		t.Errorf("second disable: response enabled want false, got %v", got)
	}

	var disabled bool
	w.Read(func(fr *flecs.Reader) { disabled = flecs.IsDisabled(fr, foo) })
	if !disabled {
		t.Error("entity should still be disabled after idempotent second PUT")
	}
}

// Test 11: component toggle flip.
func TestRESTToggle_ComponentFlip(t *testing.T) {
	w, foo, srv := newToggleWorld(t)

	posID, _ := w.Lookup("Pos")

	// Pos starts enabled; flip should disable it.
	resp := restDo(t, srv, "PUT", "/toggle/Foo/Pos", "")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("flip to disabled: want 200, got %d; body: %s", resp.StatusCode, body)
	}
	if got := decodeToggleBody(t, body); got != false {
		t.Errorf("flip to disabled: response enabled want false, got %v", got)
	}
	var enabled bool
	w.Read(func(fr *flecs.Reader) { enabled = flecs.IsEnabledID(fr, foo, posID) })
	if enabled {
		t.Error("Pos should be disabled after first flip")
	}

	// Flip again; should re-enable.
	resp2 := restDo(t, srv, "PUT", "/toggle/Foo/Pos", "")
	body2 := readBody(t, resp2)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("flip to enabled: want 200, got %d; body: %s", resp2.StatusCode, body2)
	}
	if got := decodeToggleBody(t, body2); got != true {
		t.Errorf("flip to enabled: response enabled want true, got %v", got)
	}
	w.Read(func(fr *flecs.Reader) { enabled = flecs.IsEnabledID(fr, foo, posID) })
	if !enabled {
		t.Error("Pos should be re-enabled after second flip")
	}
}

// Test 12: component entity not found → 404.
func TestRESTToggle_ComponentEntityNotFound(t *testing.T) {
	_, _, srv := newToggleWorld(t)

	resp := restDo(t, srv, "PUT", "/toggle/NoSuchEntity/Pos?enabled=false", "")
	readBody(t, resp)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

// Test 13: component not found → 404.
func TestRESTToggle_ComponentNotFound(t *testing.T) {
	_, _, srv := newToggleWorld(t)

	resp := restDo(t, srv, "PUT", "/toggle/Foo/NoSuchComp?enabled=false", "")
	readBody(t, resp)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

// Test 14: entity does not have the component → 400.
func TestRESTToggle_ComponentNotOnEntity(t *testing.T) {
	w, _, srv := newToggleWorld(t)

	// Create a separate entity without Pos.
	w.Write(func(fw *flecs.Writer) {
		bare := fw.NewEntity()
		w.SetName(bare, "Bare")
	})

	resp := restDo(t, srv, "PUT", "/toggle/Bare/Pos?enabled=false", "")
	readBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}
