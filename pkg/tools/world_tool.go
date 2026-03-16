package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type WorldToolRuntime interface {
	Snapshot(limit int) (interface{}, error)
	Tick(ctx context.Context, source string) (string, error)
	NPCList() ([]map[string]interface{}, error)
	NPCGet(id string) (map[string]interface{}, bool, error)
	EntityList() ([]map[string]interface{}, error)
	EntityGet(id string) (map[string]interface{}, bool, error)
	UpdateEntity(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error)
	UpdateLocation(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error)
	UpdateRoom(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error)
	WorldGet() (map[string]interface{}, error)
	EventLog(limit int) ([]map[string]interface{}, error)
	CreateNPC(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error)
	CreateEntity(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error)
	QuestList() ([]map[string]interface{}, error)
	QuestGet(id string) (map[string]interface{}, bool, error)
	CreateQuest(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error)
}

type WorldTool struct {
	runtime WorldToolRuntime
}

func NewWorldTool(runtime WorldToolRuntime) *WorldTool {
	return &WorldTool{runtime: runtime}
}

func (t *WorldTool) Name() string { return "world" }

func (t *WorldTool) Description() string {
	return "Inspect and drive the world runtime: snapshot, tick, npc_list, npc_get, entity_list, entity_get, entity_update, location_update, room_update, world_get, event_log, npc_create, entity_create, quest_list, quest_get, quest_create."
}

func (t *WorldTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action":        map[string]interface{}{"type": "string", "description": "snapshot|tick|npc_list|npc_get|entity_list|entity_get|entity_update|location_update|room_update|world_get|event_log|npc_create|entity_create|quest_list|quest_get|quest_create"},
			"id":            map[string]interface{}{"type": "string", "description": "npc id for npc_get"},
			"limit":         map[string]interface{}{"type": "integer", "description": "maximum event/snapshot items"},
			"source":        map[string]interface{}{"type": "string", "description": "tick source label"},
			"npc_id":        map[string]interface{}{"type": "string", "description": "new npc id for npc_create"},
			"name":          map[string]interface{}{"type": "string", "description": "display name for npc_create"},
			"persona":       map[string]interface{}{"type": "string", "description": "persona for npc_create"},
			"home_location": map[string]interface{}{"type": "string", "description": "home location for npc_create"},
			"default_goals": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "default goals for npc_create",
			},
			"title":        map[string]interface{}{"type": "string", "description": "quest title"},
			"summary":      map[string]interface{}{"type": "string", "description": "quest summary"},
			"owner_npc_id": map[string]interface{}{"type": "string", "description": "quest owner npc id"},
			"participants": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "quest participants",
			},
			"entity_id":    map[string]interface{}{"type": "string", "description": "entity id"},
			"entity_type":  map[string]interface{}{"type": "string", "description": "entity type"},
			"location_id":  map[string]interface{}{"type": "string", "description": "entity location"},
		},
		"required": []string{"action"},
	}
}

func (t *WorldTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if t == nil || t.runtime == nil {
		return "world runtime not available", nil
	}
	action := strings.ToLower(MapStringArg(args, "action"))
	switch action {
	case "snapshot":
		out, err := t.runtime.Snapshot(MapIntArg(args, "limit", 12))
		if err != nil {
			return "", err
		}
		return marshalWorldToolOutput(out)
	case "tick":
		source := strings.TrimSpace(MapStringArg(args, "source"))
		if source == "" {
			source = "tool"
		}
		return t.runtime.Tick(ctx, source)
	case "npc_list":
		out, err := t.runtime.NPCList()
		if err != nil {
			return "", err
		}
		return marshalWorldToolOutput(out)
	case "npc_get":
		id := strings.TrimSpace(MapStringArg(args, "id"))
		if id == "" {
			id = strings.TrimSpace(MapStringArg(args, "npc_id"))
		}
		if id == "" {
			return "id is required", nil
		}
		out, found, err := t.runtime.NPCGet(id)
		if err != nil {
			return "", err
		}
		if !found {
			return "npc not found", nil
		}
		return marshalWorldToolOutput(out)
	case "entity_list":
		out, err := t.runtime.EntityList()
		if err != nil {
			return "", err
		}
		return marshalWorldToolOutput(out)
	case "entity_get":
		id := strings.TrimSpace(MapStringArg(args, "id"))
		if id == "" {
			id = strings.TrimSpace(MapStringArg(args, "entity_id"))
		}
		if id == "" {
			return "id is required", nil
		}
		out, found, err := t.runtime.EntityGet(id)
		if err != nil {
			return "", err
		}
		if !found {
			return "entity not found", nil
		}
		return marshalWorldToolOutput(out)
	case "world_get":
		out, err := t.runtime.WorldGet()
		if err != nil {
			return "", err
		}
		return marshalWorldToolOutput(out)
	case "event_log":
		out, err := t.runtime.EventLog(MapIntArg(args, "limit", 12))
		if err != nil {
			return "", err
		}
		return marshalWorldToolOutput(out)
	case "npc_create":
		out, err := t.runtime.CreateNPC(ctx, args)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Created NPC %s", strings.TrimSpace(MapStringArg(out, "npc_id"))), nil
	case "entity_create":
		out, err := t.runtime.CreateEntity(ctx, args)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Created entity %s", strings.TrimSpace(MapStringArg(out, "entity_id"))), nil
	case "entity_update":
		out, err := t.runtime.UpdateEntity(ctx, args)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Updated entity %s", strings.TrimSpace(MapStringArg(out, "entity_id"))), nil
	case "location_update":
		out, err := t.runtime.UpdateLocation(ctx, args)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Updated location %s", strings.TrimSpace(MapStringArg(out, "location_id"))), nil
	case "room_update":
		out, err := t.runtime.UpdateRoom(ctx, args)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Updated room %s", strings.TrimSpace(MapStringArg(out, "room_id"))), nil
	case "quest_list":
		out, err := t.runtime.QuestList()
		if err != nil {
			return "", err
		}
		return marshalWorldToolOutput(out)
	case "quest_get":
		id := strings.TrimSpace(MapStringArg(args, "id"))
		if id == "" {
			return "id is required", nil
		}
		out, found, err := t.runtime.QuestGet(id)
		if err != nil {
			return "", err
		}
		if !found {
			return "quest not found", nil
		}
		return marshalWorldToolOutput(out)
	case "quest_create":
		out, err := t.runtime.CreateQuest(ctx, args)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Created quest %s", strings.TrimSpace(MapStringArg(out, "quest_id"))), nil
	default:
		return "unsupported action", nil
	}
}

func marshalWorldToolOutput(v interface{}) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
