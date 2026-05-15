package flecs_test

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/snichols/flecs"
)

type addonPos struct{ X, Y float32 }

// TestStatsSnapshot_EmptyWorld verifies that StatsSnapshot on a fresh world
// returns zero for tick-derived fields and does not panic.
func TestStatsSnapshot_EmptyWorld(t *testing.T) {
	w := flecs.New()
	snap := w.StatsSnapshot()
	if snap.World.FrameCount != 0 {
		t.Errorf("FrameCount: got %d, want 0", snap.World.FrameCount)
	}
	if snap.World.TotalTime != 0 {
		t.Errorf("TotalTime: got %v, want 0", snap.World.TotalTime)
	}
	if snap.World.LastTickDelta != 0 {
		t.Errorf("LastTickDelta: got %v, want 0", snap.World.LastTickDelta)
	}
	if len(snap.Phases) != 0 {
		t.Errorf("Phases: got len %d, want 0 before first Progress", len(snap.Phases))
	}
	if len(snap.Systems) != 0 {
		t.Errorf("Systems: got len %d, want 0 before first Progress", len(snap.Systems))
	}
}

// TestStatsSnapshot_EntityCountIncrements verifies that WorldStats.EntityCount
// reflects newly created entities after a Progress call.
func TestStatsSnapshot_EntityCountIncrements(t *testing.T) {
	w := flecs.New()
	w.Progress(0)
	before := w.StatsSnapshot().World.EntityCount

	w.Write(func(fw *flecs.Writer) {
		fw.NewEntity()
		fw.NewEntity()
	})

	w.Progress(0)
	after := w.StatsSnapshot().World.EntityCount

	if after != before+2 {
		t.Errorf("EntityCount: got %d, want %d after adding 2 entities", after, before+2)
	}
}

// TestStatsSnapshot_FrameCountIncrements verifies that WorldStats.FrameCount
// increments with each Progress call.
func TestStatsSnapshot_FrameCountIncrements(t *testing.T) {
	w := flecs.New()
	const n = 5
	for i := 0; i < n; i++ {
		w.Progress(0.016)
	}
	snap := w.StatsSnapshot()
	if snap.World.FrameCount != n {
		t.Errorf("FrameCount: got %d, want %d", snap.World.FrameCount, n)
	}
}

// TestStatsSnapshot_TwoSystemsHaveInvocations verifies that two registered
// systems both have non-zero Invocations after a Progress call.
func TestStatsSnapshot_TwoSystemsHaveInvocations(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[addonPos](w)
	q := flecs.NewCachedQuery(w, posID)
	noop := func(dt float32, it *flecs.QueryIter) {}

	s1 := flecs.NewSystem(w, q, noop).SetName("alpha")
	s2 := flecs.NewSystem(w, q, noop).SetName("beta")
	_ = s1
	_ = s2

	w.Progress(0.016)
	snap := w.StatsSnapshot()

	if len(snap.Systems) != 2 {
		t.Fatalf("Systems: got %d, want 2", len(snap.Systems))
	}
	for _, ss := range snap.Systems {
		if ss.Invocations != 1 {
			t.Errorf("System %q: Invocations = %d, want 1", ss.Name, ss.Invocations)
		}
	}
}

// TestStatsSnapshot_DisabledSystemHasZeroInvocations verifies that a disabled
// system contributes zero Invocations to the snapshot.
func TestStatsSnapshot_DisabledSystemHasZeroInvocations(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[addonPos](w)
	q := flecs.NewCachedQuery(w, posID)
	noop := func(dt float32, it *flecs.QueryIter) {}

	active := flecs.NewSystem(w, q, noop).SetName("active")
	disabled := flecs.NewSystem(w, q, noop).SetName("disabled")
	disabled.SetEnabled(false)
	_ = active

	w.Progress(0.016)
	snap := w.StatsSnapshot()

	found := false
	for _, ss := range snap.Systems {
		if ss.Name == "disabled" {
			found = true
			if ss.Invocations != 0 {
				t.Errorf("disabled system Invocations = %d, want 0", ss.Invocations)
			}
		}
		if ss.Name == "active" {
			if ss.Invocations != 1 {
				t.Errorf("active system Invocations = %d, want 1", ss.Invocations)
			}
		}
	}
	if !found {
		t.Error("disabled system not found in snapshot")
	}
}

