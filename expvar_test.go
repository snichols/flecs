package flecs_test

import (
	"encoding/json"
	"expvar"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	"go.uber.org/goleak"

	"github.com/snichols/flecs"
)

// expvarSeq generates unique prefixes per test invocation. The expvar global
// registry never clears registered names, so with -count=N each run must use
// a fresh prefix to avoid receiving an old handle from a previous run.
var expvarSeq atomic.Int64

func expvarPrefix(t *testing.T) string {
	t.Helper()
	n := expvarSeq.Add(1)
	return fmt.Sprintf("flecs_%s_%d", t.Name(), n)
}

// scrapeString calls String() on the named global expvar, failing if not found.
func scrapeString(t *testing.T, name string) string {
	t.Helper()
	v := expvar.Get(name)
	if v == nil {
		t.Fatalf("expvar %q not registered", name)
	}
	return v.String()
}

func TestPublishExpvar_VariablesAppear(t *testing.T) {
	w := flecs.New()
	prefix := expvarPrefix(t)
	h := flecs.PublishExpvar(w, prefix)
	if h == nil {
		t.Fatal("PublishExpvar returned nil handle")
	}

	vars := []string{
		prefix,
		prefix + ".entity_count",
		prefix + ".table_count",
		prefix + ".component_count",
		prefix + ".system_count",
		prefix + ".observer_count",
		prefix + ".frame_count",
		prefix + ".reclaimed_tables",
		prefix + ".last_progress_seconds",
		prefix + ".phases",
		prefix + ".window_second",
		prefix + ".window_minute",
		prefix + ".window_hour",
	}
	for _, name := range vars {
		if expvar.Get(name) == nil {
			t.Errorf("expvar %q not registered after PublishExpvar", name)
		}
	}
}

func TestPublishExpvar_ReflectsLiveState(t *testing.T) {
	w := flecs.New()
	prefix := expvarPrefix(t)
	flecs.PublishExpvar(w, prefix)

	// Create 10 entities, commit via Progress, check entity_count.
	var ids []flecs.ID
	for i := 0; i < 10; i++ {
		w.Write(func(fw *flecs.Writer) {
			ids = append(ids, fw.NewEntity())
		})
	}
	w.Progress(0.016)

	raw := scrapeString(t, prefix+".entity_count")
	var got int
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("entity_count not valid JSON int: %v (got %q)", err, raw)
	}
	if got < 10 {
		t.Errorf("entity_count after 10 creates: got %d, want >= 10", got)
	}
	before := got

	// Delete 5 entities, commit again.
	for i := 0; i < 5; i++ {
		e := ids[i]
		w.Write(func(fw *flecs.Writer) { fw.Delete(e) })
	}
	w.Progress(0.016)

	raw2 := scrapeString(t, prefix+".entity_count")
	var got2 int
	if err := json.Unmarshal([]byte(raw2), &got2); err != nil {
		t.Fatalf("entity_count after deletes not valid JSON int: %v", err)
	}
	if got2 >= before {
		t.Errorf("entity_count after 5 deletes: got %d, want < %d", got2, before)
	}
}

func TestPublishExpvar_PhasesJSON(t *testing.T) {
	w := flecs.New()
	prefix := expvarPrefix(t)
	flecs.PublishExpvar(w, prefix)

	w.Progress(0.016)

	raw := scrapeString(t, prefix+".phases")
	var phases map[string]float64
	if err := json.Unmarshal([]byte(raw), &phases); err != nil {
		t.Fatalf("phases not valid JSON object: %v (got %q)", err, raw)
	}

	expected := []string{"PreUpdate", "OnFixedUpdate", "OnUpdate", "PostUpdate"}
	for _, name := range expected {
		if _, ok := phases[name]; !ok {
			t.Errorf("phases JSON missing key %q; got keys: %v", name, phaseMapKeys(phases))
		}
	}
}

func phaseMapKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestPublishExpvar_WindowAggregates(t *testing.T) {
	w := flecs.New()
	prefix := expvarPrefix(t)
	flecs.PublishExpvar(w, prefix)

	// Drive 60 Progress ticks to populate the second window.
	for i := 0; i < 60; i++ {
		w.Write(func(fw *flecs.Writer) { fw.NewEntity() })
		w.Progress(0.016)
	}

	raw := scrapeString(t, prefix+".window_second")
	var agg flecs.WorldStatsAggregated
	if err := json.Unmarshal([]byte(raw), &agg); err != nil {
		t.Fatalf("window_second not valid JSON: %v (got %q)", err, raw)
	}
	// After 60 ticks with entity creation, FrameCount avg should be > 0.
	if agg.FrameCount.Avg <= 0 {
		t.Errorf("window_second.frame_count.avg: got %v, want > 0", agg.FrameCount.Avg)
	}
}

