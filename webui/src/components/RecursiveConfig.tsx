import React from 'react';
import { useTranslation } from 'react-i18next';

interface RecursiveConfigProps {
  data: any;
  labels: Record<string, string>;
  path?: string;
  onChange: (path: string, val: any) => void;
}

const RecursiveConfig: React.FC<RecursiveConfigProps> = ({ data, labels, path = '', onChange }) => {
  const { t } = useTranslation();
  
  if (typeof data !== 'object' || data === null) return null;

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-x-12 gap-y-10">
      {Object.entries(data).map(([key, value]) => {
        const currentPath = path ? `${path}.${key}` : key;
        const label = labels[key] || key.replace(/_/g, ' ');
        
        if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
          return (
            <div key={currentPath} className="space-y-6 col-span-full">
              <h3 className="text-sm font-semibold text-zinc-400 uppercase tracking-wider flex items-center gap-2">
                <span className="w-1.5 h-4 bg-indigo-500 rounded-full" />
                {label}
              </h3>
              <div className="pl-6 border-l border-zinc-800/50">
                <RecursiveConfig data={value} labels={labels} path={currentPath} onChange={onChange} />
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
