import React, { useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Check, KeyRound, ListFilter, LogOut, QrCode, RefreshCw, ShieldCheck, Smartphone, Users, Wifi, WifiOff } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
import { Button, FixedButton } from '../components/Button';
import Checkbox from '../components/Checkbox';
import FormField from '../components/FormField';
import Input from '../components/Input';
import Textarea from '../components/Textarea';

type ChannelKey = 'telegram' | 'whatsapp' | 'discord' | 'feishu' | 'qq' | 'dingtalk' | 'maixcam';

type ChannelField =
  | { key: string; type: 'text' | 'password' | 'number'; placeholder?: string }
  | { key: string; type: 'boolean' }
  | { key: string; type: 'list'; placeholder?: string };

type ChannelDefinition = {
  id: ChannelKey;
  titleKey: string;
  hintKey: string;
  sections: Array<{
    id: string;
    titleKey: string;
    hintKey: string;
    fields: ChannelField[];
    columns?: 1 | 2;
  }>;
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
    sections: [
      {
        id: 'connection',
        titleKey: 'channelSectionConnection',
        hintKey: 'channelSectionConnectionHint',
        columns: 2,
        fields: [
          { key: 'enabled', type: 'boolean' },
          { key: 'token', type: 'password' },
          { key: 'streaming', type: 'boolean' },
        ],
      },
      {
        id: 'access',
        titleKey: 'channelSectionAccess',
        hintKey: 'channelSectionAccessHint',
        columns: 2,
        fields: [
          { key: 'allow_from', type: 'list', placeholder: '123456789' },
          { key: 'allow_chats', type: 'list', placeholder: 'telegram:123456789' },
        ],
      },
      {
        id: 'groups',
        titleKey: 'channelSectionGroupPolicy',
        hintKey: 'channelSectionGroupPolicyHint',
        columns: 2,
        fields: [
          { key: 'enable_groups', type: 'boolean' },
          { key: 'require_mention_in_groups', type: 'boolean' },
        ],
      },
    ],
  },
  whatsapp: {
    id: 'whatsapp',
    titleKey: 'whatsappBridge',
    hintKey: 'whatsappBridgeHint',
    sections: [
      {
        id: 'connection',
        titleKey: 'channelSectionConnection',
        hintKey: 'channelSectionConnectionHint',
        columns: 2,
        fields: [
          { key: 'enabled', type: 'boolean' },
          { key: 'bridge_url', type: 'text' },
        ],
      },
      {
        id: 'access',
        titleKey: 'channelSectionAccess',
        hintKey: 'channelSectionAccessHint',
        columns: 1,
        fields: [
          { key: 'allow_from', type: 'list', placeholder: '8613012345678@s.whatsapp.net' },
        ],
      },
      {
        id: 'groups',
        titleKey: 'channelSectionGroupPolicy',
        hintKey: 'channelSectionGroupPolicyHint',
        columns: 2,
        fields: [
          { key: 'enable_groups', type: 'boolean' },
          { key: 'require_mention_in_groups', type: 'boolean' },
        ],
      },
    ],
  },
  discord: {
    id: 'discord',
    titleKey: 'discord',
    hintKey: 'discordChannelHint',
    sections: [
      {
        id: 'connection',
        titleKey: 'channelSectionConnection',
        hintKey: 'channelSectionConnectionHint',
        columns: 2,
        fields: [
          { key: 'enabled', type: 'boolean' },
          { key: 'token', type: 'password' },
        ],
      },
      {
        id: 'access',
        titleKey: 'channelSectionAccess',
        hintKey: 'channelSectionAccessHint',
        columns: 1,
        fields: [
          { key: 'allow_from', type: 'list', placeholder: 'discord-user-id' },
        ],
      },
    ],
  },
  feishu: {
    id: 'feishu',
    titleKey: 'feishu',
    hintKey: 'feishuChannelHint',
    sections: [
      {
        id: 'connection',
        titleKey: 'channelSectionConnection',
        hintKey: 'channelSectionConnectionHint',
        columns: 2,
        fields: [
          { key: 'enabled', type: 'boolean' },
          { key: 'app_id', type: 'text' },
          { key: 'app_secret', type: 'password' },
          { key: 'encrypt_key', type: 'password' },
          { key: 'verification_token', type: 'password' },
        ],
      },
      {
        id: 'access',
        titleKey: 'channelSectionAccess',
        hintKey: 'channelSectionAccessHint',
        columns: 2,
        fields: [
          { key: 'allow_from', type: 'list' },
          { key: 'allow_chats', type: 'list' },
        ],
      },
      {
        id: 'groups',
        titleKey: 'channelSectionGroupPolicy',
        hintKey: 'channelSectionGroupPolicyHint',
        columns: 2,
        fields: [
          { key: 'enable_groups', type: 'boolean' },
          { key: 'require_mention_in_groups', type: 'boolean' },
        ],
      },
    ],
  },
  qq: {
    id: 'qq',
    titleKey: 'qq',
    hintKey: 'qqChannelHint',
    sections: [
      {
        id: 'connection',
        titleKey: 'channelSectionConnection',
        hintKey: 'channelSectionConnectionHint',
        columns: 2,
        fields: [
          { key: 'enabled', type: 'boolean' },
          { key: 'app_id', type: 'text' },
          { key: 'app_secret', type: 'password' },
        ],
      },
      {
        id: 'access',
        titleKey: 'channelSectionAccess',
        hintKey: 'channelSectionAccessHint',
        columns: 1,
        fields: [
          { key: 'allow_from', type: 'list' },
        ],
      },
    ],
  },
  dingtalk: {
    id: 'dingtalk',
    titleKey: 'dingtalk',
    hintKey: 'dingtalkChannelHint',
    sections: [
      {
        id: 'connection',
        titleKey: 'channelSectionConnection',
        hintKey: 'channelSectionConnectionHint',
        columns: 2,
        fields: [
          { key: 'enabled', type: 'boolean' },
          { key: 'client_id', type: 'text' },
          { key: 'client_secret', type: 'password' },
        ],
      },
      {
        id: 'access',
        titleKey: 'channelSectionAccess',
        hintKey: 'channelSectionAccessHint',
        columns: 1,
        fields: [
          { key: 'allow_from', type: 'list' },
        ],
      },
    ],
  },
  maixcam: {
    id: 'maixcam',
    titleKey: 'maixcam',
    hintKey: 'maixcamChannelHint',
    sections: [
      {
        id: 'network',
        titleKey: 'channelSectionNetwork',
        hintKey: 'channelSectionNetworkHint',
        columns: 2,
        fields: [
          { key: 'enabled', type: 'boolean' },
          { key: 'host', type: 'text' },
          { key: 'port', type: 'number' },
        ],
      },
      {
        id: 'access',
        titleKey: 'channelSectionAccess',
        hintKey: 'channelSectionAccessHint',
        columns: 1,
        fields: [
          { key: 'allow_from', type: 'list' },
        ],
      },
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

function getSectionIcon(sectionID: string) {
  switch (sectionID) {
    case 'connection':
      return KeyRound;
    case 'access':
      return ListFilter;
    case 'groups':
      return Users;
    case 'network':
      return Smartphone;
    default:
      return Check;
  }
}

function getChannelFieldDescription(t: (key: string) => string, channelKey: ChannelKey, fieldKey: string) {
  if (channelKey === 'whatsapp') return getWhatsAppFieldDescription(t, fieldKey);
  const map: Partial<Record<ChannelKey, Partial<Record<string, string>>>> = {
    telegram: {
      enabled: 'channelFieldTelegramEnabledHint',
      token: 'channelFieldTelegramTokenHint',
      streaming: 'channelFieldTelegramStreamingHint',
      allow_from: 'channelFieldTelegramAllowFromHint',
      allow_chats: 'channelFieldTelegramAllowChatsHint',
      enable_groups: 'channelFieldEnableGroupsHint',
      require_mention_in_groups: 'channelFieldRequireMentionHint',
    },
    discord: {
      enabled: 'channelFieldDiscordEnabledHint',
      token: 'channelFieldDiscordTokenHint',
      allow_from: 'channelFieldDiscordAllowFromHint',
    },
    feishu: {
      enabled: 'channelFieldFeishuEnabledHint',
      app_id: 'channelFieldFeishuAppIDHint',
      app_secret: 'channelFieldFeishuAppSecretHint',
      encrypt_key: 'channelFieldFeishuEncryptKeyHint',
      verification_token: 'channelFieldFeishuVerificationTokenHint',
      allow_from: 'channelFieldFeishuAllowFromHint',
      allow_chats: 'channelFieldFeishuAllowChatsHint',
      enable_groups: 'channelFieldEnableGroupsHint',
      require_mention_in_groups: 'channelFieldRequireMentionHint',
    },
    qq: {
      enabled: 'channelFieldQQEnabledHint',
      app_id: 'channelFieldQQAppIDHint',
      app_secret: 'channelFieldQQAppSecretHint',
      allow_from: 'channelFieldQQAllowFromHint',
    },
    dingtalk: {
      enabled: 'channelFieldDingTalkEnabledHint',
      client_id: 'channelFieldDingTalkClientIDHint',
      client_secret: 'channelFieldDingTalkClientSecretHint',
      allow_from: 'channelFieldDingTalkAllowFromHint',
    },
    maixcam: {
      enabled: 'channelFieldMaixCamEnabledHint',
      host: 'channelFieldMaixCamHostHint',
      port: 'channelFieldMaixCamPortHint',
      allow_from: 'channelFieldMaixCamAllowFromHint',
    },
  };
  const key = map[channelKey]?.[fieldKey];
  return key ? t(key) : '';
}

const ChannelSettings: React.FC = () => {
  const { channelId } = useParams();
  const navigate = useNavigate();
  const { t } = useTranslation();
  const ui = useUI();
  const { cfg, setCfg, q, loadConfig, compiledChannels } = useAppContext();
  const availableChannelKeys = useMemo(
    () => (Object.keys(channelDefinitions) as ChannelKey[]).filter((item) => compiledChannels.includes(item)),
    [compiledChannels],
  );
  const fallbackChannel = availableChannelKeys[0];
  const key = (channelId || fallbackChannel || 'whatsapp') as ChannelKey;
  const definition = channelDefinitions[key];

  const [draft, setDraft] = useState<Record<string, any>>({});
  const [saving, setSaving] = useState(false);
  const [waStatus, setWaStatus] = useState<WhatsAppStatusPayload | null>(null);

  useEffect(() => {
    if (!fallbackChannel) {
      navigate('/config', { replace: true });
      return;
    }
    if (!definition || !availableChannelKeys.includes(key)) {
      navigate(`/channels/${fallbackChannel}`, { replace: true });
      return;
    }
    const next = clone(((cfg as any)?.channels?.[definition.id] || {}) as Record<string, any>);
    setDraft(next);
  }, [availableChannelKeys, cfg, definition, fallbackChannel, key, navigate]);

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

  if (!definition || !availableChannelKeys.includes(key)) return null;

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
      const res = await fetch(`/webui/api/whatsapp/logout${q}`, { method: 'POST' });
      if (!res.ok) {
        const message = (await res.text()) || `HTTP ${res.status}`;
        throw new Error(message);
      }
      const json = await res.json().catch(() => null);
      if (json && typeof json === 'object') {
        setWaStatus((prev) => ({ ...(prev || {}), ok: true, status: json as any }));
      }
    }, t('loading'));
  };

  const renderField = (field: ChannelField) => {
    const label = t(`configLabels.${field.key}`);
    const value = draft[field.key];
    const isWhatsApp = key === 'whatsapp';
    const helper = getChannelFieldDescription(t, key, field.key);
    if (field.type === 'boolean') {
      if (isWhatsApp) {
        const Icon = getWhatsAppBooleanIcon(field.key);
        return (
          <label key={field.key} className="ui-toggle-card ui-boolean-card-detailed flex items-start justify-between gap-4 cursor-pointer">
            <div className="min-w-0 flex-1 pr-3">
              <div className="ui-boolean-head">
                <div className={`ui-pill ${value ? 'ui-pill-success' : 'ui-pill-neutral'} flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl border`}>
                  <Icon className="h-4 w-4" />
                </div>
                <div className="min-w-0">
                  <div className="ui-text-primary text-sm font-semibold">{label}</div>
                  <div className="ui-form-help mt-1">{helper}</div>
                </div>
              </div>
              <div className={`ui-pill mt-4 inline-flex items-center rounded-full border px-2.5 py-1 text-[11px] font-medium ${value ? 'ui-pill-success' : 'ui-pill-neutral'}`}>
                {t(value ? 'enabled_true' : 'enabled_false')}
              </div>
            </div>
            <Checkbox
              checked={!!value}
              onChange={(e) => setDraft((prev) => ({ ...prev, [field.key]: e.target.checked }))}
              className="mt-1"
            />
          </label>
        );
      }
      return (
        <label key={field.key} className="ui-toggle-card ui-boolean-card flex items-center justify-between gap-4 cursor-pointer">
          <div className="min-w-0 flex-1 pr-3">
            <div className="ui-text-primary text-sm font-semibold">{label}</div>
            <div className={`ui-pill mt-3 inline-flex items-center rounded-full border px-2.5 py-1 text-[11px] font-medium ${value ? 'ui-pill-success' : 'ui-pill-neutral'}`}>
              {t(value ? 'enabled_true' : 'enabled_false')}
            </div>
          </div>
          <Checkbox
            checked={!!value}
            onChange={(e) => setDraft((prev) => ({ ...prev, [field.key]: e.target.checked }))}
          />
        </label>
      );
    }
    if (field.type === 'list') {
      return (
        <div key={field.key} className={`ui-form-field ${isWhatsApp ? 'lg:col-span-2' : ''}`}>
          <div className="flex items-center justify-between gap-3">
            <label className="ui-form-label">{label}</label>
            {isWhatsApp && Array.isArray(value) && value.length > 0 && (
              <span className="ui-pill ui-pill-neutral inline-flex items-center rounded-full border px-2.5 py-1 text-[11px] font-medium">
                {t('entries')}: {value.length}
              </span>
            )}
          </div>
          {helper && <div className="ui-form-help">{helper}</div>}
          <Textarea
            value={formatList(value)}
            onChange={(e) => setDraft((prev) => ({ ...prev, [field.key]: parseList(e.target.value) }))}
            placeholder={field.placeholder || ''}
            className={`px-4 py-3 text-sm ${isWhatsApp ? 'min-h-36 font-mono' : 'min-h-32'}`}
          />
          {isWhatsApp && <div className="ui-form-help text-[11px]">{t('whatsappFieldAllowFromFootnote')}</div>}
        </div>
      );
    }
    return (
      <FormField
        key={field.key}
        label={label}
        help={helper}
        className={`ui-form-field ${isWhatsApp && field.key === 'bridge_url' ? 'lg:col-span-2' : ''}`}
      >
        <Input
          type={field.type}
          value={value === null || value === undefined ? '' : String(value)}
          onChange={(e) => setDraft((prev) => ({ ...prev, [field.key]: field.type === 'number' ? Number(e.target.value || 0) : e.target.value }))}
          placeholder={field.placeholder || ''}
          className={`px-4 py-3 text-sm ${isWhatsApp && field.key === 'bridge_url' ? 'font-mono' : ''}`}
        />
      </FormField>
    );
  };

  const wa = waStatus?.status;
  const stateLabel = wa?.connected ? t('online') : wa?.logged_in ? t('whatsappStateDisconnected') : wa?.qr_available ? t('whatsappStateAwaitingScan') : t('offline');
  const canLogout = !!(wa?.logged_in || wa?.connected || wa?.user_jid);

  return (
    <div className="space-y-6 px-5 py-5 md:px-7 md:py-6 xl:px-8">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="ui-text-primary text-3xl font-bold tracking-tight">{t(definition.titleKey)}</h1>
          <p className="ui-text-muted mt-1 text-sm">{t(definition.hintKey)}</p>
        </div>
        <div className="flex items-center gap-2">
          {key === 'whatsapp' && (
            <FixedButton onClick={() => window.location.reload()} label={t('refresh')}>
              <RefreshCw className="h-4 w-4" />
            </FixedButton>
          )}
          <Button onClick={saveChannel} disabled={saving} variant="primary">
            {saving ? t('loading') : t('saveChanges')}
          </Button>
        </div>
      </div>

      <div className={`grid gap-6 ${key === 'whatsapp' ? 'xl:grid-cols-[1fr_0.92fr]' : ''}`}>
        <div className="space-y-4">
          {definition.sections.map((section) => {
            const Icon = getSectionIcon(section.id);
            return (
              <section key={section.id} className="brand-card ui-panel rounded-[30px] p-6 space-y-5">
                <div className="ui-section-header">
                  <div className="ui-subpanel flex h-11 w-11 shrink-0 items-center justify-center">
                    <Icon className="ui-icon-muted h-[18px] w-[18px]" />
                  </div>
                  <div className="min-w-0">
                    <h2 className="ui-text-primary text-lg font-semibold">{t(section.titleKey)}</h2>
                    <p className="ui-text-muted mt-1 text-sm">{t(section.hintKey)}</p>
                  </div>
                </div>
                <div className={`grid gap-4 ${section.columns === 1 ? 'grid-cols-1' : 'lg:grid-cols-2'}`}>
                  {section.fields.map(renderField)}
                </div>
              </section>
            );
          })}
        </div>

        {key === 'whatsapp' && (
          <div className="space-y-6">
            <div className="brand-card ui-panel rounded-[30px] p-6 space-y-5">
              <div className="flex items-start justify-between gap-4">
                <div className="flex items-center gap-3">
                  <div className="ui-subpanel flex h-12 w-12 items-center justify-center">
                    {wa?.connected ? <Wifi className="ui-icon-success h-5 w-5" /> : <WifiOff className="ui-icon-warning h-5 w-5" />}
                  </div>
                  <div>
                    <div className="ui-text-muted text-xs uppercase tracking-[0.28em]">{t('gatewayStatus')}</div>
                    <div className="ui-text-primary mt-1 text-2xl font-semibold">{stateLabel}</div>
                  </div>
                </div>
                {canLogout ? (
                  <Button onClick={handleLogout} variant="danger" gap="2">
                    <LogOut className="h-4 w-4" />
                    {t('logout')}
                  </Button>
                ) : null}
              </div>

              <div className="grid gap-4 sm:grid-cols-2">
                <div className="ui-subpanel p-4">
                  <div className="ui-text-muted text-xs uppercase tracking-[0.25em]">{t('whatsappBridgeURL')}</div>
                  <div className="ui-text-secondary mt-2 break-all text-sm">{waStatus?.bridge_url || draft.bridge_url || '-'}</div>
                </div>
                <div className="ui-subpanel p-4">
                  <div className="ui-text-muted text-xs uppercase tracking-[0.25em]">{t('whatsappBridgeAccount')}</div>
                  <div className="ui-text-secondary mt-2 break-all text-sm">{wa?.user_jid || '-'}</div>
                </div>
                <div className="ui-subpanel p-4">
                  <div className="ui-text-muted text-xs uppercase tracking-[0.25em]">{t('whatsappBridgeLastEvent')}</div>
                  <div className="ui-text-secondary mt-2 text-sm">{wa?.last_event || '-'}</div>
                </div>
                <div className="ui-subpanel p-4">
                  <div className="ui-text-muted text-xs uppercase tracking-[0.25em]">{t('time')}</div>
                  <div className="ui-text-secondary mt-2 text-sm">{wa?.updated_at || '-'}</div>
                </div>
                <div className="ui-subpanel p-4">
                  <div className="ui-text-muted text-xs uppercase tracking-[0.25em]">{t('whatsappInbound')}</div>
                  <div className="ui-text-secondary mt-2 text-sm">{wa?.inbound_count ?? 0}</div>
                </div>
                <div className="ui-subpanel p-4">
                  <div className="ui-text-muted text-xs uppercase tracking-[0.25em]">{t('whatsappOutbound')}</div>
                  <div className="ui-text-secondary mt-2 text-sm">{wa?.outbound_count ?? 0}</div>
                </div>
                <div className="ui-subpanel p-4">
                  <div className="ui-text-muted text-xs uppercase tracking-[0.25em]">{t('whatsappReadReceipts')}</div>
                  <div className="ui-text-secondary mt-2 text-sm">{wa?.read_receipt_count ?? 0}</div>
                </div>
                <div className="ui-subpanel p-4">
                  <div className="ui-text-muted text-xs uppercase tracking-[0.25em]">{t('whatsappLastRead')}</div>
                  <div className="ui-text-secondary mt-2 text-sm">{wa?.last_read_at || '-'}</div>
                </div>
              </div>

              <div className="grid gap-4 sm:grid-cols-2">
                <div className="ui-subpanel p-4">
                  <div className="ui-text-muted text-xs uppercase tracking-[0.25em]">{t('whatsappLastInbound')}</div>
                  <div className="ui-text-secondary mt-2 text-sm">{wa?.last_inbound_at || '-'}</div>
                  <div className="ui-text-muted mt-2 break-all text-xs">{wa?.last_inbound_from || '-'}</div>
                  <div className="ui-text-subtle mt-2 whitespace-pre-wrap text-xs">{wa?.last_inbound_text || '-'}</div>
                </div>
                <div className="ui-subpanel p-4">
                  <div className="ui-text-muted text-xs uppercase tracking-[0.25em]">{t('whatsappLastOutbound')}</div>
                  <div className="ui-text-secondary mt-2 text-sm">{wa?.last_outbound_at || '-'}</div>
                  <div className="ui-text-muted mt-2 break-all text-xs">{wa?.last_outbound_to || '-'}</div>
                  <div className="ui-text-subtle mt-2 whitespace-pre-wrap text-xs">{wa?.last_outbound_text || '-'}</div>
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
                  <QrCode className="ui-icon-info h-5 w-5" />
                </div>
                <div>
                  <div className="ui-text-muted text-xs uppercase tracking-[0.28em]">{t('whatsappBridgeQRCode')}</div>
                  <div className="ui-text-primary mt-1 text-2xl font-semibold">{wa?.qr_available ? t('whatsappQRCodeReady') : t('whatsappQRCodeUnavailable')}</div>
                </div>
              </div>

              {wa?.qr_available ? (
                <div className="ui-soft-panel rounded-[28px] p-5">
                  <img src={qrImageURL} alt={t('whatsappBridgeQRCode')} className="mx-auto block w-full max-w-[360px]" />
                </div>
              ) : (
                <div className="ui-soft-panel rounded-[28px] border-dashed p-8 text-center">
                  <div className="ui-subpanel ui-surface-strong mx-auto flex h-16 w-16 items-center justify-center rounded-3xl">
                    <Smartphone className="ui-icon-muted h-7 w-7" />
                  </div>
                  <div className="ui-text-primary mt-4 text-base font-medium">{t('whatsappQRCodeUnavailable')}</div>
                  <div className="ui-text-muted mt-2 whitespace-pre-line text-sm">{t('whatsappQRCodeHint')}</div>
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
