package flecs_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/snichols/flecs"
)

// ---------------------------------------------------------------------------
// Shared test-local types for type_info endpoint tests.
// ---------------------------------------------------------------------------

// Flat struct — two exported float64 fields.
type tiPosition struct{ X, Y float64 }

// Nested struct — one level deep.
type tiNested struct{ Inner tiPosition }

// Mixed opaque fields — pointer, interface, slice.
type tiMixed struct {
	P *int
	I interface{}
	S []byte
}

// Unit-annotated component.
type tiUnit struct{ Value float64 }

// ---------------------------------------------------------------------------
// Depth-N test types.
// ---------------------------------------------------------------------------

// 8-level deep nesting — used for DepthDefault8 and DepthExplicit3.
type (
	tiDeep8_L8 struct{ Val int32 }
	tiDeep8_L7 struct{ L tiDeep8_L8 }
	tiDeep8_L6 struct{ L tiDeep8_L7 }
	tiDeep8_L5 struct{ L tiDeep8_L6 }
	tiDeep8_L4 struct{ L tiDeep8_L5 }
	tiDeep8_L3 struct{ L tiDeep8_L4 }
	tiDeep8_L2 struct{ L tiDeep8_L3 }
	tiDeep8_L1 struct{ L tiDeep8_L2 } // registered as the component
)

// Self-referential cycle — Node.Next *Node.
type tiNode struct {
	Next  *tiNode
	Value int
}

// Mutual recursion — A ↔ B.
type tiMutA struct{ B *tiMutB }
type tiMutB struct{ A *tiMutA }

// Sibling reuse — L and R both point to the same Sub type (NOT a cycle).
type tiSub struct{ X float32 }
type tiSibling struct{ L, R *tiSub }

// Named primitive alias.
type tiScore int32
type tiNamedPrim struct{ Score tiScore }

// Containers — slice, array, map.
type tiContainers struct {
	Slice []string
	Arr   [5]int
	Map   map[string]int
}

// Interface field.
type tiWithInterface struct{ Any interface{} }

// byte/rune alias fields — indistinguishable from uint8/int32 at reflect level.
type tiByteRune struct {
	B byte
	R rune
}

// Opaque channel and func fields — reported by kind string, no recursion.
type tiOpaqueKinds struct {
	Ch chan int
	Fn func()
}

// All scalar primitive kinds.
type tiAllPrimitives struct {
	Bo  bool
	I   int
	I8  int8
	I16 int16
	I32 int32
	I64 int64
	U   uint
	U8  uint8
	U16 uint16
	U32 uint32
	U64 uint64
	F32 float32
	F64 float64
	Str string
}

// Pointer field that points to a struct.
type tiVec3 struct{ X, Y, Z float32 }
type tiPointer struct{ Pos *tiVec3 }

// ---------------------------------------------------------------------------
// newTIWorld — shared world for pre-existing tests.
// ---------------------------------------------------------------------------

// newTIWorld builds a world with the classic type_info test fixtures.
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

	posID := flecs.RegisterComponent[tiPosition](w)
	w.SetName(posID, "tiPosition")

	nestedID := flecs.RegisterComponent[tiNested](w)
	w.SetName(nestedID, "tiNested")

	mixedID := flecs.RegisterComponent[tiMixed](w)
	w.SetName(mixedID, "tiMixed")

	unitCompID := flecs.RegisterComponent[tiUnit](w)
	w.SetName(unitCompID, "tiUnit")

	w.Write(func(fw *flecs.Writer) {
		dynID := flecs.RegisterDynamicComponent(fw, "DynComp", 8, 8)
		w.SetName(dynID, "DynComp")

		bare := fw.NewEntity()
		w.SetName(bare, "BareEntity")

		unitID := flecs.RegisterUnit(fw, "Meter", "m", 0, 1.0)
		fw.SetUnit(unitCompID, unitID)
	})

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)
	return w, srv
}

// ---------------------------------------------------------------------------
// Helper — assert a JSON field within a raw JSON blob.
// ---------------------------------------------------------------------------

func jsonString(t *testing.T, data []byte, field string) string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("jsonString(%q): unmarshal: %v; body: %s", field, err, data)
	}
	raw, ok := m[field]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return string(raw) // return as-is for non-string fields
	}
	return s
}

// ---------------------------------------------------------------------------
// Pre-existing tests (updated for new default schema where necessary).
// ---------------------------------------------------------------------------

