import React, { useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Check, LogOut, QrCode, RefreshCw, ShieldCheck, Smartphone, Users, Wifi, WifiOff } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';

type ChannelKey = 'telegram' | 'whatsapp' | 'discord' | 'feishu' | 'qq' | 'dingtalk' | 'maixcam';

type ChannelField =
  | { key: string; type: 'text' | 'password' | 'number'; placeholder?: string }
  | { key: string; type: 'boolean' }
  | { key: string; type: 'list'; placeholder?: string };

type ChannelDefinition = {
  id: ChannelKey;
  titleKey: string;
  hintKey: string;
  fields: ChannelField[];
};

type WhatsAppStatusPayload = {
  ok?: boolean;
  enabled?: boolean;
  bridge_url?: string;
  bridge_running?: boolean;
  error?: string;
  status?: {
    state?: string;
    connected?: boolean;
    logged_in?: boolean;
    bridge_addr?: string;
    user_jid?: string;
    push_name?: string;
    platform?: string;
    qr_available?: boolean;
    qr_code?: string;
    last_event?: string;
    last_error?: string;
    updated_at?: string;
    inbound_count?: number;
    outbound_count?: number;
    read_receipt_count?: number;
    last_inbound_at?: string;
    last_outbound_at?: string;
    last_read_at?: string;
    last_inbound_from?: string;
    last_outbound_to?: string;
    last_inbound_text?: string;
    last_outbound_text?: string;
  };
};

const channelDefinitions: Record<ChannelKey, ChannelDefinition> = {
  telegram: {
    id: 'telegram',
    titleKey: 'telegram',
    hintKey: 'telegramChannelHint',
    fields: [
      { key: 'enabled', type: 'boolean' },
      { key: 'token', type: 'password' },
      { key: 'streaming', type: 'boolean' },
      { key: 'allow_from', type: 'list', placeholder: '123456789' },
      { key: 'allow_chats', type: 'list', placeholder: 'telegram:123456789' },
      { key: 'enable_groups', type: 'boolean' },
      { key: 'require_mention_in_groups', type: 'boolean' },
    ],
  },
  whatsapp: {
    id: 'whatsapp',
    titleKey: 'whatsappBridge',
    hintKey: 'whatsappBridgeHint',
    fields: [
      { key: 'enabled', type: 'boolean' },
      { key: 'bridge_url', type: 'text', placeholder: 'ws://127.0.0.1:3001' },
      { key: 'allow_from', type: 'list', placeholder: '8613012345678@s.whatsapp.net' },
      { key: 'enable_groups', type: 'boolean' },
      { key: 'require_mention_in_groups', type: 'boolean' },
    ],
  },
  discord: {
    id: 'discord',
    titleKey: 'discord',
    hintKey: 'discordChannelHint',
    fields: [
      { key: 'enabled', type: 'boolean' },
      { key: 'token', type: 'password' },
      { key: 'allow_from', type: 'list', placeholder: 'discord-user-id' },
    ],
  },
  feishu: {
    id: 'feishu',
    titleKey: 'feishu',
    hintKey: 'feishuChannelHint',
    fields: [
      { key: 'enabled', type: 'boolean' },
      { key: 'app_id', type: 'text' },
      { key: 'app_secret', type: 'password' },
      { key: 'encrypt_key', type: 'password' },
      { key: 'verification_token', type: 'password' },
      { key: 'allow_from', type: 'list' },
      { key: 'allow_chats', type: 'list' },
      { key: 'enable_groups', type: 'boolean' },
      { key: 'require_mention_in_groups', type: 'boolean' },
    ],
  },
  qq: {
    id: 'qq',
    titleKey: 'qq',
    hintKey: 'qqChannelHint',
    fields: [
      { key: 'enabled', type: 'boolean' },
      { key: 'app_id', type: 'text' },
      { key: 'app_secret', type: 'password' },
      { key: 'allow_from', type: 'list' },
    ],
  },
  dingtalk: {
    id: 'dingtalk',
    titleKey: 'dingtalk',
    hintKey: 'dingtalkChannelHint',
    fields: [
      { key: 'enabled', type: 'boolean' },
      { key: 'client_id', type: 'text' },
      { key: 'client_secret', type: 'password' },
      { key: 'allow_from', type: 'list' },
    ],
  },
  maixcam: {
    id: 'maixcam',
    titleKey: 'maixcam',
    hintKey: 'maixcamChannelHint',
    fields: [
      { key: 'enabled', type: 'boolean' },
      { key: 'host', type: 'text' },
      { key: 'port', type: 'number' },
      { key: 'allow_from', type: 'list' },
    ],
  },
};

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value));
}

