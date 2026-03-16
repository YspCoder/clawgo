package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YspCoder/clawgo/pkg/tools"
	"github.com/YspCoder/clawgo/pkg/world"
)

type WorldRuntime struct {
	store         *world.Store
	engine        *world.Engine
	profiles      *tools.AgentProfileStore
	dispatcher    *tools.AgentDispatcher
	manager       *tools.AgentManager
	maxCatchUp    int
	maxNPCPerTick int
	tickMu        sync.Mutex
}

func NewWorldRuntime(workspace string, profiles *tools.AgentProfileStore, dispatcher *tools.AgentDispatcher, manager *tools.AgentManager) *WorldRuntime {
	return &WorldRuntime{
		store:         world.NewStore(workspace),
		engine:        world.NewEngine(),
		profiles:      profiles,
		dispatcher:    dispatcher,
		manager:       manager,
		maxCatchUp:    3,
		maxNPCPerTick: 8,
	}
}

func (wr *WorldRuntime) Enabled() bool {
	if wr == nil {
		return false
	}
	_ = wr.ensureMainProfile()
	profiles, err := wr.worldProfiles()
	return err == nil && len(profiles) > 0
}

func (wr *WorldRuntime) AutoTick(ctx context.Context, source string) (string, error) {
	return wr.Tick(ctx, source)
}

func (wr *WorldRuntime) autonomyEnabled() bool {
	return wr != nil && wr.manager != nil && wr.manager.HasProvider()
}

func (wr *WorldRuntime) ensureMainProfile() error {
	if wr == nil || wr.profiles == nil {
		return nil
	}
	if _, ok, err := wr.profiles.Get("main"); err != nil {
		return err
	} else if ok {
		return nil
	}
	_, err := wr.profiles.Upsert(tools.AgentProfile{
		AgentID:         "main",
		Name:            "main",
		Kind:            "npc",
		Role:            "world-mind",
		Persona:         "The world mind that maintains continuity, delegates work, and protects coherence.",
		HomeLocation:    "commons",
		DefaultGoals:    []string{"maintain_world", "seed_story", "coordinate_npcs"},
		MemoryNamespace: "main",
		Status:          "active",
		WorldTags:       []string{"world_mind"},
		PerceptionScope: 2,
	})
	return err
}

func (wr *WorldRuntime) ensureAutonomousSeed(state *world.WorldState, npcStates map[string]world.NPCState) error {
	if state == nil {
		return nil
	}
	if err := wr.ensureMainProfile(); err != nil {
		return err
	}
	if !wr.autonomyEnabled() {
		return nil
	}
	for _, seed := range []struct {
		id       string
		name     string
		persona  string
		location string
		goals    []string
	}{
		{"caretaker", "Caretaker", "Keeps the commons stable, opens rooms, and keeps tasks moving.", "commons", []string{"maintain_commons", "support_main"}},
		{"merchant", "Merchant", "Trades, travels between square and commons, and creates local needs.", "square", []string{"trade", "seek_visitors"}},
	} {
		if len(npcStates) >= 3 {
			break
		}
		if _, exists := npcStates[seed.id]; exists {
			continue
		}
		if _, err := wr.CreateNPC(context.Background(), map[string]interface{}{
			"npc_id":         seed.id,
			"name":           seed.name,
			"persona":        seed.persona,
			"home_location":  seed.location,
			"default_goals":  seed.goals,
			"role":           "npc",
		}); err != nil {
			return err
		}
		npcStates[seed.id] = wr.engine.EnsureNPCState(world.NPCBlueprint{
			NPCID:        seed.id,
			DisplayName:  seed.name,
			Kind:         "npc",
			Role:         "npc",
			Persona:      seed.persona,
			HomeLocation: seed.location,
			DefaultGoals: seed.goals,
		}, npcStates[seed.id])
	}
	if len(state.ActiveQuests) == 0 && state.Clock.Tick >= 2 {
		questID := "stabilize-commons"
		if _, exists := state.ActiveQuests[questID]; !exists {
			state.ActiveQuests[questID] = world.QuestState{
				ID:           questID,
				Title:        "Stabilize Commons",
				Status:       "open",
				OwnerNPCID:   "main",
				Participants: []string{"main", "caretaker"},
				Summary:      "Inspect commons and keep the world coherent.",
			}
		}
	}
	return nil
}

func roomIDForQuest(questID string) string {
	questID = normalizeWorldID(questID)
	if questID == "" {
		return ""
	}
	return "room-" + questID
}

func normalizeNPCIDs(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = normalizeWorldID(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func runtimeEntityPlacementArg(args map[string]interface{}) *world.EntityPlacement {
	if args == nil {
		return nil
	}
	stateArgs, _ := args["state"].(map[string]interface{})
	placement := &world.EntityPlacement{}
	if model := strings.TrimSpace(fmt.Sprint(firstNonNil(args["model"], stateArgs["model"]))); model != "" && model != "<nil>" {
		placement.Model = model
	}
	if scale, ok := runtimeTupleArg(args, "scale"); ok {
		placement.Scale = scale
	} else if scale, ok := runtimeTupleMap(stateArgs, "scale"); ok {
		placement.Scale = scale
	}
	if rotation, ok := runtimeTupleArg(args, "rotation"); ok {
		placement.Rotation = rotation
	} else if rotation, ok := runtimeTupleMap(stateArgs, "rotation"); ok {
		placement.Rotation = rotation
	} else if rotationY := strings.TrimSpace(fmt.Sprint(firstNonNil(args["rotation_y"], stateArgs["rotation_y"]))); rotationY != "" && rotationY != "<nil>" {
		placement.Rotation = [3]float64{0, runtimeNumberArg(firstNonNil(args["rotation_y"], stateArgs["rotation_y"])), 0}
	}
	if offset, ok := runtimeTupleArg(args, "offset"); ok {
		placement.Offset = offset
	} else if offset, ok := runtimeTupleMap(stateArgs, "offset"); ok {
		placement.Offset = offset
	} else if args["offset_x"] != nil || args["offset_y"] != nil || args["offset_z"] != nil {
		placement.Offset = [3]float64{
			runtimeNumberArg(args["offset_x"]),
			runtimeNumberArg(args["offset_y"]),
			runtimeNumberArg(args["offset_z"]),
		}
	} else if stateArgs["offset_x"] != nil || stateArgs["offset_y"] != nil || stateArgs["offset_z"] != nil {
		placement.Offset = [3]float64{
			runtimeNumberArg(stateArgs["offset_x"]),
			runtimeNumberArg(stateArgs["offset_y"]),
			runtimeNumberArg(stateArgs["offset_z"]),
		}
	}
	if placement.Model == "" && placement.Scale == [3]float64{} && placement.Rotation == [3]float64{} && placement.Offset == [3]float64{} {
		return nil
	}
	return placement
}

func runtimeTupleArg(args map[string]interface{}, key string) ([3]float64, bool) {
	var out [3]float64
	raw, ok := args[key]
	if !ok {
		return out, false
	}
	items, ok := raw.([]interface{})
	if !ok || len(items) < 3 {
		return out, false
	}
	for i := 0; i < 3; i++ {
		out[i] = runtimeNumberArg(items[i])
	}
	return out, true
}

func runtimeTupleMap(args map[string]interface{}, key string) ([3]float64, bool) {
	var out [3]float64
	if args == nil {
		return out, false
	}
	raw, ok := args[key]
	if !ok {
		return out, false
	}
	items, ok := raw.(map[string]interface{})
	if !ok {
		array, ok := raw.([]interface{})
		if !ok || len(array) < 3 {
			return out, false
		}
		for i := 0; i < 3; i++ {
			out[i] = runtimeNumberArg(array[i])
		}
		return out, true
	}
	if items["x"] == nil || items["y"] == nil || items["z"] == nil {
		return out, false
	}
	out[0] = runtimeNumberArg(items["x"])
	out[1] = runtimeNumberArg(items["y"])
	out[2] = runtimeNumberArg(items["z"])
	return out, true
}

func firstNonNil(values ...interface{}) interface{} {
	for _, value := range values {
		if value != nil && strings.TrimSpace(fmt.Sprint(value)) != "" {
			return value
		}
	}
	return nil
}

func rawHasKey(args map[string]interface{}, key string) bool {
	if args == nil {
		return false
	}
	_, ok := args[key]
	return ok
}

func runtimeNumberArg(raw interface{}) float64 {
	switch value := raw.(type) {
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case float64:
		return value
	case float32:
		return float64(value)
	case json.Number:
		out, _ := value.Float64()
		return out
	default:
		out, _ := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(raw)), 64)
		return out
	}
}

