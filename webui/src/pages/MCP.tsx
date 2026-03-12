import React, { useEffect, useMemo, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { Plus, RefreshCw, Save, Trash2, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
import { Button, FixedButton } from '../components/ui/Button';
import EmptyState from '../components/data-display/EmptyState';
import { ModalBackdrop, ModalBody, ModalCard, ModalFooter, ModalHeader, ModalShell } from '../components/ui/ModalFrame';
import MCPServerCard from '../components/mcp/MCPServerCard';
import MCPServerEditor from '../components/mcp/MCPServerEditor';
import PageHeader from '../components/layout/PageHeader';
import SectionHeader from '../components/layout/SectionHeader';
import ToolbarRow from '../components/layout/ToolbarRow';
import { cloneJSON } from '../utils/object';

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
      env: typeof server?.env === 'object' && server?.env ? cloneJSON(server.env) : {},
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

    const next = cloneJSON(cfg || {});
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
      const next = cloneJSON(cfg || {});
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
      <PageHeader
        title={t('mcpServices')}
        subtitle={t('mcpServicesHint')}
        actions={(
          <ToolbarRow className="gap-3">
          <FixedButton onClick={async () => { await loadConfig(true); await refreshMCPTools(); }} label={t('reload')}>
            <RefreshCw className="w-4 h-4" />
          </FixedButton>
          <FixedButton onClick={openCreateModal} variant="primary" label={t('add')}>
            <Plus className="w-4 h-4" />
          </FixedButton>
          </ToolbarRow>
        )}
      />

      <div className="brand-card-subtle ui-subpanel rounded-2xl p-4 space-y-4">
        <SectionHeader title={t('configMCPServers')} meta={serverEntries.length} />
        <div className="grid grid-cols-1 xl:grid-cols-2 gap-4">
          {serverEntries.map(([name, server]) => {
            const check = mcpServerChecks.find((item) => item.name === name);
            return (
              <MCPServerCard
                key={name}
                check={check}
                name={name}
                onEdit={() => openEditModal(name, server)}
                onRemove={() => removeServer(name)}
                server={server}
                t={t}
              />
            );
          })}
        </div>
        {serverEntries.length === 0 && (
          <EmptyState
            centered
            dashed
            message={t('configNoMCPServers')}
            className="ui-subpanel rounded-[24px] px-6 py-10"
          />
        )}
      </div>

      <AnimatePresence>
        {modalOpen && (
          <motion.div
            className="fixed inset-0 z-[120]"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
          >
            <ModalShell className="z-[120]">
              <ModalBackdrop onClick={closeModal} />
              <motion.div
                className="relative z-[1] w-full max-w-4xl"
                initial={{ opacity: 0, scale: 0.96, y: 16 }}
                animate={{ opacity: 1, scale: 1, y: 0 }}
                exit={{ opacity: 0, scale: 0.96, y: 16 }}
              >
                <ModalCard className="ui-panel">
                  <ModalHeader
                    className="px-6 py-4"
                    title={editingName ? `${t('edit')} MCP` : `${t('add')} MCP`}
                    subtitle={t('mcpServicesHint')}
                    actions={
                      <FixedButton onClick={closeModal} radius="xl" label={t('close')}>
                        <X className="w-4 h-4" />
                      </FixedButton>
                    }
                  />

                  <ModalBody className="max-h-[80vh] overflow-y-auto px-6 py-5">
                    <MCPServerEditor
                      activeCheck={activeCheck}
                      addDraftArg={addDraftArg}
                      draft={draft}
                      draftArgInput={draftArgInput}
                      draftName={draftName}
                      installCheckPackage={installCheckPackage}
                      installDraftPackage={installDraftPackage}
                      removeDraftArg={removeDraftArg}
                      setDraftArgInput={setDraftArgInput}
                      setDraftName={setDraftName}
                      t={t}
                      updateDraftField={updateDraftField}
                    />
                  </ModalBody>

                  <ModalFooter className="justify-between px-6 py-4">
                    <div className="text-xs text-zinc-500">
                      {activeCheck?.resolved ? activeCheck.resolved : ''}
                    </div>
                    <div className="flex items-center gap-2">
                      {editingName && (
                        <FixedButton onClick={() => removeServer(editingName)} variant="danger" label={t('delete')}>
                          <Trash2 className="w-4 h-4" />
                        </FixedButton>
                      )}
                      <Button onClick={closeModal} size="sm">{t('cancel')}</Button>
                        <Button onClick={saveServer} variant="primary" size="sm" radius="lg" gap="1">
                          <Save className="w-4 h-4" />
                          {t('saveChanges')}
                        </Button>
                      </div>
                  </ModalFooter>
                </ModalCard>
              </motion.div>
            </ModalShell>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
};

export default MCP;
