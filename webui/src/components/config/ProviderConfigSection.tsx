import React from 'react';
import { Download, FolderOpen, LogIn, Plus, RefreshCw, RotateCcw, Trash2, Upload } from 'lucide-react';
import { Button, FixedButton } from '../Button';
import { CheckboxField, PanelField, SelectField, TextField } from '../FormControls';

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const DENSE_PROXY_FIELD_CLASS = 'bg-zinc-950/70 border-zinc-800';

export function ProxyTextField({ className, ...props }: React.ComponentProps<typeof TextField>) {
  return <TextField dense {...props} className={joinClasses(DENSE_PROXY_FIELD_CLASS, className)} />;
}

export function ProxySelectField({ className, ...props }: React.ComponentProps<typeof SelectField>) {
  return <SelectField dense {...props} className={joinClasses(DENSE_PROXY_FIELD_CLASS, className)} />;
}

type RuntimeSection = 'candidates' | 'hits' | 'errors' | 'changes';

type ProviderRuntimeToolbarProps = {
  newProxyName: string;
  onAddProxy: () => void;
  onNewProxyNameChange: (value: string) => void;
  onRefreshRuntime: () => void;
  onRuntimeAutoRefreshChange: (checked: boolean) => void;
  onRuntimeRefreshSecChange: (value: number) => void;
  onRuntimeWindowChange: (value: 'all' | '1h' | '24h' | '7d') => void;
  runtimeAutoRefresh: boolean;
  runtimeRefreshSec: number;
  runtimeWindow: 'all' | '1h' | '24h' | '7d';
  t: (key: string) => string;
};

export function ProviderRuntimeToolbar({
  newProxyName,
  onAddProxy,
  onNewProxyNameChange,
  onRefreshRuntime,
  onRuntimeAutoRefreshChange,
  onRuntimeRefreshSecChange,
  onRuntimeWindowChange,
  runtimeAutoRefresh,
  runtimeRefreshSec,
  runtimeWindow,
  t,
}: ProviderRuntimeToolbarProps) {
  return (
    <div className="flex items-center justify-between gap-2 flex-wrap">
      <div className="text-sm font-semibold text-zinc-200">{t('configProxies')}</div>
      <div className="flex items-center gap-2">
        <FixedButton onClick={onRefreshRuntime} variant="neutral" radius="lg" label={t('providersRefreshRuntime')}>
          <RefreshCw className="w-4 h-4" />
        </FixedButton>
        <label className="flex items-center gap-2 rounded-xl border border-zinc-800 bg-zinc-900/30 px-2 py-1.5 text-[11px] text-zinc-300">
          <CheckboxField checked={runtimeAutoRefresh} onChange={(e) => onRuntimeAutoRefreshChange(e.target.checked)} />
          {t('providersAutoRefresh')}
        </label>
        <SelectField dense value={String(runtimeRefreshSec)} onChange={(e) => onRuntimeRefreshSecChange(Number(e.target.value || 10))} className="bg-zinc-900/70 border-zinc-700">
          <option value="2">2s</option>
          <option value="5">5s</option>
          <option value="10">10s</option>
          <option value="30">30s</option>
        </SelectField>
        <SelectField dense value={runtimeWindow} onChange={(e) => onRuntimeWindowChange(e.target.value as 'all' | '1h' | '24h' | '7d')} className="bg-zinc-900/70 border-zinc-700">
          <option value="1h">{t('providersRuntime1h')}</option>
          <option value="24h">{t('providersRuntime24h')}</option>
          <option value="7d">{t('providersRuntime7d')}</option>
          <option value="all">{t('providersRuntimeAll')}</option>
        </SelectField>
        <TextField dense value={newProxyName} onChange={(e) => onNewProxyNameChange(e.target.value)} placeholder={t('configNewProviderName')} className="bg-zinc-900/70 border-zinc-700" />
        <FixedButton onClick={onAddProxy} variant="primary" radius="lg" label={t('add')}>
          <Plus className="w-4 h-4" />
        </FixedButton>
      </div>
    </div>
  );
}