// TestStatsSnapshot_RateFilteredSystem verifies that a rate-filtered system
// fires less often and that Invocations reflects the gate, not the tick count.
func TestStatsSnapshot_RateFilteredSystem(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[addonPos](w)
	q := flecs.NewCachedQuery(w, posID)
	noop := func(dt float32, it *flecs.QueryIter) {}

	gated := flecs.NewSystem(w, q, noop).SetName("gated")
	gated.SetRate(3) // fire every 3 ticks

	const ticks = 9
	for i := 0; i < ticks; i++ {
		w.Progress(0.016)
	}
	snap := w.StatsSnapshot()

	for _, ss := range snap.Systems {
		if ss.Name != "gated" {
			continue
		}
		want := uint64(ticks / 3)
		if ss.Invocations != want {
			t.Errorf("rate-gated Invocations = %d, want %d", ss.Invocations, want)
		}
		if ss.TotalSkipped != uint64(ticks)-want {
			t.Errorf("rate-gated TotalSkipped = %d, want %d", ss.TotalSkipped, uint64(ticks)-want)
		}
		return
	}
	t.Error("gated system not found in snapshot")
}

// TestStatsSnapshot_PhaseSystemCount verifies that each phase in the snapshot
// reports the correct active SystemCount.
func TestStatsSnapshot_PhaseSystemCount(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[addonPos](w)
	q := flecs.NewCachedQuery(w, posID)
	noop := func(dt float32, it *flecs.QueryIter) {}

	flecs.NewSystemInPhase(w, w.OnUpdate(), q, noop)
	flecs.NewSystemInPhase(w, w.OnUpdate(), q, noop)
	flecs.NewSystemInPhase(w, w.PostUpdate(), q, noop)

	w.Progress(0.016)
	snap := w.StatsSnapshot()

	found := map[string]int{}
	for _, ph := range snap.Phases {
		found[ph.Name] = ph.SystemCount
	}
	if found["OnUpdate"] != 2 {
		t.Errorf("OnUpdate SystemCount = %d, want 2", found["OnUpdate"])
	}
	if found["PostUpdate"] != 1 {
		t.Errorf("PostUpdate SystemCount = %d, want 1", found["PostUpdate"])
	}
	if found["PreUpdate"] != 0 {
		t.Errorf("PreUpdate SystemCount = %d, want 0", found["PreUpdate"])
	}
}

// TestStatsSnapshot_Concurrent verifies that concurrent StatsSnapshot calls
// during Progress are race-clean (run with -race to validate).
func TestStatsSnapshot_Concurrent(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[addonPos](w)
	q := flecs.NewCachedQuery(w, posID)

	flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		time.Sleep(time.Millisecond) // slow enough for concurrent readers
	})

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Background goroutine: continuously read snapshots.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = w.StatsSnapshot()
			}
		}
	}()

	// Run several ticks from the main goroutine.
	for i := 0; i < 10; i++ {
		w.Progress(0.016)
	}

	close(stop)
	wg.Wait()
}

// TestStatsSnapshot_IsSnapshot verifies that a retained PipelineStats value
// does not reflect subsequent Progress calls.
func TestStatsSnapshot_IsSnapshot(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[addonPos](w)
	q := flecs.NewCachedQuery(w, posID)
	noop := func(dt float32, it *flecs.QueryIter) {}
	flecs.NewSystem(w, q, noop)

	w.Progress(0.016)
	snap1 := w.StatsSnapshot()
	fc1 := snap1.World.FrameCount
	inv1 := uint64(0)
	if len(snap1.Systems) > 0 {
		inv1 = snap1.Systems[0].Invocations
	}

	w.Progress(0.016)
	w.Progress(0.016)

	// snap1 must not have changed.
	if snap1.World.FrameCount != fc1 {
		t.Errorf("snapshot mutated: FrameCount changed from %d to %d", fc1, snap1.World.FrameCount)
	}
	if len(snap1.Systems) > 0 && snap1.Systems[0].Invocations != inv1 {
		t.Errorf("snapshot mutated: Invocations changed from %d to %d", inv1, snap1.Systems[0].Invocations)
	}

	// A fresh snapshot must show the updated values.
	snap2 := w.StatsSnapshot()
	if snap2.World.FrameCount != 3 {
		t.Errorf("snap2 FrameCount = %d, want 3", snap2.World.FrameCount)
	}
	if len(snap2.Systems) > 0 && snap2.Systems[0].Invocations != 3 {
		t.Errorf("snap2 Invocations = %d, want 3", snap2.Systems[0].Invocations)
	}
}

