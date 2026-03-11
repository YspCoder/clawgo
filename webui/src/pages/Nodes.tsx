import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { Check, Download, Play, RefreshCw, RotateCcw } from 'lucide-react';
import { useAppContext } from '../context/AppContext';
import { dataUrlForArtifact, formatArtifactBytes } from '../utils/artifacts';
import { formatLocalDateTime } from '../utils/time';
import { Button, FixedButton, FixedLinkButton } from '../components/Button';
import DetailGrid from '../components/DetailGrid';
import CodeBlockPanel from '../components/CodeBlockPanel';
import EmptyState from '../components/EmptyState';
import InfoBlock from '../components/InfoBlock';
import ListPanel from '../components/ListPanel';
import DispatchArtifactPreviewSection from '../components/nodes/DispatchArtifactPreviewSection';
import DispatchReplayPanel from '../components/nodes/DispatchReplayPanel';
import AgentTreePanel from '../components/nodes/AgentTreePanel';
import NodeP2PPanel from '../components/nodes/NodeP2PPanel';
import PageHeader from '../components/PageHeader';
import PanelHeader from '../components/PanelHeader';
import { SelectField, TextField } from '../components/FormControls';
import SectionHeader from '../components/SectionHeader';
import SummaryListItem from '../components/SummaryListItem';

