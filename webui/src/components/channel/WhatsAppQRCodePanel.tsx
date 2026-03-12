import React from 'react';
import { QrCode, Smartphone } from 'lucide-react';
import ChannelSectionCard from './ChannelSectionCard';
import EmptyState from '../data-display/EmptyState';

type Translate = (key: string, options?: any) => string;

type WhatsAppQRCodePanelProps = {
  qrAvailable?: boolean;
  qrImageURL: string;
  t: Translate;
};

const WhatsAppQRCodePanel: React.FC<WhatsAppQRCodePanelProps> = ({
  qrAvailable,
  qrImageURL,
  t,
}) => {
  return (
    <ChannelSectionCard
      icon={<QrCode className="ui-icon-info h-[18px] w-[18px]" />}
      title={
        <>
          <div className="ui-text-muted text-xs uppercase tracking-[0.28em]">{t('whatsappBridgeQRCode')}</div>
          <div className="ui-text-primary mt-1 text-2xl font-semibold">
            {qrAvailable ? t('whatsappQRCodeReady') : t('whatsappQRCodeUnavailable')}
          </div>
        </>
      }
    >
      {qrAvailable ? (
        <div className="ui-soft-panel rounded-[28px] p-5">
          <img src={qrImageURL} alt={t('whatsappBridgeQRCode')} className="mx-auto block w-full max-w-[360px]" />
        </div>
      ) : (
        <EmptyState
          centered
          dashed
          className="ui-soft-panel rounded-[28px] p-8"
          icon={
            <div className="ui-subpanel ui-surface-strong mx-auto flex h-16 w-16 items-center justify-center rounded-3xl">
              <Smartphone className="ui-icon-muted h-7 w-7" />
            </div>
          }
          title={t('whatsappQRCodeUnavailable')}
          message={<div className="ui-text-muted whitespace-pre-line text-sm">{t('whatsappQRCodeHint')}</div>}
        />
      )}
    </ChannelSectionCard>
  );
};

export default WhatsAppQRCodePanel;