// TestStatsSnapshot_CumulativePhaseInvocations verifies that phase Invocations
// increments each Progress call.
func TestStatsSnapshot_CumulativePhaseInvocations(t *testing.T) {
	w := flecs.New()
	const n = 4
	for i := 0; i < n; i++ {
		w.Progress(0)
	}
	snap := w.StatsSnapshot()
	for _, ph := range snap.Phases {
		if ph.Invocations != n {
			t.Errorf("phase %q Invocations = %d, want %d", ph.Name, ph.Invocations, n)
		}
	}
}

// TestStatsSnapshot_SetName verifies that SetName overrides the auto-generated name.
func TestStatsSnapshot_SetName(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[addonPos](w)
	q := flecs.NewCachedQuery(w, posID)
	noop := func(dt float32, it *flecs.QueryIter) {}

	flecs.NewSystem(w, q, noop).SetName("my-physics-system")
	w.Progress(0.016)
	snap := w.StatsSnapshot()

	if len(snap.Systems) != 1 {
		t.Fatalf("Systems: got %d, want 1", len(snap.Systems))
	}
	if snap.Systems[0].Name != "my-physics-system" {
		t.Errorf("Name = %q, want %q", snap.Systems[0].Name, "my-physics-system")
	}
}

// ─── Ring-buffer unit tests ──────────────────────────────────────────────────

// TestStats_RingBuffer_Records60Ticks records 60 distinct values (1..60) and
// verifies Reduce() returns the correct Avg/Min/Max.
func TestStats_RingBuffer_Records60Ticks(t *testing.T) {
	var sw flecs.StatsWindow
	for i := 1; i <= 60; i++ {
		sw.Record(float64(i))
	}
	got := sw.Reduce()
	wantAvg := 30.5 // (1+60)/2
	if math.Abs(got.Avg-wantAvg) > 1e-9 {
		t.Errorf("Avg = %v, want %v", got.Avg, wantAvg)
	}
	if got.Min != 1 {
		t.Errorf("Min = %v, want 1", got.Min)
	}
	if got.Max != 60 {
		t.Errorf("Max = %v, want 60", got.Max)
	}
}

// TestStats_RingBuffer_Wraps records 70 values (1..70); only the last 60
// (11..70) should be reflected in Reduce().
func TestStats_RingBuffer_Wraps(t *testing.T) {
	var sw flecs.StatsWindow
	for i := 1; i <= 70; i++ {
		sw.Record(float64(i))
	}
	got := sw.Reduce()
	// Last 60 values: 11..70; avg = (11+70)/2 = 40.5
	wantAvg := 40.5
	if math.Abs(got.Avg-wantAvg) > 1e-9 {
		t.Errorf("Avg = %v, want %v", got.Avg, wantAvg)
	}
	if got.Min != 11 {
		t.Errorf("Min = %v, want 11", got.Min)
	}
	if got.Max != 70 {
		t.Errorf("Max = %v, want 70", got.Max)
	}
}

// TestStats_WindowReduction_MinuteFromSecond drives 60 ticks via StatsTick and
// verifies that the minute window has exactly one reduced slot.
func TestStats_WindowReduction_MinuteFromSecond(t *testing.T) {
	w := flecs.New()
	// Commit initial state so statsEntityCount etc. are non-zero.
	w.Progress(0.016)
	// 60 StatsTick calls advance the aggregator to trigger one minute reduction.
	for i := 0; i < 60; i++ {
		w.StatsTick()
	}
	// Minute window should now have exactly one slot filled (Avg should be non-zero
	// since entity count > 0 after Progress).
	agg := w.WorldStatsWindow(flecs.StatsMinute)
	// The minute window had exactly 1 reduction fed in, so Avg == that one value.
	// EntityCount must be > 0 (built-in entities exist).
	if agg.EntityCount.Avg == 0 {
		t.Error("minute window EntityCount.Avg = 0, expected > 0 after one reduction")
	}
}