func applyEntityPlacementStateToState(entity *world.Entity) {
	if entity == nil || entity.Placement == nil {
		return
	}
	if entity.State == nil {
		entity.State = map[string]interface{}{}
	}
	if strings.TrimSpace(entity.Placement.Model) != "" {
		entity.State["model"] = strings.TrimSpace(entity.Placement.Model)
	}
	if entity.Placement.Scale != [3]float64{} {
		entity.State["scale"] = []float64{entity.Placement.Scale[0], entity.Placement.Scale[1], entity.Placement.Scale[2]}
	}
	if entity.Placement.Rotation != [3]float64{} {
		entity.State["rotation"] = []float64{entity.Placement.Rotation[0], entity.Placement.Rotation[1], entity.Placement.Rotation[2]}
		entity.State["rotation_y"] = entity.Placement.Rotation[1]
	}
	if entity.Placement.Offset != [3]float64{} {
		entity.State["offset"] = []float64{entity.Placement.Offset[0], entity.Placement.Offset[1], entity.Placement.Offset[2]}
		entity.State["offset_x"] = entity.Placement.Offset[0]
		entity.State["offset_y"] = entity.Placement.Offset[1]
		entity.State["offset_z"] = entity.Placement.Offset[2]
	}
}

func (wr *WorldRuntime) syncQuestRooms(state *world.WorldState, npcStates map[string]world.NPCState) {
	if state == nil {
		return
	}
	activeQuestRooms := map[string]struct{}{}
	for _, quest := range state.ActiveQuests {
		if strings.EqualFold(strings.TrimSpace(quest.Status), "completed") {
			continue
		}
		members := normalizeNPCIDs(append(append([]string{}, quest.Participants...), quest.OwnerNPCID))
		if len(members) == 0 {
			continue
		}
		roomID := roomIDForQuest(quest.ID)
		if roomID == "" {
			continue
		}
		activeQuestRooms[roomID] = struct{}{}
		room := state.Rooms[roomID]
		room.ID = roomID
		room.Name = firstNonEmpty(room.Name, firstNonEmpty(quest.Title, quest.ID))
		room.Kind = firstNonEmpty(room.Kind, "task_room")
		room.Status = "running"
		room.LinkedQuestID = quest.ID
		room.TaskSummary = firstNonEmpty(quest.Summary, room.TaskSummary, "task execution")
		room.LocationID = firstNonEmpty(room.LocationID, "commons")
		if owner := strings.TrimSpace(quest.OwnerNPCID); owner != "" {
			if ownerState, ok := npcStates[owner]; ok && strings.TrimSpace(ownerState.CurrentLocation) != "" {
				room.LocationID = ownerState.CurrentLocation
			}
		}
		room.AssignedNPCIDs = members
		room.ReleaseOnFinish = true
		if room.CreatedTick == 0 {
			room.CreatedTick = state.Clock.Tick
		}
		room.UpdatedTick = state.Clock.Tick
		state.Rooms[roomID] = room
		for _, npcID := range members {
			npc := npcStates[npcID]
			npc.CurrentRoomID = roomID
			npc.Status = "working"
			if strings.TrimSpace(room.LocationID) != "" {
				npc.CurrentLocation = room.LocationID
			}
			npcStates[npcID] = npc
		}
	}
	for roomID, room := range state.Rooms {
		if _, ok := activeQuestRooms[roomID]; ok {
			continue
		}
		if room.ReleaseOnFinish {
			for _, npcID := range room.AssignedNPCIDs {
				npc := npcStates[npcID]
				if strings.TrimSpace(npc.CurrentRoomID) == roomID {
					npc.CurrentRoomID = ""
					if strings.TrimSpace(npc.Status) == "working" {
						npc.Status = "active"
					}
					npcStates[npcID] = npc
				}
			}
			room.Status = "closed"
			room.UpdatedTick = state.Clock.Tick
		}
		state.Rooms[roomID] = room
	}
}

func (wr *WorldRuntime) Snapshot(limit int) (interface{}, error) {
	state, npcStates, err := wr.ensureState()
	if err != nil {
		return nil, err
	}
	events, err := wr.store.Events(limit)
	if err != nil {
		return nil, err
	}
	return wr.engine.Snapshot(state, npcStates, events, 0, limit), nil
}

func (wr *WorldRuntime) Tick(ctx context.Context, source string) (string, error) {
	res, err := wr.advance(ctx, world.WorldTickRequest{Source: source, ForceStep: true})
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

func (wr *WorldRuntime) PlayerGet() (map[string]interface{}, error) {
	state, _, err := wr.ensureState()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"player": state.Player,
	}, nil
}