type ProviderRuntimeSummaryProps = {
  item: any;
  name: string;
  onClearApiCooldown: () => void;
  onClearHistory: () => void;
  onExportHistory: () => void;
  onOpenHistory: () => void;
  renderRuntimeEventList: (items: any[], prefix: string) => React.ReactNode;
  runtimeSectionOpen: (section: RuntimeSection) => boolean;
  toggleRuntimeSection: (section: RuntimeSection) => void;
  filterRuntimeEvents: (items: any[]) => any[];
};

export function ProviderRuntimeSummary({
  item,
  name,
  onClearApiCooldown,
  onClearHistory,
  onExportHistory,
  onOpenHistory,
  renderRuntimeEventList,
  runtimeSectionOpen,
  toggleRuntimeSection,
  filterRuntimeEvents,
}: ProviderRuntimeSummaryProps) {
  const hits = filterRuntimeEvents(item?.recent_hits);
  const errors = filterRuntimeEvents(item?.recent_errors);
  const changes = filterRuntimeEvents(item?.recent_changes);

  return (
    <div className="md:col-span-7 rounded-lg border border-zinc-800 bg-zinc-950/40 p-2 text-[11px] text-zinc-400">
      <div>runtime auth: {String(item?.auth || '-')}</div>
      <div>api key health: {item?.api_state?.health_score ?? 100} · failures: {item?.api_state?.failure_count ?? 0} · cooldown: {item?.api_state?.cooldown_until || '-'}</div>
      <div>api key token: {item?.api_state?.token_masked || '-'}</div>
      <div>last success: {item?.last_success ? `${item.last_success.when || '-'} ${item.last_success.kind || '-'} ${item.last_success.target || '-'}` : '-'}</div>
      {Array.isArray(item?.oauth_accounts) && item.oauth_accounts.length > 0 && (
        <div>oauth accounts: {item.oauth_accounts.length} · {item.oauth_accounts.map((account: any) => account?.account_label || account?.email || account?.account_id || account?.project_id || '-').join(', ')}</div>
      )}
      <div className="mt-1 flex items-center gap-2">
        <FixedButton onClick={onClearApiCooldown} variant="neutral" radius="lg" label="Clear API Cooldown">
          <RotateCcw className="w-4 h-4" />
        </FixedButton>
        <FixedButton onClick={onClearHistory} variant="neutral" radius="lg" label="Clear History">
          <Trash2 className="w-4 h-4" />
        </FixedButton>
        <FixedButton onClick={onExportHistory} variant="neutral" radius="lg" label="Export History">
          <Download className="w-4 h-4" />
        </FixedButton>
        <FixedButton onClick={onOpenHistory} variant="neutral" radius="lg" label="Open History">
          <FolderOpen className="w-4 h-4" />
        </FixedButton>
      </div>
      <div className="mt-2">
        <div className="mb-1 flex items-center justify-between gap-2">
          <div className="text-zinc-500">candidate order</div>
          <Button onClick={() => toggleRuntimeSection('candidates')} size="xs" radius="lg" variant="neutral">
            {runtimeSectionOpen('candidates') ? 'Collapse' : 'Expand'}
          </Button>
        </div>
        {runtimeSectionOpen('candidates') && Array.isArray(item?.candidate_order) && item.candidate_order.length > 0 ? (
          <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-2">
            {item.candidate_order.map((candidate: any, idx: number) => (
              <div key={`${candidate?.kind || 'candidate'}-${candidate?.target || idx}`} className="rounded-lg border border-zinc-800 bg-zinc-900/40 px-3 py-2">
                <div className="text-zinc-200">{idx + 1}. {candidate?.kind || '-'}</div>
                <div className="truncate text-zinc-400">{candidate?.target || '-'}</div>
                <div className="text-zinc-500">status: {candidate?.status || (candidate?.available ? 'ready' : 'skip')}</div>
                <div className="text-zinc-500">health: {candidate?.health_score ?? 100} · failures: {candidate?.failure_count ?? 0}</div>
                <div className="text-zinc-500">cooldown: {candidate?.cooldown_until || '-'}</div>
              </div>
            ))}
          </div>
        ) : (
          <div className="text-zinc-500">-</div>
        )}
      </div>
      <div className="mt-2">
        <div className="mb-1 flex items-center justify-between gap-2">
          <div className="text-zinc-500">recent hits</div>
          <Button onClick={() => toggleRuntimeSection('hits')} size="xs" radius="lg" variant="neutral">
            {runtimeSectionOpen('hits') ? 'Collapse' : 'Expand'}
          </Button>
        </div>
        {runtimeSectionOpen('hits') ? renderRuntimeEventList(hits, `${name}-hit`) : <div className="text-zinc-500">-</div>}
      </div>
      <div className="mt-2">
        <div className="mb-1 flex items-center justify-between gap-2">
          <div className="text-zinc-500">recent errors</div>
          <Button onClick={() => toggleRuntimeSection('errors')} size="xs" radius="lg" variant="neutral">
            {runtimeSectionOpen('errors') ? 'Collapse' : 'Expand'}
          </Button>
        </div>
        {runtimeSectionOpen('errors') ? renderRuntimeEventList(errors, `${name}-error`) : <div className="text-zinc-500">-</div>}
      </div>
      <div className="mt-2">
        <div className="mb-1 flex items-center justify-between gap-2">
          <div className="text-zinc-500">recent changes</div>
          <Button onClick={() => toggleRuntimeSection('changes')} size="xs" radius="lg" variant="neutral">
            {runtimeSectionOpen('changes') ? 'Collapse' : 'Expand'}
          </Button>
        </div>
        {runtimeSectionOpen('changes') ? renderRuntimeEventList(changes, `${name}-change`) : <div className="text-zinc-500">-</div>}
      </div>
    </div>
  );
}