// TestStats_WindowReduction_HourFromMinute drives 3600 ticks via StatsTick and
// verifies that the hour window has at least one reduced slot.
func TestStats_WindowReduction_HourFromMinute(t *testing.T) {
	w := flecs.New()
	w.Progress(0.016)
	for i := 0; i < 3600; i++ {
		w.StatsTick()
	}
	agg := w.WorldStatsWindow(flecs.StatsHour)
	if agg.EntityCount.Avg == 0 {
		t.Error("hour window EntityCount.Avg = 0, expected > 0 after 3600 ticks")
	}
}

// TestStats_WindowReduction_EmptyWindow verifies that all methods on a fresh
// StatsWindow return zero MetricGauge without panicking.
func TestStats_WindowReduction_EmptyWindow(t *testing.T) {
	var sw flecs.StatsWindow
	g := sw.Reduce()
	if g.Avg != 0 || g.Min != 0 || g.Max != 0 {
		t.Errorf("empty Reduce() = %+v, want zero", g)
	}
	l := sw.Last()
	if l.Avg != 0 || l.Min != 0 || l.Max != 0 {
		t.Errorf("empty Last() = %+v, want zero", l)
	}
	// WorldStatsWindow on a fresh world should also be zero-safe.
	w := flecs.New()
	agg := w.WorldStatsWindow(flecs.StatsSecond)
	if agg.EntityCount.Avg != 0 {
		t.Errorf("fresh world second window EntityCount.Avg = %v, want 0", agg.EntityCount.Avg)
	}
}

// TestStats_WorldStatsWindow_Second drives Progress 60× with a growing entity
// count and verifies that the Second window Avg lands in the expected mid-range.
func TestStats_WorldStatsWindow_Second(t *testing.T) {
	w := flecs.New()
	var baseCount int

	// Capture the entity count baseline before adding test entities.
	w.Progress(0)
	baseCount = w.StatsSnapshot().World.EntityCount

	// Add entities one per tick for 60 ticks.
	for i := 0; i < 60; i++ {
		w.Write(func(fw *flecs.Writer) { fw.NewEntity() })
		w.Progress(0.016)
	}

	agg := w.WorldStatsWindow(flecs.StatsSecond)

	// After 60 ticks adding 1 entity each, entity counts ranged from
	// baseCount+1 to baseCount+60. Avg should be in that range.
	lo := float64(baseCount + 1)
	hi := float64(baseCount + 60)
	if agg.EntityCount.Avg < lo || agg.EntityCount.Avg > hi {
		t.Errorf("EntityCount.Avg = %v, expected in [%v, %v]", agg.EntityCount.Avg, lo, hi)
	}
	if agg.EntityCount.Min < lo {
		t.Errorf("EntityCount.Min = %v, expected >= %v", agg.EntityCount.Min, lo)
	}
	if agg.EntityCount.Max > hi {
		t.Errorf("EntityCount.Max = %v, expected <= %v", agg.EntityCount.Max, hi)
	}
}

// TestStats_PipelineStatsWindow_Second drives Progress 60× and verifies that
// the second window for pipeline metrics is non-empty.
func TestStats_PipelineStatsWindow_Second(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[addonPos](w)
	q := flecs.NewCachedQuery(w, posID)
	flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {}).SetName("pipeline-test-sys")

	for i := 0; i < 60; i++ {
		w.Progress(0.016)
	}

	agg := w.PipelineStatsWindow(flecs.StatsSecond)
	if agg.World.EntityCount.Avg == 0 {
		t.Error("PipelineStatsWindow second: world EntityCount.Avg = 0, expected > 0")
	}
	if agg.World.FrameCount.Avg == 0 {
		t.Error("PipelineStatsWindow second: FrameCount.Avg = 0, expected > 0")
	}
}

// ─── REST period tests ───────────────────────────────────────────────────────