func TestPublishExpvar_IdempotentDoublePublish(t *testing.T) {
	w := flecs.New()
	// Use ONE unique prefix; call PublishExpvar twice within the same run.
	prefix := expvarPrefix(t)

	h1 := flecs.PublishExpvar(w, prefix)
	if h1 == nil {
		t.Fatal("first PublishExpvar returned nil")
	}

	// Second publish with same prefix must not panic.
	var h2 *flecs.ExpvarHandle
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("second PublishExpvar panicked: %v", r)
			}
		}()
		h2 = flecs.PublishExpvar(w, prefix)
	}()
	if h2 == nil {
		t.Fatal("second PublishExpvar returned nil")
	}
	if h1 != h2 {
		t.Errorf("second PublishExpvar returned a different handle; want the existing one")
	}
}

func TestPublishExpvar_Unpublish_EmitsNull(t *testing.T) {
	w := flecs.New()
	prefix := expvarPrefix(t)
	h := flecs.PublishExpvar(w, prefix)

	w.Progress(0.016)

	// Verify entity_count is non-null before unpublish.
	before := scrapeString(t, prefix+".entity_count")
	if before == "null" {
		t.Error("entity_count should not be null before Unpublish")
	}

	h.Unpublish()

	// After Unpublish all vars must emit null.
	for _, suffix := range []string{"", ".entity_count", ".table_count", ".frame_count"} {
		got := scrapeString(t, prefix+suffix)
		if got != "null" {
			t.Errorf("expvar %q after Unpublish: got %q, want \"null\"", prefix+suffix, got)
		}
	}

	// Name must still be registered (expvar has no deregister API).
	if expvar.Get(prefix+".entity_count") == nil {
		t.Error("expvar name deregistered after Unpublish; expvar has no deregister API")
	}
}

func TestExpvarMap_StandaloneMap(t *testing.T) {
	w := flecs.New()
	w.Progress(0.016)

	m := flecs.ExpvarMap(w)
	if m == nil {
		t.Fatal("ExpvarMap returned nil")
	}

	// Verify all expected keys are present and return valid JSON.
	keys := []string{
		"entity_count", "table_count", "component_count",
		"system_count", "observer_count", "frame_count",
		"reclaimed_tables", "last_progress_seconds",
		"phases", "window_second", "window_minute", "window_hour",
	}
	for _, k := range keys {
		v := m.Get(k)
		if v == nil {
			t.Errorf("ExpvarMap missing key %q", k)
			continue
		}
		// Call the Func to exercise the closure and verify valid JSON output.
		raw := v.String()
		if raw == "" {
			t.Errorf("ExpvarMap key %q returned empty string", k)
			continue
		}
		var out any
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Errorf("ExpvarMap key %q: not valid JSON %q: %v", k, raw, err)
		}
	}

	// Verify entity_count is a valid JSON int.
	ecRaw := m.Get("entity_count").String()
	var ec int
	if err := json.Unmarshal([]byte(ecRaw), &ec); err != nil {
		t.Errorf("entity_count not valid JSON int: %v (got %q)", err, ecRaw)
	}

	// Exercise the last_progress_seconds null branch (fresh world, no Progress).
	w2 := flecs.New()
	m2 := flecs.ExpvarMap(w2)
	lpsRaw := m2.Get("last_progress_seconds").String()
	if lpsRaw != "null" {
		t.Errorf("last_progress_seconds before Progress: got %q, want null", lpsRaw)
	}
}

func TestPublishExpvar_TwoWorlds_DistinctPrefixes(t *testing.T) {
	w1 := flecs.New()
	w2 := flecs.New()
	p1 := expvarPrefix(t)
	p2 := expvarPrefix(t)

	h1 := flecs.PublishExpvar(w1, p1)
	h2 := flecs.PublishExpvar(w2, p2)
	if h1 == nil || h2 == nil {
		t.Fatal("PublishExpvar returned nil for one of the worlds")
	}
	if h1 == h2 {
		t.Error("two distinct worlds/prefixes returned the same handle")
	}

	// Seed each world differently and verify vars are independent.
	for i := 0; i < 3; i++ {
		w1.Write(func(fw *flecs.Writer) { fw.NewEntity() })
	}
	w1.Progress(0.016)
	for i := 0; i < 7; i++ {
		w2.Write(func(fw *flecs.Writer) { fw.NewEntity() })
	}
	w2.Progress(0.016)

	raw1 := scrapeString(t, p1+".entity_count")
	raw2 := scrapeString(t, p2+".entity_count")
	if raw1 == raw2 {
		t.Errorf("two worlds share entity_count value %q; want distinct values", raw1)
	}
}

