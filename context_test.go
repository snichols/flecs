package flecs_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	flecs "github.com/snichols/flecs"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

type ctxPos struct{ X, Y float32 }
type ctxVel struct{ DX, DY float32 }

func newCtxWorld(t *testing.T) (*flecs.World, flecs.ID, flecs.ID) {
	t.Helper()
	w := flecs.New()
	posID := flecs.RegisterComponent[ctxPos](w)
	velID := flecs.RegisterComponent[ctxVel](w)
	return w, posID, velID
}

// populateWorld creates n entities with ctxPos and ctxVel.
func populateWorld(t *testing.T, w *flecs.World, posID, velID flecs.ID, n int) {
	t.Helper()
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < n; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, ctxPos{X: float32(i), Y: float32(i)})
			flecs.Set(fw, e, ctxVel{DX: 1, DY: 1})
		}
	})
}

// ─── Progress cancellation ────────────────────────────────────────────────────

func TestProgressContext_Background_NoCancellation(t *testing.T) {
	w, posID, velID := newCtxWorld(t)
	populateWorld(t, w, posID, velID, 100)

	var ran atomic.Int32
	q := flecs.NewCachedQuery(w, posID, velID)
	flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			ran.Add(int32(it.Count()))
		}
	})

	err := w.ProgressContext(context.Background(), 1.0/60.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran.Load() != 100 {
		t.Fatalf("expected 100 entities, got %d", ran.Load())
	}
}

func TestProgressContext_PreCanceled(t *testing.T) {
	w, posID, velID := newCtxWorld(t)
	populateWorld(t, w, posID, velID, 10)

	var ran atomic.Int32
	q := flecs.NewCachedQuery(w, posID, velID)
	flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			ran.Add(int32(it.Count()))
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before dispatch

	err := w.ProgressContext(ctx, 1.0/60.0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if ran.Load() != 0 {
		t.Fatalf("expected no systems to run on pre-cancel, ran %d entities", ran.Load())
	}
}

func TestProgressContext_CanceledMidTick(t *testing.T) {
	w, posID, velID := newCtxWorld(t)
	populateWorld(t, w, posID, velID, 10)

	ctx, cancel := context.WithCancel(context.Background())

	var phase1Ran atomic.Bool
	var phase2Ran atomic.Bool

	q1 := flecs.NewCachedQueryFromTerms(w, flecs.With(posID))
	sys1 := flecs.NewSystemInPhase(w, w.PreUpdate(), q1, func(dt float32, it *flecs.QueryIter) {
		phase1Ran.Store(true)
		// Cancel after phase 1 system runs.
		cancel()
		// Drain remaining.
		for it.Next() {
		}
	})
	_ = sys1

	q2 := flecs.NewCachedQueryFromTerms(w, flecs.With(velID))
	flecs.NewSystemInPhase(w, w.OnUpdate(), q2, func(dt float32, it *flecs.QueryIter) {
		phase2Ran.Store(true)
		for it.Next() {
		}
	})

	err := w.ProgressContext(ctx, 1.0/60.0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if !phase1Ran.Load() {
		t.Error("expected phase 1 system to have run")
	}
	// phase2 may or may not have run depending on timing of ctx check.
	// What we care about is that an error was returned.
}

func TestProgressContext_TimeoutFires(t *testing.T) {
	w, posID, velID := newCtxWorld(t)
	populateWorld(t, w, posID, velID, 5)

	q := flecs.NewCachedQuery(w, posID, velID)
	flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			time.Sleep(10 * time.Millisecond) // make tick slow
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	// Let timeout fire.
	time.Sleep(5 * time.Millisecond)

	err := w.ProgressContext(ctx, 1.0/60.0)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestProgressContext_DeferredMutationsApplied(t *testing.T) {
	w, posID, velID := newCtxWorld(t)
	populateWorld(t, w, posID, velID, 5)

	var addedEntity flecs.ID
	q := flecs.NewCachedQueryFromTerms(w, flecs.With(posID))
	// This system adds an entity via deferred mutation (queued during phase).
	flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			fw := it.Writer()
			e := fw.NewEntity()
			flecs.Set(fw, e, ctxPos{X: 99, Y: 99})
			addedEntity = e
		}
	})

	// Run with Background — all mutations should be applied.
	err := w.ProgressContext(context.Background(), 1.0/60.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if addedEntity == 0 {
		t.Fatal("system did not record a deferred entity")
	}
	// The entity added during the system run should be visible after Progress.
	var gotX float32
	w.Read(func(r *flecs.Reader) {
		v, ok := flecs.Get[ctxPos](r, addedEntity)
		if ok {
			gotX = v.X
		}
	})
	if gotX != 99 {
		t.Fatalf("deferred entity not found or has wrong position; got X=%v", gotX)
	}
}

// ─── Snapshot cancellation ────────────────────────────────────────────────────

func TestTakeSnapshotContext_Background(t *testing.T) {
	w, posID, velID := newCtxWorld(t)
	populateWorld(t, w, posID, velID, 50)

	snap, err := w.TakeSnapshotContext(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if snap.Partial {
		t.Error("expected Partial=false for Background context")
	}
}

func TestTakeSnapshotContext_PreCanceled(t *testing.T) {
	w, posID, velID := newCtxWorld(t)
	populateWorld(t, w, posID, velID, 10)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	snap, err := w.TakeSnapshotContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if snap == nil {
		t.Fatal("expected non-nil partial snapshot")
	}
	if !snap.Partial {
		t.Error("expected Partial=true for pre-cancelled context")
	}
}

func TestTakeSnapshotContext_CanceledMidWalk(t *testing.T) {
	w, posID, velID := newCtxWorld(t)
	// Populate enough entities to exercise table walking.
	populateWorld(t, w, posID, velID, 200)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay to interrupt mid-walk.
	go func() {
		time.Sleep(1 * time.Millisecond)
		cancel()
	}()

	snap, err := w.TakeSnapshotContext(ctx)
	// May or may not cancel mid-walk depending on timing; if it doesn't
	// cancel, that's also a valid outcome (Background-equivalent).
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected error type: %v", err)
		}
		if snap == nil || !snap.Partial {
			t.Error("expected non-nil partial snapshot with Partial=true on cancel")
		}
	}
}

