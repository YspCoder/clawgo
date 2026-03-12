import React, { useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { RefreshCw, Save } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
import { Button, FixedButton } from '../components/ui/Button';
import ChannelSectionCard from '../components/channel/ChannelSectionCard';
import ChannelFieldRenderer from '../components/channel/ChannelFieldRenderer';
import {
  channelDefinitions,
  getChannelFieldDescription,
  getChannelSectionIcon,
  parseChannelList,
} from '../components/channel/channelSchema';
import WhatsAppQRCodePanel from '../components/channel/WhatsAppQRCodePanel';
import WhatsAppStatusPanel from '../components/channel/WhatsAppStatusPanel';
import { SwitchField } from '../components/ui/FormControls';
import PageHeader from '../components/layout/PageHeader';
import type { ChannelKey } from '../components/channel/channelSchema';
import { cloneJSON } from '../utils/object';

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
  const draftRef = React.useRef<Record<string, any>>({});

  const updateDraft: React.Dispatch<React.SetStateAction<Record<string, any>>> = React.useCallback((next) => {
    setDraft((prev) => {
      const resolved = typeof next === 'function' ? (next as (value: Record<string, any>) => Record<string, any>)(prev) : next;
      draftRef.current = resolved;
      return resolved;
    });
  }, []);

  useEffect(() => {
    if (!fallbackChannel) {
      navigate('/config', { replace: true });
      return;
    }
    if (!definition || !availableChannelKeys.includes(key)) {
      navigate(`/channels/${fallbackChannel}`, { replace: true });
      return;
    }
    const next = cloneJSON(((cfg as any)?.channels?.[definition.id] || {}) as Record<string, any>);
    draftRef.current = next;
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
    if (document.activeElement instanceof HTMLElement) {
      document.activeElement.blur();
      await new Promise((resolve) => window.setTimeout(resolve, 0));
    }
    setSaving(true);
    try {
      const nextCfg = cloneJSON(cfg || {});
      if (!nextCfg.channels || typeof nextCfg.channels !== 'object') {
        (nextCfg as any).channels = {};
      }
      (nextCfg as any).channels[definition.id] = cloneJSON(draftRef.current || {});
      const submit = async (confirmRisky: boolean) => {
        const body = confirmRisky ? { ...nextCfg, confirm_risky: true } : nextCfg;
        return ui.withLoading(async () => {
          const res = await fetch(`/webui/api/config${q}`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
          });
          const text = await res.text();
          let data: any = null;
          try {
            data = text ? JSON.parse(text) : null;
          } catch {
            data = null;
          }
          return { ok: res.ok, text, data };
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

      setCfg(nextCfg);
      await loadConfig(true);
      await ui.notify({ title: t('saved'), message: t('configSaved') });
    } catch (err: any) {
      await ui.notify({ title: t('requestFailed'), message: `${t('saveConfigFailed')}: ${String(err?.message || err || 'save failed')}` });
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

  const wa = waStatus?.status;
  const stateLabel = wa?.connected ? t('online') : wa?.logged_in ? t('whatsappStateDisconnected') : wa?.qr_available ? t('whatsappStateAwaitingScan') : t('offline');

  return (
    <div className="space-y-4 px-5 py-5 md:px-7 md:py-6 xl:px-8">
      <PageHeader
        title={t(definition.titleKey)}
        titleClassName="ui-text-primary text-3xl font-bold"
        subtitle={t(definition.hintKey)}
        actions={
          <>
          {key === 'whatsapp' && (
            <FixedButton onClick={() => window.location.reload()} label={t('refresh')}>
              <RefreshCw className="h-4 w-4" />
            </FixedButton>
          )}
          <Button onClick={saveChannel} disabled={saving} variant="primary" size="sm" radius="lg" gap="1">
            <Save className="h-4 w-4" />
            {saving ? t('loading') : t('saveChanges')}
          </Button>
          </>
        }
      />

      <div className={`grid gap-4 ${key === 'whatsapp' ? 'xl:grid-cols-[1fr_0.92fr]' : ''}`}>
        <div className="brand-card ui-panel rounded-2xl p-5">
          {definition.sections.map((section, idx) => {
            const Icon = getChannelSectionIcon(section.id);
            return (
              <div key={section.id}>
                {idx > 0 && <hr className="ui-border-subtle my-4 border-t" />}
                <div className="ui-section-header mb-3">
                  <div className="ui-subpanel flex h-11 w-11 shrink-0 items-center justify-center">
                    <Icon className="ui-icon-muted h-[18px] w-[18px]" />
                  </div>
                  <div className="min-w-0">
                    <div className="ui-text-primary text-lg font-semibold">{t(section.titleKey)}</div>
                    <p className="ui-text-muted mt-0.5 text-sm">{t(section.hintKey)}</p>
                  </div>
                </div>
                <div className={`grid gap-3 ${section.columns === 1 ? 'grid-cols-1' : 'lg:grid-cols-2'}`}>
                  {section.fields.map((field) => (
                    <ChannelFieldRenderer
                      key={field.key}
                      channelKey={key}
                      draft={draft}
                      field={field}
                      getDescription={getChannelFieldDescription}
                      parseList={parseChannelList}
                      setDraft={updateDraft}
                      t={t}
                    />
                  ))}
                </div>
              </div>
            );
          })}
        </div>

        {key === 'whatsapp' && (
          <div className="space-y-6">
            <WhatsAppStatusPanel
              bridgeUrl={waStatus?.bridge_url || draft.bridge_url || '-'}
              lastError={waStatus?.error}
              onLogout={handleLogout}
              stateLabel={stateLabel}
              status={wa}
              t={t}
            />

            <WhatsAppQRCodePanel
              qrAvailable={wa?.qr_available}
              qrImageURL={qrImageURL}
              t={t}
            />
          </div>
        )}
      </div>
    </div>
  );
};

export default ChannelSettings;
