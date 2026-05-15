package flecs_test

import (
	"expvar"
	"net/http"
	"net/http/httptest"

	"github.com/snichols/flecs"
)

// ExamplePublishExpvar demonstrates publishing world stats to /debug/vars and
// mounting expvar.Handler() on an HTTP server.
func ExamplePublishExpvar() {
	w := flecs.New()

	// Register stats under the "myapp" prefix.
	// This creates: myapp, myapp.entity_count, myapp.frame_count, etc.
	h := flecs.PublishExpvar(w, "myapp")
	_ = h // call h.Unpublish() to null-out vars on shutdown

	// Run a tick to populate stats.
	w.Progress(0.016)

	// Mount expvar.Handler() on your HTTP server to expose /debug/vars.
	mux := http.NewServeMux()
	mux.Handle("/debug/vars", expvar.Handler())

	// In a real application, use http.ListenAndServe:
	//   log.Fatal(http.ListenAndServe(":8080", mux))
	//
	// In this example, use httptest to verify the endpoint works.
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/debug/vars") //nolint:noctx
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	// Output:
}

// ExampleExpvarMap demonstrates obtaining an *expvar.Map without touching the
// global expvar registry — useful for custom mounting or testing.
func ExampleExpvarMap() {
	w := flecs.New()
	w.Progress(0.016)

	// Get a live map without publishing to the global registry.
	m := flecs.ExpvarMap(w)

	// Mount it under a custom name.
	expvar.Publish("myapp_custom", m)

	// Each call to a map value re-reads the live world stats.
	_ = m.Get("entity_count").String()
	// Output:
}
