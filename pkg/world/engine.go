package world

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type Engine struct{}

func NewEngine() *Engine {
	return &Engine{}
}

func (e *Engine) EnsureWorld(state *WorldState) {
	if state == nil {
		return
	}
	if strings.TrimSpace(state.WorldID) == "" {
		*state = DefaultWorldState()
		return
	}
	if state.Clock.TickDuration <= 0 {
		state.Clock.TickDuration = 30
	}
	if state.Locations == nil || len(state.Locations) == 0 {
		state.Locations = DefaultWorldState().Locations
	}
	if state.GlobalFacts == nil {
		state.GlobalFacts = map[string]interface{}{}
	}
	if state.Entities == nil {
		state.Entities = map[string]Entity{}
	}
	if state.ActiveQuests == nil {
		state.ActiveQuests = map[string]QuestState{}
	}
}

func (e *Engine) EnsureNPCState(blueprint NPCBlueprint, state NPCState) NPCState {
	if strings.TrimSpace(state.NPCID) == "" {
		state.NPCID = strings.TrimSpace(blueprint.NPCID)
	}
	if strings.TrimSpace(state.ProfileRef) == "" {
		state.ProfileRef = strings.TrimSpace(blueprint.NPCID)
	}
	if strings.TrimSpace(state.CurrentLocation) == "" {
		state.CurrentLocation = firstNonEmpty(blueprint.HomeLocation, "commons")
	}
	if len(state.Goals.LongTerm) == 0 && len(blueprint.DefaultGoals) > 0 {
		state.Goals.LongTerm = append([]string(nil), blueprint.DefaultGoals...)
	}
	if state.Beliefs == nil {
		state.Beliefs = map[string]string{}
	}
	if state.Relationships == nil {
		state.Relationships = map[string]string{}
	}
	if state.Inventory == nil {
		state.Inventory = map[string]int{}
	}
	if state.Assets == nil {
		state.Assets = map[string]interface{}{}
	}
	if strings.TrimSpace(state.Status) == "" {
		state.Status = "active"
	}
	return state
}

func (e *Engine) NextTick(state *WorldState) int64 {
	e.EnsureWorld(state)
	state.Clock.Tick++
	now := time.Now().Unix()
	if state.Clock.SimTimeUnix <= 0 {
		state.Clock.SimTimeUnix = now
	}
	state.Clock.SimTimeUnix += maxInt64(state.Clock.TickDuration, 1)
	state.Clock.LastAdvance = now
	return state.Clock.Tick
}

func (e *Engine) BuildUserEvent(state *WorldState, input, locationID string) WorldEvent {
	e.EnsureWorld(state)
	locationID = firstNonEmpty(locationID, "commons")
	return WorldEvent{
		ID:         fmt.Sprintf("evt-user-%d", time.Now().UnixNano()),
		Type:       "user_input",
		Source:     "user",
		ActorID:    "user",
		LocationID: locationID,
		Content:    strings.TrimSpace(input),
		Tick:       state.Clock.Tick,
		CreatedAt:  time.Now().UnixMilli(),
	}
}

func (e *Engine) VisibleEventsForNPC(state WorldState, npc NPCState, events []WorldEvent, scope int) []WorldEvent {
	out := make([]WorldEvent, 0, len(events))
	location := strings.TrimSpace(npc.CurrentLocation)
	for _, evt := range events {
		if evt.LocationID == "" || evt.ActorID == npc.NPCID || evt.Source == "world" {
			out = append(out, evt)
			continue
		}
		if locationVisibleWithinScope(state, location, strings.TrimSpace(evt.LocationID), scope) {
			out = append(out, evt)
			continue
		}
		if scope > 0 && len(evt.VisibleTo) > 0 {
			for _, item := range evt.VisibleTo {
				if strings.TrimSpace(item) == npc.NPCID {
					out = append(out, evt)
					break
				}
			}
		}
	}
	return out
}

