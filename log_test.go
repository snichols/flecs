package flecs_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
)

// testHandler is a slog.Handler that captures records for inspection.
type testHandler struct {
	mu       sync.Mutex
	records  []slog.Record
	minLevel slog.Level
}

func (h *testHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

func (h *testHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *testHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *testHandler) WithGroup(name string) slog.Handler       { return h }

func (h *testHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.records)
}

func (h *testHandler) messages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	msgs := make([]string, len(h.records))
	for i, r := range h.records {
		msgs[i] = r.Message
	}
	return msgs
}

func (h *testHandler) recordAt(i int) slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.records[i]
}

// attrMap extracts all attributes from a slog.Record into a map.
func attrMap(r slog.Record) map[string]slog.Value {
	m := make(map[string]slog.Value)
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value
		return true
	})
	return m
}

// hasMessage reports whether msgs contains target.
func hasMessage(msgs []string, target string) bool {
	for _, m := range msgs {
		if m == target {
			return true
		}
	}
	return false
}

// logType is a test component for log tests.
type logType struct{ X, Y float32 }

// TestLoggerDefault: no panic when performing operations without a logger.
func TestLoggerDefault(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.RegisterComponent[logType](w)
	flecs.Set(w, e, logType{1, 2})
	w.Delete(e)
}

// TestLoggerSetThenUnset: after SetLogger(nil), no logs fire.
func TestLoggerSetThenUnset(t *testing.T) {
	w := flecs.New()
	h := &testHandler{minLevel: slog.LevelDebug}
	w.SetLogger(slog.New(h))
	w.SetLogger(nil)

	_ = w.NewEntity()

	if h.count() != 0 {
		t.Errorf("expected 0 records after SetLogger(nil), got %d: %v", h.count(), h.messages())
	}
}

// TestLoggerGetterSetter: Logger() returns the installed logger.
func TestLoggerGetterSetter(t *testing.T) {
	w := flecs.New()
	if w.Logger() != nil {
		t.Error("expected nil logger by default")
	}
	l := slog.Default()
	w.SetLogger(l)
	if w.Logger() != l {
		t.Error("Logger() should return the installed logger")
	}
	w.SetLogger(nil)
	if w.Logger() != nil {
		t.Error("Logger() should return nil after SetLogger(nil)")
	}
}

// TestLoggerEntityCreated: "entity created" fires with correct id attr.
func TestLoggerEntityCreated(t *testing.T) {
	w := flecs.New()
	h := &testHandler{minLevel: slog.LevelDebug}
	w.SetLogger(slog.New(h))

	e := w.NewEntity()

	if h.count() != 1 {
		t.Fatalf("expected 1 record, got %d: %v", h.count(), h.messages())
	}
	r := h.recordAt(0)
	if r.Message != "entity created" {
		t.Errorf("message: got %q, want %q", r.Message, "entity created")
	}
	attrs := attrMap(r)
	if v, ok := attrs["id"]; !ok || v.Uint64() != uint64(e) {
		t.Errorf("id attr: got %v, want %d", attrs["id"], uint64(e))
	}
}

// TestLoggerEntityDeleted: create + delete produces entity created and entity deleted records.
func TestLoggerEntityDeleted(t *testing.T) {
	w := flecs.New()
	h := &testHandler{minLevel: slog.LevelDebug}
	w.SetLogger(slog.New(h))

	e := w.NewEntity()
	w.Delete(e)

	msgs := h.messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 records, got %d: %v", len(msgs), msgs)
	}
	if msgs[0] != "entity created" {
		t.Errorf("record[0]: got %q, want %q", msgs[0], "entity created")
	}
	if msgs[1] != "entity deleted" {
		t.Errorf("record[1]: got %q, want %q", msgs[1], "entity deleted")
	}
	attrs := attrMap(h.recordAt(1))
	if v, ok := attrs["id"]; !ok || v.Uint64() != uint64(e) {
		t.Errorf("deleted id attr: got %v, want %d", attrs["id"], uint64(e))
	}
}

// TestLoggerComponentRegistered: "component registered" fires with name, id, size attrs.
func TestLoggerComponentRegistered(t *testing.T) {
	w := flecs.New()
	h := &testHandler{minLevel: slog.LevelDebug}
	w.SetLogger(slog.New(h))

	id := flecs.RegisterComponent[logType](w)

	msgs := h.messages()
	idx := -1
	for i, m := range msgs {
		if m == "component registered" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("no 'component registered' record; messages: %v", msgs)
	}
	attrs := attrMap(h.recordAt(idx))
	if v, ok := attrs["id"]; !ok || v.Uint64() != uint64(id) {
		t.Errorf("id attr: got %v, want %d", attrs["id"], uint64(id))
	}
	if _, ok := attrs["name"]; !ok {
		t.Error("missing 'name' attr")
	}
	if _, ok := attrs["size"]; !ok {
		t.Error("missing 'size' attr")
	}
}

