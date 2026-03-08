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
  const [mcpServerChecks, setMcpServerChecks] = useState<Array<{ name: string; status?: string; message?: string; package?: string; installer?: string; installable?: boolean; resolved?: string }>>([]);
  const [argInputs, setArgInputs] = useState<Record<string, string>>({});
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
        setMcpServerChecks(Array.isArray(data?.mcp_server_checks) ? data.mcp_server_checks : []);
      }
    } catch {
      if (!cancelled) {
        setMcpTools([]);
        setMcpServerChecks([]);
      }
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

  function addMCPArg(name: string, rawValue: string) {
    const value = rawValue.trim();
    if (!value) return;
    const current = ((((cfg as any)?.tools?.mcp?.servers?.[name]?.args) || []) as any[])
      .map((x) => String(x).trim())
      .filter(Boolean);
    updateMCPServerField(name, 'args', [...current, value]);
    setArgInputs((prev) => ({ ...prev, [name]: '' }));
  }

  function removeMCPArg(name: string, index: number) {
    const current = ((((cfg as any)?.tools?.mcp?.servers?.[name]?.args) || []) as any[])
      .map((x) => String(x).trim())
      .filter(Boolean);
    updateMCPServerField(name, 'args', current.filter((_, i) => i !== index));
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
          url: '',
          command: '',
          args: [],
          env: {},
          permission: 'workspace',
          working_dir: '',
          description: '',
          package: '',
        };
      }
      return next;
    });
    setNewMCPServerName('');
    setArgInputs((prev) => ({ ...prev, [name]: '' }));
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
    setArgInputs((prev) => {
      const next = { ...prev };
      delete next[name];
      return next;
    });
  }

  function inferMCPInstallSpec(server: any): { installer: string; packageName: string } {
    if (typeof server?.installer === 'string' && server.installer.trim() && typeof server?.package === 'string' && server.package.trim()) {
      return { installer: server.installer.trim(), packageName: server.package.trim() };
    }
    if (typeof server?.package === 'string' && server.package.trim()) {
      return { installer: 'npm', packageName: server.package.trim() };
    }
    const command = String(server?.command || '').trim().split('/').pop() || '';
    const args = Array.isArray(server?.args) ? server.args.map((x: any) => String(x).trim()).filter(Boolean) : [];
    const pkg = args.find((arg: string) => !arg.startsWith('-')) || '';
    if (command === 'npx') return { installer: 'npm', packageName: pkg };
    if (command === 'uvx') return { installer: 'uv', packageName: pkg };
    if (command === 'bunx') return { installer: 'bun', packageName: pkg };
    return { installer: 'npm', packageName: '' };
  }

  async function installMCPServerPackage(name: string, server: any) {
    const inferred = inferMCPInstallSpec(server);
    const defaultPkg = inferred.packageName;
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
        body: JSON.stringify({ package: packageName, installer: inferred.installer }),
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

  async function installMCPServerCheckPackage(check: { name: string; package?: string; installer?: string }) {
    const server = (((cfg as any)?.tools?.mcp?.servers?.[check.name]) || {}) as any;
    if (check.package && !String(server?.package || '').trim()) {
      updateMCPServerField(check.name, 'package', check.package);
    }
    await installMCPServerPackage(check.name, { ...server, package: check.package || server?.package, installer: check.installer });
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
          {Object.entries((((cfg as any)?.tools?.mcp?.servers) || {}) as Record<string, any>).map(([name, server]) => {
            const transport = String(server?.transport || 'stdio');
            const isStdio = transport === 'stdio';
            const usesURL = transport === 'http' || transport === 'streamable_http' || transport === 'sse';
            return (
            <div key={name} className="grid grid-cols-1 md:grid-cols-12 gap-2 rounded-xl border border-zinc-800 bg-zinc-900/30 p-2 text-xs">
              <div className="md:col-span-2 font-mono text-zinc-300 flex items-center">{name}</div>
              <label className="md:col-span-1 flex items-center gap-2 text-zinc-300">
                <input type="checkbox" checked={!!server?.enabled} onChange={(e)=>updateMCPServerField(name, 'enabled', e.target.checked)} />
                {t('enable')}
              </label>
              <select value={transport} onChange={(e)=>updateMCPServerField(name, 'transport', e.target.value)} className="md:col-span-2 px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800">
                <option value="stdio">stdio</option>
                <option value="http">http</option>
                <option value="streamable_http">streamable_http</option>
                <option value="sse">sse</option>
              </select>
              {isStdio && (
                <>
                  <input value={String(server?.command || '')} onChange={(e)=>updateMCPServerField(name, 'command', e.target.value)} placeholder={t('configLabels.command')} className="md:col-span-2 px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800" />
                  <select value={String(server?.permission || 'workspace')} onChange={(e)=>updateMCPServerField(name, 'permission', e.target.value)} className="md:col-span-1 px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800">
                    <option value="workspace">workspace</option>
                    <option value="full">full</option>
                  </select>
                  <input value={String(server?.working_dir || '')} onChange={(e)=>updateMCPServerField(name, 'working_dir', e.target.value)} placeholder={t('configLabels.working_dir')} className="md:col-span-2 px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800" />
                  <div className="md:col-span-2 rounded-lg bg-zinc-950/70 border border-zinc-800 p-2 space-y-2">
                    <div className="flex flex-wrap gap-2">
                      {(Array.isArray(server?.args) ? server.args : []).map((arg: any, index: number) => (
                        <span key={`${name}-arg-${index}`} className="inline-flex items-center gap-2 rounded-md bg-zinc-800 px-2 py-1 text-[11px] text-zinc-200">
                          <span className="font-mono">{String(arg)}</span>
                          <button type="button" onClick={() => removeMCPArg(name, index)} className="text-zinc-400 hover:text-zinc-100">x</button>
                        </span>
                      ))}
                    </div>
                    <input
                      value={argInputs[name] || ''}
                      onChange={(e) => setArgInputs((prev) => ({ ...prev, [name]: e.target.value }))}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault();
                          addMCPArg(name, argInputs[name] || '');
                        }
                      }}
                      onBlur={() => addMCPArg(name, argInputs[name] || '')}
                      placeholder={t('configMCPArgsEnterHint')}
                      className="w-full px-2 py-1 rounded-lg bg-zinc-900/80 border border-zinc-800"
                    />
                  </div>
                  <input value={String(server?.package || '')} onChange={(e)=>updateMCPServerField(name, 'package', e.target.value)} placeholder={t('configLabels.package')} className="md:col-span-1 px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800" />
                </>
              )}
              {usesURL && (
                <input value={String(server?.url || '')} onChange={(e)=>updateMCPServerField(name, 'url', e.target.value)} placeholder={t('configLabels.url')} className="md:col-span-5 px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800" />
              )}
              <input value={String(server?.description || '')} onChange={(e)=>updateMCPServerField(name, 'description', e.target.value)} placeholder={t('configLabels.description')} className="md:col-span-2 px-2 py-1 rounded-lg bg-zinc-950/70 border border-zinc-800" />
              {isStdio && (
                <button onClick={()=>installMCPServerPackage(name, server)} className="md:col-span-1 px-2 py-1 rounded bg-emerald-900/60 hover:bg-emerald-800 text-emerald-100">{t('install')}</button>
              )}
              <button onClick={()=>removeMCPServer(name)} className="md:col-span-1 px-2 py-1 rounded bg-red-900/60 hover:bg-red-800 text-red-100">{t('delete')}</button>
              {(() => {
                const check = mcpServerChecks.find((item) => item.name === name);
                if (!check || check.status === 'ok' || check.status === 'disabled' || check.status === 'not_applicable') return null;
                return (
                  <div className="md:col-span-12 rounded-lg border border-amber-800/60 bg-amber-950/30 px-3 py-2 text-xs text-amber-100 flex items-center justify-between gap-3 flex-wrap">
                    <div>
                      <div>{check.message || t('configMCPCommandMissing')}</div>
                      {check.package && (
                        <div className="text-amber-300/80">{t('configMCPInstallSuggested', { pkg: check.package })} {check.installer ? `(${check.installer})` : ''}</div>
                      )}
                    </div>
                    {check.installable && (
                      <button onClick={() => installMCPServerCheckPackage(check)} className="px-2 py-1 rounded bg-amber-700 hover:bg-amber-600 text-white">
                        {t('install')}
                      </button>
                    )}
                  </div>
                );
              })()}
            </div>
          )})}
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
