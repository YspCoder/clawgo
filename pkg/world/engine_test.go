package world

import "testing"

func TestVisibleEventsForNPCIncludesNeighborLocationsWithinScope(t *testing.T) {
	engine := NewEngine()
	state := DefaultWorldState()
	npc := NPCState{NPCID: "keeper", CurrentLocation: "commons"}
	events := []WorldEvent{
		{ID: "a", Type: "noise", LocationID: "square", Content: "neighbor event"},
		{ID: "b", Type: "noise", LocationID: "far", Content: "far event"},
	}
	state.Locations["far"] = Location{ID: "far", Neighbors: []string{}}
	out := engine.VisibleEventsForNPC(state, npc, events, 1)
	if len(out) != 1 || out[0].ID != "a" {
		t.Fatalf("expected only neighbor event, got %+v", out)
	}
}

func TestApplyIntentInteractUpdatesEntityState(t *testing.T) {
	engine := NewEngine()
	state := DefaultWorldState()
	state.Entities["fountain"] = Entity{
		ID:         "fountain",
		Name:       "Fountain",
		LocationID: "square",
		State:      map[string]interface{}{},
	}
	npc := &NPCState{NPCID: "merchant", CurrentLocation: "square"}
	delta := engine.ApplyIntent(&state, npc, ActionIntent{
		ActorID:      "merchant",
		Action:       "interact",
		TargetEntity: "fountain",
		Speech:       "inspects the fountain",
	})
	if !delta.Applied || delta.Event == nil {
		t.Fatalf("expected interact delta to apply, got %+v", delta)
	}
	entity := state.Entities["fountain"]
	if entity.State["last_actor"] != "merchant" {
		t.Fatalf("expected entity last_actor updated, got %+v", entity.State)
	}
	if entity.State["interaction_count"] == nil {
		t.Fatalf("expected interaction_count, got %+v", entity.State)
	}
}

func TestApplyIntentInteractRejectsDistantEntity(t *testing.T) {
	engine := NewEngine()
	state := DefaultWorldState()
	state.Locations["far"] = Location{ID: "far", Neighbors: []string{}}
	state.Entities["orb"] = Entity{
		ID:         "orb",
		LocationID: "far",
	}
	npc := &NPCState{NPCID: "sage", CurrentLocation: "commons"}
	delta := engine.ApplyIntent(&state, npc, ActionIntent{
		ActorID:      "sage",
		Action:       "interact",
		TargetEntity: "orb",
	})
	if delta.Applied {
		t.Fatalf("expected distant interact to be rejected, got %+v", delta)
	}
	if delta.Reason != "entity_not_reachable" {
		t.Fatalf("unexpected rejection reason: %+v", delta)
	}
}

func TestApplyIntentInteractAppliesQuestAndResourceEffects(t *testing.T) {
	engine := NewEngine()
	state := DefaultWorldState()
	state.Entities["chest"] = Entity{
		ID:         "chest",
		LocationID: "square",
		State:      map[string]interface{}{},
	}
	npc := &NPCState{NPCID: "rogue", CurrentLocation: "square"}
	delta := engine.ApplyIntent(&state, npc, ActionIntent{
		ActorID:      "rogue",
		Action:       "interact",
		TargetEntity: "chest",
		ProposedEffects: map[string]interface{}{
			"resource_delta": map[string]interface{}{"gold": 3},
			"quest_update": map[string]interface{}{
				"id":     "find-chest",
				"title":  "Find The Chest",
				"status": "completed",
			},
		},
	})
	if !delta.Applied {
		t.Fatalf("expected interact to apply, got %+v", delta)
	}
	resources, ok := state.Entities["chest"].State["resources"].(map[string]int)
	if !ok || resources["gold"] != 3 {
		t.Fatalf("expected gold resource delta, got %+v", state.Entities["chest"].State)
	}
	quest := state.ActiveQuests["find-chest"]
	if quest.Status != "completed" {
		t.Fatalf("expected quest completion, got %+v", quest)
	}
}