// TestLoggerComponentRegisteredIdempotent: second RegisterComponent call does not log.
func TestLoggerComponentRegisteredIdempotent(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[logType](w) // first registration before logger

	h := &testHandler{minLevel: slog.LevelDebug}
	w.SetLogger(slog.New(h))

	flecs.RegisterComponent[logType](w) // idempotent — no log

	if hasMessage(h.messages(), "component registered") {
		t.Errorf("unexpected 'component registered' on second call; messages: %v", h.messages())
	}
}

// TestLoggerTableCreated: "table created" fires when a new archetype is created.
func TestLoggerTableCreated(t *testing.T) {
	w := flecs.New()
	h := &testHandler{minLevel: slog.LevelDebug}
	w.SetLogger(slog.New(h))

	e := w.NewEntity()
	flecs.Set(w, e, logType{1, 2}) // migration → new table for [logType]

	if !hasMessage(h.messages(), "table created") {
		t.Errorf("no 'table created' record; messages: %v", h.messages())
	}
}

// TestLoggerSystemAdded: "system added" fires after NewSystem.
func TestLoggerSystemAdded(t *testing.T) {
	w := flecs.New()
	h := &testHandler{minLevel: slog.LevelDebug}
	w.SetLogger(slog.New(h))

	posID := flecs.RegisterComponent[logType](w)
	q := flecs.NewCachedQuery(w, posID)
	flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {})

	if !hasMessage(h.messages(), "system added") {
		t.Errorf("no 'system added' record; messages: %v", h.messages())
	}
}

// TestLoggerSystemAddedPhase: "system added" includes the correct phase attribute.
func TestLoggerSystemAddedPhase(t *testing.T) {
	w := flecs.New()
	h := &testHandler{minLevel: slog.LevelDebug}
	w.SetLogger(slog.New(h))

	posID := flecs.RegisterComponent[logType](w)
	q := flecs.NewCachedQuery(w, posID)
	flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(dt float32, it *flecs.QueryIter) {})

	msgs := h.messages()
	idx := -1
	for i, m := range msgs {
		if m == "system added" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("no 'system added' record; messages: %v", msgs)
	}
	attrs := attrMap(h.recordAt(idx))
	if v, ok := attrs["phase"]; !ok || v.String() != "PreUpdate" {
		t.Errorf("phase attr: got %v, want PreUpdate", attrs["phase"])
	}
}

// TestLoggerSystemClosed: "system closed" fires on first Close only.
func TestLoggerSystemClosed(t *testing.T) {
	w := flecs.New()
	h := &testHandler{minLevel: slog.LevelDebug}
	w.SetLogger(slog.New(h))

	posID := flecs.RegisterComponent[logType](w)
	q := flecs.NewCachedQuery(w, posID)
	sys := flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {})

	beforeClose := h.count()
	sys.Close()

	if !hasMessage(h.messages()[beforeClose:], "system closed") {
		t.Errorf("no 'system closed' record; messages after close: %v", h.messages()[beforeClose:])
	}

	afterClose := h.count()
	sys.Close() // idempotent — no second log
	if h.count() != afterClose {
		t.Errorf("Close() idempotent: expected %d records, got %d", afterClose, h.count())
	}
}

// TestLoggerObserverRegistered: "observer registered" fires from Observe.
func TestLoggerObserverRegistered(t *testing.T) {
	w := flecs.New()
	h := &testHandler{minLevel: slog.LevelDebug}
	w.SetLogger(slog.New(h))

	obs := flecs.Observe[logType](w, flecs.EventOnAdd, func(e flecs.ID, v *logType) {})
	_ = obs

	if !hasMessage(h.messages(), "observer registered") {
		t.Errorf("no 'observer registered' record; messages: %v", h.messages())
	}
}

// TestLoggerObserverRegisteredID: "observer registered" fires from ObserveID.
func TestLoggerObserverRegisteredID(t *testing.T) {
	w := flecs.New()
	id := flecs.RegisterComponent[logType](w)
	h := &testHandler{minLevel: slog.LevelDebug}
	w.SetLogger(slog.New(h))

	obs := flecs.ObserveID(w, id, flecs.EventOnSet, func(e flecs.ID, ptr unsafe.Pointer) {})
	_ = obs

	if !hasMessage(h.messages(), "observer registered") {
		t.Errorf("no 'observer registered' record; messages: %v", h.messages())
	}
}

// TestLoggerObserverRegisteredObserve2: "observer registered" fires once per event in Observe2.
func TestLoggerObserverRegisteredObserve2(t *testing.T) {
	w := flecs.New()
	h := &testHandler{minLevel: slog.LevelDebug}
	w.SetLogger(slog.New(h))

	events := []flecs.EventKind{flecs.EventOnAdd, flecs.EventOnRemove}
	obs := flecs.Observe2[logType](w, events, func(ev flecs.EventKind, e flecs.ID, v *logType) {})
	_ = obs

	msgs := h.messages()
	count := 0
	for _, m := range msgs {
		if m == "observer registered" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 'observer registered' records for 2 events, got %d; messages: %v", count, msgs)
	}
}

