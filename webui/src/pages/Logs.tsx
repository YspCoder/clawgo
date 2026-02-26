import React, { useEffect, useState, useRef } from 'react';
import { Terminal, Trash2, Play, Square } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { LogEntry } from '../types';

const Logs: React.FC = () => {
  const { t } = useTranslation();
  const { q } = useAppContext();
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [isStreaming, setIsStreaming] = useState(true);
  const logEndRef = useRef<HTMLDivElement>(null);
  const abortControllerRef = useRef<AbortController | null>(null);

  const loadRecent = async () => {
    try {
      const r = await fetch(`/webui/api/logs/recent${q ? `${q}&limit=10` : '?limit=10'}`);
      if (!r.ok) return;
      const j = await r.json();
      if (Array.isArray(j.logs)) {
        setLogs(j.logs as LogEntry[]);
      }
    } catch (e) {
      console.error('load recent logs failed', e);
    }
  };

  const startStreaming = async () => {
    if (abortControllerRef.current) abortControllerRef.current.abort();
    abortControllerRef.current = new AbortController();

    try {
      const response = await fetch(`/webui/api/logs/stream${q}`, {
        signal: abortControllerRef.current.signal,
      });

      if (!response.body) return;

      const reader = response.body.getReader();
      const decoder = new TextDecoder();

      while (true) {
        const { value, done } = await reader.read();
        if (done) break;

        const chunk = decoder.decode(value, { stream: true });
        const lines = chunk.split('\n').filter(line => line.trim());

        lines.forEach(line => {
          try {
            const log: LogEntry = JSON.parse(line);
            setLogs(prev => [...prev.slice(-1000), log]);
          } catch (e) {
            // Fallback for non-JSON logs
            setLogs(prev => [...prev.slice(-1000), { time: new Date().toISOString(), level: 'INFO', msg: line }]);
          }
        });
      }
    } catch (e: any) {
      if (e.name !== 'AbortError') {
        console.error('Log stream error:', e);
      }
    }
  };

  useEffect(() => {
    loadRecent();
    if (isStreaming) {
      startStreaming();
    } else {
      if (abortControllerRef.current) abortControllerRef.current.abort();
    }

    return () => {
      if (abortControllerRef.current) abortControllerRef.current.abort();
    };
  }, [isStreaming, q]);

  useEffect(() => {
    logEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [logs]);

  const clearLogs = () => setLogs([]);

  const getLevelColor = (level: string) => {
    switch (level.toUpperCase()) {
      case 'ERROR': return 'text-red-400';
      case 'WARN': return 'text-amber-400';
      case 'DEBUG': return 'text-blue-400';
      default: return 'text-emerald-400';
    }
  };

  return (
    <div className="p-8 max-w-7xl mx-auto space-y-6 h-full flex flex-col">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-semibold tracking-tight">{t('logs')}</h1>
          <div className={`flex items-center gap-1.5 px-2.5 py-0.5 rounded-md text-[10px] font-bold uppercase tracking-wider border ${
            isStreaming ? 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20' : 'bg-zinc-800 text-zinc-500 border-zinc-700'
          }`}>
            <div className={`w-1.5 h-1.5 rounded-full ${isStreaming ? 'bg-emerald-500 animate-pulse' : 'bg-zinc-600'}`} />
            {isStreaming ? 'Live' : 'Paused'}
          </div>
        </div>
        <div className="flex items-center gap-2">
          <button 
            onClick={() => setIsStreaming(!isStreaming)}
            className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${
              isStreaming ? 'bg-zinc-800 hover:bg-zinc-700 text-zinc-300' : 'bg-indigo-600 hover:bg-indigo-500 text-white'
            }`}
          >
            {isStreaming ? <><Square className="w-4 h-4" /> Pause</> : <><Play className="w-4 h-4" /> Resume</>}
          </button>
          <button onClick={clearLogs} className="flex items-center gap-2 px-4 py-2 bg-zinc-800 hover:bg-zinc-700 rounded-lg text-sm font-medium transition-colors text-zinc-300">
            <Trash2 className="w-4 h-4" /> Clear
          </button>
        </div>
      </div>

      <div className="flex-1 bg-zinc-950 border border-zinc-800 rounded-2xl overflow-hidden flex flex-col shadow-2xl">
        <div className="bg-zinc-900/50 px-4 py-2 border-b border-zinc-800 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Terminal className="w-4 h-4 text-zinc-500" />
            <span className="text-xs font-mono text-zinc-500">system.log</span>
          </div>
          <span className="text-[10px] font-mono text-zinc-600 uppercase tracking-widest">{logs.length} entries</span>
        </div>
        <div className="flex-1 overflow-y-auto p-4 font-mono text-[13px] leading-relaxed space-y-1 selection:bg-indigo-500/30">
          {logs.length === 0 && (
            <div className="h-full flex flex-col items-center justify-center text-zinc-700 space-y-2">
              <Terminal className="w-8 h-8 opacity-10" />
              <p>Waiting for logs...</p>
            </div>
          )}
          {logs.map((log, i) => (
            <div key={i} className="group flex gap-4 hover:bg-zinc-900/50 rounded px-2 py-0.5 transition-colors">
              <span className="text-zinc-600 shrink-0 select-none">[{log.time.split('T')[1].split('.')[0]}]</span>
              <span className={`font-bold shrink-0 select-none w-12 ${getLevelColor(log.level)}`}>{log.level.toUpperCase()}</span>
              <span className="text-zinc-300 break-all">{log.msg}</span>
              {Object.keys(log).filter(k => !['time', 'level', 'msg'].includes(k)).map(k => (
                <span key={k} className="text-zinc-500 italic shrink-0 select-none">{k}={JSON.stringify(log[k])}</span>
              ))}
            </div>
          ))}
          <div ref={logEndRef} />
        </div>
      </div>
    </div>
  );
};

export default Logs;
