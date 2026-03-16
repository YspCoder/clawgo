package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YspCoder/clawgo/pkg/providers"
	"github.com/YspCoder/clawgo/pkg/tools"
	"github.com/YspCoder/clawgo/pkg/world"
)

func TestWorldRuntimeHandleUserInputInitializesState(t *testing.T) {
	workspace := t.TempDir()
	manager := tools.NewAgentManager(nil, workspace, nil)
	store := manager.ProfileStore()
	if store == nil {
		t.Fatalf("expected profile store")
	}
	if _, err := store.Upsert(tools.AgentProfile{
		AgentID:      "keeper",
		Name:         "Keeper",
		Kind:         "npc",
		Persona:      "A calm keeper of the commons.",
		HomeLocation: "commons",
		DefaultGoals: []string{"watch the square"},
		Status:       "active",
	}); err != nil {
		t.Fatalf("profile upsert failed: %v", err)
	}
	manager.SetRunFunc(func(ctx context.Context, task *tools.AgentTask) (string, error) {
		out, _ := json.Marshal(map[string]interface{}{
			"actor_id": task.AgentID,
			"action":   "speak",
			"speech":   "I saw the user arrive.",
		})
		return string(out), nil
	})
	runtime := NewWorldRuntime(workspace, store, tools.NewAgentDispatcher(manager), manager)

	out, err := runtime.HandleUserInput(context.Background(), "I enter the commons.", "cli", "direct")
	if err != nil {
		t.Fatalf("handle user input failed: %v", err)
	}
	if !strings.Contains(out, "keeper") && !strings.Contains(out, "I saw the user arrive") {
		t.Fatalf("unexpected world response: %q", out)
	}

	for _, name := range []string{"world_state.json", "npc_state.json", "world_events.jsonl"} {
		if _, err := os.Stat(filepath.Join(workspace, "agents", "runtime", name)); err != nil {
			t.Fatalf("expected world artifact %s: %v", name, err)
		}
	}

	snapshot, err := runtime.Snapshot(10)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	data, _ := json.Marshal(snapshot)
	if !strings.Contains(string(data), "\"npc_count\":1") {
		t.Fatalf("expected snapshot npc_count=1, got %s", string(data))
	}
}

func TestWorldRuntimeTickSupportsAutonomousNPCAction(t *testing.T) {
	workspace := t.TempDir()
	manager := tools.NewAgentManager(nil, workspace, nil)
	store := manager.ProfileStore()
	if store == nil {
		t.Fatalf("expected profile store")
	}
	if _, err := store.Upsert(tools.AgentProfile{
		AgentID:      "patroller",
		Name:         "Patroller",
		Kind:         "npc",
		Persona:      "Walks the route.",
		HomeLocation: "commons",
		DefaultGoals: []string{"patrol the area"},
		Status:       "active",
	}); err != nil {
		t.Fatalf("profile upsert failed: %v", err)
	}
	manager.SetRunFunc(func(ctx context.Context, task *tools.AgentTask) (string, error) {
		out, _ := json.Marshal(map[string]interface{}{
			"actor_id":        task.AgentID,
			"action":          "move",
			"target_location": "square",
		})
		return string(out), nil
	})
	runtime := NewWorldRuntime(workspace, store, tools.NewAgentDispatcher(manager), manager)

	out, err := runtime.Tick(context.Background(), "test")
	if err != nil {
		t.Fatalf("tick failed: %v", err)
	}
	if !strings.Contains(out, "square") {
		t.Fatalf("expected move narration, got %q", out)
	}
	npc, found, err := runtime.NPCGet("patroller")
	if err != nil || !found {
		t.Fatalf("expected npc state after tick, found=%v err=%v", found, err)
	}
	data, _ := json.Marshal(npc)
	if !strings.Contains(string(data), "\"current_location\":\"square\"") {
		t.Fatalf("expected current_location square, got %s", string(data))
	}
}

