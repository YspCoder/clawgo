import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { RefreshCw } from 'lucide-react';
import ArtifactPreviewCard from '../components/data-display/ArtifactPreviewCard';
import { useAppContext } from '../context/AppContext';
import { dataUrlForArtifact, formatArtifactBytes } from '../utils/artifacts';
import CodeBlockPanel from '../components/data-display/CodeBlockPanel';
import DetailGrid from '../components/data-display/DetailGrid';
import EmptyState from '../components/data-display/EmptyState';
import { FixedButton } from '../components/ui/Button';
import InfoBlock from '../components/data-display/InfoBlock';
import ListPanel from '../components/layout/ListPanel';
import PageHeader from '../components/layout/PageHeader';
import PanelHeader from '../components/layout/PanelHeader';
import { SelectField } from '../components/ui/FormControls';
import SummaryListItem from '../components/list/SummaryListItem';
import ToolbarRow from '../components/layout/ToolbarRow';
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
      <PageHeader
        title={t('taskAudit')}
        titleClassName="text-xl md:text-2xl font-semibold"
        actions={(
          <ToolbarRow className="flex-nowrap">
            <SelectField dense className="w-[152px] shrink-0" value={sourceFilter} onChange={(e) => setSourceFilter(e.target.value)}>
              <option value="all">{t('allSources')}</option>
              <option value="direct">{t('sourceDirect')}</option>
              <option value="memory_todo">{t('sourceMemoryTodo')}</option>
              <option value="task_watchdog">task_watchdog</option>
              <option value="-">-</option>
            </SelectField>
            <SelectField dense className="w-[152px] shrink-0" value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)}>
              <option value="all">{t('allStatus')}</option>
              <option value="running">{t('statusRunning')}</option>
              <option value="waiting">{t('statusWaiting')}</option>
              <option value="blocked">{t('statusBlocked')}</option>
              <option value="success">{t('statusSuccess')}</option>
              <option value="error">{t('statusError')}</option>
              <option value="suppressed">{t('statusSuppressed')}</option>
            </SelectField>
            <FixedButton onClick={fetchData} variant="primary" label={loading ? t('loading') : t('refresh')}>
              <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
            </FixedButton>
          </ToolbarRow>
        )}
      />

      <div className="flex-1 min-h-0 grid grid-cols-1 xl:grid-cols-[320px_1fr_380px] gap-4">
        <ListPanel>
          <PanelHeader title={t('taskQueue')} />
          <div className="overflow-y-auto min-h-0">
            {filteredItems.length === 0 ? (
              <EmptyState message={t('noTaskAudit')} padded />
            ) : filteredItems.map((it, idx) => {
              const active = selected?.task_id === it.task_id && selected?.time === it.time;
              return (
                <SummaryListItem
                  key={`${it.task_id || idx}-${it.time || idx}`}
                  onClick={() => setSelected(it)}
                  active={active}
                  title={it.task_id || `task-${idx + 1}`}
                  subtitle={`${it.channel || '-'} · ${it.status} · attempts:${it.attempts || 1} · ${it.duration_ms || 0}ms · retry:${it.retry_count || 0} · ${it.source || '-'} · ${it.provider || '-'} / ${it.model || '-'}`}
                  meta={formatLocalDateTime(it.time)}
                />
              );
            })}
          </div>
        </ListPanel>

        <ListPanel>
          <PanelHeader title={t('taskDetail')} />
          <div className="p-4 overflow-y-auto min-h-0 space-y-3 text-sm">
            {!selected ? (
              <EmptyState message={t('selectTask')} />
            ) : (
              <>
                <DetailGrid
                  items={[
                    { label: t('taskId'), value: selected.task_id, valueClassName: 'font-mono break-all' },
                    { label: t('status'), value: selected.status },
                    { label: t('source'), value: selected.source || '-' },
                    { label: t('duration'), value: `${selected.duration_ms || 0}ms` },
                    { label: t('channel'), value: selected.channel },
                    { label: t('session'), value: selected.session, valueClassName: 'font-mono break-all' },
                    { label: t('provider'), value: selected.provider || '-' },
                    { label: t('model'), value: selected.model || '-' },
                    { label: t('time'), value: formatLocalDateTime(selected.time) },
                  ]}
                />

                <InfoBlock label={t('inputPreview')} contentClassName="whitespace-pre-wrap">{selected.input_preview || '-'}</InfoBlock>
                <InfoBlock label={t('error')} contentClassName="ui-code-danger whitespace-pre-wrap">{selected.error || '-'}</InfoBlock>
                <InfoBlock label={t('blockReason')} contentClassName="ui-code-warning whitespace-pre-wrap">{selected.block_reason || '-'}</InfoBlock>

                <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                  <InfoBlock label={t('lastPauseReason')} contentClassName="whitespace-pre-wrap">{selected.last_pause_reason || '-'}</InfoBlock>
                  <InfoBlock label={t('lastPauseAt')} contentClassName="whitespace-pre-wrap">{formatLocalDateTime(selected.last_pause_at)}</InfoBlock>
                </div>

                <InfoBlock label={t('taskLogs')} contentClassName="whitespace-pre-wrap">
                  {Array.isArray(selected.logs) && selected.logs.length ? selected.logs.join('\n') : '-'}
                </InfoBlock>

                <InfoBlock label={t('mediaSources')} contentClassName="text-xs">
                  {Array.isArray(selected.media_items) && selected.media_items.length > 0 ? (
                    <div className="space-y-1">
                      {selected.media_items.map((m, i) => (
                        <div key={i} className="font-mono break-all text-zinc-200">
                          [{m.channel || '-'}] {m.source || '-'} / {m.type || '-'} · {m.path || m.ref || '-'}
                        </div>
                      ))}
                    </div>
                  ) : '-'}
                </InfoBlock>

                <InfoBlock label={t('rawJson')} contentClassName="text-xs overflow-auto">
                  <pre>{selectedPretty}</pre>
                </InfoBlock>
              </>
            )}
          </div>
        </ListPanel>

        <ListPanel>
          <PanelHeader title={t('dashboardNodeDispatches')} />
          <div className="grid grid-cols-1 min-h-0 flex-1">
            <div className="overflow-y-auto min-h-0 border-b border-zinc-800/60 dark:border-zinc-700/60">
              {nodeItems.length === 0 ? (
                <EmptyState message={t('dashboardNodeDispatchesEmpty')} padded />
              ) : nodeItems.map((it, idx) => {
                const active = selectedNode?.time === it.time && selectedNode?.node === it.node && selectedNode?.action === it.action;
                return (
                  <SummaryListItem
                    key={`${it.time || idx}-${it.node || idx}-${it.action || idx}`}
                    onClick={() => setSelectedNode(it)}
                    active={active}
                    title={`${it.node || '-'} · ${it.action || '-'}`}
                    subtitle={`${it.used_transport || '-'} · ${(it.duration_ms || 0)}ms · ${(it.artifact_count || 0)} ${t('dashboardNodeDispatchArtifacts')}`}
                    meta={formatLocalDateTime(it.time)}
                  />
                );
              })}
            </div>
            <div className="p-4 overflow-y-auto min-h-0 space-y-3 text-sm">
              {!selectedNode ? (
                <EmptyState message={t('selectTask')} />
              ) : (
                <>
                  <DetailGrid
                    columnsClassName="grid-cols-2"
                    items={[
                      { label: t('nodeP2P'), value: selectedNode.node || '-' },
                      { label: t('action'), value: selectedNode.action || '-' },
                      { label: t('dashboardNodeDispatchTransport'), value: selectedNode.used_transport || '-' },
                      { label: t('dashboardNodeDispatchFallback'), value: selectedNode.fallback_from || '-' },
                      { label: t('duration'), value: `${selectedNode.duration_ms || 0}ms` },
                      { label: t('status'), value: selectedNode.ok ? 'ok' : 'error' },
                    ]}
                  />

                  <CodeBlockPanel label={t('error')} className="" codeClassName="ui-code-danger whitespace-pre-wrap">
                    {selectedNode.error || '-'}
                  </CodeBlockPanel>

                  <div>
                    <div className="text-zinc-500 text-xs mb-1">{t('dashboardNodeDispatchArtifactPreview')}</div>
                    <div className="space-y-3">
                      {Array.isArray(selectedNode.artifacts) && selectedNode.artifacts.length > 0 ? selectedNode.artifacts.map((artifact, artifactIndex) => {
                        const dataUrl = dataUrlForArtifact(artifact);
                        return (
                          <ArtifactPreviewCard
                            key={`artifact-${artifactIndex}`}
                            artifact={artifact}
                            dataUrl={dataUrl}
                            fallbackName={`artifact-${artifactIndex + 1}`}
                            formatBytes={formatArtifactBytes}
                          />
                        );
                      }) : (
                        <EmptyState message="-" className="ui-code-panel p-2" />
                      )}
                    </div>
                  </div>

                  <CodeBlockPanel label={t('rawJson')} pre>{JSON.stringify(selectedNode, null, 2)}</CodeBlockPanel>
                </>
              )}
            </div>
          </div>
        </ListPanel>
      </div>
    </div>
  );
};

export default TaskAudit;
