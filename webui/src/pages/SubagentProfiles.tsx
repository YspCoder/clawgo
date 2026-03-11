import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Check, Plus, RefreshCw } from 'lucide-react';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
import { Button, FixedButton } from '../components/Button';
import { FieldBlock, SelectField, TextField, TextareaField } from '../components/FormControls';

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
          <FixedButton onClick={() => load()} label={t('refresh')}>
            <RefreshCw className="w-4 h-4" />
          </FixedButton>
          <FixedButton onClick={onNew} variant="success" label={t('newProfile')}>
            <Plus className="w-4 h-4" />
          </FixedButton>
        </div>
      </div>

      <div className="flex-1 min-h-0 grid grid-cols-1 lg:grid-cols-[360px_1fr] gap-4">
        <div className="brand-card ui-border-subtle rounded-[28px] border overflow-hidden">
          <div className="ui-border-subtle ui-text-subtle px-3 py-2 border-b text-xs uppercase tracking-wider">
            {t('subagentProfiles')}
          </div>
          <div className="overflow-y-auto max-h-[70vh]">
            {items.map((it) => (
              <button
                key={it.agent_id}
                onClick={() => onSelect(it)}
                className="ui-border-subtle ui-row-hover w-full text-left px-3 py-2 border-b transition-colors"
              >
                <div className="flex items-center gap-3">
                  <div className="min-w-0 flex-1">
                    <div className="ui-text-primary text-sm truncate">{it.agent_id || '-'}</div>
                    <div className="ui-text-subtle text-xs truncate">
                      {(it.status || 'active')} · {it.role || '-'} · {(it.memory_namespace || '-')}
                    </div>
                  </div>
                  {selectedId === it.agent_id && (
                    <span className="ui-pill ui-pill-info inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full self-center">
                      <Check className="w-3.5 h-3.5" />
                    </span>
                  )}
                </div>
              </button>
            ))}
            {items.length === 0 && (
              <div className="ui-text-muted px-3 py-4 text-sm">No subagent profiles.</div>
            )}
          </div>
        </div>

        <div className="brand-card ui-border-subtle rounded-[28px] border p-4 space-y-3">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <FieldBlock label={t('id')}>
              <TextField
                value={draft.agent_id || ''}
                disabled={!!selected}
                onChange={(e) => setDraft({ ...draft, agent_id: e.target.value })}
                dense
                className="w-full disabled:opacity-60"
                placeholder="coder"
              />
            </FieldBlock>
            <FieldBlock label={t('name')}>
              <TextField
                value={draft.name || ''}
                onChange={(e) => setDraft({ ...draft, name: e.target.value })}
                dense
                className="w-full"
                placeholder="Code Agent"
              />
            </FieldBlock>
            <FieldBlock label="Role">
              <TextField
                value={draft.role || ''}
                onChange={(e) => setDraft({ ...draft, role: e.target.value })}
                dense
                className="w-full"
                placeholder="coding"
              />
            </FieldBlock>
            <FieldBlock label={t('status')}>
              <SelectField
                value={draft.status || 'active'}
                onChange={(e) => setDraft({ ...draft, status: e.target.value })}
                dense
                className="w-full"
              >
                <option value="active">active</option>
                <option value="disabled">disabled</option>
              </SelectField>
            </FieldBlock>
            <FieldBlock label="notify_main_policy">
              <SelectField
                value={draft.notify_main_policy || 'final_only'}
                onChange={(e) => setDraft({ ...draft, notify_main_policy: e.target.value })}
                dense
                className="w-full"
              >
                <option value="final_only">final_only</option>
                <option value="internal_only">internal_only</option>
                <option value="milestone">milestone</option>
                <option value="on_blocked">on_blocked</option>
                <option value="always">always</option>
              </SelectField>
            </FieldBlock>
            <FieldBlock className="md:col-span-2" label="system_prompt_file">
              <TextField
                value={draft.system_prompt_file || ''}
                onChange={(e) => setDraft({ ...draft, system_prompt_file: e.target.value })}
                dense
                className="w-full"
                placeholder="agents/coder/AGENT.md"
              />
            </FieldBlock>
            <FieldBlock className="md:col-span-2" label={t('memoryNamespace')}>
              <TextField
                value={draft.memory_namespace || ''}
                onChange={(e) => setDraft({ ...draft, memory_namespace: e.target.value })}
                dense
                className="w-full"
                placeholder="coder"
              />
            </FieldBlock>
            <FieldBlock className="md:col-span-2" label={t('toolAllowlist')}>
              <TextField
                value={allowlistText}
                onChange={(e) => setDraft({ ...draft, tool_allowlist: parseAllowlist(e.target.value) })}
                dense
                className="w-full"
                placeholder="read_file, list_files, memory_search"
              />
              <div className="ui-text-muted mt-1 text-[11px]">
                <span className="ui-text-subtle font-mono">skill_exec</span> is inherited automatically and does not need to be listed here.
              </div>
              {groups.length > 0 && (
                <div className="mt-2 flex flex-wrap gap-2">
                  {groups.map((g) => (
                    <Button key={g.name} type="button" onClick={() => addAllowlistToken(`group:${g.name}`)} size="xs" radius="lg" title={g.description || g.name}>
                      {`group:${g.name}`}
                    </Button>
                  ))}
                </div>
              )}
            </FieldBlock>
            <FieldBlock className="md:col-span-2" label="system_prompt_file content" meta={promptFileFound ? t('promptFileReady') : t('promptFileMissing')}>
              <TextareaField
                value={promptFileContent}
                onChange={(e) => setPromptFileContent(e.target.value)}
                dense
                className="w-full min-h-[220px]"
                placeholder={t('agentPromptContentPlaceholder')}
              />
              <div className="mt-2 flex items-center gap-2">
                <Button type="button" onClick={savePromptFile} disabled={!String(draft.system_prompt_file || '').trim()} size="xs" radius="lg">
                  {t('savePromptFile')}
                </Button>
              </div>
            </FieldBlock>
            <FieldBlock label={t('maxRetries')}>
              <TextField
                type="number"
                min={0}
                value={Number(draft.max_retries || 0)}
                onChange={(e) => setDraft({ ...draft, max_retries: Number(e.target.value) || 0 })}
                dense
                className="w-full"
              />
            </FieldBlock>
            <FieldBlock label={t('retryBackoffMs')}>
              <TextField
                type="number"
                min={0}
                value={Number(draft.retry_backoff_ms || 0)}
                onChange={(e) => setDraft({ ...draft, retry_backoff_ms: Number(e.target.value) || 0 })}
                dense
                className="w-full"
              />
            </FieldBlock>
            <FieldBlock label="Max Task Chars">
              <TextField
                type="number"
                min={0}
                value={Number(draft.max_task_chars || 0)}
                onChange={(e) => setDraft({ ...draft, max_task_chars: Number(e.target.value) || 0 })}
                dense
                className="w-full"
              />
            </FieldBlock>
            <FieldBlock className="md:col-span-2" label="Max Result Chars">
              <TextField
                type="number"
                min={0}
                value={Number(draft.max_result_chars || 0)}
                onChange={(e) => setDraft({ ...draft, max_result_chars: Number(e.target.value) || 0 })}
                dense
                className="w-full"
              />
            </FieldBlock>
          </div>

          <div className="flex items-center gap-2">
            <Button onClick={save} disabled={saving} variant="primary" size="xs">
              {selected ? t('update') : t('create')}
            </Button>
            <Button onClick={() => setStatus('active')} disabled={!draft.agent_id} variant="success" size="xs">
              {t('enable')}
            </Button>
            <Button onClick={() => setStatus('disabled')} disabled={!draft.agent_id} variant="warning" size="xs">
              {t('disable')}
            </Button>
            <Button onClick={remove} disabled={!draft.agent_id} variant="danger" size="xs">
              {t('delete')}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
};

export default SubagentProfiles;
