import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';

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
  const [errsigTopHeartbeat, setErrsigTopHeartbeat] = useState<EKGKV[]>([]);
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
      setErrsigTopHeartbeat(Array.isArray(ej.errsig_top_heartbeat) ? ej.errsig_top_heartbeat : []);
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

  return (
    <div className="h-full p-4 md:p-6 flex flex-col gap-4">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <h1 className="text-xl md:text-2xl font-semibold">EKG</h1>
        <div className="flex items-center gap-2">
          <select value={ekgWindow} onChange={(e)=>setEkgWindow(e.target.value as '6h' | '24h' | '7d')} className="bg-zinc-900 border border-zinc-700 rounded px-2 py-1 text-xs">
            <option value="6h">6h</option>
            <option value="24h">24h</option>
            <option value="7d">7d</option>
          </select>
          <button onClick={fetchData} className="px-3 py-1.5 rounded-lg bg-zinc-800 hover:bg-zinc-700 text-sm">{loading ? t('loading') : t('refresh')}</button>
        </div>
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-3 gap-3 text-xs">
        <div className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-3">
          <div className="text-zinc-500 mb-1">{t('ekgEscalations')}</div>
          <div className="text-zinc-100 text-2xl font-semibold">{escalationCount}</div>
        </div>
        <div className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-3">
          <div className="text-zinc-500 mb-1">{t('ekgSourceStats')}</div>
          <div className="space-y-1">{Object.keys(sourceStats).length === 0 ? <div className="text-zinc-500">-</div> : Object.entries(sourceStats).map(([k,v]) => <div key={k} className="text-zinc-200">{k}: <span className="text-zinc-400">{v}</span></div>)}</div>
        </div>
        <div className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-3">
          <div className="text-zinc-500 mb-1">{t('ekgChannelStats')}</div>
          <div className="space-y-1">{Object.keys(channelStats).length === 0 ? <div className="text-zinc-500">-</div> : Object.entries(channelStats).map(([k,v]) => <div key={k} className="text-zinc-200">{k}: <span className="text-zinc-400">{v}</span></div>)}</div>
        </div>
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-2 gap-3 text-xs">
        <div className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-3">
          <div className="text-zinc-500 mb-1">{t('ekgTopProvidersWorkload')}</div>
          <div className="space-y-1">{providerTopWorkload.length === 0 ? <div className="text-zinc-500">-</div> : providerTopWorkload.map((x,i)=><div key={i} className="text-zinc-200">{x.key} <span className="text-zinc-500">({Number(x.score||0).toFixed(2)})</span></div>)}</div>
        </div>
        <div className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-3">
          <div className="text-zinc-500 mb-1">{t('ekgTopProvidersAll')}</div>
          <div className="space-y-1">{providerTop.length === 0 ? <div className="text-zinc-500">-</div> : providerTop.map((x,i)=><div key={i} className="text-zinc-200">{x.key} <span className="text-zinc-500">({Number(x.score||0).toFixed(2)})</span></div>)}</div>
        </div>
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-3 gap-3 text-xs flex-1 min-h-0">
        <div className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-3 overflow-y-auto">
          <div className="text-zinc-500 mb-1">{t('ekgTopErrsigWorkload')}</div>
          <div className="space-y-1">{errsigTopWorkload.length === 0 ? <div className="text-zinc-500">-</div> : errsigTopWorkload.map((x,i)=><div key={i} className="text-zinc-200 truncate">{x.key} <span className="text-zinc-500">(x{x.count||0})</span></div>)}</div>
        </div>
        <div className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-3 overflow-y-auto">
          <div className="text-zinc-500 mb-1">{t('ekgTopErrsigHeartbeat')}</div>
          <div className="space-y-1">{errsigTopHeartbeat.length === 0 ? <div className="text-zinc-500">-</div> : errsigTopHeartbeat.map((x,i)=><div key={i} className="text-zinc-200 truncate">{x.key} <span className="text-zinc-500">(x{x.count||0})</span></div>)}</div>
        </div>
        <div className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-3 overflow-y-auto">
          <div className="text-zinc-500 mb-1">{t('ekgTopErrsigAll')}</div>
          <div className="space-y-1">{errsigTop.length === 0 ? <div className="text-zinc-500">-</div> : errsigTop.map((x,i)=><div key={i} className="text-zinc-200 truncate">{x.key} <span className="text-zinc-500">(x{x.count||0})</span></div>)}</div>
        </div>
      </div>
    </div>
  );
};

export default EKG;
