import React, { useMemo } from 'react';
import { RefreshCw, Activity, MessageSquare, Wrench, Sparkles, AlertTriangle, Workflow } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import ArtifactPreviewCard from '../components/ArtifactPreviewCard';
import StatCard from '../components/StatCard';
import { FixedButton } from '../components/Button';
import DetailGrid from '../components/DetailGrid';
import InfoTile from '../components/InfoTile';
import InsetCard from '../components/InsetCard';
import MetricPanel from '../components/MetricPanel';
import PageHeader from '../components/PageHeader';
import SectionPanel from '../components/SectionPanel';
import { dataUrlForArtifact, formatArtifactBytes } from '../utils/artifacts';
import { formatRuntimeTime } from '../utils/runtime';

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
      <PageHeader
        title={t('dashboard')}
        subtitle={
          <>
            {t('gateway')}: <span className="font-mono text-zinc-300">{gatewayVersion}</span>
            {' · '}
            {t('webui')}: <span className="font-mono text-zinc-300">{webuiVersion}</span>
          </>
        }
        actions={
          <FixedButton onClick={refreshAll} variant="primary" noShrink label={t('refreshAll')}>
            <RefreshCw className="w-4 h-4" />
          </FixedButton>
        }
      />

      <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-6 gap-4">
        <StatCard title={t('gatewayStatus')} value={isGatewayOnline ? t('online') : t('offline')} icon={<Activity className={`w-6 h-6 ${isGatewayOnline ? 'ui-icon-success' : 'ui-text-danger'}`} />} />
        <StatCard title={t('activeSessions')} value={sessions.length} icon={<MessageSquare className="ui-icon-info w-6 h-6" />} />
        <StatCard title={t('skills')} value={skills.length} icon={<Sparkles className="ui-pill-danger w-6 h-6 rounded-full p-1" />} />
        <StatCard title={t('subagentsRuntime')} value={subagentCount} icon={<Wrench className="ui-icon-info w-6 h-6" />} />
        <StatCard title={t('taskAudit')} value={recentTasks.length} icon={<Activity className="ui-icon-warning w-6 h-6" />} />
        <StatCard title={t('nodeP2P')} value={p2pEnabled ? `${p2pSessions} · ${p2pTransport}` : t('disabled')} icon={<Workflow className="ui-pill-accent w-6 h-6 rounded-full p-1" />} />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <MetricPanel
          icon={<AlertTriangle className="ui-icon-warning w-4 h-4" />}
          subtitle={`${t('dashboardTopErrorSignature')}: ${ekgTopErrSig}`}
          title={t('ekgEscalations')}
          value={ekgEscalationCount}
        />
        <MetricPanel
          icon={<Sparkles className="ui-icon-info w-4 h-4" />}
          subtitle={t('dashboardWorkloadSnapshot')}
          title={t('ekgTopProvidersWorkload')}
          value={ekgTopProvider}
          valueClassName="ui-text-primary text-2xl font-semibold truncate"
        />
        <MetricPanel
          icon={<Activity className="ui-text-danger w-4 h-4" />}
          subtitle={t('dashboardRecentFailedTasks')}
          title={t('taskAudit')}
          value={recentFailures.length}
        />
      </div>

      <SectionPanel
        title={t('nodeArtifactsRetention')}
        subtitle={t('nodeArtifactsRetentionHint')}
        icon={<Activity className="w-5 h-5 text-zinc-400" />}
        actions={
          <div className={`ui-pill rounded-full px-2.5 py-1 text-[11px] font-medium ${artifactRetentionEnabled ? 'ui-pill-success' : 'ui-pill-neutral'}`}>
            {artifactRetentionEnabled ? t('enabled') : t('disabled')}
          </div>
        }
      >
        <div className="grid grid-cols-1 md:grid-cols-4 gap-3 text-sm">
          <InfoTile label={t('nodeArtifactsRetentionPruned')} className="brand-card-subtle rounded-2xl border border-zinc-800" contentClassName="text-xl font-semibold text-zinc-100">
            {artifactRetentionPruned}
          </InfoTile>
          <InfoTile label={t('nodeArtifactsRetentionRemaining')} className="brand-card-subtle rounded-2xl border border-zinc-800" contentClassName="text-xl font-semibold text-zinc-100">
            {artifactRetentionRemaining}
          </InfoTile>
          <InfoTile label={t('nodeArtifactsRetentionKeepLatest')} className="brand-card-subtle rounded-2xl border border-zinc-800" contentClassName="text-xl font-semibold text-zinc-100">
            {Number(nodeArtifactRetention?.keep_latest || 0) || '-'}
          </InfoTile>
          <InfoTile label={t('time')} className="brand-card-subtle rounded-2xl border border-zinc-800" contentClassName="text-sm font-medium text-zinc-100">
            {artifactRetentionLastRun}
          </InfoTile>
        </div>
      </SectionPanel>

      <SectionPanel title={t('nodeAlerts')} icon={<AlertTriangle className="w-5 h-5 text-amber-400" />}>
        {topNodeAlerts.length === 0 ? (
          <div className="text-sm text-zinc-500 text-center py-8">{t('nodeAlertsEmpty')}</div>
        ) : (
          <div className="grid grid-cols-1 xl:grid-cols-2 gap-3">
            {topNodeAlerts.map((alert: any, index: number) => {
              const severity = String(alert?.severity || 'warning');
              return (
                <InsetCard key={`${alert?.node || 'node'}-${alert?.kind || index}-${index}`}>
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="text-sm font-medium text-zinc-100 truncate">{String(alert?.title || '-')}</div>
                      <div className="text-xs text-zinc-500 mt-1 truncate">{String(alert?.node || '-')} · {String(alert?.kind || '-')}</div>
                    </div>
                    <div className={`ui-pill shrink-0 rounded-full px-2.5 py-1 text-[11px] font-medium ${severity === 'critical' ? 'ui-pill-danger' : 'ui-pill-warning'}`}>
                      {severity}
                    </div>
                  </div>
                  <div className="mt-3 text-xs text-zinc-300 whitespace-pre-wrap break-words">{String(alert?.detail || '-')}</div>
                </InsetCard>
              );
            })}
          </div>
        )}
      </SectionPanel>

      <div className="grid grid-cols-1 xl:grid-cols-2 gap-6 items-stretch">
        <SectionPanel title={t('taskAudit')} icon={<Activity className="w-5 h-5 text-zinc-400" />} className="min-h-[340px] h-full">
          <div className="space-y-3">
            {recentTasks.length === 0 ? (
              <div className="text-sm text-zinc-500 text-center py-10">-</div>
            ) : recentTasks.map((task: any, index: number) => (
              <InsetCard key={`${task.task_id || 'task'}-${index}`}>
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <div className="text-sm font-medium text-zinc-200 truncate">{task.task_id || `task-${index + 1}`}</div>
                    <div className="text-xs text-zinc-500 truncate">{task.channel || '-'} · {task.source || '-'}</div>
                  </div>
                  <div className={`ui-pill shrink-0 rounded-full px-2.5 py-1 text-[11px] font-medium ${String(task.status || '').toLowerCase() === 'error' ? 'ui-pill-danger' : String(task.status || '').toLowerCase() === 'running' ? 'ui-pill-success' : 'ui-pill-neutral'}`}>
                    {task.status || '-'}
                  </div>
                </div>
              </InsetCard>
            ))}
          </div>
        </SectionPanel>

        <SectionPanel title={t('nodeP2P')} icon={<Workflow className="w-5 h-5 text-zinc-400" />} className="min-h-[340px] h-full">
          <div className="space-y-3">
            <InfoTile label={t('dashboardNodeP2PTransport')} className="brand-card-subtle rounded-2xl border border-zinc-800" labelClassName="text-sm font-medium text-zinc-200 normal-case tracking-normal" contentClassName="text-xs text-zinc-500 mt-1">
              {p2pTransport}
            </InfoTile>
            <InfoTile label={t('dashboardNodeP2PIce')} className="brand-card-subtle rounded-2xl border border-zinc-800" labelClassName="text-sm font-medium text-zinc-200 normal-case tracking-normal" contentClassName="text-xs text-zinc-500 mt-1">
              {`${p2pConfiguredIce} ICE · ${p2pConfiguredStun} STUN`}
            </InfoTile>
            <InfoTile label={t('dashboardNodeP2PHealth')} className="brand-card-subtle rounded-2xl border border-zinc-800" labelClassName="text-sm font-medium text-zinc-200 normal-case tracking-normal" contentClassName="text-xs text-zinc-500 mt-1">
              {t('dashboardNodeP2PDetail', { transport: p2pTransport, sessions: p2pSessions, retries: p2pRetryCount })}
            </InfoTile>
          </div>
        </SectionPanel>
      </div>

      <SectionPanel
        title={t('dashboardNodeP2PSessions')}
        subtitle={t('dashboardNodeP2PDetail', { transport: p2pTransport, sessions: p2pSessions, retries: p2pRetryCount })}
        icon={<Workflow className="w-5 h-5 text-zinc-400" />}
        actions={`${p2pConfiguredIce} ICE · ${p2pConfiguredStun} STUN`}
      >
        {p2pNodeSessions.length === 0 ? (
          <div className="text-sm text-zinc-500 text-center py-8">{t('dashboardNodeP2PSessionsEmpty')}</div>
        ) : (
          <div className="grid grid-cols-1 xl:grid-cols-2 gap-3">
            {p2pNodeSessions.map((session) => {
              const isOpen = session.status.toLowerCase() === 'open';
              const isConnecting = session.status.toLowerCase() === 'connecting';
              return (
                <InsetCard key={session.node}>
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="text-sm font-medium text-zinc-100 truncate">{session.node}</div>
                      <div className="text-xs text-zinc-500 mt-1">
                        {t('dashboardNodeP2PSessionCreated')}: {session.createdAt}
                      </div>
                    </div>
                  <div className={`ui-pill shrink-0 rounded-full px-2.5 py-1 text-[11px] font-medium ${isOpen ? 'ui-pill-success' : isConnecting ? 'ui-pill-warning' : 'ui-pill-danger'}`}>
                    {session.status}
                  </div>
                </div>
                  <DetailGrid
                    className="mt-4 text-xs"
                    columnsClassName="grid-cols-1 md:grid-cols-2"
                    items={[
                      {
                        key: 'retry',
                        label: t('dashboardNodeP2PSessionRetries'),
                        value: session.retryCount,
                        valueClassName: 'text-zinc-200 mt-1',
                      },
                      {
                        key: 'ready',
                        label: t('dashboardNodeP2PSessionReady'),
                        value: session.lastReadyAt,
                        valueClassName: 'text-zinc-200 mt-1',
                      },
                      {
                        key: 'attempt',
                        label: t('dashboardNodeP2PSessionAttempt'),
                        value: session.lastAttempt,
                        valueClassName: 'text-zinc-200 mt-1',
                      },
                      {
                        key: 'error',
                        label: t('dashboardNodeP2PSessionError'),
                        value: session.lastError || '-',
                        valueClassName: `mt-1 break-all ${session.lastError ? 'text-rose-300' : 'text-zinc-500'}`,
                      },
                    ]}
                  />
                </InsetCard>
              );
            })}
          </div>
        )}
      </SectionPanel>

      <SectionPanel
        title={t('dashboardNodeDispatches')}
        subtitle={t('dashboardNodeDispatchesHint')}
        icon={<Activity className="w-5 h-5 text-zinc-400" />}
      >
        {recentNodeDispatches.length === 0 ? (
          <div className="text-sm text-zinc-500 text-center py-8">{t('dashboardNodeDispatchesEmpty')}</div>
        ) : (
          <div className="grid grid-cols-1 xl:grid-cols-2 gap-3">
            {recentNodeDispatches.map((item) => (
              <InsetCard key={item.id}>
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="text-sm font-medium text-zinc-100 truncate">{`${item.node} · ${item.action}`}</div>
                    <div className="text-xs text-zinc-500 mt-1">{item.time}</div>
                  </div>
                  <div className={`ui-pill shrink-0 rounded-full px-2.5 py-1 text-[11px] font-medium ${item.ok ? 'ui-pill-success' : 'ui-pill-danger'}`}>
                    {item.ok ? 'ok' : 'error'}
                  </div>
                </div>
                <DetailGrid
                  className="mt-4 text-xs"
                  columnsClassName="grid-cols-1 md:grid-cols-2"
                  items={[
                    {
                      key: 'transport',
                      label: t('dashboardNodeDispatchTransport'),
                      value: item.usedTransport,
                      valueClassName: 'text-zinc-200 mt-1',
                    },
                    {
                      key: 'fallback',
                      label: t('dashboardNodeDispatchFallback'),
                      value: item.fallbackFrom || '-',
                      valueClassName: 'text-zinc-200 mt-1',
                    },
                    {
                      key: 'duration',
                      label: t('dashboardNodeDispatchDuration'),
                      value: `${item.durationMs}ms`,
                      valueClassName: 'text-zinc-200 mt-1',
                    },
                    {
                      key: 'artifacts',
                      label: t('dashboardNodeDispatchArtifacts'),
                      value: item.artifactCount > 0 ? `${item.artifactCount}${item.artifactKinds.length ? ` · ${item.artifactKinds.join(', ')}` : ''}` : '-',
                      valueClassName: 'text-zinc-200 mt-1',
                    },
                    {
                      key: 'error',
                      label: t('dashboardNodeDispatchError'),
                      value: item.error || '-',
                      valueClassName: `mt-1 break-all ${item.error ? 'text-rose-300' : 'text-zinc-500'}`,
                    },
                  ]}
                />
                {item.artifacts.length > 0 && (
                  <div className="mt-4 space-y-3">
                    <div className="text-zinc-400 text-xs">{t('dashboardNodeDispatchArtifactPreview')}</div>
                    {item.artifacts.slice(0, 2).map((artifact: any, artifactIndex: number) => {
                      const dataUrl = dataUrlForArtifact(artifact);
                      return (
                        <ArtifactPreviewCard
                          key={`${item.id}-artifact-${artifactIndex}`}
                          artifact={artifact}
                          dataUrl={dataUrl}
                          fallbackName={`artifact-${artifactIndex + 1}`}
                          formatBytes={formatArtifactBytes}
                        />
                      );
                    })}
                  </div>
                )}
              </InsetCard>
            ))}
          </div>
        )}
      </SectionPanel>
    </div>
  );
};

export default Dashboard;
