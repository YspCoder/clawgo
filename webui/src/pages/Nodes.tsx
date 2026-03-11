import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { Check, RefreshCw } from 'lucide-react';
import { useAppContext } from '../context/AppContext';
import { formatLocalDateTime } from '../utils/time';
import { Button, FixedButton, LinkButton } from '../components/Button';
import { SelectField, TextField, TextareaField } from '../components/FormControls';

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
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div>
          <h1 className="text-xl md:text-2xl font-semibold">{t('nodes')}</h1>
          <div className="text-sm text-zinc-500 mt-1">{t('nodesDetailHint')}</div>
        </div>
        <FixedButton
          onClick={() => { refreshNodes(); setReloadTick((value) => value + 1); }}
          variant="primary"
          label={loading ? t('loading') : t('refresh')}
        >
          <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
        </FixedButton>
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-[300px_1fr_1.1fr] gap-4 flex-1 min-h-0">
        <div className="brand-card ui-panel rounded-[28px] overflow-hidden flex flex-col min-h-0">
          <div className="px-3 py-2 border-b border-zinc-800 dark:border-zinc-700 space-y-2">
            <TextField
              value={nodeFilter}
              onChange={(e) => setNodeFilter(e.target.value)}
              placeholder={t('nodesFilterPlaceholder')}
            />
          </div>
          <div className="overflow-y-auto min-h-0">
            {filteredNodes.length === 0 ? (
              <div className="p-4 text-sm text-zinc-500">{t('noNodes')}</div>
            ) : filteredNodes.map((node: any, index: number) => {
              const nodeID = String(node?.id || `node-${index}`);
              const active = String(selectedNode?.id || '') === nodeID;
              const tags = Array.isArray(node?.tags) ? node.tags : [];
              return (
                <button
                  key={nodeID}
                  onClick={() => setSelectedNodeID(nodeID)}
                  className="w-full text-left px-3 py-3 border-b border-zinc-800/60 transition-colors"
                >
                  <div className="flex items-center gap-3">
                    <div className="min-w-0 flex-1">
                      <div className="text-sm font-medium text-zinc-100 truncate">{String(node?.name || nodeID)}</div>
                      <div className="text-xs text-zinc-400 truncate">{nodeID} · {String(node?.os || '-')} / {String(node?.arch || '-')}</div>
                      <div className="text-[11px] text-zinc-500 truncate">{String(node?.online ? t('online') : t('offline'))} · {String(node?.version || '-')}</div>
                      {tags.length > 0 && (
                        <div className="flex flex-wrap gap-1 mt-2">
                          {tags.slice(0, 4).map((tag: string) => (
                            <span key={`${nodeID}-${tag}`} className="rounded-full border border-zinc-700 bg-zinc-900/60 px-2 py-0.5 text-[10px] text-zinc-300">
                              {tag}
                            </span>
                          ))}
                        </div>
                      )}
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

        <div className="brand-card ui-panel rounded-[28px] overflow-hidden flex flex-col min-h-0">
          <div className="px-3 py-2 border-b border-zinc-800 dark:border-zinc-700 text-xs text-zinc-400 uppercase tracking-wider">{t('nodeDetails')}</div>
          <div className="p-4 overflow-y-auto min-h-0 space-y-4 text-sm">
            {!selectedNode ? (
              <div className="text-zinc-500">{t('noNodes')}</div>
            ) : (
              <>
                <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
                  <div><div className="text-zinc-500 text-xs">{t('status')}</div><div>{selectedNode.online ? t('online') : t('offline')}</div></div>
                  <div><div className="text-zinc-500 text-xs">{t('time')}</div><div>{formatLocalDateTime(selectedNode.last_seen_at)}</div></div>
                  <div><div className="text-zinc-500 text-xs">{t('version')}</div><div>{String(selectedNode.version || '-')}</div></div>
                  <div><div className="text-zinc-500 text-xs">OS</div><div>{String(selectedNode.os || '-')}</div></div>
                  <div><div className="text-zinc-500 text-xs">Arch</div><div>{String(selectedNode.arch || '-')}</div></div>
                  <div><div className="text-zinc-500 text-xs">Endpoint</div><div className="break-all">{String(selectedNode.endpoint || '-')}</div></div>
                </div>

                <div className="flex items-center gap-2 flex-wrap">
                  <Link to={`/node-artifacts?node=${encodeURIComponent(String(selectedNode.id || ''))}`} className="contents">
                    <Button size="xs">{t('nodeArtifacts')}</Button>
                  </Link>
                  <LinkButton href={`/webui/api/node_artifacts/export${q ? `${q}&node=${encodeURIComponent(String(selectedNode.id || ''))}` : `?node=${encodeURIComponent(String(selectedNode.id || ''))}`}`} size="xs">
                    {t('export')}
                  </LinkButton>
                </div>

                <div>
                  <div className="text-zinc-500 text-xs mb-1">{t('nodeTags')}</div>
                  <div className="ui-code-panel p-3 break-all">
                    {Array.isArray(selectedNode.tags) && selectedNode.tags.length > 0 ? selectedNode.tags.join(', ') : '-'}
                  </div>
                </div>

                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  <div>
                    <div className="text-zinc-500 text-xs mb-1">{t('nodeCapabilities')}</div>
                    <div className="ui-code-panel p-3 break-all">
                      {Object.entries(selectedNode.capabilities || {}).filter(([, enabled]) => Boolean(enabled)).map(([key]) => key).join(', ') || '-'}
                    </div>
                  </div>
                  <div>
                    <div className="text-zinc-500 text-xs mb-1">{t('nodeActions')}</div>
                    <div className="ui-code-panel p-3 break-all">
                      {Array.isArray(selectedNode.actions) && selectedNode.actions.length > 0 ? selectedNode.actions.join(', ') : '-'}
                    </div>
                  </div>
                  <div>
                    <div className="text-zinc-500 text-xs mb-1">{t('nodeModels')}</div>
                    <div className="ui-code-panel p-3 break-all">
                      {Array.isArray(selectedNode.models) && selectedNode.models.length > 0 ? selectedNode.models.join(', ') : '-'}
                    </div>
                  </div>
                  <div>
                    <div className="text-zinc-500 text-xs mb-1">{t('nodeAgents')}</div>
                    <div className="ui-code-panel p-3 break-all">
                      {Array.isArray(selectedNode.agents) && selectedNode.agents.length > 0 ? selectedNode.agents.map((item: any) => String(item?.id || '-')).join(', ') : '-'}
                    </div>
                  </div>
                </div>

                <div>
                  <div className="text-zinc-500 text-xs mb-1">{t('nodeAlerts')}</div>
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
                      <div className="ui-code-panel p-3 text-zinc-500">{t('nodeAlertsEmpty')}</div>
                    )}
                  </div>
                </div>

                <div>
                  <div className="text-zinc-500 text-xs mb-1">{t('nodeP2P')}</div>
                  <div className="ui-code-panel p-3">
                    {selectedSession ? (
                      <div className="grid grid-cols-2 gap-3 text-xs">
                        <div><div className="text-zinc-500">{t('status')}</div><div>{String(selectedSession.status || 'unknown')}</div></div>
                        <div><div className="text-zinc-500">{t('dashboardNodeP2PSessionRetries')}</div><div>{Number(selectedSession.retry_count || 0)}</div></div>
                        <div><div className="text-zinc-500">{t('dashboardNodeP2PSessionReady')}</div><div>{formatLocalDateTime(selectedSession.last_ready_at)}</div></div>
                        <div><div className="text-zinc-500">{t('dashboardNodeP2PSessionError')}</div><div className="break-all">{String(selectedSession.last_error || '-')}</div></div>
                      </div>
                    ) : (
                      <div className="text-zinc-500">{t('dashboardNodeP2PSessionsEmpty')}</div>
                    )}
                  </div>
                </div>

                <div>
                  <div className="text-zinc-500 text-xs mb-1">{t('agentTree')}</div>
                  <div className="ui-code-panel p-3 space-y-2">
                    {Array.isArray(selectedTree?.items) && selectedTree.items.length > 0 ? selectedTree.items.map((item: any, index: number) => (
                      <div key={`${item?.agent_id || index}`} className="ui-media-surface rounded-xl border border-zinc-800/80 p-3">
                        <div className="text-sm font-medium text-zinc-100">{String(item?.display_name || item?.agent_id || '-')}</div>
                        <div className="text-xs text-zinc-500 mt-1">{String(item?.agent_id || '-')} · {String(item?.transport || '-')} · {String(item?.role || '-')}</div>
                      </div>
                    )) : (
                      <div className="text-zinc-500">{t('noAgentTree')}</div>
                    )}
                  </div>
                </div>
              </>
            )}
          </div>
        </div>

        <div className="brand-card ui-panel rounded-[28px] overflow-hidden flex flex-col min-h-0">
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
                  <button
                    key={key || `dispatch-${index}`}
                    onClick={() => setSelectedDispatchKey(key)}
                    className={`w-full text-left px-3 py-2 border-b border-zinc-800/60 transition-colors ${active ? 'bg-indigo-500/15' : ''}`}
                  >
                    <div className="flex items-start gap-3">
                      <div className="min-w-0 flex-1">
                        <div className="text-sm font-medium text-zinc-100 truncate">{`${item?.action || '-'} · ${item?.used_transport || '-'}`}</div>
                        <div className="text-xs text-zinc-400 truncate">{formatLocalDateTime(item?.time)} · {Number(item?.duration_ms || 0)}ms · {Number(item?.artifact_count || 0)} {t('dashboardNodeDispatchArtifacts')}</div>
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
              {!selectedDispatch ? (
                <div className="text-zinc-500">{t('dashboardNodeDispatchesEmpty')}</div>
              ) : (
                <>
                  <div className="flex items-center justify-between gap-2">
                    <div className="text-sm font-medium text-zinc-200">{t('nodeDispatchDetail')}</div>
                    <div className="flex items-center gap-2">
                      <Button onClick={resetReplayDraft} disabled={replayPending} size="xs">{t('resetReplayDraft')}</Button>
                      <Button onClick={replayDispatch} disabled={replayPending} variant="primary" size="xs">
                        {replayPending ? t('replaying') : t('replayDispatch')}
                      </Button>
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

                  <div>
                    <div className="text-zinc-500 text-xs mb-1">{t('error')}</div>
                    <div className="ui-code-panel p-3 whitespace-pre-wrap">{selectedDispatch.error || '-'}</div>
                  </div>

                  <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                    <div>
                      <div className="text-zinc-500 text-xs mb-1">{t('nodeReplayRequest')}</div>
                      <div className="space-y-2">
                        <div className="grid grid-cols-3 gap-2">
                          <label className="space-y-1">
                            <div className="text-zinc-500 text-[11px]">{t('mode')}</div>
                            <SelectField dense value={replayModeDraft} onChange={(e) => setReplayModeDraft(e.target.value)} className="w-full">
                              <option value="auto">auto</option>
                              <option value="p2p">p2p</option>
                              <option value="relay">relay</option>
                            </SelectField>
                          </label>
                          <label className="space-y-1 col-span-2">
                            <div className="text-zinc-500 text-[11px]">{t('model')}</div>
                            <TextField dense value={replayModelDraft} onChange={(e) => setReplayModelDraft(e.target.value)} className="w-full" />
                          </label>
                        </div>
                        <label className="space-y-1 block">
                          <div className="text-zinc-500 text-[11px]">{t('task')}</div>
                          <TextareaField dense value={replayTaskDraft} onChange={(e) => setReplayTaskDraft(e.target.value)} className="min-h-24 w-full p-3" />
                        </label>
                        <label className="space-y-1 block">
                          <div className="text-zinc-500 text-[11px]">{t('args')}</div>
                          <TextareaField dense monospace value={replayArgsDraft} onChange={(e) => setReplayArgsDraft(e.target.value)} className="min-h-40 w-full p-3" />
                        </label>
                      </div>
                    </div>
                    <div>
                      <div className="text-zinc-500 text-xs mb-1">{t('nodeReplayResult')}</div>
                      {replayError ? (
                        <div className="ui-notice-danger rounded-2xl border p-3 text-xs whitespace-pre-wrap">{replayError}</div>
                      ) : (
                        <pre className="ui-code-panel p-3 text-xs overflow-auto">{replayResult ? JSON.stringify(replayResult, null, 2) : '-'}</pre>
                      )}
                    </div>
                  </div>

                  <div>
                    <div className="text-zinc-500 text-xs mb-1">{t('dashboardNodeDispatchArtifactPreview')}</div>
                    <div className="space-y-3">
                      {Array.isArray(selectedDispatch.artifacts) && selectedDispatch.artifacts.length > 0 ? selectedDispatch.artifacts.map((artifact: any, artifactIndex: number) => {
                        const kind = String(artifact?.kind || '').trim().toLowerCase();
                        const mime = String(artifact?.mime_type || '').trim().toLowerCase();
                        const isImage = kind === 'image' || mime.startsWith('image/');
                        const isVideo = kind === 'video' || mime.startsWith('video/');
                        const dataUrl = dataUrlForArtifact(artifact);
                        return (
                          <div key={`artifact-${artifactIndex}`} className="ui-media-surface rounded-xl border border-zinc-800/80 p-3">
                            <div className="text-xs font-medium text-zinc-200 truncate">{String(artifact?.name || artifact?.source_path || `artifact-${artifactIndex + 1}`)}</div>
                            <div className="text-[11px] text-zinc-500 mt-1 truncate">
                              {[artifact?.kind, artifact?.mime_type, formatBytes(artifact?.size_bytes)].filter(Boolean).join(' · ')}
                            </div>
                            <div className="mt-2">
                              {isImage && dataUrl && <img src={dataUrl} alt={String(artifact?.name || 'artifact')} className="ui-media-surface-strong max-h-56 rounded-xl border object-contain" />}
                              {isVideo && dataUrl && <video src={dataUrl} controls className="ui-media-surface-strong max-h-56 w-full rounded-xl border" />}
                              {!isImage && !isVideo && String(artifact?.content_text || '').trim() !== '' && (
                                <pre className="ui-media-surface rounded-xl border p-3 text-[11px] text-zinc-300 whitespace-pre-wrap overflow-auto max-h-48">{String(artifact?.content_text || '')}</pre>
                              )}
                            </div>
                          </div>
                        );
                      }) : (
                        <div className="text-zinc-500">{t('dashboardNodeDispatchesEmpty')}</div>
                      )}
                    </div>
                  </div>

                  <div>
                    <div className="text-zinc-500 text-xs mb-1">{t('rawJson')}</div>
                    <pre className="ui-code-panel p-3 text-xs overflow-auto">{selectedDispatchPretty}</pre>
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

export default Nodes;
