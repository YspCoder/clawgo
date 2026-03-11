export type SubagentTask = {
  id: string;
  status?: string;
  label?: string;
  role?: string;
  agent_id?: string;
  session_key?: string;
  memory_ns?: string;
  tool_allowlist?: string[];
  max_retries?: number;
  retry_count?: number;
  retry_backoff?: number;
  max_task_chars?: number;
  max_result_chars?: number;
  created?: number;
  updated?: number;
  task?: string;
  result?: string;
  thread_id?: string;
  correlation_id?: string;
  waiting_for_reply?: boolean;
};

export type StreamItem = {
  kind?: 'event' | 'message' | string;
  at?: number;
  run_id?: string;
  agent_id?: string;
  event_type?: string;
  message?: string;
  retry_count?: number;
  message_id?: string;
  thread_id?: string;
  from_agent?: string;
  to_agent?: string;
  reply_to?: string;
  correlation_id?: string;
  message_type?: string;
  content?: string;
  status?: string;
  requires_reply?: boolean;
};

export type RegistrySubagent = {
  agent_id?: string;
  enabled?: boolean;
  type?: string;
  transport?: string;
  node_id?: string;
  parent_agent_id?: string;
  managed_by?: string;
  notify_main_policy?: string;
  display_name?: string;
  role?: string;
  description?: string;
  system_prompt_file?: string;
  prompt_file_found?: boolean;
  memory_namespace?: string;
  tool_allowlist?: string[];
  inherited_tools?: string[];
  effective_tools?: string[];
  tool_visibility?: {
    mode?: string;
    inherited_tool_count?: number;
    effective_tool_count?: number;
  };
  routing_keywords?: string[];
};

export type AgentTreeNode = {
  agent_id?: string;
  display_name?: string;
  role?: string;
  type?: string;
  transport?: string;
  managed_by?: string;
  node_id?: string;
  enabled?: boolean;
  children?: AgentTreeNode[];
};

export type NodeTree = {
  node_id?: string;
  node_name?: string;
  online?: boolean;
  source?: string;
  readonly?: boolean;
  root?: {
    root?: AgentTreeNode;
  };
};

export type StreamPreviewState = {
  task: SubagentTask | null;
  items: StreamItem[];
  taskID: string;
  loading?: boolean;
};
