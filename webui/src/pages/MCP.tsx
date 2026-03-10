import React, { useEffect, useMemo, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { Package, Pencil, Plus, RefreshCw, Save, Trash2, Wrench, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
import { Button, FixedButton } from '../components/Button';
import Checkbox from '../components/Checkbox';
import FormField from '../components/FormField';
import Input from '../components/Input';
import Select from '../components/Select';

type MCPDraftServer = {
  enabled: boolean;
  transport: string;
  url: string;
  command: string;
  args: string[];
  env: Record<string, string>;
  permission: string;
  working_dir: string;
  description: string;
  package: string;
  installer?: string;
};

const emptyDraftServer = (): MCPDraftServer => ({
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
  installer: '',
});

const cloneDeep = <T,>(value: T): T => JSON.parse(JSON.stringify(value));

const MCP: React.FC = () => {
  const { t } = useTranslation();
  const { cfg, setCfg, q, loadConfig, setConfigEditing } = useAppContext();
  const ui = useUI();
  const [mcpServerChecks, setMcpServerChecks] = useState<Array<{ name: string; status?: string; message?: string; package?: string; installer?: string; installable?: boolean; resolved?: string }>>([]);
  const [modalOpen, setModalOpen] = useState(false);
  const [editingName, setEditingName] = useState<string | null>(null);
  const [draftName, setDraftName] = useState('');
  const [draft, setDraft] = useState<MCPDraftServer>(emptyDraftServer());
  const [draftArgInput, setDraftArgInput] = useState('');

  const servers = useMemo(() => ((((cfg as any)?.tools?.mcp?.servers) || {}) as Record<string, any>), [cfg]);
  const serverEntries = useMemo(() => Object.entries(servers), [servers]);

  useEffect(() => {
    setConfigEditing(false);
    return () => setConfigEditing(false);
  }, [setConfigEditing]);

  async function refreshMCPTools(cancelled = false) {
    try {
      const r = await fetch(`/webui/api/tools${q}`);
      if (!r.ok) throw new Error('Failed to load tools');
      const data = await r.json();
      if (!cancelled) {
        setMcpServerChecks(Array.isArray(data?.mcp_server_checks) ? data.mcp_server_checks : []);
      }
    } catch {
      if (!cancelled) {
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

  function openCreateModal() {
    setEditingName(null);
    setDraftName('');
    setDraft(emptyDraftServer());
    setDraftArgInput('');
    setModalOpen(true);
  }

  function openEditModal(name: string, server: any) {
    setEditingName(name);
    setDraftName(name);
    setDraft({
      enabled: Boolean(server?.enabled),
      transport: String(server?.transport || 'stdio'),
      url: String(server?.url || ''),
      command: String(server?.command || ''),
      args: Array.isArray(server?.args) ? server.args.map((x: any) => String(x)) : [],
      env: typeof server?.env === 'object' && server?.env ? cloneDeep(server.env) : {},
      permission: String(server?.permission || 'workspace'),
      working_dir: String(server?.working_dir || ''),
      description: String(server?.description || ''),
      package: String(server?.package || ''),
      installer: String(server?.installer || ''),
    });
    setDraftArgInput('');
    setModalOpen(true);
  }

  function closeModal() {
    setModalOpen(false);
    setEditingName(null);
    setDraftName('');
    setDraft(emptyDraftServer());
    setDraftArgInput('');
  }

  function updateDraftField<K extends keyof MCPDraftServer>(field: K, value: MCPDraftServer[K]) {
    setDraft((prev) => ({ ...prev, [field]: value }));
  }

  function addDraftArg(rawValue: string) {
    const value = rawValue.trim();
    if (!value) return;
    setDraft((prev) => ({ ...prev, args: [...prev.args.map((x) => x.trim()).filter(Boolean), value] }));
    setDraftArgInput('');
  }

  function removeDraftArg(index: number) {
    setDraft((prev) => ({ ...prev, args: prev.args.filter((_, i) => i !== index) }));
  }

  function inferMCPInstallSpec(server: MCPDraftServer): { installer: string; packageName: string } {
    if (typeof server.installer === 'string' && server.installer.trim() && typeof server.package === 'string' && server.package.trim()) {
      return { installer: server.installer.trim(), packageName: server.package.trim() };
    }
    if (typeof server.package === 'string' && server.package.trim()) {
      return { installer: 'npm', packageName: server.package.trim() };
    }
    const command = String(server.command || '').trim().split('/').pop() || '';
    const args = Array.isArray(server.args) ? server.args.map((x) => String(x).trim()).filter(Boolean) : [];
    const pkg = args.find((arg) => !arg.startsWith('-')) || '';
    if (command === 'npx') return { installer: 'npm', packageName: pkg };
    if (command === 'uvx') return { installer: 'uv', packageName: pkg };
    if (command === 'bunx') return { installer: 'bun', packageName: pkg };
    return { installer: 'npm', packageName: '' };
  }

  async function persistConfig(nextCfg: any) {
    const submit = async (payload: any, confirmRisky: boolean) => {
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

    let result = await submit(nextCfg, false);
    if (!result.ok && result.data?.requires_confirm) {
      const changedFields = Array.isArray(result.data?.changed_fields) ? result.data.changed_fields.join(', ') : '';
      const ok = await ui.confirmDialog({
        title: t('configRiskyChangeConfirmTitle'),
        message: t('configRiskyChangeConfirmMessage', { fields: changedFields || '-' }),
        danger: true,
        confirmText: t('saveChanges'),
      });
      if (!ok) return false;
      result = await submit(nextCfg, true);
    }

    if (!result.ok) {
      throw new Error(result.data?.error || result.text || 'save failed');
    }

    setCfg(nextCfg);
    await loadConfig(true);
    await refreshMCPTools();
    return true;
  }

  async function saveServer() {
    const name = draftName.trim();
    if (!name) {
      await ui.notify({ title: t('requestFailed'), message: t('configNewMCPServerName') });
      return;
    }
    if (editingName !== name && servers[name]) {
      await ui.notify({ title: t('requestFailed'), message: `${name} already exists` });
      return;
    }

    const next = cloneDeep(cfg || {});
    if (!next.tools || typeof next.tools !== 'object') next.tools = {};
    if (!next.tools.mcp || typeof next.tools.mcp !== 'object') {
      next.tools.mcp = { enabled: true, request_timeout_sec: 20, servers: {} };
    }
    if (!next.tools.mcp.servers || typeof next.tools.mcp.servers !== 'object' || Array.isArray(next.tools.mcp.servers)) {
      next.tools.mcp.servers = {};
    }
    if (editingName && editingName !== name) {
      delete next.tools.mcp.servers[editingName];
    }
    next.tools.mcp.servers[name] = {
      enabled: draft.enabled,
      transport: draft.transport,
      url: draft.url,
      command: draft.command,
      args: draft.args.map((x) => x.trim()).filter(Boolean),
      env: draft.env,
      permission: draft.permission,
      working_dir: draft.working_dir,
      description: draft.description,
      package: draft.package,
      installer: draft.installer,
    };

    try {
      const ok = await persistConfig(next);
      if (!ok) return;
      await ui.notify({ title: t('saved'), message: t('configSaved') });
      closeModal();
    } catch (e) {
      await ui.notify({ title: t('requestFailed'), message: `${t('saveConfigFailed')}: ${e}` });
    }
  }

  async function removeServer(name: string) {
    const ok = await ui.confirmDialog({
      title: t('configDeleteMCPServerConfirmTitle'),
      message: t('configDeleteMCPServerConfirmMessage', { name }),
      danger: true,
      confirmText: t('delete'),
    });
    if (!ok) return;
    try {
      const next = cloneDeep(cfg || {});
      if (next?.tools?.mcp?.servers && typeof next.tools.mcp.servers === 'object') {
        delete next.tools.mcp.servers[name];
      }
      const saved = await persistConfig(next);
      if (!saved) return;
      await ui.notify({ title: t('saved'), message: t('configSaved') });
      if (editingName === name) closeModal();
    } catch (e) {
      await ui.notify({ title: t('requestFailed'), message: `${t('saveConfigFailed')}: ${e}` });
    }
  }

  async function installDraftPackage() {
    const inferred = inferMCPInstallSpec(draft);
    const defaultPkg = inferred.packageName;
    const pkg = await ui.promptDialog({
      title: t('configMCPInstallTitle'),
      message: t('configMCPInstallMessage', { name: draftName.trim() || t('configMCPServers') }),
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
      setDraft((prev) => ({
        ...prev,
        command: data?.bin_path ? String(data.bin_path) : prev.command,
        args: data?.bin_path ? [] : prev.args,
        package: packageName,
      }));
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

  async function installCheckPackage(check: { package?: string; installer?: string }) {
    const inferred = inferMCPInstallSpec({ ...draft, package: check.package || draft.package, installer: check.installer || draft.installer });
    const packageName = String(check.package || draft.package || '').trim();
    if (!packageName) return;
    ui.showLoading(t('configMCPInstalling'));
    try {
      const r = await fetch(`/webui/api/mcp/install${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ package: packageName, installer: check.installer || inferred.installer }),
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
      setDraft((prev) => ({
        ...prev,
        command: data?.bin_path ? String(data.bin_path) : prev.command,
        args: data?.bin_path ? [] : prev.args,
        package: packageName,
        installer: check.installer || prev.installer,
      }));
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

  const activeCheck = useMemo(() => {
    if (!draftName.trim()) return null;
    return mcpServerChecks.find((item) => item.name === draftName.trim()) || null;
  }, [draftName, mcpServerChecks]);

  return (
    <div className="p-4 md:p-8 w-full space-y-6">
      <div className="flex items-start justify-between gap-3 flex-wrap">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('mcpServices')}</h1>
          <p className="text-sm text-zinc-500 mt-1">{t('mcpServicesHint')}</p>
        </div>
        <div className="flex items-center gap-3 flex-wrap">
          <FixedButton onClick={async () => { await loadConfig(true); await refreshMCPTools(); }} label={t('reload')}>
            <RefreshCw className="w-4 h-4" />
          </FixedButton>
          <FixedButton onClick={openCreateModal} variant="primary" label={t('add')}>
            <Plus className="w-4 h-4" />
          </FixedButton>
        </div>
      </div>

      <div className="brand-card-subtle ui-subpanel rounded-2xl p-4 space-y-4">
        <div className="flex items-center justify-between gap-2 flex-wrap">
          <div className="text-sm font-semibold text-zinc-200">{t('configMCPServers')}</div>
          <div className="text-xs text-zinc-500">{serverEntries.length}</div>
        </div>
        <div className="grid grid-cols-1 xl:grid-cols-2 gap-4">
          {serverEntries.map(([name, server]) => {
            const transport = String(server?.transport || 'stdio');
            const check = mcpServerChecks.find((item) => item.name === name);
            return (
              <div key={name} className="brand-card ui-panel rounded-[24px] p-4 space-y-4">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0 space-y-1">
                    <div className="flex items-center gap-2 flex-wrap">
                      <div className="font-mono text-sm text-zinc-100">{name}</div>
                      <span className={`ui-pill inline-flex items-center rounded-full px-2 py-0.5 text-[11px] border ${server?.enabled ? 'ui-pill-success' : 'ui-pill-neutral'}`}>
                        {server?.enabled ? t('enable') : t('paused')}
                      </span>
                      <span className="inline-flex items-center rounded-full px-2 py-0.5 text-[11px] bg-zinc-900/70 text-zinc-400 border border-zinc-800">
                        {transport}
                      </span>
                    </div>
                    <div className="text-sm text-zinc-400 break-all">
                      {transport === 'stdio' ? String(server?.command || '-') : String(server?.url || '-')}
                    </div>
                    {server?.description && (
                      <div className="text-xs text-zinc-500 line-clamp-2">{String(server.description)}</div>
                    )}
                  </div>
                  <div className="flex items-center gap-2 shrink-0">
                    <FixedButton onClick={() => openEditModal(name, server)} radius="xl" label={t('edit')}>
                      <Pencil className="w-4 h-4" />
                    </FixedButton>
                    <FixedButton onClick={() => removeServer(name)} variant="danger" radius="xl" label={t('delete')}>
                      <Trash2 className="w-4 h-4" />
                    </FixedButton>
                  </div>
                </div>

                <div className="grid grid-cols-2 gap-3 text-xs">
                  <div className="ui-code-panel px-3 py-2">
                    <div className="text-zinc-500">package</div>
                    <div className="mt-1 text-zinc-200 break-all">{String(server?.package || '-')}</div>
                  </div>
                  <div className="ui-code-panel px-3 py-2">
                    <div className="text-zinc-500">args</div>
                    <div className="mt-1 text-zinc-200">{Array.isArray(server?.args) ? server.args.length : 0}</div>
                  </div>
                  <div className="ui-code-panel px-3 py-2">
                    <div className="text-zinc-500">permission</div>
                    <div className="mt-1 text-zinc-200">{String(server?.permission || 'workspace')}</div>
                  </div>
                </div>

                {check && check.status !== 'ok' && check.status !== 'disabled' && check.status !== 'not_applicable' && (
                  <div className="ui-notice-warning rounded-2xl border px-3 py-2 text-xs">
                    <div>{check.message || t('configMCPCommandMissing')}</div>
                    {check.package && (
                      <div className="mt-1 text-amber-300/80">{t('configMCPInstallSuggested', { pkg: check.package })}</div>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
        {serverEntries.length === 0 && (
          <div className="ui-subpanel rounded-[24px] border-dashed px-6 py-10 text-center text-sm text-zinc-500">
            {t('configNoMCPServers')}
          </div>
        )}
      </div>

      <AnimatePresence>
        {modalOpen && (
          <motion.div
            className="fixed inset-0 z-[120] flex items-center justify-center p-4"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
          >
            <motion.div
              className="ui-overlay-strong absolute inset-0 backdrop-blur-sm"
              onClick={closeModal}
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
            />
            <motion.div
              className="brand-card ui-panel relative z-[1] w-full max-w-4xl shadow-2xl overflow-hidden"
              initial={{ opacity: 0, scale: 0.96, y: 16 }}
              animate={{ opacity: 1, scale: 1, y: 0 }}
              exit={{ opacity: 0, scale: 0.96, y: 16 }}
            >
              <div className="flex items-center justify-between gap-3 border-b border-zinc-800 px-6 py-4 dark:border-zinc-700">
                <div>
                  <div className="text-lg font-semibold text-zinc-100">
                    {editingName ? `${t('edit')} MCP` : `${t('add')} MCP`}
                  </div>
                  <div className="text-xs text-zinc-500 mt-1">{t('mcpServicesHint')}</div>
                </div>
                <FixedButton onClick={closeModal} radius="xl" label={t('close')}>
                  <X className="w-4 h-4" />
                </FixedButton>
              </div>

              <div className="max-h-[80vh] overflow-y-auto px-6 py-5 space-y-5">
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  <FormField label={t('configNewMCPServerName')} labelClassName="text-xs text-zinc-400" className="space-y-2">
                    <Input value={draftName} onChange={(e) => setDraftName(e.target.value)} className="h-11 px-3" />
                  </FormField>
                  <FormField label={t('configLabels.description')} labelClassName="text-xs text-zinc-400" className="space-y-2">
                    <Input value={draft.description} onChange={(e) => updateDraftField('description', e.target.value)} className="h-11 px-3" />
                  </FormField>
                </div>

                <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
                  <FormField label="transport" labelClassName="text-xs text-zinc-400" className="space-y-2">
                    <Select value={draft.transport} onChange={(e) => updateDraftField('transport', e.target.value)} className="h-11 px-3">
                      <option value="stdio">stdio</option>
                      <option value="http">http</option>
                      <option value="streamable_http">streamable_http</option>
                      <option value="sse">sse</option>
                    </Select>
                  </FormField>
                  <FormField label="enabled" labelClassName="text-xs text-zinc-400" className="space-y-2">
                    <div className="ui-toggle-card flex h-11 items-center rounded-xl px-3">
                      <Checkbox checked={draft.enabled} onChange={(e) => updateDraftField('enabled', e.target.checked)} />
                    </div>
                  </FormField>
                  {draft.transport === 'stdio' && (
                    <FormField label="permission" labelClassName="text-xs text-zinc-400" className="space-y-2">
                      <Select value={draft.permission} onChange={(e) => updateDraftField('permission', e.target.value)} className="h-11 px-3">
                        <option value="workspace">workspace</option>
                        <option value="full">full</option>
                      </Select>
                    </FormField>
                  )}
                  {draft.transport === 'stdio' && (
                    <FormField label={t('configLabels.package')} labelClassName="text-xs text-zinc-400" className="space-y-2">
                      <Input value={draft.package} onChange={(e) => updateDraftField('package', e.target.value)} className="h-11 px-3" />
                    </FormField>
                  )}
                </div>

                {draft.transport === 'stdio' ? (
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <label className="space-y-2">
                      <div className="text-xs text-zinc-400">{t('configLabels.command')}</div>
                      <Input value={draft.command} onChange={(e) => updateDraftField('command', e.target.value)} className="h-11 px-3" />
                    </label>
                    <label className="space-y-2">
                      <div className="text-xs text-zinc-400">{t('configLabels.working_dir')}</div>
                      <Input value={draft.working_dir} onChange={(e) => updateDraftField('working_dir', e.target.value)} className="h-11 px-3" />
                    </label>
                  </div>
                ) : (
                  <label className="space-y-2">
                    <div className="text-xs text-zinc-400">{t('configLabels.url')}</div>
                    <Input value={draft.url} onChange={(e) => updateDraftField('url', e.target.value)} className="h-11 px-3" />
                  </label>
                )}

                {draft.transport === 'stdio' && (
                  <div className="ui-soft-panel rounded-[24px] p-4 space-y-3">
                    <div className="flex items-center justify-between gap-3 flex-wrap">
                      <div>
                        <div className="text-sm font-medium text-zinc-200">Args</div>
                        <div className="text-xs text-zinc-500">{t('configMCPArgsEnterHint')}</div>
                      </div>
                      <Button onClick={installDraftPackage} variant="success" size="xs_tall" gap="2">
                        <Package className="w-4 h-4" /> {t('install')}
                      </Button>
                    </div>

                    <div className="flex max-h-28 flex-wrap gap-2 overflow-y-auto pr-1">
                      {draft.args.map((arg, index) => (
                        <span key={`draft-arg-${index}`} className="inline-flex max-w-full items-center gap-2 rounded-xl ui-soft-panel px-2.5 py-1.5 text-[11px] text-zinc-700 dark:text-zinc-200">
                          <span className="font-mono break-all">{arg}</span>
                          <button type="button" onClick={() => removeDraftArg(index)} className="shrink-0 text-zinc-400 hover:text-zinc-100">x</button>
                        </span>
                      ))}
                    </div>
                    <Input
                      value={draftArgInput}
                      onChange={(e) => setDraftArgInput(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault();
                          addDraftArg(draftArgInput);
                        }
                      }}
                      onBlur={() => addDraftArg(draftArgInput)}
                      placeholder={t('configMCPArgsEnterHint')}
                      className="h-11 px-3"
                    />
                  </div>
                )}

                {activeCheck && activeCheck.status !== 'ok' && activeCheck.status !== 'disabled' && activeCheck.status !== 'not_applicable' && (
                  <div className="ui-notice-warning rounded-2xl border px-4 py-3 text-xs space-y-2">
                    <div>{activeCheck.message || t('configMCPCommandMissing')}</div>
                    {activeCheck.package && (
                      <div className="text-amber-300/80">{t('configMCPInstallSuggested', { pkg: activeCheck.package })}</div>
                    )}
                    {activeCheck.installable && (
                      <Button onClick={() => installCheckPackage(activeCheck)} variant="warning" size="xs_tall">
                        <Wrench className="w-4 h-4" /> {t('install')}
                      </Button>
                    )}
                  </div>
                )}
              </div>

              <div className="flex items-center justify-between gap-3 border-t border-zinc-800 px-6 py-4 dark:border-zinc-700">
                <div className="text-xs text-zinc-500">
                  {activeCheck?.resolved ? activeCheck.resolved : ''}
                </div>
                <div className="flex items-center gap-2">
                  {editingName && (
                    <Button onClick={() => removeServer(editingName)} variant="danger" gap="2">
                      <Trash2 className="w-4 h-4" /> {t('delete')}
                    </Button>
                  )}
                  <Button onClick={closeModal} size="sm">{t('cancel')}</Button>
                  <Button onClick={saveServer} variant="primary" gap="2">
                    <Save className="w-4 h-4" /> {t('saveChanges')}
                  </Button>
                </div>
              </div>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
};

export default MCP;
