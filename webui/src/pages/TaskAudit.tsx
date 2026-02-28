import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';

type TaskAuditItem = {
  task_id?: string;
  time?: string;
  channel?: string;
  session?: string;
  chat_id?: string;
  sender_id?: string;
  status?: string;
  duration_ms?: number;
  retry_count?: number;
  error?: string;
  input_preview?: string;
  logs?: string[];
  media_items?: Array<{ source?: string; type?: string; ref?: string; path?: string; channel?: string }>;
  [key: string]: any;
};

const TaskAudit: React.FC = () => {
  const { t } = useTranslation();
  const { q } = useAppContext();
  const [items, setItems] = useState<TaskAuditItem[]>([]);
  const [selected, setSelected] = useState<TaskAuditItem | null>(null);
  const [loading, setLoading] = useState(false);

  const fetchData = async () => {
    setLoading(true);
    try {
      const url = `/webui/api/task_queue${q ? `${q}&limit=300` : '?limit=300'}`;
      const r = await fetch(url);
      if (!r.ok) throw new Error(await r.text());
      const j = await r.json();
      const arr = Array.isArray(j.items) ? j.items : [];
      const sorted = arr.sort((a: any, b: any) => String(b.time || '').localeCompare(String(a.time || '')));
      setItems(sorted);
      if (sorted.length > 0) setSelected(sorted[0]);
    } catch (e) {
      console.error(e);
      setItems([]);
      setSelected(null);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchData(); }, [q]);

  const selectedPretty = useMemo(() => selected ? JSON.stringify(selected, null, 2) : '', [selected]);

  return (
    <div className="h-full p-4 md:p-6 flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl md:text-2xl font-semibold">{t('taskAudit')}</h1>
        <button onClick={fetchData} className="px-3 py-1.5 rounded-lg bg-zinc-800 hover:bg-zinc-700 text-sm">{loading ? t('loading') : t('refresh')}</button>
      </div>

      <div className="flex-1 min-h-0 grid grid-cols-1 lg:grid-cols-[360px_1fr] gap-4">
        <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 overflow-hidden flex flex-col min-h-0">
          <div className="px-3 py-2 border-b border-zinc-800 text-xs text-zinc-400 uppercase tracking-wider">{t('taskQueue')}</div>
          <div className="overflow-y-auto min-h-0">
            {items.length === 0 ? (
              <div className="p-4 text-sm text-zinc-500">{t('noTaskAudit')}</div>
            ) : items.map((it, idx) => {
              const active = selected?.task_id === it.task_id && selected?.time === it.time;
              return (
                <button
                  key={`${it.task_id || idx}-${it.time || idx}`}
                  onClick={() => setSelected(it)}
                  className={`w-full text-left px-3 py-2 border-b border-zinc-800/60 hover:bg-zinc-800/40 ${active ? 'bg-indigo-500/15' : ''}`}
                >
                  <div className="text-sm font-medium text-zinc-100 truncate">{it.task_id || `task-${idx + 1}`}</div>
                  <div className="text-xs text-zinc-400 truncate">{it.channel} · {it.status} · {it.duration_ms || 0}ms · retry:{it.retry_count || 0}</div>
                  <div className="text-[11px] text-zinc-500 truncate">{it.time}</div>
                </button>
              );
            })}
          </div>
        </div>

        <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 overflow-hidden flex flex-col min-h-0">
          <div className="px-3 py-2 border-b border-zinc-800 text-xs text-zinc-400 uppercase tracking-wider">{t('taskDetail')}</div>
          <div className="p-4 overflow-y-auto min-h-0 space-y-3 text-sm">
            {!selected ? (
              <div className="text-zinc-500">{t('selectTask')}</div>
            ) : (
              <>
                <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
                  <div><div className="text-zinc-500 text-xs">Task ID</div><div className="font-mono break-all">{selected.task_id}</div></div>
                  <div><div className="text-zinc-500 text-xs">Status</div><div>{selected.status}</div></div>
                  <div><div className="text-zinc-500 text-xs">Duration</div><div>{selected.duration_ms || 0}ms</div></div>
                  <div><div className="text-zinc-500 text-xs">Channel</div><div>{selected.channel}</div></div>
                  <div><div className="text-zinc-500 text-xs">Session</div><div className="font-mono break-all">{selected.session}</div></div>
                  <div><div className="text-zinc-500 text-xs">Time</div><div>{selected.time}</div></div>
                </div>

                <div>
                  <div className="text-zinc-500 text-xs mb-1">Input Preview</div>
                  <div className="p-2 rounded bg-zinc-950/60 border border-zinc-800 whitespace-pre-wrap">{selected.input_preview || '-'}</div>
                </div>

                <div>
                  <div className="text-zinc-500 text-xs mb-1">{t('error')}</div>
                  <div className="p-2 rounded bg-zinc-950/60 border border-zinc-800 whitespace-pre-wrap text-red-300">{selected.error || '-'}</div>
                </div>

                <div>
                  <div className="text-zinc-500 text-xs mb-1">{t('taskLogs')}</div>
                  <div className="p-2 rounded bg-zinc-950/60 border border-zinc-800 whitespace-pre-wrap text-zinc-200">{Array.isArray(selected.logs) && selected.logs.length ? selected.logs.join('\n') : '-'}</div>
                </div>

                <div>
                  <div className="text-zinc-500 text-xs mb-1">{t('mediaSources')}</div>
                  <div className="p-2 rounded bg-zinc-950/60 border border-zinc-800 text-xs">
                    {Array.isArray(selected.media_items) && selected.media_items.length > 0 ? (
                      <div className="space-y-1">
                        {selected.media_items.map((m, i) => (
                          <div key={i} className="font-mono break-all text-zinc-200">
                            [{m.channel || '-'}] {m.source || '-'} / {m.type || '-'} · {m.path || m.ref || '-'}
                          </div>
                        ))}
                      </div>
                    ) : '-'}
                  </div>
                </div>

                <div>
                  <div className="text-zinc-500 text-xs mb-1">{t('rawJson')}</div>
                  <pre className="p-2 rounded bg-zinc-950/60 border border-zinc-800 text-xs overflow-auto">{selectedPretty}</pre>
                </div>
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

export default TaskAudit;
