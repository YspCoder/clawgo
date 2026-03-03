import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';

type TaskItem = {
  id?: string;
  content?: string;
  priority?: string;
  status?: string;
  source?: string;
  due_at?: string;
  updated_at?: string;
};

const Tasks: React.FC = () => {
  const { t } = useTranslation();
  const { q } = useAppContext();
  const [items, setItems] = useState<TaskItem[]>([]);
  const [selected, setSelected] = useState<TaskItem | null>(null);
  const [draft, setDraft] = useState<TaskItem>({ id: '', content: '' });

  const load = async () => {
    const r = await fetch(`/webui/api/tasks${q}`);
    if (!r.ok) return;
    const j = await r.json();
    const arr = Array.isArray(j.items) ? j.items : [];
    setItems(arr);
    if (arr.length > 0) {
      setSelected(arr[0]);
      setDraft({ id: arr[0].id || '', content: arr[0].content || '' });
    }
  };

  useEffect(() => { load(); }, [q]);

  const save = async (action: 'create' | 'update' | 'delete') => {
    const payload: any = { action };
    if (action === 'create') payload.item = draft;
    if (action === 'update') { payload.id = draft.id; payload.item = { id: draft.id, content: draft.content }; }
    if (action === 'delete') payload.id = draft.id;
    const r = await fetch(`/webui/api/tasks${q}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!r.ok) return;
    await load();
  };

  return (
    <div className="h-full p-4 md:p-6 flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl md:text-2xl font-semibold">{t('tasks')}</h1>
        <button onClick={() => setDraft({ id: '', content: '' })} className="px-3 py-1.5 rounded-lg bg-emerald-700/80 hover:bg-emerald-600 text-sm">{t('newTask')}</button>
      </div>

      <div className="flex-1 min-h-0 grid grid-cols-1 lg:grid-cols-[340px_1fr] gap-4">
        <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 overflow-hidden">
          <div className="px-3 py-2 border-b border-zinc-800 text-xs text-zinc-400 uppercase tracking-wider">{t('taskList')}</div>
          <div className="overflow-y-auto max-h-[70vh]">
            {items.map((it, idx) => (
              <button key={it.id || idx} onClick={() => { setSelected(it); setDraft({ id: it.id || '', content: it.content || '' }); }} className={`w-full text-left px-3 py-2 border-b border-zinc-800/50 hover:bg-zinc-800/40 ${selected?.id === it.id ? 'bg-indigo-500/15' : ''}`}>
                <div className="text-sm text-zinc-100 truncate">{it.id || '-'}</div>
                <div className="text-xs text-zinc-400 truncate">{it.status} · {it.priority} · {it.source}</div>
              </button>
            ))}
          </div>
        </div>

        <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
          <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('taskCrud')}</div>
          <input value={draft.id || ''} onChange={(e)=>setDraft({ ...draft, id: e.target.value })} placeholder={t('id')} className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
          <textarea value={draft.content || ''} onChange={(e)=>setDraft({ ...draft, content: e.target.value })} placeholder={t('content')} className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded min-h-[120px]" />
          
          <div className="flex items-center gap-2">
            <button onClick={()=>save('create')} className="px-2 py-1 text-xs rounded bg-emerald-700/70 hover:bg-emerald-600">{t('createTask')}</button>
            <button onClick={()=>save('update')} className="px-2 py-1 text-xs rounded bg-indigo-700/70 hover:bg-indigo-600">{t('updateTask')}</button>
            <button onClick={()=>save('delete')} className="px-2 py-1 text-xs rounded bg-red-700/70 hover:bg-red-600">{t('deleteTask')}</button>
          </div>
        </div>
      </div>
    </div>
  );
};

export default Tasks;
