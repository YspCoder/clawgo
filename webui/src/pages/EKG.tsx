import React, { useEffect, useMemo, useState } from 'react';
import { AlertTriangle, RefreshCw, Route, ServerCrash, Workflow } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { FixedButton } from '../components/Button';
import EKGDistributionCard from '../components/ekg/EKGDistributionCard';
import EKGRankingCard from '../components/ekg/EKGRankingCard';
import { SelectField } from '../components/FormControls';
import MetricPanel from '../components/MetricPanel';

type EKGKV = { key?: string; score?: number; count?: number };

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
          <h1 className="ui-text-primary text-2xl font-semibold tracking-tight">{t('ekg')}</h1>
          <div className="ui-text-muted mt-1 text-sm">{t('ekgOverviewHint')}</div>
        </div>
        <div className="flex items-center gap-2">
          <SelectField value={ekgWindow} onChange={(e) => setEkgWindow(e.target.value as '6h' | '24h' | '7d')} className="h-12 min-w-[96px]">
            <option value="6h">6h</option>
            <option value="24h">24h</option>
            <option value="7d">7d</option>
          </SelectField>
          <FixedButton
            onClick={fetchData}
            variant="primary"
            radius="xl"
            label={loading ? t('loading') : t('refresh')}
          >
            <RefreshCw className="w-4 h-4" />
          </FixedButton>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4">
        <MetricPanel
          title={t('ekgEscalations')}
          value={escalationCount}
          subtitle={t('ekgWindowLabel', { window: ekgWindow })}
          icon={<AlertTriangle className="w-5 h-5" />}
          iconContainerClassName="flex h-10 w-10 items-center justify-center rounded-xl ui-pill ui-pill-warning border"
          layout="split"
        />
        <MetricPanel
          title={t('ekgSourceStats')}
          value={sourceCount}
          subtitle={t('ekgActiveSources')}
          icon={<Workflow className="w-5 h-5" />}
          iconContainerClassName="flex h-10 w-10 items-center justify-center rounded-xl ui-pill ui-pill-info border"
          layout="split"
        />
        <MetricPanel
          title={t('ekgChannelStats')}
          value={channelCount}
          subtitle={t('ekgActiveChannels')}
          icon={<Route className="w-5 h-5" />}
          iconContainerClassName="flex h-10 w-10 items-center justify-center rounded-xl ui-pill ui-pill-accent border"
          layout="split"
        />
        <MetricPanel
          title={t('ekgTopProvidersWorkload')}
          value={topWorkloadProvider}
          subtitle={`${t('ekgErrorsCount')} ${totalErrorHits}`}
          icon={<ServerCrash className="w-5 h-5" />}
          iconContainerClassName="flex h-10 w-10 items-center justify-center rounded-xl ui-pill ui-pill-danger border"
          layout="split"
        />
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-[1.1fr_1fr] gap-6 min-h-0">
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 min-h-0">
          <EKGDistributionCard title={t('ekgSourceStats')} data={sourceStats} />
          <EKGDistributionCard title={t('ekgChannelStats')} data={channelStats} />
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 min-h-0">
          <EKGRankingCard title={t('ekgTopProvidersWorkload')} items={providerTopWorkload} valueMode="score" />
          <EKGRankingCard title={t('ekgTopProvidersAll')} items={providerTop} valueMode="score" />
        </div>
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-2 gap-6 min-h-0 flex-1">
        <EKGRankingCard title={t('ekgTopErrsigWorkload')} items={errsigTopWorkload} valueMode="count" />
        <EKGRankingCard title={t('ekgTopErrsigAll')} items={errsigTop} valueMode="count" />
      </div>
    </div>
  );
};

export default EKG;
