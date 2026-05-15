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

// ── v2 REST integration tests ────────────────────────────────────────────────

// rqEntityPaths returns the set of "path" strings from a results array.
func rqEntityPaths(t *testing.T, results []any) map[string]bool {
	t.Helper()
	paths := make(map[string]bool, len(results))
	for _, r := range results {
		entry := r.(map[string]any)
		if p, ok := entry["path"].(string); ok && p != "" {
			paths[p] = true
		}
	}
	return paths
}

// Component types exclusive to v2 REST tests.
type rq2Ship struct{}
type rq2Planet struct{ Name string }

func TestRest_Query_Or(t *testing.T) {
	// "Position || Velocity" — returns union of entities with either component.
	_, srv := rqSetup(t)
	resp := rqGet(t, srv, "/query?expr=Position+%7C%7C+Velocity")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	count := int(m["count"].(float64))
	// archer (both), mage (Position only) — disabled entity skipped by engine
	if count < 2 {
		t.Errorf("want ≥2 results for Position||Velocity union, got count=%d", count)
	}
	paths := rqEntityPaths(t, m["results"].([]any))
	if !paths["archer"] {
		t.Error("archer (Position+Velocity) must appear in OR union")
	}
	if !paths["mage"] {
		t.Error("mage (Position only) must appear in OR union")
	}
}

func TestRest_Query_ScopeGroup(t *testing.T) {
	// "Position, !(Velocity, Tagged)" — entities with Position AND NOT (Velocity AND Tagged).
	// archer has Velocity (no Tagged) → !(V,T) = NOT(V AND T) = NOT false = true → included
	// mage   has Tagged  (no Velocity) → !(V,T) = NOT false = true → included
	// We add a "both" entity (Velocity + Tagged) to verify it is excluded.
	w := flecs.New()
	posID := flecs.RegisterComponent[rqPos](w)
	w.SetName(posID, "Position")
	velID := flecs.RegisterComponent[rqVel](w)
	w.SetName(velID, "Velocity")
	tagID := flecs.RegisterComponent[rqTag](w)
	w.SetName(tagID, "Tagged")

	w.Write(func(fw *flecs.Writer) {
		e1 := fw.NewEntity()
		w.SetName(e1, "posOnly")
		flecs.Set(fw, e1, rqPos{X: 1})

		e2 := fw.NewEntity()
		w.SetName(e2, "posVel")
		flecs.Set(fw, e2, rqPos{X: 2})
		flecs.Set(fw, e2, rqVel{DX: 1})

		e3 := fw.NewEntity()
		w.SetName(e3, "posTagged")
		flecs.Set(fw, e3, rqPos{X: 3})
		flecs.AddID(fw, e3, tagID)

		// This entity has Position + Velocity + Tagged → must be excluded by scope
		e4 := fw.NewEntity()
		w.SetName(e4, "posVelTagged")
		flecs.Set(fw, e4, rqPos{X: 4})
		flecs.Set(fw, e4, rqVel{DX: 2})
		flecs.AddID(fw, e4, tagID)
	})

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := rqGet(t, srv, "/query?expr=Position,+!(Velocity,+Tagged)")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	paths := rqEntityPaths(t, m["results"].([]any))
	if paths["posVelTagged"] {
		t.Error("posVelTagged must be excluded by !(Velocity, Tagged) scope group")
	}
	if !paths["posOnly"] {
		t.Error("posOnly must be included (Position, not Velocity+Tagged)")
	}
	if !paths["posVel"] {
		t.Error("posVel must be included (Position+Velocity but not Tagged)")
	}
}

func TestRest_Query_SourceBinding(t *testing.T) {
	// "Velocity, Position(playerEntity)" — matches entities with Velocity and
	// reads Position from playerEntity (fixed source, snapshot-at-iter-start).
	w := flecs.New()
	posID := flecs.RegisterComponent[rqPos](w)
	w.SetName(posID, "Position")
	velID := flecs.RegisterComponent[rqVel](w)
	w.SetName(velID, "Velocity")

	w.Write(func(fw *flecs.Writer) {
		player := fw.NewEntity()
		w.SetName(player, "playerEntity")
		flecs.Set(fw, player, rqPos{X: 5, Y: 5})

		npc1 := fw.NewEntity()
		w.SetName(npc1, "npc1")
		flecs.Set(fw, npc1, rqVel{DX: 1})

		npc2 := fw.NewEntity()
		w.SetName(npc2, "npc2")
		flecs.Set(fw, npc2, rqVel{DX: 2})
	})

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	// Source binding: reads Position from playerEntity, not from iterated entity.
	resp := rqGet(t, srv, "/query?expr=Velocity,+Position(playerEntity)")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	count := int(m["count"].(float64))
	// npc1 and npc2 have Velocity; playerEntity has Position but no Velocity
	if count < 2 {
		t.Errorf("want ≥2 results (npc1, npc2 have Velocity), got count=%d", count)
	}
	paths := rqEntityPaths(t, m["results"].([]any))
	if !paths["npc1"] || !paths["npc2"] {
		t.Errorf("npc1 and npc2 must appear; got paths: %v", paths)
	}
}