func TestWorldRuntimeCreateNPCAndSnapshot(t *testing.T) {
	workspace := t.TempDir()
	manager := tools.NewAgentManager(nil, workspace, nil)
	runtime := NewWorldRuntime(workspace, manager.ProfileStore(), tools.NewAgentDispatcher(manager), manager)

	created, err := runtime.CreateNPC(context.Background(), map[string]interface{}{
		"npc_id":        "merchant",
		"name":          "Merchant",
		"persona":       "Talkative trader",
		"home_location": "square",
		"default_goals": []string{"watch trade"},
	})
	if err != nil {
		t.Fatalf("create npc failed: %v", err)
	}
	if got := strings.TrimSpace(tools.MapStringArg(created, "npc_id")); got != "merchant" {
		t.Fatalf("unexpected created npc id: %q", got)
	}

	snapshotOut, err := runtime.Snapshot(10)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	data, _ := json.Marshal(snapshotOut)
	if !strings.Contains(string(data), "\"merchant\"") {
		t.Fatalf("expected snapshot to include merchant: %s", string(data))
	}
	events, err := runtime.EventLog(10)
	if err != nil {
		t.Fatalf("event log failed: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected npc_created event")
	}
}

func TestWorldRuntimeCreateEntityAndGet(t *testing.T) {
	workspace := t.TempDir()
	manager := tools.NewAgentManager(nil, workspace, nil)
	runtime := NewWorldRuntime(workspace, manager.ProfileStore(), tools.NewAgentDispatcher(manager), manager)
	created, err := runtime.CreateEntity(context.Background(), map[string]interface{}{
		"entity_id":   "statue",
		"name":        "Old Statue",
		"entity_type": "landmark",
		"location_id": "square",
	})
	if err != nil {
		t.Fatalf("create entity failed: %v", err)
	}
	if got := strings.TrimSpace(tools.MapStringArg(created, "entity_id")); got != "statue" {
		t.Fatalf("unexpected entity id: %q", got)
	}
	entity, found, err := runtime.EntityGet("statue")
	if err != nil || !found {
		t.Fatalf("expected entity, found=%v err=%v", found, err)
	}
	if got := strings.TrimSpace(fmt.Sprint(entity["location_id"])); got != "square" {
		t.Fatalf("expected entity in square, got %q", got)
	}
}

func TestWorldRuntimeUpdateEntityPlacement(t *testing.T) {
	workspace := t.TempDir()
	manager := tools.NewAgentManager(nil, workspace, nil)
	runtime := NewWorldRuntime(workspace, manager.ProfileStore(), tools.NewAgentDispatcher(manager), manager)
	if _, err := runtime.CreateEntity(context.Background(), map[string]interface{}{
		"entity_id":   "bench",
		"name":        "Bench",
		"entity_type": "table",
		"location_id": "square",
	}); err != nil {
		t.Fatalf("create entity failed: %v", err)
	}
	if _, err := runtime.UpdateEntity(context.Background(), map[string]interface{}{
		"entity_id":   "bench",
		"location_id": "commons",
		"model":       "entity.table",
		"rotation_y":  1.57,
		"offset":      []interface{}{0.5, 0, -0.25},
		"scale":       []interface{}{1.2, 1.2, 1.2},
	}); err != nil {
		t.Fatalf("update entity failed: %v", err)
	}
	entity, found, err := runtime.EntityGet("bench")
	if err != nil || !found {
		t.Fatalf("expected updated entity, found=%v err=%v", found, err)
	}
	data, _ := json.Marshal(entity)
	text := string(data)
	if !strings.Contains(text, `"location_id":"commons"`) {
		t.Fatalf("expected updated location, got %s", text)
	}
	if !strings.Contains(text, `"model":"entity.table"`) {
		t.Fatalf("expected updated model, got %s", text)
	}
}

