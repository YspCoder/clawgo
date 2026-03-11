import React, { useRef } from 'react';
import { buildProviderRuntimeExportPayload, createDefaultProxyConfig, setPath } from './configUtils';
import { cloneJSON } from '../../utils/object';

type UI = {
  confirmDialog: (options: any) => Promise<boolean>;
  notify: (options: any) => Promise<void>;
  promptDialog: (options: any) => Promise<string | null>;
  withLoading: <T>(fn: () => Promise<T>, label: string) => Promise<T>;
};

type UseConfigProviderActionsArgs = {
  inputRef: React.RefObject<HTMLInputElement | null>;
  loadConfig: (force?: boolean) => Promise<any>;
  onProviderRuntimeRefreshed: () => void;
  providerRuntimeMap: Record<string, any>;
  q: string;
  setCfg: React.Dispatch<React.SetStateAction<any>>;
  setNewProxyName: React.Dispatch<React.SetStateAction<string>>;
  setOAuthAccounts: React.Dispatch<React.SetStateAction<Record<string, Array<any>>>>;
  t: (key: string, options?: any) => string;
  ui: UI;
};

export function useConfigProviderActions({
  inputRef,
  loadConfig,
  onProviderRuntimeRefreshed,
  providerRuntimeMap,
  q,
  setCfg,
  setNewProxyName,
  setOAuthAccounts,
  t,
  ui,
}: UseConfigProviderActionsArgs) {
  const oauthImportProviderRef = useRef<string>('');
  const oauthImportConfigRef = useRef<any>(null);

  async function parseResponseBody(res: Response) {
    const text = await res.text();
    if (!text) return { text: '', data: {} as any };
    try {
      return { text, data: JSON.parse(text) };
    } catch {
      return { text, data: null as any };
    }
  }

  function providerConfigPath(name: string) {
    return `models.providers.${name}`;
  }

  async function removeProxy(name: string) {
    const ok = await ui.confirmDialog({
      title: t('configDeleteProviderConfirmTitle'),
      message: t('configDeleteProviderConfirmMessage', { name }),
      danger: true,
      confirmText: t('delete'),
    });
    if (!ok) return;
    setCfg((value) => {
      const next = cloneJSON(value || {});
      if (next?.models?.providers && typeof next.models.providers === 'object') {
        delete next.models.providers[name];
      }
      return next;
    });
  }

  function addProxy(name: string) {
    const trimmed = name.trim();
    if (!trimmed) return;
    setCfg((value) => {
      const next = cloneJSON(value || {});
      if (!next.models || typeof next.models !== 'object') next.models = {};
      if (!next.models.providers || typeof next.models.providers !== 'object' || Array.isArray(next.models.providers)) {
        next.models.providers = {};
      }
      if (!next.models.providers[trimmed]) {
        next.models.providers[trimmed] = createDefaultProxyConfig();
      }
      return next;
    });
    setNewProxyName('');
  }

  async function startOAuthLogin(name: string, proxy?: any) {
    try {
      const oauthProvider = String(proxy?.oauth?.provider || '').trim().toLowerCase();
      const networkProxy = String(proxy?.oauth?.network_proxy || '').trim();
      let accountLabel = '';
      if (oauthProvider === 'qwen') {
        const value = await ui.promptDialog({
          title: t('providersQwenLabelTitle'),
          message: t('providersQwenLabelMessage'),
          inputPlaceholder: t('providersQwenLabelPlaceholder'),
        });
        if (value == null) return;
        accountLabel = String(value || '').trim();
        if (!accountLabel) {
          await ui.notify({ title: t('requestFailed'), message: t('providersQwenLabelRequired') });
          return;
        }
      }
      const started = await ui.withLoading(async () => {
        const res = await fetch(`/webui/api/provider/oauth/start${q}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ provider: name, account_label: accountLabel, network_proxy: networkProxy, provider_config: proxy || {} }),
        });
        const { text, data } = await parseResponseBody(res);
        if (!res.ok) throw new Error(data?.error || text || 'oauth start failed');
        return data;
      }, t('providersStartingOAuthLogin'));
      let callbackURL = '';
      if (String(started?.mode || '') === 'device') {
        const ok = await ui.confirmDialog({
          title: t('providersOAuthDeviceTitle'),
          message: `${started?.instructions || t('providersOAuthDeviceMessage')}\n\n${started?.auth_url || ''}${started?.user_code ? `\n\n${t('providersOAuthUserCode')}: ${started.user_code}` : ''}`,
          confirmText: t('continue'),
        });
        if (!ok) return;
      } else {
        const pasted = await ui.promptDialog({
          title: t('providersOAuthLoginTitle'),
          message: `${started?.instructions || t('providersOAuthLoginMessage')}\n\n${started.auth_url}`,
          inputPlaceholder: t('providersOAuthCallbackPlaceholder'),
        });
        if (pasted == null) return;
        callbackURL = pasted;
      }
      await ui.withLoading(async () => {
        const res = await fetch(`/webui/api/provider/oauth/complete${q}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ provider: name, flow_id: started.flow_id, callback_url: callbackURL, account_label: accountLabel, network_proxy: networkProxy, provider_config: proxy || {} }),
        });
        const { text, data } = await parseResponseBody(res);
        if (!res.ok) throw new Error(data?.error || text || 'oauth complete failed');
        await loadConfig(true);
        await ui.notify({ title: t('providersOAuthAddedTitle'), message: data?.account ? t('providersOAuthAddedMessage', { account: data.account }) : t('providersOAuthAddedFallback') });
      }, t('providersCompletingOAuthLogin'));
    } catch (err: any) {
      await ui.notify({ title: t('requestFailed'), message: String(err?.message || err) });
    }
  }

  async function triggerOAuthImport(name: string, proxy?: any) {
    const oauthProvider = String(proxy?.oauth?.provider || '').trim().toLowerCase();
    if (oauthProvider === 'qwen') {
      const value = await ui.promptDialog({
        title: t('providersQwenLabelTitle'),
        message: t('providersQwenImportLabelMessage'),
        inputPlaceholder: t('providersQwenLabelPlaceholder'),
      });
      if (value == null) return;
      const accountLabel = String(value || '').trim();
      if (!accountLabel) {
        await ui.notify({ title: t('requestFailed'), message: t('providersQwenLabelRequired') });
        return;
      }
      oauthImportProviderRef.current = `${name}::${accountLabel}`;
      oauthImportConfigRef.current = proxy || {};
      inputRef.current?.click();
      return;
    }
    oauthImportProviderRef.current = name;
    oauthImportConfigRef.current = proxy || {};
    inputRef.current?.click();
  }

  async function onOAuthImportChange(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    const providerValue = oauthImportProviderRef.current;
    const providerConfig = oauthImportConfigRef.current;
    e.target.value = '';
    if (!file || !providerValue) return;
    const [providerName, accountLabel = ''] = providerValue.split('::');
    try {
      await ui.withLoading(async () => {
        const form = new FormData();
        form.append('provider', providerName);
        if (accountLabel) form.append('account_label', accountLabel);
        if (String(providerConfig?.oauth?.network_proxy || '').trim()) {
          form.append('network_proxy', String(providerConfig?.oauth?.network_proxy || '').trim());
        }
        form.append('provider_config', JSON.stringify(providerConfig || {}));
        form.append('file', file);
        const res = await fetch(`/webui/api/provider/oauth/import${q}`, { method: 'POST', body: form });
        const { text, data } = await parseResponseBody(res);
        if (!res.ok) throw new Error(data?.error || text || 'oauth import failed');
        await loadConfig(true);
        await ui.notify({ title: t('providersAuthJsonImportedTitle'), message: data?.account ? t('providersOAuthAddedMessage', { account: data.account }) : t('providersAuthJsonImportedMessage') });
      }, t('providersImportingAuthJson'));
    } catch (err: any) {
      await ui.notify({ title: t('requestFailed'), message: String(err?.message || err) });
    }
  }

  async function loadOAuthAccounts(name: string) {
    try {
      const suffix = q ? `${q}&provider=${encodeURIComponent(name)}` : `?provider=${encodeURIComponent(name)}`;
      const res = await fetch(`/webui/api/provider/oauth/accounts${suffix}`);
      const { text, data } = await parseResponseBody(res);
      if (!res.ok) throw new Error(data?.error || text || 'oauth accounts load failed');
      setOAuthAccounts((prev) => ({ ...prev, [name]: Array.isArray(data?.accounts) ? data.accounts : [] }));
    } catch {
      setOAuthAccounts((prev) => ({ ...prev, [name]: [] }));
    }
  }

  async function refreshOAuthAccount(name: string, credentialFile: string) {
    try {
      await ui.withLoading(async () => {
        const res = await fetch(`/webui/api/provider/oauth/accounts${q}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ provider: name, action: 'refresh', credential_file: credentialFile }),
        });
        const { text, data } = await parseResponseBody(res);
        if (!res.ok) throw new Error(data?.error || text || 'oauth refresh failed');
      }, t('providersRefreshingOAuthAccount'));
      await loadOAuthAccounts(name);
    } catch (err: any) {
      await ui.notify({ title: t('requestFailed'), message: String(err?.message || err) });
    }
  }

  async function deleteOAuthAccount(name: string, credentialFile: string) {
    const ok = await ui.confirmDialog({ title: t('providersDeleteOAuthAccountTitle'), message: credentialFile, danger: true, confirmText: t('delete') });
    if (!ok) return;
    try {
      await ui.withLoading(async () => {
        const res = await fetch(`/webui/api/provider/oauth/accounts${q}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ provider: name, action: 'delete', credential_file: credentialFile }),
        });
        const { text, data } = await parseResponseBody(res);
        if (!res.ok) throw new Error(data?.error || text || 'oauth delete failed');
      }, t('providersDeletingOAuthAccount'));
      await loadConfig(true);
      await loadOAuthAccounts(name);
    } catch (err: any) {
      await ui.notify({ title: t('requestFailed'), message: String(err?.message || err) });
    }
  }

  async function clearOAuthCooldown(name: string, credentialFile: string) {
    try {
      await ui.withLoading(async () => {
        const res = await fetch(`/webui/api/provider/oauth/accounts${q}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ provider: name, action: 'clear_cooldown', credential_file: credentialFile }),
        });
        const { text, data } = await parseResponseBody(res);
        if (!res.ok) throw new Error(data?.error || text || 'oauth clear cooldown failed');
      }, t('providersClearingOAuthCooldown'));
      await loadOAuthAccounts(name);
    } catch (err: any) {
      await ui.notify({ title: t('requestFailed'), message: String(err?.message || err) });
    }
  }

  async function clearAPIKeyCooldown(name: string) {
    try {
      await ui.withLoading(async () => {
        const res = await fetch(`/webui/api/provider/runtime${q}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ provider: name, action: 'clear_api_cooldown' }),
        });
        const { text, data } = await parseResponseBody(res);
        if (!res.ok) throw new Error(data?.error || text || 'api cooldown clear failed');
      }, t('providersClearingAPICooldown'));
      await loadConfig(true);
    } catch (err: any) {
      await ui.notify({ title: t('requestFailed'), message: String(err?.message || err) });
    }
  }

  async function clearProviderHistory(name: string) {
    try {
      await ui.withLoading(async () => {
        const res = await fetch(`/webui/api/provider/runtime${q}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ provider: name, action: 'clear_history' }),
        });
        const { text, data } = await parseResponseBody(res);
        if (!res.ok) throw new Error(data?.error || text || 'clear history failed');
        await loadConfig(true);
      }, t('providersClearingHistory'));
    } catch (err: any) {
      await ui.notify({ title: t('requestFailed'), message: String(err?.message || err) });
    }
  }

  function exportProviderHistory(name: string) {
    const item = providerRuntimeMap[name];
    if (!item) return;
    const payload = buildProviderRuntimeExportPayload(name, item);
    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' });
    const url = window.URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `${name}-runtime-history.json`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    window.URL.revokeObjectURL(url);
  }

  async function refreshProviderRuntimeNow() {
    await loadConfig(true);
    onProviderRuntimeRefreshed();
  }

  function updateProxyField(name: string, field: string, value: any) {
    setCfg((current) => setPath(current, `${providerConfigPath(name)}.${field}`, value));
  }

  return {
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
  };
}
