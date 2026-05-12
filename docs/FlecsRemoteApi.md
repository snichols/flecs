# REST API

<!-- TODO: port from /work/agents/claude/projects/SanderMertens/flecs/docs/FlecsRemoteApi.md (Phase 14.9) -->

This manual covers the REST API addon: `NewRESTHandler`, available endpoints, snapshot save/load, and integration with `net/http`.

### Current Go endpoints (v0.18.0)

| Method | Path | Description |
|---|---|---|
| GET | `/stats` | World stats snapshot |
| GET | `/components` | All registered component infos |
| GET | `/components/{id}` | Single component info by uint64 ID |
| GET | `/entities` | Alive entities (optional `?limit=N`, default 1000, max 10 000) |
| GET | `/entities/{id}` | Entity detail (name, components, parent, prefabs, pairs) |
| GET | `/snapshot` | Full `MarshalJSON` world snapshot |
| PUT | `/snapshot` | Load a snapshot (replaces world state) |

### Feature gaps vs. upstream C

The upstream C REST API (`FlecsExplorer`) provides a full entity browser, query execution, and live component editing. The Go port provides read-only inspection plus snapshot save/load only.

See the [Quickstart](Quickstart.md) for a hands-on introduction to the available REST endpoints.