// TestLoggerObserverEventAttr: "observer registered" includes correct event attr.
func TestLoggerObserverEventAttr(t *testing.T) {
	w := flecs.New()
	h := &testHandler{minLevel: slog.LevelDebug}
	w.SetLogger(slog.New(h))

	flecs.Observe[logType](w, flecs.EventOnSet, func(e flecs.ID, v *logType) {})

	msgs := h.messages()
	idx := -1
	for i, m := range msgs {
		if m == "observer registered" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("no 'observer registered' record")
	}
	attrs := attrMap(h.recordAt(idx))
	if v, ok := attrs["event"]; !ok || v.String() != "OnSet" {
		t.Errorf("event attr: got %v, want OnSet", attrs["event"])
	}
}

// TestLoggerObserverUnsubscribed: "observer unsubscribed" fires on first Unsubscribe only.
func TestLoggerObserverUnsubscribed(t *testing.T) {
	w := flecs.New()
	h := &testHandler{minLevel: slog.LevelDebug}
	w.SetLogger(slog.New(h))

	obs := flecs.Observe[logType](w, flecs.EventOnAdd, func(e flecs.ID, v *logType) {})
	beforeUnsub := h.count()

	obs.Unsubscribe()

	if !hasMessage(h.messages()[beforeUnsub:], "observer unsubscribed") {
		t.Errorf("no 'observer unsubscribed' record; messages: %v", h.messages()[beforeUnsub:])
	}

	afterUnsub := h.count()
	obs.Unsubscribe() // idempotent — no second log
	if h.count() != afterUnsub {
		t.Errorf("Unsubscribe idempotent: expected %d records, got %d", afterUnsub, h.count())
	}
}

// TestLoggerSnapshotSerialized: "snapshot serialized" fires after MarshalJSON.
func TestLoggerSnapshotSerialized(t *testing.T) {
	w := flecs.New()
	h := &testHandler{minLevel: slog.LevelDebug}
	w.SetLogger(slog.New(h))

	_ = w.NewEntity()

	data, err := w.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	_ = data

	if !hasMessage(h.messages(), "snapshot serialized") {
		t.Errorf("no 'snapshot serialized' record; messages: %v", h.messages())
	}
}

// TestLoggerSnapshotLoaded: "snapshot loaded" fires after UnmarshalJSON.
func TestLoggerSnapshotLoaded(t *testing.T) {
	w := flecs.New()
	_ = w.NewEntity()
	data, err := w.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	w2 := flecs.New()
	h := &testHandler{minLevel: slog.LevelDebug}
	w2.SetLogger(slog.New(h))

	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatal(err)
	}

	if !hasMessage(h.messages(), "snapshot loaded") {
		t.Errorf("no 'snapshot loaded' record; messages: %v", h.messages())
	}
}

// TestLoggerSnapshotEntityCount: "snapshot serialized" and "snapshot loaded" include entities count.
func TestLoggerSnapshotEntityCount(t *testing.T) {
	w := flecs.New()
	_ = w.NewEntity()
	_ = w.NewEntity()

	h := &testHandler{minLevel: slog.LevelDebug}
	w.SetLogger(slog.New(h))

	data, err := w.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	_ = data

	msgs := h.messages()
	idx := -1
	for i, m := range msgs {
		if m == "snapshot serialized" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("no 'snapshot serialized' record")
	}
	attrs := attrMap(h.recordAt(idx))
	if v, ok := attrs["entities"]; !ok || v.Int64() != 2 {
		t.Errorf("entities attr: got %v, want 2", attrs["entities"])
	}
}

// TestLoggerLevelFilter: DEBUG records do not reach an INFO-level handler.
func TestLoggerLevelFilter(t *testing.T) {
	w := flecs.New()
	h := &testHandler{minLevel: slog.LevelInfo}
	w.SetLogger(slog.New(h))

	e := w.NewEntity()
	flecs.RegisterComponent[logType](w)
	flecs.Set(w, e, logType{1, 2})

	if h.count() != 0 {
		t.Errorf("expected 0 records at INFO level for DEBUG events, got %d: %v", h.count(), h.messages())
	}
}

// TestLoggerNoLogOnReadPaths: Get/Has/IsAlive produce no log records.
func TestLoggerNoLogOnReadPaths(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[logType](w)
	e := w.NewEntity()
	flecs.Set(w, e, logType{1, 2})

	h := &testHandler{minLevel: slog.LevelDebug}
	w.SetLogger(slog.New(h))

	_, _ = flecs.Get[logType](w, e)
	_ = flecs.Has[logType](w, e)
	_ = w.IsAlive(e)

	if h.count() != 0 {
		t.Errorf("expected 0 records on read paths, got %d: %v", h.count(), h.messages())
	}
}

// TestLoggerNoLogOnHotPaths: 1000 same-component Set calls fire no log records.
func TestLoggerNoLogOnHotPaths(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w, e, logType{1, 2}) // initial set; triggers migration

	h := &testHandler{minLevel: slog.LevelDebug}
	w.SetLogger(slog.New(h))

	for range 1000 {
		flecs.Set(w, e, logType{2, 3}) // re-set on existing component; no migration
	}

	if h.count() != 0 {
		t.Errorf("expected 0 records for 1000 same-component Set calls, got %d: %v", h.count(), h.messages())
	}
}
