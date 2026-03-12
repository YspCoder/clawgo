import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { RefreshCw, Save } from 'lucide-react';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
import { Button, FixedButton } from '../components/ui/Button';
import PageHeader from '../components/layout/PageHeader';
import { ConfigDiffModal } from '../components/config/ConfigPageChrome';
import { ProviderProxyCard, ProviderRuntimeDrawer, ProviderRuntimeSummary, ProviderRuntimeToolbar } from '../components/config/ProviderConfigSection';
import { buildDiffRows, RuntimeWindow } from '../components/config/configUtils';
import { useConfigProviderActions } from '../components/config/useConfigProviderActions';
import { useConfigRuntimeView } from '../components/config/useConfigRuntimeView';
import { useConfigSaveAction } from '../components/config/useConfigSaveAction';
import { cloneJSON } from '../utils/object';

const Providers: React.FC = () => {
  const { t } = useTranslation();
  const ui = useUI();
  const { cfg, setCfg, cfgRaw, loadConfig, q, setConfigEditing, providerRuntimeItems, setToken } = useAppContext();
  const [newProxyName, setNewProxyName] = useState('');
  const [runtimeAutoRefresh, setRuntimeAutoRefresh] = useState(true);
  const [runtimeRefreshSec, setRuntimeRefreshSec] = useState(10);
  const [runtimeWindow, setRuntimeWindow] = useState<RuntimeWindow>('24h');
  const [runtimeDrawerProvider, setRuntimeDrawerProvider] = useState('');
  const [selectedProviderTab, setSelectedProviderTab] = useState('');
  const [showDiff, setShowDiff] = useState(false);
  const [baseline, setBaseline] = useState<any>(null);
  const oauthImportInputRef = useRef<HTMLInputElement | null>(null);
  const latestProviderRuntimeRef = useRef<any[]>([]);
  const [displayedProviderRuntimeItems, setDisplayedProviderRuntimeItems] = useState<any[]>([]);
  const [oauthAccounts, setOAuthAccounts] = useState<Record<string, Array<any>>>({});
  const [oauthAccountsLoading, setOAuthAccountsLoading] = useState<Record<string, boolean>>({});
  const [oauthAccountsLoaded, setOAuthAccountsLoaded] = useState<Record<string, boolean>>({});

  const providerEntries = useMemo(() => {
    const providers = (((cfg as any)?.models || {}) as any)?.providers || {};
    const entries: Array<[string, any]> = [];
    if (providers && typeof providers === 'object' && !Array.isArray(providers)) {
      Object.entries(providers).forEach(([name, value]) => entries.push([name, value]));
    }
    return entries;
  }, [cfg]);

  const providerRuntimeMap = useMemo(() => {
    const entries = Array.isArray(displayedProviderRuntimeItems) ? displayedProviderRuntimeItems : [];
    return Object.fromEntries(entries.map((item: any) => [String(item?.name || ''), item]));
  }, [displayedProviderRuntimeItems]);

  const activeProviderName = useMemo(() => {
    if (providerEntries.length === 0) return '';
    if (providerEntries.some(([name]) => name === selectedProviderTab)) return selectedProviderTab;
    return providerEntries[0]?.[0] || '';
  }, [providerEntries, selectedProviderTab]);

  const activeProviderEntry = useMemo(
    () => providerEntries.find(([name]) => name === activeProviderName) || null,
    [providerEntries, activeProviderName],
  );

  useEffect(() => {
    latestProviderRuntimeRef.current = Array.isArray(providerRuntimeItems) ? providerRuntimeItems : [];
    if (runtimeAutoRefresh && runtimeRefreshSec <= 1) {
      setDisplayedProviderRuntimeItems(latestProviderRuntimeRef.current);
    }
  }, [providerRuntimeItems, runtimeAutoRefresh, runtimeRefreshSec]);

  useEffect(() => {
    if (!runtimeAutoRefresh) return;
    setDisplayedProviderRuntimeItems(latestProviderRuntimeRef.current);
    const timer = window.setInterval(() => {
      setDisplayedProviderRuntimeItems(latestProviderRuntimeRef.current);
    }, Math.max(1, runtimeRefreshSec) * 1000);
    return () => window.clearInterval(timer);
  }, [runtimeAutoRefresh, runtimeRefreshSec]);

  useEffect(() => {
    if (!activeProviderName && providerEntries.length > 0) {
      setSelectedProviderTab(providerEntries[0][0]);
    }
  }, [activeProviderName, providerEntries]);

  useEffect(() => {
    if (baseline == null && cfg && Object.keys(cfg).length > 0) {
      setBaseline(cloneJSON(cfg));
    }
  }, [cfg, baseline]);

  const diffRows = useMemo(() => buildDiffRows(baseline, cfg, t('configRoot')), [baseline, cfg, t]);
  const isDirty = useMemo(() => {
    if (baseline == null) return false;
    return JSON.stringify(baseline) !== JSON.stringify(cfg || {});
  }, [baseline, cfg]);

  useEffect(() => {
    setConfigEditing(isDirty);
    return () => setConfigEditing(false);
  }, [isDirty, setConfigEditing]);

  const { filterRuntimeEvents, renderRuntimeEventList, runtimeSectionOpen, toggleRuntimeSection } = useConfigRuntimeView(runtimeWindow);
  const {
    addProxy,
    clearAPIKeyCooldown,
    clearOAuthCooldown,
    clearProviderHistory,
    deleteOAuthAccount,
    exportProviderHistory,
    loadOAuthAccounts,
    onOAuthImportChange,
    refreshOAuthAccount,
    refreshProviderRuntimeNow,
    removeProxy,
    startOAuthLogin,
    triggerOAuthImport,
    updateProxyField,
  } = useConfigProviderActions({
    inputRef: oauthImportInputRef,
    loadConfig,
    onProviderRuntimeRefreshed: () => setDisplayedProviderRuntimeItems(latestProviderRuntimeRef.current),
    providerRuntimeMap,
    q,
    setCfg,
    setNewProxyName,
    setOAuthAccounts,
    t,
    ui,
  });

  const loadOAuthAccountsNow = useCallback(async (name: string) => {
    if (!name) return;
    setOAuthAccountsLoading((prev) => ({ ...prev, [name]: true }));
    try {
      await loadOAuthAccounts(name);
      setOAuthAccountsLoaded((prev) => ({ ...prev, [name]: true }));
    } finally {
      setOAuthAccountsLoading((prev) => ({ ...prev, [name]: false }));
    }
  }, [loadOAuthAccounts]);

  useEffect(() => {
    providerEntries.forEach(([name, p]) => {
      if (!['oauth', 'hybrid'].includes(String(p?.auth || ''))) return;
      if (oauthAccountsLoaded[name] || oauthAccountsLoading[name]) return;
      void loadOAuthAccountsNow(name);
    });
  }, [loadOAuthAccountsNow, oauthAccountsLoaded, oauthAccountsLoading, providerEntries]);

  useEffect(() => {
    if (!activeProviderEntry) return;
    const [name, provider] = activeProviderEntry;
    if (!['oauth', 'hybrid'].includes(String(provider?.auth || ''))) return;
    if (oauthAccountsLoaded[name] || oauthAccountsLoading[name]) return;
    void loadOAuthAccountsNow(name);
  }, [activeProviderEntry, loadOAuthAccountsNow, oauthAccountsLoaded, oauthAccountsLoading]);

  useEffect(() => {
    const oauthProviderNames = new Set(
      providerEntries
        .filter(([, provider]) => ['oauth', 'hybrid'].includes(String(provider?.auth || '')))
        .map(([name]) => name),
    );
    setOAuthAccountsLoaded((prev) => {
      const next = Object.fromEntries(Object.entries(prev).filter(([name]) => oauthProviderNames.has(name)));
      return Object.keys(next).length === Object.keys(prev).length ? prev : next;
    });
    setOAuthAccountsLoading((prev) => {
      const next = Object.fromEntries(Object.entries(prev).filter(([name]) => oauthProviderNames.has(name)));
      return Object.keys(next).length === Object.keys(prev).length ? prev : next;
    });
  }, [providerEntries]);

  const { saveConfig } = useConfigSaveAction({
    cfg,
    cfgRaw,
    loadConfig,
    q,
    setBaseline,
    setConfigEditing,
    setToken,
    setShowDiff,
    showRaw: false,
    t,
    ui,
  });

  return (
    <div className="p-4 md:p-6 xl:p-8 w-full space-y-4 flex flex-col min-h-full">
      <input ref={oauthImportInputRef} type="file" accept=".json,application/json" className="hidden" onChange={onOAuthImportChange} />

      <PageHeader
        title={t('providers')}
        titleClassName="ui-text-primary"
        actions={
          <div className="flex items-center gap-2 flex-wrap justify-end">
          <FixedButton onClick={async () => {
            const reloaded = await loadConfig(true);
            setBaseline(cloneJSON(reloaded ?? cfg));
          }} label={t('reload')}>
            <RefreshCw className="w-4 h-4" />
          </FixedButton>
          <Button onClick={() => setShowDiff(true)} size="sm">{t('configDiffPreview')}</Button>
            <Button onClick={saveConfig} variant="primary" size="sm" radius="lg" gap="1">
              <Save className="w-4 h-4" />
              {t('saveChanges')}
            </Button>
            </div>
          }
        />

      <div className="brand-card ui-border-subtle border rounded-2xl p-4 md:p-6 space-y-4">
        <ProviderRuntimeToolbar
          newProxyName={newProxyName}
          onAddProxy={() => addProxy(newProxyName)}
          onNewProxyNameChange={setNewProxyName}
          onRefreshRuntime={refreshProviderRuntimeNow}
          onRuntimeAutoRefreshChange={setRuntimeAutoRefresh}
          onRuntimeRefreshSecChange={setRuntimeRefreshSec}
          onRuntimeWindowChange={setRuntimeWindow}
          runtimeAutoRefresh={runtimeAutoRefresh}
          runtimeRefreshSec={runtimeRefreshSec}
          runtimeWindow={runtimeWindow}
          t={t}
        />

        <div className="rounded-xl border border-zinc-800 bg-zinc-950/40 px-3 py-2 text-xs text-zinc-400">
          {t('providersIntroBefore')}
          <span className="font-mono text-zinc-200">oauth</span>
          {t('providersIntroMiddle')}
          <span className="font-mono text-zinc-200">hybrid</span>
          {t('providersIntroAfter')}
        </div>

        {providerEntries.length > 0 ? (
          <>
            <div className="flex flex-wrap gap-2">
              {providerEntries.map(([name, p]) => {
                const auth = String(p?.auth || 'bearer');
                const active = name === activeProviderName;
                return (
                  <Button
                    key={`provider-tab-${name}`}
                    onClick={() => setSelectedProviderTab(name)}
                    variant={active ? 'primary' : 'neutral'}
                    size="sm"
                    radius="xl"
                  >
                    {name}
                    <span className="ml-1 text-[11px] opacity-80">{auth}</span>
                  </Button>
                );
              })}
            </div>

            {activeProviderEntry && (
              <ProviderProxyCard
                key={activeProviderEntry[0]}
                name={activeProviderEntry[0]}
                oauthAccounts={oauthAccounts[activeProviderEntry[0]] || []}
                oauthAccountsLoading={!!oauthAccountsLoading[activeProviderEntry[0]]}
                onClearOAuthCooldown={(credentialFile) => clearOAuthCooldown(activeProviderEntry[0], credentialFile)}
                onDeleteOAuthAccount={(credentialFile) => deleteOAuthAccount(activeProviderEntry[0], credentialFile)}
                onFieldChange={(field, value) => updateProxyField(activeProviderEntry[0], field, value)}
                onLoadOAuthAccounts={() => loadOAuthAccountsNow(activeProviderEntry[0])}
                onRefreshOAuthAccount={(credentialFile) => refreshOAuthAccount(activeProviderEntry[0], credentialFile)}
                onRemove={() => removeProxy(activeProviderEntry[0])}
                onStartOAuthLogin={() => startOAuthLogin(activeProviderEntry[0], activeProviderEntry[1])}
                onTriggerOAuthImport={() => triggerOAuthImport(activeProviderEntry[0], activeProviderEntry[1])}
                proxy={activeProviderEntry[1]}
                runtimeItem={providerRuntimeMap[activeProviderEntry[0]]}
                runtimeSummary={providerRuntimeMap[activeProviderEntry[0]] ? (
                  <ProviderRuntimeSummary
                    item={providerRuntimeMap[activeProviderEntry[0]]}
                    name={activeProviderEntry[0]}
                    onClearApiCooldown={() => clearAPIKeyCooldown(activeProviderEntry[0])}
                    onClearHistory={() => clearProviderHistory(activeProviderEntry[0])}
                    onExportHistory={() => exportProviderHistory(activeProviderEntry[0])}
                    onOpenHistory={() => setRuntimeDrawerProvider(activeProviderEntry[0])}
                    renderRuntimeEventList={renderRuntimeEventList}
                    runtimeSectionOpen={(section) => runtimeSectionOpen(activeProviderEntry[0], section)}
                    toggleRuntimeSection={(section) => toggleRuntimeSection(activeProviderEntry[0], section)}
                    filterRuntimeEvents={filterRuntimeEvents}
                  />
                ) : null}
                t={t}
              />
            )}
          </>
        ) : (
          <div className="text-xs text-zinc-500">{t('configNoCustomProviders')}</div>
        )}
      </div>

      {runtimeDrawerProvider && providerRuntimeMap[runtimeDrawerProvider] && (
        <ProviderRuntimeDrawer
          filterRuntimeEvents={filterRuntimeEvents}
          item={providerRuntimeMap[runtimeDrawerProvider]}
          name={runtimeDrawerProvider}
          onClearHistory={() => clearProviderHistory(runtimeDrawerProvider)}
          onClose={() => setRuntimeDrawerProvider('')}
          onExportHistory={() => exportProviderHistory(runtimeDrawerProvider)}
          renderRuntimeEventList={renderRuntimeEventList}
        />
      )}

      {showDiff && <ConfigDiffModal diffRows={diffRows} onClose={() => setShowDiff(false)} t={t} />}
    </div>
  );
};

export default Providers;
