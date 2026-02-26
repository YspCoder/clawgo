import React, { useMemo, useState } from 'react';
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
  const topKeys = useMemo(() => Object.keys(cfg || {}).filter(k => typeof (cfg as any)?.[k] === 'object' && (cfg as any)?.[k] !== null), [cfg]);
  const [selectedTop, setSelectedTop] = useState<string>('');

  const activeTop = selectedTop || topKeys[0] || '';

  async function saveConfig() {
    try {
      const payload = showRaw ? JSON.parse(cfgRaw) : cfg;
      const r = await fetch(`/webui/api/config${q}`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload),
      });
      alert(await r.text());
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

      <div className="flex items-center gap-3">
        <button onClick={loadConfig} className="flex items-center gap-2 px-4 py-2 bg-zinc-800 hover:bg-zinc-700 rounded-lg text-sm font-medium transition-colors">
          <RefreshCw className="w-4 h-4" /> {t('reload')}
        </button>
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
                {topKeys.map((k) => (
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
    </div>
  );
};

export default Config;
