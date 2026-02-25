import React, { useMemo } from 'react';
import { RefreshCw, Activity, MessageSquare, Clock, Server, CheckCircle2, Pause } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import StatCard from '../components/StatCard';

const Dashboard: React.FC = () => {
  const { t } = useTranslation();
  const { isGatewayOnline, sessions, cron, nodes, refreshAll } = useAppContext();

  const onlineNodes = useMemo(() => {
    try {
      const arr = JSON.parse(nodes);
      return Array.isArray(arr) ? arr.filter((n: any) => n?.online).length : 0;
    } catch {
      return 0;
    }
  }, [nodes]);

  return (
    <div className="p-8 max-w-7xl mx-auto space-y-8">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold tracking-tight">{t('dashboard')}</h1>
        <button onClick={refreshAll} className="flex items-center gap-2 px-4 py-2 bg-zinc-800 hover:bg-zinc-700 rounded-lg text-sm font-medium transition-colors">
          <RefreshCw className="w-4 h-4" /> {t('refreshAll')}
        </button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard title={t('gatewayStatus')} value={isGatewayOnline ? t('online') : t('offline')} icon={<Activity className={`w-6 h-6 ${isGatewayOnline ? 'text-emerald-400' : 'text-red-400'}`} />} />
        <StatCard title={t('activeSessions')} value={sessions.length} icon={<MessageSquare className="w-6 h-6 text-blue-400" />} />
        <StatCard title={t('cronJobs')} value={cron.length} icon={<Clock className="w-6 h-6 text-purple-400" />} />
        <StatCard title={t('nodesOnline')} value={onlineNodes} icon={<Server className="w-6 h-6 text-amber-400" />} />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div className="bg-zinc-900/40 border border-zinc-800/80 rounded-2xl p-6">
          <h2 className="text-lg font-medium mb-4 flex items-center gap-2 text-zinc-200">
            <Clock className="w-5 h-5 text-zinc-400"/> {t('recentCron')}
          </h2>
          <div className="space-y-3">
            {cron.slice(0, 5).map(j => (
              <div key={j.id} className="flex items-center justify-between p-3 bg-zinc-950/50 rounded-xl border border-zinc-800/50">
                <span className="font-medium text-sm text-zinc-300">{j.name || j.id}</span>
                {j.enabled ? (
                  <span className="flex items-center gap-1.5 text-xs font-medium text-emerald-400 bg-emerald-400/10 px-2.5 py-1 rounded-full">
                    <CheckCircle2 className="w-3.5 h-3.5"/> {t('active')}
                  </span>
                ) : (
                  <span className="flex items-center gap-1.5 text-xs font-medium text-zinc-400 bg-zinc-800 px-2.5 py-1 rounded-full">
                    <Pause className="w-3.5 h-3.5"/> {t('paused')}
                  </span>
                )}
              </div>
            ))}
            {cron.length === 0 && <div className="text-sm text-zinc-500 text-center py-8">{t('noCronJobs')}</div>}
          </div>
        </div>
        <div className="bg-zinc-900/40 border border-zinc-800/80 rounded-2xl p-6 flex flex-col h-[400px]">
          <h2 className="text-lg font-medium mb-4 flex items-center gap-2 text-zinc-200">
            <Server className="w-5 h-5 text-zinc-400"/> {t('nodesSnapshot')}
          </h2>
          <div className="flex-1 bg-zinc-950/80 rounded-xl border border-zinc-800/50 p-4 overflow-auto">
            {(() => {
              try {
                const parsedNodes = JSON.parse(nodes);
                if (!Array.isArray(parsedNodes) || parsedNodes.length === 0) {
                  return <div className="text-sm text-zinc-500 text-center py-8">{t('noNodes')}</div>;
                }
                return (
                  <div className="space-y-3">
                    {parsedNodes.map((node: any, i: number) => (
                      <div key={node.id || i} className="flex items-center justify-between p-3 bg-zinc-900/50 rounded-xl border border-zinc-800/50">
                        <div className="flex items-center gap-3">
                          <div className={`w-2 h-2 rounded-full ${node.online ? 'bg-emerald-500' : 'bg-red-500'}`} />
                          <span className="font-medium text-sm text-zinc-300">{node.name || node.id || `Node ${i + 1}`}</span>
                        </div>
                        <span className="text-xs font-mono text-zinc-500">{node.version || node.ip || ''}</span>
                      </div>
                    ))}
                  </div>
                );
              } catch (e) {
                return <pre className="font-mono text-[13px] leading-relaxed text-zinc-400">{nodes}</pre>;
              }
            })()}
          </div>
        </div>
      </div>
    </div>
  );
};

export default Dashboard;
