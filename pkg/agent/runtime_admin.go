package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/YspCoder/clawgo/pkg/tools"
)

var runtimeAdminActionAliases = map[string]string{
}

func (al *AgentLoop) HandleRuntimeAdmin(ctx context.Context, action string, args map[string]interface{}) (interface{}, error) {
	if al == nil || al.agentManager == nil {
		return nil, fmt.Errorf("runtime admin is not configured")
	}
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		action = "snapshot"
	}
	if canonical := runtimeAdminActionAliases[action]; canonical != "" {
		action = canonical
	}
	handler := al.runtimeAdminHandlers()[action]
	if handler == nil {
		return nil, fmt.Errorf("unsupported action: %s", action)
	}
	return handler(ctx, args)
}

type runtimeAdminHandler func(context.Context, map[string]interface{}) (interface{}, error)

func (al *AgentLoop) runtimeAdminHandlers() map[string]runtimeAdminHandler {
	sm := al.agentManager
	return map[string]runtimeAdminHandler{
		"snapshot": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			limit := runtimeIntArg(args, "limit", 100)
			snapshot := sm.RuntimeSnapshot(limit)
			if al.worldRuntime != nil && al.worldRuntime.Enabled() {
				if worldSnap, err := al.worldRuntime.Snapshot(limit); err == nil {
					snapshot.World = worldSnap
				}
			}
			return map[string]interface{}{"snapshot": snapshot}, nil
		},
		"world_snapshot": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			out, err := al.worldRuntime.Snapshot(runtimeIntArg(args, "limit", 20))
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"snapshot": out}, nil
		},
		"world_tick": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			out, err := al.worldRuntime.Tick(ctx, fallbackString(runtimeStringArg(args, "source"), "runtime_admin"))
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"message": out}, nil
		},
		"world_player_get": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			return al.worldRuntime.PlayerGet()
		},
		"world_player_action": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			return al.worldRuntime.PlayerAction(ctx, args)
		},
		"world_npc_list": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			items, err := al.worldRuntime.NPCList()
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"items": items}, nil
		},
		"world_npc_get": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			item, found, err := al.worldRuntime.NPCGet(runtimeStringArg(args, "id"))
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"found": found, "item": item}, nil
		},
		"world_entity_list": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			items, err := al.worldRuntime.EntityList()
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"items": items}, nil
		},
		"world_entity_get": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			item, found, err := al.worldRuntime.EntityGet(runtimeStringArg(args, "id"))
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"found": found, "item": item}, nil
		},
		"world_get": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			item, err := al.worldRuntime.WorldGet()
			if err != nil {
				return nil, err
			}
			return item, nil
		},
		"world_room_list": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			items, err := al.worldRuntime.RoomList()
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"items": items}, nil
		},
		"world_room_get": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			item, found, err := al.worldRuntime.RoomGet(runtimeStringArg(args, "id"))
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"found": found, "item": item}, nil
		},
		"world_location_update": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			return al.worldRuntime.UpdateLocation(ctx, args)
		},
		"world_room_update": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			return al.worldRuntime.UpdateRoom(ctx, args)
		},
		"world_event_log": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			items, err := al.worldRuntime.EventLog(runtimeIntArg(args, "limit", 20))
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"items": items}, nil
		},
		"world_npc_create": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			return al.worldRuntime.CreateNPC(ctx, args)
		},
		"world_entity_create": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			return al.worldRuntime.CreateEntity(ctx, args)
		},
		"world_entity_update": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			return al.worldRuntime.UpdateEntity(ctx, args)
		},
		"world_quest_list": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			items, err := al.worldRuntime.QuestList()
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"items": items}, nil
		},
		"world_quest_get": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			item, found, err := al.worldRuntime.QuestGet(runtimeStringArg(args, "id"))
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"found": found, "item": item}, nil
		},
		"world_quest_create": func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			if al.worldRuntime == nil {
				return nil, fmt.Errorf("world runtime is not configured")
			}
			return al.worldRuntime.CreateQuest(ctx, args)
		},
	}
}

func runtimeStringArg(args map[string]interface{}, key string) string {
	return tools.MapStringArg(args, key)
}

func runtimeRawStringArg(args map[string]interface{}, key string) string {
	return tools.MapRawStringArg(args, key)
}

func runtimeIntArg(args map[string]interface{}, key string, fallback int) int {
	return tools.MapIntArg(args, key, fallback)
}

func runtimeBoolArg(args map[string]interface{}, key string) (bool, bool) {
	return tools.MapBoolArg(args, key)
}

func fallbackString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}
