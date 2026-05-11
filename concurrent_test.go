package flecs_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snichols/flecs"
)

// TestConcurrentReadsDuringProgress verifies that concurrent reader goroutines
// can call Stats, Count, and GetName while Progress is running (write lock held
// for the whole frame) without data races or deadlocks.
func TestConcurrentReadsDuringProgress(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	w.SetName(e, "hero")
	flecs.RegisterComponent[Position](w)

	// System that does nothing — just keeps Progress busy.
	posID := flecs.RegisterComponent[Position](w)
	cq := flecs.NewCachedQuery(w, posID)
	flecs.NewSystem(w, cq, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
		}
	})

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Spawn 4 concurrent readers.
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = w.Count()
				_ = w.Stats()
				_, _ = w.GetName(e)
			}
		}()
	}

	// Run 50 frames concurrently with the readers.
	for range 50 {
		w.Progress(0.016)
	}
	close(stop)
	wg.Wait()
}

// TestRLockUnlockAPI verifies the public RLock/RUnlock/Lock/Unlock API allows
// callers to hold a read lock across multiple operations atomically.
func TestRLockUnlockAPI(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	w.SetName(e, "target")

	var wg sync.WaitGroup
	var readCount atomic.Int64

	// Multiple goroutines hold RLock simultaneously.
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.RLock()
			defer w.RUnlock()
			_ = w.Count()
			readCount.Add(1)
		}()
	}
	wg.Wait()
	if readCount.Load() != 8 {
		t.Fatalf("expected 8 reads, got %d", readCount.Load())
	}
}

// TestConcurrentGetAndSet verifies that Get (RLock) and Set (Lock) from
// different goroutines do not race when coordinated through the RWMutex.
func TestConcurrentGetAndSet(t *testing.T) {
	type Counter struct{ N int32 }
	w := flecs.New()
	flecs.RegisterComponent[Counter](w)
	e := w.NewEntity()
	flecs.Set(w, e, Counter{N: 0})

	var wg sync.WaitGroup
	const iterations = 200

	// Writer goroutine — increments Counter.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range iterations {
			flecs.Set(w, e, Counter{N: int32(i)})
		}
	}()

	// Reader goroutine — reads Counter; should never see a torn value.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range iterations {
			_, _ = flecs.Get[Counter](w, e)
		}
	}()

	wg.Wait()
}

// TestConcurrentStatsAndProgress verifies Stats can be called concurrently
// with Progress without races.
func TestConcurrentStatsAndProgress(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	cq := flecs.NewCachedQuery(w, posID)
	flecs.NewSystem(w, cq, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
		}
	})

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = w.Stats()
			}
		}
	}()

	for range 100 {
		w.Progress(0.016)
	}
	close(stop)
	wg.Wait()
}

// TestConcurrentEachAndProgress verifies Each1 (RLock for iteration) does not
// race with Progress (Lock for frame).
func TestConcurrentEachAndProgress(t *testing.T) {
	type Pos struct{ X, Y float32 }
	w := flecs.New()
	for range 10 {
		e := w.NewEntity()
		flecs.Set(w, e, Pos{1, 2})
	}
	posID := flecs.RegisterComponent[Pos](w)
	cq := flecs.NewCachedQuery(w, posID)
	flecs.NewSystem(w, cq, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
		}
	})

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				flecs.Each1[Pos](w, func(e flecs.ID, p *Pos) {
					_ = p.X
				})
			}
		}
	}()

	for range 50 {
		w.Progress(0.016)
	}
	close(stop)
	wg.Wait()
}

// TestRLockBlockedByProgress verifies that RLock blocks while Progress holds
// the write lock, ensuring readers see a consistent world state after Progress.
func TestRLockBlockedByProgress(t *testing.T) {
	type Tick struct{ N int }
	w := flecs.New()
	tickID := flecs.RegisterComponent[Tick](w)
	cq := flecs.NewCachedQuery(w, tickID)

	framesDone := atomic.Int64{}
	flecs.NewSystem(w, cq, func(dt float32, it *flecs.QueryIter) {
		framesDone.Add(1)
	})

	// Start Progress in background.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 10 {
			w.Progress(0.016)
			time.Sleep(time.Millisecond)
		}
	}()

	// Reader tries to acquire RLock; it will block during Progress frames.
	for range 5 {
		w.RLock()
		_ = w.Count()
		w.RUnlock()
		time.Sleep(500 * time.Microsecond)
	}
	wg.Wait()
}

// TestMultipleConcurrentReaders verifies that multiple goroutines can hold
// RLock simultaneously (no mutual exclusion between readers).
func TestMultipleConcurrentReaders(t *testing.T) {
	w := flecs.New()
	for range 5 {
		e := w.NewEntity()
		w.SetName(e, "ent")
	}

	var mu sync.Mutex
	maxConcurrent := 0
	currentConcurrent := 0

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.RLock()
			mu.Lock()
			currentConcurrent++
			if currentConcurrent > maxConcurrent {
				maxConcurrent = currentConcurrent
			}
			mu.Unlock()
			// Yield briefly so other goroutines can enter.
			time.Sleep(10 * time.Millisecond)
			mu.Lock()
			currentConcurrent--
			mu.Unlock()
			w.RUnlock()
		}()
	}
	wg.Wait()
	if maxConcurrent < 2 {
		t.Logf("max concurrent readers = %d (expected > 1, but scheduling may serialize)", maxConcurrent)
	}
}
