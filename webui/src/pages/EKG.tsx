import React, { useEffect, useMemo, useState } from 'react';
import { AlertTriangle, RefreshCw, Route, ServerCrash, Workflow } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';

type EKGKV = { key?: string; score?: number; count?: number };

function StatCard({
  title,
  value,
  subtitle,
  accent,
  icon,
}: {
  title: string;
  value: string | number;
  subtitle?: string;
  accent: string;
  icon: React.ReactNode;
}) {
  return (
    <div className="brand-card rounded-[28px] border border-zinc-800 p-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-[11px] uppercase tracking-widest text-zinc-500">{title}</div>
          <div className="mt-2 text-3xl font-semibold text-zinc-100">{value}</div>
          {subtitle && <div className="mt-1 text-xs text-zinc-500">{subtitle}</div>}
        </div>
        <div className={`flex h-10 w-10 items-center justify-center rounded-xl ${accent}`}>{icon}</div>
      </div>
    </div>
  );
}

function KVDistributionCard({
  title,
  data,
}: {
  title: string;
  data: Record<string, number>;
}) {
  const entries = useMemo(() => (
    Object.entries(data).sort((a, b) => b[1] - a[1])
  ), [data]);
  const maxValue = entries.length > 0 ? Math.max(...entries.map(([, value]) => value)) : 0;

  return (
    <div className="brand-card rounded-[28px] border border-zinc-800 p-5">
      <div className="mb-4 text-sm font-medium text-zinc-200">{title}</div>
      <div className="space-y-3">
        {entries.length === 0 ? (
          <div className="text-sm text-zinc-500">-</div>
        ) : entries.map(([key, value]) => (
          <div key={key} className="space-y-1">
            <div className="flex items-center justify-between gap-3 text-xs">
              <div className="truncate text-zinc-300">{key}</div>
              <div className="shrink-0 font-mono text-zinc-500">{value}</div>
            </div>
            <div className="h-2 rounded-full bg-zinc-800 overflow-hidden">
              <div
                className="ekg-bar-fill h-full rounded-full"
                style={{ width: `${maxValue > 0 ? (value / maxValue) * 100 : 0}%` }}
              />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function RankingCard({
  title,
  items,
  valueMode,
}: {
  title: string;
  items: EKGKV[];
  valueMode: 'score' | 'count';
}) {
  return (
    <div className="brand-card rounded-[28px] border border-zinc-800 p-5">
      <div className="mb-4 text-sm font-medium text-zinc-200">{title}</div>
      <div className="space-y-2">
        {items.length === 0 ? (
          <div className="text-sm text-zinc-500">-</div>
        ) : items.map((item, index) => (
          <div key={`${item.key || '-'}-${index}`} className="flex items-start gap-3 rounded-xl border border-zinc-800 bg-zinc-950/60 px-3 py-2">
            <div className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-zinc-800 text-[11px] font-semibold text-zinc-300">
              {index + 1}
            </div>
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm text-zinc-200">{item.key || '-'}</div>
              <div className="text-xs text-zinc-500">
                {valueMode === 'score'
                  ? Number(item.score || 0).toFixed(2)
                  : `x${item.count || 0}`}
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

const EKG: React.FC = () => {
  const { t } = useTranslation();
  const { q } = useAppContext();
  const [loading, setLoading] = useState(false);
  const [ekgWindow, setEkgWindow] = useState<'6h' | '24h' | '7d'>(() => {
    const saved = typeof window !== 'undefined' ? window.localStorage.getItem('taskAudit.ekgWindow') : null;
    return saved === '6h' || saved === '24h' || saved === '7d' ? saved : '24h';
  });
  const [providerTop, setProviderTop] = useState<EKGKV[]>([]);
  const [providerTopWorkload, setProviderTopWorkload] = useState<EKGKV[]>([]);
  const [errsigTop, setErrsigTop] = useState<EKGKV[]>([]);
  const [errsigTopWorkload, setErrsigTopWorkload] = useState<EKGKV[]>([]);
  const [sourceStats, setSourceStats] = useState<Record<string, number>>({});
  const [channelStats, setChannelStats] = useState<Record<string, number>>({});
  const [escalationCount, setEscalationCount] = useState(0);

  const fetchData = async () => {
    setLoading(true);
    try {
      const ekgJoin = q ? `${q}&window=${encodeURIComponent(ekgWindow)}` : `?window=${encodeURIComponent(ekgWindow)}`;
      const er = await fetch(`/webui/api/ekg_stats${ekgJoin}`);
      if (!er.ok) throw new Error(await er.text());
      const ej = await er.json();
      setProviderTop(Array.isArray(ej.provider_top) ? ej.provider_top : []);
      setProviderTopWorkload(Array.isArray(ej.provider_top_workload) ? ej.provider_top_workload : []);
      setErrsigTop(Array.isArray(ej.errsig_top) ? ej.errsig_top : []);
      setErrsigTopWorkload(Array.isArray(ej.errsig_top_workload) ? ej.errsig_top_workload : []);
      setSourceStats(ej.source_stats && typeof ej.source_stats === 'object' ? ej.source_stats : {});
      setChannelStats(ej.channel_stats && typeof ej.channel_stats === 'object' ? ej.channel_stats : {});
      setEscalationCount(Number(ej.escalation_count || 0));
    } catch (e) {
      console.error(e);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchData(); }, [q, ekgWindow]);
  useEffect(() => {
    if (typeof window !== 'undefined') window.localStorage.setItem('taskAudit.ekgWindow', ekgWindow);
  }, [ekgWindow]);

  const sourceCount = Object.keys(sourceStats).length;
  const channelCount = Object.keys(channelStats).length;
  const totalErrorHits = errsigTop.reduce((sum, item) => sum + Number(item.count || 0), 0);
  const topWorkloadProvider = providerTopWorkload[0]?.key || '-';

  return (
    <div className="h-full w-full p-4 md:p-6 xl:p-8 flex flex-col gap-6">
      <div className="flex items-start justify-between gap-4 flex-wrap">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('ekg')}</h1>
          <div className="mt-1 text-sm text-zinc-500">{t('ekgOverviewHint')}</div>
        </div>
        <div className="flex items-center gap-2">
          <select value={ekgWindow} onChange={(e) => setEkgWindow(e.target.value as '6h' | '24h' | '7d')} className="bg-zinc-900/70 border border-zinc-700 rounded-xl px-3 py-2 text-sm">
            <option value="6h">6h</option>
            <option value="24h">24h</option>
            <option value="7d">7d</option>
          </select>
          <button onClick={fetchData} className="brand-button inline-flex items-center gap-2 px-4 py-2 rounded-xl text-sm text-zinc-950">
            <RefreshCw className="w-4 h-4" />
            {loading ? t('loading') : t('refresh')}
          </button>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4">
        <StatCard title={t('ekgEscalations')} value={escalationCount} subtitle={`${ekgWindow} window`} accent="ui-pill ui-pill-warning border" icon={<AlertTriangle className="w-5 h-5" />} />
        <StatCard title={t('ekgSourceStats')} value={sourceCount} subtitle={t('ekgActiveSources')} accent="ui-pill ui-pill-info border" icon={<Workflow className="w-5 h-5" />} />
        <StatCard title={t('ekgChannelStats')} value={channelCount} subtitle={t('ekgActiveChannels')} accent="ui-pill ui-pill-accent border" icon={<Route className="w-5 h-5" />} />
        <StatCard title={t('ekgTopProvidersWorkload')} value={topWorkloadProvider} subtitle={`${t('ekgErrorsCount')} ${totalErrorHits}`} accent="ui-pill ui-pill-danger border" icon={<ServerCrash className="w-5 h-5" />} />
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-[1.1fr_1fr] gap-6 min-h-0">
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 min-h-0">
          <KVDistributionCard title={t('ekgSourceStats')} data={sourceStats} />
          <KVDistributionCard title={t('ekgChannelStats')} data={channelStats} />
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 min-h-0">
          <RankingCard title={t('ekgTopProvidersWorkload')} items={providerTopWorkload} valueMode="score" />
          <RankingCard title={t('ekgTopProvidersAll')} items={providerTop} valueMode="score" />
        </div>
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-2 gap-6 min-h-0 flex-1">
        <RankingCard title={t('ekgTopErrsigWorkload')} items={errsigTopWorkload} valueMode="count" />
        <RankingCard title={t('ekgTopErrsigAll')} items={errsigTop} valueMode="count" />
      </div>
    </div>
  );
};

export default EKG;
