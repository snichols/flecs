package flecs_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/snichols/flecs"
)

// Test-local types for type_info endpoint tests.
type tiPosition struct{ X, Y float64 }
type tiNested struct{ Inner tiPosition }
type tiMixed struct {
	P *int
	I interface{}
	S []byte
}
type tiUnit struct{ Value float64 }

// newTIWorld builds a world with all type_info test fixtures and returns
// the world and a running httptest.Server.
//
//   - "tiPosition"  – typed struct (two float64 fields)
//   - "DynComp"     – dynamic component (size 8, align 8)
//   - "BareEntity"  – alive entity with a Name but no TypeInfo
//   - "tiUnit"      – typed component with a unit attached
//   - "tiNested"    – struct with one nested-struct field
//   - "tiMixed"     – struct with pointer, interface, slice fields
func newTIWorld(t *testing.T) (*flecs.World, *httptest.Server) {
	t.Helper()
	w := flecs.New()

	// Register typed components then name their entities.
	posID := flecs.RegisterComponent[tiPosition](w)
	w.SetName(posID, "tiPosition")

	nestedID := flecs.RegisterComponent[tiNested](w)
	w.SetName(nestedID, "tiNested")

	mixedID := flecs.RegisterComponent[tiMixed](w)
	w.SetName(mixedID, "tiMixed")

	unitCompID := flecs.RegisterComponent[tiUnit](w)
	w.SetName(unitCompID, "tiUnit")

	w.Write(func(fw *flecs.Writer) {
		// Dynamic component: entity name matches the registered string.
		dynID := flecs.RegisterDynamicComponent(fw, "DynComp", 8, 8)
		w.SetName(dynID, "DynComp")

		// Bare entity: has a Name but no TypeInfo in the registry.
		bare := fw.NewEntity()
		w.SetName(bare, "BareEntity")

		// Attach a unit to tiUnit.
		unitID := flecs.RegisterUnit(fw, "Meter", "m", 0, 1.0)
		fw.SetUnit(unitCompID, unitID)
	})

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)
	return w, srv
}

// TestRESTTypeInfoTypedStruct: GET /type_info/tiPosition → 200 with X/Y fields.
func TestRESTTypeInfoTypedStruct(t *testing.T) {
	_, srv := newTIWorld(t)

	resp := restGet(t, srv, "/type_info/tiPosition")
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /type_info/tiPosition: want 200, got %d; body: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type want application/json, got %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "max-age=300" {
		t.Errorf("Cache-Control want max-age=300, got %q", cc)
	}

	var info struct {
		Size   uint64 `json:"size"`
		Align  uint64 `json:"align"`
		Fields []struct {
			Name   string `json:"name"`
			Type   string `json:"type"`
			Offset uint64 `json:"offset"`
		} `json:"fields"`
		Opaque bool   `json:"opaque"`
		Unit   string `json:"unit"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if info.Size != 16 {
		t.Errorf("size want 16, got %d", info.Size)
	}
	if info.Align != 8 {
		t.Errorf("align want 8, got %d", info.Align)
	}
	if len(info.Fields) != 2 {
		t.Fatalf("fields want 2, got %d; body: %s", len(info.Fields), body)
	}
	if info.Fields[0].Name != "X" || info.Fields[0].Type != "float64" || info.Fields[0].Offset != 0 {
		t.Errorf("fields[0] want {X float64 0}, got %+v", info.Fields[0])
	}
	if info.Fields[1].Name != "Y" || info.Fields[1].Type != "float64" || info.Fields[1].Offset != 8 {
		t.Errorf("fields[1] want {Y float64 8}, got %+v", info.Fields[1])
	}
	if info.Opaque {
		t.Error("opaque should be false for a typed struct")
	}
	if info.Unit != "" {
		t.Errorf("unit want empty, got %q", info.Unit)
	}
}

// TestRESTTypeInfoDynamicComponent: GET /type_info/DynComp → 200, opaque:true, fields:[].
func TestRESTTypeInfoDynamicComponent(t *testing.T) {
	_, srv := newTIWorld(t)

	resp := restGet(t, srv, "/type_info/DynComp")
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /type_info/DynComp: want 200, got %d; body: %s", resp.StatusCode, body)
	}

	var info struct {
		Size   uint64          `json:"size"`
		Align  uint64          `json:"align"`
		Fields json.RawMessage `json:"fields"`
		Opaque bool            `json:"opaque"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if info.Size != 8 {
		t.Errorf("size want 8, got %d", info.Size)
	}
	if info.Align != 8 {
		t.Errorf("align want 8, got %d", info.Align)
	}
	if !info.Opaque {
		t.Error("opaque want true for dynamic component")
	}
	if string(info.Fields) != "[]" {
		t.Errorf("fields want [], got %s", info.Fields)
	}
}

// TestRESTTypeInfoBareEntity: GET /type_info/BareEntity → 404 (no TypeInfo in registry).
func TestRESTTypeInfoBareEntity(t *testing.T) {
	_, srv := newTIWorld(t)

	resp := restGet(t, srv, "/type_info/BareEntity")
	readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /type_info/BareEntity: want 404, got %d", resp.StatusCode)
	}
}