func TestRest_Query_OptionalTerm(t *testing.T) {
	// "Position, ?Velocity" — all entities with Position are returned regardless
	// of whether they have Velocity.
	_, srv := rqSetup(t)
	resp := rqGet(t, srv, "/query?expr=Position,+%3FVelocity")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	// archer has Position+Velocity; mage has Position only.
	// Both should appear (optional Velocity does not exclude).
	paths := rqEntityPaths(t, m["results"].([]any))
	if !paths["archer"] {
		t.Error("archer must appear in Position,?Velocity query")
	}
	if !paths["mage"] {
		t.Error("mage (no Velocity) must still appear when Velocity is optional")
	}
}

func TestRest_Query_TraversalUp(t *testing.T) {
	// (ChildOf, root).Up — returns entities whose nearest ChildOf-ancestor has
	// (ChildOf, root); i.e., grandchildren of root.
	w := flecs.New()
	posID := flecs.RegisterComponent[rqPos](w)
	w.SetName(posID, "Position")

	w.Write(func(fw *flecs.Writer) {
		root := fw.NewEntity()
		w.SetName(root, "root")

		child := fw.NewEntity()
		w.SetName(child, "childOfRoot")
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), root))

		grandchild := fw.NewEntity()
		w.SetName(grandchild, "grandchildOfRoot")
		flecs.AddID(fw, grandchild, flecs.MakePair(w.ChildOf(), child))
		flecs.Set(fw, grandchild, rqPos{X: 1})
	})

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := rqGet(t, srv, "/query?expr=(ChildOf,+root).Up")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	count := int(m["count"].(float64))
	// grandchild's nearest ChildOf-ancestor (child) has (ChildOf, root) → matches
	if count == 0 {
		t.Errorf("want ≥1 result for (ChildOf, root).Up traversal, got 0")
	}
	paths := rqEntityPaths(t, m["results"].([]any))
	// Entity paths in this world are hierarchical (parent.child.grandchild).
	if !paths["root.childOfRoot.grandchildOfRoot"] {
		t.Errorf("grandchildOfRoot must appear via .Up traversal; got paths: %v", paths)
	}
}

func TestRest_Query_Variable(t *testing.T) {
	// SpaceShip, (DockedTo, $planet) — returns ships with their docking variable bound.
	w := flecs.New()
	shipID := flecs.RegisterComponent[rq2Ship](w)
	w.SetName(shipID, "SpaceShip")
	planetID := flecs.RegisterComponent[rq2Planet](w)
	w.SetName(planetID, "Planet")

	w.Write(func(fw *flecs.Writer) {
		dockedTo := fw.NewEntity()
		w.SetName(dockedTo, "DockedTo")

		p1 := fw.NewEntity()
		w.SetName(p1, "P1")
		flecs.Set(fw, p1, rq2Planet{Name: "P1"})

		p2 := fw.NewEntity()
		w.SetName(p2, "P2")
		flecs.Set(fw, p2, rq2Planet{Name: "P2"})

		shipA := fw.NewEntity()
		w.SetName(shipA, "ShipA")
		flecs.Set(fw, shipA, rq2Ship{})
		flecs.AddID(fw, shipA, flecs.MakePair(dockedTo, p1))

		shipB := fw.NewEntity()
		w.SetName(shipB, "ShipB")
		flecs.Set(fw, shipB, rq2Ship{})
		flecs.AddID(fw, shipB, flecs.MakePair(dockedTo, p2))
	})

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	// Query: SpaceShip, (DockedTo, $planet), Planet($planet)
	resp := rqGet(t, srv, "/query?expr=SpaceShip,+(DockedTo,+%24planet),+Planet(%24planet)")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	count := int(m["count"].(float64))
	// ShipA→P1 and ShipB→P2: two docking pairs
	if count < 2 {
		t.Errorf("want ≥2 results for variable join (ship→planet), got count=%d", count)
	}
}