func (wr *WorldRuntime) PlayerAction(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	state, _, err := wr.ensureState()
	if err != nil {
		return nil, err
	}
	action := strings.ToLower(strings.TrimSpace(tools.MapStringArg(args, "action")))
	if action == "" {
		return nil, fmt.Errorf("action is required")
	}
	player := state.Player
	if strings.TrimSpace(player.PlayerID) == "" {
		player = world.DefaultWorldState().Player
	}
	switch action {
	case "move":
		target := normalizeWorldID(firstNonEmpty(tools.MapStringArg(args, "location_id"), tools.MapStringArg(args, "target_location")))
		if target == "" {
			return nil, fmt.Errorf("location_id is required")
		}
		current := state.Locations[player.CurrentLocation]
		if player.CurrentLocation != target && !runtimeContainsString(current.Neighbors, target) {
			return nil, fmt.Errorf("location_not_reachable")
		}
		player.CurrentLocation = target
		player.LastActionTick = state.Clock.Tick
		state.Player = player
		evt := world.WorldEvent{
			ID:         fmt.Sprintf("evt-player-move-%d", time.Now().UnixNano()),
			Type:       "player_move",
			Source:     "player",
			ActorID:    player.PlayerID,
			LocationID: target,
			Content:    target,
			Tick:       state.Clock.Tick,
			CreatedAt:  time.Now().UnixMilli(),
		}
		wr.engine.AppendRecentEvent(&state, evt, 20)
		if err := wr.store.SaveWorldState(state); err != nil {
			return nil, err
		}
		if err := wr.store.AppendWorldEvent(evt); err != nil {
			return nil, err
		}
		return map[string]interface{}{"ok": true, "message": fmt.Sprintf("moved to %s", target), "player": state.Player}, nil
	case "speak":
		target := strings.TrimSpace(firstNonEmpty(tools.MapStringArg(args, "target_npc_id"), tools.MapStringArg(args, "target_id")))
		prompt := strings.TrimSpace(firstNonEmpty(tools.MapStringArg(args, "prompt"), tools.MapStringArg(args, "speech")))
		content := prompt
		if target != "" {
			content = fmt.Sprintf("我在 %s 对 NPC %s 说：%s", firstNonEmpty(player.CurrentLocation, "commons"), target, firstNonEmpty(prompt, "你好。"))
		}
		return wr.handlePlayerInput(ctx, content, player.CurrentLocation, player.PlayerID)
	case "interact":
		target := strings.TrimSpace(firstNonEmpty(tools.MapStringArg(args, "target_entity_id"), tools.MapStringArg(args, "target_id")))
		if target == "" {
			return nil, fmt.Errorf("target_entity_id is required")
		}
		prompt := strings.TrimSpace(firstNonEmpty(tools.MapStringArg(args, "prompt"), tools.MapStringArg(args, "speech")))
		content := fmt.Sprintf("我在 %s 与实体 %s 互动：%s", firstNonEmpty(player.CurrentLocation, "commons"), target, firstNonEmpty(prompt, "检查它并告诉我发生了什么。"))
		return wr.handlePlayerInput(ctx, content, player.CurrentLocation, player.PlayerID)
	case "quest_accept", "quest_progress", "quest_complete":
		questID := strings.TrimSpace(firstNonEmpty(tools.MapStringArg(args, "quest_id"), tools.MapStringArg(args, "target_id")))
		if questID == "" {
			return nil, fmt.Errorf("quest_id is required")
		}
		var content string
		switch action {
		case "quest_accept":
			content = fmt.Sprintf("接受任务 %s", questID)
		case "quest_progress":
			content = fmt.Sprintf("推进任务 %s %s", questID, strings.TrimSpace(tools.MapStringArg(args, "prompt")))
		case "quest_complete":
			content = fmt.Sprintf("完成任务 %s", questID)
		}
		return wr.handlePlayerInput(ctx, strings.TrimSpace(content), player.CurrentLocation, player.PlayerID)
	default:
		return nil, fmt.Errorf("unsupported action: %s", action)
	}
}

func (wr *WorldRuntime) NPCList() ([]map[string]interface{}, error) {
	_, npcStates, err := wr.ensureState()
	if err != nil {
		return nil, err
	}
	profiles, err := wr.worldProfiles()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(profiles))
	for _, profile := range profiles {
		state := npcStates[profile.AgentID]
		out = append(out, map[string]interface{}{
			"npc_id":           profile.AgentID,
			"display_name":     profile.Name,
			"persona":          profile.Persona,
			"home_location":    profile.HomeLocation,
			"current_location": state.CurrentLocation,
			"default_goals":    append([]string(nil), profile.DefaultGoals...),
			"status":           state.Status,
			"kind":             profile.Kind,
			"perception_scope": profile.PerceptionScope,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return fmt.Sprint(out[i]["npc_id"]) < fmt.Sprint(out[j]["npc_id"])
	})
	return out, nil
}

func (wr *WorldRuntime) NPCGet(id string) (map[string]interface{}, bool, error) {
	_, npcStates, err := wr.ensureState()
	if err != nil {
		return nil, false, err
	}
	profile, ok, err := wr.profiles.Get(id)
	if err != nil || !ok {
		return nil, false, err
	}
	state, ok := npcStates[profile.AgentID]
	if !ok {
		return nil, false, nil
	}
	return map[string]interface{}{
		"profile": profile,
		"state":   state,
	}, true, nil
}

func (wr *WorldRuntime) EntityList() ([]map[string]interface{}, error) {
	state, _, err := wr.ensureState()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(state.Entities))
	for _, entity := range state.Entities {
		out = append(out, map[string]interface{}{
			"id":          entity.ID,
			"name":        entity.Name,
			"type":        entity.Type,
			"location_id": entity.LocationID,
			"state":       entity.State,
		})
	}
	sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]["id"]) < fmt.Sprint(out[j]["id"]) })
	return out, nil
}

func (wr *WorldRuntime) EntityGet(id string) (map[string]interface{}, bool, error) {
	state, _, err := wr.ensureState()
	if err != nil {
		return nil, false, err
	}
	entity, ok := state.Entities[strings.TrimSpace(id)]
	if !ok {
		return nil, false, nil
	}
	return map[string]interface{}{
		"id":          entity.ID,
		"name":        entity.Name,
		"type":        entity.Type,
		"location_id": entity.LocationID,
		"state":       entity.State,
	}, true, nil
}

func (wr *WorldRuntime) WorldGet() (map[string]interface{}, error) {
	state, _, err := wr.ensureState()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"world_state": state,
	}, nil
}

func (wr *WorldRuntime) RoomList() ([]map[string]interface{}, error) {
	state, _, err := wr.ensureState()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(state.Rooms))
	for _, room := range state.Rooms {
		out = append(out, map[string]interface{}{
			"id":               room.ID,
			"name":             room.Name,
			"kind":             room.Kind,
			"status":           room.Status,
			"location_id":      room.LocationID,
			"task_summary":     room.TaskSummary,
			"linked_quest_id":  room.LinkedQuestID,
			"assigned_npc_ids": append([]string(nil), room.AssignedNPCIDs...),
			"created_tick":     room.CreatedTick,
			"updated_tick":     room.UpdatedTick,
		})
	}
	sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]["id"]) < fmt.Sprint(out[j]["id"]) })
	return out, nil
}

func (wr *WorldRuntime) RoomGet(id string) (map[string]interface{}, bool, error) {
	state, npcStates, err := wr.ensureState()
	if err != nil {
		return nil, false, err
	}
	room, ok := state.Rooms[strings.TrimSpace(id)]
	if !ok {
		return nil, false, nil
	}
	members := make([]world.NPCState, 0, len(room.AssignedNPCIDs))
	for _, npcID := range room.AssignedNPCIDs {
		if npc, ok := npcStates[npcID]; ok {
			members = append(members, npc)
		}
	}
	return map[string]interface{}{
		"room":    room,
		"members": members,
	}, true, nil
}

func (wr *WorldRuntime) EventLog(limit int) ([]map[string]interface{}, error) {
	events, err := wr.store.Events(limit)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(events))
	for _, evt := range events {
		out = append(out, map[string]interface{}{
			"id":          evt.ID,
			"type":        evt.Type,
			"actor_id":    evt.ActorID,
			"location_id": evt.LocationID,
			"content":     evt.Content,
			"tick":        evt.Tick,
			"created_at":  evt.CreatedAt,
		})
	}
	return out, nil
}