type ProviderRuntimeDrawerProps = {
  filterRuntimeEvents: (items: any[]) => any[];
  item: any;
  name: string;
  onClearHistory: () => void;
  onClose: () => void;
  onExportHistory: () => void;
  renderRuntimeEventList: (items: any[], prefix: string) => React.ReactNode;
};

export function ProviderRuntimeDrawer({
  filterRuntimeEvents,
  item,
  name,
  onClearHistory,
  onClose,
  onExportHistory,
  renderRuntimeEventList,
}: ProviderRuntimeDrawerProps) {
  return (
    <div className="fixed inset-0 z-[110] flex justify-end">
      <button className="absolute inset-0 bg-black/40" onClick={onClose} />
      <div className="relative h-full w-full max-w-2xl border-l border-zinc-800 bg-zinc-950 shadow-2xl">
        <div className="flex items-center justify-between border-b border-zinc-800 px-4 py-3">
          <div>
            <div className="text-sm font-semibold text-zinc-100">Provider Runtime History</div>
            <div className="text-xs text-zinc-500">{name}</div>
          </div>
          <div className="flex items-center gap-2">
            <Button onClick={onExportHistory} size="xs" radius="lg" variant="neutral">Export</Button>
            <Button onClick={onClearHistory} size="xs" radius="lg" variant="neutral">Clear</Button>
            <Button onClick={onClose} size="xs" radius="lg" variant="neutral">Close</Button>
          </div>
        </div>
        <div className="h-[calc(100%-57px)] overflow-y-auto p-4 space-y-4 text-xs text-zinc-300">
          <div className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-3 space-y-1">
            <div>auth: {String(item?.auth || '-')}</div>
            <div>last success: {item?.last_success ? `${item.last_success.when || '-'} ${item.last_success.kind || '-'} ${item.last_success.target || '-'}` : '-'}</div>
            {Array.isArray(item?.oauth_accounts) && item.oauth_accounts.length > 0 && (
              <div>oauth accounts: {item.oauth_accounts.map((account: any) => account?.account_label || account?.email || account?.account_id || '-').join(', ')}</div>
            )}
          </div>
          <div className="space-y-2">
            <div className="text-zinc-500">oauth accounts</div>
            {Array.isArray(item?.oauth_accounts) && item.oauth_accounts.length > 0 ? (
              <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
                {item.oauth_accounts.map((account: any, idx: number) => (
                  <div key={`drawer-account-${account?.credential_file || idx}`} className="rounded-lg border border-zinc-800 bg-zinc-900/30 px-3 py-2">
                    <div className="text-zinc-100">{account?.account_label || account?.email || account?.account_id || '-'}</div>
                    <div className="truncate text-zinc-500">{account?.credential_file || '-'}</div>
                    <div className="text-zinc-500">project: {account?.project_id || '-'}</div>
                    <div className="text-zinc-500">device: {account?.device_id || '-'}</div>
                    <div className="truncate text-zinc-500">resource: {account?.resource_url || '-'}</div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="text-zinc-500">-</div>
            )}
          </div>
          <div className="space-y-2">
            <div className="text-zinc-500">candidate order</div>
            {Array.isArray(item?.candidate_order) && item.candidate_order.length > 0 ? (
              <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
                {item.candidate_order.map((candidate: any, idx: number) => (
                  <div key={`drawer-candidate-${idx}`} className="rounded-lg border border-zinc-800 bg-zinc-900/30 px-3 py-2">
                    <div className="text-zinc-100">{idx + 1}. {candidate?.kind || '-'}</div>
                    <div className="truncate text-zinc-400">{candidate?.target || '-'}</div>
                    <div className="text-zinc-500">status: {candidate?.status || (candidate?.available ? 'ready' : 'skip')}</div>
                    <div className="text-zinc-500">health: {candidate?.health_score ?? 100} · failures: {candidate?.failure_count ?? 0}</div>
                    <div className="text-zinc-500">cooldown: {candidate?.cooldown_until || '-'}</div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="text-zinc-500">-</div>
            )}
          </div>
          <div className="space-y-2">
            <div className="text-zinc-500">recent hits</div>
            {renderRuntimeEventList(filterRuntimeEvents(item?.recent_hits), 'drawer-hit')}
          </div>
          <div className="space-y-2">
            <div className="text-zinc-500">recent errors</div>
            {renderRuntimeEventList(filterRuntimeEvents(item?.recent_errors), 'drawer-error')}
          </div>
          <div className="space-y-2">
            <div className="text-zinc-500">recent changes</div>
            {renderRuntimeEventList(filterRuntimeEvents(item?.recent_changes), 'drawer-change')}
          </div>
        </div>
      </div>
    </div>
  );
}

type ProviderProxyCardProps = {
  name: string;
  oauthAccounts: Array<any>;
  onClearOAuthCooldown: (credentialFile: string) => void;
  onDeleteOAuthAccount: (credentialFile: string) => void;
  onFieldChange: (field: string, value: any) => void;
  onLoadOAuthAccounts: () => void;
  onRefreshOAuthAccount: (credentialFile: string) => void;
  onRemove: () => void;
  onStartOAuthLogin: () => void;
  onTriggerOAuthImport: () => void;
  proxy: any;
  runtimeSummary?: React.ReactNode;
  t: (key: string) => string;
};

export function ProviderProxyCard({
  name,
  oauthAccounts,
  onClearOAuthCooldown,
  onDeleteOAuthAccount,
  onFieldChange,
  onLoadOAuthAccounts,
  onRefreshOAuthAccount,
  onRemove,
  onStartOAuthLogin,
  onTriggerOAuthImport,
  proxy,
  runtimeSummary,
  t,
}: ProviderProxyCardProps) {
  const authMode = String(proxy?.auth || 'bearer');
  const showOAuth = ['oauth', 'hybrid'].includes(authMode);
  const oauthProvider = String(proxy?.oauth?.provider || '');
  const [runtimeOpen, setRuntimeOpen] = React.useState(false);
  const [advancedOpen, setAdvancedOpen] = React.useState(false);
  const oauthAccountCount = Array.isArray(oauthAccounts) ? oauthAccounts.length : 0;

  return (
    <div className="grid grid-cols-1 gap-4 rounded-2xl border border-zinc-800 bg-zinc-900/30 p-4 text-xs">
      <div className="flex items-start justify-between gap-3">
        <div className="space-y-2">
          <div className="font-mono text-sm text-zinc-100">{name}</div>
          <div className="flex flex-wrap gap-2">
            <div className="rounded-full border border-zinc-700 bg-zinc-950/60 px-2.5 py-1 text-[11px] text-zinc-300">
              auth: <span className="font-mono">{authMode}</span>
            </div>
            {showOAuth ? (
              <div className="rounded-full border border-zinc-700 bg-zinc-950/60 px-2.5 py-1 text-[11px] text-zinc-300">
                oauth: <span className="font-mono">{oauthProvider || '-'}</span>
              </div>
            ) : null}
            <div className="rounded-full border border-zinc-700 bg-zinc-950/60 px-2.5 py-1 text-[11px] text-zinc-300">
              accounts: <span className="font-mono">{oauthAccountCount}</span>
            </div>
          </div>
          <div className="text-[11px] text-zinc-500">
            {showOAuth
              ? 'Configure connection first, then choose the OAuth provider and start login.'
              : 'Pick an auth mode first. OAuth and Hybrid will open the login workflow.'}
          </div>
        </div>
        <div className="flex items-center gap-2">
          {runtimeSummary ? (
            <Button onClick={() => setRuntimeOpen((v) => !v)} variant="neutral" size="xs" radius="lg">
              {runtimeOpen ? 'Hide Runtime' : 'Show Runtime'}
            </Button>
          ) : null}
          <FixedButton onClick={onRemove} variant="danger" radius="lg" label={t('delete')}>
            <Trash2 className="w-4 h-4" />
          </FixedButton>
        </div>
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-12 gap-4">
        <div className="xl:col-span-8 space-y-4">
          <div className="rounded-2xl border border-zinc-800 bg-zinc-950/20 p-4 space-y-3">
            <div className="flex items-center gap-3">
              <div className="flex h-7 w-7 items-center justify-center rounded-full bg-amber-500/15 text-[11px] font-semibold text-amber-300">1</div>
              <div className="min-w-0 flex items-center gap-2">
                <div className="text-sm font-medium text-zinc-100">Connection</div>
                <div className="truncate text-[11px] text-zinc-500">Base URL, API key, and model routing.</div>
              </div>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              <PanelField label={t('providersApiBase')} dense className="md:col-span-2">
                <ProxyTextField value={String(proxy?.api_base || '')} onChange={(e) => onFieldChange('api_base', e.target.value)} placeholder={t('configLabels.api_base')} className="w-full" />
              </PanelField>
              <PanelField label={t('providersModels')} help={t('providersModelsHelp')} dense>
                <ProxyTextField
                  value={Array.isArray(proxy?.models) ? proxy.models.join(',') : ''}
                  onChange={(e) => onFieldChange('models', e.target.value.split(',').map((s) => s.trim()).filter(Boolean))}
                  placeholder={`${t('configLabels.models')}${t('configCommaSeparatedHint')}`}
                  className="w-full"
                />
              </PanelField>
              <PanelField label={t('providersApiKey')} dense>
                <ProxyTextField value={String(proxy?.api_key || '')} onChange={(e) => onFieldChange('api_key', e.target.value)} placeholder={t('configLabels.api_key')} className="w-full" />
              </PanelField>
            </div>
          </div>

          {showOAuth && (
            <div className="rounded-2xl border border-zinc-800 bg-zinc-950/20 p-4 space-y-4">
              <div className="flex items-start justify-between gap-3">
                <div className="flex items-center gap-3">
                  <div className="flex h-7 w-7 items-center justify-center rounded-full bg-emerald-500/15 text-[11px] font-semibold text-emerald-300">3</div>
                  <div className="min-w-0 flex items-center gap-2">
                    <div className="text-sm font-medium text-zinc-100">{t('providersOAuthSetup')}</div>
                    <div className="truncate text-[11px] text-zinc-500">Select provider, then login or import.</div>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <FixedButton onClick={onStartOAuthLogin} radius="lg" disabled={!oauthProvider} label={t('providersOAuthLoginButton')}>
                    <LogIn className="w-4 h-4" />
                  </FixedButton>
                  <FixedButton onClick={onTriggerOAuthImport} radius="lg" variant="neutral" label={t('providersImportAuthJson')}>
                    <Upload className="w-4 h-4" />
                  </FixedButton>
                </div>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-12 gap-3">
                <PanelField label={t('providersOAuthProvider')} help={t('providersOAuthProviderHelp')} dense className="xl:col-span-3">
                  <ProxySelectField value={oauthProvider} onChange={(e) => onFieldChange('oauth.provider', e.target.value)} className="w-full">
                    <option value="">{t('providersSelectProvider')}</option>
                    <option value="codex">codex</option>
                    <option value="claude">claude</option>
                    <option value="antigravity">antigravity</option>
                    <option value="gemini">gemini</option>
                    <option value="kimi">kimi</option>
                    <option value="qwen">qwen</option>
                  </ProxySelectField>
                </PanelField>
                <PanelField label={t('providersClientSecret')} help={t('providersClientSecretHelp')} dense className="xl:col-span-4">
                  <ProxyTextField value={String(proxy?.oauth?.client_secret || '')} onChange={(e) => onFieldChange('oauth.client_secret', e.target.value)} placeholder={t('providersClientSecret')} className="w-full" />
                </PanelField>
                <PanelField label={t('providersNetworkProxy')} help={t('providersNetworkProxyHelp')} dense className="xl:col-span-5">
                  <ProxyTextField value={String(proxy?.oauth?.network_proxy || '')} onChange={(e) => onFieldChange('oauth.network_proxy', e.target.value)} placeholder={t('providersNetworkProxyPlaceholder')} className="w-full" />
                </PanelField>
                <PanelField label={t('providersCredentialFiles')} help={t('providersCredentialFilesHelp')} dense className="xl:col-span-12">
                  <ProxyTextField
                    value={Array.isArray(proxy?.oauth?.credential_files) ? proxy.oauth.credential_files.join(',') : ''}
                    onChange={(e) => onFieldChange('oauth.credential_files', e.target.value.split(',').map((s) => s.trim()).filter(Boolean))}
                    placeholder={t('providersCredentialFiles')}
                    className="w-full"
                  />
                </PanelField>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                <PanelField label={t('providersCooldownSec')} help="Quota / rate-limit cooldown" dense>
                  <ProxyTextField value={String(proxy?.oauth?.cooldown_sec || '')} onChange={(e) => onFieldChange('oauth.cooldown_sec', Number(e.target.value || 0))} placeholder={t('providersCooldownSec')} className="w-full" />
                </PanelField>
                <PanelField label={t('providersRefreshScanSec')} help="Background scan interval" dense>
                  <ProxyTextField value={String(proxy?.oauth?.refresh_scan_sec || '')} onChange={(e) => onFieldChange('oauth.refresh_scan_sec', Number(e.target.value || 0))} placeholder={t('providersRefreshScanSec')} className="w-full" />
                </PanelField>
                <PanelField label={t('providersRefreshLeadSec')} help="Refresh before expiry" dense>
                  <ProxyTextField value={String(proxy?.oauth?.refresh_lead_sec || '')} onChange={(e) => onFieldChange('oauth.refresh_lead_sec', Number(e.target.value || 0))} placeholder={t('providersRefreshLeadSec')} className="w-full" />
                </PanelField>
              </div>

              <div className="rounded-xl border border-dashed border-zinc-800 bg-zinc-950/30 px-3 py-2 text-[11px] text-zinc-500">
                {t('providersOAuthGuideBefore')}
                <span className="font-mono text-zinc-300">{t('providersOAuthLoginButton')}</span>
                {t('providersOAuthGuideAfter')}
              </div>
            </div>
          )}
        </div>
        <div className="xl:col-span-4 space-y-4">
          <div className="rounded-2xl border border-zinc-800 bg-zinc-950/20 p-4 space-y-3">
            <div className="flex items-center gap-3">
              <div className="flex h-7 w-7 items-center justify-center rounded-full bg-sky-500/15 text-[11px] font-semibold text-sky-300">2</div>
              <div className="min-w-0 flex items-center gap-2">
                <div className="text-sm font-medium text-zinc-100">Authentication</div>
                <div className="truncate text-[11px] text-zinc-500">Request auth and hybrid priority.</div>
              </div>
            </div>
            <PanelField label={t('providersAuthMode')} help={t('providersAuthModeHelp')} dense>
              <ProxySelectField value={authMode} onChange={(e) => onFieldChange('auth', e.target.value)} className="w-full">
                <option value="bearer">bearer</option>
                <option value="oauth">oauth</option>
                <option value="hybrid">hybrid</option>
                <option value="none">none</option>
              </ProxySelectField>
            </PanelField>
            <div className="rounded-xl border border-dashed border-zinc-800 bg-zinc-950/30 px-3 py-2 text-[11px] text-zinc-500">
              {showOAuth ? 'Model selection stays on this provider. Hybrid only switches credentials inside the same provider.' : (
                <>
                  {t('providersSwitchAuthBefore')}
                  <span className="font-mono text-zinc-300">oauth</span>
                  {t('providersSwitchAuthMiddle')}
                  <span className="font-mono text-zinc-300">hybrid</span>
                  {t('providersSwitchAuthAfter')}
                </>
              )}
            </div>
          </div>

          <div className="rounded-2xl border border-zinc-800 bg-zinc-950/20 p-4 space-y-3">
            <div className="flex items-center justify-between gap-3">
              <div className="flex items-center gap-3">
                <div className="flex h-7 w-7 items-center justify-center rounded-full bg-fuchsia-500/15 text-[11px] font-semibold text-fuchsia-300">4</div>
                <div className="min-w-0 flex items-center gap-2">
                  <div className="text-sm font-medium text-zinc-100">{t('providersOAuthAccounts')}</div>
                  <div className="truncate text-[11px] text-zinc-500">Imported sessions.</div>
                </div>
              </div>
              {showOAuth ? (
                <FixedButton onClick={onLoadOAuthAccounts} variant="neutral" radius="lg" label={t('providersRefreshList')}>
                  <RefreshCw className="w-4 h-4" />
                </FixedButton>
              ) : null}
            </div>
            {!showOAuth ? (
              <div className="text-zinc-500">Enable oauth or hybrid mode to manage OAuth accounts.</div>
            ) : oauthAccounts.length === 0 ? (
              <div className="text-zinc-500">{t('providersNoOAuthAccounts')}</div>
            ) : (
              <div className="space-y-2">
                {oauthAccounts.map((account, idx) => (
                  <div key={`${account?.credential_file || idx}`} className="rounded-xl border border-zinc-800 bg-zinc-900/40 px-3 py-3 space-y-2">
                    <div className="min-w-0">
                      <div className="text-zinc-200 truncate">{account?.email || account?.account_id || account?.credential_file}</div>
                      <div className="text-zinc-500 text-[11px]">label: {account?.account_label || account?.email || account?.account_id || '-'}</div>
                      <div className="text-zinc-500 truncate text-[11px]">{account?.credential_file}</div>
                      <div className="text-zinc-500 text-[11px]">project: {account?.project_id || '-'} · device: {account?.device_id || '-'}</div>
                      <div className="text-zinc-500 truncate text-[11px]">proxy: {account?.network_proxy || '-'}</div>
                      <div className="text-zinc-500 text-[11px]">expire: {account?.expire || '-'} · cooldown: {account?.cooldown_until || '-'}</div>
                    </div>
                    <div className="flex items-center gap-2 flex-wrap">
                      <FixedButton onClick={() => onRefreshOAuthAccount(String(account?.credential_file || ''))} variant="neutral" radius="lg" label="Refresh">
                        <RefreshCw className="w-4 h-4" />
                      </FixedButton>
                      <FixedButton onClick={() => onClearOAuthCooldown(String(account?.credential_file || ''))} variant="neutral" radius="lg" label="Clear Cooldown">
                        <RotateCcw className="w-4 h-4" />
                      </FixedButton>
                      <FixedButton onClick={() => onDeleteOAuthAccount(String(account?.credential_file || ''))} variant="danger" radius="lg" label={t('delete')}>
                        <Trash2 className="w-4 h-4" />
                      </FixedButton>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className="rounded-2xl border border-zinc-800 bg-zinc-950/20 p-4 space-y-3">
            <div className="flex items-center justify-between gap-3">
              <div className="flex items-center gap-3">
                <div className="flex h-7 w-7 items-center justify-center rounded-full bg-zinc-700 text-[11px] font-semibold text-zinc-200">5</div>
                <div className="min-w-0 flex items-center gap-2">
                  <div className="text-sm font-medium text-zinc-100">Advanced</div>
                  <div className="truncate text-[11px] text-zinc-500">Low-frequency runtime settings.</div>
                </div>
              </div>
              <Button onClick={() => setAdvancedOpen((v) => !v)} size="xs" radius="lg" variant="neutral">
                {advancedOpen ? 'Hide' : 'Show'}
              </Button>
            </div>
            <PanelField label={t('providersRuntimePersist')} help={t('providersRuntimePersistHelp')} dense>
              <CheckboxField checked={Boolean(proxy?.runtime_persist)} onChange={(e) => onFieldChange('runtime_persist', e.target.checked)} />
            </PanelField>
            {advancedOpen ? (
              <div className="space-y-3">
                <PanelField label={t('providersRuntimeHistoryFile')} dense>
                  <ProxyTextField value={String(proxy?.runtime_history_file || '')} onChange={(e) => onFieldChange('runtime_history_file', e.target.value)} placeholder={t('providersRuntimeHistoryFile')} className="w-full" />
                </PanelField>
                <PanelField label={t('providersRuntimeHistoryMax')} dense>
                  <ProxyTextField value={String(proxy?.runtime_history_max || '')} onChange={(e) => onFieldChange('runtime_history_max', Number(e.target.value || 0))} placeholder={t('providersRuntimeHistoryMax')} className="w-full" />
                </PanelField>
              </div>
            ) : null}
          </div>
        </div>
      </div>

      {runtimeSummary ? (
        <div className="rounded-xl border border-zinc-800 bg-zinc-950/20">
          <button
            type="button"
            className="flex w-full items-center justify-between px-3 py-2 text-left"
            onClick={() => setRuntimeOpen((v) => !v)}
          >
            <div>
              <div className="text-sm font-medium text-zinc-200">Runtime</div>
              <div className="text-[11px] text-zinc-500">Health, candidate order, recent hits and errors.</div>
            </div>
            <div className="text-[11px] text-zinc-400">{runtimeOpen ? 'Collapse' : 'Expand'}</div>
          </button>
          {runtimeOpen ? <div className="border-t border-zinc-800 p-3">{runtimeSummary}</div> : null}
        </div>
      ) : null}
    </div>
  );
}