func TestWorldRuntimeUpdateLocationAndRoomModel(t *testing.T) {
	workspace := t.TempDir()
	manager := tools.NewAgentManager(nil, workspace, nil)
	runtime := NewWorldRuntime(workspace, manager.ProfileStore(), tools.NewAgentDispatcher(manager), manager)

	if _, err := runtime.UpdateLocation(context.Background(), map[string]interface{}{
		"location_id": "commons",
		"model":       "location.plaza",
		"description": "Central commons",
	}); err != nil {
		t.Fatalf("update location failed: %v", err)
	}
	if _, err := runtime.CreateQuest(context.Background(), map[string]interface{}{
		"id":           "room-test",
		"title":        "Room Test",
		"owner_npc_id": "main",
		"status":       "accepted",
		"summary":      "test room",
	}); err != nil {
		t.Fatalf("create quest failed: %v", err)
	}
	state, npcStates, err := runtime.ensureState()
	if err != nil {
		t.Fatalf("ensure state failed: %v", err)
	}
	runtime.syncQuestRooms(&state, npcStates)
	if err := runtime.store.SaveWorldState(state); err != nil {
		t.Fatalf("save state failed: %v", err)
	}
	if _, err := runtime.UpdateRoom(context.Background(), map[string]interface{}{
		"room_id": "room-room-test",
		"model":   "room.task",
		"name":    "Task Room",
	}); err != nil {
		t.Fatalf("update room failed: %v", err)
	}

	worldOut, err := runtime.WorldGet()
	if err != nil {
		t.Fatalf("world get failed: %v", err)
	}
	worldState, ok := worldOut["world_state"].(world.WorldState)
	if !ok {
		t.Fatalf("unexpected world_state payload: %T", worldOut["world_state"])
	}
	if got := worldState.Locations["commons"].Model; got != "location.plaza" {
		t.Fatalf("expected commons model updated, got %q", got)
	}
	if got := worldState.Rooms["room-room-test"].Model; got != "room.task" {
		t.Fatalf("expected room model updated, got %q", got)
	}
}

func TestWorldRuntimeSnapshotIncludesEntityOccupancyAfterInteract(t *testing.T) {
	workspace := t.TempDir()
	manager := tools.NewAgentManager(nil, workspace, nil)
	store := manager.ProfileStore()
	if store == nil {
		t.Fatalf("expected profile store")
	}
	if _, err := store.Upsert(tools.AgentProfile{
		AgentID:      "caretaker",
		Name:         "Caretaker",
		Kind:         "npc",
		Persona:      "Maintains landmarks.",
		HomeLocation: "square",
		DefaultGoals: []string{"maintain landmarks"},
		Status:       "active",
	}); err != nil {
		t.Fatalf("profile upsert failed: %v", err)
	}
	runtime := NewWorldRuntime(workspace, store, tools.NewAgentDispatcher(manager), manager)
	worldOut, err := runtime.WorldGet()
	if err != nil {
		t.Fatalf("world get failed: %v", err)
	}
	worldState, ok := worldOut["world_state"].(world.WorldState)
	if !ok {
		t.Fatalf("unexpected world_state payload: %T", worldOut["world_state"])
	}
	worldState.Entities["statue"] = world.Entity{ID: "statue", LocationID: "square", State: map[string]interface{}{}}
	if err := runtime.store.SaveWorldState(worldState); err != nil {
		t.Fatalf("save world state failed: %v", err)
	}
	manager.SetRunFunc(func(ctx context.Context, task *tools.AgentTask) (string, error) {
		return `{"actor_id":"caretaker","action":"interact","target_entity":"statue","speech":"polishes the statue"}`, nil
	})
	if _, err := runtime.Tick(context.Background(), "interact"); err != nil {
		t.Fatalf("tick failed: %v", err)
	}
	snap, err := runtime.Snapshot(10)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	data, _ := json.Marshal(snap)
	if !strings.Contains(string(data), `"entity_occupancy":{"square":["statue"]}`) {
		t.Fatalf("expected entity occupancy for statue, got %s", string(data))
	}
}