func (e *Engine) ApplyIntent(state *WorldState, npc *NPCState, intent ActionIntent) WorldDelta {
	e.EnsureWorld(state)
	delta := WorldDelta{Applied: false, Intent: intent}
	if npc == nil {
		delta.Reason = "npc_state_missing"
		return delta
	}
	switch strings.ToLower(strings.TrimSpace(intent.Action)) {
	case "wait", "":
		delta.Applied = true
		delta.Reason = "noop"
		delta.Event = &WorldEvent{
			ID:         fmt.Sprintf("evt-wait-%d", time.Now().UnixNano()),
			Type:       "npc_wait",
			Source:     "npc",
			ActorID:    npc.NPCID,
			LocationID: npc.CurrentLocation,
			Content:    "waits",
			Tick:       state.Clock.Tick,
			CreatedAt:  time.Now().UnixMilli(),
		}
	case "move":
		target := strings.TrimSpace(intent.TargetLocation)
		if target == "" {
			delta.Reason = "missing_target_location"
			return delta
		}
		current := state.Locations[npc.CurrentLocation]
		if !containsString(current.Neighbors, target) && npc.CurrentLocation != target {
			delta.Reason = "location_not_reachable"
			return delta
		}
		npc.CurrentLocation = target
		npc.LastActiveTick = state.Clock.Tick
		delta.Applied = true
		delta.NPCStateChanged = true
		delta.Event = &WorldEvent{
			ID:         fmt.Sprintf("evt-move-%d", time.Now().UnixNano()),
			Type:       "npc_move",
			Source:     "npc",
			ActorID:    npc.NPCID,
			LocationID: target,
			Content:    target,
			Tick:       state.Clock.Tick,
			CreatedAt:  time.Now().UnixMilli(),
		}
	case "speak":
		delta.Applied = true
		npc.LastActiveTick = state.Clock.Tick
		delta.Event = &WorldEvent{
			ID:         fmt.Sprintf("evt-speak-%d", time.Now().UnixNano()),
			Type:       "npc_speak",
			Source:     "npc",
			ActorID:    npc.NPCID,
			LocationID: npc.CurrentLocation,
			Content:    strings.TrimSpace(intent.Speech),
			Tick:       state.Clock.Tick,
			CreatedAt:  time.Now().UnixMilli(),
		}
	case "observe":
		if npc.Beliefs == nil {
			npc.Beliefs = map[string]string{}
		}
		if summary := strings.TrimSpace(intent.InternalReasoningSummary); summary != "" {
			npc.Beliefs[fmt.Sprintf("tick-%d", state.Clock.Tick)] = summary
		}
		npc.LastActiveTick = state.Clock.Tick
		delta.Applied = true
		delta.NPCStateChanged = true
		delta.Event = &WorldEvent{
			ID:         fmt.Sprintf("evt-observe-%d", time.Now().UnixNano()),
			Type:       "npc_observe",
			Source:     "npc",
			ActorID:    npc.NPCID,
			LocationID: npc.CurrentLocation,
			Content:    strings.TrimSpace(intent.InternalReasoningSummary),
			Tick:       state.Clock.Tick,
			CreatedAt:  time.Now().UnixMilli(),
		}
	case "interact":
		targetEntity := strings.TrimSpace(intent.TargetEntity)
		if targetEntity == "" {
			delta.Reason = "missing_target_entity"
			return delta
		}
		entity, ok := state.Entities[targetEntity]
		if !ok {
			delta.Reason = "entity_not_found"
			return delta
		}
		if !locationVisibleWithinScope(*state, npc.CurrentLocation, entity.LocationID, 1) {
			delta.Reason = "entity_not_reachable"
			return delta
		}
		if entity.State == nil {
			entity.State = map[string]interface{}{}
		}
		entity.State["last_actor"] = npc.NPCID
		entity.State["last_interaction_tick"] = state.Clock.Tick
		if count, ok := entity.State["interaction_count"].(float64); ok {
			entity.State["interaction_count"] = count + 1
		} else if count, ok := entity.State["interaction_count"].(int); ok {
			entity.State["interaction_count"] = count + 1
		} else {
			entity.State["interaction_count"] = 1
		}
		if effect := strings.TrimSpace(intent.Speech); effect != "" {
			entity.State["last_effect"] = effect
		}
		e.applyProposedEffects(state, npc, intent, &entity)
		state.Entities[targetEntity] = entity
		delta.Applied = true
		npc.LastActiveTick = state.Clock.Tick
		delta.NPCStateChanged = true
		delta.Event = &WorldEvent{
			ID:         fmt.Sprintf("evt-interact-%d", time.Now().UnixNano()),
			Type:       "npc_interact",
			Source:     "npc",
			ActorID:    npc.NPCID,
			LocationID: npc.CurrentLocation,
			Content:    firstNonEmpty(targetEntity, intent.Speech, "interacts"),
			Tick:       state.Clock.Tick,
			CreatedAt:  time.Now().UnixMilli(),
		}
	case "delegate":
		delta.Applied = true
		npc.LastActiveTick = state.Clock.Tick
		delta.Event = &WorldEvent{
			ID:         fmt.Sprintf("evt-delegate-%d", time.Now().UnixNano()),
			Type:       "npc_delegate",
			Source:     "npc",
			ActorID:    npc.NPCID,
			LocationID: npc.CurrentLocation,
			Content:    firstNonEmpty(intent.TargetAgent, intent.Speech, "delegates"),
			Tick:       state.Clock.Tick,
			CreatedAt:  time.Now().UnixMilli(),
		}
	default:
		delta.Reason = "unsupported_action"
	}
	return delta
}