func TestPublishExpvar_ScrapeConsistency(t *testing.T) {
	w := flecs.New()
	prefix := expvarPrefix(t)
	flecs.PublishExpvar(w, prefix)

	for i := 0; i < 5; i++ {
		w.Write(func(fw *flecs.Writer) { fw.NewEntity() })
	}
	w.Progress(0.016)

	// The whole-tree var uses a single statsMu.RLock — scrape it multiple times
	// and verify each result is internally consistent JSON.
	for i := 0; i < 10; i++ {
		raw := scrapeString(t, prefix)
		var tree struct {
			EntityCount int    `json:"entity_count"`
			TableCount  int    `json:"table_count"`
			FrameCount  uint64 `json:"frame_count"`
			Phases      any    `json:"phases"`
		}
		if err := json.Unmarshal([]byte(raw), &tree); err != nil {
			t.Fatalf("iteration %d: whole-tree JSON invalid: %v\nraw: %s", i, err, raw)
		}
		if tree.EntityCount < 0 {
			t.Errorf("iteration %d: entity_count negative: %d", i, tree.EntityCount)
		}
		if tree.TableCount < 0 {
			t.Errorf("iteration %d: table_count negative: %d", i, tree.TableCount)
		}
		if tree.FrameCount == 0 {
			t.Errorf("iteration %d: frame_count should be > 0 after Progress", i)
		}
	}
}

func TestPublishExpvar_HTTPEndpoint(t *testing.T) {
	w := flecs.New()
	prefix := expvarPrefix(t)
	flecs.PublishExpvar(w, prefix)

	w.Progress(0.016)

	// Mount expvar.Handler() on an httptest server and verify the response.
	mux := http.NewServeMux()
	mux.Handle("/debug/vars", expvar.Handler())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/debug/vars") //nolint:noctx
	if err != nil {
		t.Fatalf("GET /debug/vars: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /debug/vars: status %d, want 200", resp.StatusCode)
	}

	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding /debug/vars JSON: %v", err)
	}

	// Verify our whole-tree var is present and valid.
	treeRaw, ok := body[prefix]
	if !ok {
		t.Errorf("expvar %q not in /debug/vars response", prefix)
	} else {
		var tree map[string]any
		if err := json.Unmarshal(treeRaw, &tree); err != nil {
			t.Errorf("whole-tree var is not valid JSON: %v (raw: %s)", err, treeRaw)
		}
		if _, ok := tree["entity_count"]; !ok {
			t.Errorf("whole-tree var missing entity_count key")
		}
	}

	// Verify individual scalar is present.
	if _, ok := body[prefix+".entity_count"]; !ok {
		t.Errorf("expvar %q not in /debug/vars response", prefix+".entity_count")
	}
}

func TestPublishExpvar_ConcurrentScrapeAndMutation(t *testing.T) {
	w := flecs.New()
	prefix := expvarPrefix(t)
	flecs.PublishExpvar(w, prefix)

	done := make(chan struct{})
	var wg sync.WaitGroup

	// Goroutine: repeatedly scrape entity_count and whole-tree var.
	// The expvar closures read via statsMu.RLock; statsCommit (called from
	// Progress on the main goroutine) writes via statsMu.Lock — no data race.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			if v := expvar.Get(prefix + ".entity_count"); v != nil {
				s := v.String()
				if s != "null" {
					var n int
					_ = json.Unmarshal([]byte(s), &n)
				}
			}
			if v := expvar.Get(prefix); v != nil {
				_ = v.String()
			}
		}
	}()

	// Main goroutine: alternate Write and Progress sequentially.
	for i := 0; i < 50; i++ {
		w.Write(func(fw *flecs.Writer) {
			fw.NewEntity()
		})
		w.Progress(0.016)
	}

	close(done)
	wg.Wait()
}

func TestPublishExpvar_NoBackgroundGoroutine(t *testing.T) {
	// Snapshot goroutines already running at test start.
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	w := flecs.New()
	prefix := expvarPrefix(t)
	h := flecs.PublishExpvar(w, prefix)

	// Scrape a few vars to exercise the lazy read path.
	w.Progress(0.016)
	for _, suffix := range []string{"", ".entity_count", ".frame_count"} {
		if v := expvar.Get(prefix + suffix); v != nil {
			_ = v.String()
		}
	}
	h.Unpublish()
	// goleak.VerifyNone fires in the deferred call above.
}

