import React, { useMemo } from 'react';
import { RefreshCw, Activity, MessageSquare, Wrench, Sparkles, AlertTriangle, Workflow } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import StatCard from '../components/StatCard';

function formatRuntimeTime(value: unknown) {
  const raw = String(value || '').trim();
  if (!raw || raw === '0001-01-01T00:00:00Z') return '-';
  const ts = Date.parse(raw);
  if (Number.isNaN(ts)) return raw;
  return new Date(ts).toLocaleString();
}

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

const Dashboard: React.FC = () => {
  const { t } = useTranslation();
  const {
    isGatewayOnline,
    sessions,
    refreshAll,
    gatewayVersion,
    webuiVersion,
    skills,
    cfg,
    nodeP2P,
    nodeDispatchItems,
    nodeAlerts,
    nodeArtifactRetention,
    taskQueueItems,
    ekgSummary,
  } = useAppContext();

  const subagentCount = useMemo(() => {
    const subagents = (cfg as any)?.agents?.subagents || {};
    return Object.keys(subagents).length;
  }, [cfg]);

  const recentTasks = useMemo(() => {
    return [...taskQueueItems]
      .sort((a: any, b: any) => String(b.time || '').localeCompare(String(a.time || '')))
      .slice(0, 8);
  }, [taskQueueItems]);

  const recentFailures = useMemo(() => {
    return recentTasks.filter((item: any) => String(item.status || '').toLowerCase() === 'error').slice(0, 5);
  }, [recentTasks]);

  const ekgEscalationCount = Number(ekgSummary?.escalation_count || 0);
  const ekgTopProvider = (Array.isArray(ekgSummary?.provider_top_workload) ? ekgSummary.provider_top_workload[0]?.key : '') || '-';
  const ekgTopErrSig = (Array.isArray(ekgSummary?.errsig_top_workload) ? ekgSummary.errsig_top_workload[0]?.key : '') || '-';
  const p2pEnabled = Boolean(nodeP2P?.enabled);
  const p2pTransport = String(nodeP2P?.transport || (p2pEnabled ? 'enabled' : 'disabled'));
  const p2pSessions = Number(nodeP2P?.active_sessions || 0);
  const p2pConfiguredStun = Array.isArray(nodeP2P?.configured_stun) ? nodeP2P.configured_stun.length : 0;
  const p2pConfiguredIce = Number(nodeP2P?.configured_ice || nodeP2P?.ice_servers || 0);
  const p2pRetryCount = Array.isArray(nodeP2P?.nodes)
    ? nodeP2P.nodes.reduce((sum: number, session: any) => sum + Number(session?.retry_count || 0), 0)
    : 0;
  const p2pNodeSessions = useMemo(() => {
    if (!Array.isArray(nodeP2P?.nodes)) return [];
    return [...nodeP2P.nodes]
      .map((session: any) => ({
        node: String(session?.node || '-'),
        status: String(session?.status || 'unknown'),
        retryCount: Number(session?.retry_count || 0),
        lastError: String(session?.last_error || '').trim(),
        lastReadyAt: formatRuntimeTime(session?.last_ready_at),
        lastAttempt: formatRuntimeTime(session?.last_attempt),
        createdAt: formatRuntimeTime(session?.created_at),
      }))
      .sort((a, b) => a.node.localeCompare(b.node));
  }, [nodeP2P]);
  const artifactRetentionEnabled = Boolean(nodeArtifactRetention?.enabled);
  const artifactRetentionPruned = Number(nodeArtifactRetention?.pruned || 0);
  const artifactRetentionRemaining = Number(nodeArtifactRetention?.remaining || 0);
  const artifactRetentionLastRun = formatRuntimeTime(nodeArtifactRetention?.last_run_at);
  const recentNodeDispatches = useMemo(() => {
    return [...nodeDispatchItems]
      .slice(0, 8)
      .map((item: any, index: number) => ({
        id: `${item?.time || 'dispatch'}-${index}`,
        time: formatRuntimeTime(item?.time),
        node: String(item?.node || '-'),
        action: String(item?.action || '-'),
        usedTransport: String(item?.used_transport || '-'),
        fallbackFrom: String(item?.fallback_from || '').trim(),
        durationMs: Number(item?.duration_ms || 0),
        artifactCount: Number(item?.artifact_count || 0),
        artifactKinds: Array.isArray(item?.artifact_kinds) ? item.artifact_kinds.map((kind: any) => String(kind || '').trim()).filter(Boolean) : [],
        artifacts: Array.isArray(item?.artifacts) ? item.artifacts : [],
        ok: Boolean(item?.ok),
        error: String(item?.error || '').trim(),
      }));
  }, [nodeDispatchItems]);
  const topNodeAlerts = useMemo(() => {
    return [...(Array.isArray(nodeAlerts) ? nodeAlerts : [])].slice(0, 6);
  }, [nodeAlerts]);

  return (
    <div className="p-4 md:p-6 xl:p-8 w-full space-y-6 xl:space-y-8">
      <div className="flex items-start justify-between gap-4 flex-wrap">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('dashboard')}</h1>
          <div className="mt-2 text-sm text-zinc-500">
            {t('gateway')}: <span className="font-mono text-zinc-300">{gatewayVersion}</span>
            {' · '}
            {t('webui')}: <span className="font-mono text-zinc-300">{webuiVersion}</span>
          </div>
        </div>
        <button onClick={refreshAll} className="brand-button flex items-center gap-2 px-4 py-2 rounded-xl text-sm font-medium transition-colors shrink-0 text-white">
          <RefreshCw className="w-4 h-4" /> {t('refreshAll')}
        </button>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-6 gap-4">
        <StatCard title={t('gatewayStatus')} value={isGatewayOnline ? t('online') : t('offline')} icon={<Activity className={`w-6 h-6 ${isGatewayOnline ? 'text-emerald-400' : 'text-red-400'}`} />} />
        <StatCard title={t('activeSessions')} value={sessions.length} icon={<MessageSquare className="w-6 h-6 text-blue-400" />} />
        <StatCard title={t('skills')} value={skills.length} icon={<Sparkles className="w-6 h-6 text-pink-400" />} />
        <StatCard title={t('subagentsRuntime')} value={subagentCount} icon={<Wrench className="w-6 h-6 text-cyan-400" />} />
        <StatCard title={t('taskAudit')} value={recentTasks.length} icon={<Activity className="w-6 h-6 text-amber-400" />} />
        <StatCard title={t('nodeP2P')} value={p2pEnabled ? `${p2pSessions} · ${p2pTransport}` : t('disabled')} icon={<Workflow className="w-6 h-6 text-violet-400" />} />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <div className="brand-card rounded-[28px] border border-zinc-800 p-5 min-h-[148px]">
          <div className="flex items-center gap-2 text-zinc-200 mb-2">
            <AlertTriangle className="w-4 h-4 text-amber-400" />
            <div className="text-sm font-medium">{t('ekgEscalations')}</div>
          </div>
          <div className="text-3xl font-semibold text-zinc-100">{ekgEscalationCount}</div>
          <div className="mt-2 text-xs text-zinc-500">{t('dashboardTopErrorSignature')}: {ekgTopErrSig}</div>
        </div>
        <div className="brand-card rounded-[28px] border border-zinc-800 p-5 min-h-[148px]">
          <div className="flex items-center gap-2 text-zinc-200 mb-2">
            <Workflow className="w-4 h-4 text-sky-400" />
            <div className="text-sm font-medium">{t('nodeP2P')}</div>
          </div>
          <div className="text-2xl font-semibold text-zinc-100 truncate">
            {p2pEnabled ? `${p2pConfiguredIce} ICE · ${p2pConfiguredStun} STUN` : t('disabled')}
          </div>
          <div className="mt-2 text-xs text-zinc-500">
            {t('dashboardNodeP2PDetail', { transport: p2pTransport, sessions: p2pSessions, retries: p2pRetryCount })}
          </div>
        </div>
        <div className="brand-card rounded-[28px] border border-zinc-800 p-5 min-h-[148px]">
          <div className="flex items-center gap-2 text-zinc-200 mb-2">
            <Activity className="w-4 h-4 text-rose-400" />
            <div className="text-sm font-medium">{t('taskAudit')}</div>
          </div>
          <div className="text-3xl font-semibold text-zinc-100">{recentFailures.length}</div>
          <div className="mt-2 text-xs text-zinc-500">{t('dashboardRecentFailedTasks')}</div>
        </div>
      </div>

      <div className="brand-card rounded-[30px] border border-zinc-800/80 p-6">
        <div className="flex items-center justify-between gap-3 mb-5 flex-wrap">
          <div>
            <div className="flex items-center gap-2 text-zinc-200">
              <Activity className="w-5 h-5 text-zinc-400" />
              <h2 className="text-lg font-medium">{t('nodeArtifactsRetention')}</h2>
            </div>
            <div className="text-xs text-zinc-500 mt-1">{t('nodeArtifactsRetentionHint')}</div>
          </div>
          <div className={`rounded-full px-2.5 py-1 text-[11px] font-medium ${artifactRetentionEnabled ? 'bg-emerald-500/10 text-emerald-300' : 'bg-zinc-800 text-zinc-400'}`}>
            {artifactRetentionEnabled ? t('enabled') : t('disabled')}
          </div>
        </div>
        <div className="grid grid-cols-1 md:grid-cols-4 gap-3 text-sm">
          <div className="brand-card-subtle rounded-2xl border border-zinc-800 p-4">
            <div className="text-zinc-400 text-xs">{t('nodeArtifactsRetentionPruned')}</div>
            <div className="mt-2 text-xl font-semibold text-zinc-100">{artifactRetentionPruned}</div>
          </div>
          <div className="brand-card-subtle rounded-2xl border border-zinc-800 p-4">
            <div className="text-zinc-400 text-xs">{t('nodeArtifactsRetentionRemaining')}</div>
            <div className="mt-2 text-xl font-semibold text-zinc-100">{artifactRetentionRemaining}</div>
          </div>
          <div className="brand-card-subtle rounded-2xl border border-zinc-800 p-4">
            <div className="text-zinc-400 text-xs">{t('nodeArtifactsRetentionKeepLatest')}</div>
            <div className="mt-2 text-xl font-semibold text-zinc-100">{Number(nodeArtifactRetention?.keep_latest || 0) || '-'}</div>
          </div>
          <div className="brand-card-subtle rounded-2xl border border-zinc-800 p-4">
            <div className="text-zinc-400 text-xs">{t('time')}</div>
            <div className="mt-2 text-sm font-medium text-zinc-100">{artifactRetentionLastRun}</div>
          </div>
        </div>
      </div>

      <div className="brand-card rounded-[30px] border border-zinc-800/80 p-6">
        <div className="flex items-center gap-2 mb-5 text-zinc-200">
          <AlertTriangle className="w-5 h-5 text-amber-400" />
          <h2 className="text-lg font-medium">{t('nodeAlerts')}</h2>
        </div>
        {topNodeAlerts.length === 0 ? (
          <div className="text-sm text-zinc-500 text-center py-8">{t('nodeAlertsEmpty')}</div>
        ) : (
          <div className="grid grid-cols-1 xl:grid-cols-2 gap-3">
            {topNodeAlerts.map((alert: any, index: number) => {
              const severity = String(alert?.severity || 'warning');
              return (
                <div key={`${alert?.node || 'node'}-${alert?.kind || index}-${index}`} className="brand-card-subtle rounded-2xl border border-zinc-800 p-4">
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="text-sm font-medium text-zinc-100 truncate">{String(alert?.title || '-')}</div>
                      <div className="text-xs text-zinc-500 mt-1 truncate">{String(alert?.node || '-')} · {String(alert?.kind || '-')}</div>
                    </div>
                    <div className={`shrink-0 rounded-full px-2.5 py-1 text-[11px] font-medium ${severity === 'critical' ? 'bg-rose-500/10 text-rose-300' : 'bg-amber-500/10 text-amber-300'}`}>
                      {severity}
                    </div>
                  </div>
                  <div className="mt-3 text-xs text-zinc-300 whitespace-pre-wrap break-words">{String(alert?.detail || '-')}</div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-2 gap-6 items-stretch">
        <div className="brand-card rounded-[30px] border border-zinc-800/80 p-6 min-h-[340px] h-full">
          <div className="flex items-center gap-2 mb-5 text-zinc-200">
            <Activity className="w-5 h-5 text-zinc-400" />
            <h2 className="text-lg font-medium">{t('taskAudit')}</h2>
          </div>
          <div className="space-y-3">
            {recentTasks.length === 0 ? (
              <div className="text-sm text-zinc-500 text-center py-10">-</div>
            ) : recentTasks.map((task: any, index: number) => (
              <div key={`${task.task_id || 'task'}-${index}`} className="brand-card-subtle rounded-2xl border border-zinc-800 p-4">
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <div className="text-sm font-medium text-zinc-200 truncate">{task.task_id || `task-${index + 1}`}</div>
                    <div className="text-xs text-zinc-500 truncate">{task.channel || '-'} · {task.source || '-'}</div>
                  </div>
                  <div className={`shrink-0 rounded-full px-2.5 py-1 text-[11px] font-medium ${String(task.status || '').toLowerCase() === 'error' ? 'bg-rose-500/10 text-rose-300' : String(task.status || '').toLowerCase() === 'running' ? 'bg-emerald-500/10 text-emerald-300' : 'bg-zinc-800 text-zinc-400'}`}>
                    {task.status || '-'}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>

        <div className="brand-card rounded-[30px] border border-zinc-800/80 p-6 min-h-[340px] h-full">
          <div className="flex items-center gap-2 mb-5 text-zinc-200">
            <Workflow className="w-5 h-5 text-zinc-400" />
            <h2 className="text-lg font-medium">{t('nodeP2P')}</h2>
          </div>
          <div className="space-y-3">
            <div className="brand-card-subtle rounded-2xl border border-zinc-800 p-4">
              <div className="text-sm font-medium text-zinc-200">{t('dashboardNodeP2PTransport')}</div>
              <div className="text-xs text-zinc-500 mt-1">{p2pTransport}</div>
            </div>
            <div className="brand-card-subtle rounded-2xl border border-zinc-800 p-4">
              <div className="text-sm font-medium text-zinc-200">{t('dashboardNodeP2PIce')}</div>
              <div className="text-xs text-zinc-500 mt-1">{`${p2pConfiguredIce} ICE · ${p2pConfiguredStun} STUN`}</div>
            </div>
            <div className="brand-card-subtle rounded-2xl border border-zinc-800 p-4">
              <div className="text-sm font-medium text-zinc-200">{t('dashboardNodeP2PHealth')}</div>
              <div className="text-xs text-zinc-500 mt-1">{t('dashboardNodeP2PDetail', { transport: p2pTransport, sessions: p2pSessions, retries: p2pRetryCount })}</div>
            </div>
          </div>
        </div>
      </div>

      <div className="brand-card rounded-[30px] border border-zinc-800/80 p-6">
        <div className="flex items-center justify-between gap-3 mb-5 flex-wrap">
          <div>
            <div className="flex items-center gap-2 text-zinc-200">
              <Workflow className="w-5 h-5 text-zinc-400" />
              <h2 className="text-lg font-medium">{t('dashboardNodeP2PSessions')}</h2>
            </div>
            <div className="text-xs text-zinc-500 mt-1">
              {t('dashboardNodeP2PDetail', { transport: p2pTransport, sessions: p2pSessions, retries: p2pRetryCount })}
            </div>
          </div>
          <div className="text-xs text-zinc-500">
            {`${p2pConfiguredIce} ICE · ${p2pConfiguredStun} STUN`}
          </div>
        </div>
        {p2pNodeSessions.length === 0 ? (
          <div className="text-sm text-zinc-500 text-center py-8">{t('dashboardNodeP2PSessionsEmpty')}</div>
        ) : (
          <div className="grid grid-cols-1 xl:grid-cols-2 gap-3">
            {p2pNodeSessions.map((session) => {
              const isOpen = session.status.toLowerCase() === 'open';
              const isConnecting = session.status.toLowerCase() === 'connecting';
              return (
                <div key={session.node} className="brand-card-subtle rounded-2xl border border-zinc-800 p-4">
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="text-sm font-medium text-zinc-100 truncate">{session.node}</div>
                      <div className="text-xs text-zinc-500 mt-1">
                        {t('dashboardNodeP2PSessionCreated')}: {session.createdAt}
                      </div>
                    </div>
                    <div className={`shrink-0 rounded-full px-2.5 py-1 text-[11px] font-medium ${isOpen ? 'bg-emerald-500/10 text-emerald-300' : isConnecting ? 'bg-amber-500/10 text-amber-300' : 'bg-rose-500/10 text-rose-300'}`}>
                      {session.status}
                    </div>
                  </div>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-3 mt-4 text-xs">
                    <div>
                      <div className="text-zinc-400">{t('dashboardNodeP2PSessionRetries')}</div>
                      <div className="text-zinc-200 mt-1">{session.retryCount}</div>
                    </div>
                    <div>
                      <div className="text-zinc-400">{t('dashboardNodeP2PSessionReady')}</div>
                      <div className="text-zinc-200 mt-1">{session.lastReadyAt}</div>
                    </div>
                    <div>
                      <div className="text-zinc-400">{t('dashboardNodeP2PSessionAttempt')}</div>
                      <div className="text-zinc-200 mt-1">{session.lastAttempt}</div>
                    </div>
                    <div>
                      <div className="text-zinc-400">{t('dashboardNodeP2PSessionError')}</div>
                      <div className={`mt-1 break-all ${session.lastError ? 'text-rose-300' : 'text-zinc-500'}`}>
                        {session.lastError || '-'}
                      </div>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      <div className="brand-card rounded-[30px] border border-zinc-800/80 p-6">
        <div className="flex items-center justify-between gap-3 mb-5 flex-wrap">
          <div>
            <div className="flex items-center gap-2 text-zinc-200">
              <Activity className="w-5 h-5 text-zinc-400" />
              <h2 className="text-lg font-medium">{t('dashboardNodeDispatches')}</h2>
            </div>
            <div className="text-xs text-zinc-500 mt-1">{t('dashboardNodeDispatchesHint')}</div>
          </div>
        </div>
        {recentNodeDispatches.length === 0 ? (
          <div className="text-sm text-zinc-500 text-center py-8">{t('dashboardNodeDispatchesEmpty')}</div>
        ) : (
          <div className="grid grid-cols-1 xl:grid-cols-2 gap-3">
            {recentNodeDispatches.map((item) => (
              <div key={item.id} className="brand-card-subtle rounded-2xl border border-zinc-800 p-4">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="text-sm font-medium text-zinc-100 truncate">{`${item.node} · ${item.action}`}</div>
                    <div className="text-xs text-zinc-500 mt-1">{item.time}</div>
                  </div>
                  <div className={`shrink-0 rounded-full px-2.5 py-1 text-[11px] font-medium ${item.ok ? 'bg-emerald-500/10 text-emerald-300' : 'bg-rose-500/10 text-rose-300'}`}>
                    {item.ok ? 'ok' : 'error'}
                  </div>
                </div>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3 mt-4 text-xs">
                  <div>
                    <div className="text-zinc-400">{t('dashboardNodeDispatchTransport')}</div>
                    <div className="text-zinc-200 mt-1">{item.usedTransport}</div>
                  </div>
                  <div>
                    <div className="text-zinc-400">{t('dashboardNodeDispatchFallback')}</div>
                    <div className="text-zinc-200 mt-1">{item.fallbackFrom || '-'}</div>
                  </div>
                  <div>
                    <div className="text-zinc-400">{t('dashboardNodeDispatchDuration')}</div>
                    <div className="text-zinc-200 mt-1">{`${item.durationMs}ms`}</div>
                  </div>
                  <div>
                    <div className="text-zinc-400">{t('dashboardNodeDispatchArtifacts')}</div>
                    <div className="text-zinc-200 mt-1">
                      {item.artifactCount > 0 ? `${item.artifactCount}${item.artifactKinds.length ? ` · ${item.artifactKinds.join(', ')}` : ''}` : '-'}
                    </div>
                  </div>
                  <div>
                    <div className="text-zinc-400">{t('dashboardNodeDispatchError')}</div>
                    <div className={`mt-1 break-all ${item.error ? 'text-rose-300' : 'text-zinc-500'}`}>
                      {item.error || '-'}
                    </div>
                  </div>
                </div>
                {item.artifacts.length > 0 && (
                  <div className="mt-4 space-y-3">
                    <div className="text-zinc-400 text-xs">{t('dashboardNodeDispatchArtifactPreview')}</div>
                    {item.artifacts.slice(0, 2).map((artifact: any, artifactIndex: number) => {
                      const kind = String(artifact?.kind || '').trim().toLowerCase();
                      const mime = String(artifact?.mime_type || '').trim().toLowerCase();
                      const isImage = kind === 'image' || mime.startsWith('image/');
                      const isVideo = kind === 'video' || mime.startsWith('video/');
                      const dataUrl = dataUrlForArtifact(artifact);
                      return (
                        <div key={`${item.id}-artifact-${artifactIndex}`} className="rounded-2xl border border-zinc-800 bg-zinc-950/40 p-3">
                          <div className="flex items-center justify-between gap-3 mb-2">
                            <div className="min-w-0">
                              <div className="text-xs font-medium text-zinc-200 truncate">{String(artifact?.name || artifact?.source_path || `artifact-${artifactIndex + 1}`)}</div>
                              <div className="text-[11px] text-zinc-500 truncate">
                                {[artifact?.kind, artifact?.mime_type, formatBytes(artifact?.size_bytes)].filter(Boolean).join(' · ')}
                              </div>
                            </div>
                            <div className="text-[11px] text-zinc-500">{String(artifact?.storage || '-')}</div>
                          </div>
                          {isImage && dataUrl && (
                            <img src={dataUrl} alt={String(artifact?.name || 'artifact')} className="max-h-48 rounded-xl border border-zinc-800 object-contain bg-black/30" />
                          )}
                          {isVideo && dataUrl && (
                            <video src={dataUrl} controls className="max-h-48 w-full rounded-xl border border-zinc-800 bg-black/30" />
                          )}
                          {!isImage && !isVideo && String(artifact?.content_text || '').trim() !== '' && (
                            <pre className="rounded-xl border border-zinc-800 bg-black/20 p-3 text-[11px] text-zinc-300 whitespace-pre-wrap overflow-auto max-h-48">{String(artifact?.content_text || '')}</pre>
                          )}
                          {!isImage && !isVideo && String(artifact?.content_text || '').trim() === '' && (
                            <div className="text-[11px] text-zinc-500 break-all">
                              {String(artifact?.source_path || artifact?.path || artifact?.url || '-')}
                            </div>
                          )}
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};

export default Dashboard;