func (wr *WorldRuntime) QuestList() ([]map[string]interface{}, error) {
	state, _, err := wr.ensureState()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(state.ActiveQuests))
	for _, quest := range state.ActiveQuests {
		out = append(out, map[string]interface{}{
			"id":           quest.ID,
			"title":        quest.Title,
			"status":       quest.Status,
			"owner_npc_id": quest.OwnerNPCID,
			"participants": append([]string(nil), quest.Participants...),
			"summary":      quest.Summary,
		})
	}
	sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]["id"]) < fmt.Sprint(out[j]["id"]) })
	return out, nil
}

func (wr *WorldRuntime) QuestGet(id string) (map[string]interface{}, bool, error) {
	state, _, err := wr.ensureState()
	if err != nil {
		return nil, false, err
	}
	quest, ok := state.ActiveQuests[strings.TrimSpace(id)]
	if !ok {
		return nil, false, nil
	}
	return map[string]interface{}{
		"id":           quest.ID,
		"title":        quest.Title,
		"status":       quest.Status,
		"owner_npc_id": quest.OwnerNPCID,
		"participants": append([]string(nil), quest.Participants...),
		"summary":      quest.Summary,
	}, true, nil
}

func (wr *WorldRuntime) CreateQuest(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	_ = ctx
	state, _, err := wr.ensureState()
	if err != nil {
		return nil, err
	}
	questID := normalizeWorldID(tools.MapStringArg(args, "id"))
	if questID == "" {
		questID = normalizeWorldID(tools.MapStringArg(args, "title"))
	}
	if questID == "" {
		return nil, fmt.Errorf("id or title is required")
	}
	quest := state.ActiveQuests[questID]
	quest.ID = questID
	quest.Title = firstNonEmpty(strings.TrimSpace(tools.MapStringArg(args, "title")), quest.Title, questID)
	quest.Status = firstNonEmpty(strings.TrimSpace(tools.MapStringArg(args, "status")), quest.Status, "open")
	quest.OwnerNPCID = firstNonEmpty(strings.TrimSpace(tools.MapStringArg(args, "owner_npc_id")), quest.OwnerNPCID)
	quest.Participants = append([]string(nil), tools.MapStringListArg(args, "participants")...)
	quest.Summary = firstNonEmpty(strings.TrimSpace(tools.MapStringArg(args, "summary")), quest.Summary)
	state.ActiveQuests[questID] = quest
	if err := wr.store.SaveWorldState(state); err != nil {
		return nil, err
	}
	evt := world.WorldEvent{
		ID:        fmt.Sprintf("evt-quest-%d", time.Now().UnixNano()),
		Type:      "quest_updated",
		Source:    "world",
		ActorID:   quest.OwnerNPCID,
		Content:   questID,
		Tick:      state.Clock.Tick,
		CreatedAt: time.Now().UnixMilli(),
	}
	_ = wr.store.AppendWorldEvent(evt)
	return map[string]interface{}{"quest_id": questID}, nil
}

func (wr *WorldRuntime) CreateNPC(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	_ = ctx
	if wr == nil || wr.profiles == nil || wr.store == nil {
		return nil, fmt.Errorf("world runtime not ready")
	}
	npcID := normalizeWorldID(tools.MapStringArg(args, "npc_id"))
	if npcID == "" {
		return nil, fmt.Errorf("npc_id is required")
	}
	persona := strings.TrimSpace(tools.MapStringArg(args, "persona"))
	home := normalizeWorldID(tools.MapStringArg(args, "home_location"))
	goals := tools.MapStringListArg(args, "default_goals")
	if persona == "" || home == "" || len(goals) == 0 {
		return nil, fmt.Errorf("persona, home_location, default_goals are required")
	}
	profile, err := wr.profiles.Upsert(tools.AgentProfile{
		AgentID:         npcID,
		Name:            strings.TrimSpace(tools.MapStringArg(args, "name")),
		Kind:            "npc",
		Persona:         persona,
		HomeLocation:    home,
		DefaultGoals:    goals,
		Role:            strings.TrimSpace(tools.MapStringArg(args, "role")),
		MemoryNamespace: npcID,
		Status:          "active",
	})
	if err != nil {
		return nil, err
	}
	state, npcStates, err := wr.ensureState()
	if err != nil {
		return nil, err
	}
	npcStates[profile.AgentID] = wr.engine.EnsureNPCState(wr.profileBlueprint(*profile), npcStates[profile.AgentID])
	if err := wr.store.SaveNPCStates(npcStates); err != nil {
		return nil, err
	}
	evt := world.WorldEvent{
		ID:         fmt.Sprintf("evt-create-npc-%d", time.Now().UnixNano()),
		Type:       "npc_created",
		Source:     "world",
		ActorID:    profile.AgentID,
		LocationID: home,
		Content:    profile.Name,
		Tick:       state.Clock.Tick,
		CreatedAt:  time.Now().UnixMilli(),
	}
	wr.engine.AppendRecentEvent(&state, evt, 20)
	if err := wr.store.SaveWorldState(state); err != nil {
		return nil, err
	}
	if err := wr.store.AppendWorldEvent(evt); err != nil {
		return nil, err
	}
	return map[string]interface{}{"npc_id": profile.AgentID}, nil
}

func (wr *WorldRuntime) CreateEntity(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	_ = ctx
	state, _, err := wr.ensureState()
	if err != nil {
		return nil, err
	}
	entityID := normalizeWorldID(firstNonEmpty(tools.MapStringArg(args, "entity_id"), tools.MapStringArg(args, "id"), tools.MapStringArg(args, "name")))
	locationID := normalizeWorldID(firstNonEmpty(tools.MapStringArg(args, "location_id"), tools.MapStringArg(args, "home_location")))
	if entityID == "" || locationID == "" {
		return nil, fmt.Errorf("entity_id and location_id are required")
	}
	if _, ok := state.Locations[locationID]; !ok {
		return nil, fmt.Errorf("unknown location_id: %s", locationID)
	}
	entity := state.Entities[entityID]
	entity.ID = entityID
	entity.Name = firstNonEmpty(tools.MapStringArg(args, "name"), entity.Name, entityID)
	entity.Type = firstNonEmpty(tools.MapStringArg(args, "entity_type"), entity.Type, "landmark")
	entity.LocationID = locationID
	if entity.State == nil {
		entity.State = map[string]interface{}{}
	}
	if rawState, ok := args["state"].(map[string]interface{}); ok {
		for key, value := range rawState {
			entity.State[strings.TrimSpace(key)] = value
		}
	}
	if placement := runtimeEntityPlacementArg(args); placement != nil {
		entity.Placement = placement
		applyEntityPlacementStateToState(&entity)
	}
	state.Entities[entityID] = entity
	if err := wr.store.SaveWorldState(state); err != nil {
		return nil, err
	}
	evt := world.WorldEvent{
		ID:         fmt.Sprintf("evt-entity-%d", time.Now().UnixNano()),
		Type:       "entity_created",
		Source:     "world",
		LocationID: locationID,
		Content:    entityID,
		Tick:       state.Clock.Tick,
		CreatedAt:  time.Now().UnixMilli(),
	}
	_ = wr.store.AppendWorldEvent(evt)
	return map[string]interface{}{"entity_id": entityID}, nil
}

