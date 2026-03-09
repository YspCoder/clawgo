import React, { useEffect, useMemo, useState } from 'react';
import { RefreshCw, Save } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
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

function parseTagRuleText(raw: string) {
  const out: Record<string, string[]> = {};
  for (const line of String(raw || '').split('\n')) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const idx = trimmed.indexOf('=');
    if (idx <= 0) continue;
    const key = trimmed.slice(0, idx).trim();
    const tags = trimmed.slice(idx + 1).split(',').map((item) => item.trim()).filter(Boolean);
    if (!key || tags.length === 0) continue;
    out[key] = tags;
  }
  return out;
}

function formatTagRuleText(value: unknown) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return '';
  return Object.entries(value as Record<string, any>)
    .map(([key, tags]) => `${key}=${Array.isArray(tags) ? tags.join(',') : ''}`)
    .filter((line) => line !== '=')
    .join('\n');
}

const Config: React.FC = () => {
  const { t } = useTranslation();
  const ui = useUI();
  const { cfg, setCfg, cfgRaw, setCfgRaw, loadConfig, hotReloadFieldDetails, q, setConfigEditing } = useAppContext();
  const [showRaw, setShowRaw] = useState(false);
  const [basicMode, setBasicMode] = useState(true);
  const [hotOnly, setHotOnly] = useState(false);
  const [search, setSearch] = useState('');
  const [newProxyName, setNewProxyName] = useState('');

  const configLabels = useMemo(
    () => t('configLabels', { returnObjects: true }) as Record<string, string>,
    [t]
  );

  const hotPrefixes = useMemo(() => hotReloadFieldDetails.map((x) => String(x.path || '').replace(/\.\*$/, '')).filter(Boolean), [hotReloadFieldDetails]);
  const hotReloadTabKey = '__hot_reload__';

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
    return [hotReloadTabKey, ...keys];
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
        if (JSON.stringify(a) !== JSON.stringify(b)) out.push({ path: p || t('configRoot'), before: a, after: b });
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

  const isDirty = useMemo(() => {
    if (baseline == null) return false;
    return JSON.stringify(baseline) !== JSON.stringify(currentPayload || {});
  }, [baseline, currentPayload]);

  useEffect(() => {
    if (baseline == null && cfg && Object.keys(cfg).length > 0) {
      setBaseline(JSON.parse(JSON.stringify(cfg)));
    }
  }, [cfg, baseline]);

  useEffect(() => {
    setConfigEditing(isDirty);
    return () => setConfigEditing(false);
  }, [isDirty, setConfigEditing]);

  function updateProxyField(name: string, field: string, value: any) {
    setCfg((v) => setPath(v, `providers.proxies.${name}.${field}`, value));
  }

  function updateGatewayP2PField(field: string, value: any) {
    setCfg((v) => setPath(v, `gateway.nodes.p2p.${field}`, value));
  }

  function updateGatewayDispatchField(field: string, value: any) {
    setCfg((v) => setPath(v, `gateway.nodes.dispatch.${field}`, value));
  }

  function updateGatewayArtifactsField(field: string, value: any) {
    setCfg((v) => setPath(v, `gateway.nodes.artifacts.${field}`, value));
  }

  function updateGatewayIceServer(index: number, field: string, value: any) {
    setCfg((v) => {
      const next = JSON.parse(JSON.stringify(v || {}));
      if (!next.gateway || typeof next.gateway !== 'object') next.gateway = {};
      if (!next.gateway.nodes || typeof next.gateway.nodes !== 'object') next.gateway.nodes = {};
      if (!next.gateway.nodes.p2p || typeof next.gateway.nodes.p2p !== 'object') next.gateway.nodes.p2p = {};
      if (!Array.isArray(next.gateway.nodes.p2p.ice_servers)) next.gateway.nodes.p2p.ice_servers = [];
      if (!next.gateway.nodes.p2p.ice_servers[index] || typeof next.gateway.nodes.p2p.ice_servers[index] !== 'object') {
        next.gateway.nodes.p2p.ice_servers[index] = { urls: [], username: '', credential: '' };
      }
      next.gateway.nodes.p2p.ice_servers[index][field] = value;
      return next;
    });
  }

  function addGatewayIceServer() {
    setCfg((v) => {
      const next = JSON.parse(JSON.stringify(v || {}));
      if (!next.gateway || typeof next.gateway !== 'object') next.gateway = {};
      if (!next.gateway.nodes || typeof next.gateway.nodes !== 'object') next.gateway.nodes = {};
      if (!next.gateway.nodes.p2p || typeof next.gateway.nodes.p2p !== 'object') next.gateway.nodes.p2p = {};
      if (!Array.isArray(next.gateway.nodes.p2p.ice_servers)) next.gateway.nodes.p2p.ice_servers = [];
      next.gateway.nodes.p2p.ice_servers.push({ urls: [], username: '', credential: '' });
      return next;
    });
  }

  function removeGatewayIceServer(index: number) {
    setCfg((v) => {
      const next = JSON.parse(JSON.stringify(v || {}));
      const iceServers = next?.gateway?.nodes?.p2p?.ice_servers;
      if (Array.isArray(iceServers)) {
        iceServers.splice(index, 1);
      }
      return next;
    });
  }

  async function removeProxy(name: string) {
    const ok = await ui.confirmDialog({
      title: t('configDeleteProviderConfirmTitle'),
      message: t('configDeleteProviderConfirmMessage', { name }),
      danger: true,
      confirmText: t('delete'),
    });
    if (!ok) return;
    setCfg((v) => {
      const next = JSON.parse(JSON.stringify(v || {}));
      if (next?.providers?.proxies && typeof next.providers.proxies === 'object') {
        delete next.providers.proxies[name];
      }
      return next;
    });
  }

  function addProxy() {
    const name = newProxyName.trim();
    if (!name) return;
    setCfg((v) => {
      const next = JSON.parse(JSON.stringify(v || {}));
      if (!next.providers || typeof next.providers !== 'object') next.providers = {};
      if (!next.providers.proxies || typeof next.providers.proxies !== 'object' || Array.isArray(next.providers.proxies)) {
        next.providers.proxies = {};
      }
      if (!next.providers.proxies[name]) {
        next.providers.proxies[name] = {
          api_key: '',
          api_base: '',
          models: [],
          responses: {
            web_search_enabled: false,
            web_search_context_size: '',
            file_search_vector_store_ids: [],
            file_search_max_num_results: 0,
            include: [],
            stream_include_usage: false,
          },
          supports_responses_compact: false,
          auth: 'bearer',
          timeout_sec: 120,
        };
      }
      return next;
    });
    setNewProxyName('');
  }

  async function saveConfig() {
    try {
      const payload = showRaw ? JSON.parse(cfgRaw) : cfg;
      const submit = async (confirmRisky: boolean) => {
        const body = confirmRisky ? { ...payload, confirm_risky: true } : payload;
        return ui.withLoading(async () => {
          const r = await fetch(`/webui/api/config${q}`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
          });
          const text = await r.text();
          let data: any = null;
          try {
            data = text ? JSON.parse(text) : null;
          } catch {
            data = null;
          }
          return { ok: r.ok, text, data };
        }, t('saving'));
      };

      let result = await submit(false);
      if (!result.ok && result.data?.requires_confirm) {
        const changedFields = Array.isArray(result.data?.changed_fields) ? result.data.changed_fields.join(', ') : '';
        const ok = await ui.confirmDialog({
          title: t('configRiskyChangeConfirmTitle'),
          message: t('configRiskyChangeConfirmMessage', { fields: changedFields || '-' }),
          danger: true,
          confirmText: t('saveChanges'),
        });
        if (!ok) return;
        result = await submit(true);
      }

      if (!result.ok) {
        throw new Error(result.data?.error || result.text || 'save failed');
      }

      await ui.notify({ title: t('saved'), message: t('configSaved') });
      setBaseline(JSON.parse(JSON.stringify(payload)));
      setConfigEditing(false);
      setShowDiff(false);
    } catch (e) {
      await ui.notify({ title: t('requestFailed'), message: `${t('saveConfigFailed')}: ${e}` });
    }
  }

  return (
    <div className="p-4 md:p-8 w-full space-y-6 flex flex-col min-h-full">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <h1 className="text-2xl font-semibold tracking-tight">{t('configuration')}</h1>
        <div className="flex items-center gap-1 bg-zinc-900/60 p-1 rounded-xl border border-zinc-800">
          <button onClick={() => setShowRaw(false)} className={`ui-button px-4 py-1.5 text-sm font-medium rounded-lg ${!showRaw ? 'ui-button-primary' : 'ui-button-neutral'}`}>{t('form')}</button>
          <button onClick={() => setShowRaw(true)} className={`ui-button px-4 py-1.5 text-sm font-medium rounded-lg ${showRaw ? 'ui-button-primary' : 'ui-button-neutral'}`}>{t('rawJson')}</button>
        </div>
      </div>

      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div className="flex items-center gap-3 flex-wrap">
          <button onClick={async () => { await loadConfig(true); setTimeout(() => setBaseline(JSON.parse(JSON.stringify(cfg))), 0); }} className="ui-button ui-button-neutral flex items-center gap-2 px-4 py-2 text-sm font-medium">
            <RefreshCw className="w-4 h-4" /> {t('reload')}
          </button>
          <button onClick={() => setShowDiff(true)} className="ui-button ui-button-neutral px-3 py-2 text-sm">{t('configDiffPreview')}</button>
          <button onClick={() => setBasicMode(v => !v)} className="ui-button ui-button-neutral px-3 py-2 text-sm">
            {basicMode ? t('configBasicMode') : t('configAdvancedMode')}
          </button>
          <label className="flex items-center gap-2 text-sm text-zinc-300">
            <input type="checkbox" checked={hotOnly} onChange={(e) => setHotOnly(e.target.checked)} />
            {t('configHotOnly')}
          </label>
          <input value={search} onChange={(e) => setSearch(e.target.value)} placeholder={t('configSearchPlaceholder')} className="px-3 py-2 bg-zinc-950/70 border border-zinc-800 rounded-xl text-sm" />
        </div>
        <button onClick={saveConfig} className="ui-button ui-button-primary flex items-center gap-2 px-4 py-2 text-sm font-medium">
          <Save className="w-4 h-4" /> {t('saveChanges')}
        </button>
      </div>

      <div className="flex-1 brand-card border border-zinc-800/80 rounded-[30px] overflow-hidden flex flex-col shadow-sm min-h-[420px]">
        {!showRaw ? (
          <div className="flex-1 flex min-h-0">
            <aside className="w-44 md:w-56 border-r border-zinc-800 bg-zinc-950/20 p-2 md:p-3 overflow-y-auto shrink-0">
              <div className="text-xs text-zinc-500 uppercase tracking-widest mb-2 px-2">{t('configTopLevel')}</div>
              <div className="space-y-1">
                {filteredTopKeys.map((k) => (
                  <button
                    key={k}
                    onClick={() => setSelectedTop(k)}
                    className={`w-full text-left px-3 py-2 rounded-xl text-sm transition-colors ${activeTop === k ? 'nav-item-active text-indigo-700 border border-indigo-500/30' : 'text-zinc-300 hover:bg-zinc-800/30'}`}
                  >
                    {k === hotReloadTabKey ? t('configHotFieldsFull') : (configLabels[k] || k)}
                  </button>
                ))}
              </div>
            </aside>

            <div className="flex-1 p-4 md:p-6 overflow-y-auto space-y-4">
              {activeTop === hotReloadTabKey && (
                <div className="space-y-3">
                  <div className="text-sm font-semibold text-zinc-300">{t('configHotFieldsFull')}</div>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-2 text-xs">
                    {hotReloadFieldDetails.map((it) => (
                      <div key={it.path} className="p-2 rounded-xl bg-zinc-950/70 border border-zinc-800">
                        <div className="font-mono text-zinc-200">{it.path}</div>
                        <div className="text-zinc-400">{it.name || ''}{it.description ? ` · ${it.description}` : ''}</div>
                      </div>
                    ))}
                  </div>
                </div>
              )}
              {activeTop === 'providers' && !showRaw && (
                <div className="brand-card-subtle rounded-2xl border border-zinc-800 p-3 space-y-3">
                  <div className="flex items-center justify-between gap-2 flex-wrap">
                    <div className="text-sm font-semibold text-zinc-200">{t('configProxies')}</div>
                    <div className="flex items-center gap-2">
                      <input value={newProxyName} onChange={(e)=>setNewProxyName(e.target.value)} placeholder={t('configNewProviderName')} className="px-2 py-1 rounded-lg bg-zinc-900/70 border border-zinc-700 text-xs" />
                      <button onClick={addProxy} className="ui-button ui-button-primary px-2 py-1 rounded-lg text-xs">{t('add')}</button>
                    </div>
                  </div>
                  <div className="space-y-2">
                    {Object.entries(((cfg as any)?.providers?.proxies || {}) as Record<string, any>).map(([name, p]) => (
                      <div key={name} className="grid grid-cols-1 md:grid-cols-7 gap-2 rounded-xl border border-zinc-800 bg-zinc-900/30 p-2 text-xs">
                        <div className="md:col-span-1 font-mono text-zinc-300 flex items-center">{name}</div>
                        <input value={String(p?.api_base || '')} onChange={(e)=>updateProxyField(name, 'api_base', e.target.value)} placeholder={t('configLabels.api_base')} className="md:col-span-2 px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800" />
                        <input value={String(p?.api_key || '')} onChange={(e)=>updateProxyField(name, 'api_key', e.target.value)} placeholder={t('configLabels.api_key')} className="md:col-span-2 px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800" />
                        <input value={Array.isArray(p?.models) ? p.models.join(',') : ''} onChange={(e)=>updateProxyField(name, 'models', e.target.value.split(',').map(s=>s.trim()).filter(Boolean))} placeholder={`${t('configLabels.models')}${t('configCommaSeparatedHint')}`} className="md:col-span-1 px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800" />
                        <button onClick={()=>removeProxy(name)} className="ui-button ui-button-danger md:col-span-1 px-2 py-1 rounded text-xs">{t('delete')}</button>
                      </div>
                    ))}
                    {Object.keys(((cfg as any)?.providers?.proxies || {}) as Record<string, any>).length === 0 && (
                      <div className="text-xs text-zinc-500">{t('configNoCustomProviders')}</div>
                    )}
                  </div>
                </div>
              )}
              {activeTop === 'gateway' && !showRaw && (
                <div className="brand-card-subtle rounded-2xl border border-zinc-800 p-3 space-y-3">
                  <div className="flex items-center justify-between gap-2 flex-wrap">
                    <div className="text-sm font-semibold text-zinc-200">{t('configNodeP2P')}</div>
                    <div className="text-xs text-zinc-500">{t('configNodeP2PHint')}</div>
                  </div>
                  <div className="grid grid-cols-1 md:grid-cols-3 gap-2 text-xs">
                    <label className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-3 space-y-2">
                      <div className="text-zinc-300">{t('enable')}</div>
                      <input
                        type="checkbox"
                        checked={Boolean((cfg as any)?.gateway?.nodes?.p2p?.enabled)}
                        onChange={(e) => updateGatewayP2PField('enabled', e.target.checked)}
                      />
                    </label>
                    <label className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-3 space-y-2">
                      <div className="text-zinc-300">{t('dashboardNodeP2PTransport')}</div>
                      <select
                        value={String((cfg as any)?.gateway?.nodes?.p2p?.transport || 'websocket_tunnel')}
                        onChange={(e) => updateGatewayP2PField('transport', e.target.value)}
                        className="w-full px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800"
                      >
                        <option value="websocket_tunnel">websocket_tunnel</option>
                        <option value="webrtc">webrtc</option>
                      </select>
                    </label>
                    <label className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-3 space-y-2">
                      <div className="text-zinc-300">{t('dashboardNodeP2PIce')}</div>
                      <input
                        value={Array.isArray((cfg as any)?.gateway?.nodes?.p2p?.stun_servers) ? (cfg as any).gateway.nodes.p2p.stun_servers.join(', ') : ''}
                        onChange={(e) => updateGatewayP2PField('stun_servers', e.target.value.split(',').map((s) => s.trim()).filter(Boolean))}
                        placeholder={t('configNodeP2PStunPlaceholder')}
                        className="w-full px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800"
                      />
                    </label>
                  </div>
                  <div className="space-y-2">
                    <div className="flex items-center justify-between gap-2 flex-wrap">
                      <div className="text-sm font-medium text-zinc-200">{t('configNodeP2PIceServers')}</div>
                      <button onClick={addGatewayIceServer} className="ui-button ui-button-primary px-2 py-1 rounded-lg text-xs">{t('add')}</button>
                    </div>
                    {Array.isArray((cfg as any)?.gateway?.nodes?.p2p?.ice_servers) && (cfg as any).gateway.nodes.p2p.ice_servers.length > 0 ? (
                      ((cfg as any).gateway.nodes.p2p.ice_servers as Array<any>).map((server, index) => (
                        <div key={`ice-${index}`} className="grid grid-cols-1 md:grid-cols-7 gap-2 rounded-xl border border-zinc-800 bg-zinc-900/30 p-2 text-xs">
                          <input
                            value={Array.isArray(server?.urls) ? server.urls.join(', ') : ''}
                            onChange={(e) => updateGatewayIceServer(index, 'urls', e.target.value.split(',').map((s) => s.trim()).filter(Boolean))}
                            placeholder={t('configNodeP2PIceUrlsPlaceholder')}
                            className="md:col-span-3 px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800"
                          />
                          <input
                            value={String(server?.username || '')}
                            onChange={(e) => updateGatewayIceServer(index, 'username', e.target.value)}
                            placeholder={t('configNodeP2PIceUsername')}
                            className="md:col-span-1 px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800"
                          />
                          <input
                            value={String(server?.credential || '')}
                            onChange={(e) => updateGatewayIceServer(index, 'credential', e.target.value)}
                            placeholder={t('configNodeP2PIceCredential')}
                            className="md:col-span-2 px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800"
                          />
                          <button onClick={() => removeGatewayIceServer(index)} className="ui-button ui-button-danger md:col-span-1 px-2 py-1 rounded text-xs">{t('delete')}</button>
                        </div>
                      ))
                    ) : (
                      <div className="text-xs text-zinc-500">{t('configNodeP2PIceServersEmpty')}</div>
                    )}
                  </div>
                  <div className="border-t border-zinc-800/70 pt-3 space-y-3">
                    <div className="flex items-center justify-between gap-2 flex-wrap">
                      <div className="text-sm font-semibold text-zinc-200">{t('configNodeDispatch')}</div>
                      <div className="text-xs text-zinc-500">{t('configNodeDispatchHint')}</div>
                    </div>
                    <div className="grid grid-cols-1 md:grid-cols-3 gap-2 text-xs">
                      <label className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-3 space-y-2">
                        <div className="text-zinc-300">{t('configNodeDispatchPreferLocal')}</div>
                        <input
                          type="checkbox"
                          checked={Boolean((cfg as any)?.gateway?.nodes?.dispatch?.prefer_local)}
                          onChange={(e) => updateGatewayDispatchField('prefer_local', e.target.checked)}
                        />
                      </label>
                      <label className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-3 space-y-2">
                        <div className="text-zinc-300">{t('configNodeDispatchPreferP2P')}</div>
                        <input
                          type="checkbox"
                          checked={Boolean((cfg as any)?.gateway?.nodes?.dispatch?.prefer_p2p ?? true)}
                          onChange={(e) => updateGatewayDispatchField('prefer_p2p', e.target.checked)}
                        />
                      </label>
                      <label className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-3 space-y-2">
                        <div className="text-zinc-300">{t('configNodeDispatchAllowRelay')}</div>
                        <input
                          type="checkbox"
                          checked={Boolean((cfg as any)?.gateway?.nodes?.dispatch?.allow_relay_fallback ?? true)}
                          onChange={(e) => updateGatewayDispatchField('allow_relay_fallback', e.target.checked)}
                        />
                      </label>
                    </div>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-xs">
                      <label className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-3 space-y-2">
                        <div className="text-zinc-300">{t('configNodeDispatchActionTags')}</div>
                        <textarea
                          value={formatTagRuleText((cfg as any)?.gateway?.nodes?.dispatch?.action_tags)}
                          onChange={(e) => updateGatewayDispatchField('action_tags', parseTagRuleText(e.target.value))}
                          placeholder={t('configNodeDispatchActionTagsPlaceholder')}
                          className="min-h-28 w-full rounded-xl bg-zinc-950/70 border border-zinc-800 px-3 py-2"
                        />
                      </label>
                      <label className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-3 space-y-2">
                        <div className="text-zinc-300">{t('configNodeDispatchAgentTags')}</div>
                        <textarea
                          value={formatTagRuleText((cfg as any)?.gateway?.nodes?.dispatch?.agent_tags)}
                          onChange={(e) => updateGatewayDispatchField('agent_tags', parseTagRuleText(e.target.value))}
                          placeholder={t('configNodeDispatchAgentTagsPlaceholder')}
                          className="min-h-28 w-full rounded-xl bg-zinc-950/70 border border-zinc-800 px-3 py-2"
                        />
                      </label>
                    </div>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-xs">
                      <label className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-3 space-y-2">
                        <div className="text-zinc-300">{t('configNodeDispatchAllowActions')}</div>
                        <textarea
                          value={formatTagRuleText((cfg as any)?.gateway?.nodes?.dispatch?.allow_actions)}
                          onChange={(e) => updateGatewayDispatchField('allow_actions', parseTagRuleText(e.target.value))}
                          placeholder={t('configNodeDispatchAllowActionsPlaceholder')}
                          className="min-h-28 w-full rounded-xl bg-zinc-950/70 border border-zinc-800 px-3 py-2"
                        />
                      </label>
                      <label className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-3 space-y-2">
                        <div className="text-zinc-300">{t('configNodeDispatchDenyActions')}</div>
                        <textarea
                          value={formatTagRuleText((cfg as any)?.gateway?.nodes?.dispatch?.deny_actions)}
                          onChange={(e) => updateGatewayDispatchField('deny_actions', parseTagRuleText(e.target.value))}
                          placeholder={t('configNodeDispatchDenyActionsPlaceholder')}
                          className="min-h-28 w-full rounded-xl bg-zinc-950/70 border border-zinc-800 px-3 py-2"
                        />
                      </label>
                    </div>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-xs">
                      <label className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-3 space-y-2">
                        <div className="text-zinc-300">{t('configNodeDispatchAllowAgents')}</div>
                        <textarea
                          value={formatTagRuleText((cfg as any)?.gateway?.nodes?.dispatch?.allow_agents)}
                          onChange={(e) => updateGatewayDispatchField('allow_agents', parseTagRuleText(e.target.value))}
                          placeholder={t('configNodeDispatchAllowAgentsPlaceholder')}
                          className="min-h-28 w-full rounded-xl bg-zinc-950/70 border border-zinc-800 px-3 py-2"
                        />
                      </label>
                      <label className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-3 space-y-2">
                        <div className="text-zinc-300">{t('configNodeDispatchDenyAgents')}</div>
                        <textarea
                          value={formatTagRuleText((cfg as any)?.gateway?.nodes?.dispatch?.deny_agents)}
                          onChange={(e) => updateGatewayDispatchField('deny_agents', parseTagRuleText(e.target.value))}
                          placeholder={t('configNodeDispatchDenyAgentsPlaceholder')}
                          className="min-h-28 w-full rounded-xl bg-zinc-950/70 border border-zinc-800 px-3 py-2"
                        />
                      </label>
                    </div>
                  </div>
                  <div className="border-t border-zinc-800/70 pt-3 space-y-3">
                    <div className="flex items-center justify-between gap-2 flex-wrap">
                      <div className="text-sm font-semibold text-zinc-200">{t('configNodeArtifacts')}</div>
                      <div className="text-xs text-zinc-500">{t('configNodeArtifactsHint')}</div>
                    </div>
                    <div className="grid grid-cols-1 md:grid-cols-3 gap-2 text-xs">
                      <label className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-3 space-y-2">
                        <div className="text-zinc-300">{t('enable')}</div>
                        <input
                          type="checkbox"
                          checked={Boolean((cfg as any)?.gateway?.nodes?.artifacts?.enabled)}
                          onChange={(e) => updateGatewayArtifactsField('enabled', e.target.checked)}
                        />
                      </label>
                      <label className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-3 space-y-2">
                        <div className="text-zinc-300">{t('configNodeArtifactsKeepLatest')}</div>
                        <input
                          type="number"
                          min={1}
                          value={Number((cfg as any)?.gateway?.nodes?.artifacts?.keep_latest || 500)}
                          onChange={(e) => updateGatewayArtifactsField('keep_latest', Math.max(1, Number.parseInt(e.target.value || '0', 10) || 1))}
                          className="w-full px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800"
                        />
                      </label>
                      <label className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-3 space-y-2">
                        <div className="text-zinc-300">{t('configNodeArtifactsPruneOnRead')}</div>
                        <input
                          type="checkbox"
                          checked={Boolean((cfg as any)?.gateway?.nodes?.artifacts?.prune_on_read ?? true)}
                          onChange={(e) => updateGatewayArtifactsField('prune_on_read', e.target.checked)}
                        />
                      </label>
                    </div>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-2 text-xs">
                      <label className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-3 space-y-2">
                        <div className="text-zinc-300">{t('configNodeArtifactsRetainDays')}</div>
                        <input
                          type="number"
                          min={0}
                          value={Number((cfg as any)?.gateway?.nodes?.artifacts?.retain_days ?? 7)}
                          onChange={(e) => updateGatewayArtifactsField('retain_days', Math.max(0, Number.parseInt(e.target.value || '0', 10) || 0))}
                          className="w-full px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800"
                        />
                      </label>
                    </div>
                  </div>
                </div>
              )}
              {activeTop && activeTop !== hotReloadTabKey ? (
                <RecursiveConfig
                  data={(cfg as any)?.[activeTop] || {}}
                  labels={configLabels}
                  path={activeTop}
                  hotPaths={hotReloadFieldDetails.map((x) => x.path)}
                  onlyHot={hotOnly}
                  onChange={(path, val) => setCfg(v => setPath(v, path, val))}
                />
              ) : (
                <div className="text-zinc-500 text-sm">{t('configNoGroups')}</div>
              )}
            </div>
          </div>
        ) : (
          <textarea
            value={cfgRaw}
            onChange={(e) => setCfgRaw(e.target.value)}
            className="flex-1 w-full bg-zinc-950/35 p-6 font-mono text-sm text-zinc-300 focus:outline-none resize-none"
            spellCheck={false}
          />
        )}
      </div>

      {showDiff && (
        <div className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center p-4">
          <div className="w-full max-w-4xl max-h-[85vh] brand-card border border-zinc-800 rounded-[30px] overflow-hidden flex flex-col">
            <div className="px-4 py-3 border-b border-zinc-800 flex items-center justify-between">
              <div className="font-semibold">{t('configDiffPreviewCount', { count: diffRows.length })}</div>
              <button className="px-3 py-1 rounded-xl bg-zinc-800" onClick={() => setShowDiff(false)}>{t('close')}</button>
            </div>
            <div className="overflow-auto text-xs">
              <table className="w-full">
                <thead className="sticky top-0 bg-zinc-900 text-zinc-300">
                  <tr>
                    <th className="text-left p-2">{t('path')}</th>
                    <th className="text-left p-2">{t('before')}</th>
                    <th className="text-left p-2">{t('after')}</th>
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