const Nodes: React.FC = () => {
  const { t } = useTranslation();
  const { q, nodes, nodeTrees, nodeP2P, nodeAlerts, refreshNodes } = useAppContext();
  const [selectedNodeID, setSelectedNodeID] = useState('');
  const [dispatches, setDispatches] = useState<any[]>([]);
  const [selectedDispatchKey, setSelectedDispatchKey] = useState('');
  const [loading, setLoading] = useState(false);
  const [reloadTick, setReloadTick] = useState(0);
  const [nodeFilter, setNodeFilter] = useState('');
  const [dispatchActionFilter, setDispatchActionFilter] = useState('all');
  const [dispatchTransportFilter, setDispatchTransportFilter] = useState('all');
  const [dispatchStatusFilter, setDispatchStatusFilter] = useState('all');
  const [replayPending, setReplayPending] = useState(false);
  const [replayResult, setReplayResult] = useState<any>(null);
  const [replayError, setReplayError] = useState('');
  const [replayModeDraft, setReplayModeDraft] = useState('auto');
  const [replayTaskDraft, setReplayTaskDraft] = useState('');
  const [replayModelDraft, setReplayModelDraft] = useState('');
  const [replayArgsDraft, setReplayArgsDraft] = useState('{}');

  const nodeItems = useMemo(() => {
    try {
      const parsed = JSON.parse(nodes || '[]');
      return Array.isArray(parsed) ? parsed : [];
    } catch {
      return [];
    }
  }, [nodes]);

  const treeItems = useMemo(() => {
    try {
      const parsed = JSON.parse(nodeTrees || '[]');
      return Array.isArray(parsed) ? parsed : [];
    } catch {
      return [];
    }
  }, [nodeTrees]);

  useEffect(() => {
    if (!selectedNodeID && nodeItems.length > 0) {
      setSelectedNodeID(String(nodeItems[0]?.id || ''));
    }
  }, [selectedNodeID, nodeItems]);

  useEffect(() => {
    let cancelled = false;
    const fetchDispatches = async () => {
      setLoading(true);
      try {
        const r = await fetch(`/webui/api/node_dispatches${q ? `${q}&limit=300` : '?limit=300'}`);
        if (!r.ok) throw new Error(await r.text());
        const j = await r.json();
        if (!cancelled) {
          const items = Array.isArray(j.items) ? j.items : [];
          setDispatches(items);
          if (items.length > 0 && !selectedDispatchKey) {
            const first = items[0];
            setSelectedDispatchKey(`${first?.time || ''}:${first?.node || ''}:${first?.action || ''}`);
          }
        }
      } catch (err) {
        console.error(err);
        if (!cancelled) setDispatches([]);
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    fetchDispatches();
    return () => { cancelled = true; };
  }, [q, reloadTick]);

  const filteredNodes = useMemo(() => {
    const keyword = nodeFilter.trim().toLowerCase();
    if (!keyword) return nodeItems;
    return nodeItems.filter((item: any) => {
      const tags = Array.isArray(item?.tags) ? item.tags.join(' ') : '';
      const haystack = [
        item?.id,
        item?.name,
        item?.os,
        item?.arch,
        item?.version,
        tags,
      ].join(' ').toLowerCase();
      return haystack.includes(keyword);
    });
  }, [nodeItems, nodeFilter]);

  const selectedNode = useMemo(() => {
    return nodeItems.find((item) => String(item?.id || '') === selectedNodeID) || filteredNodes[0] || nodeItems[0] || null;
  }, [nodeItems, filteredNodes, selectedNodeID]);

  const selectedTree = useMemo(() => {
    const nodeID = String(selectedNode?.id || '');
    return treeItems.find((item) => String(item?.node_id || '') === nodeID) || null;
  }, [treeItems, selectedNode]);

  const selectedSession = useMemo(() => {
    const nodeID = String(selectedNode?.id || '');
    const sessions = Array.isArray(nodeP2P?.nodes) ? nodeP2P.nodes : [];
    return sessions.find((item: any) => String(item?.node || '') === nodeID) || null;
  }, [nodeP2P, selectedNode]);
  const selectedNodeAlerts = useMemo(() => {
    const nodeID = String(selectedNode?.id || '');
    return (Array.isArray(nodeAlerts) ? nodeAlerts : []).filter((item: any) => String(item?.node || '') === nodeID);
  }, [nodeAlerts, selectedNode]);

  const filteredDispatches = useMemo(() => {
    const nodeID = String(selectedNode?.id || '');
    return dispatches.filter((item) => {
      if (String(item?.node || '') !== nodeID) return false;
      if (dispatchActionFilter !== 'all' && String(item?.action || '') !== dispatchActionFilter) return false;
      if (dispatchTransportFilter !== 'all' && String(item?.used_transport || '-') !== dispatchTransportFilter) return false;
      if (dispatchStatusFilter === 'ok' && !item?.ok) return false;
      if (dispatchStatusFilter === 'error' && item?.ok) return false;
      return true;
    });
  }, [dispatches, selectedNode, dispatchActionFilter, dispatchTransportFilter, dispatchStatusFilter]);

  const dispatchActions = useMemo(() => {
    return Array.from(new Set(dispatches.map((item) => String(item?.action || '').trim()).filter(Boolean))).sort();
  }, [dispatches]);

  const dispatchTransports = useMemo(() => {
    return Array.from(new Set(dispatches.map((item) => String(item?.used_transport || '').trim()).filter(Boolean))).sort();
  }, [dispatches]);

  const selectedDispatch = useMemo(() => {
    if (!selectedDispatchKey) return filteredDispatches[0] || null;
    return filteredDispatches.find((item) => `${item?.time || ''}:${item?.node || ''}:${item?.action || ''}` === selectedDispatchKey) || filteredDispatches[0] || null;
  }, [filteredDispatches, selectedDispatchKey]);

  const selectedDispatchPretty = useMemo(() => selectedDispatch ? JSON.stringify(selectedDispatch, null, 2) : '', [selectedDispatch]);

  useEffect(() => {
    if (!selectedDispatch) {
      setReplayModeDraft('auto');
      setReplayTaskDraft('');
      setReplayModelDraft('');
      setReplayArgsDraft('{}');
      return;
    }
    setReplayModeDraft(String(selectedDispatch.mode || 'auto'));
    setReplayTaskDraft(String(selectedDispatch.task || ''));
    setReplayModelDraft(String(selectedDispatch.model || ''));
    setReplayArgsDraft(JSON.stringify(selectedDispatch.request_args || {}, null, 2));
  }, [selectedDispatch]);

  function resetReplayDraft() {
    if (!selectedDispatch) return;
    setReplayModeDraft(String(selectedDispatch.mode || 'auto'));
    setReplayTaskDraft(String(selectedDispatch.task || ''));
    setReplayModelDraft(String(selectedDispatch.model || ''));
    setReplayArgsDraft(JSON.stringify(selectedDispatch.request_args || {}, null, 2));
    setReplayError('');
  }

  async function replayDispatch() {
    if (!selectedDispatch) return;
    setReplayPending(true);
    setReplayError('');
    setReplayResult(null);
    try {
      let parsedArgs: Record<string, any> = {};
      try {
        parsedArgs = replayArgsDraft.trim() ? JSON.parse(replayArgsDraft) : {};
      } catch {
        throw new Error(t('nodeReplayInvalidArgs'));
      }
      const r = await fetch(`/webui/api/node_dispatches/replay${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          node: selectedDispatch.node,
          action: selectedDispatch.action,
          mode: replayModeDraft || 'auto',
          task: replayTaskDraft,
          model: replayModelDraft,
          args: parsedArgs,
        }),
      });
      const text = await r.text();
      if (!r.ok) throw new Error(text || 'replay failed');
      const json = text ? JSON.parse(text) : {};
      setReplayResult(json.result || null);
      setReloadTick((value) => value + 1);
      refreshNodes();
    } catch (err: any) {
      setReplayError(String(err?.message || err || 'replay failed'));
    } finally {
      setReplayPending(false);
    }
  }

  return (
    <div className="h-full p-4 md:p-6 xl:p-8 flex flex-col gap-4">
      <PageHeader
        title={t('nodes')}
        titleClassName="text-xl md:text-2xl font-semibold"
        subtitle={t('nodesDetailHint')}
        actions={(
          <FixedButton
            onClick={() => { refreshNodes(); setReloadTick((value) => value + 1); }}
            variant="primary"
            label={loading ? t('loading') : t('refresh')}
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
          </FixedButton>
        )}
      />

      <div className="grid grid-cols-1 xl:grid-cols-[300px_1fr_1.1fr] gap-4 flex-1 min-h-0">
        <ListPanel header={
          <div className="px-3 py-2 border-b border-zinc-800 dark:border-zinc-700 space-y-2">
            <TextField
              value={nodeFilter}
              onChange={(e) => setNodeFilter(e.target.value)}
              placeholder={t('nodesFilterPlaceholder')}
            />
          </div>
        }>
          <div className="overflow-y-auto min-h-0">
            {filteredNodes.length === 0 ? (
              <EmptyState message={t('noNodes')} padded />
            ) : filteredNodes.map((node: any, index: number) => {
              const nodeID = String(node?.id || `node-${index}`);
              const active = String(selectedNode?.id || '') === nodeID;
              const tags = Array.isArray(node?.tags) ? node.tags : [];
              return (
                <SummaryListItem
                  key={nodeID}
                  onClick={() => setSelectedNodeID(nodeID)}
                  className="py-3"
                  active={active}
                  title={String(node?.name || nodeID)}
                  subtitle={`${nodeID} · ${String(node?.os || '-')} / ${String(node?.arch || '-')}`}
                  meta={`${String(node?.online ? t('online') : t('offline'))} · ${String(node?.version || '-')}`}
                  badges={tags.slice(0, 4).map((tag: string) => (
                    <span key={`${nodeID}-${tag}`} className="rounded-full border border-zinc-700 bg-zinc-900/60 px-2 py-0.5 text-[10px] text-zinc-300">
                      {tag}
                    </span>
                  ))}
                />
              );
            })}
          </div>
        </ListPanel>

        <ListPanel>
          <PanelHeader title={t('nodeDetails')} />
          <div className="p-4 overflow-y-auto min-h-0 space-y-4 text-sm">
            {!selectedNode ? (
              <EmptyState message={t('noNodes')} />
            ) : (
              <>
                <DetailGrid
                  items={[
                    { label: t('status'), value: selectedNode.online ? t('online') : t('offline') },
                    { label: t('time'), value: formatLocalDateTime(selectedNode.last_seen_at) },
                    { label: t('version'), value: String(selectedNode.version || '-') },
                    { label: 'OS', value: String(selectedNode.os || '-') },
                    { label: 'Arch', value: String(selectedNode.arch || '-') },
                    { label: 'Endpoint', value: String(selectedNode.endpoint || '-'), valueClassName: 'break-all' },
                  ]}
                />

                <div className="flex items-center gap-2 flex-wrap">
                  <Link to={`/node-artifacts?node=${encodeURIComponent(String(selectedNode.id || ''))}`} className="contents">
                    <Button size="xs">{t('nodeArtifacts')}</Button>
                  </Link>
                  <FixedLinkButton href={`/webui/api/node_artifacts/export${q ? `${q}&node=${encodeURIComponent(String(selectedNode.id || ''))}` : `?node=${encodeURIComponent(String(selectedNode.id || ''))}`}`} label={t('export')}>
                    <Download className="w-4 h-4" />
                  </FixedLinkButton>
                </div>

                <InfoBlock label={t('nodeTags')} contentClassName="p-3 break-all">
                  {Array.isArray(selectedNode.tags) && selectedNode.tags.length > 0 ? selectedNode.tags.join(', ') : '-'}
                </InfoBlock>

                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  <InfoBlock label={t('nodeCapabilities')} contentClassName="p-3 break-all">
                    {Object.entries(selectedNode.capabilities || {}).filter(([, enabled]) => Boolean(enabled)).map(([key]) => key).join(', ') || '-'}
                  </InfoBlock>
                  <InfoBlock label={t('nodeActions')} contentClassName="p-3 break-all">
                    {Array.isArray(selectedNode.actions) && selectedNode.actions.length > 0 ? selectedNode.actions.join(', ') : '-'}
                  </InfoBlock>
                  <InfoBlock label={t('nodeModels')} contentClassName="p-3 break-all">
                    {Array.isArray(selectedNode.models) && selectedNode.models.length > 0 ? selectedNode.models.join(', ') : '-'}
                  </InfoBlock>
                  <InfoBlock label={t('nodeAgents')} contentClassName="p-3 break-all">
                    {Array.isArray(selectedNode.agents) && selectedNode.agents.length > 0 ? selectedNode.agents.map((item: any) => String(item?.id || '-')).join(', ') : '-'}
                  </InfoBlock>
                </div>

                <div>
                  <SectionHeader title={t('nodeAlerts')} className="mb-1" />
                  <div className="space-y-2">
                    {selectedNodeAlerts.length > 0 ? selectedNodeAlerts.map((alert: any, index: number) => {
                      const severity = String(alert?.severity || 'warning');
                      return (
                        <div key={`${alert?.kind || index}-${index}`} className={`rounded-2xl border p-3 ${severity === 'critical' ? 'ui-notice-danger' : 'ui-notice-warning'}`}>
                          <div className="flex items-center justify-between gap-3">
                            <div className="text-sm font-medium text-zinc-100">{String(alert?.title || '-')}</div>
                            <div className={`ui-pill rounded-full px-2 py-0.5 text-[10px] font-medium ${severity === 'critical' ? 'ui-pill-danger' : 'ui-pill-warning'}`}>{severity}</div>
                          </div>
                          <div className="mt-2 text-xs text-zinc-300 whitespace-pre-wrap break-words">{String(alert?.detail || '-')}</div>
                        </div>
                      );
                    }) : (
                      <EmptyState message={t('nodeAlertsEmpty')} className="ui-code-panel p-3" />
                    )}
                  </div>
                </div>

                <NodeP2PPanel
                  emptyLabel={t('dashboardNodeP2PSessionsEmpty')}
                  errorLabel={t('dashboardNodeP2PSessionError')}
                  readyLabel={t('dashboardNodeP2PSessionReady')}
                  retriesLabel={t('dashboardNodeP2PSessionRetries')}
                  selectedSession={selectedSession}
                  statusLabel={t('status')}
                  timeFormatter={formatLocalDateTime}
                  title={t('nodeP2P')}
                />

                <AgentTreePanel
                  emptyLabel={t('noAgentTree')}
                  items={Array.isArray(selectedTree?.items) ? selectedTree.items : []}
                  title={t('agentTree')}
                />
              </>
            )}
          </div>
        </ListPanel>

        <ListPanel>
          <div className="px-3 py-2 border-b border-zinc-800 dark:border-zinc-700 space-y-2">
            <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('nodeDispatchDetail')}</div>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-2">
              <SelectField dense value={dispatchActionFilter} onChange={(e) => setDispatchActionFilter(e.target.value)}>
                <option value="all">{t('allActions')}</option>
                {dispatchActions.map((action) => <option key={action} value={action}>{action}</option>)}
              </SelectField>
              <SelectField dense value={dispatchTransportFilter} onChange={(e) => setDispatchTransportFilter(e.target.value)}>
                <option value="all">{t('allTransports')}</option>
                {dispatchTransports.map((transport) => <option key={transport} value={transport}>{transport}</option>)}
              </SelectField>
              <SelectField dense value={dispatchStatusFilter} onChange={(e) => setDispatchStatusFilter(e.target.value)}>
                <option value="all">{t('allStatus')}</option>
                <option value="ok">ok</option>
                <option value="error">error</option>
              </SelectField>
            </div>
          </div>
          <div className="grid grid-rows-[220px_1fr] min-h-0 flex-1">
            <div className="overflow-y-auto min-h-0 border-b border-zinc-800/60 dark:border-zinc-700/60">
              {filteredDispatches.length === 0 ? (
                <div className="p-4 text-sm text-zinc-500">{t('dashboardNodeDispatchesEmpty')}</div>
              ) : filteredDispatches.map((item: any, index: number) => {
                const key = `${item?.time || ''}:${item?.node || ''}:${item?.action || ''}`;
                const active = `${selectedDispatch?.time || ''}:${selectedDispatch?.node || ''}:${selectedDispatch?.action || ''}` === key;
                return (
                  <SummaryListItem
                    key={key || `dispatch-${index}`}
                    onClick={() => setSelectedDispatchKey(key)}
                    className="border-b border-zinc-800/60 py-2"
                    active={active}
                    title={`${item?.action || '-'} · ${item?.used_transport || '-'}`}
                    subtitle={`${formatLocalDateTime(item?.time)} · ${Number(item?.duration_ms || 0)}ms · ${Number(item?.artifact_count || 0)} ${t('dashboardNodeDispatchArtifacts')}`}
                    trailing={active ? (
                      <span className="mt-0.5 inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-indigo-500/15 text-indigo-300">
                        <Check className="w-3.5 h-3.5" />
                      </span>
                    ) : null}
                  />
                );
              })}
            </div>
            <div className="p-4 overflow-y-auto min-h-0 space-y-3 text-sm">
              {!selectedDispatch ? (
                <EmptyState message={t('dashboardNodeDispatchesEmpty')} />
              ) : (
                <>
                  <div className="flex items-center justify-between gap-2">
                    <div className="text-sm font-medium text-zinc-200">{t('nodeDispatchDetail')}</div>
                    <div className="flex items-center gap-2">
                      <FixedButton onClick={resetReplayDraft} disabled={replayPending} label={t('resetReplayDraft')}>
                        <RotateCcw className="w-4 h-4" />
                      </FixedButton>
                      <FixedButton onClick={replayDispatch} disabled={replayPending} variant="primary" label={replayPending ? t('replaying') : t('replayDispatch')}>
                        <Play className="w-4 h-4" />
                      </FixedButton>
                    </div>
                  </div>
                  <div className="grid grid-cols-2 gap-3">
                    <div><div className="text-zinc-500 text-xs">{t('node')}</div><div>{selectedDispatch.node || '-'}</div></div>
                    <div><div className="text-zinc-500 text-xs">{t('action')}</div><div>{selectedDispatch.action || '-'}</div></div>
                    <div><div className="text-zinc-500 text-xs">{t('dashboardNodeDispatchTransport')}</div><div>{selectedDispatch.used_transport || '-'}</div></div>
                    <div><div className="text-zinc-500 text-xs">{t('dashboardNodeDispatchFallback')}</div><div>{selectedDispatch.fallback_from || '-'}</div></div>
                    <div><div className="text-zinc-500 text-xs">{t('duration')}</div><div>{Number(selectedDispatch.duration_ms || 0)}ms</div></div>
                    <div><div className="text-zinc-500 text-xs">{t('status')}</div><div>{selectedDispatch.ok ? 'ok' : 'error'}</div></div>
                  </div>

                  <CodeBlockPanel label={t('error')}>{selectedDispatch.error || '-'}</CodeBlockPanel>

                  <DispatchReplayPanel
                    argsLabel={t('args')}
                    modeLabel={t('mode')}
                    modelLabel={t('model')}
                    onArgsChange={setReplayArgsDraft}
                    onModeChange={setReplayModeDraft}
                    onModelChange={setReplayModelDraft}
                    onTaskChange={setReplayTaskDraft}
                    replayArgsDraft={replayArgsDraft}
                    replayError={replayError}
                    replayModeDraft={replayModeDraft}
                    replayModelDraft={replayModelDraft}
                    replayResult={replayResult}
                    replayTaskDraft={replayTaskDraft}
                    resultTitle={t('nodeReplayResult')}
                    taskLabel={t('task')}
                    title={t('nodeReplayRequest')}
                  />

                  <DispatchArtifactPreviewSection
                    artifacts={Array.isArray(selectedDispatch.artifacts) ? selectedDispatch.artifacts : []}
                    emptyLabel={t('dashboardNodeDispatchesEmpty')}
                    formatBytes={formatArtifactBytes}
                    getDataUrl={dataUrlForArtifact}
                    title={t('dashboardNodeDispatchArtifactPreview')}
                  />

                  <CodeBlockPanel label={t('rawJson')} pre>{selectedDispatchPretty}</CodeBlockPanel>
                </>
              )}
            </div>
          </div>
        </ListPanel>
      </div>
    </div>
  );
};

export default Nodes;
