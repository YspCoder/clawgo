package rpc

import "context"

type ConfigService interface {
	View(context.Context, ConfigViewRequest) (*ConfigViewResponse, *Error)
	Save(context.Context, ConfigSaveRequest) (*ConfigSaveResponse, *Error)
}

type CronService interface {
	List(context.Context, ListCronJobsRequest) (*ListCronJobsResponse, *Error)
	Get(context.Context, GetCronJobRequest) (*GetCronJobResponse, *Error)
	Mutate(context.Context, MutateCronJobRequest) (*MutateCronJobResponse, *Error)
}

type ConfigViewRequest struct {
	Mode                 string `json:"mode,omitempty"`
	IncludeHotReloadInfo bool   `json:"include_hot_reload_info,omitempty"`
}

type ConfigViewResponse struct {
	Config                interface{}              `json:"config,omitempty"`
	RawConfig             interface{}              `json:"raw_config,omitempty"`
	PrettyText            string                   `json:"pretty_text,omitempty"`
	HotReloadFields       []string                 `json:"hot_reload_fields,omitempty"`
	HotReloadFieldDetails []map[string]interface{} `json:"hot_reload_field_details,omitempty"`
}

type ConfigSaveRequest struct {
	Mode         string                 `json:"mode,omitempty"`
	ConfirmRisky bool                   `json:"confirm_risky,omitempty"`
	Config       map[string]interface{} `json:"config"`
}

type ConfigSaveResponse struct {
	Saved           bool        `json:"saved"`
	RequiresConfirm bool        `json:"requires_confirm,omitempty"`
	ChangedFields   []string    `json:"changed_fields,omitempty"`
	Details         interface{} `json:"details,omitempty"`
}

type ListCronJobsRequest struct{}

type ListCronJobsResponse struct {
	Jobs []interface{} `json:"jobs"`
}

type GetCronJobRequest struct {
	ID string `json:"id"`
}

type GetCronJobResponse struct {
	Job interface{} `json:"job,omitempty"`
}

type MutateCronJobRequest struct {
	Action string                 `json:"action"`
	Args   map[string]interface{} `json:"args,omitempty"`
}

type MutateCronJobResponse struct {
	Result interface{} `json:"result,omitempty"`
}
