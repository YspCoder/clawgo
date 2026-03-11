import React, { useMemo, useState } from 'react';
import { Button } from '../Button';
import { TextField } from '../FormControls';

type ProviderModelPickerModalProps = {
  initialValue?: string;
  models: string[];
  onCancel: () => void;
  onConfirm: (model: string) => void;
  t: (key: string, options?: any) => string;
};

export const ProviderModelPickerModal: React.FC<ProviderModelPickerModalProps> = ({
  initialValue,
  models,
  onCancel,
  onConfirm,
  t,
}) => {
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState(initialValue || models[0] || '');

  const filtered = useMemo(() => {
    const keyword = String(query || '').trim().toLowerCase();
    if (!keyword) return models;
    return models.filter((model) => model.toLowerCase().includes(keyword));
  }, [models, query]);

  return (
    <div className="space-y-4">
      <div className="text-sm text-zinc-300">{t('providersSelectModelMessage')}</div>
      <TextField
        autoFocus
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder={t('providersSelectModelSearchPlaceholder')}
        className="w-full"
      />
      <div className="max-h-[360px] space-y-2 overflow-auto pr-1">
        {filtered.map((model) => {
          const active = model === selected;
          return (
            <button
              key={model}
              type="button"
              onClick={() => setSelected(model)}
              className={[
                'w-full rounded-xl border px-3 py-2 text-left transition',
                active ? 'border-amber-400 bg-amber-500/10 text-zinc-100' : 'border-zinc-800 bg-zinc-950/40 text-zinc-300 hover:border-zinc-700',
              ].join(' ')}
            >
              <div className="font-mono text-sm">{model}</div>
            </button>
          );
        })}
        {filtered.length === 0 && (
          <div className="rounded-xl border border-dashed border-zinc-800 px-3 py-5 text-center text-sm text-zinc-500">
            {t('providersSelectModelEmpty')}
          </div>
        )}
      </div>
      <div className="flex items-center justify-end gap-2">
        <Button onClick={onCancel} size="sm">
          {t('cancel')}
        </Button>
        <Button onClick={() => selected && onConfirm(selected)} variant="primary" size="sm" disabled={!selected}>
          {t('providersSelectModelConfirm')}
        </Button>
      </div>
    </div>
  );
};