func (wr *WorldRuntime) UpdateEntity(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	_ = ctx
	state, _, err := wr.ensureState()
	if err != nil {
		return nil, err
	}
	entityID := normalizeWorldID(firstNonEmpty(tools.MapStringArg(args, "entity_id"), tools.MapStringArg(args, "id")))
	if entityID == "" {
		return nil, fmt.Errorf("entity_id is required")
	}
	entity, ok := state.Entities[entityID]
	if !ok {
		return nil, fmt.Errorf("entity not found: %s", entityID)
	}
	if locationID := normalizeWorldID(tools.MapStringArg(args, "location_id")); locationID != "" {
		if _, exists := state.Locations[locationID]; !exists {
			return nil, fmt.Errorf("unknown location_id: %s", locationID)
		}
		entity.LocationID = locationID
	}
	if name := strings.TrimSpace(tools.MapStringArg(args, "name")); name != "" {
		entity.Name = name
	}
	if entityType := strings.TrimSpace(tools.MapStringArg(args, "entity_type")); entityType != "" {
		entity.Type = entityType
	}
	if entity.State == nil {
		entity.State = map[string]interface{}{}
	}
	if rawState, ok := args["state"].(map[string]interface{}); ok {
		for key, value := range rawState {
			entity.State[strings.TrimSpace(key)] = value
		}
	}
	if placement := runtimeEntityPlacementArg(args); placement != nil {
		entity.Placement = placement
		applyEntityPlacementStateToState(&entity)
	}
	state.Entities[entityID] = entity
	if err := wr.store.SaveWorldState(state); err != nil {
		return nil, err
	}
	evt := world.WorldEvent{
		ID:         fmt.Sprintf("evt-entity-update-%d", time.Now().UnixNano()),
		Type:       "entity_updated",
		Source:     "world",
		LocationID: entity.LocationID,
		Content:    entityID,
		Tick:       state.Clock.Tick,
		CreatedAt:  time.Now().UnixMilli(),
	}
	_ = wr.store.AppendWorldEvent(evt)
	return map[string]interface{}{"entity_id": entityID}, nil
}

func (wr *WorldRuntime) UpdateLocation(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	_ = ctx
	state, _, err := wr.ensureState()
	if err != nil {
		return nil, err
	}
	locationID := normalizeWorldID(firstNonEmpty(tools.MapStringArg(args, "location_id"), tools.MapStringArg(args, "id")))
	if locationID == "" {
		return nil, fmt.Errorf("location_id is required")
	}
	location, ok := state.Locations[locationID]
	if !ok {
		return nil, fmt.Errorf("location not found: %s", locationID)
	}
	if name := strings.TrimSpace(tools.MapStringArg(args, "name")); name != "" {
		location.Name = name
	}
	if description := strings.TrimSpace(tools.MapStringArg(args, "description")); description != "" {
		location.Description = description
	}
	if model := strings.TrimSpace(tools.MapStringArg(args, "model")); model != "" || rawHasKey(args, "model") {
		location.Model = model
	}
	state.Locations[locationID] = location
	if err := wr.store.SaveWorldState(state); err != nil {
		return nil, err
	}
	evt := world.WorldEvent{
		ID:         fmt.Sprintf("evt-location-update-%d", time.Now().UnixNano()),
		Type:       "location_updated",
		Source:     "world",
		LocationID: locationID,
		Content:    locationID,
		Tick:       state.Clock.Tick,
		CreatedAt:  time.Now().UnixMilli(),
	}
	_ = wr.store.AppendWorldEvent(evt)
	return map[string]interface{}{"location_id": locationID}, nil
}

func (wr *WorldRuntime) UpdateRoom(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	_ = ctx
	state, _, err := wr.ensureState()
	if err != nil {
		return nil, err
	}
	roomID := normalizeWorldID(firstNonEmpty(tools.MapStringArg(args, "room_id"), tools.MapStringArg(args, "id")))
	if roomID == "" {
		return nil, fmt.Errorf("room_id is required")
	}
	room, ok := state.Rooms[roomID]
	if !ok {
		return nil, fmt.Errorf("room not found: %s", roomID)
	}
	if name := strings.TrimSpace(tools.MapStringArg(args, "name")); name != "" {
		room.Name = name
	}
	if kind := strings.TrimSpace(tools.MapStringArg(args, "kind")); kind != "" {
		room.Kind = kind
	}
	if status := strings.TrimSpace(tools.MapStringArg(args, "status")); status != "" {
		room.Status = status
	}
	if summary := strings.TrimSpace(tools.MapStringArg(args, "task_summary")); summary != "" {
		room.TaskSummary = summary
	}
	if model := strings.TrimSpace(tools.MapStringArg(args, "model")); model != "" || rawHasKey(args, "model") {
		room.Model = model
	}
	room.UpdatedTick = state.Clock.Tick
	state.Rooms[roomID] = room
	if err := wr.store.SaveWorldState(state); err != nil {
		return nil, err
	}
	evt := world.WorldEvent{
		ID:         fmt.Sprintf("evt-room-update-%d", time.Now().UnixNano()),
		Type:       "room_updated",
		Source:     "world",
		LocationID: room.LocationID,
		Content:    roomID,
		Tick:       state.Clock.Tick,
		CreatedAt:  time.Now().UnixMilli(),
	}
	_ = wr.store.AppendWorldEvent(evt)
	return map[string]interface{}{"room_id": roomID}, nil
}

func (wr *WorldRuntime) handleUserQuestInput(content string) (string, bool, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", false, nil
	}
	state, _, err := wr.ensureState()
	if err != nil {
		return "", false, err
	}
	lower := strings.ToLower(content)
	switch {
	case containsAnyQuestPhrase(lower, "list quests", "show quests", "任务列表", "查看任务", "有哪些任务"):
		if len(state.ActiveQuests) == 0 {
			return "当前没有活跃任务。", true, nil
		}
		items := make([]string, 0, len(state.ActiveQuests))
		for _, quest := range state.ActiveQuests {
			items = append(items, fmt.Sprintf("%s [%s]", firstNonEmpty(quest.Title, quest.ID), firstNonEmpty(quest.Status, "open")))
		}
		sort.Strings(items)
		return "当前任务：\n" + strings.Join(items, "\n"), true, nil
	case containsAnyQuestPhrase(lower, "accept quest", "take quest", "接受任务", "接取任务"):
		id := resolveQuestReference(content, state)
		if id == "" {
			return "没有找到要接受的任务。", true, nil
		}
		quest := state.ActiveQuests[id]
		quest.Status = "accepted"
		state.ActiveQuests[id] = quest
		if err := wr.saveQuestMutation(state, quest, "quest_accepted"); err != nil {
			return "", true, err
		}
		return fmt.Sprintf("已接受任务：%s", firstNonEmpty(quest.Title, quest.ID)), true, nil
	case containsAnyQuestPhrase(lower, "complete quest", "finish quest", "完成任务"):
		id := resolveQuestReference(content, state)
		if id == "" {
			return "没有找到要完成的任务。", true, nil
		}
		quest := state.ActiveQuests[id]
		quest.Status = "completed"
		state.ActiveQuests[id] = quest
		if err := wr.saveQuestMutation(state, quest, "quest_completed"); err != nil {
			return "", true, err
		}
		return fmt.Sprintf("已完成任务：%s", firstNonEmpty(quest.Title, quest.ID)), true, nil
	case containsAnyQuestPhrase(lower, "progress quest", "advance quest", "推进任务", "更新任务进度"):
		id := resolveQuestReference(content, state)
		if id == "" {
			return "没有找到要推进的任务。", true, nil
		}
		quest := state.ActiveQuests[id]
		quest.Status = "in_progress"
		if summary := extractTailAfterQuestVerb(content); summary != "" {
			quest.Summary = summary
		}
		state.ActiveQuests[id] = quest
		if err := wr.saveQuestMutation(state, quest, "quest_progressed"); err != nil {
			return "", true, err
		}
		return fmt.Sprintf("已推进任务：%s", firstNonEmpty(quest.Title, quest.ID)), true, nil
	default:
		return "", false, nil
	}
}

