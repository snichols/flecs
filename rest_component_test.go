package flecs_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
)

// rcPos and rcVel are the typed components for component-endpoint tests.
// Registered then renamed to short names so Lookup works without dots.
type rcPos struct{ X, Y float32 }
type rcVel struct{ DX, DY float32 }

// rcPairVal is a typed pair data type.
type rcPairVal struct{ N int32 }

// newComponentWorld creates a world with two typed components, one dynamic component,
// and a named test entity.
func newComponentWorld(t *testing.T) (*flecs.World, flecs.ID, *httptest.Server) {
	t.Helper()
	w := flecs.New()

	posID := flecs.RegisterComponent[rcPos](w)
	w.SetName(posID, "Pos")

	velID := flecs.RegisterComponent[rcVel](w)
	w.SetName(velID, "Vel")

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dynID := flecs.RegisterDynamicComponent(fw, "Dyn4", 4, 4)
		w.SetName(dynID, "Dyn4")

		e = fw.NewEntity()
		w.SetName(e, "actor")
	})
	_, _ = posID, velID

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)
	return w, e, srv
}

// Test 1: PUT data component with valid JSON → 200, component is set.
func TestRESTPutComponentDataValid(t *testing.T) {
	w, e, srv := newComponentWorld(t)

	resp := restDo(t, srv, "PUT", "/component/actor/Pos", `{"X":1.5,"Y":2.5}`)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /component: want 200, got %d; body: %s", resp.StatusCode, body)
	}
	var pos rcPos
	var ok bool
	w.Read(func(fr *flecs.Reader) { pos, ok = flecs.Get[rcPos](fr, e) })
	if !ok {
		t.Fatal("Pos not found on entity after PUT")
	}
	if pos.X != 1.5 || pos.Y != 2.5 {
		t.Errorf("Pos want {1.5, 2.5}, got %v", pos)
	}
}

// Test 2: PUT data component with malformed JSON → 400.
func TestRESTPutComponentMalformedJSON(t *testing.T) {
	_, _, srv := newComponentWorld(t)

	resp := restDo(t, srv, "PUT", "/component/actor/Pos", `{not valid`)
	readBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT /component malformed JSON: want 400, got %d", resp.StatusCode)
	}
}

// Test 3: PUT data component with wrong-shape JSON (string where struct expected) → 400.
func TestRESTPutComponentWrongShape(t *testing.T) {
	_, _, srv := newComponentWorld(t)

	resp := restDo(t, srv, "PUT", "/component/actor/Pos", `"not a struct"`)
	readBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT /component wrong shape: want 400, got %d", resp.StatusCode)
	}
}

// Test 4: PUT tag entity (bare entity without TypeInfo) with empty body → 200.
func TestRESTPutTagComponentEmpty(t *testing.T) {
	w, e, srv := newComponentWorld(t)

	var tagID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tagID = fw.NewEntity()
		w.SetName(tagID, "MyTag")
	})

	resp := restDo(t, srv, "PUT", "/component/actor/MyTag", "")
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT tag with empty body: want 200, got %d; body: %s", resp.StatusCode, body)
	}
	var hasTag bool
	w.Read(func(fr *flecs.Reader) { hasTag = flecs.HasID(fr, e, tagID) })
	if !hasTag {
		t.Error("tag not added to entity after PUT")
	}
}

// Test 5: PUT tag component with non-empty body → 400.
func TestRESTPutTagComponentNonEmpty(t *testing.T) {
	w, _, srv := newComponentWorld(t)

	var tagID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tagID = fw.NewEntity()
		w.SetName(tagID, "MyTag2")
	})
	_ = tagID

	resp := restDo(t, srv, "PUT", "/component/actor/MyTag2", `"nonempty"`)
	readBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT tag with non-empty body: want 400, got %d", resp.StatusCode)
	}
}