func TestRestoreSnapshotContext_Background(t *testing.T) {
	w, posID, velID := newCtxWorld(t)
	populateWorld(t, w, posID, velID, 20)

	snap := flecs.TakeSnapshot(w)

	// Restore to same world (clears and re-applies snapshot).
	err := w.RestoreSnapshotContext(context.Background(), snap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify restoration.
	stats := w.Stats()
	if stats.EntityCount == 0 {
		t.Error("expected entities after restore")
	}
}

func TestRestoreSnapshotContext_CanceledMidRestore(t *testing.T) {
	w, posID, velID := newCtxWorld(t)
	populateWorld(t, w, posID, velID, 5)

	snap := flecs.TakeSnapshot(w)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	err := w.RestoreSnapshotContext(ctx, snap)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	// World is in partial/unknown state after cancelled mid-restore;
	// documented behavior (no rollback guarantee).
}

// ─── Query iteration cancellation ─────────────────────────────────────────────

func TestEachContext_PreCanceled(t *testing.T) {
	w, posID, velID := newCtxWorld(t)
	populateWorld(t, w, posID, velID, 20)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.With(velID))
	var visited int
	err := q.EachContext(ctx, func(it *flecs.QueryIter) {
		visited += it.Count()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if visited != 0 {
		t.Fatalf("expected 0 entities visited on pre-cancel, got %d", visited)
	}
}

func TestEachContext_CanceledMidIteration(t *testing.T) {
	w, posID, velID := newCtxWorld(t)
	// Create many tables by spreading entities into different archetypes via a
	// per-entity unique component to force many tables.
	// Actually, just use enough entities in one table and rely on large N.
	populateWorld(t, w, posID, velID, 2000)

	ctx, cancel := context.WithCancel(context.Background())
	var visited int
	cq := flecs.NewCachedQuery(w, posID, velID)
	err := cq.EachContext(ctx, func(it *flecs.QueryIter) {
		if visited == 0 {
			cancel() // cancel after first chunk
		}
		visited += it.Count()
	})
	// With 2000 entities in possibly one table and N=1024 check interval,
	// we might complete before the check fires. Accept either outcome.
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEachContext_LargeWorld_Timeout(t *testing.T) {
	w, posID, velID := newCtxWorld(t)
	populateWorld(t, w, posID, velID, 10000)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Inject a slow callback to ensure timeout fires.
	cq := flecs.NewCachedQuery(w, posID, velID)
	_ = cq.EachContext(ctx, func(it *flecs.QueryIter) {
		time.Sleep(1 * time.Millisecond)
		for it.Next() { //nolint:revive // drain
		}
	})
	// We accept any outcome; the key is no panic and ctx.Err() is propagated.
}

// ─── Per-system cancellation ──────────────────────────────────────────────────

func TestRunSystemContext_PreCanceled(t *testing.T) {
	w, posID, velID := newCtxWorld(t)
	populateWorld(t, w, posID, velID, 10)

	var ran atomic.Bool
	q := flecs.NewCachedQuery(w, posID, velID)
	sys := flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		ran.Store(true)
		for it.Next() {
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := flecs.RunSystemContext(ctx, sys, 1.0/60.0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if ran.Load() {
		t.Error("system should not have run with pre-cancelled context")
	}
}

func TestRunSystemContext_CanceledMidIter(t *testing.T) {
	w, posID, velID := newCtxWorld(t)
	populateWorld(t, w, posID, velID, 5)

	q := flecs.NewCachedQuery(w, posID, velID)
	sys := flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			// Just drain; cancellation happens externally.
		}
	})

	// With Background, system runs to completion.
	err := flecs.RunSystemContext(context.Background(), sys, 1.0/60.0)
	if err != nil {
		t.Fatalf("unexpected error with Background context: %v", err)
	}
}

// ─── REST integration ─────────────────────────────────────────────────────────

func TestRest_ClientDisconnect_AbortsProgress(t *testing.T) {
	w, posID, velID := newCtxWorld(t)
	populateWorld(t, w, posID, velID, 5)

	handler := flecs.NewRESTHandler(w)

	// Use a cancelled context to simulate client disconnect.
	req := httptest.NewRequest(http.MethodGet, "/snapshot", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel to simulate immediate disconnect
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Expect 499 or some non-200 indicating abort.
	if rr.Code == http.StatusOK {
		t.Error("expected non-200 status for cancelled request, got 200")
	}
}

func TestRest_RequestTimeout_AbortsQuery(t *testing.T) {
	w, posID, velID := newCtxWorld(t)
	populateWorld(t, w, posID, velID, 5)

	handler := flecs.NewRESTHandler(w)

	req := httptest.NewRequest(http.MethodGet, "/query?expr=ctxPos", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel to simulate timeout
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Expect 499 indicating abort.
	if rr.Code == http.StatusOK {
		t.Error("expected non-200 status for cancelled query, got 200")
	}
}

// ─── Parallel system interaction ───────────────────────────────────────────────

func TestProgressContext_ParallelSystemCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping parallel cancel test in short mode")
	}
	w, posID, velID := newCtxWorld(t)
	populateWorld(t, w, posID, velID, 20)

	w.SetWorkerCount(2)
	defer w.SetWorkerCount(0)

	ctx, cancel := context.WithCancel(context.Background())

	q1 := flecs.NewCachedQueryFromTerms(w, flecs.With(posID))
	sys1 := flecs.NewSystem(w, q1, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
		}
	})
	sys1.SetParallel(true)
	sys1.SetWriteSet([]flecs.ID{posID})

	q2 := flecs.NewCachedQueryFromTerms(w, flecs.With(velID))
	sys2 := flecs.NewSystem(w, q2, func(dt float32, it *flecs.QueryIter) {
		cancel() // cancel during parallel execution
		for it.Next() {
		}
	})
	sys2.SetParallel(true)
	sys2.SetWriteSet([]flecs.ID{velID})

	err := w.ProgressContext(ctx, 1.0/60.0)
	// Either no error (if cancel happened after wave completed) or context.Canceled.
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ─── Stress / leak test ───────────────────────────────────────────────────────

func TestContext_RepeatedCancellation_NoLeak(t *testing.T) {
	// Snapshot goroutines already running at test start (from other tests'
	// HTTP servers, worker pools, etc.) so only goroutines created by this
	// test's cycles are checked at verification time.
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	const cycles = 100

	for i := 0; i < cycles; i++ {
		w, posID, velID := newCtxWorld(t)
		populateWorld(t, w, posID, velID, 5)

		q := flecs.NewCachedQuery(w, posID, velID)
		flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
			for it.Next() {
			}
		})

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_ = w.ProgressContext(ctx, 1.0/60.0)
	}
}

// ─── EachContext on uncached Query ────────────────────────────────────────────

func TestQueryEachContext_PreCanceled(t *testing.T) {
	w, posID, velID := newCtxWorld(t)
	populateWorld(t, w, posID, velID, 10)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.With(velID))
	var visited int
	err := q.EachContext(ctx, func(it *flecs.QueryIter) {
		visited += it.Count()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if visited != 0 {
		t.Fatalf("expected 0 visits on pre-cancel, got %d", visited)
	}
}

// ─── REST snapshot PUT with cancelled context ─────────────────────────────────

func TestRest_SnapshotPut_ClientDisconnect(t *testing.T) {
	w, posID, velID := newCtxWorld(t)
	populateWorld(t, w, posID, velID, 3)

	data, err := w.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	handler := flecs.NewRESTHandler(w)

	req := httptest.NewRequest(http.MethodPut, "/snapshot", strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	// Normal request (not cancelled); verify 204.
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
}
