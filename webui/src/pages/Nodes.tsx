import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { formatLocalDateTime } from '../utils/time';

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
  const { q, nodes, nodeTrees, nodeP2P, refreshNodes } = useAppContext();
  const [selectedNodeID, setSelectedNodeID] = useState('');
  const [dispatches, setDispatches] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);

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
        const r = await fetch(`/webui/api/node_dispatches${q ? `${q}&limit=200` : '?limit=200'}`);
        if (!r.ok) throw new Error(await r.text());
        const j = await r.json();
        if (!cancelled) {
          setDispatches(Array.isArray(j.items) ? j.items : []);
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
  }, [q]);

  const selectedNode = useMemo(() => {
    return nodeItems.find((item) => String(item?.id || '') === selectedNodeID) || nodeItems[0] || null;
  }, [nodeItems, selectedNodeID]);

  const selectedTree = useMemo(() => {
    const nodeID = String(selectedNode?.id || '');
    return treeItems.find((item) => String(item?.node_id || '') === nodeID) || null;
  }, [treeItems, selectedNode]);

  const selectedSession = useMemo(() => {
    const nodeID = String(selectedNode?.id || '');
    const sessions = Array.isArray(nodeP2P?.nodes) ? nodeP2P.nodes : [];
    return sessions.find((item: any) => String(item?.node || '') === nodeID) || null;
  }, [nodeP2P, selectedNode]);

  const filteredDispatches = useMemo(() => {
    const nodeID = String(selectedNode?.id || '');
    return dispatches.filter((item) => String(item?.node || '') === nodeID);
  }, [dispatches, selectedNode]);

  return (
    <div className="h-full p-4 md:p-6 xl:p-8 flex flex-col gap-4">
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div>
          <h1 className="text-xl md:text-2xl font-semibold">{t('nodes')}</h1>
          <div className="text-sm text-zinc-500 mt-1">{t('nodesDetailHint')}</div>
        </div>
        <button onClick={() => { refreshNodes(); }} className="brand-button px-3 py-1.5 rounded-xl text-sm text-white">
          {loading ? t('loading') : t('refresh')}
        </button>
      </div>

      <div className="flex-1 min-h-0 grid grid-cols-1 xl:grid-cols-[320px_1fr] gap-4">
        <div className="brand-card rounded-[28px] border border-zinc-800 overflow-hidden flex flex-col min-h-0">
          <div className="px-3 py-2 border-b border-zinc-800 text-xs text-zinc-400 uppercase tracking-wider">{t('nodes')}</div>
          <div className="overflow-y-auto min-h-0">
            {nodeItems.length === 0 ? (
              <div className="p-4 text-sm text-zinc-500">{t('noNodes')}</div>
            ) : nodeItems.map((node: any, index: number) => {
              const nodeID = String(node?.id || `node-${index}`);
              const active = String(selectedNode?.id || '') === nodeID;
              return (
                <button
                  key={nodeID}
                  onClick={() => setSelectedNodeID(nodeID)}
                  className={`w-full text-left px-3 py-2 border-b border-zinc-800/60 hover:bg-zinc-800/20 ${active ? 'bg-indigo-500/15' : ''}`}
                >
                  <div className="text-sm font-medium text-zinc-100 truncate">{String(node?.name || nodeID)}</div>
                  <div className="text-xs text-zinc-400 truncate">{nodeID} · {String(node?.os || '-')} / {String(node?.arch || '-')}</div>
                  <div className="text-[11px] text-zinc-500 truncate">{String(node?.online ? t('online') : t('offline'))} · {String(node?.version || '-')}</div>
                </button>
              );
            })}
          </div>
        </div>

        <div className="grid grid-cols-1 2xl:grid-cols-[1.1fr_1fr] gap-4 min-h-0">
          <div className="brand-card rounded-[28px] border border-zinc-800 overflow-hidden flex flex-col min-h-0">
            <div className="px-3 py-2 border-b border-zinc-800 text-xs text-zinc-400 uppercase tracking-wider">{t('nodeDetails')}</div>
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

                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <div>
                      <div className="text-zinc-500 text-xs mb-1">{t('nodeCapabilities')}</div>
                      <div className="p-3 rounded-2xl bg-zinc-950/60 border border-zinc-800 text-zinc-200 break-all">
                        {Object.entries(selectedNode.capabilities || {}).filter(([, enabled]) => Boolean(enabled)).map(([key]) => key).join(', ') || '-'}
                      </div>
                    </div>
                    <div>
                      <div className="text-zinc-500 text-xs mb-1">{t('nodeActions')}</div>
                      <div className="p-3 rounded-2xl bg-zinc-950/60 border border-zinc-800 text-zinc-200 break-all">
                        {Array.isArray(selectedNode.actions) && selectedNode.actions.length > 0 ? selectedNode.actions.join(', ') : '-'}
                      </div>
                    </div>
                    <div>
                      <div className="text-zinc-500 text-xs mb-1">{t('nodeModels')}</div>
                      <div className="p-3 rounded-2xl bg-zinc-950/60 border border-zinc-800 text-zinc-200 break-all">
                        {Array.isArray(selectedNode.models) && selectedNode.models.length > 0 ? selectedNode.models.join(', ') : '-'}
                      </div>
                    </div>
                    <div>
                      <div className="text-zinc-500 text-xs mb-1">{t('nodeAgents')}</div>
                      <div className="p-3 rounded-2xl bg-zinc-950/60 border border-zinc-800 text-zinc-200 break-all">
                        {Array.isArray(selectedNode.agents) && selectedNode.agents.length > 0 ? selectedNode.agents.map((item: any) => String(item?.id || '-')).join(', ') : '-'}
                      </div>
                    </div>
                  </div>

                  <div>
                    <div className="text-zinc-500 text-xs mb-1">{t('nodeP2P')}</div>
                    <div className="p-3 rounded-2xl bg-zinc-950/60 border border-zinc-800 text-zinc-200">
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
                    <div className="p-3 rounded-2xl bg-zinc-950/60 border border-zinc-800 text-zinc-200 space-y-2">
                      {Array.isArray(selectedTree?.items) && selectedTree.items.length > 0 ? selectedTree.items.map((item: any, index: number) => (
                        <div key={`${item?.agent_id || index}`} className="rounded-xl border border-zinc-800/80 bg-black/20 p-3">
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

          <div className="brand-card rounded-[28px] border border-zinc-800 overflow-hidden flex flex-col min-h-0">
            <div className="px-3 py-2 border-b border-zinc-800 text-xs text-zinc-400 uppercase tracking-wider">{t('dashboardNodeDispatches')}</div>
            <div className="p-4 overflow-y-auto min-h-0 space-y-3 text-sm">
              {filteredDispatches.length === 0 ? (
                <div className="text-zinc-500">{t('dashboardNodeDispatchesEmpty')}</div>
              ) : filteredDispatches.map((item: any, index: number) => (
                <div key={`${item?.time || index}-${index}`} className="rounded-2xl border border-zinc-800 bg-zinc-950/40 p-3">
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="text-sm font-medium text-zinc-100 truncate">{String(item?.action || '-')}</div>
                      <div className="text-xs text-zinc-500 mt-1">{formatLocalDateTime(item?.time)}</div>
                    </div>
                    <div className={`shrink-0 rounded-full px-2.5 py-1 text-[11px] font-medium ${item?.ok ? 'bg-emerald-500/10 text-emerald-300' : 'bg-rose-500/10 text-rose-300'}`}>
                      {item?.ok ? 'ok' : 'error'}
                    </div>
                  </div>
                  <div className="grid grid-cols-2 gap-3 mt-4 text-xs">
                    <div><div className="text-zinc-400">{t('dashboardNodeDispatchTransport')}</div><div className="text-zinc-200 mt-1">{String(item?.used_transport || '-')}</div></div>
                    <div><div className="text-zinc-400">{t('dashboardNodeDispatchFallback')}</div><div className="text-zinc-200 mt-1">{String(item?.fallback_from || '-')}</div></div>
                    <div><div className="text-zinc-400">{t('duration')}</div><div className="text-zinc-200 mt-1">{Number(item?.duration_ms || 0)}ms</div></div>
                    <div><div className="text-zinc-400">{t('dashboardNodeDispatchArtifacts')}</div><div className="text-zinc-200 mt-1">{Number(item?.artifact_count || 0)}</div></div>
                  </div>
                  {Array.isArray(item?.artifacts) && item.artifacts.length > 0 && (
                    <div className="mt-4 space-y-3">
                      {item.artifacts.slice(0, 3).map((artifact: any, artifactIndex: number) => {
                        const kind = String(artifact?.kind || '').trim().toLowerCase();
                        const mime = String(artifact?.mime_type || '').trim().toLowerCase();
                        const isImage = kind === 'image' || mime.startsWith('image/');
                        const isVideo = kind === 'video' || mime.startsWith('video/');
                        const dataUrl = dataUrlForArtifact(artifact);
                        return (
                          <div key={`artifact-${artifactIndex}`} className="rounded-xl border border-zinc-800/80 bg-black/20 p-3">
                            <div className="text-xs font-medium text-zinc-200 truncate">{String(artifact?.name || artifact?.source_path || `artifact-${artifactIndex + 1}`)}</div>
                            <div className="text-[11px] text-zinc-500 mt-1 truncate">
                              {[artifact?.kind, artifact?.mime_type, formatBytes(artifact?.size_bytes)].filter(Boolean).join(' · ')}
                            </div>
                            <div className="mt-2">
                              {isImage && dataUrl && <img src={dataUrl} alt={String(artifact?.name || 'artifact')} className="max-h-56 rounded-xl border border-zinc-800 object-contain bg-black/30" />}
                              {isVideo && dataUrl && <video src={dataUrl} controls className="max-h-56 w-full rounded-xl border border-zinc-800 bg-black/30" />}
                              {!isImage && !isVideo && String(artifact?.content_text || '').trim() !== '' && (
                                <pre className="rounded-xl border border-zinc-800 bg-black/20 p-3 text-[11px] text-zinc-300 whitespace-pre-wrap overflow-auto max-h-48">{String(artifact?.content_text || '')}</pre>
                              )}
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  )}
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

export default Nodes;