func TestWorldRuntimePlayerDefaultsAndActions(t *testing.T) {
	workspace := t.TempDir()
	manager := tools.NewAgentManager(nil, workspace, nil)
	store := manager.ProfileStore()
	if _, err := store.Upsert(tools.AgentProfile{
		AgentID:      "keeper",
		Name:         "Keeper",
		Kind:         "npc",
		Persona:      "Greets visitors.",
		HomeLocation: "commons",
		DefaultGoals: []string{"watch the square"},
		Status:       "active",
	}); err != nil {
		t.Fatalf("profile upsert failed: %v", err)
	}
	manager.SetRunFunc(func(ctx context.Context, task *tools.AgentTask) (string, error) {
		return `{"actor_id":"keeper","action":"speak","speech":"Welcome, traveler."}`, nil
	})
	runtime := NewWorldRuntime(workspace, store, tools.NewAgentDispatcher(manager), manager)

	playerOut, err := runtime.PlayerGet()
	if err != nil {
		t.Fatalf("player get failed: %v", err)
	}
	data, _ := json.Marshal(playerOut)
	if !strings.Contains(string(data), `"current_location":"commons"`) {
		t.Fatalf("expected default player location commons, got %s", string(data))
	}

	moveOut, err := runtime.PlayerAction(context.Background(), map[string]interface{}{
		"action":      "move",
		"location_id": "square",
	})
	if err != nil {
		t.Fatalf("player move failed: %v", err)
	}
	moveData, _ := json.Marshal(moveOut)
	if !strings.Contains(string(moveData), `"current_location":"square"`) {
		t.Fatalf("expected moved player to square, got %s", string(moveData))
	}

	speakOut, err := runtime.PlayerAction(context.Background(), map[string]interface{}{
		"action":        "speak",
		"target_npc_id": "keeper",
		"prompt":        "你好",
	})
	if err != nil {
		t.Fatalf("player speak failed: %v", err)
	}
	if !strings.Contains(tools.MapStringArg(speakOut, "message"), "keeper") && !strings.Contains(tools.MapStringArg(speakOut, "message"), "Welcome") {
		t.Fatalf("unexpected speak result: %#v", speakOut)
	}
}

func TestHandleRuntimeAdminSnapshotIncludesWorld(t *testing.T) {
	workspace := t.TempDir()
	manager := tools.NewAgentManager(nil, workspace, nil)
	store := manager.ProfileStore()
	if store == nil {
		t.Fatalf("expected profile store")
	}
	if _, err := store.Upsert(tools.AgentProfile{
		AgentID:      "watcher",
		Name:         "Watcher",
		Kind:         "npc",
		Persona:      "Keeps watch.",
		HomeLocation: "commons",
		DefaultGoals: []string{"watch"},
		Status:       "active",
	}); err != nil {
		t.Fatalf("profile upsert failed: %v", err)
	}
	manager.SetRunFunc(func(ctx context.Context, task *tools.AgentTask) (string, error) {
		return `{"actor_id":"watcher","action":"observe","internal_reasoning_summary":"on watch"}`, nil
	})
	worldRuntime := NewWorldRuntime(workspace, store, tools.NewAgentDispatcher(manager), manager)
	loop := &AgentLoop{
		agentManager: manager,
		agentDispatcher:  tools.NewAgentDispatcher(manager),
		worldRuntime:    worldRuntime,
	}
	if _, err := worldRuntime.Tick(context.Background(), "seed"); err != nil {
		t.Fatalf("world tick failed: %v", err)
	}

	out, err := loop.HandleRuntimeAdmin(context.Background(), "snapshot", map[string]interface{}{"limit": 10})
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	payload, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected payload type: %T", out)
	}
	snapshot, ok := payload["snapshot"].(tools.RuntimeSnapshot)
	if !ok {
		t.Fatalf("unexpected snapshot type: %T", payload["snapshot"])
	}
	if snapshot.World == nil {
		t.Fatalf("expected world snapshot in runtime snapshot")
	}
}

