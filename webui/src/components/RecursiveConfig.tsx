import React, { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

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
        {value.length === 0 && <span className="text-xs text-zinc-500 italic">{t('empty')}</span>}
        {value.map((item, idx) => (
          <span key={`${item}-${idx}`} className="inline-flex items-center gap-1 px-2 py-1 rounded bg-zinc-900 border border-zinc-700 text-xs font-mono text-zinc-200">
            {String(item)}
            <button onClick={() => removeAt(idx)} className="text-zinc-400 hover:text-red-400">×</button>
          </span>
        ))}
      </div>

      <div className="grid grid-cols-1 md:grid-cols-[1fr_auto_auto] gap-2">
        <input
          list={`${path}-suggestions`}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder={t('recursiveAddValuePlaceholder')}
          className="w-full bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500"
        />
        <datalist id={`${path}-suggestions`}>
          {suggestions.map((s) => (
            <option key={s} value={s} />
          ))}
        </datalist>

        <button
          onClick={() => {
            addValue(draft);
            setDraft('');
          }}
          className="px-3 py-2 text-xs rounded-lg bg-zinc-800 hover:bg-zinc-700"
        >
          {t('add')}
        </button>

        <select
          value={selected}
          onChange={(e) => {
            const v = e.target.value;
            setSelected(v);
            if (v) addValue(v);
          }}
          className="px-3 py-2 text-xs rounded-lg bg-zinc-950 border border-zinc-800"
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
                <span className="text-sm font-medium text-zinc-300 block capitalize">{label}</span>
                <span className="text-[10px] text-zinc-600 font-mono">{currentPath}</span>
              </div>
              <div className="p-3 bg-zinc-950 border border-zinc-800 rounded-lg">
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
                    className="w-full min-h-28 bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-2 text-sm font-mono focus:outline-none focus:border-indigo-500"
                  />
                )}
              </div>
            </div>
          );
        }

        if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
          return (
            <div key={currentPath} className="space-y-6 col-span-full">
              <h3 className="text-sm font-semibold text-zinc-400 uppercase tracking-wider flex items-center gap-2">
                <span className="w-1.5 h-4 bg-indigo-500 rounded-full" />
                {label}
              </h3>
              <div className="pl-6 border-l border-zinc-800/50">
                <RecursiveConfig data={value} labels={labels} path={currentPath} onChange={onChange} hotPaths={hotPaths} onlyHot={onlyHot} />
              </div>
            </div>
          );
        }

        return (
          <div key={currentPath} className="space-y-2">
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium text-zinc-300 block capitalize">{label}</span>
              <span className="text-[10px] text-zinc-600 font-mono">{currentPath}</span>
            </div>
            {typeof value === 'boolean' ? (
              <label className="flex items-center gap-3 p-3 bg-zinc-950 border border-zinc-800 rounded-lg cursor-pointer hover:border-zinc-700 transition-colors group">
                <input
                  type="checkbox"
                  checked={value}
                  onChange={(e) => onChange(currentPath, e.target.checked)}
                  className="w-4 h-4 rounded border-zinc-700 text-indigo-500 focus:ring-indigo-500 focus:ring-offset-zinc-950 bg-zinc-900"
                />
                <span className="text-sm text-zinc-400 group-hover:text-zinc-300 transition-colors">
                  {value ? (labels['enabled_true'] || t('enabled_true')) : (labels['enabled_false'] || t('enabled_false'))}
                </span>
              </label>
            ) : (
              <input
                type={typeof value === 'number' ? 'number' : 'text'}
                value={value === null || value === undefined ? '' : String(value)}
                onChange={(e) => onChange(currentPath, typeof value === 'number' ? Number(e.target.value) : e.target.value)}
                className="w-full bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-2.5 text-sm focus:outline-none focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 transition-colors font-mono text-zinc-300"
              />
            )}
          </div>
        );
      })}
    </div>
  );
};

export default RecursiveConfig;