func (wr *WorldRuntime) saveQuestMutation(state world.WorldState, quest world.QuestState, eventType string) error {
	if err := wr.store.SaveWorldState(state); err != nil {
		return err
	}
	evt := world.WorldEvent{
		ID:        fmt.Sprintf("evt-%s-%d", eventType, time.Now().UnixNano()),
		Type:      eventType,
		Source:    "user",
		Content:   quest.ID,
		Tick:      state.Clock.Tick,
		CreatedAt: time.Now().UnixMilli(),
	}
	return wr.store.AppendWorldEvent(evt)
}

func (wr *WorldRuntime) HandleUserInput(ctx context.Context, content, channel, chatID string) (string, error) {
	_ = channel
	_ = chatID
	if out, handled, err := wr.handleUserQuestInput(content); handled || err != nil {
		return out, err
	}
	out, err := wr.handlePlayerInput(ctx, content, "commons", "user")
	if err != nil {
		return "", err
	}
	return tools.MapStringArg(out, "message"), nil
}

func (wr *WorldRuntime) handlePlayerInput(ctx context.Context, content, locationID, actorID string) (map[string]interface{}, error) {
	if out, handled, err := wr.handleUserQuestInput(content); handled || err != nil {
		return map[string]interface{}{"ok": err == nil, "message": out}, err
	}
	res, err := wr.advance(ctx, world.WorldTickRequest{
		Source:     "player",
		UserInput:  content,
		LocationID: locationID,
		ActorID:    actorID,
	})
	if err != nil {
		return nil, err
	}
	state, _, stateErr := wr.ensureState()
	if stateErr != nil {
		return nil, stateErr
	}
	return map[string]interface{}{
		"ok":      true,
		"message": res.Text,
		"player":  state.Player,
	}, nil
}

func (wr *WorldRuntime) advance(ctx context.Context, req world.WorldTickRequest) (world.RenderedResult, error) {
	wr.tickMu.Lock()
	defer wr.tickMu.Unlock()
	state, npcStates, err := wr.ensureState()
	if err != nil {
		return world.RenderedResult{}, err
	}
	if err := wr.ensureAutonomousSeed(&state, npcStates); err != nil {
		return world.RenderedResult{}, err
	}
	wr.syncQuestRooms(&state, npcStates)
	catchUp := wr.computeCatchUp(state)
	if req.CatchUpTicks > 0 {
		catchUp = req.CatchUpTicks
	}
	if catchUp > wr.maxCatchUp {
		catchUp = wr.maxCatchUp
	}
	var recentEvents []world.WorldEvent
	for i := 0; i < catchUp; i++ {
		wr.engine.NextTick(&state)
		if err := wr.ensureAutonomousSeed(&state, npcStates); err != nil {
			return world.RenderedResult{}, err
		}
		wr.syncQuestRooms(&state, npcStates)
		res, err := wr.runTick(ctx, &state, npcStates, nil, "background")
		if err != nil {
			return world.RenderedResult{}, err
		}
		recentEvents = append(recentEvents, res.RecentEvents...)
	}
	var userEvent *world.WorldEvent
	if strings.TrimSpace(req.UserInput) != "" {
		wr.engine.NextTick(&state)
		if err := wr.ensureAutonomousSeed(&state, npcStates); err != nil {
			return world.RenderedResult{}, err
		}
		wr.syncQuestRooms(&state, npcStates)
		evt := wr.engine.BuildUserEvent(&state, req.UserInput, req.LocationID, req.ActorID)
		userEvent = &evt
		wr.engine.AppendRecentEvent(&state, evt, 20)
		if err := wr.store.AppendWorldEvent(evt); err != nil {
			return world.RenderedResult{}, err
		}
	}
	forceStepped := false
	if strings.TrimSpace(req.UserInput) == "" && catchUp == 0 && req.ForceStep {
		wr.engine.NextTick(&state)
		if err := wr.ensureAutonomousSeed(&state, npcStates); err != nil {
			return world.RenderedResult{}, err
		}
		wr.syncQuestRooms(&state, npcStates)
		forceStepped = true
	}
	var visible []world.WorldEvent
	if userEvent != nil {
		visible = append(visible, *userEvent)
	}
	res, err := wr.runTick(ctx, &state, npcStates, visible, firstNonEmpty(req.Source, "user"))
	if err != nil {
		return world.RenderedResult{}, err
	}
	recentEvents = append(recentEvents, res.RecentEvents...)
	if forceStepped && len(recentEvents) == 0 {
		id := fmt.Sprintf("evt-world-step-%d", time.Now().UnixNano())
		stepEvent := world.WorldEvent{
			ID:        id,
			Type:      "world_tick",
			Source:    firstNonEmpty(req.Source, "world"),
			ActorID:   "main",
			Content:   "world advanced",
			Tick:      state.Clock.Tick,
			CreatedAt: time.Now().UnixMilli(),
		}
		wr.engine.AppendRecentEvent(&state, stepEvent, 20)
		_ = wr.store.AppendWorldEvent(stepEvent)
		recentEvents = append(recentEvents, stepEvent)
	}
	if err := wr.store.SaveNPCStates(npcStates); err != nil {
		return world.RenderedResult{}, err
	}
	if err := wr.store.SaveWorldState(state); err != nil {
		return world.RenderedResult{}, err
	}
	res.RecentEvents = recentEvents
	if strings.TrimSpace(res.Text) == "" {
		res.Text = wr.renderEvents(userEvent, recentEvents)
	}
	return res, nil
}

