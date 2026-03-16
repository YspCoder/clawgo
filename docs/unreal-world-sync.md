# Unreal World Sync

This document defines the recommended integration contract between `clawgo` and an Unreal Engine client.

See also:

- [Unreal Bridge Example](/Users/lpf/Desktop/project/clawgo/docs/unreal-bridge-example.md)

## Authority

- `clawgo` remains the authoritative world server.
- Unreal is a renderer and input client.
- Web and Unreal should consume the same world APIs and action APIs.

## Read Path

Unreal should subscribe to:

- `GET /api/world`
- runtime websocket snapshots from `/api/runtime`

Key world payloads to consume:

- `world_id`
- `tick`
- `locations`
- `entities`
- `rooms`
- `occupancy`
- `entity_occupancy`
- `room_occupancy`
- `npc_states`
- `recent_events`

## Write Path

Player-originated actions should map to:

- `world_player_action`
  - `move`
  - `speak`
  - `interact`
  - `quest_accept`
  - `quest_progress`
  - `quest_complete`

World admin / god-view edits should map to:

- `world_entity_update`
- `world_entity_create`
- `world_npc_create`
- `world_quest_create`

## Placement Contract

Entities expose a stable placement payload:

```json
{
  "id": "bench",
  "location_id": "commons",
  "placement": {
    "model": "entity.table",
    "scale": [1.2, 1.2, 1.2],
    "rotation": [0, 1.57, 0],
    "offset": [0.8, 0, -0.4]
  }
}
```

Interpretation:

- `model`: shared logical asset key
- `scale`: local model scale
- `rotation`: radians, XYZ order
- `offset`: local offset inside the owning location

Unreal should treat `location_id + offset` as the effective placement anchor.

## Asset Pipeline

Recommended source workflow:

1. Author in Blender / Maya / DCC source files.
2. Export web assets as `GLB`.
3. Export Unreal assets as `FBX` or Unreal-preferred import format.
4. Keep the logical asset key the same across clients.

Example:

- logical key: `entity.table`
- web asset: `/models/furniture/table.glb`
- unreal asset: `/Game/World/Furniture/SM_Table`

## Animation Contract

Recommended shared action set for humanoid characters:

- `Idle`
- `Walk`
- `Talk`
- `Interact`

Unreal should choose animations from world state intent/status:

- idle state -> `Idle`
- moving between anchors -> `Walk`
- speaking event -> `Talk`
- room/task work -> `Interact`

## Rooms

Rooms are task execution spaces created by the world mind.

Unreal should render them as:

- isolated interior pods
- task sub-scenes
- linked challenge spaces

Use:

- `rooms`
- `room_occupancy`

to render membership and lifecycle.

## Replication Model

Do not replicate game authority into Unreal.

Recommended flow:

1. Unreal receives latest snapshot.
2. Unreal interpolates transforms locally.
3. Unreal submits user actions back to `clawgo`.
4. `clawgo` arbitrates and publishes the new truth.

This keeps:

- world logic in one place
- web and Unreal behavior aligned
- NPC autonomy consistent across clients