// TestRESTTypeInfoNonexistent: GET /type_info/Nonexistent → 404 (entity not found).
func TestRESTTypeInfoNonexistent(t *testing.T) {
	_, srv := newTIWorld(t)

	resp := restGet(t, srv, "/type_info/Nonexistent")
	readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /type_info/Nonexistent: want 404, got %d", resp.StatusCode)
	}
}

// TestRESTTypeInfoWithUnit: GET /type_info/tiUnit → 200 with unit:"Meter".
func TestRESTTypeInfoWithUnit(t *testing.T) {
	_, srv := newTIWorld(t)

	resp := restGet(t, srv, "/type_info/tiUnit")
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /type_info/tiUnit: want 200, got %d; body: %s", resp.StatusCode, body)
	}

	var info struct {
		Unit string `json:"unit"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if info.Unit != "Meter" {
		t.Errorf("unit want %q, got %q", "Meter", info.Unit)
	}
}

// TestRESTTypeInfoNestedStruct: GET /type_info/tiNested → 200, depth-1 opaque rendering.
// The inner field type is the reflect string for tiPosition, not a nested fields object.
func TestRESTTypeInfoNestedStruct(t *testing.T) {
	_, srv := newTIWorld(t)

	resp := restGet(t, srv, "/type_info/tiNested")
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /type_info/tiNested: want 200, got %d; body: %s", resp.StatusCode, body)
	}

	var info struct {
		Fields []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(info.Fields) != 1 {
		t.Fatalf("fields want 1, got %d; body: %s", len(info.Fields), body)
	}
	if info.Fields[0].Name != "Inner" {
		t.Errorf("fields[0].name want Inner, got %q", info.Fields[0].Name)
	}
	if info.Fields[0].Type != "flecs_test.tiPosition" {
		t.Errorf("fields[0].type want flecs_test.tiPosition, got %q", info.Fields[0].Type)
	}
}

// TestRESTTypeInfoOpaqueFields: pointer / interface / slice fields render as type strings (no panic).
func TestRESTTypeInfoOpaqueFields(t *testing.T) {
	_, srv := newTIWorld(t)

	resp := restGet(t, srv, "/type_info/tiMixed")
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /type_info/tiMixed: want 200, got %d; body: %s", resp.StatusCode, body)
	}

	var info struct {
		Fields []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(info.Fields) != 3 {
		t.Fatalf("fields want 3, got %d; body: %s", len(info.Fields), body)
	}
	if info.Fields[0].Type != "*int" {
		t.Errorf("fields[0].type want *int, got %q", info.Fields[0].Type)
	}
	if info.Fields[1].Type != "interface {}" {
		t.Errorf("fields[1].type want interface {}, got %q", info.Fields[1].Type)
	}
	if info.Fields[2].Type != "[]uint8" {
		t.Errorf("fields[2].type want []uint8, got %q", info.Fields[2].Type)
	}
}

// TestRESTTypeInfoConcurrent: 10 goroutines × 100 iterations under -race → no race, all 200.
func TestRESTTypeInfoConcurrent(t *testing.T) {
	_, srv := newTIWorld(t)

	const goroutines = 10
	const iters = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range iters {
				resp := restGet(t, srv, "/type_info/tiPosition")
				body := readBody(t, resp)
				if resp.StatusCode != http.StatusOK {
					t.Errorf("concurrent GET /type_info/tiPosition: want 200, got %d; body: %s", resp.StatusCode, body)
				}
			}
		}()
	}
	wg.Wait()
}

// TestRESTTypeInfoCacheControl: Cache-Control: max-age=300 is present on 200 responses.
func TestRESTTypeInfoCacheControl(t *testing.T) {
	_, srv := newTIWorld(t)

	resp := restGet(t, srv, "/type_info/tiPosition")
	readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "max-age=300" {
		t.Errorf("Cache-Control want max-age=300, got %q", cc)
	}
}
