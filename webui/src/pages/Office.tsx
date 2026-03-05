import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import OfficeScene, { OfficeMainState, OfficeNodeState } from '../components/office/OfficeScene';

const IPV4_PATTERN = /\b(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})\b/g;

function maskIPv4(text: string | undefined): string {
  const raw = String(text || '');
  return raw.replace(IPV4_PATTERN, (ip) => {
    const parts = ip.split('.');
    if (parts.length !== 4) return ip;
    const valid = parts.every((p) => {
      const n = Number(p);
      return Number.isInteger(n) && n >= 0 && n <= 255;
    });
    if (!valid) return ip;
    return `${parts[0]}.${parts[1]}.**.**`;
  });
}

type OfficeStats = {
  running?: number;
  waiting?: number;
  blocked?: number;
  error?: number;
  success?: number;
  suppressed?: number;
  online_nodes?: number;
  ekg_error_5m?: number;
};

type OfficePayload = {
  ok?: boolean;
  time?: string;
  main?: OfficeMainState;
  nodes?: OfficeNodeState[];
  stats?: OfficeStats;
};

const Office: React.FC = () => {
  const { t } = useTranslation();
  const { q } = useAppContext();
  const [loading, setLoading] = useState(false);
  const [payload, setPayload] = useState<OfficePayload>({});

  const fetchState = useCallback(async () => {
    setLoading(true);
    try {
      const sep = q ? '&' : '?';
      const r = await fetch(`/webui/api/office_state${q}${sep}_=${Date.now()}`, { cache: 'no-store' });
      if (!r.ok) throw new Error(await r.text());
      const j = (await r.json()) as OfficePayload;
      setPayload(j || {});
    } catch (e) {
      console.error(e);
    } finally {
      setLoading(false);
    }
  }, [q]);

  useEffect(() => {
    fetchState();
    const timer = setInterval(fetchState, 3000);
    return () => clearInterval(timer);
  }, [fetchState]);

  const main = payload.main || {};
  const nodes = Array.isArray(payload.nodes) ? payload.nodes : [];
  const stats = payload.stats || {};
  const safeMain = useMemo(
    () => ({
      ...main,
      id: maskIPv4(main.id),
      name: maskIPv4(main.name),
      detail: maskIPv4(main.detail),
      task_id: maskIPv4(main.task_id),
    }),
    [main]
  );
  const safeNodes = useMemo(
    () =>
      nodes.map((n) => ({
        ...n,
        id: maskIPv4(n.id),
        name: maskIPv4(n.name),
        detail: maskIPv4(n.detail),
      })),
    [nodes]
  );

  const cards = useMemo(
    () => [
      { label: t('statusRunning'), value: Number(stats.running || 0) },
      { label: t('statusWaiting'), value: Number(stats.waiting || 0) },
      { label: t('statusBlocked'), value: Number(stats.blocked || 0) },
      { label: t('statusError'), value: Number(stats.error || 0) },
      { label: t('nodesOnline'), value: Number(stats.online_nodes || 0) },
      { label: t('officeEkgErr5m'), value: Number(stats.ekg_error_5m || 0) },
    ],
    [stats, t]
  );

  return (
    <div className="p-4 md:p-6 space-y-4">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <div>
          <h1 className="text-xl md:text-2xl font-semibold">{t('office')}</h1>
          <div className="text-xs text-zinc-500 mt-1">
            {t('officeMainState')}: {safeMain.state || 'idle'} {safeMain.task_id ? `· ${safeMain.task_id}` : ''}
          </div>
        </div>
        <button onClick={fetchState} className="px-3 py-1.5 rounded-lg bg-zinc-800 hover:bg-zinc-700 text-sm flex items-center gap-2">
          <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
          {loading ? t('loading') : t('refresh')}
        </button>
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-3 gap-4">
        <div className="xl:col-span-2">
          <OfficeScene main={safeMain} nodes={safeNodes} />
          <div className="mt-2 text-xs text-zinc-400 bg-zinc-900/40 border border-zinc-800 rounded-lg px-3 py-2">
            {safeMain.detail || t('officeNoDetail')}
          </div>
        </div>
        <div className="space-y-3">
          <div className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-3">
            <div className="text-zinc-500 text-xs mb-2">{t('officeSceneStats')}</div>
            <div className="grid grid-cols-2 gap-2">
              {cards.map((c) => (
                <div key={c.label} className="rounded-lg bg-zinc-950/70 border border-zinc-800 px-2 py-2">
                  <div className="text-[10px] text-zinc-500">{c.label}</div>
                  <div className="text-sm font-semibold text-zinc-100">{c.value}</div>
                </div>
              ))}
            </div>
          </div>

          <div className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-3">
            <div className="text-zinc-500 text-xs mb-2">{t('officeNodeList')}</div>
            <div className="max-h-64 overflow-auto space-y-1.5">
              {safeNodes.length === 0 ? (
                <div className="text-zinc-500 text-sm">{t('officeNoNodes')}</div>
              ) : (
                safeNodes.slice(0, 20).map((n, i) => (
                  <div key={`${n.id || 'node'}-${i}`} className="rounded-md bg-zinc-950/70 border border-zinc-800 px-2 py-1.5">
                    <div className="text-xs text-zinc-200 truncate">{n.name || n.id || 'node'}</div>
                    <div className="text-[11px] text-zinc-500 truncate">
                      {n.state || 'idle'} · {n.zone || 'breakroom'}
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

export default Office;
