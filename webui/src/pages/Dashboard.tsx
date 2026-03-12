import React, { useMemo } from 'react';
import { RefreshCw, Activity, MessageSquare, Wrench, Sparkles, AlertTriangle, Workflow } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import ArtifactPreviewCard from '../components/data-display/ArtifactPreviewCard';
import StatCard from '../components/data-display/StatCard';
import { FixedButton } from '../components/ui/Button';
import DetailGrid from '../components/data-display/DetailGrid';
import InfoTile from '../components/data-display/InfoTile';
import InsetCard from '../components/data-display/InsetCard';
import MetricPanel from '../components/data-display/MetricPanel';
import PageHeader from '../components/layout/PageHeader';
import SectionPanel from '../components/layout/SectionPanel';
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


    </div>
  );
};

export default Dashboard;
