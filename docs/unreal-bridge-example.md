# Unreal Bridge Example

This file describes a minimal Unreal-side bridge for `clawgo`.

Goal:

- render the same world as web
- keep `clawgo` authoritative
- let Unreal submit player actions and god-view edits

## Recommended Modules

Create three Unreal-side layers:

1. `UClawgoWorldClient`
- owns HTTP and WebSocket connections
- fetches `/api/world`
- subscribes to `/api/runtime`
- sends `world_player_action`
- sends `world_entity_update`

2. `AClawgoWorldManager`
- stores the latest normalized world snapshot
- maps locations / NPCs / entities / rooms to spawned actors
- applies interpolation

3. `UClawgoAssetResolver`
- maps logical model keys to Unreal assets
- example:
  - `npc.main` -> `/Game/World/Characters/BP_Main`
  - `entity.table` -> `/Game/World/Furniture/BP_Table`
  - `room.task` -> `/Game/World/Rooms/BP_TaskRoom`

## Recommended UE Data Structures

Use simple `USTRUCT`s mirroring the web/client contract.

```cpp
USTRUCT(BlueprintType)
struct FClawgoPlacement
{
    GENERATED_BODY()

    UPROPERTY(BlueprintReadOnly) FString Model;
    UPROPERTY(BlueprintReadOnly) FVector Scale = FVector(1.0, 1.0, 1.0);
    UPROPERTY(BlueprintReadOnly) FVector RotationEuler = FVector::ZeroVector;
    UPROPERTY(BlueprintReadOnly) FVector Offset = FVector::ZeroVector;
};

USTRUCT(BlueprintType)
struct FClawgoLocation
{
    GENERATED_BODY()

    UPROPERTY(BlueprintReadOnly) FString Id;
    UPROPERTY(BlueprintReadOnly) FString Name;
    UPROPERTY(BlueprintReadOnly) FString Description;
    UPROPERTY(BlueprintReadOnly) TArray<FString> Neighbors;
};

USTRUCT(BlueprintType)
struct FClawgoEntity
{
    GENERATED_BODY()

    UPROPERTY(BlueprintReadOnly) FString Id;
    UPROPERTY(BlueprintReadOnly) FString Name;
    UPROPERTY(BlueprintReadOnly) FString Type;
    UPROPERTY(BlueprintReadOnly) FString LocationId;
    UPROPERTY(BlueprintReadOnly) FClawgoPlacement Placement;
    UPROPERTY(BlueprintReadOnly) TMap<FString, FString> StateText;
};

USTRUCT(BlueprintType)
struct FClawgoNPCState
{
    GENERATED_BODY()

    UPROPERTY(BlueprintReadOnly) FString NpcId;
    UPROPERTY(BlueprintReadOnly) FString CurrentLocation;
    UPROPERTY(BlueprintReadOnly) FString CurrentRoomId;
    UPROPERTY(BlueprintReadOnly) FString Mood;
    UPROPERTY(BlueprintReadOnly) FString Status;
};

USTRUCT(BlueprintType)
struct FClawgoRoom
{
    GENERATED_BODY()

    UPROPERTY(BlueprintReadOnly) FString Id;
    UPROPERTY(BlueprintReadOnly) FString Name;
    UPROPERTY(BlueprintReadOnly) FString LocationId;
    UPROPERTY(BlueprintReadOnly) FString Status;
    UPROPERTY(BlueprintReadOnly) FString TaskSummary;
    UPROPERTY(BlueprintReadOnly) TArray<FString> AssignedNpcIds;
};
```

## World Snapshot Normalization

On Unreal side, normalize the payload exactly once.

Recommended normalized snapshot:

```cpp
struct FClawgoWorldSnapshot
{
    FString WorldId;
    int64 Tick = 0;
    TMap<FString, FClawgoLocation> Locations;
    TMap<FString, FClawgoEntity> Entities;
    TMap<FString, FClawgoNPCState> NPCStates;
    TMap<FString, TArray<FString>> Occupancy;
    TMap<FString, TArray<FString>> EntityOccupancy;
    TMap<FString, TArray<FString>> RoomOccupancy;
    TMap<FString, FClawgoRoom> Rooms;
};
```

## Spawn Rules

Recommended actor ownership:

- location -> `AClawgoLocationAnchor`
- entity -> `AClawgoEntityActor`
- npc -> `AClawgoNPCActor`
- room -> `AClawgoRoomActor`

Spawn keys:

- location actor key: `location:<id>`
- entity actor key: `entity:<id>`
- npc actor key: `npc:<id>`
- room actor key: `room:<id>`

## Placement Rules

Final transform should be resolved from:

- location anchor transform
- plus `entity.placement.offset`
- plus yaw from `entity.placement.rotation`
- plus scale from `entity.placement.scale`

Recommended interpretation:

- `rotation[1]` is yaw in radians
- convert radians to Unreal degrees before applying

## Sync Loop

Recommended runtime behavior:

1. On boot:
- fetch `/api/world`
- build initial actors

2. On websocket snapshot:
- if `tick` unchanged, ignore
- if `tick` advanced:
  - update normalized snapshot
  - diff actors
  - move actors by interpolation

3. If websocket disconnects:
- keep a low-frequency HTTP refresh fallback

## Action Writeback

### Player action example

```json
{
  "action": "world_player_action",
  "player_action": "move",
  "location_id": "square"
}
```

### God-view entity move example

```json
{
  "action": "world_entity_update",
  "id": "bench",
  "location_id": "commons",
  "model": "entity.table",
  "rotation_y": 1.57,
  "scale": [1.2, 1.2, 1.2],
  "offset": [0.8, 0, -0.4]
}
```

## NPC Animation Mapping

Recommended mapping:

- `status == "working"` -> `Interact`
- recent `npc_speak` event -> `Talk`
- location changed since last tick -> `Walk`
- otherwise -> `Idle`

## Room Rendering

Rooms should not replace the open world.

Recommended behavior:

- world view shows room portals / room pods
- selecting a room opens its isolated sub-scene
- NPCs assigned to the room appear inside while still remaining world-owned

## Unreal Asset Resolver

Keep a simple table or `UDataAsset`.

Example:

```cpp
TMap<FString, TSoftClassPtr<AActor>> ActorBlueprints;
TMap<FString, TSoftObjectPtr<UStaticMesh>> StaticMeshes;
TMap<FString, TSoftObjectPtr<USkeletalMesh>> SkeletalMeshes;
```

Logical keys should stay aligned with web:

- `npc.main`
- `npc.base`
- `entity.table`
- `entity.chair`
- `entity.crate`
- `room.task`
- `location.plaza`

## Deployment Advice

Do not make Unreal the world server.

Keep:

- `clawgo` as authority
- Unreal as client
- web as client

This avoids:

- divergent NPC logic
- duplicated quest logic
- different room assignment behavior across clients
