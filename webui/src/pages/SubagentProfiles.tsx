import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Check } from 'lucide-react';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';

type SubagentProfile = {
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

type ToolAllowlistGroup = {
  name: string;
  description?: string;
  aliases?: string[];
  tools?: string[];
};

const emptyDraft: SubagentProfile = {
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

const SubagentProfiles: React.FC = () => {
  const { t } = useTranslation();
  const { q } = useAppContext();
  const ui = useUI();

  const [items, setItems] = useState<SubagentProfile[]>([]);
  const [selectedId, setSelectedId] = useState<string>('');
  const [draft, setDraft] = useState<SubagentProfile>(emptyDraft);
  const [saving, setSaving] = useState(false);
  const [groups, setGroups] = useState<ToolAllowlistGroup[]>([]);
  const [promptFileContent, setPromptFileContent] = useState('');
  const [promptFileFound, setPromptFileFound] = useState(false);

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
      notify_main_policy: next.notify_main_policy || 'final_only',
      role: next.role || '',
      system_prompt_file: next.system_prompt_file || '',
      memory_namespace: next.memory_namespace || '',
      status: (next.status as string) || 'active',
      tool_allowlist: Array.isArray(next.tool_allowlist) ? next.tool_allowlist : [],
      max_retries: Number(next.max_retries || 0),
      retry_backoff_ms: Number(next.retry_backoff_ms || 1000),
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

  useEffect(() => {
    const path = String(draft.system_prompt_file || '').trim();
    if (!path) {
      setPromptFileContent('');
      setPromptFileFound(false);
      return;
    }
    fetch(`/webui/api/subagents_runtime${q}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action: 'prompt_file_get', path }),
    })
      .then(async (r) => {
        if (!r.ok) throw new Error(await r.text());
        return r.json();
      })
      .then((data) => {
        const found = data?.result?.found === true;
        setPromptFileFound(found);
        setPromptFileContent(found ? String(data?.result?.content || '') : '');
      })
      .catch(() => {
        setPromptFileFound(false);
        setPromptFileContent('');
      });
  }, [draft.system_prompt_file, q]);

  const onSelect = (p: SubagentProfile) => {
    setSelectedId(p.agent_id || '');
    setDraft({
      agent_id: p.agent_id || '',
      name: p.name || '',
      notify_main_policy: p.notify_main_policy || 'final_only',
      role: p.role || '',
      system_prompt_file: p.system_prompt_file || '',
      memory_namespace: p.memory_namespace || '',
      status: (p.status as string) || 'active',
      tool_allowlist: Array.isArray(p.tool_allowlist) ? p.tool_allowlist : [],
      max_retries: Number(p.max_retries || 0),
      retry_backoff_ms: Number(p.retry_backoff_ms || 1000),
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
          notify_main_policy: draft.notify_main_policy || 'final_only',
          role: draft.role || '',
          system_prompt_file: draft.system_prompt_file || '',
          memory_namespace: draft.memory_namespace || '',
          status: draft.status || 'active',
          tool_allowlist: draft.tool_allowlist || [],
          max_retries: Number(draft.max_retries || 0),
          retry_backoff_ms: Number(draft.retry_backoff_ms || 0),
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

  const savePromptFile = async () => {
    const path = String(draft.system_prompt_file || '').trim();
    if (!path) {
      await ui.notify({ title: t('requestFailed'), message: 'system_prompt_file is required' });
      return;
    }
    const r = await fetch(`/webui/api/subagents_runtime${q}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action: 'prompt_file_set', path, content: promptFileContent }),
    });
    if (!r.ok) {
      await ui.notify({ title: t('requestFailed'), message: await r.text() });
      return;
    }
    setPromptFileFound(true);
    await ui.notify({ title: t('saved'), message: t('promptFileSaved') });
  };

  return (
    <div className="h-full p-4 md:p-6 xl:p-8 flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl md:text-2xl font-semibold">{t('subagentProfiles')}</h1>
        <div className="flex items-center gap-2">
          <button onClick={() => load()} className="px-3 py-1.5 rounded-xl bg-zinc-800 hover:bg-zinc-700 text-sm">
            {t('refresh')}
          </button>
          <button onClick={onNew} className="px-3 py-1.5 rounded-xl bg-emerald-700/80 hover:bg-emerald-600 text-sm">
            {t('newProfile')}
          </button>
        </div>
      </div>

      <div className="flex-1 min-h-0 grid grid-cols-1 lg:grid-cols-[360px_1fr] gap-4">
        <div className="brand-card rounded-[28px] border border-zinc-800 overflow-hidden">
          <div className="px-3 py-2 border-b border-zinc-800 text-xs text-zinc-400 uppercase tracking-wider">
            {t('subagentProfiles')}
          </div>
          <div className="overflow-y-auto max-h-[70vh]">
            {items.map((it) => (
              <button
                key={it.agent_id}
                onClick={() => onSelect(it)}
                className="w-full text-left px-3 py-2 border-b border-zinc-800/50 transition-colors"
              >
                <div className="flex items-center gap-3">
                  <div className="min-w-0 flex-1">
                    <div className="text-sm text-zinc-100 truncate">{it.agent_id || '-'}</div>
                    <div className="text-xs text-zinc-400 truncate">
                      {(it.status || 'active')} · {it.role || '-'} · {(it.memory_namespace || '-')}
                    </div>
                  </div>
                  {selectedId === it.agent_id && (
                    <span className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-indigo-500/15 text-indigo-300 self-center">
                      <Check className="w-3.5 h-3.5" />
                    </span>
                  )}
                </div>
              </button>
            ))}
            {items.length === 0 && (
              <div className="px-3 py-4 text-sm text-zinc-500">No subagent profiles.</div>
            )}
          </div>
        </div>

        <div className="brand-card rounded-[28px] border border-zinc-800 p-4 space-y-3">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <div>
              <div className="text-xs text-zinc-400 mb-1">{t('id')}</div>
              <input
                value={draft.agent_id || ''}
                disabled={!!selected}
                onChange={(e) => setDraft({ ...draft, agent_id: e.target.value })}
                className="w-full px-2 py-1.5 text-xs bg-zinc-900/70 border border-zinc-700 rounded-xl disabled:opacity-60"
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
            <div>
              <div className="text-xs text-zinc-400 mb-1">notify_main_policy</div>
              <select
                value={draft.notify_main_policy || 'final_only'}
                onChange={(e) => setDraft({ ...draft, notify_main_policy: e.target.value })}
                className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded"
              >
                <option value="final_only">final_only</option>
                <option value="internal_only">internal_only</option>
                <option value="milestone">milestone</option>
                <option value="on_blocked">on_blocked</option>
                <option value="always">always</option>
              </select>
            </div>
            <div className="md:col-span-2">
              <div className="text-xs text-zinc-400 mb-1">system_prompt_file</div>
              <input
                value={draft.system_prompt_file || ''}
                onChange={(e) => setDraft({ ...draft, system_prompt_file: e.target.value })}
                className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded"
                placeholder="agents/coder/AGENT.md"
              />
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
              <div className="mt-1 text-[11px] text-zinc-500">
                <span className="font-mono text-zinc-400">skill_exec</span> is inherited automatically and does not need to be listed here.
              </div>
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
              <div className="flex items-center justify-between mb-1 gap-3">
                <div className="text-xs text-zinc-400">system_prompt_file content</div>
                <div className="text-[11px] text-zinc-500">{promptFileFound ? t('promptFileReady') : t('promptFileMissing')}</div>
              </div>
              <textarea
                value={promptFileContent}
                onChange={(e) => setPromptFileContent(e.target.value)}
                className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded min-h-[220px]"
                placeholder={t('agentPromptContentPlaceholder')}
              />
              <div className="mt-2 flex items-center gap-2">
                <button
                  type="button"
                  onClick={savePromptFile}
                  disabled={!String(draft.system_prompt_file || '').trim()}
                  className="px-3 py-1.5 text-xs rounded bg-zinc-800 hover:bg-zinc-700 disabled:opacity-50"
                >
                  {t('savePromptFile')}
                </button>
              </div>
            </div>
            <div>
              <div className="text-xs text-zinc-400 mb-1">{t('maxRetries')}</div>
              <input
                type="number"
                min={0}
                value={Number(draft.max_retries || 0)}
                onChange={(e) => setDraft({ ...draft, max_retries: Number(e.target.value) || 0 })}
                className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded"
              />
            </div>
            <div>
              <div className="text-xs text-zinc-400 mb-1">{t('retryBackoffMs')}</div>
              <input
                type="number"
                min={0}
                value={Number(draft.retry_backoff_ms || 0)}
                onChange={(e) => setDraft({ ...draft, retry_backoff_ms: Number(e.target.value) || 0 })}
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
              className="px-3 py-1.5 text-xs rounded-xl border border-indigo-400/30 bg-indigo-500/14 text-indigo-100 hover:bg-indigo-500/22 disabled:opacity-60"
            >
              {selected ? t('update') : t('create')}
            </button>
            <button
              onClick={() => setStatus('active')}
              disabled={!draft.agent_id}
              className="px-3 py-1.5 text-xs rounded-xl border border-emerald-400/30 bg-emerald-500/14 text-emerald-100 hover:bg-emerald-500/22 disabled:opacity-50"
            >
              {t('enable')}
            </button>
            <button
              onClick={() => setStatus('disabled')}
              disabled={!draft.agent_id}
              className="px-3 py-1.5 text-xs rounded-xl border border-amber-400/30 bg-amber-500/14 text-amber-100 hover:bg-amber-500/22 disabled:opacity-50"
            >
              {t('disable')}
            </button>
            <button
              onClick={remove}
              disabled={!draft.agent_id}
              className="px-3 py-1.5 text-xs rounded-xl border border-rose-400/30 bg-rose-500/14 text-rose-100 hover:bg-rose-500/22 disabled:opacity-50"
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
