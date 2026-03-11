import React from 'react';
import { Check, ShieldCheck, Users, Wifi } from 'lucide-react';
import { CheckboxCardField, FieldBlock, TextField } from '../FormControls';
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

type TagListFieldProps = {
  isWhatsApp: boolean;
  onChange: (values: string[]) => void;
  placeholder?: string;
  value: unknown;
};

function TagListField({ isWhatsApp, onChange, placeholder, value }: TagListFieldProps) {
  const values = Array.isArray(value) ? value.map((item) => String(item || '').trim()).filter(Boolean) : [];
  const [draft, setDraft] = React.useState('');

  React.useEffect(() => {
    setDraft('');
  }, [value]);

  function commit(raw: string) {
    const items = String(raw || '')
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean);
    if (items.length === 0) {
      setDraft('');
      return;
    }
    const next = [...values];
    items.forEach((item) => {
      if (!next.includes(item)) next.push(item);
    });
    onChange(next);
    setDraft('');
  }

  function remove(item: string) {
    onChange(values.filter((value) => value !== item));
  }

  return (
    <div className="space-y-2">
      {values.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {values.map((item) => (
            <button
              key={item}
              type="button"
              onClick={() => remove(item)}
              className={`inline-flex items-center rounded-full border px-2.5 py-1 text-[11px] transition ${
                isWhatsApp
                  ? 'border-zinc-700 bg-zinc-950/70 text-zinc-200 hover:border-zinc-500'
                  : 'border-zinc-700 bg-zinc-900/60 text-zinc-200 hover:border-zinc-500'
              }`}
              title={item}
            >
              <span className="font-mono">{item}</span>
            </button>
          ))}
        </div>
      ) : null}
      <TextField
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault();
            commit(draft);
          } else if (e.key === 'Backspace' && !draft && values.length > 0) {
            e.preventDefault();
            remove(values[values.length - 1]);
          }
        }}
        onBlur={() => {
          if (draft.trim()) commit(draft);
        }}
        placeholder={placeholder || ''}
        monospace={isWhatsApp}
      />
    </div>
  );
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
        <CheckboxCardField
          key={field.key}
          className="ui-boolean-card-detailed"
          checked={!!value}
          help={(
            <div className="ui-boolean-head">
              <div className={`ui-pill ${value ? 'ui-pill-success' : 'ui-pill-neutral'} flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl border`}>
                <Icon className="h-4 w-4" />
              </div>
              <div className="min-w-0">
                <div className="ui-form-help">{helper}</div>
              </div>
            </div>
          )}
          label={label}
          onChange={(checked) => setDraft((prev) => ({ ...prev, [field.key]: checked }))}
        />
      );
    }

    return (
      <CheckboxCardField
        key={field.key}
        className="ui-boolean-card"
        checked={!!value}
        help={helper}
        label={label}
        onChange={(checked) => setDraft((prev) => ({ ...prev, [field.key]: checked }))}
      />
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
        <TagListField
          isWhatsApp={isWhatsApp}
          onChange={(items) => setDraft((prev) => ({ ...prev, [field.key]: parseList(items.join('\n')) }))}
          placeholder={field.placeholder || ''}
          value={value}
        />
        <div className="ui-form-help text-[11px]">{t('channelListInputFootnote')}</div>
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
