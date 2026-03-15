package world

type Clock struct {
	Tick         int64 `json:"tick"`
	SimTimeUnix  int64 `json:"sim_time_unix"`
	LastAdvance  int64 `json:"last_advance_unix"`
	TickDuration int64 `json:"tick_duration_sec"`
}

type Location struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Neighbors   []string `json:"neighbors,omitempty"`
}

type Entity struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name,omitempty"`
	Type       string                 `json:"type,omitempty"`
	LocationID string                 `json:"location_id,omitempty"`
	State      map[string]interface{} `json:"state,omitempty"`
}

type QuestState struct {
	ID           string   `json:"id"`
	Title        string   `json:"title,omitempty"`
	Status       string   `json:"status,omitempty"`
	OwnerNPCID   string   `json:"owner_npc_id,omitempty"`
	Participants []string `json:"participants,omitempty"`
	Summary      string   `json:"summary,omitempty"`
}

type WorldEvent struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Source     string                 `json:"source,omitempty"`
	ActorID    string                 `json:"actor_id,omitempty"`
	LocationID string                 `json:"location_id,omitempty"`
	Content    string                 `json:"content,omitempty"`
	VisibleTo  []string               `json:"visible_to,omitempty"`
	Payload    map[string]interface{} `json:"payload,omitempty"`
	Tick       int64                  `json:"tick"`
	CreatedAt  int64                  `json:"created_at"`
}

type WorldState struct {
	WorldID      string                 `json:"world_id"`
	Clock        Clock                  `json:"clock"`
	Locations    map[string]Location    `json:"locations,omitempty"`
	GlobalFacts  map[string]interface{} `json:"global_facts,omitempty"`
	Entities     map[string]Entity      `json:"entities,omitempty"`
	ActiveQuests map[string]QuestState  `json:"active_quests,omitempty"`
	RecentEvents []WorldEvent           `json:"recent_events,omitempty"`
}

type GoalSet struct {
	ShortTerm []string `json:"short_term,omitempty"`
	LongTerm  []string `json:"long_term,omitempty"`
}

type NPCState struct {
	NPCID                string                 `json:"npc_id"`
	ProfileRef           string                 `json:"profile_ref,omitempty"`
	CurrentLocation      string                 `json:"current_location,omitempty"`
	Goals                GoalSet                `json:"goals,omitempty"`
	Beliefs              map[string]string      `json:"beliefs,omitempty"`
	Mood                 string                 `json:"mood,omitempty"`
	Relationships        map[string]string      `json:"relationships,omitempty"`
	Inventory            map[string]int         `json:"inventory,omitempty"`
	Assets               map[string]interface{} `json:"assets,omitempty"`
	PrivateMemorySummary string                 `json:"private_memory_summary,omitempty"`
	Status               string                 `json:"status,omitempty"`
	LastActiveTick       int64                  `json:"last_active_tick,omitempty"`
}

type NPCBlueprint struct {
	NPCID           string   `json:"npc_id"`
	DisplayName     string   `json:"display_name,omitempty"`
	Kind            string   `json:"kind,omitempty"`
	Role            string   `json:"role,omitempty"`
	Persona         string   `json:"persona,omitempty"`
	Traits          []string `json:"traits,omitempty"`
	Faction         string   `json:"faction,omitempty"`
	HomeLocation    string   `json:"home_location,omitempty"`
	DefaultGoals    []string `json:"default_goals,omitempty"`
	PerceptionScope int      `json:"perception_scope,omitempty"`
	ScheduleHint    string   `json:"schedule_hint,omitempty"`
	WorldTags       []string `json:"world_tags,omitempty"`
	MemoryNamespace string   `json:"memory_namespace,omitempty"`
	PromptFile      string   `json:"prompt_file,omitempty"`
}

type ActionIntent struct {
	ActorID                  string                 `json:"actor_id"`
	Action                   string                 `json:"action"`
	TargetLocation           string                 `json:"target_location,omitempty"`
	TargetEntity             string                 `json:"target_entity,omitempty"`
	TargetAgent              string                 `json:"target_agent,omitempty"`
	Speech                   string                 `json:"speech,omitempty"`
	InternalReasoningSummary string                 `json:"internal_reasoning_summary,omitempty"`
	ProposedEffects          map[string]interface{} `json:"proposed_effects,omitempty"`
}

type WorldDelta struct {
	Applied         bool         `json:"applied"`
	Reason          string       `json:"reason,omitempty"`
	Intent          ActionIntent `json:"intent"`
	Event           *WorldEvent  `json:"event,omitempty"`
	NPCStateChanged bool         `json:"npc_state_changed,omitempty"`
}

type WorldTickRequest struct {
	Source        string       `json:"source,omitempty"`
	UserInput     string       `json:"user_input,omitempty"`
	LocationID    string       `json:"location_id,omitempty"`
	CatchUpTicks  int          `json:"catch_up_ticks,omitempty"`
	MaxNPCPerTick int          `json:"max_npc_per_tick,omitempty"`
	VisibleEvents []WorldEvent `json:"visible_events,omitempty"`
}

type RenderedResult struct {
	Text         string         `json:"text,omitempty"`
	Tick         int64          `json:"tick,omitempty"`
	Intents      []ActionIntent `json:"intents,omitempty"`
	RecentEvents []WorldEvent   `json:"recent_events,omitempty"`
}

type SnapshotSummary struct {
	WorldID            string              `json:"world_id,omitempty"`
	Tick               int64               `json:"tick,omitempty"`
	SimTimeUnix        int64               `json:"sim_time_unix,omitempty"`
	Locations          map[string]Location `json:"locations,omitempty"`
	NPCCount           int                 `json:"npc_count,omitempty"`
	ActiveNPCs         []string            `json:"active_npcs,omitempty"`
	Quests             []QuestState        `json:"quests,omitempty"`
	RecentEvents       []WorldEvent        `json:"recent_events,omitempty"`
	PendingIntentCount int                 `json:"pending_intent_count,omitempty"`
	Occupancy          map[string][]string `json:"occupancy,omitempty"`
	EntityOccupancy    map[string][]string `json:"entity_occupancy,omitempty"`
	NPCStates          map[string]NPCState `json:"npc_states,omitempty"`
}
