package flecs

import (
	"context"
	"log/slog"
	"unsafe"
)

// onDeleteTargetEvent is the struct passed via unsafe.Pointer in the internal
// callback for OnDeleteTarget observers. It carries the target and pairRelID so
// the registered handler fn(fr, target, dependent, pairRelID) can be reconstructed.
type onDeleteTargetEvent struct {
	target    ID
	pairRelID ID
}

// OnDelete registers fn as an observer that fires once per entity whose lifecycle
// is about to end (via Delete or via cleanup-policy cascade), before the existing
// OnRemove hooks run. The handler receives a *Reader because the entity is
// mid-delete; its components are readable but must not be mutated. To mutate
// world state from within the handler, defer via World.Write(fn).
//
// WithYieldExisting is a no-op for OnDelete: delete events are future-only.
// Returns an *Observer handle; call Unsubscribe to stop receiving events.
func OnDelete(w *World, fn func(fr *Reader, e ID)) *Observer {
	return OnDeleteWithOptions(w, ObserverOptions{}, fn)
}

// OnDeleteWithOptions is OnDelete with additional options.
//
// If opts carries WithQuery(terms...), fn fires only for entities whose archetype
// table matches all specified terms at the moment of deletion.
//
// WithYieldExisting is a no-op: delete events have no pre-existing state.
func OnDeleteWithOptions(w *World, opts ObserverOptions, fn func(fr *Reader, e ID)) *Observer {
	w.checkExclusiveAccessWrite()

	callback := func(_ *Writer, e ID, _ unsafe.Pointer) {
		fn(&w.readCapability, e)
	}

	obs := &Observer{w: w, enabled: true}
	node := w.addObserverNode(tableCreateSentinelID, w.eventOnDeleteID, 0, callback)
	node.observer = obs
	if len(opts.filterTerms) > 0 {
		node.multiFilter = &multiTermFilter{
			filterTerms: append([]Term(nil), opts.filterTerms...),
		}
	}
	obs.nodes = append(obs.nodes, node)

	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "observer registered",
			slog.String("event", EventOnDelete.String()))
	}

	// WithYieldExisting is intentionally ignored: no pre-existing deleted entities.

	return obs
}

// OnDeleteTarget registers fn as an observer that fires once per
// (target, dependent, pairRelID) triple during cleanup-policy cascade, before
// the dependent entity is enqueued for delete-or-remove. The handler receives
// a *Reader because the cascade is in progress; mutations must be deferred via
// World.Write(fn).
//
// target is the entity being deleted. dependent is the entity that has a
// relationship pair (pairRelID, target). pairRelID is the relationship entity
// whose cleanup policy is being applied.
//
// WithYieldExisting is a no-op for OnDeleteTarget: cascade events are future-only.
// Returns an *Observer handle; call Unsubscribe to stop receiving events.
func OnDeleteTarget(w *World, fn func(fr *Reader, target ID, dependent ID, pairRelID ID)) *Observer {
	return OnDeleteTargetWithOptions(w, ObserverOptions{}, fn)
}

// OnDeleteTargetWithOptions is OnDeleteTarget with additional options.
//
// If opts carries WithRelationship(relID), fn fires only for cascades where
// pairRelID == relID. This lets observers filter to, for example, only ChildOf
// cascades: OnDeleteTargetWithOptions(w, WithRelationship(w.ChildOf()), fn).
//
// If opts carries WithQuery(terms...), fn fires only for dependent entities whose
// archetype table matches all specified terms.
//
// WithYieldExisting is a no-op: cascade events have no pre-existing state.
func OnDeleteTargetWithOptions(w *World, opts ObserverOptions, fn func(fr *Reader, target ID, dependent ID, pairRelID ID)) *Observer {
	w.checkExclusiveAccessWrite()

	callback := func(_ *Writer, dependent ID, ptr unsafe.Pointer) {
		evt := (*onDeleteTargetEvent)(ptr)
		fn(&w.readCapability, evt.target, dependent, evt.pairRelID)
	}

	obs := &Observer{w: w, enabled: true}
	node := w.addObserverNode(tableCreateSentinelID, w.eventOnDeleteTargetID, 0, callback)
	node.observer = obs
	node.relFilter = opts.filterRelationship
	if len(opts.filterTerms) > 0 {
		node.multiFilter = &multiTermFilter{
			filterTerms: append([]Term(nil), opts.filterTerms...),
		}
	}
	obs.nodes = append(obs.nodes, node)

	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "observer registered",
			slog.String("event", EventOnDeleteTarget.String()))
	}

	// WithYieldExisting is intentionally ignored: no pre-existing cascade events.

	return obs
}

