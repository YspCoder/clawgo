import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';

type PipelineTask = {
  id: string;
  role?: string;
  goal?: string;
  status?: string;
  depends_on?: string[];
  result?: string;
  error?: string;
};

type Pipeline = {
  id: string;
  label?: string;
  objective?: string;
  status?: string;
  tasks?: Record<string, PipelineTask>;
};

const Pipelines: React.FC = () => {
  const { t } = useTranslation();
  const { q } = useAppContext();
  const ui = useUI();

  const [items, setItems] = useState<Pipeline[]>([]);
  const [selectedID, setSelectedID] = useState('');
  const [detail, setDetail] = useState<Pipeline | null>(null);
  const [maxDispatch, setMaxDispatch] = useState(3);
  const [createLabel, setCreateLabel] = useState('');
  const [createObjective, setCreateObjective] = useState('');
  const [tasksJSON, setTasksJSON] = useState('[\n  {"id":"coding","role":"coding","goal":"Implement feature"},\n  {"id":"docs","role":"docs","goal":"Write docs","depends_on":["coding"]}\n]');

  const apiPath = '/webui/api/pipelines';
  const withAction = (action: string) => `${apiPath}${q}${q ? '&' : '?'}action=${encodeURIComponent(action)}`;

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

  const loadList = async () => {
    const r = await fetch(withAction('list'));
    if (!r.ok) throw new Error(await r.text());
    const j = await r.json();
    const arr = Array.isArray(j?.result?.items) ? j.result.items : [];
    setItems(arr);
    if (arr.length === 0) {
      setSelectedID('');
      setDetail(null);
      return;
    }
    const id = selectedID && arr.find((x: Pipeline) => x.id === selectedID) ? selectedID : arr[0].id;
    setSelectedID(id);
  };

  const loadDetail = async (pipelineID: string) => {
    if (!pipelineID) {
      setDetail(null);
      return;
    }
    const u = `${withAction('get')}&pipeline_id=${encodeURIComponent(pipelineID)}`;
    const r = await fetch(u);
    if (!r.ok) throw new Error(await r.text());
    const j = await r.json();
    setDetail(j?.result?.pipeline || null);
  };

  useEffect(() => {
    loadList().catch(() => {});
  }, [q]);

  useEffect(() => {
    if (!selectedID) {
      setDetail(null);
      return;
    }
    loadDetail(selectedID).catch(() => {});
  }, [selectedID, q]);

  const sortedTasks = useMemo(() => {
    const entries = Object.values(detail?.tasks || {});
    entries.sort((a, b) => (a.id || '').localeCompare(b.id || ''));
    return entries;
  }, [detail]);

  const dispatch = async () => {
    if (!detail?.id) return;
    const data = await callAction({ action: 'dispatch', pipeline_id: detail.id, max_dispatch: maxDispatch });
    if (!data) return;
    await loadList();
    await loadDetail(detail.id);
  };

  const createPipeline = async () => {
    if (!createObjective.trim()) {
      await ui.notify({ title: t('requestFailed'), message: 'objective is required' });
      return;
    }
    let tasks: any[] = [];
    try {
      const parsed = JSON.parse(tasksJSON);
      tasks = Array.isArray(parsed) ? parsed : [];
    } catch {
      await ui.notify({ title: t('requestFailed'), message: 'tasks JSON parse failed' });
      return;
    }
    const data = await callAction({
      action: 'create',
      label: createLabel,
      objective: createObjective,
      tasks,
    });
    if (!data) return;
    setCreateLabel('');
    setCreateObjective('');
    await loadList();
  };

  return (
    <div className="h-full p-4 md:p-6 flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl md:text-2xl font-semibold">{t('pipelines')}</h1>
        <button onClick={() => loadList()} className="px-3 py-1.5 rounded-lg bg-zinc-800 hover:bg-zinc-700 text-sm">{t('refresh')}</button>
      </div>

      <div className="flex-1 min-h-0 grid grid-cols-1 xl:grid-cols-[340px_1fr] gap-4">
        <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 overflow-hidden">
          <div className="px-3 py-2 border-b border-zinc-800 text-xs text-zinc-400 uppercase tracking-wider">{t('pipelines')}</div>
          <div className="overflow-y-auto max-h-[70vh]">
            {items.map((it) => (
              <button
                key={it.id}
                onClick={() => setSelectedID(it.id)}
                className={`w-full text-left px-3 py-2 border-b border-zinc-800/50 hover:bg-zinc-800/40 ${selectedID === it.id ? 'bg-indigo-500/15' : ''}`}
              >
                <div className="text-sm text-zinc-100 truncate">{it.id}</div>
                <div className="text-xs text-zinc-400 truncate">{it.status} · {it.label || '-'}</div>
              </button>
            ))}
            {items.length === 0 && <div className="px-3 py-4 text-sm text-zinc-500">No pipelines.</div>}
          </div>
        </div>

        <div className="space-y-4 min-h-0 overflow-y-auto">
          <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
            <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('pipelineDetail')}</div>
            {!detail && <div className="text-sm text-zinc-500">{t('selectTask')}</div>}
            {detail && (
              <>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-xs">
                  <div><span className="text-zinc-400">ID:</span> {detail.id}</div>
                  <div><span className="text-zinc-400">Status:</span> {detail.status}</div>
                  <div className="md:col-span-2"><span className="text-zinc-400">Label:</span> {detail.label || '-'}</div>
                </div>
                <div className="text-xs text-zinc-400">Objective</div>
                <pre className="text-xs bg-zinc-950 border border-zinc-800 rounded p-3 whitespace-pre-wrap break-words">{detail.objective || '-'}</pre>

                <div className="flex items-center gap-2">
                  <input
                    type="number"
                    value={maxDispatch}
                    min={1}
                    onChange={(e) => setMaxDispatch(Math.max(1, Number(e.target.value) || 1))}
                    className="w-24 px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded"
                  />
                  <button onClick={dispatch} className="px-3 py-1.5 text-xs rounded bg-indigo-700/80 hover:bg-indigo-600">{t('dispatch')}</button>
                </div>

                <div className="text-xs text-zinc-400">Tasks</div>
                <div className="border border-zinc-800 rounded overflow-hidden">
                  {sortedTasks.map((task) => (
                    <div key={task.id} className="px-3 py-2 border-b last:border-b-0 border-zinc-800/60 text-xs">
                      <div className="text-zinc-100">{task.id} · {task.status}</div>
                      <div className="text-zinc-400">{task.role || '-'} · deps: {(task.depends_on || []).join(', ') || '-'}</div>
                      <div className="text-zinc-300 mt-1 break-words">{task.goal || '-'}</div>
                      {task.error && <div className="text-red-400 mt-1 break-words">{task.error}</div>}
                    </div>
                  ))}
                </div>
              </>
            )}
          </div>

          <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
            <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('createPipeline')}</div>
            <input value={createLabel} onChange={(e) => setCreateLabel(e.target.value)} placeholder="label" className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
            <input value={createObjective} onChange={(e) => setCreateObjective(e.target.value)} placeholder="objective" className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
            <textarea value={tasksJSON} onChange={(e) => setTasksJSON(e.target.value)} className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded min-h-[140px]" />
            <button onClick={createPipeline} className="px-3 py-1.5 text-xs rounded bg-emerald-700/80 hover:bg-emerald-600">{t('create')}</button>
          </div>
        </div>
      </div>
    </div>
  );
};

export default Pipelines;