// TestRESTTypeInfoTypedStruct: GET /type_info/tiPosition?depth=1 → 200 with X/Y fields.
// Uses ?depth=1 to pin against the v0.87.0 schema (name/type/offset per field).
func TestRESTTypeInfoTypedStruct(t *testing.T) {
	_, srv := newTIWorld(t)

	resp := restGet(t, srv, "/type_info/tiPosition?depth=1")
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /type_info/tiPosition?depth=1: want 200, got %d; body: %s", resp.StatusCode, body)
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

// TestRESTTypeInfoDynamicComponent: GET /type_info/DynComp → 200, kind:"dynamic", no fields.
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
		Kind   string          `json:"kind"`
		Fields json.RawMessage `json:"fields"`
		Opaque *bool           `json:"opaque"`
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
	if info.Kind != "dynamic" {
		t.Errorf("kind want \"dynamic\", got %q", info.Kind)
	}
	if info.Fields != nil {
		t.Errorf("fields want absent/null for dynamic component, got %s", info.Fields)
	}
	if info.Opaque != nil {
		t.Errorf("opaque want absent for new schema, got %v", *info.Opaque)
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
	if info.Unit != "m" {
		t.Errorf("unit want %q, got %q", "m", info.Unit)
	}
}

// TestRESTTypeInfoNestedStruct: GET /type_info/tiNested → 200, Inner field has type tiPosition.
// With default depth=8 the nested struct is fully expanded; the "type" field of the
// Inner node still carries the reflect type string "flecs_test.tiPosition".
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

// TestRESTTypeInfoOpaqueFields: ?depth=1 — pointer/interface/slice fields render as type strings.
func TestRESTTypeInfoOpaqueFields(t *testing.T) {
	_, srv := newTIWorld(t)

	resp := restGet(t, srv, "/type_info/tiMixed?depth=1")
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /type_info/tiMixed?depth=1: want 200, got %d; body: %s", resp.StatusCode, body)
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

// ---------------------------------------------------------------------------
// New Phase 16.39 tests.
// ---------------------------------------------------------------------------

// drillFields recursively drills into the first field's "fields" array n times,
// returning the json.RawMessage of that level.
func drillFields(t *testing.T, data json.RawMessage, n int) json.RawMessage {
	t.Helper()
	cur := data
	for range n {
		var node struct {
			Fields []json.RawMessage `json:"fields"`
		}
		if err := json.Unmarshal(cur, &node); err != nil {
			t.Fatalf("drillFields: unmarshal: %v; data: %s", err, cur)
		}
		if len(node.Fields) == 0 {
			t.Fatalf("drillFields: fields empty at depth %d; data: %s", n, cur)
		}
		cur = node.Fields[0]
	}
	return cur
}

// TestRest_TypeInfo_DepthDefault8: default request (no ?depth=) expands all 8 struct levels.
func TestRest_TypeInfo_DepthDefault8(t *testing.T) {
	w := flecs.New()
	id := flecs.RegisterComponent[tiDeep8_L1](w)
	w.SetName(id, "tiDeep8_L1")
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := restGet(t, srv, "/type_info/tiDeep8_L1")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", resp.StatusCode, body)
	}

	var root struct {
		Kind   string            `json:"kind"`
		Fields []json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if root.Kind != "struct" {
		t.Errorf("root.kind want struct, got %q", root.Kind)
	}
	if len(root.Fields) == 0 {
		t.Fatalf("root.fields empty; body: %s", body)
	}

	// Drill: L1.L → L2.L → L3.L → L4.L → L5.L → L6.L → L7.L → L8 node
	// From root (already L1 fields), drill 7 more levels.
	// root.Fields[0] is L2 (tiDeep8_L2)
	l8Node := drillFields(t, root.Fields[0], 7) // L2→L3→L4→L5→L6→L7→L8→L8.fields[0]=Val
	// l8Node is the Val field of tiDeep8_L8
	var val struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(l8Node, &val); err != nil {
		t.Fatalf("unmarshal val node: %v; node: %s", err, l8Node)
	}
	if val.Name != "Val" || val.Kind != "primitive" || val.Type != "int32" {
		t.Errorf("Val field: want {Val primitive int32}, got {%s %s %s}", val.Name, val.Kind, val.Type)
	}
}

// TestRest_TypeInfo_DepthExplicit3: ?depth=3 expands levels 1-3, elides level 4.
func TestRest_TypeInfo_DepthExplicit3(t *testing.T) {
	w := flecs.New()
	id := flecs.RegisterComponent[tiDeep8_L1](w)
	w.SetName(id, "tiDeep8_L1")
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := restGet(t, srv, "/type_info/tiDeep8_L1?depth=3")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", resp.StatusCode, body)
	}

	var root struct {
		Fields []struct {
			Name   string `json:"name"` // L2 (tiDeep8_L2)
			Fields []struct {
				Name   string `json:"name"` // L3 (tiDeep8_L3)
				Fields []struct {
					Name   string          `json:"name"` // L4 — elided, no sub-fields
					Fields json.RawMessage `json:"fields"`
				} `json:"fields"`
			} `json:"fields"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(root.Fields) == 0 {
		t.Fatalf("level-1 fields empty; body: %s", body)
	}
	l2 := root.Fields[0]
	if l2.Name != "L" {
		t.Errorf("level-2 field name want L, got %q", l2.Name)
	}
	if len(l2.Fields) == 0 {
		t.Fatalf("level-2 fields not expanded; body: %s", body)
	}
	l3 := l2.Fields[0]
	if len(l3.Fields) == 0 {
		t.Fatalf("level-3 fields not expanded; body: %s", body)
	}
	l4 := l3.Fields[0]
	// Level 4 should be elided: the node exists but has no fields.
	if len(l4.Fields) != 0 {
		t.Errorf("level-4 should be elided (no sub-fields), got %s", l4.Fields)
	}
}

// TestRest_TypeInfo_Depth0: ?depth=0 returns only the top-level header (no fields key).
func TestRest_TypeInfo_Depth0(t *testing.T) {
	_, srv := newTIWorld(t)

	resp := restGet(t, srv, "/type_info/tiPosition?depth=0")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", resp.StatusCode, body)
	}

	var info struct {
		Name   string          `json:"name"`
		ID     string          `json:"id"`
		Size   uint64          `json:"size"`
		Align  uint64          `json:"align"`
		Kind   string          `json:"kind"`
		Fields json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if info.Kind != "struct" {
		t.Errorf("kind want struct, got %q", info.Kind)
	}
	if info.Size == 0 {
		t.Error("size want non-zero")
	}
	if info.Fields != nil {
		t.Errorf("fields want absent at depth=0, got %s", info.Fields)
	}
}

// TestRest_TypeInfo_Depth1_BackCompat: ?depth=1 produces byte-identical JSON to v0.87.0.
// v0.87.0 schema: {"name":"...","size":N,"align":N,"fields":[{"name":"...","type":"...","offset":N},...]}
func TestRest_TypeInfo_Depth1_BackCompat(t *testing.T) {
	_, srv := newTIWorld(t)

	resp := restGet(t, srv, "/type_info/tiPosition?depth=1")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", resp.StatusCode, body)
	}

	// The v0.87.0 output for tiPosition (two float64 fields at offsets 0 and 8).
	// Field order follows struct declaration order; json.Encoder appends a newline.
	want := `{"name":"flecs_test.tiPosition","size":16,"align":8,"fields":[{"name":"X","type":"float64","offset":0},{"name":"Y","type":"float64","offset":8}]}` + "\n"
	if !bytes.Equal(body, []byte(want)) {
		t.Errorf("depth=1 response not byte-identical to v0.87.0\ngot:  %s\nwant: %s", body, want)
	}
}

// TestRest_TypeInfo_DepthInvalid: invalid depth values → 400 Bad Request.
func TestRest_TypeInfo_DepthInvalid(t *testing.T) {
	_, srv := newTIWorld(t)

	cases := []string{
		"/type_info/tiPosition?depth=-1",
		"/type_info/tiPosition?depth=99",
		"/type_info/tiPosition?depth=abc",
	}
	for _, path := range cases {
		resp := restGet(t, srv, path)
		readBody(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("GET %s: want 400, got %d", path, resp.StatusCode)
		}
	}
}

// TestRest_TypeInfo_DepthLimit16Enforced: ?depth=17 → 400; ?depth=16 → 200.
func TestRest_TypeInfo_DepthLimit16Enforced(t *testing.T) {
	_, srv := newTIWorld(t)

	resp17 := restGet(t, srv, "/type_info/tiPosition?depth=17")
	readBody(t, resp17)
	if resp17.StatusCode != http.StatusBadRequest {
		t.Errorf("depth=17: want 400, got %d", resp17.StatusCode)
	}

	resp16 := restGet(t, srv, "/type_info/tiPosition?depth=16")
	readBody(t, resp16)
	if resp16.StatusCode != http.StatusOK {
		t.Errorf("depth=16: want 200, got %d", resp16.StatusCode)
	}
}

// TestRest_TypeInfo_RecursiveType: Node.Next *Node — cycle detected, recursive:true emitted.
func TestRest_TypeInfo_RecursiveType(t *testing.T) {
	w := flecs.New()
	id := flecs.RegisterComponent[tiNode](w)
	w.SetName(id, "tiNode")
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := restGet(t, srv, "/type_info/tiNode")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", resp.StatusCode, body)
	}

	// Expect something like:
	//   fields: [{name:"Next", kind:"pointer", element:{kind:"struct", type:"..tiNode..", recursive:true}},
	//            {name:"Value", kind:"primitive", type:"int"}]
	var root struct {
		Fields []struct {
			Name    string `json:"name"`
			Kind    string `json:"kind"`
			Element *struct {
				Kind      string `json:"kind"`
				Type      string `json:"type"`
				Recursive bool   `json:"recursive"`
			} `json:"element"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(root.Fields) < 1 {
		t.Fatalf("fields want >= 1, got %d; body: %s", len(root.Fields), body)
	}
	next := root.Fields[0]
	if next.Name != "Next" || next.Kind != "pointer" {
		t.Errorf("fields[0]: want {Next pointer}, got {%s %s}", next.Name, next.Kind)
	}
	if next.Element == nil {
		t.Fatal("fields[0].element is nil")
	}
	if !next.Element.Recursive {
		t.Errorf("fields[0].element.recursive want true; body: %s", body)
	}
	if !strings.Contains(next.Element.Type, "tiNode") {
		t.Errorf("fields[0].element.type want to contain tiNode, got %q", next.Element.Type)
	}
}

// TestRest_TypeInfo_MutualRecursion: A ↔ B — cycles detected from both sides.
func TestRest_TypeInfo_MutualRecursion(t *testing.T) {
	w := flecs.New()
	aID := flecs.RegisterComponent[tiMutA](w)
	w.SetName(aID, "tiMutA")
	bID := flecs.RegisterComponent[tiMutB](w)
	w.SetName(bID, "tiMutB")
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	findRecursive := func(t *testing.T, body []byte) bool {
		t.Helper()
		return strings.Contains(string(body), `"recursive":true`)
	}

	respA := restGet(t, srv, "/type_info/tiMutA")
	bodyA := readBody(t, respA)
	if respA.StatusCode != http.StatusOK {
		t.Fatalf("tiMutA: want 200, got %d", respA.StatusCode)
	}
	if !findRecursive(t, bodyA) {
		t.Errorf("tiMutA: expected recursive:true in body; body: %s", bodyA)
	}

	respB := restGet(t, srv, "/type_info/tiMutB")
	bodyB := readBody(t, respB)
	if respB.StatusCode != http.StatusOK {
		t.Fatalf("tiMutB: want 200, got %d", respB.StatusCode)
	}
	if !findRecursive(t, bodyB) {
		t.Errorf("tiMutB: expected recursive:true in body; body: %s", bodyB)
	}
}

// TestRest_TypeInfo_SiblingTypeReuse: T{L *Sub; R *Sub} — both siblings expand fully (not recursive).
func TestRest_TypeInfo_SiblingTypeReuse(t *testing.T) {
	w := flecs.New()
	id := flecs.RegisterComponent[tiSibling](w)
	w.SetName(id, "tiSibling")
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := restGet(t, srv, "/type_info/tiSibling")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", resp.StatusCode, body)
	}

	if strings.Contains(string(body), `"recursive":true`) {
		t.Errorf("siblings should not be marked recursive; body: %s", body)
	}

	// Both L and R fields should expand tiSub (has field X float32).
	var root struct {
		Fields []struct {
			Name    string `json:"name"`
			Kind    string `json:"kind"`
			Element *struct {
				Kind   string `json:"kind"`
				Fields []struct {
					Name string `json:"name"`
					Kind string `json:"kind"`
					Type string `json:"type"`
				} `json:"fields"`
			} `json:"element"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(root.Fields) != 2 {
		t.Fatalf("want 2 fields (L, R), got %d; body: %s", len(root.Fields), body)
	}
	for _, f := range root.Fields {
		if f.Kind != "pointer" {
			t.Errorf("field %s: want pointer, got %s", f.Name, f.Kind)
			continue
		}
		if f.Element == nil || len(f.Element.Fields) == 0 {
			t.Errorf("field %s element not expanded; body: %s", f.Name, body)
			continue
		}
		x := f.Element.Fields[0]
		if x.Name != "X" || x.Kind != "primitive" || x.Type != "float32" {
			t.Errorf("field %s.element.fields[0]: want {X primitive float32}, got {%s %s %s}", f.Name, x.Name, x.Kind, x.Type)
		}
	}
}

// TestRest_TypeInfo_PrimitiveAnnotations: each primitive Go kind reports the exact kind name.
func TestRest_TypeInfo_PrimitiveAnnotations(t *testing.T) {
	w := flecs.New()
	id := flecs.RegisterComponent[tiAllPrimitives](w)
	w.SetName(id, "tiAllPrimitives")
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := restGet(t, srv, "/type_info/tiAllPrimitives")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", resp.StatusCode, body)
	}

	var root struct {
		Fields []struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
			Type string `json:"type"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}

	want := map[string]string{
		"Bo":  "bool",
		"I":   "int",
		"I8":  "int8",
		"I16": "int16",
		"I32": "int32",
		"I64": "int64",
		"U":   "uint",
		"U8":  "uint8",
		"U16": "uint16",
		"U32": "uint32",
		"U64": "uint64",
		"F32": "float32",
		"F64": "float64",
		"Str": "string",
	}
	for _, f := range root.Fields {
		if f.Kind != "primitive" {
			t.Errorf("field %s: kind want primitive, got %q", f.Name, f.Kind)
		}
		if exp, ok := want[f.Name]; ok {
			if f.Type != exp {
				t.Errorf("field %s: type want %q, got %q", f.Name, exp, f.Type)
			}
		}
	}
}

// TestRest_TypeInfo_ByteAndRune: byte and rune are type aliases for uint8/int32;
// reflection cannot distinguish them, so they are reported as "uint8" and "int32".
func TestRest_TypeInfo_ByteAndRune(t *testing.T) {
	w := flecs.New()
	id := flecs.RegisterComponent[tiByteRune](w)
	w.SetName(id, "tiByteRune")
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := restGet(t, srv, "/type_info/tiByteRune")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", resp.StatusCode, body)
	}

	var root struct {
		Fields []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(root.Fields) != 2 {
		t.Fatalf("want 2 fields, got %d; body: %s", len(root.Fields), body)
	}
	// byte == uint8 at reflect level.
	if root.Fields[0].Name != "B" || root.Fields[0].Type != "uint8" {
		t.Errorf("B field: want {B uint8}, got {%s %s}", root.Fields[0].Name, root.Fields[0].Type)
	}
	// rune == int32 at reflect level.
	if root.Fields[1].Name != "R" || root.Fields[1].Type != "int32" {
		t.Errorf("R field: want {R int32}, got {%s %s}", root.Fields[1].Name, root.Fields[1].Type)
	}
}

// TestRest_TypeInfo_NamedPrimitive: type tiScore int32 → "type":"..tiScore..", "underlying":"int32".
func TestRest_TypeInfo_NamedPrimitive(t *testing.T) {
	w := flecs.New()
	id := flecs.RegisterComponent[tiNamedPrim](w)
	w.SetName(id, "tiNamedPrim")
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := restGet(t, srv, "/type_info/tiNamedPrim")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", resp.StatusCode, body)
	}

	var root struct {
		Fields []struct {
			Name       string `json:"name"`
			Kind       string `json:"kind"`
			Type       string `json:"type"`
			Underlying string `json:"underlying"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(root.Fields) != 1 {
		t.Fatalf("want 1 field, got %d; body: %s", len(root.Fields), body)
	}
	f := root.Fields[0]
	if f.Name != "Score" {
		t.Errorf("field name want Score, got %q", f.Name)
	}
	if f.Kind != "primitive" {
		t.Errorf("field kind want primitive, got %q", f.Kind)
	}
	if !strings.Contains(f.Type, "tiScore") {
		t.Errorf("field type want to contain tiScore, got %q", f.Type)
	}
	if f.Underlying != "int32" {
		t.Errorf("field underlying want int32, got %q", f.Underlying)
	}
}

// TestRest_TypeInfo_SliceArrayMap: containers report their element/key/value types.
func TestRest_TypeInfo_SliceArrayMap(t *testing.T) {
	w := flecs.New()
	id := flecs.RegisterComponent[tiContainers](w)
	w.SetName(id, "tiContainers")
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := restGet(t, srv, "/type_info/tiContainers")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", resp.StatusCode, body)
	}

	var root struct {
		Fields []struct {
			Name    string `json:"name"`
			Kind    string `json:"kind"`
			Length  int    `json:"length"`
			Element *struct {
				Kind string `json:"kind"`
				Type string `json:"type"`
			} `json:"element"`
			Key *struct {
				Kind string `json:"kind"`
				Type string `json:"type"`
			} `json:"key"`
			Value *struct {
				Kind string `json:"kind"`
				Type string `json:"type"`
			} `json:"value"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(root.Fields) != 3 {
		t.Fatalf("want 3 fields, got %d; body: %s", len(root.Fields), body)
	}

	slice := root.Fields[0]
	if slice.Name != "Slice" || slice.Kind != "slice" {
		t.Errorf("Slice field: want {Slice slice}, got {%s %s}", slice.Name, slice.Kind)
	}
	if slice.Element == nil || slice.Element.Type != "string" {
		t.Errorf("Slice.element want {primitive string}, got %+v", slice.Element)
	}

	arr := root.Fields[1]
	if arr.Name != "Arr" || arr.Kind != "array" {
		t.Errorf("Arr field: want {Arr array}, got {%s %s}", arr.Name, arr.Kind)
	}
	if arr.Length != 5 {
		t.Errorf("Arr.length want 5, got %d", arr.Length)
	}
	if arr.Element == nil || arr.Element.Type != "int" {
		t.Errorf("Arr.element want {primitive int}, got %+v", arr.Element)
	}

	m := root.Fields[2]
	if m.Name != "Map" || m.Kind != "map" {
		t.Errorf("Map field: want {Map map}, got {%s %s}", m.Name, m.Kind)
	}
	if m.Key == nil || m.Key.Type != "string" {
		t.Errorf("Map.key want {primitive string}, got %+v", m.Key)
	}
	if m.Value == nil || m.Value.Type != "int" {
		t.Errorf("Map.value want {primitive int}, got %+v", m.Value)
	}
}

// TestRest_TypeInfo_Pointer: *Vec3 field expands its target struct.
func TestRest_TypeInfo_Pointer(t *testing.T) {
	w := flecs.New()
	id := flecs.RegisterComponent[tiPointer](w)
	w.SetName(id, "tiPointer")
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := restGet(t, srv, "/type_info/tiPointer")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", resp.StatusCode, body)
	}

	var root struct {
		Fields []struct {
			Name    string `json:"name"`
			Kind    string `json:"kind"`
			Element *struct {
				Kind   string `json:"kind"`
				Type   string `json:"type"`
				Fields []struct {
					Name string `json:"name"`
					Kind string `json:"kind"`
					Type string `json:"type"`
				} `json:"fields"`
			} `json:"element"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(root.Fields) != 1 {
		t.Fatalf("want 1 field, got %d; body: %s", len(root.Fields), body)
	}
	pos := root.Fields[0]
	if pos.Name != "Pos" || pos.Kind != "pointer" {
		t.Errorf("Pos field: want {Pos pointer}, got {%s %s}", pos.Name, pos.Kind)
	}
	if pos.Element == nil || pos.Element.Kind != "struct" {
		t.Errorf("Pos.element: want struct, got %+v", pos.Element)
	}
	if len(pos.Element.Fields) != 3 {
		t.Errorf("Pos.element.fields want 3 (X Y Z), got %d; body: %s", len(pos.Element.Fields), body)
	}
}

// TestRest_TypeInfo_Interface: interface{} field reports kind:"interface" with no expansion.
func TestRest_TypeInfo_Interface(t *testing.T) {
	w := flecs.New()
	id := flecs.RegisterComponent[tiWithInterface](w)
	w.SetName(id, "tiWithInterface")
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := restGet(t, srv, "/type_info/tiWithInterface")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", resp.StatusCode, body)
	}

	var root struct {
		Fields []struct {
			Name    string          `json:"name"`
			Kind    string          `json:"kind"`
			Element json.RawMessage `json:"element"`
			Fields  json.RawMessage `json:"fields"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(root.Fields) != 1 {
		t.Fatalf("want 1 field, got %d; body: %s", len(root.Fields), body)
	}
	f := root.Fields[0]
	if f.Name != "Any" || f.Kind != "interface" {
		t.Errorf("Any field: want {Any interface}, got {%s %s}", f.Name, f.Kind)
	}
	if f.Element != nil {
		t.Errorf("interface field should have no element, got %s", f.Element)
	}
	if f.Fields != nil {
		t.Errorf("interface field should have no fields, got %s", f.Fields)
	}
}

// TestRest_TypeInfo_DynamicComponentV2: dynamic component returns kind:"dynamic" in new schema.
func TestRest_TypeInfo_DynamicComponentV2(t *testing.T) {
	_, srv := newTIWorld(t)

	resp := restGet(t, srv, "/type_info/DynComp?depth=0")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", resp.StatusCode, body)
	}
	if jsonString(t, body, "kind") != "dynamic" {
		t.Errorf("kind want dynamic; body: %s", body)
	}
}

// TestRest_TypeInfo_OpaqueKinds: chan and func fields report their kind string, no recursion.
func TestRest_TypeInfo_OpaqueKinds(t *testing.T) {
	w := flecs.New()
	id := flecs.RegisterComponent[tiOpaqueKinds](w)
	w.SetName(id, "tiOpaqueKinds")
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	resp := restGet(t, srv, "/type_info/tiOpaqueKinds")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", resp.StatusCode, body)
	}

	var root struct {
		Fields []struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(root.Fields) != 2 {
		t.Fatalf("want 2 fields, got %d; body: %s", len(root.Fields), body)
	}
	if root.Fields[0].Name != "Ch" || root.Fields[0].Kind != "chan" {
		t.Errorf("Ch field: want {Ch chan}, got {%s %s}", root.Fields[0].Name, root.Fields[0].Kind)
	}
	if root.Fields[1].Name != "Fn" || root.Fields[1].Kind != "func" {
		t.Errorf("Fn field: want {Fn func}, got {%s %s}", root.Fields[1].Name, root.Fields[1].Kind)
	}
}

// TestRest_TypeInfo_UnitAnnotation_Nested: component-level unit annotation is preserved
// in the new depth-N response schema.
func TestRest_TypeInfo_UnitAnnotation_Nested(t *testing.T) {
	_, srv := newTIWorld(t)

	// Request tiUnit with new default schema (depth=8).
	resp := restGet(t, srv, "/type_info/tiUnit")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", resp.StatusCode, body)
	}

	var info struct {
		Kind string `json:"kind"`
		Unit string `json:"unit"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if info.Kind != "struct" {
		t.Errorf("kind want struct, got %q", info.Kind)
	}
	if info.Unit != "m" {
		t.Errorf("unit want m, got %q", info.Unit)
	}
}

// tiForce is the component type used to test compound-unit annotation via the V2 REST path.
type tiForce struct{ N float32 }

// TestRest_TypeInfo_CompoundUnit_V2: V2 /type_info endpoint returns compound symbol (not entity name).
// Verifies the fix for the V2 path which previously used fr.GetName (entity name "NewtonCompound")
// instead of w.UnitSymbol (symbol "N").
func TestRest_TypeInfo_CompoundUnit_V2(t *testing.T) {
	w := flecs.New()
	forceID := flecs.RegisterComponent[tiForce](w)
	w.SetName(forceID, "tiForce")
	w.Write(func(fw *flecs.Writer) {
		fw.SetUnit(forceID, w.NewtonCompound())
	})
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	t.Cleanup(srv.Close)

	// Default (no ?depth=) → V2 path.
	resp := restGet(t, srv, "/type_info/tiForce")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /type_info/tiForce: want 200, got %d; body: %s", resp.StatusCode, body)
	}

	var info struct {
		Unit string `json:"unit"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	// Must be the compound symbol "N", not the entity name "NewtonCompound".
	if info.Unit != "N" {
		t.Errorf("unit want \"N\" (compound symbol), got %q", info.Unit)
	}
}