func (e *Engine) applyProposedEffects(state *WorldState, npc *NPCState, intent ActionIntent, entity *Entity) {
	if state == nil || intent.ProposedEffects == nil {
		return
	}
	if entity != nil {
		if raw, ok := intent.ProposedEffects["resource_delta"].(map[string]interface{}); ok {
			resources := map[string]int{}
			if existing, ok := entity.State["resources"].(map[string]int); ok {
				for k, v := range existing {
					resources[k] = v
				}
			}
			if existing, ok := entity.State["resources"].(map[string]interface{}); ok {
				for k, v := range existing {
					resources[k] = int(numberToFloat(v))
				}
			}
			for key, value := range raw {
				resources[strings.TrimSpace(key)] += int(numberToFloat(value))
			}
			entity.State["resources"] = resources
		}
		if raw, ok := intent.ProposedEffects["entity_state"].(map[string]interface{}); ok {
			for key, value := range raw {
				entity.State[strings.TrimSpace(key)] = value
			}
		}
	}
	if raw, ok := intent.ProposedEffects["quest_update"].(map[string]interface{}); ok {
		questID := strings.TrimSpace(fmt.Sprint(raw["id"]))
		if questID != "" {
			quest := state.ActiveQuests[questID]
			quest.ID = questID
			if title := strings.TrimSpace(fmt.Sprint(raw["title"])); title != "" {
				quest.Title = title
			}
			if status := strings.TrimSpace(fmt.Sprint(raw["status"])); status != "" {
				quest.Status = status
			}
			if summary := strings.TrimSpace(fmt.Sprint(raw["summary"])); summary != "" {
				quest.Summary = summary
			}
			if owner := strings.TrimSpace(fmt.Sprint(raw["owner_npc_id"])); owner != "" {
				quest.OwnerNPCID = owner
			} else if npc != nil && strings.TrimSpace(quest.OwnerNPCID) == "" {
				quest.OwnerNPCID = npc.NPCID
			}
			if participants, ok := raw["participants"].([]interface{}); ok {
				quest.Participants = make([]string, 0, len(participants))
				for _, item := range participants {
					if v := strings.TrimSpace(fmt.Sprint(item)); v != "" {
						quest.Participants = append(quest.Participants, v)
					}
				}
			}
			state.ActiveQuests[questID] = quest
		}
	}
}

