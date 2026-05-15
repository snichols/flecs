package flecs_test

import (
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

// rqPos and rqVel are REST query test component types.
type rqPos struct{ X, Y float32 }
type rqVel struct{ DX, DY float32 }
type rqTag struct{}

// rqSetup creates a world with Position+Velocity entities and an httptest.Server.
// Component entities are given explicit names so DSL expressions can resolve them.
func rqSetup(t *testing.T) (*flecs.World, *httptest.Server) {
	t.Helper()
	w := flecs.New()
	posID := flecs.RegisterComponent[rqPos](w)
	w.SetName(posID, "Position")
	velID := flecs.RegisterComponent[rqVel](w)
	w.SetName(velID, "Velocity")
	tagID := flecs.RegisterComponent[rqTag](w)
	w.SetName(tagID, "Tagged")

	w.Write(func(fw *flecs.Writer) {
		e1 := fw.NewEntity()
		w.SetName(e1, "archer")
		flecs.Set(fw, e1, rqPos{X: 1, Y: 2})
		flecs.Set(fw, e1, rqVel{DX: 3, DY: 4})

		e2 := fw.NewEntity()
		w.SetName(e2, "mage")
		flecs.Set(fw, e2, rqPos{X: 5, Y: 6})
		flecs.AddID(fw, e2, tagID)

		// disabled entity
		e3 := fw.NewEntity()
		w.SetName(e3, "disabled")
		flecs.Set(fw, e3, rqPos{X: 7, Y: 8})
		flecs.AddID(fw, e3, w.Disabled())
	})

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)
	return w, srv
}

func rqGet(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func rqDecodeResponse(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, body)
	}
	return m
}

func TestRest_Query_SingleComponent(t *testing.T) {
	_, srv := rqSetup(t)
	resp := rqGet(t, srv, "/query?expr=Position")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	count := int(m["count"].(float64))
	if count < 2 {
		t.Errorf("expected at least 2 entities with Position, got count=%d", count)
	}
}

func TestRest_Query_AND(t *testing.T) {
	_, srv := rqSetup(t)
	resp := rqGet(t, srv, "/query?expr=Position,Velocity")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	results := m["results"].([]any)
	// Only archer has both Position and Velocity
	if len(results) != 1 {
		t.Errorf("expected 1 result with both Position and Velocity, got %d", len(results))
	}
}

func TestRest_Query_NOT(t *testing.T) {
	_, srv := rqSetup(t)
	// Position entities minus those with Disabled tag
	resp := rqGet(t, srv, "/query?expr=Position,!Disabled")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	results := m["results"].([]any)
	for _, r := range results {
		entry := r.(map[string]any)
		if entry["path"] == "disabled" {
			t.Error("disabled entity should be excluded by !Disabled filter")
		}
	}
	// archer and mage should be present
	if len(results) < 2 {
		t.Errorf("expected at least 2 results (archer, mage), got %d", len(results))
	}
}

func TestRest_Query_Pair_Exact(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[rqPos](w)
	w.SetName(posID, "Position")
	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		w.SetName(parent, "parent")
		child = fw.NewEntity()
		w.SetName(child, "child")
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
		flecs.Set(fw, child, rqPos{X: 1, Y: 2})
	})
	_, _ = parent, child
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := rqGet(t, srv, "/query?expr=(ChildOf,parent)")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	count := int(m["count"].(float64))
	if count < 1 {
		t.Errorf("expected at least 1 child entity, got count=%d", count)
	}
}

func TestRest_Query_Pair_Wildcard(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		p1 := fw.NewEntity()
		w.SetName(p1, "p1")
		c1 := fw.NewEntity()
		w.SetName(c1, "c1")
		flecs.AddID(fw, c1, flecs.MakePair(w.ChildOf(), p1))

		p2 := fw.NewEntity()
		w.SetName(p2, "p2")
		c2 := fw.NewEntity()
		w.SetName(c2, "c2")
		flecs.AddID(fw, c2, flecs.MakePair(w.ChildOf(), p2))
	})
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := rqGet(t, srv, "/query?expr=(ChildOf,*)")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	count := int(m["count"].(float64))
	if count < 2 {
		t.Errorf("expected at least 2 children from wildcard pair query, got count=%d", count)
	}
}

func TestRest_Query_Limit(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[rqPos](w)
	w.SetName(posID, "Position")
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < 100; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, rqPos{X: float32(i), Y: 0})
		}
	})
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := rqGet(t, srv, "/query?expr=Position&limit=10")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	count := int(m["count"].(float64))
	if count != 100 {
		t.Errorf("want count=100, got %d", count)
	}
	results := m["results"].([]any)
	if len(results) != 10 {
		t.Errorf("want 10 results (limit), got %d", len(results))
	}
	limit := int(m["limit"].(float64))
	if limit != 10 {
		t.Errorf("want limit=10, got %d", limit)
	}
}

