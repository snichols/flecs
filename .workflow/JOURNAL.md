## build iteration 1 (2026-05-11)

Phase 10.2 complete: World-level RWMutex implemented, all tests pass clean with race detector (-count=2)

## iterate iteration 2 (2026-05-11)

Closing as blocked: requirement #9 (Each1–Each4 acquire RLock) has an unresolved design conflict with the existing Defer-wrapping pattern. Adding w.RLock()/defer w.RUnlock() to Each1–Each4 deadlocks in TestDeferWrappedIteration, where the user wraps Each1 with w.Defer() and calls w.Delete() from inside the callback. The Delete call checks inProgress (false at that point) and tries to acquire Lock, but the same goroutine holds RLock from Each1 → deadlock. The issue says "use Defer" for mutations from callbacks, but the Defer+Each combination itself deadlocks. Two design paths forward: (A) restructure mutators to check deferDepth before acquiring rwmu — safe to enqueue without the write lock, protected by deferMu; (B) have Each1–Each4 skip RLock when deferDepth > 0, accepting that Defer-wrapped iteration runs unlocked. Both require a deliberate design decision and updates to TestDeferWrappedIteration. Needs human decision before re-queuing.