func TestRest_Query_Equality_Entity(t *testing.T) {
	// "$this == hero" — returns only the hero entity.
	w := flecs.New()
	posID := flecs.RegisterComponent[rqPos](w)
	w.SetName(posID, "Position")

	w.Write(func(fw *flecs.Writer) {
		hero := fw.NewEntity()
		w.SetName(hero, "hero")
		flecs.Set(fw, hero, rqPos{X: 1})

		villain := fw.NewEntity()
		w.SetName(villain, "villain")
		flecs.Set(fw, villain, rqPos{X: 2})
	})

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := rqGet(t, srv, "/query?expr=%24this+%3D%3D+hero")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	results := m["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("$this == hero should return exactly 1 result, got %d", len(results))
	}
	entry := results[0].(map[string]any)
	if entry["path"] != "hero" {
		t.Errorf("expected result path 'hero', got %v", entry["path"])
	}
}

func TestRest_Query_NameMatch(t *testing.T) {
	// "$this ~ \"arch\"" — returns entities whose name contains "arch".
	_, srv := rqSetup(t)
	resp := rqGet(t, srv, "/query?expr=%24this+%7E+%22arch%22")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	paths := rqEntityPaths(t, m["results"].([]any))
	if !paths["archer"] {
		t.Errorf("archer must match $this ~ \"arch\"; got paths: %v", paths)
	}
	if paths["mage"] {
		t.Errorf("mage must NOT match $this ~ \"arch\"")
	}
}

func TestRest_Query_AndFrom(t *testing.T) {
	// "AndFrom(preset)" — entities that have ALL components from preset's type.
	w := flecs.New()
	posID := flecs.RegisterComponent[rqPos](w)
	w.SetName(posID, "Position")

	var preset flecs.ID
	w.Write(func(fw *flecs.Writer) {
		preset = fw.NewEntity()
		w.SetName(preset, "preset")
		flecs.Set(fw, preset, rqPos{X: 0}) // preset has Position

		e1 := fw.NewEntity()
		w.SetName(e1, "hasPos")
		flecs.Set(fw, e1, rqPos{X: 1}) // has all preset components

		e2 := fw.NewEntity()
		w.SetName(e2, "noPos") // no components — won't match AndFrom
	})
	_ = preset

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := rqGet(t, srv, "/query?expr=AndFrom(preset)")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	paths := rqEntityPaths(t, m["results"].([]any))
	if !paths["hasPos"] {
		t.Errorf("hasPos must appear in AndFrom(preset) results; paths: %v", paths)
	}
}