func (wr *WorldRuntime) runTick(ctx context.Context, state *world.WorldState, npcStates map[string]world.NPCState, seedEvents []world.WorldEvent, source string) (world.RenderedResult, error) {
	profiles, err := wr.worldProfiles()
	if err != nil {
		return world.RenderedResult{}, err
	}
	maxNPC := wr.maxNPCPerTick
	if maxNPC <= 0 {
		maxNPC = len(profiles)
	}
	intents := make([]world.ActionIntent, 0, maxNPC)
	appliedEvents := make([]world.WorldEvent, 0, maxNPC)
	count := 0
	for _, profile := range profiles {
		if count >= maxNPC {
			break
		}
		npcState := npcStates[profile.AgentID]
		visible := wr.engine.VisibleEventsForNPC(*state, npcState, seedEvents, profile.PerceptionScope)
		if len(visible) == 0 && !wr.shouldWakeNPC(profile, npcState, state.Clock.Tick) {
			continue
		}
		intent, err := wr.decideNPCIntent(ctx, *state, profile, npcState, visible)
		if err != nil {
			return world.RenderedResult{}, err
		}
		intents = append(intents, intent)
		delta := wr.engine.ApplyIntent(state, &npcState, intent)
		if delta.Applied && strings.EqualFold(strings.TrimSpace(intent.Action), "delegate") && wr.manager != nil && strings.TrimSpace(intent.TargetAgent) != "" {
			_, _ = wr.manager.SendAgentMessage(
				profile.AgentID,
				strings.TrimSpace(intent.TargetAgent),
				"delegate",
				firstNonEmpty(intent.Speech, intent.TargetEntity, "delegated task"),
				"",
			)
		}
		if !delta.Applied {
			rejected := world.WorldEvent{
				ID:         fmt.Sprintf("evt-rejected-%d", time.Now().UnixNano()),
				Type:       "rejected_intent",
				Source:     source,
				ActorID:    intent.ActorID,
				LocationID: npcState.CurrentLocation,
				Content:    delta.Reason,
				Tick:       state.Clock.Tick,
				CreatedAt:  time.Now().UnixMilli(),
			}
			wr.engine.AppendRecentEvent(state, rejected, 20)
			if err := wr.store.AppendWorldEvent(rejected); err != nil {
				return world.RenderedResult{}, err
			}
			appliedEvents = append(appliedEvents, rejected)
		} else if delta.Event != nil {
			npcStates[profile.AgentID] = npcState
			wr.engine.AppendRecentEvent(state, *delta.Event, 20)
			if err := wr.store.AppendWorldEvent(*delta.Event); err != nil {
				return world.RenderedResult{}, err
			}
			appliedEvents = append(appliedEvents, *delta.Event)
		}
		count++
	}
	return world.RenderedResult{
		Text:         wr.renderEvents(nil, appliedEvents),
		Tick:         state.Clock.Tick,
		Intents:      intents,
		RecentEvents: appliedEvents,
	}, nil
}

func (wr *WorldRuntime) decideNPCIntent(ctx context.Context, state world.WorldState, profile tools.AgentProfile, npcState world.NPCState, visible []world.WorldEvent) (world.ActionIntent, error) {
	if wr.dispatcher == nil {
		return wr.fallbackIntent(profile, npcState, visible, state), nil
	}
	worldSnapshot := map[string]interface{}{
		"tick":         state.Clock.Tick,
		"locations":    state.Locations,
		"rooms":        state.Rooms,
		"global_facts": state.GlobalFacts,
	}
	npcSnapshot := map[string]interface{}{
		"npc_id":           npcState.NPCID,
		"display_name":     profile.Name,
		"persona":          profile.Persona,
		"traits":           append([]string(nil), profile.Traits...),
		"current_location": npcState.CurrentLocation,
		"current_room_id":  npcState.CurrentRoomID,
		"goals_long_term":  append([]string(nil), npcState.Goals.LongTerm...),
		"goals_short_term": append([]string(nil), npcState.Goals.ShortTerm...),
	}
	visibleMaps := make([]map[string]interface{}, 0, len(visible))
	for _, evt := range visible {
		visibleMaps = append(visibleMaps, map[string]interface{}{
			"id":          evt.ID,
			"type":        evt.Type,
			"actor_id":    evt.ActorID,
			"location_id": evt.LocationID,
			"content":     evt.Content,
			"tick":        evt.Tick,
		})
	}
	taskText := wr.buildDecisionTask(profile, npcState, visible)
	task, err := wr.dispatcher.DispatchTask(ctx, tools.AgentDispatchRequest{
		Task:             taskText,
		RunKind:          "world_npc",
		AgentID:          profile.AgentID,
		Origin:           &tools.OriginRef{Channel: "world", ChatID: "world"},
		WorldDecision: &tools.WorldDecisionContext{
			WorldTick:           state.Clock.Tick,
			WorldSnapshot:       worldSnapshot,
			NPCSnapshot:         npcSnapshot,
			VisibleEvents:       visibleMaps,
			IntentSchemaVersion: "v1",
		},
	})
	if err != nil {
		return wr.fallbackIntent(profile, npcState, visible, state), nil
	}
	reply, err := wr.dispatcher.WaitReply(ctx, task.ID, 100*time.Millisecond)
	if err != nil {
		return wr.fallbackIntent(profile, npcState, visible, state), nil
	}
	intent, err := parseWorldIntent(reply.Result)
	if err != nil || strings.TrimSpace(intent.Action) == "" {
		return wr.fallbackIntent(profile, npcState, visible, state), nil
	}
	if strings.TrimSpace(intent.ActorID) == "" {
		intent.ActorID = profile.AgentID
	}
	return intent, nil
}

func (wr *WorldRuntime) fallbackIntent(profile tools.AgentProfile, npcState world.NPCState, visible []world.WorldEvent, state world.WorldState) world.ActionIntent {
	intent := world.ActionIntent{
		ActorID: profile.AgentID,
		Action:  "wait",
	}
	for _, evt := range visible {
		if evt.Type == "user_input" && evt.LocationID == npcState.CurrentLocation {
			speech := fmt.Sprintf("%s notices the user: %s", firstNonEmpty(profile.Name, profile.AgentID), strings.TrimSpace(evt.Content))
			return world.ActionIntent{
				ActorID:                  profile.AgentID,
				Action:                   "speak",
				Speech:                   speech,
				InternalReasoningSummary: "responded to nearby user activity",
			}
		}
	}
	for _, goal := range npcState.Goals.LongTerm {
		g := strings.ToLower(strings.TrimSpace(goal))
		if strings.Contains(g, "patrol") {
			loc := state.Locations[npcState.CurrentLocation]
			if len(loc.Neighbors) > 0 {
				return world.ActionIntent{
					ActorID:                  profile.AgentID,
					Action:                   "move",
					TargetLocation:           loc.Neighbors[0],
					InternalReasoningSummary: "patrolling according to long-term goal",
				}
			}
		}
		if strings.Contains(g, "watch") || strings.Contains(g, "guard") || strings.Contains(g, "observe") {
			return world.ActionIntent{
				ActorID:                  profile.AgentID,
				Action:                   "observe",
				InternalReasoningSummary: "maintains awareness of the surroundings",
			}
		}
	}
	return intent
}

func (wr *WorldRuntime) buildDecisionTask(profile tools.AgentProfile, npcState world.NPCState, visible []world.WorldEvent) string {
	payload := map[string]interface{}{
		"npc_id":            profile.AgentID,
		"display_name":      profile.Name,
		"persona":           profile.Persona,
		"traits":            profile.Traits,
		"current_location":  npcState.CurrentLocation,
		"current_room_id":   npcState.CurrentRoomID,
		"long_term_goals":   npcState.Goals.LongTerm,
		"short_term_goals":  npcState.Goals.ShortTerm,
		"visible_events":    visible,
		"allowed_actions":   []string{"move", "speak", "observe", "interact", "delegate", "wait"},
		"response_contract": "return JSON object with actor_id, action, target_location, target_entity, target_agent, speech, internal_reasoning_summary, proposed_effects",
	}
	data, _ := json.Marshal(payload)
	return string(data)
}

