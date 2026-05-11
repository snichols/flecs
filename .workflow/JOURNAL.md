## iterate iteration 1 (2026-05-11)

Partial implementation of rwmu concurrent reader/writer locking. Added rwmu sync.RWMutex + inFlush bool to World, 4 public RLock/RUnlock/Lock/Unlock methods, RLock in Each1-4, write-lock at immediate-mutation tail in Delete/Set/Remove/AddID/RemoveID/SetPair/SetByID, flush-level locking in DeferEnd with inFlush flag to prevent observer re-entrancy deadlock, RLock in REST read handlers, RLock/Lock in MarshalJSON/UnmarshalJSON. Blocked: user requests snichols:manager take over with a new approach before writing concurrent_test.go, docs, and running final acceptance tests.

