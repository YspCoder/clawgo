import React, { useMemo } from 'react';
import { RefreshCw, Activity, MessageSquare, Clock, Server, Wrench, Sparkles, AlertTriangle, Workflow } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import StatCard from '../components/StatCard';

const Dashboard: React.FC = () => {
  const { t } = useTranslation();
  const {
    isGatewayOnline,
    sessions,
    cron,
    nodes,
    refreshAll,
    gatewayVersion,
    webuiVersion,
    skills,
    cfg,
    taskQueueItems,
    ekgSummary,
  } = useAppContext();

  const parsedNodes = useMemo(() => {
    try {
      const arr = JSON.parse(nodes);
      return Array.isArray(arr) ? arr : [];
    } catch {
      return [];
    }
  }, [nodes]);

  const onlineNodes = useMemo(
    () => parsedNodes.filter((n: any) => n?.online).length,
    [parsedNodes],
  );

  const pausedCron = useMemo(
    () => cron.filter((job) => !job.enabled).length,
    [cron],
  );

  const enabledCron = useMemo(
    () => cron.filter((job) => job.enabled).length,
    [cron],
  );

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

      <div className="grid grid-cols-1 sm:grid-cols-2 2xl:grid-cols-6 gap-4">
        <StatCard title={t('gatewayStatus')} value={isGatewayOnline ? t('online') : t('offline')} icon={<Activity className={`w-6 h-6 ${isGatewayOnline ? 'text-emerald-400' : 'text-red-400'}`} />} />
        <StatCard title={t('activeSessions')} value={sessions.length} icon={<MessageSquare className="w-6 h-6 text-blue-400" />} />
        <StatCard title={t('nodesOnline')} value={onlineNodes} icon={<Server className="w-6 h-6 text-amber-400" />} />
        <StatCard title={t('cronJobs')} value={cron.length} icon={<Clock className="w-6 h-6 text-purple-400" />} />
        <StatCard title={t('skills')} value={skills.length} icon={<Sparkles className="w-6 h-6 text-pink-400" />} />
        <StatCard title={t('subagentsRuntime')} value={subagentCount} icon={<Wrench className="w-6 h-6 text-cyan-400" />} />
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
            <div className="text-sm font-medium">{t('ekgTopProvidersWorkload')}</div>
          </div>
          <div className="text-2xl font-semibold text-zinc-100 truncate">{ekgTopProvider}</div>
          <div className="mt-2 text-xs text-zinc-500">{t('dashboardWorkloadSnapshot')}</div>
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

      <div className="grid grid-cols-1 xl:grid-cols-2 gap-6 items-stretch">
        <div className="brand-card rounded-[30px] border border-zinc-800/80 p-6 min-h-[340px] h-full">
          <div className="flex items-center gap-2 mb-5 text-zinc-200">
            <Clock className="w-5 h-5 text-zinc-400" />
            <h2 className="text-lg font-medium">{t('recentCron')}</h2>
          </div>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="brand-card-subtle rounded-2xl border border-zinc-800 p-4 min-h-[104px]">
              <div className="text-[11px] uppercase tracking-widest text-zinc-500">{t('active')}</div>
              <div className="mt-2 text-2xl font-semibold text-zinc-100">{enabledCron}</div>
            </div>
            <div className="brand-card-subtle rounded-2xl border border-zinc-800 p-4 min-h-[104px]">
              <div className="text-[11px] uppercase tracking-widest text-zinc-500">{t('paused')}</div>
              <div className="mt-2 text-2xl font-semibold text-zinc-100">{pausedCron}</div>
            </div>
          </div>
          <div className="h-[180px] flex items-center justify-center text-sm text-zinc-500">
            {cron.length === 0 ? t('noCronJobs') : `${cron.length} ${t('cronJobs')}`}
          </div>
        </div>
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
            <AlertTriangle className="w-5 h-5 text-zinc-400" />
            <h2 className="text-lg font-medium">{t('statusError')}</h2>
          </div>
          <div className="space-y-3">
            {recentFailures.length === 0 ? (
              <div className="text-sm text-zinc-500 text-center py-10">-</div>
            ) : recentFailures.map((task: any, index: number) => (
              <div key={`${task.task_id || 'failed'}-${index}`} className="brand-card-subtle rounded-2xl border border-zinc-800 p-4">
                <div className="text-sm font-medium text-zinc-200 truncate">{task.task_id || `task-${index + 1}`}</div>
                <div className="text-xs text-zinc-500 truncate mt-1">{task.source || '-'} · {task.channel || '-'}</div>
                <div className="text-xs text-rose-300 mt-2 break-all">{task.error || task.block_reason || '-'}</div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
};

export default Dashboard;