func TestWorldRuntimeDelegateSendsMailboxMessage(t *testing.T) {
	workspace := t.TempDir()
	manager := tools.NewAgentManager(nil, workspace, nil)
	store := manager.ProfileStore()
	if store == nil {
		t.Fatalf("expected profile store")
	}
	for _, profile := range []tools.AgentProfile{
		{
			AgentID:      "chief",
			Name:         "Chief",
			Kind:         "npc",
			Persona:      "Delegates work.",
			HomeLocation: "commons",
			DefaultGoals: []string{"coordinate"},
			Status:       "active",
		},
		{
			AgentID:      "scout",
			Name:         "Scout",
			Kind:         "npc",
			Persona:      "Explores.",
			HomeLocation: "commons",
			DefaultGoals: []string{"patrol"},
			Status:       "active",
		},
	} {
		if _, err := store.Upsert(profile); err != nil {
			t.Fatalf("profile upsert failed: %v", err)
		}
	}
	manager.SetRunFunc(func(ctx context.Context, task *tools.AgentTask) (string, error) {
		if task.AgentID == "chief" {
			return `{"actor_id":"chief","action":"delegate","target_agent":"scout","speech":"Check the square."}`, nil
		}
		return `{"actor_id":"scout","action":"wait"}`, nil
	})
	runtime := NewWorldRuntime(workspace, store, tools.NewAgentDispatcher(manager), manager)

	if _, err := runtime.Tick(context.Background(), "delegate"); err != nil {
		t.Fatalf("tick failed: %v", err)
	}
	msgs, err := manager.Inbox("scout", 10)
	if err != nil {
		t.Fatalf("inbox failed: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatalf("expected delegate message in scout inbox")
	}
	found := false
	for _, msg := range msgs {
		if msg.Type != "delegate" {
			continue
		}
		if !strings.Contains(msg.Content, "Check the square") {
			t.Fatalf("unexpected delegate content: %+v", msg)
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("expected delegate message in inbox, got %+v", msgs)
	}
}

func TestHandleRuntimeAdminWorldActions(t *testing.T) {
	workspace := t.TempDir()
	manager := tools.NewAgentManager(nil, workspace, nil)
	worldRuntime := NewWorldRuntime(workspace, manager.ProfileStore(), tools.NewAgentDispatcher(manager), manager)
	loop := &AgentLoop{
		agentManager: manager,
		agentDispatcher:  tools.NewAgentDispatcher(manager),
		worldRuntime:    worldRuntime,
	}

	out, err := loop.HandleRuntimeAdmin(context.Background(), "world_npc_create", map[string]interface{}{
		"npc_id":        "merchant",
		"name":          "Merchant",
		"persona":       "Talkative trader",
		"home_location": "square",
		"default_goals": []string{"watch trade"},
	})
	if err != nil {
		t.Fatalf("world_npc_create failed: %v", err)
	}
	payload, ok := out.(map[string]interface{})
	if !ok || strings.TrimSpace(tools.MapStringArg(payload, "npc_id")) != "merchant" {
		t.Fatalf("unexpected create payload: %#v", out)
	}
	out, err = loop.HandleRuntimeAdmin(context.Background(), "world_npc_list", nil)
	if err != nil {
		t.Fatalf("world_npc_list failed: %v", err)
	}
	listPayload, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected list payload: %T", out)
	}
	items, ok := listPayload["items"].([]map[string]interface{})
	if !ok || len(items) == 0 {
		t.Fatalf("expected world npc list items, got %#v", listPayload["items"])
	}
	out, err = loop.HandleRuntimeAdmin(context.Background(), "world_quest_create", map[string]interface{}{
		"id":           "meet-merchant",
		"title":        "Meet Merchant",
		"owner_npc_id": "merchant",
		"summary":      "Find the merchant in the square.",
	})
	if err != nil {
		t.Fatalf("world_quest_create failed: %v", err)
	}
	questPayload, ok := out.(map[string]interface{})
	if !ok || strings.TrimSpace(tools.MapStringArg(questPayload, "quest_id")) != "meet-merchant" {
		t.Fatalf("unexpected quest create payload: %#v", out)
	}
	out, err = loop.HandleRuntimeAdmin(context.Background(), "world_quest_list", nil)
	if err != nil {
		t.Fatalf("world_quest_list failed: %v", err)
	}
	listPayload, ok = out.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected quest list payload: %T", out)
	}
	if _, ok := listPayload["items"].([]map[string]interface{}); !ok {
		t.Fatalf("expected quest list items, got %#v", listPayload["items"])
	}
}

type worldDecisionStubProvider struct {
	content string
}

func (p worldDecisionStubProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	return &providers.LLMResponse{Content: p.content, FinishReason: "stop"}, nil
}

func (p worldDecisionStubProvider) GetDefaultModel() string { return "stub-world-model" }

func TestRunWorldDecisionTaskUsesLLMJSONWhenAvailable(t *testing.T) {
	loop := &AgentLoop{
		provider: worldDecisionStubProvider{
			content: `{"actor_id":"keeper","action":"speak","speech":"Welcome to the square.","internal_reasoning_summary":"greets newcomers"}`,
		},
	}
	out, err := loop.runWorldDecisionTask(context.Background(), &tools.AgentTask{
		AgentID: "keeper",
		RunKind: "world_npc",
		Task:    `{"npc_id":"keeper"}`,
		WorldDecision: &tools.WorldDecisionContext{
			NPCSnapshot: map[string]interface{}{
				"display_name": "Keeper",
			},
		},
	})
	if err != nil {
		t.Fatalf("runWorldDecisionTask failed: %v", err)
	}
	if !strings.Contains(out, `"Welcome to the square."`) {
		t.Fatalf("expected llm JSON to be used, got %s", out)
	}
}