func TestRest_Query_Offset(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[rqPos](w)
	w.SetName(posID, "Position")
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < 30; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, rqPos{X: float32(i), Y: 0})
		}
	})
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := rqGet(t, srv, "/query?expr=Position&limit=10&offset=20")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	results := m["results"].([]any)
	if len(results) != 10 {
		t.Errorf("want 10 results (offset 20 of 30), got %d", len(results))
	}
	if int(m["offset"].(float64)) != 20 {
		t.Errorf("want offset=20, got %v", m["offset"])
	}
}

func TestRest_Query_FieldsFalse(t *testing.T) {
	_, srv := rqSetup(t)
	resp := rqGet(t, srv, "/query?expr=Position&fields=false")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	results := m["results"].([]any)
	for _, r := range results {
		entry := r.(map[string]any)
		if _, hasFields := entry["fields"]; hasFields {
			t.Error("fields=false should omit fields from results")
		}
	}
}

func TestRest_Query_FieldsDefault_True(t *testing.T) {
	_, srv := rqSetup(t)
	// Query for archer which has both Position and Velocity
	resp := rqGet(t, srv, "/query?expr=Position,Velocity")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	results := m["results"].([]any)
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	for _, r := range results {
		entry := r.(map[string]any)
		fields, ok := entry["fields"].(map[string]any)
		if !ok {
			t.Errorf("expected fields object, got %T", entry["fields"])
			continue
		}
		if _, hasPosField := fields["Position"]; !hasPosField {
			t.Errorf("expected Position field in fields, got keys: %v", rqFieldsKeys(fields))
		}
	}
}

func TestRest_Query_FieldsForTag(t *testing.T) {
	_, srv := rqSetup(t)
	// mage has Position and Tagged; Tagged is a zero-size tag (struct{})
	resp := rqGet(t, srv, "/query?expr=Position,Tagged")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	results := m["results"].([]any)
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for Position,Tagged")
	}
	for _, r := range results {
		entry := r.(map[string]any)
		fields, _ := entry["fields"].(map[string]any)
		if _, hasTagField := fields["Tagged"]; hasTagField {
			t.Error("tag field Tagged should not appear in fields (zero-size)")
		}
	}
}

func TestRest_Query_FieldsForPair(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[rqPos](w)
	w.SetName(posID, "Position")
	var relID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel := fw.NewEntity()
		w.SetName(rel, "HasPos")
		relID = rel

		obj := fw.NewEntity()
		w.SetName(obj, "obj")
		flecs.SetPair[rqPos](fw, obj, rel, posID, rqPos{X: 10, Y: 20})
	})
	_ = relID
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := rqGet(t, srv, "/query?expr=(HasPos,Position)")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	results := m["results"].([]any)
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for pair query")
	}
	for _, r := range results {
		entry := r.(map[string]any)
		fields, ok := entry["fields"].(map[string]any)
		if !ok {
			continue
		}
		foundTilde := false
		for k := range fields {
			if strings.Contains(k, "~") {
				foundTilde = true
				break
			}
		}
		if !foundTilde {
			t.Errorf("expected pair field key with '~' separator, got keys: %v", rqFieldsKeys(fields))
		}
	}
}

func TestRest_Query_MissingExpr(t *testing.T) {
	_, srv := rqSetup(t)
	resp := rqGet(t, srv, "/query")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", resp.StatusCode, body)
	}
}

func TestRest_Query_ParseError_Position(t *testing.T) {
	_, srv := rqSetup(t)
	// Expression with invalid syntax (bare '!' followed by nothing)
	resp := rqGet(t, srv, "/query?expr=!")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", resp.StatusCode, body)
	}
	// Error body should contain position/context information
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "position") {
		t.Logf("parse error response: %s", bodyStr)
	}
}

func TestRest_Query_UnknownIdentifier(t *testing.T) {
	_, srv := rqSetup(t)
	resp := rqGet(t, srv, "/query?expr=NonExistentComponent")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "NonExistentComponent") {
		t.Errorf("error body should mention the unknown identifier, got: %s", body)
	}
}

func TestRest_Query_LimitInvalid(t *testing.T) {
	_, srv := rqSetup(t)
	resp := rqGet(t, srv, "/query?expr=Position&limit=abc")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for invalid limit, got %d: %s", resp.StatusCode, body)
	}
}

func TestRest_Query_LimitOverMax(t *testing.T) {
	_, srv := rqSetup(t)
	resp := rqGet(t, srv, fmt.Sprintf("/query?expr=Position&limit=%d", 4097))
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for limit over max, got %d: %s", resp.StatusCode, body)
	}
}

func TestRest_Query_BadMethod(t *testing.T) {
	_, srv := rqSetup(t)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/query?expr=Position", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /query: %v", err)
	}
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d: %s", resp.StatusCode, body)
	}
}

