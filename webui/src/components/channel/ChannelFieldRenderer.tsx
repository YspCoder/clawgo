import React from 'react';
import { Check, ShieldCheck, Users, Wifi } from 'lucide-react';
import { CheckboxField, FieldBlock, TextField, TextareaField } from '../FormControls';
import type { ChannelField, ChannelKey } from './channelSchema';

type Translate = (key: string, options?: any) => string;

type ChannelFieldRendererProps = {
  channelKey: ChannelKey;
  draft: Record<string, any>;
  field: ChannelField;
  getDescription: (t: Translate, channelKey: ChannelKey, fieldKey: string) => string;
  parseList: (value: unknown) => string[];
  setDraft: React.Dispatch<React.SetStateAction<Record<string, any>>>;
  t: Translate;
};

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

function formatList(value: unknown) {
  return Array.isArray(value) ? value.join('\n') : '';
}

const ChannelFieldRenderer: React.FC<ChannelFieldRendererProps> = ({
  channelKey,
  draft,
  field,
  getDescription,
  parseList,
  setDraft,
  t,
}) => {
  const label = t(`configLabels.${field.key}`);
  const value = draft[field.key];
  const isWhatsApp = channelKey === 'whatsapp';
  const helper = getDescription(t, channelKey, field.key);

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
          <CheckboxField
            checked={!!value}
            onChange={(e) => setDraft((prev) => ({ ...prev, [field.key]: e.target.checked }))}
            className="ui-checkbox mt-1"
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
        <CheckboxField
          checked={!!value}
          onChange={(e) => setDraft((prev) => ({ ...prev, [field.key]: e.target.checked }))}
          className="ui-checkbox"
        />
      </label>
    );
  }

  if (field.type === 'list') {
    return (
      <FieldBlock
        key={field.key}
        className={`ui-form-field ${isWhatsApp ? 'lg:col-span-2' : ''}`}
        label={label}
        help={helper}
        meta={isWhatsApp && Array.isArray(value) && value.length > 0 ? `${t('entries')}: ${value.length}` : undefined}
      >
        <TextareaField
          value={formatList(value)}
          onChange={(e) => setDraft((prev) => ({ ...prev, [field.key]: parseList(e.target.value) }))}
          placeholder={field.placeholder || ''}
          monospace={isWhatsApp}
          className={isWhatsApp ? 'min-h-36 px-4 py-3' : 'min-h-32 px-4 py-3'}
        />
        {isWhatsApp ? <div className="ui-form-help text-[11px]">{t('whatsappFieldAllowFromFootnote')}</div> : null}
      </FieldBlock>
    );
  }

  return (
    <FieldBlock
      key={field.key}
      className={`ui-form-field ${isWhatsApp && field.key === 'bridge_url' ? 'lg:col-span-2' : ''}`}
      label={label}
      help={helper}
    >
      <TextField
        type={field.type}
        value={value === null || value === undefined ? '' : String(value)}
        onChange={(e) => setDraft((prev) => ({ ...prev, [field.key]: field.type === 'number' ? Number(e.target.value || 0) : e.target.value }))}
        placeholder={field.placeholder || ''}
        className={isWhatsApp && field.key === 'bridge_url' ? 'font-mono' : ''}
      />
    </FieldBlock>
  );
};

export default ChannelFieldRenderer;