// dispatchOnDeleteObservers fires all active OnDelete observers for entity e.
// Called from deleteOne before the OnRemove hook loop.
// Multi-term filters are evaluated against e's archetype table; if the table
// is nil (empty-archetype entity), multi-filter observers are skipped.
func (w *World) dispatchOnDeleteObservers(e ID) {
	if w.observers == nil {
		return
	}
	key := observerKey{id: tableCreateSentinelID, eventEntity: w.eventOnDeleteID}
	bucket := w.observers[key]
	if bucket == nil {
		return
	}
	for _, n := range bucket.anyEntity {
		if n.removed || (n.observer != nil && !n.observer.enabled) {
			continue
		}
		if n.multiFilter != nil {
			rec := w.index.Get(e)
			if rec == nil || rec.Table == nil {
				continue
			}
			if !tableMatchesTerms(rec.Table, n.multiFilter.filterTerms, n.multiFilter.filterOrGroups) {
				continue
			}
		}
		n.callback(&w.writeCapability, e, nil)
	}
}

// dispatchOnDeleteTargetObservers fires all active OnDeleteTarget observers for
// the given (target, dependent, pairRelID) triple. Called from the DFS loop in
// deleteImmediate before applying the cleanup policy.
func (w *World) dispatchOnDeleteTargetObservers(target, dependent, pairRelID ID) {
	if w.observers == nil {
		return
	}
	key := observerKey{id: tableCreateSentinelID, eventEntity: w.eventOnDeleteTargetID}
	bucket := w.observers[key]
	if bucket == nil {
		return
	}
	evt := onDeleteTargetEvent{target: target, pairRelID: pairRelID}
	ptr := unsafe.Pointer(&evt)
	for _, n := range bucket.anyEntity {
		if n.removed || (n.observer != nil && !n.observer.enabled) {
			continue
		}
		if n.relFilter != 0 && n.relFilter.Index() != pairRelID.Index() {
			continue
		}
		if n.multiFilter != nil {
			rec := w.index.Get(dependent)
			if rec == nil || rec.Table == nil {
				continue
			}
			if !tableMatchesTerms(rec.Table, n.multiFilter.filterTerms, n.multiFilter.filterOrGroups) {
				continue
			}
		}
		n.callback(&w.writeCapability, dependent, ptr)
	}
}

// applyComponentRemoveCascade removes entity e (used as a component) from all
// entities that currently hold it, when e's OnDelete policy is RemoveAction
// (the default). This is called from deleteOne after firing EventOnDelete
// observers and before firing OnRemove hooks on e's own components.
//
// O(entities-with-component): all tables containing e in their type signature
// are snapshotted and each holder entity undergoes archetype migration.
func (w *World) applyComponentRemoveCascade(e ID) {
	// Only apply RemoveAction; skip if Delete or Panic policy is explicitly set.
	if flags, ok := w.cleanupPolicies[e]; ok {
		if flags&(policyOnDeleteDelete|policyOnDeletePanic) != 0 {
			return
		}
	}

	tables := w.compIndex.TablesFor(e)
	if len(tables) == 0 {
		return
	}

	// Snapshot all holder entities before any archetype migrations.
	var holders []ID
	for _, t := range tables {
		for _, entity := range t.Entities() {
			if w.index.IsAlive(entity) {
				holders = append(holders, entity)
			}
		}
	}

	// Remove e from each holder, triggering archetype migration and OnRemove.
	for _, holder := range holders {
		if w.index.IsAlive(holder) {
			removeIDImmediate(w, holder, e)
		}
	}
}
