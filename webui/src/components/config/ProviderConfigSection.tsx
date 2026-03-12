import React from 'react';
import { Download, FolderOpen, LogIn, LogOut, Plus, RefreshCw, RotateCcw, ShieldCheck, Trash2, Upload, Wallet, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Button, FixedButton } from '../ui/Button';
import { SwitchField, InlineSwitchField, PanelField, SelectField, TextField, ToolbarSwitchField } from '../ui/FormControls';

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

type TagInputFieldProps = {
  onChange: (values: string[]) => void;
  placeholder?: string;
  values: string[];
};

function TagInputField({ onChange, placeholder, values }: TagInputFieldProps) {
  const [draft, setDraft] = React.useState('');

  React.useEffect(() => {
    setDraft('');
  }, [values]);

  function commit(raw: string) {
    const value = String(raw || '').trim();
    if (!value || values.includes(value)) {
      setDraft('');
      return;
    }
    onChange([...values, value]);
    setDraft('');
  }

  function remove(value: string) {
    onChange(values.filter((item) => item !== value));
  }

  return (
    <div className="space-y-2">
      {values.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {values.map((value) => (
            <div key={value} className="flex items-center gap-1 rounded-full border border-zinc-700 bg-zinc-950/70 px-2 py-1 text-[11px] text-zinc-200">
              <span className="font-mono">{value}</span>
              <button type="button" onClick={() => remove(value)} className="text-zinc-400 transition hover:text-zinc-100" aria-label={`remove ${value}`}>
                <X className="h-3 w-3" />
              </button>
            </div>
          ))}
        </div>
      ) : null}
      <ProxyTextField
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault();
            commit(draft);
            return;
          }
          if (e.key === 'Backspace' && !draft && values.length > 0) {
            e.preventDefault();
            remove(values[values.length - 1]);
          }
        }}
        onBlur={() => {
          if (draft.trim()) commit(draft);
        }}
        placeholder={placeholder}
        className="w-full"
      />
    </div>
  );
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
    <div className="flex flex-col xl:flex-row items-start xl:items-center justify-between gap-3">
      <div className="space-y-1">
        <div className="text-sm font-semibold text-zinc-200">{t('configProxies')}</div>
        <div className="text-[11px] text-zinc-500">{t('providersRuntimeToolbarDesc')}</div>
      </div>
      <div className="flex flex-wrap items-center xl:justify-end gap-2 shrink-0">
        <ToolbarSwitchField
          checked={runtimeAutoRefresh}
          className="shrink-0"
          label={t('providersAutoRefresh')}
          onChange={onRuntimeAutoRefreshChange}
        />
        <SelectField dense value={String(runtimeRefreshSec)} onChange={(e) => onRuntimeRefreshSecChange(Number(e.target.value || 10))} className="min-w-[124px] !w-[124px] bg-zinc-900/70 border-zinc-700">
          <option value="2">2s</option>
          <option value="5">5s</option>
          <option value="10">10s</option>
          <option value="30">30s</option>
        </SelectField>
        <SelectField dense value={runtimeWindow} onChange={(e) => onRuntimeWindowChange(e.target.value as 'all' | '1h' | '24h' | '7d')} className="min-w-[148px] !w-[148px] bg-zinc-900/70 border-zinc-700">
          <option value="1h">{t('providersRuntime1h')}</option>
          <option value="24h">{t('providersRuntime24h')}</option>
          <option value="7d">{t('providersRuntime7d')}</option>
          <option value="all">{t('providersRuntimeAll')}</option>
        </SelectField>
        <TextField dense value={newProxyName} onChange={(e) => onNewProxyNameChange(e.target.value)} placeholder={t('configNewProviderName')} className="min-w-[220px] !w-[220px] xl:!w-[280px] bg-zinc-900/70 border-zinc-700" />
        <Button onClick={onAddProxy} variant="primary" size="xs" radius="lg" gap="2" noShrink>
          <Plus className="w-4 h-4" />
          {t('add')}
        </Button>
        <Button onClick={onRefreshRuntime} size="xs" radius="lg" variant="neutral" gap="2" noShrink>
          <RefreshCw className="w-4 h-4" />
        </Button>
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
  const { t } = useTranslation();
  const hits = filterRuntimeEvents(item?.recent_hits);
  const errors = filterRuntimeEvents(item?.recent_errors);
  const changes = filterRuntimeEvents(item?.recent_changes);

  return (
    <div className="md:col-span-7 rounded-lg border border-zinc-800 bg-zinc-950/40 p-2 text-[11px] text-zinc-400">
      <div>{t('metaRuntimeAuth')}: {String(item?.auth || '-')}</div>
      {item?.api_state ? (
        <div className="mt-2 text-xs text-zinc-500">
          <div>{t('metaApiKeyHealth')}: {item?.api_state?.health_score ?? 100} · {t('metaFailures')}: {item?.api_state?.failure_count ?? 0} · {t('metaCooldown')}: {item?.api_state?.cooldown_until || '-'}</div>
        </div>
      ) : null}
      <div>{t('metaApiKeyToken')}: {item?.api_state?.token_masked || '-'}</div>
      <div>{t('metaLastSuccess')}: {item?.last_success ? `${item.last_success.when || '-'} ${item.last_success.kind || '-'} ${item.last_success.target || '-'}` : '-'}</div>
      {Array.isArray(item.oauth_accounts) && item.oauth_accounts.length > 0 ? (
        <div>{t('metaOAuthAccounts')}: {item.oauth_accounts.length} · {item.oauth_accounts.map((account: any) => account?.account_label || account?.email || account?.account_id || account?.project_id || '-').join(', ')}</div>
      ) : null}
      <div className="mt-1 flex items-center gap-2">
        <FixedButton onClick={onClearApiCooldown} variant="neutral" radius="lg" label={t('providersClearingAPICooldown')}>
          <RotateCcw className="w-4 h-4" />
        </FixedButton>
        <FixedButton onClick={onClearHistory} variant="neutral" radius="lg" label={t('providersClearHistory')}>
          <Trash2 className="w-4 h-4" />
        </FixedButton>
        <FixedButton onClick={onExportHistory} variant="neutral" radius="lg" label={t('providersExportHistory')}>
          <Download className="w-4 h-4" />
        </FixedButton>
        <FixedButton onClick={onOpenHistory} variant="neutral" radius="lg" label={t('providersOpenHistory')}>
          <FolderOpen className="w-4 h-4" />
        </FixedButton>
      </div>
      <div className="mt-2">
        <div className="mb-1 flex items-center justify-between gap-2">
          <div className="text-zinc-500">{t('providersCandidateOrder')}</div>
          <Button onClick={() => toggleRuntimeSection('candidates')} size="xs" radius="lg" variant="neutral">
            {runtimeSectionOpen('candidates') ? t('collapse') : t('expand')}
          </Button>
        </div>
        {runtimeSectionOpen('candidates') && Array.isArray(item?.candidate_order) && item.candidate_order.length > 0 ? (
          <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-2">
            {item.candidate_order.map((candidate: any, idx: number) => (
              <div key={`${candidate?.kind || 'candidate'}-${candidate?.target || idx}`} className="rounded-lg border border-zinc-800 bg-zinc-900/40 px-3 py-2">
                <div className="text-zinc-200">{idx + 1}. {candidate?.kind || '-'}</div>
                <div className="truncate text-zinc-400">{candidate?.target || '-'}</div>
                <div className="mt-2 ml-4 px-3 py-2 border-l-2 border-zinc-800 text-xs text-zinc-400 space-y-1">
                  <div className="text-zinc-500">{t('metaStatus')}: {candidate?.status || (candidate?.available ? 'ready' : 'skip')}</div>
                  <div className="text-zinc-500">{t('metaHealth')}: {candidate?.health_score ?? 100} · {t('metaFailures')}: {candidate?.failure_count ?? 0}</div>
                  <div className="text-zinc-500">{t('metaCooldown')}: {candidate?.cooldown_until || '-'}</div>
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div className="text-zinc-500">-</div>
        )}
      </div>
      <div className="mt-2">
        <div className="mb-1 flex items-center justify-between gap-2">
          <div className="text-zinc-500">{t('providersRecentHits')}</div>
          <Button onClick={() => toggleRuntimeSection('hits')} size="xs" radius="lg" variant="neutral">
            {runtimeSectionOpen('hits') ? t('collapse') : t('expand')}
          </Button>
        </div>
        {runtimeSectionOpen('hits') ? renderRuntimeEventList(hits, `${name}-hit`) : <div className="text-zinc-500">-</div>}
      </div>
      <div className="mt-2">
        <div className="mb-1 flex items-center justify-between gap-2">
          <div className="text-zinc-500">{t('providersRecentErrors')}</div>
          <Button onClick={() => toggleRuntimeSection('errors')} size="xs" radius="lg" variant="neutral">
            {runtimeSectionOpen('errors') ? t('collapse') : t('expand')}
          </Button>
        </div>
        {runtimeSectionOpen('errors') ? renderRuntimeEventList(errors, `${name}-error`) : <div className="text-zinc-500">-</div>}
      </div>
      <div className="mt-2">
        <div className="mb-1 flex items-center justify-between gap-2">
          <div className="text-zinc-500">{t('providersRecentChanges')}</div>
          <Button onClick={() => toggleRuntimeSection('changes')} size="xs" radius="lg" variant="neutral">
            {runtimeSectionOpen('changes') ? t('collapse') : t('expand')}
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
  const { t } = useTranslation();
  return (
    <div className="fixed inset-0 z-[110] flex justify-end">
      <button className="absolute inset-0 bg-black/40" onClick={onClose} />
      <div className="relative h-full w-full max-w-2xl border-l border-zinc-800 bg-zinc-950 shadow-2xl">
        <div className="flex items-center justify-between border-b border-zinc-800 px-4 py-3">
          <div>
            <div className="text-sm font-semibold text-zinc-100">{t('providersRuntimeDrawerTitle')}</div>
            <div className="text-xs text-zinc-500">{name}</div>
          </div>
          <div className="flex items-center gap-2">
            <Button onClick={onExportHistory} size="xs" radius="lg" variant="neutral">{t('export')}</Button>
            <Button onClick={onClearHistory} size="xs" radius="lg" variant="neutral">{t('clear')}</Button>
            <Button onClick={onClose} size="xs" radius="lg" variant="neutral">{t('close')}</Button>
          </div>
        </div>
        <div className="h-[calc(100%-57px)] overflow-y-auto p-4 space-y-4 text-xs text-zinc-300">
          <div className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-3 space-y-1">
            <div>{t('metaAuth')}: {String(item?.auth || '-')}</div>
            <div>{t('metaLastSuccess')}: {item?.last_success ? `${item.last_success.when || '-'} ${item.last_success.kind || '-'} ${item.last_success.target || '-'}` : '-'}</div>
            {Array.isArray(item.oauth_accounts) && item.oauth_accounts.length > 0 ? (
            <div className="mt-1 text-xs text-zinc-500">
              <div>{t('metaOAuthAccounts')}: {item.oauth_accounts.map((account: any) => account?.account_label || account?.email || account?.account_id || '-').join(', ')}</div>
            </div>
          ) : null}</div>
          <div className="space-y-2">
            <div className="text-zinc-500">{t('providersOAuthAccounts')}</div>
            {Array.isArray(item?.oauth_accounts) && item.oauth_accounts.length > 0 ? (
              <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
                {item.oauth_accounts.map((account: any, idx: number) => (
                  <div key={`drawer-account-${account?.credential_file || idx}`} className="rounded-lg border border-zinc-800 bg-zinc-900/30 px-3 py-2">
                    <div className="text-zinc-100">{account?.account_label || account?.email || account?.account_id || '-'}</div>
                    <div className="truncate text-zinc-500">{account?.credential_file || '-'}</div>
                    <div className="mt-2 ml-4 px-3 py-2 border-l-2 border-zinc-800 text-xs text-zinc-400 space-y-1">
                    <div className="text-zinc-500">{t('metaProject')}: {account?.project_id || '-'}</div>
                    <div className="text-zinc-500">{t('metaDevice')}: {account?.device_id || '-'}</div>
                  </div>
                    <div className="truncate text-zinc-500">{t('metaResource')}: {account?.resource_url || '-'}</div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="text-zinc-500">-</div>
            )}
          </div>
          <div className="space-y-2">
            <div className="text-zinc-500">{t('providersCandidateOrder')}</div>
            {Array.isArray(item?.candidate_order) && item.candidate_order.length > 0 ? (
              <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
                {item.candidate_order.map((candidate: any, idx: number) => (
                  <div key={`drawer-candidate-${idx}`} className="rounded-lg border border-zinc-800 bg-zinc-900/30 px-3 py-2">
                    <div className="text-zinc-100">{idx + 1}. {candidate?.kind || '-'}</div>
                    <div className="truncate text-zinc-400">{candidate?.target || '-'}</div>
                    <div className="mt-2 ml-4 px-3 py-2 border-l-2 border-zinc-800 text-xs text-zinc-400 space-y-1">
                    <div className="text-zinc-500">{t('metaStatus')}: {candidate?.status || (candidate?.available ? 'ready' : 'skip')}</div>
                    <div className="text-zinc-500">{t('metaHealth')}: {candidate?.health_score ?? 100} · {t('metaFailures')}: {candidate?.failure_count ?? 0}</div>
                    <div className="text-zinc-500">{t('metaCooldown')}: {candidate?.cooldown_until || '-'}</div>
                  </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="text-zinc-500">-</div>
            )}
          </div>
          <div className="space-y-2">
            <div className="text-zinc-500">{t('providersRecentHits')}</div>
            {renderRuntimeEventList(filterRuntimeEvents(item?.recent_hits), 'drawer-hit')}
          </div>
          <div className="space-y-2">
            <div className="text-zinc-500">{t('providersRecentErrors')}</div>
            {renderRuntimeEventList(filterRuntimeEvents(item?.recent_errors), 'drawer-error')}
          </div>
          <div className="space-y-2">
            <div className="text-zinc-500">{t('providersRecentChanges')}</div>
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
  oauthAccountsLoading?: boolean;
  onClearOAuthCooldown: (credentialFile: string) => void;
  onDeleteOAuthAccount: (credentialFile: string) => void;
  onFieldChange: (field: string, value: any) => void;
  onLoadOAuthAccounts: () => void;
  onRefreshOAuthAccount: (credentialFile: string) => void;
  onRemove: () => void;
  onStartOAuthLogin: () => void;
  onTriggerOAuthImport: () => void;
  proxy: any;
  runtimeItem?: any;
  runtimeSummary?: React.ReactNode;
  t: (key: string) => string;
};

export function ProviderProxyCard({
  name,
  oauthAccounts,
  oauthAccountsLoading,
  onClearOAuthCooldown,
  onDeleteOAuthAccount,
  onFieldChange,
  onLoadOAuthAccounts,
  onRefreshOAuthAccount,
  onRemove,
  onStartOAuthLogin,
  onTriggerOAuthImport,
  proxy,
  runtimeItem,
  runtimeSummary,
  t,
}: ProviderProxyCardProps) {
  const { t: ti } = useTranslation();
  const authMode = String(proxy?.auth || 'oauth');
  const providerModels = Array.isArray(proxy?.models)
    ? proxy.models.map((value: any) => String(value || '').trim()).filter(Boolean)
    : [];
  const showOAuth = ['oauth', 'hybrid'].includes(authMode);
  const oauthProvider = String(proxy?.oauth?.provider || '');
  const [runtimeOpen, setRuntimeOpen] = React.useState(false);
  const [advancedOpen, setAdvancedOpen] = React.useState(false);

  React.useEffect(() => {
    if (showOAuth) {
      onLoadOAuthAccounts();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [showOAuth]);

  const oauthAccountCount = Array.isArray(oauthAccounts) ? oauthAccounts.length : 0;
  const runtimeErrors = Array.isArray(runtimeItem?.recent_errors) ? runtimeItem.recent_errors : [];
  const lastQuotaError = runtimeErrors.find((item: any) => String(item?.reason || '').trim() === 'quota') || null;
  const connected = showOAuth && oauthAccountCount > 0;
  const primaryAccount = oauthAccounts[0] || null;
  const quotaState = !showOAuth
    ? null
    : lastQuotaError
      ? {
        label: t('providersQuotaLimited'),
        tone: 'ui-pill ui-pill-warning',
        detail: ti('providersQuotaLimitedDetail', { when: String(lastQuotaError?.when || '-') }),
      }
      : oauthAccounts.some((account) => String(account?.cooldown_until || '').trim())
        ? {
          label: t('providersQuotaCooldown'),
          tone: 'ui-pill ui-pill-warning',
          detail: oauthAccounts
            .map((account) => String(account?.cooldown_until || '').trim())
            .find(Boolean) || '-',
        }
        : oauthAccounts.some((account) => Number(account?.health_score || 100) < 60)
          ? {
            label: t('providersQuotaHealthLow'),
            tone: 'ui-pill ui-pill-danger',
            detail: ti('providersQuotaHealthLowDetail', { score: Math.min(...oauthAccounts.map((account) => Number(account?.health_score || 100))) }),
          }
          : connected
            ? {
              label: t('providersQuotaHealthy'),
              tone: 'ui-pill ui-pill-success',
              detail: t('providersQuotaHealthyDetail'),
            }
            : {
              label: t('providersOAuthDisconnected'),
              tone: 'ui-pill ui-pill-neutral',
              detail: t('providersQuotaNoAccountDetail'),
            };
  const quotaTone = quotaState?.tone || 'ui-pill ui-pill-neutral';
  const oauthStatusText = oauthAccountsLoading
    ? t('providersOAuthLoading')
    : connected
      ? `${t('providersOAuthAutoLoaded')} ${oauthAccountCount} ${t('providersOAuthAccountUnit')}`
      : t('providersNoOAuthAccounts');
  const oauthStatusDetail = oauthAccountsLoading
    ? t('providersOAuthLoadingHelp')
    : connected
      ? `${t('providersOAuthPrimaryAccount')} ${primaryAccount?.account_label || primaryAccount?.email || primaryAccount?.account_id || '-'}`
      : t('providersOAuthEmptyHelp');
  const primaryBalanceText = primaryAccount?.balance_label || primaryAccount?.plan_type || '';
  const primaryBalanceDetail = primaryAccount?.balance_detail || primaryAccount?.subscription_active_until || '';

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
              ? t('providersOAuthConfigHint')
              : t('providersAuthPickHint')}
          </div>
        </div>
        <div className="flex items-center gap-2">
          {runtimeSummary ? (
            <Button onClick={() => setRuntimeOpen((v) => !v)} variant="neutral" size="xs" radius="lg">
              {runtimeOpen ? t('hide') : t('show')}
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
                <div className="text-sm font-medium text-zinc-100">{t('providersSectionConnection')}</div>
                <div className="truncate text-[11px] text-zinc-500">{t('providersSectionConnectionDesc')}</div>
              </div>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              <PanelField label={t('providersApiBase')} dense className="md:col-span-2">
                <ProxyTextField value={String(proxy?.api_base || '')} onChange={(e) => onFieldChange('api_base', e.target.value)} placeholder={t('configLabels.api_base')} className="w-full" />
              </PanelField>
              <PanelField label={t('providersModels')} help={t('providersModelsHelp')} dense>
                <TagInputField values={providerModels} onChange={(values) => onFieldChange('models', values)} placeholder={t('providersModelsEnterHint')} />
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
                    <div className="truncate text-[11px] text-zinc-500">{t('providersSectionOAuthSetupDesc')}</div>
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
                <PanelField label={t('providersCooldownSec')} help={t('providersCooldownSecHelp')} dense>
                  <ProxyTextField value={String(proxy?.oauth?.cooldown_sec || '')} onChange={(e) => onFieldChange('oauth.cooldown_sec', Number(e.target.value || 0))} placeholder={t('providersCooldownSec')} className="w-full" />
                </PanelField>
                <PanelField label={t('providersRefreshScanSec')} help={t('providersRefreshScanSecHelp')} dense>
                  <ProxyTextField value={String(proxy?.oauth?.refresh_scan_sec || '')} onChange={(e) => onFieldChange('oauth.refresh_scan_sec', Number(e.target.value || 0))} placeholder={t('providersRefreshScanSec')} className="w-full" />
                </PanelField>
                <PanelField label={t('providersRefreshLeadSec')} help={t('providersRefreshLeadSecHelp')} dense>
                  <ProxyTextField value={String(proxy?.oauth?.refresh_lead_sec || '')} onChange={(e) => onFieldChange('oauth.refresh_lead_sec', Number(e.target.value || 0))} placeholder={t('providersRefreshLeadSec')} className="w-full" />
                </PanelField>
              </div>

              <div className="rounded-xl border border-dashed border-zinc-800 bg-zinc-950/30 px-3 py-2 text-[11px] text-zinc-500">
                {t('providersOAuthGuideBefore')}
                <span className="font-mono text-zinc-300">{t('providersOAuthLoginButton')}</span>
                {t('providersOAuthGuideAfter')}
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                <div className="rounded-2xl border border-zinc-800 bg-zinc-950/25 p-3">
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <div className="text-[11px] uppercase tracking-[0.18em] text-zinc-500">{t('providersOAuthLoginStatus')}</div>
                      <div className="mt-2 flex items-center gap-2 text-sm font-medium text-zinc-100">
                        <ShieldCheck className={`h-4 w-4 ${connected ? 'text-emerald-300' : 'text-zinc-500'}`} />
                        {oauthAccountsLoading ? t('providersOAuthLoading') : connected ? oauthStatusText : t('providersOAuthDisconnected')}
                      </div>
                      <div className="mt-2 text-[11px] text-zinc-500">{oauthStatusDetail}</div>
                    </div>
                    <div className={`rounded-full border px-2.5 py-1 text-[11px] ${connected ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-200' : 'border-zinc-700 bg-zinc-900/50 text-zinc-400'}`}>
                      {oauthAccountsLoading ? t('providersLoading') : connected ? t('providersConnected') : t('providersDisconnected')}
                    </div>
                  </div>
                </div>
                <div className="rounded-2xl border border-zinc-800 bg-zinc-950/25 p-3">
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <div className="text-[11px] uppercase tracking-[0.18em] text-zinc-500">{t('providersQuotaStatus')}</div>
                      <div className="mt-2 flex items-center gap-2 text-sm font-medium text-zinc-100">
                        <Wallet className="h-4 w-4 text-amber-300" />
                        {primaryBalanceText || quotaState?.label || t('providersQuotaPending')}
                      </div>
                      <div className="mt-2 text-[11px] text-zinc-500">{primaryBalanceDetail || quotaState?.detail || t('providersQuotaHelp')}</div>
                    </div>
                    <div className={`rounded-full border px-2.5 py-1 text-[11px] ${quotaTone}`}>
                      {lastQuotaError ? t('providersQuotaBadge') : connected ? t('providersRuntimeBadge') : t('providersPending')}
                    </div>
                  </div>
                </div>
              </div>

              <div className="hidden grid grid-cols-1 md:grid-cols-2 gap-3">
                <div className="rounded-2xl border border-zinc-800 bg-zinc-950/25 p-3">
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <div className="text-[11px] uppercase tracking-[0.18em] text-zinc-500">{t('providersOAuthLoginStatus')}</div>
                      <div className="mt-2 flex items-center gap-2 text-sm font-medium text-zinc-100">
                        <ShieldCheck className={`h-4 w-4 ${connected ? 'text-emerald-300' : 'text-zinc-500'}`} />
                        {connected ? ti('providersLoggedInCount', { count: oauthAccountCount }) : t('providersNotLoggedIn')}
                      </div>
                      <div className="mt-2 text-[11px] text-zinc-500">
                        {connected
                          ? (oauthAccounts[0]?.account_label || oauthAccounts[0]?.email || oauthAccounts[0]?.account_id || t('providersPrimaryLoaded'))
                          : t('providersLoginOAuthOrImportHint')}
                      </div>
                    </div>
                    <div className={`rounded-full border px-2.5 py-1 text-[11px] ${connected ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-200' : 'border-zinc-700 bg-zinc-900/50 text-zinc-400'}`}>
                      {connected ? t('providersConnected') : t('providersDisconnected')}
                    </div>
                  </div>
                </div>
                <div className="rounded-2xl border border-zinc-800 bg-zinc-950/25 p-3">
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <div className="text-[11px] uppercase tracking-[0.18em] text-zinc-500">{t('providersQuotaStatus')}</div>
                      <div className="mt-2 flex items-center gap-2 text-sm font-medium text-zinc-100">
                        <Wallet className="h-4 w-4 text-amber-300" />
                        {quotaState?.label || '-'}
                      </div>
                      <div className="mt-2 text-[11px] text-zinc-500">{quotaState?.detail || t('providersQuotaDefaultHelp')}</div>
                    </div>
                    <div className={`rounded-full border px-2.5 py-1 text-[11px] ${quotaState?.tone || 'border-zinc-700 bg-zinc-900/50 text-zinc-300'}`}>
                      {lastQuotaError ? 'Quota' : connected ? 'Runtime' : 'Pending'}
                    </div>
                  </div>
                </div>
              </div>
            </div>
          )}
        </div>
        <div className="xl:col-span-4 space-y-4">
          <div className="rounded-2xl border border-zinc-800 bg-zinc-950/20 p-4 space-y-3">
            <div className="flex items-center gap-3">
              <div className="flex h-7 w-7 items-center justify-center rounded-full bg-sky-500/15 text-[11px] font-semibold text-sky-300">2</div>
              <div className="min-w-0 flex items-center gap-2">
                <div className="text-sm font-medium text-zinc-100">{t('providersSectionAuth')}</div>
                <div className="truncate text-[11px] text-zinc-500">{t('providersSectionAuthDesc')}</div>
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
              {showOAuth ? t('providersOAuthHybridHint') : (
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

          {showOAuth ? (
            <div className="rounded-2xl border border-zinc-800 bg-zinc-950/20 p-4 space-y-3">
              <div className="flex items-center justify-between gap-3">
                <div className="flex items-center gap-3">
                  <div className="flex h-7 w-7 items-center justify-center rounded-full bg-fuchsia-500/15 text-[11px] font-semibold text-fuchsia-300">4</div>
                  <div className="min-w-0 flex items-center gap-2">
                    <div className="text-sm font-medium text-zinc-100">{t('providersOAuthAccounts')}</div>
                    <div className="truncate text-[11px] text-zinc-500">{t('providersSectionOAuthAccountsDesc')}</div>
                  </div>
                </div>
                <FixedButton onClick={onLoadOAuthAccounts} variant="neutral" radius="lg" label={t('providersRefreshList')}>
                  <RefreshCw className={`w-4 h-4${oauthAccountsLoading ? ' animate-spin' : ''}`} />
                </FixedButton>
              </div>
              <div className={`mt-4 rounded-xl border px-4 py-3 text-sm ${connected
                ? 'ui-pill ui-pill-success'
                : 'ui-pill ui-pill-neutral'
                }`}>
                {oauthAccountsLoading
                  ? t('providersOAuthLoadingHelp')
                  : connected
                    ? `${oauthStatusText}。${oauthStatusDetail}`
                    : t('providersOAuthEmptyHelp')}
              </div>
              <div className={`hidden rounded-xl border px-3 py-2 text-[11px] ${connected
                ? 'ui-pill ui-pill-success'
                : 'ui-pill ui-pill-neutral'
                }`}>
                {connected
                  ? ti('providersAutoLoadedCount', { count: oauthAccountCount, primary: oauthAccounts[0]?.account_label || oauthAccounts[0]?.email || oauthAccounts[0]?.account_id || '-' })
                  : t('providersNoAccountsAvailableHint')}
              </div>
              {oauthAccountsLoading ? (
                <div className="text-zinc-500">{t('providersOAuthLoading')}</div>
              ) : oauthAccounts.length === 0 ? (
                <div className="text-zinc-500">{t('providersNoOAuthAccounts')}</div>
              ) : (
                <div className="space-y-2">
                  {oauthAccounts.map((account, idx) => (
                    <div key={`${account?.credential_file || idx}`} className="rounded-xl border border-zinc-800 bg-zinc-900/40 px-3 py-3 space-y-2">
                      <div className="min-w-0">
                        <div className="flex items-center justify-between gap-2">
                          <div className="text-zinc-200 truncate">{account?.email || account?.account_id || account?.credential_file}</div>
                          <div className={`shrink-0 rounded-full border px-2 py-0.5 text-[10px] ${String(account?.cooldown_until || '').trim()
                            ? 'ui-pill ui-pill-warning'
                            : Number(account?.health_score || 100) < 60
                              ? 'ui-pill ui-pill-danger'
                              : 'ui-pill ui-pill-success'
                            }`}>
                            {String(account?.cooldown_until || '').trim() ? t('providersAccountCooldown') : Number(account?.health_score || 100) < 60 ? t('providersAccountLimited') : t('providersAccountOnline')}
                          </div>
                        </div>
                        <div className="text-zinc-500 text-[11px]">{t('metaLabel')}: {account?.account_label || account?.email || account?.account_id || '-'}</div>
                        <div className="text-zinc-500 truncate text-[11px]">{account?.credential_file}</div>
                        {(account?.balance_label || account?.plan_type) ? (
                          <div className="text-zinc-500 text-[11px]">
                            {t('providersBalanceDisplay')}: {account?.balance_label || account?.plan_type}
                            {account?.balance_detail ? ` · ${account.balance_detail}` : ''}
                          </div>
                        ) : null}
                        {account?.subscription_active_until ? (
                          <div className="text-zinc-500 text-[11px]">
                            {t('providersSubscriptionUntil')}: {account?.subscription_active_until}
                          </div>
                        ) : null}
                        <div className="flex items-start justify-between gap-4 mt-1.5">
                        <div className="text-zinc-500 text-[11px]">{t('metaProject')}: {account?.project_id || '-'} · {t('metaDevice')}: {account?.device_id || '-'}</div>
                      </div>
                        <div className="text-zinc-500 truncate text-[11px]">{t('metaProxy')}: {account?.network_proxy || '-'}</div>
                        <div className="flex items-start justify-between gap-4 mt-1.5">
                        <div className="text-zinc-500 text-[11px]">{t('metaExpire')}: {account?.expire || '-'} · {t('metaCooldown')}: {account?.cooldown_until || '-'}</div>
                      </div>
                        <div className="text-zinc-500 text-[11px]">
                          {t('providersQuotaStatus')}: {String(account?.cooldown_until || '').trim()
                            ? `${t('providersQuotaCooldown')} · ${account?.cooldown_until || '-'}`
                            : Number(account?.health_score || 100) < 60
                              ? `${t('providersQuotaHealthLow')} · ${t('providersQuotaLowestHealth')} ${Number(account?.health_score || 100)}`
                              : lastQuotaError
                                ? `${t('providersQuotaLimited')} · ${String(lastQuotaError?.when || '-')}`
                                : t('providersQuotaHealthy')}
                        </div>
                        <div className="flex items-start justify-between gap-4 mt-1.5">
                        <div className="text-zinc-500 text-[11px]">{t('metaHealth')}: {Number(account?.health_score || 100)} · {t('metaFailures')}: {Number(account?.failure_count || 0)} · {t('metaLastFailure')}: {account?.last_failure || '-'}</div>
                      </div>
                      </div>
                      <div className="flex items-center gap-2 flex-wrap">
                        <FixedButton onClick={() => onRefreshOAuthAccount(String(account?.credential_file || ''))} variant="neutral" radius="lg" label={t('refresh')}>
                          <RefreshCw className="w-4 h-4" />
                        </FixedButton>
                        <FixedButton onClick={() => onClearOAuthCooldown(String(account?.credential_file || ''))} variant="neutral" radius="lg" label={t('providersClearCooldown')}>
                          <RotateCcw className="w-4 h-4" />
                        </FixedButton>
                        <FixedButton onClick={() => onDeleteOAuthAccount(String(account?.credential_file || ''))} variant="danger" radius="lg" label={t('providersLogout')}>
                          <LogOut className="w-4 h-4" />
                        </FixedButton>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          ) : null}

          <div className="rounded-2xl border border-zinc-800 bg-zinc-950/20 p-4 space-y-3">
            <div className="flex items-center justify-between gap-3">
              <div className="flex items-center gap-3">
                <div className="flex h-7 w-7 items-center justify-center rounded-full bg-zinc-700 text-[11px] font-semibold text-zinc-200">5</div>
                <div className="min-w-0 flex items-center gap-2">
                  <div className="text-sm font-medium text-zinc-100">{t('providersSectionAdvanced')}</div>
                  <div className="truncate text-[11px] text-zinc-500">{t('providersSectionAdvancedDesc')}</div>
                </div>
              </div>
              <Button onClick={() => setAdvancedOpen((v) => !v)} size="xs" radius="lg" variant="neutral">
                {advancedOpen ? t('hide') : t('show')}
              </Button>
            </div>
            <InlineSwitchField
              checked={Boolean(proxy?.runtime_persist)}
              help={t('providersRuntimePersistHelp')}
              label={t('providersRuntimePersist')}
              onChange={(checked) => onFieldChange('runtime_persist', checked)}
            />
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
              <div className="text-sm font-medium text-zinc-200">{t('providersRuntimeTitle')}</div>
              <div className="text-[11px] text-zinc-500">{t('providersRuntimeDesc')}</div>
            </div>
            <div className="text-[11px] text-zinc-400">{runtimeOpen ? t('collapse') : t('expand')}</div>
          </button>
          {runtimeOpen ? <div className="border-t border-zinc-800 p-3">{runtimeSummary}</div> : null}
        </div>
      ) : null}
    </div>
  );
}