func TestPublishExpvar_ZeroStatsBeforeProgress(t *testing.T) {
	w := flecs.New()
	prefix := expvarPrefix(t)
	flecs.PublishExpvar(w, prefix)

	// No Progress call — all vars should be valid (zeros/nulls, no panics).
	vars := []string{
		prefix,
		prefix + ".entity_count",
		prefix + ".table_count",
		prefix + ".component_count",
		prefix + ".system_count",
		prefix + ".observer_count",
		prefix + ".frame_count",
		prefix + ".reclaimed_tables",
		prefix + ".last_progress_seconds",
		prefix + ".phases",
		prefix + ".window_second",
		prefix + ".window_minute",
		prefix + ".window_hour",
	}

	for _, name := range vars {
		v := expvar.Get(name)
		if v == nil {
			t.Errorf("expvar %q not registered", name)
			continue
		}
		// Must not panic; result must be valid JSON or "null".
		var raw string
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("expvar %q panicked: %v", name, r)
				}
			}()
			raw = v.String()
		}()
		if raw == "" {
			t.Errorf("expvar %q returned empty string", name)
			continue
		}
		var out any
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Errorf("expvar %q: not valid JSON %q: %v", name, raw, err)
		}
	}
}

// TestPublishExpvar_GoroutineCountStable verifies no goroutines are leaked by
// PublishExpvar via the runtime goroutine count.
func TestPublishExpvar_GoroutineCountStable(t *testing.T) {
	before := runtime.NumGoroutine()

	w := flecs.New()
	prefix := expvarPrefix(t)
	h := flecs.PublishExpvar(w, prefix)
	w.Progress(0.016)

	for i := 0; i < 5; i++ {
		if v := expvar.Get(prefix); v != nil {
			_ = v.String()
		}
	}
	h.Unpublish()

	after := runtime.NumGoroutine()
	// Allow a small margin for other concurrent test goroutines.
	if after > before+5 {
		t.Errorf("goroutine count grew from %d to %d after PublishExpvar+Unpublish; possible leak", before, after)
	}
}

// TestPublishExpvar_WholeTreeKeys verifies the whole-tree var contains all expected keys.
func TestPublishExpvar_WholeTreeKeys(t *testing.T) {
	w := flecs.New()
	prefix := expvarPrefix(t)
	flecs.PublishExpvar(w, prefix)
	w.Progress(0.016)

	raw := scrapeString(t, prefix)
	var tree map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &tree); err != nil {
		t.Fatalf("whole-tree JSON invalid: %v\nraw: %s", err, raw)
	}
	required := []string{
		"entity_count", "table_count", "component_count", "system_count",
		"observer_count", "frame_count", "reclaimed_tables",
		"phases", "window_second", "window_minute", "window_hour",
	}
	for _, k := range required {
		if _, ok := tree[k]; !ok {
			t.Errorf("whole-tree JSON missing key %q; present keys: %v", k, expvarTreeKeys(tree))
		}
	}
}

func expvarTreeKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestPublishExpvar_ComponentCount verifies component_count increases after registration.
func TestPublishExpvar_ComponentCount(t *testing.T) {
	type expvarPosC struct{ X, Y float32 }

	w := flecs.New()
	prefix := expvarPrefix(t)
	flecs.PublishExpvar(w, prefix)

	w.Progress(0.016)
	rawBefore := scrapeString(t, prefix+".component_count")
	var before int
	if err := json.Unmarshal([]byte(rawBefore), &before); err != nil {
		t.Fatalf("component_count before: %v", err)
	}

	flecs.RegisterComponent[expvarPosC](w)
	w.Progress(0.016)

	rawAfter := scrapeString(t, prefix+".component_count")
	var after int
	if err := json.Unmarshal([]byte(rawAfter), &after); err != nil {
		t.Fatalf("component_count after: %v", err)
	}
	if after <= before {
		t.Errorf("component_count after RegisterComponent: got %d, want > %d", after, before)
	}
}