func TestRunWorldDecisionTaskFallsBackWhenLLMOutputInvalid(t *testing.T) {
	loop := &AgentLoop{
		provider: worldDecisionStubProvider{
			content: `not-json at all`,
		},
	}
	out, err := loop.runWorldDecisionTask(context.Background(), &tools.AgentTask{
		AgentID: "keeper",
		RunKind: "world_npc",
		Task:    `{"npc_id":"keeper"}`,
		WorldDecision: &tools.WorldDecisionContext{
			NPCSnapshot: map[string]interface{}{
				"display_name":     "Keeper",
				"current_location": "commons",
			},
			VisibleEvents: []map[string]interface{}{
				{
					"type":        "user_input",
					"location_id": "commons",
					"content":     "I walk into the commons.",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("runWorldDecisionTask failed: %v", err)
	}
	if !strings.Contains(out, `"action":"speak"`) {
		t.Fatalf("expected fallback speak intent, got %s", out)
	}
}

func TestWorldRuntimeHandleUserInputQuestCommands(t *testing.T) {
	workspace := t.TempDir()
	manager := tools.NewAgentManager(nil, workspace, nil)
	runtime := NewWorldRuntime(workspace, manager.ProfileStore(), tools.NewAgentDispatcher(manager), manager)
	if _, err := runtime.CreateQuest(context.Background(), map[string]interface{}{
		"id":      "meet-merchant",
		"title":   "Meet Merchant",
		"summary": "Find the merchant in the square.",
	}); err != nil {
		t.Fatalf("create quest failed: %v", err)
	}

	out, err := runtime.HandleUserInput(context.Background(), "查看任务", "cli", "direct")
	if err != nil {
		t.Fatalf("list quest input failed: %v", err)
	}
	if !strings.Contains(out, "Meet Merchant") {
		t.Fatalf("expected quest list output, got %q", out)
	}

	out, err = runtime.HandleUserInput(context.Background(), "接受任务 Meet Merchant", "cli", "direct")
	if err != nil {
		t.Fatalf("accept quest input failed: %v", err)
	}
	if !strings.Contains(out, "已接受任务") {
		t.Fatalf("expected accept output, got %q", out)
	}
	quest, found, err := runtime.QuestGet("meet-merchant")
	if err != nil || !found {
		t.Fatalf("expected quest after accept, found=%v err=%v", found, err)
	}
	if got := strings.TrimSpace(fmt.Sprint(quest["status"])); got != "accepted" {
		t.Fatalf("expected accepted status, got %q", got)
	}

	out, err = runtime.HandleUserInput(context.Background(), "推进任务 Meet Merchant 已抵达广场", "cli", "direct")
	if err != nil {
		t.Fatalf("progress quest input failed: %v", err)
	}
	if !strings.Contains(out, "已推进任务") {
		t.Fatalf("expected progress output, got %q", out)
	}
	quest, found, err = runtime.QuestGet("meet-merchant")
	if err != nil || !found {
		t.Fatalf("expected quest after progress, found=%v err=%v", found, err)
	}
	if got := strings.TrimSpace(fmt.Sprint(quest["status"])); got != "in_progress" {
		t.Fatalf("expected in_progress status, got %q", got)
	}

	out, err = runtime.HandleUserInput(context.Background(), "完成任务 Meet Merchant", "cli", "direct")
	if err != nil {
		t.Fatalf("complete quest input failed: %v", err)
	}
	if !strings.Contains(out, "已完成任务") {
		t.Fatalf("expected complete output, got %q", out)
	}
	quest, found, err = runtime.QuestGet("meet-merchant")
	if err != nil || !found {
		t.Fatalf("expected quest after complete, found=%v err=%v", found, err)
	}
	if got := strings.TrimSpace(fmt.Sprint(quest["status"])); got != "completed" {
		t.Fatalf("expected completed status, got %q", got)
	}
}
