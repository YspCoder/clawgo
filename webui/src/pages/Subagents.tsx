import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';

type SubagentTask = {
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
  timeout_sec?: number;
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

type RouterReply = {
  task_id?: string;
  thread_id?: string;
  correlation_id?: string;
  agent_id?: string;
  status?: string;
  result?: string;
};

type AgentThread = {
  thread_id?: string;
  owner?: string;
  participants?: string[];
  status?: string;
  topic?: string;
};

type AgentMessage = {
  message_id?: string;
  thread_id?: string;
  from_agent?: string;
  to_agent?: string;
  reply_to?: string;
  correlation_id?: string;
  type?: string;
  content?: string;
  requires_reply?: boolean;
  status?: string;
  created_at?: number;
};

type PendingSubagentDraft = {
  session_key?: string;
  draft?: {
    agent_id?: string;
    role?: string;
    display_name?: string;
    description?: string;
    system_prompt?: string;
    system_prompt_file?: string;
    tool_allowlist?: string[];
    routing_keywords?: string[];
  };
};

type RegistrySubagent = {
  agent_id?: string;
  enabled?: boolean;
  type?: string;
  display_name?: string;
  role?: string;
  description?: string;
  system_prompt?: string;
  system_prompt_file?: string;
  prompt_file_found?: boolean;
  memory_namespace?: string;
  tool_allowlist?: string[];
  routing_keywords?: string[];
};

const Subagents: React.FC = () => {
  const { t } = useTranslation();
  const { q } = useAppContext();
  const ui = useUI();

  const [items, setItems] = useState<SubagentTask[]>([]);
  const [selectedId, setSelectedId] = useState<string>('');
  const [spawnTask, setSpawnTask] = useState('');
  const [spawnAgentID, setSpawnAgentID] = useState('');
  const [spawnRole, setSpawnRole] = useState('');
  const [spawnLabel, setSpawnLabel] = useState('');
  const [steerMessage, setSteerMessage] = useState('');
  const [dispatchTask, setDispatchTask] = useState('');
  const [dispatchAgentID, setDispatchAgentID] = useState('');
  const [dispatchRole, setDispatchRole] = useState('');
  const [dispatchWaitTimeout, setDispatchWaitTimeout] = useState('120');
  const [dispatchReply, setDispatchReply] = useState<RouterReply | null>(null);
  const [dispatchMerged, setDispatchMerged] = useState('');
  const [threadDetail, setThreadDetail] = useState<AgentThread | null>(null);
  const [threadMessages, setThreadMessages] = useState<AgentMessage[]>([]);
  const [inboxMessages, setInboxMessages] = useState<AgentMessage[]>([]);
  const [replyMessage, setReplyMessage] = useState('');
  const [replyToMessageID, setReplyToMessageID] = useState('');
  const [configAgentID, setConfigAgentID] = useState('');
  const [configRole, setConfigRole] = useState('');
  const [configDisplayName, setConfigDisplayName] = useState('');
  const [configSystemPrompt, setConfigSystemPrompt] = useState('');
  const [configSystemPromptFile, setConfigSystemPromptFile] = useState('');
  const [configToolAllowlist, setConfigToolAllowlist] = useState('');
  const [configRoutingKeywords, setConfigRoutingKeywords] = useState('');
  const [draftDescription, setDraftDescription] = useState('');
  const [pendingDrafts, setPendingDrafts] = useState<PendingSubagentDraft[]>([]);
  const [registryItems, setRegistryItems] = useState<RegistrySubagent[]>([]);
  const [promptFileContent, setPromptFileContent] = useState('');
  const [promptFileFound, setPromptFileFound] = useState(false);

  const apiPath = '/webui/api/subagents_runtime';
  const withAction = (action: string) => `${apiPath}${q}${q ? '&' : '?'}action=${encodeURIComponent(action)}`;

  const load = async () => {
    const [tasksRes, draftsRes, registryRes] = await Promise.all([
      fetch(withAction('list')),
      fetch(withAction('pending_drafts')),
      fetch(withAction('registry')),
    ]);
    if (!tasksRes.ok) throw new Error(await tasksRes.text());
    if (!draftsRes.ok) throw new Error(await draftsRes.text());
    if (!registryRes.ok) throw new Error(await registryRes.text());
    const j = await tasksRes.json();
    const draftsJson = await draftsRes.json();
    const registryJson = await registryRes.json();
    const arr = Array.isArray(j?.result?.items) ? j.result.items : [];
    const draftItems = Array.isArray(draftsJson?.result?.items) ? draftsJson.result.items : [];
    const registryItems = Array.isArray(registryJson?.result?.items) ? registryJson.result.items : [];
    setItems(arr);
    setPendingDrafts(draftItems);
    setRegistryItems(registryItems);
    if (arr.length === 0) {
      setSelectedId('');
    } else if (!arr.find((x: SubagentTask) => x.id === selectedId)) {
      setSelectedId(arr[0].id || '');
    }
  };

  useEffect(() => {
    load().catch(() => {});
  }, [q]);

  const selected = useMemo(() => items.find((x) => x.id === selectedId) || null, [items, selectedId]);

  const callAction = async (payload: Record<string, any>) => {
    const r = await fetch(`${apiPath}${q}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!r.ok) {
      await ui.notify({ title: t('requestFailed'), message: await r.text() });
      return null;
    }
    return r.json();
  };

  const loadThreadAndInbox = async (task: SubagentTask | null) => {
    if (!task?.id) {
      setThreadDetail(null);
      setThreadMessages([]);
      setInboxMessages([]);
      return;
    }
    const [threadRes, inboxRes] = await Promise.all([
      callAction({ action: 'thread', id: task.id, limit: 50 }),
      callAction({ action: 'inbox', id: task.id, limit: 50 }),
    ]);
    setThreadDetail(threadRes?.result?.thread || null);
    setThreadMessages(Array.isArray(threadRes?.result?.messages) ? threadRes.result.messages : []);
    setInboxMessages(Array.isArray(inboxRes?.result?.messages) ? inboxRes.result.messages : []);
  };

  useEffect(() => {
    loadThreadAndInbox(selected).catch(() => {});
  }, [selectedId, q, items]);

  useEffect(() => {
    const path = configSystemPromptFile.trim();
    if (!path) {
      setPromptFileContent('');
      setPromptFileFound(false);
      return;
    }
    callAction({ action: 'prompt_file_get', path })
      .then((data) => {
        const found = data?.result?.found === true;
        setPromptFileFound(found);
        setPromptFileContent(found ? data?.result?.content || '' : '');
      })
      .catch(() => {});
  }, [configSystemPromptFile, q]);

  const spawn = async () => {
    if (!spawnTask.trim()) {
      await ui.notify({ title: t('requestFailed'), message: 'task is required' });
      return;
    }
    const data = await callAction({
      action: 'spawn',
      task: spawnTask,
      agent_id: spawnAgentID,
      role: spawnRole,
      label: spawnLabel,
    });
    if (!data) return;
    setSpawnTask('');
    await load();
  };

  const kill = async () => {
    if (!selected?.id) return;
    await callAction({ action: 'kill', id: selected.id });
    await load();
  };

  const resume = async () => {
    if (!selected?.id) return;
    await callAction({ action: 'resume', id: selected.id });
    await load();
  };

  const steer = async () => {
    if (!selected?.id || !steerMessage.trim()) return;
    await callAction({ action: 'steer', id: selected.id, message: steerMessage });
    setSteerMessage('');
    await load();
    await loadThreadAndInbox(selected);
  };

  const dispatchAndWait = async () => {
    if (!dispatchTask.trim()) {
      await ui.notify({ title: t('requestFailed'), message: 'task is required' });
      return;
    }
    const waitTimeout = Number.parseInt(dispatchWaitTimeout, 10);
    const data = await callAction({
      action: 'dispatch_and_wait',
      task: dispatchTask,
      agent_id: dispatchAgentID,
      role: dispatchRole,
      wait_timeout_sec: Number.isFinite(waitTimeout) && waitTimeout > 0 ? waitTimeout : 120,
    });
    if (!data) return;
    setDispatchReply(data?.result?.reply || null);
    setDispatchMerged(data?.result?.merged || '');
    await load();
  };

  const sendMessage = async () => {
    if (!selected?.id || !replyMessage.trim()) return;
    await callAction({ action: 'send', id: selected.id, message: replyMessage });
    setReplyMessage('');
    setReplyToMessageID('');
    await load();
    await loadThreadAndInbox(selected);
  };

  const replyToMessage = async () => {
    if (!selected?.id || !replyMessage.trim()) return;
    await callAction({ action: 'reply', id: selected.id, message_id: replyToMessageID, message: replyMessage });
    setReplyMessage('');
    setReplyToMessageID('');
    await load();
    await loadThreadAndInbox(selected);
  };

  const ackMessage = async (messageID: string) => {
    if (!selected?.id || !messageID) return;
    await callAction({ action: 'ack', id: selected.id, message_id: messageID });
    await load();
    await loadThreadAndInbox(selected);
  };

  const upsertConfigSubagent = async () => {
    if (!configAgentID.trim()) {
      await ui.notify({ title: t('requestFailed'), message: 'agent_id is required' });
      return;
    }
    const toolAllowlist = configToolAllowlist
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean);
    const routingKeywords = configRoutingKeywords
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean);
    const data = await callAction({
      action: 'upsert_config_subagent',
      agent_id: configAgentID,
      role: configRole,
      display_name: configDisplayName,
      system_prompt: configSystemPrompt,
      system_prompt_file: configSystemPromptFile,
      tool_allowlist: toolAllowlist,
      routing_keywords: routingKeywords,
    });
    if (!data) return;
    await ui.notify({ title: t('saved'), message: t('configSubagentSaved') });
    setConfigAgentID('');
    setConfigRole('');
    setConfigDisplayName('');
    setConfigSystemPrompt('');
    setConfigSystemPromptFile('');
    setConfigToolAllowlist('');
    setConfigRoutingKeywords('');
    await load();
  };

  const draftConfigSubagent = async () => {
    if (!draftDescription.trim()) {
      await ui.notify({ title: t('requestFailed'), message: 'description is required' });
      return;
    }
    const data = await callAction({
      action: 'draft_config_subagent',
      description: draftDescription,
      agent_id_hint: configAgentID,
    });
    if (!data) return;
    const draft = data?.result?.draft || {};
    setConfigAgentID(draft.agent_id || '');
    setConfigRole(draft.role || '');
    setConfigDisplayName(draft.display_name || '');
    setConfigSystemPrompt(draft.system_prompt || '');
    setConfigSystemPromptFile(draft.system_prompt_file || '');
    setConfigToolAllowlist(Array.isArray(draft.tool_allowlist) ? draft.tool_allowlist.join(', ') : '');
    setConfigRoutingKeywords(Array.isArray(draft.routing_keywords) ? draft.routing_keywords.join(', ') : '');
    await load();
  };

  const fillDraftForm = (draft: PendingSubagentDraft['draft']) => {
    if (!draft) return;
    setConfigAgentID(draft.agent_id || '');
    setConfigRole(draft.role || '');
    setConfigDisplayName(draft.display_name || '');
    setConfigSystemPrompt(draft.system_prompt || '');
    setConfigSystemPromptFile(draft.system_prompt_file || '');
    setConfigToolAllowlist(Array.isArray(draft.tool_allowlist) ? draft.tool_allowlist.join(', ') : '');
    setConfigRoutingKeywords(Array.isArray(draft.routing_keywords) ? draft.routing_keywords.join(', ') : '');
  };

  const clearPendingDraft = async (sessionKey: string) => {
    const data = await callAction({ action: 'clear_pending_draft', session_key: sessionKey });
    if (!data) return;
    await load();
  };

  const confirmPendingDraft = async (sessionKey: string) => {
    const data = await callAction({ action: 'confirm_pending_draft', session_key: sessionKey });
    if (!data) return;
    await ui.notify({ title: t('saved'), message: data?.result?.message || t('configSubagentSaved') });
    await load();
  };

  const loadRegistryItem = (item: RegistrySubagent) => {
    setConfigAgentID(item.agent_id || '');
    setConfigRole(item.role || '');
    setConfigDisplayName(item.display_name || '');
    setConfigSystemPrompt(item.system_prompt || '');
    setConfigSystemPromptFile(item.system_prompt_file || '');
    setConfigToolAllowlist(Array.isArray(item.tool_allowlist) ? item.tool_allowlist.join(', ') : '');
    setConfigRoutingKeywords(Array.isArray(item.routing_keywords) ? item.routing_keywords.join(', ') : '');
  };

  const setRegistryEnabled = async (item: RegistrySubagent, enabled: boolean) => {
    if (!item.agent_id) return;
    const data = await callAction({ action: 'set_config_subagent_enabled', agent_id: item.agent_id, enabled });
    if (!data) return;
    await load();
  };

  const deleteRegistryItem = async (item: RegistrySubagent) => {
    if (!item.agent_id) return;
    const ok = await ui.confirmDialog({
      title: t('deleteAgent'),
      message: t('deleteAgentConfirm', { id: item.agent_id }),
      danger: true,
      confirmText: t('delete'),
    });
    if (!ok) return;
    const data = await callAction({ action: 'delete_config_subagent', agent_id: item.agent_id });
    if (!data) return;
    await load();
  };

  const savePromptFile = async () => {
    if (!configSystemPromptFile.trim()) {
      await ui.notify({ title: t('requestFailed'), message: 'system_prompt_file is required' });
      return;
    }
    const data = await callAction({
      action: 'prompt_file_set',
      path: configSystemPromptFile,
      content: promptFileContent,
    });
    if (!data) return;
    setPromptFileFound(true);
    await ui.notify({ title: t('saved'), message: t('promptFileSaved') });
    await load();
  };

  const bootstrapPromptFile = async () => {
    if (!configAgentID.trim()) {
      await ui.notify({ title: t('requestFailed'), message: 'agent_id is required' });
      return;
    }
    const data = await callAction({
      action: 'prompt_file_bootstrap',
      agent_id: configAgentID,
      role: configRole,
      display_name: configDisplayName,
      path: configSystemPromptFile,
    });
    if (!data) return;
    const path = data?.result?.path || configSystemPromptFile || `agents/${configAgentID}/AGENT.md`;
    setConfigSystemPromptFile(path);
    setPromptFileFound(true);
    setPromptFileContent(data?.result?.content || '');
    await ui.notify({ title: t('saved'), message: t('promptFileBootstrapped') });
    await load();
  };

  return (
    <div className="h-full p-4 md:p-6 flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl md:text-2xl font-semibold">{t('subagentsRuntime')}</h1>
        <button onClick={() => load()} className="px-3 py-1.5 rounded-lg bg-zinc-800 hover:bg-zinc-700 text-sm">{t('refresh')}</button>
      </div>

      <div className="flex-1 min-h-0 grid grid-cols-1 xl:grid-cols-[360px_1fr] gap-4">
        <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 overflow-hidden">
          <div className="px-3 py-2 border-b border-zinc-800 text-xs text-zinc-400 uppercase tracking-wider">{t('subagentsRuntime')}</div>
          <div className="overflow-y-auto max-h-[70vh]">
            {items.map((it) => (
              <button
                key={it.id}
                onClick={() => setSelectedId(it.id)}
                className={`w-full text-left px-3 py-2 border-b border-zinc-800/50 hover:bg-zinc-800/40 ${selectedId === it.id ? 'bg-indigo-500/15' : ''}`}
              >
                <div className="text-sm text-zinc-100 truncate">{it.id}</div>
                <div className="text-xs text-zinc-400 truncate">{it.status} · {it.agent_id || '-'} · {it.role || '-'}</div>
              </button>
            ))}
            {items.length === 0 && <div className="px-3 py-4 text-sm text-zinc-500">No subagents.</div>}
          </div>
        </div>

        <div className="space-y-4 min-h-0 overflow-y-auto">
          <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
            <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('subagentDetail')}</div>
            {!selected && <div className="text-sm text-zinc-500">{t('selectTask')}</div>}
            {selected && (
              <>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-xs">
                  <div><span className="text-zinc-400">ID:</span> {selected.id}</div>
                  <div><span className="text-zinc-400">Status:</span> {selected.status}</div>
                  <div><span className="text-zinc-400">Agent ID:</span> {selected.agent_id || '-'}</div>
                  <div><span className="text-zinc-400">Role:</span> {selected.role || '-'}</div>
                  <div className="md:col-span-2"><span className="text-zinc-400">Session:</span> {selected.session_key || '-'}</div>
                  <div className="md:col-span-2"><span className="text-zinc-400">Thread:</span> {selected.thread_id || '-'}</div>
                  <div className="md:col-span-2"><span className="text-zinc-400">Correlation:</span> {selected.correlation_id || '-'}</div>
                  <div className="md:col-span-2"><span className="text-zinc-400">Memory NS:</span> {selected.memory_ns || '-'}</div>
                  <div><span className="text-zinc-400">Retries:</span> {selected.retry_count || 0}/{selected.max_retries || 0}</div>
                  <div><span className="text-zinc-400">Timeout:</span> {selected.timeout_sec || 0}s</div>
                  <div><span className="text-zinc-400">Waiting Reply:</span> {selected.waiting_for_reply ? 'yes' : 'no'}</div>
                </div>
                <div className="text-xs text-zinc-400">Task</div>
                <pre className="text-xs bg-zinc-950 border border-zinc-800 rounded p-3 whitespace-pre-wrap break-words">{selected.task || '-'}</pre>
                <div className="text-xs text-zinc-400">Result</div>
                <pre className="text-xs bg-zinc-950 border border-zinc-800 rounded p-3 whitespace-pre-wrap break-words max-h-64 overflow-auto">{selected.result || '-'}</pre>
                <div className="flex items-center gap-2">
                  <button onClick={resume} className="px-3 py-1.5 text-xs rounded bg-emerald-700/70 hover:bg-emerald-600">{t('resume')}</button>
                  <button onClick={kill} className="px-3 py-1.5 text-xs rounded bg-red-700/70 hover:bg-red-600">{t('kill')}</button>
                </div>
                <div className="flex items-center gap-2">
                  <input
                    value={steerMessage}
                    onChange={(e) => setSteerMessage(e.target.value)}
                    placeholder={t('steerMessage')}
                    className="flex-1 px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded"
                  />
                  <button onClick={steer} className="px-3 py-1.5 text-xs rounded bg-indigo-700/70 hover:bg-indigo-600">{t('send')}</button>
                </div>
              </>
            )}
          </div>

          <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
            <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('spawnSubagent')}</div>
            <textarea
              value={spawnTask}
              onChange={(e) => setSpawnTask(e.target.value)}
              placeholder="Task"
              className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded min-h-[110px]"
            />
            <div className="grid grid-cols-1 md:grid-cols-3 gap-2">
              <input value={spawnAgentID} onChange={(e) => setSpawnAgentID(e.target.value)} placeholder="agent_id" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
              <input value={spawnRole} onChange={(e) => setSpawnRole(e.target.value)} placeholder="role" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
              <input value={spawnLabel} onChange={(e) => setSpawnLabel(e.target.value)} placeholder="label" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
            </div>
            <button onClick={spawn} className="px-3 py-1.5 text-xs rounded bg-indigo-700/80 hover:bg-indigo-600">{t('spawn')}</button>
          </div>

          <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
            <div className="flex items-center justify-between gap-3">
              <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('agentRegistry')}</div>
              <button onClick={() => load()} className="px-2 py-1 text-[11px] rounded bg-zinc-800 hover:bg-zinc-700">{t('refresh')}</button>
            </div>
            <div className="border border-zinc-800 rounded overflow-hidden">
              {registryItems.map((item) => (
                <div key={item.agent_id || 'unknown'} className="px-3 py-2 border-b last:border-b-0 border-zinc-800/60 text-xs space-y-2">
                  <div className="text-zinc-100">{item.agent_id || '-'} · {item.role || '-'} · {item.enabled ? t('active') : t('paused')}</div>
                  <div className="text-zinc-400">{item.type || '-'} · {item.display_name || '-'}</div>
                  <div className="text-zinc-500 break-words">{item.system_prompt_file || '-'}</div>
                  <div className="text-zinc-500">{item.prompt_file_found ? t('promptFileReady') : t('promptFileMissing')}</div>
                  <div className="text-zinc-300 whitespace-pre-wrap break-words">{item.system_prompt || item.description || '-'}</div>
                  <div className="text-zinc-500 break-words">{(item.routing_keywords || []).join(', ') || '-'}</div>
                  <div className="flex items-center gap-2">
                    <button onClick={() => loadRegistryItem(item)} className="px-2 py-1 rounded bg-indigo-700/70 hover:bg-indigo-600 text-[11px]">{t('loadDraft')}</button>
                    <button onClick={() => setRegistryEnabled(item, !item.enabled)} className="px-2 py-1 rounded bg-zinc-800 hover:bg-zinc-700 text-[11px]">
                      {item.enabled ? t('disableAgent') : t('enableAgent')}
                    </button>
                    {item.agent_id !== 'main' && (
                      <button onClick={() => deleteRegistryItem(item)} className="px-2 py-1 rounded bg-red-700/70 hover:bg-red-600 text-[11px]">{t('delete')}</button>
                    )}
                  </div>
                </div>
              ))}
              {registryItems.length === 0 && <div className="px-3 py-4 text-sm text-zinc-500">{t('noRegistryAgents')}</div>}
            </div>
          </div>

          <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
            <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('configSubagentDraft')}</div>
            <textarea
              value={draftDescription}
              onChange={(e) => setDraftDescription(e.target.value)}
              placeholder={t('subagentDraftDescription')}
              className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded min-h-[90px]"
            />
            <button onClick={draftConfigSubagent} className="px-3 py-1.5 text-xs rounded bg-zinc-700 hover:bg-zinc-600">{t('generateDraft')}</button>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-2">
              <input value={configAgentID} onChange={(e) => setConfigAgentID(e.target.value)} placeholder="agent_id" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
              <input value={configRole} onChange={(e) => setConfigRole(e.target.value)} placeholder="role" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
              <input value={configDisplayName} onChange={(e) => setConfigDisplayName(e.target.value)} placeholder="display_name" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
            </div>
            <textarea
              value={configSystemPrompt}
              onChange={(e) => setConfigSystemPrompt(e.target.value)}
              placeholder="system_prompt"
              className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded min-h-[96px]"
            />
            <input value={configSystemPromptFile} onChange={(e) => setConfigSystemPromptFile(e.target.value)} placeholder="system_prompt_file (relative AGENT.md path)" className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
            <input value={configToolAllowlist} onChange={(e) => setConfigToolAllowlist(e.target.value)} placeholder="tool_allowlist (comma separated)" className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
            <input value={configRoutingKeywords} onChange={(e) => setConfigRoutingKeywords(e.target.value)} placeholder="routing_keywords (comma separated)" className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
            <button onClick={upsertConfigSubagent} className="px-3 py-1.5 text-xs rounded bg-amber-700/80 hover:bg-amber-600">{t('saveToConfig')}</button>
          </div>

          <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
            <div className="flex items-center justify-between gap-3">
              <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('promptFileEditor')}</div>
              <div className="text-[11px] text-zinc-500">{promptFileFound ? t('promptFileReady') : t('promptFileMissing')}</div>
            </div>
            <input
              value={configSystemPromptFile}
              onChange={(e) => setConfigSystemPromptFile(e.target.value)}
              placeholder="agents/<agent_id>/AGENT.md"
              className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded"
            />
            <textarea
              value={promptFileContent}
              onChange={(e) => setPromptFileContent(e.target.value)}
              placeholder={t('promptFileEditorPlaceholder')}
              className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded min-h-[220px]"
            />
            <div className="flex items-center gap-2">
              <button onClick={bootstrapPromptFile} className="px-3 py-1.5 text-xs rounded bg-zinc-700 hover:bg-zinc-600">{t('bootstrapPromptFile')}</button>
              <button onClick={savePromptFile} className="px-3 py-1.5 text-xs rounded bg-emerald-700/80 hover:bg-emerald-600">{t('savePromptFile')}</button>
            </div>
          </div>

          <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
            <div className="flex items-center justify-between gap-3">
              <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('pendingSubagentDrafts')}</div>
              <button onClick={() => load()} className="px-2 py-1 text-[11px] rounded bg-zinc-800 hover:bg-zinc-700">{t('refresh')}</button>
            </div>
            <div className="border border-zinc-800 rounded overflow-hidden">
              {pendingDrafts.map((item) => (
                <div key={item.session_key || 'unknown'} className="px-3 py-2 border-b last:border-b-0 border-zinc-800/60 text-xs space-y-2">
                  <div className="text-zinc-100">{item.draft?.agent_id || '-'} · {item.draft?.role || '-'}</div>
                  <div className="text-zinc-400">session: {item.session_key || '-'}</div>
                  <div className="text-zinc-300 whitespace-pre-wrap break-words">{item.draft?.system_prompt || item.draft?.description || '-'}</div>
                  <div className="flex items-center gap-2">
                    <button onClick={() => fillDraftForm(item.draft)} className="px-2 py-1 rounded bg-indigo-700/70 hover:bg-indigo-600 text-[11px]">{t('loadDraft')}</button>
                    <button onClick={() => confirmPendingDraft(item.session_key || 'main')} className="px-2 py-1 rounded bg-emerald-700/70 hover:bg-emerald-600 text-[11px]">{t('confirmDraft')}</button>
                    <button onClick={() => clearPendingDraft(item.session_key || 'main')} className="px-2 py-1 rounded bg-zinc-800 hover:bg-zinc-700 text-[11px]">{t('discardDraft')}</button>
                  </div>
                </div>
              ))}
              {pendingDrafts.length === 0 && <div className="px-3 py-4 text-sm text-zinc-500">{t('noPendingSubagentDrafts')}</div>}
            </div>
          </div>

          <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
            <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('dispatchAndWait')}</div>
            <textarea
              value={dispatchTask}
              onChange={(e) => setDispatchTask(e.target.value)}
              placeholder="Task"
              className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded min-h-[110px]"
            />
            <div className="grid grid-cols-1 md:grid-cols-3 gap-2">
              <input value={dispatchAgentID} onChange={(e) => setDispatchAgentID(e.target.value)} placeholder="agent_id" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
              <input value={dispatchRole} onChange={(e) => setDispatchRole(e.target.value)} placeholder="role" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
              <input value={dispatchWaitTimeout} onChange={(e) => setDispatchWaitTimeout(e.target.value)} placeholder="wait_timeout_sec" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
            </div>
            <button onClick={dispatchAndWait} className="px-3 py-1.5 text-xs rounded bg-emerald-700/80 hover:bg-emerald-600">{t('dispatchAndWait')}</button>
            {dispatchReply && (
              <>
                <div className="text-xs text-zinc-400">{t('dispatchReply')}</div>
                <pre className="text-xs bg-zinc-950 border border-zinc-800 rounded p-3 whitespace-pre-wrap break-words">{JSON.stringify(dispatchReply, null, 2)}</pre>
              </>
            )}
            {dispatchMerged && (
              <>
                <div className="text-xs text-zinc-400">{t('mergedResult')}</div>
                <pre className="text-xs bg-zinc-950 border border-zinc-800 rounded p-3 whitespace-pre-wrap break-words max-h-64 overflow-auto">{dispatchMerged}</pre>
              </>
            )}
          </div>

          <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
            <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('threadTrace')}</div>
            {!selected && <div className="text-sm text-zinc-500">{t('selectTask')}</div>}
            {selected && (
              <>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-xs">
                  <div><span className="text-zinc-400">Thread:</span> {threadDetail?.thread_id || selected.thread_id || '-'}</div>
                  <div><span className="text-zinc-400">Owner:</span> {threadDetail?.owner || '-'}</div>
                  <div><span className="text-zinc-400">Status:</span> {threadDetail?.status || '-'}</div>
                  <div><span className="text-zinc-400">Participants:</span> {(threadDetail?.participants || []).join(', ') || '-'}</div>
                </div>
                <div className="text-xs text-zinc-400">{t('threadMessages')}</div>
                <div className="border border-zinc-800 rounded overflow-hidden">
                  {threadMessages.map((msg) => (
                    <div key={`${msg.message_id}-${msg.status}-${msg.created_at}`} className="px-3 py-2 border-b last:border-b-0 border-zinc-800/60 text-xs">
                      <div className="text-zinc-100">{msg.message_id} · {msg.type} · {msg.status}</div>
                      <div className="text-zinc-400">{msg.from_agent} → {msg.to_agent} · reply_to: {msg.reply_to || '-'}</div>
                      <div className="text-zinc-300 mt-1 whitespace-pre-wrap break-words">{msg.content || '-'}</div>
                    </div>
                  ))}
                  {threadMessages.length === 0 && <div className="px-3 py-4 text-sm text-zinc-500">No thread messages.</div>}
                </div>
              </>
            )}
          </div>

          <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
            <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('inbox')}</div>
            {!selected && <div className="text-sm text-zinc-500">{t('selectTask')}</div>}
            {selected && (
              <>
                <div className="border border-zinc-800 rounded overflow-hidden">
                  {inboxMessages.map((msg) => (
                    <div key={`${msg.message_id}-${msg.status}`} className="px-3 py-2 border-b last:border-b-0 border-zinc-800/60 text-xs">
                      <div className="flex items-center justify-between gap-3">
                        <div className="text-zinc-100">{msg.message_id} · {msg.type} · {msg.status}</div>
                        {msg.message_id && (
                          <button onClick={() => ackMessage(msg.message_id || '')} className="px-2 py-1 rounded bg-zinc-800 hover:bg-zinc-700 text-[11px]">{t('ack')}</button>
                        )}
                      </div>
                      <div className="text-zinc-400">{msg.from_agent} → {msg.to_agent}</div>
                      <div className="text-zinc-300 mt-1 whitespace-pre-wrap break-words">{msg.content || '-'}</div>
                    </div>
                  ))}
                  {inboxMessages.length === 0 && <div className="px-3 py-4 text-sm text-zinc-500">No queued inbox messages.</div>}
                </div>
                <div className="grid grid-cols-1 md:grid-cols-[1fr_220px] gap-2">
                  <input value={replyMessage} onChange={(e) => setReplyMessage(e.target.value)} placeholder={t('message')} className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
                  <input value={replyToMessageID} onChange={(e) => setReplyToMessageID(e.target.value)} placeholder="reply_to message_id" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
                </div>
                <div className="flex items-center gap-2">
                  <button onClick={sendMessage} className="px-3 py-1.5 text-xs rounded bg-indigo-700/70 hover:bg-indigo-600">{t('send')}</button>
                  <button onClick={replyToMessage} className="px-3 py-1.5 text-xs rounded bg-emerald-700/70 hover:bg-emerald-600">{t('reply')}</button>
                </div>
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

export default Subagents;