func (e *Engine) AppendRecentEvent(state *WorldState, evt WorldEvent, maxRecent int) {
	if state == nil {
		return
	}
	state.RecentEvents = append([]WorldEvent{evt}, state.RecentEvents...)
	if maxRecent > 0 && len(state.RecentEvents) > maxRecent {
		state.RecentEvents = state.RecentEvents[:maxRecent]
	}
}

func (e *Engine) Snapshot(state WorldState, npcStates map[string]NPCState, recentEvents []WorldEvent, pendingIntents int, limit int) SnapshotSummary {
	if limit > 0 && len(recentEvents) > limit {
		recentEvents = recentEvents[:limit]
	}
	active := make([]string, 0, len(npcStates))
	quests := make([]QuestState, 0, len(state.ActiveQuests))
	occupancy := map[string][]string{}
	entityOccupancy := map[string][]string{}
	for id, npc := range npcStates {
		active = append(active, id)
		loc := firstNonEmpty(npc.CurrentLocation, "commons")
		occupancy[loc] = append(occupancy[loc], id)
	}
	for _, quest := range state.ActiveQuests {
		quests = append(quests, quest)
	}
	for id, entity := range state.Entities {
		loc := firstNonEmpty(entity.LocationID, "commons")
		entityOccupancy[loc] = append(entityOccupancy[loc], id)
	}
	sort.Strings(active)
	sort.Slice(quests, func(i, j int) bool {
		return firstNonEmpty(quests[i].ID, quests[i].Title) < firstNonEmpty(quests[j].ID, quests[j].Title)
	})
	for key := range occupancy {
		sort.Strings(occupancy[key])
	}
	for key := range entityOccupancy {
		sort.Strings(entityOccupancy[key])
	}
	return SnapshotSummary{
		WorldID:            state.WorldID,
		Tick:               state.Clock.Tick,
		SimTimeUnix:        state.Clock.SimTimeUnix,
		Locations:          state.Locations,
		NPCCount:           len(npcStates),
		ActiveNPCs:         active,
		Quests:             quests,
		RecentEvents:       recentEvents,
		PendingIntentCount: pendingIntents,
		Occupancy:          occupancy,
		EntityOccupancy:    entityOccupancy,
		NPCStates:          npcStates,
	}
}

func locationVisibleWithinScope(state WorldState, fromLocation, targetLocation string, scope int) bool {
	fromLocation = strings.TrimSpace(fromLocation)
	targetLocation = strings.TrimSpace(targetLocation)
	if targetLocation == "" || fromLocation == "" {
		return targetLocation == fromLocation
	}
	if fromLocation == targetLocation {
		return true
	}
	if scope <= 0 {
		return false
	}
	visited := map[string]struct{}{fromLocation: {}}
	frontier := []string{fromLocation}
	for depth := 0; depth < scope; depth++ {
		next := make([]string, 0)
		for _, current := range frontier {
			loc, ok := state.Locations[current]
			if !ok {
				continue
			}
			for _, neighbor := range loc.Neighbors {
				neighbor = strings.TrimSpace(neighbor)
				if neighbor == "" {
					continue
				}
				if neighbor == targetLocation {
					return true
				}
				if _, seen := visited[neighbor]; seen {
					continue
				}
				visited[neighbor] = struct{}{}
				next = append(next, neighbor)
			}
		}
		frontier = next
		if len(frontier) == 0 {
			break
		}
	}
	return false
}

func containsString(items []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, item := range items {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}

func maxInt64(v, min int64) int64 {
	if v < min {
		return min
	}
	return v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func numberToFloat(v interface{}) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	case float32:
		return float64(n)
	default:
		return 0
	}
}