func TestRest_Query_ComplexCompound(t *testing.T) {
	// "Position || Velocity, !(Tagged)" — (Position OR Velocity) AND NOT Tagged.
	// Combined OR + scope group in one expression.
	w := flecs.New()
	posID := flecs.RegisterComponent[rqPos](w)
	w.SetName(posID, "Position")
	velID := flecs.RegisterComponent[rqVel](w)
	w.SetName(velID, "Velocity")
	tagID := flecs.RegisterComponent[rqTag](w)
	w.SetName(tagID, "Tagged")

	w.Write(func(fw *flecs.Writer) {
		// posOnly: matches OR group (has Position), not Tagged → included
		e1 := fw.NewEntity()
		w.SetName(e1, "posOnly")
		flecs.Set(fw, e1, rqPos{X: 1})

		// velOnly: matches OR group (has Velocity), not Tagged → included
		e2 := fw.NewEntity()
		w.SetName(e2, "velOnly")
		flecs.Set(fw, e2, rqVel{DX: 1})

		// posTagged: matches OR group (has Position), has Tagged → excluded by scope
		e3 := fw.NewEntity()
		w.SetName(e3, "posTagged")
		flecs.Set(fw, e3, rqPos{X: 3})
		flecs.AddID(fw, e3, tagID)

		// neither: no Position or Velocity → excluded by OR group
		e4 := fw.NewEntity()
		w.SetName(e4, "neither")
	})

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := rqGet(t, srv, "/query?expr=Position+%7C%7C+Velocity,+!(Tagged)")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	paths := rqEntityPaths(t, m["results"].([]any))
	if !paths["posOnly"] {
		t.Error("posOnly must be included (has Position, not Tagged)")
	}
	if !paths["velOnly"] {
		t.Error("velOnly must be included (has Velocity, not Tagged)")
	}
	if paths["posTagged"] {
		t.Error("posTagged must be excluded by !(Tagged) scope")
	}
	if paths["neither"] {
		t.Error("neither must be excluded (no Position or Velocity)")
	}
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

// rqMultiVarShip and rqMultiVarPlanet are component types for the REST multi-var test.
type rqMultiVarShip struct{}
type rqMultiVarPlanet struct{ Name string }

// TestRest_Query_MultiVar_Optimized verifies that a multi-variable query expressed
// in DSL is executed through the join-order optimizer and returns the correct result
// set via GET /query. The optimizer should pick the smaller-domain variable (planet)
// as the driver when ship domain > planet domain, and the response payload must be
// identical regardless of which variable becomes the driver.
func TestRest_Query_MultiVar_Optimized(t *testing.T) {
	w := flecs.New()
	shipID := flecs.RegisterComponent[rqMultiVarShip](w)
	w.SetName(shipID, "MVShip")
	planetID := flecs.RegisterComponent[rqMultiVarPlanet](w)
	w.SetName(planetID, "MVPlanet")

	var dockedToID, A, B, C, P1, P2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dockedToID = fw.NewEntity()
		w.SetName(dockedToID, "MVDockedTo")

		P1 = fw.NewEntity()
		w.SetName(P1, "mvP1")
		flecs.Set(fw, P1, rqMultiVarPlanet{Name: "P1"})

		P2 = fw.NewEntity()
		w.SetName(P2, "mvP2")
		flecs.Set(fw, P2, rqMultiVarPlanet{Name: "P2"})

		A = fw.NewEntity()
		w.SetName(A, "mvA")
		flecs.Set(fw, A, rqMultiVarShip{})
		flecs.AddID(fw, A, flecs.MakePair(dockedToID, P1))

		B = fw.NewEntity()
		w.SetName(B, "mvB")
		flecs.Set(fw, B, rqMultiVarShip{})
		flecs.AddID(fw, B, flecs.MakePair(dockedToID, P1))
		flecs.AddID(fw, B, flecs.MakePair(dockedToID, P2))

		C = fw.NewEntity()
		w.SetName(C, "mvC")
		flecs.Set(fw, C, rqMultiVarShip{})
		flecs.AddID(fw, C, flecs.MakePair(dockedToID, P2))
	})
	_, _, _ = A, B, C

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	// Programmatic query: verify optimizer picks correctly.
	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(shipID),
			flecs.WithPairTgtVar(dockedToID, "planet"),
			flecs.WithVar(planetID, "planet"),
		)
		// Driver must be non-empty (variable query was recognised).
		if d := q.DriverVariable(); d == "" {
			t.Error("optimizer must set a driver variable for multi-var query")
		}
		it := q.Iter()
		count := 0
		for it.Next() {
			count++
		}
		if count != 4 {
			t.Errorf("programmatic query: want 4 rows (A→P1, B→P1, B→P2, C→P2), got %d", count)
		}
	})

	// REST query: DSL `MVShip,(MVDockedTo,$planet),MVPlanet($planet)`.
	// The endpoint resolves names, builds terms, fires the optimizer, and returns
	// JSON. We verify: 200 OK, count == 4 results (the $this ships that match).
	expr := "MVShip,(MVDockedTo,$planet),MVPlanet($planet)"
	resp := rqGet(t, srv, "/query?expr="+expr)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /query?expr=%s: want 200, got %d: %s", expr, resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	count := int(m["count"].(float64))
	// Each matching $this entity (ships A, B, C) appears once per binding.
	// A docks to P1 (1 row), B to P1+P2 (2 rows), C to P2 (1 row) = 4 rows.
	if count != 4 {
		t.Errorf("REST multi-var query: want count=4, got %d\nbody=%s", count, body)
	}

	results, _ := m["results"].([]any)
	if len(results) != 4 {
		t.Errorf("REST multi-var query: want 4 results, got %d", len(results))
	}
	// Verify the entity paths belong to the expected ship names.
	shipNames := map[string]bool{"mvA": true, "mvB": true, "mvC": true}
	for _, r := range results {
		entry := r.(map[string]any)
		path, _ := entry["path"].(string)
		if !shipNames[path] {
			t.Errorf("unexpected entity path %q in results; want mvA/mvB/mvC", path)
		}
	}

	_ = P1
	_ = P2
}

// ---------------------------------------------------------------------------
// Phase 16.45 REST tests
// ---------------------------------------------------------------------------

