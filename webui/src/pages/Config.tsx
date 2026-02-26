import React, { useEffect, useMemo, useState } from 'react';
import { RefreshCw, Save } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import RecursiveConfig from '../components/RecursiveConfig';

function setPath(obj: any, path: string, value: any) {
  const keys = path.split('.');
  const next = JSON.parse(JSON.stringify(obj || {}));
  let cur = next;
  for (let i = 0; i < keys.length - 1; i++) {
    const k = keys[i];
    if (typeof cur[k] !== 'object' || cur[k] === null) cur[k] = {};
    cur = cur[k];
  }
  cur[keys[keys.length - 1]] = value;
  return next;
}

const Config: React.FC = () => {
  const { t } = useTranslation();
  const { cfg, setCfg, cfgRaw, setCfgRaw, loadConfig, hotReloadFieldDetails, q } = useAppContext();
  const [showRaw, setShowRaw] = useState(false);
  const [basicMode, setBasicMode] = useState(true);
  const [hotOnly, setHotOnly] = useState(false);
  const [search, setSearch] = useState('');

  const hotPrefixes = useMemo(() => hotReloadFieldDetails.map((x) => String(x.path || '').replace(/\.\*$/, '')).filter(Boolean), [hotReloadFieldDetails]);

  const allTopKeys = useMemo(() => Object.keys(cfg || {}).filter(k => typeof (cfg as any)?.[k] === 'object' && (cfg as any)?.[k] !== null), [cfg]);
  const basicTopKeys = useMemo(() => {
    const preferred = ['gateway', 'providers', 'channels', 'tools', 'cron', 'agents', 'logging'];
    return preferred.filter((k) => allTopKeys.includes(k));
  }, [allTopKeys]);

  const filteredTopKeys = useMemo(() => {
    let keys = basicMode ? basicTopKeys : allTopKeys;
    if (hotOnly) {
      keys = keys.filter((k) => hotPrefixes.some((p) => p === k || p.startsWith(`${k}.`) || k.startsWith(`${p}.`)));
    }
    if (search.trim()) {
      const s = search.trim().toLowerCase();
      keys = keys.filter((k) => k.toLowerCase().includes(s));
    }
    return keys;
  }, [allTopKeys, basicTopKeys, basicMode, hotOnly, search, hotPrefixes]);

  const [selectedTop, setSelectedTop] = useState<string>('');
  const activeTop = filteredTopKeys.includes(selectedTop) ? selectedTop : (filteredTopKeys[0] || '');
  const [baseline, setBaseline] = useState<any>(null);
  const [showDiff, setShowDiff] = useState(false);

  const currentPayload = useMemo(() => {
    if (showRaw) {
      try { return JSON.parse(cfgRaw); } catch { return cfg; }
    }
    return cfg;
  }, [showRaw, cfgRaw, cfg]);

  const diffRows = useMemo(() => {
    const out: Array<{ path: string; before: any; after: any }> = [];
    const walk = (a: any, b: any, p: string) => {
      const keys = new Set([...(a && typeof a === 'object' ? Object.keys(a) : []), ...(b && typeof b === 'object' ? Object.keys(b) : [])]);
      if (keys.size === 0) {
        if (JSON.stringify(a) !== JSON.stringify(b)) out.push({ path: p || '(root)', before: a, after: b });
        return;
      }
      keys.forEach((k) => {
        const pa = p ? `${p}.${k}` : k;
        const av = a ? a[k] : undefined;
        const bv = b ? b[k] : undefined;
        const bothObj = av && bv && typeof av === 'object' && typeof bv === 'object' && !Array.isArray(av) && !Array.isArray(bv);
        if (bothObj) walk(av, bv, pa);
        else if (JSON.stringify(av) !== JSON.stringify(bv)) out.push({ path: pa, before: av, after: bv });
      });
    };
    walk(baseline || {}, currentPayload || {}, '');
    return out;
  }, [baseline, currentPayload]);

  useEffect(() => {
    if (baseline == null && cfg && Object.keys(cfg).length > 0) {
      setBaseline(JSON.parse(JSON.stringify(cfg)));
    }
  }, [cfg, baseline]);

  async function saveConfig() {
    try {
      const payload = showRaw ? JSON.parse(cfgRaw) : cfg;
      const r = await fetch(`/webui/api/config${q}`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload),
      });
      alert(await r.text());
      setBaseline(JSON.parse(JSON.stringify(payload)));
      setShowDiff(false);
    } catch (e) {
      alert('Failed to save config: ' + e);
    }
  }

  return (
    <div className="p-4 md:p-8 max-w-7xl mx-auto space-y-6 flex flex-col min-h-full">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <h1 className="text-2xl font-semibold tracking-tight">{t('configuration')}</h1>
        <div className="flex items-center gap-1 bg-zinc-900/80 p-1 rounded-lg border border-zinc-800">
          <button onClick={() => setShowRaw(false)} className={`px-4 py-1.5 text-sm font-medium rounded-md transition-all ${!showRaw ? 'bg-zinc-800 text-white shadow-sm' : 'text-zinc-400 hover:text-zinc-200'}`}>{t('form')}</button>
          <button onClick={() => setShowRaw(true)} className={`px-4 py-1.5 text-sm font-medium rounded-md transition-all ${showRaw ? 'bg-zinc-800 text-white shadow-sm' : 'text-zinc-400 hover:text-zinc-200'}`}>{t('rawJson')}</button>
        </div>
      </div>

      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div className="flex items-center gap-3 flex-wrap">
          <button onClick={async () => { await loadConfig(); setTimeout(() => setBaseline(JSON.parse(JSON.stringify(cfg))), 0); }} className="flex items-center gap-2 px-4 py-2 bg-zinc-800 hover:bg-zinc-700 rounded-lg text-sm font-medium transition-colors">
            <RefreshCw className="w-4 h-4" /> {t('reload')}
          </button>
          <button onClick={() => setShowDiff(true)} className="px-3 py-2 bg-zinc-900 border border-zinc-800 rounded-lg text-sm">差异预览</button>
          <button onClick={() => setBasicMode(v => !v)} className="px-3 py-2 bg-zinc-900 border border-zinc-800 rounded-lg text-sm">
            {basicMode ? '基础模式' : '高级模式'}
          </button>
          <label className="flex items-center gap-2 text-sm text-zinc-300">
            <input type="checkbox" checked={hotOnly} onChange={(e) => setHotOnly(e.target.checked)} />
            仅热更新字段
          </label>
          <input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="搜索分类..." className="px-3 py-2 bg-zinc-950 border border-zinc-800 rounded-lg text-sm" />
        </div>
        <button onClick={saveConfig} className="flex items-center gap-2 px-4 py-2 bg-indigo-600 hover:bg-indigo-500 text-white rounded-lg text-sm font-medium transition-colors shadow-sm">
          <Save className="w-4 h-4" /> {t('saveChanges')}
        </button>
      </div>

      <div className="bg-zinc-900/40 border border-zinc-800/80 rounded-2xl p-4">
        <div className="text-sm font-semibold text-zinc-300 mb-2">热更新字段（完整）</div>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-2 text-xs">
          {hotReloadFieldDetails.map((it) => (
            <div key={it.path} className="p-2 rounded bg-zinc-950 border border-zinc-800">
              <div className="font-mono text-zinc-200">{it.path}</div>
              <div className="text-zinc-400">{it.name || ''}{it.description ? ` · ${it.description}` : ''}</div>
            </div>
          ))}
        </div>
      </div>

      <div className="flex-1 bg-zinc-900/40 border border-zinc-800/80 rounded-2xl overflow-hidden flex flex-col shadow-sm min-h-[420px]">
        {!showRaw ? (
          <div className="flex-1 flex min-h-0">
            <aside className="w-44 md:w-56 border-r border-zinc-800 bg-zinc-950/40 p-2 md:p-3 overflow-y-auto shrink-0">
              <div className="text-xs text-zinc-500 uppercase tracking-widest mb-2 px-2">Top Level</div>
              <div className="space-y-1">
                {filteredTopKeys.map((k) => (
                  <button
                    key={k}
                    onClick={() => setSelectedTop(k)}
                    className={`w-full text-left px-3 py-2 rounded-lg text-sm transition-colors ${activeTop === k ? 'bg-indigo-500/20 text-indigo-300 border border-indigo-500/30' : 'text-zinc-300 hover:bg-zinc-800/60'}`}
                  >
                    {k}
                  </button>
                ))}
              </div>
            </aside>

            <div className="flex-1 p-4 md:p-6 overflow-y-auto">
              {activeTop ? (
                <RecursiveConfig
                  data={(cfg as any)?.[activeTop] || {}}
                  labels={t('configLabels', { returnObjects: true }) as Record<string, string>}
                  path={activeTop}
                  hotPaths={hotReloadFieldDetails.map((x) => x.path)}
                  onlyHot={hotOnly}
                  onChange={(path, val) => setCfg(v => setPath(v, path, val))}
                />
              ) : (
                <div className="text-zinc-500 text-sm">No config groups found.</div>
              )}
            </div>
          </div>
        ) : (
          <textarea
            value={cfgRaw}
            onChange={(e) => setCfgRaw(e.target.value)}
            className="flex-1 w-full bg-zinc-950 p-6 font-mono text-sm text-zinc-300 focus:outline-none resize-none"
            spellCheck={false}
          />
        )}
      </div>

      {showDiff && (
        <div className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center p-4">
          <div className="w-full max-w-4xl max-h-[85vh] bg-zinc-950 border border-zinc-800 rounded-2xl overflow-hidden flex flex-col">
            <div className="px-4 py-3 border-b border-zinc-800 flex items-center justify-between">
              <div className="font-semibold">配置差异预览（{diffRows.length}项）</div>
              <button className="px-3 py-1 rounded bg-zinc-800" onClick={() => setShowDiff(false)}>关闭</button>
            </div>
            <div className="overflow-auto text-xs">
              <table className="w-full">
                <thead className="sticky top-0 bg-zinc-900 text-zinc-300">
                  <tr>
                    <th className="text-left p-2">Path</th>
                    <th className="text-left p-2">Before</th>
                    <th className="text-left p-2">After</th>
                  </tr>
                </thead>
                <tbody>
                  {diffRows.map((r, i) => (
                    <tr key={i} className="border-t border-zinc-900 align-top">
                      <td className="p-2 font-mono text-zinc-400">{r.path}</td>
                      <td className="p-2 text-zinc-300 break-all">{JSON.stringify(r.before)}</td>
                      <td className="p-2 text-emerald-300 break-all">{JSON.stringify(r.after)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

export default Config;