// Test 6: PUT dynamic component with valid base64 → 200, bytes written.
func TestRESTPutComponentDynamicValid(t *testing.T) {
	w, e, srv := newComponentWorld(t)

	data := []byte{0x01, 0x02, 0x03, 0x04}
	b64 := base64.StdEncoding.EncodeToString(data)
	body, _ := json.Marshal(b64)

	resp := restDo(t, srv, "PUT", "/component/actor/Dyn4", string(body))
	rb := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT dynamic valid: want 200, got %d; body: %s", resp.StatusCode, rb)
	}

	var ptr unsafe.Pointer
	w.Read(func(fr *flecs.Reader) {
		dynID, _ := fr.Lookup("Dyn4")
		ptr = flecs.GetIDPtr(fr, e, dynID)
	})
	if ptr == nil {
		t.Fatal("dynamic component pointer is nil after PUT")
	}
	got := (*[4]byte)(ptr)
	if got[0] != 0x01 || got[1] != 0x02 || got[2] != 0x03 || got[3] != 0x04 {
		t.Errorf("dynamic bytes want [1 2 3 4], got %v", got)
	}
}

// Test 7: PUT dynamic component with wrong-size base64 → 400.
func TestRESTPutComponentDynamicWrongSize(t *testing.T) {
	_, _, srv := newComponentWorld(t)

	data := []byte{0xAA, 0xBB, 0xCC} // 3 bytes instead of 4
	b64 := base64.StdEncoding.EncodeToString(data)
	body, _ := json.Marshal(b64)

	resp := restDo(t, srv, "PUT", "/component/actor/Dyn4", string(body))
	readBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT dynamic wrong size: want 400, got %d", resp.StatusCode)
	}
}

// Test 8a: PUT tag pair R~T with empty body → 200; pair is added.
func TestRESTPutTagPair(t *testing.T) {
	w, e, srv := newComponentWorld(t)

	var rel, tgt flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		w.SetName(rel, "Rel")
		tgt = fw.NewEntity()
		w.SetName(tgt, "TgtA")
	})

	resp := restDo(t, srv, "PUT", "/component/actor/Rel~TgtA", "")
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT tag pair: want 200, got %d; body: %s", resp.StatusCode, body)
	}
	pairID := flecs.MakePair(rel, tgt)
	var hasPair bool
	w.Read(func(fr *flecs.Reader) { hasPair = flecs.HasID(fr, e, pairID) })
	if !hasPair {
		t.Error("tag pair not added to entity after PUT")
	}
}

// Test 8b: PUT typed pair R~T with JSON body → 200.
func TestRESTPutTypedPair(t *testing.T) {
	w, e, srv := newComponentWorld(t)

	var rel, tgt flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		w.SetName(rel, "DataRel")
		tgt = fw.NewEntity()
		w.SetName(tgt, "TgtB")
		// Seed the pair type registration before the REST request.
		fw.SetPairByID(e, rel, tgt, rcPairVal{N: 0})
	})

	resp := restDo(t, srv, "PUT", "/component/actor/DataRel~TgtB", `{"N":42}`)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT typed pair: want 200, got %d; body: %s", resp.StatusCode, body)
	}
	var pv rcPairVal
	var ok bool
	w.Read(func(fr *flecs.Reader) { pv, ok = flecs.GetPair[rcPairVal](fr, e, rel, tgt) })
	if !ok {
		t.Fatal("typed pair not found after PUT")
	}
	if pv.N != 42 {
		t.Errorf("typed pair N want 42, got %d", pv.N)
	}
}

// Test 9: PUT with unresolvable entity path → 404.
func TestRESTPutComponentEntityNotFound(t *testing.T) {
	_, _, srv := newComponentWorld(t)

	resp := restDo(t, srv, "PUT", "/component/nosuchentity/Pos", `{}`)
	readBody(t, resp)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("PUT unresolvable entity: want 404, got %d", resp.StatusCode)
	}
}

// Test 10: PUT with unresolvable component path → 404.
func TestRESTPutComponentComponentNotFound(t *testing.T) {
	_, _, srv := newComponentWorld(t)

	resp := restDo(t, srv, "PUT", "/component/actor/NoSuchComponent", `{}`)
	readBody(t, resp)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("PUT unresolvable component: want 404, got %d", resp.StatusCode)
	}
}

// Test 11: PUT with body > 1 MB → 413.
// Uses the dynamic-component path (io.ReadAll) so MaxBytesReader fires before
// any format-specific parsing.
func TestRESTPutComponentBodyTooLarge(t *testing.T) {
	_, _, srv := newComponentWorld(t)

	bigBody := strings.Repeat("x", (1<<20)+100)
	resp := restDo(t, srv, "PUT", "/component/actor/Dyn4", bigBody)
	readBody(t, resp)

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("PUT body too large: want 413, got %d", resp.StatusCode)
	}
}

