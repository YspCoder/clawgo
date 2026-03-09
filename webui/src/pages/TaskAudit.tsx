import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Check } from 'lucide-react';
import { useAppContext } from '../context/AppContext';
import { formatLocalDateTime } from '../utils/time';

type TaskAuditItem = {
  task_id?: string;
  time?: string;
  channel?: string;
  session?: string;
  chat_id?: string;
  sender_id?: string;
  status?: string;
  source?: string;
  idle_run?: boolean;
  block_reason?: string;
  last_pause_reason?: string;
  last_pause_at?: string;
  duration_ms?: number;
  retry_count?: number;
  attempts?: number;
  error?: string;
  provider?: string;
  model?: string;
  input_preview?: string;
  logs?: string[];
  media_items?: Array<{ source?: string; type?: string; ref?: string; path?: string; channel?: string }>;
  [key: string]: any;
};

type NodeDispatchItem = {
  time?: string;
  node?: string;
  action?: string;
  ok?: boolean;
  used_transport?: string;
  fallback_from?: string;
  duration_ms?: number;
  error?: string;
  artifact_count?: number;
  artifact_kinds?: string[];
  artifacts?: any[];
  [key: string]: any;
};

function dataUrlForArtifact(artifact: any) {
  const mime = String(artifact?.mime_type || '').trim() || 'application/octet-stream';
  const content = String(artifact?.content_base64 || '').trim();
  if (!content) return '';
  return `data:${mime};base64,${content}`;
}

