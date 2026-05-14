## iterate iteration 1 (2026-05-14)

Corrected the misleading docs/README.md gap entry at line 152: reclassified OnDelete/OnDeleteTarget from "observer events not yet ported" to ✅ shipped in v0.32.0, with explanation that they are cleanup-policy relationship traits (not observer event kinds), cross-links to world.go:63–64, cleanup.go, and ComponentTraits.md, and a note that EventOnRemove is the correct deletion-reactive observer event. Added a "No Dedicated OnDelete/OnDeleteTarget Observer Events" callout section to docs/ObserversManual.md after the OnRemove Observers section. No code changes. go vet / go test -race -count=3 pass clean.