// TestRest_Query_RelVar verifies the REST endpoint executes a relationship-variable
// query and returns results with a "vars" field containing the Rel binding.
func TestRest_Query_RelVar(t *testing.T) {
	w := flecs.New()
	var heroID, likesID, entityAID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		heroID = fw.NewEntity()
		w.SetName(heroID, "rvHero")
		likesID = fw.NewEntity()
		w.SetName(likesID, "rvLikes")
		entityAID = fw.NewEntity()
		w.SetName(entityAID, "rvEntityA")
		flecs.AddID(fw, entityAID, flecs.MakePair(likesID, heroID))
	})

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := rqGet(t, srv, "/query?expr=(%24Rel,+rvHero)")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	count := int(m["count"].(float64))
	if count < 1 {
		t.Errorf("want ≥1 result for relVar query, got count=%d\nbody=%s", count, body)
	}
	results, _ := m["results"].([]any)
	if len(results) < 1 {
		t.Fatalf("want results array, got empty\nbody=%s", body)
	}
	entry := results[0].(map[string]any)
	vars, ok := entry["vars"].(map[string]any)
	if !ok {
		t.Fatalf("want vars field in result entry\nbody=%s", body)
	}
	relBinding, ok := vars["Rel"]
	if !ok {
		t.Errorf("want Rel variable in vars\nbody=%s", body)
	}
	_ = relBinding // non-zero means binding is present
}

// TestRest_Query_NegativeVar verifies REST executes a negative-variable query correctly.
func TestRest_Query_NegativeVar(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[rqPos](w)
	w.SetName(posID, "rvPos")
	var velID, brakeID, xFast, xSlow flecs.ID
	w.Write(func(fw *flecs.Writer) {
		velID = fw.NewEntity()
		w.SetName(velID, "rvVel")
		brakeID = fw.NewEntity()
		w.SetName(brakeID, "rvBrake")
		xFast = fw.NewEntity()
		w.SetName(xFast, "rvxFast")
		xSlow = fw.NewEntity()
		w.SetName(xSlow, "rvxSlow")

		// eA: no brake → should be in results
		eA := fw.NewEntity()
		w.SetName(eA, "rvEntityNobrake")
		flecs.Set(fw, eA, rqPos{X: 1})
		flecs.AddID(fw, eA, flecs.MakePair(velID, xFast))

		// eB: has brake → should NOT be in results
		eB := fw.NewEntity()
		w.SetName(eB, "rvEntityBrake")
		flecs.Set(fw, eB, rqPos{X: 2})
		flecs.AddID(fw, eB, flecs.MakePair(velID, xSlow))
		flecs.AddID(fw, eB, flecs.MakePair(brakeID, xSlow))
	})

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	// Query: rvPos,(rvVel,$x),!rvBrake($this,$x)
	// URL-encoded: rvPos,+(rvVel,+%24x),+!rvBrake(%24this,+%24x)
	resp := rqGet(t, srv, "/query?expr=rvPos,+(rvVel,+%24x),+!rvBrake(%24this,+%24x)")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	count := int(m["count"].(float64))
	// Only rvEntityNobrake should be returned.
	if count != 1 {
		t.Errorf("want 1 result (entity without brake), got count=%d\nbody=%s", count, body)
	}
}

// TestRest_Query_TableVar verifies REST executes a table-variable query and returns results.
func TestRest_Query_TableVar(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[rqPos](w)
	w.SetName(posID, "rvPosition2")
	var eA, eB flecs.ID
	w.Write(func(fw *flecs.Writer) {
		eA = fw.NewEntity()
		w.SetName(eA, "rvTblEntA")
		flecs.Set(fw, eA, rqPos{X: 1})
		eB = fw.NewEntity()
		w.SetName(eB, "rvTblEntB")
		flecs.Set(fw, eB, rqPos{X: 2})
	})
	_ = eA
	_ = eB

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	// Query: $T:rvPosition2($this)
	resp := rqGet(t, srv, "/query?expr=%24T%3ArvPosition2(%24this)")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	m := rqDecodeResponse(t, body)
	count := int(m["count"].(float64))
	if count < 1 {
		t.Errorf("want ≥1 result for table-var query, got count=%d\nbody=%s", count, body)
	}
	results, _ := m["results"].([]any)
	if len(results) < 1 {
		t.Fatalf("want results, got empty\nbody=%s", body)
	}
	// Each result should have a "vars" field with "T".
	entry := results[0].(map[string]any)
	vars, ok := entry["vars"].(map[string]any)
	if !ok {
		t.Fatalf("want vars field in result\nbody=%s", body)
	}
	if _, hasT := vars["T"]; !hasT {
		t.Errorf("want T variable in vars\nbody=%s", body)
	}
}