// Test 12: DELETE existing component → 200, component removed.
func TestRESTDeleteComponentExisting(t *testing.T) {
	w, e, srv := newComponentWorld(t)

	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, rcPos{X: 9, Y: 9}) })

	resp := restDo(t, srv, "DELETE", "/component/actor/Pos", "")
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE existing component: want 200, got %d; body: %s", resp.StatusCode, body)
	}
	var hasPos bool
	w.Read(func(fr *flecs.Reader) { hasPos = flecs.Has[rcPos](fr, e) })
	if hasPos {
		t.Error("Pos still present after DELETE")
	}
}

// Test 13: DELETE component entity never had → 200 (idempotent).
func TestRESTDeleteComponentNotPresent(t *testing.T) {
	_, _, srv := newComponentWorld(t)

	// actor has no Vel; DELETE is idempotent per locked-in decision.
	resp := restDo(t, srv, "DELETE", "/component/actor/Vel", "")
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE absent component: want 200, got %d; body: %s", resp.StatusCode, body)
	}
}

// Test 14: DELETE with unresolvable entity path → 404.
func TestRESTDeleteComponentEntityNotFound(t *testing.T) {
	_, _, srv := newComponentWorld(t)

	resp := restDo(t, srv, "DELETE", "/component/nosuchentity/Pos", "")
	readBody(t, resp)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("DELETE unresolvable entity: want 404, got %d", resp.StatusCode)
	}
}

// Test 15: DELETE with unresolvable component path → 404.
func TestRESTDeleteComponentComponentNotFound(t *testing.T) {
	_, _, srv := newComponentWorld(t)

	resp := restDo(t, srv, "DELETE", "/component/actor/NoSuchComp", "")
	readBody(t, resp)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("DELETE unresolvable component: want 404, got %d", resp.StatusCode)
	}
}

// Test 16: Concurrent PUT/DELETE goroutines — race-detector clean.
func TestRESTComponentMutationConcurrent(t *testing.T) {
	w, _, srv := newComponentWorld(t)

	entities := make([]flecs.ID, 10)
	w.Write(func(fw *flecs.Writer) {
		for i := range entities {
			entities[i] = fw.NewEntity()
			w.SetName(entities[i], fmt.Sprintf("cent%d", i))
		}
	})

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("cent%d", i)
			putResp := restDo(t, srv, "PUT", "/component/"+name+"/Pos", `{"X":1,"Y":2}`)
			readBody(t, putResp)
			if putResp.StatusCode != http.StatusOK {
				t.Errorf("concurrent PUT /component: want 200, got %d", putResp.StatusCode)
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("cent%d", i)
			delResp := restDo(t, srv, "DELETE", "/component/"+name+"/Pos", "")
			readBody(t, delResp)
			sc := delResp.StatusCode
			if sc != http.StatusOK {
				t.Errorf("concurrent DELETE /component: want 200, got %d", sc)
			}
		}(i)
	}
	wg.Wait()
}

// Test 17a: PUT component when a post-merge hook panics → 503.
func TestRESTPutComponentWorldUnavailable(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[rcPos](w)
	w.SetName(posID, "Pos")

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		w.SetName(e, "actor")
	})
	_ = e

	flecs.OnPostMerge(w, func(_ *flecs.Writer) {
		panic("simulated world unavailable")
	})
	srv := httptest.NewServer(flecs.NewRESTHandler(w))

	resp := restDo(t, srv, "PUT", "/component/actor/Pos", `{"X":1,"Y":2}`)
	readBody(t, resp)

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("PUT /component world unavailable: want 503, got %d", resp.StatusCode)
	}
}

// Test 17b: DELETE component when a post-merge hook panics → 503.
func TestRESTDeleteComponentWorldUnavailable(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[rcPos](w)
	w.SetName(posID, "Pos")

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		w.SetName(e, "actor")
		flecs.Set(fw, e, rcPos{X: 1, Y: 2})
	})
	_ = e

	flecs.OnPostMerge(w, func(_ *flecs.Writer) {
		panic("simulated world unavailable")
	})
	srv := httptest.NewServer(flecs.NewRESTHandler(w))

	resp := restDo(t, srv, "DELETE", "/component/actor/Pos", "")
	readBody(t, resp)

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("DELETE /component world unavailable: want 503, got %d", resp.StatusCode)
	}
}