// TestRest_StatsWorld_PeriodSecond verifies that GET /stats/world?period=second
// returns 200 with a JSON body shaped as WorldStatsAggregated (has avg/min/max fields).
func TestRest_StatsWorld_PeriodSecond(t *testing.T) {
	w := flecs.New()
	for i := 0; i < 5; i++ {
		w.Progress(0.016)
	}
	h := flecs.NewRESTHandler(w)
	req := httptest.NewRequest(http.MethodGet, "/stats/world?period=second", nil)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rw.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Body should have entity_count with avg/min/max sub-fields.
	ec, ok := body["entity_count"].(map[string]interface{})
	if !ok {
		t.Fatalf("entity_count not found or not object: %v", body)
	}
	if _, ok := ec["avg"]; !ok {
		t.Errorf("entity_count missing 'avg' field: %v", ec)
	}
	if _, ok := ec["min"]; !ok {
		t.Errorf("entity_count missing 'min' field: %v", ec)
	}
	if _, ok := ec["max"]; !ok {
		t.Errorf("entity_count missing 'max' field: %v", ec)
	}
}

// TestRest_StatsWorld_PeriodInvalid verifies that GET /stats/world?period=bogus
// returns 400.
func TestRest_StatsWorld_PeriodInvalid(t *testing.T) {
	w := flecs.New()
	h := flecs.NewRESTHandler(w)
	req := httptest.NewRequest(http.MethodGet, "/stats/world?period=bogus", nil)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.Code)
	}
}

// TestRest_StatsWorld_PeriodDefault verifies that omitting ?period= returns the
// same JSON structure as before (back-compat: "world" key with snake_case fields).
func TestRest_StatsWorld_PeriodDefault(t *testing.T) {
	w := flecs.New()
	w.Progress(0.016)
	h := flecs.NewRESTHandler(w)

	req := httptest.NewRequest(http.MethodGet, "/stats/world", nil)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rw.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	world, ok := body["world"].(map[string]interface{})
	if !ok {
		t.Fatalf("'world' key missing or not object: %v", body)
	}
	for _, field := range []string{"entity_count", "table_count", "frame_count"} {
		if _, exists := world[field]; !exists {
			t.Errorf("default response missing field %q", field)
		}
	}
}

// TestRest_StatsPipeline_PeriodMinute verifies that GET /stats/pipeline?period=minute
// returns 200 with a JSON body that has world.entity_count.avg field.
func TestRest_StatsPipeline_PeriodMinute(t *testing.T) {
	w := flecs.New()
	// Drive 60 ticks to produce at least one minute-window reduction.
	w.Progress(0.016)
	for i := 0; i < 60; i++ {
		w.StatsTick()
	}
	h := flecs.NewRESTHandler(w)
	req := httptest.NewRequest(http.MethodGet, "/stats/pipeline?period=minute", nil)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rw.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	world, ok := body["world"].(map[string]interface{})
	if !ok {
		t.Fatalf("'world' key missing: %v", body)
	}
	ec, ok := world["entity_count"].(map[string]interface{})
	if !ok {
		t.Fatalf("world.entity_count missing or not object: %v", world)
	}
	if _, ok := ec["avg"]; !ok {
		t.Errorf("world.entity_count missing 'avg': %v", ec)
	}
}

// TestStats_Snapshot_PreservesWindows is deferred: including aggregator state in
// the snapshot wire format requires a version bump and migration path that is
// out of scope for Phase 16.38 (stats are observable-only, not simulation state).
func TestStats_Snapshot_PreservesWindows(t *testing.T) {
	t.Skip("aggregator state is not persisted in the snapshot wire format (Phase 16.38 non-goal)")
}

// TestStats_JSON_Aggregated_RoundTrip verifies that WorldStatsAggregated can be
// marshaled to JSON and unmarshaled back with identical values.
func TestStats_JSON_Aggregated_RoundTrip(t *testing.T) {
	w := flecs.New()
	for i := 0; i < 5; i++ {
		w.Progress(0.016)
	}
	orig := w.WorldStatsWindow(flecs.StatsSecond)

	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got flecs.WorldStatsAggregated
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.EntityCount.Avg != orig.EntityCount.Avg {
		t.Errorf("EntityCount.Avg: got %v, want %v", got.EntityCount.Avg, orig.EntityCount.Avg)
	}
	if got.FrameCount.Max != orig.FrameCount.Max {
		t.Errorf("FrameCount.Max: got %v, want %v", got.FrameCount.Max, orig.FrameCount.Max)
	}
}
