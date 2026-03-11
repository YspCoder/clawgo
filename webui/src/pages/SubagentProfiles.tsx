import React, { useEffect, useMemo, useState } from 'react';
import { Plus, RefreshCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
import { FixedButton } from '../components/Button';
import PageHeader from '../components/PageHeader';
import ProfileEditorPanel from '../components/subagentProfiles/ProfileEditorPanel';
import ProfileListPanel from '../components/subagentProfiles/ProfileListPanel';
import { emptyDraft, toProfileDraft, type SubagentProfile, type ToolAllowlistGroup } from '../components/subagentProfiles/profileDraft';

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
    setDraft(toProfileDraft(next));
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
    setDraft(toProfileDraft(p));
  };

  const onNew = () => {
    setSelectedId('');
    setDraft(emptyDraft);
  };

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
      <PageHeader
        title={t('subagentProfiles')}
        actions={(
          <div className="flex items-center gap-2">
            <FixedButton onClick={() => load()} label={t('refresh')}>
              <RefreshCw className="w-4 h-4" />
            </FixedButton>
            <FixedButton onClick={onNew} variant="success" label={t('newProfile')}>
              <Plus className="w-4 h-4" />
            </FixedButton>
          </div>
        )}
      />

      <div className="flex-1 min-h-0 grid grid-cols-1 lg:grid-cols-[360px_1fr] gap-4">
        <ProfileListPanel
          emptyLabel="No subagent profiles."
          items={items}
          onSelect={onSelect}
          selectedId={selectedId}
          title={t('subagentProfiles')}
        />

        <ProfileEditorPanel
          draft={draft}
          groups={groups}
          idLabel={t('id')}
          isExisting={!!selected}
          memoryNamespaceLabel={t('memoryNamespace')}
          nameLabel={t('name')}
          onAddAllowlistToken={addAllowlistToken}
          onChange={setDraft}
          onDelete={remove}
          onDisable={() => setStatus('disabled')}
          onEnable={() => setStatus('active')}
          onPromptContentChange={setPromptFileContent}
          onSave={save}
          onSavePromptFile={savePromptFile}
          promptContent={promptFileContent}
          promptMeta={promptFileFound ? t('promptFileReady') : t('promptFileMissing')}
          promptPlaceholder={t('agentPromptContentPlaceholder')}
          roleLabel="Role"
          saving={saving}
          statusLabel={t('status')}
          toolAllowlistHint={<><span className="ui-text-subtle font-mono">skill_exec</span> is inherited automatically and does not need to be listed here.</>}
          toolAllowlistLabel={t('toolAllowlist')}
          maxRetriesLabel={t('maxRetries')}
          retryBackoffLabel={t('retryBackoffMs')}
          maxTaskCharsLabel="Max Task Chars"
          maxResultCharsLabel="Max Result Chars"
        />
      </div>
    </div>
  );
};

export default SubagentProfiles;
