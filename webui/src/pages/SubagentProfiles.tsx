import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';

type SubagentProfile = {
  agent_id: string;
  name?: string;
  role?: string;
  system_prompt?: string;
  tool_allowlist?: string[];
  memory_namespace?: string;
  max_retries?: number;
  retry_backoff_ms?: number;
  timeout_sec?: number;
  max_task_chars?: number;
  max_result_chars?: number;
  status?: 'active' | 'disabled' | string;
  created_at?: number;
  updated_at?: number;
};

type ToolAllowlistGroup = {
  name: string;
  description?: string;
  aliases?: string[];
  tools?: string[];
};

const emptyDraft: SubagentProfile = {
  agent_id: '',
  name: '',
  role: '',
  system_prompt: '',
  memory_namespace: '',
  status: 'active',
  tool_allowlist: [],
  max_retries: 0,
  retry_backoff_ms: 1000,
  timeout_sec: 0,
  max_task_chars: 0,
  max_result_chars: 0,
};

const SubagentProfiles: React.FC = () => {
  const { t } = useTranslation();
  const { q } = useAppContext();
  const ui = useUI();

  const [items, setItems] = useState<SubagentProfile[]>([]);
  const [selectedId, setSelectedId] = useState<string>('');
  const [draft, setDraft] = useState<SubagentProfile>(emptyDraft);
  const [saving, setSaving] = useState(false);
  const [groups, setGroups] = useState<ToolAllowlistGroup[]>([]);

  const selected = useMemo(
    () => items.find((p) => p.agent_id === selectedId) || null,
    [items, selectedId],
  );

  const load = async () => {
    const r = await fetch(`/webui/api/subagent_profiles${q}`);
    if (!r.ok) throw new Error(await r.text());
    const j = await r.json();
    const profiles = Array.isArray(j.profiles) ? j.profiles : [];
    setItems(profiles);
    if (profiles.length === 0) {
      setSelectedId('');
      setDraft(emptyDraft);
      return;
    }
    const keep = profiles.find((p: SubagentProfile) => p.agent_id === selectedId);
    const next = keep || profiles[0];
    setSelectedId(next.agent_id || '');
    setDraft({
      agent_id: next.agent_id || '',
      name: next.name || '',
      role: next.role || '',
      system_prompt: next.system_prompt || '',
      memory_namespace: next.memory_namespace || '',
      status: (next.status as string) || 'active',
      tool_allowlist: Array.isArray(next.tool_allowlist) ? next.tool_allowlist : [],
      max_retries: Number(next.max_retries || 0),
      retry_backoff_ms: Number(next.retry_backoff_ms || 1000),
      timeout_sec: Number(next.timeout_sec || 0),
      max_task_chars: Number(next.max_task_chars || 0),
      max_result_chars: Number(next.max_result_chars || 0),
    });
  };

  useEffect(() => {
    load().catch(() => {});
  }, [q]);

  useEffect(() => {
    const loadGroups = async () => {
      const r = await fetch(`/webui/api/tool_allowlist_groups${q}`);
      if (!r.ok) return;
      const j = await r.json();
      const arr = Array.isArray(j.groups) ? j.groups : [];
      setGroups(arr);
    };
    loadGroups().catch(() => {});
  }, [q]);

  const onSelect = (p: SubagentProfile) => {
    setSelectedId(p.agent_id || '');
    setDraft({
      agent_id: p.agent_id || '',
      name: p.name || '',
      role: p.role || '',
      system_prompt: p.system_prompt || '',
      memory_namespace: p.memory_namespace || '',
      status: (p.status as string) || 'active',
      tool_allowlist: Array.isArray(p.tool_allowlist) ? p.tool_allowlist : [],
      max_retries: Number(p.max_retries || 0),
      retry_backoff_ms: Number(p.retry_backoff_ms || 1000),
      timeout_sec: Number(p.timeout_sec || 0),
      max_task_chars: Number(p.max_task_chars || 0),
      max_result_chars: Number(p.max_result_chars || 0),
    });
  };

  const onNew = () => {
    setSelectedId('');
    setDraft(emptyDraft);
  };

  const parseAllowlist = (text: string): string[] => {
    return text
      .split(',')
      .map((x) => x.trim())
      .filter((x) => x.length > 0);
  };

  const allowlistText = (draft.tool_allowlist || []).join(', ');

  const addAllowlistToken = (token: string) => {
    const list = Array.isArray(draft.tool_allowlist) ? [...draft.tool_allowlist] : [];
    if (!list.includes(token)) {
      list.push(token);
      setDraft({ ...draft, tool_allowlist: list });
    }
  };

  const save = async () => {
    const agentId = String(draft.agent_id || '').trim();
    if (!agentId) {
      await ui.notify({ title: t('requestFailed'), message: 'agent_id is required' });
      return;
    }

    setSaving(true);
    try {
      const action = selected ? 'update' : 'create';
      const r = await fetch(`/webui/api/subagent_profiles${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          action,
          agent_id: agentId,
          name: draft.name || '',
          role: draft.role || '',
          system_prompt: draft.system_prompt || '',
          memory_namespace: draft.memory_namespace || '',
          status: draft.status || 'active',
          tool_allowlist: draft.tool_allowlist || [],
          max_retries: Number(draft.max_retries || 0),
          retry_backoff_ms: Number(draft.retry_backoff_ms || 0),
          timeout_sec: Number(draft.timeout_sec || 0),
          max_task_chars: Number(draft.max_task_chars || 0),
          max_result_chars: Number(draft.max_result_chars || 0),
        }),
      });
      if (!r.ok) {
        await ui.notify({ title: t('requestFailed'), message: await r.text() });
        return;
      }
      await load();
      setSelectedId(agentId);
    } finally {
      setSaving(false);
    }
  };

  const setStatus = async (status: 'active' | 'disabled') => {
    const agentId = String(draft.agent_id || '').trim();
    if (!agentId) return;
    const action = status === 'active' ? 'enable' : 'disable';
    const r = await fetch(`/webui/api/subagent_profiles${q}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action, agent_id: agentId }),
    });
    if (!r.ok) {
      await ui.notify({ title: t('requestFailed'), message: await r.text() });
      return;
    }
    await load();
  };

  const remove = async () => {
    const agentId = String(draft.agent_id || '').trim();
    if (!agentId) return;
    const ok = await ui.confirmDialog({
      title: t('subagentDeleteConfirmTitle'),
      message: t('subagentDeleteConfirmMessage', { id: agentId }),
      danger: true,
      confirmText: t('delete'),
    });
    if (!ok) return;
    const delQ = `${q}${q ? '&' : '?'}agent_id=${encodeURIComponent(agentId)}`;
    const r = await fetch(`/webui/api/subagent_profiles${delQ}`, { method: 'DELETE' });
    if (!r.ok) {
      await ui.notify({ title: t('requestFailed'), message: await r.text() });
      return;
    }
    await load();
  };

  return (
    <div className="h-full p-4 md:p-6 flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl md:text-2xl font-semibold">{t('subagentProfiles')}</h1>
        <div className="flex items-center gap-2">
          <button onClick={() => load()} className="px-3 py-1.5 rounded-lg bg-zinc-800 hover:bg-zinc-700 text-sm">
            {t('refresh')}
          </button>
          <button onClick={onNew} className="px-3 py-1.5 rounded-lg bg-emerald-700/80 hover:bg-emerald-600 text-sm">
            {t('newProfile')}
          </button>
        </div>
      </div>

      <div className="flex-1 min-h-0 grid grid-cols-1 lg:grid-cols-[360px_1fr] gap-4">
        <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 overflow-hidden">
          <div className="px-3 py-2 border-b border-zinc-800 text-xs text-zinc-400 uppercase tracking-wider">
            {t('subagentProfiles')}
          </div>
          <div className="overflow-y-auto max-h-[70vh]">
            {items.map((it) => (
              <button
                key={it.agent_id}
                onClick={() => onSelect(it)}
                className={`w-full text-left px-3 py-2 border-b border-zinc-800/50 hover:bg-zinc-800/40 ${selectedId === it.agent_id ? 'bg-indigo-500/15' : ''}`}
              >
                <div className="text-sm text-zinc-100 truncate">{it.agent_id || '-'}</div>
                <div className="text-xs text-zinc-400 truncate">
                  {(it.status || 'active')} · {it.role || '-'} · {(it.memory_namespace || '-')}
                </div>
              </button>
            ))}
            {items.length === 0 && (
              <div className="px-3 py-4 text-sm text-zinc-500">No subagent profiles.</div>
            )}
          </div>
        </div>

        <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <div>
              <div className="text-xs text-zinc-400 mb-1">{t('id')}</div>
              <input
                value={draft.agent_id || ''}
                disabled={!!selected}
                onChange={(e) => setDraft({ ...draft, agent_id: e.target.value })}
                className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded disabled:opacity-60"
                placeholder="coder"
              />
            </div>
            <div>
              <div className="text-xs text-zinc-400 mb-1">{t('name')}</div>
              <input
                value={draft.name || ''}
                onChange={(e) => setDraft({ ...draft, name: e.target.value })}
                className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded"
                placeholder="Code Agent"
              />
            </div>
            <div>
              <div className="text-xs text-zinc-400 mb-1">Role</div>
              <input
                value={draft.role || ''}
                onChange={(e) => setDraft({ ...draft, role: e.target.value })}
                className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded"
                placeholder="coding"
              />
            </div>
            <div>
              <div className="text-xs text-zinc-400 mb-1">{t('status')}</div>
              <select
                value={draft.status || 'active'}
                onChange={(e) => setDraft({ ...draft, status: e.target.value })}
                className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded"
              >
                <option value="active">active</option>
                <option value="disabled">disabled</option>
              </select>
            </div>
            <div className="md:col-span-2">
              <div className="text-xs text-zinc-400 mb-1">{t('memoryNamespace')}</div>
              <input
                value={draft.memory_namespace || ''}
                onChange={(e) => setDraft({ ...draft, memory_namespace: e.target.value })}
                className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded"
                placeholder="coder"
              />
            </div>
            <div className="md:col-span-2">
              <div className="text-xs text-zinc-400 mb-1">{t('toolAllowlist')}</div>
              <input
                value={allowlistText}
                onChange={(e) => setDraft({ ...draft, tool_allowlist: parseAllowlist(e.target.value) })}
                className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded"
                placeholder="read_file, list_files, memory_search"
              />
              {groups.length > 0 && (
                <div className="mt-2 flex flex-wrap gap-2">
                  {groups.map((g) => (
                    <button
                      key={g.name}
                      type="button"
                      onClick={() => addAllowlistToken(`group:${g.name}`)}
                      className="px-2 py-1 text-[11px] rounded bg-zinc-800 hover:bg-zinc-700 text-zinc-200"
                      title={g.description || g.name}
                    >
                      {`group:${g.name}`}
                    </button>
                  ))}
                </div>
              )}
            </div>
            <div className="md:col-span-2">
              <div className="text-xs text-zinc-400 mb-1">System Prompt</div>
              <textarea
                value={draft.system_prompt || ''}
                onChange={(e) => setDraft({ ...draft, system_prompt: e.target.value })}
                className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded min-h-[140px]"
                placeholder="You are a coding specialist..."
              />
            </div>
            <div>
              <div className="text-xs text-zinc-400 mb-1">Max Retries</div>
              <input
                type="number"
                min={0}
                value={Number(draft.max_retries || 0)}
                onChange={(e) => setDraft({ ...draft, max_retries: Number(e.target.value) || 0 })}
                className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded"
              />
            </div>
            <div>
              <div className="text-xs text-zinc-400 mb-1">Retry Backoff (ms)</div>
              <input
                type="number"
                min={0}
                value={Number(draft.retry_backoff_ms || 0)}
                onChange={(e) => setDraft({ ...draft, retry_backoff_ms: Number(e.target.value) || 0 })}
                className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded"
              />
            </div>
            <div>
              <div className="text-xs text-zinc-400 mb-1">Timeout (sec)</div>
              <input
                type="number"
                min={0}
                value={Number(draft.timeout_sec || 0)}
                onChange={(e) => setDraft({ ...draft, timeout_sec: Number(e.target.value) || 0 })}
                className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded"
              />
            </div>
            <div>
              <div className="text-xs text-zinc-400 mb-1">Max Task Chars</div>
              <input
                type="number"
                min={0}
                value={Number(draft.max_task_chars || 0)}
                onChange={(e) => setDraft({ ...draft, max_task_chars: Number(e.target.value) || 0 })}
                className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded"
              />
            </div>
            <div className="md:col-span-2">
              <div className="text-xs text-zinc-400 mb-1">Max Result Chars</div>
              <input
                type="number"
                min={0}
                value={Number(draft.max_result_chars || 0)}
                onChange={(e) => setDraft({ ...draft, max_result_chars: Number(e.target.value) || 0 })}
                className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded"
              />
            </div>
          </div>

          <div className="flex items-center gap-2">
            <button
              onClick={save}
              disabled={saving}
              className="px-3 py-1.5 text-xs rounded bg-indigo-700/80 hover:bg-indigo-600 disabled:opacity-60"
            >
              {selected ? t('update') : t('create')}
            </button>
            <button
              onClick={() => setStatus('active')}
              disabled={!draft.agent_id}
              className="px-3 py-1.5 text-xs rounded bg-emerald-700/70 hover:bg-emerald-600 disabled:opacity-50"
            >
              Enable
            </button>
            <button
              onClick={() => setStatus('disabled')}
              disabled={!draft.agent_id}
              className="px-3 py-1.5 text-xs rounded bg-amber-700/70 hover:bg-amber-600 disabled:opacity-50"
            >
              Disable
            </button>
            <button
              onClick={remove}
              disabled={!draft.agent_id}
              className="px-3 py-1.5 text-xs rounded bg-red-700/70 hover:bg-red-600 disabled:opacity-50"
            >
              {t('delete')}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};

export default SubagentProfiles;
