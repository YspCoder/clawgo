import React from 'react';
import { RefreshCw, Server } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';

const Nodes: React.FC = () => {
  const { t } = useTranslation();
  const { nodes, refreshNodes } = useAppContext();

  return (
    <div className="p-8 max-w-7xl mx-auto space-y-8 h-full flex flex-col">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold tracking-tight">{t('nodes')}</h1>
        <button onClick={refreshNodes} className="flex items-center gap-2 px-4 py-2 bg-zinc-800 hover:bg-zinc-700 rounded-lg text-sm font-medium transition-colors">
          <RefreshCw className="w-4 h-4" /> {t('refresh')}
        </button>
      </div>

      <div className="flex-1 bg-zinc-900/40 border border-zinc-800/80 rounded-2xl p-6 flex flex-col shadow-sm overflow-hidden">
        <div className="flex-1 bg-zinc-950/80 rounded-xl border border-zinc-800/50 p-4 overflow-auto">
          {(() => {
            try {
              const parsedNodes = JSON.parse(nodes);
              if (!Array.isArray(parsedNodes) || parsedNodes.length === 0) {
                return (
                  <div className="h-full flex flex-col items-center justify-center text-zinc-500 space-y-4">
                    <Server className="w-12 h-12 opacity-20" />
                    <p className="text-lg font-medium">{t('noNodes')}</p>
                  </div>
                );
              }
              return (
                <div className="space-y-3">
                  {parsedNodes.map((node: any, i: number) => (
                    <div key={node.id || i} className="flex items-center justify-between p-4 bg-zinc-900/50 rounded-xl border border-zinc-800/50 hover:border-zinc-700/50 transition-colors">
                      <div className="flex items-center gap-4">
                        <div className={`w-3 h-3 rounded-full ${node.online ? 'bg-emerald-500 shadow-[0_0_8px_rgba(16,185,129,0.5)]' : 'bg-red-500 shadow-[0_0_8px_rgba(239,68,68,0.5)]'}`} />
                        <div>
                          <div className="font-semibold text-zinc-200">{node.name || node.id || `Node ${i + 1}`}</div>
                          <div className="text-xs text-zinc-500 font-mono mt-0.5">{node.id}</div>
                        </div>
                      </div>
                      <div className="text-right">
                        <div className="text-xs font-mono text-zinc-400 bg-zinc-800/50 px-2 py-1 rounded">{node.ip || 'Unknown IP'}</div>
                        <div className="text-[10px] text-zinc-600 mt-1 font-mono uppercase tracking-widest">v{node.version || '0.0.0'}</div>
                      </div>
                    </div>
                  ))}
                </div>
              );
            } catch (e) {
              return <pre className="font-mono text-[13px] leading-relaxed text-zinc-400 whitespace-pre-wrap">{nodes}</pre>;
            }
          })()}
        </div>
      </div>
    </div>
  );
};

export default Nodes;