function formatList(value: unknown) {
  if (!Array.isArray(value)) return '';
  return value.map((item) => String(item ?? '')).join('\n');
}

function parseList(text: string) {
  return String(text || '')
    .split('\n')
    .map((line) => line.split(','))
    .flat()
    .map((item) => item.trim())
    .filter(Boolean);
}

function getWhatsAppFieldDescription(t: (key: string) => string, fieldKey: string) {
  switch (fieldKey) {
    case 'enabled':
      return t('whatsappFieldEnabledHint');
    case 'bridge_url':
      return t('whatsappFieldBridgeURLHint');
    case 'allow_from':
      return t('whatsappFieldAllowFromHint');
    case 'enable_groups':
      return t('whatsappFieldEnableGroupsHint');
    case 'require_mention_in_groups':
      return t('whatsappFieldRequireMentionHint');
    default:
      return '';
  }
}

function getWhatsAppBooleanIcon(fieldKey: string) {
  switch (fieldKey) {
    case 'enabled':
      return Wifi;
    case 'enable_groups':
      return Users;
    case 'require_mention_in_groups':
      return ShieldCheck;
    default:
      return Check;
  }
}

const ChannelSettings: React.FC = () => {
  const { channelId } = useParams();
  const navigate = useNavigate();
  const { t } = useTranslation();
  const ui = useUI();
  const { cfg, setCfg, q, loadConfig } = useAppContext();
  const key = (channelId || 'whatsapp') as ChannelKey;
  const definition = channelDefinitions[key];

  const [draft, setDraft] = useState<Record<string, any>>({});
  const [saving, setSaving] = useState(false);
  const [waStatus, setWaStatus] = useState<WhatsAppStatusPayload | null>(null);

  useEffect(() => {
    if (!definition) {
      navigate('/channels/whatsapp', { replace: true });
      return;
    }
    const next = clone(((cfg as any)?.channels?.[definition.id] || {}) as Record<string, any>);
    setDraft(next);
  }, [cfg, definition, navigate]);

  useEffect(() => {
    if (key !== 'whatsapp') return;
    let active = true;
    const fetchStatus = async () => {
      try {
        const res = await fetch(`/webui/api/whatsapp/status${q}`);
        const json = await res.json();
        if (active) setWaStatus(json);
      } catch {
        if (active) setWaStatus(null);
      }
    };
    void fetchStatus();
    const timer = window.setInterval(fetchStatus, 3000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [key, q]);

  const qrImageURL = useMemo(() => {
    const updatedAt = waStatus?.status?.updated_at || Date.now();
    const sep = q ? '&' : '?';
    return `/webui/api/whatsapp/qr.svg${q}${sep}ts=${encodeURIComponent(String(updatedAt))}`;
  }, [q, waStatus?.status?.updated_at]);

  if (!definition) return null;

  const saveChannel = async () => {
    setSaving(true);
    try {
      const nextCfg = clone(cfg || {});
      if (!nextCfg.channels || typeof nextCfg.channels !== 'object') {
        (nextCfg as any).channels = {};
      }
      (nextCfg as any).channels[definition.id] = clone(draft);
      const res = await fetch(`/webui/api/config${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(nextCfg),
      });
      if (!res.ok) {
        throw new Error(await res.text());
      }
      setCfg(nextCfg);
      await loadConfig(true);
      await ui.notify(t('configSaved'));
    } catch (err: any) {
      await ui.notify(String(err?.message || err || 'save failed'));
    } finally {
      setSaving(false);
    }
  };

  const handleLogout = async () => {
    const ok = await ui.confirmDialog({
      title: t('whatsappLogoutTitle'),
      message: t('whatsappLogoutMessage'),
      danger: true,
      confirmText: t('logout'),
    });
    if (!ok) return;
    await ui.withLoading(async () => {
      await fetch(`/webui/api/whatsapp/logout${q}`, { method: 'POST' });
    }, t('loading'));
  };

  const renderField = (field: ChannelField) => {
    const label = t(`configLabels.${field.key}`);
    const value = draft[field.key];
    const isWhatsApp = key === 'whatsapp';
    const helper = isWhatsApp ? getWhatsAppFieldDescription(t, field.key) : '';
    if (field.type === 'boolean') {
      if (isWhatsApp) {
        const Icon = getWhatsAppBooleanIcon(field.key);
        return (
          <label key={field.key} className="ui-toggle-card rounded-[24px] p-5 flex items-start justify-between gap-4 min-h-[148px] cursor-pointer transition-colors hover:border-zinc-700">
            <div className="min-w-0 pr-3">
              <div className="flex items-center gap-3">
                <div className={`ui-pill ${value ? 'ui-pill-success' : 'ui-pill-neutral'} flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl border`}>
                  <Icon className="h-4 w-4" />
                </div>
                <div className="min-w-0">
                  <div className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">{label}</div>
                  <div className="mt-1 text-xs text-zinc-500 dark:text-zinc-400">{helper}</div>
                </div>
              </div>
              <div className={`ui-pill mt-4 inline-flex items-center rounded-full border px-2.5 py-1 text-[11px] font-medium ${value ? 'ui-pill-success' : 'ui-pill-neutral'}`}>
                {t(value ? 'enabled_true' : 'enabled_false')}
              </div>
            </div>
            <input
              type="checkbox"
              checked={!!value}
              onChange={(e) => setDraft((prev) => ({ ...prev, [field.key]: e.target.checked }))}
              className="mt-1 h-4 w-4 rounded border-zinc-700/60 text-indigo-500 focus:ring-indigo-500 dark:border-zinc-700"
            />
          </label>
        );
      }
      return (
        <label key={field.key} className="ui-toggle-card p-4 flex items-center justify-between gap-4">
          <div>
            <div className="text-sm font-medium text-zinc-900 dark:text-zinc-100">{label}</div>
            <div className="mt-1 text-xs text-zinc-500 dark:text-zinc-400">{t(value ? 'enabled_true' : 'enabled_false')}</div>
          </div>
          <input
            type="checkbox"
            checked={!!value}
            onChange={(e) => setDraft((prev) => ({ ...prev, [field.key]: e.target.checked }))}
            className="h-4 w-4 rounded border-zinc-700/60 text-indigo-500 focus:ring-indigo-500 dark:border-zinc-700"
          />
        </label>
      );
    }
    if (field.type === 'list') {
      return (
        <div key={field.key} className={`space-y-2 ${isWhatsApp ? 'lg:col-span-2' : ''}`}>
          <div className="flex items-center justify-between gap-3">
            <label className="text-sm font-medium text-zinc-700 dark:text-zinc-200">{label}</label>
            {isWhatsApp && Array.isArray(value) && value.length > 0 && (
              <span className="ui-pill ui-pill-neutral inline-flex items-center rounded-full border px-2.5 py-1 text-[11px] font-medium">
                {t('entries')}: {value.length}
              </span>
            )}
          </div>
          {helper && <div className="text-xs text-zinc-500 dark:text-zinc-400">{helper}</div>}
          <textarea
            value={formatList(value)}
            onChange={(e) => setDraft((prev) => ({ ...prev, [field.key]: parseList(e.target.value) }))}
            placeholder={field.placeholder || ''}
            className={`ui-textarea px-4 py-3 text-sm ${isWhatsApp ? 'min-h-36 font-mono' : 'min-h-28'}`}
          />
          {isWhatsApp && <div className="text-[11px] text-zinc-500 dark:text-zinc-400">{t('whatsappFieldAllowFromFootnote')}</div>}
        </div>
      );
    }
    return (
      <div key={field.key} className={`space-y-2 ${isWhatsApp && field.key === 'bridge_url' ? 'lg:col-span-2' : ''}`}>
        <label className="text-sm font-medium text-zinc-700 dark:text-zinc-200">{label}</label>
        {helper && <div className="text-xs text-zinc-500 dark:text-zinc-400">{helper}</div>}
        <input
          type={field.type}
          value={value === null || value === undefined ? '' : String(value)}
          onChange={(e) => setDraft((prev) => ({ ...prev, [field.key]: field.type === 'number' ? Number(e.target.value || 0) : e.target.value }))}
          placeholder={field.placeholder || ''}
          className={`ui-input px-4 py-3 text-sm ${isWhatsApp && field.key === 'bridge_url' ? 'font-mono' : ''}`}
        />
      </div>
    );
  };

  const wa = waStatus?.status;
  const stateLabel = wa?.connected ? t('online') : wa?.logged_in ? t('whatsappStateDisconnected') : wa?.qr_available ? t('whatsappStateAwaitingScan') : t('offline');

  return (
    <div className="space-y-6 px-5 py-5 md:px-7 md:py-6 xl:px-8">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-zinc-950 dark:text-zinc-50">{t(definition.titleKey)}</h1>
          <p className="mt-1 text-sm text-zinc-500">{t(definition.hintKey)}</p>
        </div>
        <div className="flex items-center gap-2">
          {key === 'whatsapp' && (
            <button onClick={() => window.location.reload()} className="ui-button ui-button-neutral flex items-center gap-2 px-4 py-2 text-sm font-medium">
              <RefreshCw className="h-4 w-4" />
              {t('refresh')}
            </button>
          )}
          <button onClick={saveChannel} disabled={saving} className="ui-button ui-button-primary px-4 py-2 text-sm font-medium">
            {saving ? t('loading') : t('saveChanges')}
          </button>
        </div>
      </div>

      <div className={`grid gap-6 ${key === 'whatsapp' ? 'xl:grid-cols-[1fr_0.92fr]' : ''}`}>
        <div className="brand-card ui-panel rounded-[30px] p-6 space-y-5">
          <div className="grid gap-4 lg:grid-cols-2">
            {definition.fields.map(renderField)}
          </div>
        </div>

        {key === 'whatsapp' && (
          <div className="space-y-6">
            <div className="brand-card ui-panel rounded-[30px] p-6 space-y-5">
              <div className="flex items-start justify-between gap-4">
                <div className="flex items-center gap-3">
                  <div className="ui-subpanel flex h-12 w-12 items-center justify-center">
                    {wa?.connected ? <Wifi className="h-5 w-5 text-emerald-500" /> : <WifiOff className="h-5 w-5 text-amber-500" />}
                  </div>
                  <div>
                    <div className="text-xs uppercase tracking-[0.28em] text-zinc-500">{t('gatewayStatus')}</div>
                    <div className="mt-1 text-2xl font-semibold text-zinc-950 dark:text-zinc-50">{stateLabel}</div>
                  </div>
                </div>
                <button onClick={handleLogout} className="ui-button ui-button-danger flex items-center gap-2 px-4 py-2 text-sm font-medium">
                  <LogOut className="h-4 w-4" />
                  {t('logout')}
                </button>
              </div>

              <div className="grid gap-4 sm:grid-cols-2">
                <div className="ui-subpanel p-4">
                  <div className="text-xs uppercase tracking-[0.25em] text-zinc-500">{t('whatsappBridgeURL')}</div>
                  <div className="mt-2 break-all text-sm text-zinc-700 dark:text-zinc-200">{waStatus?.bridge_url || draft.bridge_url || '-'}</div>
                </div>
                <div className="ui-subpanel p-4">
                  <div className="text-xs uppercase tracking-[0.25em] text-zinc-500">{t('whatsappBridgeAccount')}</div>
                  <div className="mt-2 break-all text-sm text-zinc-700 dark:text-zinc-200">{wa?.user_jid || '-'}</div>
                </div>
                <div className="ui-subpanel p-4">
                  <div className="text-xs uppercase tracking-[0.25em] text-zinc-500">{t('whatsappBridgeLastEvent')}</div>
                  <div className="mt-2 text-sm text-zinc-700 dark:text-zinc-200">{wa?.last_event || '-'}</div>
                </div>
                <div className="ui-subpanel p-4">
                  <div className="text-xs uppercase tracking-[0.25em] text-zinc-500">{t('time')}</div>
                  <div className="mt-2 text-sm text-zinc-700 dark:text-zinc-200">{wa?.updated_at || '-'}</div>
                </div>
                <div className="ui-subpanel p-4">
                  <div className="text-xs uppercase tracking-[0.25em] text-zinc-500">Inbound</div>
                  <div className="mt-2 text-sm text-zinc-700 dark:text-zinc-200">{wa?.inbound_count ?? 0}</div>
                </div>
                <div className="ui-subpanel p-4">
                  <div className="text-xs uppercase tracking-[0.25em] text-zinc-500">Outbound</div>
                  <div className="mt-2 text-sm text-zinc-700 dark:text-zinc-200">{wa?.outbound_count ?? 0}</div>
                </div>
                <div className="ui-subpanel p-4">
                  <div className="text-xs uppercase tracking-[0.25em] text-zinc-500">Read Receipts</div>
                  <div className="mt-2 text-sm text-zinc-700 dark:text-zinc-200">{wa?.read_receipt_count ?? 0}</div>
                </div>
                <div className="ui-subpanel p-4">
                  <div className="text-xs uppercase tracking-[0.25em] text-zinc-500">Last Read</div>
                  <div className="mt-2 text-sm text-zinc-700 dark:text-zinc-200">{wa?.last_read_at || '-'}</div>
                </div>
              </div>

              <div className="grid gap-4 sm:grid-cols-2">
                <div className="ui-subpanel p-4">
                  <div className="text-xs uppercase tracking-[0.25em] text-zinc-500">Last Inbound</div>
                  <div className="mt-2 text-sm text-zinc-700 dark:text-zinc-200">{wa?.last_inbound_at || '-'}</div>
                  <div className="mt-2 text-xs text-zinc-500 break-all">{wa?.last_inbound_from || '-'}</div>
                  <div className="mt-2 text-xs text-zinc-600 dark:text-zinc-300 whitespace-pre-wrap">{wa?.last_inbound_text || '-'}</div>
                </div>
                <div className="ui-subpanel p-4">
                  <div className="text-xs uppercase tracking-[0.25em] text-zinc-500">Last Outbound</div>
                  <div className="mt-2 text-sm text-zinc-700 dark:text-zinc-200">{wa?.last_outbound_at || '-'}</div>
                  <div className="mt-2 text-xs text-zinc-500 break-all">{wa?.last_outbound_to || '-'}</div>
                  <div className="mt-2 text-xs text-zinc-600 dark:text-zinc-300 whitespace-pre-wrap">{wa?.last_outbound_text || '-'}</div>
                </div>
              </div>

              {!!waStatus?.error && (
                <div className="ui-notice-warning rounded-2xl border px-4 py-3 text-sm">
                  {waStatus.error}
                </div>
              )}
              {!!wa?.last_error && (
                <div className="ui-notice-danger rounded-2xl border px-4 py-3 text-sm">
                  {wa.last_error}
                </div>
              )}
            </div>

            <div className="brand-card ui-panel rounded-[30px] p-6 space-y-5">
              <div className="flex items-center gap-3">
                <div className="ui-subpanel flex h-12 w-12 items-center justify-center">
                  <QrCode className="h-5 w-5 text-sky-500" />
                </div>
                <div>
                  <div className="text-xs uppercase tracking-[0.28em] text-zinc-500">{t('whatsappBridgeQRCode')}</div>
                  <div className="mt-1 text-2xl font-semibold text-zinc-950 dark:text-zinc-50">{wa?.qr_available ? t('whatsappQRCodeReady') : t('whatsappQRCodeUnavailable')}</div>
                </div>
              </div>

              {wa?.qr_available ? (
                <div className="ui-soft-panel rounded-[28px] p-5">
                  <img src={qrImageURL} alt={t('whatsappBridgeQRCode')} className="mx-auto block w-full max-w-[360px]" />
                </div>
              ) : (
                <div className="ui-soft-panel rounded-[28px] border-dashed p-8 text-center">
                  <div className="ui-subpanel mx-auto flex h-16 w-16 items-center justify-center rounded-3xl bg-zinc-100/80">
                    <Smartphone className="h-7 w-7 text-zinc-500" />
                  </div>
                  <div className="mt-4 text-base font-medium text-zinc-900 dark:text-zinc-100">{t('whatsappQRCodeUnavailable')}</div>
                  <div className="mt-2 whitespace-pre-line text-sm text-zinc-500">{t('whatsappQRCodeHint')}</div>
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
};

export default ChannelSettings;
