import React from 'react';
import { LogOut, Wifi, WifiOff } from 'lucide-react';
import { FixedButton } from '../ui/Button';
import ChannelSectionCard from './ChannelSectionCard';
import InfoTile from '../data-display/InfoTile';
import NoticePanel from '../layout/NoticePanel';

type Translate = (key: string, options?: any) => string;

type WhatsAppStatusPanelProps = {
  bridgeUrl: string;
  lastError?: string;
  onLogout: () => void;
  stateLabel: string;
  status?: {
    connected?: boolean;
    inbound_count?: number;
    last_error?: string;
    last_event?: string;
    last_inbound_at?: string;
    last_inbound_from?: string;
    last_inbound_text?: string;
    last_outbound_at?: string;
    last_outbound_text?: string;
    last_outbound_to?: string;
    last_read_at?: string;
    outbound_count?: number;
    read_receipt_count?: number;
    updated_at?: string;
    user_jid?: string;
  };
  t: Translate;
};

const WhatsAppStatusPanel: React.FC<WhatsAppStatusPanelProps> = ({
  bridgeUrl,
  lastError,
  onLogout,
  stateLabel,
  status,
  t,
}) => {
  return (
    <ChannelSectionCard
      icon={status?.connected ? <Wifi className="ui-icon-success h-[18px] w-[18px]" /> : <WifiOff className="ui-icon-warning h-[18px] w-[18px]" />}
      title={
        <div className="flex items-center justify-between gap-3">
          <div>
            <div className="ui-text-muted text-xs uppercase tracking-[0.28em]">{t('gatewayStatus')}</div>
            <div className="ui-text-primary mt-1 text-2xl font-semibold">{stateLabel}</div>
          </div>
          <FixedButton onClick={onLogout} variant="danger" label={t('logout')}>
            <LogOut className="h-4 w-4" />
          </FixedButton>
        </div>
      }
    >

      <div className="grid gap-4 sm:grid-cols-2">
        <InfoTile label={t('whatsappBridgeURL')} className="break-all">
          {bridgeUrl || '-'}
        </InfoTile>
        <InfoTile label={t('whatsappBridgeAccount')} className="break-all">
          {status?.user_jid || '-'}
        </InfoTile>
        <InfoTile label={t('whatsappBridgeLastEvent')}>
          {status?.last_event || '-'}
        </InfoTile>
        <InfoTile label={t('time')}>
          {status?.updated_at || '-'}
        </InfoTile>
        <InfoTile label={t('whatsappInbound')}>
          {status?.inbound_count ?? 0}
        </InfoTile>
        <InfoTile label={t('whatsappOutbound')}>
          {status?.outbound_count ?? 0}
        </InfoTile>
        <InfoTile label={t('whatsappReadReceipts')}>
          {status?.read_receipt_count ?? 0}
        </InfoTile>
        <InfoTile label={t('whatsappLastRead')}>
          {status?.last_read_at || '-'}
        </InfoTile>
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <InfoTile label={t('whatsappLastInbound')}>
          <div>{status?.last_inbound_at || '-'}</div>
          <div className="ui-text-muted mt-2 break-all text-xs">{status?.last_inbound_from || '-'}</div>
          <div className="ui-text-subtle mt-2 whitespace-pre-wrap text-xs">{status?.last_inbound_text || '-'}</div>
        </InfoTile>
        <InfoTile label={t('whatsappLastOutbound')}>
          <div>{status?.last_outbound_at || '-'}</div>
          <div className="ui-text-muted mt-2 break-all text-xs">{status?.last_outbound_to || '-'}</div>
          <div className="ui-text-subtle mt-2 whitespace-pre-wrap text-xs">{status?.last_outbound_text || '-'}</div>
        </InfoTile>
      </div>

      {lastError ? <NoticePanel>{lastError}</NoticePanel> : null}
      {status?.last_error ? <NoticePanel tone="danger">{status.last_error}</NoticePanel> : null}
    </ChannelSectionCard>
  );
};

export default WhatsAppStatusPanel;
