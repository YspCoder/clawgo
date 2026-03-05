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

  const apiPath = '/webui/api/subagents_runtime';
  const withAction = (action: string) => `${apiPath}${q}${q ? '&' : '?'}action=${encodeURIComponent(action)}`;

  const load = async () => {
    const r = await fetch(withAction('list'));
    if (!r.ok) throw new Error(await r.text());
    const j = await r.json();
    const arr = Array.isArray(j?.result?.items) ? j.result.items : [];
    setItems(arr);
    if (arr.length === 0) {
      setSelectedId('');
      return;
    }
    if (!arr.find((x: SubagentTask) => x.id === selectedId)) {
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
                  <div className="md:col-span-2"><span className="text-zinc-400">Memory NS:</span> {selected.memory_ns || '-'}</div>
                  <div><span className="text-zinc-400">Retries:</span> {selected.retry_count || 0}/{selected.max_retries || 0}</div>
                  <div><span className="text-zinc-400">Timeout:</span> {selected.timeout_sec || 0}s</div>
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
        </div>
      </div>
    </div>
  );
};

export default Subagents;
