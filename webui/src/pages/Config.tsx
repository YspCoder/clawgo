import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
import { TextareaField } from '../components/ui/FormControls';
import { GatewayArtifactsSection, GatewayDispatchSection, GatewayP2PSection } from '../components/config/GatewayConfigSection';
import { ConfigDiffModal, ConfigHeader, ConfigSidebar, ConfigToolbar } from '../components/config/ConfigPageChrome';
import { buildDiffRows, formatTagRuleText, parseTagRuleText, setPath } from '../components/config/configUtils';
import { useConfigGatewayActions } from '../components/config/useConfigGatewayActions';
import { useConfigNavigation } from '../components/config/useConfigNavigation';
import { useConfigSaveAction } from '../components/config/useConfigSaveAction';
import RecursiveConfig from '../components/ui/RecursiveConfig';
import { cloneJSON } from '../utils/object';

const Config: React.FC = () => {
  const { t } = useTranslation();
  const ui = useUI();
  const { cfg, setCfg, cfgRaw, setCfgRaw, loadConfig, hotReloadFieldDetails, q, setConfigEditing, setToken } = useAppContext();
  const [showRaw, setShowRaw] = useState(false);
  const [basicMode, setBasicMode] = useState(true);
  const [hotOnly, setHotOnly] = useState(false);
  const [search, setSearch] = useState('');
  const [selectedTop, setSelectedTop] = useState<string>('');
  const [baseline, setBaseline] = useState<any>(null);
  const [showDiff, setShowDiff] = useState(false);
  const { activeTop, configLabels, filteredTopKeys, hotReloadTabKey } = useConfigNavigation({
    basicMode,
    cfg,
    hotOnly,
    hotReloadFieldDetails,
    search,
    selectedTop,
    t,
  });
  const {
    addGatewayIceServer,
    removeGatewayIceServer,
    updateGatewayArtifactsField,
    updateGatewayDispatchField,
    updateGatewayIceServer,
    updateGatewayP2PField,
  } = useConfigGatewayActions({ setCfg });
  const { saveConfig } = useConfigSaveAction({
    cfg,
    cfgRaw,
    loadConfig,
    q,
    setBaseline,
    setConfigEditing,
    setToken,
    setShowDiff,
    showRaw,
    t,
    ui,
  });

  const currentPayload = useMemo(() => {
    if (showRaw) {
      try { return JSON.parse(cfgRaw); } catch { return cfg; }
    }
    return cfg;
  }, [showRaw, cfgRaw, cfg]);

  const diffRows = useMemo(() => {
    return buildDiffRows(baseline, currentPayload, t('configRoot'));
  }, [baseline, currentPayload]);

  const isDirty = useMemo(() => {
    if (baseline == null) return false;
    return JSON.stringify(baseline) !== JSON.stringify(currentPayload || {});
  }, [baseline, currentPayload]);

  useEffect(() => {
    if (baseline == null && cfg && Object.keys(cfg).length > 0) {
      setBaseline(cloneJSON(cfg));
    }
  }, [cfg, baseline]);

  useEffect(() => {
    setConfigEditing(isDirty);
    return () => setConfigEditing(false);
  }, [isDirty, setConfigEditing]);

  return (
    <div className="p-4 md:p-6 xl:p-8 w-full space-y-4 flex flex-col min-h-full">
      <ConfigHeader onSave={saveConfig} onShowForm={() => setShowRaw(false)} onShowRaw={() => setShowRaw(true)} showRaw={showRaw} t={t} />

      <ConfigToolbar
        basicMode={basicMode}
        hotOnly={hotOnly}
        onHotOnlyChange={setHotOnly}
        onReload={async () => {
          const reloaded = await loadConfig(true);
          setBaseline(cloneJSON(reloaded ?? cfg));
        }}
        onSearchChange={setSearch}
        onShowDiff={() => setShowDiff(true)}
        onToggleBasicMode={() => setBasicMode((value) => !value)}
        search={search}
        t={t}
      />

      <div className="flex-1 brand-card ui-border-subtle border rounded-2xl overflow-hidden flex flex-col shadow-sm min-h-[420px]">
        {!showRaw ? (
          <div className="flex-1 flex min-h-0">
            <ConfigSidebar
              activeTop={activeTop}
              configLabels={configLabels}
              filteredTopKeys={filteredTopKeys}
              hotReloadTabKey={hotReloadTabKey}
              onSelectTop={setSelectedTop}
              t={t}
            />

            <div className="flex-1 p-4 md:p-6 overflow-y-auto space-y-4">
              {activeTop === hotReloadTabKey && (
                <div className="space-y-3">
                  <div className="ui-text-primary text-sm font-semibold">{t('configHotFieldsFull')}</div>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-2 text-xs">
                    {hotReloadFieldDetails.map((it) => (
                      <div key={it.path} className="ui-soft-panel ui-border-subtle p-2 rounded-xl border">
                        <div className="ui-text-primary font-mono">{it.path}</div>
                        <div className="ui-text-secondary">{it.name || ''}{it.description ? ` · ${it.description}` : ''}</div>
                      </div>
                    ))}
                  </div>
                </div>
              )}
              {activeTop === 'gateway' && !showRaw && (
                <div className="brand-card-subtle rounded-2xl border border-zinc-800 p-3 space-y-3">
                  <GatewayP2PSection
                    addGatewayIceServer={addGatewayIceServer}
                    iceServers={Array.isArray((cfg as any)?.gateway?.nodes?.p2p?.ice_servers) ? (cfg as any).gateway.nodes.p2p.ice_servers : []}
                    onP2PFieldChange={updateGatewayP2PField}
                    onRemoveGatewayIceServer={removeGatewayIceServer}
                    onUpdateGatewayIceServer={updateGatewayIceServer}
                    p2p={(cfg as any)?.gateway?.nodes?.p2p}
                    t={t}
                  />
                  <GatewayDispatchSection
                    dispatch={(cfg as any)?.gateway?.nodes?.dispatch}
                    formatTagRuleText={formatTagRuleText}
                    onDispatchFieldChange={updateGatewayDispatchField}
                    parseTagRuleText={parseTagRuleText}
                    t={t}
                  />
                  <GatewayArtifactsSection
                    artifacts={(cfg as any)?.gateway?.nodes?.artifacts}
                    onArtifactsFieldChange={updateGatewayArtifactsField}
                    t={t}
                  />
                </div>
              )}
              {activeTop && activeTop !== hotReloadTabKey && activeTop !== 'providers' && activeTop !== 'gateway' ? (
                <RecursiveConfig
                  data={(cfg as any)?.[activeTop] || {}}
                  labels={configLabels}
                  path={activeTop}
                  hotPaths={hotReloadFieldDetails.map((x) => x.path)}
                  onlyHot={hotOnly}
                  onChange={(path, val) => setCfg(v => setPath(v, path, val))}
                />
              ) : (
                activeTop !== 'providers' && activeTop !== 'gateway' && <div className="text-zinc-500 text-sm">{t('configNoGroups')}</div>
              )}
            </div>
          </div>
        ) : (
          <TextareaField
            value={cfgRaw}
            onChange={(e) => setCfgRaw(e.target.value)}
            monospace
            className="flex-1 w-full bg-zinc-950/35 p-6 text-zinc-300 focus:outline-none resize-none border-0 rounded-none"
            spellCheck={false}
          />
        )}
      </div>

      {showDiff && <ConfigDiffModal diffRows={diffRows} onClose={() => setShowDiff(false)} t={t} />}
    </div>
  );
};

export default Config;
