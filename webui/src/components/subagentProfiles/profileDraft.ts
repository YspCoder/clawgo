export type SubagentProfile = {
  agent_id: string;
  name?: string;
  notify_main_policy?: string;
  role?: string;
  system_prompt_file?: string;
  tool_allowlist?: string[];
  memory_namespace?: string;
  max_retries?: number;
  retry_backoff_ms?: number;
  max_task_chars?: number;
  max_result_chars?: number;
  status?: 'active' | 'disabled' | string;
  created_at?: number;
  updated_at?: number;
};

export type ToolAllowlistGroup = {
  name: string;
  description?: string;
  aliases?: string[];
  tools?: string[];
};

export const emptyDraft: SubagentProfile = {
  agent_id: '',
  name: '',
  notify_main_policy: 'final_only',
  role: '',
  system_prompt_file: '',
  memory_namespace: '',
  status: 'active',
  tool_allowlist: [],
  max_retries: 0,
  retry_backoff_ms: 1000,
  max_task_chars: 0,
  max_result_chars: 0,
};

export function toProfileDraft(profile?: SubagentProfile | null): SubagentProfile {
  if (!profile) return emptyDraft;
  return {
    agent_id: profile.agent_id || '',
    name: profile.name || '',
    notify_main_policy: profile.notify_main_policy || 'final_only',
    role: profile.role || '',
    system_prompt_file: profile.system_prompt_file || '',
    memory_namespace: profile.memory_namespace || '',
    status: (profile.status as string) || 'active',
    tool_allowlist: Array.isArray(profile.tool_allowlist) ? profile.tool_allowlist : [],
    max_retries: Number(profile.max_retries || 0),
    retry_backoff_ms: Number(profile.retry_backoff_ms || 1000),
    max_task_chars: Number(profile.max_task_chars || 0),
    max_result_chars: Number(profile.max_result_chars || 0),
  };
}

export function parseAllowlist(text: string): string[] {
  return text
    .split(',')
    .map((value) => value.trim())
    .filter((value) => value.length > 0);
}
