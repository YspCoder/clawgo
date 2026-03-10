import React, { useMemo, useState } from 'react';
import { Plus } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { FixedButton } from './Button';

interface RecursiveConfigProps {
  data: any;
  labels: Record<string, string>;
  path?: string;
  onChange: (path: string, val: any) => void;
  hotPaths?: string[];
  onlyHot?: boolean;
}

const isPrimitive = (v: any) => ['string', 'number', 'boolean'].includes(typeof v) || v === null;
const isPathHot = (currentPath: string, hotPaths: string[]) => {
  if (!hotPaths.length) return true;
  return hotPaths.some((hp) => {
    const p = String(hp || '').replace(/\.\*$/, '');
    if (!p) return false;
    return currentPath === p || currentPath.startsWith(`${p}.`) || p.startsWith(`${currentPath}.`);
  });
};

const PrimitiveArrayEditor: React.FC<{
  value: any[];
  path: string;
  onChange: (next: any[]) => void;
}> = ({ value, path, onChange }) => {
  const { t } = useTranslation();
  const [draft, setDraft] = useState('');
  const [selected, setSelected] = useState('');

  const suggestions = useMemo(() => {
    // 基础建议项：从当前值推导 + 针对常见配置路径补充
    const base = new Set<string>(value.map((v) => String(v)));
    if (path.includes('tools') || path.includes('tool')) {
      ['read', 'write', 'edit', 'exec', 'process', 'message', 'nodes', 'memory_search'].forEach((x) => base.add(x));
    }
    if (path.includes('channels')) {
      ['telegram', 'discord', 'whatsapp', 'slack', 'signal'].forEach((x) => base.add(x));
    }
    return Array.from(base).filter(Boolean);
  }, [value, path]);

  const addValue = (v: string) => {
    const val = v.trim();
    if (!val) return;
    if (value.some((x) => String(x) === val)) return;
    onChange([...value, val]);
  };

  const removeAt = (idx: number) => {
    onChange(value.filter((_, i) => i !== idx));
  };

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap gap-2">
        {value.length === 0 && <span className="ui-text-muted text-xs italic">{t('empty')}</span>}
        {value.map((item, idx) => (
          <span key={`${item}-${idx}`} className="ui-text-secondary inline-flex items-center gap-1 px-2 py-1 rounded-xl ui-soft-panel text-xs font-mono">
            {String(item)}
            <button onClick={() => removeAt(idx)} className="ui-text-danger-hover ui-text-subtle">×</button>
          </span>
        ))}
      </div>

      <div className="grid grid-cols-1 md:grid-cols-[1fr_auto_auto] gap-2">
        <input
          list={`${path}-suggestions`}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder={t('recursiveAddValuePlaceholder')}
          className="ui-input rounded-xl px-3 py-2 text-sm"
        />
        <datalist id={`${path}-suggestions`}>
          {suggestions.map((s) => (
            <option key={s} value={s} />
          ))}
        </datalist>

        <FixedButton
          onClick={() => {
            addValue(draft);
            setDraft('');
          }}
          radius="xl"
          label={t('add')}
        >
          <Plus className="w-4 h-4" />
        </FixedButton>

        <select
          value={selected}
          onChange={(e) => {
            const v = e.target.value;
            setSelected(v);
            if (v) addValue(v);
          }}
          className="ui-select px-3 py-2 text-xs rounded-xl"
        >
          <option value="">{t('recursiveSelectOption')}</option>
          {suggestions.filter((s) => !value.includes(s)).map((s) => (
            <option key={s} value={s}>{s}</option>
          ))}
        </select>
      </div>
    </div>
  );
};

const RecursiveConfig: React.FC<RecursiveConfigProps> = ({ data, labels, path = '', onChange, hotPaths = [], onlyHot = false }) => {
  const { t } = useTranslation();

  if (typeof data !== 'object' || data === null) return null;

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-x-10 gap-y-8">
      {Object.entries(data).map(([key, value]) => {
        const currentPath = path ? `${path}.${key}` : key;
        const label = labels[key] || key.replace(/_/g, ' ');
        if (onlyHot && !isPathHot(currentPath, hotPaths)) {
          return null;
        }

        if (Array.isArray(value)) {
          const allPrimitive = value.every(isPrimitive);
          return (
            <div key={currentPath} className="space-y-2 col-span-full">
              <div className="flex items-center justify-between">
                <span className="ui-text-secondary text-sm font-medium block capitalize">{label}</span>
                <span className="ui-text-subtle text-[10px] font-mono">{currentPath}</span>
              </div>
              <div className="ui-soft-panel p-3">
                {allPrimitive ? (
                  <PrimitiveArrayEditor
                    value={value}
                    path={currentPath}
                    onChange={(next) => onChange(currentPath, next)}
                  />
                ) : (
                  <textarea
                    value={JSON.stringify(value, null, 2)}
                    onChange={(e) => {
                      try {
                        const arr = JSON.parse(e.target.value);
                        if (Array.isArray(arr)) onChange(currentPath, arr);
                      } catch {
                        // ignore invalid json during typing
                      }
                    }}
                    className="ui-textarea w-full min-h-28 rounded-xl px-3 py-2 text-sm font-mono"
                  />
                )}
              </div>
            </div>
          );
        }

        if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
          return (
            <div key={currentPath} className="space-y-6 col-span-full">
              <h3 className="ui-text-subtle text-sm font-semibold uppercase tracking-wider flex items-center gap-2">
                <span className="ui-icon-info w-1.5 h-4 rounded-full" />
                {label}
              </h3>
              <div className="ui-border-subtle pl-6 border-l">
                <RecursiveConfig data={value} labels={labels} path={currentPath} onChange={onChange} hotPaths={hotPaths} onlyHot={onlyHot} />
              </div>
            </div>
          );
        }

        return (
          <div key={currentPath} className="space-y-2">
            <div className="flex items-center justify-between">
              <span className="ui-text-secondary text-sm font-medium block capitalize">{label}</span>
              <span className="ui-text-subtle text-[10px] font-mono">{currentPath}</span>
            </div>
            {typeof value === 'boolean' ? (
              <label className="ui-toggle-card flex items-center gap-3 p-3 cursor-pointer transition-colors group">
                <input
                  type="checkbox"
                  checked={value}
                  onChange={(e) => onChange(currentPath, e.target.checked)}
                  className="w-4 h-4 rounded border-zinc-700 text-indigo-500 focus:ring-indigo-500"
                />
                <span className="ui-text-subtle group-hover:ui-text-secondary text-sm transition-colors">
                  {value ? (labels['enabled_true'] || t('enabled_true')) : (labels['enabled_false'] || t('enabled_false'))}
                </span>
              </label>
            ) : (
              <input
                type={typeof value === 'number' ? 'number' : 'text'}
                value={value === null || value === undefined ? '' : String(value)}
                onChange={(e) => onChange(currentPath, typeof value === 'number' ? Number(e.target.value) : e.target.value)}
                className="ui-input w-full rounded-xl px-3 py-2.5 text-sm transition-colors font-mono"
              />
            )}
          </div>
        );
      })}
    </div>
  );
};

export default RecursiveConfig;
