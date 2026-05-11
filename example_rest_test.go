package flecs_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/snichols/flecs"
)

type examplePosition struct{ X, Y float32 }

// ExampleNewRESTHandler demonstrates wiring a World into an HTTP server and
// querying the /stats endpoint.
func ExampleNewRESTHandler() {
	w := flecs.New()
	flecs.RegisterComponent[examplePosition](w)

	e := w.NewEntity()
	w.SetName(e, "hero")
	flecs.Set(w, e, examplePosition{X: 10, Y: 20})

	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/stats")
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	var stats flecs.Stats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		panic(err)
	}

	fmt.Println("status:", resp.StatusCode)
	fmt.Println("ok:", stats.EntityCount > 0)

	// Output:
	// status: 200
	// ok: true
}

// ExampleNewRESTHandler_methodNotAllowed shows that the handler returns 405
// for an unsupported method on a known route.
func ExampleNewRESTHandler_methodNotAllowed() {
	w := flecs.New()
	srv := httptest.NewServer(flecs.NewRESTHandler(w))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/stats", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		panic(err)
	}
	resp.Body.Close()

	fmt.Println("status:", resp.StatusCode)

	// Output:
	// status: 405
}