func (wr *WorldRuntime) ensureState() (world.WorldState, map[string]world.NPCState, error) {
	if err := wr.ensureMainProfile(); err != nil {
		return world.WorldState{}, nil, err
	}
	state, err := wr.store.LoadWorldState()
	if err != nil {
		return world.WorldState{}, nil, err
	}
	wr.engine.EnsureWorld(&state)
	npcStates, err := wr.store.LoadNPCStates()
	if err != nil {
		return world.WorldState{}, nil, err
	}
	profiles, err := wr.worldProfiles()
	if err != nil {
		return world.WorldState{}, nil, err
	}
	for _, profile := range profiles {
		current := npcStates[profile.AgentID]
		next := wr.engine.EnsureNPCState(wr.profileBlueprint(profile), current)
		npcStates[profile.AgentID] = next
	}
	wr.syncQuestRooms(&state, npcStates)
	if err := wr.store.SaveNPCStates(npcStates); err != nil {
		return world.WorldState{}, nil, err
	}
	if err := wr.store.SaveWorldState(state); err != nil {
		return world.WorldState{}, nil, err
	}
	return state, npcStates, nil
}

func (wr *WorldRuntime) worldProfiles() ([]tools.AgentProfile, error) {
	if wr == nil || wr.profiles == nil {
		return nil, nil
	}
	items, err := wr.profiles.List()
	if err != nil {
		return nil, err
	}
	out := make([]tools.AgentProfile, 0, len(items))
	for _, item := range items {
		if !strings.EqualFold(strings.TrimSpace(item.Status), "active") {
			continue
		}
		if !isWorldNPCProfile(item) {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AgentID < out[j].AgentID })
	return out, nil
}

func isWorldNPCProfile(profile tools.AgentProfile) bool {
	if !strings.EqualFold(strings.TrimSpace(profile.Kind), "npc") {
		return false
	}
	return strings.TrimSpace(profile.HomeLocation) != "" ||
		strings.TrimSpace(profile.Persona) != "" ||
		len(profile.DefaultGoals) > 0 ||
		len(profile.WorldTags) > 0
}

func (wr *WorldRuntime) profileBlueprint(profile tools.AgentProfile) world.NPCBlueprint {
	return world.NPCBlueprint{
		NPCID:           profile.AgentID,
		DisplayName:     profile.Name,
		Kind:            profile.Kind,
		Role:            profile.Role,
		Persona:         profile.Persona,
		Traits:          append([]string(nil), profile.Traits...),
		Faction:         profile.Faction,
		HomeLocation:    firstNonEmpty(profile.HomeLocation, "commons"),
		DefaultGoals:    append([]string(nil), profile.DefaultGoals...),
		PerceptionScope: profile.PerceptionScope,
		ScheduleHint:    profile.ScheduleHint,
		WorldTags:       append([]string(nil), profile.WorldTags...),
		MemoryNamespace: profile.MemoryNamespace,
		PromptFile:      profile.PromptFile,
	}
}

func (wr *WorldRuntime) shouldWakeNPC(profile tools.AgentProfile, state world.NPCState, tick int64) bool {
	if tick == 0 {
		return true
	}
	if len(state.Goals.LongTerm) == 0 {
		return false
	}
	if state.LastActiveTick == 0 {
		return true
	}
	return tick-state.LastActiveTick >= 2
}

func (wr *WorldRuntime) computeCatchUp(state world.WorldState) int {
	if state.Clock.LastAdvance <= 0 || state.Clock.TickDuration <= 0 {
		return 0
	}
	delta := time.Now().Unix() - state.Clock.LastAdvance
	if delta <= state.Clock.TickDuration {
		return 0
	}
	return int(delta / state.Clock.TickDuration)
}

func (wr *WorldRuntime) renderEvents(userEvent *world.WorldEvent, events []world.WorldEvent) string {
	lines := make([]string, 0, len(events)+1)
	if userEvent != nil && strings.TrimSpace(userEvent.Content) != "" {
		lines = append(lines, "世界感知到你的行动："+strings.TrimSpace(userEvent.Content))
	}
	for _, evt := range events {
		switch evt.Type {
		case "npc_speak":
			lines = append(lines, fmt.Sprintf("%s 说：%s", evt.ActorID, strings.TrimSpace(evt.Content)))
		case "npc_move":
			lines = append(lines, fmt.Sprintf("%s 移动到了 %s", evt.ActorID, strings.TrimSpace(evt.LocationID)))
		case "rejected_intent":
			lines = append(lines, fmt.Sprintf("%s 的行动被世界拒绝：%s", evt.ActorID, strings.TrimSpace(evt.Content)))
		case "npc_observe":
			lines = append(lines, fmt.Sprintf("%s 正在观察局势", evt.ActorID))
		case "npc_interact":
			lines = append(lines, fmt.Sprintf("%s 发起了交互：%s", evt.ActorID, strings.TrimSpace(evt.Content)))
		case "npc_delegate":
			lines = append(lines, fmt.Sprintf("%s 发出了委托：%s", evt.ActorID, strings.TrimSpace(evt.Content)))
		}
	}
	if len(lines) == 0 {
		return "世界安静地推进了一拍。"
	}
	return strings.Join(lines, "\n")
}

func parseWorldIntent(raw string) (world.ActionIntent, error) {
	var intent world.ActionIntent
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &intent); err != nil {
		return world.ActionIntent{}, err
	}
	return intent, nil
}

func normalizeWorldID(in string) string {
	in = strings.TrimSpace(strings.ToLower(in))
	if in == "" {
		return ""
	}
	var sb strings.Builder
	for _, r := range in {
		switch {
		case r >= 'a' && r <= 'z':
			sb.WriteRune(r)
		case r >= '0' && r <= '9':
			sb.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			sb.WriteRune(r)
		case r == ' ':
			sb.WriteRune('-')
		}
	}
	return strings.Trim(sb.String(), "-_.")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func runtimeContainsString(items []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, item := range items {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}

func containsAnyQuestPhrase(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, strings.ToLower(strings.TrimSpace(needle))) {
			return true
		}
	}
	return false
}

func resolveQuestReference(content string, state world.WorldState) string {
	content = strings.ToLower(strings.TrimSpace(content))
	for id, quest := range state.ActiveQuests {
		if strings.Contains(content, strings.ToLower(id)) {
			return id
		}
		if strings.TrimSpace(quest.Title) != "" && strings.Contains(content, strings.ToLower(quest.Title)) {
			return id
		}
	}
	return ""
}

func extractTailAfterQuestVerb(content string) string {
	raw := strings.TrimSpace(content)
	lower := strings.ToLower(raw)
	for _, marker := range []string{"推进任务", "更新任务进度", "progress quest", "advance quest"} {
		idx := strings.Index(lower, strings.ToLower(marker))
		if idx >= 0 {
			out := strings.TrimSpace(raw[idx+len(marker):])
			out = strings.Trim(out, " :,-")
			return out
		}
	}
	return ""
}