// TestPublishExpvar_ObserverCount verifies observer_count reflects observer lifecycle
// including both any-entity and fixed-source observers.
func TestPublishExpvar_ObserverCount(t *testing.T) {
	type expvarTagO struct{}

	w := flecs.New()
	prefix := expvarPrefix(t)
	flecs.PublishExpvar(w, prefix)

	cid := flecs.RegisterComponent[expvarTagO](w)
	w.Progress(0.016)

	rawBefore := scrapeString(t, prefix+".observer_count")
	var before int
	if err := json.Unmarshal([]byte(rawBefore), &before); err != nil {
		t.Fatalf("observer_count before: %v", err)
	}

	// Add an any-entity observer.
	obs := flecs.ObserveID(w, cid, flecs.EventOnAdd, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {})
	w.Progress(0.016)

	rawAfter := scrapeString(t, prefix+".observer_count")
	var after int
	if err := json.Unmarshal([]byte(rawAfter), &after); err != nil {
		t.Fatalf("observer_count after ObserveID: %v", err)
	}
	if after <= before {
		t.Errorf("observer_count after ObserveID: got %d, want > %d", after, before)
	}

	// Add a fixed-source observer to exercise the fixedSource bucket path in statsCommit.
	var sourceE flecs.ID
	w.Write(func(fw *flecs.Writer) { sourceE = fw.NewEntity() })
	obsFixed := flecs.ObserveIDWithOptions(w, cid, flecs.WithSource(sourceE),
		[]flecs.EventKind{flecs.EventOnAdd},
		func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {})
	w.Progress(0.016)

	rawFixed := scrapeString(t, prefix+".observer_count")
	var afterFixed int
	if err := json.Unmarshal([]byte(rawFixed), &afterFixed); err != nil {
		t.Fatalf("observer_count after fixed-source ObserveID: %v", err)
	}
	if afterFixed <= after {
		t.Errorf("observer_count after fixed-source observer: got %d, want > %d", afterFixed, after)
	}
	obsFixed.Unsubscribe()

	obs.Unsubscribe()
	w.Progress(0.016)

	rawFinal := scrapeString(t, prefix+".observer_count")
	var final int
	if err := json.Unmarshal([]byte(rawFinal), &final); err != nil {
		t.Fatalf("observer_count after Unsubscribe: %v", err)
	}
	if final >= after {
		t.Errorf("observer_count after Unsubscribe: got %d, want < %d", final, after)
	}
}

// TestPublishExpvar_LastProgressSeconds verifies null before first Progress and a valid
// unix timestamp after.
func TestPublishExpvar_LastProgressSeconds(t *testing.T) {
	w := flecs.New()
	prefix := expvarPrefix(t)
	flecs.PublishExpvar(w, prefix)

	rawBefore := scrapeString(t, prefix+".last_progress_seconds")
	if rawBefore != "null" {
		t.Errorf("last_progress_seconds before Progress: got %q, want null", rawBefore)
	}

	w.Progress(0.016)

	rawAfter := scrapeString(t, prefix+".last_progress_seconds")
	if rawAfter == "null" {
		t.Error("last_progress_seconds after Progress: still null")
	}
	var ts float64
	if err := json.Unmarshal([]byte(rawAfter), &ts); err != nil {
		t.Fatalf("last_progress_seconds after Progress: not valid JSON float: %v", err)
	}
	if ts <= 0 {
		t.Errorf("last_progress_seconds: got %v, want > 0", ts)
	}
}

// TestPublishExpvar_PrefixIsolation verifies that two distinct prefixes register
// separate variables without collision.
func TestPublishExpvar_PrefixIsolation(t *testing.T) {
	w1 := flecs.New()
	w2 := flecs.New()
	p1 := expvarPrefix(t)
	p2 := expvarPrefix(t)

	flecs.PublishExpvar(w1, p1)
	flecs.PublishExpvar(w2, p2)

	for _, prefix := range []string{p1, p2} {
		if expvar.Get(prefix) == nil {
			t.Errorf("whole-tree var for prefix %q not registered", prefix)
		}
		if expvar.Get(prefix+".entity_count") == nil {
			t.Errorf("entity_count for prefix %q not registered", prefix)
		}
	}

	// Seed worlds differently so their entity_count values diverge.
	for i := 0; i < 2; i++ {
		w1.Write(func(fw *flecs.Writer) { fw.NewEntity() })
	}
	w1.Progress(0.016)
	for i := 0; i < 9; i++ {
		w2.Write(func(fw *flecs.Writer) { fw.NewEntity() })
	}
	w2.Progress(0.016)

	// Distinct prefixes must read from distinct worlds.
	raw1 := scrapeString(t, p1+".entity_count")
	raw2 := scrapeString(t, p2+".entity_count")
	if raw1 == raw2 {
		t.Errorf("two distinct prefixes/worlds share entity_count value %q; want distinct", raw1)
	}
}