function formatBytes(value: unknown) {
  const size = Number(value || 0);
  if (!Number.isFinite(size) || size <= 0) return '-';
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

const TaskAudit: React.FC = () => {
  const { t } = useTranslation();
  const { q } = useAppContext();
  const [items, setItems] = useState<TaskAuditItem[]>([]);
  const [selected, setSelected] = useState<TaskAuditItem | null>(null);
  const [nodeItems, setNodeItems] = useState<NodeDispatchItem[]>([]);
  const [selectedNode, setSelectedNode] = useState<NodeDispatchItem | null>(null);
  const [loading, setLoading] = useState(false);
  const [sourceFilter, setSourceFilter] = useState('all');
  const [statusFilter, setStatusFilter] = useState('all');

  const fetchData = async () => {
    setLoading(true);
    try {
      const taskURL = `/webui/api/task_queue${q ? `${q}&limit=300` : '?limit=300'}`;
      const nodeURL = `/webui/api/node_dispatches${q ? `${q}&limit=150` : '?limit=150'}`;
      const [taskResp, nodeResp] = await Promise.all([fetch(taskURL), fetch(nodeURL)]);
      if (!taskResp.ok) throw new Error(await taskResp.text());
      if (!nodeResp.ok) throw new Error(await nodeResp.text());
      const taskJSON = await taskResp.json();
      const nodeJSON = await nodeResp.json();
      const arr = Array.isArray(taskJSON.items) ? taskJSON.items : [];
      const sorted = arr.sort((a: any, b: any) => String(b.time || '').localeCompare(String(a.time || '')));
      setItems(sorted);
      if (sorted.length > 0) setSelected(sorted[0]);
      const nodeArr = Array.isArray(nodeJSON.items) ? nodeJSON.items : [];
      setNodeItems(nodeArr);
      if (nodeArr.length > 0) setSelectedNode(nodeArr[0]);
    } catch (e) {
      console.error(e);
      setItems([]);
      setSelected(null);
      setNodeItems([]);
      setSelectedNode(null);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchData(); }, [q]);
  const filteredItems = useMemo(() => items.filter((it) => {
    if (sourceFilter !== 'all' && String(it.source || '-') !== sourceFilter) return false;
    if (statusFilter !== 'all' && String(it.status || '-') !== statusFilter) return false;
    return true;
  }), [items, sourceFilter, statusFilter]);

  const selectedPretty = useMemo(() => selected ? JSON.stringify(selected, null, 2) : '', [selected]);

  return (
    <div className="h-full p-4 md:p-6 xl:p-8 flex flex-col gap-4">
      <div className="flex items-center justify-between flex-wrap gap-3">
        <h1 className="text-xl md:text-2xl font-semibold">{t('taskAudit')}</h1>
        <div className="flex items-center gap-2">
          <select value={sourceFilter} onChange={(e)=>setSourceFilter(e.target.value)} className="bg-zinc-900/70 border border-zinc-700 rounded-xl px-2 py-1.5 text-xs">
            <option value="all">{t('allSources')}</option>
            <option value="direct">{t('sourceDirect')}</option>
            <option value="memory_todo">{t('sourceMemoryTodo')}</option>
            <option value="task_watchdog">task_watchdog</option>
            <option value="-">-</option>
          </select>
          <select value={statusFilter} onChange={(e)=>setStatusFilter(e.target.value)} className="bg-zinc-900/70 border border-zinc-700 rounded-xl px-2 py-1.5 text-xs">
            <option value="all">{t('allStatus')}</option>
            <option value="running">{t('statusRunning')}</option>
            <option value="waiting">{t('statusWaiting')}</option>
            <option value="blocked">{t('statusBlocked')}</option>
            <option value="success">{t('statusSuccess')}</option>
            <option value="error">{t('statusError')}</option>
            <option value="suppressed">{t('statusSuppressed')}</option>
          </select>
          <button onClick={fetchData} className="brand-button px-3 py-1.5 rounded-xl text-sm text-white">{loading ? t('loading') : t('refresh')}</button>
        </div>
      </div>

      <div className="flex-1 min-h-0 grid grid-cols-1 xl:grid-cols-[320px_1fr_380px] gap-4">
        <div className="brand-card rounded-[28px] border border-zinc-800 overflow-hidden flex flex-col min-h-0">
          <div className="px-3 py-2 border-b border-zinc-800 text-xs text-zinc-400 uppercase tracking-wider">{t('taskQueue')}</div>
          <div className="overflow-y-auto min-h-0">
            {filteredItems.length === 0 ? (
              <div className="p-4 text-sm text-zinc-500">{t('noTaskAudit')}</div>
            ) : filteredItems.map((it, idx) => {
              const active = selected?.task_id === it.task_id && selected?.time === it.time;
              return (
                <button
                  key={`${it.task_id || idx}-${it.time || idx}`}
                  onClick={() => setSelected(it)}
                  className="w-full text-left px-3 py-2 border-b border-zinc-800/60 transition-colors"
                >
                  <div className="flex items-center gap-3">
                    <div className="min-w-0 flex-1">
                      <div className="text-sm font-medium text-zinc-100 truncate">{it.task_id || `task-${idx + 1}`}</div>
                      <div className="text-xs text-zinc-400 truncate">{it.channel || '-'} · {it.status} · attempts:{it.attempts || 1} · {it.duration_ms || 0}ms · retry:{it.retry_count || 0} · {it.source || '-'} · {it.provider || '-'} / {it.model || '-'}</div>
                      <div className="text-[11px] text-zinc-500 truncate">{formatLocalDateTime(it.time)}</div>
                    </div>
                    {active && (
                      <span className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-indigo-500/15 text-indigo-300 self-center">
                        <Check className="w-3.5 h-3.5" />
                      </span>
                    )}
                  </div>
                </button>
              );
            })}
          </div>
        </div>

        <div className="brand-card rounded-[28px] border border-zinc-800 overflow-hidden flex flex-col min-h-0">
          <div className="px-3 py-2 border-b border-zinc-800 text-xs text-zinc-400 uppercase tracking-wider">{t('taskDetail')}</div>
          <div className="p-4 overflow-y-auto min-h-0 space-y-3 text-sm">
            {!selected ? (
              <div className="text-zinc-500">{t('selectTask')}</div>
            ) : (
              <>
                <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
                  <div><div className="text-zinc-500 text-xs">{t('taskId')}</div><div className="font-mono break-all">{selected.task_id}</div></div>
                  <div><div className="text-zinc-500 text-xs">{t('status')}</div><div>{selected.status}</div></div>
                  <div><div className="text-zinc-500 text-xs">{t('source')}</div><div>{selected.source || '-'}</div></div>
                  <div><div className="text-zinc-500 text-xs">{t('duration')}</div><div>{selected.duration_ms || 0}ms</div></div>
                  <div><div className="text-zinc-500 text-xs">{t('channel')}</div><div>{selected.channel}</div></div>
                  <div><div className="text-zinc-500 text-xs">{t('session')}</div><div className="font-mono break-all">{selected.session}</div></div>
                  <div><div className="text-zinc-500 text-xs">{t('provider')}</div><div>{selected.provider || '-'}</div></div>
                  <div><div className="text-zinc-500 text-xs">{t('model')}</div><div>{selected.model || '-'}</div></div>
                  <div><div className="text-zinc-500 text-xs">{t('time')}</div><div>{formatLocalDateTime(selected.time)}</div></div>
                </div>

                <div>
                  <div className="text-zinc-500 text-xs mb-1">{t('inputPreview')}</div>
                  <div className="p-2 rounded-xl bg-zinc-950/60 border border-zinc-800 whitespace-pre-wrap">{selected.input_preview || '-'}</div>
                </div>

                <div>
                  <div className="text-zinc-500 text-xs mb-1">{t('error')}</div>
                  <div className="p-2 rounded-xl bg-zinc-950/60 border border-zinc-800 whitespace-pre-wrap text-red-300">{selected.error || '-'}</div>
                </div>

                <div>
                  <div className="text-zinc-500 text-xs mb-1">{t('blockReason')}</div>
                  <div className="p-2 rounded-xl bg-zinc-950/60 border border-zinc-800 whitespace-pre-wrap text-amber-200">{selected.block_reason || '-'}</div>
                </div>

                <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                  <div>
                    <div className="text-zinc-500 text-xs mb-1">{t('lastPauseReason')}</div>
                    <div className="p-2 rounded-xl bg-zinc-950/60 border border-zinc-800 whitespace-pre-wrap text-zinc-200">{selected.last_pause_reason || '-'}</div>
                  </div>
                  <div>
                    <div className="text-zinc-500 text-xs mb-1">{t('lastPauseAt')}</div>
                    <div className="p-2 rounded-xl bg-zinc-950/60 border border-zinc-800 whitespace-pre-wrap text-zinc-200">{formatLocalDateTime(selected.last_pause_at)}</div>
                  </div>
                </div>

                <div>
                  <div className="text-zinc-500 text-xs mb-1">{t('taskLogs')}</div>
                  <div className="p-2 rounded-xl bg-zinc-950/60 border border-zinc-800 whitespace-pre-wrap text-zinc-200">{Array.isArray(selected.logs) && selected.logs.length ? selected.logs.join('\n') : '-'}</div>
                </div>

                <div>
                  <div className="text-zinc-500 text-xs mb-1">{t('mediaSources')}</div>
                  <div className="p-2 rounded-xl bg-zinc-950/60 border border-zinc-800 text-xs">
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
                  <pre className="p-2 rounded-xl bg-zinc-950/60 border border-zinc-800 text-xs overflow-auto">{selectedPretty}</pre>
                </div>
              </>
            )}
          </div>
        </div>

        <div className="brand-card rounded-[28px] border border-zinc-800 overflow-hidden flex flex-col min-h-0">
          <div className="px-3 py-2 border-b border-zinc-800 text-xs text-zinc-400 uppercase tracking-wider">{t('dashboardNodeDispatches')}</div>
          <div className="grid grid-cols-1 min-h-0 flex-1">
            <div className="overflow-y-auto min-h-0 border-b border-zinc-800/60">
              {nodeItems.length === 0 ? (
                <div className="p-4 text-sm text-zinc-500">{t('dashboardNodeDispatchesEmpty')}</div>
              ) : nodeItems.map((it, idx) => {
                const active = selectedNode?.time === it.time && selectedNode?.node === it.node && selectedNode?.action === it.action;
                return (
                  <button
                    key={`${it.time || idx}-${it.node || idx}-${it.action || idx}`}
                    onClick={() => setSelectedNode(it)}
                    className={`w-full text-left px-3 py-2 border-b border-zinc-800/60 transition-colors ${active ? 'bg-indigo-500/15' : ''}`}
                  >
                    <div className="flex items-start gap-3">
                      <div className="min-w-0 flex-1">
                        <div className="text-sm font-medium text-zinc-100 truncate">{`${it.node || '-'} · ${it.action || '-'}`}</div>
                        <div className="text-xs text-zinc-400 truncate">{it.used_transport || '-'} · {(it.duration_ms || 0)}ms · {(it.artifact_count || 0)} {t('dashboardNodeDispatchArtifacts')}</div>
                        <div className="text-[11px] text-zinc-500 truncate">{formatLocalDateTime(it.time)}</div>
                      </div>
                      {active && (
                        <span className="mt-0.5 inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-indigo-500/15 text-indigo-300">
                          <Check className="w-3.5 h-3.5" />
                        </span>
                      )}
                    </div>
                  </button>
                );
              })}
            </div>
            <div className="p-4 overflow-y-auto min-h-0 space-y-3 text-sm">
              {!selectedNode ? (
                <div className="text-zinc-500">{t('selectTask')}</div>
              ) : (
                <>
                  <div className="grid grid-cols-2 gap-3">
                    <div><div className="text-zinc-500 text-xs">{t('nodeP2P')}</div><div>{selectedNode.node || '-'}</div></div>
                    <div><div className="text-zinc-500 text-xs">{t('action')}</div><div>{selectedNode.action || '-'}</div></div>
                    <div><div className="text-zinc-500 text-xs">{t('dashboardNodeDispatchTransport')}</div><div>{selectedNode.used_transport || '-'}</div></div>
                    <div><div className="text-zinc-500 text-xs">{t('dashboardNodeDispatchFallback')}</div><div>{selectedNode.fallback_from || '-'}</div></div>
                    <div><div className="text-zinc-500 text-xs">{t('duration')}</div><div>{selectedNode.duration_ms || 0}ms</div></div>
                    <div><div className="text-zinc-500 text-xs">{t('status')}</div><div>{selectedNode.ok ? 'ok' : 'error'}</div></div>
                  </div>

                  <div>
                    <div className="text-zinc-500 text-xs mb-1">{t('error')}</div>
                    <div className="p-2 rounded-xl bg-zinc-950/60 border border-zinc-800 whitespace-pre-wrap text-red-300">{selectedNode.error || '-'}</div>
                  </div>

                  <div>
                    <div className="text-zinc-500 text-xs mb-1">{t('dashboardNodeDispatchArtifactPreview')}</div>
                    <div className="space-y-3">
                      {Array.isArray(selectedNode.artifacts) && selectedNode.artifacts.length > 0 ? selectedNode.artifacts.map((artifact, artifactIndex) => {
                        const kind = String(artifact?.kind || '').trim().toLowerCase();
                        const mime = String(artifact?.mime_type || '').trim().toLowerCase();
                        const isImage = kind === 'image' || mime.startsWith('image/');
                        const isVideo = kind === 'video' || mime.startsWith('video/');
                        const dataUrl = dataUrlForArtifact(artifact);
                        return (
                          <div key={`artifact-${artifactIndex}`} className="rounded-2xl border border-zinc-800 bg-zinc-950/40 p-3">
                            <div className="text-xs font-medium text-zinc-200 truncate">{String(artifact?.name || artifact?.source_path || `artifact-${artifactIndex + 1}`)}</div>
                            <div className="text-[11px] text-zinc-500 mt-1 truncate">
                              {[artifact?.kind, artifact?.mime_type, formatBytes(artifact?.size_bytes)].filter(Boolean).join(' · ')}
                            </div>
                            <div className="mt-2">
                              {isImage && dataUrl && <img src={dataUrl} alt={String(artifact?.name || 'artifact')} className="max-h-48 rounded-xl border border-zinc-800 object-contain bg-black/30" />}
                              {isVideo && dataUrl && <video src={dataUrl} controls className="max-h-48 w-full rounded-xl border border-zinc-800 bg-black/30" />}
                              {!isImage && !isVideo && String(artifact?.content_text || '').trim() !== '' && (
                                <pre className="rounded-xl border border-zinc-800 bg-black/20 p-3 text-[11px] text-zinc-300 whitespace-pre-wrap overflow-auto max-h-48">{String(artifact?.content_text || '')}</pre>
                              )}
                              {!isImage && !isVideo && String(artifact?.content_text || '').trim() === '' && (
                                <div className="text-[11px] text-zinc-500 break-all mt-2">{String(artifact?.source_path || artifact?.path || artifact?.url || '-')}</div>
                              )}
                            </div>
                          </div>
                        );
                      }) : (
                        <div className="p-2 rounded-xl bg-zinc-950/60 border border-zinc-800 text-zinc-500">-</div>
                      )}
                    </div>
                  </div>

                  <div>
                    <div className="text-zinc-500 text-xs mb-1">{t('rawJson')}</div>
                    <pre className="p-2 rounded-xl bg-zinc-950/60 border border-zinc-800 text-xs overflow-auto">{JSON.stringify(selectedNode, null, 2)}</pre>
                  </div>
                </>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

export default TaskAudit;
