import type { ChatItem } from '../../types';

export type StreamItem = {
  kind?: string;
  at?: number;
  task_id?: string;
  label?: string;
  agent_id?: string;
  event_type?: string;
  message?: string;
  message_type?: string;
  content?: string;
  from_agent?: string;
  to_agent?: string;
  reply_to?: string;
  message_id?: string;
  status?: string;
};

export type RenderedChatItem = ChatItem & {
  id: string;
  actorKey?: string;
  actorName?: string;
  avatarText?: string;
  avatarClassName?: string;
  metaLine?: string;
  isReadonlyGroup?: boolean;
};

export type RegistryAgent = {
  agent_id?: string;
  display_name?: string;
  role?: string;
  enabled?: boolean;
  transport?: string;
};

export type RuntimeTask = {
  id?: string;
  agent_id?: string;
  status?: string;
  updated?: number;
  created?: number;
  waiting_for_reply?: boolean;
};

export type AgentRuntimeBadge = {
  status: 'running' | 'waiting' | 'failed' | 'completed' | 'idle';
  text: string;
};

export function formatAgentName(agentID: string | undefined, t: (key: string) => string): string {
  const normalized = String(agentID || '').trim();
  if (!normalized) return t('unknownAgent');
  if (normalized === 'main') return t('mainAgent');
  return normalized
    .split(/[-_.:]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

export function avatarSeed(key?: string): string {
  const palette = [
    'avatar-tone-1',
    'avatar-tone-2',
    'avatar-tone-3',
    'avatar-tone-4',
    'avatar-tone-5',
    'avatar-tone-6',
    'avatar-tone-7',
  ];
  const source = String(key || 'agent');
  let hash = 0;
  for (let i = 0; i < source.length; i += 1) {
    hash = (hash * 31 + source.charCodeAt(i)) | 0;
  }
  return palette[Math.abs(hash) % palette.length];
}

export function avatarText(name?: string): string {
  const parts = String(name || '')
    .split(/\s+/)
    .filter(Boolean);
  if (parts.length === 0) return 'A';
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return `${parts[0][0] || ''}${parts[1][0] || ''}`.toUpperCase();
}

export function messageActorKey(item: StreamItem): string {
  return String(item.from_agent || item.agent_id || item.to_agent || 'subagent').trim() || 'subagent';
}

export function collectActors(items: StreamItem[]): string[] {
  const set = new Set<string>();
  items.forEach((item) => {
    [item.agent_id, item.from_agent, item.to_agent].forEach((value) => {
      const normalized = String(value || '').trim();
      if (normalized) set.add(normalized);
    });
  });
  return Array.from(set).sort((a, b) => a.localeCompare(b));
}

export function isUserFacingMainSession(key?: string): boolean {
  const normalized = String(key || '').trim().toLowerCase();
  if (!normalized) return false;
  return !(
    normalized.startsWith('subagent:') ||
    normalized.startsWith('internal:') ||
    normalized.startsWith('heartbeat:') ||
    normalized.startsWith('cron:') ||
    normalized.startsWith('hook:') ||
    normalized.startsWith('node:')
  );
}