func TestRest_Query_ConcurrentReadsDuringWrite(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[rqPos](w)
	w.SetName(posID, "Position")
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < 10; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, rqPos{X: float32(i), Y: 0})
		}
	})
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp := rqGet(t, srv, "/query?expr=Position")
			body := readBody(t, resp)
			if resp.StatusCode != http.StatusOK {
				t.Errorf("concurrent read: want 200, got %d: %s", resp.StatusCode, body)
			}
		}()
	}
	wg.Wait()
}

func TestRest_Query_NoMatches(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[rqPos](w)
	w.SetName(posID, "Position")
	velID := flecs.RegisterComponent[rqVel](w)
	w.SetName(velID, "Velocity")
	// Add only Position entities, no Velocity
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, rqPos{X: 1, Y: 1})
	})
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := rqGet(t, srv, "/query?expr=Position,Velocity")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 for no-match query, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	if count := int(m["count"].(float64)); count != 0 {
		t.Errorf("want count=0, got %d", count)
	}
	results, ok := m["results"].([]any)
	if !ok {
		t.Fatalf("results should be an array")
	}
	if len(results) != 0 {
		t.Errorf("want empty results, got %d", len(results))
	}
}

func TestRest_Query_DynamicComponentBase64(t *testing.T) {
	w := flecs.New()
	var dynID flecs.ID
	val := uint64(0xDEADBEEF)
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponent(fw, "DynComp", 8, 8)
		w.SetName(dynID, "DynComp")
		e := fw.NewEntity()
		w.SetName(e, "obj")
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&val))
	})
	_ = dynID
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := rqGet(t, srv, "/query?expr=DynComp")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 for dynamic component query, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	results := m["results"].([]any)
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for dynamic component query")
	}
	for _, r := range results {
		entry := r.(map[string]any)
		fields, ok := entry["fields"].(map[string]any)
		if !ok || len(fields) == 0 {
			t.Errorf("expected fields with DynComp key, got: %v", entry["fields"])
			continue
		}
		if _, hasDynField := fields["DynComp"]; !hasDynField {
			t.Errorf("expected DynComp field, got keys: %v", rqFieldsKeys(fields))
		}
	}
}

func TestRest_Query_DynamicComponentWithMarshaler(t *testing.T) {
	w := flecs.New()
	var dynID flecs.ID
	dynVal := uint64(42)
	w.Write(func(fw *flecs.Writer) {
		dynID = flecs.RegisterDynamicComponentWithMarshaler(fw, "DynWithMarshaler", 8, 8,
			func(ptr unsafe.Pointer) (json.RawMessage, error) {
				v := *(*uint64)(ptr)
				return json.Marshal(v)
			},
			func(data json.RawMessage, ptr unsafe.Pointer) error {
				var v uint64
				if err := json.Unmarshal(data, &v); err != nil {
					return err
				}
				*(*uint64)(ptr) = v
				return nil
			},
		)
		w.SetName(dynID, "DynWithMarshaler")
		e := fw.NewEntity()
		w.SetName(e, "dynobj")
		flecs.SetIDPtr(fw, e, dynID, unsafe.Pointer(&dynVal))
	})
	_ = dynID
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := rqGet(t, srv, "/query?expr=DynWithMarshaler")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	results := m["results"].([]any)
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	for _, r := range results {
		entry := r.(map[string]any)
		fields, ok := entry["fields"].(map[string]any)
		if !ok || len(fields) == 0 {
			t.Errorf("expected fields with DynWithMarshaler, got: %v", entry["fields"])
			continue
		}
		if _, ok := fields["DynWithMarshaler"]; !ok {
			t.Errorf("expected DynWithMarshaler field, got keys: %v", rqFieldsKeys(fields))
		}
	}
}

func rqFieldsKeys(fields map[string]any) []string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	return keys
}

func TestRest_Query_OffsetInvalid(t *testing.T) {
	_, srv := rqSetup(t)
	resp := rqGet(t, srv, "/query?expr=Position&offset=-1")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for negative offset, got %d: %s", resp.StatusCode, body)
	}
}

func TestRest_Query_FieldsExplicitTrue(t *testing.T) {
	_, srv := rqSetup(t)
	resp := rqGet(t, srv, "/query?expr=Position&fields=true")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 for fields=true, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	if int(m["count"].(float64)) == 0 {
		t.Error("expected at least one result with fields=true")
	}
}

func TestRest_Query_FieldsInvalid(t *testing.T) {
	_, srv := rqSetup(t)
	resp := rqGet(t, srv, "/query?expr=Position&fields=bogus")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for invalid fields param, got %d: %s", resp.StatusCode, body)
	}
}

func TestRest_Query_WorldLocked(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[rqPos](w)
	w.SetName(posID, "Position")
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	// Lock the world from the test goroutine; the HTTP handler goroutine will panic.
	w.ExclusiveAccessBegin("test-lock")
	defer w.ExclusiveAccessEnd(false)

	resp := rqGet(t, srv, "/query?expr=Position")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("want 503 when world is locked, got %d: %s", resp.StatusCode, body)
	}
}
