package world

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Store struct {
	dir        string
	worldPath  string
	npcPath    string
	eventsPath string
	mu         sync.RWMutex
}

func NewStore(workspace string) *Store {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil
	}
	dir := filepath.Join(workspace, "agents", "runtime")
	_ = os.MkdirAll(dir, 0755)
	return &Store{
		dir:        dir,
		worldPath:  filepath.Join(dir, "world_state.json"),
		npcPath:    filepath.Join(dir, "npc_state.json"),
		eventsPath: filepath.Join(dir, "world_events.jsonl"),
	}
}

func DefaultWorldState() WorldState {
	now := time.Now().Unix()
	return WorldState{
		WorldID: "main-world",
		Clock: Clock{
			Tick:         0,
			SimTimeUnix:  now,
			LastAdvance:  now,
			TickDuration: 30,
		},
		Player: PlayerState{
			PlayerID:        "player",
			DisplayName:     "Player",
			CurrentLocation: "commons",
			Status:          "active",
		},
		Locations: map[string]Location{
			"commons": {
				ID:        "commons",
				Name:      "Commons",
				Neighbors: []string{"square"},
			},
			"square": {
				ID:        "square",
				Name:      "Square",
				Neighbors: []string{"commons"},
			},
		},
		GlobalFacts:  map[string]interface{}{},
		Entities:     map[string]Entity{},
		Rooms:        map[string]RoomState{},
		ActiveQuests: map[string]QuestState{},
		RecentEvents: []WorldEvent{},
	}
}

func (s *Store) LoadWorldState() (WorldState, error) {
	if s == nil {
		return DefaultWorldState(), nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, err := os.ReadFile(s.worldPath)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultWorldState(), nil
		}
		return WorldState{}, err
	}
	var state WorldState
	if err := json.Unmarshal(data, &state); err != nil {
		return WorldState{}, err
	}
	if strings.TrimSpace(state.WorldID) == "" {
		state.WorldID = "main-world"
	}
	if state.Clock.TickDuration <= 0 {
		state.Clock.TickDuration = 30
	}
	if state.Locations == nil {
		state.Locations = DefaultWorldState().Locations
	}
	if strings.TrimSpace(state.Player.PlayerID) == "" {
		state.Player = DefaultWorldState().Player
	}
	if strings.TrimSpace(state.Player.CurrentLocation) == "" {
		state.Player.CurrentLocation = "commons"
	}
	if state.GlobalFacts == nil {
		state.GlobalFacts = map[string]interface{}{}
	}
	if state.Entities == nil {
		state.Entities = map[string]Entity{}
	}
	if state.Rooms == nil {
		state.Rooms = map[string]RoomState{}
	}
	if state.ActiveQuests == nil {
		state.ActiveQuests = map[string]QuestState{}
	}
	return state, nil
}

func (s *Store) SaveWorldState(state WorldState) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.worldPath, data, 0644)
}

func (s *Store) LoadNPCStates() (map[string]NPCState, error) {
	if s == nil {
		return map[string]NPCState{}, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, err := os.ReadFile(s.npcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]NPCState{}, nil
		}
		return nil, err
	}
	items := map[string]NPCState{}
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) SaveNPCStates(items map[string]NPCState) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if items == nil {
		items = map[string]NPCState{}
	}
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.npcPath, data, 0644)
}

func (s *Store) SaveNPCState(id string, state NPCState) error {
	items, err := s.LoadNPCStates()
	if err != nil {
		return err
	}
	items[strings.TrimSpace(id)] = state
	return s.SaveNPCStates(items)
}

func (s *Store) AppendWorldEvent(evt WorldEvent) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.eventsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

func (s *Store) Events(limit int) ([]WorldEvent, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, err := os.Open(s.eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []WorldEvent
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var evt WorldEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}
		out = append(out, evt)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tick != out[j].Tick {
			return out[i].Tick > out[j].Tick
		}
		return out[i].CreatedAt > out[j].CreatedAt
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
