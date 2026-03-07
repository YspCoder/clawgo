import React, { useEffect, useMemo, useState } from 'react';
import { RefreshCw, Save } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';

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

const MCP: React.FC = () => {
  const { t } = useTranslation();
  const { cfg, setCfg, q, loadConfig, setConfigEditing } = useAppContext();
  const ui = useUI();
  const [newMCPServerName, setNewMCPServerName] = useState('');
  const [mcpTools, setMcpTools] = useState<Array<{ name: string; description?: string; mcp?: { server?: string; remote_tool?: string } }>>([]);
  const [baseline, setBaseline] = useState<any>(null);

  const currentPayload = useMemo(() => cfg || {}, [cfg]);
  const isDirty = useMemo(() => {
    if (baseline == null) return false;
    return JSON.stringify(baseline) !== JSON.stringify(currentPayload);
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

  async function refreshMCPTools(cancelled = false) {
    try {
      const r = await fetch(`/webui/api/tools${q}`);
      if (!r.ok) throw new Error('Failed to load tools');
      const data = await r.json();
      if (!cancelled) {
        setMcpTools(Array.isArray(data?.mcp_tools) ? data.mcp_tools : []);
      }
    } catch {
      if (!cancelled) setMcpTools([]);
    }
  }

  useEffect(() => {
    let cancelled = false;
    void refreshMCPTools(cancelled);
    return () => {
      cancelled = true;
    };
  }, [q]);

  function updateMCPServerField(name: string, field: string, value: any) {
    setCfg((v) => setPath(v, `tools.mcp.servers.${name}.${field}`, value));
  }

  function addMCPServer() {
    const name = newMCPServerName.trim();
    if (!name) return;
    setCfg((v) => {
      const next = JSON.parse(JSON.stringify(v || {}));
      if (!next.tools || typeof next.tools !== 'object') next.tools = {};
      if (!next.tools.mcp || typeof next.tools.mcp !== 'object') {
        next.tools.mcp = { enabled: true, request_timeout_sec: 20, servers: {} };
      }
      if (!next.tools.mcp.servers || typeof next.tools.mcp.servers !== 'object' || Array.isArray(next.tools.mcp.servers)) {
        next.tools.mcp.servers = {};
      }
      if (!next.tools.mcp.servers[name]) {
        next.tools.mcp.servers[name] = {
          enabled: true,
          transport: 'stdio',
          command: '',
          args: [],
          env: {},
          working_dir: '',
          description: '',
          package: '',
        };
      }
      return next;
    });
    setNewMCPServerName('');
  }

  async function removeMCPServer(name: string) {
    const ok = await ui.confirmDialog({
      title: t('configDeleteMCPServerConfirmTitle'),
      message: t('configDeleteMCPServerConfirmMessage', { name }),
      danger: true,
      confirmText: t('delete'),
    });
    if (!ok) return;
    setCfg((v) => {
      const next = JSON.parse(JSON.stringify(v || {}));
      if (next?.tools?.mcp?.servers && typeof next.tools.mcp.servers === 'object') {
        delete next.tools.mcp.servers[name];
      }
      return next;
    });
  }

  function inferMCPPackage(server: any): string {
    if (typeof server?.package === 'string' && server.package.trim()) return server.package.trim();
    const command = String(server?.command || '').trim();
    const args = Array.isArray(server?.args) ? server.args.map((x: any) => String(x).trim()).filter(Boolean) : [];
    if (command === 'npx' || command.endsWith('/npx')) {
      const pkg = args.find((arg: string) => !arg.startsWith('-'));
      return pkg || '';
    }
    return '';
  }

  async function installMCPServerPackage(name: string, server: any) {
    const defaultPkg = inferMCPPackage(server);
    const pkg = await ui.promptDialog({
      title: t('configMCPInstallTitle'),
      message: t('configMCPInstallMessage', { name }),
      inputPlaceholder: defaultPkg || t('configMCPInstallPlaceholder'),
      initialValue: defaultPkg,
      confirmText: t('install'),
    });
    const packageName = String(pkg || '').trim();
    if (!packageName) return;

    ui.showLoading(t('configMCPInstalling'));
    try {
      const r = await fetch(`/webui/api/mcp/install${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ package: packageName }),
      });
      const text = await r.text();
      if (!r.ok) {
        await ui.notify({ title: t('configMCPInstallFailedTitle'), message: text || t('configMCPInstallFailedMessage') });
        return;
      }
      let data: any = null;
      try {
        data = JSON.parse(text);
      } catch {
        data = null;
      }
      if (data?.bin_path) {
        updateMCPServerField(name, 'command', data.bin_path);
        updateMCPServerField(name, 'args', []);
        updateMCPServerField(name, 'package', packageName);
      } else {
        updateMCPServerField(name, 'package', packageName);
      }
      await ui.notify({
        title: t('configMCPInstallDoneTitle'),
        message: data?.bin_path
          ? t('configMCPInstallDoneMessage', { package: packageName, bin: data.bin_path })
          : (text || t('configMCPInstallDoneFallback')),
      });
    } finally {
      ui.hideLoading();
    }
  }

  async function saveConfig() {
    try {
      const payload = cfg;
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
      await loadConfig(true);
      await refreshMCPTools();
    } catch (e) {
      await ui.notify({ title: t('requestFailed'), message: `${t('saveConfigFailed')}: ${e}` });
    }
  }

  return (
    <div className="p-4 md:p-8 w-full space-y-6">
      <div className="flex items-start justify-between gap-3 flex-wrap">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('mcpServices')}</h1>
          <p className="text-sm text-zinc-500 mt-1">{t('mcpServicesHint')}</p>
        </div>
        <button
          onClick={async () => { setBaseline(null); await loadConfig(true); }}
          className="flex items-center gap-2 px-4 py-2 bg-zinc-800 hover:bg-zinc-700 rounded-xl text-sm font-medium transition-colors"
        >
          <RefreshCw className="w-4 h-4" /> {t('reload')}
        </button>
      </div>

      <div className="flex items-center justify-end gap-3 flex-wrap">
        <button
          onClick={saveConfig}
          disabled={!isDirty}
          className="brand-button flex items-center gap-2 px-4 py-2 text-white rounded-xl text-sm font-medium transition-colors shadow-sm disabled:opacity-50 disabled:cursor-not-allowed"
        >
          <Save className="w-4 h-4" /> {t('saveChanges')}
        </button>
      </div>

      <div className="brand-card-subtle rounded-2xl border border-zinc-800 p-3 space-y-3">
        <div className="flex items-center justify-between gap-2 flex-wrap">
          <div className="text-sm font-semibold text-zinc-200">{t('configMCPServers')}</div>
          <div className="flex items-center gap-2">
            <input value={newMCPServerName} onChange={(e)=>setNewMCPServerName(e.target.value)} placeholder={t('configNewMCPServerName')} className="px-2 py-1 rounded-lg bg-zinc-900/70 border border-zinc-700 text-xs" />
            <button onClick={addMCPServer} className="brand-button px-2 py-1 rounded-lg text-xs text-white">{t('add')}</button>
          </div>
        </div>
        <div className="space-y-2">
          {Object.entries((((cfg as any)?.tools?.mcp?.servers) || {}) as Record<string, any>).map(([name, server]) => (
            <div key={name} className="grid grid-cols-1 md:grid-cols-12 gap-2 rounded-xl border border-zinc-800 bg-zinc-900/30 p-2 text-xs">
              <div className="md:col-span-2 font-mono text-zinc-300 flex items-center">{name}</div>
              <label className="md:col-span-1 flex items-center gap-2 text-zinc-300">
                <input type="checkbox" checked={!!server?.enabled} onChange={(e)=>updateMCPServerField(name, 'enabled', e.target.checked)} />
                {t('enable')}
              </label>
              <input value={String(server?.command || '')} onChange={(e)=>updateMCPServerField(name, 'command', e.target.value)} placeholder={t('configLabels.command')} className="md:col-span-2 px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800" />
              <input value={String(server?.working_dir || '')} onChange={(e)=>updateMCPServerField(name, 'working_dir', e.target.value)} placeholder={t('configLabels.working_dir')} className="md:col-span-2 px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800" />
              <input value={Array.isArray(server?.args) ? server.args.join(',') : ''} onChange={(e)=>updateMCPServerField(name, 'args', e.target.value.split(',').map(s=>s.trim()).filter(Boolean))} placeholder={`${t('configLabels.args')}${t('configCommaSeparatedHint')}`} className="md:col-span-2 px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800" />
              <input value={String(server?.package || '')} onChange={(e)=>updateMCPServerField(name, 'package', e.target.value)} placeholder={t('configLabels.package')} className="md:col-span-1 px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800" />
              <input value={String(server?.description || '')} onChange={(e)=>updateMCPServerField(name, 'description', e.target.value)} placeholder={t('configLabels.description')} className="md:col-span-1 px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800" />
              <button onClick={()=>installMCPServerPackage(name, server)} className="md:col-span-1 px-2 py-1 rounded bg-emerald-900/60 hover:bg-emerald-800 text-emerald-100">{t('install')}</button>
              <button onClick={()=>removeMCPServer(name)} className="md:col-span-1 px-2 py-1 rounded bg-red-900/60 hover:bg-red-800 text-red-100">{t('delete')}</button>
            </div>
          ))}
          {Object.keys((((cfg as any)?.tools?.mcp?.servers) || {}) as Record<string, any>).length === 0 && (
            <div className="text-xs text-zinc-500">{t('configNoMCPServers')}</div>
          )}
        </div>
      </div>

      <div className="brand-card-subtle rounded-2xl border border-zinc-800 p-3 space-y-3">
        <div className="flex items-center justify-between gap-2 flex-wrap">
          <div className="text-sm font-semibold text-zinc-200">{t('configMCPDiscoveredTools')}</div>
          <div className="text-xs text-zinc-500">{t('configMCPDiscoveredToolsCount', { count: mcpTools.length })}</div>
        </div>
        <div className="space-y-2">
          {mcpTools.map((tool) => (
            <div key={tool.name} className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-3">
              <div className="flex items-center justify-between gap-3 flex-wrap">
                <div className="font-mono text-xs text-zinc-200">{tool.name}</div>
                <div className="text-[11px] text-zinc-500">
                  {(tool.mcp?.server || '-')}{' · '}{(tool.mcp?.remote_tool || '-')}
                </div>
              </div>
              {tool.description && (
                <div className="mt-2 text-xs text-zinc-400">{tool.description}</div>
              )}
            </div>
          ))}
          {mcpTools.length === 0 && (
            <div className="text-xs text-zinc-500">{t('configNoMCPDiscoveredTools')}</div>
          )}
        </div>
      </div>
    </div>
  );
};

export default MCP;
